package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/logx"
	"aniclitr/internal/mal"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// apiURL is a var so tests can redirect GraphQL calls to httptest.
var apiURL = endpoint

const CallbackURL = "http://localhost:47616/callback"

// Session holds the saved AniList OAuth token and viewer profile.
// Saved in anilist.json (never exported by internal/backup).
type Session struct {
	ClientID    string `json:"client_id,omitempty"`
	AccessToken string `json:"access_token"`
	UserID      int    `json:"user_id"`
	UserName    string `json:"user_name"`
}

// AuthClient manages AniList OAuth login and authenticated GraphQL calls.
type AuthClient struct {
	mu   sync.RWMutex
	sess Session
}

func sessionPath() string { return filepath.Join(config.Dir(), "anilist.json") }

// OpenAuth loads any saved AniList session from disk.
func OpenAuth(clientID string) *AuthClient {
	c := &AuthClient{}
	if raw, err := os.ReadFile(sessionPath()); err == nil {
		_ = json.Unmarshal(raw, &c.sess)
	}
	if clientID != "" && c.sess.ClientID == "" {
		c.sess.ClientID = clientID
	}
	return c
}

// Connected reports whether an AniList OAuth token is saved.
func (c *AuthClient) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sess.AccessToken != ""
}

// User returns the authenticated AniList username.
func (c *AuthClient) User() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sess.UserName
}

// UserID returns the authenticated AniList user ID.
func (c *AuthClient) UserID() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sess.UserID
}

// SetClientID updates the OAuth application client ID.
func (c *AuthClient) SetClientID(id string) {
	c.mu.Lock()
	c.sess.ClientID = strings.TrimSpace(id)
	c.mu.Unlock()
}

// Disconnect removes the saved AniList token from memory and disk.
func (c *AuthClient) Disconnect() {
	c.mu.Lock()
	c.sess = Session{ClientID: c.sess.ClientID}
	c.mu.Unlock()
	_ = os.Remove(sessionPath())
}

func (c *AuthClient) save() error {
	c.mu.RLock()
	b, err := json.MarshalIndent(c.sess, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	p := sessionPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_ = f.Chmod(0o600)
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// LoginWithToken validates a pasted AniList OAuth token via Viewer query and saves it.
func (c *AuthClient) LoginWithToken(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("erişim jetonu boş")
	}
	c.mu.Lock()
	c.sess.AccessToken = token
	c.mu.Unlock()
	id, name, err := c.fetchViewer(ctx)
	if err != nil {
		c.mu.Lock()
		c.sess.AccessToken = ""
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.sess.UserID = id
	c.sess.UserName = name
	c.mu.Unlock()
	return c.save()
}

// Login starts AniList Implicit OAuth2 flow in the user's browser and waits
// for the token on http://localhost:47616/callback.
func (c *AuthClient) Login(ctx context.Context, clientID string) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return errors.New("AniList Client ID gerekli")
	}
	c.SetClientID(clientID)

	ln, err := net.Listen("tcp", "127.0.0.1:47616")
	if err != nil {
		return fmt.Errorf("geri dönüş portu (47616) açılamadı: %w", err)
	}
	defer ln.Close()

	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Implicit grant puts #access_token=... in URL fragment: forward it to /token.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>aniclitr · AniList</title>
<body style="background:#0e0c14;color:#eceaf4;font-family:sans-serif;text-align:center;padding:48px">
<h2>aniclitr · AniList bağlantısı</h2><p id="s">Oturum tamamlanıyor…</p>
<script>
const h = new URLSearchParams(location.hash.slice(1));
const t = h.get("access_token");
if (t) {
  fetch("/token?access_token=" + encodeURIComponent(t)).then(() => {
    document.getElementById("s").textContent = "Bağlandı. Bu sekmeyi kapatıp terminale dönebilirsiniz.";
  });
} else {
  document.getElementById("s").textContent = "Erişim jetonu alınamadı — AniList'te Redirect URL olarak http://localhost:47616/callback tanımlı mı?";
}
</script></body>`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimSpace(r.URL.Query().Get("access_token"))
		if tok != "" {
			select {
			case tokenCh <- tok:
			default:
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "missing token", http.StatusBadRequest)
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case errCh <- err:
			default:
			}
		}
	}()
	defer func() {
		shCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()

	authURL := "https://anilist.co/api/v2/oauth/authorize?client_id=" + url.QueryEscape(clientID) + "&response_type=token"
	if err := mal.OpenBrowser(authURL); err != nil {
		return fmt.Errorf("tarayıcı açılamadı: %w", err)
	}

	select {
	case <-ctx.Done():
		return errors.New("zaman aşımı veya iptal edildi")
	case err := <-errCh:
		return err
	case tok := <-tokenCh:
		return c.LoginWithToken(ctx, tok)
	}
}

func (c *AuthClient) authGQL(ctx context.Context, query string, vars map[string]any, dst any) error {
	c.mu.RLock()
	tok := c.sess.AccessToken
	c.mu.RUnlock()
	if tok == "" {
		return errors.New("AniList oturumu bağlı değil")
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("User-Agent", netx.UserAgent)

	resp, err := netx.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("AniList jetonu geçersiz veya süresi dolmuş")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AniList HTTP %d", resp.StatusCode)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		return errors.New(env.Errors[0].Message)
	}
	if dst != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, dst)
	}
	return nil
}

func (c *AuthClient) fetchViewer(ctx context.Context) (int, string, error) {
	var res struct {
		Viewer struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"Viewer"`
	}
	if err := c.authGQL(ctx, `query { Viewer { id name } }`, nil, &res); err != nil {
		return 0, "", err
	}
	if res.Viewer.ID == 0 || res.Viewer.Name == "" {
		return 0, "", errors.New("AniList kullanıcı bilgisi alınamadı")
	}
	return res.Viewer.ID, res.Viewer.Name, nil
}

