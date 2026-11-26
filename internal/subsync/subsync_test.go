package subsync

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func synthCues(starts []float64, dur float64) []Cue {
	out := make([]Cue, len(starts))
	for i, s := range starts {
		out[i] = Cue{Index: i + 1, Start: s, End: s + dur, Text: "Diyalog satırı"}
	}
	return out
}

func TestAlignMultiModalCrossValidation(t *testing.T) {
	// Ground truth dialogue starts across a 200s segment
	truthStarts := []float64{5, 11, 18, 25, 34, 42, 51, 60, 72, 81, 90, 102, 115, 126, 138, 150}
	truth := synthCues(truthStarts, 2.4)

	// Input subtitle is shifted by -3.40 seconds (needs +3.40s to align!)
	const shift = 3.40
	shifted := make([]Cue, len(truth))
	for i, c := range truth {
		shifted[i] = Cue{Index: c.Index, Start: c.Start - shift, End: c.End - shift, Text: c.Text}
	}

	// Build matching synthetic VAD and SceneCut signals at ground truth
	vad := buildCueGrid(truth, 0.05)
	cuts := make([]float64, len(truthStarts))
	for i, s := range truthStarts {
		cuts[i] = s - 0.12 // hard cut 120ms before dialogue onset
	}

	res := Align(shifted, Signals{
		RefCues:   truth,
		VAD:       vad,
		VADHopSec: 0.05,
		SceneCuts: cuts,
	}, Options{MaxShiftSec: 10, CoarseStepSec: 0.10, FineStepSec: 0.01})

	if math.Abs(res.GlobalOffsetSec-shift) > 0.06 {
		t.Fatalf("GlobalOffsetSec = %.3f, want %.3f (matrix=%+v)", res.GlobalOffsetSec, shift, res.Matrix)
	}
	if res.Confidence < 0.80 {
		t.Fatalf("expected high cross-validated confidence >= 0.80, got %.2f", res.Confidence)
	}
	if len(res.Matrix) != 3 {
		t.Fatalf("expected 3 signal votes in matrix, got %d", len(res.Matrix))
	}
	for _, v := range res.Matrix {
		if !v.Agreed {
			t.Errorf("signal %q failed to agree with consensus: %+v", v.Name, v)
		}
	}
}

func TestAlignPiecewiseCommercialBreakTVvsBD(t *testing.T) {
	// Part A (0..100s): true times = orig + 1.50s
	// Eyecatch commercial gap: 100s..130s (30s silence)
	// Part B (130..240s): true times = orig - 4.20s (TV commercial cut!)
	var orig, truth []Cue
	for i := 0; i < 10; i++ {
		s := 10.0 + float64(i)*8.0
		orig = append(orig, Cue{Index: len(orig) + 1, Start: s, End: s + 2.5, Text: "Part A"})
		truth = append(truth, Cue{Index: len(truth) + 1, Start: s + 1.50, End: s + 4.00, Text: "Part A"})
	}
	for i := 0; i < 10; i++ {
		s := 140.0 + float64(i)*8.0
		orig = append(orig, Cue{Index: len(orig) + 1, Start: s, End: s + 2.5, Text: "Part B"})
		truth = append(truth, Cue{Index: len(truth) + 1, Start: s - 4.20, End: s - 1.70, Text: "Part B"})
	}

	res := Align(orig, Signals{RefCues: truth}, Options{
		MaxShiftSec:    8,
		CoarseStepSec:  0.10,
		FineStepSec:    0.01,
		TryPiecewise:   true,
		MinBreakGapSec: 15,
	})

	if len(res.Segments) != 2 {
		t.Fatalf("expected 2 piecewise commercial-break segments, got %d: %+v", len(res.Segments), res.Segments)
	}
	if math.Abs(res.Segments[0].OffsetSec-1.50) > 0.08 {
		t.Fatalf("Part A offset = %.3f, want +1.50", res.Segments[0].OffsetSec)
	}
	if math.Abs(res.Segments[1].OffsetSec-(-4.20)) > 0.08 {
		t.Fatalf("Part B offset = %.3f, want -4.20", res.Segments[1].OffsetSec)
	}
	// Verify MapTime transforms Part A and Part B accurately
	if got := res.MapTime(20.0); math.Abs(got-21.50) > 0.08 {
		t.Fatalf("MapTime(20) = %.3f, want 21.50", got)
	}
	if got := res.MapTime(160.0); math.Abs(got-155.80) > 0.08 {
		t.Fatalf("MapTime(160) = %.3f, want 155.80", got)
	}
}

