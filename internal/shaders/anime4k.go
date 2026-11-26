// Package shaders manages two independent, GPU-friendly video pipelines for mpv
// using battle-tested open-source MIT shaders (embedded via go:embed):
//
//  1. Resolution Upscaling (non-AI, monitor-targeted): AMD FidelityFX Super
//     Resolution v1.0.2 (FSR.glsl, MIT) targeting your real display or explicit
//     1920x1080 (1080p), 2560x1440 (2K), or 3840x2160 (4K) output dimensions.
//  2. Quality Restoration & Smooth Motion (Frame-Gen):
//     - Bloc97/Anime4K v4.0.1 trained CNN shaders (MIT) for glassy-smooth,
//       anti-aliased anime line reconstruction (zero pixelated sharpening).
//     - mpv built-in GPU temporal interpolation (display-resample + oversample
//       / catmull_rom) for judder-free 60/120/144Hz camera motion.
package shaders

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"aniclitr/internal/config"
)

//go:embed glsl/*.glsl
var embeddedFS embed.FS

// Category 1: Real non-AI Resolution Upscaling (AMD FSR 1.0 EASU + RCAS).
const (
	ResOff  = "kapalı"
	ResAuto = "otomatik" // Match real display resolution (OUTPUT.w x OUTPUT.h)
	Res1080 = "1080p"    // Real 1920x1080 target
	Res2K   = "2k"       // Real 2560x1440 target
	Res4K   = "4k"       // Real 3840x2160 target
)

var ResProfiles = []string{ResOff, ResAuto, Res1080, Res2K, Res4K}

// Category 2A: Anime4K v4 CNN Quality Restoration (smooth, non-pixelated).
const (
	Off    = "kapalı"
	Fast   = "hızlı"  // Anime4K CNN-S (iGPU dostu · pürüzsüz netlik)
	Clear  = "cam"    // Anime4K CNN-Soft-M + Upscale-M (cam gibi berrak)
	Cinema = "sinema" // Anime4K Çift CNN A+A (düşük kalite kurtarıcı)

	// Legacy aliases kept so older configs & tests migrate cleanly.
	P1080    = Fast
	P2K      = Clear
	P4K      = Cinema
	Light    = Fast
	Balanced = Clear
	High     = Cinema
)

var Profiles = []string{Off, Fast, Clear, Cinema}

// Category 2B: Frame-Gen / Smooth Motion (mpv GPU temporal interpolation).
const (
	FGOff    = "kapalı"
	FGSmooth = "akıcı" // video-sync=display-resample + interpolation + tscale=oversample
	FG60     = "60fps" // video-sync=display-resample + interpolation + tscale=catmull_rom
)

var FGProfiles = []string{FGOff, FGSmooth, FG60}

// Pipeline holds the full independent video enhancement configuration.
type Pipeline struct {
	Res      string // Category 1: non-AI target resolution (AMD FSR 1.0)
	Enhance  string // Category 2A: Anime4K v4 CNN restoration
	FrameGen string // Category 2B: mpv GPU smooth motion / interpolation
}

func NormalizeRes(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "otomatik", "auto", "ekran":
		return ResAuto
	case "1080p", "fhd":
		return Res1080
	case "2k", "1440p", "qhd":
		return Res2K
	case "4k", "2160p", "uhd":
		return Res4K
	default:
		return ResOff
	}
}

func Normalize(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hızlı", "hizli", "1080p", "hafif":
		return Fast
	case "cam", "2k", "dengeli":
		return Clear
	case "sinema", "4k", "yüksek", "yuksek":
		return Cinema
	default:
		return Off
	}
}

func NormalizeFG(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "akıcı", "akici", "smooth":
		return FGSmooth
	case "60fps", "60+", "catmull":
		return FG60
	default:
		return FGOff
	}
}

func cycleList(list []string, cur string, dir int) string {
	for i, p := range list {
		if p == cur {
			return list[(i+dir+len(list))%len(list)]
		}
	}
	return list[0]
}

func NextRes(cur string) string { return cycleList(ResProfiles, NormalizeRes(cur), +1) }
func PrevRes(cur string) string { return cycleList(ResProfiles, NormalizeRes(cur), -1) }
func Next(cur string) string    { return cycleList(Profiles, Normalize(cur), +1) }
func Prev(cur string) string    { return cycleList(Profiles, Normalize(cur), -1) }
func NextFG(cur string) string  { return cycleList(FGProfiles, NormalizeFG(cur), +1) }
func PrevFG(cur string) string  { return cycleList(FGProfiles, NormalizeFG(cur), -1) }

func ShortResLabel(v string) string {
	switch NormalizeRes(v) {
	case ResAuto:
		return "Otomatik"
	case Res1080:
		return "1080p"
	case Res2K:
		return "2K (1440p)"
	case Res4K:
		return "4K (2160p)"
	default:
		return "Kapalı"
	}
}

