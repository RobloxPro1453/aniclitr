package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aniclitr/internal/config"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func TestExportImportRoundTripAndSafetySnapshot(t *testing.T) {
	homeA := t.TempDir()
	t.Setenv("ANICLITR_HOME", homeA)

	cfg := config.Load()
	cfg.Theme = "catppuccin"
	cfg.SkipFiller = true
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	h := store.Open()
	h.Update(source.Anime{Source: "animecix", ID: "100", Title: "Frieren"}, func(e *store.Entry) {
		e.Season, e.Episode = 1, 7
		e.Quality = "1080p"
	})
	lib := store.OpenLibrary()
	lib.Set(source.Anime{Source: "animecix", ID: "100", Title: "Frieren", MalID: 52991}, func(it *store.Item) {
		it.Status = store.Watching
		it.Score = 9
	})
	// OAuth token file must NOT be exported.
	if err := os.WriteFile(filepath.Join(homeA, "mal.json"), []byte(`{"access_token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	bundlePath := filepath.Join(t.TempDir(), "my-backup.json")
	exp, err := Export(bundlePath, "0.20.0")
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if len(exp.Files) < 3 {
		t.Fatalf("expected config+history+library in backup, got %v", exp.Files)
	}
	raw, _ := os.ReadFile(bundlePath)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "mal.json") {
		t.Fatalf("mal.json token leaked into portable backup!")
	}

	// Restore into a second home that already has its own config.
	homeB := t.TempDir()
	t.Setenv("ANICLITR_HOME", homeB)
	origCfg := config.Load()
	origCfg.Theme = "nord"
	if err := origCfg.Save(); err != nil {
		t.Fatal(err)
	}

	imp, err := Import(bundlePath)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if imp.BackupDir == "" {
		t.Fatalf("expected pre-restore safety snapshot directory to be created")
	}
	if _, err := os.Stat(filepath.Join(imp.BackupDir, "config.json")); err != nil {
		t.Fatalf("pre-restore config.json missing: %v", err)
	}

	restoredCfg := config.Load()
	if restoredCfg.Theme != "catppuccin" || !restoredCfg.SkipFiller {
		t.Fatalf("restored config mismatch: %+v", restoredCfg)
	}
	restoredHist := store.Open()
	if e, ok := restoredHist.Get("animecix:100"); !ok || e.Episode != 7 || e.Quality != "1080p" {
		t.Fatalf("restored history mismatch: %+v (ok=%v)", e, ok)
	}
	restoredLib := store.OpenLibrary()
	if it, ok := restoredLib.Get("animecix:100"); !ok || it.Score != 9 {
		t.Fatalf("restored library mismatch: %+v (ok=%v)", it, ok)
	}
}

func TestImportRejectsInvalidAndPathTraversal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)
	cfg := config.Load()
	cfg.Theme = "matcha"
	_ = cfg.Save()

	bad := filepath.Join(t.TempDir(), "evil.json")
	_ = os.WriteFile(bad, []byte(`{"version":1,"files":{"../evil.json":{"a":1}}}`), 0o644)
	if _, err := Import(bad); err == nil {
		t.Fatal("expected path traversal file name to be rejected")
	}
	if got := config.Load().Theme; got != "matcha" {
		t.Fatalf("existing config was modified on rejected import: %q", got)
	}
}
