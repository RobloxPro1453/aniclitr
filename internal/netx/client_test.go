package netx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	h := http.Header{}
	h.Set("Retry-After", "2")
	d, ok := parseRetryAfter(h, now)
	if !ok || d != 2*time.Second {
		t.Fatalf("got (%v, %v), want (2s, true)", d, ok)
	}

	h.Set("Retry-After", "120")
	d, ok = parseRetryAfter(h, now)
	if !ok || d != maxRetryAfter {
		t.Fatalf("got (%v, %v), want (%v, true)", d, ok, maxRetryAfter)
	}

	h.Set("Retry-After", now.Add(5*time.Second).Format(http.TimeFormat))
	d, ok = parseRetryAfter(h, now)
	if !ok || d != 5*time.Second {
		t.Fatalf("got (%v, %v), want (5s, true)", d, ok)
	}
}

func TestGetRetries429WithRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	body, err := Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("got body %q, want %q", string(body), "ok")
	}
	if calls.Load() != 2 {
		t.Fatalf("got %d calls, want 2", calls.Load())
	}
}

func TestGetRetries408And425(t *testing.T) {
	for _, code := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusServiceUnavailable} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := calls.Add(1)
			if n < 3 {
				if code == http.StatusServiceUnavailable {
					w.Header().Set("Retry-After", "0")
				}
				w.WriteHeader(code)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("recovered"))
		}))
		body, err := Get(context.Background(), srv.URL, nil)
		srv.Close()
		if err != nil || string(body) != "recovered" {
			t.Fatalf("code %d: err=%v body=%q", code, err, string(body))
		}
		if calls.Load() != 3 {
			t.Fatalf("code %d: got %d calls, want 3", code, calls.Load())
		}
	}
}

func TestGetDoesNotRetryPermanent4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("file not found"))
	}))
	defer srv.Close()

	_, err := Get(context.Background(), srv.URL, nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	se, ok := err.(*StatusError)
	if !ok || se.Code != http.StatusNotFound || string(se.Body) != "file not found" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("got %d calls, want 1 (no retry on 404)", calls.Load())
	}
}

func TestWaitHostTokenBucket(t *testing.T) {
	const host = "ratelimit-test.example.com"
	SetHostLimit(host, 60*time.Millisecond, 2)
	defer SetHostLimit(host, 0, 0)

	ctx := context.Background()
	start := time.Now()
	// First 2 requests are within burst (immediate).
	for i := 0; i < 2; i++ {
		if err := WaitHost(ctx, "https://"+host+"/v1"); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 30*time.Millisecond {
		t.Fatalf("burst requests took too long: %v", elapsed)
	}
	// Third request must wait ~60ms.
	if err := WaitHost(ctx, "https://"+host+"/v1"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond {
		t.Fatalf("expected rate limit delay >= 45ms, got %v", elapsed)
	}
}