// ListEntry is one anime entry from the user's AniList MediaListCollection.
type ListEntry struct {
	EntryID     int
	MediaID     int
	MalID       int
	Title       string
	Poster      string
	Format      string
	Episodes    int
	Status      string // store.Watching, etc.
	Score       int    // 0-10
	Progress    int
	CustomLists []string
	UpdatedAt   time.Time
}

func alStatusToStore(s string) string {
	switch strings.ToUpper(s) {
	case "CURRENT", "REPEATING":
		return store.Watching
	case "COMPLETED":
		return store.Completed
	case "PLANNING":
		return store.Planned
	case "PAUSED":
		return store.OnHold
	case "DROPPED":
		return store.Dropped
	default:
		return store.Planned
	}
}

func storeStatusToAL(s string) string {
	switch s {
	case store.Watching:
		return "CURRENT"
	case store.Completed:
		return "COMPLETED"
	case store.Planned:
		return "PLANNING"
	case store.OnHold:
		return "PAUSED"
	case store.Dropped:
		return "DROPPED"
	default:
		return "PLANNING"
	}
}

// PullList retrieves the user's entire anime list from AniList.
func (c *AuthClient) PullList(ctx context.Context) ([]ListEntry, error) {
	uid := c.UserID()
	if uid == 0 {
		var err error
		uid, _, err = c.fetchViewer(ctx)
		if err != nil {
			return nil, err
		}
	}
	const q = `query($uid: Int) {
  MediaListCollection(userId: $uid, type: ANIME) {
    lists {
      isCustomList
      name
      entries {
        id
        status
        score(format: POINT_10)
        progress
        updatedAt
        customLists
        media {
          id
          idMal
          format
          episodes
          title { romaji english }
          coverImage { extraLarge large }
        }
      }
    }
  }
}`
	var res struct {
		MediaListCollection struct {
			Lists []struct {
				IsCustomList bool   `json:"isCustomList"`
				Name         string `json:"name"`
				Entries      []struct {
					ID          int             `json:"id"`
					Status      string          `json:"status"`
					Score       float64         `json:"score"`
					Progress    int             `json:"progress"`
					UpdatedAt   int64           `json:"updatedAt"`
					CustomLists map[string]bool `json:"customLists"`
					Media       struct {
						ID       int    `json:"id"`
						IDMal    int    `json:"idMal"`
						Format   string `json:"format"`
						Episodes int    `json:"episodes"`
						Title    Titles `json:"title"`
						Cover    struct {
							XL    string `json:"extraLarge"`
							Large string `json:"large"`
						} `json:"coverImage"`
					} `json:"media"`
				} `json:"entries"`
			} `json:"lists"`
		} `json:"MediaListCollection"`
	}
	if err := c.authGQL(ctx, q, map[string]any{"uid": uid}, &res); err != nil {
		return nil, err
	}
	byMedia := map[int]*ListEntry{}
	var order []int
	for _, lst := range res.MediaListCollection.Lists {
		for _, e := range lst.Entries {
			m := e.Media
			if m.ID == 0 {
				continue
			}
			ent, ok := byMedia[m.ID]
			if !ok {
				title := m.Title.Romaji
				if title == "" {
					title = m.Title.English
				}
				poster := m.Cover.XL
				if poster == "" {
					poster = m.Cover.Large
				}
				kind := "TV"
				if strings.EqualFold(m.Format, "MOVIE") {
					kind = "Film"
				}
				upd := time.Now()
				if e.UpdatedAt > 0 {
					upd = time.Unix(e.UpdatedAt, 0)
				}
				ent = &ListEntry{
					EntryID:   e.ID,
					MediaID:   m.ID,
					MalID:     m.IDMal,
					Title:     title,
					Poster:    poster,
					Format:    kind,
					Episodes:  m.Episodes,
					Status:    alStatusToStore(e.Status),
					Score:     int(e.Score + 0.5),
					Progress:  e.Progress,
					UpdatedAt: upd,
				}
				byMedia[m.ID] = ent
				order = append(order, m.ID)
			}
			for cl, on := range e.CustomLists {
				if on && cl != "" {
					ent.CustomLists = appendUnique(ent.CustomLists, cl)
				}
			}
		}
	}
	out := make([]ListEntry, 0, len(order))
	for _, id := range order {
		out = append(out, *byMedia[id])
	}
	return out, nil
}

