package extract

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

const (
	mp4ProbeBytes         = 128 * 1024 // 128 KB
	maxReasonableDuration = 86400      // 24 hours in seconds
)

// MP4Meta holds metadata parsed from an MP4 container's moov atom.
type MP4Meta struct {
	Height   int
	Duration int
	Bitrate  int64
	Size     int64
	Codec    string
	OK       bool
}

// ParseMP4Atoms scans raw MP4 box bytes (from the start or end of a file)
// for a moov box and extracts duration (mvhd), video height (tkhd), and codec (stsd).
func ParseMP4Atoms(buf []byte, totalSize int64) MP4Meta {
	var m MP4Meta
	m.Size = totalSize
	scanBoxes(buf, &m)
	if m.Duration > maxReasonableDuration || m.Duration < 0 {
		m.Duration = 0
	}
	if m.Size > 0 && m.Duration > 0 {
		bw := (m.Size * 8) / int64(m.Duration)
		if bw > 0 && bw <= maxReasonableBitrate {
			m.Bitrate = bw
		}
	}
	if m.Height > 0 || m.Duration > 0 || m.Codec != "" {
		m.OK = true
	}
	return m
}

var containerBoxes = map[string]bool{
	"moov": true,
	"trak": true,
	"mdia": true,
	"minf": true,
	"stbl": true,
	"edts": true,
	"udta": true,
}

func scanBoxes(b []byte, m *MP4Meta) {
	for len(b) >= 8 {
		size := int64(binary.BigEndian.Uint32(b[0:4]))
		typ := string(b[4:8])
		hdrLen := int64(8)
		if size == 1 {
			if len(b) < 16 {
				return
			}
			size = int64(binary.BigEndian.Uint64(b[8:16]))
			hdrLen = 16
		} else if size == 0 {
			size = int64(len(b))
		}
		if size < hdrLen || !isPrintableFourCC(typ) {
			// If we started mid-file (e.g. tail range read), scan forward for "moov".
			idx := strings.Index(string(b[1:]), "moov")
			if idx >= 3 {
				b = b[idx-3:]
				continue
			}
			return
		}
		end := size
		if end > int64(len(b)) {
			end = int64(len(b))
		}
		payload := b[hdrLen:end]

		switch typ {
		case "mvhd":
			parseMvhd(payload, m)
		case "tkhd":
			parseTkhd(payload, m)
		case "stsd":
			parseStsd(payload, m)
		default:
			if containerBoxes[typ] {
				scanBoxes(payload, m)
			}
		}

		if size > int64(len(b)) {
			return
		}
		b = b[size:]
	}
}

