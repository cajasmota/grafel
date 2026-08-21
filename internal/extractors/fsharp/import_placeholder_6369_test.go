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
// A CORRECTION TO AN EARLIER REVISION OF THIS FILE: it claimed `QualifiedName:
// name` on F# TYPE declarations was an EQUIVALENT mutant, "because a
// QualifiedName equal to Name resolves to the same entity the byName tier
// would have returned". That is false, and the reasoning inverted the tier
// order. `byQualifiedName` is a SEPARATE index that Lookup/lookupWithStatus
// probe FIRST (refs.go), and it is populated per-entity — whereas `byName`
// flips a name AMBIGUOUS as soon as two entities share it, regardless of what
// their QualifiedNames are. A bare QualifiedName therefore does not agree with
// byName, it OVERRIDES it. Measured, two entities named `Animal` (an F# type
// and a csharp type):
//
//	QualifiedName=""        Lookup("Animal") -> id="",          ok=false  (correctly ambiguous)
//	QualifiedName="Animal"  Lookup("Animal") -> id="fs-animal",  ok=true   (silent cross-language bind)
//
// It is a genuine surviving widening mutant that changes CROSS-LANGUAGE
// resolution, so it is guarded below rather than excused —
// TestFSharpEntitiesCarryNoDerivedOrIndexHijackingFields_6369. The invariant
// pinned there is narrow on purpose: no F# entity may carry a QualifiedName
// EQUAL TO ITS NAME. A genuinely namespaced value (`Domain.Animal`) would be
// an improvement and stays allowed.
//
// This test drives the real pipeline — extractor.Get("fsharp") → graph.EntityID
// → resolve.BuildIndex → resolve.ReferencesEmbedded — so it fails if the
// extractor stops stamping the marker, whatever the resolver believes.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/module"
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
// the precedence order the production reader uses
// (resolve.placeholderModuleSpecifier: import_module > module > QualifiedName
// > Name).
//
// This is a MIRROR, and a mirror can drift. It is used only for the
// per-`open` name->specifier table, where the value being asserted is the
// extractor's, not the resolver's. The claim that the PRODUCTION reader
// actually picks `import_module` up is pinned separately and without a mirror,
// by driving resolve.PruneImportPlaceholders in
// TestFSharpPlaceholderSpecifierSurvivesPrune_6369.
func fsImportSpecifier6369(e types.EntityRecord) string {
	if m := e.Properties["import_module"]; m != "" {
		return m
	}
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

// ---------------------------------------------------------------------------
// #6369 review round 3 — the fields an F# record may NOT carry.
// ---------------------------------------------------------------------------

// fsAllProbeFiles6369 is every fixture in this file, so the sweeps below run
// over the whole extractor surface (module file, namespace file, type
// declarations, members, imports) rather than one construct.
func fsAllProbeFiles6369() map[string]string {
	return map[string]string{
		fsBaseFile6369:     fsBaseSrc6369,
		fsImplFile6369:     fsImplSrc6369,
		fsCollideFile6369:  fsCollideSrc6369,
		fsModProbeFile6369: fsModProbeSrc6369,
	}
}

// TestFSharpEntitiesCarryNoDerivedOrIndexHijackingFields_6369 pins the
// invariant that TWO successive revisions of this change violated, each time by
// parking the dotted `open` specifier in a field that already means something
// else.
//
// (a) Properties["module"] IS THE MODULE-ROLLUP LABEL, NOT A SPECIFIER.
// internal/module.Derive computes it as a depth-capped path prefix of the
// source file, and both stampers treat any present value as authoritative:
// module.EnsureModule returns props unchanged when the key is set, and
// stampModuleOnEntities (internal/extractors/incremental.go) skips the entity —
// "extractor-supplied label preserved". Measured before the fix, placeholder
// "Generic" in src/Domain/Core.fs came out labelled
// module="System.Collections.Generic" where the path-derived label is
// "src/Domain".
//
// That is not cosmetic and it is NOT caught by the full rebuild, which is safe
// only by accident of ordering — cmd/grafel/index.go prunes placeholders BEFORE
// it calls EnsureModule. THE DAEMON'S INCREMENTAL REINDEX IS THE DEFAULT PATH
// (#5231) AND NEVER PRUNES: there is no PruneImportPlaceholders call anywhere in
// incremental.go. On that path each distinct `open` mints one fabricated Module
// node with CONTAINS/DEPENDS_ON edges (Group-by-Module, coverage/crossproduct,
// mcp/reachability_tools, dashboard/topology_compound), and it breaks
// stampModuleOnEntities' plain-repo label recovery, which declares `multiple`
// as soon as two distinct labels appear and falls back to doc.Repo —
// mislabelling every newly extracted entity when repoSlug != repoTag, in a
// function whose doc comment promises byte-equivalence with a full rebuild.
//
// The sweep runs over EVERY entity, not just placeholders, because the same
// mutant on TYPE declarations survived the round-2 suite — which is why the
// placeholder version shipped unnoticed.
//
// (b) A QualifiedName EQUAL TO Name hijacks the first-probed index. See the
// correction at the top of this file: `byQualifiedName` is probed before every
// other tier and is populated per-entity, so a bare QualifiedName does not
// agree with `byName`'s ambiguity, it overrides it. A genuinely namespaced
// value is fine and stays allowed.
func TestFSharpEntitiesCarryNoDerivedOrIndexHijackingFields_6369(t *testing.T) {
	saw := 0
	for path, src := range fsAllProbeFiles6369() {
		for _, e := range runFSharp(t, src, path) {
			saw++

			if got, ok := e.Properties["module"]; ok {
				t.Errorf("%s: entity %q (subtype %q) pre-stamps Properties[\"module\"]=%q; "+
					"that key is the module-rollup label (module.Derive would give %q) and "+
					"both EnsureModule and stampModuleOnEntities preserve an extractor value, "+
					"so this fabricates a Module node on the never-pruning incremental path. "+
					"An import specifier belongs on Properties[\"import_module\"].",
					path, e.Name, e.Subtype, got, module.Derive(e.SourceFile, nil))
			}

			// The same claim through the production stamper, so the guard
			// survives a rename of the key.
			props := map[string]string{}
			for k, v := range e.Properties {
				props[k] = v
			}
			want := module.Derive(e.SourceFile, nil)
			if got := module.EnsureModule(props, e.SourceFile, nil)["module"]; got != want {
				t.Errorf("%s: module.EnsureModule on entity %q returned module=%q, want the "+
					"path-derived %q — an extractor-supplied label is being preserved",
					path, e.Name, got, want)
			}

			// (c) THE EXCLUSIVITY SWEEP FOR THE NEW KEY. `import_module`
			// belongs to per-import placeholders and to nothing else. This is
			// the same widening class that let BOTH previous regressions ship:
			// the Properties["module"]-on-type-declarations mutant (X5)
			// survived round 2, and the production defect (F1) was that exact
			// shape arriving for real. The Subtype:"import" marker has had an
			// exclusivity sweep since round 2; until now the SPECIFIER channel
			// did not, so the new key inherited the identical exposure.
			//
			// Enumerated when this was written: `import_module` has exactly
			// ONE keyed reader in the tree, resolve.placeholderModuleSpecifier,
			// and BOTH of its call sites are guarded by the literal predicate
			// `r.Kind == "SCOPE.Component" && r.Subtype == "import"`. Nothing
			// anywhere prefix-matches or substring-matches property KEYS. So a
			// type declaration carrying this key cannot change any resolution,
			// mint any node, or move any edge — unlike QualifiedName (which fed
			// byQualifiedName) and Properties["module"] (which fed the module
			// rollup), both of which looked private to their consumer and were
			// not.
			//
			// It is still NOT inert, which is why this sweep exists. NO layer
			// filters unknown property keys, so every key an extractor stamps
			// is carried verbatim into (1) the persisted flatbuffer graph —
			// graph/fbwriter.buildPropertyVector sorts and writes every key it
			// is given; (2) generated documentation — docgen/tier0.go renders
			// the whole property map as `k = v` inside a fenced block; and
			// (3) MCP entity payloads, via PropRange / PropsSnapshot. A type
			// declaration carrying `import_module` therefore publishes a false
			// provenance claim — "this type is an import of X" — into the
			// graph, the docs, and every agent reading them.
			//
			// That is a LESSER severity than F1/X5, which hijacked a name index
			// and the module rollup respectively, and it is recorded as such
			// rather than inflated. But it is observable output, and it is
			// precisely the assertion the doc block on
			// placeholderModuleSpecifier already makes in prose ("the only key
			// private to this function"). An unpinned claim of that shape is
			// what has been wrong twice on this change, so it is pinned here.
			_, hasImportModule := e.Properties["import_module"]
			isPlaceholder := e.Kind == "SCOPE.Component" && e.Subtype == "import"
			if hasImportModule && !isPlaceholder {
				t.Errorf("%s: entity %q (kind %q, subtype %q) carries "+
					"Properties[\"import_module\"]=%q, but that key belongs to "+
					"per-import placeholders alone. Both readers are guarded on "+
					"Subtype==\"import\", so nothing consumes it here — it is a "+
					"false provenance claim, persisted into the graph, rendered "+
					"into docgen output and served in MCP payloads.",
					path, e.Name, e.Kind, e.Subtype, e.Properties["import_module"])
			}
			if isPlaceholder && !hasImportModule {
				t.Errorf("%s: import placeholder %q carries NO "+
					"Properties[\"import_module\"]; the specifier would fall back "+
					"to the bare display Name and the #6156 restore would record "+
					"the wrong module", path, e.Name)
			}

			if e.QualifiedName != "" && e.QualifiedName == e.Name {
				t.Errorf("%s: entity %q carries QualifiedName equal to its Name; "+
					"byQualifiedName is probed BEFORE byName and never received #6427's "+
					"placeholder precedence, so a bare QualifiedName silently overrides "+
					"byName's ambiguity (measured: a cross-language collision on `Animal` "+
					"went from ok=false to binding the F# entity)", path, e.Name)
			}
		}
	}
	if saw == 0 {
		t.Fatal("vacuous: the extractor produced no entities at all")
	}
}

// TestFSharpPlaceholderSpecifierSurvivesPrune_6369 drives the REAL prune pass
// over real F# extractor output. Round 2 asserted the prune was safe for F#
// without ever running it — no F# test exercised the path.
//
// It pins three things at once:
//
//  1. the counts: every `open` placeholder is CONSIDERED and PRUNED, and none
//     is silently kept;
//  2. the IMPORTS edges survive the prune with unchanged endpoints (the
//     round-2 safety claim, now demonstrated);
//  3. the PRODUCTION reader — resolve.placeholderModuleSpecifier, unexported
//     and reached here via the #6156 module restore inside
//     PruneImportPlaceholders — actually reads Properties["import_module"].
//     An incoming edge pointed at a placeholder id must be restored to the FULL
//     dotted specifier `System.Collections.Generic`, never to the bare display
//     Name `Generic`. Drop `import_module` from the reader's precedence and
//     this fails with the bare segment.
func TestFSharpPlaceholderSpecifierSurvivesPrune_6369(t *testing.T) {
	recs := fsIDEntities6369(runFSharp(t, fsModProbeSrc6369, fsModProbeFile6369))

	// Locate the `System.Collections.Generic` placeholder and record the
	// pre-prune IMPORTS endpoints.
	genericID := ""
	wantImports := map[string]bool{}
	for _, e := range recs {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			if e.Name == "Generic" {
				genericID = e.ID
			}
			for _, r := range e.Relationships {
				if r.Kind == "IMPORTS" {
					wantImports[r.FromID+" -> "+r.ToID] = true
				}
			}
		}
	}
	if genericID == "" {
		t.Fatal("vacuous: no placeholder named \"Generic\" was emitted")
	}
	if len(wantImports) != 3 {
		t.Fatalf("vacuous: want 3 pre-prune IMPORTS edges, got %d: %v", len(wantImports), wantImports)
	}

	// An INCOMING edge bound to the placeholder's stamped id — exactly what
	// ResolveImports leaves behind once the dotted resolver has rewritten the
	// raw module string to the placeholder entity. This is what the #6156
	// restore repairs, and the value it restores comes from the reader.
	recs = append(recs, types.EntityRecord{
		ID: "consumer", Name: "Consumer", Kind: "SCOPE.Component",
		SourceFile: "src/App/Consumer.fs", Language: "fsharp",
		Relationships: []types.RelationshipRecord{
			{FromID: "consumer", ToID: genericID, Kind: "IMPORTS"},
		},
	})

	kept, orphaned, stats := resolve.PruneImportPlaceholders(recs)

	if stats.Considered != 3 || stats.Pruned != 3 {
		t.Errorf("prune stats: considered=%d pruned=%d, want 3/3", stats.Considered, stats.Pruned)
	}
	if stats.PlaceholderKept != 0 {
		t.Errorf("prune kept %d placeholder(s); the F# placeholders must all be prunable",
			stats.PlaceholderKept)
	}

	// (2) every IMPORTS edge survives, endpoints unchanged.
	gotImports := map[string]bool{}
	collect := func(rels []types.RelationshipRecord) {
		for _, r := range rels {
			if r.Kind == "IMPORTS" && r.FromID != "consumer" {
				gotImports[r.FromID+" -> "+r.ToID] = true
			}
		}
	}
	for _, e := range kept {
		collect(e.Relationships)
	}
	collect(orphaned)
	for want := range wantImports {
		if !gotImports[want] {
			t.Errorf("prune dropped or moved IMPORTS edge %q; survivors: %v", want, gotImports)
		}
	}

	// (3) the restore read the specifier from import_module, not from Name.
	restored := ""
	for _, e := range kept {
		if e.ID != "consumer" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				restored = r.ToID
			}
		}
	}
	if restored != "System.Collections.Generic" {
		t.Errorf("the #6156 restore rewrote the incoming edge to %q, want "+
			"%q — the production reader is not picking up Properties[\"import_module\"] "+
			"(%q is the bare display Name, i.e. the Name fallback fired)",
			restored, "System.Collections.Generic", "Generic")
	}
}

