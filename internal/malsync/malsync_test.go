package malsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aniclitr/internal/mal"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// fakeMAL keeps a list in memory and serves the endpoints the syncer uses.
type fakeMAL struct {
	mu   sync.Mutex
	list map[int]map[string]any
}

func (f *fakeMAL) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/users/@me/animelist"):
		var data []any
		for id, st := range f.list {
			data = append(data, map[string]any{"node": map[string]any{"id": id, "title": st["title"], "num_episodes": 12},
				"list_status": map[string]any{"status": st["status"], "score": st["score"], "num_episodes_watched": st["watched"], "tags": st["tags"], "updated_at": st["updated"]}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data, "paging": map[string]any{}})
	case strings.HasSuffix(r.URL.Path, "/my_list_status"):
		id, _ := strconv.Atoi(strings.Split(r.URL.Path, "/")[3])
		if r.Method == http.MethodDelete {
			delete(f.list, id)
			return
		}
		r.ParseForm()
		e := f.list[id]
		if e == nil {
			e = map[string]any{"title": "?"}
			f.list[id] = e
		}
		e["status"] = r.Form.Get("status")
		e["score"], _ = strconv.Atoi(r.Form.Get("score"))
		e["watched"], _ = strconv.Atoi(r.Form.Get("num_watched_episodes"))
		e["tags"] = strings.Split(r.Form.Get("tags"), ",")
		e["updated"] = time.Now().UTC().Format(time.RFC3339)
		w.Write([]byte("{}"))
	default:
		http.NotFound(w, r)
	}
}

func TestSyncRoundTrip(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	f := &fakeMAL{list: map[int]map[string]any{
		52991: {"title": "Sousou no Frieren", "status": "completed", "score": 10, "watched": 28, "tags": []string{"favoriler"}, "updated": "2026-10-01T10:00:00Z"},
		1:     {"title": "Cowboy Bebop", "status": "plan_to_watch", "score": 0, "watched": 0, "tags": []string{}, "updated": "2026-10-02T10:00:00Z"},
	}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	mal.SetEndpoints(srv.URL+"/a", srv.URL+"/t", srv.URL+"/v2")
	c := mal.New("cid")
	c.SetToken(mal.Token{Access: "x", Refresh: "y", Expiry: time.Now().Add(time.Hour)})
	lib := store.OpenLibrary()
	// a local item that is already on a source (AnimeciX) with the same MAL id
	lib.Set(source.Anime{Source: "animecix", ID: "777", Title: "Sousou no Frieren", MalID: 52991}, func(it *store.Item) { it.Status = store.Watching })
	lib.Mutate("animecix:777", func(it *store.Item) { it.MalDirty = false; it.MalSeen = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) })
	s := New(c, lib)
	ctx := context.Background()

	// 1) pull: MAL's newer "completed 10" wins; Bebop appears as a MAL-only item
	if _, err := s.Pull(ctx); err != nil {
		t.Fatal(err)
	}
	it, _ := lib.Get("animecix:777")
	if it.Status != store.Completed || it.Score != 10 || it.Watched != 28 || !it.InList("favoriler") {
		t.Fatalf("pull not applied: %+v", it)
	}
	bebop, ok := lib.FindMal(1)
	if !ok || !bebop.FromMal() || bebop.Status != store.Planned {
		t.Fatalf("MAL-only item missing: %+v", bebop)
	}

	// 2) local edit → pushed
	lib.Set(bebop.Anime(), func(x *store.Item) { x.Status = store.Watching; x.Score = 8; x.Watched = 3; x.Lists = []string{"yaz"} })
	if n, err := s.Push(ctx); err != nil || n != 1 {
		t.Fatalf("push: %d %v", n, err)
	}
	if e := f.list[1]; e["status"] != "watching" || e["score"] != 8 || e["watched"] != 3 {
		t.Fatalf("MAL not updated: %v", e)
	}
	if it, _ := lib.FindMal(1); it.MalDirty {
		t.Fatal("dirty flag should clear after push")
	}

	// 3) opening the MAL-only item on a source adopts it (no duplicate)
	lib.Set(source.Anime{Source: "animecix", ID: "42", Title: "Cowboy Bebop", MalID: 1}, nil)
	if _, ok := lib.Get("mal:1"); ok {
		t.Fatal("MAL-only item should have been adopted by the source item")
	}
	if it, ok := lib.Get("animecix:42"); !ok || it.Score != 8 {
		t.Fatalf("adopted item lost data: %+v", it)
	}

	// 4) remove locally → deleted on MAL; removed on MAL → dropped locally
	lib.Remove("animecix:42")
	if _, err := s.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.list[1]; ok {
		t.Fatal("delete not sent to MAL")
	}
	delete(f.list, 52991)
	if _, err := s.Pull(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := lib.Get("animecix:777"); ok {
		t.Fatal("item deleted on MAL should be dropped locally")
	}
}
