package fsharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7136 — the `letSeen` cross-dedupe key, `indent + ":let:" + name`, used to be
// built from TWO independent string literals: once in the let-binding scanner
// that WRITES the key, and once in the member loop that READS it to skip a
// member already emitted as a `let`. Their agreement is the whole mechanism,
// and nothing observed it: changing only the writer's literal to ":LET:" left
// ./internal/extractors/fsharp/ green at 0 `--- FAIL` lines. The fix routes
// both sites through `letSeenKey`, so one literal exists and the sites cannot
// drift.
//
// WHAT THIS FILE ASSERTS, and why it is a COUNT. Extracting the key into a
// helper and unit-testing the helper would be the #6533 shape: it grades the
// helper, not the dedupe, and stays green if a call site goes back to building
// the string inline. So the assertion below observes the CONSEQUENCE of the
// skip firing — the number of operation entities carrying the shared name.
// A failed skip emits the SAME name twice (once subtype "let", once subtype
// "member"), so a name-only assertion ("is there an operation called
// Compute?") passes with a duplicate present and grades nothing. Only the
// count sees it.
//
// Axes VARIED: which scanner reaches the name (let scanner vs member loop),
// the member's declaration form (`member this.X` vs `static member X`), and
// the indentation at which the collision happens (nested inside a type, and
// at top level in TestLetSeenKey_DistinctIndentIsNotDeduped).
//
// Axes HELD CONSTANT: the colliding name (always `Compute`, so a wrong
// capture is unambiguous), the entity kind (SCOPE.Operation), one collision
// per source, the enclosing `module M` / `type T`, and the file path.
//
// GRAMMAR. F# permits a type to carry both private `let` bindings and members
// (F# Language Specification § 8.6 "Class Type Definitions": a class's
// `let`-bindings form its private implementation, while `member` definitions
// form its public surface), which is exactly why this extractor needs a
// cross-dedupe at all: a lenient line scanner sees both forms and would
// otherwise index one logical operation twice. No F# toolchain exists in this
// environment (`dotnet`, `fsc`, `fsharpc`, `mono` are all absent), so whether
// a compiler would accept a `let` and a `member` sharing the identifier
// `Compute` in one type is derived from the specification and NOT executed.
// That question is also immaterial to what is under test: the scanner is
// deliberately lenient and reaches both lines regardless, which is the
// condition the dedupe exists to handle.

// fsOpCountNamed counts SCOPE.Operation entities with the given name, across
// every subtype. A duplicate emitted by a failed cross-dedupe differs from the
// original only in subtype, so the count must NOT filter on subtype.
func fsOpCountNamed(ents []types.EntityRecord, name string) int {
	n := 0
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == name {
			n++
		}
	}
	return n
}

// fsOpSubtypesNamed lists the subtypes of every operation carrying name, for
// failure messages — it makes a duplicate legible as ["let" "member"].
func fsOpSubtypesNamed(ents []types.EntityRecord, name string) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == name {
			out = append(out, ents[i].Subtype)
		}
	}
	return out
}

// TestLetSeenKey_MemberSharingLetNameIsDeduped is the consequence test. The
// same indent+name is reachable by BOTH scanners, so the writer's key and the
// reader's lookup must agree; if they drift by one byte the member loop stops
// skipping and the binding is indexed twice.
func TestLetSeenKey_MemberSharingLetNameIsDeduped(t *testing.T) {
	src := `module M

type T() =
    let Compute (x: int) = x + 1
    member this.Compute (x: int) = x + 2
`
	ents := runFSharp(t, src, "DedupeInstance.fs")
	if got := fsOpCountNamed(ents, "Compute"); got != 1 {
		t.Errorf("a member sharing a same-indent let's name produced %d operations named %q, want 1 (subtypes = %v) — the letSeen cross-dedupe did not fire",
			got, "Compute", fsOpSubtypesNamed(ents, "Compute"))
	}
	if fsFindLet(ents, "Compute") == nil {
		t.Errorf("the surviving operation is not the `let` one; let names = %v, subtypes for Compute = %v",
			fsLetNames(ents), fsOpSubtypesNamed(ents, "Compute"))
	}
}

// TestLetSeenKey_StaticMemberSharingLetNameIsDeduped varies the member's
// declaration form. `static member` reaches the same loop by a different
// alternative of memberRE, so a fix that only happened to work for the
// instance-member spelling is still caught.
func TestLetSeenKey_StaticMemberSharingLetNameIsDeduped(t *testing.T) {
	src := `module M

type T() =
    let Compute (x: int) = x + 1
    static member Compute (x: int) = x + 2
`
	ents := runFSharp(t, src, "DedupeStatic.fs")
	if got := fsOpCountNamed(ents, "Compute"); got != 1 {
		t.Errorf("a `static member` sharing a same-indent let's name produced %d operations named %q, want 1 (subtypes = %v)",
			got, "Compute", fsOpSubtypesNamed(ents, "Compute"))
	}
}

// TestLetSeenKey_DistinctIndentIsNotDeduped is the negative control against
// the over-broad fix: dropping the indent from the key, or comparing on name
// alone, would make the dedupe swallow a genuinely distinct member that merely
// shares a name with a `let` at a DIFFERENT nesting level. Here the `let` is at
// top level and the member is nested inside a type, so both must survive —
// two operations named `Compute`, not one.
func TestLetSeenKey_DistinctIndentIsNotDeduped(t *testing.T) {
	src := `module M

let Compute (x: int) = x + 1

type T() =
    member this.Compute (x: int) = x + 2
`
	ents := runFSharp(t, src, "DistinctIndent.fs")
	if got := fsOpCountNamed(ents, "Compute"); got != 2 {
		t.Errorf("a top-level let and a nested member sharing the name %q produced %d operations, want 2 (subtypes = %v) — the dedupe key ignored indentation",
			"Compute", got, fsOpSubtypesNamed(ents, "Compute"))
	}
}
