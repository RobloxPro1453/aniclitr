package ui

// Nova anime screen: puts the show's identity (cover, title, status, quick
// actions, synopsis) on a calm showcase card on the left and gives the episode
// list the wide focused stage on the right. On narrow terminals (< 96 inner
// cols) it folds cleanly into a two-line hero bar above the full-width episode
// list so nothing overflows or disappears.
//
// Only presentation forks here; all keys, downloads, notes, range selection,
// source switching and MAL season sync remain shared with animeScreen.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func (s *animeScreen) novaView(a *App, w, ht int) string {
	pad := 2
	inner := max(20, w-pad*2)
	acc := s.accent(a)

	var lines []string
	if inner >= 96 && ht >= 18 {
		lw := clamp(inner*36/100, 34, 48)
		rw := inner - lw - kitGap
		leftBody := s.novaShowCard(a, lw-4, ht-2, acc)
		rightBody := s.novaEpisodesStage(a, rw-4, ht-2, acc)
		leftBox := panel("anime", leftBody, lw, ht, false, acc)
		rightTag := s.novaStageTag(a)
		rightBox := panel("bölümler", rightBody, rw, ht, true, acc, rightTag)
		lines = hjoin(kitGap, leftBox, rightBox)
	} else {
		hero := s.novaCompactHero(a, inner, acc)
		bodyH := max(6, ht-len(hero))
		stage := s.novaEpisodesStage(a, inner-4, bodyH-2, acc)
		box := panel("bölümler", stage, inner, bodyH, true, acc, s.novaStageTag(a))
		lines = append(hero, box...)
	}

	lines = block(lines, inner, ht)
	for i := range lines {
		lines[i] = strings.Repeat(" ", pad) + lines[i]
	}
	return strings.Join(lines, "\n")
}

// novaStageTag is the quiet right-hand tag on the episodes panel border.
func (s *animeScreen) novaStageTag(a *App) string {
	if len(s.selEps) > 0 {
		return fmt.Sprintf("%d seçili", len(s.selEps))
	}
	eps := s.episodes(a)
	if len(eps) == 0 {
		return ""
	}
	wv := a.watchView(s.an)
	watched := 0
	for _, e := range eps {
		if wv.done(e) {
			watched++
		}
	}
	if watched > 0 {
		return fmt.Sprintf("%d/%d izlendi", watched, len(eps))
	}
	return fmt.Sprintf("%d bölüm", len(eps))
}

// novaCompactHero is the 2-line show summary used on narrow terminals (< 96 cols).
func (s *animeScreen) novaCompactHero(a *App, w int, acc string) []string {
	title := gradient(truncate(s.an.Title, max(12, w*2/3)), []string{acc, cAccent2}, true)
	right1 := ""
	if li, ok := a.libItem(s.an); ok {
		right1 = fg(statusColor(li.Status)).Render(statusIcon(li.Status) + " " + li.Status)
		if li.Score > 0 {
			right1 += stFaint.Render(" · ") + fg(cWarn).Render(ic("star")+" ") + stBold.Render(fmt.Sprint(li.Score))
		}
	} else if s.an.Year > 0 {
		right1 = stMuted.Render(fmt.Sprint(s.an.Year))
	}

	var meta []string
	if p := source.Get(s.an.Source); p != nil {
		dot := fg(sourceColor(s.an.Source)).Render("●")
		if s.offSrc {
			dot = stFaint.Render("●")
		}
		meta = append(meta, dot+" "+stSub.Render(p.Info().Name))
	}
	if s.an.Kind != "" {
		meta = append(meta, stMuted.Render(s.an.Kind))
	}
	if s.an.Year > 0 && right1 != "" {
		meta = append(meta, stMuted.Render(fmt.Sprint(s.an.Year)))
	}
	if len(s.srcs) > 1 {
		meta = append(meta, stMuted.Render(fmt.Sprintf("%d kaynak", len(s.srcs))))
	}
	right2 := keycap("s") + " " + stMuted.Render("kaynak")
	return []string{
		row2(title, right1, w),
		row2(strings.Join(meta, stFaint.Render(" · ")), right2, w),
	}
}

