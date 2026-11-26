package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/anilist"
	"aniclitr/internal/download"
	"aniclitr/internal/media"
	"aniclitr/internal/nyaa"
	"aniclitr/internal/seasons"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
	"aniclitr/internal/tstream"
)

// Every screen at several terminal sizes: no panics, no line wider than
// the terminal, exactly h lines. Optional PNG-ready dumps for review.
func TestScreenSweep(t *testing.T) {
	sizes := [][2]int{{80, 24}, {100, 30}, {140, 40}, {200, 56}}
	dir := os.Getenv("ANICLITR_SWEEP_DIR")
	for _, mode := range []string{"", "yeni"} { // classic and Nova
		for _, sz := range sizes {
			w, h := sz[0], sz[1]
			a := showcaseApp(t, w, h)
			a.cfg.UI = mode
			a.dl.Preview(download.Job{ID: 1, Source: "animecix", AnimeID: "100", Title: "Sousou no Frieren", Season: 1, Episode: 6, State: download.Running, Done: 100 << 20, Total: 400 << 20, Speed: 3 << 20, Quality: "1080p", Fansub: "HakaiRyuu"})
			an := source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren", Poster: "test://p0", Kind: "TV", Year: 2023, Description: strings.Repeat("Büyücü Frieren uzun bir yolculuğa çıkar. ", 8), Genres: []string{"Macera", "Dram"}, MalID: 52991}
			a.details[an.Key()] = an
			eps := []source.Episode{}
			for i := 1; i <= 28; i++ {
				eps = append(eps, source.Episode{ID: fmt.Sprint(i), Season: 1, Number: i, Title: fmt.Sprintf("Bölüm adı %d", i)})
			}
			st := &source.Stream{Qualities: []source.Quality{{Label: "1080p", URL: "x"}}}
			screens := map[string]func() screen{
				"home":      func() screen { return a.stack[0] },
				"library":   func() screen { return newLibrary() },
				"libstats":  func() screen { l := newLibrary(); l.selectShelf(a, "stats"); return l },
				"discover":  func() screen { d := newDiscover(); d.schedOK = true; return d },
				"discsec":   func() screen { d := newDiscover(); d.schedOK = true; d.menu = false; return d },
				"genrepick": func() screen { d := newDiscover(); d.tab, d.menu, d.gpick = tGenre, false, true; return d },
				"tonight":   func() screen { d := newDiscover(); d.tab = tTonight; d.menu = false; return d },
				"downloads": func() screen { return newDownloads() },
				"feed":      func() screen { return newFeedScreen() },
				"homemenu":  func() screen { h := a.stack[0].(*home); h.focus = 1; h.mCur = 1; return h },
				"queue":     func() screen { d := newDownloads(); d.queue = true; return d },
				"settings":  func() screen { return newSettings() },
				"sources":   func() screen { return &sourcesScreen{} },
				"hostorder": func() screen { return newHostOrder(a) },
				"mal":       func() screen { return newMalScreen(a) },
				"anilist":   func() screen { return newAniListScreen(a) },
				"torrent": func() screen {
					ts := newTorrentScreen("frieren")
					ts.queried = "frieren"
					ts.rawItems = []nyaa.Item{
						{Title: "[ReinForce] Sousou no Frieren (BDRip 1920x1080 x264 FLAC) [RAW]", Group: "ReinForce", Resolution: "1080p", Codec: "H.264", Size: "48.2 GiB", SizeBytes: 48 << 30, Seeders: 92, Leechers: 6, Downloads: 840, Category: "Anime - Raw", Raw: true, Trusted: true, Batch: true, InfoHash: "fedcba0987654321fedcba0987654321fedcba09"},
						{Title: "[HolySubs] Sousou no Frieren - 01-28 [1080p][TR Altyazılı]", Group: "HolySubs", Resolution: "1080p", Size: "32.5 GiB", SizeBytes: 32 << 30, Seeders: 180, Leechers: 14, Downloads: 2100, Category: "Anime - Non-English-translated", Turkish: true, Batch: true, InfoHash: "1234567890abcdef1234567890abcdef12345678"},
					}
					ts.applyLocal()
					return ts
				},
				"torrent_files": func() screen {
					ts := newTorrentScreen("another")
					ts.focus = 2
					ts.openItem = nyaa.Item{Title: "[Shiniori-Raws] Another (BD 1920x1080 x265 10bit FLAC)", Group: "Shiniori-Raws", Resolution: "1080p"}
					ts.files = []tstream.VideoFile{
						{Index: 0, Name: "[Shiniori-Raws] Another - 01 (BD 1080p).mkv", Size: 680 << 20, Season: 1, Episode: 1},
						{Index: 1, Name: "[Shiniori-Raws] Another - 02 (BD 1080p).mkv", Size: 695 << 20, Season: 1, Episode: 2},
					}
					return ts
				},
				"onboard":   func() screen { return &onboard{tools: map[string]bool{"mpv": true}} },
				"characters": func() screen {
					cs := newCharactersScreen(an)
					cs.loading = false
					c1 := anilist.Character{ID: 1, Role: "MAIN", Age: "1000+", Gender: "Female", Description: "Bin yılı aşkın süredir yaşayan elf büyücü."}
					c1.Name.Full = "Frieren"
					c1.Name.Native = "フリーレン"
					c2 := anilist.Character{ID: 2, Role: "MAIN", Age: "18", Gender: "Female", Description: "Frieren'in çırağı."}
					c2.Name.Full = "Fern"
					c2.Name.Native = "フェルン"
					cs.chars = []anilist.Character{c1, c2}
					return cs
				},
				"results": func() screen {
					r := newResults("frieren")
					r.order = []string{"animecix"}
					r.states = map[string]*srcState{"animecix": {count: 1}}
					r.byProv = map[string][]source.Anime{"animecix": {an}}
					r.rebuild()
					return r
				},
				"anime": func() screen {
					sc := newAnime(an, nil, 0, 0)
					sc.ensure(a)
					a.episodes[epKey(sc.an, 1)] = eps
					return sc
				},
				"picker": func() screen {
					p := newPicker(an, eps[0], eps, false)
					p.build(a, []source.Variant{{Fansub: "HakaiRyuu & Lexellures", Host: "Tau Video", URL: "u1"}, {Fansub: "AnimeciX Fansub", Host: "Sibnet", URL: "u2"}})
					return p
				},
				"playing": func() screen {
					p := newPlaying(an, eps[4], eps, "HakaiRyuu", "Tau Video", st, st.Qualities[0])
					p.started, p.pos, p.dur = true, 600, 1420
					return p
				},
				"wrapped": func() screen {
					ws := newWrappedScreen(a)
					ws.slideAt = time.Now().Add(-2 * time.Second)
					return ws
				},
				"wrapped_persona": func() screen {
					ws := newWrappedScreen(a)
					ws.slide = 4
					ws.slideAt = time.Now().Add(-2 * time.Second)
					return ws
				},
				"party": func() screen {
					return newPartyScreen()
				},
				"franchise": func() screen {
					fs := newFranchiseScreen(an)
					fs.loading = false
					fs.tree = seasons.BuildFranchiseTree(an.Title, []seasons.FranchiseNode{
						{MalID: 52991, Title: "Sousou no Frieren", Format: "TV", Year: 2023, Episodes: 28, Status: "done"},
						{MalID: 59978, Title: "Sousou no Frieren 2", Format: "TV", Year: 2026, Episodes: 12, Status: "next"},
					})
					return fs
				},
				"ost": func() screen {
					os := newOSTScreen(an)
					os.loading = false
					os.songs = []media.ThemeSong{
						{Slug: "OP1", Title: "Yuusha", Artists: "YOASOBI", Episodes: "1-16"},
						{Slug: "ED1", Title: "Anytime Anywhere", Artists: "milet", Episodes: "1-28"},
					}
					return os
				},
				"bookmarks": func() screen {
					a.hist.AddBookmark(an, store.Bookmark{Season: 1, Episode: 10, Pos: 1120, Note: "Aura sahnesi"})
					return newBookmarksScreen(an, eps)
				},
				"discover_mood": func() screen {
					d := newDiscover()
					d.tab = tRecs
					d.menu = false
					d.moodIdx = 0
					d.moodOK = true
					return d
				},
			}
			for name, mk := range screens {
				a.stack = a.stack[:1]
				s := mk()
				if s != a.stack[0] {
					a.stack = append(a.stack, s)
				}
				a.trans = tween{}
				v := a.View()
				if strings.Contains(v, "çizim hatası") {
					t.Fatalf("%s %dx%d: view panicked (see log)", name, w, h)
				}
				lines := strings.Split(ansiRe.ReplaceAllString(v, ""), "\n")
				if len(lines) != h {
					t.Errorf("%s %dx%d: %d lines", name, w, h, len(lines))
				}
				for i, l := range lines {
					if lw := ansi.StringWidth(l); lw > w {
						t.Errorf("%s %dx%d: line %d is %d wide", name, w, h, i, lw)
					}
				}
				if dir != "" {
					os.WriteFile(fmt.Sprintf("%s/%s-%dx%d.ans", dir, name, w, h), []byte(v), 0o644)
				}
			}
			// overlays on top of home
			a.stack = a.stack[:1]
			for _, ov := range []string{"palette", "bell", "menu", "help", "popup", "actions"} {
				switch ov {
				case "palette":
					a.openPalette("")
				case "bell":
					a.openDropdown("bell")
				case "menu":
					a.openDropdown("menu")
				case "help":
					a.help = true
				case "popup":
					a.openPopup(an)
				case "actions":
					a.stack[0].(*home).focus = 1
					a.openActions()
				}
				if a.pal != nil {
					a.pal.openT = tween{}
				}
				if a.drop != nil {
					a.drop.openT = tween{}
				}
				v := a.View()
				for i, l := range strings.Split(ansiRe.ReplaceAllString(v, ""), "\n") {
					if lw := ansi.StringWidth(l); lw > w {
						t.Errorf("overlay %s %dx%d: line %d is %d wide", ov, w, h, i, lw)
					}
				}
				if dir != "" {
					os.WriteFile(fmt.Sprintf("%s/ov-%s-%dx%d.ans", dir, ov, w, h), []byte(v), 0o644)
				}
				a.pal, a.drop, a.help, a.popup = nil, nil, false, nil
			}
			_ = time.Now
			_ = tea.Quit
			_ = store.Watching
		}
	}
}
