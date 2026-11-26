package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/download"
	"aniclitr/internal/mal"
	"aniclitr/internal/nyaa"
)

type nyaaMsg struct {
	id    int
	items []nyaa.Item
	err   error
}

type torrentTab struct {
	label  string
	cat    string
	filter string
}

var torrentTabs = []torrentTab{
	{"Tümü", nyaa.CatAll, nyaa.FilterAll},
	{"Türkçe / Diğer", nyaa.CatNonEng, nyaa.FilterAll},
	{"Güvenilir", nyaa.CatAll, nyaa.FilterTrusted},
	{"İngilizce", nyaa.CatEng, nyaa.FilterAll},
}

var torrentReqID int

type torrentScreen struct {
	in      input
	focus   int // 0 search box, 1 list
	tab     int
	reqID   int
	busy    bool
	err     error
	items   []nyaa.Item
	cur     int
	off     int
	gl      glide
	queried string
}

func newTorrentScreen(initial string) *torrentScreen {
	s := &torrentScreen{
		in: input{placeholder: "Nyaa.si'de anime torrent ara (örn: frieren 1080p)…"},
	}
	if q := strings.TrimSpace(initial); q != "" {
		s.in.Set(q)
		s.focus = 1
	}
	return s
}

func (s *torrentScreen) crumb() string { return "torrent · nyaa.si" }
func (s *torrentScreen) typing() bool  { return s.focus == 0 }

func (s *torrentScreen) animating(a *App) bool {
	return a.top() == screen(s) && s.busy
}

func (s *torrentScreen) hints() []hint {
	if s.focus == 0 {
		return []hint{{"enter", "ara"}, {"↓", "sonuçlar"}, {"esc", "geri"}}
	}
	return []hint{{"d", "aria2c indir"}, {"enter", "magnet aç / izle"}, {"1-4", "filtre"}, {"/", "ara"}, {"esc", "geri"}}
}

func (s *torrentScreen) start(a *App) tea.Cmd {
	q := strings.TrimSpace(s.in.Value())
	if q == "" {
		return nil
	}
	return s.search(a, q)
}

func (s *torrentScreen) search(a *App, q string) tea.Cmd {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	torrentReqID++
	s.reqID = torrentReqID
	s.busy = true
	s.err = nil
	s.queried = q
	s.cur, s.off = 0, 0
	t := torrentTabs[s.tab]
	id := s.reqID
	return tea.Batch(a.tick(), run(20*time.Second, func(ctx context.Context) tea.Msg {
		items, err := nyaa.Search(ctx, q, t.cat, t.filter)
		return nyaaMsg{id: id, items: items, err: err}
	}))
}

func (s *torrentScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case nyaaMsg:
		if m.id != s.reqID {
			return nil
		}
		s.busy = false
		s.err = m.err
		s.items = m.items
		s.cur, s.off = 0, 0
		if len(s.items) > 0 {
			s.focus = 1
		}
		return nil
	case tea.KeyMsg:
		if s.focus == 0 {
			switch m.Type {
			case tea.KeyEsc:
				if len(s.items) > 0 {
					s.focus = 1
					return nil
				}
				a.pop()
				return nil
			case tea.KeyDown:
				if len(s.items) > 0 {
					s.focus = 1
				}
				return nil
			case tea.KeyEnter:
				return s.search(a, s.in.Value())
			}
			s.in.handle(m)
			return nil
		}

		switch m.String() {
		case "esc", "q":
			a.pop()
		case "/":
			s.focus = 0
		case "up", "k":
			if s.cur == 0 {
				s.focus = 0
			} else {
				s.cur--
			}
		case "down", "j":
			if len(s.items) > 0 {
				s.cur = clamp(s.cur+1, 0, len(s.items)-1)
			}
		case "1", "2", "3", "4":
			idx := int(m.String()[0] - '1')
			if idx != s.tab {
				s.tab = idx
				if s.queried != "" {
					return s.search(a, s.queried)
				}
			}
		case "tab":
			s.tab = (s.tab + 1) % len(torrentTabs)
			if s.queried != "" {
				return s.search(a, s.queried)
			}
		case "r":
			if s.queried != "" {
				return s.search(a, s.queried)
			}
		case "d":
			if s.cur < len(s.items) {
				it := s.items[s.cur]
				target := it.Magnet
				if target == "" {
					target = it.Torrent
				}
				dir := a.cfg.DownloadDir
				if dir == "" {
					dir = download.DefaultDir()
				}
				if err := nyaa.StartAria2(dir, target); err != nil {
					a.notify(err.Error(), "err")
				} else {
					a.notify("aria2c torrent indirmesi başladı → "+dir, "ok")
					a.flash()
				}
				return a.tick()
			}
		case "enter":
			if s.cur < len(s.items) {
				it := s.items[s.cur]
				if it.Magnet == "" {
					if it.PageURL != "" {
						_ = mal.OpenBrowser(it.PageURL)
					}
					return nil
				}
				if nyaa.HasWebTorrent() {
					if err := nyaa.StreamWebTorrent(it.Magnet); err == nil {
						a.notify("webtorrent + mpv akışı başlatılıyor…", "ok")
						return a.tick()
					}
				}
				if err := mal.OpenBrowser(it.Magnet); err != nil {
					a.notify("magnet açılamadı: "+err.Error(), "err")
				} else {
					a.notify("magnet sistem torrent istemcisinde açıldı", "ok")
				}
				return a.tick()
			}
		case "o":
			if s.cur < len(s.items) && s.items[s.cur].PageURL != "" {
				_ = mal.OpenBrowser(s.items[s.cur].PageURL)
				a.notify("nyaa.si sayfası tarayıcıda açıldı", "ok")
				return a.tick()
			}
		}
	}
	return nil
}

