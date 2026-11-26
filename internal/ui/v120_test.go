package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
	"aniclitr/internal/source"
)

func TestSleepTimerLifecycle(t *testing.T) {
	var st sleepTimer
	if st.active() || st.shortPill() != "" {
		t.Fatalf("new timer should be inactive")
	}
	// Cycle 1: this episode
	if lbl := st.cycle(); !strings.Contains(lbl, "Bu bölüm") {
		t.Fatalf("expected this-episode label, got %q", lbl)
	}
	stop, suspend := st.onEpisodeFinished()
	if !stop || suspend || st.active() {
		t.Fatalf("this-ep should stop without suspend and reset: stop=%v suspend=%v active=%v", stop, suspend, st.active())
	}

	// Cycle through to 2 episodes
	st.cycle() // 1 ep
	st.cycle() // 2 eps
	stop, _ = st.onEpisodeFinished()
	if stop || !st.active() || st.epsLeft != 1 {
		t.Fatalf("2-ep first finish should keep playing with 1 left: stop=%v active=%v left=%d", stop, st.active(), st.epsLeft)
	}
	stop, _ = st.onEpisodeFinished()
	if !stop || st.active() {
		t.Fatalf("2-ep second finish should stop and clear")
	}

	// Time-based expiration
	st.mode = sleep30Min
	st.deadline = time.Now().Add(-time.Second)
	if !st.expired() {
		t.Fatalf("past deadline must be expired")
	}

	// Suspend PC mode
	st.mode = sleepSuspendPC
	st.epsLeft = 1
	stop, suspend = st.onEpisodeFinished()
	if !stop || !suspend || st.active() {
		t.Fatalf("suspend mode should stop and request suspend: stop=%v suspend=%v", stop, suspend)
	}
}

func TestSearchTitleRelevanceRanking(t *testing.T) {
	r := newResults("another")
	r.order = []string{"animecix"}
	r.byProv = map[string][]source.Anime{
		"animecix": {
			{Source: "animecix", ID: "1", Title: "Yandere Dark Elf: She Chased Me All the Way From Another World!"},
			{Source: "animecix", ID: "2", Title: "Re:ZERO -Starting Life in Another World-"},
			{Source: "animecix", ID: "3", Title: "Another"},
			{Source: "animecix", ID: "4", Title: "Another: The Other - Inga"},
		},
	}
	r.rebuild()
	if len(r.items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(r.items))
	}
	if r.items[0].main.Title != "Another" {
		t.Fatalf("exact match 'Another' should be ranked #1, got %q", r.items[0].main.Title)
	}
	if r.items[1].main.Title != "Another: The Other - Inga" {
		t.Fatalf("prefix match should be ranked #2, got %q", r.items[1].main.Title)
	}
}

func TestWrappedScreenSlidesAndExport(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := showcaseApp(t, 120, 36)
	ws := newWrappedScreen(a)
	a.stack = append(a.stack, ws)

	// Jump through all 6 slides and verify rendering
	for i := 0; i < wrappedSlideCount; i++ {
		ws.gotoSlide(a, i)
		ws.slideAt = time.Now().Add(-2 * time.Second)
		if ws.animating(a) {
			t.Fatalf("slide %d should stop animating after 950ms", i)
		}
		v := ws.view(a, 120, 33)
		if strings.TrimSpace(v) == "" {
			t.Fatalf("slide %d rendered empty", i)
		}
	}

	// Export PNG card via key 'e'
	ws.update(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if ws.exported == "" {
		t.Fatalf("expected exported PNG path after pressing e")
	}
	st, err := os.Stat(ws.exported)
	if err != nil || st.Size() < 1000 {
		t.Fatalf("exported PNG missing or too small: err=%v", err)
	}
}

func TestDiscoverMoodCycle(t *testing.T) {
	a := showcaseApp(t, 120, 36)
	d := newDiscover()
	d.tab = tRecs
	d.menu = false
	if d.moodIdx != -1 {
		t.Fatalf("expected default personal recs (-1), got %d", d.moodIdx)
	}
	d.key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if d.moodIdx != 0 {
		t.Fatalf("pressing m should switch to first mood preset (0), got %d", d.moodIdx)
	}
	for i := 0; i < len(anilist.MoodPresets); i++ {
		d.key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	}
	if d.moodIdx != -1 {
		t.Fatalf("cycling all moods should wrap back to personal recs (-1), got %d", d.moodIdx)
	}
}
