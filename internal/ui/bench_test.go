package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"aniclitr/internal/config"
	"aniclitr/internal/source"
)

func BenchmarkComposeHome140x40(b *testing.B) {
	b.Setenv("ANICLITR_HOME", b.TempDir())
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	a := New(config.Load(), Graphics{})
	a.w, a.h = 140, 40
	h := a.stack[0].(*home)
	h.feedOK = true
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.compose()
	}
}

func BenchmarkComposeResults140x40(b *testing.B) {
	b.Setenv("ANICLITR_HOME", b.TempDir())
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	a := New(config.Load(), Graphics{})
	a.w, a.h = 140, 40
	r := newResults("frieren")
	r.order = []string{"animecix"}
	r.states["animecix"] = &srcState{count: 25}
	var list []source.Anime
	for i := 1; i <= 25; i++ {
		list = append(list, source.Anime{Source: "animecix", ID: fmt.Sprint(i), Title: fmt.Sprintf("Sousou no Frieren Part %d", i), Year: 2023, Kind: "TV", Description: strings.Repeat("Büyücü Frieren yolculuğuna devam ediyor. ", 4)})
	}
	r.byProv["animecix"] = list
	r.rebuild()
	a.stack = append(a.stack, r)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.compose()
	}
}
