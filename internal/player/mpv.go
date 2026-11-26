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

// Chapter is an MKV/MP4 chapter marker reported by mpv.
type Chapter struct {
	Title string
	Start float64
}

// Event is sent to the UI.
type Event struct {
	Kind       EventKind
	Pos        float64
	Dur        float64
	Speed      float64
	SubDelay   float64
	VideoH     int
	BitrateBps int64
	Buffering  bool
	Volume     int
	Muted      bool
	Paused     bool
	Text       string // skipped segment label
	Action     string // EvEnded: "next", "prev"; EvAction: live in-mpv command
	EOF        bool   // file played to the end
	Err        error
}

// Options configure one playback.
type Options struct {
	Title           string
	Stream          *source.Stream
	Quality         source.Quality
	Start           float64
	Speed           float64
	Fullscreen      bool
	UpscaleRes      string // "", "kapalı", "otomatik", "1080p", "2k", "4k" (non-AI AMD FSR)
	Anime4K         string // "", "kapalı", "hızlı", "cam", "sinema" (Anime4K v4 CNN)
	FrameGen        string // "", "kapalı", "akıcı", "60fps" (mpv GPU smooth motion)
	AudioProfile    string // "", "referans", "gece", "kulaklık"
	SubFont         string
	SubFontSize     int
	SubPos          int
	SubBorder       float64
	SubColor        string
	InteractiveSkip bool // true: show interactive "Opening atlansın mı? [Enter]" card; false: auto-skip binge
	Segments        []aniskip.Segment
	ExtraArgs       []string
	MpvPath         string
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

	initSpeed       float64
	interactiveSkip bool
	nextLabel       string
	thumb           string
	cmd             *exec.Cmd
	conn            net.Conn
	wmu             sync.Mutex
	done            chan struct{}
	action          atomic.Value
	subMu           sync.Mutex
	subDelay        float64
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
	s := &Session{Events: ch, events: ch, cmd: cmd, done: make(chan struct{}), initSpeed: o.Speed, interactiveSkip: o.InteractiveSkip}
	s.action.Store("")
	s.nextLabel, s.thumb = o.NextLabel, o.Thumb
	go s.run(sock, o.Segments)
	return s, nil
}

