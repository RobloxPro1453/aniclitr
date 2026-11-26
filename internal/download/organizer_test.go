package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseReleaseVariants(t *testing.T) {
	cases := []struct {
		in     string
		series string
		season int
		ep     int
		group  string
		res    string
	}{
		{
			in:     "ReZero.kara.Hajimeru.Isekai.Seikatsu.S04E18.1080p.AMZN.WEB-DL.DDP2.0.H.264-ToonsHub.mkv",
			series: "ReZero kara Hajimeru Isekai Seikatsu",
			season: 4, ep: 18, group: "ToonsHub", res: "1080p",
		},
		{
			in:     "[SubsPlease] Sousou no Frieren - 12 (1080p) [ABCDEF12].mkv",
			series: "Sousou no Frieren",
			season: 1, ep: 12, group: "SubsPlease", res: "1080p",
		},
		{
			in:     "[Shiniori-Raws] Another (BD 1920x1080 x265 10bit FLAC)",
			series: "Another",
			season: 1, ep: 0, group: "Shiniori-Raws", res: "1080p",
		},
		{
			in:     "[ToonsHub] ReZero kara Hajimeru Isekai Seikatsu S04E19 1080p AMZN WEB-DL.mkv",
			series: "ReZero kara Hajimeru Isekai Seikatsu",
			season: 4, ep: 19, group: "ToonsHub", res: "1080p",
		},
	}
	for _, tc := range cases {
		p := ParseRelease(tc.in)
		if p.Series != tc.series || p.Season != tc.season || p.Episode != tc.ep || p.Group != tc.group || p.Resolution != tc.res {
			t.Errorf("ParseRelease(%q) = %+v, want series=%q S%dE%d group=%q res=%q",
				tc.in, p, tc.series, tc.season, tc.ep, tc.group, tc.res)
		}
	}
}

func TestMatchSeriesFolderMergesReZeroIntoExistingFolder(t *testing.T) {
	existing := []string{"Another", "Oshi no Ko", "Re Zero kara Hajimeru Isekai Seikatsu"}
	got := MatchSeriesFolder("ReZero kara Hajimeru Isekai Seikatsu", existing, nil)
	if got != "Re Zero kara Hajimeru Isekai Seikatsu" {
		t.Fatalf("expected match to existing Re Zero folder, got %q", got)
	}
}

func TestOrganizeLooseFilesMovesAndIndexesMkv(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	root := t.TempDir()
	existingDir := filepath.Join(root, "Re Zero kara Hajimeru Isekai Seikatsu")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(root, "ReZero.kara.Hajimeru.Isekai.Seikatsu.S04E18.1080p.AMZN.WEB-DL-ToonsHub.mkv")
	if err := os.WriteFile(loose, []byte("fake-mkv-video-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := Open(root)
	moved := m.OrganizeLooseFiles(nil)
	if moved != 1 {
		t.Fatalf("expected 1 loose file organized, got %d", moved)
	}
	want := filepath.Join(existingDir, "S04E18 [ToonsHub] [1080p].mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected organized file at %s: %v", want, err)
	}
	if _, err := os.Stat(loose); !os.IsNotExist(err) {
		t.Fatalf("loose source file should have been moved")
	}
	jobs := m.Jobs()
	if len(jobs) != 1 || jobs[0].Season != 4 || jobs[0].Episode != 18 || jobs[0].Title != "Re Zero kara Hajimeru Isekai Seikatsu" {
		t.Fatalf("expected organized mkv to be indexed in offline shelf, got %+v", jobs)
	}
}
