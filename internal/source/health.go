package source

import (
	"context"
	"time"
)

// Health is the latest probe result for a provider.
type Health struct {
	ID      string
	OK      bool
	Latency time.Duration
	Err     string
	At      time.Time
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
	h := Health{ID: p.Info().ID, OK: err == nil, Latency: time.Since(t0), At: time.Now()}
	if err != nil {
		h.Err = err.Error()
	}
	return h
}
