package source

import (
	"context"
	"net/url"
	"strings"
	"sync"
)

var (
	mu        sync.RWMutex
	providers []Provider
)

// Register adds a provider; called from each provider package's init.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers = append(providers, p)
}

// All returns every registered provider, primary first, legacy last.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, 0, len(providers))
	rank := func(p Provider) int {
		i := p.Info()
		switch {
		case i.Primary:
			return 0
		case i.Legacy:
			return 3
		case i.Lang == "tr":
			return 1
		default:
			return 2
		}
	}
	for r := 0; r <= 3; r++ {
		for _, p := range providers {
			if rank(p) == r {
				out = append(out, p)
			}
		}
	}
	return out
}

// Get finds a provider by id.
func Get(id string) Provider {
	for _, p := range All() {
		if p.Info().ID == id {
			return p
		}
	}
	return nil
}

// IsURL reports whether s looks like an HTTP(S) URL.
func IsURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(strings.ToLower(s), "http://") || strings.HasPrefix(strings.ToLower(s), "https://")
}

// OpenURL tries every registered URLOpener to resolve a direct anime or episode URL.
func OpenURL(ctx context.Context, raw string) (Anime, *Episode, bool, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Anime{}, nil, false, ErrNotFound
	}
	for _, p := range All() {
		op, ok := p.(URLOpener)
		if !ok {
			continue
		}
		an, ep, handled, err := op.OpenURL(ctx, u)
		if handled {
			return an, ep, true, err
		}
	}
	return Anime{}, nil, false, ErrNotFound
}
