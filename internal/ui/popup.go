package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// libPopup edits one anime's place in the library: status, score, lists.
// Every change is saved immediately.

type popRow struct {
	kind string // "status" | "score" | "list" | "new" | "remove"
	val  string
}

type libPopup struct {
	an     source.Anime
	cur    int
	gl     glide
	naming bool
	name   input
	popT   tween // score pop
}

func (a *App) openPopup(an source.Anime) {
	if d, ok := a.details[an.Key()]; ok && d.ID != "" {
		an = merge(an, d)
	}
	a.popup = &libPopup{an: an, name: input{placeholder: "liste adı…"}}
	a.gfx.invalidate()
}

func (p *libPopup) rows(a *App) []popRow {
	var rs []popRow
	for _, s := range store.Statuses {
		rs = append(rs, popRow{"status", s})
	}
	rs = append(rs, popRow{kind: "score"})
	for _, l := range a.lib.ListNames() {
		rs = append(rs, popRow{"list", l})
	}
	rs = append(rs, popRow{kind: "new"})
	if _, ok := a.libItem(p.an); ok {
		rs = append(rs, popRow{kind: "remove"})
	}
	return rs
}

func (p *libPopup) setScore(a *App, v int) {
	v = clamp(v, 0, 10)
	a.libSet(p.an, func(it *store.Item) { it.Score = v })
	if a.motion() {
		p.popT = newTween(350 * time.Millisecond)
	}
}

func (p *libPopup) update(a *App, k tea.KeyMsg) tea.Cmd {
	rs := p.rows(a)
	if p.naming {
		switch k.String() {
		case "esc":
			p.naming = false
		case "enter":
			name := CleanQuery(p.name.Value())
			if a.lib.AddList(name) {
				a.lib.ToggleList(a.libAnime(p.an), name)
				a.notify("“"+name+"” listesi oluşturuldu", "ok")
				a.flash()
			}
			p.naming = false
			p.name.Set("")
		default:
			p.name.handle(k)
		}
		return nil
	}
	it, inLib := a.libItem(p.an)
	switch k.String() {
	case "esc", "q", "a", "+":
		a.popup = nil
		a.gfx.invalidate()
		return nil
	case "up", "k":
		p.cur = max(0, p.cur-1)
	case "down", "j", "tab":
		p.cur = clamp(p.cur+1, 0, len(rs)-1)
	case "left", "h", "-":
		if rs[p.cur].kind == "score" {
			p.setScore(a, it.Score-1)
		}
	case "right", "l":
		if rs[p.cur].kind == "score" {
			p.setScore(a, it.Score+1)
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
		v := int(k.String()[0] - '0')
		if v == 0 {
			v = 10
		}
		p.setScore(a, v)
	case "enter", " ":
		r := rs[p.cur]
		switch r.kind {
		case "status":
			was := it.Status
			if inLib && was != r.val {
				before := it
				a.remember(p.an.Title+" → "+r.val, func(a *App) { a.lib.Restore(before) })
			}
			a.libSet(p.an, func(x *store.Item) { x.Status = r.val })
			a.flash()
			if r.val == store.Completed && (was != store.Completed || !inLib) {
				a.anchorOK = false
				a.celebrate(70)
				a.notify("tebrikler! "+p.an.Title+" tamamlandı — puan ver: 1-9, 0=10", "ok")
			}
		case "score":
			if it.Score == 0 {
				p.setScore(a, 7)
			} else {
				p.setScore(a, 0)
			}
		case "list":
			if a.lib.ToggleList(a.libAnime(p.an), r.val) {
				a.flash()
			}
		case "new":
			p.naming = true
		case "remove":
			if saved, ok := a.libItem(p.an); ok {
				a.lib.Remove(p.an.Key())
				a.remember(p.an.Title+" kitaplıktan çıkarıldı", func(a *App) { a.lib.Restore(saved) })
			}
			p.cur = 0
		}
	}
	return nil
}

// scoreBar draws ★★★★★★★☆☆☆ with the accent gradient; the last star pops.
func scoreBar(score int, acc string, pop float64) string {
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		if i <= score {
			c := gradAt([]string{acc, cAccent2}, float64(i-1)/9)
			if i == score && pop < 1 {
				c = mix(hex("#FFFFFF"), c, easeOutCubic(pop))
			}
			b.WriteString(fg(c.hex()).Render("★"))
		} else {
			b.WriteString(stFaint.Render("☆"))
		}
	}
	return b.String()
}

