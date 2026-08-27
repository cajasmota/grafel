package nim_test

// #6481 arm A3 (nim) — the import placeholder must carry the marker the
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
// buildImportEntities (nim.go) minted its per-import record with
// Kind="SCOPE.Component" but NO Subtype at all, so the predicate never
// recognised it and #6369's fix never reached Nim. Because the placeholder is
// named by the module's LAST PATH SEGMENT (importDisplayName), one
// `import acme/Animal` anywhere in the repo collided with the real
// `type Animal = object` and silently deleted every bare-name edge to it —
// repo-wide, including from files that import nothing, with no flag and nothing
// reporting it.
//
// WHY THESE ARE UNIT TESTS AND NOT A GOLDEN FIXTURE: Subtype on a bodiless stub
// is not surfaced in golden output (#6488), so a golden fixture passes
// byte-identically before and after the stamp. The effect is only observable by
// driving the REAL pipeline — extractor.Get("nim") → graph.EntityID →
// resolve.BuildIndex → resolve.ReferencesEmbedded.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/module"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const (
	nimBaseFile6481    = "src/domain/animals.nim"
	nimImplFile6481    = "src/app/impl.nim"
	nimCollideFile6481 = "src/app/collide.nim"
)

// animals.nim declares the real `Animal`. It imports nothing.
const nimBaseSrc6481 = `type
  Animal = object
    legs: int
`

// impl.nim references `Animal` by BARE NAME (Nim object construction is a call
// site, so the extractor emits CALLS → "Animal") and imports nothing at all —
// the innocent bystander of the defect.
const nimImplSrc6481 = `proc makeDog(): int =
  discard Animal(legs: 4)
  result = 1

proc makeCat(): int =
  discard Animal(legs: 4)
  result = 2
`

// The probe file: one import whose LAST SEGMENT collides with the real type
// name. Nothing else in it touches `Animal`.
const nimCollideSrc6481 = `import acme/Animal

proc unrelated(): int =
  result = 3
`

// nimIDEntities6481 assigns the production entity IDs (graph.EntityID hashes
// repo, kind, name and sourceFile — and NOT Subtype) so the resolver sees
// exactly the records the indexer would hand it.
func nimIDEntities6481(recs []types.EntityRecord) []types.EntityRecord {
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("repo", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	return recs
}

// nimResolveCalls6481 runs the real BuildIndex → ReferencesEmbedded over the
// given files and returns caller-name → resolved CALLS ToID for every edge
// whose original bare target was `want`.
func nimResolveCalls6481(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	var recs []types.EntityRecord
	// Deterministic order; collide may be absent.
	for _, path := range []string{nimBaseFile6481, nimImplFile6481, nimCollideFile6481} {
		src, ok := files[path]
		if !ok {
			continue
		}
		recs = append(recs, nimIDEntities6481(runNim(t, src, path))...)
	}
	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)
	out := map[string]string{}
	for i := range recs {
		if recs[i].SourceFile != nimImplFile6481 {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "CALLS" {
				out[recs[i].Name] = r.ToID
			}
		}
	}
	return out
}

// TestNimImportPlaceholderDoesNotDropCrossFileCalls_6481 is the assertion that
// matters, and it drives the whole pipeline: two cross-file bare-name edges
// that resolve today must still resolve after an UNRELATED file adds one
// colliding import.
func TestNimImportPlaceholderDoesNotDropCrossFileCalls_6481(t *testing.T) {
	wantAnimal := graph.EntityID("repo", "SCOPE.Component", "Animal", nimBaseFile6481)

	base := nimResolveCalls6481(t, map[string]string{
		nimBaseFile6481: nimBaseSrc6481,
		nimImplFile6481: nimImplSrc6481,
	})
	// NON-VACUITY: without a resolving baseline the "after" assertion below
	// would pass on a corpus where nothing ever resolved.
	for _, owner := range []string{"makeDog", "makeCat"} {
		if base[owner] != wantAnimal {
			t.Fatalf("baseline is vacuous: %s CALLS = %q, want the real Animal %q",
				owner, base[owner], wantAnimal)
		}
	}

	got := nimResolveCalls6481(t, map[string]string{
		nimBaseFile6481:    nimBaseSrc6481,
		nimImplFile6481:    nimImplSrc6481,
		nimCollideFile6481: nimCollideSrc6481,
	})
	for _, owner := range []string{"makeDog", "makeCat"} {
		if got[owner] != wantAnimal {
			t.Errorf("after one colliding `import acme/Animal` in an unrelated file: "+
				"%s CALLS = %q, want %q — a single import deleted a repo-wide bare-name edge",
				owner, got[owner], wantAnimal)
		}
	}
}

// ---------------------------------------------------------------------------
// The marker on the emitted records.
// ---------------------------------------------------------------------------

const nimProbeFile6481 = "src/domain/core.nim"

