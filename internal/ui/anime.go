package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

type animeScreen struct {
	srcs       []source.Anime // same anime on different sources; [0] is the best
	srcIdx     int
	base       source.Anime
	an         source.Anime
	ready      bool
	detailErr  error
	epErr      error
	sIdx       int
	cur, off   int
	wantSeason int
	wantEp     int
	descOff    int
	epAt       time.Time
	switchAt   tween
	gl         glide
	dwellGen   int  // prefetch: the cursor rested on an episode
	noting     bool // writing a note for the selected episode
	noteIn     input
	looking    bool   // searching the other sources for this anime
	looked     bool   // …and that search already came back
	lookKey    string // the source the search started from
	waitSwitch bool   // nothing else was known: switch when it returns
	placed     bool   // the list was scrolled to the cursor once
	ordered    bool   // sources ranked and the best one picked
}

func (s *animeScreen) typing() bool { return s.noting }

func newAnime(an source.Anime, alts []source.Anime, season, ep int) *animeScreen {
	return &animeScreen{srcs: append([]source.Anime{an}, alts...), base: an, an: an, wantSeason: season, wantEp: ep}
}

func (s *animeScreen) crumb() string { return truncate(s.an.Title, 28) }

func (s *animeScreen) hints() []hint {
	h := []hint{{"enter", "fansub seç"}, {"p", "hızlı oynat"}, {"tab [ ]", "sezon"}, {"s", "kaynak değiştir"}}
	if s.noting {
		return []hint{{"enter", "notu kaydet"}, {"esc", "vazgeç"}}
	}
	return append(h, hint{"w", "izlendi"}, hint{"N", "not"}, hint{"t", "fragman"}, hint{"y", "benzerleri"}, hint{"d", "indir"}, hint{"D", "sezonu indir"}, hint{"a", "kitaplık"}, hint{"esc", "geri"})
}

func (s *animeScreen) animating(a *App) bool {
	if a.top() != screen(s) {
		return false
	}
	if !s.ready && s.detailErr == nil {
		return true
	}
	if s.ready && s.episodes(a) == nil && s.epErr == nil {
		return true
	}
	return revealing(s.epAt, 30, a.motion()) || s.switchAt.active()
}

// accent is this anime's colour, sampled from its poster.
func (s *animeScreen) accent(a *App) string { return a.posters.accent(s.an.Poster, cAccent).hex() }

func (s *animeScreen) start(a *App) tea.Cmd {
	if !s.ordered {
		// however it was opened (the shelf card stands for the source you
		// used last), the show opens on the best source: switched-on ones
		// first, the primary on top; `s` still reaches every other one
		s.ordered = true
		if len(s.srcs) == 1 {
			s.srcs = append(s.srcs, a.siblings(s.base)...)
		}
		was := s.base.Key()
		s.srcs = rankSources(a, s.srcs)
		s.srcIdx, s.base, s.an = 0, s.srcs[0], s.srcs[0]
		if s.base.Key() != was {
			// the episode came from another source's numbering: let ensure
			// pick the freshest progress for this one
			s.wantSeason, s.wantEp = 0, 0
		}
	}
	var cmds []tea.Cmd
	if a.cfg.Posters {
		cmds = append(cmds, a.posters.want(s.an.Poster))
	}
	cmds = append(cmds, a.fetchDetails(s.base), s.ensure(a), a.tick())
	return tea.Batch(cmds...)
}