func (p *libPopup) view(a *App) []string {
	W := 48 // outer width
	inner := W - 4
	acc := a.posters.accent(p.an.Poster, cAccent).hex()
	bs := fg(mix(hex(cFaint), hex(acc), .7).hex())
	it, inLib := a.libItem(p.an)
	rs := p.rows(a)
	p.cur = clamp(p.cur, 0, len(rs)-1)
	sel := p.gl.row(a, p.cur)
	var body []string
	body = append(body, gradient(truncate(p.an.Title, inner), []string{acc, cAccent2}, true))
	sub := "kitaplıkta değil — bir durum seç"
	if inLib {
		sub = "kitaplıkta · " + it.Status
	}
	body = append(body, stMuted.Render(sub), "")
	lastKind := ""
	for i, r := range rs {
		if r.kind != lastKind {
			switch r.kind {
			case "status":
				body = append(body, sectionTitle("durum", inner, false))
			case "score":
				body = append(body, "", sectionTitle("puan", inner, false))
			case "list":
				body = append(body, "", sectionTitle("listeler", inner, false))
			case "remove":
				body = append(body, "")
			}
			lastKind = r.kind
		}
		isSel := i == sel
		bg, bar := "", sp("  ", cText)
		if isSel {
			bg, bar = cBand, sp("▌ ", acc)
		}
		var left, right []span
		switch r.kind {
		case "status":
			mark, mc := "○", cFaint
			if inLib && it.Status == r.val {
				mark, mc = "●", statusColor(r.val)
			}
			left = []span{bar, sp(mark+" ", mc), sp(statusIcon(r.val)+" ", statusColor(r.val)), span{t: r.val, c: cText, bold: isSel}}
		case "score":
			label := "puansız"
			if it.Score > 0 {
				label = fmt.Sprintf("%d/10", it.Score)
			}
			body = append(body, fitRow(bar, "", scoreBar(it.Score, acc, p.popT.p())+"  "+stBold.Render(label), stMuted.Render("‹ › ya da 1-0 "), inner))
			continue
		case "list":
			mark, mc := "□", cMuted
			if it.InList(r.val) {
				mark, mc = "■", acc
			}
			left = []span{bar, sp(mark+" ", mc), sp(listIcon(r.val)+" ", cMuted), span{t: r.val, c: cText, bold: isSel}}
		case "new":
			if p.naming {
				iv := p.name.view(inner-2, true, a.frame)
				body = append(body, iv...)
				continue
			}
			left = []span{bar, sp("+ ", cAccent2), span{t: "yeni liste…", c: cSub, bold: isSel}}
		case "remove":
			left = []span{bar, sp("× ", cErr), span{t: "kitaplıktan çıkar", c: cSub, bold: isSel}}
		}
		body = append(body, row(inner, bg, left, right))
	}
	body = append(body, "", stFaint.Render("enter seç · ↑↓ gez · esc kapat"))
	return boxed(body, W, ic("library")+" kitaplık", bs)
}

// boxed frames body lines in a rounded box of outer width W with a title
// set into the top border.
func boxed(body []string, W int, title string, bs lipgloss.Style) []string {
	inner := W - 4
	t := stText.Bold(true).Render(title)
	rest := max(0, W-4-lipgloss.Width(t)-1)
	out := []string{bs.Render("╭─ ") + t + bs.Render(" "+strings.Repeat("─", rest)+"╮")}
	for _, l := range body {
		out = append(out, bs.Render("│")+" "+fit(l, inner)+" "+bs.Render("│"))
	}
	return append(out, bs.Render("╰"+strings.Repeat("─", W-2)+"╯"))
}

// fitRow renders a selection bar + styled content + right text on a band.
func fitRow(bar span, bg, content, right string, w int) string {
	l := bar.style(bg).Render(bar.t) + content
	pad := w - bar.width() - lipgloss.Width(content) - lipgloss.Width(right)
	return l + fill(max(0, pad), bg) + right
}
