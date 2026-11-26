package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/player"
	"aniclitr/internal/shaders"
)

// mpvMenuState drives the interactive, solid-card Nova Command Center inside mpv
// (`m` / `Tab` / `Alt+4`). Uses multi-layer ASS vector graphics (\p1...\p0) for a
// crisp opaque dark card, sakura accent bar, highlighted selection row, live
// video resolution telemetry, and instant repeatable ↑↓/←→ controls.
type mpvMenuState struct {
	open bool
	cur  int
}

const mpvMenuCount = 10

var speedCycle = []float64{0.75, 1.0, 1.25, 1.5, 1.75, 2.0}

func (p *playing) openQuickMenu(a *App) {
	if p.subs.open {
		p.closeSubs()
	}
	p.qmenu.open = true
	p.syncQuickMenu(a)
}

func (p *playing) closeQuickMenu() {
	if !p.qmenu.open {
		return
	}
	p.qmenu.open = false
	if p.sess != nil {
		p.sess.HideQuickMenu()
	}
}

func (p *playing) syncQuickMenu(a *App) {
	if p.sess == nil || !p.qmenu.open {
		return
	}
	p.sess.ShowQuickMenu(p.buildQuickMenuASS(a))
}

func (p *playing) applyUpscale(a *App, prof string) tea.Cmd {
	prof = shaders.Normalize(prof)
	a.cfg.Anime4K = prof
	_ = a.cfg.Save()
	if p.sess != nil {
		p.sess.SetShaders(prof)
	}
	a.notify("Görüntü yükseltme: "+shaders.ShortLabel(prof), "ok")
	p.syncQuickMenu(a)
	return a.tick()
}

func (p *playing) stepSpeed(a *App, dir int) tea.Cmd {
	curSp := a.playSpeed
	if curSp <= 0 {
		curSp = 1.0
	}
	idx := 1 // default 1.0x
	bestDiff := 99.0
	for i, sp := range speedCycle {
		if d := math.Abs(sp - curSp); d < bestDiff {
			bestDiff = d
			idx = i
		}
	}
	nextIdx := (idx + dir + len(speedCycle)) % len(speedCycle)
	nextSp := speedCycle[nextIdx]
	a.playSpeed = nextSp
	if p.sess != nil {
		p.sess.SetSpeed(nextSp)
	}
	p.syncQuickMenu(a)
	return nil
}

func (p *playing) stepVolume(a *App, delta int) tea.Cmd {
	if p.volume <= 0 && !p.muted {
		p.volume = 100
	}
	v := clamp(p.volume+delta, 0, 130)
	p.volume = v
	p.muted = false
	if p.sess != nil {
		p.sess.SetVolume(v)
	}
	p.syncQuickMenu(a)
	return nil
}

func (p *playing) stepSleep(a *App, dir int) tea.Cmd {
	var lbl string
	if dir < 0 {
		// Step backward through the 6 sleep modes.
		for i := 0; i < 5; i++ {
			lbl = a.sleep.cycle()
		}
	} else {
		lbl = a.sleep.cycle()
	}
	a.notify("☾ uyku zamanlayıcısı: "+lbl, "ok")
	p.syncQuickMenu(a)
	return a.tick()
}

func (p *playing) pickQuickMenu(a *App, idx int) tea.Cmd {
	if idx < 0 || idx >= mpvMenuCount {
		return nil
	}
	p.qmenu.cur = idx
	switch idx {
	case 0, 1, 2, 3:
		return p.applyUpscale(a, shaders.Profiles[idx])
	case 4:
		p.closeQuickMenu()
		return p.openSubs(a, true)
	case 5:
		cmd := p.adjustSubDelay(a, 0, true)
		p.syncQuickMenu(a)
		return cmd
	case 6:
		p.muted = !p.muted
		if p.sess != nil {
			p.sess.ToggleMute()
		}
		p.syncQuickMenu(a)
		return nil
	case 7:
		return p.stepSpeed(a, +1)
	case 8:
		return p.stepSleep(a, +1)
	case 9:
		p.closeQuickMenu()
		if p.sess != nil {
			p.sess.Next()
		}
		return nil
	}
	return nil
}

