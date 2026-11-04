package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/anilist"
	"aniclitr/internal/player"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// Keşfet: the live schedule with countdowns, the current season, picks
// based on what you liked, and a slot-machine random pick — all from
// AniList (public, no login). Every title carries its MAL id, so enter
// finds it on the sources and `a` puts it on your (MAL) list.

type schedMsg struct {
	items []anilist.Airing
	err   error
}

type seasonMsg struct {
	season string
	year   int
	items  []anilist.Media
	err    error
}

type recsMsg struct {
	items []anilist.Media
	err   error
}

type randMsg struct {
	m   anilist.Media
	err error
}

var discoverTabs = []string{"takvim", "bu akşam", "bu sezon", "türler", "öneriler", "rastgele"}

// discoverDesc says, in one line, what each section answers.
var discoverDesc = []string{
	"Bu haftanın yayın akışı ve bölüm saatleri",
	"Ayrılan süreye göre izleme önerileri",
	"Bu sezonun yapımları, en beğenilenler önce",
	"Türe göre en iyi ve en popüler yapımlar",
	"Beğendiklerinize benzeyen yapımlar",
	"Rastgele bir anime önerisi",
}

// tab indices
const (
	tCal = iota
	tTonight
	tSeason
	tGenre
	tRecs
	tRandom
)

type drow struct {
	header string
	air    *anilist.Airing
	m      *anilist.Media
}

type discover struct {
	menu     bool // the section menu (step one) instead of a section
	gpick    bool // türler: choosing the genre (step two) before its list
	mgl      glide
	tab      int
	cur, off [6]int
	gl       glide
	at       time.Time
	onlyMine bool
	all      bool // calendar: include low-popularity titles

	sched    []anilist.Airing
	schedOK  bool
	schedErr error

	sName     string
	sYear     int
	season    []anilist.Media
	seasonOK  bool
	seasonErr error

	recs    []anilist.Media
	recsOK  bool
	recsErr error

	rnd     *anilist.Media
	rndErr  error
	rndNote string // what the dice were rolled on

	budget int // "bu akşam": minutes you have

	genre      int
	gmode      string
	gkey       string
	genreItems []anilist.Media
	genreOK    bool
	genreErr   error
	rolling    tween
	rollPool   []string
}

func newDiscover() *discover {
	s, y := anilist.SeasonOf(time.Now())
	return &discover{menu: true, at: time.Now(), sName: s, sYear: y, budget: 60, gmode: anilist.ModeTop}
}

// openDiscover brings Keşfet forward.
func (a *App) openDiscover() tea.Cmd {
	for i, s := range a.stack {
		if d, ok := s.(*discover); ok {
			a.stack = a.stack[:i+1]
			a.slide(-1)
			return tea.Batch(a.tick(), d.load(a))
		}
	}
	d := newDiscover()
	return tea.Batch(a.push(d), d.load(a))
}

func (d *discover) crumb() string {
	if d.menu {
		return "keşfet"
	}
	return "keşfet › " + discoverTabs[d.tab]
}

func (d *discover) hints() []hint {
	if d.menu {
		return []hint{{"enter", "aç"}, {"1-6", "bölüme git"}, {"esc", "geri"}}
	}
	h := []hint{{"enter", "aç"}, {"a", "listeme ekle"}, {"t", "fragman"}}
	switch d.tab {
	case tCal:
		h = append(h, hint{"f", "sadece takip"}, hint{"p", "hepsi"})
	case tSeason:
		h = append(h, hint{"[ ]", "sezon"})
	case tTonight:
		return []hint{{"+ -", "süre"}, {"enter", "aç"}, {"tab", "sonraki bölüm"}, {"esc", "bölümler"}}
	case tGenre:
		if d.gpick {
			return []hint{{"enter", "bu türü aç"}, {"m", "sıralama: " + d.gmode}, {"tab", "sonraki bölüm"}, {"esc", "bölümler"}}
		}
		h = append(h, hint{"m", "sıralama: " + d.gmode}, hint{"[ ]", "önceki / sonraki tür"}, hint{"space", "bu türden zar at"})
	case tRandom:
		h = append(h, hint{"space", "zar at"})
	}
	return append(h, hint{"tab", "sonraki bölüm"}, hint{"esc", "bölümler"})
}