// SaveEntry pushes a single anime entry's status, score (0-10), and progress to AniList.
func (c *AuthClient) SaveEntry(ctx context.Context, mediaID int, status string, score10, progress int) error {
	if mediaID <= 0 {
		return errors.New("geçersiz AniList kimliği")
	}
	const m = `mutation($mediaId: Int, $status: MediaListStatus, $scoreRaw: Int, $progress: Int) {
  SaveMediaListEntry(mediaId: $mediaId, status: $status, scoreRaw: $scoreRaw, progress: $progress) {
    id
  }
}`
	vars := map[string]any{
		"mediaId":  mediaID,
		"status":   storeStatusToAL(status),
		"scoreRaw": clampInt(score10, 0, 10) * 10,
		"progress": max(0, progress),
	}
	return c.authGQL(ctx, m, vars, nil)
}

// LookupMediaID resolves a MAL ID to its AniList media ID.
func (c *AuthClient) LookupMediaID(ctx context.Context, malID int) (int, error) {
	if malID <= 0 {
		return 0, errors.New("MAL kimliği yok")
	}
	var res struct {
		Media struct {
			ID int `json:"id"`
		} `json:"Media"`
	}
	if err := c.authGQL(ctx, `query($mal: Int) { Media(idMal: $mal, type: ANIME) { id } }`, map[string]any{"mal": malID}, &res); err != nil {
		return 0, err
	}
	return res.Media.ID, nil
}

// ---- Background two-way syncer ----

// SyncStatus describes the AniList syncer state for UI display.
type SyncStatus struct {
	Busy     bool
	LastSync time.Time
	Count    int
	Err      string
}

