package match

import (
	"path/filepath"
	"testing"

	"aniclitr/internal/source"
)

func TestNormalizeAndSplitSeason(t *testing.T) {
	cases := []struct {
		in   string
		norm string
		base string
		s    int
	}{
		{"Shingeki no Kyojin 2. Sezon Türkçe İzle", "shingeki no kyojin 2 sezon", "shingeki no kyojin", 2},
		{"Attack on Titan Season 3 Part 2", "attack on titan season 3 part 2", "attack on titan part 2", 3},
		{"Bleach: Sennen Kessen-hen - Kısım 2 (TV)", "bleach sennen kessen hen kisim 2", "bleach sennen kessen hen", 2},
		{"Overlord III (2018)", "overlord iii", "overlord", 3},
		{"Jujutsu Kaisen S2", "jujutsu kaisen s2", "jujutsu kaisen", 2},
		{"Another (TV) İzle", "another", "another", 1},
	}
	for _, tc := range cases {
		gotNorm := Normalize(tc.in)
		if gotNorm != tc.norm {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, gotNorm, tc.norm)
		}
		gotBase, gotS := SplitSeason(tc.in)
		if gotBase != tc.base || gotS != tc.s {
			t.Errorf("SplitSeason(%q) = (%q, %d), want (%q, %d)", tc.in, gotBase, gotS, tc.base, tc.s)
		}
	}
}

func TestThreeTimesAnotherMerging(t *testing.T) {
	a1 := source.Anime{Source: "animecix", ID: "101", Title: "Another", Kind: "TV", Year: 2012}
	a2 := source.Anime{Source: "animetr", ID: "another-izle", Title: "Another (TV) İzle", Kind: "TV", Year: 2012}
	a3 := source.Anime{Source: "hianime", ID: "another-456", Title: "Another", AltTitle: "アナザー", Kind: "TV", Year: 2012}
	ova := source.Anime{Source: "animecix", ID: "102", Title: "Another: The Other - Inga", Kind: "OVA", Year: 2012}

	if !SameAnime(a1, a2) {
		t.Fatalf("expected %q and %q to merge as SameAnime", a1.Title, a2.Title)
	}
	if !SameAnime(a1, a3) {
		t.Fatalf("expected %q and %q to merge as SameAnime", a1.Title, a3.Title)
	}
	if SameAnime(a1, ova) {
		t.Fatalf("expected %q and OVA %q NOT to merge", a1.Title, ova.Title)
	}
}

func TestYoujoSenkiTVvsMovieSeparation(t *testing.T) {
	// Known real-world bug: AnimeciX tags Youjo Senki TV with the Movie's MAL ID (37055).
	tvWithWrongMal := source.Anime{
		Source: "animecix", ID: "7822",
		Title: "Youjo Senki", AltTitle: "Saga of Tanya the Evil",
		Kind: "TV", MalID: 37055, Year: 2017, EpCount: 12,
	}
	movieEntry := source.Anime{
		Source: "animetr", ID: "youjo-senki-movie",
		Title: "Youjo Senki Movie",
		Kind: "Film", MalID: 37055, Year: 2019, EpCount: 1,
	}
	if SameAnime(tvWithWrongMal, movieEntry) {
		t.Fatalf("TV series and Movie must not merge even when sharing mis-tagged MAL ID 37055")
	}

	// Scoring against AniList candidates when searching for TV series:
	want := Want{
		Titles: []string{"Youjo Senki", "Saga of Tanya the Evil"},
		Season: 1,
		Year:   2017,
		Kind:   "TV",
		Eps:    12,
	}
	cands := []Candidate{
		{
			Titles: []string{"Youjo Senki Movie", "Saga of Tanya the Evil: The Movie"},
			Year:   2019,
			Kind:   "Film",
			Eps:    1,
			MalID:  37055,
		},
		{
			Titles: []string{"Youjo Senki", "Saga of Tanya the Evil"},
			Year:   2017,
			Kind:   "TV",
			Eps:    12,
			MalID:  32615,
		},
	}
	var scores []Scored
	for i, c := range cands {
		scores = append(scores, Scored{Index: i, Score: ScoreCandidate(c, want), Eps: c.Eps, Year: c.Year})
	}
	idx, ok, amb := Pick(scores)
	if !ok || amb || idx != 1 {
		t.Fatalf("expected TV candidate (index 1, MAL 32615) to win cleanly, got idx=%d ok=%v amb=%v scores=%+v", idx, ok, amb, scores)
	}
}

func TestMappingStorePersistenceAndUserLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mappings.json")
	st := LoadFrom(path)

	st.SetAuto("animecix:7822", 32615, 21613, "Youjo Senki", 0.95)
	if e, ok := st.Get("animecix:7822"); !ok || e.MalID != 32615 || e.By != "auto" {
		t.Fatalf("unexpected auto entry: %+v (ok=%v)", e, ok)
	}

	// User overrides mapping
	st.SetUser("animecix:7822", 99999, 88888, "Manual Override")
	// Subsequent auto update must NOT overwrite user mapping
	st.SetAuto("animecix:7822", 32615, 21613, "Youjo Senki", 0.99)

	// Reload from disk to verify atomic persistence
	st2 := LoadFrom(path)
	e, ok := st2.Get("animecix:7822")
	if !ok || e.MalID != 99999 || e.By != "user" {
		t.Fatalf("user mapping was not preserved across reload/auto-set: %+v", e)
	}

	byMal := st2.ByMal(99999)
	if hit, ok := byMal["animecix"]; !ok || hit.AnimeID != "7822" {
		t.Fatalf("ByMal lookup failed: %+v", byMal)
	}
}
