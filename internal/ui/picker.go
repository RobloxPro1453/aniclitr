package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/choose"
	"aniclitr/internal/diag"
	"aniclitr/internal/download"
	"aniclitr/internal/extract"
	"aniclitr/internal/match"
	"aniclitr/internal/source"
)

type pickerWaitMsg struct{ fCur int }

// group is one fansub; uploads credited differently ("Metincix" vs
// "Çevirmen: Metincix / Dizgi: Metincix") are merged under the short name.
type group struct {
	name    string
	credits []string
	vs      []source.Variant
}

// prow is one selectable line in the hosts column.
type prow struct {
	vi int // variant index within group
	qi int // quality index, -1 for the host line itself
}

type picker struct {
	an       source.Anime
	ep       source.Episode
	eps      []source.Episode
	groups   []group
	err      error
	fCur     int
	fOff     int
	hCur     int
	focus    int // 0 fansubs, 1 hosts
	auto     bool
	pending  bool // play as soon as a host in the current group resolves
	pendHost int  // -1: any host; otherwise a specific variant index
	prefFan  string
	prefHost string
	attempt  int // automatic recoveries (from a failed playback)
	// dlMode: choosing what to download (1 = this episode, 2 = the season)
	dlMode          int
	dlSeason        []source.Episode
	tried           map[int]bool
	firstResolvedAt time.Time
	waitScheduled   bool
	waitRecovery    bool
	alts            []source.Anime
}

// recentlyFailed reports whether this variant should be skipped by automatic
// selection: permanently if it failed with a permanent upstream error
// (deleted, copyright, 404…), or for 10 minutes after a playback failure.
func recentlyFailed(a *App, url string) bool {
	if a.permFailed[url] {
		return true
	}
	if st := a.resolved[url]; st != nil && st.err != nil && st.kind.Permanent() {
		return true
	}
	t, ok := a.failed[url]
	return ok && time.Since(t) < 10*time.Minute
}

func newPicker(an source.Anime, ep source.Episode, eps []source.Episode, auto bool) *picker {
	return &picker{an: an, ep: ep, eps: eps, auto: auto, pendHost: -1}
}

// queue hands the choice to the download manager and goes back.
func (p *picker) queue(a *App, req download.Request) tea.Cmd {
	if p.dlMode == 2 && len(p.dlSeason) > 0 {
		n := 0
		for _, ep := range p.dlSeason {
			if _, ok := a.dl.For(p.an.Key(), ep.Season, ep.Number); ok {
				continue // already queued or on disk
			}
			r := download.Request{Anime: p.an, Ep: ep, PrefFansub: req.PrefFansub, PrefQual: req.PrefQual, Strict: true}
			if r.PrefQual == "" {
				r.PrefQual = a.cfg.Quality
			}
			a.dl.Add(r)
			n++
		}
		a.notify(fmt.Sprintf("↓ %d bölüm “%s” ile sıraya eklendi · ctrl+d", n, shortFansub(req.PrefFansub)), "ok")
		a.flash()
	} else {
		a.queueDownload(req)
	}
	a.pop()
	return a.tick()
}

func (p *picker) crumb() string {
	if p.an.IsMovie() {
		return "Film"
	}
	return fmt.Sprintf("S%d · B%d", p.ep.Season, p.ep.Number)
}

func (p *picker) hints() []hint {
	if p.dlMode > 0 {
		if p.focus == 0 {
			return []hint{{"enter", "bu fansubla indir"}, {"→", "kaynak/kalite seç"}, {"↑↓", "fansub"}, {"esc", "vazgeç"}}
		}
		return []hint{{"enter", "bu kaynak/kaliteyle indir"}, {"←", "fansublar"}, {"↑↓", "kaynak"}, {"esc", "vazgeç"}}
	}
	if p.focus == 0 {
		return []hint{{"enter", "en iyi kaynakla izle"}, {"d", "bu fansubla indir"}, {"→", "kaynak/kalite seç"}, {"u", "bağlantıları göster"}, {"r", "kurtarma"}, {"↑↓", "fansub"}, {"esc", "geri"}}
	}
	return []hint{{"enter", "bu kaliteyle izle"}, {"d", "bunu indir"}, {"u", "bağlantıları göster"}, {"r", "kurtarma"}, {"←", "fansublar"}, {"↑↓", "kaynak"}, {"esc", "geri"}}
}

