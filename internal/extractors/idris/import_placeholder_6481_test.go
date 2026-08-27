package idris_test

// #6481 arm A3 (idris) — the import placeholder must carry the marker the
// resolver keys on.
//
// resolve/refs.go:1602-1604 defines the import-placeholder marker as
//
//	kind == "SCOPE.Component" && subtype == "import"
//
// consumed at refs.go:1589 and symbol_index.go:209 / :448. #6427 taught
// BuildIndex that a record carrying that marker is NOT a declaration of the
// name it holds, so a real declaration outranks it instead of flipping the name
// AMBIGUOUS and dropping every bare-name edge to it.
//
// buildImportEntities (extractor.go) minted its per-`import` record with
// Kind="SCOPE.Component" but NO Subtype at all, so the predicate never
// recognised it and #6369's fix never reached Idris. Idris has no
// importDisplayName helper — the last-segment logic is INLINED in
// buildImportEntities — but the shape is identical: one `import Acme.Speaker`
// anywhere in the repo collided with the real `interface Speaker`, repo-wide,
// including from files that import nothing.
//
// MEASURED, the edges are NOT dropped — they are silently REBOUND TO THE IMPORT
// PLACEHOLDER'S OWN ENTITY ID. Without the marker the placeholder is indexed as
// a declaration of `Speaker` and outranks the real interface, so every bare-name
// IMPLEMENTS edge in the repo lands on a stub that stands for someone else's
// module. That is why the assertions below compare the resolved ToID against the
// real interface's exact id rather than merely checking that it resolved: an
// id-equality check distinguishes the rebind from a drop, and the two want
// different fixes.
//
// WHY THESE ARE UNIT TESTS AND NOT A GOLDEN FIXTURE: Subtype on a bodiless stub
// is not surfaced in golden output (#6488), so a golden fixture passes
// byte-identically before and after the stamp. The effect is only observable by
// driving the REAL pipeline — extractor.Get("idris") → graph.EntityID →
// resolve.BuildIndex → resolve.ReferencesEmbedded.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/module"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const (
	idrBaseFile6481    = "src/Domain/Base.idr"
	idrImplFile6481    = "src/App/Impl.idr"
	idrCollideFile6481 = "src/App/Collide.idr"
)

// Base.idr declares the real `Speaker` interface. It imports nothing.
const idrBaseSrc6481 = `module Domain.Base

interface Speaker a where
  speak : a -> String
`

// Impl.idr references `Speaker` by BARE NAME through two IMPLEMENTS edges and
// imports nothing at all — the innocent bystander of the defect.
const idrImplSrc6481 = `module App.Impl

data Cat = MkCat

data Dog = MkDog

implementation Speaker Cat where
  speak x = "meow"

implementation Speaker Dog where
  speak x = "woof"
`

// The probe file: one import whose LAST DOTTED SEGMENT collides with the real
// interface name. Nothing else in it touches `Speaker`.
const idrCollideSrc6481 = `module App.Collide

import Acme.Speaker
`

// idrIDEntities6481 assigns the production entity IDs (graph.EntityID hashes
// repo, kind, name and sourceFile — and NOT Subtype) so the resolver sees
// exactly the records the indexer would hand it.
func idrIDEntities6481(recs []types.EntityRecord) []types.EntityRecord {
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("repo", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	return recs
}

// idrResolveImplements6481 runs the real BuildIndex → ReferencesEmbedded over
// the given files and returns implementation-name → resolved IMPLEMENTS ToID.
func idrResolveImplements6481(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	var recs []types.EntityRecord
	// Deterministic order; collide may be absent.
	for _, path := range []string{idrBaseFile6481, idrImplFile6481, idrCollideFile6481} {
		src, ok := files[path]
		if !ok {
			continue
		}
		recs = append(recs, idrIDEntities6481(runIdris(t, src, path))...)
	}
	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)
	out := map[string]string{}
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "IMPLEMENTS" {
				out[recs[i].Name] = r.ToID
			}
		}
	}
	return out
}

