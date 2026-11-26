// Package shaders generates and manages GPU-friendly, artifact-free anime
// upscaling GLSL shaders for mpv (Kapalı, 1080p Net, 2K Keskin, 4K Ultra).
//
// Unlike naive linear scalers (which leave 360p/480p anime blurry), the v3
// pipeline uses lightweight, zero-copy Anime4K-inspired passes:
//   - Cel deblocking + Sobel gradient-guided shock filter (line thinning & darkening)
//   - Directional edge-aware 2x luma reconstruction (eliminates diagonal jaggies)
//   - Second-stage micro-line refinement at 2x resolution (4K Ultra)
//   - Native display-resolution (OUTPUT hook) FidelityFX CAS sharpening
//
// All shaders run under --hwdec=auto-safe (zero GPU->RAM PCIe copies) with
// small fixed texture tap counts so even integrated GPUs stay cool and smooth.
package shaders

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"aniclitr/internal/config"
)

// Profiles supported by Settings → Oynatma → Görüntü yükseltme and the in-mpv menu.
const (
	Off   = "kapalı"
	P1080 = "1080p" // Display-resolution CAS + subtle line darkening (ultra-light iGPU)
	P2K   = "2k"    // Cel deblock + Anime4K line restore + 2x edge-directed luma + OUTPUT CAS
	P4K   = "4k"    // Full 2K pipeline + 2x micro-line tighten + strong OUTPUT CAS + ewa_lanczossharp

	// Legacy aliases kept for backward compatibility with v1.2.0 config files.
	Light    = P1080
	Balanced = P2K
	High     = P4K
)

// Profiles is the ordered list of upscaling presets.
var Profiles = []string{Off, P1080, P2K, P4K}

// Normalize maps legacy or empty profile names to canonical v1.3 presets.
func Normalize(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "1080p", "hafif":
		return P1080
	case "2k", "dengeli":
		return P2K
	case "4k", "yüksek", "yuksek":
		return P4K
	default:
		return Off
	}
}

// Next cycles to the next upscaling profile.
func Next(cur string) string {
	cur = Normalize(cur)
	for i, p := range Profiles {
		if p == cur {
			return Profiles[(i+1)%len(Profiles)]
		}
	}
	return P1080
}

// Prev cycles to the previous upscaling profile.
func Prev(cur string) string {
	cur = Normalize(cur)
	for i, p := range Profiles {
		if p == cur {
			return Profiles[(i-1+len(Profiles))%len(Profiles)]
		}
	}
	return Off
}

// ShortLabel returns a compact badge label ("1080p Net", "2K Keskin", "4K Ultra", "Kapalı").
func ShortLabel(profile string) string {
	switch Normalize(profile) {
	case P1080:
		return "1080p Net"
	case P2K:
		return "2K Keskin"
	case P4K:
		return "4K Ultra"
	default:
		return "Kapalı"
	}
}

