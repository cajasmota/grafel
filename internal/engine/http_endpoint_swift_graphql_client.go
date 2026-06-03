// Swift (iOS) Apollo-iOS GraphQL client-side (consumer) operation synthesis
// (#4036, epic #3872; mirrors the JS/TS GraphQL client pass in
// http_endpoint_graphql_client.go and the Dart pass in
// http_endpoint_dart_graphql_client.go).
//
// Apollo-iOS does NOT embed the GraphQL document at the Swift call site. The
// Apollo codegen reads `.graphql` operation files and generates one Swift type
// per operation — `GetUserQuery`, `AddUserMutation`, `OnMessageSubscription` —
// which the app instantiates and passes to the client:
//
//	apollo.fetch(query: GetUserQuery()) { result in ... }
//	apollo.perform(mutation: AddUserMutation(name: name)) { ... }
//	apollo.subscribe(subscription: OnMessageSubscription()) { ... }
//
// Because the selected ROOT FIELD lives only in the generated/.graphql file —
// not in the Swift source we are scanning — the Swift call site carries just
// the operation NAME and KIND (encoded in the generated type's name + the
// fetch/perform/subscribe verb). This is an HONEST-PARTIAL: we emit one
// operation REFERENCE per call site, keyed on the operation NAME under the
// matching root type:
//
//	apollo.fetch(query: GetUserQuery())  →  http:GRAPHQL:/graphql/Query/GetUser
//
// plus a FETCHES edge from the enclosing func — exactly the same shape and
// mechanism as the JS/TS `graphql_client_unresolved` path (where a gql const
// is imported cross-file and its root field cannot be resolved locally). The
// operation therefore becomes DISCOVERABLE on the iOS side; it does NOT
// guarantee a field-level cross-repo link to the backend resolver (the server
// endpoint is keyed on the schema field, not the operation name), which is why
// the framework label is `apollo_ios_unresolved`. Full field resolution would
// require parsing the companion `.graphql` files; that is deferred (#4036).
//
// The generated-type recognition is intentionally NARROW: only identifiers
// passed as the `query:`/`mutation:`/`subscription:` argument of an
// `apollo`-receiver `.fetch`/`.perform`/`.subscribe` call are treated as
// operations, so an arbitrary `SearchQuery()` constructor used elsewhere is
// never mis-attributed.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// swiftApolloOpRe matches an Apollo-iOS client operation call and captures the
// generated operation type name:
//
//	apollo.fetch(query: GetUserQuery())
//	store.apollo.perform(mutation: AddUserMutation(name: x))
//	client.subscribe(subscription: OnMessageSubscription())
//
// Receiver allow-list (left of the `.`): an identifier ending in / equal to
// `apollo` or `client` (apollo, _apollo, apolloClient, client). Group 1 = the
// method (fetch/perform/subscribe), group 2 = the argument label
// (query/mutation/subscription), group 3 = the generated operation type name.
//
// The argument-label ↔ method pairing is enforced by the label→kind map below
// (fetch/query, perform/mutation, subscribe/subscription), so a mismatched
// `apollo.fetch(mutation: ...)` is not silently coerced.
var swiftApolloOpRe = regexp.MustCompile(
	`\b(?:[A-Za-z_]\w*\.)?(?:apollo|_apollo|apolloClient|client)\s*\.\s*(fetch|perform|subscribe)\s*\(\s*(query|mutation|subscription)\s*:\s*([A-Za-z_]\w*)\s*\(`,
)

// swiftApolloArgLabelToRoot maps the Apollo call argument label to the GraphQL
// server root-type name used in the synthetic path.
var swiftApolloArgLabelToRoot = map[string]string{
	"query":        "Query",
	"mutation":     "Mutation",
	"subscription": "Subscription",
}

// swiftApolloTypeSuffix maps a root type to the conventional generated-type
// name suffix Apollo-iOS appends (GetUserQuery, AddUserMutation,
// OnMessageSubscription) so the operation NAME can be recovered.
var swiftApolloTypeSuffix = map[string]string{
	"Query":        "Query",
	"Mutation":     "Mutation",
	"Subscription": "Subscription",
}

// synthesizeSwiftGraphQLClient scans a Swift file for Apollo-iOS client
// operations and emits one honest-partial operation reference per call site,
// keyed on the operation NAME under the matching root type, plus a FETCHES edge
// from the enclosing func.
//
// It is invoked from synthesizeSwiftClientWithRuntime so it shares the Swift
// dispatch and the runtime-aware emitter.
func synthesizeSwiftGraphQLClient(content string, funcs []jsFuncSpan, emit swiftClientEmitFn) {
	// File-signal gate: require an Apollo client marker so this is a no-op on
	// ordinary URLSession/Alamofire Swift files.
	if !strings.Contains(content, "apollo") && !strings.Contains(content, "Apollo") {
		return
	}

	for _, m := range swiftApolloOpRe.FindAllStringSubmatchIndex(content, -1) {
		label := content[m[4]:m[5]]
		typeName := content[m[6]:m[7]]
		root, ok := swiftApolloArgLabelToRoot[label]
		if !ok {
			continue
		}

		// Recover the operation NAME by trimming the conventional Apollo type
		// suffix (GetUserQuery → GetUser). If the generated type does not carry
		// the expected suffix (non-conventional codegen config), keep the full
		// type name — still a discoverable, correctly-rooted operation reference.
		opName := typeName
		if suffix := swiftApolloTypeSuffix[root]; suffix != "" && strings.HasSuffix(typeName, suffix) && len(typeName) > len(suffix) {
			opName = strings.TrimSuffix(typeName, suffix)
		}
		if opName == "" {
			continue
		}

		path := "/graphql/" + root + "/" + opName
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		caller := enclosingSwiftFuncAt(funcs, m[0])
		// runtimeDynamic=false: the operation reference is statically resolved from
		// the generated type name (the endpoint ID itself is concrete). It is an
		// honest-PARTIAL only in that the root FIELD — and thus a guaranteed
		// field-level cross-repo link — is unresolved without the .graphql file.
		emit(gqlVerb, canonical, "apollo_ios_unresolved", "Function", caller, false)
	}
}
