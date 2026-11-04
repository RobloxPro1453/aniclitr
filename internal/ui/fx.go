package ui

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/sound"
)

// ---- glide: a springy selection band ----
//
// Lists keep a glide per cursor. The band is drawn at round(pos) while pos
// chases the cursor with a critically damped spring, so a jump of several
// rows reads as one quick slide instead of a teleport.

type glide struct {
	pos  float64
	last time.Time
	init bool
}

// row returns the list index the band should be drawn on this frame.
func (g *glide) row(a *App, target int) int {
	now := time.Now()
	// next to a picture the band jumps instead of sliding: every extra
	// frame makes the terminal redraw the image too (Windows Terminal)
	if !g.init || !a.motion() || a.picsOn {
		g.pos, g.last, g.init = float64(target), now, true
		return target
	}
	dt := now.Sub(g.last).Seconds()
	g.last = now
	if dt > .1 {
		dt = .1
	}
	d := float64(target) - g.pos
	if math.Abs(d) > 14 { // long jumps (pgdn, home): don't sweep the whole list
		g.pos = float64(target) - math.Copysign(3, d)
		d = float64(target) - g.pos
	}
	g.pos += d * (1 - math.Exp(-dt*22))
	if math.Abs(float64(target)-g.pos) < .08 {
		g.pos = float64(target)
	} else {
		a.gliding = true
	}
	return int(math.Round(g.pos))
}

// ---- anchors: invisible markers that tell effects where a row landed ----

const anchorSeq = "\x1b_ka\x1b\\"

// anchor marks the start of the selected row so bursts can spawn there.
func anchor() string { return anchorSeq }

// findAnchor removes the marker from the frame and returns its cell.
func findAnchor(lines []string) (x, y int, ok bool) {
	for i, l := range lines {
		if k := strings.Index(l, anchorSeq); k >= 0 {
			if !ok {
				x, y, ok = ansi.StringWidth(l[:k]), i, true
			}
			lines[i] = strings.ReplaceAll(l, anchorSeq, "")
		}
	}
	return
}

// ---- confetti ----

type particle struct {
	x, y, vx, vy float64
	r            rune
	c            rgb
	born, life   float64
	grav         float64 // gravity scale (0 = float)
	trail        bool    // leaves a fading streak (fireworks)
}

type confetti struct {
	ps   []particle
	last time.Time
	t    float64
}

var confettiGlyphs = []rune{'◆', '•', '◇', '·', '●', '+', '×', '○'}

// burst throws n particles from (x,y) in body coordinates.
func (c *confetti) burst(x, y float64, n int, colors []string, seed int) {
	if c.last.IsZero() {
		c.last = time.Now()
	}
	for i := 0; i < n; i++ {
		h := func(k int) float64 { return hash(seed*131 + i*17 + k) }
		ang := -math.Pi/2 + (h(1)-.5)*math.Pi*1.1
		sp := 10 + h(2)*26
		c.ps = append(c.ps, particle{
			x: x + (h(3)-.5)*4, y: y,
			vx: math.Cos(ang) * sp * 1.6, vy: math.Sin(ang) * sp * .7,
			r:    confettiGlyphs[int(h(4)*float64(len(confettiGlyphs)))%len(confettiGlyphs)],
			c:    hex(colors[int(h(5)*float64(len(colors)))%len(colors)]),
			born: c.t, life: .9 + h(6)*.8, grav: 1,
		})
	}
}

// sparks: a tiny upward fizz (typing, toggles).
func (c *confetti) sparks(x, y float64, n int, colors []string, seed int) {
	if c.last.IsZero() {
		c.last = time.Now()
	}
	for i := 0; i < n; i++ {
		h := func(k int) float64 { return hash(seed*977 + i*31 + k) }
		ang := -math.Pi/2 + (h(1)-.5)*math.Pi*.9
		sp := 5 + h(2)*9
		c.ps = append(c.ps, particle{
			x: x, y: y, vx: math.Cos(ang) * sp * 1.8, vy: math.Sin(ang) * sp * .6,
			r:    []rune{'·', '•', '+', '∙'}[int(h(4)*4)%4],
			c:    hex(colors[int(h(5)*float64(len(colors)))%len(colors)]),
			born: c.t, life: .35 + h(6)*.35, grav: .6,
		})
	}
}

