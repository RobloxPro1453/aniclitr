// Package nyaa searches Nyaa.si's public anime RSS feed (All, English,
// Non-English/Turkish, Raw) without bypassing any protection, extracts release
// metadata (group, resolution, codec, raw/batch/Turkish flags, byte size),
// builds standard magnet links, and hands downloads to aria2c, webtorrent,
// clipboard, or the OS magnet handler.
package nyaa

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/netx"
	"aniclitr/internal/tools"
)

// base is a var so tests can replay recorded RSS feeds via httptest.
var base = "https://nyaa.si"

// Standard Nyaa anime category codes.
const (
	CatAll    = "1_0" // Anime - All
	CatEng    = "1_2" // Anime - English-translated
	CatNonEng = "1_3" // Anime - Non-English-translated (Turkish, etc.)
	CatRaw    = "1_4" // Anime - Raw (unsubtitled Japanese Blu-ray / TV)
)

// Standard Nyaa filter codes.
const (
	FilterAll      = "0"
	FilterNoRemake = "1"
	FilterTrusted  = "2"
)

// SortMode controls how search results are ordered.
type SortMode int

const (
	SortSeeders SortMode = iota
	SortNewest
	SortDownloads
	SortSizeDesc
	SortSizeAsc
)

// Label returns a Turkish display name for the sort mode.
func (m SortMode) Label() string {
	switch m {
	case SortNewest:
		return "en yeni"
	case SortDownloads:
		return "en çok indirilen"
	case SortSizeDesc:
		return "boyut ↓"
	case SortSizeAsc:
		return "boyut ↑"
	default:
		return "seed ↓"
	}
}

var defaultTrackers = []string{
	"http://nyaa.tracker.wf:7777/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

// Item is one torrent result from Nyaa.si with parsed release metadata.
type Item struct {
	Title      string
	Torrent    string // .torrent URL
	PageURL    string
	InfoHash   string
	Magnet     string
	Seeders    int
	Leechers   int
	Downloads  int
	Size       string
	SizeBytes  int64
	Category   string
	Group      string // e.g. "SubsPlease", "Erai-raws", "HolySubs"
	Resolution string // "2160p", "1080p", "720p", "480p", ""
	Codec      string // "HEVC", "AV1", "H.264", ""
	Trusted    bool
	Remake     bool
	Turkish    bool
	Raw        bool
	Batch      bool
	Relevance  int // query title relevance tier (100=exact, 80=prefix, 55=word, 20=phrase)
	Published  time.Time
}

// BuildMagnet builds a standard BitTorrent magnet URI for infoHash and display name.
func BuildMagnet(infoHash, title string) string {
	h := strings.TrimSpace(infoHash)
	if h == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(url.QueryEscape(h))
	if title != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(title))
	}
	for _, tr := range defaultTrackers {
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tr))
	}
	return b.String()
}

type rssFeed struct {
	Channel struct {
		Items []struct {
			Title     string `xml:"title"`
			Link      string `xml:"link"`
			GUID      string `xml:"guid"`
			PubDate   string `xml:"pubDate"`
			Seeders   string `xml:"seeders"`
			Leechers  string `xml:"leechers"`
			Downloads string `xml:"downloads"`
			InfoHash  string `xml:"infoHash"`
			Size      string `xml:"size"`
			Category  string `xml:"category"`
			Trusted   string `xml:"trusted"`
			Remake    string `xml:"remake"`
		} `xml:"item"`
	} `xml:"channel"`
}

