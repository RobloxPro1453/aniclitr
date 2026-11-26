package ui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
	"aniclitr/internal/source"
)

func TestSettingsZeroStickyCursorAndDeferredTick(t *testing.T) {
	a := testApp(t, Graphics{})
	a.cfg.Motion = true
	var buf bytes.Buffer
	a.selfPaintSync(&buf)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})

	st := newSettings()
	a.push(st)

	// Single-step arrow key presses must land on s.cur immediately with zero lag
	// or highlight sticking (band == s.cur on every frame).
	for step := 1; step <= 12; step++ {
		_, _ = a.Update(tea.KeyMsg{Type: tea.KeyDown})
		if st.cur != step {
			t.Fatalf("step %d: expected cur=%d, got %d", step, step, st.cur)
		}
		if int(st.gl.pos+0.5) != st.cur {
			t.Fatalf("step %d: highlight stuck at %.2f while cur=%d", step, st.gl.pos, st.cur)
		}
	}

	// Multi-row jump (PgUp) starts a glide inside paint(); Update's defer must
	// attach a tick command when a.gliding becomes true.
	a.ticking = false
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if st.cur != 4 {
		t.Fatalf("expected cur=4 after pgup, got %d", st.cur)
	}
	if a.gliding && cmd == nil {
		t.Fatalf("a.gliding became true during paint() but Update returned nil cmd (highlight would freeze)")
	}
}

func TestAniListCatalogSearchAndMultiSourceResolution(t *testing.T) {
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})

	r := newResults("frieren")
	if !r.catalogMode {
		t.Fatalf("results must default to source-agnostic AniList catalog mode")
	}
	a.push(r)

	// Simulate AniList catalog response (independent of streaming providers).
	r.update(a, catalogSearchMsg{
		gen: r.gen,
		items: []anilist.Media{
			{
				ID:          154587,
				IDMal:       52991,
				Title:       anilist.Titles{Romaji: "Sousou no Frieren", English: "Frieren: Beyond Journey's End"},
				Format:      "TV",
				Episodes:    28,
				SeasonYear:  2023,
				Score:       91,
				Genres:      []string{"Adventure", "Drama", "Fantasy"},
				Description: "Elf büyücü Frieren'in yolculuğu.",
			},
		},
	})
	if len(r.items) != 1 {
		t.Fatalf("expected 1 catalog item, got %d", len(r.items))
	}
	if r.items[0].main.Source != "anilist" || r.items[0].main.MalID != 52991 {
		t.Fatalf("unexpected catalog entry: %+v", r.items[0].main)
	}
	view := r.view(a, 120, 33)
	if !strings.Contains(view, "AniList Kataloğu") || !strings.Contains(view, "Sousou no Frieren") {
		t.Fatalf("catalog search view missing expected AniList header/title")
	}

	// Pressing Enter opens animeScreen immediately in catalog lookup mode.
	sc := newAnime(r.items[0].main, nil, 0, 0)
	a.push(sc)
	_ = sc.start(a)
	if !sc.catalog || !sc.looking {
		t.Fatalf("expected animeScreen to enter catalog parallel source lookup")
	}

	// Parallel source resolution returns 2 streaming providers.
	sc.update(a, altsMsg{
		key: sc.lookKey,
		alts: []source.Anime{
			{Source: "animetr", ID: "atr-1", Title: "Sousou no Frieren"},
			{Source: "animecix", ID: "acx-1", Title: "Sousou no Frieren", MalID: 52991},
		},
	})
	if sc.catalog || sc.catalogEmpty {
		t.Fatalf("catalog lookup should complete cleanly, got catalog=%v empty=%v", sc.catalog, sc.catalogEmpty)
	}
	if len(sc.srcs) != 2 {
		t.Fatalf("expected 2 matched streaming sources, got %d", len(sc.srcs))
	}
	// Catalog metadata (synopsis, genres) must be preserved on the active source.
	if sc.an.Description == "" || len(sc.an.Genres) == 0 {
		t.Fatalf("expected AniList catalog metadata preserved after source binding: %+v", sc.an)
	}
	// Source dropdown menu must list matched sources + Nyaa.si torrent fallback.
	items := sc.sourceItems(a)
	hasTorrent := false
	for _, it := range items {
		if strings.Contains(it.label, "Nyaa.si") {
			hasTorrent = true
		}
	}
	if !hasTorrent {
		t.Fatalf("source dropdown should include Nyaa.si Torrent option")
	}

	// Zero-match catalog case must render helpful torrent/retry empty state without panicking.
	sc2 := newAnime(r.items[0].main, nil, 0, 0)
	a.push(sc2)
	_ = sc2.start(a)
	sc2.update(a, altsMsg{key: sc2.lookKey, alts: nil})
	if !sc2.catalogEmpty {
		t.Fatalf("expected catalogEmpty=true when zero streaming sources match")
	}
	emptyView := sc2.view(a, 120, 33)
	if !strings.Contains(emptyView, "bulunamadı") || !strings.Contains(emptyView, "Torrent") {
		t.Fatalf("zero-match catalog view should offer torrent fallback")
	}
}
