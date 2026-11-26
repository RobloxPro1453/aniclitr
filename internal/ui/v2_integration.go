package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/config"
	"aniclitr/internal/dna"
	"aniclitr/internal/localmedia"
	"aniclitr/internal/media"
	"aniclitr/internal/player"
	"aniclitr/internal/router"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
	"aniclitr/internal/subsync"
	"aniclitr/internal/watchparty"
)

type localScanMsg struct{ count int }

type relayEvMsg struct {
	ev watchparty.RelayEvent
	ok bool
}

type subSyncDoneMsg struct {
	id      int
	outPath string
	res     subsync.AlignmentResult
	err     error
}

type clipDoneMsg struct {
	id   int
	path string
	err  error
}

// initV2 initializes v2.0 local disk index and lightweight predictive preload cache.
func (a *App) initV2() {
	dirs := append([]string{a.dl.Dir()}, a.cfg.LocalDirs...)
	a.local = localmedia.Open(dirs)
	a.preload = router.NewPreloadCache(5 * time.Minute)
	a.chatVisible = true
}

// scanLocalMediaAsync scans configured local folders without blocking the TUI
// and without running any local background server.
func (a *App) scanLocalMediaAsync() tea.Cmd {
	if a.local == nil {
		return nil
	}
	idx := a.local
	return guard("yerel-tarama", func() tea.Msg {
		return localScanMsg{count: idx.Scan()}
	})
}

// localFileFor looks up an episode in the serverless hybrid local disk index.
func (a *App) localFileFor(an source.Anime, season, ep int) (localmedia.EpisodeFile, bool) {
	if a.local == nil || ep <= 0 {
		return localmedia.EpisodeFile{}, false
	}
	return a.local.Lookup(an.AllTitles(), season, ep)
}

// waitRelayParty awaits the next event from a serverless Watch Party 2.0 room.
func (a *App) waitRelayParty() tea.Cmd {
	r := a.relayParty
	if r == nil {
		return nil
	}
	return guard("birlikte-izle-2", func() tea.Msg {
		ev, ok := <-r.Events
		return relayEvMsg{ev: ev, ok: ok}
	})
}

// runAutoSubSync executes the Multi-Parameter Decision Matrix subtitle alignment
// engine against the currently loaded subtitle file and live mpv media, then
// reloads the aligned subtitle seamlessly into mpv.
func (p *playing) runAutoSubSync(a *App) tea.Cmd {
	subPath := p.activeSubPath
	if subPath == "" {
		a.notify("önce y tuşuyla bir altyazı yükleyin", "warn")
		return a.tick()
	}
	mediaURL := p.q.URL
	if mediaURL == "" {
		return nil
	}
	id := p.id
	if p.sess != nil {
		p.sess.ShowOSD("⚙ Altyazı otomatik hizalanıyor…", 4000)
	}
	return run(45*time.Second, func(ctx context.Context) tea.Msg {
		raw, err := os.ReadFile(subPath)
		if err != nil {
			return subSyncDoneMsg{id: id, err: err}
		}
		doc := subsync.Parse(string(raw))
		sig := subsync.ExtractSignals(ctx, mediaURL)
		res := subsync.Align(doc.Cues, sig, subsync.Options{
			MaxShiftSec:  30,
			TryFPSGrid:   true,
			TryPiecewise: true,
		})
		if len(res.Matrix) == 0 {
			return subSyncDoneMsg{id: id, res: res, err: fmt.Errorf("%s", res.SummaryTR)}
		}
		rewritten := subsync.Rewrite(doc, res.MapTime)
		ext := filepath.Ext(subPath)
		if ext == "" {
			ext = "." + string(doc.Format)
		}
		outDir := filepath.Join(config.CacheDir(), "altyazi")
		_ = os.MkdirAll(outDir, 0o755)
		outPath := filepath.Join(outDir, fmt.Sprintf("hizali-%d%s", time.Now().UnixNano(), ext))
		if err := config.WriteAtomic(outPath, []byte(rewritten)); err != nil {
			return subSyncDoneMsg{id: id, err: err}
		}
		return subSyncDoneMsg{id: id, outPath: outPath, res: res}
	})
}

