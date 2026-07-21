// Package graph — load.go provides the shared FB-first graph loader
// introduced by ADR-0016 flip-day (issue #808).
//
// LoadGraphFromDir tries graph.fb first (via fbreader mmap + decode),
// then falls back to graph.json. All callers that previously opened
// graph.json directly should migrate to this helper so the code-base
// picks up the binary fast-path as graph.fb becomes universally present.
//
// Daemon MCP reads graph.fb via graph_cache.go (zero-copy mmap path,
// no conversion). This helper is for CLI consumers (audit, quality,
// links, doctor, dashboard) where eager conversion to *Document is
// acceptable and simplicity matters more than zero-copy.
package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbversion"
)

// minSupportedFBFormatVersion is the lowest graph.fb FormatVersion the loader
// will accept. The value is sourced from internal/graph/fbversion — a leaf
// package shared with fbwriter — so drift becomes a compile-time error.
const minSupportedFBFormatVersion = fbversion.Version

// FormatVersionError is returned (wrapped, via %w) by loadFBDocument when an
// on-disk graph.fb was stamped with a FormatVersion older than this binary
// requires. It exists as a typed error — rather than forcing callers to
// string-match the human-readable message below — so a caller like
// internal/mcp's reload loop can detect "this specific repo's on-disk graph
// is version-incompatible" via errors.As and record durable state (e.g. a
// statusfile ReindexRequired flag), instead of only stashing the opaque error
// string and silently continuing (the "silent-green lie" this type exists to
// let callers fix).
type FormatVersionError struct {
	// Found is the FormatVersion actually stamped on the rejected graph.fb.
	Found int
	// Required is the minimum FormatVersion this binary accepts
	// (fbversion.Version at build time).
	Required int
}

// Error implements the error interface. The wording is part of the loader's
// user-facing contract (internal/graph/fbwriter/writer_test.go asserts on
// substrings of it verbatim) and must keep pointing the user at `grafel
// index <repo>`.
func (e *FormatVersionError) Error() string {
	return fmt.Sprintf(
		"graph.fb format version %d is older than required version %d — please reindex (run: grafel index <repo>)",
		e.Found, e.Required,
	)
}

// ReindexRequiredReason performs a cheap, header-only check of dir's
// graph.fb (no entity/relationship materialization — just an mmap open plus
// one scalar read) and reports whether it was written by an older grafel
// build than this binary supports.
//
// required is false — the overwhelmingly common case — when graph.fb is
// absent, unreadable, or already at/above minSupportedFBFormatVersion; a
// caller must not treat that as an error, only as "nothing to report".
// required is true only when the on-disk FormatVersion is strictly below
// what this binary accepts, in which case reason names both versions and
// points at the fix, mirroring FormatVersionError's wording so a human sees
// one consistent message whether they hit it via a failed MCP reload or via
// the persisted status-plane sidecar.
//
// This is deliberately independent of any single load attempt: it is safe
// to call on every status-plane heartbeat (see internal/daemon's
// writeRepoStatusFile) so the persisted ReindexRequired flag is always
// freshly recomputed from the ACTUAL bytes on disk, never a one-shot flag
// that could go stale or get clobbered by a later write from a different
// writer.
func ReindexRequiredReason(dir string) (required bool, reason string) {
	fbPath := filepath.Join(dir, "graph.fb")
	r, err := fbreader.Open(fbPath)
	if err != nil {
		return false, ""
	}
	defer r.Close()
	v := r.Version()
	if v >= minSupportedFBFormatVersion {
		return false, ""
	}
	return true, FormatVersionReason(v, minSupportedFBFormatVersion)
}

// FormatVersionReason renders the shared human-readable "reindex required"
// explanation naming both the found and required graph.fb format versions.
// Exported so every caller that detects a *FormatVersionError (the header-only
// ReindexRequiredReason check above, and internal/mcp's reload loop reacting
// to a failed LoadGraphFromDir) renders the IDENTICAL wording — one consistent
// message regardless of which code path first observed the incompatibility.
func FormatVersionReason(found, required int) string {
	fvErr := &FormatVersionError{Found: found, Required: required}
	return fmt.Sprintf(
		"graph format v%d incompatible with v%d — reindex required (%s)",
		found, required, fvErr.Error(),
	)
}

