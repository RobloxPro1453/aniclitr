package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/diag"
	"aniclitr/internal/source"
)

// Source health: every enabled provider is probed at start and every five
// minutes; the header dots show it (colour = fine, amber = slow, red ✗ =
// down) and the "kaynaklar" screen shows latency and the last error. New
// providers get this for free (or implement source.Prober).

type healthMsg struct{ h source.Health }
type healthTickMsg struct{}

func (a *App) checkHealth() tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range source.All() {
		in := p.Info()
		if !a.cfg.Enabled(in.ID, in.Legacy) {
			continue
		}
		p := p
		cmds = append(cmds, run(25*time.Second, func(ctx context.Context) tea.Msg {
			return healthMsg{source.Check(ctx, p)}
		}))
	}
	cmds = append(cmds, tea.Tick(5*time.Minute, func(time.Time) tea.Msg { return healthTickMsg{} }))
	return tea.Batch(cmds...)
}

func (a *App) onHealth(h source.Health) {
	if a.offline {
		return // no internet at all: per-source warnings would only be noise
	}
	prev, seen := a.health[h.ID]
	a.health[h.ID] = h
	name := h.ID
	if p := source.Get(h.ID); p != nil {
		name = p.Info().Name
	}
	switch {
	case !h.OK && (!seen || prev.OK):
		a.notify(name+" şu an yanıt vermiyor — diğer kaynaklar kullanılıyor", "warn")
		a.addNotice(notice{key: fmt.Sprintf("health:%s:%d", h.ID, h.At.Unix()), kind: "warn", text: name + " yanıt vermiyor",
			action: func(a *App) tea.Cmd { return a.push(&sourcesScreen{}) }})
	case h.OK && seen && !prev.OK:
		a.notify(name+" yeniden çalışıyor", "ok")
	}
}

// healthDot is the header marker for a source.
func (a *App) healthDot(id string) string {
	h, ok := a.health[id]
	switch {
	case !ok:
		return fg(sourceColor(id)).Render("●")
	case !h.OK:
		return stErr.Render("✗")
	case h.Slow():
		return stWarn.Render("●")
	}
	return fg(sourceColor(id)).Render("●")
}

// ---- screen ----

type sourcesScreen struct {
	cur int
	gl  glide
}

func (s *sourcesScreen) crumb() string { return "kaynaklar" }
func (s *sourcesScreen) hints() []hint {
	return []hint{{"space", "aç / kapat"}, {"r", "şimdi yokla"}, {"esc", "geri"}}
}

func (s *sourcesScreen) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	ps := source.All()
	switch k.String() {
	case "esc", "q":
		a.pop()
	case "up", "k":
		s.cur = max(0, s.cur-1)
	case "down", "j":
		s.cur = clamp(s.cur+1, 0, len(ps)-1)
	case "r":
		a.notify("kaynaklar yoklanıyor…", "")
		return a.checkHealth()
	case " ", "enter":
		in := ps[s.cur].Info()
		if in.Legacy {
			return toggleLegacy(a)
		}
		a.cfg.Sources[in.ID] = !a.cfg.Enabled(in.ID, false)
		_ = a.cfg.Save()
		a.flash()
		warnSources(a)
		return a.checkHealth()
	}
	return nil
}

func (s *sourcesScreen) view(a *App, w, h int) string {
	cw := min(100, w-6)
	out := pageHeadCol("Kaynaklar", "Kaynakların erişilebilirliği ve etkinlik durumu", "", cw, w, h)
	band := s.gl.row(a, s.cur)
	for i, p := range source.All() {
		in := p.Info()
		on := a.cfg.Enabled(in.ID, in.Legacy)
		sel := i == band
		bg, bar := "", sp("  ", cText)
		if sel {
			bg, bar = cBand, sp("▌ ", cAccent)
		}
		state, sc := "kapalı", cMuted
		hl, probed := a.health[in.ID]
		switch {
		case !on:
		case !probed:
			state, sc = spinner(a.frame/2)+" yoklanıyor", cAccent2
		case !hl.OK:
			state, sc = "✗ yanıt yok", cErr
		case hl.Slow():
			state, sc = fmt.Sprintf("● yavaş · %d ms", hl.Latency.Milliseconds()), cWarn
		default:
			state, sc = fmt.Sprintf("● çalışıyor · %d ms", hl.Latency.Milliseconds()), cOK
		}
		out = append(out, center(row(cw, bg, []span{bar, sp("● ", sourceColor(in.ID)), span{t: in.Name, c: cText, bold: sel}, sp("  "+sourceRole(in), cMuted)}, []span{sp(state+" ", sc)}), w))
		if in.Note != "" {
			out = append(out, center(fit(stMuted.Render("    "+in.Note), cw), w))
		}
		if on && probed {
			lat := float64(hl.Latency) / float64(3*time.Second)
			detail := fmt.Sprintf("    son yoklama %s", agoLong(hl.At))
			if hl.Err != "" {
				detail += "  ·  " + truncate(hl.Err, cw-40)
			}
			bc := cOK
			if hl.Slow() {
				bc = cWarn
			}
			if !hl.OK {
				bc, lat = cErr, 1
			}
			out = append(out, center(fit("    "+renderSpans(miniBar(24, lat, bc))+stMuted.Render(detail), cw), w))
		}
		if on {
			if bl := diag.SummaryLine(in.ID); bl != "" {
				out = append(out, center(fit(stFaint.Render("    tanılama: ")+stMuted.Render(truncate(bl, cw-16)), cw), w))
			}
		}
		out = append(out, "")
	}
	out = append(out, center(stFaint.Render(strings.Repeat("─", cw)), w),
		center(fit(stMuted.Render("Arama bütün açık kaynaklarda paralel yapılır; biri düşerse sonuçlar diğerlerinden gelir."), cw), w))
	return strings.Join(out, "\n")
}

// sourceRole is the short role label of a source.
func sourceRole(in source.Info) string {
	switch {
	case in.Primary:
		return "ana kaynak"
	case in.Legacy:
		return "arşiv"
	case in.Lang == "en":
		return "İngilizce"
	}
	return "Türkçe"
}

// legacyNames lists the archive sources ("TürkAnime Arşivi").
func legacyNames() string {
	var names []string
	for _, p := range source.All() {
		if in := p.Info(); in.Legacy {
			names = append(names, in.Name)
		}
	}
	if len(names) == 0 {
		return "arşiv kaynakları"
	}
	return strings.Join(names, ", ")
}

func legacyDesc() string {
	return "Aramaya eklenir: " + legacyNames() + ". Veriler donmuş, linkler zamanla ölür."
}

// turkishSources names the Turkish sources ("AnimeciX · AnimeTR").
func turkishSources() string {
	var names []string
	for _, p := range source.All() {
		if in := p.Info(); in.Lang == "tr" && !in.Legacy {
			names = append(names, in.Name)
		}
	}
	return strings.Join(names, " · ")
}

// warnSources says what switching sources off means for search, so an empty
// result list never comes as a surprise.
func warnSources(a *App) {
	var on []string
	primaryOff := ""
	for _, p := range source.All() {
		in := p.Info()
		if in.Legacy {
			continue
		}
		if a.cfg.Enabled(in.ID, false) {
			on = append(on, in.Name)
		} else if in.Primary {
			primaryOff = in.Name
		}
	}
	switch {
	case len(on) == 0:
		a.notify("bütün kaynaklar kapalı — arama sonuç vermez", "err")
	case primaryOff != "":
		a.notify("ana kaynak "+primaryOff+" kapalı — arama yalnızca: "+strings.Join(on, ", "), "warn")
	}
}
