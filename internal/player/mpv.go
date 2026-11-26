// Package player drives mpv over its JSON IPC: progress, OP/ED skipping and
// "next episode" key bindings, delivered to the UI as a stream of events.
package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aniclitr/internal/aniskip"
	"aniclitr/internal/logx"
	"aniclitr/internal/netx"
	"aniclitr/internal/shaders"
	"aniclitr/internal/source"
	"aniclitr/internal/tools"
)

// EventKind enumerates playback events.
type EventKind int

const (
	EvStarted EventKind = iota
	EvProgress
	EvSkipped
	EvAction
	EvEnded
)

// Event is sent to the UI.
type Event struct {
	Kind     EventKind
	Pos      float64
	Dur      float64
	Speed    float64
	SubDelay float64
	VideoH   int
	Volume   int
	Muted    bool
	Paused   bool
	Text     string // skipped segment label
	Action   string // EvEnded: "next", "prev"; EvAction: live in-mpv command
	EOF      bool   // file played to the end
	Err      error
}

// Options configure one playback.
type Options struct {
	Title      string
	Stream     *source.Stream
	Quality    source.Quality
	Start      float64
	Speed      float64
	Fullscreen bool
	Anime4K    string // "", "kapalı", "hafif", "dengeli", "yüksek"
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

	initSpeed float64
	nextLabel string
	thumb     string
	cmd       *exec.Cmd
	conn      net.Conn
	wmu       sync.Mutex
	action    atomic.Value
	subMu     sync.Mutex
	subDelay  float64
}

var counter atomic.Int64

// FindMpv locates the mpv GUI binary (on Windows prefer mpv.exe over the mpv.com console shim).
func FindMpv(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	p, err := tools.LookPath("mpv")
	if err != nil {
		return "", errors.New("mpv bulunamadı — otomatik kurulum için: aniclitr kur")
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
	args := buildArgs(o, sock)

	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mpv başlatılamadı: %w", err)
	}
	ch := make(chan Event, 64)
	s := &Session{Events: ch, events: ch, cmd: cmd, initSpeed: o.Speed}
	s.action.Store("")
	s.nextLabel, s.thumb = o.NextLabel, o.Thumb
	go s.run(sock, o.Segments)
	return s, nil
}

