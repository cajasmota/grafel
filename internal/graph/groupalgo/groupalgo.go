// Package groupalgo assembles the union of a group's per-repo graphs and runs
// the graph algorithm pass (Louvain communities + PageRank/Betweenness
// centrality) ONCE at group scope, rather than per-repo.
//
// Motivation (#5349, epic #5350): grafel computes communities + centrality
// per-repo today (cmd/grafel/index.go Pass 4). For a multi-repo group that is
// the wrong scope — the algorithms never see cross-repo edges, so the stored
// community-ids and centrality scores are per-repo fragments. A backend
// AuthService called by 40 frontend modules has huge *cross-repo* PageRank,
// but per-repo computation never sees those inbound edges and under-ranks it.
//
// This package (Part A1) is the FOUNDATION: pure assembly + a single algorithm
// pass over the union. It does NOT schedule, persist, or swap an overlay (A2
// adds storage; A3 adds the debounced/capped/background scheduler).
//
// Cross-repo phantom CALLS edges are ALREADY written into each repo's graph.fb
// by the P5 link pass (internal/cli/links.go runPhantomEdgePass). Entity IDs
// are group-unique (slug-qualified — that is how phantom edges resolve across
// repos). So the union is plain concatenation of each repo's entities +
// relationships; no link re-derivation is needed (decision Q4 in the plan).
package groupalgo

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// memoMu guards the process-local compute-once guard below. All access to the
// shared maps goes through it.
var (
	memoMu      sync.Mutex
	memoHashes  = map[string]string{}                  // memoKey -> input hash of the last full compute
	memoResults = map[string]*graph.AlgorithmResults{} // memoKey -> that full compute's result (read-only after store)
)

// memoKeyFor returns the process-local guard key for a group. It keys on the
// resolved overlay path (which embeds GRAFEL_HOME) so distinct daemons / test
// homes never collide, falling back to the bare group name if the path cannot
// be resolved.
func memoKeyFor(group string) string {
	if p, err := OverlayPath(group); err == nil && p != "" {
		return p
	}
	return "group:" + group
}

// loadMemoizedGroupResult returns the last full-computed result for group IFF it
// was computed against the SAME group-version (inputHash). The returned pointer
// is shared and MUST be treated read-only by callers (same contract as the disk
// overlay reconstitution — consumers only read it).
func loadMemoizedGroupResult(group, inputHash string) (*graph.AlgorithmResults, bool) {
	memoMu.Lock()
	defer memoMu.Unlock()
	k := memoKeyFor(group)
	if memoHashes[k] == inputHash {
		if res, ok := memoResults[k]; ok && res != nil {
			return res, true
		}
	}
	return nil, false
}

// storeMemoizedGroupResult records that group's inputHash version has been fully
// computed this process, keeping the result so a later reload for the SAME
// version can reuse it even if the disk overlay never persisted. Recorded BEFORE
// the caller attempts to write the overlay, so a persist failure cannot reopen
// the recompute→persist-fail→recompute spin.
func storeMemoizedGroupResult(group, inputHash string, res *graph.AlgorithmResults) {
	if res == nil {
		return
	}
	memoMu.Lock()
	defer memoMu.Unlock()
	k := memoKeyFor(group)
	memoHashes[k] = inputHash
	memoResults[k] = res
}

// resetGroupAlgoMemo clears the process-local guard. Test-only seam so cases can
// assert first-compute behaviour deterministically.
func resetGroupAlgoMemo() {
	memoMu.Lock()
	defer memoMu.Unlock()
	memoHashes = map[string]string{}
	memoResults = map[string]*graph.AlgorithmResults{}
}

// GroupAlgoResult wraps the single group-scope algorithm pass.
//
// Results holds the per-entity + corpus-level outputs (community_id, pagerank,
// betweenness centrality, god-nodes, articulation points, the community
// summary, and stats). It is nil for an empty group.
//
// EntityRepo maps each entity ID to the slug of the repo it came from, so
// consumers (and the dry-run printer) can attribute a centrality hub or a
// community to its source repo, and detect communities that SPAN multiple
// repos. SourceMtimes records each repo's graph.fb mtime (unix nanoseconds) at
// assembly time — A2 uses this for overlay staleness; A1 just records it.
type GroupAlgoResult struct {
	Group        string
	Results      *graph.AlgorithmResults
	EntityRepo   map[string]string // entity id -> repo slug
	SourceMtimes map[string]int64  // repo slug -> graph.fb mtime (unix nanos)
	NumEntities  int
	NumRels      int
	NumRepos     int

	// InputHash is the content hash of the community-relevant input graph of the
	// assembled union (graph.CommunityInputHash). Because the Pass-4 pass is
	// deterministic, two unions with the same InputHash produce byte-identical
	// Results — this is the gate the incremental path uses to SKIP a recompute
	// when a reindex left the community graph unchanged (#5309 layer 4).
	InputHash string

	// Skipped is true when RunGroupAlgorithmsIncremental preserved a prior
	// overlay verbatim instead of recomputing (the input graph was unchanged).
	// Informational; the caller still writes the (unchanged) overlay so its
	// source_mtimes are refreshed.
	Skipped bool
}