func (d *discover) animating(a *App) bool {
	if a.top() != screen(d) {
		return false
	}
	loading := (d.tab == tGenre && !d.genreOK) || (d.tab == tCal && !d.schedOK) || (d.tab == tSeason && !d.seasonOK) || (d.tab == tRecs && !d.recsOK) || (d.tab == tRandom && d.rnd == nil && d.rndErr == nil)
	// the calendar's countdowns tick once a second (cheap: only changed cells are written)
	return loading || d.rolling.active() || revealing(d.at, 20, a.motion()) || (d.tab == tCal && a.frame%30 == 0)
}

// load fetches what the current tab needs (cached by the client).
func (d *discover) load(a *App) tea.Cmd {
	switch d.tab {
	case tGenre:
		return d.loadGenre(a)
	case tTonight:
		return nil
	case tCal:
		if d.schedOK {
			return nil
		}
		return run(40*time.Second, func(ctx context.Context) tea.Msg {
			now := time.Now()
			from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			items, err := anilist.Schedule(ctx, from, from.AddDate(0, 0, 7))
			return schedMsg{items, err}
		})
	case tSeason:
		if d.seasonOK {
			return nil
		}
		s, y := d.sName, d.sYear
		return run(40*time.Second, func(ctx context.Context) tea.Msg {
			items, err := anilist.Season(ctx, s, y)
			return seasonMsg{s, y, items, err}
		})
	case tRecs:
		if d.recsOK {
			return nil
		}
		var liked []int
		have := map[int]bool{}
		for _, it := range a.lib.All() {
			if it.MalID == 0 {
				continue
			}
			have[it.MalID] = true
			if it.Score >= 8 || (it.Status == store.Completed && it.Score == 0) || it.InList("favoriler") {
				liked = append(liked, it.MalID)
			}
		}
		return run(60*time.Second, func(ctx context.Context) tea.Msg {
			if len(liked) == 0 {
				return recsMsg{err: fmt.Errorf("öneri için kitaplığında puanladığın ya da bitirdiğin anime olmalı")}
			}
			items, err := anilist.Recommend(ctx, liked, have)
			return recsMsg{items, err}
		})
	case tRandom:
		if d.rnd != nil || d.rolling.active() {
			return nil
		}
		return d.roll(a)
	}
	return nil
}

func (d *discover) roll(a *App) tea.Cmd {
	if a.motion() {
		d.rolling = newTween(1100 * time.Millisecond)
	}
	d.rnd, d.rndErr = nil, nil
	if len(d.rollPool) == 0 {
		for _, m := range d.season {
			d.rollPool = append(d.rollPool, m.Name())
		}
		for _, x := range d.sched {
			d.rollPool = append(d.rollPool, x.Media.Name())
		}
		if len(d.rollPool) == 0 {
			d.rollPool = []string{"Cowboy Bebop", "Mushishi", "Monster", "Ping Pong", "Mononoke", "Odd Taxi", "Frieren", "Planetes"}
		}
	}
	return tea.Batch(a.tick(), run(20*time.Second, func(ctx context.Context) tea.Msg {
		m, err := anilist.Random(ctx)
		return randMsg{m, err}
	}))
}

// following: titles on your list you are actively following.
func following(a *App) map[int]string {
	out := map[int]string{}
	for _, it := range a.lib.All() {
		if it.MalID != 0 && it.Status != store.Dropped && it.Status != store.Completed {
			out[it.MalID] = it.Status
		}
	}
	return out
}

func (d *discover) rows(a *App) []drow {
	var out []drow
	switch d.tab {
	case tCal:
		mine := following(a)
		now := time.Now()
		lastDay := ""
		for i := range d.sched {
			x := &d.sched[i]
			_, followed := mine[x.Media.IDMal]
			if d.onlyMine && !followed {
				continue
			}
			if !d.all && !followed && x.Media.Popularity < 4000 {
				continue
			}
			day := dayLabel(x.At, now)
			if day != lastDay {
				out = append(out, drow{header: day})
				lastDay = day
			}
			out = append(out, drow{air: x, m: &x.Media})
		}
	case tSeason:
		for i := range d.season {
			out = append(out, drow{m: &d.season[i]})
		}
	case tRecs:
		for i := range d.recs {
			out = append(out, drow{m: &d.recs[i]})
		}
	case tGenre:
		for i := range d.genreItems {
			out = append(out, drow{m: &d.genreItems[i]})
		}
	}
	return out
}

