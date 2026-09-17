// issue7133_class_signature_order_test.go — buildClassSignature cut at the
// body brace BEFORE stripping attributes, so a '{' inside a type-level
// attribute's string literal truncated the declaration inside the literal and
// the emitted signature was EMPTY (#7133, C# arm).
//
// Measured on e4dee929e, before the fix (probe over this exact fixture):
//
//	[Route("api/{version}/items")] public class ItemsController -> ""
//	[Route("api/v1/items")]        public class PlainController -> "public class PlainController"
//
// The brace cut leaves an unbalanced "[" in raw; stripCSharpAttributes then
// scans for a matching "]", finds none, and swallows the remainder to
// end-of-string. So the loss is total, not a trailing clause — which is why
// this is worse than the java case (#7124) it mirrors, where the signature
// merely lost its tail.
//
// The sibling buildMethodSignature (csharp.go:1805) already runs
// stripCSharpAttributeArgs first and carries a comment naming this exact
// hazard ([HttpGet("{id}")] → [HttpGet]). The two builders use DIFFERENT
// helpers, so this is not a copy of the java fix: the class builder strips
// attributes entirely, the method builder strips only their arguments.
//
// # Ordering chosen
//
// collapse → strip → brace cut → " :" cut, i.e. the BRACE CUT MOVED LAST
// rather than the strip moved first. Moving the strip to the front would also
// move it before `strings.Join(strings.Fields(raw), " ")`, handing
// stripCSharpAttributes raw multi-line source with original indentation — an
// input class it has never received.
//
// collapse-first is chosen because it is CORRECT, not because the two
// orderings are indistinguishable. They are not. stripCSharpAttributes skips
// the run of plain SPACES following the closing "]"; when what follows "]" is
// a NEWLINE instead, that skip does not fire, and stripping before the
// collapse therefore leaves an interior space the collapse can no longer
// remove. Two ordinary-C# shapes show it, and both are pinned below
// (TPAttrNewline, PersonRec):
//
//	public class TPAttrNewline<[Route("{v}")]\n        T>
//	  collapse-then-strip -> "public class TPAttrNewline<T>"     (correct)
//	  strip-then-collapse -> "public class TPAttrNewline< T>"    (mutant d)
//
//	public record PersonRec([property: JsonPropertyName("first_{n}ame")]\n        string First, string Last)
//	  collapse-then-strip -> "public record PersonRec(string First, string Last)"
//	  strip-then-collapse -> "public record PersonRec( string First, string Last)"
//
// That is the ONLY ordering constraint in the function. The brace cut and the
// " :" base-list cut are MUTUALLY ORDER-INDEPENDENT -- both truncate to a
// PREFIX, so either order yields the prefix ending at the earlier of the two
// positions. Swapping THOSE two is an ALIVE mutant, and it is left untested on
// purpose: it is equivalent, not ungraded, enumerated over the alphabet "{",
// " :", ":", "}", "<", ">", space and a letter for all strings to length 5 --
// 37449 inputs, 0 differing. A fixture asserting that order would grade
// nothing.
//
// # Grading
//
// Axis VARIED: whether the attribute argument's string literal contains '{'.
// ItemsController/PlainController and BraceItems/PlainItems are pairs whose
// only DELIBERATE difference is the two brace characters in the route
// template. Their type names differ too, necessarily — two types cannot share
// a name in one file — and that difference is inert here: Plain (no attribute
// at all) shows a distinct name emits a correct signature on its own.
// Held CONSTANT across each pair: the file, the namespace, the attribute type,
// the attribute's argument shape, the modifiers, the declaration keyword, the
// type name length-independent identity, the body, and the enclosing scope.
//
// Second axis, varied independently of the first: the declaration kind —
// class / interface / record / struct — plus attribute POSITION (on the type,
// stacked on the type, on a type parameter, on a nested type) and whether the
// declaration carries a base list. enum is NOT in the grid because
// enum_declaration takes buildEnumEntity, a different builder that packs the
// member list into Signature and never calls buildClassSignature.
//
// The rows that were already green on e4dee929e (PlainController, PlainItems,
// ColonOnly, Plain, RouteAttribute) are non-regression controls for the
// reorder: they must NOT redden when the order is swapped back, which is what
// makes the defect rows grade the ORDER rather than merely the presence of a
// non-empty signature.
//
// # Fixtures were NOT compiled
//
// There is no C# compiler on this machine (csc, dotnet, mono, mcs all absent —
// verified). Syntax was settled against the ECMA-334 6th edition grammar and
// the Microsoft C# reference, and is cited where non-obvious:
//   - attribute on a type parameter is grammatical: `type_parameter ::=
//     attributes? identifier` (ECMA-334 §15.2.3, "Type parameters"; see also
//     learn.microsoft.com "Attributes" → attribute targets, which lists
//     `AttributeTargets.GenericParameter`). AttributeUsage(AttributeTargets.All)
//     below permits it.
//   - a record declaration may carry a positional parameter list AND a body:
//     `record_declaration ::= ... identifier type_parameter_list?
//     parameter_list? record_base? record_body` (Microsoft C# 9 records
//     specification, "Record declarations").
//   - a comment is trivia and may sit between an attribute and the
//     declaration keyword (ECMA-334 §6.3.3).
//   - `[property: ...]` on a record positional parameter is grammatical:
//     `attribute_section ::= "[" attribute_target_specifier? attribute_list
//     "]"`, and `property` is an attribute target (ECMA-334 §22.3,
//     "Attribute specification"). The C# 9 records specification names this
//     target explicitly for positional parameters — it is the conventional
//     placement for System.Text.Json attributes on a record.
//   - a line break may appear anywhere whitespace may (ECMA-334 §6.3.1), so
//     an attribute section may be followed by a newline rather than a space.
//
// Everything asserted here is the extractor's own observed output, not a
// compiler's — the expectations are transcriptions of a measured run.
package csharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const cs7133ClassSrc = `using System;

namespace Api.Sig
{
    [AttributeUsage(AttributeTargets.All, AllowMultiple = true)]
    public sealed class RouteAttribute : Attribute
    {
        public RouteAttribute(string template) { }
    }

    [AttributeUsage(AttributeTargets.All)]
    public sealed class DisplayAttribute : Attribute
    {
        public string Name { get; set; }
    }

    [Route("api/{version}/items")]
    public class ItemsController { }

    [Route("api/v1/items")]
    public class PlainController { }

    [Route("api/{v}/items")]
    public class BraceItems { }

    [Route("api/v/items")]
    public class PlainItems { }

    [Route("api/{v}/repo")]
    public interface IRepo { }

    [Route("api/{v}/rec")]
    public record ItemRec(int Id) { }

    [Route("api/{v}/pt")]
    public struct Point { public int X; }

    [Route("api/{v}/based")]
    public class Based : Object, IDisposable { public void Dispose() { } }

    [Route(
        "api/{v}/split")]
    public class MultiLine { }

    [Obsolete]
    [Route("api/{v}/stacked")]
    public class Stacked { }

    public class TypeParamAttr<[Route("{v}")] T> { }

    public class TPAttrNewline<[Route("{v}")]
        T> { }

    public record PersonRec([property: Route("first_{n}ame")]
        string First, string Last) { }

    [Route("api/{v}/outer")]
    public class Outer
    {
        [Route("api/{v}/inner")]
        public class Inner { }
    }

    public class Plain { }
}
`

