package ui

// Nova Discover screen: redesigned from the ground up with crisp spatial
// hierarchy and zero text clutter.
//
// Layout (wide >= 124 inner cols):
//   [ Keşfet · active context pill                      section controls ]
//   ┌─ KEŞFET ────────┐ ┌─ ACTIVE ROOM ────────────────┐ ┌─ AYRINTI ─────┐
//   │ ▌ 1 Takvim   14 │ │  clean scannable rows        │ │  [poster]     │
//   │   2 Bu akşam 60 │ │  or 2-col genre grid         │ │  title/meta   │
//   │   3 Bu sezon Güz│ │  or time-budget pills        │ │  score bar    │
//   │   4 Türler      │ │                              │ │  genres       │
//   │   5 Öneriler    │ │                              │ │  synopsis     │
//   │   6 Rastgele    │ │                              │ │               │
//   └─────────────────┘ └──────────────────────────────┘ └───────────────┘
//
// Spatial navigation:
//   - Focus on left rail (d.menu == true): ↑↓ previews a room, → / enter
//     steps into the center stage.
//   - Focus on center stage (d.menu == false): ↑↓ browses items, ← / esc
//     steps cleanly back to the left rail (never teleports tabs!).

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"aniclitr/internal/anilist"
)

func upperTR(s string) string { return strings.ToUpperSpecial(unicode.TurkishCase, s) }

func (d *discover) novaView(a *App, w, h int) string {
	pad := 2
	inner := max(20, w-pad*2)

	head := d.novaHead(a, inner)
	bodyH := max(6, h-len(head))

	var body []string
	switch {
	case inner < 96:
		// Narrow terminal: show either the rail menu or the active room full-width.
		if d.menu {
			body = panel("keşfet", d.novaNarrowMenu(a, inner-4, bodyH-2), inner, bodyH, true, cAccent, "1-6")
		} else {
			body = d.novaStagePanel(a, inner, bodyH, true)
		}
	default:
		railW := 24
		detailW := 0
		if inner >= 124 && d.tab != tRandom && !(d.tab == tGenre && d.gpick) {
			detailW = clamp(inner*30/100, 34, 46)
		}
		stageW := inner - railW - kitGap
		if detailW > 0 {
			stageW -= detailW + kitGap
		}

		rail := panel("keşfet", d.novaRail(a, railW-4, bodyH-2), railW, bodyH, d.menu, cAccent)
		stage := d.novaStagePanel(a, stageW, bodyH, !d.menu)
		cols := [][]string{rail, stage}
		if detailW > 0 {
			cols = append(cols, d.novaDetailPanel(a, detailW, bodyH))
		}
		body = hjoin(kitGap, cols...)
	}

	out := block(append(head, body...), inner, h)
	for i := range out {
		out[i] = strings.Repeat(" ", pad) + out[i]
	}
	return strings.Join(out, "\n")
}

