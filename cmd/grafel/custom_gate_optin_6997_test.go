package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// #6997 — subprocCustomExtractorsOptIn is what decides whether the
// custom-extractor gate crosses into the extract subprocesses, and it must
// forward ONLY the programmatic opt-in.
//
// Both error directions are real and neither is caught anywhere else:
//
//   - Always returning a non-nil false would send `--custom-extractors false`
//     to every child on a default index. Config beats env, so that would
//     SUPPRESS a set GRAFEL_INPROC_CUSTOM_EXTRACTORS in the child while the
//     in-process path honoured it — the same disagreement between paths this
//     issue exists to close, reintroduced from the other end.
//   - Always returning a non-nil true would run ~340 custom extractors for
//     operators who opted into GRAFEL_SUBPROC_EXTRACT and nothing else,
//     deciding #6966's default-OFF question for them locally.
func TestSubprocCustomExtractorsOptIn6997(t *testing.T) {
	if got := subprocCustomExtractorsOptIn(false); got != nil {
		t.Errorf("no programmatic opt-in must forward NOTHING (nil), so the child "+
			"falls back to the inherited env exactly as the in-process path does; got %v", *got)
	}
	got := subprocCustomExtractorsOptIn(true)
	if got == nil {
		t.Fatal("WithCustomExtractors must survive the process boundary; got nil")
	}
	if !*got {
		t.Errorf("opt-in forwarded as %v, want true", *got)
	}
}

// TestSubprocCoordinatorGetsTheIndexersOwnOptIn6997 grades the one line the
// helper test above cannot reach: WHAT Index() hands the coordinator.
//
// The subprocExtract() branch of Index() is not exercised by any test in this
// repo — it forks `grafel extract` via os.Executable(), which inside a test
// binary is the test binary, and there is no BinaryPath override on that path.
// So `CustomExtractors: subprocCustomExtractorsOptIn(true)` — an unconditional
// opt-in that would run ~340 custom extractors for every operator who set
// GRAFEL_SUBPROC_EXTRACT=1 — survives every behavioural suite. Measured
// distinguishing input, macOS/APFS: django-realworld indexes to 456 entities
// with the gate on and 371 with it off.
//
// Rather than plumb a binary override into production for testability, the
// forwarded EXPRESSION is asserted, the same instrument
// TestRunCustomExtractorsCallSitesCarryBothGuards6997 uses on the dispatch
// sites. A constant here fails; so does forwarding some other flag.
func TestSubprocCoordinatorGetsTheIndexersOwnOptIn6997(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "index.go", nil, 0)
	if err != nil {
		t.Fatalf("parse index.go: %v", err)
	}

	const want = "subprocCustomExtractorsOptIn(i.customExtractors)"
	found := 0
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "CoordinatorConfig" {
			return true
		}
		found++
		var got string
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "CustomExtractors" {
				var sb strings.Builder
				if err := printer.Fprint(&sb, fset, kv.Value); err != nil {
					t.Fatalf("print: %v", err)
				}
				got = sb.String()
			}
		}
		if got != want {
			t.Errorf("extract.CoordinatorConfig.CustomExtractors = %q, want %q.\n"+
				"The custom-extractor gate forwarded to the extract subprocesses must be "+
				"the INDEXER's own opt-in; a constant decides #6966's default-OFF question "+
				"for whoever set GRAFEL_SUBPROC_EXTRACT.", got, want)
		}
		return true
	})
	if found != 1 {
		t.Fatalf("found %d extract.CoordinatorConfig literals in index.go, want exactly 1; "+
			"a second call site would need the same forwarding and this test would not see it", found)
	}
}
