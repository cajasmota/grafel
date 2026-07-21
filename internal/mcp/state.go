// Package mcp implements the grafel MCP server (clean-room, per ADR-0002).
//
// Layout in this package:
//
//	state.go            in-memory graph state, registry, mtime-based reload
//	server.go           MCP server creation, tool registration, Serve()
//	tools.go            tool handler implementations
//	scoring.go          BM25 + multi-source weighting
//	traversal.go        BFS/DFS traversal helpers
//	context_filter.go   edge-context filter resolution
//	index.go            label inverted index for O(1) describe
//	routing.go          group inference from CWD + cross-repo prefix logic
//	render.go           compact output format
//	telemetry.go        latency / counter telemetry
//	enrichment.go       enrichment-resolutions.json reader
//	candidates.go       link/enrichment candidate tools
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/embed"
	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

// Registry is the on-disk registry.json describing groups and their repos.
//
// Schema:
//
//	{
//	  "groups": {
//	    "<group>": {
//	      "memory_dir": "...",                  # optional
//	      "links_file": "...",                  # optional override
//	      "repos": {
//	        "<repo>": {
//	          "path":        "/abs/path/to/repo",
//	          "graph_file":  "/abs/path/.grafel/graph.json"  // optional explicit
//	        }
//	      }
//	    }
//	  }
//	}
type Registry struct {
	Path   string                   `json:"-"`
	Groups map[string]RegistryGroup `json:"groups"`
}

// RegistryGroup describes a single group entry in the registry.
type RegistryGroup struct {
	MemoryDir string                  `json:"memory_dir,omitempty"`
	LinksFile string                  `json:"links_file,omitempty"`
	Repos     map[string]RegistryRepo `json:"repos"`
}

// RegistryRepo points at a repo's on-disk path and graph file.
type RegistryRepo struct {
	Path      string `json:"path"`
	GraphFile string `json:"graph_file,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling to accept both array format
// (written by CLI registry) and map format (legacy MCP format).
// CLI format: {"version":1,"groups":[{"name":"...","config_path":"..."}]}
// Legacy MCP format: {"groups":{"name":{...}}
func (r *Registry) UnmarshalJSON(data []byte) error {
	// Try unmarshaling as a struct with version + groups array (CLI format)
	type rawRef struct {
		Name       string `json:"name"`
		ConfigPath string `json:"config_path"`
	}
	type rawReg struct {
		Version int      `json:"version"`
		Groups  []rawRef `json:"groups"`
	}
	var raw rawReg
	if err := json.Unmarshal(data, &raw); err == nil && len(raw.Groups) > 0 {
		// CLI format: groups is an array of refs with names and config paths
		r.Groups = make(map[string]RegistryGroup, len(raw.Groups))
		for _, ref := range raw.Groups {
			grp := RegistryGroup{
				Repos: map[string]RegistryRepo{},
			}
			// Load per-group config if available
			if ref.ConfigPath != "" {
				if cfg, err := loadGroupConfig(ref.ConfigPath); err == nil {
					// Convert repos from GroupConfig format to RegistryRepo format
					for _, repo := range cfg.Repos {
						grp.Repos[repo.Slug] = RegistryRepo{
							Path: repo.Path,
						}
					}
				}
				// Silently skip missing or malformed configs—they may be loaded later
			}
			r.Groups[ref.Name] = grp
		}
		return nil
	}

	// Fall back to legacy map format
	type legacyReg struct {
		Groups map[string]RegistryGroup `json:"groups"`
	}
	var legacy legacyReg
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("unmarshal registry: invalid format (neither CLI array nor legacy map): %w", err)
	}
	r.Groups = legacy.Groups
	if r.Groups == nil {
		r.Groups = map[string]RegistryGroup{}
	}
	return nil
}

// groupConfig matches internal/registry.GroupConfig structure for per-group config files.
type groupConfig struct {
	Name  string `json:"name"`
	Repos []struct {
		Slug string `json:"slug"`
		Path string `json:"path"`
	} `json:"repos"`
}

// loadGroupConfig loads and unmarshals a per-group config file.
func loadGroupConfig(configPath string) (*groupConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg groupConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadRegistry reads a registry file. If the file does not exist an empty
// registry is returned (no error).
func LoadRegistry(path string) (*Registry, error) {
	r := &Registry{Path: path, Groups: map[string]RegistryGroup{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	if r.Groups == nil {
		r.Groups = map[string]RegistryGroup{}
	}
	r.Path = path
	return r, nil
}

// graphFile returns the absolute on-disk graph.json for a repo entry.
func (r RegistryRepo) graphFile() string {
	if r.GraphFile != "" {
		return r.GraphFile
	}
	if r.Path == "" {
		return ""
	}
	return daemon.GraphPathForRepo(r.Path)
}

// CrossRepoLink is one entry in a group-links.json file.
//
// Per ADR-0009, source/target are written as "<repo>::<localId>". Confidence
// in [0,1]. Channel/method are free-form labels carried for filtering.
type CrossRepoLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	// Kind holds the edge relation. The MCP-appended candidate format writes
	// "kind"; the links-pass on-disk format (internal/links) writes "relation".
	// Accept both so service-level SCC detection (#1502) sees pass-emitted
	// links (calls / imports / publishes_to). Relation is the on-disk alias;
	// EffectiveKind() collapses them.
	Kind       string  `json:"kind"`
	Relation   string  `json:"relation,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Channel    string  `json:"channel,omitempty"`
	Method     string  `json:"method,omitempty"`
	// Identifier is the canonical channel/topic name the links pass records for
	// method="topic"/"http" entries (internal/links.Link.Identifier). For a
	// message-topic join it is the broker-prefixed topic Name (e.g.
	// "kafka:orders.placed"), letting the query layer (grafel_related
	// direction=messaging, grafel_impact_radius) match a cross-repo topic join
	// back to the specific SCOPE.MessageTopic being inspected (#5782). Absent for
	// passes that do not stamp it.
	Identifier string `json:"identifier,omitempty"`
	// Properties carries the on-disk links-pass annotations (resolve_strategy,
	// normalization, and — since #3628 — the categorical extraction-confidence
	// honesty marker under the "confidence" key). Surfaced read-side via
	// EdgeConfidence() so MCP consumers can tell a fully-resolved cross-repo
	// edge from a heuristically-/runtime-synthesised one.
	Properties map[string]string `json:"properties,omitempty"`
}

// EffectiveKind returns the link's relation, preferring the explicit "kind"
// field and falling back to the links-pass "relation" field.
func (l CrossRepoLink) EffectiveKind() string {
	if l.Kind != "" {
		return l.Kind
	}
	return l.Relation
}

// edgeConfidenceProp is the Properties key under which the links layer stamps
// the categorical honesty marker (mirrors links.EdgeConfidenceKey; duplicated
// here to avoid an MCP→links import edge for a single string constant).
const edgeConfidenceProp = "confidence"

// EdgeConfidence returns the extraction-confidence honesty marker for this
// cross-repo edge (#3628): "resolved" (both endpoints matched on a canonical
// id), "heuristic" (fuzzy / single-side-grounded), or "inferred" (derived from
// a runtime-dynamic value). Per the honesty contract, an absent marker means
// the edge is structurally grounded, so this returns "resolved" as the
// default — AST-grounded passes (import_pass) deliberately do not stamp it.
func (l CrossRepoLink) EdgeConfidence() string {
	if l.Properties != nil {
		if v := l.Properties[edgeConfidenceProp]; v != "" {
			return v
		}
	}
	return "resolved"
}

