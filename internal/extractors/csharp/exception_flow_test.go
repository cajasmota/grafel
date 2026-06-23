package csharp_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractCSharpExc parses + extracts C# source for the error-flow tests.
func extractCSharpExc(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// csExcEdge asserts a THROWS / CATCHES edge from fromName to the shared
// exception-type node for typeName (matching the flagship edge shape: ToID ==
// ExceptionTypeTargetID, Kind == THROWS/CATCHES).
func csExcEdge(recs []types.EntityRecord, fromName, kind, typeName string) bool {
	want := extractor.ExceptionTypeTargetID(typeName)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == kind && r.ToID == want {
				return true
			}
		}
	}
	return false
}

// csExcNodeCount counts SCOPE.ExceptionType nodes named exception:<typeName>.
func csExcNodeCount(recs []types.EntityRecord, typeName string) int {
	want := extractor.ExceptionTypeName(typeName)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) && recs[i].Name == want {
			c++
		}
	}
	return c
}

// csExcPattern returns the "pattern" property of the first matching edge, or "".
func csExcPattern(recs []types.EntityRecord, fromName, kind, typeName string) string {
	want := extractor.ExceptionTypeTargetID(typeName)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == kind && r.ToID == want {
				return r.Properties["pattern"]
			}
		}
	}
	return ""
}

