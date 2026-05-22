// Package fbwriter serializes the in-memory graph.Document into the
// archigraph v2 FlatBuffers on-disk format described in
// internal/graph/schema/graph.fbs and ADR-0016.
//
// This is a phase-1 prototype: callers continue to dual-write graph.json
// via graph.WriteAtomic; fbwriter is invoked behind the --export-fb flag.
package fbwriter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cajasmota/archigraph/internal/graph"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
)

// FormatVersion is the FlatBuffers schema version this writer emits.
// Matches the default of Graph.version in graph.fbs.
const FormatVersion = 2

// WriteAtomic serializes doc to a FlatBuffers buffer and writes it to
// outPath atomically via a sibling .tmp + rename. The on-disk file is
// the canonical archigraph v2 binary graph.
func WriteAtomic(outPath string, doc *graph.Document) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("fbwriter: mkdir %s: %w", filepath.Dir(outPath), err)
	}
	buf, err := Marshal(doc)
	if err != nil {
		return fmt.Errorf("fbwriter: marshal: %w", err)
	}
	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("fbwriter: write tmp: %w", err)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("fbwriter: rename: %w", err)
	}
	return nil
}

// Marshal serializes doc into a FlatBuffers byte slice. Exported so the
// indexer and tests can drive it without touching the filesystem.
//
// Entity property maps are flattened into key-sorted PropertyEntry
// vectors so the on-disk bytes are deterministic across runs (issue
// #481 — bytewise stability).
func Marshal(doc *graph.Document) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("nil document")
	}
	b := flatbuffers.NewBuilder(1 << 20)

	// Build all entities.
	entityOffsets := make([]flatbuffers.UOffsetT, 0, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		off := buildEntity(b, e)
		entityOffsets = append(entityOffsets, off)
	}
	// EntitiesByKey relies on a sorted-by-key vector. The id (string key)
	// is already canonical in graph.json emission order (#481), so we
	// preserve insertion order. Callers that need sorted output should
	// run sortDocumentForEmission before invoking Marshal.
	fb.GraphStartEntitiesVector(b, len(entityOffsets))
	for i := len(entityOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(entityOffsets[i])
	}
	entitiesVec := b.EndVector(len(entityOffsets))

	// Build all relationships.
	relOffsets := make([]flatbuffers.UOffsetT, 0, len(doc.Relationships))
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		relOffsets = append(relOffsets, buildRelationship(b, r))
	}
	fb.GraphStartRelationshipsVector(b, len(relOffsets))
	for i := len(relOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(relOffsets[i])
	}
	relsVec := b.EndVector(len(relOffsets))

	// Build the aggregate community list (#1620). Empty when the algo pass
	// did not run; the resulting vector is then a zero-length vector which
	// the reader treats as "no communities".
	commOffsets := make([]flatbuffers.UOffsetT, 0, len(doc.Communities))
	for i := range doc.Communities {
		commOffsets = append(commOffsets, buildCommunity(b, &doc.Communities[i]))
	}
	fb.GraphStartCommunitiesVector(b, len(commOffsets))
	for i := len(commOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(commOffsets[i])
	}
	commsVec := b.EndVector(len(commOffsets))

	computedAt := b.CreateString(doc.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"))
	repoTag := b.CreateString(doc.Repo)

	fb.GraphStart(b)
	fb.GraphAddVersion(b, int32(FormatVersion))
	fb.GraphAddComputedAt(b, computedAt)
	fb.GraphAddRepoTag(b, repoTag)
	fb.GraphAddEntities(b, entitiesVec)
	fb.GraphAddRelationships(b, relsVec)
	fb.GraphAddCommunities(b, commsVec)
	if doc.AlgorithmStats != nil {
		st := doc.AlgorithmStats
		fb.GraphAddLouvainModularity(b, st.LouvainModularity)
		fb.GraphAddNumGodNodes(b, int32(st.NumGodNodes))
		fb.GraphAddNumArticulationPoints(b, int32(st.NumArticulationPts))
		fb.GraphAddNumSurpriseEdges(b, int32(st.NumSurpriseEdges))
		fb.GraphAddAlgoRuntimeMs(b, st.RuntimeMS)
		fb.GraphAddDenoisedCommunities(b, int32(st.DenoisedCommunities))
	}
	root := fb.GraphEnd(b)
	fb.FinishGraphBuffer(b, root)
	return b.FinishedBytes(), nil
}

