package shaders

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if Prev(Off) != P4K || Prev(P4K) != P2K || Prev(P2K) != P1080 || Prev(P1080) != Off {
		t.Fatalf("profile reverse cycling order broken")
	}
}

func TestLiveMpvShaderCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live mpv shader compile in -short mode")
	}
	mpvBin, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("mpv not installed")
	}
	t.Setenv("ANICLITR_HOME", t.TempDir())
	for _, prof := range []string{P1080, P2K, P4K} {
		sl, err := ShaderList(prof)
		if err != nil || sl == "" {
			t.Fatalf("ShaderList(%s) failed: %v", prof, err)
		}
		logPath := filepath.Join(t.TempDir(), "mpv_"+prof+".log")
		cmd := exec.Command(mpvBin,
			"--no-config", "--vo=gpu", "--hwdec=auto-safe",
			"--frames=2", "--no-audio", "--no-window-dragging",
			"--log-file="+logPath,
			"--glsl-shaders="+sl,
			"av://lavfi:testsrc2=size=640x360:duration=1",
		)
		_ = cmd.Run()
		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("mpv log missing for %s: %v", prof, err)
		}
		text := string(raw)
		if !strings.Contains(text, "shaderc compile status 'success'") {
			t.Fatalf("mpv did not compile shaders for %s", prof)
		}
		lines := strings.Split(text, "\n")
		for idx, line := range lines {
			if (strings.Contains(line, "[vo/gpu") || strings.Contains(line, "glsl") || strings.Contains(line, "shader")) &&
				(strings.Contains(line, "[e]") || strings.Contains(line, "[f]") || strings.Contains(line, "compile error") || strings.Contains(line, "Failed")) {
				end := min(len(lines), idx+8)
				t.Fatalf("shader error in %s:\n%s", prof, strings.Join(lines[idx:end], "\n"))
			}
		}
	}
}
