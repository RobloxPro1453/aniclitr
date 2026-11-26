package subs

// AniSub (https://anisub.co) — Türkiye'nin topluluk tabanlı anime altyazı
// platformu. API anahtarı veya giriş gerektirmez:
//   - GET /api/search?q=<ad>          → JSON anime listesi
//   - GET /anime/<slug>               → Inertia JSON props.subtitles
//   - GET /download/<subtitle_id>     → doğrudan .zip / .ass / .srt / .rar dosyası

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/config"
)

// AniSub searches and downloads community Turkish anime subtitles from anisub.co.
type AniSub struct {
	Base string // optional override for tests
}

func (a *AniSub) Name() string { return "AniSub" }

func (a *AniSub) base() string {
	if a.Base != "" {
		return strings.TrimRight(a.Base, "/")
	}
	return "https://anisub.co"
}

func (a *AniSub) get(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) aniclitr")
	req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("AniSub: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", err
	}
	return b, resp.Header.Get("Content-Disposition"), nil
}

type anisubSearchRes struct {
	Anime []struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		Details string `json:"details"`
	} `json:"anime"`
}

type anisubEntry struct {
	ID            int    `json:"subtitle_id"`
	ReleaseName   string `json:"subtitle_release_name"`
	Language      string `json:"subtitle_language"`
	Uploader      string `json:"subtitle_uploader"`
	Translator    string `json:"subtitle_translator"`
	ContentType   string `json:"content_type"`
	Format        string `json:"format"`
	File          string `json:"subtitle_file"`
	DownloadCount int    `json:"download_count"`
	Visibility    string `json:"visibility"`
	EpisodeNumber *int   `json:"episode_number"`
	IsAI          bool   `json:"is_ai_translated"`
}

var (
	reInertiaScript = regexp.MustCompile(`(?s)<script[^>]*data-page[^>]*>(.*?)</script>`)
	reInertiaAttr   = regexp.MustCompile(`id="app"[^>]*data-page="([^"]+)"`)
	reContentEp     = regexp.MustCompile(`(?i)(?:bölüm|bolum|ep|episode)\s*[:.#-]?\s*(\d+)`)
	reFileEp        = regexp.MustCompile(`(?i)(?:s\d{1,2}e|ep?\.?\s*|bölüm\s*|-\s*|\b)(\d{1,4})(?:\b|[._\s\-v])`)
)

func parseInertiaSubtitles(pageHTML []byte) ([]anisubEntry, error) {
	var raw []byte
	if m := reInertiaScript.FindSubmatch(pageHTML); m != nil {
		raw = m[1]
	} else if m := reInertiaAttr.FindSubmatch(pageHTML); m != nil {
		raw = []byte(html.UnescapeString(string(m[1])))
	}
	if len(raw) == 0 {
		return nil, errors.New("AniSub: sayfa verisi okunamadı")
	}
	var pg struct {
		Props struct {
			Subtitles []anisubEntry `json:"subtitles"`
		} `json:"props"`
	}
	if err := json.Unmarshal(raw, &pg); err != nil {
		return nil, err
	}
	return pg.Props.Subtitles, nil
}

func normSubLang(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(l, "türk") || strings.Contains(l, "turk") || l == "tr":
		return "tr"
	case strings.Contains(l, "ingiliz") || strings.Contains(l, "eng") || l == "en":
		return "en"
	case strings.Contains(l, "japon") || l == "ja" || l == "jp":
		return "ja"
	}
	if len(l) == 2 {
		return l
	}
	return "tr"
}

func entryEpisode(e anisubEntry) (ep int, isPack bool) {
	ct := strings.ToLower(strings.TrimSpace(e.ContentType))
	if strings.Contains(ct, "paket") || strings.Contains(ct, "sezon") || strings.Contains(ct, "batch") || strings.Contains(ct, "tüm") {
		return 0, true
	}
	if e.EpisodeNumber != nil && *e.EpisodeNumber > 0 {
		return *e.EpisodeNumber, false
	}
	if m := reContentEp.FindStringSubmatch(e.ContentType); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n, false
		}
	}
	return 0, false
}