var trWeekdays = []string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"}
var trMonths = []string{"", "Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}

func dayLabel(t, now time.Time) string {
	t = t.Local()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	base := fmt.Sprintf("%s %d %s", trWeekdays[t.Weekday()], d1, trMonths[m1])
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "bugün · " + base
	case t.Sub(time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())) < 48*time.Hour && t.After(now):
		return "yarın · " + base
	}
	return base
}

// countdown formats time until t ("2sa 14dk", "yayında").
func countdown(t time.Time) string {
	d := time.Until(t)
	switch {
	case d <= 0:
		return "yayınlandı"
	case d < time.Minute:
		return fmt.Sprintf("%dsn", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%ddk", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dsa %ddk", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dg %dsa", int(d.Hours()/24), int(d.Hours())%24)
}

func (d *discover) selected(a *App) *anilist.Media {
	if d.tab == tRandom {
		return d.rnd
	}
	rows := d.rows(a)
	if c := d.cur[d.tab]; c >= 0 && c < len(rows) {
		return rows[c].m
	}
	return nil
}

func (d *discover) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case schedMsg:
		d.sched, d.schedErr, d.schedOK = m.items, m.err, true
		d.at = time.Now()
		d.fixCursor(a)
		return d.wantPosters(a)
	case seasonMsg:
		if m.season == d.sName && m.year == d.sYear {
			d.season, d.seasonErr, d.seasonOK = m.items, m.err, true
			d.at = time.Now()
			return d.wantPosters(a)
		}
	case genreMsg:
		if m.key == d.gkey {
			d.genreItems, d.genreErr, d.genreOK = m.items, m.err, true
			d.at = time.Now()
			return d.wantPosters(a)
		}
	case recsMsg:
		d.recs, d.recsErr, d.recsOK = m.items, m.err, true
		d.at = time.Now()
		return d.wantPosters(a)
	case randMsg:
		if m.err != nil {
			d.rndErr = m.err
			return nil
		}
		mm := m.m
		d.rnd = &mm
		return a.posters.want(mm.Poster())
	case tea.KeyMsg:
		return d.key(a, m)
	}
	return nil
}

// fixCursor lands the calendar cursor on the first upcoming episode.
func (d *discover) fixCursor(a *App) {
	if d.tab != tCal {
		return
	}
	rows := d.rows(a)
	for i, r := range rows {
		if r.air != nil && r.air.At.After(time.Now()) {
			d.cur[0] = i
			return
		}
	}
	d.cur[0] = d.nextItem(rows, 0, +1)
}

func (d *discover) nextItem(rows []drow, from, dir int) int {
	for i := from; i >= 0 && i < len(rows); i += dir {
		if rows[i].header == "" {
			return i
		}
	}
	return clamp(from-dir, 0, len(rows)-1)
}

func (d *discover) wantPosters(a *App) tea.Cmd {
	if !a.cfg.Posters {
		return nil
	}
	var cmds []tea.Cmd
	rows := d.rows(a)
	c := d.cur[d.tab]
	for i := max(0, c-3); i < len(rows) && i < c+8; i++ {
		if rows[i].m != nil {
			cmds = append(cmds, a.posters.want(rows[i].m.Poster()))
		}
	}
	return tea.Batch(cmds...)
}

