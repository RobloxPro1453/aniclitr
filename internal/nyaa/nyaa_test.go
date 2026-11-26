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

func TestSearchNoiseExclusionHTMLFallbackFindsExactSeries(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	// Simulate Nyaa English RSS newest-75 flooded by unrelated "In Another World" shows.
	floodRSS := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:nyaa="https://nyaa.si/xmlns/nyaa"><channel><title>Nyaa</title>`
	for i := 1; i <= 8; i++ {
		title := "[SubsPlease] ReZero Starting Life in Another World S04E0" + string(rune('0'+i)) + " (1080p).mkv"
		if i > 4 {
			title = "[SubsPlease] Yandere Dark Elf From Another World E0" + string(rune('0'+i)) + " (1080p).mkv"
		}
		floodRSS += `<item>
			<title>` + title + `</title>
			<link>https://nyaa.si/download/200` + string(rune('0'+i)) + `.torrent</link>
			<guid>https://nyaa.si/view/200` + string(rune('0'+i)) + `</guid>
			<nyaa:seeders>400</nyaa:seeders><nyaa:leechers>10</nyaa:leechers><nyaa:downloads>5000</nyaa:downloads>
			<nyaa:infoHash>aaaa00000000000000000000000000000000000` + string(rune('0'+i)) + `</nyaa:infoHash>
			<nyaa:category>Anime - English-translated</nyaa:category><nyaa:size>1.2 GiB</nyaa:size>
		</item>`
	}
	floodRSS += `</channel></rss>`

	const htmlSeeders = `<html><body><table>
<tr class="success">
  <td><a href="/?c=1_2" title="Anime - English-translated"></a></td>
  <td colspan="2"><a href="/view/999001" title="[EMBER] Another (2012) (01-12 + OVA) [BDRip 1080p HEVC]">[EMBER] Another</a></td>
  <td class="text-center"><a href="/download/999001.torrent">dl</a><a href="magnet:?xt=urn:btih:deadbeef1234567890abcdef1234567890abcdef&amp;dn=Another">m</a></td>
  <td class="text-center">6.4 GiB</td>
  <td class="text-center" data-timestamp="1700000000">2023-11-14</td>
  <td class="text-center">88</td>
  <td class="text-center">4</td>
  <td class="text-center">12000</td>
</tr>
</table></body></html>`

	var htmlCalled bool
	var htmlQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "rss" {
			_, _ = w.Write([]byte(floodRSS))
			return
		}
		if r.URL.Query().Get("s") == "seeders" {
			htmlCalled = true
			htmlQuery = r.URL.Query().Get("q")
			_, _ = w.Write([]byte(htmlSeeders))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := base
	base = srv.URL
	defer func() { base = old }()

	items, err := Search(context.Background(), "another", CatEng, FilterAll)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if !htmlCalled {
		t.Fatalf("expected automatic HTML seeders fallback when RSS has <3 exact matches")
	}
	if !strings.Contains(htmlQuery, "-world") {
		t.Fatalf("expected noise term -world in refined query, got %q", htmlQuery)
	}
	if len(items) == 0 || !strings.Contains(items[0].Title, "[EMBER] Another") || !items[0].Trusted {
		t.Fatalf("expected [EMBER] Another ranked #1 ahead of 400-seed ReZero, got %+v", items[0])
	}
	// Even under SortDownloads, exact relevance tier stays on top.
	SortItems(items, SortDownloads)
	if !strings.Contains(items[0].Title, "[EMBER] Another") {
		t.Fatalf("SortDownloads should keep exact match above unrelated flood, got %q", items[0].Title)
	}
}
