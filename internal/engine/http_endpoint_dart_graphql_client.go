// Dart / Flutter GraphQL client-side (consumer) operation synthesis
// (#4036, epic #3872; mirrors the JS/TS GraphQL client pass in
// http_endpoint_graphql_client.go).
//
// The graphql_flutter / graphql Dart packages express GraphQL operations by
// wrapping a GraphQL document string in the `gql(...)` parser function and
// passing it to a Query / Mutation / Subscription widget or a
// GraphQLClient.query / .mutate / .subscribe call:
//
//	final GET_USER = gql(r'''query GetUser { user { id name } }''');
//	Query(options: QueryOptions(document: GET_USER), ...)
//
//	client.mutate(MutationOptions(
//	  document: gql(r'''mutation AddUser { createUser(input: ...) { id } }'''),
//	));
//
//	client.query(QueryOptions(document: gql('query D { stats { count } }')));
//
// Before this pass these produced no operation entity and no cross-repo link:
// the Dart HTTP-client pass (http_endpoint_dart_client.go) only recognises REST
// (Dio / package:http) verb calls, so a Flutter screen talking to a GraphQL
// backend was invisible at the operation level.
//
// This pass recognises the `gql(<string-literal>)` document — whether bound to
// a const/final and referenced by name, or inlined directly at the call site —
// parses its operation type (query/mutation/subscription) and ROOT FIELD(s),
// and emits one synthetic http_endpoint_call per (operation, root field) keyed
// to EXACTLY match the GraphQL server endpoint shape produced by the resolver
// synthesis (http_endpoint_synthesis.go):
//
//	http:GRAPHQL:/graphql/<RootType>/<field>   e.g. http:GRAPHQL:/graphql/Query/user
//
// Because the ID is identical to the server-side definition's ID, the existing
// Name-based cross-repo HTTP linker (links/http_pass.go) joins the Flutter
// client operation to the backend resolver on reindex with NO new linker code —
// exactly like the REST Dart client pass and the JS/TS GraphQL client pass.
//
// The GraphQL document parser (gqlRootFieldsFromDoc / gqlTopLevelFields and
// friends) is shared verbatim with the JS/TS pass — the GraphQL grammar is
// language-agnostic; only the host-language string-extraction differs.
//
// FETCHES edge: the enclosing Dart function/method at the call (or const-def)
// site is recorded as source_caller (Function:<name>); the shared
// emitClientRuntime → ResolveHTTPEndpointHandlers turns that into the FETCHES
// edge, the same mechanism as every other consumer pass.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// dartGqlDocRe matches a `gql(<string-literal>)` call in Dart and captures the
// document body. Dart string literals come in several flavours, all handled by
// the alternation below (optional `r` raw prefix in every case):
//
//	gql(r'''query { ... }''')   triple-quoted raw (the dominant graphql_flutter form)
//	gql("""query { ... }""")    triple-quoted double
//	gql(r'query { ... }')       single-quoted raw
//	gql('query { ... }')        single-quoted
//	gql("query { ... }")        double-quoted
//
// Capture groups (mutually exclusive, exactly one is non-empty):
//
//	1 = triple-single body, 2 = triple-double body,
//	3 = single-quote body,  4 = double-quote body.
//
// (?s) so the `.`-equivalents span newlines inside a multi-line document.
var dartGqlDocRe = regexp.MustCompile(
	`(?s)\bgql\s*\(\s*r?(?:'''(.*?)'''|"""(.*?)"""|'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)")`,
)

// dartGqlConstDefRe captures a Dart declaration whose initializer is a
// gql(...) document, capturing the const/final NAME (group 1). The gql body is
// re-parsed from the same span via dartGqlDocRe; this regex only anchors the
// name so a TOP-LEVEL const (whose gql site has no enclosing function) can have
// its FETCHES caller resolved from the REFERENCE site instead. It tolerates an
// optional type between the keyword and the name
// (`final DocumentNode GET_USER = gql(...)`).
var dartGqlConstDefRe = regexp.MustCompile(
	`\b(?:final|const|var)\s+(?:[\w<>,\[\]?.]+\s+)?([A-Za-z_$][\w$]*)\s*=\s*gql\s*\(`,
)

