package substrate

import "testing"

// substrate_java_graphql_taint_test.go — issue: vuln-finding sibling sweep
// (epic #3872). Verify-first proof that the framework-BLIND, per-LANGUAGE Java
// taint sniffer (taint_sites_java.go, registered via RegisterTaintSniffer("java",
// …), dispatched solely by file extension through LanguageForPath: ".java" ->
// "java") fires on Netflix DGS and Spring-for-GraphQL resolver-method bodies.
//
// This is the Java analog of the gqlgen (#3918), Pothos/TypeGraphQL (#3903) and
// PHP-Lighthouse GraphQL verify-first taint credits. taint_sites_java.go contains
// zero framework references: its SQL-injection sink (javaSinkSQLRe), HTML/SQL
// sanitizers (javaSanitizerHTMLRe / javaSanitizerSQLRe) and the @Valid sanitizer
// match on any Java method body and attribute to the enclosing method via
// scanJavaFuncHeaders, which accepts an annotation-prefixed header
// (`@DgsQuery public List<Show> shows(…)` / `@QueryMapping public Book …`).
//
// VERIFY-FIRST findings encoded as assertions:
//
//  1. taint_sink_detection (PARTIAL credit): the raw-Statement SQL sink
//     (`stmt.executeQuery("… '" + arg + "'")`) is flagged sql_injection and
//     attributes to the DGS / Spring-GraphQL resolver method.
//  2. sanitizer_recognition (PARTIAL credit): a PreparedStatement
//     (parameterised-SQL) sanitizer AND an HtmlUtils.htmlEscape XSS sanitizer
//     both fire and attribute to the same resolver method.
//
//  HONEST NEGATIVE (vulnerability_finding stays MISSING): the Java taint SOURCE
//  regexes key on the servlet API (request.getParameter/Header), Spring MVC
//  request annotations (@RequestParam/@PathVariable/@RequestBody),
//  System.getenv/getProperty and ObjectInputStream.readObject. A DGS /
//  Spring-GraphQL resolver receives untrusted input via the GraphQL-specific
//  @InputArgument / @Argument typed parameters, which are NOT among the
//  recognised request-input sources — so no taint SOURCE fires on the
//  resolver-arg idiom, and taint_flow.go (which only seeds its source→sink BFS
//  from a TaintKindSource match) emits no SecurityFinding. Proven by the
//  *_TaintSourceDoesNotFire tests below. vulnerability_finding therefore stays
//  honestly missing for both Java GraphQL siblings.

// dgsResolverSrc is a representative Netflix DGS data-fetcher: the canonical
// `@DgsComponent` class with a `@DgsQuery`-annotated resolver method that reads
// the @InputArgument, builds a raw concatenated SQL string into a Statement
// sink, then demonstrates both a PreparedStatement (parameterised) and an
// HtmlUtils.htmlEscape sanitizer.
const dgsResolverSrc = `
package com.example.shows;

import com.netflix.graphql.dgs.DgsComponent;
import com.netflix.graphql.dgs.DgsQuery;
import com.netflix.graphql.dgs.InputArgument;
import org.springframework.web.util.HtmlUtils;

@DgsComponent
public class ShowsDataFetcher {

    @DgsQuery
    public List<Show> shows(@InputArgument String titleFilter) throws Exception {
        String sql = "SELECT * FROM shows WHERE title = '" + titleFilter + "'";
        ResultSet rs = stmt.executeQuery(sql);
        String safe = HtmlUtils.htmlEscape(titleFilter);
        PreparedStatement ps = conn.prepareStatement("SELECT * FROM shows WHERE title = ?");
        return parse(rs, safe);
    }
}
`

// springGraphQLResolverSrc is a representative Spring-for-GraphQL controller:
// the canonical `@Controller` class with a `@QueryMapping` resolver method that
// reads the @Argument, builds a raw concatenated SQL string into a Statement
// sink, then demonstrates both a PreparedStatement and HtmlUtils.htmlEscape
// sanitizer.
const springGraphQLResolverSrc = `
package com.example.book;

import org.springframework.graphql.data.method.annotation.QueryMapping;
import org.springframework.graphql.data.method.annotation.Argument;
import org.springframework.stereotype.Controller;
import org.springframework.web.util.HtmlUtils;

@Controller
public class BookController {

    @QueryMapping
    public Book bookByName(@Argument String name) throws Exception {
        String sql = "SELECT * FROM book WHERE name = '" + name + "'";
        ResultSet rs = stmt.executeQuery(sql);
        String safe = HtmlUtils.htmlEscape(name);
        PreparedStatement ps = conn.prepareStatement("SELECT * FROM book WHERE name = ?");
        return map(rs, safe);
    }
}
`