func buildArgs(o Options, sock string) []string {
	isLocalStream := strings.HasPrefix(o.Quality.URL, "http://127.0.0.1:") || strings.HasPrefix(o.Quality.URL, "http://localhost:")
	args := []string{
		"--force-media-title=" + o.Title,
		"--title=" + o.Title + " — aniclitr",
		"--input-ipc-server=" + sock,
		"--force-window=immediate",
		"--no-terminal",
		"--screenshot-format=jpg", "--screenshot-jpeg-quality=72",
		"--keep-open=no",
		"--cache=yes",
		"--osd-font-size=36",
	}
	if isLocalStream {
		// Local BitTorrent HTTP Range stream: skip yt-dlp hook, give peers time
		// to connect and buffer opening pieces, and keep seeks resilient.
		args = append(args,
			"--ytdl=no",
			"--demuxer-max-bytes=128MiB",
			"--demuxer-max-back-bytes=32MiB",
			"--demuxer-readahead-secs=60",
			"--network-timeout=120",
			"--cache-pause=yes",
			"--cache-pause-wait=2",
			"--stream-lavf-o=reconnect=1,reconnect_streamed=1,reconnect_delay_max=5",
		)
	} else {
		args = append(args,
			"--hls-bitrate=max",
			"--demuxer-max-bytes=256MiB",
			"--demuxer-readahead-secs=90",
			"--network-timeout=30",
		)
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
	if pipeArgs := shaders.PipelineArgs(shaders.Pipeline{
		Res:      o.UpscaleRes,
		Enhance:  o.Anime4K,
		FrameGen: o.FrameGen,
	}); len(pipeArgs) > 0 {
		args = append(args, pipeArgs...)
	}
	if af := AudioProfileFilter(o.AudioProfile); af != "" {
		args = append(args, "--af="+af)
	}
	if o.SubFont != "" {
		args = append(args, "--sub-font="+o.SubFont)
	}
	if o.SubFontSize > 0 {
		args = append(args, fmt.Sprintf("--sub-font-size=%d", o.SubFontSize))
	}
	if o.SubPos > 0 {
		args = append(args, fmt.Sprintf("--sub-pos=%d", o.SubPos))
	}
	if o.SubBorder > 0 {
		args = append(args, fmt.Sprintf("--sub-border-size=%.1f", o.SubBorder))
	}
	if o.SubColor != "" {
		args = append(args, "--sub-color="+o.SubColor)
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

// Quit asks mpv to close gracefully, falling back to Process.Kill after 3s if stuck.
func (s *Session) Quit() {
	s.send("quit")
	done := s.done
	if done == nil {
		return
	}
	go func() {
		t := time.NewTimer(3 * time.Second)
		defer t.Stop()
		select {
		case <-done:
			return
		case <-t.C:
			if s.cmd != nil && s.cmd.Process != nil {
				_ = s.cmd.Process.Kill()
			}
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

// SetPipeline updates Resolution Upscaling (AMD FSR), Anime4K v4 CNN Quality,
// and GPU Smooth Motion (Frame-Gen) live without restarting mpv. When silent is
// true (e.g. triggered inside the open in-mpv menu), suppresses the conflicting
// top-left mpv show-text OSD so it never overlaps the menu.
func (s *Session) SetPipeline(p shaders.Pipeline, silent bool) {
	paths, err := shaders.PipelinePaths(p)
	res := shaders.NormalizeRes(p.Res)
	enh := shaders.Normalize(p.Enhance)
	fg := shaders.NormalizeFG(p.FrameGen)

	if err != nil || len(paths) == 0 {
		s.send("set_property", "glsl-shaders", []string{})
		s.send("set_property", "scale", "bilinear")
		s.send("set_property", "cscale", "bilinear")
		s.send("set_property", "sigmoid-upscaling", false)
	} else {
		s.send("set_property", "hwdec", "auto-safe")
		s.send("set_property", "deband", true)
		s.send("set_property", "deband-iterations", 1)
		s.send("set_property", "deband-threshold", 35)
		s.send("set_property", "deband-range", 16)
		s.send("set_property", "cscale", "spline36")
		s.send("set_property", "dscale", "mitchell")
		if res == shaders.Res4K || enh == shaders.Cinema {
			s.send("set_property", "scale", "ewa_lanczossharp")
			s.send("set_property", "sigmoid-upscaling", true)
		} else {
			s.send("set_property", "scale", "spline36")
			s.send("set_property", "sigmoid-upscaling", false)
		}
		s.send("set_property", "glsl-shaders", paths)
	}

	switch fg {
	case shaders.FGSmooth:
		s.send("set_property", "video-sync", "display-resample")
		s.send("set_property", "interpolation", true)
		s.send("set_property", "tscale", "oversample")
	case shaders.FG60:
		s.send("set_property", "video-sync", "display-resample")
		s.send("set_property", "interpolation", true)
		s.send("set_property", "tscale", "catmull_rom")
		s.send("set_property", "tscale-clamp", 0.0)
	default:
		s.send("set_property", "interpolation", false)
		s.send("set_property", "video-sync", "audio")
	}

	if !silent {
		s.send("show-text", "Görüntü: "+shaders.SummaryBadge(p), 2200)
	}
}

// SetShaders switches the Anime4K quality preset live (backward-compatible helper).
func (s *Session) SetShaders(profile string) {
	s.SetPipeline(shaders.Pipeline{Enhance: profile}, false)
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

// SwitchStream performs a serverless mid-playback failover directly via mpv
// IPC, loading backupURL at startSec without launching any local HTTP server
// or restarting the mpv window.
func (s *Session) SwitchStream(rawURL, referer, ua string, startSec float64) {
	if rawURL == "" {
		return
	}
	if referer != "" {
		s.send("set_property", "referrer", referer)
	}
	if ua != "" {
		s.send("set_property", "user-agent", ua)
	}
	opts := ""
	if startSec > 2 {
		opts = fmt.Sprintf("start=%.1f", startSec)
	}
	if opts != "" {
		s.send("loadfile", rawURL, "replace", -1, opts)
	} else {
		s.send("loadfile", rawURL, "replace")
	}
	s.send("show-text", "⚡ Yedek sunucuya geçildi", 2800)
}

// AudioProfileFilter returns the mpv --af filter chain for a named Audio Studio
// profile ("referans", "gece", "kulaklık").
func AudioProfileFilter(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "gece", "night":
		// Dynamic range compression + low-end tame + dialogue presence lift
		return "lavfi=[dynaudnorm=f=150:g=15:p=0.92:m=10,equalizer=f=100:t=h:w=80:g=-3,equalizer=f=2800:t=q:w=1.2:g=3]"
	case "kulaklık", "kulaklik", "bs2b", "headphone":
		// Bauer stereophonic-to-binaural crossfeed for natural headphone soundstage
		return "lavfi=[bs2b=profile=jmeier]"
	default:
		return ""
	}
}

// AudioProfileLabel returns the Turkish display label for an audio profile.
func AudioProfileLabel(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "gece", "night":
		return "Gece Modu (Diyalog Net)"
	case "kulaklık", "kulaklik", "bs2b", "headphone":
		return "Kulaklık Sahne (BS2B)"
	default:
		return "Stüdyo Referans"
	}
}

// SetAudioProfile switches the live mpv Audio Studio filter chain without restart.
func (s *Session) SetAudioProfile(profile string, silent bool) {
	af := AudioProfileFilter(profile)
	s.send("set_property", "af", af)
	if !silent {
		s.send("show-text", "Ses Stüdyosu: "+AudioProfileLabel(profile), 2400)
	}
}

// SetSubStyle updates live subtitle typography directly inside mpv.
func (s *Session) SetSubStyle(font string, size, pos int, border float64, color string, silent bool) {
	if font != "" {
		s.send("set_property", "sub-font", font)
	}
	if size > 0 {
		s.send("set_property", "sub-font-size", size)
	}
	if pos > 0 && pos <= 100 {
		s.send("set_property", "sub-pos", pos)
	}
	if border >= 0 {
		s.send("set_property", "sub-border-size", border)
	}
	if color != "" {
		s.send("set_property", "sub-color", color)
	}
	if !silent {
		s.send("show-text", fmt.Sprintf("Altyazı Stili · %d pt · konum %%%d", size, pos), 2000)
	}
}

// CaptureFrame saves the current video frame to path. When withSubs is false,
// captures a clean subtitle-free full-resolution video frame.
func (s *Session) CaptureFrame(path string, withSubs bool) {
	mode := "video"
	if withSubs {
		mode = "subtitles"
	}
	s.send("screenshot-to-file", path, mode)
}

// ShowSkipCard displays the interactive Tokyo Studio OP/ED confirmation pill
// ("Opening atlansın mı? [Enter]") without jumping abruptly.
func (s *Session) ShowSkipCard(label string) {
	s.send("enable-section", "aniclitr-skip", "default")
	ass := BuildSkipCardASS(label)
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     79,
		"format": "ass-events",
		"data":   ass,
		"res_x":  1280,
		"res_y":  720,
	})
}

// HideSkipCard hides the interactive OP/ED confirmation card and releases Enter.
func (s *Session) HideSkipCard() {
	s.send("disable-section", "aniclitr-skip")
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     79,
		"format": "none",
		"data":   "",
	})
}

// BuildSkipCardASS constructs the bottom-right interactive skip prompt ASS.
func BuildSkipCardASS(label string) string {
	if label == "" {
		label = "Jenerik"
	}
	// Clean rounded Tokyo Studio dark glass card at bottom-right (1280x720)
	x, y, w, h := 870, 598, 366, 62
	bg := fmt.Sprintf(`{\an7\pos(%d,%d)\bord1.5\shad0\1c&H1A1513&\1a&H28&\3c&HA268FF&\3a&H30&\p1}m 12 0 l %d 0 b %d 0 %d 0 %d 12 l %d %d b %d %d %d %d %d %d l 12 %d b 0 %d 0 %d 0 %d l 0 12 b 0 0 0 0 12 0{\p0}`,
		x, y, w-12, w, w, w, w, h-12, w, h, w, h, w-12, h, h, h, h, h-12)
	txt := fmt.Sprintf(`{\an4\pos(%d,%d)\fnSegoe UI\fs21\b1\bord0\shad0\1c&HFFFFFF&}%s atlansın mı?   {\1c&HA268FF&}[Enter]{\1c&H999999&\fs16}  Esc`,
		x+20, y+h/2, label)
	return bg + "\n" + txt
}

// ShowChatOverlay displays the toggleable Watch Party 2.0 ASS chat feed.
func (s *Session) ShowChatOverlay(ass string) {
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     80,
		"format": "ass-events",
		"data":   ass,
		"res_x":  1280,
		"res_y":  720,
	})
}

