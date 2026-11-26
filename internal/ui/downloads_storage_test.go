package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestFormatStorageBarWidthsAndContent(t *testing.T) {
	applyTheme(themeByName("gece"))

	const gb = int64(1 << 30)
	// Wide layout
	wide := formatStorageBar(4*gb, 128*gb, 512*gb, 110, true)
	plain := ansi.Strip(wide)
	if !strings.Contains(plain, "depolama") || !strings.Contains(plain, "aniclitr") || !strings.Contains(plain, "boş") {
		t.Fatalf("wide bar missing expected labels: %q", plain)
	}
	if w := lipgloss.Width(wide); w > 110 {
		t.Fatalf("wide bar overflowed: width=%d > 110", w)
	}

	// Narrow layout (80-col terminal -> inner=76)
	narrow := formatStorageBar(4*gb, 128*gb, 512*gb, 76, false)
	plainN := ansi.Strip(narrow)
	if !strings.Contains(plainN, "depolama") || !strings.Contains(plainN, "boş") {
		t.Fatalf("narrow bar missing expected labels: %q", plainN)
	}
	if w := lipgloss.Width(narrow); w > 76 {
		t.Fatalf("narrow bar overflowed: width=%d > 76", w)
	}

	// Non-zero appBytes should always produce at least one filled segment.
	tiny := formatStorageBar(1<<20, 500*gb, 1000*gb, 96, true)
	if !strings.Contains(ansi.Strip(tiny), "━") {
		t.Fatalf("expected visible bar segment for non-zero downloads: %q", ansi.Strip(tiny))
	}

	// Invalid total or too-narrow width returns empty cleanly.
	if s := formatStorageBar(gb, gb, 0, 100, true); s != "" {
		t.Fatalf("zero total should return empty, got %q", s)
	}
	if s := formatStorageBar(gb, gb, 10*gb, 20, true); s != "" {
		t.Fatalf("too narrow width should return empty, got %q", s)
	}
}

func TestDownloadsViewIncludesStorageBarInClassicAndNova(t *testing.T) {
	for _, mode := range []string{"", "yeni"} {
		a := testApp(t, Graphics{})
		a.cfg.UI = mode
		a.w, a.h = 120, 32
		d := newDownloads()
		rendered := d.view(a, 120, 29)
		plain := ansi.Strip(rendered)
		if !strings.Contains(plain, "depolama") || !strings.Contains(plain, "boş") {
			t.Fatalf("mode %q: downloads view missing storage bar:\n%s", mode, plain)
		}
		lines := strings.Split(rendered, "\n")
		if len(lines) != 29 {
			t.Fatalf("mode %q: got %d lines, want 29", mode, len(lines))
		}
	}
}