// LoadedRepo is one repo's graph plus index plus mtime tracking.
//
// LabelIndex is rebuilt eagerly when the graph file content changes (it is a
// cheap O(N) map build — ~2% of reload cost — and is read directly by many
// tools, so keeping it eager avoids a wide getter migration). BM25 is the
// heavy index (~85% of reload cost: it tokenizes every entity's name, path,
// docstring and discriminators) and is built LAZILY on first search-tool use
// via getBM25() (#3377), then cached until the next reload re-arms bm25Once.
// The derived traversal indexes (Adjacency, CallsAdj, StepAdj, ByID,
// TopKPageRank) are likewise built LAZILY on first use via the getter methods
// and cached until the next reload re-arms them (#3367) — see the field block
// below. This keeps the cheap-call path (whoami / stats / feedback_event)
// off the heavy O(R) adjacency scan, the iterative PageRank sort, and the
// BM25 tokenization entirely.
//
// #3377: reload also skips the reparse + LabelIndex rebuild when the graph.fb
// content hash is unchanged (see contentHash) — mtime churn from a no-op
// reindex no longer pays the parse cost.
//
// S8 (#2159): Reader is an mmap'd fbreader.Reader kept open for the
// lifetime of the cached slot. MCP query paths that only need to
// iterate entities or relationships should use Reader.IterateEntities /
// Reader.IterateRelationships to avoid heap allocation beyond the
// transient field-decode wrapper. Reader is nil when graph.fb is not
// present (JSON-only fallback or old index format).
type LoadedRepo struct {
	Repo      string
	Path      string
	GraphFile string
	Doc       *graph.Document
	Reader    *fbreader.Reader // mmap zero-copy reader (S8, #2159); nil when unavailable
	// handle is the F1 (ADR-0027) deferred-unmap wrapper that OWNS the lifetime
	// of Reader's mapping. Reader is the (still-dark) read accessor; handle is
	// what reload/evict/Close route the munmap through so an in-flight borrow can
	// drain first. Invariant: handle == nil iff Reader == nil, and when both are
	// non-nil handle.reader == Reader. Mutated only under State.mu via
	// publishHandle / retireHandle. Nil when graph.fb is unavailable.
	handle     *MapHandle
	LabelIndex *LabelIndex
	BM25       *BM25Index
	Semantic   *embed.Store // per-repo vector index (nil when no embeddings.bin)
	// TestsEdgeCount is a cheap O(R) count of TESTS-kind relationships, computed
	// eagerly at reload so grafel_whoami returns it in O(1) (#3325). Kept
	// eager because it is O(R) with no allocation — far cheaper than the derived
	// indexes below and read by the hot whoami path.
	TestsEdgeCount int

	// Derived indexes below are built LAZILY on first use via the getter methods
	// (getAdjacency/getCallsAdj/getStepAdj/getByID/getTopKPageRank) rather than
	// eagerly in reloadLocked (#3367). The live indexer rewrites graph.fb
	// constantly; eagerly rebuilding all of these — especially the iterative
	// TopKPageRank — on every reload imposed a ~400ms floor on EVERY MCP call,
	// including cheap tools (feedback_event/stats/whoami) that read none of
	// them. Each index is guarded by idxOnce[...] so it is built at most once
	// per reload; resetIndexes() (called under s.mu during reload) re-arms the
	// Once values so the next access rebuilds against the fresh Doc.
	//
	// All access MUST go through the getters — never read these fields directly,
	// because a nil value is indistinguishable from "not yet built". The getters
	// are safe under concurrent handler goroutines (idxMu serialises the build).
	idxMu        sync.Mutex
	adjOnce      sync.Once
	callsOnce    sync.Once
	stepOnce     sync.Once
	byIDOnce     sync.Once
	pagerankOnce sync.Once
	bm25Once     sync.Once                // #3377: BM25 built lazily on first search
	suffixOnce   sync.Once                // #5682: source-file suffix index built lazily
	adjacency    *adjacency               // in/out neighbor lists (#1656)
	callsAdj     *callsAdjacency          // CALLS-only forward adjacency, CSR layout (#1656, #5850)
	stepAdj      map[string][]stepEdge    // STEP_IN_PROCESS forward adjacency (#2417)
	byID         map[string]*graph.Entity // entity ID -> entity (#1656)
	topKPageRank []string                 // entity IDs sorted descending by PageRank (#2304)
	// bm25LastUse is the wall-clock time of the most recent getBM25() borrow (a
	// search-tool use). idxMu-guarded — written under idxMu in getBM25 and read
	// under idxMu in evictBM25IfIdle. The idle sweep (SweepIdleBM25) drops the
	// heavy BM25 index once now-bm25LastUse exceeds the configured idle window so
	// a session that has stopped searching does not keep ~313 MB (corpus) pinned;
	// the next search rebuilds it transparently via getBM25. Zero value means the
	// index has never been borrowed through getBM25 (e.g. a test that set BM25
	// directly), in which case it is NOT eligible for idle eviction.
	bm25LastUse time.Time
	// mroInbound maps a DEFINING member's local id -> the inherited-stub ids
	// that resolve to it via the MRO walk (#3834). It is the reverse of
	// mroOutboundEdges, used by neighbors(in) so a base method surfaces the
	// subclasses that inherit it. In-repo defining members only (external
	// contract endpoints have no in-repo node to query callers of).
	//
	// #5791: the map is no longer guarded by a sync.Once (which resetIndexes
	// re-armed on every reload epoch, forcing an O(members × walk) rebuild on
	// every callers query with a live indexer). It is now keyed by contentHash
	// via mroMu/mroInboundHash so an unchanged graph reuses it — mirroring the
	// byte-identical-reload skip at reloadLocked. baseChainCache memoizes the
	// EXTENDS/IMPLEMENTS frontier walk per owning class (also contentHash-keyed)
	// so the per-member resolver reuses one walk across all of a class's members.
	mroMu          sync.Mutex
	mroInbound     map[string][]string
	mroInboundHash uint64
	baseChainMu    sync.Mutex
	baseChainCache map[string][]baseRef
	baseChainHash  uint64
	// suffixIndex maps a file basename -> the repo-relative (slash) paths of
	// every source file with that basename under lr.Path (#5682). Built once by
	// a single filesystem walk (getSuffixIndex) and used to resolve a
	// nested-module-relative source_file to its real absolute path via a unique
	// suffix match — O(1) per lookup, no per-call tree walk. suffixIndexPartial
	// is true when the walk hit suffixWalkFileBudget and stopped early, in which
	// case uniqueness cannot be proven and lookups conservatively return "".
	suffixIndex        map[string][]string
	suffixIndexPartial bool

	// hotIdx is the W1 (ADR-0027) memoized handle-keyed hot index (id/label/
	// qname entity lookups + the relationship view seam) built lazily on the
	// FIRST view-getter use for a given captured MapHandle and reused across
	// borrows of that handle. It is guarded by its own leaf mutex hotIdxMu (never
	// nested under idxMu / mroMu / baseChainMu) because the build takes no other
	// index lock. The memo is keyed by the captured handle's identity: a reload
	// publishes a successor handle, so the next borrow's captured handle no longer
	// matches lr.hotIdx.Handle() and the index rebuilds. resetIndexes also clears
	// it under s.mu on every Doc replacement, covering the degenerate no-mmap
	// (nil-handle) reload where the handle identity is unchanged. Nil until the
	// first view-getter use. See LoadedRepo.hotIndexFor and hotindex.go.
	hotIdxMu sync.Mutex
	hotIdx   *hotIndex

	semMtime time.Time
	mtime    time.Time
	// contentHash is the FNV-1a hash of the graph.fb bytes last parsed into
	// Doc (#3377). The live indexer rewrites graph.fb on every reindex, which
	// bumps the mtime even when the serialized content is byte-identical (a
	// no-op reindex, a `touch`, or an unchanged re-emit). When the freshly
	// stat'd file's hash matches contentHash we SKIP the reparse + LabelIndex
	// rebuild entirely and only advance mtime — a freshness-safe shortcut
	// because identical bytes carry identical graph state. Empty until the
	// first successful load.
	contentHash uint64
	loadErr     string // populated when last reload failed; doc may be stale

	// reindexRequired / reindexReason record whether the LAST reload attempt
	// for this repo failed specifically because its on-disk graph.fb format
	// version is older than this binary supports (#reindex-required PR1:
	// detection + observability only — nothing reads these fields to trigger
	// a reindex or a user prompt yet). The authoritative, always-fresh,
	// race-free ReindexRequired/ReindexReason surfaced to the statusline and
	// `grafel status --json` is recomputed independently by the engine's
	// status writer directly from graph.fb bytes (see readDocumentFromDir's
	// call site above for why serve-side state is not the source of truth).
	reindexRequired bool
	reindexReason   string

	// algoStampedMt is the lr.mtime value at which applyGroupAlgoOverlay last
	// stamped the group overlay (CommunityID/PageRank/Centrality/god/articulation)
	// onto this repo's in-memory entities (#5400/#5401). The overlay apply is
	// memoized at the GROUP level by the overlay file's own mtime, but a repo's
	// graph.fb can be rewritten (reparse → fresh doc.Entities with the per-repo
	// sentinel community ids, e.g. -1) AFTER the overlay was first applied. When
	// that happens the overlay file mtime is unchanged, so the group-level memo
	// would skip re-stamping and the reparsed repo's entities silently revert to
	// community_id:-1 (the acme-mobile symptom in #5401). Tracking the stamped
	// mtime per repo lets the apply re-stamp exactly the repos that were reparsed
	// since the last stamp, regardless of the overlay-file memo. The zero value
	// means "never stamped", so a freshly-loaded repo is always (re-)stamped.
	algoStampedMt time.Time
}

