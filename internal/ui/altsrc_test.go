package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"testing"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// An archive title opened from the shelf knows no other source: `s` must
// look it up and move to the live source, keeping the episode (no network:
// the lookup's result is fed in by hand).
func TestSwitchSourceLooksUpOtherSources(t *testing.T) {
	a := testApp(t, Graphics{})
	a.w, a.h = 120, 40
	arc := source.Anime{Source: "animedepo", ID: "mouryou-no-hako", Title: "Mouryou no Hako", Kind: "TV"}
	a.details[arc.Key()] = arc
	sc := newAnime(arc, nil, 1, 5)
	a.stack = append(a.stack, sc)
	sc.ensure(a)
	var eps []source.Episode
	for i := 1; i <= 13; i++ {
		eps = append(eps, source.Episode{ID: "x", Season: 1, Number: i})
	}
	a.episodes[epKey(sc.an, 1)] = eps
	sc.cur = 4

	if a.Update(key("s")); !sc.looking || !sc.waitSwitch {
		t.Fatal("s with a single source should start a lookup and wait for it")
	}
	live := source.Anime{Source: "animecix", ID: "77", Title: "Mouryou no Hako", Kind: "TV"}
	a.Update(altsMsg{key: arc.Key(), alts: []source.Anime{live}})
	if sc.base.Key() != live.Key() || len(sc.srcs) != 2 {
		t.Fatalf("want switch to animecix, got base=%s srcs=%d", sc.base.Key(), len(sc.srcs))
	}
	if sc.wantSeason != 1 || sc.wantEp != 5 {
		t.Fatalf("episode lost on switch: s%d e%d", sc.wantSeason, sc.wantEp)
	}
	a.Update(key("s")) // and back again, no second lookup
	if sc.base.Key() != arc.Key() || sc.looking || !sc.looked {
		t.Fatalf("second s should cycle back to the archive, got %s looking=%v", sc.base.Key(), sc.looking)
	}

	// nothing elsewhere: say so once, do not search again
	other := newAnime(source.Anime{Source: "animedepo", ID: "yok", Title: "Yok Böyle Bir Anime"}, nil, 0, 0)
	a.stack = append(a.stack, other)
	a.Update(key("s"))
	a.Update(altsMsg{key: other.an.Key()})
	if other.looking || !other.looked || len(other.srcs) != 1 || a.toastKind != "warn" {
		t.Fatalf("empty lookup: looking=%v looked=%v srcs=%d toast=%q", other.looking, other.looked, len(other.srcs), a.toastKind)
	}
}

// Two sources known from history: `s` switches at once and still asks the
// others, so a third source (the primary) is added when it answers.
func TestSwitchSourceKnownPlusLookup(t *testing.T) {
	a := testApp(t, Graphics{})
	a.w, a.h = 120, 40
	tr := source.Anime{Source: "animetr", ID: "another", Title: "Another", MalID: 11111}
	en := source.Anime{Source: "hianime", ID: "another-992", Title: "Another"}
	a.hist.Update(en, func(e *store.Entry) { e.Season, e.Episode = 1, 1 })
	sc := newAnime(tr, nil, 0, 0)
	a.stack = append(a.stack, sc)
	sc.start(a)
	if len(sc.srcs) != 2 || sc.srcs[1].Source != "hianime" {
		t.Fatalf("history sibling should be offered up front: %+v", sc.srcs)
	}
	a.Update(key("s"))
	if sc.base.Source != "hianime" || !sc.looking || sc.lookKey != tr.Key() {
		t.Fatalf("switch now, look up in background: base=%s looking=%v key=%s", sc.base.Source, sc.looking, sc.lookKey)
	}
	cix := source.Anime{Source: "animecix", ID: "7822", Title: "Another", MalID: 11111}
	a.Update(altsMsg{key: tr.Key(), alts: []source.Anime{cix, en}})
	if len(sc.srcs) != 3 || sc.base.Source != "hianime" {
		t.Fatalf("found source joins the list without yanking the screen: srcs=%d base=%s", len(sc.srcs), sc.base.Source)
	}
}

