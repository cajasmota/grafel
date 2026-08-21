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
// NOT GUARDED, deliberately: adding `QualifiedName: name` to F# TYPE
// declarations survives every suite. It was measured, not assumed — with the
// specifier off the placeholder (below), that mutant produced byte-identical
// IMPORTS and EXTENDS output on all four probes, including the #6369 collide
// scenario, because a QualifiedName equal to Name resolves to the same entity
// the byName tier would have returned. It is an equivalent mutant inside this
// change's blast radius, and it is a type-declaration concern, not an import
// one. A guard here would be decorative, so there isn't one.
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

const fsModProbeFile6369 = "src/Domain/Core.fs"

// The extractor-level probe. It is a top-level `module` file on purpose: a
// `module` declaration is the ONE F# construct whose Name is the full dotted
// path an `open` targets, so it is both the most dangerous thing to mismark
// Subtype="import" (that DEMOTES it out of byName, breaking exactly the
// resolution `open` depends on) and the thing openRE must never match.
// It carries a 3-segment `open` and an F# 5 `open type` alongside the
// 2-segment one, because every realistic F# open is 3+ segments.
const fsModProbeSrc6369 = `module Domain.Core

open Acme.Animal
open System.Collections.Generic
open type Acme.Units

type Robot() =
    inherit Animal()
`

// fsImportSpecifier6369 reads the module specifier a placeholder carries, in
// the #6372 precedence order the production readers use
// (resolve.placeholderModuleSpecifier).
func fsImportSpecifier6369(e types.EntityRecord) string {
	if m := e.Properties["module"]; m != "" {
		return m
	}
	if e.QualifiedName != "" {
		return e.QualifiedName
	}
	return e.Name
}

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
//
// It runs over fsModProbeSrc6369, a top-level `module` file, so that the
// exclusivity sweep at the bottom actually has a `module` declaration to sweep
// — mutation-measured: against a `namespace`-only fixture, stamping
// Subtype:"import" on the module-declaration record survived all three
// packages.
func TestFSharpOpenEmitsMarkedImportPlaceholder_6369(t *testing.T) {
	ents := runFSharp(t, fsModProbeSrc6369, fsModProbeFile6369)

	// name (the LAST SEGMENT — the premise of the whole issue, and why a
	// placeholder can collide with a type name) → full module specifier.
	//
	// The 3-segment and `open type` rows are load-bearing, not padding:
	//   - "Generic" pins importDisplayName for the SHAPE THAT ACTUALLY OCCURS.
	//     Mutation-measured: widening it to return the full path whenever
	//     `mod` has >=2 dots survived a 2-segment-only fixture, and every
	//     realistic F# open (System.Collections.Generic, Microsoft.FSharp.Core)
	//     is 3+ segments.
	//   - "Units" pins the F# 5 `open type` form. Before this commit openRE
	//     captured the literal keyword `type`, minting a placeholder named
	//     "type" — junk that the new marker would have promoted to a
	//     *recognised* placeholder.
	want := map[string]string{
		"Animal":  "Acme.Animal",
		"Generic": "System.Collections.Generic",
		"Units":   "Acme.Units",
	}

	got := map[string]string{}
	for _, e := range ents {
		carriesImports := false
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				carriesImports = true
			}
		}
		if !carriesImports {
			continue
		}
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			t.Errorf("import placeholder %q: Kind=%q Subtype=%q, want SCOPE.Component/import",
				e.Name, e.Kind, e.Subtype)
		}
		// #6369 round 2 — THE PLACEHOLDER MUST NOT SET QualifiedName. That
		// index is probed before every other tier and, unlike byName, has no
		// #6427 import-placeholder precedence, so the full module path there
		// hijacks this record's own IMPORTS edge. See
		// TestFSharpOpenImportsEdgeBindsToRealModule_6369.
		if e.QualifiedName != "" {
			t.Errorf("import placeholder %q sets QualifiedName=%q; the specifier "+
				"must travel on Properties[\"module\"] so the record stays out of "+
				"byQualifiedName", e.Name, e.QualifiedName)
		}
		if _, dup := got[e.Name]; dup {
			t.Errorf("duplicate import placeholder for %q", e.Name)
		}
		got[e.Name] = fsImportSpecifier6369(e)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d import placeholders %v, want %d %v", len(got), got, len(want), want)
	}
	for name, spec := range want {
		if got[name] != spec {
			t.Errorf("import placeholder %q: specifier = %q, want %q", name, got[name], spec)
		}
	}

	// The file's OWN `module Domain.Core` declaration must survive as a real,
	// unmarked declaration. Mutation-measured, two ways:
	//   - widening openRE to `(?:open|module)` mints a placeholder for the
	//     module line too (caught by the count above, and by "Core" here);
	//   - stamping Subtype:"import" on the module-declaration record demotes
	//     the one entity whose Name is the full dotted path an `open` targets
	//     (caught by the exclusivity sweep below).
	var modDecl *types.EntityRecord
	for i := range ents {
		if ents[i].Name == "Domain.Core" {
			modDecl = &ents[i]
		}
	}
	if modDecl == nil {
		t.Fatalf("no entity for the file's own `module Domain.Core` declaration")
	}
	if modDecl.Subtype != "module" {
		t.Errorf("module declaration Domain.Core: Subtype=%q, want %q", modDecl.Subtype, "module")
	}

	// The marker is EXCLUSIVE to the placeholder. It demotes whatever carries
	// it out of the repo-wide by-name index, so stamping it on a real
	// declaration — the type, the namespace, the module — deletes that
	// declaration's own name instead of protecting it.
	//
	// Swept over BOTH probe fixtures because F# top-level scoping is either/or:
	// the module fixture has no `namespace` line and the collide fixture has no
	// `module` line, so either one alone leaves the other construct free to be
	// mismarked. Mutation-measured: each mutant survives the fixture that lacks
	// its construct.
	for _, fx := range []struct{ path, src string }{
		{fsModProbeFile6369, fsModProbeSrc6369},
		{fsCollideFile6369, fsCollideSrc6369},
	} {
		for _, e := range runFSharp(t, fx.src, fx.path) {
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
				t.Errorf("%s: entity %q (Kind=%q) is marked Subtype=\"import\" but carries "+
					"no IMPORTS edge — the placeholder marker must not land on a real declaration",
					fx.path, e.Name, e.Kind)
			}
		}
	}
}

