// pr_impact_infer.go — issue #6042: infer a community for a changed entity the
// group-algo overlay cannot place, from the entities around it that it CAN.
//
// WHY THIS EXISTS. The overlay is computed from the last indexed group union, so
// an entity that a PR ADDS is absent from it by construction. #6006 made that
// state an explicit decline rather than a silent "no conflicts" — correct, but
// it turns the single most ordinary PR shape there is (one that only adds code)
// into a tool that always refuses to answer.
//
// WHAT IT MUST NOT BECOME. Inference presented as fact is the #6006 defect one
// layer up: a confident answer the caller cannot tell from a measured one. So
// every inferred placement is labelled at the entity level
// (ChangedEntity.CommunitySource) and aggregated at the verdict level
// (CommunityDataInferredOnly / InferredEntityCount), and inference that fails
// falls back to the existing decline path unchanged.
//
// ── The signals, and why these three ────────────────────────────────────────
//
// Only signals the graph actually carries at this layer were used:
//
//	container (weight 3) — the entities sharing the new entity's SourceFile, plus
//	    any CONTAINS parent. A new function in an already-placed file is in that
//	    file's community with very high confidence; a file is the smallest unit
//	    Louvain would essentially never split.
//	module (weight 2) — Properties["module"], stamped on both the full
//	    (cmd/grafel/index.go) and incremental (internal/extractors/incremental.go)
//	    paths and round-tripped through graph.fb (load.go restores the FB scalar
//	    into props). It is a depth-capped path rollup, so it is a COARSER version
//	    of the container signal rather than an independent one — hence a lower
//	    weight, and hence it can never outvote the file on its own.
//	targets (weight 1) — placed entities the new entity calls/references. This is
//	    closest to what Louvain itself would have seen. It requires at least
//	    minPlacedTargets placed targets: one outbound edge is not evidence
//	    (everything calls a logger), and a single-edge rule would place half of
//	    every new package into whatever community the nearest shared utility
//	    happens to sit in.
//
// ── The decision rule ───────────────────────────────────────────────────────
//
// Each signal casts ONE vote, for the strict plurality of its own evidence; a
// signal with no unique winner (a file split 1-1 across communities) abstains
// entirely rather than letting map order decide. A community is then inferred
// only when its weight is STRICTLY GREATER than the total weight of every
// dissenting vote. So:
//
//	container alone                     -> infer      (3 > 0)
//	container 7 vs module 9             -> infer 7    (3 > 2; container is weighted highest)
//	container 7 vs module 9 + targets 9 -> DECLINE    (3 is not > 3)
//	module alone / targets alone        -> infer      (2 > 0 / 1 > 0)
//	nothing available                   -> DECLINE
//
// ── What is deliberately NOT inferred ───────────────────────────────────────
//
//   - Overlay-placed entities. The overlay is ground truth and always wins.
//   - Removed entities. They are absent from the head graph, so they have no
//     neighbours; the diff record is not a community signal.
//   - Blast-radius entities. Only the CHANGED set drives the verdict, and
//     inferring for a potentially unbounded downstream set would cost far more
//     than it is worth.
//   - Chained inference. Only OVERLAY-placed entities vote. An inferred
//     placement is a guess, and letting guesses vote would propagate one weak
//     signal across an entire new package — and would make the result depend on
//     the order entities were processed in.
//
// ── Cost ────────────────────────────────────────────────────────────────────
//
// Nothing is built unless at least one changed entity is present in the head
// graph AND unplaced. When that holds, the added work is:
//
//	one pass over `entities`, filtered to the (small) set of files and modules
//	the unplaced changed entities live in — O(N) comparisons, allocations bounded
//	by the changed set, not by the graph; and
//	outbound/CONTAINS edges captured during the adjacency pass AnalyzePRImpact
//	already makes, and only for changed ids — O(E) comparisons, O(deg(changed))
//	memory.
//
// No BFS, no transitive walk. Next to the 31.9 MB overlay parse the handler
// already performs (memoized on path+mtime+size), this is noise.
package graph

// CommunitySource labels HOW a changed entity's community was determined.
// It is the whole point of #6042: an inferred placement that a caller cannot
// distinguish from a measured one is worse than no placement at all.
const (
	// CommunitySourceOverlay — measured. The group-algo overlay placed this
	// entity directly; it existed at the last group index.
	CommunitySourceOverlay = "overlay"
	// CommunitySourceInferred — a guess, from placed neighbours. Good enough to
	// triage merge risk with, not good enough to present as measured.
	CommunitySourceInferred = "inferred"
	// CommunitySourceNone — not placed and not inferrable. These entities are the
	// ones #6006's decline path exists for.
	CommunitySourceNone = "none"
)

const (
	inferWeightContainer = 3
	inferWeightModule    = 2
	inferWeightTargets   = 1

	// minPlacedTargets is how many placed outbound targets the weakest signal
	// needs before it votes at all. See the package comment: one edge is not
	// evidence, and this threshold is also what keeps #6006's add-only decline
	// (a new entity calling exactly one placed entity) binding.
	minPlacedTargets = 2
)

// communityInferrer holds the (small) indexes needed to place unplaced changed
// entities. Zero value infers nothing, which is what callers get when every
// changed entity was already placed.
type communityInferrer struct {
	byID map[string]Entity
	// byFile/byModule are community histograms over OVERLAY-PLACED entities,
	// restricted to the files/modules the unplaced changed entities occupy.
	byFile   map[string]map[int]int
	byModule map[string]map[int]int
	// parents[id] = CONTAINS parents of id; targets[id] = outbound edge targets of
	// id. Populated only for unplaced changed ids.
	parents map[string][]string
	targets map[string][]string
}

