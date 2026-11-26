package wrapped

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func TestBuildPersonaAndExportPNG(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	h := store.Open()
	h.Update(source.Anime{Source: "animecix", ID: "1", Title: "Sousou no Frieren"}, func(e *store.Entry) {
		e.Season, e.Episode = 1, 28
		e.Watched[store.EpKey(1, 28)] = true
	})
	lib := store.OpenLibrary()
	lib.Set(source.Anime{Source: "animecix", ID: "1", Title: "Sousou no Frieren", Genres: []string{"Macera", "Dram", "Fantastik"}}, func(it *store.Item) {
		it.Status = store.Completed
		it.Score = 10
		it.Watched = 28
	})
	lib.Set(source.Anime{Source: "animecix", ID: "2", Title: "Chainsaw Man", Genres: []string{"Aksiyon", "Fantastik"}}, func(it *store.Item) {
		it.Status = store.Completed
		it.Score = 9
		it.Watched = 12
	})

	rep := Build(h, lib, time.Now())
	if rep.TotalEpisodes < 40 || rep.Archetype == "" || len(rep.PersonaStory) == 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if !strings.Contains(rep.ShowSentence, "Sousou no Frieren") {
		t.Fatalf("expected top show in sentence, got %q", rep.ShowSentence)
	}

	out := filepath.Join(t.TempDir(), "wrapped.png")
	got, err := ExportPNG(rep, out)
	if err != nil {
		t.Fatalf("ExportPNG failed: %v", err)
	}
	st, err := os.Stat(got)
	if err != nil || st.Size() < 5000 {
		t.Fatalf("expected valid PNG > 5KB, got err=%v size=%d", err, st.Size())
	}
}
