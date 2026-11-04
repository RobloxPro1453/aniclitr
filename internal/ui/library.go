package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// The library: a calm, separate room. Shelves (statuses + the user's own
// watchlists) on the left, the shelf's contents as a list or a poster wall
// in the middle, the selected title's card on the right.

func statusIcon(s string) string {
	switch s {
	case store.Watching:
		return "▶"
	case store.Completed:
		return "✓"
	case store.Planned:
		return "◷"
	case store.OnHold:
		return "◌"
	case store.Dropped:
		return "×"
	}
	return "•"
}

func statusColor(s string) string {
	switch s {
	case store.Watching:
		return cSky
	case store.Completed:
		return cOK
	case store.Planned:
		return cAccent2
	case store.OnHold:
		return cWarn
	case store.Dropped:
		return cErr
	}
	return cSub
}

func listIcon(name string) string {
	if strings.EqualFold(name, "favoriler") {
		return ic("heart")
	}
	return ic("list")
}

type shelf struct {
	id    string // "all" | status | "list:<name>" | "new"
	label string
	icon  string
	color string
	count int
}

var sortNames = []string{"son güncelleme", "puan", "ad", "eklenme"}

type library struct {
	focus     int // 0 shelves, 1 items
	side      int
	cur, off  int
	grid      bool
	sortBy    int
	sideGl    glide
	curGl     glide
	filter    input
	filtering bool
	naming    bool // new list / rename input in the sidebar
	renaming  string
	nameIn    input
	at        time.Time // shelf switch: cascade reveal
	scoreT    tween
	scoreKey  string
	sideID    string // current shelf id (for hints)
	lastW     int    // items pane width (grid columns)
}

func newLibrary() *library {
	return &library{at: time.Now(), filter: input{placeholder: "kitaplıkta ara…"}, nameIn: input{placeholder: "liste adı…"}}
}

// openLibrary brings the library forward (reusing one already on the stack).
func (a *App) openLibrary(shelfID string) tea.Cmd {
	for i, s := range a.stack {
		if l, ok := s.(*library); ok {
			a.stack = a.stack[:i+1]
			l.selectShelf(a, shelfID)
			a.slide(-1)
			return a.tick()
		}
	}
	l := newLibrary()
	l.selectShelf(a, shelfID)
	return tea.Batch(a.push(l), l.prefetch(a))
}

func (l *library) selectShelf(a *App, id string) {
	if id == "" {
		return
	}
	for i, s := range l.shelves(a) {
		if s.id == id {
			l.side, l.cur, l.off, l.focus = i, 0, 0, 1
			l.at = time.Now()
		}
	}
}

func (l *library) crumb() string { return "kitaplık" }

func (l *library) typing() bool { return l.filtering || l.naming }

func (l *library) hints() []hint {
	if l.filtering || l.naming {
		return []hint{{"enter", "tamam"}, {"esc", "vazgeç"}}
	}
	if l.focus == 0 {
		h := []hint{{"↑↓", "raf"}, {"→ enter", "aç"}, {"n", "yeni liste"}}
		if strings.HasPrefix(l.sideID, "list:") {
			h = append(h, hint{"r", "yeniden adlandır"}, hint{"d", "sil"})
		}
		return append(h, hint{"esc", "geri"})
	}
	return []hint{{"enter", "aç"}, {"a", "düzenle"}, {"s", "durum"}, {"1-0", "puan"}, {"f", "favori"}, {"v", "görünüm"}, {"o", "sırala"}, {"/", "ara"}, {"d", "çıkar"}}
}

func (l *library) shelves(a *App) []shelf {
	items := a.lib.All()
	count := func(f func(store.Item) bool) int {
		n := 0
		for _, it := range items {
			if f(it) {
				n++
			}
		}
		return n
	}
	out := []shelf{{id: "all", label: "tümü", icon: ic("library"), color: cText, count: len(items)}}
	for _, s := range store.Statuses {
		s := s
		out = append(out, shelf{id: s, label: s, icon: statusIcon(s), color: statusColor(s), count: count(func(it store.Item) bool { return it.Status == s })})
	}
	for _, n := range a.lib.ListNames() {
		n := n
		c := cAccent2
		if strings.EqualFold(n, "favoriler") {
			c = cAccent
		}
		out = append(out, shelf{id: "list:" + n, label: n, icon: listIcon(n), color: c, count: count(func(it store.Item) bool { return it.InList(n) })})
	}
	return append(out, shelf{id: "new", label: "yeni liste", icon: "+", color: cMuted})
}

