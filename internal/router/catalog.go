package router

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"aniclitr/internal/match"
	"aniclitr/internal/source"
)

// Offer represents one provider's listing for a canonical anime title.
type Offer struct {
	SourceID   string
	SourceName string
	Lang       string
	Primary    bool
	Anime      source.Anime
	ISPScore   float64
}

// UnifiedItem is a source-agnostic catalog entry merging every provider that
// carries the same anime (by canonical MAL ID or folded title).
type UnifiedItem struct {
	CanonicalKey string
	MalID        int
	Title        string
	AltTitle     string
	Year         int
	Kind         string
	Poster       string
	EpCount      int
	Offers       []Offer
	Relevance    int
}

// BestOffer returns the highest-ranked provider offer for this anime.
func (u UnifiedItem) BestOffer() (Offer, bool) {
	if len(u.Offers) == 0 {
		return Offer{}, false
	}
	return u.Offers[0], true
}

// MergeCatalog collapses multi-provider search hits into a single
// source-agnostic catalog ranked by query relevance, Turkish subtitle priority,
// and the user's personal ISP router score.
func MergeCatalog(query string, bySource map[string][]source.Anime, r *Router) []UnifiedItem {
	if r == nil {
		r = Default()
	}
	meta := map[string]source.Info{}
	for _, p := range source.All() {
		in := p.Info()
		meta[in.ID] = in
	}

	groups := map[string]*UnifiedItem{}
	var order []string
	for srcID, items := range bySource {
		in := meta[srcID]
		srcName := in.Name
		if srcName == "" {
			srcName = srcID
		}
		for _, an := range items {
			key := canonicalKey(an)
			u, ok := groups[key]
			if !ok {
				u = &UnifiedItem{
					CanonicalKey: key,
					MalID:        an.MalID,
					Title:        an.Title,
					AltTitle:     an.AltTitle,
					Year:         an.Year,
					Kind:         an.Kind,
					Poster:       an.Poster,
					EpCount:      an.EpCount,
				}
				groups[key] = u
				order = append(order, key)
			}
			if u.MalID == 0 && an.MalID > 0 {
				u.MalID = an.MalID
			}
			if u.Poster == "" && an.Poster != "" {
				u.Poster = an.Poster
			}
			if u.Year == 0 && an.Year > 0 {
				u.Year = an.Year
			}
			if u.EpCount == 0 && an.EpCount > 0 {
				u.EpCount = an.EpCount
			}
			if u.AltTitle == "" && an.AltTitle != "" {
				u.AltTitle = an.AltTitle
			}
			u.Offers = append(u.Offers, Offer{
				SourceID:   srcID,
				SourceName: srcName,
				Lang:       in.Lang,
				Primary:    in.Primary,
				Anime:      an,
				ISPScore:   r.Score(srcName),
			})
		}
	}

	out := make([]UnifiedItem, 0, len(order))
	for _, k := range order {
		u := groups[k]
		sort.SliceStable(u.Offers, func(i, j int) bool {
			oi, oj := u.Offers[i], u.Offers[j]
			// Turkish sources come before English sources for Turkish users
			tri := strings.EqualFold(oi.Lang, "tr") || oi.Lang == ""
			trj := strings.EqualFold(oj.Lang, "tr") || oj.Lang == ""
			if tri != trj {
				return tri
			}
			si := oi.ISPScore
			sj := oj.ISPScore
			if oi.Primary {
				si += 10
			}
			if oj.Primary {
				sj += 10
			}
			return si > sj
		})
		var allTitles []string
		allTitles = append(allTitles, u.Title, u.AltTitle)
		for _, of := range u.Offers {
			allTitles = append(allTitles, of.Anime.AllTitles()...)
		}
		u.Relevance = match.RankTitle(query, allTitles...)
		out = append(out, *u)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Relevance != out[j].Relevance {
			return out[i].Relevance > out[j].Relevance
		}
		if len(out[i].Offers) != len(out[j].Offers) {
			return len(out[i].Offers) > len(out[j].Offers)
		}
		return out[i].Year > out[j].Year
	})
	return out
}

func canonicalKey(an source.Anime) string {
	if an.MalID > 0 {
		return fmt.Sprintf("mal:%d", an.MalID)
	}
	f := match.Fold(an.Title)
	if f == "" {
		return an.Key()
	}
	return "title:" + f
}

// FailoverCandidate is one backup stream ready for serverless mpv IPC hot-swap.
type FailoverCandidate struct {
	Host    string
	Fansub  string
	Stream  *source.Stream
	Quality source.Quality
}

// PickFailover selects the highest-ISP-scored healthy backup stream excluding
// failed URLs. Completely serverless: the caller hands the chosen URL straight
// to mpv via IPC loadfile with start=<currentPos>.
func PickFailover(candidates []FailoverCandidate, failedURLs map[string]bool, r *Router) (FailoverCandidate, bool) {
	if r == nil {
		r = Default()
	}
	var avail []FailoverCandidate
	for _, c := range candidates {
		if c.Stream == nil || c.Quality.URL == "" || c.Quality.Broken {
			continue
		}
		if failedURLs != nil && failedURLs[c.Quality.URL] {
			continue
		}
		avail = append(avail, c)
	}
	if len(avail) == 0 {
		return FailoverCandidate{}, false
	}
	sort.SliceStable(avail, func(i, j int) bool {
		si := r.Score(avail[i].Host)
		sj := r.Score(avail[j].Host)
		if math.Abs(si-sj) > 5 {
			return si > sj
		}
		return avail[i].Quality.Height > avail[j].Quality.Height
	})
	return avail[0], true
}
