// issue7133_signature_order_test.go — the kotlin arm of #7133.
//
// buildClassSignature cut the declaration at the first "{" BEFORE stripping
// annotations, so a brace inside an annotation argument string truncated the
// declaration inside the literal and the emitted signature was EMPTY.
// Measured on 99bb52790, before the fix, over this exact fixture:
//
//	@Table(name = "{weird}") class Foo    ->  ""
//	@Table(name = "weird")   class Plain  ->  "class Plain"
//
// The cut leaves an unbalanced "(" in raw; stripKotlinAnnotations then scans
// for a matching ")", never finds one, and consumes the remainder to
// end-of-string. So the loss is total, not a trailing clause.
//
// # Kotlin has a PAIR, and unlike java (#7128) and C# (#7138) BOTH halves
// # were inverted
//
// The issue expected a single site. There are two: buildClassSignature
// (kotlin.go:1112) and buildFunSignature (kotlin.go:1087). In java and C# the
// sibling ran the strip first and carried a comment naming the hazard; kotlin's
// sibling has no such comment and ran its cuts first too. Its damage is worse
// in one respect and narrower in another:
//
//   - narrower on the brace: it cuts at " {" (space-brace), so the common
//     @Table(name = "{weird}") shape does not reach it — the character before
//     the brace is a quote. @Ann("a {b}") does. Pinned as spaceBraceFun.
//   - worse on the second delimiter: it also cuts at " =", which every named
//     annotation argument contains. @Table(name = "weird") fun topFun(...)
//     — no brace anywhere — recorded as "@Table(name" before this commit.
//     That is the majority shape for annotated Kotlin functions.
//
// So the pair audit did not merely find an ungraded-but-correct sibling this
// time; it found a second broken one. Both are reordered here, and the two
// halves are graded by separate tests with disjoint killers.
//
// # Ordering chosen, and why
//
// collapse -> strip -> cut, for both builders: the CUTS MOVED LAST rather than
// the strip moved first. That is the smaller change — it keeps
// stripKotlinAnnotations on the single-line, single-spaced input class it has
// always received — and, unlike the reverse, it is also the CORRECT one.
//
// The two orderings are NOT equivalent. stripKotlinAnnotations' alphabet is
// exactly '@', [A-Za-z0-9_], '(', ')' and the plain SPACE ' ' (read from the
// helper, not inferred from its name): after an annotation it skips a run of
// ' ' only. A NEWLINE after the annotation is not skipped, so stripping before
// the collapse leaves an interior space the collapse can no longer remove.
// Two ordinary-Kotlin shapes show it, and both are pinned below:
//
//	class TPNL<@Ann("{v}")\n    T>(val t: T)
//	  collapse-then-strip -> "class TPNL<T>(val t: T)"      (correct)
//	  strip-then-collapse -> "class TPNL< T>(val t: T)"     (mutant d)
//
//	class CtorNL(@Ann("{x}")\n    val a: String)
//	  collapse-then-strip -> "class CtorNL(val a: String)"
//	  strip-then-collapse -> "class CtorNL( val a: String)"
//
// In buildClassSignature that choice is GRADED: mutant (d), the collapse moved
// after the strip, is killed by CtorNL and TPNL. In buildFunSignature the
// analogous mutant (i) is ALIVE and is recorded here as a KNOWN HOLE, not as
// an equivalence. The two fun pipelines ARE distinguishable: enumerating
// prefixes over {@, A, (, ), space, newline, {, =, x, ", :} to length 4, kept
// to shapes where every '@' is followed by an identifier character, and
// appending a real function head, 48 of 35271 inputs differ — every one of
// them a character sitting IMMEDIATELY adjacent to the '@' with no whitespace
// and the annotation followed by a line break (`A@A\nfun g()` -> "Afun g()"
// vs "A fun g()"). No Kotlin source shape reaching this builder was found that
// produces one: `public@Ann("x")\nfun`, `@Ann("x")@B\nfun`, `@Ann("x")\npublic
// fun`, `@Ann("x")\n\tinline fun` and an annotated member were all measured
// byte-identical under mutant (i). So it is left UNGRADED ON PURPOSE for want
// of a reaching fixture, and deliberately NOT recorded as equivalent — a false
// equivalence would retire a real hole.
//
// # Grading
//
// Axis VARIED: whether the annotation argument's string literal contains '{'.
// Foo/Plain and BraceItems/PlainItems are pairs whose only DELIBERATE
// difference is the two brace characters. Their type names necessarily differ
// too — two types cannot share a name in one file — and that difference is
// inert: Plain carries the same annotation minus the braces and is green in
// BOTH orders, which is what makes these rows grade the ORDER rather than the
// presence of a non-empty string.
//
// Second axis, varied independently: declaration kind — class / interface /
// object / data class / enum class / sealed class / annotation class. All
// seven reach buildClassSignature; tree-sitter-kotlin maps interface, enum
// class and data class onto class_declaration and kotlin.go discriminates by
// child inspection, so there is no separate enum builder as there is in C#.
//
// Third axis, varied independently: annotation POSITION — on the type, stacked
// on the type, spanning a line break, on a constructor parameter, on a type
// parameter, on a nested type — and whether the declaration carries a
// supertype list.
//
// Held CONSTANT across the grid: the file, the package, the annotation types
// (Table/Ann), the argument shape (a single named String argument), the
// absence of modifiers other than the declaration keyword, the empty or
// one-member body, and the top-level enclosing scope.
//
// Rows that were ALREADY GREEN on 99bb52790 (Plain, PlainItems, Bare,
// TypeParamNoAnn, Table, Ann, ctrlFun, bareFun, exprBody, Holder.member) are
// non-regression controls: they must not redden when the order is swapped
// back.
//
// # Fixtures were NOT compiled
//
// There is no Kotlin compiler on this machine — `kotlinc` and `kotlin` are
// both absent (verified; `javac` IS present, but it does not compile .kt).
// Syntax was settled against the Kotlin language specification's syntax
// grammar and the official Kotlin documentation, and is cited where
// non-obvious:
//   - an annotation may be applied to a type parameter:
//     `typeParameter: typeParameterModifiers? simpleIdentifier (':' type)?`
//     with `typeParameterModifier: ... | annotation` (Kotlin specification,
//     "Syntax grammar" — typeParameter / typeParameterModifiers).
//   - an annotation may be applied to a constructor value parameter:
//     `classParameter: annotation* modifiers? ('val'|'var')? simpleIdentifier
//     ':' type ...` (same grammar, classParameter).
//   - `sealed`, `data`, `enum` and `annotation` are class modifiers, each
//     forming `<modifier> class Name` (kotlinlang.org — Classes, Sealed
//     classes, Data classes, Enum classes, Annotations).
//   - an annotation declared without an explicit `@Target` is applicable to
//     all targets except a small excluded set, so `@Target` is stated
//     explicitly here for the type-parameter and value-parameter uses
//     (kotlinlang.org — Annotations, "Usage").
//   - whitespace including a line terminator may separate an annotation from
//     what it annotates (Kotlin specification, "Syntax and grammar" —
//     whitespace is not significant between tokens except as a statement
//     separator).
//
// Everything asserted below is the extractor's own observed output, not a
// compiler's: every expectation is a transcription of a measured run, and
// every "before" value quoted in a comment was measured against the
// pre-change file rather than predicted.
package kotlin_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const kt7133ClassSrc = `package demo.sig

@Target(
    AnnotationTarget.CLASS,
    AnnotationTarget.FUNCTION,
    AnnotationTarget.VALUE_PARAMETER,
    AnnotationTarget.TYPE_PARAMETER)
annotation class Table(val name: String = "")

@Target(
    AnnotationTarget.CLASS,
    AnnotationTarget.FUNCTION,
    AnnotationTarget.VALUE_PARAMETER,
    AnnotationTarget.TYPE_PARAMETER)
annotation class Ann(val v: String = "")

@Table(name = "{weird}") class Foo

@Table(name = "weird") class Plain

@Table(name = "{w}") class BraceItems

@Table(name = "w") class PlainItems

@Table(name = "{w}") interface IFace

@Table(name = "{w}") object Obj

@Table(name = "{w}") data class Data(val a: Int)

@Table(name = "{w}") enum class Colors { RED, GREEN }

@Table(name = "{w}") sealed class Sealed

@Table(name = "{w}") annotation class AnnoCls

@Table(name = "{w}") class WithBase : Any()

@Ann @Table(name = "{w}") class Stacked

@Table(
    name = "{split}")
class MultiLine

class CtorParam(@Ann("{x}") val a: String)

class CtorNL(@Ann("{x}")
    val a: String)

class TP<@Ann("{v}") T>(val t: T)

class TPNL<@Ann("{v}")
    T>(val t: T)

class TypeParamNoAnn<T>(val t: T)

@Table(name = "{w}") class Outer {
    @Table(name = "{w}") class Inner
}

class Bare
`

