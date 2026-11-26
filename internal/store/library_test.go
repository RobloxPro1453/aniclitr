package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/source"
)

func TestLibrarySetReconcileAndPersist(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	lib := OpenLibrary()
	an := source.Anime{Source: "animecix", ID: "100", Title: "Frieren", Kind: "TV", MalID: 52991, EpCount: 28}

	it := lib.Set(an, func(x *Item) {
		x.Status = Completed
		x.Score = 10
		x.Watched = 20 // Reconcile must raise Watched to EpCount (28)
	})
	if it.Watched != 28 || it.FinishedAt.IsZero() || !it.MalDirty {
		t.Fatalf("reconcile/dirty mismatch: %+v", it)
	}
	select {
	case <-lib.Changed:
	default:
		t.Fatal("expected Changed channel to be signalled on dirty Set")
	}

	// Watched exceeding EpCount should expand EpCount.
	it = lib.Set(an, func(x *Item) {
		x.Status = Watching
		x.Watched = 30
	})
	if it.EpCount != 30 {
		t.Fatalf("EpCount = %d, want 30", it.EpCount)
	}

	reloaded := OpenLibrary()
	got, ok := reloaded.Get(an.Key())
	if !ok || got.Score != 10 || got.Watched != 30 || got.EpCount != 30 {
		t.Fatalf("reloaded item mismatch: ok=%v %+v", ok, got)
	}
}

func TestLibraryMalAdoptionAndFilmGuard(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	lib := OpenLibrary()
	// Item synced from MAL before opening on any source.
	lib.Upsert(Item{
		Source:  "mal",
		AnimeID: "52991",
		Title:   "Sousou no Frieren",
		Kind:    "TV",
		MalID:   52991,
		Status:  Watching,
		Score:   9,
		Watched: 12,
		Lists:   []string{"favoriler"},
	})

	// Opening on AnimeciX with same MAL ID and matching TV kind must adopt it.
	adopted := lib.Set(source.Anime{Source: "animecix", ID: "100", Title: "Frieren", Kind: "TV", MalID: 52991}, nil)
	if adopted.Key() != "animecix:100" || adopted.Score != 9 || adopted.Watched != 12 {
		t.Fatalf("adoption failed: %+v", adopted)
	}
	if _, stillOld := lib.Get("mal:52991"); stillOld {
		t.Fatal("old mal:52991 key should be replaced on adoption")
	}
	if len(lib.All()) != 1 {
		t.Fatalf("expected 1 item after adoption, got %d", len(lib.All()))
	}

	// Film sharing the series' MAL ID must NOT overwrite the TV series item.
	film := source.Anime{Source: "animecix", ID: "999", Title: "Frieren Film", Kind: "Film", MalID: 52991}
	lib.Set(film, func(x *Item) { x.Status = Planned })
	if len(lib.All()) != 2 {
		t.Fatalf("film must not adopt TV series item; expected 2 items, got %d", len(lib.All()))
	}
	if _, ok := lib.Get("animecix:100"); !ok {
		t.Fatal("TV series item was overwritten by film")
	}
}

func TestLibraryRemoveRestoreAndMalDeleted(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	lib := OpenLibrary()
	an := source.Anime{Source: "animecix", ID: "5", Title: "Mob Psycho", MalID: 32182}
	lib.Set(an, func(x *Item) {
		x.Status = Watching
		x.MalSeen = time.Now()
	})

	snap, _ := lib.Get(an.Key())
	lib.Remove(an.Key())
	if _, ok := lib.Get(an.Key()); ok {
		t.Fatal("item should be removed")
	}
	if len(lib.MalDeleted) != 1 || lib.MalDeleted[0] != 32182 {
		t.Fatalf("expected MAL id queued for delete, got %v", lib.MalDeleted)
	}

	// Restore cancels queued MAL delete and re-marks MalDirty.
	lib.Restore(snap)
	restored, ok := lib.Get(an.Key())
	if !ok || !restored.MalDirty {
		t.Fatalf("restore mismatch: ok=%v %+v", ok, restored)
	}
	if len(lib.MalDeleted) != 0 {
		t.Fatalf("Restore must cancel queued MAL delete, got %v", lib.MalDeleted)
	}

	// TakeDeleted & RequeueDeleted round-trip.
	lib.Remove(an.Key())
	taken := lib.TakeDeleted()
	if len(taken) != 1 || taken[0] != 32182 || len(lib.MalDeleted) != 0 {
		t.Fatalf("TakeDeleted failed: taken=%v remaining=%v", taken, lib.MalDeleted)
	}
	lib.RequeueDeleted(taken)
	if len(lib.MalDeleted) != 1 || lib.MalDeleted[0] != 32182 {
		t.Fatalf("RequeueDeleted failed: %v", lib.MalDeleted)
	}

	// Drop removes without queueing MAL delete.
	an2 := source.Anime{Source: "animecix", ID: "6", Title: "DroppedOnMAL", MalID: 777}
	lib.Set(an2, func(x *Item) { x.MalSeen = time.Now() })
	lib.Drop(an2.Key())
	for _, id := range lib.MalDeleted {
		if id == 777 {
			t.Fatal("Drop must not queue a MAL delete")
		}
	}
}

func TestLibraryCustomLists(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	lib := OpenLibrary()
	if lib.AddList("   ") {
		t.Fatal("blank list name must be rejected")
	}
	if lib.AddList("FAVORİLER") && lib.AddList("favoriler") {
		t.Fatal("duplicate default list should be rejected")
	}
	if !lib.AddList("hafta sonu") {
		t.Fatal("expected AddList to succeed")
	}
	if lib.AddList("HAFTA SONU") {
		t.Fatal("case-insensitive duplicate list must be rejected")
	}

	an := source.Anime{Source: "animecix", ID: "7", Title: "Vinland Saga"}
	if !lib.ToggleList(an, "hafta sonu") {
		t.Fatal("first ToggleList should add")
	}
	if !lib.RenameList("hafta sonu", "izlenecekler") {
		t.Fatal("RenameList failed")
	}
	it, _ := lib.Get(an.Key())
	if !it.InList("izlenecekler") || it.InList("hafta sonu") {
		t.Fatalf("item lists not updated after rename: %v", it.Lists)
	}

	lib.DeleteList("izlenecekler")
	it, ok := lib.Get(an.Key())
	if !ok {
		t.Fatal("DeleteList must keep items in the library")
	}
	if it.InList("izlenecekler") {
		t.Fatalf("deleted list still present on item: %v", it.Lists)
	}
}

func TestLibraryCorruptBacksUp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)

	raw := `{"items": CORRUPT`
	p := filepath.Join(home, "library.json")
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	lib := OpenLibrary()
	if len(lib.Items) != 0 {
		t.Fatalf("expected empty library after corrupt file, got %d", len(lib.Items))
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), "library.json.bozuk-") {
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
		t.Fatal("corrupt library.json must be backed up to library.json.bozuk-*")
	}
}
