package feedback

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/cross/imports"
	"github.com/cajasmota/grafel/internal/extractors/haskell"
	"github.com/cajasmota/grafel/internal/extractors/idris"
	"github.com/cajasmota/grafel/internal/extractors/vbnet"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6536. The field-extraction metric had never sampled a real class in any
// C#-family codebase: classLikeKindTails admitted class/struct/model/schema,
// while every VB.NET and C# class/structure/module/interface is emitted as
// SCOPE.Component. The 253-entity "100% zero fields" figure in #6535 was the
// SCOPE.Schema residue (enums and consts) after field leaves were filtered —
// a population that structurally cannot own fields, so 100% was guaranteed
// rather than measured.
//
// These tests deliberately take their kind strings FROM THE EXTRACTOR rather
// than from a hand-written literal. The entire defect was that the tail list
// and the extractor disagreed and nothing ever compared them; a fixture that
// hard-codes "SCOPE.Component" would agree with whatever the list says and
// reproduce the same blind spot.

const vbSource6536 = `
Namespace Demo
    Public Class Widget
        Public Name As String
        Private count As Integer

        Public Const Limit As Integer = 10

        Public Sub Run()
        End Sub
    End Class

    Public Structure Box
        Public Width As Integer
    End Structure

    Public Enum Colour
        Red
        Green
    End Enum

End Namespace
`

// extract6536 runs the real VB.NET extractor over src and returns the records.
func extract6536(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex := &vbnet.Extractor{}
	recs, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.vb",
		Content:  []byte(src),
		Language: "vbnet",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(recs) == 0 {
		t.Fatalf("Extract returned no records")
	}
	return recs
}

// recordsToDoc6536 projects extractor records onto a graph.Document, binding
// the structural-ref ToIDs the way the resolver's byLocation fallback does:
// the trailing ":"-separated segment of a Format A ref is exactly the target
// entity's Name (internal/extractor/structural_ref.go).
func recordsToDoc6536(t *testing.T, recs []types.EntityRecord) *graph.Document {
	t.Helper()
	ents := make([]graph.Entity, 0, len(recs))
	byName := make(map[string]string, len(recs))
	for i := range recs {
		id := "e" + string(rune('A'+i))
		r := &recs[i]
		byName[r.Name] = id
		ents = append(ents, graph.Entity{
			ID:         id,
			Name:       r.Name,
			Kind:       r.Kind,
			Subtype:    r.Subtype,
			Language:   r.Language,
			SourceFile: r.SourceFile,
			StartLine:  r.StartLine,
		}.WithProperties(map[string]string{}))
	}

	var rels []graph.Relationship
	for i := range recs {
		from := ents[i].ID
		for j, rr := range recs[i].Relationships {
			name := rr.ToID
			if k := strings.LastIndex(name, ":"); k >= 0 {
				name = name[k+1:]
			}
			to, ok := byName[name]
			if !ok {
				continue // unresolved external — irrelevant to this metric
			}
			rels = append(rels, graph.Relationship{
				ID:     from + "-" + to + "-" + string(rune('0'+j)),
				FromID: from,
				ToID:   to,
				Kind:   rr.Kind,
			})
		}
	}
	return makeDoc(ents, rels)
}

// kindOf6536 returns the kind/subtype the extractor actually emitted for the
// named declaration.
func kindOf6536(t *testing.T, recs []types.EntityRecord, name string) (kind, subtype string) {
	t.Helper()
	for i := range recs {
		if recs[i].Name == name {
			return recs[i].Kind, recs[i].Subtype
		}
	}
	t.Fatalf("no entity named %q in extractor output", name)
	return "", ""
}

// TestClassLikeKindTailsAdmitsExtractorTypeKinds_6536 compares the tail list
// against the kinds the VB.NET extractor really emits for type declarations.
// This is the assertion whose absence let #6536 ship.
func TestClassLikeKindTailsAdmitsExtractorTypeKinds_6536(t *testing.T) {
	recs := extract6536(t, vbSource6536)
	for _, name := range []string{"Widget", "Box"} {
		kind, _ := kindOf6536(t, recs, name)
		if !isClassLikeKind(kind) {
			t.Errorf("type %s is emitted as kind %q, which isClassLikeKind rejects: "+
				"the field-extraction metric cannot sample it", name, kind)
		}
	}
}

