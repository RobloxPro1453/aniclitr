// Package seasons builds canonical multi-season chains from AniList relations
// and aligns provider episode numbering (PerSeason or Absolute) to canonical
// MyAnimeList / AniList entries.
//
// Algorithm adapted from Migurdex's SeasonChainService (GPL-3.0).
package seasons

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/anilist"
	"aniclitr/internal/source"
)

const maxChainLength = 8

// Entry is one node in a franchise's season chain.
type Entry struct {
	Season    int // 1..N for numbered TV/OVA seasons; 0 for specials/movies
	MalID     int
	AniListID int
	Episodes  int // 0 if still airing / unknown
	Year      int
	Title     string
	Format    string // "TV", "OVA", "MOVIE", "ONA", "SPECIAL"
}

// Chain represents the ordered chronological season sequence of a franchise.
type Chain struct {
	Root      int // AniList ID of the first numbered season
	Entries   []Entry
	Truncated bool
}

// FetchFunc returns a media node and its relation edges for a given AniList ID.
type FetchFunc func(ctx context.Context, id int) (anilist.Media, []anilist.RelationEdge, error)

var (
	cacheMu sync.Mutex
	cache   = map[int]cachedChain{}
)

type cachedChain struct {
	chain Chain
	until time.Time
}

// Build walks PREQUEL edges backward (up to 8 steps) and SEQUEL edges forward
// to construct the canonical season chain for an AniList ID.
func Build(ctx context.Context, anilistID int) (Chain, error) {
	if anilistID <= 0 {
		return Chain{}, fmt.Errorf("seasons: geçersiz anilist id: %d", anilistID)
	}
	cacheMu.Lock()
	if c, ok := cache[anilistID]; ok && time.Now().Before(c.until) {
		cacheMu.Unlock()
		return c.chain, nil
	}
	cacheMu.Unlock()

	ch, err := BuildWithFetcher(ctx, anilistID, anilist.ByIDWithRelations)
	if err != nil {
		return Chain{}, err
	}
	ttl := 6 * time.Hour
	if ch.Truncated {
		ttl = time.Minute
	}
	cacheMu.Lock()
	cache[anilistID] = cachedChain{chain: ch, until: time.Now().Add(ttl)}
	cacheMu.Unlock()
	return ch, nil
}

// BuildByMal resolves the starting node by MAL ID and builds its season chain.
func BuildByMal(ctx context.Context, malID int) (Chain, error) {
	if malID <= 0 {
		return Chain{}, fmt.Errorf("seasons: geçersiz mal id: %d", malID)
	}
	root, edges, err := anilist.ByMalWithRelations(ctx, malID)
	if err != nil || root.ID == 0 {
		if err == nil {
			err = fmt.Errorf("seasons: mal %d bulunamadı", malID)
		}
		return Chain{}, err
	}
	return BuildWithFetcher(ctx, root.ID, func(ctx context.Context, id int) (anilist.Media, []anilist.RelationEdge, error) {
		if id == root.ID {
			return root, edges, nil
		}
		return anilist.ByIDWithRelations(ctx, id)
	})
}

