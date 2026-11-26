// Package discord provides zero-dependency Discord Rich Presence over the
// local Discord IPC socket/named pipe. When Discord is not running or RPC is
// disabled, every call is a fast silent no-op.
//
// Updates are serialized on a single worker goroutine with latest-wins
// coalescing so rapid navigation or pause toggles can never arrive out of
// order, and dropped pipes automatically reconnect once.
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
	"testing"
	"time"

	"aniclitr/internal/logx"
)

// DefaultAppID is a registered public Discord Application ID so Rich Presence
// works out-of-the-box with zero user configuration.
// Override via ANICLITR_DISCORD_APP_ID or config.json ("discord_app_id").
const DefaultAppID = "448016723057049601"

// DefaultIconURL is the hosted aniclitr emblem used on browse screens (and as
// the corner small badge alongside anime covers while watching).
const DefaultIconURL = "https://raw.githubusercontent.com/RobloxPro1453/aniclitr/main/docs/media/icon.png"

const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
	opPing      = 3
	opPong      = 4
)

// Activity describes the currently playing anime or browsing screen shown in Discord.
type Activity struct {
	Details    string
	State      string
	LargeImage string
	LargeText  string
	SmallImage string
	SmallText  string
	Start      time.Time
	End        time.Time
	Paused     bool
}

// Client manages a lazy, strictly-ordered local IPC connection to Discord.
// conn is owned exclusively by the single worker goroutine (zero lock contention
// or Close deadlocks during blocking pipe I/O).
type Client struct {
	mu      sync.Mutex
	enabled bool
	appID   string
	lastErr string

	// conn is touched only by loop()
	conn net.Conn

	// Serialized latest-wins worker state (guarded by mu)
	wake      chan struct{}
	done      chan struct{}
	started   bool
	closed    bool
	pending   *Activity
	wantClear bool
}

// New creates a Discord RPC client (does not connect until SetActivity is called).
// Optional customAppID overrides the built-in public ID (ANICLITR_DISCORD_APP_ID wins).
// Under `go test`, real IPC dialing stays disabled so tests never spam a live Discord client.
func New(enabled bool, customAppID ...string) *Client {
	if testing.Testing() {
		enabled = false
	}
	id := strings.TrimSpace(os.Getenv("ANICLITR_DISCORD_APP_ID"))
	if id == "" && len(customAppID) > 0 {
		id = strings.TrimSpace(customAppID[0])
	}
	if id == "" {
		id = DefaultAppID
	}
	return &Client{
		enabled: enabled,
		appID:   id,
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

func (c *Client) ensureWorkerLocked() {
	if c.started || c.closed {
		return
	}
	c.started = true
	go c.loop()
}

func (c *Client) signalLocked() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// LastError returns the most recent IPC error (empty when connected cleanly).
func (c *Client) LastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

func (c *Client) setLastErr(s string) {
	c.mu.Lock()
	c.lastErr = s
	c.mu.Unlock()
}

// SetEnabled toggles Discord Rich Presence; turning it off clears any active presence.
func (c *Client) SetEnabled(on bool) {
	if testing.Testing() {
		on = false
	}
	c.mu.Lock()
	was := c.enabled
	c.enabled = on
	if was && !on {
		c.pending = nil
		c.wantClear = true
		c.ensureWorkerLocked()
		c.signalLocked()
	}
	c.mu.Unlock()
}

// SetActivity queues the latest Discord presence update (non-blocking, strictly ordered).
func (c *Client) SetActivity(act Activity) {
	c.mu.Lock()
	if !c.enabled || c.closed {
		c.mu.Unlock()
		return
	}
	cp := act
	c.pending = &cp
	c.wantClear = false
	c.ensureWorkerLocked()
	c.signalLocked()
	c.mu.Unlock()
}

// Clear removes the Rich Presence activity from Discord.
func (c *Client) Clear() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.pending = nil
	c.wantClear = true
	if c.started {
		c.signalLocked()
	}
	c.mu.Unlock()
}

// Close clears any active presence and stops the worker cleanly (bounded wait, deadlock-free).
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.pending = nil
	c.wantClear = true
	wasStarted := c.started
	c.signalLocked()
	c.mu.Unlock()

	if wasStarted {
		select {
		case <-c.done:
		case <-time.After(450 * time.Millisecond):
		}
	}
}

func (c *Client) loop() {
	defer close(c.done)
	for {
		<-c.wake
		for {
			c.mu.Lock()
			act := c.pending
			clearReq := c.wantClear
			closed := c.closed
			enabled := c.enabled
			c.pending = nil
			c.wantClear = false
			c.mu.Unlock()

			if clearReq {
				c.doClear()
			} else if act != nil && enabled && !closed {
				c.doSet(*act)
			}
			if closed {
				c.closeConn()
				return
			}

			c.mu.Lock()
			more := c.pending != nil || c.wantClear || c.closed
			c.mu.Unlock()
			if !more {
				break
			}
		}
	}
}

func (c *Client) doSet(act Activity) {
	payload := buildActivityFrame(os.Getpid(), act, fmt.Sprintf("aniclitr-%d", time.Now().UnixNano()))
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.ensureConnected(); err != nil {
			c.setLastErr(err.Error())
			if attempt == 1 {
				logx.Errorf("discord rpc bağlanamadı: %v", err)
			}
			return
		}
		if err := writePacket(c.conn, opFrame, payload); err != nil {
			c.setLastErr(err.Error())
			c.closeConn()
			continue
		}
		op, reply, err := readPacket(c.conn)
		if err != nil {
			c.setLastErr(err.Error())
			c.closeConn()
			continue
		}
		if op == opPing {
			_ = writePacket(c.conn, opPong, reply)
			op, reply, err = readPacket(c.conn)
			if err != nil {
				c.setLastErr(err.Error())
				c.closeConn()
				continue
			}
		}
		if op == opClose || isDiscordError(reply) {
			msg := strings.TrimSpace(string(reply))
			c.setLastErr(msg)
			logx.Errorf("discord rpc etkinlik reddedildi (op=%d): %s", op, msg)
			if op == opClose {
				c.closeConn()
				continue
			}
			return
		}
		c.setLastErr("")
		return
	}
}

func (c *Client) doClear() {
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
	c.closeConn()
}

func (c *Client) closeConn() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) ensureConnected() error {
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
	simg := strings.TrimSpace(act.SmallImage)
	if strings.HasPrefix(simg, "https://") || strings.HasPrefix(simg, "http://") {
		assets["small_image"] = simg
		if stxt := strings.TrimSpace(act.SmallText); stxt != "" {
			assets["small_text"] = clampDiscordStr(stxt, 2, 128)
		}
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
