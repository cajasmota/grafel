package fsharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// fsRelsOfKind returns every embedded relationship of edgeKind carried by the
// named record. It deliberately looks the edge up THROUGH the owning entity, so
// an edge emitted on the file/module component instead of the type is invisible
// here and the assertion fails.
func fsRelsOfKind(t *testing.T, ents []types.EntityRecord, name, edgeKind string) []types.RelationshipRecord {
	t.Helper()
	rec := fsFind(ents, name, "SCOPE.Component")
	if rec == nil {
		t.Fatalf("no SCOPE.Component entity named %q", name)
	}
	var out []types.RelationshipRecord
	for _, r := range rec.Relationships {
		if r.Kind == edgeKind {
			out = append(out, r)
		}
	}
	return out
}

func fsToIDs(rels []types.RelationshipRecord) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.ToID)
	}
	return out
}

// #6326 — `inherit Base()` is an EXTENDS edge. Before this fix the F# extractor
// emitted no inheritance topology at all.
func TestFSharp_InheritEmitsExtends(t *testing.T) {
	src := `namespace App

type Base() =
    member _.Hello () = ()

type Derived() =
    inherit Base()
    member _.Bye () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	got := fsToIDs(fsRelsOfKind(t, ents, "Derived", "EXTENDS"))
	if len(got) != 1 || got[0] != "Base" {
		t.Fatalf("Derived EXTENDS = %v, want [Base]", got)
	}
	if base := fsRelsOfKind(t, ents, "Base", "EXTENDS"); len(base) != 0 {
		t.Errorf("Base EXTENDS = %v, want none", fsToIDs(base))
	}
}

// A generic / dotted / argument-less base still resolves to the bare type name.
func TestFSharp_InheritVariants(t *testing.T) {
	src := `namespace App

type A() =
    inherit System.Exception("boom")

type B() =
    inherit Collections.Generic.List<string>()

type C() =
    inherit MarkerBase
`
	ents := runFSharp(t, src, "src/App.fs")

	for _, tc := range []struct{ typ, want string }{
		{"A", "System.Exception"},
		{"B", "Collections.Generic.List"},
		{"C", "MarkerBase"},
	} {
		got := fsToIDs(fsRelsOfKind(t, ents, tc.typ, "EXTENDS"))
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s EXTENDS = %v, want [%s]", tc.typ, got, tc.want)
		}
	}
}

// #6326 — `interface IFoo with` is an IMPLEMENTS edge.
func TestFSharp_InterfaceWithEmitsImplements(t *testing.T) {
	src := `namespace App

open System

type Resource() =
    interface IDisposable with
        member _.Dispose () = ()
    interface ICloneable with
        member _.Clone () = box 1
`
	ents := runFSharp(t, src, "src/App.fs")

	got := fsToIDs(fsRelsOfKind(t, ents, "Resource", "IMPLEMENTS"))
	if len(got) != 2 || got[0] != "IDisposable" || got[1] != "ICloneable" {
		t.Fatalf("Resource IMPLEMENTS = %v, want [IDisposable ICloneable]", got)
	}
}

// An `interface ... end` type DECLARATION is not an implementation and must not
// produce an IMPLEMENTS edge (the `with` keyword is what marks an impl block).
func TestFSharp_InterfaceDeclarationIsNotImplements(t *testing.T) {
	src := `namespace App

type IGreeter =
    interface
        abstract member Greet : string -> unit
    end
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsRelsOfKind(t, ents, "IGreeter", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("IGreeter IMPLEMENTS = %v, want none", fsToIDs(got))
	}
}

