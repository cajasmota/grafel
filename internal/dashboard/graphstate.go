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
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// graphPayloadCache — serialised-bytes cache for GET /api/graph/{group}
// ---------------------------------------------------------------------------
//
// Each entry stores the already-JSON-encoded response body and a strong ETag
// so repeat requests can be served with a single map lookup and a memcpy,
// skipping the O(nodes + edges) build loop entirely.
//
// Cache key: "<group>::<params-fingerprint>" where the fingerprint is a
// hex-encoded SHA-256 of the sorted query parameters that affect the output
// (filterKind, filterRepo, repos, includeExternal, includeModules).
//
// Invalidation: any call to graphPayloadCache.InvalidateGroup(group) drops
// ALL entries whose key starts with "<group>::".  This is called from
// GraphCache.Invalidate / InvalidateAll so the two caches are always in sync.

// payloadEntry is one cached response.
type payloadEntry struct {
	body []byte // raw JSON bytes (not compressed — withGzip compresses on write)
	etag string // strong ETag value, including the surrounding quotes
}

// graphPayloadCache is a concurrency-safe store of pre-serialised graph
// payloads.  It is intentionally separate from GraphCache so the two caches
// can be invalidated together without circular dependencies.
type graphPayloadCache struct {
	mu      sync.RWMutex
	entries map[string]*payloadEntry // cache key → entry
}

func newGraphPayloadCache() *graphPayloadCache {
	return &graphPayloadCache{entries: map[string]*payloadEntry{}}
}

// payloadCacheKey returns a stable, collision-resistant key for the given
// (group, params) combination.  The params fingerprint is a truncated
// SHA-256 of the sorted param string so the map key stays short.
func payloadCacheKey(group, filterKind, filterRepo, reposParam string, includeExternal, includeModules bool) string {
	params := fmt.Sprintf("fk=%s&fr=%s&repos=%s&ext=%v&mod=%v",
		filterKind, filterRepo, reposParam, includeExternal, includeModules)
	sum := sha256.Sum256([]byte(params))
	return group + "::" + fmt.Sprintf("%x", sum[:8])
}

// Get returns the cached entry and true when a valid entry exists,
// or nil and false on a miss.
func (c *graphPayloadCache) Get(key string) (*payloadEntry, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	return e, ok
}

// Set stores (or replaces) a payload entry.
func (c *graphPayloadCache) Set(key string, body []byte, etag string) {
	c.mu.Lock()
	c.entries[key] = &payloadEntry{body: body, etag: etag}
	c.mu.Unlock()
}

