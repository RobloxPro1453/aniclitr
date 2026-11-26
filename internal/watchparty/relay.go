package watchparty

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultRelayBase is an open, zero-account, zero-API-key pubsub relay so two
// friends can sync across the internet with a room code WITHOUT opening any
// port or local server on either PC.
const DefaultRelayBase = "https://ntfy.sh"

// ChatMessage is one live Watch Party 2.0 chat line or reaction burst.
type ChatMessage struct {
	Nick     string    `json:"nick"`
	Text     string    `json:"text"`
	Reaction string    `json:"reaction,omitempty"`
	At       time.Time `json:"at"`
}

// RelayEvent extends watchparty events with live chat & reactions.
type RelayEvent struct {
	Kind     string // "join", "leave", "sync", "chat", "reaction", "closed"
	PeerID   string
	Nick     string
	Title    string
	Pos      float64
	Paused   bool
	Text     string
	Reaction string
}

type relayPacket struct {
	Type     string  `json:"type"` // "join", "leave", "sync", "chat", "reaction"
	PeerID   string  `json:"peer"`
	Nick     string  `json:"nick"`
	Title    string  `json:"title,omitempty"`
	Pos      float64 `json:"pos,omitempty"`
	Paused   bool    `json:"paused,omitempty"`
	Text     string  `json:"text,omitempty"`
	Reaction string  `json:"reaction,omitempty"`
}

// GenerateRoomCode returns a shareable high-entropy room code like "ANI-7K4M-9P2Q".
func GenerateRoomCode() string {
	const alpha = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	var s [8]byte
	for i := range b {
		s[i] = alpha[int(b[i])%len(alpha)]
	}
	return fmt.Sprintf("ANI-%s-%s", string(s[:4]), string(s[4:]))
}

// TopicForCode derives an unguessable pubsub topic name from a shareable room code.
func TopicForCode(code string) string {
	norm := strings.ToUpper(strings.TrimSpace(code))
	sum := sha256.Sum256([]byte("aniclitr-wp2:" + norm))
	return "aniclitr-wp2-" + hex.EncodeToString(sum[:12])
}

// RelayRoom is a 100% serverless client-only Watch Party 2.0 session:
// NO net.Listen, NO local HTTP/TCP server, NO port forwarding.
type RelayRoom struct {
	Events chan RelayEvent

	mu        sync.Mutex
	code      string
	topic     string
	relayBase string
	peerID    string
	nick      string
	client    *http.Client
	cancel    context.CancelFunc
	closed    bool
	chat      []ChatMessage
	chatOn    bool
	lastRecv  time.Time
}

// ConnectRelay joins or creates a serverless Watch Party 2.0 room by code.
// Uses pure outbound HTTPS SSE + POST — opens zero ports on the user's PC.
func ConnectRelay(ctx context.Context, relayBase, code, nick string, client *http.Client) (*RelayRoom, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		code = GenerateRoomCode()
	}
	if strings.TrimSpace(nick) == "" {
		nick = "İzleyici"
	}
	relayBase = strings.TrimRight(strings.TrimSpace(relayBase), "/")
	if relayBase == "" {
		relayBase = DefaultRelayBase
	}
	if client == nil {
		client = &http.Client{}
	}
	rctx, cancel := context.WithCancel(ctx)
	pid := make([]byte, 6)
	_, _ = rand.Read(pid)
	rr := &RelayRoom{
		Events:    make(chan RelayEvent, 64),
		code:      code,
		topic:     TopicForCode(code),
		relayBase: relayBase,
		peerID:    hex.EncodeToString(pid),
		nick:      nick,
		client:    client,
		cancel:    cancel,
		chatOn:    true,
	}
	go rr.subscribeLoop(rctx)
	_ = rr.publish(relayPacket{Type: "join", PeerID: rr.peerID, Nick: rr.nick})
	return rr, nil
}

// Code returns the shareable room code.
func (r *RelayRoom) Code() string { return r.code }

// ChatEnabled reports whether the in-mpv live ASS chat overlay is visible.
func (r *RelayRoom) ChatEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.chatOn
}

// ToggleChat flips the in-mpv live ASS chat overlay on/off.
func (r *RelayRoom) ToggleChat() bool {
	r.mu.Lock()
	r.chatOn = !r.chatOn
	on := r.chatOn
	r.mu.Unlock()
	return on
}

// Messages returns a snapshot of recent room chat messages (last 12).
func (r *RelayRoom) Messages() []ChatMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ChatMessage, len(r.chat))
	copy(out, r.chat)
	return out
}

// SendSync broadcasts playback position & pause state (ignoring echo bounces).
func (r *RelayRoom) SendSync(title string, pos float64, paused bool) error {
	r.mu.Lock()
	if r.closed || time.Since(r.lastRecv) < 900*time.Millisecond {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	return r.publish(relayPacket{Type: "sync", PeerID: r.peerID, Nick: r.nick, Title: title, Pos: pos, Paused: paused})
}

// SendChat posts a live chat message into the room (displayed on mpv ASS OSD).
func (r *RelayRoom) SendChat(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("mesaj boş")
	}
	r.appendChat(ChatMessage{Nick: r.nick, Text: text, At: time.Now()})
	return r.publish(relayPacket{Type: "chat", PeerID: r.peerID, Nick: r.nick, Text: text})
}