// tryServerlessFailover attempts a live mid-playback failover via pure mpv IPC
// (NO local HTTP server/proxy) when the current stream stalls heavily.
func (p *playing) tryServerlessFailover(a *App) bool {
	if p.sess == nil || !a.cfg.AutoFallback || p.failoverDone {
		return false
	}
	vs := a.variants[varKey(p.an, p.ep)]
	if len(vs) < 2 {
		return false
	}
	var cands []router.FailoverCandidate
	failed := map[string]bool{p.q.URL: true}
	for k := range a.failed {
		failed[k] = true
	}
	for _, v := range vs {
		rs := a.resolved[v.URL]
		if rs == nil || rs.st == nil || len(rs.st.Qualities) == 0 {
			continue
		}
		cands = append(cands, router.FailoverCandidate{
			Host:    v.Host,
			Fansub:  v.Fansub,
			Stream:  rs.st,
			Quality: rs.st.Qualities[0],
		})
	}
	pick, ok := router.PickFailover(cands, failed, router.Default())
	if !ok {
		return false
	}
	p.failoverDone = true
	p.host = pick.Host
	p.st = pick.Stream
	p.q = pick.Quality
	p.sess.SwitchStream(pick.Quality.URL, pick.Stream.Referer, pick.Stream.UserAgent, p.pos)
	a.notify("⚡ Yedek sunucuya geçildi: "+pick.Host, "ok")
	return true
}

// recordISPTelemetry updates the user's personal ISP router with measured
// startup latency, stall count, and real decoded video bitrate.
func (p *playing) recordISPTelemetry(failed bool) {
	if p.host == "" || p.telemetrySent {
		return
	}
	p.telemetrySent = true
	var startupMs int64
	if !p.launchAt.IsZero() && !p.firstFrameAt.IsZero() {
		startupMs = p.firstFrameAt.Sub(p.launchAt).Milliseconds()
	}
	router.Default().Record(router.Sample{
		Host:           p.host,
		ClaimedHeight:  p.q.Height,
		StartupMs:      startupMs,
		StallCount:     p.stallCount,
		RealBitrateBps: p.avgBitrateBps,
		Failed:         failed,
	})
}

// handleV2MpvAction handles v2.0 live in-mpv studio shortcuts:
//   - Shift+B / Ctrl+B: save scene bookmark
//   - Alt+C: export 10s MP4 clip into DownloadDir/Klipler/
//   - Alt+F: capture clean subtitle-free 4K screenshot into DownloadDir/Kareler/
//   - Alt+H: toggle Watch Party 2.0 live ASS chat overlay
func (p *playing) handleV2MpvAction(a *App, ev player.Event) (tea.Cmd, bool) {
	switch ev.Action {
	case "aniclitr-bookmark":
		pos := ev.Pos
		if pos <= 0 {
			pos = p.pos
		}
		a.hist.AddBookmark(p.an, store.Bookmark{
			Season:  p.ep.Season,
			Episode: p.ep.Number,
			Pos:     pos,
			Note:    fmt.Sprintf("Bölüm %s · %s", p.ep.Num(), clock(pos)),
		})
		if p.sess != nil {
			p.sess.ShowOSD("★ Sahne yer imi kaydedildi ("+clock(pos)+")", 2400)
		}
		a.notify("★ sahne yer imi kaydedildi · "+clock(pos), "ok")
		return a.tick(), true

	case "aniclitr-frame4k":
		dir := filepath.Join(a.dl.Dir(), "Kareler")
		_ = os.MkdirAll(dir, 0o755)
		out := filepath.Join(dir, fmt.Sprintf("kare_B%02d_%s.jpg", p.ep.Number, time.Now().Format("150405")))
		if p.sess != nil {
			p.sess.CaptureFrame(out, false)
			p.sess.ShowOSD("✓ Altyazısız 4K kare kaydedildi: "+filepath.Base(out), 2600)
		}
		a.notify("✓ altyazısız kare kaydedildi: "+out, "ok")
		return a.tick(), true

	case "aniclitr-clip":
		id := p.id
		req := media.ClipRequest{
			InputURL:   p.q.URL,
			EndSec:     p.pos,
			Duration:   10,
			OutputDir:  filepath.Join(a.dl.Dir(), "Klipler"),
			SeriesName: p.an.Title,
			Episode:    p.ep.Number,
		}
		if p.st != nil {
			req.Referer = p.st.Referer
			req.UserAgent = p.st.UserAgent
		}
		if p.sess != nil {
			p.sess.ShowOSD("✂ Son 10 sn klip hazırlanıyor…", 2800)
		}
		return run(45*time.Second, func(ctx context.Context) tea.Msg {
			out, err := media.ExportClip(ctx, req)
			return clipDoneMsg{id: id, path: out, err: err}
		}), true

	case "aniclitr-chat-toggle":
		a.chatVisible = !a.chatVisible
		if p.sess != nil {
			if !a.chatVisible {
				p.sess.HideChatOverlay()
				p.sess.ShowOSD("Sohbet katmanı gizlendi (Alt+H ile aç)", 1800)
			} else if a.relayParty != nil {
				ass := watchparty.BuildChatASS(a.relayParty.Code(), a.relayParty.Messages())
				if ass != "" {
					p.sess.ShowChatOverlay(ass)
				}
				p.sess.ShowOSD("Sohbet katmanı açık", 1800)
			}
		}
		return nil, true
	}
	return nil, false
}

