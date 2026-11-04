package download

import (
	"context"
	"os"
	"testing"
	"time"

	"aniclitr/internal/source"
	_ "aniclitr/internal/source/animecix"
)

// A real AnimeciX episode: resolve → aria2c starts → progress is parsed →
// cancel stops it. Gated: needs the network.
func TestLiveDownloadProgress(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	t.Setenv("ANICLITR_HOME", t.TempDir())
	dir := t.TempDir()
	m := Open(dir)
	p := source.Get("animecix")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) == 0 {
		t.Fatal(err)
	}
	an, err := p.Details(ctx, res[0])
	if err != nil {
		t.Fatal(err)
	}
	eps, err := p.Episodes(ctx, an, 1)
	if err != nil || len(eps) == 0 {
		t.Fatal(err)
	}
	j, _ := m.Add(Request{Anime: an, Ep: eps[0], PrefQual: "480p"})
	deadline := time.Now().Add(60 * time.Second)
	var last Job
	for time.Now().Before(deadline) {
		for _, x := range m.Jobs() {
			if x.ID == j.ID {
				last = x
			}
		}
		if last.State == Failed {
			t.Fatalf("failed: %s", last.Err)
		}
		if last.State == Running && last.Done > 0 && last.Total > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("state=%s tool=%s %d/%d bytes speed=%.0f B/s eta=%v fansub=%q quality=%s path=%s",
		last.State, last.Tool, last.Done, last.Total, last.Speed, last.ETA, last.Fansub, last.Quality, last.Path)
	if last.Done <= 0 || last.Total <= 0 {
		t.Fatal("no progress parsed")
	}
	m.Cancel(j.ID)
	time.Sleep(1500 * time.Millisecond)
	for _, x := range m.Jobs() {
		if x.ID == j.ID && x.State != Canceled {
			t.Fatalf("cancel did not stick: %s", x.State)
		}
	}
	m.Remove(j.ID, true)
}
