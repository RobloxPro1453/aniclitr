package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Posters are drawn with "▀" half blocks: each cell carries two truecolor
// pixels (fg = top, bg = bottom), so a 24-col poster is 24×36 real pixels.
// Scaled bitmaps and rendered rows are cached per size; cross-fades blend
// two cached bitmaps per frame.

// Memory: a poster keeps only a small master in RAM (≈55 KB). The sixel
// path needs the full picture once per size; its worker reads the cover back
// from the disk cache (kapaklar/, a ~1 ms read) and the encoded sixel is
// cached. Covers not drawn for a while are dropped (trim) and come back from
// disk on their own when drawn again.

const (
	smallW     = 96  // half-block master width: the largest block poster is ~60 px wide
	fullW      = 480 // sixel source width (as before: same pixels on screen)
	maxPosters = 160 // decoded masters kept (~9 MB)
	keepFresh  = 10 * time.Second
)

type posterState struct {
	img      image.Image // ≤96px master for half-block rendering and the accent colour
	raw      []byte      // the file itself, only when it is not on disk (rare)
	accent   rgb
	loading  bool
	err      error
	loadedAt time.Time
	used     time.Time // last drawn or asked about (trim keeps fresh ones)
}

type posterMsg struct {
	url    string
	img    image.Image
	raw    []byte
	accent rgb
	err    error
}

type posterCache struct {
	items    map[string]*posterState
	scaled   map[string]*image.RGBA
	rendered map[string][]string
	evicted  map[string]bool // trimmed: reload when drawn again
	reload   []string        // trimmed covers drawn again, fetched on the next update
	gfx      *gfx
}

func newPosterCache(g *gfx) *posterCache {
	p := &posterCache{items: map[string]*posterState{}, scaled: map[string]*image.RGBA{}, rendered: map[string][]string{},
		evicted: map[string]bool{}, gfx: g}
	g.posters = p
	return p
}

func (p *posterCache) sixel() bool { return p.gfx != nil && p.gfx.on }

// want returns a fetch command if url is not yet known.
func (p *posterCache) want(url string) tea.Cmd {
	if url == "" {
		return nil
	}
	if _, ok := p.items[url]; ok {
		return nil
	}
	p.items[url] = &posterState{loading: true}
	return guard("poster", func() tea.Msg { return fetchPoster(url) })
}

func (p *posterCache) set(m posterMsg) {
	now := time.Now()
	loadedAt := now
	if p.evicted[m.url] {
		// a cover coming back from the disk cache was already seen: no
		// fade-in, it simply is there again
		loadedAt = now.Add(-time.Hour)
		delete(p.evicted, m.url)
	}
	p.items[m.url] = &posterState{img: m.img, raw: m.raw, err: m.err, accent: m.accent, loadedAt: loadedAt, used: now}
	p.trim()
}

func (p *posterCache) ready(url string) bool {
	st, ok := p.items[url]
	if ok {
		st.used = time.Now()
	}
	return ok && st.img != nil
}

// trim drops the masters of covers not drawn for a while once there are more
// than maxPosters, oldest first. Their sized copies go too; the disk cache
// brings any of them back when it is drawn again.
func (p *posterCache) trim() {
	if len(p.items) <= maxPosters {
		return
	}
	type ent struct {
		url  string
		used time.Time
	}
	var old []ent
	for u, st := range p.items {
		if !st.loading && time.Since(st.used) > keepFresh {
			old = append(old, ent{u, st.used})
		}
	}
	sort.Slice(old, func(i, j int) bool { return old[i].used.Before(old[j].used) })
	for _, e := range old {
		if len(p.items) <= maxPosters {
			break
		}
		if p.items[e.url].err == nil {
			p.evicted[e.url] = true
		}
		delete(p.items, e.url)
		p.dropSized(e.url)
	}
}

// dropSized forgets the per-size copies of one cover.
func (p *posterCache) dropSized(url string) {
	pre := url + "|"
	for k := range p.scaled {
		if strings.HasPrefix(k, pre) {
			delete(p.scaled, k)
		}
	}
	for k := range p.rendered {
		if strings.HasPrefix(k, pre) {
			delete(p.rendered, k)
		}
	}
}

// resized: per-size copies of the old size are dead weight now.
func (p *posterCache) resized() {
	clear(p.scaled)
	clear(p.rendered)
}

// takeReloads hands out fetches for trimmed covers that are drawn again.
func (p *posterCache) takeReloads() []tea.Cmd {
	var cmds []tea.Cmd
	for _, u := range p.reload {
		u := u
		cmds = append(cmds, guard("poster", func() tea.Msg { return fetchPoster(u) }))
	}
	p.reload = p.reload[:0]
	return cmds
}