// buildEntity serializes a single entity table and returns its offset.
// Strings and the properties vector are built first (FlatBuffers requires
// child offsets be created before opening the parent table).
func buildEntity(b *flatbuffers.Builder, e *graph.Entity) flatbuffers.UOffsetT {
	idOff := b.CreateString(e.ID)
	qnOff := b.CreateString(e.QualifiedName)
	kindOff := b.CreateString(e.Kind)
	subOff := b.CreateString(e.Subtype)
	moduleOff := flatbuffers.UOffsetT(0)
	if mod, ok := e.Properties["module"]; ok {
		moduleOff = b.CreateString(mod)
	} else {
		moduleOff = b.CreateString("")
	}
	nameOff := b.CreateString(e.Name)
	srcOff := b.CreateString(e.SourceFile)

	propsVec := buildPropertyVector(b, e.Properties)

	fb.EntityStart(b)
	fb.EntityAddId(b, idOff)
	fb.EntityAddQualifiedName(b, qnOff)
	fb.EntityAddKind(b, kindOff)
	fb.EntityAddSubtype(b, subOff)
	fb.EntityAddModule(b, moduleOff)
	fb.EntityAddName(b, nameOff)
	fb.EntityAddSourceFile(b, srcOff)
	fb.EntityAddSourceLine(b, int32(e.StartLine))
	fb.EntityAddSourceCol(b, 0)
	fb.EntityAddProperties(b, propsVec)

	// Pass 4 (graph-algorithm) attributes (#1620). Pointers + sentinel so a
	// document written with --skip-pass=graph-algo round-trips with the
	// fields absent (community_id defaults to -2 = "not computed"). Only
	// emit each scalar when it carries real data so old-vs-new on-disk bytes
	// stay identical when the algo pass did not run.
	if e.CommunityID != nil {
		fb.EntityAddCommunityId(b, int32(*e.CommunityID))
	}
	if e.PageRank != nil {
		fb.EntityAddPagerank(b, *e.PageRank)
	}
	if e.Centrality != nil {
		fb.EntityAddCentrality(b, *e.Centrality)
	}
	if e.IsGodNode {
		fb.EntityAddIsGodNode(b, true)
	}
	if e.IsSurpriseEndpoint {
		fb.EntityAddIsSurpriseEndpoint(b, true)
	}
	if e.IsArticulationPt {
		fb.EntityAddIsArticulationPoint(b, true)
	}
	return fb.EntityEnd(b)
}

func buildRelationship(b *flatbuffers.Builder, r *graph.Relationship) flatbuffers.UOffsetT {
	fromOff := b.CreateString(r.FromID)
	toOff := b.CreateString(r.ToID)
	kindOff := b.CreateString(r.Kind)
	propsVec := buildPropertyVector(b, r.Properties)
	fb.RelationshipStart(b)
	fb.RelationshipAddFromId(b, fromOff)
	fb.RelationshipAddToId(b, toOff)
	fb.RelationshipAddKind(b, kindOff)
	fb.RelationshipAddProperties(b, propsVec)
	return fb.RelationshipEnd(b)
}

// buildCommunity serializes a single graph.CommunityResult into a
// Community table (#1620). top_entities are written in their existing
// (already-deterministic) order; strings are created before the table is
// opened per FlatBuffers ordering rules.
func buildCommunity(b *flatbuffers.Builder, c *graph.CommunityResult) flatbuffers.UOffsetT {
	autoOff := b.CreateString(c.AutoName)
	agentOff := b.CreateString(c.AgentName)

	topOffsets := make([]flatbuffers.UOffsetT, 0, len(c.TopEntities))
	for _, t := range c.TopEntities {
		topOffsets = append(topOffsets, b.CreateString(t))
	}
	fb.CommunityStartTopEntitiesVector(b, len(topOffsets))
	for i := len(topOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(topOffsets[i])
	}
	topVec := b.EndVector(len(topOffsets))

	fb.CommunityStart(b)
	fb.CommunityAddId(b, int32(c.ID))
	fb.CommunityAddSize(b, int32(c.Size))
	fb.CommunityAddModularity(b, c.Modularity)
	fb.CommunityAddTopEntities(b, topVec)
	fb.CommunityAddAutoName(b, autoOff)
	fb.CommunityAddAgentName(b, agentOff)
	return fb.CommunityEnd(b)
}

func buildPropertyVector(b *flatbuffers.Builder, props map[string]string) flatbuffers.UOffsetT {
	if len(props) == 0 {
		fb.EntityStartPropertiesVector(b, 0)
		return b.EndVector(0)
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entryOffsets := make([]flatbuffers.UOffsetT, 0, len(keys))
	for _, k := range keys {
		kOff := b.CreateString(k)
		vOff := b.CreateString(props[k])
		fb.PropertyEntryStart(b)
		fb.PropertyEntryAddKey(b, kOff)
		fb.PropertyEntryAddValue(b, vOff)
		entryOffsets = append(entryOffsets, fb.PropertyEntryEnd(b))
	}
	fb.EntityStartPropertiesVector(b, len(entryOffsets))
	for i := len(entryOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(entryOffsets[i])
	}
	return b.EndVector(len(entryOffsets))
}
