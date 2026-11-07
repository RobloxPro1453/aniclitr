// Package diag collects per-provider and per-operation latency and failure
// statistics ("blame") and persists them to CacheDir/blame.json without
// storing any sensitive headers, cookies, or tokens.
//
// Design adapted from Migurdex's BlameCollector (GPL-3.0).
package diag

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
)

// Outcome classifies the result of one provider or extractor call.
type Outcome int

const (
	OK Outcome = iota
	Empty
	Error
	Timeout
)

// Op holds accumulated metrics for a single (provider, operation) pair.
type Op struct {
	Calls    int       `json:"calls"`
	Errors   int       `json:"errors"`
	Empty    int       `json:"empty"`
	Timeouts int       `json:"timeouts"`
	TotalMs  int64     `json:"total_ms"`
	MaxMs    int64     `json:"max_ms"`
	Last     time.Time `json:"last"`
}

// AvgMs returns the mean call duration in milliseconds.
func (o Op) AvgMs() int64 {
	if o.Calls <= 0 {
		return 0
	}
	return o.TotalMs / int64(o.Calls)
}

// FailPct returns the percentage of calls that failed, timed out, or returned empty.
func (o Op) FailPct() int {
	if o.Calls <= 0 {
		return 0
	}
	bad := o.Errors + o.Empty + o.Timeouts
	return (bad*100 + o.Calls/2) / o.Calls
}

var (
	mu        sync.Mutex
	loaded    bool
	loadedDir string
	data      = map[string]map[string]Op{}
	lastSave  time.Time
	// hours: provider → hour ("2006-01-02T15", UTC) → all its ops summed.
	// The running totals in data never forget; these answer "lately".
	hours = map[string]map[string]Op{}
	now   = time.Now // tests move the clock
)

// keepHours bounds the hourly buckets (a day plus a margin).
const keepHours = 48

const hourKey = "2006-01-02T15"

func filePath() string {
	return filepath.Join(config.CacheDir(), "blame.json")
}

func hoursPath() string {
	return filepath.Join(config.CacheDir(), "blame-saatlik.json")
}

// pruneHoursLocked drops buckets older than keepHours.
func pruneHoursLocked() {
	cut := now().UTC().Add(-keepHours * time.Hour).Format(hourKey)
	for p, hs := range hours {
		for h := range hs {
			if h < cut {
				delete(hs, h)
			}
		}
		if len(hs) == 0 {
			delete(hours, p)
		}
	}
}

func ensureLoadedLocked() {
	dir := config.CacheDir()
	if loaded && loadedDir == dir {
		return
	}
	loaded = true
	loadedDir = dir
	data = map[string]map[string]Op{}
	if b, err := os.ReadFile(filepath.Join(dir, "blame.json")); err == nil {
		_ = json.Unmarshal(b, &data)
		if data == nil {
			data = map[string]map[string]Op{}
		}
	}
	hours = map[string]map[string]Op{}
	if b, err := os.ReadFile(hoursPath()); err == nil {
		_ = json.Unmarshal(b, &hours)
		if hours == nil {
			hours = map[string]map[string]Op{}
		}
	}
	pruneHoursLocked()
}

// add folds one call into an Op.
func (o *Op) add(ms int64, at time.Time, outcome Outcome) {
	o.Calls++
	o.TotalMs += ms
	if ms > o.MaxMs {
		o.MaxMs = ms
	}
	o.Last = at
	switch outcome {
	case Empty:
		o.Empty++
	case Error:
		o.Errors++
	case Timeout:
		o.Timeouts++
	}
}

// Record logs one operation's duration and outcome into the blame table.
func Record(provider, op string, d time.Duration, outcome Outcome) {
	if provider == "" || op == "" {
		return
	}
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	mu.Lock()
	defer mu.Unlock()
	ensureLoadedLocked()
	ops := data[provider]
	if ops == nil {
		ops = map[string]Op{}
		data[provider] = ops
	}
	at := now().UTC()
	cur := ops[op]
	cur.add(ms, at, outcome)
	ops[op] = cur

	hk := at.Format(hourKey)
	if _, seen := hours[provider][hk]; !seen {
		pruneHoursLocked() // a new hour: let the oldest go (before taking hs)
	}
	hs := hours[provider]
	if hs == nil {
		hs = map[string]Op{}
		hours[provider] = hs
	}
	h := hs[hk]
	h.add(ms, at, outcome)
	hs[hk] = h

	if time.Since(lastSave) >= 5*time.Second {
		saveLocked()
	}
}

