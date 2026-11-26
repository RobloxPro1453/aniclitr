package ui

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/anilist"
	"aniclitr/internal/download"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func novaApp(t *testing.T) *App {
	a := testApp(t, Graphics{})
	a.cfg.UI = "yeni"
	a.w, a.h = 140, 30
	return a
}

// Classic stays the default: nothing changes until the setting says so.
func TestNovaIsOptIn(t *testing.T) {
	a := testApp(t, Graphics{})
	a.notify("merhaba", "ok")
	if a.nova() || len(a.cards) != 0 {
		t.Fatal("the classic interface must stay the default")
	}
}

func TestNovaNotifications(t *testing.T) {
	a := novaApp(t)
	a.notify("bir", "ok")
	a.notify("iki", "warn")
	a.notify("bir", "ok") // the same text again only moves to the front
	if len(a.cards) != 2 || a.cards[1].text != "bir" {
		t.Fatalf("queue: %+v", a.cards)
	}
	if a.cards[0].until.Sub(a.cards[0].at) < 5*time.Second {
		t.Fatal("warnings should stay long enough to read")
	}
	for i := range a.cards {
		a.cards[i].at = time.Now().Add(-time.Second) // past the slide-in
	}
	foot := ansi.Strip(a.novaFooter())
	if !strings.Contains(foot, "✓ bir") || !strings.Contains(foot, "+1") {
		t.Fatalf("footer pill: %q", foot)
	}
	for i := range a.cards {
		a.cards[i].until = time.Now().Add(-time.Millisecond)
	}
	if foot := ansi.Strip(a.novaFooter()); strings.Contains(foot, "bir") || len(a.cards) != 0 {
		t.Fatalf("expired notifications must go: %q", foot)
	}
}

func TestNovaTopBar(t *testing.T) {
	a := novaApp(t)
	a.cfg.Sources = map[string]bool{"animecix": true, "hianime": false}
	a.health = map[string]source.Health{"animecix": {ID: "animecix", OK: true}, "anihub": {ID: "anihub", OK: false}}
	a.dl.Preview(download.Job{ID: 1, Title: "X", State: download.Running, Done: 50, Total: 100, Speed: 2 << 20})
	a.samplePulse()
	head := ansi.Strip(a.novaHeader())
	for _, want := range []string{"↓ 1", "%50", "cix", "hub✗", "hi"} {
		if !strings.Contains(head, want) {
			t.Errorf("top bar lacks %q: %q", want, head)
		}
	}
	if rule := ansi.Strip(a.novaRule()); !strings.Contains(rule, "━") {
		t.Error("the rule should show download progress")
	}
	a.w = 80 // narrow: dots instead of names
	if head := ansi.Strip(a.novaHeader()); strings.Contains(head, "cix") || !strings.Contains(head, "●") {
		t.Errorf("narrow top bar: %q", head)
	}
}

// Nova home: ← → walk a row, ↓ goes to the dock, ↑ back to the cards and
// then the search box; enter still runs the item.
func TestNovaHomeKeys(t *testing.T) {
	a := showcaseApp(t, 140, 40)
	a.cfg.UI = "yeni"
	h := a.stack[0].(*home)
	_ = h.novaView(a, 140, 37) // lays out (and sizes the shelf)
	items := h.items(a)
	cards, dock := novaRows(items)
	if len(cards) < 2 || len(dock) < 3 {
		t.Fatalf("rows: cards %d dock %d", len(cards), len(dock))
	}
	h.focus, h.mCur = 1, cards[0]
	press := func(k string) { h.key(a, key(k)) }
	press("right")
	if h.mCur != cards[1] {
		t.Fatal("→ should move to the next card")
	}
	press("down")
	if h.mCur != dock[0] {
		t.Fatal("↓ should reach the dock")
	}
	press("right")
	press("right")
	if h.mCur != dock[2] || items[h.mCur].id != "discover" {
		t.Fatalf("→→ on the dock: %s", items[h.mCur].id)
	}
	press("up")
	if h.mCur != cards[0] || h.focus != 1 {
		t.Fatal("↑ from the dock should return to the cards")
	}
	press("up")
	if h.focus != 0 {
		t.Fatal("↑ from the cards should reach the search box")
	}
	h.focus, h.mCur = 1, dock[1] // Kitaplık
	h.key(a, key("enter"))
	if _, ok := a.top().(*library); !ok {
		t.Fatalf("enter on Kitaplık opened %T", a.top())
	}
}

