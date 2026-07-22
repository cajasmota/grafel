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

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/flows"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/types"
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
//
// PH1c (#2087): the ref is included in the fingerprint so that two
// requests for the same group but different refs get distinct cache slots.
// Passing ref="" preserves the pre-PH1c behaviour (all refs share a slot).
func payloadCacheKey(group, filterKind, filterRepo, reposParam string, includeExternal, includeModules bool, ref ...string) string {
	refVal := ""
	if len(ref) > 0 {
		refVal = ref[0]
	}
	params := fmt.Sprintf("fk=%s&fr=%s&repos=%s&ext=%v&mod=%v&ref=%s",
		filterKind, filterRepo, reposParam, includeExternal, includeModules, refVal)
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
//
// S8 (#2159): Reader is an mmap'd fbreader.Reader opened alongside Doc.
// Handlers that only need to iterate entities/relationships should use
// Reader.IterateEntities / Reader.IterateRelationships to avoid holding
// a materialised *graph.Document in heap for the duration of the
// request. Reader is nil when graph.fb is not present (JSON-only fallback).
type DashRepo struct {
	Slug   string
	Path   string
	Doc    *graph.Document
	Reader *fbreader.Reader // mmap zero-copy reader (S8, #2159); nil when unavailable
	mtime  time.Time
	err    string
}

// ---------------------------------------------------------------------------
// DashGroup — all repos for one group plus cross-repo links
// ---------------------------------------------------------------------------

// DashGroup holds loaded repos and links for one group.
type DashGroup struct {
	Name   string
	Repos  map[string]*DashRepo // slug -> repo
	Links  []CrossRepoLink
	Search *SearchIndex // pre-built search index; nil until buildSearchIndex runs

	// stateDirs maps each repo slug to the on-disk state directory the graph
	// was loaded from. Captured at load so diskUnchanged can re-stat the
	// graph.fb / graph.json without a registry round-trip and decide whether
	// the cached group is still fresh (mtime-keyed durable cache, replacing the
	// old 60s wall-clock TTL — #50).
	stateDirs map[string]string

	// pendingAlgo lists repos whose served graph omitted Pass-4 (and had no
	// fresh sidecar), so the full sweep is computed OFF the request after this
	// group is published (schedulePendingAlgo, #50). Read-only after load.
	pendingAlgo []pendingAlgoRepo
}

// diskUnchanged reports whether the graph.fb (or graph.json fallback) for every
// repo still has the same mtime it had when the group was loaded. It is the
// freshness test for the durable, mtime-keyed cache (#50): a loaded group stays
// cached until its underlying graph file actually changes (a re-index), instead
// of being aged out by a 60s timer that discarded a possibly multi-minute warm.
//
// When no stateDirs were captured (defensive / test-injected groups) or a file
// has since disappeared, it returns true so the cache falls back to explicit
// Invalidate() (called on re-index events) rather than looping on reload.
func (g *DashGroup) diskUnchanged() bool {
	if g == nil {
		return false
	}
	if len(g.stateDirs) == 0 {
		return true
	}
	for slug, dir := range g.stateDirs {
		dr := g.Repos[slug]
		if dr == nil {
			continue
		}
		cur := statGraphMtime(dir)
		if cur.IsZero() {
			continue // file gone — don't force a reload loop
		}
		if !cur.Equal(dr.mtime) {
			return false
		}
	}
	return true
}

// statGraphMtime returns the modification time of the active graph in
// stateDir (single-file .fb or segment-set manifest.json, via
// graph.CurrentGraphMtime), falling back to graph.json, or the zero time
// when neither exists. #5915 J2 slice-2: the old
// os.Stat(graph.CurrentGraphPath(stateDir)) gate only ever resolved a flat
// .fb path, so a segment-set repo (graph.<gen>/ dir + manifest.json, no flat
// .fb) fell straight through to the zero time here.
func statGraphMtime(stateDir string) time.Time {
	if mt, ok := graph.CurrentGraphMtime(stateDir); ok { // #5891, #5915 J2 slice-2
		return mt
	}
	if info, e := os.Stat(filepath.Join(stateDir, "graph.json")); e == nil {
		return info.ModTime()
	}
	return time.Time{}
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

	// Enrichment fields resolved from the source/target entities at serve time
	// (#4596). They are NOT persisted to the on-disk links file — UnmarshalJSON
	// ignores them — and are populated by enrichLinkEndpoints just before the
	// /links payload is written, so the frontend can render readable names and
	// open a real source-peek instead of only a graph deep-link fallback.
	SourceName          string `json:"source_name,omitempty"`
	SourceQualifiedName string `json:"source_qualified_name,omitempty"`
	SourceFile          string `json:"source_file,omitempty"`
	SourceLine          int    `json:"source_line,omitempty"`
	TargetName          string `json:"target_name,omitempty"`
	TargetQualifiedName string `json:"target_qualified_name,omitempty"`
	TargetFile          string `json:"target_file,omitempty"`
	TargetLine          int    `json:"target_line,omitempty"`

	// Module sub-paths owning each endpoint within its repo (#4698), derived
	// from the source/target file and the repo's configured monorepo module
	// roots. Empty for single-repo groups or files under no module root. Let the
	// scope selector keep a link at module precision when either endpoint lives
	// in the scoped module.
	SourceModulePath string `json:"source_module_path,omitempty"`
	TargetModulePath string `json:"target_module_path,omitempty"`
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
	warmErrs map[string]error     // last load error per cache key (#5722), cleared on success
	// algoComputed records, per repo state dir, the graph.fb mtime (UnixNano) for
	// which the expensive Pass-4 sweep has ALREADY been computed this process
	// lifetime — regardless of whether persistAlgoResults managed to write the
	// sidecar. It is the termination guard for schedulePendingAlgo's
	// compute→evict→reload cycle: persistAlgoResults is best-effort and swallows
	// write failures (read-only state dir, disk-full, EPERM), and when it fails the
	// reloaded graph still omits Pass-4, so applyAlgorithmsOnLoad re-flags the repo
	// as pending and the group is recomputed on EVERY reload — an unbounded,
	// CPU-bound Pass-4 spin (RunAlgorithms is O(V·E) betweenness) that pins serve
	// while the dashboard polls the group's graph endpoint. Keying the guard in
	// memory makes "compute once per graph.fb version" hold even when the on-disk
	// sidecar cannot. A genuine re-index bumps graph.fb's mtime, so the new version
	// is correctly recomputed (the recorded mtime no longer matches).
	algoComputed map[string]int64 // repo stateDir -> fb mtime UnixNano already computed
	// ttl is retained for API compatibility but NO LONGER governs freshness
	// (#50): a loaded group stays cached until its graph.fb changes on disk
	// (diskUnchanged) or it is explicitly Invalidate()d on a re-index event.
	// The old 60s wall-clock TTL evicted multi-minute Pass-4 warms before they
	// could be reused; mtime-keyed invalidation keeps them until the data
	// actually changes.
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
		entries:      map[string]*cacheEntry{},
		loading:      map[string]*loadGate{},
		warmErrs:     map[string]error{},
		algoComputed: map[string]int64{},
		ttl:          ttl,
		Payloads:     newGraphPayloadCache(),
	}
}

