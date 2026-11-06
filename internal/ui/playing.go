package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/aniskip"
	"aniclitr/internal/extract"
	"aniclitr/internal/player"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

type segsMsg struct {
	id   int
	segs []aniskip.Segment
}

type startedMsg struct {
	id   int
	sess *player.Session
	err  error
}

type playerMsg struct {
	id int
	ev player.Event
	ok bool
}

type adjacentMsg struct {
	id  int
	ep  *source.Episode
	eps []source.Episode
	err error
}

var playID int

type playing struct {
	id       int
	an       source.Anime
	ep       source.Episode
	eps      []source.Episode
	fansub   string
	host     string
	st       *source.Stream
	vurl     string // variant (embed) URL this stream was resolved from
	q        source.Quality
	sess     *player.Session
	segs     []aniskip.Segment
	pos, dur float64
	paused   bool
	started  bool
	err      error
	skipText string
	skipAt   time.Time
	lastSave time.Time
	savedPos float64
	savedAt  time.Time
	counted  bool // library updated for this episode

	// after playback
	countdown time.Time // auto-next deadline
	nextEp    *source.Episode
	nextEps   []source.Episode
	finding   bool
	attempt   int // automatic recoveries so far for this episode
	subs      subsState
}

func newPlaying(an source.Anime, ep source.Episode, eps []source.Episode, fansub, host string, st *source.Stream, q source.Quality) *playing {
	playID++
	return &playing{id: playID, an: an, ep: ep, eps: eps, fansub: fansub, host: host, st: st, q: q}
}

func (p *playing) crumb() string { return "oynatılıyor" }

func (p *playing) hints() []hint {
	if !p.countdown.IsZero() {
		return []hint{{"enter", "şimdi başlat"}, {"esc", "iptal"}}
	}
	if p.subs.open {
		return []hint{{"↑↓", "altyazı"}, {"enter", "yükle"}, {"esc", "kapat"}}
	}
	return []hint{{"space", "duraklat"}, {"←→", "10 sn"}, {"n", "sonraki"}, {"b", "önceki"}, {"u", "altyazı ara"}, {"s", "durdur"}}
}

// While mpv plays, the terminal stays still: mpv's window is on top and
// nobody watches the TUI, so no frames are spent (only mpv's ~2/s progress
// events redraw the card, and the renderer skips unchanged lines).
// Animation is only used around playback: launching and the auto-next countdown.
func (p *playing) animating(a *App) bool {
	if a.top() != screen(p) {
		return false
	}
	return !p.started || !p.countdown.IsZero() || p.finding || p.subs.busy
}

func (p *playing) malID() int {
	if p.an.MalID > 0 {
		return p.an.MalID
	}
	return p.st.MalID
}

func (p *playing) start(a *App) tea.Cmd {
	id, mal, ep, auto := p.id, p.malID(), p.ep.Number, a.cfg.AutoSkip
	// Fetch OP/ED times first (quick, cached by the API), then launch mpv.
	return tea.Batch(a.tick(), a.posters.want(p.an.Poster), run(4*time.Second, func(ctx context.Context) tea.Msg {
		if !auto {
			return segsMsg{id: id}
		}
		segs, _ := aniskip.Fetch(ctx, mal, ep)
		return segsMsg{id: id, segs: segs}
	}))
}

func (p *playing) resumeAt(a *App) float64 {
	e, ok := a.hist.Get(p.an.Key())
	if !ok || e.Season != p.ep.Season || e.Episode != p.ep.Number || e.Watched[store.EpKey(p.ep.Season, p.ep.Number)] {
		return 0
	}
	if e.Duration > 0 && e.Position > e.Duration*0.95 {
		return 0
	}
	return e.Position
}

func (p *playing) launch(a *App) tea.Cmd {
	trimMemory() // mpv takes over; the terminal idles while you watch
	title := p.an.Title
	if !p.an.IsMovie() {
		title = fmt.Sprintf("%s — %d. Bölüm", p.an.Title, p.ep.Number)
		if p.ep.Title != "" && !strings.HasSuffix(p.ep.Title, "Bölüm") {
			title += " · " + p.ep.Title
		}
	}
	opts := player.Options{
		Title:      title,
		Stream:     p.st,
		Quality:    p.q,
		Start:      p.resumeAt(a),
		Speed:      a.playSpeed,
		Fullscreen: a.cfg.Fullscreen,
		Segments:   p.segs,
		ExtraArgs:  a.cfg.ExtraMpvArgs,
		MpvPath:    a.cfg.MpvPath,
		NextLabel:  p.nextLabel(),
		Thumb:      thumbPath(p.an.Key()),
	}
	id := p.id
	return guard("mpv", func() tea.Msg {
		s, err := player.Start(opts)
		return startedMsg{id: id, sess: s, err: err}
	})
}

