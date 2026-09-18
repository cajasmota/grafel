package fsharp_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7176 — extractIndentBody had a DEAD BAND at exactly baseIndentLen+1: such a
// line was neither appended to the body nor treated as a terminator, so the
// scan silently skipped it and kept going.
//
// F# 4.1 Language Specification §15.1.4 "Offside Lines" settles which of the
// two it must be. Its own worked example indents by exactly ONE column:
//
//	let x = 1
//	 let y = 2 <-- unmatched 'let'
//	let z = 3   <-- warning FS0058: possible incorrect indentation
//
// The one-column-deeper `let y` is NOT a sibling of `let x`; it is absorbed as
// a continuation, and it is the *return* to column 0 that goes offside. §15.1.8
// states the closing rule from the other side: "When a token occurs ON OR
// BEFORE the offside limit for the current offside stack ... enclosing contexts
// are closed." On-or-before, i.e. `indent <= base`, closes; strictly greater
// continues. So baseIndentLen+1 must be APPENDED, and the terminator test stays
// at `indent <= baseIndentLen`.
//
// DERIVED-NOT-EXECUTED: there is no F# toolchain on this machine (dotnet, fsc,
// fsharpc, fsi, mono all absent), so the rule above is read off the spec, not
// compiled. It would be falsified by an F# program in which a construct at
// column base+1 following a declaration at column `base` parses as a SIBLING of
// that declaration rather than as part of its body.

// fsCallTargets returns the CALLS edge targets carried by the named entity.
func fsCallTargets(t *testing.T, ents []types.EntityRecord, name, kind string) []string {
	t.Helper()
	rec := fsFind(ents, name, kind)
	if rec == nil {
		t.Fatalf("no %s entity named %q", kind, name)
	}
	var out []string
	for _, r := range rec.Relationships {
		if r.Kind == "CALLS" {
			out = append(out, r.ToID)
		}
	}
	return out
}

func fsHasTarget(targets []string, want string) bool {
	for _, g := range targets {
		if g == want {
			return true
		}
	}
	return false
}

func fsEndLine(t *testing.T, ents []types.EntityRecord, name, kind string) int {
	t.Helper()
	rec := fsFind(ents, name, kind)
	if rec == nil {
		t.Fatalf("no %s entity named %q", kind, name)
	}
	return rec.EndLine
}

// ---------------------------------------------------------------------------
// Call site 1 of 3 — the LET pass (extractor.go:617 on b31fe2c67). The body
// feeds EndLine and collectCalls.
// ---------------------------------------------------------------------------

