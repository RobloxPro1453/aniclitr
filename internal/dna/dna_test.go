package dna

import (
	"testing"

	"aniclitr/internal/store"
)

func TestTasteDNABuildAndCandidateMatch(t *testing.T) {
	items := []store.Item{
		{Title: "Monster", Status: store.Completed, Score: 10, Year: 2004, Genres: []string{"Psikolojik", "Gerilim"}, MalID: 19},
		{Title: "Death Note", Status: store.Completed, Score: 9, Year: 2006, Genres: []string{"Psikolojik", "Gizem"}, MalID: 1535},
		{Title: "Paranoia Agent", Status: store.Completed, Score: 9, Year: 2004, Genres: []string{"Psikolojik", "Dram"}, MalID: 323},
		{Title: "Random Isekai", Status: store.Dropped, Score: 4, Year: 2022, Genres: []string{"Komedi"}, MalID: 999},
	}
	credits := map[int]StaffCredit{
		19:   {MalID: 19, Studios: []string{"Madhouse"}, Directors: []string{"Masayuki Kojima"}, Seiyuu: []string{"Hidenobu Kiuchi"}},
		1535: {MalID: 1535, Studios: []string{"Madhouse"}, Directors: []string{"Tetsuro Araki"}, Seiyuu: []string{"Mamoru Miyano"}},
		323:  {MalID: 323, Studios: []string{"Madhouse"}, Directors: []string{"Satoshi Kon"}},
	}

	prof := Build(items, credits)
	if prof.TopGenres[0].Name != "Psikolojik" {
		t.Fatalf("top genre = %+v, want Psikolojik", prof.TopGenres)
	}
	if prof.TopStudios[0].Name != "Madhouse" {
		t.Fatalf("top studio = %+v, want Madhouse", prof.TopStudios)
	}
	if prof.TopDecades[0].Name != "2000'ler" {
		t.Fatalf("top decade = %+v, want 2000'ler", prof.TopDecades)
	}

	highMatch := prof.MatchCandidate([]string{"Psikolojik", "Gerilim"}, 2005, []string{"Madhouse"})
	lowMatch := prof.MatchCandidate([]string{"Spor"}, 2024, []string{"Unknown Studio"})
	if highMatch <= lowMatch+25 {
		t.Fatalf("expected highMatch (%d) >> lowMatch (%d)", highMatch, lowMatch)
	}
}
