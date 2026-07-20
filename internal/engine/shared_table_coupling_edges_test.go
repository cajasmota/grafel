package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// stAccess builds a SCOPE.DataAccess entity in repo `repo` accessing `table`
// with SQL operation `op` (SELECT = read, INSERT/UPDATE/… = write).
func stAccess(id, repo, table, op string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       op + " " + table,
		Kind:       "SCOPE.DataAccess",
		SourceFile: repo + "/repo.go",
	}.WithProperties(map[string]string{"repo": repo, "table": table, "operation": op})
}

// countSharesTableWith returns the SHARES_TABLE_WITH edges in doc.
func countSharesTableWith(doc *graph.Document) []graph.Relationship {
	var out []graph.Relationship
	for _, r := range doc.Relationships {
		if r.Kind == "SHARES_TABLE_WITH" {
			out = append(out, r)
		}
	}
	return out
}

// TestSharedTableCoupling_HappyPath — two DISTINCT services both WRITE the same
// physical table → exactly one SHARES_TABLE_WITH edge between their canonical
// service nodes, writer=both, confidence=high.
func TestSharedTableCoupling_HappyPath(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			stAccess("da-order", "acme/order-svc", "orders", "INSERT"),
			stAccess("da-bill", "acme/billing-svc", "public.orders", "UPDATE"),
		},
	}

	stats := ApplySharedTableCouplingEdges(doc)
	edges := countSharesTableWith(doc)

	if len(edges) != 1 {
		t.Fatalf("want exactly 1 SHARES_TABLE_WITH edge, got %d (stats=%+v)", len(edges), stats)
	}
	e := edges[0]
	// Endpoints are the canonical service nodes, smaller ID first.
	fromID := "service:acme/billing-svc"
	toID := "service:acme/order-svc"
	if e.FromID != fromID || e.ToID != toID {
		t.Errorf("endpoints = %s -> %s, want %s -> %s", e.FromID, e.ToID, fromID, toID)
	}
	if e.PropGet("table") != "orders" {
		t.Errorf("table = %q, want normalised %q", e.PropGet("table"), "orders")
	}
	if e.PropGet("writer") != "both" {
		t.Errorf("writer = %q, want both", e.PropGet("writer"))
	}
	if e.PropGet("access_from") != "write" || e.PropGet("access_to") != "write" {
		t.Errorf("access kinds = %q/%q, want write/write", e.PropGet("access_from"), e.PropGet("access_to"))
	}
	if e.PropGet("confidence") != "high" {
		t.Errorf("confidence = %q, want high", e.PropGet("confidence"))
	}
	if e.PropGet("provenance") != "SHARED_TABLE_COUPLING" {
		t.Errorf("provenance = %q", e.PropGet("provenance"))
	}
	// The service nodes must have been minted.
	if stats.ServicesMinted != 2 {
		t.Errorf("services minted = %d, want 2", stats.ServicesMinted)
	}
	if !stHasEntity(doc, "service:acme/order-svc") || !stHasEntity(doc, "service:acme/billing-svc") {
		t.Errorf("expected both synthetic service nodes to exist")
	}
}

// TestSharedTableCoupling_ReadWriteMix — one reader + one writer of the same
// table IS real mutable coupling → edge emitted, writer points at the writing
// side.
func TestSharedTableCoupling_ReadWriteMix(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			stAccess("da-a", "acme/a-svc", "orders", "SELECT"),
			stAccess("da-b", "acme/b-svc", "orders", "UPDATE"),
		},
	}
	ApplySharedTableCouplingEdges(doc)
	edges := countSharesTableWith(doc)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	e := edges[0]
	// from=a-svc(read), to=b-svc(write) since "service:acme/a-svc" < "service:acme/b-svc".
	if e.PropGet("writer") != "to" {
		t.Errorf("writer = %q, want to", e.PropGet("writer"))
	}
	if e.PropGet("access_from") != "read" || e.PropGet("access_to") != "write" {
		t.Errorf("access kinds = %q/%q, want read/write", e.PropGet("access_from"), e.PropGet("access_to"))
	}
}

