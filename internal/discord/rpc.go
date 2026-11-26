// Package discord provides zero-dependency Discord Rich Presence over the
// local Discord IPC socket/named pipe. When Discord is not running or RPC is
// disabled, every call is a fast silent no-op.
package discord

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// DefaultAppID is the Discord application ID used for Rich Presence.
// Users can override it via ANICLITR_DISCORD_APP_ID.
const DefaultAppID = "1312860542100734042"

const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
)

// Activity describes the currently playing anime shown in Discord.
type Activity struct {
	Details    string
	State      string
	LargeImage string
	LargeText  string
	Start      time.Time
}

// Client manages a lazy local IPC connection to Discord.
type Client struct {
	mu      sync.Mutex
	enabled bool
	appID   string
	conn    net.Conn
}

// New creates a Discord RPC client (does not connect until SetActivity is called).
func New(enabled bool) *Client {
	id := os.Getenv("ANICLITR_DISCORD_APP_ID")
	if id == "" {
		id = DefaultAppID
	}
	return &Client{enabled: enabled, appID: id}
}

// SetEnabled toggles Discord Rich Presence; turning it off clears any active presence.
func (c *Client) SetEnabled(on bool) {
	c.mu.Lock()
	was := c.enabled
	c.enabled = on
	c.mu.Unlock()
	if was && !on {
		c.Clear()
	}
}

// SetActivity updates the user's Discord presence asynchronously.
func (c *Client) SetActivity(act Activity) {
	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.enabled {
			return
		}
		if err := c.ensureConnectedLocked(); err != nil {
			return
		}
		payload := buildActivityFrame(os.Getpid(), act, fmt.Sprintf("aniclitr-%d", time.Now().UnixNano()))
		if err := writePacket(c.conn, opFrame, payload); err != nil {
			c.closeLocked()
			return
		}
		_, _, _ = readPacket(c.conn)
	}()
}

// Clear removes the Rich Presence activity from Discord.
func (c *Client) Clear() {
	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.conn == nil {
			return
		}
		frame := map[string]any{
			"cmd":   "SET_ACTIVITY",
			"args":  map[string]any{"pid": os.Getpid()},
			"nonce": fmt.Sprintf("aniclitr-clear-%d", time.Now().UnixNano()),
		}
		b, _ := json.Marshal(frame)
		_ = writePacket(c.conn, opFrame, b)
		c.closeLocked()
	}()
}

// Close closes the IPC connection cleanly.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) ensureConnectedLocked() error {
	if c.conn != nil {
		return nil
	}
	conn, err := dialDiscord()
	if err != nil {
		return err
	}
	hs, _ := json.Marshal(map[string]any{"v": 1, "client_id": c.appID})
	if err := writePacket(conn, opHandshake, hs); err != nil {
		_ = conn.Close()
		return err
	}
	if _, _, err := readPacket(conn); err != nil {
		_ = conn.Close()
		return err
	}
	c.conn = conn
	return nil
}

func buildActivityFrame(pid int, act Activity, nonce string) []byte {
	body := map[string]any{
		"details": act.Details,
		"state":   act.State,
		"type":    3, // Watching
	}
	if !act.Start.IsZero() {
		body["timestamps"] = map[string]int64{"start": act.Start.Unix()}
	}
	if act.LargeImage != "" || act.LargeText != "" {
		assets := map[string]string{}
		if act.LargeImage != "" {
			assets["large_image"] = act.LargeImage
		}
		if act.LargeText != "" {
			assets["large_text"] = act.LargeText
		}
		body["assets"] = assets
	}
	frame := map[string]any{
		"cmd": "SET_ACTIVITY",
		"args": map[string]any{
			"pid":      pid,
			"activity": body,
		},
		"nonce": nonce,
	}
	b, _ := json.Marshal(frame)
	return b
}

func writePacket(w io.Writer, op uint32, payload []byte) error {
	if c, ok := w.(net.Conn); ok {
		_ = c.SetWriteDeadline(time.Now().Add(1500 * time.Millisecond))
	}
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], op)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readPacket(r io.Reader) (uint32, []byte, error) {
	if c, ok := r.(net.Conn); ok {
		_ = c.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	}
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(hdr[0:4])
	n := binary.LittleEndian.Uint32(hdr[4:8])
	if n > 1<<20 {
		return 0, nil, fmt.Errorf("discord ipc frame too large: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return op, buf, nil
}
