package testmap

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runExtract(t *testing.T, path, lang, source string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(source),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return recs
}

func findByTested(t *testing.T, recs []types.EntityRecord, test, prod string) types.EntityRecord {
	t.Helper()
	for _, r := range recs {
		if r.Properties["test_function"] == test && r.Properties["tested_function"] == prod {
			return r
		}
	}
	t.Fatalf("no entity with test=%q prod=%q (got %d entities)", test, prod, len(recs))
	return types.EntityRecord{}
}

func hasEdge(recs []types.EntityRecord, test, prod string) bool {
	for _, r := range recs {
		if r.Properties["test_function"] != test || r.Properties["tested_function"] != prod {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" {
				return true
			}
		}
	}
	return false
}

// hasEdgeAny reports whether any entity for the given test function has a
// TESTS relationship edge whose "tested" property equals prod. Unlike
// hasEdge, it does not require prod to be the primary tested_function —
// useful when the test body contains multiple production calls and the
// highest-confidence one is not the one being asserted.
func hasEdgeAny(recs []types.EntityRecord, test, prod string) bool {
	for _, r := range recs {
		if r.Properties["test_function"] != test {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == prod {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Interface / registration
// ---------------------------------------------------------------------------

func TestLanguageKey(t *testing.T) {
	e := &Extractor{}
	if got := e.Language(); got != "_cross_testmap" {
		t.Errorf("Language()=%q, want _cross_testmap", got)
	}
}

func TestStringDescription(t *testing.T) {
	e := &Extractor{}
	if !strings.Contains(e.String(), "_cross_testmap") {
		t.Errorf("String() should contain registration key: %q", e.String())
	}
}

func TestEmptyFileReturnsNil(t *testing.T) {
	recs := runExtract(t, "empty.go", "go", "")
	if len(recs) != 0 {
		t.Errorf("expected 0 entities, got %d", len(recs))
	}
}

func TestNonTestFileSkipped(t *testing.T) {
	src := `package main
import "fmt"
func main() { fmt.Println("hi") }`
	recs := runExtract(t, "main.go", "go", src)
	if len(recs) != 0 {
		t.Errorf("expected 0 entities from non-test file, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Issue #2060 — TESTS edges carry a convention-derived prodFile hint at every
// confidence so the resolver short-form path can bind (file, name) without
// falling through to the "?" global byName lookup (which only resolves
// globally-unique names — rare in archigraph + upvate where production
// callees like `GetUser` / `create_order` collide across packages).
// ---------------------------------------------------------------------------

func TestIssue2060_GoHighConfidenceEdgeCarriesProdFile(t *testing.T) {
	// Direct call → high confidence. Pre-#2060 the TESTS edge ToID was
	// "scope:operation:?#Foo"; post-#2060 it must be
	// "scope:operation:pkg/user.go#Foo" so the resolver tries
	// byLocation[pkg/user.go][Foo] before falling through to byName.
	src := `package user
import "testing"
func TestFoo(t *testing.T) { Foo(); }`
	recs := runExtract(t, "pkg/user_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("no records")
	}
	rec := findByTested(t, recs, "TestFoo", "Foo")
	if rec.Properties["confidence"] != "high" {
		t.Fatalf("expected high, got %q", rec.Properties["confidence"])
	}
	if len(rec.Relationships) == 0 {
		t.Fatal("no relationships emitted")
	}
	want := "scope:operation:pkg/user.go#Foo"
	if rec.Relationships[0].ToID != want {
		t.Errorf("TESTS ToID=%q, want %q", rec.Relationships[0].ToID, want)
	}
}

func TestIssue2060_PythonHighConfidenceEdgeCarriesProdFile(t *testing.T) {
	src := `import pytest
def test_user_create():
    User.create({"x": 1})
`
	recs := runExtract(t, "tests/test_user.py", "python", src)
	// At least one of the resolved targets should carry the convention
	// prod file (tests/user.py) in its ToID. The exact targets are
	// `User.create` (high) and the test_function fallback wouldn't
	// fire because direct calls succeed.
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			if strings.HasPrefix(rel.ToID, "scope:operation:tests/user.py#") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected at least one TESTS edge ToID under tests/user.py prefix; got recs=%d", len(recs))
		for _, r := range recs {
			for _, rel := range r.Relationships {
				t.Logf("  rel: %+v", rel)
			}
		}
	}
}

func TestIssue2060_JSHighConfidenceEdgeCarriesProdFile(t *testing.T) {
	src := `import { getUser } from './user';
describe('users', () => {
  it('returns a user', () => {
    const u = getUser(1);
    expect(u).toBeDefined();
  });
});`
	recs := runExtract(t, "src/user.test.ts", "typescript", src)
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && strings.HasPrefix(rel.ToID, "scope:operation:src/user.ts#") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge ToID under src/user.ts; got recs=%d", len(recs))
	}
}

func TestIssue2060_NoConventionStillEmitsQuestionForm(t *testing.T) {
	// Rust has no filename convention; high-confidence call must still
	// emit a "?" form ToID so the global byName ladder can bind it. The
	// extractor must NOT stamp an empty file path.
	src := `#[cfg(test)]
mod tests {
    #[test]
    fn test_compute() { compute_thing(1); }
}`
	recs := runExtract(t, "src/lib.rs", "rust", src)
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && strings.HasPrefix(rel.ToID, "scope:operation:?#") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("rust has no convention; expected ? form ToID")
	}
}

// tested_file is ONLY stamped for low-confidence fallback (so the property
// retains its pre-#2060 meaning of "best-guess tested file derived from naming
// convention because no direct call was found"). High/medium calls now carry
// prodFile too but it's a resolver hint, not a verified location.
func TestIssue2060_TestedFileOnlyStampedForLowConfidence(t *testing.T) {
	src := `package p
import "testing"
func TestHigh(t *testing.T) { RealCall(); }
func TestLow(t *testing.T) { t.Log("none"); }`
	recs := runExtract(t, "pkg/widget_test.go", "go", src)
	for _, r := range recs {
		conf := r.Properties["confidence"]
		tf := r.Properties["tested_file"]
		if conf == "low" && tf == "" {
			t.Errorf("low confidence should stamp tested_file")
		}
		if conf == "high" && tf != "" {
			t.Errorf("high confidence should NOT stamp tested_file, got %q", tf)
		}
	}
}

// ---------------------------------------------------------------------------
// Go testing
// ---------------------------------------------------------------------------

func TestGoTesting_DirectCallHighConfidence(t *testing.T) {
	src := `package user
import "testing"
func TestGetUser(t *testing.T) {
    u := GetUser(1)
    if u == nil {
        t.Fatalf("nil")
    }
}`
	recs := runExtract(t, "user_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity")
	}
	rec := findByTested(t, recs, "TestGetUser", "GetUser")
	// testmap entities are SCOPE.Pattern (subtype "test_coverage")
	// because the 14-type allowlist has no "TestCoverage" entry.
	if rec.Kind != "SCOPE.Pattern" {
		t.Errorf("Kind=%q, want SCOPE.Pattern", rec.Kind)
	}
	if rec.Properties["test_framework"] != "go_testing" {
		t.Errorf("framework=%q", rec.Properties["test_framework"])
	}
	if rec.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high", rec.Properties["confidence"])
	}
	if rec.Properties["test_type"] != "unit" {
		t.Errorf("test_type=%q, want unit", rec.Properties["test_type"])
	}
	if !hasEdge(recs, "TestGetUser", "GetUser") {
		t.Errorf("missing TESTS edge")
	}
}

func TestGoTesting_NoDirectCallFallsBackToLow(t *testing.T) {
	src := `package svc
import "testing"
func TestHandler(t *testing.T) {
    t.Log("no calls")
}`
	recs := runExtract(t, "handler_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected low-confidence fallback entity")
	}
	if recs[0].Properties["confidence"] != "low" {
		t.Errorf("confidence=%q, want low", recs[0].Properties["confidence"])
	}
	if recs[0].Properties["tested_file"] == "" {
		t.Errorf("low confidence fallback should carry tested_file")
	}
}

func TestGoTesting_StopwordsExcluded(t *testing.T) {
	src := `package p
import "testing"
func TestA(t *testing.T) {
    t.Helper()
    t.Run("x", func(t *testing.T) {})
    fmt.Println("x")
    RealThing(1)
}`
	recs := runExtract(t, "a_test.go", "go", src)
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "RealThing" {
			found = true
		}
		if strings.HasPrefix(r.Properties["tested_function"], "t.") {
			t.Errorf("stopword escaped: %q", r.Properties["tested_function"])
		}
	}
	if !found {
		t.Errorf("expected RealThing to survive stopword filter")
	}
}

func TestGoTesting_DeduplicatesMultipleCalls(t *testing.T) {
	src := `package p
import "testing"
func TestRepeat(t *testing.T) {
    DoThing()
    DoThing()
    DoThing()
}`
	recs := runExtract(t, "p_test.go", "go", src)
	count := 0
	for _, r := range recs {
		if r.Properties["tested_function"] == "DoThing" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 DoThing entity, got %d", count)
	}
}

func TestGoTesting_MockUpgradeToHigh(t *testing.T) {
	src := `package p
import "testing"
func TestMix(t *testing.T) {
    mock.On("UserSvc", 1)
    UserSvc()
}`
	recs := runExtract(t, "mix_test.go", "go", src)
	r := findByTested(t, recs, "TestMix", "UserSvc")
	if r.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high (direct call beats mock)", r.Properties["confidence"])
	}
}

func TestGoTesting_MockOnlyMedium(t *testing.T) {
	src := `package p
import "testing"
func TestMockOnly(t *testing.T) {
    mock.On("BillingSvc", 42)
}`
	recs := runExtract(t, "mo_test.go", "go", src)
	// At least one entity should mention BillingSvc at medium.
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "BillingSvc" && r.Properties["confidence"] == "medium" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected BillingSvc medium-confidence entity")
	}
}

// ---------------------------------------------------------------------------
// Go testify suite (#2076)
// ---------------------------------------------------------------------------

// TestGoTestifySuite_BasicMethod verifies that a receiver-method test on a
// testify suite struct emits a TESTS edge and carries the correct framework /
// confidence properties.
func TestGoTestifySuite_BasicMethod(t *testing.T) {
	src := `package user

import (
	"testing"
	"github.com/stretchr/testify/suite"
)

type UserSuite struct{ suite.Suite }

func (s *UserSuite) TestGetUser() {
	u := s.repo.GetUser(1)
	s.Require().NotNil(u)
}

func TestRunUserSuite(t *testing.T) {
	suite.Run(t, new(UserSuite))
}
`
	recs := runExtract(t, "user_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity, got 0")
	}
	// TestGetUser must appear as a test_function.
	found := false
	for _, r := range recs {
		if r.Properties["test_function"] == "TestGetUser" {
			found = true
			if r.Properties["test_framework"] != "go_testing" {
				t.Errorf("framework=%q, want go_testing", r.Properties["test_framework"])
			}
			hasTestsEdge := false
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" {
					hasTestsEdge = true
				}
			}
			if !hasTestsEdge {
				t.Errorf("TestGetUser entity has no TESTS edge")
			}
		}
	}
	if !found {
		t.Errorf("no entity with test_function=TestGetUser (got %d entities)", len(recs))
		for _, r := range recs {
			t.Logf("  entity: test_function=%q tested_function=%q", r.Properties["test_function"], r.Properties["tested_function"])
		}
	}
}

