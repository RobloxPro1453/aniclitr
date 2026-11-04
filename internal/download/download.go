// Package download saves episodes for offline watching.
//
// A job resolves its stream right before downloading (CDN links are signed
// and expire), then hands the file to the best tool available:
//   - aria2c for direct files: many connections, resumable;
//   - yt-dlp for HLS playlists (m3u8), muxed to mp4 with ffmpeg;
//   - a built-in HTTP downloader when neither is installed.
//
// Subtitles that come with the stream are saved next to the video with the
// same base name, so mpv picks them up automatically.
package download

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/config"
	"aniclitr/internal/logx"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

// Job states.
const (
	Queued    = "sırada"
	Resolving = "hazırlanıyor"
	Running   = "iniyor"
	Done      = "bitti"
	Failed    = "hata"
	Canceled  = "iptal"
)

// Job is one episode download.
type Job struct {
	ID       int       `json:"id"`
	Source   string    `json:"source"`
	AnimeID  string    `json:"anime_id"`
	Title    string    `json:"title"`
	Poster   string    `json:"poster,omitempty"`
	Kind     string    `json:"kind,omitempty"`
	MalID    int       `json:"mal_id,omitempty"`
	Season   int       `json:"season"`
	Episode  int       `json:"episode"`
	EpID     string    `json:"ep_id"`
	EpTitle  string    `json:"ep_title,omitempty"`
	Fansub   string    `json:"fansub,omitempty"`
	Host     string    `json:"host,omitempty"`
	Quality  string    `json:"quality,omitempty"`
	Path     string    `json:"path,omitempty"`
	Strict   bool      `json:"strict,omitempty"`      // only the chosen fansub, never another
	Want     string    `json:"want_fansub,omitempty"` // the fansub you chose (kept for retries)
	Conns    int       `json:"-"`                     // connections aria2c has open
	State    string    `json:"state"`
	Err      string    `json:"err,omitempty"`
	Tool     string    `json:"tool,omitempty"`
	Done     int64     `json:"done"`
	Total    int64     `json:"total"`
	Added    time.Time `json:"added"`
	Finished time.Time `json:"finished,omitempty"`

	Speed float64       `json:"-"` // bytes/s
	ETA   time.Duration `json:"-"`

	// request (not persisted)
	prefFansub string
	prefQual   string
	variant    *source.Variant
	stream     *source.Stream
	quality    *source.Quality
	cancel     context.CancelFunc
}

// Key identifies the anime.
func (j Job) Key() string { return j.Source + ":" + j.AnimeID }

// Anime rebuilds a minimal source.Anime.
func (j Job) Anime() source.Anime {
	return source.Anime{Source: j.Source, ID: j.AnimeID, Title: j.Title, Poster: j.Poster, Kind: j.Kind, MalID: j.MalID}
}

// Frac is the progress 0..1.
func (j Job) Frac() float64 {
	if j.State == Done {
		return 1
	}
	if j.Total <= 0 {
		return 0
	}
	return float64(j.Done) / float64(j.Total)
}

// Request describes what to download. Variant/Stream/Quality are optional:
// without them the manager picks the preferred fansub, the first working
// host and the preferred quality itself.
type Request struct {
	Anime      source.Anime
	Ep         source.Episode
	PrefFansub string
	PrefQual   string // "best", "1080p", "720p"…
	// Strict: download only from PrefFansub (fail rather than switch fansub).
	Strict  bool
	Variant *source.Variant
	Stream  *source.Stream
	Quality *source.Quality
}

// Manager runs the queue (2 at a time) and persists finished jobs.
type Manager struct {
	mu      sync.Mutex
	jobs    []*Job
	nextID  int
	running int
	max     int
	dir     string
	path    string
	Updates chan struct{} // signalled (coalesced) whenever something changes
	lastSig time.Time
}

// Open loads the download index.
func Open(dir string) *Manager {
	m := &Manager{max: 2, dir: dir, path: filepath.Join(config.Dir(), "downloads.json"), Updates: make(chan struct{}, 1)}
	if b, err := os.ReadFile(m.path); err == nil {
		var jobs []*Job
		if err := json.Unmarshal(b, &jobs); err != nil {
			_ = os.WriteFile(m.path+".bozuk", b, 0o644)
			logx.Errorf("indirme listesi okunamadı: %v", err)
		}
		for _, j := range jobs {
			if j.State != Done && j.State != Failed && j.State != Canceled {
				j.State, j.Err = Failed, "yarıda kaldı — r ile devam et"
			}
			if j.ID >= m.nextID {
				m.nextID = j.ID + 1
			}
			m.jobs = append(m.jobs, j)
		}
	}
	return m
}