// firework: a radial bloom that hangs, then droops.
func (c *confetti) firework(x, y float64, n int, colors []string, seed int) {
	if c.last.IsZero() {
		c.last = time.Now()
	}
	col := colors[int(hash(seed*7)*float64(len(colors)))%len(colors)]
	for i := 0; i < n; i++ {
		h := func(k int) float64 { return hash(seed*389 + i*13 + k) }
		ang := float64(i)/float64(n)*2*math.Pi + h(1)*.3
		sp := 9 + h(2)*7
		c.ps = append(c.ps, particle{
			x: x, y: y, vx: math.Cos(ang) * sp * 2, vy: math.Sin(ang) * sp * .9,
			r: []rune{'•', '·', '◆', '+'}[int(h(4)*4)%4], c: hex(col),
			born: c.t, life: 1 + h(6)*.6, grav: .35, trail: true,
		})
	}
}

func (c *confetti) active() bool { return len(c.ps) > 0 }

func (c *confetti) step() {
	if len(c.ps) == 0 {
		c.last = time.Time{}
		return
	}
	now := time.Now()
	dt := math.Min(now.Sub(c.last).Seconds(), .1)
	c.last = now
	c.t += dt
	keep := c.ps[:0]
	for _, p := range c.ps {
		p.vy += 26 * p.grav * dt // gravity (cells/s²; rows are tall so it reads right)
		p.vx *= math.Pow(.25, dt)
		if p.trail {
			p.vy *= math.Pow(.4, dt)
		}
		p.x += p.vx * dt
		p.y += p.vy * dt
		if c.t-p.born < p.life {
			keep = append(keep, p)
		}
	}
	c.ps = keep
}

// draw splices particles into the frame (over whatever text is there).
func (c *confetti) draw(lines []string, w int) {
	byRow := map[int][]particle{}
	all := c.ps
	for _, p := range c.ps {
		if p.trail { // two fading ghosts behind each spark
			for k, back := range []float64{.06, .12} {
				g := p
				g.x -= p.vx * back
				g.y -= p.vy * back
				g.r = '·'
				g.life = p.life * (.6 - .2*float64(k))
				g.trail = false
				all = append(all, g)
			}
		}
	}
	for _, p := range all {
		x, y := int(p.x), int(p.y)
		if x < 0 || y < 0 || x >= w || y >= len(lines) {
			continue
		}
		byRow[y] = append(byRow[y], p)
	}
	for y, ps := range byRow {
		// one pass per row, every segment cut from the original line:
		// splicing repeatedly would re-copy the escape codes each time
		sort.Slice(ps, func(i, j int) bool { return ps[i].x < ps[j].x })
		l := fit(lines[y], w)
		var b strings.Builder
		prev := 0
		for _, p := range ps {
			x := int(p.x)
			if x < prev {
				continue
			}
			b.WriteString(ansi.Cut(l, prev, x))
			fade := clamp01(1 - (c.t-p.born)/p.life)
			b.WriteString("\x1b[0m")
			sgrFG(&b, mix(hex(cBg), p.c, .35+.65*fade))
			b.WriteRune(p.r)
			b.WriteString("\x1b[0m")
			prev = x + 1
		}
		b.WriteString(ansi.Cut(l, prev, w))
		lines[y] = b.String()
	}
}

// celebrate: a burst from the selected row (or the middle of the body).
func (a *App) celebrate(n int) {
	playSound(a, sndDone)
	if !a.motion() {
		return
	}
	x, y := float64(a.w/2), float64(a.h/2)
	if a.anchorOK {
		x, y = float64(a.anchorX+12), float64(a.anchorY)
	}
	cols := []string{cAccent, cAccent2, cSky, cOK, cWarn}
	a.fx.burst(x, y, n, cols, a.frame)
	if n >= 60 { // the big moments get fireworks too
		a.fx.firework(float64(a.w)*.25, float64(a.h)*.3, 20, cols, a.frame+3)
		a.fx.firework(float64(a.w)*.75, float64(a.h)*.25, 20, cols, a.frame+11)
	}
}