// GetGroupCached returns the already-loaded group and true, or (nil, false)
// when the group is not warm.  It NEVER loads from disk or runs algorithms,
// so it is safe to call from first-paint / best-effort enrichment paths that
// must not block on a cold or slow group (#1478).  A best-effort warm is
// kicked off in the background so a subsequent request finds it ready.
func (c *GraphCache) GetGroupCached(groupName string) (*DashGroup, bool) {
	return c.GetGroupCachedForRef(groupName, "")
}

// GetGroupCachedForRef is the ref-scoped variant of GetGroupCached (PH1c).
func (c *GraphCache) GetGroupCachedForRef(groupName, ref string) (*DashGroup, bool) {
	cacheKey := groupName
	if ref != "" {
		cacheKey = groupName + "@" + ref
	}
	c.mu.Lock()
	ent, ok := c.entries[cacheKey]
	c.mu.Unlock()
	// Durable cache (#50): a warm entry stays valid until its graph file
	// changes on disk, NOT until a 60s timer elapses. This keeps a (possibly
	// background-computed) multi-minute Pass-4 warm reusable instead of being
	// TTL-evicted before a second request can benefit from it.
	if ok && ent.group.diskUnchanged() {
		return ent.group, true
	}
	// Not warm (or the file changed): trigger an async warm so the next caller
	// is fast, but return immediately so we never block first paint.
	go func() { _, _ = c.GetGroupForRef(groupName, ref) }()
	return nil, false
}

