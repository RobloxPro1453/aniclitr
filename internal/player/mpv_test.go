package player

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"aniclitr/internal/aniskip"
	"aniclitr/internal/source"
	"aniclitr/internal/source/animecix"
)

func TestBuildArgsPreservesSpeed(t *testing.T) {
	st := &source.Stream{Referer: "https://example.com/"}
	q := source.Quality{Label: "1080p", URL: "https://cdn.example.com/ep1.mp4", Height: 1080}

	argsDefault := strings.Join(buildArgs(Options{Title: "Ep 1", Stream: st, Quality: q}, "sock1"), " ")
	if strings.Contains(argsDefault, "--speed=") {
		t.Fatalf("default speed should not pass --speed flag, got: %s", argsDefault)
	}

	argsOne := strings.Join(buildArgs(Options{Title: "Ep 1", Stream: st, Quality: q, Speed: 1.0}, "sock1"), " ")
	if strings.Contains(argsOne, "--speed=") {
		t.Fatalf("speed=1.0 should not pass --speed flag, got: %s", argsOne)
	}

	argsFast := strings.Join(buildArgs(Options{Title: "Ep 2", Stream: st, Quality: q, Speed: 1.5}, "sock2"), " ")
	if !strings.Contains(argsFast, "--speed=1.50") {
		t.Fatalf("expected --speed=1.50 in mpv args, got: %s", argsFast)
	}
}

// TestLivePlayback streams a real episode headless and checks the IPC event flow.
func TestLivePlayback(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := animecix.Provider{}
	an, err := p.Details(ctx, source.Anime{Source: "animecix", ID: "11130"})
	if err != nil {
		t.Fatal(err)
	}
	eps, err := p.Episodes(ctx, an, 1)
	if err != nil {
		t.Fatal(err)
	}
	vs, err := p.Variants(ctx, an, eps[0])
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.Resolve(ctx, vs[0])
	if err != nil {
		t.Fatal(err)
	}
	segs, err := aniskip.Fetch(ctx, an.MalID, 1)
	t.Logf("aniskip: %v err=%v", segs, err)

	q := st.Qualities[len(st.Qualities)-1] // smallest: faster
	s, err := Start(Options{
		Title: "aniclitr test", Stream: st, Quality: q, Segments: segs,
		ExtraArgs: []string{"--vo=null", "--ao=null", "--fs=no"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var started, progressed bool
	var askedNext bool
	timeout := time.After(45 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events:
			if !ok {
				if !started || !progressed {
					t.Fatalf("stream ended early: started=%v progressed=%v", started, progressed)
				}
				return
			}
			switch ev.Kind {
			case EvStarted:
				started = true
				t.Log("IPC connected")
			case EvProgress:
				if ev.Pos > 3 && !askedNext {
					progressed = true
					t.Logf("progress %.1f / %.1f", ev.Pos, ev.Dur)
					askedNext = true
					s.Seek(30)
					time.AfterFunc(1500*time.Millisecond, s.Next)
				}
			case EvSkipped:
				t.Logf("skipped %s", ev.Text)
			case EvEnded:
				t.Logf("ended pos=%.1f dur=%.1f action=%q eof=%v err=%v", ev.Pos, ev.Dur, ev.Action, ev.EOF, ev.Err)
				if ev.Action != "next" {
					t.Errorf("want action next, got %q", ev.Action)
				}
				if ev.Pos < 30 {
					t.Errorf("seek did not apply: pos=%.1f", ev.Pos)
				}
			}
		case <-timeout:
			s.Quit()
			t.Fatal("timeout waiting for playback events")
		}
	}
}

func TestBuildArgsHLSBandwidth(t *testing.T) {
	st := &source.Stream{}
	plain := buildArgs(Options{Title: "x", Stream: st, Quality: source.Quality{URL: "https://h/v720.m3u8"}}, "s")
	if last := lastWith(plain, "--hls-bitrate="); last != "--hls-bitrate=max" {
		t.Fatalf("default: got %q", last)
	}
	master := buildArgs(Options{Title: "x", Stream: st, Quality: source.Quality{URL: "https://h/master.m3u8", HLSBandwidth: 1712112}}, "s")
	// mpv honours the last occurrence, so the chosen rendition must come after "max"
	if last := lastWith(master, "--hls-bitrate="); last != "--hls-bitrate=1712112" {
		t.Fatalf("master: got %q", last)
	}
	if master[len(master)-1] != "https://h/master.m3u8" {
		t.Fatalf("URL must stay last: %v", master)
	}
}

func lastWith(args []string, prefix string) string {
	out := ""
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			out = a
		}
	}
	return out
}

func TestV2PlayerHelpers(t *testing.T) {
	// MKV chapters -> skip segments
	chaps := []Chapter{
		{Title: "Prologue", Start: 0},
		{Title: "Opening", Start: 95},
		{Title: "Part A", Start: 185},
		{Title: "Ending", Start: 1320},
		{Title: "Next Episode Preview", Start: 1410},
	}
	segs := ChaptersToSegments(chaps, 1440)
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments (OP/ED/Preview), got %d: %+v", len(segs), segs)
	}
	if segs[0].Kind != "op" || segs[1].Kind != "ed" || segs[2].Kind != "preview" {
		t.Fatalf("unexpected chapter kinds: %+v", segs)
	}
	merged := MergeSegments([]aniskip.Segment{{Kind: "op", Start: 90, End: 180}}, segs)
	if len(merged) != 3 {
		t.Fatalf("MergeSegments should keep primary OP and add ED+Preview, got %d", len(merged))
	}

	// Audio Studio profiles
	if AudioProfileFilter("referans") != "" {
		t.Fatalf("referans must clear audio filters")
	}
	if !strings.Contains(AudioProfileFilter("gece"), "dynaudnorm") {
		t.Fatalf("gece profile missing dynaudnorm")
	}
	if !strings.Contains(AudioProfileFilter("kulaklık"), "bs2b") {
		t.Fatalf("kulaklık profile missing bs2b")
	}

	// Interactive skip card ASS
	ass := BuildSkipCardASS("Açılış")
	if !strings.Contains(ass, "Açılış atlansın mı?") || !strings.Contains(ass, "[Enter]") {
		t.Fatalf("skip card ASS malformed: %s", ass)
	}

	// Audio + subtitle typography flags
	args := strings.Join(buildArgs(Options{
		Title:        "Test",
		Quality:      source.Quality{URL: "https://cdn/1.mp4"},
		AudioProfile: "gece",
		SubFont:      "Segoe UI",
		SubFontSize:  52,
		SubPos:       94,
		SubBorder:    2.5,
		SubColor:     "#FFF8E0",
	}, "s"), " ")
	for _, want := range []string{"--af=lavfi=[dynaudnorm", "--sub-font=Segoe UI", "--sub-font-size=52", "--sub-pos=94", "--sub-border-size=2.5", "--sub-color=#FFF8E0"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing arg %q in %s", want, args)
		}
	}
}
