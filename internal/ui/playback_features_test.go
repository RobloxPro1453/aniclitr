package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/aniskip"
	"aniclitr/internal/config"
	"aniclitr/internal/source"
)

func TestMergeSkipSegments(t *testing.T) {
	st := &source.Stream{
		Intro: source.SkipRange{Start: 12, End: 102},
		Outro: source.SkipRange{Start: 1320, End: 1410},
	}

	// Empty AniSkip: both source OP and ED are used.
	got := mergeSkipSegments(nil, st)
	if len(got) != 2 || got[0].Kind != "op" || got[0].Start != 12 || got[1].Kind != "ed" || got[1].End != 1410 {
		t.Fatalf("empty aniskip merge failed: %+v", got)
	}

	// AniSkip supplies OP only: source ED fills the gap without duplicating OP.
	ani := []aniskip.Segment{{Kind: "mixed-op", Start: 10, End: 100}}
	got = mergeSkipSegments(ani, st)
	if len(got) != 2 || got[0].Kind != "mixed-op" || got[1].Kind != "ed" {
		t.Fatalf("partial aniskip merge failed: %+v", got)
	}

	// AniSkip supplies both: source timestamps do not override.
	both := []aniskip.Segment{
		{Kind: "op", Start: 5, End: 95},
		{Kind: "ed", Start: 1300, End: 1390},
	}
	got = mergeSkipSegments(both, st)
	if len(got) != 2 || got[0].Start != 5 || got[1].Start != 1300 {
		t.Fatalf("full aniskip should take precedence: %+v", got)
	}
}

func TestFindAdjacentSkipsFillerWhenEnabled(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := New(config.Load(), Graphics{})
	an := source.Anime{Source: "hianime", ID: "show-1", Title: "Show", Seasons: []source.Season{{Number: 1}}}
	eps := []source.Episode{
		{ID: "1", Season: 1, Number: 1, Title: "Canon 1"},
		{ID: "2", Season: 1, Number: 2, Title: "Recap", Filler: true},
		{ID: "3", Season: 1, Number: 3, Title: "Beach", Filler: true},
		{ID: "4", Season: 1, Number: 4, Title: "Canon 4"},
	}
	st := &source.Stream{Qualities: []source.Quality{{Label: "1080p", URL: "http://x"}}}

	// With SkipFiller off: goes straight to ep 2.
	a.cfg.SkipFiller = false
	p := newPlaying(an, eps[0], eps, "Sub", "Host", st, st.Qualities[0])
	msg := p.findAdjacent(a, +1, false)().(adjacentMsg)
	if msg.ep == nil || msg.ep.Number != 2 || msg.skippedFiller != 0 {
		t.Fatalf("SkipFiller=false expected ep 2, got %+v", msg)
	}

	// With SkipFiller on: skips ep 2 and 3, lands on ep 4.
	a.cfg.SkipFiller = true
	p2 := newPlaying(an, eps[0], eps, "Sub", "Host", st, st.Qualities[0])
	msg2 := p2.findAdjacent(a, +1, false)().(adjacentMsg)
	if msg2.ep == nil || msg2.ep.Number != 4 || msg2.skippedFiller != 2 {
		t.Fatalf("SkipFiller=true expected ep 4 (skipped 2), got %+v", msg2)
	}

	// Backward navigation never skips fillers.
	p3 := newPlaying(an, eps[3], eps, "Sub", "Host", st, st.Qualities[0])
	msg3 := p3.findAdjacent(a, -1, false)().(adjacentMsg)
	if msg3.ep == nil || msg3.ep.Number != 3 {
		t.Fatalf("backward should land on ep 3, got %+v", msg3)
	}
}

func TestFillerBadgeShownInClassicAndNova(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	an := source.Anime{Source: "hianime", ID: "naruto-1", Title: "Naruto", Seasons: []source.Season{{Number: 1, Name: "Sezon 1"}}}
	eps := []source.Episode{
		{ID: "1", Season: 1, Number: 1, Title: "Başlangıç"},
		{ID: "2", Season: 1, Number: 2, Title: "Özel Özet", Filler: true},
	}

	for _, uiMode := range []string{"", "yeni"} {
		a := New(config.Load(), Graphics{})
		a.cfg.UI = uiMode
		a.cfg.Motion = false
		a.w, a.h = 120, 32
		sc := newAnime(an, nil, 0, 0)
		sc.ready = true
		sc.an = an
		a.episodes[epKey(an, 1)] = eps
		out := ansiRe.ReplaceAllString(sc.view(a, a.w, a.h), "")
		if !strings.Contains(out, "filler") {
			t.Errorf("ui=%q: expected filler badge on episode list, got:\n%s", uiMode, out)
		}
	}
}

func TestSubProviderAlwaysAvailableAndLocalInputToggle(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	a := New(config.Load(), Graphics{})
	a.cfg.OSKey = "" // no OpenSubtitles key

	if a.subProvider() == nil {
		t.Fatal("expected AniSub provider to be active even without OpenSubtitles API key")
	}

	an := source.Anime{Source: "animim", ID: "1", Title: "Frieren"}
	ep := source.Episode{ID: "e1", Season: 1, Number: 1}
	st := &source.Stream{Qualities: []source.Quality{{Label: "1080p", URL: "http://x"}}}
	p := newPlaying(an, ep, []source.Episode{ep}, "Fan", "Host", st, st.Qualities[0])
	p.subs.open = true

	// Pressing 'l' opens local subtitle path prompt and marks screen as typing.
	p.subKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !p.subs.local || !p.typing() {
		t.Fatalf("expected local subtitle input mode active")
	}

	// Submitting an unsupported extension shows a clear warning without crashing.
	tmp := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(tmp, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.subs.localIn.Set(tmp)
	p.subKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(a.toast, "desteklenen") {
		t.Fatalf("expected unsupported extension warning, got toast=%q", a.toast)
	}

	// Pressing esc exits local input back to subtitle list.
	p.subKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if p.subs.local || p.typing() {
		t.Fatalf("expected esc to exit local subtitle input")
	}
}
