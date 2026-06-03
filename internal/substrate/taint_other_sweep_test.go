package substrate

import "testing"

// taint_other_sweep_test.go — issue: vuln-finding sibling sweep (epic #3872),
// languages python/csharp/ruby/php. Verify-first proof that the framework-BLIND,
// per-LANGUAGE C# taint sniffer (taint_sites_csharp.go, registered via
// RegisterTaintSniffer("csharp", sniffTaintCSharp), dispatched solely by file
// extension through LanguageForPath: ".cs" -> "csharp") fires on a HotChocolate
// GraphQL resolver-method body.
//
// HotChocolate is the C# GraphQL-server sibling of the 6 HTTP-backend siblings
// (aspnet-core/aspnet-mvc/carter/fastendpoints/nancyfx/servicestack) that already
// carry partial taint cells. This is the C# analog of the Java DGS/Spring-GraphQL
// (#4183), Go gqlgen (#3918) and JS/TS Pothos/TypeGraphQL (#3903) verify-first
// taint credits. taint_sites_csharp.go contains zero HotChocolate references: its
// SQL-injection sink (csSinkSQLRe), parameterised-SQL sanitizer (csSanitizerSQLRe)
// and HTML-encode XSS sanitizer (csSanitizerHTMLRe) match on any C# method body and
// attribute to the enclosing method via scanCSharpFuncHeaders, which accepts a
// plain `public Book GetBookById(...)` header (csharpMethodHeaderRe also tolerates
// an attribute prefix such as `[UseFiltering] public ...`).
//
// VERIFY-FIRST findings encoded as EXACT (kind, category, function) assertions:
//
//  1. taint_sink_detection (PARTIAL): the CommandText-interpolation SQL sink
//     (csSinkSQLRe: `CommandText = $"... {arg} ..."`) is flagged sql_injection and
//     attributes to the HotChocolate resolver method `GetBookByTitle`.
//  2. sanitizer_recognition (PARTIAL): a parameterised-SQL sanitizer
//     (SqlCommand.Parameters.AddWithValue, csSanitizerSQLRe -> sanitizer/sql) AND an
//     HtmlEncoder.Default.Encode XSS sanitizer (csSanitizerHTMLRe -> sanitizer/xss)
//     both fire and attribute to `GetBookByTitle`.
//  3. taint_source_detection (PARTIAL): csSourceEnvRe (IConfiguration["..."]) fires
//     framework-blind and attributes to the resolver method — proving the per-LANGUAGE
//     source detector is exercised on a HotChocolate body (env/config source, NOT the
//     GraphQL request-input idiom; see the honest negative below).
//
//  HONEST NEGATIVE (vulnerability_finding stays MISSING): a vulnerability_finding
//  (SecurityFinding) requires a request-input source -> sink path and taint_flow.go
//  only seeds its BFS from a TaintKindSource match. The C# taint request-input SOURCE
//  regexes key on the ASP.NET HttpRequest accessors (Request.Form/Query/Headers/...)
//  and the [From*] action-parameter attributes (csSourceRequestRe / csSourceAttrRe).
//  A HotChocolate resolver receives untrusted input via its GraphQL-typed method
//  arguments (e.g. `string title` / an `[Argument]`-shaped DTO), which are NOT among
//  the recognised request-input sources — so no request-input source fires on the
//  resolver-arg idiom and no end-to-end request-input->sink finding is emitted.
//  Proven by TestSubstrate_CSharp_HotChocolate_RequestInputSourceDoesNotFire.
//  vulnerability_finding therefore stays honestly missing for HotChocolate.

// hotChocolateResolverSrc is a representative HotChocolate resolver: the canonical
// `Query` type with a plain resolver method that reads its GraphQL-typed `title`
// argument, builds a raw interpolated SqlCommand.CommandText SQL sink, then
// demonstrates both a SqlCommand.Parameters.AddWithValue (parameterised) sanitizer
// and an HtmlEncoder.Default.Encode XSS sanitizer. It also reads IConfiguration to
// exercise the framework-blind env/config taint source.
const hotChocolateResolverSrc = `
using HotChocolate;
using HotChocolate.Types;
using System.Data.SqlClient;
using System.Text.Encodings.Web;
using Microsoft.Extensions.Configuration;

public class Query
{
    public Book GetBookByTitle(string title)
    {
        var conn = _configuration["ConnectionStrings:Default"];
        var cmd = new SqlCommand();
        cmd.CommandText = $"SELECT * FROM books WHERE title = '{title}'";
        var safeCmd = new SqlCommand("SELECT * FROM books WHERE title = @t");
        safeCmd.Parameters.AddWithValue("@t", title);
        var safeTitle = HtmlEncoder.Default.Encode(title);
        return Load(cmd, safeTitle);
    }
}
`

