// Package anihub implements AniHub (anihub.com.tr), a Turkish anime provider
// with a public JSON REST API and token-signed HLS master playlists.
//
// Endpoints (from REFERANS_RAPORU.md §10.12):
//   - Search:   GET  https://api.anihub.com.tr/v1/animes?q={q}
//   - Details:  GET  https://api.anihub.com.tr/v1/animes/{slug}
//   - Episode:  GET  https://api.anihub.com.tr/v1/episodes/{slug}
//   - Token:    POST https://api.anihub.com.tr/v1/video-tokens {"masterPlaylistKey": "…"}
//   - Media:    https://media.anihub.com.tr/{posterKey}
package anihub

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"

	"aniclitr/internal/extract"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

const (
	webBase   = "https://anihub.com.tr"
	mediaBase = "https://media.anihub.com.tr"
)

// apiBase is a var so unit tests can point it at an httptest.Server.
var apiBase = "https://api.anihub.com.tr"

var headers = map[string]string{
	"Origin":  webBase,
	"Referer": webBase + "/",
}

type Provider struct{}

func init() { source.Register(Provider{}) }

func (Provider) Info() source.Info {
	return source.Info{
		ID:   "anihub",
		Name: "AniHub",
		Lang: "tr",
		Note: "Türkçe altyazılı anime arşivi, doğrudan HLS akışı",
	}
}

type envelope[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

type animeListData struct {
	Items []animeItem `json:"items"`
}

type animeItem struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Slug        string            `json:"slug"`
	Description string            `json:"description"`
	Year        source.FlexInt    `json:"year"`
	PosterKey   string            `json:"posterKey"`
	ContentType source.FlexString `json:"contentType"`
	Type        source.FlexString `json:"type"`
	Format      source.FlexString `json:"format"`
}

type animeDetailData struct {
	Anime    *animeItem    `json:"anime"`
	Episodes []episodeItem `json:"episodes"`
	Series   *seriesData   `json:"series"`
}

type episodeItem struct {
	ID     string  `json:"id"`
	Slug   string  `json:"slug"`
	Number float64 `json:"number"`
	Title  string  `json:"title"`
}

type seriesData struct {
	CurrentSeasonIndex int          `json:"currentSeasonIndex"`
	Seasons            []seasonItem `json:"seasons"`
}