// #6295 / #6298 — the edge must be anchored on the TYPE, not on the file.
//
// Two guarantees, both of which a file-anchored emitter breaks:
//   - FromID is EMPTY, so the assembly loop stamps the owning entity's own id.
//     A non-empty, non-hex FromID is rewritten by resolve.ReferencesEmbedded,
//     and a file path rewrites onto the file component.
//   - Each type carries only its OWN bases. When the emitter anchors on the
//     file, a multi-type file merges every base list onto one node.
func TestFSharp_HierarchyAnchoredOnTypeNotFile(t *testing.T) {
	src := `namespace App

open System

type Vault() =
    inherit Ownable()
    interface IDisposable with
        member _.Dispose () = ()

type Registry() =
    inherit Storage()
    interface ICloneable with
        member _.Clone () = box 1
`
	ents := runFSharp(t, src, "src/App.fs")

	cases := []struct {
		typ, base, iface string
	}{
		{"Vault", "Ownable", "IDisposable"},
		{"Registry", "Storage", "ICloneable"},
	}
	for _, tc := range cases {
		ext := fsRelsOfKind(t, ents, tc.typ, "EXTENDS")
		if got := fsToIDs(ext); len(got) != 1 || got[0] != tc.base {
			t.Errorf("%s EXTENDS = %v, want [%s]", tc.typ, got, tc.base)
		}
		impl := fsRelsOfKind(t, ents, tc.typ, "IMPLEMENTS")
		if got := fsToIDs(impl); len(got) != 1 || got[0] != tc.iface {
			t.Errorf("%s IMPLEMENTS = %v, want [%s]", tc.typ, got, tc.iface)
		}
		for _, r := range append(append([]types.RelationshipRecord{}, ext...), impl...) {
			if r.FromID != "" {
				t.Errorf("%s %s %s: FromID = %q, want \"\" (edge anchors on the owning type)",
					tc.typ, r.Kind, r.ToID, r.FromID)
			}
		}
	}

	// No hierarchy edge may hang off the namespace/module component either.
	for i := range ents {
		if ents[i].Subtype != "namespace" && ents[i].Subtype != "module" {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "EXTENDS" || r.Kind == "IMPLEMENTS" {
				t.Errorf("%s %q carries %s -> %s; hierarchy edges belong on the type",
					ents[i].Subtype, ents[i].Name, r.Kind, r.ToID)
			}
		}
	}
}

// #6326 requirement 5 — the edges must carry the language stamp so
// resolve.relLanguage can pick the right locality tier.
func TestFSharp_HierarchyEdgesLanguageTagged(t *testing.T) {
	src := `namespace App

open System

type Derived() =
    inherit Base()
    interface IDisposable with
        member _.Dispose () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	rels := append(fsRelsOfKind(t, ents, "Derived", "EXTENDS"),
		fsRelsOfKind(t, ents, "Derived", "IMPLEMENTS")...)
	if len(rels) != 2 {
		t.Fatalf("got %d hierarchy edges, want 2", len(rels))
	}
	for _, r := range rels {
		v, ok := r.Properties.Lookup("language")
		if !ok || v != "fsharp" {
			t.Errorf("%s -> %s: language = %q (present=%v), want \"fsharp\"", r.Kind, r.ToID, v, ok)
		}
		if _, ok := r.Properties.Lookup("line"); !ok {
			t.Errorf("%s -> %s: no line property", r.Kind, r.ToID)
		}
	}
}

// The hierarchy scanner must not fire inside comments or string literals. The
// keyword sits at the start of its line in every case below, which is exactly
// where the scanner looks — only the string/comment scrub keeps it out.
func TestFSharp_HierarchyIgnoresCommentsAndStrings(t *testing.T) {
	src := `namespace App

type Plain() =
    (*
    inherit Ghost()
    interface IPhantom with
    *)
    let doc = """
    inherit Spectre()
    interface IWraith with
    """
    member _.Doc () = doc
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsRelsOfKind(t, ents, "Plain", "EXTENDS"); len(got) != 0 {
		t.Errorf("Plain EXTENDS = %v, want none", fsToIDs(got))
	}
	if got := fsRelsOfKind(t, ents, "Plain", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("Plain IMPLEMENTS = %v, want none", fsToIDs(got))
	}
}

