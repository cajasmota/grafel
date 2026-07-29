// pr_impact_infer.go — issue #6042: infer a community for a changed entity the
// group-algo partition has NEVER SEEN, from the entities around it that it has.
//
// WHY THIS EXISTS. The overlay is computed from the last indexed group union, so
// an entity that a PR ADDS is absent from it by construction. #6006 made that
// state an explicit decline rather than a silent "no conflicts" — correct, but
// it turns the single most ordinary PR shape there is (one that only adds code)
// into a tool that always refuses to answer.
//
// WHAT IT MUST NOT BECOME. Inference presented as fact is the #6006 defect one
// layer up: a confident answer the caller cannot tell from a measured one. So
// every inferred placement is labelled per entity (CommunitySource), carries its
// own margin (CommunityInference), is tracked per community (ImpactedCommunity.
// InferredOnly) and per merge-risk pair (MergeRiskPair.InferredOnly), and
// inference that fails falls back to the existing decline path unchanged.
//
// ── Who is a candidate ──────────────────────────────────────────────────────
//
// ONLY entities ABSENT from the overlay — Entity.CommunityID == nil. This is not
// the same as communityOf(e) < 0, and the difference matters:
//
//	nil                     the partition has never seen this entity: it did not
//	                        exist at the last group index. THIS is #6042.
//	non-nil, negative (-2)  the partition DID see it and declined to place it
//	                        (groupalgo writes -2 for "not assigned a community";
//	                        legacy graph.json can carry -1 directly).
//
// Inferring for the second class would override the group algorithm's own
// decision with a path-prefix heuristic — a strictly worse answer than the one
// community detection already gave, wearing a confident label. See
// TestAnalyzePRImpact_NegativeCommunityIDIsNotInferred.
//
// ── The signals, and why these three ────────────────────────────────────────
//
//	container (primary) — the placed entities sharing the new entity's
//	    SourceFile, plus any placed CONTAINS parent. A file is the smallest unit
//	    community detection would essentially never split, so a new function in
//	    an already-placed file is in that file's community with high confidence.
//
//	module (fallback) — Properties["module"], stamped on both the full
//	    (cmd/grafel/index.go) and incremental (extractors/incremental.go) paths
//	    and round-tripped through graph.fb (load.go restores the FB scalar into
//	    props). It votes ONLY when the container abstains, because it is not an
//	    independent signal: module is module.Derive(SourceFile), a pure function
//	    of the path, so the module histogram is a strict SUPERSET of the file
//	    histogram. Letting both vote would count one measurement twice, and a
//	    file-vs-module disagreement is never "two sources disagree" — it is "my
//	    file leans X while the wider directory leans Y", where the file is
//	    strictly the more specific evidence.
//
//	    module.Derive is also weak on its own: MarkerFileNames has no per-Go-
//	    package marker, so a single-module Go repo falls through to DefaultDepth
//	    and everything under (say) internal/graph/** shares one label. A bare
//	    plurality over such a bucket (26 of 51) is not a community signal, so the
//	    module vote additionally requires a real sample AND real concentration.
//
//	call targets — placed entities the new entity CALLS/USES/EXTENDS/…, over an
//	    ALLOWLIST of edge kinds (inferTargetKinds). This is the only signal not
//	    derived from the file path, and the closest to what community detection
//	    itself would have seen. IMPORTS is excluded: importing two placed
//	    packages is universal, so it would clear the >= 2 threshold for
//	    essentially every new file while carrying almost no community
//	    information. CONTAINS is excluded because it IS the container signal and
//	    must not vote twice under a second name.
//
// ── The decision rule: unanimity ────────────────────────────────────────────
//
// At most two votes are ever cast — the container-or-module primary, and the
// call targets — and BOTH must agree:
//
//	primary only             -> infer
//	targets only             -> infer
//	primary + targets agree  -> infer (the strongest case)
//	primary + targets differ -> DECLINE
//	nothing votes            -> DECLINE
//
// This is the issue's own rule ("no inference at all when signals disagree"),
// and it is deliberately arithmetic-free. An earlier cut scored the signals
// 3/2/1 and required the winner to outweigh all dissent; once module is
// subordinate to the container that comparison can never actually block
// anything, so it was a guard held by nothing — exactly the kind of untested
// branch this feature must not ship. Each signal also abstains internally when
// its own evidence has no unique winner (a file split 1-1), rather than letting
// map iteration order decide.
//
// ── What is deliberately NOT inferred ───────────────────────────────────────
//
//   - Entities the overlay placed (it is ground truth) or explicitly declined to
//     place (see "Who is a candidate").
//   - Removed entities: absent from the head graph, so they have no neighbours;
//     the diff record is not a community signal.
//   - Blast-radius entities: only the CHANGED set drives the verdict, and
//     inferring for an unbounded downstream set would cost far more than it is
//     worth.
//   - Chained inference. Only OVERLAY-placed entities vote. An inferred
//     placement is a guess; letting guesses vote would propagate one weak signal
//     across an entire new package and make the result depend on processing
//     order.
//
// ── Cost ────────────────────────────────────────────────────────────────────
//
// Nothing is built unless a changed entity is present in the head graph AND
// absent from the overlay. Then: one pass over `entities` filtered to the small
// set of files and modules those entities occupy, plus outbound/CONTAINS edges
// captured during the adjacency pass AnalyzePRImpact already makes, for changed
// ids only. No BFS, no transitive walk. See BenchmarkAnalyzePRImpact_Inference,
// whose fixture ASSERTS that inference actually fires before timing it.
package graph

