package main

import "testing"

// TestRPCTransportBinding_FullPipeline is the real-data integration guard for
// #2906. It runs the production indexer over a small multi-file JS/TS corpus
// (a tRPC standalone HTTP+WS server and an Apollo standalone HTTP GraphQL
// server) and asserts that the RPC procedure / resolver synthetics carry the
// `transport` property derived from the server-setup adapter — proving the
// binding survives the full extract → engine → pass pipeline, not just the
// isolated synthesizer unit test.
func TestRPCTransportBinding_FullPipeline(t *testing.T) {
	doc := runIndexerOn(t, "testdata/transport2906_jsts_rpc", "transport_2906", nil)
	if len(doc.Entities) == 0 {
		t.Fatalf("no entities emitted from transport fixture corpus")
	}

	// Collect transport per (framework) over the http_endpoint_definition
	// entities the RPC synthesizers emitted.
	trpcTransports := map[string]bool{}
	gqlTransports := map[string]bool{}
	var trpcCount, gqlCount int
	for _, e := range doc.Entities {
		if e.Kind != "http_endpoint_definition" || e.Properties == nil {
			continue
		}
		switch e.Properties["framework"] {
		case "trpc":
			trpcCount++
			if tr := e.Properties["transport"]; tr != "" {
				trpcTransports[tr] = true
			}
		case "graphql":
			gqlCount++
			if tr := e.Properties["transport"]; tr != "" {
				gqlTransports[tr] = true
			}
		}
	}

	if trpcCount == 0 {
		t.Fatalf("no tRPC endpoints synthesised from corpus")
	}
	if gqlCount == 0 {
		t.Fatalf("no GraphQL resolver endpoints synthesised from corpus")
	}

	// tRPC server.ts wires BOTH the standalone HTTP adapter and the WS
	// adapter against the same router → http+ws.
	if !trpcTransports["http+ws"] {
		t.Errorf("tRPC transport: want http+ws stamped, got set %v", trpcTransports)
	}
	// Apollo standalone serves the resolver map over HTTP only.
	if !gqlTransports["http"] {
		t.Errorf("GraphQL transport: want http stamped, got set %v", gqlTransports)
	}
}