// novaShowCard is the left showcase panel: cover poster, title, library status,
// genres, keycap shortcuts, and scrollable synopsis.
func (s *animeScreen) novaShowCard(a *App, w, h int, acc string) []string {
	var out []string

	// Cover art at the top of the card when room allows (never near the bottom row).
	if a.cfg.Posters && s.an.Poster != "" && h >= 20 {
		ph := clamp(h-13, 6, 15)
		pw := min(w-2, ph*4/3)
		if pw >= 12 && ph >= 6 {
			pic := a.posters.render(s.an.Poster, pw, ph, a.frame)
			pad := strings.Repeat(" ", max(0, (w-pw)/2))
			for _, l := range pic {
				out = append(out, pad+l)
			}
			out = append(out, "")
		}
	}

	// Title + optional alt title.
	for i, l := range wrap(s.an.Title, w) {
		if i == 0 {
			out = append(out, gradient(l, []string{acc, cAccent2}, true))
		} else if i < 2 {
			out = append(out, stBold.Render(l))
		}
	}
	if s.an.AltTitle != "" && len(out) < h-8 {
		out = append(out, stMuted.Render(truncate(s.an.AltTitle, w)))
	}

	// Compact metadata line.
	if m := metaLine(s.an); m != "" {
		out = append(out, truncate(m, w))
	}

	// Library badge (always via a.libItem).
	if li, ok := a.libItem(s.an); ok {
		ln := fg(statusColor(li.Status)).Bold(true).Render(statusIcon(li.Status) + " " + li.Status)
		if li.Score > 0 {
			ln += stFaint.Render("  ·  ") + fg(cWarn).Render(ic("star")+" ") + stBold.Render(fmt.Sprint(li.Score))
		}
		if li.InList("favoriler") {
			ln += "  " + fg(cAccent).Render(ic("heart"))
		}
		out = append(out, truncate(ln, w))
	}

	// Genre chips + upcoming episode info.
	if g := genreLine(s.an.Genres, w); g != "" {
		out = append(out, g)
	}
	if s.an.NextEpisode != "" {
		out = append(out, fg(cWarn).Render("◷ ")+stSub.Render(truncate("sıradaki: "+s.an.NextEpisode, max(8, w-2))))
	}

	// Quick Nova keycap actions.
	if len(out)+2 < h {
		out = append(out, "")
		chips := []string{
			keycap("c") + " " + fg(acc).Bold(true).Render("Kadro"),
			keycap("t") + " " + stSub.Render("Fragman"),
			keycap("y") + " " + stSub.Render("Benzer"),
			keycap("a") + " " + stSub.Render("Kitaplık"),
			keycap("T") + " " + fg(cWarn).Render("Torrent"),
		}
		out = append(out, wrapChips(chips, w)...)
	}

	// Synopsis fills remaining rows cleanly.
	rem := h - len(out) - 1
	if rem >= 2 {
		out = append(out, stFaint.Render(strings.Repeat("─", w)))
		desc := strings.TrimSpace(s.an.Description)
		if desc == "" {
			out = append(out, stMuted.Render("Özet bilgisi yok."))
		} else {
			dl := wrap(desc, w)
			off := min(s.descOff, max(0, len(dl)-1))
			for _, l := range dl[off:] {
				if len(out) >= h {
					break
				}
				out = append(out, stSub.Render(l))
			}
		}
	}

	return block(out, w, h)
}

