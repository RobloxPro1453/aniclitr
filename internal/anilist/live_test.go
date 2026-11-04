package anilist

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveAniList(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	t.Setenv("ANICLITR_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	now := time.Now()
	sch, err := Schedule(ctx, now, now.AddDate(0, 0, 7))
	if err != nil || len(sch) == 0 {
		t.Fatalf("schedule: %v (%d)", err, len(sch))
	}
	t.Logf("schedule: %d episodes; first %s ep%d at %s (mal %d)", len(sch), sch[0].Media.Name(), sch[0].Episode, sch[0].At.Format("Mon 15:04"), sch[0].Media.IDMal)
	s, y := SeasonOf(now)
	ms, err := Season(ctx, s, y)
	if err != nil || len(ms) == 0 {
		t.Fatalf("season: %v", err)
	}
	t.Logf("season %s %d: %d titles, top %s (score %d)", s, y, len(ms), ms[0].Name(), ms[0].Score)
	m, recs, err := ByMal(ctx, 52991)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("frieren: %s trailer=%s recs=%d first=%s", m.Name(), m.TrailerURL(), len(recs), recs[0].Name())
	r, err := Random(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("random: %s (%d)", r.Name(), r.Score)
}

func TestLiveGenre(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("set ANICLITR_LIVE=1")
	}
	t.Setenv("ANICLITR_HOME", t.TempDir())
	ctx := context.Background()
	for _, mode := range []string{ModeTop, ModePopular, ModeCult} {
		ms, err := ByGenre(ctx, "Mystery", mode)
		if err != nil || len(ms) == 0 {
			t.Fatalf("%s: %v", mode, err)
		}
		t.Logf("%s: %s (%d, %d)", mode, ms[0].Name(), ms[0].Score, ms[0].SeasonYear)
	}
	m, err := RandomInGenre(ctx, "Sports", ModeCult)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cult sports pick: %s (%d)", m.Name(), m.SeasonYear)
}