func saveLocked() {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	_ = config.WriteAtomic(filePath(), b)
	if hb, err := json.Marshal(hours); err == nil {
		_ = config.WriteAtomic(hoursPath(), hb)
	}
	lastSave = time.Now()
}

// Recent sums a provider's calls over the last `within` (hour resolution).
func Recent(provider string, within time.Duration) Op {
	mu.Lock()
	defer mu.Unlock()
	ensureLoadedLocked()
	cut := now().UTC().Add(-within).Format(hourKey)
	var sum Op
	for h, o := range hours[provider] {
		if h < cut {
			continue
		}
		sum.Calls += o.Calls
		sum.Errors += o.Errors
		sum.Empty += o.Empty
		sum.Timeouts += o.Timeouts
		sum.TotalMs += o.TotalMs
		if o.MaxMs > sum.MaxMs {
			sum.MaxMs = o.MaxMs
		}
		if o.Last.After(sum.Last) {
			sum.Last = o.Last
		}
	}
	return sum
}

// Flush immediately persists the current blame snapshot to CacheDir/blame.json.
func Flush() string {
	mu.Lock()
	defer mu.Unlock()
	ensureLoadedLocked()
	saveLocked()
	return filePath()
}

// Snapshot returns a deep copy of all recorded blame metrics.
func Snapshot() map[string]map[string]Op {
	mu.Lock()
	defer mu.Unlock()
	ensureLoadedLocked()
	out := make(map[string]map[string]Op, len(data))
	for p, ops := range data {
		cp := make(map[string]Op, len(ops))
		for k, v := range ops {
			cp[k] = v
		}
		out[p] = cp
	}
	return out
}

// Reset clears in-memory and on-disk blame metrics (used in tests).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	loaded = true
	loadedDir = config.CacheDir()
	data = map[string]map[string]Op{}
	hours = map[string]map[string]Op{}
	saveLocked()
}

// SummaryLine returns a compact Turkish summary for a provider (for the health screen).
func SummaryLine(provider string) string {
	o := Recent(provider, 24*time.Hour)
	if o.Calls == 0 {
		return ""
	}
	if pct := o.FailPct(); pct > 0 {
		return fmt.Sprintf("son 24 sa: %d çağrı · %%%d hata/boş · ort. %s", o.Calls, pct, fmtMs(o.AvgMs()))
	}
	return fmt.Sprintf("son 24 sa: %d çağrı · ort. %s", o.Calls, fmtMs(o.AvgMs()))
}

// FormatReport renders a human-readable Turkish table for `aniclitr doctor --blame`.
func FormatReport() string {
	snap := Snapshot()
	if len(snap) == 0 {
		return "Henüz kayıtlı teşhis (blame) verisi yok.\nDosya: " + filePath()
	}
	var provs []string
	for p := range snap {
		provs = append(provs, p)
	}
	sort.Strings(provs)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("aniclitr blame raporu — %s\n", filePath()))
	for _, p := range provs {
		b.WriteString(fmt.Sprintf("\n▌%s\n", p))
		ops := snap[p]
		var keys []string
		for k := range ops {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			o := ops[k]
			emptyPct := 0
			errPct := 0
			if o.Calls > 0 {
				emptyPct = (o.Empty * 100) / o.Calls
				errPct = ((o.Errors + o.Timeouts) * 100) / o.Calls
			}
			b.WriteString(fmt.Sprintf("  %-18s %4d çağrı  ·  %%%2d boş  ·  %%%2d hata  ·  ort. %5s  ·  en uzun %5s\n",
				k, o.Calls, emptyPct, errPct,
				fmtMs(o.AvgMs()), fmtMs(o.MaxMs)))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func fmtMs(ms int64) string {
	if ms >= 1000 {
		return strings.ReplaceAll(fmt.Sprintf("%.1f sn", float64(ms)/1000.0), ".", ",")
	}
	return fmt.Sprintf("%d ms", ms)
}

var secretQueryKeys = map[string]bool{
	"token": true, "access_token": true, "auth": true, "key": true,
	"sig": true, "signature": true, "cookie": true, "session": true,
}

// RedactURL strips sensitive query parameters (tokens, signatures, keys) from
// a URL so it can be safely displayed in diagnostics or bug reports.
func RedactURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	u.User = nil
	q := u.Query()
	changed := false
	for k := range q {
		lk := strings.ToLower(k)
		if secretQueryKeys[lk] || strings.Contains(lk, "token") || strings.Contains(lk, "secret") || strings.Contains(lk, "sig") {
			q.Set(k, "•••")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}
