// Package animim implements animim.app: an open JSON API with fansub
// choice, the MAL and AniList ids, and Turkish subtitles as separate .ass
// files next to a direct MP4.
//
// Notes (verified live 2026-11-07):
//   - /api/search?q= → {anime: [...]}; /api/animes/{id} → details with
//     malId, aniListId and the fansub names; every season is its own title
//     (sequelSeasons), so a title has one season.
//   - /api/episodes?animeId= lists episodes; /api/episodes/{id} has
//     fanSubVideos: per fansub, videoUrls (1080p/720p/480p) and assSubtitles.
//   - A video URL is /api/video-proxy/…: 302 to the CDN MP4 (Range, 206).
//     Subtitle URLs are signed (exp), so Resolve always fetches fresh.
//
// Endpoints learnt from Migurdex's Animim plugin (GPL-3.0); this is our own
// code.
package animim

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/extract"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

// base is a var so tests can point the source at recorded answers.
var base = "https://animim.app"

func headers() map[string]string { return map[string]string{"Referer": base + "/"} }

type Provider struct{}

func init() { source.Register(Provider{}) }

func (Provider) Info() source.Info {
	return source.Info{ID: "animim", Name: "Animim", Lang: "tr",
		Note: "Fansub seçimi, ayrı Türkçe altyazı, doğrudan MP4"}
}

type apiAnime struct {
	ID       string            `json:"_id"`
	Title    string            `json:"title"`
	Original source.FlexString `json:"originalTitle"`
	English  source.FlexString `json:"englishTitle"`
	Type     source.FlexString `json:"type"`
	Year     source.FlexInt    `json:"releaseYear"`
	Cover    source.FlexString `json:"cover"`
	Desc     source.FlexString `json:"description"`
	MalID    source.FlexInt    `json:"malId"`
	Episodes source.FlexInt    `json:"episodes"`
	Genres   []string          `json:"genres"`
	FanSubs  []struct {
		ID   string `json:"_id"`
		Name string `json:"name"`
	} `json:"fanSubs"`
}

func (t apiAnime) toAnime() source.Anime {
	a := source.Anime{
		Source:      "animim",
		ID:          t.ID,
		Title:       strings.TrimSpace(t.Title),
		Year:        int(t.Year),
		Kind:        "TV",
		Poster:      string(t.Cover),
		Description: strings.TrimSpace(string(t.Desc)),
		MalID:       int(t.MalID),
		EpCount:     int(t.Episodes),
		Genres:      t.Genres,
	}
	if k := strings.ToLower(string(t.Type)); strings.Contains(k, "movie") || strings.Contains(k, "film") {
		a.Kind = "Film"
	}
	for _, alt := range []string{string(t.English), string(t.Original)} {
		if alt = strings.TrimSpace(alt); alt != "" && !strings.EqualFold(alt, a.Title) {
			if a.AltTitle == "" {
				a.AltTitle = alt
			}
			a.Titles = append(a.Titles, alt)
		}
	}
	a.Normalize()
	return a
}

func (Provider) Probe(ctx context.Context) error {
	return source.ProbeChain(ctx, Provider{}, "frieren")
}

func (Provider) Search(ctx context.Context, q string) ([]source.Anime, error) {
	q = source.Clean(q)
	if q == "" {
		return nil, nil
	}
	var res struct {
		Anime []apiAnime `json:"anime"`
	}
	if err := netx.GetJSON(ctx, base+"/api/search?q="+url.QueryEscape(q), headers(), &res); err != nil {
		return nil, err
	}
	out := make([]source.Anime, 0, len(res.Anime))
	for _, t := range res.Anime {
		if t.ID == "" || t.Title == "" {
			continue
		}
		out = append(out, t.toAnime())
	}
	return out, nil
}

// fansub names by id, learnt from details (or /api/fansubs/{id})
var (
	fanMu sync.Mutex
	fans  = map[string]string{}
)

func (Provider) Details(ctx context.Context, a source.Anime) (source.Anime, error) {
	var t apiAnime
	if err := netx.GetJSON(ctx, base+"/api/animes/"+url.PathEscape(a.ID), headers(), &t); err != nil {
		return a, err
	}
	if t.ID == "" {
		return a, source.ErrNotFound
	}
	fanMu.Lock()
	for _, f := range t.FanSubs {
		if f.ID != "" && f.Name != "" {
			fans[f.ID] = f.Name
		}
	}
	fanMu.Unlock()
	d := t.toAnime()
	// every season is its own title here
	d.Seasons = []source.Season{{Number: 1, Name: "Bölümler", EpisodeCount: d.EpCount}}
	return d, nil
}

type apiEpisode struct {
	ID     string            `json:"_id"`
	Title  source.FlexString `json:"title"`
	Number source.FlexString `json:"number"`
}

func (Provider) Episodes(ctx context.Context, a source.Anime, season int) ([]source.Episode, error) {
	var res []apiEpisode
	if err := netx.GetJSON(ctx, base+"/api/episodes?animeId="+url.QueryEscape(a.ID), headers(), &res); err != nil {
		return nil, err
	}
	var eps []source.Episode
	for _, e := range res {
		n, ok := source.ParseEpisodeNumber(string(e.Number))
		if !ok || e.ID == "" {
			continue
		}
		eps = append(eps, source.Episode{ID: e.ID, Season: 1, Number: n, Title: strings.TrimSpace(string(e.Title))})
	}
	if len(eps) == 0 {
		return nil, source.ErrNotFound
	}
	source.SortEpisodes(eps)
	return source.NormalizeEpisodes(&a, eps), nil
}

