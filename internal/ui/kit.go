package ui

// kit.go — the design system. docs/DESIGN.md explains the rules; this file is
// their single implementation. Screens compose these pieces instead of
// inventing their own layout, so a redesign happens here, once.
//
//	header      wordmark ╱ crumbs                       status · ◆ n ≡
//	─────────────────────────────────────────────────────────────────
//	pageHead    Title                                         meta
//	            one line that says what this page is for
//
//	body        primary list (menuRows / row)   │   detail (split)
//
//	footer      2–3 keys · ? işlemler · esc geri

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// kitPad is the left/right gutter of every page body.
const kitPad = 2

// kitGap is the space between two panels.
const kitGap = 1

// pageHead is the top of every page: the title (the one gradient on the
// page), what the page is for, and an optional right-aligned meta.
// Tall terminals get two rows, short ones a single row.
func pageHead(title, sub, meta string, w, h int) []string {
	return indent(pageHeadIn(title, sub, meta, w-kitPad*2, h))
}

// pageHeadIn is pageHead for screens that indent their body themselves.
// sub and meta may come pre-styled; plain text is set in the muted tone.
func pageHeadIn(title, sub, meta string, inner, h int) []string {
	quiet := func(s string) string {
		if strings.Contains(s, "[") {
			return s
		}
		return stMuted.Render(s)
	}
	t := gradient(title, brand, true)
	m := ""
	if meta != "" {
		m = quiet(meta)
	}
	if h < 30 || sub == "" {
		left := t
		if sub != "" {
			left += stFaint.Render("  ·  ") + quiet(sub)
		}
		return []string{"", row2(left, m, inner), ""}
	}
	return []string{"", row2(t, m, inner), truncate(quiet(sub), inner), ""}
}

// label is a small section label ("DEVAM ET"): Turkish upper case, quiet.
func label(s string, focused bool) string {
	s = strings.ToUpperSpecial(unicode.TurkishCase, s)
	if focused {
		return stText.Bold(true).Render(s)
	}
	return stMuted.Render(s)
}

// split decides the two columns of a page: the list gets what is left, the
// detail column appears only when there is room for it to be useful.
func split(w int) (lw, rw int) {
	if w < 100 {
		return w, 0
	}
	rw = clamp(w*2/5, 36, 56)
	return w - rw - kitGap, rw
}

// pane is one boxed region: a title in the border, content drawn for the
// inner size (w-4 × h-2).
type pane struct {
	title, tag string
	focused    bool
	acc        string
	draw       func(w, h int) []string
}

// panes lays out a list panel and, when rw > 0, its detail panel.
func panes(left pane, lw int, right pane, rw, h int) []string {
	box := func(p pane, w int) []string {
		acc := p.acc
		if acc == "" {
			acc = cAccent
		}
		return panel(p.title, p.draw(w-4, h-2), w, h, p.focused, acc, p.tag)
	}
	if rw == 0 || right.draw == nil {
		return box(left, lw)
	}
	return hjoin(kitGap, box(left, lw), box(right, rw))
}

// columns joins a list and its detail column with the kit gap.
func columns(left []string, lw int, right []string, rw, h int) []string {
	if rw == 0 {
		return block(left, lw, h)
	}
	return hjoin(kitGap, block(left, lw, h), block(right, rw, h))
}

// indent shifts body lines right by the page gutter.
func indent(lines []string) []string {
	pad := strings.Repeat(" ", kitPad)
	for i, l := range lines {
		lines[i] = pad + l
	}
	return lines
}

// ---- menus: the step-by-step lists (home, keşfet sections) ----

type mItem struct {
	group string // non-empty: a section label row, not selectable
	id    string // what the item stands for (screens use it for previews)
	icon  string
	label string
	desc  string // muted, after the label
	meta  string // right edge
	metaC string
	run   func(a *App) tea.Cmd // what enter does; nil = nothing
}

func (m mItem) selectable() bool { return m.group == "" }

// menuMove moves the cursor to the next selectable item in dir.
func menuMove(items []mItem, cur, dir int) int {
	for i := cur + dir; i >= 0 && i < len(items); i += dir {
		if items[i].selectable() {
			return i
		}
	}
	return cur
}

// menuFirst is the first selectable index.
func menuFirst(items []mItem) int {
	for i, it := range items {
		if it.selectable() {
			return i
		}
	}
	return 0
}

// menuRows draws a vertical menu: icon, label, description, right meta.
// The label column is as wide as the widest label so descriptions line
// up. band is the (gliding) selected row, -1 for none.
func menuRows(items []mItem, band int, w int) []string {
	groupW := func(from int) int {
		lw := 0
		for _, it := range items[from:] {
			if !it.selectable() && lw > 0 {
				break
			}
			if it.selectable() {
				lw = max(lw, lipgloss.Width(it.label))
			}
		}
		return min(lw+3, w/2)
	}
	lw := groupW(0)
	var out []string
	for i, it := range items {
		if !it.selectable() {
			lw = groupW(i + 1)
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, "  "+label(it.group, false))
			continue
		}
		sel := i == band
		bg, bar, tc, icC := "", sp("  ", cText), cText, cMuted
		if sel {
			bg, bar, tc, icC = cBand, sp("▌ ", cAccent), cText, cAccent
		}
		left := []span{bar}
		if it.icon != "" {
			left = append(left, sp(it.icon+" ", icC))
		}
		lab := it.label
		if lipgloss.Width(lab) > lw-1 {
			lab = truncate(lab, lw-1)
		}
		if pad := lw - lipgloss.Width(lab); pad > 0 {
			lab += strings.Repeat(" ", pad)
		}
		left = append(left, span{t: lab, c: tc, bold: sel})
		if it.desc != "" {
			dc := cMuted
			if sel {
				dc = cSub
			}
			// the description gives way to the meta, never touches it
			room := w - 4 - lw - lipgloss.Width(it.meta) - 3
			if it.icon != "" {
				room -= lipgloss.Width(it.icon) + 1
			}
			if room >= 8 {
				left = append(left, sp(truncate(it.desc, room), dc))
			}
		}
		var right []span
		if it.meta != "" {
			c := it.metaC
			if c == "" {
				c = cMuted
			}
			right = append(right, sp(it.meta+" ", c))
		}
		line := row(w, bg, left, right)
		if sel {
			line = anchor() + line
		}
		out = append(out, line)
	}
	return out
}

// menuRowOf maps a selectable index to its drawn row (group labels and
// the blank line before them take rows too).
func menuRowOf(items []mItem, idx int) int {
	r := 0
	for i := 0; i < idx && i < len(items); i++ {
		if !items[i].selectable() && i > 0 {
			r++
		}
		r++
	}
	if idx < len(items) && !items[idx].selectable() && idx > 0 {
		r++
	}
	return r
}

// emptyState fills a region with a short centred message.
func emptyState(title, text string, w, h int) []string {
	body := []string{stText.Bold(true).Render(title)}
	for _, l := range wrap(text, min(w-4, 56)) {
		body = append(body, stMuted.Render(l))
	}
	out := make([]string, 0, h)
	top := max(0, (h-len(body))/3)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range body {
		out = append(out, center(l, w))
	}
	return block(out, w, h)
}

// pageHeadCol is pageHead for a centred column of width cw: the title sits
// on the column's left edge, not in the middle of the screen.
func pageHeadCol(title, sub, meta string, cw, w, h int) []string {
	out := pageHeadIn(title, sub, meta, cw, h)
	for i, l := range out {
		out[i] = center(fit(l, cw), w)
	}
	return out
}
