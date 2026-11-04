package download

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/source"
)

type fakeProv struct{ url string }

func (f fakeProv) Info() source.Info                                             { return source.Info{ID: "fakedl", Name: "Fake"} }
func (fakeProv) Search(context.Context, string) ([]source.Anime, error)          { return nil, nil }
func (fakeProv) Details(_ context.Context, a source.Anime) (source.Anime, error) { return a, nil }
func (fakeProv) Episodes(context.Context, source.Anime, int) ([]source.Episode, error) {
	return nil, nil
}
func (f fakeProv) Variants(context.Context, source.Anime, source.Episode) ([]source.Variant, error) {
	return []source.Variant{{Fansub: "Hızlı Fansub", Host: "H", URL: "a"}, {Fansub: "Seçilen Fansub", Host: "S", URL: "b"}}, nil
}
func (f fakeProv) Resolve(_ context.Context, v source.Variant) (*source.Stream, error) {
	return &source.Stream{Qualities: []source.Quality{{Label: "720p", Height: 720, URL: f.url + "/" + v.URL}}}, nil
}

// The fansub you choose is binding: its file is downloaded even when another
// fansub comes first, and a fansub that is missing fails instead of switching.
func TestStrictFansub(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	noTools = true
	defer func() { noTools = false }()
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path)
		http.ServeContent(w, r, "x.mp4", time.Now(), bytes.NewReader([]byte("video")))
	}))
	defer srv.Close()
	source.Register(fakeProv{url: srv.URL})
	m := Open(t.TempDir())
	an := source.Anime{Source: "fakedl", ID: "1", Title: "Deneme"}
	ok, _ := m.Add(Request{Anime: an, Ep: source.Episode{Season: 1, Number: 1}, PrefFansub: "Seçilen Fansub", Strict: true})
	bad, _ := m.Add(Request{Anime: an, Ep: source.Episode{Season: 1, Number: 2}, PrefFansub: "Olmayan Fansub", Strict: true})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m.Active() == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, j := range m.Jobs() {
		switch j.ID {
		case ok.ID:
			if j.State != Done || j.Fansub != "Seçilen Fansub" {
				t.Fatalf("chosen fansub not used: %+v", j)
			}
		case bad.ID:
			if j.State != Failed || !strings.Contains(j.Err, "Olmayan Fansub") {
				t.Fatalf("missing fansub should fail, not switch: %+v", j)
			}
		}
	}
	for _, p := range got {
		if strings.HasSuffix(p, "/a") {
			t.Fatal("the other fansub's file was downloaded")
		}
	}
}