// TestSharedTableCoupling_NoOp_SameServiceTwoModules — the SAME service accesses
// one table from two of its own modules → NOT cross-service coupling, no edge.
func TestSharedTableCoupling_NoOp_SameServiceTwoModules(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			func() graph.Entity {
				e := stAccess("da-1", "acme/mono", "orders", "INSERT")
				e.PropSet("module", "checkout")
				return e
			}(),
			func() graph.Entity {
				e := stAccess("da-2", "acme/mono", "orders", "UPDATE")
				e.PropSet("module", "fulfilment")
				return e
			}(),
		},
	}
	ApplySharedTableCouplingEdges(doc)
	if edges := countSharesTableWith(doc); len(edges) != 0 {
		t.Fatalf("same-service multi-module access must not couple; got %d edges", len(edges))
	}
}

// TestSharedTableCoupling_NoOp_SingleService — only one service touches the
// table → no coupling.
func TestSharedTableCoupling_NoOp_SingleService(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			stAccess("da-1", "acme/solo", "orders", "INSERT"),
			stAccess("da-2", "acme/solo", "orders", "SELECT"),
		},
	}
	ApplySharedTableCouplingEdges(doc)
	if edges := countSharesTableWith(doc); len(edges) != 0 {
		t.Fatalf("single-service access must not couple; got %d edges", len(edges))
	}
}

// TestSharedTableCoupling_NoOp_ReadOnlyPair — two services both only READ the
// table → real sharing but no mutable coupling, so no edge (the smell is about
// a write that another service depends on).
func TestSharedTableCoupling_NoOp_ReadOnlyPair(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			stAccess("da-a", "acme/a-svc", "orders", "SELECT"),
			stAccess("da-b", "acme/b-svc", "orders", "SELECT"),
		},
	}
	ApplySharedTableCouplingEdges(doc)
	if edges := countSharesTableWith(doc); len(edges) != 0 {
		t.Fatalf("read-only pair must not emit mutable coupling; got %d edges", len(edges))
	}
}

// TestSharedTableCoupling_NoOp_DynamicTable — an unresolved/dynamic table
// (UNKNOWN) is skipped even across distinct writing services.
func TestSharedTableCoupling_NoOp_DynamicTable(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			stAccess("da-a", "acme/a-svc", "UNKNOWN", "INSERT"),
			stAccess("da-b", "acme/b-svc", "UNKNOWN", "UPDATE"),
			stAccess("da-c", "acme/c-svc", "", "DELETE"),
		},
	}
	ApplySharedTableCouplingEdges(doc)
	if edges := countSharesTableWith(doc); len(edges) != 0 {
		t.Fatalf("dynamic/unresolved table must be skipped; got %d edges", len(edges))
	}
}

// TestSharedTableCoupling_Idempotent — a second run mints no duplicate edges.
func TestSharedTableCoupling_Idempotent(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			stAccess("da-order", "acme/order-svc", "orders", "INSERT"),
			stAccess("da-bill", "acme/billing-svc", "orders", "UPDATE"),
		},
	}
	ApplySharedTableCouplingEdges(doc)
	first := len(countSharesTableWith(doc))
	ApplySharedTableCouplingEdges(doc)
	second := len(countSharesTableWith(doc))
	if first != 1 || second != 1 {
		t.Fatalf("expected 1 edge stable across runs, got first=%d second=%d", first, second)
	}
}

// TestSharedTableCoupling_AccessesTableEdge — coupling is detected through
// ACCESSES_TABLE edges (caller's repo attributes the access) too, not only the
// DataAccess node's own repo.
func TestSharedTableCoupling_AccessesTableEdge(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			graph.Entity{ID: "fn-a", Kind: "Function"}.WithProperties(map[string]string{"repo": "acme/a-svc"}),
			graph.Entity{ID: "fn-b", Kind: "Function"}.WithProperties(map[string]string{"repo": "acme/b-svc"}),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{FromID: "fn-a", ToID: "da-x", Kind: "ACCESSES_TABLE"}.WithProperties(map[string]string{"table": "orders", "operation": "INSERT"}),
			graph.Relationship{FromID: "fn-b", ToID: "da-y", Kind: "ACCESSES_TABLE"}.WithProperties(map[string]string{"table": "orders", "operation": "SELECT"}),
		},
	}
	ApplySharedTableCouplingEdges(doc)
	edges := countSharesTableWith(doc)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge via ACCESSES_TABLE attribution, got %d", len(edges))
	}
}

func stHasEntity(doc *graph.Document, id string) bool {
	for _, e := range doc.Entities {
		if e.ID == id {
			return true
		}
	}
	return false
}
