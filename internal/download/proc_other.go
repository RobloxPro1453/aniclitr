//go:build !windows

package download

import "os/exec"

func hideWindow(*exec.Cmd) {}
