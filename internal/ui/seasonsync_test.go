package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aniclitr/internal/anilist"
	"aniclitr/internal/mal"
	"aniclitr/internal/seasons"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// bleach is AnimeciX's Bleach as seen on 2026-11-07: one title, two seasons
// (362 + 50). MAL has five entries: Bleach (366) and Sennen Kessen-hen in
// four parts (13, 13, 14, 10). The chain is the real AniList one.
func bleach() (source.Anime, seasons.Chain) {
	an := source.Anime{Source: "animecix", ID: "82", Title: "Bleach", Kind: "TV", MalID: 269, EpCount: 412,
		Seasons: []source.Season{{Number: 1, EpisodeCount: 362}, {Number: 2, EpisodeCount: 50}}}
	ch := seasons.Chain{Entries: []seasons.Entry{
		{Season: 1, MalID: 269, Episodes: 366, Format: "TV", Title: "Bleach"},
		{Season: 2, MalID: 41467, Episodes: 13, Format: "TV", Title: "Bleach: Sennen Kessen-hen"},
		{Season: 3, MalID: 53998, Episodes: 13, Format: "TV", Title: "Bleach: Sennen Kessen-hen - Ketsubetsu-tan"},
		{Season: 4, MalID: 56784, Episodes: 14, Format: "TV", Title: "Bleach: Sennen Kessen-hen - Soukoku-tan"},
		{Season: 5, MalID: 60636, Episodes: 10, Format: "TV", Title: "Bleach: Sennen Kessen-hen - Kashin-tan"},
	}}
	return an, ch
}

func withChain(a *App, an source.Anime, ch seasons.Chain) {
	if a.aligns == nil {
		a.aligns = map[string]*alignState{}
	}
	a.aligns[an.Key()] = &alignState{malID: an.MalID, chain: ch, ok: true}
}

func connectMal(a *App) {
	a.mal.ClientID = "test"
	// a valid token (no refresh); no syncer loop runs in tests, so nothing
	// reaches MAL unless a test pushes to its own fake server
	a.mal.SetToken(mal.Token{Access: "t", Refresh: "r", Expiry: time.Now().Add(time.Hour)})
}

func ep(s, n int) source.Episode { return source.Episode{Season: s, Number: n} }

func libByMal(t *testing.T, a *App, id int) store.Item {
	t.Helper()
	it, ok := a.lib.FindMal(id)
	if !ok {
		t.Fatalf("no library item for MAL %d", id)
	}
	return it
}

// Not asked yet (or MAL not connected): exactly the old rule, the title's
// own entry and only its first season.
func TestSeasonSyncOffKeepsOldRule(t *testing.T) {
	a := testApp(t, Graphics{})
	an, ch := bleach()
	withChain(a, an, ch)

	bumpWatched(a, an, ep(2, 5))
	if _, ok := a.lib.FindMal(41467); ok {
		t.Fatal("wrote to another MAL entry without being allowed")
	}
	bumpWatched(a, an, ep(1, 7))
	if it := libByMal(t, a, 269); it.Watched != 7 {
		t.Fatalf("own entry: watched %d, want 7", it.Watched)
	}
	// the site's 412 episodes never reach the 366-episode MAL entry
	if it := libByMal(t, a, 269); it.EpCount != 366 {
		t.Fatalf("own entry count %d, want the MAL entry's 366", it.EpCount)
	}
}

func TestSeasonSyncAskBackfillAndWrite(t *testing.T) {
	a := testApp(t, Graphics{})
	an, ch := bleach()
	withChain(a, an, ch)
	connectMal(a)

	// history: all of part 1 (S2E1-13) and two episodes of part 2
	a.hist.Update(an, func(e *store.Entry) {
		if e.Watched == nil {
			e.Watched = map[string]bool{}
		}
		for n := 1; n <= 15; n++ {
			e.Watched[store.EpKey(2, n)] = true
		}
	})

	a.maybeAskSeasonSync(an)
	if a.drop == nil {
		t.Fatal("no question was asked")
	}
	var yes *dropItem
	for _, it := range a.drop.items(a) {
		it := it
		if strings.Contains(it.label, "kendi MAL kaydına") {
			yes = &it
		}
	}
	if yes == nil {
		t.Fatal("no 'yes' answer offered")
	}
	a.drop = nil
	yes.run(a)
	if !a.cfg.SeasonSync[an.Key()] {
		t.Fatal("answer not stored")
	}

	if it := libByMal(t, a, 41467); it.Status != store.Completed || it.Watched != 13 {
		t.Fatalf("part 1 after backfill: %s %d, want completed 13", it.Status, it.Watched)
	}
	if it := libByMal(t, a, 53998); it.Status != store.Watching || it.Watched != 2 || !it.MalDirty {
		t.Fatalf("part 2 after backfill: %+v", it)
	}

	// asked once only
	a.maybeAskSeasonSync(an)
	if a.drop != nil {
		t.Fatal("asked twice")
	}

	// S2E30 is part 3 (starts at site episode 27), episode 4
	bumpWatched(a, an, ep(2, 30))
	if it := libByMal(t, a, 56784); it.Watched != 4 || it.Status != store.Watching || it.EpCount != 14 {
		t.Fatalf("part 3: %+v", it)
	}
	if !strings.Contains(libByMal(t, a, 56784).Title, "Soukoku") {
		t.Fatalf("new entry should carry the chain's title, got %q", libByMal(t, a, 56784).Title)
	}

	// the reading side agrees
	wv := a.watchView(an)
	for _, c := range []struct {
		e    source.Episode
		done bool
	}{
		{ep(2, 9), true},   // part 1 completed on the list
		{ep(2, 15), true},  // part 2 ep 2
		{ep(2, 16), false}, // part 2 ep 3
		{ep(2, 30), true},  // part 3 ep 4 (just written)
		{ep(2, 31), false},
		{ep(1, 1), false}, // nothing of season 1 yet
	} {
		if got := wv.done(c.e); got != c.done {
			t.Errorf("done(S%dE%d) = %v, want %v", c.e.Season, c.e.Number, got, c.done)
		}
	}
}