// LastWarmError returns the error from the most recent load attempt for
// groupName/ref, if any load has been attempted and the most recent one
// failed. It returns (nil, false) when no attempt has failed yet — either
// because the group is warm, or because it simply hasn't finished its first
// warm attempt (the ordinary "still warming" case). Callers use this to
// distinguish a genuine load failure (#5722) from a group that just hasn't
// warmed up yet, so a failure can be surfaced instead of retried forever.
func (c *GraphCache) LastWarmError(groupName, ref string) (error, bool) {
	cacheKey := groupName
	if ref != "" {
		cacheKey = groupName + "@" + ref
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err, ok := c.warmErrs[cacheKey]
	return err, ok
}

// Invalidate drops the cached entry for group and all per-ref variants
// (called on re-index events). It also busts the pre-serialised payload
// cache for that group so the next GET /api/graph/{group} request rebuilds
// a fresh payload from the new graph.
//
// S8 (#2159): mmap readers held by DashRepo entries are closed so the OS
// can reclaim file descriptors and page-cache pages for the old graph.fb.
//
// #5722 follow-up: a re-index/invalidate means the underlying condition that
// may have caused a PRIOR warm failure has presumably changed, so any
// recorded warmErrs entry for group (and its per-ref variants) is dropped
// here too. Without this, LastWarmError would keep surfacing the stale
// failure to a connected dashboard client until some unrelated caller
// happened to trigger another load attempt for that exact group/ref. A
// genuinely still-broken source will simply re-record the error on the next
// load attempt (kicked off in the background by GetGroupCachedForRef) — this
// only clears the stale signal, it never suppresses a real recurring one.
func (c *GraphCache) Invalidate(group string) {
	prefix := group + "@"
	c.mu.Lock()
	if ent, ok := c.entries[group]; ok {
		closeDashGroupReaders(ent.group)
		delete(c.entries, group)
	}
	// Also evict all per-ref entries for this group.
	for k, ent := range c.entries {
		if strings.HasPrefix(k, prefix) {
			closeDashGroupReaders(ent.group)
			delete(c.entries, k)
		}
	}
	// Clear any recorded warm-load failure for group and its per-ref variants
	// (#5722 follow-up) so a stale error does not keep being surfaced after a
	// re-index resolves the underlying problem.
	delete(c.warmErrs, group)
	for k := range c.warmErrs {
		if strings.HasPrefix(k, prefix) {
			delete(c.warmErrs, k)
		}
	}
	c.mu.Unlock()
	c.Payloads.InvalidateGroup(group)
}

// InvalidateAll drops every cached entry and every pre-serialised payload.
// S8 (#2159): mmap readers are closed before clearing the map.
//
// #5722 follow-up: every recorded warmErrs entry is cleared too, for the
// same reason as Invalidate — a global re-index/invalidation should not
// leave stale warm-failure signals behind for callers to keep surfacing.
func (c *GraphCache) InvalidateAll() {
	c.mu.Lock()
	for _, ent := range c.entries {
		closeDashGroupReaders(ent.group)
	}
	c.entries = map[string]*cacheEntry{}
	c.warmErrs = map[string]error{}
	c.mu.Unlock()
	c.Payloads.InvalidateAll()
}

// closeDashGroupReaders releases every mmap'd fbreader.Reader held by repos
// in grp. Safe to call with nil grp. Errors are intentionally swallowed —
// closing a reader is best-effort; a leaked fd is never worse than a crash.
func closeDashGroupReaders(grp *DashGroup) {
	if grp == nil {
		return
	}
	for _, dr := range grp.Repos {
		if dr != nil && dr.Reader != nil {
			_ = dr.Reader.Close()
			dr.Reader = nil
		}
	}
}

// GetGroup returns the loaded group, refreshing from disk when the TTL has
// elapsed or when graph files have changed on disk.
func (c *GraphCache) GetGroup(groupName string) (*DashGroup, error) {
	return c.GetGroupForRef(groupName, "")
}

// GetGroupForRef returns the loaded group for a specific git ref.
// When ref is "" it behaves identically to GetGroup (reads the current
// HEAD ref via daemon.StateDirForRepo for each repo).
//
// PH1c (#2087): passing a non-empty ref scopes the graph read to that
// ref's state directory (refs/<ref-safe>/graph.fb). The cache key
// includes groupName+ref so different refs coexist in the cache.
func (c *GraphCache) GetGroupForRef(groupName, ref string) (*DashGroup, error) {
	cacheKey := groupName
	if ref != "" {
		cacheKey = groupName + "@" + ref
	}
	c.mu.Lock()
	if ent, ok := c.entries[cacheKey]; ok {
		grp := ent.group
		c.mu.Unlock()
		// Durable cache (#50): serve the cached group as long as its underlying
		// graph file has not changed on disk. Statting is done OUTSIDE c.mu (it
		// is I/O) so it never wedges other groups.
		if grp.diskUnchanged() {
			return grp, nil
		}
		// The graph file changed under us (a re-index that did not route through
		// Invalidate): evict this stale entry and fall through to a fresh load.
		c.mu.Lock()
		if cur, ok := c.entries[cacheKey]; ok && cur.group == grp {
			closeDashGroupReaders(grp)
			delete(c.entries, cacheKey)
		}
		c.mu.Unlock()
		c.mu.Lock()
	}

	// Cold / stale. Coordinate a single in-flight load via a loadGate so
	// concurrent callers for the same group share one disk-load + Pass-4
	// algorithm run instead of each launching their own. We deliberately
	// release c.mu before running loadGroup: the load can take seconds on a
	// large group, and holding the cache mutex across it would serialise
	// EVERY other group + the cheap cached-read fast path behind it — the
	// exact wedge that made first-paint endpoints (and thus the dashboard)
	// return 000 with a large group registered (#1478).
	if g, ok := c.loading[cacheKey]; ok {
		c.mu.Unlock()
		<-g.done
		return g.grp, g.err
	}
	gate := &loadGate{done: make(chan struct{})}
	c.loading[cacheKey] = gate
	c.mu.Unlock()

	grp, err := c.loadGroupForRef(groupName, ref)

	c.mu.Lock()
	if err == nil {
		c.entries[cacheKey] = &cacheEntry{group: grp, loadedAt: time.Now()}
		delete(c.warmErrs, cacheKey)
	} else {
		// Record the failure (#5722) so a subsequent best-effort caller (e.g.
		// the graph-stream endpoint) can distinguish "genuinely failed" from
		// "just hasn't warmed yet" instead of surfacing an eternal retry.
		c.warmErrs[cacheKey] = err
	}
	delete(c.loading, cacheKey)
	c.mu.Unlock()

	gate.grp, gate.err = grp, err
	close(gate.done)

	// The group is now published in the cache (entry stored above). Compute any
	// deferred Pass-4 OFF the request, read-only over the published docs, then
	// evict so the next request reloads with it applied (#50). Scheduled AFTER
	// the entry store so the goroutine's eviction can find and drop it (no
	// lost-wakeup).
	if err == nil {
		c.schedulePendingAlgo(cacheKey, grp)
	}
	return grp, err
}

// loadGroup reads the registry for groupName and loads each repo's graph.
// It runs WITHOUT c.mu held (see GetGroup) so a slow load never blocks the
// rest of the cache.
func (c *GraphCache) loadGroup(groupName string) (*DashGroup, error) {
	return c.loadGroupForRef(groupName, "")
}

// loadGroupForRef loads the group graph for a specific ref. When ref is ""
// it delegates to daemon.StateDirForRepo (which reads the current HEAD via
// gitmeta). When ref is non-empty it reads from daemon.StateDirForRepoRef.
// It runs WITHOUT c.mu held (see GetGroupForRef).
func (c *GraphCache) loadGroupForRef(groupName, ref string) (*DashGroup, error) {
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
		Name:      groupName,
		Repos:     map[string]*DashRepo{},
		stateDirs: map[string]string{},
	}
	for _, r := range cfg.Repos {
		dr := &DashRepo{Slug: r.Slug, Path: r.Path}
		var stateDir string
		if ref != "" {
			stateDir = daemon.StateDirForRepoRef(r.Path, ref)
		} else {
			stateDir = daemon.StateDirForRepo(r.Path)
		}
		grp.stateDirs[r.Slug] = stateDir
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			dr.err = err.Error()
		} else {
			// Record the newer of fb/json mtime for cache invalidation.
			dr.mtime = statGraphMtime(stateDir)
			// graph.fb (the canonical store) omits community/pagerank/centrality/
			// god-node/articulation data — those fields live only in graph.json
			// and are not encoded in the FlatBuffers schema. For a group indexed
			// with --skip-pass=graph-algo, doc.Communities is empty. Fix A#1 (#50):
			// NEVER block this load on the full multi-minute Pass-4 sweep. Apply a
			// fresh sidecar if present (Fix B); otherwise stamp a cheap fallback
			// ordering now and defer the real sweep to schedulePendingAlgo (run
			// after the group is published), which is read-only over this doc.
			if applyAlgorithmsOnLoad(doc, stateDir, dr.mtime) {
				grp.pendingAlgo = append(grp.pendingAlgo, pendingAlgoRepo{
					doc: doc, stateDir: stateDir, fbMtime: dr.mtime,
				})
			}
			dr.Doc = doc
			// Description side-table (#5904 PR-a): ADDITIVELY stamp any
			// <stateDir>/descriptions.json onto this repo's entities so the
			// PropGet("description") readers (openapi/DAG export) surface agent
			// write-back descriptions again now that write-back no longer rewrites
			// the graph. Absence/corrupt/stale-tolerant: a miss leaves the entity's
			// own (extractor-native) description intact.
			if sc, ok := descriptions.Read(stateDir); ok {
				for i := range doc.Entities {
					if d, has := sc.Results[doc.Entities[i].ID]; has {
						doc.Entities[i].PropSet("description", d)
					}
				}
			}
			// Flow side-table (#5904 PR-b): REPLACE-merge the per-repo
			// <stateDir>/flows.json onto this repo's Document so the flow handlers
			// (handlers_flows / handlers_event_flows / v2_flows), which loop
			// r.Doc.Entities/Relationships by Kind, see the cross-repo-aware flows
			// the phantom pass produced instead of the index-baked intra-repo ones.
			// MergeInto SUPPRESSES the baked SCOPE.Process/SCOPE.EventFlow entities +
			// their STEP/ENTRY/SEED edges and substitutes the sidecar's (never
			// additive → never doubled). Absence/corrupt/stale → no-op, leaving the
			// baked intra-repo flows (correct degraded state).
			flows.MergeInto(stateDir, doc)
			// S8 (#2159): open a zero-copy mmap reader alongside the Document
			// so handlers that only need to iterate entities/relationships can
			// avoid materialising the full heap slice. Best-effort: failures
			// leave Reader nil and callers fall back to doc.Entities.
			fbPath := graph.CurrentGraphPath(stateDir) // #5891: resolve active gen
			if rdr, rerr := fbreader.Open(fbPath); rerr == nil {
				dr.Reader = rdr
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

	// Build the in-memory search index once at load time so that
	// /api/search/{group} does not need to scan all entities on every request
	// (#2104).
	grp.Search = buildSearchIndex(grp)

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
//   - underscore vs dash:   "acme_core"  vs  "acme-core"
//   - short name vs full:   "catalog"      vs  "polyglot-platform-services-catalog"
//
// Because the <entity-id> suffix is globally unique within a group (verified:
// 0 collisions across acme's 19,613 and polyglot-platform's 1,316 nodes),
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

// enrichLinkEndpoints resolves each link's source and target entity (by its
// prefixed "<repo>::<localId>" id) via the same findEntity lookup the rest of
// the dashboard uses, and copies the entity's name / qualified name / source
// file / start line onto the link (#4596). This lets the /links page render a
// readable source name and open a real source-peek rather than only a graph
// deep-link fallback.
//
// It is additive and best-effort: an endpoint that does not resolve to an
// entity (e.g. a synthetic scope.operation node #4554 or a bare-external
// target #4558 that has no source-derived name) simply leaves its enrichment
// fields empty, and the frontend falls back to the graph deep-link.
// enrichLinkEndpoints resolves each link's source/target entity for readable
// rendering and source-peek (#4596) and stamps each endpoint's monorepo
// module_path (#4698) using moduleRoots (parent-repo slug → configured module
// roots). moduleRoots may be nil for single-repo / non-monorepo groups, in
// which case module paths stay empty.
func enrichLinkEndpoints(grp *DashGroup, links []CrossRepoLink, moduleRoots map[string][]string) []CrossRepoLink {
	if grp == nil || len(links) == 0 {
		return links
	}
	out := make([]CrossRepoLink, len(links))
	for i, l := range links {
		if rp, e := findEntity(grp, l.Source); e != nil {
			l.SourceName = e.Name
			l.SourceQualifiedName = e.QualifiedName
			l.SourceFile = e.SourceFile
			l.SourceLine = e.StartLine
			if rp != nil {
				l.SourceModulePath = modulePathFor(rp.Slug, e.SourceFile, moduleRoots)
			}
		}
		if rp, e := findEntity(grp, l.Target); e != nil {
			l.TargetName = e.Name
			l.TargetQualifiedName = e.QualifiedName
			l.TargetFile = e.SourceFile
			l.TargetLine = e.StartLine
			if rp != nil {
				l.TargetModulePath = modulePathFor(rp.Slug, e.SourceFile, moduleRoots)
			}
		}
		out[i] = l
	}
	return out
}

// algoSidecarName is the file (written next to graph.fb in the repo state dir)
// that persists a computed Pass-4 result so a later cold load — or a daemon
// restart — can apply it cheaply instead of recomputing the multi-minute sweep
// (Fix B, #50). It is keyed to the graph.fb mtime it was computed against, so a
// re-index (new graph.fb) transparently invalidates it.
const algoSidecarName = "graph-algo.json"

// persistedAlgo is the on-disk sidecar payload: the full Pass-4 result plus the
// graph.fb mtime it was computed against (for staleness detection).
type persistedAlgo struct {
	FBMtimeUnixNano int64                   `json:"fb_mtime_unix_nano"`
	Results         *graph.AlgorithmResults `json:"results"`
}

// backgroundAlgoGate, when non-nil, blocks a scheduled background Pass-4
// computation until it receives — a TEST-ONLY seam so a test can deterministically
// assert the load returned WITHOUT the full sweep having run. Never set in prod.
var backgroundAlgoGate chan struct{}

// backgroundAlgoDone, when non-nil, is invoked with the cache key after a
// background Pass-4 computation has been persisted AND its cache entry evicted.
// Test-only seam so a test can wait for the async work without polling. Never
// set in prod.
var backgroundAlgoDone func(cacheKey string)

// pendingAlgoRepo names a repo whose served graph.fb omitted Pass-4 and whose
// sidecar was absent/stale, so the full sweep must be computed off the request
// (Fix A#1, #50). doc holds the already-materialised (heap) Entities/Relationships
// the sweep READS — never a mmap reader — so a stale sweep after eviction is
// harmless (concern #2).
type pendingAlgoRepo struct {
	doc      *graph.Document
	stateDir string
	fbMtime  time.Time
}

// applyAlgorithmsOnLoad fills a served graph's Pass-4 attributes on the
// SYNCHRONOUS load path (before the group is published through the cache, so no
// concurrent reader exists yet — the writes here are race-free). graph.fb omits
// Pass-4, so for a --skip-pass=graph-algo group doc.Communities is empty.
// Priority:
//
//  1. a fresh persisted sidecar (Fix B) — apply it cheaply, no compute → false;
//  2. otherwise stamp a cheap degree-based fallback ordering so the graph is
//     immediately usable, and return true so the caller schedules the real
//     Pass-4 to be computed OFF the request (never mutating this now-published,
//     concurrently-read doc — data-race fix, #50).
func applyAlgorithmsOnLoad(doc *graph.Document, stateDir string, fbMtime time.Time) (needsBackground bool) {
	if len(doc.Communities) != 0 || len(doc.Entities) == 0 {
		return false
	}
	if res, ok := loadPersistedAlgoResults(stateDir, fbMtime); ok {
		applyAlgorithmResults(doc, res)
		return false
	}
	attachDegreeFallback(doc)
	return true
}

// schedulePendingAlgo computes the full Pass-4 sweep for every pending repo of a
// just-published group, OFF the request goroutine (Fix A#1, #50). It is
// deliberately READ-ONLY over each published doc: it never calls
// applyAlgorithmResults on the live, concurrently-read doc (the data race the
// review caught). Instead it persists the computed result to the sidecar and
// then EVICTS this exact cache entry, so the next request reloads the group and
// applies the freshly-persisted Pass-4 synchronously (pre-publish) — publishing
// atomically through the same c.mu-guarded cache every handler already reads.
func (c *GraphCache) schedulePendingAlgo(cacheKey string, grp *DashGroup) {
	if grp == nil || len(grp.pendingAlgo) == 0 {
		return
	}
	// Drop any repo whose current graph.fb version was ALREADY Pass-4-computed this
	// process (algoComputed). This is the loop-termination guard: when
	// persistAlgoResults fails (best-effort, swallowed), the reloaded graph still
	// omits Pass-4 and applyAlgorithmsOnLoad re-flags the repo as pending, so
	// without this filter the group would recompute the multi-minute sweep on
	// EVERY reload and pin serve. If NOTHING is left to compute we must NOT evict:
	// evicting here would force yet another reload → re-flag → re-enter, sustaining
	// the very spin we are breaking. Returning without eviction keeps the entry
	// warm (served with the cheap degree fallback when the sidecar could not
	// persist — graceful degradation, never a CPU spin).
	pending := c.filterAlreadyComputed(grp.pendingAlgo)
	if len(pending) == 0 {
		return
	}
	go func() {
		if backgroundAlgoGate != nil {
			<-backgroundAlgoGate
		}
		for _, p := range pending {
			// READ-ONLY over p.doc.Entities/Relationships (RunAlgorithms mutates
			// neither, and touches only heap slices — not the mmap reader). Safe
			// to run concurrently with handlers reading the same doc, and safe if
			// the group was already evicted under us (concern #2).
			res := graph.RunAlgorithms(p.doc.Entities, p.doc.Relationships)
			persistAlgoResults(p.stateDir, p.fbMtime, res)
			// Record that this graph.fb version has been computed BEFORE the evict
			// below, so a reload triggered by the eviction filters this repo out
			// (termination) even if the persist above failed.
			c.markAlgoComputed(p.stateDir, p.fbMtime)
		}
		// Evict THIS entry (only if it is still the group we computed for) so the
		// next request reloads with the persisted Pass-4 applied synchronously.
		c.mu.Lock()
		if ent, ok := c.entries[cacheKey]; ok && ent.group == grp {
			closeDashGroupReaders(ent.group)
			delete(c.entries, cacheKey)
		}
		c.mu.Unlock()
		if backgroundAlgoDone != nil {
			backgroundAlgoDone(cacheKey)
		}
	}()
}

// filterAlreadyComputed returns the subset of pending whose repos have NOT yet
// had Pass-4 computed for their current graph.fb mtime this process (see
// algoComputed). A repo with a zero fbMtime is never filtered (we cannot key it
// safely), so a graph whose file has vanished still recomputes rather than being
// silently skipped forever. It does not mutate the input slice.
func (c *GraphCache) filterAlreadyComputed(pending []pendingAlgoRepo) []pendingAlgoRepo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]pendingAlgoRepo, 0, len(pending))
	for _, p := range pending {
		if !p.fbMtime.IsZero() {
			if done, ok := c.algoComputed[p.stateDir]; ok && done == p.fbMtime.UnixNano() {
				continue // already computed this exact graph.fb version
			}
		}
		out = append(out, p)
	}
	return out
}

