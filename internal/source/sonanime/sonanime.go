// Package sonanime implements sonanime.com: an open JSON API with the MAL
// id in every title and direct MP4s (1080p/720p/480p) per episode.
//
// Notes (verified live 2026-11-07):
//   - /api/anime/search?q= lists titles; anime_link is the stable slug.
//   - /api/anime/link/{slug} carries mal_id, genres and every episode of
//     every season, each with its MP4 links. Links carry links_expire_at
//     (about a day), so Resolve always fetches them fresh.
//   - The MP4 host answers Range requests (206) with a sonanime.com Referer.
//
// Endpoints learnt from Migurdex's SonAnime plugin (GPL-3.0); this is our
// own code.
package sonanime

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/extract"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

// apiBase is a var so tests can point the source at recorded answers.
var apiBase = "https://api.sonanime.com/api"

const site = "https://sonanime.com"

var headers = map[string]string{"Referer": site + "/", "Origin": site, "Accept": "application/json"}

type Provider struct{}

func init() { source.Register(Provider{}) }

func (Provider) Info() source.Info {
	return source.Info{ID: "sonanime", Name: "SonAnime", Lang: "tr",
		Note: "Doğrudan MP4 (1080p'ye kadar), MAL kimliği hazır"}
}

type apiAnime struct {
	ID       source.FlexInt    `json:"anime_id"`
	Name     string            `json:"anime_name"`
	NameEN   source.FlexString `json:"anime_name_en"`
	Type     source.FlexString `json:"anime_type"`
	Desc     source.FlexString `json:"anime_description"`
	Year     source.FlexInt    `json:"anime_year"`
	Photo    source.FlexString `json:"anime_photo"`
	Link     string            `json:"anime_link"`
	MalID    source.FlexInt    `json:"mal_id"`
	Genres   []string          `json:"anime_genres"`
	Episodes []apiEpisode      `json:"episodes"`
}

type apiEpisode struct {
	ID       source.FlexInt    `json:"id"`
	Season   source.FlexInt    `json:"anime_season"`
	Number   source.FlexString `json:"episode_number"`
	L1080    source.FlexString `json:"episode_link_1080"`
	L720     source.FlexString `json:"episode_link_720"`
	L480     source.FlexString `json:"episode_link_480"`
	L360     source.FlexString `json:"episode_link_360"`
	Uploaded source.FlexString `json:"upload_date"`
}

func (t apiAnime) toAnime() source.Anime {
	a := source.Anime{
		Source:      "sonanime",
		ID:          t.Link,
		Title:       strings.TrimSpace(t.Name),
		Year:        int(t.Year),
		Kind:        "TV",
		Poster:      string(t.Photo),
		Description: strings.TrimSpace(string(t.Desc)),
		MalID:       int(t.MalID),
		Genres:      t.Genres,
	}
	if k := strings.ToLower(string(t.Type)); strings.Contains(k, "movie") || strings.Contains(k, "film") {
		a.Kind = "Film"
	}
	if en := strings.TrimSpace(string(t.NameEN)); en != "" && !strings.EqualFold(en, a.Title) {
		a.AltTitle = en
		a.Titles = []string{en}
	}
	a.Normalize()
	return a
}

func (Provider) Probe(ctx context.Context) error {
	return source.ProbeChain(ctx, Provider{}, "one piece")
}

func (Provider) Search(ctx context.Context, q string) ([]source.Anime, error) {
	q = source.Clean(q)
	if q == "" {
		return nil, nil
	}
	var res []apiAnime
	if err := netx.GetJSON(ctx, apiBase+"/anime/search?q="+url.QueryEscape(q), headers, &res); err != nil {
		return nil, err
	}
	out := make([]source.Anime, 0, len(res))
	for _, t := range res {
		if t.Link == "" || t.Name == "" {
			continue
		}
		out = append(out, t.toAnime())
	}
	return out, nil
}

// titles caches /anime/link answers briefly: Details and Episodes read the
// same document. Resolve never uses it (the MP4 links expire).
var (
	cacheMu sync.Mutex
	cache   = map[string]cached{}
)

type cached struct {
	t  apiAnime
	at time.Time
}