// The stamped line must be the real file line. stripStringsAndComments blanks
// the newlines inside block comments and triple-quoted strings, so a line count
// taken over the scrubbed text drifts by one per swallowed newline.
func TestFSharp_HierarchyLineStamping(t *testing.T) {
	src := `namespace App

type Real() =
    (*
    a
    b
    *)
    inherit Actual()
    interface IThing with
        member _.Do () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	// L1 namespace, L2 blank, L3 type, L4-L7 block comment, L8 inherit, L9 interface.
	want := map[string]string{"EXTENDS": "8", "IMPLEMENTS": "9"}
	for kind, wantLine := range want {
		rels := fsRelsOfKind(t, ents, "Real", kind)
		if len(rels) != 1 {
			t.Fatalf("Real %s = %v, want 1 edge", kind, fsToIDs(rels))
		}
		got, _ := rels[0].Properties.Lookup("line")
		if got != wantLine {
			t.Errorf("Real %s -> %s: line = %q, want %q", kind, rels[0].ToID, got, wantLine)
		}
	}
}

// ── Object expressions (review P14/P15) ──────────────────────────────────────

// An F# object expression `{ new IFoo with ... }` carries its OWN
// `interface X with` clauses. They belong to the anonymous object, not to the
// enclosing type, so attributing them to the enclosing type fabricates edges.
// This is the standard custom-sequence idiom, not a contrived shape.
func TestFSharp_ObjectExpressionInterfacesNotAttributedToType(t *testing.T) {
	src := `namespace App

open System
open System.Collections.Generic

type Counter(limit: int) =
    member _.Enumerate () =
        { new IEnumerator<int> with
              member _.Current = 0
          interface IEnumerator with
              member _.Current = box 0
          interface IDisposable with
              member _.Dispose () = () }
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsRelsOfKind(t, ents, "Counter", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("Counter IMPLEMENTS = %v, want none (all three sit inside an object expression)", fsToIDs(got))
	}
	if got := fsRelsOfKind(t, ents, "Counter", "EXTENDS"); len(got) != 0 {
		t.Errorf("Counter EXTENDS = %v, want none", fsToIDs(got))
	}
}

// The harder half: a REAL clause and a fabricated one in the same type. Before
// the fix these were indistinguishable in the output.
func TestFSharp_ObjectExpressionMixedWithRealImplements(t *testing.T) {
	src := `namespace App

open System

type Cache() =
    interface ICache with
        member _.Get k = None
    member _.Wrap () =
        { new IWrapper with
              member _.Name = "w"
          interface IDisposable with
              member _.Dispose () = () }
`
	ents := runFSharp(t, src, "src/App.fs")

	got := fsToIDs(fsRelsOfKind(t, ents, "Cache", "IMPLEMENTS"))
	if len(got) != 1 || got[0] != "ICache" {
		t.Errorf("Cache IMPLEMENTS = %v, want [ICache] (IDisposable is the object expression's)", got)
	}
}

