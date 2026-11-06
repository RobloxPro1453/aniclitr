package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
)

// dlMsg: the download manager changed something (progress, state).
type dlMsg struct{}

// waitDL re-arms on every change signal (no polling, no ticking).
func (a *App) waitDL() tea.Cmd {
	if a.dl == nil {
		return nil
	}
	ch := a.dl.Updates
	return func() tea.Msg { <-ch; return dlMsg{} }
}

// onDL announces finished / failed downloads.
func (a *App) onDL() {
	for _, j := range a.dl.Jobs() {
		prev, seen := a.dlSeen[j.ID]
		a.dlSeen[j.ID] = j.State
		if !seen || prev == j.State {
			continue
		}
		label := fmt.Sprintf("%s S%d·B%d", j.Title, j.Season, j.Episode)
		switch j.State {
		case download.Done:
			jj := j
			a.addNotice(notice{key: fmt.Sprintf("dl:%d:done", j.ID), kind: "ok", text: "indirildi: " + label,
				action: func(a *App) tea.Cmd { return a.playLocal(jj, nil) }})
			a.notify("↓ indirildi: "+label, "ok")
			a.anchorOK = false
			if a.motion() {
				a.fx.sparks(float64(a.w-20), float64(a.h-4), 14, []string{cOK, cSky, cAccent}, j.ID)
			}
		case download.Failed:
			a.addNotice(notice{key: fmt.Sprintf("dl:%d:fail:%d", j.ID, time.Now().Unix()/60), kind: "err", text: "indirilemedi: " + label,
				action: func(a *App) tea.Cmd { return a.openDownloads() }})
			a.notify("↓ indirilemedi: "+label+" — "+j.Err, "err")
		}
	}
}

// queueDownload adds a request and tells the user.
func (a *App) queueDownload(r download.Request) {
	if r.PrefQual == "" {
		r.PrefQual = a.cfg.Quality
	}
	j, added := a.dl.Add(r)
	if !added {
		a.notify(fmt.Sprintf("zaten listede: S%d·B%d (%s)", j.Season, j.Episode, j.State), "warn")
		return
	}
	a.dlSeen[j.ID] = j.State
	a.notify(fmt.Sprintf("↓ indirme sırasına eklendi: S%d·B%d", r.Ep.Season, r.Ep.Number), "ok")
	a.flash()
}

// playLocal plays a downloaded file (works offline).
func (a *App) playLocal(j download.Job, eps []source.Episode) tea.Cmd {
	st := &source.Stream{Qualities: []source.Quality{{Label: j.Quality, URL: j.Path}}, MalID: j.MalID}
	ep := source.Episode{ID: j.EpID, Season: j.Season, Number: j.Episode, Title: j.EpTitle}
	pl := newPlaying(j.Anime(), ep, eps, j.Fansub, "çevrimdışı", st, st.Qualities[0])
	a.notify("çevrimdışı oynatılıyor", "ok")
	return tea.Batch(a.push(pl), pl.start(a))
}

func dlIcon(state string) (string, string) {
	switch state {
	case download.Done:
		return "✓", cOK
	case download.Running:
		return "↓", cAccent
	case download.Resolving:
		return "◌", cAccent2
	case download.Queued:
		return "◷", cMuted
	case download.Failed:
		return "×", cErr
	case download.Canceled:
		return "−", cMuted
	}
	return "•", cSub
}

func fmtSpeed(bps float64) string {
	if bps <= 0 {
		return ""
	}
	return humanSize(int64(bps)) + "/s"
}

// ---- screen ----

type downloads struct {
	cur, off    int
	gl          glide
	confirm     int
	confirmShow string
	confirmAt   time.Time

	queue      bool // false: offline library (covers), true: the download queue
	libCur     int
	libOff     int
	lastW      int
	openShow   string // anime key whose episodes are open
	epCur      int
	speedHist  []float64
	jobSpeeds  map[int][]float64
	lastSample time.Time
}

func newDownloads() *downloads { return &downloads{confirm: -1, jobSpeeds: map[int][]float64{}} }

// newDownloadsFor opens on the queue when nothing finished yet but
// something is downloading (the shelf would be empty).
func newDownloadsFor(a *App) *downloads {
	d := newDownloads()
	d.queue = len(offlineShelf(a)) == 0 && a.dl.Active() > 0
	return d
}

