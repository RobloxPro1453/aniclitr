package source_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"aniclitr/internal/source"
	_ "aniclitr/internal/source/anihub"
	_ "aniclitr/internal/source/animecix"
	_ "aniclitr/internal/source/animetr"
	_ "aniclitr/internal/source/animim"
	_ "aniclitr/internal/source/hianime"
	_ "aniclitr/internal/source/sonanime"
)

// fake is a provider whose search and episode answers are set per test.
type fake struct {
	hits    []source.Anime
	eps     []source.Episode
	epsErr  error
	srchErr error
}

func (fake) Info() source.Info { return source.Info{ID: "fake"} }
func (f fake) Search(context.Context, string) ([]source.Anime, error) {
	return f.hits, f.srchErr
}
func (fake) Details(_ context.Context, a source.Anime) (source.Anime, error) { return a, nil }
func (f fake) Episodes(context.Context, source.Anime, int) ([]source.Episode, error) {
	return f.eps, f.epsErr
}
func (fake) Variants(context.Context, source.Anime, source.Episode) ([]source.Variant, error) {
	return nil, nil
}
func (fake) Resolve(context.Context, source.Variant) (*source.Stream, error) { return nil, nil }

func TestProbeChain(t *testing.T) {
	ctx := context.Background()
	hit := []source.Anime{{Source: "fake", ID: "1", Title: "One Piece"}}
	netErr := errors.New("bağlantı reddedildi")

	cases := []struct {
		name        string
		p           fake
		ok, changed bool
	}{
		{"healthy", fake{hits: hit, eps: []source.Episode{{Number: 1}}}, true, false},
		{"search down", fake{srchErr: netErr}, false, false},
		{"no results", fake{}, false, false},
		{"episodes unparsable", fake{hits: hit, epsErr: source.ErrNotFound}, false, true},
		{"episodes empty", fake{hits: hit}, false, true},
		{"episodes network error", fake{hits: hit, epsErr: netErr}, false, false},
	}
	for _, c := range cases {
		err := source.ProbeChain(ctx, c.p, "one piece")
		if (err == nil) != c.ok {
			t.Errorf("%s: ok=%v, err=%v", c.name, err == nil, err)
		}
		if got := errors.Is(err, source.ErrChanged); got != c.changed {
			t.Errorf("%s: changed=%v, want %v (err=%v)", c.name, got, c.changed, err)
		}
	}
}

func TestCheckMarksChanged(t *testing.T) {
	h := source.Check(context.Background(), probing{err: source.ErrChanged})
	if h.OK || !h.Changed {
		t.Fatalf("want !OK && Changed, got %+v", h)
	}
	h = source.Check(context.Background(), probing{})
	if !h.OK || h.Changed {
		t.Fatalf("want OK, got %+v", h)
	}
}

type probing struct {
	fake
	err error
}

func (p probing) Probe(context.Context) error { return p.err }

// TestLiveProbes runs every registered provider's health check against the
// real sites (ANICLITR_LIVE=1).
func TestLiveProbes(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("ANICLITR_LIVE=1 ile çalışır")
	}
	for _, p := range source.All() {
		if p.Info().Legacy {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		h := source.Check(ctx, p)
		cancel()
		t.Logf("%-10s ok=%v changed=%v %v %s", h.ID, h.OK, h.Changed, h.Latency.Round(time.Millisecond), h.Err)
		if h.Changed {
			t.Errorf("%s: %s", h.ID, h.Err)
		}
	}
}
