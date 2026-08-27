package ocaml_test

// #6481 arm A1 (ocaml) — the import placeholder must carry the marker the
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
// buildImportEntities (extractor.go) minted its per-`open` record with
// Kind="SCOPE.Component" but NO Subtype at all, so the predicate never
// recognised it and #6369's fix never reached OCaml. Because the placeholder
// is named by the module's LAST SEGMENT (importDisplayName), one
// `open Acme.Animal` anywhere in the repo collided with the real `module
// Animal` and silently deleted every bare-name edge to it — repo-wide, with no
// flag and nothing reporting it.
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
// equality, so an extractor that stops recognising `open` fails here.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const mlProbeFile6481 = "lib/core.ml"

// The probe deliberately makes the collision real: the module `Acme.Animal`
// has last segment `Animal`, and the same file declares `module Animal`. That
// is the exact shape of the defect.
//
// `Base.Map.Tree` is a 3-segment open, the shape that actually occurs, and
// `Stdio` is single-segment so importDisplayName's no-dot branch is covered
// too.
//
// EVERY real-declaration path the OCaml extractor has is represented — module,
// let-binding, type. That is not padding: the marker DEMOTES whatever carries
// it, so a construct absent from this fixture is a construct on which a
// Subtype:"import" mutant survives unobserved (measured on the sibling Haskell
// arm, where the type-synonym site lived through the whole package suite).
const mlProbeSrc6481 = `module Animal = struct
  let noise = "roar"
end

open Acme.Animal
open Base.Map.Tree
open Stdio

type animal = Dog | Cat

let speak x = x
`

// mlImportsTarget6481 returns the module specifier the record's IMPORTS edge
// points at, and whether the record carries such an edge at all. Carrying an
// IMPORTS edge is what makes a record an import placeholder independently of
// the marker under test — so the sweep below cannot be satisfied by the very
// field it is asserting. Enumerated against the extractor's full output: no
// real declaration emits an IMPORTS edge, so the discriminator is sound.
func mlImportsTarget6481(e types.EntityRecord) (string, bool) {
	for _, r := range e.Relationships {
		if r.Kind == "IMPORTS" {
			return r.ToID, true
		}
	}
	return "", false
}

func TestOCamlOpenEmitsMarkedImportPlaceholder_6481(t *testing.T) {
	ents := runOCaml(t, mlProbeSrc6481, mlProbeFile6481)

	// display name (the LAST SEGMENT — the collision-prone shape that is the
	// premise of the whole issue) -> the FULL dotted module.
	want := map[string]string{
		"Animal": "Acme.Animal",
		"Tree":   "Base.Map.Tree",
		"Stdio":  "Stdio",
	}

	got := map[string]string{}
	for _, e := range ents {
		target, isImport := mlImportsTarget6481(e)
		if !isImport {
			continue
		}
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			t.Errorf("import placeholder %q: Kind=%q Subtype=%q, want SCOPE.Component/import "+
				"(resolve.isImportPlaceholderKind will not recognise this record, so it "+
				"stays in the by-name index as a declaration and flips %q ambiguous)",
				e.Name, e.Kind, e.Subtype, e.Name)
		}

		// THE SPECIFIER CHANNEL (#6156). Now that the marker is stamped,
		// resolve.PruneImportPlaceholders recognises this record and
		// buildPlaceholderModuleRestores (imports.go:3502) rewrites incoming
		// edges to whatever placeholderModuleSpecifier reads. Its precedence is
		// import_module > module > QualifiedName > Name, so with none of the
		// first three set it would fall back to the bare last segment and
		// rename the external node from "Acme.Animal" to "Animal".
		spec := e.Properties["import_module"]
		if spec != target {
			t.Errorf("import placeholder %q: Properties[\"import_module\"]=%q, want the "+
				"full dotted module %q — the #6156 restore will otherwise record the "+
				"bare display Name", e.Name, spec, target)
		}
		// The specifier must NOT travel on Properties["module"]: that key is
		// the module-rollup label, and BOTH module.EnsureModule and
		// stampModuleOnEntities treat an extractor-supplied value as
		// authoritative, so a dotted specifier there fabricates a Module node
		// on the never-pruning incremental path.
		if got, ok := e.Properties["module"]; ok {
			t.Errorf("import placeholder %q pre-stamps Properties[\"module\"]=%q; that key "+
				"is the path-derived module-rollup label, not an import specifier", e.Name, got)
		}
		// Nor on QualifiedName: byQualifiedName is probed BEFORE byName and
		// never received #6427's placeholder precedence, so the placeholder
		// would shadow the very module it points at.
		if e.QualifiedName != "" {
			t.Errorf("import placeholder %q sets QualifiedName=%q; byQualifiedName is probed "+
				"ahead of byName and has no placeholder precedence", e.Name, e.QualifiedName)
		}

		if _, dup := got[e.Name]; dup {
			t.Errorf("duplicate import placeholder for %q", e.Name)
		}
		got[e.Name] = spec
	}

	// NON-VACUITY. Exact equality, not a subset and not a count floor: an
	// extractor that emits no placeholder at all fails right here.
	if len(got) != len(want) {
		t.Fatalf("vacuous or wrong: got %d import placeholders %v, want %d %v",
			len(got), got, len(want), want)
	}
	for name, mod := range want {
		if got[name] != mod {
			t.Errorf("import placeholder %q: specifier = %q, want %q", name, got[name], mod)
		}
	}
}

