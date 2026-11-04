package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// ---- "bu akşam ne izlesem" ----
//
// From what you already have going: a half-watched episode, new episodes
// of shows you follow, the next episodes of what you are watching, and
// short things from your plan-to-watch list — fitted to the time you have
// (+ / - changes it).

type tonightPick struct {
	an      source.Anime
	season  int
	episode int
	count   int // episodes that fit
	minutes int
	reason  string
	score   int
	item    *store.Item
}

const epMinutes = 24

func tonightPicks(a *App, budget int, feed []source.FeedItem) []tonightPick {
	var out []tonightPick
	seen := map[string]bool{}
	add := func(p tonightPick) {
		if seen[p.an.Key()] || p.minutes <= 0 {
			return
		}
		seen[p.an.Key()] = true
		out = append(out, p)
	}
	fit := func(n int) int { return max(1, min(n, budget/epMinutes)) }
	// 1) something left half-way
	for _, e := range a.recent() {
		k := store.EpKey(e.Season, e.Episode)
		if e.Watched[k] || e.Duration <= 0 || e.Percent() < 3 || e.Percent() >= 75 || time.Since(e.UpdatedAt) > 30*24*time.Hour {
			continue
		}
		left := int((e.Duration - e.Position) / 60)
		add(tonightPick{an: e.Anime(), season: e.Season, episode: e.Episode, count: 1, minutes: max(1, left),
			reason: fmt.Sprintf("yarım kalmış · %d dk kaldı", left), score: 100})
	}
	// 2) new episodes on shows you follow
	for _, n := range newEpisodes(a, feed) {
		e, _ := a.hist.Get(n.an.Key())
		next := e.Episode + 1
		if !e.Watched[store.EpKey(e.Season, e.Episode)] {
			next = e.Episode
		}
		cnt := fit(n.episode - next + 1)
		add(tonightPick{an: n.an, season: n.season, episode: next, count: cnt, minutes: cnt * epMinutes,
			reason: fmt.Sprintf("yeni bölüm çıktı · %d. bölüm", n.episode), score: 90})
	}
	// 3) carry on with what you watch
	for _, e := range a.recent() {
		if time.Since(e.UpdatedAt) > 60*24*time.Hour {
			continue
		}
		if li, ok := a.libItem(e.Anime()); ok && (li.Status == store.Completed || li.Status == store.Dropped) {
			continue
		}
		next := e.Episode
		if e.Watched[store.EpKey(e.Season, e.Episode)] {
			next++
		}
		cnt := fit(budget / epMinutes)
		add(tonightPick{an: e.Anime(), season: e.Season, episode: next, count: cnt, minutes: cnt * epMinutes,
			reason: fmt.Sprintf("kaldığın yerden %d bölüm", cnt), score: 70 - int(time.Since(e.UpdatedAt).Hours()/24)})
	}
	// 4) plan-to-watch that fits (short series, or a film on a long evening)
	for _, it := range a.lib.All() {
		if it.Status != store.Planned {
			continue
		}
		it := it
		mins := it.EpCount * epMinutes
		isFilm := strings.EqualFold(it.Kind, "film") || strings.EqualFold(it.Kind, "movie")
		switch {
		case isFilm:
			mins = 110
		case it.EpCount == 0:
			mins = fit(3) * epMinutes
		}
		if mins > budget && !(it.EpCount > 12) {
			continue
		}
		cnt := fit(max(1, it.EpCount))
		reason := "planlananlarından"
		if isFilm {
			reason, cnt = "planlananlarından · film", 1
		} else if it.EpCount > 0 && it.EpCount*epMinutes <= budget {
			reason = fmt.Sprintf("planlananlarından · tamamı %d dk", it.EpCount*epMinutes)
			cnt = it.EpCount
		}
		add(tonightPick{an: it.Anime(), season: 1, episode: 1, count: cnt, minutes: min(mins, cnt*epMinutes+0), reason: reason, score: 40 + it.Score, item: &it})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func (d *discover) tonightKey(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "+", "=":
		d.budget = min(300, d.budget+30)
	case "-":
		d.budget = max(30, d.budget-30)
	case "up", "k":
		d.cur[tTonight] = max(0, d.cur[tTonight]-1)
	case "down", "j":
		d.cur[tTonight]++
	case "enter":
		picks := tonightPicks(a, d.budget, homeFeed(a))
		if c := d.cur[tTonight]; c < len(picks) {
			p := picks[c]
			a.flash()
			if p.item != nil && p.item.FromMal() {
				return a.openItem(*p.item), true
			}
			sc := newAnime(p.an, nil, p.season, p.episode)
			return tea.Batch(a.push(sc), sc.start(a)), true
		}
	default:
		return nil, false
	}
	return a.tick(), true
}

func homeFeed(a *App) []source.FeedItem {
	if h, ok := a.stack[0].(*home); ok {
		return h.feed
	}
	return nil
}

func (d *discover) tonightView(a *App, w, h int) []string {
	picks := tonightPicks(a, d.budget, homeFeed(a))
	head := stMuted.Render("ne kadar vaktin var?  ") + stBold.Render(fmt.Sprintf("%d dk", d.budget)) + stMuted.Render("   (+ / -)")
	out := []string{head, ""}
	if len(picks) == 0 {
		return append(out, stMuted.Render("Henüz öneri için veri yok: bir şeyler izle ya da kitaplığına planlananlar ekle."))
	}
	c := &d.cur[tTonight]
	*c = clamp(*c, 0, len(picks)-1)
	band := d.gl.row(a, *c)
	for i, p := range picks {
		if len(out) >= h-1 {
			break
		}
		sel := i == band
		bg, bar := "", sp("  ", cText)
		tc := cText
		if sel {
			bg, bar, tc = cBand, sp("▌ ", cAccent), cAccent
		}
		what := fmt.Sprintf("  S%d·B%d", p.season, p.episode)
		if p.count > 1 {
			what = fmt.Sprintf("  S%d·B%d–%d", p.season, p.episode, p.episode+p.count-1)
		}
		line := row(w, bg, []span{bar, span{t: p.an.Title, c: tc, bold: sel}, sp(what, cSub)},
			[]span{sp(p.reason+"  ", cMuted), sp(fmt.Sprintf("%3d dk ", p.minutes), cAccent2)})
		if i == *c {
			line = anchor() + line
		}
		out = append(out, line)
	}
	return out
}

// ---- genres: browse, roll the dice, find the classics ----

type genreMsg struct {
	key   string
	items []anilist.Media
	err   error
}

func (d *discover) genreName() (string, string) {
	g := anilist.Genres[d.genre%len(anilist.Genres)]
	return g[0], g[1]
}

func (d *discover) genreKey() string {
	g, _ := d.genreName()
	return g + "|" + d.gmode
}

func (d *discover) loadGenre(a *App) tea.Cmd {
	if d.genreOK && d.gkey == d.genreKey() {
		return nil
	}
	d.genreOK, d.genreItems, d.genreErr = false, nil, nil
	g, _ := d.genreName()
	key, mode := d.genreKey(), d.gmode
	d.gkey = key
	return run(30*time.Second, func(ctx context.Context) tea.Msg {
		items, err := anilist.ByGenre(ctx, g, mode)
		return genreMsg{key, items, err}
	})
}

func (d *discover) genreKeyPress(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "]":
		d.genre = (d.genre + 1) % len(anilist.Genres)
	case "[":
		d.genre = (d.genre + len(anilist.Genres) - 1) % len(anilist.Genres)
	case "m":
		modes := []string{anilist.ModeTop, anilist.ModePopular, anilist.ModeCult}
		d.gmode = modes[(indexOf(modes, d.gmode)+1)%len(modes)]
		a.notify("tür sıralaması: "+d.gmode, "ok")
	case " ", "r":
		// the dice: a random title from this genre (cult mode → a classic)
		g, tr := d.genreName()
		mode := d.gmode
		d.tab, d.rnd, d.rndErr = tRandom, nil, nil
		d.rndNote = "zar · " + tr + " · " + mode
		if a.motion() {
			d.rolling = newTween(1100 * time.Millisecond)
		}
		d.rollPool = nil
		for _, m := range d.genreItems {
			d.rollPool = append(d.rollPool, m.Name())
		}
		return tea.Batch(a.tick(), run(20*time.Second, func(ctx context.Context) tea.Msg {
			m, err := anilist.RandomInGenre(ctx, g, mode)
			return randMsg{m, err}
		})), true
	default:
		return nil, false
	}
	d.cur[tGenre] = 0
	return tea.Batch(d.loadGenre(a), a.tick()), true
}

