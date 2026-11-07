// Package config holds user settings and on-disk paths.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is persisted as JSON in the user's config dir.
type Config struct {
	Sources      map[string]bool `json:"sources"`       // provider id -> enabled
	Legacy       bool            `json:"legacy"`        // include archive sources
	Quality      string          `json:"quality"`       // "best", "1080p", "720p"...
	AutoSkip     bool            `json:"auto_skip"`     // aniskip OP/ED
	AutoNext     bool            `json:"auto_next"`     // continue to the next episode
	Fullscreen   bool            `json:"fullscreen"`    // start mpv fullscreen
	Posters      bool            `json:"posters"`       // draw cover art
	Motion       bool            `json:"motion"`        // animations (reduced-motion when false)
	Graphics     string          `json:"graphics"`      // "auto" | "sixel" | "blocks"
	Theme        string          `json:"theme"`         // palette name (see ui/themes.go)
	ThemeBg      bool            `json:"theme_bg"`      // paint the terminal background in the theme colour
	Ambient      string          `json:"ambient"`       // "auto" | "sakura" | "kar" | "yağmur" | "yıldız" | "ateşböceği" | "yok"
	Intro        string          `json:"intro"`         // logo intro: "düşüş" | "glitch" | "matrix" | "rastgele"
	Icons        string          `json:"icons"`         // "unicode" | "nerd"
	Compact      bool            `json:"compact"`       // tighter spacing
	Renderer     string          `json:"renderer"`      // "hücre" (default, cell diff) | "klasik" (Bubble Tea line renderer)
	SeenVersion  string          `json:"seen_version"`  // last version whose intro / what's-new was shown
	PathAsked    bool            `json:"path_asked"`    // the "open from any terminal" question was answered
	AutoUpdate   bool            `json:"auto_update"`   // the installed copy updates itself from GitHub releases
	MalClientID  string          `json:"mal_client_id"` // MyAnimeList API app (myanimelist.net/apiconfig)
	CoverTheme   bool            `json:"cover_theme"`   // tint the UI with the open anime's cover colours
	Sounds       int             `json:"sounds"`        // UI sound volume 0-100 (0 = off)
	OSKey        string          `json:"opensubtitles_api_key"`
	DownloadDir      string          `json:"download_dir"`        // offline copies (default ~/Videos/aniclitr)
	MpvPath          string          `json:"mpv_path"`            // optional override
	ExtraMpvArgs     []string        `json:"extra_mpv_args"`
	HostOrder        []string        `json:"host_order,omitempty"`          // preferred host priority
	NeverHosts       []string        `json:"never_hosts,omitempty"`         // hosts skipped by auto-select
	OnlyHosts        []string        `json:"only_hosts,omitempty"`          // if set, auto-select picks only these hosts (falls back to all when none works)
	// SeasonSync: per anime key ("source:id"), whether a title whose seasons
	// are separate MAL entries may write progress to those entries (true), or
	// only to its own (false). Missing: not asked yet.
	SeasonSync map[string]bool `json:"season_sync,omitempty"`
	// UI: "" (klasik, the default) or "yeni" (the new Nova interface).
	UI string `json:"ui,omitempty"`
	WaitAfterFirstMs int             `json:"wait_after_first_ms,omitempty"` // grace period after first resolved host (ms)
	AutoFallback     bool            `json:"auto_fallback"`                 // offer/switch to another source when all hosts fail
	SpeedGraph       bool            `json:"speed_graph"`                   // show live ASCII speed sparkline on downloads screen
}

func defaults() Config {
	return Config{
		Sources:      map[string]bool{}, // a source missing here is on
		Quality:      "best",
		AutoUpdate:   true,
		AutoSkip:     true,
		AutoNext:     true,
		AutoFallback: true,
		SpeedGraph:   true,
		Fullscreen:   true,
		Posters:    true,
		Motion:     true,
		Graphics:   "auto",
		Theme:      "gece",
		CoverTheme: true,
		Sounds:     35,
		ThemeBg:    true,
		Ambient:    "auto",
		Intro:      "rastgele",
		Icons:      "unicode",
	}
}

// Dir is the app data directory (e.g. %APPDATA%\aniclitr).
func Dir() string {
	if d := os.Getenv("ANICLITR_HOME"); d != "" { // isolated data dir (tests, portable use)
		_ = os.MkdirAll(d, 0o755)
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	d := filepath.Join(base, "aniclitr")
	if _, err := os.Stat(d); os.IsNotExist(err) {
		// the app used to be called "kaze": carry the user's data over once
		if old := filepath.Join(base, "kaze"); dirExists(old) {
			_ = os.Rename(old, d)
		}
	}
	_ = os.MkdirAll(d, 0o755)
	return d
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// CacheDir holds disposable data (indexes, posters).
func CacheDir() string {
	if d := os.Getenv("ANICLITR_HOME"); d != "" { // isolated runs keep their cache too
		d = filepath.Join(d, "cache")
		_ = os.MkdirAll(d, 0o755)
		return d
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	d := filepath.Join(base, "aniclitr")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the config, filling missing fields with defaults.
func Load() Config {
	c := defaults()
	b, err := os.ReadFile(path())
	if err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			// keep the user's broken file for inspection; start from defaults
			_ = os.WriteFile(path()+".bozuk", b, 0o644)
			c = defaults()
		}
	}
	if c.Sources == nil {
		c.Sources = map[string]bool{}
	}
	if c.Quality == "" {
		c.Quality = "best"
	}
	return c
}

// Save writes the config atomically.
func (c Config) Save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path(), b)
}

// Enabled reports whether a provider should be queried.
func (c Config) Enabled(id string, legacy bool) bool {
	if legacy && !c.Legacy {
		return false
	}
	on, ok := c.Sources[id]
	return !ok || on
}

// WriteAtomic writes via a temp file + rename so crashes never corrupt data.
func WriteAtomic(p string, b []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