// ensure initialises once details are known, then loads the chosen season.
func (s *animeScreen) ensure(a *App) tea.Cmd {
	if s.ready {
		return nil
	}
	d, ok := a.details[s.base.Key()]
	if !ok || d.ID == "" {
		return nil
	}
	s.an = merge(s.base, d)
	s.an.Title = firstNonEmpty(d.Title, s.base.Title)
	s.ready = true
	if len(s.an.Seasons) == 0 {
		s.an.Seasons = []source.Season{{Number: 1, Name: "Sezon 1"}}
	}
	if s.wantSeason == 0 {
		e, ok := a.hist.Get(s.an.Key())
		// watched more recently on another source? pick up there (only a
		// plain season 1 maps across sources)
		if sib, found := a.sibling(s.an); found && sib.Season <= 1 && (!ok || (e.Season <= 1 && sib.UpdatedAt.After(e.UpdatedAt))) {
			e, ok = sib, true
		}
		if ok {
			s.wantSeason, s.wantEp = max(1, e.Season), e.Episode
			if e.Watched[store.EpKey(e.Season, e.Episode)] {
				s.wantEp++
			}
		}
		// your list (MAL) may be further along than what you played here
		wv := a.watchView(s.an)
		if wv.upTo > 0 && wv.upTo < 1<<30 && (s.wantSeason == 0 || (s.wantSeason == wv.first && s.wantEp <= wv.upTo)) {
			s.wantSeason, s.wantEp = wv.first, wv.upTo+1
		}
	}
	s.sIdx = 0
	for i, se := range s.an.Seasons {
		if se.Number == s.wantSeason {
			s.sIdx = i
		}
	}
	var cmds []tea.Cmd
	if a.cfg.Posters {
		cmds = append(cmds, a.posters.want(s.an.Poster))
	}
	cmds = append(cmds, s.loadSeason(a))
	return tea.Batch(cmds...)
}

var reGenericEp = regexp.MustCompile(`(?i)^\s*(?:(?:\d+\.?\s*)?(?:bölüm|episode|ep)\s*\d*|\d+)\s*$`)

func genericTitle(t string) bool { return t == "" || reGenericEp.MatchString(t) }

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func (s *animeScreen) season() int {
	if s.sIdx < len(s.an.Seasons) {
		return s.an.Seasons[s.sIdx].Number
	}
	return 1
}

func (s *animeScreen) episodes(a *App) []source.Episode {
	return a.episodes[epKey(s.an, s.season())]
}

func (s *animeScreen) loadSeason(a *App) tea.Cmd {
	s.epErr = nil
	if eps := s.episodes(a); eps != nil {
		s.epAt = time.Now()
	}
	return tea.Batch(a.fetchEpisodes(s.an, s.season()), a.tick())
}

// altsMsg brings the same anime found on other sources.
type altsMsg struct {
	key  string // base key of the screen that asked
	alts []source.Anime
}

// switchSource re-opens this anime from the next source that carries it.
// Opened from the shelf, history or palette the screen knows only one
// source, so the first press looks the title up on the others.
func (s *animeScreen) switchSource(a *App) tea.Cmd {
	var look tea.Cmd
	if !s.looking && !s.looked {
		// the first press also asks every other source, so a source missing
		// from history (AnimeciX, say) still turns up
		s.looking, s.lookKey = true, s.an.Key()
		look = a.findAlts(s.an)
	}
	if len(s.srcs) < 2 {
		switch {
		case look != nil:
			s.waitSwitch = true
			a.notify("başka kaynaklarda aranıyor…", "")
			return tea.Batch(a.tick(), look)
		case s.looking:
			return nil
		}
		a.notify("bu anime başka bir kaynakta bulunamadı", "warn")
		return a.tick()
	}
	season, ep := s.season(), 0
	if eps := s.episodes(a); s.cur < len(eps) {
		ep = eps[s.cur].Number
	}
	s.srcIdx = (s.srcIdx + 1) % len(s.srcs)
	nb := s.srcs[s.srcIdx]
	// keep the cursor on the same episode on the new source; the lookup
	// state survives the switch
	*s = animeScreen{srcs: s.srcs, srcIdx: s.srcIdx, base: nb, an: nb, wantSeason: season, wantEp: ep,
		looking: s.looking, looked: s.looked, lookKey: s.lookKey, ordered: true}
	if a.motion() {
		s.switchAt = newTween(300 * time.Millisecond)
	}
	if p := source.Get(nb.Source); p != nil {
		a.notify("kaynak: "+p.Info().Name, "ok")
	}
	return tea.Batch(s.start(a), look)
}

