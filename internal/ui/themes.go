package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/logx"
)

// Theme is a complete palette. Built-ins follow well-known rices; users can
// drop their own JSON files into %APPDATA%\aniclitr\themes (same field names).
type Theme struct {
	Name    string `json:"name"`
	Bg      string `json:"bg"`
	Text    string `json:"text"`
	Sub     string `json:"sub"`
	Muted   string `json:"muted"`
	Faint   string `json:"faint"`
	Band    string `json:"band"`
	Surface string `json:"surface"`
	Accent  string `json:"accent"`
	Accent2 string `json:"accent2"`
	Sky     string `json:"sky"`
	OK      string `json:"ok"`
	Warn    string `json:"warn"`
	Err     string `json:"err"`
	Light   bool   `json:"light,omitempty"`
}

var builtinThemes = []Theme{
	{Name: "gece", Bg: "#0E0C14", Text: "#ECEAF4", Sub: "#B4B0C6", Muted: "#858198", Faint: "#3B3849", Band: "#1D1A27", Surface: "#15131D",
		Accent: "#FF6AA2", Accent2: "#B57BFF", Sky: "#6AB8FF", OK: "#6BD39A", Warn: "#F2C14E", Err: "#FF6B7A"},
	{Name: "catppuccin", Bg: "#1E1E2E", Text: "#CDD6F4", Sub: "#BAC2DE", Muted: "#8C91AB", Faint: "#45475A", Band: "#313244", Surface: "#181825",
		Accent: "#F5C2E7", Accent2: "#CBA6F7", Sky: "#89DCEB", OK: "#A6E3A1", Warn: "#F9E2AF", Err: "#F38BA8"},
	{Name: "tokyo night", Bg: "#1A1B26", Text: "#C0CAF5", Sub: "#A9B1D6", Muted: "#7D84AC", Faint: "#3B4261", Band: "#292E42", Surface: "#16161E",
		Accent: "#BB9AF7", Accent2: "#7AA2F7", Sky: "#7DCFFF", OK: "#9ECE6A", Warn: "#E0AF68", Err: "#F7768E"},
	{Name: "rosé pine", Bg: "#191724", Text: "#E0DEF4", Sub: "#C9C5DE", Muted: "#908CAA", Faint: "#403D52", Band: "#26233A", Surface: "#1F1D2E",
		Accent: "#EBBCBA", Accent2: "#C4A7E7", Sky: "#9CCFD8", OK: "#8BD5A0", Warn: "#F6C177", Err: "#EB6F92"},
	{Name: "gruvbox", Bg: "#282828", Text: "#EBDBB2", Sub: "#D5C4A1", Muted: "#A89984", Faint: "#504945", Band: "#3C3836", Surface: "#32302F",
		Accent: "#FE8019", Accent2: "#D3869B", Sky: "#83A598", OK: "#B8BB26", Warn: "#FABD2F", Err: "#FB4934"},
	{Name: "nord", Bg: "#2E3440", Text: "#ECEFF4", Sub: "#D8DEE9", Muted: "#A3ABB9", Faint: "#4C566A", Band: "#3B4252", Surface: "#353C4A",
		Accent: "#88C0D0", Accent2: "#B48EAD", Sky: "#81A1C1", OK: "#A3BE8C", Warn: "#EBCB8B", Err: "#BF616A"},
	{Name: "matcha", Bg: "#141A16", Text: "#E4EEDF", Sub: "#BCCBB6", Muted: "#869A86", Faint: "#34413A", Band: "#1F2A23", Surface: "#18201B",
		Accent: "#9BE08F", Accent2: "#E6C77A", Sky: "#7FC8C0", OK: "#9BE08F", Warn: "#E6C77A", Err: "#F08A7E"},
	{Name: "şafak", Light: true, Bg: "#FAF6F4", Text: "#2A2433", Sub: "#4F4760", Muted: "#6B6380", Faint: "#DCD3DF", Band: "#F1E5EE", Surface: "#F4EDF1",
		Accent: "#D6337A", Accent2: "#7B4BD6", Sky: "#2F7FD0", OK: "#1F8A53", Warn: "#A06A00", Err: "#C9303F"},
}

// themes returns built-ins plus valid user themes, in a stable order.
func themes() []Theme {
	out := append([]Theme(nil), builtinThemes...)
	dir := filepath.Join(config.Dir(), "themes")
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var t Theme
		if err := json.Unmarshal(b, &t); err != nil || !t.valid() {
			logx.Errorf("tema okunamadı: %s (%v)", f, err)
			continue
		}
		if t.Name == "" {
			t.Name = strings.TrimSuffix(filepath.Base(f), ".json")
		}
		out = append(out, t)
	}
	return out
}

func (t Theme) valid() bool {
	for _, c := range []string{t.Bg, t.Text, t.Sub, t.Muted, t.Faint, t.Band, t.Surface, t.Accent, t.Accent2, t.Sky, t.OK, t.Warn, t.Err} {
		if len(c) != 7 || c[0] != '#' {
			return false
		}
	}
	return true
}

