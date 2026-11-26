// aniclitr — terminalde anime.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-sixel"
	"github.com/muesli/termenv"

	"aniclitr/internal/cli"
	"aniclitr/internal/config"
	"aniclitr/internal/doctor"
	"aniclitr/internal/logx"
	"aniclitr/internal/nyaa"
	"aniclitr/internal/pathx"
	"aniclitr/internal/plugin"
	"aniclitr/internal/source"
	_ "aniclitr/internal/source/anihub"
	_ "aniclitr/internal/source/animecix"
	_ "aniclitr/internal/source/animedepo"
	_ "aniclitr/internal/source/animetr"
	_ "aniclitr/internal/source/animim"
	_ "aniclitr/internal/source/hianime"
	_ "aniclitr/internal/source/sonanime"
	"aniclitr/internal/store"
	"aniclitr/internal/tools"
	"aniclitr/internal/tty"
	"aniclitr/internal/ui"
	"aniclitr/internal/update"
	"aniclitr/internal/wrapped"
)

// version is bumped on release; CI release builds also stamp it
// from the git tag (-ldflags "-X main.version=…").
var version = "1.3.0"

func main() {
	var initialURL string
	var initialContinue bool
	var initialQuery string
	var initialEp int
	var initialDLRange string
	var initialWrapped bool
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "doctor":
			q := "frieren"
			if len(os.Args) > 2 {
				q = strings.Join(os.Args[2:], " ")
			}
			doctor.Run(q, true)
			return
		case "-v", "--version", "version":
			fmt.Println("aniclitr", version)
			return
		case "update", "güncelle", "guncelle":
			os.Exit(runUpdate())
		case "ara", "search":
			q := strings.TrimSpace(strings.Join(os.Args[2:], " "))
			if q == "" {
				fmt.Fprintln(os.Stderr, "kullanım: aniclitr ara <anime adı>")
				os.Exit(2)
			}
			cfg := config.Load()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			hits := cli.Search(ctx, cfg, q)
			cancel()
			cli.PrintSearch(os.Stdout, q, hits)
			return
		case "devam", "continue":
			if len(os.Args) > 2 && (os.Args[2] == "--list" || os.Args[2] == "-l") {
				cli.PrintContinue(os.Stdout, store.Open())
				return
			}
			initialContinue = true
		case "izle", "watch":
			q, ep := cli.ParseWatchArgs(os.Args[2:])
			if q == "" {
				fmt.Fprintln(os.Stderr, "kullanım: aniclitr izle <anime adı> [bölüm]")
				os.Exit(2)
			}
			initialQuery, initialEp = q, ep
		case "indir", "download":
			q, rng := cli.ParseDownloadArgs(os.Args[2:])
			if q == "" {
				fmt.Fprintln(os.Stderr, "kullanım: aniclitr indir <anime adı> [aralık, örn: 1-12]")
				os.Exit(2)
			}
			initialQuery, initialDLRange = q, rng
		case "torrent", "nyaa":
			args := os.Args[2:]
			magnetIdx := 0
			cat := nyaa.CatAll
			flt := nyaa.FilterAll
			var qParts []string
			for i := 0; i < len(args); i++ {
				switch args[i] {
				case "--raw":
					cat = nyaa.CatRaw
				case "--tr":
					cat = nyaa.CatNonEng
				case "--en":
					cat = nyaa.CatEng
				case "--trusted":
					flt = nyaa.FilterTrusted
				case "--magnet":
					if i+1 < len(args) {
						fmt.Sscanf(args[i+1], "%d", &magnetIdx)
						i++
					}
				default:
					qParts = append(qParts, args[i])
				}
			}
			q := strings.TrimSpace(strings.Join(qParts, " "))
			if q == "" {
				fmt.Fprintln(os.Stderr, "kullanım: aniclitr torrent <sorgu> [--raw|--tr|--en] [--trusted] [--magnet N]")
				os.Exit(2)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			items, err := nyaa.Search(ctx, q, cat, flt)
			cancel()
			if err != nil {
				fmt.Fprintln(os.Stderr, "nyaa.si hatası:", err)
				os.Exit(1)
			}
			if magnetIdx > 0 {
				if magnetIdx > len(items) {
					fmt.Fprintf(os.Stderr, "geçersiz sıra: %d (toplam %d sonuç)\n", magnetIdx, len(items))
					os.Exit(1)
				}
				fmt.Println(items[magnetIdx-1].Magnet)
				return
			}
			cli.PrintTorrents(os.Stdout, q, items)
			return
		case "yedekle", "backup":
			dst := ""
			if len(os.Args) > 2 {
				dst = os.Args[2]
			}
			if err := cli.RunBackup(os.Stdout, dst, version); err != nil {
				fmt.Fprintln(os.Stderr, "yedekleme hatası:", err)
				os.Exit(1)
			}
			return
		case "geri-yukle", "geri-yükle", "restore":
			src := ""
			if len(os.Args) > 2 {
				src = os.Args[2]
			}
			if err := cli.RunRestore(os.Stdout, src); err != nil {
				fmt.Fprintln(os.Stderr, "geri yükleme hatası:", err)
				os.Exit(1)
			}
			return
		case "ozet", "özet", "wrapped":
			exportPNG := false
			outPath := ""
			for i := 2; i < len(os.Args); i++ {
				if os.Args[i] == "--png" || os.Args[i] == "-p" {
					exportPNG = true
					if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") {
						outPath = os.Args[i+1]
						i++
					}
				}
			}
			if exportPNG {
				rep := wrapped.Build(store.Open(), store.OpenLibrary(), time.Now())
				p, err := wrapped.ExportPNG(rep, outPath)
				if err != nil {
					fmt.Fprintln(os.Stderr, "özet kartı hatası:", err)
					os.Exit(1)
				}
				fmt.Printf("✦ %s (%s)\n", rep.Archetype, rep.Tagline)
				fmt.Printf("  %.1f saat · %d bölüm · %d seri\n", rep.TotalHours, rep.TotalEpisodes, rep.TotalSeries)
				fmt.Printf("✓ PNG özet kartı kaydedildi: %s\n", p)
				return
			}
			initialWrapped = true
		case "kur", "setup", "tools":
			os.Exit(runInstallTools())
		case "lab":
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			code := plugin.RunLab(ctx, os.Stdout, os.Args[2:])
			cancel()
			os.Exit(code)
		case "-h", "--help", "help":
			fmt.Println(`aniclitr                          arayüzü başlat
aniclitr <url>                    anime veya bölüm adresini doğrudan aç
aniclitr devam [--list]           son izlenen animeye devam et (--list: düz metin)
aniclitr ara <ad>                 kaynaklarda hızlı CLI araması yap
aniclitr izle <ad> [bölüm]        doğrudan ilgili animeyi ara ve aç
aniclitr indir <ad> [aralık]      doğrudan indirme için animeyi ara (örn: 1-12)
aniclitr torrent <ad> [--magnet]  Nyaa.si torrent ara (--magnet N: magnet yazdır)
aniclitr ozet [--png [dosya]]     canlı animasyonlu Anime Özeti (--png: paylaşım kartı)
aniclitr kur                      eksik yardımcı araçları (mpv/yt-dlp/aria2c/ffmpeg) kur
aniclitr yedekle [dosya.json]     ayar, geçmiş ve kitaplığı tek dosyaya yedekle
aniclitr geri-yukle <dosya>       yedekten güvenli geri yükle (ön-yedek alır)
aniclitr doctor [q]               kaynakları ve bağımlılıkları canlı test et
aniclitr doctor --blame           kaynak ve sunucu hata dökümü
aniclitr update                   son sürümü GitHub'dan kur
aniclitr --version                sürüm`)
			return
		default:
			if source.IsURL(os.Args[1]) {
				initialURL = os.Args[1]
			}
		}
	}
	if !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Println("aniclitr bir terminal arayüzüdür; etkileşimli bir terminalde çalıştır.\naniclitr doctor  →  kaynakları test et")
		os.Exit(2)
	}
	if exe, err := pathx.Exe(); err == nil {
		update.Cleanup(exe) // the binary a previous update replaced
	}
	logx.Init(config.Dir())
	defer logx.Close()
	if packs, errs := plugin.LoadAll(); len(errs) > 0 {
		for _, err := range errs {
			logx.Errorf("eklenti yükleme uyarısı: %v", err)
		}
	} else {
		logx.Infof("eklenti motoru: %d sıfır-derleme çözücü yüklendi", len(packs))
	}
	logx.Infof("aniclitr %s · WT_SESSION=%v TERM=%q COLORTERM=%q", version, os.Getenv("WT_SESSION") != "", os.Getenv("TERM"), os.Getenv("COLORTERM"))

	cfg := config.Load()
	switch {
	case os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb":
		cfg.Posters, cfg.Motion = false, false // plain mode
	case os.Getenv("WT_SESSION") != "" || os.Getenv("COLORTERM") == "truecolor" || os.Getenv("COLORTERM") == "24bit":
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	if os.Getenv("ANICLITR_REDUCED_MOTION") != "" {
		cfg.Motion = false
	}
	g := detectGraphics(cfg)
	logx.Infof("grafik: sixel=%v hücre=%dx%d ayar=%q", g.Sixel, g.CellW, g.CellH, cfg.Graphics)

	ui.Version = version
	ui.TuneGC()
	app := ui.New(cfg, g)
	switch {
	case initialURL != "":
		app.SetInitialURL(initialURL)
	case initialWrapped:
		app.SetInitialWrapped()
	case initialContinue:
		app.SetInitialContinue()
	case initialDLRange != "":
		app.SetInitialDownload(initialQuery, initialDLRange)
	case initialQuery != "":
		app.SetInitialWatch(initialQuery, initialEp)
	}
	var err error
	if cfg.Renderer == "klasik" {
		// fallback: Bubble Tea's own line renderer (posters may blink)
		_, err = tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	} else {
		// default: our cell renderer (internal/tty) — double/mailbox buffered,
		// absolute addressing, self-healing, pictures as a separate layer
		fmt.Print(tty.Enter)
		app.SelfPaint(os.Stdout)
		_, err = tea.NewProgram(app, tea.WithoutRenderer(), tea.WithMouseCellMotion()).Run()
		app.Close()
		fmt.Print(tty.Leave)
	}
	if cfg.ThemeBg || app.UsesThemeBg() {
		fmt.Print(ui.OSCReset) // give the terminal its own background back
	}
	if err != nil {
		logx.Errorf("program: %v", err)
		fmt.Fprintln(os.Stderr, "aniclitr:", err)
		fmt.Fprintln(os.Stderr, "ayrıntı:", logx.Path())
		os.Exit(1)
	}
}

