// Package animedepo is the legacy source: the frozen TürkAnime metadata archive
// (TürkAnime shut down on 2026-09-19). Data never updates and third-party links
// rot over time, so it is only queried in archive mode.
package animedepo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/extract"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

const base = "https://gitlab.com/AnimeDepo/animedepo/-/raw/master"

type Provider struct{}

func init() { source.Register(Provider{}) }

func (Provider) Info() source.Info {
	return source.Info{ID: "animedepo", Name: "TürkAnime Arşivi", Lang: "tr", Legacy: true,
		Note: "Eski seriler; veriler donmuş, linklerin çoğu ölü"}
}

type entry struct {
	Slug  string
	Title string
	norm  string
}

var (
	indexOnce sync.Mutex
	index     []entry
)

func loadIndex(ctx context.Context) ([]entry, error) {
	indexOnce.Lock()
	defer indexOnce.Unlock()
	if index != nil {
		return index, nil
	}
	cache := filepath.Join(config.CacheDir(), "animedepo-dizin.json")
	var body []byte
	if st, err := os.Stat(cache); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		body, _ = os.ReadFile(cache)
	}
	if body == nil {
		b, err := netx.Get(ctx, base+"/dizin.json", nil)
		if err != nil {
			if old, rerr := os.ReadFile(cache); rerr == nil {
				body = old // stale is better than nothing
			} else {
				return nil, err
			}
		} else {
			body = b
			_ = config.WriteAtomic(cache, b)
		}
	}
	var d struct {
		Index map[string]map[string]struct {
			Title string `json:"title"`
		} `json:"index"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	for _, group := range d.Index {
		for slug, a := range group {
			index = append(index, entry{Slug: slug, Title: a.Title, norm: Fold(a.Title + " " + slug)})
		}
	}
	return index, nil
}

var folder = strings.NewReplacer("ı", "i", "İ", "i", "ş", "s", "Ş", "s", "ğ", "g", "Ğ", "g", "ü", "u", "Ü", "u", "ö", "o", "Ö", "o", "ç", "c", "Ç", "c", "-", " ", ":", " ", "'", "")

// Fold lowercases and strips Turkish diacritics for fuzzy matching.
func Fold(s string) string { return strings.ToLower(folder.Replace(s)) }

func (Provider) Search(ctx context.Context, q string) ([]source.Anime, error) {
	q = source.Clean(q)
	if q == "" {
		return nil, nil
	}
	idx, err := loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(Fold(q))
	type hit struct {
		e     entry
		score int
	}
	var hits []hit
	for _, e := range idx {
		score := 0
		for _, w := range words {
			if !strings.Contains(e.norm, w) {
				score = -1
				break
			}
			score++
		}
		if score <= 0 {
			continue
		}
		if strings.HasPrefix(e.norm, strings.Join(words, " ")) {
			score += 10
		}
		hits = append(hits, hit{e, score*100 - len(e.Title)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var out []source.Anime
	for i, h := range hits {
		if i >= 30 {
			break
		}
		out = append(out, source.Anime{Source: "animedepo", ID: h.e.Slug, Title: h.e.Title, Kind: "TV"})
	}
	return out, nil
}

func (Provider) Details(ctx context.Context, a source.Anime) (source.Anime, error) {
	var info map[string]any
	if err := netx.GetJSON(ctx, base+"/animeler/"+a.ID+"/info.json", nil, &info); err == nil {
		if s, ok := info["Özet"].(string); ok {
			a.Description = strings.TrimSpace(s)
		}
		if k, ok := info["Kategori"].(string); ok && k != "" {
			a.Kind = k
		}
		if g, ok := info["Anime Türü"].([]any); ok {
			for _, x := range g {
				if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
					a.Genres = append(a.Genres, strings.TrimSpace(s))
				}
			}
		}
		if s, ok := info["Başlama Tarihi"].(string); ok {
			if m := regexp.MustCompile(`\d{4}`).FindString(s); m != "" {
				a.Year, _ = strconv.Atoi(m)
			}
		}
	}
	a.Seasons = []source.Season{{Number: 1, Name: "Bölümler"}}
	return a, nil
}

var reNum = regexp.MustCompile(`(\d+)\.\s*Bölüm`)

func (Provider) Episodes(ctx context.Context, a source.Anime, season int) ([]source.Episode, error) {
	var list [][2]string
	if err := netx.GetJSON(ctx, base+"/animeler/"+a.ID+"/bolumler.json", nil, &list); err != nil {
		return nil, err
	}
	eps := make([]source.Episode, 0, len(list))
	for i, e := range list {
		n := i + 1
		if m := reNum.FindStringSubmatch(e[1]); m != nil {
			n, _ = strconv.Atoi(m[1])
		}
		title := strings.TrimSpace(strings.TrimPrefix(e[1], a.Title))
		eps = append(eps, source.Episode{ID: e[0], Season: 1, Number: n, Title: title})
	}
	return eps, nil
}

func (Provider) Variants(ctx context.Context, a source.Anime, ep source.Episode) ([]source.Variant, error) {
	var items []struct {
		Player string `json:"player"`
		Fansub string `json:"fansub"`
		URL    string `json:"url"`
		Alive  *bool  `json:"alive"`
	}
	if err := netx.GetJSON(ctx, base+"/animeler/"+a.ID+"/"+ep.ID+".json", nil, &items); err != nil {
		return nil, err
	}
	var out []source.Variant
	for _, it := range items {
		if it.URL == "" || strings.HasPrefix(it.Player, "DEAD_") || (it.Alive != nil && !*it.Alive) {
			continue
		}
		out = append(out, source.Variant{Fansub: it.Fansub, Host: prettyHost(it.Player), URL: it.URL})
	}
	if len(out) == 0 {
		return nil, source.ErrNotFound
	}
	return out, nil
}

func prettyHost(p string) string {
	p = strings.ReplaceAll(p, "(BETA)", "")
	if len(p) <= 3 {
		return p
	}
	return strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
}

func (Provider) Resolve(ctx context.Context, v source.Variant) (*source.Stream, error) {
	return extract.Resolve(ctx, v.URL)
}
