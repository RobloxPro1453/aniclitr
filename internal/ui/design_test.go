package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Hint keys turn into the presses they stand for; movement pairs do not.
func TestParseKey(t *testing.T) {
	for _, s := range []string{"enter", "esc", "a", "x", "/", "+", "ctrl+b", "ctrl+z", "alt+a", "alt+m", "tab", "f1"} {
		k, ok := parseKey(s)
		if !ok || k.String() != s {
			t.Errorf("%q → %q (ok=%v)", s, k.String(), ok)
		}
	}
	if k, ok := parseKey("tab [ ]"); !ok || k.String() != "tab" {
		t.Errorf("tab [ ] should mean tab, got %q", k.String())
	}
	if k, ok := parseKey("space"); !ok || k.Type != tea.KeySpace {
		t.Errorf("space → %v", k)
	}
	for _, s := range []string{"← →", "↑↓", "1-6", "tab 1-6", ""} {
		if k, ok := parseKey(s); ok && s != "tab 1-6" {
			t.Errorf("%q should not be runnable, got %q", s, k.String())
		}
	}
}

// ? lists the screen's actions with their keys; choosing one runs it.
func TestActionsRunTheChosenKey(t *testing.T) {
	a := showcaseApp(t, 140, 40)
	f := newFeedScreen()
	a.stack = append(a.stack, f)
	a.Update(key("?"))
	if a.drop == nil || a.drop.kind != "actions" {
		t.Fatal("? should open işlemler")
	}
	items := a.drop.items(a)
	want := -1
	for i, it := range items {
		if it.note == "f" {
			want = i
		}
		if it.group != "" && it.run != nil {
			t.Fatal("group rows must not run")
		}
	}
	if want < 0 {
		t.Fatalf("feed's f action missing: %+v", items)
	}
	for a.drop.cur < want {
		a.Update(key("down"))
	}
	_, cmd := a.Update(key("enter"))
	drain(t, a, cmd, 0)
	if !f.mine || a.drop != nil {
		t.Fatalf("f should have run (mine=%v) and the panel closed", f.mine)
	}
}

// Keşfet is two steps: the section menu, then a section page; esc walks
// back one step at a time.
func TestDiscoverSteps(t *testing.T) {
	a := showcaseApp(t, 140, 40)
	d := newDiscover()
	d.schedOK = true
	a.stack = append(a.stack, d)
	if !d.menu || !strings.Contains(ansi.Strip(a.View()), "Bir bölüm seç") {
		t.Fatal("keşfet should open on its section menu")
	}
	a.Update(key("down"))
	a.Update(key("down"))
	a.Update(key("enter"))
	if d.menu || d.tab != tSeason {
		t.Fatalf("enter should open the third section, tab=%d menu=%v", d.tab, d.menu)
	}
	if c := d.crumb(); c != "keşfet › bu sezon" {
		t.Fatalf("crumb %q", c)
	}
	a.Update(key("esc"))
	if !d.menu || a.top() != screen(d) {
		t.Fatal("esc in a section returns to the section menu")
	}
	a.Update(key("esc"))
	if a.top() == screen(d) {
		t.Fatal("esc on the menu leaves keşfet")
	}
}

// The footer never lists more than three screen keys, and always offers
// işlemler (and the way back off the root).
func TestFooterIsShort(t *testing.T) {
	a := showcaseApp(t, 200, 50)
	for _, s := range []screen{newLibrary(), newFeedScreen(), newDownloads(), newSettings()} {
		a.stack = append(a.stack[:1], s)
		hs := a.footerHints()
		if len(hs) > 5 {
			t.Errorf("%T: %d footer hints", s, len(hs))
		}
		if hs[len(hs)-2].label != "işlemler" || hs[len(hs)-1].key != "esc" {
			t.Errorf("%T: footer should end with işlemler · geri: %+v", s, hs)
		}
	}
}

// Home's menu reaches every main screen without a shortcut.
func TestHomeMenuOpensScreens(t *testing.T) {
	a := showcaseApp(t, 140, 40)
	h := a.stack[0].(*home)
	h.focus = 1
	for _, id := range []string{"feed", "library", "downloads", "settings"} {
		a.stack = a.stack[:1]
		_, items := h.selItem(a)
		for i, it := range items {
			if it.id == id {
				h.mCur = i
			}
		}
		_, cmd := a.Update(key("enter"))
		drain(t, a, cmd, 0)
		if len(a.stack) < 2 {
			t.Errorf("menu item %s opened nothing", id)
		}
	}
}