// novaHead is a single crisp context strip (no multi-line subtitle wall).
func (d *discover) novaHead(a *App, w int) []string {
	left := gradient("Keşfet", brand, true)
	ctx := ""
	switch d.tab {
	case tCal:
		if d.onlyMine {
			ctx = pill("SADECE TAKİP", cAccent)
		} else {
			ctx = pill("YAYIN TAKVİMİ", cSky)
		}
	case tTonight:
		ctx = pill(fmt.Sprintf("BU AKŞAM · %d DK", d.budget), cAccent2)
	case tSeason:
		ctx = pill(upperTR(anilist.SeasonTR(d.sName))+fmt.Sprintf(" %d", d.sYear), cAccent)
	case tGenre:
		_, tr := d.genreName()
		if d.gpick {
			ctx = pill("TÜRLER · "+upperTR(d.gmode), cAccent2)
		} else {
			ctx = pill(upperTR(tr)+" · "+d.gmode, cAccent)
		}
	case tRecs:
		if d.moodIdx >= 0 && d.moodIdx < len(anilist.MoodPresets) {
			mp := anilist.MoodPresets[d.moodIdx]
			ctx = pill(mp.Icon+" "+upperTR(mp.Title), cAccent2)
		} else {
			ctx = pill("SANA ÖZEL", cOK)
		}
	case tRandom:
		if d.rndNote != "" {
			ctx = pill(upperTR(d.rndNote), cWarn)
		} else {
			ctx = pill("RASTGELE", cWarn)
		}
	}
	if ctx != "" {
		left += "  " + ctx
	}

	right := ""
	if d.menu {
		right = keycap("↑↓") + " " + stMuted.Render("bölüm") + "  " + keycap("→") + " " + stMuted.Render("gir")
	} else {
		switch d.tab {
		case tCal:
			right = keycap("f") + " " + stMuted.Render("takip") + "  " + keycap("p") + " " + stMuted.Render("hepsi")
		case tTonight:
			right = keycap("+ -") + " " + stMuted.Render("süre")
		case tSeason:
			right = keycap("[ ]") + " " + stMuted.Render("sezon")
		case tGenre:
			if d.gpick {
				right = keycap("m") + " " + stMuted.Render("sırala") + "  " + keycap("enter") + " " + stMuted.Render("aç")
			} else {
				right = keycap("[ ]") + " " + stMuted.Render("tür") + "  " + keycap("m") + " " + stMuted.Render("sırala") + "  " + keycap("space") + " " + stMuted.Render("zar")
			}
		case tRandom:
			right = keycap("space") + " " + stMuted.Render("yeni zar") + "  " + keycap("enter") + " " + stMuted.Render("aç")
		case tRecs:
			right = keycap("m") + " " + stMuted.Render("ruh hali") + "  " + keycap("a") + " " + stMuted.Render("listeye") + "  " + keycap("t") + " " + stMuted.Render("fragman")
		default:
			right = keycap("a") + " " + stMuted.Render("listeye") + "  " + keycap("t") + " " + stMuted.Render("fragman")
		}
		if w >= 100 {
			right = keycap("←") + " " + stMuted.Render("raflar") + "  " + right
		}
	}
	return []string{row2(left, right, w), ""}
}

// novaRail is the always-visible left navigation rail (6 discover rooms).
func (d *discover) novaRail(a *App, w, h int) []string {
	var out []string
	sel := d.mgl.row(a, d.tab)
	for i, name := range discoverTabs {
		active := i == d.tab
		isBand := i == sel
		bg, bar, tc, icC := "", sp("  ", cText), cSub, cMuted
		if active {
			tc, icC = cText, cAccent
		}
		if isBand {
			if d.menu {
				bg, bar, tc, icC = cBand, sp("▌ ", cAccent), cText, cAccent
			} else {
				bar = sp("▏ ", cAccent)
			}
		}
		meta := ""
		switch i {
		case tCal:
			if d.schedOK && d.schedErr == nil {
				meta = fmt.Sprint(len(d.sched))
			}
		case tTonight:
			meta = fmt.Sprintf("%d'", d.budget)
		case tSeason:
			meta = shortSeasonTR(d.sName)
		case tGenre:
			meta = fmt.Sprint(len(anilist.Genres))
		case tRecs:
			if d.recsOK && d.recsErr == nil && len(d.recs) > 0 {
				meta = fmt.Sprint(len(d.recs))
			}
		}
		var right []span
		if meta != "" {
			right = append(right, sp(meta+" ", cFaint))
		}
		num := sp(fmt.Sprintf("%d ", i+1), cFaint)
		if active {
			num = sp(fmt.Sprintf("%d ", i+1), cAccent)
		}
		line := row(w, bg, []span{bar, num, sp(ic(discoverIcons[i])+" ", icC), span{t: capTR(name), c: tc, bold: active}}, right)
		if active && d.menu {
			line = anchor() + line
		}
		out = append(out, line)
	}

	// Quiet spatial cue at the bottom of the rail.
	if len(out)+3 <= h {
		for len(out) < h-2 {
			out = append(out, "")
		}
		out = append(out, stFaint.Render(strings.Repeat("─", w)))
		if d.menu {
			out = append(out, stMuted.Render(truncate("→ / enter: aç · 1-6", w)))
		} else {
			out = append(out, stMuted.Render(truncate("← / esc: buraya dön", w)))
		}
	}
	return block(out, w, h)
}

