package javascript

// #7328 arm (c) — the planted-violation proof for this package's two entity
// constructors.
//
// These two are METHODS, and that is the whole reason they need their own
// file. The roster test's derivation skipped methods (`fd.Recv != nil`) and its
// call-site stage only resolved `*ast.Ident` calls, so emitWithRels and
// emitWithProps — parameters written verbatim into EntityRecord.Kind, 27
// literal call sites, a DEFAULT-ON producer — were invisible to both halves
// while the derived population stayed correct. A detector can be wrong about
// its own reach without being wrong about its answer.
//
//   - undeclared → must panic. This is the case that catches the next producer.
//   - declared   → must NOT panic, so the guard is not rejecting everything.
//   - recorded   → must NOT panic, which exercises the known-undeclared roster.
//
// WHY name IS EMPTY. Both methods take a tree-sitter node and dereference it
// (lines(n), stampTransactional) after the early-return on an empty name. The
// guard call is the FIRST statement in each body, ahead of that return, so an
// empty name exercises the guard and then exits without touching the node.
// That makes these three cases about the GUARD only; that the Kind actually
// lands on the record is pinned by the package's own extractor tests, which
// drive both methods through real parses.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// undeclaredKind7328 is spelled so that no rule, enum entry or producer carries
// it; if one ever does, the first subtest fails loudly rather than silently
// passing.
const undeclaredKind7328 = "SCOPE.NotAKind7328"

// recordedKind7328 is a real row on internal/types' known-undeclared roster.
const recordedKind7328 = "SCOPE.DI"

func TestProducedEntityKindGuard7328_JavascriptMethods(t *testing.T) {
	var zero ts.Node

	emitters := map[string]func(kind string){
		"(*extractor).emitWithRels": func(kind string) {
			x := &extractor{}
			x.emitWithRels("", kind, zero, "sub", "sig", nil)
		},
		"(*extractor).emitWithProps": func(kind string) {
			x := &extractor{}
			x.emitWithProps("", kind, zero, "sub", "sig", map[string]string{}, nil)
		},
	}

	for name, emit := range emitters {
		t.Run(name, func(t *testing.T) {
			t.Run("undeclared kind panics", func(t *testing.T) {
				defer func() {
					r := recover()
					if r == nil {
						t.Fatalf("%s accepted the undeclared kind %q: the "+
							"types.ValidateProducedEntityKind call is missing or unreachable, "+
							"so every off-vocabulary kind this default-ON producer emits is "+
							"silent", name, undeclaredKind7328)
					}
					msg, _ := r.(string)
					if !strings.Contains(msg, undeclaredKind7328) {
						t.Fatalf("panic did not name the offending kind %q: %v",
							undeclaredKind7328, r)
					}
					if !strings.Contains(msg, "#7328") {
						t.Fatalf("panic did not come from the #7328 guard: %v", r)
					}
					if !strings.Contains(msg, name) {
						t.Fatalf("panic did not name the offending constructor %q, so it "+
							"sends the reader to the wrong emitter: %v", name, r)
					}
				}()
				emit(undeclaredKind7328)
			})

			t.Run("declared kind does not panic", func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s rejected the declared kind %q: %v",
							name, string(types.EntityKindClass), r)
					}
				}()
				emit(string(types.EntityKindClass))
			})

			t.Run("recorded undeclared kind does not panic", func(t *testing.T) {
				if _, recorded := types.KnownUndeclaredProducedKinds()[recordedKind7328]; !recorded {
					t.Fatalf("premise gone: %q is no longer on the known-undeclared roster, "+
						"so this case no longer exercises the roster branch", recordedKind7328)
				}
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s rejected the recorded kind %q: %v", name, recordedKind7328, r)
					}
				}()
				emit(recordedKind7328)
			})
		})
	}
}