// fsOpenRESrc6369 holds the two openRE shapes that round-2's suite left
// unpinned, both measured as SURVIVING mutants on lines this change touched.
const fsOpenRENegFile6369 = "src/Domain/Neg.fs"

const fsOpenRENegSrc6369 = `namespace Domain

// X3: ` + "`open type`" + ` is ` + "`type` FOLLOWED BY WHITESPACE" + `. Relaxing the
// added (?:type\s+)? to (?:type\s*)? makes the next line yield a placeholder
// for "Utils.Helpers" — a module that does not exist.
open typeUtils.Helpers

type Marker() = class end
`

const fsOpenREAnchorFile6369 = "src/Domain/Anchor.fs"

// X2: openRE's ^[ \t]* anchor. Drop it and `let reopen Foo` mints a
// placeholder for "Foo", because "open" occurs mid-identifier.
const fsOpenREAnchorSrc6369 = `namespace Domain

let reopen Foo = Foo

type Marker() = class end
`

// TestFSharpOpenRENegatives_6369 pins the two directions of openRE this change
// touched but only narrowed. Both mutants SURVIVED the round-2 suite.
func TestFSharpOpenRENegatives_6369(t *testing.T) {
	for _, tc := range []struct {
		name, file, src, forbidden string
	}{
		{
			name: "X3/type-keyword-needs-whitespace",
			file: fsOpenRENegFile6369, src: fsOpenRENegSrc6369,
			// `open typeUtils.Helpers` imports the module `typeUtils.Helpers`,
			// so the display name is "Helpers" and the specifier keeps the
			// `type` prefix. What must NEVER appear is the `type`-eaten
			// reading, whose specifier is "Utils.Helpers".
			forbidden: "Utils.Helpers",
		},
		{
			name: "X2/leading-anchor",
			file: fsOpenREAnchorFile6369, src: fsOpenREAnchorSrc6369,
			forbidden: "Foo",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, e := range runFSharp(t, tc.src, tc.file) {
				if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
					continue
				}
				if got := fsImportSpecifier6369(e); got == tc.forbidden {
					t.Errorf("openRE minted an import placeholder for %q from %q — "+
						"a module that does not exist", got, tc.file)
				}
			}
		})
	}

	// Non-vacuity: the X3 fixture DOES import something, it is just not
	// `Utils.Helpers`. Without this the test passes on an extractor that
	// stopped recognising `open` altogether.
	sawX3 := false
	for _, e := range runFSharp(t, fsOpenRENegSrc6369, fsOpenRENegFile6369) {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" &&
			fsImportSpecifier6369(e) == "typeUtils.Helpers" {
			sawX3 = true
		}
	}
	if !sawX3 {
		t.Error("vacuous: `open typeUtils.Helpers` produced no placeholder for " +
			"\"typeUtils.Helpers\"; openRE is not matching at all")
	}
}
