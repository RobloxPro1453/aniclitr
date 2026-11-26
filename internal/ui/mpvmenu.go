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

// mpvMenuPage identifies which page of the Tokyo Studio in-mpv console is open.
type mpvMenuPage int

const (
	menuRoot mpvMenuPage = iota
	menuRes              // Category 1: Real non-AI Resolution Upscaling (AMD FSR 1.0)
	menuQuality          // Category 2: Anime4K v4 CNN Restoration + GPU Frame-Gen
	menuSubs             // Subtitle sync + AniSub launcher
	menuPlayback         // Volume, Speed, Sleep Timer
)

// mpvMenuState drives the hierarchical, architectural "Tokyo Studio" in-mpv console
// (`m` / `Tab` / `Alt+4`). Esc / Backspace steps back to the parent menu when inside
// a sub-menu, and closes cleanly on the root page.
type mpvMenuState struct {
	open    bool
	page    mpvMenuPage
	cur     int
	rootCur int // remembered root cursor when stepping back from a sub-menu
}

var speedCycle = []float64{0.75, 1.0, 1.25, 1.5, 1.75, 2.0}

func (p *playing) openQuickMenu(a *App) {
	if p.subs.open {
		p.closeSubs()
	}
	p.qmenu.open = true
	p.qmenu.page = menuRoot
	p.qmenu.cur = p.qmenu.rootCur
	p.syncQuickMenu(a)
}

func (p *playing) reopenQuickMenuPage(a *App, pg mpvMenuPage) {
	p.qmenu.open = true
	p.qmenu.page = pg
	p.qmenu.cur = 0
	p.syncQuickMenu(a)
}

func (p *playing) closeQuickMenu() {
	if !p.qmenu.open {
		return
	}
	p.qmenu.open = false
	p.qmenu.page = menuRoot
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

func (p *playing) curPipeline(a *App) shaders.Pipeline {
	return shaders.Pipeline{
		Res:      a.cfg.UpscaleRes,
		Enhance:  a.cfg.Anime4K,
		FrameGen: a.cfg.FrameGen,
	}
}

func (p *playing) applyPipeline(a *App, silent bool) tea.Cmd {
	_ = a.cfg.Save()
	pipe := p.curPipeline(a)
	if p.sess != nil {
		p.sess.SetPipeline(pipe, silent)
	}
	a.notify("Görüntü: "+shaders.SummaryBadge(pipe), "ok")
	p.syncQuickMenu(a)
	return nil
}

func (p *playing) stepSpeed(a *App, dir int) tea.Cmd {
	curSp := a.playSpeed
	if curSp <= 0 {
		curSp = 1.0
	}
	idx := 1 // 1.0x
	bestDiff := 99.0
	for i, sp := range speedCycle {
		if d := math.Abs(sp - curSp); d < bestDiff {
			bestDiff = d
			idx = i
		}
	}
	nextSp := speedCycle[(idx+dir+len(speedCycle))%len(speedCycle)]
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
		for i := 0; i < 5; i++ {
			lbl = a.sleep.cycle()
		}
	} else {
		lbl = a.sleep.cycle()
	}
	a.notify("Uyku zamanlayıcısı: "+lbl, "ok")
	p.syncQuickMenu(a)
	return nil
}

var audioProfileCycle = []string{"referans", "gece", "kulaklık"}

func (p *playing) stepAudioProfile(a *App, dir int) tea.Cmd {
	idx := 0
	for i, ap := range audioProfileCycle {
		if ap == a.cfg.AudioProfile {
			idx = i
		}
	}
	next := audioProfileCycle[(idx+dir+len(audioProfileCycle))%len(audioProfileCycle)]
	a.cfg.AudioProfile = next
	_ = a.cfg.Save()
	if p.sess != nil {
		p.sess.SetAudioProfile(next, true)
	}
	a.notify("Ses Stüdyosu: "+player.AudioProfileLabel(next), "ok")
	p.syncQuickMenu(a)
	return nil
}