type seasonItem struct {
	Order int    `json:"order"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type episodeDetailData struct {
	Playback *playbackData `json:"playback"`
}

type playbackData struct {
	Status            string         `json:"status"`
	MasterPlaylistKey string         `json:"masterPlaylistKey"`
	MasterPlaylistURL string         `json:"masterPlaylistUrl"`
	Subtitles         []subtitleItem `json:"subtitles"`
}

type subtitleItem struct {
	URL      string `json:"url"`
	Language string `json:"language"`
	Label    string `json:"label"`
}

type tokenData struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
}

var (
	cacheMu     sync.Mutex
	detailCache = map[string]animeDetailData{}
)

func buildMediaURL(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(key), "http") {
		return key
	}
	return mediaBase + "/" + strings.TrimLeft(key, "/")
}

func parseKind(ct, typ, fmtStr string) string {
	v := strings.TrimSpace(ct)
	if v == "" {
		v = strings.TrimSpace(typ)
	}
	if v == "" {
		v = strings.TrimSpace(fmtStr)
	}
	switch strings.ToLower(v) {
	case "movie", "film":
		return "Film"
	case "ova":
		return "OVA"
	case "special", "özel":
		return "Özel"
	}
	return "TV"
}

func normalizeSlug(id string) string {
	s := strings.Trim(strings.TrimSpace(id), "/")
	if strings.HasPrefix(strings.ToLower(s), "http") {
		if u, err := url.Parse(s); err == nil {
			s = strings.Trim(u.Path, "/")
		}
	}
	for _, p := range []string{"anime/", "animes/", "izle/", "episode/", "episodes/"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = s[len(p):]
			break
		}
	}
	return strings.Trim(s, "/")
}

func resolveSeasonNumber(slug string, series *seriesData) int {
	if series != nil && len(series.Seasons) > 0 {
		for _, se := range series.Seasons {
			if strings.EqualFold(se.Slug, slug) {
				if se.Order > 0 {
					return se.Order
				}
				return 1
			}
		}
		if series.CurrentSeasonIndex >= 0 && series.CurrentSeasonIndex < len(series.Seasons) {
			if o := series.Seasons[series.CurrentSeasonIndex].Order; o > 0 {
				return o
			}
		}
	}
	return source.ParseSeasonNumber(strings.ReplaceAll(slug, "-", " "))
}

func fetchDetail(ctx context.Context, slug string) (animeDetailData, error) {
	slug = normalizeSlug(slug)
	if slug == "" {
		return animeDetailData{}, source.ErrNotFound
	}
	cacheMu.Lock()
	if d, ok := detailCache[slug]; ok && d.Anime != nil {
		cacheMu.Unlock()
		return d, nil
	}
	cacheMu.Unlock()

	var env envelope[animeDetailData]
	u := apiBase + "/v1/animes/" + url.PathEscape(slug)
	if err := netx.GetJSON(ctx, u, headers, &env); err != nil {
		return animeDetailData{}, err
	}
	if env.Data.Anime == nil {
		return animeDetailData{}, source.ErrNotFound
	}
	cacheMu.Lock()
	detailCache[slug] = env.Data
	cacheMu.Unlock()
	return env.Data, nil
}

func (Provider) Probe(ctx context.Context) error {
	res, err := (Provider{}).Search(ctx, "frieren")
	if err != nil {
		return err
	}
	if len(res) == 0 {
		return source.ErrNotFound
	}
	return nil
}

func (Provider) Search(ctx context.Context, q string) ([]source.Anime, error) {
	q = source.Clean(q)
	if q == "" {
		return nil, nil
	}
	var env envelope[animeListData]
	u := apiBase + "/v1/animes?q=" + url.QueryEscape(q)
	if err := netx.GetJSON(ctx, u, headers, &env); err != nil {
		return nil, err
	}
	out := make([]source.Anime, 0, len(env.Data.Items))
	for _, it := range env.Data.Items {
		slug := strings.TrimSpace(it.Slug)
		if slug == "" {
			continue
		}
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = slug
		}
		a := source.Anime{
			Source:      "anihub",
			ID:          slug,
			Title:       title,
			Year:        int(it.Year),
			Kind:        parseKind(string(it.ContentType), string(it.Type), string(it.Format)),
			Poster:      buildMediaURL(it.PosterKey),
			Description: strings.TrimSpace(it.Description),
		}
		a.Normalize()
		out = append(out, a)
	}
	return out, nil
}

func (Provider) Details(ctx context.Context, a source.Anime) (source.Anime, error) {
	d, err := fetchDetail(ctx, a.ID)
	if err != nil {
		return a, err
	}
	an := d.Anime
	if an.Title != "" {
		a.Title = strings.TrimSpace(an.Title)
	}
	if an.Description != "" {
		a.Description = strings.TrimSpace(an.Description)
	}
	if p := buildMediaURL(an.PosterKey); p != "" {
		a.Poster = p
	}
	if an.Year > 0 {
		a.Year = int(an.Year)
	}
	a.Kind = parseKind(string(an.ContentType), string(an.Type), string(an.Format))
	if len(d.Episodes) > 0 {
		a.EpCount = len(d.Episodes)
	}

	seasonNum := resolveSeasonNumber(a.ID, d.Series)
	if d.Series != nil && len(d.Series.Seasons) > 0 {
		a.Seasons = nil
		seen := map[int]bool{}
		for i, se := range d.Series.Seasons {
			num := se.Order
			if num <= 0 {
				num = i + 1
			}
			if seen[num] {
				continue
			}
			seen[num] = true
			name := fmt.Sprintf("Sezon %d", num)
			if strings.TrimSpace(se.Title) != "" {
				name = strings.TrimSpace(se.Title)
			}
			a.Seasons = append(a.Seasons, source.Season{Number: num, Name: name})
		}
		sort.Slice(a.Seasons, func(i, j int) bool { return a.Seasons[i].Number < a.Seasons[j].Number })
	}
	if len(a.Seasons) == 0 {
		a.Seasons = []source.Season{{Number: seasonNum, Name: fmt.Sprintf("Sezon %d", seasonNum)}}
	}
	a.Normalize()
	return a, nil
}

func (Provider) Episodes(ctx context.Context, a source.Anime, season int) ([]source.Episode, error) {
	d, err := fetchDetail(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	curSeason := resolveSeasonNumber(a.ID, d.Series)
	// If the anime has multiple seasons in series.seasons and the requested season
	// points to a sibling season slug, fetch that season's detail automatically.
	if season > 0 && season != curSeason && d.Series != nil {
		for _, se := range d.Series.Seasons {
			if se.Order == season && se.Slug != "" && !strings.EqualFold(se.Slug, a.ID) {
				if sd, serr := fetchDetail(ctx, se.Slug); serr == nil {
					d = sd
					curSeason = season
				}
				break
			}
		}
	}
	if season <= 0 {
		season = curSeason
	}
	items := append([]episodeItem(nil), d.Episodes...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Number < items[j].Number })
	var eps []source.Episode
	for idx, ep := range items {
		slug := strings.TrimSpace(ep.Slug)
		if slug == "" {
			continue
		}
		num := int(ep.Number)
		if ep.Number != math.Trunc(ep.Number) {
			num = source.ExtraNumber(ep.Number) // 12.5: its own key, not a second 12
		}
		if ep.Number <= 0 {
			num = idx + 1
		}
		title := strings.TrimSpace(ep.Title)
		if title == "" {
			title = source.EpNum(num) + ". Bölüm"
		}
		eps = append(eps, source.Episode{
			ID:     slug,
			Season: season,
			Number: num,
			Title:  title,
		})
	}
	if len(eps) == 0 {
		return nil, source.ErrNotFound
	}
	eps = source.NormalizeEpisodes(&a, eps)
	return eps, nil
}

func (Provider) Variants(_ context.Context, _ source.Anime, ep source.Episode) ([]source.Variant, error) {
	if strings.TrimSpace(ep.ID) == "" {
		return nil, source.ErrNotFound
	}
	return []source.Variant{
		{
			Fansub: "AniHub",
			Host:   "AniHub HLS",
			URL:    ep.ID,
			Kind:   "hls",
			Lang:   "tr",
		},
	}, nil
}

func (Provider) Resolve(ctx context.Context, v source.Variant) (*source.Stream, error) {
	slug := normalizeSlug(v.URL)
	if slug == "" {
		return nil, extract.NewError(extract.KindNotFound, "anihub: bölüm kimliği boş")
	}
	var epEnv envelope[episodeDetailData]
	epURL := apiBase + "/v1/episodes/" + url.PathEscape(slug)
	if err := netx.GetJSON(ctx, epURL, headers, &epEnv); err != nil {
		kind := extract.KindOf(err)
		return nil, &extract.Error{Extractor: "AniHub HLS", Kind: kind, Detail: err.Error(), Err: err}
	}
	pb := epEnv.Data.Playback
	if pb == nil {
		return nil, extract.NewError(extract.KindDeleted, "anihub: oynatma verisi yok")
	}
	if !strings.EqualFold(pb.Status, "ready") {
		return nil, extract.NewError(extract.KindProcessing, "anihub: video henüz hazır değil ("+pb.Status+")")
	}
	if strings.TrimSpace(pb.MasterPlaylistKey) == "" || strings.TrimSpace(pb.MasterPlaylistURL) == "" {
		return nil, extract.NewError(extract.KindDeleted, "anihub: HLS manifest bilgisi eksik")
	}

	var tokEnv envelope[tokenData]
	tokURL := apiBase + "/v1/video-tokens"
	payload := map[string]string{"masterPlaylistKey": pb.MasterPlaylistKey}
	if err := netx.PostJSON(ctx, tokURL, headers, payload, &tokEnv); err != nil {
		kind := extract.KindOf(err)
		return nil, &extract.Error{Extractor: "AniHub HLS", Kind: kind, Detail: err.Error(), Err: err}
	}
	tok := strings.TrimSpace(tokEnv.Data.Token)
	if tok == "" {
		return nil, extract.NewError(extract.KindChanged, "anihub: video belirteci alınamadı")
	}

	sep := "?"
	if strings.Contains(pb.MasterPlaylistURL, "?") {
		sep = "&"
	}
	finalURL := pb.MasterPlaylistURL + sep + "token=" + url.QueryEscape(tok)
	st := &source.Stream{
		Referer:   webBase + "/",
		Qualities: []source.Quality{{Label: "auto", URL: finalURL}},
	}
	for _, sub := range pb.Subtitles {
		if strings.TrimSpace(sub.URL) == "" {
			continue
		}
		lbl := strings.TrimSpace(sub.Label)
		if lbl == "" {
			lbl = strings.TrimSpace(sub.Language)
		}
		if lbl == "" {
			lbl = "Türkçe"
		}
		st.Subtitles = append(st.Subtitles, source.Subtitle{Label: lbl, URL: sub.URL})
	}
	extract.ProbeHLS(ctx, st)
	st.SortQualities()
	return st, nil
}

// OpenURL recognises anihub.com.tr anime URLs.
func (p Provider) OpenURL(ctx context.Context, u *url.URL) (source.Anime, *source.Episode, bool, error) {
	if u == nil || !strings.Contains(strings.ToLower(u.Host), "anihub.com.tr") {
		return source.Anime{}, nil, false, nil
	}
	slug := normalizeSlug(u.Path)
	if slug == "" {
		return source.Anime{}, nil, false, nil
	}
	an, err := p.Details(ctx, source.Anime{Source: "anihub", ID: slug, Title: slug})
	if err != nil {
		return source.Anime{}, nil, true, err
	}
	return an, nil, true, nil
}
