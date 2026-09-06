// file_carrier_6852_test.go — arm 11 of #6852 (dart).
//
// THE DEFECT. buildImportRecord (dart.go) stamps `FromID: filePath` on the
// IMPORTS edge of every `import '...';`. internal/resolve/refs.go has no
// path→entity index, so a path-valued FromID resolves if and only if some
// emitted node carries that exact string as its Name or QualifiedName. Before
// this arm nothing dart emitted did, so the raw path reached the graph as the
// edge's FROM end. #6847 measured it at runtime; #6815 settled the fix shape.
//
// THE FIX. One CONDITIONAL extractor.PrependFileCarrier at the end of
// extractDart. Conditional and not unconditional because an unconditional
// carrier mints one bare orphan node per .dart file across a whole repo — a
// change no recall-shaped assertion can see, since those only ask whether the
// carrier EXISTS (#6518, #6815).
//
// ── THE SITE TABLE, GROUNDED RATHER THAN ASSERTED ────────────────────────────
//
// FromID sites in internal/extractors/dart/ (non-test): exactly ONE.
//
//	dart.go:349  buildImportRecord  FromID: filePath   — path-anchored, IMPORTS
//
// Every other relationship dart emits carries no FromID at all: the CALLS edges
// extractCallRelationships builds set only ToID/Kind/Properties, and the
// CONTAINS edges the method pass appends set only ToID/Kind. An edge with an
// empty FromID is owned by its record and never asks refs.go a path question,
// so the one site above is the whole population. That is a grep result over the
// package, not an inference from one file.
//
// ── NAME SITES, AND WHY ONLY ONE CAN EVER SPELL THE PATH ─────────────────────
//
// The question clause 3 of FileCarrierFor asks is "is some record ALREADY named
// path". dart has SIX Name sites:
//
//  1. buildImportRecord  Name = dartTopName(uri)                  ← REACHABLE
//  2. extractDart class   Name = classRE  submatch 1  `(\w+)`
//  3. extractDart method  Name = methodRE submatch 1  `(\w+)`
//  4. dartEnums           Name = dartEnumRE submatch 1 `(\w+)` via EnumEntity
//  5. dartTypeAlias       Name = dartTypedef{Alias,Func}RE `(\w+)`
//  6. dartModifiedClasses Name = dartModifiedClassRE submatch 2 `(\w+)`
//
// Sites 2-6 are each a VERBATIM SLICE of a `(\w+)` capture — src[m[a]:m[b]] —
// with no builder concatenating anything onto them anywhere in the package.
// Go's RE2 `\w` is [0-9A-Za-z_], so such a Name contains no '.' and no '/'.
// Every path the dart extractor sees in production ends in ".dart"
// (internal/classifier/classifier.go:372 maps `.dart` → "dart", and that
// extension entry is the ONLY route by which a file reaches this extractor), so
// every production path contains a '.'. A string with no '.' cannot equal a
// string with one. That closure is OBSERVED at both depths by
// TestDart_WordCharacterSitesCannotSpellThePath_6852 rather than argued —
// the four falsified predecessors of this series (astro, clojure, cobol twice)
// each rested a character-class closure on prose that no test drove.
//
// WHY THE CLOSURE SURVIVES CONCATENATION, which is the specific way the
// predecessors fell. cobol's fell because a builder concatenated a prefix onto a
// literal-derived operand; clojure's fell because a derivation cut only under a
// condition. Here there is NO builder: sites 2-6 assign the capture directly to
// Name, and the only concatenations in the package go into Signature
// ("class "+name, name+"("+params+")", "typedef "+name, modifier+" class "+name)
// and into Properties — neither of which clause 3 or refs.go reads. Adding a
// prefix to a Name would be a source change, and it would be caught: it would
// have to survive the driven cells below, which check Name at both depths.
//
// SITE 4 ALSO SETS QualifiedName, and that half is a LENGTH invariant — the
// crystal form (#6905), kept because it is the one shape concatenation cannot
// defeat. extractor.EnumEntity stamps `scope:enum:<sourceFile>:<Name>`, and
// sourceFile IS path while Name is non-empty (EnumEntity returns ok=false
// otherwise), so len(QualifiedName) = 11 + len(path) + 1 + len(Name) > len(path).
// Sites 1,2,3,5,6 leave QualifiedName empty, and "" never equals a non-empty
// path.
//
// ── SITE 1 IS REACHABLE, AND IT IS DRIVEN, NOT ASSERTED ──────────────────────
//
// dartTopName is NOT a `\w+` slice, so the closure above does not cover it, and
// an earlier draft of this header that said "no dart record can spell the path"
// was wrong. The function strips a scheme prefix at the first ':', truncates at
// the first '/', and then TrimSuffix'es ONE ".dart". So for a ROOT path
// `main.dart`, the URI `main.dart.dart` yields exactly `main.dart` — the path.
// A root Dart file that imports a `.dart.dart` sibling therefore emits an import
// record already named the path, clause 3 fires, and no second carrier is minted
// (two nodes under one graph.EntityID is the #6369/#6480 hazard). That cell is
// TestDart_ImportTopNameSpelledLikeThePathGetsNoSecondCarrier_6852/root.
//
// AT NESTED DEPTH IT IS UNREACHABLE, and the reason is a slicing property rather
// than a character class: dartTopName's return is always a SUBSTRING of uri that
// ends before the first '/' (`s = s[:i]`), so it can contain no '/'; a nested
// path contains one. There is no concatenation to defeat here either — the
// function only slices and trims. The nested contrast is DRIVEN alongside the
// root cell (…/nested_contrast), which is also what stops the root cell from
// passing on a carrier that is never emitted at all.
//
// ── PLACEMENT: ONE INDEPENDENT REASON, ONE ENTAILED-IN-EFFECT ────────────────
//
// The call sits at the END of extractDart. The cobol lesson is that a compound
// placement condition must be scored PART BY PART and the entailed conjunct
// NAMED, because prose that emphasises the entailed one hides the real hazard.
// So, explicitly:
//
//	INDEPENDENT, production-reachable, and graded:
//	  THE INDEX HAZARD. classSpan.idx is an INDEX INTO the entity slice
//	  (dart.go: `idx := len(entities)`), dereferenced in the method pass as
//	  `entities[cls.idx].Relationships = append(...)` to hang each method's
//	  CONTAINS edge on its enclosing class. THE WINDOW IS NARROW, and the
//	  narrowing was found by SCORING and not by reading: a draft of this header
//	  said a carrier placed between the IMPORT pass and the CLASS pass shifts
//	  the stored indices, and that mutant came back ALIVE — `idx :=
//	  len(entities)` is evaluated inside the class loop, so a head insertion
//	  before that loop is absorbed. The window that bites is strictly BETWEEN
//	  the class loop and the method loop, where the spans exist and have not
//	  yet been dereferenced. It is not entailed by clause 2 (dart's import pass
//	  runs FIRST, so a conditional carrier in that window really is emitted —
//	  crystal's situation, not clojure's, where the same move minted nothing).
//	  Graded by TestDart_CarrierPlacementDoesNotShiftTheContainsPass_6852.
//
//	ENTAILED IN EFFECT — real in principle, NOT production-reachable for dart:
//	  CLAUSE-3 VISIBILITY OF THE TYPE PASS. FileCarrierFor can only decline for a
//	  record it is handed, so in general the call must run after
//	  `entities = append(entities, extractDartTypes(...))`. For dart that reason
//	  buys nothing: every name extractDartTypes emits is a site-4/5/6 `\w+` slice
//	  and therefore cannot spell a '.'-bearing path, and the ONE reachable
//	  clause-3 route (site 1) is emitted in the FIRST pass, so clause 3 sees it
//	  wherever after the import loop the call sits. It is named here rather than
//	  presented as a second independent reason precisely because it is not one.
//	  Its unreachability is what
//	  TestDart_WordCharacterSitesCannotSpellThePath_6852 observes.
//
// The `dart` token argument is graded through the PROVENANCE route rather than
// through the Language field alone: dart runs extractor.TagEntitiesLanguage in
// Extract, AFTER extractDart returns, so an empty token would simply be filled
// and Language could not distinguish "" from "dart". TagEntitiesLanguage stamps
// Properties["language"] only on that fill path, and extractor.FileEntity never
// sets that key, so its ABSENCE is what makes the Language row evidence.
// TestDart_CarrierShape_6852 asserts both halves.
package dart_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/dart"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// runDart6852 drives the REGISTERED production extractor over src at path.
func runDart6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("dart")
	if !ok {
		t.Fatal("dart extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "dart",
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// dartCarriers6852 returns every record that IS the file carrier for path — the
// SCOPE.Component/file record extractor.FileEntity mints. Subtype "file" is what
// separates it from dart's other SCOPE.Component records: the import stubs carry
// no Subtype at all, and the class / modified-class records carry "class".
func dartCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// dartPathAnchored6852 returns every relationship in recs whose FromID is
// exactly path — the shape whose FROM end has nothing to resolve onto.
func dartPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
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

// dartNamedExactly6852 returns every record whose Name OR QualifiedName is path.
// Both fields, because that is the disjunction internal/resolve/refs.go resolves
// on and the disjunction #6847's runtime guard measures — a Name-only check
// would leave site 4's QualifiedName ungraded.
func dartNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// dartRelOwners6852 returns the Name of EVERY record owning at least one
// relationship of kind k whose ToID is to, in slice order. The FULL list rather
// than the first match on purpose: a first-wins scan reports one owner however
// many there are, so an edge attached to an EXTRA record while the intended
// owner keeps its own would read as correct — and that is exactly the shape a
// mis-placed carrier produces, because classSpan.idx is an INDEX into the
// entity slice.
func dartRelOwners6852(recs []types.EntityRecord, k, to string) []string {
	var out []string
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == k && rel.ToID == to {
				out = append(out, r.Name)
				break
			}
		}
	}
	return out
}