// Brace bookkeeping must be balanced, not sticky: a record literal in a member
// body opens and closes, and a real clause AFTER it is still the type's own.
func TestFSharp_RecordLiteralDoesNotSwallowLaterClauses(t *testing.T) {
	src := `namespace App

type Store() =
    member _.Make () = { Name = "a"; Size = 1 }
    interface IStore with
        member _.Save () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	got := fsToIDs(fsRelsOfKind(t, ents, "Store", "IMPLEMENTS"))
	if len(got) != 1 || got[0] != "IStore" {
		t.Errorf("Store IMPLEMENTS = %v, want [IStore]", got)
	}
}

// ── Signature files (review item 2) ─────────────────────────────────────────

// `.fsi` routes to this extractor (classifier.go:454, substrate.go:193) and
// emits NO hierarchy edges at all.
//
// F# requires every signature file to be paired with the `.fs` it describes,
// and that `.fs` carries the same `inherit` / `interface ... with` clauses. But
// graph.EntityID hashes SourceFile, so the two `Derived` components get
// DIFFERENT ids and the (from, to, kind) dedup triple differs — the edge is
// counted twice, inheritance queries return the type twice, and centrality and
// impact-radius double-weight it. That is the same "one logical thing, two
// nodes" shape as #6295/#6298 that this whole change exists to avoid, so the
// `.fs` is treated as the single source of truth for hierarchy.
func TestFSharp_SignatureFileEmitsNoHierarchyEdges(t *testing.T) {
	// Every clause form a .fsi can carry, including the `with` form.
	src := `namespace App

open System

type Derived =
    inherit Base

type Res =
    interface IDisposable

type Impl() =
    interface ICloneable with
        member _.Clone () = box 1
`
	ents := runFSharp(t, src, "src/Foo.fsi")

	for _, typ := range []string{"Derived", "Res", "Impl"} {
		if got := fsRelsOfKind(t, ents, typ, "EXTENDS"); len(got) != 0 {
			t.Errorf(".fsi %s EXTENDS = %v, want none (the paired .fs owns it)", typ, fsToIDs(got))
		}
		if got := fsRelsOfKind(t, ents, typ, "IMPLEMENTS"); len(got) != 0 {
			t.Errorf(".fsi %s IMPLEMENTS = %v, want none (the paired .fs owns it)", typ, fsToIDs(got))
		}
	}
}

// The gate keys on `.fsi` specifically, NOT on "not .fs". A `.fsx` script is
// standalone — it has no paired implementation file — so it keeps its edges.
func TestFSharp_ScriptFileKeepsHierarchyEdges(t *testing.T) {
	src := `namespace App

open System

type Derived() =
    inherit Base()
    interface IDisposable with
        member _.Dispose () = ()
`
	ents := runFSharp(t, src, "scripts/build.fsx")

	if got := fsToIDs(fsRelsOfKind(t, ents, "Derived", "EXTENDS")); len(got) != 1 || got[0] != "Base" {
		t.Errorf(".fsx Derived EXTENDS = %v, want [Base]", got)
	}
	if got := fsToIDs(fsRelsOfKind(t, ents, "Derived", "IMPLEMENTS")); len(got) != 1 || got[0] != "IDisposable" {
		t.Errorf(".fsx Derived IMPLEMENTS = %v, want [IDisposable]", got)
	}
}

// Binds the claim in insideBraces' doc comment that it is handed the SCRUBBED
// body: an unbalanced `{` inside a string literal must not suppress a real
// clause below it. Counting braces over the raw body would.
func TestFSharp_BraceInStringDoesNotSuppressClause(t *testing.T) {
	src := `namespace App

type Tagged() =
    member _.Fmt () = "unbalanced { brace"
    interface ITagged with
        member _.Tag = "t"
`
	ents := runFSharp(t, src, "src/App.fs")

	got := fsToIDs(fsRelsOfKind(t, ents, "Tagged", "IMPLEMENTS"))
	if len(got) != 1 || got[0] != "ITagged" {
		t.Errorf("Tagged IMPLEMENTS = %v, want [ITagged]", got)
	}
}

// ── Brace-count scrub adequacy (re-review item 1) ────────────────────────────

// stripStringsAndComments was built to suppress CALL TOKENS, where a leftover
// brace was harmless. insideBraces reuses it for BALANCED COUNTING, where it is
// load-bearing — and it has no case for single-quoted char literals.
//
// An unmatched `'}'` cancels the object expression's `{`, which puts the
// object expression's own clause back at apparent depth 0 and re-opens exactly
// the false positive the brace gate was added to close.
func TestFSharp_CharLiteralCloseBraceDoesNotAdmitObjectExpression(t *testing.T) {
	src := `namespace App

open System

type Lexer() =
    interface IReal with
        member _.Go () = ()
    member _.IsClose (c: char) = (c = '}')
    member _.Wrap () =
        { new IWrapper with
              member _.Name = "w"
          interface IDisposable with
              member _.Dispose () = () }
`
	ents := runFSharp(t, src, "src/App.fs")

	got := fsToIDs(fsRelsOfKind(t, ents, "Lexer", "IMPLEMENTS"))
	if len(got) != 1 || got[0] != "IReal" {
		t.Errorf("Lexer IMPLEMENTS = %v, want [IReal] (the '}' char literal must not cancel the object expression's brace)", got)
	}
}

// The mirror image: an unmatched `'{'` must not suppress the type's own real
// clauses below it.
func TestFSharp_CharLiteralOpenBraceDoesNotSuppressClauses(t *testing.T) {
	src := `namespace App

open System

type Lexer() =
    member _.IsOpen (c: char) = (c = '{')
    inherit Reader()
    interface IReal with
        member _.Go () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsToIDs(fsRelsOfKind(t, ents, "Lexer", "EXTENDS")); len(got) != 1 || got[0] != "Reader" {
		t.Errorf("Lexer EXTENDS = %v, want [Reader]", got)
	}
	if got := fsToIDs(fsRelsOfKind(t, ents, "Lexer", "IMPLEMENTS")); len(got) != 1 || got[0] != "IReal" {
		t.Errorf("Lexer IMPLEMENTS = %v, want [IReal]", got)
	}
}

// A char literal that BALANCES is fine either way; this pins that the fix does
// not over-blank and lose a real brace pair.
func TestFSharp_BalancedCharLiteralBracesStillGateObjectExpression(t *testing.T) {
	src := `namespace App

open System

type Lexer() =
    member _.Kind (c: char) =
        match c with
        | '{' -> 1
        | '}' -> 2
        | _ -> 0
    member _.Wrap () =
        { new IWrapper with
              member _.Name = "w"
          interface IDisposable with
              member _.Dispose () = () }
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsRelsOfKind(t, ents, "Lexer", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("Lexer IMPLEMENTS = %v, want none (all inside the object expression)", fsToIDs(got))
	}
}

// F# block comments NEST. The scrubber stopped at the first `*)`, so the tail
// of an outer comment stayed visible and its braces entered the count —
// silently losing every clause below it.
func TestFSharp_NestedBlockCommentDoesNotSwallowClauses(t *testing.T) {
	src := `namespace App

open System

type Doc() =
    (* outer (* inner *) { still comment *)
    inherit Base()
    interface IDisposable with
        member _.Dispose () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsToIDs(fsRelsOfKind(t, ents, "Doc", "EXTENDS")); len(got) != 1 || got[0] != "Base" {
		t.Errorf("Doc EXTENDS = %v, want [Base]", got)
	}
	if got := fsToIDs(fsRelsOfKind(t, ents, "Doc", "IMPLEMENTS")); len(got) != 1 || got[0] != "IDisposable" {
		t.Errorf("Doc IMPLEMENTS = %v, want [IDisposable]", got)
	}
}

// A nested block comment must still hide what it contains — the nesting fix
// must not stop the scrubber from blanking.
func TestFSharp_NestedBlockCommentStillHidesItsContents(t *testing.T) {
	src := `namespace App

type Doc() =
    (* outer (* inner
    inherit Ghost()
    *) interface IPhantom with *)
    member _.Noop () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsRelsOfKind(t, ents, "Doc", "EXTENDS"); len(got) != 0 {
		t.Errorf("Doc EXTENDS = %v, want none", fsToIDs(got))
	}
	if got := fsRelsOfKind(t, ents, "Doc", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("Doc IMPLEMENTS = %v, want none", fsToIDs(got))
	}
}

// charBraceRE matches only the two BRACE-bearing char literals, not char
// literals in general. F# identifiers may end in an apostrophe (`c'`), so a
// general `'.'` scrub misreads `c' '}'` — a primed identifier applied next to a
// char literal, ordinary F# — as the span `' '`, blanks that instead, and
// leaves the `}` live to cancel the object expression's brace. The narrow form
// has no such failure mode because a generic parameter or a primed identifier
// can never look like `'{'` or `'}'`.
func TestFSharp_PrimedIdentifierBeforeCharLiteralBrace(t *testing.T) {
	src := `namespace App

open System

type Lexer() =
    interface IReal with
        member _.Go () = ()
    member _.Wrap (c': char) =
        { new IWrapper with
              member _.Name = fmt c' '}'
          interface IDisposable with
              member _.Dispose () = () }
`
	ents := runFSharp(t, src, "src/App.fs")

	got := fsToIDs(fsRelsOfKind(t, ents, "Lexer", "IMPLEMENTS"))
	if len(got) != 1 || got[0] != "IReal" {
		t.Errorf("Lexer IMPLEMENTS = %v, want [IReal]", got)
	}
}

// `(*)` is F#'s multiplication operator passed as a function value, not a
// comment opener. Treating it as one opens a runaway comment that eats
// everything to the next `*)` — here, the type's own `interface` clause.
// Bound because the exclusion is load-bearing but was otherwise invisible to
// the suite: `List.fold (*) 1 xs` is ordinary F#.
func TestFSharp_MultiplicationOperatorIsNotACommentOpener(t *testing.T) {
	src := `namespace App

type Calc() =
    inherit Base()
    member _.Prod xs = List.fold (*) 1 xs
    interface IFoo with
        member _.F () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsToIDs(fsRelsOfKind(t, ents, "Calc", "EXTENDS")); len(got) != 1 || got[0] != "Base" {
		t.Errorf("Calc EXTENDS = %v, want [Base]", got)
	}
	if got := fsToIDs(fsRelsOfKind(t, ents, "Calc", "IMPLEMENTS")); len(got) != 1 || got[0] != "IFoo" {
		t.Errorf("Calc IMPLEMENTS = %v, want [IFoo] (the (*) operator must not open a comment)", got)
	}
}

// The counterpart, asserted so it is not "fixed" into a mismatch: the `(*)`
// exclusion applies at the OPENING check only, not inside the nesting loop, so
// `(* fold with the (*) operator *)` still runs away and costs the clauses
// below it. That is correct. F# block comments nest, so the inner `(*` opens a
// nested comment that the single `*)` closes, leaving the outer one unbalanced
// — fsc rejects the file outright. Matching the compiler beats being cleverer
// than it, and a file this fixture describes cannot compile in the first place.
func TestFSharp_OperatorInsideBlockCommentRunsAway_MatchesCompiler(t *testing.T) {
	src := `namespace App

type Calc() =
    inherit Base()
    (* fold with the (*) operator *)
    interface IFoo with
        member _.F () = ()
`
	ents := runFSharp(t, src, "src/App.fs")

	if got := fsToIDs(fsRelsOfKind(t, ents, "Calc", "EXTENDS")); len(got) != 1 || got[0] != "Base" {
		t.Errorf("Calc EXTENDS = %v, want [Base] (above the runaway)", got)
	}
	if got := fsRelsOfKind(t, ents, "Calc", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("Calc IMPLEMENTS = %v, want none — the unbalanced nested comment swallows it, as fsc would", fsToIDs(got))
	}
}

// TestFSharp_DeclarationAfterMidLineCommentClose pins the RECALL GAIN that
// preserving interior newlines produced (#6336).
//
// When a block comment closes mid-line and real code follows `*)` on that same
// physical line, blanking the interior newlines used to glue that code onto the
// line where the comment OPENED — behind `let x = 1`, which is not whitespace,
// so `(?m)^[ \t]*interface` had no line start to anchor on and the clause was
// invisible. Restoring the newline gives the declaration its own line whose
// prefix is all blanked-delimiter spaces, which `^[ \t]*` absorbs.
//
// Measured by diffing extractor output against the same tree with the newline
// restoration removed: before, Person had no IMPLEMENTS and no VALIDATES; after,
// it has both, with correct lines. This is ordinary F#, so it must not silently
// regress back.
func TestFSharp_DeclarationAfterMidLineCommentClose(t *testing.T) {
	src := `namespace App

open System.ComponentModel.DataAnnotations

type Person() =
    let x = 1 (* trailing
    comment *) interface IValidatableObject with
        member _.Validate ctx = Seq.empty
`
	ents := runFSharp(t, src, "midline.fs")

	rels := fsRelsOfKind(t, ents, "Person", "IMPLEMENTS")
	if len(rels) != 1 || rels[0].ToID != "IValidatableObject" {
		t.Fatalf("Person IMPLEMENTS = %v, want [IValidatableObject]", fsToIDs(rels))
	}
	// `interface ...` sits on FILE line 7 (1 namespace, 3 open, 5 type, 6 let).
	if got := rels[0].Properties.Get("line"); got != "7" {
		t.Errorf("IMPLEMENTS line = %q, want %q", got, "7")
	}
	if !fsHasRel(ents, "Person", "SCOPE.Component", "VALIDATES", "validator:dataannotations") {
		t.Error("expected VALIDATES validator:dataannotations (the IValidatableObject clause is what marks it)")
	}
}

// TestFSharp_KnownLimitation_SpaceApplicationSplitByBlockComment records the
// RECALL LOSS that came with #6336, so it is discoverable instead of folklore.
//
// spaceAppRE (extractor.go) — and felizChildRE in elmish_feliz.go — match a
// head and its argument with `[ \t]+` between them. That character class cannot
// cross a newline. While the scrub blanked interior newlines, a multi-line
// block comment BETWEEN a head and its argument collapsed to plain spaces and
// the space application still matched; now the restored newline splits it and
// the CALLS edge is gone. Verified by diffing against the same tree with the
// restoration removed: `user CALLS helper line=6` before, no edge after.
//
// The trade was taken knowingly. The gain (see
// TestFSharp_DeclarationAfterMidLineCommentClose) is ordinary F# and the line
// stamps this whole change fixes are on every call site in the corpus, while
// this shape needs an argument separated from its head by a multi-line comment.
//
// THIS TEST ASSERTS THE LIMITATION, not the desired behaviour. If it starts
// failing, someone has fixed the gap — that is good news: delete this test and
// assert the CALLS edge instead. A fix would let spaceAppRE span a run of
// whitespace that includes newlines, which needs its own issue because F#'s
// offside rule makes "argument on a later line" genuinely ambiguous.
func TestFSharp_KnownLimitation_SpaceApplicationSplitByBlockComment(t *testing.T) {
	src := `module App

let helper x = x

let user n =
    let a = helper (*
    gap
    *) n
    a
`
	ents := runFSharp(t, src, "spaceapp.fs")

	if fsHasRel(ents, "user", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("CALLS user→helper is now found — the spaceAppRE newline gap is FIXED; " +
			"delete this known-limitation test and assert the edge instead")
	}
}
