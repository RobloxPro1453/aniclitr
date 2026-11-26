package subsync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"aniclitr/internal/tools"
)

// Signals bundles every independent synchronization modality extracted from
// the target video. Any non-empty subset feeds the Decision Matrix.
type Signals struct {
	DurationSec float64
	RefCues     []Cue     // Signal 1: Embedded reference subtitle timeline (e.g. EN/JP MKV track)
	VAD         []float64 // Signal 2: Normalized speech-activity envelope in [0,1] sampled at VADHopSec
	VADHopSec   float64   // Hop duration per VAD bin (default 0.05s = 50ms)
	SceneCuts   []float64 // Signal 3: Video hard scene-cut timestamps in seconds
}

var reShowInfoPTS = regexp.MustCompile(`pts_time:([0-9]+(?:\.[0-9]+)?)`)

// ExtractSignals probes mediaPath (local file or stream URL) using ffmpeg to
// gather embedded reference subtitles, speech-band VAD energy envelope, and
// scene-cut timestamps. Gracefully degrades if any modality is unavailable.
func ExtractSignals(ctx context.Context, mediaPath string) Signals {
	var sig Signals
	ffmpeg, err := tools.LookPath("ffmpeg")
	if err != nil || mediaPath == "" {
		return sig
	}

	// Signal 1: Embedded reference subtitle (first text sub track, fast demux)
	if ref := extractEmbeddedSub(ctx, ffmpeg, mediaPath); len(ref) >= 10 {
		sig.RefCues = ref
	}

	// Signal 2: 8 kHz mono bandpass (300-3400 Hz human voice) PCM -> 50ms VAD
	if vad, dur := extractVAD(ctx, ffmpeg, mediaPath, 0.05); len(vad) > 100 {
		sig.VAD = vad
		sig.VADHopSec = 0.05
		if dur > sig.DurationSec {
			sig.DurationSec = dur
		}
	}

	// Signal 3: Hard scene cuts (sampled at 4 fps for speed on full episodes)
	if cuts := extractSceneCuts(ctx, ffmpeg, mediaPath); len(cuts) >= 5 {
		sig.SceneCuts = cuts
	}
	return sig
}

func extractEmbeddedSub(ctx context.Context, ffmpeg, mediaPath string) []Cue {
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-v", "error",
		"-i", mediaPath,
		"-map", "0:s:0",
		"-f", "srt",
		"pipe:1",
	)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	doc := Parse(string(out))
	return doc.Cues
}

func extractVAD(ctx context.Context, ffmpeg, mediaPath string, hopSec float64) ([]float64, float64) {
	const sampleRate = 8000
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-v", "error",
		"-i", mediaPath,
		"-vn", "-sn", "-dn",
		"-af", "highpass=f=300,lowpass=f=3400",
		"-ac", "1",
		"-ar", strconv.Itoa(sampleRate),
		"-f", "s16le",
		"pipe:1",
	)
	raw, err := cmd.Output()
	if err != nil || len(raw) < sampleRate*2 {
		return nil, 0
	}
	return ComputeVADFromPCM16(raw, sampleRate, hopSec)
}

// ComputeVADFromPCM16 computes a normalized [0,1] speech-activity envelope
// using adaptive noise-floor estimation (35th percentile floor + 1.6σ scale).
func ComputeVADFromPCM16(pcm []byte, sampleRate int, hopSec float64) ([]float64, float64) {
	if sampleRate <= 0 || hopSec <= 0 {
		return nil, 0
	}
	totalSamples := len(pcm) / 2
	hopSamples := int(math.Round(float64(sampleRate) * hopSec))
	if hopSamples <= 0 || totalSamples < hopSamples {
		return nil, 0
	}
	nBins := totalSamples / hopSamples
	rms := make([]float64, nBins)
	for b := 0; b < nBins; b++ {
		var sumSq float64
		base := b * hopSamples * 2
		for i := 0; i < hopSamples; i++ {
			v := int16(binary.LittleEndian.Uint16(pcm[base+i*2 : base+i*2+2]))
			f := float64(v) / 32768.0
			sumSq += f * f
		}
		rms[b] = math.Sqrt(sumSq / float64(hopSamples))
	}

	// Robust adaptive noise floor using sorted percentiles
	sorted := append([]float64(nil), rms...)
	sort.Float64s(sorted)
	floor := sorted[int(float64(len(sorted))*0.30)]
	p85 := sorted[int(float64(len(sorted))*0.85)]
	span := p85 - floor
	if span < 1e-6 {
		span = 1e-6
	}

	// Smooth 3-bin moving average and map to [0,1] via soft sigmoid
	env := make([]float64, nBins)
	for i := 0; i < nBins; i++ {
		avg := rms[i]
		cnt := 1.0
		if i > 0 {
			avg += rms[i-1]
			cnt++
		}
		if i+1 < nBins {
			avg += rms[i+1]
			cnt++
		}
		avg /= cnt
		z := (avg - floor) / span
		if z < 0 {
			z = 0
		}
		if z > 1 {
			z = 1
		}
		env[i] = z
	}
	return env, float64(totalSamples) / float64(sampleRate)
}

func extractSceneCuts(ctx context.Context, ffmpeg, mediaPath string) []float64 {
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-v", "info",
		"-i", mediaPath,
		"-an", "-sn", "-dn",
		"-vf", "fps=4,select='gt(scene,0.35)',showinfo",
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	var cuts []float64
	sc := bufio.NewScanner(&stderr)
	sc.Buffer(make([]byte, 32<<10), 512<<10)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "pts_time:") {
			continue
		}
		if m := reShowInfoPTS.FindStringSubmatch(line); len(m) > 1 {
			if t, err := strconv.ParseFloat(m[1], 64); err == nil && t >= 0 {
				cuts = append(cuts, t)
			}
		}
	}
	sort.Float64s(cuts)
	return cuts
}