// An entry already on the shelf under a real source (e.g. AnimeTR's own
// page for that part) is advanced in place, never duplicated as "mal:<id>".
func TestSeasonSyncUsesExistingItem(t *testing.T) {
	a := testApp(t, Graphics{})
	an, ch := bleach()
	withChain(a, an, ch)
	connectMal(a)
	a.cfg.SeasonSync = map[string]bool{an.Key(): true}

	part1 := source.Anime{Source: "animetr", ID: "bleach-sennen-kessen-hen", Title: "Bleach TYBW", Kind: "TV", MalID: 41467}
	a.lib.Set(part1, func(x *store.Item) { x.Status = store.Planned })

	bumpWatched(a, an, ep(2, 3))
	if _, dup := a.lib.Get("mal:41467"); dup {
		t.Fatal("duplicated the existing item as mal:41467")
	}
	if it, _ := a.lib.Get(part1.Key()); it.Watched != 3 || it.Status != store.Watching {
		t.Fatalf("existing item not advanced: %+v", it)
	}
}

// Where the site has more episodes than the MAL entry, the alignment is
// unsure: nothing is written rather than a wrong count.
func TestCanonicalRefusesOverflowAndUnknownSeason(t *testing.T) {
	ch := seasons.Chain{Entries: []seasons.Entry{
		{Season: 1, MalID: 1, Episodes: 12, Format: "TV"},
		{Season: 2, MalID: 2, Episodes: 12, Format: "TV"},
	}}
	al := seasons.AlignTitle("X", "TV", map[int]int{1: 12, 2: 14}, ch)
	if _, _, ok := canonical(al, ep(2, 12)); !ok {
		t.Fatal("S2E12 should map")
	}
	if c, _, ok := canonical(al, ep(2, 14)); ok {
		t.Fatalf("S2E14 overflows a 12-episode entry and must not map, got %+v", c)
	}

	// one TV entry, but the site lists a season 2: season 2 maps to nothing
	single := seasons.Chain{Entries: []seasons.Entry{{Season: 1, MalID: 7, Episodes: 24, Format: "TV"}}}
	al = seasons.AlignTitle("Y", "TV", map[int]int{1: 24, 2: 10}, single)
	if c, _, ok := canonical(al, ep(2, 3)); ok {
		t.Fatalf("an unknown site season must not map onto the only entry, got %+v", c)
	}
}

func TestUserMalOverridesDetails(t *testing.T) {
	a := testApp(t, Graphics{})
	an, _ := bleach()
	if got := userMal(an).MalID; got != 269 {
		t.Fatalf("no mapping: got %d", got)
	}
	a.setUserMal(an.Key(), animeMedia(41467, "Bleach: Sennen Kessen-hen"))
	if got := userMal(an).MalID; got != 41467 {
		t.Fatalf("user mapping not applied: got %d", got)
	}
}

func animeMedia(malID int, title string) anilist.Media {
	return anilist.Media{ID: malID + 100000, IDMal: malID, Title: anilist.Titles{Romaji: title}}
}

// patches records what reached the fake MAL: id → form of the last PATCH.
type patches struct {
	mu sync.Mutex
	m  map[int]map[string]string
}

func (p *patches) handler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/my_list_status") || r.Method != http.MethodPatch {
		http.NotFound(w, r)
		return
	}
	id, _ := strconv.Atoi(strings.Split(r.URL.Path, "/")[3])
	_ = r.ParseForm()
	p.mu.Lock()
	defer p.mu.Unlock()
	form := map[string]string{}
	for k := range r.Form {
		form[k] = r.Form.Get(k)
	}
	p.m[id] = form
	_, _ = w.Write([]byte("{}"))
}