func (d *discover) key(a *App, k tea.KeyMsg) tea.Cmd {
	if d.menu {
		return d.menuKey(a, k)
	}
	if d.tab == tGenre && d.gpick {
		if cmd, ok := d.genrePickKey(a, k); ok {
			return cmd
		}
	}
	switch k.String() {
	case "esc", "q", "backspace":
		if d.tab == tGenre && !d.gpick {
			d.gpick = true // back to the genre list
			return a.tick()
		}
		d.menu = true // one step back: the section menu
		a.slide(-1)
		return a.tick()
	}
	switch d.tab {
	case tTonight:
		if cmd, ok := d.tonightKey(a, k); ok {
			return cmd
		}
	case tGenre:
		if cmd, ok := d.genreKeyPress(a, k); ok {
			return cmd
		}
	}
	if d.tab != tRandom {
		d.rndNote = ""
	}
	rows := d.rows(a)
	c := &d.cur[d.tab]
	switch k.String() {
	case "tab", "right", "l":
		d.tab = (d.tab + 1) % len(discoverTabs)
		d.gpick = d.tab == tGenre
		d.at = time.Now()
		return tea.Batch(d.load(a), a.tick())
	case "shift+tab", "left", "h":
		d.tab = (d.tab + len(discoverTabs) - 1) % len(discoverTabs)
		d.gpick = d.tab == tGenre
		d.at = time.Now()
		return tea.Batch(d.load(a), a.tick())
	case "1", "2", "3", "4", "5", "6":
		d.tab = int(k.String()[0] - '1')
		d.gpick = d.tab == tGenre
		d.at = time.Now()
		return tea.Batch(d.load(a), a.tick())
	case "up", "k":
		*c = d.nextItem(rows, *c-1, -1)
	case "down", "j":
		*c = d.nextItem(rows, *c+1, +1)
	case "pgdown":
		*c = d.nextItem(rows, min(len(rows)-1, *c+10), +1)
	case "pgup":
		*c = d.nextItem(rows, max(0, *c-10), -1)
	case "f":
		if d.tab == tCal {
			d.onlyMine = !d.onlyMine
			d.fixCursor(a)
			if d.onlyMine {
				a.notify("takvim: sadece takip ettiklerin", "ok")
			}
		}
	case "p":
		if d.tab == tCal {
			d.all = !d.all
			d.fixCursor(a)
		}
	case "[", "]":
		if d.tab == tSeason {
			order := []string{"WINTER", "SPRING", "SUMMER", "FALL"}
			i := indexOf(order, d.sName)
			if k.String() == "]" {
				i++
			} else {
				i--
			}
			if i < 0 {
				i, d.sYear = 3, d.sYear-1
			} else if i > 3 {
				i, d.sYear = 0, d.sYear+1
			}
			d.sName, d.seasonOK, d.season, *c = order[i], false, nil, 0
			return tea.Batch(d.load(a), a.tick())
		}
	case " ", "r":
		if d.tab == tRandom {
			return d.roll(a)
		}
	case "enter":
		if m := d.selected(a); m != nil {
			a.flash()
			return a.openByMal(m.IDMal, m.AllTitles(), m.Poster())
		}
	case "a", "+":
		if m := d.selected(a); m != nil {
			if _, ok := a.lib.FindMal(m.IDMal); ok {
				a.notify("zaten listende", "warn")
				return nil
			}
			a.libSet(source.Anime{Source: "mal", ID: strconv.Itoa(m.IDMal), Title: m.Name(), Poster: m.Poster(), MalID: m.IDMal,
				Year: m.SeasonYear, Genres: m.Genres, EpCount: m.Episodes, Kind: m.Format}, func(it *store.Item) { it.Status = store.Planned })
			a.notify("planlananlara eklendi: "+m.Name(), "ok")
			a.celebrate(16)
			return a.tick()
		}
	case "t":
		if m := d.selected(a); m != nil {
			return a.trailer(m.IDMal, m.Name())
		}
	}
	return d.wantPosters(a)
}

// trailer plays a title's trailer in mpv (YouTube via yt-dlp).
func (a *App) trailer(malID int, name string) tea.Cmd {
	if malID == 0 {
		a.notify("bu animenin MAL eşleşmesi yok — fragman bulunamadı", "warn")
		return nil
	}
	a.notify("fragman aranıyor…", "")
	mpvPath := a.cfg.MpvPath
	return run(30*time.Second, func(ctx context.Context) tea.Msg {
		m, _, err := anilist.ByMal(ctx, malID)
		if err != nil {
			return toastMsg{"fragman alınamadı: " + err.Error(), "err"}
		}
		u := m.TrailerURL()
		if u == "" {
			return toastMsg{name + " için fragman yok", "warn"}
		}
		bin, err := player.FindMpv(mpvPath)
		if err != nil {
			return toastMsg{"mpv bulunamadı", "err"}
		}
		cmd := exec.Command(bin, "--force-window=immediate", "--title=fragman · "+name, "--ytdl-format=bestvideo[height<=1080]+bestaudio/best", u)
		if err := cmd.Start(); err != nil {
			return toastMsg{"mpv açılamadı: " + err.Error(), "err"}
		}
		go func() { _ = cmd.Wait() }()
		return toastMsg{"▶ fragman: " + name, "ok"}
	})
}

