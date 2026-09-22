package types

import (
	"fmt"
	"sort"
	"testing"
)

// #7328 arm (c) — CONSTRUCTOR-SIDE VALIDATION OF ARGUMENT-PASSED ENTITY KINDS.
//
// internal/entkinds.ScanGo recognises exactly one shape: a composite literal
// whose type's final identifier is `Entity` or `EntityRecord`, carrying a
// `Kind:` KEY. Nineteen production constructors — seventeen plain functions
// and two METHODS — take the kind as a FUNCTION ARGUMENT and write it into
// types.EntityRecord.Kind verbatim, so every call site behind them is
// invisible to that scanner — no site, no unresolved entry,
// no ledger row. #7328 is the call-site half of that hole; #6860 is the
// declaration half.
//
// This file is the RUNTIME half of the fix: the constructors ask
// ValidateProducedEntityKind before they hand the record back, so an
// off-vocabulary kind is caught BY CONSTRUCTION rather than by scanner
// coverage. It sees resolved values, so it also catches the shapes a static
// scan cannot reach at all: a kind read out of a carrier struct
// (internal/custom/java's SecondaryEntity, whose Kind field reaches
// EntityRecord through patterns_dispatch.go's makeEntity call), a kind
// computed at runtime, and a kind forwarded through two hops.
//
// WHY IT IS NOT AN OUTRIGHT REJECTION. Fifteen off-vocabulary kinds are emitted
// by the tree TODAY at ninety-three sites, all under internal/custom/**. Deciding
// which of them to declare, rename or delete is the taxonomy question #7310
// explicitly leaves open, and this arm must not settle it by making the tree
// red. So the fifteen are RECORDED on knownUndeclaredProducedKinds below rather
// than silently tolerated: a kind on that roster passes, anything else fails.
//
// THE ROSTER IS NOT HAND-MAINTAINED PROSE. A roster with no relation to the
// population it covers is the #6887 defect, and an allowlist nothing exercises
// is a vacuous guard. produced_entity_kind_roster_7328_test.go re-derives the
// whole population from the source tree — it finds the constructors by AST
// (functions that forward one of their own parameters into an EntityRecord's
// Kind field), resolves the literal argument at each call site, and sweeps the
// carrier-struct `Kind:` literals — then asserts the derived population equals
// this roster EXACTLY, counts included. So:
//
//   - a NEW off-vocabulary kind fails, at runtime via this guard and statically
//     via the roster test;
//   - a roster entry whose sites are gone, or whose count moved, fails the
//     roster test. A stale entry cannot sit here forever.
//
// IT DEGRADES IN PRODUCTION, DELIBERATELY. Outside `go test` this function is a
// no-op and the kind flows through exactly as before, so a shipped index is
// byte-identical to one built without this file. Panicking a user's index over
// a vocabulary defect would trade a cosmetic problem for a fatal one; the
// population that needs to see the failure is CI, and every one of the
// nineteen constructors' packages has direct tests.

