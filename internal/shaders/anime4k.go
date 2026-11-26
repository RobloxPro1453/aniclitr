// Package shaders generates and manages hyper-optimized Anime4K-inspired GLSL
// shaders for mpv's GPU renderer. Shaders are embedded directly in the binary
// and materialised lazily into the cache dir (content-checked, zero rewrites).
package shaders

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"aniclitr/internal/config"
)

// Profiles supported by Settings → Oynatma → Görüntü iyileştirme (Anime4K).
const (
	Off      = "kapalı"
	Light    = "hafif"   // 1-pass CAS + thin-line sharpen (60fps on laptop iGPU)
	Balanced = "dengeli" // 2-pass bilateral denoise + directional luma upscale
	High     = "yüksek"  // 3-pass denoise + line restore + 2x directional luma upscale
)

// Profiles is the ordered cycle list for settings and live mpv Alt+4 toggle.
var Profiles = []string{Off, Light, Balanced, High}

// Next cycles to the next Anime4K profile.
func Next(cur string) string {
	for i, p := range Profiles {
		if p == cur {
			return Profiles[(i+1)%len(Profiles)]
		}
	}
	return Light
}

// Label returns a human-readable Turkish description of the profile.
func Label(profile string) string {
	switch profile {
	case Light:
		return "Hafif (iGPU dostu · 1 geçiş)"
	case Balanced:
		return "Dengeli (gürültü giderme + 2× netleştirme)"
	case High:
		return "Yüksek (çizgi onarım + 2× kenar keskinleştirme)"
	default:
		return "Kapalı"
	}
}