// ---- view ----

func (d *discover) view(a *App, w, h int) string {
	if d.menu {
		return d.menuView(a, w, h)
	}
	pad := 2
	inner := w - pad*2
	info := ""
	switch d.tab {
	case tCal:
		info = "AniList · yerel saat"
		if d.onlyMine {
			info = "sadece takip ettiklerin · " + info
		}
	case tSeason:
		info = fmt.Sprintf("%s %d", anilist.SeasonTR(d.sName), d.sYear)
	case tGenre:
		info = "AniList · " + d.gmode
	case tRandom:
		info = d.rndNote
	}
	out := pageHead(capTR(discoverTabs[d.tab]), discoverDesc[d.tab], info, w, h)
	bodyH := h - len(out)
	if d.tab == tRandom {
		body := d.randomCard(a, inner, bodyH)
		for _, l := range body {
			out = append(out, strings.Repeat(" ", pad)+l)
		}
		return strings.Join(out, "\n")
	}
	lw := inner
	rw := 0
	if inner >= 110 {
		rw = min(48, inner*2/5)
		lw = inner - rw - kitGap
	}
	if d.tab == tGenre && d.gpick {
		for _, l := range d.genrePickView(a, lw, rw, bodyH) {
			out = append(out, strings.Repeat(" ", pad)+l)
		}
		return strings.Join(out, "\n")
	}
	title, tag := discoverTabs[d.tab], ""
	switch d.tab {
	case tCal:
		title = "yayın akışı"
		if d.onlyMine {
			tag = "sadece takip ettiklerin"
		}
	case tSeason:
		title, tag = anilist.SeasonTR(d.sName)+fmt.Sprint(" ", d.sYear), "[ ] sezon"
	case tGenre:
		_, tr := d.genreName()
		title, tag = tr, "m: "+d.gmode
	case tTonight:
		title, tag = "sığanlar", "+ − süre"
	case tRecs:
		title = "sana göre"
	}
	var left []string
	iw, ih := lw-4, bodyH-2
	switch d.tab {
	case tTonight:
		left = d.tonightView(a, iw, ih)
	default:
		left = d.list(a, iw, ih)
	}
	cols := [][]string{panel(title, left, lw, bodyH, true, cAccent, tag)}
	if rw > 0 {
		if d.tab == tTonight {
			cols = append(cols, d.tonightCard(a, rw, bodyH))
		} else {
			cols = append(cols, d.card(a, d.selected(a), rw, bodyH))
		}
	}
	for _, l := range hjoin(kitGap, cols...) {
		out = append(out, strings.Repeat(" ", pad)+l)
	}
	return strings.Join(out, "\n")
}

func lipglossWidth(s string) int { return ansi.StringWidth(s) }