// resetIndexes re-arms every lazy-index sync.Once and clears the cached
// derived indexes so the next getter call rebuilds against the current Doc.
// MUST be called whenever lr.Doc is replaced during reload, while the caller
// holds State.mu (which serialises reload against handler snapshots).
//
// Locking: the idxMu-guarded indexes (BM25/adjacency/callsAdj/stepAdj/byID/
// topKPageRank/suffix) are cleared under idxMu so a getter that raced in just
// before the swap cannot observe a half-reset state. The #5791 MRO caches
// (mroInbound, baseChainCache) are guarded by their OWN mutexes (mroMu /
// baseChainMu), NOT idxMu, so they are cleared under those locks here — a
// getter is mutually excluded from observing a half-cleared MRO map. Each MRO
// mutex is taken and released independently (never nested with each other or
// idxMu) so this cannot form a cycle against the read-path lock order
// mroMu -> baseChainMu -> idxMu (getMROInbound holds mroMu across
// buildMROInbound -> baseChain(baseChainMu) -> getAdjacency(idxMu)).
func (lr *LoadedRepo) resetIndexes() {
	// #5791: clear the reverse-INHERITS map and the per-class base-chain memo
	// under their own mutexes so the next getMROInbound rebuilds against the
	// fresh Doc. The contentHash guard would rebuild anyway (the caller sets the
	// new contentHash before this reset), but clearing frees the old maps now.
	lr.mroMu.Lock()
	lr.mroInbound = nil
	lr.mroInboundHash = 0
	lr.mroMu.Unlock()

	lr.baseChainMu.Lock()
	lr.baseChainCache = nil
	lr.baseChainHash = 0
	lr.baseChainMu.Unlock()

	// W1 (ADR-0027): drop the memoized hot index so the next view-getter use
	// rebuilds against the fresh Doc. Handle-identity keying already rebuilds when
	// reload publishes a successor handle, but a no-mmap reload (nil handle both
	// before and after) leaves the identity unchanged, so the explicit clear here
	// — under s.mu, on every Doc replacement — is the load-bearing invalidation
	// for that case. Its own leaf mutex, taken independently (never nested).
	lr.hotIdxMu.Lock()
	lr.hotIdx = nil
	lr.hotIdxMu.Unlock()

	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.adjOnce = sync.Once{}
	lr.callsOnce = sync.Once{}
	lr.stepOnce = sync.Once{}
	lr.byIDOnce = sync.Once{}
	lr.pagerankOnce = sync.Once{}
	lr.bm25Once = sync.Once{}
	lr.suffixOnce = sync.Once{}
	lr.BM25 = nil
	lr.adjacency = nil
	lr.callsAdj = nil
	lr.stepAdj = nil
	lr.byID = nil
	lr.topKPageRank = nil
	lr.suffixIndex = nil
	lr.suffixIndexPartial = false
}

// publishHandle installs nh as the repo's live mmap handle under the F1
// (ADR-0027) deferred-unmap protocol and retires the predecessor. The ordering
// is load-bearing: the successor is published (lr.handle / lr.Reader repointed)
// BEFORE the old handle is retired, so a fresh borrow — which only ever targets
// the currently-published lr.handle — can never re-borrow the retired
// predecessor (§Correctness "no re-borrow of a retired handle"). retire() then
// unmaps the predecessor immediately iff it has already drained (refs==0);
// otherwise the last in-flight release() unmaps it. NEVER an in-place Close().
//
// nh may be nil (JSON-only fallback / no graph.fb). Caller MUST hold State.mu.
func (lr *LoadedRepo) publishHandle(nh *MapHandle) {
	old := lr.handle
	if nh != nil {
		nh.repo = lr.Repo
		lr.Reader = nh.reader
	} else {
		lr.Reader = nil
	}
	lr.handle = nh
	if old != nil {
		old.retire()
	}
}

// setReader wraps a freshly-opened reader (or nil) in a MapHandle and publishes
// it via publishHandle. This is the reload swap: it replaces the old bare
// lr.Reader.Close() so a reload never munmaps a mapping an in-flight borrow
// still aliases. Caller MUST hold State.mu.
func (lr *LoadedRepo) setReader(newRdr *fbreader.Reader) {
	var nh *MapHandle
	if newRdr != nil {
		nh = newMapHandle(newRdr)
	}
	lr.publishHandle(nh)
}

// retireHandle retires the repo's current mmap handle (repo eviction and server
// Close), routing the munmap through the F1 drain instead of a bare
// lr.Reader.Close() — same munmap-while-borrowed hazard as reload. The mapping
// is unmapped now iff it has drained, else by the last release(). Caller MUST
// hold State.mu.
func (lr *LoadedRepo) retireHandle() {
	old := lr.handle
	lr.handle = nil
	lr.Reader = nil
	if old != nil {
		old.retire()
	}
}

// getSuffixIndex returns the source-file suffix index (basename -> repo-relative
// paths) and whether the build was truncated by the file budget, building it on
// first use via a single filesystem walk of lr.Path (#5682). Deliberately NOT
// guarded by idxMu (matching getMROInbound): the build only touches the
// filesystem, never other idxMu-guarded getters, so holding idxMu during a
// potentially slow walk would needlessly serialise unrelated index builds.
// sync.Once alone gives build-at-most-once per reload; resetIndexes() re-arms it.
func (lr *LoadedRepo) getSuffixIndex() (map[string][]string, bool) {
	lr.suffixOnce.Do(func() {
		lr.suffixIndex, lr.suffixIndexPartial = buildSuffixIndex(lr.Path)
	})
	return lr.suffixIndex, lr.suffixIndexPartial
}

// getMROInbound returns the reverse-INHERITS map (defining-member id ->
// inheriting-stub ids), building it lazily on first use (#3834). It scans the
// repo's member entities once and records, for each that resolves to an in-repo
// defining member via the MRO walk, the defining member's id. Used by
// neighbors(in) so a base method surfaces its inheriting subclasses as callers.
func (lr *LoadedRepo) getMROInbound() map[string][]string {
	// #5791 — keyed by contentHash, not a sync.Once. reloadBeforeCall runs on
	// every MCP call and resetIndexes re-armed the Once whenever graph.fb bytes
	// changed, so a live indexer forced a full O(members × walk) rebuild on every
	// callers query (p50=88s). Guarding by contentHash means an unchanged graph
	// reuses the cached map even across reload epochs (the same freshness-safe
	// principle as the byte-identical-reload skip in reloadLocked); a genuine
	// content change (new contentHash) rebuilds exactly once.
	//
	// Guarded by mroMu (NOT idxMu): buildMROInbound -> resolveMember -> baseChain
	// (baseChainMu) -> extendsBases -> getAdjacency (idxMu). Lock order is
	// mroMu -> baseChainMu -> idxMu, so holding mroMu across the build cannot
	// deadlock against those non-reentrant getters.
	lr.mroMu.Lock()
	defer lr.mroMu.Unlock()
	if lr.mroInbound != nil && lr.mroInboundHash == lr.contentHash {
		return lr.mroInbound
	}
	lr.mroInbound = buildMROInbound(lr)
	lr.mroInboundHash = lr.contentHash
	return lr.mroInbound
}

// getByID returns the entity-ID → *Entity map, building it on first use.
// Reuses LabelIndex.ByID when present (the LabelIndex is built eagerly at
// reload and carries an identical map), avoiding a second O(N) pass.
func (lr *LoadedRepo) getByID() map[string]*graph.Entity {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.byIDOnce.Do(func() {
		if lr.LabelIndex != nil && lr.LabelIndex.ByID != nil {
			lr.byID = lr.LabelIndex.ByID
			return
		}
		if lr.Doc == nil {
			lr.byID = map[string]*graph.Entity{}
			return
		}
		m := make(map[string]*graph.Entity, len(lr.Doc.Entities))
		for i := range lr.Doc.Entities {
			m[lr.Doc.Entities[i].ID] = &lr.Doc.Entities[i]
		}
		lr.byID = m
	})
	return lr.byID
}

// hotIndexFor returns the memoized W1 (ADR-0027) hot index for this repo built
// off the captured handle h, building it via build(h) exactly once per distinct
// captured handle and reusing it across borrows of that same handle.
//
// Memoization + invalidation contract:
//   - Reuse iff a memoized index exists AND it is keyed off the SAME captured
//     handle h (lr.hotIdx.Handle() == h). This is the read-through-captured-
//     handle invariant (F1/F2): the returned index's handle always equals the
//     caller's captured handle, never a live lr.handle re-deref.
//   - A reload publishes a successor handle, so a subsequent borrow captures a
//     different h and this rebuilds. resetIndexes additionally nils lr.hotIdx
//     under s.mu on every Doc replacement, so even a nil-handle (no-mmap) reload
//     invalidates. The index is therefore never read across a reload.
//
// build is invoked ONLY on a memo miss (the first getter use for a handle, or
// after invalidation), so the hot index costs nothing until a consumer reads it.
// Guarded by hotIdxMu, a leaf lock: build must not acquire any other LoadedRepo
// index lock (docEntityViewSource/docRelationshipViewSource read only lr.Doc).
func (lr *LoadedRepo) hotIndexFor(h *MapHandle, build func(*MapHandle) *hotIndex) *hotIndex {
	lr.hotIdxMu.Lock()
	defer lr.hotIdxMu.Unlock()
	if lr.hotIdx != nil && lr.hotIdx.Handle() == h {
		return lr.hotIdx
	}
	lr.hotIdx = build(h)
	return lr.hotIdx
}

