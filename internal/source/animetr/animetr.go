// Package animetr implements AnimeTR (animetr.co), a Turkish source with
// many fansubs and anime + donghua.
//
// Notes from reverse engineering (verified 2026-11-04):
//   - /search-series?q= returns JSON; every season is its own series (so the
//     MAL id of a series is exact, unlike AnimeciX's bundled seasons).
//   - /seri/{slug} lists every episode in one page (One Piece: 1100+ links)
//     and carries the MAL id in its ld+json.
//   - /izle/{slug}/bolum-N embeds the fansub × player matrix as JSON
//     (<script id="epspage-data">).
//   - Players: OK.ru and Google Drive resolve through yt-dlp; Vidmoly,
//     Vidoza and HDVid links are often dead. The "VIP" player (azelvid /
//     asyaanimeleri.pw) hides its sources behind an anti-debug trap and an
//     encrypted key, so it is deliberately left out.
package animetr

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"aniclitr/internal/extract"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

const base = "https://animetr.co"

var headers = map[string]string{"Referer": base + "/"}

type Provider struct{}

func init() { source.Register(Provider{}) }

func (Provider) Info() source.Info {
	return source.Info{ID: "animetr", Name: "AnimeTR", Lang: "tr",
		Note: "Anime ve donghua, çok fansub; her sezon ayrı seri"}
}

type apiSeries struct {
	ID       source.FlexInt    `json:"id"`
	Title    string            `json:"title"`
	EnTitle  source.FlexString `json:"en_title"`
	Poster   source.FlexString `json:"poster"`
	URLTitle string            `json:"url_title"`
	Types    source.FlexString `json:"types"`
	Year     source.FlexInt    `json:"year"`
	Hidden   bool              `json:"is_hidden"`
}

func kindOf(t string) string {
	if strings.EqualFold(strings.TrimSpace(t), "movie") {
		return "Film"
	}
	if t = strings.TrimSpace(t); t != "" {
		return t
	}
	return "TV"
}

func (Provider) Search(ctx context.Context, q string) ([]source.Anime, error) {
	q = source.Clean(q)
	if q == "" {
		return nil, nil
	}
	var res []apiSeries
	if err := netx.GetJSON(ctx, base+"/search-series?q="+url.QueryEscape(q), headers, &res); err != nil {
		return nil, err
	}
	out := make([]source.Anime, 0, len(res))
	for _, s := range res {
		if s.URLTitle == "" || s.Title == "" || s.Hidden {
			continue
		}
		a := source.Anime{
			Source: "animetr",
			ID:     s.URLTitle,
			Title:  html.UnescapeString(strings.TrimSpace(s.Title)),
			Year:   int(s.Year),
			Kind:   kindOf(string(s.Types)),
			Poster: string(s.Poster),
		}
		if en := html.UnescapeString(strings.TrimSpace(string(s.EnTitle))); en != "" && !strings.EqualFold(en, a.Title) {
			a.AltTitle = en
			a.Titles = []string{en}
		}
		a.Normalize()
		out = append(out, a)
	}
	return out, nil
}

var (
	reH1     = regexp.MustCompile(`(?s)<h1[^>]*>\s*(.*?)\s*</h1>`)
	reDesc   = regexp.MustCompile(`<meta name="description" content="([^"]*)"`)
	rePoster = regexp.MustCompile(`<meta property="og:image" content="([^"]+)"`)
	reMal    = regexp.MustCompile(`myanimelist\.net/anime/(\d+)`)
	reGenre  = regexp.MustCompile(`href="/seriler\?genres=[^"]*"[^>]*>([^<]+)<`)
	reType   = regexp.MustCompile(`(?s)Tip:\s*</span>.*?<span[^>]*>\s*([^<]+?)\s*</span>`)
	reYear   = regexp.MustCompile(`(?s)Sezon:\s*</span>.*?(\d{4})\s*</span>`)
	reEp     = regexp.MustCompile(`(?s)<a href="(/izle/[^"]+)" class="ep-item__link[^"]*"[^>]*>.*?ep-item__num">([^<]*)<.*?ep-item__title">([^<]*)<`)
	reEpNum  = regexp.MustCompile(`/bolum-(\d+)`)
	reTags   = regexp.MustCompile(`<[^>]+>`)
	reEpData = regexp.MustCompile(`(?s)<script type="application/json" id="epspage-data">(.*?)</script>`)
)

