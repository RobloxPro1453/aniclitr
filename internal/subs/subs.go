// Package subs finds external subtitles on the internet.
//
// Providers implement Provider; today that is OpenSubtitles (needs a free
// API key: opensubtitles.com → profile → API consumers). The interface is
// meant for more (Jimaku for Japanese, release-matched subs for torrents).
package subs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/config"
)

// Result is one downloadable subtitle.
type Result struct {
	Provider  string
	ID        string // provider file id
	Lang      string // "tr", "en"…
	Release   string // release / file name
	Downloads int
	Season    int
	Episode   int
}

// Label is the display name.
func (r Result) Label() string {
	l := strings.ToUpper(r.Lang)
	if r.Release != "" {
		l += " · " + r.Release
	}
	return l
}

// Query describes what to look for.
type Query struct {
	Title   string
	Season  int
	Episode int
	Langs   []string // preference order
}

// Provider searches and downloads.
type Provider interface {
	Name() string
	Search(ctx context.Context, q Query) ([]Result, error)
	// Download saves the subtitle and returns the local file path.
	Download(ctx context.Context, r Result) (string, error)
}

// ErrNoKey: the provider needs an API key that is not configured.
var ErrNoKey = errors.New("OpenSubtitles API anahtarı ayarlı değil (config.json → opensubtitles_api_key)")

// Rank sorts results: preferred language first, then popularity.
func Rank(rs []Result, langs []string) {
	pri := map[string]int{}
	for i, l := range langs {
		pri[l] = len(langs) - i
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if pri[rs[i].Lang] != pri[rs[j].Lang] {
			return pri[rs[i].Lang] > pri[rs[j].Lang]
		}
		return rs[i].Downloads > rs[j].Downloads
	})
}

// ---- OpenSubtitles (REST v1) ----

// OpenSubtitles talks to api.opensubtitles.com.
type OpenSubtitles struct {
	Key  string
	Base string // tests
}

func (o *OpenSubtitles) Name() string { return "OpenSubtitles" }

func (o *OpenSubtitles) base() string {
	if o.Base != "" {
		return o.Base
	}
	return "https://api.opensubtitles.com/api/v1"
}

func (o *OpenSubtitles) req(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if strings.TrimSpace(o.Key) == "" {
		return nil, ErrNoKey
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.base()+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Api-Key", o.Key)
	req.Header.Set("User-Agent", "aniclitr v0.7")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		return nil, fmt.Errorf("OpenSubtitles: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

// Search finds subtitles for an episode.
func (o *OpenSubtitles) Search(ctx context.Context, q Query) ([]Result, error) {
	v := url.Values{}
	v.Set("query", q.Title)
	if q.Season > 0 {
		v.Set("season_number", strconv.Itoa(q.Season))
	}
	if q.Episode > 0 {
		v.Set("episode_number", strconv.Itoa(q.Episode))
	}
	if len(q.Langs) > 0 {
		v.Set("languages", strings.Join(q.Langs, ","))
	}
	v.Set("order_by", "download_count")
	resp, err := o.req(ctx, http.MethodGet, "/subtitles?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var res struct {
		Data []struct {
			Attributes struct {
				Language  string `json:"language"`
				Release   string `json:"release"`
				Downloads int    `json:"download_count"`
				Files     []struct {
					FileID   int    `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
				Feature struct {
					Season  int `json:"season_number"`
					Episode int `json:"episode_number"`
				} `json:"feature_details"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	var out []Result
	for _, d := range res.Data {
		at := d.Attributes
		if len(at.Files) == 0 {
			continue
		}
		// keep only the asked episode when the provider knows it
		if q.Episode > 0 && at.Feature.Episode > 0 && at.Feature.Episode != q.Episode {
			continue
		}
		rel := at.Release
		if rel == "" {
			rel = at.Files[0].FileName
		}
		out = append(out, Result{Provider: o.Name(), ID: strconv.Itoa(at.Files[0].FileID), Lang: at.Language, Release: rel,
			Downloads: at.Downloads, Season: at.Feature.Season, Episode: at.Feature.Episode})
	}
	Rank(out, q.Langs)
	return out, nil
}

// Download fetches the file into the cache folder.
func (o *OpenSubtitles) Download(ctx context.Context, r Result) (string, error) {
	id, _ := strconv.Atoi(r.ID)
	resp, err := o.req(ctx, http.MethodPost, "/download", map[string]any{"file_id": id})
	if err != nil {
		return "", err
	}
	var d struct {
		Link     string `json:"link"`
		FileName string `json:"file_name"`
		Message  string `json:"message"`
	}
	err = json.NewDecoder(resp.Body).Decode(&d)
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	if d.Link == "" {
		return "", fmt.Errorf("OpenSubtitles: indirme linki yok (%s)", d.Message)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.Link, nil)
	fr, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer fr.Body.Close()
	b, err := io.ReadAll(io.LimitReader(fr.Body, 8<<20))
	if err != nil {
		return "", err
	}
	name := d.FileName
	if name == "" {
		name = r.ID + ".srt"
	}
	dir := filepath.Join(config.CacheDir(), "altyazi")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, safe(r.Lang+"-"+r.ID+"-"+name))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

var reUnsafe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func safe(s string) string {
	s = reUnsafe.ReplaceAllString(s, "_")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
