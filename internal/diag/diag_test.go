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
