package ui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// input is a small single-line editor with word-wise editing.
type input struct {
	value       []rune
	pos         int
	placeholder string
}

func (in *input) Value() string { return string(in.value) }

// cleanRunes drops control and invisible format characters: terminals can
// deliver NUL (ctrl+space / ctrl+@), BOMs or zero-width chars inside pastes,
// and a single NUL in a search query made the site answer 400.
func cleanRunes(rs []rune) []rune {
	out := rs[:0:0]
	for _, r := range rs {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			out = append(out, ' ')
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == utf8RuneError:
			continue
		default:
			out = append(out, r)
		}
	}
	return out
}

const utf8RuneError = '�'

// CleanQuery normalises user text for providers: no control chars, single spaces.
func CleanQuery(s string) string {
	return strings.Join(strings.Fields(string(cleanRunes([]rune(s)))), " ")
}

func (in *input) Set(s string) {
	in.value = []rune(s)
	in.pos = len(in.value)
}

// handle applies a key; it reports whether the key was consumed.
func (in *input) handle(k tea.KeyMsg) bool {
	switch k.Type {
	case tea.KeyRunes, tea.KeySpace:
		r := cleanRunes(k.Runes)
		if k.Type == tea.KeySpace {
			r = []rune{' '}
		}
		if len(r) == 0 {
			return true // swallow control bytes (e.g. NUL from ctrl+space)
		}
		in.value = append(in.value[:in.pos], append(append([]rune{}, r...), in.value[in.pos:]...)...)
		in.pos += len(r)
	case tea.KeyBackspace:
		if in.pos > 0 {
			in.value = append(in.value[:in.pos-1], in.value[in.pos:]...)
			in.pos--
		}
	case tea.KeyCtrlW, tea.KeyCtrlH:
		start := in.pos
		for start > 0 && unicode.IsSpace(in.value[start-1]) {
			start--
		}
		for start > 0 && !unicode.IsSpace(in.value[start-1]) {
			start--
		}
		in.value = append(in.value[:start], in.value[in.pos:]...)
		in.pos = start
	case tea.KeyDelete:
		if in.pos < len(in.value) {
			in.value = append(in.value[:in.pos], in.value[in.pos+1:]...)
		}
	case tea.KeyLeft:
		if in.pos > 0 {
			in.pos--
		}
	case tea.KeyRight:
		if in.pos < len(in.value) {
			in.pos++
		}
	case tea.KeyHome, tea.KeyCtrlA:
		in.pos = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		in.pos = len(in.value)
	case tea.KeyCtrlU:
		in.value, in.pos = nil, 0
	default:
		return false
	}
	return true
}

// view draws the field inside a rounded frame of width w (3 rows).
func (in *input) view(w int, focused bool, frame int) []string {
	border := cFaint
	if focused {
		border = cAccent
	}
	bs := fg(border)
	inner := w - 4
	icon := stMuted.Render("⌕ ")
	if focused {
		icon = stAccent.Render("⌕ ")
	}
	textW := inner - 2
	var body string
	if len(in.value) == 0 {
		body = stMuted.Render(truncate(in.placeholder, textW))
		if focused {
			body = cursorCell(" ", frame) + stMuted.Render(truncate(in.placeholder, textW-1))
		}
	} else {
		v := string(in.value)
		before := string(in.value[:in.pos])
		// keep the cursor in view on long input
		for lipgloss.Width(before) > textW-1 {
			r := []rune(before)
			before = string(r[1:])
			v = string([]rune(v)[1:])
		}
		after := strings.TrimPrefix(v, before)
		ar := []rune(after)
		cur := " "
		if len(ar) > 0 {
			cur = string(ar[0])
			after = string(ar[1:])
		}
		if focused {
			body = stText.Render(before) + cursorCell(cur, frame) + stText.Render(after)
		} else {
			body = stText.Render(before + cur + after)
		}
	}
	mid := bs.Render("│ ") + icon + fit(body, textW) + bs.Render(" │")
	return []string{
		bs.Render("╭" + strings.Repeat("─", w-2) + "╮"),
		mid,
		bs.Render("╰" + strings.Repeat("─", w-2) + "╯"),
	}
}

func cursorCell(ch string, frame int) string {
	_ = frame // a steady block reads better than blinking; frame kept for future use
	return lipgloss.NewStyle().Foreground(lipgloss.Color(cInk)).Background(lipgloss.Color(cAccent)).Render(ch)
}