// resolveDart6852 extracts src at path, stamps ids the way graph assembly does,
// runs the production resolver pipeline, and returns the records plus the
// id→record index. The assertion that follows is on the EMITTED ARTEFACT after
// resolution — the edge's FROM end — not on a helper's return value or on a
// counter the code keeps about itself.
func resolveDart6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := runDart6852(t, src, path)
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

// assertDartImportsResolve6852 fails for every IMPORTS edge whose FROM end names
// no record, and fails OUTRIGHT when the fixture produced no IMPORTS at all — a
// resolution assertion over an empty set is a no-op that reads like a guard.
func assertDartImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
	t.Helper()
	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if _, ok := byID[r.FromID]; !ok {
				t.Errorf("IMPORTS owned by %q: FROM end %q resolves to no record "+
					"(refs.go has no path→entity index; a path-valued FromID resolves "+
					"iff some record carries that exact string as its Name — emit a file "+
					"carrier, internal/extractor/file_carrier.go)", recs[i].Name, r.FromID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
	}
}

// carrierSrcDart6852 is a canonical Dart file that exercises EVERY pass of
// extractDart: the import pass (two directives, one aliased), the class pass
// (with extends, so the class record exists), the method pass inside that class
// (so CONTAINS and CALLS edges exist), a top-level function (the no-enclosing-
// class branch), and all three type passes of types.go — enum, both typedef
// spellings, and a Dart 3 modified class. A carrier wired into any one of those
// passes would still be seen by the tests below.
const carrierSrcDart6852 = `import 'package:flutter/material.dart';
import 'dart:convert' as conv;

class Handler extends Base {
  void handle(Request req) {
    render(req);
  }
}

void main() {
  runApp(1);
}

enum Colour { red, green }

typedef Ids = List<int>;

typedef int Reducer(int a, int b);

sealed class Shape {
  int area() {
    return compute();
  }
}
`

