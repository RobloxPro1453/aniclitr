package router

import (
	"fmt"
	"sync"
	"time"

	"aniclitr/internal/source"
)

// PreloadEntry holds a lightweight, short-TTL pre-resolved stream for the #1
// "Devam Et" episode so pressing Enter launches mpv with near-zero wait.
type PreloadEntry struct {
	AnimeKey  string
	Season    int
	Episode   int
	Fansub    string
	Host      string
	Stream    *source.Stream
	Quality   source.Quality
	ExpiresAt time.Time
}

// PreloadCache is a bounded in-memory cache (at most 4 entries) with TTL.
type PreloadCache struct {
	mu      sync.RWMutex
	entries map[string]PreloadEntry
	ttl     time.Duration
}

// NewPreloadCache creates a PreloadCache with ttl (default 5 minutes).
func NewPreloadCache(ttl time.Duration) *PreloadCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &PreloadCache{
		entries: map[string]PreloadEntry{},
		ttl:     ttl,
	}
}

func preloadKey(animeKey string, season, ep int) string {
	return fmt.Sprintf("%s|s%d|e%d", animeKey, season, ep)
}

// Put stores a pre-resolved stream, evicting expired entries and keeping size <= 4.
func (c *PreloadCache) Put(e PreloadEntry) {
	if e.Stream == nil || e.Quality.URL == "" {
		return
	}
	now := time.Now()
	if e.ExpiresAt.IsZero() {
		e.ExpiresAt = now.Add(c.ttl)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.entries {
		if now.After(v.ExpiresAt) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= 4 {
		// Evict earliest-expiring entry
		var oldestK string
		var oldestT time.Time
		for k, v := range c.entries {
			if oldestK == "" || v.ExpiresAt.Before(oldestT) {
				oldestK, oldestT = k, v.ExpiresAt
			}
		}
		if oldestK != "" {
			delete(c.entries, oldestK)
		}
	}
	c.entries[preloadKey(e.AnimeKey, e.Season, e.Episode)] = e
}

// Get retrieves a fresh pre-resolved stream if present and unexpired.
func (c *PreloadCache) Get(animeKey string, season, ep int) (PreloadEntry, bool) {
	c.mu.RLock()
	e, ok := c.entries[preloadKey(animeKey, season, ep)]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.ExpiresAt) {
		return PreloadEntry{}, false
	}
	return e, true
}

// Invalidate drops a cached stream (e.g. if playback failed).
func (c *PreloadCache) Invalidate(animeKey string, season, ep int) {
	c.mu.Lock()
	delete(c.entries, preloadKey(animeKey, season, ep))
	c.mu.Unlock()
}