func isPrintableFourCC(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func parseMvhd(p []byte, m *MP4Meta) {
	if len(p) < 20 {
		return
	}
	version := p[0]
	var timescale uint32
	var duration uint64
	if version == 1 {
		if len(p) < 32 {
			return
		}
		timescale = binary.BigEndian.Uint32(p[20:24])
		duration = binary.BigEndian.Uint64(p[24:32])
	} else {
		timescale = binary.BigEndian.Uint32(p[12:16])
		duration = uint64(binary.BigEndian.Uint32(p[16:20]))
	}
	if timescale > 0 && duration > 0 && duration != 0xFFFFFFFF && duration != 0xFFFFFFFFFFFFFFFF {
		sec := int(duration / uint64(timescale))
		if sec > 0 && sec <= maxReasonableDuration {
			m.Duration = sec
		}
	}
}

func parseTkhd(p []byte, m *MP4Meta) {
	if len(p) < 84 {
		return
	}
	version := p[0]
	offset := 76
	if version == 1 {
		if len(p) < 96 {
			return
		}
		offset = 88
	}
	// 16.16 fixed-point width and height at the end of tkhd
	hFixed := binary.BigEndian.Uint32(p[offset+4 : offset+8])
	h := int(hFixed >> 16)
	if h > m.Height && h <= 4320 {
		m.Height = h
	}
}

func parseStsd(p []byte, m *MP4Meta) {
	if len(p) < 16 || m.Codec != "" {
		return
	}
	count := int(binary.BigEndian.Uint32(p[4:8]))
	cur := p[8:]
	for i := 0; i < count && len(cur) >= 8; i++ {
		sz := int(binary.BigEndian.Uint32(cur[0:4]))
		fourCC := strings.ToLower(string(cur[4:8]))
		switch fourCC {
		case "avc1", "avc3":
			m.Codec = "H.264"
			return
		case "hvc1", "hev1":
			m.Codec = "HEVC"
			return
		case "av01":
			m.Codec = "AV1"
			return
		case "vp09", "vp08":
			m.Codec = "VP9"
			return
		}
		if sz <= 0 || sz > len(cur) {
			break
		}
		cur = cur[sz:]
	}
}

// ProbeMP4 performs a bounded HTTP Range request (first 128 KB, and last 128 KB
// if moov is at the tail) to verify reachability and read MP4 container metadata.
func ProbeMP4(ctx context.Context, rawURL, referer string) (MP4Meta, error) {
	pctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	headBuf, totalSize, status, err := fetchRange(pctx, rawURL, referer, 0, mp4ProbeBytes-1)
	if err != nil {
		return MP4Meta{}, err
	}
	if status >= 400 {
		return MP4Meta{}, fmt.Errorf("HTTP %d", status)
	}
	meta := ParseMP4Atoms(headBuf, totalSize)
	if !meta.OK && totalSize > mp4ProbeBytes*2 {
		start := totalSize - mp4ProbeBytes
		if tailBuf, _, tStatus, err := fetchRange(pctx, rawURL, referer, start, totalSize-1); err == nil && tStatus < 400 {
			tailMeta := ParseMP4Atoms(tailBuf, totalSize)
			if tailMeta.OK {
				meta = tailMeta
			}
		}
	}
	return meta, nil
}

func fetchRange(ctx context.Context, rawURL, referer string, start, end int64) (buf []byte, total int64, status int, err error) {
	if err := netx.WaitHost(ctx, rawURL); err != nil {
		return nil, 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := netx.Client.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()

	total = parseTotalSize(resp.Header)
	limit := end - start + 1
	if limit <= 0 || limit > mp4ProbeBytes {
		limit = mp4ProbeBytes
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
	return b, total, resp.StatusCode, nil
}

func parseTotalSize(h http.Header) int64 {
	if cr := h.Get("Content-Range"); cr != "" {
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil && n > 0 {
				return n
			}
		}
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(cl), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// ProbeQuality enriches an MP4 source.Quality in-place via ProbeMP4.
// If the server returns 4xx/5xx or if an unlabelled MP4 has no readable container
// metadata (height, duration, bitrate all unknown), q.Broken is marked true.
func ProbeQuality(ctx context.Context, q *source.Quality, referer string) {
	if q == nil || q.Page || q.Probed || strings.Contains(strings.ToLower(q.URL), ".m3u8") {
		return
	}
	meta, err := ProbeMP4(ctx, q.URL, referer)
	q.Probed = true
	if err != nil {
		q.Broken = true
		return
	}
	if meta.Height > 0 {
		q.Height = meta.Height
		q.Label = fmt.Sprintf("%dp", meta.Height)
	}
	if meta.Size > 0 && q.Size == 0 {
		q.Size = meta.Size
	}
	if meta.Bitrate > 0 {
		q.Bitrate = meta.Bitrate
	}
	if meta.Duration > 0 {
		q.Duration = meta.Duration
	}
	if meta.Codec != "" {
		q.Codec = meta.Codec
	}
	// §10.7 rule: if probed and still neither height, duration, nor bitrate is known, mark broken.
	if q.Height <= 0 && q.Duration <= 0 && q.Bitrate <= 0 {
		q.Broken = true
	}
}
