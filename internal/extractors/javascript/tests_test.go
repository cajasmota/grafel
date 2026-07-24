package javascript_test

// tests_test.go — coverage for the TS/JS TESTS-edge emission (#1726).
//
// Verifies that the JS extractor, when handed a test file, emits a TESTS
// edge from every Operation entity to each non-stopword callee in its
// CALLS edges. Non-test files must NOT receive any TESTS edges from this
// pass (only the file-to-file PLATFORM_VARIANT / TESTS edges from #713
// continue to fire there).

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// collectKindEdges returns the list of ToIDs for relationships of the
// given Kind emitted on any Operation entity matching `fromOpName`. When
// `fromOpName` is empty, every Operation entity's edges are returned.
func collectKindEdges(ents []types.EntityRecord, fromOpName, kind string) []string {
	out := []string{}
	for i := range ents {
		ent := ents[i]
		if ent.Kind != "SCOPE.Operation" {
			continue
		}
		if fromOpName != "" && ent.Name != fromOpName {
			continue
		}
		for _, r := range ent.Relationships {
			if string(r.Kind) == kind {
				out = append(out, r.ToID)
			}
		}
	}
	return out
}

// containsAll reports whether every element of `want` appears at least
// once in `got`. Used so we can assert key TESTS targets are present
// without pinning the full set (filter rules may add/remove fringe
// targets over time).
func containsAll(got, want []string) bool {
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestTestsEdge_DotTestSuffix_TS verifies the canonical case: a TS test
// file named foo.test.ts with an it() block that calls a production
// function emits a TESTS edge to that function.
func TestTestsEdge_DotTestSuffix_TS(t *testing.T) {
	src := `import { getUser } from './user';

function runUserTest() {
  const u = getUser(1);
  return u;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/user.test.ts")

	got := collectKindEdges(ents, "runUserTest", "TESTS")
	if len(got) == 0 {
		t.Fatalf("expected TESTS edges from runUserTest; got none. all ops=%v",
			collectKindEdges(ents, "", "TESTS"))
	}
	// Issue #2646: CALLS edges to relative imports now carry a structural ref
	// (scope:operation:ref:<lang>:<file>:<name>) so the corresponding TESTS
	// edges mirror that form. Accept both the bare name and the structural ref.
	found := false
	for _, g := range got {
		if g == "getUser" || strings.HasSuffix(g, ":getUser") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TESTS edges missing getUser (bare or structural ref); got %v", got)
	}
}

// TestTestsEdge_DotSpecSuffix_JSX verifies the .spec.jsx convention.
func TestTestsEdge_DotSpecSuffix_JSX(t *testing.T) {
	src := `function checkButtonRender() {
  renderButton();
  return true;
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "javascript", tree, "components/Button.spec.jsx")

	got := collectKindEdges(ents, "checkButtonRender", "TESTS")
	if !containsAll(got, []string{"renderButton"}) {
		t.Errorf("expected TESTS edge to renderButton; got %v", got)
	}
}

// TestTestsEdge_TestsDirectory verifies files under tests/ get TESTS edges
// even when they don't carry the .test/.spec filename suffix.
func TestTestsEdge_TestsDirectory(t *testing.T) {
	src := `function exerciseFlow() {
  startCheckout();
  finalizeOrder();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "tests/checkout-flow.ts")

	got := collectKindEdges(ents, "exerciseFlow", "TESTS")
	if !containsAll(got, []string{"startCheckout", "finalizeOrder"}) {
		t.Errorf("expected TESTS edges for startCheckout+finalizeOrder; got %v", got)
	}
}

// TestTestsEdge_DunderTestsDirectory verifies the __tests__/ convention.
func TestTestsEdge_DunderTestsDirectory(t *testing.T) {
	src := `function pingService() {
  callService();
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "javascript", tree, "src/__tests__/service.js")

	got := collectKindEdges(ents, "pingService", "TESTS")
	if !containsAll(got, []string{"callService"}) {
		t.Errorf("expected TESTS edge to callService; got %v", got)
	}
}

// TestTestsEdge_NonTestFile_NoEdges verifies non-test files do NOT get
// per-operation TESTS edges (only the existing file-level PLATFORM_VARIANT
// /TESTS edges from #713 continue to fire).
func TestTestsEdge_NonTestFile_NoEdges(t *testing.T) {
	src := `import { getUser } from './user';

function fetchProfile() {
  const u = getUser(1);
  return u;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/profile.ts")

	got := collectKindEdges(ents, "fetchProfile", "TESTS")
	if len(got) != 0 {
		t.Errorf("non-test file should not emit Operation-level TESTS edges; got %v", got)
	}
}

// TestTestsEdge_FiltersStopwords verifies that test-scaffolding calls
// (expect, jest.fn, beforeEach, …) do NOT show up as TESTS edge targets.
func TestTestsEdge_FiltersStopwords(t *testing.T) {
	src := `import { getUser } from './user';

function harness() {
  beforeEach();
  const m = jest.fn();
  expect(getUser(1)).toBeDefined();
  return m;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/user.test.ts")

	got := collectKindEdges(ents, "harness", "TESTS")
	for _, target := range got {
		low := strings.ToLower(target)
		if low == "beforeeach" || low == "jest.fn" || low == "expect" ||
			strings.HasSuffix(low, ".tobedefined") {
			t.Errorf("stopword %q leaked into TESTS edges; full set=%v", target, got)
		}
	}
}

// TestTestsEdge_FiltersUserMocks verifies that calls to user-defined
// helpers named mockXxx are filtered out (heuristic from the JS-side
// stopword filter).
func TestTestsEdge_FiltersUserMocks(t *testing.T) {
	src := `function harness() {
  const fake = mockUserService();
  const real = getRealUser();
  return { fake, real };
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "javascript", tree, "src/svc.test.js")

	got := collectKindEdges(ents, "harness", "TESTS")
	for _, target := range got {
		if strings.HasPrefix(strings.ToLower(target), "mock") {
			t.Errorf("mock-prefixed helper %q leaked; full set=%v", target, got)
		}
	}
	// getRealUser SHOULD survive — it's a normal production call.
	if !containsAll(got, []string{"getRealUser"}) {
		t.Errorf("expected getRealUser in TESTS edges; got %v", got)
	}
}

// TestTestsEdge_CallsEdgesPreserved verifies the CALLS edges remain in
// place — TESTS is ADDED, not a replacement.
func TestTestsEdge_CallsEdgesPreserved(t *testing.T) {
	src := `function runUserTest() {
  const u = getUser(1);
  return u;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/user.test.ts")

	calls := collectKindEdges(ents, "runUserTest", "CALLS")
	tests := collectKindEdges(ents, "runUserTest", "TESTS")
	if !containsAll(calls, []string{"getUser"}) {
		t.Errorf("CALLS edge to getUser was lost; got %v", calls)
	}
	if !containsAll(tests, []string{"getUser"}) {
		t.Errorf("TESTS edge to getUser missing; got %v", tests)
	}
}

// TestTestsEdge_FrameworkPropertyStamped verifies the framework
// metadata is recorded on the emitted TESTS edges so downstream
// consumers (UI filters, reports) can distinguish jest/cypress/etc.
func TestTestsEdge_FrameworkPropertyStamped(t *testing.T) {
	src := `function runTest() {
  doWork();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/widget.test.ts")

	var seen string
	for i := range ents {
		ent := ents[i]
		if ent.Kind != "SCOPE.Operation" || ent.Name != "runTest" {
			continue
		}
		for _, r := range ent.Relationships {
			if string(r.Kind) != "TESTS" {
				continue
			}
			if r.Properties != nil {
				seen = r.Properties.Get("test_framework")
			}
		}
	}
	if seen != "jest" {
		t.Errorf("expected test_framework=jest on TESTS edge; got %q", seen)
	}
}

// TestTestsEdge_MethodInsideClass verifies a class method in a test file
// also gets its CALLS promoted to TESTS edges (covers React Testing
// Library / Enzyme suites where tests live inside an exported helper
// class).
func TestTestsEdge_MethodInsideClass(t *testing.T) {
	src := `class UserSuite {
  runScenario() {
    fetchUser();
    persistResult();
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/user.test.ts")

	got := collectKindEdges(ents, "runScenario", "TESTS")
	if !containsAll(got, []string{"fetchUser", "persistResult"}) {
		t.Errorf("class method TESTS edges missing fetchUser/persistResult; got %v", got)
	}
}

// TestTestsEdge_SpecMjsExtension verifies the .mjs/.cjs extensions are
// recognised (Node ESM/CJS test runners).
func TestTestsEdge_SpecMjsExtension(t *testing.T) {
	src := `function runIt() {
  doThing();
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJSPath(t, src, "javascript", tree, "src/thing.spec.mjs")

	got := collectKindEdges(ents, "runIt", "TESTS")
	if !containsAll(got, []string{"doThing"}) {
		t.Errorf("expected .spec.mjs to produce TESTS edges; got %v", got)
	}
}