// mlFindBySubtype6481 locates a record by name AND subtype. Name alone is
// ambiguous here on purpose: `Animal` is both the import placeholder and the
// real module declaration.
func mlFindBySubtype6481(ents []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

// TestOCamlRealDeclarationsDoNotAcquireImportMarker_6481 pins the NEIGHBOURING
// axis. The marker DEMOTES whatever carries it out of the repo-wide by-name
// index, so stamping it on a real declaration deletes that declaration's own
// name instead of protecting it — the same defect with the sign flipped. It
// bites hardest here: an OCaml `module` declaration is precisely the thing an
// `open` targets.
//
// Without this control, a mutant that stamps Subtype:"import" on every
// emission path would satisfy the sweep above and survive.
func TestOCamlRealDeclarationsDoNotAcquireImportMarker_6481(t *testing.T) {
	ents := runOCaml(t, mlProbeSrc6481, mlProbeFile6481)

	// The exclusivity sweep: nothing may carry the marker, or the specifier
	// key, except a record that actually stands in for an import.
	sawMarked := 0
	for _, e := range ents {
		_, isImport := mlImportsTarget6481(e)
		if e.Subtype == "import" {
			sawMarked++
			if !isImport {
				t.Errorf("entity %q (Kind=%q) is marked Subtype=\"import\" but carries no "+
					"IMPORTS edge — the placeholder marker landed on a real declaration, "+
					"demoting it out of the by-name index", e.Name, e.Kind)
			}
		}
		if spec, ok := e.Properties["import_module"]; ok && !isImport {
			t.Errorf("entity %q (Kind=%q subtype=%q) carries Properties[\"import_module\"]=%q "+
				"but is not an import placeholder; no layer filters unknown property keys, so "+
				"that false provenance claim is persisted into the graph, rendered into docgen "+
				"output and served in MCP payloads", e.Name, e.Kind, e.Subtype, spec)
		}
	}
	if sawMarked == 0 {
		t.Fatal("vacuous: no entity carries Subtype=\"import\" at all")
	}

	// Each real declaration keeps its OWN subtype, asserted positively so the
	// check cannot be satisfied by the declaration disappearing.
	for _, want := range []struct{ name, subtype string }{
		{"Animal", "module"},
		{"animal", "type"},
		{"speak", "function"},
	} {
		e := mlFindBySubtype6481(ents, want.name, want.subtype)
		if e == nil {
			t.Fatalf("no entity %q with Subtype=%q; the real-declaration paths are not "+
				"emitting, so this control is vacuous", want.name, want.subtype)
		}
		if _, isImport := mlImportsTarget6481(*e); isImport {
			t.Errorf("real declaration %q (subtype %q) carries an IMPORTS edge", e.Name, e.Subtype)
		}
	}
}