// flash briefly lights the selection band (feedback for enter / toggles).
func (a *App) flash() {
	playSound(a, sndSelect)
	if a.motion() {
		a.flashT = newTween(260 * time.Millisecond)
	}
}

// ---- ambient weather ----

var ambientKinds = []string{"auto", "sakura", "kar", "yağmur", "yıldız", "ateşböceği", "yok"}

// ambientNow resolves "auto" from the season and the hour.
func ambientNow(kind string, t time.Time) string {
	if kind != "auto" && kind != "" {
		return kind
	}
	if h := t.Hour(); h >= 21 || h < 6 {
		return "yıldız"
	}
	switch t.Month() {
	case time.December, time.January, time.February:
		return "kar"
	case time.March, time.April, time.May:
		return "sakura"
	case time.June, time.July, time.August:
		return "ateşböceği"
	}
	return "yağmur"
}

type weather struct {
	kind string
	ps   []petal
	last time.Time
	w, h int
	t    float64
}

func (wt *weather) step(kind string, w, h, seed int) {
	now := time.Now()
	if wt.kind != kind || wt.w != w || wt.h != h || len(wt.ps) == 0 {
		wt.kind, wt.w, wt.h = kind, w, h
		n := max(6, w*h/220)
		switch kind {
		case "yağmur":
			n = max(10, w*h/90)
		case "yıldız":
			n = max(10, w*h/120)
		case "ateşböceği":
			n = max(5, w*h/400)
		}
		wt.ps = make([]petal, n)
		for i := range wt.ps {
			wt.ps[i] = spawnFor(kind, i+seed, w, h, true)
		}
		wt.last = now
		return
	}
	dt := math.Min(now.Sub(wt.last).Seconds(), .2)
	wt.last = now
	wt.t += dt
	for i := range wt.ps {
		q := &wt.ps[i]
		q.phase += dt * 1.3
		switch kind {
		case "yıldız":
			// stars stay; a rare shooting star streaks across
			if q.glyph == '━' {
				q.x += q.vx * dt * 18
				q.y += q.vy * dt * 3
				if q.x > float64(w) || q.y > float64(h) {
					*q = spawnFor(kind, i+int(now.UnixNano()%9973), w, h, true)
				}
			} else if hash(i+int(wt.t*.2)*977) > .9993 {
				q.glyph, q.vx, q.vy = '━', 2.5, .4
			}
			continue
		case "ateşböceği":
			q.x += math.Sin(q.phase*.7+q.tint*9) * dt * 1.5
			q.y += math.Cos(q.phase*.5+q.tint*7) * dt * .6
			if q.x < 0 || q.x >= float64(w) || q.y < 0 || q.y >= float64(h) {
				*q = spawnFor(kind, i+int(now.UnixNano()%9973), w, h, true)
			}
			continue
		case "yağmur":
			q.x += q.vx * dt
			q.y += q.vy * dt
		default: // sakura, kar
			q.x += (q.vx + math.Sin(q.phase)*.9) * dt
			q.y += q.vy * dt
		}
		if q.y >= float64(h) || q.x >= float64(w)+2 || q.x < -3 {
			*q = spawnFor(kind, i+int(now.UnixNano()%9973), w, h, false)
		}
	}
}

var (
	snowGlyphs = []rune{'·', '∙', '•', '*', '·', '+'}
	rainGlyphs = []rune{'│', '╎', '¦', '│'}
	starGlyphs = []rune{'·', '·', '∙', '◇', '·', '+', '•'}
)

