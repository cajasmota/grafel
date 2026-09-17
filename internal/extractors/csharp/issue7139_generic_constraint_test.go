// issue7139_generic_constraint_test.go — buildClassSignature's " :" base-list
// cut also fired on the colon of a GENERIC CONSTRAINT clause, truncating the
// signature mid-clause and leaving a dangling `where T` (#7139, C# arm).
//
// Measured on c1e2c3008, before the fix, over this exact fixture:
//
//	public class Gen<T> where T : class        -> "public class Gen<T> where T"
//	public class TwoClause<T, U> where T : class where U : struct
//	                                           -> "public class TwoClause<T, U> where T"
//	public record RecGen<T>(T Value) where T : class
//	                                           -> "public record RecGen<T>(T Value) where T"
//
// No attribute and no brace are involved, so this is not the #7133 hazard: the
// output is TRUNCATED rather than emptied, which is why it survived #7138 —
// the signature still names the class and a reader has to notice the trailing
// `where T` is meaningless.
//
// # Grammar (ECMA-334 6th edition, 2017 — the edition #7138 cited)
//
//   - §15.2.1 (Class declarations, General) gives
//     `class_declaration ::= attributes? class_modifier* 'partial'? 'class'
//     identifier type_parameter_list? class_base?
//     type_parameter_constraints_clause* class_body ';'?`.
//     The BASE LIST PRECEDES the constraint clauses, and there is exactly one
//     class_base. So when a header has both, the FIRST " :" is the base-list
//     colon and cutting at it is correct — which is why `Both` and `BothTwo`
//     below were already green. The defect is only that the cut also fires
//     when there is NO class_base at all.
//   - §15.2.5 (Type parameter constraints) gives
//     `type_parameter_constraints_clause ::= 'where' type_parameter ':'
//     type_parameter_constraints`, i.e. every clause contains a colon, and a
//     declaration may carry several clauses (`*` above).
//   - §15.2.3 (Type parameters) gives `type_parameter ::= attributes?
//     identifier` and `type_parameter_list ::= '<' type_parameters '>'`. A
//     colon can therefore reach the inside of the `<...>` list only through an
//     attribute (§22.3's attribute_target_specifier ::= identifier ':'), and
//     attributes are stripped from `raw` before either cut runs. TPGen and
//     TgtSpace pin that, the latter with a SPACE before the target colon,
//     which §6.3.1 permits between any two tokens.
//   - §6.4.4 (Keywords): `where` is a CONTEXTUAL keyword, not reserved, so
//     `public class where : Object` is a legal declaration whose base list
//     must still be dropped. The `where`/`whereGen` rows pin it.
//   - §6.4.3 (Identifiers): a type_parameter may be `@`-prefixed and may
//     contain Unicode letters, which is why the clause matcher's identifier
//     class is `@?[\p{L}_][\p{L}\p{N}_]*` rather than ASCII.
//
// # Fixtures were NOT compiled
//
// There is no C# compiler on this machine (csc, dotnet, mono, mcs all absent —
// verified again here). Every section number above is derived-not-executed;
// every EXPECTATION below is a transcription of the extractor's own measured
// output, not a compiler's.
//
// # Grading — axes
//
// VARIED, independently:
//   - whether the header has a base list, a constraint clause, BOTH, or
//     neither (BaseOnly / Gen / Both / Plain7139) — the axis the defect lives
//     on. The base-list-only and both rows are what stop the fix from being a
//     widening that abandons the base-list cut.
//   - number of constraint clauses (1: Gen, 2: TwoClause, DictLike).
//   - number of type parameters (1: Gen, 2: TwoClause, DictLike).
//   - constraint kind: class / interface / new() / notnull / struct /
//     self-referential generic (GenNew, GenNotNull, GenSelf, StrGen).
//   - whitespace around the clause colon and before `where` (NoSpaceCons,
//     WhereNewline, TightWhere).
//   - declaration kind: class / interface / record / struct.
//   - attribute present, on the type and on a type parameter (AttrGen, TPGen).
//   - whether the constraint TYPE is alias-qualified, and whether the `::`
//     carries surrounding whitespace (AliasCons, AliasTight, AliasBase).
//   - a `where`-like token that is NOT a clause: inside a string literal
//     (LitWhere, LitWhereColon), as the class NAME (`where`, `whereGen`), and as the
//     suffix of an identifier (Nowhere).
//
// HELD CONSTANT across the grid: the file, the namespace, the enclosing scope,
// the accessibility modifier (`public` on every row), the body (`{ }` or a
// one-statement body), and the base type where one appears (`Object`).
//
// Nothing here grades the brace cut or the attribute strip — #7133's file owns
// those, and the two cuts remain MUTUALLY ORDER-INDEPENDENT after this change:
// the base-list cut still only ever truncates `raw` to a prefix. This change
// alters WHETHER it fires, never what it produces when it does, so the comment
// in csharp.go claiming independence stays true (re-enumerated: see the PR).
package csharp_test

