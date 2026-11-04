package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// writeCovers makes n TMDB-sized (500×750) noisy JPEG covers on disk and
// returns their file:// URLs, which fetchPoster reads like the disk cache.
func writeCovers(t *testing.T, n int) []string {
	t.Helper()
	dir := t.TempDir()
	r := rand.New(rand.NewSource(1))
	var urls []string
	for i := 0; i < n; i++ {
		img := image.NewRGBA(image.Rect(0, 0, 500, 750))
		c := color.RGBA{uint8(r.Intn(255)), uint8(r.Intn(255)), uint8(r.Intn(255)), 255}
		for y := 0; y < 750; y++ {
			for x := 0; x < 500; x++ {
				o := img.PixOffset(x, y)
				n := uint8(r.Intn(40))
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c.R/2+uint8(y/6)+n, c.G/2+uint8(x/5)+n, c.B/2+n, 255
			}
		}
		p := filepath.Join(dir, fmt.Sprintf("p%03d.jpg", i))
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		_ = jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
		f.Close()
		urls = append(urls, "file://"+p)
	}
	return urls
}

func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A cover on disk costs only its small master in RAM: no full-size pixels,
// no copy of the file. (Was 1.47 MB per cover.)
func TestPosterMemoryBudget(t *testing.T) {
	a := testApp(t, Graphics{Sixel: true, CellW: 10, CellH: 20})
	urls := writeCovers(t, 40)
	h0 := liveHeap()
	for _, u := range urls {
		a.posters.set(fetchPoster(u))
	}
	per := float64(liveHeap()-h0) / float64(len(urls))
	t.Logf("retained per cover: %.1f KB", per/1024)
	if per > 80<<10 {
		t.Fatalf("a cover should cost < 80 KB of RAM, costs %.0f KB", per/1024)
	}
	for _, u := range urls {
		st := a.posters.items[u]
		if st.img == nil || st.raw != nil || st.img.Bounds().Dx() > smallW {
			t.Fatalf("cover on disk: small master only (raw=%d bytes, w=%d)", len(st.raw), st.img.Bounds().Dx())
		}
	}
	// the sixel source still comes back at the usual size, from disk
	if full := fullImage(urls[0], nil); full == nil || full.Bounds().Dx() != fullW {
		t.Fatal("sixel source should be read back from disk at 480px")
	}
}

// Over the cap, covers not drawn lately are dropped, and one drawn again
// is fetched back by itself (no screen has to ask).
func TestPosterTrimReloads(t *testing.T) {
	a := testApp(t, Graphics{})
	img := image.NewRGBA(image.Rect(0, 0, 50, 75))
	old := time.Now().Add(-time.Minute)
	for i := 0; i < maxPosters+40; i++ {
		u := fmt.Sprintf("test://c%d", i)
		a.posters.items[u] = &posterState{img: img, used: old.Add(time.Duration(i) * time.Millisecond)}
		a.posters.scaled[u+"|10|10"] = image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	a.posters.set(posterMsg{url: "test://new", img: img})
	if len(a.posters.items) != maxPosters {
		t.Fatalf("trim to %d, have %d", maxPosters, len(a.posters.items))
	}
	if _, ok := a.posters.items["test://c0"]; ok || !a.posters.evicted["test://c0"] {
		t.Fatal("the least recently drawn cover goes first")
	}
	if _, ok := a.posters.scaled["test://c0|10|10"]; ok {
		t.Fatal("its sized copies go with it")
	}
	if _, ok := a.posters.items["test://new"]; !ok {
		t.Fatal("a fresh cover is never trimmed")
	}
	a.posters.render("test://c0", 10, 8, 0)
	if st, ok := a.posters.items["test://c0"]; !ok || !st.loading || len(a.posters.reload) != 1 {
		t.Fatal("drawing a trimmed cover queues it back")
	}
	if !a.needsTick() || len(a.posters.takeReloads()) != 1 || len(a.posters.reload) != 0 {
		t.Fatal("the reload goes out on the next update")
	}
}

// The heavy session from OPTIMIZATION.md: 150 covers, sixel on, the library
// wall scrolled end to end. ANICLITR_MEM=1 (slow: encodes 150 sixels).
func TestMemHeavySession(t *testing.T) {
	if os.Getenv("ANICLITR_MEM") == "" {
		t.Skip("set ANICLITR_MEM=1")
	}
	mb := func(b uint64) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }
	urls := writeCovers(t, 150)
	debug.FreeOSMemory() // drop the test's own cover-making garbage
	TuneGC()             // as the app runs
	defer debug.SetGCPercent(100)
	defer debug.SetMemoryLimit(math.MaxInt64)
	a := testApp(t, Graphics{Sixel: true, CellW: 10, CellH: 20})
	a.Update(tea.WindowSizeMsg{Width: 200, Height: 56})
	h0 := liveHeap()
	t0 := time.Now()
	for _, u := range urls {
		a.posters.set(fetchPoster(u))
	}
	h1 := liveHeap()
	t.Logf("B 150 covers: heap %s (+%s, %.1f KB/cover) in %v", mb(h1), mb(h1-h0), float64(h1-h0)/150/1024, time.Since(t0).Round(time.Millisecond))
	for i, u := range urls {
		an := source.Anime{Source: "animecix", ID: fmt.Sprint(1000 + i), Title: fmt.Sprintf("Anime %d", i), Poster: u, Kind: "TV", MalID: 90000 + i}
		a.lib.Set(an, func(it *store.Item) { it.Status = store.Completed })
	}
	l := newLibrary()
	l.grid, l.focus = true, 1
	a.stack = append(a.stack, l)
	wait := func() {
		for k := 0; k < 500; k++ {
			_ = a.View()
			if !a.gfx.busy() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	for i := 0; i < 150; i += 6 {
		l.cur = i
		wait()
	}
	h2 := liveHeap()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	enc := 0
	a.gfx.mu.Lock()
	for _, s := range a.gfx.enc {
		enc += len(s)
	}
	nEnc := len(a.gfx.enc)
	a.gfx.mu.Unlock()
	t.Logf("C wall scrolled: heap %s · covers %d · sixels %d (%s)", mb(h2), len(a.posters.items), nEnc, mb(uint64(enc)))
	// what the OS would see after the app hands memory back (trimMemory)
	debug.FreeOSMemory()
	runtime.ReadMemStats(&m)
	t.Logf("D resident estimate after trim: %s (heap in use %s, stacks %s, runtime %s)",
		mb(m.Sys-m.HeapReleased), mb(m.HeapInuse), mb(m.StackSys), mb(m.Sys-m.HeapSys-m.StackSys))
	runtime.KeepAlive(a) // the app (and its caches) must be alive for the numbers above
	if h2 > 40<<20 {
		t.Fatalf("heavy session should stay under 40 MB live heap, got %s", mb(h2))
	}
}
