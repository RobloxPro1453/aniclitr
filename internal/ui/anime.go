package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/download"
	"aniclitr/internal/match"
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
	dwellGen   int          // prefetch: the cursor rested on an episode
	noting     bool         // writing a note for the selected episode
	noteIn     input
	ranging    bool         // typing a custom episode range to download (e.g. "30-100")
	rangeIn    input
	selEps     map[int]bool // episode numbers toggled with 'v' for batch download
	looking    bool         // searching the other sources for this anime
	looked     bool         // …and that search already came back
	lookKey    string       // the source the search started from
	waitSwitch bool         // nothing else was known: switch when it returns
	placed     bool         // the list was scrolled to the cursor once
	ordered    bool         // sources ranked and the best one picked
	// Switched-off sources (Settings → Kaynaklar) decide where you watch,
	// not what you may open: a title known only on one is first looked
	// for on the switched-on sources (gate), and opened on the off one,
	// marked, only when no other has it (offSrc). explicit skips that: the
	// user named this very source (a pasted link, "open on X").
	gate     bool
	offSrc   bool
	explicit bool
}

// srcOn reports whether an's source is switched on in Settings.
func srcOn(a *App, an source.Anime) bool {
	p := source.Get(an.Source)
	if p == nil {
		return true // "mal:" items and the like: not a source to gate
	}
	in := p.Info()
	return a.cfg.Enabled(in.ID, in.Legacy)
}

// anyOn reports whether at least one source is switched on.
func anyOn(a *App) bool {
	for _, p := range source.All() {
		if in := p.Info(); a.cfg.Enabled(in.ID, in.Legacy) {
			return true
		}
	}
	return false
}

func srcName(id string) string {
	if p := source.Get(id); p != nil {
		return p.Info().Name
	}
	return id
}

func (s *animeScreen) typing() bool { return s.noting || s.ranging }

func newAnime(an source.Anime, alts []source.Anime, season, ep int) *animeScreen {
	return &animeScreen{srcs: append([]source.Anime{an}, alts...), base: an, an: an, wantSeason: season, wantEp: ep}
}

func (s *animeScreen) crumb() string { return truncate(s.an.Title, 28) }

func (s *animeScreen) hints() []hint {
	h := []hint{{"enter", "fansub seç"}, {"p", "hızlı oynat"}, {"c", "karakterler"}, {"tab [ ]", "sezon"}, {"s", "kaynak seç"}}
	if s.noting {
		return []hint{{"enter", "notu kaydet"}, {"esc", "vazgeç"}}
	}
	if s.ranging {
		return []hint{{"enter", "aralığı indir"}, {"esc", "vazgeç"}}
	}
	if len(s.selEps) > 0 {
		return []hint{{"v", "seç/bırak"}, {"d/D", fmt.Sprintf("seçilileri indir (%d)", len(s.selEps))}, {"V", "seçimi temizle"}, {"esc", "geri"}}
	}
	return append(h, hint{"w", "izlendi"}, hint{"v", "seç"}, hint{"d", "indir"}, hint{"D", "toplu indir"}, hint{"x", "indirilen bölümü sil"}, hint{"N", "not"}, hint{"t", "fragman"}, hint{"y", "benzerleri"}, hint{"a", "kitaplık"}, hint{"r", "eşleme / kurtarma"}, hint{"esc", "geri"})
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
		// the best known source is switched off (the switched-on ones rank
		// first, so none of them is known yet): look on those before
		// loading anything from it
		if !s.explicit && !srcOn(a, s.base) && anyOn(a) {
			s.gate = true
			s.looking, s.lookKey = true, s.base.Key()
			var cmds []tea.Cmd
			if a.cfg.Posters {
				cmds = append(cmds, a.posters.want(s.an.Poster))
			}
			return tea.Batch(append(cmds, a.findAltsIn(s.base, true), a.tick())...)
		}
	}
	return s.load(a)
}

func (s *animeScreen) load(a *App) tea.Cmd {
	var cmds []tea.Cmd
	if a.cfg.Posters {
		cmds = append(cmds, a.posters.want(s.an.Poster))
	}
	cmds = append(cmds, a.fetchDetails(s.base), s.ensure(a), a.tick())
	return tea.Batch(cmds...)
}

