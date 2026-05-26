package mcp

// testhelpers_test.go — shared server-construction helpers for internal/mcp tests.
//
// Introduced in #2306 to unify the near-duplicate newTestServerWithDoc
// (dashboard_tools_test.go) and newTestServerWithDocs (ux_1650_test.go).
// The two originals had subtly different Registry plumbing; this single
// variadic helper handles both the single-doc and multi-doc cases.

import (
	"fmt"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// newTestServer builds a minimal Server with one group ("test") loaded from
// the supplied documents.
//
// Repo naming: each doc is keyed by its doc.Repo field.  When doc.Repo is
// empty the repo is auto-named "repo1", "repo2", … in the order the docs are
// provided.  Callers that need a specific repo name (e.g. cross-repo tests
// that check prefixed IDs) must set doc.Repo before calling this helper.
//
// Single-doc shorthand:
//
//	srv := newTestServer(t, doc)   // repo name = doc.Repo or "repo1"
//
// Multi-doc (cross-repo tests):
//
//	frontend.Repo = "frontend"
//	backend.Repo  = "backend"
//	srv := newTestServer(t, frontend, backend)
func newTestServer(t *testing.T, docs ...*graph.Document) *Server {
	t.Helper()

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{}},
	}}

	// Assign repo names: prefer doc.Repo; fall back to "repo<N>".
	type namedDoc struct {
		name string
		doc  *graph.Document
	}
	named := make([]namedDoc, 0, len(docs))
	for i, doc := range docs {
		name := doc.Repo
		if name == "" {
			name = fmt.Sprintf("repo%d", i+1)
		}
		named = append(named, namedDoc{name, doc})
		reg.Groups["test"].Repos[name] = RegistryRepo{Path: t.TempDir()}
	}

	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{}}
	for _, nd := range named {
		doc := nd.doc
		byID := make(map[string]*graph.Entity, len(doc.Entities))
		for i := range doc.Entities {
			byID[doc.Entities[i].ID] = &doc.Entities[i]
		}
		lg.Repos[nd.name] = &LoadedRepo{
			Repo:       nd.name,
			Doc:        doc,
			LabelIndex: BuildLabelIndex(doc),
			BM25:       BuildBM25(doc),
			Adjacency:  buildAdjacency(doc, nd.name),
			CallsAdj:   buildCallsAdjacency(doc),
			ByID:       byID,
		}
	}
	st.groups["test"] = lg
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}
