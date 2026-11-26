package router

import (
	"path/filepath"
	"testing"
	"time"

	"aniclitr/internal/source"
)

func TestDetectFakeQuality(t *testing.T) {
	if fake, note := DetectFakeQuality(1080, 600_000); !fake || note == "" {
		t.Fatalf("expected fake 1080p at 600kbps, got fake=%v note=%q", fake, note)
	}
	if fake, _ := DetectFakeQuality(1080, 3_200_000); fake {
		t.Fatalf("3.2 Mbps 1080p must not be flagged fake")
	}
	if fake, _ := DetectFakeQuality(720, 300_000); !fake {
		t.Fatalf("300 kbps 720p must be flagged fake")
	}
}

func TestRouterLearnsISPPerformanceAndPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "isp-router.json")
	r := Open(p)

	// Fast, genuine 1080p host
	for i := 0; i < 4; i++ {
		r.Record(Sample{Host: "Tau Video", ClaimedHeight: 1080, StartupMs: 650, StallCount: 0, RealBitrateBps: 3_500_000})
	}
	// Slow, stalling, fake 1080p host
	for i := 0; i < 4; i++ {
		r.Record(Sample{Host: "FakeHost", ClaimedHeight: 1080, StartupMs: 4200, StallCount: 3, RealBitrateBps: 500_000})
	}

	if r.Score("Tau Video") <= r.Score("FakeHost")+30 {
		t.Fatalf("expected Tau Video (%.1f) >> FakeHost (%.1f)", r.Score("Tau Video"), r.Score("FakeHost"))
	}
	ranked := r.RankHosts([]string{"FakeHost", "Tau Video"}, []string{"FakeHost"})
	if ranked[0] != "Tau Video" {
		t.Fatalf("ISP router should demote slow fake host even with user preference, got %v", ranked)
	}
	if badge := r.SummaryBadge("FakeHost"); badge == "" {
		t.Fatalf("expected non-empty summary badge")
	}

	// Reload from disk and verify persistence
	r2 := Open(p)
	st, ok := r2.Stats("Tau Video")
	if !ok || st.Plays != 4 {
		t.Fatalf("reload stats failed: ok=%v st=%+v", ok, st)
	}
}

func TestMergeCatalogAndServerlessFailover(t *testing.T) {
	r := Open(filepath.Join(t.TempDir(), "r.json"))
	r.Record(Sample{Host: "FastCDN", ClaimedHeight: 1080, StartupMs: 500, RealBitrateBps: 3_000_000})
	r.Record(Sample{Host: "SlowCDN", ClaimedHeight: 1080, StartupMs: 4500, StallCount: 2, RealBitrateBps: 800_000})

	items := MergeCatalog("frieren", map[string][]source.Anime{
		"animecix": {{Source: "animecix", ID: "1", Title: "Sousou no Frieren", MalID: 52991, Year: 2023}},
		"animetr":  {{Source: "animetr", ID: "2", Title: "Frieren: Beyond Journey's End", MalID: 52991, Year: 2023}},
	}, r)
	if len(items) != 1 {
		t.Fatalf("expected 1 canonical merged item by MalID, got %d", len(items))
	}
	if len(items[0].Offers) != 2 {
		t.Fatalf("expected 2 offers, got %d", len(items[0].Offers))
	}

	cands := []FailoverCandidate{
		{Host: "SlowCDN", Stream: &source.Stream{}, Quality: source.Quality{URL: "https://slow/v.mp4", Height: 1080}},
		{Host: "FastCDN", Stream: &source.Stream{}, Quality: source.Quality{URL: "https://fast/v.mp4", Height: 1080}},
	}
	pick, ok := PickFailover(cands, map[string]bool{"https://dead/v.mp4": true}, r)
	if !ok || pick.Host != "FastCDN" {
		t.Fatalf("PickFailover = %+v (ok=%v), want FastCDN", pick, ok)
	}
	// If FastCDN fails, seamlessly select SlowCDN
	pick2, ok := PickFailover(cands, map[string]bool{"https://fast/v.mp4": true}, r)
	if !ok || pick2.Host != "SlowCDN" {
		t.Fatalf("second failover = %+v (ok=%v), want SlowCDN", pick2, ok)
	}
}

func TestPreloadCacheTTLAndBound(t *testing.T) {
	c := NewPreloadCache(50 * time.Millisecond)
	st := &source.Stream{Qualities: []source.Quality{{URL: "https://cdn/1.mp4", Height: 1080}}}
	c.Put(PreloadEntry{AnimeKey: "animecix:1", Season: 1, Episode: 5, Stream: st, Quality: st.Qualities[0]})
	if got, ok := c.Get("animecix:1", 1, 5); !ok || got.Quality.URL != "https://cdn/1.mp4" {
		t.Fatalf("preload hit failed: %+v %v", got, ok)
	}
	time.Sleep(70 * time.Millisecond)
	if _, ok := c.Get("animecix:1", 1, 5); ok {
		t.Fatalf("expected entry to expire after TTL")
	}
}