// accent returns the poster's signature colour, eased in after it loads.
func (p *posterCache) accent(url string, fallback string) rgb {
	def := hex(fallback)
	st, ok := p.items[url]
	if !ok || st.img == nil {
		return def
	}
	st.used = time.Now()
	if p.sixel() {
		// a colour fade next to a sixel image would force the image to be
		// re-sent every frame (its rows get repainted) — switch instantly
		return st.accent
	}
	t := easeInOutSine(float64(time.Since(st.loadedAt)) / float64(600*time.Millisecond))
	return mix(def, st.accent, t)
}

func (p *posterCache) fading() bool {
	if p.sixel() {
		return false
	}
	for _, st := range p.items {
		if st.img != nil && time.Since(st.loadedAt) < 700*time.Millisecond {
			return true
		}
	}
	return false
}

func fetchPoster(url string) posterMsg {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// TMDB serves huge originals; ask for a rendition that fits sixel posters.
	body, err := posterBytes(ctx, url)
	if err != nil {
		return posterMsg{url: url, err: err}
	}
	if len(body) > 15<<20 {
		return posterMsg{url: url, err: fmt.Errorf("görsel çok büyük")}
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return posterMsg{url: url, err: err}
	}
	// straight to the small master (no 480px copy in between: that one is
	// only built, on demand, for a sixel encode)
	small := downscale(img, smallW)
	m := posterMsg{url: url, img: small, accent: dominant(small)}
	if !posterOnDisk(url) {
		m.raw = body // nowhere to read it back from: keep the file (not the pixels)
	}
	return m
}

// fullImage is the sixel source: the cover read back from disk (or the kept
// file), decoded and brought to the usual 480px. Runs on encode workers.
func fullImage(url string, raw []byte) image.Image {
	body := raw
	if body == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		b, err := posterBytes(ctx, url)
		if err != nil {
			return nil
		}
		body = b
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	return downscale(img, fullW)
}

// downscale limits width to maxW (keeping aspect); small images pass through.
func downscale(img image.Image, maxW int) image.Image {
	b := img.Bounds()
	if b.Dx() <= maxW || b.Dx() == 0 {
		return img
	}
	h := max(1, b.Dy()*maxW/b.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, maxW, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// dominant finds a vivid representative colour (saturation-weighted mean),
// then lifts it so it reads as an accent on a dark terminal.
func dominant(img image.Image) rgb {
	b := img.Bounds()
	var sr, sg, sb, sw float64
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8), float64(g>>8), float64(bl>>8)
			mx := math.Max(fr, math.Max(fg, fb))
			mn := math.Min(fr, math.Min(fg, fb))
			sat := 0.0
			if mx > 0 {
				sat = (mx - mn) / mx
			}
			w := sat*sat*(mx/255) + .002
			sr, sg, sb, sw = sr+fr*w, sg+fg*w, sb+fb*w, sw+w
		}
	}
	c := rgb{sr / sw, sg / sw, sb / sw}
	return liftAccent(c)
}