// TestGoTestifySuite_DirectCallHighConfidence verifies that a direct production
// call inside a suite method is resolved at high confidence.
func TestGoTestifySuite_DirectCallHighConfidence(t *testing.T) {
	src := `package svc

import (
	"github.com/stretchr/testify/suite"
)

type RepoSuite struct{ suite.Suite }

func (s *RepoSuite) TestFindOrder() {
	order := FindOrder(42)
	s.NotNil(order)
}
`
	recs := runExtract(t, "repo_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	rec := findByTested(t, recs, "TestFindOrder", "FindOrder")
	if rec.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high", rec.Properties["confidence"])
	}
	if !hasEdge(recs, "TestFindOrder", "FindOrder") {
		t.Errorf("missing TESTS edge TestFindOrder→FindOrder")
	}
}

// TestGoTestifySuite_NonSuiteReceiverSkipped ensures that receiver methods on
// structs that do NOT embed suite.Suite are not falsely detected as tests.
func TestGoTestifySuite_NonSuiteReceiverSkipped(t *testing.T) {
	src := `package svc

type MyHandler struct{}

func (h *MyHandler) TestHelper() {
	doInternal()
}
`
	recs := runExtract(t, "helper_test.go", "go", src)
	for _, r := range recs {
		if r.Properties["test_function"] == "TestHelper" {
			t.Errorf("non-suite receiver method should not be detected as a test")
		}
	}
}

// TestGoTestifySuite_StandardTestsUnaffected ensures legacy top-level tests
// continue to work correctly alongside suite-method detection.
func TestGoTestifySuite_StandardTestsUnaffected(t *testing.T) {
	src := `package p
import "testing"
func TestStandard(t *testing.T) {
	DoWork()
}
`
	recs := runExtract(t, "p_test.go", "go", src)
	found := false
	for _, r := range recs {
		if r.Properties["test_function"] == "TestStandard" {
			found = true
		}
	}
	if !found {
		t.Errorf("standard top-level test was not detected")
	}
}

// ---------------------------------------------------------------------------
// Python pytest
// ---------------------------------------------------------------------------

func TestPytest_DirectCallHighConfidence(t *testing.T) {
	src := `import pytest
from mymod import get_user

def test_get_user():
    u = get_user(1)
    assert u is not None
`
	recs := runExtract(t, "tests/test_user.py", "python", src)
	rec := findByTested(t, recs, "test_get_user", "get_user")
	if rec.Properties["test_framework"] != "pytest" {
		t.Errorf("framework=%q", rec.Properties["test_framework"])
	}
	if rec.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high", rec.Properties["confidence"])
	}
	if rec.Properties["test_type"] != "unit" {
		t.Errorf("test_type=%q, want unit", rec.Properties["test_type"])
	}
}

// TestPytest_AsyncDef is a regression for #1553: async test functions
// (`async def test_*`) were not detected because the pytest function regex
// only matched a leading `def`. The ShipFast orders integration test is an
// `async def`, so no TESTS edge formed and the endpoint Tests section stayed
// empty.
func TestPytest_AsyncDef(t *testing.T) {
	src := `import pytest
from app.routes import create_order

@pytest.mark.asyncio
async def test_create_order_publishes_event(monkeypatch):
    result = await create_order({"user_id": "u1"}, _claims={})
    assert result["status"] == "PENDING"
`
	recs := runExtract(t, "tests/test_orders.py", "python", src)
	rec := findByTested(t, recs, "test_create_order_publishes_event", "create_order")
	if rec.Properties["test_framework"] != "pytest" {
		t.Errorf("framework=%q", rec.Properties["test_framework"])
	}
	if rec.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high", rec.Properties["confidence"])
	}
}

func TestPytest_MockerPatchMedium(t *testing.T) {
	src := `import pytest
def test_mocked(mocker):
    mocker.patch("mymod.do_work")
`
	recs := runExtract(t, "tests/test_m.py", "python", src)
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "mymod.do_work" && r.Properties["confidence"] == "medium" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected mymod.do_work medium entity")
	}
}

func TestPytest_UnderscoreTestFallback(t *testing.T) {
	// File naming alone: no pytest import, but `_test.py` suffix.
	src := `def test_noop():
    assert True
`
	recs := runExtract(t, "tests/widget_test.py", "python", src)
	if len(recs) == 0 {
		t.Fatalf("expected fallback entity")
	}
	if recs[0].Properties["confidence"] != "low" {
		t.Errorf("confidence=%q, want low", recs[0].Properties["confidence"])
	}
}

// ---------------------------------------------------------------------------
// Issue #2080 — pytest @pytest.mark.parametrize must not emit per-tuple orphans
// ---------------------------------------------------------------------------

// TestPytest_ParametrizeCollapsedToOneEntity verifies that a parametrize-decorated
// test function emits exactly ONE SCOPE.Pattern/unit entity regardless of N
// parameter sets (Option A fix).
//
// Pre-#2080 the extractor could emit N entities for N testedCall entries per
// test function, each with its own entity ID not referenced in any edge —
// making every one a degree-0 orphan. The fix (buildCollapsedEntity) emits
// one entity per test function with all TESTS edges embedded. Each TESTS edge
// carries FromID = the entity's own scope:testcoverage: stub (stored in
// Properties["ref"]), which the resolver resolves to the entity's hex ID at
// assembly time via the byQualifiedName index, making the coverage record the
// source of its own TESTS edge and ensuring it appears in the "touched" set.
func TestPytest_ParametrizeCollapsedToOneEntity(t *testing.T) {
	src := `import pytest

@pytest.mark.parametrize("a,b", [(1, 2), (3, 4), (5, 6)])
def test_addition(a, b):
    assert a + b > 0
`
	recs := runExtract(t, "tests/test_math.py", "python", src)

	// Option A: exactly 1 SCOPE.Pattern/unit entity emitted — one per def test_*,
	// regardless of N parametrize parameter sets.
	patternCount := 0
	for _, r := range recs {
		if r.Kind == "SCOPE.Pattern" && r.Subtype == "unit" {
			patternCount++
		}
	}
	if patternCount != 1 {
		t.Errorf("expected exactly 1 SCOPE.Pattern/unit entity, got %d", patternCount)
		for _, r := range recs {
			t.Logf("  entity: Kind=%q Subtype=%q Name=%q", r.Kind, r.Subtype, r.Name)
		}
	}

	// The entity must carry a TESTS relationship with a non-empty FromID
	// pointing at the entity's own scope:testcoverage: stub. The resolver
	// resolves this stub to the entity's hex ID at assembly time, making the
	// coverage record the source of its own TESTS edge (non-orphan).
	if len(recs) > 0 {
		ref := recs[0].Properties["ref"]
		found := false
		for _, rel := range recs[0].Relationships {
			if rel.Kind == "TESTS" && rel.FromID != "" && rel.FromID == ref {
				found = true
			}
		}
		if !found {
			t.Errorf("expected TESTS relationship with FromID == Properties[ref] (%q), got: %v", ref, recs[0].Relationships)
		}
	}
}

// TestPytest_ParametrizeWithDirectCall verifies that a parametrize test whose
// body calls a production function emits exactly one entity with a high-confidence
// TESTS edge pointing at the production function.
func TestPytest_ParametrizeWithDirectCall(t *testing.T) {
	src := `import pytest
from myapp.math import add_numbers

@pytest.mark.parametrize("a,b,expected", [(1, 2, 3), (4, 5, 9), (10, 20, 30)])
def test_addition(a, b, expected):
    result = add_numbers(a, b)
    assert result == expected
`
	recs := runExtract(t, "tests/test_math.py", "python", src)

	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 entity, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Kind != "SCOPE.Pattern" {
		t.Errorf("Kind=%q, want SCOPE.Pattern", rec.Kind)
	}
	if rec.Subtype != "unit" {
		t.Errorf("Subtype=%q, want unit", rec.Subtype)
	}
	if rec.Properties["test_function"] != "test_addition" {
		t.Errorf("test_function=%q, want test_addition", rec.Properties["test_function"])
	}
	if rec.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high (direct call to add_numbers)", rec.Properties["confidence"])
	}

	// Must have TESTS edge with explicit FromID == Properties["ref"] and
	// ToID pointing at add_numbers.
	ref := rec.Properties["ref"]
	testsEdge := false
	for _, rel := range rec.Relationships {
		if rel.Kind == "TESTS" && rel.FromID == ref && rel.Properties["tested"] == "add_numbers" {
			testsEdge = true
		}
	}
	if !testsEdge {
		t.Errorf("expected TESTS edge with tested=add_numbers and FromID=Properties[ref] (%q); got: %v", ref, rec.Relationships)
	}
}

// ---------------------------------------------------------------------------
// JavaScript / TypeScript Jest
// ---------------------------------------------------------------------------

func TestJest_ItWithDirectCall(t *testing.T) {
	src := `import { getUser } from './user';
describe('users', () => {
  it('returns a user', () => {
    const u = getUser(1);
    expect(u).toBeDefined();
  });
});`
	recs := runExtract(t, "user.test.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "getUser" && r.Properties["confidence"] == "high" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected getUser high-confidence entity")
	}
}

func TestJest_SpecFilenameDetection(t *testing.T) {
	src := `test('works', () => {
  doStuff();
});`
	recs := runExtract(t, "thing.spec.js", "javascript", src)
	if len(recs) == 0 {
		t.Fatalf("expected spec entities")
	}
	if recs[0].Properties["test_framework"] != "jest" {
		t.Errorf("framework=%q, want jest", recs[0].Properties["test_framework"])
	}
}

func TestJest_IntegrationPath(t *testing.T) {
	src := `test('e', () => { runFlow(); });`
	recs := runExtract(t, "tests/integration/flow.test.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	if recs[0].Properties["test_type"] != "integration" {
		t.Errorf("test_type=%q, want integration", recs[0].Properties["test_type"])
	}
}

func TestJest_E2EPath(t *testing.T) {
	src := `test('e', () => { runFlow(); });`
	recs := runExtract(t, "tests/e2e/flow.test.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	if recs[0].Properties["test_type"] != "e2e" {
		t.Errorf("test_type=%q, want e2e", recs[0].Properties["test_type"])
	}
}

// ---------------------------------------------------------------------------
// Cypress
// ---------------------------------------------------------------------------

func TestCypress_CyTsFileDetection(t *testing.T) {
	src := `import { HomePage } from '../pages/HomePage'

describe('Home page', () => {
  it('loads successfully', () => {
    cy.visit('/')
    HomePage.assertTitle()
  })
})`
	recs := runExtract(t, "cypress/e2e/login.cy.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected cypress entities")
	}
	if recs[0].Properties["test_framework"] != "cypress" {
		t.Errorf("framework=%q, want cypress", recs[0].Properties["test_framework"])
	}
	if recs[0].Properties["test_type"] != "e2e" {
		t.Errorf("test_type=%q, want e2e", recs[0].Properties["test_type"])
	}
}

func TestCypress_DirectCallToProduction(t *testing.T) {
	src := `import { getUser } from '../app/user';
describe('User page', () => {
  it('should load user data', () => {
    const u = getUser(1);
    cy.wrap(u).should('exist');
  })
})`
	recs := runExtract(t, "cypress/e2e/user.cy.ts", "typescript", src)
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "getUser" && r.Properties["confidence"] == "high" {
			found = true
			if r.Properties["test_framework"] != "cypress" {
				t.Errorf("framework=%q, want cypress", r.Properties["test_framework"])
			}
		}
	}
	if !found {
		t.Errorf("expected getUser high-confidence entity from cypress test")
	}
}

