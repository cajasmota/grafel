package zig_test

// #6481 arm A3 (zig) — the import placeholder must carry the marker the
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
// buildImportEntities (zig.go) minted its per-`@import` record with
// Kind="SCOPE.Component" but NO Subtype at all, so the predicate never
// recognised it and #6369's fix never reached Zig. Because the placeholder is
// named by importTopSegment — the BASENAME WITHOUT EXTENSION — one
// `@import("acme/animal.zig")` anywhere in the repo collided with a real
// `pub fn animal` and silently deleted every bare-name edge to it, repo-wide,
// including from files that import nothing.
//
// WHY THESE ARE UNIT TESTS AND NOT A GOLDEN FIXTURE: Subtype on a bodiless stub
// is not surfaced in golden output (#6488), so a golden fixture passes
// byte-identically before and after the stamp. The effect is only observable by
// driving the REAL pipeline — extractor.Get("zig") → graph.EntityID →
// resolve.BuildIndex → resolve.ReferencesEmbedded.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/module"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const (
	zigBaseFile6481    = "src/domain/base.zig"
	zigImplFile6481    = "src/app/impl.zig"
	zigCollideFile6481 = "src/app/collide.zig"
)

// base.zig declares the real `Animal` struct AND the real `animal` function.
// It imports nothing.
//
// TWO collision targets on purpose, because they are demoted through different
// tiers: `Animal` (SCOPE.Component) shares a kind with the placeholder and is
// the one the by-name index actually loses, while `animal` (SCOPE.Operation)
// is the control that must keep resolving.
const zigBaseSrc6481 = `pub const Animal = struct {
    legs: u8,
};

pub fn animal() u8 {
    return 1;
}
`

// impl.zig calls `animal` by BARE NAME twice and imports nothing at all — the
// innocent bystander of the defect.
const zigImplSrc6481 = `pub fn makeDog() u8 {
    return animal();
}

pub fn makeCat() u8 {
    return animal();
}
`

// The probe file: two `@import`s whose BASENAME-WITHOUT-EXTENSION collides with
// a real declaration in base.zig. Nothing else in it touches either name.
const zigCollideSrc6481 = `const acme = @import("acme/animal.zig");
const pkg = @import("pkg/Animal.zig");

pub fn unrelated() u8 {
    return 2;
}
`

