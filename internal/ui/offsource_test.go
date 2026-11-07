package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"aniclitr/internal/source"
)

// Only AnimeciX is switched on; the show was watched on HiAnime.
func offApp(t *testing.T) (*App, source.Anime) {
	a := testApp(t, Graphics{})
	a.cfg.Sources = map[string]bool{"animecix": true, "hianime": false, "animetr": false, "anihub": false}
	return a, source.Anime{Source: "hianime", ID: "another-123", Title: "Another", Kind: "TV"}
}

func TestOffSourceOpensOnSwitchedOnOne(t *testing.T) {
	a, an := offApp(t)
	s := newAnime(an, nil, 1, 5)
	if cmd := s.start(a); cmd == nil || !s.gate {
		t.Fatal("a switched-off source must be looked past first")
	}
	if out := ansi.Strip(s.view(a, 120, 30)); !strings.Contains(out, "HiAnime ayarlarda kapalı") {
		t.Fatalf("the wait must say why:\n%s", out)
	}
	cix := source.Anime{Source: "animecix", ID: "77", Title: "Another"}
	s.update(a, altsMsg{key: an.Key(), alts: []source.Anime{cix}})
	if s.gate || s.base.Key() != cix.Key() || s.offSrc {
		t.Fatalf("should open on AnimeciX: base %s gate %v off %v", s.base.Key(), s.gate, s.offSrc)
	}
	if !strings.Contains(a.toast, "açıldı: AnimeciX") {
		t.Fatalf("toast %q", a.toast)
	}
	// s still reaches HiAnime, marked as switched off
	if len(s.srcs) != 2 {
		t.Fatalf("sources %v", s.srcs)
	}
	var off bool
	for _, it := range s.sourceItems(a) {
		off = off || (strings.Contains(it.label, "HiAnime") && strings.Contains(it.note, "ayarlarda kapalı"))
	}
	if !off {
		t.Fatalf("the picker must mark HiAnime as switched off: %+v", s.sourceItems(a))
	}
}

func TestOffSourceOnlyThereStillOpens(t *testing.T) {
	a, an := offApp(t)
	s := newAnime(an, nil, 1, 5)
	s.start(a)
	s.update(a, altsMsg{key: an.Key()})
	if s.gate || !s.offSrc || s.base.Key() != an.Key() {
		t.Fatalf("history must not dead-end: gate %v off %v base %s", s.gate, s.offSrc, s.base.Key())
	}
	if bar := ansi.Strip(strings.Join(s.sourceLine(a, 100), "")); !strings.Contains(bar, "kapalı") {
		t.Fatalf("the off source must be marked: %q", bar)
	}
	if a.toastKind != "warn" {
		t.Fatalf("should warn, got %q %q", a.toastKind, a.toast)
	}
}

func TestOffSourceExplicitIsNotRedirected(t *testing.T) {
	a, an := offApp(t)
	s := newAnime(an, nil, 0, 0)
	s.explicit = true // a pasted link / "open on HiAnime"
	s.start(a)
	if s.gate || s.base.Key() != an.Key() {
		t.Fatal("an explicit choice was second-guessed")
	}
}
