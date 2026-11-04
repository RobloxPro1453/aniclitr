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

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

// Resolve picks a host-specific extractor, falling back to yt-dlp.
func Resolve(ctx context.Context, embed string) (*source.Stream, error) {
	embed = NormalizeURL(embed)
	u, err := url.Parse(embed)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "tau-video"):
		return Tau(ctx, u)
	case strings.Contains(host, "drive.google.com"):
		return Drive(ctx, embed)
	case strings.Contains(host, "sibnet"):
		if s, err := Sibnet(ctx, embed); err == nil {
			return s, nil
		}
		return YtDlp(ctx, embed)
	default:
		return YtDlp(ctx, embed)
	}
}

// NormalizeURL repairs URLs like "https://animecix.tv/https://tau-video.xyz/embed/x" or "//host/x".
func NormalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "https://"); i > 0 {
		s = s[i:]
	}
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	return s
}

// Tau resolves tau-video.xyz embeds through its public JSON API.
func Tau(ctx context.Context, u *url.URL) (*source.Stream, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	id := parts[len(parts)-1]
	if id == "" || id == "embed" {
		return nil, errors.New("tau: embed id yok")
	}
	api := "https://tau-video.xyz/api/video/" + id
	if vid := u.Query().Get("vid"); vid != "" {
		api += "?vid=" + url.QueryEscape(vid)
	}
	var res struct {
		URLs []struct {
			Label string `json:"label"`
			URL   string `json:"url"`
			Size  int64  `json:"size"`
		} `json:"urls"`
	}
	if err := netx.GetJSON(ctx, api, map[string]string{"Referer": u.String()}, &res); err != nil {
		return nil, fmt.Errorf("tau: %w", err)
	}
	st := &source.Stream{Referer: "https://tau-video.xyz/"}
	seen := map[string]bool{}
	for _, x := range res.URLs {
		if x.URL == "" || seen[x.Label] {
			continue
		}
		seen[x.Label] = true
		st.Qualities = append(st.Qualities, source.Quality{Label: x.Label, URL: x.URL, Height: Height(x.Label), Size: x.Size})
	}
	if len(st.Qualities) == 0 {
		return nil, errors.New("tau: video yok")
	}
	st.SortQualities()
	return st, nil
}

// Drive checks a Google Drive video with yt-dlp, then hands the page itself
// on: its stream links only work with cookies set while resolving, which a
// plain URL would lose.
func Drive(ctx context.Context, embed string) (*source.Stream, error) {
	st, err := YtDlp(ctx, embed)
	if err != nil {
		return nil, fmt.Errorf("drive: %w", err)
	}
	h := st.Qualities[0].Height
	if u, err := url.Parse(embed); err == nil {
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/preview"), "/view") + "/view"
		embed = u.String()
	}
	return &source.Stream{Qualities: []source.Quality{{Label: labelFor(h), URL: embed, Height: h, Page: true}}}, nil
}

var sibnetSrc = regexp.MustCompile(`player\.src\(\[\{src:\s*"([^"]+)"`)

// Sibnet scrapes the mp4 path from the shell page.
func Sibnet(ctx context.Context, embed string) (*source.Stream, error) {
	body, err := netx.Get(ctx, embed, map[string]string{"Referer": "https://video.sibnet.ru/"})
	if err != nil {
		return nil, fmt.Errorf("sibnet: %w", err)
	}
	m := sibnetSrc.FindSubmatch(body)
	if m == nil {
		return nil, errors.New("sibnet: kaynak bulunamadı")
	}
	src := string(m[1])
	if strings.HasPrefix(src, "/") {
		src = "https://video.sibnet.ru" + src
	}
	return &source.Stream{Referer: embed, Qualities: []source.Quality{{Label: "auto", URL: src}}}, nil
}

// YtDlp asks yt-dlp to resolve any of the ~1800 hosts it supports.
func YtDlp(ctx context.Context, embed string) (*source.Stream, error) {
	bin, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil, errors.New("yt-dlp kurulu değil")
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-J", "--no-warnings", "--no-playlist", "--socket-timeout", "15", embed)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			msg := strings.TrimSpace(string(ee.Stderr))
			if i := strings.LastIndex(msg, "ERROR:"); i >= 0 {
				msg = msg[i+6:]
			}
			if len(msg) > 140 {
				msg = msg[:140]
			}
			return nil, fmt.Errorf("yt-dlp: %s", strings.TrimSpace(msg))
		}
		return nil, fmt.Errorf("yt-dlp: %w", err)
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
		return nil, fmt.Errorf("yt-dlp çıktısı okunamadı: %w", err)
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
		return nil, errors.New("yt-dlp: oynatılabilir format yok")
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
