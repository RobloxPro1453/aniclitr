package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
)

// The offline library: everything you downloaded, as a shelf of covers
// (the cover is saved next to the videos, so it works without internet).
// enter opens an anime's downloaded episodes, enter again plays one.

type dlShow struct {
	key    string
	title  string
	poster string // local cover if present, else the remote URL
	eps    []download.Job
	bytes  int64
	latest int64
}

// offlineShelf groups finished downloads by anime, newest first.
func offlineShelf(a *App) []dlShow {
	by := map[string]*dlShow{}
	for _, j := range a.dl.Jobs() {
		if j.State != download.Done {
			continue
		}
		if _, err := os.Stat(j.Path); err != nil {
			continue
		}
		s, ok := by[j.Key()]
		if !ok {
			s = &dlShow{key: j.Key(), title: j.Title, poster: j.Poster}
			if c := filepath.Join(filepath.Dir(j.Path), "kapak.jpg"); fileOK(c) {
				s.poster = "file://" + c
			}
			by[j.Key()] = s
		}
		s.eps = append(s.eps, j)
		s.bytes += j.Total
		if t := j.Finished.Unix(); t > s.latest {
			s.latest = t
		}
	}
	out := make([]dlShow, 0, len(by))
	for _, s := range by {
		sort.Slice(s.eps, func(i, j int) bool {
			if s.eps[i].Season != s.eps[j].Season {
				return s.eps[i].Season < s.eps[j].Season
			}
			return s.eps[i].Episode < s.eps[j].Episode
		})
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].latest > out[j].latest })
	return out
}

func fileOK(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Size() > 0
}

// libKey handles keys in the shelf view; ok=false falls through.
func (d *downloads) libKey(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	shows := offlineShelf(a)
	if d.openShow != "" {
		var show *dlShow
		for i := range shows {
			if shows[i].key == d.openShow {
				show = &shows[i]
			}
		}
		if show == nil {
			d.openShow = ""
			return nil, true
		}
		switch k.String() {
		case "esc", "backspace", "left", "h":
			d.openShow = ""
		case "up", "k":
			d.epCur = max(0, d.epCur-1)
		case "down", "j":
			d.epCur = clamp(d.epCur+1, 0, len(show.eps)-1)
		case "enter", "p", " ":
			if d.epCur < len(show.eps) {
				var eps []source.Episode
				for _, j := range show.eps {
					eps = append(eps, source.Episode{ID: j.EpID, Season: j.Season, Number: j.Episode, Title: j.EpTitle})
				}
				a.flash()
				return a.playLocal(show.eps[d.epCur], eps), true
			}
		case "x", "delete":
			if d.epCur < len(show.eps) {
				j := show.eps[d.epCur]
				a.dl.Remove(j.ID, true)
				a.notify(fmt.Sprintf("%s S%d·B%d diskten silindi", j.Title, j.Season, j.Episode), "ok")
			}
		case "X":
			n := a.dl.RemoveAnime(show.key)
			d.openShow = ""
			a.notify(fmt.Sprintf("%s (%d bölüm) tümüyle diskten silindi", show.title, n), "ok")
		default:
			return nil, false
		}
		return a.tick(), true
	}
	cols := max(1, d.lastW/20)
	switch k.String() {
	case "left", "h":
		d.libCur = max(0, d.libCur-1)
	case "right", "l":
		d.libCur = clamp(d.libCur+1, 0, len(shows)-1)
	case "up", "k":
		d.libCur = max(0, d.libCur-cols)
	case "down", "j":
		d.libCur = clamp(d.libCur+cols, 0, len(shows)-1)
	case "enter":
		if d.libCur < len(shows) {
			d.openShow, d.epCur = shows[d.libCur].key, 0
			a.flash()
		}
	case "x", "X", "delete":
		if d.libCur < len(shows) {
			sh := shows[d.libCur]
			if d.confirmShow == sh.key && time.Since(d.confirmAt) < 4*time.Second {
				n := a.dl.RemoveAnime(sh.key)
				d.confirmShow = ""
				a.notify(fmt.Sprintf("%s (%d bölüm) diskten silindi", sh.title, n), "ok")
			} else {
				d.confirmShow, d.confirmAt = sh.key, time.Now()
				a.notify(fmt.Sprintf("%s (%d bölüm) diskten silinsin mi? onay için tekrar x", sh.title, len(sh.eps)), "warn")
			}
		}
	default:
		return nil, false
	}
	return a.tick(), true
}

