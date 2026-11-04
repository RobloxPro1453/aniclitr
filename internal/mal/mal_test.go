package mal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A fake MyAnimeList: OAuth (PKCE plain), paging, PATCH, refresh.
func TestLoginListUpdate(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	var verifier string
	var patched url.Values
	refreshed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			r.ParseForm()
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				if r.Form.Get("code") != "kod123" || r.Form.Get("code_verifier") != verifier || r.Form.Get("client_id") != "cid" {
					http.Error(w, "bad", 400)
					return
				}
				fmt.Fprint(w, `{"access_token":"A1","refresh_token":"R1","expires_in":60}`) // < 5 min: forces a refresh on first use
			case "refresh_token":
				refreshed = r.Form.Get("refresh_token") == "R1"
				fmt.Fprint(w, `{"access_token":"A2","refresh_token":"R2","expires_in":2678400}`)
			}
		case r.URL.Path == "/v2/users/@me":
			fmt.Fprint(w, `{"name":"yasin"}`)
		case r.URL.Path == "/v2/users/@me/animelist":
			if r.Header.Get("Authorization") != "Bearer A2" {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("offset") == "" {
				json.NewEncoder(w).Encode(map[string]any{
					"data":   []any{map[string]any{"node": map[string]any{"id": 52991, "title": "Sousou no Frieren", "num_episodes": 28, "main_picture": map[string]any{"large": "p.jpg"}}, "list_status": map[string]any{"status": "completed", "score": 10, "num_episodes_watched": 28, "tags": []string{"favoriler"}, "updated_at": "2026-10-01T10:00:00+00:00"}}},
					"paging": map[string]any{"next": "http://" + r.Host + "/v2/users/@me/animelist?offset=1"},
				})
				return
			}
			fmt.Fprint(w, `{"data":[{"node":{"id":1,"title":"Cowboy Bebop"},"list_status":{"status":"plan_to_watch","score":0}}],"paging":{}}`)
		case strings.HasSuffix(r.URL.Path, "/my_list_status") && r.Method == http.MethodPatch:
			r.ParseForm()
			patched = r.Form
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	authURL, tokenURL, apiBase = srv.URL+"/authorize", srv.URL+"/token", srv.URL+"/v2"
	// the "browser": read the PKCE challenge, approve, hit our callback
	OpenBrowser = func(u string) error {
		pu, _ := url.Parse(u)
		q := pu.Query()
		verifier = q.Get("code_challenge")
		go http.Get(RedirectURL + "?code=kod123&state=" + q.Get("state"))
		return nil
	}
	c := New("cid")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Login(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.Connected() || c.User() != "yasin" {
		t.Fatalf("not connected / user %q", c.User())
	}
	list, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed {
		t.Fatal("expiring token should have been refreshed")
	}
	if len(list) != 2 || list[0].Score != 10 || list[0].Tags[0] != "favoriler" || list[1].Status != PlanToWatch {
		t.Fatalf("list: %+v", list)
	}
	sc, w := 9, 5
	tags := []string{"a", "b"}
	if err := c.Set(ctx, 52991, Update{Status: Watching, Score: &sc, Watched: &w, Tags: &tags}); err != nil {
		t.Fatal(err)
	}
	if patched.Get("status") != "watching" || patched.Get("score") != "9" || patched.Get("num_watched_episodes") != "5" || patched.Get("tags") != "a,b" {
		t.Fatalf("patch form: %v", patched)
	}
	// the token survives a restart
	if c2 := New("cid"); !c2.Connected() || c2.User() != "yasin" {
		t.Fatal("token not persisted")
	}
}
