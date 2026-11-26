// Package tools locates and automatically installs optional/required helper
// binaries (mpv, yt-dlp, aria2c, ffmpeg) into a portable user directory
// (%LOCALAPPDATA%\Programs\aniclitr\tools on Windows) with zero admin rights.
package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"aniclitr/internal/netx"
	"aniclitr/internal/pathx"
)

// Spec describes one external helper tool.
type Spec struct {
	Name     string
	Required bool
	Purpose  string
	WingetID string
	BrewPkg  string
}

// All lists every helper tool aniclitr uses.
var All = []Spec{
	{Name: "mpv", Required: true, Purpose: "Video oynatıcı", WingetID: "mpv-player.mpv-CI.MSVC", BrewPkg: "mpv"},
	{Name: "yt-dlp", Required: false, Purpose: "Yedek sunucular, fragmanlar ve HLS indirme", WingetID: "yt-dlp.yt-dlp", BrewPkg: "yt-dlp"},
	{Name: "aria2c", Required: false, Purpose: "Çok bağlantılı hızlı indirme & torrent", WingetID: "aria2.aria2", BrewPkg: "aria2"},
	{Name: "ffmpeg", Required: false, Purpose: "HLS akışlarını MP4'e birleştirme", WingetID: "Gyan.FFmpeg", BrewPkg: "ffmpeg"},
}

// Dir is the managed portable tools directory alongside aniclitr's install dir.
func Dir() string {
	return filepath.Join(pathx.InstallDir(), "tools")
}

// LookPath checks the managed tools directory first, then system PATH.
func LookPath(name string) (string, error) {
	candidates := []string{name}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		candidates = []string{name + ".exe", name}
	}
	dir := Dir()
	for _, c := range candidates {
		p := filepath.Join(dir, c)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return exec.LookPath(name)
}

// Status reports presence of every helper tool.
func Status() map[string]bool {
	out := make(map[string]bool, len(All))
	for _, s := range All {
		_, err := LookPath(s.Name)
		out[s.Name] = err == nil
	}
	return out
}

// Missing returns specs for all tools not currently installed.
func Missing() []Spec {
	st := Status()
	var out []Spec
	for _, s := range All {
		if !st[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// Install installs a single tool using portable direct download (yt-dlp) or
// the native OS package manager (winget on Windows, brew on macOS).
func Install(ctx context.Context, name string) error {
	var spec *Spec
	for i := range All {
		if All[i].Name == name {
			spec = &All[i]
			break
		}
	}
	if spec == nil {
		return fmt.Errorf("bilinmeyen araç: %s", name)
	}
	// yt-dlp ships single-file official binaries on GitHub Releases: download directly (no winget needed).
	if name == "yt-dlp" {
		if err := installYtDlpDirect(ctx); err == nil {
			return nil
		}
	}

	switch runtime.GOOS {
	case "windows":
		wg, err := exec.LookPath("winget")
		if err != nil {
			return fmt.Errorf("%s kurulumu için winget bulunamadı", name)
		}
		cmd := exec.CommandContext(ctx, wg, "install",
			"--id", spec.WingetID,
			"--exact",
			"--silent",
			"--accept-package-agreements",
			"--accept-source-agreements",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if len(msg) > 120 {
				msg = msg[len(msg)-120:]
			}
			return fmt.Errorf("%s kurulamadı: %v (%s)", name, err, msg)
		}
		return nil
	case "darwin":
		brew, err := exec.LookPath("brew")
		if err != nil {
			return fmt.Errorf("%s için Homebrew kurulu değil (brew install %s)", name, spec.BrewPkg)
		}
		cmd := exec.CommandContext(ctx, brew, "install", spec.BrewPkg)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("brew install %s başarısız: %w", spec.BrewPkg, err)
		}
		return nil
	default:
		return fmt.Errorf("Linux'ta paket yöneticinizle kurun (örn: sudo apt install %s)", spec.BrewPkg)
	}
}

func installYtDlpDirect(ctx context.Context) error {
	asset := "yt-dlp"
	if runtime.GOOS == "windows" {
		asset = "yt-dlp.exe"
	} else if runtime.GOOS == "darwin" {
		asset = "yt-dlp_macos"
	}
	u := "https://github.com/yt-dlp/yt-dlp/releases/latest/download/" + asset
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	resp, err := netx.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("yt-dlp HTTP %d", resp.StatusCode)
	}
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dstName := "yt-dlp"
	if runtime.GOOS == "windows" {
		dstName = "yt-dlp.exe"
	}
	dst := filepath.Join(dir, dstName)
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil || closeErr != nil || n < 100<<10 {
		_ = os.Remove(tmp)
		if err == nil {
			err = errors.New("yt-dlp indirmesi eksik")
		}
		return err
	}
	return os.Rename(tmp, dst)
}
