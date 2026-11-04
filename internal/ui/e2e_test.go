package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/tty"
)

// emuWriter feeds the renderer's bytes into an emulated terminal.
type emuWriter struct{ e *tty.Emulator }

func (w emuWriter) Write(p []byte) (int, error) { w.e.Feed(string(p)); return len(p), nil }

// A whole session — home with the animated hero, palette, theme preview,
// library list/wall, popup, confetti, screensaver, resize — rendered through
// the real cell renderer into an emulated terminal. After every step the
// emulated screen must equal the frame the app meant to show: no drift, no
// leftovers from previous screens.
func TestEndToEndNoDrift(t *testing.T) {
	a := showcaseApp(t, 160, 48)
	a.cfg.Motion = true
	emu := tty.NewEmulator(160, 48)
	a.selfPaintSync(emuWriter{emu})
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	step := 0
	verify := func(label string) {
		step++
		if bad := emu.Mismatch(a.pt.front, nil); len(bad) > 0 {
			t.Fatalf("step %d (%s): %v", step, label, bad)
		}
	}
	tick := func(n int) {
		for i := 0; i < n; i++ {
			time.Sleep(15 * time.Millisecond)
			a.Update(tickMsg(time.Now()))
		}
	}
	keys := func(label string, ks ...tea.KeyMsg) {
		for _, k := range ks {
			a.Update(k)
			tick(2)
			verify(label)
		}
	}
	tick(20)
	verify("hero intro")
	keys("typing", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fri")})
	keys("menu", key("down"), key("right"), key("right"))
	keys("shelf", key("down"), key("right"), key("right"), key("left"))
	keys("feed", key("down"), key("down"), key("down"), key("up"))
	keys("palette", tea.KeyMsg{Type: tea.KeyCtrlK}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tema")}, key("down"), key("down"))
	tick(30) // theme morph
	verify("theme preview")
	keys("palette close", key("esc"))
	tick(30)
	verify("theme restored")
	keys("library", tea.KeyMsg{Type: tea.KeyCtrlB})
	tick(20)
	keys("library nav", key("down"), key("right"), key("down"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}, key("right"))
	keys("popup", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, key("down"), key("esc"))
	a.celebrate(90)
	tick(25)
	verify("confetti")
	keys("back home", key("esc"), key("esc"), key("esc"))
	a.saver, a.saverAt = true, time.Now()
	tick(20)
	verify("screensaver")
	keys("wake", key("x"))
	// resize: full repaint on a differently sized terminal
	emu2 := tty.NewEmulator(120, 40)
	a.pt.sync = emuWriter{emu2}
	emu = emu2
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tick(5)
	verify("after resize")
}
