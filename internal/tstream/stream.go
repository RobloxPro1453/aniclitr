// Package tstream embeds a pure-Go BitTorrent streaming engine (anacrolix/torrent)
// that opens a .torrent or magnet link, lists playable video files inside it,
// and streams any chosen file directly to mpv over a local HTTP Range server
// without waiting for a full download or needing an external torrent client.
package tstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"aniclitr/internal/config"
	"aniclitr/internal/download"
	"aniclitr/internal/netx"
)

// fallbackTrackers supplement .torrent files whose single tracker (e.g.
// nyaa.tracker.wf) may be blocked or unreachable on some networks.
var fallbackTrackers = []string{
	"http://nyaa.tracker.wf:7777/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"http://tracker.opentrackr.org:1337/announce",
	"https://tracker.tamersunion.org:443/announce",
}

// VideoFile is one playable video entry inside a torrent.
type VideoFile struct {
	Index   int
	Name    string
	RelPath string
	Size    int64
	Season  int
	Episode int
	file    *torrent.File
}

// Stats holds live swarm telemetry for an active stream session.
type Stats struct {
	Peers     int
	Seeders   int
	Completed int64
	Length    int64
}

// Session owns an embedded BitTorrent client, a single torrent, and an optional
// local HTTP Range streaming server for mpv.
type Session struct {
	mu      sync.Mutex
	client  *torrent.Client
	store   storage.ClientImplCloser
	tor     *torrent.Torrent
	tempDir string
	files   []VideoFile
	active  *torrent.File
	srv     *http.Server
	ln      net.Listener
	closed  bool
}

// Open loads torrent metadata (preferring the instant .torrent URL over DHT magnet)
// and returns a Session listing all playable video files inside.
func Open(ctx context.Context, torrentURL, magnet string) (*Session, error) {
	torrentURL = strings.TrimSpace(torrentURL)
	magnet = strings.TrimSpace(magnet)
	if torrentURL == "" && magnet == "" {
		return nil, errors.New("geçerli torrent veya magnet bağlantısı yok")
	}

	root := filepath.Join(config.CacheDir(), "tstream")
	go cleanupStaleDirs(root, 6*time.Hour)

	tempDir := filepath.Join(root, fmt.Sprintf("s-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, err
	}

	st := storage.NewFile(tempDir)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = tempDir
	cfg.DefaultStorage = st
	// Allow tit-for-tat reciprocation while streaming so peers unchoke us,
	// without continuing to seed after playback stops.
	cfg.NoUpload = false
	cfg.Seed = false
	cfg.DisableIPv6 = true // prevents IPv6 UDP tracker/DHT stalls on IPv4-only networks
	cfg.ListenPort = 0
	cfg.EstablishedConnsPerTorrent = 80
	cfg.HalfOpenConnsPerTorrent = 40
	cfg.Slogger = slog.New(slog.DiscardHandler)

	cl, err := torrent.NewClient(cfg)
	if err != nil {
		_ = st.Close()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("torrent motoru başlatılamadı: %w", err)
	}

	var tor *torrent.Torrent
	if torrentURL != "" {
		if body, gerr := netx.Get(ctx, torrentURL, nil); gerr == nil && len(body) > 0 {
			if mi, lerr := metainfo.Load(bytes.NewReader(body)); lerr == nil {
				tor, err = cl.AddTorrent(mi)
			}
		}
	}
	if tor == nil && magnet != "" {
		tor, err = cl.AddMagnet(magnet)
	}
	if err != nil || tor == nil {
		cl.Close()
		_ = st.Close()
		_ = os.RemoveAll(tempDir)
		if err == nil {
			err = errors.New("torrent metaverisi alınamadı")
		}
		return nil, err
	}

	// Always merge magnet trackers and reliable public trackers so peer discovery
	// succeeds even when the .torrent's primary tracker is down or throttled.
	if extra := extraTrackers(magnet); len(extra) > 0 {
		tor.AddTrackers(extra)
	}

	return initSession(ctx, cl, st, tor, tempDir)
}

// initSession waits for metadata and indexes playable video files.
// Split out so unit tests can attach a local peer directly to a Session.
func initSession(ctx context.Context, cl *torrent.Client, st storage.ClientImplCloser, tor *torrent.Torrent, tempDir string) (*Session, error) {
	select {
	case <-tor.GotInfo():
	case <-ctx.Done():
		cl.Close()
		if st != nil {
			_ = st.Close()
		}
		_ = os.RemoveAll(tempDir)
		return nil, errors.New("torrent bilgisi zaman aşımına uğradı (eşler yanıt vermedi)")
	}

	// Do not download any file until the user selects one to stream.
	for _, f := range tor.Files() {
		f.SetPriority(torrent.PiecePriorityNone)
	}

	var vids []VideoFile
	for _, f := range tor.Files() {
		rel := f.DisplayPath()
		if !download.IsVideoExt(filepath.Ext(rel)) {
			continue
		}
		parsed := download.ParseRelease(filepath.Base(rel))
		vids = append(vids, VideoFile{
			Name:    filepath.Base(rel),
			RelPath: rel,
			Size:    f.Length(),
			Season:  parsed.Season,
			Episode: parsed.Episode,
			file:    f,
		})
	}
	sort.SliceStable(vids, func(i, j int) bool {
		if vids[i].Season != vids[j].Season {
			return vids[i].Season < vids[j].Season
		}
		if vids[i].Episode != vids[j].Episode && vids[i].Episode > 0 && vids[j].Episode > 0 {
			return vids[i].Episode < vids[j].Episode
		}
		return strings.ToLower(vids[i].RelPath) < strings.ToLower(vids[j].RelPath)
	})
	for i := range vids {
		vids[i].Index = i
	}
	if len(vids) == 0 {
		cl.Close()
		if st != nil {
			_ = st.Close()
		}
		_ = os.RemoveAll(tempDir)
		return nil, errors.New("bu torrent içinde oynatılabilir video dosyası (.mkv/.mp4) bulunamadı")
	}

	return &Session{
		client:  cl,
		store:   st,
		tor:     tor,
		tempDir: tempDir,
		files:   vids,
	}, nil
}

// Name returns the torrent's display name.
func (s *Session) Name() string {
	if s.tor != nil {
		return s.tor.Name()
	}
	return ""
}

// Files returns the sorted playable video files inside this torrent.
func (s *Session) Files() []VideoFile {
	out := make([]VideoFile, len(s.files))
	copy(out, s.files)
	return out
}

// ServeFile starts (or switches) the local HTTP Range server to stream file idx
// directly into mpv with sequential edge piece prioritization.
func (s *Session) ServeFile(idx int) (string, VideoFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", VideoFile{}, errors.New("torrent oturumu kapalı")
	}
	if idx < 0 || idx >= len(s.files) {
		return "", VideoFile{}, errors.New("geçersiz dosya sırası")
	}
	vf := s.files[idx]
	// Keep every file at PiecePriorityNone so rarest-first never wastes peer
	// slots on random middle pieces; edge priorities + Reader.SetReadahead drive
	// strictly sequential streaming right ahead of mpv's playhead.
	for _, f := range s.tor.Files() {
		f.SetPriority(torrent.PiecePriorityNone)
	}
	prioritizeEdges(vf.file)
	s.active = vf.file

	if s.ln == nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", VideoFile{}, fmt.Errorf("yerel akış sunucusu açılamadı: %w", err)
		}
		s.ln = ln
		mux := http.NewServeMux()
		mux.HandleFunc("/stream", s.handleStream)
		s.srv = &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() { _ = s.srv.Serve(ln) }()
	}

	ext := filepath.Ext(vf.Name)
	if ext == "" {
		ext = ".mkv"
	}
	u := fmt.Sprintf("http://%s/stream?i=%d&ext=%s", s.ln.Addr().String(), idx, ext)
	return u, vf, nil
}