var (
	reGroup    = regexp.MustCompile(`^\[([^\[\]]{1,28})\]`)
	reSize     = regexp.MustCompile(`(?i)^\s*([\d.]+)\s*(B|KiB|KB|MiB|MB|GiB|GB|TiB|TB)\s*$`)
	reBatch    = regexp.MustCompile(`(?i)(batch|complete|\b0?1\s*[-~]\s*\d{2}\b|season\s*\d|s\d{2}\b|bd\s*box)`)
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\([^\)]*\)`)
	reEpTail   = regexp.MustCompile(`(?i)(\s+-\s+\d.*|\bs\d{1,2}e\d{1,3}.*|\b(1080p|720p|2160p|480p|bdrip|web-dl|webrip|hevc|x264|x265|flac|10bit)\b.*)`)
)

// CleanSeriesName extracts the core anime title from a torrent release string
// (e.g. "[Shiniori-Raws] Another (BD 1920x1080 x265)" -> "another").
func CleanSeriesName(title string) string {
	s := reBrackets.ReplaceAllString(title, " ")
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = reEpTail.ReplaceAllString(s, "")
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}

// RelevanceScore tiers how closely a torrent's series title matches query so
// searching "another" ranks "Another" ahead of "Re:ZERO ... in Another World".
func RelevanceScore(query, title string) int {
	q := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(query)), " "))
	if q == "" {
		return 50
	}
	clean := CleanSeriesName(title)
	switch {
	case clean == q:
		return 100
	case strings.HasPrefix(clean, q+" ") || strings.HasPrefix(clean, q+":") || strings.HasPrefix(clean, q+"-"):
		return 80
	case len(clean) <= len(q)+10 && strings.Contains(clean, q):
		return 65
	case strings.Contains(" "+clean+" ", " "+q+" "):
		// Penalize long unrelated subtitles like "Starting Life in Another World".
		if len(strings.Fields(clean)) >= len(strings.Fields(q))+3 {
			return 20
		}
		return 50
	default:
		return 10
	}
}

func parseGroup(title string) string {
	if m := reGroup.FindStringSubmatch(strings.TrimSpace(title)); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func parseResolution(title string) string {
	l := strings.ToLower(title)
	switch {
	case strings.Contains(l, "2160p") || strings.Contains(l, "3840x2160") || strings.Contains(l, " 4k ") || strings.Contains(l, "[4k]") || strings.Contains(l, "(4k)"):
		return "2160p"
	case strings.Contains(l, "1080p") || strings.Contains(l, "1920x1080"):
		return "1080p"
	case strings.Contains(l, "720p") || strings.Contains(l, "1280x720"):
		return "720p"
	case strings.Contains(l, "480p") || strings.Contains(l, "576p"):
		return "480p"
	}
	return ""
}

func parseCodec(title string) string {
	l := strings.ToLower(title)
	switch {
	case strings.Contains(l, "av1"):
		return "AV1"
	case strings.Contains(l, "hevc") || strings.Contains(l, "x265") || strings.Contains(l, "h265") || strings.Contains(l, "h.265"):
		return "HEVC"
	case strings.Contains(l, "x264") || strings.Contains(l, "h264") || strings.Contains(l, "h.264") || strings.Contains(l, "avc"):
		return "H.264"
	}
	return ""
}

func parseSizeBytes(s string) int64 {
	m := reSize.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "kib", "kb":
		return int64(v * 1024)
	case "mib", "mb":
		return int64(v * 1024 * 1024)
	case "gib", "gb":
		return int64(v * 1024 * 1024 * 1024)
	case "tib", "tb":
		return int64(v * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(v)
	}
}

func isTurkishRelease(title string) bool {
	l := strings.ToLower(title)
	for _, kw := range []string{"turkish", "türkçe", "turkce", "[tr]", "(tr)", "[tr ", "altyazılı", "altyazili", "dublaj", "multi-sub", "multisub", "multi sub"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

func isRawRelease(title, cat string) bool {
	if strings.Contains(strings.ToLower(cat), "raw") {
		return true
	}
	l := strings.ToLower(title)
	for _, kw := range []string{"[raw]", "(raw)", "bdmv", "raws]", "raw-"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

// Search queries Nyaa.si's public RSS feed and returns results ordered by seeders descending.
func Search(ctx context.Context, query, cat, filter string) ([]Item, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if cat == "" {
		cat = CatAll
	}
	if filter == "" {
		filter = FilterAll
	}
	u := fmt.Sprintf("%s/?page=rss&c=%s&f=%s&q=%s",
		base, url.QueryEscape(cat), url.QueryEscape(filter), url.QueryEscape(query))
	body, err := netx.Get(ctx, u, map[string]string{"Accept": "application/rss+xml, application/xml"})
	if err != nil {
		return nil, err
	}
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("nyaa RSS ayrıştırılamadı: %w", err)
	}
	out := make([]Item, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		seed, _ := strconv.Atoi(strings.TrimSpace(it.Seeders))
		leech, _ := strconv.Atoi(strings.TrimSpace(it.Leechers))
		dl, _ := strconv.Atoi(strings.TrimSpace(it.Downloads))
		pub, _ := time.Parse(time.RFC1123Z, strings.TrimSpace(it.PubDate))
		hash := strings.TrimSpace(it.InfoHash)
		sizeStr := strings.TrimSpace(it.Size)
		catStr := strings.TrimSpace(it.Category)
		out = append(out, Item{
			Title:      title,
			Torrent:    strings.TrimSpace(it.Link),
			PageURL:    strings.TrimSpace(it.GUID),
			InfoHash:   hash,
			Magnet:     BuildMagnet(hash, title),
			Seeders:    seed,
			Leechers:   leech,
			Downloads:  dl,
			Size:       sizeStr,
			SizeBytes:  parseSizeBytes(sizeStr),
			Category:   catStr,
			Group:      parseGroup(title),
			Resolution: parseResolution(title),
			Codec:      parseCodec(title),
			Trusted:    strings.EqualFold(strings.TrimSpace(it.Trusted), "Yes"),
			Remake:     strings.EqualFold(strings.TrimSpace(it.Remake), "Yes"),
			Turkish:    isTurkishRelease(title),
			Raw:        cat == CatRaw || isRawRelease(title, catStr),
			Batch:      reBatch.MatchString(title),
			Relevance:  RelevanceScore(query, title),
			Published:  pub,
		})
	}
	SortItems(out, SortSeeders)
	return out, nil
}

// SortItems sorts a slice of Nyaa items in place according to mode.
// Under the default SortSeeders mode, title relevance tier wins first so exact
// matches aren't buried under unrelated high-seed series.
func SortItems(items []Item, mode SortMode) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch mode {
		case SortSeeders:
			if a.Relevance != b.Relevance {
				return a.Relevance > b.Relevance
			}
		case SortNewest:
			if !a.Published.Equal(b.Published) {
				return a.Published.After(b.Published)
			}
		case SortDownloads:
			if a.Downloads != b.Downloads {
				return a.Downloads > b.Downloads
			}
		case SortSizeDesc:
			if a.SizeBytes != b.SizeBytes {
				return a.SizeBytes > b.SizeBytes
			}
		case SortSizeAsc:
			if a.SizeBytes != b.SizeBytes {
				return a.SizeBytes < b.SizeBytes
			}
		}
		if a.Seeders != b.Seeders {
			return a.Seeders > b.Seeders
		}
		return a.Downloads > b.Downloads
	})
}

// HasAria2 reports whether aria2c is available in aniclitr's tools dir or PATH.
func HasAria2() bool {
	_, err := tools.LookPath("aria2c")
	return err == nil
}

// StartAria2 launches aria2c in the background to download a magnet or .torrent into dir.
func StartAria2(dir, target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("geçersiz torrent/magnet bağlantısı")
	}
	bin, err := tools.LookPath("aria2c")
	if err != nil {
		return errors.New("aria2c bulunamadı — kurulum: aniclitr kur veya winget install aria2.aria2")
	}
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	args := []string{"--seed-time=0", "--summary-interval=0"}
	if dir != "" {
		args = append(args, "-d", dir)
	}
	args = append(args, target)
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Start()
}

// HasWebTorrent reports whether webtorrent-cli is installed on PATH.
func HasWebTorrent() bool {
	_, err := exec.LookPath("webtorrent")
	return err == nil
}

// StreamWebTorrent starts `webtorrent <magnet> --mpv` in the background.
func StreamWebTorrent(magnet string) error {
	bin, err := exec.LookPath("webtorrent")
	if err != nil {
		return errors.New("webtorrent bulunamadı")
	}
	cmd := exec.Command(bin, magnet, "--mpv")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Start()
}

// CopyClipboard copies text to the OS clipboard using native system utilities.
func CopyClipboard(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("kopyalanacak metin boş")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		switch {
		case hasBin("wl-copy"):
			cmd = exec.Command("wl-copy")
		case hasBin("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard")
		case hasBin("xsel"):
			cmd = exec.Command("xsel", "--clipboard", "--input")
		default:
			return errors.New("pano aracı bulunamadı (wl-copy / xclip / xsel)")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