func (l *library) curShelf(a *App) shelf {
	ss := l.shelves(a)
	l.side = clamp(l.side, 0, len(ss)-1)
	return ss[l.side]
}

// items on the current shelf, filtered and sorted.
func (l *library) items(a *App) []store.Item {
	sh := l.curShelf(a)
	q := CleanQuery(l.filter.Value())
	var out []store.Item
	for _, it := range a.lib.All() {
		switch {
		case sh.id == "all":
		case strings.HasPrefix(sh.id, "list:"):
			if !it.InList(strings.TrimPrefix(sh.id, "list:")) {
				continue
			}
		case it.Status != sh.id:
			continue
		}
		if q != "" {
			if sc, _ := fuzzy(q, it.Title); sc <= 0 {
				continue
			}
		}
		out = append(out, it)
	}
	switch l.sortBy {
	case 1:
		sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	case 2:
		sort.SliceStable(out, func(i, j int) bool {
			return string(foldRunes(out[i].Title)) < string(foldRunes(out[j].Title))
		})
	case 3:
		sort.SliceStable(out, func(i, j int) bool { return out[i].AddedAt.After(out[j].AddedAt) })
	}
	return out
}

// prefetch posters (and details for items saved without one).
func (l *library) prefetch(a *App) tea.Cmd {
	if !a.cfg.Posters {
		return nil
	}
	var cmds []tea.Cmd
	for i, it := range l.items(a) {
		if i > 40 {
			break
		}
		if it.Poster != "" {
			cmds = append(cmds, a.posters.want(it.Poster))
		} else {
			cmds = append(cmds, a.fetchDetails(it.Anime()))
		}
	}
	return tea.Batch(cmds...)
}

func (l *library) animating(a *App) bool {
	return a.top() == screen(l) && (revealing(l.at, 20, a.motion()) || l.scoreT.active())
}

func (l *library) gridCols(w int) int { return max(1, w/18) }

func (l *library) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case detailsMsg:
		if d, ok := a.details[m.key]; ok && d.ID != "" {
			if it, ok := a.lib.Get(m.key); ok && it.Poster == "" && d.Poster != "" {
				a.libSet(d, nil) // remember the poster for next time
				return a.posters.want(d.Poster)
			}
		}
	case tea.MouseMsg:
		if m.Action == tea.MouseActionPress && (m.Button == tea.MouseButtonWheelUp || m.Button == tea.MouseButtonWheelDown) {
			d := 1
			if m.Button == tea.MouseButtonWheelUp {
				d = -1
			}
			if l.focus == 0 {
				l.side = clamp(l.side+d, 0, len(l.shelves(a))-1)
			} else {
				l.cur = clamp(l.cur+d, 0, len(l.items(a))-1)
			}
		}
	case tea.KeyMsg:
		return l.key(a, m)
	}
	return nil
}