func shortSeasonTR(s string) string {
	switch s {
	case "WINTER":
		return "Kış"
	case "SPRING":
		return "Bahar"
	case "SUMMER":
		return "Yaz"
	case "FALL":
		return "Güz"
	}
	return ""
}

// novaNarrowMenu renders the 6 rooms cleanly on narrow terminals (< 96 cols).
func (d *discover) novaNarrowMenu(a *App, w, h int) []string {
	var out []string
	sel := d.mgl.row(a, d.tab)
	for i, name := range discoverTabs {
		active := i == sel
		bg, bar, tc, icC := "", sp("  ", cText), cText, cMuted
		if active {
			bg, bar, icC = cBand, sp("▌ ", cAccent), cAccent
		}
		line := row(w, bg, []span{
			bar,
			sp(fmt.Sprintf("%d ", i+1), cAccent),
			sp(ic(discoverIcons[i])+" ", icC),
			span{t: capTR(name), c: tc, bold: active},
		}, []span{sp("→ ", cFaint)})
		if active {
			line = anchor() + line
		}
		out = append(out, line)
		if w >= 52 {
			out = append(out, "      "+stMuted.Render(truncate(discoverDesc[i], max(8, w-6))))
		}
	}
	return block(out, w, h)
}

// novaStagePanel draws the center room panel with clean visual hierarchy.
func (d *discover) novaStagePanel(a *App, w, h int, focused bool) []string {
	iw, ih := w-4, h-2
	title, tag := capTR(discoverTabs[d.tab]), ""
	acc := cAccent

	switch d.tab {
	case tCal:
		title = "yayın akışı"
		if d.onlyMine {
			tag = "sadece takip"
		} else {
			tag = "yerel saat"
		}
		return panel(title, d.list(a, iw, ih), w, h, focused, acc, tag)

	case tTonight:
		title = "bu akşam ne izlesem"
		tag = fmt.Sprintf("%d dk", d.budget)
		return panel(title, d.novaTonightBody(a, iw, ih), w, h, focused, acc, tag)

	case tSeason:
		title = anilist.SeasonTR(d.sName) + fmt.Sprintf(" %d", d.sYear)
		tag = "[ ] sezon"
		return panel(title, d.list(a, iw, ih), w, h, focused, acc, tag)

	case tGenre:
		if d.gpick {
			title = "tür seç"
			tag = "m: " + d.gmode
			return panel(title, d.novaGenreGrid(a, iw, ih), w, h, focused, acc, tag)
		}
		_, tr := d.genreName()
		title = tr
		tag = d.gmode
		return panel(title, d.novaGenreList(a, iw, ih), w, h, focused, acc, tag)

	case tRecs:
		title = "sana göre · ruh hali"
		if d.moodIdx >= 0 && d.moodIdx < len(anilist.MoodPresets) {
			title = anilist.MoodPresets[d.moodIdx].Title
			tag = "m / [ ] ruh hali"
		} else if d.recsOK && len(d.recs) > 0 {
			tag = fmt.Sprintf("%d öneri · m ruh hali", len(d.recs))
		} else {
			tag = "m ruh hali"
		}
		return panel(title, d.novaRecsBody(a, iw, ih), w, h, focused, acc, tag)

	case tRandom:
		title = "rastgele öneri"
		tag = "space: zar at"
		return panel(title, d.novaRandomBody(a, iw, ih), w, h, focused, acc, tag)
	}
	return panel(title, nil, w, h, focused, acc)
}

