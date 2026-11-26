package seasons

import (
	"testing"

	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

func TestFranchiseTreeWatchOrderAndNextTransition(t *testing.T) {
	t.Setenv("ANICLITR_HOME", t.TempDir())
	raw := []FranchiseNode{
		{MalID: 20, Title: "Series S2", Format: "TV", Year: 2022, Episodes: 12, Relation: "SEQUEL"},
		{MalID: 10, Title: "Series S1", Format: "TV", Year: 2020, Episodes: 12, Relation: "MAIN"},
		{MalID: 15, Title: "Series Movie", Format: "MOVIE", Year: 2021, Episodes: 1, Relation: "SEQUEL"},
		{MalID: 99, Title: "Series OVA", Format: "OVA", Year: 2021, Episodes: 2, Relation: "SIDE_STORY", SideStory: true},
	}
	tree := BuildFranchiseTree("Series", raw)
	if len(tree.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(tree.Nodes))
	}
	if tree.Nodes[0].MalID != 10 || tree.Nodes[1].MalID != 15 || tree.Nodes[2].MalID != 20 || !tree.Nodes[3].SideStory {
		t.Fatalf("unexpected watch order: %+v", tree.Nodes)
	}

	lib := store.OpenLibrary()
	lib.Set(source.Anime{Source: "animecix", ID: "1", Title: "Series S1", MalID: 10}, func(it *store.Item) {
		it.Status = store.Completed
	})
	tree.AnnotateStatus(lib, nil)
	if tree.Nodes[0].Status != "done" || tree.Nodes[1].Status != "next" {
		t.Fatalf("unexpected status annotation: %+v", tree.Nodes)
	}

	next, ok := tree.NextAfter(10)
	if !ok || next.MalID != 15 {
		t.Fatalf("NextAfter(10) = %+v (ok=%v), want Movie (15)", next, ok)
	}
}
