package ui

import (
	"image"
	"image/color"
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
