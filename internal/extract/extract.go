// Package extract turns embed/player URLs into directly playable streams.
package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/diag"
	"aniclitr/internal/logx"
	"aniclitr/internal/source"
)

// Resolve tries every registered extractor that matches embed, falling back
// to yt-dlp as the last resort unless an extractor reports a permanent error.
func Resolve(ctx context.Context, embed string) (*source.Stream, error) {
	st, fails := ResolveDetailed(ctx, embed, nil)
	if st != nil && len(st.Qualities) > 0 {
		return st, nil
	}
	return nil, summarizeFailures(fails)
}

// ResolveDetailed runs all matching extractors in registration order and
// returns either the resolved stream or the deduplicated list of typed failures.
func ResolveDetailed(ctx context.Context, embed string, hdr map[string]string) (*source.Stream, []Failure) {
	embed = NormalizeURL(embed)
	u, err := url.Parse(embed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		if err == nil {
			err = errors.New("geçersiz embed adresi")
		}
		return nil, []Failure{{Extractor: "url", Kind: KindUnknown, Detail: err.Error(), Err: err}}
	}

	var fails []Failure
	usedYtDlp := false
	for _, e := range Match(u) {
		if e.Name() == "Google Drive" {
			usedYtDlp = true
		}
		t0 := time.Now()
		st, err := e.Resolve(ctx, u, hdr)
		elapsed := time.Since(t0)
		if err == nil && st != nil && len(st.Qualities) > 0 {
			ProbeHLS(ctx, st)
			if dead, kind := ProbeMP4s(ctx, st); dead {
				// every direct file was refused or unreadable: nothing here
				// will play, and yt-dlp would only find the same dead files
				diag.Record("extract", e.Name(), elapsed, diag.Error)
				fails = append(fails, Failure{Extractor: e.Name(), Kind: kind, Detail: "hiçbir kalite açılmıyor"})
				return nil, DedupeFailures(fails)
			}
			diag.Record("extract", e.Name(), elapsed, diag.OK)
			return st, DedupeFailures(fails)
		}
		diag.Record("extract", e.Name(), elapsed, diagOutcome(err, st))
		f := failureOf(e.Name(), err)
		if f.Kind == KindChanged {
			logx.Errorf("çözümleyici [%s] site yapısı değişmiş (%s): %s", e.Name(), embed, f.Detail)
		}
		fails = append(fails, f)
		// Permanent upstream failures (deleted, private, copyright, 404, embed blocked)
		// or bot walls will not succeed in yt-dlp either.
		if f.Kind.Permanent() || f.Kind == KindBlocked {
			return nil, DedupeFailures(fails)
		}
	}

	if !usedYtDlp {
		t0 := time.Now()
		st, err := YtDlp(ctx, embed)
		elapsed := time.Since(t0)
		if err == nil && st != nil && len(st.Qualities) > 0 {
			diag.Record("extract", "yt-dlp", elapsed, diag.OK)
			return st, DedupeFailures(fails)
		}
		diag.Record("extract", "yt-dlp", elapsed, diagOutcome(err, st))
		fails = append(fails, failureOf("yt-dlp", err))
	}
	return nil, DedupeFailures(fails)
}

func diagOutcome(err error, st *source.Stream) diag.Outcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return diag.Timeout
	}
	if err == nil && (st == nil || len(st.Qualities) == 0) {
		return diag.Empty
	}
	return diag.Error
}

func failureOf(extractor string, err error) Failure {
	if err == nil {
		return Failure{Extractor: extractor, Kind: KindDeleted, Detail: "oynatılabilir kaynak yok"}
	}
	var ee *Error
	if errors.As(err, &ee) {
		name := extractor
		if ee.Extractor != "" {
			name = ee.Extractor
		}
		return Failure{
			Extractor: name,
			Kind:      ee.Kind,
			Detail:    ee.Detail,
			Err:       ee,
		}
	}
	k := KindOf(err)
	return Failure{
		Extractor: extractor,
		Kind:      k,
		Detail:    Summarize(err.Error(), 200),
		Err:       err,
	}
}

func summarizeFailures(fails []Failure) error {
	if len(fails) == 0 {
		return &Error{Kind: KindUnknown, Detail: "oynatılabilir kaynak yok"}
	}
	// Prefer a permanent or specific failure over a generic yt-dlp fallback error.
	chosen := fails[0]
	for _, f := range fails {
		if f.Kind.Permanent() {
			chosen = f
			break
		}
		if chosen.Kind == KindUnknown && f.Kind != KindUnknown {
			chosen = f
		}
	}
	return &Error{
		Extractor: chosen.Extractor,
		Kind:      chosen.Kind,
		Detail:    chosen.Detail,
		Err:       chosen.Err,
		Failures:  fails,
	}
}

