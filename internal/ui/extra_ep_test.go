package ui

import (
	"testing"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// A 12.5 never counts as progress: MAL and the shelf only see 12 and 13.
func TestInBetweenEpisodeIsNotProgress(t *testing.T) {
	a := testApp(t, Graphics{})
	an := source.Anime{Source: "animecix", ID: "5", Title: "X", Kind: "TV", MalID: 77}
	half := source.Episode{Season: 1, Number: source.ExtraNumber(12.5)}
	a.libSet(an, func(x *store.Item) { x.Status, x.Watched = store.Watching, 12 })

	bumpWatched(a, an, half)
	if it, _ := a.libItem(an); it.Watched != 12 {
		t.Fatalf("12.5 changed the watched count to %d", it.Watched)
	}
	// it is the list's last row, but not the finale
	eps := []source.Episode{{Season: 1, Number: 12}, half}
	newPlaying(an, half, eps, "", "", &source.Stream{}, source.Quality{}).libraryProgress(a)
	if it, _ := a.libItem(an); it.Status == store.Completed {
		t.Fatal("watching 12.5 completed the show")
	}
	newPlaying(an, eps[0], eps, "", "", &source.Stream{}, source.Quality{}).libraryProgress(a)
	if it, _ := a.libItem(an); it.Status != store.Completed {
		t.Fatalf("episode 12 is the finale even with 12.5 after it: %s", it.Status)
	}
	if got := epCell(half, 2); got != "12,5" {
		t.Fatalf("list cell %q", got)
	}
	if got := source.NextNumber(half.Number); got != 13 {
		t.Fatalf("after 12.5 comes %d", got)
	}
}
