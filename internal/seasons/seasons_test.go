package seasons

import (
	"context"
	"testing"

	"aniclitr/internal/anilist"
)

func TestBuildWalksPrequelAndSequelSkippingMovies(t *testing.T) {
	graph := map[int]struct {
		m     anilist.Media
		edges []anilist.RelationEdge
	}{
		100: {
			m: anilist.Media{ID: 100, IDMal: 1000, Format: "TV", Episodes: 12, Title: anilist.Titles{Romaji: "Show S1"}},
			edges: []anilist.RelationEdge{
				{RelationType: "SEQUEL", Media: anilist.Media{ID: 150, IDMal: 1500, Format: "MOVIE", Episodes: 1}},
				{RelationType: "SEQUEL", Media: anilist.Media{ID: 200, IDMal: 2000, Format: "TV", Episodes: 12}},
			},
		},
		200: {
			m: anilist.Media{ID: 200, IDMal: 2000, Format: "TV", Episodes: 12, Title: anilist.Titles{Romaji: "Show S2"}},
			edges: []anilist.RelationEdge{
				{RelationType: "PREQUEL", Media: anilist.Media{ID: 100, IDMal: 1000, Format: "TV", Episodes: 12}},
				{RelationType: "SEQUEL", Media: anilist.Media{ID: 300, IDMal: 3000, Format: "TV", Episodes: 13}},
			},
		},
		300: {
			m: anilist.Media{ID: 300, IDMal: 3000, Format: "TV", Episodes: 13, Title: anilist.Titles{Romaji: "Show S3"}},
			edges: []anilist.RelationEdge{
				{RelationType: "PREQUEL", Media: anilist.Media{ID: 200, IDMal: 2000, Format: "TV", Episodes: 12}},
			},
		},
	}

	// Start from the middle season (200) and verify it walks back to 100 and forward to 300.
	ch, err := BuildWithFetcher(context.Background(), 200, func(ctx context.Context, id int) (anilist.Media, []anilist.RelationEdge, error) {
		n := graph[id]
		return n.m, n.edges, nil
	})
	if err != nil {
		t.Fatalf("BuildWithFetcher failed: %v", err)
	}
	if ch.Root != 100 || len(ch.Entries) != 3 {
		t.Fatalf("unexpected chain: %+v", ch)
	}
	if ch.Entries[0].Season != 1 || ch.Entries[1].Season != 2 || ch.Entries[2].Season != 3 {
		t.Fatalf("expected seasons 1,2,3; got %+v", ch.Entries)
	}
}

func TestAlignPerSeasonAndAbsoluteAndOverflow(t *testing.T) {
	ch := Chain{
		Root: 100,
		Entries: []Entry{
			{Season: 1, MalID: 1000, AniListID: 100, Episodes: 12, Format: "TV"},
			{Season: 2, MalID: 2000, AniListID: 200, Episodes: 12, Format: "TV"},
			{Season: 3, MalID: 3000, AniListID: 300, Episodes: 13, Format: "TV"},
		},
	}

	// 1. PerSeason: AnimeciX style (3 distinct seasons)
	alPer := Align(map[int]int{1: 12, 2: 12, 3: 13}, ch)
	if alPer.Mode != PerSeason {
		t.Fatalf("expected PerSeason mode, got %v", alPer.Mode)
	}
	c, ok := alPer.ToCanonical(2, 5)
	if !ok || c.Season != 2 || c.Episode != 5 || c.MalID != 2000 || c.Overflow {
		t.Fatalf("PerSeason ToCanonical(2, 5) = %+v (ok=%v)", c, ok)
	}

	// 2. Absolute: single provider list of 37 episodes spanning all 3 seasons
	alAbs := Align(map[int]int{1: 37}, ch)
	if alAbs.Mode != Absolute || len(alAbs.Slices) != 3 {
		t.Fatalf("expected Absolute mode with 3 slices, got mode=%v slices=%+v", alAbs.Mode, alAbs.Slices)
	}
	// Episode 25 in absolute list -> Season 3, Episode 1, MalID 3000
	c25, ok := alAbs.ToCanonical(1, 25)
	if !ok || c25.Season != 3 || c25.Episode != 1 || c25.MalID != 3000 || c25.Overflow {
		t.Fatalf("Absolute ToCanonical(1, 25) = %+v (ok=%v)", c25, ok)
	}
	// Episode 39 (past 37 known episodes -> Overflow)
	c39, ok := alAbs.ToCanonical(1, 39)
	if !ok || c39.Season != 3 || c39.Episode != 15 || !c39.Overflow {
		t.Fatalf("expected Overflow on episode 39, got %+v (ok=%v)", c39, ok)
	}

	// 3. Reverse mapping FromCanonical: 5 episodes watched in MAL ID 2000 -> provider ep 17 in Absolute mode
	ps, upTo, ok := alAbs.FromCanonical(2000, 5)
	if !ok || ps != 1 || upTo != 17 {
		t.Fatalf("FromCanonical(2000, 5) = (ps=%d, upTo=%d, ok=%v), want (1, 17, true)", ps, upTo, ok)
	}
}

func TestAlignWarnsOnTVToMovieFormatMismatch(t *testing.T) {
	movieChain := Chain{
		Root: 999,
		Entries: []Entry{
			{Season: 1, MalID: 37055, AniListID: 999, Episodes: 1, Format: "MOVIE"},
		},
	}
	al := AlignTitle("Youjo Senki", "TV", map[int]int{1: 12}, movieChain)
	if len(al.Warnings) == 0 {
		t.Fatalf("expected format mismatch warning when aligning TV series to MOVIE chain")
	}
}
