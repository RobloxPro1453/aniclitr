package animim

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// serve replays animim.app answers recorded on 2026-11-07 (Frieren,
// trimmed, plus a made-up 2.5 recap). The video proxy redirects to a CDN
// file, like the real one.
func serve(t *testing.T) {
	t.Helper()
	t.Setenv("ANICLITR_HOME", t.TempDir())
	var srv *httptest.Server
	file := func(n string) []byte {
		b, err := os.ReadFile("testdata/" + n)
		if err != nil {
			t.Fatal(err)
		}
		return []byte(strings.ReplaceAll(strings.ReplaceAll(string(b), "https://animim.app", srv.URL), "https://cdn.animim.app", srv.URL+"/cdn"))
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/api/search":
			_, _ = w.Write(file("search.json"))
		case p == "/api/animes/6813f503c8d1ba08dccd7657":
			_, _ = w.Write(file("anime.json"))
		case p == "/api/episodes" && r.URL.Query().Get("animeId") == "6813f503c8d1ba08dccd7657":
			_, _ = w.Write(file("episodes.json"))
		case p == "/api/episodes/6814bf4a3631a5a5281a23a7":
			_, _ = w.Write(file("episode.json"))
		case strings.HasPrefix(p, "/api/video-proxy/"):
			http.Redirect(w, r, srv.URL+"/cdn/file.mp4", http.StatusFound)
		case p == "/cdn/file.mp4":
			w.Header().Set("Content-Range", "bytes 0-1023/800000000")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(make([]byte, 1024))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := base
	base = srv.URL
	t.Cleanup(func() { base = old })
}

func TestRecorded(t *testing.T) {
	serve(t)
	ctx := context.Background()
	p := Provider{}
	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) != 1 || res[0].Title != "Sousou no Frieren" {
		t.Fatalf("search: %v %+v", err, res)
	}
	d, err := p.Details(ctx, res[0])
	if err != nil || d.MalID != 52991 || len(d.Seasons) != 1 || d.AltTitle == "" {
		t.Fatalf("details: %v %+v", err, d)
	}
	eps, err := p.Episodes(ctx, d, 1)
	if err != nil || len(eps) != 4 {
		t.Fatalf("episodes: %v %+v", err, eps)
	}
	if eps[2].Num() != "2,5" || !eps[2].Extra() || eps[3].Number != 3 {
		t.Fatalf("2.5 should sit between 2 and 3: %+v", eps)
	}
	vs, err := p.Variants(ctx, d, eps[0])
	if err != nil || len(vs) != 1 || vs[0].Fansub != "HolySubs" || vs[0].Kind != "mp4" {
		t.Fatalf("variants: %v %+v", err, vs)
	}
	st, err := p.Resolve(ctx, vs[0])
	if err != nil {
		t.Fatal(err)
	}
	if st.Qualities[0].Height != 1080 || st.Qualities[0].Broken {
		t.Fatalf("quality behind the redirect: %+v", st.Qualities)
	}
	if len(st.Subtitles) != 1 || st.Subtitles[0].Label != "Türkçe" || !strings.Contains(st.Subtitles[0].URL, ".ass") {
		t.Fatalf("Turkish .ass subtitle: %+v", st.Subtitles)
	}
}
