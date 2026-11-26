package ui

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/player"
	"aniclitr/internal/shaders"
	"aniclitr/internal/subs"
)

// External & local subtitles while playing:
//   - Press `y` inside mpv OR in the terminal to open the AniSub (+ optional
//     OpenSubtitles) chooser. Inside mpv it renders an interactive ASS OSD
//     menu directly on top of the video (no Alt-Tab needed).
//   - Press `z`/`x` (±0.1s), `Z`/`X` (±0.5s), or `0` / `Alt+0` (reset) at any
//     time to fine-tune subtitle sync.
//   - Press `l` inside the terminal subtitle card to load a local
//     .ass/.srt/.ssa/.vtt file straight into running mpv.

type subsMsg struct {
	id  int
	rs  []subs.Result
	err error
}

type subGotMsg struct {
	id   int
	path string
	r    subs.Result
	err  error
}

type subsState struct {
	open    bool
	busy    bool
	mpvOSD  bool // also render interactive ASS overlay inside mpv
	local   bool // typing a local .srt/.ass/.vtt path
	localIn input
	rs      []subs.Result
	cur     int
	err     string
}

func (a *App) subProvider() subs.Provider {
	provs := []subs.Provider{&subs.AniSub{}}
	if strings.TrimSpace(a.cfg.OSKey) != "" {
		provs = append(provs, &subs.OpenSubtitles{Key: a.cfg.OSKey})
	}
	return &subs.Multi{Providers: provs}
}

func (p *playing) openSubs(a *App, fromMpv bool) tea.Cmd {
	// If results were already loaded for this episode, reopen immediately.
	if len(p.subs.rs) > 0 && p.subs.err == "" && !p.subs.busy {
		p.subs.open = true
		p.subs.local = false
		p.subs.mpvOSD = fromMpv || p.subs.mpvOSD
		if p.subs.mpvOSD {
			p.syncMpvSubMenu()
		}
		return nil
	}
	return p.searchSubs(a, fromMpv)
}

func (p *playing) closeSubs() {
	p.subs.open = false
	p.subs.local = false
	if p.subs.mpvOSD && p.sess != nil {
		p.sess.HideSubMenu()
	}
	p.subs.mpvOSD = false
}

func (p *playing) searchSubs(a *App, fromMpv bool) tea.Cmd {
	p.subs = subsState{
		open:    true,
		busy:    true,
		mpvOSD:  fromMpv,
		localIn: input{placeholder: `C:\yol\altyazi.ass veya .srt yapıştır…`},
	}
	if fromMpv {
		p.syncMpvSubMenu()
	}
	id, prov := p.id, a.subProvider()
	q := subs.Query{Title: p.an.Title, Season: p.ep.Season, Episode: p.ep.Number, Langs: []string{"tr", "en"}}
	if p.an.IsMovie() {
		q.Season, q.Episode = 0, 0
	}
	return tea.Batch(a.tick(), run(30*time.Second, func(ctx context.Context) tea.Msg {
		rs, err := prov.Search(ctx, q)
		return subsMsg{id, rs, err}
	}))
}

func (p *playing) pickSub(a *App, idx int) tea.Cmd {
	if idx < 0 || idx >= len(p.subs.rs) || p.subs.busy {
		return nil
	}
	p.subs.cur = idx
	r := p.subs.rs[idx]
	p.subs.busy = true
	if p.subs.mpvOSD {
		p.syncMpvSubMenu()
	}
	id, prov := p.id, a.subProvider()
	return tea.Batch(a.tick(), run(40*time.Second, func(ctx context.Context) tea.Msg {
		path, err := prov.Download(ctx, r)
		return subGotMsg{id, path, r, err}
	}))
}

// adjustSubDelay shifts subtitle timing in seconds (delta == 0 resets to 0.0).
func (p *playing) adjustSubDelay(a *App, delta float64, reset bool) tea.Cmd {
	if reset {
		p.subDelay = 0
		if p.sess != nil {
			p.sess.ResetSubDelay()
		}
		a.notify("altyazı senkronu sıfırlandı (0,0 sn)", "ok")
	} else {
		if p.sess != nil {
			p.subDelay = p.sess.AdjustSubDelay(delta)
		} else {
			p.subDelay = math.Round((p.subDelay+delta)*10) / 10
			if math.Abs(p.subDelay) < 0.05 {
				p.subDelay = 0
			}
		}
		a.notify("altyazı senkronu: "+player.FormatSubDelay(p.subDelay), "ok")
	}
	if p.subs.open && p.subs.mpvOSD {
		p.syncMpvSubMenu()
	}
	return a.tick()
}