// genreHeader: "‹ Gizem ›  en iyi · popüler · kült".
func (d *discover) genreHeader(w int) string {
	_, tr := d.genreName()
	var modes []string
	for _, m := range []string{anilist.ModeTop, anilist.ModePopular, anilist.ModeCult} {
		if m == d.gmode {
			modes = append(modes, stAccent.Bold(true).Render(m))
		} else {
			modes = append(modes, stMuted.Render(m))
		}
	}
	left := stFaint.Render("[ ") + gradient(tr, brand, true) + stFaint.Render(" ]") + "    " + strings.Join(modes, stFaint.Render(" · "))
	return row2(left, stMuted.Render("m sırala · space zar"), w)
}

// tonightCard shows the highlighted pick's cover and why it was picked.
func (d *discover) tonightCard(a *App, w, h int) []string {
	picks := tonightPicks(a, d.budget, homeFeed(a))
	c := d.cur[tTonight]
	if c >= len(picks) {
		return panel("ayrıntı", nil, w, h, false, cAccent)
	}
	p := picks[c]
	poster := p.an.Poster
	if poster == "" {
		if dd, ok := a.details[p.an.Key()]; ok {
			poster = dd.Poster
		}
	}
	acc := a.posters.accent(poster, cAccent).hex()
	iw := w - 4
	var out []string
	if a.cfg.Posters && h >= 24 && poster != "" {
		pw := min(iw, 26)
		ph := min(pw*3/4, h/2)
		ind := strings.Repeat(" ", (iw-pw)/2)
		for _, l := range a.posters.render(poster, pw, ph, a.frame) {
			out = append(out, ind+l)
		}
		out = append(out, "")
	}
	out = append(out, gradient(truncate(p.an.Title, iw), []string{acc, cAccent2}, true), "",
		stSub.Render(p.reason),
		stMuted.Render(fmt.Sprintf("%d bölüm · yaklaşık %d dk", p.count, p.minutes)), "",
		stText.Bold(true).Render("enter")+stMuted.Render(" bölümden başlat"))
	return panel("bu akşam", out, w, h, false, acc)
}
