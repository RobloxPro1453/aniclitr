package shaders

import (
	"os"
	"strings"
	"testing"
)

func TestAnime4KProfilesAndArgs(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	if args := MpvArgs(Off); len(args) != 0 {
		t.Fatalf("Off profile should produce no args, got %v", args)
	}

	for _, tc := range []struct {
		prof string
		want int
	}{
		{Light, 1},
		{Balanced, 2},
		{High, 3},
	} {
		ps, err := Paths(tc.prof)
		if err != nil {
			t.Fatalf("profile %s failed: %v", tc.prof, err)
		}
		if len(ps) != tc.want {
			t.Fatalf("profile %s expected %d shaders, got %d", tc.prof, tc.want, len(ps))
		}
		for _, p := range ps {
			b, err := os.ReadFile(p)
			if err != nil || !strings.Contains(string(b), "//!HOOK") {
				t.Fatalf("shader file %s missing //!HOOK header: %v", p, err)
			}
		}
		args := MpvArgs(tc.prof)
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--hwdec=auto-copy") || !strings.Contains(joined, "--glsl-shaders=") {
			t.Fatalf("profile %s missing GPU args: %v", tc.prof, args)
		}
	}

	if Next(Off) != Light || Next(High) != Off {
		t.Fatalf("unexpected Next cycle")
	}
}
