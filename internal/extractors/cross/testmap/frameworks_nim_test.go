// Package testmap — value-asserting tests for the Nim std/unittest detector and
// the Nim indentation block-body extractor (#4749).
package testmap

import (
	"testing"
)

// TestNimUnittest_DirectCallHighConfidence proves a direct production call
// inside a `test "...":` body yields a high-confidence TESTS edge, attributed to
// the nim-unittest framework.
func TestNimUnittest_DirectCallHighConfidence(t *testing.T) {
	src := `
import std/unittest
import user

suite "User":
  test "creates a user":
    let u = createUser("ada")
    check u.name == "ada"
`
	recs := runExtract(t, "tests/tUser.nim", "nim", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for nim-unittest")
	}
	rec := findByTested(t, recs, "creates_a_user", "createUser")
	if rec.Properties["test_framework"] != "nim-unittest" {
		t.Errorf("framework=%q, want nim-unittest", rec.Properties["test_framework"])
	}
	if !hasEdge(recs, "creates_a_user", "createUser") {
		t.Errorf("missing TESTS edge creates_a_user -> createUser")
	}
}

// TestNimUnittest_BlockBodyScoped proves the indentation block-body extractor
// only scans the test's own body — a call in a SIBLING test must not leak into
// another test case's body.
func TestNimUnittest_BlockBodyScoped(t *testing.T) {
	src := `
import std/unittest
import svc

suite "Svc":
  test "alpha":
    let a = runAlpha()
    check a
  test "beta":
    let b = runBeta()
    check b
`
	recs := runExtract(t, "tests/tSvc.nim", "nim", src)
	if !hasEdgeAny(recs, "alpha", "runAlpha") {
		t.Errorf("expected alpha -> runAlpha edge")
	}
	if !hasEdgeAny(recs, "beta", "runBeta") {
		t.Errorf("expected beta -> runBeta edge")
	}
	// runBeta must NOT appear in the alpha test body (block-body scoping).
	if hasEdgeAny(recs, "alpha", "runBeta") {
		t.Errorf("runBeta leaked into the alpha test body — block-body scoping failed")
	}
}

// TestNimUnittest_NonTestFileNoop proves a plain production file (no unittest
// import, no test convention) is not detected as a test file.
func TestNimUnittest_NonTestFileNoop(t *testing.T) {
	src := `
proc createUser(name: string): User =
  result = User(name: name)
`
	recs := runExtract(t, "src/user.nim", "nim", src)
	if len(recs) != 0 {
		t.Fatalf("expected no testmap entities for a production file, got %d", len(recs))
	}
}

// TestNimTestament_SpecHeaderFileLevel proves a testament-style Nim test file
// (a `discard """ … """` spec header, asserting via top-level doAssert rather
// than `test "…"` cases) is recognised as one file-level test, with its
// production call linked (#5367).
func TestNimTestament_SpecHeaderFileLevel(t *testing.T) {
	src := `discard """
  output: "ok"
  description: "validate parseConfig"
"""

import config

let cfg = parseConfig("app.cfg")
doAssert cfg.name == "app"
`
	recs := runExtract(t, "tests/tparser.nim", "nim", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for a testament file")
	}
	if !hasEdgeAny(recs, "validate_parseConfig", "parseConfig") {
		t.Errorf("missing TESTS edge validate_parseConfig -> parseConfig")
	}
}

// TestNimTestament_NoSpecNoFallback proves a non-test file without a testament
// spec or unittest case is not spuriously detected.
func TestNimTestament_NoSpecNoFallback(t *testing.T) {
	src := `import config
let cfg = parseConfig("app.cfg")
echo cfg.name
`
	recs := runExtract(t, "tests/tparser.nim", "nim", src)
	if len(recs) != 0 {
		t.Fatalf("expected no testmap entities without a spec/test, got %d", len(recs))
	}
}