// Search queries anisub.co for community subtitles matching q.
func (a *AniSub) Search(ctx context.Context, q Query) ([]Result, error) {
	title := strings.TrimSpace(q.Title)
	if title == "" {
		return nil, nil
	}
	b, _, err := a.get(ctx, a.base()+"/api/search?q="+url.QueryEscape(title), 2<<20)
	if err != nil {
		return nil, err
	}
	var sr anisubSearchRes
	if err := json.Unmarshal(b, &sr); err != nil {
		return nil, err
	}
	if len(sr.Anime) == 0 {
		return nil, nil
	}
	// Pick up to 2 best matching anime slugs (prefer closest title match).
	qLow := strings.ToLower(title)
	sort.SliceStable(sr.Anime, func(i, j int) bool {
		ni, nj := strings.ToLower(sr.Anime[i].Name), strings.ToLower(sr.Anime[j].Name)
		si := subTitleScore(qLow, ni)
		sj := subTitleScore(qLow, nj)
		return si > sj
	})

	limit := min(2, len(sr.Anime))
	var out []Result
	seen := map[int]bool{}
	for idx := 0; idx < limit; idx++ {
		slug := strings.Trim(sr.Anime[idx].URL, "/")
		if slug == "" {
			continue
		}
		page, _, err := a.get(ctx, a.base()+"/anime/"+url.PathEscape(slug), 4<<20)
		if err != nil {
			continue
		}
		subs, err := parseInertiaSubtitles(page)
		if err != nil {
			continue
		}
		for _, e := range subs {
			if e.ID == 0 || seen[e.ID] {
				continue
			}
			if e.Visibility != "" && !strings.EqualFold(e.Visibility, "public") {
				continue
			}
			epNum, isPack := entryEpisode(e)
			if q.Episode > 0 && !isPack && epNum > 0 && epNum != q.Episode {
				continue
			}
			seen[e.ID] = true

			var labelParts []string
			if e.ReleaseName != "" {
				labelParts = append(labelParts, e.ReleaseName)
			}
			who := strings.TrimSpace(e.Translator)
			if who == "" {
				who = strings.TrimSpace(e.Uploader)
			}
			if who != "" && !strings.EqualFold(who, e.ReleaseName) {
				labelParts = append(labelParts, who)
			}
			if e.Format != "" {
				labelParts = append(labelParts, strings.ToLower(e.Format))
			}
			if isPack {
				labelParts = append(labelParts, "Paket")
			} else if epNum > 0 {
				labelParts = append(labelParts, fmt.Sprintf("B%d", epNum))
			}
			if e.IsAI {
				labelParts = append(labelParts, "AI")
			}
			rel := strings.Join(labelParts, " · ")
			if rel == "" {
				rel = fmt.Sprintf("AniSub #%d", e.ID)
			}
			out = append(out, Result{
				Provider:  a.Name(),
				ID:        strconv.Itoa(e.ID),
				Lang:      normSubLang(e.Language),
				Release:   rel,
				Downloads: e.DownloadCount,
				Season:    q.Season,
				Episode:   q.Episode, // kept so Download extracts the right ep from a pack
			})
		}
		if len(out) > 0 {
			break
		}
	}
	Rank(out, q.Langs)
	return out, nil
}

func subTitleScore(q, cand string) int {
	if cand == q {
		return 100
	}
	if strings.HasPrefix(cand, q) || strings.HasPrefix(q, cand) {
		return 80
	}
	if strings.Contains(cand, q) {
		return 60
	}
	score := 0
	for _, w := range strings.Fields(q) {
		if len(w) >= 3 && strings.Contains(cand, w) {
			score += 15
		}
	}
	return score
}

// Download fetches the subtitle (or archive) from anisub.co and returns a ready
// local .ass / .srt / .vtt path in the cache folder.
func (a *AniSub) Download(ctx context.Context, r Result) (string, error) {
	b, disp, err := a.get(ctx, a.base()+"/download/"+url.PathEscape(r.ID), 16<<20)
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", errors.New("AniSub: boş dosya döndü")
	}
	dir := filepath.Join(config.CacheDir(), "altyazi")
	_ = os.MkdirAll(dir, 0o755)

	// ZIP archive (native Go extraction, zero external tools)
	if len(b) >= 4 && b[0] == 'P' && b[1] == 'K' && b[2] == 3 && b[3] == 4 {
		return extractZipSubtitle(b, r, dir)
	}
	// RAR archive (try system tar / 7z / unrar)
	if len(b) >= 4 && string(b[:4]) == "Rar!" {
		return extractRarSubtitle(ctx, b, r, dir)
	}

	// Plain subtitle file (.ass / .srt / .vtt)
	ext := detectSubExt(b, disp)
	path := filepath.Join(dir, safe(fmt.Sprintf("anisub-%s-e%d%s", r.ID, r.Episode, ext)))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func isSubExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ass", ".ssa", ".srt", ".vtt":
		return true
	}
	return false
}

func detectSubExt(b []byte, disp string) string {
	low := strings.ToLower(disp)
	for _, ext := range []string{".ass", ".ssa", ".srt", ".vtt"} {
		if strings.Contains(low, ext) {
			return ext
		}
	}
	head := string(b[:min(512, len(b))])
	if strings.Contains(head, "[Script Info]") {
		return ".ass"
	}
	if strings.Contains(head, "WEBVTT") {
		return ".vtt"
	}
	return ".srt"
}

