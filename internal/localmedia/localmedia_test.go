package localmedia

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanLookupAndMissingEpisodes(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	root := t.TempDir()

	// Create two styles: release-named flat file + Season subfolder + sidecar .ass
	f1 := filepath.Join(root, "[SubsPlease] Sousou no Frieren - 01 (1080p).mkv")
	s1 := filepath.Join(root, "[SubsPlease] Sousou no Frieren - 01 (1080p).tr.ass")
	f2Dir := filepath.Join(root, "Sousou no Frieren", "Season 1")
	if err := os.MkdirAll(f2Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f2 := filepath.Join(f2Dir, "03.mkv")
	_ = os.WriteFile(f1, []byte("fake video 1"), 0o644)
	_ = os.WriteFile(s1, []byte("[Script Info]"), 0o644)
	_ = os.WriteFile(f2, []byte("fake video 3"), 0o644)

	idx := Open([]string{root})
	n := idx.Scan()
	if n != 2 {
		t.Fatalf("Scan found %d files, want 2", n)
	}

	ef1, ok := idx.Lookup([]string{"Sousou no Frieren"}, 1, 1)
	if !ok || ef1.Path != f1 || ef1.SubPath != s1 {
		t.Fatalf("Lookup S1E1 = %+v (ok=%v)", ef1, ok)
	}
	ef3, ok := idx.Lookup([]string{"Sousou no Frieren"}, 1, 3)
	if !ok || ef3.Path != f2 {
		t.Fatalf("Lookup S1E3 = %+v (ok=%v)", ef3, ok)
	}
	miss := idx.MissingEpisodes([]string{"Sousou no Frieren"}, 1, 4)
	if len(miss) != 2 || miss[0] != 2 || miss[1] != 4 {
		t.Fatalf("MissingEpisodes = %v, want [2 4]", miss)
	}

	// Verify cache reload works without re-scanning
	idx2 := Open([]string{root})
	if idx2.Count() != 2 {
		t.Fatalf("cached reload count = %d, want 2", idx2.Count())
	}
}
