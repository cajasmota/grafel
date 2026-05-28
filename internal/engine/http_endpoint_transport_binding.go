// RPC transport-binding detection (#2906). tRPC and GraphQL are
// transport-agnostic RPC layers: the same router / resolver map is served
// over HTTP, WebSocket, or both depending on which adapter the server-setup
// code wires up. The procedure / resolver-field synthesizers
// (synthesizeTRPC, synthesizeGraphQLResolvers) emit the addressable endpoints
// but say nothing about HOW they are reached. This pass scans the same file
// for the adapter/handler call that binds the router to a wire protocol and
// stamps a `transport` property on each synthetic the RPC pass just emitted.
//
// The result is one of:
//
//	http     — request/response adapter only (createHTTPServer,
//	           fetchRequestHandler, express/fastify/next adapter,
//	           startStandaloneServer, expressMiddleware, createYoga, …)
//	ws       — WebSocket adapter only (@trpc/server/adapters/ws,
//	           applyWSSHandler, graphql-ws useServer, SubscriptionServer, …)
//	http+ws  — both wired in the same module (the common "queries+mutations
//	           over HTTP, subscriptions over WS" setup)
//
// Detection is intentionally same-file and signal-based (substring/marker
// match on the well-known adapter import + call), mirroring the same-file
// scope of synthesizeTRPC / synthesizeGraphQLResolvers. When no adapter
// signal is present (e.g. a router defined in a standalone module that is
// served from elsewhere) the property is left unset rather than guessed —
// the cell stays honest.

package engine

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// transportHTTP / transportWS / transportBoth are the canonical values of
// the `transport` property stamped on RPC http_endpoint_definition entities.
const (
	transportHTTP = "http"
	transportWS   = "ws"
	transportBoth = "http+ws"
)

// trpcHTTPAdapterSignals are the well-known tRPC adapter imports / calls that
// bind a router to an HTTP request/response transport. Matching any one of
// these in the module is sufficient to claim the HTTP binding.
var trpcHTTPAdapterSignals = []string{
	"@trpc/server/adapters/standalone", // createHTTPServer
	"createHTTPServer",
	"@trpc/server/adapters/express", // createExpressMiddleware
	"createExpressMiddleware",
	"@trpc/server/adapters/fastify", // fastifyTRPCPlugin
	"fastifyTRPCPlugin",
	"@trpc/server/adapters/next", // createNextApiHandler
	"createNextApiHandler",
	"@trpc/server/adapters/fetch", // fetchRequestHandler
	"fetchRequestHandler",
	"@trpc/server/adapters/aws-lambda", // awsLambdaRequestHandler
	"awsLambdaRequestHandler",
}

// trpcWSAdapterSignals are the tRPC adapter imports / calls that bind a
// router to a WebSocket transport.
var trpcWSAdapterSignals = []string{
	"@trpc/server/adapters/ws", // applyWSSHandler
	"applyWSSHandler",
}

// gqlHTTPAdapterSignals are the well-known GraphQL server-setup calls that
// serve a resolver map over HTTP.
var gqlHTTPAdapterSignals = []string{
	"startStandaloneServer", // @apollo/server/standalone
	"expressMiddleware",     // @apollo/server/express4
	"applyMiddleware",       // apollo-server-express (v3)
	"createYoga",            // graphql-yoga
	"createHandler",         // graphql-http
}

// gqlWSAdapterSignals are the GraphQL server-setup calls that serve
// subscriptions over a WebSocket transport.
var gqlWSAdapterSignals = []string{
	"graphql-ws",                   // import marker
	"useServer",                    // graphql-ws/lib/use/ws
	"subscriptions-transport-ws",   // legacy import marker
	"SubscriptionServer",           // legacy subscriptions-transport-ws
	"WebSocketServer",              // ws server paired with useServer
	"@apollo/server/plugin/drainHttpServer",
}

// applyRPCTransportBinding stamps the `transport` property on the RPC
// http_endpoint_definition entities emitted into `entities` at index `from`
// or later. `framework` scopes the stamp to this synthesizer's own
// synthetics (trpc / graphql), so a file that wires both layers does not
// cross-contaminate. The label is derived once per file from the adapter
// signals; when no adapter is wired in this module the property is left
// unset (honest "binding not visible here" rather than a guessed default).
func applyRPCTransportBinding(
	content string,
	entities []types.EntityRecord,
	from int,
	framework string,
	httpSignals, wsSignals []string,
) {
	transport := detectTransportBinding(content, httpSignals, wsSignals)
	if transport == "" {
		return
	}
	for i := from; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Properties == nil || e.Properties["framework"] != framework {
			continue
		}
		e.Properties["transport"] = transport
	}
}

// detectTransportBinding returns the transport label implied by the adapter
// signals present in `content`, or "" when no adapter is wired in this file.
// `httpSignals` / `wsSignals` are the framework-specific signal sets.
func detectTransportBinding(content string, httpSignals, wsSignals []string) string {
	hasHTTP := containsAny(content, httpSignals)
	hasWS := containsAny(content, wsSignals)
	switch {
	case hasHTTP && hasWS:
		return transportBoth
	case hasWS:
		return transportWS
	case hasHTTP:
		return transportHTTP
	default:
		return ""
	}
}

// containsAny reports whether `content` contains any of `signals`.
func containsAny(content string, signals []string) bool {
	for _, s := range signals {
		if strings.Contains(content, s) {
			return true
		}
	}
	return false
}
