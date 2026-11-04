package ui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/source"
	"aniclitr/internal/source/animedepo"
)

type srcState struct {
	info    source.Info
	loading bool
	count   int
	err     error
}

type hoverMsg struct{ key string }

// entry is one anime, possibly found on several sources. main is the copy
// from the highest-ranked source (AnimeciX first); alts are the others.
type entry struct {
	main source.Anime
	alts []source.Anime
	keys map[string]bool
}

func (e entry) all() []source.Anime { return append([]source.Anime{e.main}, e.alts...) }

func (e entry) has(src string) bool {
	for _, x := range e.all() {
		if x.Source == src {
			return true
		}
	}
	return false
}

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// titleKeys normalises every title variant so "Box of Goblins" (HiAnime,
// jname "Mouryou no Hako") meets AnimeciX's "Mouryou no Hako".
func titleKeys(a source.Anime) []string {
	var out []string
	for _, t := range []string{a.Title, a.AltTitle} {
		k := strings.TrimSpace(reNonAlnum.ReplaceAllString(animedepo.Fold(t), " "))
		if len(k) >= 3 {
			out = append(out, k)
		}
	}
	return out
}

type results struct {
	query    string
	gen      int
	order    []string // provider ids in rank order
	states   map[string]*srcState
	byProv   map[string][]source.Anime
	items    []entry
	cur      int
	off      int
	descOff  int
	revealAt time.Time
	posterAt tween
	prevPost string
	curPost  string
	gl       glide
}

var searchGen int

func newResults(q string) *results {
	searchGen++
	return &results{query: q, gen: searchGen, states: map[string]*srcState{}, byProv: map[string][]source.Anime{}}
}

func (r *results) crumb() string { return "“" + r.query + "”" }

func (r *results) hints() []hint {
	return []hint{{"enter", "aç"}, {"↑↓", "gez"}, {"a", "kitaplığa ekle"}, {"J K", "özeti kaydır"}, {"/", "yeni arama"}, {"alt+a", "arşiv"}, {"esc", "geri"}}
}

func (r *results) animating(a *App) bool {
	if a.top() != screen(r) {
		return false
	}
	for _, s := range r.states {
		if s.loading {
			return true
		}
	}
	return revealing(r.revealAt, 30, a.motion()) || r.posterAt.active()
}

// start fires one search per enabled provider, concurrently.
func (r *results) start(a *App) tea.Cmd {
	var cmds []tea.Cmd
	r.order = nil
	for _, p := range source.All() {
		in := p.Info()
		if !a.cfg.Enabled(in.ID, in.Legacy) {
			continue
		}
		r.order = append(r.order, in.ID)
		r.states[in.ID] = &srcState{info: in, loading: true}
		p, gen, q := p, r.gen, r.query
		cmds = append(cmds, run(45*time.Second, func(ctx context.Context) tea.Msg {
			res, err := p.Search(ctx, q)
			return searchMsg{gen: gen, src: p.Info().ID, results: res, err: err}
		}))
	}
	cmds = append(cmds, a.tick())
	return tea.Batch(cmds...)
}

func (r *results) rebuild() {
	var keep string
	if r.cur < len(r.items) {
		keep = r.items[r.cur].main.Key()
	}
	r.items = r.items[:0]
	for _, id := range r.order {
	next:
		for _, an := range r.byProv[id] {
			ks := titleKeys(an)
			for i := range r.items {
				e := &r.items[i]
				if e.has(an.Source) {
					continue
				}
				for _, k := range ks {
					if e.keys[k] {
						e.alts = append(e.alts, an)
						for _, k2 := range ks {
							e.keys[k2] = true
						}
						continue next
					}
				}
			}
			e := entry{main: an, keys: map[string]bool{}}
			for _, k := range ks {
				e.keys[k] = true
			}
			r.items = append(r.items, e)
		}
	}
	for i, it := range r.items {
		if it.main.Key() == keep {
			r.cur = i
		}
	}
}

func (r *results) selected() (entry, bool) {
	if r.cur < 0 || r.cur >= len(r.items) {
		return entry{}, false
	}
	return r.items[r.cur], true
}