// onMpvAction handles live key actions triggered inside the mpv window (no Alt-Tab).
func (p *playing) onMpvAction(a *App, action string) tea.Cmd {
	switch {
	case action == "aniclitr-subs":
		if p.subs.open {
			p.closeSubs()
			return nil
		}
		return p.openSubs(a, true)
	case action == "aniclitr-sub-close":
		p.closeSubs()
		return nil
	case action == "aniclitr-sub-up":
		if len(p.subs.rs) > 0 {
			p.subs.cur = max(0, p.subs.cur-1)
			p.syncMpvSubMenu()
		}
		return nil
	case action == "aniclitr-sub-down":
		if len(p.subs.rs) > 0 {
			p.subs.cur = clamp(p.subs.cur+1, 0, len(p.subs.rs)-1)
			p.syncMpvSubMenu()
		}
		return nil
	case action == "aniclitr-sub-pick":
		return p.pickSub(a, p.subs.cur)
	case strings.HasPrefix(action, "aniclitr-sub-num:"):
		if n, err := strconv.Atoi(strings.TrimPrefix(action, "aniclitr-sub-num:")); err == nil && n >= 1 {
			off := p.subWindowOffset(8)
			return p.pickSub(a, off+n-1)
		}
	case action == "aniclitr-anime4k":
		a.cfg.Anime4K = shaders.Next(a.cfg.Anime4K)
		_ = a.cfg.Save()
		if p.sess != nil {
			p.sess.SetShaders(a.cfg.Anime4K)
		}
		a.notify("Anime4K: "+shaders.Label(a.cfg.Anime4K), "ok")
		return a.tick()
	case action == "aniclitr-sleep":
		lbl := a.sleep.cycle()
		if p.sess != nil {
			p.sess.ShowOSD("☾ Uyku zamanlayıcısı: "+lbl, 2400)
		}
		a.notify("☾ uyku zamanlayıcısı: "+lbl, "ok")
		return a.tick()
	}
	return nil
}

func (p *playing) subWindowOffset(limit int) int {
	if p.subs.cur >= limit {
		return p.subs.cur - limit + 1
	}
	return 0
}

func (p *playing) subKey(a *App, k tea.KeyMsg) tea.Cmd {
	if p.subs.local {
		switch k.Type {
		case tea.KeyEsc:
			p.subs.local = false
			return nil
		case tea.KeyEnter:
			raw := strings.TrimSpace(p.subs.localIn.Value())
			raw = strings.Trim(raw, `"'`)
			if raw == "" {
				p.subs.local = false
				return nil
			}
			st, err := os.Stat(raw)
			if err != nil || st.IsDir() {
				a.notify("dosya bulunamadı: "+raw, "err")
				return a.tick()
			}
			ext := strings.ToLower(filepath.Ext(raw))
			if ext != ".srt" && ext != ".ass" && ext != ".ssa" && ext != ".vtt" {
				a.notify("desteklenen biçimler: .ass, .srt, .ssa, .vtt", "warn")
				return a.tick()
			}
			base := filepath.Base(raw)
			if p.sess != nil {
				p.sess.AddSub(raw, base, "tr")
			}
			p.activeSub = base
			p.closeSubs()
			a.notify("yerel altyazı yüklendi: "+base, "ok")
			a.flash()
			return a.tick()
		}
		p.subs.localIn.handle(k)
		return nil
	}

	s := k.String()
	switch s {
	case "esc", "y":
		p.closeSubs()
	case "l":
		p.subs.local = true
		p.subs.localIn.Set("")
		return a.tick()
	case "r":
		return p.searchSubs(a, p.subs.mpvOSD)
	case "z":
		return p.adjustSubDelay(a, -0.1, false)
	case "x":
		return p.adjustSubDelay(a, +0.1, false)
	case "Z":
		return p.adjustSubDelay(a, -0.5, false)
	case "X":
		return p.adjustSubDelay(a, +0.5, false)
	case "0":
		return p.adjustSubDelay(a, 0, true)
	case "up", "k":
		p.subs.cur = max(0, p.subs.cur-1)
		if p.subs.mpvOSD {
			p.syncMpvSubMenu()
		}
	case "down", "j":
		p.subs.cur = clamp(p.subs.cur+1, 0, len(p.subs.rs)-1)
		if p.subs.mpvOSD {
			p.syncMpvSubMenu()
		}
	case "enter":
		return p.pickSub(a, p.subs.cur)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		n := int(s[0] - '0')
		off := p.subWindowOffset(6)
		return p.pickSub(a, off+n-1)
	}
	return nil
}