func (l *library) key(a *App, k tea.KeyMsg) tea.Cmd {
	if l.filtering {
		switch k.String() {
		case "esc":
			l.filtering = false
			l.filter.Set("")
		case "enter", "down":
			l.filtering = false
		default:
			l.filter.handle(k)
			l.cur, l.off = 0, 0
		}
		return nil
	}
	if l.naming {
		switch k.String() {
		case "esc":
			l.naming, l.renaming = false, ""
		case "enter":
			name := CleanQuery(l.nameIn.Value())
			ok := false
			if l.renaming != "" {
				ok = a.lib.RenameList(l.renaming, name)
			} else {
				ok = a.lib.AddList(name)
			}
			if ok {
				a.notify("liste: "+name, "ok")
				a.flash()
				l.selectShelf(a, "list:"+name)
				l.focus = 0
			} else {
				a.notify("bu adla liste var ya da ad boş", "warn")
			}
			l.naming, l.renaming = false, ""
			l.nameIn.Set("")
		default:
			l.nameIn.handle(k)
		}
		return nil
	}
	ss := l.shelves(a)
	sh := l.curShelf(a)
	items := l.items(a)
	l.cur = clamp(l.cur, 0, len(items)-1)
	switch k.String() {
	case "esc", "q", "backspace":
		if l.filter.Value() != "" {
			l.filter.Set("")
			return nil
		}
		if l.focus == 1 {
			l.focus = 0
			return nil
		}
		a.pop()
		return nil
	case "/":
		l.filtering, l.focus = true, 1
		return nil
	case "tab":
		l.focus = 1 - l.focus
		return nil
	case "v":
		l.grid = !l.grid
		l.at = time.Now()
		a.gfx.invalidate()
		return l.prefetch(a)
	case "m":
		return a.push(newMalScreen(a))
	case "o":
		l.sortBy = (l.sortBy + 1) % len(sortNames)
		l.at = time.Now()
		a.notify("sıralama: "+sortNames[l.sortBy], "ok")
		return a.tick()
	}
	if l.focus == 0 {
		switch k.String() {
		case "up", "k":
			l.side = max(0, l.side-1)
			l.cur, l.off, l.at = 0, 0, time.Now()
			return tea.Batch(l.prefetch(a), a.tick())
		case "down", "j":
			l.side = clamp(l.side+1, 0, len(ss)-1)
			l.cur, l.off, l.at = 0, 0, time.Now()
			return tea.Batch(l.prefetch(a), a.tick())
		case "right", "l", "enter", " ":
			if sh.id == "new" {
				l.naming = true
				return nil
			}
			l.focus = 1
		case "n":
			l.naming = true
		case "r":
			if strings.HasPrefix(sh.id, "list:") {
				l.renaming = strings.TrimPrefix(sh.id, "list:")
				l.nameIn.Set(l.renaming)
				l.naming = true
			}
		case "d", "delete":
			if strings.HasPrefix(sh.id, "list:") {
				name := strings.TrimPrefix(sh.id, "list:")
				var members []store.Item
				for _, it := range a.lib.All() {
					if it.InList(name) {
						members = append(members, it)
					}
				}
				a.lib.DeleteList(name)
				l.side = max(0, l.side-1)
				a.remember("“"+name+"” listesi silindi", func(a *App) {
					a.lib.AddList(name)
					for _, it := range members {
						a.lib.ToggleList(a.libAnime(it.Anime()), name)
					}
				})
				return a.tick()
			}
		}
		return nil
	}
	// items
	cols := 1
	if l.grid {
		cols = l.gridCols(l.lastW)
	}
	switch k.String() {
	case "up", "k":
		if l.cur-cols < 0 {
			if l.cur == 0 {
				l.filtering = false
			}
			l.cur = 0
		} else {
			l.cur -= cols
		}
	case "down", "j":
		l.cur = clamp(l.cur+cols, 0, len(items)-1)
	case "left", "h":
		if l.grid && l.cur%cols > 0 {
			l.cur--
		} else {
			l.focus = 0
		}
	case "right", "l":
		if l.grid {
			l.cur = clamp(l.cur+1, 0, len(items)-1)
		}
	case "home", "g":
		l.cur = 0
	case "end", "G":
		l.cur = max(0, len(items)-1)
	case "pgdown":
		l.cur = clamp(l.cur+10, 0, len(items)-1)
	case "pgup":
		l.cur = max(0, l.cur-10)
	}
	if l.cur >= len(items) {
		return nil
	}
	it := items[l.cur]
	an := it.Anime()
	switch k.String() {
	case "enter":
		a.flash()
		return a.openItem(it)
	case "a", "+", "e":
		a.openPopup(an)
		return a.tick()
	case "s", "S":
		idx := 0
		for i, s := range store.Statuses {
			if s == it.Status {
				idx = i
			}
		}
		d := 1
		if k.String() == "S" {
			d = len(store.Statuses) - 1
		}
		next := store.Statuses[(idx+d)%len(store.Statuses)]
		before := it
		a.libSet(an, func(x *store.Item) { x.Status = next })
		a.flash()
		if next == store.Completed {
			a.celebrate(60)
		}
		// one key changes your MAL list too: always reversible
		a.remember(it.Title+" → "+next, func(a *App) { a.lib.Restore(before) })
		return a.tick()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "-":
		v := 0
		if k.String() != "-" {
			v = int(k.String()[0] - '0')
			if v == 0 {
				v = 10
			}
		}
		a.libSet(an, func(x *store.Item) { x.Score = v })
		l.scoreKey = it.Key()
		if a.motion() {
			l.scoreT = newTween(400 * time.Millisecond)
		}
		if v == 10 {
			a.celebrate(40)
		}
		return a.tick()
	case "f":
		if a.lib.ToggleList(a.libAnime(an), "favoriler") {
			a.notify(ic("heart")+" favorilere eklendi", "ok")
			a.celebrate(18)
		} else {
			a.notify("favorilerden çıkarıldı", "ok")
		}
		a.lib.AddList("favoriler")
		return a.tick()
	case "d", "delete", "x":
		a.lib.Remove(it.Key())
		saved := it
		a.remember(it.Title+" kitaplıktan çıkarıldı", func(a *App) { a.lib.Restore(saved) })
		return a.tick()
	}
	return nil
}

