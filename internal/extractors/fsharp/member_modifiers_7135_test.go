package fsharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// #7135 (`memberRE` arm) — `memberRE` accommodated NO member modifiers at all:
//
//	^([ \t]*)(?:member|override|abstract member|default)\s+(?:ident\.)?(name)
//
// It has two distinct failure modes, and they are not the same bug.
//
//   - MIS-NAME. `member private this.Go = ...` is indexed as an operation
//     called `private`: the optional instance-qualifier group needs a trailing
//     `.`, `private ` has none, so the qualifier group matches empty and the
//     NAME capture takes `private`. `member val Go = 1` is indexed as `val`
//     the same way. Note what this ISN'T: the qualifier group itself is
//     correct — it handles `this.`/`self.`/`_.` fine, and does so both before
//     and after this change. The single cause is the ABSENT modifier group,
//     which leaves the name capture as the first thing after the keyword.
//
//   - SILENT TOTAL MISS. `static member` is not extracted at all, because
//     `static` precedes the keyword the pattern anchors on. A missing entity
//     leaves no trace — it reads as "this type has no members" — so mode B is
//     asserted here by ENTITY COUNT, which is the only direction that can see
//     an absence.
//
// GRAMMAR, and WHICH SOURCES WERE UNIONED. The accepted modifier set is not
// readable off one grammar production: § 8.13's `member-defn` genuinely omits
// `inline`, and a first revision of this change derived from that production
// alone and shipped an incomplete set, under which `member inline this.f`
// still captured the name `inline`. Three sources are unioned here:
//
//  1. F# Language Specification § 8.13 "Members"
//     (https://fsharp.github.io/fslang-spec/type-definitions/) for the
//     productions and the ORDER, plus § 10.5 "Accessibility Annotations"
//     for `access`;
//  2. the official language reference — MS Learn "Inline Functions"
//     (learn.microsoft.com/dotnet/fsharp/language-reference/functions/
//     inline-functions): the `inline` modifier "can be applied to functions
//     at the top level, at the module level, or at the method level in a
//     class", with the example `member inline this.incrementByOne(x)` /
//     `static member inline Increment(x)`;
//  3. the sibling scanner in the same file — letRE (#7131) already
//     allowlists `inline`.
//
// § 8.13 productions:
//
//	member-defn :=
//	    attributes? static? member access? method-or-prop-defn
//	    attributes? abstract member? access? member-sig
//	    attributes? override access? method-or-prop-defn
//	    attributes? default access? method-or-prop-defn
//	    attributes? static? member auto-prop-defn
//	    attributes? override auto-prop-defn
//	    attributes? default auto-prop-defn
//	    attributes? static? val mutable? access? ident ':' type
//	    additional-constr-defn
//
//	auto-prop-defn :=
//	    val access? ident ':' type? = expr
//	    val access? ident ':' type? = expr with get
//	    val access? ident ':' type? = expr with get ',' set        (and set,get)
//
//	method-or-prop-defn :=
//	    (ident '.')? ident pat1 ... patn = expr
//	    (ident '.')? ident = expr
//	    ... (property with get/set forms)
//
//	access := public | private | internal          (§ 10.5)
//
// Four consequences the table below is built on:
//
//  1. `static` is a PREFIX to the keyword, never a suffix — `member static` is
//     not F#, so no row claims it.
//  2. `inline` is legal on a member per source (2) above even though the
//     § 8.13 production omits it.
//  3. `val` is NOT a modifier: `member val` selects the auto-property
//     production, in which the NAME is the `ident` after `val` (and after an
//     optional access modifier, since auto-prop-defn is `val access? ident`).
//     `member val private Count = 0 with get, set` therefore names `Count`.
//  4. The attested order after the keyword is access-then-name for
//     method-or-prop-defn, and `val` then access then name for the
//     auto-property. Any other order is scanner lenience only, and is labelled
//     per row.
//
// `abstract`/`abstract member` declare a member-sig with NO `= expr`, so those
// forms never reach this pattern (which requires a `=`); the keyword stays in
// the alternation for `abstract member Foo() = ...`-shaped lenience only and
// no row claims an abstract signature is extracted.
//
// Axes VARIED: the keyword (`member`/`override`/`default`), presence of
// `static`, presence of `inline`, access modifier identity
// (public/private/internal), the auto-
// property `val` form with and without access and with and without
// `with get, set`, presence of the instance qualifier (`this.`, `_.`, none),
// presence of parameters, presence of a generic parameter list, and (outside
// the table) indentation and duplicate-collapse.
//
// Axes HELD CONSTANT: the member name (always `Target`, so a wrong capture is
// unambiguous), the entity kind (SCOPE.Operation) and subtype (`member`), one
// member of interest per source, the enclosing `type Holder() =`, and the file
// path.
//
// No F# toolchain exists in this environment (`dotnet`, `fsc`, `fsharpc`,
// `fsi`, `mono` are all absent; `javac` is the only compiler present), so
// every classification below is read off the specification grammar and was
// NOT executed against a compiler.