// TestDart_ImportsFromEndResolves_6852 is the fix's behavioural test, and it
// drives every cell of dart's depth/condition table in which the edge DANGLES.
// The remaining cells — the ones where the source itself spells the path — are
// driven by the clause-3 test below.
//
//	                              | nested lib/src/api.dart  | root api.dart
//	import URI's top segment      | dangled (this test)      | dangled (this test)
//	≠ path                        |                          |
//	import URI = "<path>.dart"    | still dangles; topName   | resolves already;
//	                              | truncates at '/' so it   | clause 3 mints
//	                              | cannot spell a nested    | nothing
//	                              | path (nested_contrast)   | (ImportTopName test)
//	class / method / enum /       | NOT EXPRESSIBLE — every  | NOT EXPRESSIBLE —
//	typedef / modified class      | such Name is a `\w+`     | same, and observed
//	named like the path           | slice, so it holds no    | by the WordCharacter
//	                              | '.' or '/'               | test at both depths
//
// Every cell in THIS test FAILS before the carrier.
//
// Axis VARIED: path depth (nested vs root). HELD CONSTANT: the source, so the
// anchored edge set is byte-identical in both rows and depth is the only
// difference between them.
func TestDart_ImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"lib/src/api.dart", "api.dart"} {
		t.Run(path, func(t *testing.T) {
			// Premise: nothing dart emits is named after the file here, so the
			// only record that can be named the path is the carrier itself.
			// This PINS the "both depths dangled" claim rather than assuming it
			// — cobol's arm shipped a false root-only claim for want of it.
			pre := runDart6852(t, carrierSrcDart6852, path)
			named := dartNamedExactly6852(pre, path)
			if len(named) != 1 {
				t.Fatalf("premise: exactly 1 record may be named %q (the carrier), got %d", path, len(named))
			}
			if named[0].Subtype != "file" {
				t.Fatalf("premise: the one record named %q must be the carrier, got kind=%q "+
					"subtype=%q name=%q qname=%q — dart names records after the import URI's "+
					"top segment, the class, the method, the enum, the typedef or the "+
					"modified class, never after the file", path, named[0].Kind,
					named[0].Subtype, named[0].Name, named[0].QualifiedName)
			}
			// Premise: the path-anchored edge the carrier exists for is really
			// present, so the resolution assertion below is not vacuous.
			if n := len(dartPathAnchored6852(pre, path)); n == 0 {
				t.Fatalf("premise: fixture emits no relationship with FromID == %q", path)
			}
			recs, byID := resolveDart6852(t, carrierSrcDart6852, path)
			assertDartImportsResolve6852(t, recs, byID)
		})
	}
}