// ---- view ----

func (l *library) view(a *App, w, h int) string {
	pad := 2
	if a.cfg.Compact {
		pad = 1
	}
	inner := w - pad*2
	items := l.items(a)
	// header: title + the numbers that make a shelf feel lived in
	var out []string
	all := a.lib.All()
	scored, sum := 0, 0
	for _, it := range all {
		if it.Score > 0 {
			scored++
			sum += it.Score
		}
	}
	eps, secs := a.hist.Totals()
	stats := []string{
		stBold.Render(fmt.Sprint(len(all))) + stMuted.Render(" anime"),
		stBold.Render(fmt.Sprint(eps)) + stMuted.Render(" bölüm"),
	}
	if secs > 0 {
		stats = append(stats, stBold.Render(fmt.Sprintf("%.0f", secs/3600))+stMuted.Render(" saat"))
	}
	if scored > 0 {
		stats = append(stats, fg(cWarn).Render(ic("star"))+" "+stBold.Render(fmt.Sprintf("%.1f", float64(sum)/float64(scored)))+stMuted.Render(" ort."))
	}
	if st := a.hist.Streak(); st > 0 {
		stats = append(stats, fg(cAccent).Render(ic("fire"))+" "+stBold.Render(fmt.Sprint(st))+stMuted.Render(" gün seri"))
	}
	title := gradient("kitaplık", brand, true) + "   " + a.malLine()
	// numbers drop from the right until the line fits (never an ellipsis mid-way)
	for len(stats) > 0 && lipgloss.Width(title)+lipgloss.Width(strings.Join(stats, stFaint.Render("  ·  ")))+4 > inner {
		stats = stats[:len(stats)-1]
	}
	out = append(out, "", row2(title, strings.Join(stats, stFaint.Render("  ·  ")), inner), "")
	bodyH := h - len(out)
	sideW := 26
	detailW := 0
	if inner >= 120 {
		detailW = min(44, inner/3)
	}
	mainW := inner - sideW - 1
	if detailW > 0 {
		mainW -= detailW + 1
	}
	l.lastW = mainW - 4
	acc := cAccent
	var sel *store.Item
	if l.cur < len(items) {
		sel = &items[l.cur]
		acc = a.posters.accent(sel.Poster, cAccent).hex()
	}
	cols := [][]string{panel(ic("library")+" raflar", l.sidebar(a, sideW-4, bodyH-2), sideW, bodyH, l.focus == 0, cAccent)}
	sh := l.curShelf(a)
	l.sideID = sh.id
	mt := sh.label + fmt.Sprintf(" · %d", len(items))
	if l.filter.Value() != "" || l.filtering {
		mt += " · “" + l.filter.Value() + "”"
	}
	mainBody := l.mainPane(a, items, mainW-4, bodyH-2, acc)
	cols = append(cols, panel(mt, mainBody, mainW, bodyH, l.focus == 1, acc, sortNames[l.sortBy]))
	if detailW > 0 {
		var body []string
		if sel != nil {
			body = l.card(a, *sel, detailW-4, bodyH-2)
		} else {
			body = []string{stMuted.Render("seçili bir şey yok")}
		}
		cols = append(cols, panel("ayrıntı", body, detailW, bodyH, false, acc))
	}
	lines := hjoin(1, cols...)
	for _, ln := range lines {
		out = append(out, strings.Repeat(" ", pad)+ln)
	}
	return strings.Join(out, "\n")
}

