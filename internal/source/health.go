package source

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrChanged marks a probe that reached the site but could not read it: the
// site's structure (or its access guard) changed and the provider needs an
// update. Retrying will not help, unlike a timeout.
var ErrChanged = errors.New("sitenin yapısı değişmiş")

// Health is the latest probe result for a provider.
type Health struct {
	ID      string
	OK      bool
	Changed bool // the site answered but its data no longer parses (ErrChanged)
	Latency time.Duration
	Err     string
	At      time.Time
}

// ProbeChain is a deeper health check than a bare search: it searches for
// query, then lists the first hit's season-1 episodes. A site that still
// answers searches but whose episode pages no longer parse is reported as
// ErrChanged, so a broken scraper shows up before a user hits it.
func ProbeChain(ctx context.Context, p Provider, query string) error {
	res, err := p.Search(ctx, query)
	if err != nil {
		return err
	}
	if len(res) == 0 {
		return ErrNotFound
	}
	an := res[0]
	eps, err := p.Episodes(ctx, an, 1)
	if errors.Is(err, ErrNotFound) || (err == nil && len(eps) == 0) {
		return fmt.Errorf("%w: %q bölüm listesi okunamadı", ErrChanged, an.Title)
	}
	return err
}

// Slow reports a working but sluggish source.
func (h Health) Slow() bool { return h.OK && h.Latency > 2500*time.Millisecond }

// Prober is optional: a provider can implement its own cheap health check.
// Without it, a small search is used.
type Prober interface {
	Probe(ctx context.Context) error
}

// Check probes one provider.
func Check(ctx context.Context, p Provider) Health {
	t0 := time.Now()
	var err error
	if pr, ok := p.(Prober); ok {
		err = pr.Probe(ctx)
	} else {
		var res []Anime
		res, err = p.Search(ctx, "one piece")
		if err == nil && len(res) == 0 {
			err = ErrNotFound
		}
	}
	h := Health{ID: p.Info().ID, OK: err == nil, Changed: errors.Is(err, ErrChanged), Latency: time.Since(t0), At: time.Now()}
	if err != nil {
		h.Err = err.Error()
	}
	return h
}
