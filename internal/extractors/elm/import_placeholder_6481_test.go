package elm_test

// #6481 arm A1 (elm) — the import placeholder must carry the marker the
// resolver keys on.
//
// resolve/refs.go:1602-1604 defines the import-placeholder marker as
//
//	kind == "SCOPE.Component" && subtype == "import"
//
// and it is consumed by refs.go:1589 and by symbol_index.go:209 / :448. #6427
// taught BuildIndex that a record carrying that marker is NOT a declaration of
// the name it holds, so a real declaration outranks it instead of flipping the
// name AMBIGUOUS and dropping every bare-name edge to it.
//
// buildImportEntities (extractor.go) minted its per-`import` record with
// Kind="SCOPE.Component" but NO Subtype at all, so the predicate never
// recognised it and #6369's fix never reached Elm. Because the placeholder is
// named by the module's LAST SEGMENT (importDisplayName), one
// `import Acme.Animal` anywhere in the repo collided with the real type
// `Animal` and silently deleted every bare-name edge to that type — repo-wide,
// with no flag and nothing reporting it.
//
// WHY THIS IS A UNIT TEST AND NOT A GOLDEN FIXTURE: Subtype on a bodiless stub
// is not surfaced in golden output (#6488), so a golden fixture passes
// byte-identically before and after the stamp. It has to be asserted on the
// extractor's emitted records.
//
// NON-VACUITY IS THE POINT. A test that sweeps "every placeholder must carry
// the marker" passes trivially when the extractor emits ZERO placeholders —
// which is exactly the shape that let this drift unnoticed across 25
// languages. The expected name->module table below is compared for EXACT
// equality, so an extractor that stops recognising `import` fails here.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const elmProbeFile6481 = "src/Domain/Core.elm"

// The probe deliberately makes the collision real: the module `Acme.Animal`
// has last segment `Animal`, and the same file declares a custom type called
// `Animal`. That is the exact shape of the defect.
//
// `Html.Attributes` is a 3-segment import, the shape that actually occurs, and
// `Browser` is single-segment so importDisplayName's no-dot branch is covered
// too.
const elmProbeSrc6481 = `module Domain.Core exposing (..)

import Acme.Animal
import Html.Attributes exposing (class, id)
import Browser

type Animal
    = Dog
    | Cat

view model =
    model
`

// elmImportsTarget6481 returns the module specifier the record's IMPORTS edge
// points at, and whether the record carries such an edge at all. Carrying an
// IMPORTS edge is what makes a record an import placeholder independently of
// the marker under test — so the sweep below cannot be satisfied by the very
// field it is asserting.
func elmImportsTarget6481(e types.EntityRecord) (string, bool) {
	for _, r := range e.Relationships {
		if r.Kind == "IMPORTS" {
			return r.ToID, true
		}
	}
	return "", false
}

func TestElmImportEmitsMarkedImportPlaceholder_6481(t *testing.T) {
	ents := runElm(t, elmProbeSrc6481, elmProbeFile6481)

	// display name (the LAST SEGMENT — the collision-prone shape that is the
	// premise of the whole issue) -> the full module the IMPORTS edge targets.
	want := map[string]string{
		"Animal":     "Acme.Animal",
		"Attributes": "Html.Attributes",
		"Browser":    "Browser",
	}

	got := map[string]string{}
	for _, e := range ents {
		target, isImport := elmImportsTarget6481(e)
		if !isImport {
			continue
		}
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			t.Errorf("import placeholder %q: Kind=%q Subtype=%q, want SCOPE.Component/import "+
				"(resolve.isImportPlaceholderKind will not recognise this record, so it "+
				"stays in the by-name index as a declaration and flips %q ambiguous)",
				e.Name, e.Kind, e.Subtype, e.Name)
		}
		if _, dup := got[e.Name]; dup {
			t.Errorf("duplicate import placeholder for %q", e.Name)
		}
		got[e.Name] = target
	}

	// NON-VACUITY. Exact equality, not a subset and not a count floor: an
	// extractor that emits no placeholder at all fails right here.
	if len(got) != len(want) {
		t.Fatalf("vacuous or wrong: got %d import placeholders %v, want %d %v",
			len(got), got, len(want), want)
	}
	for name, mod := range want {
		if got[name] != mod {
			t.Errorf("import placeholder %q: IMPORTS target = %q, want %q", name, got[name], mod)
		}
	}
}

// elmFindBySubtype6481 locates a record by name AND subtype. Name alone is
// ambiguous here on purpose: `Animal` is both the import placeholder and the
// real custom type.
func elmFindBySubtype6481(ents []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

// TestElmRealDeclarationsDoNotAcquireImportMarker_6481 pins the NEIGHBOURING
// axis. The marker DEMOTES whatever carries it out of the repo-wide by-name
// index, so stamping it on a real declaration deletes that declaration's own
// name instead of protecting it — the same defect with the sign flipped.
//
// Without this control, a mutant that stamps Subtype:"import" on every
// emission path would satisfy the sweep above and survive.
func TestElmRealDeclarationsDoNotAcquireImportMarker_6481(t *testing.T) {
	ents := runElm(t, elmProbeSrc6481, elmProbeFile6481)

	// The exclusivity sweep: nothing may carry the marker except a record that
	// actually stands in for an import.
	sawMarked := 0
	for _, e := range ents {
		if e.Subtype != "import" {
			continue
		}
		sawMarked++
		if _, isImport := elmImportsTarget6481(e); !isImport {
			t.Errorf("entity %q (Kind=%q) is marked Subtype=\"import\" but carries no "+
				"IMPORTS edge — the placeholder marker landed on a real declaration, "+
				"demoting it out of the by-name index", e.Name, e.Kind)
		}
	}
	if sawMarked == 0 {
		t.Fatal("vacuous: no entity carries Subtype=\"import\" at all")
	}

	// Each real declaration keeps its OWN subtype, asserted positively so the
	// check cannot be satisfied by the declaration disappearing.
	for _, want := range []struct{ name, subtype string }{
		{"Domain.Core", "module"},
		{"Animal", "type"},
		{"view", "function"},
	} {
		e := elmFindBySubtype6481(ents, want.name, want.subtype)
		if e == nil {
			t.Fatalf("no entity %q with Subtype=%q; the real-declaration paths are not "+
				"emitting, so this control is vacuous", want.name, want.subtype)
		}
		if _, isImport := elmImportsTarget6481(*e); isImport {
			t.Errorf("real declaration %q (subtype %q) carries an IMPORTS edge", e.Name, e.Subtype)
		}
	}
}