// InvalidateGroup drops all entries whose key begins with "<group>::".
func (c *graphPayloadCache) InvalidateGroup(group string) {
	prefix := group + "::"
	c.mu.Lock()
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

// InvalidateAll drops every entry.
func (c *graphPayloadCache) InvalidateAll() {
	c.mu.Lock()
	c.entries = map[string]*payloadEntry{}
	c.mu.Unlock()
}

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
//
// The on-disk links file written by the link pass uses "relation" for the
// edge kind; the dashboard struct also accepts "kind" for backward compat
// (tests and older files).  UnmarshalJSON prefers "relation" when present.
type CrossRepoLink struct {
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence,omitempty"`
	Channel    string  `json:"channel,omitempty"`
	Method     string  `json:"method,omitempty"`
}

// UnmarshalJSON handles the "relation" field used by the link pass as a
// synonym for "kind".  When the JSON object has a non-empty "relation" key
// and an absent or empty "kind" key, the relation value is used as Kind.
func (l *CrossRepoLink) UnmarshalJSON(b []byte) error {
	type plain struct {
		Source     string  `json:"source"`
		Target     string  `json:"target"`
		Kind       string  `json:"kind"`
		Relation   string  `json:"relation"`
		Confidence float64 `json:"confidence,omitempty"`
		Channel    string  `json:"channel,omitempty"`
		Method     string  `json:"method,omitempty"`
	}
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	l.Source = p.Source
	l.Target = p.Target
	l.Kind = p.Kind
	if l.Kind == "" && p.Relation != "" {
		l.Kind = p.Relation
	}
	l.Confidence = p.Confidence
	l.Channel = p.Channel
	l.Method = p.Method
	return nil
}

// ---------------------------------------------------------------------------
// GraphCache — mtime-driven per-group cache with TTL
// ---------------------------------------------------------------------------

// cacheEntry holds a loaded group plus the time it was last refreshed.
type cacheEntry struct {
	group    *DashGroup
	loadedAt time.Time
}

// GraphCache is the dashboard's in-memory graph store.  It is safe for
// concurrent use.  Reload is lazy: the first call for a group loads it;
// subsequent calls check mtime and skip the reload when graphs haven't
// changed.
//
// GraphCache also owns a graphPayloadCache so that Invalidate/InvalidateAll
// atomically bust both the loaded-group cache and the pre-serialised payload
// cache.  Handlers call c.Payloads to access the payload cache directly.
type GraphCache struct {
	mu       sync.Mutex
	entries  map[string]*cacheEntry
	loading  map[string]*loadGate // in-flight loads, keyed by group (singleflight)
	ttl      time.Duration
	Payloads *graphPayloadCache // pre-serialised dense graph JSON, keyed by group+params
}

// loadGate coordinates a single in-flight loadGroup call so that N concurrent
// GetGroup callers for the same group do not each kick off a (potentially
// multi-second) disk-load + Pass-4 algorithm run.  The first caller loads; the
// rest wait on done and read the shared result.  Critically, loadGroup runs
// WITHOUT GraphCache.mu held, so a slow group never wedges unrelated groups or
// the cheap cached-read fast path used by first-paint endpoints (#1478).
type loadGate struct {
	done chan struct{}
	grp  *DashGroup
	err  error
}

// NewGraphCache returns a cache with the given TTL.  Use 60 * time.Second
// for production; tests may use a lower value.
func NewGraphCache(ttl time.Duration) *GraphCache {
	return &GraphCache{
		entries:  map[string]*cacheEntry{},
		loading:  map[string]*loadGate{},
		ttl:      ttl,
		Payloads: newGraphPayloadCache(),
	}
}

// GetGroupCached returns the already-loaded group and true, or (nil, false)
// when the group is not warm.  It NEVER loads from disk or runs algorithms,
// so it is safe to call from first-paint / best-effort enrichment paths that
// must not block on a cold or slow group (#1478).  A best-effort warm is
// kicked off in the background so a subsequent request finds it ready.
func (c *GraphCache) GetGroupCached(groupName string) (*DashGroup, bool) {
	c.mu.Lock()
	ent, ok := c.entries[groupName]
	if ok && time.Since(ent.loadedAt) < c.ttl {
		grp := ent.group
		c.mu.Unlock()
		return grp, true
	}
	c.mu.Unlock()
	// Not warm (or stale): trigger an async warm so the next caller is fast,
	// but return immediately so we never block first paint.
	go func() { _, _ = c.GetGroup(groupName) }()
	return nil, false
}

// Invalidate drops the cached entry for group (called on re-index events).
// It also busts the pre-serialised payload cache for that group so the next
// GET /api/graph/{group} request rebuilds a fresh payload from the new graph.
func (c *GraphCache) Invalidate(group string) {
	c.mu.Lock()
	delete(c.entries, group)
	c.mu.Unlock()
	c.Payloads.InvalidateGroup(group)
}

// InvalidateAll drops every cached entry and every pre-serialised payload.
func (c *GraphCache) InvalidateAll() {
	c.mu.Lock()
	c.entries = map[string]*cacheEntry{}
	c.mu.Unlock()
	c.Payloads.InvalidateAll()
}

// GetGroup returns the loaded group, refreshing from disk when the TTL has
// elapsed or when graph files have changed on disk.
func (c *GraphCache) GetGroup(groupName string) (*DashGroup, error) {
	c.mu.Lock()
	now := time.Now()
	if ent, ok := c.entries[groupName]; ok && now.Sub(ent.loadedAt) < c.ttl {
		grp := ent.group
		c.mu.Unlock()
		return grp, nil
	}

	// Cold / stale. Coordinate a single in-flight load via a loadGate so
	// concurrent callers for the same group share one disk-load + Pass-4
	// algorithm run instead of each launching their own. We deliberately
	// release c.mu before running loadGroup: the load can take seconds on a
	// large group, and holding the cache mutex across it would serialise
	// EVERY other group + the cheap cached-read fast path behind it — the
	// exact wedge that made first-paint endpoints (and thus the dashboard)
	// return 000 with a large group registered (#1478).
	if g, ok := c.loading[groupName]; ok {
		c.mu.Unlock()
		<-g.done
		return g.grp, g.err
	}
	gate := &loadGate{done: make(chan struct{})}
	c.loading[groupName] = gate
	c.mu.Unlock()

	grp, err := c.loadGroup(groupName)

	c.mu.Lock()
	if err == nil {
		c.entries[groupName] = &cacheEntry{group: grp, loadedAt: time.Now()}
	}
	delete(c.loading, groupName)
	c.mu.Unlock()

	gate.grp, gate.err = grp, err
	close(gate.done)
	return grp, err
}

// loadGroup reads the registry for groupName and loads each repo's graph.
// It runs WITHOUT c.mu held (see GetGroup) so a slow load never blocks the
// rest of the cache.
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
			// graph.fb (the canonical store) omits community/pagerank/god-node
			// data — those fields live only in graph.json and are not encoded in
			// the FlatBuffers schema. Re-derive them from the loaded entities so
			// the dashboard graph endpoints (centroids, mid, full) see the data.
			if len(doc.Communities) == 0 && len(doc.Entities) > 0 {
				attachAlgorithmResults(doc)
			}
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

	// Load cross-repo links file.  The file can be either a bare array of
	// CrossRepoLink objects or the wrapper format {"version":N,"links":[...]}.
	// Both forms are accepted; the wrapper form is written by the link pass.
	lf := defaultLinksFile(groupName)
	if data, err := os.ReadFile(lf); err == nil {
		if links, err2 := readCrossRepoLinks(data); err2 == nil {
			grp.Links = normalizeLinkEndpoints(links, grp.Repos)
		}
	}

	return grp, nil
}

