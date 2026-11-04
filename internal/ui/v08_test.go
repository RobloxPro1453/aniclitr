package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
)

func TestV08Screens(t *testing.T) {
	dir := os.Getenv("ANICLITR_SNAP_DIR")
	a := showcaseApp(t, 170, 46)
	// onboarding on a fresh install
	Version = "0.8.0"
	defer func() { Version = "dev" }()
	a.cfg.SeenVersion = ""
	a.stack = a.stack[:1]
	o := &onboard{tools: map[string]bool{"mpv": true, "yt-dlp": true, "aria2c": true}}
	a.stack = append(a.stack, o)
	if dir != "" {
		snap(t, a, "onboard-1")
		o.step = 1
		snap(t, a, "onboard-2")
		o.step = 3
		snap(t, a, "onboard-4")
	}
	o.step = len(onboardSteps) - 1
	a.Update(key("enter")) // last step → finish
	if a.cfg.SeenVersion != "0.8.0" || a.top() != a.stack[0] {
		t.Fatal("onboarding should finish and remember the version")
	}
	a.stack = append(a.stack, &onboard{isUpdate: true})
	if dir != "" {
		snap(t, a, "whatsnew")
	}
	a.stack = a.stack[:1]

	// tonight
	d := newDiscover()
	d.tab = tTonight
	a.stack = append(a.stack, d)
	if picks := tonightPicks(a, 60, homeFeed(a)); len(picks) == 0 {
		t.Fatal("tonight should have picks from history")
	}
	if dir != "" {
		time.Sleep(300 * time.Millisecond)
		snap(t, a, "tonight")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if d.budget != 90 {
		t.Fatalf("budget %d", d.budget)
	}

	// offline library with a real file + cover
	vdir := filepath.Join(t.TempDir(), "Sousou no Frieren")
	os.MkdirAll(vdir, 0o755)
	vf := filepath.Join(vdir, "S01E05.mp4")
	os.WriteFile(vf, []byte("x"), 0o644)
	a.dl.Preview(download.Job{ID: 77, Source: "animecix", AnimeID: "100", Title: "Sousou no Frieren", Poster: "test://p0", Season: 1, Episode: 5, State: download.Done, Path: vf, Total: 400 << 20, Quality: "1080p", Finished: time.Now()})
	if len(offlineShelf(a)) != 1 {
		t.Fatal("offline shelf should list the downloaded anime")
	}
	a.stack = a.stack[:1]
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	a.trans = tween{}
	if dir != "" {
		snap(t, a, "offline-shelf")
	}
	a.Update(key("enter"))
	if dir != "" {
		snap(t, a, "offline-show")
	}
	_ = source.Anime{}
}

func TestLiveGenreTab(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" || os.Getenv("ANICLITR_SNAP_DIR") == "" {
		t.Skip("live")
	}
	a := showcaseApp(t, 170, 46)
	d := newDiscover()
	a.stack = append(a.stack, d)
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	drain(t, a, cmd, 0)
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	drain(t, a, cmd, 0)
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	drain(t, a, cmd, 0)
	drain(t, a, d.wantPosters(a), 0)
	time.Sleep(600 * time.Millisecond)
	snap(t, a, "genre-cult")
}
