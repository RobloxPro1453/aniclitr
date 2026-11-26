package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func TestV2PaletteQueryAndEpisodeScoreSuggestion(t *testing.T) {
	a := showcaseApp(t, 120, 36)
	an := source.Anime{Source: "animecix", ID: "501", Title: "Monster", Year: 2004, Genres: []string{"Psikolojik", "Gerilim"}, EpCount: 74, MalID: 19}
	a.libSet(an, func(x *store.Item) {
		x.Status = store.Completed
		x.Score = 10
		x.Year = 2004
		x.Genres = []string{"Psikolojik", "Gerilim"}
		x.EpCount = 74
	})

	// ctrl+k 2.0 structured query engine
	a.openPalette("puan>=9 durum:bitti tur:psikolojik")
	foundMonster := false
	for _, h := range a.pal.hits {
		if h.it.label == "Monster" && h.it.group == "sorgu" {
			foundMonster = true
		}
	}
	if !foundMonster {
		t.Fatalf("ctrl+k 2.0 structured query missed Monster; hits=%d", len(a.pal.hits))
	}
	a.pal = nil

	// Episode ratings -> automatic series average suggestion -> G applies score
	a.hist.SetEpisodeScore(an, 1, 1, 8)
	a.hist.SetEpisodeScore(an, 1, 2, 9)
	a.hist.SetEpisodeScore(an, 1, 3, 9)
	sc := newAnime(an, nil, 1, 1)
	a.episodes[epKey(an, 1)] = []source.Episode{
		{ID: "1", Season: 1, Number: 1, Title: "Bölüm 1"},
		{ID: "2", Season: 1, Number: 2, Title: "Bölüm 2"},
		{ID: "3", Season: 1, Number: 3, Title: "Bölüm 3"},
	}
	a.push(sc)
	sc.update(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if it, ok := a.libItem(an); !ok || it.Score != 9 {
		t.Fatalf("G should apply episode average score (9) to library, got %+v (ok=%v)", it, ok)
	}

	// Library Stats renders 365-day heatmap & Taste DNA
	lib := newLibrary()
	lib.selectShelf(a, "stats")
	view := ansi.Strip(lib.view(a, 120, 48))
	if !strings.Contains(view, "ZEVK DNA") && !strings.Contains(view, "ISI HARİTASI") {
		t.Fatalf("library stats missing Taste DNA / 365-day heatmap:\n%s", view)
	}
}
