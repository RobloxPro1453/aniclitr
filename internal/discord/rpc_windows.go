//go:build windows

package discord

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dialDiscord() (net.Conn, error) {
	timeout := 400 * time.Millisecond
	for i := 0; i < 10; i++ {
		pipe := fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
		conn, err := winio.DialPipe(pipe, &timeout)
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("discord ipc pipe bulunamadı (Discord kapalı olabilir)")
}
