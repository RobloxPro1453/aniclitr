package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// plainWidth measures visual cell width with a zero-alloc fast-path for
// single-line plain ASCII + Turkish/Latin/box-drawing text (no ANSI escapes,
// no combining marks or wide CJK/emoji). Falls back to lipgloss.Width otherwise.
func plainWidth(s string) int {
	if s == "" {
		return 0
	}
	w := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < 0x80 {
			if b < 0x20 || b == 0x7f {
				return lipgloss.Width(s)
			}
			w++
			i++
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		// Common single-cell Latin-1/Turkish/Greek/Cyrillic, punctuation, arrows, box/block drawing.
		if (r >= 0x00A0 && r <= 0x024F) || (r >= 0x2010 && r <= 0x2027) || (r >= 0x2190 && r <= 0x25FF) {
			w++
			i += sz
			continue
		}
		return lipgloss.Width(s)
	}
	return w
}

// truncate shortens styled or plain text to n cells with an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if plainWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

// fit returns s padded/truncated to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := plainWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	if sw == w {
		return s
	}
	pad := w - sw
	if pad <= len(spaceRun) {
		return s + spaceRun[:pad]
	}
	return s + strings.Repeat(" ", pad)
}

// block normalises lines to a w×h rectangle.
func block(lines []string, w, h int) []string {
	if h <= 0 {
		return nil
	}
	if w < 0 {
		w = 0
	}
	out := make([]string, h)
	blank := ""
	if w <= len(spaceRun) {
		blank = spaceRun[:w]
	} else {
		blank = strings.Repeat(" ", w)
	}
	for i := 0; i < h; i++ {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = blank
		}
	}
	return out
}

// hjoin glues rectangles side by side; each must already have equal height.
func hjoin(gap int, cols ...[]string) []string {
	if len(cols) == 0 {
		return nil
	}
	if gap < 0 {
		gap = 0
	}
	h := len(cols[0])
	sep := strings.Repeat(" ", gap)
	out := make([]string, h)
	for i := 0; i < h; i++ {
		var b strings.Builder
		n := gap * (len(cols) - 1)
		for _, c := range cols {
			if i < len(c) {
				n += len(c[i])
			}
		}
		b.Grow(n)
		for j, c := range cols {
			if j > 0 {
				b.WriteString(sep)
			}
			if i < len(c) {
				b.WriteString(c[i])
			}
		}
		out[i] = b.String()
	}
	return out
}

// vrule is a 1-column divider.
func vrule(h int) []string {
	if h <= 0 {
		return nil
	}
	out := make([]string, h)
	bar := stFaint.Render("│")
	for i := range out {
		out[i] = bar
	}
	return out
}