// The class half. Every row is the Signature the extractor emits for one
// SCOPE.Component. The "before" values are transcriptions of a run of this
// exact fixture against kotlin.go as of 99bb52790.
func TestKotlin7133_ClassSignatureStripsAnnotationsBeforeBraceCut(t *testing.T) {
	recs := extractKotlinRecords(t, kt7133ClassSrc, "demo/sig/Classes.kt")
	for _, want := range []struct{ name, sig string }{
		// The reported shape. "" before.
		{"Foo", "class Foo"},
		// Its control — differs from Foo only in the two brace characters.
		// Green before AND after; this is what makes Foo grade the order.
		{"Plain", "class Plain"},
		// The same pair again at minimal width, so neither row can be green
		// for a reason specific to the word "weird".
		{"BraceItems", "class BraceItems"},
		{"PlainItems", "class PlainItems"},
		// Declaration-kind axis. All six emitted "" before.
		{"IFace", "interface IFace"},
		{"Obj", "object Obj"},
		{"Data", "data class Data(val a: Int)"},
		{"Colors", "enum class Colors"},
		{"Sealed", "sealed class Sealed"},
		{"AnnoCls", "annotation class AnnoCls"},
		// Brace in the annotation AND a real supertype list. The supertype
		// list is KEPT — kotlin's builder has no equivalent of the C# " :"
		// base-list cut, so this row also pins that there is no second
		// truncation delimiter to interact with. "" before.
		{"WithBase", "class WithBase : Any()"},
		// Two annotations, brace in the second. "" before.
		{"Stacked", "class Stacked"},
		// The annotation spans a line break. "" before. Note this row does
		// NOT grade collapse-before-strip: the break falls inside the
		// annotation's parentheses. TPNL and CtorNL below grade that.
		{"MultiLine", "class MultiLine"},
		// Annotation POSITION: on a constructor value parameter, not the
		// type. Truncated rather than emptied before: "class CtorParam(".
		{"CtorParam", "class CtorParam(val a: String)"},
		// The same shape with a NEWLINE after the annotation's ")" instead of
		// a space. THIS is the row that grades the ordering CHOICE: under
		// mutant (d) — collapse moved after the strip — stripKotlinAnnotations'
		// trailing-SPACE skip does not fire on a newline and this emits
		// "class CtorNL( val a: String)". Before the fix it was "class CtorNL(".
		{"CtorNL", "class CtorNL(val a: String)"},
		// Annotation position: on a type parameter. "class TP<" before.
		{"TP", "class TP<T>(val t: T)"},
		// The second ordering counter-example, on the type-parameter shape.
		// Under mutant (d) this emits "class TPNL< T>(val t: T)".
		// "class TPNL<" before.
		{"TPNL", "class TPNL<T>(val t: T)"},
		// Control for the two rows above: the same generic shape with no
		// annotation at all. Green before AND after.
		{"TypeParamNoAnn", "class TypeParamNoAnn<T>(val t: T)"},
		// Annotation position: on a nested type. Both emitted "" before —
		// the outer because its node span contains the inner annotation too.
		{"Outer", "class Outer"},
		{"Inner", "class Inner"},
		// Non-regression controls: no annotation at all, and the two
		// annotation declarations themselves (whose own @Target argument
		// lists span line breaks).
		{"Bare", "class Bare"},
		{"Table", "annotation class Table(val name: String = \"\")"},
		{"Ann", "annotation class Ann(val v: String = \"\")"},
	} {
		got := kt7133Sigs(recs, "SCOPE.Component", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Component named %q, got %d", want.name, len(got))
			continue
		}
		if got[0] != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0], want.sig)
		}
	}
}