func TestFSharp_IndentBand7176_LetPass(t *testing.T) {
	// Axis VARIED: the indent of the first body line (1, 2, and 0 columns).
	// Axis HELD CONSTANT across all three: the declaration column (0), the
	// declaration keyword (`let`), the second body line's indent (4), the
	// callee names, and the file path.
	const band = "module M\n" + // 1
		"\n" + // 2
		"let alpha () =\n" + // 3   base indent 0
		" stepOne ()\n" + // 4   base+1  <-- the dead band
		"    stepTwo ()\n" + // 5
		"\n" + // 6
		"let beta () = ()\n" // 7

	ents := runFSharp(t, band, "src/Let.fs")
	if got := fsEndLine(t, ents, "alpha", "SCOPE.Operation"); got != 6 {
		t.Errorf("band+1: alpha EndLine = %d, want 6 (the base+1 line is body, so the span reaches the blank line before the sibling)", got)
	}
	calls := fsCallTargets(t, ents, "alpha", "SCOPE.Operation")
	if !fsHasTarget(calls, "stepOne") {
		t.Errorf("band+1: alpha CALLS = %v, want it to contain stepOne (the base+1 line was swallowed)", calls)
	}
	if !fsHasTarget(calls, "stepTwo") {
		t.Errorf("band+1: alpha CALLS = %v, want it to contain stepTwo", calls)
	}

	// Control, the deeper neighbour: base+2 must behave exactly as base+1 now does.
	ctrlDeep := strings.Replace(band, " stepOne ()", "  stepOne ()", 1)
	ents = runFSharp(t, ctrlDeep, "src/Let.fs")
	if got := fsEndLine(t, ents, "alpha", "SCOPE.Operation"); got != 6 {
		t.Errorf("ctrl base+2: alpha EndLine = %d, want 6", got)
	}
	if calls := fsCallTargets(t, ents, "alpha", "SCOPE.Operation"); !fsHasTarget(calls, "stepOne") {
		t.Errorf("ctrl base+2: alpha CALLS = %v, want it to contain stepOne", calls)
	}

	// Control, the shallower neighbour: a sibling AT base must still TERMINATE.
	const ctrlFlat = "module M\n" + // 1
		"\n" + // 2
		"let alpha () =\n" + // 3
		"    stepTwo ()\n" + // 4
		"\n" + // 5
		"let beta () =\n" + // 6   base+0 -> terminator
		"    siblingOnly ()\n" // 7

	ents = runFSharp(t, ctrlFlat, "src/Let.fs")
	if got := fsEndLine(t, ents, "alpha", "SCOPE.Operation"); got != 5 {
		t.Errorf("ctrl base+0: alpha EndLine = %d, want 5 (alpha must stop before the sibling at column 0)", got)
	}
	// FORBIDDEN: the sibling's body must not be attributed to alpha.
	if calls := fsCallTargets(t, ents, "alpha", "SCOPE.Operation"); fsHasTarget(calls, "siblingOnly") {
		t.Errorf("ctrl base+0: alpha CALLS = %v, must NOT contain siblingOnly (that call belongs to beta)", calls)
	}
	if calls := fsCallTargets(t, ents, "beta", "SCOPE.Operation"); !fsHasTarget(calls, "siblingOnly") {
		t.Errorf("ctrl base+0: beta CALLS = %v, want it to contain siblingOnly", calls)
	}
}

// ---------------------------------------------------------------------------
// Call site 2 of 3 — the MEMBER pass (extractor.go:665 on b31fe2c67). Same
// shape as the let pass, but the base indent is non-zero, which is the case the
// let fixture cannot reach.
// ---------------------------------------------------------------------------

func TestFSharp_IndentBand7176_MemberPass(t *testing.T) {
	// Axis VARIED: the first body line's indent (5, 6, 4 = base+1, base+2, base+0).
	// Axis HELD CONSTANT: the member's own column (4, i.e. a NON-ZERO base —
	// deliberately different from the let fixture's 0), the enclosing type, the
	// second body line's indent (8), and the callee names.
	const band = "module M\n" + // 1
		"\n" + // 2
		"type Holder() =\n" + // 3
		"    member _.Alpha () =\n" + // 4   base indent 4
		"     memberStep ()\n" + // 5   base+1 <-- the dead band
		"        deeperStep ()\n" + // 6
		"    member _.Beta () = ()\n" // 7   base+0 -> terminator

	ents := runFSharp(t, band, "src/Member.fs")
	if got := fsEndLine(t, ents, "Alpha", "SCOPE.Operation"); got != 6 {
		t.Errorf("band+1: Alpha EndLine = %d, want 6", got)
	}
	calls := fsCallTargets(t, ents, "Alpha", "SCOPE.Operation")
	if !fsHasTarget(calls, "memberStep") {
		t.Errorf("band+1: Alpha CALLS = %v, want it to contain memberStep (the base+1 line was swallowed)", calls)
	}
	if !fsHasTarget(calls, "deeperStep") {
		t.Errorf("band+1: Alpha CALLS = %v, want it to contain deeperStep", calls)
	}
	// FORBIDDEN, base+0: the sibling member at column 4 terminates Alpha, so
	// Alpha's span must not reach line 7.
	if got := fsEndLine(t, ents, "Beta", "SCOPE.Operation"); got != 8 {
		t.Errorf("band+1: Beta EndLine = %d, want 8", got)
	}

	// Control, the deeper neighbour.
	ctrlDeep := strings.Replace(band, "     memberStep ()", "      memberStep ()", 1)
	ents = runFSharp(t, ctrlDeep, "src/Member.fs")
	if got := fsEndLine(t, ents, "Alpha", "SCOPE.Operation"); got != 6 {
		t.Errorf("ctrl base+2: Alpha EndLine = %d, want 6", got)
	}
	if calls := fsCallTargets(t, ents, "Alpha", "SCOPE.Operation"); !fsHasTarget(calls, "memberStep") {
		t.Errorf("ctrl base+2: Alpha CALLS = %v, want it to contain memberStep", calls)
	}

	// Control, the shallower neighbour: a body line pulled back to the member's
	// own column terminates the member, and its call belongs to nobody.
	ctrlFlat := strings.Replace(band, "     memberStep ()", "    memberStep ()", 1)
	ents = runFSharp(t, ctrlFlat, "src/Member.fs")
	if got := fsEndLine(t, ents, "Alpha", "SCOPE.Operation"); got != 4 {
		t.Errorf("ctrl base+0: Alpha EndLine = %d, want 4 (a line at the member's own column terminates the body)", got)
	}
	if calls := fsCallTargets(t, ents, "Alpha", "SCOPE.Operation"); fsHasTarget(calls, "deeperStep") {
		t.Errorf("ctrl base+0: Alpha CALLS = %v, must NOT contain deeperStep — the body ended at column 4", calls)
	}
}

