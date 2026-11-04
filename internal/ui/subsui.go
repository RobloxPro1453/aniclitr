package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/subs"
)

// External subtitles while playing: `u` searches the internet for the
// current episode, the list appears in the player card, enter loads the
// chosen file straight into the running mpv (no restart).

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
	open bool
	busy bool
	rs   []subs.Result
	cur  int
	err  string
}

func (a *App) subProvider() subs.Provider { return &subs.OpenSubtitles{Key: a.cfg.OSKey} }

func (p *playing) searchSubs(a *App) tea.Cmd {
	if a.cfg.OSKey == "" {
		a.notify("altyazı için OpenSubtitles API anahtarı gerekli: config.json → opensubtitles_api_key", "warn")
		return nil
	}
	p.subs = subsState{open: true, busy: true}
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
	switch k.String() {
	case "esc", "u":
		p.subs.open = false
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
			p.subs.err = "bu bölüm için altyazı bulunamadı"
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
	out := []string{"", sectionTitle("altyazı · OpenSubtitles", w, true)}
	switch {
	case p.subs.busy && len(p.subs.rs) == 0:
		out = append(out, stAccent.Render(spinner(a.frame/2))+stMuted.Render(" aranıyor…"))
	case p.subs.err != "" && len(p.subs.rs) == 0:
		out = append(out, stErr.Render("✗ "+truncate(p.subs.err, w-2)))
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
		out = append(out, row(w, bg, []span{bar, spb(fmt.Sprintf("%-3s", r.Lang), lc), span{t: r.Release, c: cText, bold: sel}}, []span{sp(fmt.Sprintf("↓%d ", r.Downloads), cMuted)}))
	}
	if p.subs.busy && len(p.subs.rs) > 0 {
		out = append(out, stAccent.Render(spinner(a.frame/2))+stMuted.Render(" indiriliyor…"))
	}
	return out
}
