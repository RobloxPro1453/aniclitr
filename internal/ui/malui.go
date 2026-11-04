package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/mal"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// malMsg: the MAL syncer's status changed.
type malMsg struct{}

func (a *App) waitMal() tea.Cmd {
	if a.msync == nil {
		return nil
	}
	ch := a.msync.Updates
	return func() tea.Msg { <-ch; return malMsg{} }
}

// onMal reports sync errors (each one once) and recovery.
func (a *App) onMal() {
	st := a.msync.Status()
	if st.Err != "" && st.Err != a.malErr {
		a.notify("MAL: "+st.Err, "err")
		a.addNotice(notice{key: "mal:" + st.Err, kind: "err", text: "MAL eşitlemesi: " + st.Err,
			action: func(a *App) tea.Cmd { return a.push(newMalScreen(a)) }})
	}
	if st.Err == "" && a.malErr != "" {
		a.notify("MAL eşitlemesi yeniden çalışıyor", "ok")
	}
	a.malErr = st.Err
}

// malLine is the small status shown in the library header.
func (a *App) malLine() string {
	if !a.mal.Connected() {
		return stFaint.Render("○ ") + stMuted.Render("MAL · m")
	}
	st := a.msync.Status()
	switch {
	case st.Busy:
		return stAccent.Render(spinner(a.frame/2)+" ") + stMuted.Render("MAL eşitleniyor…")
	case st.Err != "":
		return stErr.Render("● ") + stMuted.Render("MAL hata · m")
	case !st.LastSync.IsZero():
		return stOK.Render("● ") + stSub.Render("MAL "+a.mal.User()) + stMuted.Render(" · "+agoLong(st.LastSync))
	}
	return stOK.Render("● ") + stSub.Render("MAL "+a.mal.User())
}

// bumpWatched records progress for MAL: the highest episode of the first
// season (later seasons are separate MAL entries — see TODO).
func bumpWatched(a *App, an source.Anime, ep source.Episode) {
	if len(an.Seasons) > 1 && ep.Season != an.Seasons[0].Number {
		return
	}
	a.libSet(an, func(x *store.Item) {
		if ep.Number > x.Watched {
			x.Watched = ep.Number
		}
	})
}

// ---- opening something known only by MAL id (MAL list, AniList) ----

type malOpenMsg struct {
	an    source.Anime
	alts  []source.Anime
	query string
	err   error
}

// openByMal finds a title on the sources (matching the MAL id) and opens it.
func (a *App) openByMal(malID int, titles []string, poster string) tea.Cmd {
	a.notify("kaynaklarda aranıyor: "+firstNonEmpty(titles...), "")
	var provs []source.Provider
	for _, p := range source.All() {
		if a.cfg.Enabled(p.Info().ID, p.Info().Legacy) {
			provs = append(provs, p)
		}
	}
	return tea.Batch(a.tick(), run(45*time.Second, func(ctx context.Context) tea.Msg {
		var found, fallback []source.Anime
		for _, p := range provs {
		titles:
			for ti, t := range titles {
				if ti > 2 || strings.TrimSpace(t) == "" {
					break
				}
				res, err := p.Search(ctx, t)
				if err != nil || len(res) == 0 {
					continue
				}
				if len(fallback) == 0 && fold(res[0].Title) == fold(t) {
					fallback = append(fallback, res[0])
				}
				for i, r := range res {
					if i >= 4 {
						break
					}
					if malID != 0 && r.MalID == malID {
						found = append(found, r)
						break titles
					}
					if d, err := p.Details(ctx, r); err == nil && malID != 0 && d.MalID == malID {
						found = append(found, d)
						break titles
					}
				}
			}
		}
		if len(found) == 0 {
			found = fallback
		}
		if len(found) == 0 {
			return malOpenMsg{query: firstNonEmpty(titles...), err: source.ErrNotFound}
		}
		an := found[0]
		if an.Poster == "" {
			an.Poster = poster
		}
		if malID != 0 {
			an.MalID = malID
		}
		return malOpenMsg{an: an, alts: found[1:]}
	}))
}

func (a *App) onMalOpen(m malOpenMsg) tea.Cmd {
	if m.err != nil {
		a.notify("kaynaklarda bulunamadı — aramaya düşüyorum", "warn")
		r := newResults(m.query)
		return tea.Batch(a.push(r), r.start(a))
	}
	sc := newAnime(m.an, m.alts, 0, 0)
	return tea.Batch(a.push(sc), sc.start(a))
}

