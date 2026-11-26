package ui

// Nova playing screen: a calm, studio-style "now playing" stage framed in the
// anime's cover accent with structured stream chips, OP/ED markers under the
// progress bar, and a keycap control strip underneath. Stays idle while mpv is
// open (zero extra animation ticks).

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/aniskip"
	"aniclitr/internal/player"
	"aniclitr/internal/shaders"
)

func (p *playing) novaView(a *App, w, h int) string {
	cw := clamp(w-8, 52, 96)
	innerW := cw - 4
	acc := a.posters.accent(p.an.Poster, cAccent).hex()

	posterW := 0
	if a.cfg.Posters && innerW >= 58 && h >= 18 {
		posterW = min(20, innerW/4)
		if a.posters.sixel() {
			posterW = min(16, innerW/5)
		}
	}
	tw := innerW - posterW
	if posterW > 0 {
		tw -= 3
	}

	panelTitle := "▶ oynatılıyor"
	panelAcc := acc
	switch {
	case p.err != nil:
		panelTitle, panelAcc = "✗ hata", cErr
	case !p.countdown.IsZero():
		panelTitle, panelAcc = "✓ bölüm bitti", cOK
	case !p.started:
		panelTitle, panelAcc = spinner(a.frame/2)+" mpv açılıyor", cAccent2
	case p.paused:
		panelTitle, panelAcc = "❚❚ duraklatıldı", cWarn
	}

	panelTag := p.host
	if p.q.Label != "" {
		if panelTag != "" {
			panelTag += " · " + p.q.Label
		} else {
			panelTag = p.q.Label
		}
	}

	var info []string
	for i, l := range wrap(p.an.Title, tw) {
		if i == 0 {
			info = append(info, gradient(l, []string{acc, cAccent2}, true))
		} else if i < 2 {
			info = append(info, stBold.Render(l))
		}
	}
	epLine := fmt.Sprintf("Sezon %d · Bölüm %s", p.ep.Season, p.ep.Num())
	if p.an.IsMovie() {
		epLine = "Film"
	}
	if p.ep.Title != "" && !genericTitle(p.ep.Title) && !p.an.IsMovie() {
		epLine += "  —  " + p.ep.Title
	}
	info = append(info, stText.Render(truncate(epLine, tw)), "")

	// Stream metadata chips.
	var chips []string
	if p.fansub != "" {
		chips = append(chips, fg(cText).Background(colorOf(cSurface)).Render(" "+truncate(shortFansub(p.fansub), tw/2)+" "))
	}
	if p.host != "" {
		chips = append(chips, fg(cSub).Background(colorOf(cSurface)).Render(" "+p.host+" "))
	}
	if p.q.Label != "" {
		chips = append(chips, pill(p.q.Label, acc))
	}
	if p.activeSub != "" {
		chips = append(chips, fg(cOK).Background(colorOf(cSurface)).Render(" altyazı: "+truncate(p.activeSub, max(12, tw/2))+" "))
	} else if p.st != nil && len(p.st.Subtitles) > 0 {
		chips = append(chips, fg(cAccent2).Background(colorOf(cSurface)).Render(" altyazı: "+p.st.Subtitles[0].Label+" "))
	}
	if math.Abs(p.subDelay) >= 0.05 {
		chips = append(chips, fg(cWarn).Background(colorOf(cSurface)).Bold(true).Render(" senkron: "+player.FormatSubDelay(p.subDelay)+" "))
	}
	if pipe := (shaders.Pipeline{Res: a.cfg.UpscaleRes, Enhance: a.cfg.Anime4K, FrameGen: a.cfg.FrameGen}); shaders.SummaryBadge(pipe) != "Orijinal" {
		chips = append(chips, fg(cAccent2).Background(colorOf(cSurface)).Bold(true).Render(" "+shaders.SummaryBadge(pipe)+" "))
	}
	if sp := a.sleep.shortPill(); sp != "" {
		chips = append(chips, fg(cWarn).Background(colorOf(cSurface)).Bold(true).Render(" "+sp+" "))
	}
	if a.party != nil {
		chips = append(chips, fg(cOK).Background(colorOf(cSurface)).Bold(true).Render(fmt.Sprintf(" ◈ oda · %d ", a.party.PeerCount())))
	} else if a.relayParty != nil {
		chips = append(chips, fg(cOK).Background(colorOf(cSurface)).Bold(true).Render(" ◈ oda · "+a.relayParty.Code()+" "))
	}
	if p.prefetched != nil {
		chips = append(chips, fg(cOK).Background(colorOf(cSurface)).Render(" ⚡ sıradaki hazır "))
	}
	if len(chips) > 0 {
		info = append(info, wrapChips(chips, tw)...)
	}
	if ts := p.torrentStatusLine(); ts != "" {
		info = append(info, fg(cSky).Render("✦ ")+stSub.Render(truncate(ts, max(8, tw-2))))
	}

	segs := aniskip.ForDuration(p.segs, p.dur)
	if len(segs) > 0 {
		var ks []string
		for _, s := range segs {
			ks = append(ks, s.Label())
		}
		info = append(info, fg(cAccent2).Render("↷ ")+stMuted.Render("otomatik atlama: "+strings.Join(ks, ", ")))
	}
	info = append(info, "")

	// Progress bar + clock + percentage.
	frac := 0.0
	if p.dur > 0 {
		frac = p.pos / p.dur
	}
	pct := ""
	if p.dur > 0 {
		pct = fmt.Sprintf("  %%%d", int(frac*100))
	}
	tl := stText.Bold(true).Render(clock(p.pos)) + stMuted.Render(" / "+clock(p.dur)+pct)
	barW := max(8, tw-lipgloss.Width(tl)-2)
	info = append(info, progressBar(barW, frac, []string{acc, cAccent2})+"  "+tl)

	// OP/ED skip indicators directly under the timeline bar.
	if len(segs) > 0 && p.dur > 0 && barW > 10 {
		marks := []rune(strings.Repeat(" ", barW))
		for _, s := range segs {
			for x := int(s.Start / p.dur * float64(barW)); x < int(s.End/p.dur*float64(barW)) && x < barW; x++ {
				if x >= 0 {
					marks[x] = '▔'
				}
			}
		}
		info = append(info, fg(cAccent2).Render(string(marks)))
	}
	if p.skipText != "" && time.Since(p.skipAt) < 90*time.Second {
		info = append(info, fg(acc).Bold(true).Render("↷ "+p.skipText+" atlandı"))
	}

	info = append(info, p.subLines(a, tw)...)

	if p.err != nil {
		info = append(info, "",
			stErr.Render(truncate(shortErr(p.err), tw)),
			stMuted.Render("Bağlantı süresi dolmuş ya da sunucu yanıt vermiyor olabilir."),
			keycap("enter")+" "+stSub.Render("başka kaynak seç")+"   "+keycap("esc")+" "+stMuted.Render("geri"))
	}
	if !p.countdown.IsZero() {
		left := time.Until(p.countdown).Seconds()
		if left < 0 {
			left = 0
		}
		info = append(info, "")
		if p.nextEp != nil {
			info = append(info,
				stText.Bold(true).Render(fmt.Sprintf("Sıradaki: %s. Bölüm", p.nextEp.Num()))+stMuted.Render(fmt.Sprintf("  ·  %.0f sn", left+.5)),
				progressBar(tw, 1-left/6, []string{acc, cAccent2}))
		} else {
			info = append(info, fg(acc).Render(spinner(a.frame/2))+stMuted.Render(" sıradaki bölüm aranıyor…"))
		}
	}

	var card []string
	if posterW > 0 {
		ph := clamp(posterW*3/4, 6, max(6, h-10))
		bh := max(len(info), ph)
		card = hjoin(3, block(a.posters.render(p.an.Poster, posterW, ph, a.frame), posterW, bh), block(info, tw, bh))
	} else {
		card = block(info, tw, len(info))
	}

	// Pad top/bottom inside the panel for airy breathing room.
	innerLines := append([]string{""}, card...)
	innerLines = append(innerLines, "")
	panelH := min(h-2, len(innerLines)+2)
	boxed := panel(panelTitle, innerLines, cw, panelH, true, panelAcc, panelTag)

	top := max(0, (h-len(boxed)-2)/2)
	out := make([]string, 0, h)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range boxed {
		out = append(out, center(l, w))
	}
	if len(out)+2 <= h && p.sess != nil && !p.subs.open {
		ctrls := strings.Join([]string{
			keycap("m") + " " + stSub.Render("hızlı menü (1080p/2K/4K)"),
			keycap("space") + " " + stMuted.Render("duraklat"),
			keycap("←→") + " " + stMuted.Render("10 sn"),
			keycap("n/b") + " " + stMuted.Render("bölüm"),
			keycap("y") + " " + stMuted.Render("altyazı"),
			keycap("z/x") + " " + stMuted.Render("senkron"),
			keycap("s") + " " + stMuted.Render("durdur"),
		}, "  ")
		out = append(out, "", center(truncate(ctrls, w-4), w))
	}
	return strings.Join(block(out, w, h), "\n")
}