// TestDart_NoCarrierWithoutAnImport_6852 is the OVER-FIRING control for
// FileCarrierFor CLAUSE 2 — the half of the grade that a "the edge now resolves"
// test cannot supply, because recall-shaped assertions only ever ask whether the
// carrier EXISTS. It matches the `if !anchored` return path, and it is what
// fails if the conditional carrier is ever made unconditional.
//
// Axis VARIED: the `import` directives (absent). HELD CONSTANT: a full record
// set — a class with a superclass and a method, a top-level function, an enum,
// both typedef spellings and a modified class, plus the CONTAINS and CALLS edges
// they own — so the file still produces entities and relationships and the only
// thing missing is the anchor. Asserted below rather than assumed.
func TestDart_NoCarrierWithoutAnImport_6852(t *testing.T) {
	const src = `class Handler extends Base {
  void handle(Request req) {
    render(req);
  }
}

void main() {
  runApp(1);
}

enum Colour { red, green }

typedef Ids = List<int>;

sealed class Shape {
  int area() {
    return compute();
  }
}
`
	for _, path := range []string{"lib/src/api.dart", "api.dart"} {
		t.Run(path, func(t *testing.T) {
			recs := runDart6852(t, src, path)
			// Premise: the file is NOT empty of records, so "no carrier" is a
			// decision and not an artefact of an extraction that produced
			// nothing at all.
			if len(recs) < 5 {
				t.Fatalf("premise: want a full record set, got %d records", len(recs))
			}
			rels := 0
			for i := range recs {
				rels += len(recs[i].Relationships)
			}
			if rels == 0 {
				t.Fatal("premise: fixture owns no relationships — clause 2 would be " +
					"trivially unsatisfied for a reason other than the missing import")
			}
			// Premise: clause 2's actual input is empty.
			if n := len(dartPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 relationships anchored on %q, got %d", path, n)
			}
			if n := len(dartCarriers6852(recs, path)); n != 0 {
				t.Errorf("want 0 carriers for a file with no path-anchored edge, got %d — "+
					"an unconditional carrier mints one bare orphan node per .dart file "+
					"across a whole repo (#6518, #6815)", n)
			}
			if n := len(dartNamedExactly6852(recs, path)); n != 0 {
				t.Errorf("want 0 records named %q, got %d", path, n)
			}
		})
	}
}

