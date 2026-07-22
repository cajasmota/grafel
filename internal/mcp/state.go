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
	"runtime"
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
	handle *MapHandle
	// readerMu is the ADR-0027 read-side SIGBUS-safety mutex (memory epic #5850,
	// "Option B") that gates the GRAFEL_SERVE_FROM_MMAP flag-on read path. It is
	// STRICTLY INNERMOST: nothing else is acquired while it is held. The 4 flag-on
	// read choke points (LabelIndex.at + the three build*AdjacencyFromReader
	// getters) hold it around their mmap dereference; publishHandle/retireHandle
	// hold it around the predecessor's munmap (setting handle.readRetired true
	// first). Because it sits below idxMu in the order (read: idxMu->readerMu, or
	// bare readerMu at at()) and reload already holds s.mu (reload:
	// s.mu->...->readerMu), a deref and the munmap of that same mapping can never
	// interleave and no lock inversion is possible. Its zero value is usable, so a
	// LoadedRepo needs no explicit init. The wired *LabelIndex holds rmu() so a
	// lookup on a pre-reload-captured index locks the SAME mutex.
	//
	// NEVER lock readerMu directly — go through rmu() (below). A keepReader
	// eviction (issue #5872) builds a cold-shell twin that shares this repo's
	// *MapHandle but is a DISTINCT *LoadedRepo; both generations MUST serialize
	// reads-vs-munmap of the shared mapping on ONE mutex, or a retire under the
	// shell's mutex races an in-flight read under the origin's mutex → data race
	// on MapHandle.readRetired + SIGSEGV on the munmapped region. rmu() delivers
	// that single mutex; sharedReaderMu threads it across generations.
	readerMu sync.Mutex
	// sharedReaderMu, when non-nil, overrides readerMu as this repo's effective
	// reads-vs-retire mutex. It is set ONLY by coldShellRepo, to the ORIGIN
	// generation's rmu(), so every cold-shell/revived *LoadedRepo derived from one
	// original — all sharing that original's *MapHandle — locks the SAME mutex as
	// the original and any in-flight reader still holding it (issue #5872, epic
	// #5850). Nil on every freshly-loaded repo (rmu() then returns &readerMu), so
	// the zero value stays usable and non-shell construction is unchanged.
	sharedReaderMu *sync.Mutex
	LabelIndex     *LabelIndex
	BM25           *BM25Index
	Semantic       *embed.Store // per-repo vector index (nil when no embeddings.bin)
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
	byID         map[string]*graph.Entity // entity ID -> entity, flag-OFF resident cache (#1656)
	// byIDIdx is the FLAG-ON resident getByID cache (memory epic #5850 Path P): it
	// maps entity ID -> vector INDEX and holds NO *graph.Entity, so it cannot
	// re-pin the ~608 MB entity set that the mmap flip moves out of the Go heap.
	// getByID resolves each index to an entity ON DEMAND via the readerMu-guarded
	// LabelIndex.at (Reader base + overlay side-table). Built once per reload under
	// byIDOnce, cleared by resetIndexes. Nil on the flag-OFF default path (which
	// keeps byID above — Doc is retained flag-OFF anyway, so pointer retention
	// there is free). Mirrors the BM25 int32-index no-retention build (PR3b L4).
	byIDIdx      map[string]int32
	topKPageRank []string // entity IDs sorted descending by PageRank (#2304)
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
	lr.byIDIdx = nil
	lr.topKPageRank = nil
	lr.suffixIndex = nil
	lr.suffixIndexPartial = false
}

// rmu returns this repo's effective reads-vs-retire mutex: the shared override
// (a cold-shell twin's link to its origin generation's mutex) when set, else this
// repo's own readerMu. Every LoadedRepo-receiver mmap-read choke point, every
// publishHandle/retireHandle munmap, and every LabelIndex readerMu wiring MUST go
// through this — never lr.readerMu directly — so all generations sharing one
// *MapHandle serialize on ONE mutex (issue #5872). Cheap: a nil check + address-of
// on the fast (non-shell) path; never mutated after coldShellRepo, so it needs no
// lock of its own.
func (lr *LoadedRepo) rmu() *sync.Mutex {
	if lr.sharedReaderMu != nil {
		return lr.sharedReaderMu
	}
	return &lr.readerMu
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
	// ADR-0027 SIGBUS-safety (memory epic #5850): the whole swap runs under the
	// strictly-innermost readerMu so a flag-on read choke point sees a consistent
	// (lr.Reader/lr.handle) pair and never derefs a mapping being munmapped here.
	// The predecessor is flagged readRetired BEFORE its munmap so a stale captured
	// *LabelIndex checking readRetired (under this same readerMu) falls back to the
	// Doc instead of dereferencing the freed region. readerMu is innermost: the
	// only thing done while holding it is field assignment + old.retire()'s munmap
	// syscall — no other grafel lock is acquired.
	lr.rmu().Lock()
	old := lr.handle
	if nh != nil {
		nh.repo = lr.Repo
		lr.Reader = nh.reader
	} else {
		lr.Reader = nil
	}
	lr.handle = nh
	if old != nil {
		old.readRetired = true
		old.retire()
	}
	lr.rmu().Unlock()
}

