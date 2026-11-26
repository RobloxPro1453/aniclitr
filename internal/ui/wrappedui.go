package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/wrapped"
)

const wrappedSlideCount = 6

var wrappedSlideTitles = []string{
	"genel bakış",
	"izleme ritmi",
	"en sevilen türler",
	"zirvedeki seriler",
	"izleyici kişiliğin",
	"özet kartı",
}

// wrappedScreen renders an interactive, animated Spotify-Wrapped-style 6-slide
// story built from the user's real history and library, with one-key PNG export.
type wrappedScreen struct {
	rep      wrapped.Report
	slide    int
	slideAt  time.Time
	exported string
}

func newWrappedScreen(a *App) *wrappedScreen {
	return &wrappedScreen{
		rep:     wrapped.Build(a.hist, a.lib, time.Now()),
		slide:   0,
		slideAt: time.Now(),
	}
}

func (s *wrappedScreen) crumb() string {
	return fmt.Sprintf("özet › %d/%d", s.slide+1, wrappedSlideCount)
}

func (s *wrappedScreen) hints() []hint {
	return []hint{
		{"←→/enter", "slayt"},
		{"1-6", "atla"},
		{"e", "PNG kart indir"},
		{"esc", "kapat"},
	}
}

func (s *wrappedScreen) animating(a *App) bool {
	if a.top() != screen(s) || !a.motion() {
		return false
	}
	return time.Since(s.slideAt) < 950*time.Millisecond
}

func (s *wrappedScreen) prog(a *App) float64 {
	if !a.motion() {
		return 1
	}
	p := float64(time.Since(s.slideAt)) / float64(750*time.Millisecond)
	if p >= 1 {
		return 1
	}
	if p < 0 {
		return 0
	}
	return easeOutCubic(p)
}

func (s *wrappedScreen) gotoSlide(a *App, idx int) tea.Cmd {
	idx = clamp(idx, 0, wrappedSlideCount-1)
	if idx == s.slide {
		return nil
	}
	s.slide = idx
	s.slideAt = time.Now()
	a.flash()
	if idx == 4 {
		a.anchorOK = false
		a.celebrate(55)
	}
	return a.tick()
}

func (s *wrappedScreen) exportCard(a *App) tea.Cmd {
	p, err := wrapped.ExportPNG(s.rep, "")
	if err != nil {
		a.notify("kart kaydedilemedi: "+err.Error(), "err")
		return a.tick()
	}
	s.exported = p
	a.anchorOK = false
	a.celebrate(45)
	a.notify("PNG özet kartı kaydedildi: "+p, "ok")
	return a.tick()
}

func (s *wrappedScreen) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc", "q":
		a.pop()
		return nil
	case "right", "l", " ", "enter", "tab":
		if s.slide >= wrappedSlideCount-1 {
			return s.gotoSlide(a, 0)
		}
		return s.gotoSlide(a, s.slide+1)
	case "left", "h", "shift+tab", "backspace":
		if s.slide <= 0 {
			return s.gotoSlide(a, wrappedSlideCount-1)
		}
		return s.gotoSlide(a, s.slide-1)
	case "1", "2", "3", "4", "5", "6":
		return s.gotoSlide(a, int(k.String()[0]-'1'))
	case "e", "E":
		return s.exportCard(a)
	}
	return nil
}

