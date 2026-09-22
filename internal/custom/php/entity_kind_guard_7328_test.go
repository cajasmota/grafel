package php

// #7328 arm (c) — the planted-violation proof for this package's entity
// constructor.
//
// A validating constructor that is never reached reports a perfectly clean
// tree, so the wiring itself is the whole risk: `types.ValidateProducedEntityKind`
// could be absent from makeEntity, or present and unreachable, and every other
// test in this package would still pass. These three cases fix the constructor's
// behaviour in all three directions, so deleting or neutering the call in
// internal/custom/php/helpers.go fails HERE and not only somewhere downstream.
//
//   - undeclared  → must panic. This is the case that catches the next producer.
//   - declared    → must NOT panic. Without it a guard that rejected EVERY kind
//                   would pass the first case and break the whole extractor.
//   - recorded    → must NOT panic. This exercises the known-undeclared roster
//                   in internal/types; without it the roster branch is a
//                   vacuous allowlist nothing in this package ever reaches.
//
// The undeclared spelling is deliberately one no producer emits, so this test
// cannot be satisfied by the tree happening to declare it later.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func TestProducedEntityKindGuard7328_Php(t *testing.T) {
	t.Run("undeclared kind panics", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("makeEntity accepted the undeclared kind %q: the "+
					"types.ValidateProducedEntityKind call in internal/custom/php is missing or "+
					"unreachable, so every off-vocabulary kind this package emits is silent",
					undeclaredKind7328)
			}
			msg, _ := r.(string)
			if !strings.Contains(msg, undeclaredKind7328) {
				t.Fatalf("panic did not name the offending kind %q: %v", undeclaredKind7328, r)
			}
			if !strings.Contains(msg, "#7328") {
				t.Fatalf("panic did not come from the #7328 guard: %v", r)
			}
		}()
		_ = makeEntity("N", undeclaredKind7328, "sub", "f.src", "lang", 1)
	})

	t.Run("declared kind does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("makeEntity rejected the declared kind %q: %v",
					string(types.EntityKindClass), r)
			}
		}()
		got := makeEntity("N", string(types.EntityKindClass), "sub", "f.src", "lang", 1)
		if got.Kind != string(types.EntityKindClass) {
			t.Fatalf("Kind = %q, want %q", got.Kind, string(types.EntityKindClass))
		}
	})

	t.Run("recorded undeclared kind does not panic", func(t *testing.T) {
		if _, recorded := types.KnownUndeclaredProducedKinds()[recordedKind7328]; !recorded {
			t.Fatalf("premise gone: %q is no longer on the known-undeclared roster, so "+
				"this case no longer exercises the roster branch; pick a kind that is on it",
				recordedKind7328)
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("makeEntity rejected the recorded kind %q: %v", recordedKind7328, r)
			}
		}()
		got := makeEntity("N", recordedKind7328, "sub", "f.src", "lang", 1)
		if got.Kind != recordedKind7328 {
			t.Fatalf("Kind = %q, want %q", got.Kind, recordedKind7328)
		}
	})
}

// undeclaredKind7328 is spelled so that no rule, enum entry or producer carries
// it; if one ever does, the first subtest fails loudly rather than silently
// passing.
const undeclaredKind7328 = "SCOPE.NotAKind7328"

// recordedKind7328 is a real row on internal/types' known-undeclared roster.
const recordedKind7328 = "SCOPE.DI"
