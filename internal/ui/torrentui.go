package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aniclitr/internal/download"
	"aniclitr/internal/mal"
	"aniclitr/internal/nyaa"
)

type nyaaMsg struct {
	id    int
	items []nyaa.Item
	err   error
}

type torrentTab struct {
	label  string
	short  string
	cat    string
	filter string
	accent string
}

var torrentTabs = []torrentTab{
	{"Tümü", "TÜMÜ", nyaa.CatAll, nyaa.FilterAll, cAccent},
	{"Türkçe / Diğer", "TÜRKÇE", nyaa.CatNonEng, nyaa.FilterAll, cSky},
	{"İngilizce", "İNGİLİZCE", nyaa.CatEng, nyaa.FilterAll, cAccent2},
	{"Raw (Ham)", "RAW (HAM)", nyaa.CatRaw, nyaa.FilterAll, cWarn},
	{"Güvenilir", "GÜVENİLİR", nyaa.CatAll, nyaa.FilterTrusted, cOK},
}

var torrentQualFilters = []string{"tümü", "1080p", "720p", "2160p", "paket"}

var torrentReqID int

type torrentScreen struct {
	in       input
	focus    int // 0 search box, 1 list
	tab      int
	sortMode nyaa.SortMode
	qualIdx  int
	noRemake bool
	reqID    int
	busy     bool
	err      error
	rawItems []nyaa.Item // full server response
	items    []nyaa.Item // filtered + sorted view
	cur      int
	off      int
	gl       glide
	queried  string
	hasAria2 bool
	hasWT    bool
}

func newTorrentScreen(initial string) *torrentScreen {
	s := &torrentScreen{
		in:       input{placeholder: "Nyaa.si'de ara (örn: frieren 1080p, one piece raw, dandadan batch)…"},
		hasAria2: nyaa.HasAria2(),
		hasWT:    nyaa.HasWebTorrent(),
	}
	if q := strings.TrimSpace(initial); q != "" {
		s.in.Set(q)
		s.focus = 1
	}
	return s
}

func (s *torrentScreen) crumb() string {
	return "torrent › " + strings.ToLower(torrentTabs[s.tab].label)
}

func (s *torrentScreen) typing() bool { return s.focus == 0 }

func (s *torrentScreen) animating(a *App) bool {
	return a.top() == screen(s) && s.busy
}

func (s *torrentScreen) hints() []hint {
	if s.focus == 0 {
		return []hint{{"enter", "ara"}, {"↓", "sonuçlar"}, {"esc", "geri"}}
	}
	return []hint{
		{"d", "aria2c indir"},
		{"enter", "magnet / izle"},
		{"c", "magnet kopyala"},
		{"1-5", "kategori (4: Raw)"},
		{"o", "sırala"},
		{"q", "kalite"},
		{"/", "ara"},
		{"esc", "geri"},
	}
}

func (s *torrentScreen) start(a *App) tea.Cmd {
	q := strings.TrimSpace(s.in.Value())
	if q == "" {
		return nil
	}
	return s.search(a, q)
}

func (s *torrentScreen) search(a *App, q string) tea.Cmd {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	torrentReqID++
	s.reqID = torrentReqID
	s.busy = true
	s.err = nil
	s.queried = q
	s.cur, s.off = 0, 0
	t := torrentTabs[s.tab]
	flt := t.filter
	if flt == nyaa.FilterAll && s.noRemake {
		flt = nyaa.FilterNoRemake
	}
	id := s.reqID
	return tea.Batch(a.tick(), run(20*time.Second, func(ctx context.Context) tea.Msg {
		items, err := nyaa.Search(ctx, q, t.cat, flt)
		return nyaaMsg{id: id, items: items, err: err}
	}))
}

