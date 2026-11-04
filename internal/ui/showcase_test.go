package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// fakePoster paints a two-tone gradient cover so layouts can be judged
// offline (no network).
func fakePoster(a *App, url string, c1, c2 color.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		t := float64(y) / 299
		for x := 0; x < 200; x++ {
			u := float64(x) / 199 * .3
			img.Set(x, y, color.RGBA{
				uint8(float64(c1.R)*(1-t-u) + float64(c2.R)*(t+u)),
				uint8(float64(c1.G)*(1-t) + float64(c2.G)*t),
				uint8(float64(c1.B)*(1-t) + float64(c2.B)*t), 255})
		}
	}
	putPoster(a, url, img)
}

// putPoster loads img as the cover of url the way fetchPoster would (test://
// is not on disk, so the file itself is kept for the sixel path).
func putPoster(a *App, url string, img image.Image) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	a.posters.set(posterMsg{url: url, img: downscale(img, smallW), raw: buf.Bytes(), accent: dominant(img)})
}

var showTitles = []string{"Sousou no Frieren", "Dandadan", "Kusuriya no Hitorigoto", "Chainsaw Man", "Mushishi", "Odd Taxi", "Mononoke", "Ping Pong the Animation", "Bocchi the Rock!", "Vinland Saga"}

func showcaseApp(t *testing.T, w, h int) *App {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	pal := []color.RGBA{{230, 120, 170, 255}, {90, 140, 230, 255}, {240, 190, 90, 255}, {120, 210, 160, 255}, {180, 110, 240, 255}, {60, 60, 90, 255}}
	now := time.Now()
	var feed []source.FeedItem
	for i, ti := range showTitles {
		an := source.Anime{Source: "animecix", ID: fmt.Sprint(100 + i), Title: ti, Kind: "TV", Poster: fmt.Sprintf("test://p%d", i), Year: 2020 + i%6, Genres: []string{"Macera", "Dram", "Fantastik"}[:1+i%3]}
		fakePoster(a, an.Poster, pal[i%len(pal)], pal[(i+2)%len(pal)])
		a.details[an.Key()] = an
		for d := 0; d < 3; d++ {
			feed = append(feed, source.FeedItem{Anime: an, Season: 1, Episode: 12 - d, Fansub: "AnimeciX Fansub", Released: now.Add(-time.Duration(i*5+d*24*7)*time.Hour - time.Duration(d*3)*time.Hour)})
		}
		if i < 6 {
			e := an
			a.hist.Update(e, func(x *store.Entry) {
				x.Season, x.Episode, x.Position, x.Duration = 1, 3+i, float64(300+i*200), 1420
				for k := 1; k < 3+i; k++ {
					x.Watched[store.EpKey(1, k)] = true
				}
			})
		}
		st := store.Statuses[i%len(store.Statuses)]
		a.lib.Set(an, func(it *store.Item) {
			it.Status = st
			it.Score = []int{9, 8, 0, 10, 7, 6, 0, 8, 9, 5}[i]
			if i%3 == 0 {
				it.Lists = []string{"favoriler"}
			}
		})
	}
	a.lib.AddList("yaz 2026")
	for d := 0; d < 7; d++ {
		a.hist.Activity[store.DayKey(now.AddDate(0, 0, -d))] = float64([]int{50, 0, 90, 30, 120, 75, 40}[d] * 60)
	}
	a.Update(feedMsg{items: feed})
	a.feedPrime()
	return a
}

// feedPrime finishes reveal/intro animations so snapshots are stable.
func (a *App) feedPrime() {
	a.start = time.Now().Add(-time.Hour)
}

