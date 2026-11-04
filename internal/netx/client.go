// Package netx is the shared HTTP layer: browser-like headers, retries, timeouts.
package netx

import (
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
	Code int
	URL  string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Code, e.URL) }

// Get fetches url with headers, retrying transient failures with jittered backoff.
func Get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(300*(1<<attempt))*time.Millisecond + time.Duration(rand.IntN(250))*time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		body, err := once(ctx, url, headers)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if se, ok := err.(*StatusError); ok && se.Code >= 400 && se.Code < 500 && se.Code != 429 {
			return nil, err // permanent
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func once(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
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
		return nil, &StatusError{Code: resp.StatusCode, URL: url}
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
