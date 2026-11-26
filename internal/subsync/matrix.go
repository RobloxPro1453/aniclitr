package subsync

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// SignalVote captures one independent parameter's alignment measurement inside
// the Decision Matrix.
type SignalVote struct {
	Name       string  `json:"name"`
	OffsetSec  float64 `json:"offset_sec"`
	Scale      float64 `json:"scale"`
	PeakScore  float64 `json:"peak_score"`
	SNR        float64 `json:"snr"`        // sharpness ratio vs best distant sidelobe
	Weight     float64 `json:"weight"`     // final normalized weight in the decision matrix
	Confidence float64 `json:"confidence"` // 0..1 individual reliability
	Agreed     bool    `json:"agreed"`     // within ±150ms of fused consensus
}

// SegmentOffset defines a piecewise time mapping for a contiguous act of the
// episode (handling BD vs TV commercial-break / eyecatch cuts).
type SegmentOffset struct {
	StartSec   float64 `json:"start_sec"` // original cue start boundary
	EndSec     float64 `json:"end_sec"`
	OffsetSec  float64 `json:"offset_sec"`
	Confidence float64 `json:"confidence"`
}

// AlignmentResult is the output of the Multi-Parameter Decision Matrix.
type AlignmentResult struct {
	Scale           float64         `json:"scale"`
	GlobalOffsetSec float64         `json:"global_offset_sec"`
	Segments        []SegmentOffset `json:"segments"`
	Confidence      float64         `json:"confidence"`
	RequireConfirm  bool            `json:"require_confirm"`
	Matrix          []SignalVote    `json:"matrix"`
	SummaryTR       string          `json:"summary_tr"`
}

// Options tune the alignment search window and piecewise break sensitivity.
type Options struct {
	MaxShiftSec    float64 // default 30.0s
	CoarseStepSec  float64 // default 0.10s
	FineStepSec    float64 // default 0.01s
	TryFPSGrid     bool    // evaluate 23.976/24/25 fps ratios
	TryPiecewise   bool    // detect TV/BD commercial-break multi-segment jumps
	MinBreakGapSec float64 // dialogue silence gap to consider an act boundary (default 12s)
}

func defaultOptions(o Options) Options {
	if o.MaxShiftSec <= 0 {
		o.MaxShiftSec = 30.0
	}
	if o.CoarseStepSec <= 0 {
		o.CoarseStepSec = 0.10
	}
	if o.FineStepSec <= 0 {
		o.FineStepSec = 0.01
	}
	if o.MinBreakGapSec <= 0 {
		o.MinBreakGapSec = 12.0
	}
	return o
}

// Canonical anime mastering FPS conversion ratios.
var canonicalScales = []float64{
	1.0,
	23.976 / 24.0,  // 0.999000
	24.0 / 23.976,  // 1.001001
	25.0 / 23.976,  // PAL speedup
	23.976 / 25.0,  // PAL slowdown
	25.0 / 24.0,
	24.0 / 25.0,
}

// MapTime transforms an original cue timestamp t (seconds) using the piecewise
// or global alignment model.
func (r AlignmentResult) MapTime(t float64) float64 {
	sc := r.Scale
	if sc <= 0 {
		sc = 1.0
	}
	off := r.GlobalOffsetSec
	for _, seg := range r.Segments {
		if t >= seg.StartSec && t <= seg.EndSec {
			off = seg.OffsetSec
			break
		}
	}
	out := t*sc + off
	if out < 0 {
		return 0
	}
	return out
}

