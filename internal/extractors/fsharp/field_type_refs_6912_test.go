package fsharp

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm E — the field→declared-type edge for F#.
//
// VARIED vs HELD CONSTANT, stated up front because #6912's neighbours kept
// pinning one axis and leaving the adjacent one open:
//
//	VARIED across the fixtures below
//	  - the TYPE-EXPRESSION SHAPE: bare, postfix `option`/`list`, prefix
//	    `Map<,>`, array `[]`, tuple `*`, function `->`, qualified `A.B`,
//	    generic parameter `'T`, nested `Customer list option`
//	  - the TARGET's Kind/Subtype: record, DU, class, interface, alias,
//	    computation_builder, and the two Elmish re-kinds
//	  - the NON-target record that shares a name: import placeholder, module,
//	    SCOPE.Operation, active pattern
//	  - WHERE the target is declared: same file vs sibling file
//	  - the MEMBER's Subtype: field vs du_case
//
//	HELD CONSTANT
//	  - one repo, one language, the `.fs` extension (`.fsi` differs only in
//	    hierarchy emission, which this pass does not touch)
//	  - the edge Kind (REFERENCES) and the anchor (the field record)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func fsExtract(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []types.EntityRecord
	for _, p := range paths {
		out = append(out, extractFSharp(files[p], p)...)
	}
	return out
}

func fsPropOf(ps types.Props, k string) string {
	for _, p := range ps {
		if p.K == k {
			return p.V
		}
	}
	return ""
}