import "testing"

const cs7139Src = `using System;

namespace Api.Sig
{
    public class BaseOnly : Object { }

    public class BaseTwo : Object, IDisposable { public void Dispose() { } }

    public class Gen<T> where T : class { }

    public class GenIface<T> where T : IDisposable { }

    public class GenNew<T> where T : new() { }

    public class GenNotNull<TKey> where TKey : notnull { }

    public class TwoClause<T, U> where T : class where U : struct { }

    public class DictLike<TKey, TValue> where TKey : notnull where TValue : class { }

    public class GenSelf<T> where T : IEquatable<T> { }

    public class Both<T> : Object where T : class { }

    public class BothTwo<T, U> : Object, IDisposable where T : class where U : struct { public void Dispose() { } }

    public class GenBase<T> : System.Collections.Generic.List<T> where T : class { }

    public class NoSpaceCons<T> where T: class { }

    public class NoSpaceBase: Object { }

    public class WhereNewline<T>
        where T : class { }

    public class TightWhere<T>where T : class { }

    public interface IGen<T> where T : class { }

    public record RecGen<T>(T Value) where T : class { }

    public struct StrGen<T> where T : struct { }

    [Obsolete]
    public class AttrGen<T> where T : class { }

    public class TPGen<[Obsolete] T> where T : class { }

    public record TgtSpace([property : Obsolete] string First, string Last) { }

    public record LitColon<T>(string S = " : x") where T : class { }

    public record LitWhere<T>(string S = "a where b") where T : class { }

    public record LitWhereColon<T>(string S = "a where b") : Object where T : class { }

    public class Nowhere<T> : Object where T : class { }

    public class AliasCons<T> where T: global :: System.IDisposable { }

    public class AliasTight<T> where T: global::System.IDisposable { }

    public class AliasBase<T> : global :: System.Object where T : class { }

    public class Plain7139 { }
}
`

// `where` used as an identifier — legal, because it is a contextual keyword
// (§6.4.4). Kept in its own source because a type may not be named twice in
// one namespace and `where`/`where2` would otherwise crowd the main grid.
const cs7139WhereNameSrc = `using System;

namespace Api.Sig
{
    public class where : Object { }

    public class whereGen<T> : Object { }
}
`