func (p *picker) animating(a *App) bool {
	if p.groups == nil && p.err == nil {
		return true
	}
	for _, g := range p.groups {
		for _, v := range g.vs {
			if st := a.resolved[v.URL]; st != nil && st.loading {
				return true
			}
		}
	}
	return false
}

func (p *picker) start(a *App) tea.Cmd {
	a.pruneResolved()
	if e, ok := a.hist.Get(p.an.Key()); ok {
		if p.prefFan == "" {
			p.prefFan = e.Fansub
		}
		if p.prefHost == "" {
			p.prefHost = e.Host
		}
	}
	cmd := a.fetchVariants(p.an, p.ep)
	if vs, ok := a.variants[varKey(p.an, p.ep)]; ok && vs != nil {
		return tea.Batch(p.build(a, vs), a.tick())
	}
	return tea.Batch(cmd, a.tick())
}

func (p *picker) build(a *App, vs []source.Variant) tea.Cmd {
	idx := map[string]int{}
	p.groups = nil
	for _, v := range vs {
		short := shortFansub(v.Fansub)
		k := strings.ToLower(short)
		i, ok := idx[k]
		if !ok {
			i = len(p.groups)
			idx[k] = i
			p.groups = append(p.groups, group{name: short})
		}
		g := &p.groups[i]
		g.vs = append(g.vs, v)
		if v.Fansub != short && !contains(g.credits, v.Fansub) {
			g.credits = append(g.credits, v.Fansub)
		}
	}
	// subtitles before dubs (sites list the dub first); the remembered
	// fansub still wins below
	sort.SliceStable(p.groups, func(i, j int) bool { return !isDub(p.groups[i].name) && isDub(p.groups[j].name) })
	// remembered fansub first, then sort hosts by rule priority (with remembered host first)
	rules := a.selectionRules()
	p.fCur = 0
	for i := range p.groups {
		g := &p.groups[i]
		if p.prefFan != "" && strings.EqualFold(g.name, shortFansub(p.prefFan)) {
			p.fCur = i
		}
		sort.SliceStable(g.vs, func(x, y int) bool {
			if p.prefHost != "" {
				px := strings.EqualFold(g.vs[x].Host, p.prefHost)
				py := strings.EqualFold(g.vs[y].Host, p.prefHost)
				if px != py {
					return px
				}
			}
			return hostOrderRank(g.vs[x].Host, rules.HostOrder) < hostOrderRank(g.vs[y].Host, rules.HostOrder)
		})
	}
	p.firstResolvedAt = time.Time{}
	p.waitScheduled = false
	if p.auto {
		p.pending = true
	}
	return p.resolveCurrent(a)
}

func hostOrderRank(host string, order []string) int {
	h := strings.ToLower(strings.TrimSpace(host))
	for i, want := range order {
		w := strings.ToLower(strings.TrimSpace(want))
		if w != "" && (h == w || strings.Contains(h, w)) {
			return i
		}
	}
	return len(order) + 1
}

func (p *picker) group() *group {
	if p.fCur < 0 || p.fCur >= len(p.groups) {
		return nil
	}
	return &p.groups[p.fCur]
}

// resolveCurrent warms up to 6 hosts of the highlighted fansub at once.
func (p *picker) resolveCurrent(a *App) tea.Cmd {
	g := p.group()
	if g == nil {
		return nil
	}
	var cmds []tea.Cmd
	inflight := 0
	for _, v := range g.vs {
		if st := a.resolved[v.URL]; st != nil && st.loading {
			inflight++
		}
	}
	for _, v := range g.vs {
		if inflight >= 6 {
			break
		}
		if a.resolved[v.URL] == nil && !a.permFailed[v.URL] {
			cmds = append(cmds, a.resolve(p.an.Source, v))
			inflight++
		}
	}
	return tea.Batch(cmds...)
}

func (p *picker) rows(a *App) []prow {
	g := p.group()
	if g == nil {
		return nil
	}
	var out []prow
	for vi, v := range g.vs {
		st := a.resolved[v.URL]
		if st != nil && st.st != nil {
			for qi := range st.st.Qualities {
				out = append(out, prow{vi: vi, qi: qi})
			}
			continue
		}
		out = append(out, prow{vi: vi, qi: -1})
	}
	return out
}

