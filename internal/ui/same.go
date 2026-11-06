package ui

import (
	"sort"
	"strconv"

	"aniclitr/internal/match"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// One anime often lives on several sources (AnimeciX, AnimeTR, HiAnime…),
// and history keeps one entry per source. Everything the user sees as "this
// anime" goes through here, so a show is one card on the shelf and its other
// sources are one keypress away.

// sameKeys identifies an anime across sources: its MAL id and its folded
// titles (the same keys that merge search results).
func sameKeys(an source.Anime) []string {
	ks := titleKeys(an)
	if an.MalID > 0 {
		ks = append(ks, "mal:"+strconv.Itoa(an.MalID))
	}
	return ks
}

func sameAnime(x, y source.Anime) bool {
	return match.SameAnime(x, y)
}

// recent is history newest first with one entry per anime: the most recently
// watched source stands for the show.
func (a *App) recent() []store.Entry {
	// home asks every frame: answer from memory until history changes
	if rev := a.hist.Rev(); a.recentOK && rev == a.recentRev {
		return a.recentMemo
	}
	var out []store.Entry
next:
	for _, e := range a.hist.Recent() {
		for _, o := range out {
			if sameAnime(o.Anime(), e.Anime()) {
				continue next
			}
		}
		out = append(out, e)
	}
	a.recentRev, a.recentMemo, a.recentOK = a.hist.Rev(), out, true
	return out
}

// siblings lists the same anime on other sources, from history and the
// library (newest first), so `s` works without a lookup.
func (a *App) siblings(an source.Anime) []source.Anime {
	var out []source.Anime
	add := func(x source.Anime) {
		if x.Source == an.Source || x.Source == "mal" || source.Get(x.Source) == nil || !sameAnime(an, x) {
			return
		}
		for _, o := range out {
			if o.Source == x.Source {
				return
			}
		}
		out = append(out, x)
	}
	for _, e := range a.hist.Recent() {
		add(e.Anime())
	}
	for _, it := range a.lib.All() {
		add(it.Anime())
	}
	return out
}

// sibling finds where you left this anime on another source.
func (a *App) sibling(an source.Anime) (store.Entry, bool) {
	for _, e := range a.hist.Recent() {
		if e.Source != an.Source && sameAnime(an, e.Anime()) {
			return e, true
		}
	}
	return store.Entry{}, false
}

// libItem finds this anime in the library whatever key it sits under: its
// own source key, its MAL id (a show pulled from MAL is "mal:<id>" until it
// is opened on a source), or the same title on another source.
func (a *App) libItem(an source.Anime) (store.Item, bool) {
	if it, ok := a.lib.Get(an.Key()); ok {
		return it, true
	}
	// views ask every frame for many rows: remember answers until the
	// library changes
	ck := an.Key() + "|" + strconv.Itoa(an.MalID) + "|" + an.Kind + "|" + an.Title
	if rev := a.lib.Rev(); rev != a.libRev || a.libMemo == nil {
		a.libRev, a.libMemo = rev, map[string]libHit{}
	}
	if h, ok := a.libMemo[ck]; ok {
		if !h.ok {
			return store.Item{}, false
		}
		return a.lib.Get(h.key)
	}
	it, ok := a.findLibItem(an)
	a.libMemo[ck] = libHit{key: it.Key(), ok: ok}
	return it, ok
}

type libHit struct {
	key string
	ok  bool
}

func (a *App) findLibItem(an source.Anime) (store.Item, bool) {
	var byTitle store.Item
	found := false
	for _, it := range a.lib.All() {
		x := it.Anime()
		if an.MalID != 0 && it.MalID == an.MalID {
			// sites sometimes give a movie its series' id: kinds must agree
			if x.Kind == "" || an.Kind == "" || x.IsMovie() == an.IsMovie() {
				return it, true
			}
			continue
		}
		if !found && (an.MalID == 0 || it.MalID == 0) && sameAnime(an, x) {
			byTitle, found = it, true
		}
	}
	return byTitle, found
}

// libAnime is an as the library should store it: when the show is already
// there under another key, the write goes to that item instead of starting
// a second one. (With a MAL id the library adopts the item by itself.)
func (a *App) libAnime(an source.Anime) source.Anime {
	it, ok := a.libItem(an)
	if !ok || it.Key() == an.Key() || (an.MalID != 0 && it.MalID == an.MalID) {
		return an
	}
	x := an
	x.Source, x.ID = it.Source, it.AnimeID
	if x.MalID == 0 {
		x.MalID = it.MalID
	}
	return x
}

func (a *App) libSet(an source.Anime, fn func(it *store.Item)) store.Item {
	return a.lib.Set(a.libAnime(an), fn)
}

// watchView answers "have I seen this episode?" from both what you played
// here and what your list says (MAL's watched count). MAL counts one title,
// which here is the first season: only that season takes the count.
type watchView struct {
	ent    store.Entry
	upTo   int // episodes 1..upTo of the first season are watched per the list
	first  int // first season number
	listed bool
}

func (a *App) watchView(an source.Anime) watchView {
	v := watchView{first: 1}
	v.ent, _ = a.hist.Get(an.Key())
	if len(an.Seasons) > 0 {
		v.first = an.Seasons[0].Number
	}
	if it, ok := a.libItem(an); ok {
		v.listed = true
		v.upTo = it.Watched
		if it.Status == store.Completed {
			v.upTo = 1 << 30
		}
	}
	return v
}

func (v watchView) done(ep source.Episode) bool {
	return v.ent.Watched[store.EpKey(ep.Season, ep.Number)] || (ep.Season == v.first && ep.Number <= v.upTo)
}

// rankSources orders the same show on several sources: sources switched on
// first, then the registry's rank (primary, Turkish, English, archive).
func rankSources(a *App, xs []source.Anime) []source.Anime {
	rank := map[string]int{}
	for i, p := range source.All() {
		in := p.Info()
		r := i
		if !a.cfg.Enabled(in.ID, in.Legacy) {
			r += 100
		}
		rank[in.ID] = r
	}
	out := append([]source.Anime(nil), xs...)
	of := func(id string) int {
		if r, ok := rank[id]; ok {
			return r
		}
		return 1000 // unknown source: last
	}
	sort.SliceStable(out, func(i, j int) bool { return of(out[i].Source) < of(out[j].Source) })
	return out
}
