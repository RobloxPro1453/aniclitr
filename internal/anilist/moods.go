package anilist

import (
	"context"
	"time"
)

// MoodPreset is one curated "Ne İzlesem?" mood filter for Keşfet.
type MoodPreset struct {
	ID      string
	Icon    string
	Title   string
	Desc    string
	Genres  []string
	MaxEps  int
	MinPop  int
	MaxPop  int
	MinRate int
	Done    bool
}

// MoodPresets lists the 6 curated mood picks in Keşfet.
var MoodPresets = []MoodPreset{
	{ID: "kisa", Icon: "⚡", Title: "Kısa & Akıcı", Desc: "Bitmiş, 13 bölüm ve altı, yüksek puanlı seriler", MaxEps: 13, MinRate: 76, Done: true},
	{ID: "cevher", Icon: "◈", Title: "Gizli Cevherler", Desc: "8.0+ puanlı ama herkesin bilmediği kaliteli yapımlar", MinRate: 80, MaxPop: 140000, Done: true},
	{ID: "aksiyon", Icon: "▸", Title: "Saf Aksiyon", Desc: "Temposu hiç düşmeyen adrenalin ve dövüş serileri", Genres: []string{"Action"}, MinRate: 78},
	{ID: "dram", Icon: "☁", Title: "Ağlatan Dram", Desc: "Derin hikâyeli, duygusal ve unutulmaz yapımlar", Genres: []string{"Drama"}, MinRate: 80},
	{ID: "sakin", Icon: "☕", Title: "Kafa Boşaltmalık", Desc: "Sıcak, rahatlatıcı komedi ve günlük yaşam serileri", Genres: []string{"Slice of Life", "Comedy"}, MinRate: 75},
	{ID: "gizem", Icon: "❖", Title: "Zihin Yakan Gizem", Desc: "Gerilim, psikolojik ve sürpriz sonlu gizem serileri", Genres: []string{"Mystery", "Psychological", "Thriller"}, MinRate: 78},
}

// ByMood fetches AniList titles matching the given mood preset.
func ByMood(ctx context.Context, preset MoodPreset) ([]Media, error) {
	vars := map[string]any{"score": max(60, preset.MinRate)}
	var conds []string
	conds = append(conds, "type:ANIME", "isAdult:false", "averageScore_greater:$score")
	sig := "query($score:Int"
	if len(preset.Genres) > 0 {
		sig += ",$genres:[String]"
		conds = append(conds, "genre_in:$genres")
		vars["genres"] = preset.Genres
	}
	if preset.MaxEps > 0 {
		sig += ",$maxEps:Int"
		conds = append(conds, "episodes_lesser:$maxEps", "episodes_greater:2")
		vars["maxEps"] = preset.MaxEps + 1
	}
	if preset.MaxPop > 0 {
		sig += ",$maxPop:Int"
		conds = append(conds, "popularity_lesser:$maxPop", "popularity_greater:12000")
		vars["maxPop"] = preset.MaxPop
	}
	if preset.Done {
		conds = append(conds, "status:FINISHED")
	}
	q := sig + `){Page(page:1,perPage:36){media(` + joinComma(conds) + `,sort:[SCORE_DESC,POPULARITY_DESC]){` + mediaFields + `}}}`
	var res struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	if err := query(ctx, q, vars, 6*time.Hour, &res); err != nil {
		return nil, err
	}
	var out []Media
	for _, m := range res.Page.Media {
		if keep(m) {
			out = append(out, m)
		}
	}
	return out, nil
}

func joinComma(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	out := xs[0]
	for _, x := range xs[1:] {
		out += "," + x
	}
	return out
}
