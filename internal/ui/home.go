package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

const (
	dropStagger = 75 * time.Millisecond
	dropDur     = 700 * time.Millisecond
	landedAt    = 7*dropStagger + dropDur // last letter lands
	spotEvery   = 6 * time.Second
)

var introKinds = []string{"düşüş", "glitch", "matrix", "rastgele"}

// pickIntro resolves "rastgele" to one of the effects.
func pickIntro(kind string, now time.Time) string {
	switch kind {
	case "düşüş", "glitch", "matrix":
		return kind
	}
	return introKinds[int(now.UnixNano()/1e6)%3]
}

func introDur(kind string) time.Duration {
	switch kind {
	case "glitch":
		return 850 * time.Millisecond
	case "matrix":
		return 1300 * time.Millisecond
	}
	return landedAt
}

// replayIntro plays the logo entrance again (palette toy).
func (a *App) replayIntro() {
	prev := a.intro
	a.intro = pickIntro(a.cfg.Intro, time.Now())
	for a.cfg.Intro == "rastgele" && a.intro == prev {
		a.intro = introKinds[(indexOf(introKinds, prev)+1)%3]
	}
	a.start = time.Now()
	a.stack = a.stack[:1]
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return 0
}

type home struct {
	in      input
	focus   int // 0 search box, 1 menu
	zones   []zone
	inX     int // search cursor cell (body coords) for typing sparks
	inY     int
	mCur    int   // menu cursor (index into items)
	mGl     glide // the sliding selection band
	shelfN  int   // continue cards that fit (set by the last view)
	feed    []source.FeedItem
	feedErr error
	feedOK  bool
	feedAt  time.Time

	wx               weather
	sug              suggest
	sugX, sugY, sugW int
	feedCached       bool
	news             []newEp // new episodes on shows you follow
}

func newHome(a *App) *home {
	return &home{in: input{placeholder: "anime ara…   frieren, one piece, mouryou no hako"}, shelfN: 3, mCur: -1}
}

func (h *home) crumb() string { return "" }

func (h *home) typing() bool { return h.focus == 0 }

func (h *home) animating(a *App) bool {
	if a.top() != screen(h) {
		return false
	}
	if !h.feedOK {
		return true
	}
	return a.motion() // weather, logo, spotlight rotation
}

func (h *home) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case feedMsg:
		if m.cached && h.feedOK {
			return nil // the live feed already arrived
		}
		if m.err != nil && len(h.feed) > 0 {
			// offline: keep showing the cached feed
			a.notify("akış yenilenemedi — son kayıtlı akış gösteriliyor", "warn")
			a.addNotice(notice{key: "offline", kind: "warn", text: "çevrimdışısın — indirdiklerini internetsiz izleyebilirsin",
				action: func(a *App) tea.Cmd { return a.openDownloads() }})
			h.feedOK = true
			return nil
		}
		if m.err != nil {
			a.addNotice(notice{key: "offline", kind: "warn", text: "çevrimdışısın — indirdiklerini internetsiz izleyebilirsin",
				action: func(a *App) tea.Cmd { return a.openDownloads() }})
		}
		h.feed, h.feedErr, h.feedOK, h.feedAt = m.items, m.err, true, time.Now()
		if m.cached {
			h.feedOK = false // still waiting for the live one (skeleton stays off: we have rows)
			h.feedCached = true
		}
		h.news = newEpisodes(a, h.feed)
		for _, e := range h.news {
			e := e
			label := fmt.Sprintf("%s · %s. bölüm çıktı", e.an.Title, source.EpNum(e.episode))
			if e.season > 1 {
				label = fmt.Sprintf("%s · S%d %s. bölüm çıktı", e.an.Title, e.season, source.EpNum(e.episode))
			}
			a.addNotice(notice{key: fmt.Sprintf("ep:%s:%d:%d", e.an.Key(), e.season, e.episode), kind: "new", text: label,
				action: func(a *App) tea.Cmd {
					sc := newAnime(e.an, nil, e.season, e.episode)
					return tea.Batch(a.push(sc), sc.start(a))
				}})
		}
		var cmds []tea.Cmd
		for i, it := range h.feed {
			if i < 10 && a.cfg.Posters {
				cmds = append(cmds, a.posters.want(it.Anime.Poster))
			}
		}
		return tea.Batch(append(cmds, h.prefetchShelf(a))...)
	case tea.MouseMsg:
		if z, ok := h.zoneAt(m.X, m.Y); ok && m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			return h.click(a, z)
		}
		if m.Action == tea.MouseActionPress && (m.Button == tea.MouseButtonWheelUp || m.Button == tea.MouseButtonWheelDown) {
			d := 1
			if m.Button == tea.MouseButtonWheelUp {
				d = -1
			}
			if h.focus == 1 {
				_, items := h.selItem(a)
				h.mCur = menuMove(items, h.mCur, d)
			}
		}
	case sugFireMsg:
		return h.sug.fire(a, m.gen)
	case sugResMsg:
		h.sug.result(m)
		return a.tick()
	case detailsMsg:
		if d, ok := a.details[m.key]; ok && d.Poster != "" && a.cfg.Posters {
			return a.posters.want(d.Poster)
		}
	case tea.KeyMsg:
		return tea.Batch(h.key(a, m), h.prefetchShelf(a))
	}
	return nil
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- menu dock: the screens are reachable without knowing any shortcut ----

