// Package player drives mpv over its JSON IPC: progress, OP/ED skipping and
// "next episode" key bindings, delivered to the UI as a stream of events.
package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aniclitr/internal/aniskip"
	"aniclitr/internal/logx"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

// EventKind enumerates playback events.
type EventKind int

const (
	EvStarted EventKind = iota
	EvProgress
	EvSkipped
	EvEnded
)

// Event is sent to the UI.
type Event struct {
	Kind   EventKind
	Pos    float64
	Dur    float64
	Paused bool
	Text   string // skipped segment label
	Action string // on EvEnded: "next", "prev" or "" (user quit / eof)
	EOF    bool   // file played to the end
	Err    error
}

// Options configure one playback.
type Options struct {
	Title      string
	Stream     *source.Stream
	Quality    source.Quality
	Start      float64
	Fullscreen bool
	Segments   []aniskip.Segment
	ExtraArgs  []string
	MpvPath    string
	// NextLabel ("6. bölüm") enables the end card: when the ending starts
	// (AniSkip ED time, else 90 s before the end) mpv shows what is next.
	NextLabel string
	// Thumb: where to keep a frame of where you are (resume preview). A
	// screenshot is taken every minute and when quitting with q.
	Thumb string
}

// Session is a running mpv instance.
type Session struct {
	Events <-chan Event
	events chan Event

	nextLabel string
	thumb     string
	cmd       *exec.Cmd
	conn      net.Conn
	wmu       sync.Mutex
	action    atomic.Value
}

var counter atomic.Int64

// FindMpv locates the mpv GUI binary (on Windows prefer mpv.exe over the mpv.com console shim).
func FindMpv(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	p, err := exec.LookPath("mpv")
	if err != nil {
		return "", errors.New("mpv bulunamadı — kurulum: winget install mpv-player.mpv-CI.MSVC")
	}
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(p), ".com") {
		exe := strings.TrimSuffix(p, filepath.Ext(p)) + ".exe"
		if _, err := os.Stat(exe); err == nil {
			return exe, nil
		}
	}
	return p, nil
}

// Start launches mpv and begins streaming events.
func Start(o Options) (*Session, error) {
	bin, err := FindMpv(o.MpvPath)
	if err != nil {
		return nil, err
	}
	sock := socketPath()
	args := []string{
		"--force-media-title=" + o.Title,
		"--title=" + o.Title + " — aniclitr",
		"--input-ipc-server=" + sock,
		"--no-terminal",
		"--screenshot-format=jpg", "--screenshot-jpeg-quality=72",
		"--keep-open=no",
		"--hls-bitrate=max",
		"--cache=yes",
		"--demuxer-max-bytes=256MiB",
		"--demuxer-readahead-secs=90",
		"--network-timeout=30",
		"--osd-font-size=36",
	}
	ua := o.Stream.UserAgent
	if ua == "" {
		ua = netx.UserAgent
	}
	args = append(args, "--user-agent="+ua)
	if o.Stream.Referer != "" {
		args = append(args, "--referrer="+o.Stream.Referer)
	}
	for i, s := range o.Stream.Subtitles {
		args = append(args, "--sub-file="+s.URL)
		if i == 0 {
			args = append(args, "--sid=1")
		}
	}
	if o.Start > 5 {
		args = append(args, fmt.Sprintf("--start=%.0f", o.Start))
	}
	if o.Fullscreen {
		args = append(args, "--fs")
	}
	if o.Quality.Page {
		// a host page: mpv's yt-dlp hook resolves it; keep within the preference
		h := o.Quality.Height
		if h <= 0 {
			h = 1080
		}
		args = append(args, fmt.Sprintf("--ytdl-format=bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", h, h))
	}
	args = append(args, o.ExtraArgs...)
	args = append(args, "--", o.Quality.URL)

	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mpv başlatılamadı: %w", err)
	}
	ch := make(chan Event, 64)
	s := &Session{Events: ch, events: ch, cmd: cmd}
	s.action.Store("")
	s.nextLabel, s.thumb = o.NextLabel, o.Thumb
	go s.run(sock, o.Segments)
	return s, nil
}