// normalizeLinkEndpoints rewrites the "<repo-slug>::<entity-id>" endpoints of
// cross-repo links so each one matches the canonical prefixed node ID used by
// the served graph (built with dashPrefixedID(rp.Slug, entity.ID)). Without
// this rewrite the merge guard `visible[l.Source] && visible[l.Target]` in
// buildV2Graph / serveGraphDense never matches and every cross-repo edge is
// silently dropped from the served graph — the #1582 symptom (0 of 37,104
// served edges were cross-repo).
//
// The repo-slug PREFIX written by the link pass diverges from the dashboard
// repo slugs in (at least) two ways observed in the corpus:
//
//   - underscore vs dash:   "upvate_core"  vs  "upvate-core"
//   - short name vs full:   "catalog"      vs  "polyglot-platform-services-catalog"
//
// Because the <entity-id> suffix is globally unique within a group (verified:
// 0 collisions across upvate's 19,613 and polyglot-platform's 1,316 nodes),
// the most reliable resolution is by that suffix: we index every entity ID ->
// its canonical prefixed node ID and rewrite each endpoint to the canonical
// form of the entity it points at. Endpoints whose entity ID is unknown (or
// whose suffix collides — a defensive guard) are left untouched, so the merge
// guard drops them exactly as before.
func normalizeLinkEndpoints(links []CrossRepoLink, repos map[string]*DashRepo) []CrossRepoLink {
	if len(repos) == 0 || len(links) == 0 {
		return links
	}
	// entity-ID suffix -> canonical prefixed node ID. Suffixes seen more than
	// once are recorded as ambiguous and never used for rewriting.
	canonical := make(map[string]string)
	ambiguous := make(map[string]bool)
	for _, rp := range repos {
		if rp == nil || rp.Doc == nil {
			continue
		}
		for i := range rp.Doc.Entities {
			id := rp.Doc.Entities[i].ID
			if _, seen := canonical[id]; seen {
				ambiguous[id] = true
				continue
			}
			canonical[id] = dashPrefixedID(rp.Slug, id)
		}
	}
	rewrite := func(endpoint string) string {
		_, local := dashSplitPrefixed(endpoint)
		if local == "" || ambiguous[local] {
			return endpoint
		}
		if cid, ok := canonical[local]; ok {
			return cid
		}
		return endpoint // unknown entity; leave as-is (merge guard drops it)
	}
	out := make([]CrossRepoLink, len(links))
	for i, l := range links {
		l.Source = rewrite(l.Source)
		l.Target = rewrite(l.Target)
		out[i] = l
	}
	return out
}