// passGate ends the switched-on search: open on what it found, or on the
// switched-off source, said out loud.
func (s *animeScreen) passGate(a *App, alts []source.Anime) tea.Cmd {
	s.gate, s.looking, s.looked = false, false, false // s may still search everywhere
	off := s.base
	for _, x := range alts {
		dup := false
		for _, o := range s.srcs {
			dup = dup || o.Source == x.Source
		}
		if !dup {
			s.srcs = append(s.srcs, x)
		}
	}
	s.srcs = rankSources(a, s.srcs)
	if best := s.srcs[0]; srcOn(a, best) {
		s.srcIdx, s.base, s.an = 0, best, best
		s.wantSeason, s.wantEp = 0, 0 // ensure takes the freshest progress
		a.notify("açıldı: "+srcName(best.Source)+" · "+srcName(off.Source)+" ayarlarda kapalı", "ok")
	} else {
		s.offSrc = true
		a.notify("bu anime yalnızca "+srcName(off.Source)+" kaynağında var · ayarlarda kapalı, yine de açıldı", "warn")
	}
	return s.load(a)
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
	s.an = userMal(merge(s.base, d)) // a hand-picked MAL match wins
	s.an.Title = firstNonEmpty(d.Title, s.base.Title)
	s.ready = true
	alignCmd := a.wantAlign(s.an)
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
				s.wantEp = source.NextNumber(s.wantEp)
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
	cmds = append(cmds, s.loadSeason(a), alignCmd)
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
	return tea.Batch(s.switchTo(a, (s.srcIdx+1)%len(s.srcs)), look)
}

// switchTo opens the show on s.srcs[idx], keeping the cursor's episode.
func (s *animeScreen) switchTo(a *App, idx int) tea.Cmd {
	if idx < 0 || idx >= len(s.srcs) || idx == s.srcIdx {
		return nil
	}
	season, ep := s.season(), 0
	if eps := s.episodes(a); s.cur < len(eps) {
		ep = eps[s.cur].Number
	}
	s.srcIdx = idx
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
	return s.start(a)
}

// findAlts searches every other enabled source for the same anime, matched
// by MAL id or by folded title (like merged search results).
func (a *App) findAlts(an source.Anime) tea.Cmd { return a.findAltsIn(an, false) }

