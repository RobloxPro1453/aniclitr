// Package router implements the personal ISP self-healing speed/quality router,
// fake-quality detector, source-agnostic unified catalog, serverless mid-play
// failover coordinator, and lightweight predictive preloader.
package router

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
)

// HostStats records real playback telemetry measured on the user's own ISP.
type HostStats struct {
	Host           string    `json:"host"`
	Plays          int       `json:"plays"`
	Failures       int       `json:"failures"`
	AvgStartupMs   float64   `json:"avg_startup_ms"`
	AvgStalls      float64   `json:"avg_stalls"`
	AvgBitrateKbps float64   `json:"avg_bitrate_kbps"`
	FakeQuality    int       `json:"fake_quality_hits"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Sample represents one measured playback session on a host.
type Sample struct {
	Host           string
	ClaimedHeight  int
	StartupMs      int64
	StallCount     int
	RealBitrateBps int64
	Failed         bool
}

// DetectFakeQuality flags streams whose actual bitrate is far below the
// claimed resolution (e.g., "1080p" streaming at 480p bitrate).
func DetectFakeQuality(claimedHeight int, realBitrateBps int64) (isFake bool, note string) {
	if claimedHeight <= 0 || realBitrateBps <= 0 {
		return false, ""
	}
	kbps := float64(realBitrateBps) / 1000.0
	switch {
	case claimedHeight >= 1080 && kbps < 950:
		return true, fmt.Sprintf("sahte 1080p (gerçek ~%.0f kbps)", kbps)
	case claimedHeight >= 720 && kbps < 450:
		return true, fmt.Sprintf("sahte 720p (gerçek ~%.0f kbps)", kbps)
	case claimedHeight >= 2160 && kbps < 3500:
		return true, fmt.Sprintf("sahte 4K (gerçek ~%.0f kbps)", kbps)
	}
	return false, ""
}

// Router tracks per-host ISP performance and scores/ranks candidate hosts.
type Router struct {
	mu    sync.RWMutex
	path  string
	hosts map[string]HostStats
}

var (
	defaultOnce sync.Once
	defaultR    *Router
	defaultDir  string
	defaultMu   sync.Mutex
)

// Default returns the process-wide router backed by CacheDir/isp-router.json.
func Default() *Router {
	dir := config.CacheDir()
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultR != nil && defaultDir == dir {
		return defaultR
	}
	defaultR = Open(filepath.Join(dir, "isp-router.json"))
	defaultDir = dir
	return defaultR
}

// Open loads or initializes a Router backed by path.
func Open(path string) *Router {
	r := &Router{
		path:  path,
		hosts: map[string]HostStats{},
	}
	if b, err := os.ReadFile(path); err == nil {
		var saved map[string]HostStats
		if json.Unmarshal(b, &saved) == nil && saved != nil {
			r.hosts = saved
		}
	}
	return r
}

func normHost(h string) string {
	return strings.ToLower(strings.TrimSpace(h))
}

// Record folds a playback telemetry sample into the host's exponential moving
// averages and persists atomically.
func (r *Router) Record(s Sample) {
	k := normHost(s.Host)
	if k == "" {
		return
	}
	r.mu.Lock()
	st := r.hosts[k]
	if st.Host == "" {
		st.Host = strings.TrimSpace(s.Host)
	}
	const alpha = 0.35
	if s.Failed {
		st.Failures++
	} else {
		st.Plays++
		if s.StartupMs > 0 {
			if st.AvgStartupMs == 0 {
				st.AvgStartupMs = float64(s.StartupMs)
			} else {
				st.AvgStartupMs = st.AvgStartupMs*(1-alpha) + float64(s.StartupMs)*alpha
			}
		}
		if st.Plays == 1 {
			st.AvgStalls = float64(s.StallCount)
		} else {
			st.AvgStalls = st.AvgStalls*(1-alpha) + float64(s.StallCount)*alpha
		}
		if s.RealBitrateBps > 0 {
			kbps := float64(s.RealBitrateBps) / 1000.0
			if st.AvgBitrateKbps == 0 {
				st.AvgBitrateKbps = kbps
			} else {
				st.AvgBitrateKbps = st.AvgBitrateKbps*(1-alpha) + kbps*alpha
			}
			if fake, _ := DetectFakeQuality(s.ClaimedHeight, s.RealBitrateBps); fake {
				st.FakeQuality++
			}
		}
	}
	st.UpdatedAt = time.Now().UTC()
	r.hosts[k] = st
	snap := make(map[string]HostStats, len(r.hosts))
	for kk, vv := range r.hosts {
		snap[kk] = vv
	}
	p := r.path
	r.mu.Unlock()

	if p != "" {
		if b, err := json.MarshalIndent(snap, "", "  "); err == nil {
			_ = config.WriteAtomic(p, b)
		}
	}
}

// Stats returns the recorded telemetry for host, if any.
func (r *Router) Stats(host string) (HostStats, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	st, ok := r.hosts[normHost(host)]
	return st, ok
}

// AllStats returns a sorted snapshot of all measured hosts (best score first).
func (r *Router) AllStats() []HostStats {
	r.mu.RLock()
	out := make([]HostStats, 0, len(r.hosts))
	for _, st := range r.hosts {
		out = append(out, st)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return r.Score(out[i].Host) > r.Score(out[j].Host)
	})
	return out
}

// Score returns a 0..100 personal ISP health & quality score for host.
// Unknown hosts receive a neutral baseline (60) so new hosts get explored.
func (r *Router) Score(host string) float64 {
	r.mu.RLock()
	st, ok := r.hosts[normHost(host)]
	r.mu.RUnlock()
	if !ok || (st.Plays == 0 && st.Failures == 0) {
		return 60
	}
	total := float64(st.Plays + st.Failures)
	// Laplace-smoothed reliability in [0..1]
	rel := (float64(st.Plays) + 1.0) / (total + 2.0)
	score := rel * 60.0

	// Startup latency bonus (0..20): <=500ms = 20, >=5000ms = 0
	if st.AvgStartupMs > 0 {
		latFrac := 1.0 - math.Min(1.0, math.Max(0, (st.AvgStartupMs-500.0)/4500.0))
		score += latFrac * 20.0
	} else {
		score += 10.0
	}

	// Bitrate authenticity bonus (0..20): >=2500kbps = 20
	if st.AvgBitrateKbps > 0 {
		brFrac := math.Min(1.0, st.AvgBitrateKbps/2500.0)
		score += brFrac * 20.0
	} else {
		score += 10.0
	}

	// Penalties for buffering stalls and fake-quality misrepresentations
	score -= math.Min(25.0, st.AvgStalls*8.0)
	if st.Plays > 0 && st.FakeQuality > 0 {
		fakeRatio := float64(st.FakeQuality) / float64(st.Plays)
		score -= math.Min(30.0, fakeRatio*30.0)
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// RankHosts orders hosts by combining user preference with personal ISP scores.
// Explicit userPreferred hosts receive a modest tie-breaker boost (+12) while
// severely degraded or fake-quality hosts are automatically demoted.
func (r *Router) RankHosts(hosts []string, userPreferred []string) []string {
	prefBoost := map[string]float64{}
	for i, h := range userPreferred {
		k := normHost(h)
		if k != "" {
			prefBoost[k] = math.Max(2, 12.0-float64(i)*2.0)
		}
	}
	out := append([]string(nil), hosts...)
	sort.SliceStable(out, func(i, j int) bool {
		si := r.Score(out[i]) + prefBoost[normHost(out[i])]
		sj := r.Score(out[j]) + prefBoost[normHost(out[j])]
		return si > sj
	})
	return out
}

// SummaryBadge returns a concise Turkish badge for a host's ISP record, e.g.
// "1,1 sn · 2,4 Mbps" or "sahte 1080p uyarısı".
func (r *Router) SummaryBadge(host string) string {
	st, ok := r.Stats(host)
	if !ok || st.Plays == 0 {
		return ""
	}
	var parts []string
	if st.AvgStartupMs > 0 {
		parts = append(parts, fmt.Sprintf("%.1f sn", st.AvgStartupMs/1000.0))
	}
	if st.AvgBitrateKbps >= 1000 {
		parts = append(parts, fmt.Sprintf("%.1f Mbps", st.AvgBitrateKbps/1000.0))
	} else if st.AvgBitrateKbps > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kbps", st.AvgBitrateKbps))
	}
	if st.FakeQuality > 0 && float64(st.FakeQuality)/float64(st.Plays) >= 0.5 {
		parts = append(parts, "sahte kalite")
	}
	return strings.Join(parts, " · ")
}
