// Package match implements Turkish-aware title normalization, season splitting,
// fuzzy candidate scoring, and persistent tracker ID mappings.
//
// Scoring and normalization rules are adapted from Migurdex's TitleNormalizer
// and TrackerIdResolver (GPL-3.0), combined with curd's cover-URL signal and
// anime-sdk's episode-count border validation.
package match

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"aniclitr/internal/source"
)

const (
	MinScore              = 0.80
	BorderScore           = 0.73
	AmbiguityGap          = 0.05
	 exactTieEpsilon      = 1e-9
	yearBonus             = 0.05
	formatBonus           = 0.03
	formatMismatchPenalty = -0.15
	seasonBonus           = 0.05
	seasonMismatchPenalty = -0.10
	containmentFloor      = 0.90
)

var (
	reParenTag     = regexp.MustCompile(`(?i)\((?:tv|movie|film|ova|ona|special|özel|ozel|bd|blu-?ray|dublaj|altyaz[ıi]l[ıi]|19\d{2}|20\d{2})\)`)
	reTrailingIzle = regexp.MustCompile(`\s+(?:turkce\s+)?(?:altyazili\s+|dublaj\s+)?izle\s*$`)
	reNonAlnum     = regexp.MustCompile(`[^\p{L}\p{N}\s]+`)
	reSpaces       = regexp.MustCompile(`\s+`)

	reSeasonPre = regexp.MustCompile(`\b(?:season|sezon|part|kisim)\s*(\d{1,2})(?:st|nd|rd|th)?\b`)
	reSeasonSuf = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s*\.?\s*(?:season|sezon|part|kisim)\b`)
	reSeasonS   = regexp.MustCompile(`\bs(\d{1,2})\b`)
	reRomanEnd  = regexp.MustCompile(`\b(ii|iii|iv|v|vi|vii|viii|ix|x)\s*$`)
)

// FoldRune folds Turkish and accented Latin runes to ASCII lowercase.
func FoldRune(r rune) rune {
	switch r {
	case 'İ', 'I', 'ı', 'î', 'Î', 'í', 'ì', 'ï':
		return 'i'
	case 'ğ', 'Ğ':
		return 'g'
	case 'ü', 'Ü', 'û', 'Û', 'ú', 'ù':
		return 'u'
	case 'ş', 'Ş':
		return 's'
	case 'ö', 'Ö', 'ô', 'Ô', 'ó', 'ò':
		return 'o'
	case 'ç', 'Ç':
		return 'c'
	case 'â', 'Â', 'á', 'à', 'ä', 'ã':
		return 'a'
	case 'é', 'è', 'ê', 'ë', 'É', 'È', 'Ê':
		return 'e'
	case 'ñ', 'Ñ':
		return 'n'
	}
	return unicode.ToLower(r)
}

// Fold converts a string using Turkish-aware rune folding.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(FoldRune(r))
	}
	return b.String()
}

// Normalize folds Turkish characters, strips trailing "izle" and format tags
// like "(TV)" or "(2023)", converts punctuation to spaces, and collapses whitespace.
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = reParenTag.ReplaceAllString(s, " ")
	s = Fold(s)
	s = reTrailingIzle.ReplaceAllString(s, "")
	if strings.HasSuffix(s, " izle") {
		s = strings.TrimSpace(s[:len(s)-5])
	}
	s = reNonAlnum.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// SplitSeason returns the normalized base title (with season markers removed)
// and the detected season number (1 if none).
func SplitSeason(s string) (base string, season int) {
	norm := Normalize(s)
	if norm == "" {
		return "", 1
	}
	season = 1
	base = norm

	if m := reSeasonPre.FindStringSubmatchIndex(norm); m != nil {
		if n, err := strconv.Atoi(norm[m[2]:m[3]]); err == nil && n > 0 {
			season = n
		}
		base = norm[:m[0]] + " " + norm[m[1]:]
	} else if m := reSeasonSuf.FindStringSubmatchIndex(norm); m != nil {
		if n, err := strconv.Atoi(norm[m[2]:m[3]]); err == nil && n > 0 {
			season = n
		}
		base = norm[:m[0]] + " " + norm[m[1]:]
	} else if m := reSeasonS.FindStringSubmatchIndex(norm); m != nil {
		if n, err := strconv.Atoi(norm[m[2]:m[3]]); err == nil && n > 0 {
			season = n
		}
		base = norm[:m[0]] + " " + norm[m[1]:]
	} else if m := reRomanEnd.FindStringSubmatchIndex(norm); m != nil {
		// Only treat trailing Roman numeral as season when there is a base title before it
		prefix := strings.TrimSpace(norm[:m[0]])
		if len(prefix) >= 3 {
			season = romanToInt(norm[m[2]:m[3]])
			base = prefix
		}
	}

	base = strings.TrimSpace(reSpaces.ReplaceAllString(base, " "))
	if base == "" {
		base = norm
	}
	return base, season
}

func romanToInt(r string) int {
	switch strings.ToLower(r) {
	case "ii":
		return 2
	case "iii":
		return 3
	case "iv":
		return 4
	case "v":
		return 5
	case "vi":
		return 6
	case "vii":
		return 7
	case "viii":
		return 8
	case "ix":
		return 9
	case "x":
		return 10
	}
	return 1
}

// Ratio computes the normalized Levenshtein similarity in [0, 1]:
// (|a| + |b| - 2*d) / (|a| + |b|).
func Ratio(a, b string) float64 {
	x := []rune(Normalize(a))
	y := []rune(Normalize(b))
	if len(x) == 0 && len(y) == 0 {
		return 1.0
	}
	if len(x) == 0 || len(y) == 0 {
		return 0.0
	}
	if string(x) == string(y) {
		return 1.0
	}
	d := levenshtein(x, y)
	total := len(x) + len(y)
	r := float64(total-2*d) / float64(total)
	if r < 0 {
		return 0
	}
	return r
}

func levenshtein(x, y []rune) int {
	if len(x) < len(y) {
		x, y = y, x
	}
	prev := make([]int, len(y)+1)
	curr := make([]int, len(y)+1)
	for j := 0; j <= len(y); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		curr[0] = i
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			curr[j] = min(ins, min(del, sub))
		}
		prev, curr = curr, prev
	}
	return prev[len(y)]
}

// Want describes the target anime we are trying to match against candidates.
type Want struct {
	Titles    []string
	Season    int
	Year      int
	Kind      string
	Eps       int
	MalID     int
	AniListID int
}

// Candidate describes one search hit or metadata entry to be scored.
type Candidate struct {
	Titles    []string
	Season    int // 0 = infer from first title
	Year      int
	Kind      string
	Eps       int
	MalID     int
	AniListID int
	Poster    string
}

// Scored holds a candidate's index and similarity score.
type Scored struct {
	Index int
	Score float64
	Eps   int
	Year  int
}

func isMovieKind(k string) bool {
	k = strings.ToLower(strings.TrimSpace(k))
	return k == "film" || k == "movie"
}

func canonicalKind(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	switch k {
	case "film", "movie":
		return "movie"
	case "tv", "dizi", "series":
		return "tv"
	case "ova", "ona", "special", "özel":
		return k
	}
	return ""
}

// Score computes a similarity score for a candidate against Want.
func Score(candTitles []string, candSeason, candYear int, candKind string, w Want) float64 {
	return ScoreCandidate(Candidate{
		Titles: candTitles,
		Season: candSeason,
		Year:   candYear,
		Kind:   candKind,
	}, w)
}

// ScoreCandidate evaluates a full Candidate (including optional MalID, Poster, Eps) against Want.
func ScoreCandidate(c Candidate, w Want) float64 {
	// Curd signal: cover URL contains the tracker ID directly.
	if c.Poster != "" {
		if w.MalID > 0 && strings.Contains(c.Poster, "/"+strconv.Itoa(w.MalID)+".") {
			return 1.0
		}
		if w.AniListID > 0 && strings.Contains(c.Poster, strconv.Itoa(w.AniListID)) {
			return 1.0
		}
	}

	ck := canonicalKind(c.Kind)
	wk := canonicalKind(w.Kind)
	kindConflict := ck != "" && wk != "" && ck != wk && (ck == "movie" || wk == "movie")

	// Direct MAL ID match — unless TV vs Movie format conflicts (e.g. Youjo Senki TV given Movie's MAL ID).
	if c.MalID > 0 && w.MalID > 0 && c.MalID == w.MalID && !kindConflict {
		return 1.0
	}

	wSeason := w.Season
	if wSeason <= 0 && len(w.Titles) > 0 {
		_, wSeason = SplitSeason(w.Titles[0])
	}
	if wSeason <= 0 {
		wSeason = 1
	}

	best := 0.0
	for _, variant := range uniqueStrings(c.Titles) {
		candBase, candSeason := SplitSeason(variant)
		if c.Season > 0 && candSeason == 1 {
			candSeason = c.Season
		}
		variantBest := 0.0
		for _, pt := range uniqueStrings(w.Titles) {
			ptBase, _ := SplitSeason(pt)
			if ptBase == "" {
				ptBase = pt
			}
			exactRatio := Ratio(variant, pt)
			baseRatio := Ratio(candBase, ptBase)
			if candSeason > 1 && wSeason == 1 {
				baseRatio += seasonMismatchPenalty
			}
			variantBest = math.Max(variantBest, math.Max(exactRatio, baseRatio))
		}
		if wSeason > 1 {
			if candSeason == wSeason {
				variantBest += seasonBonus
			} else {
				variantBest += seasonMismatchPenalty
			}
		}
		if variantBest < containmentFloor && (candSeason == wSeason || wSeason <= 1) &&
			VariantContained(variant, candBase, w.Titles) {
			variantBest = containmentFloor
		}
		if variantBest > best {
			best = variantBest
		}
	}

	if w.Year > 0 && c.Year > 0 && w.Year == c.Year {
		best += yearBonus
	}
	if ck != "" && wk != "" {
		if ck == wk {
			best += formatBonus
		} else if kindConflict {
			best += formatMismatchPenalty
		}
	}

	// anime-sdk border zone episode count validation: if score is in [0.73, 0.80)
	// and episode counts match within ±3, accept it.
	if best >= BorderScore && best < MinScore && w.Eps > 0 && c.Eps > 0 {
		diff := w.Eps - c.Eps
		if diff < 0 {
			diff = -diff
		}
		if diff <= 3 {
			best = MinScore + 0.02
		}
	}

	return best
}

// VariantContained reports whether tokens of variant/candBase are a valid subset
// of any provider title (or vice versa), preventing trivial 1-word short prefix false positives.
func VariantContained(variant, candBase string, providerTitles []string) bool {
	vTokens := strings.Fields(Normalize(variant))
	bTokens := strings.Fields(Normalize(candBase))
	for _, pt := range providerTitles {
		ptNorm := Normalize(pt)
		ptTokens := strings.Fields(ptNorm)
		ptBase, _ := SplitSeason(pt)
		ptBaseTokens := strings.Fields(ptBase)

		if len(vTokens) >= 2 && isSubset(vTokens, ptTokens) {
			return true
		}
		if len(bTokens) >= 2 && isSubset(bTokens, ptBaseTokens) {
			return true
		}
		if isValidQuerySubset(ptTokens, vTokens) {
			return true
		}
		if isValidQuerySubset(ptBaseTokens, bTokens) {
			return true
		}
	}
	return false
}

func isValidQuerySubset(queryTokens, candTokens []string) bool {
	if len(queryTokens) == 0 || len(candTokens) == 0 || len(queryTokens) > len(candTokens) {
		return false
	}
	if len(queryTokens) == 1 && len([]rune(queryTokens[0])) < 5 {
		return false
	}
	return isSubset(queryTokens, candTokens)
}

func isSubset(small, big []string) bool {
	if len(small) == 0 || len(big) == 0 || len(small) > len(big) {
		return false
	}
	set := make(map[string]bool, len(big))
	for _, w := range big {
		set[w] = true
	}
	for _, w := range small {
		if !set[w] {
			return false
		}
	}
	return true
}

// Pick selects the best candidate index from a list of Scored candidates.
// Returns ok=false if the top score is below MinScore or if the top two candidates
// are within AmbiguityGap (unless broken by a unique maximum episode count).
func Pick(scores []Scored) (idx int, ok bool, ambiguous bool) {
	if len(scores) == 0 {
		return -1, false, true
	}
	ranked := append([]Scored(nil), scores...)
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})
	top := ranked[0]
	if top.Score < MinScore {
		return top.Index, false, true
	}
	if len(ranked) > 1 && top.Score-ranked[1].Score < AmbiguityGap {
		// Check exact tie-break by episode count then year (Migurdex rule)
		var tied []Scored
		for _, s := range ranked {
			if top.Score-s.Score < exactTieEpsilon {
				tied = append(tied, s)
			} else {
				break
			}
		}
		if len(tied) > 1 {
			sort.SliceStable(tied, func(i, j int) bool {
				if tied[i].Eps != tied[j].Eps {
					return tied[i].Eps > tied[j].Eps
				}
				return tied[i].Year > tied[j].Year
			})
			winner := tied[0]
			if winner.Eps > 0 {
				uniqueMax := true
				for _, x := range tied[1:] {
					if x.Eps >= winner.Eps {
						uniqueMax = false
						break
					}
				}
				if uniqueMax {
					return winner.Index, true, false
				}
			}
		}
		return top.Index, false, true
	}
	return top.Index, true, false
}

// BuildQueries produces search query strings from a list of known titles:
// the first 2 distinct titles, their season-stripped base titles (>= 3 chars),
// and the longest word (>= 5 chars).
func BuildQueries(titles []string) []string {
	distinct := uniqueStrings(titles)
	var out []string
	seen := map[string]bool{}
	add := func(q string) {
		q = strings.TrimSpace(q)
		k := Fold(q)
		if q != "" && !seen[k] {
			seen[k] = true
			out = append(out, q)
		}
	}
	limit := min(2, len(distinct))
	for _, t := range distinct[:limit] {
		add(t)
		base, _ := SplitSeason(t)
		if len([]rune(base)) >= 3 {
			add(base)
		}
	}
	longest := ""
	for _, t := range distinct {
		for _, w := range strings.Fields(Normalize(t)) {
			if len([]rune(w)) >= 5 && len(w) > len(longest) {
				longest = w
			}
		}
	}
	if longest != "" {
		add(longest)
	}
	return out
}

// TitleKeys returns normalized keys for every title variant of an anime.
func TitleKeys(a source.Anime) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range a.AllTitles() {
		k := Normalize(t)
		if len(k) >= 3 && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// SameAnime reports whether two source.Anime records represent the same work
// across providers, respecting MAL ID, movie/TV distinction, and normalized titles.
func SameAnime(x, y source.Anime) bool {
	// Movie vs TV never merge even if a provider mis-tagged the MAL ID (e.g. Youjo Senki).
	if x.Kind != "" && y.Kind != "" && x.IsMovie() != y.IsMovie() {
		return false
	}
	if x.MalID > 0 && y.MalID > 0 {
		return x.MalID == y.MalID
	}
	ky := map[string]bool{}
	for _, k := range TitleKeys(y) {
		ky[k] = true
	}
	for _, k := range TitleKeys(x) {
		if ky[k] {
			return true
		}
	}
	return false
}

func uniqueStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		k := Fold(x)
		if !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}
