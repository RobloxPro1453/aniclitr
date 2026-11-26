package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/config"
	"aniclitr/internal/shaders"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func TestMpvQuickMenuASSAndPick(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := showcaseApp(t, 120, 36)
	an := source.Anime{Source: "animecix", ID: "1", Title: "Another"}
	ep := source.Episode{ID: "1", Season: 1, Number: 1}
	st := &source.Stream{Qualities: []source.Quality{{Label: "1080p", URL: "x"}}}
	p := newPlaying(an, ep, []source.Episode{ep}, "Fansub", "Tau", st, st.Qualities[0])
	p.videoH = 480
	p.volume = 100

	// Open menu via in-mpv action
	p.onMpvAction(a, "aniclitr-menu")
	if !p.qmenu.open {
		t.Fatalf("expected quick menu to open")
	}
	ass := p.buildQuickMenuASS(a)
	for _, want := range []string{`\p1`, "ANICLITR", "HIZLI MENÜ", "480p", "1080p Net", "2K Keskin", "4K Ultra", "Ses Seviyesi", "Uyku Zamanlayıcısı", "Oynatma Hızı"} {
		if !strings.Contains(ass, want) {
			t.Fatalf("quick menu ASS missing %q", want)
		}
	}

	// Pick item #3 (2K Keskin) directly via number shortcut
	p.onMpvAction(a, "aniclitr-menu-num:3")
	if a.cfg.Anime4K != shaders.P2K {
		t.Fatalf("expected 2k profile after menu-num:3, got %q", a.cfg.Anime4K)
	}

	// Inline ←/→ adjusts upscale cleanly
	p.onMpvAction(a, "aniclitr-menu-right")
	if a.cfg.Anime4K != shaders.P4K {
		t.Fatalf("expected 4k profile after menu-right, got %q", a.cfg.Anime4K)
	}

	// Pick item #9 (Sleep timer)
	p.onMpvAction(a, "aniclitr-menu-num:9")
	if !a.sleep.active() {
		t.Fatalf("expected sleep timer active after menu-num:9")
	}

	// Volume inline step on row #7 (index 6)
	p.qmenu.cur = 6
	p.onMpvAction(a, "aniclitr-menu-left")
	if p.volume != 95 {
		t.Fatalf("expected volume 95 after menu-left, got %d", p.volume)
	}

	// Close menu
	p.onMpvAction(a, "aniclitr-menu-close")
	if p.qmenu.open {
		t.Fatalf("expected quick menu to close")
	}
}

func TestNovaHomeAmbientAndZeroIdleCPU(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := New(config.Load(), Graphics{})
	a.w, a.h = 120, 36
	a.cfg.UI = "" // Nova default
	a.cfg.Motion = true
	a.start = time.Now().Add(-10 * time.Second)
	a.oscUntil = time.Time{}
	h := a.stack[0].(*home)
	h.feedOK = true
	h.sug.busy = false

	// With ambient active ("auto" / "yağmur"), Nova home keeps ambient smoothly alive at 20 FPS (50ms).
	a.cfg.Ambient = "yağmur"
	if !h.animating(a) {
		t.Fatalf("Nova home must keep ambient weather moving when Ambient != yok")
	}
	if got := a.tickInterval(); got != 50*time.Millisecond {
		t.Fatalf("calm ambient idle should pace at 20 FPS (50ms), got %v", got)
	}

	// With ambient turned off ("yok"), Nova home settles cleanly to 0 FPS / 0% CPU.
	a.cfg.Ambient = "yok"
	if h.animating(a) {
		t.Fatalf("Nova home must stop animating (0 FPS / 0%% CPU) when ambient is yok")
	}
}

func TestTotalWatchedEpsIncludesSyncedLibrary(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := showcaseApp(t, 120, 36)
	a.lib.Set(source.Anime{Source: "mal", ID: "1", Title: "Another"}, func(it *store.Item) {
		it.Status = store.Completed
		it.Watched = 12
	})
	a.lib.Set(source.Anime{Source: "mal", ID: "2", Title: "Frieren"}, func(it *store.Item) {
		it.Status = store.Completed
		it.Watched = 28
	})
	if got := totalWatchedEps(a); got < 40 {
		t.Fatalf("expected totalWatchedEps to reflect synced library episodes (>=40), got %d", got)
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