// getBM25 returns the per-repo BM25 index, building it on first search-tool
// use (#3377). BM25 construction tokenizes every entity (name, file path,
// docstring, discriminators) and dominates reload cost (~85%); deferring it
// keeps the cheap-call path and reloads off it entirely. Built at most once
// per reload via bm25Once; resetIndexes() re-arms it against the fresh Doc.
// Returns nil for a repo with no Doc (caller's BM25.Search handles nil).
func (lr *LoadedRepo) getBM25() *BM25Index {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.bm25Once.Do(func() {
		if lr.Doc == nil {
			return
		}
		lr.BM25 = BuildBM25(lr.Doc)
	})
	// Record the borrow time so the idle sweep can distinguish an actively-used
	// index from one whose last search is older than the eviction window. Stamped
	// under idxMu (same lock evictBM25IfIdle reads it under) so the sweep never
	// observes a torn timestamp. The caller reads through the returned pointer for
	// the duration of its Search; a concurrent eviction only nils lr.BM25, which —
	// under Go's GC — merely drops one reference and can never free the index out
	// from under this borrow (contrast F1's munmap, which needs a refcount drain).
	lr.bm25LastUse = time.Now()
	return lr.BM25
}

// evictBM25IfIdle drops the repo's BM25 index (and re-arms bm25Once so the next
// getBM25 rebuilds it) iff it is currently built AND its last search borrow is
// older than idle relative to now. Returns true when it evicted.
//
// Concurrency: runs under idxMu, exactly like getBM25 and resetIndexes, so it is
// mutually excluded from a concurrent build and from the reload-time reset — the
// three are the only writers of lr.BM25 / lr.bm25Once. Re-arming the sync.Once by
// assignment is safe here for the same reason resetIndexes does it: no other
// goroutine can be inside bm25Once.Do (it holds idxMu for the whole Do). Nilling
// lr.BM25 does NOT free memory an in-flight search still reads — that search holds
// its own live *BM25Index from getBM25, so Go's GC keeps the object alive until
// the search returns; only after the last reference drops is the ~313 MB (corpus)
// reclaimable. This is why a plain lock suffices and no F1-style refcount drain is
// needed: BM25 is a pure heap object, not a manually-unmapped mmap region.
//
// Reload interaction: resetIndexes already nils lr.BM25 under idxMu on a graph
// change, so a stale index for the new graph is dropped there; an eviction that
// races a reload finds lr.BM25 already nil and no-ops (no double-free), and a
// reload that races an eviction likewise just re-nils an already-nil field.
func (lr *LoadedRepo) evictBM25IfIdle(idle time.Duration, now time.Time) bool {
	if idle <= 0 {
		return false
	}
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	if lr.BM25 == nil {
		return false
	}
	// A never-borrowed index (bm25LastUse zero) is not idle-eligible: it was
	// populated outside the search path (e.g. a test that set BM25 directly) and
	// has no meaningful last-use to age out.
	if lr.bm25LastUse.IsZero() || now.Sub(lr.bm25LastUse) < idle {
		return false
	}
	lr.BM25 = nil
	lr.bm25Once = sync.Once{}
	return true
}

// getAdjacency returns the in/out neighbor index, building it on first use.
func (lr *LoadedRepo) getAdjacency() *adjacency {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.adjOnce.Do(func() {
		if lr.Doc == nil {
			// Zero-value adjacency works out of the box: nodes.code/kinds.code
			// are nil maps (lookups miss cleanly, returning ok=false) and
			// out/in are zero-value csrDir (nil slices), so Outgoing/Incoming
			// return nil for every id, matching the pre-#5852 empty-map
			// behaviour (#5852).
			lr.adjacency = &adjacency{}
			return
		}
		// ADR-0027 Cutover PR1: source the build from the resident mmap Reader
		// when present (byte-identical to the Document build — same rows, same
		// order). Falls back to the Document only when no graph.fb is mapped
		// (JSON-only load / Open failure). Not flag-gated.
		if lr.Reader != nil {
			lr.adjacency = buildAdjacencyFromReader(lr.Reader, lr.Repo)
		} else {
			lr.adjacency = buildAdjacency(lr.Doc, lr.Repo)
		}
	})
	return lr.adjacency
}

// getCallsAdj returns the CALLS-only forward adjacency, building it on first use.
func (lr *LoadedRepo) getCallsAdj() *callsAdjacency {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.callsOnce.Do(func() {
		if lr.Doc == nil {
			// Zero-value callsAdjacency works out of the box (nodes.code is a
			// nil map, so Get's lookup misses cleanly and returns nil),
			// matching the pre-#5850 empty-map behaviour.
			lr.callsAdj = &callsAdjacency{}
			return
		}
		// ADR-0027 Cutover PR1: prefer the resident mmap Reader (see getAdjacency).
		if lr.Reader != nil {
			lr.callsAdj = buildCallsAdjacencyFromReader(lr.Reader)
		} else {
			lr.callsAdj = buildCallsAdjacency(lr.Doc)
		}
	})
	return lr.callsAdj
}

// getStepAdj returns the STEP_IN_PROCESS forward adjacency, building it on first use.
func (lr *LoadedRepo) getStepAdj() map[string][]stepEdge {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.stepOnce.Do(func() {
		if lr.Doc == nil {
			lr.stepAdj = map[string][]stepEdge{}
			return
		}
		// ADR-0027 Cutover PR1: prefer the resident mmap Reader (see getAdjacency).
		if lr.Reader != nil {
			lr.stepAdj = buildStepAdjacencyFromReader(lr.Reader)
		} else {
			lr.stepAdj = buildStepAdjacency(lr.Doc)
		}
	})
	return lr.stepAdj
}

// getTopKPageRank returns the top-K PageRank-ordered entity IDs, building the
// sorted slice on first use. This is the heaviest derived index and feeds only
// pickFallback (#2304); building it lazily keeps it off the cheap-call path.
func (lr *LoadedRepo) getTopKPageRank() []string {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.pagerankOnce.Do(func() {
		// ADR-0027 Cutover PR1: prefer the resident mmap Reader; PageRank is
		// read from the FB Pagerank() scalar (interface-absent field). Falls
		// back to the Document only when no graph.fb is mapped.
		if lr.Reader != nil {
			lr.topKPageRank = buildTopKPageRankFromReader(lr.Reader, 64)
		} else {
			lr.topKPageRank = buildTopKPageRank(lr.Doc, 64)
		}
	})
	return lr.topKPageRank
}

// LoadedGroup holds all loaded repos for a group plus cross-repo links.
type LoadedGroup struct {
	Name      string
	Repos     map[string]*LoadedRepo
	Links     []CrossRepoLink
	LinksFile string
	linksMt   time.Time
	MemoryDir string

	// Communities is the GROUP-scope community summary applied from the
	// <group>-algo.json overlay (A2, #5354). nil when no overlay is present —
	// consumers then fall back to per-repo community data carried in graph.fb.
	Communities []graph.CommunityResult
	// algoFile / algoMt memoize the overlay by mtime (mirrors LinksFile/linksMt)
	// so a mid-session swap of <group>-algo.json reloads only the overlay, not
	// the graphs. algoApplied records whether the last-loaded overlay's values
	// are currently stamped onto the in-memory entities, so a stale/absent
	// overlay after a previous apply does not leave group values lingering.
	algoFile    string
	algoMt      time.Time
	algoApplied bool
}

// WorktreeLookup is the narrow interface ResolveCWD uses to query the PH3
// ephemeral worktree-child registry.  The interface avoids a direct import
// of internal/daemon/worktree from the mcp package.
type WorktreeLookup interface {
	// LookupPath returns the (groupName, parentSlug, branch) for a worktree
	// whose absolute path matches wtPath, or ("","","") if not found.
	LookupPath(wtPath string) (group, slug, branch string)
}

// State is the long-lived in-memory model. All accesses go through Reload()
// which is safe to call from any goroutine.
type State struct {
	mu       sync.Mutex
	registry *Registry
	groups   map[string]*LoadedGroup
	created  time.Time
	// registryMtime tracks the on-disk mtime of registry.json so Reload()
	// can detect mid-session mutations (group register/unregister, repo add/
	// remove) and fire a tools/list_changed notification (#1772).
	registryMtime time.Time
	// registrySignature is a stable digest of group→repo names. When this
	// changes between reloads, the visible tool surface may have changed and
	// the server should emit notifications/tools/list_changed.
	registrySignature string
	// worktreeLookup is the optional PH3 ephemeral registry.  When non-nil,
	// ResolveCWD checks it before falling through to the parent-repo logic so
	// that a cwd inside a linked worktree returns the worktree-specific entry.
	worktreeLookup WorktreeLookup

	// CrossLinkCache is the ref-keyed in-memory cache for cross-repo link
	// candidates (issue #2224). Keyed by (repoA, refA, repoB, refB); the
	// secondary index allows O(affected-entries) invalidation on ref-switch.
	// Exported so that the daemon server can call NotifyRefSwitch on receipt
	// of a BranchSwitchEvent and integration tests can inspect cache state.
	CrossLinkCache *CrossLinkCache

	// warmingFn is the optional read-only scheduler warming accessor (#5690).
	// Injected by cmd/grafel via SetWarmingSnapshot when the daemon has a live
	// scheduler; nil on the stdio-native path (no daemon) or in tests, in which
	// case Warming() reports "not warming / unknown" gracefully. Read-only — it
	// never affects scheduling.
	warmingFn func() daemon.WarmingSnapshot
}

