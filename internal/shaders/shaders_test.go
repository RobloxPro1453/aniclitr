package shaders

import (
	"os"
	"strings"
	"testing"
)

func TestUpscaleProfilesAndZeroCopyArgs(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	if args := MpvArgs(Off); len(args) != 0 {
		t.Fatalf("Off profile should return no args, got %v", args)
	}
	// Legacy v1.2.0 config values migrate cleanly
	if Normalize("hafif") != P1080 || Normalize("dengeli") != P2K || Normalize("yüksek") != P4K {
		t.Fatalf("legacy profile normalization failed")
	}

	for _, prof := range []string{P1080, P2K, P4K} {
		ps, err := Paths(prof)
		if err != nil || len(ps) == 0 {
			t.Fatalf("Paths(%s) failed: ps=%v err=%v", prof, ps, err)
		}
		for _, p := range ps {
			b, err := os.ReadFile(p)
			if err != nil || !strings.Contains(string(b), "//!HOOK") {
				t.Fatalf("invalid shader file %s: err=%v", p, err)
			}
		}
		args := MpvArgs(prof)
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--glsl-shaders=") {
			t.Fatalf("expected --glsl-shaders for %s: %v", prof, args)
		}
		if strings.Contains(joined, "auto-copy") {
			t.Fatalf("must NEVER use hwdec=auto-copy (destroys GPU/CPU performance): %v", args)
		}
		if !strings.Contains(joined, "--hwdec=auto-safe") {
			t.Fatalf("expected zero-copy --hwdec=auto-safe for %s: %v", prof, args)
		}
	}

	if Next(Off) != P1080 || Next(P1080) != P2K || Next(P2K) != P4K || Next(P4K) != Off {
		t.Fatalf("profile cycling order broken")
	}
}