func (p *patches) want(t *testing.T, id int, status string, watched int) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	f, ok := p.m[id]
	if !ok {
		t.Errorf("MAL %d: nothing sent", id)
		return
	}
	if f["status"] != status || f["num_watched_episodes"] != strconv.Itoa(watched) {
		t.Errorf("MAL %d: got %s %s, want %s %d", id, f["status"], f["num_watched_episodes"], status, watched)
	}
}

// Acceptance (roadmap stage 2): Bleach, One Piece and Frieren, from a watched
// episode on the site to what the MAL API receives.
func TestSeasonSyncReachesMAL(t *testing.T) {
	p := &patches{m: map[int]map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(p.handler))
	defer srv.Close()
	mal.SetEndpoints(srv.URL+"/a", srv.URL+"/t", srv.URL+"/v2")

	a := testApp(t, Graphics{})
	connectMal(a)

	// Bleach: allowed to write the other entries
	bl, blCh := bleach()
	withChain(a, bl, blCh)
	a.cfg.SeasonSync = map[string]bool{bl.Key(): true}
	bumpWatched(a, bl, ep(1, 10))
	bumpWatched(a, bl, ep(2, 5))
	// finishing the title's own entry: MAL must get 366, never the site's 412
	a.libSet(bl, func(x *store.Item) { x.Status = store.Completed })

	// One Piece: one long entry still airing (count unknown), absolute numbering
	op := source.Anime{Source: "animecix", ID: "66", Title: "One Piece", Kind: "TV", MalID: 21,
		Seasons: []source.Season{{Number: 1, EpisodeCount: 1180}}}
	withChain(a, op, seasons.Chain{Entries: []seasons.Entry{{Season: 1, MalID: 21, Format: "TV", Title: "One Piece"}}})
	bumpWatched(a, op, ep(1, 1100))

	// Frieren: season 2 is its own entry; this title was never asked about,
	// so only season 1 may be written
	fr := source.Anime{Source: "animecix", ID: "9001", Title: "Sousou no Frieren", Kind: "TV", MalID: 52991,
		Seasons: []source.Season{{Number: 1, EpisodeCount: 28}, {Number: 2, EpisodeCount: 4}}}
	withChain(a, fr, seasons.Chain{Entries: []seasons.Entry{
		{Season: 1, MalID: 52991, Episodes: 28, Format: "TV"},
		{Season: 2, MalID: 59978, Episodes: 10, Format: "TV"},
	}})
	// through the real end-of-episode path, as mpv reaching 75 % does
	var s1 []source.Episode
	for n := 1; n <= 28; n++ {
		s1 = append(s1, ep(1, n))
	}
	newPlaying(fr, ep(1, 28), s1, "", "", &source.Stream{}, source.Quality{}).libraryProgress(a)
	newPlaying(fr, ep(2, 3), []source.Episode{ep(2, 1), ep(2, 2), ep(2, 3), ep(2, 4)}, "", "", &source.Stream{}, source.Quality{}).libraryProgress(a)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.msync.Push(ctx); err != nil {
		t.Fatal(err)
	}
	p.want(t, 269, "completed", 366)
	p.want(t, 41467, "watching", 5)
	p.want(t, 21, "watching", 1100)
	p.want(t, 52991, "watching", 28)
	p.mu.Lock()
	_, leaked := p.m[59978]
	p.mu.Unlock()
	if leaked {
		t.Error("Frieren S2 reached MAL without the user agreeing")
	}
}

// TestLiveSeasonSyncBleach runs the whole chain against the real sites:
// AnimeciX details, AniList relations, alignment (ANICLITR_LIVE=1).
func TestLiveSeasonSyncBleach(t *testing.T) {
	if os.Getenv("ANICLITR_LIVE") == "" {
		t.Skip("ANICLITR_LIVE=1 ile çalışır")
	}
	a := testApp(t, Graphics{})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := source.Get("animecix")
	an, err := p.Details(ctx, source.Anime{Source: "animecix", ID: "82"})
	if err != nil {
		t.Fatal(err)
	}
	cmd := a.wantAlign(an)
	if cmd == nil {
		t.Fatalf("no chain requested for %+v", an)
	}
	m, ok := cmd().(alignMsg)
	if !ok || m.err != nil {
		t.Fatalf("chain: %+v", m)
	}
	a.onAlign(m)
	al, ok := a.alignment(an)
	if !ok || !spansEntries(al, an.MalID) {
		t.Fatalf("Bleach should span several MAL entries: %+v", al)
	}
	c, _, ok := canonical(al, ep(2, 30))
	t.Logf("site seasons %v → S2E30 = MAL %d ep %d", a.providerSeasons(an), c.MalID, c.Episode)
	if !ok || c.MalID == an.MalID {
		t.Fatalf("S2E30 should land in a later Sennen Kessen-hen entry, got %+v ok=%v", c, ok)
	}
}
