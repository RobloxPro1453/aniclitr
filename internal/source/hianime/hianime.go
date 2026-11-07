// Package hianime is the optional English source (hianime.at).
//
// Recipe follows ani-cli (2026): server hashes are base64 embed URLs; the
// ZokoAnime embed ships its config as base64(json XOR "otaku-embed-v1").
package hianime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"aniclitr/internal/extract"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

// base is a var so tests can point the source at recorded pages.
var base = "https://hianime.at"

type Provider struct{}

func init() { source.Register(Provider{}) }

func (Provider) Info() source.Info {
	return source.Info{ID: "hianime", Name: "HiAnime", Lang: "en",
		Note: "İngilizce altyazı ve dublaj"}
}

var (
	reCard   = regexp.MustCompile(`(?s)<div class="flw-item`)
	rePoster = regexp.MustCompile(`<img[^>]+(?:data-src|src)="([^"]+)"[^>]*class="film-poster-img"`)
	reName   = regexp.MustCompile(`(?s)<h3 class="film-name">\s*<a href="[^"]*/([^"/?]+)"\s*title="([^"]*)"(?:[^>]*data-jname="([^"]*)")?`)
	reDesc   = regexp.MustCompile(`(?s)<div class="description">\s*(.*?)\s*</div>`)
	reType   = regexp.MustCompile(`<span class="fdi-item">([^<]+)</span>`)
	reSubs   = regexp.MustCompile(`tick-sub"><i[^>]*></i>(\d+)`)
	// ep-item may be followed by more classes ("ssl-item-filler"): fillers
	// were silently dropped while the class had to end with ep-item
	reEp     = regexp.MustCompile(`(?s)<a title="([^"]*)"\s+class="[^"]*ep-item[^"]*"\s+data-number="([^"]+)"\s+data-id="(\d+)"`)
	reServer = regexp.MustCompile(`(?s)server-item" data-type="(\w+)"\s+data-server-name="([^"]+)"\s+data-hash="([^"]+)"`)
	reBlob   = regexp.MustCompile(`window\.__P="([^"]*)"`)
	reMal    = regexp.MustCompile(`/mal/(\d+)/`)
)

// Probe checks the search cards and the episode-list API in one go.
func (p Provider) Probe(ctx context.Context) error {
	return source.ProbeChain(ctx, p, "one piece")
}