func (p *playing) onSubs(a *App, msg tea.Msg) {
	switch m := msg.(type) {
	case subsMsg:
		if m.id != p.id {
			return
		}
		p.subs.busy = false
		if m.err != nil {
			p.subs.err = m.err.Error()
			if p.subs.open && p.subs.mpvOSD {
				p.syncMpvSubMenu()
			}
			return
		}
		p.subs.rs, p.subs.cur, p.subs.err = m.rs, 0, ""
		if len(m.rs) == 0 {
			p.subs.err = "bu bölüm için çevrimiçi altyazı bulunamadı"
		}
		if p.subs.open && p.subs.mpvOSD {
			p.syncMpvSubMenu()
		}
	case subGotMsg:
		if m.id != p.id {
			return
		}
		p.subs.busy = false
		if m.err != nil {
			a.notify("altyazı indirilemedi: "+m.err.Error(), "err")
			if p.sess != nil && p.subs.mpvOSD {
				p.sess.ShowOSD("✗ Altyazı indirilemedi: "+m.err.Error(), 3200)
				p.syncMpvSubMenu()
			}
			return
		}
		if p.sess != nil {
			p.sess.AddSub(m.path, m.r.Label(), m.r.Lang)
		}
		p.activeSub = m.r.Label()
		p.closeSubs()
		a.notify("altyazı eklendi: "+m.r.Label(), "ok")
		a.flash()
	}
}

func assEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `{`, `\{`)
	s = strings.ReplaceAll(s, `}`, `\}`)
	return s
}

// buildMpvSubASS formats the interactive ASS OSD menu drawn directly inside mpv.
func (p *playing) buildMpvSubASS() string {
	const (
		hdr   = `{\an7\pos(40,34)\fnSegoe UI\fs24\b1\bord2.2\shad1\c&HA88BF3&\3c&H1B1011&}`
		sub   = `{\fs18\b0\c&HC8ADB6&}`
		sel   = `{\fs21\b1\c&HAFE2F9&}`
		norm  = `{\fs20\b0\c&HF0E8E8&}`
		dim   = `{\fs17\b0\c&H9AA0B4&}`
		errC  = `{\fs20\b0\c&HA88BF3&}`
		okTag = `{\c&HA1E3A6&\b1}`
		enTag = `{\c&HFAE089&\b1}`
	)
	var b strings.Builder
	b.WriteString(hdr)
	b.WriteString("ANISUB ALTYAZI SEÇİCİ")
	b.WriteString(sub)
	b.WriteString("   ·   senkron: " + assEscape(player.FormatSubDelay(p.subDelay)))
	b.WriteString(`\N`)

	switch {
	case p.subs.busy && len(p.subs.rs) == 0:
		b.WriteString(norm + "◌  AniSub arşivi taranıyor…" + `\N`)
	case p.subs.err != "" && len(p.subs.rs) == 0:
		b.WriteString(errC + "✗  " + assEscape(p.subs.err) + `\N`)
	case p.subs.busy && len(p.subs.rs) > 0:
		b.WriteString(sel + "◌  Seçilen altyazı indiriliyor ve yükleniyor…" + `\N`)
	default:
		const limit = 8
		off := p.subWindowOffset(limit)
		end := min(len(p.subs.rs), off+limit)
		for i := off; i < end; i++ {
			r := p.subs.rs[i]
			num := i - off + 1
			langCol := enTag
			if strings.EqualFold(r.Lang, "tr") {
				langCol = okTag
			}
			lineStyle := norm
			prefix := fmt.Sprintf("   %d. ", num)
			if i == p.subs.cur {
				lineStyle = sel
				prefix = fmt.Sprintf("▸ %d. ", num)
			}
			rel := truncate(r.Release, 58)
			meta := ""
			if r.Provider != "" {
				meta = "  [" + r.Provider + "]"
			}
			b.WriteString(lineStyle + prefix)
			b.WriteString(langCol + strings.ToUpper(r.Lang) + " ")
			b.WriteString(lineStyle + assEscape(rel))
			if meta != "" {
				b.WriteString(dim + assEscape(meta))
			}
			b.WriteString(`\N`)
		}
		if len(p.subs.rs) > limit {
			b.WriteString(dim + fmt.Sprintf("   (%d / %d sonuç)", p.subs.cur+1, len(p.subs.rs)) + `\N`)
		}
	}
	b.WriteString(`\N` + dim + "↑↓ seç · Enter / 1-8 yükle · z/x senkron (±0,1s) · Z/X (±0,5s) · Alt+0 sıfırla · Esc kapat")
	return b.String()
}

func (p *playing) syncMpvSubMenu() {
	if p.sess == nil {
		return
	}
	p.sess.ShowSubMenu(p.buildMpvSubASS())
}

