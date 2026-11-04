package ui

import (
	"math"
	"time"
)

// Cover theme ("album art" theming): while an anime is open, the UI's
// accents, selection band and surfaces take on its cover's colours, then
// glide back to your theme when you leave. The ground colour stays, so
// sixel posters (whose rounded corners are painted in it) stay valid.

// coverTheme tints base with a cover colour.
func coverTheme(base Theme, acc rgb) Theme {
	t := base
	t.Name = base.Name // the palette picker still shows your theme as current
	t.Accent = acc.hex()
	t.Accent2 = hueShift(acc, 38).hex()
	t.Sky = hueShift(acc, -48).hex()
	t.Band = mix(hex(base.Band), acc, .16).hex()
	t.Surface = mix(hex(base.Surface), acc, .07).hex()
	t.Faint = mix(hex(base.Faint), acc, .22).hex()
	return t
}

// hueShift rotates a colour's hue by deg degrees (keeps lightness/saturation).
func hueShift(c rgb, deg float64) rgb {
	r, g, b := c.r/255, c.g/255, c.b/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l := (mx + mn) / 2
	if mx == mn {
		return c
	}
	d := mx - mn
	s := d / (1 - math.Abs(2*l-1))
	var h float64
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h = math.Mod(h*60+deg+360, 360)
	return hsl(h/360, s, l)
}

// coverURL: the poster of the anime the top screen is about ("" = none).
func (a *App) coverURL() string {
	switch s := a.top().(type) {
	case *animeScreen:
		return s.an.Poster
	case *picker:
		return s.an.Poster
	case *playing:
		return s.an.Poster
	}
	return ""
}

// applyCover morphs to the cover palette (or back) when the subject changes.
func (a *App) applyCover() {
	url := ""
	if a.cfg.CoverTheme {
		url = a.coverURL()
		if st, ok := a.posters.items[url]; !ok || st.img == nil {
			url = "" // not loaded yet: keep the base theme until it is
		}
	}
	if url == a.coverAt {
		return
	}
	a.coverAt = url
	base := themeByName(a.cfg.Theme)
	if a.pal != nil && a.pal.previewd {
		return // a theme preview is running: leave the palette alone
	}
	target := base
	if url != "" {
		target = coverTheme(base, a.posters.items[url].accent)
	}
	if a.motion() {
		a.tfade = &themeFade{from: currentTheme(), to: target, tw: newTween(500 * time.Millisecond)}
	} else {
		applyTheme(target)
	}
}