// novaEpisodesStage is the main right-hand panel: source summary, season tabs,
// progress bar + offline count, episode rows, and note/range editor.
func (s *animeScreen) novaEpisodesStage(a *App, w, h int, acc string) []string {
	switch {
	case s.detailErr != nil:
		out := []string{
			"",
			stErr.Bold(true).Render("✗ ayrıntılar alınamadı"),
			stMuted.Render(truncate(shortErr(s.detailErr), w)),
			"",
		}
		if len(s.srcs) > 1 {
			out = append(out, keycap("s")+" "+stSub.Render("başka kaynakta dene"))
		}
		out = append(out, keycap("r")+" "+stMuted.Render("tekrar dene")+"   "+keycap("esc")+" "+stMuted.Render("geri"))
		return block(out, w, h)
	case s.catalog:
		out := []string{
			"",
			fg(acc).Render(spinner(a.frame/2)) + " " + stText.Bold(true).Render("Tüm kaynaklarda aranıyor…"),
			stMuted.Render(truncate("AnimeciX · AnimeTR · AniHub · SonAnime · Animim · HiAnime", w)),
			"",
			stMuted.Render(truncate("En uygun kaynak otomatik seçilecek; ardından s ile kaynak değiştirebilirsiniz.", w)),
			"",
			keycap("T") + " " + fg(cWarn).Render("Nyaa.si Torrent ile anlık izle"),
		}
		return block(out, w, h)
	case s.catalogEmpty:
		out := []string{
			"",
			stWarn.Bold(true).Render("Bu anime etkin izleme kaynaklarında bulunamadı."),
			stMuted.Render(truncate("Farklı bir kaynakta veya Nyaa.si torrent arşivinde arayabilirsiniz.", w)),
			"",
			keycap("T") + " " + fg(cWarn).Bold(true).Render("Nyaa.si Torrent ile anlık izle"),
			keycap("s") + " " + stSub.Render("tüm kaynakları tara"),
			keycap("r") + " " + stMuted.Render("tekrar dene") + "   " + keycap("esc") + " " + stMuted.Render("geri"),
		}
		return block(out, w, h)
	case s.gate:
		out := []string{
			"",
			stWarn.Render("◌ ") + stSub.Render(srcName(s.base.Source)+" ayarlarda kapalı"),
			"",
			fg(acc).Render(spinner(a.frame/2)) + stMuted.Render(" açık kaynaklarda aranıyor…"),
			"",
			stMuted.Render(truncate("bulunursa orada, kaldığın yerden açılır", w)),
		}
		return block(out, w, h)
	case !s.ready:
		out := s.novaSourceStrip(a, w)
		out = append(out, "", fg(acc).Render(spinner(a.frame/2))+stMuted.Render(" ayrıntılar yükleniyor…"))
		return block(out, w, h)
	}

	var out []string
	if strip := s.novaSourceStrip(a, w); len(strip) > 0 {
		out = append(out, strip...)
	}

	// Season tabs with accent underline.
	var tabs, under strings.Builder
	used := 0
	for i, se := range s.an.Seasons {
		lbl := se.Name
		if lbl == "" || strings.HasPrefix(strings.ToLower(lbl), "season") {
			lbl = fmt.Sprintf("Sezon %d", se.Number)
		}
		if s.an.IsMovie() {
			lbl = "Film"
		}
		if se.EpisodeCount > 0 && len(s.an.Seasons) > 1 {
			lbl += fmt.Sprintf(" · %d", se.EpisodeCount)
		}
		lw := lipgloss.Width(lbl)
		if used+lw+3 > w {
			tabs.WriteString(stMuted.Render("…"))
			break
		}
		if i == s.sIdx {
			tabs.WriteString(fg(acc).Bold(true).Render(lbl))
			under.WriteString(fg(acc).Render(strings.Repeat("━", lw)))
		} else {
			tabs.WriteString(stMuted.Render(lbl))
			under.WriteString(stFaint.Render(strings.Repeat("─", lw)))
		}
		tabs.WriteString("   ")
		under.WriteString(stFaint.Render("───"))
		used += lw + 3
	}
	if used < w {
		under.WriteString(stFaint.Render(strings.Repeat("─", w-used)))
	}
	out = append(out, tabs.String(), under.String())

	eps := s.episodes(a)
	switch {
	case errors.Is(s.epErr, source.ErrNotFound):
		out = append(out, "", stSub.Render("Bu sezonda henüz bölüm yok."))
		if len(s.an.Seasons) > 1 {
			out = append(out, keycap("tab")+" "+stMuted.Render("diğer sezona geç"))
		}
		out = append(out, keycap("s")+" "+stMuted.Render("başka kaynakta ara"))
		return block(out, w, h)
	case s.epErr != nil:
		out = append(out, "",
			stErr.Render("✗ bölümler alınamadı: ")+stMuted.Render(truncate(shortErr(s.epErr), max(10, w-24))),
			keycap("r")+" "+stMuted.Render("tekrar dene")+"   "+keycap("s")+" "+stSub.Render("başka kaynak"))
		return block(out, w, h)
	case eps == nil:
		for i := 0; i < 6 && len(out) < h; i++ {
			t := (sin(float64(a.frame)/6+float64(i)*.7) + 1) / 2
			c := mix(hex(cSurface), hex(cShade), t).hex()
			out = append(out, "  "+fg(c).Render("▬▬")+"  "+fg(c).Render(strings.Repeat("▬", 10+int(hash(i+9)*22))))
		}
		return block(out, w, h)
	}

	wv := a.watchView(s.an)
	ent := wv.ent
	watched, offlineCnt := 0, 0
	for _, e := range eps {
		if wv.done(e) {
			watched++
		}
		if j, ok := a.dl.ForAnime(s.an, e.Season, e.Number); ok && j.State == download.Done {
			offlineCnt++
		}
	}

	// Progress / selection bar.
	if len(s.selEps) > 0 {
		selText := fmt.Sprintf("● %d bölüm seçildi · d/D indir · v ekle/çıkar · V temizle", len(s.selEps))
		out = append(out, fg(acc).Bold(true).Render(truncate(selText, w)))
	} else if watched > 0 || offlineCnt > 0 {
		pct := watched * 100 / max(1, len(eps))
		summary := fmt.Sprintf(" %d/%d (%%%d)", watched, len(eps), pct)
		if offlineCnt > 0 {
			summary += fmt.Sprintf(" · ↓%d", offlineCnt)
		}
		barW := max(8, w-lipgloss.Width(summary))
		bar := progressBar(barW, float64(watched)/float64(max(1, len(eps))), []string{acc, cAccent2})
		rightSum := stMuted.Render(fmt.Sprintf(" %d/%d (%%%d)", watched, len(eps), pct))
		if offlineCnt > 0 {
			rightSum += stFaint.Render(" · ") + fg(cOK).Render(fmt.Sprintf("↓%d", offlineCnt))
		}
		out = append(out, bar+rightSum)
	} else {
		out = append(out, "")
	}

	// Note preview / note editor / range download input at bottom.
	var noteLines []string
	if s.ranging {
		noteLines = append([]string{stSub.Render("İndirilecek bölüm aralığı (örn: 30-100 veya 10-20, 25):")}, s.rangeIn.view(w-1, true, a.frame)...)
	} else if s.noting {
		noteLines = s.noteIn.view(w-1, true, a.frame)
	} else if s.cur < len(eps) {
		e, _ := a.hist.Get(s.an.Key())
		if n := e.Notes[store.EpKey(eps[s.cur].Season, eps[s.cur].Number)]; n != "" {
			noteLines = []string{stFaint.Render(strings.Repeat("─", max(1, w-1)))}
			for i, l := range wrap(n, max(8, w-5)) {
				if i > 2 {
					break
				}
				prefix := "   "
				if i == 0 {
					prefix = stAccent.Render("✎") + "  "
				}
				noteLines = append(noteLines, prefix+stSub.Italic(true).Render(l))
			}
		}
	}

	listH := max(1, h-len(out)-len(noteLines))
	s.cur = clamp(s.cur, 0, len(eps)-1)
	if !s.placed && listH > 0 {
		s.off = clamp(s.cur-listH/2, 0, max(0, len(eps)-listH))
		s.placed = true
	}
	s.off = scroll(s.cur, s.off, listH, len(eps))
	numW := max(2, len(fmt.Sprint(eps[len(eps)-1].Number)))
	band := s.gl.row(a, s.cur)

	var rows []string
	for i := s.off; i < len(eps) && len(rows) < listH; i++ {
		ep := eps[i]
		rv := reveal(s.epAt, i-s.off, a.motion())
		if rv <= 0 {
			rows = append(rows, "")
			continue
		}
		sel := i == band
		key := store.EpKey(ep.Season, ep.Number)
		done := wv.done(ep)
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		nc := cMuted
		if done {
			tc = cSub
		}
		if sel {
			bg, bar, tc, nc = cBand, sp("▌ ", acc), acc, acc
		}
		title := ep.Title
		if genericTitle(title) {
			title = "bölüm"
			if !sel {
				tc = mix(hex(cFaint), hex(cMuted), rv).hex()
			}
			if s.an.IsMovie() {
				title = s.an.Title
			}
		}

		var right []span
		if ep.Filler {
			right = append(right, sp("filler  ", cWarn))
		}
		if sc := ent.EpScores[key]; sc > 0 {
			right = append(right, sp(fmt.Sprintf("★%d ", sc), cWarn))
		}
		if ent.Notes[key] != "" {
			right = append(right, sp("✎ ", cAccent2))
		}
		if j, ok := a.dl.ForAnime(s.an, ep.Season, ep.Number); ok {
			ic, c := dlIcon(j.State)
			switch j.State {
			case download.Done:
				right = append(right, sp("↓ ", cOK))
			case download.Running:
				right = append(right, sp(fmt.Sprintf("↓%2.0f%% ", j.Frac()*100), cAccent))
			default:
				right = append(right, sp(ic+" ", c))
			}
		} else if _, ok := a.localFileFor(s.an, ep.Season, ep.Number); ok {
			right = append(right, sp("◆ Diskte Hazır  ", cOK))
		}
		switch {
		case done:
			right = append(right, sp("✓", cOK))
		case ent.Progress[key] > 0:
			right = append(right, miniBar(6, float64(ent.Progress[key])/100, cWarn)...)
			right = append(right, sp(fmt.Sprintf(" %%%d", ent.Progress[key]), cWarn))
		}
		if ent.Season == ep.Season && ent.Episode == ep.Number && !done {
			right = append([]span{sp("▸ kaldığın yer  ", cWarn)}, right...)
		}
		right = append(right, sp(" ", cText))

		leftSpans := []span{bar}
		if s.selEps[ep.Number] {
			leftSpans = append(leftSpans, sp("[✓] ", acc))
		} else if len(s.selEps) > 0 {
			leftSpans = append(leftSpans, sp("[ ] ", cFaint))
		}
		leftSpans = append(leftSpans, sp(epCell(ep, numW)+"  ", nc), span{t: title, c: tc, bold: sel})

		line := row(max(1, w-1), bg, leftSpans, right)
		if i == s.cur {
			line = anchor() + line
		}
		rows = append(rows, line)
	}
	rows = block(rows, max(1, w-1), listH)
	out = append(out, hjoin(0, rows, scrollbar(listH, s.off, len(eps)))...)
	out = append(out, noteLines...)
	return block(out, w, h)
}

// novaSourceStrip is the compact source bar at the top of the Nova episode stage.
func (s *animeScreen) novaSourceStrip(a *App, w int) []string {
	if s.srcIdx >= len(s.srcs) {
		return nil
	}
	x := s.srcs[s.srcIdx]
	p := source.Get(x.Source)
	if p == nil {
		return nil
	}
	dot := fg(sourceColor(x.Source)).Render("●")
	left := dot + " " + stBold.Render(p.Info().Name)
	if !srcOn(a, x) {
		left = stFaint.Render("●") + " " + stBold.Render(p.Info().Name) + " " + pill("KAPALI", cWarn)
	}
	if n := len(s.srcs); n > 1 {
		left += stMuted.Render(fmt.Sprintf("  ·  %d kaynak", n))
	}
	right := keycap("s") + " " + stSub.Render("kaynak seç ▾")
	return []string{row2(left, right, w), ""}
}