// findAltsIn looks for an on the other sources; onlyOn keeps to the ones
// switched on in Settings (opening a title known only on an off source).
func (a *App) findAltsIn(an source.Anime, onlyOn bool) tea.Cmd {
	var provs []source.Provider
	for _, p := range source.All() {
		// asking to switch is explicit: sources switched off for search count
		// too; only the archive stays behind its own mode
		in := p.Info()
		if in.ID == an.Source || (in.Legacy && !a.cfg.Legacy) || (onlyOn && !a.cfg.Enabled(in.ID, in.Legacy)) {
			continue
		}
		provs = append(provs, p)
	}
	want := map[string]bool{}
	for _, k := range titleKeys(an) {
		want[k] = true
	}
	queries := match.BuildQueries(an.AllTitles())
	if len(queries) == 0 {
		queries = []string{an.Title}
	}
	cachedByMal := map[string]match.EntryWithID{}
	if an.MalID > 0 {
		cachedByMal = match.Default().ByMal(an.MalID)
	}
	key := an.Key()
	return run(45*time.Second, func(ctx context.Context) tea.Msg {
		found := make([]source.Anime, len(provs)) // one per source, rank order kept
		var wg sync.WaitGroup
		for i, p := range provs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				srcID := p.Info().ID
				if hit, ok := cachedByMal[srcID]; ok && hit.AnimeID != "" {
					found[i] = source.Anime{
						Source: srcID,
						ID:     hit.AnimeID,
						Title:  firstNonEmpty(hit.Entry.Title, an.Title),
						Poster: an.Poster,
						MalID:  an.MalID,
					}
					return
				}
				for _, q := range queries {
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
		if s.gate {
			return s.passGate(a, m.alts)
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
		if len(added) > 0 && (a.drop == nil || a.drop.kind != "sources") { // the open picker shows them itself
			a.notify("şurada da var: "+strings.Join(added, ", ")+" · s ile seç", "ok")
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
		s.selEps = nil
		if len(m.eps) > 0 && len(s.an.Seasons) <= 1 {
			s.an.EpCount = len(m.eps)
			if it, ok := a.libItem(s.an); ok && (it.EpCount == 0 || (it.Status == store.Completed && it.EpCount != len(m.eps))) {
				a.lib.Mutate(it.Key(), func(x *store.Item) {
					x.EpCount = len(m.eps)
					x.Reconcile()
				})
			}
		}
		if s.wantEp > 0 && m.season == s.wantSeason {
			for i, e := range m.eps {
				if e.Number == s.wantEp {
					s.cur = i
				}
			}
			if s.wantEp > m.eps[len(m.eps)-1].Number {
				s.cur = len(m.eps) - 1
				if a.cfg.AutoFallback && !s.looking && !s.looked {
					s.looking, s.lookKey = true, s.an.Key()
					a.notify(fmt.Sprintf("%s. bölüm bu kaynakta henüz yok — diğer kaynaklar kontrol ediliyor…", source.EpNum(s.wantEp)), "warn")
					return tea.Batch(a.tick(), a.findAlts(s.an))
				}
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
	if s.ranging {
		switch k.String() {
		case "esc":
			s.ranging = false
		case "enter":
			s.ranging = false
			picked := parseEpRange(s.rangeIn.Value(), eps)
			if len(picked) == 0 {
				a.notify("geçersiz bölüm aralığı (örn: 30-100 veya 10-20, 25)", "warn")
				return a.tick()
			}
			pk := newPicker(s.an, picked[0], eps, false)
			pk.dlMode, pk.dlSeason = 2, picked
			return tea.Batch(a.push(pk), pk.start(a))
		default:
			s.rangeIn.handle(k)
		}
		return nil
	}
	switch k.String() {
	case "esc", "q", "backspace":
		if len(s.selEps) > 0 && k.String() == "esc" {
			s.selEps = nil
			a.notify("bölüm seçimi temizlendi", "ok")
			return a.tick()
		}
		a.pop()
	case "c", "C":
		a.flash()
		cs := newCharactersScreen(s.an)
		return tea.Batch(a.push(cs), cs.start(a))
	case "s":
		return s.openSources(a)
	case "r":
		if s.detailErr != nil {
			s.detailErr = nil
			return tea.Batch(a.fetchDetails(s.base), a.tick())
		}
		if s.epErr != nil {
			return s.loadSeason(a)
		}
		if s.ready {
			a.matchMenu(s)
			return a.tick()
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
				if !now && wv.upTo == 1<<30 {
					// Transitioning out of Completed: preserve all other episodes of this season as watched
					for _, other := range eps {
						if other.Number != ep.Number {
							e.Watched[store.EpKey(other.Season, other.Number)] = true
						}
					}
				}
				e.Watched[key] = now
				if e.Season == 0 {
					e.Season, e.Episode, e.EpTitle = ep.Season, ep.Number, ep.Title
				}
			})
			if !now {
				if ep.Season == wv.first && ep.Number <= wv.upTo {
					// the list said you saw it: take the count back to before it
					a.libSet(s.an, func(x *store.Item) {
						if x.Status == store.Completed {
							x.Status = store.Watching
							if len(eps) > 0 {
								x.EpCount = max(x.EpCount, len(eps))
								x.Watched = max(0, len(eps)-1)
							} else if x.Watched >= ep.Number {
								x.Watched = ep.Number - 1
							}
						} else if x.Watched >= ep.Number {
							x.Watched = ep.Number - 1
						}
					})
				}
				a.notify(fmt.Sprintf("%s. bölüm izlenmedi olarak işaretlendi", ep.Num()), "ok")
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
					a.libSet(s.an, func(x *store.Item) {
						x.Status = store.Completed
						if len(s.an.Seasons) <= 1 && len(eps) > 0 {
							x.EpCount = max(x.EpCount, len(eps))
							x.Watched = x.EpCount
						}
					})
					a.notify("◆ seri bitti — kitaplıkta tamamlandı · a ile puan ver", "ok")
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
	case "v":
		if s.cur < len(eps) {
			if s.selEps == nil {
				s.selEps = map[int]bool{}
			}
			num := eps[s.cur].Number
			if s.selEps[num] {
				delete(s.selEps, num)
			} else {
				s.selEps[num] = true
			}
			if s.cur < len(eps)-1 {
				s.cur++
			}
			a.flash()
			return a.tick()
		}
	case "V":
		if len(s.selEps) > 0 {
			s.selEps = nil
			a.notify("bölüm seçimi temizlendi", "ok")
			return a.tick()
		}
	case "d":
		if len(s.selEps) > 0 {
			picked := s.selectedEpisodes(eps)
			s.selEps = nil
			if len(picked) > 0 {
				pk := newPicker(s.an, picked[0], eps, false)
				pk.dlMode, pk.dlSeason = 2, picked
				return tea.Batch(a.push(pk), pk.start(a))
			}
		}
		// choose the fansub (and, if you like, host and quality) to download with
		if s.cur < len(eps) {
			pk := newPicker(s.an, eps[s.cur], eps, false)
			pk.dlMode = 1
			return tea.Batch(a.push(pk), pk.start(a))
		}
	case "D":
		if len(eps) > 0 {
			return s.openBatchDownloadMenu(a, eps)
		}
	case "x", "delete":
		if s.cur < len(eps) {
			ep := eps[s.cur]
			if j, ok := a.dl.LocalFor(s.an, ep.Season, ep.Number); ok {
				a.dl.Remove(j.ID, true)
				a.notify(fmt.Sprintf("%s S%d·B%s diskten silindi", s.an.Title, ep.Season, ep.Num()), "ok")
				return a.tick()
			}
			if j, ok := a.dl.ForAnime(s.an, ep.Season, ep.Number); ok && (j.State == download.Queued || j.State == download.Resolving || j.State == download.Running) {
				a.dl.Cancel(j.ID)
				a.notify(fmt.Sprintf("S%d·B%s indirmesi iptal edildi", ep.Season, ep.Num()), "ok")
				return a.tick()
			}
			a.notify("bu bölüm henüz indirilmemiş", "warn")
			return a.tick()
		}
	case "enter", "p", " ":
		if s.cur < len(eps) && k.String() != "enter" {
			// a downloaded copy plays offline, instantly
			if j, ok := a.dl.LocalFor(s.an, eps[s.cur].Season, eps[s.cur].Number); ok {
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

func (s *animeScreen) view(a *App, w, ht int) string {
	pad := 2
	inner := w - pad*2
	var meta []string
	if s.an.Kind != "" {
		meta = append(meta, s.an.Kind)
	}
	if s.an.Year > 0 {
		meta = append(meta, fmt.Sprint(s.an.Year))
	}
	if p := source.Get(s.an.Source); p != nil {
		meta = append(meta, p.Info().Name)
	}
	head := pageHeadIn(truncate(s.an.Title, inner*2/3), "Bir bölüm seçin: enter çeviri grubunu sorar, p doğrudan oynatır", strings.Join(meta, " · "), inner, ht)
	h := ht - len(head)
	showDetail := inner >= 92
	lw := inner
	rw := 0
	if showDetail {
		lw = inner * 55 / 100
		rw = inner - lw - kitGap
	}
	var left []string
	switch {
	case s.detailErr != nil:
		left = []string{stErr.Render("✗ ayrıntılar alınamadı"), stMuted.Render(truncate(shortErr(s.detailErr), lw)), ""}
		if len(s.srcs) > 1 {
			left = append(left, stSub.Render("s ile başka kaynağı dene"))
		}
		left = append(left, stMuted.Render("r tekrar dene · esc geri"))
	case s.gate:
		left = []string{stWarn.Render("◌ ") + stSub.Render(srcName(s.base.Source)+" ayarlarda kapalı"), "",
			fg(s.accent(a)).Render(spinner(a.frame/2)) + stMuted.Render(" açık kaynaklarda aranıyor…"),
			"", stMuted.Render(truncate("bulunursa orada, kaldığın yerden açılır", lw-4))}
	case !s.ready:
		left = append(s.sourceLine(a, lw), "", fg(s.accent(a)).Render(spinner(a.frame/2))+stMuted.Render(" yükleniyor…"))
	default:
		left = s.episodePane(a, lw-4, h-2)
	}
	acc := s.accent(a)
	cols := [][]string{panel("bölümler", left, lw, h, true, acc)}
	if showDetail {
		cols = append(cols, panel("ayrıntı", detailPane(a, s.an, nil, rw-4, h-2, s.descOff, "", 1), rw, h, false, acc))
	}
	lines := append(head, hjoin(kitGap, cols...)...)
	for i := range lines {
		lines[i] = strings.Repeat(" ", pad) + lines[i]
	}
	return strings.Join(lines, "\n")
}


func (s *animeScreen) episodePane(a *App, w, h int) []string {
	acc := s.accent(a)
	out := s.sourceLine(a, w)
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
	if len(s.selEps) > 0 {
		selText := fmt.Sprintf("● %d bölüm indirme için seçildi  ·  d/D indir  ·  v ekle/çıkar  ·  V temizle", len(s.selEps))
		out = append(out, fg(acc).Bold(true).Render(truncate(selText, w)))
	} else if watched > 0 {
		label := fmt.Sprintf(" %d/%d izlendi", watched, len(eps))
		out = append(out, progressBar(w-lipgloss.Width(label), float64(watched)/float64(len(eps)), []string{acc, cAccent2})+stMuted.Render(label))
	} else {
		out = append(out, "")
	}
	listH := h - len(out)
	// the selected episode's note (or the note editor / range editor) sits under the list
	var noteLines []string
	if s.ranging {
		noteLines = append([]string{stSub.Render("İndirilecek bölüm aralığı (örn: 30-100 veya 10-20, 25):")}, s.rangeIn.view(w-1, true, a.frame)...)
	} else if s.noting {
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
		if j, ok := a.dl.ForAnime(s.an, ep.Season, ep.Number); ok {
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
		leftSpans := []span{bar}
		if s.selEps[ep.Number] {
			leftSpans = append(leftSpans, sp("[✓] ", cAccent))
		} else if len(s.selEps) > 0 {
			leftSpans = append(leftSpans, sp("[ ] ", cFaint))
		}
		leftSpans = append(leftSpans, sp(epCell(ep, numW)+"  ", nc), span{t: title, c: tc, bold: sel})
		line := row(w-1, bg, leftSpans, right)
		if i == s.cur {
			line = anchor() + line
		}
		rows = append(rows, line)
	}
	rows = block(rows, w-1, listH)
	out = append(out, hjoin(0, rows, scrollbar(listH, s.off, len(eps)))...)
	return append(out, noteLines...)
}

func (s *animeScreen) selectedEpisodes(eps []source.Episode) []source.Episode {
	var out []source.Episode
	for _, e := range eps {
		if s.selEps[e.Number] {
			out = append(out, e)
		}
	}
	return out
}

func (s *animeScreen) openBatchDownloadMenu(a *App, eps []source.Episode) tea.Cmd {
	if len(eps) == 0 {
		return nil
	}
	curIdx := clamp(s.cur, 0, len(eps)-1)
	curEp := eps[curIdx]
	lastEp := eps[len(eps)-1]
	firstEp := eps[0]

	var items []dropItem
	if len(s.selEps) > 0 {
		picked := s.selectedEpisodes(eps)
		if len(picked) > 0 {
			items = append(items, dropItem{
				icon:  "✓",
				color: cOK,
				label: fmt.Sprintf("Seçili bölümleri indir (%d bölüm)", len(picked)),
				note:  "v ile seçilenler",
				run: func(a *App) tea.Cmd {
					s.selEps = nil
					pk := newPicker(s.an, picked[0], eps, false)
					pk.dlMode, pk.dlSeason = 2, picked
					return tea.Batch(a.push(pk), pk.start(a))
				},
			})
		}
	}

	fromHere := eps[curIdx:]
	if len(fromHere) > 0 {
		items = append(items, dropItem{
			icon:  "▸",
			color: cAccent,
			label: fmt.Sprintf("Seçili bölümden sona kadar indir (%s. – %s. bölüm)", curEp.Num(), lastEp.Num()),
			note:  fmt.Sprintf("%d bölüm", len(fromHere)),
			run: func(a *App) tea.Cmd {
				pk := newPicker(s.an, fromHere[0], eps, false)
				pk.dlMode, pk.dlSeason = 2, fromHere
				return tea.Batch(a.push(pk), pk.start(a))
			},
		})
	}

	items = append(items, dropItem{
		icon:  "✎",
		color: cAccent2,
		label: "Özel bölüm aralığı yazarak indir…",
		note:  fmt.Sprintf("örn: %d-%d", curEp.Number, lastEp.Number),
		run: func(a *App) tea.Cmd {
			s.rangeIn = input{placeholder: "aralık girin (örn: 30-100 veya 10-20, 25)"}
			s.rangeIn.Set(fmt.Sprintf("%d-%d", curEp.Number, lastEp.Number))
			s.ranging = true
			return a.tick()
		},
	})

	wv := a.watchView(s.an)
	var unwatched []source.Episode
	for _, e := range eps {
		if !wv.done(e) {
			unwatched = append(unwatched, e)
		}
	}
	if len(unwatched) > 0 && len(unwatched) < len(eps) {
		items = append(items, dropItem{
			icon:  "◌",
			color: cSky,
			label: fmt.Sprintf("Sadece izlenmeyen bölümleri indir (%d bölüm)", len(unwatched)),
			note:  fmt.Sprintf("%s. – %s. bölüm", unwatched[0].Num(), unwatched[len(unwatched)-1].Num()),
			run: func(a *App) tea.Cmd {
				pk := newPicker(s.an, unwatched[0], eps, false)
				pk.dlMode, pk.dlSeason = 2, unwatched
				return tea.Batch(a.push(pk), pk.start(a))
			},
		})
	}

	items = append(items, dropItem{
		icon:  "↓",
		color: cSub,
		label: fmt.Sprintf("Tüm sezonu indir (%s. – %s. bölüm)", firstEp.Num(), lastEp.Num()),
		note:  fmt.Sprintf("%d bölüm", len(eps)),
		run: func(a *App) tea.Cmd {
			pk := newPicker(s.an, eps[0], eps, false)
			pk.dlMode, pk.dlSeason = 2, eps
			return tea.Batch(a.push(pk), pk.start(a))
		},
	})

	a.openList("↓ toplu bölüm indir", items)
	return a.tick()
}

// parseEpRange parses range strings like "30-100", "30-", "-30", or "10-20, 25, 30-40".
func parseEpRange(spec string, eps []source.Episode) []source.Episode {
	if len(eps) == 0 {
		return nil
	}
	minEp, maxEp := eps[0].Number, eps[len(eps)-1].Number
	want := map[int]bool{}
	parts := strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if idx := strings.IndexAny(p, "-–.."); idx >= 0 {
			left := strings.TrimSpace(p[:idx])
			right := strings.TrimLeft(strings.TrimSpace(p[idx+1:]), "-–.")
			lo, hi := minEp, maxEp
			if left != "" {
				v, err := strconv.Atoi(left)
				if err != nil {
					continue
				}
				lo = v
			}
			if right != "" {
				v, err := strconv.Atoi(right)
				if err != nil {
					continue
				}
				hi = v
			}
			if lo > hi {
				lo, hi = hi, lo
			}
			for n := lo; n <= hi; n++ {
				want[n] = true
			}
		} else if v, err := strconv.Atoi(p); err == nil {
			want[v] = true
		}
	}
	var out []source.Episode
	for _, e := range eps {
		if want[e.Number] {
			out = append(out, e)
		}
	}
	return out
}

// epCell is an episode's number in the list's number column: zero-padded
// for regular episodes (07), as it reads for in-between ones (12,5).
func epCell(ep source.Episode, w int) string {
	if ep.Extra() {
		return fmt.Sprintf("%*s", w, ep.Num())
	}
	return fmt.Sprintf("%0*d", w, ep.Number)
}
