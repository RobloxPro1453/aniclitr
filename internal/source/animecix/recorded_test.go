package animecix

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/source"
)

// serveRecorded replays animecix.tv answers recorded on 2026-11-07
// (testdata/, Sousou no Frieren, title 11130). Like the real site, it hands
// out a different title when the x-e-h header is missing.
func serveRecorded(t *testing.T) {
	t.Helper()
	t.Setenv("ANICLITR_HOME", t.TempDir())
	file := func(name string) []byte {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/secure/search/"):
			_, _ = w.Write(file("search.json"))
		case p == "/secure/titles/11130":
			if r.Header.Get("x-e-h") == "" {
				_, _ = w.Write([]byte(`{"title":{"id":1,"name":"Başka Bir Anime"}}`))
				return
			}
			_, _ = w.Write(file("title.json"))
		case p == "/secure/related-videos":
			_, _ = w.Write(file("related.json"))
		case p == "/secure/episode-videos-points":
			_, _ = w.Write(file("points.json"))
		case p == "/secure/translators":
			_, _ = w.Write(file("translators.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := bases
	bases = []string{srv.URL}
	trMu.Lock()
	trEntries, trAt, trTried = nil, time.Time{}, time.Time{}
	trMu.Unlock()
	t.Cleanup(func() {
		bases = old
		trMu.Lock()
		trEntries, trAt, trTried = nil, time.Time{}, time.Time{}
		trMu.Unlock()
	})
}

func TestRecordedFrieren(t *testing.T) {
	serveRecorded(t)
	ctx := context.Background()
	p := Provider{}

	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) == 0 {
		t.Fatalf("search: %v", err)
	}
	var an = res[0]
	for _, r := range res {
		if r.ID == "11130" {
			an = r
		}
	}
	if an.ID != "11130" {
		t.Fatalf("Frieren not in results: %+v", res)
	}

	d, err := p.Details(ctx, an)
	if err != nil {
		t.Fatal(err)
	}
	if d.MalID != 52991 || len(d.Seasons) != 2 || d.Seasons[0].EpisodeCount != 28 || d.Kind != "TV" {
		t.Fatalf("details: mal %d kind %s seasons %+v", d.MalID, d.Kind, d.Seasons)
	}

	eps, err := p.Episodes(ctx, d, 1)
	if err != nil || len(eps) == 0 {
		t.Fatalf("episodes: %v", err)
	}
	for i, e := range eps {
		if e.Season != 1 || e.Number != i+1 {
			t.Fatalf("season 1 must be 1…N in order, got %+v at %d", e, i)
		}
	}

	vs, err := p.Variants(ctx, d, eps[0])
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]bool{}
	for _, v := range vs {
		groups[v.Fansub] = true
		if v.Kind != "embed" || v.Lang != "tr" || v.URL == "" {
			t.Errorf("variant: %+v", v)
		}
		if strings.Contains(v.Fansub, "Çevirmen") || strings.Contains(v.Fansub, "/") {
			t.Errorf("a credit line reached the picker: %q", v.Fansub)
		}
	}
	// "Shikisubs" → the registered "ShikiSubs"; "Çevirmen: Cent / Redaktör: …" → "Cent";
	// "Çevirmen: Metincix / Dizgi: Metincix" merges with "Metincix"
	for _, want := range []string{"ShikiSubs", "Cent", "Metincix"} {
		if !groups[want] {
			t.Errorf("group %q missing: %v", want, groups)
		}
	}
	if vs[0].Host != "Tau Video" {
		t.Errorf("Tau Video should rank first, got %s", vs[0].Host)
	}
}

// Without the x-e-h header the site answers with another title: the
// provider must refuse it rather than show the wrong anime.
func TestRecordedRejectsPoisonedTitle(t *testing.T) {
	serveRecorded(t)
	old := headers["x-e-h"]
	delete(headers, "x-e-h")
	defer func() { headers["x-e-h"] = old }()
	if _, err := (Provider{}).Details(context.Background(), frierenRef()); err == nil {
		t.Fatal("accepted a different title")
	}
}

func frierenRef() source.Anime { return source.Anime{Source: "animecix", ID: "11130"} }
