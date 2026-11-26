// Package plugin provides a zero-compile declarative resolver engine for
// video host extractors. Built-in packs are embedded into the binary, and any
// JSON manifest placed in <config.Dir()>/plugins/*.json overrides or extends
// the catalog without recompiling aniclitr.exe.
package plugin

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"aniclitr/internal/config"
	"aniclitr/internal/extract"
)

//go:embed builtin/*.json
var builtinFS embed.FS

// Manifest describes one zero-compile video host extractor.
type Manifest struct {
	Schema         int      `json:"schema"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Author         string   `json:"author,omitempty"`
	Note           string   `json:"note,omitempty"`
	Domains        []string `json:"domains"`
	URLPatterns    []string `json:"url_patterns,omitempty"`
	Mirrors        []string `json:"mirrors,omitempty"`
	MirrorTemplate string   `json:"mirror_template,omitempty"`
	Steps          []Step   `json:"steps"`

	// Runtime metadata (not serialized).
	SourcePath string `json:"-"`
	External   bool   `json:"-"`
}

// Step is one atomic operation in a plugin's extraction pipeline.
type Step struct {
	Op         string            `json:"op"`
	From       string            `json:"from,omitempty"`
	Var        string            `json:"var,omitempty"`
	Vars       []string          `json:"vars,omitempty"`
	URL        string            `json:"url,omitempty"`
	Pattern    string            `json:"pattern,omitempty"`
	Replace    string            `json:"replace,omitempty"`
	Template   string            `json:"template,omitempty"`
	Key        string            `json:"key,omitempty"`
	Path       string            `json:"path,omitempty"`
	URLField   string            `json:"url_field,omitempty"`
	LabelField string            `json:"label_field,omitempty"`
	URLGroup   int               `json:"url_group,omitempty"`
	LabelGroup int               `json:"label_group,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Form       map[string]string `json:"form,omitempty"`
	Referer    string            `json:"referer,omitempty"`
	UserAgent  string            `json:"user_agent,omitempty"`
	Label      string            `json:"label,omitempty"`
	HLS        bool              `json:"hls,omitempty"`
	NoFollow   bool              `json:"no_follow,omitempty"`
	Optional   bool              `json:"optional,omitempty"`
	OnlyIf     string            `json:"only_if,omitempty"`
	SkipIf     string            `json:"skip_if,omitempty"`
}

var validOps = map[string]bool{
	"url_replace":    true,
	"http_get":       true,
	"http_post":      true,
	"regex":          true,
	"js_unpack":      true,
	"base64_decode":  true,
	"hex_decode":     true,
	"url_unescape":   true,
	"html_unescape":  true,
	"json_unescape":  true,
	"xor_decode":     true,
	"rc4_decrypt":    true,
	"set":            true,
	"json_extract":   true,
	"emit":           true,
	"emit_regex_all": true,
	"mirror_retry":   true,
}

// Validate checks manifest syntax, regex compilation, and step invariants.
func (m *Manifest) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("manifest 'id' alanı boş olamaz")
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("%s: 'name' alanı boş olamaz", m.ID)
	}
	if len(m.Domains) == 0 && len(m.URLPatterns) == 0 {
		return fmt.Errorf("%s: en az bir 'domains' veya 'url_patterns' tanımlanmalı", m.ID)
	}
	if len(m.Steps) == 0 {
		return fmt.Errorf("%s: en az bir işlem adımı ('steps') gerekli", m.ID)
	}
	for _, p := range m.URLPatterns {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("%s: geçersiz url_patterns regex %q: %w", m.ID, p, err)
		}
	}
	for i, st := range m.Steps {
		if !validOps[st.Op] {
			return fmt.Errorf("%s: adım #%d bilinmeyen işlem %q", m.ID, i+1, st.Op)
		}
		if st.Pattern != "" {
			if _, err := regexp.Compile(st.Pattern); err != nil {
				return fmt.Errorf("%s: adım #%d (%s) geçersiz regex %q: %w", m.ID, i+1, st.Op, st.Pattern, err)
			}
		}
		switch st.Op {
		case "regex", "url_replace", "emit_regex_all":
			if st.Pattern == "" {
				return fmt.Errorf("%s: adım #%d (%s) için 'pattern' zorunlu", m.ID, i+1, st.Op)
			}
		case "http_get", "http_post":
			if st.URL == "" {
				return fmt.Errorf("%s: adım #%d (%s) için 'url' zorunlu", m.ID, i+1, st.Op)
			}
		}
	}
	return nil
}

// Dir returns the user's zero-compile plugin directory (<ANICLITR_HOME>/plugins).
func Dir() string {
	return filepath.Join(config.Dir(), "plugins")
}