func (p *playing) adjustQuickMenu(a *App, dir int) tea.Cmd {
	switch p.qmenu.cur {
	case 0, 1, 2, 3:
		next := shaders.Next(a.cfg.Anime4K)
		if dir < 0 {
			next = shaders.Prev(a.cfg.Anime4K)
		}
		for i, pr := range shaders.Profiles {
			if pr == next {
				p.qmenu.cur = i
				break
			}
		}
		return p.applyUpscale(a, next)
	case 5:
		cmd := p.adjustSubDelay(a, float64(dir)*0.1, false)
		p.syncQuickMenu(a)
		return cmd
	case 6:
		return p.stepVolume(a, dir*5)
	case 7:
		return p.stepSpeed(a, dir)
	case 8:
		return p.stepSleep(a, dir)
	case 9:
		p.closeQuickMenu()
		if p.sess != nil {
			if dir < 0 {
				p.sess.Prev()
			} else {
				p.sess.Next()
			}
		}
		return nil
	}
	return nil
}

func (p *playing) handleQuickMenuAction(a *App, action string) (tea.Cmd, bool) {
	switch {
	case action == "aniclitr-menu":
		if p.qmenu.open {
			p.closeQuickMenu()
		} else {
			p.openQuickMenu(a)
		}
		return nil, true
	case action == "aniclitr-menu-close":
		p.closeQuickMenu()
		return nil, true
	case action == "aniclitr-menu-up":
		p.qmenu.cur = (p.qmenu.cur - 1 + mpvMenuCount) % mpvMenuCount
		p.syncQuickMenu(a)
		return nil, true
	case action == "aniclitr-menu-down":
		p.qmenu.cur = (p.qmenu.cur + 1) % mpvMenuCount
		p.syncQuickMenu(a)
		return nil, true
	case action == "aniclitr-menu-left":
		return p.adjustQuickMenu(a, -1), true
	case action == "aniclitr-menu-right":
		return p.adjustQuickMenu(a, +1), true
	case action == "aniclitr-menu-pick":
		return p.pickQuickMenu(a, p.qmenu.cur), true
	case strings.HasPrefix(action, "aniclitr-menu-num:"):
		if n, err := strconv.Atoi(strings.TrimPrefix(action, "aniclitr-menu-num:")); err == nil && n >= 1 && n <= mpvMenuCount {
			return p.pickQuickMenu(a, n-1), true
		}
		return nil, true
	}
	return nil, false
}

// assRoundedRect returns an ASS vector path (\p1) for a rounded rectangle of size w×h with radius r.
func assRoundedRect(w, h, r int) string {
	if r <= 0 {
		return fmt.Sprintf("m 0 0 l %d 0 %d %d 0 %d", w, w, h, h)
	}
	// Cubic Bezier approximation constant k ≈ 0.552 * r
	k := (r * 55) / 100
	return fmt.Sprintf(
		"m %d 0 l %d 0 b %d 0 %d %d %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d 0 %d 0 %d l 0 %d b 0 %d %d 0 %d 0",
		r, w-r,
		w-r+k, w, r-k, w, r,
		w, h-r,
		w, h-r+k, w-r+k, h, w-r, h,
		r, h,
		r-k, h, h-r+k, h-r,
		r,
		r-k, r-k, r,
	)
}