// The brace path. Every row is the Signature the extractor emits for one
// SCOPE.Component; the defect rows all emitted "" on e4dee929e.
func TestCSharp7133_ClassSignatureStripsAttributesBeforeBraceCut(t *testing.T) {
	recs := csExtract(t, cs7133ClassSrc, "Api/Sig/Items.cs")
	for _, want := range []struct{ name, sig string }{
		// The reported shape: the canonical ASP.NET route template. "" before.
		{"ItemsController", "public class ItemsController"},
		// Its control — differs from ItemsController only in the two brace
		// characters of the route template. Green before AND after.
		{"PlainController", "public class PlainController"},
		// The same pair again at minimal width, so neither row can be green
		// for a reason specific to the word "version".
		{"BraceItems", "public class BraceItems"},
		{"PlainItems", "public class PlainItems"},
		// Declaration-kind axis. All three emitted "" before.
		{"IRepo", "public interface IRepo"},
		{"ItemRec", "public record ItemRec(int Id)"},
		{"Point", "public struct Point"},
		// Brace in the attribute AND a real base list: the base list must
		// still be dropped, so the " :" cut must still run after the brace
		// cut. "" before.
		{"Based", "public class Based"},
		// The attribute spans a line break — the input class #7133 asked
		// about. "" before. Note this row does NOT by itself grade
		// collapse-before-strip: the line break here falls INSIDE the
		// attribute's parentheses, and the text after "]" is what the
		// orderings disagree about. TPAttrNewline and PersonRec below are the
		// rows that grade it.
		{"MultiLine", "public class MultiLine"},
		// Two attribute lists, brace in the second. "" before.
		{"Stacked", "public class Stacked"},
		// Attribute POSITION: on a type parameter, not on the type. This one
		// was truncated rather than emptied before ("public class TypeParamAttr<"),
		// because the brace cut landed after the "<".
		{"TypeParamAttr", "public class TypeParamAttr<T>"},
		// The same shape with a NEWLINE after the attribute's "]" instead of
		// a space. This is the row that grades the ORDERING CHOICE: under
		// mutant d (collapse moved after the strip) stripCSharpAttributes'
		// trailing-SPACE skip does not fire on a newline, and the signature
		// comes out "public class TPAttrNewline< T>". Before the fix this row
		// was "public class TPAttrNewline<" — truncated at the brace inside
		// the route template, like TypeParamAttr above, not emptied.
		{"TPAttrNewline", "public class TPAttrNewline<T>"},
		// The second ordering counter-example, on a shape that needs no
		// generics: an attribute with an explicit `property:` target on a
		// record positional parameter, written on its own line — the way
		// System.Text.Json attributes are conventionally placed. Under mutant
		// d this emits "public record PersonRec( string First, string Last)".
		// Before the fix this row was "public record PersonRec(".
		{"PersonRec", "public record PersonRec(string First, string Last)"},
		// Attribute position: on a nested type. Both the outer and the inner
		// declaration emitted "" before — the outer because its node span
		// contains the inner attribute too.
		{"Outer", "public class Outer"},
		{"Inner", "public class Inner"},
		// Non-regression controls: no attribute at all, and an attribute with
		// no brace on a type that HAS a base list.
		{"Plain", "public class Plain"},
		{"RouteAttribute", "public sealed class RouteAttribute"},
		{"DisplayAttribute", "public sealed class DisplayAttribute"},
	} {
		got := cs7133Components(recs, want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Component named %q, got %d", want.name, len(got))
			continue
		}
		if got[0] != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0], want.sig)
		}
	}
}