// Dir is the download folder.
func (m *Manager) Dir() string { return m.dir }

// SetDir changes the folder for new downloads.
func (m *Manager) SetDir(d string) { m.mu.Lock(); m.dir = d; m.mu.Unlock() }

// Jobs returns copies, newest first.
func (m *Manager) Jobs() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		c := *j
		c.cancel = nil
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// Active reports whether anything is queued or running.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, j := range m.jobs {
		if j.State == Queued || j.State == Resolving || j.State == Running {
			n++
		}
	}
	return n
}

// Local returns the finished file for an episode, if it still exists.
func (m *Manager) Local(animeKey string, season, ep int) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.Key() == animeKey && j.Season == season && j.Episode == ep && j.State == Done {
			if _, err := os.Stat(j.Path); err == nil {
				return *j, true
			}
		}
	}
	return Job{}, false
}

// For returns the latest job for an episode (any state).
func (m *Manager) For(animeKey string, season, ep int) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *Job
	for _, j := range m.jobs {
		if j.Key() == animeKey && j.Season == season && j.Episode == ep && (best == nil || j.ID > best.ID) {
			best = j
		}
	}
	if best == nil {
		return Job{}, false
	}
	return *best, true
}

// Add queues a download (no-op if that episode is already queued/done).
func (m *Manager) Add(r Request) (Job, bool) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.Key() == r.Anime.Key() && j.Season == r.Ep.Season && j.Episode == r.Ep.Number &&
			(j.State == Queued || j.State == Resolving || j.State == Running || (j.State == Done && fileExists(j.Path))) {
			c := *j
			m.mu.Unlock()
			return c, false
		}
	}
	j := &Job{
		ID: m.nextID, Source: r.Anime.Source, AnimeID: r.Anime.ID, Title: r.Anime.Title, Poster: r.Anime.Poster, Kind: r.Anime.Kind, MalID: r.Anime.MalID,
		Season: r.Ep.Season, Episode: r.Ep.Number, EpID: r.Ep.ID, EpTitle: r.Ep.Title,
		State: Queued, Added: time.Now(), prefFansub: r.PrefFansub, prefQual: r.PrefQual, Strict: r.Strict, Want: r.PrefFansub,
		variant: r.Variant, stream: r.Stream, quality: r.Quality,
	}
	if r.Variant != nil {
		j.Fansub, j.Host = r.Variant.Fansub, r.Variant.Host
	}
	m.nextID++
	m.jobs = append(m.jobs, j)
	c := *j
	m.mu.Unlock()
	m.changed(true)
	m.pump()
	return c, true
}

// Retry re-queues a failed/canceled job.
func (m *Manager) Retry(id int) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.ID == id && (j.State == Failed || j.State == Canceled) {
			j.State, j.Err, j.stream, j.quality = Queued, "", nil, nil // links expire: resolve again
			switch {
			case j.Want != "":
				j.prefFansub = j.Want // a retry keeps the fansub you chose
			case j.variant == nil && j.Fansub != "":
				j.prefFansub = j.Fansub
			}
		}
	}
	m.mu.Unlock()
	m.changed(true)
	m.pump()
}

// Cancel stops a running/queued job; Remove forgets it (deleteFile also removes the video).
func (m *Manager) Cancel(id int) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.ID == id && (j.State == Queued || j.State == Resolving || j.State == Running) {
			j.State = Canceled
			if j.cancel != nil {
				j.cancel()
			}
		}
	}
	m.mu.Unlock()
	m.changed(true)
}

func (m *Manager) Remove(id int, deleteFile bool) {
	m.mu.Lock()
	var keep []*Job
	for _, j := range m.jobs {
		if j.ID == id {
			if j.cancel != nil {
				j.cancel()
			}
			if deleteFile && j.Path != "" {
				_ = os.Remove(j.Path)
				base := strings.TrimSuffix(j.Path, filepath.Ext(j.Path))
				if subs, _ := filepath.Glob(base + "*.vtt"); len(subs) > 0 {
					for _, s := range subs {
						_ = os.Remove(s)
					}
				}
			}
			continue
		}
		keep = append(keep, j)
	}
	m.jobs = keep
	m.mu.Unlock()
	m.changed(true)
}

// changed persists (when asked) and signals the UI, at most ~3×/s for
// progress-only updates.
func (m *Manager) changed(persist bool) {
	if persist {
		m.save()
	}
	m.mu.Lock()
	if !persist && time.Since(m.lastSig) < 300*time.Millisecond {
		m.mu.Unlock()
		return
	}
	m.lastSig = time.Now()
	m.mu.Unlock()
	select {
	case m.Updates <- struct{}{}:
	default:
	}
}