// markAlgoComputed records that stateDir's graph.fb at fbMtime has had its Pass-4
// sweep computed this process, so filterAlreadyComputed drops it on the next
// reload even if the sidecar write failed.
func (c *GraphCache) markAlgoComputed(stateDir string, fbMtime time.Time) {
	if fbMtime.IsZero() {
		return
	}
	c.mu.Lock()
	c.algoComputed[stateDir] = fbMtime.UnixNano()
	c.mu.Unlock()
}

// attachDegreeFallback stamps a cheap degree-based PageRank proxy on every
// connected entity so importance-ordered views (e.g. the graph stream's
// important-first ordering) have a sensible ordering the instant the graph is
// served — before the real Pass-4 lands in the background. O(entities+edges),
// no matrix math. Overwritten by the real PageRank when the background sweep
// completes.
func attachDegreeFallback(doc *graph.Document) {
	deg := make(map[string]int, len(doc.Entities))
	for i := range doc.Relationships {
		deg[doc.Relationships[i].FromID]++
		deg[doc.Relationships[i].ToID]++
	}
	maxDeg := 0
	for _, d := range deg {
		if d > maxDeg {
			maxDeg = d
		}
	}
	if maxDeg == 0 {
		return
	}
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if d := deg[e.ID]; d > 0 {
			pr := float64(d) / float64(maxDeg)
			e.PageRank = &pr
		}
	}
}