// TestFieldExtractionSamplesRealClasses_6536 is the killing test for the
// missing "component" tail: a class with field children must report a
// NON-100% zero-field rate.
func TestFieldExtractionSamplesRealClasses_6536(t *testing.T) {
	recs := extract6536(t, vbSource6536)
	doc := recordsToDoc6536(t, recs)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.FieldExtractionRate.ClassTotal == 0 {
		t.Fatalf("no class-like entities sampled; the metric never saw the real classes")
	}
	if r.FieldExtractionRate.ZeroFieldsPct == 100.0 {
		t.Fatalf("zero-field rate is 100%% on a graph whose Widget owns 2 fields "+
			"and Box owns 1: ClassTotal=%d", r.FieldExtractionRate.ClassTotal)
	}
	// Widget (2 fields) and Box (1 field) are the only candidates once enums
	// and consts are exempt, so nothing is zero-field.
	if r.FieldExtractionRate.ZeroFieldsPct != 0.0 {
		t.Errorf("ZeroFieldsPct = %v, want 0; ClassTotal=%d",
			r.FieldExtractionRate.ZeroFieldsPct, r.FieldExtractionRate.ClassTotal)
	}
	if r.FieldExtractionRate.ClassTotal != 2 {
		t.Errorf("ClassTotal = %d, want 2 (Widget, Box)", r.FieldExtractionRate.ClassTotal)
	}
}

// TestEnumAndConstExemptFromFieldDenominator_6536 pins the denominator
// decision: enum and const declarations cannot own field children, so leaving
// them in the denominator is a permanent floor of false failures.
func TestEnumAndConstExemptFromFieldDenominator_6536(t *testing.T) {
	recs := extract6536(t, vbSource6536)

	// Guard the premise against the extractor: Colour/Limit must really be
	// class-like-kinded (SCOPE.Schema) and subtyped enum/const, otherwise this
	// test is exempting something that was never in the denominator.
	for _, name := range []string{"Colour", "Widget.Limit"} {
		kind, subtype := kindOf6536(t, recs, name)
		if !isClassLikeKind(kind) {
			t.Fatalf("premise gone: %s has kind %q which is not class-like", name, kind)
		}
		if subtype != "enum" && subtype != "const" {
			t.Fatalf("premise gone: %s has subtype %q", name, subtype)
		}
	}

	// A graph of ONLY enums and consts must sample nothing at all, rather than
	// reporting a guaranteed 100% failure.
	var only []types.EntityRecord
	for i := range recs {
		if recs[i].Name == "Colour" || recs[i].Name == "Widget.Limit" {
			only = append(only, recs[i])
		}
	}
	doc := recordsToDoc6536(t, only)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.FieldExtractionRate.ClassTotal != 0 {
		t.Errorf("ClassTotal = %d on an enum/const-only graph, want 0 "+
			"(zero-field rate %v)", r.FieldExtractionRate.ClassTotal,
			r.FieldExtractionRate.ZeroFieldsPct)
	}
}

// ---------------------------------------------------------------------------
// #6536, round 2. The first pass exempted only Subtype "file". Three larger
// SCOPE.Component populations were still in the denominator, and every one of
// them is guaranteed-zero-field: import placeholders (emitted per imported
// module by cross/imports and eighteen language extractors — one to several
// PER FILE, so they outnumber classes in most repos), the per-file
// Subtype "module" carriers of the ML-family extractors, and Haskell's import
// placeholders which set no Subtype at all.
//
// The exemptions below are keyed on what the extractors emit, not on the
// language's rules as remembered: a VB.NET `Module` and a Kotlin `interface`
// both really do own field children, so neither subtype may be exempted
// globally. Dropping a population that CAN pass biases the rate exactly the
// way counting one that cannot does.

const vbCarriers6536 = `
Public Module Util
    Public Shared Total As Integer
    Private cache As String
    Public Sub Go()
    End Sub
End Module

Public Interface IThing
    Sub Do1()
End Interface

Public Delegate Sub Handler(x As Integer)
`

