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

	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
)

// minSupportedFBFormatVersion is the lowest graph.fb FormatVersion the loader
// will accept. It must stay in sync with fbwriter.FormatVersion; we keep a
// local copy here to avoid an import cycle (fbwriter imports the graph
// package). Bumped for #2370 — Entity now carries `language` directly; old
// graph.fb files lack the slot and must be reindexed.
const minSupportedFBFormatVersion = 3

// LoadGraphFromDir loads a graph.Document from dir, where dir is the
// .archigraph state directory for a repo (the directory that contains
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

	// #2370 — refuse to read old-format graph.fb files. archigraph is
	// pre-1.0; there is no on-disk compat path. The user is expected to
	// rerun `archigraph index <repo>` to regenerate graph.fb against the
	// current schema.
	if v := r.Version(); v < minSupportedFBFormatVersion {
		return nil, fmt.Errorf(
			"graph.loadFBDocument: graph.fb format version %d is older than required version %d — please reindex (run: archigraph index <repo>)",
			v, minSupportedFBFormatVersion,
		)
	}

	meta := r.LoadGraphMeta()
	generatedAt, _ := time.Parse(time.RFC3339, meta.ComputedAt)

	nEnts := r.EntityCount()
	nRels := r.RelationshipCount()

	entities := make([]Entity, 0, nEnts)
	for i := 0; i < nEnts; i++ {
		fbEnt := r.EntityAt(i)
		if fbEnt == nil {
			continue
		}
		entities = append(entities, fbEntityToGraphEntity(fbEnt))
	}

	rels := make([]Relationship, 0, nRels)
	for i := 0; i < nRels; i++ {
		fbRel := r.RelationshipAt(i)
		if fbRel == nil {
			continue
		}
		rels = append(rels, fbRelToGraphRel(fbRel))
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
func fbEntityToGraphEntity(e *fb.Entity) Entity {
	props := make(map[string]string, e.PropertiesLength())
	var pe fb.PropertyEntry
	for i := 0; i < e.PropertiesLength(); i++ {
		if e.Properties(&pe, i) {
			props[string(pe.Key())] = string(pe.Value())
		}
	}
	ent := Entity{
		ID:            string(e.Id()),
		Name:          string(e.Name()),
		QualifiedName: string(e.QualifiedName()),
		Kind:          string(e.Kind()),
		Subtype:       string(e.Subtype()),
		SourceFile:    string(e.SourceFile()),
		StartLine:     int(e.SourceLine()),
		Properties:    props,
	}
	// The Module field is stored as a top-level FB scalar by the writer
	// (see fbwriter.buildEntity). Restore it into Properties["module"]
	// so callers that read props["module"] continue to work.
	if mod := string(e.Module()); mod != "" {
		if props == nil {
			props = map[string]string{}
		}
		props["module"] = mod
		ent.Properties = props
	}
	// Issue #2370 — Language is read directly from the dedicated FB slot.
	// The PR #2365 property-tunnel restore (props["language"]) is retired.
	if lang := string(e.Language()); lang != "" {
		ent.Language = lang
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
func fbRelToGraphRel(r *fb.Relationship) Relationship {
	props := make(map[string]string, r.PropertiesLength())
	var pe fb.PropertyEntry
	for i := 0; i < r.PropertiesLength(); i++ {
		if r.Properties(&pe, i) {
			props[string(pe.Key())] = string(pe.Value())
		}
	}
	rel := Relationship{
		FromID:     string(r.FromId()),
		ToID:       string(r.ToId()),
		Kind:       string(r.Kind()),
		Properties: props,
	}
	// Restore the ID from Properties if the writer stored it.
	if id, ok := props["id"]; ok {
		rel.ID = id
	}
	return rel
}