// ---------------------------------------------------------------------------
// Call site 3 of 3 — the TYPE pass (extractor.go:720 on b31fe2c67). The body
// feeds EndLine, classifyTypeSubtype, the DU-case sub-entities and their
// CONTAINS edges, and collectHierarchyEdges. Two fixtures, because the subtype
// and the hierarchy artefacts cannot be produced by one type declaration.
// ---------------------------------------------------------------------------

func TestFSharp_IndentBand7176_TypePass_SubtypeAndContains(t *testing.T) {
	// Axis VARIED: the DU cases' indent (1, 2, 0).
	// Axis HELD CONSTANT: the type's column (0), the case names, the trailing
	// sibling type, and the file path.
	const band = "module M\n" + // 1
		"\n" + // 2
		"type Shape =\n" + // 3   base indent 0
		" | Circle\n" + // 4   base+1 <-- the dead band
		" | Square\n" + // 5   base+1
		"\n" + // 6
		"type Other() = class end\n" // 7   base+0 -> terminator

	ents := runFSharp(t, band, "src/Type.fs")
	rec := fsFind(ents, "Shape", "SCOPE.Component")
	if rec == nil {
		t.Fatal("band+1: no SCOPE.Component named Shape")
	}
	if rec.Subtype != "discriminated_union" {
		t.Errorf("band+1: Shape Subtype = %q, want %q (the swallowed `|` lines made it classify as a plain type)", rec.Subtype, "discriminated_union")
	}
	if rec.EndLine != 6 {
		t.Errorf("band+1: Shape EndLine = %d, want 6", rec.EndLine)
	}
	contains := fsToIDs(fsRelsOfKind(t, ents, "Shape", "CONTAINS"))
	for _, want := range []string{
		"scope:schema:field:fsharp:src/Type.fs:Shape.Circle",
		"scope:schema:field:fsharp:src/Type.fs:Shape.Square",
	} {
		if !fsHasTarget(contains, want) {
			t.Errorf("band+1: Shape CONTAINS = %v, want it to contain %s", contains, want)
		}
	}
	if fsFind(ents, "Shape.Circle", "SCOPE.Schema") == nil {
		t.Error("band+1: no SCOPE.Schema sub-entity Shape.Circle")
	}
	// FORBIDDEN, base+0: the sibling type must stay its own entity and must not
	// be reparented into Shape's body.
	if other := fsFind(ents, "Other", "SCOPE.Component"); other == nil {
		t.Error("band+1: sibling type Other at column 0 must still be its own entity")
	} else if other.StartLine != 7 {
		t.Errorf("band+1: Other StartLine = %d, want 7", other.StartLine)
	}

	// Control, the deeper neighbour: base+2 must classify identically.
	ctrlDeep := strings.ReplaceAll(band, " | ", "  | ")
	ents = runFSharp(t, ctrlDeep, "src/Type.fs")
	rec = fsFind(ents, "Shape", "SCOPE.Component")
	if rec == nil {
		t.Fatal("ctrl base+2: no SCOPE.Component named Shape")
	}
	if rec.Subtype != "discriminated_union" || rec.EndLine != 6 {
		t.Errorf("ctrl base+2: Shape = (%q, EndLine %d), want (discriminated_union, 6)", rec.Subtype, rec.EndLine)
	}

	// Control, the shallower neighbour: cases pulled back to column 0 terminate
	// the body, so Shape is NOT a discriminated union by body inspection.
	ctrlFlat := strings.ReplaceAll(band, " | ", "| ")
	ents = runFSharp(t, ctrlFlat, "src/Type.fs")
	rec = fsFind(ents, "Shape", "SCOPE.Component")
	if rec == nil {
		t.Fatal("ctrl base+0: no SCOPE.Component named Shape")
	}
	if rec.EndLine != 3 {
		t.Errorf("ctrl base+0: Shape EndLine = %d, want 3 (a line at column 0 terminates the body)", rec.EndLine)
	}
	if fsFind(ents, "Shape.Circle", "SCOPE.Schema") != nil {
		t.Error("ctrl base+0: Shape.Circle must NOT be a sub-entity — the body ended at column 0")
	}
}