func (s *torrentScreen) applyLocal() {
	qf := torrentQualFilters[s.qualIdx]
	out := make([]nyaa.Item, 0, len(s.rawItems))
	for _, it := range s.rawItems {
		switch qf {
		case "1080p", "720p", "2160p":
			if it.Resolution != qf {
				continue
			}
		case "paket":
			if !it.Batch {
				continue
			}
		}
		out = append(out, it)
	}
	nyaa.SortItems(out, s.sortMode)
	s.items = out
	s.cur = clamp(s.cur, 0, max(0, len(s.items)-1))
	s.off = 0
}

func (s *torrentScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case nyaaMsg:
		if m.id != s.reqID {
			return nil
		}
		s.busy = false
		s.err = m.err
		s.rawItems = m.items
		s.applyLocal()
		if len(s.items) > 0 || len(s.rawItems) > 0 {
			s.focus = 1
		}
		return nil
	case tea.KeyMsg:
		if s.focus == 0 {
			switch m.Type {
			case tea.KeyEsc:
				if len(s.rawItems) > 0 {
					s.focus = 1
					return nil
				}
				a.pop()
				return nil
			case tea.KeyDown:
				if len(s.rawItems) > 0 {
					s.focus = 1
				}
				return nil
			case tea.KeyEnter:
				return s.search(a, s.in.Value())
			}
			s.in.handle(m)
			return nil
		}

		switch m.String() {
		case "esc":
			a.pop()
		case "/":
			s.focus = 0
		case "up", "k":
			if s.cur == 0 {
				s.focus = 0
			} else {
				s.cur--
			}
		case "down", "j":
			if len(s.items) > 0 {
				s.cur = clamp(s.cur+1, 0, len(s.items)-1)
			}
		case "pgup":
			s.cur = max(0, s.cur-8)
		case "pgdown":
			if len(s.items) > 0 {
				s.cur = clamp(s.cur+8, 0, len(s.items)-1)
			}
		case "1", "2", "3", "4", "5":
			idx := int(m.String()[0] - '1')
			if idx != s.tab {
				s.tab = idx
				if s.queried != "" {
					return s.search(a, s.queried)
				}
			}
		case "tab":
			s.tab = (s.tab + 1) % len(torrentTabs)
			if s.queried != "" {
				return s.search(a, s.queried)
			}
		case "shift+tab":
			s.tab = (s.tab - 1 + len(torrentTabs)) % len(torrentTabs)
			if s.queried != "" {
				return s.search(a, s.queried)
			}
		case "o":
			s.sortMode = (s.sortMode + 1) % 5
			s.applyLocal()
			a.notify("sıralama: "+s.sortMode.Label(), "ok")
			return a.tick()
		case "q":
			s.qualIdx = (s.qualIdx + 1) % len(torrentQualFilters)
			s.applyLocal()
			a.notify("kalite süzgeci: "+torrentQualFilters[s.qualIdx], "ok")
			return a.tick()
		case "f":
			s.noRemake = !s.noRemake
			if s.queried != "" {
				return s.search(a, s.queried)
			}
		case "r":
			if s.queried != "" {
				return s.search(a, s.queried)
			}
		case "c":
			if s.cur < len(s.items) {
				it := s.items[s.cur]
				if it.Magnet == "" {
					a.notify("bu kayıtta magnet yok", "warn")
				} else if err := nyaa.CopyClipboard(it.Magnet); err != nil {
					a.notify("kopyalanamadı: "+err.Error(), "err")
				} else {
					a.notify("magnet panoya kopyalandı", "ok")
					a.flash()
				}
				return a.tick()
			}
		case "d":
			if s.cur < len(s.items) {
				it := s.items[s.cur]
				target := it.Magnet
				if target == "" {
					target = it.Torrent
				}
				dir := a.cfg.DownloadDir
				if dir == "" {
					dir = download.DefaultDir()
				}
				if err := nyaa.StartAria2(dir, target); err != nil {
					a.notify(err.Error(), "err")
				} else {
					a.notify("aria2c torrent indirmesi başladı → "+dir, "ok")
					a.flash()
				}
				return a.tick()
			}
		case "enter":
			if s.cur < len(s.items) {
				it := s.items[s.cur]
				if it.Magnet == "" {
					if it.PageURL != "" {
						_ = mal.OpenBrowser(it.PageURL)
					}
					return nil
				}
				if s.hasWT {
					if err := nyaa.StreamWebTorrent(it.Magnet); err == nil {
						a.notify("webtorrent + mpv akışı başlatılıyor…", "ok")
						return a.tick()
					}
				}
				if err := mal.OpenBrowser(it.Magnet); err != nil {
					a.notify("magnet açılamadı: "+err.Error(), "err")
				} else {
					a.notify("magnet sistem torrent istemcisinde açıldı", "ok")
				}
				return a.tick()
			}
		case "w":
			if s.cur < len(s.items) && s.items[s.cur].PageURL != "" {
				_ = mal.OpenBrowser(s.items[s.cur].PageURL)
				a.notify("nyaa.si sayfası tarayıcıda açıldı", "ok")
				return a.tick()
			}
		}
	}
	return nil
}