func TestAlignFPSDriftDetection(t *testing.T) {
	// PAL 25fps -> 23.976fps drift over 1200s
	scale := 25.0 / 23.976
	var orig, truth []Cue
	for i := 0; i < 20; i++ {
		s := 30.0 + float64(i)*55.0
		orig = append(orig, Cue{Index: i + 1, Start: s, End: s + 2.5, Text: "Line"})
		truth = append(truth, Cue{Index: i + 1, Start: s*scale + 0.80, End: (s+2.5)*scale + 0.80, Text: "Line"})
	}
	res := Align(orig, Signals{RefCues: truth}, Options{
		MaxShiftSec:   5,
		CoarseStepSec: 0.10,
		FineStepSec:   0.01,
		TryFPSGrid:    true,
	})
	if math.Abs(res.Scale-scale) > 0.002 {
		t.Fatalf("expected FPS scale %.5f, got %.5f", scale, res.Scale)
	}
	if math.Abs(res.GlobalOffsetSec-0.80) > 0.10 {
		t.Fatalf("expected offset +0.80, got %.3f", res.GlobalOffsetSec)
	}
}

func TestParseAndLosslessRewriteASSandSRT(t *testing.T) {
	srt := "1\n00:00:10,000 --> 00:00:12,500\nMerhaba dünya\n\n2\n00:00:20,000 --> 00:00:22,000\nİkinci satır\n"
	doc := Parse(srt)
	if doc.Format != FormatSRT || len(doc.Cues) != 2 {
		t.Fatalf("SRT parse failed: %+v", doc)
	}
	out := Rewrite(doc, func(t float64) float64 { return t + 1.5 })
	if !strings.Contains(out, "00:00:11,500 --> 00:00:14,000") {
		t.Fatalf("SRT rewrite failed:\n%s", out)
	}

	ass := "[Script Info]\nTitle: Test\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:10.00,0:00:12.50,Default,,0,0,0,,{\\i1}Merhaba{\\i0}\n"
	adoc := Parse(ass)
	if adoc.Format != FormatASS || len(adoc.Cues) != 1 {
		t.Fatalf("ASS parse failed: %+v", adoc)
	}
	aout := Rewrite(adoc, func(t float64) float64 { return t + 2.25 })
	if !strings.Contains(aout, "Dialogue: 0,0:00:12.25,0:00:14.75,Default,,0,0,0,,{\\i1}Merhaba{\\i0}") {
		t.Fatalf("ASS lossless rewrite failed:\n%s", aout)
	}
}

func TestComputeVADFromPCM16(t *testing.T) {
	// 2 seconds at 8kHz: first 1s silent, second 1s 440Hz tone
	const sr = 8000
	pcm := make([]byte, sr*2*2)
	for i := sr; i < sr*2; i++ {
		v := int16(18000 * math.Sin(2*math.Pi*440*float64(i)/float64(sr)))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(v))
	}
	vad, dur := ComputeVADFromPCM16(pcm, sr, 0.05)
	if math.Abs(dur-2.0) > 0.05 || len(vad) != 40 {
		t.Fatalf("VAD shape unexpected: dur=%.2f len=%d", dur, len(vad))
	}
	if vad[5] > 0.15 || vad[30] < 0.85 {
		t.Fatalf("VAD failed to separate silence (%.2f) from speech (%.2f)", vad[5], vad[30])
	}
}
