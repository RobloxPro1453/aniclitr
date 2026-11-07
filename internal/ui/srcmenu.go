package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
)

// The source picker (s on the anime screen): a centred, launcher-style list
// of every source, so you jump straight to the one you want (1-9 or enter)
// instead of stepping through the others. Sources not known yet for this
// show are searched while the list is open; their rows fill in live.

// openSources opens the picker, starting the search on the other sources
// the first time.
func (s *animeScreen) openSources(a *App) tea.Cmd {
	var look tea.Cmd
	if !s.looking && !s.looked {
		s.looking, s.lookKey = true, s.an.Key()
		look = a.findAlts(s.an)
	}
	a.openList("kaynak seç · "+truncate(s.an.Title, 30), nil)
	a.drop.kind, a.drop.center = "sources", true
	a.drop.items = func(a *App) []dropItem { return s.sourceItems(a) }
	a.drop.cur = s.srcIdx
	if items := s.sourceItems(a); a.drop.cur >= len(items) || items[a.drop.cur].group != "" {
		a.drop.cur = dropNext(items, -1, 1)
	}
	return tea.Batch(a.tick(), look)
}

// sourceItems lists the known sources (in their rank, the open one first
// among them) and then the sources still being searched or without it.
func (s *animeScreen) sourceItems(a *App) []dropItem {
	var items []dropItem
	known := map[string]bool{}
	for i, x := range s.srcs {
		i, x := i, x
		known[x.Source] = true
		p := source.Get(x.Source)
		if p == nil {
			continue
		}
		in := p.Info()
		var note []string
		switch {
		case i == s.srcIdx:
			note = append(note, "açık")
		case !srcOn(a, x):
			note = append(note, "ayarlarda kapalı")
		}
		if in.Lang == "en" {
			note = append(note, "İngilizce")
		}
		if h, ok := a.health[in.ID]; ok && !h.OK {
			note = append(note, "yanıt vermiyor")
		}
		it := dropItem{icon: "●", color: sourceColor(x.Source), label: fmt.Sprintf("%d  %s", i+1, in.Name),
			note: strings.Join(note, " · ")}
		if !srcOn(a, x) {
			it.color = cFaint
		}
		if i == s.srcIdx {
			it.icon = "▶"
			it.run = func(*App) tea.Cmd { return nil } // already here
		} else {
			it.run = func(a *App) tea.Cmd { return s.switchTo(a, i) }
		}
		items = append(items, it)
	}
	var rest []dropItem
	for _, p := range source.All() {
		in := p.Info()
		if known[in.ID] || (in.Legacy && !a.cfg.Legacy) {
			continue
		}
		state := "bu animeyi bulamadı"
		if s.looking {
			state = spinner(a.frame/2) + " aranıyor…"
		}
		rest = append(rest, dropItem{icon: "○", color: cFaint, label: "   " + in.Name, note: state, dim: true})
	}
	if len(rest) > 0 {
		items = append(items, dropItem{group: "diğer kaynaklar"})
		items = append(items, rest...)
	}
	return items
}

// sourceLine is the episode list's one-line source summary (the full list
// lives behind s).
func (s *animeScreen) sourceLine(a *App, w int) []string {
	if len(s.srcs) < 2 && !s.offSrc {
		return nil
	}
	x := s.srcs[s.srcIdx]
	p := source.Get(x.Source)
	if p == nil {
		return nil
	}
	name := p.Info().Name
	dot := fg(sourceColor(x.Source)).Render("●")
	left := dot + " " + stBold.Render(name)
	if !srcOn(a, x) {
		left = stFaint.Render("●") + " " + stBold.Render(name) + stWarn.Render(" · ayarlarda kapalı")
	}
	if n := len(s.srcs); n > 1 {
		left += stMuted.Render(fmt.Sprintf("  ·  %d kaynak", n))
	}
	return []string{row2(left, stText.Bold(true).Render("s")+stMuted.Render(" seç"), w)}
}
