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
	"os"
	"path/filepath"
	"sort"
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

	tempDir := filepath.Join(config.CacheDir(), "tstream", fmt.Sprintf("s-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, err
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = tempDir
	cfg.DefaultStorage = storage.NewFile(tempDir)
	cfg.NoUpload = true
	cfg.Seed = false
	cfg.ListenPort = 0
	cfg.Slogger = slog.New(slog.DiscardHandler)

	cl, err := torrent.NewClient(cfg)
	if err != nil {
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
		_ = os.RemoveAll(tempDir)
		if err == nil {
			err = errors.New("torrent metaverisi alınamadı")
		}
		return nil, err
	}

	select {
	case <-tor.GotInfo():
	case <-ctx.Done():
		cl.Close()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("torrent bilgisi zaman aşımına uğradı (seed/DHT yanıt vermedi)")
	}

	// Don't download anything until the user picks a file to stream.
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
		_ = os.RemoveAll(tempDir)
		return nil, errors.New("bu torrent içinde oynatılabilir video dosyası (.mkv/.mp4) bulunamadı")
	}

	return &Session{
		client:  cl,
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
// directly into mpv with high-priority header/footer piece prefetching.
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
	// Deprioritize other files so full swarm bandwidth goes to the watched episode.
	for _, f := range s.tor.Files() {
		if f == vf.file {
			f.SetPriority(torrent.PiecePriorityNormal)
		} else {
			f.SetPriority(torrent.PiecePriorityNone)
		}
	}
	// Prioritize first ~4 MiB and last ~1 MiB so MKV/MP4 cues & headers load fast.
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
		s.srv = &http.Server{Handler: mux}
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
	f := s.active
	closed := s.closed
	s.mu.Unlock()
	if closed || f == nil {
		http.Error(w, "stream closed", http.StatusGone)
		return
	}
	rd := f.NewReader()
	defer rd.Close()
	rd.SetResponsive()
	rd.SetReadahead(24 << 20) // 24 MiB sliding lookahead buffer
	w.Header().Set("Content-Type", mimeForExt(filepath.Ext(f.DisplayPath())))
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
	// Prioritize opening pieces for immediate playback start.
	for p := begin; p < end && p < begin+6; p++ {
		if p == begin || p == begin+1 {
			t.Piece(p).SetPriority(torrent.PiecePriorityNow)
		} else {
			t.Piece(p).SetPriority(torrent.PiecePriorityNext)
		}
	}
	// Prioritize tail piece for MKV cues / MP4 moov atom.
	if end > begin {
		t.Piece(end - 1).SetPriority(torrent.PiecePriorityNow)
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
