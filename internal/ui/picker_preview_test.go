package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/diag"
	"aniclitr/internal/source"
)

// Before a host is resolved, the picker shows how it has done before.
func TestPickerShowsHostRecord(t *testing.T) {
	a := testApp(t, Graphics{})
	diag.Reset()
	for i := 0; i < 3; i++ {
		diag.Record("extract", "Tau Video", 1200*time.Millisecond, diag.OK)
	}
	diag.Record("extract", "Tau Video", 2000*time.Millisecond, diag.Error)
	for i := 0; i < 3; i++ {
		diag.Record("extract", "Sibnet", 500*time.Millisecond, diag.Error)
	}
	diag.Record("extract", "Voe", time.Second, diag.OK) // too few calls to say anything

	an := source.Anime{Source: "animecix", ID: "1", Title: "X"}
	vs := []source.Variant{
		{Fansub: "Grup", Host: "Tau Video", URL: "https://tau-video.xyz/embed/abc"},
		{Fansub: "Grup", Host: "Sibnet", URL: "https://video.sibnet.ru/shell.php?videoid=1"},
		{Fansub: "Grup", Host: "Voe", URL: "https://voe.sx/e/1"},
		{Fansub: "Grup", Host: "Doodstream", URL: "https://dood.example/e/1"},
	}
	p := newPicker(an, source.Episode{Season: 1, Number: 1}, nil, false)
	_ = p.build(a, vs) // queues resolves; none run in the test
	for _, v := range vs {
		delete(a.resolved, v.URL) // as if not tried yet
	}
	a.failed["https://dood.example/e/1"] = time.Now()

	out := ansi.Strip(strings.Join(p.hostPane(a, 70, 30), "\n"))
	for _, want := range []string{"%75 açıldı · 1,4 sn", "%0 açıldı", "son denemede açılmadı", "enter: dene"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
