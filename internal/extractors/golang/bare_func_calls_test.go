// bare_func_calls_test.go — fixture tests for issue #1806.
//
// Covers CALLS edge emission for intra-package bare-function calls: the
// `funcName(args)` invocation pattern where one top-level function (or method)
// calls another function in the same package by its unqualified identifier,
// without a selector operand (no `pkg.Func()` or `recv.Method()` form).
//
// These tests verify:
//  1. func A() { B() } and func B() {} in the SAME file → CALLS A→B emitted
//     with intra_file=true property and structural-ref ToID.
//  2. func A() { B() } where B is in a SEPARATE sibling file (not present in
//     this extraction) → CALLS edge still emitted with structural-ref keyed on
//     A's file; cross-file resolution is handled by the resolver's
//     byPackageOperation index (v1 limitation: extractor cannot confirm
//     cross-file resolution at extraction time).
//  3. Method calling a bare function in the same file → CALLS edge emitted,
//     including calls from within goroutine closures (the dogfood case from
//     iter6 Q17: handleGetNodeSource → readSourceWindow).
//  4. Existing CALLS behaviour UNCHANGED: selector-form calls (pkg.Func(),
//     recv.Method()) are NOT affected by the intra_file property.
//  5. Self-recursion is still suppressed (no self-edge A→A for top-level funcs
//     without a receiver, or method recursion suppressed when isSelfReceiver).
package golang

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// runBareCallExtract runs the Go extractor on src with the given path.
func runBareCallExtract(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ex := &GoExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// findCallsEdgeTo looks in e.Relationships for any CALLS edge whose ToID
// contains toSubstr.
func findCallsEdgeTo(e *types.EntityRecord, toSubstr string) *types.RelationshipRecord {
	if e == nil {
		return nil
	}
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind == "CALLS" && strings.Contains(r.ToID, toSubstr) {
			return r
		}
	}
	return nil
}

// bareCallSummary returns a compact string of all CALLS edges on ents for test
// failure messages.
func bareCallSummary(ents []types.EntityRecord) string {
	var b strings.Builder
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				b.WriteString(e.Name)
				b.WriteString(" -[CALLS")
				if r.Properties["intra_file"] == "true" {
					b.WriteString(",intra_file")
				}
				b.WriteString("]-> ")
				b.WriteString(r.ToID)
				b.WriteString("; ")
			}
		}
	}
	if b.Len() == 0 {
		return "(no CALLS)"
	}
	return b.String()
}

// ---- Test 1: func A() { B() } and func B() {} in the SAME file -------------
//
// The canonical intra-file bare-function call. A and B are both top-level
// functions in file.go; A calls B. The extractor must:
//   - Emit a CALLS edge from A to B.
//   - The ToID must be the Format A structural-ref keyed on file.go.
//   - The edge must carry Properties["intra_file"]="true" because B is
//     confirmed in the same file's function entity set.
//
// Issue #1806: resolves `find_callers(B)` returning no_incoming_edges when
// both A and B are in the same package file.

func TestBareFuncCalls_SameFile(t *testing.T) {
	src := `package pkg

func A() {
	B()
}

func B() {}
`
	ents := runBareCallExtract(t, src, "pkg/file.go")
	a := findEntityByName(ents, "A")
	if a == nil {
		t.Fatal("entity A not found")
	}

	// Must have a CALLS edge whose ToID contains "B".
	hit := findCallsEdgeTo(a, "B")
	if hit == nil {
		t.Fatalf("expected CALLS edge from A → B; all edges: %s", bareCallSummary(ents))
	}

	// ToID must be a Format A structural-ref keyed on the caller's (= callee's)
	// file — scope:operation:method:go:pkg/file.go:B.
	wantToIDPrefix := "scope:operation:method:go:pkg/file.go:B"
	if hit.ToID != wantToIDPrefix {
		t.Errorf("expected ToID %q, got %q", wantToIDPrefix, hit.ToID)
	}

	// intra_file=true must be stamped because B is in the same file.
	if hit.Properties["intra_file"] != "true" {
		t.Errorf("expected intra_file=true on same-file CALLS edge, got Properties=%v", hit.Properties)
	}
}

// ---- Test 2: A and B in SEPARATE files of the same package -----------------
//
// A is in file_a.go; B is in file_b.go (not present in this extraction run).
// The extractor processes file_a.go in isolation: it cannot confirm that B
// is in the same package, only that B is a bare identifier call.
// The extractor must:
//   - Still emit a CALLS edge from A to B (cross-file same-package call).
//   - The ToID must be the Format A structural-ref keyed on A's file (file_a.go).
//   - The edge must NOT carry intra_file=true (B is not in this file).
// The resolver's byPackageOperation index handles cross-file resolution at
// query time (v1 limitation documented in extractCallRelationships).

func TestBareFuncCalls_CrossFile(t *testing.T) {
	// Only file_a.go is extracted here; B is intentionally absent.
	src := `package pkg

func A() {
	B()
}
`
	ents := runBareCallExtract(t, src, "pkg/file_a.go")
	a := findEntityByName(ents, "A")
	if a == nil {
		t.Fatal("entity A not found")
	}

	// A CALLS edge must be emitted even though B is not in this file.
	hit := findCallsEdgeTo(a, "B")
	if hit == nil {
		t.Fatalf("expected CALLS edge from A → B even for cross-file call; all edges: %s",
			bareCallSummary(ents))
	}

	// ToID must be the structural-ref keyed on A's file (file_a.go), not on
	// an imagined file_b.go — the extractor cannot know B's file at this point.
	wantToID := "scope:operation:method:go:pkg/file_a.go:B"
	if hit.ToID != wantToID {
		t.Errorf("expected ToID %q, got %q", wantToID, hit.ToID)
	}

	// intra_file must NOT be set for a cross-file call (B is not in this file).
	if hit.Properties["intra_file"] == "true" {
		t.Errorf("cross-file CALLS edge should NOT carry intra_file=true; got Properties=%v",
			hit.Properties)
	}
}

