package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-sixel"
	"github.com/muesli/termenv"

	"aniclitr/internal/config"
	"aniclitr/internal/player"
	"aniclitr/internal/source"
)

// TestMain isolates every test in this package from the user's real
// %APPDATA%\aniclitr (history, config, logs).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aniclitr-test-*")
	if err == nil {
		os.Setenv("ANICLITR_HOME", dir)
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

func testApp(t *testing.T, g Graphics) *App {
	t.Helper()
	t.Setenv("ANICLITR_HOME", t.TempDir()) // never touch the user's history/config
	lipgloss.SetColorProfile(termenv.TrueColor)
	cfg := config.Load()
	cfg.Motion = false
	cfg.Sounds = 0                // tests must stay silent
	cfg.DownloadDir = t.TempDir() // never the user's Videos folder
	cfg.Posters = true
	cfg.Graphics = "auto"
	cfg.Sources = map[string]bool{"animecix": true}
	return New(cfg, g)
}

// The search box must never pass control bytes on: a NUL typed via
// ctrl+space produced "…/search/%00bleach" and HTTP 400.
func TestInputDropsControlRunes(t *testing.T) {
	var in input
	in.handle(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}})
	in.handle(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ble\u200bach\x00")})
	if got := in.Value(); got != "bleach" {
		t.Fatalf("got %q", got)
	}
	if got := source.Clean("\x00 one\tpiece  "); got != "one piece" {
		t.Fatalf("source.Clean got %q", got)
	}
	if got := CleanQuery("  frieren\n2 "); got != "frieren 2" {
		t.Fatalf("CleanQuery got %q", got)
	}
}

// Closing mpv (EvEnded) used to schedule a wait on a nil session → panic.
func TestPlayerEndDoesNotPanic(t *testing.T) {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	st := &source.Stream{Qualities: []source.Quality{{Label: "720p", URL: "x"}}}
	p := newPlaying(source.Anime{Source: "animecix", ID: "1", Title: "T", Kind: "TV"}, source.Episode{Season: 1, Number: 1}, nil, "F", "Tau", st, st.Qualities[0])
	a.stack = append(a.stack, p)
	ch := make(chan player.Event, 1)
	p.sess = &player.Session{Events: ch}
	p.started, p.pos, p.dur = true, 100, 1400
	close(ch)
	_, cmd := a.Update(playerMsg{id: p.id, ok: true, ev: player.Event{Kind: player.EvEnded, Pos: 100, Dur: 1400}})
	runAll(t, cmd)
	if a.top() == screen(p) {
		t.Fatal("player screen should close after mpv quits")
	}
}

// runAll executes a command tree synchronously (ignores ticks).
func runAll(t *testing.T, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case m := <-done:
		if b, ok := m.(tea.BatchMsg); ok {
			for _, c := range b {
				runAll(t, c)
			}
		}
	case <-time.After(2 * time.Second):
	}
}

// A panic inside any background command must become a toast, not a crash.
func TestGuardRecovers(t *testing.T) {
	msg := guard("test", func() tea.Msg { panic("boom") })()
	if _, ok := msg.(toastMsg); !ok {
		t.Fatalf("want toastMsg, got %T", msg)
	}
}

var reSixelPos = regexp.MustCompile(`\x1b7\x1b\[(\d+);(\d+)H(\x1bP[^\x1b]*\x1b\\)\x1b8`)

