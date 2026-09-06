package crystal_test

// file_carrier_6852_test.go — #6852, crystal arm (tenth of twelve).
//
// extractCrystal's require pass (extractor.go:161) stamps `FromID: filePath` on
// the IMPORTS edge of every `require "..."` / `require_relative "..."`.
// internal/resolve/refs.go has no path→entity index, so a path-valued FromID
// resolves if and only if some emitted node carries that exact string as its
// Name. Same defect #6815 fixed in erlang/nim/groovy and #6852 fixed in bicep
// (#6864), terraform (#6871), html (#6879), fsharp (#6880), shell (#6882),
// dockerfile (#6883), lua (#6885), astro (#6891) and clojure (#6897); same fix,
// extractor.PrependFileCarrier.
//
// ONE ANCHORING SITE, N EDGES — html's / fsharp's / lua's / astro's / clojure's
// multiplicity shape, not bicep's single edge and not terraform's two sites.
// extractor.go:161 is the ONLY `FromID:` in the whole package (extractor.go and
// depth.go both): the EXTENDS edge on a class, the CONTAINS edges a scope owns,
// the CALLS edges extractCallRelationships builds, the REFERENCES edge on an
// alias and the TESTS edge on a spec suite all leave FromID EMPTY, which is the
// #120 convention for "the owning record is the FROM end". One import record is
// emitted per `require`, so a file with four requires anchors four edges on ONE
// string and must still gain exactly ONE carrier.
//
// WHAT CRYSTAL NAMES ITS RECORDS AFTER, enumerated from the source rather than
// sampled — there are exactly seven EntityRecord.Name sites in the package:
//
//	extractor.go:154  the require STRING, verbatim (`requireRE` group 1, `[^"]+`)
//	extractor.go:183  the class/struct name       (`classRE`,  `\w+`)
//	extractor.go:220  the module name             (`moduleRE`, `[\w:]+`)
//	extractor.go:248  the lib name                (`libRE`,    `\w+`)
//	extractor.go:287  the def/macro name          (`defRE`, `(?:self\.)?[\w?!]+`;
//	                                               `macroRE`, `\w+`)
//	depth.go:132      the alias name              (`aliasRE`,  `[A-Z][\w:]*`)
//	depth.go:289      "spec_suite:" + BASENAME(path) minus ".cr"
//
// plus extractor.EnumEntity's `name`, which is enumRE's `[A-Z][\w:]*` trimmed.
// No site sets QualifiedName at all, so the resolution question refs.go asks
// (Name or QualifiedName) and the question FileCarrierFor asks (Name) coincide
// for this package.
//
// TWO of those seven can equal the path, and BOTH are driven below rather than
// argued:
//
//   - THE REQUIRE-STRING ROUTE, ANY DEPTH. requireRE captures `[^"]+` — every
//     byte but a quote, slashes and dots included — so a file whose require is
//     spelled exactly like its own path mints a record named the path. shell's
//     self-sourcing route (#6882) and clojure's dot-leading stub (#6897), here
//     through the widest character class of the ten arms. It is NOT a
//     root-depth phenomenon: the nested cell is driven too.
//   - THE `def self.<x>` ROUTE, ROOT DEPTH ONLY. defRE's group is
//     `(?:self\.)?[\w?!]+`, which admits ONE dot and only in the `self.` prefix.
//     So a ROOT file `self.cr` declaring `def self.cr` emits a SCOPE.Operation
//     named exactly the path. No '/' is admissible, so this route is closed at
//     a nested path — driven below as a contrast row, not asserted.
//
// The remaining five are closed, and the argument is stated with the search that
// backs it rather than as a character-class closure alone — the failure mode the
// astro (#6891), clojure (#6897) and cobol arms each hit:
//
//   - classRE / moduleRE / libRE / macroRE / aliasRE / enumRE admit no '.' and
//     no '/'. classifier.go:374 routes `.cr` and only `.cr` to crystal, so every
//     path this extractor is handed in production contains a '.', and none of
//     these names can equal one at any depth.
//   - depth.go:289 is the one BUILDER that concatenates, which is exactly the
//     shape a character-class argument misses (the cobol correction). It is
//     closed by a fixed-point argument, not by a character class: the Name is
//     "spec_suite:" + base where base = BASENAME(path) with a ".cr" suffix
//     trimmed. Name == path would need path to begin with "spec_suite:", which
//     contains no '/', so path can have no directory component; then
//     base == path or base == path minus ".cr", and
//     len(Name) = 11 + len(base) <= 11 + len(path) is never len(path).
//     There is no fixed point at any depth.
//
// So the answer to "does crystal emit anything named after the file or its
// basename, and under what condition" is: it derives a name from the BASENAME
// (depth.go:289) but always under a prefix that makes equality impossible, and
// it names a record the PATH only when the SOURCE spells the path — never as a
// rule. That is lua's/astro's/clojure's shape on the derivation axis and
// shell's on the self-reference axis.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .cr file across a whole repo — a change no recall-shaped assertion can see.
// The forbidden-row controls below are what forbid it, each matched to a
// DISTINCT return path:
//
//	Extract's `len(file.Content) == 0` → TestCrystal_EmptyFileGetsNoCarrier_6852
//	FileCarrierFor clause 1 (path == "") → TestCrystal_EmptyPathGetsNoCarrier_6852
//	FileCarrierFor clause 2 (!anchored)  → TestCrystal_NoCarrierWithoutARequire_6852
//	FileCarrierFor clause 3 (path-named) → TestCrystal_SelfRequiringImportGetsNoSecondCarrier_6852
//	                                       and TestCrystal_DefSelfNamedLikeThePathGetsNoSecondCarrier_6852

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/crystal"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// runCr6852 drives the registered production extractor over src at path.
func runCr6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("crystal")
	if !ok {
		t.Fatal("crystal extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// crCarriers6852 returns every record that IS the file carrier for path — the
// SCOPE.Component/file record extractor.FileEntity mints. Subtype "file" is what
// separates it from the import stubs (Subtype "module"), the class/struct
// records ("class"), the lib records ("lib") and the alias records ("alias").
func crCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// crPathAnchored6852 returns every relationship in recs whose FromID is exactly
// path — the shape whose FROM end has nothing to resolve onto.
func crPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
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

// crNamedExactly6852 returns every record whose Name or QualifiedName is path.
// This is the resolution question internal/resolve/refs.go actually asks: it has
// no path→entity index, so a path-valued FromID resolves if and only if such a
// record exists.
func crNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// crRelOwners6852 returns the Name of EVERY record owning at least one
// relationship of kind k whose ToID is to, in slice order. It returns the full
// list rather than the first or last match on purpose: a first-wins scan reports
// one owner however many there are, so an edge attached to an EXTRA record while
// the intended owner keeps its own would read as correct. That is precisely the
// shape a mis-placed carrier produces here, because scopeSpan.idx is an INDEX
// into the entity slice.
func crRelOwners6852(recs []types.EntityRecord, k, to string) []string {
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

// resolveCr6852 extracts src at path, stamps ids the way graph assembly does,
// runs the production resolver pipeline, and returns the records plus the
// id→record index. This asserts on the EMITTED ARTEFACT after resolution — the
// edge's FROM end — not on a helper's return value or a counter.
func resolveCr6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := runCr6852(t, src, path)
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

// assertCrImportsResolve6852 fails for every IMPORTS edge whose FROM end names
// no record, and fails outright when the fixture produced no IMPORTS at all — a
// resolution assertion over an empty set is a no-op that reads like a guard.
func assertCrImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
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

// carrierSrcCr6852 is a canonical Crystal file that exercises EVERY pass of
// extractCrystal — the require pass, the class/struct pass (with a superclass,
// so an EXTENDS edge exists), the module pass, the lib pass, the def and macro
// passes (so CONTAINS and CALLS edges exist), and depth.go's enum and alias
// passes. A carrier wired into any one of them would still be seen.
const carrierSrcCr6852 = `require "kemal"
require "json"

module Api
  class Handler < Base
    def handle(env)
      render(env)
    end

    macro define_helper(name)
      def {{name}}
        1
      end
    end
  end
end

lib LibC
  fun getpid : Int32
end

enum Colour
  Red
  Green = 2
end

alias Ids = Array(Int32)
`

// TestCrystal_ImportsFromEndResolves_6852 is the fix's behavioural test, and it
// drives every cell of crystal's depth/condition table in which the edge
// DANGLES. The remaining cells — the ones where the source itself spells the
// path — are driven by the two clause-3 tests below.
//
//	                          | nested src/app/api.cr      | root api.cr
//	require ≠ path,           | dangled (this test)        | dangled (this test)
//	no `def self.<path>`      |                            |
//	require SPELLED = path    | resolves already; clause 3 | resolves already;
//	                          | mints nothing              | clause 3 mints nothing
//	                          | (SelfRequiring test)       | (SelfRequiring test)
//	`def self.cr` in self.cr  | NOT EXPRESSIBLE — defRE    | resolves already;
//	                          | admits no '/', so the name | clause 3 mints nothing
//	                          | cannot be a nested path    | (DefSelf test)
//	                          | (DefSelf nested contrast)  |
//
// Every cell in THIS test FAILS before the carrier.
//
// Axis VARIED: path depth. HELD CONSTANT: the source, so the anchored edge set
// is identical in both rows and depth is the only difference between them.
func TestCrystal_ImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"src/app/api.cr", "api.cr"} {
		t.Run(path, func(t *testing.T) {
			// Premise: nothing crystal emits is named after the file here, so
			// the only record that can be named the path is the carrier itself.
			// This pins the "both depths dangled" claim rather than assuming it.
			pre := runCr6852(t, carrierSrcCr6852, path)
			named := crNamedExactly6852(pre, path)
			if len(named) != 1 {
				t.Fatalf("premise: exactly 1 record may be named %q (the carrier), got %d", path, len(named))
			}
			if named[0].Subtype != "file" {
				t.Fatalf("premise: the one record named %q must be the carrier, got subtype %q — "+
					"crystal names records after the require string, the class/struct, the "+
					"module, the lib, the def/macro, the enum, the alias or "+
					"\"spec_suite:\"+basename, never after the file", path, named[0].Subtype)
			}
			recs, byID := resolveCr6852(t, carrierSrcCr6852, path)
			assertCrImportsResolve6852(t, recs, byID)
		})
	}
}

// TestCrystal_NoCarrierWithoutARequire_6852 is the OVER-FIRING control for
// FileCarrierFor CLAUSE 2 — the half of the grade a "the edge now resolves"
// test cannot supply. It matches the `if !anchored` return path.
//
// Axis VARIED: the `require` lines (absent). HELD CONSTANT: a full record set —
// a class with a superclass, a module, a lib, a def, a macro, an enum, an alias,
// plus the CONTAINS/EXTENDS/CALLS/REFERENCES edges they own — so the file still
// extracts plenty and still runs every pass; only the path-anchored edge is
// gone. extractor.go:161 is the sole producer of one, so removing the requires
// removes the anchor and nothing else.
func TestCrystal_NoCarrierWithoutARequire_6852(t *testing.T) {
	const src = `module Api
  class Handler < Base
    def handle(env)
      render(env)
    end
  end
end

lib LibC
  fun getpid : Int32
end

enum Colour
  Red
  Green = 2
end

alias Ids = Array(Int32)
`
	for _, path := range []string{"src/app/api.cr", "api.cr"} {
		t.Run(path, func(t *testing.T) {
			recs := runCr6852(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(crPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(crCarriers6852(recs, path)); n != 0 {
				t.Errorf("a crystal file with nothing to carry must emit no file carrier, got %d — "+
					"an unconditional carrier mints one bare orphan node per .cr file across a "+
					"whole repo, which no recall-shaped assertion can see", n)
			}
			// Forbidden-row form: a carrier smuggled in under a different Kind
			// or Subtype is caught too.
			for _, r := range recs {
				if r.Subtype == "file" {
					t.Errorf("no Subtype=%q record may exist here, got kind=%q name=%q",
						"file", r.Kind, r.Name)
				}
			}
			for _, r := range crNamedExactly6852(recs, path) {
				t.Errorf("no record may be named %q here, got kind=%q subtype=%q name=%q qname=%q",
					path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
			}
		})
	}
}

// TestCrystal_EmptyRequireStringGetsNoCarrier_6852 reaches the SAME clause-2
// return path by a route the test above cannot: the require pass `continue`s on
// an empty capture (extractor.go:150-152), so text that looks exactly like an
// import produces no record and no anchored edge. A carrier keyed on the SOURCE
// TEXT ("this file has a require line") rather than on the emitted
// relationships is the mutant this kills and the one above does not.
//
// `require ""` is what requireRE's `[^"]+` cannot match, so the pass sees no
// match at all here; the sibling row uses a require whose path is a lone quote
// character, which also cannot match. Both are text a naive scanner reads as an
// import.
func TestCrystal_EmptyRequireStringGetsNoCarrier_6852(t *testing.T) {
	const src = `require ""

class Handler
  def handle(env)
    render(env)
  end
end
`
	for _, path := range []string{"src/app/api.cr", "api.cr"} {
		t.Run(path, func(t *testing.T) {
			recs := runCr6852(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(crPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d — "+
					"an empty require string matches nothing and emits no import record", n)
			}
			if n := len(crCarriers6852(recs, path)); n != 0 {
				t.Errorf("a crystal file whose only require has an empty path must emit "+
					"no file carrier, got %d", n)
			}
		})
	}
}

// TestCrystal_EmptyFileGetsNoCarrier_6852 drives Extract's FIRST return path:
// `len(file.Content) == 0` returns nil before extractCrystal runs. A carrier
// placed at the head of Extract rather than at the end of extractCrystal would
// mint a node for a file with no content whatsoever.
func TestCrystal_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const path = "src/app/empty.cr"
	recs := runCr6852(t, "", path)
	if len(recs) != 0 {
		t.Fatalf("an empty crystal file must extract no records at all, got %d (first: kind=%q name=%q)",
			len(recs), recs[0].Kind, recs[0].Name)
	}
}

// TestCrystal_EmptyPathGetsNoCarrier_6852 drives FileCarrierFor CLAUSE 1, and
// crystal is a caller for which that clause is doing real work rather than
// shadowing clause 2. With an empty path the IMPORTS edge's FromID is ALSO empty
// — extractor.go:161 stamps filePath verbatim — so clause 2's `FromID == path`
// test is trivially SATISFIED and only the empty-path guard rejects. That is
// exactly the case file_carrier.go's clause-1 paragraph describes, driven here
// through a production extractor.
func TestCrystal_EmptyPathGetsNoCarrier_6852(t *testing.T) {
	recs := runCr6852(t, carrierSrcCr6852, "")
	if len(recs) == 0 {
		t.Fatal("premise: fixture produced no records at all")
	}
	// Premise: the anchoring test WOULD pass — there is an IMPORTS edge whose
	// FromID equals the (empty) path. Without this row the assertion below could
	// pass because nothing anchored, which is a different reason.
	if n := len(crPathAnchored6852(recs, "")); n == 0 {
		t.Fatal("premise: want at least one relationship whose FromID is the empty path, got 0 — " +
			"without it clause 2 rejects first and clause 1 is not the thing under test")
	}
	for _, r := range recs {
		if r.Subtype == "file" {
			t.Errorf("an empty path must never mint a carrier, got kind=%q name=%q qname=%q — "+
				"a nameless carrier resolves nothing and adds a blank node",
				r.Kind, r.Name, r.QualifiedName)
		}
	}
}

// TestCrystal_OneCarrierPerFileNotPerRequire_6852 is the multiplicity control.
// Axis VARIED: the NUMBER of requires (five, mixing `require` and
// `require_relative`, each its own import record with its own path-anchored
// IMPORTS). HELD CONSTANT: one file, one path, driven at both depths. The
// carrier is per-FILE, not per-EDGE; a per-edge carrier would put five nodes
// under one graph.EntityID.
func TestCrystal_OneCarrierPerFileNotPerRequire_6852(t *testing.T) {
	const src = `require "kemal"
require "json"
require "redis"
require_relative "./item"
require_relative "./store"

class Handler
  def handle(env)
    render(env)
  end
end
`
	for _, path := range []string{"src/app/api.cr", "api.cr"} {
		t.Run(path, func(t *testing.T) {
			recs := runCr6852(t, src, path)
			if n := len(crPathAnchored6852(recs, path)); n != 5 {
				t.Fatalf("premise: want 5 path-anchored IMPORTS edges, got %d", n)
			}
			if n := len(crCarriers6852(recs, path)); n != 1 {
				t.Errorf("5 requires must still yield exactly 1 file carrier, got %d", n)
			}
			if n := len(crNamedExactly6852(recs, path)); n != 1 {
				t.Errorf("exactly 1 record may be named %q, got %d", path, n)
			}
		})
	}
}

// TestCrystal_SelfRequiringImportGetsNoSecondCarrier_6852 drives FileCarrierFor
// CLAUSE 3 by its FIRST route, at BOTH depths.
//
// requireRE captures `[^"]+` — every byte but a quote — so the import record's
// Name is the require string VERBATIM, slashes and extension included. A file
// whose require is spelled exactly like its own path therefore already carries a
// record named the path. graph.EntityID hashes (repo, kind, name, sourceFile)
// and NOT Subtype (#6369 / PR #6480), so a carrier beside it would land a second
// SCOPE.Component under that record's id.
//
// This is shell's self-sourcing route (#6882) and clojure's dot-leading stub
// route (#6897), reached here through the widest character class of the ten
// arms — and, unlike clojure's ns route and fsharp's module route, it is NOT
// root-only. The nested cell is what makes "clause 3 is not a root-depth
// phenomenon" a measurement for this package rather than an inference.
//
// PRODUCTION-REACHABILITY, stated honestly rather than over-claimed: the
// classifier premise row below drives that the router hands these paths to
// crystal at all, which is the half the astro arm's componentNameFromPath
// argument got wrong. What is NOT claimed is that a real Crystal project
// requires itself by its own repo-relative spelling; the search behind that is
// the seven-site Name enumeration in this file's header, which is what
// establishes that the require string is one of only two routes to clause 3.
// The cell is driven because #6369/#6480 make the duplicate real whenever it
// does occur, not because a corpus file exhibits it.
//
// THE CONTROL IS THE POINT. The same source with a require that is NOT the
// path DOES get a carrier (that is TestCrystal_ImportsFromEndResolves_6852),
// so this test cannot pass on an extractor that never mints a carrier at all.
func TestCrystal_SelfRequiringImportGetsNoSecondCarrier_6852(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"root", "api.cr"},
		{"nested", "src/app/api.cr"},
	}
	cls := classifier.New(nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Premise: the classifier actually routes this path to crystal.
			// Without this row "production-reachable" would be an assertion
			// about the extractor standing in for one about the classifier.
			res := cls.Classify(context.Background(), c.path)
			if res.Language != "crystal" || res.Skip {
				t.Fatalf("premise: classifier must route %q to crystal unskipped, got "+
					"language=%q skip=%v reason=%q", c.path, res.Language, res.Skip, res.SkipReason)
			}
			src := "require \"" + c.path + "\"\n\nclass Handler\n  def handle(env)\n    render(env)\n  end\nend\n"
			recs := runCr6852(t, src, c.path)
			if n := len(crPathAnchored6852(recs, c.path)); n == 0 {
				t.Fatal("premise: the fixture must still anchor an IMPORTS on the path, " +
					"or clause 2 rejects first and clause 3 is not the thing under test")
			}
			named := crNamedExactly6852(recs, c.path)
			if len(named) != 1 {
				t.Fatalf("exactly 1 record may be named %q, got %d — two records under one "+
					"graph.EntityID, which does not hash Subtype (#6369/#6480)", c.path, len(named))
			}
			if named[0].Subtype != "module" {
				t.Errorf("the one record named %q must be the import record (subtype %q), got %q",
					c.path, "module", named[0].Subtype)
			}
			if n := len(crCarriers6852(recs, c.path)); n != 0 {
				t.Errorf("no file carrier may be minted when the import record is already named "+
					"the path, got %d", n)
			}
		})
	}
}