// SetWarmingSnapshot wires a read-only scheduler warming accessor into the
// State (#5690). Called from cmd/grafel once the daemon's scheduler is up so
// grafel_whoami / grafel_status can distinguish a warming group (post-index
// enrichment in flight) from a genuinely slow query. Passing nil clears it.
func (s *State) SetWarmingSnapshot(fn func() daemon.WarmingSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.warmingFn = fn
}

// Warming returns the current warming snapshot and whether a scheduler handle
// is wired. When no handle is injected it returns the zero snapshot (not
// warming) and false, so callers degrade gracefully to warming=false. The
// accessor is invoked WITHOUT holding s.mu so the scheduler's own snapshot
// lock never nests under the MCP state lock.
func (s *State) Warming() (daemon.WarmingSnapshot, bool) {
	s.mu.Lock()
	fn := s.warmingFn
	s.mu.Unlock()
	if fn == nil {
		return daemon.WarmingSnapshot{}, false
	}
	return fn(), true
}

// SetWorktreeLookup wires the PH3 ephemeral worktree registry.  Call from
// cmd/grafel after the worktree.Store is loaded.
func (s *State) SetWorktreeLookup(wl WorktreeLookup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.worktreeLookup = wl
}

// NewState constructs an empty state for the given registry.
func NewState(reg *Registry) *State {
	return &State{
		registry:       reg,
		groups:         map[string]*LoadedGroup{},
		created:        time.Now(),
		CrossLinkCache: NewCrossLinkCache(),
	}
}

// NotifyRefSwitch is called by the daemon when a BranchSwitchEvent arrives for
// a participating repo. It synchronously invalidates every CrossLinkCache entry
// whose key references (repo, oldRef), ensuring the next cross-repo query
// produces fresh data for the repo's new ref.
//
// Returns the number of cache entries evicted (0 when none were cached for
// this repo+ref pair, which is the common case on single-ref installations).
//
// This is the hook wired from cmd/grafel's BranchSwitchSink into the MCP
// server to close the stale-cache bug tracked in issue #2224.
func (s *State) NotifyRefSwitch(repo, oldRef string) int {
	return s.CrossLinkCache.InvalidateRepo(repo, oldRef)
}

// Registry returns the loaded registry.
func (s *State) Registry() *Registry { return s.registry }

