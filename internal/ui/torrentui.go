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
	"aniclitr/internal/source"
	"aniclitr/internal/tstream"
)

type nyaaMsg struct {
	id    int
	items []nyaa.Item
	err   error
}

type torrentOpenMsg struct {
	id   int
	item nyaa.Item
	sess *tstream.Session
	err  error
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
	focus    int // 0 search box, 1 results list, 2 embedded file picker
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

	// Embedded BitTorrent streaming session & batch file picker
	openReqID int
	opening   bool
	openItem  nyaa.Item
	sess      *tstream.Session
	files     []tstream.VideoFile
	fCur      int
	fOff      int
	fGl       glide
}

func newTorrentScreen(initial string) *torrentScreen {
	s := &torrentScreen{
		in: input{placeholder: "Nyaa.si'de ara (örn: another, frieren 1080p, one piece raw)…"},
	}
	if q := strings.TrimSpace(initial); q != "" {
		s.in.Set(q)
		s.focus = 1
	}
	return s
}

func (s *torrentScreen) closeStream() {
	if s.sess != nil {
		s.sess.Close()
		s.sess = nil
	}
	s.files = nil
	s.opening = false
	s.fCur, s.fOff = 0, 0
}

func (s *torrentScreen) crumb() string {
	if s.focus == 2 {
		return "torrent › dosyalar"
	}
	return "torrent › " + strings.ToLower(torrentTabs[s.tab].label)
}

func (s *torrentScreen) typing() bool { return s.focus == 0 }

func (s *torrentScreen) animating(a *App) bool {
	return a.top() == screen(s) && (s.busy || s.opening)
}