const kt7133FunSrc = `package demo.sig

annotation class Table(val name: String = "")
annotation class Ann(val v: String = "")

@Table(name = "{weird}") fun topFun(a: Int): Int { return a }

@Table(name = "weird") fun plainFun(a: Int): Int { return a }

@Ann("a {b}") fun spaceBraceFun(a: Int): Int { return a }

@Ann("ab") fun ctrlFun(a: Int): Int { return a }

fun bareFun(a: Int): Int { return a }

fun exprBody(a: Int): Int = a

@Ann("{x}") fun exprAnn(a: Int): Int = a

class Holder {
    @Ann("{x}") fun member(b: String): String { return b }

    @Table(name = "{w}") fun namedArg(b: String): String { return b }
}
`

// The SIBLING half of the pair. In java and C# this half was already correct
// and merely ungraded; in kotlin it was ALSO inverted, so this test both fixes
// and grades it. Scored separately from the class test: with this test file
// held out, swapping buildFunSignature's statements back on the FIXED tree
// left ./internal/extractors/kotlin/ green (0 "--- FAIL").
func TestKotlin7133_FunSignatureStripsAnnotationsBeforeBodyCuts(t *testing.T) {
	recs := extractKotlinRecords(t, kt7133FunSrc, "demo/sig/Funs.kt")
	for _, want := range []struct{ name, sig string }{
		// Brace AND " =" inside the annotation. "@Table(name" before.
		{"topFun", "fun topFun(a: Int): Int"},
		// NO brace anywhere — and still broken before, because every named
		// annotation argument carries a " =" and the " =" cut ran first.
		// "@Table(name" before. This row is the reason the fun half is a
		// defect and not just an ungraded sibling.
		{"plainFun", "fun plainFun(a: Int): Int"},
		// A brace preceded by a SPACE inside the annotation string, which is
		// what the " {" cut actually matches. "@Ann(\"a" before.
		{"spaceBraceFun", "fun spaceBraceFun(a: Int): Int"},
		// Its control: same annotation, no brace and no " =". Green before
		// AND after — so the three rows above grade the ORDER, not the
		// presence of a signature.
		{"ctrlFun", "fun ctrlFun(a: Int): Int"},
		// No annotation at all. Green before and after.
		{"bareFun", "fun bareFun(a: Int): Int"},
		// Expression body: the " =" cut is a real delimiter and must keep
		// running AFTER the strip. Green before and after.
		{"exprBody", "fun exprBody(a: Int): Int"},
		// Expression body WITH a brace-carrying annotation: both the strip
		// and the " =" cut must fire, in that order. Measured green BEFORE
		// as well as after — the brace in @Ann("{x}") is preceded by a quote,
		// so the " {" cut does not match it, and the annotation carries no
		// " =" either. A control, not a defect row.
		{"exprAnn", "fun exprAnn(a: Int): Int"},
		// Member function, annotation with an unspaced brace: green before
		// (neither " {" nor " =" appears inside @Ann("{x}")).
		{"Holder.member", "fun member(b: String): String"},
		// Member function with a NAMED annotation argument: the " =" cut
		// fired inside it. "@Table(name" before.
		{"Holder.namedArg", "fun namedArg(b: String): String"},
	} {
		got := kt7133Sigs(recs, "SCOPE.Operation", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Operation named %q, got %d", want.name, len(got))
			continue
		}
		if got[0] != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0], want.sig)
		}
	}
}