// TestDart_EmptyFileGetsNoCarrier_6852 drives Extract's FIRST return path — the
// `len(file.Content) == 0` guard, which returns before extractDart is reached at
// all. A carrier minted there would be a bare orphan for every empty .dart file.
func TestDart_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	recs := runDart6852(t, "", "lib/empty.dart")
	if len(recs) != 0 {
		t.Errorf("empty content: want 0 records, got %d (first: kind=%q name=%q)",
			len(recs), recs[0].Kind, recs[0].Name)
	}
}

// TestDart_EmptyPathGetsNoCarrier_6852 drives FileCarrierFor CLAUSE 1, which is
// the clause that is NOT redundant with clause 2: an empty FromID trivially
// equals an empty path, so a record that DOES carry an anchoring relationship
// still must not get a nameless carrier. dart reaches this because
// buildImportRecord stamps FromID: filePath unconditionally, so at path "" the
// IMPORTS edge is anchored on "" and clause 2 is SATISFIED — only clause 1
// rejects. That premise is asserted, not assumed.
func TestDart_EmptyPathGetsNoCarrier_6852(t *testing.T) {
	const src = `import 'package:http/http.dart';

class Client {
  void get(String u) {
    send(u);
  }
}
`
	recs := runDart6852(t, src, "")
	if n := len(dartPathAnchored6852(recs, "")); n == 0 {
		t.Fatal("premise: want at least one relationship anchored on the empty path, " +
			"got 0 — without it clause 2 rejects first and clause 1 is never consulted")
	}
	for i := range recs {
		if recs[i].Name == "" {
			t.Errorf("record %d (kind=%q subtype=%q) is named the empty path — a nameless "+
				"carrier resolves nothing and adds a blank node", i, recs[i].Kind, recs[i].Subtype)
		}
	}
	if n := len(dartCarriers6852(recs, "")); n != 0 {
		t.Errorf("want 0 carriers at the empty path, got %d", n)
	}
}

// TestDart_OneCarrierPerFileNotPerImport_6852 is the multiplicity control: the
// carrier is a property of the FILE, not of each anchored edge. Three imports
// produce three path-anchored IMPORTS edges and must still produce exactly one
// carrier — the shape a per-edge implementation would get wrong while leaving
// every "the edge resolves" assertion green.
//
// Axis VARIED: the number of anchored edges (3, vs 2 in the canonical fixture).
// HELD CONSTANT: path depth, and the presence of exactly one class.
func TestDart_OneCarrierPerFileNotPerImport_6852(t *testing.T) {
	const src = `import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'dart:convert';

class Client {
  void get(String u) {
    send(u);
  }
}
`
	const path = "lib/src/client.dart"
	recs := runDart6852(t, src, path)
	if n := len(dartPathAnchored6852(recs, path)); n != 3 {
		t.Fatalf("premise: want 3 path-anchored edges, got %d — the multiplicity claim "+
			"is about MORE anchors than carriers and is vacuous without them", n)
	}
	if n := len(dartCarriers6852(recs, path)); n != 1 {
		t.Errorf("want exactly 1 carrier for 3 anchored edges, got %d", n)
	}
	if n := len(dartNamedExactly6852(recs, path)); n != 1 {
		t.Errorf("want exactly 1 record named %q, got %d — two nodes under one "+
			"graph.EntityID is the #6369/#6480 hazard", path, n)
	}
}

