package elixir_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractEx parses Elixir source and runs the registered extractor, returning
// the entity records.
func extractEx(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("elixir")
	if !ok {
		t.Fatal("elixir extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "elixir",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// exEdge reports whether the entity Named fromName has a relationship of the
// given kind whose ToID targets the exception type (the convergence-node ref).
func exEdge(recs []types.EntityRecord, fromName, kind, typeName string) bool {
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

// exNodeCount returns how many SCOPE.ExceptionType nodes exist for typeName.
func exNodeCount(recs []types.EntityRecord, typeName string) int {
	want := extractor.ExceptionTypeName(typeName)
	count := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) && recs[i].Name == want {
			count++
		}
	}
	return count
}

// TestExExceptionFlow_RaiseRescueConverge: `raise NotFoundError` in find and
// `rescue e in NotFoundError` in get must produce THROWS + CATCHES edges that
// converge on the SAME exception-type node — the value of the capability.
func TestExExceptionFlow_RaiseRescueConverge(t *testing.T) {
	src := `
defmodule Accounts do
  def find(id) do
    raise NotFoundError, "missing #{id}"
  end

  def get(id) do
    try do
      find(id)
    rescue
      e in NotFoundError -> handle(e)
    end
  end
end
`
	recs := extractEx(t, src, "accounts.ex")

	if !exEdge(recs, "find", "THROWS", "NotFoundError") {
		t.Errorf("missing THROWS(find -> NotFoundError)")
	}
	if !exEdge(recs, "get", "CATCHES", "NotFoundError") {
		t.Errorf("missing CATCHES(get -> NotFoundError)")
	}

	// Convergence invariant: exactly one NotFoundError node, and the THROWS +
	// CATCHES ToIDs both equal ExceptionTypeTargetID("NotFoundError").
	if n := exNodeCount(recs, "NotFoundError"); n != 1 {
		t.Fatalf("THROWS + CATCHES must converge on ONE NotFoundError node, got %d", n)
	}
	target := extractor.ExceptionTypeTargetID("NotFoundError")
	for i := range recs {
		if recs[i].Kind != string(types.EntityKindExceptionType) || recs[i].Name != extractor.ExceptionTypeName("NotFoundError") {
			continue
		}
		if recs[i].QualifiedName != target {
			t.Fatalf("exception node QualifiedName=%q, want %q (byQualifiedName binding)", recs[i].QualifiedName, target)
		}
	}
}

// TestExExceptionFlow_PlainRaise: `raise ArgumentError` (no message arg) still
// THROWS the named module.
func TestExExceptionFlow_PlainRaise(t *testing.T) {
	src := `
defmodule M do
  def parse(x) do
    raise ArgumentError
  end
end
`
	recs := extractEx(t, src, "m.ex")
	if !exEdge(recs, "parse", "THROWS", "ArgumentError") {
		t.Errorf("missing THROWS(parse -> ArgumentError)")
	}
}

// TestExExceptionFlow_QualifiedRaise: `raise MyApp.NotFoundError, message:`
// THROWS the normalized last segment (NotFoundError).
func TestExExceptionFlow_QualifiedRaise(t *testing.T) {
	src := `
defmodule M do
  def go(x) do
    raise MyApp.NotFoundError, message: "x"
  end
end
`
	recs := extractEx(t, src, "m.ex")
	if !exEdge(recs, "go", "THROWS", "NotFoundError") {
		t.Errorf("missing THROWS(go -> NotFoundError) from qualified raise")
	}
}

// TestExExceptionFlow_MultiRescue: `rescue e in [RuntimeError, ArgumentError]`
// yields a CATCHES edge for EACH named type.
func TestExExceptionFlow_MultiRescue(t *testing.T) {
	src := `
defmodule M do
  def run(x) do
    try do
      go(x)
    rescue
      e in [RuntimeError, ArgumentError] -> handle(e)
    end
  end
end
`
	recs := extractEx(t, src, "m.ex")
	if !exEdge(recs, "run", "CATCHES", "RuntimeError") {
		t.Errorf("missing CATCHES(run -> RuntimeError)")
	}
	if !exEdge(recs, "run", "CATCHES", "ArgumentError") {
		t.Errorf("missing CATCHES(run -> ArgumentError)")
	}
}

// TestExExceptionFlow_RescueNoBinding: `rescue MyApp.BadError ->` (no `e in`
// binding) still CATCHES the normalized type.
func TestExExceptionFlow_RescueNoBinding(t *testing.T) {
	src := `
defmodule M do
  def run(x) do
    try do
      go(x)
    rescue
      MyApp.BadError -> :err
    end
  end
end
`
	recs := extractEx(t, src, "m.ex")
	if !exEdge(recs, "run", "CATCHES", "BadError") {
		t.Errorf("missing CATCHES(run -> BadError) from unbound typed rescue")
	}
}

// --- Negatives (precision-first / honest-partial) ---

// TestExExceptionFlow_NegBareRescue: `rescue _ ->` and `rescue e ->` carry no
// exception type → NO CATCHES edge, NO exception node.
func TestExExceptionFlow_NegBareRescue(t *testing.T) {
	src := `
defmodule M do
  def run(x) do
    try do
      go(x)
    rescue
      _ -> :error
    end
  end

  def run2(x) do
    try do
      go(x)
    rescue
      e -> log(e)
    end
  end
end
`
	recs := extractEx(t, src, "m.ex")
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) {
			t.Fatalf("untyped catch-all rescue must emit no exception node, got %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindCatches) {
				t.Fatalf("untyped catch-all rescue must emit no CATCHES edge, got ToID=%q", r.ToID)
			}
		}
	}
}