// A dropdown hides only the pictures it touches; no picture may end up
// under the box (text is never written into a picture).
func TestCoversDropOnlyTouchedPictures(t *testing.T) {
	a := testApp(t, Graphics{})
	a.covers = []coverRect{{x: 100, y: 2, w: 40, h: 10}}
	if !a.covered(5, 120, 18, 12) || a.covered(5, 10, 18, 12) || a.covered(20, 120, 18, 12) {
		t.Fatal("cover overlap math")
	}

	a = testApp(t, Graphics{Sixel: true, CellW: 10, CellH: 20})
	a.Update(tea.WindowSizeMsg{Width: 150, Height: 44})
	img := image.NewRGBA(image.Rect(0, 0, 300, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, color.RGBA{uint8(y / 2), uint8(x / 2), 180, 255})
		}
	}
	putPoster(a, "test://poster", img)
	an := source.Anime{Source: "animecix", ID: "1", Title: "Deneme", Kind: "TV", Poster: "test://poster", Description: "özet"}
	a.details[an.Key()] = an
	sc := newAnime(an, nil, 0, 0)
	a.stack = append(a.stack, sc)
	sc.ensure(a)
	a.episodes[epKey(sc.an, 1)] = []source.Episode{{ID: "1", Season: 1, Number: 1, Title: "Bir"}}
	a.buildFrame()
	deadline := time.Now().Add(5 * time.Second)
	for a.gfx.busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f := a.buildFrame(); len(f.Images) == 0 {
		t.Fatal("the poster should be placed")
	}
	a.openDropdown("bell")
	f := a.buildFrame()
	if len(a.covers) == 0 {
		t.Fatal("the bell panel should register its box")
	}
	for _, im := range f.Images {
		if a.covered(im.Row, im.Col, im.W, im.H) {
			t.Fatalf("a picture was placed under the panel: %+v", im)
		}
	}
}

// The band speaks plainly: when, what, which episode; stats as sentences.
func TestNovaBandPlain(t *testing.T) {
	a := novaApp(t)
	now := time.Now()
	a.schedule = []anilist.Airing{
		{At: now.Add(2 * time.Hour), Episode: 33, Media: anilist.Media{IDMal: 777, Title: anilist.Titles{Romaji: "Holo no Graffiti"}}},
		{At: now.Add(-time.Hour), Episode: 1, Media: anilist.Media{IDMal: 1, Title: anilist.Titles{Romaji: "Geçmiş Yayın"}}},
	}
	a.lib.Set(source.Anime{Source: "animecix", ID: "1", Title: "Holo", MalID: 777}, func(x *store.Item) { x.Status = store.Watching })
	up := ansi.Strip(strings.Join(novaUpcoming(a, 60, 8), "\n"))
	if !strings.Contains(up, "bugün") || !strings.Contains(up, "Holo no Graffiti · 33. bölüm") || strings.Contains(up, "Geçmiş Yayın") {
		t.Fatalf("upcoming:\n%s", up)
	}
	st := ansi.Strip(strings.Join(novaStats(a, 40), "\n"))
	if !strings.Contains(st, "bu hafta") || !strings.Contains(st, "izledin") || !strings.Contains(st, "kitaplık") {
		t.Fatalf("stats:\n%s", st)
	}
	if fmtWatch(45*60) != "45 dk" || fmtWatch(130*60) != "2 sa 10 dk" {
		t.Fatal("watch time wording")
	}
}

