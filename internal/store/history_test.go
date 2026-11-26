package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/source"
)

func TestHistoryUpdateGetRecentAndPersist(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	h := Open()
	rev0 := h.Rev()
	a1 := source.Anime{Source: "animecix", ID: "10", Title: "Frieren", Kind: "TV", MalID: 52991}
	a2 := source.Anime{Source: "anihub", ID: "20", Title: "Dandadan", Kind: "TV", MalID: 57334}

	h.Update(a1, func(e *Entry) {
		e.Season, e.Episode = 1, 5
		e.Position, e.Duration = 600, 1200
		e.Fansub, e.Host = "HakaiRyuu", "Tau Video"
		e.Watched[EpKey(1, 4)] = true
		e.Progress[EpKey(1, 5)] = 50
	})
	time.Sleep(2 * time.Millisecond)
	h.Update(a2, func(e *Entry) {
		e.Season, e.Episode = 1, 1
		e.Position, e.Duration = 1200, 1200
		e.Watched[EpKey(1, 1)] = true
	})

	if h.Rev() <= rev0+1 {
		t.Fatalf("expected Rev to advance on each update: rev0=%d rev=%d", rev0, h.Rev())
	}

	got, ok := h.Get(a1.Key())
	if !ok {
		t.Fatal("expected entry for a1")
	}
	if got.Percent() != 50 || got.Fansub != "HakaiRyuu" || !got.Watched["1:4"] {
		t.Fatalf("unexpected entry: %+v", got)
	}

	recent := h.Recent()
	if len(recent) != 2 || recent[0].AnimeID != "20" || recent[1].AnimeID != "10" {
		t.Fatalf("Recent order wrong: %+v", recent)
	}

	// Re-open from disk and confirm persistence.
	h2 := Open()
	reloaded, ok := h2.Get(a1.Key())
	if !ok || reloaded.Title != "Frieren" || reloaded.MalID != 52991 || !reloaded.Watched["1:4"] {
		t.Fatalf("reloaded entry mismatch: ok=%v %+v", ok, reloaded)
	}
}

func TestHistoryCorruptBacksUpNeverOverwrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)

	raw := `{"entries": BROKEN`
	p := filepath.Join(home, "history.json")
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Open()
	if len(h.Entries) != 0 {
		t.Fatalf("expected empty entries after corrupt file, got %d", len(h.Entries))
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), "history.json.bozuk-") {
			b, err := os.ReadFile(filepath.Join(home, de.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != raw {
				t.Fatalf("backup content = %q, want %q", string(b), raw)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("corrupt history.json must be backed up to history.json.bozuk-*")
	}
}

func TestHistoryRemoveAndRestore(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	h := Open()
	an := source.Anime{Source: "animecix", ID: "99", Title: "Bleach", MalID: 269}
	h.Update(an, func(e *Entry) {
		e.Season, e.Episode = 1, 12
		e.Watched[EpKey(1, 12)] = true
	})

	snap, ok := h.Get(an.Key())
	if !ok {
		t.Fatal("missing entry before remove")
	}
	h.Remove(an.Key())
	if _, ok := h.Get(an.Key()); ok {
		t.Fatal("entry should be removed")
	}

	h.Restore(snap)
	restored, ok := h.Get(an.Key())
	if !ok || restored.Episode != 12 || !restored.Watched["1:12"] {
		t.Fatalf("restore failed: ok=%v %+v", ok, restored)
	}
	if _, ok := Open().Get(an.Key()); !ok {
		t.Fatal("restored entry was not persisted")
	}
}

func TestHistoryActivityStreakAndTotals(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	h := Open()
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	twoDaysAgo := now.AddDate(0, 0, -2)

	h.AddActivity(-50)        // ignored
	h.AddActivity(4 * 3600)   // >3h ignored
	h.AddActivity(1200)       // 20 min today
	h.Activity[DayKey(yesterday)] = 600
	h.Activity[DayKey(twoDaysAgo)] = 300

	if got := h.Day(now); got != 1200 {
		t.Fatalf("Day(today) = %v, want 1200", got)
	}
	if got := h.Streak(); got != 3 {
		t.Fatalf("Streak() = %d, want 3", got)
	}

	an := source.Anime{Source: "animecix", ID: "1", Title: "Test"}
	h.Update(an, func(e *Entry) {
		e.Watched[EpKey(1, 1)] = true
		e.Watched[EpKey(1, 2)] = true
		e.Watched[EpKey(1, 3)] = false
	})

	eps, secs := h.Totals()
	if eps != 2 {
		t.Fatalf("Totals eps = %d, want 2", eps)
	}
	if secs != 2100 {
		t.Fatalf("Totals secs = %v, want 2100", secs)
	}
}