// knownUndeclaredProducedKinds is the exact set of entity kinds that reach
// types.EntityRecord.Kind through an argument-passed constructor today without
// being in the EntityKind enum, mapped to the number of source sites that
// produce each one.
//
// The counts are part of the pin, not documentation: produced_entity_kind_roster_7328_test.go
// re-derives them and fails on any drift in either direction. Do NOT edit a
// count to make that test pass — the test is reporting that the tree changed.
//
// Membership here is a STATEMENT OF RECORD, not approval. Every row is a
// vocabulary defect awaiting the #7310 taxonomy decision. The right way to
// remove a row is to declare the kind in the EntityKind enum (or change the
// producer), never to leave it here.
var knownUndeclaredProducedKinds = map[string]int{
	// internal/custom/scala — 52 sites across seven kinds.
	"SCOPE.DI":            16,
	"SCOPE.Type":          12,
	"SCOPE.Middleware":    9,
	"SCOPE.Security":      7,
	"SCOPE.Observability": 5,
	"SCOPE.Interface":     2,
	"SCOPE.Router":        1,
	// internal/custom/kotlin — 5 sites.
	"SCOPE.Relationship": 5,
	// internal/custom/java — 36 sites, all reaching EntityRecord as `Kind:`
	// FIELDS on a SecondaryEntity carrier, which patterns_dispatch.go forwards
	// into makeEntity. A static kind-argument scan cannot see these at all;
	// this guard catches them because it runs after the forward.
	"SCOPE.DataModel":  2,
	"SCOPE.Dependency": 1,
	"SCOPE.Field":      1,
	// The four below are NOT spelled like the vocabulary, and that is why
	// #7328's scoping inventory — which swept `"SCOPE.*"` literals — reported
	// eleven kinds at sixty-one sites rather than fifteen at ninety-three.
	// `"Handler"` was the one that surfaced it: the guard rejected it from the
	// real extractor dispatch in internal/extractors before any static sweep
	// here had listed it. An off-vocabulary kind need not look off-vocabulary.
	"AuthGuard": 15,
	"Handler":   7,
	"TestSetup": 6,
	"TestCase":  4,
}

// KnownUndeclaredProducedKinds returns a copy of the roster above: each
// off-vocabulary entity kind the tree produces today, mapped to its pinned
// source-site count. Exported for the roster test, which is the only thing that
// reads the counts.
func KnownUndeclaredProducedKinds() map[string]int {
	out := make(map[string]int, len(knownUndeclaredProducedKinds))
	for k, v := range knownUndeclaredProducedKinds {
		out[k] = v
	}
	return out
}

// inTestProcess reports whether this process is a `go test` binary. It is a
// VARIABLE, not a direct testing.Testing() call, and that is the whole point.
//
// The production behaviour — an unrecorded kind flows through UNCHANGED, so a
// shipped index is byte-identical to one built without this file — is the claim
// the entire "record, do not reject" design rests on. Nothing in an in-process
// test can observe it by construction: under `go test`, testing.Testing() is
// true, so the `!testing.Testing()` early return is unreachable and DELETING
// THE GATE ENTIRELY IS INVISIBLE. A mutant that removes it survives every one
// of the nineteen panic-expecting package tests, and the resulting binary
// panics a real index.
//
// So the gate is a seam a test can drive from the other side. The inverse
// mutation (a gate that is inert UNDER test) is caught by those nineteen tests;
// this seam is what catches the mutation that is inert IN PRODUCTION.
// produced_entity_kind_guard_7328_test.go drives it.
var inTestProcess = testing.Testing

// ValidateProducedEntityKind reports an entity kind that a producer is about to
// write into types.EntityRecord.Kind but that the EntityKind enum does not
// carry and the known-undeclared roster does not record.
//
// site names the constructor for the failure message — it is what makes the
// panic actionable, since the kind alone does not say which of nineteen
// constructors let it through.
//
// Under `go test` an unrecorded kind PANICS. Outside `go test` it is a no-op:
// the kind flows through unchanged and the index is unaffected.
func ValidateProducedEntityKind(site, kind string) {
	if IsValidEntityKind(kind) {
		return
	}
	if _, recorded := knownUndeclaredProducedKinds[kind]; recorded {
		return
	}
	if !inTestProcess() {
		return
	}
	panic(fmt.Sprintf(
		"#7328: %s produced entity kind %q, which is not in the EntityKind enum "+
			"(internal/types/kinds.go) and is not on the known-undeclared roster "+
			"(internal/types/produced_entity_kind.go). Declare the kind, or fix the "+
			"producer. Recorded kinds are: %v",
		site, kind, sortedRosterKinds()))
}

func sortedRosterKinds() []string {
	out := make([]string, 0, len(knownUndeclaredProducedKinds))
	for k := range knownUndeclaredProducedKinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
