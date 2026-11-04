package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// First run walks the whole tour; on the PATH step `y` adds aniclitr (to the
// fake PATH) and moves on, and the answer is remembered.
func TestTourPathStep(t *testing.T) {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	Version = "9.9.9"
	defer func() { Version = "dev" }()
	a.cfg.SeenVersion = ""
	a.maybeOnboard()
	o, ok := a.top().(*onboard)
	if !ok || o.isUpdate {
		t.Fatal("first run should open the tour")
	}
	for o.step != obPath {
		a.Update(key("enter"))
	}
	fp := a.path.(*fakePath)
	a.Update(key("y"))
	if !fp.on || fp.adds != 1 || !a.cfg.PathAsked || o.step != obKeys {
		t.Fatalf("y should add to PATH and move on: on=%v adds=%d asked=%v step=%d", fp.on, fp.adds, a.cfg.PathAsked, o.step)
	}
	a.Update(key("enter")) // last step finishes
	if a.cfg.SeenVersion != "9.9.9" || a.top() != a.stack[0] {
		t.Fatal("finishing the tour should remember the version and close it")
	}
}

// `n` on the PATH step leaves PATH alone; settings can still do it later,
// and the tour can be replayed from settings without touching SeenVersion.
func TestTourFromSettings(t *testing.T) {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	o := newOnboard(a, false)
	o.step = obPath
	a.stack = append(a.stack, o)
	a.Update(key("n"))
	fp := a.path.(*fakePath)
	if fp.on || !a.cfg.PathAsked || o.step != obKeys {
		t.Fatal("n should skip without adding")
	}
	a.stack = a.stack[:1]

	st := newSettings()
	a.stack = append(a.stack, st)
	pick := func(label string) {
		for i, it := range st.items {
			if it.label == label {
				st.cur = i
				return
			}
		}
		t.Fatalf("no setting %q", label)
	}
	pick("Terminalden aç")
	a.Update(key("enter"))
	if !fp.on {
		t.Fatal("settings should add to PATH")
	}
	a.Update(key("enter"))
	if fp.on {
		t.Fatal("and take it off again")
	}
	seen := a.cfg.SeenVersion
	pick("Tanıtım turu")
	a.Update(key("enter"))
	tour, ok := a.top().(*onboard)
	if !ok || !tour.tour || tour.step != 0 {
		t.Fatal("settings should start the tour from the top")
	}
	a.Update(key("esc"))
	if a.top() != screen(st) || a.cfg.SeenVersion != seen {
		t.Fatal("leaving a replayed tour returns to settings and changes nothing")
	}
}