// TestCrystal_DefSelfNamedLikeThePathGetsNoSecondCarrier_6852 drives
// FileCarrierFor CLAUSE 3 by its SECOND route, and it is the cell that grades
// the PERMISSIVE WIDENING of clause 3 that no other crystal cell can.
//
// defRE's group is `(?:self\.)?[\w?!]+`: one dot, only in the `self.` prefix,
// and never a '/'. So a ROOT file named `self.cr` declaring `def self.cr` emits
// a SCOPE.Operation named exactly the path — fsharp's root-only shape (#6880),
// through a different producer. The nested contrast row drives the closure: the
// same spelling cannot name a nested path, so a carrier IS due there.
//
// WHY THIS CELL EXISTS AT ALL. The plausible maintainer edit to clause 3 is
//
//	if records[i].Name == path && len(records[i].Relationships) > 0 {
//
// on the reasoning that "a path-named record carrying no edges is not really the
// file's node, so minting a proper carrier beside it is harmless". astro (#6891)
// killed that mutant; against clojure (#6897) it was ALIVE until a rel-less cell
// was written; cobol was ALIVE until a new cell was added. For crystal the
// require-string cell CANNOT kill it — the import record owns the IMPORTS edge,
// so its relationship count is 1 — and a top-level `def self.cr` with a
// call-free body owns ZERO relationships (its CALLS edges are built from its
// body, and the CONTAINS edge belongs to the ENCLOSING scope, of which a
// top-level def has none). This row is therefore the only thing in the package
// that distinguishes the widening. Distinguishable and ungraded, so it is graded
// here rather than recorded.
//
// The `== 0` variant of the same widening is killed by the require-string cell
// above, which is why the two tests are a pair and neither is redundant.
func TestCrystal_DefSelfNamedLikeThePathGetsNoSecondCarrier_6852(t *testing.T) {
	cls := classifier.New(nil)
	t.Run("root", func(t *testing.T) {
		const path = "self.cr"
		res := cls.Classify(context.Background(), path)
		if res.Language != "crystal" || res.Skip {
			t.Fatalf("premise: classifier must route %q to crystal unskipped, got "+
				"language=%q skip=%v reason=%q", path, res.Language, res.Skip, res.SkipReason)
		}
		const src = `require "json"

def self.cr
end
`
		recs := runCr6852(t, src, path)
		if n := len(crPathAnchored6852(recs, path)); n == 0 {
			t.Fatal("premise: the fixture must still anchor an IMPORTS on the path, " +
				"or clause 2 rejects first and clause 3 is not the thing under test")
		}
		named := crNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d — a carrier here would land a "+
				"second node under the def record's graph.EntityID, which does not hash "+
				"Subtype (#6369/#6480)", path, len(named))
		}
		if named[0].Kind != "SCOPE.Operation" {
			t.Errorf("the one record named %q must be the def record, got kind=%q subtype=%q",
				path, named[0].Kind, named[0].Subtype)
		}
		if n := len(named[0].Relationships); n != 0 {
			t.Fatalf("premise: the path-named def record must own ZERO relationships here, "+
				"got %d — otherwise this cell cannot distinguish the permissive widening "+
				"`Name == path && len(Relationships) > 0` from clause 3 as written", n)
		}
		if n := len(crCarriers6852(recs, path)); n != 0 {
			t.Errorf("no file carrier may be minted beside a path-named record that owns no "+
				"edges, got %d — graph.EntityID does not hash Subtype (#6369/#6480), so the "+
				"two land under one id", n)
		}
	})
	t.Run("nested_contrast", func(t *testing.T) {
		const path = "src/self.cr"
		const src = `require "json"

def self.cr
end
`
		recs := runCr6852(t, src, path)
		named := crNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d", path, len(named))
		}
		if named[0].Subtype != "file" {
			t.Errorf("at a nested path `def self.cr` is NOT expressible as a path-named record "+
				"(defRE admits no '/'), so the one record named %q must be the carrier; got "+
				"kind=%q subtype=%q", path, named[0].Kind, named[0].Subtype)
		}
	})
}

