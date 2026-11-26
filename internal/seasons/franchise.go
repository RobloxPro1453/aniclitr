package seasons

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"aniclitr/internal/netx"
	"aniclitr/internal/store"
)

// FranchiseNode represents one entry in a franchise Watch Order tree
// (mainline seasons, sequel movies, prequels, side stories, OVAs).
type FranchiseNode struct {
	AnilistID int    `json:"anilist_id"`
	MalID     int    `json:"mal_id"`
	Title     string `json:"title"`
	Format    string `json:"format"` // TV, MOVIE, OVA, SPECIAL, ONA
	Year      int    `json:"year"`
	Episodes  int    `json:"episodes"`
	Relation  string `json:"relation"` // MAIN, SEQUEL, PREQUEL, SIDE_STORY, SPIN_OFF
	SideStory bool   `json:"side_story"`
	Order     int    `json:"order"`
	Status    string `json:"status,omitempty"` // "done", "next", "watching", "pending"
}

// FranchiseTree is the complete chronological + release-order watch graph.
type FranchiseTree struct {
	RootTitle string
	Nodes     []FranchiseNode
}

// AnnotateStatus marks each node with ✓ ("done"), ▶ ("watching"/"next"), or
// ○ ("pending") using the user's library and watch history.
func (t *FranchiseTree) AnnotateStatus(lib *store.Library, hist *store.History) {
	doneByMal := map[int]bool{}
	watchByMal := map[int]bool{}
	if lib != nil {
		for _, it := range lib.All() {
			if it.MalID <= 0 {
				continue
			}
			if it.Status == store.Completed {
				doneByMal[it.MalID] = true
			} else if it.Status == store.Watching {
				watchByMal[it.MalID] = true
			}
		}
	}
	if hist != nil {
		for _, e := range hist.Recent() {
			if e.MalID > 0 && !doneByMal[e.MalID] {
				watchByMal[e.MalID] = true
			}
		}
	}

	nextAssigned := false
	for i := range t.Nodes {
		n := &t.Nodes[i]
		switch {
		case doneByMal[n.MalID]:
			n.Status = "done"
		case watchByMal[n.MalID]:
			n.Status = "watching"
			nextAssigned = true
		case !n.SideStory && !nextAssigned:
			n.Status = "next"
			nextAssigned = true
		default:
			n.Status = "pending"
		}
	}
}

// NextAfter returns the next mainline node (season or sequel movie) following
// curMalID so playback can transition cleanly to the right movie or sequel.
func (t FranchiseTree) NextAfter(curMalID int) (FranchiseNode, bool) {
	for i, n := range t.Nodes {
		if n.MalID == curMalID {
			for j := i + 1; j < len(t.Nodes); j++ {
				if !t.Nodes[j].SideStory {
					return t.Nodes[j], true
				}
			}
			return FranchiseNode{}, false
		}
	}
	return FranchiseNode{}, false
}

// BuildFranchiseTree builds a sorted watch-order tree from raw nodes.
func BuildFranchiseTree(rootTitle string, raw []FranchiseNode) FranchiseTree {
	seen := map[int]bool{}
	var main, side []FranchiseNode
	for _, n := range raw {
		key := n.MalID
		if key == 0 {
			key = -n.AnilistID
		}
		if key != 0 && seen[key] {
			continue
		}
		if key != 0 {
			seen[key] = true
		}
		if n.SideStory {
			side = append(side, n)
		} else {
			main = append(main, n)
		}
	}
	byChrono := func(xs []FranchiseNode) {
		sort.SliceStable(xs, func(i, j int) bool {
			yi, yj := xs[i].Year, xs[j].Year
			if yi == 0 {
				yi = 9999
			}
			if yj == 0 {
				yj = 9999
			}
			if yi != yj {
				return yi < yj
			}
			return xs[i].MalID < xs[j].MalID
		})
	}
	byChrono(main)
	byChrono(side)
	all := make([]FranchiseNode, 0, len(main)+len(side))
	for i, n := range main {
		n.Order = i + 1
		all = append(all, n)
	}
	for _, n := range side {
		n.Order = 0
		all = append(all, n)
	}
	return FranchiseTree{RootTitle: rootTitle, Nodes: all}
}

