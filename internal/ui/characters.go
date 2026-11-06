package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/anilist"
	"aniclitr/internal/match"
	"aniclitr/internal/source"
)

type charsMsg struct {
	key   string
	chars []anilist.Character
	err   error
}

type charactersScreen struct {
	an          source.Anime
	chars       []anilist.Character
	loading     bool
	err         error
	cur, off    int
	descOff     int
	showVA      bool // false = character portrait, true = voice actor (seiyuu) portrait
	showSpoiler bool
	filtering   bool
	filter      input
	gl          glide
	revealAt    time.Time
	prevPost    string
	curPost     string
	posterAt    tween
}

func newCharactersScreen(an source.Anime) *charactersScreen {
	return &charactersScreen{
		an:       an,
		loading:  true,
		filter:   input{placeholder: "karakter veya seslendiren ara…"},
		revealAt: time.Now(),
	}
}

func (s *charactersScreen) crumb() string {
	return truncate(s.an.Title, 20) + " › karakterler"
}

func (s *charactersScreen) typing() bool { return s.filtering }

func (s *charactersScreen) hints() []hint {
	if s.filtering {
		return []hint{{"enter", "tamam"}, {"esc", "temizle"}}
	}
	return []hint{
		{"↑↓", "karakter seç"},
		{"v", "karakter/seiyuu görseli"},
		{"x", "spoiler aç/kapat"},
		{"J/K", "biyografi kaydır"},
		{"/", "ara"},
		{"esc", "geri"},
	}
}

func (s *charactersScreen) animating(a *App) bool {
	if a.top() != screen(s) {
		return false
	}
	return s.loading || s.posterAt.active() || revealing(s.revealAt, 20, a.motion())
}

func (s *charactersScreen) start(a *App) tea.Cmd {
	key := s.an.Key()
	malID := s.an.MalID
	title, _ := match.SplitSeason(s.an.Title)
	if strings.TrimSpace(title) == "" {
		title = s.an.Title
	}
	return tea.Batch(a.tick(), run(30*time.Second, func(ctx context.Context) tea.Msg {
		chars, err := anilist.Characters(ctx, malID, title)
		return charsMsg{key: key, chars: chars, err: err}
	}))
}

func (s *charactersScreen) visible() []anilist.Character {
	q := CleanQuery(s.filter.Value())
	if q == "" {
		return s.chars
	}
	var out []anilist.Character
	for _, c := range s.chars {
		hay := c.Name.Full + " " + c.Name.Native + " " + strings.Join(c.Name.Alternative, " ")
		for _, va := range c.VoiceActors {
			hay += " " + va.Name.Full + " " + va.Name.Native
		}
		if sc, _ := fuzzy(q, hay); sc > 0 {
			out = append(out, c)
		}
	}
	return out
}

func (s *charactersScreen) prefetchPosters(a *App) tea.Cmd {
	if !a.cfg.Posters {
		return nil
	}
	vis := s.visible()
	var cmds []tea.Cmd
	for i, c := range vis {
		if i > 12 {
			break
		}
		if c.Image.Large != "" {
			cmds = append(cmds, a.posters.want(c.Image.Large))
		}
		if len(c.VoiceActors) > 0 && c.VoiceActors[0].Image.Large != "" {
			cmds = append(cmds, a.posters.want(c.VoiceActors[0].Image.Large))
		}
	}
	if s.cur >= 0 && s.cur < len(vis) {
		c := vis[s.cur]
		if c.Image.Large != "" {
			cmds = append(cmds, a.posters.want(c.Image.Large))
		}
		if len(c.VoiceActors) > 0 && c.VoiceActors[0].Image.Large != "" {
			cmds = append(cmds, a.posters.want(c.VoiceActors[0].Image.Large))
		}
	}
	return tea.Batch(cmds...)
}

func (s *charactersScreen) move(a *App, d int) tea.Cmd {
	vis := s.visible()
	n := clamp(s.cur+d, 0, max(0, len(vis)-1))
	if n == s.cur {
		return nil
	}
	s.cur = n
	s.descOff = 0
	return tea.Batch(s.prefetchPosters(a), a.tick())
}