// openDownloads brings the downloads screen forward.
func (a *App) openDownloads() tea.Cmd {
	if a.dl != nil {
		a.dl.ScanDisk()
	}
	for i, s := range a.stack {
		if _, ok := s.(*downloads); ok {
			a.stack = a.stack[:i+1]
			a.slide(-1)
			return a.tick()
		}
	}
	return tea.Batch(a.push(newDownloadsFor(a)), a.offlineCovers())
}

func (d *downloads) crumb() string { return "indirilenler" }

func (d *downloads) hints() []hint {
	if !d.queue {
		if d.openShow != "" {
			return []hint{{"enter", "oynat (internetsiz)"}, {"x", "bölümü diskten sil"}, {"X", "tüm seriyi sil"}, {"esc", "raflara dön"}}
		}
		return []hint{{"←→↑↓", "gez"}, {"enter", "aç"}, {"x", "seriyi diskten sil"}, {"v", "indirme sırası"}, {"o", "klasörü aç"}, {"esc", "geri"}}
	}
	return []hint{{"v", "kitaplık"}, {"enter", "oynat"}, {"r", "tekrar dene"}, {"x", "iptal / sıradan gizle"}, {"X", "dosyayla sil"}, {"o", "klasörü aç"}, {"c", "bitenleri temizle"}, {"esc", "geri"}}
}

func (d *downloads) animating(a *App) bool {
	// running bars carry a moving sheen
	return a.top() == screen(d) && a.dl.Active() > 0 && a.motion()
}

func (d *downloads) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if k.String() == "v" || k.String() == "tab" {
		d.queue = !d.queue
		a.gfx.invalidate()
		return tea.Batch(a.tick(), a.offlineCovers())
	}
	if !d.queue {
		if cmd, ok := d.libKey(a, k); ok {
			return cmd
		}
	}
	jobs := a.dl.QueueJobs()
	d.cur = clamp(d.cur, 0, len(jobs)-1)
	switch k.String() {
	case "esc", "q", "backspace":
		a.pop()
		return nil
	case "up", "k":
		d.cur = max(0, d.cur-1)
	case "down", "j":
		d.cur = clamp(d.cur+1, 0, len(jobs)-1)
	case "o":
		dir := a.dl.Dir()
		if d.cur < len(jobs) && jobs[d.cur].Path != "" {
			dir = filepath.Dir(jobs[d.cur].Path)
		}
		_ = exec.Command("explorer", dir).Start()
	case "c":
		for _, j := range jobs {
			if j.State == download.Done || j.State == download.Canceled {
				a.dl.Remove(j.ID, false)
			}
		}
		a.notify("biten indirmeler kuyruktan temizlendi (çevrimdışı kitaplıkta duruyor)", "ok")
	}
	if d.cur >= len(jobs) {
		return a.tick()
	}
	j := jobs[d.cur]
	switch k.String() {
	case "enter", "p":
		if j.State == download.Done {
			a.flash()
			return a.playLocal(j, nil)
		}
		a.notify("henüz inmedi ("+j.State+")", "warn")
	case "r":
		a.dl.Retry(j.ID)
		a.flash()
	case "x", "delete":
		if j.State == download.Queued || j.State == download.Resolving || j.State == download.Running {
			a.dl.Cancel(j.ID)
			a.notify("iptal edildi", "ok")
		} else {
			a.dl.Remove(j.ID, false)
		}
	case "X":
		if d.confirm == j.ID && time.Since(d.confirmAt) < 3*time.Second {
			a.dl.Remove(j.ID, true)
			a.notify("dosya silindi", "ok")
			d.confirm = -1
		} else {
			d.confirm, d.confirmAt = j.ID, time.Now()
			a.notify("dosyayı da silmek için tekrar X", "warn")
		}
	}
	return a.tick()
}