// Align runs the full Multi-Parameter Decision Matrix across all available
// signals (Embedded RefSub, Audio Speech VAD, Video Scene Cuts, FPS Drift Grid,
// and Piecewise Commercial-Break Segmentation).
func Align(cues []Cue, sig Signals, opts Options) AlignmentResult {
	opts = defaultOptions(opts)
	speech := filterSpeechCues(cues)
	if len(speech) < 4 {
		return AlignmentResult{
			Scale:          1.0,
			Confidence:     0,
			RequireConfirm: true,
			SummaryTR:      "Hizalama için yeterli diyalog satırı bulunamadı.",
		}
	}

	scales := []float64{1.0}
	if opts.TryFPSGrid {
		scales = canonicalScales
	}

	// Build reference binary grid if embedded reference subtitles exist
	const refHop = 0.05
	var refGrid []float64
	if len(sig.RefCues) >= 4 {
		refGrid = buildCueGrid(filterSpeechCues(sig.RefCues), refHop)
	}

	// --- STAGE 1 & 2: Evaluate each independent signal across FPS + offset ---
	var votes []SignalVote

	if len(refGrid) > 0 {
		v := evaluateGridSignal("Gömülü Referans Altyazı", speech, refGrid, refHop, scales, opts, 0.55)
		votes = append(votes, v)
	}
	if len(sig.VAD) > 0 {
		hop := sig.VADHopSec
		if hop <= 0 {
			hop = 0.05
		}
		v := evaluateGridSignal("Ses Konuşma Zarfı (VAD)", speech, sig.VAD, hop, scales, opts, 0.35)
		votes = append(votes, v)
	}
	if len(sig.SceneCuts) >= 5 {
		v := evaluateSceneCutSignal(speech, sig.SceneCuts, scales, opts, 0.15)
		votes = append(votes, v)
	}

	if len(votes) == 0 {
		return AlignmentResult{
			Scale:          1.0,
			Confidence:     0,
			RequireConfirm: true,
			SummaryTR:      "Hizalama sinyali çıkarılamadı (ffmpeg veya referans iz yok).",
		}
	}

	// --- STAGE 3: Decision Matrix Fusion & Cross-Validation ---
	fusedScale, fusedOffset, fusedConf, conflict := fuseDecisionMatrix(votes)

	res := AlignmentResult{
		Scale:           fusedScale,
		GlobalOffsetSec: roundMs(fusedOffset),
		Confidence:      fusedConf,
		RequireConfirm:  conflict || fusedConf < 0.70,
		Matrix:          votes,
	}

	// --- STAGE 4: Piecewise Commercial-Break / Eyecatch Segmentation ---
	if opts.TryPiecewise {
		var primaryGrid []float64
		var primaryHop float64
		if len(refGrid) > 0 {
			primaryGrid, primaryHop = refGrid, refHop
		} else if len(sig.VAD) > 0 {
			primaryGrid = sig.VAD
			primaryHop = sig.VADHopSec
			if primaryHop <= 0 {
				primaryHop = 0.05
			}
		}
		if len(primaryGrid) > 0 {
			segs := detectPiecewiseBreaks(speech, primaryGrid, primaryHop, fusedScale, fusedOffset, opts)
			if len(segs) > 1 {
				res.Segments = segs
			}
		}
	}

	if len(res.Segments) == 0 {
		res.Segments = []SegmentOffset{{
			StartSec:   0,
			EndSec:     1e9,
			OffsetSec:  res.GlobalOffsetSec,
			Confidence: res.Confidence,
		}}
	}

	res.SummaryTR = formatSummaryTR(res, conflict)
	return res
}

func filterSpeechCues(cues []Cue) []Cue {
	var out []Cue
	for _, c := range cues {
		if c.IsSign || c.End-c.Start < 0.15 {
			continue
		}
		out = append(out, c)
	}
	if len(out) < 4 {
		return cues
	}
	return out
}

func buildCueGrid(cues []Cue, hop float64) []float64 {
	maxT := 0.0
	for _, c := range cues {
		if c.End > maxT {
			maxT = c.End
		}
	}
	n := int(math.Ceil((maxT+60)/hop)) + 1
	grid := make([]float64, n)
	for _, c := range cues {
		i0 := max(0, int(math.Floor(c.Start/hop)))
		i1 := min(n-1, int(math.Ceil(c.End/hop)))
		for i := i0; i <= i1; i++ {
			grid[i] = 1.0
		}
	}
	return grid
}

// scoreCuesOnGrid computes normalized speech-vs-silence contrast score for a
// (scale, offset) hypothesis against a continuous [0,1] target grid (RefSub or VAD).
func scoreCuesOnGrid(cues []Cue, grid []float64, hop, scale, offset float64) float64 {
	n := len(grid)
	if n == 0 || len(cues) == 0 {
		return 0
	}
	var insideSum, insideCnt float64
	var outsideSum, outsideCnt float64
	var onsetBonus float64
	for _, c := range cues {
		s := c.Start*scale + offset
		e := c.End*scale + offset
		if e <= 0 || s >= float64(n)*hop {
			continue
		}
		i0 := max(0, int(math.Round(s/hop)))
		i1 := min(n-1, int(math.Round(e/hop)))
		for i := i0; i <= i1; i++ {
			insideSum += grid[i]
			insideCnt++
		}
		// Sample local silence guard bands (150-350ms before start & after end)
		for _, offBins := range []int{-6, -4, -3} {
			if idx := i0 + offBins; idx >= 0 && idx < n {
				outsideSum += grid[idx]
				outsideCnt++
			}
		}
		for _, offBins := range []int{3, 4, 6} {
			if idx := i1 + offBins; idx >= 0 && idx < n {
				outsideSum += grid[idx]
				outsideCnt++
			}
		}
		// Onset edge gradient bonus: reward rising edge right at cue start
		if i0 > 1 && i0+1 < n {
			edge := grid[i0+1] - grid[i0-2]
			if edge > 0 {
				onsetBonus += edge
			}
		}
	}
	if insideCnt == 0 {
		return -1
	}
	inMean := insideSum / insideCnt
	outMean := 0.0
	if outsideCnt > 0 {
		outMean = outsideSum / outsideCnt
	}
	return (inMean - 0.65*outMean) + 0.15*(onsetBonus/float64(len(cues)))
}