// specGrammarAdmits is a THIRD grammar label, added because `specLegal` is
// defined in let_modifiers_7131_test.go as "legal F#" — a claim about the
// COMPILER — while some rows are only known to be admitted by the § 8.13
// grammar. An instance member with no self-identifier (`member Target x = x`)
// and an accessibility modifier on an `override`/`default` are both
// grammar-admitted and plausibly compiler-rejected; no F# compiler exists in
// this environment, so neither direction is executed. The rows are kept
// because they pin the scanner against the shape, not against the language.
const specGrammarAdmits fsGrammar = "grammar-admitted by § 8.13; compiler acceptance NOT verified"

// fsFindMember returns the SCOPE.Operation named name with subtype "member".
func fsFindMember(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "member" {
			return &ents[i]
		}
	}
	return nil
}

// fsMemberNames lists the names of every `member` SCOPE.Operation, in order.
func fsMemberNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "member" {
			out = append(out, ents[i].Name)
		}
	}
	return out
}

// fsMemberModifierWords are the words that must never become an entity name.
var fsMemberModifierWords = []string{
	"static", "inline", "val", "public", "private", "internal",
	"member", "override", "default", "abstract", "this", "self",
}

// TestMemberModifiers_NameIsNeverTheModifier enumerates the member modifier
// space rather than sampling it. Each row must yield exactly one `member`
// entity named `Target` — the COUNT is asserted, not just the presence, so
// that mode B (a declaration the scanner does not see at all) is visible.
func TestMemberModifiers_NameIsNeverTheModifier(t *testing.T) {
	cases := []struct {
		name    string    // modifier phrase under test
		grammar fsGrammar // legal F#, or a deliberate lenience probe
		decl    string    // the member line, indented 4 spaces inside a type
	}{
		// method-or-prop-defn, no modifier — the pre-existing working shapes.
		{"none, qualified", specLegal, `    member this.Target (a: int) = a`},
		{"none, underscore qualifier", specLegal, `    member _.Target (a: int) = a`},
		{"none, unqualified", specGrammarAdmits, `    member Target (a: int) = a`},

		// access modifier — mis-named `private`/`internal`/`public` before.
		{"private, qualified", specLegal, `    member private this.Target (a: int) = a`},
		{"internal, qualified", specLegal, `    member internal this.Target (a: int) = a`},
		{"public, qualified", specLegal, `    member public this.Target (a: int) = a`},
		{"private, unqualified", specGrammarAdmits, `    member private Target (a: int) = a`},

		// static — missed ENTIRELY before.
		{"static", specLegal, `    static member Target (a: int) = a`},
		{"static private", specLegal, `    static member private Target (a: int) = a`},
		{"static internal", specLegal, `    static member internal Target (a: int) = a`},
		{"static public", specLegal, `    static member public Target (a: int) = a`},
		{"static, generic params", specLegal, `    static member Target<'T> (a: 'T) = a`},
		{"static, no params", specLegal, `    static member Target = 0`},

		// override / default.
		{"override", specLegal, `    override this.Target (a: int) = a`},
		{"override private", specGrammarAdmits, `    override private this.Target (a: int) = a`},
		{"default", specLegal, `    default this.Target (a: int) = a`},
		{"default internal", specGrammarAdmits, `    default internal this.Target (a: int) = a`},

		// inline — admitted on members by the language reference (source 2
		// in this file's header) though § 8.13 omits it. Without `inline` in
		// the allowlist these rows capture the NAME `inline`, i.e. mode A
		// survives the rest of the fix untouched.
		{"inline, qualified", specLegal, `    member inline this.Target (x: int) = x + 1`},
		{"inline, unqualified", specLegal, `    member inline Target (x: int) = x + 1`},
		{"static inline", specLegal, `    static member inline Target (x: int) = x + 1`},
		{"override inline", specGrammarAdmits, `    override inline this.Target (x: int) = x + 1`},

		// auto-prop-defn: `val access? ident` — the name is the ident AFTER
		// `val`, and after the access modifier when one is present.
		{"val", specLegal, `    member val Target = 0`},
		{"val with get set", specLegal, `    member val Target = 0 with get, set`},
		{"val private", specLegal, `    member val private Target = 0 with get, set`},
		{"val internal with get", specLegal, `    member val internal Target = 0 with get`},
		{"static val", specLegal, `    static member val Target = 0 with get, set`},
		{"override val", specLegal, `    override val Target = 0 with get, set`},

		// Orders the grammar does NOT admit. Kept because they pin the
		// scanner's deliberate tolerance — it is a lenient scanner, not a
		// compiler, and ranking the orders could only create a way to LOSE a
		// real member. They are NOT a claim about the language.
		{"private val (reversed)", lenienceOnly, `    member private val Target = 0 with get, set`},
		{"internal val (reversed)", lenienceOnly, `    member internal val Target = 0 with get, set`},
		{"private public (repeated access)", lenienceOnly, `    member private public Target (a: int) = a`},
		// `inline` relative to `access`: no source attests either order on a
		// member (§ 8.13 omits `inline` entirely), so both are lenience only.
		{"inline private (unattested order)", lenienceOnly, `    member inline private this.Target (x: int) = x + 1`},
		{"private inline (unattested order)", lenienceOnly, `    member private inline this.Target (x: int) = x + 1`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "module M\n\ntype Holder() =\n" + tc.decl + "\n"
			ents := runFSharp(t, src, "Members.fs")
			names := fsMemberNames(ents)

			// Count first: a silently-missed declaration is an ABSENCE, and a
			// presence-only assertion cannot see one.
			if len(names) != 1 {
				t.Errorf("%q [%s] produced %d member entities, want 1: %v",
					tc.decl, tc.grammar, len(names), names)
			}
			if fsFindMember(ents, "Target") == nil {
				t.Errorf("no `member` SCOPE.Operation named \"Target\" for %q [%s]; member names = %v",
					tc.decl, tc.grammar, names)
			}
			// The complementary direction: recall alone cannot detect
			// over-firing, so no entity may be named after a modifier word.
			for _, bad := range fsMemberModifierWords {
				if e := fsFindMember(ents, bad); e != nil {
					t.Errorf("modifier %q recorded as the entity name for %q [%s] (line %d)",
						bad, tc.decl, tc.grammar, e.StartLine)
				}
			}
		})
	}
}