func (d *discover) list(a *App, w, h int) []string {
	var err error
	ok := true
	switch d.tab {
	case tCal:
		err, ok = d.schedErr, d.schedOK
	case tSeason:
		err, ok = d.seasonErr, d.seasonOK
	case tRecs:
		err, ok = d.recsErr, d.recsOK
	case tGenre:
		err, ok = d.genreErr, d.genreOK
	}
	if !ok {
		return []string{"", stAccent.Render(spinner(a.frame/2)) + stMuted.Render(" AniList'ten yükleniyor…")}
	}
	if err != nil {
		return []string{"", stErr.Render("✗ " + truncate(err.Error(), w-2)), "", stMuted.Render("esc geri · tekrar açınca yeniden dener")}
	}
	rows := d.rows(a)
	if len(rows) == 0 {
		msg := "Bu görünümde bir şey yok."
		if d.onlyMine {
			msg = "Takip ettiğin (izliyor/planlanan) anime bu hafta yayınlanmıyor. f ile hepsini göster."
		}
		return []string{"", stMuted.Render(msg)}
	}
	mine := following(a)
	c := &d.cur[d.tab]
	*c = clamp(*c, 0, len(rows)-1)
	if rows[*c].header != "" {
		*c = d.nextItem(rows, *c, +1)
	}
	d.off[d.tab] = scroll(*c, d.off[d.tab], h, len(rows))
	band := d.gl.row(a, *c)
	var out []string
	for i := d.off[d.tab]; i < len(rows) && len(out) < h; i++ {
		r := rows[i]
		if r.header != "" {
			out = append(out, sectionTitle(r.header, w, strings.HasPrefix(r.header, "bugün")))
			continue
		}
		rv := reveal(d.at, i-d.off[d.tab], a.motion())
		sel := i == band
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		if sel {
			bg, bar, tc = cBand, sp("▌ ", cAccent), cAccent
		}
		m := r.m
		_, followed := mine[m.IDMal]
		_, inLib := a.lib.FindMal(m.IDMal)
		var left, right []span
		left = append(left, bar)
		switch d.tab {
		case tCal:
			at := r.air.At.Local()
			past := at.Before(time.Now())
			tcol := cSub
			if past {
				tcol = cFaint
			}
			left = append(left, sp(at.Format("15:04")+"  ", tcol))
			if followed {
				left = append(left, sp("● ", cAccent))
			} else {
				left = append(left, sp("  ", cText))
			}
			left = append(left, span{t: m.Name(), c: tc, bold: sel || followed}, sp(fmt.Sprintf("  B%d", r.air.Episode), cMuted))
			cd := countdown(r.air.At)
			cc := cAccent2
			if past {
				cc = cFaint
			} else if time.Until(r.air.At) < 3*time.Hour {
				cc = cWarn
			}
			right = append(right, sp(cd+" ", cc))
		default:
			rank := fmt.Sprintf("%3d  ", i+1)
			left = append(left, sp(rank, cFaint))
			if inLib {
				left = append(left, sp("● ", cAccent2))
			} else {
				left = append(left, sp("  ", cText))
			}
			left = append(left, span{t: m.Name(), c: tc, bold: sel})
			if m.Score > 0 {
				right = append(right, sp(ic("star")+" ", cWarn), sp(fmt.Sprintf("%.1f  ", float64(m.Score)/10), cText))
			}
			right = append(right, sp(fmtFormat(m)+" ", cMuted))
		}
		line := row(w, bg, left, right)
		if i == *c {
			line = anchor() + line
		}
		out = append(out, line)
	}
	return out
}

func fmtFormat(m *anilist.Media) string {
	f := map[string]string{"TV": "TV", "TV_SHORT": "TV kısa", "MOVIE": "Film", "OVA": "OVA", "ONA": "ONA", "SPECIAL": "Özel", "MUSIC": "Klip"}[m.Format]
	if m.Episodes > 0 && m.Format != "MOVIE" {
		f += fmt.Sprintf(" · %d bl", m.Episodes)
	}
	return f
}

// card: poster + facts for the highlighted title.
func (d *discover) card(a *App, m *anilist.Media, w, h int) []string {
	if m == nil {
		return panel("ayrıntı", nil, w, h, false, cAccent)
	}
	acc := a.posters.accent(m.Poster(), cAccent).hex()
	iw := w - 4
	var out []string
	if a.cfg.Posters && h >= 26 {
		pw := min(iw, 26)
		ph := min(pw*3/4, h/2)
		ind := strings.Repeat(" ", (iw-pw)/2)
		for _, l := range a.posters.render(m.Poster(), pw, ph, a.frame) {
			out = append(out, ind+l)
		}
		out = append(out, "")
	}
	for i, l := range wrap(m.Name(), iw) {
		if i > 1 {
			break
		}
		out = append(out, gradient(l, []string{acc, cAccent2}, true))
	}
	if m.Title.English != "" && m.Title.English != m.Name() {
		out = append(out, stMuted.Render(truncate(m.Title.English, iw)))
	}
	var meta []string
	if f := fmtFormat(m); f != "" {
		meta = append(meta, f)
	}
	if m.SeasonYear > 0 {
		meta = append(meta, fmt.Sprintf("%s %d", anilist.SeasonTR(m.Season), m.SeasonYear))
	}
	if s := m.Studio(); s != "" {
		meta = append(meta, s)
	}
	out = append(out, stSub.Render(truncate(strings.Join(meta, " · "), iw)))
	if m.Score > 0 {
		out = append(out, scoreBar((m.Score+5)/10, acc, 1)+"  "+stBold.Render(fmt.Sprintf("%.1f", float64(m.Score)/10))+stMuted.Render(" AniList"))
	}
	if m.Next != nil {
		out = append(out, fg(cWarn).Render("◷ ")+stSub.Render(fmt.Sprintf("%d. bölüm %s", m.Next.Episode, countdownLong(time.Unix(m.Next.AiringAt, 0)))))
	}
	if it, ok := a.lib.FindMal(m.IDMal); ok {
		out = append(out, fg(statusColor(it.Status)).Render(statusIcon(it.Status)+" listende: "+it.Status))
	}
	if g := genreLine(m.Genres, iw); g != "" {
		out = append(out, "", g)
	}
	if m.Description != "" {
		out = append(out, "")
		for _, l := range wrap(anilist.CleanDescription(m.Description), iw) {
			out = append(out, stSub.Render(l))
		}
	}
	return panel("ayrıntı", out, w, h, false, acc)
}

