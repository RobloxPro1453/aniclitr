package ui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// The command palette (ctrl+k): one fuzzy box for everything — screens,
// shelves, history, themes (live preview while browsing), toggles, toys.

type palItem struct {
	icon    string
	label   string
	group   string
	note    string
	run     func(a *App) tea.Cmd
	preview *Theme // highlighting it previews this theme live
}

type palHit struct {
	it    palItem
	score int
	pos   map[int]bool
}

type palette struct {
	in       input
	items    []palItem
	hits     []palHit
	cur      int
	gl       glide
	openT    tween
	orig     Theme
	origName string
	previewd bool
}

func (a *App) openPalette(prefix string) {
	p := &palette{in: input{placeholder: "komut, anime, tema…  (ör. tema, kitaplık, frieren)"}, orig: currentTheme(), origName: a.themeName}
	p.orig.Name = a.themeName
	if a.motion() {
		p.openT = newTween(220 * time.Millisecond)
	}
	p.items = a.paletteItems()
	p.in.Set(prefix)
	p.filter(a)
	a.pal = p
}

func onOff(b bool) string {
	if b {
		return "kapat"
	}
	return "aç"
}

func (a *App) paletteItems() []palItem {
	var out []palItem
	add := func(icon, label, group, note string, run func(a *App) tea.Cmd) {
		out = append(out, palItem{icon: icon, label: label, group: group, note: note, run: run})
	}
	add(ic("home"), "Ana sayfa", "ekran", "", func(a *App) tea.Cmd { a.stack = a.stack[:1]; a.slide(-1); return a.tick() })
	add(ic("library"), "Kitaplık", "ekran", "ctrl+b", func(a *App) tea.Cmd { return a.openLibrary("") })
	add(ic("feed"), "Keşfet: takvim · sezon · öneriler · rastgele", "ekran", "ctrl+e", func(a *App) tea.Cmd { return a.openDiscover() })
	add("↓", "İndirilenler", "ekran", "ctrl+d", func(a *App) tea.Cmd { return a.openDownloads() })
	add(ic("archive"), "İndirme klasörünü aç", "indirme", "", func(a *App) tea.Cmd {
		_ = os.MkdirAll(a.dl.Dir(), 0o755)
		_ = exec.Command("explorer", a.dl.Dir()).Start()
		return nil
	})
	add(ic("star"), "MyAnimeList", "ekran", "bağlan / eşitle", func(a *App) tea.Cmd { return a.push(newMalScreen(a)) })
	if a.mal.Connected() {
		add(ic("star"), "MAL ile şimdi eşitle", "mal", "", func(a *App) tea.Cmd { a.msync.SyncNow(); return a.tick() })
	}
	add("●", "Kaynak durumu", "ekran", "sağlık", func(a *App) tea.Cmd { return tea.Batch(a.push(&sourcesScreen{}), a.checkHealth()) })
	add(ic("settings"), "Ayarlar", "ekran", "ctrl+o", func(a *App) tea.Cmd { return a.push(newSettings()) })
	add("?", "Klavye kısayolları", "ekran", "?", func(a *App) tea.Cmd { a.help = true; return nil })
	for _, st := range store.Statuses {
		st := st
		add(statusIcon(st), "Kitaplık › "+st, "raf", "", func(a *App) tea.Cmd { return a.openLibrary(st) })
	}
	add("◈", "Kitaplık › İstatistikler", "raf", "i", func(a *App) tea.Cmd { return a.openLibrary("stats") })
	for _, l := range a.lib.ListNames() {
		l := l
		add(listIcon(l), "Liste › "+l, "liste", "", func(a *App) tea.Cmd { return a.openLibrary("list:" + l) })
	}
	seen := map[string]bool{}
	for i, e := range a.recent() {
		if i >= 25 {
			break
		}
		e := e
		seen[e.Anime().Key()] = true
		pos := fmt.Sprintf("S%d·B%d", e.Season, e.Episode)
		add(ic("play"), e.Title, "devam", pos, func(a *App) tea.Cmd {
			s, ep := e.Season, e.Episode
			if e.Watched[store.EpKey(s, ep)] {
				ep++
			}
			sc := newAnime(e.Anime(), a.siblings(e.Anime()), s, ep)
			return tea.Batch(a.push(sc), sc.start(a))
		})
	}
	for _, it := range a.lib.All() {
		if seen[it.Key()] {
			continue
		}
		it := it
		add(statusIcon(it.Status), it.Title, "kitaplık", it.Status, func(a *App) tea.Cmd {
			return a.openItem(it)
		})
	}
	for _, t := range themes() {
		t := t
		note := ""
		if strings.EqualFold(t.Name, a.themeName) {
			note = "şu an"
		}
		out = append(out, palItem{icon: ic("theme"), label: "Tema: " + t.Name, group: "tema", note: note, preview: &t,
			run: func(a *App) tea.Cmd {
				a.cfg.Theme = t.Name
				_ = a.cfg.Save()
				a.notify("tema: "+t.Name, "ok")
				return a.tick()
			}})
	}
	for _, k := range ambientKinds {
		k := k
		note := ""
		if k == a.cfg.Ambient {
			note = "şu an"
		}
		label := "Ambiyans: " + k
		if k == "auto" {
			label = "Ambiyans: otomatik (mevsim ve saate göre)"
		}
		add("◇", label, "görünüm", note, func(a *App) tea.Cmd {
			a.cfg.Ambient = k
			_ = a.cfg.Save()
			return a.tick()
		})
	}
	for _, k := range introKinds {
		k := k
		add(ic("spark"), "Logo efekti: "+k, "görünüm", "", func(a *App) tea.Cmd {
			a.cfg.Intro = k
			_ = a.cfg.Save()
			a.replayIntro()
			return a.tick()
		})
	}
	add(ic("spark"), "Logoyu yeniden oynat", "oyuncak", "", func(a *App) tea.Cmd { a.replayIntro(); return a.tick() })
	add("◆", "Konfeti!", "oyuncak", "", func(a *App) tea.Cmd { a.anchorOK = false; a.celebrate(90); return a.tick() })
	add(ic("clock"), "Ekran koruyucuyu başlat", "oyuncak", "", func(a *App) tea.Cmd {
		a.saver, a.saverAt = true, time.Now()
		return a.tick()
	})
	add("◌", "Animasyonları "+onOff(a.cfg.Motion), "ayar", "", func(a *App) tea.Cmd {
		a.cfg.Motion = !a.cfg.Motion
		_ = a.cfg.Save()
		return a.tick()
	})
	add(ic("archive"), "Arşiv modunu "+onOff(a.cfg.Legacy), "ayar", "alt+a", func(a *App) tea.Cmd { return toggleLegacy(a) })
	add("◆", "Nerd Font ikonlarını "+onOff(nerd), "ayar", "", func(a *App) tea.Cmd {
		nerd = !nerd
		a.cfg.Icons = map[bool]string{true: "nerd", false: "unicode"}[nerd]
		_ = a.cfg.Save()
		return nil
	})
	add("▤", "Sıkı düzeni "+onOff(a.cfg.Compact), "ayar", "", func(a *App) tea.Cmd {
		a.cfg.Compact = !a.cfg.Compact
		_ = a.cfg.Save()
		return nil
	})
	add("▅", "İndirme hız grafiğini "+onOff(a.cfg.SpeedGraph), "ayar", "", func(a *App) tea.Cmd {
		a.cfg.SpeedGraph = !a.cfg.SpeedGraph
		_ = a.cfg.Save()
		return nil
	})
	add("◐", "Terminal zeminini temaya boyamayı "+onOff(a.cfg.ThemeBg), "ayar", "", func(a *App) tea.Cmd {
		a.cfg.ThemeBg = !a.cfg.ThemeBg
		_ = a.cfg.Save()
		a.oscUntil = time.Now().Add(300 * time.Millisecond) // paint or restore
		return a.tick()
	})
	add(ic("tv"), "Kapak görsellerini "+onOff(a.cfg.Posters), "ayar", "", func(a *App) tea.Cmd {
		a.cfg.Posters = !a.cfg.Posters
		a.applyGraphics()
		_ = a.cfg.Save()
		return nil
	})
	add(ic("exit"), "Çık", "aniclitr", "ctrl+c", func(a *App) tea.Cmd { return tea.Quit })
	return out
}