// The sixel layer: markers vanish, images are placed at the exact cell,
// sized cells×(10×20) px, re-sent only when rows under them change.
func TestSixelPlacement(t *testing.T) {
	a := testApp(t, Graphics{Sixel: true, CellW: 10, CellH: 20})
	if !a.gfx.on {
		t.Fatal("sixel should be on")
	}
	a.Update(tea.WindowSizeMsg{Width: 150, Height: 44})
	// synthetic poster: vertical gradient so encoding is non-trivial
	img := image.NewRGBA(image.Rect(0, 0, 300, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, color.RGBA{uint8(y / 2), uint8(x / 2), 180, 255})
		}
	}
	url := "test://poster"
	putPoster(a, url, img)
	an := source.Anime{Source: "animecix", ID: "1", Title: "Deneme Anime", Kind: "TV", Poster: url, Description: "özet"}
	a.details[an.Key()] = an
	sc := newAnime(an, nil, 0, 0)
	a.stack = append(a.stack, sc)
	sc.ensure(a)
	a.episodes[epKey(sc.an, 1)] = []source.Episode{{ID: "1", Season: 1, Number: 1, Title: "Bir"}}

	// first frame: half-block poster while the sixel encodes in background
	if v := a.View(); strings.Contains(v, "\x1bP") {
		t.Fatal("sixel should not be inline-encoded on the first frame")
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.gfx.busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	v1 := a.View()
	if strings.Contains(v1, "\x1b_kz") {
		t.Fatal("marker leaked to terminal output")
	}
	m := reSixelPos.FindStringSubmatch(v1)
	if m == nil {
		t.Fatal("no sixel placement in frame")
	}
	row, _ := strconv.Atoi(m[1])
	col, _ := strconv.Atoi(m[2])
	t.Logf("poster at row %d col %d, %d bytes", row, col, len(m[3]))
	// decode back and check the pixel size matches the reserved cells
	dec, err := decodeSixel(m[3])
	if err != nil {
		t.Fatal(err)
	}
	b := dec.Bounds()
	if b.Dx()%10 != 0 || b.Dy()%20 != 0 {
		t.Fatalf("image %dx%d is not a whole number of 10x20 cells", b.Dx(), b.Dy())
	}
	if dir := os.Getenv("ANICLITR_SNAP_DIR"); dir != "" {
		var buf bytes.Buffer
		_ = png.Encode(&buf, dec)
		_ = os.WriteFile(filepath.Join(dir, "sixel-decoded.png"), buf.Bytes(), 0o644)
	}
	// frames spaced past the rate limit
	emits := func() bool { time.Sleep(110 * time.Millisecond); return strings.Contains(a.View(), "\x1bP") }
	// burst: re-sent for a few frames, then a static screen costs nothing
	sent := 1
	for i := 0; i < 6; i++ {
		if emits() {
			sent++
		}
	}
	if sent > 3 {
		t.Fatalf("burst too long: %d sends", sent)
	}
	if emits() || a.gfx.busy() {
		t.Fatal("static frame should not resend sixel")
	}
	// a change under the image (cursor move on rows it spans) → re-sent
	a.episodes[epKey(sc.an, 1)] = append(a.episodes[epKey(sc.an, 1)], source.Episode{ID: "2", Season: 1, Number: 2, Title: "İki"})
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !emits() {
		t.Fatal("changed rows under the image should resend it")
	}
	// rate limit: an immediate second change is deferred, not dropped
	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	if strings.Contains(a.View(), "\x1bP") {
		t.Fatal("sends within 100ms should be deferred")
	}
	if !a.gfx.busy() {
		t.Fatal("deferred send must keep the UI ticking")
	}
	if !emits() {
		t.Fatal("deferred send should go out on the next frame")
	}
	// overlays hide images entirely
	a.help = true
	if v := a.View(); strings.Contains(v, "\x1bP") || strings.Contains(v, "\x1b_kz") {
		t.Fatal("help overlay must suppress images")
	}
}

// TestLiveSixelPoster encodes a real cover for visual comparison.
func TestLiveSixelPoster(t *testing.T) {
	dir := os.Getenv("ANICLITR_SNAP_DIR")
	if os.Getenv("ANICLITR_LIVE") == "" || dir == "" {
		t.Skip("set ANICLITR_LIVE=1 and ANICLITR_SNAP_DIR")
	}
	a := testApp(t, Graphics{Sixel: true, CellW: 10, CellH: 20})
	url := "https://image.tmdb.org/t/p/original/bYOrD9rjnDsyJESTFbJdgiDXNhb.jpg"
	m := fetchPoster(url)
	if m.err != nil {
		t.Fatal(m.err)
	}
	a.posters.set(m)
	t0 := time.Now()
	s := encodeSixel(fullImage(url, m.raw), 260, 380, 10, cBg)
	t.Logf("encode 260x380: %v, %d KB", time.Since(t0), len(s)/1024)
	img, err := decodeSixel(s)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	_ = os.WriteFile(filepath.Join(dir, "sixel-frieren.png"), buf.Bytes(), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "blocks-frieren.ans"), []byte(strings.Join(halfblocks(a.posters.scaledFor(url, 26, 19), nil, 26, 19, 1), "\n")), 0o644)
}

