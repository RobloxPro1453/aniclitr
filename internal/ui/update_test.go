package ui

import (
	"crypto/sha256"
	enchex "encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/update"
)

// fakeReleases serves a v9.9.9 release whose binary is bin.
func fakeReleases(t *testing.T, bin []byte) {
	h := sha256.Sum256(bin)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[{"name":%q,"browser_download_url":"%s/bin"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`,
			update.AssetName(), srv.URL, srv.URL)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(bin) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", enchex.EncodeToString(h[:]), update.AssetName())
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oldAPI, oldRepo := update.API, update.Repo
	update.API, update.Repo = srv.URL, "o/r"
	Version = "0.1.0"
	t.Cleanup(func() { update.API, update.Repo, Version = oldAPI, oldRepo, "dev" })
}

// The installed copy fetches the new release and swaps itself; the notice
// says it applies on the next start.
func TestUpdateInstalledCopy(t *testing.T) {
	fakeReleases(t, []byte("aniclitr 9.9.9"))
	exe := filepath.Join(t.TempDir(), "aniclitr.exe")
	os.WriteFile(exe, []byte("aniclitr 0.1.0"), 0o755)
	exePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { exePath = defaultExePath })
	a := testApp(t, Graphics{})
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	a.path.(*fakePath).installed = true
	a.Update(a.checkUpdate(true)())
	if b, _ := os.ReadFile(exe); string(b) != "aniclitr 9.9.9" {
		t.Fatalf("installed copy should be replaced, has %q", b)
	}
	if !strings.Contains(a.toast, "9.9.9") || len(a.notices) == 0 {
		t.Fatalf("user should hear about it: toast=%q notices=%d", a.toast, len(a.notices))
	}
}

// A development build only says a release exists; it never replaces itself.
func TestUpdateDevCopyOnlyTells(t *testing.T) {
	fakeReleases(t, []byte("aniclitr 9.9.9"))
	exe := filepath.Join(t.TempDir(), "aniclitr.exe")
	os.WriteFile(exe, []byte("dev build"), 0o755)
	exePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { exePath = defaultExePath })
	a := testApp(t, Graphics{})
	a.Update(a.checkUpdate(true)())
	if b, _ := os.ReadFile(exe); string(b) != "dev build" {
		t.Fatal("a dev build must not replace itself")
	}
	if !strings.Contains(a.toast, "9.9.9") {
		t.Fatalf("toast: %q", a.toast)
	}
}

// The old PATH entry (the build folder) moves to an installed copy, and
// that copy becomes the published release (a local build may not know where
// updates come from; the release always does).
func TestPathMigratesToInstalledCopy(t *testing.T) {
	fakeReleases(t, []byte("aniclitr release"))
	Version = "9.9.9" // same as the release: still take the official build
	a := testApp(t, Graphics{})
	fp := a.path.(*fakePath)
	fp.legacy = true
	fp.target = filepath.Join(t.TempDir(), "aniclitr.exe")
	os.WriteFile(fp.target, []byte("local build"), 0o755)
	if a.migratePath() == nil {
		t.Fatal("a build folder on PATH should be migrated")
	}
	a.Update(a.movePath()())
	if !fp.on || fp.legacy || fp.adds != 1 {
		t.Fatalf("migration should install and switch PATH: %+v", fp)
	}
	a.Update(a.officialInstall()())
	if b, _ := os.ReadFile(fp.target); string(b) != "aniclitr release" {
		t.Fatalf("the installed copy should be the release, has %q", b)
	}
	if a.migratePath() != nil {
		t.Fatal("nothing to do the second time")
	}
	// a build newer than the release is left alone
	Version = "10.0.0"
	os.WriteFile(fp.target, []byte("newer local build"), 0o755)
	if msg := a.officialInstall()(); msg != nil {
		t.Fatalf("a newer build must not be replaced by an older release: %v", msg)
	}
}