// TestDart_ImportTopNameSpelledLikeThePathGetsNoSecondCarrier_6852 drives
// FileCarrierFor CLAUSE 3, and it is THE cell of this arm: the one dart Name
// site that is not a `\w+` slice and can therefore spell the path.
//
// THE ROUTE, mechanically. dartTopName(uri) strips a scheme prefix at the first
// ':', truncates at the first '/', then TrimSuffix'es ONE ".dart". Give it
// `main.dart.dart` and it returns `main.dart`. At the ROOT path `main.dart` that
// IS the path, so buildImportRecord emits a SCOPE.Component already named the
// path, the IMPORTS edge's FROM end already resolves onto it, and clause 3 must
// decline — because graph.EntityID does not hash Subtype, so a second node here
// would land under the import record's id (#6369/#6480).
//
// A `.dart.dart` sibling is an odd but legal on-disk spelling, and the point of
// this cell is that it is DRIVEN rather than argued: four arms of this series
// shipped a closure that a driven cell later disproved.
//
// THE NESTED CONTRAST is not decoration. It is what stops the root subtest from
// passing on a carrier that is never emitted at all — the failure mode #6834
// names — and it independently OBSERVES the nested half of the site-1 closure:
// dartTopName's return is always a substring of uri ending before the first '/',
// so it cannot hold a '/' and cannot spell a nested path, and the carrier is
// therefore minted there.
//
// Axis VARIED: path depth. HELD CONSTANT: the source, byte for byte — the same
// `main.dart.dart` import in both rows, so the ONLY difference between "clause 3
// declines" and "the carrier is minted" is the depth of the path.
func TestDart_ImportTopNameSpelledLikeThePathGetsNoSecondCarrier_6852(t *testing.T) {
	const src = `import 'main.dart.dart';

class App {
  void run() {
    start();
  }
}
`
	t.Run("root", func(t *testing.T) {
		const path = "main.dart"
		recs := runDart6852(t, src, path)
		named := dartNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("want exactly 1 record named %q, got %d — a second one would put two "+
				"nodes under one graph.EntityID (#6369/#6480)", path, len(named))
		}
		if named[0].Subtype == "file" {
			t.Errorf("the one record named %q is the CARRIER — clause 3 should have "+
				"declined in favour of the import record dartTopName already named %q",
				path, path)
		}
		if n := len(dartCarriers6852(recs, path)); n != 0 {
			t.Errorf("want 0 carriers when a record is already named %q, got %d", path, n)
		}
		// The edge still resolves — which is the whole reason declining is safe.
		recs2, byID := resolveDart6852(t, src, path)
		assertDartImportsResolve6852(t, recs2, byID)
	})

	t.Run("nested_contrast", func(t *testing.T) {
		const path = "lib/main.dart"
		recs := runDart6852(t, src, path)
		// dartTopName truncates at the first '/', so its return can never hold
		// one and can never spell this path. Observed, not asserted:
		for i := range recs {
			if recs[i].Subtype == "file" {
				continue
			}
			if recs[i].Name == path {
				t.Errorf("record %d (kind=%q) is named the nested path %q — dartTopName "+
					"truncates at the first '/', so nothing but the carrier should be",
					i, recs[i].Kind, path)
			}
		}
		if n := len(dartCarriers6852(recs, path)); n != 1 {
			t.Fatalf("want exactly 1 carrier at nested depth, got %d — without this row "+
				"the root subtest above would pass on a carrier that is never emitted", n)
		}
		recs2, byID := resolveDart6852(t, src, path)
		assertDartImportsResolve6852(t, recs2, byID)
	})
}

