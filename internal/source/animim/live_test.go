package animim

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveChain walks search → details → episodes → variants → resolve on
// the real site (ANICLITR_LIVE=1).
func TestLiveChain(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("ANICLITR_LIVE=1 ile çalışır")
	}
	t.Setenv("ANICLITR_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := Provider{}
	res, err := p.Search(ctx, "frieren")
	if err != nil || len(res) == 0 {
		t.Fatalf("search: %v", err)
	}
	d, err := p.Details(ctx, res[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s · mal %d · sezonlar %+v", d.Title, d.MalID, d.Seasons)
	if d.MalID != 52991 {
		t.Errorf("mal id %d", d.MalID)
	}
	eps, err := p.Episodes(ctx, d, 1)
	if err != nil || len(eps) < 28 {
		t.Fatalf("episodes: %v %d", err, len(eps))
	}
	vs, err := p.Variants(ctx, d, eps[0])
	if err != nil || len(vs) == 0 {
		t.Fatalf("variants: %v", err)
	}
	st, err := p.Resolve(ctx, vs[0])
	if err != nil {
		t.Fatalf("resolve %+v: %v", vs[0], err)
	}
	for _, q := range st.Qualities {
		t.Logf("%-12s %s probed=%v broken=%v", vs[0].Fansub, q.DisplayLabel(), q.Probed, q.Broken)
	}
	for _, s := range st.Subtitles {
		t.Logf("altyazı: %s", s.Label)
	}
	if st.Qualities[0].Broken {
		t.Fatal("best quality marked broken")
	}
}
