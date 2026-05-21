package dashboard

// handlers_graph_thumbnail.go — layout-snapshot endpoint for landing card thumbnails.
//
//	GET /api/graph/{group}/layout-snapshot?top=200
//
// Returns a compact positional snapshot of the top-N nodes (by degree) with
// pre-computed (x,y) positions laid out in a deterministic radial arrangement
// grouped by community. The frontend renders this as a static inline SVG
// thumbnail — no Cosmograph or WebGL required on the landing page.
//
// Design (#983):
//
//   - Only the top-N nodes by degree are included (default 200, max 500).
//   - Positions are normalised to [0,1]×[0,1] so the frontend can scale to any
//     viewport without recomputing layout.
//   - Communities are arranged on an outer ring; nodes within each community
//     are placed in a tighter inner ring around its community centroid.
//   - The response is intentionally tiny (<5 KB for 200 nodes) and served with
//     a short-lived cache header (5 min stale-while-revalidate) to let the
//     browser LRU cache it across navigations.
//   - If the group has no indexed entities the response is {nodes:[], edges:[]}
//     so the frontend can render an empty-state placeholder.
//   - External (stdlib/builtin) entities are always excluded.

import (
	"math"
	"net/http"
	"sort"
	"strconv"
)

// thumbnailMaxTop caps the layout-snapshot top-N regardless of what the caller
// requests. Keeps JSON payload under ~8 KB.
const thumbnailMaxTop = 500

// thumbnailDefaultTop is the number of nodes returned when ?top= is absent.
const thumbnailDefaultTop = 200

// ThumbnailNode is the per-node shape in the layout-snapshot response.
type ThumbnailNode struct {
	ID          string  `json:"id"`
	Repo        string  `json:"repo"`
	CommunityID *int    `json:"c,omitempty"` // omitted for unclustered nodes
	X           float64 `json:"x"`           // normalised [0,1]
	Y           float64 `json:"y"`           // normalised [0,1]
	Degree      int     `json:"d"`           // used by frontend for dot sizing
}