// TestMemberModifiers_StaticMemberSurface pins failure mode B at the scale it
// actually bites: `static member` is how F# expresses a type's factory and
// operator members, so a type can lose its whole public surface and read as
// having none. The assertion is the COUNT plus every name.
func TestMemberModifiers_StaticMemberSurface(t *testing.T) {
	src := `module M

type Money(amount: decimal) =
    member this.Amount = amount
    static member Zero = Money(0m)
    static member OfDecimal (d: decimal) = Money(d)
    static member private Scale (m: Money) (f: decimal) = Money(m.Amount * f)
    static member Combine (a: Money) (b: Money) = Money(a.Amount + b.Amount)
`
	ents := runFSharp(t, src, "Money.fs")
	names := fsMemberNames(ents)
	want := []string{"Amount", "Zero", "OfDecimal", "Scale", "Combine"}
	if len(names) != len(want) {
		t.Errorf("got %d member entities, want %d: %v", len(names), len(want), names)
	}
	for _, w := range want {
		if fsFindMember(ents, w) == nil {
			t.Errorf("static member %q not extracted; member names = %v", w, names)
		}
	}
}

// TestMemberModifiers_SameModifierNoCollapse pins the second consequence of
// the mis-name, the one a name-only reading leaves behind: `memberSeen` is
// keyed `indent + ":member:" + name`, exactly as `letSeen` is, so when two
// same-indent members are both misnamed `private` the second is DROPPED — one
// entity where there should be two.
func TestMemberModifiers_SameModifierNoCollapse(t *testing.T) {
	src := `module M

type Holder() =
    member private this.Alpha (a: int) = a + 1
    member private this.Beta (b: int) = b + 2
`
	ents := runFSharp(t, src, "Collapse.fs")
	names := fsMemberNames(ents)
	if len(names) != 2 {
		t.Errorf("two same-indent `member private` declarations produced %d member entities, want 2: %v",
			len(names), names)
	}
	for _, w := range []string{"Alpha", "Beta"} {
		if fsFindMember(ents, w) == nil {
			t.Errorf("missing member entity %q; member names = %v", w, names)
		}
	}
}

