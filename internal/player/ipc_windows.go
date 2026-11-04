package player

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dial(path string) (net.Conn, error) {
	timeout := 500 * time.Millisecond
	return winio.DialPipe(path, &timeout)
}
