package ui

// Season sync: one title on a site, several entries on MyAnimeList.
//
// AnimeciX keeps "Bleach" as one title with two seasons (362 + 50 episodes);
// MAL has five entries (Bleach, then four Sennen Kessen-hen parts). The
// seasons package builds the franchise chain from AniList and aligns the
// site's numbering to it. This file wires that into the app:
//
//   - reading: an episode counts as watched when the MAL entry it belongs to
//     says so (watchView), whatever season or part that is;
//   - writing: finishing an episode advances the MAL entry it belongs to;
//     entries other than the title's own are written only after the user
//     agreed once per title (cfg.SeasonSync), and that "yes" backfills what
//     history already knows;
//   - fixing: the anime screen's r menu lets the user correct the MAL match
//     by hand (mappings.json, By: "user").
//
// Without a chain (no MAL id, AniList down, a film) everything behaves as
// before: the title's own MAL entry, first season only.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
	"aniclitr/internal/logx"
	"aniclitr/internal/match"
	"aniclitr/internal/seasons"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

type alignState struct {
	malID   int // the MAL id the chain was built from
	chain   seasons.Chain
	ok      bool
	loading bool
	at      time.Time
}

type alignMsg struct {
	key   string
	malID int
	chain seasons.Chain
	err   error
}

// wantAlign starts building the season chain of an (once per MAL id; a
// failure is retried after a minute). It returns nil when nothing is needed.
func (a *App) wantAlign(an source.Anime) tea.Cmd {
	if an.MalID <= 0 || an.IsMovie() {
		return nil
	}
	if a.aligns == nil {
		a.aligns = map[string]*alignState{}
	}
	key := an.Key()
	if st, ok := a.aligns[key]; ok && st.malID == an.MalID && (st.ok || st.loading || time.Since(st.at) < time.Minute) {
		return nil
	}
	a.aligns[key] = &alignState{malID: an.MalID, loading: true, at: time.Now()}
	malID := an.MalID
	return run(90*time.Second, func(ctx context.Context) tea.Msg {
		ch, err := seasons.BuildByMal(ctx, malID)
		return alignMsg{key: key, malID: malID, chain: ch, err: err}
	})
}

func (a *App) onAlign(m alignMsg) {
	st, ok := a.aligns[m.key]
	if !ok || st.malID != m.malID {
		return // the match was changed meanwhile
	}
	st.loading, st.at = false, time.Now()
	if m.err != nil {
		logx.Infof("sezon zinciri alınamadı [%s mal %d]: %v", m.key, m.malID, m.err)
		return
	}
	st.chain, st.ok = m.chain, len(m.chain.Entries) > 0
	a.malWMemo = nil
}

// providerSeasons is the site's season → episode count, from the details
// or, when a count is missing, from the loaded episode list.
func (a *App) providerSeasons(an source.Anime) map[int]int {
	out := map[int]int{}
	ss := an.Seasons
	if len(ss) == 0 {
		ss = []source.Season{{Number: 1, EpisodeCount: an.EpCount}}
	}
	for _, s := range ss {
		n := s.EpisodeCount
		if eps, ok := a.episodes[epKey(an, s.Number)]; ok && len(eps) > 0 {
			n = len(eps) // what the site actually lists wins
		}
		if n > 0 {
			out[s.Number] = n
		}
	}
	return out
}

// alignment returns how an's seasons map onto MAL entries, if known.
func (a *App) alignment(an source.Anime) (seasons.Alignment, bool) {
	st, ok := a.aligns[an.Key()]
	if !ok || !st.ok || st.malID != an.MalID {
		return seasons.Alignment{}, false
	}
	ps := a.providerSeasons(an)
	if len(ps) == 0 {
		return seasons.Alignment{}, false
	}
	al := seasons.AlignTitle(an.Title, an.Kind, ps, st.chain)
	return al, len(al.Slices) > 0
}

// spansEntries reports whether the title's episodes belong to MAL entries
// other than its own: only then is there something to ask and to write.
func spansEntries(al seasons.Alignment, own int) bool {
	for _, s := range al.Slices {
		if s.MalID > 0 && s.MalID != own {
			return true
		}
	}
	return false
}

func sliceOf(al seasons.Alignment, malID int) (seasons.Slice, bool) {
	for _, s := range al.Slices {
		if s.MalID == malID {
			return s, true
		}
	}
	return seasons.Slice{}, false
}

