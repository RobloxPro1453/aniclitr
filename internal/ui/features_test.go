package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// Opening an anime tints the UI with its cover; leaving restores the theme.
func TestCoverTheme(t *testing.T) {
	a := showcaseApp(t, 140, 44)
	a.cfg.CoverTheme, a.cfg.Motion = true, false
	base := cAccent
	an := source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren", Poster: "test://p0"}
	sc := newAnime(an, nil, 0, 0)
	a.stack = append(a.stack, sc)
	a.Update(tickMsg(time.Now()))
	want := a.posters.items["test://p0"].accent.hex()
	if cAccent != want {
		t.Fatalf("accent %s, want cover colour %s", cAccent, want)
	}
	a.pop()
	a.Update(tickMsg(time.Now()))
	if cAccent != base {
		t.Fatalf("leaving should restore %s, got %s", base, cAccent)
	}
}

// N writes a note on the selected episode; it shows under the list.
func TestEpisodeNote(t *testing.T) {
	a := showcaseApp(t, 160, 44)
	an := source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren", Poster: "test://p0"}
	a.details[an.Key()] = an
	sc := newAnime(an, nil, 0, 0)
	a.stack = append(a.stack, sc)
	sc.ensure(a)
	a.episodes[epKey(sc.an, 1)] = []source.Episode{{ID: "1", Season: 1, Number: 1}, {ID: "2", Season: 1, Number: 2}}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	for _, r := range "efsane final" {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	a.Update(key("enter"))
	e, _ := a.hist.Get(an.Key())
	if e.Notes["1:1"] != "efsane final" {
		t.Fatalf("note not saved: %v", e.Notes)
	}
	if !strings.Contains(ansiRe.ReplaceAllString(a.View(), ""), "efsane final") {
		t.Fatal("note not shown")
	}
}

// An item that only exists on the MAL list is found on the sources first.
func TestOpenMalOnlyItem(t *testing.T) {
	a := showcaseApp(t, 140, 44)
	a.lib.Upsert(store.Item{Source: "mal", AnimeID: "1", MalID: 1, Title: "Cowboy Bebop", Status: store.Planned})
	it, _ := a.lib.FindMal(1)
	cmd := a.openItem(it)
	if cmd == nil {
		t.Fatal("expected a search command")
	}
	// simulate the search result arriving
	a.Update(malOpenMsg{an: source.Anime{Source: "animecix", ID: "42", Title: "Cowboy Bebop", MalID: 1}})
	sc, ok := a.top().(*animeScreen)
	if !ok || sc.an.ID != "42" {
		t.Fatalf("anime screen not opened: %T", a.top())
	}
}

// Header buttons: the bell collects notices, the burger lists the screens.
func TestBellAndMenu(t *testing.T) {
	a := showcaseApp(t, 180, 50)
	if len(a.notices) == 0 {
		t.Fatal("the showcase feed should have produced new-episode notices")
	}
	a.notices = nil
	a.addNotice(notice{key: "ep:1", kind: "new", text: "Bleach · S2 50. bölüm çıktı", action: func(a *App) tea.Cmd { return a.openDiscover() }})
	a.addNotice(notice{key: "dl:1", kind: "ok", text: "indirildi: Sousou no Frieren S1·B5"})
	if a.unread() != 2 {
		t.Fatalf("unread %d", a.unread())
	}
	if !strings.Contains(ansiRe.ReplaceAllString(a.header(), ""), "◆ 2") {
		t.Fatal("bell count missing from header")
	}
	if dir := os.Getenv("ANICLITR_SNAP_DIR"); dir != "" {
		time.Sleep(600 * time.Millisecond)
		snap(t, a, "home-clean")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyF9})
	if a.drop == nil || a.drop.kind != "bell" {
		t.Fatal("bell panel did not open")
	}
	a.drop.openT = tween{}
	if os.Getenv("ANICLITR_SNAP_DIR") != "" {
		snap(t, a, "bell")
	}
	a.Update(key("down"))
	a.Update(key("enter")) // runs the episode notice's action
	if _, ok := a.top().(*discover); !ok || a.unread() != 0 {
		t.Fatalf("notice action / read marking failed: %T unread=%d", a.top(), a.unread())
	}
	a.stack = a.stack[:1]
	a.Update(tea.KeyMsg{Type: tea.KeyF10})
	a.drop.openT = tween{}
	if os.Getenv("ANICLITR_SNAP_DIR") != "" {
		snap(t, a, "menu")
	}
	a.Update(key("down"))
	a.Update(key("enter")) // Kitaplık
	if _, ok := a.top().(*library); !ok {
		t.Fatalf("menu did not open the library: %T", a.top())
	}
}