// NormalizeURL repairs URLs like "https://animecix.tv/https://tau-video.xyz/embed/x" or "//host/x".
func NormalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "https://"); i > 0 {
		s = s[i:]
	} else if i := strings.LastIndex(s, "http://"); i > 0 {
		s = s[i:]
	}
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	return s
}

// YtDlp asks yt-dlp to resolve any of the ~1800 hosts it supports.
func YtDlp(ctx context.Context, embed string) (*source.Stream, error) {
	bin, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil, &Error{Extractor: "yt-dlp", Kind: KindUnknown, Detail: "yt-dlp kurulu değil", Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-J", "--no-warnings", "--no-playlist", "--socket-timeout", "15", embed)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			rawMsg := strings.TrimSpace(string(ee.Stderr))
			kind := Classify(0, ee.Stderr)
			msg := rawMsg
			if i := strings.LastIndex(msg, "ERROR:"); i >= 0 {
				msg = msg[i+6:]
			}
			msg = Summarize(msg, 140)
			return nil, &Error{
				Extractor: "yt-dlp",
				Kind:      kind,
				Detail:    "yt-dlp: " + msg,
				Err:       err,
			}
		}
		return nil, &Error{
			Extractor: "yt-dlp",
			Kind:      KindOf(err),
			Detail:    fmt.Sprintf("yt-dlp: %v", err),
			Err:       err,
		}
	}
	var info struct {
		URL         string            `json:"url"`
		HTTPHeaders map[string]string `json:"http_headers"`
		Height      int               `json:"height"`
		Formats     []struct {
			URL         string            `json:"url"`
			Height      int               `json:"height"`
			VCodec      string            `json:"vcodec"`
			ACodec      string            `json:"acodec"`
			Protocol    string            `json:"protocol"`
			FormatID    string            `json:"format_id"`
			HTTPHeaders map[string]string `json:"http_headers"`
		} `json:"formats"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, &Error{
			Extractor: "yt-dlp",
			Kind:      KindChanged,
			Detail:    fmt.Sprintf("yt-dlp çıktısı okunamadı: %v", err),
			Err:       err,
		}
	}
	st := &source.Stream{}
	seen := map[int]bool{}
	for i := len(info.Formats) - 1; i >= 0; i-- { // yt-dlp lists worst→best
		f := info.Formats[i]
		if f.URL == "" || f.VCodec == "none" || (f.ACodec == "none" && f.VCodec != "") || strings.Contains(f.Protocol, "dash") {
			continue
		}
		if seen[f.Height] {
			continue
		}
		seen[f.Height] = true
		label := "auto"
		if f.Height > 0 {
			label = strconv.Itoa(f.Height) + "p"
		}
		st.Qualities = append(st.Qualities, source.Quality{Label: label, URL: f.URL, Height: f.Height})
		if st.Referer == "" {
			st.Referer = f.HTTPHeaders["Referer"]
			st.UserAgent = f.HTTPHeaders["User-Agent"]
		}
	}
	if len(st.Qualities) == 0 && info.URL != "" {
		st.Qualities = append(st.Qualities, source.Quality{Label: labelFor(info.Height), URL: info.URL, Height: info.Height})
		st.Referer = info.HTTPHeaders["Referer"]
		st.UserAgent = info.HTTPHeaders["User-Agent"]
	}
	if len(st.Qualities) == 0 {
		return nil, &Error{Extractor: "yt-dlp", Kind: KindDeleted, Detail: "yt-dlp: oynatılabilir format yok"}
	}
	st.SortQualities()
	return st, nil
}

func labelFor(h int) string {
	if h <= 0 {
		return "auto"
	}
	return strconv.Itoa(h) + "p"
}

var digits = regexp.MustCompile(`\d{3,4}`)

// Height parses "1080p", "720", "FHD" style labels into pixel heights.
func Height(label string) int {
	if m := digits.FindString(label); m != "" {
		n, _ := strconv.Atoi(m)
		return n
	}
	switch strings.ToLower(label) {
	case "4k", "uhd":
		return 2160
	case "fhd", "full":
		return 1080
	case "hd":
		return 720
	case "sd":
		return 480
	}
	return 0
}
