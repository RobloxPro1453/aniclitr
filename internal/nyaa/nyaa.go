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
	"html"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/match"
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
	return match.CleanDisplayTitle(title)
}

// RelevanceScore tiers how closely a torrent's series title matches query so
// searching "another" ranks "Another" ahead of "Re:ZERO ... in Another World"
// across EVERY category (All, Turkish, English, Raw, Trusted).
func RelevanceScore(query, title string) int {
	return match.RankTitle(query, title)
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
	// If RSS (newest-75) was flooded by unrelated high-volume series (e.g.
	// "In Another World" hiding 2012's "Another" in English/All tabs), run one
	// automatic TF-DF noise-excluded seeders search and merge without losing any results.
	if exactCount(out) < 3 && !strings.Contains(query, "-") && len(strings.Fields(query)) <= 3 {
		if noise := extractNoiseTerms(query, out); len(noise) > 0 {
			refined := query
			for _, w := range noise {
				refined += " -" + w
			}
			cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			extra := searchHTMLSeeders(cctx, refined, query, cat, filter)
			cancel()
			if len(extra) > 0 {
				out = mergeDedup(out, extra)
			}
		}
	}
	SortItems(out, SortSeeders)
	return out, nil
}

func exactCount(items []Item) int {
	n := 0
	for _, it := range items {
		if it.Relevance >= 80 {
			n++
		}
	}
	return n
}

var ignoreNoiseWords = map[string]bool{
	"1080p": true, "720p": true, "2160p": true, "480p": true, "hevc": true, "x264": true, "x265": true,
	"h264": true, "h265": true, "flac": true, "10bit": true, "8bit": true, "dual": true, "audio": true,
	"multi": true, "subs": true, "subbed": true, "dubbed": true, "english": true, "japanese": true,
	"batch": true, "complete": true, "season": true, "bdrip": true, "bluray": true, "webrip": true,
	"amzn": true, "varyg": true, "toonshub": true, "subsplease": true, "judas": true, "ember": true,
	"opus": true, "aac2": true, "ddp2": true, "with": true, "from": true, "that": true, "this": true,
	"into": true, "under": true, "over": true, "kara": true, "no": true, "wa": true, "ga": true,
}

// extractNoiseTerms identifies up to 3 dominant non-query words flooding the
// initial RSS results so a follow-up Nyaa search can exclude them cleanly.
func extractNoiseTerms(query string, items []Item) []string {
	if len(items) < 6 {
		return nil
	}
	qSet := map[string]bool{}
	for _, w := range strings.Fields(match.Fold(query)) {
		qSet[w] = true
	}
	docFreq := map[string]int{}
	nonExact := 0
	for _, it := range items {
		if it.Relevance >= 80 {
			continue
		}
		nonExact++
		seen := map[string]bool{}
		clean := strings.NewReplacer(".", " ", "_", " ", "-", " ", ":", " ", "(", " ", ")", " ", "[", " ", "]", " ", "|", " ", "/", " ").Replace(it.Title)
		for _, raw := range strings.Fields(match.Fold(clean)) {
			w := strings.Trim(raw, "'\"!?.,~+")
			if len(w) < 4 || qSet[w] || ignoreNoiseWords[w] || seen[w] {
				continue
			}
			// Skip pure numbers or episode codes (s01e01, 2026)
			if w[0] >= '0' && w[0] <= '9' {
				continue
			}
			if len(w) >= 3 && w[0] == 's' && w[1] >= '0' && w[1] <= '9' {
				continue
			}
			seen[w] = true
			docFreq[w]++
		}
	}
	if nonExact < 5 {
		return nil
	}
	type kv struct {
		w string
		c int
	}
	var cands []kv
	minDocs := max(3, nonExact/5)
	for w, c := range docFreq {
		if c >= minDocs {
			cands = append(cands, kv{w, c})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].c != cands[j].c {
			return cands[i].c > cands[j].c
		}
		return cands[i].w < cands[j].w
	})
	var out []string
	for i := 0; i < len(cands) && i < 3; i++ {
		out = append(out, cands[i].w)
	}
	return out
}

var (
	reHTMLRow      = regexp.MustCompile(`(?s)<tr class="([^"]*)">(.*?)</tr>`)
	reHTMLView     = regexp.MustCompile(`<a href="(/view/\d+)"[^>]*title="([^"]+)"`)
	reHTMLTorrent  = regexp.MustCompile(`href="(/download/\d+\.torrent)"`)
	reHTMLMagnet   = regexp.MustCompile(`href="(magnet:\?[^"]+)"`)
	reHTMLHash     = regexp.MustCompile(`btih:([A-Fa-f0-9]{32,40})`)
	reHTMLTD       = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	reHTMLStripTag = regexp.MustCompile(`<[^>]+>`)
	reHTMLTs       = regexp.MustCompile(`data-timestamp="(\d+)"`)
	reHTMLCat      = regexp.MustCompile(`<a href="/\?c=[^"]+" title="([^"]+)"`)
)

