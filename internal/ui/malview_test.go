package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func epsN(season, n int) []source.Episode {
	var eps []source.Episode
	for i := 1; i <= n; i++ {
		eps = append(eps, source.Episode{ID: "x", Season: season, Number: i})
	}
	return eps
}

// Charlotte is completed on MAL and sits in the library as "mal:28999";
// opened on AnimeciX it must show as completed and every episode watched.
func TestListStateShowsOnSourceScreen(t *testing.T) {
	a := testApp(t, Graphics{})
	a.lib.Upsert(store.Item{Source: "mal", AnimeID: "28999", MalID: 28999, Title: "Charlotte", Kind: "TV", Status: store.Completed, Score: 7, Watched: 13, EpCount: 13})
	an := source.Anime{Source: "animecix", ID: "226", Title: "Charlotte", MalID: 28999, Kind: "TV", Seasons: []source.Season{{Number: 1}}}
	it, ok := a.libItem(an)
	if !ok || it.Status != store.Completed || it.Score != 7 {
		t.Fatalf("library item not found for the source anime: %+v %v", it, ok)
	}
	wv := a.watchView(an)
	for _, ep := range epsN(1, 13) {
		if !wv.done(ep) {
			t.Fatalf("episode %d should count as watched", ep.Number)
		}
	}
	// a title-only source (no MAL id) finds it too, and writes land on it
	hi := source.Anime{Source: "hianime", ID: "charlotte-1", Title: "Charlotte", Kind: "TV"}
	if _, ok := a.libItem(hi); !ok {
		t.Fatal("title match should find the MAL item")
	}
	n := len(a.lib.All())
	a.libSet(hi, func(x *store.Item) { x.Score = 8 })
	if len(a.lib.All()) != n {
		t.Fatal("writing from another source must not create a second item")
	}
	if it, _ := a.libItem(an); it.Score != 8 {
		t.Fatalf("score should land on the one item, got %d", it.Score)
	}
}

// Bleach: watching on MAL at 154. Opening it lands on 155, 1–154 are ticked
// in the first season only, and unticking 150 takes the count back.
func TestListProgressDrivesEpisodes(t *testing.T) {
	a := testApp(t, Graphics{})
	a.w, a.h = 140, 40
	a.lib.Upsert(store.Item{Source: "mal", AnimeID: "269", MalID: 269, Title: "Bleach", Kind: "TV", Status: store.Watching, Watched: 154, EpCount: 366})
	an := source.Anime{Source: "animecix", ID: "82", Title: "Bleach", MalID: 269, Kind: "TV",
		Seasons: []source.Season{{Number: 1, EpisodeCount: 362}, {Number: 2, EpisodeCount: 50}}}
	a.details[an.Key()] = an
	sc := newAnime(an, nil, 0, 0)
	a.stack = append(a.stack, sc)
	sc.ensure(a)
	if sc.wantSeason != 1 || sc.wantEp != 155 {
		t.Fatalf("should open at S1E155, got S%dE%d", sc.wantSeason, sc.wantEp)
	}
	eps := epsN(1, 362)
	a.episodes[epKey(sc.an, 1)] = eps
	a.Update(episodesMsg{key: epKey(sc.an, 1), season: 1, eps: eps})
	if sc.cur != 154 {
		t.Fatalf("cursor should sit on episode 155 (index 154), got %d", sc.cur)
	}
	wv := a.watchView(sc.an)
	if !wv.done(eps[153]) || wv.done(eps[154]) || wv.done(source.Episode{Season: 2, Number: 1}) {
		t.Fatal("1–154 of season 1 watched, 155 and season 2 not")
	}
	sc.cur = 149 // episode 150
	a.Update(key("w"))
	if it, _ := a.libItem(an); it.Watched != 149 {
		t.Fatalf("unticking 150 should take the list to 149, got %d", it.Watched)
	}
	if a.watchView(sc.an).done(eps[149]) {
		t.Fatal("episode 150 should now be unwatched")
	}
}

// A status flip from the library is one key: ctrl+z must put it (and the
// MAL push) back, without leaving a second copy behind.
func TestStatusChangeIsUndoable(t *testing.T) {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.lib.Upsert(store.Item{Source: "mal", AnimeID: "269", MalID: 269, Title: "Bleach", Kind: "TV", Status: store.Watching, Watched: 154})
	an := source.Anime{Source: "animecix", ID: "82", Title: "Bleach", MalID: 269, Kind: "TV"}
	p := &libPopup{an: an}
	a.popup = p
	before, _ := a.libItem(an)
	// popup row for "completed"
	for i, r := range p.rows(a) {
		if r.kind == "status" && r.val == store.Completed {
			p.cur = i
		}
	}
	a.Update(key("enter"))
	if it, _ := a.libItem(an); it.Status != store.Completed {
		t.Fatalf("status should be completed, got %s", it.Status)
	}
	a.popup = nil
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	it, _ := a.libItem(an)
	if it.Status != store.Watching || it.Watched != before.Watched || !it.MalDirty {
		t.Fatalf("undo should restore watching 154 and resend it: %+v", it)
	}
	n := 0
	for _, x := range a.lib.All() {
		if x.MalID == 269 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("one Bleach item expected, got %d", n)
	}
}
