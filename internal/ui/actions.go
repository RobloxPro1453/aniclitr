package ui

// actions.go — "işlemler": one key (?) lists everything the current screen
// can do, with its shortcut beside it, and runs the chosen one. The footer
// then needs only the two or three keys that matter most; nothing has to
// be memorised, and the shortcuts are learnt by seeing them here.

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

var namedKeys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
	"space": tea.KeySpace, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
	"delete": tea.KeyDelete, "backspace": tea.KeyBackspace,
	"f1": tea.KeyF1, "f2": tea.KeyF2, "f3": tea.KeyF3, "f4": tea.KeyF4, "f9": tea.KeyF9, "f10": tea.KeyF10,
	"↑": tea.KeyUp, "↓": tea.KeyDown, "←": tea.KeyLeft, "→": tea.KeyRight,
}

// parseKey turns a hint's key text into the key press it stands for.
// "tab [ ]" and "+ -" mean their first key; ranges like "1-6" or pairs
// like "← →" that describe movement are not runnable.
func parseKey(s string) (tea.KeyMsg, bool) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return tea.KeyMsg{}, false
	}
	k := f[0]
	if len(f) > 1 && utf8.RuneCountInString(f[1]) == 1 && strings.ContainsAny(k+f[1], "←→↑↓") {
		return tea.KeyMsg{}, false // a direction pair: navigation, not an action
	}
	if t, ok := namedKeys[k]; ok {
		return tea.KeyMsg{Type: t}, true
	}
	if strings.HasPrefix(k, "ctrl+") && len(k) == 6 && k[5] >= 'a' && k[5] <= 'z' {
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(k[5]-'a')}, true
	}
	if strings.HasPrefix(k, "alt+") && utf8.RuneCountInString(k) == 5 {
		r, _ := utf8.DecodeLastRuneInString(k)
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}, true
	}
	if utf8.RuneCountInString(k) == 1 {
		r, _ := utf8.DecodeRuneInString(k)
		if r == ' ' {
			return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}, true
	}
	return tea.KeyMsg{}, false
}

// press replays a key through the whole Update (globals and the screen).
func replayKey(k tea.KeyMsg) tea.Cmd { return func() tea.Msg { return k } }

// globalActions are reachable from every screen.
func globalActions() []hint {
	return []hint{
		{"alt+m", "menü: bütün ekranlar"},
		{"alt+n", "bildirimler"},
		{"ctrl+k", "komut paleti"},
		{"ctrl+b", "kitaplık"},
		{"ctrl+e", "keşfet"},
		{"ctrl+d", "indirilenler"},
		{"ctrl+o", "ayarlar"},
		{"ctrl+t", "tema"},
		{"ctrl+z", "son silmeyi geri al"},
		{"alt+a", "arşiv modu"},
		{"ctrl+l", "ekranı baştan çiz"},
	}
}

// openActions shows the işlemler panel for the top screen.
func (a *App) openActions() {
	var items []dropItem
	add := func(h hint) {
		it := dropItem{label: h.label, note: h.key, icon: "·", color: cFaint}
		if k, ok := parseKey(h.key); ok {
			it.icon, it.color = "›", cAccent
			it.run = func(*App) tea.Cmd { return replayKey(k) }
		} else {
			it.dim = true
		}
		items = append(items, it)
	}
	items = append(items, dropItem{group: "bu ekran"})
	for _, h := range a.top().hints() {
		if h.key == "?" || h.label == "" {
			continue
		}
		add(h)
	}
	items = append(items, dropItem{group: "her yerde"})
	for _, h := range globalActions() {
		add(h)
	}
	items = append(items, dropItem{icon: "?", color: cMuted, label: "bütün kısayollar", note: "F1",
		run: func(a *App) tea.Cmd { a.help = true; return nil }})
	a.openList("işlemler", items)
	a.drop.kind = "actions"
	a.drop.cur = dropNext(items, -1, 1)
}

// footerHints is what the footer shows: the screen's first keys, then
// işlemler and the way back. Everything else is one ? away.
func (a *App) footerHints() []hint {
	var hs []hint
	for _, h := range a.top().hints() {
		if h.key == "?" || h.key == "esc" || strings.HasPrefix(h.key, "esc") {
			continue
		}
		hs = append(hs, h)
		if len(hs) == 3 {
			break
		}
	}
	key := "?"
	if t, ok := a.top().(typer); ok && t.typing() {
		key = "F1" // ? would land in the text field
	}
	hs = append(hs, hint{key, "işlemler"})
	if len(a.stack) > 1 {
		hs = append(hs, hint{"esc", "geri"})
	}
	return hs
}