func wait(id int, s *player.Session) tea.Cmd {
	if s == nil {
		return nil
	}
	return guard("oynatıcı", func() tea.Msg {
		ev, ok := <-s.Events
		return playerMsg{id: id, ev: ev, ok: ok}
	})
}

func (p *playing) save(a *App, final bool) {
	if p.dur <= 0 {
		return
	}
	// watch time for stats: only forward progress at roughly real speed
	if d := p.pos - p.savedPos; p.savedPos > 0 && d > 0 && d < time.Since(p.savedAt).Seconds()+5 {
		a.hist.AddActivity(d)
	}
	p.savedPos, p.savedAt = p.pos, time.Now()
	frac := p.pos / p.dur
	key := store.EpKey(p.ep.Season, p.ep.Number)
	if frac >= watchedAt && !p.counted {
		p.counted = true
		p.libraryProgress(a)
	}
	a.hist.Update(p.an, func(e *store.Entry) {
		e.Season, e.Episode, e.EpTitle = p.ep.Season, p.ep.Number, p.ep.Title
		e.Position, e.Duration = p.pos, p.dur
		e.Fansub, e.Host = p.fansub, p.host
		if frac >= watchedAt {
			e.Watched[key] = true
			delete(e.Progress, key)
		} else if final || frac > 0.02 {
			e.Progress[key] = int(frac * 100)
		}
	})
}

func (p *playing) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case segsMsg:
		if m.id != p.id {
			return nil
		}
		p.segs = m.segs
		return p.launch(a)
	case startedMsg:
		if m.id != p.id {
			return nil
		}
		if m.err != nil {
			p.err = m.err
			p.started = true
			return nil
		}
		p.sess = m.sess
		return wait(p.id, p.sess)
	case playerMsg:
		if m.id != p.id || !m.ok {
			return nil
		}
		// Capture the session before onEvent: EvEnded clears p.sess, and
		// waiting on a nil session crashed the app when mpv was closed.
		sess := p.sess
		return tea.Batch(p.onEvent(a, m.ev), wait(p.id, sess))
	case subsMsg, subGotMsg:
		p.onSubs(a, msg)
		return nil
	case adjacentMsg:
		if m.id != p.id {
			return nil
		}
		p.finding = false
		if m.err != nil || m.ep == nil {
			p.countdown = time.Time{}
			a.notify("sonraki bölüm bulunamadı — seri bitti mi?", "warn")
			a.pop()
			return nil
		}
		p.nextEp, p.nextEps = m.ep, m.eps
		if p.countdown.IsZero() { // explicit n/b: go immediately
			return p.goAdjacent(a)
		}
		return a.tick()
	case tickMsg:
		if !p.countdown.IsZero() && time.Now().After(p.countdown) && p.nextEp != nil && a.top() == p {
			return p.goAdjacent(a)
		}
	case tea.KeyMsg:
		return p.key(a, m)
	}
	return nil
}

