package ui

// Nova home: the same items and cursor as the classic home (home.items),
// laid out as a launcher you read at a glance:
//
//	          (the hero logo, on tall terminals)
//	        ╭─ search ──────────────────────────╮
//	        iyi akşamlar · 2 yeni bölüm · 1 iniyor
//	  DEVAM ET
//	  [cover] [cover] [cover] [cover]      ← → between cards
//	  title   title   title   title
//	  ━━━━─── ━━━━━━━ ──────  ━━──────     progress under every card
//	  Sezon 1 · Bölüm 6 · %62 · 2 saat önce   (the selected card)
//
//	  ◆ Yeni bölümler 6   ▤ Kitaplık 42   ◎ Keşfet   ↓ İndirilenler   ⚙ Ayarlar
//
// ↑↓ move between search, cards and the dock; enter and every shortcut
// (a, x, r, m, n, typing to search) work as in the classic home.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// novaRows splits the home items into the card row and the dock row.
func novaRows(items []mItem) (cards, dock []int) {
	for i, it := range items {
		switch {
		case !it.selectable():
		case recentIdx(it) >= 0:
			cards = append(cards, i)
		default:
			dock = append(dock, i)
		}
	}
	return cards, dock
}

func indexIn(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

// novaKey moves the cursor on the two rows; anything else falls through to
// the classic menu keys (enter, a, x, r, m, n, typing).
func (h *home) novaKey(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	items := h.items(a)
	cards, dock := novaRows(items)
	inCards, inDock := indexIn(cards, h.mCur), indexIn(dock, h.mCur)
	step := func(row []int, i, d int) {
		if j := i + d; j >= 0 && j < len(row) {
			h.mCur = row[j]
		}
	}
	switch k.String() {
	case "left", "h":
		if inCards >= 0 {
			step(cards, inCards, -1)
		} else if inDock >= 0 {
			step(dock, inDock, -1)
		}
		return nil, true
	case "right", "l":
		if inCards >= 0 {
			step(cards, inCards, 1)
		} else if inDock >= 0 {
			step(dock, inDock, 1)
		}
		return nil, true
	case "down", "j":
		if inCards >= 0 && len(dock) > 0 {
			h.mCur = dock[0]
		}
		return nil, true
	case "up", "k":
		switch {
		case inDock >= 0 && len(cards) > 0:
			h.mCur = cards[0]
		default:
			h.focus = 0
		}
		return nil, true
	}
	return nil, false
}

func greeting(t time.Time) string {
	switch hr := t.Hour(); {
	case hr < 5:
		return "iyi geceler"
	case hr < 12:
		return "günaydın"
	case hr < 18:
		return "iyi günler"
	case hr < 23:
		return "iyi akşamlar"
	}
	return "iyi geceler"
}

// novaStatus is the one sentence under the search box.
func (h *home) novaStatus(a *App) string {
	parts := []string{stText.Render(greeting(time.Now()))}
	if a.offline {
		parts = append(parts, stWarn.Render("çevrimdışı"))
	}
	if n := len(h.news); n > 0 {
		parts = append(parts, fg(cAccent2).Render(fmt.Sprintf("takip ettiklerinden %d yeni bölüm", n)))
	}
	if n := a.dl.Active(); n > 0 {
		parts = append(parts, fg(cAccent).Render(fmt.Sprintf("↓ %d iniyor", n)))
	}
	if u := a.mal.User(); u != "" {
		parts = append(parts, stMuted.Render("MAL · "+u))
	}
	return strings.Join(parts, stFaint.Render("  ·  "))
}

// novaCard is one continue card: cover (or a lettered tile), title and a
// progress line; the selected one lights up.
func (h *home) novaCard(a *App, e store.Entry, cw, ph int, sel bool) []string {
	an := e.Anime()
	if d, ok := a.details[an.Key()]; ok && an.Poster == "" {
		an.Poster = d.Poster
	}
	acc := a.posters.accent(an.Poster, cAccent)
	var out []string
	if ph > 0 {
		if a.cfg.Posters && an.Poster != "" {
			out = append(out, a.posters.render(an.Poster, cw, ph, a.frame)...)
		} else {
			// no cover: a tile in the show's colour with its initial
			tile := lipgloss.NewStyle().Background(lipgloss.Color(mix(hex(cSurface), acc, .35).hex()))
			for y := 0; y < ph; y++ {
				l := strings.Repeat(" ", cw)
				if y == ph/2 {
					l = center(stBold.Render(strings.ToUpper(string([]rune(an.Title)[:1]))), cw)
				}
				out = append(out, tile.Render(fit(l, cw)))
			}
		}
	}
	if sel {
		out = append(out, fg(acc.hex()).Bold(true).Render(truncate(an.Title, cw)))
	} else {
		out = append(out, stMuted.Render(truncate(an.Title, cw)))
	}
	// progress: done share of this episode, full green once it was watched
	frac, c := float64(e.Percent())/100, acc.hex()
	if e.Watched[store.EpKey(e.Season, e.Episode)] {
		frac, c = 1, cOK
	}
	n := int(frac * float64(cw))
	thick, thin := "━", "─"
	if !sel {
		c = mix(hex(c), hex(cBg), .35).hex()
	}
	out = append(out, fg(c).Render(strings.Repeat(thick, n))+stFaint.Render(strings.Repeat(thin, cw-n)))
	// a rounded frame in the show's colour marks the selected card (as on
	// the downloads shelf); the others keep an invisible one, so nothing
	// shifts when the selection moves
	bc := cBg
	if sel {
		bc = acc.hex()
	}
	bs := fg(bc)
	framed := []string{bs.Render("╭" + strings.Repeat("─", cw+2) + "╮")}
	for _, l := range out {
		framed = append(framed, bs.Render("│ ")+fit(l, cw)+bs.Render(" │"))
	}
	return append(framed, bs.Render("╰"+strings.Repeat("─", cw+2)+"╯"))
}

// novaDock is the row of destinations; the selected one is a pill.
func (h *home) novaDock(items []mItem, dock []int, w int) string {
	render := func(withMeta, withIcon bool) string {
		var parts []string
		for _, i := range dock {
			it := items[i]
			txt := it.label
			if withIcon && it.icon != "" {
				txt = it.icon + " " + txt
			}
			meta := ""
			if withMeta && it.meta != "" {
				meta = " " + it.meta
			}
			if h.focus == 1 && i == h.mCur {
				pill := lipgloss.NewStyle().Background(lipgloss.Color(cBand)).Foreground(lipgloss.Color(cAccent)).Bold(true)
				parts = append(parts, pill.Render(" "+txt+meta+" "))
				continue
			}
			mc := cMuted
			if it.metaC != "" {
				mc = it.metaC
			}
			parts = append(parts, " "+stText.Render(txt)+fg(mc).Render(meta)+" ")
		}
		return strings.Join(parts, " ")
	}
	for _, v := range [][2]bool{{true, true}, {false, true}, {false, false}} {
		if s := render(v[0], v[1]); lipgloss.Width(s) <= w {
			return s
		}
	}
	return truncate(render(false, false), w)
}

// novaView lays out the Nova home.
func (h *home) novaView(a *App, w, ht int) string {
	h.zones = h.zones[:0]
	var out []string
	heroH := 0
	if ht >= 40 && w >= logoWidth+8 {
		heroH = 8
		out = append(out, h.hero(a, w, heroH)...)
	} else {
		out = append(out, "", center(gradient("aniclitr", brand, true), w), "")
	}
	iw := min(64, w-8)
	h.inX = (w-iw)/2 + 4 + min(lipgloss.Width(h.in.Value()), iw-8)
	h.inY = len(out) + 1
	for _, l := range h.in.view(iw, h.focus == 0, a.frame) {
		out = append(out, center(l, w))
	}
	h.sugY, h.sugX, h.sugW = len(out)-1, (w-iw)/2, iw
	out = append(out, center(h.novaStatus(a), w), "")

	gw := min(w-kitPad*2, 160)
	recent := a.recent()

	// Rows under the search: label, covers, title, progress, details, then
	// (when it fits) the band — where you left off, this week, stats — and
	// the dock. Covers take what the band leaves, the band only shows when
	// the covers keep a useful size.
	fixed := len(out) + 1 + 2 + 2 + 1 + 2 // label, title+progress, frame, details, dock
	bandH := 0
	if a.cfg.Posters && gw >= 100 && ht-fixed-9 >= 11 {
		bandH = min(11, ht-fixed-9) // what the panels hold; the rest goes to the covers
	}
	ph := clamp(ht-fixed-bandH-1, 0, 16)
	if !a.cfg.Posters || ph < 5 {
		ph = 0
	}
	cw := 22
	if ph > 0 {
		cw = max(10, ph*4/3)
	}
	gap := 1 // the invisible frames already space the cards
	h.shelfN = clamp((gw+gap)/(cw+4+gap), 1, 6)
	items := h.items(a)
	cards, dock := novaRows(items)
	if h.focus == 1 && (h.mCur >= len(items) || !items[clamp(h.mCur, 0, len(items)-1)].selectable()) {
		h.mCur = menuFirst(items)
	}

	if len(cards) > 0 {
		rowW := len(cards)*(cw+4) + (len(cards)-1)*gap
		pad := strings.Repeat(" ", max(0, (w-rowW)/2)) // centred on what is there
		out = append(out, pad+label("devam et", h.focus == 1 && indexIn(cards, h.mCur) >= 0))
		var cols [][]string
		for _, i := range cards {
			e := recent[recentIdx(items[i])]
			cols = append(cols, h.novaCard(a, e, cw, ph, h.focus == 1 && i == h.mCur))
		}
		for _, l := range hjoin(gap, cols...) {
			out = append(out, pad+l)
		}
		// the selected card's details (else the first's)
		sel := cards[0]
		if indexIn(cards, h.mCur) >= 0 {
			sel = h.mCur
		}
		e := recent[recentIdx(items[sel])]
		line := fmt.Sprintf("Sezon %d · Bölüm %s", e.Season, source.EpNum(e.Episode))
		if strings.EqualFold(e.Kind, "film") {
			line = "Film"
		}
		detail := stText.Render(line)
		if e.Watched[store.EpKey(e.Season, e.Episode)] {
			detail += stOK.Render("  ·  ✓ bitti, enter sıradakini açar")
		} else if p := e.Percent(); p > 0 {
			detail += stMuted.Render(fmt.Sprintf("  ·  %%%d", p))
		}
		detail += stFaint.Render("  ·  " + agoLong(e.UpdatedAt))
		out = append(out, pad+truncate(detail, rowW))

		if bandH > 0 {
			out = append(out, "")
			bw := max(rowW, min(gw, 140))
			bpad := strings.Repeat(" ", max(0, (w-bw)/2))
			var band [][]string
			if bw >= 130 {
				w1 := bw * 2 / 5
				w2 := (bw - w1 - 2*kitGap) * 3 / 5
				w3 := bw - w1 - w2 - 2*kitGap
				band = append(band,
					panel("kaldığın yer", h.novaResume(a, e, w1-4, bandH-2), w1, bandH, false, cAccent),
					panel(ic("calendar")+" yaklaşanlar", novaUpcoming(a, w2-4, bandH-2), w2, bandH, false, cAccent),
					panel(ic("stats")+" istatistik", novaStats(a, w3-4), w3, bandH, false, cAccent))
			} else {
				w1 := (bw - kitGap) / 2
				band = append(band,
					panel("kaldığın yer", h.novaResume(a, e, w1-4, bandH-2), w1, bandH, false, cAccent),
					panel(ic("calendar")+" yaklaşanlar", novaUpcoming(a, bw-w1-kitGap-4, bandH-2), bw-w1-kitGap, bandH, false, cAccent))
			}
			for _, l := range hjoin(kitGap, band...) {
				out = append(out, bpad+l)
			}
		}
	} else {
		out = append(out, center(stMuted.Render("henüz bir şey izlemedin · yukarıya bir ad yaz ya da Keşfet'e göz at"), w))
	}
	out = append(out, "", center(h.novaDock(items, dock, gw), w))

	if extra := (ht - len(out)) / 3; extra > 0 {
		h.inY += extra
		h.sugY += extra
		out = append(make([]string, extra), out...)
	}
	return strings.Join(block(out, w, ht), "\n")
}

// novaResume is "where you left off" for the band: the resume frame (16:9,
// never cropped) on the left, the episode, progress and time on the right.
// Without a frame it is text only.
func (h *home) novaResume(a *App, e store.Entry, w, ht int) []string {
	an := e.Anime()
	acc := a.posters.accent(an.Poster, cAccent)
	var pic []string
	if a.cfg.Posters {
		if t := thumbURL(an.Key()); t != "" && (a.posters.ready(t) || a.posters.items[t] != nil) {
			th := min(ht, (w*11/20)*9/32)
			tw := th * 32 / 9
			if th >= 4 && tw <= w-20 {
				pic = a.posters.render(t, tw, th, a.frame)
			}
		}
	}
	tw := 0
	if len(pic) > 0 {
		tw = lipgloss.Width(pic[0]) + 2
	}
	txtW := w - tw
	var txt []string
	for i, l := range wrap(an.Title, txtW) {
		if i > 1 {
			break
		}
		txt = append(txt, gradient(l, []string{acc.hex(), cAccent2}, true))
	}
	line := fmt.Sprintf("Sezon %d · Bölüm %s", e.Season, source.EpNum(e.Episode))
	if strings.EqualFold(e.Kind, "film") {
		line = "Film"
	}
	txt = append(txt, stText.Render(line), "")
	if e.Watched[store.EpKey(e.Season, e.Episode)] {
		txt = append(txt, stOK.Render("✓ bitti"), stMuted.Render("enter sıradakini açar"))
	} else {
		txt = append(txt, renderSpans(miniBar(max(6, txtW-6), float64(e.Percent())/100, acc.hex()))+stMuted.Render(fmt.Sprintf(" %%%d", e.Percent())))
		if e.Position > 0 {
			txt = append(txt, stMuted.Render("kaldığın an "+clock(e.Position)))
		}
	}
	txt = append(txt, stFaint.Render(agoLong(e.UpdatedAt)))
	if len(pic) == 0 {
		return txt
	}
	return hjoin(2, pic, block(txt, txtW, len(txt)))
}

// ---- the band: plain lists instead of charts ----

// fmtWatch is watch time read aloud: "45 dk", "2 sa 10 dk".
func fmtWatch(secs float64) string {
	m := int(secs / 60)
	if m < 60 {
		return fmt.Sprintf("%d dk", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%d sa", m/60)
	}
	return fmt.Sprintf("%d sa %d dk", m/60, m%60)
}

// whenLabel says when an episode airs: "bugün 21:00", "yarın 12:00", "Pzt 18:30".
func whenLabel(t, now time.Time) string {
	t = t.Local()
	d0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch days := int(t.Sub(d0).Hours() / 24); days {
	case 0:
		return "bugün " + t.Format("15:04")
	case 1:
		return "yarın " + t.Format("15:04")
	}
	return trDays[t.Weekday()] + " " + t.Format("15:04")
}

// novaUpcoming lists what airs next, in time order: the shows you follow
// first, then (if room) the most popular, quieter.
func novaUpcoming(a *App, w, ht int) []string {
	if len(a.schedule) == 0 {
		return []string{stMuted.Render(spinner(a.frame/2) + " takvim yükleniyor…")}
	}
	now := time.Now()
	mine := following(a)
	type row struct {
		at     time.Time
		title  string
		ep, pp int
		mine   bool
	}
	var yours, popular []row
	for _, x := range a.schedule {
		if x.At.Before(now) || x.At.After(now.Add(7*24*time.Hour)) {
			continue
		}
		_, f := mine[x.Media.IDMal]
		r := row{x.At, x.Media.Name(), x.Episode, x.Media.Popularity, f}
		if f {
			yours = append(yours, r)
		} else {
			popular = append(popular, r)
		}
	}
	sort.SliceStable(yours, func(i, j int) bool { return yours[i].at.Before(yours[j].at) })
	sort.SliceStable(popular, func(i, j int) bool { return popular[i].pp > popular[j].pp })
	if len(popular) > 3 {
		popular = popular[:3]
	}
	sort.SliceStable(popular, func(i, j int) bool { return popular[i].at.Before(popular[j].at) })

	const whenW = 12
	line := func(r row, strong bool) string {
		when, tc := fg(cAccent).Render(fit(whenLabel(r.at, now), whenW)), stText
		if !strong {
			when, tc = stMuted.Render(fit(whenLabel(r.at, now), whenW)), stMuted
		}
		ep := fmt.Sprintf(" · %d. bölüm", r.ep)
		return when + tc.Render(truncate(r.title, max(8, w-whenW-lipgloss.Width(ep)))) + stFaint.Render(ep)
	}
	var out []string
	if len(yours) == 0 {
		out = append(out, stMuted.Render("takip ettiklerinden bu hafta yayın yok"))
	}
	for _, r := range yours {
		if len(out) >= ht {
			return out
		}
		out = append(out, line(r, true))
	}
	if len(popular) > 0 && len(out)+2 <= ht {
		out = append(out, "", stFaint.Render("popüler"))
		for _, r := range popular {
			if len(out) >= ht {
				break
			}
			out = append(out, line(r, false))
		}
	}
	return out
}

// novaStats is four plain sentences.
func novaStats(a *App, w int) []string {
	now := time.Now()
	var week float64
	for i := 0; i < 7; i++ {
		week += a.hist.Day(now.AddDate(0, 0, -i))
	}
	eps, _ := a.hist.Totals()
	lib, done := 0, 0
	for _, it := range a.lib.All() {
		lib++
		if it.Status == store.Completed {
			done++
		}
	}
	kv := func(k, v, rest string) string {
		return stMuted.Render(fit(k, 11)) + stText.Bold(true).Render(v) + stMuted.Render(rest)
	}
	out := []string{kv("bu hafta", fmtWatch(week), " izledin")}
	if s := a.hist.Streak(); s > 0 {
		out = append(out, kv("üst üste", fmt.Sprintf("%d gün", s), " izliyorsun"))
	}
	out = append(out, kv("toplam", fmt.Sprint(eps), " bölüm"), kv("kitaplık", fmt.Sprint(lib), fmt.Sprintf(" seri · %d bitti", done)))
	for i := range out {
		out[i] = truncate(out[i], w)
	}
	return out
}
