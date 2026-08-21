package fsharp_test

// #6369 (F# arm) — an F# `open` must not delete a type's inheritance topology
// repo-wide.
//
// #6427 taught BuildIndex that an import placeholder is not a declaration of
// the name it carries, so a real declaration outranks it instead of flipping
// the name ambiguous. That precedence is keyed on the placeholder marker
// `Kind=="SCOPE.Component" && Subtype=="import"` (resolve/refs.go,
// isImportPlaceholderKind) — the marker a dozen extractors stamp.
//
// The F# extractor emitted its per-`open` placeholder with NO Subtype at all,
// so the predicate never recognised it and the #6369 defect stayed live for
// F#: one `open Acme.Animal` in one file dropped every bare-name EXTENDS to
// `Animal` across the whole repo, including in files that import nothing.
//
// This test drives the real pipeline — extractor.Get("fsharp") → graph.EntityID
// → resolve.BuildIndex → resolve.ReferencesEmbedded — so it fails if the
// extractor stops stamping the marker, whatever the resolver believes.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const (
	fsBaseFile6369    = "src/Domain/Base.fs"
	fsImplFile6369    = "src/App/Impl.fs"
	fsCollideFile6369 = "src/Domain/Collide.fs"
)

// Base.fs declares the real `Animal`. Impl.fs derives from it by bare name and
// imports nothing — the innocent bystander of the defect.
const fsBaseSrc6369 = `namespace Domain

type Animal() =
    member _.Speak () = ()
`

const fsImplSrc6369 = `namespace App

type Dog() =
    inherit Animal()

type Service() =
    inherit Animal()
`

// The probe file: one `open` whose LAST SEGMENT collides with the real type
// name, plus a local derivation so the file is otherwise ordinary.
const fsCollideSrc6369 = `namespace Domain

open Acme.Animal

type Robot() =
    inherit Animal()
`

// fsIDEntities assigns the production entity IDs (graph.EntityID hashes repo,
// kind, name, sourceFile — and NOT Subtype) so the resolver sees exactly the
// records the indexer would hand it.
func fsIDEntities6369(recs []types.EntityRecord) []types.EntityRecord {
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("repo", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	return recs
}

// fsResolveExtends6369 runs BuildIndex → ReferencesEmbedded over the given
// files and returns owner-name → resolved EXTENDS ToID.
func fsResolveExtends6369(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	var recs []types.EntityRecord
	// Deterministic order: base, impl, collide (collide may be absent).
	for _, path := range []string{fsBaseFile6369, fsImplFile6369, fsCollideFile6369} {
		src, ok := files[path]
		if !ok {
			continue
		}
		recs = append(recs, fsIDEntities6369(runFSharp(t, src, path))...)
	}
	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)
	out := map[string]string{}
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "EXTENDS" {
				out[recs[i].Name] = r.ToID
			}
		}
	}
	return out
}

// TestFSharpImportPlaceholderDoesNotDropCrossFileExtends_6369 is the assertion
// that matters: two cross-file EXTENDS that resolve today must still resolve
// after an unrelated file adds a colliding `open`.
func TestFSharpImportPlaceholderDoesNotDropCrossFileExtends_6369(t *testing.T) {
	wantAnimal := graph.EntityID("repo", "SCOPE.Component", "Animal", fsBaseFile6369)

	base := fsResolveExtends6369(t, map[string]string{
		fsBaseFile6369: fsBaseSrc6369,
		fsImplFile6369: fsImplSrc6369,
	})
	for _, owner := range []string{"Dog", "Service"} {
		if base[owner] != wantAnimal {
			t.Fatalf("baseline is vacuous: %s EXTENDS = %q, want real Animal %q",
				owner, base[owner], wantAnimal)
		}
	}

	got := fsResolveExtends6369(t, map[string]string{
		fsBaseFile6369:    fsBaseSrc6369,
		fsImplFile6369:    fsImplSrc6369,
		fsCollideFile6369: fsCollideSrc6369,
	})
	for _, owner := range []string{"Dog", "Service"} {
		if got[owner] != wantAnimal {
			t.Errorf("after one colliding `open Acme.Animal`: %s EXTENDS = %q, want %q "+
				"(a single import deleted a repo-wide inheritance edge)",
				owner, got[owner], wantAnimal)
		}
	}
}

// The marker itself, asserted directly on the extractor's output: the record
// minted per `open` must carry Kind=SCOPE.Component AND Subtype="import", the
// shape resolve.isImportPlaceholderKind recognises. Without this the resolver
// test above can only fail long after the cause.
func TestFSharpOpenEmitsMarkedImportPlaceholder_6369(t *testing.T) {
	ents := runFSharp(t, fsCollideSrc6369, fsCollideFile6369)

	var found int
	for _, e := range ents {
		hasImports := false
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				hasImports = true
			}
		}
		if !hasImports {
			continue
		}
		found++
		// The premise of the whole issue: the placeholder is named after the
		// module's LAST SEGMENT, which is why it can collide with a type name.
		// Pinned so the resolver test above cannot go vacuously green on a
		// change that quietly names placeholders after the full path (mutation
		// -measured: `Name: mod` survived the package suite).
		if e.Name != "Animal" {
			t.Errorf("import placeholder Name = %q, want the last segment %q", e.Name, "Animal")
		}
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			t.Errorf("import placeholder %q: Kind=%q Subtype=%q, want SCOPE.Component/import",
				e.Name, e.Kind, e.Subtype)
		}
		// The specifier readers (#6372: Properties["module"] > QualifiedName >
		// Name) must be able to recover the FULL module path; Name is only the
		// last segment.
		if e.QualifiedName != "Acme.Animal" && e.Properties["module"] != "Acme.Animal" {
			t.Errorf("import placeholder %q carries no full module specifier: "+
				"QualifiedName=%q Properties[module]=%q, want Acme.Animal",
				e.Name, e.QualifiedName, e.Properties["module"])
		}
	}
	if found != 1 {
		t.Fatalf("got %d import-carrying entities, want 1", found)
	}

	// The marker is EXCLUSIVE to the placeholder. It demotes whatever carries
	// it out of the repo-wide by-name index, so stamping it on a real
	// declaration — the type, the namespace, the module — deletes that
	// declaration's own name instead of protecting it. Mutation-measured: the
	// assertions above alone let `namespace Domain` be mismarked "import" and
	// the whole package suite stayed green.
	for _, e := range ents {
		if e.Subtype != "import" {
			continue
		}
		carriesImports := false
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				carriesImports = true
			}
		}
		if !carriesImports {
			t.Errorf("entity %q (Kind=%q) is marked Subtype=\"import\" but carries no "+
				"IMPORTS edge — the placeholder marker must not land on a real declaration",
				e.Name, e.Kind)
		}
	}
}