func (Provider) page(ctx context.Context, slug string) (string, error) {
	b, err := netx.Get(ctx, base+"/seri/"+url.PathEscape(slug), headers)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p Provider) Details(ctx context.Context, a source.Anime) (source.Anime, error) {
	pg, err := p.page(ctx, a.ID)
	if err != nil {
		return a, err
	}
	if m := reH1.FindStringSubmatch(pg); m != nil {
		if t := html.UnescapeString(strings.TrimSpace(reTags.ReplaceAllString(m[1], ""))); t != "" {
			a.Title = t
		}
	}
	if m := reDesc.FindStringSubmatch(pg); m != nil {
		a.Description = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := rePoster.FindStringSubmatch(pg); m != nil && a.Poster == "" {
		a.Poster = m[1]
	}
	if m := reMal.FindStringSubmatch(pg); m != nil {
		a.MalID, _ = strconv.Atoi(m[1])
	}
	if m := reType.FindStringSubmatch(pg); m != nil {
		// "Anime | TV", "Donghua | ONA", "Anime | Movie"
		parts := strings.Split(m[1], "|")
		a.Kind = kindOf(parts[len(parts)-1])
	}
	if m := reYear.FindStringSubmatch(pg); m != nil && a.Year == 0 {
		a.Year, _ = strconv.Atoi(m[1])
	}
	a.Genres = nil
	for _, g := range reGenre.FindAllStringSubmatch(pg, -1) {
		if name := strings.TrimSpace(html.UnescapeString(g[1])); name != "" {
			a.Genres = append(a.Genres, name)
		}
	}
	eps := parseEpisodes(pg, a.ID)
	eps = source.NormalizeEpisodes(&a, eps)
	a.EpCount = len(eps)
	// each season is its own series here; one "season" per title
	a.Seasons = []source.Season{{Number: 1, Name: "Bölümler", EpisodeCount: a.EpCount}}
	return a, nil
}

// parseEpisodes reads the episode list of a series page (links of other
// series on the page, like the next season, are skipped).
func parseEpisodes(pg, slug string) []source.Episode {
	prefix := "/izle/" + slug + "/"
	seen := map[string]bool{}
	var eps []source.Episode
	for _, m := range reEp.FindAllStringSubmatch(pg, -1) {
		href := m[1]
		if !strings.HasPrefix(href, prefix) || seen[href] {
			continue
		}
		seen[href] = true
		n, err := strconv.Atoi(strings.TrimSpace(m[2]))
		if err != nil {
			if mm := reEpNum.FindStringSubmatch(href); mm != nil {
				n, _ = strconv.Atoi(mm[1])
			}
		}
		if n <= 0 {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(m[3]))
		eps = append(eps, source.Episode{ID: href, Season: 1, Number: n, Title: title})
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Number < eps[j].Number })
	return eps
}