// LoadGraphFromDir loads a graph.Document from dir, where dir is the
// .grafel state directory for a repo (the directory that contains
// graph.fb / graph.json).
//
// Strategy (ADR-0016 flip-day, #808):
//  1. If graph.fb exists and graph.json does NOT, read graph.fb.
//  2. If graph.fb exists and graph.json ALSO exists, prefer the newer
//     file. Log a warning when mtimes disagree (indicates a partial
//     write or a mixed-version install).
//  3. If only graph.json exists, fall back to JSON.
//  4. If neither exists, return a non-nil error.
func LoadGraphFromDir(dir string) (*Document, error) {
	fbPath := filepath.Join(dir, "graph.fb")
	jsonPath := filepath.Join(dir, "graph.json")

	fbInfo, fbErr := os.Stat(fbPath)
	jsonInfo, jsonErr := os.Stat(jsonPath)

	hasFB := fbErr == nil
	hasJSON := jsonErr == nil

	switch {
	case hasFB && hasJSON:
		// Both present — always prefer graph.fb (the canonical binary).
		//
		// #1626: we no longer log "mtime drift" here. graph.fb and
		// graph.json are two encodings of the SAME index pass and are now
		// stamped with an identical mtime by the indexer, so any residual
		// sub-second skew is meaningless. The old drift warning implied a
		// problem that didn't exist and was the symptom that surfaced the
		// in-repo reindex loop; treating fb as authoritative is sufficient.
		_ = fbInfo
		_ = jsonInfo
		return loadFBDocument(fbPath)

	case hasFB:
		return loadFBDocument(fbPath)

	case hasJSON:
		return loadJSONDocument(jsonPath)

	default:
		return nil, fmt.Errorf("graph.LoadGraphFromDir: neither graph.fb nor graph.json found in %s", dir)
	}
}

// PersistedStats is a cheap, on-disk view of a repo's index size and
// freshness, read from the graph.fb header WITHOUT materializing the
// entity/relationship vectors. Used by the dashboard group overview and
// `grafel status` as a fallback when the graph-stats.json sidecar is absent
// (e.g. a graph written by the daemon's incremental reindex path, which does
// not emit the sidecar) so a cold-but-indexed group reports its real counts
// instead of "0 entities / never indexed".
type PersistedStats struct {
	Entities      int
	Relationships int
	// ComputedAt is the index timestamp stamped into the graph.fb header
	// (the same value the sidecar's computed_at would carry). Zero when the
	// header carries no/unparseable timestamp.
	ComputedAt time.Time
	// IndexedRef is the git ref name (branch/tag) at index time, read
	// directly off the graph.fb header (#5729-W1 status plane). Empty for
	// legacy graphs written before Phase 0 git metadata (#2088) or a
	// detached HEAD / non-git repo.
	IndexedRef string
	// IndexedSHA is the abbreviated (short) HEAD commit hash at index time,
	// read directly off the graph.fb header. Empty under the same
	// conditions as IndexedRef.
	IndexedSHA string
}

// PersistedStatsFromDir reads cheap persisted stats from <dir>/graph.fb.
//
// It mmaps the file and reads the entity/relationship vector lengths and the
// header timestamp — it does NOT decode any entity or relationship — then
// closes the mapping immediately. This is the inexpensive way to learn a
// cold group's true size without paying for a full LoadGraphFromDir.
//
// Returns ok=false when graph.fb is absent or cannot be opened (in which case
// callers should treat the repo as genuinely never-indexed via graph.fb).
func PersistedStatsFromDir(dir string) (PersistedStats, bool) {
	fbPath := filepath.Join(dir, "graph.fb")
	r, err := fbreader.Open(fbPath)
	if err != nil {
		return PersistedStats{}, false
	}
	defer r.Close()
	ps := PersistedStats{
		Entities:      r.EntityCount(),
		Relationships: r.RelationshipCount(),
	}
	meta := r.LoadGraphMeta()
	if t, perr := time.Parse(time.RFC3339, meta.ComputedAt); perr == nil {
		ps.ComputedAt = t
	}
	ps.IndexedRef = meta.IndexedRef
	ps.IndexedSHA = meta.IndexedSHA
	return ps, true
}

