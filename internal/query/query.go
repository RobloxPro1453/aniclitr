// Package query implements the ctrl+k 2.0 universal structured query language
// and dynamic Smart Playlists engine (e.g. "puan>=8 durum:bitti tur:psikolojik",
// "yıl:2000..2012 stüdyo:madhouse", "eksik:disk").
package query

import (
	"strconv"
	"strings"

	"aniclitr/internal/config"
	"aniclitr/internal/localmedia"
	"aniclitr/internal/match"
	"aniclitr/internal/store"
)

// Record is a normalized searchable library item enriched with local disk and
// watch-history metadata.
type Record struct {
	Item       store.Item
	HasLocal   bool
	MissingDisk bool
	Studios    []string
	Notes      string
	EpAvgScore float64
}

// Predicate is one parsed filter clause.
type Predicate struct {
	Field   string // "", "puan", "durum", "tur", "yil", "bolum", "studyo", "kaynak", "liste", "eksik", "disk", "mal"
	Op      string // ":", "=", ">=", "<=", ">", "<", ".."
	Val     string
	Num     float64
	NumHi   float64
	Negated bool
}

// Parsed represents a compiled query.
type Parsed struct {
	Raw        string
	Predicates []Predicate
}

// DefaultSmartLists returns built-in dynamic Smart Playlists.
func DefaultSmartLists() []config.SmartList {
	return []config.SmartList{
		{Name: "Başyapıtlar (9+)", Query: "puan>=9"},
		{Name: "Kısa ve Öz (≤13 bölüm)", Query: "bolum<=13 durum:bitti"},
		{Name: "Puanlanmamış Bitenler", Query: "durum:bitti puan=0"},
		{Name: "Diskte Hazır", Query: "disk:hazir"},
		{Name: "Eksik Disk Arşivi", Query: "eksik:disk"},
	}
}

// Parse compiles a user query string into structured Predicates.
func Parse(raw string) Parsed {
	p := Parsed{Raw: strings.TrimSpace(raw)}
	tokens := splitTokens(p.Raw)
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		neg := false
		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			neg = true
			tok = tok[1:]
		}
		if pred, ok := parseComparison(tok, neg); ok {
			p.Predicates = append(p.Predicates, pred)
			continue
		}
		p.Predicates = append(p.Predicates, Predicate{
			Field:   "",
			Op:      "text",
			Val:     match.Fold(tok),
			Negated: neg,
		})
	}
	return p
}

