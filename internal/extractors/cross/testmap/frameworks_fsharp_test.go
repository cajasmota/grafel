// Package testmap — value-asserting tests for the F# Expecto + xUnit/NUnit
// detector (#4906). Mirrors the Nim std/unittest tests; reuses the shared
// off-side-rule block-body extractor.
//
// The shared resolver's directCallRE captures PAREN-style call sites
// (`UserService()`, `Account.create()`) and the describe-subject from a
// `testList "Subject"` container. F#'s SPACE-APPLIED application (`createUser
// "ada"`) is the dominant functional call idiom and is now captured by the
// resolver's gated space-application pass (#5034), a port of the base
// extractor's spaceAppRE gated to tf.lang == "fsharp".
package testmap

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// TestFSharpExpecto_SubjectLinkage proves an Expecto `testList "User"` container
// seeds a describe-subject TESTS edge for each leaf `testCase`, attributed to the
// fsharp-expecto framework.
func TestFSharpExpecto_SubjectLinkage(t *testing.T) {
	src := `
module UserTests

open Expecto
open User

let tests =
    testList "User" [
        testCase "creates a user" <| fun _ ->
            let u = makeUser "ada"
            Expect.equal u.name "ada" "name"
    ]
`
	recs := runExtract(t, "tests/UserTests.fs", "fsharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for fsharp-expecto")
	}
	// #5034: the body's space-applied call `makeUser "ada"` is now captured as a
	// high-confidence direct SUT signal. Per the resolver's documented design the
	// describe-subject (`User`) is a FALLBACK that only fires when no call/mock is
	// resolved (resolveCalls Pass 3a, gated on len(seen)==0), so once the real
	// space-applied call is found it — not the testList subject — is the SUT.
	rec := findByTested(t, recs, "creates_a_user", "makeUser")
	if rec.Properties["test_framework"] != "fsharp-expecto" {
		t.Errorf("framework=%q, want fsharp-expecto", rec.Properties["test_framework"])
	}
	if c := edgeConf(recs, "creates_a_user", "makeUser"); c != "high" {
		t.Errorf("makeUser edge confidence = %q, want high (space-applied SUT)", c)
	}
}

// TestFSharpExpecto_ParenCallHighConfidence proves a PAREN-style .NET-interop
// call inside a `testCase` body is captured as a direct (high-confidence) TESTS
// edge — the F# constructor/interop call idiom.
func TestFSharpExpecto_ParenCallHighConfidence(t *testing.T) {
	src := `
module SvcTests

open Expecto
open Svc

let tests =
    testList "Svc" [
        testCase "alpha" <| fun _ ->
            let svc = OrderService()
            let r = svc.PlaceOrder(42)
            Expect.isTrue r "ok"
        testCase "beta" <| fun _ ->
            let q = QueryRunner()
            Expect.isTrue (q.RunBeta()) "b"
    ]
`
	recs := runExtract(t, "tests/SvcTests.fs", "fsharp", src)
	if !hasEdgeAny(recs, "alpha", "OrderService") {
		t.Errorf("expected alpha -> OrderService paren-call edge")
	}
	// Block-body scoping: QueryRunner (from beta) must not leak into alpha.
	if hasEdgeAny(recs, "alpha", "QueryRunner") {
		t.Errorf("QueryRunner leaked into the alpha case body — block-body scoping failed")
	}
	if !hasEdgeAny(recs, "beta", "QueryRunner") {
		t.Errorf("expected beta -> QueryRunner paren-call edge")
	}
}

// TestFSharpXUnit_AttributedLetBinding proves an xUnit [<Fact>]-decorated `let`
// binding is detected as a test case and its paren-call body is scanned.
func TestFSharpXUnit_AttributedLetBinding(t *testing.T) {
	src := `
module AccountTests

open Xunit
open Account

[<Fact>]
let ` + "``creates an account``" + ` () =
    let svc = AccountService()
    let acc = svc.Open("savings")
    Assert.NotNull acc
`
	recs := runExtract(t, "tests/AccountTests.fs", "fsharp", src)
	if len(recs) == 0 {
		t.Fatalf("expected >=1 testmap entity for fsharp xUnit")
	}
	if !hasEdgeAny(recs, "creates_an_account", "AccountService") {
		t.Errorf("expected creates_an_account -> AccountService edge")
	}
}

// TestFSharp_NonTestFileNoop proves a plain production .fs file (no test
// attribute, no Expecto case) is not detected as a test file.
func TestFSharp_NonTestFileNoop(t *testing.T) {
	src := `
module User

let createUser (name: string) : User =
    { name = name }
`
	recs := runExtract(t, "src/User.fs", "fsharp", src)
	if len(recs) != 0 {
		t.Fatalf("expected no testmap entities for a production file, got %d", len(recs))
	}
}

