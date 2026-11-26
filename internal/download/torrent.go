package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"aniclitr/internal/netx"
)

// TorrentRequest describes a Nyaa.si torrent download managed inside download.Manager
// with live progress in ctrl+d İndirilenler and automatic series folder organization.
type TorrentRequest struct {
	Title      string // full Nyaa release title
	TorrentURL string // .torrent download URL (instant metadata)
	Magnet     string // magnet fallback
	FilePath   string // optional: download only this file inside a batch torrent
	Group      string
	Resolution string
	Size       int64
	LibTitles  []string // known library titles for smart series folder matching
}

// AddTorrent queues a tracked BitTorrent download in the unified download queue.
func (m *Manager) AddTorrent(req TorrentRequest) (Job, bool) {
	parsed := ParseRelease(req.Title)
	if req.FilePath != "" {
		fp := ParseRelease(filepath.Base(req.FilePath))
		if fp.Episode > 0 {
			parsed.Season, parsed.Episode = fp.Season, fp.Episode
		}
		if fp.Resolution != "" && parsed.Resolution == "" {
			parsed.Resolution = fp.Resolution
		}
	}
	if req.Group != "" && parsed.Group == "" {
		parsed.Group = req.Group
	}
	if req.Resolution != "" && parsed.Resolution == "" {
		parsed.Resolution = req.Resolution
	}

	m.mu.Lock()
	var existingFolders []string
	if entries, err := os.ReadDir(m.dir); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				existingFolders = append(existingFolders, e.Name())
			}
		}
	}
	series := MatchSeriesFolder(parsed.Series, existingFolders, req.LibTitles)
	j := &Job{
		ID:         m.nextID,
		Source:     "nyaa",
		AnimeID:    safeName(series),
		Title:      series,
		Season:     max(1, parsed.Season),
		Episode:    parsed.Episode,
		EpTitle:    req.Title,
		Fansub:     parsed.Group,
		Host:       "Nyaa.si",
		Quality:    parsed.Resolution,
		State:      Queued,
		Tool:       "torrent",
		Total:      req.Size,
		Added:      time.Now(),
		torrentReq: &req,
	}
	if parsed.IsMovie {
		j.Kind = "Film"
	}
	m.nextID++
	m.jobs = append(m.jobs, j)
	c := *j
	m.mu.Unlock()
	m.changed(true)
	m.pump()
	return c, true
}

