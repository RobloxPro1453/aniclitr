package subs

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAniSubSearchAndZipEpisodeExtract(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	zipData := makeTestZip(t, map[string]string{
		"Frieren - 01 [1080p].ass": "[Script Info]\nTitle: Ep1\n",
		"Frieren - 05 [1080p].ass": "[Script Info]\nTitle: Ep5\n",
		"Frieren - 12 [1080p].ass": "[Script Info]\nTitle: Ep12\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"anime":[{"name":"Sousou no Frieren","url":"sousou-no-frieren","details":"5 altyazı"}]}`))
		case "/anime/sousou-no-frieren":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><script data-page="app" type="application/json">{"props":{"subtitles":[` +
				`{"subtitle_id":209,"subtitle_release_name":"HolySubs","subtitle_language":"Türkçe","subtitle_uploader":"HolySubs","content_type":"Paket","format":"ass","download_count":120,"visibility":"public"},` +
				`{"subtitle_id":210,"subtitle_release_name":"Ep3Only","subtitle_language":"Türkçe","content_type":"Bölüm: 03","episode_number":3,"format":"srt","download_count":50,"visibility":"public"},` +
				`{"subtitle_id":211,"subtitle_release_name":"SingleEp5","subtitle_language":"Türkçe","content_type":"Bölüm: 05","episode_number":5,"format":"ass","download_count":80,"visibility":"public"}` +
				`]}}</script></html>`))
		case "/download/209":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prov := &AniSub{Base: srv.URL}
	rs, err := prov.Search(context.Background(), Query{Title: "Sousou no Frieren", Episode: 5, Langs: []string{"tr", "en"}})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(rs) != 2 {
		t.Fatalf("expected 2 results (pack + ep5, excluding ep3), got %d: %+v", len(rs), rs)
	}
	if rs[0].Provider != "AniSub" || rs[0].Lang != "tr" || rs[0].ID != "209" {
		t.Fatalf("unexpected top result: %+v", rs[0])
	}

	path, err := prov.Download(context.Background(), rs[0])
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Title: Ep5") {
		t.Fatalf("expected episode 5 extracted from ZIP pack, got %q (file %s)", string(got), filepath.Base(path))
	}
}

func TestMultiCombinesProviders(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			_, _ = w.Write([]byte(`{"anime":[{"name":"Another","url":"another"}]}`))
		case "/anime/another":
			_, _ = w.Write([]byte(`<script data-page="app" type="application/json">{"props":{"subtitles":[{"subtitle_id":99,"subtitle_release_name":"FansubTR","subtitle_language":"Türkçe","content_type":"Bölüm: 01","episode_number":1,"format":"srt","download_count":10,"visibility":"public"}]}}</script>`))
		case "/download/99":
			_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:03,000\nMerhaba\n"))
		}
	}))
	defer srv.Close()

	m := &Multi{Providers: []Provider{&AniSub{Base: srv.URL}}}
	rs, err := m.Search(context.Background(), Query{Title: "Another", Episode: 1, Langs: []string{"tr"}})
	if err != nil || len(rs) != 1 {
		t.Fatalf("Multi.Search: err=%v rs=%+v", err, rs)
	}
	p, err := m.Download(context.Background(), rs[0])
	if err != nil {
		t.Fatalf("Multi.Download: %v", err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "Merhaba") || !strings.HasSuffix(p, ".srt") {
		t.Fatalf("downloaded plain srt mismatch: path=%s content=%q", p, string(b))
	}
}
