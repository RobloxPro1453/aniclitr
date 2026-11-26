package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"aniclitr/internal/config"
	"aniclitr/internal/tools"
)

// ScrubFrame is one cached preview frame extracted along the timeline.
type ScrubFrame struct {
	Sec  float64
	Path string
}

// ScrubSet holds a timeline of cached preview frames for one episode.
type ScrubSet struct {
	Dir    string
	Step   float64
	Frames []ScrubFrame
}

// Nearest returns the preview frame closest to sec.
func (s ScrubSet) Nearest(sec float64) (ScrubFrame, bool) {
	if len(s.Frames) == 0 {
		return ScrubFrame{}, false
	}
	idx := sort.Search(len(s.Frames), func(i int) bool { return s.Frames[i].Sec >= sec })
	if idx == 0 {
		return s.Frames[0], true
	}
	if idx >= len(s.Frames) {
		return s.Frames[len(s.Frames)-1], true
	}
	if sec-s.Frames[idx-1].Sec <= s.Frames[idx].Sec-sec {
		return s.Frames[idx-1], true
	}
	return s.Frames[idx], true
}

// BuildScrubSet extracts lightweight JPEG timeline thumbnails (1 frame every
// stepSec, default 30s) into CacheDir/scrubber/<key>/ for fast visual seeking.
// Works best on local/downloaded files; returns cached frames immediately if
// already generated.
func BuildScrubSet(ctx context.Context, mediaPath, key string, stepSec float64) (ScrubSet, error) {
	if stepSec <= 0 {
		stepSec = 30
	}
	dir := filepath.Join(config.CacheDir(), "scrubber", sanitizeFilename(key))
	if existing := loadScrubFrames(dir, stepSec); len(existing) >= 4 {
		return ScrubSet{Dir: dir, Step: stepSec, Frames: existing}, nil
	}
	ffmpeg, err := tools.LookPath("ffmpeg")
	if err != nil {
		return ScrubSet{}, fmt.Errorf("önizleme karesi için ffmpeg gerekli")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ScrubSet{}, err
	}
	pattern := filepath.Join(dir, "f_%04d.jpg")
	vf := fmt.Sprintf("fps=1/%.2f,scale=240:-1", stepSec)
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-v", "error",
		"-y",
		"-i", mediaPath,
		"-vf", vf,
		"-q:v", "6",
		pattern,
	)
	if err := cmd.Run(); err != nil {
		return ScrubSet{}, err
	}
	return ScrubSet{Dir: dir, Step: stepSec, Frames: loadScrubFrames(dir, stepSec)}, nil
}

func loadScrubFrames(dir string, stepSec float64) []ScrubFrame {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ScrubFrame
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "f_") || !strings.HasSuffix(e.Name(), ".jpg") {
			continue
		}
		numStr := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "f_"), ".jpg")
		n, err := strconv.Atoi(numStr)
		if err != nil || n <= 0 {
			continue
		}
		out = append(out, ScrubFrame{
			Sec:  float64(n-1) * stepSec,
			Path: filepath.Join(dir, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sec < out[j].Sec })
	return out
}
