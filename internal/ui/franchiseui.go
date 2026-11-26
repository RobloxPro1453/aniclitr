package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/media"
	"aniclitr/internal/seasons"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// ---- Watch Order / Franchise Tree Screen (U on anime screen) ----

type franchiseMsg struct {
	malID int
	tree  seasons.FranchiseTree
	err   error
}

type franchiseScreen struct {
	an      source.Anime
	tree    seasons.FranchiseTree
	loading bool
	err     error
	cur     int
	off     int
	gl      glide
}

func newFranchiseScreen(an source.Anime) *franchiseScreen {
	return &franchiseScreen{an: an, loading: true}
}

func (s *franchiseScreen) crumb() string { return "evren › izleme sırası" }

func (s *franchiseScreen) animating(a *App) bool {
	return a.top() == screen(s) && s.loading
}

func (s *franchiseScreen) hints() []hint {
	return []hint{{"enter", "seriyi aç"}, {"↑↓", "gez"}, {"esc", "geri"}}
}

func (s *franchiseScreen) start(a *App) tea.Cmd {
	malID := s.an.MalID
	if malID <= 0 {
		s.loading = false
		s.err = fmt.Errorf("bu seri için MAL/AniList kimliği bulunamadı")
		return nil
	}
	return run(15*time.Second, func(ctx context.Context) tea.Msg {
		tree, err := seasons.FetchFranchise(ctx, malID)
		return franchiseMsg{malID: malID, tree: tree, err: err}
	})
}

func (s *franchiseScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case franchiseMsg:
		if m.malID != s.an.MalID {
			return nil
		}
		s.loading = false
		if m.err != nil {
			s.err = m.err
			return nil
		}
		m.tree.AnnotateStatus(a.lib, a.hist)
		s.tree = m.tree
		// Focus the "next" or "watching" node automatically
		for i, n := range s.tree.Nodes {
			if n.Status == "watching" || n.Status == "next" {
				s.cur = i
				break
			}
		}
		return a.tick()
	case tea.KeyMsg:
		switch m.String() {
		case "esc", "q":
			a.pop()
			return nil
		case "up", "k":
			if s.cur > 0 {
				s.cur--
			}
		case "down", "j":
			if s.cur+1 < len(s.tree.Nodes) {
				s.cur++
			}
		case "enter":
			if s.cur >= 0 && s.cur < len(s.tree.Nodes) {
				n := s.tree.Nodes[s.cur]
				a.notify("evren halkası aranıyor: "+n.Title, "ok")
				return a.openByMal(n.MalID, []string{n.Title}, s.an.Poster)
			}
		}
	}
	return nil
}

func (s *franchiseScreen) view(a *App, w, h int) string {
	cw := min(92, w-4)
	head := pageHeadCol("İzleme Sırası & Evren Ağacı", s.an.Title+" · Kronolojik & yayın sırası", "", cw, w, h)
	bodyH := max(6, h-len(head)-2)
	var rows []string
	switch {
	case s.loading:
		rows = append(rows, center(stAccent.Render(spinner(a.frame/2))+" "+stSub.Render("AniList ilişki grafiği çözülüyor…"), cw))
	case s.err != nil:
		rows = append(rows, center(stErr.Render("✗ "+s.err.Error()), cw))
	case len(s.tree.Nodes) == 0:
		rows = append(rows, center(stMuted.Render("İlişkili sezon veya film bulunamadı."), cw))
	default:
		s.off = scroll(s.cur, s.off, bodyH, len(s.tree.Nodes))
		band := s.gl.row(a, s.cur)
		for i := s.off; i < len(s.tree.Nodes) && len(rows) < bodyH; i++ {
			n := s.tree.Nodes[i]
			sel := i == band
			bg, bar := "", sp("  ", cText)
			if sel {
				bg, bar = cBand, sp("▌ ", cAccent)
			}
			mark, mc := "○", cMuted
			switch n.Status {
			case "done":
				mark, mc = "✓", cOK
			case "watching":
				mark, mc = "▶", cAccent
			case "next":
				mark, mc = "▸", cWarn
			}
			prefix := fmt.Sprintf("%2d. ", n.Order)
			if n.SideStory {
				prefix = "  └ "
			}
			meta := n.Format
			if n.Year > 0 {
				meta += fmt.Sprintf(" · %d", n.Year)
			}
			if n.Episodes > 0 {
				meta += fmt.Sprintf(" · %d böl.", n.Episodes)
			}
			tc := cText
			if n.Status == "done" && !sel {
				tc = cSub
			}
			if sel {
				tc = cAccent
			}
			rows = append(rows, row(cw, bg,
				[]span{bar, sp(prefix, cMuted), sp(mark+" ", mc), span{t: n.Title, c: tc, bold: sel}},
				[]span{sp(meta+" ", cMuted)},
			))
		}
	}
	var out []string
	out = append(out, head...)
	for _, r := range block(rows, cw, bodyH) {
		out = append(out, center(r, w))
	}
	return strings.Join(block(out, w, h), "\n")
}

