// #7328 arm (c) — the PRODUCTION direction of the guard's gate.
//
// This file is in package `types` rather than `types_test` because the thing it
// grades is unexported on purpose: the seam `inTestProcess`.
//
// Every other test of this guard — the nineteen per-constructor ones and the
// verdict table in produced_entity_kind_guard_7328_test.go — runs inside a
// `go test` binary, where testing.Testing() is true. That makes the
// `!inTestProcess()` early return unreachable from all of them, so DELETING
// the gate is behaviourally identical under test and survives every one of
// them. It is not identical in production: without the gate a shipped index
// panics on the first unrecorded kind.
//
// The claim "outside `go test` this is a no-op and the index is byte-identical"
// is the load-bearing line of the whole design, and before this file it had no
// observer at all.

package types

import (
	"reflect"
	"testing"
)

// TestProducedEntityKindGuard7328_ProductionIsANoOp drives the seam from the
// production side and asserts the guard does nothing at all.
func TestProducedEntityKindGuard7328_ProductionIsANoOp(t *testing.T) {
	const kind = "SCOPE.NotAKind7328"
	if IsValidEntityKind(kind) {
		t.Fatalf("premise gone: %q became a declared kind", kind)
	}
	if _, recorded := knownUndeclaredProducedKinds[kind]; recorded {
		t.Fatalf("premise gone: %q was added to the roster", kind)
	}

	// The positive control: with the seam left alone, this same kind MUST
	// panic. Without it, a guard that had been neutered some other way would
	// make the production case below pass for the wrong reason.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("control failed: the guard did not panic under test, so the " +
					"production case below proves nothing")
			}
		}()
		ValidateProducedEntityKind("internal/types.test", kind)
	}()

	saved := inTestProcess
	t.Cleanup(func() { inTestProcess = saved })
	inTestProcess = func() bool { return false }

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the guard PANICKED in a non-test process on the unrecorded kind %q: "+
				"%v\n\nThis is the production-fatal direction. A shipped grafel index would "+
				"abort on the first off-vocabulary kind instead of recording it, and the "+
				"\"degrades in production\" claim in produced_entity_kind.go is false.",
				kind, r)
		}
	}()
	ValidateProducedEntityKind("internal/types.test", kind)
}

// TestProducedEntityKindGuard7328_SeamDefaultsToTestingTesting pins that the
// seam IS testing.Testing and not a constant that happens to agree with it.
//
// Calling it and checking the answer cannot settle this: inside a `go test`
// binary testing.Testing() is true, so `func() bool { return true }` — which
// would make the production no-op unreachable in the shipping binary, exactly
// the M-GATE failure the seam exists to catch — returns the same answer as the
// real thing. Every behavioural check in this package is blind to it.
//
// Function-pointer identity is not blind to it, and it is an exact test rather
// than a source-text pin.
func TestProducedEntityKindGuard7328_SeamDefaultsToTestingTesting(t *testing.T) {
	want := reflect.ValueOf(testing.Testing).Pointer()
	got := reflect.ValueOf(inTestProcess).Pointer()
	if got != want {
		t.Fatalf("inTestProcess is not testing.Testing (got fn %#x, want %#x). A seam that "+
			"defaults to anything else decides the gate's production behaviour at compile "+
			"time, and no behavioural test in this package can see the difference: under "+
			"`go test` a constant `true` and testing.Testing() are indistinguishable.",
			got, want)
	}
	if !inTestProcess() {
		t.Fatal("inTestProcess() is false inside a go test binary")
	}
}