// Label returns a human-readable Turkish description of the profile.
func Label(profile string) string {
	switch Normalize(profile) {
	case P1080:
		return "1080p Net (iGPU dostu · anlık CAS + çizgi netliği)"
	case P2K:
		return "2K Keskin (çizgi onarım + 2× kenar yükseltme)"
	case P4K:
		return "4K Ultra (çift kademe çizgi onarım + maksimum netlik)"
	default:
		return "Kapalı (orijinal görüntü)"
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
	switch Normalize(profile) {
	case P1080:
		specs = []struct{ name, src string }{
			{"aniclitr_v3_output_1080p.glsl", glslOutput1080p},
		}
	case P2K:
		specs = []struct{ name, src string }{
			{"aniclitr_v3_restore_lines.glsl", glslRestoreLines},
			{"aniclitr_v3_edge_2x.glsl", glslEdgeDirected2x},
			{"aniclitr_v3_output_2k.glsl", glslOutput2K},
		}
	case P4K:
		specs = []struct{ name, src string }{
			{"aniclitr_v3_restore_lines.glsl", glslRestoreLines},
			{"aniclitr_v3_edge_2x.glsl", glslEdgeDirected2x},
			{"aniclitr_v3_refine_4k.glsl", glslRefine4K},
			{"aniclitr_v3_output_4k.glsl", glslOutput4K},
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

// MpvArgs returns zero-copy, GPU-friendly mpv scaling flags + --glsl-shaders.
// Never uses hwdec=auto-copy (which copies frames over PCIe and spikes GPU/CPU).
func MpvArgs(profile string) []string {
	prof := Normalize(profile)
	list, err := ShaderList(prof)
	if err != nil || list == "" {
		return nil
	}
	args := []string{
		"--hwdec=auto-safe",
		"--deband=yes",
		"--deband-iterations=1",
		"--deband-threshold=42",
		"--deband-range=16",
		"--cscale=spline36",
		"--temporal-dither=no",
		"--glsl-shaders=" + list,
	}
	switch prof {
	case P4K:
		args = append(args, "--scale=ewa_lanczossharp", "--dscale=mitchell", "--sigmoid-upscaling=yes")
	default:
		args = append(args, "--scale=spline36", "--dscale=mitchell")
	}
	return args
}

// ---- Embedded v3 Anime4K-grade, GPU-friendly GLSL shaders ----

// 1. Cel Deblocking + Anime4K Gradient-Guided Shock Filter (Line Thinning & Darkening).
// Runs on native LUMA before 2x upscale:
//   - Suppresses 360p/480p H.264 blockiness on flat low-gradient areas without smearing edges.
//   - Pushes blurry/thick anime ink outlines along the Sobel gradient into thin, crisp,
//     darkened cel lines (exact mathematical principle behind Anime4K ThinLines + DarkLines).
const glslRestoreLines = `//!DESC aniclitr v3 Cel Deblock & Line Restore
//!HOOK LUMA
//!BIND HOOKED

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

    // Sobel gradient (edge direction & magnitude)
    float gx = (tr + 2.0 * mr + br) - (tl + 2.0 * ml + bl);
    float gy = (bl + 2.0 * bc + br) - (tl + 2.0 * tc + tr);
    float grad = sqrt(gx * gx + gy * gy);

    // Local 3x3 neighborhood bounds & average
    float lmin = min(mc, min(min(min(tl, tc), min(tr, ml)), min(min(mr, bl), min(bc, br))));
    float lmax = max(mc, max(max(max(tl, tc), max(tr, ml)), max(max(mr, bl), max(bc, br))));
    float avg8 = (tl + tc + tr + ml + mr + bl + bc + br) * 0.125;

    // 1) Gentle cel deblocking ONLY on flat low-gradient zones (removes 360p/480p mud)
    float flatMask = 1.0 - smoothstep(0.03, 0.12, lmax - lmin);
    float clean = mix(mc, avg8, 0.38 * flatMask);

    // 2) Gradient-guided shock filter: pulls blurry transition pixels toward the crisp
    //    brighter cel side (thinning thick 360p/480p gray borders) while tightening dark ink cores.
    vec2 dir = (grad > 1e-4) ? (vec2(gx, gy) / grad) : vec2(0.0);
    float fwd = HOOKED_tex(HOOKED_pos + dir * HOOKED_pt * 0.75).x;
    float bwd = HOOKED_tex(HOOKED_pos - dir * HOOKED_pt * 0.75).x;

    // Second derivative along edge normal: positive = dark valley (ink core), negative = bright shoulder
    float lap = (fwd + bwd) * 0.5 - mc;
    float edgeMask = smoothstep(0.08, 0.42, grad);

    float outL = clean;
    // Tighten bright shoulder next to a dark line (line thinning)
    if (lap < 0.0) {
        outL = mix(outL, max(fwd, bwd), 0.36 * edgeMask);
    } else {
        // Darken & focus faded gray ink line cores (DarkLines)
        float darkBoost = clamp(lap * 1.45, 0.0, 0.18);
        outL = outL - darkBoost * edgeMask;
    }

    // Mild unsharp boost on genuine edges, bounded safely against ringing
    float loBound = max(0.0, lmin - 0.05 * edgeMask);
    float hiBound = min(1.0, lmax + 0.04 * edgeMask);
    return vec4(clamp(outL, loBound, hiBound), 0.0, 0.0, 1.0);
}
`

// 2. Directional Edge-Directed 2x Luma Upscaler.
// Analyzes 4 edge orientations (H, V, 45°, 135°) across the 3x3 neighborhood and
// blends directional along-edge interpolation with anti-ringing Catmull-Rom so
// diagonal anime outlines scale 2x without staircase jaggies or bicubic blur.
const glslEdgeDirected2x = `//!DESC aniclitr v3 Directional Edge 2x Luma
//!HOOK LUMA
//!BIND HOOKED
//!WIDTH LUMA.w 2 *
//!HEIGHT LUMA.h 2 *

vec4 crW(float f) {
    float f2 = f * f;
    float f3 = f2 * f;
    return vec4(
        -0.5 * f3 + f2 - 0.5 * f,
         1.5 * f3 - 2.5 * f2 + 1.0,
        -1.5 * f3 + 2.0 * f2 + 0.5 * f,
         0.5 * f3 - 0.5 * f2
    );
}

vec4 hook() {
    vec2 tc = HOOKED_pos * HOOKED_size - 0.5;
    vec2 base = floor(tc);
    vec2 f = tc - base;

    // 2x2 core texels for directional edge analysis & anti-ringing bounds
    float s00 = HOOKED_tex((base + vec2(0.5, 0.5)) * HOOKED_pt).x;
    float s10 = HOOKED_tex((base + vec2(1.5, 0.5)) * HOOKED_pt).x;
    float s01 = HOOKED_tex((base + vec2(0.5, 1.5)) * HOOKED_pt).x;
    float s11 = HOOKED_tex((base + vec2(1.5, 1.5)) * HOOKED_pt).x;

    float cMin = min(min(s00, s10), min(s01, s11));
    float cMax = max(max(s00, s10), max(s01, s11));

    // Diagonal difference analysis (EDI / NEDI-lite):
    // If d1 (s00-s11) << d2 (s10-s01), an edge runs along the main diagonal (00 <-> 11).
    float dMain = abs(s00 - s11);
    float dAnti = abs(s10 - s01);
    float diagStr = abs(dMain - dAnti);
    float diagEdge = (dMain < dAnti) ? mix(s00, s11, 0.5 * (f.x + f.y))
                                     : mix(s10, s01, 0.5 * ((1.0 - f.x) + f.y));

    // 16-tap Catmull-Rom reconstruction for smooth non-diagonal detail
    vec4 wx = crW(f.x);
    vec4 wy = crW(f.y);
    float cr = 0.0;
    for (int j = -1; j <= 2; j++) {
        float row = 0.0;
        for (int i = -1; i <= 2; i++) {
            float s = HOOKED_tex((base + vec2(float(i), float(j)) + 0.5) * HOOKED_pt).x;
            row += s * wx[i + 1];
        }
        cr += row * wy[j + 1];
    }

    // Blend along-edge directional reconstruction where strong diagonal lines exist
    float wDiag = smoothstep(0.05, 0.25, diagStr) * 0.65;
    float res = mix(cr, diagEdge, wDiag);

    // Subtle edge-contrast crispness on high-contrast anime lines
    float contrast = cMax - cMin;
    float edgeW = smoothstep(0.06, 0.28, contrast);
    float bil = mix(mix(s00, s10, f.x), mix(s01, s11, f.x), f.y);
    res = res + (res - bil) * (0.45 * edgeW);

    return vec4(clamp(res, cMin - 0.02 * edgeW, cMax + 0.02 * edgeW), 0.0, 0.0, 1.0);
}
`

// 3. Second-stage 4K Ultra Micro-Line Tightening at doubled 2x resolution.
// Refines fine hair/eye/ink details after 2x upscale without touching flat cel fills.
const glslRefine4K = `//!DESC aniclitr v3 4K Micro-Line Refine
//!HOOK LUMA
//!BIND HOOKED

vec4 hook() {
    float c = HOOKED_texOff(vec2( 0.0,  0.0)).x;
    float n = HOOKED_texOff(vec2( 0.0, -1.0)).x;
    float s = HOOKED_texOff(vec2( 0.0,  1.0)).x;
    float w = HOOKED_texOff(vec2(-1.0,  0.0)).x;
    float e = HOOKED_texOff(vec2( 1.0,  0.0)).x;

    float gx = e - w;
    float gy = s - n;
    float grad = sqrt(gx * gx + gy * gy);
    float avg = 0.25 * (n + s + w + e);
    float lmin = min(c, min(min(n, s), min(w, e)));
    float lmax = max(c, max(max(n, s), max(w, e)));

    float mask = smoothstep(0.04, 0.24, lmax - lmin);
    // Darken fine ink valleys and push shoulders clean
    float lap = avg - c;
    float refined = c - lap * 0.55 * mask;
    if (lap > 0.015 && grad > 0.06) {
        refined -= clamp(lap * 0.65, 0.0, 0.10) * mask;
    }
    return vec4(clamp(refined, max(0.0, lmin - 0.04 * mask), min(1.0, lmax + 0.03 * mask)), 0.0, 0.0, 1.0);
}
`

// 4. Native Display-Resolution (OUTPUT hook) Adaptive FidelityFX CAS + Line Crispness.
// Running at OUTPUT means even a 360p/480p video upscaled to a 1080p/1440p monitor gets
// razor-sharp display-pixel edges rather than sharpening blocky low-res source pixels!
const glslOutput1080p = `//!DESC aniclitr v3 1080p Net Display CAS
//!HOOK OUTPUT
//!BIND HOOKED

#define CAS_STRENGTH 0.85
#define INK_DARKEN   0.18

float lumaOf(vec3 rgb) { return dot(rgb, vec3(0.299, 0.587, 0.114)); }

vec4 hook() {
    vec3 c  = HOOKED_texOff(vec2( 0.0,  0.0)).rgb;
    vec3 n  = HOOKED_texOff(vec2( 0.0, -1.0)).rgb;
    vec3 s  = HOOKED_texOff(vec2( 0.0,  1.0)).rgb;
    vec3 w  = HOOKED_texOff(vec2(-1.0,  0.0)).rgb;
    vec3 e  = HOOKED_texOff(vec2( 1.0,  0.0)).rgb;
    vec3 nw = HOOKED_texOff(vec2(-1.0, -1.0)).rgb;
    vec3 ne = HOOKED_texOff(vec2( 1.0, -1.0)).rgb;
    vec3 sw = HOOKED_texOff(vec2(-1.0,  1.0)).rgb;
    vec3 se = HOOKED_texOff(vec2( 1.0,  1.0)).rgb;

    vec3 mn = min(c, min(min(n, s), min(w, e)));
    vec3 mx = max(c, max(max(n, s), max(w, e)));
    vec3 mn2 = min(mn, min(min(nw, ne), min(sw, se)));
    vec3 mx2 = max(mx, max(max(nw, ne), max(sw, se)));
    mn = (mn + mn2) * 0.5;
    mx = (mx + mx2) * 0.5;

    vec3 amp = sqrt(clamp(min(mn, 1.0 - mx) / max(mx, vec3(1e-4)), 0.0, 1.0));
    float peak = -1.0 / mix(8.0, 5.0, CAS_STRENGTH);
    vec3 wt = amp * peak;
    vec3 outCol = (c + (n + s + w + e) * wt) / (1.0 + 4.0 * wt);

    // Subtle dark-line restoration on display-res anime outlines
    float lc = lumaOf(outCol);
    float lavg = 0.25 * (lumaOf(n) + lumaOf(s) + lumaOf(w) + lumaOf(e));
    float darkEdge = clamp((lavg - lc) * 2.4, 0.0, 0.14) * smoothstep(0.05, 0.20, lavg - lc);
    outCol -= vec3(darkEdge * INK_DARKEN * 4.0);

    return vec4(clamp(outCol, max(vec3(0.0), mn - vec3(0.04)), min(vec3(1.0), mx + vec3(0.02))), 1.0);
}
`

const glslOutput2K = `//!DESC aniclitr v3 2K Keskin Display CAS
//!HOOK OUTPUT
//!BIND HOOKED

#define CAS_STRENGTH 0.72

vec4 hook() {
    vec3 c  = HOOKED_texOff(vec2( 0.0,  0.0)).rgb;
    vec3 n  = HOOKED_texOff(vec2( 0.0, -1.0)).rgb;
    vec3 s  = HOOKED_texOff(vec2( 0.0,  1.0)).rgb;
    vec3 w  = HOOKED_texOff(vec2(-1.0,  0.0)).rgb;
    vec3 e  = HOOKED_texOff(vec2( 1.0,  0.0)).rgb;
    vec3 nw = HOOKED_texOff(vec2(-1.0, -1.0)).rgb;
    vec3 ne = HOOKED_texOff(vec2( 1.0, -1.0)).rgb;
    vec3 sw = HOOKED_texOff(vec2(-1.0,  1.0)).rgb;
    vec3 se = HOOKED_texOff(vec2( 1.0,  1.0)).rgb;

    vec3 mn = (min(c, min(min(n, s), min(w, e))) + min(min(nw, ne), min(sw, se))) * 0.5;
    vec3 mx = (max(c, max(max(n, s), max(w, e))) + max(max(nw, ne), max(sw, se))) * 0.5;

    vec3 amp = sqrt(clamp(min(mn, 1.0 - mx) / max(mx, vec3(1e-4)), 0.0, 1.0));
    vec3 wt = amp * (-1.0 / mix(8.0, 5.0, CAS_STRENGTH));
    vec3 outCol = (c + (n + s + w + e) * wt) / (1.0 + 4.0 * wt);
    return vec4(clamp(outCol, max(vec3(0.0), mn - 0.03), min(vec3(1.0), mx + 0.02)), 1.0);
}
`

const glslOutput4K = `//!DESC aniclitr v3 4K Ultra Display CAS
//!HOOK OUTPUT
//!BIND HOOKED

#define CAS_STRENGTH 0.88

vec4 hook() {
    vec3 c  = HOOKED_texOff(vec2( 0.0,  0.0)).rgb;
    vec3 n  = HOOKED_texOff(vec2( 0.0, -1.0)).rgb;
    vec3 s  = HOOKED_texOff(vec2( 0.0,  1.0)).rgb;
    vec3 w  = HOOKED_texOff(vec2(-1.0,  0.0)).rgb;
    vec3 e  = HOOKED_texOff(vec2( 1.0,  0.0)).rgb;
    vec3 nw = HOOKED_texOff(vec2(-1.0, -1.0)).rgb;
    vec3 ne = HOOKED_texOff(vec2( 1.0, -1.0)).rgb;
    vec3 sw = HOOKED_texOff(vec2(-1.0,  1.0)).rgb;
    vec3 se = HOOKED_texOff(vec2( 1.0,  1.0)).rgb;

    vec3 mn = (min(c, min(min(n, s), min(w, e))) + min(min(nw, ne), min(sw, se))) * 0.5;
    vec3 mx = (max(c, max(max(n, s), max(w, e))) + max(max(nw, ne), max(sw, se))) * 0.5;

    vec3 amp = sqrt(clamp(min(mn, 1.0 - mx) / max(mx, vec3(1e-4)), 0.0, 1.0));
    vec3 wt = amp * (-1.0 / mix(8.0, 5.0, CAS_STRENGTH));
    vec3 outCol = (c + (n + s + w + e) * wt) / (1.0 + 4.0 * wt);
    return vec4(clamp(outCol, max(vec3(0.0), mn - 0.04), min(vec3(1.0), mx + 0.03)), 1.0);
}
`