// BuildWithFetcher builds a Chain using the supplied relation fetcher (used by tests).
func BuildWithFetcher(ctx context.Context, startID int, fetch FetchFunc) (Chain, error) {
	root, rootEdges, err := fetch(ctx, startID)
	if err != nil {
		return Chain{}, err
	}
	memo := map[int][]anilist.RelationEdge{root.ID: rootEdges}
	nodes := map[int]anilist.Media{root.ID: root}

	getNode := func(id int) (anilist.Media, []anilist.RelationEdge, bool) {
		if m, ok := nodes[id]; ok {
			if es, ok2 := memo[id]; ok2 {
				return m, es, true
			}
		}
		m, es, err := fetch(ctx, id)
		if err != nil || m.ID == 0 {
			return anilist.Media{}, nil, false
		}
		nodes[m.ID] = m
		memo[m.ID] = es
		return m, es, true
	}

	path := []anilist.Media{root}
	seen := map[int]bool{root.ID: true}
	cursor := root.ID
	fetchErr := false
	headCapped := true

	for i := 0; i < maxChainLength-1; i++ {
		_, edges, ok := getNode(cursor)
		if !ok {
			fetchErr = true
			headCapped = false
			break
		}
		prequelID := findRelation(edges, "PREQUEL")
		if prequelID == 0 || seen[prequelID] {
			headCapped = false
			break
		}
		seen[prequelID] = true
		pm, _, ok := getNode(prequelID)
		if !ok {
			fetchErr = true
			headCapped = false
			break
		}
		path = append(path, pm)
		cursor = pm.ID
	}

	headTruncated := false
	if headCapped {
		if _, edges, ok := getNode(cursor); !ok {
			fetchErr = true
		} else if findRelation(edges, "PREQUEL") != 0 {
			headTruncated = true
		}
	}

	// Reverse path so earliest prequel is first.
	ordered := make([]anilist.Media, len(path))
	for i := range path {
		ordered[i] = path[len(path)-1-i]
	}
	orderedIDs := map[int]bool{}
	for _, m := range ordered {
		orderedIDs[m.ID] = true
	}
	cursor = ordered[len(ordered)-1].ID
	truncated := false

	for len(ordered) < maxChainLength {
		_, edges, ok := getNode(cursor)
		if !ok {
			fetchErr = true
			break
		}
		sequelID := findRelation(edges, "SEQUEL")
		if sequelID == 0 || orderedIDs[sequelID] {
			break
		}
		orderedIDs[sequelID] = true
		sm, _, ok := getNode(sequelID)
		if !ok {
			fetchErr = true
			break
		}
		ordered = append(ordered, sm)
		cursor = sm.ID
	}

	if len(ordered) >= maxChainLength {
		if _, edges, ok := getNode(cursor); !ok {
			fetchErr = true
		} else if findRelation(edges, "SEQUEL") != 0 {
			truncated = true
		}
	}

	truncated = truncated || headTruncated || fetchErr

	anySeasonFormat := false
	firstTV := -1
	for i, m := range ordered {
		if isSeasonFormat(m.Format) {
			anySeasonFormat = true
		}
		if firstTV < 0 && strings.EqualFold(m.Format, "TV") {
			firstTV = i
		}
	}
	if firstTV < 0 {
		firstTV = 0
	}

	ch := Chain{
		Root:      ordered[0].ID,
		Truncated: truncated,
	}
	seasonNo := 0
	for i, m := range ordered {
		isSeason := true
		if anySeasonFormat {
			isSeason = isSeasonFormat(m.Format)
		}
		sNum := 0
		if i >= firstTV && isSeason {
			seasonNo++
			sNum = seasonNo
		}
		ch.Entries = append(ch.Entries, Entry{
			Season:    sNum,
			MalID:     m.IDMal,
			AniListID: m.ID,
			Episodes:  m.Episodes,
			Year:      m.SeasonYear,
			Title:     m.Name(),
			Format:    strings.ToUpper(m.Format),
		})
	}
	for _, e := range ch.Entries {
		if e.Season > 0 {
			ch.Root = e.AniListID
			break
		}
	}
	return ch, nil
}

func findRelation(edges []anilist.RelationEdge, relType string) int {
	// Prefer TV/OVA seasons over Movie/Special when multiple relations exist.
	var fallback int
	for _, e := range edges {
		if !strings.EqualFold(e.RelationType, relType) || e.Media.ID == 0 {
			continue
		}
		if isSeasonFormat(e.Media.Format) {
			return e.Media.ID
		}
		if fallback == 0 {
			fallback = e.Media.ID
		}
	}
	return fallback
}