type fanVideo struct {
	ID        string `json:"_id"`
	FanSubID  string `json:"fanSubId"`
	Credits   source.FlexString
	VideoURLs []struct {
		P1080      source.FlexString `json:"1080p_url"`
		P720       source.FlexString `json:"720p_url"`
		P480       source.FlexString `json:"480p_url"`
		URL        source.FlexString `json:"url"`
		Identifier source.FlexString `json:"identifier"`
		Type       source.FlexString `json:"type"`
	} `json:"videoUrls"`
	Subs []struct {
		URL      string            `json:"url"`
		Language source.FlexString `json:"language"`
	} `json:"assSubtitles"`
}

func episode(ctx context.Context, id string) ([]fanVideo, error) {
	var res struct {
		Videos []fanVideo `json:"fanSubVideos"`
	}
	if err := netx.GetJSON(ctx, base+"/api/episodes/"+url.PathEscape(id), headers(), &res); err != nil {
		return nil, err
	}
	return res.Videos, nil
}

// fanName is a fansub's name: from details, else asked once.
func fanName(ctx context.Context, id string) string {
	fanMu.Lock()
	n, ok := fans[id]
	fanMu.Unlock()
	if ok {
		return n
	}
	var res struct {
		Name string `json:"name"`
	}
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := netx.GetJSON(c, base+"/api/fansubs/"+url.PathEscape(id), headers(), &res); err == nil && res.Name != "" {
		n = res.Name
	} else {
		n = "Bilinmeyen"
	}
	fanMu.Lock()
	fans[id] = n
	fanMu.Unlock()
	return n
}

// Variants: one per fansub upload (and per CDN when a fansub has two).
func (Provider) Variants(ctx context.Context, a source.Anime, ep source.Episode) ([]source.Variant, error) {
	vids, err := episode(ctx, ep.ID)
	if err != nil {
		return nil, err
	}
	var out []source.Variant
	for _, f := range vids {
		name := fanName(ctx, f.FanSubID)
		for i, v := range f.VideoURLs {
			if v.P1080 == "" && v.P720 == "" && v.P480 == "" && v.URL == "" {
				continue
			}
			host := "Animim"
			if id := strings.TrimSpace(string(v.Identifier)); id != "" {
				host += " · " + strings.ReplaceAll(strings.TrimSuffix(id, "_cdn"), "_", " ")
			}
			out = append(out, source.Variant{Fansub: name, Host: host,
				URL:  fmt.Sprintf("%s|%s|%d", ep.ID, f.ID, i),
				Kind: "mp4", Lang: "tr", Dub: source.IsDubName(name)})
		}
	}
	if len(out) == 0 {
		return nil, source.ErrNotFound
	}
	return out, nil
}

func (Provider) Resolve(ctx context.Context, v source.Variant) (*source.Stream, error) {
	parts := strings.Split(v.URL, "|")
	if len(parts) != 3 {
		return nil, extract.NewError(extract.KindNotFound, "animim: bölüm kimliği yok")
	}
	vids, err := episode(ctx, parts[0]) // subtitle links are signed: fetch fresh
	if err != nil {
		return nil, &extract.Error{Extractor: "Animim", Kind: extract.KindOf(err), Detail: err.Error(), Err: err}
	}
	var idx int
	fmt.Sscanf(parts[2], "%d", &idx)
	for _, f := range vids {
		if f.ID != parts[1] || idx >= len(f.VideoURLs) {
			continue
		}
		vu := f.VideoURLs[idx]
		st := &source.Stream{Referer: base + "/"}
		for _, q := range []struct {
			u string
			h int
		}{{string(vu.P1080), 1080}, {string(vu.P720), 720}, {string(vu.P480), 480}} {
			if strings.HasPrefix(q.u, "http") {
				st.Qualities = append(st.Qualities, source.Quality{Label: fmt.Sprintf("%dp", q.h), URL: q.u, Height: q.h})
			}
		}
		if u := string(vu.URL); strings.HasPrefix(u, "http") {
			st.Qualities = append(st.Qualities, source.Quality{Label: "auto", URL: u})
		}
		for _, s := range f.Subs {
			if !strings.HasPrefix(s.URL, "http") {
				continue
			}
			lbl := "Türkçe"
			if l := strings.ToLower(string(s.Language)); l != "" && l != "tr" {
				lbl = strings.ToUpper(l)
			}
			st.Subtitles = append(st.Subtitles, source.Subtitle{Label: lbl, URL: s.URL})
		}
		if len(st.Qualities) == 0 {
			return nil, extract.NewError(extract.KindProcessing, "animim: video henüz eklenmemiş")
		}
		extract.ProbeHLS(ctx, st)
		if dead, kind := extract.ProbeMP4s(ctx, st); dead {
			return nil, &extract.Error{Extractor: "Animim", Kind: kind, Detail: "animim: hiçbir kalite açılmıyor"}
		}
		return st, nil
	}
	return nil, extract.NewError(extract.KindDeleted, "animim: bu yükleme kaldırılmış")
}
