package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"aniclitr/internal/config"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func TestParseWatchArgs(t *testing.T) {
	q, ep := ParseWatchArgs([]string{"sousou", "no", "frieren", "12"})
	if q != "sousou no frieren" || ep != 12 {
		t.Fatalf("got q=%q ep=%d", q, ep)
	}
	q, ep = ParseWatchArgs([]string{"frieren"})
	if q != "frieren" || ep != 0 {
		t.Fatalf("single token should not be parsed as episode: q=%q ep=%d", q, ep)
	}
	q, ep = ParseWatchArgs([]string{"86"})
	if q != "86" || ep != 0 {
		t.Fatalf("numeric title alone must stay query: q=%q ep=%d", q, ep)
	}

	dq, dr := ParseDownloadArgs([]string{"sousou", "no", "frieren", "1-12"})
	if dq != "sousou no frieren" || dr != "1-12" {
		t.Fatalf("ParseDownloadArgs range failed: q=%q r=%q", dq, dr)
	}
	dq, dr = ParseDownloadArgs([]string{"frieren", "5,7-9"})
	if dq != "frieren" || dr != "5,7-9" {
		t.Fatalf("ParseDownloadArgs multi-range failed: q=%q r=%q", dq, dr)
	}
}

func TestPrintContinueAndBackupCommands(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	cfg := config.Load()
	cfg.Quality = "1080p"
	_ = cfg.Save()

	h := store.Open()
	h.Update(source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren"}, func(e *store.Entry) {
		e.Season, e.Episode = 1, 5
		e.Position, e.Duration = 600, 1200
		e.Quality = "1080p"
	})

	var buf bytes.Buffer
	PrintContinue(&buf, h)
	if !strings.Contains(buf.String(), "Sousou no Frieren") || !strings.Contains(buf.String(), "S1·B5") {
		t.Fatalf("unexpected continue output:\n%s", buf.String())
	}

	dst := filepath.Join(t.TempDir(), "snap.json")
	buf.Reset()
	if err := RunBackup(&buf, dst, "0.20.0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Yedek oluşturuldu") {
		t.Fatalf("unexpected backup output: %s", buf.String())
	}

	buf.Reset()
	if err := RunRestore(&buf, dst); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "geri yüklendi") {
		t.Fatalf("unexpected restore output: %s", buf.String())
	}
}
