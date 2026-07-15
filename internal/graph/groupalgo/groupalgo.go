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

	entities = []graph.Entity{}
	rels = []graph.Relationship{}
	entityRepo = map[string]string{}
	srcMtimes = map[string]int64{}

	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)

		// Record the graph.fb mtime when present. A repo that was never indexed
		// has neither graph.fb nor graph.json — skip it (not an error): the
		// union of the remaining repos is still valid.
		// #5891: resolve the active generation so the recorded mtime is the
		// gen file's — otherwise the overlay staleness check below (which reads
		// the same resolved mtime via CurrentSourceMtimes) would see a frozen
		// legacy graph.fb mtime and treat a fresh overlay as permanently stale.
		fbPath := graph.CurrentGraphPath(stateDir)
		jsonPath := filepath.Join(stateDir, "graph.json")
		fbExists := false
		if fi, statErr := os.Stat(fbPath); statErr == nil {
			srcMtimes[r.Slug] = fi.ModTime().UnixNano()
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
	entities, rels, entityRepo, srcMtimes, err := AssembleGroupGraph(group)
	if err != nil {
		return nil, err
	}

	cfg, _ := resolveGroup(group) // already validated above; ignore re-lookup error
	numRepos := 0
	if cfg != nil {
		numRepos = len(cfg.Repos)
	}

	res := graph.RunAlgorithms(entities, rels)

	return &GroupAlgoResult{
		Group:        group,
		Results:      res,
		EntityRepo:   entityRepo,
		SourceMtimes: srcMtimes,
		NumEntities:  len(entities),
		NumRels:      len(rels),
		NumRepos:     numRepos,
		InputHash:    graph.CommunityInputHash(entities, rels),
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
	entities, rels, entityRepo, srcMtimes, err := AssembleGroupGraph(group)
	if err != nil {
		return nil, err
	}

	cfg, _ := resolveGroup(group)
	numRepos := 0
	if cfg != nil {
		numRepos = len(cfg.Repos)
	}

	inputHash := graph.CommunityInputHash(entities, rels)

	// Skip-when-unaffected: a prior overlay whose recorded input hash matches the
	// freshly assembled union is, by determinism, exactly what a full recompute
	// would produce. Reconstitute it instead of re-running Louvain+PageRank.
	if path, perr := OverlayPath(group); perr == nil && path != "" {
		if prior := readOverlayUnconditional(path); prior != nil && prior.InputHash != "" && prior.InputHash == inputHash {
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
	res := graph.RunAlgorithms(entities, rels)
	// Record BEFORE returning (and thus before the caller's overlay write), so a
	// persist failure cannot cause a re-run for this same version.
	storeMemoizedGroupResult(group, inputHash, res)
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
