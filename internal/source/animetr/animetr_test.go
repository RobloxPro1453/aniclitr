package animetr

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/source"
)

// Trimmed from a real series page: own episodes plus a next-season link
// that must not leak into the list.
const seriesPage = `<h1 class="mb-3">
Sousou no Frieren
</h1>
<script type="application/ld+json">{"sameAs":["https://myanimelist.net/anime/52991"]}</script>
<a href="/izle/sousou-no-frieren/bolum-2" class="ep-item__link episode-link"
   data-episode-id="15911" >
    <span class="ep-item__num">2</span>
    <span class="ep-item__body">
        <span class="ep-item__title">2. Bölüm</span>
</a>
<a href="/izle/sousou-no-frieren/bolum-1" class="ep-item__link episode-link"
   data-episode-id="15910" >
    <span class="ep-item__num">1</span>
    <span class="ep-item__body">
        <span class="ep-item__title">Yolculuğun Sonu</span>
</a>
<a href="/izle/sousou-no-frieren-2nd-season/bolum-1" class="ep-item__link episode-link">
    <span class="ep-item__num">1</span><span class="ep-item__title">x</span>
</a>`

const episodePage = `<script type="application/json" id="epspage-data">{"episodeId":1,"fansubs":[
{"name":"Asyaanimeleri","players":[
 {"label":"Player Toplu","embed_url":"https://asyaanimeleri.pw/video/abc","provider":"Vip"},
 {"label":"Player Toplu","embed_url":"https://drive.google.com/file/d/X/preview","provider":"Google Drive"},
 {"label":"Player Toplu","embed_url":"https://drive.google.com/file/d/Y/preview","provider":"Google Drive"},
 {"label":"Player Toplu","embed_url":"https://ok.ru/videoembed/1","provider":"OK.ru"},
 {"label":"Player Toplu","embed_url":"https://azelvid.com/player/index.php?data=z","provider":"AnimeTR VIP"}]},
{"name":"Başka &amp; Fansub","players":[
 {"label":"","embed_url":"","original_url":"https://vidmoly.biz/embed-q.html","provider":""}]}]}</script>`

func TestParseEpisodes(t *testing.T) {
	eps := parseEpisodes(seriesPage, "sousou-no-frieren")
	if len(eps) != 2 || eps[0].Number != 1 || eps[1].Number != 2 {
		t.Fatalf("want episodes 1,2 of this series only, got %+v", eps)
	}
	if eps[0].Title != "Yolculuğun Sonu" || eps[0].ID != "/izle/sousou-no-frieren/bolum-1" {
		t.Fatalf("episode 1: %+v", eps[0])
	}
	if m := reMal.FindStringSubmatch(seriesPage); m == nil || m[1] != "52991" {
		t.Fatalf("mal id: %v", m)
	}
}

func TestParseVariants(t *testing.T) {
	vs, err := parseVariants([]byte(episodePage))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.Fansub+"/"+v.Host)
		if strings.Contains(v.URL, "asyaanimeleri") || strings.Contains(v.URL, "azelvid") {
			t.Fatalf("VIP player must be skipped: %s", v.URL)
		}
	}
	want := "Asyaanimeleri/OK.ru Asyaanimeleri/Google Drive Asyaanimeleri/Google Drive 2 Başka & Fansub/vidmoly.biz"
	if strings.Join(got, " ") != want {
		t.Fatalf("variants:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	if _, err := parseVariants([]byte("<html></html>")); err == nil {
		t.Fatal("a page without the player list must fail clearly")
	}
}

// Real site: search → details (MAL id) → episodes → variants → resolve.
func TestLiveAnimeTR(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	p := Provider{}
	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) == 0 {
		t.Fatalf("search: %v (%d)", err, len(res))
	}
	var an source.Anime
	for _, r := range res {
		if r.ID == "sousou-no-frieren" {
			an = r
		}
	}
	if an.ID == "" {
		t.Fatalf("sousou-no-frieren not in %+v", res)
	}
	d, err := p.Details(ctx, an)
	if err != nil || d.MalID != 52991 || d.EpCount < 28 {
		t.Fatalf("details: err=%v mal=%d eps=%d", err, d.MalID, d.EpCount)
	}
	t.Logf("details: %q kind=%s year=%d genres=%v", d.Title, d.Kind, d.Year, d.Genres)
	eps, err := p.Episodes(ctx, d, 1)
	if err != nil || len(eps) < 28 {
		t.Fatalf("episodes: %v (%d)", err, len(eps))
	}
	vs, err := p.Variants(ctx, d, eps[len(eps)-1])
	if err != nil || len(vs) == 0 {
		t.Fatalf("variants: %v", err)
	}
	for _, v := range vs {
		t.Logf("variant %s · %s", v.Fansub, v.Host)
	}
	for _, v := range vs {
		st, err := p.Resolve(ctx, v)
		if err != nil {
			t.Logf("resolve %s: %v", v.Host, err)
			continue
		}
		t.Logf("resolved %s: %s page=%v", v.Host, st.Qualities[0].Label, st.Qualities[0].Page)
		return
	}
	t.Fatal("no variant resolved")
}
