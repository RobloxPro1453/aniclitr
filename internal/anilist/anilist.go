// Package anilist reads public anime metadata from AniList's GraphQL API:
// the weekly airing schedule, seasons, recommendations, trailers and MAL id
// mapping. No login. (Jikan, the unofficial MAL API, is unreachable from
// some networks; AniList carries the MAL id of every title, so the rest of
// the app can stay MAL-centric.)
package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/netx"
)

const endpoint = "https://graphql.anilist.co"

// Media is one anime.
type Media struct {
	ID       int    `json:"id"`
	IDMal    int    `json:"idMal"`
	Title    Titles `json:"title"`
	Synonyms []string
	Cover    struct {
		XL    string `json:"extraLarge"`
		Large string `json:"large"`
		Color string `json:"color"`
	} `json:"coverImage"`
	Banner      string   `json:"bannerImage"`
	Format      string   `json:"format"`
	Episodes    int      `json:"episodes"`
	Status      string   `json:"status"`
	Score       int      `json:"averageScore"`
	Popularity  int      `json:"popularity"`
	Genres      []string `json:"genres"`
	Season      string   `json:"season"`
	SeasonYear  int      `json:"seasonYear"`
	Description string   `json:"description"`
	IsAdult     bool     `json:"isAdult"`
	Country     string   `json:"countryOfOrigin"`
	Studios     struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"studios"`
	Next *struct {
		Episode  int   `json:"episode"`
		AiringAt int64 `json:"airingAt"`
	} `json:"nextAiringEpisode"`
	Trailer *struct {
		ID   string `json:"id"`
		Site string `json:"site"`
	} `json:"trailer"`
}

// Titles holds the title variants.
type Titles struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

// Name is the display title (romaji, like the Turkish sites use).
func (m Media) Name() string {
	if m.Title.Romaji != "" {
		return m.Title.Romaji
	}
	return m.Title.English
}

// Poster is the best cover URL.
func (m Media) Poster() string {
	if m.Cover.XL != "" {
		return m.Cover.XL
	}
	return m.Cover.Large
}

// Studio is the main studio name.
func (m Media) Studio() string {
	if len(m.Studios.Nodes) > 0 {
		return m.Studios.Nodes[0].Name
	}
	return ""
}

// TrailerURL returns a playable URL (YouTube/Dailymotion) or "".
func (m Media) TrailerURL() string {
	if m.Trailer == nil || m.Trailer.ID == "" {
		return ""
	}
	switch m.Trailer.Site {
	case "youtube":
		return "https://www.youtube.com/watch?v=" + m.Trailer.ID
	case "dailymotion":
		return "https://www.dailymotion.com/video/" + m.Trailer.ID
	}
	return ""
}

// Titles lists every name worth searching the sources with.
func (m Media) AllTitles() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range append([]string{m.Title.Romaji, m.Title.English}, m.Synonyms...) {
		k := strings.ToLower(strings.TrimSpace(t))
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, t)
		}
	}
	return out
}

// Airing is one scheduled episode.
type Airing struct {
	At      time.Time
	Episode int
	Media   Media
}

const mediaFields = `id idMal title{romaji english native} synonyms coverImage{extraLarge large color} bannerImage
format episodes status averageScore popularity genres season seasonYear isAdult countryOfOrigin
studios(isMain:true){nodes{name}} nextAiringEpisode{episode airingAt}`

// ---- client (rate limited, cached) ----

var (
	mu       sync.Mutex
	lastCall time.Time
	mem      = map[string]memEntry{}
)

type memEntry struct {
	at   time.Time
	data json.RawMessage
}

// query runs a GraphQL query, cached for ttl (memory + disk).
func query(ctx context.Context, q string, vars map[string]any, ttl time.Duration, out any) error {
	vb, _ := json.Marshal(vars)
	key := fmt.Sprintf("%x", hash(q+string(vb)))
	mu.Lock()
	if e, ok := mem[key]; ok && time.Since(e.at) < ttl {
		mu.Unlock()
		return json.Unmarshal(e.data, out)
	}
	mu.Unlock()
	path := filepath.Join(config.CacheDir(), "anilist", key+".json")
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < ttl {
		if b, err := os.ReadFile(path); err == nil {
			mu.Lock()
			mem[key] = memEntry{at: st.ModTime(), data: b}
			mu.Unlock()
			return json.Unmarshal(b, out)
		}
	}
	// AniList allows ~30-90 requests a minute: keep a polite gap
	mu.Lock()
	if wait := 350*time.Millisecond - time.Since(lastCall); wait > 0 {
		mu.Unlock()
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
		mu.Lock()
	}
	lastCall = time.Now()
	mu.Unlock()

	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", netx.UserAgent)
	resp, err := netx.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("anilist: HTTP %d: %w", resp.StatusCode, err)
	}
	if len(env.Errors) > 0 {
		return errors.New("anilist: " + env.Errors[0].Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anilist: HTTP %d", resp.StatusCode)
	}
	mu.Lock()
	mem[key] = memEntry{at: time.Now(), data: env.Data}
	mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = config.WriteAtomic(path, env.Data)
	return json.Unmarshal(env.Data, out)
}

func hash(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func keep(m Media) bool { return !m.IsAdult }

// Schedule returns episodes airing between from and to (local time), soonest first.
func Schedule(ctx context.Context, from, to time.Time) ([]Airing, error) {
	q := `query($from:Int,$to:Int,$page:Int){Page(page:$page,perPage:50){pageInfo{hasNextPage}
airingSchedules(airingAt_greater:$from,airingAt_lesser:$to,sort:TIME){airingAt episode media{` + mediaFields + `}}}}`
	var out []Airing
	for page := 1; page <= 6; page++ {
		var res struct {
			Page struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				Items []struct {
					AiringAt int64 `json:"airingAt"`
					Episode  int   `json:"episode"`
					Media    Media `json:"media"`
				} `json:"airingSchedules"`
			} `json:"Page"`
		}
		// cache per half hour bucket so repeated opens are instant
		if err := query(ctx, q, map[string]any{"from": from.Unix(), "to": to.Unix(), "page": page}, 30*time.Minute, &res); err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		for _, it := range res.Page.Items {
			if keep(it.Media) {
				out = append(out, Airing{At: time.Unix(it.AiringAt, 0), Episode: it.Episode, Media: it.Media})
			}
		}
		if !res.Page.PageInfo.HasNextPage {
			break
		}
	}
	return out, nil
}

// SeasonOf maps a date to AniList's season name.
func SeasonOf(t time.Time) (string, int) {
	switch t.Month() {
	case time.January, time.February, time.March:
		return "WINTER", t.Year()
	case time.April, time.May, time.June:
		return "SPRING", t.Year()
	case time.July, time.August, time.September:
		return "SUMMER", t.Year()
	}
	return "FALL", t.Year()
}

// SeasonTR is the Turkish season label.
func SeasonTR(s string) string {
	return map[string]string{"WINTER": "Kış", "SPRING": "İlkbahar", "SUMMER": "Yaz", "FALL": "Sonbahar"}[s]
}

// Season lists a season's anime, most popular first.
func Season(ctx context.Context, season string, year int) ([]Media, error) {
	q := `query($s:MediaSeason,$y:Int,$page:Int){Page(page:$page,perPage:50){pageInfo{hasNextPage}
media(season:$s,seasonYear:$y,type:ANIME,sort:POPULARITY_DESC,isAdult:false){` + mediaFields + `}}}`
	var out []Media
	for page := 1; page <= 2; page++ {
		var res struct {
			Page struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				Media []Media `json:"media"`
			} `json:"Page"`
		}
		if err := query(ctx, q, map[string]any{"s": season, "y": year, "page": page}, 6*time.Hour, &res); err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		for _, m := range res.Page.Media {
			if keep(m) {
				out = append(out, m)
			}
		}
		if !res.Page.PageInfo.HasNextPage {
			break
		}
	}
	return out, nil
}

// ByMal fetches one title (with description, trailer, recommendations).
func ByMal(ctx context.Context, idMal int) (Media, []Media, error) {
	q := `query($id:Int){Media(idMal:$id,type:ANIME){` + mediaFields + ` description(asHtml:false) trailer{id site}
recommendations(sort:RATING_DESC,perPage:12){nodes{mediaRecommendation{` + mediaFields + `}}}}}`
	var res struct {
		Media struct {
			Media
			Recs struct {
				Nodes []struct {
					M *Media `json:"mediaRecommendation"`
				} `json:"nodes"`
			} `json:"recommendations"`
		} `json:"Media"`
	}
	if err := query(ctx, q, map[string]any{"id": idMal}, 24*time.Hour, &res); err != nil {
		return Media{}, nil, err
	}
	var recs []Media
	for _, n := range res.Media.Recs.Nodes {
		if n.M != nil && keep(*n.M) {
			recs = append(recs, *n.M)
		}
	}
	return res.Media.Media, recs, nil
}

// Search finds titles by name (used to map a name to a MAL id).
func Search(ctx context.Context, name string) ([]Media, error) {
	q := `query($q:String){Page(perPage:8){media(search:$q,type:ANIME,isAdult:false){` + mediaFields + `}}}`
	var res struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	if err := query(ctx, q, map[string]any{"q": name}, 24*time.Hour, &res); err != nil {
		return nil, err
	}
	return res.Page.Media, nil
}

// Random picks a well-liked anime at random.
func Random(ctx context.Context) (Media, error) {
	q := `query($page:Int){Page(page:$page,perPage:1){media(type:ANIME,sort:POPULARITY_DESC,isAdult:false,averageScore_greater:68,format_in:[TV,MOVIE,ONA]){` + mediaFields + ` description(asHtml:false) trailer{id site}}}}`
	var res struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	page := 1 + rand.IntN(600)
	if err := query(ctx, q, map[string]any{"page": page}, 7*24*time.Hour, &res); err != nil {
		return Media{}, err
	}
	if len(res.Page.Media) == 0 {
		return Media{}, errors.New("anilist: boş sonuç")
	}
	return res.Page.Media[0], nil
}

// Recommend aggregates recommendations for several titles you liked:
// titles recommended by many of them rank first; ones you have are skipped.
func Recommend(ctx context.Context, liked []int, have map[int]bool) ([]Media, error) {
	score := map[int]int{}
	byID := map[int]Media{}
	var firstErr error
	for i, id := range liked {
		if i >= 8 {
			break
		}
		_, recs, err := ByMal(ctx, id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for rank, r := range recs {
			if r.IDMal == 0 || have[r.IDMal] {
				continue
			}
			score[r.IDMal] += 12 - rank
			byID[r.IDMal] = r
		}
	}
	if len(byID) == 0 && firstErr != nil {
		return nil, firstErr
	}
	out := make([]Media, 0, len(byID))
	for _, m := range byID {
		out = append(out, m)
	}
	sortBy(out, func(a, b Media) bool {
		if score[a.IDMal] != score[b.IDMal] {
			return score[a.IDMal] > score[b.IDMal]
		}
		return a.Score > b.Score
	})
	return out, nil
}

func sortBy(ms []Media, less func(a, b Media) bool) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && less(ms[j], ms[j-1]); j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

// CleanDescription strips AniList's light HTML.
func CleanDescription(s string) string {
	r := strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "<i>", "", "</i>", "", "<b>", "", "</b>", "", "&quot;", "\"", "&amp;", "&", "&#039;", "'")
	s = r.Replace(s)
	for {
		i := strings.Index(s, "<")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], ">")
		if j < 0 {
			break
		}
		s = s[:i] + s[i+j+1:]
	}
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// Genres AniList knows (adult ones left out), with Turkish labels.
var Genres = [][2]string{
	{"Action", "Aksiyon"}, {"Adventure", "Macera"}, {"Comedy", "Komedi"}, {"Drama", "Dram"},
	{"Fantasy", "Fantastik"}, {"Horror", "Korku"}, {"Mahou Shoujo", "Büyücü kız"}, {"Mecha", "Mecha"},
	{"Music", "Müzik"}, {"Mystery", "Gizem"}, {"Psychological", "Psikolojik"}, {"Romance", "Romantik"},
	{"Sci-Fi", "Bilim kurgu"}, {"Slice of Life", "Gündelik hayat"}, {"Sports", "Spor"},
	{"Supernatural", "Doğaüstü"}, {"Thriller", "Gerilim"},
}

// Genre modes.
const (
	ModeTop     = "en iyi"  // highest rated
	ModePopular = "popüler" // most watched
	ModeCult    = "kült"    // beloved classics: older, highly rated, widely known
)

func genreVars(genre, mode string, page int) (string, map[string]any) {
	sort, extra := "SCORE_DESC", ""
	vars := map[string]any{"g": genre, "page": page}
	switch mode {
	case ModePopular:
		sort = "POPULARITY_DESC"
	case ModeCult:
		sort, extra = "POPULARITY_DESC", ",averageScore_greater:77,startDate_lesser:$y"
		vars["y"] = (time.Now().Year() - 8) * 10000 // FuzzyDateInt YYYYMMDD
	}
	decl := "$g:String,$page:Int"
	if mode == ModeCult {
		decl += ",$y:FuzzyDateInt"
	}
	return `query(` + decl + `){Page(page:$page,perPage:40){media(genre:$g,type:ANIME,isAdult:false,sort:` + sort + `,format_in:[TV,MOVIE,ONA,OVA]` + extra + `){` + mediaFields + ` description(asHtml:false)}}}`, vars
}

// ByGenre lists a genre's titles in the given mode.
func ByGenre(ctx context.Context, genre, mode string) ([]Media, error) {
	q, vars := genreVars(genre, mode, 1)
	var res struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	if err := query(ctx, q, vars, 12*time.Hour, &res); err != nil {
		return nil, err
	}
	return res.Page.Media, nil
}

// RandomInGenre rolls the dice inside a genre (cult mode: a classic).
func RandomInGenre(ctx context.Context, genre, mode string) (Media, error) {
	page := 1 + rand.IntN(4)
	if mode == ModeCult {
		page = 1 + rand.IntN(2)
	}
	q, vars := genreVars(genre, mode, page)
	var res struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	if err := query(ctx, q, vars, 12*time.Hour, &res); err != nil {
		return Media{}, err
	}
	if len(res.Page.Media) == 0 {
		return Media{}, errors.New("anilist: bu türde sonuç yok")
	}
	return res.Page.Media[rand.IntN(len(res.Page.Media))], nil
}