// newCommunityInferrer builds the indexes for `want` — the unplaced changed
// entity ids that are present in the head graph. Returns the zero inferrer when
// there is nothing to infer, so the O(N) entity pass is skipped entirely.
func newCommunityInferrer(entities []Entity, byID map[string]Entity, want map[string]struct{},
	parents, targets map[string][]string) *communityInferrer {
	if len(want) == 0 {
		return nil
	}
	// Which files/modules do we actually care about? Usually a handful.
	wantFiles := make(map[string]struct{}, len(want))
	wantModules := make(map[string]struct{}, len(want))
	for id := range want {
		e := byID[id]
		if e.SourceFile != "" {
			wantFiles[e.SourceFile] = struct{}{}
		}
		if m := e.PropGet("module"); m != "" {
			wantModules[m] = struct{}{}
		}
	}
	// Also index the files/modules of the CONTAINS parents, so a parent that
	// lives elsewhere still contributes through its own community (below we read
	// the parent's community directly, so no extra file bookkeeping is needed).
	ci := &communityInferrer{
		byID:     byID,
		byFile:   make(map[string]map[int]int, len(wantFiles)),
		byModule: make(map[string]map[int]int, len(wantModules)),
		parents:  parents,
		targets:  targets,
	}
	if len(wantFiles) == 0 && len(wantModules) == 0 && len(parents) == 0 && len(targets) == 0 {
		return ci // nothing to histogram; target/parent votes still work
	}
	for i := range entities {
		c := communityOf(entities[i])
		if c < 0 {
			continue // only OVERLAY-placed entities vote — no chained inference
		}
		if f := entities[i].SourceFile; f != "" {
			if _, ok := wantFiles[f]; ok {
				addVote(ci.byFile, f, c)
			}
		}
		if len(wantModules) > 0 {
			if m := entities[i].PropGet("module"); m != "" {
				if _, ok := wantModules[m]; ok {
					addVote(ci.byModule, m, c)
				}
			}
		}
	}
	return ci
}

func addVote(dst map[string]map[int]int, key string, community int) {
	h := dst[key]
	if h == nil {
		h = map[int]int{}
		dst[key] = h
	}
	h[community]++
}

// infer returns the community inferred for id, and whether inference succeeded.
// See the package comment for the rule; the short version is that each signal
// casts one weighted vote and the winner must strictly outweigh all dissent.
func (ci *communityInferrer) infer(id string) (int, bool) {
	if ci == nil {
		return -1, false
	}
	e, ok := ci.byID[id]
	if !ok {
		return -1, false // removed entity: no head-graph neighbours to read
	}

	votes := map[int]int{}
	if c, ok := ci.containerVote(id, e); ok {
		votes[c] += inferWeightContainer
	}
	if m := e.PropGet("module"); m != "" {
		if c, ok := plurality(ci.byModule[m], 1); ok {
			votes[c] += inferWeightModule
		}
	}
	if c, ok := ci.targetVote(id); ok {
		votes[c] += inferWeightTargets
	}
	if len(votes) == 0 {
		return -1, false
	}

	best, bestW, total := -1, 0, 0
	tied := false
	for c, w := range votes {
		total += w
		switch {
		case w > bestW:
			best, bestW, tied = c, w, false
		case w == bestW:
			tied = true
		}
	}
	// Strictly greater than the sum of all dissent: 2*bestW > total.
	if tied || 2*bestW <= total {
		return -1, false
	}
	return best, true
}

// containerVote combines the entity's own file with any CONTAINS parents — the
// "containing component" signal. Both describe the same thing (what physically
// encloses the entity) so they share one vote rather than double-counting.
func (ci *communityInferrer) containerVote(id string, e Entity) (int, bool) {
	var hist map[int]int
	if base := ci.byFile[e.SourceFile]; e.SourceFile != "" && len(base) > 0 {
		hist = make(map[int]int, len(base)+1)
		for c, n := range base {
			hist[c] = n
		}
	}
	for _, pid := range ci.parents[id] {
		p, ok := ci.byID[pid]
		if !ok {
			continue
		}
		if c := communityOf(p); c >= 0 {
			if hist == nil {
				hist = map[int]int{}
			}
			hist[c]++
		}
	}
	return plurality(hist, 1)
}

// targetVote is the plurality community of the entity's placed outbound targets,
// requiring at least minPlacedTargets of them.
func (ci *communityInferrer) targetVote(id string) (int, bool) {
	tgts := ci.targets[id]
	if len(tgts) < minPlacedTargets {
		return -1, false
	}
	hist := map[int]int{}
	placed := 0
	for _, tid := range tgts {
		t, ok := ci.byID[tid]
		if !ok {
			continue
		}
		if c := communityOf(t); c >= 0 {
			hist[c]++
			placed++
		}
	}
	if placed < minPlacedTargets {
		return -1, false
	}
	return plurality(hist, minPlacedTargets)
}

// plurality returns the uniquely most-common community in hist, provided it has
// at least `min` votes. A tie ABSTAINS: with no unique winner the answer would
// otherwise be decided by map iteration order, which is both non-deterministic
// and, worse, an invented placement.
func plurality(hist map[int]int, min int) (int, bool) {
	best, bestN := -1, 0
	tied := false
	for c, n := range hist {
		switch {
		case n > bestN:
			best, bestN, tied = c, n, false
		case n == bestN:
			tied = true
		}
	}
	if best < 0 || bestN < min || tied {
		return -1, false
	}
	return best, true
}
