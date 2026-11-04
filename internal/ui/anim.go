package ui

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// ---- easing ----

func clamp01(t float64) float64 { return math.Max(0, math.Min(1, t)) }

func sin(x float64) float64 { return math.Sin(x) }

func easeOutCubic(t float64) float64 { t = clamp01(t); return 1 - math.Pow(1-t, 3) }

func easeInOutSine(t float64) float64 { t = clamp01(t); return -(math.Cos(math.Pi*t) - 1) / 2 }

// easeOutBack overshoots slightly then settles: a springy "landing".
func easeOutBack(t float64) float64 {
	t = clamp01(t)
	const c1, c3 = 1.70158, 2.70158
	return 1 + c3*math.Pow(t-1, 3) + c1*math.Pow(t-1, 2)
}

// easeOutBounce drops and bounces like a toy.
func easeOutBounce(t float64) float64 {
	t = clamp01(t)
	const n1, d1 = 7.5625, 2.75
	switch {
	case t < 1/d1:
		return n1 * t * t
	case t < 2/d1:
		t -= 1.5 / d1
		return n1*t*t + .75
	case t < 2.5/d1:
		t -= 2.25 / d1
		return n1*t*t + .9375
	}
	t -= 2.625 / d1
	return n1*t*t + .984375
}

// tween is a one-shot timeline.
type tween struct {
	at  time.Time
	dur time.Duration
}

func newTween(d time.Duration) tween { return tween{at: time.Now(), dur: d} }

func (t tween) p() float64 {
	if t.at.IsZero() || t.dur <= 0 {
		return 1
	}
	return clamp01(float64(time.Since(t.at)) / float64(t.dur))
}

func (t tween) active() bool { return !t.at.IsZero() && t.p() < 1 }

// reveal returns the 0..1 entrance progress of the i-th list row (staggered cascade).
func reveal(start time.Time, i int, motion bool) float64 {
	if !motion || start.IsZero() {
		return 1
	}
	return easeOutCubic(float64(time.Since(start)-time.Duration(i)*22*time.Millisecond) / float64(160*time.Millisecond))
}

func revealing(start time.Time, n int, motion bool) bool {
	return motion && !start.IsZero() && time.Since(start) < time.Duration(n)*22*time.Millisecond+200*time.Millisecond
}

// ---- raw ANSI canvas (fast path for per-cell animated art) ----

type cell struct {
	r    rune
	fg   rgb
	bold bool
}

type canvas struct {
	w, h  int
	cells []cell
}

// canvases live for one frame: the last one is kept for the next
// (single goroutine: only the UI draws).
var spareCanvas *canvas

func newCanvas(w, h int) *canvas {
	c := spareCanvas
	spareCanvas = nil
	if c == nil || cap(c.cells) < w*h {
		c = &canvas{cells: make([]cell, w*h)}
	}
	c.w, c.h, c.cells = w, h, c.cells[:w*h]
	for i := range c.cells {
		c.cells[i] = cell{r: ' '}
	}
	return c
}

func (c *canvas) set(x, y int, r rune, fg rgb, bold bool) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	c.cells[y*c.w+x] = cell{r: r, fg: fg, bold: bold}
}

func (c *canvas) empty(x, y int) bool {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return false
	}
	return c.cells[y*c.w+x].r == ' '
}

// text writes s starting at x (no wrapping).
func (c *canvas) text(x, y int, s string, fg rgb, bold bool) {
	for _, r := range s {
		c.set(x, y, r, fg, bold)
		x++
	}
}

func sgrFG(b *strings.Builder, c rgb) {
	b.WriteString("\x1b[38;2;")
	b.WriteString(strconv.Itoa(int(c.r)))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(int(c.g)))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(int(c.b)))
	b.WriteByte('m')
}

func (c *canvas) lines() []string {
	out := make([]string, c.h)
	var b strings.Builder
	for y := 0; y < c.h; y++ {
		b.Reset()
		var last rgb
		lastBold, styled := false, false
		for x := 0; x < c.w; x++ {
			ce := c.cells[y*c.w+x]
			if ce.r == ' ' {
				b.WriteByte(' ')
				continue
			}
			if ce.bold != lastBold {
				if ce.bold {
					b.WriteString("\x1b[1m")
				} else {
					b.WriteString("\x1b[22m")
				}
				lastBold = ce.bold
			}
			if !styled || ce.fg != last {
				sgrFG(&b, ce.fg)
				last, styled = ce.fg, true
			}
			b.WriteRune(ce.r)
		}
		if styled || lastBold {
			b.WriteString("\x1b[0m")
		}
		out[y] = b.String()
	}
	spareCanvas = c // its cells are free for the next frame
	return out
}

// ---- sakura petals: ambient particles drifting through empty cells ----

type petal struct {
	x, y, vx, vy, phase float64
	glyph               rune
	tint                float64
}

var petalGlyphs = []rune{'✿', '·', '•', '·', '✿', '∙', '·'}

func hash(n int) float64 {
	x := math.Sin(float64(n)*12.9898) * 43758.5453
	return x - math.Floor(x)
}

func spawn(n, w, h int, anywhere bool) petal {
	y := -1.0
	if anywhere {
		y = hash(n*7) * float64(h)
	}
	return petal{
		x:     hash(n*3)*float64(w+10) - 10,
		y:     y,
		vx:    1.2 + hash(n*5)*2.2,
		vy:    .7 + hash(n*11)*1.1,
		phase: hash(n*13) * 6,
		glyph: petalGlyphs[int(hash(n*17)*float64(len(petalGlyphs)))%len(petalGlyphs)],
		tint:  hash(n * 19),
	}
}
