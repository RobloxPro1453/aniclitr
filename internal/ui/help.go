package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// dim fades a frame behind a modal: text goes faint and pictures (half
// blocks) drop out, so the eye lands on the modal.
func dim(lines []string) []string {
	r := strings.NewReplacer("▀", " ", "▄", " ", "█", " ")
	for i, l := range lines {
		lines[i] = stFaint.Render(r.Replace(ansi.Strip(l)))
	}
	return lines
}

// overlay draws box centred on top of lines (cells under the box are replaced).
func overlay(lines, box []string, w int) []string {
	if len(box) == 0 {
		return lines
	}
	bw := 0
	for _, l := range box {
		bw = max(bw, lipgloss.Width(l))
	}
	x := max(0, (w-bw)/2)
	y := max(0, (len(lines)-len(box))/2)
	out := append([]string(nil), lines...)
	for i, b := range box {
		j := y + i
		if j >= len(out) {
			break
		}
		l := fit(out[j], w)
		left := ansi.Truncate(l, x, "")
		right := ansi.TruncateLeft(l, x+bw, "")
		out[j] = left + "\x1b[0m" + fit(b, bw) + "\x1b[0m" + right
	}
	return out
}

func (a *App) helpBox() []string {
	type grp struct {
		title string
		keys  [][2]string
	}
	var screenKeys [][2]string
	for _, h := range a.top().hints() {
		screenKeys = append(screenKeys, [2]string{h.key, h.label})
	}
	groups := []grp{
		{"bu ekran", screenKeys},
		{"her yerde", [][2]string{
			{"alt+m · F10", "menü (≡)"}, {"alt+n · F9", "bildirimler (◆)"},
			{"ctrl+k", "komut paleti"}, {"ctrl+b · F2", "kitaplık"}, {"ctrl+d · F3", "indirilenler"}, {"ctrl+e · F4", "keşfet"}, {"ctrl+t", "tema seç (canlı)"},
			{"esc", "geri"}, {"/", "ara"}, {"ctrl+o", "ayarlar"}, {"alt+a", "arşiv modu"},
			{"ctrl+z", "geri al"}, {"ctrl+l", "ekranı baştan çiz"}, {"? · F1", "bu pencere"}, {"ctrl+c", "çık"},
		}},
		{"oynatırken (mpv'de de)", [][2]string{
			{"n / b", "sonraki / önceki bölüm"}, {"space", "duraklat"}, {"← →", "10 sn sar"},
		}},
		{"kitaplık", [][2]string{
			{"a", "durum · puan · listeler (her ekranda)"}, {"1-9 · 0", "puan (0 = 10)"}, {"v", "liste / poster duvarı"}, {"i", "izleme istatistikleri"},
		}},
	}
	colW := 46
	acc := hex(cAccent)
	border := fg(mix(hex(cFaint), acc, .5).hex())
	render := func(gs []grp) []string {
		var col []string
		for gi, g := range gs {
			if gi > 0 {
				col = append(col, "")
			}
			col = append(col, stMuted.Render(strings.ToUpperSpecial(unicode.TurkishCase, g.title)))
			for _, k := range g.keys {
				col = append(col, fit(stText.Bold(true).Render(k[0]), 16)+stSub.Render(truncate(k[1], colW-16)))
			}
		}
		return col
	}
	var body []string
	body = append(body, gradient("klavye", brand, true)+stMuted.Render("  ·  aniclitr"), "")
	inner := colW
	if a.w >= 2*colW+14 {
		// two columns so the whole map fits on one screen
		left, right := render(groups[:2]), render(groups[2:])
		n := max(len(left), len(right))
		for i := 0; i < n; i++ {
			var l, r string
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			body = append(body, fit(l, colW)+"    "+fit(r, colW))
		}
		inner = 2*colW + 4
	} else {
		body = append(body, render(groups)...)
	}
	body = append(body, "", stMuted.Render("herhangi bir tuş ile kapat"))
	if maxH := a.h - 6; len(body) > maxH && maxH > 4 {
		body = append(body[:maxH-1], stMuted.Render("… (pencereyi büyüt)"))
	}
	out := []string{border.Render("╭" + strings.Repeat("─", inner+4) + "╮")}
	for _, l := range body {
		out = append(out, border.Render("│")+"  "+fit(l, inner)+"  "+border.Render("│"))
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", inner+4)+"╯"))
	return out
}
