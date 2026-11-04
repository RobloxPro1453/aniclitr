package ui

// discmenu.go — Keşfet, step one: six sections as a menu with a live peek
// at the selected one. A section opens as a page of its own; esc comes
// back here. (It used to be six tabs and a detail box on one screen.)

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
)

// capTR upper-cases the first letter the Turkish way ("işlemler" → "İşlemler").
func capTR(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return strings.ToUpperSpecial(unicode.TurkishCase, string(r)) + s[n:]
}

var discoverIcons = []string{"calendar", "clock", "tv", "list", "heart", "spark"}

func (d *discover) menuItems(a *App) []mItem {
	var out []mItem
	for i, t := range discoverTabs {
		meta := ""
		switch i {
		case tCal:
			if d.schedOK && d.schedErr == nil {
				meta = fmt.Sprintf("%d yayın", len(d.sched))
			}
		case tSeason:
			meta = fmt.Sprintf("%s %d", anilist.SeasonTR(d.sName), d.sYear)
		case tRecs:
			if d.recsOK && d.recsErr == nil {
				meta = fmt.Sprintf("%d öneri", len(d.recs))
			}
		}
		out = append(out, mItem{id: t, icon: ic(discoverIcons[i]), label: capTR(t), desc: discoverDesc[i], meta: meta})
	}
	return out
}

func (d *discover) menuKey(a *App, k tea.KeyMsg) tea.Cmd {
	n := len(discoverTabs)
	move := func(t int) tea.Cmd {
		d.tab = clamp(t, 0, n-1)
		if d.tab == tRandom {
			return nil // the dice roll when you open it, not while browsing
		}
		return tea.Batch(d.load(a), a.tick())
	}
	switch k.String() {
	case "esc", "q", "backspace":
		a.pop()
		return nil
	case "up", "k", "shift+tab":
		return move(d.tab - 1)
	case "down", "j", "tab":
		return move(d.tab + 1)
	case "home", "g":
		return move(0)
	case "end", "G":
		return move(n - 1)
	case "1", "2", "3", "4", "5", "6":
		d.tab = int(k.String()[0] - '1')
		fallthrough
	case "enter", "right", "l":
		a.flash()
		d.menu = false
		d.gpick = d.tab == tGenre
		d.at = time.Now()
		a.slide(1)
		return tea.Batch(d.load(a), d.wantPosters(a), a.tick())
	}
	return nil
}

func (d *discover) menuView(a *App, w, h int) string {
	out := pageHead("Keşfet", "Yayın takvimi, sezon, türler ve öneriler. Bir bölüm seçin.", "AniList", w, h)
	bodyH := h - len(out)
	inner := w - kitPad*2
	lw, rw := split(inner)
	if rw > 0 {
		lw = min(lw, 72)
		rw = inner - lw - kitGap
	}
	items := d.menuItems(a)
	band := d.mgl.row(a, d.tab)
	left := pane{title: "bölümler", focused: true, draw: func(w, h int) []string { return menuRows(items, band, w) }}
	right := pane{title: discoverTabs[d.tab], draw: func(w, h int) []string { return d.peek(a, w, h) }}
	out = append(out, indent(panes(left, lw, right, rw, bodyH))...)
	return strings.Join(out, "\n")
}

// peek: the first few things the selected section holds.
func (d *discover) peek(a *App, w, h int) []string {
	var out []string
	loading := func(ok bool, err error) []string {
		switch {
		case err != nil:
			return append(out, wrapMuted("Alınamadı: "+err.Error(), w)...)
		case !ok:
			return append(out, stMuted.Render("yükleniyor"+strings.Repeat(".", a.frame/8%4)))
		}
		return nil
	}
	switch d.tab {
	case tTonight:
		return append(out, wrapMuted("Kitaplıktaki ve yarım kalan animelerden ayrılan süreye sığanları sıralar. Süre + ve - tuşlarıyla ayarlanır.", w)...)
	case tRandom:
		return append(out, wrapMuted("AniList'ten rastgele bir anime seçer. Yeni bir seçim için space tuşuna basın.", w)...)
	case tGenre:
		return append(out, wrapMuted("Bir tür seçildiğinde o türün en iyi, en popüler ya da az bilinen animeleri listelenir.", w)...)
	case tCal:
		if l := loading(d.schedOK, d.schedErr); l != nil {
			return l
		}
	case tSeason:
		if l := loading(d.seasonOK, d.seasonErr); l != nil {
			return l
		}
	case tRecs:
		if l := loading(d.recsOK, d.recsErr); l != nil {
			return l
		}
	}
	for _, r := range d.rows(a) {
		if len(out) >= h-1 {
			break
		}
		if r.header != "" {
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, stSub.Render(r.header))
			continue
		}
		if r.m == nil {
			continue
		}
		right := []span{}
		if r.air != nil {
			right = append(right, sp(r.air.At.Format("15:04"), cMuted))
		} else if r.m.Score > 0 {
			right = append(right, sp(fmt.Sprintf("%d", r.m.Score), cMuted))
		}
		out = append(out, row(w, "", []span{sp(r.m.Name(), cText)}, right))
	}
	return out
}

// ---- türler: pick the genre first, then see its list ----

func (d *discover) genrePickKey(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	n := len(anilist.Genres)
	switch k.String() {
	case "up", "k":
		d.genre = (d.genre + n - 1) % n
	case "down", "j":
		d.genre = (d.genre + 1) % n
	case "home", "g":
		d.genre = 0
	case "end", "G":
		d.genre = n - 1
	case "m":
		modes := []string{anilist.ModeTop, anilist.ModePopular, anilist.ModeCult}
		d.gmode = modes[(indexOf(modes, d.gmode)+1)%len(modes)]
	case "enter", "right", "l":
		a.flash()
		d.gpick = false
		d.cur[tGenre] = 0
		d.at = time.Now()
		return tea.Batch(d.loadGenre(a), a.tick()), true
	default:
		return nil, false
	}
	return a.tick(), true
}

var genreModeDesc = map[string]string{
	anilist.ModeTop:     "puanı en yüksek olanlar",
	anilist.ModePopular: "en çok izlenenler",
	anilist.ModeCult:    "az bilinen ama çok sevilenler",
}

func (d *discover) genrePickView(a *App, lw, rw, h int) []string {
	var items []mItem
	for _, g := range anilist.Genres {
		items = append(items, mItem{id: g[0], label: g[1], desc: g[0]})
	}
	band := d.mgl.row(a, d.genre)
	left := pane{title: "tür seç", focused: true, draw: func(w, hh int) []string {
		rows := menuRows(items, band, w)
		off := scroll(d.genre, 0, hh, len(rows))
		return rows[off:min(len(rows), off+hh)]
	}}
	right := pane{title: "sıralama", tag: "m ile değiştir", draw: func(w, hh int) []string {
		var out []string
		for _, m := range []string{anilist.ModeTop, anilist.ModePopular, anilist.ModeCult} {
			if m == d.gmode {
				out = append(out, row(w, cBand, []span{sp("▌ ", cAccent), spb(m, cText)}, []span{sp(genreModeDesc[m]+" ", cSub)}))
			} else {
				out = append(out, row(w, "", []span{sp("  ", cText), sp(m, cSub)}, []span{sp(genreModeDesc[m]+" ", cMuted)}))
			}
		}
		return append(out, "", stMuted.Render("Türü açmak için enter. Listede space"), stMuted.Render("o türden rastgele bir anime seçer."))
	}}
	return panes(left, lw, right, rw, h)
}
