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

	"aniclitr/internal/config"
	"aniclitr/internal/doctor"
	"aniclitr/internal/logx"
	"aniclitr/internal/pathx"
	_ "aniclitr/internal/source/animecix"
	_ "aniclitr/internal/source/animedepo"
	_ "aniclitr/internal/source/animetr"
	_ "aniclitr/internal/source/hianime"
	"aniclitr/internal/tty"
	"aniclitr/internal/ui"
	"aniclitr/internal/update"
)

// version is bumped by scripts/release.ps1; release builds also stamp it
// from the git tag (-ldflags "-X main.version=…").
var version = "0.12.0"

func main() {
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
		case "update", "güncelle":
			os.Exit(runUpdate())
		case "-h", "--help", "help":
			fmt.Println("aniclitr             arayüzü başlat\naniclitr doctor [q]  kaynakları ve bağımlılıkları canlı test et\naniclitr update      son sürümü GitHub'dan kur\naniclitr --version   sürüm")
			return
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
	var err error
	if cfg.Renderer == "klasik" {
		// fallback: Bubble Tea's own line renderer (posters may blink)
		_, err = tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	} else {
		// default: our cell renderer (internal/tty) — double/mailbox buffered,
		// absolute addressing, self-healing, pictures as a separate layer
		fmt.Print(tty.Enter)
		app.SelfPaint(os.Stdout)
		_, err = tea.NewProgram(app, tea.WithoutRenderer(), tea.WithMouseAllMotion()).Run()
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
