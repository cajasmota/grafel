package graph

// orientation.go — graph-orientation analysis (issue #4290).
//
// This file synthesises a "where do I start reading this codebase?" view from
// attributes the Pass-4 algorithm pass (see algorithms.go) already wrote onto
// the on-disk graph: per-entity betweenness Centrality, PageRank, CommunityID,
// and per-edge Confidence. Nothing here re-runs an expensive centrality pass —
// betweenness is read straight off Entity.Centrality, so the analysis is O(V+E)
// and safe to run inline on an MCP request.
//
// Three parts:
//   1. KeyEntities    — structural hubs/bridges ranked by a blend of degree
//                       centrality (cheap, computed here) and the precomputed
//                       approximate betweenness centrality.
//   2. CrossCutEdges  — edges that bridge communities, cross a file-type/layer
//                       boundary, or wire a peripheral node to a hub. Each
//                       carries a human-readable reason and a composite score.
//   3. Questions      — templated orientation questions mined from ambiguous
//                       (low-confidence) edges, high-betweenness bridge nodes,
//                       and isolated nodes.
//
// All outputs are deterministic: every ranking has an ID-based tiebreak so the
// surface is reproducible across runs (matching the #481 determinism contract).

import (
	"fmt"
	"sort"
	"strings"
)

// AmbiguousConfidenceThreshold is the effective-confidence ceiling below which
// an edge is treated as "ambiguous" for question mining. Fallback speculation
// (0.4) and anything more pessimistic falls below it; regex/AST-grade edges
// (>=0.7) do not. Edges with a zero/unset confidence read as 1.0 (direct AST)
// and are never ambiguous (see types.EffectiveConfidence).
const AmbiguousConfidenceThreshold = 0.5

// KeyEntity is a structural hub/bridge in the graph.
type KeyEntity struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	SourceFile  string  `json:"source_file,omitempty"`
	CommunityID int     `json:"community_id"`
	Degree      int     `json:"degree"`      // in+out degree (cheap centrality)
	Betweenness float64 `json:"betweenness"` // precomputed approx. betweenness
	PageRank    float64 `json:"pagerank"`    // precomputed pagerank
	Score       float64 `json:"score"`       // composite ranking score
	Reason      string  `json:"reason"`      // why this entity is a key entity
}

// CrossCutEdge is an edge that crosses a structural boundary.
type CrossCutEdge struct {
	FromID        string   `json:"from_id"`
	ToID          string   `json:"to_id"`
	FromName      string   `json:"from_name"`
	ToName        string   `json:"to_name"`
	Kind          string   `json:"kind"`
	Score         float64  `json:"score"`
	Reasons       []string `json:"reasons"`
	FromCommunity int      `json:"from_community"`
	ToCommunity   int      `json:"to_community"`
}

// OrientQuestion is a templated orientation question with provenance.
type OrientQuestion struct {
	Question string   `json:"question"`
	Source   string   `json:"source"` // ambiguous_edge | bridge_node | isolated_node
	Entities []string `json:"entities,omitempty"`
}

// OrientationResult bundles the three analysis parts.
type OrientationResult struct {
	KeyEntities   []KeyEntity      `json:"key_entities"`
	CrossCutEdges []CrossCutEdge   `json:"cross_cutting_edges"`
	Questions     []OrientQuestion `json:"orientation_questions"`
}

// OrientationOptions bounds the analysis output.
type OrientationOptions struct {
	TopEntities  int // max KeyEntities returned (default 15)
	TopEdges     int // max CrossCutEdges returned (default 15)
	MaxQuestions int // max OrientQuestions returned (default 12)
}

// DefaultOrientationOptions returns the production caps.
func DefaultOrientationOptions() OrientationOptions {
	return OrientationOptions{TopEntities: 15, TopEdges: 15, MaxQuestions: 12}
}

func (o OrientationOptions) normalized() OrientationOptions {
	if o.TopEntities <= 0 {
		o.TopEntities = 15
	}
	if o.TopEdges <= 0 {
		o.TopEdges = 15
	}
	if o.MaxQuestions <= 0 {
		o.MaxQuestions = 12
	}
	return o
}

