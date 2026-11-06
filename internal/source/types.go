// Package source defines the provider contract every anime site implements.
package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ErrNotFound is returned when a provider has no data for a request.
var ErrNotFound = errors.New("bulunamadı")

// Info describes a provider.
type Info struct {
	ID      string // stable key, e.g. "animecix"
	Name    string // display name
	Lang    string // "tr" or "en"
	Primary bool   // the default source
	Legacy  bool   // archive source: frozen data, links may be dead
	Note    string // one line for settings and the sources screen
}

// Anime is a title as returned by search or details.
type Anime struct {
	Source      string
	ID          string
	Title       string
	AltTitle    string
	Titles      []string // additional title variants (English, Romaji, Japanese, synonyms)
	Year        int
	Kind        string // "TV", "Film", "OVA"...
	Poster      string
	Description string
	Genres      []string
	MalID       int
	EpCount     int
	NextEpisode string // human readable next release info, if known
	Seasons     []Season
}

// AllTitles returns all distinct non-empty titles known for this anime.
func (a Anime) AllTitles() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		k := strings.ToLower(s)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	add(a.Title)
	add(a.AltTitle)
	for _, t := range a.Titles {
		add(t)
	}
	return out
}

var (
	reMovieTitle   = regexp.MustCompile(`(?i)\b(?:movie|film|gekijouban)\b`)
	reMovieSummary = regexp.MustCompile(`(?i)\b(?:film|filmi|filmidir|filmlerinden|filmleri|movie|movies)\b`)
	reSeasonPrefix = regexp.MustCompile(`(?i)\b(?:season|sezon|part|k[ıi]s[ıi]m)\s*(\d{1,2})\b`)
	reSeasonSuffix = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\.?\s*(?:season|sezon|part|k[ıi]s[ıi]m)\b`)
	reSeasonShort  = regexp.MustCompile(`(?i)\bs(\d{1,2})\b`)
	reRomanSuffix  = regexp.MustCompile(`(?i)\b(I|II|III|IV|V|VI|VII|VIII|IX|X)\s*$`)
)

// IsMovieTitle reports whether a title string explicitly marks a movie.
func IsMovieTitle(title string) bool {
	return title != "" && reMovieTitle.MatchString(title)
}

// IsMovieSummary reports whether a Turkish/English synopsis describes a movie.
func IsMovieSummary(summary string) bool {
	return summary != "" && reMovieSummary.MatchString(summary)
}

// ParseSeasonNumber extracts a season number from a title (defaults to 1).
func ParseSeasonNumber(title string) int {
	title = strings.TrimSpace(title)
	if title == "" {
		return 1
	}
	if m := reSeasonPrefix.FindStringSubmatch(title); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	if m := reSeasonSuffix.FindStringSubmatch(title); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	if m := reSeasonShort.FindStringSubmatch(title); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	if m := reRomanSuffix.FindStringSubmatch(title); m != nil {
		switch strings.ToUpper(m[1]) {
		case "I":
			return 1
		case "II":
			return 2
		case "III":
			return 3
		case "IV":
			return 4
		case "V":
			return 5
		case "VI":
			return 6
		case "VII":
			return 7
		case "VIII":
			return 8
		case "IX":
			return 9
		case "X":
			return 10
		}
	}
	return 1
}

// Normalize applies Turkish/English movie and season normalization rules
// adapted from Migurdex's AnimeDetails.Normalize().
func (a *Anime) Normalize() {
	if a == nil {
		return
	}
	if a.Kind == "" || strings.EqualFold(a.Kind, "tv") || strings.EqualFold(a.Kind, "unknown") {
		for _, t := range a.AllTitles() {
			if IsMovieTitle(t) {
				a.Kind = "Film"
				break
			}
		}
		if a.Kind != "Film" && a.EpCount == 1 && IsMovieSummary(a.Description) {
			a.Kind = "Film"
		}
	}
}

// NormalizeEpisodes normalizes episode numbering and labels when an anime is a movie.
func NormalizeEpisodes(a *Anime, eps []Episode) []Episode {
	if a == nil {
		return eps
	}
	if (a.Kind == "" || strings.EqualFold(a.Kind, "tv")) && len(eps) == 1 && IsMovieSummary(a.Description) {
		a.Kind = "Film"
	}
	a.Normalize()
	if a.IsMovie() && len(eps) == 1 {
		eps[0].Season = 1
		eps[0].Number = 1
		t := strings.TrimSpace(eps[0].Title)
		switch strings.ToLower(t) {
		case "", "1. bölüm", "bölüm 1", "1", "movie", "film", "special":
			eps[0].Title = "Film"
		}
	}
	return eps
}

// Clean strips control/format characters and collapses whitespace so no
// stray byte (e.g. NUL) ever reaches a site's URL.
func Clean(q string) string {
	q = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '�' {
			return ' '
		}
		return r
	}, q)
	return strings.Join(strings.Fields(q), " ")
}

// Key uniquely identifies an anime across providers.
func (a Anime) Key() string { return a.Source + ":" + a.ID }

// IsMovie reports whether the title is a single-video movie.
func (a Anime) IsMovie() bool {
	return strings.EqualFold(a.Kind, "film") || strings.EqualFold(a.Kind, "movie")
}

// Season groups episodes.
type Season struct {
	Number       int
	Name         string
	EpisodeCount int
}

// Episode is a single playable unit.
type Episode struct {
	ID     string
	Season int
	Number int
	Title  string
}

// Variant is one way to watch an episode: a fansub/audio track on a host.
type Variant struct {
	Fansub string
	Host   string
	URL    string
	Data   map[string]string
}

// Quality is one directly playable rendition.
type Quality struct {
	Label    string
	URL      string
	Height   int
	Size     int64
	Bitrate  int64  // bits per second (0 if unknown)
	Codec    string // e.g. "H.264", "HEVC", "AV1"
	Duration int    // duration in seconds (0 if unknown)
	Probed   bool   // metadata verified by HLS/MP4 probe
	Broken   bool   // probe determined the stream is unplayable
	// Page means URL is the host's web page, not a media file: mpv (through
	// its yt-dlp hook) and yt-dlp open it themselves, keeping the cookies
	// some hosts (Google Drive) tie their stream links to.
	Page bool
}

// DisplayLabel formats a quality label with probed codec and bitrate if known.
func (q Quality) DisplayLabel() string {
	lbl := q.Label
	if lbl == "" {
		if q.Height > 0 {
			lbl = fmt.Sprintf("%dp", q.Height)
		} else {
			lbl = "auto"
		}
	}
	var parts []string
	parts = append(parts, lbl)
	if q.Bitrate > 0 {
		mbps := float64(q.Bitrate) / 1_000_000.0
		if mbps >= 0.1 {
			parts = append(parts, strings.ReplaceAll(fmt.Sprintf("%.1f Mbps", mbps), ".", ","))
		}
	}
	if q.Codec != "" {
		parts = append(parts, q.Codec)
	}
	if q.Duration >= 60 {
		parts = append(parts, fmt.Sprintf("%d dk", (q.Duration+30)/60))
	}
	return strings.Join(parts, " • ")
}

// Subtitle is an external subtitle track.
type Subtitle struct {
	Label string
	URL   string
}

// Stream is a resolved, playable variant.
type Stream struct {
	Qualities []Quality // best first
	Referer   string
	UserAgent string
	Subtitles []Subtitle
	MalID     int // discovered while resolving (used for aniskip)
}

// SortQualities orders renditions from best to worst (healthy before broken,
// higher resolution first, higher bitrate on ties).
func (s *Stream) SortQualities() {
	sort.SliceStable(s.Qualities, func(i, j int) bool {
		a, b := s.Qualities[i], s.Qualities[j]
		if a.Broken != b.Broken {
			return !a.Broken
		}
		if a.Height != b.Height {
			return a.Height > b.Height
		}
		return a.Bitrate > b.Bitrate
	})
}

// FeedItem is a freshly released episode.
type FeedItem struct {
	Anime    Anime
	Season   int
	Episode  int
	Fansub   string
	Released time.Time
}

// Provider is implemented by every source.
type Provider interface {
	Info() Info
	Search(ctx context.Context, query string) ([]Anime, error)
	// Details fills description, genres and season metadata.
	Details(ctx context.Context, a Anime) (Anime, error)
	Episodes(ctx context.Context, a Anime, season int) ([]Episode, error)
	Variants(ctx context.Context, a Anime, ep Episode) ([]Variant, error)
	Resolve(ctx context.Context, v Variant) (*Stream, error)
}

// Feeder is implemented by providers that expose a "latest episodes" feed.
type Feeder interface {
	Latest(ctx context.Context) ([]FeedItem, error)
}

// URLOpener is an optional interface implemented by providers that can open
// a direct anime or episode URL pasted by the user.
type URLOpener interface {
	OpenURL(ctx context.Context, u *url.URL) (Anime, *Episode, bool, error)
}
