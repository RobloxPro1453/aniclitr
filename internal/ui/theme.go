package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
func (s span) width() int  { return plainWidth(s.t) }
func (s span) style(bg string) lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(s.c)).Bold(s.bold).Italic(s.italic)
	if bg != "" {
		st = st.Background(lipgloss.Color(bg))
	}
	return st
}

// writeSpan writes a styled span directly into b without allocating a
// lipgloss.Style on the hot path (TrueColor and Ascii profiles). Non-TrueColor
// terminals fall back cleanly to lipgloss.
func writeSpan(b *strings.Builder, s span, t, bg string) {
	if t == "" {
		return
	}
	prof := lipgloss.ColorProfile()
	if prof == termenv.Ascii {
		b.WriteString(t)
		return
	}
	if prof != termenv.TrueColor {
		b.WriteString(s.style(bg).Render(t))
		return
	}
	fr, fgC, fb, hasFG := parseHexRGB(s.c)
	br, bgC, bb, hasBG := parseHexRGB(bg)
	if !hasFG && !hasBG && !s.bold && !s.italic {
		b.WriteString(t)
		return
	}
	var seq [48]byte
	buf := append(seq[:0], '\x1b', '[')
	needSep := false
	if s.bold {
		buf = append(buf, '1')
		needSep = true
	}
	if s.italic {
		if needSep {
			buf = append(buf, ';')
		}
		buf = append(buf, '3')
		needSep = true
	}
	if hasFG {
		if needSep {
			buf = append(buf, ';')
		}
		buf = append(buf, '3', '8', ';', '2', ';')
		buf = strconv.AppendUint(buf, uint64(fr), 10)
		buf = append(buf, ';')
		buf = strconv.AppendUint(buf, uint64(fgC), 10)
		buf = append(buf, ';')
		buf = strconv.AppendUint(buf, uint64(fb), 10)
		needSep = true
	}
	if hasBG {
		if needSep {
			buf = append(buf, ';')
		}
		buf = append(buf, '4', '8', ';', '2', ';')
		buf = strconv.AppendUint(buf, uint64(br), 10)
		buf = append(buf, ';')
		buf = strconv.AppendUint(buf, uint64(bgC), 10)
		buf = append(buf, ';')
		buf = strconv.AppendUint(buf, uint64(bb), 10)
	}
	buf = append(buf, 'm')
	b.Write(buf)
	b.WriteString(t)
	b.WriteString("\x1b[0m")
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
	b.Grow(w*2 + 64)
	used := 0
	for _, s := range left {
		if used >= avail {
			break
		}
		t := s.t
		sw := plainWidth(t)
		if used+sw > avail {
			t = truncate(t, avail-used)
			sw = plainWidth(t)
		}
		used += sw
		writeSpan(&b, s, t, bg)
	}
	pad := w - used - rw
	if pad > 0 {
		writeFill(&b, pad, bg)
	}
	if rw > 0 && rw <= w {
		for _, s := range right {
			writeSpan(&b, s, s.t, bg)
		}
	}
	return b.String()
}

var spaceRun = strings.Repeat(" ", 256)

func writeSpaces(b *strings.Builder, n int) {
	for n > len(spaceRun) {
		b.WriteString(spaceRun)
		n -= len(spaceRun)
	}
	if n > 0 {
		b.WriteString(spaceRun[:n])
	}
}

func writeFill(b *strings.Builder, n int, bg string) {
	if n <= 0 {
		return
	}
	if bg == "" || lipgloss.ColorProfile() == termenv.Ascii {
		writeSpaces(b, n)
		return
	}
	if lipgloss.ColorProfile() == termenv.TrueColor {
		if r, g, bl, ok := parseHexRGB(bg); ok {
			var seq [24]byte
			buf := append(seq[:0], '\x1b', '[', '4', '8', ';', '2', ';')
			buf = strconv.AppendUint(buf, uint64(r), 10)
			buf = append(buf, ';')
			buf = strconv.AppendUint(buf, uint64(g), 10)
			buf = append(buf, ';')
			buf = strconv.AppendUint(buf, uint64(bl), 10)
			buf = append(buf, 'm')
			b.Write(buf)
			writeSpaces(b, n)
			b.WriteString("\x1b[0m")
			return
		}
	}
	b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(strings.Repeat(" ", n)))
}