// canonical maps a site episode to the MAL entry and episode it is. It
// refuses when the site has more episodes than that entry (the alignment
// is unsure there, and a wrong count on MAL is worse than none) unless the
// entry is still airing (count unknown).
func canonical(al seasons.Alignment, ep source.Episode) (seasons.Canon, int, bool) {
	if al.Mode != seasons.Absolute {
		// ToCanonical falls back to the only slice for a season it does not
		// know; a site season the chain has no entry for maps to nothing
		known := false
		for _, s := range al.Slices {
			known = known || s.ProviderSeason == ep.Season
		}
		if !known {
			return seasons.Canon{}, 0, false
		}
	}
	c, ok := al.ToCanonical(ep.Season, ep.Number)
	if !ok || c.MalID <= 0 {
		return c, 0, false
	}
	s, _ := sliceOf(al, c.MalID)
	if c.Overflow && s.CanonEps > 0 {
		return c, s.CanonEps, false
	}
	return c, s.CanonEps, true
}

// seasonSyncOK: may progress go to MAL entries other than the title's own?
func (a *App) seasonSyncOK(an source.Anime) bool {
	return a.mal.Connected() && a.cfg.SeasonSync[an.Key()]
}

// ownMal is the MAL id the title's own library item uses.
func (a *App) ownMal(an source.Anime) int {
	if it, ok := a.libItem(an); ok && it.MalID > 0 {
		return it.MalID
	}
	return an.MalID
}

// recordWatched files a watched episode under the MAL entry it belongs to.
// It reports false when no chain applies and the caller should fall back
// to the old first-season rule.
func (a *App) recordWatched(an source.Anime, ep source.Episode) bool {
	al, ok := a.alignment(an)
	if !ok {
		return false
	}
	own := a.ownMal(an)
	if spansEntries(al, own) && !a.seasonSyncOK(an) {
		return false // not agreed (or MAL not connected): old behaviour
	}
	c, total, ok := canonical(al, ep)
	if !ok {
		logx.Infof("sezon eşleme: %s S%dE%d MAL'e yazılmadı (hizalama emin değil)", an.Key(), ep.Season, ep.Number)
		return true // known, but unsure here: write nothing rather than wrong
	}
	if c.MalID == own {
		a.libSet(an, func(x *store.Item) {
			if c.Episode > x.Watched {
				x.Watched = c.Episode
			}
			if total > 0 && c.Episode >= total && spansEntries(al, own) {
				x.Status = store.Completed // this entry is done, the title goes on
			} else if x.Status == store.Planned || x.Status == store.OnHold {
				x.Status = store.Watching
			}
		})
		return true
	}
	a.writeEntry(c.MalID, c.Episode, total, entryTitle(a, an, c))
	return true
}

// writeEntry advances one MAL entry in the library: the item that already
// has that MAL id (whatever its key), or a new "mal:<id>" item.
func (a *App) writeEntry(malID, ep, total int, title string) {
	target := source.Anime{Source: "mal", ID: strconv.Itoa(malID), MalID: malID, Title: title, Kind: "TV", EpCount: total}
	if it, ok := a.lib.FindMal(malID); ok {
		target = source.Anime{Source: it.Source, ID: it.AnimeID, MalID: malID}
	}
	a.lib.Set(target, func(x *store.Item) {
		if ep > x.Watched {
			x.Watched = ep
		}
		switch {
		case total > 0 && ep >= total:
			x.Status = store.Completed
		case x.Status == "" || x.Status == store.Planned || x.Status == store.OnHold:
			x.Status = store.Watching
		}
	})
}

func entryTitle(a *App, an source.Anime, c seasons.Canon) string {
	if st, ok := a.aligns[an.Key()]; ok {
		for _, e := range st.chain.Entries {
			if e.MalID == c.MalID && e.Title != "" {
				return e.Title
			}
		}
	}
	return fmt.Sprintf("%s · %d. sezon", an.Title, c.Season)
}

// malWatched is MAL id → episodes watched per the library (completed: all).
func (a *App) malWatched() map[int]int {
	if rev := a.lib.Rev(); a.malWMemo != nil && rev == a.malWRev {
		return a.malWMemo
	}
	m := map[int]int{}
	for _, it := range a.lib.All() {
		if it.MalID <= 0 {
			continue
		}
		n := it.Watched
		if it.Status == store.Completed {
			n = 1 << 30
		}
		if n > m[it.MalID] {
			m[it.MalID] = n
		}
	}
	a.malWRev, a.malWMemo = a.lib.Rev(), m
	return m
}

// ---- asking once ----