// Syncer keeps the local store.Library synced two-way with AniList.
type Syncer struct {
	Updates chan struct{}

	auth    *AuthClient
	lib     *store.Library
	mu      sync.Mutex
	st      SyncStatus
	malToAL map[int]int // cached MAL id -> AniList media id
}

// NewSyncer creates a two-way AniList library syncer.
func NewSyncer(auth *AuthClient, lib *store.Library) *Syncer {
	return &Syncer{
		Updates: make(chan struct{}, 1),
		auth:    auth,
		lib:     lib,
		malToAL: map[int]int{},
	}
}

func (s *Syncer) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

func (s *Syncer) ping() {
	select {
	case s.Updates <- struct{}{}:
	default:
	}
}

// Start kicks off an immediate background pull when connected.
func (s *Syncer) Start() {
	if !s.auth.Connected() {
		return
	}
	go s.pull()
}

// SyncNow triggers a manual pull immediately.
func (s *Syncer) SyncNow() {
	if !s.auth.Connected() {
		return
	}
	go s.pull()
}

func (s *Syncer) pull() {
	s.mu.Lock()
	if s.st.Busy {
		s.mu.Unlock()
		return
	}
	s.st.Busy = true
	s.mu.Unlock()
	s.ping()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	entries, err := s.auth.PullList(ctx)

	s.mu.Lock()
	s.st.Busy = false
	if err != nil {
		s.st.Err = err.Error()
		s.mu.Unlock()
		logx.Errorf("anilist sync pull: %v", err)
		s.ping()
		return
	}
	for _, e := range entries {
		if e.MalID > 0 && e.MediaID > 0 {
			s.malToAL[e.MalID] = e.MediaID
		}
	}
	s.st.Err = ""
	s.st.LastSync = time.Now()
	s.st.Count = len(entries)
	s.mu.Unlock()

	for _, e := range entries {
		an := source.Anime{
			Source:  "anilist",
			ID:      strconv.Itoa(e.MediaID),
			Title:   e.Title,
			Poster:  e.Poster,
			Kind:    e.Format,
			MalID:   e.MalID,
			EpCount: e.Episodes,
		}
		s.lib.Set(an, func(it *store.Item) {
			// Only adopt AniList state if the local record is older or brand new.
			if it.UpdatedAt.IsZero() || !it.UpdatedAt.After(e.UpdatedAt) {
				it.Status = e.Status
				if e.Score > 0 {
					it.Score = e.Score
				}
				if e.Progress > it.Watched {
					it.Watched = e.Progress
				}
				if e.Episodes > 0 && it.EpCount == 0 {
					it.EpCount = e.Episodes
				}
				for _, cl := range e.CustomLists {
					if !it.InList(cl) {
						it.Lists = append(it.Lists, cl)
					}
				}
			}
		})
	}
	s.ping()
}

// Push asynchronously sends a local library update to AniList when connected.
func (s *Syncer) Push(it store.Item) {
	if !s.auth.Connected() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		mediaID := 0
		if it.Source == "anilist" {
			mediaID, _ = strconv.Atoi(it.AnimeID)
		}
		if mediaID == 0 && it.MalID > 0 {
			s.mu.Lock()
			mediaID = s.malToAL[it.MalID]
			s.mu.Unlock()
			if mediaID == 0 {
				if id, err := s.auth.LookupMediaID(ctx, it.MalID); err == nil && id > 0 {
					mediaID = id
					s.mu.Lock()
					s.malToAL[it.MalID] = id
					s.mu.Unlock()
				}
			}
		}
		if mediaID == 0 {
			return
		}
		if err := s.auth.SaveEntry(ctx, mediaID, it.Status, it.Score, it.Watched); err != nil {
			s.mu.Lock()
			s.st.Err = err.Error()
			s.mu.Unlock()
			logx.Errorf("anilist sync push (%s): %v", it.Title, err)
			s.ping()
		}
	}()
}

func appendUnique(xs []string, x string) []string {
	for _, v := range xs {
		if strings.EqualFold(v, x) {
			return xs
		}
	}
	return append(xs, x)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
