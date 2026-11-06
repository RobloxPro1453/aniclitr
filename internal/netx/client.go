// Package netx is the shared HTTP layer: browser-like headers, retries, timeouts.
package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// UserAgent is a current desktop Chrome UA; many hosts reject Go's default.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var jar, _ = cookiejar.New(nil)

// Client follows redirects and keeps cookies.
var Client = &http.Client{Timeout: 20 * time.Second, Jar: jar}

// NoRedirect returns the redirect response itself instead of following it.
var NoRedirect = &http.Client{
	Timeout: 20 * time.Second,
	Jar:     jar,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// StatusError is returned for non-2xx responses.
type StatusError struct {
	Code       int
	URL        string
	Body       []byte
	RetryAfter time.Duration
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Code, e.URL) }

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // 408
		http.StatusTooEarly,            // 425
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return code >= 500
}

// Get fetches url with headers, retrying transient failures with jittered backoff
// and honouring Retry-After headers on 429/503 responses.
func Get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	var lastErr error
	var hintedDelay time.Duration
	var hasHint bool
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt > 0 {
			delay := time.Duration(300*(1<<attempt))*time.Millisecond + time.Duration(rand.IntN(250))*time.Millisecond
			if hasHint {
				delay = hintedDelay
			}
			if delay > 0 {
				t := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					t.Stop()
					return nil, ctx.Err()
				case <-t.C:
				}
			}
		}
		body, err := once(ctx, url, headers)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		hasHint = false
		if se, ok := err.(*StatusError); ok {
			if !isRetryableStatus(se.Code) {
				return nil, err // permanent 4xx
			}
			if se.RetryAfter > 0 {
				hintedDelay, hasHint = se.RetryAfter, true
			}
		}
	}
	return nil, lastErr
}

func once(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	if err := WaitHost(ctx, url); err != nil {
		return nil, err
	}
	req, err := NewRequest(ctx, url, headers)
	if err != nil {
		return nil, err
	}
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var ra time.Duration
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			ra, _ = parseRetryAfter(resp.Header, time.Now())
		}
		return nil, &StatusError{
			Code:       resp.StatusCode,
			URL:        url,
			Body:       errBody,
			RetryAfter: ra,
		}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// NewRequest builds a GET request with browser defaults plus headers.
func NewRequest(ctx context.Context, url string, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "tr-TR,tr;q=0.9,en-US;q=0.8,en;q=0.7")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// GetJSON fetches and decodes JSON into v.
func GetJSON(ctx context.Context, url string, headers map[string]string, v any) error {
	h := map[string]string{"Accept": "application/json, text/plain, */*"}
	for k, val := range headers {
		h[k] = val
	}
	body, err := Get(ctx, url, h)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("geçersiz JSON (%s): %w", url, err)
	}
	return nil
}

// PostJSON sends a JSON POST request with retries/rate-limiting and decodes the JSON response into out (if non-nil).
func PostJSON(ctx context.Context, url string, headers map[string]string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 0 {
			delay := time.Duration(300*(1<<attempt))*time.Millisecond + time.Duration(rand.IntN(200))*time.Millisecond
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		if err := WaitHost(ctx, url); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept-Language", "tr-TR,tr;q=0.9,en-US;q=0.8,en;q=0.7")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/plain, */*")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := Client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			se := &StatusError{Code: resp.StatusCode, URL: url, Body: body}
			lastErr = se
			if !isRetryableStatus(resp.StatusCode) {
				return se
			}
			continue
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if out != nil && len(body) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("geçersiz JSON (%s): %w", url, err)
			}
		}
		return nil
	}
	return lastErr
}