// zigIDEntities6481 assigns the production entity IDs (graph.EntityID hashes
// repo, kind, name and sourceFile — and NOT Subtype) so the resolver sees
// exactly the records the indexer would hand it.
func zigIDEntities6481(recs []types.EntityRecord) []types.EntityRecord {
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("repo", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	return recs
}

// zigResolveCalls6481 runs the real BuildIndex → ReferencesEmbedded over the
// given files and returns caller-name → resolved CALLS ToID for callers in
// impl.zig.
func zigResolveCalls6481(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	var recs []types.EntityRecord
	// Deterministic order; collide may be absent.
	for _, path := range []string{zigBaseFile6481, zigImplFile6481, zigCollideFile6481} {
		src, ok := files[path]
		if !ok {
			continue
		}
		recs = append(recs, zigIDEntities6481(runZig(t, src, path))...)
	}
	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)
	out := map[string]string{}
	for i := range recs {
		if recs[i].SourceFile != zigImplFile6481 {
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

// zigBuildIndex6481 runs the production BuildIndex over the given files.
func zigBuildIndex6481(t *testing.T, files map[string]string) resolve.Index {
	t.Helper()
	var recs []types.EntityRecord
	for _, path := range []string{zigBaseFile6481, zigImplFile6481, zigCollideFile6481} {
		src, ok := files[path]
		if !ok {
			continue
		}
		recs = append(recs, zigIDEntities6481(runZig(t, src, path))...)
	}
	return resolve.BuildIndex(recs)
}

// TestZigImportPlaceholderDoesNotDeleteRepoWideName_6481 is the assertion that
// matters for Zig, observed on the production by-name index.
//
// The Zig extractor emits no bare-name edge that targets a SCOPE.Component, so
// the damage does not show up on a Zig-to-Zig CALLS edge — it shows up one
// layer down, where the placeholder and the real `const Animal = struct` share
// BOTH the name and the kind. resolve.Index.Lookup is the tier every bare-name
// reference in the repo goes through, in EVERY language, so a name lost here is
// lost for every consumer of that name repo-wide.
func TestZigImportPlaceholderDoesNotDeleteRepoWideName_6481(t *testing.T) {
	wantStruct := graph.EntityID("repo", "SCOPE.Component", "Animal", zigBaseFile6481)

	// NON-VACUITY: the name must resolve to the real struct BEFORE the
	// colliding import exists, or the "after" assertion proves nothing.
	baseIdx := zigBuildIndex6481(t, map[string]string{
		zigBaseFile6481: zigBaseSrc6481,
		zigImplFile6481: zigImplSrc6481,
	})
	gotID, ok := baseIdx.Lookup("Animal")
	if !ok || gotID != wantStruct {
		t.Fatalf("baseline is vacuous: Lookup(\"Animal\") = (%q, %v), want the real struct %q",
			gotID, ok, wantStruct)
	}

	idx := zigBuildIndex6481(t, map[string]string{
		zigBaseFile6481:    zigBaseSrc6481,
		zigImplFile6481:    zigImplSrc6481,
		zigCollideFile6481: zigCollideSrc6481,
	})
	gotID, ok = idx.Lookup("Animal")
	if !ok || gotID != wantStruct {
		t.Errorf("after one colliding `@import(\"pkg/Animal.zig\")` in an unrelated file: "+
			"Lookup(\"Animal\") = (%q, %v), want the real struct %q — the placeholder is "+
			"indexed as a declaration, so the name went AMBIGUOUS and every bare-name edge "+
			"to it is dropped repo-wide", gotID, ok, wantStruct)
	}
}

// TestZigImportPlaceholderDoesNotDropCrossFileCalls_6481 is the NEGATIVE
// direction, driven end-to-end: two cross-file bare-name CALLS edges that
// resolve today must STILL resolve after the stamp. Marking the placeholder is
// a demotion, and a demotion applied too widely — or to the wrong record —
// silently deletes a real declaration's own name with no loud symptom.
func TestZigImportPlaceholderDoesNotDropCrossFileCalls_6481(t *testing.T) {
	wantAnimal := graph.EntityID("repo", "SCOPE.Operation", "animal", zigBaseFile6481)

	base := zigResolveCalls6481(t, map[string]string{
		zigBaseFile6481: zigBaseSrc6481,
		zigImplFile6481: zigImplSrc6481,
	})
	// NON-VACUITY: without a resolving baseline the "after" assertion below
	// would pass on a corpus where nothing ever resolved.
	for _, owner := range []string{"makeDog", "makeCat"} {
		if base[owner] != wantAnimal {
			t.Fatalf("baseline is vacuous: %s CALLS = %q, want the real animal %q",
				owner, base[owner], wantAnimal)
		}
	}

	got := zigResolveCalls6481(t, map[string]string{
		zigBaseFile6481:    zigBaseSrc6481,
		zigImplFile6481:    zigImplSrc6481,
		zigCollideFile6481: zigCollideSrc6481,
	})
	for _, owner := range []string{"makeDog", "makeCat"} {
		if got[owner] != wantAnimal {
			t.Errorf("after one colliding `@import(\"acme/animal.zig\")` in an unrelated file: "+
				"%s CALLS = %q, want %q — a single import deleted a repo-wide bare-name edge",
				owner, got[owner], wantAnimal)
		}
	}
}

// ---------------------------------------------------------------------------
// The marker on the emitted records.
// ---------------------------------------------------------------------------

const zigProbeFile6481 = "src/domain/core.zig"

// The probe deliberately makes the collision real INSIDE ONE FIXTURE: the
// import `pkg/Animal.zig` has basename `Animal`, and the same file declares
// `const Animal = struct`.
//
// Every SEPARATOR AND EXTENSION variant importTopSegment handles is
// represented, because the display name is what collides:
//
//	"std"                     bare stdlib token, no separator, no extension
//	"acme/animal.zig"         slash path + extension
//	"./util/text.zig"         leading "./" strip
//	"../lib/deep/core.zig"    leading "../" strip, 3 path segments
//	"pkg/Animal.zig"          the colliding one
//
// MULTI-SEGMENT paths are mandatory: a bare single-segment import cannot tell
// the full specifier apart from the display name, so a mutant that stamps the
// display name into import_module would survive a std-only fixture.
//
// EVERY real-declaration path the extractor has is represented — pub fn, plain
// fn, a struct, and a method inside a struct body. That is not padding: the
// marker DEMOTES whatever carries it, so a construct absent from this fixture
// is a construct on which a Subtype:"import" mutant survives unobserved.
const zigProbeSrc6481 = `const std = @import("std");
const acme = @import("acme/animal.zig");
const text = @import("./util/text.zig");
const core = @import("../lib/deep/core.zig");
const pkg = @import("pkg/Animal.zig");

pub const Animal = struct {
    legs: u8,

    pub fn speak(self: Animal) u8 {
        return self.legs;
    }
};

pub fn doPub() u8 {
    return 1;
}

fn doPriv() u8 {
    return 2;
}
`

// zigImportsTarget6481 returns the module specifier the record's IMPORTS edge
// points at, and whether the record carries such an edge at all. Carrying an
// IMPORTS edge is what makes a record an import placeholder INDEPENDENTLY of
// the marker under test — so the sweep below cannot be satisfied by the very
// field it is asserting. Enumerated against the package's full source:
// `Kind: "IMPORTS"` is emitted at exactly one place (zig.go, inside
// buildImportEntities); every other path emits CALLS or CONTAINS, so the
// discriminator is sound.
func zigImportsTarget6481(e types.EntityRecord) (string, bool) {
	for _, r := range e.Relationships {
		if r.Kind == "IMPORTS" {
			return r.ToID, true
		}
	}
	return "", false
}

func TestZigImportEmitsMarkedImportPlaceholder_6481(t *testing.T) {
	ents := runZig(t, zigProbeSrc6481, zigProbeFile6481)

	// display name (importTopSegment's basename-without-extension — the
	// collision-prone shape that is the premise of the whole issue) -> the FULL
	// import target, verbatim.
	want := map[string]string{
		"std":    "std",
		"animal": "acme/animal.zig",
		"text":   "./util/text.zig",
		"core":   "../lib/deep/core.zig",
		"Animal": "pkg/Animal.zig",
	}

	got := map[string]string{}
	for _, e := range ents {
		target, isImport := zigImportsTarget6481(e)
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
		// first three set it falls back to the bare basename and renames the
		// external node from "acme/animal.zig" to "animal".
		if spec := e.Properties["import_module"]; spec != target {
			t.Errorf("import placeholder %q: Properties[\"import_module\"]=%q, want the full "+
				"import target %q — the #6156 restore will otherwise record the bare display "+
				"Name", e.Name, spec, target)
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

// zigFindBySubtype6481 locates a record by name AND subtype. Name alone is
// ambiguous here on purpose: `Animal` is both the import placeholder and the
// real struct.
func zigFindBySubtype6481(ents []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

// TestZigRealDeclarationsDoNotAcquireImportMarker_6481 pins the NEGATIVE
// direction. The marker DEMOTES whatever carries it out of the repo-wide
// by-name index, so stamping it on a real declaration deletes that
// declaration's own name instead of protecting it — #6481's defect with the
// sign flipped, and with no loud symptom either.
//
// Without this control, a mutant that stamps Subtype:"import" on an emission
// path would satisfy the sweep above and survive.
func TestZigRealDeclarationsDoNotAcquireImportMarker_6481(t *testing.T) {
	ents := runZig(t, zigProbeSrc6481, zigProbeFile6481)

	// The exclusivity sweep: nothing may carry the marker, or the specifier
	// key, except a record that actually stands in for an import.
	sawMarked := 0
	for _, e := range ents {
		_, isImport := zigImportsTarget6481(e)
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
		{"Animal", "struct"},
		{"speak", "function"},
		{"doPub", "function"},
		{"doPriv", "function"},
	} {
		e := zigFindBySubtype6481(ents, want.name, want.subtype)
		if e == nil {
			t.Fatalf("no entity %q with Subtype=%q; that declaration path is not emitting, "+
				"so this control is vacuous for it", want.name, want.subtype)
		}
		if _, isImport := zigImportsTarget6481(*e); isImport {
			t.Errorf("real declaration %q (subtype %q) carries an IMPORTS edge", e.Name, e.Subtype)
		}
	}
}