// The shelf shows a show once even when it was watched on three sources;
// the newest one stands for it.
func TestRecentMergesSources(t *testing.T) {
	a := testApp(t, Graphics{})
	for _, an := range []source.Anime{
		{Source: "animecix", ID: "7822", Title: "Another", MalID: 11111},
		{Source: "animetr", ID: "another", Title: "Another", MalID: 11111},
		{Source: "hianime", ID: "another-992", Title: "Another"},
		{Source: "animecix", ID: "82", Title: "Bleach", MalID: 269},
		{Source: "animetr", ID: "another-the-other", Title: "Another: The Other - Inga", MalID: 13161},
	} {
		a.hist.Update(an, func(e *store.Entry) { e.Season, e.Episode = 1, 1 })
	}
	got := map[string]int{}
	for _, e := range a.recent() {
		got[e.Title]++
	}
	if got["Another"] != 1 || got["Bleach"] != 1 || got["Another: The Other - Inga"] != 1 || len(a.recent()) != 3 {
		t.Fatalf("recent: %v", got)
	}
	if r := a.recent(); r[len(r)-1].Title != "Another: The Other - Inga" && r[0].Source != "animetr" {
		t.Fatalf("newest source should stand for the show: %+v", r[0])
	}
	if sib := a.siblings(source.Anime{Source: "animecix", ID: "7822", Title: "Another", MalID: 11111}); len(sib) != 2 {
		t.Fatalf("siblings: %+v", sib)
	}
}

// Deleting a shelf card removes the show on every source (the older source
// must not pop back in), and ctrl+z brings all of them back.
func TestShelfDeleteRemovesAllSources(t *testing.T) {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	for _, an := range []source.Anime{
		{Source: "animecix", ID: "7822", Title: "Another", MalID: 11111},
		{Source: "animetr", ID: "another", Title: "Another", MalID: 11111},
		{Source: "hianime", ID: "another-992", Title: "Another"},
	} {
		a.hist.Update(an, func(e *store.Entry) { e.Season, e.Episode = 1, 1 })
	}
	h := a.stack[0].(*home)
	h.focus, h.hCur = 1, 0
	a.Update(key("x"))
	if n := len(a.hist.Recent()); n != 0 {
		t.Fatalf("all three source entries should go, %d left", n)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if n := len(a.hist.Recent()); n != 3 {
		t.Fatalf("undo should restore every source, got %d", n)
	}
}

// The shelf card stands for the source you used last (HiAnime here), but the
// show opens on the primary source, at the freshest episode across sources.
func TestShelfOpensOnPrimarySource(t *testing.T) {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	cix := source.Anime{Source: "animecix", ID: "7822", Title: "Another", MalID: 11111, Kind: "TV"}
	hi := source.Anime{Source: "hianime", ID: "another-992", Title: "Another", Kind: "TV"}
	a.hist.Update(cix, func(e *store.Entry) { e.Season, e.Episode = 1, 1 })
	time.Sleep(5 * time.Millisecond)
	a.hist.Update(hi, func(e *store.Entry) { e.Season, e.Episode = 1, 5 })
	if r := a.recent(); len(r) != 1 || r[0].Source != "hianime" {
		t.Fatalf("shelf card should be the HiAnime entry: %+v", r)
	}
	h := a.stack[0].(*home)
	h.focus, h.hCur = 1, 0
	a.Update(key("enter"))
	sc, ok := a.top().(*animeScreen)
	if !ok || sc.base.Source != "animecix" || sc.srcIdx != 0 {
		t.Fatalf("should open on AnimeciX, got %+v", sc.base)
	}
	a.details[cix.Key()] = cix
	sc.ensure(a)
	if sc.wantSeason != 1 || sc.wantEp != 5 {
		t.Fatalf("progress from HiAnime should carry over: S%dE%d", sc.wantSeason, sc.wantEp)
	}
	// with the primary switched off, the next switched-on source leads
	a.cfg.Sources["animecix"] = false
	sc2 := newAnime(hi, nil, 0, 0)
	sc2.start(a)
	if sc2.base.Source != "hianime" || sc2.srcs[len(sc2.srcs)-1].Source != "animecix" {
		t.Fatalf("switched-off source goes last: %+v", sc2.srcs)
	}
}
