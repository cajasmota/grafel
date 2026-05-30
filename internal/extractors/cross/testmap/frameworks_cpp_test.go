package testmap

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// C/C++ test-framework deep linkage (#3495).
//
// Each test asserts a SPECIFIC test→production TESTS edge (not len>0), proving
// the detector + resolver bind the test case to a named production symbol.
// ---------------------------------------------------------------------------

// gtest — TEST(Suite, Name): a direct production call inside the body yields a
// high-confidence edge to that symbol.
func TestCpp_GTest_DirectCall(t *testing.T) {
	src := `#include <gtest/gtest.h>
#include "calculator.h"

TEST(CalculatorTest, AddsTwoNumbers) {
    Calculator calc;
    int result = computeSum(2, 3);
    EXPECT_EQ(result, 5);
}`
	recs := runExtract(t, "calculator_test.cpp", "cpp", src)
	if !hasEdgeAny(recs, "CalculatorTest_AddsTwoNumbers", "computeSum") {
		t.Fatalf("expected TESTS edge CalculatorTest_AddsTwoNumbers -> computeSum; got %+v", relSummary(recs))
	}
}

// gtest — TEST_F(Fixture, Name): the fixture name (minus trailing "Test") seeds
// a medium-confidence subject edge when the body has no resolvable call.
func TestCpp_GTest_FixtureSubject(t *testing.T) {
	src := `#include <gtest/gtest.h>

TEST_F(UserServiceTest, ReturnsUser) {
    EXPECT_TRUE(true);
}`
	recs := runExtract(t, "user_service_test.cpp", "cpp", src)
	if !hasEdgeAny(recs, "UserServiceTest_ReturnsUser", "UserService") {
		t.Fatalf("expected subject edge UserServiceTest_ReturnsUser -> UserService; got %+v", relSummary(recs))
	}
}

// gtest — assertion macros (EXPECT_EQ / ASSERT_TRUE) must never become the
// tested target.
func TestCpp_GTest_AssertionsStopWorded(t *testing.T) {
	src := `#include <gtest/gtest.h>

TEST(MathTest, Squares) {
    int v = square(4);
    EXPECT_EQ(v, 16);
    ASSERT_TRUE(v > 0);
}`
	recs := runExtract(t, "math_test.cpp", "cpp", src)
	for _, bad := range []string{"EXPECT_EQ", "ASSERT_TRUE", "expect_eq", "assert_true"} {
		if hasEdgeAny(recs, "MathTest_Squares", bad) {
			t.Fatalf("assertion macro %q leaked as a tested target", bad)
		}
	}
	if !hasEdgeAny(recs, "MathTest_Squares", "square") {
		t.Fatalf("expected TESTS edge MathTest_Squares -> square; got %+v", relSummary(recs))
	}
}

// catch2 — TEST_CASE("name", "[tag]") + a direct production call.
func TestCpp_Catch2_DirectCall(t *testing.T) {
	src := `#include <catch2/catch_test_macros.hpp>
#include "parser.h"

TEST_CASE("parses a valid token", "[parser]") {
    auto tok = parseToken("abc");
    REQUIRE(tok.valid);
}`
	recs := runExtract(t, "parser_test.cpp", "cpp", src)
	if !hasEdgeAny(recs, "it_parses_a_valid_token", "parseToken") {
		t.Fatalf("expected TESTS edge it_parses_a_valid_token -> parseToken; got %+v", relSummary(recs))
	}
	if hasEdgeAny(recs, "it_parses_a_valid_token", "REQUIRE") {
		t.Fatalf("REQUIRE leaked as a tested target")
	}
}

// doctest — shares the TEST_CASE surface; selected by the doctest header.
func TestCpp_Doctest_DirectCall(t *testing.T) {
	src := `#include "doctest/doctest.h"

TEST_CASE("formats a date") {
    auto s = formatDate(2026, 5, 31);
    CHECK(s == "2026-05-31");
}`
	recs := runExtract(t, "date_test.cpp", "cpp", src)
	if !hasEdgeAny(recs, "it_formats_a_date", "formatDate") {
		t.Fatalf("expected TESTS edge it_formats_a_date -> formatDate; got %+v", relSummary(recs))
	}
}

