// http_endpoint_middleware_chain.go — the shared, framework-agnostic
// middleware-chain → endpoint property contract for the engine-side
// synthesizers (Python: Django/DRF/FastAPI; Java/Spring).
//
// PR #3777 (Go gin/echo/chi) and #2853 (JS/TS) already bind an ORDERED
// middleware chain to each synthetic http_endpoint_definition. This file ports
// the SAME contract to the Python and Spring endpoints so the middleware view
// is queryable uniformly across every backend stack.
//
// The byte-for-byte contract (matching internal/custom/golang/route_middleware.go):
//
//	middleware_chain  — JSON array of {name, expr, scope, order, auth_kind?},
//	                    OUTERMOST-first, so index 0 is the first middleware a
//	                    request traverses.
//	middleware_count  — decimal count of resolved middleware (>0 ⇒ chain bound).
//	middleware_names  — comma-joined middleware symbols in chain order.
//	middleware_scope  — "+"-joined set of contributing scopes (outermost-first).
//
// Auth middleware appears IN the chain (auth_kind set), never double-modeled.
//
// Refs #3628 (child: bind ordered chain to endpoints — Django/DRF/FastAPI/Spring).
package engine

import (
	"encoding/json"
	"strconv"
	"strings"
)

// middlewareEntry is one resolved middleware bound to an endpoint. The JSON
// shape is identical to the Go pass's goMiddlewareEntry so the
// `middleware_chain` property is structurally interchangeable across stacks.
type middlewareEntry struct {
	Name     string `json:"name"`
	Expr     string `json:"expr"`
	Scope    string `json:"scope"`
	Order    int    `json:"order"`
	AuthKind string `json:"auth_kind,omitempty"`
}

// stampMiddlewareChainEntries writes the resolved ordered chain onto an endpoint
// op's Properties map using the cross-stack contract. The scopeOrder argument
// lists every scope name OUTERMOST-first (e.g. {"global","view"} for Python,
// {"filter","interceptor"} for Spring) so middleware_scope is rendered in
// request-traversal order. No-op on an empty chain so an un-wrapped route stays
// unstamped.
func stampMiddlewareChainEntries(props map[string]string, chain []middlewareEntry, scopeOrder []string) {
	if props == nil || len(chain) == 0 {
		return
	}
	for i := range chain {
		chain[i].Order = i
	}
	if encoded := encodeMiddlewareEntries(chain); encoded != "" {
		props["middleware_chain"] = encoded
	}
	props["middleware_count"] = strconv.Itoa(len(chain))
	props["middleware_names"] = middlewareEntryNames(chain)
	props["middleware_scope"] = middlewareEntryScope(chain, scopeOrder)
}

// encodeMiddlewareEntries JSON-encodes the chain for the middleware_chain prop.
func encodeMiddlewareEntries(chain []middlewareEntry) string {
	b, err := json.Marshal(chain)
	if err != nil {
		return ""
	}
	return string(b)
}

// middlewareEntryNames returns the comma-joined middleware symbols in chain
// order (the MCP signal key).
func middlewareEntryNames(chain []middlewareEntry) string {
	names := make([]string, 0, len(chain))
	for _, e := range chain {
		names = append(names, e.Name)
	}
	return strings.Join(names, ",")
}

// middlewareEntryScope returns the "+"-joined set of contributing scopes,
// rendered in the caller's outermost-first scopeOrder.
func middlewareEntryScope(chain []middlewareEntry, scopeOrder []string) string {
	present := map[string]bool{}
	for _, e := range chain {
		present[e.Scope] = true
	}
	var parts []string
	for _, s := range scopeOrder {
		if present[s] {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "+")
}

// dedupeMiddlewareEntries removes duplicate Expr entries preserving order. A
// middleware registered at two scopes keeps its first (outer) occurrence — it
// runs once at the outer position.
func dedupeMiddlewareEntries(in []middlewareEntry) []middlewareEntry {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := in[:0:0]
	for _, e := range in {
		if seen[e.Expr] {
			continue
		}
		seen[e.Expr] = true
		out = append(out, e)
	}
	return out
}

// middlewareAuthKind classifies a middleware/permission/filter symbol into an
// auth_kind tag (shared across Python and Spring resolvers) so auth middleware
// is annotated IN the chain rather than double-modeled. Returns "" for a
// non-auth middleware.
func middlewareAuthKind(sym string) string {
	l := strings.ToLower(sym)
	switch {
	case strings.Contains(l, "isauthenticated"),
		strings.Contains(l, "isadmin"),
		strings.Contains(l, "permission"),
		strings.Contains(l, "authentication"),
		strings.Contains(l, "authmiddleware"),
		strings.Contains(l, "jwt"),
		strings.Contains(l, "oauth"),
		strings.Contains(l, "login"),
		strings.Contains(l, "security"),
		strings.Contains(l, "auth"):
		return "auth"
	default:
		return ""
	}
}
