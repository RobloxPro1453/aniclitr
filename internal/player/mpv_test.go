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