func (r *results) onMove(a *App) tea.Cmd {
	r.descOff = 0
	it, ok := r.selected()
	if !ok {
		return nil
	}
	var cmds []tea.Cmd
	if a.cfg.Posters {
		for _, x := range it.all() {
			cmds = append(cmds, a.posters.want(x.Poster))
		}
	}
	if it.main.Description == "" {
		key := it.main.Key()
		cmds = append(cmds, tea.Tick(220*time.Millisecond, func(time.Time) tea.Msg { return hoverMsg{key} }))
	}
	return tea.Batch(cmds...)
}

func (r *results) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case searchMsg:
		if m.gen != r.gen {
			return nil
		}
		st := r.states[m.src]
		if st == nil {
			return nil
		}
		st.loading, st.err, st.count = false, m.err, len(m.results)
		r.byProv[m.src] = m.results
		first := len(r.items) == 0
		r.rebuild()
		if first && len(r.items) > 0 {
			r.cur = 0
			r.revealAt = time.Now()
			return tea.Batch(r.onMove(a), a.tick())
		}
	case hoverMsg:
		if it, ok := r.selected(); ok && it.main.Key() == m.key {
			return a.fetchDetails(it.main)
		}
	case detailsMsg:
		if d, ok := a.details[m.key]; ok && d.Poster != "" && a.cfg.Posters {
			return a.posters.want(d.Poster)
		}
	case tea.MouseMsg:
		if m.Action == tea.MouseActionPress {
			switch m.Button {
			case tea.MouseButtonWheelUp:
				return r.move(a, -1)
			case tea.MouseButtonWheelDown:
				return r.move(a, 1)
			}
		}
	case tea.KeyMsg:
		switch m.String() {
		case "esc", "q":
			a.pop()
		case "/":
			a.pop()
			if hm, ok := a.top().(*home); ok {
				hm.focus = 0
				hm.in.Set(r.query)
			}
		case "alt+a":
			cmd := toggleLegacy(a)
			nr := newResults(r.query)
			a.pop()
			return tea.Batch(cmd, a.push(nr), nr.start(a))
		case "up", "k":
			return r.move(a, -1)
		case "down", "j":
			return r.move(a, 1)
		case "pgup":
			return r.move(a, -10)
		case "pgdown":
			return r.move(a, 10)
		case "home", "g":
			return r.move(a, -len(r.items))
		case "end", "G":
			return r.move(a, len(r.items))
		case "J", "shift+down":
			r.descOff++
		case "K", "shift+up":
			r.descOff = max(0, r.descOff-1)
		case "a", "+":
			if it, ok := r.selected(); ok {
				a.openPopup(it.main)
				return a.tick()
			}
		case "enter", "right", "l":
			if it, ok := r.selected(); ok {
				a.flash()
				sc := newAnime(it.main, it.alts, 0, 0)
				return tea.Batch(a.push(sc), sc.start(a))
			}
		}
	}
	return nil
}

func (r *results) move(a *App, d int) tea.Cmd {
	n := clamp(r.cur+d, 0, len(r.items)-1)
	if n == r.cur {
		return nil
	}
	r.cur = n
	return tea.Batch(r.onMove(a), a.tick())
}