func (s *wrappedScreen) view(a *App, w, h int) string {
	cw := clamp(w-6, 52, 90)
	iw := cw - 4
	p := s.prog(a)
	r := s.rep

	var body []string
	slideAcc := cAccent
	switch s.slide {
	case 0:
		slideAcc = cAccent
		hrs := r.TotalHours * p
		eps := int(float64(r.TotalEpisodes)*p + 0.5)
		ser := int(float64(r.TotalSeries)*p + 0.5)

		body = append(body,
			center(pill(fmt.Sprintf("✦ ANİME ÖZETİ · %d", r.Year), cAccent), iw),
			"",
			center(gradient("Bu Yıl Anime Dünyasında", brand, true), iw),
			"",
		)
		// 3 crisp side-by-side metric boxes
		colW := max(14, (iw-4)/3)
		metric := func(val, lbl, col string) []string {
			v := center(fg(col).Bold(true).Render(val), colW-2)
			l := center(stMuted.Render(lbl), colW-2)
			bs := fg(mix(hex(cFaint), hex(col), 0.45).hex())
			return []string{
				bs.Render("╭" + strings.Repeat("─", colW-2) + "╮"),
				bs.Render("│") + fit(v, colW-2) + bs.Render("│"),
				bs.Render("│") + fit(l, colW-2) + bs.Render("│"),
				bs.Render("╰" + strings.Repeat("─", colW-2) + "╯"),
			}
		}
		m1 := metric(fmt.Sprintf("%.1f sa", hrs), "toplam süre", cAccent)
		m2 := metric(fmt.Sprintf("%d", eps), "izlenen bölüm", cAccent2)
		m3 := metric(fmt.Sprintf("%d", ser), "farklı seri", cOK)
		for _, l := range hjoin(2, m1, m2, m3) {
			body = append(body, center(l, iw))
		}
		body = append(body, "")
		for _, l := range wrap(r.TimeComparison, iw-4) {
			body = append(body, center(stText.Render(l), iw))
		}

	case 1:
		slideAcc = cSky
		body = append(body,
			center(pill("☾ İZLEME RİTMİN", cSky), iw),
			"",
			center(gradient(r.PeakPeriod, []string{cSky, cAccent2}, true)+"  "+stMuted.Render("· "+r.PeakHourRange), iw),
			"",
		)
		days := []string{"Pzt", "Sal", "Çar", "Per", "Cum", "Cmt", "Paz"}
		maxMin := 1
		for _, m := range r.DayMinutes {
			if m > maxMin {
				maxMin = m
			}
		}
		barW := max(10, iw-22)
		for i, d := range days {
			frac := (float64(r.DayMinutes[i]) / float64(maxMin)) * p
			mins := int(float64(r.DayMinutes[i])*p + 0.5)
			lbl := stSub.Render(fmt.Sprintf("  %-4s", d))
			bar := progressBar(barW, frac, []string{cSky, cAccent})
			val := stMuted.Render(fmt.Sprintf(" %3d dk", mins))
			body = append(body, lbl+" "+bar+val)
		}
		if r.StreakDays > 0 {
			body = append(body, "", center(pill(fmt.Sprintf("⚡ %d GÜN KESİNTİSİZ SERİ", r.StreakDays), cWarn), iw))
		}
		body = append(body, "")
		for _, l := range wrap(r.HabitSentence, iw-4) {
			body = append(body, center(stText.Render(l), iw))
		}

	case 2:
		slideAcc = cAccent2
		body = append(body,
			center(pill("❖ EN SEVDİĞİN TÜRLER", cAccent2), iw),
			"",
			center(gradient("Ruhunu Besleyen Türler", []string{cAccent2, cAccent}, true), iw),
			"",
		)
		if len(r.TopGenres) == 0 {
			body = append(body, center(stMuted.Render("Kitaplığına birkaç seri eklediğinde tür profilin burada belirecek."), iw))
		} else {
			maxShare := r.TopGenres[0].Share
			if maxShare <= 0 {
				maxShare = 1
			}
			barW := max(10, iw-30)
			for i, g := range r.TopGenres {
				frac := (g.Share / maxShare) * p
				pct := int(g.Share*100*p + 0.5)
				rank := fg(cAccent2).Bold(true).Render(fmt.Sprintf("  #%d ", i+1))
				name := fit(stText.Bold(i == 0).Render(truncate(g.Name, 14)), 15)
				bar := progressBar(barW, frac, []string{cAccent2, cAccent})
				meta := stMuted.Render(fmt.Sprintf(" %%%d (%d)", pct, g.Count))
				body = append(body, rank+name+" "+bar+meta)
			}
		}
		body = append(body, "")
		for _, l := range wrap(r.GenreSentence, iw-4) {
			body = append(body, center(stText.Render(l), iw))
		}

	case 3:
		slideAcc = cWarn
		body = append(body,
			center(pill("★ ZİRVEDEKİ SERİLERİN", cWarn), iw),
			"",
			center(gradient("En Çok İz Bırakanlar", []string{cWarn, cAccent}, true), iw),
			"",
		)
		if len(r.TopShows) == 0 {
			body = append(body, center(stMuted.Render("Henüz listelenmiş seri yok — ilk serini açtığında podyum burada oluşur."), iw))
		} else {
			for i, sh := range r.TopShows {
				if p < float64(i)*0.12 {
					continue
				}
				medal := fmt.Sprintf("  #%d ", i+1)
				mc := cMuted
				if i == 0 {
					medal, mc = "  ★  ", cWarn
				} else if i == 1 {
					mc = cAccent
				} else if i == 2 {
					mc = cAccent2
				}
				right := ""
				if sh.Score > 0 {
					right += fg(cWarn).Bold(true).Render(fmt.Sprintf("★ %d/10  ", sh.Score))
				}
				if sh.Watched > 0 {
					right += stMuted.Render(fmt.Sprintf("%d bölüm", sh.Watched))
				}
				left := fg(mc).Bold(true).Render(medal) + stText.Bold(i == 0).Render(truncate(sh.Title, max(12, iw-lipgloss.Width(right)-8)))
				body = append(body, row2(left, right+"  ", iw))
			}
		}
		body = append(body, "")
		for _, l := range wrap(r.ShowSentence, iw-4) {
			body = append(body, center(stText.Render(l), iw))
		}

	case 4:
		slideAcc = cOK
		body = append(body,
			center(pill(r.ArchetypeBadge, cAccent), iw),
			"",
			center(gradient("✦  "+r.Archetype+"  ✦", brand, true), iw),
			center(stSub.Italic(true).Render(truncate(r.Tagline, iw-2)), iw),
			"",
			stFaint.Render(strings.Repeat("─", iw)),
			"",
		)
		for i, para := range r.PersonaStory {
			if p < float64(i)*0.16 {
				continue
			}
			for j, wl := range wrap(para, iw-6) {
				pref := "    "
				if j == 0 {
					pref = fg(cAccent).Render("  ◆ ")
				}
				body = append(body, pref+stText.Render(wl))
			}
		}

	case 5:
		slideAcc = cAccent
		topShow := "—"
		if len(r.TopShows) > 0 {
			topShow = r.TopShows[0].Title
		}
		topGenre := "—"
		if len(r.TopGenres) > 0 {
			topGenre = r.TopGenres[0].Name
		}
		body = append(body,
			center(pill(fmt.Sprintf("✦ PAYLAŞILABİLİR ÖZET KARTI · %d", r.Year), cAccent), iw),
			"",
			center(gradient(r.Archetype, brand, true), iw),
			center(stMuted.Render(truncate(r.Tagline, iw-2)), iw),
			"",
			stFaint.Render(strings.Repeat("─", iw)),
			row2("  "+stMuted.Render("Toplam süre / bölüm"), stText.Bold(true).Render(fmt.Sprintf("%.1f saat · %d bölüm  ", r.TotalHours, r.TotalEpisodes)), iw),
			row2("  "+stMuted.Render("Tamamlama oranı"), stText.Bold(true).Render(fmt.Sprintf("%%%d (%d/%d seri)  ", r.CompletionRate, r.CompletedCount, r.TotalSeries)), iw),
			row2("  "+stMuted.Render("İzleme ritmi"), stText.Bold(true).Render(r.PeakPeriod+"  "), iw),
			row2("  "+stMuted.Render("Favori tür"), stText.Bold(true).Render(truncate(topGenre, iw/2)+"  "), iw),
			row2("  "+stMuted.Render("Zirvedeki seri"), fg(cWarn).Bold(true).Render(truncate(topShow, iw/2)+"  "), iw),
			stFaint.Render(strings.Repeat("─", iw)),
			"",
			center(keycap("e")+" "+fg(cOK).Bold(true).Render("900×1200 PNG Kart Olarak Kaydet"), iw),
		)
		if s.exported != "" {
			body = append(body, center(fg(cOK).Render("✓ "+truncate(s.exported, iw-4)), iw))
		}
	}

	// Step progress bar (6 dots)
	var dots []string
	for i := 0; i < wrappedSlideCount; i++ {
		if i == s.slide {
			dots = append(dots, fg(slideAcc).Render("━━━"))
		} else {
			dots = append(dots, stFaint.Render("──"))
		}
	}
	tag := strings.Join(dots, " ")

	innerLines := append([]string{""}, body...)
	innerLines = append(innerLines, "")
	panelH := min(h-2, max(12, len(innerLines)+2))
	title := fmt.Sprintf("özet · %s (%d/%d)", wrappedSlideTitles[s.slide], s.slide+1, wrappedSlideCount)
	boxed := panel(title, innerLines, cw, panelH, true, slideAcc, tag)

	top := max(0, (h-len(boxed)-2)/2)
	out := make([]string, 0, h)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range boxed {
		out = append(out, center(l, w))
	}
	if len(out)+2 <= h {
		nav := keycap("←→") + " " + stMuted.Render("slayt") + "   " +
			keycap("1-6") + " " + stMuted.Render("bölüm") + "   " +
			keycap("e") + " " + stMuted.Render("PNG indir") + "   " +
			keycap("esc") + " " + stMuted.Render("kapat")
		out = append(out, "", center(truncate(nav, w-4), w))
	}
	return strings.Join(block(out, w, h), "\n")
}
