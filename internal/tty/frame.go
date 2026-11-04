// Package tty is aniclitr's terminal renderer.
//
// Design (why it exists): Bubble Tea v1 rewrites a whole line when anything
// on it changes, which wipes sixel pictures on that line (they blink). A
// first replacement (x/cellbuf diffing with cursor-movement tricks) drifted
// out of sync with the real terminal — a single disagreement about a
// glyph's width or a cursor optimisation the terminal read differently left
// garbage behind for good. This renderer is built so that cannot persist:
//
//   - Double buffering: Frame is a plain cell grid. The writer keeps the
//     front frame (what the terminal shows) and diffs each new back frame
//     against it.
//   - Mailbox ("triple") buffering: the UI submits frames without ever
//     blocking; the writer goroutine always renders the latest one and
//     drops stale ones, paced to ~60 fps.
//   - Absolute addressing only: every changed run starts with CUP, and the
//     cursor is re-anchored after any glyph whose width terminals disagree
//     on. No LF/REP/relative moves, autowrap off — nothing can drift or scroll.
//   - Self-healing: after a burst of activity the writer quietly rewrites
//     every text cell once; ctrl+l / resize do a full clear-and-repaint.
//   - Images are a layer: their rectangles are reserved in the grid, the
//     text diff never writes into them, and they are re-sent only when they
//     appear or move. Removing one forces its cells to be rewritten.
//   - Every frame is wrapped in synchronized-output markers (mode 2026).
package tty

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Colour encoding: 0 = terminal default; bit 24 set = truecolor RGB;
// bit 25 set = indexed (low byte).
const (
	colRGB   = 1 << 24
	colIndex = 1 << 25
)

// Attr bits.
const (
	Bold = 1 << iota
	Faint
	Italic
	Underline
	Reverse
	Strike
)

// Style is a cell's look.
type Style struct {
	FG, BG uint32
	Attr   uint8
}

// SGR renders the style as one absolute sequence (always starts with a reset).
func (s Style) SGR() string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	for i, code := range []string{"1", "2", "3", "4", "7", "9"} {
		if s.Attr&(1<<i) != 0 {
			b.WriteByte(';')
			b.WriteString(code)
		}
	}
	writeCol(&b, s.FG, "38")
	writeCol(&b, s.BG, "48")
	b.WriteByte('m')
	return b.String()
}

func writeCol(b *strings.Builder, c uint32, base string) {
	switch {
	case c&colRGB != 0:
		b.WriteString(";" + base + ";2;")
		b.WriteString(strconv.Itoa(int(c>>16) & 0xff))
		b.WriteByte(';')
		b.WriteString(strconv.Itoa(int(c>>8) & 0xff))
		b.WriteByte(';')
		b.WriteString(strconv.Itoa(int(c) & 0xff))
	case c&colIndex != 0:
		b.WriteString(";" + base + ";5;" + strconv.Itoa(int(c&0xff)))
	}
}

// Cell is one terminal cell. W is the glyph width (1 or 2); 0 marks the
// second half of a wide glyph. Img > 0 reserves the cell for a picture.
type Cell struct {
	G   string
	W   uint8
	S   Style
	Img int32
}

var blank = Cell{G: " ", W: 1}

// Image is a picture drawn over a reserved rectangle.
type Image struct {
	Key            string // identity of the encoded data (url|w|h)
	Row, Col, W, H int
	Data           string // full escape sequence (sixel DCS)
}

func (im Image) same(o Image) bool {
	return im.Key == o.Key && im.Row == o.Row && im.Col == o.Col && im.W == o.W && im.H == o.H
}

// Frame is a full screen of cells plus pictures.
type Frame struct {
	W, H   int
	Cells  []Cell
	Images []Image
	Bg     string // terminal background (OSC 11), "" to leave alone, "reset" to restore
}

// NewFrame returns a blank w×h frame.
func NewFrame(w, h int) *Frame {
	f := &Frame{W: w, H: h, Cells: make([]Cell, w*h)}
	for i := range f.Cells {
		f.Cells[i] = blank
	}
	return f
}

// A screen of cells is ~0.5 MB at 200×56; building one per frame made most
// of the UI's garbage. Frames are recycled once nobody reads them: the
// writer hands back the one it replaced on screen (and any it skipped).
//
// A small fixed free list, not sync.Pool: the pool is emptied by every GC,
// and at most three frames are alive at once (on screen, pending, building).
var framePool = make(chan *Frame, 4)

// GetFrame is NewFrame, reusing a recycled frame when one fits.
func GetFrame(w, h int) *Frame {
	select {
	case f := <-framePool:
		if cap(f.Cells) >= w*h {
			f.W, f.H, f.Bg = w, h, ""
			f.Cells = f.Cells[:w*h]
			for i := range f.Cells {
				f.Cells[i] = blank
			}
			clear(f.Images) // drop the old pictures' data
			f.Images = f.Images[:0]
			return f
		}
	default:
	}
	return NewFrame(w, h)
}

// Recycle gives back a frame nothing will read again.
func Recycle(f *Frame) {
	if f == nil {
		return
	}
	select {
	case framePool <- f:
	default: // enough spares
	}
}

// At returns the cell at (x, y).
func (f *Frame) At(x, y int) Cell { return f.Cells[y*f.W+x] }

