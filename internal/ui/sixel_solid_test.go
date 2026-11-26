package ui

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// Regression: overlayAt previously ran ansi.TruncateLeft on lines containing
// zero-width Sixel APC markers (\x1b_kzN\x1b\\), teleporting a covered image's
// marker to the right edge of the dropdown box instead of dropping it.
func TestOverlayAtNeverTeleportsSixel(t *testing.T) {
	applyTheme(themeByName("gece"))
	g := newGfx(Graphics{Sixel: true, CellW: 10, CellH: 20}, true)

	// Picture 0 at col 50 (will be covered by box [40..90)).
	// Picture 1 at col 100 (to the right of box — must stay at exact col 100).
	// Picture 2 at col 10 (to the left of box — must stay at exact col 10).
	p0 := g.place("u0", 20, 4)
	p1 := g.place("u1", 16, 4)
	p2 := g.place("u2", 16, 4)

	line := strings.Repeat(" ", 10) + p2[0] + strings.Repeat(" ", 24) + p0[0] + strings.Repeat(" ", 30) + p1[0]
	lines := []string{line, strings.Repeat(" ", 130)}
	box := []string{strings.Repeat("X", 50)}

	out := overlayAt(lines, box, 40, 0, 130)
	places := g.extract(out)

	byURL := map[string]placement{}
	for _, p := range places {
		byURL[p.url] = p
	}
	if _, leaked := byURL["u0"]; leaked {
		t.Fatalf("covered picture u0 was teleported instead of dropped: %+v", byURL["u0"])
	}
	if p, ok := byURL["u2"]; !ok || p.col != 10 {
		t.Fatalf("left picture u2 shifted: ok=%v got=%+v want col=10", ok, p)
	}
	if p, ok := byURL["u1"]; !ok || p.col != 100 {
		t.Fatalf("right picture u1 shifted: ok=%v got=%+v want col=100", ok, p)
	}
}

// Regression: typing sparks / confetti slicing a row with ansi.Cut must never
// shift, drop or duplicate a Sixel picture marker on that row.
func TestConfettiNeverShiftsSixel(t *testing.T) {
	applyTheme(themeByName("gece"))
	g := newGfx(Graphics{Sixel: true, CellW: 10, CellH: 20}, true)
	p0 := g.place("cover", 18, 4)

	line := strings.Repeat(" ", 35) + p0[0] + strings.Repeat(" ", 20)
	lines := []string{line}

	var c confetti
	c.ps = []particle{
		{x: 10, y: 0, r: '•', c: hex(cAccent), born: 0, life: 1},
		{x: 40, y: 0, r: '◆', c: hex(cSky), born: 0, life: 1},
		{x: 60, y: 0, r: '+', c: hex(cOK), born: 0, life: 1},
	}
	c.draw(lines, 90)

	places := g.extract(lines)
	if len(places) != 1 {
		t.Fatalf("expected 1 picture after confetti draw, got %d: %+v", len(places), places)
	}
	if places[0].col != 35 || places[0].row != 0 {
		t.Fatalf("confetti shifted Sixel marker to (%d,%d), want (0,35)", places[0].row, places[0].col)
	}
}

// End-to-end reproduction of the screenshot bug: Nova home with a single
// continue card under the open search suggestions dropdown. The card's Sixel
// poster must NOT teleport to the right of the suggestions box.
func TestHomeSuggestCoversCardWithoutTeleport(t *testing.T) {
	a := testApp(t, Graphics{Sixel: true, CellW: 10, CellH: 20})
	a.cfg.UI = "yeni"
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 44})

	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{180, 40, 60, 255})
		}
	}
	putPoster(a, "test://another", img)

	an := source.Anime{Source: "animecix", ID: "1", Title: "Another", Kind: "TV", Poster: "test://another"}
	a.hist.Update(an, func(e *store.Entry) {
		e.Season, e.Episode = 1, 1
		e.Position, e.Duration = 35, 1440
	})

	// Prime the encoded sixel cache.
	a.buildFrame()
	deadline := time.Now().Add(5 * time.Second)
	for a.gfx.busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Open the search suggestions dropdown over the centred card.
	h := a.stack[0].(*home)
	h.focus = 0
	h.in.Set("another")
	h.sug.items = []sugItem{
		{an: an, local: "geçmiş"},
		{an: source.Anime{Source: "animecix", ID: "2", Title: "Another World"}},
		{an: source.Anime{Source: "animecix", ID: "3", Title: "Another Layer"}},
		{an: source.Anime{Source: "animecix", ID: "4", Title: "Another Story"}},
		{an: source.Anime{Source: "animecix", ID: "5", Title: "Another Side"}},
		{an: source.Anime{Source: "animecix", ID: "6", Title: "Another Quest"}},
	}

	f := a.buildFrame()
	for _, im := range f.Images {
		if strings.HasPrefix(im.Key, "test://another|") {
			t.Fatalf("covered card poster should not render while suggest dropdown overlaps it, got %+v", im)
		}
	}

	// Closing suggestions must restore the card poster cleanly in its centred slot.
	h.sug.close()
	f2 := a.buildFrame()
	found := false
	for _, im := range f2.Images {
		if strings.HasPrefix(im.Key, "test://another|") {
			found = true
			if im.Col < 40 || im.Col > 95 {
				t.Fatalf("restored poster landed at unexpected col %d (expected centred)", im.Col)
			}
		}
	}
	if !found {
		t.Fatal("poster did not restore after closing suggestions dropdown")
	}
}
