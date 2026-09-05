package fsharp_test

// file_carrier_6852_test.go — #6852, fsharp arm.
//
// buildImportEntities (extractor.go) stamps `FromID: filePath` on the IMPORTS
// edge of every `open` placeholder, and NOTHING this extractor emits is named
// after the file: every record is named after a module, namespace, type,
// operation, DU case or active pattern. internal/resolve/refs.go has no
// path→entity index, so a path-valued FromID resolves if and only if some
// emitted node carries that exact string as its Name — nothing did, so the raw
// path reached the graph as the edge's FROM end. Same defect #6815 fixed in
// erlang/nim/groovy and #6852 fixed in bicep (#6864), terraform (#6871) and
// html; same fix, extractor.PrependFileCarrier.
//
// ONE ANCHORING SITE, MANY EDGES. buildImportEntities is the only producer of a
// bare `FromID: path` in the package (collectHierarchyEdges deliberately leaves
// FromID EMPTY — #6295 — and elmish_feliz.go's RENDERS/USES edges leave it
// empty too). But it emits ONE record per distinct `open`, each carrying its
// own path-anchored IMPORTS, so a real F# file anchors N edges and must still
// gain exactly ONE carrier.
//
// BOTH DEPTHS DANGLED, unlike terraform. hcl already emitted a file component
// named BASENAME(path), so its ROOT case resolved by the accident #6367
// documents and only nested files dangled. fsharp emits no file-scoped record
// of any kind, so `Core.fs` and `src/Domain/Core.fs` both dangled — the same
// shape as html.
//
// THE Subtype HAZARD (#6369 / PR #6480), stated because this is the language
// where it lives. graph.EntityID hashes (repo, kind, name, sourceFile) and NOT
// Subtype, so two SCOPE.Component records with the same Name and SourceFile
// collide on one id however their Subtype differs. Two record shapes could in
// principle be named after the file:
//
//   - the IMPORT MARKER (Subtype "import"). It CANNOT be: its Name is
//     importDisplayName(mod), the last dot-segment of an `open` target, while
//     every fsharp path ends in `.fs`/`.fsi`/`.fsx`/`.fsproj` and therefore
//     contains a dot — so the marker's name is at most the extension, never the
//     path. WHERE THAT LOAD-BEARING FACT LIVES is worth stating, because it is
//     not here: importDisplayName returns mod UNCHANGED when it has no dot, so
//     this is not an argument about the extractor at all. It reduces to "every
//     path routed to fsharp contains a dot", which holds because
//     internal/classifier/classifier.go:428-431 is what maps
//     .fs/.fsi/.fsx/.fsproj to this extractor. A dotless extension mapped to
//     fsharp later would break the premise there, not in this package.
//     Pinned by TestFSharp_ImportMarkerIsNeverThePathNamedRecord_6852.
//   - the MODULE / NAMESPACE record (Subtype "module"/"namespace"). It CAN be:
//     moduleRE captures `[\w.]+`, so a root file `Core.fs` declaring
//     `module Core.fs` emits a record named exactly the path. That is
//     FileCarrierFor CLAUSE 3 (`records[i].Name == path`; clause 1 is the
//     empty-path guard) — NOT anything Subtype-aware — and it is what stops a
//     second node landing under the one id. Pinned by
//     TestFSharp_ModuleNamedLikeThePathGetsNoSecondCarrier_6852.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .fs file across a whole repo — a change no recall-shaped assertion can see.
// The forbidden-row controls below are what forbid it.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// fsCarriers6852 returns every record that IS the file carrier for path — the
// SCOPE.Component/file record extractor.FileEntity mints.
func fsCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// fsPathAnchored6852 returns every relationship in recs whose FromID is exactly
// path — the shape whose FROM end has nothing to resolve onto.
func fsPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.FromID == path {
				out = append(out, r)
			}
		}
	}
	return out
}

// fsNamedExactly6852 returns every record whose Name or QualifiedName is path.
// This is the resolution question internal/resolve/refs.go actually asks: it
// has no path→entity index, so a path-valued FromID resolves if and only if
// such a record exists.
func fsNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// carrierSrcFSharp6852 declares ONE `open`, so the only path-anchored edge in
// the extraction is the IMPORTS this issue is about. The module declaration and
// the let binding are there so the file is not degenerate — the fixture must
// extract a full record set, or the "no carrier" controls below could pass for
// the wrong reason.
const carrierSrcFSharp6852 = `module Acme.Domain.Core

open System.Text

let helper x = x + 1
`