// maybeSuggestSeriesScore checks if the user finished the series and has
// episode ratings, displaying an automatic series-score suggestion from their
// exact episode average.
func (p *playing) maybeSuggestSeriesScore(a *App) {
	avg, cnt, sug := a.hist.EpisodeScoreStats(p.an.Key())
	if cnt < 2 || sug <= 0 {
		return
	}
	if it, ok := a.libItem(p.an); ok && it.Score == sug {
		return
	}
	avgStr := strings.ReplaceAll(fmt.Sprintf("%.1f", avg), ".", ",")
	a.notify(fmt.Sprintf("★ Bölüm puanı ortalaması %s (%d böl.) — seri puanını %d olarak işlemek için G", avgStr, cnt, sug), "ok")
}

// renderHeatmap365 draws a compact GitHub-style 365-day watching heatmap.
func renderHeatmap365(a *App, w int) []string {
	cells := a.hist.Heatmap365(time.Now())
	weeks := min(52, max(12, (w-6)/2))
	startIdx := len(cells) - weeks*7
	if startIdx < 0 {
		startIdx = 0
	}
	glyphs := []string{"·", "░", "▒", "▓", "█"}
	cols := []string{cFaint, cMuted, cSky, cAccent2, cOK}
	dayLbls := []string{"Pzt", "   ", "Çar", "   ", "Cum", "   ", "Paz"}

	var out []string
	out = append(out, sectionTitle("365 günlük izleme ısı haritası", w, false))
	for row := 0; row < 7; row++ {
		var b strings.Builder
		b.WriteString(stMuted.Render(dayLbls[row] + " "))
		for wk := 0; wk < weeks; wk++ {
			idx := startIdx + wk*7 + row
			if idx >= len(cells) {
				break
			}
			lvl := cells[idx].Level
			b.WriteString(fg(cols[lvl]).Render(glyphs[lvl] + " "))
		}
		out = append(out, b.String())
	}
	out = append(out, stMuted.Render("    az  ")+fg(cFaint).Render("· ")+fg(cMuted).Render("░ ")+fg(cSky).Render("▒ ")+fg(cAccent2).Render("▓ ")+fg(cOK).Render("█")+stMuted.Render("  maraton"))
	return out
}

// renderTasteDNA draws the local viewing profile & taste summary.
func renderTasteDNA(a *App, w int) []string {
	prof := dna.Build(a.lib.All(), nil)
	var out []string
	out = append(out, sectionTitle("izleme profili ve zevk analizi", w, false))
	out = append(out, fg(cAccent).Bold(true).Render("◈ "+prof.SignatureLine))
	meta := fmt.Sprintf("tamamlama %%%.0f  ·  bırakma %%%.0f  ·  sapma σ=%.2f",
		prof.CompletionRate*100, prof.DropRate*100, prof.StdDevScore)
	out = append(out, stMuted.Render(meta))
	if len(prof.TopGenres) > 0 {
		var chips []string
		for i, g := range prof.TopGenres {
			if i >= 5 {
				break
			}
			chips = append(chips, pill(fmt.Sprintf("%s · ★%.1f (%d)", g.Name, g.AvgScore, g.Count), cAccent2))
		}
		out = append(out, wrapChips(chips, w)...)
	}
	return out
}