// fsFieldTypeRefEdges renders every field→declared-type edge as
// "<file>:<Field> -> <ToID>", sorted. It filters on the `ref_kind` property
// with an INDEPENDENT literal rather than on fsharpFieldTargetRefKind, so a
// mutant that respells the constant is not silently followed by the grader.
func fsFieldTypeRefEdges(t *testing.T, recs []types.EntityRecord) []string {
	t.Helper()
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || fsPropOf(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			out = append(out, recs[i].SourceFile+":"+recs[i].Name+" -> "+r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

func fsWantEdges(t *testing.T, recs []types.EntityRecord, want ...string) {
	t.Helper()
	got := fsFieldTypeRefEdges(t, recs)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("field-type edges mismatch\n got:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// ---------------------------------------------------------------------------
// 1. What Properties["member_type"] actually contains
// ---------------------------------------------------------------------------

// TestFSharpFieldTypeRefs_MemberTypeIsVerbatimSourceText is the EVIDENCE this
// whole arm rests on, and it is asserted rather than assumed because #6912's
// body was wrong about the analogous java datum in one direction and about
// swift's in the other.
//
// The claim being pinned is exact: `member_type` is the declared type as
// WRITTEN, character for character after TrimSpace — not a leaf, not a first
// pre-order identifier (swift's failure), not absent (java's alleged one). If
// parseRecordFields ever starts normalising, unwrapping or truncating, this
// test fails BEFORE the candidate scanner silently starts seeing a different
// string, which is the ordering that matters: the scanner's correctness is
// entirely a function of this property's shape.
func TestFSharpFieldTypeRefs_MemberTypeIsVerbatimSourceText(t *testing.T) {
	src := `namespace D

type Order =
    { Plain: Customer
      Opt: Customer option
      Lst: Customer list
      Deep: Customer list option
      Mp: Map<string, Customer>
      Arr: Customer[]
      Tup: int * Customer
      Fn: int -> Customer
      Qual: Domain.Customer
      Gen: 'T
      Mut: mutable Customer }
`
	got := map[string]string{}
	for _, e := range extractFSharp(src, "D.fs") {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			got[e.Name] = e.Properties["member_type"]
		}
	}
	want := map[string]string{
		"Order.Plain": "Customer",
		"Order.Opt":   "Customer option",
		"Order.Lst":   "Customer list",
		"Order.Deep":  "Customer list option",
		"Order.Mp":    "Map<string, Customer>",
		"Order.Arr":   "Customer[]",
		"Order.Tup":   "int * Customer",
		"Order.Fn":    "int -> Customer",
		"Order.Qual":  "Domain.Customer",
		"Order.Gen":   "'T",
		"Order.Mut":   "mutable Customer",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("member_type[%s] = %q, want %q", name, got[name], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("field count = %d, want %d (%v)", len(got), len(want), got)
	}
}

// TestFSharpFieldTypeRefs_CandidateScannerEnumeratesTheShapeSpace drives the
// scanner directly over an ENUMERATED shape space rather than over three
// hand-picked strings. Every row is a type expression F# can write; the
// expected output is the set of bare identifiers in it, with dotted tokens and
// generic parameters absent WHOLE.
func TestFSharpFieldTypeRefs_CandidateScannerEnumeratesTheShapeSpace(t *testing.T) {
	cases := []struct {
		typ  string
		want []string
	}{
		{"Customer", []string{"Customer"}},
		{"int", []string{"int"}},
		{"Customer option", []string{"Customer", "option"}},
		{"Customer list", []string{"Customer", "list"}},
		{"Customer list option", []string{"Customer", "list", "option"}},
		{"Map<string, Customer>", []string{"Map", "string", "Customer"}},
		{"Customer[]", []string{"Customer"}},
		{"Customer [] []", []string{"Customer"}},
		{"int * Customer", []string{"int", "Customer"}},
		{"int -> Customer", []string{"int", "Customer"}},
		{"(int -> Customer) option", []string{"int", "Customer", "option"}},
		{"float<kg>", []string{"float", "kg"}},
		{"Result<Customer, string>", []string{"Result", "Customer", "string"}},
		{"x'", []string{"x'"}},
		{"Customer' option", []string{"Customer'", "option"}},
		{"_private", []string{"_private"}},
		{"T2", []string{"T2"}},
		// Qualified — the WHOLE token is dropped, never its tail. Three
		// depths, because a one-dot case cannot tell "dropped whole" from
		// "the scanner stopped at the first dot".
		{"Domain.Customer", nil},
		{"A.B.Customer", nil},
		{"System.Collections.Generic.List<Customer>", []string{"Customer"}},
		{"Domain.Customer option", []string{"option"}},
		// Generic parameters — consumed leading-quote-first, so the bare name
		// can never re-enter the scanner.
		{"'T", nil},
		{"'TKey", nil},
		{"Map<'TKey, Customer>", []string{"Map", "Customer"}},
		{"'T -> 'U", nil},
		{"", nil},
		{"  ", nil},
		{"<>", nil},
	}
	for _, c := range cases {
		got := fsharpFieldTypeCandidates(c.typ)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("candidates(%q) = %v, want %v", c.typ, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Recall — the shapes that must produce an edge
// ---------------------------------------------------------------------------

const fsRecallSrc = `namespace Domain

type Customer =
    { Id: int
      Name: string }

type Money =
    { Amount: decimal }

type Order =
    { Buyer: Customer
      MaybeBuyer: Customer option
      History: Customer list
      Nested: Customer list option
      ByName: Map<string, Customer>
      Batch: Customer[]
      Pair: int * Customer
      Lookup: int -> Customer
      Total: Money
      Both: Map<Customer, Customer>
      Count: int
      Label: string }
`

// TestFSharpFieldTypeRefs_EveryWrapperShapeBinds is the recall row. Ten of the
// twelve fields must produce an edge, one per DISTINCT target, and the two
// primitive fields must produce none.
//
// `Both: Map<Customer, Customer>` names one target twice and grades the
// per-field ToID dedup — it matters because the audit pass does not dedup
// edges, so a duplicate would be counted twice by every metric that reads them.
func TestFSharpFieldTypeRefs_EveryWrapperShapeBinds(t *testing.T) {
	recs := fsExtract(t, map[string]string{"Domain.fs": fsRecallSrc})
	c := "scope:component:class:fsharp:Domain.fs:Customer"
	m := "scope:component:class:fsharp:Domain.fs:Money"
	fsWantEdges(t, recs,
		"Domain.fs:Order.Batch -> "+c,
		"Domain.fs:Order.Both -> "+c,
		"Domain.fs:Order.Buyer -> "+c,
		"Domain.fs:Order.ByName -> "+c,
		"Domain.fs:Order.History -> "+c,
		"Domain.fs:Order.Lookup -> "+c,
		"Domain.fs:Order.MaybeBuyer -> "+c,
		"Domain.fs:Order.Nested -> "+c,
		"Domain.fs:Order.Pair -> "+c,
		"Domain.fs:Order.Total -> "+m,
	)
}

// TestFSharpFieldTypeRefs_EdgePropertiesAreComplete pins the three properties
// and the anchor. FromID must stay EMPTY so graph assembly anchors the edge on
// the field record — an explicit FromID is Hibernate's shape, which hangs its
// edge off a parallel node, and #6912's own body was corrected on that point.
func TestFSharpFieldTypeRefs_EdgePropertiesAreComplete(t *testing.T) {
	recs := fsExtract(t, map[string]string{"Domain.fs": fsRecallSrc})
	var found int
	for i := range recs {
		if recs[i].Name != "Order.MaybeBuyer" {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			found++
			if r.FromID != "" {
				t.Errorf("FromID = %q, want empty so the edge anchors on the field record", r.FromID)
			}
			for k, want := range map[string]string{
				"field_name":  "MaybeBuyer",
				"ref_kind":    "field_target_type",
				"target_type": "Customer",
			} {
				if got := fsPropOf(r.Properties, k); got != want {
					t.Errorf("property %s = %q, want %q", k, got, want)
				}
			}
		}
		// The owner→member CONTAINS lives on the OWNER's record, so the field
		// record must carry exactly the one new edge.
		if n := len(recs[i].Relationships); n != 1 {
			t.Errorf("field record carries %d relationships, want exactly 1", n)
		}
	}
	if found != 1 {
		t.Fatalf("found %d REFERENCES edges on Order.MaybeBuyer, want 1", found)
	}
}

// TestFSharpFieldTypeRefs_RefKindSpellingIsExact near-misses the constant. The
// spelling is the cross-language discriminator: five arms hardcode the same
// literal with no shared constant, so each arm's own literal is the only thing
// enforcing agreement.
func TestFSharpFieldTypeRefs_RefKindSpellingIsExact(t *testing.T) {
	if fsharpFieldTargetRefKind != "field_target_type" {
		t.Fatalf("ref_kind = %q, want %q — arms A-D spell it this way and the "+
			"edge is only queryable across languages if all five agree",
			fsharpFieldTargetRefKind, "field_target_type")
	}
}

// TestFSharpFieldTypeRefs_EveryAdmittedTargetKindBinds enumerates the
// allow-list's admitted rows against a real source construct for each, so a
// row that is admitted but unreachable in practice shows up as a missing edge
// rather than as silent dead weight.
func TestFSharpFieldTypeRefs_EveryAdmittedTargetKindBinds(t *testing.T) {
	src := `namespace Domain

type Rec = { A: int }

type Du =
    | X
    | Y

type Alias = int

type IShipper =
    interface
        abstract Ship: unit -> unit
    end

type Klass(x: int) =
    class
        member _.X = x
    end

type OptionBuilder() =
    member _.Bind(m, f) = f m
    member _.Return(x) = x
    member _.Zero() = None

type Holder =
    { R: Rec
      D: Du
      A: Alias
      I: IShipper
      K: Klass
      B: OptionBuilder }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})
	p := "scope:component:class:fsharp:Domain.fs:"
	fsWantEdges(t, recs,
		"Domain.fs:Holder.A -> "+p+"Alias",
		"Domain.fs:Holder.B -> "+p+"OptionBuilder",
		"Domain.fs:Holder.D -> "+p+"Du",
		"Domain.fs:Holder.I -> "+p+"IShipper",
		"Domain.fs:Holder.K -> "+p+"Klass",
		"Domain.fs:Holder.R -> "+p+"Rec",
	)
}

// TestFSharpFieldTypeRefs_ElmishModelAndMsgAreTargets grades the two rows arm
// C's `Kind != "SCOPE.Component"` guard would have dropped. applyElmishFeliz
// re-kinds the `Model` record to SCOPE.Model and the `Msg` DU to SCOPE.Event,
// and `State: Model` is the commonest declared-type relation in an Elmish app.
//
// The fixture carries a POSITIVE CONTROL — an ordinary record `Config` in the
// same file — so a failure distinguishes "the re-kinded targets are dropped"
// from "this file produces no edges at all", which is what makes the row a
// grader rather than a recall assertion.
func TestFSharpFieldTypeRefs_ElmishModelAndMsgAreTargets(t *testing.T) {
	src := `module App

open Elmish

type Config = { Debug: bool }

type Model = { Count: int }

type Msg =
    | Inc
    | Dec

type Env =
    { State: Model
      Last: Msg
      Cfg: Config }
`
	recs := fsExtract(t, map[string]string{"App.fs": src})
	// Guard the premise: if the re-kind did not happen this test would be
	// grading the plain Component path and prove nothing.
	var sawModel, sawMsg bool
	for _, e := range recs {
		if e.Name == "Model" && e.Kind == "SCOPE.Model" {
			sawModel = true
		}
		if e.Name == "Msg" && e.Kind == "SCOPE.Event" {
			sawMsg = true
		}
	}
	if !sawModel || !sawMsg {
		t.Fatalf("applyElmishFeliz did not re-kind Model/Msg (model=%v msg=%v) — "+
			"this fixture is not exercising the SCOPE.Model/SCOPE.Event allow-list rows",
			sawModel, sawMsg)
	}
	p := "scope:component:class:fsharp:App.fs:"
	fsWantEdges(t, recs,
		"App.fs:Env.Cfg -> "+p+"Config",
		"App.fs:Env.Last -> "+p+"Msg",
		"App.fs:Env.State -> "+p+"Model",
	)
}

// ---------------------------------------------------------------------------
// 3. Forbidden rows — the permissive direction
// ---------------------------------------------------------------------------

// TestFSharpFieldTypeRefs_PrimitiveFieldsProduceNoEdge enumerates F#'s common
// primitive type abbreviations against an INDEPENDENT literal list. There is
// no primitive blocklist in the implementation — these are refused by the
// in-file declaration gate — so this row grades that gate in the direction a
// recall assertion cannot see.
func TestFSharpFieldTypeRefs_PrimitiveFieldsProduceNoEdge(t *testing.T) {
	prims := []string{
		"int", "int64", "float", "float32", "decimal", "string", "bool",
		"char", "byte", "unit", "obj", "bigint", "single", "sbyte", "uint32",
	}
	var b strings.Builder
	b.WriteString("namespace Domain\n\ntype Prims =\n    { ")
	for i, p := range prims {
		if i > 0 {
			b.WriteString("\n      ")
		}
		b.WriteString("F" + strings.ToUpper(p[:1]) + p[1:] + ": " + p)
	}
	b.WriteString(" }\n")
	recs := fsExtract(t, map[string]string{"Domain.fs": b.String()})
	fsWantEdges(t, recs)
	// The fixture must actually have produced the fields, or the emptiness
	// above is vacuous.
	n := 0
	for _, e := range recs {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			n++
		}
	}
	if n != len(prims) {
		t.Fatalf("fixture produced %d field entities, want %d — the empty edge "+
			"set above proves nothing if the fields were never emitted", n, len(prims))
	}
}

// TestFSharpFieldTypeRefs_NoPrimitiveBlocklistIsNeeded_AndTheReasonIsNotTheObviousOne
// is the mirror of the row above, and the reason it is written as one test with
// two halves is that the obvious justification for having no blocklist turned
// out to be WRONG for F# and had to be replaced by a measured one.
//
// The obvious argument — arm D's, for Go — is that a primitive is a legally
// shadowable identifier, so a blocklist would refuse the one edge that SHOULD
// exist when a file declares its own `type string`. HALF of that does not hold
// here: typeRE (extractor.go:73) requires `[A-Z]` as the first character, so a
// LOWERCASE F# type declaration emits no entity at all. `type string = {...}`
// is legal F# and this extractor is blind to it, which means no lowercase
// primitive abbreviation — int, string, float, bool, char, unit, obj — can ever
// be an in-file target however the gate is written. Recorded as a pre-existing
// limitation of typeRE, NOT introduced or relied on here.
//
// What survives is the UPPERCASE half, and it is live: `String`, `Object`,
// `Decimal` and friends are real .NET type names an F# module may legally
// declare its own version of, and there the blocklist a naive port would have
// written (arm B's protoScalars, transliterated) WOULD refuse a correct edge.
// So the design stands — refuse by the declaration gate, never by a name list —
// but on the second argument rather than the first.
func TestFSharpFieldTypeRefs_NoPrimitiveBlocklistIsNeeded_AndTheReasonIsNotTheObviousOne(t *testing.T) {
	src := `namespace Domain

type string = { LowerCaseIsInvisibleToTypeRE: char }

type String = { Chars: char }

type Holder =
    { Lower: string
      Upper: String }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})

	// Half 1 — the pre-existing typeRE limitation, pinned so a future widening
	// of typeRE shows up here rather than silently changing this arm's output.
	for _, e := range recs {
		if e.Name == "string" && e.Kind == "SCOPE.Component" {
			t.Fatal("typeRE now emits lowercase type declarations — this arm's " +
				"lowercase-primitive reasoning is stale; re-derive it and check " +
				"whether a blocklist is now needed for `int`/`string`/`float`")
		}
	}

	// Half 2 — the live one. `String` is an uppercase .NET type name a
	// blocklist would plausibly have carried, and the edge to the file's own
	// declaration of it is correct.
	fsWantEdges(t, recs,
		"Domain.fs:Holder.Upper -> scope:component:class:fsharp:Domain.fs:String",
	)
}

// TestFSharpFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName is the
// correctness row arm D found the hard way in Go: reducing `other.Order` to
// `Order` binds to a same-file `Order`, a WRONG binding that never reaches
// `bug-extractor` because it binds.
//
// The fixture is built so the two possible behaviours give DIFFERENT output —
// `Domain.Customer` is written in a file that also declares its own
// `Customer`, with a positive control (`Direct: Customer`) proving the target
// is otherwise reachable. A scanner that took the trailing segment would emit
// two edges here, not one.
func TestFSharpFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName(t *testing.T) {
	src := `namespace Local

type Customer = { Id: int }

type Order =
    { Direct: Customer
      Foreign: Domain.Customer
      Deep: A.B.Customer
      Wrapped: Domain.Customer option }
`
	recs := fsExtract(t, map[string]string{"Local.fs": src})
	fsWantEdges(t, recs,
		"Local.fs:Order.Direct -> scope:component:class:fsharp:Local.fs:Customer",
	)
}

// TestFSharpFieldTypeRefs_GenericParameterIsNotStrippedToASameFileType is the
// same failure mode one token over. Arm D SHIPPED this as a known over-fire
// because Go's `T` is indistinguishable from a type name; F#'s leading
// apostrophe makes it distinguishable, so this arm closes it.
//
// The distinguishing input is a file that declares `type T` — without it, `'T`
// would produce no edge for the wrong reason (nothing named `T` exists) and
// the test would pass against a scanner that merely skipped the apostrophe.
func TestFSharpFieldTypeRefs_GenericParameterIsNotStrippedToASameFileType(t *testing.T) {
	src := `namespace Domain

type T = { Marker: int }

type Holder<'T> =
    { Item: 'T
      Real: T
      Keyed: Map<'T, int> }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})
	fsWantEdges(t, recs,
		"Domain.fs:Holder.Real -> scope:component:class:fsharp:Domain.fs:T",
	)
}

// TestFSharpFieldTypeRefs_ImportPlaceholderIsNeverATarget is THE allow-list's
// kill, and F# is more exposed to it than Go: buildImportEntities names the
// placeholder importDisplayName(mod) — the LAST SEGMENT of the module path —
// so `open Acme.Customer` puts a SCOPE.Component literally named `Customer` in
// this file.
//
// The file declares NO type of that name, which is what makes the row grade
// the ALLOW-LIST specifically: with one record of that name there is one graph
// node, so the ambiguity rule stays silent and cannot mask the result. Without
// the allow-list the field binds TO THE IMPORT PLACEHOLDER — resolve/refs.go
// filters placeholders out of indexByName (:1611) but not out of byLocation
// (:1271-1317), and byLocation is the fallback this address lands in — so the
// failure is a wrong binding, not a dangle.
//
// The positive control (`Ok: Money`) proves the file emits edges at all.
func TestFSharpFieldTypeRefs_ImportPlaceholderIsNeverATarget(t *testing.T) {
	src := `namespace App

open Acme.Customer

type Money = { Amount: decimal }

type Order =
    { Buyer: Customer
      Ok: Money }
`
	recs := fsExtract(t, map[string]string{"App.fs": src})
	// Premise guard: the placeholder must really be present and really be
	// named `Customer`, or the row grades nothing.
	var placeholder bool
	for _, e := range recs {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" && e.Name == "Customer" {
			placeholder = true
		}
	}
	if !placeholder {
		t.Fatal("no SCOPE.Component/import named Customer in this file — " +
			"importDisplayName no longer truncates to the last segment, so this " +
			"test is not exercising the hazard it was written for")
	}
	fsWantEdges(t, recs,
		"App.fs:Order.Ok -> scope:component:class:fsharp:App.fs:Money",
	)
}

// TestFSharpFieldTypeRefs_OperationSharingATypeNameGetsNoEdge grades rule 2 in
// its no-rule direction: `let Customer = ...` emits a SCOPE.Operation named
// `Customer` beside the type of the same name. Two KINDS, hence two graph
// nodes, hence resolve's ambigLocation returns statusAmbiguous and any edge
// would DANGLE.
//
// The POSITIVE CONTROL is in the SAME fixture — `Ok: Money`, an identical
// record field whose target has no rival — so the assertion cannot pass by the
// pass being dead. Without the pair, "the shadowed name is respected" is
// indistinguishable from "a record target never works".
func TestFSharpFieldTypeRefs_TypeShadowedByAnotherKindGetsNoEdge(t *testing.T) {
	src := `namespace Domain

type Customer = { Id: int }

type Money = { Amount: decimal }

let Customer = 42

type Order =
    { Buyer: Customer
      Ok: Money }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})
	// Premise guard: two DISTINCT kinds must really share the name.
	kinds := map[string]bool{}
	for _, e := range recs {
		if e.Name == "Customer" && e.SourceFile == "Domain.fs" {
			kinds[e.Kind] = true
		}
	}
	if len(kinds) < 2 {
		t.Fatalf("Customer denotes %d kind(s) %v in this file, want 2 — the "+
			"ambiguity this row grades is not present", len(kinds), kinds)
	}
	fsWantEdges(t, recs,
		"Domain.fs:Order.Ok -> scope:component:class:fsharp:Domain.fs:Money",
	)
}

// TestFSharpFieldTypeRefs_CompanionModuleSharingATypeNameBinds is rule 2's
// OTHER direction, and it is the mutant that separates counting KINDS from
// counting RECORDS.
//
// `module Customer` heading a `Customer.fs` that also declares `type Customer`
// is the ordinary F# file shape, not a contrivance. Both records are
// SCOPE.Component with the same Name in the same file, so graph.EntityID
// (which excludes Subtype) hashes them to ONE node and the resolver binds the
// name perfectly well. A rule that counted RECORDS would refuse this edge; the
// resolver's own rule, mirrored here, keeps it. #7038 is filed against arm C
// for the same over-strictness.
//
// The fixture uses the TOP-LEVEL module form deliberately. The nested
// companion-module form (`module Customer =`, indented under a namespace) does
// NOT produce a second record at all: moduleRE (extractor.go:51) anchors on
// `\s*$` and so never matches a module header ending in `=`. That is a
// pre-existing gap in this extractor, noted here because it is the reason the
// more famous spelling of this idiom could not be used — not because this arm
// depends on it either way.
func TestFSharpFieldTypeRefs_CompanionModuleSharingATypeNameBinds(t *testing.T) {
	src := `module Customer

type Customer = { Id: int }

type Order =
    { Buyer: Customer }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})
	// Premise guard: there really must be TWO records and ONE kind.
	recCount, kinds := 0, map[string]bool{}
	for _, e := range recs {
		if e.Name == "Customer" && e.SourceFile == "Domain.fs" {
			recCount++
			kinds[e.Kind] = true
		}
	}
	if recCount < 2 || len(kinds) != 1 {
		t.Fatalf("Customer has %d records across %d kind(s) %v, want >=2 records "+
			"in exactly 1 kind — a record-count rule and a kind-count rule are "+
			"indistinguishable on this fixture otherwise", recCount, len(kinds), kinds)
	}
	fsWantEdges(t, recs,
		"Domain.fs:Order.Buyer -> scope:component:class:fsharp:Domain.fs:Customer",
	)
}

// TestFSharpFieldTypeRefs_DUCasePayloadGetsNoEdge grades the RECORDS-ONLY
// decision. `| Placed of Customer` puts a real, admitted, same-file,
// unambiguous type name in the du_case record's `member_type` — so with the
// Subtype guard removed this fixture WOULD emit, and the row fails. It is a
// grader, not an assertion that nothing would have bound anyway.
//
// The positive control is the record field `Latest: Customer` in the same
// file, naming the same target.
func TestFSharpFieldTypeRefs_DUCasePayloadGetsNoEdge(t *testing.T) {
	src := `namespace Domain

type Customer = { Id: int }

type Status =
    | Placed of Customer
    | Named of who: Customer * qty: int
    | Empty

type Order =
    { Latest: Customer }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})
	// Premise guard: the du_case records must really carry the type.
	var payload string
	for _, e := range recs {
		if e.Name == "Status.Placed" {
			payload = e.Properties["member_type"]
		}
	}
	if payload != "Customer" {
		t.Fatalf("Status.Placed member_type = %q, want %q — the Subtype guard "+
			"is ungraded if the du_case carries no admissible candidate", payload, "Customer")
	}
	fsWantEdges(t, recs,
		"Domain.fs:Order.Latest -> scope:component:class:fsharp:Domain.fs:Customer",
	)
}

// TestFSharpFieldTypeRefs_SelfReferenceGetsNoEdge — `type Node = { Next: Node
// option }`. The CONTAINS edge already relates the field to its owner and the
// self-edge asserts nothing new. Arm D declines it for the same reason.
func TestFSharpFieldTypeRefs_SelfReferenceGetsNoEdge(t *testing.T) {
	src := `namespace Domain

type Leaf = { V: int }

type Node =
    { Next: Node option
      Self: Node
      Down: Leaf }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})
	fsWantEdges(t, recs,
		"Domain.fs:Node.Down -> scope:component:class:fsharp:Domain.fs:Leaf",
	)
}

// TestFSharpFieldTypeRefs_CrossFileTypeGetsNoEdge — the same-file promise.
// `Customer` is declared in Customer.fs and used in Order.fs; no edge. The
// positive control (`Money`, declared in Order.fs) proves the pass is live in
// that file, so the emptiness is a refusal rather than a no-op.
func TestFSharpFieldTypeRefs_CrossFileTypeGetsNoEdge(t *testing.T) {
	recs := fsExtract(t, map[string]string{
		"Customer.fs": "namespace Domain\n\ntype Customer = { Id: int }\n",
		"Order.fs": `namespace Domain

type Money = { Amount: decimal }

type Order =
    { Buyer: Customer
      Total: Money }
`,
	})
	fsWantEdges(t, recs,
		"Order.fs:Order.Total -> scope:component:class:fsharp:Order.fs:Money",
	)
}

// TestFSharpFieldTypeRefs_ActivePatternCasesAreNotFields — the third
// SCOPE.Schema subtype in this package. An active-pattern case carries no
// `member_type` at all, so this row also grades the empty-property guard.
func TestFSharpFieldTypeRefs_ActivePatternCasesAreNotFields(t *testing.T) {
	src := `namespace Domain

type Customer = { Id: int }

let (|Even|Odd|) n = if n % 2 = 0 then Even else Odd

type Order = { Buyer: Customer }
`
	recs := fsExtract(t, map[string]string{"Domain.fs": src})
	fsWantEdges(t, recs,
		"Domain.fs:Order.Buyer -> scope:component:class:fsharp:Domain.fs:Customer",
	)
}

// ---------------------------------------------------------------------------
// 4. The binding gate
// ---------------------------------------------------------------------------

// TestFSharpFieldTypeRefs_ResolverBindsEveryEdge is the gate this whole issue
// class turns on: #6906 exists because a comment described a binding nobody
// built, and #6912's standing instruction is that a NON-BINDING edge is worse
// than no edge.
//
// It matters more here than in any previous arm, because the corpus at
// /Users/jorgecajas/Projects/archigraph-corpora contains ZERO .fs/.fsi/.fsx
// files — arms A-D could each cite a corpus bind-rate and this one cannot. So
// every emitted edge is driven through the PRODUCTION resolver
// (BuildIndex -> ReferencesEmbedded, exactly as graph assembly does) and must
// come back rewritten to a real entity ID. Zero dangling is the assertion, not
// a ratio.
func TestFSharpFieldTypeRefs_ResolverBindsEveryEdge(t *testing.T) {
	recs := fsExtract(t, map[string]string{
		"Domain.fs": fsRecallSrc,
		// A rival file declaring the SAME names, so the address dialect is
		// under test rather than merely being unique by accident.
		"Rival.fs": `namespace Other

type Customer = { Other: int }

type Money = { Other: int }
`,
	})
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6912fs", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	idx := resolve.BuildIndex(recs)

	// Positive control on the dialect: the BARE name is AMBIGUOUS in this
	// two-file set, which is exactly why a bare-name ToID is not what we emit
	// (#6986 measured that form at 91.6% dangling).
	if _, st := idx.LookupStatusHint("Customer", "REFERENCES"); st == 1 {
		t.Fatal("the bare name Customer binds unambiguously in this fixture, so " +
			"the ambiguity the structural address controls for is absent and the " +
			"dialect is ungraded")
	}

	idToName := map[string]string{}
	for i := range recs {
		idToName[recs[i].ID] = recs[i].SourceFile + ":" + recs[i].Kind + ":" + recs[i].Name
	}

	before := len(fsFieldTypeRefEdges(t, recs))
	if before == 0 {
		t.Fatal("no field-type edges to drive through the resolver")
	}

	resolve.ReferencesEmbedded(recs, idx)

	var bound []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || fsPropOf(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			target, ok := idToName[r.ToID]
			if !ok {
				t.Errorf("%s -[REFERENCES]-> %q did NOT bind to an entity ID (dangling stub)",
					recs[i].Name, r.ToID)
				continue
			}
			bound = append(bound, recs[i].Name+" => "+target)
		}
	}
	sort.Strings(bound)
	want := []string{
		"Order.Batch => Domain.fs:SCOPE.Component:Customer",
		"Order.Both => Domain.fs:SCOPE.Component:Customer",
		"Order.Buyer => Domain.fs:SCOPE.Component:Customer",
		"Order.ByName => Domain.fs:SCOPE.Component:Customer",
		"Order.History => Domain.fs:SCOPE.Component:Customer",
		"Order.Lookup => Domain.fs:SCOPE.Component:Customer",
		"Order.MaybeBuyer => Domain.fs:SCOPE.Component:Customer",
		"Order.Nested => Domain.fs:SCOPE.Component:Customer",
		"Order.Pair => Domain.fs:SCOPE.Component:Customer",
		"Order.Total => Domain.fs:SCOPE.Component:Money",
	}
	if strings.Join(bound, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resolved edges mismatch\n got:\n  %s\nwant:\n  %s",
			strings.Join(bound, "\n  "), strings.Join(want, "\n  "))
	}
	if len(bound) != before {
		t.Fatalf("%d edges emitted but %d resolved to an entity", before, len(bound))
	}
}
