package clojure_test

// file_carrier_6852_test.go — #6852, clojure arm.
//
// buildImportEntities (clojure.go) stamps `FromID: filePath` on the IMPORTS
// edge of every (:require ...) / (:use ...) / (:import ...) entry.
// internal/resolve/refs.go has no path→entity index, so a path-valued FromID
// resolves if and only if some emitted node carries that exact string as its
// Name. Same defect #6815 fixed in erlang/nim/groovy and #6852 fixed in bicep
// (#6864), terraform (#6871), html (#6879), fsharp (#6880), shell (#6882),
// dockerfile, lua (#6885) and astro (#6891); same fix,
// extractor.PrependFileCarrier.
//
// ONE ANCHORING SITE, N EDGES — fsharp's / lua's / astro's multiplicity shape,
// not bicep's single edge and not terraform's two sites. buildImportEntities is
// the only bare `FromID: <path>` in the whole package: the CONTAINS edges the
// namespace record owns, the CALLS edges collectCalls builds and every
// IMPLEMENTS/EXTENDS edge hierarchy.go scans all leave FromID EMPTY, and
// hierarchy.go's header says why (a non-empty non-hex FromID would be rewritten
// away from the owning component). One import record is emitted per deduped
// (module, local name) pair, so an ns form with four requires anchors four
// edges on ONE string and must still gain exactly ONE carrier.
//
// BOTH DEPTHS DANGLED — html's, fsharp's, lua's and astro's shape, not
// terraform's root-by-accident and not shell's three-way split. Clojure names
// its records after the NAMESPACE ("app.core"), the defn/defmacro, the
// defrecord/deftype, or — for an import stub — topSegment(module), the module
// name cut at its first dot at index > 0 (see the clause-3 note below for what
// that guard does NOT cut). Nothing is named after the file or its basename
// under any condition, so there is no depth at which the edge resolved by the
// #6367 accident.
//
// CLAUSE 3 IS REACHABLE BY TWO ROUTES. FileCarrierFor clause 3 is
// `records[i].Name == path` (clause 1 is the empty-path guard, clause 2 the
// anchoring test). graph.EntityID hashes (repo, kind, name, sourceFile) and NOT
// Subtype (#6369 / PR #6480), so a carrier beside a path-named record would land
// a second SCOPE.Component under that record's id.
//
//   - THE NAMESPACE ROUTE, root depth only. fsharp's route (#6880): nsRE
//     captures a DOTTED `[\w\-\.]+`, so a root file core.clj whose header reads
//     `(ns core.clj)` emits a SCOPE.Component named exactly the path. nsRE
//     admits no '/', so at a nested path this route is closed — DRIVEN below,
//     not asserted.
//   - THE IMPORT-STUB ROUTE, any depth, for a path whose FIRST BYTE is a dot.
//     lua's and shell's route (#6885, #6882), reached here through a narrower
//     door. buildImportEntities names the stub topSegment(module), and
//     topSegment (clojure.go:648) guards with `dot > 0` — so when the module
//     begins with a dot it returns the module VERBATIM, slashes and extension
//     included. requireVecRE's class is `[\w\-\./]+`, so `.clj-kondo/hooks/foo.clj`
//     is a capturable module, and a file at that path requiring itself mints a
//     record named exactly the path. That is not a contrivance:
//     `.clj-kondo/hooks/*.clj` is a ubiquitous real Clojure repo layout, and
//     classifier.Classify routes it Skip:false, Tier:1. DRIVEN below at both
//     depths, with a non-dot-leading control.
//
// SO THE CHARACTER-SET CLOSURE IS NARROWER THAN THIS FILE FIRST CLAIMED, and the
// correction is recorded rather than quietly edited. The shipped text said the
// stub name "never CONTAINS a dot and can never equal a routed path", and that
// "this is where clojure differs from lua and shell". Both are false for a
// dot-leading path — under one, clojure does exactly what lua and shell do. The
// surviving claim, and it is the one the tests drive:
//
//   - defnRE / defmacroRE / deftypeRE capture `[\w\-\?!\*'+]+`, which admits
//     neither '.' nor '/'. Every path the classifier routes to clojure ends in
//     .clj / .cljs / .cljc / .edn (classifier.go:406-409) and therefore contains
//     a dot, so no operation or class record can equal any routed path, at any
//     depth. This one holds unconditionally.
//   - the stub route is closed for every module NOT beginning with '.', because
//     topSegment then cuts at a dot at index > 0 and the result contains none.
//
// There is NO behavioural defect on the dot-leading path — clause 3 declines,
// the IMPORTS edge resolves onto the path-named stub, and nothing is fabricated.
// What was wrong was a STATED PROOF OF UNREACHABILITY, the same shape
// hierarchy.go's `:gen-class` argument got wrong (it now carries a 40k-input
// panic proof) and the astro arm got wrong about componentNameFromPath. The
// repair is a driven cell, not a better argument. Do NOT "fix" topSegment to
// `dot >= 0`: that mints an empty-named stub.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .clj/.cljs/.cljc/.edn file across a whole repo — a change no recall-shaped
// assertion can see. The forbidden-row controls below are what forbid it, and
// each is matched to a DISTINCT return path:
//
//	Extract's `len(file.Content) == 0` → TestClojure_EmptyFileGetsNoCarrier_6852
//	FileCarrierFor clause 1 (path == "") → TestClojure_EmptyPathGetsNoCarrier_6852
//	FileCarrierFor clause 2 (!anchored)  → TestClojure_NoCarrierWithoutAnImport_6852
//	                                       and TestClojure_NoNsFormGetsNoCarrier_6852
//	FileCarrierFor clause 3 (path-named) → TestClojure_NamespaceNamedLikeThePathGetsNoSecondCarrier_6852
//	                                       and TestClojure_SelfRequiringStubUnderADotLeadingPathGetsNoSecondCarrier_6852

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/clojure"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// runClj6852 drives the registered production extractor over src at path.
func runClj6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("clojure")
	if !ok {
		t.Fatal("clojure extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "clojure",
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// cljCarriers6852 returns every record that IS the file carrier for path — the
// SCOPE.Component/file record extractor.FileEntity mints. Subtype "file" is
// what separates it from the namespace record (Subtype "namespace"), the class
// records (Subtype "class") and the import stubs (Subtype "").
func cljCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// cljPathAnchored6852 returns every relationship in recs whose FromID is exactly
// path — the shape whose FROM end has nothing to resolve onto.
func cljPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
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

// cljNamedExactly6852 returns every record whose Name or QualifiedName is path.
// This is the resolution question internal/resolve/refs.go actually asks: it has
// no path→entity index, so a path-valued FromID resolves if and only if such a
// record exists.
func cljNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// cljRelOwners6852 returns the Name of EVERY record owning at least one
// relationship of kind k whose ToID is to, in slice order. It returns the full
// list rather than the first or last match on purpose: a first-wins scan reports
// one owner however many there are, so an edge attached to an EXTRA record while
// the intended owner keeps its own would read as correct. That is precisely the
// shape a mis-placed carrier produces here, because compOffset.idx is an INDEX
// into the entity slice.
func cljRelOwners6852(recs []types.EntityRecord, k, to string) []string {
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

// resolveClj6852 extracts src at path, stamps ids the way graph assembly does,
// runs the production resolver pipeline, and returns the records plus the
// id→record index. This asserts on the EMITTED ARTEFACT after resolution — the
// edge's FROM end — not on a helper's return value or a counter.
func resolveClj6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := runClj6852(t, src, path)
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

// assertCljImportsResolve6852 fails for every IMPORTS edge whose FROM end names
// no record, and fails outright when the fixture produced no IMPORTS at all — a
// resolution assertion over an empty set is a no-op that reads like a guard.
func assertCljImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
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

// carrierSrcClj6852 is the canonical Clojure namespace: an ns form with a
// :require vector (aliased), a bare :require symbol, a :use and a Java :import,
// plus a defn, a defmacro and a defrecord. It exercises every pass of
// extractClojure, so a carrier wired into any one of them would still be seen.
const carrierSrcClj6852 = `(ns app.core
  (:require [clojure.string :as str]
            clojure.set)
  (:use clojure.walk)
  (:import [java.util Date]))

(defrecord Point [x y])

(defmacro with-log [& body]
  ` + "`" + `(do ~@body))

(defn render [p]
  (str/join "," [(:x p) (:y p)]))
`

// TestClojure_ImportsFromEndResolves_6852 is the fix's behavioural test, and it
// drives EVERY cell of clojure's depth/condition table.
//
// The table has two axes. DEPTH: nested and root. CONDITION: whether the ns
// form's name is spelled like the file's path. The second axis exists because
// nsRE captures a dotted `[\w\-\.]+`, which is fsharp's clause-3 route — and it
// is the ONLY producer in the package that can reach it, since defn/defmacro/
// deftype names admit no '.' or '/' and an import stub is cut at its first dot.
//
//	                       | nested src/app/core.clj | root core.clj
//	ns name ≠ path         | dangled (this test)     | dangled (this test)
//	ns name spelled = path | NOT EXPRESSIBLE — nsRE  | resolves already, and
//	                       | admits no '/', so the   | clause 3 correctly mints
//	                       | name truncates to "src" | nothing
//	                       | and the file dangles    |
//	                       | (this test, 3rd cell)   | (the clause-3 test below)
//
// Three cells here, one in TestClojure_NamespaceNamedLikeThePathGetsNoSecondCarrier_6852.
// Every cell in THIS test FAILS before the carrier.
//
// Axis VARIED: path depth, and the ns name's spelling. HELD CONSTANT: the
// :require/:use/:import body, so the anchored edge set is the same in all three.
func TestClojure_ImportsFromEndResolves_6852(t *testing.T) {
	const importBody = `
  (:require [clojure.string :as str])
  (:import [java.util Date]))

(defn render [p] (str/join "," p))
`
	cases := []struct {
		name string
		path string
		src  string
	}{
		{"nested", "src/app/core.clj", "(ns app.core" + importBody},
		{"root", "core.clj", "(ns app.core" + importBody},
		// The third cell: the ns name is WRITTEN as the nested path. nsRE's
		// character class has no '/', so the captured name truncates at the
		// first slash and the record is named "src" — no record is named the
		// path, and the file dangles exactly like the ordinary nested case.
		// Driven rather than asserted: this is what makes "clause 3 is a
		// root-depth phenomenon for clojure" a measurement.
		{"nested_ns_spelled_like_path", "src/app/core.clj", "(ns src/app/core.clj" + importBody},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Premise: nothing clojure emits is named after the file here, so
			// the only record that can be named the path is the carrier itself.
			// This pins the "both depths dangled" claim rather than assuming it.
			pre := runClj6852(t, c.src, c.path)
			named := cljNamedExactly6852(pre, c.path)
			if len(named) != 1 {
				t.Fatalf("premise: exactly 1 record may be named %q (the carrier), got %d", c.path, len(named))
			}
			if named[0].Subtype != "file" {
				t.Fatalf("premise: the one record named %q must be the carrier, got subtype %q — "+
					"clojure names records after the namespace, the defn/defmacro, the "+
					"defrecord/deftype or topSegment(module), never after the file", c.path, named[0].Subtype)
			}
			recs, byID := resolveClj6852(t, c.src, c.path)
			assertCljImportsResolve6852(t, recs, byID)
		})
	}
}

// TestClojure_NoCarrierWithoutAnImport_6852 is the OVER-FIRING control for
// FileCarrierFor CLAUSE 2 — the half of the grade a "the edge now resolves"
// test cannot supply. It matches the `if !anchored` return path.
//
// Axis VARIED: the :require/:use/:import sections (absent). HELD CONSTANT: a
// full record set — an ns form, a defn, a defmacro, a defrecord, a CALLS edge
// and the namespace's CONTAINS edges — so the file still extracts plenty and
// still runs every pass; only the path-anchored edge is gone.
func TestClojure_NoCarrierWithoutAnImport_6852(t *testing.T) {
	const src = `(ns app.core)

(defrecord Point [x y])

(defmacro with-log [& body] body)

(defn helper [x] (inc x))

(defn render [p] (helper p))
`
	for _, path := range []string{"src/app/core.clj", "core.clj"} {
		t.Run(path, func(t *testing.T) {
			recs := runClj6852(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(cljPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(cljCarriers6852(recs, path)); n != 0 {
				t.Errorf("a clojure file with nothing to carry must emit no file carrier, got %d — "+
					"an unconditional carrier mints one bare orphan node per .clj file across a "+
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
			for _, r := range cljNamedExactly6852(recs, path) {
				t.Errorf("no record may be named %q here, got kind=%q subtype=%q name=%q qname=%q",
					path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
			}
		})
	}
}

// TestClojure_NoNsFormGetsNoCarrier_6852 reaches the SAME clause-2 return path
// by a route the test above cannot: collectImports is fed findNsForm's body, so
// a file with NO (ns ...) form collects no imports at all however many require
// vectors its text contains. The fixture therefore carries a bare `(:require
// [clojure.string :as str])` at top level — text that looks exactly like an
// import and produces none — which is what separates this from the test above.
//
// A carrier keyed on the SOURCE TEXT rather than on the emitted relationships
// is the mutant this kills and the one above does not.
func TestClojure_NoNsFormGetsNoCarrier_6852(t *testing.T) {
	const src = `;; no (ns ...) form anywhere
(:require [clojure.string :as str])

(defn render [p] (str/join "," p))
`
	for _, path := range []string{"src/app/core.clj", "core.clj"} {
		t.Run(path, func(t *testing.T) {
			recs := runClj6852(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(cljPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d — "+
					"collectImports reads findNsForm's body, so a file with no ns form "+
					"anchors nothing", n)
			}
			if n := len(cljCarriers6852(recs, path)); n != 0 {
				t.Errorf("a clojure file whose require text is outside any ns form must emit "+
					"no file carrier, got %d", n)
			}
		})
	}
}

// TestClojure_EmptyFileGetsNoCarrier_6852 drives Extract's FIRST return path:
// `len(file.Content) == 0` returns nil before extractClojure runs. A carrier
// placed at the head of Extract rather than at the end of extractClojure would
// mint a node for a file with no content whatsoever.
func TestClojure_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const path = "src/app/empty.clj"
	recs := runClj6852(t, "", path)
	if len(recs) != 0 {
		t.Fatalf("an empty clojure file must extract no records at all, got %d (first: kind=%q name=%q)",
			len(recs), recs[0].Kind, recs[0].Name)
	}
}

// TestClojure_EmptyPathGetsNoCarrier_6852 drives FileCarrierFor CLAUSE 1, and
// clojure is a caller for which that clause is doing real work rather than
// shadowing clause 2. With an empty path the IMPORTS edge's FromID is ALSO
// empty — buildImportEntities stamps filePath verbatim — so clause 2's
// `FromID == path` test is trivially SATISFIED and only the empty-path guard
// rejects. That is exactly the case file_carrier.go's clause-1 paragraph
// describes, driven here through a production extractor.
func TestClojure_EmptyPathGetsNoCarrier_6852(t *testing.T) {
	recs := runClj6852(t, carrierSrcClj6852, "")
	if len(recs) == 0 {
		t.Fatal("premise: fixture produced no records at all")
	}
	// Premise: the anchoring test WOULD pass — there is an IMPORTS edge whose
	// FromID equals the (empty) path. Without this row the assertion below
	// could pass because nothing anchored, which is a different reason.
	if n := len(cljPathAnchored6852(recs, "")); n == 0 {
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

// TestClojure_OneCarrierPerFileNotPerImport_6852 is the multiplicity control.
// Axis VARIED: the NUMBER of import entries (six, spread across :require, :use
// and :import, each its own stub with its own path-anchored IMPORTS). HELD
// CONSTANT: one file, one path, driven at both depths. The carrier is per-FILE,
// not per-EDGE; a per-edge carrier would put six nodes under one id.
func TestClojure_OneCarrierPerFileNotPerImport_6852(t *testing.T) {
	const src = `(ns app.core
  (:require [clojure.string :as str]
            [clojure.set :refer [union]]
            clojure.edn)
  (:use clojure.walk)
  (:import [java.util Date Calendar]))

(defn render [p] (str/join "," p))
`
	for _, path := range []string{"src/app/core.clj", "core.clj"} {
		t.Run(path, func(t *testing.T) {
			recs := runClj6852(t, src, path)
			if n := len(cljPathAnchored6852(recs, path)); n != 6 {
				t.Fatalf("premise: want 6 path-anchored IMPORTS edges, got %d", n)
			}
			if n := len(cljCarriers6852(recs, path)); n != 1 {
				t.Errorf("6 import entries must still yield exactly 1 file carrier, got %d", n)
			}
			if n := len(cljNamedExactly6852(recs, path)); n != 1 {
				t.Errorf("exactly 1 record may be named %q, got %d", path, n)
			}
		})
	}
}

// TestClojure_NamespaceNamedLikeThePathGetsNoSecondCarrier_6852 drives
// FileCarrierFor CLAUSE 3, and completes the fourth cell of the table in
// TestClojure_ImportsFromEndResolves_6852.
//
// nsRE captures a DOTTED `[\w\-\.]+`, so a root file core.clj whose header
// reads `(ns core.clj)` emits a SCOPE.Component named exactly the path — the
// fsharp route (#6880), reached through the namespace NAME. graph.EntityID
// hashes (repo, kind, name, sourceFile) and NOT Subtype (#6369 / PR #6480), so
// a carrier there would land a second SCOPE.Component under the namespace
// record's id.
//
// This is ONE of clause 3's two routes for clojure, not the only one. The other
// is the import stub under a dot-leading path, driven by
// TestClojure_SelfRequiringStubUnderADotLeadingPathGetsNoSecondCarrier_6852 —
// see this file's header for the correction that produced it.
//
// The NESTED subtest is the contrast that stops this passing on a carrier that
// is never emitted at all: at a nested path the same spelling is not
// expressible (nsRE admits no '/'), so a carrier IS due and one appears.
func TestClojure_NamespaceNamedLikeThePathGetsNoSecondCarrier_6852(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		const path = "core.clj"
		src := "(ns " + path + "\n  (:require [clojure.string :as str]))\n\n(defn render [p] (str/join \",\" p))\n"
		recs := runClj6852(t, src, path)
		if n := len(cljPathAnchored6852(recs, path)); n == 0 {
			t.Fatal("premise: the fixture must still anchor an IMPORTS on the path, " +
				"or clause 2 rejects first and clause 3 is not the thing under test")
		}
		named := cljNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d — a carrier here would land a "+
				"second SCOPE.Component under the namespace record's graph.EntityID, which does "+
				"not hash Subtype (#6369/#6480)", path, len(named))
		}
		if named[0].Subtype != "namespace" {
			t.Errorf("the one record named %q must be the namespace record, got subtype %q",
				path, named[0].Subtype)
		}
		if n := len(cljCarriers6852(recs, path)); n != 0 {
			t.Errorf("no file carrier may be minted when the namespace record already carries "+
				"the path as its Name, got %d", n)
		}
	})
	// The path-named namespace record with NO relationships of its own. This is
	// the PERMISSIVE WIDENING control, and it is a separate cell rather than a
	// variation of the row above: the plausible maintainer edit to clause 3 is
	//
	//	if records[i].Name == path && len(records[i].Relationships) > 0 {
	//
	// on the reasoning that "a path-named record carrying no edges is not really
	// the file's node, so minting a proper carrier beside it is harmless". astro
	// (#6891) scored that mutant and killed it — but only with an astro fixture.
	// Against clojure's package alone it was ALIVE, because the root fixture
	// above declares a defn and its namespace record therefore owns a CONTAINS.
	// A root file whose ns form has a :require and NO top-level declaration is
	// the ordinary shape that separates them, and it is production-reachable —
	// a re-export/facade namespace is exactly that. Distinguishable and
	// ungraded, so it is graded here rather than recorded.
	t.Run("root_namespace_owning_no_edges", func(t *testing.T) {
		const path = "core.clj"
		src := "(ns " + path + "\n  (:require [clojure.string :as str]))\n"
		recs := runClj6852(t, src, path)
		if n := len(cljPathAnchored6852(recs, path)); n == 0 {
			t.Fatal("premise: the fixture must still anchor an IMPORTS on the path")
		}
		named := cljNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d", path, len(named))
		}
		if n := len(named[0].Relationships); n != 0 {
			t.Fatalf("premise: the path-named namespace record must own ZERO relationships "+
				"here, got %d — otherwise this cell is the same as the one above and the "+
				"permissive widening of clause 3 is not under test", n)
		}
		if n := len(cljCarriers6852(recs, path)); n != 0 {
			t.Errorf("no file carrier may be minted beside a path-named record that owns no "+
				"edges, got %d — graph.EntityID does not hash Subtype (#6369/#6480), so the "+
				"two land under one id", n)
		}
	})
	t.Run("nested_contrast", func(t *testing.T) {
		const path = "src/app/core.clj"
		src := "(ns " + path + "\n  (:require [clojure.string :as str]))\n\n(defn render [p] (str/join \",\" p))\n"
		recs := runClj6852(t, src, path)
		named := cljNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d", path, len(named))
		}
		if named[0].Subtype != "file" {
			t.Errorf("at a nested path the same source is NOT expressible as a path-named "+
				"namespace (nsRE admits no '/'), so the one record named %q must be the "+
				"carrier; got subtype %q", path, named[0].Subtype)
		}
	})
}

// TestClojure_SelfRequiringStubUnderADotLeadingPathGetsNoSecondCarrier_6852
// drives clause 3's SECOND route, which this file originally argued did not
// exist. The correction, and why it is a driven cell rather than a better
// argument, are in the header; the mechanism in one line:
//
//	topSegment (clojure.go:648) guards with `dot > 0`, so a module whose FIRST
//	byte is a dot comes back VERBATIM — slashes and extension included.
//
// requireVecRE's class is `[\w\-\./]+`, so `.clj-kondo/hooks/foo.clj` is a
// capturable module and a file at that path requiring itself mints a record
// named exactly the path — lua's and shell's self-reference route, reached
// through a narrower door. It is production-reachable rather than merely
// expressible: `.clj-kondo/hooks/*.clj` is a ubiquitous real Clojure repo
// layout, and the classifier PREMISE ROW below drives that rather than asserting
// it, because "the extractor would do X if the file reached it" is not the same
// claim as "the file reaches it" — that gap is exactly what the astro arm's
// componentNameFromPath argument got wrong.
//
// THE CONTROL IS THE POINT. A non-dot-leading nested path with the identical
// self-require DOES get a carrier, because topSegment then cuts at a dot at
// index > 0. Without that row the test would pass on an extractor that never
// mints a carrier at all, and the surviving half of the character-set claim
// would be ungraded.
func TestClojure_SelfRequiringStubUnderADotLeadingPathGetsNoSecondCarrier_6852(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		wantCarrier bool
	}{
		// Dot-leading, root depth.
		{"dot_leading_root", ".core.clj", false},
		// Dot-leading, NESTED — the real-world layout, and the cell that
		// falsifies "root depth only".
		{"dot_leading_nested_clj_kondo", ".clj-kondo/hooks/foo.clj", false},
		// Control: same source shape, first byte not a dot. topSegment cuts at
		// the dot in "src/app/core.clj" and yields "src/app/core", which is not
		// the path, so clause 3 does not fire and a carrier IS due.
		{"not_dot_leading_nested_control", "src/app/core.clj", true},
	}
	cls := classifier.New(nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Premise: the classifier actually routes this path to clojure.
			// Without this row "production-reachable" would be an assertion
			// about the extractor standing in for one about the classifier.
			res := cls.Classify(context.Background(), c.path)
			if res.Language != "clojure" || res.Skip {
				t.Fatalf("premise: classifier must route %q to clojure unskipped, got "+
					"language=%q skip=%v reason=%q", c.path, res.Language, res.Skip, res.SkipReason)
			}
			src := "(ns app.core\n  (:require [" + c.path + " :as self]))\n"
			recs := runClj6852(t, src, c.path)
			if n := len(cljPathAnchored6852(recs, c.path)); n == 0 {
				t.Fatal("premise: the fixture must still anchor an IMPORTS on the path, " +
					"or clause 2 rejects first and clause 3 is not the thing under test")
			}
			named := cljNamedExactly6852(recs, c.path)
			if len(named) != 1 {
				t.Fatalf("exactly 1 record may be named %q, got %d — two records under one "+
					"graph.EntityID, which does not hash Subtype (#6369/#6480)", c.path, len(named))
			}
			carriers := len(cljCarriers6852(recs, c.path))
			if c.wantCarrier {
				if carriers != 1 {
					t.Errorf("want 1 carrier for %q (topSegment cuts a module whose first byte "+
						"is not a dot, so the stub is not named the path), got %d", c.path, carriers)
				}
				if named[0].Subtype != "file" {
					t.Errorf("the one record named %q must be the carrier, got subtype %q",
						c.path, named[0].Subtype)
				}
				return
			}
			if carriers != 0 {
				t.Errorf("no file carrier may be minted for %q: topSegment returns a "+
					"dot-leading module VERBATIM, so the import stub is already named the "+
					"path and clause 3 must decline; got %d", c.path, carriers)
			}
			if named[0].Subtype == "file" {
				t.Errorf("the one record named %q must be the import stub, not a carrier", c.path)
			}
		})
	}
}