// findAlts searches every other enabled source for the same anime, matched
// by MAL id or by folded title (like merged search results).
func (a *App) findAlts(an source.Anime) tea.Cmd {
	var provs []source.Provider
	for _, p := range source.All() {
		// asking to switch is explicit: sources switched off for search count
		// too; only the archive stays behind its own mode
		if in := p.Info(); in.ID != an.Source && (!in.Legacy || a.cfg.Legacy) {
			provs = append(provs, p)
		}
	}
	want := map[string]bool{}
	for _, k := range titleKeys(an) {
		want[k] = true
	}
	key := an.Key()
	return run(45*time.Second, func(ctx context.Context) tea.Msg {
		found := make([]source.Anime, len(provs)) // one per source, rank order kept
		var wg sync.WaitGroup
		for i, p := range provs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for _, q := range []string{an.Title, an.AltTitle} {
					if strings.TrimSpace(q) == "" {
						continue
					}
					res, err := p.Search(ctx, q)
					if err != nil {
						continue
					}
					for _, r := range res {
						if an.MalID != 0 && r.MalID == an.MalID {
							found[i] = r
							return
						}
						for _, k := range titleKeys(r) {
							if want[k] {
								found[i] = r
								return
							}
						}
					}
				}
			}()
		}
		wg.Wait()
		var alts []source.Anime
		for _, f := range found {
			if f.ID != "" {
				alts = append(alts, f)
			}
		}
		return altsMsg{key: key, alts: alts}
	})
}

func (s *animeScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case detailsMsg:
		if m.key != s.base.Key() {
			return nil
		}
		if m.err != nil {
			s.detailErr = m.err
			a.notifyErr("detay", m.err)
			return nil
		}
		return s.ensure(a)
	case altsMsg:
		if m.key != s.lookKey || !s.looking {
			return nil
		}
		s.looking, s.looked = false, true
		var added []string
		for _, x := range m.alts {
			dup := false
			for _, o := range s.srcs {
				dup = dup || o.Source == x.Source
			}
			if !dup {
				s.srcs = append(s.srcs, x)
				if p := source.Get(x.Source); p != nil {
					added = append(added, p.Info().Name)
				}
			}
		}
		if s.waitSwitch {
			s.waitSwitch = false
			if len(s.srcs) < 2 {
				a.notify("bu anime başka bir kaynakta bulunamadı", "warn")
				return a.tick()
			}
			return s.switchSource(a)
		}
		if len(added) > 0 {
			a.notify("şurada da var: "+strings.Join(added, ", ")+" · s ile geç", "ok")
			return a.tick()
		}
		return nil
	case dwellMsg:
		return s.onDwell(a, m)
	case variantsMsg:
		return s.warm(a, m.key)
	case episodesMsg:
		if m.key != epKey(s.an, s.season()) {
			return nil
		}
		if m.err != nil {
			s.epErr = m.err
			return nil
		}
		s.epAt = time.Now()
		s.cur, s.placed = 0, false
		if s.wantEp > 0 && m.season == s.wantSeason {
			for i, e := range m.eps {
				if e.Number == s.wantEp {
					s.cur = i
				}
			}
			if s.wantEp > m.eps[len(m.eps)-1].Number {
				s.cur = len(m.eps) - 1
			}
		}
	case tea.MouseMsg:
		if m.Action == tea.MouseActionPress {
			if m.Button == tea.MouseButtonWheelUp {
				s.cur = max(0, s.cur-1)
			} else if m.Button == tea.MouseButtonWheelDown {
				s.cur = clamp(s.cur+1, 0, len(s.episodes(a))-1)
			}
		}
	case tea.KeyMsg:
		return s.key(a, m)
	}
	return nil
}

