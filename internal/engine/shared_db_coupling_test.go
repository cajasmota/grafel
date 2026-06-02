package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

const sdRepo = "acme/shop"

// sdModule builds a synthetic Module node with the deterministic ID the pass
// resolves via moduleNodeID (graph.EntityID(repo,"Module",name,"")).
func sdModule(name string) graph.Entity {
	return graph.Entity{
		ID:         graph.EntityID(sdRepo, "Module", name, ""),
		Name:       name,
		Kind:       "Module",
		Properties: map[string]string{"module": name, "synthetic": "true", "repo": sdRepo},
	}
}

// sdAccess builds a SCOPE.DataAccess entity attributed to module `mod` that
// accesses table `table`.
func sdAccess(id, mod, table string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       "SELECT " + table,
		Kind:       "SCOPE.DataAccess",
		SourceFile: mod + "/repo.go",
		Properties: map[string]string{"module": mod, "repo": sdRepo, "table": table},
	}
}

func sdModuleID(name string) string {
	return graph.EntityID(sdRepo, "Module", name, "")
}

// TestApplySharedDataCoupling_SharedAndPrivate is the value-asserting fixture:
//
//	OrderSvc   ACCESSES_TABLE orders   (shared)
//	BillingSvc ACCESSES_TABLE orders   (shared)
//	OrderSvc   ACCESSES_TABLE audit    (private — only OrderSvc touches it)
//
// Expected:
//   - `orders` → shared=true, accessor_count=2, accessor_modules="BillingSvc,OrderSvc"
//   - `audit`  → shared=false, accessor_count=1
//   - exactly ONE SHARES_DATA edge, between OrderSvc and BillingSvc, listing
//     `orders` as the shared table.
func TestApplySharedDataCoupling_SharedAndPrivate(t *testing.T) {
	orderMod := sdModuleID("OrderSvc")
	billMod := sdModuleID("BillingSvc")

	doc := &graph.Document{
		Repo: sdRepo,
		Entities: []graph.Entity{
			sdModule("OrderSvc"),
			sdModule("BillingSvc"),
			sdAccess("da-order-orders", "OrderSvc", "orders"),
			sdAccess("da-bill-orders", "BillingSvc", "orders"),
			sdAccess("da-order-audit", "OrderSvc", "audit"),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn-order", ToID: "da-order-orders", Kind: "ACCESSES_TABLE"},
			{ID: "r2", FromID: "fn-bill", ToID: "da-bill-orders", Kind: "ACCESSES_TABLE"},
			{ID: "r3", FromID: "fn-order2", ToID: "da-order-audit", Kind: "ACCESSES_TABLE"},
		},
	}

	stats := ApplySharedDataCoupling(doc)
	if stats.Skipped {
		t.Fatalf("expected pass to run, got Skipped=true")
	}
	if stats.SharedTables != 1 {
		t.Errorf("SharedTables = %d, want 1", stats.SharedTables)
	}
	if stats.CouplingEdges != 1 {
		t.Errorf("CouplingEdges = %d, want 1", stats.CouplingEdges)
	}

	byID := make(map[string]graph.Entity, len(doc.Entities))
	for _, e := range doc.Entities {
		byID[e.ID] = e
	}

	// `orders` is shared by exactly 2 modules.
	for _, id := range []string{"da-order-orders", "da-bill-orders"} {
		e := byID[id]
		if e.Properties["shared"] != "true" {
			t.Errorf("%s: shared = %q, want \"true\"", id, e.Properties["shared"])
		}
		if e.Properties["accessor_count"] != "2" {
			t.Errorf("%s: accessor_count = %q, want \"2\"", id, e.Properties["accessor_count"])
		}
		if got := e.Properties["accessor_modules"]; got != "BillingSvc,OrderSvc" {
			t.Errorf("%s: accessor_modules = %q, want \"BillingSvc,OrderSvc\"", id, got)
		}
	}

	// `audit` is private to one module.
	audit := byID["da-order-audit"]
	if audit.Properties["shared"] != "false" {
		t.Errorf("audit: shared = %q, want \"false\"", audit.Properties["shared"])
	}
	if audit.Properties["accessor_count"] != "1" {
		t.Errorf("audit: accessor_count = %q, want \"1\"", audit.Properties["accessor_count"])
	}

	// Exactly one SHARES_DATA edge, between OrderSvc and BillingSvc.
	var shares []graph.Relationship
	for _, r := range doc.Relationships {
		if r.Kind == "SHARES_DATA" {
			shares = append(shares, r)
		}
	}
	if len(shares) != 1 {
		t.Fatalf("want 1 SHARES_DATA edge, got %d", len(shares))
	}
	edge := shares[0]
	// Endpoints must be the two module IDs (smaller first).
	wantA, wantB := orderMod, billMod
	if wantA > wantB {
		wantA, wantB = wantB, wantA
	}
	if edge.FromID != wantA || edge.ToID != wantB {
		t.Errorf("SHARES_DATA endpoints = (%s,%s), want (%s,%s)", edge.FromID, edge.ToID, wantA, wantB)
	}
	if edge.Properties["shared_tables"] != "orders" {
		t.Errorf("shared_tables = %q, want \"orders\"", edge.Properties["shared_tables"])
	}
	if edge.Properties["shared_count"] != "1" {
		t.Errorf("shared_count = %q, want \"1\"", edge.Properties["shared_count"])
	}
	if edge.Properties["coupling"] != "shared_data" {
		t.Errorf("coupling = %q, want \"shared_data\"", edge.Properties["coupling"])
	}
	if edge.Properties["provenance"] != "SHARED_DB_COUPLING" {
		t.Errorf("provenance = %q, want \"SHARED_DB_COUPLING\"", edge.Properties["provenance"])
	}

	// Idempotent: a second run must not add a second SHARES_DATA edge.
	before := len(doc.Relationships)
	ApplySharedDataCoupling(doc)
	count := 0
	for _, r := range doc.Relationships {
		if r.Kind == "SHARES_DATA" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("after second run: SHARES_DATA count = %d, want 1 (idempotent)", count)
	}
	if len(doc.Relationships) != before {
		t.Errorf("second run changed relationship count %d → %d (not idempotent)", before, len(doc.Relationships))
	}
}

