package choose

import "testing"

func TestBestAndExactSelection(t *testing.T) {
	r := DefaultRules("1080p")

	cs := []Candidate{
		{Host: "Sibnet", Height: 720, Bitrate: 2_000_000, VariantIdx: 0, QualityIdx: 0},
		{Host: "Vidmoly", Height: 1080, Bitrate: 5_000_000, VariantIdx: 1, QualityIdx: 0}, // last in HostOrder: loses despite the higher bitrate
		{Host: "Google Drive", Height: 1080, Bitrate: 4_000_000, VariantIdx: 2, QualityIdx: 0},
		{Host: "Tau Video", Height: 1080, Bitrate: 3_500_000, VariantIdx: 3, QualityIdx: 0},
		{Host: "Tau Video", Height: 1080, Bitrate: 6_000_000, Broken: true, VariantIdx: 4, QualityIdx: 0}, // broken must be dropped
	}

	best, ok := Best(cs, r)
	if !ok || best.VariantIdx != 3 {
		t.Fatalf("expected Tau Video 1080p (idx 3) to win, got %+v (ok=%v)", best, ok)
	}
	if !Exact(cs[3], r) {
		t.Fatalf("expected Tau Video 1080p to be an Exact match")
	}
	if Exact(cs[2], r) {
		t.Fatalf("expected Google Drive 1080p NOT to be an Exact match when Tau Video is #1 host")
	}
	if Exact(cs[4], r) {
		t.Fatalf("broken candidate must never be Exact")
	}
}

func TestNeverHostsFallbackWhenPoolEmpty(t *testing.T) {
	r := Rules{
		QualityOrder: []string{"1080p", "720p"},
		HostOrder:    []string{"Tau Video"},
		NeverHosts:   []string{"Vidmoly"},
	}
	// If only Vidmoly is available and healthy, fallback uses it rather than failing.
	cs := []Candidate{
		{Host: "Vidmoly", Height: 720, VariantIdx: 0, QualityIdx: 0},
	}
	best, ok := Best(cs, r)
	if !ok || best.Host != "Vidmoly" {
		t.Fatalf("expected fallback to Vidmoly when no other host is available, got %+v (ok=%v)", best, ok)
	}
}

func TestOnlyHostsRestrictsAndFallsBack(t *testing.T) {
	r := Rules{
		QualityOrder: []string{"1080p", "720p"},
		HostOrder:    []string{"Tau Video", "OK.ru"},
		OnlyHosts:    []string{"OK.ru"},
	}
	cs := []Candidate{
		{Host: "Tau Video", Height: 1080, VariantIdx: 0},
		{Host: "OK.ru", Height: 720, VariantIdx: 1},
	}
	// only OK.ru may be auto-picked, even though Tau Video ranks higher
	if best, ok := Best(cs, r); !ok || best.VariantIdx != 1 {
		t.Fatalf("OnlyHosts: want OK.ru (idx 1), got %+v (ok=%v)", best, ok)
	}
	if Exact(cs[0], r) {
		t.Fatal("a host outside OnlyHosts must never be Exact")
	}
	// no allowed host works: fall back to the whole healthy pool
	if best, ok := Best(cs[:1], r); !ok || best.VariantIdx != 0 {
		t.Fatalf("OnlyHosts fallback: want Tau Video, got %+v (ok=%v)", best, ok)
	}
}