type zone struct {
	kind           string // "dock" | "shelf" | "feed"
	idx            int
	x0, x1, y0, y1 int // screen cells, inclusive x0/y0, exclusive x1/y1
}

func (h *home) zoneAt(x, y int) (zone, bool) {
	for _, z := range h.zones {
		if x >= z.x0 && x < z.x1 && y >= z.y0 && y < z.y1 {
			return z, true
		}
	}
	return zone{}, false
}

type dockItem struct {
	icon, label, key string
	run              func(a *App, h *home) tea.Cmd
}

func dockItems() []dockItem {
	return []dockItem{
		{ic("search"), "Ara", "/", func(a *App, h *home) tea.Cmd { h.focus = 0; return nil }},
		{ic("library"), "Kitaplık", "ctrl+b", func(a *App, h *home) tea.Cmd { return a.openLibrary("") }},
		{ic("feed"), "Keşfet", "ctrl+e", func(a *App, h *home) tea.Cmd { return a.openDiscover() }},
		{"↓", "İndirilenler", "ctrl+d", func(a *App, h *home) tea.Cmd { return a.openDownloads() }},
		{ic("theme"), "Tema", "ctrl+t", func(a *App, h *home) tea.Cmd { a.openPalette("tema "); return a.tick() }},
		{ic("palette"), "Komutlar", "ctrl+k", func(a *App, h *home) tea.Cmd { a.openPalette(""); return a.tick() }},
		{ic("settings"), "Ayarlar", "ctrl+o", func(a *App, h *home) tea.Cmd { return a.push(newSettings()) }},
		{"?", "Yardım", "?", func(a *App, h *home) tea.Cmd { a.help = true; return nil }},
		{ic("spark"), "Sürpriz", "", func(a *App, h *home) tea.Cmd { return surprise(a) }},
	}
}

// surprise: a random toy — fireworks, a new theme, the logo dance…
func surprise(a *App) tea.Cmd {
	n := int(time.Now().UnixNano()/1e6) % 4
	switch n {
	case 0:
		for i := 0; i < 4; i++ {
			a.fx.firework(float64(a.w/5*(i+1)), float64(4+i%2*3), 22, []string{cAccent, cAccent2, cSky, cWarn, cOK}, a.frame+i*7)
		}
	case 1:
		ts := themes()
		t := ts[(indexOfTheme(ts, a.themeName)+1+int(time.Now().UnixNano()%int64(len(ts)-1)))%len(ts)]
		a.setTheme(t)
		a.cfg.Theme = t.Name
		_ = a.cfg.Save()
		a.notify("tema: "+t.Name, "ok")
	case 2:
		a.replayIntro()
	default:
		a.anchorOK = false
		a.celebrate(90)
	}
	a.logoKick = newTween(1400 * time.Millisecond)
	return a.tick()
}

func indexOfTheme(ts []Theme, name string) int {
	for i, t := range ts {
		if t.Name == name {
			return i
		}
	}
	return 0
}