// TestIdrisImportPlaceholderDoesNotDropCrossFileImplements_6481 is the
// assertion that matters, and it drives the whole pipeline: two cross-file
// bare-name edges that resolve today must still resolve after an UNRELATED file
// adds one colliding import.
func TestIdrisImportPlaceholderDoesNotDropCrossFileImplements_6481(t *testing.T) {
	wantSpeaker := graph.EntityID("repo", "SCOPE.Component", "Speaker", idrBaseFile6481)

	base := idrResolveImplements6481(t, map[string]string{
		idrBaseFile6481: idrBaseSrc6481,
		idrImplFile6481: idrImplSrc6481,
	})
	// NON-VACUITY: without a resolving baseline the "after" assertion below
	// would pass on a corpus where nothing ever resolved.
	for _, owner := range []string{"Speaker Cat", "Speaker Dog"} {
		if base[owner] != wantSpeaker {
			t.Fatalf("baseline is vacuous: %q IMPLEMENTS = %q, want the real Speaker %q",
				owner, base[owner], wantSpeaker)
		}
	}

	got := idrResolveImplements6481(t, map[string]string{
		idrBaseFile6481:    idrBaseSrc6481,
		idrImplFile6481:    idrImplSrc6481,
		idrCollideFile6481: idrCollideSrc6481,
	})
	for _, owner := range []string{"Speaker Cat", "Speaker Dog"} {
		if got[owner] != wantSpeaker {
			t.Errorf("after one colliding `import Acme.Speaker` in an unrelated file: "+
				"%q IMPLEMENTS = %q, want the real interface %q — a single import "+
				"REBOUND a repo-wide bare-name edge to the import placeholder's own "+
				"entity id", owner, got[owner], wantSpeaker)
		}
	}
}

// ---------------------------------------------------------------------------
// The marker on the emitted records.
// ---------------------------------------------------------------------------

const idrProbeFile6481 = "src/Domain/Core.idr"

// The probe deliberately makes the collision real INSIDE ONE FIXTURE: the
// module `Acme.Speaker` has last segment `Speaker`, and the same file declares
// `interface Speaker`.
//
// `Data.Vect.Extra` is a 3-segment import — MULTI-SEGMENT modules are mandatory
// here, because a single-segment import cannot tell the full dotted specifier
// apart from the display name, so a mutant that stamps the display name into
// import_module would survive. `Prelude` is single-segment so the inlined
// no-dot branch is covered too, and `Data.List as L` covers the aliased form
// importRE accepts.
//
// EVERY real-declaration path the extractor has is represented — module,
// function, data, record, interface and implementation. That is not padding:
// the marker DEMOTES whatever carries it, so a construct absent from this
// fixture is a construct on which a Subtype:"import" mutant survives
// unobserved.
const idrProbeSrc6481 = `module Domain.Core

import Acme.Speaker
import Data.Vect.Extra
import Prelude
import Data.List as L

data Animal = Dog | Cat

record Pair where
  constructor MkPair

interface Speaker a where
  speak : a -> String

implementation Speaker Animal where
  speak x = "noise"

greet : String -> String
greet x = x
`

// idrImportsTarget6481 returns the module specifier the record's IMPORTS edge
// points at, and whether the record carries such an edge at all. Carrying an
// IMPORTS edge is what makes a record an import placeholder INDEPENDENTLY of
// the marker under test — so the sweep below cannot be satisfied by the very
// field it is asserting. Enumerated against the package's full source:
// `Kind: "IMPORTS"` is emitted at exactly one place (extractor.go, inside
// buildImportEntities); every other path emits CALLS, CONTAINS or IMPLEMENTS,
// so the discriminator is sound.
func idrImportsTarget6481(e types.EntityRecord) (string, bool) {
	for _, r := range e.Relationships {
		if r.Kind == "IMPORTS" {
			return r.ToID, true
		}
	}
	return "", false
}