// TestInterfaceAndDelegateExemptModuleCounted_6536 pins both halves of the
// rule against real VB.NET extractor output.
func TestInterfaceAndDelegateExemptModuleCounted_6536(t *testing.T) {
	recs := extract6536(t, vbCarriers6536)

	// Premise guard: all three are class-like-kinded, so all three really are
	// candidates for the denominator until something exempts them.
	for _, name := range []string{"Util", "IThing", "Handler"} {
		kind, _ := kindOf6536(t, recs, name)
		if !isClassLikeKind(kind) {
			t.Fatalf("premise gone: %s has kind %q which is not class-like", name, kind)
		}
	}
	if _, sub := kindOf6536(t, recs, "Util"); sub != "module" {
		t.Fatalf("premise gone: VB.NET Module emits subtype %q, not \"module\"", sub)
	}

	doc := recordsToDoc6536(t, recs)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Util owns 2 fields and is the ONLY candidate: the interface and the
	// delegate cannot own fields in VB.NET, and the file carrier is exempt.
	if r.FieldExtractionRate.ClassTotal != 1 {
		t.Errorf("ClassTotal = %d, want 1 (the VB.NET Module only; interface and "+
			"delegate cannot own fields and must not be in the denominator)",
			r.FieldExtractionRate.ClassTotal)
	}
	if r.FieldExtractionRate.ZeroFieldsPct != 0.0 {
		t.Errorf("ZeroFieldsPct = %v, want 0: Util owns 2 field children",
			r.FieldExtractionRate.ZeroFieldsPct)
	}
}

// TestKotlinInterfaceStaysInDenominator_6536 is the other direction: Kotlin
// interfaces really do own Subtype "field" children, so the "interface"
// exemption must not delete them from the measurement.
func TestKotlinInterfaceStaysInDenominator_6536(t *testing.T) {
	if !isFieldExtractionCandidate("SCOPE.Component", "interface", "kotlin") {
		t.Errorf("a Kotlin interface is excluded from the field-extraction denominator, " +
			"but `interface Shape { val sides: Int }` emits Shape CONTAINS Shape.sides " +
			"with Subtype \"field\": a population that passes is being dropped")
	}
	if isFieldExtractionCandidate("SCOPE.Component", "interface", "vbnet") {
		t.Errorf("a VB.NET interface is still in the denominator; it cannot own fields")
	}
}

// TestImportPlaceholdersExemptFromFieldDenominator_6536 is the killing test for
// the biggest leak: cross/imports emits one SCOPE.Component/import per imported
// module, so a five-`using` C# file contributed five guaranteed-zero-field
// entities against its one class.
func TestImportPlaceholdersExemptFromFieldDenominator_6536(t *testing.T) {
	const cs = `using System;
using System.Collections.Generic;
using Acme.Billing;

namespace Demo { public class Widget { } }
`
	ex := &imports.Extractor{}
	recs, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "Demo.cs",
		Content:  []byte(cs),
		Language: "csharp",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Premise guard, taken from the extractor: there must really be class-like
	// import placeholders here, or this test exempts nothing.
	placeholders := 0
	for i := range recs {
		if recs[i].Subtype == "import" {
			if !isClassLikeKind(recs[i].Kind) {
				t.Fatalf("premise gone: import placeholder %q has kind %q",
					recs[i].Name, recs[i].Kind)
			}
			placeholders++
		}
	}
	if placeholders == 0 {
		t.Fatalf("premise gone: cross/imports emitted no import placeholders for %d usings", 3)
	}

	doc := recordsToDoc6536(t, recs)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.FieldExtractionRate.ClassTotal != 0 {
		t.Errorf("ClassTotal = %d on a graph of %d import placeholders and nothing else, "+
			"want 0; zero-field rate %v is guaranteed, not measured",
			r.FieldExtractionRate.ClassTotal, placeholders, r.FieldExtractionRate.ZeroFieldsPct)
	}
}

