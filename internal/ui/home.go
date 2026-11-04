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
	focus   int // 0 search, 1 continue shelf, 2 feed, 3 menu dock
	zones   []zone
	inX     int // search cursor cell (body coords) for typing sparks
	inY     int
	hCur    int
	hOff    int
	fCur    int
	fOff    int
	fGl     glide
	feed    []source.FeedItem
	feedErr error
	feedOK  bool
	feedAt  time.Time

	wx               weather
	sug              suggest
	sugX, sugY, sugW int
	feedCached       bool
	news             []newEp // new episodes on shows you follow

	// spotlight
	spotKey    string
	spotFrom   string
	spotFade   tween
	autoIdx    int
	autoRotate time.Time
}

func newHome(a *App) *home {
	return &home{in: input{placeholder: "anime ara…   frieren, one piece, mouryou no hako"}}
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

func (h *home) hints() []hint {
	switch h.focus {
	case 0:
		return []hint{{"enter", "ara (boşken: devam et)"}, {"↓", "listeler"}, {"alt+m", "menü"}, {"alt+n", "bildirimler"}, {"ctrl+k", "komutlar"}, {"?", "yardım"}}
	case 1:
		return []hint{{"enter", "devam et"}, {"← →", "gez"}, {"a", "kitaplık"}, {"x", "geçmişten sil"}, {"m", "menü"}, {"n", "bildirimler"}}
	}
	return []hint{{"enter", "aç"}, {"a", "kitaplığa ekle"}, {"r", "yenile"}, {"/", "ara"}, {"m", "menü"}, {"n", "bildirimler"}}
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
			label := fmt.Sprintf("%s · %d. bölüm çıktı", e.an.Title, e.episode)
			if e.season > 1 {
				label = fmt.Sprintf("%s · S%d %d. bölüm çıktı", e.an.Title, e.season, e.episode)
			}
			a.addNotice(notice{key: fmt.Sprintf("ep:%s:%d:%d", e.an.Key(), e.season, e.episode), kind: "new", text: label,
				action: func(a *App) tea.Cmd {
					sc := newAnime(e.an, nil, e.season, e.episode)
					return tea.Batch(a.push(sc), sc.start(a))
				}})
		}
		h.autoRotate = time.Now()
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
			if h.focus == 2 {
				h.fCur = clamp(h.fCur+d, 0, len(h.feed)-1)
			} else if h.focus == 1 {
				h.hCur = clamp(h.hCur+d, 0, len(a.recent())-1)
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

// prefetchShelf loads posters (or details, for the poster URL) of the
// continue-watching cards that are about to be visible.
func (h *home) prefetchShelf(a *App) tea.Cmd {
	if !a.cfg.Posters {
		return nil
	}
	recent := a.recent()
	var cmds []tea.Cmd
	for i := max(0, h.hCur-2); i < len(recent) && i < h.hCur+10; i++ {
		an := recent[i].Anime()
		if t := thumbURL(an.Key()); t != "" {
			cmds = append(cmds, a.posters.want(t)) // the resume frame
		}
		if an.Poster != "" {
			cmds = append(cmds, a.posters.want(an.Poster))
		} else if d, ok := a.details[an.Key()]; ok && d.Poster != "" {
			cmds = append(cmds, a.posters.want(d.Poster))
		} else {
			cmds = append(cmds, a.fetchDetails(an))
		}
	}
	return tea.Batch(cmds...)
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

func (h *home) click(a *App, z zone) tea.Cmd {
	a.flash()
	switch z.kind {
	case "shelf":
		if h.focus == 1 && h.hCur == z.idx {
			return h.key(a, tea.KeyMsg{Type: tea.KeyEnter})
		}
		h.focus, h.hCur = 1, z.idx
	case "feed":
		if h.focus == 2 && h.fCur == z.idx {
			return h.key(a, tea.KeyMsg{Type: tea.KeyEnter})
		}
		h.focus, h.fCur = 2, z.idx
	case "search":
		h.focus = 0
	}
	return a.tick()
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
	case "tab":
		h.focus = map[int]int{0: 1, 1: 2, 2: 0}[h.focus]
		if h.focus == 1 && len(recent) == 0 {
			h.focus = 2
		}
		return nil
	case "shift+tab":
		h.focus = map[int]int{0: 2, 2: 1, 1: 0}[h.focus]
		if h.focus == 1 && len(recent) == 0 {
			h.focus = 0
		}
		return nil
	case "esc":
		switch {
		case h.focus != 0:
			h.focus = 0
		case h.in.Value() != "":
			h.in.Set("")
			h.sug.close()
		case len(recent) > 0:
			h.focus = 1 // out of the search box: single keys are shortcuts again
		default:
			h.focus = 2
		}
		return nil
	}
	if h.focus == 0 {
		switch k.Type {
		case tea.KeyEnter:
			q := CleanQuery(h.in.Value())
			if q == "" && len(recent) > 0 {
				// empty box + enter: carry on with the last thing you watched
				h.focus, h.hCur = 1, 0
				return h.key(a, tea.KeyMsg{Type: tea.KeyEnter})
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
			if len(recent) > 0 {
				h.focus = 1
			} else {
				h.focus = 2
			}
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
	switch k.String() {
	case "/":
		h.focus = 0
	case "left", "h":
		if h.focus == 1 {
			h.hCur = max(0, h.hCur-1)
		}
	case "right", "l":
		if h.focus == 1 {
			h.hCur = clamp(h.hCur+1, 0, len(recent)-1)
		}
	case "m":
		a.openDropdown("menu")
		return a.tick()
	case "n":
		a.openDropdown("bell")
		return a.tick()
	case "up", "k":
		if h.focus == 1 {
			h.focus = 0
		} else if h.fCur == 0 {
			if len(recent) > 0 {
				h.focus = 1
			} else {
				h.focus = 0
			}
		} else {
			h.fCur--
		}
	case "down", "j":
		if h.focus == 1 {
			h.focus = 2
		} else {
			h.fCur = clamp(h.fCur+1, 0, len(h.feed)-1)
		}
	case "pgdown":
		if h.focus == 2 {
			h.fCur = clamp(h.fCur+10, 0, len(h.feed)-1)
		}
	case "pgup":
		if h.focus == 2 {
			h.fCur = max(0, h.fCur-10)
		}
	case "home", "g":
		h.hCur, h.fCur = 0, 0
	case "end", "G":
		h.hCur, h.fCur = max(0, len(recent)-1), max(0, len(h.feed)-1)
	case "r":
		h.feedOK = false
		return tea.Batch(a.loadFeed(), a.tick())
	case "a", "+":
		if h.focus == 1 && h.hCur < len(recent) {
			a.openPopup(recent[h.hCur].Anime())
		} else if h.focus == 2 && h.fCur < len(h.feed) {
			a.openPopup(h.feed[h.fCur].Anime)
		}
		return a.tick()
	case "delete", "x":
		if h.focus == 1 && h.hCur < len(recent) {
			// the card stands for the show on every source: remove them all,
			// or the older source's entry would pop right back in
			gone := []store.Entry{}
			for _, x := range a.hist.Recent() {
				if sameAnime(recent[h.hCur].Anime(), x.Anime()) {
					a.hist.Remove(x.Anime().Key())
					gone = append(gone, x)
				}
			}
			a.remember(recent[h.hCur].Title+" geçmişten silindi", func(a *App) {
				for _, x := range gone {
					a.hist.Restore(x)
				}
			})
			h.hCur = clamp(h.hCur, 0, len(recent)-2)
			if len(recent) <= 1 {
				h.focus = 2
			}
		}
	case "enter":
		a.flash()
		if h.focus == 1 && h.hCur < len(recent) {
			e := recent[h.hCur]
			s, ep := e.Season, e.Episode
			if e.Watched[store.EpKey(s, ep)] {
				ep++
			}
			sc := newAnime(e.Anime(), a.siblings(e.Anime()), s, ep)
			return tea.Batch(a.push(sc), sc.start(a))
		}
		if h.focus == 2 && h.fCur < len(h.feed) {
			it := h.feed[h.fCur]
			sc := newAnime(it.Anime, nil, it.Season, it.Episode)
			return tea.Batch(a.push(sc), sc.start(a))
		}
	default:
		if k.Type == tea.KeyRunes {
			h.focus = 0
			h.in.handle(k)
		}
	}
	return nil
}

// ---- view ----

func (h *home) view(a *App, w, ht int) string {
	s := h.viewBase(a, w, ht)
	if h.focus != 0 || !h.sug.open() {
		return s
	}
	// the dropdown covers what is below the search box (pictures hide meanwhile)
	a.hideImages = true
	lines := strings.Split(s, "\n")
	return strings.Join(overlayAt(lines, h.sug.box(a, h.sugW), h.sugX, h.sugY, w), "\n")
}

func (h *home) viewBase(a *App, w, ht int) string {
	var out []string
	h.zones = h.zones[:0]
	const top = 2 // body starts under the header and its rule
	big := ht >= 34 && w >= logoWidth+8
	if big {
		out = append(out, h.hero(a, w, 8)...)
	} else {
		out = append(out, "", center(gradient("aniclitr", brand, true), w), "")
	}
	iw := min(72, w-8)
	h.zones = append(h.zones, zone{kind: "search", x0: (w - iw) / 2, x1: (w-iw)/2 + iw, y0: top + len(out), y1: top + len(out) + 3})
	h.inX = (w-iw)/2 + 4 + min(lipgloss.Width(h.in.Value()), iw-8)
	h.inY = len(out) + 1
	for _, l := range h.in.view(iw, h.focus == 0, a.frame) {
		out = append(out, center(l, w))
	}
	h.sugY, h.sugX, h.sugW = len(out)-1, (w-iw)/2, iw // dropdown hangs from the input

	recent := a.recent()
	pad := 2
	if a.cfg.Compact {
		pad = 1
	}
	gw := min(w-pad*2, 180)
	margin := strings.Repeat(" ", (w-gw)/2)

	// continue-watching shelf
	if len(recent) > 0 {
		posterShelf := a.cfg.Posters && ht-len(out) >= 36
		out = append(out, "")
		shelf := h.shelf(a, recent, gw, posterShelf, len(margin), top+len(out))
		for _, l := range shelf {
			out = append(out, margin+l)
		}
	}
	listH := ht - len(out) - 1
	if listH < 5 {
		return strings.Join(out, "\n")
	}
	out = append(out, "")
	rightW := 38
	var cols []string
	fw := gw
	switch {
	case gw >= 150 && a.cfg.Posters:
		sw := 34
		fw = gw - sw - rightW - 2
		cols = hjoin(1, h.feedPane(a, fw, listH), h.spotlight(a, sw, listH, recent), h.rightCol(a, rightW, listH))
	case gw >= 112:
		fw = gw - rightW - 1
		cols = hjoin(1, h.feedPane(a, fw, listH), h.rightCol(a, rightW, listH))
	default:
		cols = h.feedPane(a, gw, listH)
	}
	for r := 0; r < listH-2; r++ {
		if i := h.fOff + r; i < len(h.feed) {
			y := top + len(out) + 1 + r
			h.zones = append(h.zones, zone{kind: "feed", idx: i, x0: len(margin), x1: len(margin) + fw, y0: y, y1: y + 1})
		}
	}
	for _, l := range cols {
		out = append(out, margin+l)
	}
	return strings.Join(out, "\n")
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

// shelf: "devam et" — horizontal cards for what you were watching. With
// room and posters on, each card is a cover; otherwise a compact text card.
func (h *home) shelf(a *App, recent []store.Entry, w int, posters bool, zx, zy int) []string {
	focused := h.focus == 1
	h.hCur = clamp(h.hCur, 0, len(recent)-1)
	cw := 26
	if posters {
		cw = 18
	}
	n := max(1, w/cw)
	h.hOff = scroll(h.hCur, h.hOff, n, len(recent))
	title := sectionTitle(ic("history")+" devam et", w-12, focused)
	more := ""
	if len(recent) > n {
		more = stMuted.Render(fmt.Sprintf("%d/%d ", h.hCur+1, len(recent)))
	}
	out := []string{row2(title, more, w)}
	var cards [][]string
	for i := h.hOff; i < len(recent) && i < h.hOff+n; i++ {
		e := recent[i]
		an := e.Anime()
		if d, ok := a.details[an.Key()]; ok && an.Poster == "" && d.Poster != "" {
			an.Poster = d.Poster
		}
		sel := focused && i == h.hCur
		acc := a.posters.accent(an.Poster, cAccent).hex()
		pw := cw - 4
		var body []string
		if posters {
			body = append(body, a.posters.render(an.Poster, pw, pw*3/4, a.frame)...)
		}
		tc := cText
		if sel {
			tc = acc
		}
		body = append(body, span{t: truncate(e.Title, pw), c: tc, bold: sel}.style("").Render(truncate(e.Title, pw)))
		pos := fmt.Sprintf("S%d·B%d", e.Season, e.Episode)
		if strings.EqualFold(e.Kind, "film") {
			pos = "Film"
		}
		var meta string
		if e.Watched[store.EpKey(e.Season, e.Episode)] {
			meta = stMuted.Render(pos+" ") + stOK.Render("✓") + stMuted.Render(" sıradaki →")
		} else {
			bw := max(4, pw-lipgloss.Width(pos)-6)
			meta = stMuted.Render(pos+" ") + renderSpans(miniBar(bw, float64(e.Percent())/100, acc)) + stMuted.Render(fmt.Sprintf(" %d%%", e.Percent()))
		}
		body = append(body, meta)
		bc := cFaint
		if !posters {
			bc = cBand
		}
		if posters {
			bc = cBg // covers carry the card; only the selection is framed
		}
		if sel {
			bc = acc
		}
		bs := fg(bc)
		card := []string{bs.Render("╭" + strings.Repeat("─", pw+2) + "╮")}
		for j, ln := range body {
			pre := bs.Render("│ ")
			if j == 0 && sel {
				pre = anchor() + pre
			}
			card = append(card, pre+fit(ln, pw)+bs.Render(" │"))
		}
		card = append(card, bs.Render("╰"+strings.Repeat("─", pw+2)+"╯"))
		// the selected card lifts one row, the others rest on the shelf
		if sel && a.motion() {
			card = append(card, "")
		} else {
			card = append([]string{""}, card...)
		}
		k := len(cards)
		h.zones = append(h.zones, zone{kind: "shelf", idx: i, x0: zx + k*cw, x1: zx + (k+1)*cw, y0: zy + 1, y1: zy + 1 + len(card)})
		cards = append(cards, block(card, cw, len(card)))
	}
	if len(cards) > 0 {
		out = append(out, hjoin(0, cards...)...)
	}
	return out
}

func renderSpans(ss []span) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.style("").Render(s.t))
	}
	return b.String()
}

func (h *home) feedPane(a *App, w, ht int) []string {
	title := ic("feed") + " yeni bölümler"
	tag := ""
	if h.feedOK && h.feedErr == nil {
		tag = fmt.Sprint(len(h.feed))
	}
	iw, ih := w-4, ht-2
	var out []string
	switch {
	case !h.feedOK && !h.feedCached:
		for i := 0; i < min(8, ih); i++ { // skeleton rows breathing while loading
			t := (math.Sin(float64(a.frame)/6+float64(i)*.7) + 1) / 2
			c := mix(hex(cSurface), hex(cShade), t).hex()
			out = append(out, "  "+fg(c).Render(strings.Repeat("▬", 5))+"  "+fg(c).Render(strings.Repeat("▬", 14+int(hash(i)*16))))
		}
		return panel(title, out, w, ht, h.focus == 2, cAccent, tag)
	case h.feedErr != nil:
		out = append(out, stErr.Render("✗ akış alınamadı"), stMuted.Render(truncate(h.feedErr.Error(), iw)), "", stMuted.Render("r ile tekrar dene"))
		return panel(title, out, w, ht, h.focus == 2, cAccent, tag)
	case len(h.feed) == 0:
		return panel(title, []string{stMuted.Render("Akış boş.")}, w, ht, h.focus == 2, cAccent, tag)
	}
	h.fCur = clamp(h.fCur, 0, len(h.feed)-1)
	h.fOff = scroll(h.fCur, h.fOff, ih, len(h.feed))
	selRow := h.fGl.row(a, h.fCur)
	for i := h.fOff; i < len(h.feed) && i < h.fOff+ih; i++ {
		it := h.feed[i]
		sel := h.focus == 2 && i == selRow
		rv := reveal(h.feedAt, i-h.fOff, a.motion())
		if rv <= 0 {
			out = append(out, "")
			continue
		}
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		if sel {
			bg, bar, tc = cBand, sp("▌ ", cAccent), cAccent
		}
		// a quiet mark for shows you follow (not a whole coloured title)
		mark := sp("  ", cText)
		if _, mine := a.libItem(it.Anime); mine {
			mark = sp("◆ ", cAccent2)
		}
		fresh := time.Since(it.Released) < 12*time.Hour
		when := fmt.Sprintf("%-7s", ago(it.Released))
		wc := cMuted
		if fresh {
			wc = cAccent2
		}
		// the episode and fansub sit in fixed columns on the right; the title
		// takes what is left (it is the one that gets an ellipsis)
		ep := fmt.Sprintf("%d. bölüm", it.Episode)
		if it.Season > 1 {
			ep = fmt.Sprintf("S%d · %d. bölüm", it.Season, it.Episode)
		}
		right := []span{sp(fmt.Sprintf("%-14s", ep), cSub)}
		if fs := shortFansub(it.Fansub); iw > 72 && fs != "" && fs != "Bilinmeyen" && !strings.Contains(fs, "www.") && !strings.Contains(fs, "://") {
			right = append(right, sp(fmt.Sprintf("%-13s", truncate(fs, 12)), cMuted))
		}
		line := row(iw, bg, []span{bar, sp(when, wc), mark, span{t: it.Anime.Title, c: tc, bold: sel}}, right)
		if sel {
			line = anchor() + line
		}
		out = append(out, line)
	}
	return panel(title, out, w, ht, h.focus == 2, cAccent, tag)
}

// rightCol stacks the weekly schedule and the stats card.
func (h *home) rightCol(a *App, w, ht int) []string {
	calH := min(ht, 11)
	out := panel(ic("calendar")+" bu hafta", h.calendar(a, w-4, calH-2), w, calH, false, cAccent)
	if rest := ht - calH; rest >= 7 {
		out = append(out, panel(ic("stats")+" istatistik", h.stats(a, w-4, rest-2), w, rest, false, cAccent)...)
	} else {
		out = append(out, block(nil, w, rest)...)
	}
	return out
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

// spotlight is a big poster card for whatever is highlighted; otherwise it
// slowly rotates through the newest releases.
func (h *home) spotlight(a *App, w, ht int, recent []store.Entry) []string {
	var an source.Anime
	var line1, line2, thumb string
	switch {
	case h.focus == 1 && h.hCur < len(recent):
		e := recent[h.hCur]
		an = e.Anime()
		if d, ok := a.details[an.Key()]; ok && an.Poster == "" && d.Poster != "" {
			an.Poster = d.Poster
		}
		line1 = fmt.Sprintf("Sezon %d · Bölüm %d", e.Season, e.Episode)
		line2 = fmt.Sprintf("%%%d izlendi · %s", e.Percent(), agoLong(e.UpdatedAt))
		thumb = thumbURL(an.Key())
		if thumb != "" {
			line2 = fmt.Sprintf("kaldığın yer %s · %%%d", clock(e.Position), e.Percent())
		}
	case len(h.feed) > 0:
		idx := h.fCur
		if h.focus != 2 {
			n := min(8, len(h.feed))
			if a.motion() && time.Since(h.autoRotate) > spotEvery {
				h.autoIdx = (h.autoIdx + 1) % n
				h.autoRotate = time.Now()
			}
			idx = h.autoIdx % n
		}
		it := h.feed[idx]
		an = it.Anime
		line1 = fmt.Sprintf("%d. bölüm", it.Episode)
		if it.Season > 1 {
			line1 = fmt.Sprintf("Sezon %d · %d. bölüm", it.Season, it.Episode)
		}
		line2 = agoLong(it.Released)
		if it.Fansub != "" && it.Fansub != "Bilinmeyen" {
			line2 += " · " + shortFansub(it.Fansub)
		}
	default:
		return panel(ic("spark")+" öne çıkan", nil, w, ht, false, cAccent)
	}
	key := an.Poster
	if key != h.spotKey {
		h.spotFrom, h.spotKey = h.spotKey, key
		h.spotFade = newTween(450 * time.Millisecond)
		if !a.motion() {
			h.spotFade = tween{}
		}
	}
	iw := w - 4
	var out []string
	// keep the poster's 2:3 shape: height first, width follows
	ph := ht - 2 - 7
	pw := min(iw, ph*4/3)
	ph = min(ph, pw*3/4)
	if ph >= 6 {
		p := a.posters.renderFade(h.spotFrom, h.spotKey, pw, ph, h.spotFade.p(), a.frame)
		indent := strings.Repeat(" ", max(0, (iw-pw)/2))
		if thumb != "" {
			// the frame you stopped at (16:9), instead of the poster
			tw := iw
			th := max(4, tw*9/32)
			if a.posters.ready(thumb) || a.posters.items[thumb] != nil {
				p = a.posters.render(thumb, tw, th, a.frame)
				indent = ""
			}
		}
		for _, l := range p {
			out = append(out, indent+l)
		}
		out = append(out, "")
	}
	acc := a.posters.accent(an.Poster, cAccent)
	for i, l := range wrap(an.Title, iw-1) {
		if i > 1 {
			break
		}
		out = append(out, gradient(l, []string{acc.hex(), cAccent2}, true))
	}
	out = append(out, stText.Render(truncate(line1, iw)), stMuted.Render(truncate(line2, iw)))
	if h.focus != 2 && h.focus != 1 && len(h.feed) > 1 {
		n := min(8, len(h.feed))
		var dots strings.Builder
		for i := 0; i < n; i++ {
			if i == h.autoIdx%n {
				dots.WriteString(fg(acc.hex()).Render("━━"))
			} else {
				dots.WriteString(stFaint.Render("──"))
			}
			dots.WriteString(" ")
		}
		out = append(out, dots.String())
	}
	// the panel says what it is showing
	title := ic("spark") + " öne çıkan"
	switch {
	case h.focus == 1 && thumb != "":
		title = ic("history") + " kaldığın yer"
	case h.focus == 1:
		title = ic("history") + " devam et"
	case h.focus == 2:
		title = ic("feed") + " seçili bölüm"
	}
	return panel(title, out, w, ht, false, acc.hex())
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
