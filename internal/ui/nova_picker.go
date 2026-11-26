package ui

// Nova picker screen: frames fansub selection and host/quality resolution in
// crisp rounded panels tinted with the anime's cover accent, with keycap cues
// and clean single-panel folding on narrow terminals (< 88 inner cols).

import (
	"fmt"
	"strings"

	"aniclitr/internal/source"
)

func (p *picker) novaView(a *App, w, h int) string {
	pad := 2
	inner := max(20, w-pad*2)
	acc := a.posters.accent(p.an.Poster, cAccent).hex()

	// Compact two-line banner: anime title + episode/download pill on line 1,
	// source + preferred fansub cue on line 2.
	badge := ""
	switch p.dlMode {
	case 1:
		badge = pill("↓ İNDİRME", acc)
	case 2:
		badge = pill(fmt.Sprintf("↓ TOPLU İNDİRME · %d BÖLÜM", len(p.dlSeason)), acc)
	default:
		if p.an.IsMovie() {
			badge = pill("FİLM", acc)
		} else if p.ep.Season > 1 || len(p.an.Seasons) > 1 {
			badge = pill(fmt.Sprintf("S%d · B%s", p.ep.Season, p.ep.Num()), acc)
		} else {
			badge = pill(fmt.Sprintf("BÖLÜM %s", p.ep.Num()), acc)
		}
	}
	title := gradient(truncate(p.an.Title, max(12, inner-28)), []string{acc, cAccent2}, true)
	var subParts []string
	if p.dlMode == 2 && len(p.dlSeason) > 1 {
		subParts = append(subParts, stText.Render(fmt.Sprintf("%s. – %s. Bölümler", p.dlSeason[0].Num(), p.dlSeason[len(p.dlSeason)-1].Num())))
	} else if p.ep.Title != "" && !genericTitle(p.ep.Title) && !p.an.IsMovie() {
		subParts = append(subParts, stText.Render(truncate("“"+p.ep.Title+"”", inner/2)))
	}
	if src := source.Get(p.an.Source); src != nil {
		subParts = append(subParts, fg(sourceColor(p.an.Source)).Render("● ")+stMuted.Render(src.Info().Name))
	}
	if p.prefFan != "" {
		subParts = append(subParts, fg(cWarn).Render("★ ")+stMuted.Render(shortFansub(p.prefFan)))
	}
	if len(subParts) == 0 {
		subParts = append(subParts, stMuted.Render("çeviri grubunu ve sunucuyu seç"))
	}
	head := []string{
		row2(title, badge, inner),
		row2(strings.Join(subParts, stFaint.Render("  ·  ")), keycap("←→")+" "+stMuted.Render("panel"), inner),
		"",
	}
	bodyH := max(6, h-len(head))

	var body []string
	switch {
	case p.err != nil:
		errLines := []string{
			"",
			stErr.Bold(true).Render("✗ seçenekler alınamadı"),
			stMuted.Render(truncate(shortErr(p.err), inner-6)),
			"",
			keycap("esc") + " " + stMuted.Render("geri dön"),
		}
		body = panel("seçici", errLines, inner, bodyH, true, cErr)
	case p.groups == nil:
		waitLines := []string{
			"",
			fg(acc).Render(spinner(a.frame/2)) + stSub.Render(" çeviri grupları ve sunucular aranıyor…"),
		}
		body = panel("seçici", waitLines, inner, bodyH, true, acc)
	default:
		if inner >= 88 {
			lw := clamp(inner*40/100, 32, 46)
			rw := inner - lw - kitGap
			left := panel("çeviri grupları", p.novaFansubLines(a, lw-4, bodyH-2, acc), lw, bodyH, p.focus == 0, acc, fmt.Sprintf("%d grup", len(p.groups)))
			rightTag := ""
			if g := p.group(); g != nil {
				rightTag = fmt.Sprintf("%d sunucu", len(g.vs))
			}
			right := panel("sunucu · kalite", p.novaHostLines(a, rw-4, bodyH-2, acc), rw, bodyH, p.focus == 1, acc, rightTag)
			body = hjoin(kitGap, left, right)
		} else {
			// Narrow terminal: focused panel takes full width with a clear tab strip.
			t0, t1 := stMuted.Render("çeviri grupları"), stMuted.Render("sunucu · kalite")
			if p.focus == 0 {
				t0 = fg(acc).Bold(true).Render("▸ çeviri grupları")
			} else {
				t1 = fg(acc).Bold(true).Render("▸ sunucu · kalite")
			}
			tabLine := row2(t0+stFaint.Render("   ·   ")+t1, keycap("←→"), inner)
			ph := max(5, bodyH-1)
			if p.focus == 0 {
				body = append([]string{tabLine}, panel("çeviri grupları", p.novaFansubLines(a, inner-4, ph-2, acc), inner, ph, true, acc, fmt.Sprintf("%d grup", len(p.groups)))...)
			} else {
				body = append([]string{tabLine}, panel("sunucu · kalite", p.novaHostLines(a, inner-4, ph-2, acc), inner, ph, true, acc)...)
			}
		}
	}

	out := block(append(head, body...), inner, h)
	for i := range out {
		out[i] = strings.Repeat(" ", pad) + out[i]
	}
	return strings.Join(out, "\n")
}