// ---- Keyless OST / Theme Songs Screen (O on anime screen) ----

type ostMsg struct {
	key   string
	songs []media.ThemeSong
	err   error
}

type ostScreen struct {
	an      source.Anime
	songs   []media.ThemeSong
	loading bool
	err     error
	cur     int
	off     int
}

func newOSTScreen(an source.Anime) *ostScreen {
	return &ostScreen{an: an, loading: true}
}

func (s *ostScreen) crumb() string { return "müzikler · OST" }

func (s *ostScreen) animating(a *App) bool {
	return a.top() == screen(s) && s.loading
}

func (s *ostScreen) hints() []hint {
	return []hint{{"↑↓", "gez"}, {"esc", "geri"}}
}

func (s *ostScreen) start(a *App) tea.Cmd {
	an := s.an
	return run(12*time.Second, func(ctx context.Context) tea.Msg {
		songs, err := media.FetchThemes(ctx, an.MalID, an.Title)
		return ostMsg{key: an.Key(), songs: songs, err: err}
	})
}

func (s *ostScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ostMsg:
		if m.key != s.an.Key() {
			return nil
		}
		s.loading = false
		s.songs, s.err = m.songs, m.err
		return a.tick()
	case tea.KeyMsg:
		switch m.String() {
		case "esc", "q":
			a.pop()
		case "up", "k":
			if s.cur > 0 {
				s.cur--
			}
		case "down", "j":
			if s.cur+1 < len(s.songs) {
				s.cur++
			}
		}
	}
	return nil
}

func (s *ostScreen) view(a *App, w, h int) string {
	cw := min(90, w-4)
	head := pageHeadCol("OST & Müzik Katmanı", s.an.Title+" · Opening, Ending ve Insert parçalar (AnimeThemes)", "", cw, w, h)
	bodyH := max(6, h-len(head)-2)
	var rows []string
	switch {
	case s.loading:
		rows = append(rows, center(stAccent.Render(spinner(a.frame/2))+" "+stSub.Render("Açık müzik kataloğu sorgulanıyor…"), cw))
	case s.err != nil:
		rows = append(rows, center(stErr.Render("✗ "+s.err.Error()), cw))
	case len(s.songs) == 0:
		rows = append(rows, center(stMuted.Render("Bu seri için OP/ED müzik kaydı bulunamadı."), cw))
	default:
		s.off = scroll(s.cur, s.off, bodyH, len(s.songs))
		for i := s.off; i < len(s.songs) && len(rows) < bodyH; i++ {
			t := s.songs[i]
			sel := i == s.cur
			bg, bar := "", sp("  ", cText)
			if sel {
				bg, bar = cBand, sp("▌ ", cAccent)
			}
			sc := cSky
			if strings.HasPrefix(t.Slug, "ED") {
				sc = cAccent2
			}
			right := t.Artists
			if t.Episodes != "" {
				if right != "" {
					right += "  ·  "
				}
				right += "Böl. " + t.Episodes
			}
			rows = append(rows, row(cw, bg,
				[]span{bar, sp(fmt.Sprintf("%-5s ", t.Slug), sc), span{t: t.Title, c: cText, bold: sel}},
				[]span{sp(right+" ", cMuted)},
			))
		}
	}
	var out []string
	out = append(out, head...)
	for _, r := range block(rows, cw, bodyH) {
		out = append(out, center(r, w))
	}
	return strings.Join(block(out, w, h), "\n")
}

