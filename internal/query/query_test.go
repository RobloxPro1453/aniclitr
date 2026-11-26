package query

import (
	"testing"

	"aniclitr/internal/store"
)

func TestUniversalQueryEngine(t *testing.T) {
	recs := []Record{
		{
			Item:     store.Item{Title: "Monster", Status: store.Completed, Score: 10, Year: 2004, EpCount: 74, Genres: []string{"Psikolojik", "Gerilim", "Dram"}, MalID: 19},
			Studios:  []string{"Madhouse"},
			HasLocal: true,
		},
		{
			Item:        store.Item{Title: "Sousou no Frieren", Status: store.Watching, Score: 9, Year: 2023, EpCount: 28, Genres: []string{"Macera", "Fantastik", "Dram"}, MalID: 52991},
			Studios:     []string{"Madhouse"},
			HasLocal:    true,
			MissingDisk: true,
		},
		{
			Item:    store.Item{Title: "K-On!", Status: store.Dropped, Score: 6, Year: 2009, EpCount: 13, Genres: []string{"Komedi", "Müzik"}},
			Studios: []string{"Kyoto Animation"},
		},
	}

	cases := []struct {
		q    string
		want []string
	}{
		{"puan>=8 durum:bitti tur:psikolojik", []string{"Monster"}},
		{"yıl:2000..2012 stüdyo:madhouse", []string{"Monster"}},
		{"stüdyo:madhouse -durum:bitti", []string{"Sousou no Frieren"}},
		{"eksik:disk", []string{"Sousou no Frieren"}},
		{"disk:hazir puan>=9", []string{"Monster", "Sousou no Frieren"}},
		{"bolum<=13", []string{"K-On!"}},
	}

	for _, tc := range cases {
		p := Parse(tc.q)
		var got []string
		for _, r := range recs {
			if p.Match(r) {
				got = append(got, r.Item.Title)
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("query %q: got %v, want %v", tc.q, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("query %q[%d]: got %q, want %q", tc.q, i, got[i], tc.want[i])
			}
		}
	}
}
