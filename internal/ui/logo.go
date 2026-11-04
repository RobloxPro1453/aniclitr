package ui

import "strings"

// logoGlyphs: ANSI Shadow letters of A N I C L I T R. Kept per letter so
// intros can animate letters independently.
var logoGlyphs = [][]string{
	{" █████╗ ", "██╔══██╗", "███████║", "██╔══██║", "██║  ██║", "╚═╝  ╚═╝"},
	{"███╗   ██╗", "████╗  ██║", "██╔██╗ ██║", "██║╚██╗██║", "██║ ╚████║", "╚═╝  ╚═══╝"},
	{"██╗", "██║", "██║", "██║", "██║", "╚═╝"},
	{" ██████╗", "██╔════╝", "██║     ", "██║     ", "╚██████╗", " ╚═════╝"},
	{"██╗     ", "██║     ", "██║     ", "██║     ", "███████╗", "╚══════╝"},
	{"██╗", "██║", "██║", "██║", "██║", "╚═╝"},
	{"████████╗", "╚══██╔══╝", "   ██║   ", "   ██║   ", "   ██║   ", "   ╚═╝   "},
	{"██████╗ ", "██╔══██╗", "██████╔╝", "██╔══██╗", "██║  ██║", "╚═╝  ╚═╝"},
}

// logoGap is the space between letters.
const logoGap = 1

var (
	logoArt    []string // composed rows
	logoLetter []int    // column → letter index (-1 for gaps)
	logoWidth  int
)

func init() {
	rows := make([]strings.Builder, len(logoGlyphs[0]))
	for li, g := range logoGlyphs {
		if li > 0 {
			for y := range rows {
				rows[y].WriteString(strings.Repeat(" ", logoGap))
			}
			for i := 0; i < logoGap; i++ {
				logoLetter = append(logoLetter, -1)
			}
		}
		w := len([]rune(g[0]))
		for y := range rows {
			rows[y].WriteString(g[y])
		}
		for i := 0; i < w; i++ {
			logoLetter = append(logoLetter, li)
		}
	}
	for _, r := range rows {
		logoArt = append(logoArt, r.String())
	}
	logoWidth = len(logoLetter)
}

// letterAt returns the letter index of logo column x.
func letterAt(x int) int {
	if x < 0 || x >= len(logoLetter) {
		return 0
	}
	if l := logoLetter[x]; l >= 0 {
		return l
	}
	return 0
}

// wordmark is the compact one-line brand used in the header.
func wordmark() string {
	return gradient("aniclitr", brand, true)
}