// TestNonFieldBearingLanguageCarriersExempt_6536 covers the ML-family per-file
// `module` carriers and the Haskell import placeholders against the real
// Haskell extractor.
//
// #6481 SPLIT THESE TWO POPULATIONS APART, and this test was rewritten to say
// so rather than to keep asserting the shape that no longer exists.
//
// Originally NEITHER was reachable by a subtype exclusion: "module" is a
// legitimate field-bearing subtype in VB.NET, and the Haskell import
// placeholders set NO SUBTYPE AT ALL, so nonClassSubtypes could not see them.
// Both therefore leaned on the same language-level exemption. That second
// clause is the one #6481 removed the need for — buildImportEntities now
// stamps Subtype "import", the marker resolve.isImportPlaceholderKind keys on,
// and "import" has been in nonClassSubtypes since #6536 itself. The
// placeholders are exempted one step EARLIER now, by the subtype rule, exactly
// as C#'s cross/imports placeholders already were in
// TestImportPlaceholdersExemptFromFieldDenominator_6536.
//
// The premise guard that caught this was doing its job: it detected that its
// fixture population had emptied out instead of passing on an exemption that
// no longer exempted anything. It is kept and RE-AIMED, not weakened — the
// bare-subtype count is now asserted to be ZERO, so this test pins #6481's
// haskell arm from the consumer side and fails if the stamp is ever reverted.
//
// The `module` half is UNCHANGED and still uniquely load-bearing: "module" is
// deliberately absent from nonClassSubtypes, so dropping "haskell" from
// nonFieldBearingLanguages re-admits the per-file carrier. That is asserted
// below rule-by-rule, because the obsolete half is a standing invitation to
// delete the whole entry.
//
// SCOPE NOTE: fsharp (#6369), haskell/elm/ocaml (#6481 arm A1),
// reasonml/rescript (arm A2) and idris (arm A3) stamp the marker today; the
// idris half is observed directly in
// TestIdrisImportPlaceholdersMarked_6481 below rather than only stated here.
// Two members of nonFieldBearingLanguages still need the language exemption for
// BOTH halves — erlang, whose placeholder sets no Subtype at all, and crystal,
// whose placeholder sets Subtype "module", which nonClassSubtypes does not
// exclude either — so that set must not be pruned on the strength of this test.
// Every member still needs it for its `module` carrier regardless.
func TestNonFieldBearingLanguageCarriersExempt_6536(t *testing.T) {
	const hs = `module My.Mod where
import Data.List
import qualified Data.Map as M

f :: Int -> Int
f x = x
`
	ex := &haskell.Extractor{}
	recs, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "M.hs",
		Content:  []byte(hs),
		Language: "haskell",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Premise guard: the carrier and the import placeholders must really be
	// class-like-kinded, otherwise nothing here was ever counted.
	var carrier, marked, bare int
	for i := range recs {
		if !isClassLikeKind(recs[i].Kind) || recs[i].Subtype == "file" {
			continue
		}
		switch recs[i].Subtype {
		case "module":
			carrier++
		case "import":
			marked++
		case "":
			bare++
		}
	}
	if carrier == 0 || marked == 0 {
		t.Fatalf("premise gone: haskell emitted %d module carriers and %d import "+
			"placeholders; this test would exempt nothing", carrier, marked)
	}
	// #6481, pinned from the consumer side: the placeholders must be MARKED,
	// not bare. A bare one is invisible to resolve.isImportPlaceholderKind and
	// sits in the by-name index as a declaration, flipping any colliding type
	// name ambiguous repo-wide.
	if bare != 0 {
		t.Errorf("#6481 regressed: haskell emitted %d class-like entities with an EMPTY "+
			"Subtype; buildImportEntities must stamp Subtype \"import\"", bare)
	}

	// WHICH RULE exempts WHICH population. Asserted separately because the two
	// halves no longer share an exemption, and a reader who notices the import
	// half is redundant must not conclude the whole entry is.
	//
	// (a) The per-file `module` carrier is exempted ONLY by the language rule:
	// "module" is deliberately absent from nonClassSubtypes because a VB.NET
	// `Module` is a genuine field-bearing container.
	if nonClassSubtypes["module"] {
		t.Error("\"module\" entered nonClassSubtypes; that deletes VB.NET's genuinely " +
			"field-bearing `Public Module Util` from the denominator")
	}
	if isFieldExtractionCandidate("SCOPE.Component", "module", "haskell") {
		t.Error("the haskell per-file `module` carrier is in the field-extraction " +
			"denominator; it is a guaranteed zero-field failure")
	}
	if !isFieldExtractionCandidate("SCOPE.Component", "module", "vbnet") {
		t.Error("a VB.NET `Module` is excluded from the denominator, but it really does " +
			"own Shared and Private field children: the language exemption must stay " +
			"keyed on the language, never on the \"module\" subtype")
	}

	// (b) The import placeholder is now exempted by the SUBTYPE rule, which is
	// checked ahead of the language rule and so holds independently of it —
	// including for a language that DOES bear fields.
	if !nonClassSubtypes["import"] {
		t.Error("\"import\" left nonClassSubtypes; every import placeholder in every " +
			"language re-enters the field-extraction denominator")
	}
	for _, lang := range []string{"haskell", "vbnet", "kotlin"} {
		if isFieldExtractionCandidate("SCOPE.Component", "import", lang) {
			t.Errorf("a %s import placeholder is in the field-extraction denominator; "+
				"the subtype exemption must not depend on the language", lang)
		}
	}

	doc := recordsToDoc6536(t, recs)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.FieldExtractionRate.ClassTotal != 0 {
		t.Errorf("ClassTotal = %d on a Haskell graph, want 0: the haskell extractor "+
			"emits no Subtype \"field\" anywhere, so every entity it produces is a "+
			"guaranteed zero-field failure (zero-field rate %v)",
			r.FieldExtractionRate.ClassTotal, r.FieldExtractionRate.ZeroFieldsPct)
	}
}