func (p *playing) onEvent(a *App, ev player.Event) tea.Cmd {
	if ev.Speed > 0 {
		a.playSpeed = ev.Speed
	}
	switch ev.Kind {
	case player.EvStarted:
		p.started = true
	case player.EvProgress:
		if !p.started || p.savedPos == 0 {
			p.savedPos, p.savedAt = ev.Pos, time.Now()
		}
		p.started = true
		p.pos, p.dur, p.paused = ev.Pos, ev.Dur, ev.Paused
		if time.Since(p.lastSave) > 10*time.Second {
			p.lastSave = time.Now()
			p.save(a, false)
		}
	case player.EvSkipped:
		p.skipText, p.skipAt = ev.Text, time.Now()
	case player.EvEnded:
		p.started = true
		if ev.Err != nil {
			p.err = ev.Err
			if k := extract.KindOf(ev.Err); k.Permanent() {
				a.permFailed[p.vurl] = true
			} else {
				delete(a.resolved, p.vurl) // transient failure: allow re-resolving later
				a.failed[p.vurl] = time.Now()
			}
			// smart recovery: don't make the user pick — try the next source
			if p.attempt < 3 && p.pos < 5 {
				a.notify(p.host+" açılmadı — sıradaki kaynak deneniyor…", "warn")
				pk := newPicker(p.an, p.ep, p.eps, true)
				pk.prefFan, pk.attempt = p.fansub, p.attempt+1
				a.pop()
				return tea.Batch(a.push(pk), pk.start(a))
			}
			return nil
		}
		if ev.Pos > 0 {
			p.pos = ev.Pos
		}
		if ev.Dur > 0 {
			p.dur = ev.Dur
		}
		p.save(a, true)
		p.sess = nil
		switch {
		case ev.Action == "next":
			return p.findAdjacent(a, +1, false)
		case ev.Action == "prev":
			return p.findAdjacent(a, -1, false)
		case a.cfg.AutoNext && !p.an.IsMovie() && (ev.EOF || (p.dur > 0 && p.pos/p.dur >= 0.9)):
			return p.findAdjacent(a, +1, true)
		default:
			if p.dur > 0 {
				a.notify(fmt.Sprintf("kaydedildi · %s / %s", clock(p.pos), clock(p.dur)), "ok")
			}
			if a.top() == screen(p) {
				a.pop()
			}
		}
	}
	return nil
}

// findAdjacent locates the next/previous episode, crossing season borders.
func (p *playing) findAdjacent(a *App, dir int, countdown bool) tea.Cmd {
	p.finding = true
	if countdown {
		p.countdown = time.Now().Add(6 * time.Second)
	}
	for i, e := range p.eps {
		if e.Number == p.ep.Number && e.Season == p.ep.Season {
			if j := i + dir; j >= 0 && j < len(p.eps) {
				ep := p.eps[j]
				id := p.id
				eps := p.eps
				return func() tea.Msg { return adjacentMsg{id: id, ep: &ep, eps: eps} }
			}
		}
	}
	// crossing into the neighbouring season
	var target *source.Season
	for i, s := range p.an.Seasons {
		if s.Number == p.ep.Season {
			if j := i + dir; j >= 0 && j < len(p.an.Seasons) {
				target = &p.an.Seasons[j]
			}
		}
	}
	id := p.id
	if target == nil {
		return func() tea.Msg { return adjacentMsg{id: id} }
	}
	prov := source.Get(p.an.Source)
	an, sn := p.an, target.Number
	return tea.Batch(a.tick(), run(30*time.Second, func(ctx context.Context) tea.Msg {
		eps, err := prov.Episodes(ctx, an, sn)
		if err != nil || len(eps) == 0 {
			return adjacentMsg{id: id, err: err}
		}
		ep := eps[0]
		if dir < 0 {
			ep = eps[len(eps)-1]
		}
		return adjacentMsg{id: id, ep: &ep, eps: eps}
	}))
}

// goAdjacent hands over to a picker that auto-plays with the same fansub/host.
func (p *playing) goAdjacent(a *App) tea.Cmd {
	ep := *p.nextEp
	pk := newPicker(p.an, ep, p.nextEps, true)
	pk.prefFan, pk.prefHost = p.fansub, p.host
	a.pop()
	return tea.Batch(a.push(pk), pk.start(a))
}

func (p *playing) key(a *App, k tea.KeyMsg) tea.Cmd {
	if p.subs.open {
		return p.subKey(a, k)
	}
	if k.String() == "u" && p.sess != nil {
		return p.searchSubs(a)
	}
	if !p.countdown.IsZero() {
		switch k.String() {
		case "enter", " ", "n":
			if p.nextEp != nil {
				return p.goAdjacent(a)
			}
		case "esc", "q", "s":
			p.countdown = time.Time{}
			a.pop()
		}
		return nil
	}
	if p.err != nil {
		switch k.String() {
		case "esc", "q":
			a.pop()
		case "enter", "r":
			// back to the source picker for this episode to try another host
			pk := newPicker(p.an, p.ep, p.eps, false)
			pk.prefFan = p.fansub
			a.pop()
			return tea.Batch(a.push(pk), pk.start(a))
		}
		return nil
	}
	if p.sess == nil {
		return nil
	}
	switch k.String() {
	case " ", "p":
		p.sess.TogglePause()
	case "left", "h":
		p.sess.Seek(-10)
	case "right", "l":
		p.sess.Seek(10)
	case "shift+left", "H":
		p.sess.Seek(-85)
	case "shift+right", "L":
		p.sess.Seek(85)
	case "n":
		p.sess.Next()
	case "b":
		p.sess.Prev()
	case "s", "esc", "q":
		p.sess.Quit()
	}
	return nil
}

