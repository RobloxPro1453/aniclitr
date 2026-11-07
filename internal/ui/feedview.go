package ui

// feedview.go — "Yeni bölümler": the live feed on a page of its own (it
// used to crowd the home screen). The data stays on home, which loads it
// at start; this page only reads it.

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
)

type feedScreen struct {
	cur, off int
	gl       glide
	mine     bool // only shows you follow
}

func newFeedScreen() *feedScreen { return &feedScreen{} }

func (f *feedScreen) crumb() string { return "yeni bölümler" }

func (f *feedScreen) hints() []hint {
	return []hint{{"enter", "aç"}, {"a", "kitaplığa ekle"}, {"f", "yalnızca takip edilenler"}, {"r", "yenile"}, {"esc", "geri"}}
}

func (f *feedScreen) animating(a *App) bool {
	h := a.homeOf()
	return h != nil && (!h.feedOK || revealing(h.feedAt, 20, a.motion()))
}

// items is the feed as this page shows it.
func (f *feedScreen) items(a *App) []source.FeedItem {
	h := a.homeOf()
	if h == nil {
		return nil
	}
	if !f.mine {
		return h.feed
	}
	var out []source.FeedItem
	for _, it := range h.feed {
		if _, ok := a.libItem(it.Anime); ok {
			out = append(out, it)
		}
	}
	return out
}

func (f *feedScreen) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	items := f.items(a)
	switch k.String() {
	case "esc", "q", "backspace":
		a.pop()
	case "up", "k":
		f.cur = max(0, f.cur-1)
	case "down", "j":
		f.cur = clamp(f.cur+1, 0, len(items)-1)
	case "pgup":
		f.cur = max(0, f.cur-10)
	case "pgdown":
		f.cur = clamp(f.cur+10, 0, len(items)-1)
	case "home", "g":
		f.cur = 0
	case "end", "G":
		f.cur = max(0, len(items)-1)
	case "f":
		f.mine, f.cur = !f.mine, 0
	case "r":
		if h := a.homeOf(); h != nil {
			h.feedOK = false
		}
		return tea.Batch(a.loadFeed(), a.tick())
	case "a", "+":
		if f.cur < len(items) {
			a.openPopup(items[f.cur].Anime)
			return a.tick()
		}
	case "enter", "right", "l":
		if f.cur < len(items) {
			a.flash()
			it := items[f.cur]
			sc := newAnime(it.Anime, nil, it.Season, it.Episode)
			return tea.Batch(a.push(sc), sc.start(a))
		}
	}
	if f.cur < len(items) && a.cfg.Posters {
		return a.posters.want(items[f.cur].Anime.Poster)
	}
	return nil
}

func (f *feedScreen) view(a *App, w, ht int) string {
	h := a.homeOf()
	items := f.items(a)
	sub := "Tüm kaynaklarda son yayınlanan bölümler · ◆ takip ettikleriniz"
	if f.mine {
		sub = "Yalnızca takip ettiğiniz animeler"
	}
	meta := ""
	if len(items) > 0 {
		meta = fmt.Sprintf("%d bölüm", len(items))
	}
	out := pageHead("Yeni bölümler", sub, meta, w, ht)
	bodyH := ht - len(out)
	inner := w - kitPad*2
	lw, rw := split(inner)
	list := func(lw, bodyH int) []string {
		switch {
		case h == nil || (!h.feedOK && len(h.feed) == 0):
			var left []string
			for i := 0; i < min(8, bodyH); i++ { // skeleton rows breathing while loading
				t := (math.Sin(float64(a.frame)/6+float64(i)*.7) + 1) / 2
				c := mix(hex(cSurface), hex(cShade), t).hex()
				left = append(left, fg(c).Render(strings.Repeat("▬", 5))+"  "+fg(c).Render(strings.Repeat("▬", 14+int(hash(i)*16))))
			}
			return left
		case h.feedErr != nil && len(h.feed) == 0:
			return emptyState("Akış alınamadı", h.feedErr.Error()+" — yeniden denemek için r.", lw, bodyH)
		case len(items) == 0:
			return emptyState("Gösterilecek bölüm yok", "Takip ettiğiniz animelerin yeni bölümleri burada listelenir. Tümünü görmek için f tuşuna basın.", lw, bodyH)
		}
		f.cur = clamp(f.cur, 0, len(items)-1)
		f.off = scroll(f.cur, f.off, bodyH, len(items))
		return feedRows(a, items, f.off, f.gl.row(a, f.cur), h.feedAt, lw, bodyH)
	}
	var detail pane
	if f.cur < len(items) {
		it := items[clamp(f.cur, 0, len(items)-1)]
		detail = pane{title: "seçili bölüm", acc: a.posters.accent(it.Anime.Poster, cAccent).hex(), draw: func(w, h int) []string { return feedCard(a, it, w, h) }}
	}
	tag := ""
	if f.mine {
		tag = "takip edilenler"
	}
	out = append(out, indent(panes(pane{title: "bölümler", tag: tag, focused: true, draw: list}, lw, detail, rw, bodyH))...)
	return strings.Join(out, "\n")
}