// Place adds a picture and reserves its cells. Pictures that would touch
// the last row are refused: drawing there can scroll the terminal.
func (f *Frame) Place(im Image) bool {
	if im.Data == "" || im.W <= 0 || im.H <= 0 || im.Row < 0 || im.Col < 0 ||
		im.Col+im.W > f.W || im.Row+im.H >= f.H {
		return false
	}
	// a wide glyph straddling the picture's edge would be half under it:
	// replace the half outside with a blank
	for y := im.Row; y < im.Row+im.H; y++ {
		bl := func(x int) {
			c := &f.Cells[y*f.W+x]
			*c = Cell{G: " ", W: 1, S: c.S, Img: c.Img}
		}
		if l := im.Col - 1; l >= 0 && f.Cells[y*f.W+l].W == 2 {
			bl(l)
			bl(im.Col)
		}
		if r := im.Col + im.W; r < f.W && f.Cells[y*f.W+r].W == 0 {
			bl(r)
			bl(r - 1)
		}
		// a width-disputed glyph right before the picture could spill into it
		if l := im.Col - 1; l >= 0 && risky(f.Cells[y*f.W+l].G) {
			bl(l)
		}
	}
	f.Images = append(f.Images, im)
	id := int32(len(f.Images))
	for y := im.Row; y < im.Row+im.H; y++ {
		for x := im.Col; x < im.Col+im.W; x++ {
			f.Cells[y*f.W+x].Img = id
		}
	}
	return true
}

// SetLines parses styled text lines (ANSI SGR) into the grid. Other escape
// sequences (OSC, APC, DCS, cursor movement) are dropped: the frame is pure
// cells. Text past the right edge is clipped.
func (f *Frame) SetLines(lines []string) {
	for y := 0; y < f.H && y < len(lines); y++ {
		f.setLine(y, lines[y])
	}
}

func (f *Frame) setLine(y int, s string) {
	var st Style
	x := 0
	row := f.Cells[y*f.W : (y+1)*f.W]
	for i := 0; i < len(s) && x < f.W; {
		c := s[i]
		if c == 0x1b {
			i = f.escape(s, i, &st)
			continue
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		cluster, _, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
		i += len(cluster)
		w := ansi.StringWidth(cluster)
		if w <= 0 {
			// zero-width (combining) — attach to the previous glyph
			if x > 0 {
				p := x - 1
				if row[p].W == 0 && p > 0 {
					p--
				}
				row[p].G += cluster
			}
			continue
		}
		if w > 2 {
			w = 2
		}
		if x+w > f.W {
			break
		}
		// overwriting half of an existing wide glyph: blank its other half
		if row[x].W == 0 && x > 0 {
			row[x-1] = Cell{G: " ", W: 1, S: row[x-1].S}
		}
		row[x] = Cell{G: cluster, W: uint8(w), S: st}
		if w == 2 {
			row[x+1] = Cell{G: "", W: 0, S: st}
		}
		x += w
	}
}

// escape consumes one escape sequence starting at s[i] and returns the
// index after it; SGR updates st.
func (f *Frame) escape(s string, i int, st *Style) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j >= len(s) {
			return len(s)
		}
		if s[j] == 'm' {
			applySGR(st, s[i+2:j])
		}
		return j + 1
	case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS: skip to ST or BEL
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return i + 2
}

func applySGR(st *Style, params string) {
	if params == "" {
		*st = Style{}
		return
	}
	var buf [16]int
	ps := buf[:0]
	for i := 0; i < len(params); {
		j := i
		for j < len(params) && params[j] != ';' && params[j] != ':' {
			j++
		}
		if j > i {
			v, _ := strconv.Atoi(params[i:j]) // a substring: no allocation
			ps = append(ps, v)
		}
		i = j + 1
	}
	n := func(k int) int {
		if k >= len(ps) {
			return 0
		}
		return ps[k]
	}
	for k := 0; k < len(ps); k++ {
		p := n(k)
		switch {
		case p == 0:
			*st = Style{}
		case p == 1:
			st.Attr |= Bold
		case p == 2:
			st.Attr |= Faint
		case p == 3:
			st.Attr |= Italic
		case p == 4:
			st.Attr |= Underline
		case p == 7:
			st.Attr |= Reverse
		case p == 9:
			st.Attr |= Strike
		case p == 22:
			st.Attr &^= Bold | Faint
		case p == 23:
			st.Attr &^= Italic
		case p == 24:
			st.Attr &^= Underline
		case p == 27:
			st.Attr &^= Reverse
		case p == 29:
			st.Attr &^= Strike
		case p >= 30 && p <= 37:
			st.FG = colIndex | uint32(p-30)
		case p >= 90 && p <= 97:
			st.FG = colIndex | uint32(p-90+8)
		case p == 39:
			st.FG = 0
		case p >= 40 && p <= 47:
			st.BG = colIndex | uint32(p-40)
		case p >= 100 && p <= 107:
			st.BG = colIndex | uint32(p-100+8)
		case p == 49:
			st.BG = 0
		case p == 38 || p == 48:
			var c uint32
			switch n(k + 1) {
			case 2:
				c = colRGB | uint32(n(k+2)&0xff)<<16 | uint32(n(k+3)&0xff)<<8 | uint32(n(k+4)&0xff)
				k += 4
			case 5:
				c = colIndex | uint32(n(k+2)&0xff)
				k += 2
			default:
				k++
				continue
			}
			if p == 38 {
				st.FG = c
			} else {
				st.BG = c
			}
		}
	}
}

// risky reports glyphs whose width terminals and libraries may disagree on
// (symbols, private-use icons, CJK, emoji). After writing one, the writer
// re-anchors the cursor so a disagreement cannot shift what follows.
func risky(g string) bool {
	for _, r := range g {
		switch {
		case r < 0x2000:
			// Latin, Turkish letters, Greek, Cyrillic… narrow everywhere
		case r >= 0x2500 && r <= 0x259f:
			// box drawing + block elements: narrow everywhere
		default:
			return true
		}
	}
	return utf8.RuneCountInString(g) > 1 // multi-rune cluster (emoji, combining)
}
