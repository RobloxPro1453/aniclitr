package ui

import (
	"bytes"
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-sixel"
	"golang.org/x/image/draw"

	"aniclitr/internal/logx"
)

// Graphics describes the terminal's pixel capabilities, detected at startup.
type Graphics struct {
	Sixel        bool // terminal answered DA1 with sixel support
	CellW, CellH int  // pixel size of one cell (Windows Terminal: 10×20 virtual)
}

// gfx draws full-resolution posters with Sixel on top of the text frame.
//
// How it fits Bubble Tea's line renderer:
//  1. screens render a blank w×h placeholder whose first line carries an
//     invisible APC marker (zero width, ignored by terminals);
//  2. after the frame is composed, markers are located (row, column) and
//     removed;
//  3. if any row under an image changed (the renderer will repaint — and
//     thereby erase — those rows) or the placement set changed, the images
//     are re-sent: save cursor → move → sixel → restore, appended to the
//     last line. A toggling no-op keeps that line "changed" so it is flushed.
//
// Static screens therefore cost nothing; images are only re-sent when text
// under them was actually redrawn.
type gfx struct {
	on       bool
	cw, ch   int
	reqs     []placeReq
	mu       sync.Mutex
	enc      map[string]string    // encoded sixel per url|w|h (written by workers)
	encAt    map[string]time.Time // last drawn (trimEnc keeps fresh and on-screen ones)
	pending  map[string]bool
	dirty    atomic.Bool // a worker finished: re-send images
	prev     []string
	prevSig  string
	lastEmit time.Time
	burst    int
	nonce    bool
	posters  *posterCache
	shown    []placement // what is on screen now
	stale    []staleRect // areas whose image went away and must be repainted
}

// staleRect: a sixel stays on screen until the cells under it are
// rewritten, and Bubble Tea only rewrites lines whose text changed. When an
// image disappears (shelf emptied, cursor on nothing…) the blank lines under
// it look unchanged, so the old picture would linger. For a short while we
// make those lines differ (an alternating no-op SGR) so they get repainted.
type staleRect struct {
	placement
	until time.Time
}

type placeReq struct {
	url  string
	w, h int
}

type placement struct {
	placeReq
	row, col int
}

func newGfx(g Graphics, on bool) *gfx {
	cw, ch := g.CellW, g.CellH
	if cw <= 0 || ch <= 0 {
		cw, ch = 10, 20
	}
	return &gfx{on: on, cw: cw, ch: ch, enc: map[string]string{}, pending: map[string]bool{}}
}

func encKey(url string, w, h int) string { return fmt.Sprintf("%s|%d|%d", url, w, h) }

// ready reports whether the sixel for url at w×h is encoded.
func (g *gfx) ready(url string, w, h int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.enc[encKey(url, w, h)]
	return ok
}

// request starts a background encode (encoding takes ~50-80ms; the UI shows
// the half-block poster meanwhile, then sharpens to sixel).
func (g *gfx) request(url string, w, h int, load func() image.Image) {
	key := encKey(url, w, h)
	g.mu.Lock()
	if _, ok := g.enc[key]; ok || g.pending[key] || load == nil {
		g.mu.Unlock()
		return
	}
	g.pending[key] = true
	g.mu.Unlock()
	cw, ch, bg := g.cw, g.ch, cBg // read the palette here: workers must not touch globals
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logx.Panic("sixel encode", r)
			}
		}()
		s := ""
		if src := load(); src != nil { // the full cover, read back from disk
			s = encodeSixel(src, w*cw, h*ch, cw, bg)
		}
		g.mu.Lock()
		g.enc[key] = s // "" on failure: we fall back to blocks for this one
		if g.encAt == nil {
			g.encAt = map[string]time.Time{}
		}
		g.encAt[key] = time.Now()
		g.trimEncLocked()
		g.dirty.Store(true) // before clearing pending: observers of !busy() see it
		delete(g.pending, key)
		g.mu.Unlock()
	}()
}

// busy reports whether encodes are in flight or a send is owed (the UI keeps ticking).
func (g *gfx) busy() bool {
	if g.on && g.burst > 0 && len(g.prevSig) > 0 {
		return true
	}
	if len(g.stale) > 0 {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.pending) > 0
}

// retheme drops encoded images (their rounded corners carry the old ground).
func (g *gfx) retheme() {
	g.mu.Lock()
	for k := range g.enc {
		delete(g.enc, k)
	}
	g.mu.Unlock()
	g.invalidate()
}

