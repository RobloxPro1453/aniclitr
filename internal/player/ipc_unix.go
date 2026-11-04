//go:build !windows

package player

import (
	"net"
	"time"
)

func dial(path string) (net.Conn, error) {
	return net.DialTimeout("unix", path, 500*time.Millisecond)
}
