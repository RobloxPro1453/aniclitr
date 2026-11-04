package ui

import (
	"io"
	"time"

	"aniclitr/internal/logx"
	"aniclitr/internal/tty"
)

// Self-paint: the app composes its frame as styled lines, turns them into
// a cell grid (tty.Frame) with pictures as a separate layer, and hands the
// grid to tty.Writer, which diffs it against what the terminal shows on its
// own goroutine (latest frame wins). See internal/tty for the guarantees.

type painter struct {
	tw    *tty.Writer // async (the real terminal)
	sync  io.Writer   // tests: render inline, deterministic
	front *tty.Frame  // sync mode only
}

// SelfPaint switches the app to drawing itself on w (async writer).
func (a *App) SelfPaint(w io.Writer) {
	a.out = w
	a.pt = &painter{tw: tty.NewWriter(w)}
}

// selfPaintSync renders every frame inline (tests).
func (a *App) selfPaintSync(w io.Writer) {
	a.out = w
	a.pt = &painter{sync: w}
}

// Close flushes the last frame and stops the writer.
func (a *App) Close() {
	if a.pt != nil && a.pt.tw != nil {
		a.pt.tw.Close()
	}
}

// redraw clears and repaints everything (resize, ctrl+l).
func (a *App) redraw() {
	if a.pt == nil {
		return
	}
	if a.pt.tw != nil {
		a.pt.tw.Redraw()
	} else {
		a.pt.front = nil
	}
}

func (a *App) resizeScreen() { a.redraw() }

// buildFrame builds the cell grid for the current state.
func (a *App) buildFrame() *tty.Frame {
	lines, suppress := a.compose()
	if lines == nil {
		return nil
	}
	f := tty.GetFrame(a.w, a.h)
	places := a.gfx.extract(lines)
	f.SetLines(lines)
	if a.cfg.ThemeBg {
		f.Bg = cBg
	} else {
		f.Bg = "reset"
	}
	if !suppress && a.gfx.on {
		for _, p := range places {
			if data := a.gfx.encode(p.placeReq); data != "" {
				f.Place(tty.Image{Key: encKey(p.url, p.w, p.h), Row: p.row, Col: p.col, W: p.w, H: p.h, Data: data})
			}
		}
	}
	a.gfx.reqs = a.gfx.reqs[:0]
	return f
}

// paint renders the current state (called after every update).
func (a *App) paint() {
	defer func() {
		if r := recover(); r != nil {
			logx.Panic("paint", r)
		}
	}()
	if a.pt == nil || a.w <= 0 || a.h <= 0 {
		return
	}
	t0 := time.Now()
	f := a.buildFrame()
	if f == nil {
		return
	}
	a.paintCost = time.Since(t0)
	if a.pt.tw != nil {
		a.pt.tw.Submit(f)
		return
	}
	s := tty.Diff(a.pt.front, f, a.pt.front == nil, false)
	_, _ = io.WriteString(a.pt.sync, s)
	tty.Recycle(a.pt.front)
	a.pt.front = f
}

// painterBusy: nothing to wait for — the writer owns pacing.
func (a *App) painterBusy() bool { return false }