// The " :" base-list cut is a SECOND, independent truncation delimiter in
// buildClassSignature, and #7133 asked whether a partial attribute left by the
// brace cut could reach it. Traced step by step on e4dee929e it CANNOT:
//
//	raw            "[Display(Name = \"{x} : b\")] public class BraceColon { }"
//	after braceCut "[Display(Name = \""            (FIRED)
//	after collapse "[Display(Name = \""
//	after strip    ""                              <- unbalanced "[" swallowed
//	after colonCut ""                              (no " :" left to find)
//
// So in the broken order the brace cut always wins and the " :" cut is handed
// an empty string; the hypothesised second path is unreachable. The " :" cut
// is nonetheless graded here in both directions, separately from the brace
// grid above, because the fix moves the brace cut past the attribute strip and
// could have changed which delimiter fires.
func TestCSharp7133_ColonInAttributeArgumentIsNotTheDelimiter(t *testing.T) {
	const src = `using System;

namespace Api.Sig
{
    [Display(Name = "{x} : b")]
    public class BraceColon { }

    [Display(Name = "a : b")]
    public class ColonOnly { }

    [Display(Name = "a : b")]
    public class ColonAndBase : Object { }

    public class BareBase : Object { }
}
`
	recs := csExtract(t, src, "Api/Sig/Colon.cs")
	for _, want := range []struct{ name, sig string }{
		// Colon AND brace inside one attribute argument. "" before the fix,
		// and the trace above shows the brace cut — not the " :" cut — is
		// what emptied it.
		{"BraceColon", "public class BraceColon"},
		// Colon inside an attribute argument, no brace. Already green before
		// the fix: the attribute is stripped before the " :" cut in BOTH
		// orders, so a colon in an attribute has never been a delimiter.
		{"ColonOnly", "public class ColonOnly"},
		// Colon in an attribute argument AND a real base list: the real base
		// list is still dropped, and the attribute's colon is not what gets
		// cut at.
		{"ColonAndBase", "public class ColonAndBase"},
		// The " :" cut must still do its job with no attribute in play.
		{"BareBase", "public class BareBase"},
	} {
		got := cs7133Components(recs, want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Component named %q, got %d", want.name, len(got))
			continue
		}
		if got[0] != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0], want.sig)
		}
	}
}