// TestIdrisImportPlaceholdersMarked_6481 pins #6481 arm A3's idris stamp from
// the CONSUMER side, exactly as the haskell test above pins arm A1's.
//
// The scope note on nonFieldBearingLanguages names which of its members still
// emit bare-subtype placeholders and which are now marked. That list decides
// whether the language exemption's import half is still load-bearing for a
// given entry, and an unobserved list goes stale the moment the next arm lands
// — which is how it came to name idris after idris had already been fixed. So
// the idris half is measured here rather than asserted in prose.
//
// The `module` carrier half is UNCHANGED and still uniquely load-bearing for
// idris: "module" is deliberately absent from nonClassSubtypes, so removing
// "idris" from nonFieldBearingLanguages re-admits its per-file carrier to a
// denominator it can only fail.
func TestIdrisImportPlaceholdersMarked_6481(t *testing.T) {
	const src = `module Domain.Core

import Acme.Speaker
import Data.Vect

data Animal = Dog | Cat
`
	ex := &idris.Extractor{}
	recs, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "Core.idr",
		Content:  []byte(src),
		Language: "idris",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var carrier, marked, bare int
	for i := range recs {
		if !isClassLikeKind(recs[i].Kind) || recs[i].Subtype == "file" {
			continue
		}
		switch recs[i].Subtype {
		case "module":
			carrier++
		case "import":
			marked++
		case "":
			bare++
		}
	}
	// Premise guard: without a carrier AND a placeholder in the population,
	// everything below exempts nothing and passes trivially.
	if carrier == 0 || marked == 0 {
		t.Fatalf("premise gone: idris emitted %d module carriers and %d marked import "+
			"placeholders; this test would exempt nothing", carrier, marked)
	}
	if bare != 0 {
		t.Errorf("#6481 arm A3 regressed: idris emitted %d class-like entities with an "+
			"EMPTY Subtype; buildImportEntities must stamp Subtype \"import\"", bare)
	}

	// (a) the import placeholders are exempted by the SUBTYPE rule now, so the
	// exemption holds independently of the language.
	if isFieldExtractionCandidate("SCOPE.Component", "import", "idris") {
		t.Error("an idris import placeholder is in the field-extraction denominator")
	}
	// (b) the per-file `module` carrier is still exempted ONLY by the language
	// rule — the half that must not be pruned.
	if isFieldExtractionCandidate("SCOPE.Component", "module", "idris") {
		t.Error("the idris per-file `module` carrier is in the field-extraction " +
			"denominator; it is a guaranteed zero-field failure")
	}
}

