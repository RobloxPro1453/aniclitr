package ui

import (
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLiveDiscover(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" || os.Getenv("ANICLITR_SNAP_DIR") == "" {
		t.Skip("set ANICLITR_LIVE=1 and ANICLITR_SNAP_DIR")
	}
	a := showcaseApp(t, 180, 50)
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	drain(t, a, cmd, 0)
	a.trans = tween{}
	d := a.top().(*discover)
	if d.schedErr != nil {
		t.Fatal(d.schedErr)
	}
	// posters for the selection
	drain(t, a, d.wantPosters(a), 0)
	time.Sleep(700 * time.Millisecond)
	snap(t, a, "discover-calendar")
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	drain(t, a, cmd, 0)
	drain(t, a, d.wantPosters(a), 0)
	time.Sleep(700 * time.Millisecond)
	snap(t, a, "discover-season")
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	drain(t, a, cmd, 0)
	d.rolling = tween{}
	time.Sleep(700 * time.Millisecond)
	snap(t, a, "discover-random")
	a.stack = a.stack[:1]
	a.Update(appSchedMsg{items: d.sched})
	time.Sleep(600 * time.Millisecond)
	snap(t, a, "home-calendar")
}