// retireHandle retires the repo's current mmap handle (repo eviction and server
// Close), routing the munmap through the F1 drain instead of a bare
// lr.Reader.Close() — same munmap-while-borrowed hazard as reload. The mapping
// is unmapped now iff it has drained, else by the last release(). Caller MUST
// hold State.mu.
func (lr *LoadedRepo) retireHandle() {
	// Same readerMu discipline as publishHandle: flag readRetired then munmap, all
	// under the strictly-innermost readerMu, so a concurrent flag-on read choke
	// point cannot deref this mapping while (or after) it is unmapped.
	lr.rmu().Lock()
	old := lr.handle
	lr.handle = nil
	lr.Reader = nil
	if old != nil {
		old.readRetired = true
		old.retire()
	}
	lr.rmu().Unlock()
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

// getByID returns the entity-ID → *Entity map, building the cache on first use.
//
// Memory epic #5850 Path P — de-retain byID. The resident cache is gated by
// GRAFEL_SERVE_FROM_MMAP so the flag-ON path pins NO entity pointers (the flip
// prerequisite; post-flip lr.Doc is emptied and a retained entity map here would
// re-pin the whole ~608 MB entity set in the Go heap, defeating the flip):
//
//   - Flag-ON (Reader resident): the resident cache is lr.byIDIdx — ID → vector
//     INDEX (map[string]int32), learned once from the LabelIndex's own int32 map.
//     getByID resolves each index to an entity ON DEMAND via the readerMu-guarded
//     LabelIndex.at (Reader base + overlay side-table, byte-equal to the overlaid
//     Doc row) and returns a TRANSIENT map[string]*graph.Entity that the caller
//     drops after use — nothing entity-shaped stays resident. The bound is the
//     Reader's row count (EntityCount), so a PR7 Doc-emptying does not collapse
//     the result. Mirrors the BM25 int32-index no-retention build (PR3b L4).
//   - Flag-OFF (default): the resident cache is lr.byID — INDEPENDENT heap copies
//     of the live Doc rows (ADR-0027 PR2), so post-load in-place overlay stamps
//     stay visible, GC-safe, with NO handler-path mmap read. Doc is retained
//     flag-OFF anyway, so this pointer retention is free. Built once and cached,
//     so the values are INTERNALLY STABLE for the life of the reload.
//
// The mmap path short-circuits to the flag-OFF Doc build whenever at() cannot
// serve an index (nil LabelIndex/Reader, JSON load). getByID consumers index the
// returned map by id and never compare value pointers across calls, so they are
// insulated from the per-lookup pointer instability of LabelIndex.at.
func (lr *LoadedRepo) getByID() map[string]*graph.Entity {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.buildByIDCacheLocked()

	if lr.byIDIdx != nil {
		// Flag-ON: resolve every cached index to an entity ON DEMAND via the
		// readerMu-guarded LabelIndex.at (Reader base + overlay side-table). The
		// returned map is transient — the caller drops it, so no entity stays
		// resident. A retired mapping falls back to the Doc inside at() (nil when
		// Doc is emptied), never a read-after-munmap.
		//
		// This whole-map build materializes the ENTIRE entity set per call, so it
		// is reserved for genuine iterate-many callers (lookup tables built across
		// forEachEntity/forEachRelationship). Single/few-id callers MUST use
		// getByIDOne, which materializes exactly one entity.
		out := make(map[string]*graph.Entity, len(lr.byIDIdx))
		for id, i := range lr.byIDIdx {
			if ent := lr.LabelIndex.at(i); ent != nil {
				out[id] = ent
			}
		}
		return out
	}
	return lr.byID
}

// getByIDOne resolves a SINGLE entity by id, materializing exactly one entity on
// the flag-ON path (never the whole entity set) — the accessor for the single/
// few-lookup callers (memory epic #5850 Path P). It returns (entity, true) when
// present, (nil, false) when the id is unknown OR the mmap mapping was retired
// out from under a lookup (at() returns nil). Byte-identical to today's
// getByID()[id] / `_, ok := getByID()[id]` on the flag-OFF path.
//
// Flag-ON it looks up the resident int32 index cache (lr.byIDIdx, built/reused
// under the same byIDOnce/idxMu as getByID) and resolves the ONE index via the
// readerMu-guarded LabelIndex.at. Flag-OFF it indexes the memoized Doc-backed
// map (lr.byID). Sharing byIDOnce means getByID and getByIDOne never build the
// cache twice for a repo.
func (lr *LoadedRepo) getByIDOne(id string) (*graph.Entity, bool) {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.buildByIDCacheLocked()

	if lr.byIDIdx != nil {
		idx, ok := lr.byIDIdx[id]
		if !ok {
			return nil, false
		}
		ent := lr.LabelIndex.at(idx)
		if ent == nil {
			return nil, false // retired mapping / out-of-range — graceful miss
		}
		return ent, true
	}
	e, ok := lr.byID[id]
	return e, ok
}

// buildByIDCacheLocked populates the reload-scoped getByID cache exactly once
// (guarded by byIDOnce). Caller MUST hold idxMu. Flag-ON it builds lr.byIDIdx
// (ID → vector index, NO entities retained); flag-OFF it builds the Doc-backed
// lr.byID map. Shared by getByID and getByIDOne.
func (lr *LoadedRepo) buildByIDCacheLocked() {
	lr.byIDOnce.Do(func() {
		mmapSourced := serveFromMMap() && lr.LabelIndex != nil && lr.LabelIndex.reader != nil
		if mmapSourced {
			// Learn ID → vector index once, retaining ONLY the int32 map. The
			// LabelIndex already holds the exact ID → index mapping (built from the
			// Reader in vector order); copy it into an independent resident cache so
			// getByID's lifetime is decoupled from LabelIndex churn. No *graph.Entity
			// is materialized or retained here.
			idxMap := make(map[string]int32, len(lr.LabelIndex.byID))
			for id, i := range lr.LabelIndex.byID {
				idxMap[id] = i
			}
			lr.byIDIdx = idxMap
			return
		}
		// Flag-OFF: memoize the Doc-backed *graph.Entity map (unchanged, PR2).
		if lr.Doc == nil {
			lr.byID = map[string]*graph.Entity{}
			return
		}
		m := make(map[string]*graph.Entity, len(lr.Doc.Entities))
		for i := range lr.Doc.Entities {
			ent := lr.Doc.Entities[i] // heap copy — a fresh pointer, not an alias into Doc
			m[ent.ID] = &ent
		}
		lr.byID = m
	})
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
		// ADR-0027 Cutover / issue #5871 L4 (memory epic #5850, Path P PR3b):
		// build the BM25 index from the resident mmap Reader when ON, else the
		// Document. This is the FLIP PREREQUISITE — post-flip lr.Doc is emptied,
		// so BuildBM25(lr.Doc) would build an EMPTY index; the Reader still holds
		// every row. Flag-gated (default OFF) for the same handler-path munmap
		// SIGBUS reason as getAdjacency. ADR-0027 SIGBUS-safety (#5850): the mmap
		// read (BuildBM25FromReader materializes every row) + a concurrent
		// reload's munmap are serialized by the strictly-innermost readerMu, so
		// the mapping cannot be freed mid-scan. Lock order: idxMu (held) ->
		// readerMu; the Doc build takes no mmap so readerMu is dropped first.
		lr.rmu().Lock()
		rdr := lr.Reader
		h := lr.handle
		if rdr != nil && serveFromMMap() && (h == nil || !h.readRetired) {
			idx := BuildBM25FromReader(rdr)
			// Search resolves each ranked vector index -> *graph.Entity at return
			// time via the readerMu-guarded, materialize-on-demand LabelIndex.at
			// (mmap Reader base + group-algo overlay side-table) — NOT a retained
			// pointer slice. Capture the current LabelIndex generation so the
			// resolver stays byte-consistent with the graph.fb this index was
			// built from; at() takes readerMu itself and falls back to the Doc if
			// the mapping is retired, so it is safe to call lock-free from Search.
			li := lr.LabelIndex
			idx.resolve = func(vi int32) *graph.Entity { return li.at(vi) }
			lr.BM25 = idx
			lr.rmu().Unlock()
		} else {
			lr.rmu().Unlock()
			lr.BM25 = BuildBM25(lr.Doc)
		}
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
		// order), ELSE the Document. Gated behind GRAFEL_SERVE_FROM_MMAP (default
		// OFF): this getter runs on the HANDLER path (idxMu only), and a
		// concurrent reload retire()s+munmaps the old Reader WITHOUT idxMu (F1's
		// borrow protocol is inert → refs==0 → immediate munmap), so an
		// unconditional Reader iteration here is a read-after-unmap SIGBUS (latent
		// PR1 #5865). OFF → the GC-safe Document build. Nil-Reader always uses the
		// Document.
		//
		// ADR-0027 SIGBUS-safety (memory epic #5850): read lr.Reader/lr.handle and
		// run the mmap build UNDER the strictly-innermost readerMu, so a concurrent
		// reload's munmap (also under readerMu) cannot free the mapping mid-scan.
		// readRetired is checked for uniformity with at() (lr.handle here is always
		// the current, non-retired generation). Lock order: idxMu (held) -> readerMu;
		// the Doc build takes no mmap so readerMu is dropped before it.
		lr.rmu().Lock()
		rdr := lr.Reader
		h := lr.handle
		if rdr != nil && serveFromMMap() && (h == nil || !h.readRetired) {
			lr.adjacency = buildAdjacencyFromReader(rdr, lr.Repo)
			lr.rmu().Unlock()
		} else {
			lr.rmu().Unlock()
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
		// ADR-0027 Cutover PR1: prefer the resident mmap Reader when ON, else the
		// Document. Flag-gated (default OFF) for the same handler-path munmap
		// SIGBUS reason as getAdjacency. ADR-0027 SIGBUS-safety (#5850): the mmap
		// read + reload munmap are serialized by the strictly-innermost readerMu.
		lr.rmu().Lock()
		rdr := lr.Reader
		h := lr.handle
		if rdr != nil && serveFromMMap() && (h == nil || !h.readRetired) {
			lr.callsAdj = buildCallsAdjacencyFromReader(rdr)
			lr.rmu().Unlock()
		} else {
			lr.rmu().Unlock()
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
		// ADR-0027 Cutover PR1: prefer the resident mmap Reader when ON, else the
		// Document. Flag-gated (default OFF) for the same handler-path munmap
		// SIGBUS reason as getAdjacency. ADR-0027 SIGBUS-safety (#5850): the mmap
		// read + reload munmap are serialized by the strictly-innermost readerMu.
		lr.rmu().Lock()
		rdr := lr.Reader
		h := lr.handle
		if rdr != nil && serveFromMMap() && (h == nil || !h.readRetired) {
			lr.stepAdj = buildStepAdjacencyFromReader(rdr)
			lr.rmu().Unlock()
		} else {
			lr.rmu().Unlock()
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
		// Real PageRank is NEVER in graph.fb: per-repo Pass-4 (the per-repo
		// PageRank/CommunityID/Centrality/god/articulation compute) was removed
		// when the group-scope algo pass (A1-A3, #5349) replaced it, so graph.fb's
		// Pagerank() scalar is a PERMANENT sentinel (0) for every entity. The one
		// authoritative source of real PageRank is applyGroupAlgoOverlay, from the
		// <group>-algo.json overlay.
		//
		//   - Flag-OFF (default): the overlay is stamped IN PLACE onto
		//     lr.Doc.Entities[i].PageRank, so source from lr.Doc. This is
		//     byte-identical to the pre-PR5 behavior.
		//   - Flag-ON (flip-ready): lr.Doc is (post-PR7) empty; the real PageRank
		//     lives in the overlay SIDE-TABLE. buildTopKPageRankFromSideTable reads
		//     the entity ids from the Reader (vector order) and PageRank from the
		//     side-table (overlay[i].PageRank) — it MUST NOT read the Reader's
		//     Pagerank() scalar (the permanent sentinel), which would collapse
		//     top-K to id order and corrupt pickFallback (the reverted PR1 #5866
		//     bug — see buildTopKPageRankFromReader's BLOCKED doc comment).
		if serveFromMMap() {
			lr.topKPageRank = lr.buildTopKPageRankFromSideTable(64)
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

	// lastAccess is the wall-clock time of the most recent State.Group(name)
	// routing decision for this group — the per-call choke point every tool call
	// passes through to resolve its target group. s.mu-guarded (written in
	// State.Group, read in SweepIdleGroups, both under s.mu). It is the idle-group
	// LRU signal AND the PIN signal (memory epic #5850, issue #5872 PR2): the idle
	// sweep evicts a whole group whose now-lastAccess exceeds the configured window
	// but NEVER the currently-active group (the max-lastAccess group — the working
	// set the fleet is servicing). Zero value means the group has been loaded but
	// never routed to through State.Group (e.g. eager startup warm never queried):
	// mirroring bm25LastUse, such a group is NOT idle-eligible until first access.
	lastAccess time.Time
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

	// evicted is the per-group whole-graph LRU-eviction cold-gate (memory epic
	// #5850, issue #5872 PR1). A group name present here has been reclaimed by
	// EvictGroup and swapped OUT of s.groups; the gate keeps reloadAllLocked from
	// eagerly resurrecting it before it is explicitly accessed again.
	//
	//   - value nil  → full evict (keepReader=false): the mmap was munmapped and
	//     the whole heavy group dropped; revive reloads from disk.
	//   - value !nil → keepReader evict: a lightweight COLD SHELL whose repos kept
	//     their mmap Readers resident (LabelIndex/derived-index heap dropped);
	//     revive re-materializes the heap from those Readers with no re-Open.
	//
	// Mutated only under s.mu. Nil until the first EvictGroup call. No policy
	// lives here in PR1 — this is the eviction PRIMITIVE's bookkeeping only.
	evicted map[string]*LoadedGroup

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

	// memSampler is the resident-memory sampler used by the memory-watermark
	// eviction trigger (SweepToMemoryBudget, memory epic #5850, issue #5872 PR3).
	// Nil in production → heapAllocSample (runtime.ReadMemStats().HeapAlloc, the
	// metric group eviction actually reclaims — see the PR3 grounding doc). Tests
	// inject a deterministic closure. Read under s.mu inside SweepToMemoryBudget.
	memSampler func() uint64
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
	return s.reloadAllLocked()
}

// reloadAllLocked is the reload body; the caller MUST already hold s.mu. Split
// out of reloadLocked (memory epic #5850 PR1) so the eviction revive path can
// reload from disk while already holding s.mu (Group -> reviveEvictedLocked)
// without re-taking the lock.
func (s *State) reloadAllLocked() (int, bool, error) {
	prevSig := s.registrySignature
	registryChanged := s.refreshRegistryFromDisk()
	reloaded := 0
	for gName, gEntry := range s.registry.Groups {
		// Cold-gate (memory epic #5850, issue #5872 PR1): a group EvictGroup put
		// in s.evicted is intentionally NOT resident. Skip it so reload does not
		// eagerly resurrect every registered group before it is touched — an
		// evicted group stays evicted until an explicit State.Group access revives
		// it (reviveEvictedLocked). Without this gate the loop below would recreate
		// the LoadedGroup and reload every repo from disk on the very next reload,
		// defeating the eviction.
		if _, gated := s.evicted[gName]; gated {
			continue
		}
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
					// ADR-0027 Cutover PR2: open the mmap Reader BEFORE building the
					// LabelIndex so the index sources entity indices (int32 positions)
					// and MATERIALIZES each lookup from the resident mmap — byte-
					// identical to the Document rows (PR1) — instead of pinning
					// *graph.Entity pointers into Doc.Entities (the PR7 flip removes
					// Doc.Entities entirely). Best-effort: Open failure leaves newRdr
					// nil and the index falls back to the Document (JSON-only path).
					// The SAME reader is published via setReader below (F1 protocol).
					// #5891: open the SAME resolved graph file that was
					// discovered above (lr.GraphFile, via CurrentGraphPath in
					// FindGraphFileAnyRef), NOT a re-derived flat graph.fb —
					// under the gen layout the flat path may not exist, and
					// re-deriving it would open a stale/absent file. Guard on the
					// .fb extension exactly as the old hardcoded graph.fb path did
					// implicitly: lr.GraphFile may be a graph.json (json-only
					// repo), and handing JSON bytes to fbreader.Open crashes with
					// an out-of-range slice — the mmap-reader is only meaningful
					// for the FlatBuffers format (gen graph.<gen>.fb or flat
					// graph.fb), so a json-only repo correctly leaves newRdr nil.
					//
					// #5901 segment-set: when the active graph is the
					// multi-segment gen-dir layout, lr.GraphFile does NOT carry a
					// .fb extension (it is a graph.<gen>/ dir or a graph.json
					// fallback), so newRdr stays nil and this repo is served from
					// the Document that readDocumentFromDir → LoadGraphFromDir
					// already materialized segment-aware. The zero-copy mmap
					// cutover (a resident *MultiReader + LabelIndex/BM25/adjacency
					// sourcing rows straight from segment mmaps) is a later serve
					// slice of #5890 — this dark read substrate only guarantees the
					// segment-set is READABLE (via the Doc), never mis-mmapped as a
					// single file. filepath.Ext of a gen dir is "" so the guard
					// below already excludes it; the single-file path is unchanged.
					fbPath := lr.GraphFile
					var newRdr *fbreader.Reader
					if filepath.Ext(fbPath) == ".fb" {
						if rdr, rErr := fbreader.Open(fbPath); rErr == nil {
							newRdr = rdr
						}
					}
					// ADR-0027 SIGBUS-safety (memory epic #5850): build the successor
					// MapHandle up front so the reader-sourced LabelIndex is FULLY wired
					// (readerMu + this generation's retirement handle) BEFORE it is
					// published to lr.LabelIndex — a handler live-reading lr.LabelIndex
					// never sees a half-wired index, and at()'s mmap deref is serialized
					// (its stale-capture fallback armed) against this handle's later
					// munmap. publishHandle below installs this SAME handle.
					var newHandle *MapHandle
					if newRdr != nil {
						newHandle = newMapHandle(newRdr)
						li := BuildLabelIndexFromReader(newRdr, doc)
						li.readerMu = lr.rmu()
						li.handle = newHandle
						// ADR-0027 SIGBUS-safety (memory epic #5850, Path P PR1 /
						// view_iter.go): publish lr.LabelIndex under the SAME
						// readerMu that guards lr.Reader/lr.handle (publishHandle
						// below) so forEachEntity's flag-on scan — which holds
						// readerMu across the whole scan and reads lr.LabelIndex.
						// overlay for the group-algo merge — never observes a
						// torn/concurrent write to this field. Field assignment
						// only; no other lock is acquired while held (matches the
						// readerMu contract documented on LoadedRepo.readerMu).
						lr.rmu().Lock()
						lr.LabelIndex = li
						lr.rmu().Unlock()
					} else {
						lr.rmu().Lock()
						lr.LabelIndex = BuildLabelIndex(doc)
						lr.rmu().Unlock()
					}
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
					// S8 (#2159) + F1 (ADR-0027): publish the successor (newRdr, opened
					// above alongside the LabelIndex) through the deferred-unmap protocol
					// instead of a bare Close() of the old reader. setReader repoints
					// lr.handle/lr.Reader to the new mapping FIRST, then retires the
					// predecessor — which unmaps it now iff it has already drained, else
					// the last in-flight release() unmaps it. Reload never waits on
					// borrows and never munmaps in place. Passing nil on Open failure
					// retires the stale predecessor and leaves Reader nil; callers then
					// fall back to doc.Entities / doc.Relationships.
					// TESTS-edge count cached once per reload so grafel_whoami can
					// return it in O(1) without rescanning all relationships. This is a
					// cheap O(R) count with no allocation, so it stays eager (#3325).
					//
					// ADR-0027 Cutover PR1: count TESTS-kind edges off the freshly
					// opened mmap Reader (Kind() read directly) rather than the
					// materialized Document. Byte-neutral (Reader == Document's rows).
					// Gated behind GRAFEL_SERVE_FROM_MMAP (default OFF) so the whole
					// cutover shares ONE switch and no mmap read happens off the flag;
					// falls back to doc.Relationships when OFF or no graph.fb is mapped.
					testsCount := 0
					if newRdr != nil && serveFromMMap() {
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
					lr.publishHandle(newHandle)
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
	if grp == nil {
		// Cold-gate revive (memory epic #5850, issue #5872 PR1): a group EvictGroup
		// reclaimed is absent from s.groups and pinned in s.evicted. An explicit
		// access is exactly the "touch" that ends the eviction — bring it back on
		// demand (re-materialize from the retained Reader, or reload from disk).
		if cold, gated := s.evicted[name]; gated {
			grp = s.reviveEvictedLocked(name, cold)
		}
	}
	if grp != nil {
		// Stamp the LRU/PIN signal on every routing decision (issue #5872 PR2).
		// This is the per-call choke point, so the group with the newest lastAccess
		// is definitionally the active working set — SweepIdleGroups pins it. It is
		// also why a group queried within the idle window is never idle-evicted, and
		// why a revive (below) immediately re-arms the signal for the freshly
		// re-materialized group.
		grp.lastAccess = time.Now()
		s.refreshGroupAlgoOverlayLocked(grp)
	}
	return grp
}

// EvictGroup reclaims the resident graph of group name — the per-group whole-graph
// LRU-eviction PRIMITIVE (memory epic #5850, issue #5872 PR1). It is the clean
// RSS-reclamation lever for an idle group in an agent fleet. Returns true when a
// resident group was evicted, false when name was not resident.
//
// PR1 is the primitive + cold-gate ONLY — no policy, LRU bookkeeping, watermark,
// or knobs, and no production caller (tests only). Those are PR2/PR3.
//
// Safety (the load-bearing part). The serve read path is lock-free: a handler that
// already called State.Group holds its own *LoadedGroup / *LoadedRepo pointers and
// dereferences their fields WITHOUT any lock. So eviction MUST NOT nil live fields
// on a struct such a reader might still hold — instead it SWAPS the whole group out
// of s.groups (delete below): new lookups miss immediately, existing holders finish
// against the still-valid old struct, and the heavy heap is reclaimed by GC once
// they drop it. The mmap is always released through the F1 retireHandle drain, never
// a raw munmap, so an in-flight borrow drains first.
//
//   - keepReader=false → full evict: each repo's Reader is retired (munmapped via the
//     safe drain) and the whole old group is discarded. A later access reloads the
//     group from disk.
//   - keepReader=true → keep the mmap mapped: a lightweight COLD SHELL takes over each
//     repo's Reader/handle (transferred, NOT retired — the file stays mapped) while
//     the heavy old group (LabelIndex + adjacency/BM25/byIDIdx/overlay/… heap) is
//     dropped to GC. A later access re-materializes the heap from the already-mapped
//     Readers with no fbreader.Open and no disk re-read.
func (s *State) EvictGroup(name string, keepReader bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evictGroupLocked(name, keepReader)
}

// evictGroupLocked is EvictGroup's body with the lock already held. Extracted
// (issue #5872 PR2) so the idle-group sweep (SweepIdleGroups) can evict several
// idle groups inside ONE s.mu critical section without re-entrant locking.
// Caller MUST hold s.mu.
func (s *State) evictGroupLocked(name string, keepReader bool) bool {
	grp := s.groups[name]
	if grp == nil {
		return false
	}
	// Swap the whole group out FIRST: new State.Group / borrowGroup lookups miss
	// from here on, while any lock-free reader mid-handler keeps its own pointers.
	delete(s.groups, name)
	if s.evicted == nil {
		s.evicted = map[string]*LoadedGroup{}
	}

	if !keepReader {
		// Full evict: munmap every repo's mapping through the F1 drain (defers if a
		// borrow is live) and drop the heavy group entirely. Revive reloads from disk.
		for _, lr := range grp.Repos {
			if lr != nil {
				lr.retireHandle()
			}
		}
		s.evicted[name] = nil
		return true
	}

	// keepReader: transfer each repo's live mapping to a fresh cold shell that
	// carries ONLY the fields a re-materialization needs; the heavy old group is
	// left untouched (safe for in-flight readers) and becomes garbage. The mmap is
	// NOT retired — it stays mapped, owned henceforth by the shell.
	shell := &LoadedGroup{
		Name:      grp.Name,
		Repos:     make(map[string]*LoadedRepo, len(grp.Repos)),
		Links:     grp.Links,
		LinksFile: grp.LinksFile,
		linksMt:   grp.linksMt,
		MemoryDir: grp.MemoryDir,
		// Overlay re-application is FORCED on revive (algoApplied=false) so the
		// freshly rebuilt LabelIndex re-acquires its group-algo values / side-table.
		algoFile: grp.algoFile,
	}
	for rName, lr := range grp.Repos {
		if lr == nil {
			continue
		}
		shell.Repos[rName] = coldShellRepo(lr)
	}
	s.evicted[name] = shell
	return true
}

// coldShellRepo builds the keepReader cold-shell twin of lr: a fresh *LoadedRepo
// that takes over lr's mmap Reader/handle and header/meta but carries NONE of the
// dropped heap (LabelIndex nil, every lazy derived index zero). The mapping is
// transferred (not retired), so a revive re-materializes the heap from it with no
// fbreader.Open. The old lr is left fully intact for any lock-free reader still
// holding it and is otherwise unreferenced (GC reclaims its heap); it has no
// finalizer, so it never munmaps — only the shell (or a later full evict/Close)
// ever retires the shared handle, and no borrow is outstanding on the dark read
// path, so the transfer cannot double-free.
func coldShellRepo(lr *LoadedRepo) *LoadedRepo {
	return &LoadedRepo{
		Repo:      lr.Repo,
		Path:      lr.Path,
		GraphFile: lr.GraphFile,
		Doc:       lr.Doc,
		Reader:    lr.Reader,
		handle:    lr.handle,
		// sharedReaderMu is the load-bearing fix for issue #5872: the shell shares
		// lr's *MapHandle (above), so it MUST serialize reads-vs-munmap of that one
		// mapping on the SAME mutex as lr and any in-flight reader still holding lr.
		// Point at lr's EFFECTIVE mutex (rmu(), not a fresh zero-value) so a later
		// retireHandle on the shell and an old-generation at()/forEach read lock the
		// identical *sync.Mutex. Threading rmu() (rather than &lr.readerMu) also keeps
		// a shell-of-a-shell collapsed onto the one origin mutex.
		sharedReaderMu: lr.rmu(),
		Semantic:       lr.Semantic,
		semMtime:       lr.semMtime,
		TestsEdgeCount: lr.TestsEdgeCount,
		mtime:          lr.mtime,
		contentHash:    lr.contentHash,
		// algoStampedMt is PRESERVED (not zeroed): the graph did not reparse during
		// eviction, so the per-repo overlay-staleness check must stay "not stale".
		// The single load-bearing trigger that forces revive to re-apply the overlay
		// (rebuilding the LabelIndex side-table dropped with the heap) is the shell
		// group's algoApplied=false, set in EvictGroup — not a spurious stale mtime.
		algoStampedMt: lr.algoStampedMt,
	}
}

// reviveEvictedLocked ends the eviction of name and returns the now-resident group.
// Caller MUST hold s.mu. cold is the s.evicted[name] value (nil for a full evict, a
// cold shell for a keepReader evict).
func (s *State) reviveEvictedLocked(name string, cold *LoadedGroup) *LoadedGroup {
	delete(s.evicted, name)
	if cold != nil {
		// keepReader: re-materialize each repo's dropped heap from its retained,
		// still-mapped Reader — no fbreader.Open, no disk re-read — then reinstall.
		for _, lr := range cold.Repos {
			if lr != nil {
				lr.rematerializeFromReaderLocked()
			}
		}
		s.groups[name] = cold
		return cold
	}
	// Full evict: the group is gone from both maps and the gate is now clear, so
	// reloadAllLocked recreates it and loads it from disk. Other still-gated groups
	// stay skipped; already-resident groups mtime/hash-skip, so this is targeted.
	_, _, _ = s.reloadAllLocked()
	return s.groups[name]
}

// rematerializeFromReaderLocked rebuilds the heap indexes dropped by a keepReader
// eviction from this repo's retained mmap Reader, mirroring reloadLocked's
// LabelIndex wiring. Caller MUST hold s.mu. No fbreader.Open and no disk read: the
// Reader is the same mapping the eviction kept resident. The lazy derived indexes
// (adjacency/BM25/byID/…) are left for their getters to rebuild on first use
// (resetIndexes re-arms the Once guards). The group-algo overlay side-table is
// re-applied by the caller's refreshGroupAlgoOverlayLocked (forced via the shell's
// algoApplied=false).
func (lr *LoadedRepo) rematerializeFromReaderLocked() {
	lr.rmu().Lock()
	if lr.Reader != nil {
		li := BuildLabelIndexFromReader(lr.Reader, lr.Doc)
		li.readerMu = lr.rmu()
		li.handle = lr.handle
		lr.LabelIndex = li
	} else {
		lr.LabelIndex = BuildLabelIndex(lr.Doc)
	}
	lr.rmu().Unlock()
	lr.resetIndexes()
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

// SweepIdleGroups evicts the whole resident graph of every group whose last
// State.Group routing is older than idle, EXCEPT the currently-active group,
// returning how many groups were evicted. It is the production wiring of the
// EvictGroup primitive (memory epic #5850, issue #5872 PR2) — the lever that
// makes idle-group RSS reclamation actually happen in an agent fleet — fired from
// the MCP server's per-call reload hook (reloadBeforeCall) alongside
// SweepIdleBM25. A non-positive idle disables the sweep (returns 0 without
// locking), so it is behaviour-neutral when the knob is unset.
//
// PIN (the load-bearing safety requirement). The group being serviced RIGHT NOW
// must NEVER be evicted — a flag-on in-flight read on an evicted group would
// otherwise observe an empty graph. The pin is the MAX-lastAccess group: State.Group
// stamps lastAccess on every routing decision, so the newest-stamped group is the
// active working set the fleet is currently querying, and it is always skipped
// regardless of its absolute age. Reinforcing the pin:
//   - The idle window itself: any group touched within idle (now-lastAccess <= idle)
//     is skipped, so a group queried concurrently mid-sweep is safe.
//   - Never-routed groups (lastAccess zero — loaded but never queried through
//     State.Group) are NOT idle-eligible, mirroring bm25LastUse's zero semantics.
//   - The revive-on-access backstop in State.Group: even a group this sweep does
//     evict is transparently re-materialized on its very next State.Group, so a
//     pin miss degrades to an extra evict+revive, never an empty read.
//
// Composition with SweepIdleBM25: BM25 evicts at the short window (~5 min); the
// whole group evicts at the longer group window — they stack. A group idle long
// enough first loses its BM25 (heap already dropped, bm25Once re-armed) and then,
// still idle, loses the whole graph — evicting a group whose BM25 was already
// evicted is a plain no-op interaction (EvictGroup drops whatever heap remains).
//
// Locking: takes s.mu once and evicts via evictGroupLocked (the EvictGroup body),
// which routes every munmap through the F1 retireHandle drain — same s.mu ->
// readerMu order the primitive and reload use, so no in-flight borrow is unmapped.
// keepReader is threaded straight to evictGroupLocked (keep the mmap mapped and
// re-materialize on revive vs. full munmap + disk re-read).
func (s *State) SweepIdleGroups(idle time.Duration, keepReader bool) int {
	if idle <= 0 {
		return 0
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	// Identify the PIN: the active (max-lastAccess) group. Skipped unconditionally
	// below so the group the fleet is currently servicing is never the victim.
	pin := ""
	var pinAt time.Time
	for name, g := range s.groups {
		if g == nil {
			continue
		}
		if pin == "" || g.lastAccess.After(pinAt) {
			pin, pinAt = name, g.lastAccess
		}
	}

	// Collect eligible names first — evictGroupLocked mutates s.groups, so we must
	// not delete while ranging it.
	var idleNames []string
	for name, g := range s.groups {
		if g == nil || name == pin {
			continue
		}
		// Never-routed (zero lastAccess) → not idle-eligible (mirrors bm25LastUse).
		if g.lastAccess.IsZero() || now.Sub(g.lastAccess) <= idle {
			continue
		}
		idleNames = append(idleNames, name)
	}

	evicted := 0
	for _, name := range idleNames {
		if s.evictGroupLocked(name, keepReader) {
			evicted++
		}
	}
	return evicted
}

// heapAllocSample is the production resident-memory sampler: the current Go heap
// allocation (runtime.ReadMemStats().HeapAlloc). No syscall (a brief STW only), and
// it reports precisely the arena that whole-group eviction reclaims to GC — the
// derived-index heap (LabelIndex/adjacency/BM25/overlay), which is what
// evictGroupLocked drops. Under the default keepReader=true the mmap stays mapped,
// so the mapped-file portion of RSS is NOT this lever's to release; HeapAlloc is
// therefore both cheaper AND the metric the watermark trigger actually moves. See
// .grafel/research/eviction-pr3-grounding.md §3 for the full metric justification.
//
// Caveat handled by SweepToMemoryBudget: HeapAlloc is LAGGY — freed heap is reflected
// only after a GC, and a keepReader evict transiently RAISES it (fresh cold shell). So
// the watermark trigger samples this ONCE per sweep and sheds a single group, never
// re-sampling mid-loop (which would misread the lag as "still over budget" and evict
// every non-pinned group at once).
func heapAllocSample() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// sampleResidentMem returns the current resident-memory sample, using the injected
// s.memSampler when present (tests) and heapAllocSample otherwise (production).
// Caller holds s.mu.
func (s *State) sampleResidentMem() uint64 {
	if s.memSampler != nil {
		return s.memSampler()
	}
	return heapAllocSample()
}

// SweepToMemoryBudget is the MEMORY-PRESSURE eviction trigger (memory epic #5850,
// issue #5872 PR3): when resident memory exceeds budgetBytes it evicts the SINGLE
// oldest-lastAccess non-pinned group (LRU victim) and returns how many it evicted
// (0 or 1). It bounds total fleet RSS regardless of the idle window (PR2's trigger)
// and stacks with it: SweepIdleGroups reclaims groups a session has paused on; this
// sheds the LRU tail under real memory pressure even when nothing has been idle long
// enough.
//
// budgetBytes == 0 DISABLES the trigger (returns 0 before locking or sampling), so
// it is 100% behaviour-neutral when the knob is unset — no sample, no lock, no evict.
//
// ONE VICTIM PER SWEEP — and why we do NOT re-sample mid-loop. HeapAlloc (the sample)
// does not drop the instant a group is evicted: the freed heap is only reflected
// after a GC, and under the default keepReader=true the evict itself ALLOCATES a fresh
// cold shell, transiently RAISING HeapAlloc. A loop that re-sampled after each evict
// would therefore never see the sample fall under budget within one sweep and would
// evict EVERY non-pinned group on the first breach — a thundering-herd revive storm,
// the opposite of graduated shedding. Instead we take a single ENTRY sample, shed the
// one oldest non-pinned group, and return. Successive reloadBeforeCall sweeps — each
// grounded in a fresh entry sample once GC has reflected the prior frees — shed more
// if still over budget, converging across windows. reloadBeforeCall fires frequently,
// so convergence is prompt; and forcing a runtime.GC() here (to make an in-sweep
// re-sample honest) is deliberately avoided as STW-expensive on the hot-ish path.
//
// PIN (same load-bearing safety rule as PR2). The active group — the MAX-lastAccess
// group the fleet is servicing RIGHT NOW — is NEVER the victim, even under memory
// pressure: it is the newest by definition, so the oldest-non-pin victim is never it.
// A flag-on in-flight read on an evicted group would otherwise observe an empty graph.
// When only the pinned group remains, the sweep evicts nothing (returns 0) — the
// active working set is never sacrificed no matter how far over budget we are.
//
// Locking: takes s.mu once and evicts via evictGroupLocked (the EvictGroup body) —
// same s.mu → readerMu order the primitive, reload, and the idle sweep use, so no
// in-flight borrow is unmapped. keepReader is threaded straight to evictGroupLocked.
func (s *State) SweepToMemoryBudget(budgetBytes uint64, keepReader bool) int {
	if budgetBytes == 0 {
		return 0 // disabled — behaviour-neutral, no sample/lock/evict.
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Single ENTRY sample — the only sample this sweep takes. Nothing to do if we are
	// already under budget (see the doc comment for why we never re-sample mid-loop).
	if s.sampleResidentMem() <= budgetBytes {
		return 0
	}

	// Identify the PIN: the active (max-lastAccess) group, excluded as a victim so the
	// group the fleet is currently servicing is never evicted — even under memory
	// pressure (the same hard rule as SweepIdleGroups).
	pin := ""
	var pinAt time.Time
	for name, g := range s.groups {
		if g == nil {
			continue
		}
		if pin == "" || g.lastAccess.After(pinAt) {
			pin, pinAt = name, g.lastAccess
		}
	}

	// Pick the LRU victim: the oldest-lastAccess group that is NOT the pin. Name is
	// the deterministic tie-break so equal timestamps evict in a stable order.
	victim := ""
	var victimAt time.Time
	for name, g := range s.groups {
		if g == nil || name == pin {
			continue
		}
		if victim == "" || g.lastAccess.Before(victimAt) ||
			(g.lastAccess.Equal(victimAt) && name < victim) {
			victim, victimAt = name, g.lastAccess
		}
	}
	if victim == "" {
		return 0 // only the pinned group remains — never sacrifice the active set.
	}
	if s.evictGroupLocked(victim, keepReader) {
		return 1
	}
	return 0
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
	// Drain kept-reader cold shells too (memory epic #5850, issue #5872 PR1). A
	// keepReader EvictGroup leaves a group's mmap RESIDENT on a shell in s.evicted
	// (not in s.groups), so the loop above would miss it and leak the mapping on
	// shutdown. Retire each shell's handle through the same F1 drain. Full-evict
	// entries are nil (already munmapped at eviction) and skipped.
	for _, g := range s.evicted {
		if g == nil {
			continue
		}
		for _, lr := range g.Repos {
			if lr != nil {
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
	// ADR-0027 mmap-cutover PR7 (memory epic #5850): when serving from mmap is
	// enabled (opt-in, GRAFEL_SERVE_FROM_MMAP; DEFAULT OFF), load a HEADER-ONLY
	// Document — meta/Stats/counts populated, Entities/Relationships left empty
	// — because reloadLocked opens a resident fbreader.Reader right after this
	// call and every read path (forEachEntity/at/getByIDOne/BM25/adjacency/
	// overlay) serves from that Reader. The Doc stays non-nil (the "loaded"
	// sentinel), so the ~608 MB entity/relationship materialization is dropped
	// from the Go heap. DEFAULT OFF ⇒ full materialization, 100% unchanged.
	if serveFromMMap() {
		return graph.LoadGraphHeaderOnlyFromDir(stateDir)
	}
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
// reading Id/Pagerank directly off the fbreader.Reader.
//
// BLOCKED — NOT CALLED (post-PR1 regression fix, see getTopKPageRank): the FB
// Pagerank() scalar this reads is a permanent sentinel (0) for every entity.
// Per-repo Pass-4 (the code that used to compute + persist real per-entity
// PageRank into graph.fb) was removed when the group-scope algo pass (A1-A3,
// #5349) replaced it. The only place real PageRank now lives is the
// <group>-algo.json overlay, which applyGroupAlgoOverlay stamps onto
// lr.Doc.Entities[i].PageRank in memory — it is never written back into
// graph.fb, so the Reader can never see it. Calling this from
// getTopKPageRank silently collapsed top-K to id order and corrupted
// pickFallback's entity choice; that regression is why getTopKPageRank now
// always sources from lr.Doc via buildTopKPageRank instead.
//
// This function is kept — with TestTopKPageRankReaderParity_PR1 still
// exercising it — because it correctly proves byte-identical top-K
// extraction logic between the Reader path and the Document path for
// whatever PageRank values are actually baked into a given graph.fb (the
// parity test writes real values directly into its fixture, bypassing the
// production sentinel). A follow-up PR can re-enable calling this once a
// side-table lets the overlay's real PageRank reach the Reader seam (the
// overlay's per-entity map keyed by ID, applied at Reader-borrow time rather
// than only onto lr.Doc), at which point this comment should be replaced.
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

// buildTopKPageRankFromSideTable is the flip-ready (GRAFEL_SERVE_FROM_MMAP ON)
// top-K builder. It sources entity IDS from the resident mmap Reader in vector
// order and PageRank VALUES from the group-algo overlay SIDE-TABLE
// (lr.LabelIndex.overlay[i].PageRank) — NOT the Reader's Pagerank() scalar,
// which is the permanent sentinel (0) and would collapse top-K to id order
// (the reverted PR1 #5866 bug). An entity with no overlay entry ranks at 0,
// exactly as an un-stamped lr.Doc row (nil PageRank) does under
// buildTopKPageRank, so this is byte-identical to the flag-off Doc-sourced
// top-K for the same overlay data.
//
// Falls back to the Doc-sourced buildTopKPageRank when no mmap Reader is
// resident or the mapping was retired (mirrors at()/forEachEntity). readerMu
// guards the mmap dereference (SIGBUS-safety, memory epic #5850).
func (lr *LoadedRepo) buildTopKPageRankFromSideTable(k int) []string {
	lr.rmu().Lock()
	rdr := lr.Reader
	if rdr == nil || (lr.handle != nil && lr.handle.readRetired) {
		lr.rmu().Unlock()
		return buildTopKPageRank(lr.Doc, k) // Reader gone → Doc fallback
	}
	var overlay map[int32]entityOverlay
	if lr.LabelIndex != nil {
		overlay = lr.LabelIndex.overlay
	}
	type ranked struct {
		id string
		pr float64
	}
	all := make([]ranked, 0, rdr.EntityCount())
	var i int32
	rdr.IterateEntities(func(e *fb.Entity) bool {
		pr := 0.0
		if ov, ok := overlay[i]; ok && ov.PageRank != nil {
			pr = *ov.PageRank
		}
		all = append(all, ranked{id: string(e.Id()), pr: pr})
		i++
		return true
	})
	lr.rmu().Unlock()

	sort.Slice(all, func(a, b int) bool {
		return all[a].pr > all[b].pr
	})
	if k > len(all) {
		k = len(all)
	}
	out := make([]string, k)
	for j := 0; j < k; j++ {
		out[j] = all[j].id
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
		// In-place Doc stamp: the flag-OFF read path (LabelIndex.at / forEachEntity
		// / buildTopKPageRank) reads these values directly, and the flag-ON path
		// still reads them PRE-FLIP while lr.Doc.Entities is populated. Kept
		// byte-identical to the original; a no-op once PR7 empties lr.Doc.Entities
		// (the side-table below then carries the values on the flag-ON path).
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
		// ADR-0027 overlay SIDE-TABLE (built ONLY when GRAFEL_SERVE_FROM_MMAP is
		// ON): a fresh entity-INDEX-keyed table of the 5 overlay fields so the
		// Reader-materialized read path (LabelIndex.at/getByID/forEachEntity) can
		// merge them WITHOUT depending on lr.Doc for VALUES. Sourced by iterating
		// the resident mmap Reader (not lr.Doc) so it is populated even after PR7
		// empties lr.Doc.Entities — flip-readiness is the whole point of this PR.
		// Keyed by vector index i (== the LabelIndex/Reader position, the same
		// order BuildLabelIndexFromReader assigns). Falls back to the Doc ONLY when
		// no Reader is resident (JSON-only / no graph.fb), where the read path also
		// reads the Doc, so the side-table is never consulted there anyway. On the
		// flag-off default path the table is left nil — no resident cost pre-flip.
		//
		// ADR-0027 SIGBUS-safety (memory epic #5850, Path P): readerMu guards BOTH
		// the mmap iteration (buildOverlayTableFromReader dereferences the mapping)
		// AND the publish onto the current LabelIndex generation, mirroring
		// forEachEntity/at() — which read lr.LabelIndex.overlay under readerMu, so
		// this re-stamp must not race them. handle.readRetired is checked so a
		// retired mapping falls back to the Doc instead of dereferencing freed
		// memory. Publish happens BEFORE resetIndexes below; resetIndexes does NOT
		// touch LabelIndex, so the table persists for this generation's life and is
		// reassigned on the next re-stamp.
		if serveFromMMap() {
			lr.rmu().Lock()
			var table map[int32]entityOverlay
			if rdr := lr.Reader; rdr != nil && !(lr.handle != nil && lr.handle.readRetired) {
				table = buildOverlayTableFromReader(rdr, ov)
			} else {
				table = buildOverlayTableFromDoc(lr.Doc, ov)
			}
			if lr.LabelIndex != nil {
				lr.LabelIndex.overlay = table
			}
			lr.rmu().Unlock()
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

// newEntityOverlay heap-copies a groupalgo.EntityOverlay into an entityOverlay
// side-table row. The three pointer fields are INDEPENDENT copies (distinct from
// the in-place Doc stamp's pointers) so the side-table remains a valid value
// source after PR7 drops lr.Doc.Entities.
func newEntityOverlay(eo groupalgo.EntityOverlay) entityOverlay {
	cid := eo.CommunityID
	pr := eo.PageRank
	cen := eo.Centrality
	return entityOverlay{
		CommunityID:      &cid,
		PageRank:         &pr,
		Centrality:       &cen,
		IsGodNode:        eo.IsGodNode,
		IsArticulationPt: eo.IsArticulationPoint,
	}
}

// buildOverlayTableFromReader builds the entity-INDEX-keyed overlay side-table
// by iterating the resident mmap Reader (NOT lr.Doc), matching each entity to
// the overlay by ID. The int32 key is the vector index i in Reader iteration
// order — identical to the order BuildLabelIndexFromReader assigns and the order
// at()/materializeFromReader/forEachEntity look the table up by. Only entities
// WITH an overlay entry are inserted; a miss leaves the fb sentinel on the read
// path. Callers MUST hold the owning LoadedRepo's readerMu (the mmap is
// dereferenced here via IterateEntities).
func buildOverlayTableFromReader(r *fbreader.Reader, ov *groupalgo.Overlay) map[int32]entityOverlay {
	table := make(map[int32]entityOverlay)
	if r == nil || ov == nil {
		return table
	}
	var i int32
	r.IterateEntities(func(e *fb.Entity) bool {
		if eo, has := ov.Results[string(e.Id())]; has {
			table[i] = newEntityOverlay(eo)
		}
		i++
		return true
	})
	return table
}

// buildOverlayTableFromDoc is the Doc-sourced twin used ONLY when no mmap Reader
// is resident (JSON-only / no graph.fb) on the flag-on path. Keyed by the same
// vector index i as the Reader path. The read path reads the Doc directly when
// the Reader is absent, so this table is a belt-and-braces parity fallback.
func buildOverlayTableFromDoc(doc *graph.Document, ov *groupalgo.Overlay) map[int32]entityOverlay {
	table := make(map[int32]entityOverlay)
	if doc == nil || ov == nil {
		return table
	}
	for i := range doc.Entities {
		if eo, has := ov.Results[doc.Entities[i].ID]; has {
			table[int32(i)] = newEntityOverlay(eo)
		}
	}
	return table
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
