package watchparty

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServerlessRelayWatchPartySyncAndChat(t *testing.T) {
	// Mock ntfy-compatible pubsub relay (in tests only; production uses remote HTTPS relay)
	var mu sync.Mutex
	subs := map[string][]chan string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sse") {
			topic := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/sse")
			ch := make(chan string, 16)
			mu.Lock()
			subs[topic] = append(subs[topic], ch)
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			fl.Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case msg := <-ch:
					env, _ := json.Marshal(map[string]string{"event": "message", "message": msg})
					_, _ = w.Write([]byte("data: " + string(env) + "\n\n"))
					fl.Flush()
				}
			}
		}
		topic := strings.TrimPrefix(r.URL.Path, "/")
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		for _, ch := range subs[topic] {
			select {
			case ch <- string(body):
			default:
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code := GenerateRoomCode()
	if !strings.HasPrefix(code, "ANI-") || len(code) != 13 {
		t.Fatalf("unexpected room code format: %q", code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	alice, err := ConnectRelay(ctx, srv.URL, code, "Alice", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	bob, err := ConnectRelay(ctx, srv.URL, code, "Bob", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	time.Sleep(80 * time.Millisecond)
	if err := alice.SendSync("Frieren B1", 125.5, false); err != nil {
		t.Fatal(err)
	}
	if err := alice.SendChat("Bu sahne efsane!"); err != nil {
		t.Fatal(err)
	}
	if err := alice.SendReaction("🔥"); err != nil {
		t.Fatal(err)
	}

	gotSync, gotChat, gotReact := false, false, false
	deadline := time.After(3 * time.Second)
	for !(gotSync && gotChat && gotReact) {
		select {
		case ev := <-bob.Events:
			switch ev.Kind {
			case "sync":
				if ev.Pos == 125.5 && ev.Nick == "Alice" {
					gotSync = true
				}
			case "chat":
				if ev.Text == "Bu sahne efsane!" {
					gotChat = true
				}
			case "reaction":
				if ev.Reaction == "🔥" {
					gotReact = true
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for relay events: sync=%v chat=%v react=%v", gotSync, gotChat, gotReact)
		}
	}

	ass := BuildChatASS(code, bob.Messages())
	if !strings.Contains(ass, "Bu sahne efsane!") || !strings.Contains(ass, code) {
		t.Fatalf("BuildChatASS malformed: %s", ass)
	}
}