func (s *charactersScreen) update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case charsMsg:
		if m.key != s.an.Key() {
			return nil
		}
		s.loading = false
		if m.err != nil {
			s.err = m.err
			return nil
		}
		s.chars = m.chars
		s.revealAt = time.Now()
		return tea.Batch(s.prefetchPosters(a), a.tick())
	case tea.MouseMsg:
		if m.Action == tea.MouseActionPress {
			switch m.Button {
			case tea.MouseButtonWheelUp:
				return s.move(a, -1)
			case tea.MouseButtonWheelDown:
				return s.move(a, 1)
			}
		}
	case tea.KeyMsg:
		if s.filtering {
			switch m.String() {
			case "esc":
				s.filtering = false
				s.filter.Set("")
				s.cur, s.off = 0, 0
			case "enter", "down":
				s.filtering = false
			default:
				s.filter.handle(m)
				s.cur, s.off, s.descOff = 0, 0, 0
			}
			return a.tick()
		}
		switch m.String() {
		case "esc", "q", "backspace", "c":
			if s.filter.Value() != "" && m.String() == "esc" {
				s.filter.Set("")
				s.cur, s.off = 0, 0
				return a.tick()
			}
			a.pop()
			return nil
		case "/":
			s.filtering = true
			return nil
		case "up", "k":
			return s.move(a, -1)
		case "down", "j":
			return s.move(a, 1)
		case "pgup":
			return s.move(a, -8)
		case "pgdown":
			return s.move(a, 8)
		case "home", "g":
			return s.move(a, -len(s.chars))
		case "end", "G":
			return s.move(a, len(s.chars))
		case "J", "shift+down":
			s.descOff++
		case "K", "shift+up":
			s.descOff = max(0, s.descOff-1)
		case "v", "tab":
			s.showVA = !s.showVA
			a.flash()
			if s.showVA {
				a.notify("portre: seslendiren sanatçı (seiyuu)", "ok")
			} else {
				a.notify("portre: karakter", "ok")
			}
			return tea.Batch(s.prefetchPosters(a), a.tick())
		case "x":
			s.showSpoiler = !s.showSpoiler
			a.flash()
			if s.showSpoiler {
				a.notify("spoiler metinleri gösteriliyor", "warn")
			} else {
				a.notify("spoiler metinleri gizlendi", "ok")
			}
			return a.tick()
		case "r":
			if s.err != nil {
				s.err = nil
				s.loading = true
				return s.start(a)
			}
		}
	}
	return nil
}