func (m *Manager) save() {
	m.mu.Lock()
	b, err := json.MarshalIndent(m.jobs, "", " ")
	m.mu.Unlock()
	if err == nil {
		_ = config.WriteAtomic(m.path, b)
	}
}

// pump starts queued jobs while there is capacity.
func (m *Manager) pump() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// oldest first
	for _, j := range m.jobs {
		if m.running >= m.max {
			return
		}
		if j.State != Queued {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		j.cancel = cancel
		j.State = Resolving
		m.running++
		go m.run(ctx, j)
	}
}

func (m *Manager) run(ctx context.Context, j *Job) {
	defer func() {
		if r := recover(); r != nil {
			logx.Panic("indirme", r)
			m.finish(j, fmt.Errorf("iç hata: %v", r))
		}
	}()
	m.changed(false)
	err := m.do(ctx, j)
	m.finish(j, err)
}

func (m *Manager) finish(j *Job, err error) {
	m.mu.Lock()
	m.running--
	switch {
	case j.State == Canceled:
	case err != nil:
		j.State, j.Err = Failed, err.Error()
		logx.Errorf("indirme [%s S%dE%d]: %v", j.Title, j.Season, j.Episode, err)
	default:
		j.State, j.Finished, j.Err = Done, time.Now(), ""
		if st, e := os.Stat(j.Path); e == nil {
			j.Done, j.Total = st.Size(), st.Size()
		}
	}
	j.Speed, j.ETA = 0, 0
	j.cancel = nil
	m.mu.Unlock()
	m.changed(true)
	m.pump()
}

func (m *Manager) set(f func()) {
	m.mu.Lock()
	f()
	m.mu.Unlock()
}

func (m *Manager) do(ctx context.Context, j *Job) error {
	st, q, err := m.resolve(ctx, j)
	if err != nil {
		return err
	}
	m.mu.Lock()
	dir := filepath.Join(m.dir, safeName(j.Title))
	m.mu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// the cover travels with the videos: the offline library shows it
	if cover := filepath.Join(dir, "kapak.jpg"); j.Poster != "" && !fileExists(cover) {
		if b, err := netx.Get(ctx, strings.Replace(j.Poster, "/t/p/original/", "/t/p/w500/", 1), nil); err == nil && len(b) > 0 {
			_ = os.WriteFile(cover, b, 0o644)
		}
	}
	name := fmt.Sprintf("S%02dE%02d", j.Season, j.Episode)
	if j.Kind == "Film" || strings.EqualFold(j.Kind, "movie") {
		name = "Film"
	}
	if j.Fansub != "" {
		name += " [" + safeName(shortName(j.Fansub)) + "]"
	}
	if q.Label != "" {
		name += " [" + safeName(q.Label) + "]"
	}
	hls := strings.Contains(strings.ToLower(q.URL), ".m3u8") || q.Page // yt-dlp opens host pages too
	path := filepath.Join(dir, name+".mp4")
	m.set(func() { j.Path, j.Quality, j.State = path, q.Label, Running })
	m.changed(true)
	// subtitles alongside (best effort)
	for i, s := range st.Subtitles {
		suffix := ""
		if i > 0 {
			suffix = "." + safeName(s.Label)
		}
		if b, err := netx.Get(ctx, s.URL, map[string]string{"Referer": st.Referer}); err == nil {
			_ = os.WriteFile(filepath.Join(dir, name+suffix+".vtt"), b, 0o644)
		}
	}
	switch {
	case hls:
		return m.ytdlp(ctx, j, st, q, path)
	case have("aria2c"):
		return m.aria2(ctx, j, st, q, dir, name+".mp4")
	}
	return m.native(ctx, j, st, q, path)
}