func (d *downloads) view(a *App, w, h int) string {
	pad := 2
	inner := w - pad*2
	jobs := a.dl.Jobs()
	active, done := 0, 0
	var bytes int64
	var speed float64
	for _, j := range jobs {
		switch j.State {
		case download.Done:
			done++
			bytes += j.Total
		case download.Running, download.Resolving, download.Queued:
			active++
			speed += j.Speed
		}
	}
	var stats []string
	stats = append(stats, stBold.Render(fmt.Sprint(active))+stMuted.Render(" aktif"), stBold.Render(fmt.Sprint(done))+stMuted.Render(" bitti"))
	if bytes > 0 {
		stats = append(stats, stBold.Render(humanSize(bytes))+stMuted.Render(" diskte"))
	}
	if a.cfg.SpeedGraph {
		d.sampleSpeeds(jobs, speed, active)
	}
	if speed > 0 {
		if a.cfg.SpeedGraph && len(d.speedHist) > 0 {
			stats = append(stats, fg(cAccent).Render("↓ ")+sparkline(d.speedHist, 12, cAccent, cSky)+" "+stBold.Render(fmtSpeed(speed)))
		} else {
			stats = append(stats, fg(cAccent).Render("↓ ")+stBold.Render(fmtSpeed(speed)))
		}
	}
	sub := "Çevrimdışı izleme için indirilen bölümler"
	if d.queue {
		sub = "İndirme kuyruğu"
	}
	out := pageHeadIn("İndirilenler", sub, strings.Join(stats, stFaint.Render("  ·  ")), inner, h)
	aria, yt := download.Tools()
	tools := []string{}
	if aria {
		tools = append(tools, stOK.Render("●")+stMuted.Render(" aria2c"))
	} else {
		tools = append(tools, stFaint.Render("○")+stMuted.Render(" aria2c yok (dahili indirici)"))
	}
	if yt {
		tools = append(tools, stOK.Render("●")+stMuted.Render(" yt-dlp"))
	}
	foot := stMuted.Render("klasör ") + stSub.Render(truncate(a.dl.Dir(), inner/2)) + stFaint.Render("   ") + strings.Join(tools, "  ")
	bodyH := h - len(out) - 1
	var body []string
	iw := inner - 4
	qjobs := a.dl.QueueJobs()
	if len(qjobs) == 0 {
		body = append(body, "", center(stSub.Render("Kuyrukta bekleyen indirme yok."), iw), "",
			center(stMuted.Render("Bölüm listesinde d: o bölümü indir  ·  D: toplu / aralıklı indir"), iw),
			center(stMuted.Render("v: çevrimdışı kitaplığa geç"), iw))
	} else {
		rowsVisible := max(1, (bodyH-2)/3)
		d.cur = clamp(d.cur, 0, len(qjobs)-1)
		d.off = scroll(d.cur, d.off, rowsVisible, len(qjobs))
		band := d.gl.row(a, d.cur)
		for i := d.off; i < len(qjobs) && i < d.off+rowsVisible; i++ {
			body = append(body, d.item(a, qjobs[i], iw, i == band)...)
		}
	}
	if d.queue {
		out = append(out, panel(ic("bolt")+" kuyruk", body, inner, bodyH, true, cAccent, fmt.Sprintf("%d iş · v: kitaplık", len(qjobs)))...)
	} else {
		title := ic("library") + " çevrimdışı kitaplık"
		out = append(out, panel(title, d.libView(a, inner-4, bodyH-2), inner, bodyH, true, cAccent, fmt.Sprintf("%d anime · v: sıra", len(offlineShelf(a))))...)
	}
	out = append(out, foot)
	for i := range out {
		out[i] = strings.Repeat(" ", pad) + out[i]
	}
	return strings.Join(out, "\n")
}