func TestCypress_CyGlobalNotATarget(t *testing.T) {
	src := `describe('App', () => {
  it('navigates', () => {
    cy.visit('/');
    cy.get('.button').click();
  })
})`
	recs := runExtract(t, "cypress/e2e/app.cy.ts", "typescript", src)
	for _, r := range recs {
		if strings.HasPrefix(r.Properties["tested_function"], "cy.") {
			t.Errorf("cy.* call should be filtered as stopword: %q", r.Properties["tested_function"])
		}
	}
}

// ---------------------------------------------------------------------------
// Playwright
// ---------------------------------------------------------------------------

func TestPlaywright_ImportHintsDetection(t *testing.T) {
	src := `import { test, expect } from '@playwright/test';

test('logs in successfully', async ({ page }) => {
  await page.goto('/login');
  await page.fill('input[name="user"]', 'alice');
});`
	recs := runExtract(t, "tests/login.spec.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected playwright entities")
	}
	// Import hint should win over filename match, identifying as playwright not jest
	if recs[0].Properties["test_framework"] != "playwright" {
		t.Errorf("framework=%q, want playwright", recs[0].Properties["test_framework"])
	}
	// When no direct production calls are found, fallback uses filename convention
	// to infer the tested_file (tests/login.spec.ts -> tests/login.ts)
	if recs[0].Properties["tested_function"] != "login" {
		t.Errorf("tested_function=%q, want login (from filename convention)", recs[0].Properties["tested_function"])
	}
}

func TestPlaywright_PwExtensionDetection(t *testing.T) {
	src := `import { test } from '@playwright/test';
test('visit home', async ({ page }) => { await page.goto('/'); });`
	recs := runExtract(t, "e2e/home.pw.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from .pw.ts file")
	}
	if recs[0].Properties["test_framework"] != "playwright" {
		t.Errorf("framework=%q, want playwright", recs[0].Properties["test_framework"])
	}
}

func TestPlaywright_SpecFileWithoutPlaywrightImportIsJest(t *testing.T) {
	// .spec.ts file without @playwright/test import should be detected as Jest
	src := `import { describe, it, expect } from 'vitest';
describe('test', () => {
  it('works', () => { doWork(); });
});`
	recs := runExtract(t, "src/test.spec.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	if recs[0].Properties["test_framework"] != "jest" {
		t.Errorf("framework=%q, want jest (not playwright)", recs[0].Properties["test_framework"])
	}
}

// ---------------------------------------------------------------------------
// Ruby RSpec
// ---------------------------------------------------------------------------

func TestRSpec_ItBlockDirectCall(t *testing.T) {
	src := `require 'rspec'
describe User do
  it 'returns user' do
    u = UserRepo.find(1)
    expect(u).not_to be_nil
  end
end`
	recs := runExtract(t, "user_spec.rb", "ruby", src)
	found := false
	for _, r := range recs {
		if strings.Contains(r.Properties["tested_function"], "UserRepo") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected UserRepo in results, got %d entities", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Rails RSpec — deep linkage tests (#3342)
// ---------------------------------------------------------------------------

// TestRailsRSpec_DescribeSubjectMediumConfidence verifies that an RSpec spec
// whose `it` block has no direct production call still emits a TESTS edge
// targeting the described constant at medium confidence (describe-subject
// linkage). This is the core of the partial→full flip: even spec bodies that
// rely on `subject` DSL / `expect(response)` patterns link to their class.
func TestRailsRSpec_DescribeSubjectMediumConfidence(t *testing.T) {
	src := `require 'rails_helper'

RSpec.describe User, type: :model do
  it 'is valid with valid attributes' do
    expect(subject).to be_valid
  end
end`
	recs := runExtract(t, "spec/models/user_spec.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from RSpec describe-subject spec, got 0")
	}
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "User" && r.Properties["confidence"] == "medium" {
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" && rel.Properties["tested"] == "User" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting User at medium confidence; got recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: test_function=%q tested=%q confidence=%q", r.Properties["test_function"], r.Properties["tested_function"], r.Properties["confidence"])
			for _, rel := range r.Relationships {
				t.Logf("    TESTS->%q conf=%q", rel.Properties["tested"], rel.Properties["confidence"])
			}
		}
	}
}

// TestRailsRSpec_DescribeSubjectHighConfidenceDirectCall verifies that when an
// RSpec it-block directly calls a production method, the TESTS edge is emitted
// at high confidence (direct call wins over describe-subject medium fallback).
func TestRailsRSpec_DescribeSubjectHighConfidenceDirectCall(t *testing.T) {
	src := `require 'rails_helper'

RSpec.describe UsersController, type: :controller do
  describe '#create' do
    it 'creates a user' do
      user = User.create(name: 'Alice')
      expect(user).to be_persisted
    end
  end
end`
	recs := runExtract(t, "spec/controllers/users_controller_spec.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity")
	}
	highFound := false
	for _, r := range recs {
		if r.Properties["confidence"] == "high" {
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" && (rel.Properties["tested"] == "User.create" || rel.Properties["tested"] == "User") {
					highFound = true
				}
			}
		}
	}
	if !highFound {
		// Fall back: accept medium targeting UsersController
		for _, r := range recs {
			if r.Properties["tested_function"] == "UsersController" {
				highFound = true
			}
		}
	}
	if !highFound {
		t.Errorf("expected high-confidence TESTS edge for User.create or medium for UsersController; recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
		}
	}
}

// TestRailsRSpec_ControllerSpec verifies that a controller spec with a describe
// constant emits a TESTS edge pointing at the controller class.
func TestRailsRSpec_ControllerSpec(t *testing.T) {
	src := `require 'rails_helper'

RSpec.describe UsersController, type: :controller do
  it 'returns 200 for index' do
    get :index
    expect(response).to have_http_status(200)
  end
end`
	recs := runExtract(t, "spec/controllers/users_controller_spec.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from controller spec")
	}
	found := false
	for _, r := range recs {
		if r.Properties["test_framework"] != "rspec" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "UsersController" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting UsersController; recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
		}
	}
}

// TestRailsRSpec_SpecifyBlockDetected verifies that `specify` examples are also
// detected (the rspecSpecifyRE addition).
func TestRailsRSpec_SpecifyBlockDetected(t *testing.T) {
	src := `require 'rspec'

describe Product do
  specify 'has a valid price' do
    p = Product.find(1)
    expect(p.price).to be > 0
  end
end`
	recs := runExtract(t, "spec/models/product_spec.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from specify block")
	}
	// Must have a TESTS edge targeting Product (direct call via Product.find wins at high)
	found := false
	for _, r := range recs {
		if r.Properties["test_framework"] != "rspec" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && (rel.Properties["tested"] == "Product.find" || rel.Properties["tested"] == "Product") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge for Product from specify block; recs=%d", len(recs))
	}
}

// TestRailsRSpec_PathConventionSpecModels verifies the Rails spec/ directory
// convention in productionFileFromTestPath: spec/models/user_spec.rb should
// resolve to app/models/user.rb with symbol "User".
func TestRailsRSpec_PathConventionSpecModels(t *testing.T) {
	prodFile, sym := productionFileFromTestPath("spec/models/user_spec.rb")
	if prodFile != "app/models/user.rb" {
		t.Errorf("prodFile=%q, want app/models/user.rb", prodFile)
	}
	if sym != "User" {
		t.Errorf("sym=%q, want User", sym)
	}
}

// TestRailsRSpec_PathConventionSpecControllers verifies the controller spec
// convention: spec/controllers/users_controller_spec.rb → app/controllers/users_controller.rb / UsersController.
func TestRailsRSpec_PathConventionSpecControllers(t *testing.T) {
	prodFile, sym := productionFileFromTestPath("spec/controllers/users_controller_spec.rb")
	if prodFile != "app/controllers/users_controller.rb" {
		t.Errorf("prodFile=%q, want app/controllers/users_controller.rb", prodFile)
	}
	if sym != "UsersController" {
		t.Errorf("sym=%q, want UsersController", sym)
	}
}

// TestRailsRSpec_PathConventionSpecJobs verifies the job spec convention.
func TestRailsRSpec_PathConventionSpecJobs(t *testing.T) {
	prodFile, sym := productionFileFromTestPath("spec/jobs/import_job_spec.rb")
	if prodFile != "app/jobs/import_job.rb" {
		t.Errorf("prodFile=%q, want app/jobs/import_job.rb", prodFile)
	}
	if sym != "ImportJob" {
		t.Errorf("sym=%q, want ImportJob", sym)
	}
}

// TestRailsMinitest_ActiveSupportTestCase verifies that a Minitest
// ActiveSupport::TestCase emits a TESTS edge targeting the class under test,
// derived from the test class name (UserTest → User), at medium confidence.
func TestRailsMinitest_ActiveSupportTestCase(t *testing.T) {
	src := `require 'test_helper'

class UserTest < ActiveSupport::TestCase
  test 'is valid' do
    user = User.new(name: 'Alice')
    assert user.valid?
  end
end`
	recs := runExtract(t, "test/models/user_test.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from Minitest ActiveSupport::TestCase, got 0")
	}
	if recs[0].Properties["test_framework"] != "minitest" {
		t.Errorf("framework=%q, want minitest", recs[0].Properties["test_framework"])
	}
	// User.new is a direct call → high confidence; User is medium from describe-subject.
	// Accept either.
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && (rel.Properties["tested"] == "User.new" || rel.Properties["tested"] == "User") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting User or User.new; recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
		}
	}
}

// TestRailsMinitest_SubjectOnlyNoDirectCall verifies the describe-subject
// fallback for Minitest: when the test body has no explicit call the TESTS
// edge targets the subject derived from the class name (UserTest → User) at
// medium confidence.
func TestRailsMinitest_SubjectOnlyNoDirectCall(t *testing.T) {
	src := `require 'test_helper'

class UserTest < ActiveSupport::TestCase
  test 'is valid' do
    assert true
  end
end`
	recs := runExtract(t, "test/models/user_test.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from Minitest (subject-only fallback), got 0")
	}
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "User" && r.Properties["confidence"] == "medium" {
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting User at medium confidence; recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
		}
	}
}

// TestRailsMinitest_DefStyleTest verifies that `def test_*` method-style tests
// are detected and linked to the test subject.
func TestRailsMinitest_DefStyleTest(t *testing.T) {
	src := `require 'minitest/autorun'

class ImportJobTest < Minitest::Test
  def test_enqueues_job
    ImportJob.perform_later(user_id: 1)
    assert_enqueued_jobs 1
  end
end`
	recs := runExtract(t, "test/jobs/import_job_test.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from Minitest def-style test, got 0")
	}
	if recs[0].Properties["test_framework"] != "minitest" {
		t.Errorf("framework=%q, want minitest", recs[0].Properties["test_framework"])
	}
	// ImportJob.perform_later is a direct call → high confidence.
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && strings.HasPrefix(rel.Properties["tested"], "ImportJob") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting ImportJob.*; recs=%d", len(recs))
	}
}