// stringInterner canonicalizes repeated string VALUES seen during a single
// loadFBDocument call so that N occurrences of an equal string share ONE Go
// backing array in the resulting *Document, instead of each occurrence
// paying for its own independently-allocated copy.
//
// This is the RESIDENT-memory counterpart to fbwriter's CreateSharedString
// (Tier-1a, #5846): that optimization dedupes strings ON DISK, but the
// loader still calls string(fbBytes) once per field per record, which always
// allocates a fresh backing array regardless of on-disk sharing — so Tier-1a
// alone shrinks graph.fb but not the loaded Document's heap footprint. The
// interner closes that gap for the loader's own materialization.
//
// Only apply this to HIGH-DUPLICATION fields (entity id/kind/subtype/module/
// source_file/language and property keys; relationship from_id/to_id/kind).
// Do NOT intern genuinely-unique/high-cardinality fields (name,
// qualified_name, signature, property VALUES) — interning those would only
// grow the interner map with no dedup benefit.
//
// Concurrency: loadFBDocument builds every entity and relationship
// SEQUENTIALLY in a single goroutine (the for-loops below run in program
// order, no goroutines are spawned), so a plain, unsynchronized map is safe
// here. If this loop is ever parallelized, this type must be guarded (mutex
// or sharded) or replaced with a sync.Map to avoid a data race.
//
// Critically, ONE interner instance is shared across BOTH entity and
// relationship construction within a single load (see loadFBDocument below),
// so a relationship's from_id/to_id canonicalizes to the SAME backing array
// as the referenced entity's id — the biggest win, since on the real corpus
// each entity id is referenced by relationship endpoints ~8.7x on average
// (mean node degree).
type stringInterner struct {
	m map[string]string
}

func newStringInterner() *stringInterner {
	return &stringInterner{m: make(map[string]string)}
}

// intern returns the canonical copy of the string represented by b, sharing
// backing storage with any prior call that saw byte-identical content. The
// initial lookup uses the b-as-map-key form (m[string(b)]) directly as the
// index expression so the Go compiler's built-in optimization avoids
// allocating a string on the (common, high-duplication) hit path; only a
// miss pays for one allocation to create the canonical copy.
func (si *stringInterner) intern(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if v, ok := si.m[string(b)]; ok {
		return v
	}
	s := string(b)
	si.m[s] = s
	return s
}

// loadFBDocument decodes a graph.fb file into a *Document. It opens the
// file with fbreader (mmap + single bulk read), then converts the lazy
// FlatBuffers view into an in-memory *Document by iterating the entity
// and relationship vectors. This is O(N) but allocates far fewer
// intermediate objects than JSON unmarshal.
func loadFBDocument(path string) (*Document, error) {
	r, err := fbreader.Open(path)
	if err != nil {
		return nil, fmt.Errorf("graph.loadFBDocument: open %s: %w", path, err)
	}
	defer r.Close()

	// #2370 — refuse to read old-format graph.fb files. grafel is
	// pre-1.0; there is no on-disk compat path. The user is expected to
	// rerun `grafel index <repo>` to regenerate graph.fb against the
	// current schema.
	if v := r.Version(); v < minSupportedFBFormatVersion {
		return nil, fmt.Errorf("graph.loadFBDocument: %w",
			&FormatVersionError{Found: v, Required: minSupportedFBFormatVersion})
	}

	meta := r.LoadGraphMeta()
	generatedAt, _ := time.Parse(time.RFC3339, meta.ComputedAt)

	nEnts := r.EntityCount()
	nRels := r.RelationshipCount()

	// Shared across both entity and relationship construction below (see
	// stringInterner doc comment) so relationship endpoints canonicalize to
	// the same backing array as the entity ids they reference.
	si := newStringInterner()

	entities := make([]Entity, 0, nEnts)
	for i := 0; i < nEnts; i++ {
		fbEnt := r.EntityAt(i)
		if fbEnt == nil {
			continue
		}
		entities = append(entities, fbEntityToGraphEntity(fbEnt, si))
	}

	rels := make([]Relationship, 0, nRels)
	for i := 0; i < nRels; i++ {
		fbRel := r.RelationshipAt(i)
		if fbRel == nil {
			continue
		}
		rels = append(rels, fbRelToGraphRel(fbRel, si))
	}

	// Restore the aggregate Pass-4 community list + corpus stats (#1620).
	// These are empty/zero when the graph was written with the algo pass
	// skipped, so the resulting Document matches the JSON path exactly.
	nComms := r.CommunityCount()
	var communities []CommunityResult
	if nComms > 0 {
		communities = make([]CommunityResult, 0, nComms)
		for i := 0; i < nComms; i++ {
			fbComm := r.CommunityAt(i)
			if fbComm == nil {
				continue
			}
			communities = append(communities, fbCommunityToResult(fbComm))
		}
	}

	doc := &Document{
		// Preserve SchemaVersion (JSON schema = 1) rather than the FB
		// binary format version (2) so callers that check Version == 1
		// continue to work unchanged.
		Version:       SchemaVersion,
		GeneratedAt:   generatedAt,
		Repo:          meta.RepoTag,
		Entities:      entities,
		Relationships: rels,
		Communities:   communities,
		Stats: Stats{
			Entities:      len(entities),
			Relationships: len(rels),
		},
		// Phase 0 git metadata (#2088). Defaults to "" / false for graphs
		// written before these fields were added.
		IndexedRef: meta.IndexedRef,
		IndexedSHA: meta.IndexedSHA,
		IsWorktree: meta.IsWorktree,
		// M4 sparse-checkout (#2181). Defaults to "" for legacy graphs.
		CoverageStatus: meta.CoverageStatus,
	}

	// AlgorithmStats: only attach when the algo pass actually ran. We treat
	// "any communities present" as the signal, matching how the indexer only
	// emits AlgorithmStats when Pass 4 ran (index.go).
	if nComms > 0 {
		as := r.LoadAlgoStats()
		doc.AlgorithmStats = &AlgorithmStats{
			LouvainModularity:   as.LouvainModularity,
			NumCommunities:      nComms,
			NumGodNodes:         as.NumGodNodes,
			NumArticulationPts:  as.NumArticulationPts,
			NumSurpriseEdges:    as.NumSurpriseEdges,
			RuntimeMS:           as.RuntimeMS,
			DenoisedCommunities: as.DenoisedCommunities,
		}
	}
	return doc, nil
}