func (m *Manager) doTorrent(ctx context.Context, j *Job, req *TorrentRequest) error {
	if strings.TrimSpace(req.TorrentURL) == "" && strings.TrimSpace(req.Magnet) == "" {
		return errors.New("geçersiz torrent bağlantısı")
	}

	m.mu.Lock()
	rootDir := m.dir
	m.mu.Unlock()
	stageDir := filepath.Join(rootDir, ".torrent-tmp", fmt.Sprintf("job-%d", j.ID))
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	st := storage.NewFile(stageDir)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = stageDir
	cfg.DefaultStorage = st
	cfg.NoUpload = false
	cfg.Seed = false
	cfg.DisableIPv6 = true
	cfg.ListenPort = 0
	cfg.EstablishedConnsPerTorrent = 80
	cfg.HalfOpenConnsPerTorrent = 40
	cfg.Slogger = slog.New(slog.DiscardHandler)

	cl, err := torrent.NewClient(cfg)
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("torrent motoru açılamadı: %w", err)
	}
	clientClosed := false
	closeClient := func() {
		if !clientClosed {
			clientClosed = true
			cl.Close()
			_ = st.Close()
		}
	}
	defer closeClient()

	var tor *torrent.Torrent
	if u := strings.TrimSpace(req.TorrentURL); u != "" {
		if body, gerr := netx.Get(ctx, u, nil); gerr == nil && len(body) > 0 {
			if mi, lerr := metainfo.Load(bytes.NewReader(body)); lerr == nil {
				tor, err = cl.AddTorrent(mi)
			}
		}
	}
	if tor == nil && strings.TrimSpace(req.Magnet) != "" {
		tor, err = cl.AddMagnet(req.Magnet)
	}
	if err != nil || tor == nil {
		if err == nil {
			err = errors.New("torrent yüklenemedi")
		}
		return err
	}
	tor.AddTrackers(torrentFallbackTrackers(req.Magnet))

	select {
	case <-tor.GotInfo():
	case <-ctx.Done():
		return ctx.Err()
	}

	// Select target video/subtitle files to download.
	var targets []*torrent.File
	var totalBytes int64
	wantSingle := strings.TrimSpace(req.FilePath)
	for _, f := range tor.Files() {
		rel := f.DisplayPath()
		ext := strings.ToLower(filepath.Ext(rel))
		want := false
		if wantSingle != "" {
			want = rel == wantSingle || filepath.Base(rel) == filepath.Base(wantSingle)
		} else {
			want = IsVideoExt(ext) || ext == ".ass" || ext == ".srt" || ext == ".vtt"
		}
		if want {
			f.Download()
			targets = append(targets, f)
			totalBytes += f.Length()
		} else {
			f.SetPriority(torrent.PiecePriorityNone)
		}
	}
	if len(targets) == 0 {
		// Fall back to downloading everything if no recognized extensions matched.
		tor.DownloadAll()
		targets = tor.Files()
		totalBytes = tor.Length()
	}

	m.set(func() {
		j.State = Running
		j.Tool = "torrent"
		if totalBytes > 0 {
			j.Total = totalBytes
		}
	})
	m.changed(true)

	// Poll progress cleanly at 2 Hz until every selected file finishes or ctx cancels.
	tk := time.NewTicker(500 * time.Millisecond)
	defer tk.Stop()
	lastAt := time.Now()
	var lastDone int64
	var speed float64

	for {
		var done int64
		for _, f := range targets {
			done += f.BytesCompleted()
		}
		now := time.Now()
		dt := now.Sub(lastAt).Seconds()
		if dt >= 0.4 {
			speed = float64(max(int64(0), done-lastDone)) / dt
			lastAt, lastDone = now, done
		}
		var eta time.Duration
		if speed > 0 && totalBytes > done {
			eta = time.Duration(float64(totalBytes-done)/speed) * time.Second
		}
		peers := tor.Stats().ActivePeers
		m.set(func() {
			j.Done, j.Total, j.Speed, j.ETA, j.Conns = done, totalBytes, speed, eta, peers
		})
		m.changed(false)

		if totalBytes > 0 && done >= totalBytes {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tk.C:
		}
	}

	// Close client & bolt storage before moving files on Windows so all handles are released.
	closeClient()

	// Organize every downloaded video into <DownloadDir>/<CanonicalSeries>/SxxEyy [Group] [Res].mkv.
	var firstVideo string
	var firstParsed ParsedRelease
	var organized int
	for _, f := range targets {
		src := filepath.Join(stageDir, filepath.FromSlash(f.Path()))
		if !fileExists(src) || !IsVideoExt(filepath.Ext(src)) {
			continue
		}
		dst, parsed, oerr := OrganizeFile(rootDir, src, req.Title, req.LibTitles)
		if oerr != nil {
			continue
		}
		organized++
		if firstVideo == "" {
			firstVideo = dst
			firstParsed = parsed
		}
	}
	if firstVideo == "" {
		return errors.New("indirilen torrent içinde video dosyası bulunamadı")
	}

	m.set(func() {
		j.Path = firstVideo
		if firstParsed.Series != "" {
			j.Title = firstParsed.Series
		}
		if firstParsed.Episode > 0 {
			j.Season, j.Episode = firstParsed.Season, firstParsed.Episode
		}
		if firstParsed.Group != "" && j.Fansub == "" {
			j.Fansub = firstParsed.Group
		}
		if firstParsed.Resolution != "" && j.Quality == "" {
			j.Quality = firstParsed.Resolution
		}
	})
	// If a batch torrent produced multiple episodes, index all of them in the offline shelf.
	if organized > 1 {
		go m.ScanDisk()
	}
	return nil
}

func torrentFallbackTrackers(magnet string) [][]string {
	defaults := []string{
		"http://nyaa.tracker.wf:7777/announce",
		"udp://tracker.opentrackr.org:1337/announce",
		"udp://open.stealth.si:80/announce",
		"udp://exodus.desync.com:6969/announce",
		"udp://tracker.torrent.eu.org:451/announce",
		"udp://tracker.openbittorrent.com:6969/announce",
	}
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
	for _, tr := range defaults {
		add(tr)
	}
	return tiers
}