func spawnFor(kind string, n, w, h int, anywhere bool) petal {
	p := spawn(n, w, h, anywhere)
	pick := func(gs []rune) rune { return gs[int(hash(n*23)*float64(len(gs)))%len(gs)] }
	switch kind {
	case "kar":
		p.glyph, p.vx, p.vy = pick(snowGlyphs), (hash(n*5)-.5)*1.2, .5+hash(n*11)*.9
	case "yağmur":
		p.glyph, p.vx, p.vy = pick(rainGlyphs), .4, 11+hash(n*11)*8
	case "yıldız":
		p.glyph = pick(starGlyphs)
		p.x, p.y = hash(n*3)*float64(w), hash(n*7)*float64(h)
	case "ateşböceği":
		p.glyph = '•'
		p.x, p.y = hash(n*3)*float64(w), hash(n*7)*float64(h)
	}
	return p
}

func (wt *weather) draw(c *canvas) {
	bg := hex(cBg)
	acc := hex(cAccent)
	for _, q := range wt.ps {
		x, y := int(q.x), int(q.y)
		if !c.empty(x, y) {
			continue
		}
		var col rgb
		switch wt.kind {
		case "kar":
			col = mix(bg, mix(hex(cText), hex(cSky), .25), .35+q.tint*.4)
		case "yağmur":
			col = mix(bg, hex(cSky), .2+q.tint*.25)
		case "yıldız":
			tw := (math.Sin(q.phase*1.7+q.tint*20) + 1) / 2
			col = mix(bg, mix(hex(cText), hex(cAccent2), q.tint*.6), .15+.55*tw*q.tint+.1)
			if q.glyph == '━' {
				col = mix(bg, hex(cText), .8)
			}
		case "ateşböceği":
			glow := math.Pow((math.Sin(q.phase*2.2+q.tint*30)+1)/2, 3)
			col = mix(bg, hex(cWarn), .1+.9*glow)
		default:
			col = mix(mix(bg, acc, .08), acc, .25+q.tint*.35)
			if q.glyph == '·' {
				col = mix(bg, acc, .2+q.tint*.2)
			}
		}
		c.set(x, y, q.glyph, col, false)
	}
}

// ---- screensaver: after a quiet minute on the home screen ----

const saverAfter = 75 * time.Second

func (a *App) saverView(w, h int) []string {
	c := newCanvas(w, h)
	kind := ambientNow(a.cfg.Ambient, time.Now())
	if kind == "yok" {
		kind = "yıldız"
	}
	a.saverWx.step(kind, w, h, 3)
	a.saverWx.draw(c)
	t := time.Since(a.saverAt).Seconds()
	// fireworks now and then
	if b := int(t / 1.6); b != a.saverPop {
		a.saverPop = b
		x := 6 + hash(b*17)*float64(max(1, w-12))
		y := 2 + hash(b*29)*float64(max(1, h/2))
		a.fx.firework(x, y, 18+int(hash(b)*14), []string{cAccent, cAccent2, cSky, cWarn, cOK}, b)
	}
	// the logo drifts and bounces off the edges (Lissajous-ish, never stalls)
	maxX, maxY := float64(max(1, w-logoWidth)), float64(max(1, h-len(logoArt)-3))
	tri := func(v float64) float64 { v = math.Mod(v, 2); return 1 - math.Abs(1-v) }
	lx := int(tri(t*.045) * maxX)
	ly := int(tri(t*.071+.3) * maxY)
	hue := t * .05
	for y, line := range logoArt {
		for x, r := range []rune(line) {
			if r == ' ' {
				continue
			}
			gt := math.Mod((float64(x)+float64(y)*1.5)/float64(logoWidth+8)+hue, 1)
			col := gradAt(append(append([]string{}, brand...), brand[0]), gt)
			if r != '█' {
				col = mix(hex(cBg), col, .4)
			}
			c.set(lx+x, ly+y, r, col, true)
		}
	}
	now := time.Now()
	clk := now.Format("15:04")
	c.text(lx+(logoWidth-len(clk))/2, ly+len(logoArt)+1, clk, hex(cSub), true)
	return c.lines()
}

// ---- UI sounds ----

const (
	sndTick   = sound.Tick
	sndSelect = sound.Select
	sndDone   = sound.Done
	sndError  = sound.Error
)

func playSound(a *App, k sound.Kind) {
	if a.cfg.Motion || k != sndTick { // reduced-motion users get no tick chatter
		sound.Play(k, a.cfg.Sounds)
	}
}
