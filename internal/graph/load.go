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
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
)

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
		// Both present — prefer the more-recent file and warn on drift.
		fbMT := fbInfo.ModTime()
		jsonMT := jsonInfo.ModTime()
		threshold := time.Second // mtime granularity on some filesystems
		if fbMT.After(jsonMT.Add(threshold)) || jsonMT.After(fbMT.Add(threshold)) {
			log.Printf("archigraph: mtime drift in %s: graph.fb=%s graph.json=%s — preferring graph.fb",
				dir, fbMT.Format(time.RFC3339), jsonMT.Format(time.RFC3339))
		}
		// Always prefer graph.fb when both exist (it is the new canonical).
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

	doc := &Document{
		// Preserve SchemaVersion (JSON schema = 1) rather than the FB
		// binary format version (2) so callers that check Version == 1
		// continue to work unchanged.
		Version:       SchemaVersion,
		GeneratedAt:   generatedAt,
		Repo:          meta.RepoTag,
		Entities:      entities,
		Relationships: rels,
		Stats: Stats{
			Entities:      len(entities),
			Relationships: len(rels),
		},
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
	// Restore Language from Properties if the indexer stored it there.
	if lang, ok := props["language"]; ok {
		ent.Language = lang
	}
	return ent
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