// Groups returns the names of all known groups (registry-defined).
func (s *State) Groups() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.registry.Groups))
	for g := range s.registry.Groups {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// computeRegistrySignature returns a stable, order-independent digest of the
// (group → sorted repo names) mapping. Used to detect mid-session mutations
// of the tool surface for notifications/tools/list_changed (#1772).
func computeRegistrySignature(reg *Registry) string {
	if reg == nil || len(reg.Groups) == 0 {
		return ""
	}
	groups := make([]string, 0, len(reg.Groups))
	for g := range reg.Groups {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	var b []byte
	for _, g := range groups {
		b = append(b, g...)
		b = append(b, '{')
		repos := make([]string, 0, len(reg.Groups[g].Repos))
		for r := range reg.Groups[g].Repos {
			repos = append(repos, r)
		}
		sort.Strings(repos)
		for _, r := range repos {
			b = append(b, r...)
			b = append(b, ',')
		}
		b = append(b, '}', ';')
	}
	return string(b)
}

// refreshRegistryFromDisk re-reads registry.json when its on-disk mtime is
// newer than the last load. Returns true when the registry was actually
// reloaded. Caller must hold s.mu.
func (s *State) refreshRegistryFromDisk() bool {
	if s.registry == nil || s.registry.Path == "" {
		return false
	}
	fi, err := os.Stat(s.registry.Path)
	if err != nil {
		return false
	}
	if !fi.ModTime().After(s.registryMtime) {
		return false
	}
	reg, err := LoadRegistry(s.registry.Path)
	if err != nil {
		return false
	}
	s.registry = reg
	s.registryMtime = fi.ModTime()
	return true
}

// Reload performs lazy mtime-driven reload of every repo + links file in
// the registry. Returns the count of (re)loaded files. Safe for concurrent
// callers — serialized through s.mu.
//
// #1772: when the registry file on disk has been mutated mid-session (group
// register/unregister, repo add/remove) the registry is re-read and the
// tool-surface signature is recomputed. Callers that want to observe surface
// changes should use ReloadAndSurfaceChanged.
func (s *State) Reload() (int, error) {
	n, _, err := s.reloadLocked()
	return n, err
}

// ReloadAndSurfaceChanged behaves like Reload but additionally reports whether
// the visible tool surface for this session likely changed (registry
// signature mutation). Wired into the MCP server to gate emission of
// notifications/tools/list_changed (#1772).
func (s *State) ReloadAndSurfaceChanged() (int, bool, error) {
	return s.reloadLocked()
}

func (s *State) reloadLocked() (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prevSig := s.registrySignature
	registryChanged := s.refreshRegistryFromDisk()
	reloaded := 0
	for gName, gEntry := range s.registry.Groups {
		grp, ok := s.groups[gName]
		if !ok {
			grp = &LoadedGroup{
				Name:      gName,
				Repos:     map[string]*LoadedRepo{},
				MemoryDir: gEntry.MemoryDir,
			}
			s.groups[gName] = grp
		}
		// Load each repo.
		seen := map[string]bool{}
		for rName, rEntry := range gEntry.Repos {
			seen[rName] = true

			lr, ok := grp.Repos[rName]

			// #2550: short-circuit graph discovery when we already know the path.
			// daemon.FindGraphFile → daemon.StateDirForRepo → gitmeta.Capture forks
			// up to 4 git subprocesses per repo per call (each ~50-200ms on a cold
			// PATH cache). When lr.GraphFile is already set from a previous reload,
			// stat the file directly — no git subprocesses needed. Only fall through
			// to FindGraphFile on first load or when the cached path disappears.
			var graphPath string
			var modtimeNs int64
			if ok && lr.GraphFile != "" {
				if fi, statErr := os.Stat(lr.GraphFile); statErr == nil {
					graphPath = lr.GraphFile
					modtimeNs = fi.ModTime().UnixNano()
				}
				// If stat failed (file removed) fall through to full discovery below.
			}
			if graphPath == "" {
				// Use FindGraphFileAnyRef to discover graph.fb (preferred) or
				// graph.json, fixing issue #1374 item #1: repos that only have
				// graph.fb were silently dropped because the old os.Stat always
				// targeted graph.json.
				//
				// #3648: AnyRef falls back to the newest graph under ANY indexed
				// ref when the current HEAD ref's dir is empty. A group registered
				// via `group add --index` (watchers default OFF) is indexed once at
				// the then-HEAD ref; when HEAD later moves the current-ref dir is
				// empty and the old current-ref-only FindGraphFile returned "",
				// leaving Doc nil and every repo-scoped tool reporting
				// "no repos loaded for this group". Serving the most-recent indexed
				// ref keeps find/inspect/expand working regardless of how the group
				// was registered.
				graphPath, modtimeNs = daemon.FindGraphFileAnyRef(rEntry.Path)
			}

			if graphPath == "" {
				// Neither graph.fb nor graph.json exists yet; skip without error.
				if !ok {
					lr = &LoadedRepo{Repo: rName, Path: rEntry.Path}
					grp.Repos[rName] = lr
				}
				lr.loadErr = "no graph file found (graph.fb or graph.json)"
				continue
			}
			if !ok {
				lr = &LoadedRepo{Repo: rName, Path: rEntry.Path, GraphFile: graphPath}
				grp.Repos[rName] = lr
			}
			// Update GraphFile in case .fb appeared after initial load.
			lr.GraphFile = graphPath
			fileMtime := time.Unix(0, modtimeNs)
			// #3648: derive the state dir from the directory the graph file was
			// actually discovered in (graphPath may live under a non-HEAD ref dir
			// when AnyRef fell back), NOT from the current-HEAD per-ref dir. The
			// graph.fb mmap reader and the embeddings.bin / graph.json sidecars
			// below must all read from the SAME dir as the parsed Document, or a
			// fallback-discovered graph would be paired with a stale/empty sidecar.
			stateDir := filepath.Dir(lr.GraphFile)
			if !(fileMtime.Equal(lr.mtime) && lr.Doc != nil) {
				// #3377 content-hash skip: the live indexer rewrites graph.fb on
				// every reindex, bumping mtime even when the bytes are identical
				// (no-op reindex / touch / unchanged re-emit). Hash the file
				// first; if it matches the bytes we last parsed, advance mtime
				// and skip the (heavy) reparse + LabelIndex rebuild entirely.
				// This is freshness-safe: identical bytes ⇒ identical graph.
				newHash, hErr := hashGraphFile(lr.GraphFile)
				if hErr == nil && lr.contentHash != 0 && newHash == lr.contentHash {
					// Identical bytes — advance mtime, skip the heavy reparse.
					// No reload counted: nothing observable changed.
					lr.mtime = fileMtime
				} else {
					doc, err := readDocumentFromDir(stateDir)
					if err != nil {
						lr.loadErr = err.Error()
						// #reindex-required PR1: detect (not act on) a graph.fb
						// format-version incompatibility. errors.As lets us tell
						// "this repo's on-disk graph.fb was written by an older
						// grafel build than this process supports" apart from any
						// other load failure (corrupt file, permission error,
						// etc.), without string-matching err.Error(). Recorded
						// here for observability on the in-memory LoadedRepo; the
						// DURABLE, authoritative ReindexRequired/ReindexReason
						// truth served to the statusline and `grafel status
						// --json` is (re)computed independently, per on-disk
						// graph.fb bytes, by the engine's status writer
						// (internal/daemon/statuswriter.go, writeRepoStatusFile ->
						// graph.ReindexRequiredReason) on every heartbeat — NOT
						// written from here — because in ADR-0024 split mode this
						// mcp/state.go reload loop runs in the separate `serve`
						// process, whose write here could be silently clobbered by
						// the engine process's own periodic heartbeat write to the
						// same statusfile.File. This slice is detection + state
						// ONLY: no reindex is triggered.
						var fvErr *graph.FormatVersionError
						if errors.As(err, &fvErr) {
							lr.reindexRequired = true
							lr.reindexReason = graph.FormatVersionReason(fvErr.Found, fvErr.Required)
						}
						continue
					}
					lr.reindexRequired = false
					lr.reindexReason = ""
					lr.Doc = doc
					lr.mtime = fileMtime
					lr.contentHash = newHash // 0 on hash failure → next reload re-parses
					lr.loadErr = ""
					lr.LabelIndex = BuildLabelIndex(doc)
					// BM25 is NO LONGER built eagerly here (#3377). It tokenizes every
					// entity (name, path, docstring, discriminators) and dominated
					// reload cost (~85%); it is now built lazily on first search-tool
					// use via getBM25() and cached until resetIndexes() re-arms it.
					// Re-arm the lazy derived indexes (BM25 / Adjacency / CallsAdj /
					// StepAdj / ByID / TopKPageRank). They are NOT built eagerly here —
					// the live indexer rewrites graph.fb constantly, so an eager
					// rebuild on every reload imposed a ~400ms floor (dominated by
					// BM25 tokenization and the iterative TopKPageRank) on every MCP
					// call, including cheap tools that read none of them. Each index
					// is built on first use by its getter and cached until the next
					// reload re-arms the Once here (#3367, #3377).
					lr.resetIndexes()
					// S8 (#2159): open the mmap reader alongside the Document.
					// Best-effort: failures leave Reader nil; callers fall back to
					// doc.Entities / doc.Relationships.
					//
					// F1 (ADR-0027): publish the successor through the deferred-unmap
					// protocol instead of a bare Close() of the old reader. setReader
					// repoints lr.handle/lr.Reader to the new mapping FIRST, then
					// retires the predecessor — which unmaps it now iff it has already
					// drained, else the last in-flight release() unmaps it. Reload
					// never waits on borrows and never munmaps in place. Passing nil on
					// Open failure retires the stale predecessor and leaves Reader nil.
					fbPath := filepath.Join(stateDir, "graph.fb")
					var newRdr *fbreader.Reader
					if rdr, rErr := fbreader.Open(fbPath); rErr == nil {
						newRdr = rdr
					}
					// TESTS-edge count cached once per reload so grafel_whoami can
					// return it in O(1) without rescanning all relationships. This is a
					// cheap O(R) count with no allocation, so it stays eager (#3325).
					//
					// ADR-0027 Cutover PR1: count TESTS-kind edges off the freshly
					// opened mmap Reader (Kind() read directly) rather than the
					// materialized Document. Byte-neutral (Reader == Document's rows);
					// falls back to doc.Relationships only when no graph.fb is mapped.
					testsCount := 0
					if newRdr != nil {
						newRdr.IterateRelationships(func(rel *fb.Relationship) bool {
							if string(rel.Kind()) == "TESTS" {
								testsCount++
							}
							return true
						})
					} else {
						for i := range doc.Relationships {
							if doc.Relationships[i].Kind == "TESTS" {
								testsCount++
							}
						}
					}
					lr.TestsEdgeCount = testsCount
					lr.setReader(newRdr)
					reloaded++
				}
			}
			// Refresh the semantic vector sidecar independently of the
			// graph mtime — embeddings.bin may be written after the graph
			// by a debounced embed pass. Missing file → Semantic stays nil
			// and the MCP search path falls back to BM25-only.
			if lr.Doc != nil {
				semPath := embed.StorePath(stateDir)
				if fi, statErr := os.Stat(semPath); statErr == nil {
					if !fi.ModTime().Equal(lr.semMtime) {
						if st, lerr := embed.Load(semPath, 0); lerr == nil && st.Len() > 0 {
							lr.Semantic = st
							lr.semMtime = fi.ModTime()
						}
					}
				}
			}
		}
		// Drop repos no longer in the registry.
		// S8 (#2159) + F1 (ADR-0027): route the mmap munmap through the deferred-
		// unmap protocol (retireHandle) rather than a bare Reader.Close(), so an
		// in-flight borrow of an evicted repo's mapping drains before the unmap.
		for rName, lr := range grp.Repos {
			if !seen[rName] {
				if lr != nil {
					lr.retireHandle()
				}
				delete(grp.Repos, rName)
			}
		}
		// Load links file.
		lf := gEntry.LinksFile
		if lf == "" {
			lf = defaultLinksFile(gName)
		}
		grp.LinksFile = lf
		info, err := os.Stat(lf)
		if err == nil && !info.ModTime().Equal(grp.linksMt) {
			links, err := readLinks(lf)
			if err == nil {
				grp.Links = links
				grp.linksMt = info.ModTime()
				reloaded++
			}
		} else if os.IsNotExist(err) {
			grp.Links = nil
		}

		// Apply the group-algo overlay (A2, #5354). Absence-tolerant: when the
		// <group>-algo.json overlay is missing or stale the entities keep
		// whatever graph.fb carried (today's per-repo algo values / sentinels) —
		// NO behavior change until an overlay exists. Memoized by mtime so a
		// mid-session atomic swap reloads only the overlay, not the graphs.
		applyGroupAlgoOverlay(grp)
	}
	// Recompute the tool-surface signature. If it differs from the previous
	// value the caller will emit notifications/tools/list_changed (#1772).
	newSig := computeRegistrySignature(s.registry)
	surfaceChanged := newSig != prevSig
	s.registrySignature = newSig
	_ = registryChanged // retained for future telemetry
	return reloaded, surfaceChanged, nil
}

// Group returns a loaded group by name, or nil.
//
// Overlay-freshness check (#5403): the group-algo overlay is applied at group
// LOAD (during Reload, via applyGroupAlgoOverlay). A long-running daemon caches
// the group, so a settled group whose <group>-algo.json overlay is recomputed
// mid-session (by the scheduler after a reindex, or a manual
// `grafel group-algo --write`) keeps serving the LAST-APPLIED state until a
// restart — the apply was never re-called on the cached group. Here, on the
// canonical group-serving entry path (used by clusters/inspect/orient/stats),
// we cheaply os.Stat the overlay file and, only when its mtime ADVANCES past the
// memoized grp.algoMt, re-call applyGroupAlgoOverlay so the fresh overlay takes
// effect without a reload. The stat is the only per-query cost; the overlay is
// re-read/re-stamped solely when the file genuinely advanced (the #5402 per-repo
// memo then re-stamps exactly the repos that need it). Absence-tolerant: a
// missing overlay → no re-apply (today's behavior). Runs under s.mu, the same
// lock applyGroupAlgoOverlay holds during Reload, so the shared apply/memo
// fields are mutated race-free.
func (s *State) Group(name string) *LoadedGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	grp := s.groups[name]
	if grp != nil {
		s.refreshGroupAlgoOverlayLocked(grp)
	}
	return grp
}

// groupBorrow is the immutable per-call snapshot returned by borrowGroup — the
// F1 (ADR-0027) read-through-captured-handle seam. It captures the group pointer
// AND a borrow on every repo's current MapHandle in one critical section under
// State.mu. A caller reads through the captured handles for the whole call and
// MUST NEVER live-re-dereference lr.handle / lr.Reader / lr.Doc at read time:
// reload mutates *LoadedRepo in place (repointing lr.handle to a successor this
// call never borrow()-incremented), so a live re-deref could hand a reader a
// mapping a concurrent reload is free to unmap → SIGSEGV. The captured handle is
// the read cursor for the entire call; Release() drops every borrow on return.
//
// In F1 the read path is DARK — no handler calls this yet (the borrow protocol
// is inert). It establishes the seam and lifetime guarantee that F2's hot index
// and F3's mmap read path bind to.
type groupBorrow struct {
	Group   *LoadedGroup
	handles []*MapHandle
}

// Handle returns the borrowed MapHandle captured for repo slug, or nil when the
// repo had no mmap mapping at borrow time. The returned handle is the immutable
// read cursor for this call — it stays valid (not munmapped) until Release,
// even across a concurrent reload that retires it.
func (b *groupBorrow) Handle(repo string) *MapHandle {
	if b == nil {
		return nil
	}
	for _, h := range b.handles {
		if h != nil && h.repo == repo {
			return h
		}
	}
	return nil
}

// Release drops every borrow captured by this snapshot. The last releaser of a
// retired handle performs its munmap. Idempotent: safe to call once per borrow.
func (b *groupBorrow) Release() {
	if b == nil {
		return
	}
	for _, h := range b.handles {
		if h != nil {
			h.release()
		}
	}
	b.handles = nil
}

// borrowGroup returns an immutable per-call snapshot of a group with a borrow
// held on each repo's current MapHandle. The group lookup, overlay refresh, and
// every borrow happen in ONE critical section under s.mu, so the borrow cannot
// race reload's publish+retire (which also runs under s.mu): a call either
// borrows before retire (refs>=1 → reload defers the unmap to this call's
// Release) or borrows the already-published successor. The caller MUST defer
// snapshot.Release(). Returns nil when the group is unknown.
//
// F1: inert — no production caller yet. Wired here so the seam and its race
// safety are proven (TestBorrowGroupSurvivesReload) before F3 lights the read
// path.
func (s *State) borrowGroup(name string) *groupBorrow {
	s.mu.Lock()
	defer s.mu.Unlock()
	grp := s.groups[name]
	if grp == nil {
		return nil
	}
	s.refreshGroupAlgoOverlayLocked(grp)
	b := &groupBorrow{Group: grp}
	for _, lr := range grp.Repos {
		if lr != nil && lr.handle != nil {
			b.handles = append(b.handles, lr.handle.borrow())
		}
	}
	return b
}

// refreshGroupAlgoOverlayLocked re-applies the group-algo overlay to an
// already-loaded group when its overlay file mtime has advanced past the
// memoized grp.algoMt (#5403). Cheap by design: a single os.Stat; the overlay
// is re-read and re-stamped only when the file advanced. Absence/stat-error →
// no-op (the overlay may legitimately not exist yet). Caller must hold s.mu.
func (s *State) refreshGroupAlgoOverlayLocked(grp *LoadedGroup) {
	path := grp.algoFile
	if path == "" {
		var err error
		if path, err = groupalgo.OverlayPath(grp.Name); err != nil || path == "" {
			return
		}
	}
	fi, err := os.Stat(path)
	if err != nil {
		// Absent/unreadable overlay → no-op. If the group had previously applied
		// an overlay that has since been removed, applyGroupAlgoOverlay (on the
		// next Reload) handles the clear; per-query we stay non-destructive.
		return
	}
	// Only re-apply when the file genuinely advanced. grp.algoApplied false with a
	// present file (overlay appeared after a load that saw none) also re-applies.
	if grp.algoApplied && !fi.ModTime().After(grp.algoMt) {
		// #5401 residual (#5729): the overlay FILE is unchanged, but a repo may
		// have been reparsed since its last stamp (lr.mtime advanced past
		// lr.algoStampedMt) WITHOUT going through a full Reload — e.g. some other
		// code path swapped a sub-repo's Doc/mtime in memory. Reload always calls
		// applyGroupAlgoOverlay unconditionally and its PER-REPO memo (#5400/#5401)
		// catches this; this read-path early-return did not, so a reparsed repo's
		// entities stayed pinned at the per-repo sentinel (e.g. community_id:-1)
		// until the next full Reload. Fall through to the per-repo-memoized
		// applyGroupAlgoOverlay ONLY when some repo is actually stale, so the
		// steady-state (nothing changed) stays a cheap no-op.
		stale := false
		for _, lr := range grp.Repos {
			if lr != nil && lr.Doc != nil && !lr.mtime.Equal(lr.algoStampedMt) {
				stale = true
				break
			}
		}
		if !stale {
			return
		}
	}
	applyGroupAlgoOverlay(grp)
}

// SweepIdleBM25 drops the BM25 index of every loaded repo whose last search
// borrow is older than idle, returning how many were evicted. It is the idle
// sweep the MCP server fires from its per-call reload hook (reloadBeforeCall):
// a session that has stopped issuing searches releases the heavy BM25 index
// (~313 MB warmed on the corpus) instead of pinning it for the life of the
// process; the next search rebuilds it transparently and single-flighted via
// getBM25. A non-positive idle disables the sweep (returns 0 without locking).
//
// Locking: takes s.mu once and calls evictBM25IfIdle (which takes idxMu) per
// repo — the same s.mu -> idxMu order reload uses (reloadLocked -> resetIndexes),
// so it cannot invert against any read-path getter (which only ever takes idxMu).
func (s *State) SweepIdleBM25(idle time.Duration) int {
	if idle <= 0 {
		return 0
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	evicted := 0
	for _, g := range s.groups {
		for _, lr := range g.Repos {
			if lr != nil && lr.evictBM25IfIdle(idle, now) {
				evicted++
			}
		}
	}
	return evicted
}

// SnapshotGroups returns a stable list of loaded group pointers.
func (s *State) SnapshotGroups() []*LoadedGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*LoadedGroup, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, g)
	}
	return out
}

