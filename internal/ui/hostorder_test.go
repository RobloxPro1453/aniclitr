package ui

import (
	"testing"

	"aniclitr/internal/config"
)

func TestHostOrderScreen(t *testing.T) {
	a := testApp(t, Graphics{})
	s := newHostOrder(a)
	if len(s.order) != len(primaryHosts) || s.order[0] != "Tau Video" {
		t.Fatalf("default order: %v", s.order)
	}
	// move OK.ru (4th) to the top
	for range 3 {
		s.update(a, key("down"))
	}
	for range 3 {
		s.update(a, key("["))
	}
	if a.cfg.HostOrder[0] != "OK.ru" || s.cur != 0 {
		t.Fatalf("move up: %v cur=%d", a.cfg.HostOrder, s.cur)
	}
	// never, then only: a host is one or the other
	s.update(a, key("n"))
	if !containsFold(a.cfg.NeverHosts, "OK.ru") {
		t.Fatal("n did not exclude")
	}
	s.update(a, key("o"))
	if containsFold(a.cfg.NeverHosts, "OK.ru") || !containsFold(a.cfg.OnlyHosts, "ok.ru") {
		t.Fatalf("o: never=%v only=%v", a.cfg.NeverHosts, a.cfg.OnlyHosts)
	}
	// saved: a fresh load sees it, and quick play uses it
	if c := config.Load(); c.HostOrder[0] != "OK.ru" || len(c.OnlyHosts) != 1 {
		t.Fatalf("not saved: %+v %+v", c.HostOrder, c.OnlyHosts)
	}
	if r := a.selectionRules(); r.HostOrder[0] != "OK.ru" || len(r.OnlyHosts) != 1 {
		t.Fatalf("rules: %+v", r)
	}
	s.update(a, key("0"))
	if a.cfg.HostOrder != nil || a.cfg.OnlyHosts != nil || s.order[0] != "Tau Video" {
		t.Fatalf("reset: %v %v %v", a.cfg.HostOrder, a.cfg.OnlyHosts, s.order)
	}
}