func ResLabel(v string) string {
	switch NormalizeRes(v) {
	case ResAuto:
		return "Otomatik · AMD FSR (monitör çözünürlüğü)"
	case Res1080:
		return "1080p · AMD FSR (1920×1080 gerçek ölçek)"
	case Res2K:
		return "2K · AMD FSR (2560×1440 gerçek ölçek)"
	case Res4K:
		return "4K · AMD FSR (3840×2160 gerçek ölçek)"
	default:
		return "Kapalı (orijinal çözünürlük)"
	}
}

func ShortLabel(v string) string {
	switch Normalize(v) {
	case Fast:
		return "Anime4K Hızlı"
	case Clear:
		return "Anime4K Cam"
	case Cinema:
		return "Anime4K Sinema"
	default:
		return "Kapalı"
	}
}

func Label(v string) string {
	switch Normalize(v) {
	case Fast:
		return "Anime4K Hızlı (CNN-S · iGPU dostu)"
	case Clear:
		return "Anime4K Cam (CNN-Soft-M · pürüzsüz berraklık)"
	case Cinema:
		return "Anime4K Sinema (Çift CNN · maksimum onarım)"
	default:
		return "Kapalı"
	}
}

func ShortFGLabel(v string) string {
	switch NormalizeFG(v) {
	case FGSmooth:
		return "Akıcı"
	case FG60:
		return "60+ FPS"
	default:
		return "Kapalı"
	}
}

func FGLabel(v string) string {
	switch NormalizeFG(v) {
	case FGSmooth:
		return "Akıcı · Smooth Motion (sıfır judder, oversample)"
	case FG60:
		return "60+ FPS · Catmull-Rom (yumuşak ara kare)"
	default:
		return "Kapalı (orijinal 24 fps)"
	}
}

func shaderDir() string {
	return filepath.Join(config.CacheDir(), "shaders", "v4")
}

func ensureShader(name, src string) (string, error) {
	dir := shaderDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if cur, err := os.ReadFile(p); err == nil && string(cur) == src {
		return p, nil
	}
	if err := config.WriteAtomic(p, []byte(src)); err != nil {
		return "", err
	}
	return p, nil
}

