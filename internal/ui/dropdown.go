package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Two header buttons replace the old row of menu tiles: the bell (◆ n)
// opens the notification centre, the burger (≡) opens the menu. Both are
// dropdown panels hanging from the top-right corner.
//
//	alt+n / F9  notifications      alt+m / F10  menu
//	(on the home screen, when not typing: n / m)

type dropItem struct {
	icon, label, note string
	color             string
	dim               bool // already read
	run               func(a *App) tea.Cmd
	del               func(a *App) // x on the item
}

type dropdown struct {
	kind  string // "menu" | "bell"
	title string
	items func(a *App) []dropItem
	cur   int
	gl    glide
	openT tween
	empty string
}

func (a *App) openDropdown(kind string) {
	if a.drop != nil && a.drop.kind == kind {
		a.drop = nil // the same key closes it
		return
	}
	d := &dropdown{kind: kind}
	switch kind {
	case "menu":
		d.title = "≡ menü"
		d.items = menuItems
	case "bell":
		d.title = "◆ bildirimler"
		d.items = bellItems
		d.empty = "Şimdilik sessiz. Yeni bölümler, inen dosyalar ve uyarılar burada toplanır."
	}
	if a.motion() {
		d.openT = newTween(200 * time.Millisecond)
	}
	a.drop = d
	a.pal, a.popup = nil, nil
}

func menuItems(a *App) []dropItem {
	var out []dropItem
	for _, it := range dockItems() {
		it := it
		out = append(out, dropItem{icon: it.icon, label: it.label, note: it.key, run: func(a *App) tea.Cmd {
			var h *home
			if hh, ok := a.stack[0].(*home); ok {
				h = hh
			}
			if it.label == "Ara" {
				a.stack = a.stack[:1]
				if h != nil {
					h.focus = 0
				}
				return a.tick()
			}
			return it.run(a, h)
		}})
	}
	out = append(out, dropItem{icon: ic("star"), label: "MyAnimeList", note: "bağlan / eşitle", run: func(a *App) tea.Cmd { return a.push(newMalScreen(a)) }},
		dropItem{icon: "●", label: "Kaynak durumu", note: "sağlık", run: func(a *App) tea.Cmd { return tea.Batch(a.push(&sourcesScreen{}), a.checkHealth()) }},
		dropItem{icon: ic("exit"), label: "Çık", note: "ctrl+c", run: func(a *App) tea.Cmd { return tea.Quit }})
	return out
}

// ---- notifications ----

type notice struct {
	key    string // de-duplication key
	text   string
	kind   string // "new" | "ok" | "err" | "warn"
	at     time.Time
	read   bool
	action func(a *App) tea.Cmd
}

// addNotice records a notification once (by key); returns false if known.
func (a *App) addNotice(n notice) bool {
	for _, x := range a.notices {
		if x.key == n.key {
			return false
		}
	}
	if n.at.IsZero() {
		n.at = time.Now()
	}
	a.notices = append([]notice{n}, a.notices...)
	if len(a.notices) > 60 {
		a.notices = a.notices[:60]
	}
	a.bellT = newTween(1500 * time.Millisecond) // the bell glows for a moment
	return true
}

func (a *App) unread() int {
	n := 0
	for _, x := range a.notices {
		if !x.read {
			n++
		}
	}
	return n
}

func bellItems(a *App) []dropItem {
	var out []dropItem
	for i := range a.notices {
		n := a.notices[i]
		icon, c := "•", cSub
		switch n.kind {
		case "new":
			icon, c = "◆", cAccent
		case "ok":
			icon, c = "✓", cOK
		case "err":
			icon, c = "✗", cErr
		case "warn":
			icon, c = "!", cWarn
		}
		key := n.key
		out = append(out, dropItem{icon: icon, color: c, label: n.text, note: agoLong(n.at), dim: n.read, run: n.action,
			del: func(a *App) {
				for j, x := range a.notices {
					if x.key == key {
						a.notices = append(a.notices[:j], a.notices[j+1:]...)
						return
					}
				}
			}})
	}
	return out
}

// ---- dropdown behaviour ----

func (d *dropdown) update(a *App, k tea.KeyMsg) tea.Cmd {
	items := d.items(a)
	switch k.String() {
	case "esc", "q":
		a.closeDropdown()
		return nil
	case "alt+m", "f10", "alt+n", "f9":
		a.closeDropdown()
		return nil
	case "up", "k":
		d.cur = max(0, d.cur-1)
	case "down", "j", "tab":
		d.cur = clamp(d.cur+1, 0, len(items)-1)
	case "x", "delete":
		if d.cur < len(items) && items[d.cur].del != nil {
			items[d.cur].del(a)
			d.cur = clamp(d.cur, 0, len(items)-2)
		}
	case "c":
		if d.kind == "bell" {
			saved := a.notices
			a.notices = nil
			a.remember("bildirimler temizlendi", func(a *App) { a.notices = append(saved, a.notices...) })
		}
	case "enter", " ":
		if d.cur < len(items) && items[d.cur].run != nil {
			run := items[d.cur].run
			a.closeDropdown()
			a.flash()
			return run(a)
		}
		a.closeDropdown() // nothing to open: just dismiss
	}
	return nil
}

