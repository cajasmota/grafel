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
// C#
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