func decodeSixel(s string) (image.Image, error) {
	var img image.Image
	err := sixel.NewDecoder(strings.NewReader(s)).Decode(&img)
	return img, err
}

// A picture that goes away must not linger: the blank rows it covered are
// forced to repaint (they would look unchanged to Bubble Tea otherwise).
func TestSixelVanishedImageRepaints(t *testing.T) {
	g := newGfx(Graphics{Sixel: true, CellW: 10, CellH: 20}, true)
	g.enc[encKey("u", 4, 3)] = "\x1bPq#0~\x1b\\"
	frame := func(withImg bool) []string {
		g.reqs = g.reqs[:0]
		lines := []string{"header", "a", "b", "c", "d", "footer"}
		if withImg {
			ph := g.place("u", 4, 3)
			for i := range ph {
				lines[1+i] = ph[i]
			}
		} else {
			for i := 1; i <= 3; i++ {
				lines[i] = "    "
			}
		}
		return g.finish(lines, false)
	}
	frame(true)
	out := frame(false)
	for r := 1; r <= 3; r++ {
		if out[r] == "    " {
			t.Fatalf("row %d under the vanished image was not forced to repaint", r)
		}
	}
	if out[4] != "d" || out[0] != "header" {
		t.Fatal("rows outside the old image must be untouched")
	}
	if !g.busy() {
		t.Fatal("stale repaint must keep the UI ticking briefly")
	}
	time.Sleep(200 * time.Millisecond)
	frame(false)
	if out := frame(false); out[2] != "    " || g.busy() {
		t.Fatal("stale repaint should stop after a short while")
	}
}

// Self-paint renderer: moving the cursor in a list beside a poster must
// not re-send (and so blink) the picture; overlays hide it and closing
// them brings it back; a picture that goes away is blanked.
func TestSelfPaintNoBlink(t *testing.T) {
	a := testApp(t, Graphics{Sixel: true, CellW: 10, CellH: 20})
	var out bytes.Buffer
	a.selfPaintSync(&out)
	a.Update(tea.WindowSizeMsg{Width: 150, Height: 44})
	img := image.NewRGBA(image.Rect(0, 0, 300, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, color.RGBA{uint8(y / 2), uint8(x / 2), 180, 255})
		}
	}
	url := "test://poster"
	putPoster(a, url, img)
	an := source.Anime{Source: "animecix", ID: "1", Title: "Deneme Anime", Kind: "TV", Poster: url, Description: "özet"}
	a.details[an.Key()] = an
	sc := newAnime(an, nil, 0, 0)
	a.stack = append(a.stack, sc)
	sc.ensure(a)
	var eps []source.Episode
	for i := 1; i <= 12; i++ {
		eps = append(eps, source.Episode{ID: fmt.Sprint(i), Season: 1, Number: i, Title: fmt.Sprint("Bölüm adı ", i)})
	}
	a.episodes[epKey(sc.an, 1)] = eps
	a.Update(tickMsg(time.Now()))
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "\x1bP") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		a.Update(tickMsg(time.Now()))
	}
	if !strings.Contains(out.String(), "\x1bP") {
		t.Fatal("poster was never sent")
	}
	if strings.Contains(out.String(), "\x1b_k") {
		t.Fatal("markers leaked")
	}
	time.Sleep(150 * time.Millisecond)
	for i := 0; i < 5; i++ {
		out.Reset()
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
		time.Sleep(120 * time.Millisecond)
		a.Update(tickMsg(time.Now()))
		if strings.Contains(out.String(), "\x1bP") {
			t.Fatalf("cursor move %d re-sent the poster (blink)", i)
		}
	}
	// overlay hides it; closing brings it back
	a.Update(tea.KeyMsg{Type: tea.KeyF1}) // işlemler panel
	out.Reset()
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(out.String(), "\x1bP") {
		t.Fatal("poster should come back after the overlay closes")
	}
	// leaving the screen blanks the picture's rectangle
	out.Reset()
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	a.trans = tween{}
	a.Update(tickMsg(time.Now()))
	// the poster's area is rewritten as text (row 4 = where it sat)
	if strings.Contains(out.String(), "\x1bP") || !strings.Contains(out.String(), "\x1b[4;") {
		t.Fatal("leaving the screen should repaint the poster area, not re-send it")
	}
}
