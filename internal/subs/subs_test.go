package subs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOpenSubtitlesFlow(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Api-Key") != "k" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/subtitles":
			q := r.URL.Query()
			if q.Get("query") != "Frieren" || q.Get("episode_number") != "5" || q.Get("languages") != "tr,en" {
				t.Errorf("bad query %v", q)
			}
			json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"attributes": map[string]any{"language": "en", "release": "[Sub] Frieren 05", "download_count": 900, "files": []any{map[string]any{"file_id": 11, "file_name": "a.srt"}}, "feature_details": map[string]any{"episode_number": 5}}},
				map[string]any{"attributes": map[string]any{"language": "tr", "release": "Frieren 05 TR", "download_count": 40, "files": []any{map[string]any{"file_id": 22, "file_name": "b.srt"}}, "feature_details": map[string]any{"episode_number": 5}}},
				map[string]any{"attributes": map[string]any{"language": "tr", "release": "wrong ep", "download_count": 5000, "files": []any{map[string]any{"file_id": 33}}, "feature_details": map[string]any{"episode_number": 6}}},
			}})
		case "/download":
			fmt.Fprintf(w, `{"link":"%s/file/22","file_name":"b.srt"}`, srv.URL)
		case "/file/22":
			fmt.Fprint(w, "1\n00:00:01,000 --> 00:00:02,000\nMerhaba\n")
		}
	}))
	defer srv.Close()
	o := &OpenSubtitles{Key: "k", Base: srv.URL}
	rs, err := o.Search(context.Background(), Query{Title: "Frieren", Season: 1, Episode: 5, Langs: []string{"tr", "en"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Lang != "tr" || rs[1].Lang != "en" {
		t.Fatalf("ranking/filter wrong: %+v", rs)
	}
	p, err := o.Download(context.Background(), rs[0])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) == "" {
		t.Fatal("empty file")
	}
	if _, err := (&OpenSubtitles{}).Search(context.Background(), Query{Title: "x"}); err != ErrNoKey {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
}