// TestDart_WordCharacterSitesCannotSpellThePath_6852 OBSERVES the closure that
// covers Name sites 2-6 (class, method, enum, typedef ×2, modified class), at
// BOTH depths. It exists because the four falsified predecessors of this series
// each asserted a character-class closure that no test drove.
//
// The fixture names EVERY one of those sites the path's stem (`api`) — the
// closest a `\w+` capture can get to the path — and ALSO attempts the literal
// path spelling on each of them (`class api.dart {`, `enum api.dart {`,
// `typedef api.dart = ...`, `sealed class api.dart`). Those attempts are the
// point: `\w` is [0-9A-Za-z_], so each regex either fails to match or captures
// only the pre-dot run, and no record can come out named `api.dart`. The
// assertion is over EVERY record, so a site that started concatenating a prefix
// (cobol's route) or a new site added later shows up here.
//
// Axis VARIED: path depth, and within the source the spelling of each declared
// name (stem vs literal path). HELD CONSTANT: the set of declaration kinds
// present, so every site is exercised in both rows.
func TestDart_WordCharacterSitesCannotSpellThePath_6852(t *testing.T) {
	const src = `import 'package:http/http.dart';

class api {
  void api() {
    go();
  }
}

class api.dart {
  void nope() {
    go();
  }
}

enum api { a, b }

enum api.dart { a, b }

typedef api = List<int>;

typedef api.dart = List<int>;

sealed class api {
  int size() {
    return 1;
  }
}

sealed class api.dart {
  int size2() {
    return 1;
  }
}
`
	for _, path := range []string{"lib/src/api.dart", "api.dart"} {
		t.Run(path, func(t *testing.T) {
			recs := runDart6852(t, src, path)
			// Premise: the `\w+` sites really did produce records, so the
			// absence below is a property of their NAMES and not of an
			// extraction that emitted nothing.
			stems := 0
			for i := range recs {
				if recs[i].Name == "api" {
					stems++
				}
			}
			if stems < 3 {
				t.Fatalf("premise: want at least 3 records named %q (class, enum, typedef, "+
					"sealed class, method), got %d — with none of them this test observes "+
					"nothing", "api", stems)
			}
			for i := range recs {
				if recs[i].Subtype == "file" {
					continue
				}
				if recs[i].Name == path || recs[i].QualifiedName == path {
					t.Errorf("record %d spells the path: kind=%q subtype=%q name=%q qname=%q — "+
						"sites 2-6 are verbatim `\\w+` captures and `\\w` is [0-9A-Za-z_], so "+
						"no such Name can contain the '.' every production dart path carries "+
						"(classifier.go:372 routes only `.dart`); site 4's QualifiedName is "+
						"`scope:enum:<path>:<Name>`, strictly longer than the path",
						i, recs[i].Kind, recs[i].Subtype, recs[i].Name, recs[i].QualifiedName)
				}
			}
		})
	}
}

// TestDart_CarrierShape_6852 pins what the carrier IS: stamped dart, named and
// anchored on the FULL path, owning no relationships of its own, and FIRST in
// the slice.
//
// The Language row is what grades the `lang` ARGUMENT, and it is only evidence
// because of the Properties row beside it. dart runs
// extractor.TagEntitiesLanguage in Extract, AFTER extractDart returns, so an
// empty token would be FILLED and the Language field alone could not distinguish
// "" from "dart". TagEntitiesLanguage stamps Properties["language"] only on that
// fill path and extractor.FileEntity never sets that key, so its ABSENCE is what
// says the token arrived non-empty. Both rows are asserted; the second is the
// premise for reading the first.
//
// The no-relationships row is not decoration: dart's import records carry the
// IMPORTS edges themselves, so re-homing them onto the carrier would DOUBLE
// every import edge.
func TestDart_CarrierShape_6852(t *testing.T) {
	for _, path := range []string{"lib/src/api.dart", "api.dart"} {
		t.Run(path, func(t *testing.T) {
			recs := runDart6852(t, carrierSrcDart6852, path)
			carriers := dartCarriers6852(recs, path)
			if len(carriers) != 1 {
				t.Fatalf("want exactly 1 carrier for %q, got %d", path, len(carriers))
			}
			c := carriers[0]
			if c.Language != "dart" {
				t.Errorf("carrier Language = %q, want %q", c.Language, "dart")
			}
			if v, ok := c.Properties["language"]; ok {
				t.Errorf("carrier carries Properties[\"language\"] = %q — extractor.FileEntity "+
					"never sets that key, so its presence means TagEntitiesLanguage FILLED an "+
					"empty Language and the Language row above is not evidence of the token "+
					"passed to PrependFileCarrier", v)
			}
			if c.SourceFile != path {
				t.Errorf("carrier SourceFile = %q, want %q", c.SourceFile, path)
			}
			if c.Name != path {
				t.Errorf("carrier Name = %q, want the FULL path %q — a basename-named carrier "+
					"resolves nothing, because the FromID refs.go must match is the path",
					c.Name, path)
			}
			if n := len(c.Relationships); n != 0 {
				t.Errorf("carrier owns %d relationships, want 0 — dart's import records carry "+
					"the IMPORTS edges themselves, so re-homing them here would double them", n)
			}
			// #577: the file entity is the first record an extractor appends,
			// and python/re_exports.go and python/prune_import_placeholders.go
			// both rely on index 0.
			if recs[0].Subtype != "file" {
				t.Errorf("the carrier must be at index 0 (#577), got kind=%q subtype=%q name=%q there",
					recs[0].Kind, recs[0].Subtype, recs[0].Name)
			}
		})
	}
}