func (s *animeScreen) key(a *App, k tea.KeyMsg) tea.Cmd {
	eps := s.episodes(a)
	if s.noting {
		switch k.String() {
		case "esc":
			s.noting = false
		case "enter":
			s.noting = false
			if s.cur < len(eps) {
				ep := eps[s.cur]
				text := strings.TrimSpace(s.noteIn.Value())
				a.hist.Update(s.an, func(e *store.Entry) {
					if e.Notes == nil {
						e.Notes = map[string]string{}
					}
					if text == "" {
						delete(e.Notes, store.EpKey(ep.Season, ep.Number))
					} else {
						e.Notes[store.EpKey(ep.Season, ep.Number)] = text
					}
					if e.Season == 0 {
						e.Season, e.Episode = ep.Season, ep.Number
					}
				})
				if text != "" {
					a.notify("✎ not kaydedildi", "ok")
					a.flash()
				}
			}
		default:
			s.noteIn.handle(k)
		}
		return nil
	}
	switch k.String() {
	case "esc", "q", "backspace":
		a.pop()
	case "s":
		return s.switchSource(a)
	case "r":
		if s.detailErr != nil {
			s.detailErr = nil
			return tea.Batch(a.fetchDetails(s.base), a.tick())
		}
		if s.epErr != nil {
			return s.loadSeason(a)
		}
	case "tab", "]", "right", "l":
		if len(s.an.Seasons) > 1 {
			s.sIdx = (s.sIdx + 1) % len(s.an.Seasons)
			s.cur, s.off = 0, 0
			return s.loadSeason(a)
		}
	case "shift+tab", "[", "left", "h":
		if len(s.an.Seasons) > 1 {
			s.sIdx = (s.sIdx + len(s.an.Seasons) - 1) % len(s.an.Seasons)
			s.cur, s.off = 0, 0
			return s.loadSeason(a)
		}
	case "up", "k":
		s.cur = max(0, s.cur-1)
		return s.dwell(a)
	case "down", "j":
		s.cur = clamp(s.cur+1, 0, len(eps)-1)
		return s.dwell(a)
	case "pgup":
		s.cur = max(0, s.cur-10)
	case "pgdown":
		s.cur = clamp(s.cur+10, 0, len(eps)-1)
	case "home", "g":
		s.cur = 0
	case "end", "G":
		s.cur = max(0, len(eps)-1)
	case "J":
		s.descOff++
	case "K":
		s.descOff = max(0, s.descOff-1)
	case "a", "+":
		a.openPopup(s.an)
		return a.tick()
	case "w":
		if s.cur < len(eps) {
			ep := eps[s.cur]
			key := store.EpKey(ep.Season, ep.Number)
			wv := a.watchView(s.an)
			now := !wv.done(ep)
			a.hist.Update(s.an, func(e *store.Entry) {
				e.Watched[key] = now
				if e.Season == 0 {
					e.Season, e.Episode, e.EpTitle = ep.Season, ep.Number, ep.Title
				}
			})
			if !now {
				if ep.Season == wv.first && ep.Number <= wv.upTo {
					// the list said you saw it: take the count back to before it
					a.libSet(s.an, func(x *store.Item) {
						if x.Watched >= ep.Number {
							x.Watched = ep.Number - 1
						}
						if x.Status == store.Completed {
							x.Status = store.Watching
						}
					})
				}
				a.notify(fmt.Sprintf("%d. bölüm izlenmedi olarak işaretlendi", ep.Number), "ok")
				return a.tick()
			}
			a.flash()
			bumpWatched(a, s.an, ep)
			// a little pop for every tick, a real party when the season is done
			wv = a.watchView(s.an)
			all := true
			for _, e := range eps {
				if !wv.done(e) {
					all = false
					break
				}
			}
			if all {
				a.celebrate(70)
				a.notify("◆ sezon tamamlandı!", "ok")
				lastSeason := len(s.an.Seasons) == 0 || s.an.Seasons[len(s.an.Seasons)-1].Number == ep.Season
				if lastSeason && s.an.NextEpisode == "" {
					if it, _ := a.libItem(s.an); it.Status != store.Completed {
						a.libSet(s.an, func(x *store.Item) { x.Status = store.Completed })
						a.notify("◆ seri bitti — kitaplıkta tamamlandı · a ile puan ver", "ok")
					}
				}
			} else {
				a.celebrate(12)
				if it, ok := a.libItem(s.an); !ok || it.Status == store.Planned {
					a.libSet(s.an, func(x *store.Item) { x.Status = store.Watching })
				}
			}
			if s.cur < len(eps)-1 {
				s.cur++
			}
			return a.tick()
		}
	case "t":
		return a.trailer(s.an.MalID, s.an.Title)
	case "y":
		return a.similar(s.an)
	case "N":
		if s.cur < len(eps) {
			ep := eps[s.cur]
			e, _ := a.hist.Get(s.an.Key())
			s.noteIn = input{placeholder: "bu bölüme not… (enter kaydet · esc vazgeç · boş bırak = sil)"}
			s.noteIn.Set(e.Notes[store.EpKey(ep.Season, ep.Number)])
			s.noting = true
		}
		return nil
	case "d":
		// choose the fansub (and, if you like, host and quality) to download with
		if s.cur < len(eps) {
			pk := newPicker(s.an, eps[s.cur], eps, false)
			pk.dlMode = 1
			return tea.Batch(a.push(pk), pk.start(a))
		}
	case "D":
		// the whole season with one fansub: chosen on the first episode
		if len(eps) > 0 {
			pk := newPicker(s.an, eps[0], eps, false)
			pk.dlMode, pk.dlSeason = 2, eps
			return tea.Batch(a.push(pk), pk.start(a))
		}
	case "enter", "p", " ":
		if s.cur < len(eps) && k.String() != "enter" {
			// a downloaded copy plays offline, instantly
			if j, ok := a.dl.Local(s.an.Key(), eps[s.cur].Season, eps[s.cur].Number); ok {
				a.flash()
				return a.playLocal(j, eps)
			}
		}
		if s.cur < len(eps) {
			a.flash()
			pk := newPicker(s.an, eps[s.cur], eps, k.String() != "enter")
			return tea.Batch(a.push(pk), pk.start(a))
		}
	}
	return nil
}

