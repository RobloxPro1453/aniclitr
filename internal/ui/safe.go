package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/logx"
)

// guard turns a panic inside a background command into an error toast
// (and a log entry) instead of letting Bubble Tea kill the whole program.
func guard(where string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logx.Panic(where, r)
				msg = toastMsg{text: fmt.Sprintf("iç hata (%s) — ayrıntı: %s", where, logx.Path()), kind: "err"}
			}
		}()
		return cmd()
	}
}