func themeByName(name string) Theme {
	for _, t := range themes() {
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return builtinThemes[0]
}

// current reads the palette back as a Theme (start point of a transition).
func currentTheme() Theme {
	return Theme{Bg: cBg, Text: cText, Sub: cSub, Muted: cMuted, Faint: cFaint, Band: cBand, Surface: cSurface,
		Accent: cAccent, Accent2: cAccent2, Sky: cSky, OK: cOK, Warn: cWarn, Err: cErr}
}

func lerpTheme(a, b Theme, t float64) Theme {
	m := func(x, y string) string { return mix(hex(x), hex(y), t).hex() }
	return Theme{Name: b.Name, Light: b.Light, Bg: m(a.Bg, b.Bg), Text: m(a.Text, b.Text), Sub: m(a.Sub, b.Sub), Muted: m(a.Muted, b.Muted),
		Faint: m(a.Faint, b.Faint), Band: m(a.Band, b.Band), Surface: m(a.Surface, b.Surface), Accent: m(a.Accent, b.Accent),
		Accent2: m(a.Accent2, b.Accent2), Sky: m(a.Sky, b.Sky), OK: m(a.OK, b.OK), Warn: m(a.Warn, b.Warn), Err: m(a.Err, b.Err)}
}

// applyTheme swaps the global palette (all rendering reads these vars).
func applyTheme(t Theme) {
	cBg, cText, cSub, cMuted, cFaint = t.Bg, t.Text, t.Sub, t.Muted, t.Faint
	cBand, cSurface = t.Band, t.Surface
	cAccent, cAccent2, cSky = t.Accent, t.Accent2, t.Sky
	cOK, cWarn, cErr = t.OK, t.Warn, t.Err
	cInk = t.Bg
	if t.Light {
		cInk = "#FFFFFF"
	}
	cShade = mix(hex(t.Surface), hex(t.Text), .12).hex()
	brand = []string{cAccent, cAccent2, cSky}
	stText, stSub, stMuted, stFaint = fg(cText), fg(cSub), fg(cMuted), fg(cFaint)
	stAccent, stBold = fg(cAccent), fg(cText).Bold(true)
	stOK, stWarn, stErr = fg(cOK), fg(cWarn), fg(cErr)
}

// themeFade animates the palette between two themes.
type themeFade struct {
	from, to Theme
	tw       tween
}

// setTheme switches (with a short colour morph when motion is on).
func (a *App) setTheme(t Theme) {
	if a.motion() {
		a.tfade = &themeFade{from: currentTheme(), to: t, tw: newTween(380 * time.Millisecond)}
	} else {
		applyTheme(t)
		a.tfade = nil
	}
	a.themeName = t.Name
	a.coverAt = "-" // re-evaluate the cover tint on top of the new theme
	a.gfx.retheme()
	a.oscUntil = time.Now().Add(300 * time.Millisecond)
}

// stepTheme advances a running palette morph; called once per frame.
func (a *App) stepTheme() {
	if a.tfade == nil {
		return
	}
	p := easeInOutSine(a.tfade.tw.p())
	applyTheme(lerpTheme(a.tfade.from, a.tfade.to, p))
	a.oscUntil = time.Now().Add(300 * time.Millisecond)
	if !a.tfade.tw.active() {
		applyTheme(a.tfade.to)
		a.tfade = nil
	}
}

// oscBg paints the terminal's own background so the whole window follows
// the theme (ignored by terminals that do not support OSC 11).
func oscBg(c string) string { return "\x1b]11;" + c + "\x1b\\" }

// OSCReset restores the terminal's default background (call on exit).
const OSCReset = "\x1b]111\x1b\\"

// ---- icons: Nerd Font glyphs when enabled, plain Unicode otherwise ----

var nerd bool

var icons = map[string][2]string{
	"search":   {"\uf002", "⌕"},
	"home":     {"\uf015", "⌂"},
	"library":  {"\uf02d", "▤"},
	"history":  {"\uf1da", "◉"},
	"feed":     {"\uf0e7", "◆"},
	"calendar": {"\uf073", "▦"},
	"stats":    {"\uf080", "▥"},
	"star":     {"\uf005", "★"},
	"play":     {"\uf04b", "▶"},
	"palette":  {"\uf120", "❯"},
	"theme":    {"\uf1fc", "◐"},
	"list":     {"\uf03a", "≡"},
	"check":    {"\uf00c", "✓"},
	"plus":     {"\uf067", "+"},
	"heart":    {"\uf004", "♥"},
	"fire":     {"\uf06d", "▲"},
	"clock":    {"\uf017", "◷"},
	"tv":       {"\uf26c", "▭"},
	"settings": {"\uf013", "◎"},
	"archive":  {"\uf187", "▣"},
	"exit":     {"\uf011", "×"},
	"bolt":     {"\uf0e7", "▲"},
	"spark":    {"\uf005", "◈"},
}

func ic(name string) string {
	v, ok := icons[name]
	if !ok {
		return "•"
	}
	if nerd {
		return v[0]
	}
	return v[1]
}
