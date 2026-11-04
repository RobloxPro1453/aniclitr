// Package mal talks to the official MyAnimeList API v2.
//
// Login is OAuth2 with PKCE: the user registers a free "other"-type app at
// https://myanimelist.net/apiconfig (redirect URL RedirectURL below), pastes
// the client ID into aniclitr, and approves access in the browser. A tiny
// local HTTP server catches the redirect; tokens are stored in
// %APPDATA%\aniclitr\mal.json and refreshed automatically. The password
// never touches this program.
package mal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/logx"
)

// Endpoints (vars so tests can point them at a fake server).
var (
	authURL  = "https://myanimelist.net/v1/oauth2/authorize"
	tokenURL = "https://myanimelist.net/v1/oauth2/token"
	apiBase  = "https://api.myanimelist.net/v2"
)

const (
	// Port/RedirectURL must match the app registered on MAL.
	Port        = 47615
	RedirectURL = "http://localhost:47615/callback"
)

// ErrNotConnected means there is no usable login.
var ErrNotConnected = errors.New("MyAnimeList bağlı değil")

// Token is the stored OAuth token.
type Token struct {
	Access  string    `json:"access_token"`
	Refresh string    `json:"refresh_token"`
	Expiry  time.Time `json:"expiry"`
	User    string    `json:"user,omitempty"`
}

// Client is a logged-in (or not) MAL client.
type Client struct {
	ClientID string
	mu       sync.Mutex
	tok      *Token
	path     string
	http     *http.Client
}

// New loads a stored token (if any).
func New(clientID string) *Client {
	c := &Client{ClientID: strings.TrimSpace(clientID), path: filepath.Join(config.Dir(), "mal.json"), http: &http.Client{Timeout: 30 * time.Second}}
	if b, err := os.ReadFile(c.path); err == nil {
		var t Token
		if json.Unmarshal(b, &t) == nil && t.Access != "" {
			c.tok = &t
		}
	}
	return c
}

// Connected reports whether a token is stored.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tok != nil && c.ClientID != ""
}

// User is the logged-in user's name ("" if unknown).
func (c *Client) User() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tok == nil {
		return ""
	}
	return c.tok.User
}

// Logout forgets the token.
func (c *Client) Logout() {
	c.mu.Lock()
	c.tok = nil
	c.mu.Unlock()
	_ = os.Remove(c.path)
}

func (c *Client) save() {
	c.mu.Lock()
	b, err := json.MarshalIndent(c.tok, "", " ")
	c.mu.Unlock()
	if err == nil {
		_ = config.WriteAtomic(c.path, b)
	}
}

// ---- login ----

func randomVerifier() string {
	b := make([]byte, 64)
	_, _ = rand.Read(b)
	v := base64.RawURLEncoding.EncodeToString(b)
	if len(v) > 128 {
		v = v[:128]
	}
	return v
}

// Login opens the browser and waits (until ctx ends) for the user to approve.
func (c *Client) Login(ctx context.Context) error {
	if c.ClientID == "" {
		return errors.New("MAL client ID girilmemiş")
	}
	verifier := randomVerifier() // MAL supports only the "plain" PKCE method
	state := randomVerifier()[:16]
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(Port))
	if err != nil {
		return fmt.Errorf("yerel port %d açılamadı: %w", Port, err)
	}
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch {
		case q.Get("state") != state:
			fmt.Fprint(w, page("Bir şeyler ters gitti", "Güvenlik doğrulaması tutmadı. aniclitr'de tekrar dene."))
			errCh <- errors.New("state uyuşmadı")
		case q.Get("error") != "":
			fmt.Fprint(w, page("İzin verilmedi", "aniclitr'e dönebilirsin."))
			errCh <- errors.New("izin verilmedi: " + q.Get("error"))
		default:
			fmt.Fprint(w, page("Bağlandı ✓", "MyAnimeList hesabın aniclitr'e bağlandı. Bu sekmeyi kapatıp terminale dönebilirsin."))
			codeCh <- q.Get("code")
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	u := authURL + "?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"code_challenge":        {verifier},
		"code_challenge_method": {"plain"},
		"state":                 {state},
		"redirect_uri":          {RedirectURL},
	}.Encode()
	if err := OpenBrowser(u); err != nil {
		logx.Errorf("tarayıcı açılamadı: %v — adres: %s", err, u)
	}
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return errors.New("giriş zaman aşımına uğradı")
	}
	form := url.Values{
		"client_id":     {c.ClientID},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {RedirectURL},
	}
	if err := c.exchange(ctx, form); err != nil {
		return err
	}
	if me, err := c.Me(ctx); err == nil {
		c.mu.Lock()
		c.tok.User = me
		c.mu.Unlock()
		c.save()
	}
	return nil
}

func page(title, msg string) string {
	return `<!doctype html><meta charset="utf-8"><title>aniclitr</title><body style="background:#0e0c14;color:#ecebf4;font-family:system-ui;display:grid;place-items:center;height:100vh;margin:0"><div style="text-align:center"><h1 style="color:#ff6aa2">` + title + `</h1><p>` + msg + `</p></div>`
}

func (c *Client) exchange(ctx context.Context, form url.Values) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MAL token: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var t struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return err
	}
	c.mu.Lock()
	user := ""
	if c.tok != nil {
		user = c.tok.User
	}
	c.tok = &Token{Access: t.Access, Refresh: t.Refresh, Expiry: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second), User: user}
	c.mu.Unlock()
	c.save()
	return nil
}