// ---- Test 3: method calling a bare function in the same file ---------------
//
// Dogfood case from iter6 Q17: (s *Server) handleGetNodeSource calls
// readSourceWindow (a top-level function in the same file), including from
// inside a goroutine closure. The extractor must emit the CALLS edge even
// though the call site is inside a func_literal (goroutine body).

func TestBareFuncCalls_MethodCallsSamePkgFunc(t *testing.T) {
	src := `package mcp

type Server struct{}

func readSourceWindow(path string, start, end int) (string, error) {
	return "", nil
}

func (s *Server) handleGetNodeSource() {
	go func() {
		text, rerr := readSourceWindow("x", 1, 10)
		_ = text
		_ = rerr
	}()
}
`
	ents := runBareCallExtract(t, src, "internal/mcp/tools.go")
	caller := findEntityByName(ents, "Server.handleGetNodeSource")
	if caller == nil {
		t.Fatal("Server.handleGetNodeSource entity not found")
	}

	// Must have a CALLS edge targeting readSourceWindow.
	hit := findCallsEdgeTo(caller, "readSourceWindow")
	if hit == nil {
		t.Fatalf("expected CALLS from Server.handleGetNodeSource → readSourceWindow; all edges: %s",
			bareCallSummary(ents))
	}

	// The ToID must be the structural-ref keyed on tools.go.
	wantToID := "scope:operation:method:go:internal/mcp/tools.go:readSourceWindow"
	if hit.ToID != wantToID {
		t.Errorf("expected ToID %q, got %q", wantToID, hit.ToID)
	}

	// intra_file=true because readSourceWindow is declared in the same file.
	if hit.Properties["intra_file"] != "true" {
		t.Errorf("expected intra_file=true on same-file CALLS edge, got Properties=%v",
			hit.Properties)
	}
}

// ---- Test 4: selector-form calls are NOT affected by intra_file stamp ------
//
// pkg.Func() and recv.Method() calls must NOT carry intra_file=true; they are
// selector_expression calls resolved through separate paths (byMember,
// receiver_type, external-package). This test confirms no regression.

func TestBareFuncCalls_SelectorCallNoIntraFile(t *testing.T) {
	src := `package demo

import "fmt"

type Foo struct{}

func (f *Foo) Bar() {}

func driver(x *Foo) {
	x.Bar()
	fmt.Println("hi")
}
`
	ents := runBareCallExtract(t, src, "demo/demo.go")
	caller := findEntityByName(ents, "driver")
	if caller == nil {
		t.Fatal("driver not found")
	}

	// x.Bar() — selector_expression call; must NOT have intra_file=true.
	barEdge := findCallsEdgeTo(caller, "Bar")
	if barEdge == nil {
		t.Fatalf("expected CALLS edge from driver → Bar; all edges: %s", bareCallSummary(ents))
	}
	if barEdge.Properties["intra_file"] == "true" {
		t.Errorf("selector-form x.Bar() should NOT carry intra_file=true, got Properties=%v",
			barEdge.Properties)
	}

	// fmt.Println() — external selector call; no intra_file.
	printEdge := findCallsEdgeTo(caller, "Println")
	if printEdge != nil && printEdge.Properties["intra_file"] == "true" {
		t.Errorf("external selector fmt.Println() should NOT carry intra_file=true, got Properties=%v",
			printEdge.Properties)
	}
}

// ---- Test 5: self-recursion suppression still works ------------------------
//
// func A() { A() } must NOT emit a self-loop CALLS edge (recvType == "",
// so isSelfCall fires for target == callerName). Regression guard.

func TestBareFuncCalls_NoSelfLoop(t *testing.T) {
	src := `package pkg

func A() {
	A() // recursive call
}
`
	ents := runBareCallExtract(t, src, "pkg/file.go")
	a := findEntityByName(ents, "A")
	if a == nil {
		t.Fatal("entity A not found")
	}

	// No CALLS edge from A to A (self-loop must be suppressed).
	for _, r := range a.Relationships {
		if r.Kind == "CALLS" && strings.Contains(r.ToID, ":A") {
			t.Errorf("self-loop edge leaked: %+v", r)
		}
	}
}

// ---- Test 6: multiple same-file bare-function calls from one caller --------
//
// func Orchestrate() { Step1(); Step2(); Step3() } where all three targets
// are declared in the same file. All three must emit CALLS edges with
// intra_file=true.

func TestBareFuncCalls_MultipleIntraFileCalls(t *testing.T) {
	src := `package pipeline

func Orchestrate() {
	Step1()
	Step2()
	Step3()
}

func Step1() {}
func Step2() {}
func Step3() {}
`
	ents := runBareCallExtract(t, src, "pipeline/pipeline.go")
	caller := findEntityByName(ents, "Orchestrate")
	if caller == nil {
		t.Fatal("Orchestrate not found")
	}

	for _, want := range []string{"Step1", "Step2", "Step3"} {
		hit := findCallsEdgeTo(caller, want)
		if hit == nil {
			t.Errorf("expected CALLS edge from Orchestrate → %s; all edges: %s",
				want, bareCallSummary(ents))
			continue
		}
		if hit.Properties["intra_file"] != "true" {
			t.Errorf("expected intra_file=true on Orchestrate → %s edge, got Properties=%v",
				want, hit.Properties)
		}
	}
}