func (g *gfx) invalidate() {
	if g != nil {
		g.burst = 3
		g.prev = nil
	}
}

func marker(id int) string { return "\x1b_kz" + strconv.Itoa(id) + "\x1b\\" }

// place reserves a w×h cell area for url and returns its placeholder lines.
func (g *gfx) place(url string, w, h int) []string {
	id := len(g.reqs)
	g.reqs = append(g.reqs, placeReq{url: url, w: w, h: h})
	// the reserved area is painted in the ground colour (not plain blanks):
	// the cell renderer never "erases to end of line" across styled cells,
	// so it cannot wipe the picture while redrawing text beside it
	blank := fill(w, cBg)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = blank
	}
	lines[0] = marker(id) + blank
	return lines
}

// finish locates markers in the composed frame, strips them and, when
// needed, appends the sixel draw commands to the last line.
func (g *gfx) finish(lines []string, suppress bool) []string {
	defer func() { g.reqs = g.reqs[:0] }()
	var places []placement
	for i, l := range lines {
		for {
			k := strings.Index(l, "\x1b_kz")
			if k < 0 {
				break
			}
			end := strings.Index(l[k:], "\x1b\\")
			if end < 0 {
				break
			}
			id, err := strconv.Atoi(l[k+4 : k+end])
			col := ansi.StringWidth(l[:k])
			l = l[:k] + l[k+end+2:]
			if err == nil && id < len(g.reqs) && !suppress {
				places = append(places, placement{placeReq: g.reqs[id], row: i, col: col})
			}
		}
		lines[i] = l
	}
	g.markStale(places, suppress)
	g.paintStale(lines)
	if suppress {
		g.burst = 3 // redraw once the overlay/transition is gone
		g.prev = nil
		return lines
	}
	var sig strings.Builder
	for _, p := range places {
		fmt.Fprintf(&sig, "%s|%d|%d|%d|%d;", p.url, p.w, p.h, p.row, p.col)
	}
	encoded := g.dirty.Swap(false) // always consume (no short-circuit), or it leaks into the next frame
	changed := encoded || sig.String() != g.prevSig || g.prev == nil || len(g.prev) != len(lines)
	if !changed {
		for _, p := range places {
			for r := p.row; r < p.row+p.h && r < len(lines); r++ {
				if lines[r] != g.prev[r] {
					changed = true
				}
			}
		}
	}
	if changed {
		// several frames in a row: Bubble Tea may coalesce views between flushes
		g.burst = 3
	}
	g.prev = append(g.prev[:0], lines...)
	g.prevSig = sig.String()
	if g.burst == 0 || len(places) == 0 || len(lines) == 0 {
		return lines
	}
	// Safety valve: never stream more than ~10 images/s even if something
	// animates next to a poster; the owed send happens on the next tick.
	if time.Since(g.lastEmit) < 100*time.Millisecond {
		return lines
	}
	g.lastEmit = time.Now()
	g.burst--
	var b strings.Builder
	for _, p := range places {
		data := g.encode(p.placeReq)
		if data == "" {
			continue
		}
		fmt.Fprintf(&b, "\x1b7\x1b[%d;%dH%s\x1b8", p.row+1, p.col+1, data)
	}
	g.nonce = !g.nonce
	if g.nonce {
		b.WriteString("\x1b[0m")
	}
	lines[len(lines)-1] += b.String()
	return lines
}

// encode returns the cached sixel for r ("" if not ready).
func (g *gfx) encode(r placeReq) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := encKey(r.url, r.w, r.h)
	if g.encAt != nil {
		if _, ok := g.enc[k]; ok {
			g.encAt[k] = time.Now()
		}
	}
	return g.enc[k]
}

// encBudget caps encoded sixels (~25 KB–200 KB each). Over it, the ones not
// drawn for a while go first; anything on screen is always kept, and a
// dropped one is simply encoded again when it comes back.
const encBudget = 24 << 20

func (g *gfx) trimEncLocked() {
	total := 0
	for _, s := range g.enc {
		total += len(s)
	}
	if total <= encBudget {
		return
	}
	keys := make([]string, 0, len(g.enc))
	for k := range g.enc {
		if time.Since(g.encAt[k]) > keepFresh {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return g.encAt[keys[i]].Before(g.encAt[keys[j]]) })
	for _, k := range keys {
		if total <= encBudget {
			break
		}
		total -= len(g.enc[k])
		delete(g.enc, k)
		delete(g.encAt, k)
	}
}

