package media

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/netx"
)

// CinemaAdvice is an OPTIONAL, non-forcing quality recommendation derived from
// the real decoded video height and bitrate. Original/native playback always
// remains the safe default.
type CinemaAdvice struct {
	SuggestUpscale string // "" (keep original), "1080p", "2k"
	SuggestAnime4K string // "" (keep original), "hızlı", "cam"
	Reason         string
	OptionalNote   string
}

// AnalyzeStream inspects real resolution & bitrate and returns a gentle,
// non-forcing suggestion. High-bitrate 1080p+ sources recommend staying on
// untouched original quality to avoid shader artifacts.
func AnalyzeStream(height int, bitrateBps int64) CinemaAdvice {
	kbps := float64(bitrateBps) / 1000.0
	switch {
	case height >= 1080 && (bitrateBps == 0 || kbps >= 2200):
		return CinemaAdvice{
			Reason:       fmt.Sprintf("%dp yüksek bit hızlı kaynak — orijinal görüntü kalitesi önerilir", height),
			OptionalNote: "m menüsü → Çözünürlük",
		}
	case height >= 1080 && kbps > 0 && kbps < 1200:
		return CinemaAdvice{
			SuggestAnime4K: "hızlı",
			Reason:         fmt.Sprintf("%dp düşük bit hızı (%.0f kbps) — Anime4K Hızlı profili sıkıştırma izlerini azaltır", height, kbps),
			OptionalNote:   "m menüsü → Kalite ve Akıcılık",
		}
	case height > 0 && height <= 720:
		return CinemaAdvice{
			SuggestUpscale: "1080p",
			SuggestAnime4K: "hızlı",
			Reason:         fmt.Sprintf("%dp kaynak — AMD FSR 1080p ölçekleme profili uygulanabilir", height),
			OptionalNote:   "m menüsü → Çözünürlük",
		}
	default:
		return CinemaAdvice{
			Reason:       "Orijinal görüntü kalitesi etkin",
			OptionalNote: "Görüntü profilleri için m menüsü",
		}
	}
}

// ThemeSong describes an Opening, Ending, or Insert theme from open keyless
// AnimeThemes data.
type ThemeSong struct {
	Slug     string `json:"slug"`     // "OP1", "ED2"
	Title    string `json:"title"`    // song name
	Artists  string `json:"artists"`  // artist(s)
	Episodes string `json:"episodes"` // e.g. "1-16"
}

// FetchThemes retrieves Opening/Ending/Insert themes from the open, keyless
// AnimeThemes API (api.animethemes.moe) with a 7-day local disk cache.
func FetchThemes(ctx context.Context, malID int, title string) ([]ThemeSong, error) {
	if malID <= 0 && strings.TrimSpace(title) == "" {
		return nil, nil
	}
	cacheKey := fmt.Sprintf("ost-%d.json", malID)
	if malID <= 0 {
		cacheKey = "ost-" + sanitizeFilename(strings.ToLower(title)) + ".json"
	}
	cachePath := filepath.Join(config.CacheDir(), "ost", cacheKey)
	if st, err := os.Stat(cachePath); err == nil && time.Since(st.ModTime()) < 7*24*time.Hour {
		if b, err := os.ReadFile(cachePath); err == nil {
			var cached []ThemeSong
			if json.Unmarshal(b, &cached) == nil {
				return cached, nil
			}
		}
	}

	var apiURL string
	if malID > 0 {
		apiURL = fmt.Sprintf("https://api.animethemes.moe/anime?filter[has]=resources&filter[site]=MyAnimeList&filter[external_id]=%d&include=animethemes.song.artists,animethemes.animethemeentries", malID)
	} else {
		apiURL = fmt.Sprintf("https://api.animethemes.moe/anime?q=%s&page[size]=1&include=animethemes.song.artists,animethemes.animethemeentries", url.QueryEscape(title))
	}

	var resp struct {
		Anime []struct {
			Themes []struct {
				Slug string `json:"slug"`
				Song struct {
					Title   string `json:"title"`
					Artists []struct {
						Name string `json:"name"`
					} `json:"artists"`
				} `json:"song"`
				Entries []struct {
					Episodes string `json:"episodes"`
				} `json:"animethemeentries"`
			} `json:"animethemes"`
		} `json:"anime"`
	}
	if err := netx.GetJSON(ctx, apiURL, nil, &resp); err != nil {
		return nil, err
	}
	var out []ThemeSong
	if len(resp.Anime) > 0 {
		for _, th := range resp.Anime[0].Themes {
			if th.Song.Title == "" {
				continue
			}
			var arts []string
			for _, a := range th.Song.Artists {
				if a.Name != "" {
					arts = append(arts, a.Name)
				}
			}
			eps := ""
			if len(th.Entries) > 0 {
				eps = th.Entries[0].Episodes
			}
			out = append(out, ThemeSong{
				Slug:     th.Slug,
				Title:    th.Song.Title,
				Artists:  strings.Join(arts, ", "),
				Episodes: eps,
			})
		}
	}
	if len(out) > 0 {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		if b, err := json.Marshal(out); err == nil {
			_ = config.WriteAtomic(cachePath, b)
		}
	}
	return out, nil
}
