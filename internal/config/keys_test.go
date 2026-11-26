package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeysDefaultsAndOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)

	km := LoadKeys()
	if act, ok := km.Action("ctrl+b"); !ok || act != "library" {
		t.Fatalf("expected default ctrl+b -> library, got %q (%v)", act, ok)
	}

	// Custom override moves library to alt+l.
	if err := os.WriteFile(filepath.Join(home, "keys.json"), []byte(`{"library": "alt+l", "unknown_act": "x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	km2 := LoadKeys()
	if act, ok := km2.Action("alt+l"); !ok || act != "library" {
		t.Fatalf("expected override alt+l -> library, got %q (%v)", act, ok)
	}
	if _, ok := km2.Action("ctrl+b"); ok {
		t.Fatalf("old key ctrl+b should no longer trigger library after override")
	}

	// Corrupt keys.json is backed up to keys.json.bozuk without losing defaults.
	if err := os.WriteFile(filepath.Join(home, "keys.json"), []byte(`{bad json`), 0o644); err != nil {
		t.Fatal(err)
	}
	km3 := LoadKeys()
	if act, ok := km3.Action("ctrl+k"); !ok || act != "palette" {
		t.Fatalf("expected defaults after corrupt keys.json, got %q (%v)", act, ok)
	}
	if _, err := os.Stat(filepath.Join(home, "keys.json.bozuk")); err != nil {
		t.Fatalf("expected keys.json.bozuk backup: %v", err)
	}
}