func (a *App) closeDropdown() {
	if a.drop != nil && a.drop.kind == "bell" {
		for i := range a.notices {
			a.notices[i].read = true // seen once the panel closes
		}
	}
	a.drop = nil
	a.gfx.invalidate()
}

// draw hangs the panel from the top-right corner of the body.
func (d *dropdown) draw(a *App, lines []string) []string {
	items := d.items(a)
	w := min(60, a.w-4)
	if d.kind == "menu" {
		w = min(40, a.w-4)
	}
	inner := w - 2
	bs := fg(mix(hex(cFaint), hex(cAccent), .7).hex())
	maxRows := min(len(lines)-4, 14)
	var box []string
	title := stText.Bold(true).Render(d.title)
	if d.kind == "bell" && len(items) > 0 {
		title += stMuted.Render(fmt.Sprintf("  %d", len(items)))
	}
	rest := max(0, w-5-lipgloss.Width(title))
	box = append(box, bs.Render("╭─ ")+title+bs.Render(" "+strings.Repeat("─", rest)+"╮"))
	if len(items) == 0 {
		for _, l := range wrap(d.empty, inner-4) {
			box = append(box, bs.Render("│")+"  "+fit(stMuted.Render(l), inner-2)+bs.Render("│"))
		}
	}
	d.cur = clamp(d.cur, 0, len(items)-1)
	off := 0
	if d.cur >= maxRows {
		off = d.cur - maxRows + 1
	}
	band := d.gl.row(a, d.cur)
	for i := off; i < len(items) && i < off+maxRows; i++ {
		it := items[i]
		sel := i == band
		bg, bar := "", sp("  ", cText)
		tc := cText
		if it.dim {
			tc = cSub
		}
		if sel {
			bg, bar, tc = cBand, sp("▌ ", cAccent), cText
		}
		ic := it.color
		if ic == "" {
			ic = cMuted
		}
		right := []span{}
		if it.note != "" {
			right = append(right, sp(it.note+" ", cFaint))
			if sel {
				right[0] = sp(it.note+" ", cMuted)
			}
		}
		box = append(box, bs.Render("│")+row(inner, bg, []span{bar, sp(it.icon+" ", ic), span{t: it.label, c: tc, bold: sel || !it.dim && d.kind == "bell"}}, right)+bs.Render("│"))
	}
	foot := stText.Bold(true).Render("↑↓") + stMuted.Render(" seç  ") + stText.Bold(true).Render("enter") + stMuted.Render(" aç  ")
	if d.kind == "bell" {
		foot += stText.Bold(true).Render("x") + stMuted.Render(" sil  ") + stText.Bold(true).Render("c") + stMuted.Render(" temizle  ")
	}
	foot += stText.Bold(true).Render("esc") + stMuted.Render(" kapat")
	box = append(box, bs.Render("│")+" "+fit(foot, inner-1)+bs.Render("│"))
	box = append(box, bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	// slides down from under the header
	y := 0
	if d.openT.active() {
		y = int(float64(-len(box)) * (1 - easeOutCubic(d.openT.p())))
	}
	return overlayAt(lines, box, a.w-w-1, y, a.w)
}

// headerButtons renders "◆ 2  ≡" for the header's right edge.
func (a *App) headerButtons() string {
	n := a.unread()
	bell := stMuted.Render("◇")
	if n > 0 {
		c := cAccent
		if a.bellT.active() {
			// a soft pulse when something new arrives
			t := a.bellT.p()
			c = mix(hex("#FFFFFF"), hex(cAccent), t).hex()
		}
		bell = fg(c).Bold(true).Render(fmt.Sprintf("◆ %d", n))
	}
	if a.drop != nil && a.drop.kind == "bell" {
		bell = pill(strings.TrimSpace(ansiPlain(bell)), cAccent)
	}
	menu := stSub.Render("≡")
	if a.drop != nil && a.drop.kind == "menu" {
		menu = pill("≡", cAccent)
	}
	return bell + "  " + menu
}

func ansiPlain(s string) string { return ansi.Strip(s) }

// openList shows any list of choices in the dropdown panel.
func (a *App) openList(title string, items []dropItem) {
	d := &dropdown{kind: "list", title: title, items: func(*App) []dropItem { return items }, empty: "boş"}
	if a.motion() {
		d.openT = newTween(200 * time.Millisecond)
	}
	a.drop = d
	a.pal, a.popup = nil, nil
}
