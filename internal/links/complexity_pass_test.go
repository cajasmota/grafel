package links

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// runComplexityForTest runs the universal complexity pass over a single in-memory
// repo fixture (no sidecar — the pass stamps Properties in place) and returns the
// mutated graph for assertion.
func runComplexityForTest(t *testing.T, file, content string, entities []entityNode) repoGraph {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, file, content)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: entities,
	}}
	if _, err := runComplexityPass(graphs, Paths{}); err != nil {
		t.Fatalf("pass error: %v", err)
	}
	return graphs[0]
}

// TestComplexityPass_NonHandlerHelperGetsStamped is the core #4831 guarantee:
// a plain helper function (NOT a data-flow-bound handler — no effects, no flows)
// with known branches still receives cyclomatic_complexity / branch_count via
// the universal pass. The body has 3 decision points (if, &&, for) → cyclomatic
// 4, branch_count 3.
func TestComplexityPass_NonHandlerHelperGetsStamped(t *testing.T) {
	src := `package util

func computeScore(a, b int) int { // line 3
	total := 0
	if a > 0 && b > 0 {
		for i := 0; i < a; i++ {
			total += i
		}
	}
	return total
}
`
	g := runComplexityForTest(t, "util/score.go", src, []entityNode{{
		ID:         "fn:computeScore",
		Name:       "computeScore",
		Kind:       "function",
		SourceFile: "util/score.go",
		StartLine:  3,
		EndLine:    11,
	}})

	e := g.Entities[0]
	if got := e.Properties.Get(ComplexityPropertyKeyCyclomatic); got != "4" {
		t.Fatalf("cyclomatic_complexity = %q, want 4", got)
	}
	if got := e.Properties.Get(ComplexityPropertyKeyBranchCount); got != "3" {
		t.Fatalf("branch_count = %q, want 3", got)
	}
}

// TestComplexityPass_Idempotent verifies the pass never clobbers a value a prior
// path already stamped (the data-flow handler stamp), keeping a single source of
// truth and preventing double-counting.
func TestComplexityPass_Idempotent(t *testing.T) {
	src := "func f() { if x { return } }\n"
	pre := types.PropsFromMap(map[string]string{
		ComplexityPropertyKeyCyclomatic:  "99",
		ComplexityPropertyKeyBranchCount: "98",
	})
	g := runComplexityForTest(t, "f.go", src, []entityNode{{
		ID:         "fn:f",
		Name:       "f",
		Kind:       "function",
		SourceFile: "f.go",
		StartLine:  1,
		EndLine:    1,
		Properties: pre,
	}})
	if got := g.Entities[0].Properties.Get(ComplexityPropertyKeyCyclomatic); got != "99" {
		t.Fatalf("idempotency broken: cyclomatic_complexity = %q, want preserved 99", got)
	}
}

// TestComplexityPass_NoSourceLineInfoSkipped confirms an honest skip: a function
// entity without StartLine info gets no complexity property (no source window to
// read).
func TestComplexityPass_NoSourceLineInfoSkipped(t *testing.T) {
	g := runComplexityForTest(t, "f.go", "func f() {}\n", []entityNode{{
		ID:         "fn:f",
		Name:       "f",
		Kind:       "function",
		SourceFile: "f.go",
		StartLine:  0, // unknown
	}})
	if _, ok := g.Entities[0].Properties.Lookup(ComplexityPropertyKeyCyclomatic); ok {
		t.Fatal("expected no complexity property when StartLine is unknown")
	}
}

// TestComplexityPass_NonFunctionKindSkipped verifies only function-like entities
// are stamped (a class field / model is not).
func TestComplexityPass_NonFunctionKindSkipped(t *testing.T) {
	g := runComplexityForTest(t, "m.go", "type M struct{ X int }\n", []entityNode{{
		ID:         "model:M",
		Name:       "M",
		Kind:       "model",
		SourceFile: "m.go",
		StartLine:  1,
		EndLine:    1,
	}})
	if _, ok := g.Entities[0].Properties.Lookup(ComplexityPropertyKeyCyclomatic); ok {
		t.Fatal("expected no complexity property on a non-function entity")
	}
}