func (p *playing) stepSubFontSize(a *App, delta int) tea.Cmd {
	sz := a.cfg.SubFontSize
	if sz <= 0 {
		sz = 48
	}
	sz = clamp(sz+delta, 24, 96)
	a.cfg.SubFontSize = sz
	_ = a.cfg.Save()
	if p.sess != nil {
		p.sess.SetSubStyle(a.cfg.SubFont, sz, a.cfg.SubPos, a.cfg.SubBorder, a.cfg.SubColor, true)
	}
	p.syncQuickMenu(a)
	return nil
}

func (p *playing) stepSubPos(a *App, delta int) tea.Cmd {
	sp := a.cfg.SubPos
	if sp <= 0 {
		sp = 96
	}
	sp = clamp(sp+delta, 50, 100)
	a.cfg.SubPos = sp
	_ = a.cfg.Save()
	if p.sess != nil {
		p.sess.SetSubStyle(a.cfg.SubFont, a.cfg.SubFontSize, sp, a.cfg.SubBorder, a.cfg.SubColor, true)
	}
	p.syncQuickMenu(a)
	return nil
}

func (p *playing) menuRowCount() int {
	switch p.qmenu.page {
	case menuRes:
		return len(shaders.ResProfiles) // 5: Kapalı, Otomatik, 1080p, 2K, 4K
	case menuQuality:
		return len(shaders.Profiles) + 1 // 4 Anime4K + 1 Frame-Gen inline row = 5
	case menuSubs:
		return 6 // AniSub aç, Senkron ±0.1s, Sıfırla, Oto-Hizala, Tipografi Boyut, Tipografi Konum
	case menuPlayback:
		return 6 // Ses, Ses Stüdyosu, Hız, Uyku, Sahne Yer İmi, Klip 10sn
	default:
		return 5 // Root: Çözünürlük, Kalite & Akıcılık, Altyazı, Ses & Oynatma, Bölüm
	}
}

func (p *playing) enterSubMenu(a *App, pg mpvMenuPage, defaultCur int) {
	p.qmenu.rootCur = p.qmenu.cur
	p.qmenu.page = pg
	p.qmenu.cur = clamp(defaultCur, 0, p.menuRowCount()-1)
	p.syncQuickMenu(a)
}

func (p *playing) backQuickMenu(a *App) {
	if p.qmenu.page != menuRoot {
		p.qmenu.page = menuRoot
		p.qmenu.cur = clamp(p.qmenu.rootCur, 0, p.menuRowCount()-1)
		p.syncQuickMenu(a)
		return
	}
	p.closeQuickMenu()
}

