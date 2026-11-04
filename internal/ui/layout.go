package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// truncate shortens styled or plain text to n cells with an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

// fit returns s padded/truncated to exactly w cells.
func fit(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", w-sw)
}

// block normalises lines to a w×h rectangle.
func block(lines []string, w, h int) []string {
	out := make([]string, h)
	for i := 0; i < h; i++ {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return out
}

// hjoin glues rectangles side by side; each must already have equal height.
func hjoin(gap int, cols ...[]string) []string {
	if len(cols) == 0 {
		return nil
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
	out := make([]string, h)
	for i := range out {
		out[i] = stFaint.Render("│")
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
				out = append(out, cut)
				word = strings.TrimPrefix(word, cut)
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
	for i := 0; i < full; i++ {
		c := gradAt(stops, float64(i)/float64(max(w-1, 1))).hex()
		b.WriteString(fg(c).Render("█"))
	}
	n := full
	if n < w && rem > 0 {
		c := gradAt(stops, float64(n)/float64(max(w-1, 1))).hex()
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
	n := int(frac*float64(w) + .5)
	if n > w {
		n = w
	}
	return []span{sp(strings.Repeat("━", n), c), sp(strings.Repeat("━", w-n), cFaint)}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinner(frame int) string { return spinFrames[frame%len(spinFrames)] }

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

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
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
	return fmt.Sprintf("%d %s", t.Day(), trMonthsShort[t.Month()])
}

func humanSize(n int64) string {
	if n <= 0 {
		return ""
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
	out := make([]string, h)
	if n <= h || h <= 0 {
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