// detectGraphics asks the terminal (before Bubble Tea owns it) whether it
// speaks Sixel and how big a cell is in pixels.
func detectGraphics(cfg config.Config) ui.Graphics {
	g := ui.Graphics{CellW: 10, CellH: 20}
	if !cfg.Posters || cfg.Graphics == "blocks" {
		return g
	}
	t0 := time.Now()
	g.Sixel = sixel.IsSupported()
	// Windows Terminal maps sixel onto a fixed 10×20 virtual cell, whatever
	// the font; elsewhere ask the terminal for the real cell size.
	if g.Sixel && os.Getenv("WT_SESSION") == "" {
		if w, h, err := sixel.CellSize(); err == nil && w > 0 && h > 0 {
			g.CellW, g.CellH = w, h
		}
	}
	logx.Infof("terminal sorgusu %v sürdü", time.Since(t0).Round(time.Millisecond))
	return g
}

// runUpdate is `aniclitr update`: install the latest release over the
// installed copy (or say where the installed copy is).
func runUpdate() int {
	if update.Repo == "" {
		fmt.Println("bu derlemede güncelleme deposu yok (geliştirme sürümü). Kurulu kopya: " + pathx.Installed())
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fmt.Println("aniclitr", version, "· son sürüm denetleniyor…")
	rel, err := update.Latest(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "güncelleme:", err)
		return 1
	}
	if !update.Newer(rel.Version, version) {
		fmt.Println("güncelsin:", version)
		return 0
	}
	target := pathx.Installed()
	if !pathx.RunningInstalled() {
		if _, err := os.Stat(target); err != nil {
			fmt.Println("yeni sürüm var:", rel.Version, "· kurulu kopya yok; uygulamada Ayarlar → Terminalden aç ile kur.")
			return 0
		}
	}
	fmt.Println("indiriliyor:", rel.Version)
	if err := update.Install(ctx, rel, target); err != nil {
		fmt.Fprintln(os.Stderr, "güncelleme:", err)
		return 1
	}
	fmt.Println("kuruldu:", rel.Version, "→", target)
	return 0
}

func runInstallTools() int {
	fmt.Println("aniclitr yardımcı araç denetimi:")
	st := tools.Status()
	var missing []tools.Spec
	for _, s := range tools.All {
		if st[s.Name] {
			fmt.Printf("  ✓ %-8s kurulu  (%s)\n", s.Name, s.Purpose)
		} else {
			fmt.Printf("  ✗ %-8s eksik   (%s)\n", s.Name, s.Purpose)
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		fmt.Println("Tüm yardımcı araçlar hazır.")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	failed := 0
	for _, m := range missing {
		fmt.Printf("\n↓ %s kuruluyor…\n", m.Name)
		if err := tools.Install(ctx, m.Name); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %v\n", err)
			failed++
		} else {
			fmt.Printf("  ✓ %s kuruldu.\n", m.Name)
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}
