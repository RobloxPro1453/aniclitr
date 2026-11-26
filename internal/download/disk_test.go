package download

import (
	"path/filepath"
	"testing"
)

func TestDiskSpaceExistingDir(t *testing.T) {
	free, total, ok := DiskSpace(t.TempDir())
	if !ok {
		t.Fatal("DiskSpace returned ok=false for temp dir")
	}
	if total <= 0 {
		t.Fatalf("total = %d, want > 0", total)
	}
	if free < 0 || free > total {
		t.Fatalf("free = %d out of [0, %d]", free, total)
	}
}

func TestDiskSpaceNonExistentChildFallsBackToAncestor(t *testing.T) {
	child := filepath.Join(t.TempDir(), "olmayan", "alt", "klasor")
	free, total, ok := DiskSpace(child)
	if !ok || total <= 0 || free < 0 || free > total {
		t.Fatalf("DiskSpace(%q) = (%d, %d, %v), want valid volume stats", child, free, total, ok)
	}
}

func TestDiskSpaceEmptyPath(t *testing.T) {
	if _, _, ok := DiskSpace(""); ok {
		t.Fatal("DiskSpace(\"\") should return ok=false")
	}
}
