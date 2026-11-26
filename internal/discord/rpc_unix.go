//go:build !windows

package discord

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

func dialDiscord() (net.Conn, error) {
	dirs := []string{
		os.Getenv("XDG_RUNTIME_DIR"),
		os.Getenv("TMPDIR"),
		os.Getenv("TMP"),
		"/tmp",
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		for i := 0; i < 10; i++ {
			for _, sub := range []string{"", "app/com.discordapp.Discord"} {
				p := filepath.Join(d, sub, fmt.Sprintf("discord-ipc-%d", i))
				if c, err := net.DialTimeout("unix", p, 400*time.Millisecond); err == nil {
					return c, nil
				}
			}
		}
	}
	return nil, errors.New("discord ipc not found")
}