// loadJSONDocument is the JSON fallback. Identical to the old readDocument
// helpers that were scattered across the code-base.
func loadJSONDocument(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("graph.loadJSONDocument: open %s: %w", path, err)
	}
	defer f.Close()
	var doc Document
	if err := json.NewDecoder(f).Decode(&doc); err != nil {
		return nil, fmt.Errorf("graph.loadJSONDocument: decode %s: %w", path, err)
	}
	return &doc, nil
}

// fbEntityToGraphEntity converts one lazy FlatBuffers Entity view into
// the canonical graph.Entity struct. All string fields are copied out
// of the mmap'd bytes so the returned struct is safe to use after the
// Reader is closed.
//
// High-duplication fields (Id/Kind/Subtype/Module/SourceFile/Language and
// property keys) are canonicalized through si so repeated values across
// entities (and, for Id, across relationship endpoints referencing it) share
// one backing array. Name/QualifiedName/Signature/property VALUES are
// genuinely high-cardinality and are intentionally NOT interned.
func fbEntityToGraphEntity(e *fb.Entity, si *stringInterner) Entity {
	// #5850 Phase B: build the sorted []propKV slice directly from the FB
	// PropertyEntry vector instead of routing through an intermediate map.
	// fbwriter.buildPropertyVector writes entries in ascending key order
	// (sort.Strings(keys) before emission), so the FB vector is already
	// sorted — no re-sort needed here, just a straight copy.
	n := e.PropertiesLength()
	var props []propKV
	if n > 0 {
		props = make([]propKV, 0, n)
		var pe fb.PropertyEntry
		for i := 0; i < n; i++ {
			if e.Properties(&pe, i) {
				props = append(props, propKV{K: si.intern(pe.Key()), V: string(pe.Value())})
			}
		}
	}
	ent := Entity{
		ID:            si.intern(e.Id()),
		Name:          string(e.Name()),
		QualifiedName: string(e.QualifiedName()),
		Kind:          si.intern(e.Kind()),
		Subtype:       si.intern(e.Subtype()),
		SourceFile:    si.intern(e.SourceFile()),
		StartLine:     int(e.SourceLine()),
	}
	ent.properties = props
	// The Module field is stored as a top-level FB scalar by the writer
	// (see fbwriter.buildEntity). Restore it into Properties["module"]
	// so callers that read props["module"] continue to work. PropSet keeps
	// the slice sorted (binary-search insert), so this stays correct
	// regardless of where "module" falls alphabetically among the FB props.
	if mod := si.intern(e.Module()); mod != "" {
		ent.PropSet("module", mod)
	}
	// Issue #2370 — Language is read directly from the dedicated FB slot.
	// The PR #2365 property-tunnel restore (props["language"]) is retired.
	if lang := si.intern(e.Language()); lang != "" {
		ent.Language = lang
	}
	// Issue #4881 — restore the entity Signature from its dedicated FB slot.
	// Previously absent from the schema, so every entity loaded from graph.fb
	// had Signature="" — which made SCOPE.Schema field entities (whose
	// signature carries the field TYPE, e.g. "id: number") render with an
	// empty type in the dashboard shape API.
	if sig := string(e.Signature()); sig != "" {
		ent.Signature = sig
	}
	// Restore Pass 4 (graph-algorithm) attributes (#1620). community_id uses
	// a sentinel of -2 to mean "not computed"; only materialise the pointer
	// when the algo pass actually ran so an Entity loaded from a
	// graph-algo-skipped graph.fb stays byte-identical to the JSON path
	// (nil pointers, false flags).
	if cid := e.CommunityId(); cid != -2 {
		c := int(cid)
		ent.CommunityID = &c
	}
	if pr := e.Pagerank(); pr != 0 {
		p := pr
		ent.PageRank = &p
	}
	if cen := e.Centrality(); cen != 0 {
		c := cen
		ent.Centrality = &c
	}
	ent.IsGodNode = e.IsGodNode()
	ent.IsSurpriseEndpoint = e.IsSurpriseEndpoint()
	ent.IsArticulationPt = e.IsArticulationPoint()
	return ent
}