// Close releases all open mmap readers held by this State.
// Must be called when the State is no longer needed to avoid leaking file
// descriptors and to allow temp-dir cleanup on Windows (which cannot delete
// open mmap'd files).
func (s *State) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.groups {
		for _, lr := range g.Repos {
			if lr != nil {
				// F1 (ADR-0027): retire (not bare Close) so any in-flight borrow
				// drains before the mapping is unmapped — same munmap-while-borrowed
				// hazard as reload/eviction. When nothing is borrowed (the common
				// shutdown case) retire unmaps immediately, matching prior behavior.
				lr.retireHandle()
			}
		}
	}
}

// readDocumentFromDir loads a graph document from a state directory.
// Delegates to graph.LoadGraphFromDir which prefers graph.fb over graph.json
// (ADR-0016, issue #808).
// readDocumentFromDir is a package var (not a plain func) so tests can wrap it
// with a parse counter to assert the #3377 content-hash skip avoids reparsing.
var readDocumentFromDir = func(stateDir string) (*graph.Document, error) {
	return graph.LoadGraphFromDir(stateDir)
}

// hashGraphFile streams the graph file at path through FNV-1a (64-bit) and
// returns the content hash (#3377). Used to detect no-op reindexes: when the
// hash matches the bytes last parsed into Doc, reload skips the reparse +
// LabelIndex rebuild. Streaming (io.Copy) avoids loading the whole file into a
// heap buffer; FNV-1a is non-cryptographic and fast — collision risk on a
// graph.fb is negligible for an in-process freshness check, and a (vanishingly
// unlikely) collision degrades only to a missed reload that the next genuine
// mtime+content change repairs. Returns a non-nil error on open/read failure,
// in which case the caller treats content as changed and reparses.
func hashGraphFile(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	h := fnv.New64a()
	if _, err := io.Copy(h, f); err != nil {
		return 0, err
	}
	return h.Sum64(), nil
}

// readDocument loads a graph document from disk. It receives the
// graph.json path for back-compat with the registry's graphFile()
// helper, derives the state directory, then delegates to
// graph.LoadGraphFromDir which prefers graph.fb when present (ADR-0016
// flip-day, issue #808).
//
// Deprecated: callers should prefer readDocumentFromDir.
func readDocument(graphJSONPath string) (*graph.Document, error) {
	stateDir := filepath.Dir(graphJSONPath)
	return graph.LoadGraphFromDir(stateDir)
}

// readLinks reads the cross-repo links file.
func readLinks(path string) ([]CrossRepoLink, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// File can be either an array or {"links":[...]}.
	var asArr []CrossRepoLink
	if err := json.Unmarshal(data, &asArr); err == nil {
		return asArr, nil
	}
	var asObj struct {
		Links []CrossRepoLink `json:"links"`
	}
	if err := json.Unmarshal(data, &asObj); err != nil {
		return nil, fmt.Errorf("links %s: %w", path, err)
	}
	return asObj.Links, nil
}

// buildTopKPageRank returns a slice of the top-k entity IDs sorted by
// descending PageRank. Built lazily on first ranking use via
// LoadedRepo.getTopKPageRank() (#3367, formerly eager #2304). pickFallback
// reads from this slice instead of iterating Doc.Entities on every call.
func buildTopKPageRank(doc *graph.Document, k int) []string {
	if doc == nil || len(doc.Entities) == 0 {
		return nil
	}
	type ranked struct {
		id string
		pr float64
	}
	all := make([]ranked, 0, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		pr := 0.0
		if e.PageRank != nil {
			pr = *e.PageRank
		}
		all = append(all, ranked{id: e.ID, pr: pr})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].pr > all[j].pr
	})
	if k > len(all) {
		k = len(all)
	}
	out := make([]string, k)
	for i := 0; i < k; i++ {
		out[i] = all[i].id
	}
	return out
}

