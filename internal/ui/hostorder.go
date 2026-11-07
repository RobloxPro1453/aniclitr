package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// hostOrderScreen edits how quick play ranks the video hosts
// (cfg.HostOrder) and which it skips (never_hosts) or keeps to (only_hosts).
// Every change is saved at once; esc only goes back.
type hostOrderScreen struct {
	cur   int
	gl    glide
	order []string
}

func newHostOrder(a *App) *hostOrderScreen {
	return &hostOrderScreen{order: hostOrder(a)}
}

// hostOrder is the configured order completed with the known hosts it lacks.
func hostOrder(a *App) []string {
	var out []string
	for _, h := range a.cfg.HostOrder {
		if h != "" && !containsFold(out, h) {
			out = append(out, h)
		}
	}
	for _, h := range primaryHosts {
		if !containsFold(out, h) {
			out = append(out, h)
		}
	}
	return out
}

func containsFold(xs []string, s string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func withoutFold(xs []string, s string) []string {
	var out []string
	for _, x := range xs {
		if !strings.EqualFold(x, s) {
			out = append(out, x)
		}
	}
	return out
}

func (s *hostOrderScreen) crumb() string { return "sunucu sırası" }

func (s *hostOrderScreen) hints() []hint {
	return []hint{{"[ ]", "yukarı / aşağı taşı"}, {"n", "asla seçme"}, {"o", "yalnızca bunlar"}, {"0", "varsayılana dön"}, {"esc", "geri"}}
}

func (s *hostOrderScreen) save(a *App) {
	a.cfg.HostOrder = append([]string(nil), s.order...)
	_ = a.cfg.Save()
}

func (s *hostOrderScreen) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok || len(s.order) == 0 {
		return nil
	}
	h := s.order[s.cur]
	switch k.String() {
	case "esc", "q":
		a.pop()
	case "up", "k":
		s.cur = max(0, s.cur-1)
	case "down", "j":
		s.cur = clamp(s.cur+1, 0, len(s.order)-1)
	case "[", "K", "shift+up":
		if s.cur > 0 {
			s.order[s.cur-1], s.order[s.cur] = s.order[s.cur], s.order[s.cur-1]
			s.cur--
			s.save(a)
		}
	case "]", "J", "shift+down":
		if s.cur < len(s.order)-1 {
			s.order[s.cur+1], s.order[s.cur] = s.order[s.cur], s.order[s.cur+1]
			s.cur++
			s.save(a)
		}
	case "n":
		if containsFold(a.cfg.NeverHosts, h) {
			a.cfg.NeverHosts = withoutFold(a.cfg.NeverHosts, h)
		} else {
			a.cfg.NeverHosts = append(a.cfg.NeverHosts, h)
			a.cfg.OnlyHosts = withoutFold(a.cfg.OnlyHosts, h) // a host is one or the other
		}
		_ = a.cfg.Save()
		a.flash()
	case "o":
		if containsFold(a.cfg.OnlyHosts, h) {
			a.cfg.OnlyHosts = withoutFold(a.cfg.OnlyHosts, h)
		} else {
			a.cfg.OnlyHosts = append(a.cfg.OnlyHosts, h)
			a.cfg.NeverHosts = withoutFold(a.cfg.NeverHosts, h)
		}
		_ = a.cfg.Save()
		a.flash()
	case "0":
		a.cfg.HostOrder, a.cfg.NeverHosts, a.cfg.OnlyHosts = nil, nil, nil
		_ = a.cfg.Save()
		s.order, s.cur = hostOrder(a), 0
		a.notify("sunucu sırası varsayılana döndü", "ok")
	}
	return nil
}

func (s *hostOrderScreen) view(a *App, w, h int) string {
	cw := min(80, w-6)
	meta := ""
	if n := len(a.cfg.OnlyHosts); n > 0 {
		meta = fmt.Sprintf("yalnızca %d sunucu", n)
	}
	out := pageHeadCol("Sunucu sırası", "hızlı oynatma bu sırayı izler", meta, cw, w, h)
	band := s.gl.row(a, s.cur)
	for i, host := range s.order {
		sel := i == band
		bg, bar := "", sp("  ", cText)
		if sel {
			bg, bar = cBand, sp("▌ ", cAccent)
		}
		var right []span
		switch {
		case containsFold(a.cfg.NeverHosts, host):
			right = append(right, sp("asla ", cErr))
		case containsFold(a.cfg.OnlyHosts, host):
			right = append(right, sp("yalnızca ", cOK))
		case len(a.cfg.OnlyHosts) > 0:
			right = append(right, sp("yedek ", cMuted))
		}
		out = append(out, center(row(cw, bg,
			[]span{bar, sp(fmt.Sprintf("%d. ", i+1), cMuted), {t: host, c: cText, bold: sel}}, right), w))
	}
	out = append(out, "", center(stFaint.Render(strings.Repeat("─", cw)), w))
	note := "Önce kalite tercihi, sonra bu sıra uygulanır. \"Asla\" ve \"yalnızca\" yalnızca otomatik seçimi etkiler: " +
		"uygun sunucuların hiçbiri açılmazsa diğerleri yine denenir. Fansub seçicide her sunucu elle seçilebilir."
	for _, l := range wrap(note, cw) {
		out = append(out, center(fit(stMuted.Render(l), cw), w))
	}
	return strings.Join(out, "\n")
}
