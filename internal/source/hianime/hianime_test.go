package hianime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"aniclitr/internal/source"
)

// serve replays pages recorded from hianime.at on 2026-11-07 (testdata/),
// so a parser break shows up without the network.
func serve(t *testing.T) {
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
		switch {
		case r.URL.Path == "/search":
			_, _ = w.Write(file("search.html"))
		case r.URL.Path == "/api/theme/episode/list/481":
			_, _ = w.Write(file("eplist.json"))
		case r.URL.Path == "/api/theme/episode/list/999": // in-between episodes
			_, _ = w.Write([]byte(`{"html":"<a title=\"Ep 12\" class=\"ssl-item ep-item\" data-number=\"12\" data-id=\"1\"></a><a title=\"Recap\" class=\"ssl-item ep-item ssl-item-filler\" data-number=\"12.5\" data-id=\"2\"></a><a title=\"Ep 13\" class=\"ssl-item ep-item\" data-number=\"13\" data-id=\"3\"></a>"}`))
		case r.URL.Path == "/api/theme/episode/servers":
			_, _ = w.Write(file("servers.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := base
	base = srv.URL
	t.Cleanup(func() { base = old })
}

func TestRecordedSearchEpisodesVariants(t *testing.T) {
	serve(t)
	ctx := context.Background()
	p := Provider{}

	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) < 2 {
		t.Fatalf("search: %v %+v", err, res)
	}
	an := res[0]
	if an.ID != "frieren-beyond-journeys-end-481" || an.Title != "Frieren: Beyond Journey's End" || an.Poster == "" || an.EpCount == 0 {
		t.Fatalf("first card parsed wrong: %+v", an)
	}

	eps, err := p.Episodes(ctx, an, 1)
	if err != nil || len(eps) != an.EpCount {
		t.Fatalf("episodes: %v, %d (card says %d)", err, len(eps), an.EpCount)
	}
	if eps[0].Number != 1 || eps[0].ID == "" || eps[len(eps)-1].Number != len(eps) {
		t.Fatalf("episode numbering: first %+v last %+v", eps[0], eps[len(eps)-1])
	}

	vs, err := p.Variants(ctx, an, eps[0])
	if err != nil || len(vs) == 0 {
		t.Fatalf("variants: %v", err)
	}
	for _, v := range vs {
		if !strings.HasPrefix(v.URL, "http") || v.Host == "" || v.Kind != "embed" {
			t.Errorf("variant: %+v", v)
		}
		if v.Dub != strings.HasPrefix(v.Fansub, "Dublaj") || (v.Lang == "en") == strings.HasPrefix(v.Fansub, "Ham") {
			t.Errorf("track flags disagree with the label: %+v", v)
		}
	}
}

func TestInBetweenEpisodes(t *testing.T) {
	serve(t)
	eps, err := Provider{}.Episodes(context.Background(), source.Anime{ID: "x-999"}, 1)
	if err != nil || len(eps) != 3 {
		t.Fatalf("%v %+v", err, eps)
	}
	// 12.5 used to become episode 0; now it keeps its own key, between 12 and 13.
	// It is also marked filler, which once made it vanish from the list.
	if eps[0].Number != 12 || !eps[1].Extra() || eps[1].Num() != "12,5" || eps[2].Number != 13 {
		t.Fatalf("got %+v", eps)
	}
}
