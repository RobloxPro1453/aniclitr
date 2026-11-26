// Package store persists watch history and per-anime preferences.
package store

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/logx"
	"aniclitr/internal/source"
)

// Bookmark is a saved timestamped scene marker inside an episode.
type Bookmark struct {
	Season    int       `json:"season"`
	Episode   int       `json:"episode"`
	Pos       float64   `json:"pos"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Entry is the history of one anime.
type Entry struct {
	Source    string            `json:"source"`
	AnimeID   string            `json:"anime_id"`
	Title     string            `json:"title"`
	Poster    string            `json:"poster,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	MalID     int               `json:"mal_id,omitempty"`
	Season    int               `json:"season"`
	Episode   int               `json:"episode"`
	EpTitle   string            `json:"ep_title,omitempty"`
	Position  float64           `json:"position"`
	Duration  float64           `json:"duration"`
	Fansub    string            `json:"fansub,omitempty"`
	Host      string            `json:"host,omitempty"`
	Quality   string            `json:"quality,omitempty"`
	Watched   map[string]bool   `json:"watched,omitempty"`   // "s:e" -> done
	Progress  map[string]int    `json:"progress,omitempty"`  // "s:e" -> percent for partial eps
	Notes     map[string]string `json:"notes,omitempty"`     // "s:e" -> your note on that episode
	EpScores  map[string]int    `json:"ep_scores,omitempty"` // "s:e" -> 1..10 episode rating
	Bookmarks []Bookmark        `json:"bookmarks,omitempty"` // scene bookmarks
	UpdatedAt time.Time         `json:"updated_at"`
}

// Anime rebuilds a minimal source.Anime from the entry.
func (e Entry) Anime() source.Anime {
	return source.Anime{Source: e.Source, ID: e.AnimeID, Title: e.Title, Poster: e.Poster, Kind: e.Kind, MalID: e.MalID}
}

// Percent of the last episode watched.
func (e Entry) Percent() int {
	if e.Duration <= 0 {
		return 0
	}
	return int(e.Position / e.Duration * 100)
}

// EpKey formats a season/episode key.
func EpKey(season, ep int) string { return fmt.Sprintf("%d:%d", season, ep) }

// History is a thread-safe JSON-backed store.
type History struct {
	mu      sync.Mutex
	path    string
	Entries map[string]*Entry `json:"entries"`
	// Activity: seconds watched per local day ("2006-01-02"), for stats.
	Activity map[string]float64 `json:"activity,omitempty"`
	rev      int                // bumped on every change (readers cache by it)
}

// Rev changes whenever entries do.
func (h *History) Rev() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rev
}

// Open loads history from the config dir.
func Open() *History {
	h := &History{path: filepath.Join(config.Dir(), "history.json"), Entries: map[string]*Entry{}}
	if b, err := os.ReadFile(h.path); err == nil {
		if err := json.Unmarshal(b, h); err != nil {
			// Never silently overwrite a damaged history: keep a copy aside.
			bak := h.path + ".bozuk-" + time.Now().Format("20060102-150405")
			_ = os.WriteFile(bak, b, 0o644)
			logx.Errorf("geçmiş okunamadı (%v); yedek: %s", err, bak)
			h.Entries = map[string]*Entry{}
		}
		if h.Entries == nil {
			h.Entries = map[string]*Entry{}
		}
	}
	if h.Activity == nil {
		h.Activity = map[string]float64{}
	}
	return h
}

// DayKey formats a local date for Activity.
func DayKey(t time.Time) string { return t.Format("2006-01-02") }

// AddActivity records watched seconds for today (saved with the next Update).
func (h *History) AddActivity(secs float64) {
	if secs <= 0 || secs > 3*3600 {
		return
	}
	h.mu.Lock()
	h.Activity[DayKey(time.Now())] += secs
	h.mu.Unlock()
}

// Day returns the seconds watched on day t.
func (h *History) Day(t time.Time) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.Activity[DayKey(t)]
}

// Streak counts consecutive days (ending today, or yesterday if today is
// still empty) with at least a few minutes watched.
func (h *History) Streak() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := time.Now()
	if h.Activity[DayKey(d)] < 180 {
		d = d.AddDate(0, 0, -1)
	}
	n := 0
	for h.Activity[DayKey(d)] >= 180 {
		n++
		d = d.AddDate(0, 0, -1)
	}
	return n
}

// Totals: episodes marked watched and seconds watched overall.
func (h *History) Totals() (eps int, secs float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.Entries {
		for _, w := range e.Watched {
			if w {
				eps++
			}
		}
	}
	for _, s := range h.Activity {
		secs += s
	}
	return
}

func cloneEntry(e *Entry) Entry {
	c := *e
	if e.Watched != nil {
		c.Watched = make(map[string]bool, len(e.Watched))
		for k, v := range e.Watched {
			c.Watched[k] = v
		}
	}
	if e.Progress != nil {
		c.Progress = make(map[string]int, len(e.Progress))
		for k, v := range e.Progress {
			c.Progress[k] = v
		}
	}
	if e.Notes != nil {
		c.Notes = make(map[string]string, len(e.Notes))
		for k, v := range e.Notes {
			c.Notes[k] = v
		}
	}
	if e.EpScores != nil {
		c.EpScores = make(map[string]int, len(e.EpScores))
		for k, v := range e.EpScores {
			c.EpScores[k] = v
		}
	}
	if len(e.Bookmarks) > 0 {
		c.Bookmarks = append([]Bookmark(nil), e.Bookmarks...)
	}
	return c
}

// Get returns a deep copy of the entry for key, if any.
func (h *History) Get(key string) (Entry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.Entries[key]
	if !ok {
		return Entry{}, false
	}
	return cloneEntry(e), true
}