func shaderDir() string {
	return filepath.Join(config.CacheDir(), "shaders")
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

// Paths writes (if needed) and returns the ordered GLSL shader file paths for profile.
func Paths(profile string) ([]string, error) {
	var specs []struct{ name, src string }
	switch profile {
	case Light:
		specs = []struct{ name, src string }{
			{"aniclitr_a4k_fast_cas.glsl", glslFastCAS},
		}
	case Balanced:
		specs = []struct{ name, src string }{
			{"aniclitr_a4k_denoise.glsl", glslBilateralDenoise},
			{"aniclitr_a4k_upscale_luma.glsl", glslDirectionalUpscale},
		}
	case High:
		specs = []struct{ name, src string }{
			{"aniclitr_a4k_denoise.glsl", glslBilateralDenoise},
			{"aniclitr_a4k_restore.glsl", glslLineRestore},
			{"aniclitr_a4k_upscale_luma.glsl", glslDirectionalUpscale},
		}
	default:
		return nil, nil
	}
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		p, err := ensureShader(s.name, s.src)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ShaderList joins shader paths using mpv's platform list separator (';' on Windows, ':' on Unix).
func ShaderList(profile string) (string, error) {
	ps, err := Paths(profile)
	if err != nil || len(ps) == 0 {
		return "", err
	}
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	return strings.Join(ps, sep), nil
}

// MpvArgs returns optimized GPU pipeline flags + --glsl-shaders for the chosen profile.
// Returns nil when profile is Off.
func MpvArgs(profile string) []string {
	list, err := ShaderList(profile)
	if err != nil || list == "" {
		return nil
	}
	args := []string{
		"--hwdec=auto-copy",
		"--deband=yes",
		"--deband-iterations=2",
		"--deband-threshold=35",
		"--cscale=bilinear",
		"--glsl-shaders=" + list,
	}
	if profile == High {
		args = append(args, "--scale=ewa_lanczossharp", "--dscale=mitchell")
	} else {
		args = append(args, "--scale=spline36")
	}
	return args
}

// ---- Embedded optimized mpv GLSL shaders ----

// 1. Ultra-fast Contrast Adaptive Sharpening (CAS) tuned for anime cel lines.
// Only 5 texture fetches per pixel — runs at 60+ FPS even on Intel UHD / Vega iGPUs.
const glslFastCAS = `//!DESC aniclitr Anime4K-Fast CAS
//!HOOK MAIN
//!BIND HOOKED

#define SHARPNESS 0.62

vec4 hook() {
    vec2 pt = HOOKED_pt;
    vec3 c = HOOKED_texOff(vec2(0.0, 0.0)).rgb;
    vec3 n = HOOKED_texOff(vec2(0.0, -1.0)).rgb;
    vec3 s = HOOKED_texOff(vec2(0.0,  1.0)).rgb;
    vec3 w = HOOKED_texOff(vec2(-1.0, 0.0)).rgb;
    vec3 e = HOOKED_texOff(vec2( 1.0, 0.0)).rgb;

    vec3 mn = min(c, min(min(n, s), min(w, e)));
    vec3 mx = max(c, max(max(n, s), max(w, e)));
    vec3 amp = clamp(min(mn, 1.0 - mx) / max(mx, vec3(1e-4)), 0.0, 1.0);
    amp = sqrt(amp);

    vec3 weight = -amp * (0.125 + 0.075 * SHARPNESS);
    vec3 outCol = (c + (n + s + w + e) * weight) / (1.0 + 4.0 * weight);
    return vec4(clamp(outCol, mn, mx), 1.0);
}
`

// 2. Lightweight edge-preserving bilateral denoiser: strips anime H.264/streaming
// compression mosquito noise on flat cel regions while keeping dark outlines intact.
const glslBilateralDenoise = `//!DESC aniclitr Anime4K Bilateral Denoise
//!HOOK LUMA
//!BIND HOOKED

#define SIGMA_I 0.045

vec4 hook() {
    float c = HOOKED_texOff(vec2(0.0, 0.0)).x;
    float sum = c;
    float wsum = 1.0;

    vec2 offs[8] = vec2[8](
        vec2(-1.0, 0.0), vec2(1.0, 0.0), vec2(0.0, -1.0), vec2(0.0, 1.0),
        vec2(-1.0, -1.0), vec2(1.0, -1.0), vec2(-1.0, 1.0), vec2(1.0, 1.0)
    );
    for (int i = 0; i < 8; i++) {
        float v = HOOKED_texOff(offs[i]).x;
        float d = (v - c) / SIGMA_I;
        float spatial = (i < 4) ? 0.75 : 0.45;
        float w = spatial * exp(-0.5 * d * d);
        sum += v * w;
        wsum += w;
    }
    return vec4(sum / wsum, 0.0, 0.0, 1.0);
}
`

// 3. Thin-line reconstruction + dark-line crispness pass for soft 720p/1080p encodes.
const glslLineRestore = `//!DESC aniclitr Anime4K Line Restore
//!HOOK LUMA
//!BIND HOOKED

#define STRENGTH 0.32

vec4 hook() {
    float tl = HOOKED_texOff(vec2(-1.0, -1.0)).x;
    float tc = HOOKED_texOff(vec2( 0.0, -1.0)).x;
    float tr = HOOKED_texOff(vec2( 1.0, -1.0)).x;
    float ml = HOOKED_texOff(vec2(-1.0,  0.0)).x;
    float mc = HOOKED_texOff(vec2( 0.0,  0.0)).x;
    float mr = HOOKED_texOff(vec2( 1.0,  0.0)).x;
    float bl = HOOKED_texOff(vec2(-1.0,  1.0)).x;
    float bc = HOOKED_texOff(vec2( 0.0,  1.0)).x;
    float br = HOOKED_texOff(vec2( 1.0,  1.0)).x;

    float gx = -tl - 2.0 * ml - bl + tr + 2.0 * mr + br;
    float gy = -tl - 2.0 * tc - tr + bl + 2.0 * bc + br;
    float edge = clamp(sqrt(gx * gx + gy * gy), 0.0, 1.0);

    float localMin = min(min(min(tl, tr), min(bl, br)), min(min(tc, bc), min(ml, mr)));
    float localMax = max(max(max(tl, tr), max(bl, br)), max(max(tc, bc), max(ml, mr)));

    // Gently tighten dark cel outlines without creating bright ringing halos.
    float target = mix(mc, localMin, STRENGTH * edge * smoothstep(0.15, 0.65, localMax - localMin));
    return vec4(clamp(target, localMin, localMax), 0.0, 0.0, 1.0);
}
`

// 4. Directional 2x luma upscaler with anti-ringing clamp for razor-sharp diagonal lines.
const glslDirectionalUpscale = `//!DESC aniclitr Anime4K 2x Directional Luma Upscale
//!HOOK LUMA
//!BIND HOOKED
//!WIDTH LUMA.w 2 *
//!HEIGHT LUMA.h 2 *

vec4 hook() {
    vec2 pos = HOOKED_pos;
    vec2 pt = HOOKED_pt;

    float c  = HOOKED_tex(pos).x;
    float n  = HOOKED_tex(pos + vec2(0.0, -pt.y)).x;
    float s  = HOOKED_tex(pos + vec2(0.0,  pt.y)).x;
    float w  = HOOKED_tex(pos + vec2(-pt.x, 0.0)).x;
    float e  = HOOKED_tex(pos + vec2( pt.x, 0.0)).x;
    float nw = HOOKED_tex(pos + vec2(-pt.x, -pt.y)).x;
    float ne = HOOKED_tex(pos + vec2( pt.x, -pt.y)).x;
    float sw = HOOKED_tex(pos + vec2(-pt.x,  pt.y)).x;
    float se = HOOKED_tex(pos + vec2( pt.x,  pt.y)).x;

    float mn = min(c, min(min(n, s), min(w, e)));
    float mx = max(c, max(max(n, s), max(w, e)));

    float d1 = abs(nw - se);
    float d2 = abs(ne - sw);
    float dh = abs(w - e);
    float dv = abs(n - s);

    float dir = c;
    if (d1 < d2 && d1 < dh && d1 < dv) {
        dir = mix(c, 0.5 * (nw + se), 0.25);
    } else if (d2 < d1 && d2 < dh && d2 < dv) {
        dir = mix(c, 0.5 * (ne + sw), 0.25);
    }
    float sharp = c + (c - 0.25 * (n + s + w + e)) * 0.45;
    float res = mix(dir, sharp, 0.55);
    return vec4(clamp(res, mn, mx), 0.0, 0.0, 1.0);
}
`