// access returns a valid access token, refreshing when near expiry.
func (c *Client) access(ctx context.Context) (string, error) {
	c.mu.Lock()
	t := c.tok
	c.mu.Unlock()
	if t == nil || c.ClientID == "" {
		return "", ErrNotConnected
	}
	if time.Until(t.Expiry) > 5*time.Minute {
		return t.Access, nil
	}
	if err := c.exchange(ctx, url.Values{"client_id": {c.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {t.Refresh}}); err != nil {
		return "", fmt.Errorf("oturum yenilenemedi (tekrar bağlan): %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tok.Access, nil
}

// do performs an authorised API call.
func (c *Client) do(ctx context.Context, method, path string, form url.Values, out any) error {
	tok, err := c.access(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	u := path
	if !strings.HasPrefix(u, "http") {
		u = apiBase + path
	}
	req, _ := http.NewRequestWithContext(ctx, method, u, body)
	req.Header.Set("Authorization", "Bearer "+tok)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("MAL: yetki reddedildi (tekrar bağlan)")
	case resp.StatusCode >= 300:
		return fmt.Errorf("MAL: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// Me returns the user's name.
func (c *Client) Me(ctx context.Context) (string, error) {
	var me struct {
		Name string `json:"name"`
	}
	err := c.do(ctx, http.MethodGet, "/users/@me", nil, &me)
	return me.Name, err
}

// ---- list ----

// MAL list statuses.
const (
	Watching    = "watching"
	Completed   = "completed"
	OnHold      = "on_hold"
	Dropped     = "dropped"
	PlanToWatch = "plan_to_watch"
)

// Entry is one anime on the user's MAL list.
type Entry struct {
	ID          int
	Title       string
	Picture     string
	MediaType   string
	NumEpisodes int
	Year        int
	Status      string
	Score       int
	Watched     int
	Tags        []string
	Updated     time.Time
}

// List downloads the user's whole anime list.
func (c *Client) List(ctx context.Context) ([]Entry, error) {
	next := "/users/@me/animelist?limit=1000&nsfw=true&fields=list_status{status,score,num_episodes_watched,tags,updated_at},num_episodes,media_type,start_season,alternative_titles"
	var out []Entry
	for next != "" {
		var res struct {
			Data []struct {
				Node struct {
					ID          int    `json:"id"`
					Title       string `json:"title"`
					NumEpisodes int    `json:"num_episodes"`
					MediaType   string `json:"media_type"`
					MainPicture struct {
						Large  string `json:"large"`
						Medium string `json:"medium"`
					} `json:"main_picture"`
					StartSeason struct {
						Year int `json:"year"`
					} `json:"start_season"`
				} `json:"node"`
				ListStatus struct {
					Status    string    `json:"status"`
					Score     int       `json:"score"`
					Watched   int       `json:"num_episodes_watched"`
					Tags      []string  `json:"tags"`
					UpdatedAt time.Time `json:"updated_at"`
				} `json:"list_status"`
			} `json:"data"`
			Paging struct {
				Next string `json:"next"`
			} `json:"paging"`
		}
		if err := c.do(ctx, http.MethodGet, next, nil, &res); err != nil {
			return nil, err
		}
		for _, d := range res.Data {
			pic := d.Node.MainPicture.Large
			if pic == "" {
				pic = d.Node.MainPicture.Medium
			}
			out = append(out, Entry{
				ID: d.Node.ID, Title: d.Node.Title, Picture: pic, MediaType: d.Node.MediaType,
				NumEpisodes: d.Node.NumEpisodes, Year: d.Node.StartSeason.Year,
				Status: d.ListStatus.Status, Score: d.ListStatus.Score, Watched: d.ListStatus.Watched,
				Tags: d.ListStatus.Tags, Updated: d.ListStatus.UpdatedAt,
			})
		}
		next = res.Paging.Next
	}
	return out, nil
}

// Update is a list change; nil fields are left alone.
type Update struct {
	Status  string
	Score   *int
	Watched *int
	Tags    *[]string
}

// Set updates (or adds) an anime on the list.
func (c *Client) Set(ctx context.Context, id int, u Update) error {
	form := url.Values{}
	if u.Status != "" {
		form.Set("status", u.Status)
	}
	if u.Score != nil {
		form.Set("score", strconv.Itoa(*u.Score))
	}
	if u.Watched != nil {
		form.Set("num_watched_episodes", strconv.Itoa(*u.Watched))
	}
	if u.Tags != nil {
		form.Set("tags", strings.Join(*u.Tags, ","))
	}
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/anime/%d/my_list_status", id), form, nil)
}

// Delete removes an anime from the list.
func (c *Client) Delete(ctx context.Context, id int) error {
	err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/anime/%d/my_list_status", id), nil, nil)
	if err != nil && strings.Contains(err.Error(), "HTTP 404") {
		return nil
	}
	return err
}

// OpenBrowser opens url in the default browser (a var for tests).
var OpenBrowser = openBrowser

func openBrowser(u string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	case "darwin":
		return exec.Command("open", u).Start()
	}
	return exec.Command("xdg-open", u).Start()
}

// SetEndpoints points the client at another server (tests, mirrors).
func SetEndpoints(auth, token, api string) { authURL, tokenURL, apiBase = auth, token, api }

// SetToken installs a token directly (tests).
func (c *Client) SetToken(t Token) {
	c.mu.Lock()
	c.tok = &t
	c.mu.Unlock()
	c.save()
}