// Recent lists deep-copied entries, newest first.
func (h *History) Recent() []Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Entry, 0, len(h.Entries))
	for _, e := range h.Entries {
		out = append(out, cloneEntry(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Update mutates (or creates) the entry for a and saves.
func (h *History) Update(a source.Anime, fn func(e *Entry)) {
	h.mu.Lock()
	h.rev++
	e, ok := h.Entries[a.Key()]
	if !ok {
		e = &Entry{Source: a.Source, AnimeID: a.ID}
		h.Entries[a.Key()] = e
	}
	if a.Title != "" {
		e.Title = a.Title
	}
	if a.Poster != "" {
		e.Poster = a.Poster
	}
	if a.Kind != "" {
		e.Kind = a.Kind
	}
	if a.MalID != 0 {
		e.MalID = a.MalID
	}
	if e.Watched == nil {
		e.Watched = map[string]bool{}
	}
	if e.Progress == nil {
		e.Progress = map[string]int{}
	}
	fn(e)
	e.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(h, "", " ")
	h.mu.Unlock()
	if err == nil {
		_ = config.WriteAtomic(h.path, b)
	}
}

// Remove deletes an entry.
func (h *History) Remove(key string) {
	h.mu.Lock()
	h.rev++
	delete(h.Entries, key)
	b, err := json.MarshalIndent(h, "", " ")
	h.mu.Unlock()
	if err == nil {
		_ = config.WriteAtomic(h.path, b)
	}
}

// Restore puts back an entry removed earlier (undo).
func (h *History) Restore(e Entry) {
	h.mu.Lock()
	h.rev++
	c := e
	h.Entries[e.Source+":"+e.AnimeID] = &c
	b, err := json.MarshalIndent(h, "", " ")
	h.mu.Unlock()
	if err == nil {
		_ = config.WriteAtomic(h.path, b)
	}
}

// SetEpisodeScore stores a 1..10 rating for (season, ep). Score <= 0 removes it.
func (h *History) SetEpisodeScore(a source.Anime, season, ep, score int) {
	if score > 10 {
		score = 10
	}
	h.Update(a, func(e *Entry) {
		if e.EpScores == nil {
			e.EpScores = map[string]int{}
		}
		k := EpKey(season, ep)
		if score <= 0 {
			delete(e.EpScores, k)
		} else {
			e.EpScores[k] = score
		}
	})
}

// EpisodeScoreStats returns the mean episode rating (e.g. 8.6), rated episode
// count, and nearest MAL/AniList integer score (1..10) for series-score suggestion.
func (h *History) EpisodeScoreStats(key string) (avg float64, count int, suggested int) {
	e, ok := h.Get(key)
	if !ok || len(e.EpScores) == 0 {
		return 0, 0, 0
	}
	sum := 0
	for _, sc := range e.EpScores {
		if sc >= 1 && sc <= 10 {
			sum += sc
			count++
		}
	}
	if count == 0 {
		return 0, 0, 0
	}
	avg = math.Round((float64(sum)/float64(count))*10) / 10
	suggested = int(math.Round(float64(sum) / float64(count)))
	if suggested < 1 {
		suggested = 1
	}
	if suggested > 10 {
		suggested = 10
	}
	return avg, count, suggested
}

// EpRatingPoint is one point on a series' episode rating wave chart.
type EpRatingPoint struct {
	Season  int
	Episode int
	Score   int
}

// EpisodeWave returns rated episodes of key sorted chronologically.
func (h *History) EpisodeWave(key string) []EpRatingPoint {
	e, ok := h.Get(key)
	if !ok || len(e.EpScores) == 0 {
		return nil
	}
	var out []EpRatingPoint
	for k, sc := range e.EpScores {
		if sc < 1 || sc > 10 {
			continue
		}
		parts := strings.SplitN(k, ":", 2)
		if len(parts) != 2 {
			continue
		}
		sn, err1 := strconv.Atoi(parts[0])
		ep, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, EpRatingPoint{Season: sn, Episode: ep, Score: sc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Episode < out[j].Episode
	})
	return out
}

// AddBookmark saves a timestamped scene bookmark atomically.
func (h *History) AddBookmark(a source.Anime, bm Bookmark) {
	if bm.CreatedAt.IsZero() {
		bm.CreatedAt = time.Now()
	}
	h.Update(a, func(e *Entry) {
		e.Bookmarks = append(e.Bookmarks, bm)
	})
}

// DeleteBookmark removes the bookmark at index idx for key.
func (h *History) DeleteBookmark(a source.Anime, idx int) {
	h.Update(a, func(e *Entry) {
		if idx >= 0 && idx < len(e.Bookmarks) {
			e.Bookmarks = append(e.Bookmarks[:idx], e.Bookmarks[idx+1:]...)
		}
	})
}

// DayCell is one day in the 365-day GitHub-style watching intensity heatmap.
type DayCell struct {
	Date  string  // "2006-01-02"
	Secs  float64
	Level int // 0 (none) .. 4 (marathon)
}

// Heatmap365 returns the last 365 days ending at now (oldest first).
func (h *History) Heatmap365(now time.Time) []DayCell {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]DayCell, 365)
	for i := 0; i < 365; i++ {
		d := now.AddDate(0, 0, -(364 - i))
		k := DayKey(d)
		s := h.Activity[k]
		lvl := 0
		switch {
		case s >= 7200: // 2h+
			lvl = 4
		case s >= 3600: // 1h+
			lvl = 3
		case s >= 1200: // 20m+ (1 ep)
			lvl = 2
		case s >= 180: // 3m+
			lvl = 1
		}
		out[i] = DayCell{Date: k, Secs: s, Level: lvl}
	}
	return out
}