func (r *results) view(a *App, w, ht int) string {
	pad := 2
	inner := w - pad*2
	showDetail := inner >= 96
	lw := inner
	rw := 0
	if showDetail {
		lw = inner * 50 / 100
		rw = inner - lw - kitGap
	}
	var chips []string
	for _, id := range r.order {
		s := r.states[id]
		c := sourceColor(id)
		var st string
		switch {
		case s.loading:
			st = fg(c).Render(spinner(a.frame / 2))
		case s.err != nil:
			st = stErr.Render("✗")
		default:
			st = fg(c).Render(strconv.Itoa(s.count))
		}
		name := stSub.Render(s.info.Name)
		if s.info.Lang == "en" {
			name += stMuted.Render(" en")
		}
		chips = append(chips, st+" "+name)
	}
	status := strings.Join(chips, stFaint.Render("   "))
	sub := fmt.Sprintf("%d sonuç · farklı kaynaklardaki aynı anime tek satırda gösterilir", len(r.items))
	page := pageHeadIn("“"+r.query+"”", sub, status, inner, ht)
	h := ht - len(page)
	listH := h - 2
	var left []string
	switch {
	case len(r.items) == 0 && r.animating(a):
		for i := 0; i < min(5, listH); i++ {
			t := (sin(float64(a.frame)/6+float64(i)*.7) + 1) / 2
			c := mix(hex(cSurface), hex(cShade), t).hex()
			left = append(left, "  "+fg(c).Render("●")+"  "+fg(c).Render(strings.Repeat("▬", 12+int(hash(i+3)*20))))
		}
	case len(r.items) == 0:
		left = append(left, stSub.Render("Hiçbir kaynakta sonuç yok."), "")
		for _, id := range r.order {
			if s := r.states[id]; s.err != nil {
				left = append(left, stErr.Render("✗ ")+stSub.Render(s.info.Name+": ")+stMuted.Render(truncate(shortErr(s.err), lw-24)))
			}
		}
		left = append(left, "", stMuted.Render("Yazımı değiştirmeyi dene: japonca / ingilizce ad, ya da daha kısa bir kelime."))
		if !a.cfg.Legacy {
			left = append(left, stMuted.Render("Eski seriler için ")+stText.Render("alt+a")+stMuted.Render(" ile TürkAnime arşivini de ara."))
		}
	default:
		r.cur = clamp(r.cur, 0, len(r.items)-1)
		r.off = scroll(r.cur, r.off, listH, len(r.items))
		var rows []string
		band := r.gl.row(a, r.cur)
		for i := r.off; i < len(r.items) && len(rows) < listH; i++ {
			line := resultRow(r.items[i], lw-5, i == band, reveal(r.revealAt, i-r.off, a.motion()))
			if i == r.cur {
				line = anchor() + line
			}
			rows = append(rows, line)
		}
		rows = block(rows, lw-5, listH)
		left = append(left, hjoin(0, rows, scrollbar(listH, r.off, len(r.items)))...)
	}
	cols := [][]string{panel("sonuçlar", left, lw, h, true, cAccent, plural(len(r.items)))}
	if showDetail {
		var right []string
		if it, ok := r.selected(); ok {
			an := it.main
			if d, ok := a.details[an.Key()]; ok && d.ID != "" {
				an = merge(an, d)
			}
			if an.Poster != r.curPost {
				r.prevPost, r.curPost = r.curPost, an.Poster
				if a.motion() {
					r.posterAt = newTween(380 * time.Millisecond)
				}
			}
			right = detailPane(a, an, it.alts, rw-4, h-2, r.descOff, r.prevPost, r.posterAt.p())
		}
		cols = append(cols, panel("ayrıntı", right, rw, h, false, cAccent))
	}
	lines := append(page, hjoin(kitGap, cols...)...)
	for i := range lines {
		lines[i] = strings.Repeat(" ", pad) + lines[i]
	}
	return strings.Join(lines, "\n")
}

// row2 places styled left text and right-aligned styled text in w cells.
func row2(left, right string, w int) string {
	rw := lipgloss.Width(right)
	if rw >= w-4 {
		return truncate(left, w)
	}
	return fit(truncate(left, w-rw-1), w-rw) + right
}

func resultRow(e entry, w int, sel bool, rv float64) string {
	if rv <= 0 {
		return ""
	}
	it := e.main
	bg, bar := "", sp("  ", cText)
	tc := mix(hex(cFaint), hex(cText), rv).hex()
	if sel {
		bg, bar, tc = cBand, sp("▌ ", cAccent), cAccent
	}
	left := []span{bar, span{t: it.Title, c: tc, bold: sel}}
	if it.AltTitle != "" && w > 70 {
		left = append(left, sp("  "+it.AltTitle, cMuted))
	}
	var right []span
	if it.Year > 0 {
		right = append(right, sp(strconv.Itoa(it.Year)+"  ", cSub))
	}
	// one dot per source carrying this anime, in source colours
	for _, x := range e.all() {
		right = append(right, sp("●", sourceColor(x.Source)))
	}
	right = append(right, sp(" ", cText))
	return row(w, bg, left, right)
}

// merge overlays fetched details onto a search hit.
func merge(base, d source.Anime) source.Anime {
	if d.Description != "" {
		base.Description = d.Description
	}
	if len(d.Genres) > 0 {
		base.Genres = d.Genres
	}
	if d.Poster != "" {
		base.Poster = d.Poster
	}
	if d.Year > 0 {
		base.Year = d.Year
	}
	if d.MalID > 0 {
		base.MalID = d.MalID
	}
	if d.EpCount > 0 {
		base.EpCount = d.EpCount
	}
	if d.NextEpisode != "" {
		base.NextEpisode = d.NextEpisode
	}
	if len(d.Seasons) > 0 {
		base.Seasons = d.Seasons
	}
	if d.Kind != "" {
		base.Kind = d.Kind
	}
	return base
}

