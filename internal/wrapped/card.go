package wrapped

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// ExportPNG renders a crisp, dark-neon shareable summary card (900x1200 PNG)
// using pure Go stdlib image/png (zero external font/CGO dependencies).
func ExportPNG(r Report, dst string) (string, error) {
	if dst == "" {
		home, _ := os.UserHomeDir()
		dst = filepath.Join(home, "Desktop", fmt.Sprintf("aniclitr-ozet-%d.png", r.Year))
		if _, err := os.Stat(filepath.Dir(dst)); err != nil {
			dst = fmt.Sprintf("aniclitr-ozet-%d.png", r.Year)
		}
	}
	const W, H = 900, 1200
	img := image.NewRGBA(image.Rect(0, 0, W, H))

	// Rich dark-neon gradient background (#0E0C14 -> #1B1530 with soft pink/violet glow).
	for y := 0; y < H; y++ {
		ty := float64(y) / float64(H)
		for x := 0; x < W; x++ {
			tx := float64(x) / float64(W)
			br := 14.0 + 16.0*ty
			bg := 12.0 + 9.0*ty
			bb := 20.0 + 28.0*ty
			// Top-right sakura glow
			d1 := math.Hypot(tx-0.85, ty-0.12)
			if d1 < 0.55 {
				g := (1 - d1/0.55)
				g *= g
				br += 85 * g
				bg += 28 * g
				bb += 68 * g
			}
			// Bottom-left violet glow
			d2 := math.Hypot(tx-0.18, ty-0.82)
			if d2 < 0.6 {
				g := (1 - d2/0.6)
				g *= g
				br += 45 * g
				bg += 30 * g
				bb += 95 * g
			}
			img.SetRGBA(x, y, color.RGBA{clampU8(br), clampU8(bg), clampU8(bb), 255})
		}
	}

	pink := color.RGBA{255, 106, 162, 255}
	violet := color.RGBA{181, 123, 255, 255}
	sky := color.RGBA{106, 184, 255, 255}
	mint := color.RGBA{107, 211, 154, 255}
	gold := color.RGBA{242, 193, 78, 255}
	text := color.RGBA{236, 234, 244, 255}
	muted := color.RGBA{154, 150, 178, 255}
	panelBg := color.RGBA{24, 21, 36, 230}
	panelBorder := color.RGBA{72, 64, 102, 255}

	// Outer subtle card border.
	drawRoundedRect(img, 28, 28, W-28, H-28, 24, color.RGBA{18, 16, 28, 200}, panelBorder)

	// Header brand + year pill.
	drawText(img, 64, 68, "ANICLITR", 4, pink)
	drawRoundedRect(img, W-250, 62, W-64, 114, 14, color.RGBA{46, 30, 64, 255}, violet)
	drawText(img, W-228, 76, fmt.Sprintf("%d OZETI", r.Year), 3, text)

	// Archetype hero banner.
	drawRoundedRect(img, 60, 148, W-60, 310, 18, panelBg, pink)
	drawText(img, 88, 174, "IZLEYICI KISILIGIN", 2, muted)
	drawText(img, 88, 208, foldASCII(strings.ToUpper(r.Archetype)), 4, pink)
	drawWrapped(img, 88, 260, foldASCII(r.Tagline), 2, text, W-176)

	// 4 Key Stat Tiles (2x2).
	stats := []struct {
		lbl, val string
		col      color.RGBA
	}{
		{"TOPLAM SURE", fmt.Sprintf("%.1f SAAT", r.TotalHours), sky},
		{"IZLENEN BOLUM", fmt.Sprintf("%d BOLUM", r.TotalEpisodes), pink},
		{"TAMAMLANAN", fmt.Sprintf("%d SERI (%%%d)", r.CompletedCount, r.CompletionRate), mint},
		{"AKTIF ZAMAN", foldASCII(r.PeakPeriod), gold},
	}
	for i, st := range stats {
		col := i % 2
		row := i / 2
		x0 := 60 + col*396
		y0 := 338 + row*126
		drawRoundedRect(img, x0, y0, x0+380, y0+108, 16, panelBg, panelBorder)
		drawText(img, x0+24, y0+20, st.lbl, 2, muted)
		drawText(img, x0+24, y0+54, st.val, 3, st.col)
	}

	// Top Genres bar block.
	drawRoundedRect(img, 60, 608, W-60, 818, 18, panelBg, panelBorder)
	drawText(img, 88, 630, "FAVORI TURLER", 2, violet)
	if len(r.TopGenres) == 0 {
		drawText(img, 88, 676, "HENUZ TUR VERISI YOK", 2, muted)
	} else {
		maxC := max(1, r.TopGenres[0].Count)
		for i, g := range r.TopGenres {
			if i >= 4 {
				break
			}
			y := 668 + i*34
			drawText(img, 88, y, padRight(foldASCII(g.Name), 16), 2, text)
			barW := int(float64(420) * float64(g.Count) / float64(maxC))
			drawRoundedRect(img, 320, y+2, 320+420, y+18, 6, color.RGBA{40, 36, 58, 255}, color.RGBA{40, 36, 58, 255})
			if barW > 8 {
				drawRoundedRect(img, 320, y+2, 320+barW, y+18, 6, pink, pink)
			}
			drawText(img, 756, y, fmt.Sprintf("%d", g.Count), 2, muted)
		}
	}

	// Top Shows block.
	drawRoundedRect(img, 60, 842, W-60, 1084, 18, panelBg, panelBorder)
	drawText(img, 88, 864, "ZIRVEDEKI SERILER", 2, sky)
	if len(r.TopShows) == 0 {
		drawText(img, 88, 910, "HENUZ SERI KAYDI YOK", 2, muted)
	} else {
		for i, sh := range r.TopShows {
			if i >= 4 {
				break
			}
			y := 904 + i*40
			line := fmt.Sprintf("%d. %s", i+1, cutASCII(foldASCII(sh.Title), 32))
			drawText(img, 88, y, line, 2, text)
			meta := fmt.Sprintf("%d BL", sh.Watched)
			if sh.Score > 0 {
				meta = fmt.Sprintf("%d/10  %s", sh.Score, meta)
			}
			drawText(img, W-250, y, meta, 2, gold)
		}
	}

	// Footer watermark.
	drawText(img, 64, H-78, "GITHUB.COM/ROBLOXPRO1453/ANICLITR", 2, muted)
	drawText(img, W-220, H-78, "ANICLITR V1.2", 2, pink)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil && filepath.Dir(dst) != "." {
		return "", err
	}
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return dst, nil
}

