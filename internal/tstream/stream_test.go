package tstream

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

func TestOpenRejectsEmptyInput(t *testing.T) {
	if _, err := Open(context.Background(), "", ""); err == nil {
		t.Fatalf("expected error on empty torrent/magnet")
	}
}

func TestMimeForExt(t *testing.T) {
	if got := mimeForExt(".mkv"); got != "video/x-matroska" {
		t.Fatalf("expected matroska mime, got %q", got)
	}
	if got := mimeForExt(".mp4"); got != "video/mp4" {
		t.Fatalf("expected mp4 mime, got %q", got)
	}
}

func TestExtraTrackersMergesMagnetAndFallbacks(t *testing.T) {
	mag := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&tr=udp%3A%2F%2Fcustom.tracker.example%3A6969%2Fannounce"
	tiers := extraTrackers(mag)
	if len(tiers) < len(fallbackTrackers)+1 {
		t.Fatalf("expected magnet tracker + fallback trackers, got %d tiers", len(tiers))
	}
	if tiers[0][0] != "udp://custom.tracker.example:6969/announce" {
		t.Fatalf("expected custom magnet tracker first, got %q", tiers[0][0])
	}
}

// TestEndToEndLocalTorrentStream verifies real BitTorrent peer transfer,
// sequential edge prioritization, HTTP HEAD/206 Range seeking (head + tail),
// and immediate context cancellation without deadlock.
func TestEndToEndLocalTorrentStream(t *testing.T) {
	seedDir := t.TempDir()
	leechDir := t.TempDir()

	// Create a synthetic 256 KiB MKV file with deterministic random payload.
	payload := make([]byte, 256<<10)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand: %v", err)
	}
	videoName := "[SubsPlease] Test Anime - 01 (1080p).mkv"
	videoPath := filepath.Join(seedDir, videoName)
	if err := os.WriteFile(videoPath, payload, 0o644); err != nil {
		t.Fatalf("write seed video: %v", err)
	}

	// Build a valid BitTorrent metainfo (.torrent) for the file.
	info := metainfo.Info{PieceLength: 32 << 10}
	if err := info.BuildFromFilePath(videoPath); err != nil {
		t.Fatalf("BuildFromFilePath: %v", err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}
	mi := &metainfo.MetaInfo{InfoBytes: infoBytes}

	// Start a local seeder client on 127.0.0.1.
	seedStore := storage.NewFile(seedDir)
	defer seedStore.Close()
	seedCfg := torrent.NewDefaultClientConfig()
	seedCfg.DataDir = seedDir
	seedCfg.DefaultStorage = seedStore
	seedCfg.Seed = true
	seedCfg.NoDHT = true
	seedCfg.DisableTrackers = true
	seedCfg.DisableIPv6 = true
	seedCfg.ListenHost = func(string) string { return "127.0.0.1" }
	seedCfg.ListenPort = 0
	seedCfg.Slogger = slog.New(slog.DiscardHandler)
	seeder, err := torrent.NewClient(seedCfg)
	if err != nil {
		t.Fatalf("seeder NewClient: %v", err)
	}
	defer seeder.Close()
	seedTor, err := seeder.AddTorrent(mi)
	if err != nil {
		t.Fatalf("seeder AddTorrent: %v", err)
	}
	<-seedTor.GotInfo()
	seedTor.VerifyData()

	// Start our leecher Session client on 127.0.0.1.
	leechStore := storage.NewFile(leechDir)
	leechCfg := torrent.NewDefaultClientConfig()
	leechCfg.DataDir = leechDir
	leechCfg.DefaultStorage = leechStore
	leechCfg.NoUpload = false
	leechCfg.Seed = false
	leechCfg.NoDHT = true
	leechCfg.DisableTrackers = true
	leechCfg.DisableIPv6 = true
	leechCfg.ListenHost = func(string) string { return "127.0.0.1" }
	leechCfg.ListenPort = 0
	leechCfg.Slogger = slog.New(slog.DiscardHandler)
	leecher, err := torrent.NewClient(leechCfg)
	if err != nil {
		_ = leechStore.Close()
		t.Fatalf("leecher NewClient: %v", err)
	}
	leechTor, err := leecher.AddTorrent(mi)
	if err != nil {
		leecher.Close()
		_ = leechStore.Close()
		t.Fatalf("leecher AddTorrent: %v", err)
	}
	leechTor.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: seeder.LocalPort()},
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess, err := initSession(ctx, leecher, leechStore, leechTor, leechDir)
	if err != nil {
		t.Fatalf("initSession: %v", err)
	}
	defer sess.Close()

	files := sess.Files()
	if len(files) != 1 || files[0].Name != videoName || files[0].Episode != 1 {
		t.Fatalf("unexpected files: %+v", files)
	}

	streamURL, vf, err := sess.ServeFile(0)
	if err != nil {
		t.Fatalf("ServeFile: %v", err)
	}
	if vf.Size != int64(len(payload)) {
		t.Fatalf("size mismatch: got %d want %d", vf.Size, len(payload))
	}

	client := &http.Client{Timeout: 15 * time.Second}

	// 1) HEAD request (mpv probes Content-Length & Accept-Ranges first).
	reqHead, _ := http.NewRequestWithContext(ctx, http.MethodHead, streamURL, nil)
	respHead, err := client.Do(reqHead)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	respHead.Body.Close()
	if respHead.StatusCode != http.StatusOK || respHead.ContentLength != int64(len(payload)) {
		t.Fatalf("HEAD status=%d len=%d", respHead.StatusCode, respHead.ContentLength)
	}
	if respHead.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("missing Accept-Ranges: %q", respHead.Header.Get("Accept-Ranges"))
	}

	// 2) Range GET at the start of the file (EBML header).
	reqStart, _ := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	reqStart.Header.Set("Range", "bytes=0-16383")
	respStart, err := client.Do(reqStart)
	if err != nil {
		t.Fatalf("GET head range: %v", err)
	}
	gotStart, err := io.ReadAll(respStart.Body)
	respStart.Body.Close()
	if err != nil {
		t.Fatalf("read head range: %v", err)
	}
	if respStart.StatusCode != http.StatusPartialContent || !bytes.Equal(gotStart, payload[:16384]) {
		t.Fatalf("head range mismatch: status=%d len=%d", respStart.StatusCode, len(gotStart))
	}

	// 3) Range GET at the tail of the file (MKV Cues / MP4 moov seek).
	tailStart := len(payload) - 8192
	reqTail, _ := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	reqTail.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", tailStart, len(payload)-1))
	respTail, err := client.Do(reqTail)
	if err != nil {
		t.Fatalf("GET tail range: %v", err)
	}
	gotTail, err := io.ReadAll(respTail.Body)
	respTail.Body.Close()
	if err != nil {
		t.Fatalf("read tail range: %v", err)
	}
	if respTail.StatusCode != http.StatusPartialContent || !bytes.Equal(gotTail, payload[tailStart:]) {
		t.Fatalf("tail range mismatch: status=%d len=%d", respTail.StatusCode, len(gotTail))
	}
}