// resolve picks fansub → host → quality like quick play does.
func (m *Manager) resolve(ctx context.Context, j *Job) (*source.Stream, source.Quality, error) {
	if j.stream != nil && j.quality != nil {
		return j.stream, *j.quality, nil
	}
	p := source.Get(j.Source)
	if p == nil {
		return nil, source.Quality{}, errors.New("kaynak yok: " + j.Source)
	}
	var vs []source.Variant
	if j.variant != nil {
		vs = []source.Variant{*j.variant}
	} else {
		all, err := p.Variants(ctx, j.Anime(), source.Episode{ID: j.EpID, Season: j.Season, Number: j.Episode, Title: j.EpTitle})
		if err != nil {
			return nil, source.Quality{}, fmt.Errorf("kaynaklar alınamadı: %w", err)
		}
		vs = orderByFansub(all, j.prefFansub)
		if j.Strict && j.prefFansub != "" {
			// you picked a fansub: its hosts only — never silently another one
			var mine []source.Variant
			p := strings.ToLower(shortName(j.prefFansub))
			for _, v := range vs {
				if strings.Contains(strings.ToLower(v.Fansub), p) || strings.Contains(p, strings.ToLower(shortName(v.Fansub))) {
					mine = append(mine, v)
				}
			}
			if len(mine) == 0 {
				return nil, source.Quality{}, fmt.Errorf("bu bölümde “%s” fansubu yok — başka fansubla tekrar dene", shortName(j.prefFansub))
			}
			vs = mine
		}
	}
	var lastErr error
	for _, v := range vs {
		if ctx.Err() != nil {
			return nil, source.Quality{}, ctx.Err()
		}
		st, err := p.Resolve(ctx, v)
		if err != nil || st == nil || len(st.Qualities) == 0 {
			lastErr = err
			continue
		}
		st.SortQualities()
		q := pickQuality(st, j.prefQual)
		m.set(func() { j.Fansub, j.Host = v.Fansub, v.Host })
		return st, q, nil
	}
	if lastErr == nil {
		lastErr = errors.New("çalışan kaynak yok")
	}
	if j.Strict && j.prefFansub != "" {
		lastErr = fmt.Errorf("“%s” fansubunun kaynakları açılmadı (%v)", shortName(j.prefFansub), lastErr)
	}
	return nil, source.Quality{}, lastErr
}

// orderByFansub puts the preferred fansub's hosts first, keeping the
// provider's host ranking inside each group.
func orderByFansub(vs []source.Variant, pref string) []source.Variant {
	if pref == "" {
		return vs
	}
	p := strings.ToLower(shortName(pref))
	var first, rest []source.Variant
	for _, v := range vs {
		if strings.Contains(strings.ToLower(v.Fansub), p) {
			first = append(first, v)
		} else {
			rest = append(rest, v)
		}
	}
	return append(first, rest...)
}

func pickQuality(st *source.Stream, pref string) source.Quality {
	q := st.Qualities[0]
	want := strings.TrimSuffix(strings.ToLower(pref), "p")
	if want == "" || want == "best" {
		return q
	}
	h, _ := strconv.Atoi(want)
	for _, c := range st.Qualities {
		if c.Height > 0 && c.Height <= h {
			return c
		}
	}
	return q
}

// ---- tools ----

// noTools forces the built-in downloader (tests).
var noTools bool

