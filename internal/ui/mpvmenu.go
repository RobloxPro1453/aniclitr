package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/player"
	"aniclitr/internal/shaders"
)

// mpvMenuState drives the interactive in-mpv ASS OSD dropdown menu (`m` / `Tab` / `Alt+4`).
// Users see and pick upscaling (Kapalı / 1080p / 2K / 4K), sleep timer, playback speed,
// and subtitle picker directly over the video — zero Alt-Tab, zero memorized hotkeys.
type mpvMenuState struct {
	open bool
	cur  int
}

const mpvMenuCount = 8

var speedCycle = []float64{1.0, 1.25, 1.5, 2.0, 0.75}

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

func (p *playing) pickQuickMenu(a *App, idx int) tea.Cmd {
	if idx < 0 || idx >= mpvMenuCount {
		return nil
	}
	p.qmenu.cur = idx
	switch idx {
	case 0, 1, 2, 3:
		prof := shaders.Profiles[idx]
		a.cfg.Anime4K = prof
		_ = a.cfg.Save()
		if p.sess != nil {
			p.sess.SetShaders(prof)
		}
		a.notify("Görüntü yükseltme: "+shaders.ShortLabel(prof), "ok")
		p.syncQuickMenu(a)
		return a.tick()
	case 4:
		p.closeQuickMenu()
		return p.openSubs(a, true)
	case 5:
		lbl := a.sleep.cycle()
		a.notify("☾ uyku zamanlayıcısı: "+lbl, "ok")
		p.syncQuickMenu(a)
		return a.tick()
	case 6:
		curSp := a.playSpeed
		if curSp <= 0 {
			curSp = 1.0
		}
		nextSp := speedCycle[0]
		for i, sp := range speedCycle {
			if sp == curSp {
				nextSp = speedCycle[(i+1)%len(speedCycle)]
				break
			}
		}
		a.playSpeed = nextSp
		if p.sess != nil {
			p.sess.SetSpeed(nextSp)
		}
		p.syncQuickMenu(a)
		return a.tick()
	case 7:
		cmd := p.adjustSubDelay(a, 0, true)
		p.syncQuickMenu(a)
		return cmd
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

func (p *playing) buildQuickMenuASS(a *App) string {
	const (
		hdr    = `{\an7\pos(40,34)\fnSegoe UI\fs24\b1\bord2.4\shad1\c&HA88BF3&\3c&H181111&}`
		sec    = `{\fs17\b1\c&H9AA0B4&}`
		sel    = `{\fs21\b1\c&HAFE2F9&}`
		norm   = `{\fs20\b0\c&HF0E8E8&}`
		active = `{\c&HA1E3A6&\b1}`
		dim    = `{\fs17\b0\c&H9AA0B4&}`
	)
	curProf := shaders.Normalize(a.cfg.Anime4K)
	curSp := a.playSpeed
	if curSp <= 0 {
		curSp = 1.0
	}

	type rowItem struct {
		group string
		label string
		badge string
		on    bool
	}
	items := []rowItem{
		{group: "GÖRÜNTÜ YÜKSELTME (GPU OPTİMİZE)", label: "Kapalı (orijinal görüntü)", badge: "en düşük GPU", on: curProf == shaders.Off},
		{label: "1080p Net", badge: "iGPU dostu · CAS", on: curProf == shaders.P1080},
		{label: "2K Keskin", badge: "2× anti-ringing", on: curProf == shaders.P2K},
		{label: "4K Ultra", badge: "maksimum netlik", on: curProf == shaders.P4K},
		{group: "OYNATMA KONTROLLERİ", label: "Altyazı Seçici (AniSub)", badge: "y"},
		{label: "Uyku Zamanlayıcısı", badge: a.sleep.label(), on: a.sleep.active()},
		{label: "Oynatma Hızı", badge: fmt.Sprintf("%.2g×", curSp), on: curSp != 1.0},
		{label: "Altyazı Senkronunu Sıfırla", badge: player.FormatSubDelay(p.subDelay)},
	}

	var b strings.Builder
	b.WriteString(hdr)
	b.WriteString("ANICLITR HIZLI MENÜ")
	b.WriteString(`\N`)

	for i, it := range items {
		if it.group != "" {
			b.WriteString(`\N` + sec + assEscape(it.group) + `\N`)
		}
		st := norm
		pref := fmt.Sprintf("   %d. ", i+1)
		if i == p.qmenu.cur {
			st = sel
			pref = fmt.Sprintf("▸ %d. ", i+1)
		}
		dot := "○ "
		if it.on {
			dot = active + "● " + st
		}
		b.WriteString(st + pref + dot + assEscape(it.label))
		if it.badge != "" {
			b.WriteString(dim + "   [" + assEscape(it.badge) + "]")
		}
		b.WriteString(`\N`)
	}
	b.WriteString(`\N` + dim + "↑↓ seç · Enter / 1-8 uygula · m / Esc kapat")
	return b.String()
}
