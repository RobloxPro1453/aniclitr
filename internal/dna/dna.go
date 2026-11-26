// Package dna computes a pure-local, zero-AI mathematical "Taste DNA" profile
// and Studio/Director/Seiyuu discovery graph from the user's library and watch
// history.
package dna

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"aniclitr/internal/store"
)

// Affinity ranks one dimension (genre, decade, studio, director, seiyuu) using
// Bayesian shrinkage so a single 10/10 doesn't distort genuine taste patterns.
type Affinity struct {
	Name       string  `json:"name"`
	Count      int     `json:"count"`
	AvgScore   float64 `json:"avg_score"`
	BayesScore float64 `json:"bayes_score"` // prior-weighted affinity score
	Weight     float64 `json:"weight"`      // 0..1 normalized preference share
}

// StaffCredit attaches studio/director/seiyuu metadata to a library item.
type StaffCredit struct {
	MalID     int
	Studios   []string
	Directors []string
	Seiyuu    []string
}

// Profile is the complete local Taste DNA report.
type Profile struct {
	TotalTitles    int
	RatedTitles    int
	MeanScore      float64
	StdDevScore    float64
	CompletionRate float64 // 0..1
	DropRate       float64 // 0..1
	CriticStyle    string  // "Seçici Eleştirmen", "Dengeli İzleyici", "Cömert Hayran"
	TopGenres      []Affinity
	TopDecades     []Affinity
	TopStudios     []Affinity
	TopDirectors   []Affinity
	TopSeiyuu      []Affinity
	SignatureLine  string
}

// Build computes the user's Taste DNA purely from local mathematics (zero AI).
func Build(items []store.Item, credits map[int]StaffCredit) Profile {
	var p Profile
	p.TotalTitles = len(items)
	if len(items) == 0 {
		p.CriticStyle = "Henüz Keşif Aşamasında"
		p.SignatureLine = "Kitaplığınıza birkaç anime ekleyip puanladığınızda izleme profiliniz otomatik olarak oluşur."
		return p
	}

	var scores []float64
	completed, dropped := 0, 0
	for _, it := range items {
		if it.Score > 0 {
			scores = append(scores, float64(it.Score))
		}
		if it.Status == store.Completed {
			completed++
		} else if it.Status == store.Dropped {
			dropped++
		}
	}
	p.RatedTitles = len(scores)
	p.CompletionRate = float64(completed) / float64(len(items))
	p.DropRate = float64(dropped) / float64(len(items))

	prior := 7.2
	if len(scores) > 0 {
		sum := 0.0
		for _, s := range scores {
			sum += s
		}
		p.MeanScore = sum / float64(len(scores))
		prior = p.MeanScore
		var sq float64
		for _, s := range scores {
			d := s - p.MeanScore
			sq += d * d
		}
		p.StdDevScore = math.Sqrt(sq / float64(len(scores)))
	}

	switch {
	case p.RatedTitles < 3:
		p.CriticStyle = "Yeni Kâşif"
	case p.MeanScore < 6.8:
		p.CriticStyle = "Seçici Eleştirmen"
	case p.MeanScore > 8.4:
		p.CriticStyle = "Tutkulu Hayran"
	default:
		p.CriticStyle = "Dengeli Sinefil"
	}

	genreAcc := map[string]*acc{}
	decadeAcc := map[string]*acc{}
	studioAcc := map[string]*acc{}
	dirAcc := map[string]*acc{}
	seiAcc := map[string]*acc{}

	for _, it := range items {
		sc := float64(it.Score)
		for _, g := range it.Genres {
			addAcc(genreAcc, strings.TrimSpace(g), sc)
		}
		if it.Year >= 1960 && it.Year <= 2035 {
			dec := fmt.Sprintf("%d'ler", (it.Year/10)*10)
			addAcc(decadeAcc, dec, sc)
		}
		if cr, ok := credits[it.MalID]; ok {
			for _, s := range cr.Studios {
				addAcc(studioAcc, s, sc)
			}
			for _, d := range cr.Directors {
				addAcc(dirAcc, d, sc)
			}
			for _, v := range cr.Seiyuu {
				addAcc(seiAcc, v, sc)
			}
		}
	}

	p.TopGenres = rankAffinity(genreAcc, prior, 8)
	p.TopDecades = rankAffinity(decadeAcc, prior, 5)
	p.TopStudios = rankAffinity(studioAcc, prior, 6)
	p.TopDirectors = rankAffinity(dirAcc, prior, 6)
	p.TopSeiyuu = rankAffinity(seiAcc, prior, 6)

	var sigParts []string
	if len(p.TopGenres) >= 2 {
		sigParts = append(sigParts, fmt.Sprintf("%s & %s odaklı", p.TopGenres[0].Name, p.TopGenres[1].Name))
	} else if len(p.TopGenres) == 1 {
		sigParts = append(sigParts, p.TopGenres[0].Name+" odaklı")
	}
	if len(p.TopDecades) > 0 {
		sigParts = append(sigParts, p.TopDecades[0].Name+" dönemi favori")
	}
	if len(p.TopStudios) > 0 {
		sigParts = append(sigParts, "stüdyo: "+p.TopStudios[0].Name)
	}
	if len(sigParts) == 0 {
		p.SignatureLine = p.CriticStyle
	} else {
		p.SignatureLine = p.CriticStyle + " · " + strings.Join(sigParts, " · ")
	}
	return p
}