// ---- fuzzy matching (Turkish-aware, one rune in = one rune out) ----

func foldRune(r rune) rune {
	switch r {
	case 'İ', 'I', 'ı', 'î', 'Î':
		return 'i'
	case 'Ç', 'ç':
		return 'c'
	case 'Ğ', 'ğ':
		return 'g'
	case 'Ö', 'ö':
		return 'o'
	case 'Ş', 'ş':
		return 's'
	case 'Ü', 'ü', 'û':
		return 'u'
	case 'â', 'Â':
		return 'a'
	}
	return unicode.ToLower(r)
}

func foldRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = foldRune(r)
	}
	return rs
}

// fuzzy scores q against s; every query word must match as a subsequence.
func fuzzy(q, s string) (int, map[int]bool) {
	words := strings.Fields(string(foldRunes(q)))
	if len(words) == 0 {
		return 1, nil
	}
	t := foldRunes(s)
	pos := map[int]bool{}
	total := 0
	for _, w := range words {
		wr := []rune(w)
		// contiguous match wins big, at a word start even more
		if k := indexRunes(t, wr); k >= 0 {
			sc := 30 + len(wr)*4
			if k == 0 || !unicode.IsLetter(t[k-1]) {
				sc += 20
			}
			for i := range wr {
				pos[k+i] = true
			}
			total += sc - k/4
			continue
		}
		j, sc, prev, first := 0, 0, -2, -1
		for i := 0; i < len(t) && j < len(wr); i++ {
			if t[i] != wr[j] {
				continue
			}
			sc += 2
			if i == prev+1 {
				sc += 5
			}
			if i == 0 || !unicode.IsLetter(t[i-1]) {
				sc += 6
			}
			pos[i] = true
			if first < 0 {
				first = i
			}
			prev = i
			j++
		}
		if j < len(wr) || prev-first+1 > len(wr)*2 { // scattered letters are noise
			return 0, nil
		}
		total += sc
	}
	return total, pos
}