func (p *playing) pickQuickMenu(a *App, idx int) tea.Cmd {
	n := p.menuRowCount()
	if idx < 0 || idx >= n {
		return nil
	}
	p.qmenu.cur = idx
	switch p.qmenu.page {
	case menuRoot:
		switch idx {
		case 0:
			// Focus the currently active resolution preset inside sub-menu
			cur := 0
			for i, r := range shaders.ResProfiles {
				if r == shaders.NormalizeRes(a.cfg.UpscaleRes) {
					cur = i
				}
			}
			p.enterSubMenu(a, menuRes, cur)
		case 1:
			cur := 0
			for i, e := range shaders.Profiles {
				if e == shaders.Normalize(a.cfg.Anime4K) {
					cur = i
				}
			}
			p.enterSubMenu(a, menuQuality, cur)
		case 2:
			p.enterSubMenu(a, menuSubs, 0)
		case 3:
			p.enterSubMenu(a, menuPlayback, 0)
		case 4:
			p.closeQuickMenu()
			if p.sess != nil {
				p.sess.Next()
			}
		}
	case menuRes:
		a.cfg.UpscaleRes = shaders.ResProfiles[idx]
		return p.applyPipeline(a, true)
	case menuQuality:
		if idx < len(shaders.Profiles) {
			a.cfg.Anime4K = shaders.Profiles[idx]
			return p.applyPipeline(a, true)
		}
		a.cfg.FrameGen = shaders.NextFG(a.cfg.FrameGen)
		return p.applyPipeline(a, true)
	case menuSubs:
		switch idx {
		case 0:
			p.qmenu.open = false
			if p.sess != nil {
				p.sess.HideQuickMenu()
			}
			cmd := p.openSubs(a, true)
			p.subs.returnMenu = true
			return cmd
		case 1, 2:
			cmd := p.adjustSubDelay(a, 0, true)
			p.syncQuickMenu(a)
			return cmd
		case 3:
			p.closeQuickMenu()
			return p.runAutoSubSync(a)
		case 4:
			return p.stepSubFontSize(a, +2)
		case 5:
			return p.stepSubPos(a, -2)
		}
	case menuPlayback:
		switch idx {
		case 0:
			p.muted = !p.muted
			if p.sess != nil {
				p.sess.ToggleMute()
			}
			p.syncQuickMenu(a)
		case 1:
			return p.stepSpeed(a, +1)
		case 2:
			return p.stepSleep(a, +1)
		case 3:
			return p.stepAudioProfile(a, +1)
		case 4:
			p.closeQuickMenu()
			cmd, _ := p.handleV2MpvAction(a, player.Event{Action: "aniclitr-bookmark", Pos: p.pos})
			return cmd
		case 5:
			p.closeQuickMenu()
			cmd, _ := p.handleV2MpvAction(a, player.Event{Action: "aniclitr-clip", Pos: p.pos})
			return cmd
		}
	}
	return nil
}

func (p *playing) adjustQuickMenu(a *App, dir int) tea.Cmd {
	switch p.qmenu.page {
	case menuRoot:
		if dir > 0 && p.qmenu.cur < 4 {
			return p.pickQuickMenu(a, p.qmenu.cur)
		}
		if p.qmenu.cur == 4 {
			p.closeQuickMenu()
			if p.sess != nil {
				if dir < 0 {
					p.sess.Prev()
				} else {
					p.sess.Next()
				}
			}
		}
	case menuRes:
		if dir < 0 {
			p.backQuickMenu(a)
			return nil
		}
		return p.pickQuickMenu(a, p.qmenu.cur)
	case menuQuality:
		if p.qmenu.cur == len(shaders.Profiles) {
			if dir < 0 {
				a.cfg.FrameGen = shaders.PrevFG(a.cfg.FrameGen)
			} else {
				a.cfg.FrameGen = shaders.NextFG(a.cfg.FrameGen)
			}
			return p.applyPipeline(a, true)
		}
		if dir < 0 {
			p.backQuickMenu(a)
			return nil
		}
		return p.pickQuickMenu(a, p.qmenu.cur)
	case menuSubs:
		switch p.qmenu.cur {
		case 1:
			cmd := p.adjustSubDelay(a, float64(dir)*0.1, false)
			p.syncQuickMenu(a)
			return cmd
		case 4:
			return p.stepSubFontSize(a, dir*2)
		case 5:
			return p.stepSubPos(a, dir*2)
		}
		if dir < 0 {
			p.backQuickMenu(a)
		} else if p.qmenu.cur == 0 || p.qmenu.cur == 3 {
			return p.pickQuickMenu(a, p.qmenu.cur)
		}
	case menuPlayback:
		switch p.qmenu.cur {
		case 0:
			return p.stepVolume(a, dir*5)
		case 1:
			return p.stepSpeed(a, dir)
		case 2:
			return p.stepSleep(a, dir)
		case 3:
			return p.stepAudioProfile(a, dir)
		}
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
	case action == "aniclitr-menu-back":
		p.backQuickMenu(a)
		return nil, true
	case action == "aniclitr-menu-up":
		n := p.menuRowCount()
		p.qmenu.cur = (p.qmenu.cur - 1 + n) % n
		p.syncQuickMenu(a)
		return nil, true
	case action == "aniclitr-menu-down":
		n := p.menuRowCount()
		p.qmenu.cur = (p.qmenu.cur + 1) % n
		p.syncQuickMenu(a)
		return nil, true
	case action == "aniclitr-menu-left":
		return p.adjustQuickMenu(a, -1), true
	case action == "aniclitr-menu-right":
		return p.adjustQuickMenu(a, +1), true
	case action == "aniclitr-menu-pick":
		return p.pickQuickMenu(a, p.qmenu.cur), true
	case strings.HasPrefix(action, "aniclitr-menu-num:"):
		if n, err := strconv.Atoi(strings.TrimPrefix(action, "aniclitr-menu-num:")); err == nil && n >= 1 && n <= p.menuRowCount() {
			return p.pickQuickMenu(a, n-1), true
		}
		return nil, true
	}
	return nil, false
}