func evaluateGridSignal(name string, cues []Cue, grid []float64, hop float64, scales []float64, opts Options, baseWeight float64) SignalVote {
	bestScore := -1e9
	bestOffset := 0.0
	bestScale := 1.0

	// Coarse sweep over every FPS hypothesis and offset in [-MaxShift, +MaxShift]
	type peak struct {
		scale, off, score float64
	}
	var coarsePeaks []peak
	for _, sc := range scales {
		for off := -opts.MaxShiftSec; off <= opts.MaxShiftSec+1e-9; off += opts.CoarseStepSec {
			s := scoreCuesOnGrid(cues, grid, hop, sc, off)
			// Slight parsimony prior favoring 1.0x FPS unless another rate clearly wins
			adj := s
			if math.Abs(sc-1.0) > 1e-5 {
				adj -= 0.025
			}
			coarsePeaks = append(coarsePeaks, peak{sc, off, adj})
			if adj > bestScore {
				bestScore = adj
				bestOffset = off
				bestScale = sc
			}
		}
	}

	// Fine local refinement around best coarse peak (±CoarseStep at FineStep)
	fineBest := scoreCuesOnGrid(cues, grid, hop, bestScale, bestOffset)
	fineOff := bestOffset
	for off := bestOffset - opts.CoarseStepSec*1.5; off <= bestOffset+opts.CoarseStepSec*1.5; off += opts.FineStepSec {
		s := scoreCuesOnGrid(cues, grid, hop, bestScale, off)
		if s > fineBest {
			fineBest = s
			fineOff = off
		}
	}

	// Measure peak-to-sidelobe SNR (best score >= 1.5s away from winner)
	secondBest := -1e9
	for _, p := range coarsePeaks {
		if p.scale == bestScale && math.Abs(p.off-fineOff) >= 1.5 && p.score > secondBest {
			secondBest = p.score
		}
	}
	if secondBest < -1e8 {
		secondBest = 0
	}
	margin := fineBest - secondBest
	snr := 0.0
	if fineBest > 0 {
		snr = margin / math.Max(0.05, math.Abs(secondBest)+0.15)
	}
	conf := clamp01(0.30 + 0.55*math.Tanh(margin*4.0) + 0.25*math.Tanh(snr*0.8))
	if fineBest <= 0.05 {
		conf *= 0.35
	}

	return SignalVote{
		Name:       name,
		OffsetSec:  roundMs(fineOff),
		Scale:      bestScale,
		PeakScore:  math.Round(fineBest*1000) / 1000,
		SNR:        math.Round(snr*100) / 100,
		Weight:     baseWeight,
		Confidence: math.Round(conf*100) / 100,
	}
}

