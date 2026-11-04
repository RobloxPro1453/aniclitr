package tty

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Emulator is a minimal terminal (for tests): it understands exactly what a renderer may
// emit (CUP, SGR, ED, private modes, OSC/DCS) and records sixel pictures.
// widthOf lets a test simulate a terminal that disagrees on glyph widths.
type Emulator struct {
	w, h    int
	cells   []ecell
	cx, cy  int
	pen     Style
	saved   [2]int
	WidthOf func(g string) int
}

type ecell struct {
	g   string
	s   Style
	pic string // picture covering this cell ("" none)
	con bool   // second half of a wide glyph
}

// NewEmulator returns a blank w×h emulated terminal.
func NewEmulator(w, h int) *Emulator {
	e := &Emulator{w: w, h: h, WidthOf: ansi.StringWidth}
	e.clear()
	return e
}

func (e *Emulator) clear() {
	e.cells = make([]ecell, e.w*e.h)
	for i := range e.cells {
		e.cells[i] = ecell{g: " "}
	}
}

func (e *Emulator) put(g string, w int) {
	if e.cy < 0 || e.cy >= e.h {
		return
	}
	if e.cx >= e.w {
		e.cx = e.w - 1 // autowrap off: overwrite the last column
	}
	i := e.cy*e.w + e.cx
	// like a real terminal: overwriting either half of a wide glyph destroys
	// the whole glyph (its other half becomes a blank)
	if e.cells[i].con && e.cx > 0 {
		e.cells[i-1] = ecell{g: " ", s: e.cells[i-1].s}
	}
	last := e.cx + w - 1
	if last >= e.w {
		last = e.w - 1
	}
	if last+1 < e.w && e.cells[e.cy*e.w+last+1].con {
		e.cells[e.cy*e.w+last+1] = ecell{g: " ", s: e.cells[e.cy*e.w+last+1].s}
	}
	e.cells[i] = ecell{g: g, s: e.pen}
	if w == 2 && e.cx+1 < e.w {
		e.cells[i+1] = ecell{g: "", s: e.pen, con: true}
	}
	e.cx += w
}

// Feed interprets renderer output.
func (e *Emulator) Feed(s string) {
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[':
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				params := s[i+2 : j]
				switch s[j] {
				case 'H':
					ps := strings.Split(params, ";")
					y, _ := strconv.Atoi(ps[0])
					x := 1
					if len(ps) > 1 {
						x, _ = strconv.Atoi(ps[1])
					}
					e.cy, e.cx = max(0, y-1), max(0, x-1)
				case 'm':
					applySGR(&e.pen, params)
				case 'J':
					if params == "2" {
						e.clear()
					}
				}
				i = j + 1
				continue
			case 'P': // DCS: a sixel picture at the cursor
				end := strings.Index(s[i:], "\x1b\\")
				data := s[i : i+end+2]
				var pw, ph int
				fmt.Sscanf(data, "\x1bPq%d;%d", &pw, &ph)
				for y := e.cy; y < e.cy+ph && y < e.h; y++ {
					for x := e.cx; x < e.cx+pw && x < e.w; x++ {
						e.cells[y*e.w+x].pic = data
					}
				}
				i += end + 2
				continue
			case ']':
				end := strings.Index(s[i:], "\x1b\\")
				i += end + 2
				continue
			case '7':
				e.saved = [2]int{e.cx, e.cy}
				i += 2
				continue
			case '8':
				e.cx, e.cy = e.saved[0], e.saved[1]
				i += 2
				continue
			}
			i += 2
			continue
		}
		cl, _, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
		i += len(cl)
		w := e.WidthOf(cl)
		if w <= 0 {
			continue
		}
		e.put(cl, w)
		// text over a picture removes the picture from that cell
		if e.cy < e.h {
			for k := 0; k < w; k++ {
				if x := e.cx - w + k; x >= 0 && x < e.w {
					e.cells[e.cy*e.w+x].pic = ""
				}
			}
		}
	}
}

// Mismatch lists cells where the emulated screen differs from f (pictures
// must show where reserved, nowhere else). skip excuses cells.
func (e *Emulator) Mismatch(f *Frame, skip func(x, y int) bool) []string {
	var out []string
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			if skip != nil && skip(x, y) {
				continue
			}
			want := f.At(x, y)
			got := e.cells[y*f.W+x]
			switch {
			case want.Img > 0:
				if got.pic != f.Images[want.Img-1].Data {
					out = append(out, fmt.Sprintf("(%d,%d) should show picture %s", x, y, f.Images[want.Img-1].Key))
				}
			case got.pic != "":
				out = append(out, fmt.Sprintf("(%d,%d) stale picture left on screen", x, y))
			case want.W == 0:
				if !got.con {
					out = append(out, fmt.Sprintf("(%d,%d) expected wide-glyph tail", x, y))
				}
			case got.g != want.G || got.s != want.S:
				out = append(out, fmt.Sprintf("(%d,%d) got %q %+v want %q %+v", x, y, got.g, got.s, want.G, want.S))
			}
			if len(out) > 8 {
				return out
			}
		}
	}
	return out
}
