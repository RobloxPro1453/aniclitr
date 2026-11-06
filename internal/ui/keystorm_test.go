package ui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
	"aniclitr/internal/source"
)

func keyFor(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "space", " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// Every screen survives every key it advertises plus a random storm (no
// internal-error toast, the app stays usable). Network is not touched:
// commands returned by Update are not executed.
func TestKeyStorm(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	pool := []string{"enter", "esc", "up", "down", "left", "right", "tab", "space", "pgdown", "delete",
		"a", "b", "d", "D", "f", "g", "G", "j", "k", "m", "n", "o", "p", "r", "s", "t", "u", "v", "w", "x", "y", "N", "[", "]", "+", "-", "1", "4", "6", "0", "/"}
	an := source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren", Poster: "test://p0", Kind: "TV", MalID: 52991}
	makers := map[string]func(a *App) screen{
		"home":      func(a *App) screen { return nil },
		"library":   func(a *App) screen { return newLibrary() },
		"libstats":  func(a *App) screen { l := newLibrary(); l.selectShelf(a, "stats"); return l },
		"discover":  func(a *App) screen { d := newDiscover(); d.schedOK = true; return d },
		"discsec":   func(a *App) screen { d := newDiscover(); d.schedOK = true; d.menu = false; return d },
		"genrepick": func(a *App) screen { d := newDiscover(); d.tab, d.menu, d.gpick = tGenre, false, true; return d },
		"downloads": func(a *App) screen { return newDownloads() },
		"feed":      func(a *App) screen { return newFeedScreen() },
		"settings":  func(a *App) screen { return newSettings() },
		"sources":   func(a *App) screen { return &sourcesScreen{} },
		"characters": func(a *App) screen {
			cs := newCharactersScreen(an)
			cs.loading = false
			c1 := anilist.Character{ID: 1, Role: "MAIN", Description: "Elf büyücü ~!gizli spoiler!~ metni."}
			c1.Name.Full = "Frieren"
			c2 := anilist.Character{ID: 2, Role: "SUPPORTING", Description: "Büyücü."}
			c2.Name.Full = "Fern"
			cs.chars = []anilist.Character{c1, c2}
			return cs
		},
		"anime": func(a *App) screen {
			a.details[an.Key()] = an
			sc := newAnime(an, nil, 0, 0)
			sc.ensure(a)
			var eps []source.Episode
			for i := 1; i <= 12; i++ {
				eps = append(eps, source.Episode{ID: fmt.Sprint(i), Season: 1, Number: i})
			}
			a.episodes[epKey(sc.an, 1)] = eps
			return sc
		},
		"picker": func(a *App) screen {
			eps := []source.Episode{{ID: "1", Season: 1, Number: 1}}
			p := newPicker(an, eps[0], eps, false)
			p.build(a, []source.Variant{{Fansub: "A", Host: "Tau", URL: "u1"}})
			return p
		},
		"playing": func(a *App) screen {
			st := &source.Stream{Qualities: []source.Quality{{Label: "720p", URL: "x"}}}
			p := newPlaying(an, source.Episode{Season: 1, Number: 1}, nil, "A", "Tau", st, st.Qualities[0])
			p.started = true
			return p
		},
	}
	for name, mk := range makers {
		a := showcaseApp(t, 140, 40)
		if s := mk(a); s != nil {
			a.stack = append(a.stack, s)
		}
		var keys []string
		for _, h := range a.top().hints() {
			for _, k := range strings.Fields(strings.NewReplacer("·", " ", "/", " ").Replace(h.key)) {
				if len([]rune(k)) == 1 || k == "enter" || k == "esc" || k == "tab" {
					keys = append(keys, k)
				}
			}
		}
		for i := 0; i < 250; i++ {
			keys = append(keys, pool[r.Intn(len(pool))])
		}
		for i, k := range keys {
			if k == "esc" && len(a.stack) == 1 {
				continue
			}
			a.Update(keyFor(k))
			a.View()
			if strings.Contains(a.toast, "iç hata") {
				t.Fatalf("%s: key %d %q caused an internal error", name, i, k)
			}
			if len(a.stack) == 0 {
				t.Fatalf("%s: stack emptied", name)
			}
		}
	}
}
