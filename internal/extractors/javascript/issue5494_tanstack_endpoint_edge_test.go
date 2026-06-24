// issue5494_tanstack_endpoint_edge_test.go — issue #5494 proving tests.
//
// Each TanStack query/mutation operation (#5492) is linked to the HTTP endpoint
// its queryFn/mutationFn fetcher calls:
//
//   - inline `() => fetch('/api/users')` / `() => axios.get('/api/orders')` →
//     a USES edge to the endpoint stub `http:<VERB>:<path>` — the SAME synthetic
//     Name the consumer-side HTTP synthesiser emits, so the cross-repo linker
//     binds query→server-route.
//   - named ref `mutationFn: createUser` → a CALLS edge to the named data fn;
//     the existing call-graph + http-client edges carry it transitively to the
//     endpoint.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func TestIssue5494_TanstackEndpointEdge(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/TanstackEndpointEdge.tsx")

	ops := append(
		bySubtype(ents, "SCOPE.Operation", "tanstack_query"),
		bySubtype(ents, "SCOPE.Operation", "tanstack_mutation")...,
	)
	if len(ops) == 0 {
		t.Fatalf("no tanstack operations extracted: %s", dumpKinds(ents))
	}

	// edgeTo reports whether any op carries a relationship of the given kind to
	// the given ToID.
	edgeTo := func(kind, toID string) bool {
		for _, op := range ops {
			for _, r := range op.Relationships {
				if r.Kind == kind && r.ToID == toID {
					return true
				}
			}
		}
		return false
	}

	// Inline GET fetch → USES edge to the /api/users endpoint stub.
	if !edgeTo("USES", "http:GET:/api/users") {
		t.Errorf("missing USES edge to http:GET:/api/users; edges: %s", dumpEdges(ops))
	}
	// Inline axios.get → USES edge to the /api/orders endpoint stub.
	if !edgeTo("USES", "http:GET:/api/orders") {
		t.Errorf("missing USES edge to http:GET:/api/orders; edges: %s", dumpEdges(ops))
	}
	// Inline POST fetch → USES edge to http:POST:/api/users.
	if !edgeTo("USES", "http:POST:/api/users") {
		t.Errorf("missing USES edge to http:POST:/api/users; edges: %s", dumpEdges(ops))
	}
	// Named ref → CALLS edge to createUser.
	if !edgeTo("CALLS", "createUser") {
		t.Errorf("missing CALLS edge to createUser; edges: %s", dumpEdges(ops))
	}

	// Regression guard (#3171): the queryKey strings must NEVER be linked as an
	// endpoint. No USES edge may target an http:* stub derived from a key label.
	for _, op := range ops {
		for _, r := range op.Relationships {
			if r.Kind == "USES" &&
				(r.ToID == "http:GET:/users" || r.ToID == "http:GET:/orders") {
				t.Errorf("queryKey leaked as endpoint stub: %s", r.ToID)
			}
		}
	}

	// Every fetcher-linked op stamps fetcher_linked=true and the edges carry the
	// tanstack_query provenance.
	for _, op := range ops {
		for _, r := range op.Relationships {
			if (r.Kind == "USES" || r.Kind == "CALLS") && r.Properties["via"] == propViaTanstackQuery5494 {
				if op.Properties["fetcher_linked"] != "true" {
					t.Errorf("%s carries a fetcher edge but fetcher_linked != true", op.Name)
				}
			}
		}
	}
}

// propViaTanstackQuery5494 mirrors the unexported provenance stamp on the edges
// (kept local to the external test package).
const propViaTanstackQuery5494 = "tanstack_query"

// dumpEdges renders the USES/CALLS edges across the given ops for failure output.
func dumpEdges(ops []types.EntityRecord) string {
	out := ""
	for _, op := range ops {
		for _, r := range op.Relationships {
			if r.Kind == "USES" || r.Kind == "CALLS" {
				out += op.Name + " -" + r.Kind + "-> " + r.ToID + "; "
			}
		}
	}
	if out == "" {
		return "(none)"
	}
	return out
}
