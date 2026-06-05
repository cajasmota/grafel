package dashboard

import "testing"

// efProps builds a THROWS/CATCHES edge property map.
func efProps(exceptionType, pattern string) map[string]string {
	m := map[string]string{}
	if exceptionType != "" {
		m["exception_type"] = exceptionType
	}
	if pattern != "" {
		m["pattern"] = pattern
	}
	return m
}

// TestErrorFlowFoldRollupAndUncaught verifies the core fold: per-exception
// thrower/catcher rollup, per-site de-duplication, the honest uncaught flag
// (thrown-but-no-catcher-in-graph), totals, and uncaught-first ordering.
func TestErrorFlowFoldRollupAndUncaught(t *testing.T) {
	a := newEFAccum()

	// Exception nodes (synthetic "<exception>" SourceFile) + callables.
	a.entByID["exc:ValidationError"] = efEntMeta{name: "exception:ValidationError", kind: "SCOPE.ExceptionType", sourceFile: "<exception>", repoSlug: "api"}
	a.entByID["exc:NotFound"] = efEntMeta{name: "exception:NotFound", kind: "SCOPE.ExceptionType", sourceFile: "<exception>", repoSlug: "api"}
	a.entByID["createUser"] = efEntMeta{name: "createUser", kind: "SCOPE.Function", sourceFile: "users.py", repoSlug: "api", startLine: 10}
	a.entByID["updateUser"] = efEntMeta{name: "updateUser", kind: "SCOPE.Function", sourceFile: "users.py", repoSlug: "api", startLine: 30}
	a.entByID["handler"] = efEntMeta{name: "handler", kind: "SCOPE.Function", sourceFile: "views.py", repoSlug: "api", startLine: 5}

	// ValidationError: thrown by two functions, caught by one → CAUGHT.
	a.addEdge(efEdge{fromID: "createUser", toID: "exc:ValidationError", catch: false, props: efProps("ValidationError", "raise")})
	a.addEdge(efEdge{fromID: "updateUser", toID: "exc:ValidationError", catch: false, props: efProps("ValidationError", "raise")})
	a.addEdge(efEdge{fromID: "createUser", toID: "exc:ValidationError", catch: false, props: efProps("ValidationError", "raise")}) // dup thrower
	a.addEdge(efEdge{fromID: "handler", toID: "exc:ValidationError", catch: true, props: efProps("ValidationError", "except")})

	// NotFound: thrown once, never caught → UNCAUGHT (no_catcher_in_graph).
	a.addEdge(efEdge{fromID: "createUser", toID: "exc:NotFound", catch: false, props: efProps("NotFound", "raise")})

	rep := a.assemble("g")

	if rep.TotalThrows != 3 { // dup thrower not counted (2 ValidationError + 1 NotFound)
		t.Fatalf("TotalThrows = %d, want 3", rep.TotalThrows)
	}
	if rep.TotalCatches != 1 {
		t.Fatalf("TotalCatches = %d, want 1", rep.TotalCatches)
	}
	if rep.TotalExceptions != 2 {
		t.Fatalf("TotalExceptions = %d, want 2", rep.TotalExceptions)
	}
	if rep.TotalUncaught != 1 {
		t.Fatalf("TotalUncaught = %d, want 1", rep.TotalUncaught)
	}

	// Uncaught (NotFound) must sort first — the actionable signal.
	first := rep.Exceptions[0]
	if first.Type != "NotFound" || !first.Uncaught || first.UncaughtReason != "no_catcher_in_graph" {
		t.Fatalf("expected NotFound uncaught first, got %+v", first)
	}
	if first.ThrowCount != 1 || first.CatchCount != 0 {
		t.Fatalf("NotFound counts wrong: %+v", first)
	}

	// ValidationError caught, two throwers de-duped, resolved + sorted by name.
	ve := rep.Exceptions[1]
	if ve.Type != "ValidationError" || ve.Uncaught {
		t.Fatalf("expected ValidationError caught, got %+v", ve)
	}
	if ve.ThrowCount != 2 || ve.CatchCount != 1 {
		t.Fatalf("ValidationError counts wrong: %+v", ve)
	}
	if ve.Throwers[0].Name != "createUser" || ve.Throwers[1].Name != "updateUser" {
		t.Fatalf("thrower order wrong: %+v", ve.Throwers)
	}
	// Resolved thrower carries repo-qualified ID + kind + ref + pattern.
	if ve.Throwers[0].EntityID != "api/createUser" || ve.Throwers[0].Kind != "SCOPE.Function" ||
		ve.Throwers[0].SourceFile != "users.py" || ve.Throwers[0].Pattern != "raise" {
		t.Fatalf("thrower resolution wrong: %+v", ve.Throwers[0])
	}
	if ve.Catchers[0].Name != "handler" || ve.Catchers[0].SourceFile != "views.py" {
		t.Fatalf("catcher resolution wrong: %+v", ve.Catchers[0])
	}
}

// TestErrorFlowTypeKeyResolution verifies exception-type key resolution falls
// back honestly: entity Name (prefix-stripped) → edge property → raw tail.
func TestErrorFlowTypeKeyResolution(t *testing.T) {
	a := newEFAccum()

	// Resolved node: name wins (prefix stripped).
	a.entByID["n1"] = efEntMeta{name: "exception:IOException"}
	if got := a.exceptionTypeKey("n1", efProps("ignored", "")); got != "IOException" {
		t.Fatalf("resolved key = %q, want IOException", got)
	}
	// Unresolved node but edge property present → property.
	if got := a.exceptionTypeKey("scope:exceptiontype:TimeoutError", efProps("TimeoutError", "")); got != "TimeoutError" {
		t.Fatalf("prop key = %q, want TimeoutError", got)
	}
	// Unresolved, no property → raw tail (prefix-stripped), never invented.
	if got := a.exceptionTypeKey("scope:exceptiontype:exception:Boom", nil); got != "Boom" {
		t.Fatalf("tail key = %q, want Boom", got)
	}
}

// TestErrorFlowCatchOnlyNotUncaught verifies an exception that is only ever
// CAUGHT (no THROWS in graph) is surfaced but never flagged uncaught.
func TestErrorFlowCatchOnlyNotUncaught(t *testing.T) {
	a := newEFAccum()
	a.addEdge(efEdge{fromID: "mw", toID: "exc:Panic", catch: true, props: efProps("Panic", "recover")})

	rep := a.assemble("g")
	if rep.TotalExceptions != 1 || rep.TotalUncaught != 0 {
		t.Fatalf("catch-only: exceptions=%d uncaught=%d, want 1/0", rep.TotalExceptions, rep.TotalUncaught)
	}
	if rep.Exceptions[0].Uncaught {
		t.Fatalf("catch-only exception must not be uncaught: %+v", rep.Exceptions[0])
	}
	if rep.Exceptions[0].CatchCount != 1 || rep.Exceptions[0].ThrowCount != 0 {
		t.Fatalf("catch-only counts wrong: %+v", rep.Exceptions[0])
	}
}

// TestErrorFlowEmpty verifies a clean empty report when no THROWS/CATCHES edges
// were folded (repo with no exception modelling).
func TestErrorFlowEmpty(t *testing.T) {
	rep := newEFAccum().assemble("g")
	if rep.TotalExceptions != 0 || rep.TotalThrows != 0 || rep.TotalCatches != 0 || rep.TotalUncaught != 0 {
		t.Fatalf("empty report not clean: %+v", rep)
	}
	if rep.Exceptions == nil {
		t.Fatalf("Exceptions must be non-nil empty slice for stable JSON")
	}
}
