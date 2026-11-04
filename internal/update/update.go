// Package update keeps the installed aniclitr current from GitHub Releases.
//
// A release (made by .github/workflows/release.yml when a vX.Y.Z tag is
// pushed) carries one binary per platform plus checksums.txt (SHA-256). The
// installed copy checks the latest release in the background, downloads the
// binary for its platform, verifies the checksum and swaps itself: a running
// .exe cannot be overwritten on Windows but it can be renamed, so the old one
// steps aside as .old (removed on the next start) and the new one takes its
// name. The change applies the next time aniclitr is opened.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases come from ("owner/name"). Release
// builds stamp it from the repository they are built in (-ldflags "-X
// aniclitr/internal/update.Repo=…"); empty turns update checks off.
var Repo = "RobloxPro1453/aniclitr"

// API is the GitHub API base (tests point it at a fake server).
var API = "https://api.github.com"

var client = &http.Client{Timeout: 5 * time.Minute}

// Release is the newest published version.
type Release struct {
	Version string // "0.11.0"
	Notes   string
	URL     string // release page
	asset   string // binary download URL for this platform
	sums    string // checksums.txt download URL
	size    int64
}

// AssetName is the release file for this platform.
func AssetName() string {
	n := fmt.Sprintf("aniclitr_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

// Latest asks GitHub for the newest release.
func Latest(ctx context.Context) (Release, error) {
	if Repo == "" {
		return Release{}, errors.New("güncelleme deposu ayarlı değil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, API+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aniclitr-updater")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, errors.New("henüz yayınlanmış sürüm yok")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub: HTTP %d", resp.StatusCode)
	}
	var r struct {
		Tag     string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return Release{}, err
	}
	rel := Release{Version: strings.TrimPrefix(r.Tag, "v"), Notes: r.Body, URL: r.HTMLURL}
	for _, a := range r.Assets {
		switch a.Name {
		case AssetName():
			rel.asset, rel.size = a.URL, a.Size
		case "checksums.txt":
			rel.sums = a.URL
		}
	}
	return rel, nil
}

// Newer reports whether version a is newer than b ("0.10.1" style; a
// leading "v" and anything after "-" are ignored; "dev" is never newer).
func Newer(a, b string) bool {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka {
		return false
	}
	if !okb {
		return true
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Ready reports whether the release can be installed on this platform.
func (r Release) Ready() bool { return r.asset != "" && r.sums != "" }

// Install downloads the release binary, checks it against checksums.txt and
// puts it in place of exe. The running program keeps working; the new
// version starts next time.
func Install(ctx context.Context, r Release, exe string) error {
	if !r.Ready() {
		return fmt.Errorf("bu sürümde %s yok", AssetName())
	}
	want, err := checksum(ctx, r.sums, AssetName())
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".aniclitr-yeni-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed into place
	h := sha256.New()
	if err := fetch(ctx, r.asset, io.MultiWriter(tmp, h)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return errors.New("indirilen dosyanın SHA-256 özeti tutmuyor; kurulmadı")
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmp.Name(), 0o755)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("eski sürüm kenara alınamadı: %w", err)
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		_ = os.Rename(old, exe) // put the working one back
		return fmt.Errorf("yeni sürüm yerine konamadı: %w", err)
	}
	return nil
}

// Cleanup removes what a previous update left behind (the .old binary).
func Cleanup(exe string) { _ = os.Remove(exe + ".old") }

func fetch(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "aniclitr-updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("indirme: HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, 200<<20))
	return err
}

// checksum finds name's SHA-256 in a checksums.txt ("<hex>  <name>").
func checksum(ctx context.Context, url, name string) (string, error) {
	var b strings.Builder
	if err := fetch(ctx, url, &b); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(strings.NewReader(b.String()))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt içinde %s yok", name)
}