func TestShowcase(t *testing.T) {
	dir := os.Getenv("ANICLITR_SNAP_DIR")
	if dir == "" {
		t.Skip("set ANICLITR_SNAP_DIR to render showcase snapshots")
	}
	a := showcaseApp(t, 200, 56)
	time.Sleep(600 * time.Millisecond) // let cascades settle
	snap(t, a, "home")
	a.Update(key("down"))
	a.Update(key("right"))
	time.Sleep(300 * time.Millisecond)
	a.View()
	snap(t, a, "home-shelf")

	// command palette, plain and in theme mode
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	a.pal.openT = tween{}
	snap(t, a, "palette")
	for _, r := range "fri" {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	snap(t, a, "palette-fri")
	a.Update(key("esc"))
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	a.pal.openT = tween{}
	a.Update(key("down"))
	time.Sleep(450 * time.Millisecond)
	a.Update(tickMsg(time.Now()))
	snap(t, a, "palette-theme")
	a.Update(key("esc"))
	time.Sleep(450 * time.Millisecond)
	a.Update(tickMsg(time.Now()))

	// library: list, then the poster wall, then the edit popup
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	a.trans = tween{}
	time.Sleep(500 * time.Millisecond)
	snap(t, a, "library")
	a.Update(key("down"))
	a.Update(key("right"))
	a.Update(key("down"))
	time.Sleep(300 * time.Millisecond)
	snap(t, a, "library-shelf")
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	time.Sleep(300 * time.Millisecond)
	snap(t, a, "library-wall")
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	snap(t, a, "library-popup")
	a.Update(key("esc"))

	// every theme on the home screen
	a.stack = a.stack[:1]
	a.cfg.Motion = false
	for _, th := range themes() {
		a.setTheme(th)
		snap(t, a, "theme-"+strings.ReplaceAll(th.Name, " ", "_"))
	}
	a.setTheme(builtinThemes[0])
	a.cfg.Motion = true

	// screensaver + confetti
	a.saver, a.saverAt = true, time.Now().Add(-7*time.Second)
	a.View()
	time.Sleep(100 * time.Millisecond)
	snap(t, a, "saver")
	a.saver = false
	a.anchorOK = false
	a.celebrate(90)
	for i := 0; i < 8; i++ {
		time.Sleep(40 * time.Millisecond)
		a.fx.step()
	}
	snap(t, a, "confetti")
}

// Fuzzy matching is Turkish-aware and rewards contiguous matches.
func TestFuzzy(t *testing.T) {
	if sc, _ := fuzzy("kitap", "Kitaplık"); sc <= 0 {
		t.Fatal("kitap should match Kitaplık")
	}
	if sc, _ := fuzzy("sik", "Sıkı düzen"); sc <= 0 {
		t.Fatal("folded match failed")
	}
	if sc, _ := fuzzy("zzz", "Kitaplık"); sc != 0 {
		t.Fatal("non-match scored")
	}
	a, _ := fuzzy("tema", "Tema: gece")
	b, _ := fuzzy("tema", "Arşiv modunu aç (tema yok)")
	if a <= b {
		t.Fatalf("prefix match should win: %d vs %d", a, b)
	}
}

// Theme switching swaps every token and the frames still lay out exactly.
func TestThemesLayout(t *testing.T) {
	a := showcaseApp(t, 140, 44)
	a.cfg.Motion = false
	for _, th := range themes() {
		a.setTheme(th)
		if cAccent != th.Accent || cBg != th.Bg {
			t.Fatalf("%s not applied", th.Name)
		}
		snap(t, a, "t")
	}
	a.setTheme(builtinThemes[0])
}

// The library store round-trips statuses, scores and lists.
func TestLibraryStore(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	l := store.OpenLibrary()
	an := source.Anime{Source: "x", ID: "1", Title: "A"}
	l.Set(an, func(it *store.Item) { it.Status = store.Completed; it.Score = 9 })
	l.AddList("yaz")
	if !l.ToggleList(an, "yaz") {
		t.Fatal("toggle should add")
	}
	l.RenameList("yaz", "kış")
	l2 := store.OpenLibrary()
	it, ok := l2.Get(an.Key())
	if !ok || it.Score != 9 || it.Status != store.Completed || !it.InList("kış") || it.FinishedAt.IsZero() {
		t.Fatalf("round trip failed: %+v", it)
	}
	l2.DeleteList("kış")
	if it, _ := l2.Get(an.Key()); it.InList("kış") {
		t.Fatal("list delete should remove membership")
	}
}

// While mpv plays the TUI must not animate (no ticks).
func TestPlayingIsIdle(t *testing.T) {
	a := testApp(t, Graphics{})
	a.cfg.Motion = true
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	st := &source.Stream{Qualities: []source.Quality{{Label: "720p", URL: "x"}}}
	p := newPlaying(source.Anime{Source: "animecix", ID: "1", Title: "T", Kind: "TV"}, source.Episode{Season: 1, Number: 1}, nil, "F", "Tau", st, st.Qualities[0])
	a.stack = append(a.stack, p)
	p.sess = nil
	p.started, p.pos, p.dur = true, 100, 1400
	a.toastUntil = time.Time{}
	a.trans = tween{}
	a.oscUntil = time.Time{}
	a.View()
	if a.needsTick() {
		t.Fatal("playing screen should not request frames while mpv plays")
	}
}

// Frame budget on the busiest screens (offline data, motion on).
func TestShowcaseFrameCost(t *testing.T) {
	a := showcaseApp(t, 200, 56)
	a.cfg.Motion = true
	measure := func(name string) {
		a.View()
		t0 := time.Now()
		const n = 60
		for i := 0; i < n; i++ {
			a.frame++
			a.View()
		}
		per := time.Since(t0) / n
		t.Logf("%s: %v/frame", name, per)
		if per > 12*time.Millisecond {
			t.Errorf("%s too slow: %v", name, per)
		}
	}
	measure("home")
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	measure("home+palette")
	a.Update(key("esc"))
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	measure("library")
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	measure("library wall")
	a.stack = a.stack[:1]
	a.saver = true
	measure("screensaver")
	a.saver = false
	a.celebrate(90)
	measure("confetti")
}

// The self-paint path (compose + cell diff) must stay cheap.
func TestPaintCost(t *testing.T) {
	a := showcaseApp(t, 200, 56)
	var out bytes.Buffer
	a.selfPaintSync(&out)
	a.Update(tea.WindowSizeMsg{Width: 200, Height: 56})
	t0 := time.Now()
	const n = 60
	for i := 0; i < n; i++ {
		out.Reset()
		a.frame++
		a.paint()
	}
	per := time.Since(t0) / n
	t.Logf("paint: %v/frame, last frame %d bytes", per, out.Len())
	if per > 15*time.Millisecond {
		t.Errorf("paint too slow: %v", per)
	}
}

func TestShowcaseDownloads(t *testing.T) {
	dir := os.Getenv("ANICLITR_SNAP_DIR")
	if dir == "" {
		t.Skip("set ANICLITR_SNAP_DIR")
	}
	a := showcaseApp(t, 200, 50)
	now := time.Now()
	mk := func(id int, title string, s, e int, state string, done, total int64, speed float64) download.Job {
		return download.Job{ID: id, Source: "animecix", AnimeID: fmt.Sprint(100 + id%5), Title: title, Poster: fmt.Sprintf("test://p%d", id%5),
			Season: s, Episode: e, Fansub: "HakaiRyuu & Lexellures", Quality: "1080p", State: state, Done: done, Total: total,
			Speed: speed, ETA: 95 * time.Second, Tool: "aria2c", Added: now, Finished: now.Add(-2 * time.Hour)}
	}
	a.dl.Preview(mk(1, "Sousou no Frieren", 1, 5, download.Done, 412<<20, 412<<20, 0))
	a.dl.Preview(mk(2, "Sousou no Frieren", 1, 6, download.Running, 180<<20, 405<<20, 6.2*(1<<20)))
	a.dl.Preview(mk(3, "Dandadan", 1, 2, download.Running, 40<<20, 380<<20, 3.1*(1<<20)))
	a.dl.Preview(mk(4, "Dandadan", 1, 3, download.Queued, 0, 0, 0))
	f := mk(5, "Mushishi", 1, 1, download.Failed, 0, 0, 0)
	f.Err = "kaynaklar alınamadı: zaman aşımı"
	a.dl.Preview(f)
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	a.trans = tween{}
	a.Update(key("down"))
	time.Sleep(200 * time.Millisecond)
	snap(t, a, "downloads")
}

// Search-as-you-type: local hits show instantly, stale live answers are
// dropped, ↓ + enter opens the suggestion directly.
func TestSuggest(t *testing.T) {
	a := showcaseApp(t, 160, 48)
	h := a.stack[0].(*home)
	for _, r := range "fri" {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(h.sug.items) == 0 || h.sug.items[0].an.Title != "Sousou no Frieren" || h.sug.items[0].local == "" {
		t.Fatalf("local suggestion missing: %+v", h.sug.items)
	}
	old := h.sug.gen - 1
	a.Update(sugResMsg{gen: old, q: "fr", res: []source.Anime{{Source: "animecix", ID: "9", Title: "Eski Sonuç"}}})
	for _, it := range h.sug.items {
		if it.an.Title == "Eski Sonuç" {
			t.Fatal("stale result should be ignored")
		}
	}
	a.Update(sugResMsg{gen: h.sug.gen, q: "fri", res: []source.Anime{{Source: "animecix", ID: "100", Title: "Sousou no Frieren"}, {Source: "animecix", ID: "5", Title: "Frieren Movie"}}})
	if len(h.sug.items) != 2 {
		t.Fatalf("live results should merge without duplicates: %d", len(h.sug.items))
	}
	v := a.View()
	if !strings.Contains(ansiRe.ReplaceAllString(v, ""), "Frieren Movie") {
		t.Fatal("dropdown not drawn")
	}
	a.Update(key("down"))
	a.Update(key("down"))
	a.Update(key("enter"))
	if _, ok := a.top().(*animeScreen); !ok {
		t.Fatalf("enter on a suggestion should open the anime, top is %T", a.top())
	}
}