// TestCrystal_DepthPassRecordNamedLikeThePathGetsNoSecondCarrier_6852 grades
// the SECOND half of the placement decision, which the CONTAINS test below
// cannot reach.
//
// The shipped call sits AFTER extractor.go:345-347 — the three appends
// extractEnums / extractAliases / extractSpecSuite make — so the records they
// add are inside `records` when clause 3 runs. Moving the call to just BEFORE
// those appends leaves the CONTAINS pass untouched (every scopeSpan.idx has
// already been dereferenced by then) and every over-emission control green, so
// it is invisible to the rest of this file: SCORED AS SUCH — that mutant was
// ALIVE until this test existed.
//
// It is not, however, production-reachable, and this test says so in the row it
// asserts rather than in prose: enumRE and aliasRE both capture `[A-Z][\w:]*`,
// which admits no '.', and classifier.go:374 routes `.cr` and only `.cr` to
// crystal, so a classifier-routed path always contains a '.' and can never be an
// enum or alias name. The path used here is therefore one the classifier
// PROVABLY does not route — asserted below, so the test cannot be mistaken for a
// claim that it does. What it pins is the placement itself: the carrier call
// must see every record the file ultimately emits, because clause 3 is the guard
// against two nodes under one graph.EntityID (#6369/#6480) and it can only guard
// what it is handed.
//
// This is the "distinguishable and ungraded → grade it" disposition, applied to
// a placement conjunct rather than to a clause.
func TestCrystal_DepthPassRecordNamedLikeThePathGetsNoSecondCarrier_6852(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
	}{
		{"enum_named_like_the_path", "Colour", "require \"json\"\n\nenum Colour\n  Red\n  Green = 2\nend\n"},
		{"alias_named_like_the_path", "Ids", "require \"json\"\n\nalias Ids = Array(Int32)\n"},
	}
	cls := classifier.New(nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The INVERTED premise row. enumRE/aliasRE admit no '.', and only
			// `.cr` reaches this extractor, so this path is unreachable through
			// the router by construction. Asserting it keeps the test honest
			// about what it does and does not claim.
			if res := cls.Classify(context.Background(), c.path); res.Language == "crystal" {
				t.Fatalf("premise: %q must NOT be routed to crystal (classifier.go:374 maps "+
					"only \".cr\"); it now is, so this test's honesty note is stale and the "+
					"cell has become production-reachable", c.path)
			}
			recs := runCr6852(t, c.src, c.path)
			if n := len(crPathAnchored6852(recs, c.path)); n == 0 {
				t.Fatal("premise: the fixture must still anchor an IMPORTS on the path, " +
					"or clause 2 rejects first and clause 3 is not the thing under test")
			}
			named := crNamedExactly6852(recs, c.path)
			if len(named) != 1 {
				t.Fatalf("exactly 1 record may be named %q, got %d — the depth-pass record and a "+
					"carrier would land under one graph.EntityID, which does not hash Subtype "+
					"(#6369/#6480). The carrier call must run AFTER extractEnums/extractAliases/"+
					"extractSpecSuite so clause 3 can see what they append", c.path, len(named))
			}
			if named[0].Subtype == "file" {
				t.Errorf("the one record named %q must be the depth-pass record, not a carrier; "+
					"got kind=%q subtype=%q", c.path, named[0].Kind, named[0].Subtype)
			}
			if n := len(crCarriers6852(recs, c.path)); n != 0 {
				t.Errorf("no file carrier may be minted beside a record the depth passes already "+
					"named %q, got %d", c.path, n)
			}
		})
	}
}