func (s *torrentScreen) view(a *App, w, h int) string {
	pad := 2
	inner := max(48, w-pad*2)
	activeTab := torrentTabs[s.tab]

	// Top Nova studio header strip (matches Keşfet).
	leftHead := gradient("Torrent · Nyaa.si", brand, true) + "   " + pill(activeTab.short, activeTab.accent)
	if s.noRemake {
		leftHead += " " + fg(cSub).Background(colorOf(cSurface)).Render(" remake hariç ")
	}
	rightHead := keycap("o") + " " + stSub.Render(s.sortMode.Label()) + "   " +
		keycap("q") + " " + stSub.Render(torrentQualFilters[s.qualIdx])
	gap := max(2, inner-lipgloss.Width(leftHead)-lipgloss.Width(rightHead))
	headLine := fit(leftHead+strings.Repeat(" ", gap)+rightHead, inner)

	// Search input + 5 category tabs (including 4 Raw (Ham)!).
	var top []string
	top = append(top, headLine)
	top = append(top, s.in.view(inner, s.focus == 0, a.frame)...)
	var tabs []string
	for i, t := range torrentTabs {
		lbl := fmt.Sprintf("%d %s", i+1, t.label)
		if i == s.tab {
			tabs = append(tabs, pill(lbl, t.accent))
		} else {
			tabs = append(tabs, fg(cMuted).Background(colorOf(cSurface)).Render(" "+lbl+" "))
		}
	}
	top = append(top, truncate(strings.Join(tabs, " "), inner))

	bodyH := max(6, h-len(top)-1)
	var body []string
	if inner >= 102 {
		// Wide 2-column Nova studio layout: results list + right showcase detail card.
		detW := clamp(inner*34/100, 34, 46)
		listW := inner - detW - 2
		leftPane := s.renderListPane(a, listW, bodyH, activeTab.accent)
		rightPane := s.renderDetailPane(detW, bodyH, activeTab.accent)
		body = hjoin(2, leftPane, rightPane)
	} else {
		// Compact terminal: full-width list panel.
		body = s.renderListPane(a, inner, bodyH, activeTab.accent)
	}

	var out []string
	for _, l := range top {
		out = append(out, strings.Repeat(" ", pad)+fit(l, inner))
	}
	out = append(out, "")
	for _, l := range body {
		out = append(out, strings.Repeat(" ", pad)+fit(l, inner))
	}
	return strings.Join(block(out, w, h), "\n")
}