func TestIdrisImportEmitsMarkedImportPlaceholder_6481(t *testing.T) {
	ents := runIdris(t, idrProbeSrc6481, idrProbeFile6481)

	// display name (the LAST SEGMENT — the collision-prone shape that is the
	// premise of the whole issue) -> the FULL dotted module.
	want := map[string]string{
		"Speaker": "Acme.Speaker",
		"Extra":   "Data.Vect.Extra",
		"Prelude": "Prelude",
		"List":    "Data.List",
	}

	got := map[string]string{}
	for _, e := range ents {
		target, isImport := idrImportsTarget6481(e)
		if !isImport {
			continue
		}
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			t.Errorf("import placeholder %q: Kind=%q Subtype=%q, want SCOPE.Component/import "+
				"(resolve.isImportPlaceholderKind will not recognise this record, so it stays "+
				"in the by-name index as a declaration and flips %q ambiguous)",
				e.Name, e.Kind, e.Subtype, e.Name)
		}

		// THE SPECIFIER CHANNEL (#6156). Now that the marker is stamped,
		// resolve.PruneImportPlaceholders recognises this record and
		// buildPlaceholderModuleRestores rewrites incoming edges to whatever
		// placeholderModuleSpecifier reads. Its precedence is
		// import_module > module > QualifiedName > Name, so with none of the
		// first three set it falls back to the bare last segment and renames
		// the external node from "Acme.Speaker" to "Speaker".
		if spec := e.Properties["import_module"]; spec != target {
			t.Errorf("import placeholder %q: Properties[\"import_module\"]=%q, want the full "+
				"dotted module %q — the #6156 restore will otherwise record the bare display "+
				"Name", e.Name, spec, target)
		}
		// The specifier must NOT travel on Properties["module"]: that key is
		// the module-rollup label, and BOTH module.EnsureModule and
		// stampModuleOnEntities treat an extractor-supplied value as
		// authoritative, so a dotted specifier there fabricates a Module node
		// on the never-pruning incremental path.
		if mod, ok := e.Properties["module"]; ok {
			t.Errorf("import placeholder %q pre-stamps Properties[\"module\"]=%q; that key is "+
				"the path-derived module-rollup label (module.Derive gives %q), not an import "+
				"specifier", e.Name, mod, module.Derive(e.SourceFile, nil))
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
		got[e.Name] = e.Properties["import_module"]
	}

	// NON-VACUITY. Exact equality, not a subset and not a count floor: an
	// extractor that emits no placeholder at all fails right here. This is the
	// exact shape that let the defect drift unnoticed across ~26 languages.
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

// idrFindBySubtype6481 locates a record by name AND subtype. Name alone is
// ambiguous here on purpose: `Speaker` is both the import placeholder and the
// real interface.
func idrFindBySubtype6481(ents []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

// TestIdrisRealDeclarationsDoNotAcquireImportMarker_6481 pins the NEGATIVE
// direction. The marker DEMOTES whatever carries it out of the repo-wide
// by-name index, so stamping it on a real declaration deletes that
// declaration's own name instead of protecting it — #6481's defect with the
// sign flipped, and with no loud symptom either.
//
// Without this control, a mutant that stamps Subtype:"import" on an emission
// path would satisfy the sweep above and survive.
func TestIdrisRealDeclarationsDoNotAcquireImportMarker_6481(t *testing.T) {
	ents := runIdris(t, idrProbeSrc6481, idrProbeFile6481)

	// The exclusivity sweep: nothing may carry the marker, or the specifier
	// key, except a record that actually stands in for an import.
	sawMarked := 0
	for _, e := range ents {
		_, isImport := idrImportsTarget6481(e)
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
	// check cannot be satisfied by the declaration disappearing. One row per
	// Subtype-producing emission path in the extractor.
	//
	// `Domain.Core` is the most dangerous row: a module declaration is the one
	// Idris construct whose Name is the full dotted path an `import` targets,
	// so mismarking it demotes exactly the entity `import` resolution depends
	// on.
	for _, want := range []struct{ name, subtype string }{
		{"Domain.Core", "module"},
		{"greet", "function"},
		{"Animal", "data"},
		{"Pair", "record"},
		{"Speaker", "interface"},
		{"Speaker Animal", "implementation"},
	} {
		e := idrFindBySubtype6481(ents, want.name, want.subtype)
		if e == nil {
			t.Fatalf("no entity %q with Subtype=%q; that declaration path is not emitting, "+
				"so this control is vacuous for it", want.name, want.subtype)
		}
		if _, isImport := idrImportsTarget6481(*e); isImport {
			t.Errorf("real declaration %q (subtype %q) carries an IMPORTS edge", e.Name, e.Subtype)
		}
	}
}