// assSolidCard emits the bottom shadow, solid opaque Nova card body, header band,
// sakura left accent strip, and footer band as stacked ASS vector layers.
func assSolidCard(x, y, w, h, hdrH, footH int) []string {
	var evs []string
	// 1. Soft drop shadow
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\blur7\1c&H000000&\1a&H60&\p1}%s{\p0}`,
		x+6, y+8, assRoundedRect(w, h, 14)))
	// 2. Solid high-contrast Nova card body (#110E1A -> BGR &H1A0E11&, 95% opaque) + subtle border (#3F3955)
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord1.6\shad0\1c&H1A0E11&\1a&H0C&\3c&H55393F&\3a&H18&\p1}%s{\p0}`,
		x, y, assRoundedRect(w, h, 12)))
	// 3. Elevated header strip (#1B1628 -> BGR &H28161B&)
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H28161B&\1a&H08&\p1}%s{\p0}`,
		x+2, y+2, assRoundedRect(w-4, hdrH, 10)))
	// 4. Header bottom hairline divider
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H4B415C&\1a&H20&\p1}m 0 0 l %d 0 %d 1 0 1{\p0}`,
		x+2, y+hdrH+2, w-4, w-4))
	// 5. Footer top hairline divider + tinted footer strip (#1A1624 -> BGR &H24161A&)
	fy := y + h - footH - 2
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H24161A&\1a&H08&\p1}%s{\p0}`,
		x+2, fy, assRoundedRect(w-4, footH, 8)))
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H4B415C&\1a&H20&\p1}m 0 0 l %d 0 %d 1 0 1{\p0}`,
		x+2, fy, w-4, w-4))
	// 6. Vibrant sakura left accent stripe (#FF6AA2 -> BGR &HA26AFF&)
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&HA26AFF&\1a&H00&\p1}%s{\p0}`,
		x, y+12, assRoundedRect(4, h-24, 2)))
	return evs
}

func (p *playing) buildQuickMenuASS(a *App) string {
	const (
		cardX = 34
		cardY = 28
		cardW = 528
		hdrH  = 54
		footH = 38
		rowH  = 38
		secH  = 26
	)
	curProf := shaders.Normalize(a.cfg.Anime4K)
	curSp := a.playSpeed
	if curSp <= 0 {
		curSp = 1.0
	}
	vol := p.volume
	if vol <= 0 && !p.muted {
		vol = 100
	}
	volTxt := fmt.Sprintf("%%%d", vol)
	if p.muted {
		volTxt = "Sessiz"
	}

	type rowItem struct {
		section string
		key     string
		label   string
		value   string
		sub     string
		active  bool
		arrows  bool
	}
	items := []rowItem{
		{section: "GÖRÜNTÜ YÜKSELTME  ·  GPU OPTİMİZE", key: "1", label: "Kapalı", sub: "orijinal görüntü", active: curProf == shaders.Off},
		{key: "2", label: "1080p Net", sub: "iGPU dostu · CAS", active: curProf == shaders.P1080},
		{key: "3", label: "2K Keskin", sub: "çizgi onarım + 2×", active: curProf == shaders.P2K},
		{key: "4", label: "4K Ultra", sub: "çift çizgi + ultra", active: curProf == shaders.P4K},
		{section: "ALTYAZI & SES", key: "5", label: "Altyazı Seçici (AniSub)", value: "y · aç"},
		{key: "6", label: "Altyazı Senkronu", value: player.FormatSubDelay(p.subDelay), active: math.Abs(p.subDelay) >= 0.05, arrows: true},
		{key: "7", label: "Ses Seviyesi", value: volTxt, active: p.muted || vol != 100, arrows: true},
		{section: "OYNATMA & BÖLÜM", key: "8", label: "Oynatma Hızı", value: fmt.Sprintf("%.2g×", curSp), active: curSp != 1.0, arrows: true},
		{key: "9", label: "Uyku Zamanlayıcısı", value: a.sleep.label(), active: a.sleep.active(), arrows: true},
		{key: "0", label: "Bölüm Geçişi", value: "Önceki · Sonraki", arrows: true},
	}

	// Precompute exact Y positions for every section & row.
	rowY := make([]int, len(items))
	secY := make([]int, len(items))
	cy := cardY + hdrH + 14
	for i, it := range items {
		if it.section != "" {
			if i > 0 {
				cy += 8
			}
			secY[i] = cy
			cy += secH
		}
		rowY[i] = cy
		cy += rowH
	}
	cardH := (cy + 10 + footH + 4) - cardY

	var evs []string
	// Solid opaque Nova card base layers
	evs = append(evs, assSolidCard(cardX, cardY, cardW, cardH, hdrH, footH)...)

	// Header title (left) + live resolution/upscale telemetry pill (right)
	evs = append(evs, fmt.Sprintf(
		`{\an7\pos(%d,%d)\fnSegoe UI\fs23\b1\bord0\shad0\1c&HA26AFF&}✦ {\1c&HF4F2FA&}ANICLITR {\fs17\b0\1c&H9E97B3&}HIZLI MENÜ`,
		cardX+20, cardY+15))

	statusText := shaders.ShortLabel(curProf)
	if p.videoH > 0 {
		statusText = fmt.Sprintf("%dp  →  %s", p.videoH, shaders.ShortLabel(curProf))
	}
	statusCol := "&HB8B1CC&"
	if curProf != shaders.Off {
		statusCol = "&H9AD36B&" // cOK green in BGR (#6BD39A)
	}
	evs = append(evs, fmt.Sprintf(
		`{\an9\pos(%d,%d)\fnSegoe UI\fs16\b1\bord0\shad0\1c%s}%s`,
		cardX+cardW-18, cardY+19, statusCol, assEscape(statusText)))

	// Highlighted selection bar behind the active cursor row
	selY := rowY[p.qmenu.cur]
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord1\shad0\1c&H422830&\1a&H0A&\3c&HA26AFF&\3a&H60&\p1}%s{\p0}`,
		cardX+12, selY-2, assRoundedRect(cardW-24, rowH-4, 7)))
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&HA26AFF&\1a&H00&\p1}%s{\p0}`,
		cardX+12, selY+3, assRoundedRect(4, rowH-14, 2)))

	// Render sections & rows
	for i, it := range items {
		if it.section != "" {
			if i > 0 {
				evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H3B344E&\1a&H30&\p1}m 0 0 l %d 0 %d 1 0 1{\p0}`,
					cardX+18, secY[i]-5, cardW-36, cardW-36))
			}
			evs = append(evs, fmt.Sprintf(
				`{\an7\pos(%d,%d)\fnSegoe UI\fs14\b1\bord0\shad0\1c&HFF7BB5&}%s`,
				cardX+22, secY[i]+3, assEscape(it.section)))
		}
		isSel := i == p.qmenu.cur
		y := rowY[i] + 6

		// Key badge + active dot + label
		keyCol := "&H948DA8&"
		lblCol := "&HE8E4F2&"
		boldTag := `\b0`
		if isSel {
			keyCol = "&HA26AFF&"
			lblCol = "&HFFFFFF&"
			boldTag = `\b1`
		}
		dot := `{\1c&H59526E&}○ `
		if it.active {
			dot = `{\1c&H9AD36B&\b1}● `
		}
		subPart := ""
		if it.sub != "" {
			subPart = `   {\fs15\b0\1c&H9E97B3&}` + assEscape(it.sub)
		}
		evs = append(evs, fmt.Sprintf(
			`{\an7\pos(%d,%d)\fnSegoe UI\fs19\bord0\shad0\1c%s\b1}%s  %s{\1c%s%s}%s%s`,
			cardX+24, y, keyCol, it.key, dot, lblCol, boldTag, assEscape(it.label), subPart))

		// Right-aligned status pill / inline value control
		rightTxt := ""
		rightCol := "&HB8B1CC&"
		switch {
		case i < 4 && it.active:
			rightTxt = "✓ AKTİF"
			rightCol = "&H9AD36B&"
		case it.value != "":
			if it.arrows {
				rightTxt = "◂  " + it.value + "  ▸"
			} else {
				rightTxt = it.value
			}
			if isSel {
				rightCol = "&H4EC1F2&" // warm gold highlight (#F2C14E -> BGR)
			} else if it.active {
				rightCol = "&H9AD36B&"
			}
		}
		if rightTxt != "" {
			evs = append(evs, fmt.Sprintf(
				`{\an9\pos(%d,%d)\fnSegoe UI\fs17\b1\bord0\shad0\1c%s}%s`,
				cardX+cardW-24, y+2, rightCol, assEscape(rightTxt)))
		}
	}

	// Footer keycap bar
	fy := cardY + cardH - footH + 8
	evs = append(evs, fmt.Sprintf(
		`{\an7\pos(%d,%d)\fnSegoe UI\fs16\bord0\shad0\1c&HFFFFFF&\b1}↑↓{\b0\1c&H9E97B3&} gez   {\1c&HFFFFFF&\b1}←→{\b0\1c&H9E97B3&} ayarla   {\1c&HFFFFFF&\b1}Enter{\b0\1c&H9E97B3&} uygula   {\1c&HFFFFFF&\b1}1-9/0{\b0\1c&H9E97B3&} hızlı   {\1c&HFFFFFF&\b1}m/Esc{\b0\1c&H9E97B3&} kapat`,
		cardX+20, fy))

	return strings.Join(evs, "\n")
}