func (p *picker) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case variantsMsg:
		if m.key != varKey(p.an, p.ep) {
			return nil
		}
		if m.err != nil {
			p.err = m.err
			if a.cfg.AutoFallback {
				return p.offerRecovery(a)
			}
			return nil
		}
		return p.build(a, m.vs)
	case altsMsg:
		if m.key == p.an.Key() {
			p.alts = m.alts
			if p.waitRecovery {
				p.waitRecovery = false
				return p.showRecoveryMenu(a)
			}
		}
		return nil
	case resolveMsg:
		g := p.group()
		if g == nil {
			return nil
		}
		mine := false
		for _, v := range g.vs {
			if v.URL == m.url {
				mine = true
			}
		}
		if !mine {
			return nil
		}
		if p.pending {
			return tea.Batch(p.resolveCurrent(a), p.tryPlay(a))
		}
		return p.resolveCurrent(a)
	case pickerWaitMsg:
		if p.pending && m.fCur == p.fCur {
			return p.tryPlay(a)
		}
		return nil
	case tea.KeyMsg:
		return p.key(a, m)
	}
	return nil
}

// tryPlay starts playback using rule-based selection (choose.Best / choose.Exact).
func (p *picker) tryPlay(a *App) tea.Cmd {
	g := p.group()
	if g == nil {
		p.pending = false
		return nil
	}
	if p.pendHost >= 0 {
		v := g.vs[p.pendHost]
		st := a.resolved[v.URL]
		switch {
		case st == nil:
			return a.resolve(p.an.Source, v)
		case st.loading:
			return nil
		case st.st != nil:
			p.pending = false
			return p.play(a, v, st.st, a.pickQuality(st.st))
		default:
			p.pending = false
			a.notify(v.Host+" çalışmıyor: "+shortErr(st.err), "err")
			return nil
		}
	}

	rules := a.selectionRules()
	if p.prefHost != "" {
		rules.HostOrder = append([]string{p.prefHost}, rules.HostOrder...)
	}

	var cands []choose.Candidate
	waiting := false
	for vi, v := range g.vs {
		if recentlyFailed(a, v.URL) {
			continue
		}
		st := a.resolved[v.URL]
		if st == nil {
			continue
		}
		if st.loading {
			waiting = true
			continue
		}
		if st.st != nil {
			for qi, q := range st.st.Qualities {
				if q.Broken {
					continue
				}
				cands = append(cands, choose.Candidate{
					Host:         v.Host,
					Fansub:       v.Fansub,
					QualityLabel: q.Label,
					Height:       q.Height,
					Bitrate:      q.Bitrate,
					Broken:       q.Broken,
					VariantIdx:   vi,
					QualityIdx:   qi,
				})
			}
		}
	}

	if len(cands) > 0 {
		if p.firstResolvedAt.IsZero() {
			p.firstResolvedAt = time.Now()
		}
		if best, ok := choose.Best(cands, rules); ok {
			waitDur := time.Duration(rules.WaitAfterFirstMs) * time.Millisecond
			if choose.Exact(best, rules) || !waiting || time.Since(p.firstResolvedAt) >= waitDur {
				p.pending = false
				bv := g.vs[best.VariantIdx]
				bst := a.resolved[bv.URL].st
				bq := bst.Qualities[best.QualityIdx]
				return p.play(a, bv, bst, bq)
			}
			if !p.waitScheduled {
				p.waitScheduled = true
				rem := waitDur - time.Since(p.firstResolvedAt)
				if rem < 20*time.Millisecond {
					rem = 20 * time.Millisecond
				}
				fCur := p.fCur
				return tea.Tick(rem, func(time.Time) tea.Msg {
					return pickerWaitMsg{fCur: fCur}
				})
			}
			return nil
		}
	}

	if waiting {
		return nil
	}
	// nothing ready and nothing in flight: start the next unresolved one
	for _, v := range g.vs {
		if a.resolved[v.URL] == nil && !recentlyFailed(a, v.URL) {
			return p.resolveCurrent(a)
		}
	}
	// this fansub has nothing that works: move on to the next one by itself
	if p.tried == nil {
		p.tried = map[int]bool{}
	}
	p.tried[p.fCur] = true
	for i := range p.groups {
		if !p.tried[i] {
			a.notify(g.name+" açılmadı — "+p.groups[i].name+" deneniyor…", "warn")
			p.fCur, p.hCur = i, 0
			p.firstResolvedAt = time.Time{}
			p.waitScheduled = false
			return tea.Batch(p.resolveCurrent(a), a.tick())
		}
	}
	p.pending = false
	if a.cfg.AutoFallback {
		return p.offerRecovery(a)
	}
	a.notify("hiçbir fansubun kaynağı açılmadı — biraz sonra tekrar dene ya da s ile başka siteye geç", "err")
	return nil
}

