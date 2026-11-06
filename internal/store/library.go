package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/logx"
	"aniclitr/internal/source"
)

// Library statuses. The set mirrors MyAnimeList's so a later sync is a
// straight mapping (see MalStatus).
const (
	Watching  = "izliyor"
	Completed = "tamamlandı"
	Planned   = "planlanan"
	OnHold    = "beklemede"
	Dropped   = "bırakıldı"
)

// Statuses in display order.
var Statuses = []string{Watching, Completed, Planned, OnHold, Dropped}

// MalStatus maps a library status to MyAnimeList's API value.
func MalStatus(s string) string {
	return map[string]string{Watching: "watching", Completed: "completed", Planned: "plan_to_watch", OnHold: "on_hold", Dropped: "dropped"}[s]
}

// Item is one anime on the user's shelf.
type Item struct {
	Source     string    `json:"source"`
	AnimeID    string    `json:"anime_id"`
	Title      string    `json:"title"`
	Poster     string    `json:"poster,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	Year       int       `json:"year,omitempty"`
	Genres     []string  `json:"genres,omitempty"`
	MalID      int       `json:"mal_id,omitempty"`
	EpCount    int       `json:"ep_count,omitempty"`
	Status     string    `json:"status"`
	Score      int       `json:"score,omitempty"` // 0 = unrated, 1..10
	Fav        bool      `json:"fav,omitempty"`
	Lists      []string  `json:"lists,omitempty"`
	AddedAt    time.Time `json:"added_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`

	// MyAnimeList sync
	Watched  int       `json:"watched,omitempty"`   // episodes watched (MAL num_watched_episodes)
	MalDirty bool      `json:"mal_dirty,omitempty"` // changed locally, not yet sent
	MalSeen  time.Time `json:"mal_seen,omitempty"`  // MAL's updated_at last applied / sent
	NoMal    bool      `json:"no_mal,omitempty"`    // no MAL match found (don't retry forever)
}

// FromMal reports items that exist only because they are on the MAL list
// (not opened on a source yet).
func (it Item) FromMal() bool { return it.Source == "mal" }

// syncSig is what MAL stores; a change marks the item dirty.
func (it Item) syncSig() string {
	return fmt.Sprintf("%s|%d|%d|%s", it.Status, it.Score, it.Watched, strings.Join(it.Lists, ","))
}

// Anime rebuilds a minimal source.Anime.
func (it Item) Anime() source.Anime {
	return source.Anime{Source: it.Source, ID: it.AnimeID, Title: it.Title, Poster: it.Poster, Kind: it.Kind, Year: it.Year, Genres: it.Genres, MalID: it.MalID}
}

// Key is the anime key (source:id).
func (it Item) Key() string { return it.Source + ":" + it.AnimeID }

// InList reports membership in a custom list.
func (it Item) InList(name string) bool {
	for _, l := range it.Lists {
		if strings.EqualFold(l, name) {
			return true
		}
	}
	return false
}

// Library is the JSON-backed shelf (%APPDATA%\aniclitr\library.json).
type Library struct {
	mu    sync.Mutex
	path  string
	Items map[string]*Item `json:"items"`
	Lists []string         `json:"lists"` // custom watchlists, user order
	// MalDeleted: MAL ids removed locally, to be deleted on MAL.
	MalDeleted []int `json:"mal_deleted,omitempty"`
	// Changed is signalled (coalesced) whenever MAL-relevant data changes.
	Changed chan struct{} `json:"-"`
	rev     int           // bumped on every change (readers cache by it)
}

// Rev changes whenever the library does.
func (l *Library) Rev() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rev
}

func (l *Library) signal() {
	select {
	case l.Changed <- struct{}{}:
	default:
	}
}

// OpenLibrary loads the library (a damaged file is set aside, never overwritten).
func OpenLibrary() *Library {
	l := &Library{path: filepath.Join(config.Dir(), "library.json"), Items: map[string]*Item{}, Changed: make(chan struct{}, 1)}
	if b, err := os.ReadFile(l.path); err == nil {
		if err := json.Unmarshal(b, l); err != nil {
			bak := l.path + ".bozuk-" + time.Now().Format("20060102-150405")
			_ = os.WriteFile(bak, b, 0o644)
			logx.Errorf("kitaplık okunamadı (%v); yedek: %s", err, bak)
			l.Items = map[string]*Item{}
			l.Lists = nil
		}
	}
	if l.Items == nil {
		l.Items = map[string]*Item{}
	}
	if l.Lists == nil {
		l.Lists = []string{"favoriler"}
	}
	reconciled := false
	for _, it := range l.Items {
		if it != nil && it.Reconcile() {
			reconciled = true
		}
	}
	if reconciled {
		l.saveLocked()
	}
	return l
}