// TestDart_CarrierPlacementDoesNotShiftTheContainsPass_6852 grades the ONE
// independent, production-reachable placement reason, rather than leaving it
// inside a bulleted comment — the cobol lesson (#6897/#6899) is that a placement
// comment listing reasons without a mutant per reason is prose.
//
// THE HAZARD. extractDart records `idx := len(entities)` in each classSpan and
// dereferences it in the method pass as
// `entities[cls.idx].Relationships = append(...)`, to hang each method's
// CONTAINS edge on its enclosing class. An insertion at position 0 after the
// class loop and before the method loop shifts every stored idx by one, so each
// class's CONTAINS edges land on the record BEFORE it. Nothing crashes; the
// graph is quietly wrong.
//
// THE WINDOW, STATED EXACTLY, because a mutant disproved the loose version. A
// carrier prepended BEFORE the class loop does NOT shift anything: `idx :=
// len(entities)` is evaluated inside that loop, so the insertion is absorbed and
// the move survives (scored ALIVE, and recorded as such). What this test kills
// is the insertion STRICTLY BETWEEN the class loop and the method loop, where
// every classSpan.idx has been recorded and none has been dereferenced.
//
// IT IS NOT ENTAILED BY CLAUSE 2, which is what distinguishes dart from clojure
// (#6897). dart's IMPORT pass runs FIRST, so the path-anchored edge is already
// in `entities` by then and a CONDITIONAL carrier placed in that window is
// emitted for real. For clojure the same move minted nothing, so the hazard
// graded itself away.
//
// The enumeration behind "the one consumer": classSpan.idx is read at exactly
// one site (the CONTAINS append in the method pass). `methodEntityIdx` is
// assigned and then discarded (`_ = methodEntityIdx`), so it has ZERO consumers
// and a shift cannot be observed through it. The type pass appended afterwards
// (extractDartTypes) stores no index and reads none.
//
// The fixture is the ordinary Flutter shape that makes this production-reachable
// — a file that imports its dependencies and then declares classes with methods.
// A file with no import would leave the carrier unemitted and the test vacuous,
// so the import is a premise and is asserted.
func TestDart_CarrierPlacementDoesNotShiftTheContainsPass_6852(t *testing.T) {
	const src = `import 'package:flutter/material.dart';

class Handler extends Base {
  void handle(Request req) {
    render(req);
  }
}

class Registry {
  void register(Thing x) {
    store(x);
  }
}
`
	for _, path := range []string{"lib/src/api.dart", "api.dart"} {
		t.Run(path, func(t *testing.T) {
			recs := runDart6852(t, src, path)
			// Premise: a carrier really is emitted, so the assertions below are
			// about a SHIFTED slice and not about an unchanged one. A full
			// revert makes this row go fatal, so the test is not vacuous.
			if n := len(dartCarriers6852(recs, path)); n != 1 {
				t.Fatalf("premise: want exactly 1 carrier for %q, got %d", path, n)
			}
			for _, row := range []struct{ owner, method string }{
				{"Handler", "handle"},
				{"Registry", "register"},
			} {
				to := extractor.BuildOperationStructuralRef("dart", path, row.method)
				owners := dartRelOwners6852(recs, "CONTAINS", to)
				if len(owners) != 1 || owners[0] != row.owner {
					t.Errorf("CONTAINS -> %q is owned by %v, want exactly [%q] — "+
						"classSpan.idx indexes the entity slice, so a carrier prepended "+
						"before the method pass re-homes this edge onto whichever record "+
						"now sits at that index", to, owners, row.owner)
				}
			}
			// The carrier owns nothing at all, which is the same statement read
			// from the other end: it is the record at index 0 and therefore the
			// one a shifted classSpan.idx would target.
			carrier := dartCarriers6852(recs, path)[0]
			if n := len(carrier.Relationships); n != 0 {
				t.Errorf("the carrier owns %d relationships, want 0 — it is the record at "+
					"index 0 and therefore the one a shifted classSpan.idx would target", n)
			}
		})
	}
}