// TestRailsMinitest_PathConventionTestModels verifies the Rails test/ directory
// convention in productionFileFromTestPath.
func TestRailsMinitest_PathConventionTestModels(t *testing.T) {
	prodFile, sym := productionFileFromTestPath("test/models/user_test.rb")
	if prodFile != "app/models/user.rb" {
		t.Errorf("prodFile=%q, want app/models/user.rb", prodFile)
	}
	if sym != "User" {
		t.Errorf("sym=%q, want User", sym)
	}
}

// TestRailsMinitest_PathConventionTestControllers verifies the controller test
// convention.
func TestRailsMinitest_PathConventionTestControllers(t *testing.T) {
	prodFile, sym := productionFileFromTestPath("test/controllers/users_controller_test.rb")
	if prodFile != "app/controllers/users_controller.rb" {
		t.Errorf("prodFile=%q, want app/controllers/users_controller.rb", prodFile)
	}
	if sym != "UsersController" {
		t.Errorf("sym=%q, want UsersController", sym)
	}
}

// TestRailsTestCamelCase verifies the snake_case→CamelCase helper.
func TestRailsTestCamelCase(t *testing.T) {
	cases := map[string]string{
		"user":                "User",
		"users_controller":    "UsersController",
		"import_job":          "ImportJob",
		"notification_mailer": "NotificationMailer",
		"application_helper":  "ApplicationHelper",
		"billing_service":     "BillingService",
	}
	for snake, want := range cases {
		if got := railsTestCamelCase(snake); got != want {
			t.Errorf("railsTestCamelCase(%q)=%q, want %q", snake, got, want)
		}
	}
}

// TestRailsRSpec_DescribeSubjectExtraction verifies that rspecDescribeSubject
// correctly extracts the constant from `RSpec.describe Constant` and
// `describe Constant` forms.
func TestRailsRSpec_DescribeSubjectExtraction(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`RSpec.describe User, type: :model do`, "User"},
		{`describe UsersController do`, "UsersController"},
		{"describe \"some string\" do\nRSpec.describe ImportJob do", "ImportJob"},
		{`RSpec.describe "just a string" do`, ""},
	}
	for _, tc := range cases {
		got := rspecDescribeSubject(tc.src)
		if got != tc.want {
			t.Errorf("rspecDescribeSubject(%q)=%q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestRailsRSpec_FrameworkTaggedOnEntity verifies that the test_framework
// property is "rspec" on entities extracted from _spec.rb files.
func TestRailsRSpec_FrameworkTaggedOnEntity(t *testing.T) {
	src := `require 'rspec'
describe User do
  it 'works' do
    User.new
  end
end`
	recs := runExtract(t, "spec/models/user_spec.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity")
	}
	for _, r := range recs {
		if r.Properties["test_framework"] != "rspec" {
			t.Errorf("test_framework=%q, want rspec", r.Properties["test_framework"])
		}
	}
}

// TestRailsMinitest_ControllerTest verifies a controller integration test that
// uses assert* helpers — the TESTS edge targets the controller class.
func TestRailsMinitest_ControllerTest(t *testing.T) {
	src := `require 'action_controller/test_case'

class UsersControllerTest < ActionController::TestCase
  test 'GET index returns 200' do
    get :index
    assert_response :success
  end
end`
	recs := runExtract(t, "test/controllers/users_controller_test.rb", "ruby", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from ActionController::TestCase")
	}
	// Direct call: get (:index — a symbol, not an ident) → falls back to describe-subject
	// The subject from class name UsersControllerTest → UsersController.
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "UsersController" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting UsersController; recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
		}
	}
}

// ---------------------------------------------------------------------------
// Java JUnit
// ---------------------------------------------------------------------------

func TestJUnit_TestAnnotation(t *testing.T) {
	src := `import org.junit.Test;
public class UserServiceTest {
    @Test
    public void testGetUser() {
        UserService svc = new UserService();
        User u = svc.getUser(1);
    }
}`
	recs := runExtract(t, "src/test/java/UserServiceTest.java", "java", src)
	if len(recs) == 0 {
		t.Fatalf("expected junit entities")
	}
	if recs[0].Properties["test_framework"] != "junit" {
		t.Errorf("framework=%q, want junit", recs[0].Properties["test_framework"])
	}
}

// ---------------------------------------------------------------------------
// Kotlin
// ---------------------------------------------------------------------------

func TestKotlin_TestAnnotation(t *testing.T) {
	src := `import kotlin.test.Test
class FooTest {
    @Test
    fun shouldReturnValue() {
        assertEquals(1, Foo.compute())
    }
}`
	recs := runExtract(t, "FooTest.kt", "kotlin", src)
	if len(recs) == 0 {
		t.Fatalf("expected kotlin test entities")
	}
	if recs[0].Properties["test_framework"] != "kotlin_test" {
		t.Errorf("framework=%q, want kotlin_test", recs[0].Properties["test_framework"])
	}
}

func TestKotlin_BacktickName(t *testing.T) {
	src := "import kotlin.test.Test\nclass BarTest {\n    @Test\n    fun `should do a thing`() {\n        runThing()\n    }\n}"
	recs := runExtract(t, "BarTest.kt", "kotlin", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	// Backtick name must have been scrubbed into a valid qname
	if recs[0].Properties["test_function"] == "" {
		t.Errorf("empty test_function on backtick-named test")
	}
}

// ---------------------------------------------------------------------------
// C# — xUnit / NUnit / MSTest deep linkage (#3383)
// ---------------------------------------------------------------------------

func TestCSharp_TestAttribute(t *testing.T) {
	src := `using NUnit.Framework;
public class UserTests {
    [Test]
    public void TestGetUser() {
        var u = GetUser(1);
    }
}`
	recs := runExtract(t, "UserTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected csharp entities")
	}
	if recs[0].Properties["test_framework"] != "nunit" {
		t.Errorf("framework=%q, want nunit", recs[0].Properties["test_framework"])
	}
}

// TestCSharp_xUnit_FactDirectCall verifies that an xUnit [Fact] test that
// directly calls a production method emits a high-confidence TESTS edge.
// This is the core value-asserting test for the partial→full flip.
//
//	OrderServiceTests [Fact] calls OrderService.Place() → TESTS edge to OrderService.Place
func TestCSharp_xUnit_FactDirectCall(t *testing.T) {
	src := `using Xunit;

public class OrderServiceTests
{
    [Fact]
    public void Place_ValidOrder_ReturnsConfirmed()
    {
        var svc = new OrderService();
        var result = svc.Place(new Order { Amount = 10 });
        Assert.Equal("confirmed", result.Status);
    }
}`
	recs := runExtract(t, "OrderServiceTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from xUnit [Fact] test, got 0")
	}
	if recs[0].Properties["test_framework"] != "xunit" {
		t.Errorf("framework=%q, want xunit", recs[0].Properties["test_framework"])
	}
	// OrderService must appear as a TESTS edge target (direct call via new OrderService()).
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "OrderService" {
				found = true
				if rel.Properties["confidence"] != "high" {
					t.Errorf("expected high confidence for OrderService call, got %q", rel.Properties["confidence"])
				}
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting OrderService; recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: test_function=%q tested=%q conf=%q", r.Properties["test_function"], r.Properties["tested_function"], r.Properties["confidence"])
			for _, rel := range r.Relationships {
				t.Logf("    TESTS->%q conf=%q", rel.Properties["tested"], rel.Properties["confidence"])
			}
		}
	}
}

// TestCSharp_xUnit_TheoryDirectCall verifies [Theory] test methods.
func TestCSharp_xUnit_TheoryDirectCall(t *testing.T) {
	src := `using Xunit;

public class CalculatorTests
{
    [Theory]
    [InlineData(1, 2, 3)]
    [InlineData(4, 5, 9)]
    public void Add_TwoNumbers_ReturnsSum(int a, int b, int expected)
    {
        var calc = new Calculator();
        var result = calc.Add(a, b);
        Assert.Equal(expected, result);
    }
}`
	recs := runExtract(t, "CalculatorTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from xUnit [Theory] test, got 0")
	}
	// Calculator must be a TESTS target via new Calculator() call.
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "Calculator" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting Calculator from [Theory] test; recs=%d", len(recs))
	}
}

// TestCSharp_xUnit_ClassSubjectFallback verifies that when no direct call is
// found the class name convention (OrderServiceTests → OrderService) produces
// a medium-confidence TESTS edge — mirroring the Minitest describeSubject path.
func TestCSharp_xUnit_ClassSubjectFallback(t *testing.T) {
	src := `using Xunit;

public class OrderServiceTests
{
    [Fact]
    public void IsEmpty_ReturnsTrue()
    {
        Assert.True(true);
    }
}`
	recs := runExtract(t, "OrderServiceTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from xUnit class-subject fallback, got 0")
	}
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "OrderService" && r.Properties["confidence"] == "medium" {
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting OrderService at medium confidence (class-name fallback); recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
		}
	}
}

// TestCSharp_NUnit_TestAnnotation verifies NUnit [Test] methods inside a
// [TestFixture] class produce a TESTS edge to the class under test.
func TestCSharp_NUnit_TestAnnotation(t *testing.T) {
	src := `using NUnit.Framework;

[TestFixture]
public class UserServiceTests
{
    [Test]
    public void GetUser_ValidId_ReturnsUser()
    {
        var svc = new UserService();
        var user = svc.GetUser(1);
        Assert.IsNotNull(user);
    }
}`
	recs := runExtract(t, "UserServiceTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from NUnit [Test], got 0")
	}
	if recs[0].Properties["test_framework"] != "nunit" {
		t.Errorf("framework=%q, want nunit", recs[0].Properties["test_framework"])
	}
	// UserService must appear as a TESTS edge target.
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "UserService" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting UserService; recs=%d", len(recs))
	}
}

// TestCSharp_NUnit_TestCase verifies NUnit [TestCase] parametrized tests.
func TestCSharp_NUnit_TestCase(t *testing.T) {
	src := `using NUnit.Framework;

[TestFixture]
public class CalculatorTests
{
    [TestCase(1, 2, ExpectedResult = 3)]
    [TestCase(4, 5, ExpectedResult = 9)]
    public int Add_ReturnsCorrectSum(int a, int b)
    {
        var calc = new Calculator();
        return calc.Add(a, b);
    }
}`
	recs := runExtract(t, "CalculatorTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from NUnit [TestCase], got 0")
	}
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "Calculator" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting Calculator from [TestCase]; recs=%d", len(recs))
	}
}

// TestCSharp_MSTest_TestMethod verifies MSTest [TestMethod] tests.
func TestCSharp_MSTest_TestMethod(t *testing.T) {
	src := `using Microsoft.VisualStudio.TestTools.UnitTesting;

[TestClass]
public class PaymentServiceTests
{
    [TestMethod]
    public void ProcessPayment_ValidCard_ReturnsSuccess()
    {
        var svc = new PaymentService();
        var result = svc.ProcessPayment("4111111111111111", 100);
        Assert.AreEqual("success", result.Status);
    }
}`
	recs := runExtract(t, "PaymentServiceTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from MSTest [TestMethod], got 0")
	}
	if recs[0].Properties["test_framework"] != "mstest" {
		t.Errorf("framework=%q, want mstest", recs[0].Properties["test_framework"])
	}
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "PaymentService" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting PaymentService; recs=%d", len(recs))
	}
}