// TestMemberModifiers_ValCollapse is the same collapse for the auto-property
// form, where BOTH siblings were named `val`.
func TestMemberModifiers_ValCollapse(t *testing.T) {
	src := `module M

type Holder() =
    member val Alpha = 0 with get, set
    member val Beta = 1 with get, set
`
	ents := runFSharp(t, src, "ValCollapse.fs")
	names := fsMemberNames(ents)
	if len(names) != 2 {
		t.Errorf("two `member val` declarations produced %d member entities, want 2: %v",
			len(names), names)
	}
	for _, w := range []string{"Alpha", "Beta"} {
		if fsFindMember(ents, w) == nil {
			t.Errorf("missing member entity %q; member names = %v", w, names)
		}
	}
}

// TestMemberModifiers_ModifierPrefixedNames pins that a member name which
// merely BEGINS with a modifier word survives intact, and that six such
// members produce six entities rather than collapsing.
//
// What it does NOT control: it is not a control for the `\b` in the modifier
// group. This test PASSES UNCHANGED with that `\b` removed — measured, not
// assumed — because every repetition of the group is gated by a mandatory
// `\s+` and `Validate` supplies no whitespace after a `val` prefix, so the
// boundary never gets to matter. The guard that keeps these names is the
// separator, not the boundary. An earlier revision of this comment claimed
// the opposite and was false.
func TestMemberModifiers_ModifierPrefixedNames(t *testing.T) {
	src := `module M

type Holder() =
    member this.Validate (a: int) = a
    member this.PublicFacing (a: int) = a
    member this.PrivateKey (a: int) = a
    member this.InternalId (a: int) = a
    member this.StaticCache (a: int) = a
    member this.DefaultValue (a: int) = a
`
	ents := runFSharp(t, src, "Prefix.fs")
	want := []string{"Validate", "PublicFacing", "PrivateKey", "InternalId", "StaticCache", "DefaultValue"}
	names := fsMemberNames(ents)
	for _, w := range want {
		if fsFindMember(ents, w) == nil {
			t.Errorf("name %q lost to a modifier-prefix match; member names = %v", w, names)
		}
	}
	if len(names) != len(want) {
		t.Errorf("got %d member entities, want %d: %v", len(names), len(want), names)
	}
}

// TestMemberModifiers_NestedIndent varies the indentation axis the table holds
// constant, and pins that a modifier does not disturb the indent capture the
// dedup key and the type→member CONTAINS edges are built from.
func TestMemberModifiers_NestedIndent(t *testing.T) {
	src := `module M

module Inner =
    type Holder() =
        static member private Target (a: int) = a
`
	ents := runFSharp(t, src, "Nested.fs")
	e := fsFindMember(ents, "Target")
	if e == nil {
		t.Fatalf("deeply indented `static member private` not extracted; member names = %v",
			fsMemberNames(ents))
	}
	if e.StartLine != 5 {
		t.Errorf("start line = %d, want 5", e.StartLine)
	}
}

// TestMemberModifiers_Inline is the language reference's own example
// (MS Learn "Inline Functions", source 2 in this file's header) verbatim. It
// is BOTH failure modes at once against a fix that omits `inline`: each
// member is named `inline`, and because `memberSeen` is keyed
// indent+":member:"+name the two then collapse to a single entity. The count
// is what makes the collapse visible.
func TestMemberModifiers_Inline(t *testing.T) {
	src := `module M

type WrapInt32() =
    member inline this.incrementByOne(x) = x + 1
    static member inline Increment(x) = x + 1
`
	ents := runFSharp(t, src, "Inline.fs")
	names := fsMemberNames(ents)
	if len(names) != 2 {
		t.Errorf("two `member inline` declarations produced %d member entities, want 2: %v",
			len(names), names)
	}
	for _, w := range []string{"incrementByOne", "Increment"} {
		if fsFindMember(ents, w) == nil {
			t.Errorf("missing member entity %q; member names = %v", w, names)
		}
	}
	if e := fsFindMember(ents, "inline"); e != nil {
		t.Errorf("modifier \"inline\" recorded as the entity name (line %d)", e.StartLine)
	}
}

