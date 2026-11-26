// Package media provides v2.0 playback studio features: one-key MP4/GIF clip
// export, optional non-forcing Smart Cinema Profile advice, keyless AnimeThemes
// OST lookup, thumbnail scrubber frame extraction, and user-optional visual
// scene search (zero embedded developer API keys).
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"aniclitr/internal/netx"
	"aniclitr/internal/tools"
)

// ClipRequest describes a short MP4 or GIF clip cut from an episode.
type ClipRequest struct {
	InputURL   string
	Referer    string
	UserAgent  string
	EndSec     float64 // current mpv playback position
	Duration   float64 // clip length in seconds (default 10s)
	AsGIF      bool
	OutputDir  string
	SeriesName string
	Episode    int
}

// ExportClip cuts the last Duration seconds ending at EndSec into an MP4 or
// palette-optimized GIF using ffmpeg.
func ExportClip(ctx context.Context, req ClipRequest) (string, error) {
	ffmpeg, err := tools.LookPath("ffmpeg")
	if err != nil {
		return "", errors.New("klip oluşturmak için ffmpeg gerekli (kurulum: aniclitr kur)")
	}
	if req.InputURL == "" {
		return "", errors.New("akış adresi boş")
	}
	dur := req.Duration
	if dur <= 0 {
		dur = 10
	}
	start := req.EndSec - dur
	if start < 0 {
		start = 0
		if req.EndSec > 1 {
			dur = req.EndSec
		}
	}
	if req.OutputDir == "" {
		req.OutputDir = os.TempDir()
	}
	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return "", err
	}
	ext := ".mp4"
	if req.AsGIF {
		ext = ".gif"
	}
	safe := sanitizeFilename(req.SeriesName)
	if safe == "" {
		safe = "klip"
	}
	fname := fmt.Sprintf("%s_B%02d_%ds_%s%s", safe, req.Episode, int(start), time.Now().Format("150405"), ext)
	outPath := filepath.Join(req.OutputDir, fname)

	args := []string{"-y", "-v", "error"}
	if strings.HasPrefix(req.InputURL, "http://") || strings.HasPrefix(req.InputURL, "https://") {
		var hdrs strings.Builder
		if req.Referer != "" {
			hdrs.WriteString("Referer: " + req.Referer + "\r\n")
		}
		ua := req.UserAgent
		if ua == "" {
			ua = netx.UserAgent
		}
		hdrs.WriteString("User-Agent: " + ua + "\r\n")
		args = append(args, "-headers", hdrs.String())
	}
	args = append(args,
		"-ss", fmt.Sprintf("%.2f", start),
		"-i", req.InputURL,
		"-t", fmt.Sprintf("%.2f", dur),
	)
	if req.AsGIF {
		args = append(args,
			"-vf", "fps=15,scale=640:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=128[p];[s1][p]paletteuse=dither=bayer",
			outPath,
		)
	} else {
		args = append(args,
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
			"-c:a", "aac", "-b:a", "128k",
			"-movflags", "+faststart",
			outPath,
		)
	}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg klip hatası: %s (%w)", strings.TrimSpace(stderr.String()), err)
	}
	return outPath, nil
}

// SceneHit is one match returned by a user-configured visual scene search endpoint.
type SceneHit struct {
	Title      string
	MalID      int
	AnilistID  int
	Episode    int
	FromSec    float64
	Similarity float64
}

// SearchScene queries a user-supplied visual scene search endpoint (such as
// trace.moe) with an image frame. Per product policy, no developer API key is
// ever embedded; endpoint must be explicitly configured by the user.
func SearchScene(ctx context.Context, endpoint, apiKey, imagePath string) ([]SceneHit, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("görsel sahne arama ucu ayarlı değil (Ayarlar → scene_search_url)")
	}
	imgBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(imgBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("User-Agent", netx.UserAgent)
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("x-trace-key", strings.TrimSpace(apiKey))
	}
	resp, err := netx.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sahne arama HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Result []struct {
			Anilist    any     `json:"anilist"`
			Filename   string  `json:"filename"`
			Episode    any     `json:"episode"`
			From       float64 `json:"from"`
			Similarity float64 `json:"similarity"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	var hits []SceneHit
	for _, r := range payload.Result {
		h := SceneHit{
			Title:      r.Filename,
			FromSec:    r.From,
			Similarity: r.Similarity,
		}
		if f, ok := r.Episode.(float64); ok {
			h.Episode = int(f)
		}
		switch v := r.Anilist.(type) {
		case float64:
			h.AnilistID = int(v)
		case map[string]any:
			if id, ok := v["id"].(float64); ok {
				h.AnilistID = int(id)
			}
			if mid, ok := v["idMal"].(float64); ok {
				h.MalID = int(mid)
			}
			if tm, ok := v["title"].(map[string]any); ok {
				if rom, ok := tm["romaji"].(string); ok && rom != "" {
					h.Title = rom
				}
			}
		}
		hits = append(hits, h)
	}
	return hits, nil
}

func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 36 {
		out = out[:36]
	}
	return out
}