func readEmbedded(name string) (string, error) {
	b, err := embeddedFS.ReadFile("glsl/" + name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// buildTargetFSR adapts AMD's official MIT FSR.glsl to target an explicit
// resolution (1920x1080, 2560x1440, 3840x2160) or OUTPUT (monitor native).
func buildTargetFSR(res string) (name, src string, err error) {
	raw, err := readEmbedded("FSR.glsl")
	if err != nil {
		return "", "", err
	}
	var tw, th int
	switch NormalizeRes(res) {
	case ResAuto:
		return "FSR_auto.glsl", raw, nil
	case Res1080:
		tw, th, name = 1920, 1080, "FSR_1080p.glsl"
	case Res2K:
		tw, th, name = 2560, 1440, "FSR_2k_1440p.glsl"
	case Res4K:
		tw, th, name = 3840, 2160, "FSR_4k_2160p.glsl"
	default:
		return "", "", nil
	}
	// Replace OUTPUT.w / OUTPUT.h in the mpv //!WHEN / //!WIDTH / //!HEIGHT directives
	// with the real target resolution so mpv genuinely upscales LUMA to 1080p/2K/4K.
	lines := strings.Split(raw, "\n")
	wStr, hStr := fmt.Sprintf("%d", tw), fmt.Sprintf("%d", th)
	for i, l := range lines {
		if strings.HasPrefix(l, "//!WHEN ") || strings.HasPrefix(l, "//!WIDTH ") || strings.HasPrefix(l, "//!HEIGHT ") {
			l = strings.ReplaceAll(l, "OUTPUT.w", wStr)
			l = strings.ReplaceAll(l, "OUTPUT.h", hStr)
			lines[i] = l
		}
	}
	return name, strings.Join(lines, "\n"), nil
}

// PipelinePaths materialises and returns the ordered GLSL shader paths for p.
// Anime4K restoration runs first on native LUMA, followed by AMD FSR resolution scaling.
func PipelinePaths(p Pipeline) ([]string, error) {
	enh := Normalize(p.Enhance)
	res := NormalizeRes(p.Res)

	var names []string
	switch enh {
	case Fast:
		names = []string{
			"Anime4K_Clamp_Highlights.glsl",
			"Anime4K_Restore_CNN_S.glsl",
			"Anime4K_Upscale_CNN_x2_S.glsl",
			"Anime4K_AutoDownscalePre_x2.glsl",
		}
	case Clear:
		names = []string{
			"Anime4K_Clamp_Highlights.glsl",
			"Anime4K_Restore_CNN_Soft_M.glsl",
			"Anime4K_Upscale_CNN_x2_M.glsl",
			"Anime4K_AutoDownscalePre_x2.glsl",
		}
	case Cinema:
		names = []string{
			"Anime4K_Clamp_Highlights.glsl",
			"Anime4K_Restore_CNN_M.glsl",
			"Anime4K_Upscale_CNN_x2_M.glsl",
			"Anime4K_AutoDownscalePre_x2.glsl",
			"Anime4K_Restore_CNN_S.glsl",
			"Anime4K_Upscale_CNN_x2_S.glsl",
		}
	}

	var out []string
	for _, n := range names {
		src, err := readEmbedded(n)
		if err != nil {
			return nil, err
		}
		path, err := ensureShader(n, src)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}

	if res != ResOff {
		fname, fsrc, err := buildTargetFSR(res)
		if err != nil {
			return nil, err
		}
		if fname != "" {
			fp, err := ensureShader(fname, fsrc)
			if err != nil {
				return nil, err
			}
			out = append(out, fp)
			// For 4K on very low-res (360p/480p) sources, a second EASU pass cleanly bridges >2x scale.
			if res == Res4K && enh == Off {
				fp2, err := ensureShader("FSR_4k_2160p_p2.glsl", fsrc)
				if err == nil {
					out = append(out, fp2)
				}
			}
		}
	}
	return out, nil
}

// Paths keeps backward compatibility for single-profile callers/tests.
func Paths(profile string) ([]string, error) {
	return PipelinePaths(Pipeline{Enhance: profile})
}

// ShaderList joins shader paths using mpv's platform list separator.
func ShaderList(profile string) (string, error) {
	return PipelineShaderList(Pipeline{Enhance: profile})
}

// PipelineShaderList joins all shader paths for p using mpv's platform list separator.
func PipelineShaderList(p Pipeline) (string, error) {
	ps, err := PipelinePaths(p)
	if err != nil || len(ps) == 0 {
		return "", err
	}
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	return strings.Join(ps, sep), nil
}

// PipelineArgs returns zero-copy, GPU-friendly mpv arguments for Resolution
// Upscaling, Anime4K CNN Quality Restoration, and Smooth Motion Frame-Gen.
func PipelineArgs(p Pipeline) []string {
	enh := Normalize(p.Enhance)
	res := NormalizeRes(p.Res)
	fg := NormalizeFG(p.FrameGen)
	if enh == Off && res == ResOff && fg == FGOff {
		return nil
	}
	var args []string
	if enh != Off || res != ResOff {
		list, err := PipelineShaderList(p)
		if err == nil && list != "" {
			args = append(args,
				"--hwdec=auto-safe",
				"--deband=yes",
				"--deband-iterations=1",
				"--deband-threshold=35",
				"--deband-range=16",
				"--cscale=spline36",
				"--dscale=mitchell",
				"--temporal-dither=no",
				"--glsl-shaders="+list,
			)
			if res == Res4K || enh == Cinema {
				args = append(args, "--scale=ewa_lanczossharp", "--sigmoid-upscaling=yes")
			} else {
				args = append(args, "--scale=spline36")
			}
		}
	}
	switch fg {
	case FGSmooth:
		args = append(args,
			"--video-sync=display-resample",
			"--interpolation=yes",
			"--tscale=oversample",
		)
	case FG60:
		args = append(args,
			"--video-sync=display-resample",
			"--interpolation=yes",
			"--tscale=catmull_rom",
			"--tscale-clamp=0.0",
		)
	}
	return args
}

// MpvArgs keeps backward compatibility for callers passing a single quality profile.
func MpvArgs(profile string) []string {
	return PipelineArgs(Pipeline{Enhance: profile})
}

// SummaryBadge returns a compact Turkish telemetry badge for headers (e.g. "2K · Cam · Akıcı").
func SummaryBadge(p Pipeline) string {
	var parts []string
	switch NormalizeRes(p.Res) {
	case ResAuto:
		parts = append(parts, "Oto")
	case Res1080:
		parts = append(parts, "1080p")
	case Res2K:
		parts = append(parts, "2K")
	case Res4K:
		parts = append(parts, "4K")
	}
	if e := Normalize(p.Enhance); e != Off {
		switch e {
		case Fast:
			parts = append(parts, "Hızlı")
		case Clear:
			parts = append(parts, "Cam")
		case Cinema:
			parts = append(parts, "Sinema")
		}
	}
	if f := NormalizeFG(p.FrameGen); f != FGOff {
		parts = append(parts, ShortFGLabel(f))
	}
	if len(parts) == 0 {
		return "Orijinal"
	}
	return strings.Join(parts, " · ")
}
