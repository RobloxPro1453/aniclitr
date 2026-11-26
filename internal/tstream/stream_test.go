package tstream

import (
	"context"
	"testing"
)

func TestOpenRejectsEmptyInput(t *testing.T) {
	if _, err := Open(context.Background(), "", ""); err == nil {
		t.Fatalf("expected error on empty torrent/magnet")
	}
}

func TestMimeForExt(t *testing.T) {
	if got := mimeForExt(".mkv"); got != "video/x-matroska" {
		t.Fatalf("expected matroska mime, got %q", got)
	}
	if got := mimeForExt(".mp4"); got != "video/mp4" {
		t.Fatalf("expected mp4 mime, got %q", got)
	}
}
