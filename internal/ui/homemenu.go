package ui

// homemenu.go — the front door. Classic terminal apps open on a short menu
// you walk with the arrows; this is that, with a web app's manners: what
// you are about to open is previewed beside the menu, so nothing has to be
// guessed and nothing is shown that you did not point at.
//
//	          ANICLITR
//	      [ anime ara…            ]
//
//	  DEVAM ET                          │  (preview of the selected item)
//	▌ ▶ Odd Taxi        S1 · B8   %91   │
//	    Mushishi        S1 · B7   %77   │
//	  MENÜ                              │
//	    Yeni bölümler   son çıkanlar 30 │
//	    Kitaplık · Keşfet · İndirilenler · Ayarlar · Çıkış

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// homeOf returns the root home screen (nil when offline).
func (a *App) homeOf() *home {
	if len(a.stack) > 0 {
		if h, ok := a.stack[0].(*home); ok {
			return h
		}
	}
	return nil
}

func (h *home) items(a *App) []mItem {
	var out []mItem
	recent := a.recent()
	if n := min(len(recent), max(1, h.shelfN)); n > 0 {
		out = append(out, mItem{group: "devam et"})
		for i, e := range recent[:n] {
			e := e
			pos := fmt.Sprintf("S%d · B%d", e.Season, e.Episode)
			if strings.EqualFold(e.Kind, "film") {
				pos = "film"
			}
			meta, mc := fmt.Sprintf("%%%d", e.Percent()), cMuted
			if e.Watched[store.EpKey(e.Season, e.Episode)] {
				meta, mc = "sıradaki", cOK
			}
			out = append(out, mItem{id: fmt.Sprintf("rec:%d", i), icon: ic("play"), label: e.Title, desc: pos, meta: meta, metaC: mc,
				run: func(a *App) tea.Cmd { return continueEntry(a, e) }})
		}
	}
	out = append(out, mItem{group: "ekranlar"})
	feedMeta, feedC := "", cMuted
	switch {
	case h.feedErr != nil && len(h.feed) == 0:
		feedMeta, feedC = "alınamadı", cWarn
	case !h.feedOK && len(h.feed) == 0:
		feedMeta = "yükleniyor…"
	case len(h.news) > 0:
		feedMeta, feedC = fmt.Sprintf("%d takip ettiğin", len(h.news)), cAccent2
	default:
		feedMeta = fmt.Sprint(len(h.feed))
	}
	libN := len(a.lib.All())
	dlMeta, dlC := "", cMuted
	if n := a.dl.Active(); n > 0 {
		dlMeta, dlC = fmt.Sprintf("↓ %d iniyor", n), cAccent
	} else if n := countSeries(a); n > 0 {
		dlMeta = fmt.Sprintf("%d seri", n) // episodes are counted on the downloads screen
	}
	out = append(out,
		mItem{id: "feed", icon: ic("feed"), label: "Yeni bölümler", desc: "son yayınlananlar", meta: feedMeta, metaC: feedC,
			run: func(a *App) tea.Cmd { return a.push(newFeedScreen()) }},
		mItem{id: "library", icon: ic("library"), label: "Kitaplık", desc: "listeler ve puanlar", meta: plural(libN),
			run: func(a *App) tea.Cmd { return a.openLibrary("") }},
		mItem{id: "discover", icon: ic("calendar"), label: "Keşfet", desc: "takvim, sezon, öneriler",
			run: func(a *App) tea.Cmd {
				if a.offline {
					a.notify("keşfet internet ister — şu an çevrimdışısın", "warn")
					return a.tick()
				}
				return a.openDiscover()
			}},
		mItem{id: "downloads", icon: "↓", label: "İndirilenler", desc: "çevrimdışı izleme", meta: dlMeta, metaC: dlC,
			run: func(a *App) tea.Cmd { return a.openDownloads() }},
		mItem{id: "settings", icon: ic("settings"), label: "Ayarlar", desc: "görünüm, oynatma, kaynaklar",
			run: func(a *App) tea.Cmd { return a.push(newSettings()) }},
		mItem{id: "exit", icon: ic("exit"), label: "Çıkış",
			run: func(a *App) tea.Cmd { return tea.Quit }},
	)
	return out
}