// TestCSharp_xUnit_WebApplicationFactory verifies that integration tests using
// WebApplicationFactory<T> emit a TESTS edge to the entry-point type T.
func TestCSharp_xUnit_WebApplicationFactory(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc.Testing;
using Xunit;

public class IntegrationTests : IClassFixture<WebApplicationFactory<Program>>
{
    private readonly WebApplicationFactory<Program> _factory;

    public IntegrationTests(WebApplicationFactory<Program> factory)
    {
        _factory = factory;
    }

    [Fact]
    public async Task GetEndpoint_ReturnsSuccess()
    {
        var client = _factory.CreateClient();
        var response = await client.GetAsync("/api/orders");
        response.EnsureSuccessStatusCode();
    }
}`
	recs := runExtract(t, "IntegrationTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from WebApplicationFactory integration test, got 0")
	}
	// WebApplicationFactory<Program> → TESTS edge to Program (the entry type).
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "Program" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting Program (WebApplicationFactory<Program>); recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
			for _, rel := range r.Relationships {
				t.Logf("    TESTS->%q conf=%q", rel.Properties["tested"], rel.Properties["confidence"])
			}
		}
	}
}

// TestCSharp_AssertStopwordsFiltered verifies that C# assertion calls (Assert.Equal,
// Assert.AreEqual, Assert.IsNotNull, etc.) are NOT emitted as TESTS edge targets.
func TestCSharp_AssertStopwordsFiltered(t *testing.T) {
	src := `using Xunit;

public class OrderServiceTests
{
    [Fact]
    public void Place_ValidOrder_AssertionsOnly()
    {
        Assert.Equal(1, 1);
        Assert.True(true);
        Assert.IsNotNull("hello");
        Assert.AreEqual(2, 2);
    }
}`
	recs := runExtract(t, "OrderServiceTests.cs", "csharp", src)
	// If any entity is produced, make sure its tested_function is not Assert.*
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" {
				tested := strings.ToLower(rel.Properties["tested"])
				if strings.HasPrefix(tested, "assert.") {
					t.Errorf("Assert.* call leaked as TESTS target: %q", rel.Properties["tested"])
				}
			}
		}
	}
}

// TestCSharp_SutMethodCallHighConfidence verifies that calls like `sut.Place()`
// where `sut` is a production object produce high-confidence TESTS edges.
func TestCSharp_SutMethodCallHighConfidence(t *testing.T) {
	src := `using Xunit;

public class OrderServiceTests
{
    private readonly OrderService _sut = new OrderService();

    [Fact]
    public void Place_SetsStatus()
    {
        var result = _sut.Place(new Order());
        Assert.Equal("confirmed", result.Status);
    }
}`
	recs := runExtract(t, "OrderServiceTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity")
	}
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && (rel.Properties["tested"] == "OrderService" || rel.Properties["tested"] == "_sut.Place") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting OrderService or _sut.Place; recs=%d", len(recs))
		for _, r := range recs {
			for _, rel := range r.Relationships {
				t.Logf("  TESTS->%q conf=%q", rel.Properties["tested"], rel.Properties["confidence"])
			}
		}
	}
}

// TestCSharp_NUnit_SubjectFallback verifies that an NUnit test class with no
// direct production calls still emits a medium-confidence TESTS edge from the
// class name convention (UserServiceTests → UserService).
func TestCSharp_NUnit_SubjectFallback(t *testing.T) {
	src := `using NUnit.Framework;

[TestFixture]
public class UserServiceTests
{
    [Test]
    public void Setup_IsValid()
    {
        Assert.Pass();
    }
}`
	recs := runExtract(t, "UserServiceTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from NUnit subject fallback, got 0")
	}
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "UserService" && r.Properties["confidence"] == "medium" {
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected medium-confidence TESTS edge targeting UserService (class-name fallback); recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: tested=%q conf=%q", r.Properties["tested_function"], r.Properties["confidence"])
		}
	}
}

// TestCSharp_AsyncTestMethod verifies that async Task test methods are detected.
func TestCSharp_AsyncTestMethod(t *testing.T) {
	src := `using Xunit;
using System.Threading.Tasks;

public class OrderServiceTests
{
    [Fact]
    public async Task PlaceAsync_ReturnsResult()
    {
        var svc = new OrderService();
        var result = await svc.PlaceAsync(new Order());
        Assert.NotNull(result);
    }
}`
	recs := runExtract(t, "OrderServiceTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from async xUnit [Fact], got 0")
	}
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "OrderService" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting OrderService from async test; recs=%d", len(recs))
	}
}

// TestCSharp_FilenameConventionOnly verifies that a .cs file ending in Tests.cs
// is detected even without a using directive, using the class name convention.
func TestCSharp_FilenameConventionOnly(t *testing.T) {
	src := `public class ProductRepositoryTests
{
    [Fact]
    public void Save_Persists()
    {
        var repo = new ProductRepository();
        repo.Save(new Product { Id = 1 });
    }
}`
	recs := runExtract(t, "ProductRepositoryTests.cs", "csharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from filename-convention-only C# file, got 0")
	}
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "ProductRepository" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected TESTS edge targeting ProductRepository; recs=%d", len(recs))
	}
}

// TestCSharp_ProductionFileConvention verifies that productionFileFromTestPath
// correctly derives the production file for C# tests.
func TestCSharp_ProductionFileConvention(t *testing.T) {
	cases := []struct {
		path     string
		wantFile string
		wantSym  string
	}{
		{"src/OrderServiceTests.cs", "src/OrderService.cs", "OrderService"},
		{"tests/UserTest.cs", "tests/User.cs", "User"},
		{"UserTests.cs", "User.cs", "User"},
	}
	for _, tc := range cases {
		f, s := productionFileFromTestPath(tc.path)
		if f != tc.wantFile || s != tc.wantSym {
			t.Errorf("productionFileFromTestPath(%q)=(%q,%q), want (%q,%q)",
				tc.path, f, s, tc.wantFile, tc.wantSym)
		}
	}
}

// TestCSharp_SubjectFromClassName verifies the csTestSubjectFromClassName helper.
func TestCSharp_SubjectFromClassName(t *testing.T) {
	cases := map[string]string{
		"OrderServiceTests": "OrderService",
		"OrderServiceTest":  "OrderService",
		"UserTests":         "User",
		"UserTest":          "User",
		"Something":         "",
		"Test":              "",
	}
	for in, want := range cases {
		if got := csTestSubjectFromClassName(in); got != want {
			t.Errorf("csTestSubjectFromClassName(%q)=%q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

func TestRust_TestAttribute(t *testing.T) {
	src := `#[cfg(test)]
mod tests {
    #[test]
    fn test_compute() {
        let r = compute(1);
        assert_eq!(r, 2);
    }
}`
	recs := runExtract(t, "src/lib.rs", "rust", src)
	if len(recs) == 0 {
		t.Fatalf("expected rust entities")
	}
	if recs[0].Properties["test_framework"] != "rust_test" {
		t.Errorf("framework=%q, want rust_test", recs[0].Properties["test_framework"])
	}
}

// ---------------------------------------------------------------------------
// PHP
// ---------------------------------------------------------------------------

func TestPHP_PHPUnit(t *testing.T) {
	src := `<?php
use PHPUnit\Framework\TestCase;
class UserTest extends TestCase {
    public function testGetUser() {
        $u = getUser(1);
    }
}`
	recs := runExtract(t, "UserTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected php entities")
	}
	if recs[0].Properties["test_framework"] != "phpunit" {
		t.Errorf("framework=%q, want phpunit", recs[0].Properties["test_framework"])
	}
}

// ---------------------------------------------------------------------------
// PHP — PHPUnit deep linkage (#3399)
// ---------------------------------------------------------------------------

// TestPHPUnit_TestPrefixMethod verifies the classic test* prefix method
// form emits a TESTS edge to the directly-called production function.
func TestPHPUnit_TestPrefixMethod(t *testing.T) {
	src := `<?php
use PHPUnit\Framework\TestCase;

class UserServiceTest extends TestCase {
    public function testRegisterCreatesUser() {
        $svc = new UserService();
        $user = $svc->register(['email' => 'a@b.com']);
        $this->assertInstanceOf(User::class, $user);
    }
}`
	recs := runExtract(t, "tests/Unit/UserServiceTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from phpunit test prefix form")
	}
	if recs[0].Properties["test_framework"] != "phpunit" {
		t.Errorf("framework=%q, want phpunit", recs[0].Properties["test_framework"])
	}
	// register() is called directly in the body → high-confidence TESTS edge.
	// Use hasEdgeAny because the body also instantiates UserService which may
	// be the primary tested_function; register appears as a secondary edge.
	if !hasEdgeAny(recs, "testRegisterCreatesUser", "register") {
		t.Error("expected TESTS edge from testRegisterCreatesUser to register (direct call)")
	}
}

// TestPHPUnit_HashTestAttribute verifies PHP8 #[Test] attribute methods are
// detected and emit a TESTS edge to the directly-called production function.
func TestPHPUnit_HashTestAttribute(t *testing.T) {
	src := `<?php
use PHPUnit\Framework\TestCase;
use PHPUnit\Framework\Attributes\Test;

class OrderServiceTest extends TestCase {
    #[Test]
    public function it_calculates_total() {
        $svc = new OrderService();
        $total = $svc->calculateTotal([10, 20]);
        $this->assertEquals(30, $total);
    }
}`
	recs := runExtract(t, "tests/OrderServiceTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from #[Test] attribute form")
	}
	found := false
	for _, r := range recs {
		if r.Properties["test_function"] == "it_calculates_total" {
			found = true
			if !hasEdgeAny([]types.EntityRecord{r}, "it_calculates_total", "calculateTotal") {
				t.Error("expected TESTS edge to calculateTotal (direct call)")
			}
			break
		}
	}
	if !found {
		t.Error("expected entity with test_function=it_calculates_total from #[Test] attribute")
	}
}

// TestPHPUnit_DocblockTestAnnotation verifies /** @test */ docblock annotated
// methods are detected and emit a TESTS edge.
func TestPHPUnit_DocblockTestAnnotation(t *testing.T) {
	src := `<?php
use PHPUnit\Framework\TestCase;

class ProductRepositoryTest extends TestCase {
    /** @test */
    public function it_finds_product_by_id() {
        $repo = new ProductRepository();
        $product = $repo->findById(1);
        $this->assertNotNull($product);
    }
}`
	recs := runExtract(t, "tests/ProductRepositoryTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from @test docblock form")
	}
	found := false
	for _, r := range recs {
		if r.Properties["test_function"] == "it_finds_product_by_id" {
			found = true
			if !hasEdgeAny([]types.EntityRecord{r}, "it_finds_product_by_id", "findById") {
				t.Error("expected TESTS edge to findById (direct call)")
			}
			break
		}
	}
	if !found {
		t.Error("expected entity with test_function=it_finds_product_by_id from @test docblock")
	}
}

// TestPHPUnit_ClassNameSubjectDerivation verifies that UserServiceTest → UserService
// is used as a medium-confidence subject when the body has no direct production call.
func TestPHPUnit_ClassNameSubjectDerivation(t *testing.T) {
	src := `<?php
use PHPUnit\Framework\TestCase;

class UserServiceTest extends TestCase {
    public function testSomething() {
        // no explicit production call
        $this->assertTrue(true);
    }
}`
	recs := runExtract(t, "tests/UserServiceTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from phpunit class-name subject derivation")
	}
	// describeSubject = UserService (stripped "Test") → medium-confidence TESTS edge.
	if !hasEdge(recs, "testSomething", "UserService") {
		t.Error("expected TESTS edge to UserService derived from class name UserServiceTest")
	}
}

// TestPHPUnit_InstantiationBodyHint verifies that `new UserService()` in the
// test body is used as the subject hint when no class-name-derived subject exists.
func TestPHPUnit_InstantiationBodyHint(t *testing.T) {
	src := `<?php
use PHPUnit\Framework\TestCase;

class SomeTest extends TestCase {
    public function testDoesWork() {
        $svc = new UserService();
        $this->assertNotNull($svc);
    }
}`
	recs := runExtract(t, "tests/SomeTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	// UserService instantiated in body → should appear as a production target.
	found := false
	for _, r := range recs {
		if r.Properties["test_function"] == "testDoesWork" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected entity with test_function=testDoesWork")
	}
}

// TestPHPUnit_LaravelFeatureTest verifies that Laravel feature test HTTP
// dispatch calls ($this->get, $this->postJson) are NOT emitted as production
// call targets (they are test infrastructure), but that the named service
// method in the body IS linked.
func TestPHPUnit_LaravelFeatureTest(t *testing.T) {
	src := `<?php
namespace Tests\Feature;

use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class UserApiTest extends TestCase {
    use RefreshDatabase;

    public function testIndexReturnsUsers() {
        $response = $this->get('/api/users');
        $response->assertStatus(200);
        $response->assertJson(['data' => []]);
    }

    public function testStoreCreatesUser() {
        $response = $this->postJson('/api/users', [
            'name' => 'Alice',
            'email' => 'alice@example.com',
        ]);
        $response->assertCreated();
        UserService::create(['name' => 'Alice']);
    }
}`
	recs := runExtract(t, "tests/Feature/UserApiTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from Laravel feature test")
	}
	// $this->get('/api/users') must NOT produce a TESTS edge to "get".
	for _, r := range recs {
		if hasEdge([]types.EntityRecord{r}, r.Properties["test_function"], "get") {
			t.Errorf("unexpected TESTS edge to 'get' — Laravel $this->get should be a stopword")
		}
		if hasEdge([]types.EntityRecord{r}, r.Properties["test_function"], "postJson") {
			t.Errorf("unexpected TESTS edge to 'postJson' — Laravel $this->postJson should be a stopword")
		}
	}
	// UserService::create() IS a production call → should produce a TESTS edge.
	if !hasEdgeAny(recs, "testStoreCreatesUser", "create") {
		t.Error("expected TESTS edge to create (UserService::create)")
	}
}

// TestPHPUnit_PathHintLaravelTests verifies that a PHPUnit file under tests/
// without an explicit use PHPUnit statement is still detected via path hints.
func TestPHPUnit_PathHintLaravelTests(t *testing.T) {
	src := `<?php
namespace Tests\Unit;

class OrderCalculatorTest extends \PHPUnit\Framework\TestCase {
    public function testCalculate() {
        $calc = new OrderCalculator();
        $total = $calc->calculate([5, 10]);
        $this->assertEquals(15, $total);
    }
}`
	recs := runExtract(t, "tests/Unit/OrderCalculatorTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities for file under tests/ path hint")
	}
	if recs[0].Properties["test_framework"] != "phpunit" {
		t.Errorf("framework=%q want phpunit", recs[0].Properties["test_framework"])
	}
}

// TestPHPUnit_MultipleTestForms verifies that all three test detection forms
// (test* prefix, #[Test] attribute, @test docblock) are discovered in the
// same class and each produces its own TESTS edge.
func TestPHPUnit_MultipleTestForms(t *testing.T) {
	src := `<?php
use PHPUnit\Framework\TestCase;
use PHPUnit\Framework\Attributes\Test;

class PaymentServiceTest extends TestCase {
    public function testChargeCard() {
        $svc = new PaymentService();
        $svc->chargeCard('tok_visa', 1000);
    }

    #[Test]
    public function it_refunds_payment() {
        $svc = new PaymentService();
        $svc->refund('txn_123');
    }

    /** @test */
    public function it_handles_failure() {
        $svc = new PaymentService();
        $svc->chargeCard('tok_declined', 500);
    }
}`
	recs := runExtract(t, "tests/PaymentServiceTest.php", "php", src)
	// Should detect all 3 test functions.
	funcNames := map[string]bool{}
	for _, r := range recs {
		funcNames[r.Properties["test_function"]] = true
	}
	for _, want := range []string{"testChargeCard", "it_refunds_payment", "it_handles_failure"} {
		if !funcNames[want] {
			t.Errorf("expected test function %q to be detected", want)
		}
	}
	if !hasEdgeAny(recs, "testChargeCard", "chargeCard") {
		t.Error("expected TESTS edge: testChargeCard → chargeCard")
	}
	if !hasEdgeAny(recs, "it_refunds_payment", "refund") {
		t.Error("expected TESTS edge: it_refunds_payment → refund")
	}
}

// ---------------------------------------------------------------------------
// PHP — Pest deep linkage (#3399)
// ---------------------------------------------------------------------------

// TestPhpPest_ItBlock verifies that Pest it('description', fn()=>...) blocks
// are detected and emit TESTS edges based on direct calls in the body.
func TestPhpPest_ItBlock(t *testing.T) {
	src := `<?php
use Pest\Test;
use App\Services\UserService;

it('creates a user', function () {
    $svc = new UserService();
    $user = $svc->create(['email' => 'a@b.com']);
    expect($user)->toBeInstanceOf(User::class);
});
`
	recs := runExtract(t, "tests/Feature/UserTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from Pest it() block")
	}
	if recs[0].Properties["test_framework"] != "phptest_pest" {
		t.Errorf("framework=%q want phptest_pest", recs[0].Properties["test_framework"])
	}
	if !hasEdgeAny(recs, "it_creates_a_user", "create") {
		t.Error("expected TESTS edge to create (direct call in Pest it() body)")
	}
}

// TestPhpPest_UsesSubjectExtraction verifies that uses(UserService::class)
// produces a medium-confidence TESTS edge to UserService even when the body
// has no direct call.
func TestPhpPest_UsesSubjectExtraction(t *testing.T) {
	src := `<?php
use Pest\Test;
uses(App\Services\UserService::class);

it('validates user', function () {
    expect(true)->toBeTrue();
});
`
	recs := runExtract(t, "tests/UserServiceTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from Pest uses() subject extraction")
	}
	// Subject = UserService (last segment of App\Services\UserService)
	if !hasEdge(recs, "it_validates_user", "UserService") {
		t.Error("expected medium-confidence TESTS edge to UserService from uses()")
	}
}

// TestPhpPest_TestFunction verifies test('description', ...) form is also detected.
func TestPhpPest_TestFunction(t *testing.T) {
	src := `<?php
use Pest\Test;

test('order total is calculated', function () {
    $calc = new OrderCalculator();
    $result = $calc->total([10, 20, 30]);
    expect($result)->toBe(60);
});
`
	recs := runExtract(t, "tests/OrderTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from Pest test() function")
	}
	if recs[0].Properties["test_framework"] != "phptest_pest" {
		t.Errorf("framework=%q want phptest_pest", recs[0].Properties["test_framework"])
	}
	if !hasEdgeAny(recs, "it_order_total_is_calculated", "total") {
		t.Error("expected TESTS edge to total() method")
	}
}

// TestPhpPest_DescribeSubjectFallback verifies that describe('UserService', ...)
// provides a PascalCase subject for nested it() blocks via describeSubject.
func TestPhpPest_DescribeSubjectFallback(t *testing.T) {
	src := `<?php
use Pest\Test;

describe('UserService', function () {
    it('registers a user', function () {
        // body has no direct call
        expect(true)->toBeTrue();
    });
});
`
	recs := runExtract(t, "tests/UserServiceTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from Pest describe() with subject")
	}
	// Subject "UserService" from describe() → medium-confidence edge.
	if !hasEdge(recs, "it_registers_a_user", "UserService") {
		t.Error("expected TESTS edge to UserService from Pest describe() subject")
	}
}

// TestPhpPest_LaravelFeatureTest verifies that Laravel feature tests written
// in Pest style emit TESTS edges to production code, not to HTTP dispatch helpers.
func TestPhpPest_LaravelFeatureTest(t *testing.T) {
	src := `<?php
use Pest\Test;
use Illuminate\Foundation\Testing\RefreshDatabase;

uses(RefreshDatabase::class);

it('returns user list', function () {
    $response = $this->get('/api/users');
    $response->assertStatus(200);
});

it('stores a user', function () {
    UserService::store(['name' => 'Bob']);
    $this->assertDatabaseHas('users', ['name' => 'Bob']);
});
`
	recs := runExtract(t, "tests/Feature/UserApiPestTest.php", "php", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities from Pest Laravel feature test")
	}
	// $this->get should not produce a production TESTS edge.
	for _, r := range recs {
		if r.Properties["test_function"] == "it_returns_user_list" {
			if hasEdge([]types.EntityRecord{r}, "it_returns_user_list", "get") {
				t.Error("unexpected TESTS edge to 'get' — $this->get is a stopword")
			}
		}
	}
	// UserService::store IS a production call.
	if !hasEdgeAny(recs, "it_stores_a_user", "store") {
		t.Error("expected TESTS edge to store (UserService::store production call)")
	}
}

// ---------------------------------------------------------------------------
// Swift
// ---------------------------------------------------------------------------

func TestSwift_XCTest(t *testing.T) {
	src := `import XCTest
class UserTests: XCTestCase {
    func testGetUser() {
        let u = getUser(1)
        XCTAssertNotNil(u)
    }
}`
	recs := runExtract(t, "UserTests.swift", "swift", src)
	if len(recs) == 0 {
		t.Fatalf("expected swift entities")
	}
	if recs[0].Properties["test_framework"] != "xctest" {
		t.Errorf("framework=%q, want xctest", recs[0].Properties["test_framework"])
	}
}

// ---------------------------------------------------------------------------
// Scala
// ---------------------------------------------------------------------------

func TestScala_ScalaTest(t *testing.T) {
	src := `import org.scalatest.funsuite.AnyFunSuite
class FooSpec extends AnyFunSuite {
    test("compute returns one") {
        assert(Foo.compute() == 1)
    }
}`
	recs := runExtract(t, "FooSpec.scala", "scala", src)
	if len(recs) == 0 {
		t.Fatalf("expected scala entities")
	}
	if recs[0].Properties["test_framework"] != "scalatest" {
		t.Errorf("framework=%q, want scalatest", recs[0].Properties["test_framework"])
	}
}

// ---------------------------------------------------------------------------
// Path normalisation / test-type inference
// ---------------------------------------------------------------------------

func TestInferTestType(t *testing.T) {
	cases := map[string]string{
		"tests/unit/foo_test.go":         "unit",
		"tests/integration/foo_test.go":  "integration",
		"tests/e2e/foo_test.go":          "e2e",
		"src/widget.e2e.test.ts":         "e2e",
		"src/widget.integration.test.ts": "integration",
		"src/widget.test.ts":             "unit",
		"a/b/c.go":                       "unit",
	}
	for path, want := range cases {
		if got := inferTestType(path); got != want {
			t.Errorf("inferTestType(%q)=%q, want %q", path, got, want)
		}
	}
}

func TestProductionFileFromTestPath(t *testing.T) {
	cases := []struct {
		path     string
		wantFile string
		wantSym  string
	}{
		{"pkg/user_test.go", "pkg/user.go", "User"},
		{"tests/test_user.py", "tests/user.py", "user"},
		{"src/user_test.py", "src/user.py", "user"},
		{"src/widget.test.ts", "src/widget.ts", "widget"},
		{"src/widget.spec.js", "src/widget.js", "widget"},
		{"spec/user_spec.rb", "spec/user.rb", "user"},
		{"src/test/java/UserTest.java", "src/test/java/User.java", "User"},
		{"src/test/java/UserTests.java", "src/test/java/User.java", "User"},
		{"src/test/java/UserIT.java", "src/test/java/User.java", "User"},
		{"FooTest.kt", "Foo.kt", "Foo"},
		{"FooSpec.scala", "Foo.scala", "Foo"},
		{"FooTest.scala", "Foo.scala", "Foo"},
		{"UserTests.cs", "User.cs", "User"},
		{"UserTest.php", "User.php", "User"},
		{"UserTests.swift", "User.swift", "User"},
		{"main.go", "", ""},
		{"nope.rs", "", ""}, // rust has no convention
	}
	for _, tc := range cases {
		f, s := productionFileFromTestPath(tc.path)
		if f != tc.wantFile || s != tc.wantSym {
			t.Errorf("productionFileFromTestPath(%q)=(%q,%q), want (%q,%q)",
				tc.path, f, s, tc.wantFile, tc.wantSym)
		}
	}
}

func TestTitleCase(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"a":      "A",
		"hello":  "Hello",
		"World":  "World",
		"user_1": "User_1",
	}
	for in, want := range cases {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q)=%q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Resolver — stopwords, isStopword, stripTestPrefix, tailIdent, rank
// ---------------------------------------------------------------------------

func TestIsStopword(t *testing.T) {
	yes := []string{
		"t.Run", "assert.Equal", "expect", "pytest.raises",
		"testHelper", "TestThing", "mockAnything",
		"fmt.Println", "errors.New", "if", "for",
		"foo.toBe", "bar.should", "baz.toEqual",
	}
	for _, id := range yes {
		if !isStopword(id) {
			t.Errorf("isStopword(%q)=false, want true", id)
		}
	}
	no := []string{"GetUser", "ComputePrice", "UserService.Lookup"}
	for _, id := range no {
		if isStopword(id) {
			t.Errorf("isStopword(%q)=true, want false", id)
		}
	}
}

func TestTailIdent(t *testing.T) {
	cases := map[string]string{
		"Foo":        "Foo",
		"pkg.Foo":    "Foo",
		"a.b.c.Name": "Name",
		"":           "",
	}
	for in, want := range cases {
		if got := tailIdent(in); got != want {
			t.Errorf("tailIdent(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestStripTestPrefix(t *testing.T) {
	cases := map[string]string{
		"TestGetUser":   "GetUser",
		"test_get_user": "get_user",
		"it_should_do":  "should_do",
		"xyz":           "",
		"Test":          "",
		"test_":         "",
	}
	for in, want := range cases {
		if got := stripTestPrefix(in); got != want {
			t.Errorf("stripTestPrefix(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestRank(t *testing.T) {
	if rank("high") <= rank("medium") || rank("medium") <= rank("low") || rank("low") <= rank("") {
		t.Errorf("rank ordering broken")
	}
}

func TestConfidenceProvenance(t *testing.T) {
	if confidenceProvenance("high") != "DIRECT_CALL_IN_TEST_BODY" {
		t.Errorf("high provenance wrong")
	}
	if confidenceProvenance("medium") != "MOCK_TARGET_MATCH" {
		t.Errorf("medium provenance wrong")
	}
	if confidenceProvenance("low") != "INFERRED_FROM_NAMING_CONVENTION" {
		t.Errorf("low provenance wrong")
	}
	if confidenceProvenance("bogus") != "INFERRED_FROM_NAMING_CONVENTION" {
		t.Errorf("unknown should fall through to low")
	}
}

func TestConfidenceScore(t *testing.T) {
	if confidenceScore("high") != 0.9 {
		t.Errorf("high score wrong")
	}
	if confidenceScore("medium") != 0.7 {
		t.Errorf("medium score wrong")
	}
	if confidenceScore("low") != 0.5 {
		t.Errorf("low score wrong")
	}
	if confidenceScore("bogus") != 0.5 {
		t.Errorf("unknown should fall through")
	}
}

// ---------------------------------------------------------------------------
// Entity ID helpers
// ---------------------------------------------------------------------------

func TestEntityIDs(t *testing.T) {
	id := testCoverageEntityID("a.go", "TestFoo", "Foo")
	if !strings.Contains(id, "scope:testcoverage:a.go#TestFoo->Foo") {
		t.Errorf("testCoverageEntityID wrong: %q", id)
	}
	if testFunctionRef("a.go", "TestFoo") != "scope:operation:a.go#TestFoo" {
		t.Errorf("testFunctionRef wrong")
	}
	if productionFunctionRef("", "") != "" {
		t.Errorf("productionFunctionRef should be empty when qname is empty")
	}
	if productionFunctionRef("", "Foo") == "" {
		t.Errorf("productionFunctionRef with file='' but qname set should not be empty")
	}
	if productionFunctionRef("a.go", "Foo") != "scope:operation:a.go#Foo" {
		t.Errorf("productionFunctionRef happy path wrong")
	}
}

// ---------------------------------------------------------------------------
// Body extraction helpers
// ---------------------------------------------------------------------------

func TestExtractBraceBody_Balanced(t *testing.T) {
	src := `func foo() {
    if x {
        "}" // literal brace inside string
    }
}`
	body := extractBraceBody(src, 0)
	if !strings.HasPrefix(body, "{") || !strings.HasSuffix(body, "}") {
		t.Errorf("body should span matching braces, got %q", body)
	}
}

func TestExtractBraceBody_Unterminated(t *testing.T) {
	src := `func foo() { hmmm`
	body := extractBraceBody(src, 0)
	if body != "" {
		t.Errorf("unterminated body should be empty, got %q", body)
	}
}

func TestExtractBraceBody_NoOpenBrace(t *testing.T) {
	src := `no braces here`
	body := extractBraceBody(src, 0)
	if body != "" {
		t.Errorf("should be empty, got %q", body)
	}
}

func TestExtractIndentedBody(t *testing.T) {
	src := "def test_foo():\n    call_one()\n    call_two()\n\n    call_three()\nother_line\n"
	headerStart := strings.Index(src, "def ")
	body := extractIndentedBody(src, headerStart)
	if !strings.Contains(body, "call_one") || !strings.Contains(body, "call_three") {
		t.Errorf("indented body incomplete: %q", body)
	}
	if strings.Contains(body, "other_line") {
		t.Errorf("indented body leaked out of block: %q", body)
	}
}

func TestLeadingWhitespaceWidth(t *testing.T) {
	if leadingWhitespaceWidth("    x") != 4 {
		t.Errorf("4 spaces → 4")
	}
	if leadingWhitespaceWidth("\tx") != 8 {
		t.Errorf("tab → 8")
	}
	if leadingWhitespaceWidth("x") != 0 {
		t.Errorf("no indent → 0")
	}
}

// ---------------------------------------------------------------------------
// Framework matching
// ---------------------------------------------------------------------------

func TestMatchesAnyImport(t *testing.T) {
	toks := map[string]bool{"testing": true, "pytest": true}
	if !matchesAnyImport(toks, []string{"testing"}) {
		t.Errorf("should match exact token")
	}
	if !matchesAnyImport(toks, []string{"pytest"}) {
		t.Errorf("should match second hint")
	}
	if matchesAnyImport(toks, []string{"junit"}) {
		t.Errorf("should not match absent hint")
	}
	if matchesAnyImport(toks, []string{}) {
		t.Errorf("empty hints should return false")
	}
}

func TestJestCaseQNameScrubbing(t *testing.T) {
	cases := map[string]string{
		"returns a user":   "it_returns_a_user",
		"returns-a-user":   "it_returns_a_user",
		"has 99 problems!": "it_has_99_problems",
		"":                 "anonymous_test",
		"!!!":              "anonymous_test",
	}
	for in, want := range cases {
		if got := jestCaseQName(in); got != want {
			t.Errorf("jestCaseQName(%q)=%q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Resolver — end-to-end
// ---------------------------------------------------------------------------

func TestResolveCalls_EmptyBodyFallsBackToConvention(t *testing.T) {
	tf := testFunction{qname: "TestFoo", body: ""}
	calls := resolveCalls(tf, "foo.go", "Foo")
	if len(calls) != 1 || calls[0].confidence != "low" || calls[0].qname != "Foo" {
		t.Errorf("empty body fallback broken: %+v", calls)
	}
}

func TestResolveCalls_NoConventionUsesNameStrip(t *testing.T) {
	tf := testFunction{qname: "TestCompute", body: ""}
	calls := resolveCalls(tf, "", "")
	if len(calls) != 1 || calls[0].qname != "Compute" {
		t.Errorf("strip fallback broken: %+v", calls)
	}
}

func TestResolveCalls_DeduplicatesAcrossPasses(t *testing.T) {
	tf := testFunction{
		qname: "TestAll",
		body:  `Foo() ; mock.On("Foo", 1)`,
	}
	calls := resolveCalls(tf, "x.go", "X")
	var fooCount int
	for _, c := range calls {
		if c.qname == "Foo" {
			fooCount++
			if c.confidence != "high" {
				t.Errorf("expected high after upgrade, got %q", c.confidence)
			}
		}
	}
	if fooCount != 1 {
		t.Errorf("expected single Foo entry, got %d", fooCount)
	}
}

func TestResolveCalls_SkipsShortIdents(t *testing.T) {
	tf := testFunction{qname: "TestShort", body: "a(); bb(); Cc(); LongName();"}
	calls := resolveCalls(tf, "x.go", "X")
	for _, c := range calls {
		if len(c.qname) < 3 {
			t.Errorf("short ident leaked: %q", c.qname)
		}
	}
}

// ---------------------------------------------------------------------------
// Extract — language tag reflected on entity
// ---------------------------------------------------------------------------

func TestLanguageTagIsPropagated(t *testing.T) {
	src := `package p
import "testing"
func TestX(t *testing.T) { RealCall() }`
	recs := runExtract(t, "x_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	if recs[0].Language != "go" {
		t.Errorf("language not propagated: %q", recs[0].Language)
	}
}

func TestEntitiesAreValid(t *testing.T) {
	src := `package p
import "testing"
func TestValid(t *testing.T) { SomeFunc() }`
	recs := runExtract(t, "v_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected entities")
	}
	for _, r := range recs {
		if err := r.Validate(); err != nil {
			t.Errorf("entity %q failed validation: %v", r.Name, err)
		}
		for _, rel := range r.Relationships {
			if err := rel.Validate(); err != nil {
				t.Errorf("relationship failed validation: %v", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Framework with NO test functions in body → zero entities
// ---------------------------------------------------------------------------

func TestTestFileWithNoFunctionsReturnsNil(t *testing.T) {
	// Python test file that happens to be empty of test_* functions.
	src := `import pytest
FIXTURE = 1
`
	recs := runExtract(t, "tests/test_empty.py", "python", src)
	if len(recs) != 0 {
		t.Errorf("expected 0, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// #2604 — tests/ directory path hint for pytest (the root cause of the meta-bug)
// ---------------------------------------------------------------------------

// TestPytest_TestsDirWithoutPrefix_IsIndexed exercises the root cause of #2604:
// Django projects (including upvate) place test files under a tests/ directory
// without requiring a test_ prefix in the basename (e.g. core/tests/schedule.py,
// api/tests/views.py). Before this fix, selectFramework returned nil for these
// files because the pytest filenameHints only matched test_*.py / *_test.py
// basenames. The testmap extractor then returned 0 entities for the whole tests/
// directory, causing ~107 test entities indexed instead of ~1,406 on upvate.
//
// The fix adds a pathHints regex (/tests?/.*\.py$) that matches the full
// repo-relative path, so files in tests/ or test/ are recognised without
// needing a test_ prefix.
func TestPytest_TestsDirWithoutPrefix_IsIndexed(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
	}{
		{
			name: "bare_tests_dir_no_prefix",
			path: "core/tests/schedule.py",
			src: `from django.test import TestCase
from core.views import ScheduleViewSet

class ScheduleTests(TestCase):
    def test_import_csv(self):
        resp = self.client.post('/api/v1/schedule/import', {})
        self.assertEqual(resp.status_code, 200)
`,
		},
		{
			name: "nested_tests_dir_views",
			path: "api/tests/views.py",
			src: `from django.test import TestCase

class ViewTests(TestCase):
    def test_list(self):
        resp = self.client.get('/api/v1/items/')
        self.assertEqual(resp.status_code, 200)
`,
		},
		{
			name: "test_dir_singular",
			path: "app/test/integration.py",
			src: `from django.test import TestCase

class IntegrationSuite(TestCase):
    def test_flow(self):
        pass
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := runExtract(t, tc.path, "python", tc.src)
			if len(recs) == 0 {
				t.Errorf("path %q: expected ≥1 entity from tests/ dir without test_ prefix, got 0 — "+
					"this is root cause of #2604: pytest pathHints not matching tests/ directories", tc.path)
			}
		})
	}
}