func (s *animeScreen) view(a *App, w, h int) string {
	pad := 2
	inner := w - pad*2
	showDetail := inner >= 92
	lw := inner
	rw := 0
	if showDetail {
		lw = inner * 55 / 100
		rw = inner - lw - 3
	}
	var left []string
	switch {
	case s.detailErr != nil:
		left = []string{stErr.Render("✗ ayrıntılar alınamadı"), stMuted.Render(truncate(shortErr(s.detailErr), lw)), ""}
		if len(s.srcs) > 1 {
			left = append(left, stSub.Render("s ile başka kaynağı dene"))
		}
		left = append(left, stMuted.Render("r tekrar dene · esc geri"))
	case !s.ready:
		left = append(s.sourceBar(a, lw), "", fg(s.accent(a)).Render(spinner(a.frame/2))+stMuted.Render(" yükleniyor…"))
	default:
		left = s.episodePane(a, lw, h)
	}
	cols := [][]string{block(left, lw, h)}
	if showDetail {
		cols = append(cols, vrule(h), detailPane(a, s.an, nil, rw, h, s.descOff, "", 1))
	}
	lines := hjoin(1, cols...)
	for i := range lines {
		lines[i] = strings.Repeat(" ", pad) + lines[i]
	}
	return strings.Join(lines, "\n")
}