func (l *library) sidebar(a *App, w, h int) []string {
	ss := l.shelves(a)
	sel := l.sideGl.row(a, l.side)
	var out []string
	for i, s := range ss {
		if i == 1+len(store.Statuses) {
			out = append(out, "", stFaint.Render("LİSTELER"))
		}
		if s.id == "new" && l.naming && l.renaming == "" {
			out = append(out, l.nameIn.view(w, true, a.frame)...)
			continue
		}
		if l.naming && l.renaming != "" && s.id == "list:"+l.renaming {
			out = append(out, l.nameIn.view(w, true, a.frame)...)
			continue
		}
		isSel := i == sel
		bg, bar := "", sp("  ", cText)
		tc := cSub
		if i == l.side {
			tc = cText
		}
		if isSel {
			bg = cBand
			if l.focus == 0 {
				bar = sp("▌ ", cAccent)
			} else {
				bar = sp("▏ ", cMuted)
			}
		}
		right := []span{}
		if s.id != "new" {
			right = append(right, sp(fmt.Sprint(s.count)+" ", cMuted))
		}
		out = append(out, row(w, bg, []span{bar, sp(s.icon+" ", s.color), span{t: s.label, c: tc, bold: i == l.side}}, right))
	}
	return block(out, w, h)
}

func (l *library) mainPane(a *App, items []store.Item, w, h int, acc string) []string {
	var out []string
	if l.filtering {
		out = append(out, l.filter.view(min(w, 48), true, a.frame)...)
		h -= 3
	}
	if len(items) == 0 {
		return append(out, l.empty(a, w)...)
	}
	l.cur = clamp(l.cur, 0, len(items)-1)
	if l.grid {
		return append(out, l.wall(a, items, w, h, acc)...)
	}
	sel := l.curGl.row(a, l.cur)
	l.off = scroll(l.cur, l.off, h, len(items))
	for i := l.off; i < len(items) && i < l.off+h; i++ {
		it := items[i]
		rv := reveal(l.at, i-l.off, a.motion())
		if rv <= 0 {
			out = append(out, "")
			continue
		}
		isSel := i == sel && l.focus == 1
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		if isSel {
			bg, bar, tc = cBand, sp("▌ ", acc), acc
		}
		left := []span{bar, sp(statusIcon(it.Status)+" ", statusColor(it.Status)), span{t: it.Title, c: tc, bold: isSel}}
		var right []span
		if e, ok := a.hist.Get(it.Key()); ok && e.Season > 0 && w > 60 {
			n := 0
			for _, d := range e.Watched {
				if d {
					n++
				}
			}
			if n > 0 {
				right = append(right, sp(fmt.Sprintf("%d bölüm  ", n), cMuted))
			}
		}
		if it.InList("favoriler") {
			right = append(right, sp(ic("heart")+" ", cAccent))
		}
		if it.Score > 0 {
			c := cWarn
			if l.scoreKey == it.Key() && l.scoreT.active() {
				c = mix(hex("#FFFFFF"), hex(cWarn), easeOutCubic(l.scoreT.p())).hex()
			}
			right = append(right, sp(ic("star")+" ", c), spb(fmt.Sprintf("%2d ", it.Score), cText))
		} else {
			right = append(right, sp("·  ", cFaint), sp(" ", cText))
		}
		line := row(w, bg, left, right)
		if isSel {
			line = anchor() + line
		}
		out = append(out, line)
	}
	return out
}