func (s *charactersScreen) view(a *App, w, ht int) string {
	pad := 2
	inner := w - pad*2
	mainCount, supCount := 0, 0
	for _, c := range s.chars {
		if c.Role == "MAIN" {
			mainCount++
		} else {
			supCount++
		}
	}
	status := ""
	if len(s.chars) > 0 {
		status = fmt.Sprintf("%d ana · %d yardımcı karakter", mainCount, supCount)
	}
	head := pageHeadIn(
		truncate(s.an.Title, inner*2/3)+" — Karakterler & Kadro",
		"↑↓ karakter seç  ·  v karakter/seiyuu portresi  ·  x spoiler aç/kapat  ·  J/K biyografi kaydır",
		status,
		inner,
		ht,
	)
	h := ht - len(head)
	lw := inner * 44 / 100
	rw := inner - lw - kitGap
	if inner < 84 {
		lw = inner
		rw = 0
	}

	acc := a.posters.accent(s.an.Poster, cAccent).hex()
	vis := s.visible()
	if s.cur >= 0 && s.cur < len(vis) && vis[s.cur].Image.Large != "" {
		acc = a.posters.accent(vis[s.cur].Image.Large, acc).hex()
	}

	left := s.listPane(a, vis, lw-4, h-2, acc)
	cols := [][]string{panel("❖ karakterler", left, lw, h, true, acc, fmt.Sprintf("%d", len(vis)))}
	if rw > 0 {
		right := s.detailPane(a, vis, rw-4, h-2, acc)
		cols = append(cols, panel("karakter & seiyuu ayrıntısı", right, rw, h, false, acc))
	}
	lines := append(head, hjoin(kitGap, cols...)...)
	for i := range lines {
		lines[i] = strings.Repeat(" ", pad) + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (s *charactersScreen) listPane(a *App, vis []anilist.Character, w, h int, acc string) []string {
	var out []string
	if s.filtering || s.filter.Value() != "" {
		out = append(out, s.filter.view(w-1, s.filtering, a.frame)...)
		h -= len(out)
	}
	switch {
	case s.loading:
		for i := 0; i < min(8, h); i++ {
			t := (sin(float64(a.frame)/6+float64(i)*.7) + 1) / 2
			c := mix(hex(cSurface), hex(cShade), t).hex()
			out = append(out, "  "+fg(c).Render("●")+"  "+fg(c).Render(strings.Repeat("▬", 12+int(hash(i+5)*18))))
		}
		return out
	case s.err != nil:
		return append(out,
			"",
			stErr.Render("✗ karakterler alınamadı"),
			stMuted.Render(truncate(shortErr(s.err), w)),
			"",
			stSub.Render("r tekrar dene · esc geri"),
		)
	case len(vis) == 0:
		return append(out, "", stSub.Render("Eşleşen karakter bulunamadı."))
	}

	s.cur = clamp(s.cur, 0, len(vis)-1)
	s.off = scroll(s.cur, s.off, h, len(vis))
	band := s.gl.row(a, s.cur)
	var rows []string
	for i := s.off; i < len(vis) && len(rows) < h; i++ {
		c := vis[i]
		rv := reveal(s.revealAt, i-s.off, a.motion())
		if rv <= 0 {
			rows = append(rows, "")
			continue
		}
		sel := i == band
		bg, bar := "", sp("  ", cText)
		tc := mix(hex(cFaint), hex(cText), rv).hex()
		roleDot := sp("○ ", cMuted)
		roleBadge := sp("YRD ", cMuted)
		if c.Role == "MAIN" {
			roleDot = sp("★ ", cWarn)
			roleBadge = sp("ANA ", cWarn)
		}
		if sel {
			bg, bar, tc = cBand, sp("▌ ", acc), acc
		}
		var right []span
		if len(c.VoiceActors) > 0 && w >= 38 {
			vaName := truncate(c.VoiceActors[0].Name.Full, max(10, w/3))
			right = append(right, sp("♫ "+vaName+"  ", cSub))
		}
		right = append(right, roleBadge)
		line := row(w-1, bg, []span{bar, roleDot, span{t: c.Name.Full, c: tc, bold: sel}}, right)
		if i == s.cur {
			line = anchor() + line
		}
		rows = append(rows, line)
	}
	rows = block(rows, w-1, h)
	return append(out, hjoin(0, rows, scrollbar(h, s.off, len(vis)))...)
}

func (s *charactersScreen) detailPane(a *App, vis []anilist.Character, w, h int, acc string) []string {
	if s.loading {
		return []string{"", fg(acc).Render(spinner(a.frame/2)) + stMuted.Render(" karakter bilgileri yükleniyor…")}
	}
	if s.cur < 0 || s.cur >= len(vis) {
		return []string{"", stMuted.Render("Seçili karakter yok.")}
	}
	c := vis[s.cur]

	posterURL := c.Image.Large
	posterModeLabel := "Karakter Portresi"
	if s.showVA && len(c.VoiceActors) > 0 && c.VoiceActors[0].Image.Large != "" {
		posterURL = c.VoiceActors[0].Image.Large
		posterModeLabel = "Seiyuu: " + c.VoiceActors[0].Name.Full
	}
	if posterURL != s.curPost {
		s.prevPost, s.curPost = s.curPost, posterURL
		if a.motion() {
			s.posterAt = newTween(340 * time.Millisecond)
		}
	}

	pw := min(24, w/3)
	ph := min(pw*3/4, max(8, h/2))
	sideBySide := a.cfg.Posters && w >= 54 && h >= 14 && posterURL != ""
	tw := w
	if sideBySide {
		tw = w - pw - 3
	}

	var info []string
	for i, l := range wrap(c.Name.Full, tw) {
		if i == 0 {
			info = append(info, gradient(l, []string{acc, cAccent2}, true))
		} else {
			info = append(info, stBold.Render(l))
		}
	}
	if c.Name.Native != "" {
		info = append(info, stSub.Render(c.Name.Native))
	}
	if len(c.Name.Alternative) > 0 {
		alts := strings.Join(c.Name.Alternative, " · ")
		info = append(info, stMuted.Italic(true).Render(truncate(alts, tw)))
	}
	info = append(info, "")

	roleLabel := "◆ YARDIMCI KARAKTER"
	roleCol := cAccent2
	if c.Role == "MAIN" {
		roleLabel = "★ ANA KARAKTER"
		roleCol = cWarn
	}
	badgeLine := pill(roleLabel, roleCol)
	if c.Favourites > 0 {
		badgeLine += "  " + fg(cAccent).Render(ic("heart")+" ") + stBold.Render(fmt.Sprint(c.Favourites)) + stMuted.Render(" favori")
	}
	info = append(info, badgeLine)

	var facts []string
	if g := genderTR(c.Gender); g != "" {
		facts = append(facts, "Cinsiyet: "+g)
	}
	if c.Age != "" && c.Age != "-" {
		facts = append(facts, "Yaş: "+strings.TrimSpace(c.Age))
	}
	if c.DateOfBirth.Day > 0 && c.DateOfBirth.Month > 0 {
		facts = append(facts, fmt.Sprintf("Doğum: %d %s", c.DateOfBirth.Day, monthTR(c.DateOfBirth.Month)))
	}
	if c.BloodType != "" {
		facts = append(facts, "Kan: "+c.BloodType)
	}
	if len(facts) > 0 {
		info = append(info, "", stSub.Render(strings.Join(facts, "  ·  ")))
	}

	// Voice Actor (Seiyuu) card section
	if len(c.VoiceActors) > 0 {
		va := c.VoiceActors[0]
		info = append(info, "")
		info = append(info, sectionTitle("seslendiren (seiyuu)", tw, false))
		vaLine := fg(cAccent2).Render("♫ ") + stBold.Render(va.Name.Full)
		if va.Name.Native != "" {
			vaLine += stMuted.Render("  (" + va.Name.Native + ")")
		}
		info = append(info, vaLine)
		var vaMeta []string
		if va.HomeTown != "" {
			vaMeta = append(vaMeta, truncate(va.HomeTown, 24))
		}
		if va.Age > 0 {
			vaMeta = append(vaMeta, fmt.Sprintf("%d yaş", va.Age))
		}
		if len(vaMeta) > 0 {
			info = append(info, stMuted.Render("  "+strings.Join(vaMeta, " · ")))
		}
		toggleTxt := "v: seiyuu fotoğrafını göster"
		if s.showVA {
			toggleTxt = "v: karakter portresine dön"
		}
		info = append(info, stFaint.Render("  ["+toggleTxt+"]"))
	}

	var out []string
	if sideBySide {
		var pLines []string
		if s.posterAt.p() < 1 && s.prevPost != "" {
			pLines = a.posters.renderFade(s.prevPost, posterURL, pw, ph, s.posterAt.p(), a.frame)
		} else {
			pLines = a.posters.render(posterURL, pw, ph, a.frame)
		}
		pLines = append(pLines, center(stMuted.Render(truncate(posterModeLabel, pw)), pw))
		bh := max(len(pLines), len(info))
		out = append(out, hjoin(3, block(pLines, pw, bh), block(info, tw, bh))...)
	} else {
		out = append(out, info...)
	}

	out = append(out, "", sectionTitle("karakter biyografisi", w, false))
	bio, hadSpoilers := cleanCharacterBio(c.Description, s.showSpoiler)
	if bio == "" {
		bio = "Bu karakter için henüz biyografi eklenmemiş."
	}
	if hadSpoilers && !s.showSpoiler {
		out = append(out, fg(cWarn).Render("⚑ Biyografideki spoiler kısımlar gizlendi (görmek için x tuşuna basın)."))
	} else if hadSpoilers && s.showSpoiler {
		out = append(out, fg(cWarn).Render("⚑ Spoiler kısımlar açık (gizlemek için x tuşuna basın)."))
	}
	bioLines := wrap(bio, w)
	s.descOff = clamp(s.descOff, 0, max(0, len(bioLines)-1))
	for _, l := range bioLines[s.descOff:] {
		out = append(out, stSub.Render(l))
	}
	return block(out, w, h)
}

var (
	reSpoiler  = regexp.MustCompile(`(?s)~!(.*?)!~`)
	reMdLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reMdBold   = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	reMdItalic = regexp.MustCompile(`\*([^*]+)\*|_([^_]+)_`)
)

func cleanCharacterBio(raw string, showSpoilers bool) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	s := anilist.CleanDescription(raw)
	hadSpoiler := reSpoiler.MatchString(s)
	if hadSpoiler {
		if showSpoilers {
			s = reSpoiler.ReplaceAllString(s, "[SPOILER: $1]")
		} else {
			s = reSpoiler.ReplaceAllString(s, " [🔒 Spoiler gizlendi] ")
		}
	}
	s = reMdLink.ReplaceAllString(s, "$1")
	s = reMdBold.ReplaceAllString(s, "$1$2")
	s = reMdItalic.ReplaceAllString(s, "$1$2")
	return strings.TrimSpace(s), hadSpoiler
}

func genderTR(g string) string {
	switch strings.ToLower(strings.TrimSpace(g)) {
	case "male":
		return "Erkek"
	case "female":
		return "Kadın"
	case "non-binary":
		return "Non-binary"
	}
	return g
}

func monthTR(m int) string {
	months := []string{"", "Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}
	if m >= 1 && m <= 12 {
		return months[m]
	}
	return fmt.Sprint(m)
}