// assSharpBox draws an architectural sharp-cornered rectangle (2px bevel) for
// the Tokyo Studio / Cinema HUD aesthetic — zero bubbly SaaS rounding or purple glow.
func assSharpBox(w, h int) string {
	return fmt.Sprintf("m 2 0 l %d 0 %d 2 %d %d %d %d 2 %d 0 %d 0 2",
		w-2, w, w, h-2, w-2, h, h, h-2)
}

// assStudioPanel draws the matte obsidian Cinema HUD frame:
// crisp 1px graphite frame, subtle drop shadow, vermilion signature accent bar,
// and clean top/bottom architectural dividers.
func assStudioPanel(x, y, w, h, hdrH, footH int) []string {
	return []string{
		// Soft matte elevation shadow
		fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\blur6\1c&H000000&\1a&H70&\p1}%s{\p0}`,
			x+5, y+6, assSharpBox(w, h)),
		// Solid matte obsidian ink (#0B0B0D -> BGR &H0D0B0B&, 94% opaque) + crisp 1px graphite edge (#27272C)
		fmt.Sprintf(`{\an7\pos(%d,%d)\bord1\shad0\1c&H0D0B0B&\1a&H0E&\3c&H2C2727&\3a&H00&\p1}%s{\p0}`,
			x, y, assSharpBox(w, h)),
		// Top signature vermilion bar (52px wide x 2px tall — Japanese broadcast HUD motif)
		fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H6A48E5&\1a&H00&\p1}m 0 0 l 56 0 56 2 0 2{\p0}`,
			x, y),
		// Header bottom rule
		fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H262424&\1a&H00&\p1}m 0 0 l %d 0 %d 1 0 1{\p0}`,
			x+1, y+hdrH, w-2, w-2),
		// Footer top rule
		fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H262424&\1a&H00&\p1}m 0 0 l %d 0 %d 1 0 1{\p0}`,
			x+1, y+h-footH, w-2, w-2),
	}
}

type studioRow struct {
	section string
	idx     string
	label   string
	note    string
	right   string
	active  bool
	submenu bool
	slider  bool
}