func TestCSharp7139_GenericConstraintIsNotTheBaseListDelimiter(t *testing.T) {
	recs := csExtract(t, cs7139Src, "Api/Sig/Constraints.cs")
	for _, want := range []struct{ name, sig string }{
		// ---- base list only: the cut MUST still fire. Green before and
		// after; these are the rows that redden if the fix is a widening.
		{"BaseOnly", "public class BaseOnly"},
		{"BaseTwo", "public class BaseTwo"},

		// ---- constraint only: the reported defect. All of these were
		// truncated mid-clause before ("... where T", "... where TKey").
		{"Gen", "public class Gen<T> where T : class"},
		{"GenIface", "public class GenIface<T> where T : IDisposable"},
		{"GenNew", "public class GenNew<T> where T : new()"},
		{"GenNotNull", "public class GenNotNull<TKey> where TKey : notnull"},
		{"TwoClause", "public class TwoClause<T, U> where T : class where U : struct"},
		{"DictLike", "public class DictLike<TKey, TValue> where TKey : notnull where TValue : class"},
		// A constraint whose own type is generic: the "<T>" inside the
		// constraint is not a second delimiter.
		{"GenSelf", "public class GenSelf<T> where T : IEquatable<T>"},

		// ---- BOTH, base list first (§15.2.1). Already green before the fix,
		// and they must STAY green: they are what proves the fix did not
		// simply stop cutting whenever a `where` appears anywhere.
		{"Both", "public class Both<T>"},
		{"BothTwo", "public class BothTwo<T, U>"},
		{"GenBase", "public class GenBase<T>"},

		// ---- whitespace variants.
		// No space before the clause colon: there is no " :" in the header at
		// all, so the cut never fired here even before the fix. Control.
		{"NoSpaceCons", "public class NoSpaceCons<T> where T: class"},
		// The mirror remainder, PRE-EXISTING and deliberately unchanged: no
		// space before the BASE colon means the base list is not dropped.
		// Recorded so the next reader sees it is known, not missed.
		{"NoSpaceBase", "public class NoSpaceBase: Object"},
		// The clause begins on the next line; the collapse turns the newline
		// into a single space before either cut sees it. Truncated before.
		{"WhereNewline", "public class WhereNewline<T> where T : class"},
		// No whitespace at all between ">" and "where" — legal, since ">" is
		// not an identifier character (§6.4.3), so the two tokens need no
		// separator. This row is why the clause matcher accepts any
		// non-identifier char before `where` rather than requiring a space.
		{"TightWhere", "public class TightWhere<T>where T : class"},

		// ---- declaration kind.
		{"IGen", "public interface IGen<T> where T : class"},
		{"RecGen", "public record RecGen<T>(T Value) where T : class"},
		{"StrGen", "public struct StrGen<T> where T : struct"},

		// ---- attributes, which are stripped before either cut.
		{"AttrGen", "public class AttrGen<T> where T : class"},
		{"TPGen", "public class TPGen<T> where T : class"},
		// An attribute target specifier (§22.3) written with a SPACE before
		// its colon, i.e. a real " :" inside the parameter list. The strip
		// removes it, so it is not a delimiter. Green before and after.
		{"TgtSpace", "public record TgtSpace(string First, string Last)"},

		// ---- `where`-shaped text that is not a clause.
		// PRE-EXISTING REMAINDER, unchanged by this fix: a " :" inside a
		// string literal in the header is still a delimiter. Out of scope —
		// the fix must not silently repair or worsen it.
		{"LitColon", `public record LitColon<T>(string S = "`},
		// "a where b" is not a constraint clause (no colon follows the
		// identifier), so it must not suppress anything. Truncated at the
		// REAL clause colon before the fix.
		{"LitWhere", `public record LitWhere<T>(string S = "a where b") where T : class`},
		// The same literal plus a real base list: the base list must still be
		// dropped. This row fails if the clause matcher accepts a bare
		// `where <ident>` without a following colon. Green before and after.
		{"LitWhereColon", `public record LitWhereColon<T>(string S = "a where b")`},
		// "Nowhere" ends in "where" and is followed by " : Object". This row
		// fails if the matcher does not require a non-identifier character
		// before `where`: the base list would stop being dropped.
		{"Nowhere", "public class Nowhere<T>"},

		// ---- alias-qualified constraint types. `::` is a single token
		// (§6.4.6) and §6.3.1 permits whitespace around it, so a constraint
		// TYPE can itself contain a " :" -- the only way a " :" reaches the
		// header AFTER the `where` token. These three rows are what make the
		// clause matcher's `\s*:` load-bearing rather than cosmetic: with
		// `\s+:` the no-space clause colon of AliasCons stops matching, the
		// cut falls through to the " ::" and the constraint type is lost.
		{"AliasCons", "public class AliasCons<T> where T: global :: System.IDisposable"},
		// The conventional spelling of the same constraint: no " :" anywhere,
		// so it was green before the fix too. Control for the row above.
		{"AliasTight", "public class AliasTight<T> where T: global::System.IDisposable"},
		// The alias qualifier in the BASE LIST instead: the base-list colon
		// is still the first " :", so the cut still fires there.
		{"AliasBase", "public class AliasBase<T>"},

		// ---- neither. Control.
		{"Plain7139", "public class Plain7139"},
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

// `where` as the class NAME. Both rows have a base list and NO constraint
// clause, so the cut must fire; they fail if the clause matcher treats the
// bare word `where` as the start of a clause.
func TestCSharp7139_ContextualKeywordAsTypeNameStillDropsBaseList(t *testing.T) {
	recs := csExtract(t, cs7139WhereNameSrc, "Api/Sig/WhereName.cs")
	for _, want := range []struct{ name, sig string }{
		{"where", "public class where"},
		{"whereGen", "public class whereGen<T>"},
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

// The SIBLING half of the pair. buildMethodSignature (csharp.go:1805) has NO
// " :" cut at all -- its delimiters are "{" and "=>" -- so it never had this
// defect, and it is NOT changed by this commit. But a method may carry
// constraint clauses too (ECMA-334 §15.6.1 orders method_header as
// ... identifier type_parameter_list? "(" formal_parameter_list? ")"
// type_parameter_constraints_clause*), so the absence of a colon cut there is
// load-bearing behaviour that nothing observed: with this test held out,
// ADDING the naive `if idx := strings.Index(raw, " :"); idx >= 0` cut to
// buildMethodSignature left ./internal/extractors/csharp/ green (0 "--- FAIL"
// lines). #7138 found the same thing about this sibling's ordering, so this is
// the second time the documented-correct half of this pair turned out to be
// ungraded. These rows are its cover.
func TestCSharp7139_MethodSignatureKeepsGenericConstraints(t *testing.T) {
	const src = `using System;

namespace Api.Sig
{
    public class Svc
    {
        public void Do<T>(T x) where T : class { }

        public T Id<T>(T x) where T : class => x;

        public void Two<T, U>(T t, U u) where T : class where U : struct { }

        public void Plain(int x) { }
    }
}
`
	recs := csExtract(t, src, "Api/Sig/SvcMethods.cs")
	for _, want := range []struct{ name, sig string }{
		// Body-brace delimiter, constraint kept whole.
		{"Svc.Do", "public void Do<T>(T x) where T : class"},
		// Expression-bodied member: the "=>" cut runs and the constraint,
		// which precedes it, survives.
		{"Svc.Id", "public T Id<T>(T x) where T : class"},
		// Two clauses.
		{"Svc.Two", "public void Two<T, U>(T t, U u) where T : class where U : struct"},
		// Control: no generics at all.
		{"Svc.Plain", "public void Plain(int x)"},
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
