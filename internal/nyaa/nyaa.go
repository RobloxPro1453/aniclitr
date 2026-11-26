// Package nyaa searches Nyaa.si's public anime RSS feed without scraping
// HTML or bypassing any protection, builds standard magnet links from
// infoHashes, and optionally hands downloads to aria2c or the OS magnet handler.
package nyaa

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/netx"
)

// base is a var so tests can replay recorded RSS feeds via httptest.
var base = "https://nyaa.si"

// Standard Nyaa anime category codes.
const (
	CatAll    = "1_0" // Anime - All
	CatEng    = "1_2" // Anime - English-translated
	CatNonEng = "1_3" // Anime - Non-English-translated (Turkish, etc.)
	CatRaw    = "1_4" // Anime - Raw
)

// Standard Nyaa filter codes.
const (
	FilterAll      = "0"
	FilterNoRemake = "1"
	FilterTrusted  = "2"
)

var defaultTrackers = []string{
	"http://nyaa.tracker.wf:7777/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

// Item is one torrent result from Nyaa.si.
type Item struct {
	Title     string
	Torrent   string // .torrent URL
	PageURL   string
	InfoHash  string
	Magnet    string
	Seeders   int
	Leechers  int
	Downloads int
	Size      string
	Category  string
	Trusted   bool
	Remake    bool
	Turkish   bool
	Published time.Time
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

func isTurkishRelease(title string) bool {
	l := strings.ToLower(title)
	for _, kw := range []string{"turkish", "türkçe", "turkce", "[tr]", "(tr)", "[tr ", "altyazılı", "altyazili", "dublaj", "multi-sub", "multisub", "multi sub"} {
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
		out = append(out, Item{
			Title:     title,
			Torrent:   strings.TrimSpace(it.Link),
			PageURL:   strings.TrimSpace(it.GUID),
			InfoHash:  hash,
			Magnet:    BuildMagnet(hash, title),
			Seeders:   seed,
			Leechers:  leech,
			Downloads: dl,
			Size:      strings.TrimSpace(it.Size),
			Category:  strings.TrimSpace(it.Category),
			Trusted:   strings.EqualFold(strings.TrimSpace(it.Trusted), "Yes"),
			Remake:    strings.EqualFold(strings.TrimSpace(it.Remake), "Yes"),
			Turkish:   isTurkishRelease(title),
			Published: pub,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Seeders != out[j].Seeders {
			return out[i].Seeders > out[j].Seeders
		}
		return out[i].Downloads > out[j].Downloads
	})
	return out, nil
}

// StartAria2 launches aria2c in the background to download a magnet or .torrent into dir.
func StartAria2(dir, target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("geçersiz torrent/magnet bağlantısı")
	}
	bin, err := exec.LookPath("aria2c")
	if err != nil {
		return errors.New("aria2c bulunamadı — kurulum: winget install aria2.aria2")
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
