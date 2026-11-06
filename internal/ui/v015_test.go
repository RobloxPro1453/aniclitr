package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/config"
	"aniclitr/internal/download"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func TestParseEpRange(t *testing.T) {
	var eps []source.Episode
	for i := 1; i <= 100; i++ {
		eps = append(eps, source.Episode{Season: 1, Number: i})
	}

	got := parseEpRange("30-100", eps)
	if len(got) != 71 || got[0].Number != 30 || got[len(got)-1].Number != 100 {
		t.Fatalf("parseEpRange(30-100) = %d eps (first=%v), want 71 eps (30..100)", len(got), got)
	}

	got2 := parseEpRange("5-7, 12, 98-", eps)
	var nums []int
	for _, e := range got2 {
		nums = append(nums, e.Number)
	}
	want := []int{5, 6, 7, 12, 98, 99, 100}
	if len(nums) != len(want) {
		t.Fatalf("parseEpRange(5-7, 12, 98-) = %v, want %v", nums, want)
	}
	for i := range want {
		if nums[i] != want[i] {
			t.Fatalf("idx %d = %d, want %d", i, nums[i], want[i])
		}
	}
}

func TestCleanCharacterBioSpoilers(t *testing.T) {
	raw := "Normal text ~!secret spoiler!~ and [Link](https://anilist.co) **bold**."
	masked, had := cleanCharacterBio(raw, false)
	if !had || strings.Contains(masked, "secret spoiler") || !strings.Contains(masked, "Spoiler gizlendi") {
		t.Fatalf("masked bio = %q (had=%v)", masked, had)
	}
	revealed, had2 := cleanCharacterBio(raw, true)
	if !had2 || !strings.Contains(revealed, "secret spoiler") {
		t.Fatalf("revealed bio = %q (had=%v)", revealed, had2)
	}
}

func TestItemReconcileAndWatchedCount(t *testing.T) {
	it := store.Item{
		Source:  "animecix",
		AnimeID: "42",
		Title:   "Test Anime",
		Status:  store.Completed,
		Watched: 15,
		EpCount: 16,
	}
	if !it.Reconcile() {
		t.Fatal("expected Reconcile to report change for Completed 15/16 item")
	}
	if it.Watched != 16 || it.EpCount != 16 {
		t.Fatalf("after Reconcile: Watched=%d EpCount=%d, want 16/16", it.Watched, it.EpCount)
	}
}

func TestSettingsLeftRightDirection(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	app := New(config.Load(), Graphics{})
	s := newSettings()

	// Find "Tercih edilen kalite" (qualities = ["best", "1080p", "720p", "480p"])
	idx := -1
	for i, it := range s.items {
		if it.label == "Tercih edilen kalite" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("Tercih edilen kalite setting not found")
	}
	s.cur = idx
	app.cfg.Quality = "720p" // index 2 in ["best", "1080p", "720p", "480p"]

	// Press Left: should move backward to "1080p" (index 1), NOT forward to "480p"
	s.update(app, tea.KeyMsg{Type: tea.KeyLeft})
	if app.cfg.Quality != "1080p" {
		t.Fatalf("after Left from 720p: got %q, want 1080p", app.cfg.Quality)
	}

	// Press Right: should move forward back to "720p" (index 2)
	s.update(app, tea.KeyMsg{Type: tea.KeyRight})
	if app.cfg.Quality != "720p" {
		t.Fatalf("after Right from 1080p: got %q, want 720p", app.cfg.Quality)
	}
}

func TestSparklineRender(t *testing.T) {
	samples := []float64{512 * 1024, 2 * 1024 * 1024, 8 * 1024 * 1024, 16 * 1024 * 1024}
	out := ansi.Strip(sparkline(samples, 8, cAccent, cSky))
	if len([]rune(out)) != 8 {
		t.Fatalf("sparkline rune width = %d (%q), want 8", len([]rune(out)), out)
	}
}

func TestOfflineShelfSurvivesQueueClearAndScanDisk(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	dlDir := t.TempDir()
	showDir := filepath.Join(dlDir, "Re Zero")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	epFile := filepath.Join(showDir, "S01E01 [HakaiRyuu] [1080p].mp4")
	if err := os.WriteFile(epFile, []byte("fake-mp4-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. ScanDisk recovers orphaned .mp4 on startup even when downloads.json is empty
	mgr := download.Open(dlDir)
	jobs := mgr.Jobs()
	if len(jobs) != 1 || jobs[0].Season != 1 || jobs[0].Episode != 1 {
		t.Fatalf("ScanDisk jobs = %+v, want 1 recovered S01E01 job", jobs)
	}

	// 2. Removing without deleteFile (like pressing 'c' to clear finished from queue)
	// hides the job from QueueJobs() but keeps it in Jobs() for the offline shelf!
	mgr.Remove(jobs[0].ID, false)
	if len(mgr.QueueJobs()) != 0 {
		t.Fatalf("QueueJobs after clear = %d, want 0", len(mgr.QueueJobs()))
	}
	if len(mgr.Jobs()) != 1 {
		t.Fatalf("Jobs (offline shelf) after clear = %d, want 1", len(mgr.Jobs()))
	}

	// 3. RemoveAnime deletes the file from disk and cleans up the empty anime folder
	removed := mgr.RemoveAnime(jobs[0].Key())
	if removed != 1 || len(mgr.Jobs()) != 0 {
		t.Fatalf("after RemoveAnime: removed=%d, remaining=%d", removed, len(mgr.Jobs()))
	}
	if _, err := os.Stat(epFile); !os.IsNotExist(err) {
		t.Fatal("expected episode file to be deleted from disk")
	}
}