// TestMemberModifiers_StaticMemberGetsContainsEdge grades the FIRST of two
// downstream consequences of this widening, which would otherwise be
// unobserved. `memberRE` has three consumers: the member loop (extractor.go),
// the type→member CONTAINS scan in the type loop, and `detectCEBuilder` in
// compexpr_active_patterns.go. Recognising `static member` at all means the
// type loop now emits a CONTAINS edge for it — directionally right, and
// asserted here rather than assumed. (The second consequence, a
// `static member Bind`/`Return` type now qualifying as a CE builder, is
// graded by TestMemberModifiers_StaticCEBuilder below.)
func TestMemberModifiers_StaticMemberGetsContainsEdge(t *testing.T) {
	src := `module M

type Money(amount: decimal) =
    member this.Amount = amount
    static member Zero = Money(0m)
`
	ents := runFSharp(t, src, "MoneyEdges.fs")
	for _, name := range []string{"Amount", "Zero"} {
		ref := extractor.BuildOperationStructuralRef("fsharp", "MoneyEdges.fs", name)
		if !fsHasRel(ents, "Money", "SCOPE.Component", "CONTAINS", ref) {
			t.Errorf("type Money has no CONTAINS edge to member %q (ref %q)", name, ref)
		}
	}
}

// TestMemberModifiers_StaticCEBuilder grades the second downstream
// consequence: `detectCEBuilder` counts members through `memberRE`, so a
// builder whose protocol members are STATIC is now recognised where before
// the type looked memberless. Deliberate, and asserted rather than left as a
// side effect.
func TestMemberModifiers_StaticCEBuilder(t *testing.T) {
	src := `module Builders

type StaticOptionBuilder() =
    static member Bind(m, f) = Option.bind f m
    static member Return(x) = Some x
`
	ents := runFSharp(t, src, "staticbuilders.fs")
	for _, name := range []string{"Bind", "Return"} {
		op := fsFind(ents, name, "SCOPE.Operation")
		if op == nil {
			t.Fatalf("expected SCOPE.Operation %s", name)
		}
		if op.Subtype != "ce_member" {
			t.Errorf("%s subtype=%q, want ce_member (static CE protocol members "+
				"are now visible to detectCEBuilder)", name, op.Subtype)
		}
	}
}

// TestMemberModifiers_MandatorySeparator grades the PAIR of redundant guards
// in the modifier group — the mandatory `\s+` separator and the `\b` — which
// are individually ungraded because each masks the other.
//
// Measured, on this suite: removing `\b` alone is ALIVE (0 `--- FAIL`), and
// relaxing `\s+` to `\s*` alone is ALIVE (0). Neither is load-bearing by
// itself: with `\s+` intact, a glued `privateinternal` offers no whitespace
// for the second repetition; with `\b` intact, it offers no word boundary.
// Remove BOTH and the group walks straight through two glued modifier words
// and names the member after its first PARAMETER instead.
//
// The fixture is a static member, deliberately: static members take no
// self-identifier, so `static member <name> <param> = expr` is
// space-applied and unqualified — the only shape in which the two guards can
// diverge — and it is legal F# rather than merely grammar-admitted. The names
// glue two modifier words, which is unusual but a perfectly legal identifier;
// that is the shape the guards exist for.
func TestMemberModifiers_MandatorySeparator(t *testing.T) {
	src := `module M

type Holder() =
    static member privateinternal x = x
    static member valinline y = y
`
	ents := runFSharp(t, src, "Separator.fs")
	names := fsMemberNames(ents)
	if len(names) != 2 {
		t.Errorf("got %d member entities, want 2: %v", len(names), names)
	}
	for _, w := range []string{"privateinternal", "valinline"} {
		if fsFindMember(ents, w) == nil {
			t.Errorf("glued modifier-word name %q lost; member names = %v", w, names)
		}
	}
	for _, bad := range []string{"x", "y"} {
		if e := fsFindMember(ents, bad); e != nil {
			t.Errorf("parameter %q captured as the member name (line %d)", bad, e.StartLine)
		}
	}
}
