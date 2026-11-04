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

// Following: shows you watch get a "new episode" banner when the live feed
// carries an episode past where you are, and the home calendar uses the
// real AniList airing schedule (local times, countdowns).

type appSchedMsg struct {
	items []anilist.Airing
	err   error
}

func (a *App) loadSchedule() tea.Cmd {
	return run(40*time.Second, func(ctx context.Context) tea.Msg {
		now := time.Now()
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		items, err := anilist.Schedule(ctx, from, from.AddDate(0, 0, 7))
		return appSchedMsg{items, err}
	})
}

type newEp struct {
	an      source.Anime
	season  int
	episode int
	poster  string
}

// newEpisodes: feed episodes newer than your progress on shows you follow
// (watching on your list, or watched in the last 45 days).
func newEpisodes(a *App, feed []source.FeedItem) []newEp {
	var out []newEp
	seen := map[string]bool{}
	for _, it := range feed {
		key := it.Anime.Key()
		if seen[key] {
			continue
		}
		e, ok := a.hist.Get(key)
		if !ok && it.Season <= 1 {
			// watched on another source (season 1 maps across sources)
			if sib, found := a.sibling(it.Anime); found && sib.Season <= 1 {
				e, ok = sib, true
			}
		}
		if !ok {
			continue
		}
		li, inLib := a.libItem(it.Anime)
		if !(inLib && li.Status == store.Watching) && time.Since(e.UpdatedAt) >= 45*24*time.Hour {
			continue // not following it any more
		}
		newer := it.Season > e.Season || (it.Season == e.Season && it.Episode > e.Episode)
		if it.Season == e.Season && it.Episode == e.Episode+1 && !e.Watched[store.EpKey(e.Season, e.Episode)] {
			newer = false // you have not finished the current one yet: not "new" for you
		}
		if newer {
			seen[key] = true
			out = append(out, newEp{an: it.Anime, season: it.Season, episode: it.Episode, poster: it.Anime.Poster})
		}
	}
	return out
}

// calendarAniList renders the home "bu hafta" panel from the real schedule:
// per day, the shows you follow first (with local airing time), then the
// most popular one, then a count.
func calendarAniList(a *App, sched []anilist.Airing, w, ht int) []string {
	mine := following(a)
	now := time.Now()
	type ent struct {
		at       time.Time
		title    string
		ep       int
		followed bool
		pop      int
	}
	byDay := map[int][]ent{}
	for _, x := range sched {
		t := x.At.Local()
		day := int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())).Hours() / 24)
		if day < 0 || day > 6 {
			continue
		}
		_, f := mine[x.Media.IDMal]
		byDay[day] = append(byDay[day], ent{t, x.Media.Name(), x.Episode, f, x.Media.Popularity})
	}
	var out []string
	for d := 0; d < 7 && len(out) < ht; d++ {
		es := byDay[d]
		sort.SliceStable(es, func(i, j int) bool {
			if es[i].followed != es[j].followed {
				return es[i].followed
			}
			return es[i].pop > es[j].pop
		})
		day := now.AddDate(0, 0, d)
		label, lc := trDays[day.Weekday()], cMuted
		switch d {
		case 0:
			label, lc = "Bugün", cAccent
		case 1:
			label = "Yarın"
		}
		var b strings.Builder
		avail := w - 7
		used, shown := 0, 0
		for _, e := range es {
			if !e.followed && shown >= 1 {
				break
			}
			t := e.title
			if e.followed {
				t = e.at.Format("15:04") + " " + t
			}
			tw := len([]rune(t)) + 3
			if used+tw > avail-3 && shown > 0 {
				break
			}
			if shown > 0 {
				b.WriteString(stFaint.Render(" · "))
			}
			c := cSub
			if e.followed {
				c = cAccent2
				if d == 0 && e.at.Before(now) {
					c = cOK
				}
			}
			b.WriteString(fg(c).Render(truncate(t, avail-used)))
			used += tw
			shown++
		}
		if rest := len(es) - shown; rest > 0 {
			b.WriteString(stMuted.Render(fmt.Sprintf(" +%d", rest)))
		}
		if len(es) == 0 {
			b.WriteString(stFaint.Render("—"))
		}
		out = append(out, fit(fg(lc).Bold(d == 0).Render(label), 7)+b.String())
	}
	return out
}
