package ui

import (
	"strings"
	"testing"
	"time"
)

func TestAgoLongNoAbsoluteDateOnceBug(t *testing.T) {
	loc := time.FixedZone("TRT", 3*3600)
	now := time.Date(2026, 11, 26, 20, 0, 0, 0, loc)

	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"future", now.Add(5 * time.Minute), "yeni"},
		{"just now", now.Add(-20 * time.Second), "şimdi"},
		{"minutes", now.Add(-15 * time.Minute), "15 dk önce"},
		{"hours", now.Add(-3 * time.Hour), "3 saat önce"},
		{"days", now.Add(-4 * 24 * time.Hour), "4 gün önce"},
		{"same year absolute date", time.Date(2026, 9, 21, 14, 0, 0, 0, loc), "21 Eyl"},
		{"prior year absolute date", time.Date(2025, 9, 21, 14, 0, 0, 0, loc), "21 Eyl 2025"},
	}
	for _, tc := range cases {
		got := agoLongAt(tc.at, now)
		if got != tc.want {
			t.Errorf("%s: agoLongAt = %q, want %q", tc.name, got, tc.want)
		}
		if strings.Contains(got, "Eyl önce") || strings.TrimSpace(got) == "önce" {
			t.Errorf("%s: invalid Turkish date suffix in %q", tc.name, got)
		}
	}
}

func TestCalendarDayDiffAndLabels(t *testing.T) {
	loc := time.FixedZone("TRT", 3*3600)
	now := time.Date(2026, 11, 26, 23, 30, 0, 0, loc)

	if d := calendarDayDiff(time.Date(2026, 11, 26, 0, 10, 0, 0, loc), now); d != 0 {
		t.Fatalf("same day diff = %d, want 0", d)
	}
	if d := calendarDayDiff(time.Date(2026, 11, 27, 0, 10, 0, 0, loc), now); d != 1 {
		t.Fatalf("tomorrow diff = %d, want 1", d)
	}
	if d := calendarDayDiff(time.Date(2026, 11, 25, 22, 0, 0, 0, loc), now); d != -1 {
		t.Fatalf("yesterday diff = %d, want -1", d)
	}
	if got := whenLabel(time.Date(2026, 11, 27, 1, 15, 0, 0, loc), now); !strings.HasPrefix(got, "yarın ") {
		t.Fatalf("whenLabel tomorrow = %q", got)
	}
	if got := dayLabel(time.Date(2026, 11, 27, 12, 0, 0, 0, loc), now); !strings.HasPrefix(got, "yarın · ") {
		t.Fatalf("dayLabel tomorrow = %q", got)
	}
}

func TestFormatHelpersSafetyAndTurkish(t *testing.T) {
	if got := fmtWatch(-10); got != "0 dk" {
		t.Errorf("fmtWatch(-10) = %q", got)
	}
	if got := fmtWatch(30); got != "< 1 dk" {
		t.Errorf("fmtWatch(30) = %q", got)
	}
	if got := fmtWatch(3600); got != "1 sa" {
		t.Errorf("fmtWatch(3600) = %q", got)
	}
	if got := fmtWatch(4500); got != "1 sa 15 dk" {
		t.Errorf("fmtWatch(4500) = %q", got)
	}
	if got := fmtETA(45 * time.Second); got != "45 sn" {
		t.Errorf("fmtETA(45s) = %q", got)
	}
	if got := fmtETA(3*time.Minute + 12*time.Second); got != "3 dk 12 sn" {
		t.Errorf("fmtETA(3m12s) = %q", got)
	}
	if got := fmtETA(2 * time.Hour); got != "2 sa" {
		t.Errorf("fmtETA(2h) = %q", got)
	}
	if got := humanSize(512); got != "512 B" {
		t.Errorf("humanSize(512) = %q", got)
	}
	if got := humanSize(120 * 1024); got != "120 KB" {
		t.Errorf("humanSize(120KB) = %q", got)
	}
	if got := humanSize(25 * (1 << 20)); got != "25 MB" {
		t.Errorf("humanSize(25MB) = %q", got)
	}
	if got := humanSize(2 * (1 << 30)); got != "2.0 GB" {
		t.Errorf("humanSize(2GB) = %q", got)
	}
	now := time.Now()
	if got := countdownAt(now.Add(400*time.Millisecond), now); got != "1sn" {
		t.Errorf("countdownAt(400ms) = %q, want 1sn", got)
	}
	if got := countdownAt(now.Add(2*time.Hour), now); got != "2sa" {
		t.Errorf("countdownAt(2h) = %q, want 2sa", got)
	}
	// Safety guards: must never panic
	if got := miniBar(0, 0.5, cAccent); got != nil {
		t.Errorf("miniBar(0) = %v", got)
	}
	_ = miniBar(10, -2.0, cAccent)
	_ = miniBar(10, 5.0, cAccent)
	if s := spinner(-3); s == "" {
		t.Errorf("spinner(-3) empty")
	}
}
