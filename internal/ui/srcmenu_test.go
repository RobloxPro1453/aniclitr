package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/source"
)

// s opens a centred picker; a digit jumps straight to that source, without
// stepping through (and loading) the ones in between.
func TestSourcePicker(t *testing.T) {
	a := testApp(t, Graphics{})
	a.cfg.Sources = map[string]bool{"animecix": true, "anihub": true, "animetr": true, "animim": true, "sonanime": true, "hianime": true}
	mk := func(src string) source.Anime { return source.Anime{Source: src, ID: "x", Title: "Re:Zero"} }
	a.w, a.h = 120, 34
	s := newAnime(mk("animecix"), []source.Anime{mk("anihub"), mk("animetr"), mk("animim")}, 0, 0)
	a.push(s)
	s.start(a)
	s.update(a, key("s"))
	if a.drop == nil || a.drop.kind != "sources" || !a.drop.center {
		t.Fatal("s should open the centred source picker")
	}
	items := s.sourceItems(a)
	if !strings.HasPrefix(items[0].label, "1  AnimeciX") || items[0].note != "açık" {
		t.Fatalf("open source first and marked: %+v", items[0])
	}
	// sources not known yet are being searched, shown as such
	var searching bool
	for _, it := range items {
		searching = searching || (strings.Contains(it.label, "SonAnime") && strings.Contains(it.note, "aranıyor"))
	}
	if !searching {
		t.Fatalf("unknown sources should show the search: %+v", items)
	}
	out := ansi.Strip(a.View())
	if !strings.Contains(out, "kaynak seç") || !strings.Contains(out, "1-9") {
		t.Fatalf("picker not drawn:\n%s", out)
	}
	// 4 → Animim directly, AniHub and AnimeTR untouched
	a.drop.update(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if s.base.Source != "animim" || a.drop != nil {
		t.Fatalf("digit should switch to Animim, got %s", s.base.Source)
	}
	if !strings.Contains(ansi.Strip(strings.Join(s.sourceLine(a, 100), "")), "Animim") {
		t.Fatal("the summary line should name the open source")
	}
}