func plural(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

// countSeries is how many shows have at least one downloaded episode.
func countSeries(a *App) int {
	seen := map[string]bool{}
	for _, j := range a.dl.Jobs() {
		if j.State == download.Done {
			seen[j.Key()] = true
		}
	}
	return len(seen)
}

func countDone(a *App) int {
	n := 0
	for _, j := range a.dl.Jobs() {
		if j.State == download.Done {
			n++
		}
	}
	return n
}

// continueEntry opens a history entry where it was left (the next episode
// when that one was finished).
func continueEntry(a *App, e store.Entry) tea.Cmd {
	a.flash()
	s, ep := e.Season, e.Episode
	if e.Watched[store.EpKey(s, ep)] {
		ep++
	}
	sc := newAnime(e.Anime(), a.siblings(e.Anime()), s, ep)
	return tea.Batch(a.push(sc), sc.start(a))
}

// selItem is the menu item under the cursor (cursor kept valid).
func (h *home) selItem(a *App) (mItem, []mItem) {
	items := h.items(a)
	if h.mCur < 0 || h.mCur >= len(items) || !items[h.mCur].selectable() {
		h.mCur = menuFirst(items)
	}
	return items[h.mCur], items
}

// recentIdx is the history index of a "rec:N" item, or -1.
func recentIdx(it mItem) int {
	var i int
	if _, err := fmt.Sscanf(it.id, "rec:%d", &i); err != nil {
		return -1
	}
	return i
}

// focusRecent puts the cursor on the i-th continue card.
func (h *home) focusRecent(a *App, i int) {
	h.focus = 1
	for j, it := range h.items(a) {
		if recentIdx(it) == i {
			h.mCur = j
			return
		}
	}
}

func (h *home) hints() []hint {
	if h.focus == 0 {
		return []hint{{"enter", "ara (kutu boşken: devam et)"}, {"↓", "menü"}, {"ctrl+k", "komutlar"}}
	}
	return []hint{{"enter", "aç"}, {"/", "ara"}, {"a", "kitaplığa ekle"}, {"x", "geçmişten sil"}, {"r", "akışı yenile"},
		{"m", "bütün ekranlar"}, {"n", "bildirimler"}}
}

// menuKey handles keys while the menu (not the search box) has focus.
func (h *home) menuKey(a *App, k tea.KeyMsg) tea.Cmd {
	it, items := h.selItem(a)
	recent := a.recent()
	switch k.String() {
	case "/":
		h.focus = 0
	case "up", "k":
		if n := menuMove(items, h.mCur, -1); n != h.mCur {
			h.mCur = n
		} else {
			h.focus = 0
		}
	case "down", "j":
		h.mCur = menuMove(items, h.mCur, 1)
	case "home", "g", "pgup":
		h.mCur = menuFirst(items)
	case "end", "G", "pgdown":
		h.mCur = menuMove(items, len(items), -1)
	case "m":
		a.openDropdown("menu")
		return a.tick()
	case "n":
		a.openDropdown("bell")
		return a.tick()
	case "r":
		h.feedOK = false
		return tea.Batch(a.loadFeed(), a.tick())
	case "a", "+":
		if i := recentIdx(it); i >= 0 && i < len(recent) {
			a.openPopup(recent[i].Anime())
			return a.tick()
		}
	case "delete", "x":
		if i := recentIdx(it); i >= 0 && i < len(recent) {
			// the card stands for the show on every source: remove them all,
			// or the older source's entry would pop right back in
			gone := []store.Entry{}
			for _, x := range a.hist.Recent() {
				if sameAnime(recent[i].Anime(), x.Anime()) {
					a.hist.Remove(x.Anime().Key())
					gone = append(gone, x)
				}
			}
			a.remember(recent[i].Title+" geçmişten silindi", func(a *App) {
				for _, x := range gone {
					a.hist.Restore(x)
				}
			})
		}
	case "enter", "right", "l":
		if it.run != nil {
			a.flash()
			return it.run(a)
		}
	default:
		if k.Type == tea.KeyRunes {
			h.focus = 0
			h.in.handle(k)
			return h.sug.changed(a, h.in.Value())
		}
	}
	return nil
}

// viewBase lays out the front door.
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
	out = append(out, "")

	// how many continue cards fit: the menu panel is 11 rows plus one per card
	room := ht - len(out)
	h.shelfN = clamp(room-11, 1, 5)
	it, items := h.selItem(a)
	band := -1
	if h.focus == 1 {
		band = h.mGl.row(a, h.mCur)
		if !items[clamp(band, 0, len(items)-1)].selectable() {
			band = h.mCur
		}
	}
	gw := min(w-kitPad*2, 176)
	lw, rw, sideW := gw, 0, 0
	if gw >= 150 {
		sideW = 40
	}
	if gw >= 100 {
		lw = min(62, (gw-sideW)*11/20)
		rw = gw - lw - kitGap
		if sideW > 0 {
			rw -= sideW + kitGap
		}
	}
	listH := min(room, max(len(menuRows(items, -1, lw-4))+2, 24))
	margin := (w - gw) / 2
	for i := range items {
		if items[i].selectable() {
			y := top + len(out) + 1 + menuRowOf(items, i)
			h.zones = append(h.zones, zone{kind: "menu", idx: i, x0: margin + 2, x1: margin + lw - 2, y0: y, y1: y + 1})
		}
	}
	menu := pane{title: "ana menü", focused: h.focus == 1, draw: func(w, _ int) []string { return menuRows(items, band, w) }}
	prev := h.preview(a, it, sideW > 0)
	cols := panes(menu, lw, prev, rw, listH)
	if sideW > 0 {
		cols = hjoin(kitGap, cols, h.sideCol(a, sideW, listH))
	}
	pad := strings.Repeat(" ", margin)
	for _, l := range cols {
		out = append(out, pad+l)
	}
	if extra := (ht - len(out)) / 3; extra > 0 {
		for i := range h.zones {
			h.zones[i].y0 += extra
			h.zones[i].y1 += extra
		}
		h.inY += extra
		h.sugY += extra
		out = append(make([]string, extra), out...)
	}
	return strings.Join(out, "\n")
}

