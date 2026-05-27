// find_default_repo_2643_test.go — tests for #2643: archigraph_find defaults
// to the cwd-resolved repo rather than searching all repos in the group.
package mcp

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// newTestServerWithPaths is like newTestServer but returns the per-repo
// registry paths so tests can pass them as cwd arguments to trigger
// cwd-based repo resolution (required for #2643 tests).
//
// repoPaths[i] is the registered Path for docs[i].
func newTestServerWithPaths(t *testing.T, docs ...*graph.Document) (*Server, []string) {
	t.Helper()

	paths := make([]string, len(docs))
	for i := range docs {
		paths[i] = t.TempDir()
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{}},
	}}

	type namedDoc struct {
		name string
		doc  *graph.Document
		path string
	}
	named := make([]namedDoc, 0, len(docs))
	for i, doc := range docs {
		name := doc.Repo
		if name == "" {
			name = "repo" + string(rune('1'+i))
		}
		named = append(named, namedDoc{name, doc, paths[i]})
		reg.Groups["test"].Repos[name] = RegistryRepo{Path: paths[i]}
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
			StepAdj:    buildStepAdjacency(doc),
			ByID:       byID,
		}
	}
	st.groups["test"] = lg
	st.mu.Unlock()

	srv := &Server{State: st, Tel: NewTelemetry(0)}
	return srv, paths
}

// buildFindDefaultRepoFixture returns two docs with distinct, non-overlapping entities.
// repo A ("mobile") contains mobile-only symbols.
// repo B ("backend") contains backend-only symbols.
func buildFindDefaultRepoFixture() (*graph.Document, *graph.Document) {
	mobile := &graph.Document{
		Repo: "mobile",
		Entities: []graph.Entity{
			{ID: "fn_sync_push", Name: "syncPushNotification",
				Kind: "SCOPE.Function", SourceFile: "src/push.ts", StartLine: 10},
			{ID: "fn_device_register", Name: "deviceRegister",
				Kind: "SCOPE.Function", SourceFile: "src/device.ts", StartLine: 5},
		},
	}
	backend := &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "fn_api_handler", Name: "apiInspectionHandler",
				Kind: "SCOPE.Function", SourceFile: "api/views.py", StartLine: 42},
			{ID: "fn_db_query", Name: "dbScheduleQuery",
				Kind: "SCOPE.Function", SourceFile: "db/queries.py", StartLine: 7},
		},
	}
	return mobile, backend
}

// TestFind_DefaultsCurrentRepoWhenCwdResolvable verifies that when no
// repo_filter or cross_repo flag is supplied, find scopes to the
// cwd-resolved repo only. Query matches entities in both repos but only
// mobile entities should be returned when cwd is inside the mobile repo.
func TestFind_DefaultsCurrentRepoWhenCwdResolvable(t *testing.T) {
	mobile, backend := buildFindDefaultRepoFixture()
	srv, paths := newTestServerWithPaths(t, mobile, backend)
	mobilePath := paths[0] // cwd will be inside the mobile repo

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group": "test",
		"query": "sync device api inspection db schedule",
		"cwd":   mobilePath,
		"full":  true,
	})

	if res == "" {
		t.Fatal("expected non-empty result")
	}

	// Mobile entities must appear.
	if !strings.Contains(res, "syncPushNotification") && !strings.Contains(res, "deviceRegister") {
		t.Errorf("expected at least one mobile entity; got:\n%s", res)
	}

	// Backend entities must NOT appear (cwd resolved to mobile repo only).
	if strings.Contains(res, "apiInspectionHandler") || strings.Contains(res, "dbScheduleQuery") {
		t.Errorf("cross-repo bleed: backend entities leaked into mobile-scoped find; got:\n%s", res)
	}
}

// TestFind_CrossRepoTrueSpansAll verifies that cross_repo=true returns hits
// from both repos even when cwd is inside one specific repo.
func TestFind_CrossRepoTrueSpansAll(t *testing.T) {
	mobile, backend := buildFindDefaultRepoFixture()
	srv, paths := newTestServerWithPaths(t, mobile, backend)
	mobilePath := paths[0]

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":      "test",
		"query":      "sync device api inspection",
		"cwd":        mobilePath,
		"cross_repo": true,
		"full":       true,
		"min_score":  0.0, // defeat min_score to ensure both repos' hits appear
	})

	if res == "" {
		t.Fatal("expected non-empty result with cross_repo=true")
	}

	// Expect at least one entity from each repo.
	hasMobile := strings.Contains(res, "syncPushNotification") || strings.Contains(res, "deviceRegister")
	hasBackend := strings.Contains(res, "apiInspectionHandler") || strings.Contains(res, "dbScheduleQuery")

	if !hasMobile {
		t.Errorf("cross_repo=true: expected mobile entities; got:\n%s", res)
	}
	if !hasBackend {
		t.Errorf("cross_repo=true: expected backend entities; got:\n%s", res)
	}
}

// TestFind_RepoFilterTakesPriority verifies that when repo_filter is set
// alongside cross_repo=true, repo_filter wins (explicit always beats opt-in).
func TestFind_RepoFilterTakesPriority(t *testing.T) {
	mobile, backend := buildFindDefaultRepoFixture()
	srv, paths := newTestServerWithPaths(t, mobile, backend)
	mobilePath := paths[0]

	// repo_filter=["backend"] + cross_repo=true → only backend results.
	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":       "test",
		"query":       "sync device api inspection schedule",
		"cwd":         mobilePath,
		"repo_filter": []any{"backend"},
		"cross_repo":  true,
		"full":        true,
		"min_score":   0.0,
	})

	if res == "" {
		t.Fatal("expected non-empty result")
	}

	// Backend entities must appear.
	if !strings.Contains(res, "apiInspectionHandler") && !strings.Contains(res, "dbScheduleQuery") {
		t.Errorf("repo_filter=[backend]: expected backend entities; got:\n%s", res)
	}

	// Mobile entities must NOT appear (repo_filter wins).
	if strings.Contains(res, "syncPushNotification") || strings.Contains(res, "deviceRegister") {
		t.Errorf("repo_filter=[backend]: mobile entities leaked; got:\n%s", res)
	}
}

// TestFind_NoResolvableCwdFallsBack verifies that when cwd does not resolve
// to any registered repo and no repo_filter/cross_repo is set, find searches
// all repos (graceful fallback).
func TestFind_NoResolvableCwdFallsBack(t *testing.T) {
	mobile, backend := buildFindDefaultRepoFixture()
	// Use newTestServer (not newTestServerWithPaths) so cwd "/" won't match any
	// registered repo path and resolution returns CWDResolution{Source:"none"}.
	srv := newTestServer(t, mobile, backend)

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":     "test",
		"query":     "sync device api inspection schedule",
		"cwd":       "/", // root — cannot match any t.TempDir() path
		"full":      true,
		"min_score": 0.0,
	})

	if res == "" {
		t.Fatal("expected non-empty result when cwd is unresolvable (fallback to all repos)")
	}

	// At least one entity from each repo must appear, since we search all.
	hasMobile := strings.Contains(res, "syncPushNotification") || strings.Contains(res, "deviceRegister")
	hasBackend := strings.Contains(res, "apiInspectionHandler") || strings.Contains(res, "dbScheduleQuery")

	if !hasMobile || !hasBackend {
		t.Errorf("unresolvable-cwd fallback: expected entities from both repos; mobile=%v backend=%v\nout:\n%s",
			hasMobile, hasBackend, res)
	}
}
