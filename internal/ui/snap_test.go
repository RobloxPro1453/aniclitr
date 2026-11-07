package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"aniclitr/internal/aniskip"
	"aniclitr/internal/config"
	"aniclitr/internal/source"
	_ "aniclitr/internal/source/anihub"
	_ "aniclitr/internal/source/animecix"
	_ "aniclitr/internal/source/animedepo"
	_ "aniclitr/internal/source/animetr"
	_ "aniclitr/internal/source/animim"
	_ "aniclitr/internal/source/hianime"
	_ "aniclitr/internal/source/sonanime"
)

// drain runs commands synchronously (ignoring animation ticks) and feeds results back.
func drain(t *testing.T, a *App, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 12 {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(70 * time.Second):
		t.Log("cmd timeout")
		return
	}
	switch m := msg.(type) {
	case nil, tickMsg:
		return
	case tea.BatchMsg:
		for _, c := range m {
			drain(t, a, c, depth+1)
		}
		return
	}
	if _, ok := msg.(startedMsg); ok {
		return // never launch mpv in tests
	}
	if _, ok := msg.(segsMsg); ok {
		return
	}
	_, next := a.Update(msg)
	drain(t, a, next, depth+1)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b[\]_][^\x1b]*\x1b\\`)

func snap(t *testing.T, a *App, name string) {
	v := a.View()
	dir := os.Getenv("ANICLITR_SNAP_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	_ = os.WriteFile(filepath.Join(dir, name+".ans"), []byte(v), 0o644)
	plain := ansiRe.ReplaceAllString(v, "")
	_ = os.WriteFile(filepath.Join(dir, name+".txt"), []byte(plain), 0o644)
	lines := strings.Split(plain, "\n")
	if len(lines) != a.h {
		t.Errorf("%s: %d lines, want %d", name, len(lines), a.h)
	}
	for i, l := range lines {
		if w := len([]rune(l)); w > a.w {
			t.Errorf("%s: line %d is %d cells wide (> %d)", name, i, w, a.w)
		}
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, a *App, k string) {
	_, cmd := a.Update(key(k))
	drain(t, a, cmd, 0)
}

func TestLiveSnapshots(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1 to render against live sources")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, size := range [][2]int{{150, 44}, {100, 30}, {70, 22}} {
		cfg := config.Load()
		cfg.Legacy = true
		cfg.Motion = true
		cfg.Sources = map[string]bool{"animecix": true, "hianime": true, "animedepo": true}
		a := New(cfg, Graphics{CellW: 10, CellH: 20})
		a.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		drain(t, a, a.loadFeed(), 0)
		if hm, ok := a.top().(*home); ok {
			for i := 0; i < 8 && i < len(hm.feed); i++ {
				drain(t, a, func() tea.Msg { return fetchPoster(hm.feed[i].Anime.Poster) }, 0)
			}
		}
		tag := func(s string) string { return s + "-" + itoa(size[0]) }
		if size[0] == 150 { // intro animation frames
			for _, ms := range []int{250, 520, 1250, 1500} {
				a.start = time.Now().Add(-time.Duration(ms) * time.Millisecond)
				snap(t, a, tag("0-intro-"+itoa(ms)))
			}
		}
		a.start = time.Now().Add(-time.Hour)
		snap(t, a, tag("1-home"))
		for _, r := range "frieren" {
			press(t, a, string(r))
		}
		press(t, a, "enter")
		snap(t, a, tag("2-results"))
		press(t, a, "enter")
		snap(t, a, tag("3-anime"))
		press(t, a, "enter")
		snap(t, a, tag("4-picker"))
		press(t, a, "right")
		snap(t, a, tag("5-picker-hosts"))
	}
}

// TestFrameCost measures a full animated home frame (petals + logo + lists)
// and checks the help overlay keeps every line inside the grid.
func TestFrameCost(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	cfg := config.Load()
	cfg.Motion = true
	a := New(cfg, Graphics{CellW: 10, CellH: 20})
	a.Update(tea.WindowSizeMsg{Width: 200, Height: 55})
	drain(t, a, a.loadFeed(), 0)
	a.start = time.Now().Add(-time.Hour)
	const n = 120
	t0 := time.Now()
	for i := 0; i < n; i++ {
		a.frame++
		_ = a.View()
	}
	per := time.Since(t0) / n
	t.Logf("home frame 200x55: %v (%.0f fps budget used at 30fps: %.1f%%)", per, 1/per.Seconds(), per.Seconds()*30*100)
	if per > 8*time.Millisecond {
		t.Errorf("frame too slow: %v", per)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyTab})
	a.Update(key("?"))
	if !a.help {
		t.Fatal("? should open help when not typing")
	}
	snap(t, a, "10-help")
}

// TestMergedSources: the same anime on AnimeciX and HiAnime must collapse
// into one result whose primary copy is AnimeciX (the user hit this).
func TestMergedSources(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	cfg := config.Load()
	cfg.Motion = false
	cfg.Sources = map[string]bool{"animecix": true, "hianime": true}
	a := New(cfg, Graphics{CellW: 10, CellH: 20})
	a.Update(tea.WindowSizeMsg{Width: 150, Height: 44})
	for _, r := range "mouryou no hako" {
		press(t, a, string(r))
	}
	press(t, a, "enter")
	r := a.top().(*results)
	if len(r.items) == 0 {
		t.Fatal("no results")
	}
	for id, st := range r.states {
		t.Logf("%s loading=%v n=%d err=%v got=%d", id, st.loading, st.count, st.err, len(r.byProv[id]))
	}
	for _, e := range r.items {
		t.Logf("item %s %q alts=%d keys=%v", e.main.Source, e.main.Title, len(e.alts), e.keys)
	}
	first := r.items[0]
	if first.main.Source != "animecix" || !first.has("hianime") {
		t.Errorf("want animecix main with hianime alt, got main=%s alts=%d", first.main.Source, len(first.alts))
	}
	snap(t, a, "8-merged")
	press(t, a, "enter")
	snap(t, a, "9-merged-anime")
	press(t, a, "s")
	sc := a.top().(*animeScreen)
	if sc.base.Source != "hianime" {
		t.Errorf("s should switch to hianime, got %s", sc.base.Source)
	}
}

// TestPlayingSnapshot renders the now-playing card with synthetic state.
func TestPlayingSnapshot(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	a := New(config.Load(), Graphics{CellW: 10, CellH: 20})
	a.start = time.Now().Add(-time.Hour)
	a.Update(tea.WindowSizeMsg{Width: 150, Height: 44})
	an := source.Anime{Source: "animecix", ID: "11130", Title: "Sousou no Frieren", Kind: "TV",
		Poster: "https://image.tmdb.org/t/p/original/bYOrD9rjnDsyJESTFbJdgiDXNhb.jpg"}
	st := &source.Stream{Qualities: []source.Quality{{Label: "1080p"}}}
	ep := source.Episode{Season: 1, Number: 5, Title: "Ölülerin Hayaletleri"}
	p := newPlaying(an, ep, nil, "HakaiRyuu & Lexellures", "Tau Video", st, st.Qualities[0])
	p.started, p.pos, p.dur = true, 731, 1440
	p.segs = []aniskip.Segment{{Kind: "op", Start: 3, End: 93, EpLen: 1440}, {Kind: "ed", Start: 1330, End: 1420, EpLen: 1440}}
	p.skipText, p.skipAt = "Opening", time.Now()
	a.stack = append(a.stack, p)
	drain(t, a, a.posters.want(an.Poster), 0)
	snap(t, a, "6-playing-150")
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	snap(t, a, "6-playing-100")
	s := newSettings()
	a.stack = append(a.stack[:1], s)
	snap(t, a, "7-settings-100")
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + func() string {
		s := ""
		for n > 0 {
			s = string(rune('0'+n%10)) + s
			n /= 10
		}
		return s
	}())
}