// HideChatOverlay clears the Watch Party 2.0 ASS chat feed.
func (s *Session) HideChatOverlay() {
	s.sendNamed(map[string]any{
		"name":   "osd-overlay",
		"id":     80,
		"format": "none",
		"data":   "",
	})
}

// ChaptersToSegments converts MKV/MP4 chapter markers (Opening, Ending, Recap,
// Preview) into skip segments so Nyaa.si torrents and local MKVs get full
// interactive skip cards even without AniSkip coverage.
func ChaptersToSegments(chapters []Chapter, dur float64) []aniskip.Segment {
	if len(chapters) == 0 {
		return nil
	}
	var out []aniskip.Segment
	for i, ch := range chapters {
		kind := classifyChapterTitle(ch.Title)
		if kind == "" {
			continue
		}
		end := dur
		if i+1 < len(chapters) && chapters[i+1].Start > ch.Start {
			end = chapters[i+1].Start
		}
		length := end - ch.Start
		if length < 15 || length > 210 {
			continue
		}
		out = append(out, aniskip.Segment{
			Kind:  kind,
			Start: ch.Start,
			End:   end,
			EpLen: dur,
		})
	}
	return out
}

func classifyChapterTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	switch {
	case t == "op" || strings.Contains(t, "opening") || strings.Contains(t, "açılış") || strings.Contains(t, "intro"):
		return "op"
	case t == "ed" || strings.Contains(t, "ending") || strings.Contains(t, "bitiş") || strings.Contains(t, "outro") || strings.Contains(t, "credits"):
		return "ed"
	case strings.Contains(t, "recap") || strings.Contains(t, "özet"):
		return "recap"
	case strings.Contains(t, "preview") || strings.Contains(t, "önizleme") || strings.Contains(t, "next episode"):
		return "preview"
	}
	return ""
}