func (p *picker) play(a *App, v source.Variant, st *source.Stream, q source.Quality) tea.Cmd {
	g := p.group()
	pl := newPlaying(p.an, p.ep, p.eps, g.name, v.Host, st, q)
	pl.vurl = v.URL
	pl.attempt = p.attempt
	// replace the picker with the player; returning lands on the episode list
	a.pop()
	return tea.Batch(a.push(pl), pl.start(a))
}

func (p *picker) key(a *App, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "u":
		return p.showLinks(a)
	case "r":
		return p.offerRecovery(a)
	case "esc", "q", "backspace":
		if p.focus == 1 {
			p.focus = 0
			return nil
		}
		a.pop()
		return nil
	}
	if p.focus == 0 {
		switch k.String() {
		case "up", "k":
			if p.fCur > 0 {
				p.fCur--
				p.hCur, p.pending = 0, false
				return p.resolveCurrent(a)
			}
		case "down", "j":
			if p.fCur < len(p.groups)-1 {
				p.fCur++
				p.hCur, p.pending = 0, false
				return p.resolveCurrent(a)
			}
		case "right", "l", "tab":
			if p.group() != nil {
				p.focus = 1
			}
		case "enter", " ":
			if g := p.group(); g != nil && p.dlMode > 0 {
				return p.queue(a, download.Request{Anime: p.an, Ep: p.ep, PrefFansub: g.name, Strict: true})
			}
			if p.group() != nil {
				p.pending, p.pendHost = true, -1
				return tea.Batch(p.tryPlay(a), a.tick())
			}
		case "d":
			if g := p.group(); g != nil {
				// the manager resolves the link when the job starts (links expire)
				return p.queue(a, download.Request{Anime: p.an, Ep: p.ep, PrefFansub: g.name, Strict: true})
			}
		}
		return nil
	}
	rows := p.rows(a)
	switch k.String() {
	case "up", "k":
		p.hCur = max(0, p.hCur-1)
	case "down", "j":
		p.hCur = clamp(p.hCur+1, 0, len(rows)-1)
	case "left", "h", "shift+tab":
		p.focus = 0
	case "d":
		if p.hCur < len(rows) {
			r := rows[p.hCur]
			v := p.group().vs[r.vi]
			req := download.Request{Anime: p.an, Ep: p.ep, Variant: &v, PrefFansub: p.group().name, Strict: true}
			if st := a.resolved[v.URL]; r.qi >= 0 && st != nil && st.st != nil {
				req.PrefQual = st.st.Qualities[r.qi].Label
			}
			return p.queue(a, req)
		}
	case "enter", " ":
		if p.hCur >= len(rows) {
			return nil
		}
		r := rows[p.hCur]
		g := p.group()
		v := g.vs[r.vi]
		st := a.resolved[v.URL]
		if p.dlMode > 0 {
			req := download.Request{Anime: p.an, Ep: p.ep, Variant: &v, PrefFansub: g.name, Strict: true}
			if r.qi >= 0 && st != nil && st.st != nil {
				req.PrefQual = st.st.Qualities[r.qi].Label
			}
			return p.queue(a, req)
		}
		if r.qi >= 0 && st != nil && st.st != nil {
			return p.play(a, v, st.st, st.st.Qualities[r.qi])
		}
		if st != nil && st.err != nil {
			if st.kind.Permanent() || a.permFailed[v.URL] {
				a.notify(v.Host+" çalışmıyor: "+shortErr(st.err), "err")
				return nil
			}
			delete(a.resolved, v.URL) // transient error: allow retry
		}
		p.pending, p.pendHost = true, r.vi
		return tea.Batch(p.tryPlay(a), a.tick())
	}
	return nil
}