// Reconcile ensures Status, Watched, and EpCount never contradict each other
// (e.g. Status == Completed while Watched == 15 and EpCount == 16).
func (it *Item) Reconcile() bool {
	changed := false
	if it.Status == Completed {
		if it.FinishedAt.IsZero() {
			it.FinishedAt = time.Now()
			changed = true
		}
		if it.EpCount > 0 && it.Watched < it.EpCount {
			it.Watched = it.EpCount
			changed = true
		} else if it.EpCount == 0 && it.Watched > 0 {
			it.EpCount = it.Watched
			changed = true
		}
	}
	if it.EpCount > 0 && it.Watched > it.EpCount {
		it.EpCount = it.Watched
		changed = true
	}
	return changed
}

func (l *Library) saveLocked() {
	l.rev++
	b, err := json.MarshalIndent(l, "", " ")
	if err == nil {
		_ = config.WriteAtomic(l.path, b)
	}
}

// Get returns a copy of the item.
func (l *Library) Get(key string) (Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	it, ok := l.Items[key]
	if !ok {
		return Item{}, false
	}
	c := *it
	c.Lists = append([]string(nil), it.Lists...)
	return c, true
}

// All returns every item, most recently updated first.
func (l *Library) All() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Item, 0, len(l.Items))
	for _, it := range l.Items {
		c := *it
		c.Lists = append([]string(nil), it.Lists...)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Set creates or edits the item for a (metadata from a refreshes the copy).
func (l *Library) Set(a source.Anime, fn func(it *Item)) Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	it, ok := l.Items[a.Key()]
	if !ok && a.MalID != 0 {
		// the same anime may already be here under another key (e.g. it
		// came from the MAL list as "mal:<id>" and is now opened on a
		// source): adopt that item instead of creating a duplicate
		for k, x := range l.Items {
			// sites sometimes give a movie its series' MAL id: never let a
			// film take over the series' item (or the other way round)
			if x.MalID == a.MalID && (x.Kind == "" || a.Kind == "" || isFilm(x.Kind) == isFilm(a.Kind)) {
				delete(l.Items, k)
				x.Source, x.AnimeID = a.Source, a.ID
				l.Items[a.Key()] = x
				it, ok = x, true
				break
			}
		}
	}
	if !ok {
		it = &Item{Source: a.Source, AnimeID: a.ID, Status: Planned, AddedAt: time.Now()}
		l.Items[a.Key()] = it
	}
	if a.Title != "" {
		it.Title = a.Title
	}
	if a.Poster != "" {
		it.Poster = a.Poster
	}
	if a.Kind != "" {
		it.Kind = a.Kind
	}
	if a.Year != 0 {
		it.Year = a.Year
	}
	if len(a.Genres) > 0 {
		it.Genres = a.Genres
	}
	if a.MalID != 0 {
		it.MalID = a.MalID
	}
	if a.EpCount != 0 {
		it.EpCount = a.EpCount
	}
	prev := it.Status
	sig := it.syncSig()
	if !ok {
		sig = "" // a new item always needs to reach MAL
	}
	if fn != nil {
		fn(it)
	}
	if it.Status == Completed && prev != Completed && it.FinishedAt.IsZero() {
		it.FinishedAt = time.Now()
	}
	it.Reconcile()
	if it.syncSig() != sig {
		it.MalDirty = true
		defer l.signal()
	}
	it.UpdatedAt = time.Now()
	l.saveLocked()
	return *it
}

// FindMal returns the item for a MAL id.
func (l *Library) FindMal(id int) (Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range l.Items {
		if it.MalID == id {
			c := *it
			c.Lists = append([]string(nil), it.Lists...)
			return c, true
		}
	}
	return Item{}, false
}

// Mutate edits an item in place without marking it dirty (sync bookkeeping).
func (l *Library) Mutate(key string, fn func(it *Item)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if it, ok := l.Items[key]; ok {
		fn(it)
		l.saveLocked()
	}
}