// detailPane: poster (cross-fading from the previous one) beside title,
// meta, genres and source availability; synopsis underneath.
func detailPane(a *App, it source.Anime, alts []source.Anime, w, h, descOff int, fromPoster string, fade float64) []string {
	var out []string
	acc := a.posters.accent(it.Poster, cAccent)
	pw := min(28, w)
	ph := pw * 3 / 4
	tw := w
	sideBySide := a.cfg.Posters && w >= 64 && h >= 14
	if sideBySide {
		pw = min(26, w/3)
		ph = min(pw*3/4, h-1)
		tw = w - pw - 2
	}
	var text []string
	for i, l := range wrap(it.Title, tw) {
		if i == 0 {
			text = append(text, gradient(l, []string{acc.hex(), cAccent2}, true))
		} else {
			text = append(text, stBold.Render(l))
		}
	}
	if it.AltTitle != "" {
		for _, l := range wrap(it.AltTitle, tw) {
			text = append(text, stMuted.Render(l))
		}
	}
	text = append(text, "", metaLine(it))
	if li, ok := a.libItem(it); ok {
		ln := fg(statusColor(li.Status)).Render(statusIcon(li.Status) + " " + li.Status)
		if li.Score > 0 {
			ln += stFaint.Render("  ·  ") + fg(cWarn).Render(ic("star")+" ") + stBold.Render(fmt.Sprint(li.Score))
		}
		if li.InList("favoriler") {
			ln += "  " + fg(cAccent).Render(ic("heart"))
		}
		text = append(text, ln)
	}
	if g := genreLine(it.Genres, tw); g != "" {
		text = append(text, g)
	}
	if it.NextEpisode != "" {
		text = append(text, fg(cWarn).Render("◷ ")+stSub.Render("sıradaki: "+it.NextEpisode))
	}
	if len(alts) > 0 {
		var srcs []string
		for _, x := range append([]source.Anime{it}, alts...) {
			if p := source.Get(x.Source); p != nil {
				name := p.Info().Name
				if p.Info().Lang == "en" {
					name += " (en)"
				}
				srcs = append(srcs, fg(sourceColor(x.Source)).Render("● ")+stSub.Render(name))
			}
		}
		text = append(text, "", stMuted.Render("kaynaklar  ")+strings.Join(srcs, "  "))
	}
	desc := it.Description
	if desc == "" {
		desc = "…"
	}
	poster := func() []string {
		if fade < 1 && fromPoster != "" {
			return a.posters.renderFade(fromPoster, it.Poster, pw, ph, fade, a.frame)
		}
		return a.posters.render(it.Poster, pw, ph, a.frame)
	}
	if sideBySide {
		bh := max(ph, len(text))
		out = append(out, hjoin(2, block(poster(), pw, bh), block(text, tw, bh))...)
		out = append(out, "")
		dl := wrap(desc, w)
		descOff = min(descOff, max(0, len(dl)-1))
		for _, l := range dl[descOff:] {
			out = append(out, stSub.Render(l))
		}
	} else {
		if a.cfg.Posters && h >= 26 {
			ph = min(ph, h/2)
			out = append(out, poster()...)
			out = append(out, "")
		}
		out = append(out, text...)
		out = append(out, "")
		for _, l := range wrap(desc, w) {
			out = append(out, stSub.Render(l))
		}
	}
	return block(out, w, h)
}

func metaLine(it source.Anime) string {
	var parts []string
	if src := source.Get(it.Source); src != nil {
		parts = append(parts, fg(sourceColor(it.Source)).Render("● ")+stSub.Render(src.Info().Name))
	}
	if it.Year > 0 {
		parts = append(parts, stSub.Render(strconv.Itoa(it.Year)))
	}
	if it.Kind != "" {
		parts = append(parts, stSub.Render(it.Kind))
	}
	if it.EpCount > 0 {
		parts = append(parts, stSub.Render(fmt.Sprintf("%d bölüm", it.EpCount)))
	}
	return strings.Join(parts, stFaint.Render(" · "))
}

func genreLine(gs []string, w int) string {
	var b strings.Builder
	used := 0
	for _, g := range gs {
		chip := " " + g + " "
		cw := lipgloss.Width(chip) + 1
		if used+cw > w {
			break
		}
		b.WriteString(fg(cSub).Background(colorOf(cSurface)).Render(chip) + " ")
		used += cw
	}
	return b.String()
}