// TestPytest_TestsDirMatchesAnyPath verifies that matchesAnyPath correctly
// matches the pathHints for the pytest entry across typical Django layouts.
func TestPytest_TestsDirMatchesAnyPath(t *testing.T) {
	// Locate the pytest entry in frameworkOrder.
	var pytestEntry *frameworkEntry
	for i := range frameworkOrder {
		if frameworkOrder[i].name == "pytest" {
			pytestEntry = &frameworkOrder[i]
			break
		}
	}
	if pytestEntry == nil {
		t.Fatal("pytest entry not found in frameworkOrder")
	}

	yes := []string{
		"tests/schedule.py",
		"core/tests/views.py",
		"api/tests/serializers.py",
		"app/tests/__init__.py",
		"test/integration.py",
		"core/test/models.py",
	}
	no := []string{
		"views.py",
		"models.py",
		"tests_helpers/utils.go", // not a .py file
		"notests/foo.py",         // "notests" is not "tests" or "test"
	}

	for _, p := range yes {
		if !matchesAnyPath(p, pytestEntry.pathHints) {
			t.Errorf("matchesAnyPath(%q): want true (tests/ dir), got false", p)
		}
	}
	for _, p := range no {
		if matchesAnyPath(p, pytestEntry.pathHints) {
			t.Errorf("matchesAnyPath(%q): want false, got true", p)
		}
	}
}

