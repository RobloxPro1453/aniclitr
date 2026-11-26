// Package localmedia implements a zero-server hybrid local + online media
// library scanner. It inspects configured local or external drive folders for
// .mkv/.mp4 anime episodes, matches them to series/seasons/episodes using
// pure-Go release parsing and title folding, detects sidecar .ass/.srt
// subtitles, and caches the index on disk for instant startup lookups.
package localmedia

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/download"
	"aniclitr/internal/match"
)

var videoExts = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".webm": true,
	".m4v":  true,
	".avi":  true,
	".mov":  true,
}

var subExts = []string{".tr.ass", ".tr.srt", ".ass", ".srt", ".vtt"}

var (
	reDirSeason = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:season|sezon|s)\s*0*(\d{1,2})(?:$|[\s._-])`)
	reFileEpNum = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:e|ep|bolum|bölüm|episode)?\s*0*(\d{1,4})(?:v\d)?(?:$|[\s._\-\[\(])`)
)

// EpisodeFile describes one playable anime video file discovered on disk.
type EpisodeFile struct {
	Path        string    `json:"path"`
	SeriesTitle string    `json:"series_title"`
	FoldedTitle string    `json:"folded_title"`
	Season      int       `json:"season"`
	Episode     int       `json:"episode"`
	Resolution  string    `json:"resolution,omitempty"`
	Group       string    `json:"group,omitempty"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	SubPath     string    `json:"sub_path,omitempty"`
}

// SeriesSummary groups local episodes belonging to one series.
type SeriesSummary struct {
	Title       string
	FoldedTitle string
	Episodes    []EpisodeFile
	TotalBytes  int64
}

// Index holds all scanned local media files with fast lookup by title/season/ep.
type Index struct {
	mu      sync.RWMutex
	dirs    []string
	files   []EpisodeFile
	scanned time.Time
	cache   string
}

// Open creates an Index for the given directories, loading any cached scan
// from CacheDir/local-media.json immediately for zero-latency startup.
func Open(dirs []string) *Index {
	clean := dedupeDirs(dirs)
	idx := &Index{
		dirs:  clean,
		cache: filepath.Join(config.CacheDir(), "local-media.json"),
	}
	idx.loadCache()
	return idx
}

func dedupeDirs(dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			abs = d
		}
		k := strings.ToLower(abs)
		if !seen[k] {
			seen[k] = true
			out = append(out, abs)
		}
	}
	return out
}

func (idx *Index) loadCache() {
	b, err := os.ReadFile(idx.cache)
	if err != nil {
		return
	}
	var saved struct {
		Scanned time.Time     `json:"scanned"`
		Files   []EpisodeFile `json:"files"`
	}
	if json.Unmarshal(b, &saved) == nil {
		idx.mu.Lock()
		idx.files = saved.Files
		idx.scanned = saved.Scanned
		idx.mu.Unlock()
	}
}

func (idx *Index) saveCache() {
	idx.mu.RLock()
	payload := struct {
		Scanned time.Time     `json:"scanned"`
		Files   []EpisodeFile `json:"files"`
	}{
		Scanned: idx.scanned,
		Files:   idx.files,
	}
	p := idx.cache
	idx.mu.RUnlock()
	if b, err := json.Marshal(payload); err == nil {
		_ = config.WriteAtomic(p, b)
	}
}

// Scan walks all configured local directories (no server, no background daemon)
// and updates the index atomically.
func (idx *Index) Scan() int {
	idx.mu.RLock()
	dirs := append([]string(nil), idx.dirs...)
	idx.mu.RUnlock()

	var found []EpisodeFile
	seenPath := map[string]bool{}
	for _, root := range dirs {
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				base := strings.ToLower(d.Name())
				if strings.HasPrefix(base, ".") || base == "$recycle.bin" || base == "system volume information" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !videoExts[ext] {
				return nil
			}
			lp := strings.ToLower(p)
			if seenPath[lp] {
				return nil
			}
			seenPath[lp] = true
			info, err := d.Info()
			if err != nil || info.Size() == 0 {
				return nil
			}
			ef := classifyFile(root, p, info.Size(), info.ModTime())
			if ef.FoldedTitle != "" {
				found = append(found, ef)
			}
			return nil
		})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].FoldedTitle != found[j].FoldedTitle {
			return found[i].FoldedTitle < found[j].FoldedTitle
		}
		if found[i].Season != found[j].Season {
			return found[i].Season < found[j].Season
		}
		return found[i].Episode < found[j].Episode
	})

	idx.mu.Lock()
	idx.files = found
	idx.scanned = time.Now().UTC()
	idx.mu.Unlock()
	idx.saveCache()
	return len(found)
}

func classifyFile(root, fullPath string, size int64, mod time.Time) EpisodeFile {
	base := filepath.Base(fullPath)
	rel := download.ParseRelease(base)

	series := strings.TrimSpace(rel.Series)
	season := rel.Season
	ep := rel.Episode

	// Inspect parent directories relative to root for series & season names
	relPath, err := filepath.Rel(root, fullPath)
	if err == nil {
		parts := strings.Split(filepath.ToSlash(filepath.Dir(relPath)), "/")
		for i := len(parts) - 1; i >= 0; i-- {
			seg := strings.TrimSpace(parts[i])
			if seg == "" || seg == "." {
				continue
			}
			if m := reDirSeason.FindStringSubmatch(seg); len(m) > 1 {
				if sn, err := strconv.Atoi(m[1]); err == nil && sn > 0 && season <= 1 {
					season = sn
				}
				continue
			}
			if series == "" || isNumericOnly(series) {
				series = seg
			}
		}
	}
	if season <= 0 {
		season = 1
	}
	if ep <= 0 {
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if m := reFileEpNum.FindStringSubmatch(stem); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 2000 {
				ep = n
			}
		}
	}

	return EpisodeFile{
		Path:        fullPath,
		SeriesTitle: series,
		FoldedTitle: match.Fold(series),
		Season:      season,
		Episode:     ep,
		Resolution:  rel.Resolution,
		Group:       rel.Group,
		Size:        size,
		ModTime:     mod,
		SubPath:     findSidecarSub(fullPath),
	}
}

func findSidecarSub(videoPath string) string {
	stem := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	for _, ext := range subExts {
		cand := stem + ext
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func isNumericOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != ' ' && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

// Lookup finds a local video file matching any of titles for (season, ep).
// Verifies the file still exists on disk before returning true.
func (idx *Index) Lookup(titles []string, season, ep int) (EpisodeFile, bool) {
	if idx == nil || ep <= 0 {
		return EpisodeFile{}, false
	}
	if season <= 0 {
		season = 1
	}
	folded := make([]string, 0, len(titles))
	for _, t := range titles {
		if f := match.Fold(t); f != "" {
			folded = append(folded, f)
		}
	}
	if len(folded) == 0 {
		return EpisodeFile{}, false
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var best EpisodeFile
	bestScore := 0.0
	for _, ef := range idx.files {
		if ef.Season != season || ef.Episode != ep {
			continue
		}
		sc := 0.0
		for _, want := range folded {
			if ef.FoldedTitle == want {
				sc = 1.0
				break
			}
			if d := match.Ratio(ef.FoldedTitle, want); d > sc {
				sc = d
			}
		}
		if sc >= 0.82 && sc > bestScore {
			if _, err := os.Stat(ef.Path); err == nil {
				bestScore = sc
				best = ef
			}
		}
	}
	return best, bestScore >= 0.82
}

// MissingEpisodes returns episode numbers in 1..totalEps not yet on disk.
func (idx *Index) MissingEpisodes(titles []string, season, totalEps int) []int {
	if totalEps <= 0 {
		return nil
	}
	var missing []int
	for ep := 1; ep <= totalEps; ep++ {
		if _, ok := idx.Lookup(titles, season, ep); !ok {
			missing = append(missing, ep)
		}
	}
	return missing
}

// Series returns all local series grouped and sorted alphabetically.
func (idx *Index) Series() []SeriesSummary {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	byKey := map[string]*SeriesSummary{}
	for _, ef := range idx.files {
		s, ok := byKey[ef.FoldedTitle]
		if !ok {
			s = &SeriesSummary{Title: ef.SeriesTitle, FoldedTitle: ef.FoldedTitle}
			byKey[ef.FoldedTitle] = s
		}
		s.Episodes = append(s.Episodes, ef)
		s.TotalBytes += ef.Size
	}
	out := make([]SeriesSummary, 0, len(byKey))
	for _, s := range byKey {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// Count returns total indexed local video files.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.files)
}