func socketPath() string {
	n := counter.Add(1)
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\aniclitr-mpv-%d-%d`, os.Getpid(), n)
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("aniclitr-mpv-%d-%d.sock", os.Getpid(), n))
}

func (s *Session) send(cmd ...any) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.conn == nil {
		return
	}
	b, _ := json.Marshal(map[string]any{"command": cmd})
	_ = s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = s.conn.Write(append(b, '\n'))
}

// Quit asks mpv to close.
func (s *Session) Quit() {
	s.send("quit")
	go func() {
		time.Sleep(3 * time.Second)
		if s.cmd.ProcessState == nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}()
}

// Next ends playback asking for the next episode.
func (s *Session) Next() { s.action.Store("next"); s.send("quit") }

// Prev ends playback asking for the previous episode.
func (s *Session) Prev() { s.action.Store("prev"); s.send("quit") }

// TogglePause flips play/pause.
func (s *Session) TogglePause() { s.send("cycle", "pause") }

// Seek moves relative seconds.
func (s *Session) Seek(sec float64) { s.send("seek", sec, "relative") }

// AddSub loads an external subtitle file into the running player and
// selects it; title is shown in mpv's track list.
func (s *Session) AddSub(path, title, lang string) {
	s.send("sub-add", path, "select", title, lang)
	s.send("show-text", "altyazı: "+title, 2500)
}

func (s *Session) run(sock string, segs []aniskip.Segment) {
	// This goroutine lives outside Bubble Tea's panic protection: a panic
	// here would kill the process with the terminal still in raw/alt mode.
	defer func() {
		if r := recover(); r != nil {
			logx.Panic("mpv oturumu", r)
			if s.cmd.Process != nil {
				_ = s.cmd.Process.Kill()
			}
			func() {
				defer func() { _ = recover() }() // channel may already be closed
				s.events <- Event{Kind: EvEnded, Err: fmt.Errorf("oynatıcı hatası: %v", r)}
				close(s.events)
			}()
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()

	// Wait for the IPC endpoint (mpv needs a moment to create it).
	deadline := time.Now().Add(8 * time.Second)
	for s.conn == nil && time.Now().Before(deadline) {
		select {
		case err := <-exited:
			s.events <- Event{Kind: EvEnded, Err: fmt.Errorf("mpv hemen kapandı: %v", err)}
			close(s.events)
			return
		default:
		}
		if c, err := dial(sock); err == nil {
			s.wmu.Lock()
			s.conn = c
			s.wmu.Unlock()
			break
		}
		time.Sleep(120 * time.Millisecond)
	}
	var pos, dur float64
	var paused, eof bool
	if s.conn != nil {
		s.send("observe_property", 1, "time-pos")
		s.send("observe_property", 2, "duration")
		s.send("observe_property", 3, "pause")
		s.send("keybind", "n", "script-message aniclitr-next")
		s.send("keybind", "N", "script-message aniclitr-next")
		s.send("keybind", "b", "script-message aniclitr-prev")
		s.send("keybind", "B", "script-message aniclitr-prev")
		if s.thumb != "" {
			// quitting first grabs the frame you stopped at (resume preview)
			s.send("keybind", "q", "script-message aniclitr-quit")
		}
		s.events <- Event{Kind: EvStarted}

		all := segs
		segs = nil // armed once the real duration is known
		skipped := map[string]bool{}
		var lastEmit time.Time
		endAt, cardShown := 0.0, false
		lastShot := -1000.0
		sc := bufio.NewScanner(s.conn)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var m struct {
				Event  string          `json:"event"`
				Name   string          `json:"name"`
				Data   json.RawMessage `json:"data"`
				Args   []string        `json:"args"`
				Reason string          `json:"reason"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			switch m.Event {
			case "property-change":
				switch m.Name {
				case "time-pos":
					_ = json.Unmarshal(m.Data, &pos)
					// end card: the ending started — say what comes next
					if s.nextLabel != "" && !cardShown && endAt > 0 && pos >= endAt && pos < dur-3 {
						cardShown = true
						s.send("show-text", "▶  Sıradaki: "+s.nextLabel+"\n     n  hemen geç   ·   q  bitir", 9000)
					}
					// resume preview: a frame every minute of playback
					if s.thumb != "" && pos > 8 && pos-lastShot >= 60 {
						lastShot = pos
						s.send("screenshot-to-file", s.thumb, "video")
					}
					for _, sg := range segs {
						if !skipped[sg.Label()] && pos >= sg.Start && pos < sg.End-1.5 {
							skipped[sg.Label()] = true
							s.send("seek", sg.End, "absolute")
							s.send("show-text", "» "+sg.Label()+" atlandı", 1800)
							s.events <- Event{Kind: EvSkipped, Text: sg.Label(), Pos: pos, Dur: dur}
						}
					}
				case "duration":
					_ = json.Unmarshal(m.Data, &dur)
					if segs == nil && dur > 0 {
						segs = aniskip.ForDuration(all, dur)
						for _, sg := range segs {
							if sg.Kind == "ed" || sg.Kind == "mixed-ed" {
								endAt = sg.Start
							}
						}
						if endAt == 0 && dur > 600 {
							endAt = dur - 90
						}
					}
				case "pause":
					_ = json.Unmarshal(m.Data, &paused)
				}
				if time.Since(lastEmit) > 400*time.Millisecond {
					lastEmit = time.Now()
					select {
					case s.events <- Event{Kind: EvProgress, Pos: pos, Dur: dur, Paused: paused}:
					default: // UI busy; drop a frame rather than block mpv
					}
				}
			case "client-message":
				if len(m.Args) > 0 {
					switch m.Args[0] {
					case "aniclitr-next":
						s.action.Store("next")
						s.send("quit")
					case "aniclitr-prev":
						s.action.Store("prev")
						s.send("quit")
					case "aniclitr-quit":
						if s.thumb != "" && pos > 8 {
							s.send("screenshot-to-file", s.thumb, "video")
							time.Sleep(350 * time.Millisecond) // let mpv write it
						}
						s.send("quit")
					}
				}
			case "end-file":
				if m.Reason == "eof" {
					eof = true
				}
			}
		}
	}
	err := <-exited
	if s.conn != nil {
		s.conn.Close()
	}
	act, _ := s.action.Load().(string)
	if err != nil && act == "" && pos == 0 {
		s.events <- Event{Kind: EvEnded, Pos: pos, Dur: dur, Err: fmt.Errorf("mpv: %v", err)}
	} else {
		s.events <- Event{Kind: EvEnded, Pos: pos, Dur: dur, Action: act, EOF: eof}
	}
	close(s.events)
}