// TestFSharpOpenImportsEdgeBindsToRealModule_6369 is the round-2 regression
// guard for the specifier channel.
//
// `open Acme.Animal` against an in-repo `module Acme.Animal` must resolve its
// IMPORTS edge to that real module entity. Measured three ways on this exact
// input:
//
//	main (no marker, no specifier)   -> 7409c1a21efbbed7 (the module)  OK
//	Properties["module"] (this fix)  -> 7409c1a21efbbed7 (the module)  OK
//	QualifiedName                    -> this file's own placeholder    BAD
//
// The QualifiedName row is why the specifier is not stored there: byQualifiedName
// is probed ahead of every other tier and has no #6427 placeholder precedence,
// so the placeholder shadows the declaration it is pointing at.
func TestFSharpOpenImportsEdgeBindsToRealModule_6369(t *testing.T) {
	const modFile = "src/Acme/Animal.fs"
	const modSrc = `module Acme.Animal

type Animal() =
    member _.Speak () = ()
`
	const useFile = "src/App/Use.fs"
	const useSrc = `namespace App

open Acme.Animal
`

	var recs []types.EntityRecord
	for _, f := range [][2]string{{modFile, modSrc}, {useFile, useSrc}} {
		recs = append(recs, fsIDEntities6369(runFSharp(t, f[1], f[0]))...)
	}
	wantModule := ""
	for _, e := range recs {
		if e.Name == "Acme.Animal" && e.SourceFile == modFile {
			wantModule = e.ID
		}
	}
	if wantModule == "" {
		t.Fatalf("baseline is vacuous: no in-repo entity for `module Acme.Animal`")
	}

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	found := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			found++
			if r.ToID != wantModule {
				t.Errorf("IMPORTS from %s: ToID = %q, want the real `module Acme.Animal` %q "+
					"(a placeholder in byQualifiedName shadows the module it imports)",
					r.FromID, r.ToID, wantModule)
			}
			if r.ToID == recs[i].ID {
				t.Errorf("IMPORTS from %s bound to the placeholder's OWN id %q — "+
					"a fabricated intra-file dependency", r.FromID, r.ToID)
			}
		}
	}
	if found != 1 {
		t.Fatalf("got %d IMPORTS edges, want 1", found)
	}
}
