// Package choose implements rule-based host and quality selection for episodes.
//
// Design adapted from Migurdex's SourceSelector (GPL-3.0). Pure package with no I/O.
package choose

import (
	"fmt"
	"sort"
	"strings"
)

// Rules defines the user's preferred quality and host ordering and auto-selection timing.
type Rules struct {
	QualityOrder     []string `json:"quality_order,omitempty"`
	HostOrder        []string `json:"host_order,omitempty"`
	NeverHosts       []string `json:"never_hosts,omitempty"`
	OnlyHosts        []string `json:"only_hosts,omitempty"`
	WaitAfterFirstMs int      `json:"wait_after_first_ms,omitempty"`
}

// DefaultHosts is the default host priority order.
var DefaultHosts = []string{"Tau Video", "AniHub HLS", "Google Drive", "OK.ru", "Sibnet", "Voe", "Vidmoly"}

// DefaultQualities is the default quality priority order.
var DefaultQualities = []string{"1080p", "720p", "480p", "360p"}

// DefaultRules returns sensible defaults, factoring in a legacy single-string quality setting.
func DefaultRules(prefQuality string) Rules {
	qo := append([]string(nil), DefaultQualities...)
	switch strings.ToLower(strings.TrimSpace(prefQuality)) {
	case "720p":
		qo = []string{"720p", "480p", "1080p", "360p"}
	case "480p":
		qo = []string{"480p", "360p", "720p", "1080p"}
	}
	return Rules{
		QualityOrder: qo,
		// No host is excluded by default: Vidmoly, once skipped, has a native
		// resolver now and sits last in HostOrder, so it is only picked when
		// nothing better works. Exclusions are the user's (never_hosts).
		HostOrder:        append([]string(nil), DefaultHosts...),
		WaitAfterFirstMs: 1200,
	}
}

// Effective fills any empty fields of r with DefaultRules(prefQuality).
func (r Rules) Effective(prefQuality string) Rules {
	def := DefaultRules(prefQuality)
	if len(r.QualityOrder) == 0 {
		r.QualityOrder = def.QualityOrder
	}
	if len(r.HostOrder) == 0 {
		r.HostOrder = def.HostOrder
	}
	if r.WaitAfterFirstMs <= 0 {
		r.WaitAfterFirstMs = def.WaitAfterFirstMs
	}
	return r
}

// Candidate represents a resolved quality option on a specific host.
type Candidate struct {
	Host         string
	Fansub       string
	QualityLabel string
	Height       int
	Kind         string
	Bitrate      int64
	Broken       bool
	VariantIdx   int
	QualityIdx   int
}

func labelOf(c Candidate) string {
	if c.Height > 0 {
		return fmt.Sprintf("%dp", c.Height)
	}
	if c.QualityLabel != "" {
		return c.QualityLabel
	}
	return "auto"
}

// rank returns a higher score for items appearing earlier in order; 0 if not found.
func rank(v string, order []string) int {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return 0
	}
	for i, o := range order {
		o = strings.ToLower(strings.TrimSpace(o))
		if o != "" && strings.Contains(v, o) {
			return len(order) - i
		}
	}
	return 0
}

func matchesAny(v string, list []string) bool {
	return rank(v, list) > 0
}

func filterPool(cs []Candidate, r Rules) []Candidate {
	var healthy []Candidate
	for _, c := range cs {
		if !c.Broken {
			healthy = append(healthy, c)
		}
	}
	if len(healthy) == 0 {
		return nil
	}
	var pool []Candidate
	for _, c := range healthy {
		if len(r.OnlyHosts) > 0 && !matchesAny(c.Host, r.OnlyHosts) {
			continue
		}
		if len(r.NeverHosts) > 0 && matchesAny(c.Host, r.NeverHosts) {
			continue
		}
		pool = append(pool, c)
	}
	if len(pool) > 0 {
		return pool
	}
	return healthy
}

// Best returns the highest-ranked candidate according to Rules.
func Best(cs []Candidate, r Rules) (Candidate, bool) {
	pool := filterPool(cs, r)
	if len(pool) == 0 {
		return Candidate{}, false
	}
	sort.SliceStable(pool, func(i, j int) bool {
		a, b := pool[i], pool[j]
		if x, y := rank(labelOf(a), r.QualityOrder), rank(labelOf(b), r.QualityOrder); x != y {
			return x > y
		}
		if x, y := rank(a.Host, r.HostOrder), rank(b.Host, r.HostOrder); x != y {
			return x > y
		}
		if a.Height != b.Height {
			return a.Height > b.Height
		}
		return a.Bitrate > b.Bitrate
	})
	return pool[0], true
}

// Exact reports whether a candidate matches BOTH the #1 preferred quality
// and the #1 preferred host (and is not excluded by NeverHosts/OnlyHosts),
// allowing auto-selection to start immediately without waiting for slower hosts.
func Exact(c Candidate, r Rules) bool {
	if c.Broken {
		return false
	}
	if len(r.OnlyHosts) > 0 && !matchesAny(c.Host, r.OnlyHosts) {
		return false
	}
	if len(r.NeverHosts) > 0 && matchesAny(c.Host, r.NeverHosts) {
		return false
	}
	if len(r.QualityOrder) > 0 {
		topQ := strings.ToLower(strings.TrimSpace(r.QualityOrder[0]))
		lbl := strings.ToLower(labelOf(c))
		if topQ != "best" && topQ != "auto" && !strings.Contains(lbl, topQ) {
			return false
		}
	}
	if len(r.HostOrder) > 0 {
		topH := strings.ToLower(strings.TrimSpace(r.HostOrder[0]))
		if !strings.Contains(strings.ToLower(c.Host), topH) {
			return false
		}
	}
	return true
}
