// Package malsync keeps the local library and the MyAnimeList list in step.
//
// MAL is the source of truth for status, score, watched-episode count and
// tags (= our watchlists). Local edits mark an item dirty and are pushed
// within a couple of seconds (or when the network is back); remote edits
// are pulled at start-up and every 15 minutes. When both sides changed,
// the local, not yet sent edit wins (it is newer than the last pull).
package malsync

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/anilist"
	"aniclitr/internal/logx"
	"aniclitr/internal/mal"
	"aniclitr/internal/match"
	"aniclitr/internal/seasons"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// Status is shown in the UI.
type Status struct {
	Busy     bool
	LastSync time.Time
	Err      string
	Pulled   int
	Pushed   int
}

// Syncer runs in the background.
type Syncer struct {
	C   *mal.Client
	Lib *store.Library

	mu     sync.Mutex
	st     Status
	kick   chan struct{}
	pullCh chan struct{}
	// Updates is signalled when Status changes (UI repaints).
	Updates chan struct{}
	started bool
}

// New prepares a syncer (Start launches it).
func New(c *mal.Client, lib *store.Library) *Syncer {
	return &Syncer{C: c, Lib: lib, kick: make(chan struct{}, 1), pullCh: make(chan struct{}, 1), Updates: make(chan struct{}, 1)}
}

// Status returns a snapshot.
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

func (s *Syncer) set(f func(st *Status)) {
	s.mu.Lock()
	f(&s.st)
	s.mu.Unlock()
	select {
	case s.Updates <- struct{}{}:
	default:
	}
}

// Start launches the loop once (no-op when not connected).
func (s *Syncer) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		s.SyncNow()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.loop()
	s.SyncNow()
}

// SyncNow requests a full pull + push.
func (s *Syncer) SyncNow() {
	select {
	case s.pullCh <- struct{}{}:
	default:
	}
}

func (s *Syncer) loop() {
	defer func() {
		if r := recover(); r != nil {
			logx.Panic("mal sync", r)
		}
	}()
	pullTick := time.NewTicker(15 * time.Minute)
	defer pullTick.Stop()
	for {
		pull := false
		select {
		case <-s.pullCh:
			pull = true
		case <-pullTick.C:
			pull = true
		case <-s.Lib.Changed:
			time.Sleep(2 * time.Second) // let a burst of edits settle
		case <-s.kick:
		}
		if !s.C.Connected() {
			continue
		}
		s.run(pull)
	}
}

func (s *Syncer) run(pull bool) {
	s.set(func(st *Status) { st.Busy = true })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	pulled, pushed := 0, 0
	if pull {
		pulled, err = s.Pull(ctx)
	}
	if err == nil {
		pushed, err = s.Push(ctx)
	}
	s.set(func(st *Status) {
		st.Busy = false
		st.Pulled, st.Pushed = pulled, pushed
		if err != nil {
			st.Err = err.Error()
			logx.Errorf("MAL eşitleme: %v", err)
		} else {
			st.Err = ""
			st.LastSync = time.Now()
		}
	})
}

// Status mapping.
var toMal = map[string]string{store.Watching: mal.Watching, store.Completed: mal.Completed, store.Planned: mal.PlanToWatch, store.OnHold: mal.OnHold, store.Dropped: mal.Dropped}
var fromMal = map[string]string{mal.Watching: store.Watching, mal.Completed: store.Completed, mal.PlanToWatch: store.Planned, mal.OnHold: store.OnHold, mal.Dropped: store.Dropped}

// Pull applies the MAL list locally. Returns how many items changed.
func (s *Syncer) Pull(ctx context.Context) (int, error) {
	list, err := s.C.List(ctx)
	if err != nil {
		return 0, err
	}
	onMal := map[int]bool{}
	changed := 0
	for _, e := range list {
		onMal[e.ID] = true
		it, ok := s.Lib.FindMal(e.ID)
		switch {
		case !ok:
			// on MAL but not here yet: shelve it (opened later on a source)
			s.Lib.Upsert(store.Item{
				Source: "mal", AnimeID: strconv.Itoa(e.ID), MalID: e.ID,
				Title: e.Title, Poster: e.Picture, Kind: kind(e.MediaType), Year: e.Year, EpCount: e.NumEpisodes,
				Status: fromMal[e.Status], Score: e.Score, Watched: e.Watched, Lists: e.Tags,
				AddedAt: e.Updated, UpdatedAt: e.Updated, MalSeen: e.Updated,
			})
			changed++
		case it.MalDirty:
			// a local edit not sent yet is newer than what we last pulled
		case e.Updated.After(it.MalSeen):
			s.Lib.Upsert(apply(it, e))
			changed++
		}
	}
	// gone from MAL (deleted there) after having been synced: drop it here
	for _, it := range s.Lib.All() {
		if it.MalID != 0 && !onMal[it.MalID] && !it.MalSeen.IsZero() && !it.MalDirty {
			s.Lib.Drop(it.Key())
			changed++
		}
	}
	return changed, nil
}