// MatchCandidate scores an unwatched anime against the user's Taste DNA (0..100)
// using cosine-like weighted affinity overlap across genres, decade, and studios.
func (p Profile) MatchCandidate(genres []string, year int, studios []string) int {
	if len(p.TopGenres) == 0 {
		return 50
	}
	gMap := map[string]float64{}
	for _, g := range p.TopGenres {
		gMap[strings.ToLower(g.Name)] = g.Weight
	}
	sMap := map[string]float64{}
	for _, s := range p.TopStudios {
		sMap[strings.ToLower(s.Name)] = s.Weight
	}
	dMap := map[string]float64{}
	for _, d := range p.TopDecades {
		dMap[d.Name] = d.Weight
	}

	score := 35.0
	for _, g := range genres {
		if w, ok := gMap[strings.ToLower(strings.TrimSpace(g))]; ok {
			score += w * 45.0
		}
	}
	for _, s := range studios {
		if w, ok := sMap[strings.ToLower(strings.TrimSpace(s))]; ok {
			score += w * 25.0
		}
	}
	if year >= 1960 {
		dec := fmt.Sprintf("%d'ler", (year/10)*10)
		if w, ok := dMap[dec]; ok {
			score += w * 15.0
		}
	}
	if score > 99 {
		score = 99
	}
	return int(math.Round(score))
}

type acc struct {
	count int
	rated int
	sum   float64
}

func addAcc(m map[string]*acc, key string, score float64) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	a, ok := m[key]
	if !ok {
		a = &acc{}
		m[key] = a
	}
	a.count++
	if score > 0 {
		a.rated++
		a.sum += score
	}
}

func rankAffinity(m map[string]*acc, prior float64, limit int) []Affinity {
	if len(m) == 0 {
		return nil
	}
	const priorWeight = 2.0
	out := make([]Affinity, 0, len(m))
	maxSignal := 0.0
	for k, a := range m {
		avg := prior
		if a.rated > 0 {
			avg = a.sum / float64(a.rated)
		}
		bayes := (a.sum + prior*priorWeight) / (float64(a.rated) + priorWeight)
		// Signal combines Bayesian quality with log-frequency engagement
		sig := bayes * (1.0 + 0.35*math.Log1p(float64(a.count)))
		if sig > maxSignal {
			maxSignal = sig
		}
		out = append(out, Affinity{
			Name:       k,
			Count:      a.count,
			AvgScore:   math.Round(avg*10) / 10,
			BayesScore: math.Round(bayes*100) / 100,
			Weight:     sig,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		if maxSignal > 0 {
			out[i].Weight = math.Round((out[i].Weight/maxSignal)*100) / 100
		}
	}
	return out
}