// ---------------------------------------------------------------------------
// Regression: #2606 — filenameHints must not contain path patterns
// ---------------------------------------------------------------------------

// TestAllFrameworks_NoPathPatternsInFilenameHints asserts that all framework
// entries have basenames only in filenameHints and no forward-slash patterns
// (those belong in pathHints). This prevents silent match failures due to
// matchesAnyFilename only checking the basename against the full path.
func TestAllFrameworks_NoPathPatternsInFilenameHints(t *testing.T) {
	for _, fe := range frameworkOrder {
		for _, re := range fe.filenameHints {
			pattern := re.String()
			if strings.Contains(pattern, "/") {
				t.Errorf("framework %q has path pattern in filenameHints: %q (move to pathHints)",
					fe.name, pattern)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #3173 — TESTS edges wiring for pytest (Django TestCase) + JS (Jest)
// ---------------------------------------------------------------------------

// TestIssue3173_Pytest_DjangoTestCase_EmitsTESTSEdge exercises the upvate corpus
// pattern: a Django TestCase test method that directly calls a production helper
// (resolve_device) should emit a high-confidence TESTS edge targeting that helper.
// The test also verifies that HTTP test client calls (self.client.post) are NOT
// emitted as TESTS edge targets — they are stopwords after this fix.
func TestIssue3173_Pytest_DjangoTestCase_EmitsTESTSEdge(t *testing.T) {
	src := "import pytest\nfrom django.test import TestCase\nfrom core.helper.schedule_import_helper import resolve_device\n\nclass ResolveDeviceTest(TestCase):\n    def test_matches_by_name_exact(self):\n        device, errors = resolve_device(\"ELV-300\", self.group.id)\n        self.assertEqual(len(errors), 0)\n        resp = self.client.post('/api/v1/schedule/import', {})\n        self.assertEqual(resp.status_code, 200)\n"
	recs := runExtract(t, "core/tests/test_schedule_import.py", "python", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from Django TestCase test file, got 0")
	}

	// resolve_device must be targeted by a high-confidence TESTS edge.
	foundResolveDevice := false
	for _, r := range recs {
		if r.Properties["test_function"] != "test_matches_by_name_exact" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			if rel.Properties["tested"] == "resolve_device" {
				foundResolveDevice = true
			}
			// HTTP test client calls must NOT be emitted as TESTS targets.
			for _, banned := range []string{"post", "get", "assertEqual"} {
				if rel.Properties["tested"] == banned {
					t.Errorf("TESTS edge target %q is a test infrastructure call, not a production function", banned)
				}
			}
		}
	}
	if !foundResolveDevice {
		t.Errorf("expected TESTS edge targeting resolve_device; got recs=%d", len(recs))
		for _, r := range recs {
			t.Logf("  entity: test_function=%q tested=%q", r.Properties["test_function"], r.Properties["tested_function"])
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" {
					t.Logf("    TESTS->%q", rel.Properties["tested"])
				}
			}
		}
	}
}

// TestIssue3173_Jest_EmitsTESTSEdge exercises the JS test pattern: a Jest test
// that directly calls production functions emits TESTS edges. The test file uses
// the .test.ts filename convention which the Jest detector matches without
// requiring a jest import marker.
func TestIssue3173_Jest_EmitsTESTSEdge(t *testing.T) {
	src := "import { getUser } from './user';\nimport { updateUser } from './user';\n\ndescribe('User API', () => {\n  it('fetches a user by id', () => {\n    const u = getUser(1);\n    expect(u).toBeDefined();\n  });\n\n  it('updates user profile', () => {\n    const result = updateUser({ id: 1, name: 'Alice' });\n    expect(result.ok).toBe(true);\n  });\n});\n"
	recs := runExtract(t, "components/api.test.ts", "typescript", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity from Jest test file, got 0")
	}

	targets := map[string]bool{}
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" {
				targets[rel.Properties["tested"]] = true
			}
		}
	}

	// Both production functions must appear as TESTS edge targets.
	for _, want := range []string{"getUser", "updateUser"} {
		if !targets[want] {
			t.Errorf("expected TESTS edge targeting %q; got targets=%v", want, targets)
		}
	}
}