// buildTopKPageRankFromReader is the mmap-sourced twin of buildTopKPageRank,
// reading Id/Pagerank directly off the fbreader.Reader. PageRank is an
// interface-absent field (no EntityView accessor), so it MUST come from the
// FB Pagerank() scalar. loadFBDocument sets Entity.PageRank to nil when
// Pagerank()==0, so the Document build's pr==0 for those rows exactly matches
// reading Pagerank()==0 here — and identical (id, pr) pairs in identical
// vector order make sort.Slice produce a byte-identical top-K. Byte-identical
// to buildTopKPageRank (proven by TestTopKPageRankReaderParity_PR1).
// ADR-0027 Cutover PR1.
func buildTopKPageRankFromReader(r *fbreader.Reader, k int) []string {
	if r == nil || r.EntityCount() == 0 {
		return nil
	}
	type ranked struct {
		id string
		pr float64
	}
	all := make([]ranked, 0, r.EntityCount())
	r.IterateEntities(func(e *fb.Entity) bool {
		all = append(all, ranked{id: string(e.Id()), pr: e.Pagerank()})
		return true
	})
	sort.Slice(all, func(i, j int) bool {
		return all[i].pr > all[j].pr
	})
	if k > len(all) {
		k = len(all)
	}
	out := make([]string, k)
	for i := 0; i < k; i++ {
		out[i] = all[i].id
	}
	return out
}

// repoIndexedRef returns the git ref that was active when lr was last indexed.
// Falls back to "_unknown" for graphs produced before PH1a (#2088).
func repoIndexedRef(lr *LoadedRepo) string {
	if lr != nil && lr.Doc != nil && lr.Doc.IndexedRef != "" {
		return lr.Doc.IndexedRef
	}
	return "_unknown"
}

// linksForSourceRepo returns every cross-repo link in lg where the source
// side belongs to lr. Results are cached in state.CrossLinkCache keyed by
// (lr.Path, ref, "_all_", "_all_") so that a ref switch on lr triggers fresh
// computation on the next query (issue #2224).
//
// The repo dimension of the cache key uses lr.Path (the absolute on-disk repo
// path) rather than lr.Repo (the registry slug). This matches the key used by
// State.NotifyRefSwitch, which receives the repoPath from BranchSwitchEvent —
// the same path the daemon watcher monitors.
//
// The sentinel values "_all_" for repoB/refB signal "all target repos" —
// this lets InvalidateRepo(repo, oldRef) correctly evict per-source caches
// because the secondary index tracks the A-side (repo, ref) key regardless
// of the B-side sentinel. The B-side is stable ("_all_" never switches ref),
// so the entry is only ever evicted via the A-side hook.
func linksForSourceRepo(st *State, lg *LoadedGroup, lr *LoadedRepo) []CrossRepoLink {
	if lr == nil {
		return nil
	}
	repo := lr.Path // use path so NotifyRefSwitch(repoPath, ref) invalidates correctly
	if repo == "" {
		repo = lr.Repo // fallback for tests that set only Repo
	}
	slug := lr.Repo
	ref := repoIndexedRef(lr)

	if st != nil && st.CrossLinkCache != nil {
		return st.CrossLinkCache.GetOrCompute(repo, ref, "_all_", "_all_", func() []CrossRepoLink {
			return filterLinksBySource(lg.Links, slug)
		})
	}
	return filterLinksBySource(lg.Links, slug)
}

// filterLinksBySource returns the subset of links whose source side belongs
// to the given repo slug.
func filterLinksBySource(links []CrossRepoLink, repo string) []CrossRepoLink {
	var out []CrossRepoLink
	for _, l := range links {
		sr, _ := splitPrefixed(l.Source)
		if sr == repo {
			out = append(out, l)
		}
	}
	return out
}

// applyGroupAlgoOverlay loads the <group>-algo.json overlay (A2, #5354) and, if
// present and NOT stale, stamps the group-scope algo values
// (CommunityID/Centrality/PageRank/IsGodNode/IsArticulationPt) onto the
// in-memory entities by ID, and sets grp.Communities from the overlay summary.
//
// Absence-tolerant: a missing or stale overlay is a no-op — entities keep
// whatever graph.fb carried (per-repo values or sentinels). There is NO
// behavior change until an overlay file exists (which only A3's scheduler
// produces). The overlay is memoized by mtime (mirrors LinksFile/linksMt) so a
// mid-session atomic swap reloads only the overlay.
//
// Re-stamping is idempotent: the values overwrite the same pointer fields each
// reload. A reparse (graph.fb mtime change) resets the entity to its graph.fb
// value first, then this re-applies the (matching) overlay — so a stale overlay
// (which always coincides with a graph.fb mtime change) cleanly falls back.
func applyGroupAlgoOverlay(grp *LoadedGroup) {
	path, err := groupalgo.OverlayPath(grp.Name)
	if err != nil || path == "" {
		return
	}
	grp.algoFile = path

	fi, statErr := os.Stat(path)
	if statErr != nil {
		// Absent overlay → no-op. Clear any memoized mtime so a later-created
		// overlay is picked up; leave entity fields as graph.fb carried them.
		grp.algoMt = time.Time{}
		grp.algoApplied = false
		return
	}

	cur, mtErr := groupalgo.CurrentSourceMtimes(grp.Name)
	if mtErr != nil {
		return
	}
	ov, ok := groupalgo.ReadOverlay(path, cur)
	if !ok {
		// Stale or corrupt → no-op (fall back to graph.fb values). A stale
		// overlay coincides with a repo reparse, which already reset the fields.
		grp.algoMt = time.Time{}
		grp.algoApplied = false
		return
	}

	// The community summary is always cheap to refresh and keeps grp.Communities
	// authoritative for the clusters path even when no per-entity re-stamp is
	// needed this reload.
	grp.Communities = ov.Communities

	// Memoization is now PER REPO, not group-wide (#5400/#5401). The previous
	// group-level memo skipped ALL re-stamping whenever the overlay file mtime
	// was unchanged — but a repo's graph.fb can be rewritten AFTER the overlay
	// was first applied (a reparse produces fresh doc.Entities carrying the
	// per-repo sentinel community ids, e.g. -1). With the group memo, that
	// reparsed repo never got re-stamped and silently reverted to
	// community_id:-1 (the acme-mobile symptom in #5401; the same staleness
	// also left grafel_inspect surfacing nothing in #5400). We re-stamp a repo
	// whenever EITHER the overlay file advanced (overlayChanged) OR that repo's
	// graph.fb was reparsed since we last stamped it (lr.mtime moved). This
	// re-stamps exactly the repos that need it and is a no-op for the steady
	// state where neither the overlay nor any graph changed.
	overlayChanged := !grp.algoApplied || !fi.ModTime().Equal(grp.algoMt)

	for _, lr := range grp.Repos {
		if lr == nil || lr.Doc == nil {
			continue
		}
		// Skip a repo only when the overlay is unchanged AND this repo was not
		// reparsed since we last stamped it. The zero algoStampedMt (never
		// stamped) always falls through to (re-)stamp.
		if !overlayChanged && lr.mtime.Equal(lr.algoStampedMt) {
			continue
		}
		ents := lr.Doc.Entities
		for i := range ents {
			eo, has := ov.Results[ents[i].ID]
			if !has {
				continue
			}
			cid := eo.CommunityID
			pr := eo.PageRank
			cen := eo.Centrality
			ents[i].CommunityID = &cid
			ents[i].PageRank = &pr
			ents[i].Centrality = &cen
			ents[i].IsGodNode = eo.IsGodNode
			ents[i].IsArticulationPt = eo.IsArticulationPoint
		}
		// Re-arm lazy derived indexes (TopKPageRank etc.) so they rebuild
		// against the freshly-stamped group values rather than stale per-repo
		// ones (mirrors the resetIndexes() call after a reparse).
		lr.resetIndexes()
		lr.algoStampedMt = lr.mtime
	}

	grp.algoMt = fi.ModTime()
	grp.algoApplied = true
}

// defaultLinksFile is the conventional path for cross-repo links.
func defaultLinksFile(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grafel", "groups", group+"-links.json")
}

// defaultMemoryDir is the conventional path for save_finding outputs.
func defaultMemoryDir(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grafel", "groups", group+"-memory")
}

// defaultLinkCandidatesFile is the on-disk file for pending link candidates.
func defaultLinkCandidatesFile(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grafel", "groups", group+"-link-candidates.json")
}

// defaultRegistryPath is "~/.grafel/registry.json".
func defaultRegistryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "registry.json"
	}
	return filepath.Join(home, ".grafel", "registry.json")
}
