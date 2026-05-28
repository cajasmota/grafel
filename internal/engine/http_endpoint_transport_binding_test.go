package engine

import "testing"

// transportOf returns the `transport` property of the first
// http_endpoint_definition with the given framework, or "" if none carries it.
func transportOf(ents []entityRec, framework string) string {
	for _, e := range ents {
		if e.kind == httpEndpointDefinitionKind &&
			e.framework == framework && e.transport != "" {
			return e.transport
		}
	}
	return ""
}

type entityRec struct {
	kind, framework, transport string
}

// collectRPCDefs reads a committed fixture under testdata/fixtures/typescript/
// and returns the RPC http_endpoint_definition entities the synthesis pass
// emits for it, projected to (kind, framework, transport).
func collectRPCDefs(t *testing.T, path, fixture string) []entityRec {
	t.Helper()
	src := readBackendFixture(t, fixture)
	res := runDetectWS(t, "typescript", path, src)
	var out []entityRec
	for _, e := range res.Entities {
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		out = append(out, entityRec{
			kind:      e.Kind,
			framework: e.Properties["framework"],
			transport: e.Properties["transport"],
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// tRPC transport binding (#2906)
// ---------------------------------------------------------------------------

func TestTRPCTransport_HTTPStandaloneAdapter(t *testing.T) {
	defs := collectRPCDefs(t, "server.ts", "trpc_transport_http.ts")
	if len(defs) == 0 {
		t.Fatalf("no tRPC endpoints emitted")
	}
	if got := transportOf(defs, "trpc"); got != transportHTTP {
		t.Fatalf("transport = %q, want %q", got, transportHTTP)
	}
}

func TestTRPCTransport_WSAdapter(t *testing.T) {
	defs := collectRPCDefs(t, "ws.ts", "trpc_transport_ws.ts")
	if len(defs) == 0 {
		t.Fatalf("no tRPC endpoints emitted")
	}
	if got := transportOf(defs, "trpc"); got != transportWS {
		t.Fatalf("transport = %q, want %q", got, transportWS)
	}
}

func TestTRPCTransport_HTTPAndWS(t *testing.T) {
	defs := collectRPCDefs(t, "both.ts", "trpc_transport_http_ws.ts")
	if len(defs) == 0 {
		t.Fatalf("no tRPC endpoints emitted")
	}
	if got := transportOf(defs, "trpc"); got != transportBoth {
		t.Fatalf("transport = %q, want %q", got, transportBoth)
	}
}

func TestTRPCTransport_NoAdapterLeavesUnset(t *testing.T) {
	// Router defined in a standalone module with no adapter wired — the
	// transport binding is not visible here, so the property stays unset.
	defs := collectRPCDefs(t, "router.ts", "trpc_transport_none.ts")
	if len(defs) == 0 {
		t.Fatalf("no tRPC endpoints emitted")
	}
	if got := transportOf(defs, "trpc"); got != "" {
		t.Fatalf("transport = %q, want unset (no adapter in module)", got)
	}
}

// ---------------------------------------------------------------------------
// GraphQL resolver transport binding (#2906)
// ---------------------------------------------------------------------------

func TestGraphQLTransport_StandaloneHTTP(t *testing.T) {
	defs := collectRPCDefs(t, "apollo.ts", "graphql_transport_http.ts")
	if len(defs) == 0 {
		t.Fatalf("no GraphQL resolver endpoints emitted")
	}
	if got := transportOf(defs, "graphql"); got != transportHTTP {
		t.Fatalf("transport = %q, want %q", got, transportHTTP)
	}
}

func TestGraphQLTransport_HTTPAndWS(t *testing.T) {
	defs := collectRPCDefs(t, "apollo_ws.ts", "graphql_transport_http_ws.ts")
	if len(defs) == 0 {
		t.Fatalf("no GraphQL resolver endpoints emitted")
	}
	if got := transportOf(defs, "graphql"); got != transportBoth {
		t.Fatalf("transport = %q, want %q", got, transportBoth)
	}
}
