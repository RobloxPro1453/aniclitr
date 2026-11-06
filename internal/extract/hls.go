package extract

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

const maxReasonableBitrate = 1_000_000_000 // 1 Gbps

var (
	reHLSResolution = regexp.MustCompile(`RESOLUTION=(\d+)x(\d+)`)
	reHLSAvgBW      = regexp.MustCompile(`AVERAGE-BANDWIDTH=(\d+)`)
	reHLSBW         = regexp.MustCompile(`(?:^|,)BANDWIDTH=(\d+)`)
	reHLSCodecs     = regexp.MustCompile(`CODECS="([^"]+)"`)
)

// Variants parses an HLS master playlist (#EXT-X-STREAM-INF) into sorted
// source.Quality entries with real resolution, bitrate, and video codec.
// Adapted from Migurdex's HlsMasterParser (GPL-3.0).
func Variants(master []byte, base *url.URL) []source.Quality {
	lines := strings.Split(strings.ReplaceAll(string(master), "\r\n", "\n"), "\n")
	var out []source.Quality
	seen := map[string]bool{}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:") {
			continue
		}
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		attrs := line[len("#EXT-X-STREAM-INF:"):]

		// Find the URI line immediately following #EXT-X-STREAM-INF (skipping comments/blanks).
		uri := ""
		for j := i + 1; j < len(lines); j++ {
			cand := strings.TrimSpace(lines[j])
			if cand == "" {
				continue
			}
			if strings.HasPrefix(cand, "#") {
				if strings.HasPrefix(cand, "#EXT-X-STREAM-INF:") || strings.HasPrefix(cand, "#EXT-X-I-FRAME-STREAM-INF:") {
					break
				}
				continue
			}
			uri = cand
			i = j
			break
		}
		if uri == "" {
			continue
		}

		resolvedURL := uri
		if base != nil {
			if ref, err := url.Parse(uri); err == nil {
				resolvedURL = base.ResolveReference(ref).String()
			}
		}
		if seen[resolvedURL] {
			continue
		}

		var height int
		if m := reHLSResolution.FindStringSubmatch(attrs); m != nil {
			height, _ = strconv.Atoi(m[2])
		}

		var bw int64
		if m := reHLSAvgBW.FindStringSubmatch(attrs); m != nil {
			bw, _ = strconv.ParseInt(m[1], 10, 64)
		} else if m := reHLSBW.FindStringSubmatch(attrs); m != nil {
			bw, _ = strconv.ParseInt(m[1], 10, 64)
		}
		if bw > maxReasonableBitrate {
			continue // drop absurd bandwidths (> 1 Gbps)
		}

		var codec string
		if m := reHLSCodecs.FindStringSubmatch(attrs); m != nil {
			codec = parseVideoCodec(m[1])
		}

		lbl := "auto"
		if height > 0 {
			lbl = fmt.Sprintf("%dp", height)
		}
		seen[resolvedURL] = true
		out = append(out, source.Quality{
			Label:   lbl,
			URL:     resolvedURL,
			Height:  height,
			Bitrate: bw,
			Codec:   codec,
			Probed:  true,
		})
	}

	st := source.Stream{Qualities: out}
	st.SortQualities()
	return st.Qualities
}

func parseVideoCodec(codecs string) string {
	for _, part := range strings.Split(codecs, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		switch {
		case strings.HasPrefix(p, "avc1"), strings.HasPrefix(p, "avc3"):
			return "H.264"
		case strings.HasPrefix(p, "hvc1"), strings.HasPrefix(p, "hev1"):
			return "HEVC"
		case strings.HasPrefix(p, "av01"):
			return "AV1"
		case strings.HasPrefix(p, "vp09"), strings.HasPrefix(p, "vp9"):
			return "VP9"
		}
	}
	return ""
}

// ProbeHLS fetches and expands an HLS master playlist if st has a single unprobed .m3u8 URL.
func ProbeHLS(ctx context.Context, st *source.Stream) {
	if st == nil || len(st.Qualities) != 1 {
		return
	}
	q := st.Qualities[0]
	if q.Page || q.Probed || !strings.Contains(strings.ToLower(q.URL), ".m3u8") {
		return
	}
	base, err := url.Parse(q.URL)
	if err != nil {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hdrs := map[string]string{}
	if st.Referer != "" {
		hdrs["Referer"] = st.Referer
	}
	body, err := netx.Get(pctx, q.URL, hdrs)
	if err != nil {
		return
	}
	if qs := Variants(body, base); len(qs) > 0 {
		// Keep the original master playlist at the end as "auto" so mpv can still
		// attach separate #EXT-X-MEDIA audio tracks if needed.
		orig := q
		orig.Label = "auto"
		orig.Probed = true
		st.Qualities = append(qs, orig)
	}
}