func (h *home) key(a *App, k tea.KeyMsg) tea.Cmd {
	recent := a.recent()
	if h.focus == 0 && h.sug.open() {
		if cmd, ok := h.sug.key(a, h, k); ok {
			return cmd
		}
	}
	switch k.String() {
	case "ctrl+o":
		return a.push(newSettings())
	case "alt+a":
		return toggleLegacy(a)
	case "tab", "shift+tab":
		h.focus = 1 - h.focus
		return nil
	case "esc":
		switch {
		case h.focus != 0:
			h.focus = 0
		case h.in.Value() != "":
			h.in.Set("")
			h.sug.close()
		default:
			h.focus = 1 // out of the search box: single keys are shortcuts again
		}
		return nil
	}
	if h.focus != 0 {
		if a.nova() {
			if cmd, ok := h.novaKey(a, k); ok {
				return cmd
			}
		}
		return h.menuKey(a, k)
	}
	switch k.Type {
	case tea.KeyEnter:
		q := CleanQuery(h.in.Value())
		if q == "" && len(recent) > 0 {
			// empty box + enter: carry on with the last thing you watched
			return continueEntry(a, recent[0])
		}
		if source.IsURL(q) {
			a.flash()
			h.sug.close()
			h.in.Set("")
			return a.openURL(q)
		}
		if len([]rune(q)) < 2 {
			a.notify("en az 2 harf yaz", "warn")
			return a.tick()
		}
		a.flash()
		h.sug.close()
		a.logoKick = newTween(1200 * time.Millisecond)
		r := newResults(q)
		return tea.Batch(a.push(r), r.start(a))
	case tea.KeyDown:
		h.focus = 1
		_, items := h.selItem(a)
		h.mCur = menuFirst(items)
		return nil
	}
	before := h.in.Value()
	h.in.handle(k)
	if h.in.Value() == before {
		return nil
	}
	if k.Type == tea.KeyRunes && a.motion() {
		// every keystroke fizzes a little
		a.fx.sparks(float64(h.inX), float64(h.inY), 4, brand, a.frame+len(before))
	}
	return h.sug.changed(a, h.in.Value())
}

// ---- view ----

func (h *home) view(a *App, w, ht int) string {
	var s string
	if a.nova() {
		s = h.novaView(a, w, ht)
	} else {
		s = h.viewBase(a, w, ht)
	}
	if h.focus != 0 || !h.sug.open() {
		return s
	}
	// the dropdown covers what is below the search box (pictures hide meanwhile)
	lines := strings.Split(s, "\n")
	box := h.sug.box(a, h.sugW)
	a.cover(h.sugX, h.sugY, h.sugW, len(box)) // only the pictures under it hide
	return strings.Join(overlayAt(lines, box, h.sugX, h.sugY, w), "\n")
}

