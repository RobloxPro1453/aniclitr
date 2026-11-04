package download

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"aniclitr/internal/source"
)

func TestNativeDownload(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	noTools = true
	defer func() { noTools = false }()
	data := bytes.Repeat([]byte("aniclitr"), 400_000) // 3.2 MB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x.mp4", time.Now(), bytes.NewReader(data))
	}))
	defer srv.Close()
	m := Open(t.TempDir())
	st := &source.Stream{Qualities: []source.Quality{{Label: "720p", URL: srv.URL + "/x.mp4", Height: 720}}}
	q := st.Qualities[0]
	j, _ := m.Add(Request{Anime: source.Anime{Source: "t", ID: "1", Title: "Deneme: Anime?"}, Ep: source.Episode{Season: 1, Number: 3}, Stream: st, Quality: &q})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if x, ok := m.Local(j.Key(), 1, 3); ok {
			b, _ := os.ReadFile(x.Path)
			if !bytes.Equal(b, data) {
				t.Fatal("content mismatch")
			}
			t.Logf("saved %s (%d bytes, tool %s)", x.Path, len(b), x.Tool)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("not finished: %+v", m.Jobs())
}