func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func drawRoundedRect(img *image.RGBA, x0, y0, x1, y1, r int, fill, border color.RGBA) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			dx, dy := 0, 0
			if x < x0+r {
				dx = x0 + r - x
			} else if x > x1-r {
				dx = x - (x1 - r)
			}
			if y < y0+r {
				dy = y0 + r - y
			} else if y > y1-r {
				dy = y - (y1 - r)
			}
			dist := math.Hypot(float64(dx), float64(dy))
			if dx > 0 && dy > 0 && dist > float64(r) {
				continue
			}
			isBorder := x == x0 || x == x1 || y == y0 || y == y1 || (dx > 0 && dy > 0 && dist >= float64(r)-1.2)
			if isBorder {
				img.SetRGBA(x, y, border)
			} else {
				img.SetRGBA(x, y, fill)
			}
		}
	}
}

// Compact 5x7 uppercase bitmap font for clean dependency-free PNG rendering.
var font5x7 = map[rune][7]string{
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
	'C': {"01110", "10001", "10000", "10000", "10000", "10001", "01110"},
	'D': {"11110", "10001", "10001", "10001", "10001", "10001", "11110"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'F': {"11111", "10000", "10000", "11110", "10000", "10000", "10000"},
	'G': {"01110", "10001", "10000", "10111", "10001", "10001", "01110"},
	'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
	'I': {"01110", "00100", "00100", "00100", "00100", "00100", "01110"},
	'J': {"00111", "00010", "00010", "00010", "10010", "10010", "01100"},
	'K': {"10001", "10010", "10100", "11000", "10100", "10010", "10001"},
	'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10011", "10001", "10001", "10001"},
	'O': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
	'P': {"11110", "10001", "10001", "11110", "10000", "10000", "10000"},
	'Q': {"01110", "10001", "10001", "10001", "10101", "10010", "01101"},
	'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
	'S': {"01111", "10000", "10000", "01110", "00001", "00001", "11110"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'U': {"10001", "10001", "10001", "10001", "10001", "10001", "01110"},
	'V': {"10001", "10001", "10001", "10001", "10001", "01010", "00100"},
	'W': {"10001", "10001", "10001", "10101", "10101", "11011", "10001"},
	'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
	'Y': {"10001", "10001", "01010", "00100", "00100", "00100", "00100"},
	'Z': {"11111", "00001", "00010", "00100", "01000", "10000", "11111"},
	'0': {"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
	'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	'2': {"01110", "10001", "00001", "00110", "01000", "10000", "11111"},
	'3': {"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	'6': {"01110", "10000", "11110", "10001", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00001", "01110"},
	'.': {"00000", "00000", "00000", "00000", "00000", "01100", "01100"},
	',': {"00000", "00000", "00000", "00000", "00000", "00100", "01000"},
	':': {"00000", "00100", "00100", "00000", "00100", "00100", "00000"},
	'-': {"00000", "00000", "00000", "11111", "00000", "00000", "00000"},
	'/': {"00001", "00010", "00010", "00100", "01000", "01000", "10000"},
	'%': {"11001", "11010", "00010", "00100", "01000", "01011", "10011"},
	'(': {"00010", "00100", "01000", "01000", "01000", "00100", "00010"},
	')': {"01000", "00100", "00010", "00010", "00010", "00100", "01000"},
	'!': {"00100", "00100", "00100", "00100", "00100", "00000", "00100"},
	' ': {"00000", "00000", "00000", "00000", "00000", "00000", "00000"},
}

func drawText(img *image.RGBA, x, y int, s string, scale int, col color.RGBA) {
	cx := x
	for _, r := range strings.ToUpper(s) {
		g, ok := font5x7[r]
		if !ok {
			g = font5x7[' ']
		}
		for row, bits := range g {
			for bit, ch := range bits {
				if ch == '1' {
					for dy := 0; dy < scale; dy++ {
						for dx := 0; dx < scale; dx++ {
							img.SetRGBA(cx+bit*scale+dx, y+row*scale+dy, col)
						}
					}
				}
			}
		}
		cx += 6 * scale
	}
}

func drawWrapped(img *image.RGBA, x, y int, s string, scale int, col color.RGBA, maxW int) {
	maxChars := max(10, maxW/(6*scale))
	words := strings.Fields(s)
	var line string
	cy := y
	for _, w := range words {
		if len(line)+len(w)+1 > maxChars && line != "" {
			drawText(img, x, cy, line, scale, col)
			cy += 9 * scale
			line = w
		} else if line == "" {
			line = w
		} else {
			line += " " + w
		}
	}
	if line != "" {
		drawText(img, x, cy, line, scale, col)
	}
}

func foldASCII(s string) string {
	r := strings.NewReplacer(
		"ğ", "g", "Ğ", "G", "ü", "u", "Ü", "U", "ş", "s", "Ş", "S",
		"ı", "i", "İ", "I", "ö", "o", "Ö", "O", "ç", "c", "Ç", "C",
		"’", "'", "‘", "'", "—", "-", "–", "-", "·", "-",
	)
	return r.Replace(s)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func cutASCII(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:max(1, n-2)] + ".."
}