func TestFSharp_IndentBand7176_TypePass_Hierarchy(t *testing.T) {
	// Axis VARIED: the `inherit` clause's indent (1, 2, 0).
	// Axis HELD CONSTANT: the derived type's column (0), the base type's name,
	// and the preceding declaration.
	const band = "module M\n" + // 1
		"\n" + // 2
		"type Base() =\n" + // 3
		"    member _.Hello () = ()\n" + // 4
		"\n" + // 5
		"type Derived() =\n" + // 6   base indent 0
		" inherit Base()\n" // 7   base+1 <-- the dead band

	ents := runFSharp(t, band, "src/Hier.fs")
	if got := fsToIDs(fsRelsOfKind(t, ents, "Derived", "EXTENDS")); len(got) != 1 || got[0] != "Base" {
		t.Errorf("band+1: Derived EXTENDS = %v, want [Base] (the base+1 inherit clause was swallowed)", got)
	}
	if got := fsEndLine(t, ents, "Derived", "SCOPE.Component"); got != 8 {
		t.Errorf("band+1: Derived EndLine = %d, want 8", got)
	}

	// Control, the deeper neighbour.
	ctrlDeep := strings.Replace(band, " inherit Base()", "  inherit Base()", 1)
	ents = runFSharp(t, ctrlDeep, "src/Hier.fs")
	if got := fsToIDs(fsRelsOfKind(t, ents, "Derived", "EXTENDS")); len(got) != 1 || got[0] != "Base" {
		t.Errorf("ctrl base+2: Derived EXTENDS = %v, want [Base]", got)
	}

	// Control, the shallower neighbour: an `inherit` at column 0 is outside the
	// type's body and must NOT produce an edge.
	ctrlFlat := strings.Replace(band, " inherit Base()", "inherit Base()", 1)
	ents = runFSharp(t, ctrlFlat, "src/Hier.fs")
	if got := fsToIDs(fsRelsOfKind(t, ents, "Derived", "EXTENDS")); len(got) != 0 {
		t.Errorf("ctrl base+0: Derived EXTENDS = %v, want none — a clause at column 0 is offside of the type body", got)
	}
}

// ---------------------------------------------------------------------------
// The WIDENING direction. Appending the base+1 line makes every affected body
// LARGER, so collectCalls and the hierarchy scanner see more text. These rows
// are forbidden-only: nothing may be minted from a comment or a string literal
// that the fix has newly pulled into a body.
// ---------------------------------------------------------------------------

