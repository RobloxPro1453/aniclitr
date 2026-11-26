package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
	"aniclitr/internal/mal"
)

type aniListMsg struct{}

func (a *App) waitAniList() tea.Cmd {
	if a.alsync == nil {
		return nil
	}
	ch := a.alsync.Updates
	return func() tea.Msg { <-ch; return aniListMsg{} }
}

func (a *App) onAniList() {
	if a.alsync == nil {
		return
	}
	st := a.alsync.Status()
	if st.Err != "" && st.Err != a.alErr {
		a.notify("AniList: "+st.Err, "err")
		a.addNotice(notice{key: "al:" + st.Err, kind: "err", text: "AniList eşitlemesi: " + st.Err,
			action: func(a *App) tea.Cmd { return a.push(newAniListScreen(a)) }})
	}
	if st.Err == "" && a.alErr != "" {
		a.notify("AniList eşitlemesi yeniden çalışıyor", "ok")
	}
	a.alErr = st.Err
}

// ---- AniList connect screen ----

type aniListScreen struct {
	in      input
	waiting bool
	err     string
}

type aniListLoginMsg struct{ err error }

func newAniListScreen(a *App) *aniListScreen {
	s := &aniListScreen{in: input{placeholder: "AniList Client ID (veya erişim jetonu) yapıştır"}}
	s.in.Set(a.cfg.AniListClientID)
	return s
}

func (s *aniListScreen) crumb() string { return "AniList" }
func (s *aniListScreen) typing() bool  { return !s.waiting }

func (s *aniListScreen) hints() []hint {
	if s.waiting {
		return []hint{{"esc", "vazgeç"}}
	}
	return []hint{{"enter", "bağlan"}, {"ctrl+y", "developer sayfası"}, {"ctrl+s", "şimdi eşitle"}, {"ctrl+x", "bağlantıyı kes"}, {"esc", "geri"}}
}

func (s *aniListScreen) animating(a *App) bool {
	return s.waiting || (a.alsync != nil && a.alsync.Status().Busy)
}

func (s *aniListScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case aniListLoginMsg:
		s.waiting = false
		if m.err != nil {
			s.err = m.err.Error()
			a.notify("AniList girişi olmadı: "+s.err, "err")
			return nil
		}
		s.err = ""
		a.alsync.Start()
		a.anchorOK = false
		a.celebrate(70)
		a.notify("AniList bağlandı: "+a.al.User()+" · listen çekiliyor", "ok")
		return a.tick()
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			a.pop()
			return nil
		case "ctrl+y":
			_ = mal.OpenBrowser("https://anilist.co/settings/developer")
			return nil
		case "ctrl+s":
			if a.al.Connected() {
				a.alsync.SyncNow()
				a.notify("AniList eşitleniyor…", "ok")
				return a.tick()
			}
			return nil
		case "ctrl+x":
			if a.al.Connected() {
				a.al.Disconnect()
				a.notify("AniList bağlantısı kesildi", "warn")
				return a.tick()
			}
			return nil
		case "enter":
			if s.waiting {
				return nil
			}
			val := strings.TrimSpace(s.in.Value())
			if val == "" {
				s.err = "AniList Client ID veya erişim jetonu gir"
				return nil
			}
			s.waiting, s.err = true, ""
			al := a.al
			// Long strings (>24 chars) are pasted OAuth access tokens; short numeric IDs run browser OAuth.
			if len(val) > 24 {
				return tea.Batch(a.tick(), run(20*time.Second, func(ctx context.Context) tea.Msg {
					return aniListLoginMsg{err: al.LoginWithToken(ctx, val)}
				}))
			}
			a.cfg.AniListClientID = val
			_ = a.cfg.Save()
			return tea.Batch(a.tick(), run(3*time.Minute, func(ctx context.Context) tea.Msg {
				return aniListLoginMsg{err: al.Login(ctx, val)}
			}))
		}
		if !s.waiting {
			s.in.handle(m)
		}
	}
	return nil
}

func (s *aniListScreen) view(a *App, w, h int) string {
	cw := min(78, w-4)
	head := pageHeadCol("AniList", "Kitaplık, puan, bölüm ilerlemesi ve özel listeleri iki yönlü eşitle", "", cw, w, h)
	var body []string
	if a.al.Connected() {
		st := a.alsync.Status()
		line := stOK.Render("● bağlı  ·  ") + stBold.Render(a.al.User())
		if st.Busy {
			line += stAccent.Render("   " + spinner(a.frame/2) + " eşitleniyor…")
		} else if !st.LastSync.IsZero() {
			line += stMuted.Render(fmt.Sprintf("   · %d anime · %s", st.Count, agoLong(st.LastSync)))
		}
		body = append(body, line, "")
	} else {
		body = append(body, stMuted.Render("○ bağlı değil"), "")
	}

	body = append(body,
		stBold.Render("Tek seferlik kurulum"),
		stSub.Render("1. anilist.co/settings/developer sayfasında 'Create New Client' seç (ctrl+y)."),
		stSub.Render("2. Redirect URL olarak şunu yaz:  ")+stAccent.Render(anilist.CallbackURL),
		stSub.Render("3. Oluşan Client ID'yi aşağıya yapıştırıp enter'a bas (veya doğrudan erişim jetonu yapıştır)."),
		"",
	)
	body = append(body, s.in.view(cw-2, !s.waiting, a.frame)...)
	if s.waiting {
		body = append(body, "", stAccent.Render(spinner(a.frame/2))+" "+stSub.Render("tarayıcıda AniList onayı bekleniyor…"))
	}
	if s.err != "" {
		body = append(body, "", stErr.Render("✗ "+truncate(s.err, cw-4)))
	}
	body = append(body, "",
		stMuted.Render("Not: AniList jetonun anilist.json içinde yalnızca yerel kalır, taşınabilir yedeklere eklenmez."),
	)
	var lines []string
	for _, l := range body {
		lines = append(lines, center(fit(l, cw), w))
	}
	out := append(head, block(lines, w, max(4, h-len(head)))...)
	return strings.Join(out, "\n")
}
