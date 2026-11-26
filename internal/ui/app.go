// Package ui is aniclitr's terminal interface (Bubble Tea).
//
// Rendering rules: every frame is composed from fixed-size rectangles
// (block/hjoin), animations tick only while something actually animates,
// and every network call runs in a tea.Cmd so the UI never blocks.
package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/anilist"
	"aniclitr/internal/choose"
	"aniclitr/internal/config"
	"aniclitr/internal/diag"
	"aniclitr/internal/discord"
	"aniclitr/internal/download"
	"aniclitr/internal/extract"
	"aniclitr/internal/localmedia"
	"aniclitr/internal/logx"
	"aniclitr/internal/mal"
	"aniclitr/internal/malsync"
	"aniclitr/internal/player"
	"aniclitr/internal/router"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
	"aniclitr/internal/tools"
	"aniclitr/internal/watchparty"
)

// screen is one level of the navigation stack.
type screen interface {
	update(a *App, msg tea.Msg) tea.Cmd
	view(a *App, w, h int) string
	crumb() string
	hints() []hint
}

type hint struct{ key, label string }

// animator screens request frames while they animate.
type animator interface{ animating(a *App) bool }

// ---- messages ----

type tickMsg time.Time

type feedMsg struct {
	items  []source.FeedItem
	err    error
	cached bool // from disk, shown until the live feed arrives
}

type searchMsg struct {
	gen     int
	src     string
	results []source.Anime
	err     error
}

type detailsMsg struct {
	key   string
	anime source.Anime
	err   error
}

type episodesMsg struct {
	key    string
	season int
	eps    []source.Episode
	err    error
}

type variantsMsg struct {
	key string
	vs  []source.Variant
	err error
}

type resolveMsg struct {
	src string
	url string
	st  *source.Stream
	err error
}

type toastMsg struct {
	text string
	kind string
}

type urlOpenMsg struct {
	raw     string
	an      source.Anime
	ep      *source.Episode
	handled bool
	err     error
}

type partyEvMsg struct {
	ev watchparty.Event
	ok bool
}

type toolsDoneMsg struct {
	installed []string
	failed    []string
}

type deferredInitMsg struct{}

// ---- app ----

type resolveState struct {
	st      *source.Stream
	err     error
	kind    extract.Kind
	loading bool
	at      time.Time
}

// pruneResolved forgets stream links that have likely expired (CDN links are
// signed/temporary) and failures that may have been transient. Permanent
// upstream errors (deleted, copyright, 404, private) stay cached.
func (a *App) pruneResolved() {
	for k, v := range a.resolved {
		if v.loading {
			continue
		}
		if v.st != nil && time.Since(v.at) > 20*time.Minute {
			delete(a.resolved, k)
			continue
		}
		if v.err != nil && !v.kind.Permanent() && time.Since(v.at) > 2*time.Minute {
			delete(a.resolved, k)
		}
	}
}

// App is the root Bubble Tea model.
type App struct {
	cfg     config.Config
	hist    *store.History
	stack   []screen
	w, h    int
	frame   int
	start   time.Time
	ticking bool

	posters  *posterCache
	details  map[string]source.Anime
	episodes map[string][]source.Episode // key|season
	variants map[string][]source.Variant // key|s|e
	resolved map[string]*resolveState    // variant url

	toast      string
	toastKind  string
	toastUntil time.Time
	toastAt    time.Time

	graphics Graphics
	gfx      *gfx

	trans    tween // screen slide transition
	transDir int   // +1 push (enter from right), -1 pop
	help     bool

	path    pathOps // aniclitr on PATH (pathui.go)
	lib     *store.Library
	libRev  int               // library revision libMemo belongs to
	libMemo map[string]libHit // libItem answers (same.go)

	aligns     map[string]*alignState // season chains by anime key (seasonsync.go)
	pulse      dlPulse                // download speed history for the Nova top bar
	cards      []toastCard            // Nova notifications (nova_shell.go)
	covers     []coverRect            // boxes drawn over the body this frame (pictures under them are dropped)
	malWRev    int                    // library revision malWMemo belongs to
	malWMemo   map[int]int            // MAL id → watched count (seasonsync.go)
	recentRev  int                    // history revision recentMemo belongs to
	recentMemo []store.Entry
	recentOK   bool
	dl         *download.Manager
	mal        *mal.Client
	msync      *malsync.Syncer
	malErr     string
	al         *anilist.AuthClient
	alsync     *anilist.Syncer
	alErr      string
	rpc        *discord.Client
	keys       config.KeyMap
	sleep       sleepTimer
	party       *watchparty.Room
	relayParty  *watchparty.RelayRoom
	chatVisible bool
	local       *localmedia.Index
	preload     *router.PreloadCache
	toolsBusy   bool

	schedule []anilist.Airing // this week's airing (AniList)
	coverAt  string           // poster the cover theme currently follows
	health   map[string]source.Health

	drop       *dropdown // header menu / notification panel
	undos      []undoOp
	offline    bool                 // no internet: only the offline shelf is shown
	failed     map[string]time.Time // variant URLs that failed to play recently
	permFailed map[string]bool      // variant URLs that failed permanently (deleted, copyright, 404…)
	playSpeed  float64              // mpv playback speed preserved across episodes
	notices    []notice
	bellT      tween
	dlSeen     map[int]string

	out        io.Writer // self-paint target (nil: Bubble Tea renders View)
	pt         *painter
	paintCost  time.Duration
	picsOn     bool // the last frame had pictures (glides jump instead)
	hideImages bool // a screen covers its own pictures this frame (dropdowns)

	// look & feel
	themeName string
	tfade     *themeFade
	oscUntil  time.Time // (re)send the terminal background colour until then
	intro     string

	// micro-interactions
	gliding  bool // a selection band is still sliding (set during View)
	fx       confetti
	flashT   tween
	logoKick tween // the logo letters hop (after a search, theme change…)
	anchorX  int
	anchorY  int
	anchorOK bool

	// overlays
	pal          *palette
	popup        *libPopup
	initURL      string
	initContinue bool
	initQuery    string
	initEp       int
	initDLRange  string
	initWrapped  bool
	forward      []screen // screen history for alt+left / alt+right

	// screensaver
	lastInput  time.Time
	saver      bool
	saverAt    time.Time
	saverWx    weather
	saverPop   int // last firework bucket
	lastRPCKey string
	rpcBootAt  time.Time
}

// typer screens own printable keys while a text field is focused.
type typer interface{ typing() bool }