// TestClassLikeKindTailsConstrainedFromAbove_6536 pins the tail set from the
// permissive side. No fixture in this package carried a kind outside
// SCOPE.{Class,Component,Schema,Operation,Function}, so admitting a real but
// non-container kind — SCOPE.External, the sresolver/swift external-dependency
// placeholder — passed the whole suite. Every kind below is emitted somewhere
// in the graph and none can own a field child.
func TestClassLikeKindTailsConstrainedFromAbove_6536(t *testing.T) {
	recs := extract6536(t, vbSource6536)
	doc := recordsToDoc6536(t, recs)

	base, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := base.FieldExtractionRate.ClassTotal
	if want == 0 {
		t.Fatalf("premise gone: baseline ClassTotal is 0")
	}

	for _, kind := range []string{
		"SCOPE.External",  // sresolver external-dependency placeholder
		"SCOPE.Operation", // methods, functions
		"SCOPE.Function",
		"SCOPE.Endpoint",
		// SCOPE.Datastore was here. REPLACEMENT JUSTIFICATION (#6543):
		//
		// This arm asserted isClassLikeKind("SCOPE.Datastore") == false because
		// "admitting it puts a guaranteed-zero-field population back in the
		// denominator". That reasoning was correct about the DANGER and wrong
		// about the KIND. SCOPE.Datastore is not one population: SQL tables are
		// genuine member-bearing containers (sql.go:249 emits CONTAINS
		// contained_kind=column children) and could always have passed, while
		// the jcl/cobol/erlang datastores really cannot. Excluding the kind
		// wholesale traded a false-failure population for an unmeasured one —
		// the same #6535 blind spot, just silent instead of loud.
		//
		// The precondition this arm was protecting is now DISCHARGED rather
		// than removed, and the replacement asserts strictly more:
		//   - report_datastore_column_6543_test.go
		//     TestSQLTableColumnsMeasuredNonVacuously_6543 — a SQL table with
		//     column children is in the denominator AND does not report 100%.
		//   - TestColumnlessTableStillCountsAsZeroField_6543 — a container with
		//     no members is still counted as a zero-field observation, so the
		//     widened numerator did not simply mask failures.
		//   - TestNonColumnBearingDatastoresExcluded_6543 — the exact property
		//     this arm defended, kept and narrowed: a jcl/cobol
		//     SCOPE.Datastore still does not move ClassTotal.
		//   - TestOnlyTableSubtypeIsMemberBearing_6543 — and the narrowing goes
		//     one level further than "not this kind": of the six SCOPE.Datastore
		//     subtypes the sql extractor emits, only `table` owns members, so
		//     only `table` is admitted.
		//   - TestUnknownDatastoreEmitSitesAreExcluded_6543 — an emit site
		//     nobody has enumerated stays out, which is the conservative
		//     default this arm was enforcing by excluding the kind outright.
		// Deleting any one of those five re-opens what this arm covered.
		"SCOPE.Config",
		"SCOPE.Job",
	} {
		if isClassLikeKind(kind) {
			t.Errorf("isClassLikeKind(%q) is true: %s is a real emitted kind that cannot "+
				"own field children, and admitting it puts a guaranteed-zero-field "+
				"population back in the denominator", kind, kind)
		}

		ents := append([]graph.Entity(nil), doc.Entities...)
		ents = append(ents, graph.Entity{
			ID:       "extra-" + kind,
			Name:     "Extra",
			Kind:     kind,
			Language: "vbnet",
		}.WithProperties(map[string]string{}))
		d2 := makeDoc(ents, doc.Relationships)

		r, err := Generate(context.Background(), []*graph.Document{d2}, Opts{GroupName: "g", Version: "t"})
		if err != nil {
			t.Fatalf("Generate(%s): %v", kind, err)
		}
		if got := r.FieldExtractionRate.ClassTotal; got != want {
			t.Errorf("adding one %s entity changed ClassTotal %d -> %d: it was admitted "+
				"to the field-extraction denominator", kind, want, got)
		}
	}
}