// hasTaintInFn asserts a TaintMatch of the given kind+category exists AND is
// attributed to method fn (so taint_flow.go can bind it to a graph entity).
func hasTaintInFn(t *testing.T, ms []TaintMatch, kind TaintKind, cat TaintCategory, fn string) {
	t.Helper()
	for _, m := range ms {
		if m.Kind == kind && m.Category == cat && m.Function == fn {
			return
		}
	}
	t.Errorf("expected taint %s/%s attributed to %q; got %+v", kind, cat, fn, ms)
}

// --- Netflix DGS ------------------------------------------------------------

// TestSubstrate_Java_DGS_TaintSinkFires proves taint_sink_detection for DGS: the
// raw-Statement SQL sink fires on the @DgsQuery resolver body and attributes to
// the resolver method `shows`.
func TestSubstrate_Java_DGS_TaintSinkFires(t *testing.T) {
	ms := sniffTaintJava(dgsResolverSrc)
	hasTaintInFn(t, ms, TaintKindSink, TaintCategorySQL, "shows")
}

// TestSubstrate_Java_DGS_SanitizerFires proves sanitizer_recognition for DGS:
// both the PreparedStatement (parameterised-SQL) sanitizer and the
// HtmlUtils.htmlEscape XSS sanitizer fire and attribute to `shows`.
func TestSubstrate_Java_DGS_SanitizerFires(t *testing.T) {
	ms := sniffTaintJava(dgsResolverSrc)
	hasTaintInFn(t, ms, TaintKindSanitizer, TaintCategorySQL, "shows")
	hasTaintInFn(t, ms, TaintKindSanitizer, TaintCategoryXSS, "shows")
}

// TestSubstrate_Java_DGS_TaintSourceDoesNotFire documents WHY
// vulnerability_finding is left missing for DGS: the Java taint SOURCE regexes
// key on servlet / Spring-MVC request accessors, System.getenv and
// ObjectInputStream.readObject. A DGS resolver reads its untrusted input via the
// @InputArgument typed parameter, which is NOT a recognised source — so no taint
// source fires, hence no source→sink SecurityFinding.
func TestSubstrate_Java_DGS_TaintSourceDoesNotFire(t *testing.T) {
	ms := sniffTaintJava(dgsResolverSrc)
	if n := countTaint(ms, TaintKindSource, TaintCategoryGeneric); n != 0 {
		t.Errorf("expected NO taint source (resolver reads @InputArgument typed arg, not request.getParameter/@RequestParam); got %d: %+v", n, ms)
	}
}

// --- Spring for GraphQL -----------------------------------------------------

// TestSubstrate_Java_SpringGraphQL_TaintSinkFires proves taint_sink_detection
// for Spring-for-GraphQL: the raw-Statement SQL sink fires on the @QueryMapping
// resolver body and attributes to `bookByName`.
func TestSubstrate_Java_SpringGraphQL_TaintSinkFires(t *testing.T) {
	ms := sniffTaintJava(springGraphQLResolverSrc)
	hasTaintInFn(t, ms, TaintKindSink, TaintCategorySQL, "bookByName")
}

// TestSubstrate_Java_SpringGraphQL_SanitizerFires proves sanitizer_recognition
// for Spring-for-GraphQL: both the PreparedStatement (parameterised-SQL)
// sanitizer and the HtmlUtils.htmlEscape XSS sanitizer fire and attribute to
// `bookByName`.
func TestSubstrate_Java_SpringGraphQL_SanitizerFires(t *testing.T) {
	ms := sniffTaintJava(springGraphQLResolverSrc)
	hasTaintInFn(t, ms, TaintKindSanitizer, TaintCategorySQL, "bookByName")
	hasTaintInFn(t, ms, TaintKindSanitizer, TaintCategoryXSS, "bookByName")
}

// TestSubstrate_Java_SpringGraphQL_TaintSourceDoesNotFire documents WHY
// vulnerability_finding is left missing for Spring-for-GraphQL: the @Argument
// typed parameter is not a recognised taint source, so no source fires and no
// source→sink SecurityFinding is emitted.
func TestSubstrate_Java_SpringGraphQL_TaintSourceDoesNotFire(t *testing.T) {
	ms := sniffTaintJava(springGraphQLResolverSrc)
	if n := countTaint(ms, TaintKindSource, TaintCategoryGeneric); n != 0 {
		t.Errorf("expected NO taint source (resolver reads @Argument typed arg, not request.getParameter/@RequestParam); got %d: %+v", n, ms)
	}
}