func indexRunes(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		ok := true
		for j := range sub {
			if s[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func (p *palette) filter(a *App) {
	q := strings.TrimSpace(p.in.Value())
	p.hits = p.hits[:0]
	var search []palHit
	if q != "" && len([]rune(q)) >= 2 && !strings.HasPrefix(string(foldRunes(q)), "tema") {
		qq := q
		if source.IsURL(qq) {
			search = append(search, palHit{it: palItem{icon: ic("play"), label: "Bağlantıyı aç: " + qq, group: "bağlantı", run: func(a *App) tea.Cmd {
				return a.openURL(qq)
			}}, score: 100})
		} else {
			search = append(search, palHit{it: palItem{icon: ic("search"), label: "“" + qq + "” ara", group: "arama", run: func(a *App) tea.Cmd {
				r := newResults(qq)
				return tea.Batch(a.push(r), r.start(a))
			}}, score: 1})
		}
	}
	var hs []palHit
	for _, it := range p.items {
		sc, pos := fuzzy(q, it.label+" "+it.group)
		if sc <= 0 {
			continue
		}
		// keep only positions inside the label (the group is matched silently)
		lp := map[int]bool{}
		n := len([]rune(it.label))
		for k := range pos {
			if k < n {
				lp[k] = true
			}
		}
		hs = append(hs, palHit{it: it, score: sc, pos: lp})
	}
	if q != "" {
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].score > hs[j].score })
	}
	// strong matches first; otherwise "search the sites for it" leads
	if len(hs) > 0 && hs[0].score >= 40 {
		p.hits = append(append(p.hits, hs...), search...)
	} else {
		p.hits = append(append(p.hits, search...), hs...)
	}
	p.cur = 0
	// the theme picker opens on the current theme
	for i, h := range p.hits {
		if h.it.preview != nil && strings.EqualFold(h.it.preview.Name, a.themeName) && strings.HasPrefix(string(foldRunes(q)), "tema") {
			p.cur = i
			break
		}
	}
	p.preview(a)
}

// preview applies the highlighted theme (instantly; reverted on esc).
func (p *palette) preview(a *App) {
	if p.cur < len(p.hits) && p.hits[p.cur].it.preview != nil {
		t := *p.hits[p.cur].it.preview
		if t.Name != a.themeName {
			a.setTheme(t)
		}
		p.previewd = true
		return
	}
	if p.previewd && a.themeName != p.origName {
		a.setTheme(p.orig)
	}
}

func (p *palette) close(a *App, keepTheme bool) {
	if !keepTheme && p.previewd && a.themeName != p.origName {
		a.setTheme(p.orig)
	}
	a.pal = nil
	a.gfx.invalidate()
}