// Note: unlike the JS/TS pass, Dart graphql_flutter ALWAYS inlines the document
// string inside the gql(...) parser call — `final GET_USER = gql(r”'...”')` —
// so the document body is present at EVERY operation site (there is no
// cross-file honest-partial case: every gql(...) carries its own document).
// The only subtlety is FETCHES caller attribution for a TOP-LEVEL const: its
// gql site has no enclosing function, so we attribute the caller to the
// function that REFERENCES the const (`document: GET_USER`) — typically the
// widget build() method — which is the real consumer of the operation.
//
// synthesizeDartGraphQLClient scans a Dart file for graphql_flutter / graphql
// GraphQL operations and emits one http_endpoint_call per (operation, root
// field), keyed to match the server endpoint shape
// http:GRAPHQL:/graphql/<RootType>/<field>, plus a FETCHES edge from the
// enclosing Dart function.
//
// It is invoked from synthesizeDartClientWithRuntime so it shares the Dart
// dispatch and the runtime-aware emitter.
func synthesizeDartGraphQLClient(content string, funcs []jsFuncSpan, emit dartClientEmitFn) {
	// File-signal gate: require the `gql(` parser marker so this is a no-op on
	// ordinary REST/widget Dart files. (`gql(` is the parser-call form unique to
	// the graphql/graphql_flutter packages; the REST Dart pass keys off `dio`/
	// `http.`/`Uri.parse` instead, so the two passes never collide.)
	if !strings.Contains(content, "gql(") {
		return
	}

	// emitDoc emits one client-call per resolved root field for a gql document.
	emitDoc := func(rt string, fields []string, caller string) {
		for _, f := range fields {
			path := "/graphql/" + rt + "/" + f
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			// runtimeDynamic=false: the operation root field is statically resolved
			// from the literal gql document, so the endpoint is fully concrete.
			emit(gqlVerb, canonical, "graphql_flutter", "Function", caller, false)
		}
	}

	// dartGqlDocBody returns the document body from a dartGqlDocRe submatch
	// (exactly one of the four quote-flavour groups is populated).
	dartGqlDocBody := func(m []string) string {
		for g := 1; g <= 4; g++ {
			if m[g] != "" {
				return m[g]
			}
		}
		return ""
	}

	// Record each gql const-definition span → the const NAME, so a gql site that
	// resolves to an empty enclosing function (a top-level const) can have its
	// FETCHES caller resolved from a reference site below. dartGqlConstDefRe
	// spans from the `final`/`const`/`var` keyword through the `(` of `gql(`, so
	// the gql token sits inside [start, end).
	type constSpan struct {
		start, end int
		name       string
	}
	var constSpans []constSpan
	for _, dm := range dartGqlConstDefRe.FindAllStringSubmatchIndex(content, -1) {
		constSpans = append(constSpans, constSpan{start: dm[0], end: dm[1], name: content[dm[2]:dm[3]]})
	}
	constNameForGqlSite := func(gqlOff int) string {
		for _, cs := range constSpans {
			if cs.start <= gqlOff && gqlOff < cs.end {
				return cs.name
			}
		}
		return ""
	}

	// refCallerForConst returns the enclosing function of the FIRST reference to
	// `name` (`document: name`, `query: name`, or a bare `name` argument) that
	// lands inside some function, so a top-level gql const is attributed to its
	// consumer (the widget build()/method that passes it to Query/Mutation).
	refCallerForConst := func(name string) string {
		if name == "" {
			return ""
		}
		ref := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		for _, loc := range ref.FindAllStringIndex(content, -1) {
			if c := enclosingDartFuncAt(funcs, loc[0]); c != "" {
				return c
			}
		}
		return ""
	}

	// Every gql(...) site, in source order, handled exactly once. The caller is
	// the enclosing Dart function; for a top-level const def (no enclosing
	// function) the caller is resolved from the const's reference site.
	for _, m := range dartGqlDocRe.FindAllStringSubmatchIndex(content, -1) {
		bodyGroups := dartGqlDocRe.FindStringSubmatch(content[m[0]:m[1]])
		if bodyGroups == nil {
			continue
		}
		doc := dartGqlDocBody(bodyGroups)
		if strings.TrimSpace(doc) == "" {
			continue
		}
		rt, fields, ok := gqlRootFieldsFromDoc(doc)
		if !ok || len(fields) == 0 {
			continue
		}
		caller := enclosingDartFuncAt(funcs, m[0])
		if caller == "" {
			// Top-level gql site: if it is a const definition, attribute the
			// operation to the function that references the const.
			caller = refCallerForConst(constNameForGqlSite(m[0]))
		}
		emitDoc(rt, fields, caller)
	}
}