func searchHTMLSeeders(ctx context.Context, refinedQuery, origQuery, cat, filter string) []Item {
	u := fmt.Sprintf("%s/?f=%s&c=%s&q=%s&s=seeders&o=desc",
		base, url.QueryEscape(filter), url.QueryEscape(cat), url.QueryEscape(refinedQuery))
	body, err := netx.Get(ctx, u, map[string]string{"Accept": "text/html,application/xhtml+xml"})
	if err != nil || len(body) == 0 {
		return nil
	}
	text := string(body)
	rows := reHTMLRow.FindAllStringSubmatch(text, 60)
	var out []Item
	for _, rm := range rows {
		rowCls, rowHTML := rm[1], rm[2]
		vm := reHTMLView.FindStringSubmatch(rowHTML)
		if vm == nil {
			continue
		}
		pageURL := base + vm[1]
		title := strings.TrimSpace(html.UnescapeString(vm[2]))
		if title == "" {
			continue
		}
		torURL := ""
		if tm := reHTMLTorrent.FindStringSubmatch(rowHTML); tm != nil {
			torURL = base + tm[1]
		}
		mag := ""
		hash := ""
		if mm := reHTMLMagnet.FindStringSubmatch(rowHTML); mm != nil {
			mag = html.UnescapeString(mm[1])
			if hm := reHTMLHash.FindStringSubmatch(mag); hm != nil {
				hash = strings.ToLower(hm[1])
			}
		}
		catStr := ""
		if cm := reHTMLCat.FindStringSubmatch(rowHTML); cm != nil {
			catStr = html.UnescapeString(cm[1])
		}
		tds := reHTMLTD.FindAllStringSubmatch(rowHTML, -1)
		sizeStr := ""
		var pub time.Time
		var seed, leech, dl int
		if len(tds) >= 8 {
			sizeStr = strings.TrimSpace(reHTMLStripTag.ReplaceAllString(tds[3][1], ""))
			if tsm := reHTMLTs.FindStringSubmatch(tds[4][0]); tsm != nil {
				if sec, err := strconv.ParseInt(tsm[1], 10, 64); err == nil && sec > 0 {
					pub = time.Unix(sec, 0)
				}
			}
			seed, _ = strconv.Atoi(strings.TrimSpace(reHTMLStripTag.ReplaceAllString(tds[5][1], "")))
			leech, _ = strconv.Atoi(strings.TrimSpace(reHTMLStripTag.ReplaceAllString(tds[6][1], "")))
			dl, _ = strconv.Atoi(strings.TrimSpace(reHTMLStripTag.ReplaceAllString(tds[7][1], "")))
		}
		if mag == "" && hash != "" {
			mag = BuildMagnet(hash, title)
		}
		out = append(out, Item{
			Title:      title,
			Torrent:    torURL,
			PageURL:    pageURL,
			InfoHash:   hash,
			Magnet:     mag,
			Seeders:    seed,
			Leechers:   leech,
			Downloads:  dl,
			Size:       sizeStr,
			SizeBytes:  parseSizeBytes(sizeStr),
			Category:   catStr,
			Group:      parseGroup(title),
			Resolution: parseResolution(title),
			Codec:      parseCodec(title),
			Trusted:    strings.Contains(rowCls, "success"),
			Remake:     strings.Contains(rowCls, "danger"),
			Turkish:    isTurkishRelease(title),
			Raw:        cat == CatRaw || isRawRelease(title, catStr),
			Batch:      reBatch.MatchString(title),
			Relevance:  RelevanceScore(origQuery, title),
			Published:  pub,
		})
	}
	return out
}

func mergeDedup(a, b []Item) []Item {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]Item, 0, len(a)+len(b))
	keyOf := func(it Item) string {
		if it.InfoHash != "" {
			return strings.ToLower(it.InfoHash)
		}
		if it.PageURL != "" {
			return it.PageURL
		}
		return strings.ToLower(it.Title)
	}
	for _, list := range [][]Item{a, b} {
		for _, it := range list {
			k := keyOf(it)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, it)
		}
	}
	return out
}

func relTier(r int) int {
	switch {
	case r >= 80:
		return 2
	case r >= 50:
		return 1
	default:
		return 0
	}
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
			ta, tb := relTier(a.Relevance), relTier(b.Relevance)
			if ta != tb {
				return ta > tb
			}
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
