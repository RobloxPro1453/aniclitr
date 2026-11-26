package tty

import (
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Terminal mode setup / teardown. Autowrap is off so a glyph-width
// disagreement can never wrap or scroll the screen. Mode 1000 (button/wheel
// clicks only, NOT 1003 all-motion) eliminates 100+ Hz mouse-move storms.
const (
	Enter = "\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[?1000h\x1b[?1006h\x1b[?2004h\x1b]2;aniclitr\x07\x1b[2J"
	Leave = "\x1b[?2026l\x1b[?2004l\x1b[?1006l\x1b[?1000l\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l"
)

const (
	syncOn  = "\x1b[?2026h"
	syncOff = "\x1b[?2026l"
)

// Diff renders the bytes that turn front into back. front == nil means the
// screen content is unknown: everything is drawn (clear first if clear).
// heal rewrites every text cell (used to repair silent drift) but keeps
// pictures that did not change.
func Diff(front, back *Frame, clear, heal bool) string {
	return diffBuf(front, back, clear, heal, nil)
}

func diffBuf(front, back *Frame, clearFrame, heal bool, scratch []bool) string {
	var b strings.Builder
	b.Grow(1024)
	b.WriteString(syncOn)
	if back.Bg != "" && (front == nil || front.Bg != back.Bg || clearFrame || heal) {
		if back.Bg == "reset" {
			b.WriteString("\x1b]111\x1b\\")
		} else {
			b.WriteString("\x1b]11;")
			b.WriteString(back.Bg)
			b.WriteString("\x1b\\")
		}
	}
	full := front == nil || front.W != back.W || front.H != back.H
	if full && clearFrame {
		b.WriteString("\x1b[0m\x1b[2J")
	}
	// which cells must be (re)written
	var dirty []bool
	if cap(scratch) >= len(back.Cells) {
		dirty = scratch[:len(back.Cells)]
		clear(dirty)
	} else {
		dirty = make([]bool, len(back.Cells))
	}
	if full || heal {
		for i := range dirty {
			dirty[i] = true
		}
	} else {
		for i := range back.Cells {
			if back.Cells[i] != front.Cells[i] && !(back.Cells[i].Img > 0 && front.Cells[i].Img > 0) {
				dirty[i] = true
			}
		}
		// a picture that went away (or moved) leaves stale pixels even where
		// the text underneath did not change: rewrite its whole rectangle
		for _, im := range front.Images {
			if hasSame(back.Images, im) {
				continue
			}
			for y := im.Row; y < im.Row+im.H && y < back.H; y++ {
				for x := im.Col; x < im.Col+im.W && x < back.W; x++ {
					dirty[y*back.W+x] = true
				}
			}
		}
	}
	// a glyph some terminals draw two cells wide spills into its right
	// neighbour: whenever one is written or replaced, rewrite the neighbour too
	if !full && !heal {
		for i := range back.Cells {
			if !dirty[i] || (i+1)%back.W == 0 {
				continue
			}
			if risky(back.Cells[i].G) || risky(front.Cells[i].G) {
				dirty[i+1] = true
			}
		}
	}
	// a picture is re-sent when it is new/moved, or when any of its cells was
	// showing something else before (another picture went away from on top)
	var resend []bool
	if len(back.Images) > 0 {
		resend = make([]bool, len(back.Images))
		for k, im := range back.Images {
			if full || clearFrame || front == nil || !hasSame(front.Images, im) {
				resend[k] = true
				continue
			}
			for y := im.Row; y < im.Row+im.H && !resend[k]; y++ {
				for x := im.Col; x < im.Col+im.W; x++ {
					fc := front.Cells[y*back.W+x]
					if fc.Img == 0 || !front.Images[fc.Img-1].same(im) || back.Cells[y*back.W+x].Img != int32(k+1) {
						resend[k] = true
						break
					}
				}
			}
		}
	}
	// the tail of a wide glyph can only be drawn by writing its lead
	for i := range back.Cells {
		if dirty[i] && back.Cells[i].W == 0 && i%back.W > 0 {
			dirty[i-1] = true
		}
	}
	// pictures are drawn in order; re-sending one would cover any later
	// picture overlapping it, so those must be re-sent too
	for k := range back.Images {
		if !resend[k] {
			continue
		}
		for j := k + 1; j < len(back.Images); j++ {
			if overlap(back.Images[k], back.Images[j]) {
				resend[j] = true
			}
		}
	}
	// pictures that stay keep their cells untouched
	if len(back.Images) > 0 {
		for i := range back.Cells {
			if id := back.Cells[i].Img; id > 0 && !resend[id-1] {
				dirty[i] = false
			}
		}
	}

	var pen Style
	hasPen := false
	for y := 0; y < back.H; y++ {
		row := back.Cells[y*back.W : (y+1)*back.W]
		d := dirty[y*back.W : (y+1)*back.W]
		x := 0
		for x < back.W {
			if !d[x] || row[x].Img > 0 {
				x++
				continue
			}
			start := x
			if row[start].W == 0 && start > 0 && row[start-1].Img == 0 {
				start--
			}
			end := x
			for end < back.W && row[end].Img == 0 {
				if d[end] {
					end++
					continue
				}
				gap := end
				for gap < back.W && gap-end < 4 && !d[gap] && row[gap].Img == 0 {
					gap++
				}
				if gap < back.W && gap-end < 4 && d[gap] && row[gap].Img == 0 {
					end = gap
					continue
				}
				break
			}
			cup(&b, y, start)
			for c := start; c < end; c++ {
				cell := row[c]
				if cell.W == 0 {
					continue
				}
				if !hasPen || pen != cell.S {
					pen = cell.S
					hasPen = true
					writeSGR(&b, pen)
				}
				g := cell.G
				if g == "" {
					g = " "
				}
				b.WriteString(g)
				if risky(g) && c+int(cell.W) < back.W {
					cup(&b, y, c+int(cell.W))
				}
			}
			x = end
		}
	}
	// pictures last (over the text), only when new, moved or after a clear
	for k, im := range back.Images {
		if !resend[k] {
			continue
		}
		b.WriteString("\x1b[0m")
		sp := strings.Repeat(" ", im.W)
		for y := im.Row; y < im.Row+im.H; y++ {
			cup(&b, y, im.Col)
			b.WriteString(sp)
		}
		cup(&b, im.Row, im.Col)
		b.WriteString(im.Data)
		hasPen = false
	}
	b.WriteString("\x1b[0m")
	b.WriteString(syncOff)
	return b.String()
}

func hasSame(list []Image, im Image) bool {
	for _, o := range list {
		if o.same(im) {
			return true
		}
	}
	return false
}

func cup(b *strings.Builder, y, x int) {
	var num [12]byte
	b.WriteString("\x1b[")
	b.Write(strconv.AppendInt(num[:0], int64(y+1), 10))
	b.WriteByte(';')
	b.Write(strconv.AppendInt(num[:0], int64(x+1), 10))
	b.WriteByte('H')
}

// Writer owns the terminal output. Submit never blocks; a goroutine renders
// the newest frame (older pending ones are dropped), at most ~60 per second.
type Writer struct {
	out io.Writer

	mu      sync.Mutex
	pending *Frame
	clear   bool // next render clears and repaints everything
	wake    chan struct{}
	done    chan struct{}
	closed  bool

	front    *Frame
	dirty    []bool // reusable scratch buffer (zero alloc per diff)
	lastDiff time.Time
	lastHeal time.Time
	dirtyRun bool // there was activity since the last heal

	// HealAfter: rewrite every text cell once this long after activity stops.
	HealAfter time.Duration
	// HealEvery: during continuous animation, heal at least this often.
	HealEvery time.Duration
	// Written counts bytes (diagnostics).
	Written int64
	// Slow, when set, hears about writes the terminal took long to accept
	// (it is behind: ConPTY back-pressure, slow image drawing).
	Slow func(d time.Duration, bytes int, images bool)
}

// NewWriter starts the render goroutine.
func NewWriter(out io.Writer) *Writer {
	w := &Writer{out: out, wake: make(chan struct{}, 1), done: make(chan struct{}), HealAfter: 1800 * time.Millisecond, HealEvery: 15 * time.Second, lastHeal: time.Now()}
	go w.loop()
	return w
}

// Submit hands the writer a new frame (the writer owns it afterwards).
func (w *Writer) Submit(f *Frame) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	if old := w.pending; old != nil && old != f {
		Recycle(old) // never shown: latest wins
	}
	w.pending = f
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Redraw clears the terminal and repaints everything on the next frame
// (resize, ctrl+l, after another program used the screen).
func (w *Writer) Redraw() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.clear = true
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Close stops the goroutine after flushing the last frame.
func (w *Writer) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.mu.Unlock()
	close(w.wake)
	<-w.done
}