// hero: the wordmark enters (bouncing drop, glitch or matrix rain), then
// lives: the gradient flows, a light sweeps across every few seconds, the
// letters hop in a wave now and then, and a rippling reflection sits under
// it — with the ambient weather drifting around.
func (h *home) hero(a *App, w, ht int) []string {
	c := newCanvas(w, ht)
	el := time.Since(a.start)
	motion := a.motion()
	if !motion {
		el = time.Hour
	}
	kind := ambientNow(a.cfg.Ambient, time.Now())
	if motion && kind != "yok" {
		h.wx.step(kind, w, ht, 7)
		h.wx.draw(c)
	}
	lx := (w - logoWidth) / 2
	ly := 1
	intro := a.intro
	end := introDur(intro)
	live := el - end // time since the intro finished
	white := hex("#FFFFFF")
	shadow := mix(hex(cBg), hex(cAccent2), .4)
	flow := 0.0
	if motion {
		flow = el.Seconds() * .07
	}
	cyc := append(append([]string{}, brand...), brand[0]) // cyclic: the flow never jumps
	// a light sweep right after the intro, then every 7 s
	shimmer := -1.0
	if live > 0 {
		shimmer = float64(live%(7*time.Second)) / float64(900*time.Millisecond)
	}
	// letter wave every 11 s (and on demand, e.g. after a search)
	wave := -1.0
	if live > 3*time.Second && motion {
		wave = float64((live-3*time.Second)%(11*time.Second)) / float64(time.Second)
	}
	if a.logoKick.active() {
		wave = a.logoKick.p() * 1.4
	}
	hop := func(li int) int {
		if wave < 0 {
			return 0
		}
		t := (wave - float64(li)*.07) / .32
		if t <= 0 || t >= 1 {
			return 0
		}
		return -int(math.Round(math.Sin(t*math.Pi) * 1))
	}
	gt01 := float64(el) / float64(end)
	bucket := int(el / (45 * time.Millisecond))
	glitchCols := []string{cAccent, cSky, cErr, cAccent2, cOK}
	katakana := []rune("ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ01")
	if intro == "matrix" && el < end {
		// rain columns over the logo area; heads reveal the letters
		for x := -2; x < logoWidth+2; x++ {
			delay := hash(x*31+5) * .45
			head := (el.Seconds() - delay) * 16
			for y := 0; y < ht; y++ {
				d := head - float64(y)
				if d < 0 || d > 6 {
					continue
				}
				r := katakana[int(hash(x*7+y*13+bucket)*float64(len(katakana)))%len(katakana)]
				col := mix(hex(cBg), hex(cOK), 1-d/6)
				if d < 1 {
					col = mix(white, hex(cOK), .3)
				}
				c.set(lx+x, y, r, col, d < 1)
			}
		}
	}
	colorAt := func(x, y int, r rune) rgb {
		gt := (float64(x)+float64(y)*1.5)/float64(logoWidth+8)*.8 + flow
		col := gradAt(cyc, gt-math.Floor(gt))
		if r != '█' {
			col = mix(shadow, col, .35)
		}
		if shimmer > 0 && shimmer < 1 {
			lin := (float64(x) + float64(y)*1.5) / float64(logoWidth+8)
			if d := math.Abs(lin - (shimmer*1.6 - .3)); d < .1 {
				col = mix(col, white, (1-d/.1)*.8)
			}
		}
		return col
	}
	for y, line := range logoArt {
		dx := 0
		if intro == "glitch" && el < end {
			dx = int(math.Round((hash(y*7+bucket*3) - .5) * 10 * (1 - gt01)))
		}
		for x, r := range []rune(line) {
			if r == ' ' {
				continue
			}
			col := colorAt(x, y, r)
			li := letterAt(x)
			cy := ly + y
			switch intro {
			case "matrix":
				delay := hash(x*31+5) * .45
				head := (el.Seconds() - delay) * 16
				if head < float64(cy)+1 {
					continue
				}
				if d := head - float64(cy); d < 4 {
					col = mix(white, col, d/4)
				}
			case "glitch":
				if el < end {
					hv := hash(x*13 + y*29 + bucket*7)
					if hv > gt01+.15 {
						continue
					}
					if hv > gt01*.9 {
						r = []rune("▓▒░█")[int(hv*40)%4]
						col = hex(glitchCols[int(hv*97)%len(glitchCols)])
					}
				}
			default:
				t := float64(el-time.Duration(li)*dropStagger) / float64(dropDur)
				if t <= 0 {
					continue
				}
				cy += int(math.Round((1 - easeOutBounce(t)) * -float64(ly+8)))
				if cy < 0 {
					continue
				}
			}
			c.set(lx+x+dx, cy+hop(li), r, col, false)
		}
	}
	return c.lines()
}

func renderSpans(ss []span) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.style("").Render(s.t))
	}
	return b.String()
}

var trDays = []string{"Paz", "Pzt", "Sal", "Çar", "Per", "Cum", "Cmt"}