// liftAccent pushes a colour into a readable, saturated band (HSL L≈.68, S≥.55).
func liftAccent(c rgb) rgb {
	r, g, b := c.r/255, c.g/255, c.b/255
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	l := (mx + mn) / 2
	var h, s float64
	if mx != mn {
		d := mx - mn
		if l > .5 {
			s = d / (2 - mx - mn)
		} else {
			s = d / (mx + mn)
		}
		switch mx {
		case r:
			h = (g - b) / d
			if g < b {
				h += 6
			}
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h /= 6
	} else {
		return hex(cAccent) // greyscale art: keep the brand accent
	}
	s = math.Max(s, .55)
	l = .68
	return hsl(h, s, l)
}

func hsl(h, s, l float64) rgb {
	hue := func(p, q, t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < .5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	q := l * (1 + s)
	if l >= .5 {
		q = l + s - l*s
	}
	p := 2*l - q
	return rgb{hue(p, q, h+1.0/3) * 255, hue(p, q, h) * 255, hue(p, q, h-1.0/3) * 255}
}

func (p *posterCache) scaledFor(url string, w, h int) *image.RGBA {
	st, ok := p.items[url]
	if url == "" || !ok || st.img == nil {
		return nil
	}
	st.used = time.Now()
	key := fmt.Sprintf("%s|%d|%d", url, w, h)
	if s, ok := p.scaled[key]; ok {
		return s
	}
	src := st.img
	sb := src.Bounds()
	pw, ph := w, h*2
	ta := float64(pw) / float64(ph)
	sa := float64(sb.Dx()) / float64(sb.Dy())
	crop := sb // cover-crop to the target aspect
	if sa > ta {
		nw := int(float64(sb.Dy()) * ta)
		x0 := sb.Min.X + (sb.Dx()-nw)/2
		crop = image.Rect(x0, sb.Min.Y, x0+nw, sb.Max.Y)
	} else if sa < ta {
		nh := int(float64(sb.Dx()) / ta)
		y0 := sb.Min.Y + (sb.Dy()-nh)/2
		crop = image.Rect(sb.Min.X, y0, sb.Max.X, y0+nh)
	}
	dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	p.scaled[key] = dst
	return dst
}

// render returns the poster as w×h cells, or a placeholder.
func (p *posterCache) render(url string, w, h int, frame int) []string {
	if _, ok := p.items[url]; !ok && p.evicted[url] {
		// trimmed earlier and wanted again: back from disk on the next update
		p.items[url] = &posterState{loading: true, used: time.Now()}
		p.reload = append(p.reload, url)
	}
	if p.sixel() && p.ready(url) {
		if p.gfx.ready(url, w, h) && p.gfx.encode(placeReq{url: url, w: w, h: h}) != "" {
			return p.gfx.place(url, w, h)
		}
		raw := p.items[url].raw
		p.gfx.request(url, w, h, func() image.Image { return fullImage(url, raw) }) // sharpen to sixel once encoded
	}
	img := p.scaledFor(url, w, h)
	if img == nil {
		st, ok := p.items[url]
		return placeholder(w, h, ok && st.loading, frame)
	}
	key := fmt.Sprintf("%s|%d|%d", url, w, h)
	if r, ok := p.rendered[key]; ok {
		// a just-loaded poster fades up from the placeholder
		st := p.items[url]
		if t := float64(time.Since(st.loadedAt)) / float64(350*time.Millisecond); t < 1 {
			return halfblocks(img, nil, w, h, easeOutCubic(t))
		}
		return r
	}
	r := halfblocks(img, nil, w, h, 1)
	p.rendered[key] = r
	return r
}

// renderFade cross-fades from one poster to another (t: 0→1).
func (p *posterCache) renderFade(from, to string, w, h int, t float64, frame int) []string {
	if t >= 1 || from == to || p.sixel() {
		return p.render(to, w, h, frame)
	}
	a, b := p.scaledFor(from, w, h), p.scaledFor(to, w, h)
	if a == nil || b == nil {
		return p.render(to, w, h, frame)
	}
	return halfblocks(b, a, w, h, easeInOutSine(t))
}

// halfblocks renders img (optionally blended over under, or over the
// placeholder surface when under is nil and alpha<1).
func halfblocks(img, under *image.RGBA, w, h int, alpha float64) []string {
	px := func(x, y int) color.RGBA {
		c := img.RGBAAt(x, y)
		if alpha >= 1 {
			return c
		}
		base := hex(cSurface)
		if under != nil {
			u := under.RGBAAt(x, y)
			base = rgb{float64(u.R), float64(u.G), float64(u.B)}
		}
		m := mix(base, rgb{float64(c.R), float64(c.G), float64(c.B)}, alpha)
		return color.RGBA{uint8(m.r), uint8(m.g), uint8(m.b), 255}
	}
	lines := make([]string, h)
	var b strings.Builder
	for y := 0; y < h; y++ {
		b.Reset()
		var lastF, lastB color.RGBA
		first := true
		for x := 0; x < w; x++ {
			t := px(x, y*2)
			u := px(x, y*2+1)
			if (x == 0 || x == w-1) && (y == 0 || y == h-1) { // rounded corners
				b.WriteString("\x1b[0m")
				first = true
				if y == 0 {
					fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm▄", u.R, u.G, u.B)
				} else {
					fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm▀", t.R, t.G, t.B)
				}
				b.WriteString("\x1b[0m")
				continue
			}
			if first || t != lastF {
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", t.R, t.G, t.B)
			}
			if first || u != lastB {
				fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm", u.R, u.G, u.B)
			}
			first = false
			lastF, lastB = t, u
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		lines[y] = b.String()
	}
	return lines
}

// placeholder is a quiet card; a soft light sweeps across it while loading.
func placeholder(w, h int, loading bool, frame int) []string {
	lines := make([]string, h)
	base := hex(cSurface)
	hi := hex(cShade)
	var b strings.Builder
	for y := 0; y < h; y++ {
		b.Reset()
		for x := 0; x < w; x++ {
			t := 0.0
			if loading {
				period := float64(w + h*2 + 12)
				d := math.Mod(float64(x+y*2)-float64(frame)*.9, period)
				if d < 0 {
					d += period
				}
				if d < 6 {
					t = math.Sin(d / 6 * math.Pi)
				}
			}
			c := mix(base, hi, t)
			fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm ", int(c.r), int(c.g), int(c.b))
		}
		b.WriteString("\x1b[0m")
		lines[y] = b.String()
	}
	if h > 2 && !loading {
		label := "kapak yok"
		pad := max(0, (w-len([]rune(label)))/2)
		lines[h/2] = fill(pad, cSurface) + fg(cMuted).Background(colorOf(cSurface)).Render(truncate(label, w)) + fill(max(0, w-pad-len([]rune(label))), cSurface)
	}
	return lines
}