func fill(n int, bg string) string {
	if n <= 0 {
		return ""
	}
	if bg == "" || lipgloss.ColorProfile() == termenv.Ascii {
		if n <= len(spaceRun) {
			return spaceRun[:n]
		}
		return strings.Repeat(" ", n)
	}
	var b strings.Builder
	b.Grow(n + 28)
	writeFill(&b, n, bg)
	return b.String()
}

type rgb struct{ r, g, b float64 }

func hexNibble(ch byte) (uint8, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0', true
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10, true
	case ch >= 'A' && ch <= 'F':
		return ch - 'A' + 10, true
	default:
		return 0, false
	}
}

func parseHexRGB(c string) (uint8, uint8, uint8, bool) {
	if len(c) == 7 && c[0] == '#' {
		c = c[1:]
	}
	if len(c) != 6 {
		return 0, 0, 0, false
	}
	h0, ok0 := hexNibble(c[0])
	h1, ok1 := hexNibble(c[1])
	h2, ok2 := hexNibble(c[2])
	h3, ok3 := hexNibble(c[3])
	h4, ok4 := hexNibble(c[4])
	h5, ok5 := hexNibble(c[5])
	if !(ok0 && ok1 && ok2 && ok3 && ok4 && ok5) {
		return 0, 0, 0, false
	}
	return h0<<4 | h1, h2<<4 | h3, h4<<4 | h5, true
}

func hex(c string) rgb {
	if r, g, b, ok := parseHexRGB(c); ok {
		return rgb{float64(r), float64(g), float64(b)}
	}
	return rgb{}
}

const hexDigits = "0123456789ABCDEF"

func clampByte(x float64) uint8 {
	if x <= 0 {
		return 0
	}
	if x >= 255 {
		return 255
	}
	return uint8(x + .5)
}

func (c rgb) hex() string {
	r, g, b := clampByte(c.r), clampByte(c.g), clampByte(c.b)
	buf := [7]byte{'#', hexDigits[r>>4], hexDigits[r&0xf], hexDigits[g>>4], hexDigits[g&0xf], hexDigits[b>>4], hexDigits[b&0xf]}
	return string(buf[:])
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

func writeRGBFG(b *strings.Builder, c rgb) {
	var seq [24]byte
	buf := append(seq[:0], '\x1b', '[', '3', '8', ';', '2', ';')
	buf = strconv.AppendUint(buf, uint64(clampByte(c.r)), 10)
	buf = append(buf, ';')
	buf = strconv.AppendUint(buf, uint64(clampByte(c.g)), 10)
	buf = append(buf, ';')
	buf = strconv.AppendUint(buf, uint64(clampByte(c.b)), 10)
	buf = append(buf, 'm')
	b.Write(buf)
}

// gradient paints each rune of s along the stops.
func gradient(s string, stops []string, bold bool) string {
	if s == "" || len(stops) == 0 {
		return s
	}
	prof := lipgloss.ColorProfile()
	if prof == termenv.Ascii {
		return s
	}
	rs := []rune(s)
	if prof == termenv.TrueColor {
		var b strings.Builder
		b.Grow(len(s) + len(rs)*20 + 8)
		if bold {
			b.WriteString("\x1b[1m")
		}
		for i, r := range rs {
			t := 0.0
			if len(rs) > 1 {
				t = float64(i) / float64(len(rs)-1)
			}
			writeRGBFG(&b, gradAt(stops, t))
			b.WriteRune(r)
		}
		b.WriteString("\x1b[0m")
		return b.String()
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
