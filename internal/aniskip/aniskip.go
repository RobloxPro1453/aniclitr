// Package aniskip fetches crowd-sourced opening/ending timestamps.
package aniskip

import (
	"context"
	"fmt"

	"aniclitr/internal/netx"
)

// Segment is a skippable interval in seconds.
type Segment struct {
	Kind  string // "op", "ed", "mixed-op", "mixed-ed", "recap"
	Start float64
	End   float64
	EpLen float64 // episode length the timestamps were submitted for
}

func family(kind string) string {
	switch kind {
	case "op", "mixed-op":
		return "op"
	case "ed", "mixed-ed":
		return "ed"
	}
	return kind
}

// ForDuration keeps one segment per family (op/ed/recap): the one submitted
// for the episode length closest to the real duration.
func ForDuration(segs []Segment, dur float64) []Segment {
	best := map[string]Segment{}
	var order []string
	for _, s := range segs {
		f := family(s.Kind)
		cur, ok := best[f]
		if !ok {
			order = append(order, f)
			best[f] = s
			continue
		}
		if dur > 0 && s.EpLen > 0 && abs(s.EpLen-dur) < abs(cur.EpLen-dur) {
			best[f] = s
		} else if dur <= 0 && cur.Kind != f && s.Kind == f { // prefer plain op/ed
			best[f] = s
		}
	}
	out := make([]Segment, 0, len(order))
	for _, f := range order {
		s := best[f]
		if dur > 0 && s.End > dur+5 {
			continue // timestamps don't fit this cut at all
		}
		out = append(out, s)
	}
	return out
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Label is a Turkish display name for the segment.
func (s Segment) Label() string {
	switch s.Kind {
	case "op", "mixed-op":
		return "Opening"
	case "ed", "mixed-ed":
		return "Ending"
	case "recap":
		return "Özet"
	}
	return s.Kind
}

// Fetch returns segments for a MAL id + episode number; nil if unknown.
func Fetch(ctx context.Context, malID, episode int) ([]Segment, error) {
	if malID <= 0 || episode <= 0 {
		return nil, nil
	}
	url := fmt.Sprintf("https://api.aniskip.com/v2/skip-times/%d/%d?types[]=op&types[]=ed&types[]=mixed-op&types[]=mixed-ed&types[]=recap&episodeLength=0", malID, episode)
	var res struct {
		Found   bool `json:"found"`
		Results []struct {
			SkipType string `json:"skipType"`
			Interval struct {
				Start float64 `json:"startTime"`
				End   float64 `json:"endTime"`
			} `json:"interval"`
			EpisodeLength float64 `json:"episodeLength"`
		} `json:"results"`
	}
	if err := netx.GetJSON(ctx, url, nil, &res); err != nil {
		return nil, err
	}
	var out []Segment
	for _, r := range res.Results {
		if r.Interval.End > r.Interval.Start {
			out = append(out, Segment{Kind: r.SkipType, Start: r.Interval.Start, End: r.Interval.End, EpLen: r.EpisodeLength})
		}
	}
	return out, nil
}
