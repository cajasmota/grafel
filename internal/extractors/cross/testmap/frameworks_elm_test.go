// Package testmap — value-asserting tests for the elm-test detector (#5375).
package testmap

import "testing"

// TestElmTest_DirectCallHighConfidence proves a direct production call inside a
// `test "..." <| \_ -> …` body yields a TESTS edge, attributed to elm-test.
func TestElmTest_DirectCallHighConfidence(t *testing.T) {
	src := `module MathTest exposing (suite)

import Test exposing (..)
import Expect


suite : Test
suite =
    describe "Math"
        [ test "adds two numbers" <|
            \_ -> Expect.equal 4 (add 2 2)
        ]
`
	recs := runExtract(t, "tests/MathTest.elm", "elm", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for elm-test")
	}
	rec := findByTested(t, recs, "adds_two_numbers", "add")
	if rec.Properties["test_framework"] != "elm-test" {
		t.Errorf("framework=%q, want elm-test", rec.Properties["test_framework"])
	}
	if !hasEdge(recs, "adds_two_numbers", "add") {
		t.Errorf("missing TESTS edge adds_two_numbers -> add")
	}
}

// TestElmTest_BodyScoped proves the body extractor scans only the leaf's own
// body — a call in a SIBLING test must not leak into another test case's body.
func TestElmTest_BodyScoped(t *testing.T) {
	src := `module SvcTest exposing (suite)

import Test exposing (..)
import Expect


suite : Test
suite =
    describe "Svc"
        [ test "alpha" <|
            \_ -> Expect.equal True (runAlpha 1)
        , test "beta" <|
            \_ -> Expect.equal True (runBeta 2)
        ]
`
	recs := runExtract(t, "tests/SvcTest.elm", "elm", src)
	if !hasEdgeAny(recs, "alpha", "runAlpha") {
		t.Errorf("expected alpha -> runAlpha edge")
	}
	if !hasEdgeAny(recs, "beta", "runBeta") {
		t.Errorf("expected beta -> runBeta edge")
	}
	// alpha's body must NOT reach runBeta (sibling leak).
	if hasEdgeAny(recs, "alpha", "runBeta") {
		t.Errorf("alpha test body leaked into sibling beta (runBeta)")
	}
}

// TestElmTest_FuzzCase proves a `fuzz` property leaf is detected (the description
// is the LAST string literal, after the fuzzer argument).
func TestElmTest_FuzzCase(t *testing.T) {
	src := `module AddTest exposing (suite)

import Test exposing (..)
import Expect
import Fuzz exposing (int)


suite : Test
suite =
    describe "Add"
        [ fuzz int "is commutative" <|
            \n -> Expect.equal (add n 1) (add 1 n)
        ]
`
	recs := runExtract(t, "tests/AddTest.elm", "elm", src)
	if !hasEdgeAny(recs, "is_commutative", "add") {
		t.Errorf("expected fuzz case is_commutative -> add edge")
	}
}

// TestElmTest_AssertionDSLNotSubject proves the Expect.*/describe/test DSL is
// stop-worded — it never surfaces as the production subject under test.
func TestElmTest_AssertionDSLNotSubject(t *testing.T) {
	src := `module DslTest exposing (suite)

import Test exposing (..)
import Expect


suite : Test
suite =
    describe "Dsl"
        [ test "only assertions" <|
            \_ -> Expect.equal 1 1
        ]
`
	recs := runExtract(t, "tests/DslTest.elm", "elm", src)
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind != "TESTS" {
				continue
			}
			low := rel.ToID
			if containsAny(low, "Expect", "expect", "equal", "describe") {
				t.Errorf("DSL identifier surfaced as production subject: %s", rel.ToID)
			}
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if s == sub {
			return true
		}
	}
	return false
}