func (p *picker) view(a *App, w, h int) string {
	pad := 2
	inner := w - pad*2
	var out []string
	// heading: the anime, then which episode and what this page asks
	meta := ""
	switch p.dlMode {
	case 1:
		meta = pill("↓ İNDİRME", cAccent)
	case 2:
		meta = pill(fmt.Sprintf("↓ TOPLU İNDİRME · %d BÖLÜM", len(p.dlSeason)), cAccent)
	}
	epl := fmt.Sprintf("%d. Bölüm", p.ep.Number)
	if p.dlMode == 2 && len(p.dlSeason) > 1 {
		epl = fmt.Sprintf("%d. – %d. Bölümler (%d bölüm)", p.dlSeason[0].Number, p.dlSeason[len(p.dlSeason)-1].Number, len(p.dlSeason))
	} else if p.ep.Season > 1 || (len(p.an.Seasons) > 1) {
		epl = fmt.Sprintf("Sezon %d · Bölüm %d", p.ep.Season, p.ep.Number)
	}
	if p.an.IsMovie() {
		epl = "Film"
	}
	sub := stText.Render(epl)
	if p.dlMode != 2 && p.ep.Title != "" && !strings.HasSuffix(p.ep.Title, "Bölüm") && !p.an.IsMovie() {
		sub += stMuted.Render("  — " + p.ep.Title)
	}
	sub += stFaint.Render("  ·  ") + stMuted.Render("önce çeviri grubunu, ardından kaynağı seçin")
	out = append(out, pageHeadIn(truncate(p.an.Title, inner*2/3), sub, meta, inner, h)...)
	bodyH := h - len(out)
	switch {
	case p.err != nil:
		out = append(out, stErr.Render("✗ seçenekler alınamadı: ")+stMuted.Render(p.err.Error()), stMuted.Render("esc ile geri dön"))
	case p.groups == nil:
		out = append(out, stAccent.Render(spinner(a.frame/2))+stMuted.Render(" fansub ve kaynaklar aranıyor…"))
	default:
		lw := min(44, inner*2/5)
		rw := inner - lw - kitGap
		left := p.fansubPane(a, lw, bodyH)
		right := p.hostPane(a, rw, bodyH)
		out = append(out, columns(left, lw, right, rw, bodyH)...)
	}
	for i := range out {
		out[i] = strings.Repeat(" ", pad) + out[i]
	}
	return strings.Join(out, "\n")
}

func (p *picker) fansubPane(a *App, w, h int) []string {
	out := []string{sectionTitle(fmt.Sprintf("çeviri · %d", len(p.groups)), w, p.focus == 0), ""}
	rows := h - 2
	p.fOff = scroll(p.fCur, p.fOff, rows, len(p.groups))
	for i := p.fOff; i < len(p.groups) && len(out) < h; i++ {
		g := p.groups[i]
		sel := i == p.fCur
		bg, bar, tc := "", sp("  ", cText), cText
		if sel {
			bar, tc = sp("▌ ", cAccent), cAccent
			if p.focus == 0 {
				bg = cBand
			} else {
				bg = cSurface
			}
		}
		// summary of this fansub's hosts: ✓ works, ✗ dead, · unknown
		var dots []span
		ok := 0
		for _, v := range g.vs {
			st := a.resolved[v.URL]
			switch {
			case st == nil:
				dots = append(dots, sp("·", cFaint))
			case st.loading:
				dots = append(dots, sp(spinner(a.frame/2), cAccent))
			case st.st != nil:
				ok++
				dots = append(dots, sp("●", cOK))
			default:
				dots = append(dots, sp("●", cErr))
			}
		}
		if len(dots) > 8 {
			dots = append(dots[:8], sp("+", cMuted))
		}
		dots = append(dots, sp(" ", cText))
		name := g.name
		if p.prefFan != "" && strings.EqualFold(g.name, shortFansub(p.prefFan)) {
			name += " ★"
		}
		out = append(out, row(w, bg, []span{bar, span{t: name, c: tc, bold: sel}}, dots))
	}
	if src := source.Get(p.an.Source); src != nil && src.Info().Lang == "en" && len(out) < h-4 {
		out = append(out, "", stWarn.Render("! ")+stSub.Render("İngilizce kaynak."),
			stMuted.Render(truncate("Türkçe altyazı için esc, sonra s:", w)),
			stMuted.Render(truncate(turkishSources(), w)))
	}
	if g := p.group(); g != nil && len(out) < h-3 {
		out = append(out, "", stFaint.Render("── ")+stMuted.Render(fmt.Sprintf("%d kaynak", len(g.vs))))
		for _, c := range g.credits {
			for _, l := range wrap(c, w-2) {
				if len(out) < h {
					out = append(out, stMuted.Italic(true).Render(l))
				}
			}
		}
	}
	return out
}