// libView draws the offline shelf (or one anime's episodes).
func (d *downloads) libView(a *App, w, h int) []string {
	shows := offlineShelf(a)
	if len(shows) == 0 {
		return []string{"", center(stSub.Render("İndirilmiş bir şey yok."), w), "",
			center(stMuted.Render("Bölüm listesinde d ile indir; burada kapaklarıyla birikir ve internetsiz izlenir."), w),
			center(stMuted.Render("v: indirme sırası"), w)}
	}
	if d.openShow != "" {
		for _, s := range shows {
			if s.key == d.openShow {
				return d.showView(a, s, w, h)
			}
		}
	}
	d.lastW = w
	cw := 20
	cols := max(1, w/cw)
	pw := cw - 4
	ph := pw * 3 / 4
	cardH := ph + 5
	rows := max(1, h/cardH)
	d.libCur = clamp(d.libCur, 0, len(shows)-1)
	d.libOff = scroll(d.libCur/cols, d.libOff, rows, (len(shows)+cols-1)/cols)
	var out []string
	for r := d.libOff; r < d.libOff+rows; r++ {
		var cards [][]string
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(shows) {
				cards = append(cards, block(nil, cw, cardH))
				continue
			}
			s := shows[i]
			sel := i == d.libCur
			acc := a.posters.accent(s.poster, cAccent).hex()
			body := []string{}
			if a.cfg.Posters {
				body = append(body, a.posters.render(s.poster, pw, ph, a.frame)...)
			}
			tc := cText
			if sel {
				tc = acc
			}
			body = append(body, fg(tc).Bold(sel).Render(truncate(s.title, pw)),
				stMuted.Render(fmt.Sprintf("%d bölüm · %s", len(s.eps), humanSize(s.bytes))))
			bc := cBg
			if sel {
				bc = acc
			}
			bs := fg(bc)
			card := []string{bs.Render("╭" + strings.Repeat("─", pw+2) + "╮")}
			for j, l := range body {
				pre := bs.Render("│ ")
				if j == 0 && sel {
					pre = anchor() + pre
				}
				card = append(card, pre+fit(l, pw)+bs.Render(" │"))
			}
			card = append(card, bs.Render("╰"+strings.Repeat("─", pw+2)+"╯"))
			cards = append(cards, block(card, cw, cardH))
		}
		out = append(out, hjoin(0, cards...)...)
	}
	return out
}

func (d *downloads) showView(a *App, s dlShow, w, h int) []string {
	acc := a.posters.accent(s.poster, cAccent).hex()
	pw := min(28, w/3)
	ph := pw * 3 / 4
	var left []string
	if a.cfg.Posters {
		left = a.posters.render(s.poster, pw, ph, a.frame)
	}
	lw := w - pw - 3
	right := []string{gradient(truncate(s.title, lw), []string{acc, cAccent2}, true),
		stMuted.Render(fmt.Sprintf("%d bölüm · %s · internetsiz izlenebilir", len(s.eps), humanSize(s.bytes))), ""}
	d.epCur = clamp(d.epCur, 0, len(s.eps)-1)
	e, _ := a.hist.Get(s.key)
	for i, j := range s.eps {
		if len(right) >= h {
			break
		}
		sel := i == d.epCur
		bg, bar := "", sp("  ", cText)
		if sel {
			bg, bar = cBand, sp("▌ ", acc)
		}
		mark := sp("  ", cText)
		if e.Watched[fmt.Sprintf("%d:%d", j.Season, j.Episode)] {
			mark = sp("✓ ", cOK)
		}
		title := fmt.Sprintf("S%d · %s. bölüm", j.Season, source.EpNum(j.Episode))
		if j.EpTitle != "" && !genericTitle(j.EpTitle) {
			title += "  " + j.EpTitle
		}
		line := row(lw, bg, []span{bar, mark, span{t: title, c: cText, bold: sel}}, []span{sp(j.Quality+"  "+humanSize(j.Total)+" ", cMuted)})
		if sel {
			line = anchor() + line
		}
		right = append(right, line)
	}
	return hjoin(3, block(left, pw, h), block(right, lw, h))
}

// offlineCover is the cover URL the shelf uses for a job.
func offlineCover(j download.Job) string {
	if c := filepath.Join(filepath.Dir(j.Path), "kapak.jpg"); j.Path != "" && fileOK(c) {
		return "file://" + c
	}
	return j.Poster
}