// attachAlgorithmResults computes AND applies the full Pass-4 sweep synchronously.
// Retained for callers/tests that want the derived attributes inline; the live
// dashboard load path uses deriveOrScheduleAlgorithms instead so it never blocks.
func attachAlgorithmResults(doc *graph.Document) {
	res := graph.RunAlgorithms(doc.Entities, doc.Relationships)
	applyAlgorithmResults(doc, res)
}

// applyAlgorithmResults copies a computed Pass-4 result onto a Document in place.
//
// #50 discard-bug fix: the previous implementation kept ONLY community, pagerank
// and god-node data and silently DISCARDED the betweenness Centrality and
// ArticulationPoints it had already paid to compute. All computed attributes are
// now written (centrality + articulation + surprise), and the result is
// persisted (see persistAlgoResults) so subsequent cold loads reuse it.
func applyAlgorithmResults(doc *graph.Document, res *graph.AlgorithmResults) {
	if res == nil {
		return
	}
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
		if bt, ok := res.Centrality[e.ID]; ok {
			btCopy := bt
			e.Centrality = &btCopy
		}
		if res.GodNodes[e.ID] {
			e.IsGodNode = true
		}
		if res.ArticulationPoints[e.ID] {
			e.IsArticulationPt = true
		}
		if res.SurpriseEndpoints[e.ID] {
			e.IsSurpriseEndpoint = true
		}
	}
	doc.Communities = res.Communities
	if len(res.SurpriseEdges) > 0 {
		doc.SurpriseEdges = res.SurpriseEdges
	}
	if doc.AlgorithmStats == nil {
		stats := res.Stats
		doc.AlgorithmStats = &stats
	}
}