func isSeasonFormat(format string) bool {
	switch strings.ToUpper(strings.TrimSpace(format)) {
	case "", "TV", "TV_SHORT", "OVA", "ONA":
		return true
	}
	return false
}

// Mode describes how a provider numbers episodes across a franchise.
type Mode int

const (
	Unknown Mode = iota
	PerSeason
	Absolute
)

// Slice maps a contiguous range of provider episodes to a canonical season.
type Slice struct {
	ProviderSeason int
	CanonSeason    int
	MalID          int
	AniListID      int
	ProviderEps    int
	CanonEps       int // 0 = unknown (still airing)
	StartOffset    int // first provider episode number of this slice (1-based)
}

// Alignment holds the result of aligning a provider's season/episode counts to a Chain.
type Alignment struct {
	Mode     Mode
	Slices   []Slice
	Warnings []string
}

// Align matches a source's seasons and episode counts against a canonical Chain.
func Align(providerSeasons map[int]int, c Chain) Alignment {
	return AlignTitle("", "", providerSeasons, c)
}

// AlignTitle aligns provider seasons to a canonical Chain, also considering
// the provider's title (for embedded season numbers) and format (TV vs Film warnings).
func AlignTitle(title, kind string, providerSeasons map[int]int, c Chain) Alignment {
	al := Alignment{}
	var tv []Entry
	for _, e := range c.Entries {
		if e.Season > 0 {
			tv = append(tv, e)
		}
	}

	// Check for TV vs Movie mismatch on chain entries (e.g. Youjo Senki TV mapped to Movie MAL ID).
	if strings.EqualFold(kind, "TV") && len(c.Entries) == 1 && strings.EqualFold(c.Entries[0].Format, "MOVIE") {
		al.Warnings = append(al.Warnings, "Sağlayıcı TV dizisi diyor ancak eşleşen kayıt Film biçiminde.")
	}

	var pSeasons []int
	totalEps := 0
	for s, cnt := range providerSeasons {
		if cnt > 0 {
			pSeasons = append(pSeasons, s)
			totalEps += cnt
		}
	}
	sort.Ints(pSeasons)

	if len(tv) == 0 || len(pSeasons) == 0 || totalEps == 0 {
		al.Warnings = append(al.Warnings, "Zincir veya bölüm listesi boş; hizalama yapılamadı.")
		return al
	}

	usedChain := map[int]bool{}
	titleSeason := source.ParseSeasonNumber(title)

	if len(pSeasons) == 1 {
		ps := pSeasons[0]
		count := providerSeasons[ps]
		startIdx := 0
		if titleSeason > 1 && titleSeason <= len(tv) {
			startIdx = titleSeason - 1
		} else if titleSeason > len(tv) {
			al.Warnings = append(al.Warnings, fmt.Sprintf("Başlık S%d diyor ama zincirde %d sezon var; S1'den başlandı.", titleSeason, len(tv)))
		}
		if !tryExpandAbsolute(tv, startIdx, count, &al, ps) {
			target := tv[startIdx]
			al.Slices = append(al.Slices, Slice{
				ProviderSeason: ps,
				CanonSeason:    target.Season,
				MalID:          target.MalID,
				AniListID:      target.AniListID,
				ProviderEps:    count,
				CanonEps:       target.Episodes,
				StartOffset:    1,
			})
		}
	} else {
		for _, ps := range pSeasons {
			count := providerSeasons[ps]
			if ps == 0 {
				for _, e := range c.Entries {
					if e.Season == 0 {
						al.Slices = append(al.Slices, Slice{
							ProviderSeason: 0,
							CanonSeason:    0,
							MalID:          e.MalID,
							AniListID:      e.AniListID,
							ProviderEps:    count,
							CanonEps:       e.Episodes,
							StartOffset:    1,
						})
						break
					}
				}
				continue
			}
			startIdx := -1
			for i, ce := range tv {
				if ce.Season >= ps && !usedChain[ce.Season] {
					startIdx = i
					break
				}
			}
			if startIdx < 0 {
				for i, ce := range tv {
					if !usedChain[ce.Season] {
						startIdx = i
						break
					}
				}
			}
			if startIdx < 0 {
				continue
			}
			span := spanSeasons(tv, startIdx, count, usedChain)
			used := 0
			for i, sp := range span {
				size := count
				if len(span) > 1 {
					if i < len(span)-1 {
						size = sp.entry.Episodes
					} else {
						size = count - used
					}
				}
				used += size
				usedChain[sp.entry.Season] = true
				al.Slices = append(al.Slices, Slice{
					ProviderSeason: ps,
					CanonSeason:    sp.entry.Season,
					MalID:          sp.entry.MalID,
					AniListID:      sp.entry.AniListID,
					ProviderEps:    size,
					CanonEps:       sp.entry.Episodes,
					StartOffset:    sp.offset,
				})
			}
		}
	}

	al.Mode = detectNumberingMode(pSeasons, totalEps, al, tv)
	for _, s := range al.Slices {
		if s.CanonEps > 0 && s.ProviderEps > s.CanonEps {
			al.Warnings = append(al.Warnings, fmt.Sprintf("S%d: sağlayıcı %d bölüm veriyor, kanonik %d.", s.CanonSeason, s.ProviderEps, s.CanonEps))
		}
	}
	return al
}