func TestNovaAnimeWideAndCompact(t *testing.T) {
	a := novaApp(t)
	an := source.Anime{
		Source:      "animecix",
		ID:          "100",
		Title:       "Sousou no Frieren",
		AltTitle:    "Frieren: Beyond Journey's End",
		Kind:        "TV",
		Year:        2023,
		Genres:      []string{"Macera", "Dram", "Fantastik"},
		Description: "Büyücü Frieren uzun bir yolculuğa çıkar ve insanları tanımaya çalışır.",
		MalID:       52991,
	}
	a.details[an.Key()] = an
	a.lib.Set(an, func(it *store.Item) {
		it.Status = store.Watching
		it.Score = 10
		it.Lists = []string{"favoriler"}
	})
	a.hist.Update(an, func(e *store.Entry) {
		e.Season, e.Episode = 1, 2
		e.Watched[store.EpKey(1, 1)] = true
		e.Progress[store.EpKey(1, 2)] = 45
		e.Notes = map[string]string{store.EpKey(1, 2): "Harika bölüm notu"}
	})
	ep1File := filepath.Join(t.TempDir(), "S01E01.mp4")
	if err := os.WriteFile(ep1File, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.dl.Preview(download.Job{
		ID: 42, Source: "animecix", AnimeID: "100", Title: an.Title,
		Season: 1, Episode: 1, State: download.Done, Total: 300 << 20, Path: ep1File,
	})
	eps := []source.Episode{
		{ID: "1", Season: 1, Number: 1, Title: "Yolculuğun Sonu"},
		{ID: "2", Season: 1, Number: 2, Title: "Büyü Olmasına Gerek Yoktu"},
		{ID: "3", Season: 1, Number: 3, Title: "Öldürme Büyüsü"},
	}
	sc := newAnime(an, nil, 1, 2)
	sc.ensure(a)
	a.episodes[epKey(sc.an, 1)] = eps
	sc.cur = 1

	// Wide layout: showcase card + episode stage + badges + note.
	wide := sc.view(a, 140, 36)
	linesW := strings.Split(wide, "\n")
	if len(linesW) != 36 {
		t.Fatalf("wide nova anime: got %d lines, want 36", len(linesW))
	}
	plainW := ansi.Strip(wide)
	for _, want := range []string{"Sousou no Frieren", "izliyor", "Kadro", "1/3 izlendi", "↓1", "kaldığın yer", "Harika bölüm notu"} {
		if !strings.Contains(plainW, want) {
			t.Errorf("wide nova anime missing %q:\n%s", want, plainW)
		}
	}

	// Compact layout (80x24): folds cleanly into 2-line hero + episode stage.
	narrow := sc.view(a, 80, 21)
	linesN := strings.Split(narrow, "\n")
	if len(linesN) != 21 {
		t.Fatalf("compact nova anime: got %d lines, want 21", len(linesN))
	}
	plainN := ansi.Strip(narrow)
	for _, want := range []string{"Sousou no Frieren", "1/3 izlendi", "Yolculuğun Sonu"} {
		if !strings.Contains(plainN, want) {
			t.Errorf("compact nova anime missing %q:\n%s", want, plainN)
		}
	}
}

func TestNovaPickerAndPlayingViews(t *testing.T) {
	a := novaApp(t)
	an := source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren", Kind: "TV"}
	ep := source.Episode{ID: "5", Season: 1, Number: 5, Title: "Ölülerin Hayaletleri"}
	eps := []source.Episode{ep}

	pk := newPicker(an, ep, eps, false)
	pk.build(a, []source.Variant{
		{Fansub: "HakaiRyuu", Host: "Tau Video", URL: "https://tau-video.xyz/v1"},
		{Fansub: "AnimeciX", Host: "Sibnet", URL: "https://sibnet.ru/v2"},
	})
	a.resolved["https://tau-video.xyz/v1"] = &resolveState{
		st: &source.Stream{Qualities: []source.Quality{{Label: "1080p", Height: 1080, Size: 420 << 20}}},
	}

	wide := pk.view(a, 140, 32)
	if len(strings.Split(wide, "\n")) != 32 {
		t.Fatalf("nova picker wide height mismatch")
	}
	plain := ansi.Strip(wide)
	for _, want := range []string{"Sousou no Frieren", "HakaiRyuu", "Tau Video", "1080p", "çeviri"} {
		if !strings.Contains(strings.ToLower(plain), strings.ToLower(want)) {
			t.Errorf("nova picker wide missing %q:\n%s", want, plain)
		}
	}
	narrow := pk.view(a, 80, 21)
	if len(strings.Split(narrow, "\n")) != 21 {
		t.Fatalf("nova picker narrow height mismatch")
	}

	st := &source.Stream{Qualities: []source.Quality{{Label: "1080p", URL: "x"}}}
	pl := newPlaying(an, ep, eps, "HakaiRyuu", "Tau Video", st, st.Qualities[0])
	pl.started, pl.pos, pl.dur = true, 620, 1440
	pv := pl.view(a, 120, 30)
	if len(strings.Split(pv, "\n")) != 30 {
		t.Fatalf("nova playing height mismatch")
	}
	plainP := ansi.Strip(pv)
	for _, want := range []string{"Sousou no Frieren", "HakaiRyuu", "Tau Video", "1080p", "10:20", "24:00"} {
		if !strings.Contains(plainP, want) {
			t.Errorf("nova playing missing %q:\n%s", want, plainP)
		}
	}
}

func TestNovaDiscoverRedesignAndSpatialKeys(t *testing.T) {
	a := novaApp(t)
	d := newDiscover()
	d.schedOK = true
	d.seasonOK = true
	d.season = []anilist.Media{
		{ID: 1, IDMal: 52991, Format: "TV", Episodes: 28, Score: 91, Season: "FALL", SeasonYear: 2023,
			Title: anilist.Titles{Romaji: "Sousou no Frieren"}, Genres: []string{"Adventure", "Drama"}},
	}
	a.stack = append(a.stack, d)

	// Starts on left rail; → enters active room; inside room → NEVER jumps tabs; ← returns to rail.
	if !d.menu || d.tab != tCal {
		t.Fatalf("initial discover state: menu=%v tab=%d", d.menu, d.tab)
	}
	a.Update(key("down"))
	a.Update(key("down")) // Bu sezon
	a.Update(key("right"))
	if d.menu || d.tab != tSeason {
		t.Fatalf("→ on rail should enter Bu sezon: menu=%v tab=%d", d.menu, d.tab)
	}
	a.Update(key("right"))
	if d.tab != tSeason || d.menu {
		t.Fatalf("→ inside a section must NEVER jump to another section: menu=%v tab=%d", d.menu, d.tab)
	}
	wide := d.view(a, 150, 36)
	if len(strings.Split(wide, "\n")) != 36 {
		t.Fatalf("nova discover wide height mismatch")
	}
	plain := ansi.Strip(wide)
	for _, want := range []string{"Keşfet", "Takvim", "Bu sezon", "Sousou no Frieren", "9.1"} {
		if !strings.Contains(plain, want) {
			t.Errorf("nova discover missing %q:\n%s", want, plain)
		}
	}

	// ← steps cleanly back to the left rail without jumping to another tab.
	a.Update(key("left"))
	if !d.menu || d.tab != tSeason {
		t.Fatalf("← inside a section should return to rail on same tab: menu=%v tab=%d", d.menu, d.tab)
	}
}

func TestContinueEntriesIncludesMalWatching(t *testing.T) {
	a := novaApp(t)
	a.hist.Update(source.Anime{Source: "animecix", ID: "1", Title: "Frieren", MalID: 52991}, func(e *store.Entry) {
		e.Season, e.Episode = 1, 4
	})
	// Duplicate already in history must NOT appear twice.
	a.lib.Upsert(store.Item{Source: "mal", AnimeID: "52991", Title: "Sousou no Frieren", MalID: 52991, Status: store.Watching, Watched: 4})
	// New MAL watching show not yet opened locally MUST appear in continueEntries.
	a.lib.Upsert(store.Item{Source: "mal", AnimeID: "40748", Title: "Jujutsu Kaisen", MalID: 40748, Status: store.Watching, Watched: 7, EpCount: 24})
	// Completed / Planned items must NOT appear in continueEntries.
	a.lib.Upsert(store.Item{Source: "mal", AnimeID: "111", Title: "Planned Show", MalID: 111, Status: store.Planned})

	got := a.continueEntries()
	if len(got) != 2 {
		t.Fatalf("continueEntries len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Title != "Frieren" || got[1].Title != "Jujutsu Kaisen" || got[1].Episode != 7 {
		t.Fatalf("unexpected continueEntries: %+v", got)
	}
}

func TestPickQualityForRemembersPerShow(t *testing.T) {
	a := novaApp(t)
	a.cfg.Quality = "best"
	an := source.Anime{Source: "animecix", ID: "77", Title: "One Piece"}
	a.hist.Update(an, func(e *store.Entry) {
		e.Quality = "720p"
	})
	st := &source.Stream{Qualities: []source.Quality{
		{Label: "1080p", Height: 1080, URL: "u1080"},
		{Label: "720p", Height: 720, URL: "u720"},
		{Label: "480p", Height: 480, URL: "u480"},
	}}
	q := a.pickQualityFor(an, st)
	if q.Label != "720p" {
		t.Fatalf("pickQualityFor = %q, want 720p (per-show memory)", q.Label)
	}
	// A different anime without saved preference still follows global cfg.Quality ("best").
	other := source.Anime{Source: "animecix", ID: "88", Title: "Frieren"}
	if q2 := a.pickQualityFor(other, st); q2.Label != "1080p" {
		t.Fatalf("pickQualityFor(other) = %q, want 1080p", q2.Label)
	}
}