// subLines renders the redesigned bordered subtitle studio card inside the
// terminal playing view (both classic and Nova).
func (p *playing) subLines(a *App, w int) []string {
	if !p.subs.open {
		return nil
	}
	acc := a.posters.accent(p.an.Poster, cAccent).hex()
	innerW := max(24, w-4)
	title := "altyazı · AniSub"
	if strings.TrimSpace(a.cfg.OSKey) != "" {
		title = "altyazı · AniSub + OpenSub"
	}
	syncLbl := "senkron " + player.FormatSubDelay(p.subDelay)

	var body []string
	// Top fine-tuning strip — always visible so user sees sync controls immediately.
	syncCol := cMuted
	if math.Abs(p.subDelay) >= 0.05 {
		syncCol = cWarn
	}
	syncBar := fg(syncCol).Bold(math.Abs(p.subDelay) >= 0.05).Render("⏱ "+syncLbl) +
		stFaint.Render("   ") +
		keycap("z") + keycap("x") + " " + stMuted.Render("±0,1s") + "  " +
		keycap("Z") + keycap("X") + " " + stMuted.Render("±0,5s") + "  " +
		keycap("0") + " " + stMuted.Render("sıfırla")
	body = append(body, truncate(syncBar, innerW), stFaint.Render(strings.Repeat("─", innerW)))

	if p.subs.local {
		body = append(body,
			stText.Bold(true).Render("Yerel altyazı dosyası (.ass · .srt · .ssa · .vtt)"),
			stMuted.Render("Dosya yolunu yapıştır veya sürükle-bırak (tırnaklar otomatik temizlenir):"),
		)
		body = append(body, p.subs.localIn.view(max(16, innerW), true, a.frame)...)
		body = append(body, "", keycap("enter")+" "+stSub.Render("mpv'ye yükle")+"   "+keycap("esc")+" "+stMuted.Render("listeye dön"))
		return append([]string{""}, panel(title, body, w, len(body)+2, true, acc, syncLbl)...)
	}

	switch {
	case p.subs.busy && len(p.subs.rs) == 0:
		body = append(body,
			fg(acc).Render(spinner(a.frame/2))+stSub.Render(" AniSub arşivi taranıyor…"),
			stMuted.Render("Hazır dosyan varsa l tuşuyla hemen yükleyebilirsin."),
		)
	case p.subs.err != "" && len(p.subs.rs) == 0:
		body = append(body,
			stErr.Render("✗ "+truncate(p.subs.err, max(8, innerW-2))),
			stMuted.Render("r ile tekrar ara ya da l ile bilgisayarından .ass/.srt seç."),
		)
	}

	const limit = 6
	off := p.subWindowOffset(limit)
	for i := off; i < len(p.subs.rs) && i < off+limit; i++ {
		r := p.subs.rs[i]
		sel := i == p.subs.cur
		bg, bar := "", sp("  ", cText)
		if sel {
			bg, bar = cBand, sp("▌ ", acc)
		}
		lc := cSky
		if strings.EqualFold(r.Lang, "tr") {
			lc = cOK
		}
		num := fmt.Sprintf("%d ", i-off+1)
		var right []span
		if r.Provider != "" {
			pc := cAccent2
			if r.Provider == "AniSub" {
				pc = cOK
			}
			right = append(right, sp(r.Provider+" ", pc))
		}
		if r.Downloads > 0 {
			right = append(right, sp(fmt.Sprintf("↓%d ", r.Downloads), cMuted))
		}
		left := []span{
			bar,
			sp(num, cMuted),
			spb(strings.ToUpper(r.Lang)+" ", lc),
			{t: r.Release, c: cText, bold: sel},
		}
		body = append(body, row(innerW, bg, left, right))
	}

	if len(p.subs.rs) > limit {
		body = append(body, stFaint.Render(fmt.Sprintf("  %d / %d sonuç · ↑↓ ile kaydır", p.subs.cur+1, len(p.subs.rs))))
	}

	body = append(body, "")
	if p.subs.busy && len(p.subs.rs) > 0 {
		body = append(body, fg(acc).Render(spinner(a.frame/2))+stSub.Render(" indiriliyor ve mpv'ye yükleniyor…"))
	} else {
		ctrls := keycap("enter") + " " + stSub.Render("yükle") + "  " +
			keycap("1-6") + " " + stMuted.Render("hızlı seç") + "  " +
			keycap("l") + " " + stSub.Render("yerel dosya") + "  " +
			keycap("r") + " " + stMuted.Render("yenile") + "  " +
			keycap("esc") + " " + stMuted.Render("kapat")
		if lipgloss.Width(ctrls) > innerW {
			ctrls = keycap("enter") + " " + stSub.Render("yükle") + "  " +
				keycap("l") + " " + stSub.Render("yerel") + "  " +
				keycap("esc") + " " + stMuted.Render("kapat")
		}
		body = append(body, ctrls)
	}

	return append([]string{""}, panel(title, body, w, len(body)+2, true, acc, syncLbl)...)
}