type spanItem struct {
	entry  Entry
	offset int
}

func spanSeasons(tv []Entry, startIdx, epCount int, usedChain map[int]bool) []spanItem {
	single := []spanItem{{entry: tv[startIdx], offset: 1}}
	for k := len(tv) - startIdx; k >= 2; k-- {
		slice := tv[startIdx : startIdx+k]
		valid := true
		sum := 0
		for i, e := range slice {
			if e.Episodes <= 0 || e.Season != tv[startIdx].Season+i || (i > 0 && usedChain[e.Season]) {
				valid = false
				break
			}
			sum += e.Episodes
		}
		if !valid || epCount < sum-2 || epCount > sum+5 {
			continue
		}
		out := make([]spanItem, 0, len(slice))
		offset := 1
		for _, e := range slice {
			out = append(out, spanItem{entry: e, offset: offset})
			offset += e.Episodes
		}
		return out
	}
	return single
}

func tryExpandAbsolute(tv []Entry, startIdx, epCount int, al *Alignment, providerSeason int) bool {
	for k := len(tv) - startIdx; k >= 2; k-- {
		slice := tv[startIdx : startIdx+k]
		valid := true
		sum := 0
		for _, e := range slice {
			if e.Episodes <= 0 {
				valid = false
				break
			}
			sum += e.Episodes
		}
		if !valid || epCount < sum-2 || epCount > sum+5 {
			continue
		}
		offset := 1
		for i, e := range slice {
			cnt := e.Episodes
			if i == len(slice)-1 {
				cnt = epCount - offset + 1
			}
			al.Slices = append(al.Slices, Slice{
				ProviderSeason: providerSeason,
				CanonSeason:    e.Season,
				MalID:          e.MalID,
				AniListID:      e.AniListID,
				ProviderEps:    cnt,
				CanonEps:       e.Episodes,
				StartOffset:    offset,
			})
			offset += cnt
		}
		return true
	}
	return false
}

func detectNumberingMode(pSeasons []int, totalEps int, al Alignment, tv []Entry) Mode {
	if len(pSeasons) > 1 {
		return PerSeason
	}
	if len(al.Slices) > 1 {
		return Absolute
	}
	canonTotal := 0
	maxSingle := 0
	for _, c := range tv {
		canonTotal += c.Episodes
		if c.Episodes > maxSingle {
			maxSingle = c.Episodes
		}
	}
	if canonTotal > 0 && totalEps >= canonTotal-2 && totalEps <= canonTotal+5 && totalEps > maxSingle {
		return Absolute
	}
	if len(al.Slices) > 0 {
		return PerSeason
	}
	return Unknown
}