// wrap word-wraps plain text into lines of at most w cells.
func wrap(text string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range words {
			for lipgloss.Width(word) > w { // hard-break very long tokens
				if line != "" {
					out = append(out, line)
					line = ""
				}
				cut := ansi.Truncate(word, w, "")
				if cut == "" {
					rs := []rune(word)
					if len(rs) == 0 {
						break
					}
					cut = string(rs[:1])
					word = string(rs[1:])
				} else {
					word = strings.TrimPrefix(word, cut)
				}
				out = append(out, cut)
			}
			switch {
			case line == "":
				line = word
			case lipgloss.Width(line)+1+lipgloss.Width(word) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// center pads s to sit in the middle of w cells.
func center(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw >= w {
		return truncate(s, w)
	}
	l := (w - sw) / 2
	return strings.Repeat(" ", l) + s + strings.Repeat(" ", w-sw-l)
}

// progressBar renders a smooth gradient bar using eighth blocks.
func progressBar(w int, frac float64, stops []string) string {
	if w <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	eighths := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	total := frac * float64(w)
	full := int(total)
	rem := int((total - float64(full)) * 8)
	var b strings.Builder
	prof := lipgloss.ColorProfile()
	denom := float64(max(w-1, 1))
	switch prof {
	case termenv.Ascii:
		b.Grow(w * 3)
		for i := 0; i < full; i++ {
			b.WriteString("█")
		}
		n := full
		if n < w && rem > 0 {
			b.WriteString(eighths[rem])
			n++
		}
		for i := n; i < w; i++ {
			b.WriteString("─")
		}
		return b.String()
	case termenv.TrueColor:
		b.Grow(w*22 + 16)
		for i := 0; i < full; i++ {
			writeRGBFG(&b, gradAt(stops, float64(i)/denom))
			b.WriteString("█")
		}
		n := full
		if n < w && rem > 0 {
			writeRGBFG(&b, gradAt(stops, float64(n)/denom))
			b.WriteString(eighths[rem])
			n++
		}
		if n > 0 {
			b.WriteString("\x1b[0m")
		}
		if n < w {
			writeRGBFG(&b, hex(cFaint))
			for i := n; i < w; i++ {
				b.WriteString("─")
			}
			b.WriteString("\x1b[0m")
		}
		return b.String()
	}
	for i := 0; i < full; i++ {
		c := gradAt(stops, float64(i)/denom).hex()
		b.WriteString(fg(c).Render("█"))
	}
	n := full
	if n < w && rem > 0 {
		c := gradAt(stops, float64(n)/denom).hex()
		b.WriteString(fg(c).Render(eighths[rem]))
		n++
	}
	if n < w {
		b.WriteString(stFaint.Render(strings.Repeat("─", w-n)))
	}
	return b.String()
}

// miniBar is a compact 1-row meter for list rows.
func miniBar(w int, frac float64, c string) []span {
	if w <= 0 {
		return nil
	}
	n := int(frac*float64(w) + .5)
	if n < 0 {
		n = 0
	}
	if n > w {
		n = w
	}
	return []span{sp(strings.Repeat("━", n), c), sp(strings.Repeat("━", w-n), cFaint)}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinner(frame int) string {
	idx := frame % len(spinFrames)
	if idx < 0 {
		idx += len(spinFrames)
	}
	return spinFrames[idx]
}

func clock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	s := int(sec)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// calendarDayDiff returns the whole-day calendar difference between t and now
// in the local timezone, immune to DST transitions and sub-day truncation.
func calendarDayDiff(t, now time.Time) int {
	tl, nl := t.Local(), now.Local()
	d1 := time.Date(tl.Year(), tl.Month(), tl.Day(), 12, 0, 0, 0, time.UTC)
	d0 := time.Date(nl.Year(), nl.Month(), nl.Day(), 12, 0, 0, 0, time.UTC)
	return int(d1.Sub(d0) / (24 * time.Hour))
}

func ago(t time.Time) string {
	return agoAt(t, time.Now())
}

func agoAt(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "yeni"
	case d < time.Minute:
		return "şimdi"
	case d < time.Hour:
		return fmt.Sprintf("%ddk", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dsa", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dg", int(d.Hours()/24))
	}
	tl := t.Local()
	return fmt.Sprintf("%d %s", tl.Day(), trMonthsShort[tl.Month()])
}

// agoLong formats t as natural Turkish relative time ("3 saat önce", "2 gün
// önce") for the past 7 days, and as a clean absolute date ("21 Eyl" or
// "21 Eyl 2025") for older timestamps — never appending "önce" to a date.
func agoLong(t time.Time) string {
	return agoLongAt(t, time.Now())
}

func agoLongAt(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "yeni"
	case d < time.Minute:
		return "şimdi"
	case d < time.Hour:
		return fmt.Sprintf("%d dk önce", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d saat önce", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d gün önce", int(d.Hours()/24))
	}
	tl, nl := t.Local(), now.Local()
	if tl.Year() != nl.Year() {
		return fmt.Sprintf("%d %s %d", tl.Day(), trMonthsShort[tl.Month()], tl.Year())
	}
	return fmt.Sprintf("%d %s", tl.Day(), trMonthsShort[tl.Month()])
}

// fmtETA formats a remaining download/playback duration in Turkish.
func fmtETA(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int(d.Round(time.Second).Seconds())
	if s <= 0 {
		s = 1
	}
	if s < 60 {
		return fmt.Sprintf("%d sn", s)
	}
	m := s / 60
	rs := s % 60
	if m < 60 {
		if rs == 0 {
			return fmt.Sprintf("%d dk", m)
		}
		return fmt.Sprintf("%d dk %d sn", m, rs)
	}
	h := m / 60
	rm := m % 60
	if rm == 0 {
		return fmt.Sprintf("%d sa", h)
	}
	return fmt.Sprintf("%d sa %d dk", h, rm)
}

func humanSize(n int64) string {
	if n <= 0 {
		return ""
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1<<20 {
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	}
	mb := float64(n) / (1 << 20)
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", mb/1024)
	}
	return fmt.Sprintf("%.0f MB", mb)
}

var (
	reCredit  = regexp.MustCompile(`(?i)^\s*(?:çevirmen|çeviri|çeviren|ceviri|translator)\s*:\s*`)
	reCutRole = regexp.MustCompile(`(?i)\s*(?:[/|,&]|\s-\s)?\s*(?:redakt[öo]r|redakte|encoder|encode|dizgi|typesetter|timer|kontrol|düzenleme|düzenleyen|editör)\s*:.*$`)
)

// shortFansub turns credit lines like "Çevirmen: Cent / Redaktör: Venus"
// into the group/translator name ("Cent"). Plain names pass through.
func shortFansub(s string) string {
	t := strings.TrimSpace(s)
	if !reCredit.MatchString(t) && !reCutRole.MatchString(t) {
		return t
	}
	t = reCredit.ReplaceAllString(t, "")
	t = reCutRole.ReplaceAllString(t, "")
	t = strings.Trim(t, " /|,-&")
	if t == "" {
		return strings.TrimSpace(s)
	}
	return t
}

// sectionTitle renders "LABEL ──────" headers.
func sectionTitle(label string, w int, active bool) string {
	c := stMuted
	if active {
		c = stText.Bold(true)
	}
	t := c.Render(strings.ToUpperSpecial(unicode.TurkishCase, label))
	rest := w - lipgloss.Width(t) - 1
	if rest <= 0 {
		return truncate(t, w)
	}
	return t + " " + stFaint.Render(strings.Repeat("─", rest))
}

// scroll keeps cursor visible inside a window of h rows.
func scroll(cursor, offset, h, n int) int {
	if h <= 0 {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+h {
		offset = cursor - h + 1
	}
	if offset > n-h {
		offset = n - h
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// scrollbar draws a thin indicator column for a list.
func scrollbar(h, offset, n int) []string {
	if h <= 0 {
		return nil
	}
	out := make([]string, h)
	if n <= h {
		for i := range out {
			out[i] = " "
		}
		return out
	}
	size := max(1, h*h/n)
	pos := offset * (h - size) / max(1, n-h)
	for i := range out {
		if i >= pos && i < pos+size {
			out[i] = stMuted.Render("┃")
		} else {
			out[i] = stFaint.Render("│")
		}
	}
	return out
}

var trMonthsShort = []string{"", "Oca", "Şub", "Mar", "Nis", "May", "Haz", "Tem", "Ağu", "Eyl", "Eki", "Kas", "Ara"}
