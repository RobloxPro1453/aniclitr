package nyaa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:nyaa="https://nyaa.si/xmlns/nyaa">
  <channel>
    <title>Nyaa - RSS</title>
    <item>
      <title>[SubsPlease] Sousou no Frieren - 03 (1080p) [Multi-Sub] [x265].mkv</title>
      <link>https://nyaa.si/download/1001.torrent</link>
      <guid isPermaLink="true">https://nyaa.si/view/1001</guid>
      <pubDate>Fri, 20 Nov 2026 18:00:00 -0000</pubDate>
      <nyaa:seeders>42</nyaa:seeders>
      <nyaa:leechers>5</nyaa:leechers>
      <nyaa:downloads>300</nyaa:downloads>
      <nyaa:infoHash>abcdef1234567890abcdef1234567890abcdef12</nyaa:infoHash>
      <nyaa:category>Anime - English-translated</nyaa:category>
      <nyaa:size>1.4 GiB</nyaa:size>
      <nyaa:trusted>Yes</nyaa:trusted>
      <nyaa:remake>No</nyaa:remake>
    </item>
    <item>
      <title>[HolySubs] Sousou no Frieren - 01-28 [1080p][TR Altyazılı]</title>
      <link>https://nyaa.si/download/1002.torrent</link>
      <guid isPermaLink="true">https://nyaa.si/view/1002</guid>
      <pubDate>Thu, 19 Nov 2026 15:00:00 -0000</pubDate>
      <nyaa:seeders>180</nyaa:seeders>
      <nyaa:leechers>14</nyaa:leechers>
      <nyaa:downloads>2100</nyaa:downloads>
      <nyaa:infoHash>1234567890abcdef1234567890abcdef12345678</nyaa:infoHash>
      <nyaa:category>Anime - Non-English-translated</nyaa:category>
      <nyaa:size>32.5 GiB</nyaa:size>
      <nyaa:trusted>No</nyaa:trusted>
      <nyaa:remake>No</nyaa:remake>
    </item>
    <item>
      <title>[ReinForce] Sousou no Frieren (BDRip 1920x1080 x264 FLAC) [RAW]</title>
      <link>https://nyaa.si/download/1003.torrent</link>
      <guid isPermaLink="true">https://nyaa.si/view/1003</guid>
      <pubDate>Sat, 21 Nov 2026 10:00:00 -0000</pubDate>
      <nyaa:seeders>65</nyaa:seeders>
      <nyaa:leechers>3</nyaa:leechers>
      <nyaa:downloads>540</nyaa:downloads>
      <nyaa:infoHash>fedcba0987654321fedcba0987654321fedcba09</nyaa:infoHash>
      <nyaa:category>Anime - Raw</nyaa:category>
      <nyaa:size>48.2 GiB</nyaa:size>
      <nyaa:trusted>Yes</nyaa:trusted>
      <nyaa:remake>No</nyaa:remake>
    </item>
  </channel>
</rss>`

func TestSearchParsesRSSMetadataRawAndSorting(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "rss" || r.URL.Query().Get("q") != "frieren" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()
	old := base
	base = srv.URL
	defer func() { base = old }()

	items, err := Search(context.Background(), "frieren", CatAll, FilterAll)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Default sort is seeders descending: 180 -> 65 -> 42.
	if items[0].Seeders != 180 || items[0].Group != "HolySubs" || items[0].Resolution != "1080p" || !items[0].Turkish || !items[0].Batch {
		t.Fatalf("first item metadata mismatch: %+v", items[0])
	}
	if !strings.HasPrefix(items[0].Magnet, "magnet:?xt=urn:btih:1234567890abcdef") {
		t.Fatalf("bad magnet: %q", items[0].Magnet)
	}
	// Raw release detection (category + [RAW] + 1920x1080 + x264).
	if !items[1].Raw || items[1].Group != "ReinForce" || items[1].Resolution != "1080p" || items[1].Codec != "H.264" || items[1].SizeBytes <= 40<<30 {
		t.Fatalf("raw item metadata mismatch: %+v", items[1])
	}
	if !items[2].Trusted || items[2].Codec != "HEVC" {
		t.Fatalf("third item metadata mismatch: %+v", items[2])
	}

	// SortNewest puts Sat 21 Nov (ReinForce) first.
	SortItems(items, SortNewest)
	if items[0].Group != "ReinForce" {
		t.Fatalf("SortNewest expected ReinForce first, got %q", items[0].Group)
	}
	// SortSizeAsc puts 1.4 GiB (SubsPlease) first.
	SortItems(items, SortSizeAsc)
	if items[0].Group != "SubsPlease" {
		t.Fatalf("SortSizeAsc expected SubsPlease first, got %q", items[0].Group)
	}
}