// Canon is the canonical season, episode number, and tracker IDs for a provider episode.
type Canon struct {
	Season    int
	Episode   int
	MalID     int
	AniListID int
	Overflow  bool
}

// ToCanonical maps (providerSeason, ep) to its canonical MAL/AniList entry and episode number.
func (al Alignment) ToCanonical(providerSeason, ep int) (Canon, bool) {
	if len(al.Slices) == 0 || ep <= 0 {
		return Canon{}, false
	}
	if al.Mode == Absolute {
		slices := append([]Slice(nil), al.Slices...)
		sort.SliceStable(slices, func(i, j int) bool { return slices[i].StartOffset < slices[j].StartOffset })
		var last *Slice
		for i := range slices {
			s := &slices[i]
			last = s
			end := s.StartOffset + s.ProviderEps - 1
			if ep >= s.StartOffset && ep <= end {
				num := ep - s.StartOffset + 1
				return Canon{
					Season:    s.CanonSeason,
					Episode:   num,
					MalID:     s.MalID,
					AniListID: s.AniListID,
					Overflow:  s.CanonEps > 0 && num > s.CanonEps,
				}, true
			}
		}
		if last != nil && ep > last.StartOffset {
			num := ep - last.StartOffset + 1
			return Canon{
				Season:    last.CanonSeason,
				Episode:   num,
				MalID:     last.MalID,
				AniListID: last.AniListID,
				Overflow:  true,
			}, true
		}
		return Canon{}, false
	}

	ps := providerSeason
	if ps <= 0 {
		ps = 1
	}
	var group []Slice
	for _, s := range al.Slices {
		if s.ProviderSeason == ps {
			group = append(group, s)
		}
	}
	sort.SliceStable(group, func(i, j int) bool { return group[i].StartOffset < group[j].StartOffset })
	if len(group) == 0 {
		if len(al.Slices) == 1 {
			group = al.Slices[:1]
		} else {
			return Canon{}, false
		}
	}
	if len(group) > 1 {
		for _, s := range group {
			end := s.StartOffset + s.ProviderEps - 1
			if ep >= s.StartOffset && ep <= end {
				num := ep - s.StartOffset + 1
				return Canon{
					Season:    s.CanonSeason,
					Episode:   num,
					MalID:     s.MalID,
					AniListID: s.AniListID,
					Overflow:  s.CanonEps > 0 && num > s.CanonEps,
				}, true
			}
		}
		if ep < group[0].StartOffset {
			s := group[0]
			return Canon{Season: s.CanonSeason, Episode: ep, MalID: s.MalID, AniListID: s.AniListID}, true
		}
		last := group[len(group)-1]
		num := ep - last.StartOffset + 1
		return Canon{
			Season:    last.CanonSeason,
			Episode:   num,
			MalID:     last.MalID,
			AniListID: last.AniListID,
			Overflow:  true,
		}, true
	}
	m := group[0]
	return Canon{
		Season:    m.CanonSeason,
		Episode:   ep,
		MalID:     m.MalID,
		AniListID: m.AniListID,
		Overflow:  m.CanonEps > 0 && ep > m.CanonEps,
	}, true
}

// FromCanonical maps a MAL entry's watched episode count back to provider
// (providerSeason, maxProviderEpisode) so multi-season lists render watched marks accurately.
func (al Alignment) FromCanonical(malID, watched int) (providerSeason, upToEp int, ok bool) {
	if malID <= 0 || watched <= 0 {
		return 0, 0, false
	}
	for _, s := range al.Slices {
		if s.MalID == malID {
			n := watched
			if s.CanonEps > 0 && n > s.CanonEps {
				n = s.CanonEps
			}
			return s.ProviderSeason, s.StartOffset + n - 1, true
		}
	}
	return 0, 0, false
}