func (s *torrentScreen) renderListPane(a *App, w, h int, acc string) []string {
	innerW := max(20, w-4)
	listH := max(3, h-2)
	tag := ""
	if s.queried != "" && !s.busy {
		if len(s.items) != len(s.rawItems) {
			tag = fmt.Sprintf("%d / %d sonuç", len(s.items), len(s.rawItems))
		} else {
			tag = fmt.Sprintf("%d sonuç", len(s.items))
		}
	}

	var rows []string
	switch {
	case s.busy:
		rows = append(rows, "", fg(acc).Render(spinner(a.frame/2))+" "+stSub.Render("Nyaa.si aranıyor…"))
		for i := 0; i < 4 && len(rows) < listH; i++ {
			rows = append(rows, "  "+fg(cSurface).Render(strings.Repeat("▬", min(innerW-6, 24+i*7))))
		}
	case s.err != nil:
		rows = append(rows, "",
			stErr.Bold(true).Render("✗ Nyaa.si'ye ulaşılamadı"),
			stMuted.Render(truncate(shortErr(s.err), innerW)),
			"",
			keycap("r")+" "+stSub.Render("tekrar dene"),
		)
	case s.queried == "":
		rows = append(rows, "",
			stText.Bold(true).Render("Nyaa.si Anime Torrent Araması"),
			stSub.Render("Yukarıdaki kutuya anime adını yazıp Enter'a bas."),
			"",
			stMuted.Render("Kategoriler:"),
			"  "+keycap("1")+" "+stSub.Render("Tümü")+"   "+keycap("2")+" "+fg(cSky).Render("Türkçe / Diğer")+"   "+keycap("3")+" "+stSub.Render("İngilizce"),
			"  "+keycap("4")+" "+fg(cWarn).Bold(true).Render("Raw (Ham Japonca)")+"   "+keycap("5")+" "+fg(cOK).Render("Güvenilir"),
			"",
			stMuted.Render("Örnek aramalar: frieren 1080p · one piece raw · dandadan batch"),
		)
	case len(s.items) == 0:
		rows = append(rows, "", stMuted.Render("Bu filtrede sonuç yok."))
		if len(s.rawItems) > 0 {
			rows = append(rows, "", keycap("q")+" "+stSub.Render("kalite süzgecini temizle ("+fmt.Sprintf("%d gizli sonuç", len(s.rawItems))+")"))
		} else {
			rows = append(rows, "", stMuted.Render("1-5 ile başka kategori (örn. 1 Tümü veya 4 Raw) dene."))
		}
	default:
		s.cur = clamp(s.cur, 0, len(s.items)-1)
		s.off = scroll(s.cur, s.off, listH, len(s.items))
		band := s.gl.row(a, s.cur)
		for i := s.off; i < len(s.items) && len(rows) < listH; i++ {
			it := s.items[i]
			sel := s.focus == 1 && i == band
			bg, bar := "", sp("  ", cText)
			if sel {
				bg, bar = cBand, sp("▌ ", acc)
			}
			left := []span{bar}
			if it.Trusted {
				left = append(left, sp("★ ", cOK))
			}
			switch {
			case it.Raw:
				left = append(left, spb("RAW ", cWarn))
			case it.Turkish:
				left = append(left, spb("TR ", cSky))
			}
			if it.Batch {
				left = append(left, sp("PKT ", cAccent2))
			}
			if it.Resolution != "" {
				left = append(left, sp(it.Resolution+" ", cMuted))
			}
			left = append(left, span{t: it.Title, c: cText, bold: sel})

			sc := cOK
			if it.Seeders == 0 {
				sc = cErr
			} else if it.Seeders < 5 {
				sc = cWarn
			}
			right := []span{
				sp(fmt.Sprintf("%8s ", it.Size), cSub),
				sp(fmt.Sprintf("▲%-4d", it.Seeders), sc),
			}
			rows = append(rows, row(innerW-1, bg, left, right))
		}
		rows = hjoin(0, block(rows, innerW-1, listH), scrollbar(listH, s.off, len(s.items)))
	}

	return panel("torrentler", rows, w, h, s.focus == 1, acc, tag)
}