func (p *picker) hostPane(a *App, w, h int) []string {
	out := []string{sectionTitle("kaynak · kalite", w, p.focus == 1), ""}
	g := p.group()
	if g == nil {
		return out
	}
	rows := p.rows(a)
	p.hCur = clamp(p.hCur, 0, len(rows)-1)
	ri := 0
	for vi, v := range g.vs {
		st := a.resolved[v.URL]
		host := v.Host
		if host == "" {
			host = "Oynatıcı"
		}
		// host header line
		var status []span
		switch {
		case st == nil:
			status = []span{sp("enter: dene ", cMuted)}
		case st.loading:
			status = []span{sp(spinner(a.frame/2)+" çözülüyor ", cAccent)}
		case st.st != nil:
			status = []span{sp("✓ hazır ", cOK)}
		default:
			status = []span{sp("✗ "+truncate(shortErr(st.err), w/2)+" ", cErr)}
		}
		if st != nil && st.st != nil {
			out = append(out, row(w, "", []span{sp("  ", cText), spb(host, cText)}, status))
			for qi, q := range st.st.Qualities {
				sel := p.focus == 1 && ri == p.hCur
				bg, bar, tc := "", sp("    ", cText), cSub
				if sel {
					bg, bar, tc = cBand, sp("  ▌ ", cAccent), cAccent
				}
				label := q.DisplayLabel()
				right := []span{}
				if q.Broken {
					right = append(right, sp("bozuk  ", cErr))
				}
				if s := humanSize(q.Size); s != "" {
					right = append(right, sp(s, cMuted))
				}
				if qi == 0 && !q.Broken {
					right = append(right, sp("  en iyi", cMuted))
				}
				if len(st.st.Subtitles) > 0 && qi == 0 {
					right = append(right, sp("  altyazı: "+st.st.Subtitles[0].Label, cMuted))
				}
				right = append(right, sp(" ", cText))
				out = append(out, row(w, bg, []span{bar, span{t: label, c: tc, bold: sel}}, right))
				ri++
			}
		} else {
			sel := p.focus == 1 && ri == p.hCur
			bg, bar, tc := "", sp("  ", cText), cText
			if sel {
				bg, bar, tc = cBand, sp("▌ ", cAccent), cAccent
			}
			out = append(out, row(w, bg, []span{bar, span{t: host, c: tc, bold: true}}, status))
			ri++
		}
		_ = vi
	}
	if p.pending {
		out = append(out, "", stAccent.Render(spinner(a.frame/2))+stSub.Render(" en iyi kaynak hazırlanıyor, birazdan mpv açılacak…"))
	}
	// keep the cursor visible in long legacy lists
	if len(out) > h {
		target := 2
		cnt := 0
		for i, l := range out {
			if strings.Contains(l, "▌") {
				target = i
				break
			}
			cnt++
		}
		start := clamp(target-h/2, 0, len(out)-h)
		out = append(out[:2:2], out[max(2, start+2):]...)
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	if k := extract.KindOf(err); k != extract.KindUnknown {
		if msg := k.Message(); msg != "" {
			return msg
		}
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "403"):
		return "erişim engellendi (403)"
	case strings.Contains(s, "404"), strings.Contains(s, "not been found"), strings.Contains(s, "removed"), strings.Contains(s, "deleted"):
		return "video silinmiş"
	case strings.Contains(s, "Unsupported URL"):
		return "desteklenmeyen oynatıcı"
	case strings.Contains(s, "deadline"), strings.Contains(s, "timeout"):
		return "zaman aşımı"
	}
	return s
}

func isDub(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "dublaj") || strings.Contains(n, "dub")
}