func title(ctx context.Context, slug string, fresh bool) (apiAnime, error) {
	if !fresh {
		cacheMu.Lock()
		c, ok := cache[slug]
		cacheMu.Unlock()
		if ok && time.Since(c.at) < 5*time.Minute {
			return c.t, nil
		}
	}
	var t apiAnime
	if err := netx.GetJSON(ctx, apiBase+"/anime/link/"+url.PathEscape(slug), headers, &t); err != nil {
		return apiAnime{}, err
	}
	if t.Link == "" && t.Name == "" {
		return apiAnime{}, source.ErrNotFound
	}
	cacheMu.Lock()
	cache[slug] = cached{t: t, at: time.Now()}
	cacheMu.Unlock()
	return t, nil
}

func (Provider) Details(ctx context.Context, a source.Anime) (source.Anime, error) {
	t, err := title(ctx, a.ID, false)
	if err != nil {
		return a, err
	}
	d := t.toAnime()
	if d.ID == "" {
		d.ID = a.ID
	}
	counts := map[int]int{}
	for _, e := range t.Episodes {
		counts[max(1, int(e.Season))]++
	}
	var nums []int
	for s := range counts {
		nums = append(nums, s)
	}
	sort.Ints(nums)
	for _, s := range nums {
		d.Seasons = append(d.Seasons, source.Season{Number: s, Name: fmt.Sprintf("%d. Sezon", s), EpisodeCount: counts[s]})
		d.EpCount += counts[s]
	}
	return d, nil
}

func (Provider) Episodes(ctx context.Context, a source.Anime, season int) ([]source.Episode, error) {
	t, err := title(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	var eps []source.Episode
	for _, e := range t.Episodes {
		if max(1, int(e.Season)) != season {
			continue
		}
		n, ok := source.ParseEpisodeNumber(string(e.Number))
		if !ok {
			continue
		}
		eps = append(eps, source.Episode{ID: strconv.Itoa(int(e.ID)), Season: season, Number: n})
	}
	if len(eps) == 0 {
		return nil, source.ErrNotFound
	}
	source.SortEpisodes(eps)
	return source.NormalizeEpisodes(&a, eps), nil
}

// Variants: SonAnime has one upload per episode (its own encode). The URL
// names the episode; Resolve fetches its links fresh.
func (Provider) Variants(_ context.Context, a source.Anime, ep source.Episode) ([]source.Variant, error) {
	if ep.ID == "" {
		return nil, source.ErrNotFound
	}
	return []source.Variant{{Fansub: "SonAnime", Host: "SonAnime MP4", URL: a.ID + "|" + ep.ID, Kind: "mp4", Lang: "tr"}}, nil
}

func (Provider) Resolve(ctx context.Context, v source.Variant) (*source.Stream, error) {
	slug, epID, ok := strings.Cut(v.URL, "|")
	if !ok || slug == "" || epID == "" {
		return nil, extract.NewError(extract.KindNotFound, "sonanime: bölüm kimliği yok")
	}
	t, err := title(ctx, slug, true) // links expire: always fresh
	if err != nil {
		return nil, &extract.Error{Extractor: "SonAnime", Kind: extract.KindOf(err), Detail: err.Error(), Err: err}
	}
	for _, e := range t.Episodes {
		if strconv.Itoa(int(e.ID)) != epID {
			continue
		}
		st := &source.Stream{Referer: site + "/", MalID: int(t.MalID)}
		for _, q := range []struct {
			u string
			h int
		}{{string(e.L1080), 1080}, {string(e.L720), 720}, {string(e.L480), 480}, {string(e.L360), 360}} {
			if strings.HasPrefix(q.u, "http") {
				st.Qualities = append(st.Qualities, source.Quality{Label: fmt.Sprintf("%dp", q.h), URL: q.u, Height: q.h})
			}
		}
		if len(st.Qualities) == 0 {
			return nil, extract.NewError(extract.KindProcessing, "sonanime: bölümün video bağlantısı yok")
		}
		if dead, kind := extract.ProbeMP4s(ctx, st); dead {
			return nil, &extract.Error{Extractor: "SonAnime", Kind: kind, Detail: "sonanime: hiçbir kalite açılmıyor"}
		}
		return st, nil
	}
	return nil, extract.NewError(extract.KindDeleted, "sonanime: bölüm kaldırılmış")
}