// maybeAskSeasonSync offers, once per title, to write progress to the other
// MAL entries the title spans. Called when the chain arrives on the anime
// screen; it stays quiet while anything else is open.
func (a *App) maybeAskSeasonSync(an source.Anime) {
	if !a.mal.Connected() || a.drop != nil || a.pal != nil || a.popup != nil {
		return
	}
	if _, asked := a.cfg.SeasonSync[an.Key()]; asked {
		return
	}
	al, ok := a.alignment(an)
	if !ok || !spansEntries(al, a.ownMal(an)) {
		return
	}
	n := map[int]bool{}
	for _, s := range al.Slices {
		n[s.MalID] = true
	}
	decide := func(yes bool) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd {
			if a.cfg.SeasonSync == nil {
				a.cfg.SeasonSync = map[string]bool{}
			}
			a.cfg.SeasonSync[an.Key()] = yes
			_ = a.cfg.Save()
			if !yes {
				a.notify("yalnızca ilk sezon eşitlenecek · fikrini r menüsünden değiştirebilirsin", "ok")
				return nil
			}
			k := a.backfill(an)
			a.notify(fmt.Sprintf("sezonlar MAL'de ayrı kayıtlara yazılacak · %d kayıt güncellendi", k), "ok")
			return nil
		}
	}
	a.openList("MAL sezon eşitlemesi", []dropItem{
		{group: fmt.Sprintf("bu seri MAL'de %d ayrı kayıt", len(n))},
		{icon: "✓", color: cOK, label: "Her sezonu kendi MAL kaydına yaz", note: "izlediklerin de aktarılır", run: decide(true)},
		{icon: "·", label: "Yalnızca ilk kaydı eşitle", note: an.Title, run: decide(false)},
	})
}

