package netx

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type limit struct {
	every time.Duration
	burst int
}

var (
	limMu   sync.Mutex
	buckets = map[string]*bucket{}

	// Conservative per-host rate limits; unknown external hosts get defaultExternalLimit.
	hostLimits = map[string]limit{
		"graphql.anilist.co":  {every: 700 * time.Millisecond, burst: 3}, // ~85 req/min
		"animecix.tv":         {every: 150 * time.Millisecond, burst: 6},
		"mangacix.net":        {every: 150 * time.Millisecond, burst: 6},
		"api.myanimelist.net": {every: 300 * time.Millisecond, burst: 4},
	}

	defaultExternalLimit = limit{every: 125 * time.Millisecond, burst: 8} // 8 req/s
)

type bucket struct {
	lim    limit
	tokens float64
	last   time.Time
}

// SetHostLimit configures or overrides the rate limit for a host (pass every <= 0 to disable).
func SetHostLimit(host string, every time.Duration, burst int) {
	host = normalizeHost(host)
	limMu.Lock()
	defer limMu.Unlock()
	if every <= 0 {
		hostLimits[host] = limit{}
		delete(buckets, host)
		return
	}
	if burst < 1 {
		burst = 1
	}
	hostLimits[host] = limit{every: every, burst: burst}
	delete(buckets, host)
}

// WaitHost blocks until the host's token bucket permits a request or ctx is done.
func WaitHost(ctx context.Context, rawURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	host := extractHost(rawURL)
	if host == "" {
		return nil
	}
	wait := reserve(host, time.Now())
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		refund(host)
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func reserve(host string, now time.Time) time.Duration {
	limMu.Lock()
	defer limMu.Unlock()
	lim, ok := lookupLimitLocked(host)
	if !ok || lim.every <= 0 {
		return 0
	}
	burst := max(1, lim.burst)
	b, exists := buckets[host]
	if !exists {
		buckets[host] = &bucket{
			lim:    lim,
			tokens: float64(burst - 1),
			last:   now,
		}
		return 0
	}
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens += float64(elapsed) / float64(b.lim.every)
		if maxTokens := float64(burst); b.tokens > maxTokens {
			b.tokens = maxTokens
		}
		b.last = now
	}
	b.tokens -= 1
	if b.tokens >= 0 {
		return 0
	}
	return time.Duration(-b.tokens * float64(b.lim.every))
}

func refund(host string) {
	limMu.Lock()
	defer limMu.Unlock()
	if b, ok := buckets[host]; ok {
		burst := float64(max(1, b.lim.burst))
		if b.tokens+1 <= burst {
			b.tokens += 1
		}
	}
}

func lookupLimitLocked(host string) (limit, bool) {
	if lim, ok := hostLimits[host]; ok {
		return lim, lim.every > 0
	}
	if isLoopback(host) {
		return limit{}, false
	}
	return defaultExternalLimit, true
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return normalizeHost(rawURL)
	}
	return normalizeHost(u.Host)
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimPrefix(strings.Trim(h, "[]"), "www.")
}

const maxRetryAfter = 30 * time.Second

// parseRetryAfter parses "Retry-After: <seconds>" or HTTP-date forms, capped at 30 s.
func parseRetryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	if h == nil {
		return 0, false
	}
	val := strings.TrimSpace(h.Get("Retry-After"))
	if val == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(val); err == nil {
		if secs <= 0 {
			return 0, true
		}
		d := time.Duration(secs) * time.Second
		if d > maxRetryAfter {
			d = maxRetryAfter
		}
		return d, true
	}
	if t, err := http.ParseTime(val); err == nil {
		d := t.Sub(now)
		if d <= 0 {
			return 0, true
		}
		if d > maxRetryAfter {
			d = maxRetryAfter
		}
		return d, true
	}
	return 0, false
}
