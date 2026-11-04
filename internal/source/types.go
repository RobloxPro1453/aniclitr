// Package source defines the provider contract every anime site implements.
package source

import (
	"context"
	"errors"
	"sort"
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
	Label  string
	URL    string
	Height int
	Size   int64
	// Page means URL is the host's web page, not a media file: mpv (through
	// its yt-dlp hook) and yt-dlp open it themselves, keeping the cookies
	// some hosts (Google Drive) tie their stream links to.
	Page bool
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

// SortQualities orders renditions from best to worst.
func (s *Stream) SortQualities() {
	sort.SliceStable(s.Qualities, func(i, j int) bool { return s.Qualities[i].Height > s.Qualities[j].Height })
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