func (p *picker) novaFansubLines(a *App, w, h int, acc string) []string {
	var out []string
	rows := max(1, h-3)
	p.fOff = scroll(p.fCur, p.fOff, rows, len(p.groups))
	for i := p.fOff; i < len(p.groups) && len(out) < rows; i++ {
		g := p.groups[i]
		sel := i == p.fCur
		bg, bar, tc := "", sp("  ", cText), cText
		if sel {
			bar, tc = sp("▌ ", acc), acc
			if p.focus == 0 {
				bg = cBand
			} else {
				bg = cSurface
			}
		}
		var dots []span
		for _, v := range g.vs {
			st := a.resolved[v.URL]
			switch {
			case st == nil:
				dots = append(dots, sp("·", cFaint))
			case st.loading:
				dots = append(dots, sp(spinner(a.frame/2), acc))
			case st.st != nil:
				dots = append(dots, sp("●", cOK))
			default:
				dots = append(dots, sp("●", cErr))
			}
		}
		if len(dots) > 8 {
			dots = append(dots[:8], sp("+", cMuted))
		}
		dots = append(dots, sp(" ", cText))

		name := g.name
		if p.prefFan != "" && strings.EqualFold(g.name, shortFansub(p.prefFan)) {
			name += " ★"
		}
		var tags []span
		if g.dub() && !isDub(g.name) {
			tags = append(tags, sp("dublaj ", cAccent2))
		}
		if l := g.lang(); l != "" && l != "tr" && !strings.Contains(strings.ToLower(g.name), "("+l+")") {
			tags = append(tags, sp(strings.ToUpper(l)+" ", cMuted))
		}
		line := row(w, bg, []span{bar, span{t: name, c: tc, bold: sel}}, append(tags, dots...))
		if sel && p.focus == 0 {
			line = anchor() + line
		}
		out = append(out, line)
	}
	if src := source.Get(p.an.Source); src != nil && src.Info().Lang == "en" && len(out)+3 <= h {
		out = append(out, "", stWarn.Render("! ")+stSub.Render("İngilizce kaynak"),
			stMuted.Render(truncate("Türkçe için esc → s", w)))
	}
	if g := p.group(); g != nil && len(g.credits) > 0 && len(out)+2 <= h {
		out = append(out, stFaint.Render(strings.Repeat("─", w)))
		for _, c := range g.credits {
			for _, l := range wrap(c, w) {
				if len(out) < h {
					out = append(out, stMuted.Italic(true).Render(l))
				}
			}
		}
	}
	return block(out, w, h)
}

func (p *picker) novaHostLines(a *App, w, h int, acc string) []string {
	g := p.group()
	if g == nil {
		return block(nil, w, h)
	}
	var out []string
	rows := p.rows(a)
	p.hCur = clamp(p.hCur, 0, len(rows)-1)
	ri := 0
	for _, v := range g.vs {
		st := a.resolved[v.URL]
		host := v.Host
		if host == "" {
			host = "Oynatıcı"
		}
		var status []span
		rec, hasRec := p.hostRecord(v)
		switch {
		case st == nil && recentlyFailed(a, v.URL):
			status = []span{sp("son denemede açılmadı ", cWarn)}
		case st == nil && hasRec:
			status = []span{recordSpan(rec)}
		case st == nil:
			status = []span{sp("enter: çöz ", cMuted)}
		case st.loading && hasRec && w >= 52:
			status = []span{recordSpan(rec), sp(spinner(a.frame/2)+" çözülüyor ", acc)}
		case st.loading:
			status = []span{sp(spinner(a.frame/2)+" çözülüyor ", acc)}
		case st.st != nil:
			status = []span{sp("✓ hazır ", cOK)}
		default:
			status = []span{sp("✗ "+truncate(shortErr(st.err), max(10, w/2))+" ", cErr)}
		}
		if st != nil && st.st != nil {
			out = append(out, row(w, "", []span{sp("● ", cOK), spb(host, cText)}, status))
			for qi, q := range st.st.Qualities {
				sel := p.focus == 1 && ri == p.hCur
				bg, bar, tc := "", sp("    ", cText), cSub
				if sel {
					bg, bar, tc = cBand, sp("  ▌ ", acc), acc
				}
				lbl := q.DisplayLabel()
				var right []span
				if q.Broken {
					right = append(right, sp("bozuk  ", cErr))
				}
				if s := humanSize(q.Size); s != "" {
					right = append(right, sp(s, cMuted))
				}
				if qi == 0 && !q.Broken {
					right = append(right, sp("  en iyi", cOK))
				}
				if len(st.st.Subtitles) > 0 && qi == 0 {
					right = append(right, sp("  "+st.st.Subtitles[0].Label, cAccent2))
				}
				right = append(right, sp(" ", cText))
				line := row(w, bg, []span{bar, span{t: lbl, c: tc, bold: sel}}, right)
				if sel {
					line = anchor() + line
				}
				out = append(out, line)
				ri++
			}
		} else {
			sel := p.focus == 1 && ri == p.hCur
			bg, bar, tc := "", sp("  ", cText), cText
			if sel {
				bg, bar, tc = cBand, sp("▌ ", acc), acc
			}
			line := row(w, bg, []span{bar, span{t: host, c: tc, bold: true}}, status)
			if sel {
				line = anchor() + line
			}
			out = append(out, line)
			ri++
		}
	}
	if p.pending {
		out = append(out, "", fg(acc).Render(spinner(a.frame/2))+stSub.Render(" en iyi kaynak hazırlanıyor, mpv açılacak…"))
	}
	if len(out) > h {
		target := 0
		for i, l := range out {
			if strings.Contains(l, "▌") {
				target = i
				break
			}
		}
		start := clamp(target-h/2, 0, len(out)-h)
		out = out[start : start+h]
	}
	return block(out, w, h)
}
