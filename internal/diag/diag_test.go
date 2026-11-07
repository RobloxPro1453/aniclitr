package diag

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecordFlushAndReport(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	Reset()

	Record("animecix", "search", 180*time.Millisecond, OK)
	Record("animecix", "search", 220*time.Millisecond, OK)
	Record("animetr", "resolve", 2800*time.Millisecond, Empty)
	Record("animetr", "resolve", 5200*time.Millisecond, Error)

	path := Flush()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected blame.json to be written at %s: %v", path, err)
	}

	snap := Snapshot()
	if snap["animecix"]["search"].Calls != 2 || snap["animecix"]["search"].AvgMs() != 200 {
		t.Fatalf("unexpected animecix search stats: %+v", snap["animecix"]["search"])
	}
	if snap["animetr"]["resolve"].Empty != 1 || snap["animetr"]["resolve"].Errors != 1 {
		t.Fatalf("unexpected animetr resolve stats: %+v", snap["animetr"]["resolve"])
	}

	rep := FormatReport()
	if !strings.Contains(rep, "animecix") || !strings.Contains(rep, "animetr") || !strings.Contains(rep, "resolve") {
		t.Fatalf("unexpected report output:\n%s", rep)
	}
}

func TestRedactURLStripsSecrets(t *testing.T) {
	in := "https://cdn.example.com/hls/master.m3u8?token=supersecret123&id=42&signature=abc"
	got := RedactURL(in)
	if strings.Contains(got, "supersecret123") || strings.Contains(got, "abc") {
		t.Fatalf("secret query param leaked in %q", got)
	}
	if !strings.Contains(got, "id=42") {
		t.Fatalf("non-secret param should be kept in %q", got)
	}
}

func TestRecentWindow(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	Reset()
	base := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	defer func() { now = time.Now }()
	at := func(d time.Duration) { now = func() time.Time { return base.Add(d) } }

	at(-30 * time.Hour) // a bad spell yesterday
	Record("animetr", "resolve", 4*time.Second, Error)
	Record("animetr", "resolve", 4*time.Second, Error)
	at(-2 * time.Hour)
	Record("animetr", "resolve", 200*time.Millisecond, OK)
	at(0)
	Record("animetr", "search", 400*time.Millisecond, Empty)

	o := Recent("animetr", 24*time.Hour)
	if o.Calls != 2 || o.Errors != 0 || o.Empty != 1 || o.AvgMs() != 300 {
		t.Fatalf("last 24 h: %+v", o)
	}
	if got := SummaryLine("animetr"); !strings.Contains(got, "son 24 sa: 2 çağrı") || !strings.Contains(got, "%50") {
		t.Fatalf("summary: %q", got)
	}
	if all := Snapshot()["animetr"]["resolve"]; all.Calls != 3 {
		t.Fatalf("totals must keep everything: %+v", all)
	}

	// survives a restart, and buckets past 48 h are dropped
	Flush()
	at(40 * time.Hour) // keeps 48 h back to base-8h: the -2h and 0h calls, not -30h
	loaded = false
	if o := Recent("animetr", 72*time.Hour); o.Calls != 2 {
		t.Fatalf("after reload, only the last 48 h should remain: %+v", o)
	}
	if SummaryLine("animetr") != "" {
		t.Fatal("no calls in the last day: no line")
	}
}
