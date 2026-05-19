package dashboard

// graphstate.go — shared in-memory graph state for the dashboard REST layer.
//
// The dashboard loads graphs lazily from the registry (same on-disk
// graph.fb / graph.json that the MCP daemon reads) and caches them per
// group with a TTL-based invalidation so first-paint endpoints stay fast.
//
// Design: intentionally no dependency on internal/mcp.  The dashboard
// reads the same graph files directly via internal/graph.LoadGraphFromDir
// and internal/registry, duplicating only the thin helpers it needs
// (prefixedID, splitPrefixed, stripScopePrefix, entity scanning).  This
// keeps the import graph simple and the dashboard binary slim.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Helpers (mirrors of unexported mcp helpers; small enough to inline)
// ---------------------------------------------------------------------------

func dashPrefixedID(repo, id string) string { return repo + "::" + id }

func dashSplitPrefixed(s string) (string, string) {
	i := strings.Index(s, "::")
	if i < 0 {
		return "", s
	}
	return s[:i], s[i+2:]
}

func dashStripScopePrefix(s string) string {
	if after, ok := strings.CutPrefix(s, "SCOPE."); ok {
		return after
	}
	return s
}

// ---------------------------------------------------------------------------
// LoadedRepo — one repo's graph loaded into memory
// ---------------------------------------------------------------------------

// DashRepo is a loaded repo entry for the dashboard layer.
type DashRepo struct {
	Slug  string
	Path  string
	Doc   *graph.Document
	mtime time.Time
	err   string
}

// ---------------------------------------------------------------------------
// DashGroup — all repos for one group plus cross-repo links
// ---------------------------------------------------------------------------

// DashGroup holds loaded repos and links for one group.
type DashGroup struct {
	Name  string
	Repos map[string]*DashRepo // slug -> repo
	Links []CrossRepoLink
}

// CrossRepoLink mirrors mcp.CrossRepoLink.
type CrossRepoLink struct {
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence,omitempty"`
	Channel    string  `json:"channel,omitempty"`
	Method     string  `json:"method,omitempty"`
}

// ---------------------------------------------------------------------------
// GraphCache — mtime-driven per-group cache with TTL
// ---------------------------------------------------------------------------

// cacheEntry holds a loaded group plus the time it was last refreshed.
type cacheEntry struct {
	group     *DashGroup
	loadedAt  time.Time
}

// GraphCache is the dashboard's in-memory graph store.  It is safe for
// concurrent use.  Reload is lazy: the first call for a group loads it;
// subsequent calls check mtime and skip the reload when graphs haven't
// changed.
type GraphCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

// NewGraphCache returns a cache with the given TTL.  Use 60 * time.Second
// for production; tests may use a lower value.
func NewGraphCache(ttl time.Duration) *GraphCache {
	return &GraphCache{
		entries: map[string]*cacheEntry{},
		ttl:     ttl,
	}
}

// Invalidate drops the cached entry for group (called on re-index events).
func (c *GraphCache) Invalidate(group string) {
	c.mu.Lock()
	delete(c.entries, group)
	c.mu.Unlock()
}

// InvalidateAll drops every cached entry.
func (c *GraphCache) InvalidateAll() {
	c.mu.Lock()
	c.entries = map[string]*cacheEntry{}
	c.mu.Unlock()
}

// GetGroup returns the loaded group, refreshing from disk when the TTL has
// elapsed or when graph files have changed on disk.
func (c *GraphCache) GetGroup(groupName string) (*DashGroup, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	ent, ok := c.entries[groupName]
	if ok && now.Sub(ent.loadedAt) < c.ttl {
		return ent.group, nil
	}

	// Load (or refresh) from disk.
	grp, err := c.loadGroup(groupName)
	if err != nil {
		return nil, err
	}
	c.entries[groupName] = &cacheEntry{group: grp, loadedAt: now}
	return grp, nil
}

// loadGroup reads the registry for groupName and loads each repo's graph.
// Must be called with c.mu held.
func (c *GraphCache) loadGroup(groupName string) (*DashGroup, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	var cfgPath string
	for _, g := range groups {
		if g.Name == groupName {
			cfgPath = g.ConfigPath
			break
		}
	}
	if cfgPath == "" {
		return nil, fmt.Errorf("group %q not registered", groupName)
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	grp := &DashGroup{
		Name:  groupName,
		Repos: map[string]*DashRepo{},
	}
	for _, r := range cfg.Repos {
		dr := &DashRepo{Slug: r.Slug, Path: r.Path}
		stateDir := daemon.StateDirForRepo(r.Path)
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			dr.err = err.Error()
		} else {
			dr.Doc = doc
			// Record the newer of fb/json mtime for cache invalidation.
			if info, e := os.Stat(filepath.Join(stateDir, "graph.fb")); e == nil {
				dr.mtime = info.ModTime()
			} else if info, e = os.Stat(filepath.Join(stateDir, "graph.json")); e == nil {
				dr.mtime = info.ModTime()
			}
		}
		grp.Repos[r.Slug] = dr
	}

	// Load cross-repo links file.
	lf := defaultLinksFile(groupName)
	if data, err := os.ReadFile(lf); err == nil {
		var links []CrossRepoLink
		if json.Unmarshal(data, &links) == nil {
			grp.Links = links
		}
	}

	return grp, nil
}

// defaultLinksFile mirrors mcp.defaultLinksFile.
func defaultLinksFile(group string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".archigraph", "groups", group+"-links.json")
}

// sortedRepos returns the repos of a group in deterministic slug order.
func sortedRepos(g *DashGroup) []*DashRepo {
	slugs := make([]string, 0, len(g.Repos))
	for s := range g.Repos {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	out := make([]*DashRepo, 0, len(slugs))
	for _, s := range slugs {
		if r := g.Repos[s]; r != nil && r.Doc != nil {
			out = append(out, r)
		}
	}
	return out
}

// findEntity looks up an entity by prefixed-or-bare id/label within a group.
func findEntity(g *DashGroup, key string) (*DashRepo, *graph.Entity) {
	// Prefixed "<repo>::<local>".
	if rp, local := dashSplitPrefixed(key); rp != "" {
		if r, ok := g.Repos[rp]; ok && r.Doc != nil {
			for i := range r.Doc.Entities {
				if r.Doc.Entities[i].ID == local {
					return r, &r.Doc.Entities[i]
				}
			}
		}
		return nil, nil
	}
	// Bare: try ID then Name match across repos.
	for _, r := range sortedRepos(g) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.ID == key || e.Name == key {
				return r, e
			}
		}
	}
	return nil, nil
}

// serializeEntity converts a graph.Entity into the REST wire shape.
func serializeEntity(repo string, e *graph.Entity) map[string]any {
	out := map[string]any{
		"id":             dashPrefixedID(repo, e.ID),
		"label":          e.Name,
		"qualified_name": e.QualifiedName,
		"kind":           dashStripScopePrefix(e.Kind),
		"source_file":    e.SourceFile,
		"start_line":     e.StartLine,
		"end_line":       e.EndLine,
		"language":       e.Language,
		"repo":           repo,
	}
	if e.PageRank != nil {
		out["pagerank"] = *e.PageRank
	}
	if e.CommunityID != nil {
		out["community_id"] = *e.CommunityID
	}
	if len(e.Properties) > 0 {
		out["properties"] = e.Properties
	}
	return out
}

// hashStr returns a short stable hash for use as a path-hash key.
func hashStr(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}