// calendar: a live weekly schedule inferred from the feed — what aired on
// each weekday during the last 7 days is what to expect this week. Titles
// you follow are coloured; today's already-released ones get a tick.
func (h *home) calendar(a *App, w, ht int) []string {
	if len(a.schedule) > 0 {
		return calendarAniList(a, a.schedule, w, ht)
	}
	if !h.feedOK {
		return []string{stMuted.Render(spinner(a.frame/2) + " yükleniyor…")}
	}
	now := time.Now()
	type ent struct {
		title string
		mine  bool
		done  bool
	}
	byDay := map[time.Weekday][]ent{}
	seen := map[string]bool{}
	for _, it := range h.feed {
		if it.Released.IsZero() || now.Sub(it.Released) > 7*24*time.Hour || it.Released.After(now.Add(time.Hour)) {
			continue
		}
		rel := it.Released.Local()
		k := fmt.Sprintf("%d|%s", rel.Weekday(), it.Anime.Key())
		if seen[k] {
			continue
		}
		seen[k] = true
		_, mine := a.libItem(it.Anime)
		if _, ok := a.hist.Get(it.Anime.Key()); ok {
			mine = true
		}
		done := rel.YearDay() == now.YearDay() && rel.Year() == now.Year()
		byDay[rel.Weekday()] = append(byDay[rel.Weekday()], ent{it.Anime.Title, mine, done})
	}
	var out []string
	for d := 0; d < 7 && len(out) < ht; d++ {
		day := now.AddDate(0, 0, d)
		label := trDays[day.Weekday()]
		lc := cMuted
		switch d {
		case 0:
			label, lc = "Bugün", cAccent
		case 1:
			label = "Yarın"
		}
		es := byDay[day.Weekday()]
		// yours first
		var mine, other []ent
		for _, e := range es {
			if e.mine {
				mine = append(mine, e)
			} else {
				other = append(other, e)
			}
		}
		es = append(mine, other...)
		var b strings.Builder
		used := 0
		avail := w - 7
		shown := 0
		for _, e := range es {
			t := e.title
			if e.done && d == 0 {
				t = "✓ " + t
			}
			tw := lipgloss.Width(t) + 3
			if used+tw > avail-3 && shown > 0 {
				break
			}
			if shown > 0 {
				b.WriteString(stFaint.Render(" · "))
			}
			c := cSub
			if e.mine {
				c = cAccent2
			}
			if e.done && d == 0 {
				c = cOK
			}
			b.WriteString(fg(c).Render(truncate(t, avail-used)))
			used += tw
			shown++
		}
		if rest := len(es) - shown; rest > 0 {
			b.WriteString(stMuted.Render(fmt.Sprintf(" +%d", rest)))
		}
		if len(es) == 0 {
			b.WriteString(stFaint.Render("—"))
		}
		out = append(out, fit(fg(lc).Bold(d == 0).Render(label), 7)+b.String())
	}
	return out
}

// stats: a week of watch time as a sparkline, streak and totals.
func (h *home) stats(a *App, w, ht int) []string {
	now := time.Now()
	var mins [7]float64
	peak := 1.0
	week := 0.0
	for i := 0; i < 7; i++ {
		s := a.hist.Day(now.AddDate(0, 0, i-6))
		mins[i] = s / 60
		week += s
		peak = math.Max(peak, mins[i])
	}
	levels := []rune(" ▁▂▃▄▅▆▇█")
	var bars, days strings.Builder
	bw := max(1, min(4, (w-2)/7-1))
	for i, m := range mins {
		lv := int(math.Round(m / peak * float64(len(levels)-1)))
		if m > 0 && lv == 0 {
			lv = 1
		}
		c := gradAt([]string{cAccent2, cAccent}, float64(i)/6).hex()
		if lv == 0 {
			bars.WriteString(stFaint.Render(strings.Repeat("▁", bw)) + " ")
		} else {
			bars.WriteString(fg(c).Render(strings.Repeat(string(levels[lv]), bw)) + " ")
		}
		dn := string([]rune(trDays[now.AddDate(0, 0, i-6).Weekday()])[:min(3, bw)])
		if i == 6 {
			days.WriteString(stAccent.Render(fit(dn, bw+1)))
		} else {
			days.WriteString(stMuted.Render(fit(dn, bw+1)))
		}
	}
	eps, _ := a.hist.Totals()
	done := 0
	for _, it := range a.lib.All() {
		if it.Status == store.Completed {
			done++
		}
	}
	out := []string{bars.String(), days.String(), ""}
	wk := fmt.Sprintf("%dsa %ddk", int(week/3600), int(week/60)%60)
	if week < 3600 {
		wk = fmt.Sprintf("%ddk", int(week/60))
	}
	line := stMuted.Render("bu hafta ") + stBold.Render(wk)
	if st := a.hist.Streak(); st > 0 {
		line += stFaint.Render("  ·  ") + stAccent.Render(ic("fire")+" ") + stBold.Render(fmt.Sprint(st)) + stMuted.Render(" gün")
	}
	out = append(out, line)
	out = append(out, stBold.Render(fmt.Sprint(eps))+stMuted.Render(" bölüm izlendi"),
		stBold.Render(fmt.Sprint(len(a.lib.All())))+stMuted.Render(" kitaplıkta · ")+stBold.Render(fmt.Sprint(done))+stMuted.Render(" bitti"))
	return block(out, w, ht)
}

func toggleLegacy(a *App) tea.Cmd {
	a.cfg.Legacy = !a.cfg.Legacy
	_ = a.cfg.Save()
	if a.cfg.Legacy {
		a.notify("Arşiv modu açık — aramaya eklendi: "+legacyNames()+" (linkler eski olabilir)", "warn")
	} else {
		a.notify("Arşiv modu kapalı", "ok")
	}
	return a.tick()
}
