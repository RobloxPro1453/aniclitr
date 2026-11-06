package extract

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"aniclitr/internal/source"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   Kind
	}{
		{200, `{"error":{"title":"Özel içerik.","message":"sahibi tarafından özel olarak etiketlendi"}}`, KindPrivate},
		{200, `<title>Google Drive - Quota exceeded</title>`, KindQuota},
		{429, `{"error":{"code":429}}`, KindRateLimited},
		{429, "", KindRateLimited},
		{404, `<html>not found</html>`, KindDeleted},
		{404, "", KindNotFound},
		{410, "", KindDeleted},
		{403, "", KindUnknown},
		{200, "this video has been deleted", KindDeleted},
		{200, "Bu video silinmiş", KindDeleted},
		{200, `{"noAd":{"reason":"embed|blocklist"}}`, KindEmbedBlocked},
		{200, "removed due to copyright claim", KindCopyright},
		{403, `<title>Just a moment...</title><form id="challenge-form"`, KindBlocked},
		{403, "checking your browser before you access the site", KindBlocked},
		{200, "Видео заблокировано из-за нарушений авторских прав", KindCopyright},
		{200, "File was deleted", KindDeleted},
		{200, "File is no longer available as it expired or has been deleted.", KindDeleted},
		{200, "Видео удалено", KindDeleted},
		{200, "The author of this video has not been found or is blocked", KindDeleted},
		{200, "This video was removed at the request of the copyright holder", KindCopyright},
		{200, `{"error":{"title":"Publishing in progress...","code":"DM006"}}`, KindProcessing},
		{200, `<div class="copyright">&copy; mp4upload</div><h4>File Not Found</h4>`, KindDeleted},
		{200, "store everything privately and securely", KindUnknown},
		{200, `<html><body>normal player page</body></html>`, KindUnknown},
		{200, "", KindUnknown},
	}
	for i, tc := range cases {
		got := Classify(tc.status, []byte(tc.body))
		if got != tc.want {
			t.Errorf("case %d (status=%d body=%q): got %v, want %v", i, tc.status, tc.body, got, tc.want)
		}
	}
}

func TestPermanentAndMessage(t *testing.T) {
	perm := []Kind{KindNotFound, KindDeleted, KindPrivate, KindCopyright, KindEmbedBlocked}
	for _, k := range perm {
		if !k.Permanent() {
			t.Errorf("%v should be permanent", k)
		}
		if k.Message() == "" {
			t.Errorf("%v should have a Turkish message", k)
		}
	}
	transient := []Kind{KindUnknown, KindQuota, KindRateLimited, KindChanged, KindBlocked, KindProcessing}
	for _, k := range transient {
		if k.Permanent() {
			t.Errorf("%v should NOT be permanent", k)
		}
	}
}

func TestSummarizeAndDedupe(t *testing.T) {
	s := Summarize("<title>\n  Google Drive  -\n  Quota exceeded</title>", 200)
	if strings.Contains(s, "\n") || !strings.Contains(s, "Quota exceeded") {
		t.Fatalf("unexpected summary: %q", s)
	}
	fails := DedupeFailures([]Failure{
		{Extractor: "A", Kind: KindDeleted, Detail: "gone"},
		{Extractor: "B", Kind: KindDeleted, Detail: "gone"},
		{Extractor: "C", Kind: KindQuota, Detail: "quota"},
	})
	if len(fails) != 2 {
		t.Fatalf("got %d failures after dedupe, want 2", len(fails))
	}
}

func TestBuiltInRegistryMatches(t *testing.T) {
	if !CanExtract("https://tau-video.xyz/embed/649a3a9edbea6eaa6569012e") {
		t.Fatal("Tau Video should be claimed by registry")
	}
	if !CanExtract("https://video.sibnet.ru/shell.php?videoid=3642169") {
		t.Fatal("Sibnet should be claimed by registry")
	}
	if !CanExtract("https://drive.google.com/file/d/1VfeTq_uYg4rlmG1tkW02Lahv6g4Q8h_R/preview") {
		t.Fatal("Google Drive should be claimed by registry")
	}
	if !CanExtract("https://ok.ru/videoembed/123456789") {
		t.Fatal("OK.ru should be claimed by registry")
	}
	if !CanExtract("https://vidmoly.to/embed-abc123.html") {
		t.Fatal("Vidmoly should be claimed by registry")
	}
	if !CanExtract("https://voe.sx/e/xyz987") {
		t.Fatal("Voe should be claimed by registry")
	}
}

func TestTauExtractorWithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/video/ok123":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urls":[{"label":"720p","url":"https://cdn.example/720.mp4","size":100},{"label":"1080p","url":"https://cdn.example/1080.mp4","size":200}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Video not found"}`))
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL + "/embed/ok123")
	st, err := Tau(context.Background(), u)
	if err != nil {
		t.Fatalf("Tau ok123 failed: %v", err)
	}
	if len(st.Qualities) != 2 || st.Qualities[0].Height != 1080 {
		t.Fatalf("unexpected qualities: %+v", st.Qualities)
	}

	uDead, _ := url.Parse(srv.URL + "/embed/dead999")
	_, err = Tau(context.Background(), uDead)
	if err == nil {
		t.Fatal("expected error for dead tau video")
	}
	if k := KindOf(err); !k.Permanent() {
		t.Fatalf("expected permanent error kind for dead tau video, got %v (%v)", k, err)
	}
}

func TestSibnetExtractorWithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("videoid") {
		case "111":
			_, _ = w.Write([]byte(`<html><script>player.src([{src: "/v/abc111.mp4", type: "video/mp4"}]);</script></html>`))
		case "222":
			_, _ = w.Write([]byte(`<html><body>Видео удалено</body></html>`))
		default:
			_, _ = w.Write([]byte(`<html><body>unexpected new layout</body></html>`))
		}
	}))
	defer srv.Close()

	// Path form (/video111) should rewrite to /shell.php?videoid=111
	st, err := Sibnet(context.Background(), srv.URL+"/video111")
	if err != nil {
		t.Fatalf("Sibnet 111 failed: %v", err)
	}
	if len(st.Qualities) != 1 || !strings.HasSuffix(st.Qualities[0].URL, "/v/abc111.mp4") {
		t.Fatalf("unexpected sibnet stream: %+v", st)
	}

	// Deleted video page -> KindDeleted (Permanent)
	_, err = Sibnet(context.Background(), srv.URL+"/shell.php?videoid=222")
	if KindOf(err) != KindDeleted {
		t.Fatalf("expected KindDeleted for removed sibnet video, got %v (%v)", KindOf(err), err)
	}

	// Unrecognised HTML without error markers -> KindChanged
	_, err = Sibnet(context.Background(), srv.URL+"/shell.php?videoid=333")
	if KindOf(err) != KindChanged {
		t.Fatalf("expected KindChanged for altered sibnet layout, got %v (%v)", KindOf(err), err)
	}
}

type fakeDeletedExtractor struct{}

func (fakeDeletedExtractor) Name() string { return "FakeDeadHost" }
func (fakeDeletedExtractor) CanHandle(u *url.URL) bool {
	return u != nil && u.Host == "fakedead.example.com"
}
func (fakeDeletedExtractor) Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	return nil, NewError(KindDeleted, "File was deleted")
}

func TestResolveShortCircuitsOnPermanentError(t *testing.T) {
	Register(fakeDeletedExtractor{})
	st, fails := ResolveDetailed(context.Background(), "https://fakedead.example.com/embed/1", nil)
	if st != nil {
		t.Fatal("expected nil stream")
	}
	// Because KindDeleted is permanent, yt-dlp must NOT be called as a second failure.
	if len(fails) != 1 || fails[0].Extractor != "FakeDeadHost" || fails[0].Kind != KindDeleted {
		t.Fatalf("expected single FakeDeadHost KindDeleted failure without yt-dlp fallback, got %+v", fails)
	}
}