// TestExExceptionFlow_NegStringRaise: `raise "msg"` (implicit RuntimeError, no
// static type token) emits NO THROWS edge — honest-partial.
func TestExExceptionFlow_NegStringRaise(t *testing.T) {
	src := `
defmodule M do
  def boom(x) do
    raise "something broke"
  end
end
`
	recs := extractEx(t, src, "m.ex")
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindThrows) {
				t.Fatalf("message-only raise must emit no THROWS edge, got ToID=%q", r.ToID)
			}
		}
	}
}

// TestExExceptionFlow_NegReraise: `reraise err, __STACKTRACE__` re-raises a
// bound variable (dynamic type) → NO THROWS edge.
func TestExExceptionFlow_NegReraise(t *testing.T) {
	src := `
defmodule M do
  def again(x) do
    reraise err, __STACKTRACE__
  end
end
`
	recs := extractEx(t, src, "m.ex")
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindThrows) {
				t.Fatalf("reraise must emit no THROWS edge, got ToID=%q", r.ToID)
			}
		}
	}
}

// TestExExceptionFlow_NegErrorTuple: the `{:error, reason}` value convention is
// NOT the exception model → no error_flow edge at all.
func TestExExceptionFlow_NegErrorTuple(t *testing.T) {
	src := `
defmodule M do
  def find(id) do
    {:error, :not_found}
  end
end
`
	recs := extractEx(t, src, "m.ex")
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) {
			t.Fatalf("error tuple must emit no exception node, got %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindThrows) || r.Kind == string(types.RelationshipKindCatches) {
				t.Fatalf("error tuple must emit no error_flow edge, got %s ToID=%q", r.Kind, r.ToID)
			}
		}
	}
}

// TestExExceptionFlow_NegCatchThrow: `catch :throw, val ->` is a value/exit
// catch, not the typed-exception model → no CATCHES edge.
func TestExExceptionFlow_NegCatchThrow(t *testing.T) {
	src := `
defmodule M do
  def run(x) do
    try do
      go(x)
    catch
      :throw, val -> val
    end
  end
end
`
	recs := extractEx(t, src, "m.ex")
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindCatches) {
				t.Fatalf("catch :throw must emit no CATCHES edge, got ToID=%q", r.ToID)
			}
		}
	}
}

// TestExExceptionFlow_NegPlainDef: a plain function with no raise/rescue emits
// no error_flow entities.
func TestExExceptionFlow_NegPlainDef(t *testing.T) {
	src := `
defmodule M do
  def add(a, b), do: a + b
end
`
	recs := extractEx(t, src, "m.ex")
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) {
			t.Fatalf("plain def must emit no exception node, got %q", recs[i].Name)
		}
	}
}
