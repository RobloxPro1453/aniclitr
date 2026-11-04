package ui

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// Search-as-you-type: local matches (history, library) appear instantly;
// the primary source is queried once typing pauses for 220 ms. Stale
// answers (older generations) are dropped.

type sugItem struct {
	an    source.Anime
	local string // "geçmiş" / "kitaplık" for local hits, "" for live results
	item  *store.Item
	score int
	pos   map[int]bool
}

type sugFireMsg struct{ gen int }

type sugResMsg struct {
	gen int
	q   string
	res []source.Anime
	err error
}

type suggest struct {
	items []sugItem
	cur   int // -1: the input itself
	gen   int
	q     string
	busy  bool
	shown time.Time
}

func (s *suggest) open() bool { return len(s.items) > 0 || s.busy }

func (s *suggest) close() {
	s.items, s.cur, s.busy, s.q = nil, -1, false, ""
	s.gen++
}

// changed is called after every edit of the search box.
func (s *suggest) changed(a *App, value string) tea.Cmd {
	q := CleanQuery(value)
	s.gen++
	if len([]rune(q)) < 2 {
		s.close()
		return nil
	}
	if !s.open() {
		s.shown = time.Now()
	}
	s.q = q
	s.cur = -1
	s.items = localHits(a, q)
	s.busy = true
	gen := s.gen
	return tea.Tick(220*time.Millisecond, func(time.Time) tea.Msg { return sugFireMsg{gen: gen} })
}

// localHits finds history/library titles matching q.
func localHits(a *App, q string) []sugItem {
	var out []sugItem
	seen := map[string]bool{}
	for _, e := range a.recent() {
		if sc, pos := fuzzy(q, e.Title); sc > 0 {
			out = append(out, sugItem{an: e.Anime(), local: "geçmiş", score: sc + 5, pos: pos})
			seen[fold(e.Title)] = true
		}
	}
	for _, it := range a.lib.All() {
		if seen[fold(it.Title)] {
			continue
		}
		if sc, pos := fuzzy(q, it.Title); sc > 0 {
			it := it
			out = append(out, sugItem{an: it.Anime(), local: "kitaplık", item: &it, score: sc, pos: pos})
			seen[fold(it.Title)] = true
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func (s *suggest) fire(a *App, gen int) tea.Cmd {
	if gen != s.gen || s.q == "" {
		return nil
	}
	q := s.q
	var prov source.Provider
	for _, p := range source.All() {
		if a.cfg.Enabled(p.Info().ID, p.Info().Legacy) {
			prov = p
			break
		}
	}
	if prov == nil {
		s.busy = false
		return nil
	}
	return run(10*time.Second, func(ctx context.Context) tea.Msg {
		res, err := prov.Search(ctx, q)
		return sugResMsg{gen: gen, q: q, res: res, err: err}
	})
}

func (s *suggest) result(m sugResMsg) {
	if m.gen != s.gen {
		return // the user kept typing: this answer is stale
	}
	s.busy = false
	if m.err != nil {
		return
	}
	seen := map[string]bool{}
	for _, it := range s.items {
		seen[fold(it.an.Title)] = true
	}
	n := 0
	for _, r := range m.res {
		if n >= 7 || seen[fold(r.Title)] {
			continue
		}
		_, pos := fuzzy(m.q, r.Title)
		s.items = append(s.items, sugItem{an: r, pos: pos})
		seen[fold(r.Title)] = true
		n++
	}
}

// key handles navigation while the dropdown is open; ok=false passes the
// key on to the search box.
func (s *suggest) key(a *App, h *home, k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "down":
		if len(s.items) > 0 {
			s.cur = min(s.cur+1, len(s.items)-1)
			return nil, true
		}
	case "up":
		if s.cur >= 0 {
			s.cur--
			return nil, true
		}
	case "tab":
		if s.cur >= 0 && s.cur < len(s.items) {
			h.in.Set(s.items[s.cur].an.Title)
			return s.changed(a, h.in.Value()), true
		}
	case "esc":
		s.close()
		return nil, true
	case "enter":
		if s.cur >= 0 && s.cur < len(s.items) {
			it := s.items[s.cur]
			s.close()
			a.flash()
			if it.item != nil {
				return a.openItem(*it.item), true
			}
			sc := newAnime(it.an, nil, 0, 0)
			return tea.Batch(a.push(sc), sc.start(a)), true
		}
		s.close()
	}
	return nil, false
}

// box renders the dropdown (w cells wide).
func (s *suggest) box(a *App, w int) []string {
	inner := w - 2
	bs := fg(mix(hex(cFaint), hex(cAccent), .7).hex())
	var rows []string
	for i, it := range s.items {
		rv := reveal(s.shown, i, a.motion())
		sel := i == s.cur
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		if sel {
			bg, bar, tc = cBand, sp("▌ ", cAccent), cText
		}
		icon := sp("● ", sourceColor(it.an.Source))
		if it.local != "" {
			icon = sp(ic("history")+" ", cAccent2)
		}
		spans := []span{bar, icon}
		for j, r := range []rune(it.an.Title) {
			c := tc
			if it.pos[j] {
				c = cAccent
			}
			spans = append(spans, span{t: string(r), c: c, bold: sel || it.pos[j]})
		}
		var meta []string
		if it.local != "" {
			meta = append(meta, it.local)
		}
		if it.an.Year > 0 {
			meta = append(meta, strconv.Itoa(it.an.Year))
		}
		if it.an.Kind != "" {
			meta = append(meta, it.an.Kind)
		}
		rows = append(rows, bs.Render("│")+row(inner, bg, spans, []span{sp(strings.Join(meta, " · ")+" ", cMuted)})+bs.Render("│"))
	}
	if s.busy {
		rows = append(rows, bs.Render("│")+row(inner, "", []span{sp("  "+spinner(a.frame/2)+" ", cAccent), sp("aranıyor…", cMuted)}, nil)+bs.Render("│"))
	} else if len(s.items) == 0 {
		rows = append(rows, bs.Render("│")+row(inner, "", []span{sp("  eşleşme yok — enter ile tam arama", cMuted)}, nil)+bs.Render("│"))
	}
	foot := stText.Bold(true).Render("↓↑") + stMuted.Render(" seç  ") + stText.Bold(true).Render("enter") + stMuted.Render(" aç  ") + stText.Bold(true).Render("tab") + stMuted.Render(" tamamla  ") + stText.Bold(true).Render("esc") + stMuted.Render(" kapat")
	rows = append(rows, bs.Render("│")+" "+fit(foot, inner-1)+bs.Render("│"))
	return append(rows, bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))
}