// sideCol: this week's calendar over the stats (wide terminals).
func (h *home) sideCol(a *App, w, ht int) []string {
	calH := min(ht, 11)
	out := panel(ic("calendar")+" bu hafta", h.calendar(a, w-4, calH-2), w, calH, false, cAccent)
	if rest := ht - calH; rest >= 7 {
		out = append(out, panel(ic("stats")+" istatistik", h.stats(a, w-4, rest-2), w, rest, false, cAccent)...)
	} else {
		out = append(out, block(nil, w, rest)...)
	}
	return out
}

// preview is the panel beside the menu: what the selected item opens.
// side says the calendar and stats are already on screen.
func (h *home) preview(a *App, it mItem, side bool) pane {
	recent := a.recent()
	if i := recentIdx(it); i >= 0 && i < len(recent) {
		e := recent[i]
		an := e.Anime()
		if d, ok := a.details[an.Key()]; ok && an.Poster == "" && d.Poster != "" {
			an.Poster = d.Poster
		}
		return pane{title: "kaldığın yer", acc: a.posters.accent(an.Poster, cAccent).hex(),
			draw: func(w, ht int) []string { return h.previewRecent(a, e, w, ht) }}
	}
	switch it.id {
	case "feed":
		return pane{title: "son çıkanlar", tag: plural(len(h.feed)), draw: func(w, ht int) []string {
			if len(h.feed) == 0 {
				return []string{stMuted.Render("Akış henüz gelmedi.")}
			}
			var out []string
			for i, f := range h.feed {
				if len(out) >= ht {
					break
				}
				mark := sp("  ", cText)
				if _, mine := a.libItem(f.Anime); mine {
					mark = sp("◆ ", cAccent2)
				}
				rv := reveal(h.feedAt, i, a.motion())
				tc := mix(hex(cFaint), hex(cText), rv).hex()
				out = append(out, row(w, "", []span{sp(fmt.Sprintf("%-7s", ago(f.Released)), cMuted), mark, sp(f.Anime.Title, tc)},
					[]span{sp(source.EpNum(f.Episode)+". bölüm", cSub)}))
			}
			return out
		}}
	case "library":
		if !side {
			return pane{title: "istatistik", draw: func(w, ht int) []string { return h.stats(a, w, ht) }}
		}
		return pane{title: "izlediklerin", draw: func(w, ht int) []string {
			var out []string
			for _, x := range a.lib.All() {
				if x.Status != store.Watching || len(out) >= ht {
					continue
				}
				out = append(out, row(w, "", []span{sp("▶ ", cSky), sp(x.Title, cText)}, []span{sp(fmt.Sprintf("%d. bölüm", x.Watched), cMuted)}))
			}
			if len(out) == 0 {
				return wrapMuted("İzlenen anime yok. Bir animenin durumunu a tuşuyla “izliyor” olarak işaretleyebilirsiniz.", w)
			}
			return out
		}}
	case "discover":
		if !side {
			return pane{title: "bu hafta", draw: func(w, ht int) []string { return h.calendar(a, w, ht) }}
		}
		return pane{title: "keşfet", draw: func(w, ht int) []string {
			var out []string
			for i, t := range discoverTabs {
				out = append(out, row(w, "", []span{sp(ic(discoverIcons[i])+" ", cMuted), sp(capTR(t), cText)}, nil), stMuted.Render("  "+truncate(discoverDesc[i], w-2)))
			}
			return out
		}}
	case "downloads":
		return pane{title: "indirilenler", draw: func(w, ht int) []string {
			jobs := a.dl.Jobs()
			if len(jobs) == 0 {
				return wrapMuted("Henüz indirilmiş bölüm yok. Bölüm listesinde d tuşuyla indirilen bölümler burada çevrimdışı izlenebilir.", w)
			}
			var out []string
			for _, j := range jobs {
				if len(out) >= ht {
					break
				}
				st := stMuted.Render("hazır")
				switch j.State {
				case download.Running:
					st = fg(cAccent).Render(fmt.Sprintf("%%%d", pct(j.Done, j.Total)))
				case download.Failed:
					st = stErr.Render("hata")
				case download.Queued:
					st = stMuted.Render("sırada")
				}
				out = append(out, row2(stText.Render(fmt.Sprintf("%s · %d", j.Title, j.Episode)), st, w))
			}
			return out
		}}
	case "settings":
		return pane{title: "şu an", draw: func(w, ht int) []string {
			return []string{
				row2(stMuted.Render("tema"), stText.Render(a.themeName), w),
				row2(stMuted.Render("hareket"), stText.Render(onOff(a.cfg.Motion)), w),
				row2(stMuted.Render("kapaklar"), stText.Render(onOff(a.cfg.Posters)), w),
				row2(stMuted.Render("kaynaklar"), stText.Render(truncate(strings.Join(sourceNames(a), ", "), w-12)), w),
			}
		}}
	case "exit":
		return pane{title: "çıkış", draw: func(w, ht int) []string {
			return []string{stMuted.Render("Kalınan yerler kaydedildi. İyi seyirler.")}
		}}
	}
	return pane{}
}

