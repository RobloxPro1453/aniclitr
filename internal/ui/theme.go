package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/source"
)

// Palette — every colour comes from the active Theme (see themes.go). The
// default "gece": a night ground, one sakura accent, signs before colour.
// Accent use is reserved for: the selection bar, the active tab line, the logo.
var (
	cBg      = "#0E0C14" // terminal ground (painted via OSC 11 when allowed)
	cInk     = "#0E0C14" // text on accent pills
	cText    = "#ECEAF4"
	cSub     = "#B4B0C6"
	cMuted   = "#858198" // ≥4.5:1 on the ground
	cFaint   = "#3B3849" // rules and decoration only, never text you must read
	cBand    = "#1D1A27" // selected row band
	cSurface = "#15131D"
	cShade   = "#2A2638" // skeleton shimmer peak
	cAccent  = "#FF6AA2"
	cAccent2 = "#B57BFF"
	cSky     = "#6AB8FF"
	cOK      = "#6BD39A"
	cWarn    = "#F2C14E"
	cErr     = "#FF6B7A"
)

// Brand gradient stops: accent → accent2 → sky.
var brand = []string{cAccent, cAccent2, cSky}

// sourceColor gives every provider a stable identity colour for its badge.
// sourceColor gives every source a stable colour from its role: the primary
// one takes the accent, the archive the warning tone, the rest share out the
// remaining tokens in rank order (so a new source needs no entry here).
func sourceColor(id string) string {
	rest := []string{cAccent2, cSky, cOK, cSub}
	n := 0
	for _, p := range source.All() {
		in := p.Info()
		switch {
		case in.Primary:
			if in.ID == id {
				return cAccent
			}
		case in.Legacy:
			if in.ID == id {
				return cWarn
			}
		default:
			if in.ID == id {
				return rest[n%len(rest)]
			}
			n++
		}
	}
	return cSub
}

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

func colorOf(c string) lipgloss.Color { return lipgloss.Color(c) }

var (
	stText   = fg(cText)
	stSub    = fg(cSub)
	stMuted  = fg(cMuted)
	stFaint  = fg(cFaint)
	stAccent = fg(cAccent)
	stBold   = fg(cText).Bold(true)
	stOK     = fg(cOK)
	stWarn   = fg(cWarn)
	stErr    = fg(cErr)
)

// pill is a filled label (status, badges).
func pill(text, bg string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(cInk)).Background(lipgloss.Color(bg)).Bold(true).Render(" " + text + " ")
}

// span is a run of plain text with a style; rows are built from spans so a
// background band can be applied to every piece (ANSI resets would punch holes otherwise).
type span struct {
	t      string
	c      string
	bold   bool
	italic bool
}

func sp(t, c string) span  { return span{t: t, c: c} }
func spb(t, c string) span { return span{t: t, c: c, bold: true} }
func (s span) width() int  { return lipgloss.Width(s.t) }
func (s span) style(bg string) lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(s.c)).Bold(s.bold).Italic(s.italic)
	if bg != "" {
		st = st.Background(lipgloss.Color(bg))
	}
	return st
}

// row lays out left spans and right-aligned right spans in exactly w cells.
func row(w int, bg string, left, right []span) string {
	rw := 0
	for _, s := range right {
		rw += s.width()
	}
	avail := w - rw
	if rw > 0 {
		avail-- // keep a gap
	}
	var b strings.Builder
	used := 0
	for _, s := range left {
		if used >= avail {
			break
		}
		t := s.t
		if used+s.width() > avail {
			t = truncate(t, avail-used)
		}
		used += lipgloss.Width(t)
		b.WriteString(s.style(bg).Render(t))
	}
	pad := w - used - rw
	if pad > 0 {
		b.WriteString(fill(pad, bg))
	}
	if rw > 0 && rw <= w {
		for _, s := range right {
			b.WriteString(s.style(bg).Render(s.t))
		}
	}
	return b.String()
}

func fill(n int, bg string) string {
	if n <= 0 {
		return ""
	}
	if bg == "" {
		return strings.Repeat(" ", n)
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(strings.Repeat(" ", n))
}

type rgb struct{ r, g, b float64 }

func hex(c string) rgb {
	c = strings.TrimPrefix(c, "#")
	v, _ := strconv.ParseUint(c, 16, 32)
	return rgb{float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)}
}

func (c rgb) hex() string {
	cl := func(x float64) int {
		if x < 0 {
			return 0
		}
		if x > 255 {
			return 255
		}
		return int(x + .5)
	}
	return fmt.Sprintf("#%02X%02X%02X", cl(c.r), cl(c.g), cl(c.b))
}

func mix(a, b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

// gradAt samples a multi-stop gradient at t∈[0,1].
func gradAt(stops []string, t float64) rgb {
	if t <= 0 {
		return hex(stops[0])
	}
	if t >= 1 {
		return hex(stops[len(stops)-1])
	}
	seg := t * float64(len(stops)-1)
	i := int(seg)
	return mix(hex(stops[i]), hex(stops[i+1]), seg-float64(i))
}

// gradient paints each rune of s along the stops.
func gradient(s string, stops []string, bold bool) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range rs {
		t := 0.0
		if len(rs) > 1 {
			t = float64(i) / float64(len(rs)-1)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(gradAt(stops, t).hex())).Bold(bold).Render(string(r)))
	}
	return b.String()
}
