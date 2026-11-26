// Package notify sends lightweight, non-blocking OS desktop notifications
// without external Go dependencies.
package notify

import (
	"strings"
	"sync"
)

var (
	mu   sync.Mutex
	seen = map[string]bool{}
)

// Send dispatches a desktop notification in the background (best-effort).
func Send(title, body string) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" && body == "" {
		return
	}
	go sendOS(title, body)
}

// SendOnce sends a desktop notification at most once per unique key per session.
func SendOnce(key, title, body string) bool {
	if key != "" {
		mu.Lock()
		if seen[key] {
			mu.Unlock()
			return false
		}
		seen[key] = true
		mu.Unlock()
	}
	Send(title, body)
	return true
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