// fbCommunityToResult converts one lazy FlatBuffers Community view into a
// graph.CommunityResult, copying all strings out of the mmap'd bytes (#1620).
func fbCommunityToResult(c *fb.Community) CommunityResult {
	top := make([]string, 0, c.TopEntitiesLength())
	for i := 0; i < c.TopEntitiesLength(); i++ {
		top = append(top, string(c.TopEntities(i)))
	}
	return CommunityResult{
		ID:          int(c.Id()),
		Size:        int(c.Size()),
		Modularity:  c.Modularity(),
		TopEntities: top,
		AutoName:    string(c.AutoName()),
		AgentName:   string(c.AgentName()),
	}
}

// fbRelToGraphRel converts one lazy FlatBuffers Relationship view into
// the canonical graph.Relationship struct.
//
// FromID/ToID/Kind and property keys are canonicalized through si — the SAME
// interner instance used for entity ids (see loadFBDocument) — so an
// endpoint string shares backing storage with the entity.ID it references
// rather than allocating its own copy.
func fbRelToGraphRel(r *fb.Relationship, si *stringInterner) Relationship {
	// #5850 Phase B: same direct-to-[]propKV construction as
	// fbEntityToGraphEntity above — the FB vector is already key-sorted.
	n := r.PropertiesLength()
	var props []propKV
	if n > 0 {
		props = make([]propKV, 0, n)
		var pe fb.PropertyEntry
		for i := 0; i < n; i++ {
			if r.Properties(&pe, i) {
				props = append(props, propKV{K: si.intern(pe.Key()), V: string(pe.Value())})
			}
		}
	}
	rel := Relationship{
		FromID: si.intern(r.FromId()),
		ToID:   si.intern(r.ToId()),
		Kind:   si.intern(r.Kind()),
	}
	rel.properties = props
	// Restore the ID from Properties if the writer stored it.
	if id, ok := rel.PropLookup("id"); ok {
		rel.ID = id
	}
	return rel
}

// MaterializeEntity decodes the i-th entity of the mmap'd Reader into a
// single heap-safe graph.Entity, on demand. It is a thin exported wrapper
// over the same fbEntityToGraphEntity conversion loadFBDocument uses per
// row, so a MaterializeEntity(r, i) result is byte-identical to the
// Document's Entities[i] for the same graph.fb.
//
// Every string field is COPIED out of the mmap region (see
// fbEntityToGraphEntity), so the returned Entity is safe to retain after
// the Reader is closed — no borrow of the underlying mapping is held.
//
// A fresh interner is used per call: interning only deduplicates repeated
// substrings WITHIN one materialized row, and the copied-out strings are
// independent of any Document-wide interner. Returns the zero Entity when
// r is nil or i is out of range.
func MaterializeEntity(r *fbreader.Reader, i int) Entity {
	if r == nil {
		return Entity{}
	}
	fbEnt := r.EntityAt(i)
	if fbEnt == nil {
		return Entity{}
	}
	return fbEntityToGraphEntity(fbEnt, newStringInterner())
}

// MaterializeRelationship decodes the i-th relationship of the mmap'd
// Reader into a single heap-safe graph.Relationship, on demand. Thin
// exported wrapper over fbRelToGraphRel — byte-identical to the Document's
// Relationships[i] for the same graph.fb. All strings are copied out of
// the mmap region, so the result outlives the Reader. Returns the zero
// Relationship when r is nil or i is out of range.
func MaterializeRelationship(r *fbreader.Reader, i int) Relationship {
	if r == nil {
		return Relationship{}
	}
	fbRel := r.RelationshipAt(i)
	if fbRel == nil {
		return Relationship{}
	}
	return fbRelToGraphRel(fbRel, newStringInterner())
}