func (s *torrentScreen) view(a *App, w, h int) string {
	cw := min(116, w-4)
	head := pageHeadCol("Torrent · Nyaa.si", "Açık anime torrent araması — aria2c ile indir veya magnet/webtorrent ile aç", "", cw, w, h)

	var top []string
	top = append(top, s.in.view(cw, s.focus == 0, a.frame)...)
	// Filter pills
	var tabs []string
	for i, t := range torrentTabs {
		lbl := fmt.Sprintf("%d %s", i+1, t.label)
		if i == s.tab {
			tabs = append(tabs, pill(lbl, cAccent))
		} else {
			tabs = append(tabs, fg(cMuted).Background(colorOf(cSurface)).Render(" "+lbl+" "))
		}
	}
	top = append(top, strings.Join(tabs, "  "), stFaint.Render(strings.Repeat("─", cw)))

	listH := max(4, h-len(head)-len(top)-3)
	var rows []string
	switch {
	case s.busy:
		rows = append(rows, "", stAccent.Render(spinner(a.frame/2))+" "+stSub.Render("Nyaa.si aranıyor…"))
	case s.err != nil:
		rows = append(rows, "", stErr.Render("✗ "+truncate(shortErr(s.err), cw-4)), stMuted.Render("r ile tekrar dene"))
	case s.queried == "":
		rows = append(rows, "",
			stSub.Render("Yukarıya bir anime adı yazıp enter'a bas (ör. frieren 1080p veya one piece)."),
			stMuted.Render("d: aria2c ile doğrudan indir · enter: webtorrent/mpv veya sistem torrent istemcisi · o: nyaa.si sayfası"),
		)
	case len(s.items) == 0:
		rows = append(rows, "", stMuted.Render("Sonuç bulunamadı — farklı bir filtre (1-4) ya da sorgu dene."))
	default:
		s.cur = clamp(s.cur, 0, len(s.items)-1)
		s.off = scroll(s.cur, s.off, listH, len(s.items))
		band := s.gl.row(a, s.cur)
		for i := s.off; i < len(s.items) && len(rows) < listH; i++ {
			it := s.items[i]
			sel := s.focus == 1 && i == band
			bg, bar := "", sp("  ", cText)
			if sel {
				bg, bar = cBand, sp("▌ ", cAccent)
			}
			left := []span{bar}
			if it.Trusted {
				left = append(left, sp("★ ", cOK))
			}
			if it.Turkish {
				left = append(left, spb("TR ", cSky))
			}
			left = append(left, span{t: it.Title, c: cText, bold: sel})

			sc := cOK
			if it.Seeders == 0 {
				sc = cErr
			} else if it.Seeders < 5 {
				sc = cWarn
			}
			right := []span{
				sp(fmt.Sprintf("%9s  ", it.Size), cSub),
				sp(fmt.Sprintf("▲%-4d ", it.Seeders), sc),
				sp(fmt.Sprintf("▼%-3d ", it.Leechers), cMuted),
			}
			rows = append(rows, row(cw-1, bg, left, right))
		}
		rows = hjoin(0, block(rows, cw-1, listH), scrollbar(listH, s.off, len(s.items)))
	}

	var foot []string
	if s.focus == 1 && s.cur < len(s.items) {
		it := s.items[s.cur]
		foot = append(foot,
			stFaint.Render(strings.Repeat("─", cw)),
			truncate(stSub.Render(it.Category)+stFaint.Render("  ·  ")+stMuted.Render(fmt.Sprintf("↓%d indirme · hash %s", it.Downloads, truncate(it.InfoHash, 14))), cw),
		)
	}

	all := append(top, block(rows, cw, listH)...)
	all = append(all, foot...)
	var centered []string
	for _, l := range all {
		centered = append(centered, center(fit(l, cw), w))
	}
	out := append(head, block(centered, w, max(4, h-len(head)))...)
	return strings.Join(out, "\n")
}
