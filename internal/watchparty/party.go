// Package watchparty implements zero-dependency real-time "Watch Together"
// playback synchronisation over plain TCP (host & join with short address/code),
// plus optional Syncplay detection.
package watchparty

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DriftThreshold avoids bouncing tiny network jitter between players.
const DriftThreshold = 1.5

type wireMsg struct {
	Type   string  `json:"type"` // "hello", "state", "leave"
	Nick   string  `json:"nick,omitempty"`
	Title  string  `json:"title,omitempty"`
	Pos    float64 `json:"pos,omitempty"`
	Paused bool    `json:"paused,omitempty"`
}

// Event is delivered to the UI whenever room membership or playback state changes.
type Event struct {
	Kind   string // "join", "leave", "sync", "closed"
	Nick   string
	Pos    float64
	Paused bool
	Peers  int
}

// Room is an active host or guest watch party session.
type Room struct {
	Events chan Event

	mu       sync.Mutex
	nick     string
	title    string
	isHost   bool
	addr     string
	ln       net.Listener
	conn     net.Conn           // guest -> host
	clients  map[net.Conn]string // host -> connected guests (conn -> nick)
	closed   bool
	lastRecv time.Time
}

// LocalIPv4 returns the machine's preferred LAN IPv4 address for sharing a host room.
func LocalIPv4() string {
	conn, err := net.Dial("udp", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
			return addr.IP.String()
		}
	}
	return "127.0.0.1"
}

// HasSyncplay reports whether the external Syncplay client is installed.
func HasSyncplay() bool {
	_, err := exec.LookPath("syncplay")
	if err == nil {
		return true
	}
	_, err = exec.LookPath("Syncplay")
	return err == nil
}

// Host starts a watch-together room listening on addr (e.g. ":47620" or ":0").
func Host(listenAddr, nick, title string) (*Room, error) {
	if listenAddr == "" {
		listenAddr = ":47620"
	}
	if strings.TrimSpace(nick) == "" {
		nick = "Ev sahibi"
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// Fall back to any free port if 47620 is already in use.
		ln, err = net.Listen("tcp", ":0")
		if err != nil {
			return nil, fmt.Errorf("oda açılamadı: %w", err)
		}
	}
	port := ln.Addr().(*net.TCPAddr).Port
	r := &Room{
		Events:  make(chan Event, 32),
		nick:    nick,
		title:   title,
		isHost:  true,
		addr:    fmt.Sprintf("%s:%d", LocalIPv4(), port),
		ln:      ln,
		clients: map[net.Conn]string{},
	}
	go r.acceptLoop()
	return r, nil
}

// Join connects to a hosted room at addr (e.g. "192.168.1.42:47620").
func Join(addr, nick string) (*Room, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("oda adresi boş (örn: 192.168.1.5:47620)")
	}
	if !strings.Contains(addr, ":") {
		addr += ":47620"
	}
	if strings.TrimSpace(nick) == "" {
		nick = "Misafir"
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("odaya bağlanılamadı: %w", err)
	}
	r := &Room{
		Events: make(chan Event, 32),
		nick:   nick,
		isHost: false,
		addr:   addr,
		conn:   conn,
	}
	_ = writeWire(conn, wireMsg{Type: "hello", Nick: nick})
	go r.guestReadLoop(conn)
	return r, nil
}

// Addr returns the shareable host:port address for this room.
func (r *Room) Addr() string { return r.addr }

// ListenAddr returns the actual bound TCP address (useful in tests).
func (r *Room) ListenAddr() string {
	if r.ln != nil {
		return r.ln.Addr().String()
	}
	return r.addr
}

// IsHost reports whether this peer created the room.
func (r *Room) IsHost() bool { return r.isHost }

// PeerCount returns how many peers are currently in the room (including self).
func (r *Room) PeerCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.isHost {
		return 1 + len(r.clients)
	}
	if r.closed {
		return 0
	}
	return 2
}

// BroadcastState sends the local player's position and pause state to all peers.
// Suppressed briefly right after applying a remote sync to prevent feedback loops.
func (r *Room) BroadcastState(pos float64, paused bool) {
	r.mu.Lock()
	if r.closed || time.Since(r.lastRecv) < 900*time.Millisecond {
		r.mu.Unlock()
		return
	}
	msg := wireMsg{Type: "state", Nick: r.nick, Pos: math.Round(pos*10) / 10, Paused: paused}
	if r.isHost {
		conns := make([]net.Conn, 0, len(r.clients))
		for c := range r.clients {
			conns = append(conns, c)
		}
		r.mu.Unlock()
		for _, c := range conns {
			_ = writeWire(c, msg)
		}
		return
	}
	c := r.conn
	r.mu.Unlock()
	if c != nil {
		_ = writeWire(c, msg)
	}
}

// Close shuts down the room cleanly.
func (r *Room) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	if r.ln != nil {
		_ = r.ln.Close()
	}
	if r.conn != nil {
		_ = writeWire(r.conn, wireMsg{Type: "leave", Nick: r.nick})
		_ = r.conn.Close()
	}
	for c := range r.clients {
		_ = c.Close()
	}
	r.clients = nil
	r.mu.Unlock()
	r.emit(Event{Kind: "closed"})
}

func (r *Room) emit(ev Event) {
	select {
	case r.Events <- ev:
	default:
	}
}

func (r *Room) acceptLoop() {
	for {
		c, err := r.ln.Accept()
		if err != nil {
			return
		}
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			_ = c.Close()
			return
		}
		r.clients[c] = "Misafir"
		r.mu.Unlock()
		go r.hostClientLoop(c)
	}
}

func (r *Room) hostClientLoop(c net.Conn) {
	defer func() {
		r.mu.Lock()
		nick := r.clients[c]
		delete(r.clients, c)
		peers := 1 + len(r.clients)
		r.mu.Unlock()
		_ = c.Close()
		if nick != "" {
			r.emit(Event{Kind: "leave", Nick: nick, Peers: peers})
		}
	}()
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		var m wireMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m.Type {
		case "hello":
			nick := strings.TrimSpace(m.Nick)
			if nick == "" {
				nick = "Misafir"
			}
			r.mu.Lock()
			if r.closed || r.clients == nil {
				r.mu.Unlock()
				return
			}
			r.clients[c] = nick
			peers := 1 + len(r.clients)
			r.mu.Unlock()
			r.emit(Event{Kind: "join", Nick: nick, Peers: peers})
		case "state":
			r.mu.Lock()
			r.lastRecv = time.Now()
			peers := 1 + len(r.clients)
			others := make([]net.Conn, 0, len(r.clients))
			for o := range r.clients {
				if o != c {
					others = append(others, o)
				}
			}
			r.mu.Unlock()
			for _, o := range others {
				_ = writeWire(o, m)
			}
			r.emit(Event{Kind: "sync", Nick: m.Nick, Pos: m.Pos, Paused: m.Paused, Peers: peers})
		case "leave":
			return
		}
	}
}

func (r *Room) guestReadLoop(c net.Conn) {
	defer func() {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		_ = c.Close()
		r.emit(Event{Kind: "closed"})
	}()
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		var m wireMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Type == "state" {
			r.mu.Lock()
			r.lastRecv = time.Now()
			r.mu.Unlock()
			r.emit(Event{Kind: "sync", Nick: m.Nick, Pos: m.Pos, Paused: m.Paused, Peers: 2})
		}
	}
}

func writeWire(c net.Conn, m wireMsg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = c.Write(append(b, '\n'))
	return err
}
