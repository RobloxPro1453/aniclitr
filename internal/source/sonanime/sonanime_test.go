package sonanime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"aniclitr/internal/extract"
)

// serve replays api.sonanime.com answers recorded on 2026-11-07 (Frieren,
// trimmed). Video links point back at this server: the second season-1
// episode's files are gone (404), the others answer like the real CDN.
func serve(t *testing.T) {
	t.Helper()
	t.Setenv("ANICLITR_HOME", t.TempDir())
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/anime/search":
			b, _ := os.ReadFile("testdata/search.json")
			_, _ = w.Write(b)
		case r.URL.Path == "/api/anime/link/sousou-no-frieren":
			b, _ := os.ReadFile("testdata/link.json")
			_, _ = w.Write([]byte(strings.ReplaceAll(string(b), "https://video.konununuzmani.com/video/", srv.URL+"/video/")))
		case strings.HasPrefix(r.URL.Path, "/video/") && strings.Contains(r.URL.Path, "MeJL22SFmzQfQiUN"), // S1E2 720p
			strings.HasPrefix(r.URL.Path, "/video/") && strings.Contains(r.URL.Path, "RQ9YzVCFbjBbCCWl"): // S1E2 480p
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/video/"):
			w.Header().Set("Content-Range", "bytes 0-1023/300000000")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(make([]byte, 1024))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL + "/api"
	t.Cleanup(func() { apiBase = old })
	cacheMu.Lock()
	cache = map[string]cached{}
	cacheMu.Unlock()
}

func TestRecorded(t *testing.T) {
	serve(t)
	ctx := context.Background()
	p := Provider{}
	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) != 2 || res[0].ID != "sousou-no-frieren" || res[0].Year != 2023 {
		t.Fatalf("search: %v %+v", err, res)
	}
	d, err := p.Details(ctx, res[0])
	if err != nil || d.MalID != 52991 || len(d.Seasons) != 2 || d.Seasons[0].EpisodeCount != 3 || d.Seasons[1].EpisodeCount != 2 {
		t.Fatalf("details: %v mal %d seasons %+v", err, d.MalID, d.Seasons)
	}
	s2, err := p.Episodes(ctx, d, 2)
	if err != nil || len(s2) != 2 || s2[0].Season != 2 {
		t.Fatalf("season 2: %v %+v", err, s2)
	}
	eps, _ := p.Episodes(ctx, d, 1)
	vs, _ := p.Variants(ctx, d, eps[0])
	st, err := p.Resolve(ctx, vs[0])
	if err != nil || len(st.Qualities) != 3 || st.Qualities[0].Height != 1080 || st.MalID != 52991 || st.Referer == "" {
		t.Fatalf("resolve E1: %v %+v", err, st)
	}
	// E2: no 1080p, and its 720p/480p files are gone: a dead, permanent failure
	vs, _ = p.Variants(ctx, d, eps[1])
	if _, err := p.Resolve(ctx, vs[0]); err == nil || !extract.KindOf(err).Permanent() {
		t.Fatalf("E2 should fail for good, got %v", err)
	}
}