func evaluateSceneCutSignal(cues []Cue, cuts []float64, scales []float64, opts Options, baseWeight float64) SignalVote {
	scoreAt := func(sc, off float64) float64 {
		if len(cuts) == 0 {
			return 0
		}
		var score float64
		for _, c := range cues {
			s := c.Start*sc + off
			e := c.End*sc + off
			// Binary search nearest cut near cue start and cue end
			idx := sort.SearchFloat64s(cuts, s)
			for _, ci := range []int{idx - 1, idx} {
				if ci >= 0 && ci < len(cuts) {
					d := s - cuts[ci]
					// Dialogue cleanly starts 0..350ms after a hard cut
					if d >= -0.05 && d <= 0.35 {
						score += 1.0 - math.Abs(d-0.12)/0.40
					}
				}
			}
			// Penalize cues straddling a hard cut right in the middle
			eIdx := sort.SearchFloat64s(cuts, e)
			for ci := idx; ci < eIdx && ci < len(cuts); ci++ {
				if cuts[ci] > s+0.35 && cuts[ci] < e-0.35 {
					score -= 0.45
				}
			}
		}
		return score / float64(len(cues))
	}

	bestScore, bestOff, bestScale := -1e9, 0.0, 1.0
	secondBest := -1e9
	for _, sc := range scales {
		for off := -opts.MaxShiftSec; off <= opts.MaxShiftSec+1e-9; off += opts.CoarseStepSec {
			s := scoreAt(sc, off)
			if math.Abs(sc-1.0) > 1e-5 {
				s -= 0.02
			}
			if s > bestScore {
				bestScore, bestOff, bestScale = s, off, sc
			}
		}
	}
	for off := bestOff - opts.CoarseStepSec; off <= bestOff+opts.CoarseStepSec; off += opts.FineStepSec {
		if s := scoreAt(bestScale, off); s > bestScore {
			bestScore, bestOff = s, off
		}
	}
	for off := -opts.MaxShiftSec; off <= opts.MaxShiftSec+1e-9; off += opts.CoarseStepSec * 2 {
		if math.Abs(off-bestOff) >= 1.5 {
			if s := scoreAt(bestScale, off); s > secondBest {
				secondBest = s
			}
		}
	}
	margin := bestScore - secondBest
	snr := margin / math.Max(0.05, math.Abs(secondBest)+0.1)
	conf := clamp01(0.25 + 0.55*math.Tanh(margin*5.0))
	return SignalVote{
		Name:       "Sahne Kesimi (Scene-Cut) Korelasyonu",
		OffsetSec:  roundMs(bestOff),
		Scale:      bestScale,
		PeakScore:  math.Round(bestScore*1000) / 1000,
		SNR:        math.Round(snr*100) / 100,
		Weight:     baseWeight,
		Confidence: math.Round(conf*100) / 100,
	}
}

// fuseDecisionMatrix combines independent votes using confidence-weighted
// consensus clustering and cross-validation.
func fuseDecisionMatrix(votes []SignalVote) (scale, offset, confidence float64, conflict bool) {
	// Effective weight = baseWeight * confidence * (1 + 0.25*min(snr, 3))
	effW := make([]float64, len(votes))
	totalW := 0.0
	for i, v := range votes {
		w := v.Weight * math.Max(0.1, v.Confidence) * (1.0 + 0.25*math.Min(3.0, math.Max(0, v.SNR)))
		effW[i] = w
		totalW += w
	}
	for i := range votes {
		if totalW > 0 {
			votes[i].Weight = math.Round((effW[i]/totalW)*100) / 100
		}
	}

	// Find the consensus cluster (largest combined weight within ±200ms)
	bestClusterW := -1.0
	bestIdx := 0
	for i := range votes {
		cw := 0.0
		for j := range votes {
			if math.Abs(votes[i].OffsetSec-votes[j].OffsetSec) <= 0.20 && math.Abs(votes[i].Scale-votes[j].Scale) <= 0.002 {
				cw += effW[j]
			}
		}
		if cw > bestClusterW {
			bestClusterW = cw
			bestIdx = i
		}
	}

	// Weighted average within the winning consensus cluster
	var wOff, wSum, wConf float64
	agreedCount := 0
	for i := range votes {
		if math.Abs(votes[i].OffsetSec-votes[bestIdx].OffsetSec) <= 0.20 && math.Abs(votes[i].Scale-votes[bestIdx].Scale) <= 0.002 {
			votes[i].Agreed = true
			agreedCount++
			wOff += votes[i].OffsetSec * effW[i]
			wConf += votes[i].Confidence * effW[i]
			wSum += effW[i]
		}
	}
	scale = votes[bestIdx].Scale
	if wSum > 0 {
		offset = wOff / wSum
		confidence = wConf / wSum
	} else {
		offset = votes[bestIdx].OffsetSec
		confidence = votes[bestIdx].Confidence
	}

	// Cross-validation bonus when multiple independent modalities agree;
	// conflict warning if a high-confidence modality disagrees by >500ms.
	if agreedCount >= 2 {
		confidence = clamp01(confidence + 0.14)
	}
	for _, v := range votes {
		if !v.Agreed && v.Confidence >= 0.65 && math.Abs(v.OffsetSec-offset) > 0.50 {
			conflict = true
			confidence = clamp01(confidence - 0.18)
		}
	}
	confidence = math.Round(confidence*100) / 100
	return scale, offset, confidence, conflict
}

