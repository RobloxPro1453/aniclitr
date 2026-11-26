package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeStreamNonForcingAdvice(t *testing.T) {
	// High bitrate 1080p -> keep original
	a1 := AnalyzeStream(1080, 4_000_000)
	if a1.SuggestUpscale != "" || a1.SuggestAnime4K != "" {
		t.Fatalf("clean 1080p should recommend untouched original, got %+v", a1)
	}
	// Low bitrate 1080p -> gentle Anime4K suggestion
	a2 := AnalyzeStream(1080, 800_000)
	if a2.SuggestAnime4K != "hızlı" {
		t.Fatalf("low-bitrate 1080p should gently suggest hızlı, got %+v", a2)
	}
	// 720p -> optional FSR 1080p suggestion
	a3 := AnalyzeStream(720, 1_500_000)
	if a3.SuggestUpscale != "1080p" {
		t.Fatalf("720p should suggest optional 1080p, got %+v", a3)
	}
}

func TestSearchSceneRequiresUserEndpointAndParsesResponse(t *testing.T) {
	if _, err := SearchScene(context.Background(), "", "", "x.jpg"); err == nil {
		t.Fatal("empty endpoint must fail cleanly (no hardcoded developer API)")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-trace-key") != "user-key-123" {
			t.Errorf("expected user key header, got %q", r.Header.Get("x-trace-key"))
		}
		_, _ = w.Write([]byte(`{"result":[{"anilist":{"id":154587,"idMal":52991,"title":{"romaji":"Sousou no Frieren"}},"episode":12,"from":642.5,"similarity":0.97}]}`))
	}))
	defer srv.Close()

	img := filepath.Join(t.TempDir(), "frame.jpg")
	_ = os.WriteFile(img, []byte("jpeg"), 0o644)
	hits, err := SearchScene(context.Background(), srv.URL, "user-key-123", img)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "Sousou no Frieren" || hits[0].Episode != 12 || hits[0].MalID != 52991 {
		t.Fatalf("unexpected scene hits: %+v", hits)
	}
}

func TestSanitizeFilename(t *testing.T) {
	got := sanitizeFilename("Re:ZERO - Starting Life / Ep?!")
	if strings.ContainsAny(got, ":/?!") || got == "" {
		t.Fatalf("unsafe filename: %q", got)
	}
}
