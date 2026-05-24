// Package mcp implements the archigraph MCP server (clean-room, per ADR-0002).
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
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/embed"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
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
//	          "graph_file":  "/abs/path/.archigraph/graph.json"  // optional explicit
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
}

// EffectiveKind returns the link's relation, preferring the explicit "kind"
// field and falling back to the links-pass "relation" field.
func (l CrossRepoLink) EffectiveKind() string {
	if l.Kind != "" {
		return l.Kind
	}
	return l.Relation
}

// LoadedRepo is one repo's graph plus index plus mtime tracking.
//
// Pre-computed indexes (LabelIndex, BM25, Adjacency, CallsAdj, ByID) are
// rebuilt only when the graph file mtime changes. This eliminates the
// O(N) + O(R) cost per MCP query that handlers used to pay by calling
// indexByID / buildAdjacency on every invocation (#1656).
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
	Reader     *fbreader.Reader         // mmap zero-copy reader (S8, #2159); nil when unavailable
	LabelIndex *LabelIndex
	BM25       *BM25Index
	Adjacency  *adjacency               // in/out neighbor lists (#1656)
	CallsAdj   map[string][]string      // CALLS-only forward adjacency (#1656)
	ByID       map[string]*graph.Entity // entity ID -> entity (#1656)
	Semantic   *embed.Store             // per-repo vector index (nil when no embeddings.bin)
	semMtime   time.Time
	byID       map[string]*graph.Entity // deprecated alias for ByID — kept for back-compat during #1656 rollout
	mtime      time.Time
	loadErr    string // populated when last reload failed; doc may be stale
}

// LoadedGroup holds all loaded repos for a group plus cross-repo links.
type LoadedGroup struct {
	Name      string
	Repos     map[string]*LoadedRepo
	Links     []CrossRepoLink
	LinksFile string
	linksMt   time.Time
	MemoryDir string
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
}

// SetWorktreeLookup wires the PH3 ephemeral worktree registry.  Call from
// cmd/archigraph after the worktree.Store is loaded.
func (s *State) SetWorktreeLookup(wl WorktreeLookup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.worktreeLookup = wl
}

// NewState constructs an empty state for the given registry.
func NewState(reg *Registry) *State {
	return &State{
		registry: reg,
		groups:   map[string]*LoadedGroup{},
		created:  time.Now(),
	}
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
			// Use FindGraphFile to discover graph.fb (preferred) or graph.json,
			// fixing issue #1374 item #1: repos that only have graph.fb were
			// silently dropped because the old os.Stat always targeted graph.json.
			graphPath, modtimeNs := daemon.FindGraphFile(rEntry.Path)
			if graphPath == "" {
				// Neither graph.fb nor graph.json exists yet; skip without error.
				lr, exists := grp.Repos[rName]
				if !exists {
					lr = &LoadedRepo{Repo: rName, Path: rEntry.Path}
					grp.Repos[rName] = lr
				}
				lr.loadErr = "no graph file found (graph.fb or graph.json)"
				continue
			}
			lr, ok := grp.Repos[rName]
			if !ok {
				lr = &LoadedRepo{Repo: rName, Path: rEntry.Path, GraphFile: graphPath}
				grp.Repos[rName] = lr
			}
			// Update GraphFile in case .fb appeared after initial load.
			lr.GraphFile = graphPath
			fileMtime := time.Unix(0, modtimeNs)
			stateDir := daemon.StateDirForRepo(rEntry.Path)
			if !(fileMtime.Equal(lr.mtime) && lr.Doc != nil) {
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
				lr.loadErr = ""
				lr.LabelIndex = BuildLabelIndex(doc)
				lr.BM25 = BuildBM25(doc)
				lr.ByID = make(map[string]*graph.Entity, len(doc.Entities))
				for i := range doc.Entities {
					lr.ByID[doc.Entities[i].ID] = &doc.Entities[i]
				}
				lr.byID = lr.ByID // back-compat alias (deprecated)
				// Pre-build adjacency once per reload. Eliminates the per-query
				// O(R)=117k scan that every flow/traversal handler used to pay
				// via buildAdjacency(r.Doc, r.Repo). (#1656)
				lr.Adjacency = buildAdjacency(doc, lr.Repo)
				// CALLS-only forward adjacency for traces.followCallsBFS, which
				// previously rebuilt this on every traces=follow query. (#1656)
				lr.CallsAdj = buildCallsAdjacency(doc)
				// S8 (#2159): open the mmap reader alongside the Document.
				// Best-effort: failures leave Reader nil; callers fall back to
				// doc.Entities / doc.Relationships.
				fbPath := filepath.Join(stateDir, "graph.fb")
				if rdr, rErr := fbreader.Open(fbPath); rErr == nil {
					lr.Reader = rdr
				}
				reloaded++
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

// readDocumentFromDir loads a graph document from a state directory.
// Delegates to graph.LoadGraphFromDir which prefers graph.fb over graph.json
// (ADR-0016, issue #808).
func readDocumentFromDir(stateDir string) (*graph.Document, error) {
	return graph.LoadGraphFromDir(stateDir)
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

// defaultLinksFile is the conventional path for cross-repo links.
func defaultLinksFile(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", "groups", group+"-links.json")
}

// defaultMemoryDir is the conventional path for save_finding outputs.
func defaultMemoryDir(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", "groups", group+"-memory")
}

// defaultLinkCandidatesFile is the on-disk file for pending link candidates.
func defaultLinkCandidatesFile(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", "groups", group+"-link-candidates.json")
}

// defaultRegistryPath is "~/.archigraph/registry.json".
func defaultRegistryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "registry.json"
	}
	return filepath.Join(home, ".archigraph", "registry.json")
}