// DISCLOSED REMAINDERS — shapes this commit does NOT fix, pinned here so they
// are visible rather than silently absent. Every expectation below was
// measured to be BYTE-IDENTICAL before and after the reorder; none of them is
// caused by the ordering, and each needs its own decision.
//
//  1. A '{' in a string or char literal in the declaration HEADER (a default
//     value). No annotation is involved and no reordering can help: the brace
//     is in the text the signature is supposed to keep. Same remainder the C#
//     arm disclosed.
//  2. A comment BETWEEN the annotation and the declaration keyword. A KDoc or
//     line comment PRECEDING the whole declaration is outside the node span
//     and is therefore structurally unreachable — verified here for kotlin
//     rather than carried over from java/C# (KDocOnly, LineOnly below), and
//     the verdict matches. But a comment sitting after an annotation IS
//     inside the span, because the annotation is, so `{` in such a comment
//     does reach the cut. That is a kotlin-specific difference from the other
//     two arms.
//  3. A use-site-targeted annotation (`@get:Ann(...)`, `@field:`, `@param:`).
//     stripKotlinAnnotations skips '@' then [A-Za-z0-9_] and then expects
//     '(' — a ':' ends the identifier scan and the helper falls through, so
//     the target prefix is consumed but the rest of the annotation is
//     emitted verbatim. A separate defect in the helper, not in the order.
//  4. The " =" cut in buildFunSignature firing on a DEFAULT PARAMETER VALUE.
//     Also annotation-independent and also unaffected by the order.
func TestKotlin7133_DisclosedRemainders(t *testing.T) {
	const src = `package demo.sig

annotation class Ann(val v: String = "")

class DefStr(val s: String = "{x}")

class DefChar(val c: Char = '{')

@Ann("x") /* { */ class BlockCmt

@Ann("x") // { here
class LineCmt

/** doc { brace */
class KDocOnly

// line { comment
class LineOnly

@get:Ann("{w}") class UseSite

fun defaultArg(a: Int = 3): Int { return a }
`
	recs := extractKotlinRecords(t, src, "demo/sig/Remainders.kt")
	for _, want := range []struct{ kind, name, sig string }{
		// (1) brace inside a header string / char literal — truncated.
		{"SCOPE.Component", "DefStr", "class DefStr(val s: String = \""},
		{"SCOPE.Component", "DefChar", "class DefChar(val c: Char = '"},
		// (2) comment after the annotation: inside the node span, so it is
		// reachable — unlike java and C#, where the analogous shape is not.
		{"SCOPE.Component", "BlockCmt", "/*"},
		{"SCOPE.Component", "LineCmt", "//"},
		// (2, controls) a doc or line comment PRECEDING the declaration with
		// no annotation between is OUTSIDE the node span: unreachable, and
		// these two rows are the demonstration.
		{"SCOPE.Component", "KDocOnly", "class KDocOnly"},
		{"SCOPE.Component", "LineOnly", "class LineOnly"},
		// (3) use-site target: the helper does not recognise "@get:".
		{"SCOPE.Component", "UseSite", ":Ann(\""},
		// (4) the " =" cut on a default parameter value.
		{"SCOPE.Operation", "defaultArg", "fun defaultArg(a: Int"},
	} {
		got := kt7133Sigs(recs, want.kind, want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 %s named %q, got %d", want.kind, want.name, len(got))
			continue
		}
		if got[0] != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0], want.sig)
		}
	}
}

// kt7133Sigs returns the Signature of every record with this kind and name.
// It returns a slice, not a string, so a duplicate or missing entity is
// reported as such instead of silently matching the first row.
func kt7133Sigs(recs []types.EntityRecord, kind, name string) []string {
	var out []string
	for _, r := range recs {
		if r.Kind == kind && r.Name == name {
			out = append(out, r.Signature)
		}
	}
	return out
}