func buildArgs(o Options, sock string) []string {
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
	ua := ""
	if o.Stream != nil {
		ua = o.Stream.UserAgent
	}
	if ua == "" {
		ua = netx.UserAgent
	}
	args = append(args, "--user-agent="+ua)
	if o.Stream != nil && o.Stream.Referer != "" {
		args = append(args, "--referrer="+o.Stream.Referer)
	}
	if o.Stream != nil {
		for i, s := range o.Stream.Subtitles {
			args = append(args, "--sub-file="+s.URL)
			if i == 0 {
				args = append(args, "--sid=1")
			}
		}
	}
	if o.Start > 5 {
		args = append(args, fmt.Sprintf("--start=%.0f", o.Start))
	}
	if o.Speed > 0 && o.Speed != 1 {
		args = append(args, fmt.Sprintf("--speed=%.2f", o.Speed))
	}
	if o.Fullscreen {
		args = append(args, "--fs")
	}
	if a4k := shaders.MpvArgs(o.Anime4K); len(a4k) > 0 {
		args = append(args, a4k...)
	}
	if o.Quality.HLSBandwidth > 0 {
		// URL is the master playlist (the rendition's audio is a separate
		// group): pick the chosen rendition; a later option overrides "max"
		args = append(args, fmt.Sprintf("--hls-bitrate=%d", o.Quality.HLSBandwidth))
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
	return args
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

func (s *Session) sendNamed(cmd map[string]any) {
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

// SetPause sets explicit pause state (used by Watch Party sync).
func (s *Session) SetPause(paused bool) { s.send("set_property", "pause", paused) }

// Seek moves relative seconds.
func (s *Session) Seek(sec float64) { s.send("seek", sec, "relative") }

// SeekAbs jumps to an absolute timestamp in seconds (used by Watch Party sync).
func (s *Session) SeekAbs(sec float64) { s.send("seek", sec, "absolute") }

// SetShaders switches upscaling profile (kapalı / 1080p / 2k / 4k) live without restarting mpv.
// Uses native JSON []string set_property so Windows backslashes and semicolons never break.
func (s *Session) SetShaders(profile string) {
	prof := shaders.Normalize(profile)
	paths, err := shaders.Paths(prof)
	if err != nil || len(paths) == 0 {
		s.send("set_property", "glsl-shaders", []string{})
		s.send("set_property", "scale", "bilinear")
		s.send("set_property", "cscale", "bilinear")
		s.send("set_property", "sigmoid-upscaling", false)
		s.send("show-text", "Görüntü Yükseltme: Kapalı", 2200)
		return
	}
	s.send("set_property", "hwdec", "auto-safe")
	s.send("set_property", "deband", true)
	s.send("set_property", "deband-iterations", 1)
	s.send("set_property", "deband-threshold", 42)
	s.send("set_property", "deband-range", 16)
	s.send("set_property", "cscale", "spline36")
	if prof == shaders.P4K {
		s.send("set_property", "scale", "ewa_lanczossharp")
		s.send("set_property", "sigmoid-upscaling", true)
	} else {
		s.send("set_property", "scale", "spline36")
		s.send("set_property", "sigmoid-upscaling", false)
	}
	s.send("set_property", "glsl-shaders", paths)
	s.send("show-text", "✦ Yükseltme: "+shaders.ShortLabel(prof), 2400)
}

// SetVolume sets mpv volume percentage (0..130) live.
func (s *Session) SetVolume(vol int) {
	if vol < 0 {
		vol = 0
	}
	if vol > 130 {
		vol = 130
	}
	s.send("set_property", "mute", false)
	s.send("set_property", "volume", vol)
}

// ToggleMute flips mpv audio mute live.
func (s *Session) ToggleMute() {
	s.send("cycle", "mute")
}

// SetSpeed changes playback speed live inside mpv.
func (s *Session) SetSpeed(speed float64) {
	if speed <= 0 {
		speed = 1.0
	}
	s.send("set_property", "speed", speed)
	s.send("show-text", fmt.Sprintf("Hız: %.2g×", speed), 1800)
}

// AddSub loads an external subtitle file into the running player and
// selects it; title is shown in mpv's track list.
func (s *Session) AddSub(path, title, lang string) {
	s.send("sub-add", path, "select", title, lang)
	s.send("show-text", "✓ Altyazı yüklendi: "+title, 2800)
}

// FormatSubDelay formats a subtitle delay in seconds for Turkish OSD/UI display.
func FormatSubDelay(sec float64) string {
	sec = math.Round(sec*10) / 10
	if math.Abs(sec) < 0.05 {
		return "0,0 sn"
	}
	s := strings.ReplaceAll(fmt.Sprintf("%+.1f sn", sec), ".", ",")
	if sec < 0 {
		return s + " (erken)"
	}
	return s + " (geç)"
}

// AdjustSubDelay shifts mpv's subtitle sync by delta seconds and shows Turkish OSD.
func (s *Session) AdjustSubDelay(delta float64) float64 {
	s.subMu.Lock()
	v := math.Round((s.subDelay+delta)*10) / 10
	if math.Abs(v) < 0.05 {
		v = 0
	}
	s.subDelay = v
	s.subMu.Unlock()
	s.send("set_property", "sub-delay", v)
	s.send("show-text", "Altyazı senkronu: "+FormatSubDelay(v), 2200)
	return v
}

// ResetSubDelay resets mpv's subtitle delay to 0.0s.
func (s *Session) ResetSubDelay() {
	s.subMu.Lock()
	s.subDelay = 0
	s.subMu.Unlock()
	s.send("set_property", "sub-delay", 0.0)
	s.send("show-text", "Altyazı senkronu: 0,0 sn (sıfırlandı)", 2200)
}

// ShowOSD flashes a short message inside mpv.
func (s *Session) ShowOSD(text string, ms int) {
	if ms <= 0 {
		ms = 2200
	}
	s.send("show-text", text, ms)
}

// ShowSubMenu displays an interactive ASS overlay inside mpv and activates
// modal navigation keys (arrows, Enter, 1-9, Esc) without requiring Alt-Tab.
func (s *Session) ShowSubMenu(ass string) {
	s.send("enable-section", "aniclitr-subs", "allow-hide-cursor+allow-vo-dragging")
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     77,
		"format": "ass-events",
		"data":   ass,
		"res_x":  1280,
		"res_y":  720,
	})
}

// HideSubMenu clears the in-mpv subtitle overlay and restores normal mpv keys.
func (s *Session) HideSubMenu() {
	s.send("disable-section", "aniclitr-subs")
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     77,
		"format": "none",
		"data":   "",
	})
}