const frameGap = time.Second / 60

func (w *Writer) loop() {
	defer close(w.done)
	heal := time.NewTimer(time.Hour)
	defer heal.Stop()
	for {
		select {
		case _, ok := <-w.wake:
			if !ok {
				w.render(false)
				return
			}
			if wait := frameGap - time.Since(w.lastDiff); wait > 0 {
				time.Sleep(wait) // pace; frames submitted meanwhile coalesce
			}
			if w.render(time.Since(w.lastHeal) > w.HealEvery) {
				heal.Reset(w.HealAfter)
			}
		case <-heal.C:
			if w.dirtyRun {
				w.render(true)
			}
		}
	}
}

// render diffs the newest frame; heal repaints all text cells of the front.
func (w *Writer) render(heal bool) bool {
	w.mu.Lock()
	f, clear := w.pending, w.clear
	w.pending, w.clear = nil, false
	w.mu.Unlock()
	if f == nil {
		if !heal && !clear {
			return false
		}
		f = w.front
		if f == nil {
			return false
		}
	}
	front := w.front
	if clear {
		front = nil
	}
	if cap(w.dirty) < len(f.Cells) {
		w.dirty = make([]bool, len(f.Cells))
	}
	s := diffBuf(front, f, clear, heal, w.dirty)
	t0 := time.Now()
	if _, err := io.WriteString(w.out, s); err == nil {
		w.Written += int64(len(s))
	}
	if d := time.Since(t0); w.Slow != nil && d > 50*time.Millisecond {
		w.Slow(d, len(s), strings.Contains(s, "\x1bP"))
	}
	if w.front != nil && w.front != f {
		Recycle(w.front) // what the terminal showed before; nobody reads it now
	}
	w.front = f
	w.lastDiff = time.Now()
	w.dirtyRun = !heal
	if heal {
		w.lastHeal = time.Now()
	}
	return true
}

func overlap(a, b Image) bool {
	return a.Col < b.Col+b.W && b.Col < a.Col+a.W && a.Row < b.Row+b.H && b.Row < a.Row+a.H
}