func (p *palette) update(a *App, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+k", "ctrl+t":
		p.close(a, false)
		return nil
	case "up", "ctrl+p", "shift+tab":
		if p.cur > 0 {
			p.cur--
			p.preview(a)
		}
		return nil
	case "down", "ctrl+n", "tab":
		if p.cur < len(p.hits)-1 {
			p.cur++
			p.preview(a)
		}
		return nil
	case "pgdown":
		p.cur = clamp(p.cur+8, 0, len(p.hits)-1)
		p.preview(a)
		return nil
	case "pgup":
		p.cur = max(0, p.cur-8)
		p.preview(a)
		return nil
	case "enter":
		if p.cur >= len(p.hits) {
			return nil
		}
		it := p.hits[p.cur].it
		p.close(a, it.preview != nil)
		a.flash()
		return it.run(a)
	}
	before := p.in.Value()
	p.in.handle(k)
	if p.in.Value() != before {
		p.filter(a)
	}
	return nil
}

// draw places the palette near the top; the background dims except while
// a theme is being previewed (so the new colours can be judged).
func (p *palette) draw(a *App, lines []string) []string {
	w := min(70, a.w-6)
	inner := w - 4
	maxRows := min(12, len(lines)-8)
	if maxRows < 3 {
		maxRows = 3
	}
	acc := cAccent
	bs := fg(mix(hex(cFaint), hex(acc), .7).hex())
	var box []string
	box = append(box, bs.Render("╭"+strings.Repeat("─", w-2)+"╮"))
	iv := p.in.view(inner+4, true, a.frame)[1] // the input's middle row (has its own frame chars)
	iv = ansi.TruncateLeft(iv, 1, "")
	iv = ansi.Truncate(iv, inner+2, "")
	box = append(box, bs.Render("│")+" "+fit(iv, inner+1)+bs.Render("│"))
	box = append(box, bs.Render("├"+strings.Repeat("─", w-2)+"┤"))
	off := 0
	if p.cur >= maxRows {
		off = p.cur - maxRows + 1
	}
	sel := p.gl.row(a, p.cur)
	if len(p.hits) == 0 {
		box = append(box, bs.Render("│")+" "+fit(stMuted.Render("  eşleşme yok — enter ile aramayı dene"), inner+1)+bs.Render("│"))
	}
	lastGroup := ""
	for i := off; i < len(p.hits) && i < off+maxRows; i++ {
		h := p.hits[i]
		isSel := i == sel
		bg, bar := "", sp("  ", cText)
		if isSel {
			bg, bar = cBand, sp("▌ ", acc)
		}
		spans := []span{bar, sp(h.it.icon+" ", cMuted)}
		// label with matched letters highlighted
		for j, r := range []rune(h.it.label) {
			c := cText
			if h.pos[j] {
				c = acc
			}
			spans = append(spans, span{t: string(r), c: c, bold: isSel || h.pos[j]})
		}
		right := []span{}
		if h.it.note != "" {
			right = append(right, sp(h.it.note+"  ", cSub))
		}
		g := h.it.group
		if g == lastGroup && !isSel {
			g = strings.Repeat(" ", lipgloss.Width(g))
		}
		lastGroup = h.it.group
		right = append(right, sp(g+" ", cFaint))
		if isSel {
			right[len(right)-1] = sp(h.it.group+" ", cMuted)
		}
		box = append(box, bs.Render("│")+row(w-2, bg, spans, right)+bs.Render("│"))
	}
	more := ""
	if n := len(p.hits) - (off + maxRows); n > 0 {
		more = fmt.Sprintf("+%d daha", n)
	}
	foot := stText.Bold(true).Render("↑↓") + stMuted.Render(" seç  ") + stText.Bold(true).Render("enter") + stMuted.Render(" çalıştır  ") + stText.Bold(true).Render("esc") + stMuted.Render(" kapat")
	box = append(box, bs.Render("│")+" "+row2(foot, stFaint.Render(more), inner+1)+bs.Render("│"))
	box = append(box, bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))

	previewing := p.cur < len(p.hits) && p.hits[p.cur].it.preview != nil
	out := append([]string(nil), lines...)
	if !previewing {
		out = dim(out)
	}
	// drop in from above with a little overshoot
	y := 1
	if p.openT.active() {
		y = int(float64(-len(box)/3) + easeOutBack(p.openT.p())*float64(1+len(box)/3))
	}
	return overlayAt(out, box, (a.w-w)/2, y, a.w)
}

// overlayAt draws box with its top-left at (x,y) (rows above 0 are clipped).
func overlayAt(lines, box []string, x, y, w int) []string {
	for i, b := range box {
		j := y + i
		if j < 0 || j >= len(lines) {
			continue
		}
		l := fit(lines[j], w)
		bw := lipgloss.Width(b)
		lines[j] = ansi.Truncate(l, x, "") + "\x1b[0m" + b + "\x1b[0m" + ansi.TruncateLeft(l, x+bw, "")
	}
	return lines
}
