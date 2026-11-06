package anihub

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAniHubFlowAndHLSProbe(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/animes" && r.URL.Query().Get("q") == "frieren":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"success": true,
				"data": {
					"items": [
						{
							"id": "1",
							"title": "Sousou no Frieren",
							"slug": "sousou-no-frieren",
							"year": 2023,
							"posterKey": "posters/frieren.jpg",
							"contentType": "tv"
						}
					]
				}
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/animes/sousou-no-frieren":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"success": true,
				"data": {
					"anime": {
						"id": "1",
						"title": "Sousou no Frieren",
						"slug": "sousou-no-frieren",
						"description": "Elf büyücü Frieren'in yolculuğu.",
						"year": 2023,
						"posterKey": "posters/frieren.jpg",
						"contentType": "tv"
					},
					"episodes": [
						{"id": "e1", "slug": "sousou-no-frieren-1-bolum", "number": 1, "title": "Yolculuğun Sonu"},
						{"id": "e2", "slug": "sousou-no-frieren-2-bolum", "number": 2, "title": ""}
					],
					"series": {
						"currentSeasonIndex": 0,
						"seasons": [{"order": 1, "slug": "sousou-no-frieren"}]
					}
				}
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/episodes/sousou-no-frieren-1-bolum":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{
				"success": true,
				"data": {
					"playback": {
						"status": "ready",
						"masterPlaylistKey": "hls/frieren/ep1/master.m3u8",
						"masterPlaylistUrl": "%s/hls/frieren/ep1/master.m3u8",
						"subtitles": [{"url": "%s/subs/ep1.vtt", "language": "tr", "label": "Türkçe"}]
					}
				}
			}`, srv.URL, srv.URL)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/video-tokens":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"success": true, "data": {"token": "signed-tok-123", "expiresAt": 1999999999}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/hls/frieren/ep1/master.m3u8":
			if r.URL.Query().Get("token") != "signed-tok-123" {
				http.Error(w, "unauthorized", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=4500000,RESOLUTION=1920x1080,CODECS=\"avc1.640028,mp4a.40.2\"\n1080p.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2200000,RESOLUTION=1280x720,CODECS=\"avc1.4d401f,mp4a.40.2\"\n720p.m3u8\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	ctx := context.Background()
	p := Provider{}
	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) != 1 {
		t.Fatalf("Search got %v, err=%v", res, err)
	}
	if res[0].Poster != "https://media.anihub.com.tr/posters/frieren.jpg" {
		t.Fatalf("unexpected poster: %s", res[0].Poster)
	}

	det, err := p.Details(ctx, res[0])
	if err != nil || det.Description == "" || len(det.Seasons) != 1 {
		t.Fatalf("Details failed: %+v, err=%v", det, err)
	}

	eps, err := p.Episodes(ctx, det, 1)
	if err != nil || len(eps) != 2 {
		t.Fatalf("Episodes failed: %+v, err=%v", eps, err)
	}

	vs, err := p.Variants(ctx, det, eps[0])
	if err != nil || len(vs) != 1 {
		t.Fatalf("Variants failed: %+v, err=%v", vs, err)
	}

	st, err := p.Resolve(ctx, vs[0])
	if err != nil || st == nil || len(st.Qualities) != 3 {
		t.Fatalf("Resolve failed: %+v, err=%v", st, err)
	}
	if st.Qualities[0].Height != 1080 || st.Qualities[0].Codec != "H.264" || !strings.Contains(st.Qualities[0].URL, "1080p.m3u8") {
		t.Fatalf("unexpected top quality after HLS probe: %+v", st.Qualities[0])
	}
	if len(st.Subtitles) != 1 || st.Subtitles[0].Label != "Türkçe" {
		t.Fatalf("unexpected subtitles: %+v", st.Subtitles)
	}

	u, _ := url.Parse("https://anihub.com.tr/anime/sousou-no-frieren")
	openAn, _, handled, err := p.OpenURL(ctx, u)
	if !handled || err != nil || openAn.Title != "Sousou no Frieren" {
		t.Fatalf("OpenURL failed: handled=%v err=%v an=%+v", handled, err, openAn)
	}
}
