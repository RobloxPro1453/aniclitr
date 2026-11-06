package match

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
)

// Entry is one persisted provider-to-tracker mapping in mappings.json.
type Entry struct {
	MalID     int       `json:"mal"`
	AniListID int       `json:"anilist,omitempty"`
	Title     string    `json:"title,omitempty"`
	Score     float64   `json:"score"`
	By        string    `json:"by"` // "auto" or "user"
	At        time.Time `json:"at"`
}

// Store manages persistent provider key ("source:id") -> tracker ID mappings.
type Store struct {
	mu   sync.Mutex
	path string
	m    map[string]Entry
}

var (
	defaultMu    sync.Mutex
	defaultStore *Store
	defaultDir   string
)

// Default returns the process-wide mapping store rooted at config.Dir().
// If config.Dir() changed (e.g. in tests setting ANICLITR_HOME), it reloads.
func Default() *Store {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	dir := config.Dir()
	if defaultStore == nil || defaultDir != dir {
		defaultDir = dir
		defaultStore = LoadFrom(filepath.Join(dir, "mappings.json"))
	}
	return defaultStore
}

// LoadFrom loads a mapping store from the given file path.
func LoadFrom(path string) *Store {
	s := &Store{path: path, m: map[string]Entry{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.m)
		if s.m == nil {
			s.m = map[string]Entry{}
		}
	}
	return s
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	b, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return
	}
	_ = config.WriteAtomic(s.path, b)
}

// Get looks up the mapping for a provider key ("source:id").
func (s *Store) Get(key string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	return e, ok
}

// SetAuto records an automatic mapping if no user override exists for key.
func (s *Store) SetAuto(key string, malID, anilistID int, title string, score float64) {
	if key == "" || (malID <= 0 && anilistID <= 0) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.m[key]; ok && cur.By == "user" {
		return // never overwrite manual user remap
	}
	if score > 1.0 {
		score = 1.0
	}
	s.m[key] = Entry{
		MalID:     malID,
		AniListID: anilistID,
		Title:     title,
		Score:     score,
		By:        "auto",
		At:        time.Now().UTC(),
	}
	s.saveLocked()
}

// SetUser records a manual user-confirmed mapping that auto-matching will never overwrite.
func (s *Store) SetUser(key string, malID, anilistID int, title string) {
	if key == "" || (malID <= 0 && anilistID <= 0) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = Entry{
		MalID:     malID,
		AniListID: anilistID,
		Title:     title,
		Score:     1.0,
		By:        "user",
		At:        time.Now().UTC(),
	}
	s.saveLocked()
}

// Delete removes a mapping entry.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		delete(s.m, key)
		s.saveLocked()
	}
}

// ByMal returns all provider keys ("source:id") mapped to the given MAL ID,
// keyed by source ID (e.g. "animecix" -> "7822"). User mappings take precedence
// over auto mappings, and higher scores win within the same source.
func (s *Store) ByMal(malID int) map[string]EntryWithID {
	if malID <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]EntryWithID{}
	for key, e := range s.m {
		if e.MalID != malID || (e.By != "user" && e.Score < MinScore) {
			continue
		}
		src, id, ok := strings.Cut(key, ":")
		if !ok || src == "" || id == "" {
			continue
		}
		cur, exists := out[src]
		if !exists || (e.By == "user" && cur.Entry.By != "user") || (e.By == cur.Entry.By && e.Score > cur.Entry.Score) {
			out[src] = EntryWithID{Source: src, AnimeID: id, Entry: e}
		}
	}
	return out
}

// EntryWithID pairs a provider's source/anime ID with its mapping Entry.
type EntryWithID struct {
	Source  string
	AnimeID string
	Entry   Entry
}