// ---- Scene Bookmarks Screen (B on anime screen) ----

type bookmarksScreen struct {
	an  source.Anime
	eps []source.Episode
	cur int
	off int
}

func newBookmarksScreen(an source.Anime, eps []source.Episode) *bookmarksScreen {
	return &bookmarksScreen{an: an, eps: eps}
}

func (s *bookmarksScreen) crumb() string { return "sahne yer imleri" }

func (s *bookmarksScreen) hints() []hint {
	return []hint{{"enter", "o saniyeden aç"}, {"d", "sil"}, {"esc", "geri"}}
}

func (s *bookmarksScreen) list(a *App) []store.Bookmark {
	if e, ok := a.hist.Get(s.an.Key()); ok {
		return e.Bookmarks
	}
	return nil
}

func (s *bookmarksScreen) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	bms := s.list(a)
	switch k.String() {
	case "esc", "q":
		a.pop()
	case "up", "k":
		if s.cur > 0 {
			s.cur--
		}
	case "down", "j":
		if s.cur+1 < len(bms) {
			s.cur++
		}
	case "d", "x", "delete":
		if s.cur >= 0 && s.cur < len(bms) {
			a.hist.DeleteBookmark(s.an, s.cur)
			if s.cur > 0 && s.cur >= len(bms)-1 {
				s.cur--
			}
			a.notify("sahne yer imi silindi", "ok")
			return a.tick()
		}
	case "enter":
		if s.cur >= 0 && s.cur < len(bms) {
			bm := bms[s.cur]
			// Store desired resume position and jump straight into playback
			a.hist.Update(s.an, func(e *store.Entry) {
				e.Season, e.Episode, e.Position = bm.Season, bm.Episode, bm.Pos
				if e.Duration < bm.Pos+30 {
					e.Duration = bm.Pos + 300
				}
				delete(e.Watched, store.EpKey(bm.Season, bm.Episode))
			})
			targetEp := source.Episode{Season: bm.Season, Number: bm.Episode}
			for _, ep := range s.eps {
				if ep.Season == bm.Season && ep.Number == bm.Episode {
					targetEp = ep
					break
				}
			}
			pk := newPicker(s.an, targetEp, s.eps, true)
			a.notify(fmt.Sprintf("★ %s anına zıplanıyor…", clock(bm.Pos)), "ok")
			return tea.Batch(a.push(pk), pk.start(a))
		}
	}
	return nil
}

func (s *bookmarksScreen) view(a *App, w, h int) string {
	cw := min(88, w-4)
	bms := s.list(a)
	head := pageHeadCol("Sahne Yer İmleri", s.an.Title+" · mpv içinde Shift+B / Ctrl+B ile kaydedilen anlar", "", cw, w, h)
	bodyH := max(6, h-len(head)-2)
	var rows []string
	if len(bms) == 0 {
		rows = append(rows, center(stMuted.Render("Henüz sahne yer imi yok. İzlerken mpv içinde Shift+B tuşuna bas."), cw))
	} else {
		s.off = scroll(s.cur, s.off, bodyH, len(bms))
		for i := s.off; i < len(bms) && len(rows) < bodyH; i++ {
			bm := bms[i]
			sel := i == s.cur
			bg, bar := "", sp("  ", cText)
			if sel {
				bg, bar = cBand, sp("▌ ", cAccent)
			}
			note := bm.Note
			if note == "" {
				note = "Kaydedilen sahne"
			}
			rows = append(rows, row(cw, bg,
				[]span{bar, sp(fmt.Sprintf("S%d·B%d  ", bm.Season, bm.Episode), cAccent2), sp(clock(bm.Pos)+"  ", cWarn), span{t: note, c: cText, bold: sel}},
				[]span{sp(agoLong(bm.CreatedAt)+" ", cMuted)},
			))
		}
	}
	var out []string
	out = append(out, head...)
	for _, r := range block(rows, cw, bodyH) {
		out = append(out, center(r, w))
	}
	return strings.Join(block(out, w, h), "\n")
}