func TestHLSVariantsParser(t *testing.T) {
	master := []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=86000,URI="iframe.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2800000,AVERAGE-BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.64001f,mp4a.40.2"
720p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5200000,AVERAGE-BANDWIDTH=4500000,RESOLUTION=1920x1080,CODECS="hvc1.1.6.L120.90,mp4a.40.2"
https://cdn.example.com/hls/1080p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=999999999999,RESOLUTION=3840x2160
absurd.m3u8
`)
	base, _ := url.Parse("https://cdn.example.com/hls/master.m3u8")
	qs := Variants(master, base)
	if len(qs) != 2 {
		t.Fatalf("expected 2 valid variants (skipping I-frame and absurd bandwidth), got %d: %+v", len(qs), qs)
	}
	if qs[0].Height != 1080 || qs[0].Codec != "HEVC" || qs[0].Bitrate != 4500000 || !qs[0].Probed {
		t.Fatalf("unexpected first variant: %+v", qs[0])
	}
	if qs[1].Height != 720 || qs[1].Codec != "H.264" || qs[1].URL != "https://cdn.example.com/hls/720p/index.m3u8" {
		t.Fatalf("unexpected second variant: %+v", qs[1])
	}
}

func buildFakeMoov(timescale, durationSec, width, height uint32, codecFourCC string) []byte {
	box := func(typ string, payload []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(8+len(payload)))
		b.WriteString(typ)
		b.Write(payload)
		return b.Bytes()
	}
	// mvhd v0: 12 bytes pre + 4 timescale + 4 duration
	mvhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mvhd[12:16], timescale)
	binary.BigEndian.PutUint32(mvhd[16:20], timescale*durationSec)

	// tkhd v0: 76 bytes pre + 4 width (16.16) + 4 height (16.16)
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[76:80], width<<16)
	binary.BigEndian.PutUint32(tkhd[80:84], height<<16)

	// stsd v0: 4 version/flags + 4 entry count + entry (4 size + 4 fourCC + 8 pad)
	stsdEntry := box(codecFourCC, make([]byte, 8))
	stsd := make([]byte, 8+len(stsdEntry))
	binary.BigEndian.PutUint32(stsd[4:8], 1)
	copy(stsd[8:], stsdEntry)

	stbl := box("stsd", stsd)
	minf := box("stbl", stbl)
	mdia := box("minf", minf)
	trak := box("trak", append(box("tkhd", tkhd), box("mdia", mdia)...))
	return box("moov", append(box("mvhd", mvhd), trak...))
}

func TestMP4ProbeAndBrokenMarking(t *testing.T) {
	moov := buildFakeMoov(1000, 1440, 1920, 1080, "avc1") // 24 min, 1080p, H.264
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.mp4":
			w.Header().Set("Content-Range", "bytes 0-131071/754974720") // ~720 MB
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(moov)
		case "/garbage.mp4":
			w.Header().Set("Content-Range", "bytes 0-100/1000")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("not an mp4 file at all"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	qGood := source.Quality{Label: "auto", URL: srv.URL + "/good.mp4"}
	ProbeQuality(context.Background(), &qGood, "")
	if qGood.Broken || !qGood.Probed || qGood.Height != 1080 || qGood.Codec != "H.264" || qGood.Duration != 1440 || qGood.Bitrate <= 0 {
		t.Fatalf("expected valid 1080p H.264 MP4 probe, got %+v", qGood)
	}

	qGarbage := source.Quality{Label: "auto", URL: srv.URL + "/garbage.mp4"}
	ProbeQuality(context.Background(), &qGarbage, "")
	if !qGarbage.Broken {
		t.Fatalf("expected unprobable garbage MP4 to be marked Broken, got %+v", qGarbage)
	}

	qDead := source.Quality{Label: "1080p", Height: 1080, URL: srv.URL + "/missing.mp4"}
	ProbeQuality(context.Background(), &qDead, "")
	if !qDead.Broken {
		t.Fatalf("expected 404 MP4 to be marked Broken, got %+v", qDead)
	}
}

func TestOkRuVidmolyAndVoeExtractors(t *testing.T) {
	// Helper to encode a JSON payload with the reverse of Voe's 6-step decryption pipeline:
	// Decrypt: rot13 -> pattern->'_' -> strip '_' -> b64dec -> charShift(-3) -> reverse -> b64dec
	// Encrypt: b64enc -> reverse -> charShift(+3) -> b64enc -> rot13 (with pattern noise inserted)
	voeEncrypt := func(plainJSON string) string {
		b1 := base64.StdEncoding.EncodeToString([]byte(plainJSON))
		rev := voeReverse(b1)
		shifted := make([]byte, len(rev))
		for i := 0; i < len(rev); i++ {
			shifted[i] = rev[i] + 3
		}
		b2 := base64.StdEncoding.EncodeToString(shifted)
		rot := voeRot13(b2)
		return rot[:4] + "@$" + rot[4:]
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/videoembed/999":
			meta := `{"videos":[{"name":"hd","url":"https://vd.ok.ru/720.mp4"},{"name":"full","url":"https://vd.ok.ru/1080.mp4"}]}`
			opts := fmt.Sprintf(`{"flashvars":{"metadata":%q}}`, meta)
			fmt.Fprintf(w, `<div data-options="%s"></div>`, html.EscapeString(opts))
		case "/embed-vm1.html":
			fmt.Fprint(w, `<script>player.setup({sources: [{file:"https://cdn.vidmoly.to/hls/master.m3u8"}]});</script>`)
		case "/voe-start":
			fmt.Fprintf(w, `<script>window.location.href = '%s/voe-real';</script>`, "http://"+r.Host)
		case "/voe-real":
			enc := voeEncrypt(`{"source":"https://cdn.voe.sx/hls/index.m3u8","direct_access_url":"https://cdn.voe.sx/mp4/720.mp4"}`)
			fmt.Fprintf(w, `<html><script type="application/json">[%q]</script></html>`, enc)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()

	stOk, err := OkRu(ctx, srv.URL+"/video/999")
	if err != nil || len(stOk.Qualities) != 2 || stOk.Qualities[0].Height != 1080 || stOk.Qualities[1].Height != 720 {
		t.Fatalf("OkRu failed: st=%+v err=%v", stOk, err)
	}

	stVm, err := Vidmoly(ctx, srv.URL+"/w/vm1")
	if err != nil || len(stVm.Qualities) != 1 || stVm.Qualities[0].URL != "https://cdn.vidmoly.to/hls/master.m3u8" {
		t.Fatalf("Vidmoly failed: st=%+v err=%v", stVm, err)
	}

	stVoe, err := Voe(ctx, srv.URL+"/voe-start")
	if err != nil || len(stVoe.Qualities) != 1 || stVoe.Qualities[0].URL != "https://cdn.voe.sx/hls/index.m3u8" {
		t.Fatalf("Voe failed: st=%+v err=%v", stVoe, err)
	}
}
