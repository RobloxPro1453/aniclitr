package ui

import (
	"context"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/netx"
)

// Offline mode: at start (and every 15 s while offline) a quick probe
// checks the internet. Without it there is no point in search, feeds or
// discovery — the app opens a calm screen with only what you downloaded,
// and switches back to the normal home by itself when the line returns.

type netMsg struct{ online bool }

const netRecheck = 15 * time.Second

// probeNet: any of a few well-known endpoints answering means online.
func probeNet() tea.Cmd {
	return guard("ağ", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		targets := []string{"https://www.cloudflare.com/cdn-cgi/trace", "https://graphql.anilist.co", "https://www.google.com/generate_204"}
		ok := make(chan bool, len(targets))
		for _, u := range targets {
			u := u
			go func() {
				req, _ := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
				req.Header.Set("User-Agent", netx.UserAgent)
				resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(req)
				if err == nil {
					resp.Body.Close()
				}
				ok <- err == nil
			}()
		}
		for range targets {
			select {
			case good := <-ok:
				if good {
					return netMsg{online: true}
				}
			case <-ctx.Done():
				return netMsg{online: false}
			}
		}
		return netMsg{online: false}
	})
}

func (a *App) onNet(m netMsg) tea.Cmd {
	switch {
	case !m.online && !a.offline:
		a.offline = true
		// the offline screen becomes the root: no search, no feeds
		a.stack = []screen{newOfflineScreen(a)}
		a.drop, a.pal, a.popup = nil, nil, nil
		a.gfx.invalidate()
		return tea.Batch(a.offlineCovers(), tea.Tick(netRecheck, func(time.Time) tea.Msg { return netRetryMsg{} }))
	case !m.online:
		return tea.Tick(netRecheck, func(time.Time) tea.Msg { return netRetryMsg{} })
	case m.online && a.offline:
		a.offline = false
		h := newHome(a)
		a.stack = []screen{h}
		a.notify("internet geldi — her şey yeniden açık", "ok")
		a.flash()
		a.gfx.invalidate()
		return tea.Batch(a.loadFeed(), a.loadSchedule(), a.checkHealth(), a.tick())
	}
	return nil
}

type netRetryMsg struct{}

// offlineCovers loads the covers saved next to the downloads.
func (a *App) offlineCovers() tea.Cmd {
	if !a.cfg.Posters {
		return nil
	}
	var cmds []tea.Cmd
	for _, s := range offlineShelf(a) {
		cmds = append(cmds, a.posters.want(s.poster))
	}
	return tea.Batch(cmds...)
}

// ---- the offline screen: the download shelf, nothing that needs a network ----

type offlineScreen struct {
	dl      *downloads
	checked time.Time
}

func newOfflineScreen(a *App) *offlineScreen {
	return &offlineScreen{dl: newDownloads(), checked: time.Now()}
}

func (o *offlineScreen) crumb() string { return "çevrimdışı" }

func (o *offlineScreen) hints() []hint {
	h := o.dl.hints()
	out := []hint{}
	for _, x := range h {
		if x.key == "esc" && o.dl.openShow == "" {
			continue // nowhere to go back to
		}
		out = append(out, x)
	}
	return append(out, hint{"r", "bağlantıyı dene"}, hint{"ctrl+c", "çık"})
}

func (o *offlineScreen) animating(a *App) bool { return false }

func (o *offlineScreen) update(a *App, msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "r":
			o.checked = time.Now()
			a.notify("bağlantı deneniyor…", "")
			return probeNet()
		case "esc", "q", "backspace":
			if o.dl.openShow == "" {
				return nil // the offline shelf is the root
			}
		case "v":
			return nil // the queue needs the network; stay on the shelf
		}
	}
	return o.dl.update(a, msg)
}

func (o *offlineScreen) view(a *App, w, h int) string {
	o.dl.queue = false
	pad := 2
	inner := w - pad*2
	banner := pill("ÇEVRİMDIŞI", cWarn) + "  " + stSub.Render("İnternet yok — indirdiklerini izleyebilirsin.") +
		stMuted.Render("  Bağlantı gelince ana sayfa kendiliğinden açılır.")
	shows := offlineShelf(a)
	var out []string
	out = append(out, "", strings.Repeat(" ", pad)+truncate(banner, inner), "")
	bodyH := h - len(out)
	var body []string
	if len(shows) == 0 {
		body = []string{"", center(stSub.Render("İndirilmiş bir şey yok."), inner-4), "",
			center(stMuted.Render("İnternet varken bölüm listesinde d ile indirdiklerin burada internetsiz izlenir."), inner-4)}
	} else {
		body = o.dl.libView(a, inner-4, bodyH-2)
	}
	for _, l := range panel(ic("library")+" indirdiklerin", body, inner, bodyH, true, cAccent, "") {
		out = append(out, strings.Repeat(" ", pad)+l)
	}
	return strings.Join(out, "\n")
}