// The probe deliberately makes the collision real INSIDE ONE FIXTURE: the
// module `acme/Animal` has last segment `Animal`, and the same file declares
// `Animal = object`.
//
// `pkg/util/text` is a 3-segment import — MULTI-SEGMENT modules are mandatory
// here, because a single-segment import cannot tell the full module specifier
// apart from the display name, so a mutant that stamps the display name into
// import_module would survive. `asyncdispatch` is single-segment so
// importDisplayName's no-slash branch is covered too.
//
// EVERY real-declaration path the extractor has is represented — all five type
// kinds (object / ref object / enum / tuple / distinct) and every routine
// keyword procRE accepts. That is not padding: the marker DEMOTES whatever
// carries it, so a construct absent from this fixture is a construct on which a
// Subtype:"import" mutant survives unobserved. All three import FORMS
// (`import`, `include`, `from ... import`) are present because all three feed
// buildImportEntities.
const nimProbeSrc6481 = `import acme/Animal
import std/strutils, pkg/util/text
include acme/Shared
from pkg/net/http import get
import asyncdispatch

type
  Animal = object
    legs: int
  Handle = ref object
    fd: int
  Color = enum
    Red
  Pair = tuple[a: int]
  Meters = distinct int

proc doProc(): int =
  result = 1

func doFunc(): int =
  result = 1

method doMethod(self: Animal): int =
  result = 1

converter doConverter(x: int): float =
  result = 1.0

template doTemplate(): int =
  result = 1

macro doMacro(): int =
  result = 1

iterator doIterator(): int =
  result = 1
`

// nimImportsTarget6481 returns the module specifier the record's IMPORTS edge
// points at, and whether the record carries such an edge at all. Carrying an
// IMPORTS edge is what makes a record an import placeholder INDEPENDENTLY of
// the marker under test — so the sweep below cannot be satisfied by the very
// field it is asserting. Enumerated against the package's full source:
// `Kind: "IMPORTS"` is emitted at exactly one place (nim.go, inside
// buildImportEntities); every other path emits CALLS or CONTAINS, so the
// discriminator is sound.
func nimImportsTarget6481(e types.EntityRecord) (string, bool) {
	for _, r := range e.Relationships {
		if r.Kind == "IMPORTS" {
			return r.ToID, true
		}
	}
	return "", false
}

func TestNimImportEmitsMarkedImportPlaceholder_6481(t *testing.T) {
	ents := runNim(t, nimProbeSrc6481, nimProbeFile6481)

	// display name (the LAST SEGMENT — the collision-prone shape that is the
	// premise of the whole issue) -> the FULL module path.
	want := map[string]string{
		"Animal":        "acme/Animal",
		"strutils":      "std/strutils",
		"text":          "pkg/util/text",
		"Shared":        "acme/Shared",
		"http":          "pkg/net/http",
		"asyncdispatch": "asyncdispatch",
	}

	got := map[string]string{}
	for _, e := range ents {
		target, isImport := nimImportsTarget6481(e)
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
		// the external node from "acme/Animal" to "Animal".
		if spec := e.Properties["import_module"]; spec != target {
			t.Errorf("import placeholder %q: Properties[\"import_module\"]=%q, want the full "+
				"module path %q — the #6156 restore will otherwise record the bare display Name",
				e.Name, spec, target)
		}
		// The specifier must NOT travel on Properties["module"]: that key is
		// the module-rollup label, and BOTH module.EnsureModule and
		// stampModuleOnEntities treat an extractor-supplied value as
		// authoritative, so a value there fabricates a Module node on the
		// never-pruning incremental path.
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

// nimFindBySubtype6481 locates a record by name AND subtype. Name alone is
// ambiguous here on purpose: `Animal` is both the import placeholder and the
// real object type.
func nimFindBySubtype6481(ents []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

// TestNimRealDeclarationsDoNotAcquireImportMarker_6481 pins the NEGATIVE
// direction. The marker DEMOTES whatever carries it out of the repo-wide
// by-name index, so stamping it on a real declaration deletes that
// declaration's own name instead of protecting it — #6481's defect with the
// sign flipped, and with no loud symptom either.
//
// Without this control, a mutant that stamps Subtype:"import" on an emission
// path would satisfy the sweep above and survive.
func TestNimRealDeclarationsDoNotAcquireImportMarker_6481(t *testing.T) {
	ents := runNim(t, nimProbeSrc6481, nimProbeFile6481)

	// The exclusivity sweep: nothing may carry the marker, or the specifier
	// key, except a record that actually stands in for an import.
	sawMarked := 0
	for _, e := range ents {
		_, isImport := nimImportsTarget6481(e)
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
	for _, want := range []struct{ name, subtype string }{
		{"Animal", "object"},
		{"Handle", "ref object"},
		{"Color", "enum"},
		{"Pair", "tuple"},
		{"Meters", "distinct"},
		{"doProc", "proc"},
		{"doFunc", "proc"},
		{"doConverter", "proc"},
		{"doMethod", "method"},
		{"doTemplate", "template"},
		{"doMacro", "macro"},
		{"doIterator", "iterator"},
	} {
		e := nimFindBySubtype6481(ents, want.name, want.subtype)
		if e == nil {
			t.Fatalf("no entity %q with Subtype=%q; that declaration path is not emitting, "+
				"so this control is vacuous for it", want.name, want.subtype)
		}
		if _, isImport := nimImportsTarget6481(*e); isImport {
			t.Errorf("real declaration %q (subtype %q) carries an IMPORTS edge", e.Name, e.Subtype)
		}
	}
}