func (Provider) Search(ctx context.Context, q string) ([]source.Anime, error) {
	q = source.Clean(q)
	if q == "" {
		return nil, nil
	}
	body, err := netx.Get(ctx, base+"/search?keyword="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	page := string(body)
	if i := strings.Index(page, `id="main-sidebar"`); i > 0 {
		page = page[:i]
	}
	if strings.Contains(page, "<title>Just a moment") {
		return nil, extract.NewError(extract.KindBlocked, "hianime: cloudflare engeli")
	}
	idx := reCard.FindAllStringIndex(page, -1)
	var out []source.Anime
	for i, loc := range idx {
		end := len(page)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		card := page[loc[0]:end]
		m := reName.FindStringSubmatch(card)
		if m == nil {
			continue
		}
		a := source.Anime{Source: "hianime", ID: m[1], Title: html.UnescapeString(m[2]), AltTitle: html.UnescapeString(m[3]), Kind: "TV"}
		if p := rePoster.FindStringSubmatch(card); p != nil {
			a.Poster = p[1]
		}
		if d := reDesc.FindStringSubmatch(card); d != nil {
			a.Description = html.UnescapeString(strings.TrimSpace(stripTags(d[1])))
		}
		if t := reType.FindStringSubmatch(card); t != nil {
			a.Kind = strings.TrimSpace(strings.SplitN(t[1], "(", 2)[0])
			if strings.EqualFold(a.Kind, "movie") {
				a.Kind = "Film"
			}
		}
		if s := reSubs.FindStringSubmatch(card); s != nil {
			a.EpCount, _ = strconv.Atoi(s[1])
		}
		if a.AltTitle != "" {
			a.Titles = []string{a.AltTitle}
		}
		a.Normalize()
		out = append(out, a)
	}
	return out, nil
}

var reTags = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string { return reTags.ReplaceAllString(s, "") }

func (Provider) Details(ctx context.Context, a source.Anime) (source.Anime, error) {
	a.Seasons = []source.Season{{Number: 1, Name: "Bölümler", EpisodeCount: a.EpCount}}
	return a, nil
}

func numericID(slug string) string {
	if i := strings.LastIndex(slug, "-"); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

func (Provider) Episodes(ctx context.Context, a source.Anime, season int) ([]source.Episode, error) {
	var res struct {
		HTML string `json:"html"`
	}
	if err := netx.GetJSON(ctx, base+"/api/theme/episode/list/"+numericID(a.ID), map[string]string{"Referer": base + "/"}, &res); err != nil {
		return nil, err
	}
	var eps []source.Episode
	for _, m := range reEp.FindAllStringSubmatch(res.HTML, -1) {
		n, ok := source.ParseEpisodeNumber(m[2]) // "12.5" used to become episode 0
		if !ok {
			continue
		}
		eps = append(eps, source.Episode{ID: m[3], Season: 1, Number: n, Title: html.UnescapeString(m[1])})
	}
	source.SortEpisodes(eps)
	if len(eps) == 0 {
		return nil, source.ErrNotFound
	}
	return eps, nil
}

func (Provider) Variants(ctx context.Context, a source.Anime, ep source.Episode) ([]source.Variant, error) {
	var res struct {
		HTML string `json:"html"`
	}
	if err := netx.GetJSON(ctx, base+"/api/theme/episode/servers?episodeId="+ep.ID, map[string]string{"Referer": base + "/"}, &res); err != nil {
		return nil, err
	}
	html := strings.ReplaceAll(res.HTML, `\"`, `"`)
	var out []source.Variant
	for _, m := range reServer.FindAllStringSubmatch(html, -1) {
		embed, err := base64.StdEncoding.DecodeString(m[3])
		if err != nil {
			continue
		}
		track, lang, dub := "Altyazılı (EN)", "en", false
		if strings.EqualFold(m[1], "dub") {
			track, dub = "Dublaj (EN)", true
		} else if strings.EqualFold(m[1], "raw") {
			track, lang = "Ham (JP)", ""
		}
		out = append(out, source.Variant{Fansub: track, Host: m[2], URL: string(embed), Kind: "embed", Lang: lang, Dub: dub})
	}
	// ZokoAnime is the only player we decode natively; keep it on top.
	for i := range out {
		if strings.EqualFold(out[i].Host, "ZokoAnime") && i > 0 {
			out[0], out[i] = out[i], out[0]
		}
	}
	if len(out) == 0 {
		return nil, source.ErrNotFound
	}
	return out, nil
}

var xorKey = []byte("otaku-embed-v1")

func (Provider) Resolve(ctx context.Context, v source.Variant) (*source.Stream, error) {
	if !strings.Contains(strings.ToLower(v.URL), "zokoanime") {
		return extract.YtDlp(ctx, v.URL)
	}
	u, err := url.Parse(v.URL)
	if err != nil {
		return nil, err
	}
	origin := u.Scheme + "://" + u.Host + "/"
	body, err := netx.Get(ctx, v.URL, map[string]string{"Referer": base + "/"})
	if err != nil {
		return nil, err
	}
	m := reBlob.FindSubmatch(body)
	if m == nil {
		if k := extract.Classify(200, body); k != extract.KindUnknown {
			return nil, extract.NewError(k, "zoko: "+k.Message())
		}
		return nil, extract.NewError(extract.KindChanged, "zoko: yapılandırma bulunamadı (site değişmiş olabilir)")
	}
	raw, err := base64.StdEncoding.DecodeString(string(m[1]))
	if err != nil {
		return nil, fmt.Errorf("zoko: %w", err)
	}
	for i := range raw {
		raw[i] ^= xorKey[i%len(xorKey)]
	}
	var cfg struct {
		Src       string `json:"src"`
		Subtitles []struct {
			Label   string `json:"label"`
			Src     string `json:"src"`
			Default bool   `json:"default"`
		} `json:"subtitles"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("zoko: %w", err)
	}
	if cfg.Src == "" {
		return nil, errors.New("zoko: kaynak yok")
	}
	st := &source.Stream{Referer: origin, Qualities: []source.Quality{{Label: "auto", URL: cfg.Src}}}
	for _, s := range cfg.Subtitles {
		sub := source.Subtitle{Label: s.Label, URL: s.Src}
		if s.Default {
			st.Subtitles = append([]source.Subtitle{sub}, st.Subtitles...)
		} else {
			st.Subtitles = append(st.Subtitles, sub)
		}
	}
	if mm := reMal.FindStringSubmatch(v.URL); mm != nil {
		st.MalID, _ = strconv.Atoi(mm[1])
	}
	extract.ProbeHLS(ctx, st)
	return st, nil
}

// OpenURL recognises hianime watch/series URLs.
func (p Provider) OpenURL(ctx context.Context, u *url.URL) (source.Anime, *source.Episode, bool, error) {
	if u == nil || !strings.Contains(strings.ToLower(u.Host), "hianime") {
		return source.Anime{}, nil, false, nil
	}
	slug := strings.Trim(u.Path, "/")
	slug = strings.TrimPrefix(slug, "watch/")
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		slug = slug[:i]
	}
	if slug == "" {
		return source.Anime{}, nil, false, nil
	}
	an, err := p.Details(ctx, source.Anime{Source: "hianime", ID: slug, Title: slug})
	if err != nil {
		return source.Anime{}, nil, true, err
	}
	return an, nil, true, nil
}