// sourceBar shows which sources carry this anime; the active one is marked.
func (s *animeScreen) sourceBar(a *App, w int) []string {
	if len(s.srcs) < 2 {
		return nil
	}
	var parts []string
	for i, x := range s.srcs {
		p := source.Get(x.Source)
		if p == nil {
			continue
		}
		name := p.Info().Name
		if p.Info().Lang == "en" {
			name += " en"
		}
		if i == s.srcIdx {
			parts = append(parts, fg(sourceColor(x.Source)).Render("●")+" "+stBold.Render(name))
		} else {
			parts = append(parts, stFaint.Render("○")+" "+stMuted.Render(name))
		}
	}
	return []string{row2(strings.Join(parts, "   "), stText.Bold(true).Render("s")+stMuted.Render(" değiştir"), w)}
}

func (s *animeScreen) episodePane(a *App, w, h int) []string {
	acc := s.accent(a)
	out := s.sourceBar(a, w)
	if len(out) > 0 {
		out = append(out, "")
	}
	// season tabs with an accent underline under the active one
	var tabs, under strings.Builder
	used := 0
	for i, se := range s.an.Seasons {
		label := se.Name
		if label == "" || strings.HasPrefix(strings.ToLower(label), "season") {
			label = fmt.Sprintf("Sezon %d", se.Number)
		}
		if s.an.IsMovie() {
			label = "Film"
		}
		if se.EpisodeCount > 0 && len(s.an.Seasons) > 1 {
			label += fmt.Sprintf(" · %d", se.EpisodeCount)
		}
		lw := lipgloss.Width(label)
		if used+lw+3 > w {
			tabs.WriteString(stMuted.Render("…"))
			break
		}
		if i == s.sIdx {
			tabs.WriteString(stBold.Render(label))
			under.WriteString(fg(acc).Render(strings.Repeat("━", lw)))
		} else {
			tabs.WriteString(stMuted.Render(label))
			under.WriteString(stFaint.Render(strings.Repeat("─", lw)))
		}
		tabs.WriteString("   ")
		under.WriteString(stFaint.Render("───"))
		used += lw + 3
	}
	if used < w {
		under.WriteString(stFaint.Render(strings.Repeat("─", w-used)))
	}
	out = append(out, tabs.String(), under.String())

	eps := s.episodes(a)
	switch {
	case errors.Is(s.epErr, source.ErrNotFound):
		// a season the site lists but has not filled yet: not an error
		out = append(out, "", stSub.Render("Bu sezonda henüz bölüm yok."))
		if len(s.an.Seasons) > 1 {
			out = append(out, stMuted.Render("tab ile diğer sezona geç"))
		}
		out = append(out, stMuted.Render("s ile başka kaynakta ara"))
		return out
	case s.epErr != nil:
		out = append(out, "", stErr.Render("✗ bölümler alınamadı: ")+stMuted.Render(truncate(shortErr(s.epErr), w-24)), stMuted.Render("r ile tekrar dene"))
		out = append(out, stSub.Render("s ile başka kaynağı dene"))
		return out
	case eps == nil:
		for i := 0; i < 6; i++ {
			t := (sin(float64(a.frame)/6+float64(i)*.7) + 1) / 2
			c := mix(hex(cSurface), hex(cShade), t).hex()
			out = append(out, "  "+fg(c).Render("▬▬")+"  "+fg(c).Render(strings.Repeat("▬", 10+int(hash(i+9)*22))))
		}
		return out
	}
	wv := a.watchView(s.an)
	ent := wv.ent
	watched := 0
	for _, e := range eps {
		if wv.done(e) {
			watched++
		}
	}
	// progress strip for the season
	if watched > 0 {
		label := fmt.Sprintf(" %d/%d izlendi", watched, len(eps))
		out = append(out, progressBar(w-lipgloss.Width(label), float64(watched)/float64(len(eps)), []string{acc, cAccent2})+stMuted.Render(label))
	} else {
		out = append(out, "")
	}
	listH := h - len(out)
	// the selected episode's note (or the note editor) sits under the list
	var noteLines []string
	if s.noting {
		noteLines = s.noteIn.view(w-1, true, a.frame)
	} else if eps := s.episodes(a); s.cur < len(eps) {
		e, _ := a.hist.Get(s.an.Key())
		if n := e.Notes[store.EpKey(eps[s.cur].Season, eps[s.cur].Number)]; n != "" {
			noteLines = []string{stFaint.Render(strings.Repeat("─", w-1))}
			for i, l := range wrap(n, w-5) {
				if i > 2 {
					break
				}
				prefix := "   "
				if i == 0 {
					prefix = stAccent.Render("✎") + "  "
				}
				noteLines = append(noteLines, prefix+stSub.Italic(true).Render(l))
			}
		}
	}
	listH -= len(noteLines)
	s.cur = clamp(s.cur, 0, len(eps)-1)
	if !s.placed && listH > 0 {
		// first look at a long list: the cursor sits mid-screen, so what you
		// watched and what comes next are both in view
		s.off = clamp(s.cur-listH/2, 0, max(0, len(eps)-listH))
		s.placed = true
	}
	s.off = scroll(s.cur, s.off, listH, len(eps))
	numW := max(2, len(fmt.Sprint(eps[len(eps)-1].Number)))
	var rows []string
	band := s.gl.row(a, s.cur)
	for i := s.off; i < len(eps) && len(rows) < listH; i++ {
		ep := eps[i]
		rv := reveal(s.epAt, i-s.off, a.motion())
		if rv <= 0 {
			rows = append(rows, "")
			continue
		}
		sel := i == band
		key := store.EpKey(ep.Season, ep.Number)
		done := wv.done(ep)
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		nc := cMuted
		if done {
			tc = cSub
		}
		if sel {
			bg, bar, tc, nc = cBand, sp("▌ ", acc), acc, acc
		}
		title := ep.Title
		if genericTitle(title) {
			// no real name: the number column already says which one, so
			// only a quiet word, and named episodes stand out
			title = "bölüm"
			if !sel {
				tc = mix(hex(cFaint), hex(cMuted), rv).hex()
			}
			if s.an.IsMovie() {
				title = s.an.Title
			}
		}
		var right []span
		if ent.Notes[key] != "" {
			right = append(right, sp("✎ ", cAccent2))
		}
		if j, ok := a.dl.For(s.an.Key(), ep.Season, ep.Number); ok {
			ic, c := dlIcon(j.State)
			switch j.State {
			case download.Done:
				right = append(right, sp("↓ ", cOK))
			case download.Running:
				right = append(right, sp(fmt.Sprintf("↓%2.0f%% ", j.Frac()*100), cAccent))
			default:
				right = append(right, sp(ic+" ", c))
			}
		}
		switch {
		case done:
			right = append(right, sp("✓", cOK))
		case ent.Progress[key] > 0:
			right = append(right, miniBar(6, float64(ent.Progress[key])/100, cWarn)...)
			right = append(right, sp(fmt.Sprintf(" %d%%", ent.Progress[key]), cWarn))
		}
		if ent.Season == ep.Season && ent.Episode == ep.Number && !done {
			right = append([]span{sp("▸ kaldığın yer  ", cWarn)}, right...)
		}
		right = append(right, sp(" ", cText))
		line := row(w-1, bg, []span{bar, sp(fmt.Sprintf("%0*d  ", numW, ep.Number), nc), span{t: title, c: tc, bold: sel}}, right)
		if i == s.cur {
			line = anchor() + line
		}
		rows = append(rows, line)
	}
	rows = block(rows, w-1, listH)
	out = append(out, hjoin(0, rows, scrollbar(listH, s.off, len(eps)))...)
	return append(out, noteLines...)
}