func (s *torrentScreen) renderDetailPane(w, h int, acc string) []string {
	innerW := max(20, w-4)
	bodyH := max(3, h-2)

	if len(s.items) == 0 || s.cur >= len(s.items) {
		empty := []string{
			"",
			center(stText.Bold(true).Render("Ayrıntı & İndirme"), innerW),
			center(stMuted.Render("Bir torrent seçildiğinde"), innerW),
			center(stMuted.Render("grup, çözünürlük, seed sağlığı"), innerW),
			center(stMuted.Render("ve indirme araçları burada görünür."), innerW),
		}
		return panel("ayrıntı", block(empty, innerW, bodyH), w, h, false, cFaint, "")
	}

	it := s.items[s.cur]
	tag := it.Group
	var out []string

	// Full unwrapped release title.
	for i, l := range wrap(it.Title, innerW) {
		if i == 0 {
			out = append(out, gradient(l, []string{acc, cAccent2}, true))
		} else if i < 4 {
			out = append(out, stBold.Render(l))
		}
	}
	out = append(out, "")

	// Metadata chips.
	var chips []string
	if it.Raw {
		chips = append(chips, pill("RAW · HAM", cWarn))
	} else if it.Turkish {
		chips = append(chips, pill("TÜRKÇE", cSky))
	}
	if it.Trusted {
		chips = append(chips, pill("★ GÜVENİLİR", cOK))
	}
	if it.Batch {
		chips = append(chips, fg(cAccent2).Background(colorOf(cSurface)).Render(" PAKET "))
	}
	if it.Resolution != "" {
		chips = append(chips, fg(cText).Background(colorOf(cSurface)).Render(" "+it.Resolution+" "))
	}
	if it.Codec != "" {
		chips = append(chips, fg(cSub).Background(colorOf(cSurface)).Render(" "+it.Codec+" "))
	}
	if len(chips) > 0 {
		out = append(out, wrapChips(chips, innerW)...)
		out = append(out, "")
	}

	// Health & stats block.
	sc := cOK
	healthText := "sağlıklı"
	if it.Seeders == 0 {
		sc, healthText = cErr, "seed yok"
	} else if it.Seeders < 5 {
		sc, healthText = cWarn, "zayıf"
	}
	out = append(out,
		fg(sc).Bold(true).Render(fmt.Sprintf("▲ %d seed", it.Seeders))+
			stFaint.Render("  ·  ")+
			stMuted.Render(fmt.Sprintf("▼ %d leech", it.Leechers))+
			stFaint.Render("  ·  ")+
			fg(sc).Render(healthText),
	)
	seedRatio := float64(it.Seeders) / float64(max(1, it.Seeders+it.Leechers))
	out = append(out, progressBar(innerW, seedRatio, []string{sc, cAccent2}), "")

	kv := func(k, v string) string {
		return fit(stMuted.Render(k), 12) + stText.Render(truncate(v, max(8, innerW-12)))
	}
	out = append(out, kv("Boyut", it.Size))
	if it.Category != "" {
		out = append(out, kv("Kategori", it.Category))
	}
	if it.Group != "" {
		out = append(out, kv("Grup", it.Group))
	}
	out = append(out, kv("İndirme", fmt.Sprintf("%d kez", it.Downloads)))
	if !it.Published.IsZero() {
		out = append(out, kv("Tarih", agoLong(it.Published)+" ("+it.Published.Local().Format("02.01.2006")+")"))
	}
	if it.InfoHash != "" {
		out = append(out, kv("Hash", it.InfoHash[:min(16, len(it.InfoHash))]+"…"))
	}

	// Actions strip at bottom of detail card.
	out = append(out, "", stFaint.Render(strings.Repeat("─", innerW)))
	ariaStatus := stErr.Render("○ yok")
	if s.hasAria2 {
		ariaStatus = stOK.Render("● hazır")
	}
	out = append(out,
		keycap("d")+" "+stText.Bold(true).Render("aria2c ile indir")+"  "+ariaStatus,
		keycap("enter")+" "+stSub.Render(map[bool]string{true: "webtorrent + mpv izle", false: "magnet istemcide aç"}[s.hasWT]),
		keycap("c")+" "+stSub.Render("magnet bağlantısını kopyala"),
		keycap("w")+" "+stMuted.Render("nyaa.si sayfasını aç"),
	)

	return panel("ayrıntı", block(out, innerW, bodyH), w, h, false, acc, tag)
}