// TestClojure_CarrierShape_6852 pins what the carrier IS: stamped clojure,
// anchored on the file it names, owning no relationships of its own, and FIRST
// in the slice.
//
// The Language row is what grades the `lang` argument. clojure DOES run
// extractor.TagEntitiesLanguage (clojure.go), and the carrier is prepended
// BEFORE that call, so an empty token would be filled in and the Language field
// alone could not distinguish `""` from `"clojure"`. The Properties row is what
// makes it distinguishable — fsharp's provenance route (#6880):
// TagEntitiesLanguage stamps Properties["language"] ONLY on the fill path, and
// every other clojure record sets Language explicitly, so a carrier carrying
// that key is a carrier that arrived with an empty token. Both rows are
// asserted; the second is the premise for reading the first as evidence.
//
// The no-relationships row is not decoration: clojure's import stubs carry the
// IMPORTS edges themselves, so re-homing them onto the carrier would DOUBLE
// every import edge.
func TestClojure_CarrierShape_6852(t *testing.T) {
	for _, path := range []string{"src/app/core.clj", "core.clj"} {
		t.Run(path, func(t *testing.T) {
			recs := runClj6852(t, carrierSrcClj6852, path)
			carriers := cljCarriers6852(recs, path)
			if len(carriers) != 1 {
				t.Fatalf("want exactly 1 carrier for %q, got %d", path, len(carriers))
			}
			c := carriers[0]
			if c.Language != "clojure" {
				t.Errorf("carrier Language = %q, want %q", c.Language, "clojure")
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
			if n := len(c.Relationships); n != 0 {
				t.Errorf("carrier owns %d relationships, want 0 — clojure's import stubs carry "+
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

// TestClojure_CarrierPlacementDoesNotShiftTheExtendPass_6852 pins the index
// hazard the carrier's position could reach, graded on its own rather than left
// inside a bulleted comment — and the grading is what showed the hazard to be
// ENTAILED by clause 2 rather than an independent placement constraint. See the
// paragraph below the mechanism.
//
// extractClojure keeps `comps`, a slice of compOffset{idx, name} where idx is an
// INDEX INTO THE ENTITY SLICE. Step 3b dereferences it — `rec := &entities[cp.idx]`
// — to attach the extend-type / extend-protocol edges hierarchy.go scanned by
// NAME. An insertion at position 0 before that step shifts every stored index by
// one, so each component's IMPLEMENTS edges land on the record BEFORE it, and
// the record before the first component is the carrier itself. Nothing crashes;
// the graph is quietly wrong. That is lua's #6885 hazard, in a package that has
// it too.
//
// THE MUTANT THIS KILLS IS NOT "the carrier moved", AND THAT IS THE FINDING.
// Moving the SHIPPED (conditional) call above step 3b — or above step 4 — is
// DEAD, but on the EMPTINESS of the result, never on ordering: importEntities
// do not enter `entities` until the head-prepend at clojure.go:316, so clause 2
// rejects at every earlier point and no carrier is minted at all. The index
// hazard is therefore ENTAILED by the clause-2 requirement rather than an
// independent constraint on that call, and the only shape that reaches it is an
// UNCONDITIONAL carrier placed before step 3b. Scored: that mutant produces
// FOUR wrong-owner rows here (the carrier takes Triangle's IMPLEMENTS -> Shape;
// Hexagon's IMPLEMENTS -> Sized lands on Triangle), at both depths — and the
// SAME unconditional carrier placed AFTER step 3b produces ZERO, which is what
// separates this test's subject from the over-emission tests' subject.
//
// The enumeration behind "the ONE conjunct": compOffset.idx has exactly one
// consumer (clojure.go:283, step 3b). opOffset.idx has ZERO consumers — step 4
// reads only op.name — so a shift cannot be observed through it, and no test is
// written for something that cannot be observed.
//
// The fixture is the ordinary Clojure shape that makes this production-reachable
// — a namespace that requires its protocols and then extends its own records —
// i.e. the golden fixture clojure-protocols-mini/src/extend.clj in miniature. An
// extend form with no :require would leave the carrier unemitted and the test
// vacuous, so the :require is a premise, asserted below.
func TestClojure_CarrierPlacementDoesNotShiftTheExtendPass_6852(t *testing.T) {
	const src = `(ns app.extend
  (:require [app.protocols :refer [Shape Sized]]))

(defrecord Triangle [base height])

(defrecord Hexagon [side])

(extend-type Triangle
  Shape
  (area [this] 1))

(extend-protocol Sized
  Hexagon
  (size [this] 2))
`
	for _, path := range []string{"src/app/extend.clj", "extend.clj"} {
		t.Run(path, func(t *testing.T) {
			recs := runClj6852(t, src, path)
			// Premise: a carrier really is emitted, so the assertions below are
			// about a SHIFTED slice and not about an unchanged one. A full
			// revert makes this row go fatal, so the test is not vacuous.
			if n := len(cljCarriers6852(recs, path)); n != 1 {
				t.Fatalf("premise: want exactly 1 carrier for %q, got %d", path, n)
			}
			for _, row := range []struct{ owner, to string }{
				{"Triangle", "Shape"},
				{"Hexagon", "Sized"},
			} {
				owners := cljRelOwners6852(recs, "IMPLEMENTS", row.to)
				if len(owners) != 1 || owners[0] != row.owner {
					t.Errorf("IMPLEMENTS -> %q is owned by %v, want exactly [%q] — "+
						"compOffset.idx indexes the entity slice, so a carrier prepended "+
						"before the extend-type/extend-protocol pass re-homes this edge onto "+
						"whichever record now sits at that index",
						row.to, owners, row.owner)
				}
			}
			// The carrier owns nothing at all, which is the same statement read
			// from the other end: the record an off-by-one shift would hand the
			// edges to.
			carrier := cljCarriers6852(recs, path)[0]
			if n := len(carrier.Relationships); n != 0 {
				t.Errorf("the carrier owns %d relationships, want 0 — it is the record at "+
					"index 0 and therefore the one a shifted compOffset.idx would target", n)
			}
		})
	}
}