// resolveGroup looks up a group by name and loads its config.
func resolveGroup(group string) (*registry.GroupConfig, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return nil, fmt.Errorf("unknown group: %s", group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// unionCapacityHint pre-computes the exact size of the union about to be
// assembled, from each repo's PERSISTED header counts (#5909, refs #5954).
//
// Why: the assembly below appends entities one at a time and relationships
// repo-by-repo. An unpresized append grows by doubling, so at the copy instant
// the process transiently holds BOTH the old backing array and a new one up to
// ~2x its size — for the real corpus (427k entities) that copy is the single
// largest allocation in the group-algo child. Presizing removes every growth
// copy: one allocation of the final size, no transient double.
//
// graph.PersistedStatsFromDir is a header read (fbreader/MultiReader entity +
// relationship counts) — no entity materialization — the same cheap pre-pass
// cmd/grafel/daemon.go:767 uses for SchedulerEntityCount.
//
// MISSING/ZERO STATS ARE CONTRIBUTED AS ZERO, deliberately. A repo that has no
// readable graph descriptor (never indexed, or graph.json-only — the JSON
// fallback carries no header counts) is simply not counted, so it makes the hint
// an UNDER-estimate and the remaining share is filled by ordinary append growth
// from a truthful base. That is the safe direction: a hint that OVER-estimates
// would allocate a union larger than the union, which is exactly the hazard this
// change exists to remove. For the same reason nothing is ever guessed, rounded
// up, or extrapolated from a sibling repo.
//
// The one way the hint can go WRONG IN EITHER DIRECTION is the TOCTOU window
// between this header pre-pass and each repo's LoadGraphFromDir below: a repo
// re-indexed LARGER in that window under-counts, and one re-indexed SMALLER
// over-counts. Both are bounded by that single repo's own delta, both are
// transient, and neither has any correctness effect — an under-count is
// absorbed by append growth, an over-count leaves the union slice with spare
// capacity for its (short) lifetime. Nothing is re-read or reconciled to close
// the window: a second header pass would only move it, and the group union is
// discarded at the end of the pass either way.
func unionCapacityHint(cfg *registry.GroupConfig) (entCap, relCap int) {
	if cfg == nil {
		return 0, 0
	}
	for _, r := range cfg.Repos {
		ps, ok := graph.PersistedStatsFromDir(daemon.StateDirForRepo(r.Path))
		if !ok {
			continue
		}
		if ps.Entities > 0 {
			entCap += ps.Entities
		}
		if ps.Relationships > 0 {
			relCap += ps.Relationships
		}
	}
	return entCap, relCap
}

// AssembleGroupGraph loads every repo's graph.fb and concatenates entities +
// relationships into a single in-memory group graph. The cross-repo phantom
// CALLS edges are already present in each repo's graph.fb (post-P5), so the
// union is a plain concatenation — no link re-derivation.
//
// Returns:
//   - entities: union of every repo's doc.Entities (IDs are group-unique).
//   - rels:     union of every repo's doc.Relationships (includes the phantom
//     cross-repo CALLS edges injected by the link pass).
//   - entityRepo: entity id -> repo slug (for attribution).
//   - srcMtimes: repo slug -> graph.fb mtime in unix nanoseconds.
//
// A repo whose graph.fb is missing (never indexed) is skipped, not an error —
// the union of the remaining repos is still valid. An unknown group is an
// error. An empty group yields empty (non-nil) slices and maps.
func AssembleGroupGraph(group string) (entities []graph.Entity, rels []graph.Relationship, entityRepo map[string]string, srcMtimes map[string]int64, err error) {
	cfg, err := resolveGroup(group)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Presize from the persisted per-repo header counts (#5909). Under-estimates
	// are safe (append growth covers the rest); over-estimates are the hazard, so
	// unionCapacityHint never guesses for a repo it cannot read. cap==0 degrades
	// to exactly the previous behaviour.
	entCap, relCap := unionCapacityHint(cfg)
	entities = make([]graph.Entity, 0, entCap)
	rels = make([]graph.Relationship, 0, relCap)
	entityRepo = make(map[string]string, entCap)
	srcMtimes = map[string]int64{}

	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)

		// Record the graph mtime when present. A repo that was never indexed
		// has neither a graph nor graph.json — skip it (not an error): the
		// union of the remaining repos is still valid.
		// #5891: resolve the active generation so the recorded mtime is the
		// gen file's — otherwise the overlay staleness check below (which reads
		// the same resolved mtime via CurrentSourceMtimes) would see a frozen
		// legacy graph.fb mtime and treat a fresh overlay as permanently stale.
		// #5915 J2 P2: graphSourceMtime is segment-set aware — a segment-set
		// repo (graph.<gen>/ dir + manifest.json, no flat .fb) would otherwise
		// stat the absent flat path and be wrongly treated as never-indexed.
		jsonPath := filepath.Join(stateDir, "graph.json")
		fbExists := false
		if mt, ok := graphSourceMtime(stateDir); ok {
			srcMtimes[r.Slug] = mt
			fbExists = true
		}
		if !fbExists {
			if _, statErr := os.Stat(jsonPath); statErr != nil {
				// Neither artifact present — repo not yet indexed; skip.
				continue
			}
		}

		doc, lerr := graph.LoadGraphFromDir(stateDir)
		if lerr != nil {
			return nil, nil, nil, nil, fmt.Errorf("load graph for repo %q (%s): %w", r.Slug, r.Path, lerr)
		}
		if doc == nil {
			continue
		}
		for i := range doc.Entities {
			entities = append(entities, doc.Entities[i])
			entityRepo[doc.Entities[i].ID] = r.Slug
		}
		rels = append(rels, doc.Relationships...)
	}

	return entities, rels, entityRepo, srcMtimes, nil
}

// RunGroupAlgorithms assembles the group union and runs graph.RunAlgorithms
// ONCE over it. This is the A1 deliverable: a single algorithm pass at group
// scope so cross-repo edges are finally seen by communities + centrality.
//
// An empty group (no entities across any repo) returns a non-nil result whose
// Results is an empty AlgorithmResults (graph.RunAlgorithms guards len==0), so
// callers get a safe no-op rather than a panic.
func RunGroupAlgorithms(group string) (*GroupAlgoResult, error) {
	SetPhase(PhaseAssembling)
	entities, rels, entityRepo, srcMtimes, err := AssembleGroupGraph(group)
	if err != nil {
		return nil, err
	}

	cfg, _ := resolveGroup(group) // already validated above; ignore re-lookup error
	numRepos := 0
	if cfg != nil {
		numRepos = len(cfg.Repos)
	}

	// Hash BEFORE running the algorithms, matching RunGroupAlgorithmsIncremental.
	// inputHash is a pure function of (entities, rels) and has no dependency on
	// res, so the order is free — and one true phase order across both
	// entrypoints is worth more than a comment explaining two. The phase labels
	// are the operator-facing contract for reading a memtrace, in a workstream
	// whose premise is that a misread memory metric already cost a day.
	SetPhase(PhaseHashing)
	inputHash := graph.CommunityInputHash(entities, rels)

	SetPhase(PhaseRunningAlgorithms)
	res := graph.RunAlgorithms(entities, rels)

	return &GroupAlgoResult{
		Group:        group,
		Results:      res,
		EntityRepo:   entityRepo,
		SourceMtimes: srcMtimes,
		NumEntities:  len(entities),
		NumRels:      len(rels),
		NumRepos:     numRepos,
		InputHash:    inputHash,
	}, nil
}

// RunGroupAlgorithmsIncremental is the incremental (#5309 layer 4) entrypoint
// for the group-scope Pass-4 sweep. It assembles the group union (cheap), then:
//
//   - if a prior <group>-algo.json overlay exists AND its recorded input_hash
//     equals the freshly assembled union's CommunityInputHash, the community
//     graph is UNCHANGED. Because the pass is deterministic, a full recompute
//     would reproduce the existing overlay byte-for-byte — so the recompute is
//     SKIPPED and the prior overlay is reconstituted into a GroupAlgoResult
//     verbatim (community ids, PageRank, centrality, flags, communities, stats
//     all preserved). Only source_mtimes are refreshed when the caller writes
//     it back, settling the staleness gate. This is the ~zero-cost path a
//     docs-only / comment-only / config-only push takes.
//
//   - otherwise (no overlay, stale/corrupt overlay, or a changed input hash —
//     a node added/removed or any community-graph edge changed), it falls
//     through to a full deterministic RunGroupAlgorithms. This is identical to
//     the prior behaviour and is CPU-bounded by the daemon-wide reindex ceiling
//     (#5602).
//
// The result is ALWAYS strictly equivalent to a full RunGroupAlgorithms over
// the same end-state union: the skip branch is only taken when the input that
// fully determines the deterministic output is identical, so there is no
// partition drift or label relabel. (A blast-radius-local community update is
// deliberately NOT attempted: global integer community labels are a function of
// the whole union and a partial pass could not reproduce the exact same labels
// a full pass assigns — that would break strict parity.)
func RunGroupAlgorithmsIncremental(group string) (*GroupAlgoResult, error) {
	return runGroupAlgorithmsIncremental(group, true)
}

// RunGroupAlgorithmsIncrementalOneShot is RunGroupAlgorithmsIncremental for a
// process that computes ONCE and then exits — the `grafel group-algo --write`
// child the daemon's scheduler forks (#5909, refs #5954).
//
// It is identical in every observable way (same union, same input_hash, same
// deterministic results, same overlay) except that it does NOT populate the
// process-local memo. The memo exists so a LONG-LIVED daemon does not re-run
// Louvain + PageRank + O(V*E) betweenness for a group-version it already
// computed, and so a failed overlay persist cannot reopen the
// recompute->persist-fail->recompute spin. Neither property can ever be
// exercised by a process that is about to exit: for the one-shot child the memo
// is pure retention — a SECOND live reference to the PageRank / community /
// centrality maps over the whole union, pinned across the entire
// WriteOverlayFromResult window, which is precisely when the child is already
// at its heap peak (running_algorithms measured at 1433 MB live / 1983 MB RSS
// on the 427k-entity corpus, #5992).
//
// NOTE ON #5909's FRAMING: the issue says the retention is "on the in-process
// path (GRAFEL_SUBPROCESS_INDEXER=0)". That is inverted. storeMemoizedGroupResult
// was called UNCONDITIONALLY from the single incremental entrypoint, so the
// subprocess child paid the retention too — and the child is the process where
// it can never pay for itself. The in-process/daemon path keeps the memo
// unchanged; it is the one caller for which the memo is the point.
func RunGroupAlgorithmsIncrementalOneShot(group string) (*GroupAlgoResult, error) {
	return runGroupAlgorithmsIncremental(group, false)
}

// runGroupAlgorithmsIncremental is the shared body. memoize selects whether a
// full recompute is recorded in the process-local guard: true for the
// long-lived daemon (see RunGroupAlgorithmsIncremental), false for the one-shot
// child (see RunGroupAlgorithmsIncrementalOneShot). It affects RETENTION only —
// never the union, the hash, the results, or the overlay.
func runGroupAlgorithmsIncremental(group string, memoize bool) (*GroupAlgoResult, error) {
	// Phase stamps (#5954). The group-algo child is one of the two largest
	// processes on the machine at the whole-machine memory peak and had zero
	// instrumentation; memtrace polls CurrentPhase on a ticker so each memstats
	// sample and each per-phase heap profile is attributable to a stage.
	SetPhase(PhaseAssembling)
	entities, rels, entityRepo, srcMtimes, err := AssembleGroupGraph(group)
	if err != nil {
		return nil, err
	}

	cfg, _ := resolveGroup(group)
	numRepos := 0
	if cfg != nil {
		numRepos = len(cfg.Repos)
	}

	SetPhase(PhaseHashing)
	inputHash := graph.CommunityInputHash(entities, rels)

	// Skip-when-unaffected: a prior overlay whose recorded input hash matches the
	// freshly assembled union is, by determinism, exactly what a full recompute
	// would produce. Reconstitute it instead of re-running Louvain+PageRank.
	if path, perr := OverlayPath(group); perr == nil && path != "" {
		// OverlayAlgoVersionCurrent is part of the skip condition, not just the
		// hash: the hash covers the INPUT, the version covers the FUNCTION
		// applied to it. Without it, an upgrade that changes the partitioning
		// hits this memo on an unchanged union and reconstitutes the OLD
		// implementation's overlay indefinitely. See OverlayAlgoVersion.
		if prior := readOverlayUnconditional(path); prior != nil && OverlayAlgoVersionCurrent(prior) &&
			prior.InputHash != "" && prior.InputHash == inputHash {
			res := overlayToResults(prior, entities)
			return &GroupAlgoResult{
				Group:        group,
				Results:      res,
				EntityRepo:   entityRepo,
				SourceMtimes: srcMtimes,
				NumEntities:  len(entities),
				NumRels:      len(rels),
				NumRepos:     numRepos,
				InputHash:    inputHash,
				Skipped:      true,
			}, nil
		}
	}

	// Process-local compute-once guard. The disk overlay skip above is the fast
	// path, but it only fires when the overlay could be PERSISTED. If the overlay
	// is absent because a prior WriteOverlayFromResult FAILED (read-only
	// ~/.grafel/groups, disk-full, EPERM) — the "sidecars=0" symptom — the disk
	// skip can never engage, and without this guard every trigger re-ran the full
	// ~O(V·E) betweenness over the whole group union, pinning the daemon (the
	// group-scope analog of the per-repo #50 compute→evict spin). This guard makes
	// the heavy pass run at most once per group-version in a process regardless of
	// whether the overlay reached disk; a real re-index bumps the input hash and
	// falls through to exactly one recompute (correctness preserved).
	//
	// VERSION SAFETY. This memo is keyed on (group, inputHash) with NO
	// OverlayAlgoVersion component, unlike the disk skip above. That is safe for
	// exactly one reason: OverlayAlgoVersion is a compile-time constant and the
	// memo is process-local, so every entry was necessarily produced by the
	// running binary — a hit can never be another version's result. If this memo
	// is ever persisted across restarts, or shared between processes, that
	// argument evaporates and the key MUST gain the version, or an upgraded
	// daemon will serve the previous implementation's partition from cache.
	if res, ok := loadMemoizedGroupResult(group, inputHash); ok {
		return &GroupAlgoResult{
			Group:        group,
			Results:      res,
			EntityRepo:   entityRepo,
			SourceMtimes: srcMtimes,
			NumEntities:  len(entities),
			NumRels:      len(rels),
			NumRepos:     numRepos,
			InputHash:    inputHash,
			Skipped:      true,
		}, nil
	}

	// Full deterministic recompute (input changed, or no usable prior overlay).
	SetPhase(PhaseRunningAlgorithms)
	res := graph.RunAlgorithms(entities, rels)
	// Record BEFORE returning (and thus before the caller's overlay write), so a
	// persist failure cannot cause a re-run for this same version. Skipped for
	// the one-shot child, which exits before any second run could read it — see
	// RunGroupAlgorithmsIncrementalOneShot.
	if memoize {
		storeMemoizedGroupResult(group, inputHash, res)
	}
	return &GroupAlgoResult{
		Group:        group,
		Results:      res,
		EntityRepo:   entityRepo,
		SourceMtimes: srcMtimes,
		NumEntities:  len(entities),
		NumRels:      len(rels),
		NumRepos:     numRepos,
		InputHash:    inputHash,
	}, nil
}

// graphSourceMtime resolves a repo's freshness mtime for its current-ref
// stateDir, segment-set aware (#5915 J2 P2). os.Stat(graph.CurrentGraphPath(
// stateDir)) — the pattern this replaces — only ever names a flat .fb path,
// which is ABSENT for a segment-set repo (graph.<gen>/ dir + manifest.json,
// no flat .fb), so that stat would report the repo as never-indexed and
// silently drop it from the union / freeze its overlay-staleness signal.
//
// Delegates to graph.CurrentGraphMtime (#5915 J2 slice-3), the shared
// descriptor-mtime resolution promoted to internal/graph so every other
// flat-.fb-only mtime gate (internal/daemon/deadref.go, internal/daemon/algo,
// internal/cli/branches.go) can reuse it instead of duplicating this
// descriptor branch.
func graphSourceMtime(stateDir string) (mtimeNanos int64, ok bool) {
	mt, ok := graph.CurrentGraphMtime(stateDir)
	if !ok {
		return 0, false
	}
	return mt.UnixNano(), true
}