func wrapMuted(s string, w int) []string {
	var out []string
	for _, l := range wrap(s, w) {
		out = append(out, stMuted.Render(l))
	}
	return out
}

func pct(done, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(float64(done) / float64(total) * 100)
}

// sourceNames lists the switched-on sources by name.
func sourceNames(a *App) []string {
	var out []string
	for _, p := range source.All() {
		in := p.Info()
		if a.cfg.Enabled(in.ID, in.Legacy) {
			out = append(out, in.Name)
		}
	}
	return out
}

// previewRecent: where you left a show — the frame you stopped at (or the
// cover), the episode, and how far in you were.
func (h *home) previewRecent(a *App, e store.Entry, w, ht int) []string {
	an := e.Anime()
	if d, ok := a.details[an.Key()]; ok && an.Poster == "" && d.Poster != "" {
		an.Poster = d.Poster
	}
	var out []string
	acc := a.posters.accent(an.Poster, cAccent)
	if a.cfg.Posters {
		thumb := thumbURL(an.Key())
		var pic []string
		switch {
		case thumb != "" && (a.posters.ready(thumb) || a.posters.items[thumb] != nil):
			// the whole 16:9 frame, never cropped: height first (what is
			// left under the text), then the width that keeps the shape
			th := min(ht-7, w*9/32)
			tw := min(w, th*32/9)
			th = tw * 9 / 32
			if th >= 4 {
				in := strings.Repeat(" ", max(0, (w-tw)/2))
				for _, l := range a.posters.render(thumb, tw, th, a.frame) {
					pic = append(pic, in+l)
				}
			}
		case an.Poster != "":
			ph := min(ht-7, 16)
			pw := min(w, ph*4/3)
			ph = pw * 3 / 4
			if ph >= 6 {
				in := strings.Repeat(" ", max(0, (w-pw)/2))
				for _, l := range a.posters.render(an.Poster, pw, ph, a.frame) {
					pic = append(pic, in+l)
				}
			}
		}
		if len(pic) > 0 && len(pic)+7 <= ht {
			out = append(out, pic...)
			out = append(out, "")
		}
	}
	for i, l := range wrap(an.Title, w) {
		if i > 1 {
			break
		}
		out = append(out, gradient(l, []string{acc.hex(), cAccent2}, true))
	}
	line := fmt.Sprintf("Sezon %d · Bölüm %d", e.Season, e.Episode)
	if strings.EqualFold(e.Kind, "film") {
		line = "Film"
	}
	out = append(out, stText.Render(line))
	if e.Watched[store.EpKey(e.Season, e.Episode)] {
		out = append(out, stOK.Render("✓ bitti")+stMuted.Render(" · enter sıradaki bölümü açar"))
	} else {
		bw := max(6, w-8)
		out = append(out, renderSpans(miniBar(bw, float64(e.Percent())/100, acc.hex()))+stMuted.Render(fmt.Sprintf(" %%%d", e.Percent())))
		if e.Position > 0 {
			out = append(out, stMuted.Render("kaldığın an "+clock(e.Position)))
		}
	}
	out = append(out, stFaint.Render(agoLong(e.UpdatedAt)))
	return out
}

// prefetchShelf loads what the continue items and their preview need.
func (h *home) prefetchShelf(a *App) tea.Cmd {
	if !a.cfg.Posters {
		return nil
	}
	recent := a.recent()
	var cmds []tea.Cmd
	for i := 0; i < len(recent) && i < max(1, h.shelfN); i++ {
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

func (h *home) click(a *App, z zone) tea.Cmd {
	a.flash()
	switch z.kind {
	case "menu":
		if h.focus == 1 && h.mCur == z.idx {
			return h.menuKey(a, tea.KeyMsg{Type: tea.KeyEnter})
		}
		h.focus, h.mCur = 1, z.idx
	case "search":
		h.focus = 0
	}
	return a.tick()
}
