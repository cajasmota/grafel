// Package testmap — value-asserting tests for the Lua busted/luaunit detectors
// and the Lua-specific block-body extractor (#3485).
package testmap

import (
	"testing"
)

// ---------------------------------------------------------------------------
// busted — direct production call inside it() body → high-confidence TESTS edge
// ---------------------------------------------------------------------------

func TestBusted_DirectCallHighConfidence(t *testing.T) {
	src := `
local user = require("user")

describe("User", function()
  it("creates a user", function()
    local u = create_user({ name = "ada" })
    assert.are.equal("ada", u.name)
  end)
end)
`
	recs := runExtract(t, "spec/user_spec.lua", "lua", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for busted spec")
	}
	rec := findByTested(t, recs, "it_creates_a_user", "create_user")
	if rec.Properties["test_framework"] != "busted" {
		t.Errorf("framework=%q, want busted", rec.Properties["test_framework"])
	}
	if rec.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high", rec.Properties["confidence"])
	}
	if !hasEdge(recs, "it_creates_a_user", "create_user") {
		t.Errorf("missing TESTS edge it_creates_a_user -> create_user")
	}
}

// busted assertion helpers (assert.are.equal, assert.is_true, …) must never
// surface as the tested production subject.
func TestBusted_AssertionStopwordsExcluded(t *testing.T) {
	src := `
describe("Account", function()
  it("validates balance", function()
    assert.are.equal(100, balance_of(acct))
    assert.is_true(is_active(acct))
    assert.has_error(function() withdraw(acct, -1) end)
  end)
end)
`
	recs := runExtract(t, "spec/account_spec.lua", "lua", src)
	for _, r := range recs {
		tf := r.Properties["tested_function"]
		if tf == "assert.are.equal" || tf == "assert.is_true" || tf == "assert.has_error" ||
			tf == "assert" {
			t.Errorf("assertion helper escaped stopword filter: %q", tf)
		}
		for _, rel := range r.Relationships {
			if rel.Properties["tested"] == "assert.are.equal" || rel.Properties["tested"] == "assert.is_true" {
				t.Errorf("assertion helper escaped into TESTS edge: %q", rel.Properties["tested"])
			}
		}
	}
	// At least one real production call must survive (balance_of / is_active / withdraw).
	if !hasEdgeAny(recs, "it_validates_balance", "balance_of") &&
		!hasEdgeAny(recs, "it_validates_balance", "is_active") &&
		!hasEdgeAny(recs, "it_validates_balance", "withdraw") {
		t.Errorf("expected a real production call to survive the stopword filter")
	}
}

