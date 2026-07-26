package main

// group_algo_wiring_5909_test.go — pins WHICH group-algo entrypoint each caller
// uses (#5909, refs #5954).
//
// groupalgo has two incremental entrypoints that differ only in retention:
//
//   - RunGroupAlgorithmsIncremental       — memoizes. The long-lived daemon's
//     in-process fallback needs this: the process-local guard is what bounds the
//     O(V*E) betweenness recompute to once per group-version when the overlay
//     cannot be persisted (#5309 / the group-scope CPU spin).
//   - RunGroupAlgorithmsIncrementalOneShot — does NOT memoize. The forked
//     `group-algo --write` child computes once and exits, so the memo can never
//     be read again; storing it only pins a SECOND full *AlgorithmResults
//     (PageRank + community + centrality maps over the whole union) live across
//     the entire WriteOverlayFromResult window, at the child's heap peak.
//
// Each entrypoint is well covered by the groupalgo package tests. The CHOICE
// between them at the call site is not: swapping the two calls compiles, and
// every behavioural test in both packages still passes, because each function
// remains individually correct. A silent swap is a DOUBLE regression that CI
// cannot see — the daemon loses the compute-once guard AND the child re-acquires
// the retention this change removed.
//
// So this test asserts the wiring structurally, from source. It parses the two
// call sites and matches the selector identifier EXACTLY (never by substring:
// "RunGroupAlgorithmsIncremental" is a prefix of the one-shot name).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

const (
	memoizingEntrypoint = "RunGroupAlgorithmsIncremental"
	oneShotEntrypoint   = "RunGroupAlgorithmsIncrementalOneShot"
)

// groupalgoCallsIn returns the set of groupalgo.<Name> functions called within
// the named top-level func in file. Exact identifier match on both the package
// qualifier and the selector.
func groupalgoCallsIn(t *testing.T, file, funcName string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var target *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == funcName && fd.Recv == nil {
			target = fd
			break
		}
	}
	if target == nil {
		t.Fatalf("func %s not found in %s — this test's anchor moved; re-point it, do not delete it", funcName, file)
	}
	got := map[string]bool{}
	ast.Inspect(target, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "groupalgo" {
			return true
		}
		got[sel.Sel.Name] = true
		return true
	})
	return got
}

// TestWiring_OneShotChildUsesOneShotEntrypoint: the `group-algo --write` child
// must call the NON-memoizing entrypoint.
func TestWiring_OneShotChildUsesOneShotEntrypoint(t *testing.T) {
	calls := groupalgoCallsIn(t, "group_algo.go", "runGroupAlgo")

	if !calls[oneShotEntrypoint] {
		t.Errorf("the `group-algo --write` child does not call groupalgo.%s — the short-lived child would retain a second full *AlgorithmResults across the overlay write (#5909)", oneShotEntrypoint)
	}
	if calls[memoizingEntrypoint] {
		t.Errorf("the `group-algo --write` child calls the MEMOIZING groupalgo.%s — that is the daemon's entrypoint; in a process that exits immediately the memo is pure retention", memoizingEntrypoint)
	}
}

// TestWiring_DaemonInProcessUsesMemoizingEntrypoint: the daemon's in-process
// group-algo fallback must call the MEMOIZING entrypoint — the compute-once
// guard (#5309) only exists in that one.
func TestWiring_DaemonInProcessUsesMemoizingEntrypoint(t *testing.T) {
	calls := groupalgoCallsIn(t, "daemon.go", "daemonSchedulerGroupAlgo")

	if !calls[memoizingEntrypoint] {
		t.Errorf("the daemon in-process group-algo fallback does not call groupalgo.%s — it loses the compute-once-per-version guard, reopening the unbounded O(V*E) betweenness re-spin when the overlay cannot be persisted (#5309)", memoizingEntrypoint)
	}
	if calls[oneShotEntrypoint] {
		t.Errorf("the daemon in-process group-algo fallback calls the one-shot groupalgo.%s — the daemon is long-lived and MUST memoize", oneShotEntrypoint)
	}
}
