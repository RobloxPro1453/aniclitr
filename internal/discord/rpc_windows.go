//go:build windows

package discord

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

type pipeConn struct{ *os.File }

func (p pipeConn) LocalAddr() net.Addr                { return pipeAddr("local") }
func (p pipeConn) RemoteAddr() net.Addr               { return pipeAddr("discord-ipc") }
func (p pipeConn) SetDeadline(t time.Time) error      { return p.File.SetDeadline(t) }
func (p pipeConn) SetReadDeadline(t time.Time) error  { return p.File.SetReadDeadline(t) }
func (p pipeConn) SetWriteDeadline(t time.Time) error { return p.File.SetWriteDeadline(t) }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

func dialDiscord() (net.Conn, error) {
	for i := 0; i < 10; i++ {
		f, err := os.OpenFile(fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i), os.O_RDWR, 0)
		if err == nil {
			return pipeConn{f}, nil
		}
	}
	return nil, errors.New("discord ipc not found")
}