var (
	packMu     sync.RWMutex
	loaded     []*Pack
	loadedByID = map[string]*Pack{}
)

// Pack is a compiled, ready-to-run Manifest implementing extract.Extractor.
type Pack struct {
	Manifest Manifest
	urlRes   []*regexp.Regexp
	stepRes  []*regexp.Regexp
}

// Compile validates m and pre-compiles all regular expressions.
func Compile(m Manifest) (*Pack, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	p := &Pack{
		Manifest: m,
		urlRes:   make([]*regexp.Regexp, 0, len(m.URLPatterns)),
		stepRes:  make([]*regexp.Regexp, len(m.Steps)),
	}
	for _, pat := range m.URLPatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, err
		}
		p.urlRes = append(p.urlRes, re)
	}
	for i, st := range m.Steps {
		if st.Pattern != "" {
			re, err := regexp.Compile(st.Pattern)
			if err != nil {
				return nil, err
			}
			p.stepRes[i] = re
		}
	}
	return p, nil
}

// LoadEmbedded reads and compiles all embedded built-in resolver packs.
func LoadEmbedded() ([]*Pack, error) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil, err
	}
	var out []*Pack
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := builtinFS.ReadFile("builtin/" + ent.Name())
		if err != nil {
			return nil, fmt.Errorf("builtin/%s: %w", ent.Name(), err)
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("builtin/%s JSON hatası: %w", ent.Name(), err)
		}
		m.SourcePath = "builtin/" + ent.Name()
		m.External = false
		p, err := Compile(m)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out, nil
}

// LoadDir reads and compiles every *.json manifest in dir. Missing directory
// is not an error. Invalid individual manifests are collected in errs without
// aborting the rest.
func LoadDir(dir string) ([]*Pack, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	var out []*Pack
	var errs []error
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".json") {
			continue
		}
		full := filepath.Join(dir, ent.Name())
		raw, err := os.ReadFile(full)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ent.Name(), err))
			continue
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			errs = append(errs, fmt.Errorf("%s JSON hatası: %w", ent.Name(), err))
			continue
		}
		m.SourcePath = full
		m.External = true
		p, err := Compile(m)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ent.Name(), err))
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out, errs
}

// LoadAll loads built-in packs and user overrides from Dir(), registering every
// pack with extract.Register so zero-compile plugins take effect immediately.
func LoadAll() ([]*Pack, []error) {
	return LoadAllFrom(Dir())
}

// LoadAllFrom loads built-in packs plus external packs from extraDir.
func LoadAllFrom(extraDir string) ([]*Pack, []error) {
	built, err := LoadEmbedded()
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	ext, dirErrs := LoadDir(extraDir)
	errs = append(errs, dirErrs...)

	byID := make(map[string]*Pack, len(built)+len(ext))
	var order []string
	for _, p := range built {
		byID[p.Manifest.ID] = p
		order = append(order, p.Manifest.ID)
	}
	for _, p := range ext {
		if _, exists := byID[p.Manifest.ID]; !exists {
			order = append(order, p.Manifest.ID)
		}
		byID[p.Manifest.ID] = p
	}
	sort.Strings(order)
	merged := make([]*Pack, 0, len(order))
	for _, id := range order {
		p := byID[id]
		merged = append(merged, p)
		extract.Register(p)
	}
	packMu.Lock()
	loaded = merged
	loadedByID = byID
	packMu.Unlock()
	return merged, errs
}

// All returns all currently loaded packs.
func All() []*Pack {
	packMu.RLock()
	defer packMu.RUnlock()
	out := make([]*Pack, len(loaded))
	copy(out, loaded)
	return out
}

// ByID returns a loaded pack by its ID or Name (case-insensitive).
func ByID(id string) *Pack {
	packMu.RLock()
	defer packMu.RUnlock()
	if p, ok := loadedByID[strings.ToLower(strings.TrimSpace(id))]; ok {
		return p
	}
	for _, p := range loaded {
		if strings.EqualFold(p.Manifest.ID, id) || strings.EqualFold(p.Manifest.Name, id) {
			return p
		}
	}
	return nil
}

// Name implements extract.Extractor.
func (p *Pack) Name() string { return p.Manifest.Name }

// CanHandle implements extract.Extractor.
func (p *Pack) CanHandle(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	raw := u.String()
	for _, d := range p.Manifest.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if strings.HasSuffix(d, ".") {
			// Prefix match like "mixdrop." or "uqload."
			if strings.HasPrefix(host, d) || strings.Contains(host, "."+d) {
				return true
			}
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	for _, re := range p.urlRes {
		if re.MatchString(raw) {
			return true
		}
	}
	return false
}