// novaTonightBody shows interactive duration pills above the curated picks.
func (d *discover) novaTonightBody(a *App, w, h int) []string {
	budgets := []int{30, 60, 90, 120, 180}
	var pills []string
	for _, b := range budgets {
		lbl := fmt.Sprintf(" %d dk ", b)
		if b == d.budget {
			pills = append(pills, pill(fmt.Sprintf("%d dk", b), cAccent))
		} else {
			pills = append(pills, fg(cMuted).Background(colorOf(cSurface)).Render(lbl))
		}
	}
	bar := row2(strings.Join(pills, " "), stMuted.Render("+ / −"), w)
	out := []string{bar, stFaint.Render(strings.Repeat("─", w))}

	picks := tonightPicks(a, d.budget, homeFeed(a))
	if len(picks) == 0 {
		out = append(out, emptyState("Henüz öneri yok", "Birkaç bölüm izleyin veya kitaplığınıza planlananlar ekleyin.", w, max(4, h-len(out)))...)
		return block(out, w, h)
	}
	c := &d.cur[tTonight]
	*c = clamp(*c, 0, len(picks)-1)
	band := d.gl.row(a, *c)
	for i, p := range picks {
		if len(out) >= h {
			break
		}
		sel := i == band && !d.menu
		bg, marker, tc := "", sp("  ", cText), cText
		if sel {
			bg, marker, tc = cBand, sp("▌ ", cAccent), cAccent
		}
		what := fmt.Sprintf("  S%d·B%d", p.season, p.episode)
		if p.count > 1 {
			what = fmt.Sprintf("  S%d·B%d–%d", p.season, p.episode, p.episode+p.count-1)
		}
		line := row(w, bg, []span{marker, span{t: p.an.Title, c: tc, bold: sel}, sp(what, cSub)},
			[]span{sp(p.reason+"  ", cMuted), sp(fmt.Sprintf("%3d dk ", p.minutes), cAccent2)})
		if i == *c && !d.menu {
			line = anchor() + line
		}
		out = append(out, line)
	}
	return block(out, w, h)
}

// novaGenreGrid renders genres in a clean 2-column grid with sort mode pills.
func (d *discover) novaGenreGrid(a *App, w, h int) []string {
	var modes []string
	for _, m := range []string{anilist.ModeTop, anilist.ModePopular, anilist.ModeCult} {
		if m == d.gmode {
			modes = append(modes, pill(m, cAccent))
		} else {
			modes = append(modes, fg(cMuted).Background(colorOf(cSurface)).Render(" "+m+" "))
		}
	}
	topBar := row2(strings.Join(modes, " "), stMuted.Render(genreModeDesc[d.gmode]), w)
	out := []string{topBar, stFaint.Render(strings.Repeat("─", w)), ""}

	n := len(anilist.Genres)
	cols := 2
	if w < 44 {
		cols = 1
	}
	rowsPerCol := (n + cols - 1) / cols
	colW := (w - (cols-1)*2) / cols

	for r := 0; r < rowsPerCol && len(out) < h; r++ {
		var parts []string
		for c := 0; c < cols; c++ {
			idx := c*rowsPerCol + r
			if idx >= n {
				parts = append(parts, strings.Repeat(" ", colW))
				continue
			}
			g := anilist.Genres[idx]
			sel := idx == d.genre
			bg, bar, tc := "", sp("  ", cText), cText
			if sel {
				if !d.menu {
					bg, bar, tc = cBand, sp("▌ ", cAccent), cAccent
				} else {
					bar, tc = sp("▏ ", cAccent), cText
				}
			}
			cell := row(colW, bg, []span{bar, span{t: g[1], c: tc, bold: sel}}, []span{sp(g[0]+" ", cFaint)})
			if sel && !d.menu {
				cell = anchor() + cell
			}
			parts = append(parts, cell)
		}
		out = append(out, strings.Join(parts, "  "))
	}
	return block(out, w, h)
}

// novaRecsBody renders the interactive "Ruh Haline Göre" mood pill bar above the recommendations list.
func (d *discover) novaRecsBody(a *App, w, h int) []string {
	var pills []string
	if d.moodIdx < 0 {
		pills = append(pills, pill("★ Sana göre", cOK))
	} else {
		pills = append(pills, fg(cMuted).Background(colorOf(cSurface)).Render(" Sana göre "))
	}
	for i, mp := range anilist.MoodPresets {
		short := strings.Fields(mp.Title)[0]
		if i == d.moodIdx {
			pills = append(pills, pill(mp.Icon+" "+mp.Title, cAccent2))
		} else {
			pills = append(pills, fg(cMuted).Background(colorOf(cSurface)).Render(" "+mp.Icon+" "+short+" "))
		}
	}
	out := []string{truncate(strings.Join(pills, " "), w)}
	if d.moodIdx >= 0 && d.moodIdx < len(anilist.MoodPresets) {
		out = append(out, stMuted.Render(truncate(anilist.MoodPresets[d.moodIdx].Desc, w)))
	}
	out = append(out, stFaint.Render(strings.Repeat("─", w)))
	out = append(out, d.list(a, w, max(1, h-len(out)))...)
	return block(out, w, h)
}