func (a *App) motion() bool { return a.cfg.Motion }

// New builds the app with the home screen on the stack.
func New(cfg config.Config, g Graphics) *App {
	a := &App{
		cfg:       cfg,
		graphics:  g,
		hist:      store.Open(),
		lib:       store.OpenLibrary(),
		dlSeen:     map[int]string{},
		health:     map[string]source.Health{},
		failed:     map[string]time.Time{},
		permFailed: map[string]bool{},
		start:      time.Now(),
		lastInput: time.Now(),
		details:   map[string]source.Anime{},
		episodes:  map[string][]source.Episode{},
		variants:  map[string][]source.Variant{},
		resolved:  map[string]*resolveState{},
	}
	a.path = newPathOps()
	a.gfx = newGfx(g, a.sixelWanted())
	a.posters = newPosterCache(a.gfx)
	dir := cfg.DownloadDir
	if dir == "" {
		dir = download.DefaultDir()
	}
	a.dl = download.OpenLazy(dir)
	a.initV2()
	a.mal = mal.New(cfg.MalClientID)
	a.msync = malsync.New(a.mal, a.lib)
	if a.mal.Connected() {
		a.msync.Start()
	}
	a.al = anilist.OpenAuth(cfg.AniListClientID)
	a.alsync = anilist.NewSyncer(a.al, a.lib)
	if a.al.Connected() {
		a.alsync.Start()
	}
	a.rpc = discord.New(cfg.DiscordRPC, cfg.DiscordAppID)
	a.keys = config.LoadKeys()
	for _, j := range a.dl.Jobs() {
		a.dlSeen[j.ID] = j.State
	}
	nerd = cfg.Icons == "nerd"
	t := themeByName(cfg.Theme)
	applyTheme(t)
	a.themeName = t.Name
	a.oscUntil = time.Now().Add(1500 * time.Millisecond) // first frames may take a moment
	a.intro = pickIntro(cfg.Intro, time.Now())
	a.stack = []screen{newHome(a)}
	a.maybeOnboard()
	return a
}

// sixelWanted applies the graphics setting to what the terminal can do.
func (a *App) sixelWanted() bool {
	switch a.cfg.Graphics {
	case "sixel":
		return true
	case "blocks":
		return false
	}
	return a.graphics.Sixel
}

func (a *App) applyGraphics() {
	a.gfx.on = a.sixelWanted() && a.cfg.Posters
	a.gfx.invalidate()
}

func (a *App) Init() tea.Cmd {
	// Critical path first: frame 1 + feed + live listeners start immediately.
	// Secondary background probes (health, schedule, updates, loose-file organizer)
	// stagger cleanly after 320ms so launch feels instantaneous.
	cmds := []tea.Cmd{
		a.tick(), a.loadFeed(),
		probeNet(), a.waitDL(), a.waitMal(), a.waitAniList(), tea.SetWindowTitle("aniclitr"),
		a.migratePath(),
		tea.Tick(320*time.Millisecond, func(time.Time) tea.Msg { return deferredInitMsg{} }),
	}
	if a.initURL != "" {
		cmds = append(cmds, a.openURL(a.initURL))
	} else if a.initWrapped {
		cmds = append(cmds, a.push(newWrappedScreen(a)))
	} else if a.initContinue {
		if recent := a.continueEntries(); len(recent) > 0 {
			cmds = append(cmds, continueEntry(a, recent[0]))
		} else {
			a.notify("devam edilecek anime bulunamadı", "warn")
		}
	} else if a.initQuery != "" {
		res := newResults(a.initQuery)
		cmds = append(cmds, a.push(res), res.start(a))
		if a.initDLRange != "" {
			a.notify(fmt.Sprintf("%s · indirme aralığı: %s (animeyi açıp D'ye bas)", a.initQuery, a.initDLRange), "info")
		} else if a.initEp > 0 {
			a.notify(fmt.Sprintf("%s · %d. bölüm aranıyor…", a.initQuery, a.initEp), "info")
		}
	}
	return tea.Batch(cmds...)
}

// SetInitialURL schedules a direct anime/episode URL to be opened on startup.
func (a *App) SetInitialURL(raw string) {
	a.initURL = strings.TrimSpace(raw)
}

// SetInitialContinue resumes the most recently watched anime immediately on startup.
func (a *App) SetInitialContinue() {
	a.initContinue = true
}

// SetInitialWatch launches directly into searching for query (and optional episode hint).
func (a *App) SetInitialWatch(query string, ep int) {
	a.initQuery = strings.TrimSpace(query)
	a.initEp = ep
}

// SetInitialDownload launches directly into searching for query with a download range hint.
func (a *App) SetInitialDownload(query, epRange string) {
	a.initQuery = strings.TrimSpace(query)
	a.initDLRange = strings.TrimSpace(epRange)
}

// SetInitialWrapped opens the animated Wrapped summary immediately on startup.
func (a *App) SetInitialWrapped() {
	a.initWrapped = true
}

func (a *App) libTitles() []string {
	items := a.lib.All()
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Title != "" {
			out = append(out, it.Title)
		}
	}
	return out
}

func (a *App) organizeLooseDownloads() tea.Cmd {
	titles := a.libTitles()
	return guard("klasörleme", func() tea.Msg {
		if n := a.dl.OrganizeLooseFiles(titles); n > 0 {
			return toastMsg{text: fmt.Sprintf("✓ %d dağınık video otomatik klasörlendi (ctrl+d)", n), kind: "ok"}
		}
		return nil
	})
}

func (a *App) waitParty() tea.Cmd {
	r := a.party
	if r == nil {
		return nil
	}
	return guard("oda", func() tea.Msg {
		ev, ok := <-r.Events
		return partyEvMsg{ev: ev, ok: ok}
	})
}

func (a *App) installMissingTools() tea.Cmd {
	if a.toolsBusy {
		a.notify("araç kurulumu zaten devam ediyor…", "warn")
		return a.tick()
	}
	missing := tools.Missing()
	if len(missing) == 0 {
		a.notify("tüm yardımcı araçlar zaten kurulu (mpv, yt-dlp, aria2c, ffmpeg)", "ok")
		return a.tick()
	}
	a.toolsBusy = true
	names := make([]string, len(missing))
	for i, m := range missing {
		names[i] = m.Name
	}
	a.notify("kuruluyor: "+strings.Join(names, ", ")+"…", "")
	return tea.Batch(a.tick(), run(8*time.Minute, func(ctx context.Context) tea.Msg {
		var okList, failList []string
		for _, m := range missing {
			if err := tools.Install(ctx, m.Name); err != nil {
				failList = append(failList, m.Name)
			} else {
				okList = append(okList, m.Name)
			}
		}
		return toolsDoneMsg{installed: okList, failed: failList}
	}))
}