// attachAlgorithmResults re-derives community, pagerank, and god-node data
// for a Document loaded from graph.fb.  graph.fb does not encode these
// fields (they are only present in graph.json), so the dashboard server runs
// the same Pass-4 algorithms that the indexer ran at index time.  Results are
// attached in place so subsequent handler calls see the community-derived
// topology needed by serveGraphCentroids and serveGraphMid.
func attachAlgorithmResults(doc *graph.Document) {
	res := graph.RunAlgorithms(doc.Entities, doc.Relationships)
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if cid, ok := res.CommunityID[e.ID]; ok {
			cidCopy := cid
			e.CommunityID = &cidCopy
		}
		if pr, ok := res.PageRank[e.ID]; ok {
			prCopy := pr
			e.PageRank = &prCopy
		}
		if res.GodNodes[e.ID] {
			e.IsGodNode = true
		}
	}
	doc.Communities = res.Communities
	if doc.AlgorithmStats == nil {
		stats := res.Stats
		doc.AlgorithmStats = &stats
	}
}

// defaultLinksFile mirrors mcp.defaultLinksFile.
func defaultLinksFile(group string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".archigraph", "groups", group+"-links.json")
}

// readCrossRepoLinks parses cross-repo links from raw JSON.
// The file can be either a bare array or the wrapper object
// {"version": N, "links": [...]}.  Both formats are accepted.
func readCrossRepoLinks(data []byte) ([]CrossRepoLink, error) {
	// Try bare array first (older format).
	var asArr []CrossRepoLink
	if err := json.Unmarshal(data, &asArr); err == nil {
		return asArr, nil
	}
	// Try wrapped object format {"links":[...]}.
	var asObj struct {
		Links []CrossRepoLink `json:"links"`
	}
	if err := json.Unmarshal(data, &asObj); err != nil {
		return nil, err
	}
	return asObj.Links, nil
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

// groupEntityHit bundles an entity with its owning repo slug for group-wide lookups.
type groupEntityHit struct {
	repo   string
	entity *graph.Entity
}

// buildGroupEntityIndex builds a group-wide map of entity ID → {repo, entity}.
// When the same entity ID appears in multiple repos (should not happen given the
// hash salting, but defensive), the first repo encountered (sorted) wins.
// Used by handleFlowDetail to resolve bridge-step entity IDs that live in
// companion repos (#1905).
func buildGroupEntityIndex(g *DashGroup) map[string]groupEntityHit {
	total := 0
	for _, r := range g.Repos {
		if r.Doc != nil {
			total += len(r.Doc.Entities)
		}
	}
	out := make(map[string]groupEntityHit, total)
	for _, r := range sortedRepos(g) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if _, exists := out[e.ID]; !exists {
				out[e.ID] = groupEntityHit{repo: r.Slug, entity: e}
			}
		}
	}
	return out
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

// groupTopFrameworks scans all http_endpoint / Endpoint / Route entities
// across the group and returns the top-N framework names sorted by usage
// frequency (descending). cap controls the maximum number returned (≤8).
func groupTopFrameworks(grp *DashGroup, cap int) []string {
	freq := map[string]int{}
	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			kind := dashStripScopePrefix(e.Kind)
			// #1217 backward compat: accept all three http endpoint kind strings.
			if !types.IsHTTPEndpointKind(kind) && !strings.EqualFold(kind, httpEndpointKind) &&
				kind != "Endpoint" && kind != "Route" {
				continue
			}
			if fw := e.Properties["framework"]; fw != "" {
				freq[fw]++
			}
		}
	}
	if len(freq) == 0 {
		return nil
	}

	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(freq))
	for k, v := range freq {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	if cap > 0 && len(pairs) > cap {
		pairs = pairs[:cap]
	}
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p.k
	}
	return out
}