// communityOf reads an entity's community id, defaulting to -1 ("ungrouped")
// when the Pass-4 attribute is absent.
func communityOf(e Entity) int {
	if e.CommunityID != nil {
		return *e.CommunityID
	}
	return -1
}

func centralityOf(e Entity) float64 {
	if e.Centrality != nil {
		return *e.Centrality
	}
	return 0
}

func pageRankOf(e Entity) float64 {
	if e.PageRank != nil {
		return *e.PageRank
	}
	return 0
}

// effectiveEdgeConfidence mirrors types.EffectiveConfidence without importing
// the types package (graph is a lower-level package): a zero/unset confidence
// reads as 1.0 (direct AST), and values are clamped to [0,1].
func effectiveEdgeConfidence(c float64) float64 {
	if c == 0 {
		return 1.0
	}
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

// layerOf derives a coarse architectural layer from an entity's source path and
// language. The mapping is intentionally heuristic and cheap; it exists so the
// cross-cutting scorer can reward edges that span layers (e.g. an HTTP handler
// reaching into a data-access function). Returns "" when no layer is inferable.
func layerOf(e Entity) string {
	p := strings.ToLower(e.SourceFile)
	switch {
	case containsAny(p, "test", "spec", "__tests__", "_test."):
		return "test"
	case containsAny(p, "migration", "schema", "models/", "model/", "entity/", "entities/", "repository", "repositories", "dao", "/db/", "database"):
		return "data"
	case containsAny(p, "controller", "handler", "route", "router", "endpoint", "/api/", "resolver", "rest"):
		return "api"
	case containsAny(p, "service", "usecase", "use_case", "domain", "business"):
		return "service"
	case containsAny(p, "/ui/", "component", "view", "page", "/web/", "frontend", "client/"):
		return "ui"
	case containsAny(p, "config", "settings", "/infra", "deploy", "terraform", "k8s", "helm"):
		return "infra"
	}
	return ""
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// fileExt returns the lowercased extension (incl dot) of an entity's source
// file, or "" if none. Used for file-type-boundary detection.
func fileExt(e Entity) string {
	p := e.SourceFile
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		return strings.ToLower(p[i:])
	}
	return ""
}

// AnalyzeOrientation computes the three-part orientation view over a single
// graph document's entities and relationships. It reads the precomputed Pass-4
// attributes (Centrality/PageRank/CommunityID) and computes only cheap O(V+E)
// quantities (degree, boundary checks) inline.
func AnalyzeOrientation(entities []Entity, rels []Relationship, opts OrientationOptions) OrientationResult {
	opts = opts.normalized()

	byID := make(map[string]Entity, len(entities))
	for _, e := range entities {
		byID[e.ID] = e
	}

	// Degree centrality: count in+out edges per entity (endpoints present in the
	// entity set only, matching BuildGraph's edge-filtering contract).
	inDeg := make(map[string]int, len(entities))
	outDeg := make(map[string]int, len(entities))
	for _, r := range rels {
		if r.FromID == "" || r.ToID == "" || r.FromID == r.ToID {
			continue
		}
		if _, ok := byID[r.FromID]; !ok {
			continue
		}
		if _, ok := byID[r.ToID]; !ok {
			continue
		}
		outDeg[r.FromID]++
		inDeg[r.ToID]++
	}
	degree := func(id string) int { return inDeg[id] + outDeg[id] }

	// ---- Part 1: key entities -------------------------------------------------
	// Normalise degree and betweenness to [0,1] across the corpus so the blend is
	// scale-free, then rank by 0.6*betweenness + 0.4*degree (betweenness is the
	// better bridge signal; degree is the better hub signal — both matter).
	maxDeg, maxBetw := 0, 0.0
	for _, e := range entities {
		if d := degree(e.ID); d > maxDeg {
			maxDeg = d
		}
		if b := centralityOf(e); b > maxBetw {
			maxBetw = b
		}
	}
	keys := make([]KeyEntity, 0, len(entities))
	for _, e := range entities {
		d := degree(e.ID)
		if d == 0 {
			continue // isolated nodes are surfaced via questions, not as hubs
		}
		betw := centralityOf(e)
		normDeg, normBetw := 0.0, 0.0
		if maxDeg > 0 {
			normDeg = float64(d) / float64(maxDeg)
		}
		if maxBetw > 0 {
			normBetw = betw / maxBetw
		}
		score := 0.6*normBetw + 0.4*normDeg
		keys = append(keys, KeyEntity{
			ID:          e.ID,
			Name:        e.Name,
			Kind:        e.Kind,
			SourceFile:  e.SourceFile,
			CommunityID: communityOf(e),
			Degree:      d,
			Betweenness: betw,
			PageRank:    pageRankOf(e),
			Score:       score,
			Reason:      keyEntityReason(normBetw, normDeg, e),
		})
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].Score != keys[j].Score {
			return keys[i].Score > keys[j].Score
		}
		return keys[i].ID < keys[j].ID
	})
	if len(keys) > opts.TopEntities {
		keys = keys[:opts.TopEntities]
	}

	// ---- Part 2: cross-cutting edges -----------------------------------------
	// Score = sum of boundary signals:
	//   +2.0 bridges two distinct (grouped) communities
	//   +1.0 crosses an architectural layer boundary
	//   +0.5 crosses a file-type (extension) boundary
	//   +1.0 wires a peripheral node (degree<=2) to a hub (top-tier degree)
	hubCut := maxDeg / 5 // "hub" = top-20%-degree-ish; cheap proxy
	if hubCut < 3 {
		hubCut = 3
	}
	cuts := make([]CrossCutEdge, 0)
	for _, r := range rels {
		if r.FromID == "" || r.ToID == "" || r.FromID == r.ToID {
			continue
		}
		from, okf := byID[r.FromID]
		to, okt := byID[r.ToID]
		if !okf || !okt {
			continue
		}
		var reasons []string
		score := 0.0
		cf, ct := communityOf(from), communityOf(to)
		if cf != ct && cf >= 0 && ct >= 0 {
			score += 2.0
			reasons = append(reasons, "bridges_communities")
		}
		if lf, lt := layerOf(from), layerOf(to); lf != "" && lt != "" && lf != lt {
			score += 1.0
			reasons = append(reasons, fmt.Sprintf("crosses_layer:%s->%s", lf, lt))
		}
		if ef, et := fileExt(from), fileExt(to); ef != "" && et != "" && ef != et {
			score += 0.5
			reasons = append(reasons, "crosses_file_type")
		}
		df, dt := degree(r.FromID), degree(r.ToID)
		if (df <= 2 && dt >= hubCut) || (dt <= 2 && df >= hubCut) {
			score += 1.0
			reasons = append(reasons, "peripheral_to_hub")
		}
		if score == 0 {
			continue
		}
		cuts = append(cuts, CrossCutEdge{
			FromID:        r.FromID,
			ToID:          r.ToID,
			FromName:      from.Name,
			ToName:        to.Name,
			Kind:          r.Kind,
			Score:         score,
			Reasons:       reasons,
			FromCommunity: cf,
			ToCommunity:   ct,
		})
	}
	sort.SliceStable(cuts, func(i, j int) bool {
		if cuts[i].Score != cuts[j].Score {
			return cuts[i].Score > cuts[j].Score
		}
		if cuts[i].FromID != cuts[j].FromID {
			return cuts[i].FromID < cuts[j].FromID
		}
		return cuts[i].ToID < cuts[j].ToID
	})
	if len(cuts) > opts.TopEdges {
		cuts = cuts[:opts.TopEdges]
	}

	// ---- Part 3: orientation questions ---------------------------------------
	questions := mineQuestions(entities, rels, byID, degree, keys, opts.MaxQuestions)

	return OrientationResult{
		KeyEntities:   keys,
		CrossCutEdges: cuts,
		Questions:     questions,
	}
}

