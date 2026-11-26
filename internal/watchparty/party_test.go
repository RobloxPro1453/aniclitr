package watchparty

import (
	"testing"
	"time"
)

func TestHostJoinAndTwoWaySync(t *testing.T) {
	host, err := Host("127.0.0.1:0", "Yasin", "Sousou no Frieren")
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	guest, err := Join(host.ListenAddr(), "Ahmet")
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()

	// Wait for join event on host.
	select {
	case ev := <-host.Events:
		if ev.Kind != "join" || ev.Nick != "Ahmet" || ev.Peers != 2 {
			t.Fatalf("unexpected join event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for peer join")
	}

	// Host broadcasts pause at 142.5s -> guest receives sync.
	host.BroadcastState(142.5, true)
	select {
	case ev := <-guest.Events:
		if ev.Kind != "sync" || ev.Nick != "Yasin" || ev.Pos != 142.5 || !ev.Paused {
			t.Fatalf("unexpected guest sync event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for guest sync")
	}
}
