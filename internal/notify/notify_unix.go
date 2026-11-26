//go:build !windows

package notify

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func sendOS(title, body string) {
	if runtime.GOOS == "darwin" {
		esc := func(s string) string {
			s = strings.ReplaceAll(s, `\`, `\\`)
			return strings.ReplaceAll(s, `"`, `\"`)
		}
		script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(body), esc(title))
		_ = exec.Command("osascript", "-e", script).Run()
		return
	}
	if _, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command("notify-send", "-a", "aniclitr", title, body).Run()
	}
}
