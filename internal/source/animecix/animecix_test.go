package animecix

import (
	"context"
	"os"
	"testing"
	"time"

	"aniclitr/internal/source"
)

func TestGroupNameWithIndex(t *testing.T) {
	idx := buildIndex([]translator{
		{Name: "AnimeOU", URL: "https://animeou.com"},
		{Name: "SomeSub Fansub", Discord: "https://discord.gg/somesub"},
		{Name: "Anime Ou Plus"},
		{Name: "TR Dublaj Ekibi", IsDubbing: true},
		{Name: "ab"}, // too short to match safely
	})
	cases := map[string]string{
		"AnimeOU":                "AnimeOU",
		"animeou":                "AnimeOU",
		"Anime Ou Plus çevirisi": "Anime Ou Plus", // the longest name wins
		"https://discord.gg/somesub Çevirmen: Abdulhalimyilmaz, Encoder: S4Patrol-P": "SomeSub Fansub",
		"çeviri: www.animeou.com": "AnimeOU",
		"TR Dublaj Ekibi":         "TR Dublaj Ekibi", // already says dublaj
		"":                        "Bilinmeyen",
		"ab":                      "ab", // unmatched: cleaned text kept
	}
	for in, want := range cases {
		if got := groupName(in, idx); got != want {
			t.Errorf("groupName(%q) = %q, want %q", in, got, want)
		}
	}
	dub := buildIndex([]translator{{Name: "Seslendirme TR", IsDubbing: true}})
	if got := groupName("Seslendirme TR", dub); got != "Seslendirme TR (Dublaj)" {
		t.Errorf("dubbing group: got %q", got)
	}
}

func TestCleanCreditWithoutIndex(t *testing.T) {
	cases := map[string]string{
		"https://discord.gg/xyz Çevirmen: Ali Veli, Encoder: X, Uploader: Y": "Ali Veli",
		"Cent / Redaktör: Venus":      "Cent",
		"Net-rip":                     "Net-rip",
		"Arda'":                       "Arda",
		"https://www.kara-fansub.net": "kara-fansub.net",
		"   ":                         "Bilinmeyen",
		"Çok Uzun Bir Fansub Adı Ki Ekrana Sığmaz Ve Kesilmesi Gerekir Kesinlikle": "Çok Uzun Bir Fansub Adı Ki Ekrana Sığma…",
	}
	for in, want := range cases {
		if got := groupName(in, nil); got != want {
			t.Errorf("groupName(%q, nil) = %q, want %q", in, got, want)
		}
	}
}

func TestAddrKey(t *testing.T) {
	cases := map[string]string{
		"https://www.Example-Sub.com/": "example-sub.com",
		"discord.gg/abc":               "discord.gg/abc",
		"https://discord.gg/":          "",
		"localhost":                    "",
		"":                             "",
	}
	for in, want := range cases {
		if got := addrKey(in); got != want {
			t.Errorf("addrKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLiveLongSeriesComplete guards the assumption that related-videos
// returns every episode in one response (no paging): One Piece had 1180
// episodes in a single answer on 2026-11-07. ANICLITR_LIVE=1.
func TestLiveLongSeriesComplete(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("ANICLITR_LIVE=1 ile çalışır")
	}
	t.Setenv("ANICLITR_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := Provider{}
	eps, err := p.Episodes(ctx, source.Anime{Source: "animecix", ID: "66", Kind: "TV"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) < 1100 {
		t.Fatalf("One Piece: %d bölüm döndü; related-videos sayfalanıyor olabilir", len(eps))
	}
	if eps[0].Number != 1 || eps[len(eps)-1].Number < 1100 {
		t.Fatalf("unexpected range %d…%d", eps[0].Number, eps[len(eps)-1].Number)
	}

	idx := translators(ctx)
	if len(idx) < 100 {
		t.Fatalf("translators index too small: %d", len(idx))
	}
	vs, err := p.Variants(ctx, source.Anime{Source: "animecix", ID: "66"}, source.Episode{Season: 1, Number: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Logf("%-12s %s", v.Host, v.Fansub)
		if len([]rune(v.Fansub)) > 40 {
			t.Errorf("fansub label too long: %q", v.Fansub)
		}
	}
}