// showLinks opens a dropdown inspecting resolved URLs (tokens masked).
func (p *picker) showLinks(a *App) tea.Cmd {
	g := p.group()
	if g == nil {
		return nil
	}
	var items []dropItem
	for _, v := range g.vs {
		st := a.resolved[v.URL]
		host := v.Host
		if host == "" {
			host = "Oynatıcı"
		}
		items = append(items, dropItem{group: host + " · " + diag.RedactURL(v.URL)})
		if st == nil {
			items = append(items, dropItem{icon: "·", label: "henüz çözülmedi", dim: true})
			continue
		}
		if st.loading {
			items = append(items, dropItem{icon: "◌", label: "çözülüyor…", dim: true})
			continue
		}
		if st.err != nil {
			items = append(items, dropItem{icon: "✗", color: cErr, label: shortErr(st.err), note: st.kind.Message()})
			continue
		}
		if st.st != nil {
			for _, q := range st.st.Qualities {
				v := v
				st := st.st
				q := q
				ic, col := "✓", cOK
				if q.Broken {
					ic, col = "✗", cErr
				}
				items = append(items, dropItem{
					icon:  ic,
					color: col,
					label: q.DisplayLabel() + "  " + truncate(diag.RedactURL(q.URL), 38),
					note:  humanSize(q.Size),
					run: func(a *App) tea.Cmd {
						return p.play(a, v, st, q)
					},
				})
			}
		}
	}
	a.openList("çözülen bağlantılar · "+g.name, items)
	return a.tick()
}

// offerRecovery finds alternative sources and presents the recovery menu
// (switch to another source, reset mapping and re-search, or return to episodes).
func (p *picker) offerRecovery(a *App) tea.Cmd {
	if len(p.alts) == 0 {
		for _, s := range a.stack {
			if as, ok := s.(*animeScreen); ok {
				for _, cand := range as.srcs {
					if cand.Source != p.an.Source {
						p.alts = append(p.alts, cand)
					}
				}
			}
		}
	}
	if len(p.alts) > 0 {
		return p.showRecoveryMenu(a)
	}
	p.waitRecovery = true
	a.notify("bu kaynakta açılmadı — diğer kaynaklar kontrol ediliyor…", "warn")
	return tea.Batch(a.tick(), a.findAlts(p.an))
}

func (p *picker) showRecoveryMenu(a *App) tea.Cmd {
	var items []dropItem
	if len(p.alts) > 0 {
		items = append(items, dropItem{group: "diğer kaynaklarda devam et"})
		for _, alt := range p.alts {
			if alt.Source == p.an.Source {
				continue
			}
			alt := alt
			srcName := alt.Source
			if prov := source.Get(alt.Source); prov != nil {
				srcName = prov.Info().Name
			}
			items = append(items, dropItem{
				icon:  "▶",
				color: cOK,
				label: fmt.Sprintf("%s üzerinden aç (%s)", srcName, p.crumb()),
				note:  alt.Title,
				run: func(a *App) tea.Cmd {
					a.pop() // close picker
					if as, ok := a.top().(*animeScreen); ok {
						*as = *newAnime(alt, p.alts, p.ep.Season, p.ep.Number)
						a.notify("kaynak değiştirildi: "+srcName, "ok")
						return as.start(a)
					}
					sc := newAnime(alt, nil, p.ep.Season, p.ep.Number)
					return tea.Batch(a.push(sc), sc.start(a))
				},
			})
		}
	}
	items = append(items,
		dropItem{group: "kurtarma işlemleri"},
		dropItem{
			icon:  "↻",
			color: cAccent,
			label: "Eşlemeyi sıfırla ve yeniden ara",
			note:  p.an.Title,
			run: func(a *App) tea.Cmd {
				match.Default().Delete(p.an.Key())
				a.pop()
				r := newResults(p.an.Title)
				a.notify("eşleme sıfırlandı — yeniden aranıyor", "ok")
				return tea.Batch(a.push(r), r.start(a))
			},
		},
		dropItem{
			icon:  "←",
			label: "Bölüm listesine dön",
			run: func(a *App) tea.Cmd {
				a.pop()
				return a.tick()
			},
		},
	)
	a.openList("kurtarma · "+p.crumb(), items)
	return a.tick()
}