func (p Provider) Episodes(ctx context.Context, a source.Anime, season int) ([]source.Episode, error) {
	pg, err := p.page(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	eps := parseEpisodes(pg, a.ID)
	if len(eps) == 0 {
		return nil, source.ErrNotFound
	}
	return eps, nil
}

type epData struct {
	Fansubs []struct {
		Name    string `json:"name"`
		Players []struct {
			Label    string `json:"label"`
			Embed    string `json:"embed_url"`
			Original string `json:"original_url"`
			Provider string `json:"provider"`
		} `json:"players"`
	} `json:"fansubs"`
}

func (Provider) Variants(ctx context.Context, a source.Anime, ep source.Episode) ([]source.Variant, error) {
	path := ep.ID
	if !strings.HasPrefix(path, "/izle/") {
		path = fmt.Sprintf("/izle/%s/bolum-%d", a.ID, ep.Number)
	}
	b, err := netx.Get(ctx, base+path, headers)
	if err != nil {
		return nil, err
	}
	return parseVariants(b)
}

// parseVariants reads the fansub × player matrix of an episode page.
func parseVariants(b []byte) ([]source.Variant, error) {
	m := reEpData.FindSubmatch(b)
	if m == nil {
		if k := extract.Classify(200, b); k != extract.KindUnknown {
			return nil, extract.NewError(k, "animetr: "+k.Message())
		}
		return nil, extract.NewError(extract.KindChanged, "animetr: oynatıcı listesi yok (sayfa değişmiş olabilir)")
	}
	var d epData
	if err := json.Unmarshal(m[1], &d); err != nil {
		return nil, fmt.Errorf("animetr: oynatıcı listesi okunamadı: %w", err)
	}
	var out []source.Variant
	for _, f := range d.Fansubs {
		fansub := strings.TrimSpace(html.UnescapeString(f.Name))
		if fansub == "" {
			fansub = "Bilinmeyen"
		}
		count := map[string]int{}
		for _, pl := range f.Players {
			u := pl.Embed
			if u == "" {
				u = pl.Original
			}
			if u == "" || skipped(pl.Provider, u) {
				continue
			}
			host := strings.TrimSpace(pl.Provider)
			if host == "" {
				host = hostOf(u)
			}
			// a fansub can upload the same host twice (two copies)
			if count[host]++; count[host] > 1 {
				host += " " + strconv.Itoa(count[host])
			}
			out = append(out, source.Variant{Fansub: fansub, Host: host, URL: u})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return hostRank(out[i].URL) < hostRank(out[j].URL) })
	if len(out) == 0 {
		return nil, source.ErrNotFound
	}
	return out, nil
}

// skipped leaves out players we do not open: the VIP player hides its
// sources behind anti-debugging and an encrypted key.
func skipped(provider, u string) bool {
	h := strings.ToLower(hostOf(u))
	return strings.Contains(strings.ToLower(provider), "vip") ||
		strings.Contains(h, "azelvid") || strings.Contains(h, "asyaanimeleri")
}

func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return strings.TrimPrefix(p.Host, "www.")
	}
	return u
}

// hostRank: OK.ru gives HLS up to 1080p and resolves fast; Drive is good
// but rate-limited; the rest are often dead links.
func hostRank(u string) int {
	h := strings.ToLower(hostOf(u))
	switch {
	case strings.Contains(h, "ok.ru"):
		return 0
	case strings.Contains(h, "drive.google"):
		return 1
	case strings.Contains(h, "vidmoly"):
		return 2
	}
	return 3
}

func (Provider) Resolve(ctx context.Context, v source.Variant) (*source.Stream, error) {
	return extract.Resolve(ctx, v.URL)
}

var (
	reURLSeri = regexp.MustCompile(`(?i)^/seri/([^/?#]+)`)
	reURLIzle = regexp.MustCompile(`(?i)^/izle/([^/?#]+)/bolum-(\d+)`)
)

// OpenURL recognises animetr.co series (/seri/slug) and episode (/izle/slug/bolum-N) URLs.
func (p Provider) OpenURL(ctx context.Context, u *url.URL) (source.Anime, *source.Episode, bool, error) {
	if u == nil || !strings.Contains(strings.ToLower(u.Host), "animetr") {
		return source.Anime{}, nil, false, nil
	}
	if m := reURLIzle.FindStringSubmatch(u.Path); m != nil {
		slug := m[1]
		num, _ := strconv.Atoi(m[2])
		an, err := p.Details(ctx, source.Anime{Source: "animetr", ID: slug, Title: slug})
		if err != nil {
			return source.Anime{}, nil, true, err
		}
		if num > 0 {
			ep := source.Episode{ID: fmt.Sprintf("/izle/%s/bolum-%d", slug, num), Season: 1, Number: num}
			return an, &ep, true, nil
		}
		return an, nil, true, nil
	}
	if m := reURLSeri.FindStringSubmatch(u.Path); m != nil {
		slug := m[1]
		an, err := p.Details(ctx, source.Anime{Source: "animetr", ID: slug, Title: slug})
		if err != nil {
			return source.Anime{}, nil, true, err
		}
		return an, nil, true, nil
	}
	return source.Anime{}, nil, false, nil
}