func (p *playing) buildQuickMenuASS(a *App) string {
	const (
		cardX = 36
		cardY = 32
		cardW = 504
		hdrH  = 50
		footH = 36
		rowH  = 42
		secH  = 24
	)
	pipe := p.curPipeline(a)
	curRes := shaders.NormalizeRes(a.cfg.UpscaleRes)
	curEnh := shaders.Normalize(a.cfg.Anime4K)
	curFG := shaders.NormalizeFG(a.cfg.FrameGen)
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

	breadcrumb := "ANICLITR"
	subHdr := "KONTROL"
	var rows []studioRow
	footHint := `{\1c&HF0F0F2&}↑↓{\1c&H84848E&} gez    {\1c&HF0F0F2&}Enter/→{\1c&H84848E&} aç    {\1c&HF0F0F2&}1-5{\1c&H84848E&} hızlı    {\1c&HF0F0F2&}Esc{\1c&H84848E&} kapat`

	switch p.qmenu.page {
	case menuRes:
		breadcrumb = "MENÜ / ÇÖZÜNÜRLÜK"
		subHdr = "AMD FIDELITYFX FSR 1.0"
		rows = []studioRow{
			{idx: "01", label: "Kapalı", note: "orijinal video", active: curRes == shaders.ResOff},
			{idx: "02", label: "Otomatik", note: "monitör çözünürlüğü", active: curRes == shaders.ResAuto},
			{idx: "03", label: "1080p", note: "1920×1080 ölçekleme", active: curRes == shaders.Res1080},
			{idx: "04", label: "2K", note: "2560×1440 ölçekleme", active: curRes == shaders.Res2K},
			{idx: "05", label: "4K", note: "3840×2160 ölçekleme", active: curRes == shaders.Res4K},
		}
		footHint = `{\1c&HF0F0F2&}↑↓{\1c&H84848E&} seç    {\1c&HF0F0F2&}Enter/1-5{\1c&H84848E&} uygula    {\1c&HF0F0F2&}Esc/←{\1c&H84848E&} üst menü`
	case menuQuality:
		breadcrumb = "MENÜ / KALİTE & AKICILIK"
		subHdr = "ANIME4K v4 · GPU"
		rows = []studioRow{
			{section: "KALİTE İYİLEŞTİRME · ANIME4K v4 CNN", idx: "01", label: "Kapalı", note: "orijinal doku", active: curEnh == shaders.Off},
			{idx: "02", label: "Hızlı", note: "CNN-S · düşük GPU yükü", active: curEnh == shaders.Fast},
			{idx: "03", label: "Cam Gibi", note: "CNN-Soft · dengeli netlik", active: curEnh == shaders.Clear},
			{idx: "04", label: "Sinema", note: "Çift CNN · yüksek netlik", active: curEnh == shaders.Cinema},
			{section: "AKICILIK · FRAME-GEN (GPU)", idx: "05", label: "Hareket Yumuşatma", right: shaders.ShortFGLabel(curFG), active: curFG != shaders.FGOff, slider: true},
		}
		footHint = `{\1c&HF0F0F2&}↑↓{\1c&H84848E&} seç    {\1c&HF0F0F2&}Enter/←→{\1c&H84848E&} uygula    {\1c&HF0F0F2&}Esc{\1c&H84848E&} üst menü`
	case menuSubs:
		breadcrumb = "MENÜ / ALTYAZI & TİPOGRAFİ"
		subHdr = "ANISUB · SENKRON · TİPOGRAFİ"
		sz := a.cfg.SubFontSize
		if sz <= 0 {
			sz = 48
		}
		sp := a.cfg.SubPos
		if sp <= 0 {
			sp = 96
		}
		rows = []studioRow{
			{idx: "01", label: "AniSub Altyazı Seçici", right: "aç", submenu: true},
			{idx: "02", label: "Altyazı Senkronu", right: player.FormatSubDelay(p.subDelay), active: math.Abs(p.subDelay) >= 0.05, slider: true},
			{idx: "03", label: "Senkronu Sıfırla", right: "0,0 sn"},
			{section: "OTOMATİK HİZALAMA · TİPOGRAFİ", idx: "04", label: "Otomatik Hizala", note: "ses + sahne + FPS", right: "çalıştır"},
			{idx: "05", label: "Altyazı Boyutu", right: fmt.Sprintf("%d pt", sz), active: sz != 48, slider: true},
			{idx: "06", label: "Dikey Konum", right: fmt.Sprintf("%%%d", sp), active: sp != 96, slider: true},
		}
		footHint = `{\1c&HF0F0F2&}↑↓{\1c&H84848E&} seç    {\1c&HF0F0F2&}←→{\1c&H84848E&} ayarla    {\1c&HF0F0F2&}Enter{\1c&H84848E&} uygula    {\1c&HF0F0F2&}Esc{\1c&H84848E&} üst menü`
	case menuPlayback:
		breadcrumb = "MENÜ / SES & KLİP"
		subHdr = "SES PROFİLİ · KLİP"
		rows = []studioRow{
			{idx: "01", label: "Ses Seviyesi", right: volTxt, active: p.muted || vol != 100, slider: true},
			{idx: "02", label: "Oynatma Hızı", right: fmt.Sprintf("%.2g×", curSp), active: curSp != 1.0, slider: true},
			{idx: "03", label: "Uyku Zamanlayıcısı", right: a.sleep.label(), active: a.sleep.active(), slider: true},
			{idx: "04", label: "Ses Stüdyosu", right: player.AudioProfileLabel(a.cfg.AudioProfile), active: a.cfg.AudioProfile != "" && a.cfg.AudioProfile != "referans", slider: true},
			{section: "SAHNE & KLİP STÜDYOSU", idx: "05", label: "Sahne Yer İmi Ekle", note: "Shift+B", right: clock(p.pos)},
			{idx: "06", label: "Son 10 sn Klip Kaydet", note: "Alt+C · MP4", right: "kes"},
		}
		footHint = `{\1c&HF0F0F2&}↑↓{\1c&H84848E&} seç    {\1c&HF0F0F2&}←→{\1c&H84848E&} ayarla    {\1c&HF0F0F2&}Enter{\1c&H84848E&} değiştir    {\1c&HF0F0F2&}Esc{\1c&H84848E&} üst menü`
	default:
		qSummary := shaders.ShortLabel(curEnh)
		if curEnh == shaders.Off && curFG != shaders.FGOff {
			qSummary = shaders.ShortFGLabel(curFG)
		} else if curEnh != shaders.Off && curFG != shaders.FGOff {
			qSummary = shaders.SummaryBadge(shaders.Pipeline{Enhance: curEnh, FrameGen: curFG})
		}
		rows = []studioRow{
			{idx: "01", label: "Çözünürlük (FSR)", right: shaders.ShortResLabel(curRes), active: curRes != shaders.ResOff, submenu: true},
			{idx: "02", label: "Kalite & Akıcılık", right: qSummary, active: curEnh != shaders.Off || curFG != shaders.FGOff, submenu: true},
			{idx: "03", label: "Altyazı & Senkron", right: player.FormatSubDelay(p.subDelay), active: math.Abs(p.subDelay) >= 0.05, submenu: true},
			{idx: "04", label: "Ses & Oynatma", right: fmt.Sprintf("%s · %.2g×", volTxt, curSp), active: p.muted || curSp != 1.0 || a.sleep.active(), submenu: true},
			{idx: "05", label: "Bölüm Geçişi", right: "Önceki · Sonraki", slider: true},
		}
	}

	rowY := make([]int, len(rows))
	secY := make([]int, len(rows))
	cy := cardY + hdrH + 10
	for i, r := range rows {
		if r.section != "" {
			if i > 0 {
				cy += 6
			}
			secY[i] = cy
			cy += secH
		}
		rowY[i] = cy
		cy += rowH
	}
	cardH := (cy + 8 + footH) - cardY

	var evs []string
	evs = append(evs, assStudioPanel(cardX, cardY, cardW, cardH, hdrH, footH)...)

	// Header: clean architectural breadcrumb + right-aligned video telemetry
	hdrLeft := assEscape(breadcrumb)
	if p.qmenu.page == menuRoot && subHdr != "" {
		hdrLeft += `  {\fs13\b0\1c&H787882&}` + assEscape(subHdr)
	}
	evs = append(evs, fmt.Sprintf(
		`{\an7\pos(%d,%d)\fnConsolas\fs16\b1\bord0\shad0\1c&HF4F4F6&}%s`,
		cardX+18, cardY+17, hdrLeft))

	tele := shaders.SummaryBadge(pipe)
	if p.videoH > 0 {
		tele = fmt.Sprintf("%dp  ›  %s", p.videoH, tele)
	}
	teleCol := "&H8E8E98&"
	if curRes != shaders.ResOff || curEnh != shaders.Off || curFG != shaders.FGOff {
		teleCol = "&H9AD36B&"
	}
	evs = append(evs, fmt.Sprintf(
		`{\an9\pos(%d,%d)\fnConsolas\fs14\b1\bord0\shad0\1c%s}%s`,
		cardX+cardW-18, cardY+18, teleCol, assEscape(tele)))

	// Selection row highlight: restrained matte graphite fill + razor-sharp 3px vermilion tick
	sel := clamp(p.qmenu.cur, 0, len(rows)-1)
	selY := rowY[sel]
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H1D1919&\1a&H00&\p1}m 0 0 l %d 0 %d %d 0 %d{\p0}`,
		cardX+1, selY, cardW-2, cardW-2, rowH-2, rowH-2))
	evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H6A48E5&\1a&H00&\p1}m 0 0 l 3 0 3 %d 0 %d{\p0}`,
		cardX+1, selY+5, rowH-12, rowH-12))

	// Render rows
	for i, r := range rows {
		if r.section != "" {
			if i > 0 {
				evs = append(evs, fmt.Sprintf(`{\an7\pos(%d,%d)\bord0\shad0\1c&H222020&\1a&H00&\p1}m 0 0 l %d 0 %d 1 0 1{\p0}`,
					cardX+18, secY[i]-3, cardW-36, cardW-36))
			}
			evs = append(evs, fmt.Sprintf(
				`{\an7\pos(%d,%d)\fnConsolas\fs12\b1\bord0\shad0\1c&H787882&}%s`,
				cardX+18, secY[i]+4, assEscape(r.section)))
		}
		isSel := i == sel
		y := rowY[i] + 9

		idxCol := "&H5C5C66&"
		lblCol := "&HD8D8E0&"
		bold := `\b0`
		if isSel {
			idxCol = "&H6A48E5&"
			lblCol = "&HFFFFFF&"
			bold = `\b1`
		}
		notePart := ""
		if r.note != "" {
			notePart = `   {\fnSegoe UI\fs14\b0\1c&H7E7E88&}` + assEscape(r.note)
		}
		evs = append(evs, fmt.Sprintf(
			`{\an7\pos(%d,%d)\fnConsolas\fs15\b1\bord0\shad0\1c%s}%s  {\fnSegoe UI\fs19%s\1c%s}%s%s`,
			cardX+18, y+1, idxCol, r.idx, bold, lblCol, assEscape(r.label), notePart))

		// Right-aligned value / active marker / submenu chevron
		rightTxt := ""
		rightCol := "&H9A9AA6&"
		switch {
		case r.active && !r.submenu && !r.slider:
			rightTxt = "● AKTİF"
			rightCol = "&H9AD36B&"
		case r.slider:
			rightTxt = "◂ " + r.right + " ▸"
			if isSel {
				rightCol = "&HFFFFFF&"
			} else if r.active {
				rightCol = "&H9AD36B&"
			}
		case r.submenu:
			rightTxt = r.right + "  ›"
			if isSel {
				rightCol = "&HFFFFFF&"
			} else if r.active {
				rightCol = "&H9AD36B&"
			}
		case r.right != "":
			rightTxt = r.right
		}
		if rightTxt != "" {
			evs = append(evs, fmt.Sprintf(
				`{\an9\pos(%d,%d)\fnConsolas\fs15\b1\bord0\shad0\1c%s}%s`,
				cardX+cardW-18, y+2, rightCol, assEscape(rightTxt)))
		}
	}

	// Footer strip
	fy := cardY + cardH - footH + 10
	evs = append(evs, fmt.Sprintf(
		`{\an7\pos(%d,%d)\fnConsolas\fs13\b0\bord0\shad0}%s`,
		cardX+18, fy, footHint))

	return strings.Join(evs, "\n")
}