// handleGraphLayoutSnapshot — GET /api/graph/{group}/layout-snapshot
func (s *Server) handleGraphLayoutSnapshot(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	topN := thumbnailDefaultTop
	if q := r.URL.Query().Get("top"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			topN = n
		}
	}
	if topN > thumbnailMaxTop {
		topN = thumbnailMaxTop
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// ── 1. Collect top-N nodes by degree (no External entities) ─────────────

	type candidate struct {
		id          string
		repo        string
		communityID *int
		degree      int
	}

	var pool []candidate

	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		degMap := buildDegreeMap(repo.Doc.Relationships)
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			// Always exclude External (stdlib/builtin) placeholders.
			if dashStripScopePrefix(e.Kind) == externalKindSuffix {
				continue
			}
			pid := dashPrefixedID(repo.Slug, e.ID)
			var cid *int
			if e.CommunityID != nil {
				c := *e.CommunityID
				cid = &c
			}
			pool = append(pool, candidate{
				id:          pid,
				repo:        repo.Slug,
				communityID: cid,
				degree:      degMap[e.ID],
			})
		}
	}

	// Sort descending by degree, then by id for determinism.
	sort.Slice(pool, func(i, j int) bool {
		if pool[i].degree != pool[j].degree {
			return pool[i].degree > pool[j].degree
		}
		return pool[i].id < pool[j].id
	})
	if len(pool) > topN {
		pool = pool[:topN]
	}

	if len(pool) == 0 {
		// Nothing indexed yet — return empty so the frontend shows a placeholder.
		w.Header().Set("Cache-Control", "no-cache")
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}})
		return
	}

	// ── 2. Radial layout ─────────────────────────────────────────────────────
	//
	// Strategy:
	//   a) Collect distinct community IDs and assign each a centroid on the
	//      outer ring (radius 0.38, centred at 0.5,0.5).
	//   b) Each node is placed in a small inner ring around its community
	//      centroid; unclustered nodes (nil communityID) go on the outer ring
	//      directly, spread evenly among themselves.
	//
	// All coordinates are normalised to [0.05, 0.95] so dots never touch the
	// SVG edge.

	const (
		cx         = 0.5  // SVG centre x
		cy         = 0.5  // SVG centre y
		outerR     = 0.38 // community centroids ring radius
		innerR     = 0.10 // per-community node spread radius
		borderPad  = 0.05 // inset from [0,1] edges
	)

	// Build community → node list map.
	type communityKey = int
	communityNodes := map[communityKey][]int{} // community id → indices into pool
	var unclustered []int
	seenCommunity := map[int]struct{}{}

	for i, c := range pool {
		if c.communityID == nil {
			unclustered = append(unclustered, i)
			continue
		}
		k := *c.communityID
		communityNodes[k] = append(communityNodes[k], i)
		seenCommunity[k] = struct{}{}
	}

	// Stable ordered community list for deterministic centroid placement.
	communityOrder := make([]int, 0, len(seenCommunity))
	for k := range seenCommunity {
		communityOrder = append(communityOrder, k)
	}
	sort.Ints(communityOrder)

	// Assign centroid angles evenly around the outer ring.
	centroidAngle := map[int]float64{}
	nCommunities := len(communityOrder)
	for i, k := range communityOrder {
		angle := 2 * math.Pi * float64(i) / float64(nCommunities)
		centroidAngle[k] = angle
	}

	// Position each node.
	positions := make([][2]float64, len(pool))

	for k, idxs := range communityNodes {
		angle := centroidAngle[k]
		// Community centroid in normalised space.
		centX := cx + outerR*math.Cos(angle)
		centY := cy + outerR*math.Sin(angle)

		n := len(idxs)
		for j, idx := range idxs {
			var nodeAngle, nodeR float64
			if n == 1 {
				nodeAngle = angle
				nodeR = 0
			} else {
				nodeAngle = 2 * math.Pi * float64(j) / float64(n)
				// Denser communities get a slightly larger spread radius.
				spread := innerR * math.Sqrt(float64(n)/float64(topN)*float64(nCommunities))
				if spread < 0.04 {
					spread = 0.04
				}
				if spread > innerR {
					spread = innerR
				}
				nodeR = spread
			}
			x := centX + nodeR*math.Cos(nodeAngle)
			y := centY + nodeR*math.Sin(nodeAngle)
			// Clamp to padded canvas.
			x = math.Max(borderPad, math.Min(1-borderPad, x))
			y = math.Max(borderPad, math.Min(1-borderPad, y))
			positions[idx] = [2]float64{x, y}
		}
	}

	// Unclustered nodes: evenly spaced on a smaller inner ring.
	nUnclust := len(unclustered)
	for j, idx := range unclustered {
		var x, y float64
		if nUnclust == 1 {
			x, y = cx, cy
		} else {
			angle := 2 * math.Pi * float64(j) / float64(nUnclust)
			r := outerR * 0.5
			x = cx + r*math.Cos(angle)
			y = cy + r*math.Sin(angle)
		}
		positions[idx] = [2]float64{
			math.Max(borderPad, math.Min(1-borderPad, x)),
			math.Max(borderPad, math.Min(1-borderPad, y)),
		}
	}

	// ── 3. Serialise ─────────────────────────────────────────────────────────

	nodes := make([]ThumbnailNode, len(pool))
	for i, c := range pool {
		nodes[i] = ThumbnailNode{
			ID:          c.id,
			Repo:        c.repo,
			CommunityID: c.communityID,
			X:           math.Round(positions[i][0]*10000) / 10000,
			Y:           math.Round(positions[i][1]*10000) / 10000,
			Degree:      c.degree,
		}
	}

	// 5-minute stale-while-revalidate — snapshot changes only on re-index.
	w.Header().Set("Cache-Control", "public, max-age=300, stale-while-revalidate=600")
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":       nodes,
		"total_nodes": len(nodes),
	})
}
