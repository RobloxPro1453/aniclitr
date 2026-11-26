package shaders

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTwoCategoryPipelineAndZeroCopyArgs(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	if args := PipelineArgs(Pipeline{}); len(args) != 0 {
		t.Fatalf("empty pipeline should return no args, got %v", args)
	}
	// Legacy config migration
	if Normalize("hafif") != Fast || Normalize("1080p") != Fast || Normalize("dengeli") != Clear || Normalize("2k") != Clear || Normalize("yüksek") != Cinema || Normalize("4k") != Cinema {
		t.Fatalf("legacy profile normalization failed")
	}

	// Category 1: Non-AI AMD FSR real resolution targets
	for _, res := range []string{ResAuto, Res1080, Res2K, Res4K} {
		ps, err := PipelinePaths(Pipeline{Res: res})
		if err != nil || len(ps) == 0 {
			t.Fatalf("PipelinePaths(Res=%s) failed: ps=%v err=%v", res, ps, err)
		}
		b, err := os.ReadFile(ps[0])
		if err != nil || !strings.Contains(string(b), "FidelityFX Super Resolution") {
			t.Fatalf("expected official AMD FSR shader for %s", res)
		}
		switch res {
		case Res1080:
			if !strings.Contains(string(b), "1920") || !strings.Contains(string(b), "1080") {
				t.Fatalf("1080p FSR must target real 1920x1080")
			}
		case Res2K:
			if !strings.Contains(string(b), "2560") || !strings.Contains(string(b), "1440") {
				t.Fatalf("2K FSR must target real 2560x1440")
			}
		case Res4K:
			if !strings.Contains(string(b), "3840") || !strings.Contains(string(b), "2160") {
				t.Fatalf("4K FSR must target real 3840x2160")
			}
		}
	}

	// Category 2A: Official Bloc97/Anime4K v4 CNN quality restoration
	for _, enh := range []string{Fast, Clear, Cinema} {
		ps, err := PipelinePaths(Pipeline{Enhance: enh})
		if err != nil || len(ps) < 4 {
			t.Fatalf("PipelinePaths(Enhance=%s) failed: ps=%v err=%v", enh, ps, err)
		}
		args := PipelineArgs(Pipeline{Enhance: enh})
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "auto-copy") {
			t.Fatalf("must NEVER use hwdec=auto-copy: %v", args)
		}
		if !strings.Contains(joined, "--hwdec=auto-safe") {
			t.Fatalf("expected zero-copy --hwdec=auto-safe: %v", args)
		}
	}

	// Category 2B: Frame-Gen / Smooth Motion
	fgArgs := strings.Join(PipelineArgs(Pipeline{FrameGen: FGSmooth}), " ")
	if !strings.Contains(fgArgs, "--interpolation=yes") || !strings.Contains(fgArgs, "--video-sync=display-resample") {
		t.Fatalf("expected mpv interpolation flags for FGSmooth, got %q", fgArgs)
	}
	fg60 := strings.Join(PipelineArgs(Pipeline{FrameGen: FG60}), " ")
	if !strings.Contains(fg60, "--tscale=catmull_rom") {
		t.Fatalf("expected catmull_rom for FG60, got %q", fg60)
	}
}

func TestLiveMpvGpuNextShaderCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live mpv shader compile in -short mode")
	}
	mpvBin, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("mpv not installed")
	}
	t.Setenv("ANICLITR_HOME", t.TempDir())
	cases := []struct {
		name string
		p    Pipeline
	}{
		{"fsr_2k", Pipeline{Res: Res2K}},
		{"fsr_4k", Pipeline{Res: Res4K}},
		{"a4k_cam", Pipeline{Enhance: Clear}},
		{"combined_2k_cam_fg", Pipeline{Res: Res2K, Enhance: Clear, FrameGen: FGSmooth}},
	}
	for _, tc := range cases {
		args := append([]string{"--no-config", "--frames=2", "--no-audio", "--no-window-dragging"}, PipelineArgs(tc.p)...)
		logPath := filepath.Join(t.TempDir(), "mpv_"+tc.name+".log")
		args = append(args, "--log-file="+logPath, "av://lavfi:testsrc2=size=640x360:duration=1")
		if out, err := exec.Command(mpvBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("mpv failed on %s: %v\n%s", tc.name, err, string(out))
		}
		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("mpv log missing for %s: %v", tc.name, err)
		}
		lines := strings.Split(string(raw), "\n")
		for idx, line := range lines {
			if (strings.Contains(line, "[vo/gpu") || strings.Contains(line, "glsl") || strings.Contains(line, "shader")) &&
				(strings.Contains(line, "[e]") || strings.Contains(line, "[f]") || strings.Contains(line, "compile error")) {
				end := min(len(lines), idx+8)
				t.Fatalf("shader error in %s:\n%s", tc.name, strings.Join(lines[idx:end], "\n"))
			}
		}
	}
}