// wall: the poster grid. The selected card gets an accent frame.
func (l *library) wall(a *App, items []store.Item, w, h int, acc string) []string {
	cols := l.gridCols(w)
	cw := w / cols
	pw := cw - 4
	ph := max(4, pw*3/4)
	cardH := ph + 4
	rows := max(1, h/cardH)
	rowOf := l.cur / cols
	l.off = scroll(rowOf, l.off, rows, (len(items)+cols-1)/cols)
	var out []string
	for r := l.off; r < l.off+rows; r++ {
		var cards [][]string
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(items) {
				cards = append(cards, block(nil, cw, cardH))
				continue
			}
			it := items[i]
			isSel := i == l.cur && l.focus == 1
			var body []string
			if a.cfg.Posters {
				body = append(body, a.posters.render(it.Poster, pw, ph, a.frame)...)
			} else {
				body = append(body, block([]string{"", center(stFaint.Render(statusIcon(it.Status)), pw)}, pw, ph)...)
			}
			tc := cSub
			if isSel {
				tc = acc
			}
			body = append(body, span{t: truncate(it.Title, pw), c: tc, bold: isSel}.style("").Render(truncate(it.Title, pw)))
			meta := fg(statusColor(it.Status)).Render(statusIcon(it.Status))
			if it.Score > 0 {
				meta += " " + fg(cWarn).Render(ic("star")) + stText.Render(fmt.Sprint(it.Score))
			}
			if it.InList("favoriler") {
				meta += " " + fg(cAccent).Render(ic("heart"))
			}
			body = append(body, meta)
			bc := cBg
			if isSel {
				bc = acc
			}
			bs := fg(bc)
			card := []string{bs.Render("╭" + strings.Repeat("─", pw+2) + "╮")}
			for j, ln := range body {
				pre := bs.Render("│ ")
				if j == 0 && isSel {
					pre = anchor() + pre
				}
				card = append(card, pre+fit(ln, pw)+bs.Render(" │"))
			}
			card = append(card, bs.Render("╰"+strings.Repeat("─", pw+2)+"╯"))
			cards = append(cards, block(card, cw, cardH))
		}
		out = append(out, hjoin(0, cards...)...)
	}
	return out
}

func (l *library) empty(a *App, w int) []string {
	sh := l.curShelf(a)
	art := []string{
		"   ╭──────╮   ",
		"   │ ◕  ◕ │   ",
		"   │  ──  │   ",
		"   ╰──────╯   ",
	}
	var out []string
	out = append(out, "")
	for _, s := range art {
		out = append(out, center(fg(mix(hex(cFaint), hex(cAccent), .5).hex()).Render(s), w))
	}
	msg := "Bu raf boş."
	tip := "Bir animenin sayfasında a tuşuyla kitaplığa ekle."
	switch {
	case l.filter.Value() != "":
		msg, tip = "Eşleşme yok.", "esc ile aramayı temizle."
	case sh.id == "all":
		msg = "Kitaplığın henüz boş."
		tip = "İzlediğin her anime buraya kendiliğinden gelir; a ile durum, puan ve liste seç."
	case sh.id == store.Completed:
		tip = "Bir seriyi bitirdiğinde burada konfetiyle karşılanır."
	case strings.HasPrefix(sh.id, "list:"):
		tip = "Anime sayfasında a → listeler bölümünden bu listeye ekle."
	}
	out = append(out, "", center(stSub.Render(msg), w))
	for _, t := range wrap(tip, w-8) {
		out = append(out, center(stMuted.Render(t), w))
	}
	return out
}

