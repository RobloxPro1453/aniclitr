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
	st := &source.Stream{Qualities: []source.Quality{{Label: "480p", URL: "x"}}}
	p := newPlaying(an, ep, []source.Episode{ep}, "Fansub", "Tau", st, st.Qualities[0])
	p.videoH = 480
	p.volume = 100

	// Open root menu via in-mpv action
	p.onMpvAction(a, "aniclitr-menu")
	if !p.qmenu.open || p.qmenu.page != menuRoot {
		t.Fatalf("expected root quick menu to open")
	}
	ass := p.buildQuickMenuASS(a)
	for _, want := range []string{`\p1`, "ANICLITR", "480p", "Çözünürlük (FSR)", "Kalite & Akıcılık", "Altyazı & Senkron", "Ses & Oynatma", "Bölüm Geçişi"} {
		if !strings.Contains(ass, want) {
			t.Fatalf("root quick menu ASS missing %q", want)
		}
	}

	// Enter sub-menu 1 (Resolution FSR) -> pick 2K (row 4) -> Esc returns to root (does NOT close!)
	p.onMpvAction(a, "aniclitr-menu-num:1")
	if p.qmenu.page != menuRes {
		t.Fatalf("expected menuRes sub-menu, got %v", p.qmenu.page)
	}
	p.onMpvAction(a, "aniclitr-menu-num:4")
	if a.cfg.UpscaleRes != shaders.Res2K {
		t.Fatalf("expected 2k FSR after menu-num:4, got %q", a.cfg.UpscaleRes)
	}
	p.onMpvAction(a, "aniclitr-menu-back")
	if !p.qmenu.open || p.qmenu.page != menuRoot {
		t.Fatalf("Esc inside sub-menu MUST return to parent root menu, got open=%v page=%v", p.qmenu.open, p.qmenu.page)
	}

	// Enter sub-menu 2 (Quality & Frame-Gen) -> pick Cam Gibi (row 3) + Frame-Gen (row 5)
	p.onMpvAction(a, "aniclitr-menu-num:2")
	if p.qmenu.page != menuQuality {
		t.Fatalf("expected menuQuality sub-menu, got %v", p.qmenu.page)
	}
	p.onMpvAction(a, "aniclitr-menu-num:3")
	if a.cfg.Anime4K != shaders.Clear {
		t.Fatalf("expected Anime4K Clear ('cam'), got %q", a.cfg.Anime4K)
	}
	p.onMpvAction(a, "aniclitr-menu-num:5")
	if a.cfg.FrameGen != shaders.FGSmooth {
		t.Fatalf("expected FrameGen FGSmooth ('akıcı'), got %q", a.cfg.FrameGen)
	}
	p.onMpvAction(a, "aniclitr-menu-back")

	// Enter sub-menu 4 (Playback) -> adjust volume & sleep
	p.onMpvAction(a, "aniclitr-menu-num:4")
	if p.qmenu.page != menuPlayback {
		t.Fatalf("expected menuPlayback, got %v", p.qmenu.page)
	}
	p.qmenu.cur = 0
	p.onMpvAction(a, "aniclitr-menu-left")
	if p.volume != 95 {
		t.Fatalf("expected volume 95 after menu-left, got %d", p.volume)
	}
	p.onMpvAction(a, "aniclitr-menu-num:3")
	if !a.sleep.active() {
		t.Fatalf("expected sleep timer active")
	}

	// Back to root, then Esc on root closes completely
	p.onMpvAction(a, "aniclitr-menu-back")
	if !p.qmenu.open || p.qmenu.page != menuRoot {
		t.Fatalf("expected return to root")
	}
	p.onMpvAction(a, "aniclitr-menu-back")
	if p.qmenu.open {
		t.Fatalf("Esc on root menu should close menu")
	}
}

func TestBrowseDiscordPresenceCoversScreens(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := New(config.Load(), Graphics{})
	if _, act, ok := a.browseDiscordActivity(); !ok || !strings.Contains(act.State, "Ana sayfada") {
		t.Fatalf("unexpected home browse activity: ok=%v act=%+v", ok, act)
	}
	a.stack = append(a.stack, newResults("another"))
	if _, act, ok := a.browseDiscordActivity(); !ok || !strings.Contains(act.State, "another") {
		t.Fatalf("unexpected search browse activity: ok=%v act=%+v", ok, act)
	}
	a.stack = append(a.stack, newAnime(source.Anime{Source: "animecix", ID: "1", Title: "Another", Poster: "https://example.com/a.jpg"}, nil, 1, 0))
	if _, act, ok := a.browseDiscordActivity(); !ok || act.Details != "Another" || act.LargeImage != "https://example.com/a.jpg" {
		t.Fatalf("unexpected anime browse activity: ok=%v act=%+v", ok, act)
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
