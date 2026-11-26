// Package subsync implements a multi-parameter decision-matrix automatic
// subtitle alignment engine (embedded reference timeline, speech VAD energy
// envelope, video scene-cut correlation, FPS drift grid, and piecewise
// commercial-break/eyecatch segmentation with cross-validation & confidence
// scoring).
package subsync

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Format identifies a subtitle file container format.
type Format string

const (
	FormatSRT Format = "srt"
	FormatASS Format = "ass"
	FormatVTT Format = "vtt"
)

// Cue represents one timed subtitle event.
type Cue struct {
	Index  int
	Start  float64 // seconds
	End    float64 // seconds
	Text   string  // stripped plain text
	IsSign bool    // true for positioned typeset signs/karaoke (excluded from VAD correlation)
	LineNo int     // source line index (for lossless ASS rewrite)
}

// Document holds parsed subtitle cues plus raw content for lossless rewriting.
type Document struct {
	Format Format
	Raw    string
	Cues   []Cue
}

var (
	reSRTTime = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})\s*-->\s*(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})`)
	reVTTTime = regexp.MustCompile(`(?:(\d{1,2}):)?(\d{2}):(\d{2})\.(\d{1,3})\s*-->\s*(?:(\d{1,2}):)?(\d{2}):(\d{2})\.(\d{1,3})`)
	reASSTags = regexp.MustCompile(`\{[^}]*\}`)
	reHTMLTag = regexp.MustCompile(`<[^>]+>`)
)

// Parse decodes SRT, ASS/SSA, or WebVTT subtitles into a Document.
func Parse(raw string) Document {
	raw = strings.TrimPrefix(raw, "\xef\xbb\xbf") // UTF-8 BOM
	norm := strings.ReplaceAll(raw, "\r\n", "\n")
	low := strings.ToLower(norm)
	switch {
	case strings.Contains(low, "[events]") && strings.Contains(low, "dialogue:"):
		return parseASS(norm)
	case strings.HasPrefix(strings.TrimSpace(low), "webvtt"):
		return parseVTT(norm)
	default:
		return parseSRT(norm)
	}
}

func parseSRT(norm string) Document {
	doc := Document{Format: FormatSRT, Raw: norm}
	blocks := strings.Split(norm, "\n\n")
	idx := 0
	for _, blk := range blocks {
		lines := strings.Split(strings.TrimSpace(blk), "\n")
		if len(lines) < 2 {
			continue
		}
		timeLine := -1
		var start, end float64
		for i, l := range lines {
			if m := reSRTTime.FindStringSubmatch(l); len(m) == 9 {
				start = hmsToSec(m[1], m[2], m[3], m[4])
				end = hmsToSec(m[5], m[6], m[7], m[8])
				timeLine = i
				break
			}
		}
		if timeLine < 0 || end <= start {
			continue
		}
		txt := strings.TrimSpace(reHTMLTag.ReplaceAllString(strings.Join(lines[timeLine+1:], " "), ""))
		idx++
		doc.Cues = append(doc.Cues, Cue{
			Index:  idx,
			Start:  start,
			End:    end,
			Text:   txt,
			IsSign: isLikelySign(txt, ""),
		})
	}
	return doc
}

func parseVTT(norm string) Document {
	doc := Document{Format: FormatVTT, Raw: norm}
	blocks := strings.Split(norm, "\n\n")
	idx := 0
	for _, blk := range blocks {
		lines := strings.Split(strings.TrimSpace(blk), "\n")
		timeLine := -1
		var start, end float64
		for i, l := range lines {
			if m := reVTTTime.FindStringSubmatch(l); len(m) == 9 {
				start = hmsToSec(m[1], m[2], m[3], m[4])
				end = hmsToSec(m[5], m[6], m[7], m[8])
				timeLine = i
				break
			}
		}
		if timeLine < 0 || end <= start {
			continue
		}
		txt := strings.TrimSpace(reHTMLTag.ReplaceAllString(strings.Join(lines[timeLine+1:], " "), ""))
		idx++
		doc.Cues = append(doc.Cues, Cue{
			Index:  idx,
			Start:  start,
			End:    end,
			Text:   txt,
			IsSign: isLikelySign(txt, ""),
		})
	}
	return doc
}

func parseASS(norm string) Document {
	doc := Document{Format: FormatASS, Raw: norm}
	lines := strings.Split(norm, "\n")
	idx := 0
	for i, l := range lines {
		if !strings.HasPrefix(l, "Dialogue:") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(l, "Dialogue:"))
		parts := strings.SplitN(body, ",", 10)
		if len(parts) < 10 {
			continue
		}
		start, ok1 := parseASSTime(parts[1])
		end, ok2 := parseASSTime(parts[2])
		if !ok1 || !ok2 || end <= start {
			continue
		}
		style := strings.ToLower(strings.TrimSpace(parts[3]))
		rawText := parts[9]
		sign := strings.Contains(rawText, `\pos(`) || strings.Contains(rawText, `\move(`) ||
			strings.Contains(style, "sign") || strings.Contains(style, "op") ||
			strings.Contains(style, "ed") || strings.Contains(style, "kara") ||
			strings.Contains(style, "title")
		plain := strings.TrimSpace(reASSTags.ReplaceAllString(strings.ReplaceAll(rawText, `\N`, " "), ""))
		idx++
		doc.Cues = append(doc.Cues, Cue{
			Index:  idx,
			Start:  start,
			End:    end,
			Text:   plain,
			IsSign: sign || isLikelySign(plain, style),
			LineNo: i,
		})
	}
	return doc
}

func isLikelySign(plain, style string) bool {
	if strings.TrimSpace(plain) == "" {
		return true
	}
	if strings.Contains(style, "sign") || strings.Contains(style, "note") {
		return true
	}
	return false
}

func hmsToSec(hs, ms, ss, frac string) float64 {
	h, _ := strconv.Atoi(hs)
	m, _ := strconv.Atoi(ms)
	s, _ := strconv.Atoi(ss)
	for len(frac) < 3 {
		frac += "0"
	}
	if len(frac) > 3 {
		frac = frac[:3]
	}
	msVal, _ := strconv.Atoi(frac)
	return float64(h*3600+m*60+s) + float64(msVal)/1000.0
}

func parseASSTime(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	secParts := strings.Split(parts[2], ".")
	if err1 != nil || err2 != nil || len(secParts) != 2 {
		return 0, false
	}
	sec, err3 := strconv.Atoi(secParts[0])
	cs, err4 := strconv.Atoi(secParts[1])
	if err3 != nil || err4 != nil {
		return 0, false
	}
	return float64(h*3600+m*60+sec) + float64(cs)/100.0, true
}

func formatSRTTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	msTotal := int(math.Round(sec * 1000))
	h := msTotal / 3600000
	m := (msTotal / 60000) % 60
	s := (msTotal / 1000) % 60
	ms := msTotal % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func formatVTTTime(sec float64) string {
	return strings.ReplaceAll(formatSRTTime(sec), ",", ".")
}

func formatASSTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	csTotal := int(math.Round(sec * 100))
	h := csTotal / 360000
	m := (csTotal / 6000) % 60
	s := (csTotal / 100) % 60
	cs := csTotal % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}

// Rewrite applies TransformTime(t) to every cue in doc losslessly, preserving
// ASS headers/styles/tags and SRT/VTT formatting.
func Rewrite(doc Document, mapTime func(t float64) float64) string {
	switch doc.Format {
	case FormatASS:
		lines := strings.Split(doc.Raw, "\n")
		for i, l := range lines {
			if !strings.HasPrefix(l, "Dialogue:") {
				continue
			}
			body := strings.TrimPrefix(l, "Dialogue:")
			leadSpace := ""
			if strings.HasPrefix(body, " ") {
				leadSpace = " "
				body = body[1:]
			}
			parts := strings.SplitN(body, ",", 10)
			if len(parts) < 10 {
				continue
			}
			st, ok1 := parseASSTime(parts[1])
			en, ok2 := parseASSTime(parts[2])
			if !ok1 || !ok2 {
				continue
			}
			parts[1] = formatASSTime(mapTime(st))
			parts[2] = formatASSTime(mapTime(en))
			lines[i] = "Dialogue:" + leadSpace + strings.Join(parts, ",")
		}
		return strings.Join(lines, "\n")
	case FormatVTT:
		return reVTTTime.ReplaceAllStringFunc(doc.Raw, func(m string) string {
			sub := reVTTTime.FindStringSubmatch(m)
			if len(sub) != 9 {
				return m
			}
			st := mapTime(hmsToSec(sub[1], sub[2], sub[3], sub[4]))
			en := mapTime(hmsToSec(sub[5], sub[6], sub[7], sub[8]))
			return formatVTTTime(st) + " --> " + formatVTTTime(en)
		})
	default:
		return reSRTTime.ReplaceAllStringFunc(doc.Raw, func(m string) string {
			sub := reSRTTime.FindStringSubmatch(m)
			if len(sub) != 9 {
				return m
			}
			st := mapTime(hmsToSec(sub[1], sub[2], sub[3], sub[4]))
			en := mapTime(hmsToSec(sub[5], sub[6], sub[7], sub[8]))
			return formatSRTTime(st) + " --> " + formatSRTTime(en)
		})
	}
}