func fold(s string) string {
	var b strings.Builder
	for _, r := range foldRunes(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---- MAL connect screen ----

type malScreen struct {
	in      input
	waiting bool
	err     string
}

type malLoginMsg struct{ err error }

func newMalScreen(a *App) *malScreen {
	s := &malScreen{in: input{placeholder: "MAL Client ID'yi buraya yapıştır"}}
	s.in.Set(a.cfg.MalClientID)
	return s
}

func (s *malScreen) crumb() string { return "MyAnimeList" }
func (s *malScreen) typing() bool  { return !s.waiting }

func (s *malScreen) hints() []hint {
	if s.waiting {
		return []hint{{"esc", "vazgeç"}}
	}
	return []hint{{"enter", "bağlan"}, {"ctrl+y", "apiconfig sayfası"}, {"ctrl+s", "şimdi eşitle"}, {"ctrl+x", "bağlantıyı kes"}, {"esc", "geri"}}
}

func (s *malScreen) animating(a *App) bool { return s.waiting || a.msync.Status().Busy }

func (s *malScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case malLoginMsg:
		s.waiting = false
		if m.err != nil {
			s.err = m.err.Error()
			a.notify("MAL girişi olmadı: "+s.err, "err")
			return nil
		}
		s.err = ""
		a.msync.Start()
		a.anchorOK = false
		a.celebrate(70)
		a.notify("MyAnimeList bağlandı: "+a.mal.User()+" · listen çekiliyor", "ok")
		return a.tick()
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			a.pop()
			return nil
		case "ctrl+y":
			_ = mal.OpenBrowser("https://myanimelist.net/apiconfig")
			return nil
		case "ctrl+s":
			if a.mal.Connected() {
				a.msync.SyncNow()
				a.notify("MAL eşitleniyor…", "")
			}
			return a.tick()
		case "ctrl+x":
			a.mal.Logout()
			a.notify("MAL bağlantısı kesildi (kitaplık yerelde duruyor)", "ok")
			return nil
		case "enter":
			if s.waiting {
				return nil
			}
			id := strings.TrimSpace(s.in.Value())
			if len(id) < 16 {
				s.err = "Client ID çok kısa görünüyor (32 karakterlik bir kod)"
				return nil
			}
			a.cfg.MalClientID = id
			_ = a.cfg.Save()
			a.mal.ClientID = id
			s.waiting, s.err = true, ""
			c := a.mal
			return tea.Batch(a.tick(), guard("mal giriş", func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
				defer cancel()
				return malLoginMsg{err: c.Login(ctx)}
			}))
		}
		if !s.waiting {
			s.in.handle(m)
		}
	}
	return nil
}

func (s *malScreen) view(a *App, w, h int) string {
	cw := min(90, w-6)
	out := pageHeadCol("MyAnimeList", "Kitaplığın MyAnimeList ile iki yönlü eşitlenmesi", "", cw, w, h)
	st := a.msync.Status()
	if a.mal.Connected() {
		line := stOK.Render("● bağlı") + stMuted.Render("  ·  ") + stBold.Render(a.mal.User())
		if !st.LastSync.IsZero() {
			line += stMuted.Render(fmt.Sprintf("  ·  son eşitleme %s (↓%d ↑%d)", agoLong(st.LastSync), st.Pulled, st.Pushed))
		}
		if st.Busy {
			line += "  " + stAccent.Render(spinner(a.frame/2)+" eşitleniyor")
		}
		out = append(out, center(line, w))
		if st.Err != "" {
			out = append(out, "", center(stErr.Render(truncate(st.Err, cw)), w))
		}
		out = append(out, "", center(stMuted.Render("Kitaplığın MAL listenin aynası: durum, puan, izlenen bölüm ve listeler (MAL'de etiket) iki yönlü eşitlenir."), w),
			center(stMuted.Render("ctrl+s şimdi eşitle  ·  ctrl+x bağlantıyı kes"), w))
		return strings.Join(out, "\n")
	}
	steps := []string{
		stBold.Render("1") + stSub.Render("  myanimelist.net/apiconfig → ") + stBold.Render("Create ID") + stMuted.Render("   (ctrl+y açar)"),
		stBold.Render("2") + stSub.Render("  App Type: ") + stBold.Render("other") + stSub.Render("   ·   App Redirect URL:"),
		"   " + stAccent.Render(mal.RedirectURL),
		stBold.Render("3") + stSub.Render("  Diğer alanlara ne istersen yaz, kaydet; çıkan ") + stBold.Render("Client ID") + stSub.Render("'yi kopyala"),
		stBold.Render("4") + stSub.Render("  Aşağıya yapıştır, enter → tarayıcıda ") + stBold.Render("Allow") + stSub.Render(" de, bitti"),
	}
	for _, l := range panel("bağlanmak için", steps, cw, len(steps)+2, true, cAccent) {
		out = append(out, center(l, w))
	}
	out = append(out, "")
	for _, l := range s.in.view(min(72, cw), !s.waiting, a.frame) {
		out = append(out, center(l, w))
	}
	switch {
	case s.waiting:
		out = append(out, "", center(stAccent.Render(spinner(a.frame/2))+stSub.Render(" tarayıcıda onay bekleniyor… (şifren aniclitr'e hiç gelmez)"), w))
	case s.err != "":
		out = append(out, "", center(stErr.Render("✗ "+truncate(s.err, cw)), w))
	}
	return strings.Join(out, "\n")
}

// openItem opens a library item: on its source, or (MAL-only items) by
// finding it on the sources first.
func (a *App) openItem(it store.Item) tea.Cmd {
	if it.FromMal() {
		return a.openByMal(it.MalID, []string{it.Title}, it.Poster)
	}
	sc := newAnime(it.Anime(), nil, 0, 0)
	return tea.Batch(a.push(sc), sc.start(a))
}
