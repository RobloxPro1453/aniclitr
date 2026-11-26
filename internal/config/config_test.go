package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestANICLITRHomeIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)

	if got := Dir(); got != home {
		t.Fatalf("Dir() = %q, want %q", got, home)
	}
	wantCache := filepath.Join(home, "cache")
	if got := CacheDir(); got != wantCache {
		t.Fatalf("CacheDir() = %q, want %q", got, wantCache)
	}
	if st, err := os.Stat(wantCache); err != nil || !st.IsDir() {
		t.Fatalf("CacheDir was not created: %v", err)
	}
}

func TestLoadDefaultsAndSaveRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)

	cfg := Load()
	if cfg.Quality != "best" || cfg.Theme != "gece" || !cfg.AutoSkip || !cfg.Posters || cfg.UI != "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	cfg.Quality = "1080p"
	cfg.Theme = "nord"
	cfg.UI = "yeni"
	cfg.Sources["hianime"] = false
	cfg.HostOrder = []string{"Tau Video", "OK.ru"}
	cfg.SeasonSync = map[string]bool{"animecix:7822": true}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("atomic temp file should not remain after Save")
	}

	reloaded := Load()
	if reloaded.Quality != "1080p" || reloaded.Theme != "nord" || reloaded.UI != "yeni" {
		t.Fatalf("reloaded fields mismatch: %+v", reloaded)
	}
	if reloaded.Sources["hianime"] != false {
		t.Fatalf("reloaded Sources[hianime] = %v, want false", reloaded.Sources["hianime"])
	}
	if len(reloaded.HostOrder) != 2 || reloaded.HostOrder[0] != "Tau Video" {
		t.Fatalf("reloaded HostOrder = %v", reloaded.HostOrder)
	}
	if !reloaded.SeasonSync["animecix:7822"] {
		t.Fatalf("reloaded SeasonSync = %v", reloaded.SeasonSync)
	}
}

func TestLoadCorruptBacksUpWithoutOverwriting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)

	raw := "{not valid json!!!"
	cfgPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(cfgPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Load()
	if cfg.Quality != "best" || cfg.Theme != "gece" {
		t.Fatalf("corrupt config should fall back to defaults, got %+v", cfg)
	}

	bakBytes, err := os.ReadFile(cfgPath + ".bozuk")
	if err != nil {
		t.Fatalf("expected backup config.json.bozuk: %v", err)
	}
	if string(bakBytes) != raw {
		t.Fatalf("backup content = %q, want %q", string(bakBytes), raw)
	}
}

func TestEnabledSourceRules(t *testing.T) {
	cfg := defaults()
	if !cfg.Enabled("animecix", false) {
		t.Fatal("missing non-legacy source should default to enabled")
	}
	if cfg.Enabled("animedepo", true) {
		t.Fatal("legacy source should be disabled when cfg.Legacy is false")
	}
	cfg.Legacy = true
	if !cfg.Enabled("animedepo", true) {
		t.Fatal("legacy source should be enabled when cfg.Legacy is true")
	}
	cfg.Sources["animecix"] = false
	if cfg.Enabled("animecix", false) {
		t.Fatal("explicitly disabled source must return false")
	}
	cfg.Sources["animedepo"] = false
	if cfg.Enabled("animedepo", true) {
		t.Fatal("explicitly disabled legacy source must return false even when cfg.Legacy is true")
	}
}

func TestWriteAtomicOverwritesSafely(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data.json")

	if err := WriteAtomic(target, []byte("first")); err != nil {
		t.Fatalf("first WriteAtomic: %v", err)
	}
	if err := WriteAtomic(target, []byte("second-version")); err != nil {
		t.Fatalf("second WriteAtomic: %v", err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "second-version" {
		t.Fatalf("got %q, want %q", string(b), "second-version")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
