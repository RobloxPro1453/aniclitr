package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/subs"
)

// External & local subtitles while playing: `u` opens the subtitle chooser.
// Searches AniSub (free Turkish community archive, no API key needed) and
// OpenSubtitles (if configured), or press `l` to load a local .srt/.ass/.vtt
// file straight into the running mpv without restarting playback.

type subsMsg struct {
	id  int
	rs  []subs.Result
	err error
}

type subGotMsg struct {
	id   int
	path string
	r    subs.Result
	err  error
}

type subsState struct {
	open    bool
	busy    bool
	local   bool // typing a local .srt/.ass/.vtt path
	localIn input
	rs      []subs.Result
	cur     int
	err     string
}

func (a *App) subProvider() subs.Provider {
	provs := []subs.Provider{&subs.AniSub{}}
	if strings.TrimSpace(a.cfg.OSKey) != "" {
		provs = append(provs, &subs.OpenSubtitles{Key: a.cfg.OSKey})
	}
	return &subs.Multi{Providers: provs}
}

func (p *playing) searchSubs(a *App) tea.Cmd {
	p.subs = subsState{
		open:    true,
		busy:    true,
		localIn: input{placeholder: `C:\yol\altyazi.ass veya .srt yapıştır…`},
	}
	id, prov := p.id, a.subProvider()
	q := subs.Query{Title: p.an.Title, Season: p.ep.Season, Episode: p.ep.Number, Langs: []string{"tr", "en"}}
	if p.an.IsMovie() {
		q.Season, q.Episode = 0, 0
	}
	return tea.Batch(a.tick(), run(30*time.Second, func(ctx context.Context) tea.Msg {
		rs, err := prov.Search(ctx, q)
		return subsMsg{id, rs, err}
	}))
}

func (p *playing) subKey(a *App, k tea.KeyMsg) tea.Cmd {
	if p.subs.local {
		switch k.Type {
		case tea.KeyEsc:
			p.subs.local = false
			return nil
		case tea.KeyEnter:
			raw := strings.TrimSpace(p.subs.localIn.Value())
			raw = strings.Trim(raw, `"'`)
			if raw == "" {
				p.subs.local = false
				return nil
			}
			st, err := os.Stat(raw)
			if err != nil || st.IsDir() {
				a.notify("dosya bulunamadı: "+raw, "err")
				return a.tick()
			}
			ext := strings.ToLower(filepath.Ext(raw))
			if ext != ".srt" && ext != ".ass" && ext != ".ssa" && ext != ".vtt" {
				a.notify("desteklenen biçimler: .ass, .srt, .ssa, .vtt", "warn")
				return a.tick()
			}
			if p.sess != nil {
				p.sess.AddSub(raw, filepath.Base(raw), "tr")
			}
			p.subs.open, p.subs.local = false, false
			a.notify("yerel altyazı yüklendi: "+filepath.Base(raw), "ok")
			a.flash()
			return a.tick()
		}
		p.subs.localIn.handle(k)
		return nil
	}

	switch k.String() {
	case "esc", "u":
		p.subs.open = false
	case "l":
		p.subs.local = true
		p.subs.localIn.Set("")
		return a.tick()
	case "up", "k":
		p.subs.cur = max(0, p.subs.cur-1)
	case "down", "j":
		p.subs.cur = clamp(p.subs.cur+1, 0, len(p.subs.rs)-1)
	case "enter":
		if p.subs.cur < len(p.subs.rs) && !p.subs.busy {
			r := p.subs.rs[p.subs.cur]
			p.subs.busy = true
			id, prov := p.id, a.subProvider()
			return tea.Batch(a.tick(), run(40*time.Second, func(ctx context.Context) tea.Msg {
				path, err := prov.Download(ctx, r)
				return subGotMsg{id, path, r, err}
			}))
		}
	}
	return nil
}

func (p *playing) onSubs(a *App, msg tea.Msg) {
	switch m := msg.(type) {
	case subsMsg:
		if m.id != p.id {
			return
		}
		p.subs.busy = false
		if m.err != nil {
			p.subs.err = m.err.Error()
			return
		}
		p.subs.rs, p.subs.cur = m.rs, 0
		if len(m.rs) == 0 {
			p.subs.err = "bu bölüm için çevrimiçi altyazı bulunamadı (l ile yerel dosya seç)"
		}
	case subGotMsg:
		if m.id != p.id {
			return
		}
		p.subs.busy = false
		if m.err != nil {
			a.notify("altyazı indirilemedi: "+m.err.Error(), "err")
			return
		}
		if p.sess != nil {
			p.sess.AddSub(m.path, m.r.Label(), m.r.Lang)
		}
		p.subs.open = false
		a.notify("altyazı eklendi: "+m.r.Label(), "ok")
		a.flash()
	}
}

// subLines renders the subtitle chooser inside the player card.
func (p *playing) subLines(a *App, w int) []string {
	if !p.subs.open {
		return nil
	}
	srcLbl := "altyazı · AniSub"
	if strings.TrimSpace(a.cfg.OSKey) != "" {
		srcLbl = "altyazı · AniSub + OpenSubtitles"
	}
	out := []string{"", sectionTitle(srcLbl, w, true)}
	if p.subs.local {
		out = append(out, stSub.Render("Yerel altyazı dosyası (.ass / .srt / .vtt):"))
		out = append(out, p.subs.localIn.view(max(16, w-1), true, a.frame)...)
		out = append(out, stMuted.Render("enter yükle · esc listeye dön"))
		return out
	}
	switch {
	case p.subs.busy && len(p.subs.rs) == 0:
		out = append(out, stAccent.Render(spinner(a.frame/2))+stMuted.Render(" AniSub'da aranıyor…  (l: yerel dosya)"))
	case p.subs.err != "" && len(p.subs.rs) == 0:
		out = append(out, stErr.Render("✗ "+truncate(p.subs.err, max(8, w-2))))
	}
	off := 0
	if p.subs.cur >= 6 {
		off = p.subs.cur - 5
	}
	for i := off; i < len(p.subs.rs) && i < off+6; i++ {
		r := p.subs.rs[i]
		sel := i == p.subs.cur
		bg, bar := "", sp("  ", cText)
		if sel {
			bg, bar = cBand, sp("▌ ", cAccent)
		}
		lc := cSub
		if r.Lang == "tr" {
			lc = cOK
		}
		right := []span{}
		if r.Provider != "" {
			right = append(right, sp(r.Provider+" ", cFaint))
		}
		if r.Downloads > 0 {
			right = append(right, sp(fmt.Sprintf("↓%d ", r.Downloads), cMuted))
		}
		out = append(out, row(w, bg, []span{bar, spb(fmt.Sprintf("%-3s", r.Lang), lc), span{t: r.Release, c: cText, bold: sel}}, right))
	}
	if p.subs.busy && len(p.subs.rs) > 0 {
		out = append(out, stAccent.Render(spinner(a.frame/2))+stMuted.Render(" indiriliyor…"))
	} else {
		out = append(out, stMuted.Render("enter yükle · l yerel dosya (.ass/.srt) · esc kapat"))
	}
	return out
}