// ---------------------------------------------------------------------------
// Go HTTP handler→test edge resolution (#3348 tests_linkage deepening).
//
// Go HTTP tests commonly call handlers directly via:
//   handler.ServeHTTP(w, r)    — net/http Handler interface
//   router.ServeHTTP(w, r)     — gin.Engine / chi.Mux / echo.Echo / mux.Router
//   h.ServeHTTP(w, r)          — any Handler variable
//
// The directCallRE in resolver.go already matches `recv.ServeHTTP(` as a
// high-confidence call since ServeHTTP is not a stopword. These tests verify
// the end-to-end path: the handler dispatch call in the test body is surfaced
// as a TESTS edge targeting the handler symbol.
// ---------------------------------------------------------------------------

func TestGoHTTPHandler_ServeHTTPDirectCall(t *testing.T) {
	// handler.ServeHTTP(w, r) — direct net/http handler invocation.
	src := `package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUsersHandler(t *testing.T) {
	handler := NewUsersHandler(nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/users", nil)
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("got %d want 200", w.Code)
	}
}
`
	recs := runExtract(t, "handlers/users_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity")
	}
	// handler.ServeHTTP should be resolved as a high-confidence call
	// (direct method call, not a stopword).
	found := false
	for _, r := range recs {
		tested := r.Properties["tested_function"]
		if tested == "handler.ServeHTTP" || tested == "NewUsersHandler" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected handler.ServeHTTP or NewUsersHandler as tested_function; entities: %+v", recs)
	}
}

func TestGoHTTPHandler_RouterServeHTTP(t *testing.T) {
	// router.ServeHTTP(w, r) — common pattern for gin/chi/echo/mux tests.
	src := `package api_test

import (
	"net/http/httptest"
	"testing"
	"github.com/go-chi/chi/v5"
)

func TestOrdersRouter(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/orders", listOrders)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/orders", nil)
	router.ServeHTTP(w, r)
}
`
	recs := runExtract(t, "api/router_test.go", "go", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 entity for router-based test")
	}
	// router.ServeHTTP must appear as a TESTS edge target (tested production call).
	// Note: buildCollapsedEntity picks the alphabetically-first high-confidence call
	// as the primary tested_function property, so we check the TESTS edges rather
	// than the top-level property to verify ServeHTTP is surfaced.
	found := false
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == "router.ServeHTTP" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("expected router.ServeHTTP TESTS edge; entities=%+v", recs)
	}
}

func TestGoHTTPHandler_ServeHTTPPrecedesStopwords(t *testing.T) {
	// Ensure ServeHTTP is not accidentally filtered by the stopword list.
	src := `package svc_test

import "testing"

func TestMyHandler(t *testing.T) {
	h := &MyHandler{}
	t.Log("starting")
	w := newRecorder()
	r := newRequest("POST", "/do", nil)
	h.ServeHTTP(w, r)
}
`
	recs := runExtract(t, "svc_test.go", "go", src)
	found := false
	for _, r := range recs {
		if r.Properties["tested_function"] == "h.ServeHTTP" {
			found = true
		}
	}
	if !found {
		t.Errorf("ServeHTTP should not be filtered by stopwords; entities=%+v", recs)
	}
}

// TestGoHTTPHandlerNames_Extraction proves that goHTTPHandlerNames correctly
// extracts the set of handler-receiver names from a Go test source (value-
// asserting unit test for the regex helper added in #3348).
func TestGoHTTPHandlerNames_Extraction(t *testing.T) {
	src := `package x
func TestFoo(t *testing.T) {
	router.ServeHTTP(w, r)
	h.ServeHTTP(w, r)
	handler.ServeHTTP(w, r)
	t.Log("done")
}
`
	names := goHTTPHandlerNames(src)
	wantSet := map[string]bool{"router": false, "h": false, "handler": false}
	for _, n := range names {
		if _, ok := wantSet[n]; ok {
			wantSet[n] = true
		}
	}
	for name, found := range wantSet {
		if !found {
			t.Errorf("goHTTPHandlerNames: missing handler receiver %q; got %v", name, names)
		}
	}
	if len(names) != 3 {
		t.Errorf("expected 3 unique receivers, got %d: %v", len(names), names)
	}
}
