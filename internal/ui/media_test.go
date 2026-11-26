package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/anilist"
	"aniclitr/internal/download"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// TestMediaSnapshots dumps realistic Nova ANSI frames for README screenshots
// and the animated demo GIF when ANICLITR_MEDIA_DIR is set.
func TestMediaSnapshots(t *testing.T) {
	dir := os.Getenv("ANICLITR_MEDIA_DIR")
	if dir == "" {
		t.Skip("set ANICLITR_MEDIA_DIR to generate README media snapshots")
	}

	now := time.Now()
	makeSchedule := func() []anilist.Airing {
		mk := func(id, mal int, title, cover, studio, desc string, score, pop, eps, ep int, genres []string, add time.Duration) anilist.Airing {
			var m anilist.Media
			m.ID, m.IDMal = id, mal
			m.Title.Romaji = title
			m.Cover.XL = cover
			m.Format = "TV"
			m.Episodes = eps
			m.Score = score
			m.Popularity = pop
			m.Genres = genres
			m.Season, m.SeasonYear = "FALL", now.Year()
			m.Description = desc
			m.Studios.Nodes = []struct {
				Name string `json:"name"`
			}{{Name: studio}}
			return anilist.Airing{At: now.Add(add), Episode: ep, Media: m}
		}
		return []anilist.Airing{
			mk(154587, 52991, "Sousou no Frieren", "test://p0", "Madhouse",
				"Şeytan Kral'ı yendikten yıllar sonra elf büyücü Frieren, insanları daha yakından tanımak için yeni yoldaşlarıyla kuzeye doğru uzun bir yolculuğa çıkar.",
				91, 980000, 28, 9, []string{"Macera", "Dram", "Fantastik"}, 3*time.Hour),
			mk(161645, 54492, "Kusuriya no Hitorigoto", "test://p2", "OLM",
				"İmparatorluk sarayında görev yapan zeki eczacı Maomao, saray entrikalarını ve gizemli zehirlenme vakalarını keskin gözlemleriyle çözer.",
				89, 760000, 24, 14, []string{"Gizem", "Dram", "Tarihi"}, 5*time.Hour),
			mk(171018, 57334, "Dandadan", "test://p1", "Science SARU",
				"Hayaletlere inanan Momo ile uzaylılara inanan Okarun, iddiaya girdikleri gece ikisinin de gerçek olduğunu öğrenir.",
				86, 820000, 12, 8, []string{"Aksiyon", "Komedi", "Doğaüstü"}, 26*time.Hour),
			mk(127230, 44511, "Chainsaw Man", "test://p3", "MAPPA",
				"Yoksulluk içinde şeytan avlayan Denji, yoldaşı Pochita ile birleşerek Testere İblisi'ne dönüşür ve Kamu Güvenliği bürosuna katılır.",
				85, 920000, 12, 5, []string{"Aksiyon", "Fantastik"}, 30*time.Hour),
			mk(131681, 46102, "Odd Taxi", "test://p5", "OLM",
				"Tokyo'nun gece sokaklarında taksi süren Odokawa, yolcularının sıradan sohbetleri arasında kayıp bir lise öğrencisinin izine rastlar.",
				87, 450000, 13, 10, []string{"Gizem", "Dram"}, 52*time.Hour),
			mk(457, 457, "Mushishi", "test://p4", "Artland",
				"Mushi adı verilen ilkel varlıkları inceleyen gezgin Ginko, doğa ile insan arasındaki kırılgan dengeyi korumak için köy köy dolaşır.",
				87, 510000, 26, 17, []string{"Macera", "Fantastik", "Gizem"}, 74*time.Hour),
		}
	}

	buildApp := func(w, h int) (*App, source.Anime, []source.Episode) {
		a := showcaseApp(t, w, h)
		a.cfg.UI = "yeni"
		a.cfg.Motion = false
		a.cfg.SpeedGraph = true
		a.cfg.DownloadDir = "~/Videos/aniclitr"
		a.schedule = makeSchedule()

		an := source.Anime{
			Source: "animecix", ID: "100", Title: "Sousou no Frieren",
			AltTitle: "Frieren: Beyond Journey's End",
			Poster:   "test://p0", Kind: "TV", Year: 2023, MalID: 52991, EpCount: 28,
			Description: "Şeytan Kral'ı yendikten yıllar sonra elf büyücü Frieren, eski yoldaşlarını anmak ve insanları daha yakından tanımak için çırak büyücü Fern ile kuzeye doğru uzun bir yolculuğa çıkar.",
			Genres:      []string{"Macera", "Dram", "Fantastik"},
			NextEpisode: "Cmt 18:30 · 29. bölüm",
			Seasons:     []source.Season{{Number: 1, Name: "Sezon 1", EpisodeCount: 28}},
		}
		a.details[an.Key()] = an

		titles := []string{
			"Yolculuğun Sonu", "Büyülü Olmasına Gerek Yoktu", "Öldüren Büyü",
			"Ruhların Dinlendiği Diyar", "Ölülerin Hayaletleri", "Köyün Kahramanı",
			"Bir Masal Gibi", "Cellat Frieren", "Giyotin Aura", "Güçlü Bir Büyücü",
			"Kuzey Yaylasında Kış", "Gerçek Bir Kahraman", "Aynı Türden Tiksinme",
			"Gençlerin Ayrıcalığı", "Sorun Kokusu", "Uzun Ömürlü Dostlar",
			"Kendine İyi Bak", "Birinci Sınıf Büyücü Sınavı", "Kusursuz Plan",
			"Gerekli Öldürme", "Büyücülerin Dünyası", "Yarından İtibaren Düşmanız",
		}
		var eps []source.Episode
		for i, title := range titles {
			ep := source.Episode{ID: fmt.Sprint(i + 1), Season: 1, Number: i + 1, Title: title}
			if i == 11 {
				ep.Filler = true
			}
			eps = append(eps, ep)
		}
		a.episodes[epKey(an, 1)] = eps

		// Seed realistic offline downloads + running download with speed sparkline.
		dlRoot := t.TempDir()
		f1 := filepath.Join(dlRoot, "S01E01.mp4")
		f2 := filepath.Join(dlRoot, "S01E02.mp4")
		f3 := filepath.Join(dlRoot, "S01E03.mp4")
		for _, f := range []string{f1, f2, f3} {
			_ = os.WriteFile(f, make([]byte, 1024), 0o644)
		}
		for _, j := range []download.Job{
			{ID: 1, Source: "animecix", AnimeID: "100", Title: "Sousou no Frieren", Poster: "test://p0", Season: 1, Episode: 1, State: download.Done, Done: 412 << 20, Total: 412 << 20, Quality: "1080p", Fansub: "HakaiRyuu", Path: f1, Finished: now.Add(-25 * time.Minute)},
			{ID: 2, Source: "animecix", AnimeID: "100", Title: "Sousou no Frieren", Poster: "test://p0", Season: 1, Episode: 2, State: download.Done, Done: 398 << 20, Total: 398 << 20, Quality: "1080p", Fansub: "HakaiRyuu", Path: f2, Finished: now.Add(-12 * time.Minute)},
			{ID: 3, Source: "animim", AnimeID: "102", Title: "Kusuriya no Hitorigoto", Poster: "test://p2", Season: 1, Episode: 1, State: download.Done, Done: 365 << 20, Total: 365 << 20, Quality: "1080p", Fansub: "HolySubs", Path: f3, Finished: now.Add(-4 * time.Minute)},
			{ID: 4, Source: "animecix", AnimeID: "100", Title: "Sousou no Frieren", Poster: "test://p0", Season: 1, Episode: 6, State: download.Running, Done: 245 << 20, Total: 410 << 20, Speed: 8.4 * (1 << 20), Quality: "1080p", Fansub: "HakaiRyuu"},
			{ID: 5, Source: "anihub", AnimeID: "101", Title: "Dandadan", Poster: "test://p1", Season: 1, Episode: 8, State: download.Queued, Total: 380 << 20, Quality: "1080p", Fansub: "AniHub"},
		} {
			a.dl.Preview(j)
		}
		return a, an, eps
	}

	writeSet := func(tag string, w, h int) {
		a, an, eps := buildApp(w, h)
		st := &source.Stream{
			Qualities: []source.Quality{
				{Label: "1080p", URL: "x", Height: 1080, Bitrate: 4_800_000, Codec: "H.264", Duration: 1440, Probed: true},
				{Label: "720p", URL: "x", Height: 720, Bitrate: 2_400_000, Codec: "H.264", Duration: 1440, Probed: true},
			},
			Subtitles: []source.Subtitle{{Label: "Türkçe", URL: "https://example.com/tr.ass"}},
		}
		views := map[string]func() screen{
			"home": func() screen {
				hm := a.stack[0].(*home)
				hm.focus = 1
				hm.mCur = 0
				return hm
			},
			"discover": func() screen {
				d := newDiscover()
				d.sched = a.schedule
				d.schedOK = true
				d.menu = false
				d.tab = tCal
				return d
			},
			"genrepick": func() screen {
				d := newDiscover()
				d.sched = a.schedule
				d.schedOK = true
				d.tab = tGenre
				d.menu = false
				d.gpick = true
				return d
			},
			"anime": func() screen {
				sc := newAnime(an, nil, 0, 0)
				sc.ready = true
				sc.an = an
				sc.srcs = []source.Anime{an, {Source: "animim", ID: "m1"}, {Source: "anihub", ID: "h1"}}
				sc.cur = 2
				a.hist.Update(an, func(e *store.Entry) {
					if e.Notes == nil {
						e.Notes = map[string]string{}
					}
					e.Notes[store.EpKey(1, 3)] = "Zoltraak sahnesi efsane — 14:20 civarı tekrar izle."
				})
				return sc
			},
			"picker": func() screen {
				p := newPicker(an, eps[2], eps, false)
				p.build(a, []source.Variant{
					{Fansub: "HakaiRyuu & Lexellures", Host: "Tau Video", URL: "u1", Lang: "tr"},
					{Fansub: "HolySubs", Host: "Animim · minami", URL: "u2", Lang: "tr"},
					{Fansub: "AnimeciX Fansub", Host: "AniHub HLS", URL: "u3", Lang: "tr"},
					{Fansub: "TR Fansub", Host: "Sibnet", URL: "u4", Lang: "tr"},
				})
				return p
			},
			"downloads": func() screen {
				dl := newDownloads()
				dl.queue = true
				dl.speedHist = []float64{3.2e6, 4.8e6, 5.5e6, 7.1e6, 6.8e6, 8.2e6, 7.9e6, 8.9e6, 8.4e6, 9.1e6, 8.4e6}
				dl.jobSpeeds[4] = []float64{3.2e6, 4.8e6, 5.5e6, 7.1e6, 6.8e6, 8.2e6, 7.9e6, 8.9e6, 8.4e6, 9.1e6, 8.4e6}
				dl.diskFree = 218 << 30
				dl.diskTotal = 512 << 30
				dl.diskOK = true
				dl.diskAt = time.Now()
				return dl
			},
			"playing": func() screen {
				p := newPlaying(an, eps[2], eps, "HakaiRyuu & Lexellures", "Tau Video", st, st.Qualities[0])
				p.started, p.pos, p.dur = true, 862, 1440
				return p
			},
		}
		for name, mk := range views {
			a.stack = a.stack[:1]
			s := mk()
			if s != a.stack[0] {
				a.stack = append(a.stack, s)
			}
			a.trans = tween{}
			v := a.View()
			if name == "downloads" {
				if realDir := a.dl.Dir(); realDir != "" {
					v = strings.ReplaceAll(v, realDir, "~/Videos/aniclitr")
				}
			}
			outPath := filepath.Join(dir, fmt.Sprintf("%s-%s.ans", name, tag))
			if err := os.WriteFile(outPath, []byte(v), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	writeSet("hi", 140, 42)
	writeSet("gif", 104, 32)
}