// The SIBLING half of the pair. buildMethodSignature already ran
// stripCSharpAttributeArgs before its brace cut and already documented why —
// and #7128's pair audit found that on the java twin nothing observed the
// correct sibling's order, so it was free to regress silently. Scored here:
// swapping buildMethodSignature's two statements back on the FIXED tree, with
// this test held out, left ./internal/extractors/csharp/ green (0 "--- FAIL").
// So the C# method half was ungraded too, and this test is its killer. It is
// test-only — buildMethodSignature is NOT changed by this commit.
func TestCSharp7133_MethodSignatureStripsAttributeArgsBeforeBraceCut(t *testing.T) {
	const src = `using System;

namespace Api.Sig
{
    public class Repo
    {
        [HttpDelete("items/{id}")]
        public void Remove(string id) { }

        [HttpDelete("items/all")]
        public void RemoveAll(string id) { }

        [HttpGet("items/{id}")]
        public string Get(string id) => id;
    }
}
`
	recs := csExtract(t, src, "Api/Sig/Repo.cs")
	for _, want := range []struct{ name, sig string }{
		// The brace lives inside the attribute's string literal. With the
		// order swapped this loses the whole declaration.
		{"Repo.Remove", "[HttpDelete] public void Remove(string id)"},
		// Its control: same attribute, same declaration, no brace in the
		// literal. Green in both orders.
		{"Repo.RemoveAll", "[HttpDelete] public void RemoveAll(string id)"},
		// Expression-bodied member: the "=>" cut is a third delimiter in the
		// method builder and must keep running after the brace cut.
		{"Repo.Get", "[HttpGet] public string Get(string id)"},
	} {
		got := cs7133Operations(recs, want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Operation named %q, got %d", want.name, len(got))
			continue
		}
		if got[0] != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0], want.sig)
		}
	}
}

// cs7133Components returns the Signature of every SCOPE.Component with this
// name. It returns a slice, not a string, so a duplicate or missing entity is
// reported as such instead of silently matching the first row.
func cs7133Components(recs []types.EntityRecord, name string) []string {
	var out []string
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == name {
			out = append(out, r.Signature)
		}
	}
	return out
}

func cs7133Operations(recs []types.EntityRecord, name string) []string {
	var out []string
	for _, r := range recs {
		if r.Kind == "SCOPE.Operation" && r.Name == name {
			out = append(out, r.Signature)
		}
	}
	return out
}