// novaGenreList adds a sleek sort-mode pill strip above the genre's anime list.
func (d *discover) novaGenreList(a *App, w, h int) []string {
	var modes []string
	for _, m := range []string{anilist.ModeTop, anilist.ModePopular, anilist.ModeCult} {
		if m == d.gmode {
			modes = append(modes, pill(m, cAccent))
		} else {
			modes = append(modes, fg(cMuted).Background(colorOf(cSurface)).Render(" "+m+" "))
		}
	}
	topBar := row2(strings.Join(modes, " "), keycap("←")+" "+stMuted.Render("türler"), w)
	out := []string{topBar, stFaint.Render(strings.Repeat("─", w))}
	out = append(out, d.list(a, w, max(1, h-len(out)))...)
	return block(out, w, h)
}

// novaRandomBody renders the slot reel or the highlighted random showcase card.
func (d *discover) novaRandomBody(a *App, w, h int) []string {
	if d.rolling.active() || (d.rnd == nil && d.rndErr == nil) {
		pool := d.rollPool
		if len(pool) == 0 {
			pool = []string{"Frieren", "Mushishi", "Monster", "Cowboy Bebop", "Odd Taxi"}
		}
		p := d.rolling.p()
		speed := 1 - easeOutCubic(p)
		idx := int(float64(a.frame)*(0.3+2*speed)) % len(pool)
		var out []string
		top := max(1, (h-7)/2)
		for i := 0; i < top; i++ {
			out = append(out, "")
		}
		for k := -1; k <= 1; k++ {
			name := pool[(idx+k+len(pool))%len(pool)]
			if k == 0 {
				out = append(out, center(gradient("▸  "+name+"  ◂", brand, true), w))
			} else {
				out = append(out, center(stFaint.Render(name), w))
			}
		}
		out = append(out, "", center(stMuted.Render("zar atılıyor…"), w))
		return block(out, w, h)
	}
	if d.rndErr != nil {
		return emptyState("Seçilemedi", d.rndErr.Error()+" · space ile tekrar deneyin", w, h)
	}
	m := d.rnd
	acc := a.posters.accent(m.Poster(), cAccent).hex()

	posterW := 0
	if a.cfg.Posters && w >= 54 && h >= 14 {
		posterW = clamp(w/3, 18, 26)
	}
	tw := w - posterW
	if posterW > 0 {
		tw -= 3
	}

	var info []string
	for i, l := range wrap(m.Name(), tw) {
		if i == 0 {
			info = append(info, gradient(l, []string{acc, cAccent2}, true))
		} else if i < 2 {
			info = append(info, stBold.Render(l))
		}
	}
	if m.Title.English != "" && m.Title.English != m.Name() {
		info = append(info, stMuted.Render(truncate(m.Title.English, tw)))
	}
	info = append(info, "")
	var meta []string
	if f := fmtFormat(m); f != "" {
		meta = append(meta, f)
	}
	if m.SeasonYear > 0 {
		meta = append(meta, fmt.Sprintf("%s %d", anilist.SeasonTR(m.Season), m.SeasonYear))
	}
	if s := m.Studio(); s != "" {
		meta = append(meta, s)
	}
	if len(meta) > 0 {
		info = append(info, stSub.Render(truncate(strings.Join(meta, " · "), tw)))
	}
	if m.Score > 0 {
		info = append(info, scoreBar((m.Score+5)/10, acc, 1)+"  "+stBold.Render(fmt.Sprintf("%.1f", float64(m.Score)/10))+stMuted.Render(" AniList"))
	}
	if g := genreLine(m.Genres, tw); g != "" {
		info = append(info, "", g)
	}
	info = append(info, "", keycap("enter")+" "+fg(acc).Bold(true).Render("Aç")+"   "+keycap("space")+" "+stSub.Render("Yeni zar")+"   "+keycap("a")+" "+stSub.Render("Listeye ekle"))
	if m.Description != "" && len(info)+2 < h {
		info = append(info, "", stFaint.Render(strings.Repeat("─", tw)))
		for _, l := range wrap(anilist.CleanDescription(m.Description), tw) {
			if len(info) >= h {
				break
			}
			info = append(info, stSub.Render(l))
		}
	}

	if posterW > 0 {
		ph := clamp(posterW*3/4, 8, max(8, h-4))
		pic := a.posters.render(m.Poster(), posterW, ph, a.frame)
		bh := max(len(info), ph)
		return block(hjoin(3, block(pic, posterW, bh), block(info, tw, bh)), w, h)
	}
	return block(info, w, h)
}

