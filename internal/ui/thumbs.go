package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/netx"
)

type thumbEntry struct {
	url string
	at  time.Time
}

var (
	thumbMu    sync.Mutex
	thumbCache = map[string]thumbEntry{}
)

// thumbPath returns where mpv stores the last watched frame for an anime
// without touching the filesystem.
func thumbPath(animeKey string) string {
	dir := filepath.Join(config.CacheDir(), "kareler")
	return filepath.Join(dir, strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(animeKey)+".jpg")
}

// thumbPathForWrite ensures the thumbnail directory exists before mpv writes a frame.
func thumbPathForWrite(animeKey string) string {
	p := thumbPath(animeKey)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return p
}

// invalidateThumb clears the cached stat entry for animeKey (called after playback saves a new frame).
func invalidateThumb(animeKey string) {
	thumbMu.Lock()
	delete(thumbCache, animeKey)
	thumbMu.Unlock()
}

// thumbURL is the poster-cache URL of that frame ("" if none yet). Results are
// cached briefly in memory so 60 FPS views never invoke os.Stat per frame.
func thumbURL(animeKey string) string {
	now := time.Now()
	thumbMu.Lock()
	if e, ok := thumbCache[animeKey]; ok && now.Sub(e.at) < 5*time.Second {
		thumbMu.Unlock()
		return e.url
	}
	thumbMu.Unlock()

	p := thumbPath(animeKey)
	url := ""
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		url = fmt.Sprintf("file://%s?t=%d", p, st.ModTime().Unix())
	}
	thumbMu.Lock()
	if len(thumbCache) > 256 {
		clear(thumbCache)
	}
	thumbCache[animeKey] = thumbEntry{url: url, at: now}
	thumbMu.Unlock()
	return url
}

// posterBytes loads an image: local files directly, remote ones through a
// disk cache (covers rarely change; instant start, works offline).
func posterBytes(ctx context.Context, url string) ([]byte, error) {
	if strings.HasPrefix(url, "file://") {
		p := strings.TrimPrefix(url, "file://")
		if i := strings.Index(p, "?t="); i >= 0 {
			p = p[:i]
		}
		return os.ReadFile(p)
	}
	path := filepath.Join(config.CacheDir(), "kapaklar", fmt.Sprintf("%016x.img", fnv64(url)))
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return b, nil
	}
	// TMDB serves huge originals; ask for a rendition that fits sixel posters.
	u := strings.Replace(url, "/t/p/original/", "/t/p/w500/", 1)
	b, err := netx.Get(ctx, u, nil)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = config.WriteAtomic(path, b)
	return b, nil
}

// posterOnDisk reports whether posterBytes can read url back without the
// network (a local file, or the cover is in the disk cache).
func posterOnDisk(url string) bool {
	p := strings.TrimPrefix(url, "file://")
	if p != url {
		if i := strings.Index(p, "?t="); i >= 0 {
			p = p[:i]
		}
	} else {
		p = filepath.Join(config.CacheDir(), "kapaklar", fmt.Sprintf("%016x.img", fnv64(url)))
	}
	fi, err := os.Stat(p)
	return err == nil && fi.Size() > 0
}

func fnv64(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// nextLabel names what comes after this episode (for mpv's end card).
func (p *playing) nextLabel() string {
	if p.an.IsMovie() {
		return ""
	}
	for i, e := range p.eps {
		if e.Season == p.ep.Season && e.Number == p.ep.Number {
			if i+1 < len(p.eps) {
				n := p.eps[i+1]
				if n.Title != "" && !genericTitle(n.Title) {
					return fmt.Sprintf("%s. bölüm · %s", n.Num(), n.Title)
				}
				return n.Num() + ". bölüm"
			}
		}
	}
	for i, s := range p.an.Seasons {
		if s.Number == p.ep.Season && i+1 < len(p.an.Seasons) {
			return fmt.Sprintf("Sezon %d başlıyor", p.an.Seasons[i+1].Number)
		}
	}
	return ""
}
