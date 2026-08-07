package mcp

// testhelpers_test.go — shared server-construction helpers for internal/mcp tests.
//
// Introduced in #2306 to unify the near-duplicate newTestServerWithDoc
// (dashboard_tools_test.go) and newTestServerWithDocs (ux_1650_test.go).
// The two originals had subtly different Registry plumbing; this single
// variadic helper handles both the single-doc and multi-doc cases.

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// sandboxGrafelHome points every grafel-home derivation at a fresh temp dir
// and returns it, so a test that writes a sidecar can be sure the tool reads
// THAT one — and a test that writes none can be sure the tool reads nothing.
//
// t.Setenv("HOME", …) alone does neither on Windows, which is what failed the
// four internal/mcp tests in run 31132076151. The grafel-home derivation
// bottoms out in registry.HomeDir() → os.UserHomeDir(), and os.UserHomeDir()
// reads %USERPROFILE% on Windows and ignores HOME (#6178). A HOME-only sandbox
// there resolves to the runner's REAL profile, so the sidecar the test wrote
// under HOME was invisible and the tool fell back to the entity-property
// effects — the exact "v3 resolved=false, oracle resolved=true" the stub
// detector reported. For the isolation-only callers the same gap points the
// tool at whatever happens to sit in the developer's real ~/.grafel.
//
// Both vars are set on every platform: the one that is not consulted is inert,
// and a per-GOOS switch here would be a second thing to keep correct.
//
// GRAFEL_HOME is set too, and it is not redundant — registry.HomeDir() checks
// it FIRST and returns it verbatim, so a developer who exports it (plausible in
// this repo) would otherwise have every one of these tests reading and writing
// their real grafel home no matter what HOME said. CI does not set it, so this
// is a developer-machine hazard rather than a CI one. The value mirrors what
// the HOME path resolves to, so the sidecar writers' <home>/.grafel/groups join
// and the readers' registry.HomeDir() land on the same directory either way.
func sandboxGrafelHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
	return home
}

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
		// Derived indexes (Adjacency/CallsAdj/StepAdj/ByID/TopKPageRank) are
		// built lazily on first use by the getters (#3367) — only Doc + the
		// eager LabelIndex/BM25 need to be set here.
		lg.Repos[nd.name] = &LoadedRepo{
			Repo:       nd.name,
			Doc:        doc,
			LabelIndex: BuildLabelIndex(doc),
			BM25:       BuildBM25(doc),
		}
	}
	st.groups["test"] = lg
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}