// ShowQuickMenu displays the interactive in-mpv control dropdown (upscale 1080p/2K/4K,
// sleep timer, speed, subtitle picker) directly over the video via ASS OSD.
func (s *Session) ShowQuickMenu(ass string) {
	s.send("enable-section", "aniclitr-menu", "allow-hide-cursor+allow-vo-dragging")
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     78,
		"format": "ass-events",
		"data":   ass,
		"res_x":  1280,
		"res_y":  720,
	})
}

// HideQuickMenu clears the in-mpv quick control dropdown and restores normal keys.
func (s *Session) HideQuickMenu() {
	s.send("disable-section", "aniclitr-menu")
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     78,
		"format": "none",
		"data":   "",
	})
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
	speed := s.initSpeed
	if speed <= 0 {
		speed = 1.0
	}
	var paused, eof, muted bool
	videoH := 0
	volume := 100
	if s.conn != nil {
		s.send("observe_property", 1, "time-pos")
		s.send("observe_property", 2, "duration")
		s.send("observe_property", 3, "pause")
		s.send("observe_property", 4, "speed")
		s.send("observe_property", 5, "sub-delay")
		s.send("observe_property", 6, "video-params/h")
		s.send("observe_property", 7, "volume")
		s.send("observe_property", 8, "mute")
		s.send("keybind", "n", "script-message aniclitr-next")
		s.send("keybind", "N", "script-message aniclitr-next")
		s.send("keybind", "b", "script-message aniclitr-prev")
		s.send("keybind", "B", "script-message aniclitr-prev")
		// y opens the in-mpv AniSub overlay (never clashes with mpv's built-in u)
		s.send("keybind", "y", "script-message aniclitr-subs")
		// Subtitle sync fine-tuning directly in mpv: z/x ±0.1s, Z/X ±0.5s, Alt+0 reset
		s.send("keybind", "z", "script-message aniclitr-subdelay -0.1")
		s.send("keybind", "x", "script-message aniclitr-subdelay +0.1")
		s.send("keybind", "Z", "script-message aniclitr-subdelay -0.5")
		s.send("keybind", "X", "script-message aniclitr-subdelay +0.5")
		s.send("keybind", "Alt+0", "script-message aniclitr-subdelay 0")
		// m / Tab / Alt+4 open the interactive in-mpv Nova command center
		s.send("keybind", "m", "script-message aniclitr-menu")
		s.send("keybind", "TAB", "script-message aniclitr-menu")
		s.send("keybind", "Alt+4", "script-message aniclitr-menu")
		s.send("keybind", "Alt+s", "script-message aniclitr-sleep")
		menuSection := strings.Join([]string{
			"UP repeatable script-message aniclitr-menu-up",
			"DOWN repeatable script-message aniclitr-menu-down",
			"LEFT repeatable script-message aniclitr-menu-left",
			"RIGHT repeatable script-message aniclitr-menu-right",
			"k repeatable script-message aniclitr-menu-up",
			"j repeatable script-message aniclitr-menu-down",
			"h repeatable script-message aniclitr-menu-left",
			"l repeatable script-message aniclitr-menu-right",
			"ENTER script-message aniclitr-menu-pick",
			"KP_ENTER script-message aniclitr-menu-pick",
			"ESC script-message aniclitr-menu-close",
			"m script-message aniclitr-menu-close",
			"TAB script-message aniclitr-menu-close",
			"1 script-message aniclitr-menu-num 1",
			"2 script-message aniclitr-menu-num 2",
			"3 script-message aniclitr-menu-num 3",
			"4 script-message aniclitr-menu-num 4",
			"5 script-message aniclitr-menu-num 5",
			"6 script-message aniclitr-menu-num 6",
			"7 script-message aniclitr-menu-num 7",
			"8 script-message aniclitr-menu-num 8",
			"9 script-message aniclitr-menu-num 9",
			"0 script-message aniclitr-menu-num 10",
		}, "\n")
		s.send("define-section", "aniclitr-menu", menuSection, "default")
		subSection := strings.Join([]string{
			"UP repeatable script-message aniclitr-sub-up",
			"DOWN repeatable script-message aniclitr-sub-down",
			"k repeatable script-message aniclitr-sub-up",
			"j repeatable script-message aniclitr-sub-down",
			"ENTER script-message aniclitr-sub-pick",
			"KP_ENTER script-message aniclitr-sub-pick",
			"ESC script-message aniclitr-sub-close",
			"y script-message aniclitr-sub-close",
			"1 script-message aniclitr-sub-num 1",
			"2 script-message aniclitr-sub-num 2",
			"3 script-message aniclitr-sub-num 3",
			"4 script-message aniclitr-sub-num 4",
			"5 script-message aniclitr-sub-num 5",
			"6 script-message aniclitr-sub-num 6",
			"7 script-message aniclitr-sub-num 7",
			"8 script-message aniclitr-sub-num 8",
			"9 script-message aniclitr-sub-num 9",
		}, "\n")
		s.send("define-section", "aniclitr-subs", subSection, "default")
		if s.thumb != "" {
			// quitting first grabs the frame you stopped at (resume preview)
			s.send("keybind", "q", "script-message aniclitr-quit")
		}
		// Subtle discoverability cue on start (recognition over recall: no memorising keys)
		s.send("show-text", "m: hızlı menü (1080p/2K/4K · uyku · hız)   ·   y: altyazı   ·   n/b: bölüm", 3000)
		s.events <- Event{Kind: EvStarted, Speed: speed}

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
							s.events <- Event{Kind: EvSkipped, Text: sg.Label(), Pos: pos, Dur: dur, Speed: speed}
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
				case "speed":
					var sp float64
					if json.Unmarshal(m.Data, &sp) == nil && sp > 0 {
						speed = sp
					}
				case "sub-delay":
					var sd float64
					if json.Unmarshal(m.Data, &sd) == nil {
						s.subMu.Lock()
						s.subDelay = math.Round(sd*10) / 10
						s.subMu.Unlock()
					}
				case "video-params/h":
					var vh float64
					if json.Unmarshal(m.Data, &vh) == nil && vh > 0 {
						videoH = int(math.Round(vh))
					}
				case "volume":
					var vv float64
					if json.Unmarshal(m.Data, &vv) == nil {
						volume = int(math.Round(vv))
					}
				case "mute":
					_ = json.Unmarshal(m.Data, &muted)
				}
				if time.Since(lastEmit) > 400*time.Millisecond {
					lastEmit = time.Now()
					s.subMu.Lock()
					sd := s.subDelay
					s.subMu.Unlock()
					select {
					case s.events <- Event{Kind: EvProgress, Pos: pos, Dur: dur, Speed: speed, SubDelay: sd, VideoH: videoH, Volume: volume, Muted: muted, Paused: paused}:
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
					case "aniclitr-subdelay":
						if len(m.Args) > 1 {
							if m.Args[1] == "0" {
								s.ResetSubDelay()
							} else if d, err := strconv.ParseFloat(m.Args[1], 64); err == nil {
								s.AdjustSubDelay(d)
							}
							s.subMu.Lock()
							sd := s.subDelay
							s.subMu.Unlock()
							select {
							case s.events <- Event{Kind: EvProgress, Pos: pos, Dur: dur, Speed: speed, SubDelay: sd, Paused: paused}:
							default:
							}
						}
					case "aniclitr-subs", "aniclitr-sub-up", "aniclitr-sub-down", "aniclitr-sub-pick", "aniclitr-sub-close",
						"aniclitr-menu", "aniclitr-menu-up", "aniclitr-menu-down", "aniclitr-menu-left", "aniclitr-menu-right", "aniclitr-menu-pick", "aniclitr-menu-close",
						"aniclitr-anime4k", "aniclitr-sleep":
						select {
						case s.events <- Event{Kind: EvAction, Action: m.Args[0]}:
						default:
						}
					case "aniclitr-sub-num":
						if len(m.Args) > 1 {
							select {
							case s.events <- Event{Kind: EvAction, Action: "aniclitr-sub-num:" + m.Args[1]}:
							default:
							}
						}
					case "aniclitr-menu-num":
						if len(m.Args) > 1 {
							select {
							case s.events <- Event{Kind: EvAction, Action: "aniclitr-menu-num:" + m.Args[1]}:
							default:
							}
						}
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
		s.events <- Event{Kind: EvEnded, Pos: pos, Dur: dur, Speed: speed, Err: fmt.Errorf("mpv: %v", err)}
	} else {
		s.events <- Event{Kind: EvEnded, Pos: pos, Dur: dur, Speed: speed, Action: act, EOF: eof}
	}
	close(s.events)
}