func (a *App) openURL(raw string) tea.Cmd {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	a.notify("bağlantı açılıyor…", "")
	return tea.Batch(a.tick(), run(30*time.Second, func(ctx context.Context) tea.Msg {
		an, ep, handled, err := source.OpenURL(ctx, raw)
		return urlOpenMsg{raw: raw, an: an, ep: ep, handled: handled, err: err}
	}))
}

func (a *App) onURLOpen(m urlOpenMsg) tea.Cmd {
	if !m.handled {
		a.notify("bu sitenin bağlantısı tanınmadı", "warn")
		return a.tick()
	}
	if m.err != nil {
		a.notifyErr("bağlantı açılamadı", m.err)
		return a.tick()
	}
	a.details[m.an.Key()] = m.an
	season, epNum := 0, 0
	if m.ep != nil {
		season, epNum = max(1, m.ep.Season), m.ep.Number
	}
	sc := newAnime(m.an, a.siblings(m.an), season, epNum)
	sc.explicit = true // a pasted link names its source
	return tea.Batch(a.push(sc), sc.start(a))
}

func (a *App) top() screen { return a.stack[len(a.stack)-1] }

func (a *App) push(s screen) tea.Cmd {
	a.forward = nil
	a.stack = append(a.stack, s)
	a.slide(+1)
	a.syncBrowseDiscord()
	return a.tick()
}