// backfill writes what history already knows into the MAL entries, after
// the user agreed. It returns how many entries moved.
func (a *App) backfill(an source.Anime) int {
	al, ok := a.alignment(an)
	if !ok {
		return 0
	}
	ent, ok := a.hist.Get(an.Key())
	if !ok {
		return 0
	}
	type best struct{ ep, total int }
	most := map[int]best{}
	for k, w := range ent.Watched {
		if !w {
			continue
		}
		var s, e int
		if _, err := fmt.Sscanf(k, "%d:%d", &s, &e); err != nil {
			continue
		}
		c, total, ok := canonical(al, source.Episode{Season: s, Number: e})
		if ok && c.Episode > most[c.MalID].ep {
			most[c.MalID] = best{c.Episode, total}
		}
	}
	own := a.ownMal(an)
	ids := make([]int, 0, len(most))
	for id := range most {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	moved := 0
	for _, id := range ids {
		b := most[id]
		if id == own {
			continue // the title's own entry is already kept by the old rule
		}
		if cur, ok := a.malWatched()[id]; ok && cur >= b.ep {
			continue
		}
		a.writeEntry(id, b.ep, b.total, entryTitle(a, an, seasons.Canon{MalID: id}))
		moved++
	}
	return moved
}

// ---- fixing the match by hand ----

type malCandsMsg struct {
	key   string
	cands []anilist.Media
	err   error
}

// matchMenu is the anime screen's r menu when nothing failed: correct or
// reset the MAL match, and change the season-sync answer.
func (a *App) matchMenu(s *animeScreen) {
	an := s.an
	cur := "MAL eşlemesi yok"
	if an.MalID > 0 {
		cur = "şu an MAL #" + strconv.Itoa(an.MalID)
		if e, ok := match.Default().Get(an.Key()); ok && e.By == "user" {
			cur += " (elle seçildi)"
		}
	}
	items := []dropItem{
		{group: "eşleme"},
		{icon: "⌕", color: cAccent, label: "MAL eşlemesini düzelt", note: cur, run: func(a *App) tea.Cmd {
			a.notify("AniList'te aranıyor…", "ok")
			return tea.Batch(a.tick(), searchMalCands(an))
		}},
		{icon: "↻", label: "Eşlemeyi sıfırla ve yeniden ara", note: an.Title, run: func(a *App) tea.Cmd {
			match.Default().Delete(an.Key())
			delete(a.aligns, an.Key())
			r := newResults(an.Title)
			a.notify("eşleme sıfırlandı — yeniden aranıyor", "ok")
			return tea.Batch(a.push(r), r.start(a))
		}},
	}
	if v, asked := a.cfg.SeasonSync[an.Key()]; asked {
		label, next := "Sezonları ayrı MAL kayıtlarına yazmayı kapat", false
		if !v {
			label, next = "Sezonları ayrı MAL kayıtlarına yaz", true
		}
		items = append(items, dropItem{group: "sezon eşitlemesi"}, dropItem{icon: "⇄", label: label, run: func(a *App) tea.Cmd {
			a.cfg.SeasonSync[an.Key()] = next
			_ = a.cfg.Save()
			if next {
				a.notify(fmt.Sprintf("açıldı · %d kayıt güncellendi", a.backfill(an)), "ok")
			} else {
				a.notify("kapatıldı · yalnızca ilk kayıt eşitlenecek", "ok")
			}
			return nil
		}})
	}
	a.openList("kurtarma · "+an.Title, items)
}

func searchMalCands(an source.Anime) tea.Cmd {
	key := an.Key()
	titles := an.AllTitles()
	return run(30*time.Second, func(ctx context.Context) tea.Msg {
		var out []anilist.Media
		seen := map[int]bool{}
		var lastErr error
		for _, q := range match.BuildQueries(titles) {
			res, err := anilist.Search(ctx, q)
			if err != nil {
				lastErr = err
				continue
			}
			for _, m := range res {
				if m.IDMal > 0 && !seen[m.IDMal] {
					seen[m.IDMal] = true
					out = append(out, m)
				}
			}
			if len(out) >= 12 {
				break
			}
		}
		if len(out) == 0 && lastErr != nil {
			return malCandsMsg{key: key, err: lastErr}
		}
		return malCandsMsg{key: key, cands: out}
	})
}

// onMalCands shows the candidates; picking one stores a user mapping.
func (a *App) onMalCands(m malCandsMsg) {
	s, ok := a.top().(*animeScreen)
	if !ok || s.an.Key() != m.key {
		return
	}
	if m.err != nil {
		a.notifyErr("AniList", m.err)
		return
	}
	if len(m.cands) == 0 {
		a.notify("AniList'te aday bulunamadı", "warn")
		return
	}
	items := []dropItem{{group: "doğru kaydı seç"}}
	for _, c := range m.cands {
		c := c
		meta := []string{}
		if c.SeasonYear > 0 {
			meta = append(meta, strconv.Itoa(c.SeasonYear))
		}
		if c.Format != "" {
			meta = append(meta, strings.ToUpper(c.Format))
		}
		if c.Episodes > 0 {
			meta = append(meta, fmt.Sprintf("%d bölüm", c.Episodes))
		}
		meta = append(meta, "MAL #"+strconv.Itoa(c.IDMal))
		icon, color := "·", ""
		if c.IDMal == s.an.MalID {
			icon, color = "●", cOK
		}
		items = append(items, dropItem{icon: icon, color: color, label: c.Name(), note: strings.Join(meta, " · "),
			run: func(a *App) tea.Cmd { return a.setUserMal(m.key, c) }})
	}
	a.openList("MAL eşlemesi", items)
}

// setUserMal applies a hand-picked MAL entry to the open title.
func (a *App) setUserMal(key string, c anilist.Media) tea.Cmd {
	match.Default().SetUser(key, c.IDMal, c.ID, c.Name())
	if d, ok := a.details[key]; ok {
		d.MalID = c.IDMal
		a.details[key] = d
	}
	delete(a.aligns, key)
	if a.cfg.SeasonSync != nil {
		delete(a.cfg.SeasonSync, key) // a new match: ask again
		_ = a.cfg.Save()
	}
	var cmd tea.Cmd
	for _, sc := range a.stack {
		if s, ok := sc.(*animeScreen); ok && s.an.Key() == key {
			old := s.an.MalID
			s.an.MalID, s.base.MalID = c.IDMal, c.IDMal
			if it, ok := a.lib.Get(key); ok && it.MalID != c.IDMal {
				a.lib.Mutate(key, func(x *store.Item) { x.MalID, x.MalDirty, x.NoMal = c.IDMal, true, false })
				logx.Infof("MAL eşlemesi elle değişti [%s]: %d → %d", key, old, c.IDMal)
				if a.mal.Connected() {
					a.msync.SyncNow()
				}
			}
			cmd = a.wantAlign(s.an)
		}
	}
	a.notify("MAL eşlemesi: "+c.Name(), "ok")
	return tea.Batch(cmd, a.tick())
}

// userMal applies a stored hand-picked match to freshly loaded details.
func userMal(an source.Anime) source.Anime {
	if e, ok := match.Default().Get(an.Key()); ok && e.By == "user" && e.MalID > 0 {
		an.MalID = e.MalID
	}
	return an
}
