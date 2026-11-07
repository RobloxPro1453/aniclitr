package ui

// Nova: the new interface, switched on with Settings → Görünüm → Arayüz →
// yeni (cfg.UI). Only presentation forks here; data, screens and keys are
// shared with the classic interface, which stays the default.
//
// This file is the shell every screen sits in:
//
//   - top bar: wordmark and crumbs on the left; on the right the live
//     download wave, a chip per source (lit when on, faint when off, warm
//     when slow, red when down) and the bell/menu;
//   - the rule under it doubles as a download progress line;
//   - bottom bar: keycaps instead of bare letters, and notifications as a
//     tinted pill that slides in and fades out (several at once: "+2").
//
// Everything degrades by width: names → short codes → dots.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
)

// nova reports whether the new interface is on.
func (a *App) nova() bool { return a.cfg.UI == "yeni" }

// ---- download pulse: an app-wide speed history for the top bar ----

type dlPulse struct {
	hist []float64
	last time.Time
}

const pulseLen = 24

// samplePulse records the total download speed (on every dlMsg; the
// manager pushes, nothing polls).
func (a *App) samplePulse() {
	if a.dl.Active() == 0 {
		a.pulse.hist = nil
		return
	}
	if time.Since(a.pulse.last) < 500*time.Millisecond && len(a.pulse.hist) > 0 {
		return
	}
	var sp float64
	for _, j := range a.dl.Jobs() {
		if j.State == download.Running {
			sp += j.Speed
		}
	}
	a.pulse.last = time.Now()
	a.pulse.hist = append(a.pulse.hist, sp)
	if len(a.pulse.hist) > pulseLen {
		a.pulse.hist = a.pulse.hist[len(a.pulse.hist)-pulseLen:]
	}
}

// dlProgress is the share of the running downloads done (ok false: none).
func (a *App) dlProgress() (frac float64, n int, ok bool) {
	var done, total int64
	for _, j := range a.dl.Jobs() {
		if j.State == download.Running {
			n++
			done += j.Done
			total += j.Total
		}
	}
	if n == 0 {
		return 0, 0, false
	}
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	return frac, n, true
}

// ---- top bar ----

// shortSource is a source's chip label.
func shortSource(id string) string {
	switch id {
	case "animecix":
		return "cix"
	case "animetr":
		return "tr"
	case "anihub":
		return "hub"
	case "sonanime":
		return "son"
	case "animim":
		return "mim"
	case "hianime":
		return "hi"
	case "animedepo":
		return "arşiv"
	}
	if len(id) > 4 {
		return id[:4]
	}
	return id
}

// sourceChips renders every source: lit in its colour when on and fine,
// warm when slow, red with ✗ when down or changed, faint when switched
// off. compact: coloured dots only (narrow terminals).
func (a *App) sourceChips(compact bool) string {
	var out []string
	for _, p := range source.All() {
		in := p.Info()
		if in.Legacy && !a.cfg.Legacy {
			continue
		}
		label := shortSource(in.ID)
		if compact {
			label = "●"
		}
		c := sourceColor(in.ID)
		mark := ""
		on := a.cfg.Enabled(in.ID, in.Legacy)
		h, probed := a.health[in.ID]
		switch {
		case !on:
			c = cFaint
			if compact {
				label = "○"
			}
		case !probed:
			c = cMuted
		case !h.OK:
			c, mark = cErr, "✗"
		case h.Slow():
			c = cWarn
		}
		s := fg(c).Render(label)
		if on && probed && h.OK && !compact {
			s = fg(c).Bold(true).Render(label)
		}
		if mark != "" && !compact {
			s += fg(cErr).Render(mark)
		}
		out = append(out, s)
	}
	sep := " "
	if !compact {
		sep = stFaint.Render(" · ")
	}
	return strings.Join(out, sep)
}

// novaHeader is the top bar.
func (a *App) novaHeader() string {
	left := " " + wordmark()
	var crumbs []string
	for _, s := range a.stack[1:] {
		if c := s.crumb(); c != "" {
			crumbs = append(crumbs, c)
		}
	}
	for i, c := range crumbs {
		sep := stFaint.Render("  ›  ")
		if i == len(crumbs)-1 {
			left += sep + fg(cAccent).Bold(true).Render(c)
		} else {
			left += sep + stMuted.Render(c)
		}
	}

	wide, mid := a.w >= 140, a.w >= 104
	var right []string
	if frac, n, ok := a.dlProgress(); ok {
		seg := fg(cAccent).Bold(true).Render(fmt.Sprintf("↓ %d", n))
		if mid {
			width := 8
			if wide {
				width = 14
			}
			if len(a.pulse.hist) > 0 {
				seg += " " + sparkline(a.pulse.hist, width, cAccent, cSky)
			}
		}
		if wide && len(a.pulse.hist) > 0 {
			seg += " " + stText.Render(fmtSpeed(a.pulse.hist[len(a.pulse.hist)-1]))
		}
		if frac > 0 {
			seg += " " + stMuted.Render(fmt.Sprintf("%%%d", int(frac*100)))
		}
		right = append(right, seg)
	}
	right = append(right, a.sourceChips(!mid))
	if a.cfg.Legacy {
		right = append(right, pill("ARŞİV", cWarn))
	}
	right = append(right, stFaint.Render("│"), a.headerButtons())
	r := strings.Join(right, "   ") + " "
	lw := a.w - lipgloss.Width(r)
	if lipgloss.Width(left) > lw {
		left = truncate(left, max(0, lw-1))
		if lw < 24 {
			return fit(" "+wordmark(), a.w)
		}
	}
	return fit(left, lw) + r
}