// encodeSixel scales src to exactly pw×ph pixels (cover-cropped, rounded
// corners) and encodes it. Pure function: safe on any goroutine.
func encodeSixel(src image.Image, pw, ph, cw int, bg string) string {
	dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, coverCrop(src.Bounds(), pw, ph), draw.Src, nil)
	roundCorners(dst, 3*cw/10+2, hex(bg))
	var buf bytes.Buffer
	e := sixel.NewEncoder(&buf)
	e.Dither = true
	e.Colors = 200
	if err := e.Encode(dst); err != nil {
		logx.Errorf("sixel encode: %v", err)
		return ""
	}
	return strings.NewReplacer("\n", "", "\r", "").Replace(buf.String())
}

// coverCrop picks the centred source rectangle with the target aspect.
func coverCrop(sb image.Rectangle, w, h int) image.Rectangle {
	ta := float64(w) / float64(h)
	sa := float64(sb.Dx()) / float64(sb.Dy())
	switch {
	case sa > ta:
		nw := int(float64(sb.Dy()) * ta)
		x0 := sb.Min.X + (sb.Dx()-nw)/2
		return image.Rect(x0, sb.Min.Y, x0+nw, sb.Max.Y)
	case sa < ta:
		nh := int(float64(sb.Dx()) / ta)
		y0 := sb.Min.Y + (sb.Dy()-nh)/2
		return image.Rect(sb.Min.X, y0, sb.Max.X, y0+nh)
	}
	return sb
}

// roundCorners paints the corner pixels in the terminal ground colour so
// covers get soft rounded edges.
func roundCorners(img *image.RGBA, r int, bg rgb) {
	b := img.Bounds()
	for y := 0; y < r; y++ {
		for x := 0; x < r; x++ {
			dx, dy := float64(r-x)-.5, float64(r-y)-.5
			if dx*dx+dy*dy <= float64(r*r) {
				continue
			}
			for _, pt := range [][2]int{{b.Min.X + x, b.Min.Y + y}, {b.Max.X - 1 - x, b.Min.Y + y}, {b.Min.X + x, b.Max.Y - 1 - y}, {b.Max.X - 1 - x, b.Max.Y - 1 - y}} {
				i := img.PixOffset(pt[0], pt[1])
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(bg.r), uint8(bg.g), uint8(bg.b), 255
			}
		}
	}
}

// markStale remembers the areas of images that are no longer placed.
func (g *gfx) markStale(now []placement, suppress bool) {
	cur := map[placement]bool{}
	if !suppress {
		for _, p := range now {
			cur[p] = true
		}
	}
	for _, p := range g.shown {
		if !cur[p] {
			g.stale = append(g.stale, staleRect{placement: p, until: time.Now().Add(180 * time.Millisecond)})
		}
	}
	if suppress {
		g.shown = nil
	} else {
		g.shown = append(g.shown[:0], now...)
	}
}

// paintStale forces a repaint of rows under vanished images.
func (g *gfx) paintStale(lines []string) {
	if len(g.stale) == 0 {
		return
	}
	keep := g.stale[:0]
	rows := map[int]bool{}
	for _, s := range g.stale {
		if time.Now().After(s.until) {
			continue
		}
		keep = append(keep, s)
		for r := s.row; r < s.row+s.h && r < len(lines); r++ {
			rows[r] = true
		}
	}
	g.stale = keep
	g.nonce = !g.nonce
	for r := range rows {
		if g.nonce {
			lines[r] += "\x1b[0m"
		} else {
			lines[r] += "\x1b[0m\x1b[0m"
		}
	}
}

// extract strips picture markers from a composed frame and returns where
// each picture sits (self-paint path; View uses finish).
func (g *gfx) extract(lines []string) []placement {
	var places []placement
	for i, l := range lines {
		for {
			k := strings.Index(l, "\x1b_kz")
			if k < 0 {
				break
			}
			end := strings.Index(l[k:], "\x1b\\")
			if end < 0 {
				break
			}
			id, err := strconv.Atoi(l[k+4 : k+end])
			col := ansi.StringWidth(l[:k])
			l = l[:k] + l[k+end+2:]
			if err == nil && id < len(g.reqs) {
				places = append(places, placement{placeReq: g.reqs[id], row: i, col: col})
			}
		}
		lines[i] = l
	}
	return places
}