func TestFSharp_IndentBand7176_WideningMintsNothingFromCommentsOrStrings(t *testing.T) {
	// Axis VARIED: the KIND of text on the base+1 line (comment / string /
	// genuine code). Axis HELD CONSTANT: the indent (always base+1), the
	// declaration column, and the token spelled inside each carrier.
	const letSrc = "module M\n" + // 1
		"\n" + // 2
		"let alpha () =\n" + // 3
		" // ghostOne ()\n" + // 4   base+1 comment
		" printfn \"ghostStr ()\"\n" + // 5   base+1 string literal
		" realTwo ()\n" + // 6   base+1 genuine call
		"\n" + // 7
		"let beta () = ()\n" // 8

	ents := runFSharp(t, letSrc, "src/Wide.fs")
	calls := fsCallTargets(t, ents, "alpha", "SCOPE.Operation")
	if !fsHasTarget(calls, "realTwo") {
		t.Errorf("widening: alpha CALLS = %v, want it to contain realTwo", calls)
	}
	for _, ghost := range []string{"ghostOne", "ghostStr"} {
		if fsHasTarget(calls, ghost) {
			t.Errorf("widening: alpha CALLS = %v, must NOT contain %s — it lives in a comment or a string literal", calls, ghost)
		}
	}

	const typeSrc = "module M\n" + // 1
		"\n" + // 2
		"type Derived() =\n" + // 3
		" inherit RealBase()\n" + // 4   base+1 genuine clause
		" // inherit GhostA()\n" + // 5   base+1 comment
		" member _.Show () = \"inherit GhostB()\"\n" // 6   base+1 string literal

	ents = runFSharp(t, typeSrc, "src/Wide.fs")
	got := fsToIDs(fsRelsOfKind(t, ents, "Derived", "EXTENDS"))
	if len(got) != 1 || got[0] != "RealBase" {
		t.Errorf("widening: Derived EXTENDS = %v, want exactly [RealBase] — GhostA is a comment and GhostB a string literal", got)
	}
}

// ---------------------------------------------------------------------------
// Call sites 4 and 5 of 5 — compexpr_active_patterns.go:517 (collectCEBuilderTypes)
// and validators.go:344 (collectRecordTypeNames), both on b31fe2c67. The issue
// body named only three; these two also call extractIndentBody, and an earlier
// revision of this PR wrote them off as "covered transitively by the package
// suite". That claim was false: M2 measures the 162 pre-existing tests as blind
// to this boundary, so transitive coverage here is coverage by nothing.
//
// Each of these two passes threads its result through a DIFFERENT consumer than
// the type pass does, so the artefact asserted below is the one that only that
// pass can produce.
// ---------------------------------------------------------------------------

func TestFSharp_IndentBand7176_CEBuilderPass(t *testing.T) {
	// Axis VARIED: the builder members' indent (1, 2, 0).
	// Axis HELD CONSTANT: the builder type's column (0), the member names, the
	// binding `let optional = OptBuilder()`, and the consuming `optional { }`.
	//
	// collectCEBuilderTypes (:517) is the ONLY producer of ceMemberNames, which
	// is what re-types a protocol member to the `ce_member` subtype in the
	// member pass. So Bind/Return's Subtype is this call site's own artefact —
	// OptBuilder's own `computation_builder` subtype comes from the type pass's
	// separate detectCEBuilder call and is asserted here only as a companion.
	src := func(pad string) string {
		return "module M\n" + // 1
			"\n" + // 2
			"type OptBuilder() =\n" + // 3   base indent 0
			pad + "member _.Bind (x, f) = Option.bind f x\n" + // 4
			pad + "member _.Return x = Some x\n" + // 5
			"\n" + // 6
			"let optional = OptBuilder()\n" // 7
	}

	ents := runFSharp(t, src(" "), "src/CE.fs") // base+1 <-- the dead band
	for _, name := range []string{"Bind", "Return"} {
		rec := fsFind(ents, name, "SCOPE.Operation")
		if rec == nil {
			t.Fatalf("band+1: no SCOPE.Operation named %q", name)
		}
		if rec.Subtype != "ce_member" {
			t.Errorf("band+1: %s Subtype = %q, want %q (collectCEBuilderTypes saw an empty body, so ceMemberNames was empty)", name, rec.Subtype, "ce_member")
		}
		if rec.Properties["ce_member"] != "true" {
			t.Errorf("band+1: %s ce_member property = %q, want \"true\"", name, rec.Properties["ce_member"])
		}
	}
	if rec := fsFind(ents, "OptBuilder", "SCOPE.Component"); rec == nil {
		t.Fatal("band+1: no SCOPE.Component named OptBuilder")
	} else if rec.Subtype != "computation_builder" {
		t.Errorf("band+1: OptBuilder Subtype = %q, want %q", rec.Subtype, "computation_builder")
	}

	// Control, the deeper neighbour.
	ents = runFSharp(t, src("  "), "src/CE.fs")
	if rec := fsFind(ents, "Bind", "SCOPE.Operation"); rec == nil || rec.Subtype != "ce_member" {
		t.Errorf("ctrl base+2: Bind Subtype = %v, want ce_member", rec)
	}

	// Control, the shallower neighbour: members at the type's own column are
	// outside its body, so the type is NOT a builder and nothing is re-typed.
	ents = runFSharp(t, src(""), "src/CE.fs")
	for _, name := range []string{"Bind", "Return"} {
		rec := fsFind(ents, name, "SCOPE.Operation")
		if rec == nil {
			t.Fatalf("ctrl base+0: no SCOPE.Operation named %q", name)
		}
		if rec.Subtype == "ce_member" {
			t.Errorf("ctrl base+0: %s Subtype = %q, must NOT be ce_member — a member at column 0 is offside of the type body", name, rec.Subtype)
		}
	}
	if rec := fsFind(ents, "OptBuilder", "SCOPE.Component"); rec == nil {
		t.Fatal("ctrl base+0: no SCOPE.Component named OptBuilder")
	} else if rec.Subtype == "computation_builder" {
		t.Error("ctrl base+0: OptBuilder must NOT be a computation_builder — its body ended at column 0")
	}
}