// CommunitySource labels HOW a changed entity's community was determined. It is
// the whole point of #6042: an inferred placement a caller cannot distinguish
// from a measured one is worse than no placement at all.
const (
	// CommunitySourceOverlay — measured. The group-algo overlay placed this
	// entity directly; it existed at the last group index.
	CommunitySourceOverlay = "overlay"
	// CommunitySourceInferred — deduced from placed neighbours. Good enough to
	// triage merge risk with, not good enough to present as measured.
	CommunitySourceInferred = "inferred"
	// CommunitySourceNone — not placed and not inferrable. These entities are
	// what #6006's decline path exists for.
	CommunitySourceNone = "none"
)

// Signal names as they appear in CommunityInference.Signals.
const (
	inferSignalContainer = "container"
	inferSignalModule    = "module"
	inferSignalTargets   = "call_targets"
)

const (
	// minPlacedTargets is how many placed outbound targets the call-target signal
	// needs before it votes. One edge is not evidence, and this threshold is also
	// what keeps #6006's add-only decline (a new entity calling exactly one placed
	// entity) binding.
	minPlacedTargets = 2
	// minModuleSample / minModuleConcentration bound the module fallback: at
	// least this many placed entities in the module, and at least this fraction
	// of them in the winning community. A 26/51 plurality over a depth-capped
	// path bucket is not a community signal; 40/41 is.
	minModuleSample        = 3
	minModuleConcentration = 0.7
)

// inferTargetKinds is the allowlist of edge kinds the call-target signal reads.
// Containment edges (CONTAINS — that is the container signal) and package-level
// edges (IMPORTS, DEPENDS_ON — universal, and already what the module signal
// measures) are excluded on purpose; see the package comment.
var inferTargetKinds = map[string]struct{}{
	"CALLS":         {},
	"REFERENCES":    {},
	"USES":          {},
	"USES_HOOK":     {},
	"EXTENDS":       {},
	"IMPLEMENTS":    {},
	"INJECTED_INTO": {},
	"RETURNS":       {},
	"ACCEPTS_INPUT": {},
}

// isInferTargetKind reports whether an edge kind carries community signal for
// the call-target vote.
func isInferTargetKind(kind string) bool {
	_, ok := inferTargetKinds[kind]
	return ok
}

// overlayAbsent reports whether the group-algo partition has never seen this
// entity — the only class #6042 infers for. See the package comment.
func overlayAbsent(e Entity) bool { return e.CommunityID == nil }

// CommunityInference is the provenance of ONE inferred placement: which signals
// voted, and how strong the deciding signal's evidence was. Without the margin a
// 26-of-51 plurality and a 40-of-41 consensus are indistinguishable on the wire,
// and an agent cannot weigh them differently.
type CommunityInference struct {
	// Signals that voted, primary first. Two entries means the container-or-module
	// primary and the call targets independently agreed — the strongest case.
	Signals []string `json:"signals"`
	// Support / Sample are the deciding signal's placed neighbours backing the
	// chosen community, out of those it considered.
	Support int `json:"support"`
	Sample  int `json:"sample"`
}

// communityInferrer holds the (small) indexes needed to place overlay-absent
// changed entities. A nil inferrer infers nothing, which is what callers get
// when every changed entity was already placed — or explicitly declined — by the
// partition.
type communityInferrer struct {
	byID map[string]Entity
	// byFile/byModule are community histograms over OVERLAY-PLACED entities,
	// restricted to the files/modules the candidate entities occupy.
	byFile   map[string]map[int]int
	byModule map[string]map[int]int
	// parents[id] = CONTAINS parents of id; targets[id] = allowlisted outbound
	// edge targets of id. Populated only for candidate ids.
	parents map[string][]string
	targets map[string][]string
}