// item: two lines + a spacer: what / where it is, then the progress bar.
func (d *downloads) item(a *App, j download.Job, w int, sel bool) []string {
	icon, ic := dlIcon(j.State)
	acc := a.posters.accent(j.Poster, cAccent).hex()
	bg, bar := "", sp("  ", cText)
	tc := cText
	if sel {
		bg, bar, tc = cBand, sp("▌ ", acc), acc
	}
	what := fmt.Sprintf("  S%d · B%d", j.Season, j.Episode)
	if strings.EqualFold(j.Kind, "film") {
		what = "  Film"
	}
	var right []span
	if j.Fansub != "" {
		right = append(right, sp(truncate(shortFansub(j.Fansub), 18)+"  ", cMuted))
	}
	if j.Quality != "" {
		right = append(right, sp(j.Quality+"  ", cSub))
	}
	right = append(right, sp(j.State+" ", ic))
	l1 := row(w, bg, []span{bar, sp(icon+" ", ic), span{t: j.Title, c: tc, bold: sel}, sp(what, cSub)}, right)
	if sel {
		l1 = anchor() + l1
	}
	// progress line
	info := ""
	switch j.State {
	case download.Running:
		parts := []string{}
		if j.Total > 0 {
			parts = append(parts, humanSize(j.Done)+" / "+humanSize(j.Total))
		} else if j.Done > 0 {
			parts = append(parts, humanSize(j.Done))
		}
		if s := fmtSpeed(j.Speed); s != "" {
			if a.cfg.SpeedGraph && len(d.jobSpeeds[j.ID]) > 1 {
				parts = append(parts, sparkline(d.jobSpeeds[j.ID], 8, acc, cSky)+" "+s)
			} else {
				parts = append(parts, s)
			}
		}
		if j.Conns > 1 {
			parts = append(parts, fmt.Sprintf("%d bağlantı", j.Conns))
		}
		if j.ETA > 0 {
			parts = append(parts, "kalan "+j.ETA.Round(time.Second).String())
		}
		info = strings.Join(parts, " · ")
	case download.Done:
		info = humanSize(j.Total) + " · " + agoLong(j.Finished)
	case download.Failed:
		info = j.Err
	case download.Resolving:
		info = "kaynak çözülüyor…"
	case download.Queued:
		info = "sırada bekliyor"
	}
	infoW := min(46, w/2)
	barW := w - 4 - infoW - 2
	pb := progressBar(barW, j.Frac(), []string{acc, cAccent2})
	if j.State == download.Running && a.motion() {
		pb = sheen(pb, barW, j.Frac(), a.frame)
	}
	if j.State == download.Failed || j.State == download.Canceled {
		pb = stFaint.Render(strings.Repeat("─", barW))
	}
	pct := fmt.Sprintf("%3.0f%%", j.Frac()*100)
	ic2 := cMuted
	if j.State == download.Failed {
		ic2 = cErr
	}
	l2 := "    " + pb + "  " + fg(cText).Render(pct) + "  " + fg(ic2).Render(truncate(info, infoW-6))
	return []string{l1, l2, ""}
}

// sheen sweeps a light band over the filled part of a running bar.
func sheen(_ string, w int, frac float64, frame int) string {
	filled := int(frac * float64(w))
	pos := frame % (w + 12)
	var b strings.Builder
	for x := 0; x < w; x++ {
		t := float64(x) / float64(max(1, w-1))
		c := gradAt([]string{cAccent, cAccent2, cSky}, t)
		if x < filled {
			if d := x - (pos - 6); d >= 0 && d < 6 {
				c = mix(c, hex("#FFFFFF"), .45*(1-float64(absInt(d-3))/3))
			}
			b.WriteString(fg(c.hex()).Render("█"))
		} else {
			b.WriteString(stFaint.Render("─"))
		}
	}
	return b.String()
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (d *downloads) sampleSpeeds(jobs []download.Job, totalSpeed float64, active int) {
	if active == 0 {
		d.speedHist = nil
		if len(d.jobSpeeds) > 0 {
			d.jobSpeeds = map[int][]float64{}
		}
		return
	}
	if time.Since(d.lastSample) < 550*time.Millisecond && len(d.speedHist) > 0 {
		return
	}
	d.lastSample = time.Now()
	const maxSamples = 20
	d.speedHist = append(d.speedHist, totalSpeed)
	if len(d.speedHist) > maxSamples {
		d.speedHist = d.speedHist[len(d.speedHist)-maxSamples:]
	}
	if d.jobSpeeds == nil {
		d.jobSpeeds = map[int][]float64{}
	}
	for _, j := range jobs {
		if j.State == download.Running && j.Speed > 0 {
			h := append(d.jobSpeeds[j.ID], j.Speed)
			if len(h) > maxSamples {
				h = h[len(h)-maxSamples:]
			}
			d.jobSpeeds[j.ID] = h
		} else if j.State != download.Running {
			delete(d.jobSpeeds, j.ID)
		}
	}
}

var sparkBars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func sparkline(samples []float64, width int, cLo, cHi string) string {
	if width <= 0 || len(samples) == 0 {
		return ""
	}
	if len(samples) > width {
		samples = samples[len(samples)-width:]
	}
	peak := 256.0 * 1024.0 // minimum 256 KB/s floor so tiny Trickle doesn't peg at 100%
	for _, v := range samples {
		if v > peak {
			peak = v
		}
	}
	var b strings.Builder
	for i := 0; i < width-len(samples); i++ {
		b.WriteString(stFaint.Render("▁"))
	}
	for _, v := range samples {
		t := v / peak
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		idx := int(t * float64(len(sparkBars)-1))
		c := mix(hex(cLo), hex(cHi), t).hex()
		b.WriteString(fg(c).Render(string(sparkBars[idx])))
	}
	return b.String()
}