// novaRule is the line under the top bar: a quiet rule that fills with the
// accent as downloads progress.
func (a *App) novaRule() string {
	frac, _, ok := a.dlProgress()
	if !ok || frac <= 0 {
		return stFaint.Render(strings.Repeat("─", a.w))
	}
	n := int(frac * float64(a.w))
	return fg(mix(hex(cAccent), hex(cSky), frac).hex()).Render(strings.Repeat("━", n)) +
		stFaint.Render(strings.Repeat("─", a.w-n))
}

// ---- bottom bar ----

// keycap draws a key as a small raised cap.
func keycap(k string) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(cSurface)).Foreground(lipgloss.Color(cText)).Bold(true).Render(" " + k + " ")
}

// novaHints fits keycap hints into w cells, dropping screen keys from the
// end; işlemler and geri always stay (as in hintLine).
func novaHints(hs []hint, w int) string {
	render := func(hs []hint) string {
		out := " "
		for i, h := range hs {
			if i > 0 {
				out += "  "
			}
			out += keycap(h.key)
			if h.label != "" {
				out += " " + stMuted.Render(h.label)
			}
		}
		return out
	}
	tail := 0
	for i := len(hs) - 1; i >= 0 && (hs[i].key == "?" || hs[i].key == "F1" || hs[i].key == "esc"); i-- {
		tail++
	}
	for n := len(hs) - tail; n >= 0; n-- {
		cut := append(append([]hint{}, hs[:n]...), hs[len(hs)-tail:]...)
		if l := render(cut); lipgloss.Width(l) <= w {
			return l
		}
	}
	return truncate(render(hs[len(hs)-tail:]), w)
}

// ---- notifications ----

type toastCard struct {
	text, kind string
	at, until  time.Time
}

// pushCard queues a notification (the same text again only refreshes it).
func (a *App) pushCard(text, kind string) {
	now := time.Now()
	life := 4 * time.Second
	if kind == "err" || kind == "warn" {
		life = 6 * time.Second // problems stay long enough to read
	}
	for i, c := range a.cards {
		if c.text == text {
			a.cards = append(a.cards[:i], a.cards[i+1:]...)
			break
		}
	}
	a.cards = append(a.cards, toastCard{text: text, kind: kind, at: now, until: now.Add(life)})
	if len(a.cards) > 4 {
		a.cards = a.cards[len(a.cards)-4:]
	}
	if a.toastUntil.Before(now.Add(life)) {
		a.toastUntil = now.Add(life) // keeps the frames coming while one shows
	}
}

func kindStyle(kind string) (c, icon string) {
	switch kind {
	case "err":
		return cErr, "✗"
	case "ok":
		return cOK, "✓"
	case "warn":
		return cWarn, "!"
	}
	return cSky, "•"
}

// cardPill renders the newest live notification as a tinted pill with
// rounded ends, sliding in from the right and fading at the end; older
// ones still showing are counted ("+2").
func (a *App) cardPill(maxW int) string {
	now := time.Now()
	var live []toastCard
	for _, c := range a.cards {
		if now.Before(c.until) {
			live = append(live, c)
		}
	}
	a.cards = live
	if len(live) == 0 || maxW < 16 {
		return ""
	}
	c := live[len(live)-1]
	kc, icon := kindStyle(c.kind)
	fade := 1.0
	if left := c.until.Sub(now); left < 400*time.Millisecond && a.motion() {
		fade = float64(left) / float64(400*time.Millisecond)
	}
	bg := mix(hex(cBg), mix(hex(cSurface), hex(kc), .22), fade).hex()
	ink := mix(hex(cBg), hex(cText), fade).hex()
	icn := mix(hex(cBg), hex(kc), fade).hex()
	more := ""
	if n := len(live) - 1; n > 0 {
		more = fmt.Sprintf("  +%d", n)
	}
	text := truncate(c.text, maxW-8-len(more))
	body := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(icn)).Bold(true).Render(" "+icon+" ") +
		lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(ink)).Render(text+more+" ")
	edge := lipgloss.NewStyle().Foreground(lipgloss.Color(bg))
	pill := edge.Render("▐") + body + edge.Render("▌")
	if t := now.Sub(c.at); t < 260*time.Millisecond && a.motion() {
		n := int(easeOutCubic(float64(t)/float64(260*time.Millisecond)) * float64(lipgloss.Width(pill)))
		pill = ansi.TruncateLeft(pill, lipgloss.Width(pill)-n, "")
	}
	return pill + " "
}

// novaFooter is the bottom bar.
func (a *App) novaFooter() string {
	right := a.cardPill(max(16, a.w/2))
	lw := a.w - lipgloss.Width(right)
	return fit(novaHints(a.footerHints(), lw), lw) + right
}