// TestFSharp_TestFilenameButNoCasesNoop proves a test-named .fs file that
// contains no Expecto case and no test attribute yields zero entities (the
// self-confirming detector, like rust_test).
func TestFSharp_TestFilenameButNoCasesNoop(t *testing.T) {
	src := `
module Helpers

let buildFixture () = ()
`
	recs := runExtract(t, "tests/HelpersTests.fs", "fsharp", src)
	if len(recs) != 0 {
		t.Fatalf("expected no entities for a test-named file with no cases, got %d", len(recs))
	}
}

// edgeConf returns the confidence Property of the highest-ranking TESTS edge
// from `test` to `prod`, or "" if no such edge exists.
func edgeConf(recs []types.EntityRecord, test, prod string) string {
	best := ""
	rank := map[string]int{"low": 1, "medium": 2, "high": 3}
	for _, r := range recs {
		if r.Properties["test_function"] != test {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "TESTS" && rel.Properties["tested"] == prod {
				if rank[rel.Properties["confidence"]] > rank[best] {
					best = rel.Properties["confidence"]
				}
			}
		}
	}
	return best
}

// TestFSharp_SpaceAppHappyPath — #5034: an F# test that exercises the SUT via
// SPACE application (`createUser "ada"`, `notify user`) yields a high-confidence
// TESTS edge to the space-applied head symbol. This is the dominant F# call
// idiom and is NOT paren-captured by directCallRE, so this is the resolver's
// space-application port of the base extractor's spaceAppRE.
func TestFSharp_SpaceAppHappyPath(t *testing.T) {
	src := `
module UserTests

open Expecto
open User

let tests =
    testList "User" [
        testCase "creates and notifies" <| fun _ ->
            let u = createUser "ada"
            notify u
            Expect.equal u.name "ada" "name"
    ]
`
	recs := runExtract(t, "tests/UserTests.fs", "fsharp", src)
	if !hasEdgeAny(recs, "creates_and_notifies", "createUser") {
		t.Errorf("expected space-applied TESTS edge -> createUser")
	}
	if c := edgeConf(recs, "creates_and_notifies", "createUser"); c != "high" {
		t.Errorf("createUser edge confidence = %q, want high", c)
	}
	if !hasEdgeAny(recs, "creates_and_notifies", "notify") {
		t.Errorf("expected space-applied TESTS edge -> notify")
	}
	// The Expecto assertion combinator must NOT surface as a SUT (stopword gate).
	if hasEdgeAny(recs, "creates_and_notifies", "Expect.equal") {
		t.Errorf("Expect.equal (assertion combinator) leaked as a tested subject")
	}
}

// TestFSharp_SpaceAppWrongLanguageNoop — #5034: the space-application pass is
// gated to tf.lang == "fsharp". A non-F# test body that happens to contain
// `name arg`-shaped text must NOT yield a space-applied edge — only the
// paren-call (Go `helper(x)`) is captured. Proves no cross-language regression.
func TestFSharp_SpaceAppWrongLanguageNoop(t *testing.T) {
	src := `package svc

import "testing"

func TestThing(t *testing.T) {
	helper(x)
	createUser "ada"
}
`
	recs := runExtract(t, "svc/thing_test.go", "go", src)
	// The Go paren-call is captured…
	if !hasEdgeAny(recs, "TestThing", "helper") {
		t.Errorf("expected Go paren-call edge -> helper")
	}
	// …but the F#-shaped space application must NOT fire for a Go body.
	if hasEdgeAny(recs, "TestThing", "createUser") {
		t.Errorf("F# space-application pass fired on a non-F# body (cross-language regression)")
	}
}

// TestFSharp_SpaceAppNoMatchNoop — #5034: an F# test whose body has no
// space-applied PRODUCTION call (only an Expecto assertion that is stop-worded,
// plus a paren-captured .NET interop constructor) yields NO spurious
// space-applied edge. The space-app pass fires (lang==fsharp) but every head it
// could see is a stop-worded combinator, so nothing new is emitted — only the
// genuine paren call survives.
func TestFSharp_SpaceAppNoMatchNoop(t *testing.T) {
	src := `
module CalcTests

open Expecto
open Calc

let tests =
    testList "Calc" [
        testCase "adds" <| fun _ ->
            let svc = Calculator()
            Expect.equal svc.Total 5 "sum"
    ]
`
	recs := runExtract(t, "tests/CalcTests.fs", "fsharp", src)
	// The paren-call interop constructor is captured by directCallRE.
	if !hasEdgeAny(recs, "adds", "Calculator") {
		t.Errorf("expected paren-call edge -> Calculator")
	}
	// The Expecto assertion combinator (stop-worded) must NOT leak, and the
	// space-app pass must not invent any other subject.
	for _, bad := range []string{"Expect.equal", "Expect", "equal"} {
		if hasEdgeAny(recs, "adds", bad) {
			t.Errorf("noise token %q leaked as a tested subject", bad)
		}
	}
}
