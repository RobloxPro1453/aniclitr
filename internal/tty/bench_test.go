package tty

import (
	"strings"
	"testing"
)

func sampleLines(w, h int) []string {
	row := "\x1b[38;2;243;139;168m▌ \x1b[1mSousou no Frieren\x1b[0m \x1b[38;2;166;173;200m· Sezon 1 · Bölüm 12 · 1080p · Tau Video\x1b[0m"
	pad := strings.Repeat(" ", max(0, w-68))
	lines := make([]string, h)
	for i := range lines {
		if i == 0 {
			lines[i] = "\x1b[38;2;137;180;250m╭" + strings.Repeat("─", max(0, w-2)) + "╮\x1b[0m"
		} else if i == h-1 {
			lines[i] = "\x1b[38;2;137;180;250m╰" + strings.Repeat("─", max(0, w-2)) + "╯\x1b[0m"
		} else {
			lines[i] = row + pad
		}
	}
	return lines
}

func BenchmarkSetLines140x40(b *testing.B) {
	lines := sampleLines(140, 40)
	f := NewFrame(140, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.SetLines(lines)
	}
}

func BenchmarkDiff140x40(b *testing.B) {
	l1 := sampleLines(140, 40)
	l2 := append([]string(nil), l1...)
	l2[10] = "\x1b[48;2;49;50;68m\x1b[38;2;243;139;168m▌ \x1b[1mAnother · Bölüm 1 · 1080p\x1b[0m"
	f1 := NewFrame(140, 40)
	f1.SetLines(l1)
	f2 := NewFrame(140, 40)
	f2.SetLines(l2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Diff(f1, f2, false, false)
	}
}