func (p *playing) view(a *App, w, h int) string {
	cw := min(84, w-10)
	posterW := 0
	if a.cfg.Posters && cw >= 60 && h >= 16 {
		posterW = min(22, cw/4)
		if a.posters.sixel() {
			posterW = min(16, cw/5)
		}
	}
	tw := cw - posterW
	if posterW > 0 {
		tw -= 3
	}
	var info []string
	acc := a.posters.accent(p.an.Poster, cAccent).hex()
	// status pill
	switch {
	case p.err != nil:
		info = append(info, pill("HATA", cErr))
	case !p.countdown.IsZero():
		info = append(info, pill("BİTTİ", cOK))
	case !p.started:
		info = append(info, pill(spinner(a.frame/2)+" MPV AÇILIYOR", cAccent2))
	case p.paused:
		info = append(info, pill("❚❚ DURAKLATILDI", cWarn))
	default:
		info = append(info, pill("▶ OYNATILIYOR", acc)+stMuted.Render("  mpv penceresinde"))
	}
	info = append(info, "")
	for i, l := range wrap(p.an.Title, tw) {
		if i == 0 {
			info = append(info, gradient(l, []string{acc, cAccent2}, true))
		} else {
			info = append(info, stBold.Render(l))
		}
	}
	epLine := fmt.Sprintf("Sezon %d · Bölüm %d", p.ep.Season, p.ep.Number)
	if p.an.IsMovie() {
		epLine = "Film"
	}
	info = append(info, stText.Render(epLine))
	if p.ep.Title != "" && !strings.HasSuffix(p.ep.Title, "Bölüm") && !p.an.IsMovie() {
		info = append(info, stSub.Italic(true).Render(truncate("“"+p.ep.Title+"”", tw)))
	}
	info = append(info, "")
	meta := []string{stSub.Render(truncate(p.fansub, tw/2)), stSub.Render(p.host), stSub.Render(p.q.Label)}
	if len(p.st.Subtitles) > 0 {
		meta = append(meta, stSub.Render("altyazı: "+p.st.Subtitles[0].Label))
	}
	info = append(info, truncate(strings.Join(meta, stFaint.Render(" · ")), tw))
	segs := aniskip.ForDuration(p.segs, p.dur)
	if len(segs) > 0 {
		var ks []string
		for _, s := range segs {
			ks = append(ks, s.Label())
		}
		info = append(info, stMuted.Render("↷ otomatik atlanacak: "+strings.Join(ks, ", ")))
	}
	info = append(info, "")
	// progress
	frac := 0.0
	if p.dur > 0 {
		frac = p.pos / p.dur
	}
	tl := stText.Render(clock(p.pos)) + stMuted.Render(" / "+clock(p.dur))
	barW := tw - lipgloss.Width(tl) - 2
	info = append(info, progressBar(barW, frac, []string{acc, cAccent2})+"  "+tl)
	// skip-marks under the bar
	if len(segs) > 0 && p.dur > 0 && barW > 10 {
		marks := []rune(strings.Repeat(" ", barW))
		for _, s := range segs {
			for x := int(s.Start / p.dur * float64(barW)); x < int(s.End/p.dur*float64(barW)) && x < barW; x++ {
				if x >= 0 {
					marks[x] = '▔'
				}
			}
		}
		info = append(info, stFaint.Render(string(marks)))
	}
	if p.skipText != "" && time.Since(p.skipAt) < 90*time.Second {
		info = append(info, fg(acc).Render("↷ "+p.skipText+" atlandı"))
	}
	info = append(info, p.subLines(a, tw)...)
	if p.err != nil {
		info = append(info, "", stErr.Render(truncate(shortErr(p.err), tw)),
			stSub.Render("Link süresi dolmuş ya da sunucu yanıt vermiyor olabilir."),
			stText.Bold(true).Render("enter")+stMuted.Render(" başka kaynak seç  ·  ")+stText.Bold(true).Render("esc")+stMuted.Render(" geri"))
	}
	if !p.countdown.IsZero() {
		left := time.Until(p.countdown).Seconds()
		if left < 0 {
			left = 0
		}
		info = append(info, "")
		if p.nextEp != nil {
			info = append(info, stText.Render(fmt.Sprintf("Sıradaki: %d. Bölüm", p.nextEp.Number))+stMuted.Render(fmt.Sprintf("  ·  %.0f sn", left+.5)))
			info = append(info, progressBar(tw, 1-left/6, []string{acc, cAccent2}))
		} else {
			info = append(info, stAccent.Render(spinner(a.frame/2))+stMuted.Render(" sıradaki bölüm aranıyor…"))
		}
	}
	ih := len(info)
	var card []string
	if posterW > 0 {
		ph := posterW * 3 / 4
		bh := max(ih, ph)
		card = hjoin(3, block(a.posters.render(p.an.Poster, posterW, ph, a.frame), posterW, bh), block(info, tw, bh))
	} else {
		card = block(info, tw, ih)
	}
	// frame the card; the border carries a faint tint of the poster colour
	bs := fg(mix(hex(cFaint), hex(acc), .35).hex())
	framed := []string{bs.Render("╭" + strings.Repeat("─", cw+4) + "╮"), bs.Render("│") + strings.Repeat(" ", cw+4) + bs.Render("│")}
	for _, l := range card {
		framed = append(framed, bs.Render("│  ")+fit(l, cw)+bs.Render("  │"))
	}
	framed = append(framed, bs.Render("│")+strings.Repeat(" ", cw+4)+bs.Render("│"), bs.Render("╰"+strings.Repeat("─", cw+4)+"╯"))
	top := max(0, (h-len(framed))/2-1)
	out := make([]string, 0, h)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range framed {
		out = append(out, center(l, w))
	}
	if top+len(framed)+2 < h && p.sess != nil {
		out = append(out, "", center(stMuted.Render("tuşlar mpv penceresinde de çalışır: ")+stText.Render("n")+stMuted.Render(" sonraki · ")+stText.Render("b")+stMuted.Render(" önceki"), w))
	}
	return strings.Join(out, "\n")
}