func (s *torrentScreen) hints() []hint {
	if s.focus == 0 {
		return []hint{{"enter", "ara"}, {"↓", "sonuçlar"}, {"esc", "geri"}}
	}
	if s.focus == 2 {
		return []hint{
			{"enter", "anlık izle (mpv)"},
			{"d", "seçili bölümü indir"},
			{"D", "tüm paketi indir"},
			{"↑↓", "dosya seç"},
			{"esc", "listeye dön"},
		}
	}
	return []hint{
		{"enter", "anlık izle / dosyalar"},
		{"d", "indir (ctrl+d)"},
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
	s.closeStream()
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

func (s *torrentScreen) openTorrent(a *App, it nyaa.Item) tea.Cmd {
	s.closeStream()
	s.openReqID++
	id := s.openReqID
	s.opening = true
	s.openItem = it
	a.notify("torrent dosya listesi alınıyor…", "")
	return tea.Batch(a.tick(), run(35*time.Second, func(ctx context.Context) tea.Msg {
		sess, err := tstream.Open(ctx, it.Torrent, it.Magnet)
		return torrentOpenMsg{id: id, item: it, sess: sess, err: err}
	}))
}

func (s *torrentScreen) streamFile(a *App, idx int) tea.Cmd {
	if s.sess == nil || idx < 0 || idx >= len(s.files) {
		return nil
	}
	u, vf, err := s.sess.ServeFile(idx)
	if err != nil {
		a.notify("akış başlatılamadı: "+err.Error(), "err")
		return a.tick()
	}
	parsed := download.ParseRelease(vf.Name)
	title := parsed.Series
	if title == "" || title == "anime" {
		title = download.ParseRelease(s.openItem.Title).Series
	}
	if title == "" {
		title = s.openItem.Title
	}
	an := source.Anime{
		Source: "nyaa",
		ID:     s.openItem.InfoHash,
		Title:  title,
	}
	if parsed.IsMovie {
		an.Kind = "Film"
	}
	epNum := max(1, vf.Episode)
	ep := source.Episode{
		ID:     vf.RelPath,
		Season: max(1, vf.Season),
		Number: epNum,
		Title:  vf.Name,
	}
	qual := s.openItem.Resolution
	if qual == "" {
		qual = parsed.Resolution
	}
	if qual == "" {
		qual = "Torrent"
	}
	st := &source.Stream{
		Qualities: []source.Quality{{Label: qual, URL: u}},
	}
	fansub := s.openItem.Group
	if fansub == "" {
		fansub = "Nyaa.si"
	}
	pl := newPlaying(an, ep, []source.Episode{ep}, fansub, "Gömülü Torrent", st, st.Qualities[0])
	a.notify("▶ gömülü torrent akışı mpv'de başlatılıyor…", "ok")
	return tea.Batch(a.push(pl), pl.start(a))
}

func (s *torrentScreen) queueTorrentDownload(a *App, it nyaa.Item, filePath string) tea.Cmd {
	req := download.TorrentRequest{
		Title:      it.Title,
		TorrentURL: it.Torrent,
		Magnet:     it.Magnet,
		FilePath:   filePath,
		Group:      it.Group,
		Resolution: it.Resolution,
		Size:       it.SizeBytes,
		LibTitles:  a.libTitles(),
	}
	job, _ := a.dl.AddTorrent(req)
	a.flash()
	a.notify(fmt.Sprintf("↓ İndirilenler kuyruğuna eklendi → %s (ctrl+d)", job.Title), "ok")
	return a.tick()
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
	case torrentOpenMsg:
		if m.id != s.openReqID {
			if m.sess != nil {
				m.sess.Close()
			}
			return nil
		}
		s.opening = false
		if m.err != nil {
			a.notify("torrent açılamadı: "+m.err.Error(), "err")
			return a.tick()
		}
		s.sess = m.sess
		s.files = m.sess.Files()
		s.fCur, s.fOff = 0, 0
		if len(s.files) == 1 {
			// Single video file: stream immediately into mpv!
			s.focus = 2
			return s.streamFile(a, 0)
		}
		s.focus = 2
		a.notify(fmt.Sprintf("✓ %d video dosyası hazır — enter ile anlık izle", len(s.files)), "ok")
		return a.tick()
	case tea.KeyMsg:
		if s.focus == 0 {
			switch m.Type {
			case tea.KeyEsc:
				if len(s.rawItems) > 0 {
					s.focus = 1
					return nil
				}
				s.closeStream()
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

		// Embedded torrent file picker mode
		if s.focus == 2 {
			switch m.String() {
			case "esc", "backspace", "left", "h":
				s.closeStream()
				s.focus = 1
				return a.tick()
			case "up", "k":
				s.fCur = max(0, s.fCur-1)
			case "down", "j":
				if len(s.files) > 0 {
					s.fCur = clamp(s.fCur+1, 0, len(s.files)-1)
				}
			case "pgup":
				s.fCur = max(0, s.fCur-8)
			case "pgdown":
				if len(s.files) > 0 {
					s.fCur = clamp(s.fCur+8, 0, len(s.files)-1)
				}
			case "enter", " ":
				return s.streamFile(a, s.fCur)
			case "d":
				if s.fCur < len(s.files) {
					return s.queueTorrentDownload(a, s.openItem, s.files[s.fCur].RelPath)
				}
			case "D":
				return s.queueTorrentDownload(a, s.openItem, "")
			}
			return nil
		}

		switch m.String() {
		case "esc":
			s.closeStream()
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
				return s.queueTorrentDownload(a, s.items[s.cur], "")
			}
		case "enter":
			if s.cur < len(s.items) && !s.opening {
				return s.openTorrent(a, s.items[s.cur])
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

	leftHead := gradient("Torrent · Nyaa.si", brand, true) + "   " + pill(activeTab.short, activeTab.accent)
	if s.focus == 2 {
		leftHead = gradient("Torrent · Dosya Seçici", brand, true) + "   " + pill("GÖMÜLÜ AKIŞ", cOK)
	} else if s.noRemake {
		leftHead += " " + fg(cSub).Background(colorOf(cSurface)).Render(" remake hariç ")
	}
	rightHead := keycap("o") + " " + stSub.Render(s.sortMode.Label()) + "   " +
		keycap("q") + " " + stSub.Render(torrentQualFilters[s.qualIdx])
	if s.focus == 2 {
		rightHead = keycap("enter") + " " + stSub.Render("anlık izle") + "   " + keycap("d") + " " + stSub.Render("indir") + "   " + keycap("esc") + " " + stMuted.Render("listeye dön")
	}
	gap := max(2, inner-lipgloss.Width(leftHead)-lipgloss.Width(rightHead))
	headLine := fit(leftHead+strings.Repeat(" ", gap)+rightHead, inner)

	var top []string
	top = append(top, headLine)
	if s.focus == 2 {
		top = append(top, stSub.Render(truncate(s.openItem.Title, inner)))
	} else {
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
	}

	bodyH := max(6, h-len(top)-1)
	var body []string
	if s.focus == 2 {
		body = s.renderFilePickerPane(a, inner, bodyH)
	} else if inner >= 102 {
		detW := clamp(inner*34/100, 34, 46)
		listW := inner - detW - 2
		leftPane := s.renderListPane(a, listW, bodyH, activeTab.accent)
		rightPane := s.renderDetailPane(a, detW, bodyH, activeTab.accent)
		body = hjoin(2, leftPane, rightPane)
	} else {
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

func (s *torrentScreen) renderFilePickerPane(a *App, w, h int) []string {
	innerW := max(20, w-4)
	listH := max(3, h-2)
	tag := fmt.Sprintf("%d video", len(s.files))
	if s.sess != nil {
		if st := s.sess.Stats(); st.Peers > 0 {
			tag += fmt.Sprintf(" · %d eş", st.Peers)
		}
	}

	var rows []string
	if len(s.files) == 0 {
		rows = append(rows, "", stMuted.Render("Oynatılabilir video dosyası yok."))
	} else {
		s.fCur = clamp(s.fCur, 0, len(s.files)-1)
		s.fOff = scroll(s.fCur, s.fOff, listH, len(s.files))
		band := s.fGl.row(a, s.fCur)
		for i := s.fOff; i < len(s.files) && len(rows) < listH; i++ {
			f := s.files[i]
			sel := i == band
			bg, bar := "", sp("  ", cText)
			if sel {
				bg, bar = cBand, sp("▌ ", cOK)
			}
			left := []span{bar, sp(fmt.Sprintf("%2d  ", i+1), cMuted)}
			if f.Episode > 0 {
				left = append(left, spb(fmt.Sprintf("S%02dE%02d ", max(1, f.Season), f.Episode), cAccent))
			}
			left = append(left, span{t: f.Name, c: cText, bold: sel})
			right := []span{sp(fmtTorrentBytes(f.Size)+" ", cSub)}
			rows = append(rows, row(innerW-1, bg, left, right))
		}
		rows = hjoin(0, block(rows, innerW-1, listH), scrollbar(listH, s.fOff, len(s.files)))
	}
	return panel("torrent dosyaları · seç ve anlık izle", rows, w, h, true, cOK, tag)
}

func fmtTorrentBytes(b int64) string {
	const (
		kib = 1024.0
		mib = kib * 1024
		gib = mib * 1024
	)
	f := float64(b)
	switch {
	case f >= gib:
		return fmt.Sprintf("%.2f GiB", f/gib)
	case f >= mib:
		return fmt.Sprintf("%.1f MiB", f/mib)
	case f >= kib:
		return fmt.Sprintf("%.0f KiB", f/kib)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (s *torrentScreen) renderListPane(a *App, w, h int, acc string) []string {
	innerW := max(20, w-4)
	listH := max(3, h-2)
	tag := ""
	if s.opening {
		tag = spinner(a.frame/2) + " açılıyor…"
	} else if s.queried != "" && !s.busy {
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
			stText.Bold(true).Render("Nyaa.si Gömülü Torrent Akışı & İndirme"),
			stSub.Render("Yukarıdaki kutuya anime adını yazıp Enter'a bas."),
			"",
			stMuted.Render("Kategoriler:"),
			"  "+keycap("1")+" "+stSub.Render("Tümü")+"   "+keycap("2")+" "+fg(cSky).Render("Türkçe / Diğer")+"   "+keycap("3")+" "+stSub.Render("İngilizce"),
			"  "+keycap("4")+" "+fg(cWarn).Bold(true).Render("Raw (Ham Japonca)")+"   "+keycap("5")+" "+fg(cOK).Render("Güvenilir"),
			"",
			stMuted.Render("Enter: harici istemci olmadan anlık mpv akışı  ·  d: düzenli indir (ctrl+d)"),
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

func (s *torrentScreen) renderDetailPane(a *App, w, h int, acc string) []string {
	innerW := max(20, w-4)
	bodyH := max(3, h-2)

	if len(s.items) == 0 || s.cur >= len(s.items) {
		empty := []string{
			"",
			center(stText.Bold(true).Render("Ayrıntı & Anlık Akış"), innerW),
			center(stMuted.Render("Bir torrent seçildiğinde"), innerW),
			center(stMuted.Render("grup, çözünürlük, seed sağlığı"), innerW),
			center(stMuted.Render("ve gömülü mpv akışı burada görünür."), innerW),
		}
		return panel("ayrıntı", block(empty, innerW, bodyH), w, h, false, cFaint, "")
	}

	it := s.items[s.cur]
	tag := it.Group
	var out []string

	for i, l := range wrap(it.Title, innerW) {
		if i == 0 {
			out = append(out, gradient(l, []string{acc, cAccent2}, true))
		} else if i < 4 {
			out = append(out, stBold.Render(l))
		}
	}
	out = append(out, "")

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

	out = append(out, "", stFaint.Render(strings.Repeat("─", innerW)))
	if s.opening {
		out = append(out, fg(cOK).Bold(true).Render(spinner(a.frame/2)+" Dosya listesi hazırlanıyor…"))
	} else {
		out = append(out,
			keycap("enter")+" "+fg(cOK).Bold(true).Render("anlık izle (gömülü mpv)"),
			keycap("d")+" "+stText.Bold(true).Render("indir → ctrl+d (oto klasörle)"),
			keycap("c")+" "+stSub.Render("magnet bağlantısını kopyala"),
			keycap("w")+" "+stMuted.Render("nyaa.si sayfasını aç"),
		)
	}

	return panel("ayrıntı", block(out, innerW, bodyH), w, h, false, acc, tag)
}