// A describe subject that names a code symbol seeds a naming-convention edge
// when the it() body has no resolvable production call.
func TestBusted_DescribeSubjectFallback(t *testing.T) {
	src := `
describe("UserService", function()
  it("exists", function()
    assert.is_not_nil(true)
  end)
end)
`
	recs := runExtract(t, "spec/userservice_spec.lua", "lua", src)
	if !hasEdgeAny(recs, "it_exists", "UserService") {
		t.Errorf("expected describe-subject fallback edge it_exists -> UserService; recs=%d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// luaunit — TestClass:testXxx with a direct call → high-confidence TESTS edge
// ---------------------------------------------------------------------------

func TestLuaunit_DirectCallHighConfidence(t *testing.T) {
	src := `
local luaunit = require("luaunit")

TestCalculator = {}

function TestCalculator:testAdd()
  local result = compute_sum(2, 3)
  luaunit.assertEquals(result, 5)
end

os.exit(luaunit.run())
`
	recs := runExtract(t, "test_calculator.lua", "lua", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for luaunit test")
	}
	rec := findByTested(t, recs, "testAdd", "compute_sum")
	if rec.Properties["test_framework"] != "luaunit" {
		t.Errorf("framework=%q, want luaunit", rec.Properties["test_framework"])
	}
	if rec.Properties["confidence"] != "high" {
		t.Errorf("confidence=%q, want high", rec.Properties["confidence"])
	}
	if !hasEdge(recs, "testAdd", "compute_sum") {
		t.Errorf("missing TESTS edge testAdd -> compute_sum")
	}
}

// luaunit.assertXxx helpers must be stop-worded.
func TestLuaunit_AssertionStopwordsExcluded(t *testing.T) {
	src := `
local luaunit = require("luaunit")
TestUser = {}
function TestUser:testCreate()
  luaunit.assertEquals(make_user("x").name, "x")
  luaunit.assertTrue(make_user("x").active)
end
`
	recs := runExtract(t, "test_user.lua", "lua", src)
	for _, r := range recs {
		tf := r.Properties["tested_function"]
		if tf == "luaunit.assertEquals" || tf == "luaunit.assertTrue" {
			t.Errorf("luaunit assertion escaped stopword filter: %q", tf)
		}
	}
	if !hasEdgeAny(recs, "testCreate", "make_user") {
		t.Errorf("expected make_user to survive the stopword filter")
	}
}

// luaunit subject-from-class fallback: TestAccount with no resolvable body call
// links to Account.
func TestLuaunit_ClassSubjectFallback(t *testing.T) {
	src := `
TestAccount = {}
function TestAccount:testNoop()
  local x = 1
end
`
	recs := runExtract(t, "test_account.lua", "lua", src)
	if !hasEdgeAny(recs, "testNoop", "Account") {
		t.Errorf("expected class-subject fallback edge testNoop -> Account; recs=%d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Lua block-body extractor — keyword balancing, string/comment awareness
// ---------------------------------------------------------------------------

func TestExtractLuaBlockBody_NestedAndStrings(t *testing.T) {
	src := `it("x", function()
  if cond then
    do_work()
  end
  local s = "this end is in a string"
  -- this end is in a comment
  for i = 1, 3 do
    step(i)
  end
end)
trailing_should_not_appear()`
	// Start scan just after the description arg (at the comma) — mimic detector.
	idx := indexOfByte(src, ',')
	body := extractLuaBlockBody(src, idx)
	if body == "" {
		t.Fatal("expected a non-empty body")
	}
	if containsStr(body, "trailing_should_not_appear") {
		t.Errorf("body over-ran the matching end:\n%s", body)
	}
	if !containsStr(body, "do_work()") || !containsStr(body, "step(i)") {
		t.Errorf("body missing inner calls:\n%s", body)
	}
}

// Long-bracket strings/comments containing `end` must not close the block early.
func TestExtractLuaBlockBody_LongBracketString(t *testing.T) {
	src := `function()
  local doc = [[ this end should be ignored ]]
  real_call()
end
after()`
	body := extractLuaBlockBody(src, 0)
	if containsStr(body, "after()") {
		t.Errorf("long-bracket string `end` prematurely closed the block:\n%s", body)
	}
	if !containsStr(body, "real_call()") {
		t.Errorf("body missing real_call():\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// luaunitSubjectFromClass / bustedDescribeSubject unit checks
// ---------------------------------------------------------------------------

func TestLuaunitSubjectFromClass(t *testing.T) {
	cases := map[string]string{
		"TestUserService": "UserService",
		"TestAccount":     "Account",
		"Account":         "",
		"Test":            "",
	}
	for in, want := range cases {
		if got := luaunitSubjectFromClass(in); got != want {
			t.Errorf("luaunitSubjectFromClass(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestBustedDescribeSubject(t *testing.T) {
	if got := bustedDescribeSubject(`describe("UserService", function() end)`); got != "UserService" {
		t.Errorf("identifier subject: got %q", got)
	}
	if got := bustedDescribeSubject(`describe("users.handler", function() end)`); got != "handler" {
		t.Errorf("dotted subject tail: got %q", got)
	}
	if got := bustedDescribeSubject(`describe("returns 200 on GET", function() end)`); got != "" {
		t.Errorf("prose subject should be rejected: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// tiny local helpers (avoid importing strings into this _test file twice)
// ---------------------------------------------------------------------------

func indexOfByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func containsStr(haystack, needle string) bool {
	return len(needle) == 0 || indexOfSub(haystack, needle) >= 0
}

func indexOfSub(s, sub string) int {
	n, m := len(s), len(sub)
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return i
		}
	}
	return -1
}
