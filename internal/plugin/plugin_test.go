package plugin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnpackDeanEdwards(t *testing.T) {
	packed := `eval(function(p,a,c,k,e,d){while(c--)if(k[c])p=p.replace(new RegExp('\\b'+c.toString(a)+'\\b','g'),k[c]);return p}('0.1="2://3.4/5/6.7";',8,8,'var|wurl|https|cdn|example|video|ep01|mp4'.split('|')))`
	got := UnpackAll(packed)
	want := `var.wurl="https://cdn.example/video/ep01.mp4";`
	if got != want {
		t.Fatalf("UnpackAll = %q, want %q", got, want)
	}
}

func TestCryptoHelpers(t *testing.T) {
	msg := []byte("aniclitr-v2-plugin-vm")
	key := []byte("secret-key")
	enc := rc4Crypt(msg, key)
	dec := rc4Crypt(enc, key)
	if !bytes.Equal(dec, msg) {
		t.Fatalf("rc4 roundtrip failed: %q", dec)
	}
	xenc := xorBytes(msg, key)
	xdec := xorBytes(xenc, key)
	if !bytes.Equal(xdec, msg) {
		t.Fatalf("xor roundtrip failed: %q", xdec)
	}
}

func TestLoadEmbeddedValidatesAll17Packs(t *testing.T) {
	packs, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded error: %v", err)
	}
	if len(packs) < 17 {
		t.Fatalf("expected >=17 embedded packs, got %d", len(packs))
	}
	for _, p := range packs {
		if err := p.Manifest.Validate(); err != nil {
			t.Errorf("pack %s failed validation: %v", p.Manifest.ID, err)
		}
	}
}

func TestBuiltinFixturesInIsolation(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	_, errs := LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll errs: %v", errs)
	}

	cases := []struct {
		id      string
		rawURL  string
		fixture string
		wantURL string
	}{
		{
			id:      "streamtape",
			rawURL:  "https://streamtape.com/e/abc123",
			fixture: `<script>document.getElementById('robotlink').innerHTML = '//streamtape.com/get_video?id=abc' + '&token=xyz';</script>`,
			wantURL: "https://streamtape.com/get_video?id=abc&token=xyz&stream=1",
		},
		{
			id:      "vidoza",
			rawURL:  "https://vidoza.net/embed-xyz.html",
			fixture: `<video><source src="https://str1.vidoza.net/nvl/v.mp4" type="video/mp4"></video>`,
			wantURL: "https://str1.vidoza.net/nvl/v.mp4",
		},
		{
			id:      "mp4upload",
			rawURL:  "https://www.mp4upload.com/embed-xyz.html",
			fixture: `player.src({ type: "video/mp4", src: "https://a4.mp4upload.com:183/d/abc/video.mp4" });`,
			wantURL: "https://a4.mp4upload.com:183/d/abc/video.mp4",
		},
		{
			id:      "sendvid",
			rawURL:  "https://sendvid.com/abc123",
			fixture: `<meta property="og:video:height" content="720"><meta property="og:video" content="https://cdn.sendvid.com/abc.mp4">`,
			wantURL: "https://cdn.sendvid.com/abc.mp4",
		},
		{
			id:      "filemoon",
			rawURL:  "https://filemoon.sx/e/abc123",
			fixture: `eval(function(p,a,c,k,e,d){return p}('0:"1://2.3/4.5"',6,6,'file|https|cdn|moon|master|m3u8'.split('|')))`,
			wantURL: "https://cdn.moon/master.m3u8",
		},
		{
			id:      "mixdrop",
			rawURL:  "https://mixdrop.co/f/abc123",
			fixture: `eval(function(p,a,c,k,e,d){return p}('0="1://2.3/4.5"',6,6,'wurl|https|s1|mxcontent|v|mp4'.split('|')))`,
			wantURL: "https://s1.mxcontent/v.mp4",
		},
		{
			id:      "streamwish",
			rawURL:  "https://streamwish.to/e/abcd1234ef",
			fixture: `eval(function(p,a,c,k,e,d){return p}('0:"1://2.3/4/5.6"',7,7,'file|https|cdn|wish|hls|index|m3u8'.split('|')))`,
			wantURL: "https://cdn.wish/hls/index.m3u8",
		},
		{
			id:      "uqload",
			rawURL:  "https://uqload.co/embed-abc123.html",
			fixture: `eval(function(p,a,c,k,e,d){return p}('0:"1://2.3/4.5"',6,6,'file|https|m1|uqload|master|m3u8'.split('|')))`,
			wantURL: "https://m1.uqload/master.m3u8",
		},
		{
			id:      "yandexdisk",
			rawURL:  "https://disk.yandex.ru/i/abc123",
			fixture: `<script id="store-prefetch">{"resources":{"r1":{"videoStreams":{"videos":[{"dimension":"1080p","url":"https://streaming.disk.yandex.net/hls/1080.m3u8"}]}}}}</script>`,
			wantURL: "https://streaming.disk.yandex.net/hls/1080.m3u8",
		},
		{
			id:      "vk",
			rawURL:  "https://vk.com/video_ext.php?oid=1&id=2",
			fixture: `var playerParams = {"files":{"mp4_720":"https:\/\/cdn.vk.com\/720.mp4","mp4_1080":"https:\/\/cdn.vk.com\/1080.mp4"}};`,
			wantURL: "https://cdn.vk.com/1080.mp4",
		},
		{
			id:      "direct",
			rawURL:  "https://cdn.example.com/anime/ep1.m3u8",
			fixture: "",
			wantURL: "https://cdn.example.com/anime/ep1.m3u8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			p := ByID(tc.id)
			if p == nil {
				t.Fatalf("pack %q not loaded", tc.id)
			}
			u, err := url.Parse(tc.rawURL)
			if err != nil {
				t.Fatal(err)
			}
			if !p.CanHandle(u) {
				t.Fatalf("pack %q refused %s", tc.id, tc.rawURL)
			}
			rep := p.ResolveTrace(context.Background(), u, nil, nil, tc.fixture)
			if rep.Stream == nil || len(rep.Stream.Qualities) == 0 {
				t.Fatalf("pack %q failed: diag=%q traces=%+v", tc.id, rep.Diagnosis, rep.Traces)
			}
			if rep.Stream.Qualities[0].URL != tc.wantURL {
				t.Fatalf("pack %q got URL %q, want %q", tc.id, rep.Stream.Qualities[0].URL, tc.wantURL)
			}
		})
	}
}

func TestZeroCompileExternalOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANICLITR_HOME", home)
	pdir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop a brand-new custom resolver JSON with zero Go recompilation.
	custom := `{
  "schema": 1,
  "id": "customhost",
  "name": "CustomHost",
  "version": "2.1.0",
  "domains": ["customhost.example"],
  "steps": [
    {"op": "http_get", "url": "{{url}}"},
    {"op": "emit_regex_all", "from": "body", "pattern": "data-stream=\"(https?://[^\"]+)\"", "url_group": 1}
  ]
}`
	if err := os.WriteFile(filepath.Join(pdir, "customhost.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	packs, errs := LoadAll()
	if len(errs) > 0 {
		t.Fatalf("unexpected load errs: %v", errs)
	}
	p := ByID("customhost")
	if p == nil || !p.Manifest.External {
		t.Fatalf("expected external customhost pack in %d loaded packs", len(packs))
	}
	u, _ := url.Parse("https://customhost.example/e/99")
	rep := p.ResolveTrace(context.Background(), u, nil, nil, `<div data-stream="https://cdn.customhost.example/ep99.mp4"></div>`)
	if rep.Stream == nil || rep.Stream.Qualities[0].URL != "https://cdn.customhost.example/ep99.mp4" {
		t.Fatalf("custom plugin resolve failed: %+v", rep)
	}
}

func TestLabBreakageDiagnoses(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	_, _ = LoadAll()

	// 1. JS packer changed diagnosis
	fm := ByID("filemoon")
	u1, _ := url.Parse("https://filemoon.sx/e/test")
	rep1 := fm.ResolveTrace(context.Background(), u1, nil, nil, `<html><body>no packed script anymore</body></html>`)
	if !strings.Contains(rep1.Diagnosis, "JS packer") {
		t.Fatalf("expected JS packer diagnosis, got %q", rep1.Diagnosis)
	}

	// 2. Referer rejected (HTTP 403) diagnosis
	srv403 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden referer"))
	}))
	defer srv403.Close()
	pack403, err := Compile(Manifest{
		Schema:  1,
		ID:      "t403",
		Name:    "T403",
		Version: "1.0.0",
		Domains: []string{"127.0.0.1"},
		Steps: []Step{
			{Op: "http_get", URL: "{{url}}", Headers: map[string]string{"Referer": "https://wrong.example/"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	u403, _ := url.Parse(srv403.URL + "/e/1")
	rep2 := pack403.ResolveTrace(context.Background(), u403, nil, srv403.Client(), "")
	if !strings.Contains(rep2.Diagnosis, "Referer reddedildi") {
		t.Fatalf("expected Referer 403 diagnosis, got %q", rep2.Diagnosis)
	}

	// 3. LabValidate and LabScaffold smoke test
	var buf bytes.Buffer
	if code := LabValidate(&buf); code != 0 {
		t.Fatalf("LabValidate failed (%d): %s", code, buf.String())
	}
	buf.Reset()
	if code := LabScaffold(&buf, "demo", "demo.example"); code != 0 {
		t.Fatalf("LabScaffold failed (%d): %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(Dir(), "demo.json")); err != nil {
		t.Fatalf("scaffolded file missing: %v", err)
	}
}