func apply(it store.Item, e mal.Entry) store.Item {
	if st := fromMal[e.Status]; st != "" {
		it.Status = st
	}
	it.Score, it.Watched = e.Score, e.Watched
	it.Lists = append([]string(nil), e.Tags...)
	if it.Poster == "" {
		it.Poster = e.Picture
	}
	if e.NumEpisodes > 0 {
		it.EpCount = e.NumEpisodes
	}
	it.Reconcile()
	it.MalSeen = e.Updated
	it.UpdatedAt = time.Now()
	return it
}

func kind(mediaType string) string {
	switch mediaType {
	case "movie":
		return "Film"
	case "tv":
		return "TV"
	}
	return strings.ToUpper(mediaType)
}

// Push sends local changes (and queued deletions). Items without a MAL id
// are matched by title through AniList first.
func (s *Syncer) Push(ctx context.Context) (int, error) {
	pushed := 0
	if del := s.Lib.TakeDeleted(); len(del) > 0 {
		var failed []int
		for _, id := range del {
			if err := s.C.Delete(ctx, id); err != nil {
				failed = append(failed, id)
			}
		}
		if len(failed) > 0 {
			s.Lib.RequeueDeleted(failed)
		}
	}
	for _, it := range s.Lib.All() {
		if !it.MalDirty {
			continue
		}
		if it.MalID == 0 {
			if it.NoMal {
				continue
			}
			id := matchItemMal(ctx, it)
			if id == 0 {
				s.Lib.Mutate(it.Key(), func(x *store.Item) { x.NoMal = true })
				continue
			}
			s.Lib.Mutate(it.Key(), func(x *store.Item) { x.MalID = id })
			it.MalID = id
		}
		st := toMal[it.Status]
		score, watched := it.Score, it.Watched
		if it.Status == store.Completed && it.EpCount > 0 && watched < it.EpCount {
			watched = it.EpCount
		}
		tags := append([]string(nil), it.Lists...)
		if err := s.C.Set(ctx, it.MalID, mal.Update{Status: st, Score: &score, Watched: &watched, Tags: &tags}); err != nil {
			return pushed, err
		}
		key, sig := it.Key(), it.UpdatedAt
		s.Lib.Mutate(key, func(x *store.Item) {
			if x.UpdatedAt.Equal(sig) { // not edited again meanwhile
				x.MalDirty = false
			}
			x.MalSeen = time.Now()
		})
		pushed++
	}
	return pushed, nil
}

// matchItemMal finds a MAL id for a library item using persistent mappings.json
// first, then scored fuzzy matching against AniList.
func matchItemMal(ctx context.Context, it store.Item) int {
	if e, ok := match.Default().Get(it.Key()); ok && e.MalID > 0 {
		return e.MalID
	}
	titles := it.Anime().AllTitles()
	want := match.Want{
		Titles: titles,
		Season: source.ParseSeasonNumber(it.Title),
		Year:   it.Year,
		Kind:   it.Kind,
		Eps:    it.EpCount,
	}
	var cands []anilist.Media
	seen := map[int]bool{}
	for _, q := range match.BuildQueries(titles) {
		res, err := anilist.Search(ctx, q)
		if err != nil {
			continue
		}
		for _, m := range res {
			if m.IDMal != 0 && !seen[m.IDMal] {
				seen[m.IDMal] = true
				cands = append(cands, m)
			}
		}
		if len(cands) >= 10 {
			break
		}
	}
	if len(cands) == 0 {
		return 0
	}
	scores := make([]match.Scored, len(cands))
	for i, m := range cands {
		sc := match.ScoreCandidate(match.Candidate{
			Titles:    m.AllTitles(),
			Year:      m.SeasonYear,
			Kind:      kind(strings.ToLower(m.Format)),
			Eps:       m.Episodes,
			MalID:     m.IDMal,
			AniListID: m.ID,
		}, want)
		scores[i] = match.Scored{Index: i, Score: sc, Eps: m.Episodes, Year: m.SeasonYear}
	}
	idx, ok, amb := match.Pick(scores)
	if !ok || amb || idx < 0 {
		return 0
	}
	win := cands[idx]
	match.Default().SetAuto(it.Key(), win.IDMal, win.ID, win.Name(), scores[idx].Score)
	return win.IDMal
}

// TranslateEpisode maps a watched provider episode (season, ep) through a
// canonical season Alignment into the target MAL entry ID and episode number.
func TranslateEpisode(al seasons.Alignment, fallbackMalID, season, ep int) (malID, canonEp int) {
	if c, ok := al.ToCanonical(season, ep); ok && c.MalID > 0 {
		return c.MalID, c.Episode
	}
	return fallbackMalID, ep
}

// Anime makes a source.Anime for an item that only exists on MAL.
func Anime(it store.Item) source.Anime { return it.Anime() }
