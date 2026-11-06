// Extractor registry architecture adapted from Migurdex (GPL-3.0),
// Migurdex.Core/PluginSystem/ExtractorManager.cs
package extract

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"aniclitr/internal/source"
)

// Extractor turns one host's embed page into directly playable qualities.
type Extractor interface {
	Name() string
	CanHandle(u *url.URL) bool
	Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error)
}

var (
	regMu    sync.RWMutex
	registry []Extractor
)

// Register adds an extractor to the global registry in registration order.
func Register(e Extractor) {
	if e == nil {
		return
	}
	regMu.Lock()
	defer regMu.Unlock()
	for i, existing := range registry {
		if strings.EqualFold(existing.Name(), e.Name()) {
			registry[i] = e
			return
		}
	}
	registry = append(registry, e)
}

// All returns a snapshot of all registered extractors.
func All() []Extractor {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Extractor, len(registry))
	copy(out, registry)
	return out
}

// Match returns all registered extractors that claim u, in registration order.
func Match(u *url.URL) []Extractor {
	if u == nil {
		return nil
	}
	regMu.RLock()
	defer regMu.RUnlock()
	var out []Extractor
	for _, e := range registry {
		if e.CanHandle(u) {
			out = append(out, e)
		}
	}
	return out
}

// CanExtract reports whether any registered extractor claims rawURL.
func CanExtract(rawURL string) bool {
	u, err := url.Parse(NormalizeURL(rawURL))
	if err != nil {
		return false
	}
	return len(Match(u)) > 0
}