// SendReaction posts an instant emoji reaction burst into the room.
func (r *RelayRoom) SendReaction(emoji string) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return nil
	}
	r.appendChat(ChatMessage{Nick: r.nick, Reaction: emoji, At: time.Now()})
	return r.publish(relayPacket{Type: "reaction", PeerID: r.peerID, Nick: r.nick, Reaction: emoji})
}

// Close leaves the room cleanly and stops the outbound SSE stream.
func (r *RelayRoom) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	_ = r.publish(relayPacket{Type: "leave", PeerID: r.peerID, Nick: r.nick})
	r.cancel()
}

func (r *RelayRoom) appendChat(m ChatMessage) {
	r.mu.Lock()
	r.chat = append(r.chat, m)
	if len(r.chat) > 12 {
		r.chat = r.chat[len(r.chat)-12:]
	}
	r.mu.Unlock()
}

func (r *RelayRoom) publish(pkt relayPacket) error {
	raw, err := json.Marshal(pkt)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.relayBase+"/"+r.topic, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("relay HTTP %d", resp.StatusCode)
	}
	return nil
}

func (r *RelayRoom) subscribeLoop(ctx context.Context) {
	defer func() {
		select {
		case r.Events <- RelayEvent{Kind: "closed"}:
		default:
		}
		close(r.Events)
	}()

	sseURL := r.relayBase + "/" + r.topic + "/sse"
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
		if err != nil {
			return
		}
		resp, err := r.client.Do(req)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 32<<10), 256<<10)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var envelope struct {
				Event   string `json:"event"`
				Message string `json:"message"`
			}
			if json.Unmarshal([]byte(data), &envelope) != nil || envelope.Message == "" {
				continue
			}
			var pkt relayPacket
			if json.Unmarshal([]byte(envelope.Message), &pkt) != nil {
				continue
			}
			if pkt.PeerID == r.peerID {
				continue // ignore our own echo
			}
			if pkt.Type == "sync" {
				r.mu.Lock()
				r.lastRecv = time.Now()
				r.mu.Unlock()
			}
			if pkt.Type == "chat" || pkt.Type == "reaction" {
				r.appendChat(ChatMessage{Nick: pkt.Nick, Text: pkt.Text, Reaction: pkt.Reaction, At: time.Now()})
			}
			select {
			case r.Events <- RelayEvent{
				Kind:     pkt.Type,
				PeerID:   pkt.PeerID,
				Nick:     pkt.Nick,
				Title:    pkt.Title,
				Pos:      pkt.Pos,
				Paused:   pkt.Paused,
				Text:     pkt.Text,
				Reaction: pkt.Reaction,
			}:
			default:
			}
		}
		resp.Body.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

// BuildChatASS renders recent Watch Party 2.0 chat & reactions into a sleek
// translucent top-right ASS overlay for mpv (1280x720 virtual canvas).
func BuildChatASS(code string, msgs []ChatMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	start := 0
	if len(msgs) > 5 {
		start = len(msgs) - 5
	}
	vis := msgs[start:]
	x, y, w := 870, 24, 386
	lineH := 26
	h := 34 + len(vis)*lineH + 10
	var b strings.Builder
	// Glass panel
	fmt.Fprintf(&b, `{\an7\pos(%d,%d)\bord1\shad0\1c&H181412&\1a&H42&\3c&H7052B8&\3a&H55&\p1}m 10 0 l %d 0 b %d 0 %d 0 %d 10 l %d %d b %d %d %d %d %d %d l 10 %d b 0 %d 0 %d 0 %d l 0 10 b 0 0 0 0 10 0{\p0}`+"\n",
		x, y, w-10, w, w, w, w, h-10, w, h, w, h, w-10, h, h, h, h, h-10)
	fmt.Fprintf(&b, `{\an7\pos(%d,%d)\fnSegoe UI\fs15\b1\bord0\shad0\1c&HA268FF&}● BİRLİKTE İZLE  ·  %s  {\1c&H888888&\fs13\b0}Alt+H gizle`+"\n",
		x+14, y+8, escapeASS(code))
	for i, m := range vis {
		ry := y + 32 + i*lineH
		body := m.Text
		if m.Reaction != "" {
			body = m.Reaction + " " + body
		}
		fmt.Fprintf(&b, `{\an7\pos(%d,%d)\fnSegoe UI\fs17\bord0\shad0\1c&H7DD3FC&\b1}%s: {\1c&HF5F5F5&\b0}%s`+"\n",
			x+14, ry, escapeASS(m.Nick), escapeASS(strings.TrimSpace(body)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func escapeASS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "{", `\{`)
	s = strings.ReplaceAll(s, "}", `\}`)
	return s
}