// TestCrystal_CarrierShape_6852 pins what the carrier IS: stamped crystal,
// anchored on the file it names, owning no relationships of its own, and FIRST
// in the slice.
//
// The Language row is what grades the `lang` argument. crystal DOES run
// extractor.TagEntitiesLanguage (extractor.go:124), and the carrier is prepended
// at the end of extractCrystal, i.e. BEFORE that call, so an empty token would
// be filled in and the Language field ALONE could not distinguish `""` from
// `"crystal"`. The Properties row is what makes it distinguishable — fsharp's
// and clojure's provenance route (#6880, #6897): TagEntitiesLanguage stamps
// Properties["language"] ONLY on the fill path, extractor.FileEntity never sets
// that key, and every other crystal record sets Language explicitly at its
// construction site, so a carrier carrying that key is a carrier that arrived
// with an empty token. Both rows are asserted; the second is the premise for
// reading the first as evidence.
//
// The no-relationships row is not decoration: crystal's import records carry the
// IMPORTS edges themselves, so re-homing them onto the carrier would DOUBLE
// every import edge.
func TestCrystal_CarrierShape_6852(t *testing.T) {
	for _, path := range []string{"src/app/api.cr", "api.cr"} {
		t.Run(path, func(t *testing.T) {
			recs := runCr6852(t, carrierSrcCr6852, path)
			carriers := crCarriers6852(recs, path)
			if len(carriers) != 1 {
				t.Fatalf("want exactly 1 carrier for %q, got %d", path, len(carriers))
			}
			c := carriers[0]
			if c.Language != "crystal" {
				t.Errorf("carrier Language = %q, want %q", c.Language, "crystal")
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
				t.Errorf("carrier owns %d relationships, want 0 — crystal's import records carry "+
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

// TestCrystal_CarrierPlacementDoesNotShiftTheContainsPass_6852 pins the index
// hazard the carrier's position reaches, graded on its own rather than left
// inside a bulleted comment.
//
// THIS IS WHERE CRYSTAL DIFFERS FROM CLOJURE (#6897), AND IT IS THE FINDING OF
// THIS ARM. extractCrystal keeps `scopes`, a slice of scopeSpan whose `idx` is
// an INDEX INTO THE ENTITY SLICE. Step 3 dereferences it —
// `entities[cls.idx].Relationships = append(...)` at extractor.go:325 — to hang
// each def/macro's CONTAINS edge on its innermost enclosing scope. An insertion
// at position 0 after step 2 shifts every stored index by one, so each scope's
// CONTAINS edges land on the record BEFORE it. Nothing crashes; the graph is
// quietly wrong.
//
// For clojure that hazard turned out ENTAILED by clause 2 — its import records
// do not enter the entity slice until the very end, so a conditional carrier
// moved earlier simply mints nothing. FOR CRYSTAL IT IS NOT ENTAILED: the
// require pass is step 1, so the path-anchored edge already exists in `entities`
// before `scopes` is built, and a CONDITIONAL carrier placed between step 2 and
// step 3 is emitted for real and shifts the slice for real. The reason the
// shipped call sits at the END of extractCrystal is therefore this hazard and
// nothing else, and this test is its grade.
//
// The enumeration behind "the ONE consumer": scopeSpan.idx is read at exactly
// one site (extractor.go:325). `opIdx` in emitOperation is assigned and then
// discarded (`_ = opIdx`), so it has ZERO consumers and a shift cannot be
// observed through it. The three appends AFTER step 3 (extractEnums,
// extractAliases, extractSpecSuite at extractor.go:345-347) store no index and
// read none.
//
// The fixture is the ordinary Crystal shape that makes this production-reachable
// — a file that requires its dependencies and then declares a class with
// methods, i.e. the corpus fixture testdata/fixtures/sources/crystal in
// miniature. A class with no require would leave the carrier unemitted and the
// test vacuous, so the require is a premise, asserted below.
func TestCrystal_CarrierPlacementDoesNotShiftTheContainsPass_6852(t *testing.T) {
	const src = `require "kemal"

class Handler
  def handle(env)
    render(env)
  end
end

module Registry
  def register(x)
    store(x)
  end
end
`
	for _, path := range []string{"src/app/api.cr", "api.cr"} {
		t.Run(path, func(t *testing.T) {
			recs := runCr6852(t, src, path)
			// Premise: a carrier really is emitted, so the assertions below are
			// about a SHIFTED slice and not about an unchanged one. A full
			// revert makes this row go fatal, so the test is not vacuous.
			if n := len(crCarriers6852(recs, path)); n != 1 {
				t.Fatalf("premise: want exactly 1 carrier for %q, got %d", path, n)
			}
			for _, row := range []struct{ owner, method string }{
				{"Handler", "handle"},
				{"Registry", "register"},
			} {
				to := extractor.BuildOperationStructuralRef("crystal", path, row.method)
				owners := crRelOwners6852(recs, "CONTAINS", to)
				if len(owners) != 1 || owners[0] != row.owner {
					t.Errorf("CONTAINS -> %q is owned by %v, want exactly [%q] — "+
						"scopeSpan.idx indexes the entity slice, so a carrier prepended "+
						"before the def/macro pass re-homes this edge onto whichever record "+
						"now sits at that index", to, owners, row.owner)
				}
			}
			// The carrier owns nothing at all, which is the same statement read
			// from the other end: the record an off-by-one shift would hand the
			// edges to.
			carrier := crCarriers6852(recs, path)[0]
			if n := len(carrier.Relationships); n != 0 {
				t.Errorf("the carrier owns %d relationships, want 0 — it is the record at "+
					"index 0 and therefore the one a shifted scopeSpan.idx would target", n)
			}
		})
	}
}