// loadPersistedAlgoResults reads the Pass-4 sidecar from stateDir, returning the
// stored result only when it was computed against the CURRENT graph.fb mtime
// (staleness guard). A missing / malformed / stale sidecar returns (nil, false)
// so the caller recomputes.
func loadPersistedAlgoResults(stateDir string, fbMtime time.Time) (*graph.AlgorithmResults, bool) {
	data, err := os.ReadFile(filepath.Join(stateDir, algoSidecarName))
	if err != nil {
		return nil, false
	}
	var p persistedAlgo
	if json.Unmarshal(data, &p) != nil || p.Results == nil {
		return nil, false
	}
	if !fbMtime.IsZero() && p.FBMtimeUnixNano != fbMtime.UnixNano() {
		return nil, false // stale: graph.fb changed since this was computed
	}
	return p.Results, true
}

// persistAlgoResults writes the Pass-4 sidecar next to graph.fb, keyed to the
// graph.fb mtime it was computed against. The write is ATOMIC (temp file +
// rename) so a concurrent loadPersistedAlgoResults never observes a torn,
// half-written sidecar (review non-blocking #1). Best-effort: any failure just
// means the next cold load recomputes.
func persistAlgoResults(stateDir string, fbMtime time.Time, res *graph.AlgorithmResults) {
	if res == nil {
		return
	}
	data, err := json.Marshal(persistedAlgo{
		FBMtimeUnixNano: fbMtime.UnixNano(),
		Results:         res,
	})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(stateDir, algoSidecarName+".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, filepath.Join(stateDir, algoSidecarName)); err != nil {
		_ = os.Remove(tmpName)
	}
}

// defaultLinksFile mirrors mcp.defaultLinksFile.
func defaultLinksFile(group string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".grafel", "groups", group+"-links.json")
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
			// First try exact ID match.
			for i := range r.Doc.Entities {
				if r.Doc.Entities[i].ID == local {
					return r, &r.Doc.Entities[i]
				}
			}
			// Synthetic IDs produced during JS/TS extraction have the form
			// "Kind:Name" (e.g. "Function:confirmTransfer") — they never match
			// hex entity IDs. Resolve by extracting the name portion and falling
			// back to a name-based lookup within the same repo.
			if idx := strings.LastIndex(local, ":"); idx >= 0 {
				name := local[idx+1:]
				for i := range r.Doc.Entities {
					if r.Doc.Entities[i].Name == name {
						return r, &r.Doc.Entities[i]
					}
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
	if e.PropLen() > 0 {
		out["properties"] = e.PropsSnapshot()
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
			if fw := e.PropGet("framework"); fw != "" {
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