func have(bin string) bool {
	if noTools {
		return false
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// Tools reports which external downloaders were found.
func Tools() (aria2, ytdlp bool) { return have("aria2c"), have("yt-dlp") }

func headers(st *source.Stream) (ref, ua string) {
	ref, ua = st.Referer, st.UserAgent
	if ua == "" {
		ua = netx.UserAgent
	}
	return
}

var reAria = regexp.MustCompile(`\[#\w+ ([\d.]+)(\w+)/([\d.]+)(\w+)\((\d+)%\)[^\]]*?DL:([\d.]+)(\w+)(?: ETA:([\dhms]+))?`)
var reConns = regexp.MustCompile(`CN:(\d+)`)

func (m *Manager) aria2(ctx context.Context, j *Job, st *source.Stream, q source.Quality, dir, name string) error {
	ref, ua := headers(st)
	args := []string{
		"--dir=" + dir, "--out=" + name,
		"--max-connection-per-server=16", "--split=16", "--min-split-size=1M",
		"--continue=true", "--auto-file-renaming=false", "--allow-overwrite=true",
		"--file-allocation=none", "--summary-interval=1", "--console-log-level=warn",
		// a slow or dropped connection is retried instead of stalling the file
		"--max-tries=10", "--retry-wait=2", "--timeout=30", "--connect-timeout=15",
		"--enable-color=false", "--user-agent=" + ua, "--check-certificate=false",
	}
	if ref != "" {
		args = append(args, "--referer="+ref)
	}
	args = append(args, q.URL)
	m.set(func() { j.Tool = "aria2c" })
	return m.runTool(ctx, j, "aria2c", args, func(line string) {
		mm := reAria.FindStringSubmatch(line)
		if mm == nil {
			return
		}
		done := size(mm[1], mm[2])
		total := size(mm[3], mm[4])
		speed := size(mm[6], mm[7])
		eta, _ := time.ParseDuration(mm[8])
		conns := 0
		if c := reConns.FindStringSubmatch(line); c != nil {
			conns, _ = strconv.Atoi(c[1])
		}
		m.set(func() { j.Done, j.Total, j.Speed, j.ETA, j.Conns = done, total, float64(speed), eta, conns })
		m.changed(false)
	})
}

var reYt = regexp.MustCompile(`^dl\|([\d.]*)\|(\d*)\|([\d.]*)\|([\d.]*)\|(\d*)`)

func (m *Manager) ytdlp(ctx context.Context, j *Job, st *source.Stream, q source.Quality, path string) error {
	ref, ua := headers(st)
	args := []string{
		"--newline", "--no-warnings", "--no-part", "--force-overwrites",
		"--concurrent-fragments", "8", "--remux-video", "mp4",
		"--progress-template", "dl|%(progress.fragment_index)s|%(progress.downloaded_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s|%(progress.eta)s",
		"--user-agent", ua, "-o", path,
	}
	if ref != "" {
		args = append(args, "--referer", ref)
	}
	if q.Page {
		// a host page (Google Drive): yt-dlp picks the file, within the height
		h := q.Height
		if h <= 0 {
			h = 1080
		}
		args = append(args, "-f", fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", h, h))
	}
	args = append(args, q.URL)
	m.set(func() { j.Tool = "yt-dlp" })
	return m.runTool(ctx, j, "yt-dlp", args, func(line string) {
		mm := reYt.FindStringSubmatch(strings.ReplaceAll(line, "NA", ""))
		if mm == nil {
			return
		}
		done, _ := strconv.ParseInt(mm[2], 10, 64)
		total, _ := strconv.ParseFloat(mm[3], 64)
		speed, _ := strconv.ParseFloat(mm[4], 64)
		eta, _ := strconv.Atoi(mm[5])
		m.set(func() { j.Done, j.Total, j.Speed, j.ETA = done, int64(total), speed, time.Duration(eta)*time.Second })
		m.changed(false)
	})
}

func (m *Manager) runTool(ctx context.Context, j *Job, bin string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	hideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	var tail []string
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	sc.Split(scanLinesCR)
	for sc.Scan() {
		line := sc.Text()
		onLine(line)
		if strings.TrimSpace(line) != "" {
			tail = append(tail, line)
			if len(tail) > 6 {
				tail = tail[1:]
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logx.Errorf("%s: %v\n%s", bin, err, strings.Join(tail, "\n"))
		return fmt.Errorf("%s başarısız (%v)", bin, err)
	}
	return nil
}

// native: single-connection HTTP download with resume.
func (m *Manager) native(ctx context.Context, j *Job, st *source.Stream, q source.Quality, path string) error {
	ref, ua := headers(st)
	m.set(func() { j.Tool = "dahili" })
	part := path + ".part"
	var have int64
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	if ref != "" {
		req.Header.Set("Referer", ref)
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	client := &http.Client{Jar: netx.Client.Jar} // no overall timeout: files are big
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	switch resp.StatusCode {
	case http.StatusPartialContent:
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	case http.StatusOK:
		have = 0
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	total := have + resp.ContentLength
	buf := make([]byte, 256*1024)
	done := have
	last, lastDone := time.Now(), done
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
		}
		if el := time.Since(last); el > 500*time.Millisecond || rerr != nil {
			sp := float64(done-lastDone) / el.Seconds()
			last, lastDone = time.Now(), done
			eta := time.Duration(0)
			if sp > 0 && total > done {
				eta = time.Duration(float64(total-done)/sp) * time.Second
			}
			m.set(func() { j.Done, j.Total, j.Speed, j.ETA = done, total, sp, eta })
			m.changed(false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(part, path)
}

// ---- helpers ----

func scanLinesCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func size(num, unit string) int64 {
	v, _ := strconv.ParseFloat(num, 64)
	mult := map[string]float64{"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}[unit]
	if mult == 0 {
		mult = 1
	}
	return int64(v * mult)
}

var reBad = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func safeName(s string) string {
	s = strings.TrimSpace(reBad.ReplaceAllString(s, " "))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, ". ")
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80])
	}
	if s == "" {
		s = "anime"
	}
	return s
}

// shortName trims credit lines ("Çeviri: X / Encode: Y" → "X").
func shortName(s string) string {
	for _, sep := range []string{"/", "|", ","} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	if i := strings.Index(s, ":"); i >= 0 && i < len(s)-1 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// DefaultDir is ~/Videos/aniclitr.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(config.Dir(), "indirilenler")
	}
	return filepath.Join(home, "Videos", "aniclitr")
}

// Preview inserts a job as-is (UI snapshots/tests only; never runs it).
func (m *Manager) Preview(j Job) {
	m.mu.Lock()
	c := j
	m.jobs = append(m.jobs, &c)
	m.mu.Unlock()
}