func (a *App) browseDiscordActivity() (string, discord.Activity, bool) {
	if len(a.stack) == 0 {
		return "", discord.Activity{}, false
	}
	if a.rpcBootAt.IsZero() {
		a.rpcBootAt = time.Now()
	}
	top := a.top()
	switch s := top.(type) {
	case *playing:
		return "", discord.Activity{}, false // playing owns Discord presence
	case *animeScreen:
		img := s.an.Poster
		if img == "" {
			if d, ok := a.details[s.an.Key()]; ok && d.Poster != "" {
				img = d.Poster
			}
		}
		largeImg, smallImg, smallTxt := img, discord.DefaultIconURL, "aniclitr"
		if largeImg == "" {
			largeImg, smallImg, smallTxt = discord.DefaultIconURL, "", ""
		}
		key := "anime:" + s.an.Key() + ":" + largeImg
		return key, discord.Activity{
			Details:    s.an.Title,
			State:      "Bölümlere göz atıyor",
			LargeImage: largeImg,
			LargeText:  s.an.Title,
			SmallImage: smallImg,
			SmallText:  smallTxt,
			Start:      a.rpcBootAt,
		}, true
	case *picker:
		largeImg, smallImg, smallTxt := s.an.Poster, discord.DefaultIconURL, "aniclitr"
		if largeImg == "" {
			largeImg, smallImg, smallTxt = discord.DefaultIconURL, "", ""
		}
		key := fmt.Sprintf("pick:%s:%d:%d", s.an.Key(), s.ep.Season, s.ep.Number)
		return key, discord.Activity{
			Details:    s.an.Title,
			State:      fmt.Sprintf("Bölüm %s · kaynak seçiyor", s.ep.Num()),
			LargeImage: largeImg,
			LargeText:  s.an.Title,
			SmallImage: smallImg,
			SmallText:  smallTxt,
			Start:      a.rpcBootAt,
		}, true
	case *results:
		q := strings.TrimSpace(s.query)
		st := "Anime arıyor"
		if q != "" {
			st = fmt.Sprintf("“%s” arıyor", q)
		}
		return "results:" + q, discord.Activity{
			Details:    "aniclitr",
			State:      st,
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *torrentScreen:
		st := "Nyaa.si · torrent arıyor"
		if q := strings.TrimSpace(s.queried); q != "" {
			st = fmt.Sprintf("Torrent · “%s”", q)
		}
		return "torrent:" + st, discord.Activity{
			Details:    "aniclitr",
			State:      st,
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *discover:
		return "discover", discord.Activity{
			Details:    "aniclitr",
			State:      "Keşfet · yeni anime arıyor",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *feedScreen:
		return "feed", discord.Activity{
			Details:    "aniclitr",
			State:      "Yeni çıkan bölümlere bakıyor",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *library:
		st := fmt.Sprintf("Kitaplığına bakıyor (%d seri)", len(a.lib.All()))
		return "library:" + st, discord.Activity{
			Details:    "aniclitr",
			State:      st,
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *downloads:
		return "downloads", discord.Activity{
			Details:    "aniclitr",
			State:      "Çevrimdışı indirilenlere bakıyor",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *wrappedScreen:
		return "wrapped", discord.Activity{
			Details:    "aniclitr",
			State:      "Anime Özetine bakıyor",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *settings:
		return "settings", discord.Activity{
			Details:    "aniclitr",
			State:      "Ayarları düzenliyor",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *partyScreen:
		return "party", discord.Activity{
			Details:    "aniclitr",
			State:      "Birlikte İzle odasında",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *malScreen, *aniListScreen:
		return "sync", discord.Activity{
			Details:    "aniclitr",
			State:      "Hesap eşitliyor",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	case *charactersScreen:
		largeImg, smallImg, smallTxt := s.an.Poster, discord.DefaultIconURL, "aniclitr"
		if largeImg == "" {
			largeImg, smallImg, smallTxt = discord.DefaultIconURL, "", ""
		}
		return "chars:" + s.an.Key(), discord.Activity{
			Details:    s.an.Title,
			State:      "Karakterler ve kadroya bakıyor",
			LargeImage: largeImg,
			LargeText:  s.an.Title,
			SmallImage: smallImg,
			SmallText:  smallTxt,
			Start:      a.rpcBootAt,
		}, true
	default:
		return "home", discord.Activity{
			Details:    "aniclitr",
			State:      "Ana sayfada geziniyor",
			LargeImage: discord.DefaultIconURL,
			LargeText:  "aniclitr",
			Start:      a.rpcBootAt,
		}, true
	}
}

func (a *App) syncBrowseDiscord() {
	if a.rpc == nil || !a.cfg.DiscordRPC {
		return
	}
	key, act, ok := a.browseDiscordActivity()
	if !ok || key == a.lastRPCKey {
		return
	}
	a.lastRPCKey = key
	a.rpc.SetActivity(act)
}

func browsableScreen(s screen) bool {
	switch s.(type) {
	case *playing, *picker, *onboard:
		return false
	}
	return true
}

func (a *App) goBack() {
	if len(a.stack) <= 1 {
		return
	}
	left := a.top()
	if browsableScreen(left) {
		if len(a.forward) >= 16 {
			a.forward = a.forward[1:]
		}
		a.forward = append(a.forward, left)
	}
	a.pop()
}

func (a *App) goForward() {
	if len(a.forward) == 0 {
		return
	}
	s := a.forward[len(a.forward)-1]
	a.forward = a.forward[:len(a.forward)-1]
	a.stack = append(a.stack, s)
	a.slide(+1)
	a.syncBrowseDiscord()
}

func (a *App) pop() {
	if len(a.stack) > 1 {
		left := a.top()
		a.stack = a.stack[:len(a.stack)-1]
		a.slide(-1)
		switch left.(type) {
		case *library, *discover, *results, *downloads, *charactersScreen:
			trimMemory() // poster-heavy screens leave garbage behind
		}
		a.syncBrowseDiscord()
	}
}

func (a *App) slide(dir int) {
	if a.motion() {
		a.trans, a.transDir = newTween(125*time.Millisecond), dir
	}
}

func (a *App) notify(text, kind string) {
	if kind == "err" {
		playSound(a, sndError)
	}
	a.toast, a.toastKind, a.toastUntil, a.toastAt = text, kind, time.Now().Add(4*time.Second), time.Now()
	if a.nova() {
		a.pushCard(text, kind)
	}
}

func (a *App) notifyErr(prefix string, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if errors.Is(err, source.ErrNotFound) {
		msg = "bulunamadı"
	} else if errors.Is(err, context.DeadlineExceeded) {
		msg = "zaman aşımı"
	} else if k := extract.KindOf(err); k != extract.KindUnknown && k.Message() != "" {
		msg = k.Message()
	}
	a.notify(prefix+": "+msg, "err")
}

func (a *App) markSourceBlocked(src string) {
	if src == "" {
		return
	}
	a.health[src] = source.Health{
		ID:  src,
		OK:  false,
		Err: extract.KindBlocked.Message(),
		At:  time.Now(),
	}
}

// ---- animation clock ----

func (a *App) needsTick() bool {
	if time.Now().Before(a.toastUntil) || a.trans.active() || a.posters.fading() || a.gfx.busy() || a.painterBusy() || len(a.posters.reload) > 0 {
		return true
	}
	if a.pal != nil && a.pal.openT.active() {
		return true
	}
	if (a.drop != nil && a.drop.openT.active()) || a.bellT.active() {
		return true
	}
	if a.tfade != nil || a.fx.active() || a.flashT.active() || a.logoKick.active() || a.gliding || time.Now().Before(a.oscUntil) || a.saver {
		return true
	}
	for _, p := range a.posters.items {
		if p.loading {
			return true
		}
	}
	for _, s := range a.stack {
		if an, ok := s.(animator); ok && an.animating(a) {
			return true
		}
	}
	return false
}

func (a *App) fastTickNeeded() bool {
	if time.Now().Before(a.toastUntil) || a.trans.active() || a.posters.fading() || a.gfx.busy() || a.painterBusy() || len(a.posters.reload) > 0 {
		return true
	}
	if a.pal != nil && a.pal.openT.active() {
		return true
	}
	if (a.drop != nil && a.drop.openT.active()) || a.bellT.active() {
		return true
	}
	if a.tfade != nil || a.fx.active() || a.flashT.active() || a.logoKick.active() || a.gliding || time.Now().Before(a.oscUntil) {
		return true
	}
	for _, p := range a.posters.items {
		if p.loading {
			return true
		}
	}
	for _, s := range a.stack {
		if h, ok := s.(*home); ok {
			if h.fastAnimating(a) {
				return true
			}
			continue
		}
		if an, ok := s.(animator); ok && an.animating(a) {
			return true
		}
	}
	return false
}

// tickInterval returns 60 FPS during active motion/interaction and a silky,
// ultra-low-CPU 20 FPS cadence when only calm background ambient weather runs.
func (a *App) tickInterval() time.Duration {
	if a.fastTickNeeded() {
		return time.Second / 60
	}
	return 50 * time.Millisecond // 20 FPS calm ambient weather & screensaver
}

func (a *App) tick() tea.Cmd {
	if a.ticking {
		return nil
	}
	a.ticking = true
	return tea.Tick(a.tickInterval(), func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---- async helpers ----

func run(timeout time.Duration, f func(ctx context.Context) tea.Msg) tea.Cmd {
	return guard("arka plan", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return f(ctx)
	})
}

func (a *App) loadFeed() tea.Cmd {
	live := run(20*time.Second, func(ctx context.Context) tea.Msg {
		for _, p := range source.All() {
			if f, ok := p.(source.Feeder); ok && a.cfg.Enabled(p.Info().ID, p.Info().Legacy) {
				items, err := f.Latest(ctx)
				if err == nil {
					saveFeedCache(items)
				}
				return feedMsg{items: items, err: err}
			}
		}
		return feedMsg{}
	})
	// the last known feed paints instantly; the live one replaces it
	return tea.Batch(func() tea.Msg {
		if items := loadFeedCache(); len(items) > 0 {
			return feedMsg{items: items, cached: true}
		}
		return nil
	}, live)
}

func diagOutcome(err error, empty bool) diag.Outcome {
	if err == nil {
		if empty {
			return diag.Empty
		}
		return diag.OK
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return diag.Timeout
	}
	return diag.Error
}

func (a *App) fetchDetails(an source.Anime) tea.Cmd {
	key := an.Key()
	if _, ok := a.details[key]; ok {
		return nil
	}
	p := source.Get(an.Source)
	if p == nil {
		return nil
	}
	a.details[key] = source.Anime{} // mark in-flight
	return run(30*time.Second, func(ctx context.Context) tea.Msg {
		t0 := time.Now()
		d, err := p.Details(ctx, an)
		diag.Record(an.Source, "details", time.Since(t0), diagOutcome(err, false))
		return detailsMsg{key: key, anime: d, err: err}
	})
}

func epKey(an source.Anime, season int) string { return fmt.Sprintf("%s|%d", an.Key(), season) }

func varKey(an source.Anime, ep source.Episode) string {
	return fmt.Sprintf("%s|%d|%d|%s", an.Key(), ep.Season, ep.Number, ep.ID)
}

func (a *App) fetchEpisodes(an source.Anime, season int) tea.Cmd {
	key := epKey(an, season)
	if _, ok := a.episodes[key]; ok {
		return nil
	}
	p := source.Get(an.Source)
	a.episodes[key] = nil
	return run(40*time.Second, func(ctx context.Context) tea.Msg {
		t0 := time.Now()
		eps, err := p.Episodes(ctx, an, season)
		if !errors.Is(err, source.ErrNotFound) {
			diag.Record(an.Source, "episodes", time.Since(t0), diagOutcome(err, len(eps) == 0))
		}
		return episodesMsg{key: key, season: season, eps: eps, err: err}
	})
}

func (a *App) fetchVariants(an source.Anime, ep source.Episode) tea.Cmd {
	key := varKey(an, ep)
	if _, ok := a.variants[key]; ok {
		return nil
	}
	p := source.Get(an.Source)
	a.variants[key] = nil
	return run(30*time.Second, func(ctx context.Context) tea.Msg {
		t0 := time.Now()
		vs, err := p.Variants(ctx, an, ep)
		diag.Record(an.Source, "variants", time.Since(t0), diagOutcome(err, len(vs) == 0))
		return variantsMsg{key: key, vs: vs, err: err}
	})
}

func (a *App) resolve(src string, v source.Variant) tea.Cmd {
	if a.permFailed[v.URL] {
		return nil
	}
	if st, ok := a.resolved[v.URL]; ok && (st.loading || st.st != nil || st.err != nil) {
		return nil
	}
	p := source.Get(src)
	a.resolved[v.URL] = &resolveState{loading: true}
	return tea.Batch(a.tick(), run(60*time.Second, func(ctx context.Context) tea.Msg {
		t0 := time.Now()
		st, err := p.Resolve(ctx, v)
		if err == nil && (st == nil || len(st.Qualities) == 0) {
			err = extract.NewError(extract.KindDeleted, "oynatılabilir kaynak yok")
		}
		diag.Record(src, "resolve", time.Since(t0), diagOutcome(err, st == nil || len(st.Qualities) == 0))
		diag.Flush()
		return resolveMsg{src: src, url: v.URL, st: st, err: err}
	}))
}

func (a *App) selectionRules() choose.Rules {
	return choose.Rules{
		HostOrder:        a.cfg.HostOrder,
		NeverHosts:       a.cfg.NeverHosts,
		OnlyHosts:        a.cfg.OnlyHosts,
		WaitAfterFirstMs: a.cfg.WaitAfterFirstMs,
	}.Effective(a.cfg.Quality)
}

// pickQualityFor prefers the quality last played for this anime (across
// sources via sibling), falling back to the global cfg.Quality preference.
func (a *App) pickQualityFor(an source.Anime, st *source.Stream) source.Quality {
	pref := ""
	if e, ok := a.hist.Get(an.Key()); ok && e.Quality != "" {
		pref = e.Quality
	} else if sib, ok := a.sibling(an); ok && sib.Quality != "" {
		pref = sib.Quality
	}
	if pref != "" {
		if q, ok := matchQualityPref(st.Qualities, pref); ok {
			return q
		}
	}
	return a.pickQuality(st)
}

func matchQualityPref(qs []source.Quality, pref string) (source.Quality, bool) {
	if len(qs) == 0 {
		return source.Quality{}, false
	}
	for _, c := range qs {
		if !c.Broken && strings.EqualFold(c.Label, pref) {
			return c, true
		}
	}
	var h int
	for _, tok := range strings.Fields(strings.ToLower(pref)) {
		if strings.HasSuffix(tok, "p") {
			fmt.Sscanf(strings.TrimSuffix(tok, "p"), "%d", &h)
			if h > 0 {
				break
			}
		}
	}
	if h > 0 {
		for _, c := range qs {
			if !c.Broken && c.Height > 0 && c.Height <= h {
				return c, true
			}
		}
	}
	return source.Quality{}, false
}

// pickQuality applies the configured preference.
func (a *App) pickQuality(st *source.Stream) source.Quality {
	q := st.Qualities[0]
	want := strings.TrimSuffix(strings.ToLower(a.cfg.Quality), "p")
	if want == "" || want == "best" {
		return q
	}
	var h int
	fmt.Sscanf(want, "%d", &h)
	for _, c := range st.Qualities {
		if !c.Broken && c.Height > 0 && c.Height <= h {
			return c
		}
	}
	return q
}

// ---- update ----

func (a *App) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	skipPaint := false
	if a.out != nil {
		defer func() {
			if !skipPaint {
				a.paint()
			}
		}()
	}
	// A bug in one screen must never take the whole app (and the user's
	// terminal) down: log it, tell the user, keep running.
	defer func() {
		if r := recover(); r != nil {
			logx.Panic(fmt.Sprintf("Update(%T)", msg), r)
			a.notify("iç hata — ayrıntı: "+logx.Path(), "err")
			model, cmd = a, a.tick()
		}
	}()
	logMsgErr(msg)
	var cmds []tea.Cmd
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		a.gfx.invalidate() // the terminal clears on resize; images must be re-sent
		a.posters.resized()
		a.resizeScreen()
		return a, nil
	case tickMsg:
		a.ticking = false
		a.frame++
		a.stepTheme()
		a.fx.step()
		if _, home := a.top().(*home); home && a.motion() && !a.saver && a.pal == nil && a.popup == nil && !a.help &&
			time.Since(a.lastInput) > saverAfter {
			a.saver, a.saverAt = true, time.Now()
		}
		if a.needsTick() {
			cmds = append(cmds, a.tick())
		}
	case tea.MouseMsg:
		// Ignore bare mouse-move events completely without repainting a frame.
		if m.Action == tea.MouseActionMotion {
			skipPaint = true
			return a, nil
		}
		a.lastInput = time.Now()
		if a.saver {
			a.wake()
			return a, nil
		}
		if a.pal != nil || a.popup != nil {
			return a, nil
		}
	case tea.KeyMsg:
		a.lastInput = time.Now()
		if m.Type == tea.KeyCtrlC {
			if pl, ok := a.top().(*playing); ok && pl.sess != nil {
				pl.sess.Quit()
			}
			for _, sc := range a.stack {
				if ts, ok := sc.(*torrentScreen); ok {
					ts.closeStream()
				}
			}
			if a.party != nil {
				a.party.Close()
			}
			if a.rpc != nil {
				a.rpc.Close()
			}
			return a, tea.Quit
		}
		if a.saver {
			a.wake()
			return a, a.tick()
		}
		if a.help {
			a.help = false
			return a, nil
		}
		if a.drop != nil {
			return a, tea.Batch(a.drop.update(a, m), a.tick())
		}
		if a.pal != nil {
			return a, tea.Batch(a.pal.update(a, m), a.tick())
		}
		if a.popup != nil {
			return a, tea.Batch(a.popup.update(a, m), a.tick())
		}
		typing := false
		if t, ok := a.top().(typer); ok {
			typing = t.typing()
		}
		switch m.String() {
		case "up", "down", "left", "right", "tab", "shift+tab", "pgup", "pgdown":
			playSound(a, sndTick)
		case "j", "k", "h", "l":
			if !typing {
				playSound(a, sndTick)
			}
		}
		act, hasAct := a.keys.Action(m.String())
		if !hasAct {
			// Fixed function-key aliases remain active even when ctrl+* keys are customized.
			switch m.String() {
			case "f1":
				act, hasAct = "actions", true
			case "f2":
				act, hasAct = "library", true
			case "f3":
				act, hasAct = "downloads", true
			case "f4":
				act, hasAct = "discover", true
			case "f9":
				act, hasAct = "bell", true
			case "f10":
				act, hasAct = "menu", true
			}
		}
		if (hasAct && act == "actions") || (m.String() == "?" && !typing) {
			a.openActions()
			return a, a.tick()
		}
		if hasAct {
			switch act {
			case "palette":
				a.openPalette("")
				return a, a.tick()
			case "themes":
				a.openPalette("tema ")
				return a, a.tick()
			case "library":
				return a, a.openLibrary("")
			case "downloads":
				return a, a.openDownloads()
			case "discover":
				if a.offline {
					a.notify("keşfet internet ister — şu an çevrimdışısın", "warn")
					return a, a.tick()
				}
				return a, a.openDiscover()
			case "torrent":
				ts := newTorrentScreen("")
				return a, a.push(ts)
			case "undo":
				return a, a.undoLast()
			case "menu":
				a.openDropdown("menu")
				return a, a.tick()
			case "bell":
				a.openDropdown("bell")
				return a, a.tick()
			case "back":
				if _, pl := a.top().(*playing); !pl && len(a.stack) > 1 {
					a.goBack()
					return a, a.tick()
				}
			case "forward":
				if _, pl := a.top().(*playing); !pl && len(a.forward) > 0 {
					a.goForward()
					return a, a.tick()
				}
			case "redraw":
				a.redraw() // repaint the whole terminal from scratch
				a.gfx.invalidate()
				return a, nil
			}
		}
	case playerMsg:
		// Pure in-mpv menu cursor moves only redraw the mpv ASS overlay — skip
		// terminal frame recomposition behind mpv for <1ms menu responsiveness.
		if m.ev.Kind == player.EvAction {
			switch m.ev.Action {
			case "aniclitr-menu", "aniclitr-menu-close", "aniclitr-menu-back", "aniclitr-menu-up", "aniclitr-menu-down",
				"aniclitr-menu-left", "aniclitr-menu-right", "aniclitr-sub-up", "aniclitr-sub-down":
				skipPaint = true
			}
		}
	case searchMsg:
		if m.err != nil && extract.KindOf(m.err) == extract.KindBlocked {
			a.markSourceBlocked(m.src)
		}
	case posterMsg:
		a.posters.set(m)
		return a, a.tick()
	case detailsMsg:
		if m.err != nil {
			delete(a.details, m.key)
		} else {
			a.details[m.key] = m.anime
			if _, ok := a.top().(*animeScreen); ok {
				a.syncBrowseDiscord()
			}
		}
	case episodesMsg:
		if m.err != nil {
			delete(a.episodes, m.key)
		} else {
			a.episodes[m.key] = m.eps
		}
	case variantsMsg:
		if m.err != nil {
			delete(a.variants, m.key)
		} else {
			a.variants[m.key] = m.vs
		}
	case resolveMsg:
		k := extract.KindOf(m.err)
		a.resolved[m.url] = &resolveState{st: m.st, err: m.err, kind: k, at: time.Now()}
		if k.Permanent() {
			a.permFailed[m.url] = true
		}
		if k == extract.KindBlocked {
			a.markSourceBlocked(m.src)
		}
	case toastMsg:
		a.notify(m.text, m.kind)
		cmds = append(cmds, a.tick())
	case dlMsg:
		a.onDL()
		a.samplePulse()
		cmds = append(cmds, a.waitDL(), a.tick(), a.offlineCovers()) // new shelf cards get their covers
	case malMsg:
		a.onMal()
		cmds = append(cmds, a.waitMal())
	case aniListMsg:
		a.onAniList()
		cmds = append(cmds, a.waitAniList())
	case partyEvMsg:
		if m.ok {
			switch m.ev.Kind {
			case "join":
				a.notify(fmt.Sprintf("◈ %s odaya katıldı (%d kişi)", m.ev.Nick, m.ev.Peers), "ok")
			case "leave":
				a.notify(fmt.Sprintf("◈ %s odadan ayrıldı (%d kişi)", m.ev.Nick, m.ev.Peers), "warn")
			case "closed":
				a.party = nil
				a.notify("◈ birlikte izleme odası kapandı", "warn")
			}
			if a.party != nil {
				cmds = append(cmds, a.waitParty())
			}
			cmds = append(cmds, a.tick())
		}
	case relayEvMsg:
		if m.ok {
			switch m.ev.Kind {
			case "join":
				a.notify(fmt.Sprintf("◈ %s odaya katıldı", m.ev.Nick), "ok")
			case "leave":
				a.notify(fmt.Sprintf("◈ %s odadan ayrıldı", m.ev.Nick), "warn")
			case "chat":
				a.notify(fmt.Sprintf("💬 %s: %s", m.ev.Nick, m.ev.Text), "ok")
			case "reaction":
				a.notify(fmt.Sprintf("%s %s", m.ev.Reaction, m.ev.Nick), "ok")
			case "closed":
				a.relayParty = nil
			}
			if a.relayParty != nil {
				cmds = append(cmds, a.waitRelayParty())
			}
			cmds = append(cmds, a.tick())
		}
	case localScanMsg:
		if m.count > 0 {
			logx.Infof("yerel hibrit kütüphane: %d dosya indekslendi", m.count)
		}
	case toolsDoneMsg:
		a.toolsBusy = false
		switch {
		case len(m.installed) > 0 && len(m.failed) == 0:
			a.notify("✓ araçlar kuruldu: "+strings.Join(m.installed, ", "), "ok")
		case len(m.installed) > 0:
			a.notify("kuruldu: "+strings.Join(m.installed, ", ")+" · kurulamadı: "+strings.Join(m.failed, ", "), "warn")
		case len(m.failed) > 0:
			a.notify("kurulamadı: "+strings.Join(m.failed, ", ")+" (aniclitr kur ile terminalden dene)", "err")
		}
		cmds = append(cmds, a.tick())
	case malOpenMsg:
		cmds = append(cmds, a.onMalOpen(m))
	case urlOpenMsg:
		cmds = append(cmds, a.onURLOpen(m))
	case alignMsg:
		a.onAlign(m)
		if s, ok := a.top().(*animeScreen); ok && s.an.Key() == m.key {
			a.maybeAskSeasonSync(s.an)
		}
	case malCandsMsg:
		a.onMalCands(m)
	case similarMsg:
		a.onSimilar(m)
	case netMsg:
		cmds = append(cmds, a.onNet(m))
	case netRetryMsg:
		if a.offline {
			cmds = append(cmds, probeNet())
		}
	case healthMsg:
		a.onHealth(m.h)
	case healthTickMsg:
		cmds = append(cmds, a.checkHealth())
	case updateMsg:
		a.onUpdate(m)
		cmds = append(cmds, a.tick())
	case deferredInitMsg:
		a.syncBrowseDiscord()
		cmds = append(cmds, a.loadSchedule(), a.checkHealth(), a.checkUpdate(false), a.scanLocalMediaAsync(), guard("indirme-tara", func() tea.Msg {
			a.dl.ScanDisk()
			return nil
		}), a.organizeLooseDownloads())
	case appSchedMsg:
		if m.err == nil {
			a.schedule = m.items
		}
	}
	// Every screen sees data messages (so background screens stay in sync);
	// input goes to the top screen only.
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		cmds = append(cmds, a.top().update(a, msg))
	default:
		for _, s := range append([]screen(nil), a.stack...) {
			cmds = append(cmds, s.update(a, msg))
		}
	}
	a.applyCover()
	cmds = append(cmds, a.posters.takeReloads()...) // trimmed covers drawn again
	if a.needsTick() {
		cmds = append(cmds, a.tick())
	}
	return a, tea.Batch(cmds...)
}

// logMsgErr records every failed background operation.
func logMsgErr(msg tea.Msg) {
	switch m := msg.(type) {
	case searchMsg:
		if m.err != nil {
			logx.Errorf("arama [%s]: %v", m.src, m.err)
		}
	case detailsMsg:
		if m.err != nil {
			logx.Errorf("detay [%s]: %v", m.key, m.err)
		}
	case episodesMsg:
		if m.err != nil && !errors.Is(m.err, source.ErrNotFound) { // an empty season is not a failure
			logx.Errorf("bölümler [%s]: %v", m.key, m.err)
		}
	case variantsMsg:
		if m.err != nil {
			logx.Errorf("varyant [%s]: %v", m.key, m.err)
		}
	case resolveMsg:
		if m.err != nil {
			logx.Errorf("çözüm [%s]: %v", m.url, m.err)
		}
	case feedMsg:
		if m.err != nil {
			logx.Errorf("akış: %v", m.err)
		}
	}
}

// ---- view ----

// View is Bubble Tea's string render path (used in tests and when the
// app does not paint itself). In self-paint mode it returns nothing: the
// cell renderer (render.go) draws instead.
func (a *App) View() (out string) {
	if a.out != nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			logx.Panic("View", r)
			out = "\n  aniclitr: çizim hatası — esc ile geri dön.\n  ayrıntı: " + logx.Path()
		}
	}()
	frame, suppress := a.compose()
	if frame == nil {
		return ""
	}
	frame = a.gfx.finish(frame, suppress || len(a.covers) > 0) // the classic path cannot drop single pictures
	return a.withOSC(strings.Join(frame, "\n"))
}

// compose builds the frame lines (with image markers still in place) and
// says whether images must be hidden (overlays, transitions).
func (a *App) compose() (frame []string, suppress bool) {
	a.gfx.reqs = a.gfx.reqs[:0]
	a.gliding = false
	a.hideImages = false
	a.covers = a.covers[:0]
	if a.w == 0 || a.h == 0 {
		return nil, true
	}
	if a.flashT.active() {
		// the selection band lights up and settles back (enter / toggle feedback)
		saved := cBand
		cBand = mix(mix(hex(saved), hex(cAccent), .45), hex(saved), easeOutCubic(a.flashT.p())).hex()
		defer func() { cBand = saved }()
	}
	if a.w < 60 || a.h < 18 {
		msg := []string{stAccent.Render("aniclitr"), stMuted.Render(fmt.Sprintf("pencere çok küçük (%d×%d)", a.w, a.h)), stMuted.Render("en az 60×18")}
		out := make([]string, a.h)
		for i := range out {
			out[i] = ""
		}
		for i, l := range msg {
			if y := a.h/2 - 1 + i; y >= 0 && y < a.h {
				out[y] = center(l, a.w)
			}
		}
		return out, true
	}
	if a.saver {
		frame := a.saverView(a.w, a.h)
		if a.fx.active() {
			a.fx.draw(frame, a.w)
		}
		return frame, true
	}
	bodyH := a.h - 3
	header := a.header()
	body := a.top().view(a, a.w, bodyH)
	lines := strings.Split(body, "\n")
	lines = block(lines, a.w, bodyH)
	a.anchorX, a.anchorY, a.anchorOK = findAnchor(lines)
	if a.fx.active() {
		a.fx.draw(lines, a.w)
	}
	// slide transition: snappy 8-col ease-out glide (Doherty threshold <130ms)
	if a.trans.active() {
		off := int((1 - easeOutCubic(a.trans.p())) * 8)
		for i, l := range lines {
			if off <= 0 {
				break
			}
			if a.transDir > 0 {
				lines[i] = strings.Repeat(" ", off) + ansi.Truncate(l, a.w-off, "")
			} else {
				lines[i] = ansi.TruncateLeft(l, off, "") + strings.Repeat(" ", off)
			}
		}
	}
	if a.help {
		lines = overlay(dim(lines), a.helpBox(), a.w)
	}
	if a.popup != nil {
		lines = overlay(dim(lines), a.popup.view(a), a.w)
	}
	if a.pal != nil {
		lines = a.pal.draw(a, lines)
	}
	if a.drop != nil {
		lines = a.drop.draw(a, lines)
	}
	frame = make([]string, 0, a.h)
	if a.nova() {
		frame = append(frame, a.novaHeader(), a.novaRule())
		frame = append(frame, lines...)
		frame = append(frame, a.novaFooter())
	} else {
		frame = append(frame, header, stFaint.Render(strings.Repeat("─", a.w)))
		frame = append(frame, lines...)
		frame = append(frame, a.footer())
	}
	// images are hidden while something slides or covers them
	// dropdowns and the search suggestions only cover a box: the pictures
	// under it go (a.covers), the rest stay; full-screen dims hide them all
	return frame, a.trans.active() || a.help || a.pal != nil || a.popup != nil || a.hideImages || !a.gfx.on
}

// withOSC prefixes the terminal-background sequence for a few frames after
// a theme change (Bubble Tea may drop views between flushes, so it is
// repeated for a short while).
func (a *App) withOSC(frame string) string {
	if time.Now().After(a.oscUntil) {
		return frame
	}
	if !a.cfg.ThemeBg {
		return OSCReset + frame
	}
	return oscBg(cBg) + frame
}

// wake leaves the screensaver.
func (a *App) wake() {
	a.saver = false
	a.lastInput = time.Now()
	a.gfx.invalidate()
}

// UsesThemeBg reports whether the terminal background was repainted (main
// restores it on exit).
func (a *App) UsesThemeBg() bool { return a.cfg.ThemeBg }

func (a *App) header() string {
	left := " " + wordmark()
	crumbs := []string{}
	for _, s := range a.stack[1:] {
		if c := s.crumb(); c != "" {
			crumbs = append(crumbs, c)
		}
	}
	if len(crumbs) > 0 {
		var parts []string
		for i, c := range crumbs {
			if i == len(crumbs)-1 {
				parts = append(parts, stText.Render(c))
			} else {
				parts = append(parts, stMuted.Render(c))
			}
		}
		left += stFaint.Render("  ╱  ") + strings.Join(parts, stFaint.Render(" › "))
	}
	var right, dots []string
	for _, p := range source.All() {
		in := p.Info()
		if in.Legacy && !a.cfg.Legacy {
			continue
		}
		dot := stFaint.Render("○")
		if a.cfg.Enabled(in.ID, in.Legacy) {
			dot = a.healthDot(in.ID)
		}
		dots = append(dots, dot) // the dots alone: names and details are on "kaynak durumu"
	}
	if len(dots) > 0 {
		right = append(right, stFaint.Render("kaynaklar ")+strings.Join(dots, " "))
	}
	if a.cfg.Legacy {
		right = append(right, pill("ARŞİV", cWarn))
	}
	if n := a.dl.Active(); n > 0 {
		// downloads in flight: a small live counter on every screen
		var done, total int64
		for _, j := range a.dl.Jobs() {
			if j.State == download.Running {
				done += j.Done
				total += j.Total
			}
		}
		label := fmt.Sprintf("↓ %d", n)
		if total > 0 {
			label += fmt.Sprintf(" · %%%d", int(float64(done)/float64(total)*100))
		}
		right = append([]string{fg(cAccent).Bold(true).Render(label)}, right...)
	}
	right = append(right, stFaint.Render("│"), a.headerButtons())
	r := strings.Join(right, "  ") + " "
	lw := a.w - lipgloss.Width(r)
	if lipgloss.Width(left) > lw {
		left = truncate(left, max(0, lw-1))
		if lw < 30 {
			return fit(" "+wordmark(), a.w)
		}
	}
	return fit(left, lw) + r
}

func (a *App) footer() string {
	right := ""
	if time.Now().Before(a.toastUntil) && a.toast != "" {
		c := cSub
		icon := "•"
		switch a.toastKind {
		case "err":
			c, icon = cErr, "✗"
		case "ok":
			c, icon = cOK, "✓"
		case "warn":
			c, icon = cWarn, "!"
		}
		msg := icon + " " + truncate(a.toast, max(10, a.w/2)) + " "
		if t := time.Since(a.toastAt); t < 260*time.Millisecond && a.motion() {
			// slide in from the right edge
			n := int(easeOutCubic(float64(t)/float64(260*time.Millisecond)) * float64(lipgloss.Width(msg)))
			msg = ansi.Truncate(msg, n, "")
		}
		right = fg(c).Render("▌") + fg(c).Render(msg)
	}
	lw := a.w - lipgloss.Width(right)
	return fit(hintLine(a.footerHints(), lw), lw) + right
}

// hintLine fits key hints into w cells. The last two (işlemler, geri)
// always stay; screen keys are dropped from the end until the line fits.
func hintLine(hs []hint, w int) string {
	sep := stFaint.Render("   ")
	render := func(hs []hint) string {
		out := " "
		for i, h := range hs {
			if i > 0 {
				out += sep
			}
			out += stText.Bold(true).Render(h.key)
			if h.label != "" {
				out += " " + stMuted.Render(h.label)
			}
		}
		return out
	}
	tail := 0
	for i := len(hs) - 1; i >= 0 && (hs[i].key == "?" || hs[i].key == "F1" || hs[i].key == "esc"); i-- {
		tail++
	}
	for n := len(hs) - tail; n >= 0; n-- {
		cut := append(append([]hint{}, hs[:n]...), hs[len(hs)-tail:]...)
		if l := render(cut); lipgloss.Width(l) <= w {
			return l
		}
	}
	return truncate(render(hs[len(hs)-tail:]), w)
}

// coverRect is a box drawn over the body, in frame cells.
type coverRect struct{ x, y, w, h int }

// cover records a box drawn at body position (x, y): pictures that touch it
// are not sent this frame (text may never be written into a picture), while
// the others stay on screen.
func (a *App) cover(x, y, w, h int) {
	a.covers = append(a.covers, coverRect{x: x, y: y + 2, w: w, h: h}) // the body starts under the top bar and its rule
}

// covered reports whether a picture at (row, col) sized w×h touches a cover.
func (a *App) covered(row, col, w, h int) bool {
	for _, c := range a.covers {
		if col < c.x+c.w && c.x < col+w && row < c.y+c.h && c.y < row+h {
			return true
		}
	}
	return false
}
