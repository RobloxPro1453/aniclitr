package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/config"
	"aniclitr/internal/source"
)

// ---- feed cache: the home screen paints instantly (and offline) ----

func feedCachePath() string { return filepath.Join(config.CacheDir(), "akis.json") }

func saveFeedCache(items []source.FeedItem) {
	if b, err := json.Marshal(items); err == nil {
		_ = config.WriteAtomic(feedCachePath(), b)
	}
}

func loadFeedCache() []source.FeedItem {
	b, err := os.ReadFile(feedCachePath())
	if err != nil {
		return nil
	}
	var items []source.FeedItem
	if json.Unmarshal(b, &items) != nil {
		return nil
	}
	return items
}

// ---- dwell prefetch ----
//
// When the cursor rests on an episode for a moment, its fansub list is
// fetched and the preferred fansub's best host is resolved — so enter
// starts playback without waiting. One episode at a time; moving on
// cancels the intent (nothing is fetched for episodes you scroll past).

type dwellMsg struct {
	gen int
	key string
}

const dwellAfter = 450 * time.Millisecond

func (s *animeScreen) dwell(a *App) tea.Cmd {
	s.dwellGen++
	eps := s.episodes(a)
	if s.cur >= len(eps) {
		return nil
	}
	gen, key := s.dwellGen, varKey(s.an, eps[s.cur])
	return tea.Tick(dwellAfter, func(time.Time) tea.Msg { return dwellMsg{gen: gen, key: key} })
}

func (s *animeScreen) onDwell(a *App, m dwellMsg) tea.Cmd {
	eps := s.episodes(a)
	if m.gen != s.dwellGen || s.cur >= len(eps) || varKey(s.an, eps[s.cur]) != m.key {
		return nil
	}
	if _, ok := a.dl.Local(s.an.Key(), eps[s.cur].Season, eps[s.cur].Number); ok {
		return nil // plays from disk anyway
	}
	if vs, ok := a.variants[m.key]; ok && vs != nil {
		return s.warm(a, m.key)
	}
	return a.fetchVariants(s.an, eps[s.cur])
}

// warm resolves the preferred fansub's first working-looking host.
func (s *animeScreen) warm(a *App, key string) tea.Cmd {
	eps := s.episodes(a)
	if s.cur >= len(eps) || varKey(s.an, eps[s.cur]) != key || a.top() != screen(s) {
		return nil
	}
	vs := a.variants[key]
	if len(vs) == 0 {
		return nil
	}
	pref := ""
	if e, ok := a.hist.Get(s.an.Key()); ok {
		pref = strings.ToLower(shortFansub(e.Fansub))
	}
	pick := vs[0]
	for _, v := range vs {
		if pref != "" && strings.ToLower(shortFansub(v.Fansub)) == pref {
			pick = v
			break
		}
	}
	if recentlyFailed(a, pick.URL) {
		return nil
	}
	return a.resolve(s.an.Source, pick)
}
