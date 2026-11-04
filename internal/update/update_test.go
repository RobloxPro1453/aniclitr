package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.11.0", "0.10.1", true}, {"v0.10.2", "0.10.1", true}, {"0.10.1", "0.10.1", false},
		{"0.9.9", "0.10.0", false}, {"1.0", "0.99.9", true}, {"dev", "0.1.0", false},
		{"0.1.0", "dev", true}, {"0.11.0-rc1", "0.10.0", true},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// A fake GitHub: latest release, the binary and its checksum.
func fakeGitHub(t *testing.T, bin []byte, sum string) *httptest.Server {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","body":"notlar","html_url":"x","assets":[
			{"name":%q,"browser_download_url":"%s/bin","size":%d},
			{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`, AssetName(), srv.URL, len(bin), srv.URL)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(bin) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  aniclitr_other\n%s  %s\n", sum, sum, AssetName())
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oldAPI, oldRepo := API, Repo
	API, Repo = srv.URL, "o/r"
	t.Cleanup(func() { API, Repo = oldAPI, oldRepo })
	return srv
}

func TestInstallSwapsVerifiedBinary(t *testing.T) {
	bin := []byte("new aniclitr")
	h := sha256.Sum256(bin)
	fakeGitHub(t, bin, hex.EncodeToString(h[:]))
	rel, err := Latest(context.Background())
	if err != nil || rel.Version != "9.9.9" || !rel.Ready() {
		t.Fatalf("latest: %+v %v", rel, err)
	}
	exe := filepath.Join(t.TempDir(), "aniclitr.exe")
	os.WriteFile(exe, []byte("old aniclitr"), 0o755)
	if err := Install(context.Background(), rel, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new aniclitr" {
		t.Fatalf("binary not swapped: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old aniclitr" {
		t.Fatal("the old binary steps aside as .old")
	}
	Cleanup(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("cleanup removes .old")
	}
}

func TestInstallRefusesBadChecksum(t *testing.T) {
	fakeGitHub(t, []byte("tampered"), "00ff")
	rel, _ := Latest(context.Background())
	exe := filepath.Join(t.TempDir(), "aniclitr.exe")
	os.WriteFile(exe, []byte("old aniclitr"), 0o755)
	if err := Install(context.Background(), rel, exe); err == nil {
		t.Fatal("a checksum mismatch must not install")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old aniclitr" {
		t.Fatal("the working binary must stay")
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".aniclitr-yeni-*"))
	if len(left) != 0 {
		t.Fatalf("temp download left behind: %v", left)
	}
}