// hasTaintInFnExact asserts a TaintMatch of the given kind+category exists AND is
// attributed to method fn (so taint_flow.go can bind it to a graph entity). It is
// deliberately exact (kind, category, function) — never len>0 — so the test FAILS
// if the primitive is miscategorised or mis-attributed.
func hasTaintInFnExact(t *testing.T, ms []TaintMatch, kind TaintKind, cat TaintCategory, fn string) {
	t.Helper()
	for _, m := range ms {
		if m.Kind == kind && m.Category == cat && m.Function == fn {
			return
		}
	}
	t.Errorf("expected taint %s/%s attributed to %q; got %+v", kind, cat, fn, ms)
}

// TestSubstrate_CSharp_HotChocolate_TaintSinkFires proves taint_sink_detection for
// HotChocolate: the CommandText-interpolation SQL sink fires on the resolver body
// and attributes EXACTLY to (sink, sql_injection, GetBookByTitle).
func TestSubstrate_CSharp_HotChocolate_TaintSinkFires(t *testing.T) {
	ms := sniffTaintCSharp(hotChocolateResolverSrc)
	hasTaintInFnExact(t, ms, TaintKindSink, TaintCategorySQL, "GetBookByTitle")
}

// TestSubstrate_CSharp_HotChocolate_SanitizerFires proves sanitizer_recognition for
// HotChocolate: both the parameterised-SQL sanitizer (sanitizer, sql_injection) and
// the HtmlEncoder XSS sanitizer (sanitizer, xss) fire and attribute EXACTLY to
// GetBookByTitle.
func TestSubstrate_CSharp_HotChocolate_SanitizerFires(t *testing.T) {
	ms := sniffTaintCSharp(hotChocolateResolverSrc)
	hasTaintInFnExact(t, ms, TaintKindSanitizer, TaintCategorySQL, "GetBookByTitle")
	hasTaintInFnExact(t, ms, TaintKindSanitizer, TaintCategoryXSS, "GetBookByTitle")
}

// TestSubstrate_CSharp_HotChocolate_TaintSourceFires proves taint_source_detection
// for HotChocolate: the framework-blind IConfiguration["..."] env/config source
// fires on the resolver body and attributes EXACTLY to (source, generic,
// GetBookByTitle). This exercises the per-LANGUAGE source detector on a HotChocolate
// body — it is NOT the GraphQL request-input idiom (see the negative test).
func TestSubstrate_CSharp_HotChocolate_TaintSourceFires(t *testing.T) {
	ms := sniffTaintCSharp(hotChocolateResolverSrc)
	hasTaintInFnExact(t, ms, TaintKindSource, TaintCategoryGeneric, "GetBookByTitle")
}

// hotChocolateNoRequestSrc is a HotChocolate resolver that takes only a GraphQL-typed
// argument and contains NO ASP.NET request accessor and NO [From*] attribute and NO
// env/config read — proving that the request-input source idiom of a GraphQL resolver
// (its typed args) is not a recognised taint source.
const hotChocolateNoRequestSrc = `
using HotChocolate;
using HotChocolate.Types;

public class Query
{
    public Book GetBookByTitle(string title)
    {
        return Lookup(title);
    }
}
`

// TestSubstrate_CSharp_HotChocolate_RequestInputSourceDoesNotFire documents WHY
// vulnerability_finding is left missing for HotChocolate: the C# taint request-input
// SOURCE regexes key on Request.Form/Query/Headers (csSourceRequestRe) and the
// [From*] action-parameter attributes (csSourceAttrRe). A HotChocolate resolver reads
// untrusted input via its GraphQL-typed `title` argument, which is NOT a recognised
// request-input source — so zero taint sources fire and taint_flow.go (which seeds its
// source->sink BFS only from a TaintKindSource match) emits no SecurityFinding.
func TestSubstrate_CSharp_HotChocolate_RequestInputSourceDoesNotFire(t *testing.T) {
	ms := sniffTaintCSharp(hotChocolateNoRequestSrc)
	if n := countTaint(ms, TaintKindSource, TaintCategoryGeneric); n != 0 {
		t.Errorf("expected NO taint source (resolver reads a GraphQL-typed arg, not Request.Form/Query or [From*]); got %d: %+v", n, ms)
	}
	if n := countTaint(ms, TaintKindSource, TaintCategoryDeserialization); n != 0 {
		t.Errorf("expected NO deserialization taint source on a plain resolver; got %d: %+v", n, ms)
	}
}