func (s *Session) handleStream(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	closed := s.closed
	var f *torrent.File
	if q := r.URL.Query().Get("i"); q != "" {
		if idx, err := strconv.Atoi(q); err == nil && idx >= 0 && idx < len(s.files) {
			f = s.files[idx].file
		}
	}
	if f == nil {
		f = s.active
	}
	s.mu.Unlock()
	if closed || f == nil {
		http.Error(w, "stream closed", http.StatusGone)
		return
	}
	rd := f.NewReader()
	defer rd.Close()
	// Bind reads to the HTTP request context so canceled probes/seeks from mpv
	// unblock immediately instead of leaking goroutines and stalling piece windows.
	rd.SetContext(r.Context())
	rd.SetResponsive()
	rd.SetReadahead(32 << 20) // 32 MiB sequential sliding lookahead buffer
	w.Header().Set("Content-Type", mimeForExt(filepath.Ext(f.DisplayPath())))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(f.DisplayPath()), time.Time{}, rd)
}

// Stats returns live peer and completion telemetry for the active file.
func (s *Session) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.tor == nil {
		return Stats{}
	}
	ts := s.tor.Stats()
	st := Stats{
		Peers:   ts.ActivePeers,
		Seeders: ts.ConnectedSeeders,
	}
	if s.active != nil {
		st.Completed = s.active.BytesCompleted()
		st.Length = s.active.Length()
	}
	return st
}

// Close shuts down the HTTP server, BitTorrent client, and temporary piece cache.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	srv := s.srv
	ln := s.ln
	cl := s.client
	st := s.store
	dir := s.tempDir
	s.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	if ln != nil {
		_ = ln.Close()
	}
	if cl != nil {
		cl.Close()
	}
	if st != nil {
		_ = st.Close()
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

func prioritizeEdges(f *torrent.File) {
	if f == nil || f.Length() <= 0 {
		return
	}
	begin := f.BeginPieceIndex()
	end := f.EndPieceIndex()
	t := f.Torrent()
	pieceLen := t.Info().PieceLength
	if pieceLen <= 0 {
		pieceLen = 1 << 20
	}
	// Prioritize first ~6 MiB for immediate EBML/header/first-cluster playback.
	headPieces := max(4, int((6<<20+pieceLen-1)/pieceLen))
	for p := begin; p < end && p < begin+headPieces; p++ {
		if p < begin+2 {
			t.Piece(p).SetPriority(torrent.PiecePriorityNow)
		} else {
			t.Piece(p).SetPriority(torrent.PiecePriorityNext)
		}
	}
	// Prioritize last ~2 MiB for MKV Cues / MP4 moov atom so seeking works right away.
	tailPieces := max(1, int((2<<20+pieceLen-1)/pieceLen))
	for p := max(begin, end-tailPieces); p < end; p++ {
		t.Piece(p).SetPriority(torrent.PiecePriorityNow)
	}
}

func extraTrackers(magnet string) [][]string {
	seen := map[string]bool{}
	var tiers [][]string
	add := func(tr string) {
		tr = strings.TrimSpace(tr)
		if tr == "" || seen[tr] {
			return
		}
		seen[tr] = true
		tiers = append(tiers, []string{tr})
	}
	if magnet != "" {
		if u, err := url.Parse(magnet); err == nil {
			for _, tr := range u.Query()["tr"] {
				add(tr)
			}
		}
	}
	for _, tr := range fallbackTrackers {
		add(tr)
	}
	return tiers
}

func cleanupStaleDirs(root string, maxAge time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "s-") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

func mimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	default:
		return "video/x-matroska"
	}
}
