package source

import "sync"

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