func pickSubtitleFile(names []string, wantEp int) int {
	bestIdx, bestScore := -1, -1
	for i, name := range names {
		if !isSubExt(name) {
			continue
		}
		base := filepath.Base(name)
		sc := 10
		if strings.EqualFold(filepath.Ext(base), ".ass") {
			sc += 2 // prefer styled .ass over .srt when equal
		}
		if wantEp > 0 && filenameMatchesEp(base, wantEp) {
			sc += 100
		}
		if sc > bestScore {
			bestScore, bestIdx = sc, i
		}
	}
	return bestIdx
}

func filenameMatchesEp(name string, ep int) bool {
	// Strip resolutions (1080p, 720p, 480p, 2160p, x264, x265) so they don't spoof episode numbers.
	cleaned := strings.NewReplacer("2160p", "", "1080p", "", "720p", "", "480p", "", "360p", "", "x264", "", "x265", "", "h264", "", "h265", "").Replace(strings.ToLower(name))
	for _, m := range reFileEp.FindAllStringSubmatch(cleaned, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n == ep {
			return true
		}
	}
	return false
}

func extractZipSubtitle(data []byte, r Result, dir string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("AniSub ZIP okunamadı: %w", err)
	}
	names := make([]string, len(zr.File))
	for i, f := range zr.File {
		if !f.FileInfo().IsDir() {
			names[i] = f.Name
		}
	}
	idx := pickSubtitleFile(names, r.Episode)
	if idx < 0 {
		return "", errors.New("AniSub ZIP içinde .ass/.srt altyazı bulunamadı")
	}
	f := zr.File[idx]
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, 8<<20))
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(f.Name))
	path := filepath.Join(dir, safe(fmt.Sprintf("anisub-%s-e%d-%s", r.ID, r.Episode, filepath.Base(f.Name))))
	if !strings.HasSuffix(strings.ToLower(path), ext) {
		path += ext
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func extractRarSubtitle(ctx context.Context, data []byte, r Result, dir string) (string, error) {
	tmp, err := os.MkdirTemp(dir, "rar-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	rarPath := filepath.Join(tmp, "sub.rar")
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		return "", err
	}
	outDir := filepath.Join(tmp, "out")
	_ = os.MkdirAll(outDir, 0o755)

	extracted := false
	for _, tool := range [][]string{
		{"tar", "-xf", rarPath, "-C", outDir},
		{"7z", "e", "-y", "-o" + outDir, rarPath},
		{"unrar", "e", "-o+", rarPath, outDir},
	} {
		if _, err := exec.LookPath(tool[0]); err != nil {
			continue
		}
		if err := exec.CommandContext(ctx, tool[0], tool[1:]...).Run(); err == nil {
			extracted = true
			break
		}
	}
	if !extracted {
		return "", errors.New("bu altyazı RAR arşivinde — ZIP/.ass sürümünü seçin ya da 7-Zip kurun")
	}
	var found []string
	_ = filepath.WalkDir(outDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && isSubExt(p) {
			found = append(found, p)
		}
		return nil
	})
	idx := pickSubtitleFile(found, r.Episode)
	if idx < 0 {
		return "", errors.New("RAR arşivi içinde .ass/.srt altyazı bulunamadı")
	}
	content, err := os.ReadFile(found[idx])
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, safe(fmt.Sprintf("anisub-%s-e%d-%s", r.ID, r.Episode, filepath.Base(found[idx]))))
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Multi queries multiple subtitle providers in parallel and routes Download to
// the provider that produced each result.
type Multi struct {
	Providers []Provider
}

func (m *Multi) Name() string {
	var names []string
	for _, p := range m.Providers {
		names = append(names, p.Name())
	}
	return strings.Join(names, " + ")
}

func (m *Multi) Search(ctx context.Context, q Query) ([]Result, error) {
	type res struct {
		rs  []Result
		err error
	}
	ch := make(chan res, len(m.Providers))
	for _, p := range m.Providers {
		p := p
		go func() {
			rs, err := p.Search(ctx, q)
			ch <- res{rs, err}
		}()
	}
	var all []Result
	var lastErr error
	for range m.Providers {
		r := <-ch
		if r.err != nil {
			lastErr = r.err
			continue
		}
		all = append(all, r.rs...)
	}
	if len(all) == 0 && lastErr != nil {
		return nil, lastErr
	}
	Rank(all, q.Langs)
	return all, nil
}

func (m *Multi) Download(ctx context.Context, r Result) (string, error) {
	for _, p := range m.Providers {
		if strings.EqualFold(p.Name(), r.Provider) {
			return p.Download(ctx, r)
		}
	}
	if len(m.Providers) > 0 {
		return m.Providers[0].Download(ctx, r)
	}
	return "", errors.New("altyazı sağlayıcısı yok")
}