// libraryProgress files a finished episode into the library: the show moves
// to "izliyor", and the final episode of a finished series completes it
// (with confetti on the next screen).
func (p *playing) libraryProgress(a *App) {
	last := false
	if len(p.eps) > 0 && p.eps[len(p.eps)-1].Number == p.ep.Number && p.an.NextEpisode == "" {
		lastSeason := len(p.an.Seasons) <= 1 || p.an.Seasons[len(p.an.Seasons)-1].Number == p.ep.Season
		if lastSeason && (p.an.EpCount <= 0 || len(p.an.Seasons) > 1 || p.ep.Number >= p.an.EpCount) {
			last = true
		}
	}
	if p.an.IsMovie() {
		last = true
	}
	bumpWatched(a, p.an, p.ep)
	it, inLib := a.libItem(p.an)
	switch {
	case last && it.Status != store.Completed:
		a.libSet(p.an, func(x *store.Item) {
			x.Status = store.Completed
			if len(p.an.Seasons) <= 1 && p.ep.Number > x.EpCount {
				x.EpCount = p.ep.Number
			}
			if x.EpCount > 0 && x.Watched < x.EpCount {
				x.Watched = x.EpCount
			}
		})
		a.anchorOK = false
		a.celebrate(80)
		a.notify("◆ "+p.an.Title+" bitti! kitaplıkta puan ver (ctrl+b)", "ok")
	case !inLib || it.Status == store.Planned || it.Status == store.OnHold:
		a.libSet(p.an, func(x *store.Item) { x.Status = store.Watching })
	default:
		a.libSet(p.an, nil) // refresh metadata / updated time
	}
}

// watchedAt: an episode counts as watched (history ✓, MAL episode count,
// "next" on the continue shelf) once this share of it has been played —
// nobody watches every second of the credits.
const watchedAt = 0.75