// resolveFSharp6852 extracts src at path, stamps ids the way graph assembly
// does, runs the production resolver pipeline, and returns the records plus the
// id→record index. This asserts on the EMITTED ARTEFACT after resolution — the
// edge's FROM end — not on a helper's return value or a counter.
func resolveFSharp6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := runFSharp(t, src, path)
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6852", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	return recs, byID
}

// TestFSharp_OpenImportsFromEndResolves_6852 is the fix's behavioural test.
// Axis VARIED: path DEPTH (nested and root). HELD CONSTANT: the source. fsharp
// names no record after its containing file at either depth, so both dangled
// before the carrier and neither depth can carry the other.
func TestFSharp_OpenImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"src/Domain/Core.fs", "Core.fs"} {
		t.Run(path, func(t *testing.T) {
			recs, byID := resolveFSharp6852(t, carrierSrcFSharp6852, path)

			seen := 0
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind != "IMPORTS" {
						continue
					}
					seen++
					if _, ok := byID[r.FromID]; !ok {
						t.Errorf("IMPORTS owned by %q: FROM end %q resolves to no record "+
							"(refs.go has no path→entity index; a path-valued FromID "+
							"resolves iff some record carries that exact string as its "+
							"Name — emit a file carrier, internal/extractor/file_carrier.go)",
							recs[i].Name, r.FromID)
					}
				}
			}
			if seen == 0 {
				t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
			}
		})
	}
}