func countdownLong(t time.Time) string {
	c := countdown(t)
	if c == "yayınlandı" {
		return "yayınlandı"
	}
	return c + " sonra"
}

// randomCard: the slot machine — names spin, slow down, land on the pick.
func (d *discover) randomCard(a *App, w, h int) []string {
	cw := min(100, w)
	var out []string
	if d.rolling.active() || (d.rnd == nil && d.rndErr == nil) {
		p := d.rolling.p()
		// a reel: three names, the middle one is "the" pick, slowing down
		speed := 1 - easeOutCubic(p)
		idx := int(float64(a.frame)*(0.3+2*speed)) % max(1, len(d.rollPool))
		out = append(out, "", "", "")
		for k := -1; k <= 1; k++ {
			name := d.rollPool[(idx+k+len(d.rollPool))%len(d.rollPool)]
			if k == 0 {
				out = append(out, center(gradient("▸  "+name+"  ◂", brand, true), w))
			} else {
				out = append(out, center(stFaint.Render(name), w))
			}
		}
		out = append(out, "", center(stMuted.Render("zar atılıyor…"), w))
		return block(out, w, h)
	}
	if d.rndErr != nil {
		return block([]string{"", center(stErr.Render("✗ "+d.rndErr.Error()), w), "", center(stMuted.Render("space ile tekrar dene"), w)}, w, h)
	}
	m := d.rnd
	body := d.card(a, m, cw, h)
	ind := strings.Repeat(" ", (w-cw)/2)
	for _, l := range body {
		out = append(out, ind+l)
	}
	return block(out, w, h)
}

// ---- "benzer animeler" on the anime screen (y) ----

type similarMsg struct {
	key  string
	recs []anilist.Media
	err  error
}

func (a *App) similar(an source.Anime) tea.Cmd {
	if an.MalID == 0 {
		a.notify("bu animenin MAL eşleşmesi yok — benzerleri bulunamadı", "warn")
		return nil
	}
	a.notify("benzer animeler aranıyor…", "")
	key, id := an.Key(), an.MalID
	return run(30*time.Second, func(ctx context.Context) tea.Msg {
		_, recs, err := anilist.ByMal(ctx, id)
		return similarMsg{key, recs, err}
	})
}

func (a *App) onSimilar(m similarMsg) {
	if m.err != nil {
		a.notify("benzerler alınamadı: "+m.err.Error(), "err")
		return
	}
	if len(m.recs) == 0 {
		a.notify("AniList'te benzer öneri yok", "warn")
		return
	}
	var items []dropItem
	for _, r := range m.recs {
		r := r
		note := fmtFormat(&r)
		if r.Score > 0 {
			note = fmt.Sprintf("%s · %.1f", note, float64(r.Score)/10)
		}
		icon, c := "◇", cAccent2
		if _, ok := a.lib.FindMal(r.IDMal); ok {
			icon, c = "●", cOK
		}
		items = append(items, dropItem{icon: icon, color: c, label: r.Name(), note: note,
			run: func(a *App) tea.Cmd { return a.openByMal(r.IDMal, r.AllTitles(), r.Poster()) }})
	}
	a.openList("◇ benzer animeler", items)
}
