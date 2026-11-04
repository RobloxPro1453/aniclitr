package ui

import (
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/download"
	"aniclitr/internal/source"
)

// No internet at start → only the offline shelf; covers saved next to the
// videos are loaded; the line coming back restores the normal home.
func TestOfflineMode(t *testing.T) {
	a := showcaseApp(t, 140, 40)
	vdir := filepath.Join(t.TempDir(), "Youjo Senki")
	os.MkdirAll(vdir, 0o755)
	vf := filepath.Join(vdir, "S01E13.mp4")
	os.WriteFile(vf, []byte("x"), 0o644)
	// a real JPEG cover (copied from a showcase poster encoded as jpeg is overkill: reuse any image file)
	src := a.posters.items["test://p0"].img
	writeJPEG(t, filepath.Join(vdir, "kapak.jpg"), src)
	a.dl.Preview(download.Job{ID: 5, Source: "animecix", AnimeID: "9", Title: "Youjo Senki", Poster: "https://example.invalid/x.jpg", Season: 1, Episode: 13, State: download.Done, Path: vf, Total: 300 << 20, Finished: time.Now()})

	a.Update(netMsg{online: false})
	if !a.offline {
		t.Fatal("should be offline")
	}
	if _, ok := a.top().(*offlineScreen); !ok || len(a.stack) != 1 {
		t.Fatalf("offline screen should be the only screen, got %T (%d)", a.top(), len(a.stack))
	}
	cover := "file://" + filepath.Join(vdir, "kapak.jpg")
	if st, ok := a.posters.items[cover]; !ok || !st.loading {
		t.Fatal("going offline should start loading the saved cover")
	}
	delete(a.posters.items, cover)    // the test drops the batch; run just the cover fetch
	drain(t, a, a.offlineCovers(), 0) // (not the 15 s re-probe, which would hit the network)
	if !a.posters.ready(cover) {
		t.Fatal("the saved cover was not loaded")
	}
	v := ansiRe.ReplaceAllString(a.View(), "")
	if !strings.Contains(v, "ÇEVRİMDIŞI") || strings.Contains(v, "anime ara") {
		t.Fatal("offline screen should show the banner and no search box")
	}
	if strings.Contains(v, "kapak yok") {
		t.Fatal("cover still missing on the shelf")
	}
	// esc on the root does nothing; ctrl+e is refused
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if _, ok := a.top().(*offlineScreen); !ok {
		t.Fatal("offline root must stay")
	}
	// the line returns
	a.Update(netMsg{online: true})
	if a.offline {
		t.Fatal("should be back online")
	}
	if _, ok := a.top().(*home); !ok {
		t.Fatalf("home should be back, got %T", a.top())
	}
}

// d opens the fansub picker in download mode; enter queues that fansub, strictly.
func TestDownloadPicksFansub(t *testing.T) {
	a := showcaseApp(t, 140, 40)
	an := source.Anime{Source: "animecix", ID: "100", Title: "Sousou no Frieren", Poster: "test://p0"}
	a.details[an.Key()] = an
	sc := newAnime(an, nil, 0, 0)
	a.stack = append(a.stack, sc)
	sc.ensure(a)
	eps := []source.Episode{{ID: "1", Season: 1, Number: 1}, {ID: "2", Season: 1, Number: 2}}
	a.episodes[epKey(sc.an, 1)] = eps
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	pk, ok := a.top().(*picker)
	if !ok || pk.dlMode != 1 {
		t.Fatalf("d should open the picker in download mode, got %T", a.top())
	}
	pk.build(a, []source.Variant{{Fansub: "HakaiRyuu & Lexellures", Host: "Tau", URL: "u1"}, {Fansub: "AnimeciX Fansub", Host: "Tau", URL: "u2"}})
	pk.fCur = 1
	a.dl = download.Open(t.TempDir()) // a manager that will not reach the network meaningfully
	a.Update(key("enter"))
	var j download.Job
	for _, x := range a.dl.Jobs() {
		j = x
	}
	if !j.Strict || j.Want != "AnimeciX Fansub" {
		t.Fatalf("job should be locked to the chosen fansub: %+v", j)
	}
	if _, ok := a.top().(*animeScreen); !ok {
		t.Fatal("picker should close after queueing")
	}
	a.dl.Cancel(j.ID)
}

func writeJPEG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}
