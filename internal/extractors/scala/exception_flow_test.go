package scala_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractScalaRaw runs the registered scala extractor on src and returns the
// raw entity records (including exception-type convergence nodes and their
// THROWS / CATCHES edges).
func extractScalaRaw(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("scala")
	if !ok {
		t.Fatal("scala extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.scala",
		Content:  []byte(src),
		Language: "scala",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// scalaExcEdge reports whether some entity named fromName carries a kind-edge
// (THROWS / CATCHES) whose ToID is the shared exception-type convergence target
// for typeName — the SAME ExceptionTypeTargetID the flagships emit.
func scalaExcEdge(recs []types.EntityRecord, fromName, kind, typeName string) bool {
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

// scalaExcNodeCount counts exception-type convergence nodes for typeName.
func scalaExcNodeCount(recs []types.EntityRecord, typeName string) int {
	want := extractor.ExceptionTypeName(typeName)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) && recs[i].Name == want {
			c++
		}
	}
	return c
}

// scalaExcAnyNode reports whether ANY exception-type node exists (used by
// negatives to assert nothing was fabricated).
func scalaExcNodeNames(recs []types.EntityRecord) []string {
	var out []string
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExceptionType) {
			out = append(out, recs[i].Name)
		}
	}
	return out
}

// TestScalaExceptionFlow_ThrowsAndCatchConverge: a `throw new NotFoundException`
// in find() and a typed `catch { case e: NotFoundException => }` in load()
// converge on the SAME exception:NotFoundException node (flagship invariant).
func TestScalaExceptionFlow_ThrowsAndCatchConverge(t *testing.T) {
	src := `package demo

class UserService {
  def find(id: Int): User = {
    throw new NotFoundException("nope")
  }

  def load(): Unit = {
    try {
      find(1)
    } catch {
      case e: NotFoundException => handle(e)
    }
  }
}
`
	recs := extractScalaRaw(t, src)

	if !scalaExcEdge(recs, "find", "THROWS", "NotFoundException") {
		t.Errorf("missing THROWS(find -> NotFoundException)")
	}
	if !scalaExcEdge(recs, "load", "CATCHES", "NotFoundException") {
		t.Errorf("missing CATCHES(load -> NotFoundException)")
	}
	if n := scalaExcNodeCount(recs, "NotFoundException"); n != 1 {
		t.Fatalf("throw + catch of NotFoundException must converge on ONE node, got %d", n)
	}
	// ToID must equal the shared cross-language convergence target.
	wantID := extractor.ExceptionTypeTargetID("NotFoundException")
	if wantID != "scope:exceptiontype:NotFoundException" {
		t.Fatalf("unexpected convergence ToID shape: %q", wantID)
	}
}

// TestScalaExceptionFlow_MultiCatch: a pattern-match catch with two typed cases
// yields a CATCHES edge for EACH type (`case e: IOException`, `case _: Timeout`).
func TestScalaExceptionFlow_MultiCatch(t *testing.T) {
	src := `package demo

class Svc {
  def run(): Unit = {
    try {
      doWork()
    } catch {
      case e: IOException => a()
      case _: TimeoutException => b()
    }
  }
}
`
	recs := extractScalaRaw(t, src)
	if !scalaExcEdge(recs, "run", "CATCHES", "IOException") {
		t.Errorf("missing CATCHES(run -> IOException)")
	}
	if !scalaExcEdge(recs, "run", "CATCHES", "TimeoutException") {
		t.Errorf("missing CATCHES(run -> TimeoutException)")
	}
}