// boost.test — BOOST_AUTO_TEST_CASE(name) + direct call.
func TestCpp_BoostTest_DirectCall(t *testing.T) {
	src := `#include <boost/test/unit_test.hpp>
#include "money.h"

BOOST_AUTO_TEST_CASE(adds_amounts) {
    auto total = addMoney(100, 250);
    BOOST_CHECK_EQUAL(total, 350);
}`
	recs := runExtract(t, "money_test.cpp", "cpp", src)
	if !hasEdgeAny(recs, "adds_amounts", "addMoney") {
		t.Fatalf("expected TESTS edge adds_amounts -> addMoney; got %+v", relSummary(recs))
	}
	if hasEdgeAny(recs, "adds_amounts", "BOOST_CHECK_EQUAL") {
		t.Fatalf("BOOST_CHECK_EQUAL leaked as a tested target")
	}
}

// boost.test — BOOST_FIXTURE_TEST_CASE(name, Fixture): fixture seeds subject.
func TestCpp_BoostTest_FixtureSubject(t *testing.T) {
	src := `#include <boost/test/unit_test.hpp>

BOOST_FIXTURE_TEST_CASE(uses_cache, CacheFixture) {
    BOOST_CHECK(true);
}`
	recs := runExtract(t, "cache_test.cpp", "cpp", src)
	if !hasEdgeAny(recs, "uses_cache", "Cache") {
		t.Fatalf("expected subject edge uses_cache -> Cache; got %+v", relSummary(recs))
	}
}

// cppunit — CPPUNIT_TEST registration + out-of-line method def with a call.
func TestCpp_CppUnit_DirectCall(t *testing.T) {
	src := `#include <cppunit/extensions/HelperMacros.h>

class AccountTest : public CppUnit::TestFixture {
    CPPUNIT_TEST_SUITE(AccountTest);
    CPPUNIT_TEST(testDeposit);
    CPPUNIT_TEST_SUITE_END();
public:
    void testDeposit() {
        depositFunds(account, 50);
        CPPUNIT_ASSERT_EQUAL(50, account.balance);
    }
};`
	recs := runExtract(t, "AccountTest.cpp", "cpp", src)
	if !hasEdgeAny(recs, "AccountTest_testDeposit", "depositFunds") {
		t.Fatalf("expected TESTS edge AccountTest_testDeposit -> depositFunds; got %+v", relSummary(recs))
	}
	if hasEdgeAny(recs, "AccountTest_testDeposit", "CPPUNIT_ASSERT_EQUAL") {
		t.Fatalf("CPPUNIT_ASSERT_EQUAL leaked as a tested target")
	}
}

// cpputest — TEST(group, name) + direct call; group seeds subject too.
func TestCpp_CppUTest_DirectCall(t *testing.T) {
	src := `#include "CppUTest/TestHarness.h"

TEST(StringUtils, Trims) {
    auto out = trimWhitespace("  hi  ");
    STRCMP_EQUAL("hi", out.c_str());
}`
	recs := runExtract(t, "string_utils_test.cpp", "cpp", src)
	if !hasEdgeAny(recs, "StringUtils_Trims", "trimWhitespace") {
		t.Fatalf("expected TESTS edge StringUtils_Trims -> trimWhitespace; got %+v", relSummary(recs))
	}
	if hasEdgeAny(recs, "StringUtils_Trims", "STRCMP_EQUAL") {
		t.Fatalf("STRCMP_EQUAL leaked as a tested target")
	}
}

// A plain (non-test) C++ source file must not be classified as a test file.
func TestCpp_NonTestFileSkipped(t *testing.T) {
	src := `#include <string>
int computeSum(int a, int b) { return a + b; }`
	recs := runExtract(t, "calculator.cpp", "cpp", src)
	if len(recs) != 0 {
		t.Fatalf("expected 0 entities from non-test C++ file, got %d", len(recs))
	}
}

// cppStripTestAffix unit coverage.
func TestCpp_StripTestAffix(t *testing.T) {
	cases := map[string]string{
		"UserServiceTest": "UserService",
		"CalculatorTests": "Calculator",
		"TestUserService": "UserService",
		"AccountFixture":  "Account",
		"PlainName":       "PlainName",
		"Test":            "Test",
	}
	for in, want := range cases {
		if got := cppStripTestAffix(in); got != want {
			t.Errorf("cppStripTestAffix(%q)=%q, want %q", in, got, want)
		}
	}
}

// relSummary renders the TESTS edges of recs for failure messages.
func relSummary(recs []types.EntityRecord) string {
	var b strings.Builder
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" {
				fmt.Fprintf(&b, "[%s -> %s (%s)] ",
					rel.Properties["test_function"], rel.Properties["tested"], rel.Properties["confidence"])
			}
		}
	}
	return b.String()
}