// TestFSharp_SignatureFileImportsFromEndResolves_6852 varies the FILE FORM: a
// `.fsi` signature file routes to this same extractor (classifier.go,
// substrate.go) and emits NO hierarchy edges (#6326), but collectOpenStatements
// still runs, so it still anchors. HELD CONSTANT: one `open`, the nested depth.
// A carrier wired to some `.fs`-only branch would pass the case above and leave
// every signature file dangling.
func TestFSharp_SignatureFileImportsFromEndResolves_6852(t *testing.T) {
	const src = `module Acme.Domain.Core

open System.Text

val helper : int -> int
`
	const path = "src/Domain/Core.fsi"
	recs, byID := resolveFSharp6852(t, src, path)

	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if _, ok := byID[r.FromID]; !ok {
				t.Errorf("signature-file IMPORTS owned by %q: FROM end %q resolves to no record",
					recs[i].Name, r.FromID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
	}
}

// TestFSharp_NoCarrierWithoutAnOpen_6852 is the OVER-FIRING control, and it is
// the half of the grade a "the edge now resolves" test cannot supply. Axis
// VARIED: the `open` statements (absent). HELD CONSTANT: a full record set —
// module, record type, DU, members, operations — so the file still extracts
// plenty and still exercises the type/operation passes; only the path-anchored
// edge is gone.
func TestFSharp_NoCarrierWithoutAnOpen_6852(t *testing.T) {
	const src = `module Acme.Domain.Core

type Shape =
    | Circle of float
    | Square of float

type Widget =
    { Id: int
      Label: string }

let area s =
    match s with
    | Circle r -> r
    | Square a -> a
`
	for _, path := range []string{"src/Domain/Core.fs", "Core.fs"} {
		t.Run(path, func(t *testing.T) {
			recs := runFSharp(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(fsPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(fsCarriers6852(recs, path)); n != 0 {
				t.Errorf("an F# file with nothing to carry must emit no file carrier, got %d — "+
					"an unconditional carrier mints one bare orphan node per .fs file "+
					"across a whole repo, which no recall-shaped assertion can see", n)
			}
			// Forbidden-row form: no record of ANY kind may be named after the
			// file here, so a carrier smuggled in under a different Kind or
			// Subtype is caught too.
			for _, r := range fsNamedExactly6852(recs, path) {
				t.Errorf("no record may be named %q here, got kind=%q subtype=%q name=%q qname=%q",
					path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
			}
		})
	}
}

// TestFSharp_EmptyFileGetsNoCarrier_6852 drives Extract's OTHER return path:
// len(file.Content) == 0 returns nil before extractFSharp is called at all. A
// carrier placed in Extract rather than at the end of extractFSharp could mint
// a node for a file with no content whatsoever.
func TestFSharp_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const path = "src/Domain/Empty.fs"
	recs := runFSharp(t, "", path)
	if len(recs) != 0 {
		t.Fatalf("an empty .fs must extract no records at all, got %d (first: kind=%q name=%q)",
			len(recs), recs[0].Kind, recs[0].Name)
	}
}

// TestFSharp_OneCarrierPerFileNotPerOpen_6852 is the multiplicity control. Axis
// VARIED: the NUMBER of `open` statements (four, each its own record with its
// own path-anchored IMPORTS). HELD CONSTANT: one file, one path, driven at both
// depths. The carrier is per-FILE, not per-EDGE; a per-edge carrier would put
// four nodes under one id.
func TestFSharp_OneCarrierPerFileNotPerOpen_6852(t *testing.T) {
	const src = `module Acme.Domain.Core

open System
open System.Text
open type System.Math
open Acme.Shared.Utils

let helper x = x + 1
`
	for _, path := range []string{"src/Domain/Core.fs", "Core.fs"} {
		t.Run(path, func(t *testing.T) {
			recs := runFSharp(t, src, path)
			if n := len(fsPathAnchored6852(recs, path)); n != 4 {
				t.Fatalf("premise: want 4 path-anchored IMPORTS edges, got %d", n)
			}
			if n := len(fsCarriers6852(recs, path)); n != 1 {
				t.Errorf("4 opens must still yield exactly 1 file carrier, got %d", n)
			}
			if n := len(fsNamedExactly6852(recs, path)); n != 1 {
				t.Errorf("exactly 1 record may be named %q, got %d", path, n)
			}
		})
	}
}

// TestFSharp_ModuleNamedLikeThePathGetsNoSecondCarrier_6852 drives
// FileCarrierFor CLAUSE 3 (no record is ALREADY named path) in production.
//
// moduleRE captures a dotted `[\w.]+` name, so the root file `Core.fs`
// declaring `module Core.fs` emits a SCOPE.Component named EXACTLY the path
// while also anchoring an IMPORTS on it. graph.EntityID hashes
// (repo, kind, name, sourceFile) and NOT Subtype, so a carrier here would land
// a second SCOPE.Component under the module record's id and make the resolver's
// rewrite target ambiguous — the #6369/#6480 hazard, handled by clause 3's
// `records[i].Name == path` test rather than by anything Subtype-aware.
//
// The nested subtest is the contrast, not decoration: at `src/Domain/Core.fs`
// the same source's module name is NOT the path, so clause 3 does not fire and
// the carrier is minted. Without it this test would pass for a carrier that was
// never emitted at all.
func TestFSharp_ModuleNamedLikeThePathGetsNoSecondCarrier_6852(t *testing.T) {
	const src = `module Core.fs

open System.Text

let helper x = x + 1
`
	t.Run("root path — the module name IS the path", func(t *testing.T) {
		const path = "Core.fs"
		recs := runFSharp(t, src, path)
		if n := len(fsPathAnchored6852(recs, path)); n == 0 {
			t.Fatal("premise: the fixture must still anchor an IMPORTS on the path")
		}
		named := fsNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d — clause 3 must reject a "+
				"second carrier when the extractor already minted a path-named record", path, len(named))
		}
		if named[0].Subtype != "module" {
			t.Errorf("the one record named %q must be the MODULE declaration, got subtype %q",
				path, named[0].Subtype)
		}
		if n := len(fsCarriers6852(recs, path)); n != 0 {
			t.Errorf("no file carrier may be minted when a module record already carries the "+
				"path as its Name, got %d", n)
		}
	})

	t.Run("nested path — the same source DOES get a carrier", func(t *testing.T) {
		const path = "src/Domain/Core.fs"
		recs := runFSharp(t, src, path)
		if n := len(fsCarriers6852(recs, path)); n != 1 {
			t.Fatalf("want exactly 1 carrier at a nested path, got %d — without this the root "+
				"subtest above would pass for a carrier that is never emitted anywhere", n)
		}
	})
}

// TestFSharp_ImportMarkerIsNeverThePathNamedRecord_6852 pins the premise the
// header states about the #6369 import marker: its Name is
// importDisplayName(mod) — the last dot-segment of the `open` target — so it
// can never equal a path, which always carries a `.fs`/`.fsi`/`.fsx` dot.
// The dangerous spelling is driven directly: an `open` whose target is the
// root path itself.
func TestFSharp_ImportMarkerIsNeverThePathNamedRecord_6852(t *testing.T) {
	const src = `module Acme.Domain.Core

open Core.fs

let helper x = x + 1
`
	const path = "Core.fs"
	recs := runFSharp(t, src, path)

	for _, r := range recs {
		if r.Subtype != "import" {
			continue
		}
		if r.Name == path {
			t.Errorf("an import marker is named %q — the same string as the file path — so it "+
				"would collide with the carrier under one graph.EntityID (Subtype is not "+
				"hashed, #6369/#6480)", path)
		}
	}
	if n := len(fsNamedExactly6852(recs, path)); n != 1 {
		t.Errorf("exactly 1 record may be named %q (the carrier), got %d", path, n)
	}
	if n := len(fsCarriers6852(recs, path)); n != 1 {
		t.Errorf("want exactly 1 file carrier, got %d", n)
	}
}

// TestFSharp_CarrierShape_6852 pins what the carrier IS: stamped fsharp,
// anchored on the file it names, and owning no relationships of its own — the
// import placeholders still carry the IMPORTS edges, so re-homing them onto the
// carrier would double every edge.
func TestFSharp_CarrierShape_6852(t *testing.T) {
	const path = "src/Domain/Core.fs"
	recs := runFSharp(t, carrierSrcFSharp6852, path)
	cs := fsCarriers6852(recs, path)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if cs[0].Language != "fsharp" {
		t.Errorf("carrier Language = %q, want %q", cs[0].Language, "fsharp")
	}
	if cs[0].SourceFile != path {
		t.Errorf("carrier SourceFile = %q, want %q", cs[0].SourceFile, path)
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Errorf("the fsharp file carrier must own no relationships, got %d", n)
	}
	// The lang ARGUMENT is what is graded here, not the field it ends up in.
	// fsharp's Extract runs extractor.TagEntitiesLanguage, which fills an EMPTY
	// Language with "fsharp" — so Language alone cannot tell `"fsharp"` from
	// `""`. What does tell them apart is Properties: TagEntitiesLanguage only
	// touches a record whose Language is empty, and when it does it also stamps
	// Properties["language"]. Every OTHER fsharp record sets Language
	// explicitly and therefore carries no such key, so a carrier that acquired
	// one would be the single record in the extraction whose provenance differs
	// from its siblings' — proto's #6356 trap, which is the reason
	// file_carrier.go takes the token as a parameter at all.
	if v, ok := cs[0].Properties["language"]; ok {
		t.Errorf("carrier carries Properties[\"language\"]=%q — it was language-tagged after the "+
			"fact rather than stamped by the lang argument, so it disagrees with every other "+
			"fsharp record, none of which carries that key", v)
	}
	// THE PREMISE THAT ASSERTION RESTS ON, pinned rather than assumed. The check
	// above distinguishes lang="" from lang="fsharp" only while NO OTHER record
	// carries the key either. If some future fsharp record shipped without an
	// explicit Language, TagEntitiesLanguage would fill it and stamp the key —
	// and the check above would go on passing while quietly grading nothing,
	// with the empty-token mutant back to ALIVE and no test going red.
	for _, r := range recs {
		if v, ok := r.Properties["language"]; ok {
			t.Errorf("record kind=%q subtype=%q name=%q carries Properties[\"language\"]=%q — "+
				"every fsharp record is meant to set Language explicitly, and the carrier's "+
				"empty-token mutant is only observable while none of them is language-tagged "+
				"after the fact", r.Kind, r.Subtype, r.Name, v)
		}
	}
	if n := len(fsPathAnchored6852(recs, path)); n != 1 {
		t.Errorf("the open IMPORTS edge must still be emitted exactly once, got %d", n)
	}
	// #577 convention: the file entity is the FIRST record.
	if recs[0].Name != path {
		t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
	}
}
