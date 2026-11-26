package discord

import (
	"bytes"
	"encoding/json"
	"strings"
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
		SmallImage: DefaultIconURL,
		SmallText:  "aniclitr",
		Start:      now,
		End:        now.Add(24 * time.Minute),
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
	if act["details"] != "Sousou no Frieren" || int(act["type"].(float64)) != 3 || int(act["status_display_type"].(float64)) != 2 {
		t.Fatalf("unexpected activity: %+v", act)
	}
	if _, hasName := act["name"]; hasName {
		t.Fatalf("must not send immutable 'name' field in local SET_ACTIVITY")
	}
	ts := act["timestamps"].(map[string]any)
	if int64(ts["end"].(float64)) <= int64(ts["start"].(float64)) {
		t.Fatalf("expected end > start timestamp: %+v", ts)
	}
	assets := act["assets"].(map[string]any)
	if assets["small_image"] != DefaultIconURL {
		t.Fatalf("expected small_image icon, got %+v", assets)
	}
}

func TestPausedPayloadKeepsPresenceWithoutCountdown(t *testing.T) {
	now := time.Unix(1764000000, 0)
	frame := buildActivityFrame(1234, Activity{
		Details:    "Another",
		State:      "Sezon 1 · Bölüm 1 (02:19)",
		LargeImage: "https://example.com/another.jpg",
		Start:      now,
		End:        now.Add(20 * time.Minute),
		Paused:     true,
	}, "n-2")

	var parsed map[string]any
	if err := json.Unmarshal(frame, &parsed); err != nil {
		t.Fatal(err)
	}
	act := parsed["args"].(map[string]any)["activity"].(map[string]any)
	if !strings.Contains(act["state"].(string), "Duraklatıldı") {
		t.Fatalf("expected paused state label, got %q", act["state"])
	}
	if _, hasTS := act["timestamps"]; hasTS {
		t.Fatalf("paused activity should omit running timestamps so clock freezes")
	}
}
