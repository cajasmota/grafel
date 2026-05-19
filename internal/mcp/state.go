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
	"github.com/cajasmota/archigraph/internal/graph"
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
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence,omitempty"`
	Channel    string  `json:"channel,omitempty"`
	Method     string  `json:"method,omitempty"`
}

// LoadedRepo is one repo's graph plus index plus mtime tracking.
type LoadedRepo struct {
	Repo       string
	Path       string
	GraphFile  string
	Doc        *graph.Document
	LabelIndex *LabelIndex
	BM25       *BM25Index
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

// State is the long-lived in-memory model. All accesses go through Reload()
// which is safe to call from any goroutine.
type State struct {
	mu       sync.Mutex
	registry *Registry
	groups   map[string]*LoadedGroup
	created  time.Time
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

// Reload performs lazy mtime-driven reload of every repo + links file in
// the registry. Returns the count of (re)loaded files. Safe for concurrent
// callers — serialized through s.mu.
func (s *State) Reload() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
			gf := rEntry.graphFile()
			lr, ok := grp.Repos[rName]
			if !ok {
				lr = &LoadedRepo{Repo: rName, Path: rEntry.Path, GraphFile: gf}
				grp.Repos[rName] = lr
			}
			info, err := os.Stat(gf)
			if err != nil {
				lr.loadErr = err.Error()
				continue
			}
			if info.ModTime().Equal(lr.mtime) && lr.Doc != nil {
				continue
			}
			doc, err := readDocument(gf)
			if err != nil {
				lr.loadErr = err.Error()
				continue
			}
			lr.Doc = doc
			lr.mtime = info.ModTime()
			lr.loadErr = ""
			lr.LabelIndex = BuildLabelIndex(doc)
			lr.BM25 = BuildBM25(doc)
			reloaded++
		}
		// Drop repos no longer in the registry.
		for rName := range grp.Repos {
			if !seen[rName] {
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
	return reloaded, nil
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

// readDocument loads a graph document from disk. It receives the
// graph.json path for back-compat with the registry's graphFile()
// helper, derives the state directory, then delegates to
// graph.LoadGraphFromDir which prefers graph.fb when present (ADR-0016
// flip-day, issue #808).
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