// card: the right-hand detail for a library item.
func (l *library) card(a *App, it store.Item, w, h int) []string {
	acc := a.posters.accent(it.Poster, cAccent).hex()
	var out []string
	if a.cfg.Posters && h >= 24 {
		pw := min(w, 24)
		ph := pw * 3 / 4
		ind := strings.Repeat(" ", (w-pw)/2)
		for _, ln := range a.posters.render(it.Poster, pw, ph, a.frame) {
			out = append(out, ind+ln)
		}
		out = append(out, "")
	}
	for i, ln := range wrap(it.Title, w) {
		if i > 2 {
			break
		}
		out = append(out, gradient(ln, []string{acc, cAccent2}, true))
	}
	var meta []string
	if it.Year > 0 {
		meta = append(meta, fmt.Sprint(it.Year))
	}
	if it.Kind != "" {
		meta = append(meta, it.Kind)
	}
	if p := source.Get(it.Source); p != nil {
		meta = append(meta, p.Info().Name)
	}
	out = append(out, stMuted.Render(strings.Join(meta, " · ")), "")
	out = append(out, pill(statusIcon(it.Status)+" "+strings.ToUpperSpecial(unicode.TurkishCase, it.Status), statusColor(it.Status)))
	out = append(out, "")
	pop := 1.0
	if l.scoreKey == it.Key() {
		pop = l.scoreT.p()
	}
	sc := stMuted.Render("puansız")
	if it.Score > 0 {
		sc = stBold.Render(fmt.Sprintf("%d", it.Score)) + stMuted.Render("/10")
	}
	out = append(out, scoreBar(it.Score, acc, pop)+"  "+sc)
	if e, ok := a.hist.Get(it.Key()); ok && e.Season > 0 {
		n := 0
		for _, d := range e.Watched {
			if d {
				n++
			}
		}
		out = append(out, "", stSub.Render(fmt.Sprintf("son: S%d · B%d", e.Season, e.Episode))+stMuted.Render(fmt.Sprintf("  ·  %d bölüm izlendi", n)))
		if it.EpCount > 0 {
			out = append(out, progressBar(w, float64(n)/float64(it.EpCount), []string{acc, cAccent2}))
		}
	}
	if len(it.Lists) > 0 {
		var chips []string
		for _, ls := range it.Lists {
			chips = append(chips, fg(cSub).Background(colorOf(cSurface)).Render(" "+listIcon(ls)+" "+ls+" "))
		}
		out = append(out, "")
		out = append(out, wrapChips(chips, w)...)
	}
	if g := genreLine(it.Genres, w); g != "" {
		out = append(out, "", g)
	}
	out = append(out, "")
	out = append(out, stMuted.Render("eklendi "+agoLong(it.AddedAt)))
	if !it.FinishedAt.IsZero() {
		out = append(out, stMuted.Render("bitirdin "+agoLong(it.FinishedAt)))
	}
	return block(out, w, h)
}

func agoLong(t time.Time) string {
	s := ago(t)
	if s == "yeni" || s == "şimdi" {
		return s
	}
	return s + " önce"
}

func wrapChips(chips []string, w int) []string {
	var out []string
	line, used := "", 0
	for _, c := range chips {
		cw := lipgloss.Width(c) + 1
		if used+cw > w && line != "" {
			out = append(out, line)
			line, used = "", 0
		}
		line += c + " "
		used += cw
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// panel frames body as a btop-style box: rounded border, the title set into
// the top edge, an optional right-hand tag; the focused panel's border
// takes the accent colour.
func panel(title string, body []string, w, h int, focused bool, acc string, tag ...string) []string {
	bc := cFaint
	tc := stMuted
	if focused {
		bc = mix(hex(cFaint), hex(acc), .8).hex()
		tc = stText.Bold(true)
	}
	bs := fg(bc)
	t := tc.Render(strings.ToUpperSpecial(unicode.TurkishCase, title))
	right := ""
	if len(tag) > 0 && tag[0] != "" {
		right = stMuted.Render(" " + tag[0] + " ")
	}
	fillW := w - 5 - lipgloss.Width(t) - lipgloss.Width(right) - 1
	if fillW < 0 {
		t = truncate(t, max(0, lipgloss.Width(t)+fillW))
		fillW = 0
	}
	out := []string{bs.Render("╭─ ") + t + bs.Render(" "+strings.Repeat("─", fillW)) + right + bs.Render("─╮")}
	inner := w - 4
	b := block(body, inner, h-2)
	for _, ln := range b {
		out = append(out, bs.Render("│ ")+ln+bs.Render(" │"))
	}
	out = append(out, bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return out
}
