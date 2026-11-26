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
	"strings"
	"sync"
	"time"

	"aniclitr/internal/logx"
)

// DefaultAppID is a registered public Discord Application ID for mpv playback
// so Rich Presence works out-of-the-box with zero user configuration.
// Override via ANICLITR_DISCORD_APP_ID to use a custom Discord application.
const DefaultAppID = "448016723057049601"

const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
	opPing      = 3
	opPong      = 4
)

// Activity describes the currently playing anime shown in Discord.
type Activity struct {
	Details    string
	State      string
	LargeImage string
	LargeText  string
	Start      time.Time
	End        time.Time
	Paused     bool
}

// Client manages a lazy local IPC connection to Discord.
type Client struct {
	mu      sync.Mutex
	enabled bool
	appID   string
	conn    net.Conn
	lastErr string
}

// New creates a Discord RPC client (does not connect until SetActivity is called).
// Optional customAppID overrides the built-in public ID (ANICLITR_DISCORD_APP_ID wins).
func New(enabled bool, customAppID ...string) *Client {
	id := strings.TrimSpace(os.Getenv("ANICLITR_DISCORD_APP_ID"))
	if id == "" && len(customAppID) > 0 {
		id = strings.TrimSpace(customAppID[0])
	}
	if id == "" {
		id = DefaultAppID
	}
	return &Client{enabled: enabled, appID: id}
}

// LastError returns the most recent IPC error (empty when connected cleanly).
func (c *Client) LastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
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
// Automatically reconnects & retries once if the existing IPC pipe dropped so
// presence never vanishes on pause or screen transitions.
func (c *Client) SetActivity(act Activity) {
	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.enabled {
			return
		}
		payload := buildActivityFrame(os.Getpid(), act, fmt.Sprintf("aniclitr-%d", time.Now().UnixNano()))
		for attempt := 0; attempt < 2; attempt++ {
			if err := c.ensureConnectedLocked(); err != nil {
				c.lastErr = err.Error()
				if attempt == 1 {
					logx.Errorf("discord rpc bağlanamadı: %v", err)
				}
				return
			}
			if err := writePacket(c.conn, opFrame, payload); err != nil {
				c.lastErr = err.Error()
				c.closeLocked()
				continue
			}
			op, reply, err := readPacket(c.conn)
			if err != nil {
				c.lastErr = err.Error()
				c.closeLocked()
				continue
			}
			if op == opClose || isDiscordError(reply) {
				c.lastErr = strings.TrimSpace(string(reply))
				logx.Errorf("discord rpc etkinlik reddedildi (op=%d): %s", op, c.lastErr)
				if op == opClose {
					c.closeLocked()
					continue
				}
				return
			}
			c.lastErr = ""
			return
		}
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
	op, reply, err := readPacket(conn)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if op == opClose || isDiscordError(reply) {
		_ = conn.Close()
		return fmt.Errorf("el sıkışma reddedildi (op=%d): %s", op, strings.TrimSpace(string(reply)))
	}
	c.conn = conn
	return nil
}

func isDiscordError(payload []byte) bool {
	var resp struct {
		Evt  string `json:"evt"`
		Code int    `json:"code"`
	}
	if json.Unmarshal(payload, &resp) != nil {
		return false
	}
	return strings.EqualFold(resp.Evt, "ERROR") || resp.Code >= 4000
}

func clampDiscordStr(s string, minLen, maxLen int) string {
	s = strings.TrimSpace(s)
	rs := []rune(s)
	if len(rs) > maxLen {
		rs = append(rs[:maxLen-1], '…')
	}
	s = string(rs)
	for len([]rune(s)) < minLen {
		s += " "
	}
	return s
}

func buildActivityFrame(pid int, act Activity, nonce string) []byte {
	details := clampDiscordStr(act.Details, 2, 128)
	rawState := strings.TrimSpace(act.State)
	if act.Paused && rawState != "" && !strings.HasPrefix(rawState, "❚❚") {
		rawState = "❚❚ Duraklatıldı · " + rawState
	}
	state := clampDiscordStr(rawState, 2, 128)
	body := map[string]any{
		"details":             details,
		"state":               state,
		"type":                3, // Watching
		"status_display_type": 2, // Show details (anime/screen) in member sidebar
		"instance":            false,
	}
	if !act.Paused && !act.Start.IsZero() {
		ts := map[string]int64{"start": act.Start.Unix()}
		if !act.End.IsZero() && act.End.After(act.Start) {
			ts["end"] = act.End.Unix()
		}
		body["timestamps"] = ts
	}
	assets := map[string]string{}
	img := strings.TrimSpace(act.LargeImage)
	if strings.HasPrefix(img, "https://") || strings.HasPrefix(img, "http://") {
		assets["large_image"] = img
	}
	if txt := strings.TrimSpace(act.LargeText); txt != "" {
		assets["large_text"] = clampDiscordStr(txt, 2, 128)
	}
	if len(assets) > 0 {
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
