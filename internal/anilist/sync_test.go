package anilist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/store"
)

func TestAuthPullAndPushFlow(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())

	var savedVars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token-123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.Contains(body.Query, "Viewer"):
			_, _ = w.Write([]byte(`{"data":{"Viewer":{"id":42,"name":"YasinAL"}}}`))
		case strings.Contains(body.Query, "MediaListCollection"):
			_, _ = w.Write([]byte(`{"data":{"MediaListCollection":{"lists":[{"isCustomList":false,"name":"Watching","entries":[{
				"id":999,"status":"CURRENT","score":9,"progress":6,"updatedAt":1764000000,
				"customLists":{"Favoriler":true},
				"media":{"id":154587,"idMal":52991,"format":"TV","episodes":28,"title":{"romaji":"Sousou no Frieren"},"coverImage":{"extraLarge":"https://img/frieren.jpg"}}
			}]}]}}}`))
		case strings.Contains(body.Query, "SaveMediaListEntry"):
			savedVars = body.Variables
			_, _ = w.Write([]byte(`{"data":{"SaveMediaListEntry":{"id":999}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := apiURL
	apiURL = srv.URL
	defer func() { apiURL = old }()

	auth := OpenAuth("12345")
	if err := auth.LoginWithToken(context.Background(), "test-token-123"); err != nil {
		t.Fatalf("LoginWithToken failed: %v", err)
	}
	if !auth.Connected() || auth.User() != "YasinAL" || auth.UserID() != 42 {
		t.Fatalf("unexpected auth state: user=%q id=%d", auth.User(), auth.UserID())
	}

	// Reload from disk to verify persistence.
	auth2 := OpenAuth("12345")
	if !auth2.Connected() || auth2.User() != "YasinAL" {
		t.Fatalf("session did not persist to anilist.json")
	}

	// Pull list into local library via Syncer.
	lib := store.OpenLibrary()
	syncer := NewSyncer(auth2, lib)
	syncer.pull()
	st := syncer.Status()
	if st.Err != "" || st.Count != 1 {
		t.Fatalf("pull status mismatch: %+v", st)
	}
	it, ok := lib.Get("anilist:154587")
	if !ok || it.Title != "Sousou no Frieren" || it.Status != store.Watching || it.Watched != 6 || it.Score != 9 || !it.InList("Favoriler") {
		t.Fatalf("pulled library item mismatch: ok=%v %+v", ok, it)
	}

	// Push update back to AniList.
	it.Watched = 7
	it.Score = 10
	syncer.Push(it)
	deadline := time.Now().Add(3 * time.Second)
	for savedVars == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if savedVars == nil {
		t.Fatal("expected SaveMediaListEntry push mutation")
	}
	if int(savedVars["progress"].(float64)) != 7 || int(savedVars["scoreRaw"].(float64)) != 100 {
		t.Fatalf("unexpected pushed vars: %+v", savedVars)
	}

	// Disconnect clears session.
	auth2.Disconnect()
	if auth2.Connected() {
		t.Fatal("expected disconnected state")
	}
}