// detectPiecewiseBreaks splits the episode at natural act/commercial-break
// silences (>= MinBreakGapSec) and checks if different acts have significantly
// different offsets (|Δoffset| >= 0.45s, e.g., TV eyecatch cut vs Blu-ray).
func detectPiecewiseBreaks(cues []Cue, grid []float64, hop, scale, globalOff float64, opts Options) []SegmentOffset {
	if len(cues) < 16 {
		return nil
	}
	// Candidate split points: silence gaps >= MinBreakGapSec with >=6 cues on each side
	var splitIndices []int
	for i := 6; i < len(cues)-6; i++ {
		gap := cues[i].Start - cues[i-1].End
		if gap >= opts.MinBreakGapSec {
			splitIndices = append(splitIndices, i)
		}
	}
	if len(splitIndices) == 0 {
		return nil
	}

	// Partition cues into acts using up to 3 largest gaps
	if len(splitIndices) > 3 {
		sort.Slice(splitIndices, func(i, j int) bool {
			gi := cues[splitIndices[i]].Start - cues[splitIndices[i]-1].End
			gj := cues[splitIndices[j]].Start - cues[splitIndices[j]-1].End
			return gi > gj
		})
		splitIndices = splitIndices[:3]
		sort.Ints(splitIndices)
	}

	bounds := append([]int{0}, splitIndices...)
	bounds = append(bounds, len(cues))

	type actEst struct {
		startIdx, endIdx int
		startT, endT     float64
		offset           float64
		scoreGain        float64
		conf             float64
	}
	var acts []actEst
	for b := 0; b < len(bounds)-1; b++ {
		sub := cues[bounds[b]:bounds[b+1]]
		if len(sub) < 4 {
			continue
		}
		baseScore := scoreCuesOnGrid(sub, grid, hop, scale, globalOff)
		bestScore, bestOff := baseScore, globalOff
		for off := -opts.MaxShiftSec; off <= opts.MaxShiftSec+1e-9; off += opts.CoarseStepSec {
			if s := scoreCuesOnGrid(sub, grid, hop, scale, off); s > bestScore {
				bestScore, bestOff = s, off
			}
		}
		for off := bestOff - opts.CoarseStepSec; off <= bestOff+opts.CoarseStepSec; off += opts.FineStepSec {
			if s := scoreCuesOnGrid(sub, grid, hop, scale, off); s > bestScore {
				bestScore, bestOff = s, off
			}
		}
		startT := 0.0
		if bounds[b] > 0 {
			startT = (cues[bounds[b]-1].End + cues[bounds[b]].Start) / 2.0
		}
		endT := 1e9
		if bounds[b+1] < len(cues) {
			endT = (cues[bounds[b+1]-1].End + cues[bounds[b+1]].Start) / 2.0
		}
		acts = append(acts, actEst{
			startIdx:  bounds[b],
			endIdx:    bounds[b+1],
			startT:    startT,
			endT:      endT,
			offset:    roundMs(bestOff),
			scoreGain: bestScore - baseScore,
			conf:      clamp01(0.45 + 0.55*math.Tanh(bestScore*2.5)),
		})
	}

	// Merge adjacent acts whose offsets agree within 0.45s (BIC parsimony)
	var merged []SegmentOffset
	for _, a := range acts {
		if len(merged) > 0 && math.Abs(merged[len(merged)-1].OffsetSec-a.offset) < 0.45 {
			merged[len(merged)-1].EndSec = a.endT
			continue
		}
		merged = append(merged, SegmentOffset{
			StartSec:   a.startT,
			EndSec:     a.endT,
			OffsetSec:  a.offset,
			Confidence: math.Round(a.conf*100) / 100,
		})
	}
	if len(merged) <= 1 {
		return nil
	}
	// Require meaningful total improvement over a single global offset
	var totalGain float64
	for _, a := range acts {
		totalGain += a.scoreGain
	}
	if totalGain < 0.12 {
		return nil
	}
	return merged
}

func formatSummaryTR(r AlignmentResult, conflict bool) string {
	pct := int(math.Round(r.Confidence * 100))
	var parts []string
	if conflict {
		parts = append(parts, fmt.Sprintf("⚠ %%%d güven (sinyaller kısmen ayrıştı · onay önerilir)", pct))
	} else {
		parts = append(parts, fmt.Sprintf("✓ %%%d güven", pct))
	}
	if len(r.Segments) > 1 {
		var segStrs []string
		for i, s := range r.Segments {
			segStrs = append(segStrs, fmt.Sprintf("Parça %d: %+.2f sn", i+1, s.OffsetSec))
		}
		parts = append(parts, fmt.Sprintf("%d parça (%s)", len(r.Segments), strings.Join(segStrs, " → ")))
	} else {
		parts = append(parts, fmt.Sprintf("kayma %+.2f sn", r.GlobalOffsetSec))
	}
	if math.Abs(r.Scale-1.0) > 1e-4 {
		parts = append(parts, fmt.Sprintf("FPS oranı %.5f×", r.Scale))
	}
	return strings.ReplaceAll(strings.Join(parts, " · "), ".", ",")
}

func roundMs(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
