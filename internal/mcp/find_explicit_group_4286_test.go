// find_explicit_group_4286_test.go — tests for #4286: archigraph_find honors an
// explicit `group=` even when the caller's cwd is inside a repo of a DIFFERENT
// group. Before the fix, find pinned the cwd-resolved repo slug as a repo_filter
// regardless of which group it belonged to; since that slug does not exist in
// the explicitly-requested group's loaded repo set, reposToConsider returned
// zero repos and find emitted "# no repos loaded for this group" — while the
// other group-scoped tools (search_entities, etc.) served the same group fine.
package mcp

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// newTestServerTwoGroups builds a Server whose registry+state contain two
// distinct groups, each with one repo backed by a real on-disk path (t.TempDir)
// so cwd resolution can match group A's repo path. Returns the server and the
// on-disk path of group A's repo (to be used as cwd).
func newTestServerTwoGroups(t *testing.T, groupA, repoA string, docA *graph.Document, groupB, repoB string, docB *graph.Document) (*Server, string) {
	t.Helper()

	pathA := t.TempDir()
	pathB := t.TempDir()

	reg := &Registry{Groups: map[string]RegistryGroup{
		groupA: {Repos: map[string]RegistryRepo{repoA: {Path: pathA}}},
		groupB: {Repos: map[string]RegistryRepo{repoB: {Path: pathB}}},
	}}

	st := NewState(reg)
	st.mu.Lock()
	lgA := &LoadedGroup{Name: groupA, Repos: map[string]*LoadedRepo{
		repoA: {Repo: repoA, Doc: docA, LabelIndex: BuildLabelIndex(docA), BM25: BuildBM25(docA)},
	}}
	lgB := &LoadedGroup{Name: groupB, Repos: map[string]*LoadedRepo{
		repoB: {Repo: repoB, Doc: docB, LabelIndex: BuildLabelIndex(docB), BM25: BuildBM25(docB)},
	}}
	st.groups[groupA] = lgA
	st.groups[groupB] = lgB
	st.mu.Unlock()

	return &Server{State: st, Tel: NewTelemetry(0)}, pathA
}

// TestFind_ExplicitGroupFromOutsideCwd reproduces #4286: cwd is inside group A's
// repo, but the request passes group=B (and leaves cross_repo unset). find must
// resolve group B and search its repos rather than bailing with "no repos
// loaded for this group".
func TestFind_ExplicitGroupFromOutsideCwd(t *testing.T) {
	docA := &graph.Document{
		Repo: "repoA",
		Entities: []graph.Entity{
			{ID: "fn_a", Name: "alphaWidgetBuilder", Kind: "SCOPE.Function", SourceFile: "a/alpha.go", StartLine: 3},
		},
	}
	docB := &graph.Document{
		Repo: "repoB",
		Entities: []graph.Entity{
			{ID: "fn_b", Name: "betaWidgetBuilder", Kind: "SCOPE.Function", SourceFile: "b/beta.go", StartLine: 9},
		},
	}

	srv, cwdInA := newTestServerTwoGroups(t, "groupA", "repoA", docA, "groupB", "repoB", docB)

	// cwd is inside groupA's repo, but we explicitly ask for groupB. cross_repo
	// is intentionally left unset — that is the broken path.
	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":     "groupB",
		"query":     "widget builder beta",
		"cwd":       cwdInA,
		"full":      true,
		"min_score": 0.0,
	})

	if strings.Contains(res, "no repos loaded") {
		t.Fatalf("#4286 regression: explicit group=groupB from a cwd inside groupA returned 'no repos loaded'; got:\n%s", res)
	}
	if !strings.Contains(res, "betaWidgetBuilder") {
		t.Errorf("expected groupB entity betaWidgetBuilder in results; got:\n%s", res)
	}
	// groupA's entity must NOT appear — we searched groupB only.
	if strings.Contains(res, "alphaWidgetBuilder") {
		t.Errorf("group isolation broken: groupA entity leaked into groupB find; got:\n%s", res)
	}
}

// TestFind_ExplicitGroupMatchingCwdStillScopesToRepo guards the preserved
// behavior: when the explicit group MATCHES the cwd's group, find still pins to
// the cwd-resolved repo (the #2643 default), not all repos.
func TestFind_ExplicitGroupMatchingCwdStillScopesToRepo(t *testing.T) {
	docA := &graph.Document{
		Repo: "repoA",
		Entities: []graph.Entity{
			{ID: "fn_a", Name: "alphaWidgetBuilder", Kind: "SCOPE.Function", SourceFile: "a/alpha.go", StartLine: 3},
		},
	}
	docB := &graph.Document{
		Repo: "repoB",
		Entities: []graph.Entity{
			{ID: "fn_b", Name: "betaWidgetBuilder", Kind: "SCOPE.Function", SourceFile: "b/beta.go", StartLine: 9},
		},
	}

	srv, cwdInA := newTestServerTwoGroups(t, "groupA", "repoA", docA, "groupB", "repoB", docB)

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":     "groupA",
		"query":     "widget builder alpha",
		"cwd":       cwdInA,
		"full":      true,
		"min_score": 0.0,
	})

	if !strings.Contains(res, "alphaWidgetBuilder") {
		t.Errorf("expected groupA entity alphaWidgetBuilder; got:\n%s", res)
	}
}