// TestScalaExceptionFlow_QualifiedCatchNormalized: a qualified caught type
// `case e: java.io.IOException =>` normalizes to bare IOException and converges
// with a bare `throw new IOException` on ONE node.
func TestScalaExceptionFlow_QualifiedCatchNormalized(t *testing.T) {
	src := `package demo

class Svc {
  def emit(): Unit = {
    throw new IOException("x")
  }
  def run(): Unit = {
    try { emit() } catch {
      case e: java.io.IOException => recover()
    }
  }
}
`
	recs := extractScalaRaw(t, src)
	if !scalaExcEdge(recs, "run", "CATCHES", "IOException") {
		t.Errorf("qualified java.io.IOException not normalized to bare IOException")
	}
	if !scalaExcEdge(recs, "emit", "THROWS", "IOException") {
		t.Errorf("missing THROWS(emit -> IOException)")
	}
	if n := scalaExcNodeCount(recs, "IOException"); n != 1 {
		t.Fatalf("qualified catch + bare throw must converge on ONE node, got %d", n)
	}
}

// TestScalaExceptionFlow_Recover: `Try { ... }.recover { case e: X => }`
// records CATCHES of the recovered exception type.
func TestScalaExceptionFlow_Recover(t *testing.T) {
	src := `package demo

class Svc {
  def rec(): Unit = {
    Try(risky()).recover { case e: IllegalStateException => fallback() }
  }
  def recW(): Unit = {
    fut.recoverWith { case e: ServiceException => retry() }
  }
}
`
	recs := extractScalaRaw(t, src)
	if !scalaExcEdge(recs, "rec", "CATCHES", "IllegalStateException") {
		t.Errorf("missing CATCHES(rec -> IllegalStateException) from .recover")
	}
	if !scalaExcEdge(recs, "recW", "CATCHES", "ServiceException") {
		t.Errorf("missing CATCHES(recW -> ServiceException) from .recoverWith")
	}
}

// TestScalaExceptionFlow_ThrowNew: a plain `throw new ValidationException(...)`.
func TestScalaExceptionFlow_ThrowNew(t *testing.T) {
	src := `package demo

class Validator {
  def check(x: Int): Unit = {
    if (x < 0) throw new ValidationException("negative")
  }
}
`
	recs := extractScalaRaw(t, src)
	if !scalaExcEdge(recs, "check", "THROWS", "ValidationException") {
		t.Errorf("missing THROWS(check -> ValidationException)")
	}
}

// TestScalaExceptionFlow_Negatives — NONE of these typed-less shapes may
// fabricate an exception node or edge:
//   - catch-all `case _ =>`
//   - `case NonFatal(e) =>` (extractor pattern, not a static type)
//   - re-throw `throw e` (variable, no NEW type)
//   - a plain def with no error flow
func TestScalaExceptionFlow_Negatives(t *testing.T) {
	src := `package demo

class Svc {
  def catchAll(): Unit = {
    try { work() } catch { case _ => log() }
  }
  def nonFatal(): Unit = {
    try { work() } catch { case NonFatal(e) => log() }
  }
  def rethrow(): Unit = {
    try { work() } catch { case e: RuntimeException => throw e }
  }
  def plain(): Int = 42
}
`
	recs := extractScalaRaw(t, src)

	// The ONLY legitimate exception node here is RuntimeException (the typed
	// catch in rethrow). The bare `throw e` re-throw must add nothing.
	names := scalaExcNodeNames(recs)
	for _, n := range names {
		if n != "exception:RuntimeException" {
			t.Errorf("typed-less shape fabricated an exception node: %q", n)
		}
	}
	if !scalaExcEdge(recs, "rethrow", "CATCHES", "RuntimeException") {
		t.Errorf("missing CATCHES(rethrow -> RuntimeException)")
	}
	// catch-all / NonFatal must NOT produce any edge.
	for _, fn := range []string{"catchAll", "nonFatal"} {
		for i := range recs {
			if recs[i].Name != fn {
				continue
			}
			for _, r := range recs[i].Relationships {
				if r.Kind == "CATCHES" || r.Kind == "THROWS" {
					t.Errorf("%s produced an unexpected %s edge", fn, r.Kind)
				}
			}
		}
	}
	// rethrow must NOT emit a THROWS edge (bare `throw e`).
	if scalaExcEdge(recs, "rethrow", "THROWS", "RuntimeException") {
		t.Errorf("bare `throw e` fabricated a THROWS edge")
	}
}