// newCommunityInferrer builds the indexes for `want` — the overlay-absent
// changed entity ids present in the head graph. Returns nil when there is
// nothing to infer, so the O(N) entity pass is skipped entirely.
func newCommunityInferrer(entities []Entity, byID map[string]Entity, want map[string]struct{},
	parents, targets map[string][]string) *communityInferrer {
	if len(want) == 0 {
		return nil
	}
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
	ci := &communityInferrer{
		byID:     byID,
		byFile:   make(map[string]map[int]int, len(wantFiles)),
		byModule: make(map[string]map[int]int, len(wantModules)),
		parents:  parents,
		targets:  targets,
	}
	if len(wantFiles) == 0 && len(wantModules) == 0 {
		return ci // no path signals possible; target/parent votes still work
	}
	for i := range entities {
		c := communityOf(entities[i])
		if c < 0 {
			continue // only OVERLAY-PLACED entities vote — no chained inference
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

// signalVote is one signal's opinion, with the evidence behind it.
type signalVote struct {
	name      string
	community int
	support   int
	sample    int
}

// infer returns the inferred community for id and its provenance. Unanimity: the
// container-or-module primary and the call targets must not contradict each
// other, and at least one must vote. See the package comment.
func (ci *communityInferrer) infer(id string) (int, *CommunityInference, bool) {
	if ci == nil {
		return -1, nil, false
	}
	e, ok := ci.byID[id]
	if !ok {
		return -1, nil, false // removed entity: no head-graph neighbours to read
	}

	// Primary: the containing component, falling back to the module ONLY when the
	// container has NO EVIDENCE — module is a coarser view of the same path, so
	// the two must never both vote.
	//
	// The distinction between "no evidence" and "contradictory evidence" is
	// load-bearing. A file split 1-1 across two communities has spoken: this
	// location is genuinely ambiguous. Falling through to the module then asks a
	// SUPERSET of that same evidence — the file's entities are inside the
	// module's sample — and gets a confident answer purely because the wider
	// bucket dilutes the contradiction. That is manufacturing agreement.
	primary, hasPrimary, containerHadEvidence := ci.containerVote(id, e)
	switch {
	case hasPrimary:
	case containerHadEvidence:
		return -1, nil, false // the container looked and found a contradiction
	default:
		primary, hasPrimary = ci.moduleVote(e)
	}
	targets, hasTargets := ci.targetVote(id)

	switch {
	case hasPrimary && hasTargets:
		if primary.community != targets.community {
			return -1, nil, false // signals disagree — decline rather than guess
		}
		return primary.community, &CommunityInference{
			Signals: []string{primary.name, targets.name},
			Support: primary.support,
			Sample:  primary.sample,
		}, true
	case hasPrimary:
		return primary.community, &CommunityInference{
			Signals: []string{primary.name}, Support: primary.support, Sample: primary.sample,
		}, true
	case hasTargets:
		return targets.community, &CommunityInference{
			Signals: []string{targets.name}, Support: targets.support, Sample: targets.sample,
		}, true
	}
	return -1, nil, false
}

// containerVote combines the entity's own file with any CONTAINS parents — both
// describe what physically encloses the entity, so they share one vote.
//
// The third return says whether the container had ANY placed evidence to look
// at, which is what lets infer() tell "the file is new" (fall through to the
// module) from "the file is contradictory" (decline outright).
func (ci *communityInferrer) containerVote(id string, e Entity) (signalVote, bool, bool) {
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
	c, support, sample, ok := plurality(hist, 1)
	return signalVote{inferSignalContainer, c, support, sample}, ok, len(hist) > 0
}

// moduleVote is the fallback prior. It demands a real sample AND real
// concentration, because module is a depth-capped path rollup that can cover a
// large, heterogeneous slice of a repo — a bare plurality over such a bucket is
// noise wearing a community id.
func (ci *communityInferrer) moduleVote(e Entity) (signalVote, bool) {
	m := e.PropGet("module")
	if m == "" {
		return signalVote{}, false
	}
	c, support, sample, ok := plurality(ci.byModule[m], minModuleSample)
	if !ok {
		return signalVote{}, false
	}
	if float64(support)/float64(sample) < minModuleConcentration {
		return signalVote{}, false
	}
	return signalVote{inferSignalModule, c, support, sample}, true
}

// targetVote is the plurality community of the entity's placed outbound targets
// over the allowlisted edge kinds, requiring at least minPlacedTargets of them.
func (ci *communityInferrer) targetVote(id string) (signalVote, bool) {
	tgts := ci.targets[id]
	if len(tgts) < minPlacedTargets {
		return signalVote{}, false
	}
	hist := map[int]int{}
	for _, tid := range tgts {
		t, ok := ci.byID[tid]
		if !ok {
			continue
		}
		if c := communityOf(t); c >= 0 {
			hist[c]++
		}
	}
	c, support, sample, ok := plurality(hist, minPlacedTargets)
	return signalVote{inferSignalTargets, c, support, sample}, ok
}

// plurality returns the uniquely most-common community in hist, with its support
// and the total sample, provided the sample is at least minSample. A tie
// ABSTAINS: with no unique winner the answer would be decided by map iteration
// order — non-deterministic, and an invented placement.
func plurality(hist map[int]int, minSample int) (community, support, sample int, ok bool) {
	best, bestN, total := -1, 0, 0
	tied := false
	for c, n := range hist {
		total += n
		switch {
		case n > bestN:
			best, bestN, tied = c, n, false
		case n == bestN:
			tied = true
		}
	}
	if best < 0 || total < minSample || tied {
		return -1, 0, 0, false
	}
	return best, bestN, total, true
}