func TestFSharp_IndentBand7176_RecordTypeNamesPass(t *testing.T) {
	// Axis VARIED: the record brace line's indent (1, 2, 0).
	// Axis HELD CONSTANT: Addr's column (0), the field names, and the consuming
	// record Person (whose own fields stay at their original columns).
	//
	// collectRecordTypeNames (:344) is the ONLY producer of the recordTypes set
	// that lets a field whose type is another in-file RECORD mint the
	// nested-model VALIDATES edge. So `Person VALIDATES -> Addr` is this call
	// site's own artefact — Addr's own `record` subtype comes from the type
	// pass's separate classifyTypeSubtype call and is a companion assertion.
	src := func(pad string) string {
		return "module M\n" + // 1
			"\n" + // 2
			"type Addr =\n" + // 3   base indent 0
			pad + "{ City : string }\n" + // 4
			"\n" + // 5
			"type Person =\n" + // 6
			"    { Name : string\n" + // 7
			"      Home : Addr }\n" // 8
	}

	ents := runFSharp(t, src(" "), "src/Rec.fs") // base+1 <-- the dead band
	if !fsHasTarget(fsToIDs(fsRelsOfKind(t, ents, "Person", "VALIDATES")), "Addr") {
		t.Errorf("band+1: Person VALIDATES = %v, want it to contain Addr (collectRecordTypeNames saw an empty body, so Addr was not a record)", fsToIDs(fsRelsOfKind(t, ents, "Person", "VALIDATES")))
	}
	if rec := fsFind(ents, "Addr", "SCOPE.Component"); rec == nil {
		t.Fatal("band+1: no SCOPE.Component named Addr")
	} else if rec.Subtype != "record" {
		t.Errorf("band+1: Addr Subtype = %q, want %q", rec.Subtype, "record")
	}

	// Control, the deeper neighbour.
	ents = runFSharp(t, src("  "), "src/Rec.fs")
	if !fsHasTarget(fsToIDs(fsRelsOfKind(t, ents, "Person", "VALIDATES")), "Addr") {
		t.Error("ctrl base+2: Person VALIDATES must contain Addr")
	}

	// Control, the shallower neighbour: a brace line at column 0 is outside
	// Addr's body, so Addr is not a record and the nested edge must not exist.
	ents = runFSharp(t, src(""), "src/Rec.fs")
	if fsHasTarget(fsToIDs(fsRelsOfKind(t, ents, "Person", "VALIDATES")), "Addr") {
		t.Error("ctrl base+0: Person VALIDATES must NOT contain Addr — Addr's body ended at column 0")
	}
	if rec := fsFind(ents, "Addr", "SCOPE.Component"); rec == nil {
		t.Fatal("ctrl base+0: no SCOPE.Component named Addr")
	} else if rec.Subtype == "record" {
		t.Error("ctrl base+0: Addr must NOT be a record — its body ended at column 0")
	}
}