func splitTokens(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func parseComparison(tok string, neg bool) (Predicate, bool) {
	ops := []string{">=", "<=", ">", "<", "=", ":"}
	bestPos := -1
	bestOp := ""
	for _, op := range ops {
		if i := strings.Index(tok, op); i > 0 {
			if bestPos == -1 || i < bestPos || (i == bestPos && len(op) > len(bestOp)) {
				bestPos = i
				bestOp = op
			}
		}
	}
	if bestPos <= 0 {
		return Predicate{}, false
	}
	field := normalizeField(tok[:bestPos])
	if field == "" {
		return Predicate{}, false
	}
	val := strings.TrimSpace(tok[bestPos+len(bestOp):])
	pr := Predicate{Field: field, Op: bestOp, Val: match.Fold(val), Negated: neg}

	// Support range syntax e.g. yıl:2000..2012 or puan:7..9
	if strings.Contains(val, "..") {
		parts := strings.SplitN(val, "..", 2)
		lo, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		hi, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 == nil && err2 == nil {
			pr.Op = ".."
			pr.Num, pr.NumHi = lo, hi
			return pr, true
		}
	}
	if f, err := strconv.ParseFloat(strings.ReplaceAll(val, ",", "."), 64); err == nil {
		pr.Num = f
		if pr.Op == ":" {
			pr.Op = "="
		}
	}
	return pr, true
}

func normalizeField(raw string) string {
	f := match.Fold(raw)
	switch f {
	case "puan", "score", "rating":
		return "puan"
	case "durum", "status":
		return "durum"
	case "tur", "genre", "kategori":
		return "tur"
	case "yil", "year":
		return "yil"
	case "bolum", "ep", "eps", "episodes":
		return "bolum"
	case "studyo", "studio":
		return "studyo"
	case "kaynak", "source":
		return "kaynak"
	case "liste", "list", "raf":
		return "liste"
	case "eksik", "missing":
		return "eksik"
	case "disk", "yerel", "local":
		return "disk"
	case "mal":
		return "mal"
	}
	return ""
}

// Match reports whether rec satisfies every predicate in p.
func (p Parsed) Match(rec Record) bool {
	for _, pr := range p.Predicates {
		ok := evalPredicate(pr, rec)
		if pr.Negated {
			ok = !ok
		}
		if !ok {
			return false
		}
	}
	return true
}

func evalPredicate(pr Predicate, rec Record) bool {
	it := rec.Item
	switch pr.Field {
	case "":
		hay := match.Fold(it.Title + " " + rec.Notes + " " + strings.Join(it.Genres, " ") + " " + strings.Join(it.Lists, " ") + " " + strings.Join(rec.Studios, " "))
		return strings.Contains(hay, pr.Val)
	case "puan":
		if pr.Val == "yok" || pr.Val == "none" {
			return it.Score == 0
		}
		if pr.Val == "var" {
			return it.Score > 0
		}
		sc := float64(it.Score)
		if sc == 0 && rec.EpAvgScore > 0 {
			sc = rec.EpAvgScore
		}
		return cmpNum(sc, pr.Op, pr.Num, pr.NumHi)
	case "yil":
		if it.Year == 0 {
			return false
		}
		return cmpNum(float64(it.Year), pr.Op, pr.Num, pr.NumHi)
	case "bolum":
		n := it.EpCount
		if n == 0 {
			n = it.Watched
		}
		return cmpNum(float64(n), pr.Op, pr.Num, pr.NumHi)
	case "durum":
		return matchStatus(it.Status, pr.Val)
	case "tur":
		for _, g := range it.Genres {
			if strings.Contains(match.Fold(g), pr.Val) {
				return true
			}
		}
		return false
	case "studyo":
		for _, s := range rec.Studios {
			if strings.Contains(match.Fold(s), pr.Val) {
				return true
			}
		}
		return false
	case "kaynak":
		return strings.Contains(match.Fold(it.Source), pr.Val)
	case "liste":
		for _, l := range it.Lists {
			if strings.Contains(match.Fold(l), pr.Val) {
				return true
			}
		}
		return false
	case "disk":
		return rec.HasLocal
	case "eksik":
		if pr.Val == "disk" || pr.Val == "" {
			return rec.MissingDisk
		}
		return it.EpCount > 0 && it.Watched < it.EpCount
	case "mal":
		if pr.Val == "yok" {
			return it.MalID == 0
		}
		return it.MalID > 0
	}
	return true
}

func cmpNum(v float64, op string, target, hi float64) bool {
	switch op {
	case "=":
		return v == target
	case ">=":
		return v >= target
	case "<=":
		return v <= target
	case ">":
		return v > target
	case "<":
		return v < target
	case "..":
		return v >= target && v <= hi
	}
	return false
}

func matchStatus(st string, want string) bool {
	switch want {
	case "bitti", "tamamlandi", "completed", "done":
		return st == store.Completed
	case "izliyor", "watching", "devam":
		return st == store.Watching
	case "bekliyor", "plan", "planlanan", "izlenecek", "ptw":
		return st == store.Planned
	case "duraklatildi", "hold", "beklemede":
		return st == store.OnHold
	case "birakti", "birakildi", "dropped":
		return st == store.Dropped
	}
	return strings.Contains(match.Fold(st), want)
}

// Filter evaluates rawQuery over library items enriched by history and localmedia.
func Filter(rawQuery string, lib *store.Library, hist *store.History, local *localmedia.Index, studiosByMal map[int][]string) []Record {
	if lib == nil {
		return nil
	}
	parsed := Parse(rawQuery)
	needsLocal := false
	for _, pr := range parsed.Predicates {
		if pr.Field == "disk" || pr.Field == "eksik" {
			needsLocal = true
			break
		}
	}
	items := lib.All()
	var out []Record
	for _, it := range items {
		rec := Record{Item: it}
		if studiosByMal != nil && it.MalID > 0 {
			rec.Studios = studiosByMal[it.MalID]
		}
		if hist != nil {
			if e, ok := hist.Get(it.Key()); ok {
				var notes []string
				for _, n := range e.Notes {
					notes = append(notes, n)
				}
				rec.Notes = strings.Join(notes, " ")
				if avg, cnt, _ := hist.EpisodeScoreStats(it.Key()); cnt > 0 {
					rec.EpAvgScore = avg
				}
			}
		}
		if local != nil && needsLocal {
			// Check whether at least episode 1 is on disk and whether any are missing
			if _, ok := local.Lookup([]string{it.Title}, 1, 1); ok {
				rec.HasLocal = true
			}
			if it.EpCount > 0 {
				miss := local.MissingEpisodes([]string{it.Title}, 1, it.EpCount)
				rec.MissingDisk = len(miss) > 0 && len(miss) < it.EpCount
				if len(miss) < it.EpCount {
					rec.HasLocal = true
				}
			}
		}
		if parsed.Match(rec) {
			out = append(out, rec)
		}
	}
	return out
}
