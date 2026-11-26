package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLookPathFindsManagedToolDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", t.TempDir())
	} else {
		t.Setenv("HOME", t.TempDir())
	}
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "aniclitr-fake-tool"
	fileName := name
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	full := filepath.Join(dir, fileName)
	if err := os.WriteFile(full, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := LookPath(name)
	if err != nil || got != full {
		t.Fatalf("expected managed tool %q, got %q (err=%v)", full, got, err)
	}
}