// feedRows: when · ◆ (yours) · title … episode · fansub.
func feedRows(a *App, items []source.FeedItem, off, band int, at time.Time, w, h int) []string {
	var out []string
	for i := off; i < len(items) && i < off+h; i++ {
		it := items[i]
		sel := i == band
		rv := reveal(at, i-off, a.motion())
		if rv <= 0 {
			out = append(out, "")
			continue
		}
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		if sel {
			bg, bar, tc = cBand, sp("▌ ", cAccent), cText
		}
		mark := sp("  ", cText)
		if _, mine := a.libItem(it.Anime); mine {
			mark = sp("◆ ", cAccent2)
		}
		wc := cMuted
		if time.Since(it.Released) < 12*time.Hour {
			wc = cAccent2
		}
		ep := source.EpNum(it.Episode) + ". bölüm"
		if it.Season > 1 {
			ep = fmt.Sprintf("S%d · %s. bölüm", it.Season, source.EpNum(it.Episode))
		}
		right := []span{sp(fmt.Sprintf("%-14s", ep), cSub)}
		if fs := shortFansub(it.Fansub); w > 76 && fs != "" && fs != "Bilinmeyen" && !strings.Contains(fs, "www.") && !strings.Contains(fs, "://") {
			right = append(right, sp(fmt.Sprintf("%-13s", truncate(fs, 12)), cMuted))
		}
		line := row(w, bg, []span{bar, sp(fmt.Sprintf("%-7s", ago(it.Released)), wc), mark, span{t: it.Anime.Title, c: tc, bold: sel}}, right)
		if sel {
			line = anchor() + line
		}
		out = append(out, line)
	}
	return out
}

// feedCard is the detail column: cover, title, the episode and who
// translated it.
func feedCard(a *App, it source.FeedItem, w, h int) []string {
	var out []string
	an := it.Anime
	if a.cfg.Posters && an.Poster != "" {
		ph := min(h-9, 16)
		pw := min(w, ph*4/3)
		if ph >= 6 {
			in := strings.Repeat(" ", max(0, (w-pw)/2))
			for _, l := range a.posters.render(an.Poster, pw, pw*3/4, a.frame) {
				out = append(out, in+l)
			}
			out = append(out, "")
		}
	}
	acc := a.posters.accent(an.Poster, cAccent)
	for i, l := range wrap(an.Title, w) {
		if i > 1 {
			break
		}
		out = append(out, gradient(l, []string{acc.hex(), cAccent2}, true))
	}
	ep := source.EpNum(it.Episode) + ". bölüm"
	if it.Season > 1 {
		ep = fmt.Sprintf("Sezon %d · %s. bölüm", it.Season, source.EpNum(it.Episode))
	}
	out = append(out, stText.Render(ep), stMuted.Render(agoLong(it.Released)))
	if it.Fansub != "" && it.Fansub != "Bilinmeyen" {
		out = append(out, stMuted.Render(truncate(shortFansub(it.Fansub), w)))
	}
	if _, mine := a.libItem(an); mine {
		out = append(out, "", fg(cAccent2).Render("◆ takip ediliyor"))
	}
	return out
}
