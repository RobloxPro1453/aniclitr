package tty

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// fakePic is a recognisable "sixel": DCS q<w>;<h> ... ST (cells, for the emulator).
func fakePic(key string, w, h int) string {
	return fmt.Sprintf("\x1bPq%d;%d#%s\x1b\\", w, h, key)
}

// check compares the emulated screen with the intended frame.
func check(t *testing.T, step int, e *Emulator, f *Frame, skip func(x, y int) bool) {
	t.Helper()
	for _, m := range e.Mismatch(f, skip) {
		t.Errorf("step %d: %s", step, m)
	}
	if t.Failed() {
		t.FailNow()
	}
}

var glyphs = []string{"a", "b", "ş", "Ğ", "İ", "ü", " ", " ", "─", "│", "╭", "█", "▀", "●", "◆", "♥", "★", "✓", "", "漢", "ｱ", "é"}

func randLine(r *rand.Rand, w int) string {
	var b strings.Builder
	n := r.Intn(w + 1)
	for i := 0; i < n; i++ {
		if r.Intn(5) == 0 {
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", r.Intn(256), r.Intn(256), r.Intn(256))
		}
		if r.Intn(9) == 0 {
			fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm", r.Intn(4)*60, 10, 20)
		}
		if r.Intn(11) == 0 {
			b.WriteString("\x1b[1m")
		}
		if r.Intn(13) == 0 {
			b.WriteString("\x1b[0m")
		}
		b.WriteString(glyphs[r.Intn(len(glyphs))])
	}
	return b.String()
}

func randFrame(r *rand.Rand, w, h int, prev *Frame) *Frame {
	f := NewFrame(w, h)
	lines := make([]string, h)
	for y := range lines {
		if prev != nil && r.Intn(3) > 0 {
			lines[y] = "" // keep most rows similar: reuse below
		} else {
			lines[y] = randLine(r, w)
		}
	}
	if prev != nil {
		// copy most of the previous frame so diffs stay small and realistic
		copy(f.Cells, prev.Cells)
		for i := range f.Cells {
			f.Cells[i].Img = 0
		}
		for y, l := range lines {
			if l != "" {
				for x := 0; x < w; x++ {
					f.Cells[y*w+x] = blank
				}
				f.setLine(y, l)
			}
		}
	} else {
		f.SetLines(lines)
	}
	// 0-2 pictures, often in the same spot as before
	for k := 0; k < r.Intn(3); k++ {
		iw, ih := 3+r.Intn(8), 2+r.Intn(5)
		row, col := r.Intn(max(1, h-ih-1)), r.Intn(max(1, w-iw))
		key := fmt.Sprintf("p%d", r.Intn(4))
		if prev != nil && k < len(prev.Images) && r.Intn(2) == 0 {
			im := prev.Images[k]
			row, col, iw, ih, key = im.Row, im.Col, im.W, im.H, im.Key
		}
		f.Place(Image{Key: key, Row: row, Col: col, W: iw, H: ih, Data: fakePic(key+fmt.Sprint(iw, ih), iw, ih)})
	}
	return f
}

// Fuzz: hundreds of random frames (styles, wide glyphs, icons, pictures
// coming and going); after every diff the emulated terminal must equal the
// intended frame exactly. Heals and clears are mixed in.
func TestDiffFuzz(t *testing.T) {
	for seed := int64(1); seed <= int64(fuzzSeeds()); seed++ {
		r := rand.New(rand.NewSource(seed))
		w, h := 30+r.Intn(50), 8+r.Intn(16)
		e := NewEmulator(w, h)
		var front *Frame
		for step := 0; step < 250; step++ {
			back := randFrame(r, w, h, front)
			clear := front == nil || r.Intn(40) == 0
			heal := !clear && r.Intn(25) == 0
			e.Feed(Diff(front, back, clear, heal))
			check(t, step, e, back, nil)
			front = back
		}
	}
}

// A terminal that draws some glyphs two cells wide while we think they are
// one: thanks to re-anchoring, only the cell right after such a glyph may
// differ — the rest of the row stays correct (no drift).
func TestDiffSurvivesWidthDisagreement(t *testing.T) {
	for seed := int64(1); seed <= int64(fuzzSeeds()); seed++ {
		disagreement(t, seed)
	}
}