// TestApplySharedDataCoupling_SingleAccessorNoEdge asserts a table touched by a
// single module produces NO coupling edge and shared=false (not len>0 logic).
func TestApplySharedDataCoupling_SingleAccessorNoEdge(t *testing.T) {
	doc := &graph.Document{
		Repo: sdRepo,
		Entities: []graph.Entity{
			sdModule("OrderSvc"),
			sdAccess("da1", "OrderSvc", "orders"),
			sdAccess("da2", "OrderSvc", "orders"), // same module, second op
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn1", ToID: "da1", Kind: "ACCESSES_TABLE"},
			{ID: "r2", FromID: "fn2", ToID: "da2", Kind: "ACCESSES_TABLE"},
		},
	}
	stats := ApplySharedDataCoupling(doc)
	if stats.SharedTables != 0 {
		t.Errorf("SharedTables = %d, want 0", stats.SharedTables)
	}
	if stats.CouplingEdges != 0 {
		t.Errorf("CouplingEdges = %d, want 0", stats.CouplingEdges)
	}
	for _, r := range doc.Relationships {
		if r.Kind == "SHARES_DATA" {
			t.Errorf("unexpected SHARES_DATA edge for single-module table")
		}
	}
	for _, e := range doc.Entities {
		if e.Kind == "SCOPE.DataAccess" && e.Properties["shared"] != "false" {
			t.Errorf("%s: shared = %q, want \"false\"", e.ID, e.Properties["shared"])
		}
	}
}

// TestApplySharedDataCoupling_ExternalNotCounted asserts that the "_external"
// catch-all module is never counted as a distinct accessor, so unattributed
// access cannot fabricate coupling.
func TestApplySharedDataCoupling_ExternalNotCounted(t *testing.T) {
	doc := &graph.Document{
		Repo: sdRepo,
		Entities: []graph.Entity{
			sdModule("OrderSvc"),
			sdAccess("da1", "OrderSvc", "orders"),
			// second accessor is unattributed (no module → _external).
			{ID: "da2", Kind: "SCOPE.DataAccess", Name: "SELECT orders",
				Properties: map[string]string{"repo": sdRepo, "table": "orders"}},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn1", ToID: "da1", Kind: "ACCESSES_TABLE"},
		},
	}
	stats := ApplySharedDataCoupling(doc)
	if stats.SharedTables != 0 {
		t.Errorf("SharedTables = %d, want 0 (external not a real accessor)", stats.SharedTables)
	}
	if stats.CouplingEdges != 0 {
		t.Errorf("CouplingEdges = %d, want 0", stats.CouplingEdges)
	}
}

// TestApplySharedDataCoupling_JoinsCollection asserts the Mongo
// JOINS_COLLECTION signal contributes accessors, and two modules joining the
// same collection are flagged shared with a coupling edge.
func TestApplySharedDataCoupling_JoinsCollection(t *testing.T) {
	doc := &graph.Document{
		Repo: sdRepo,
		Entities: []graph.Entity{
			sdModule("CartSvc"),
			sdModule("ReportSvc"),
			sdAccess("da-cart", "CartSvc", "products"),
			sdAccess("da-report", "ReportSvc", "products"),
		},
		Relationships: []graph.Relationship{
			{ID: "j1", FromID: "agg-cart", ToID: "da-cart", Kind: "JOINS_COLLECTION",
				Properties: map[string]string{"from": "products"}},
			{ID: "j2", FromID: "agg-report", ToID: "da-report", Kind: "JOINS_COLLECTION",
				Properties: map[string]string{"from": "products"}},
		},
	}
	stats := ApplySharedDataCoupling(doc)
	if stats.SharedTables != 1 {
		t.Errorf("SharedTables = %d, want 1", stats.SharedTables)
	}
	if stats.CouplingEdges != 1 {
		t.Errorf("CouplingEdges = %d, want 1", stats.CouplingEdges)
	}
	var found bool
	for _, r := range doc.Relationships {
		if r.Kind == "SHARES_DATA" {
			found = true
			if !strings.Contains(r.Properties["shared_tables"], "products") {
				t.Errorf("shared_tables = %q, want it to contain \"products\"", r.Properties["shared_tables"])
			}
		}
	}
	if !found {
		t.Error("expected a SHARES_DATA edge for the co-joined collection")
	}
}

// TestApplySharedDataCoupling_NoModulesSkips asserts an honest skip when module
// aggregation has not run (no Module nodes → no attribution possible).
func TestApplySharedDataCoupling_NoModulesSkips(t *testing.T) {
	doc := &graph.Document{
		Repo: sdRepo,
		Entities: []graph.Entity{
			sdAccess("da1", "OrderSvc", "orders"),
			sdAccess("da2", "BillingSvc", "orders"),
		},
	}
	stats := ApplySharedDataCoupling(doc)
	if !stats.Skipped {
		t.Errorf("expected Skipped=true with no Module nodes, got %+v", stats)
	}
}