// FetchFranchise queries AniList's open keyless GraphQL relation graph starting
// from malID (walking PREQUEL/SEQUEL/SIDE_STORY/SPIN_OFF up to 12 hops).
func FetchFranchise(ctx context.Context, malID int) (FranchiseTree, error) {
	if malID <= 0 {
		return FranchiseTree{}, nil
	}
	const gql = `query ($idMal: Int) {
  Media(idMal: $idMal, type: ANIME) {
    id idMal format seasonYear episodes
    title { romaji english }
    relations {
      edges {
        relationType
        node {
          id idMal type format seasonYear episodes
          title { romaji english }
        }
      }
    }
  }
}`
	visited := map[int]bool{}
	queue := []int{malID}
	var nodes []FranchiseNode
	rootTitle := ""

loop:
	for len(queue) > 0 && len(visited) < 12 {
		if ctx.Err() != nil {
			break
		}
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true

		body, _ := json.Marshal(map[string]any{
			"query":     gql,
			"variables": map[string]any{"idMal": cur},
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://graphql.anilist.co", strings.NewReader(string(body)))
		if err != nil {
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", netx.UserAgent)
		resp, err := netx.Client.Do(req)
		if err != nil {
			break
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		var out struct {
			Data struct {
				Media struct {
					ID         int    `json:"id"`
					IDMal      int    `json:"idMal"`
					Format     string `json:"format"`
					SeasonYear int    `json:"seasonYear"`
					Episodes   int    `json:"episodes"`
					Title      struct {
						Romaji  string `json:"romaji"`
						English string `json:"english"`
					} `json:"title"`
					Relations struct {
						Edges []struct {
							RelationType string `json:"relationType"`
							Node         struct {
								ID         int    `json:"id"`
								IDMal      int    `json:"idMal"`
								Type       string `json:"type"`
								Format     string `json:"format"`
								SeasonYear int    `json:"seasonYear"`
								Episodes   int    `json:"episodes"`
								Title      struct {
									Romaji  string `json:"romaji"`
									English string `json:"english"`
								} `json:"title"`
							} `json:"node"`
						} `json:"edges"`
					} `json:"relations"`
				} `json:"Media"`
			} `json:"data"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out)
		resp.Body.Close()

		m := out.Data.Media
		if m.ID == 0 {
			continue
		}
		tName := m.Title.Romaji
		if tName == "" {
			tName = m.Title.English
		}
		if rootTitle == "" {
			rootTitle = tName
		}
		nodes = append(nodes, FranchiseNode{
			AnilistID: m.ID,
			MalID:     m.IDMal,
			Title:     tName,
			Format:    m.Format,
			Year:      m.SeasonYear,
			Episodes:  m.Episodes,
			Relation:  "MAIN",
		})
		for _, e := range m.Relations.Edges {
			if e.Node.Type != "ANIME" || e.Node.IDMal <= 0 {
				continue
			}
			nt := e.Node.Title.Romaji
			if nt == "" {
				nt = e.Node.Title.English
			}
			switch e.RelationType {
			case "PREQUEL", "SEQUEL":
				if !visited[e.Node.IDMal] {
					queue = append(queue, e.Node.IDMal)
				}
			case "SIDE_STORY", "SPIN_OFF", "ALTERNATIVE":
				nodes = append(nodes, FranchiseNode{
					AnilistID: e.Node.ID,
					MalID:     e.Node.IDMal,
					Title:     nt,
					Format:    e.Node.Format,
					Year:      e.Node.SeasonYear,
					Episodes:  e.Node.Episodes,
					Relation:  e.RelationType,
					SideStory: true,
				})
			}
		}
		if len(queue) > 0 {
			t := time.NewTimer(300 * time.Millisecond)
			select {
			case <-ctx.Done():
				t.Stop()
				break loop
			case <-t.C:
			}
		}
	}
	if len(nodes) == 0 {
		return FranchiseTree{}, fmt.Errorf("evren grafiği bulunamadı (MAL %d)", malID)
	}
	return BuildFranchiseTree(rootTitle, nodes), nil
}