// TestCSharpExceptionFlow_ThrowAndCatchConverge: the canonical controller idiom
// — `throw new NotFoundException(...)` in one method and
// `catch (NotFoundException ex) { return NotFound(); }` in another — produces a
// THROWS edge naming the raised type, a CATCHES edge naming the caught type,
// and BOTH converge on ONE shared exception-type node (cross-method,
// cross-language convergence invariant).
func TestCSharpExceptionFlow_ThrowAndCatchConverge(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;
public class UsersController {
    public User Find(int id) {
        var u = repo.Get(id);
        if (u == null) {
            throw new NotFoundException("missing");
        }
        return u;
    }

    public IActionResult Get(int id) {
        try {
            return Ok(Find(id));
        } catch (NotFoundException ex) {
            return NotFound();
        }
    }
}
`
	recs := extractCSharpExc(t, src)

	if !csExcEdge(recs, "UsersController.Find", "THROWS", "NotFoundException") {
		t.Errorf("missing THROWS(UsersController.Find -> NotFoundException)")
	}
	if p := csExcPattern(recs, "UsersController.Find", "THROWS", "NotFoundException"); p != "throw_new" {
		t.Errorf("THROWS pattern = %q, want throw_new", p)
	}
	if !csExcEdge(recs, "UsersController.Get", "CATCHES", "NotFoundException") {
		t.Errorf("missing CATCHES(UsersController.Get -> NotFoundException)")
	}
	if p := csExcPattern(recs, "UsersController.Get", "CATCHES", "NotFoundException"); p != "catch_type" {
		t.Errorf("CATCHES pattern = %q, want catch_type", p)
	}
	if n := csExcNodeCount(recs, "NotFoundException"); n != 1 {
		t.Fatalf("throw + catch of NotFoundException must converge on ONE node, got %d", n)
	}
}

// TestCSharpExceptionFlow_QualifiedTypesNormalize: qualified throw / catch types
// collapse to their bare class name (System.IO.IOException -> IOException) so a
// qualified raise and an unqualified catch resolve to the SAME node.
func TestCSharpExceptionFlow_QualifiedTypesNormalize(t *testing.T) {
	src := `
public class IoSvc {
    public void Read() {
        try {
            Stream();
        } catch (System.IO.IOException ex) {
            throw new System.IO.IOException("re");
        }
    }
}
`
	recs := extractCSharpExc(t, src)
	if !csExcEdge(recs, "IoSvc.Read", "CATCHES", "IOException") {
		t.Errorf("missing CATCHES(IoSvc.Read -> IOException) from qualified catch")
	}
	if !csExcEdge(recs, "IoSvc.Read", "THROWS", "IOException") {
		t.Errorf("missing THROWS(IoSvc.Read -> IOException) from qualified throw new")
	}
	if n := csExcNodeCount(recs, "IOException"); n != 1 {
		t.Fatalf("qualified throw + catch must converge on ONE IOException node, got %d", n)
	}
}

// TestCSharpExceptionFlow_TypedCatchNoVar: `catch (Exception) { ... }` with no
// bound variable still yields a CATCHES edge naming the caught type.
func TestCSharpExceptionFlow_TypedCatchNoVar(t *testing.T) {
	src := `
public class Svc {
    public void M() {
        try { Do(); }
        catch (InvalidOperationException) { Recover(); }
    }
}
`
	recs := extractCSharpExc(t, src)
	if !csExcEdge(recs, "Svc.M", "CATCHES", "InvalidOperationException") {
		t.Errorf("missing CATCHES(Svc.M -> InvalidOperationException) for var-less catch")
	}
}

// TestCSharpExceptionFlow_CatchFilterTypeOnly: `catch (DbException ex) when (...)`
// records ONLY the caught type — the `when` filter expression is a sibling
// clause and must never be inspected.
func TestCSharpExceptionFlow_CatchFilterTypeOnly(t *testing.T) {
	src := `
public class Svc {
    public void M() {
        try { Do(); }
        catch (DbException ex) when (ex.IsTransient) { Retry(); }
    }
}
`
	recs := extractCSharpExc(t, src)
	if !csExcEdge(recs, "Svc.M", "CATCHES", "DbException") {
		t.Errorf("missing CATCHES(Svc.M -> DbException) with when-filter")
	}
	// No spurious node fabricated from the filter expression.
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) &&
			recs[i].Name != "exception:DbException" {
			t.Errorf("when-filter fabricated an exception node: %q", recs[i].Name)
		}
	}
}

// TestCSharpExceptionFlow_BareCatchDropped: NEGATIVE — an untyped `catch { }`
// (no catch_declaration) emits NO CATCHES edge / node.
func TestCSharpExceptionFlow_BareCatchDropped(t *testing.T) {
	src := `
public class Svc {
    public void M() {
        try { Do(); }
        catch { Swallow(); }
    }
}
`
	recs := extractCSharpExc(t, src)
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) {
			t.Errorf("bare catch fabricated an exception node: %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindCatches) {
				t.Errorf("bare catch emitted a CATCHES edge: %+v", r)
			}
		}
	}
}

// TestCSharpExceptionFlow_RethrowDropped: NEGATIVE — `throw;` and `throw ex;`
// re-throw an existing exception (no NEW type) and must emit no THROWS edge/node.
func TestCSharpExceptionFlow_RethrowDropped(t *testing.T) {
	src := `
public class Svc {
    public void M() {
        try { Do(); }
        catch (ArgumentException ex) {
            throw;
        }
    }
    public void N() {
        try { Do(); }
        catch (ArgumentException ex) {
            throw ex;
        }
    }
}
`
	recs := extractCSharpExc(t, src)
	// Only ArgumentException (from the typed catches) may exist.
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) &&
			recs[i].Name != "exception:ArgumentException" {
			t.Errorf("re-throw fabricated an exception node: %q", recs[i].Name)
		}
	}
	if csExcEdge(recs, "Svc.M", "THROWS", "ArgumentException") {
		t.Errorf("bare `throw;` must not emit a THROWS edge")
	}
}

// TestCSharpExceptionFlow_PlainMethodNoEdges: NEGATIVE — a method with no
// throw/catch produces no error-flow entities or edges.
func TestCSharpExceptionFlow_PlainMethodNoEdges(t *testing.T) {
	src := `
public class Calc {
    public int Add(int a, int b) {
        return a + b;
    }
}
`
	recs := extractCSharpExc(t, src)
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) {
			t.Errorf("plain method produced an exception node: %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindThrows) ||
				r.Kind == string(types.RelationshipKindCatches) {
				t.Errorf("plain method produced an error-flow edge: %+v", r)
			}
		}
	}
}
