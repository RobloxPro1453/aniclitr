package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/shaders"
	"aniclitr/internal/source"
)

func TestMpvQuickMenuASSAndPick(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := showcaseApp(t, 120, 36)
	an := source.Anime{Source: "animecix", ID: "1", Title: "Another"}
	ep := source.Episode{ID: "1", Season: 1, Number: 1}
	st := &source.Stream{Qualities: []source.Quality{{Label: "1080p", URL: "x"}}}
	p := newPlaying(an, ep, []source.Episode{ep}, "Fansub", "Tau", st, st.Qualities[0])

	// Open menu via in-mpv action
	p.onMpvAction(a, "aniclitr-menu")
	if !p.qmenu.open {
		t.Fatalf("expected quick menu to open")
	}
	ass := p.buildQuickMenuASS(a)
	for _, want := range []string{"ANICLITR HIZLI MENÜ", "1080p Net", "2K Keskin", "4K Ultra", "Uyku Zamanlayıcısı", "Oynatma Hızı"} {
		if !strings.Contains(ass, want) {
			t.Fatalf("quick menu ASS missing %q", want)
		}
	}

	// Pick item #3 (2K Keskin) directly via number shortcut
	p.onMpvAction(a, "aniclitr-menu-num:3")
	if a.cfg.Anime4K != shaders.P2K {
		t.Fatalf("expected 2k profile after menu-num:3, got %q", a.cfg.Anime4K)
	}

	// Pick item #6 (Sleep timer)
	p.onMpvAction(a, "aniclitr-menu-num:6")
	if !a.sleep.active() {
		t.Fatalf("expected sleep timer active after menu-num:6")
	}

	// Close menu
	p.onMpvAction(a, "aniclitr-menu-close")
	if p.qmenu.open {
		t.Fatalf("expected quick menu to close")
	}
}

func TestNovaHomeZeroIdleCPU(t *testing.T) {
	a := showcaseApp(t, 120, 36)
	a.cfg.UI = "" // Nova default
	a.start = time.Now().Add(-10 * time.Second)
	h := a.stack[0].(*home)
	h.feedOK = true
	h.sug.busy = false
	if h.animating(a) {
		t.Fatalf("Nova home must stop animating (0 FPS / 0%% CPU) once intro and feed finish")
	}
}

func TestSuggestRanksExactMatchFirst(t *testing.T) {
	var s suggest
	s.gen = 1
	s.q = "another"
	s.result(sugResMsg{
		gen: 1,
		q:   "another",
		res: []source.Anime{
			{Source: "animecix", ID: "1", Title: "Re:ZERO -Starting Life in Another World-"},
			{Source: "animecix", ID: "2", Title: "Yandere Dark Elf: From Another World!"},
			{Source: "animecix", ID: "3", Title: "Another"},
		},
	})
	if len(s.items) == 0 || s.items[0].an.Title != "Another" {
		t.Fatalf("expected exact match 'Another' at #1 in home suggest dropdown, got %+v", s.items)
	}
}

func TestMouseMotionSkipsPaint(t *testing.T) {
	a := showcaseApp(t, 100, 30)
	var buf strings.Builder
	a.selfPaintSync(&buf)
	buf.Reset()
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: 10, Y: 5})
	if buf.Len() != 0 {
		t.Fatalf("mouse motion must NOT trigger a frame paint (wrote %d bytes)", buf.Len())
	}
}