func disagreement(t *testing.T, seed int64) {
	r := rand.New(rand.NewSource(seed))
	w, h := 30+r.Intn(50), 8+r.Intn(16)
	e := NewEmulator(w, h)
	disputed := map[string]bool{"♥": true, "★": true, "": true}
	e.WidthOf = func(g string) int {
		if disputed[g] {
			return 2
		}
		return ansi.StringWidth(g)
	}
	var front *Frame
	for step := 0; step < 200; step++ {
		back := randFrame(r, w, h, front)
		e.Feed(Diff(front, back, front == nil, false))
		check(t, step, e, back, func(x, y int) bool {
			// only the disputed glyph itself may look wrong (the terminal
			// may have erased it when its spill-over cell was repaired)
			return disputed[back.At(x, y).G]
		})
		front = back
	}
}

// Static frames cost nothing; moving a list next to a picture never re-sends it.
func TestDiffQuietAndPictureStable(t *testing.T) {
	mk := func(sel int) *Frame {
		f := NewFrame(40, 10)
		var lines []string
		for i := 0; i < 10; i++ {
			l := fmt.Sprintf("satır %d", i)
			if i == sel {
				l = "\x1b[48;2;30;30;40m▌ " + l + "\x1b[0m"
			}
			lines = append(lines, l)
		}
		f.SetLines(lines)
		f.Place(Image{Key: "poster", Row: 1, Col: 25, W: 10, H: 6, Data: fakePic("poster", 10, 6)})
		return f
	}
	a := mk(1)
	first := Diff(nil, a, true, false)
	if !strings.Contains(first, "\x1bPq") {
		t.Fatal("picture not drawn on first frame")
	}
	if s := Diff(a, mk(1), false, false); strings.Contains(s, "H") {
		t.Fatalf("identical frame wrote %q", s)
	}
	if s := Diff(a, mk(4), false, false); strings.Contains(s, "\x1bPq") {
		t.Fatal("cursor move beside the picture re-sent it")
	}
	if s := Diff(a, mk(1), false, true); strings.Contains(s, "\x1bPq") {
		t.Fatal("heal should not re-send unchanged pictures")
	}
}

// The writer coalesces bursts: many submits, few renders, latest wins.
func TestWriterMailbox(t *testing.T) {
	var out strings.Builder
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	w := NewWriter(writerFunc(func(p []byte) (int, error) {
		<-mu
		out.Write(p)
		mu <- struct{}{}
		return len(p), nil
	}))
	w.HealAfter, w.HealEvery = time.Hour, time.Hour
	var last *Frame
	for i := 0; i < 200; i++ {
		f := NewFrame(20, 3)
		f.SetLines([]string{fmt.Sprintf("kare %03d", i)})
		w.Submit(f)
		last = f
	}
	time.Sleep(100 * time.Millisecond)
	w.Close()
	<-mu
	s := out.String()
	if n := strings.Count(s, syncOn); n > 20 {
		t.Fatalf("%d renders for 200 submits: no coalescing", n)
	}
	e := NewEmulator(20, 3)
	e.Feed(s)
	check(t, 0, e, last, nil)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// fuzzSeeds: ANICLITR_FUZZ=n runs a longer fuzz (default 6 seeds).
func fuzzSeeds() int {
	if n, err := strconv.Atoi(os.Getenv("ANICLITR_FUZZ")); err == nil && n > 0 {
		return n
	}
	return 6
}

// The async writer heals: after a burst of frames, one full text rewrite
// follows (repairs drift a diff alone could never see).
func TestWriterHeals(t *testing.T) {
	var mu sync.Mutex
	var out strings.Builder
	w := NewWriter(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		out.Write(p)
		mu.Unlock()
		return len(p), nil
	}))
	w.HealAfter = 50 * time.Millisecond
	f := NewFrame(20, 3)
	f.SetLines([]string{"merhaba"})
	w.Submit(f)
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	n1 := strings.Count(out.String(), syncOn)
	mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	w.Close()
	mu.Lock()
	defer mu.Unlock()
	if n := strings.Count(out.String(), syncOn); n != n1+1 {
		t.Fatalf("expected exactly one heal pass after quiet: %d → %d", n1, n)
	}
	// the heal rewrote the whole screen (every row addressed)
	if !strings.Contains(out.String()[strings.LastIndex(out.String(), syncOn):], "\x1b[3;1H") {
		t.Fatal("heal did not rewrite all rows")
	}
}