// Upsert inserts or replaces an item as-is (sync bookkeeping, no dirty mark).
func (l *Library) Upsert(it Item) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := it
	c.Reconcile()
	l.Items[it.Key()] = &c
	for _, name := range it.Lists {
		found := false
		for _, x := range l.Lists {
			if strings.EqualFold(x, name) {
				found = true
			}
		}
		if !found {
			l.Lists = append(l.Lists, name)
		}
	}
	l.saveLocked()
}

// TakeDeleted returns and clears the MAL ids waiting to be deleted.
func (l *Library) TakeDeleted() []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.MalDeleted
	l.MalDeleted = nil
	l.saveLocked()
	return d
}

// RequeueDeleted puts ids back after a failed delete.
func (l *Library) RequeueDeleted(ids []int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.MalDeleted = append(l.MalDeleted, ids...)
	l.saveLocked()
}

// Drop removes an item without queueing a MAL delete (it is gone there already).
func (l *Library) Drop(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.Items, key)
	l.saveLocked()
}

// Remove deletes the item.
func (l *Library) Remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if it, ok := l.Items[key]; ok && it.MalID != 0 && !it.MalSeen.IsZero() {
		l.MalDeleted = append(l.MalDeleted, it.MalID)
		defer l.signal()
	}
	delete(l.Items, key)
	l.saveLocked()
}

// ListNames returns the custom lists.
func (l *Library) ListNames() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.Lists...)
}

// AddList creates a custom list (no-op if it exists). Returns false on a blank/duplicate name.
func (l *Library) AddList(name string) bool {
	name = strings.TrimSpace(name)
	l.mu.Lock()
	defer l.mu.Unlock()
	if name == "" {
		return false
	}
	for _, x := range l.Lists {
		if strings.EqualFold(x, name) {
			return false
		}
	}
	l.Lists = append(l.Lists, name)
	l.saveLocked()
	return true
}

// RenameList renames a list everywhere.
func (l *Library) RenameList(from, to string) bool {
	to = strings.TrimSpace(to)
	l.mu.Lock()
	defer l.mu.Unlock()
	if to == "" {
		return false
	}
	for _, x := range l.Lists {
		if strings.EqualFold(x, to) && !strings.EqualFold(x, from) {
			return false
		}
	}
	for i, x := range l.Lists {
		if x == from {
			l.Lists[i] = to
		}
	}
	for _, it := range l.Items {
		for i, x := range it.Lists {
			if x == from {
				it.Lists[i] = to
			}
		}
	}
	l.saveLocked()
	return true
}

// DeleteList removes a list (items stay in the library).
func (l *Library) DeleteList(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keep []string
	for _, x := range l.Lists {
		if x != name {
			keep = append(keep, x)
		}
	}
	l.Lists = keep
	for _, it := range l.Items {
		var ls []string
		for _, x := range it.Lists {
			if x != name {
				ls = append(ls, x)
			}
		}
		it.Lists = ls
	}
	l.saveLocked()
}

// ToggleList adds/removes the item (creating it as planned) from a list.
func (l *Library) ToggleList(a source.Anime, name string) bool {
	in := false
	l.Set(a, func(it *Item) {
		for i, x := range it.Lists {
			if x == name {
				it.Lists = append(it.Lists[:i], it.Lists[i+1:]...)
				return
			}
		}
		it.Lists = append(it.Lists, name)
		in = true
	})
	return in
}

// Restore puts back a removed item (undo) and cancels its queued MAL delete.
func (l *Library) Restore(it Item) {
	l.mu.Lock()
	c := it
	c.MalDirty = c.MalID != 0
	// the item may have moved key since (adopted from "mal:<id>" when the
	// show was opened on a source): the old copy replaces it, never joins it
	if c.MalID != 0 {
		for k, x := range l.Items {
			if k != it.Key() && x.MalID == c.MalID && (x.Kind == "" || c.Kind == "" || isFilm(x.Kind) == isFilm(c.Kind)) {
				delete(l.Items, k)
			}
		}
	}
	l.Items[it.Key()] = &c
	var keep []int
	for _, id := range l.MalDeleted {
		if id != it.MalID {
			keep = append(keep, id)
		}
	}
	l.MalDeleted = keep
	l.saveLocked()
	l.mu.Unlock()
	l.signal()
}

func isFilm(kind string) bool {
	return strings.EqualFold(kind, "film") || strings.EqualFold(kind, "movie")
}
