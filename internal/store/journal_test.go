package store

import (
	"testing"
	"time"

	"aniclitr/internal/source"
)

func TestEpisodeScoresBookmarksAndHeatmap365(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	h := Open()
	an := source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren", MalID: 52991}

	h.SetEpisodeScore(an, 1, 1, 9)
	h.SetEpisodeScore(an, 1, 2, 8)
	h.SetEpisodeScore(an, 1, 3, 9)
	avg, cnt, sug := h.EpisodeScoreStats(an.Key())
	if cnt != 3 || avg != 8.7 || sug != 9 {
		t.Fatalf("EpisodeScoreStats = avg=%.1f cnt=%d sug=%d, want 8.7 / 3 / 9", avg, cnt, sug)
	}
	wave := h.EpisodeWave(an.Key())
	if len(wave) != 3 || wave[0].Episode != 1 || wave[2].Score != 9 {
		t.Fatalf("EpisodeWave = %+v", wave)
	}

	h.AddBookmark(an, Bookmark{Season: 1, Episode: 3, Pos: 642.5, Note: "Aura sahnesi"})
	e, ok := h.Get(an.Key())
	if !ok || len(e.Bookmarks) != 1 || e.Bookmarks[0].Note != "Aura sahnesi" {
		t.Fatalf("bookmark not saved: %+v", e.Bookmarks)
	}

	now := time.Now()
	h.AddActivity(4000) // ~66 min today -> level 3
	grid := h.Heatmap365(now)
	if len(grid) != 365 || grid[364].Level != 3 {
		t.Fatalf("Heatmap365 len=%d today=%+v", len(grid), grid[364])
	}

	// Reload from disk to verify atomic persistence
	h2 := Open()
	avg2, cnt2, _ := h2.EpisodeScoreStats(an.Key())
	if cnt2 != 3 || avg2 != 8.7 {
		t.Fatalf("persisted EpisodeScoreStats = avg=%.1f cnt=%d", avg2, cnt2)
	}
}