func keyEntityReason(normBetw, normDeg float64, e Entity) string {
	switch {
	case e.IsArticulationPt:
		return "articulation_point — removing it disconnects part of the graph"
	case e.IsGodNode:
		return "god_node — top-percentile betweenness and/or pagerank"
	case normBetw >= 0.5 && normDeg >= 0.5:
		return "high_betweenness_and_degree — central hub bridging the graph"
	case normBetw >= 0.5:
		return "high_betweenness — a bridge on many shortest paths"
	default:
		return "high_degree — a well-connected hub"
	}
}

// mineQuestions builds a deterministic, deduplicated list of templated
// orientation questions from three sources: ambiguous (low-confidence) edges,
// high-betweenness bridge nodes (the top key entities), and isolated nodes.
func mineQuestions(entities []Entity, rels []Relationship, byID map[string]Entity, degree func(string) int, keys []KeyEntity, max int) []OrientQuestion {
	out := make([]OrientQuestion, 0, max)
	seen := make(map[string]bool)
	add := func(q OrientQuestion) {
		if seen[q.Question] {
			return
		}
		seen[q.Question] = true
		out = append(out, q)
	}

	// (a) Ambiguous edges — sorted by ascending confidence (most ambiguous
	// first), tiebroken by endpoint IDs for determinism.
	type ambEdge struct {
		from, to string
		kind     string
		conf     float64
	}
	var amb []ambEdge
	for _, r := range rels {
		if r.FromID == "" || r.ToID == "" {
			continue
		}
		conf := effectiveEdgeConfidence(r.Confidence)
		if conf >= AmbiguousConfidenceThreshold {
			continue
		}
		if _, ok := byID[r.FromID]; !ok {
			continue
		}
		if _, ok := byID[r.ToID]; !ok {
			continue
		}
		amb = append(amb, ambEdge{r.FromID, r.ToID, r.Kind, conf})
	}
	sort.SliceStable(amb, func(i, j int) bool {
		if amb[i].conf != amb[j].conf {
			return amb[i].conf < amb[j].conf
		}
		if amb[i].from != amb[j].from {
			return amb[i].from < amb[j].from
		}
		return amb[i].to < amb[j].to
	})

	// (b) Bridge nodes — the highest-betweenness key entities.
	type bridge struct {
		id, name string
		betw     float64
	}
	var bridges []bridge
	for _, k := range keys {
		if k.Betweenness > 0 {
			bridges = append(bridges, bridge{k.ID, k.Name, k.Betweenness})
		}
	}
	// keys are already betweenness-weighted & sorted; keep that order.

	// (c) Isolated nodes — degree 0, sorted by ID.
	var isolated []Entity
	for _, e := range entities {
		if degree(e.ID) == 0 {
			isolated = append(isolated, e)
		}
	}
	sort.SliceStable(isolated, func(i, j int) bool { return isolated[i].ID < isolated[j].ID })

	// Interleave the three sources round-robin so a single dominant source can't
	// crowd the others out of a small budget. Order of preference per round:
	// bridge, ambiguous, isolated.
	bi, ai, ii := 0, 0, 0
	for len(out) < max && (bi < len(bridges) || ai < len(amb) || ii < len(isolated)) {
		if bi < len(bridges) && len(out) < max {
			b := bridges[bi]
			bi++
			name := b.name
			if name == "" {
				name = b.id
			}
			add(OrientQuestion{
				Question: fmt.Sprintf("%q sits on many shortest paths (a bridge). What does it coordinate, and what breaks if it changes?", name),
				Source:   "bridge_node",
				Entities: []string{b.id},
			})
		}
		if ai < len(amb) && len(out) < max {
			e := amb[ai]
			ai++
			fn := nameOr(byID, e.from)
			tn := nameOr(byID, e.to)
			add(OrientQuestion{
				Question: fmt.Sprintf("Is the %s edge %q -> %q real? It was inferred with low confidence (%.2f) and may be a dynamic/ambiguous binding.", strings.ToLower(orDash(e.kind)), fn, tn, e.conf),
				Source:   "ambiguous_edge",
				Entities: []string{e.from, e.to},
			})
		}
		if ii < len(isolated) && len(out) < max {
			e := isolated[ii]
			ii++
			name := e.Name
			if name == "" {
				name = e.ID
			}
			add(OrientQuestion{
				Question: fmt.Sprintf("%q has no recorded relationships. Is it dead code, an entry point, or wired up dynamically?", name),
				Source:   "isolated_node",
				Entities: []string{e.ID},
			})
		}
	}
	return out
}

func nameOr(byID map[string]Entity, id string) string {
	if e, ok := byID[id]; ok && e.Name != "" {
		return e.Name
	}
	return id
}

func orDash(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