// MergeSegments combines AniSkip segments with MKV chapter segments without
// duplicating overlapping kinds.
func MergeSegments(primary, extra []aniskip.Segment) []aniskip.Segment {
	out := append([]aniskip.Segment(nil), primary...)
	hasKind := map[string]bool{}
	for _, s := range out {
		hasKind[s.Kind] = true
	}
	for _, s := range extra {
		if !hasKind[s.Kind] {
			hasKind[s.Kind] = true
			out = append(out, s)
		}
	}
	return out
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
	go func() {
		err := s.cmd.Wait()
		if s.done != nil {
			close(s.done)
		}
		exited <- err
	}()

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
	var paused, eof, muted, buffering bool
	videoH := 0
	var bitrateBps int64
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
		s.send("observe_property", 9, "video-bitrate")
		s.send("observe_property", 10, "paused-for-cache")
		s.send("observe_property", 11, "chapter-list")
		s.send("keybind", "n", "script-message aniclitr-next")
		s.send("keybind", "N", "script-message aniclitr-next")
		s.send("keybind", "b", "script-message aniclitr-prev")
		// Shift+B / Ctrl+b saves a scene bookmark at the current timestamp
		s.send("keybind", "B", "script-message aniclitr-bookmark")
		s.send("keybind", "Ctrl+b", "script-message aniclitr-bookmark")
		// Alt+c saves a clip/GIF; Alt+f saves a clean subtitle-free 4K frame; Alt+h toggles Watch Party chat
		s.send("keybind", "Alt+c", "script-message aniclitr-clip")
		s.send("keybind", "Alt+f", "script-message aniclitr-frame4k")
		s.send("keybind", "Alt+h", "script-message aniclitr-chat-toggle")
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
		skipSection := strings.Join([]string{
			"ENTER script-message aniclitr-skip-now",
			"KP_ENTER script-message aniclitr-skip-now",
			"ESC script-message aniclitr-skip-dismiss",
		}, "\n")
		s.send("define-section", "aniclitr-skip", skipSection, "default")
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
			"ESC script-message aniclitr-menu-back",
			"BS script-message aniclitr-menu-back",
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
			"BS script-message aniclitr-sub-close",
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
		var mkvChaps []Chapter
		skipped := map[string]bool{}
		dismissed := map[string]bool{}
		var activePrompt *aniskip.Segment
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
					var inSeg *aniskip.Segment
					for i := range segs {
						sg := &segs[i]
						if pos >= sg.Start && pos < sg.End-1.5 {
							inSeg = sg
							break
						}
					}
					if inSeg != nil {
						lbl := inSeg.Label()
						if s.interactiveSkip {
							if !skipped[lbl] && !dismissed[lbl] && (activePrompt == nil || activePrompt.Label() != lbl) {
								cp := *inSeg
								activePrompt = &cp
								s.ShowSkipCard(lbl)
							}
						} else if !skipped[lbl] {
							skipped[lbl] = true
							s.send("seek", inSeg.End, "absolute")
							s.send("show-text", "» "+lbl+" atlandı", 1800)
							s.events <- Event{Kind: EvSkipped, Text: lbl, Pos: pos, Dur: dur, Speed: speed}
						}
					} else if activePrompt != nil {
						activePrompt = nil
						s.HideSkipCard()
					}
				case "duration":
					_ = json.Unmarshal(m.Data, &dur)
					if segs == nil && dur > 0 {
						segs = MergeSegments(aniskip.ForDuration(all, dur), ChaptersToSegments(mkvChaps, dur))
						for _, sg := range segs {
							if sg.Kind == "ed" || sg.Kind == "mixed-ed" {
								endAt = sg.Start
							}
						}
						if endAt == 0 && dur > 600 {
							endAt = dur - 90
						}
					}
				case "chapter-list":
					var rawCh []struct {
						Title string  `json:"title"`
						Time  float64 `json:"time"`
					}
					if json.Unmarshal(m.Data, &rawCh) == nil && len(rawCh) > 0 {
						mkvChaps = make([]Chapter, 0, len(rawCh))
						for _, rc := range rawCh {
							mkvChaps = append(mkvChaps, Chapter{Title: rc.Title, Start: rc.Time})
						}
						if dur > 0 {
							segs = MergeSegments(segs, ChaptersToSegments(mkvChaps, dur))
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
				case "video-bitrate":
					var br float64
					if json.Unmarshal(m.Data, &br) == nil && br > 0 {
						bitrateBps = int64(math.Round(br))
					}
				case "paused-for-cache":
					_ = json.Unmarshal(m.Data, &buffering)
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
					case s.events <- Event{Kind: EvProgress, Pos: pos, Dur: dur, Speed: speed, SubDelay: sd, VideoH: videoH, BitrateBps: bitrateBps, Buffering: buffering, Volume: volume, Muted: muted, Paused: paused}:
					default: // UI busy; drop a frame rather than block mpv
					}
				}
			case "client-message":
				if len(m.Args) > 0 {
					switch m.Args[0] {
					case "aniclitr-skip-now":
						if activePrompt != nil {
							sg := *activePrompt
							activePrompt = nil
							skipped[sg.Label()] = true
							s.HideSkipCard()
							s.send("seek", sg.End, "absolute")
							s.send("show-text", "» "+sg.Label()+" atlandı", 1800)
							s.events <- Event{Kind: EvSkipped, Text: sg.Label(), Pos: pos, Dur: dur, Speed: speed}
						}
					case "aniclitr-skip-dismiss":
						if activePrompt != nil {
							dismissed[activePrompt.Label()] = true
							activePrompt = nil
							s.HideSkipCard()
						}
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
						"aniclitr-menu", "aniclitr-menu-up", "aniclitr-menu-down", "aniclitr-menu-left", "aniclitr-menu-right", "aniclitr-menu-pick", "aniclitr-menu-back", "aniclitr-menu-close",
						"aniclitr-anime4k", "aniclitr-sleep",
						"aniclitr-bookmark", "aniclitr-clip", "aniclitr-frame4k", "aniclitr-chat-toggle":
						select {
						case s.events <- Event{Kind: EvAction, Action: m.Args[0], Pos: pos, Dur: dur}:
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
	s.wmu.Lock()
	c := s.conn
	s.conn = nil
	s.wmu.Unlock()
	if c != nil {
		c.Close()
	}
	act, _ := s.action.Load().(string)
	if (err != nil || (pos == 0 && dur == 0 && !eof)) && act == "" {
		if err == nil {
			err = errors.New("akış açılamadı veya zaman aşımına uğradı")
		}
		logx.Errorf("mpv kapandı (pos=%.1f dur=%.1f): %v", pos, dur, err)
		s.events <- Event{Kind: EvEnded, Pos: pos, Dur: dur, Speed: speed, Err: fmt.Errorf("mpv: %v", err)}
	} else {
		s.events <- Event{Kind: EvEnded, Pos: pos, Dur: dur, Speed: speed, Action: act, EOF: eof}
	}
	close(s.events)
}