// novaDetailPanel renders the right-hand showcase card cleanly for the active room.
func (d *discover) novaDetailPanel(a *App, w, h int) []string {
	if d.tab == tTonight {
		return d.tonightCard(a, w, h)
	}
	m := d.selected(a)
	if m == nil {
		return panel("ayrıntı", emptyState("Önizleme", "Bir anime seçildiğinde ayrıntılar burada görünür.", w-4, h-2), w, h, false, cAccent)
	}
	acc := a.posters.accent(m.Poster(), cAccent).hex()
	iw := w - 4
	ih := h - 2
	var out []string
	if a.cfg.Posters && m.Poster() != "" && ih >= 18 {
		ph := clamp(ih-12, 6, 14)
		pw := min(iw-2, ph*4/3)
		if pw >= 12 {
			ind := strings.Repeat(" ", max(0, (iw-pw)/2))
			for _, l := range a.posters.render(m.Poster(), pw, ph, a.frame) {
				out = append(out, ind+l)
			}
			out = append(out, "")
		}
	}
	for i, l := range wrap(m.Name(), iw) {
		if i == 0 {
			out = append(out, gradient(l, []string{acc, cAccent2}, true))
		} else if i < 2 {
			out = append(out, stBold.Render(l))
		}
	}
	if m.Title.English != "" && m.Title.English != m.Name() {
		out = append(out, stMuted.Render(truncate(m.Title.English, iw)))
	}
	var meta []string
	if f := fmtFormat(m); f != "" {
		meta = append(meta, f)
	}
	if m.SeasonYear > 0 {
		meta = append(meta, fmt.Sprintf("%s %d", anilist.SeasonTR(m.Season), m.SeasonYear))
	}
	if s := m.Studio(); s != "" {
		meta = append(meta, s)
	}
	if len(meta) > 0 {
		out = append(out, stSub.Render(truncate(strings.Join(meta, " · "), iw)))
	}
	if m.Score > 0 {
		out = append(out, scoreBar((m.Score+5)/10, acc, 1)+"  "+stBold.Render(fmt.Sprintf("%.1f", float64(m.Score)/10))+stMuted.Render(" AniList"))
	}
	if m.Next != nil {
		out = append(out, fg(cWarn).Render("◷ ")+stSub.Render(truncate(fmt.Sprintf("%d. bölüm %s", m.Next.Episode, countdownLong(time.Unix(m.Next.AiringAt, 0))), max(8, iw-2))))
	}
	if it, ok := a.lib.FindMal(m.IDMal); ok {
		out = append(out, fg(statusColor(it.Status)).Bold(true).Render(statusIcon(it.Status)+" listende: "+it.Status))
	} else {
		out = append(out, keycap("a")+" "+stSub.Render("listeme ekle")+"   "+keycap("t")+" "+stMuted.Render("fragman"))
	}
	if g := genreLine(m.Genres, iw); g != "" {
		out = append(out, g)
	}
	if m.Description != "" && len(out)+2 < ih {
		out = append(out, stFaint.Render(strings.Repeat("─", iw)))
		for _, l := range wrap(anilist.CleanDescription(m.Description), iw) {
			if len(out) >= ih {
				break
			}
			out = append(out, stSub.Render(l))
		}
	}
	return panel("ayrıntı", block(out, iw, ih), w, h, false, acc)
}
