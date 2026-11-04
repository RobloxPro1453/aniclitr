package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Undo: destructive actions (removing from the library or history,
// deleting a list, clearing notifications…) push a restore step; ctrl+z
// brings the last one back within a couple of minutes.

type undoOp struct {
	label string
	at    time.Time
	undo  func(a *App)
}

const undoWindow = 2 * time.Minute

// remember records how to undo something and tells the user.
func (a *App) remember(label string, undo func(a *App)) {
	a.undos = append(a.undos, undoOp{label: label, at: time.Now(), undo: undo})
	if len(a.undos) > 20 {
		a.undos = a.undos[len(a.undos)-20:]
	}
	a.notify(label+" · ctrl+z geri al", "ok")
}

func (a *App) undoLast() tea.Cmd {
	for len(a.undos) > 0 {
		op := a.undos[len(a.undos)-1]
		a.undos = a.undos[:len(a.undos)-1]
		if time.Since(op.at) > undoWindow {
			continue
		}
		op.undo(a)
		a.notify("geri alındı: "+op.label, "ok")
		a.flash()
		return a.tick()
	}
	a.notify("geri alınacak bir şey yok", "warn")
	return a.tick()
}
