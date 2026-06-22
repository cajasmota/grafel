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
	Repo       string
	Path       string
	GraphFile  string
	Doc        *graph.Document
	Reader     *fbreader.Reader // mmap zero-copy reader (S8, #2159); nil when unavailable
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
	mroInOnce    sync.Once                // #3834: reverse-INHERITS map built lazily
	adjacency    *adjacency               // in/out neighbor lists (#1656)
	callsAdj     map[string][]string      // CALLS-only forward adjacency (#1656)
	stepAdj      map[string][]stepEdge    // STEP_IN_PROCESS forward adjacency (#2417)
	byID         map[string]*graph.Entity // entity ID -> entity (#1656)
	topKPageRank []string                 // entity IDs sorted descending by PageRank (#2304)
	// mroInbound maps a DEFINING member's local id -> the inherited-stub ids
	// that resolve to it via the MRO walk (#3834). It is the reverse of
	// mroOutboundEdges, used by neighbors(in) so a base method surfaces the
	// subclasses that inherit it. In-repo defining members only (external
	// contract endpoints have no in-repo node to query callers of).
	mroInbound map[string][]string

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
}

// resetIndexes re-arms every lazy-index sync.Once and clears the cached
// derived indexes so the next getter call rebuilds against the current Doc.
// MUST be called whenever lr.Doc is replaced during reload, while the caller
// holds State.mu (which serialises reload against handler snapshots). It also
// takes idxMu so a getter that raced in just before the swap cannot observe a
// half-reset state.
func (lr *LoadedRepo) resetIndexes() {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.adjOnce = sync.Once{}
	lr.callsOnce = sync.Once{}
	lr.stepOnce = sync.Once{}
	lr.byIDOnce = sync.Once{}
	lr.pagerankOnce = sync.Once{}
	lr.bm25Once = sync.Once{}
	lr.mroInOnce = sync.Once{}
	lr.BM25 = nil
	lr.adjacency = nil
	lr.callsAdj = nil
	lr.stepAdj = nil
	lr.byID = nil
	lr.topKPageRank = nil
	lr.mroInbound = nil
}

// getMROInbound returns the reverse-INHERITS map (defining-member id ->
// inheriting-stub ids), building it lazily on first use (#3834). It scans the
// repo's member entities once and records, for each that resolves to an in-repo
// defining member via the MRO walk, the defining member's id. Used by
// neighbors(in) so a base method surfaces its inheriting subclasses as callers.
func (lr *LoadedRepo) getMROInbound() map[string][]string {
	// NOTE: deliberately NOT guarded by idxMu — buildMROInbound calls
	// mroOutboundEdges -> resolveMember -> extendsBases -> getAdjacency, which
	// itself takes idxMu (a non-reentrant Mutex). sync.Once.Do is independently
	// safe for concurrent callers, so the Once alone gives build-at-most-once.
	lr.mroInOnce.Do(func() {
		lr.mroInbound = buildMROInbound(lr)
	})
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
	return lr.BM25
}

// getAdjacency returns the in/out neighbor index, building it on first use.
func (lr *LoadedRepo) getAdjacency() *adjacency {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.adjOnce.Do(func() {
		if lr.Doc == nil {
			lr.adjacency = &adjacency{out: map[string][]edge{}, in: map[string][]edge{}}
			return
		}
		lr.adjacency = buildAdjacency(lr.Doc, lr.Repo)
	})
	return lr.adjacency
}

// getCallsAdj returns the CALLS-only forward adjacency, building it on first use.
func (lr *LoadedRepo) getCallsAdj() map[string][]string {
	lr.idxMu.Lock()
	defer lr.idxMu.Unlock()
	lr.callsOnce.Do(func() {
		if lr.Doc == nil {
			lr.callsAdj = map[string][]string{}
			return
		}
		lr.callsAdj = buildCallsAdjacency(lr.Doc)
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
		lr.stepAdj = buildStepAdjacency(lr.Doc)
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
		lr.topKPageRank = buildTopKPageRank(lr.Doc, 64)
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
						continue
					}
					// S8 (#2159): close the previous reader before replacing it so
					// we don't leak mmap fds across reloads.
					if lr.Reader != nil {
						_ = lr.Reader.Close()
						lr.Reader = nil
					}
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
					// TESTS-edge count cached once per reload so grafel_whoami can
					// return it in O(1) without rescanning all relationships. This is a
					// cheap O(R) count with no allocation, so it stays eager (#3325).
					testsCount := 0
					for i := range doc.Relationships {
						if doc.Relationships[i].Kind == "TESTS" {
							testsCount++
						}
					}
					lr.TestsEdgeCount = testsCount
					// S8 (#2159): open the mmap reader alongside the Document.
					// Best-effort: failures leave Reader nil; callers fall back to
					// doc.Entities / doc.Relationships.
					fbPath := filepath.Join(stateDir, "graph.fb")
					if rdr, rErr := fbreader.Open(fbPath); rErr == nil {
						lr.Reader = rdr
					}
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
		// S8 (#2159): close the mmap reader when evicting a repo entry.
		for rName, lr := range grp.Repos {
			if !seen[rName] {
				if lr != nil && lr.Reader != nil {
					_ = lr.Reader.Close()
					lr.Reader = nil
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
func (s *State) Group(name string) *LoadedGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groups[name]
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
			if lr != nil && lr.Reader != nil {
				_ = lr.Reader.Close()
				lr.Reader = nil
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

	// Memoize: skip re-stamping when neither the overlay nor the graphs changed.
	// We re-apply whenever the overlay mtime advanced OR a repo was reparsed
	// this reload (which would have reset entity fields to graph.fb values).
	if grp.algoApplied && fi.ModTime().Equal(grp.algoMt) {
		// Still set the summary (cheap; keeps grp.Communities authoritative).
		grp.Communities = ov.Communities
		return
	}

	for _, lr := range grp.Repos {
		if lr == nil || lr.Doc == nil {
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
	}

	grp.Communities = ov.Communities
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
