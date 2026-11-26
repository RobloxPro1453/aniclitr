package discord

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestPacketFramingAndActivityPayload(t *testing.T) {
	now := time.Unix(1764000000, 0)
	frame := buildActivityFrame(1234, Activity{
		Details:    "Sousou no Frieren",
		State:      "Sezon 1 · Bölüm 3",
		LargeImage: "https://s4.anilist.co/cover.jpg",
		LargeText:  "aniclitr",
		Start:      now,
	}, "n-1")

	var buf bytes.Buffer
	if err := writePacket(&buf, opFrame, frame); err != nil {
		t.Fatal(err)
	}
	op, got, err := readPacket(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if op != opFrame {
		t.Fatalf("expected opcode %d, got %d", opFrame, op)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["cmd"] != "SET_ACTIVITY" || parsed["nonce"] != "n-1" {
		t.Fatalf("unexpected frame: %+v", parsed)
	}
	args := parsed["args"].(map[string]any)
	act := args["activity"].(map[string]any)
	if act["details"] != "Sousou no Frieren" || int(act["type"].(float64)) != 3 {
		t.Fatalf("unexpected activity: %+v", act)
	}
}
