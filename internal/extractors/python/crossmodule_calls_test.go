// crossmodule_calls_test.go — issue #1694 coverage for Python cross-module
// `<alias>.<leaf>(...)` CALL-target extraction.
//
// These tests exercise the extractor in isolation: they verify that the
// emitted CALLS edges carry the `import_alias` / `call_leaf` property
// hints the resolver needs. Resolver-side binding is covered separately
// in internal/resolve/imports_test.go (issue #1694 resolver test).

package python

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractEntities runs the Python extractor on the given file and returns
// its entity records, failing the test on any error.
func extractEntities(t *testing.T, path, content string) []types.EntityRecord {
	t.Helper()
	ents, err := (&Extractor{}).Extract(context.Background(), extractor.FileInput{
		Path:    path,
		Content: []byte(content),
	})
	if err != nil {
		t.Fatalf("Extract(%q) returned error: %v", path, err)
	}
	return ents
}

// findCallsFrom returns every CALLS relationship emitted from the entity
// whose Name == callerName in the given entity slice. Methods are looked
// up under their dotted "Class.method" form.
func findCallsFrom(ents []types.EntityRecord, callerName string) []types.RelationshipRecord {
	for i := range ents {
		if ents[i].Name != callerName {
			continue
		}
		var out []types.RelationshipRecord
		for _, r := range ents[i].Relationships {
			if r.Kind == "CALLS" {
				out = append(out, r)
			}
		}
		return out
	}
	return nil
}

// TestCrossModuleCall_FromImportSubmodule covers the canonical PlaceOrderSaga
// failure case: `from . import steps; steps.create_order(...)`. The extractor
// must emit a CALLS edge with ToID="create_order" and Properties carrying
// {import_alias: "steps", call_leaf: "create_order"} so the resolver can
// bind the edge cross-file.
func TestCrossModuleCall_FromImportSubmodule(t *testing.T) {
	src := `from . import steps

class PlaceOrderSaga:
    def run(self):
        steps.create_order()
        steps.charge_payment()
        steps.reserve_inventory()
`
	ents := extractEntities(t, "services/order_saga/orchestrator.py", src)
	calls := findCallsFrom(ents, "PlaceOrderSaga.run")
	if len(calls) != 3 {
		t.Fatalf("want 3 CALLS edges from PlaceOrderSaga.run, got %d (%+v)", len(calls), calls)
	}
	wantLeaves := map[string]bool{
		"create_order":       true,
		"charge_payment":     true,
		"reserve_inventory":  true,
	}
	for _, c := range calls {
		if !wantLeaves[c.ToID] {
			t.Errorf("unexpected CALLS ToID %q", c.ToID)
		}
		if c.Properties == nil {
			t.Errorf("CALLS to %q missing Properties — expected import_alias hint", c.ToID)
			continue
		}
		if got := c.Properties["import_alias"]; got != "steps" {
			t.Errorf("CALLS to %q: import_alias = %q, want %q", c.ToID, got, "steps")
		}
		if got := c.Properties["call_leaf"]; got != c.ToID {
			t.Errorf("CALLS to %q: call_leaf = %q, want %q", c.ToID, got, c.ToID)
		}
		// The disposition_hint should NOT be set when import_alias is — the
		// alias hint disambiguates the call precisely.
		if _, has := c.Properties["disposition_hint"]; has {
			t.Errorf("CALLS to %q: disposition_hint set together with import_alias", c.ToID)
		}
	}
}

// TestCrossModuleCall_PlainImport covers the `import x; x.fn(...)` shape.
// The extractor must stamp import_alias="x" with the bare leaf as ToID.
func TestCrossModuleCall_PlainImport(t *testing.T) {
	src := `import billing

def checkout():
    billing.charge_card()
`
	ents := extractEntities(t, "services/orders/checkout.py", src)
	calls := findCallsFrom(ents, "checkout")
	if len(calls) != 1 {
		t.Fatalf("want 1 CALLS edge from checkout, got %d (%+v)", len(calls), calls)
	}
	c := calls[0]
	if c.ToID != "charge_card" {
		t.Errorf("CALLS ToID = %q, want %q", c.ToID, "charge_card")
	}
	if c.Properties["import_alias"] != "billing" {
		t.Errorf("import_alias = %q, want %q", c.Properties["import_alias"], "billing")
	}
	if c.Properties["call_leaf"] != "charge_card" {
		t.Errorf("call_leaf = %q, want %q", c.Properties["call_leaf"], "charge_card")
	}
}

// TestCrossModuleCall_FromImportFunction covers the `from x import y; y(...)`
// shape — a direct call (no attribute). This should bind via the existing
// bare-name resolver path; no import_alias hint is needed and none must be
// stamped (the alias would point the resolver away from the right target).
func TestCrossModuleCall_FromImportFunction(t *testing.T) {
	src := `from billing import charge_card

def checkout():
    charge_card()
`
	ents := extractEntities(t, "services/orders/checkout.py", src)
	calls := findCallsFrom(ents, "checkout")
	if len(calls) != 1 {
		t.Fatalf("want 1 CALLS edge from checkout, got %d (%+v)", len(calls), calls)
	}
	c := calls[0]
	if c.ToID != "charge_card" {
		t.Errorf("CALLS ToID = %q, want %q", c.ToID, "charge_card")
	}
	// Direct bare-name call — no import_alias property should be stamped.
	if c.Properties != nil {
		if alias := c.Properties["import_alias"]; alias != "" {
			t.Errorf("bare-name call must not carry import_alias (got %q)", alias)
		}
	}
}

// TestCrossModuleCall_SelfRecursionUnchanged confirms that bare-name
// self-recursion (a function calling itself by name) is still dropped, even
// when the alias-extraction path is active.
func TestCrossModuleCall_SelfRecursionUnchanged(t *testing.T) {
	src := `import sys

def factorial(n):
    if n <= 1:
        return 1
    return factorial(n - 1)
`
	ents := extractEntities(t, "math/factorial.py", src)
	calls := findCallsFrom(ents, "factorial")
	for _, c := range calls {
		if c.ToID == "factorial" {
			t.Errorf("self-recursion edge should have been dropped, got %+v", c)
		}
	}
}

// TestCrossModuleCall_NoImportNoAlias confirms that an attribute call whose
// receiver is NOT an import binding (e.g. `obj.fn()` where `obj` is a local
// variable) gets the prior ambiguous treatment: bare leaf as ToID, with the
// disposition_hint="ambiguous" property, and no import_alias.
func TestCrossModuleCall_NoImportNoAlias(t *testing.T) {
	src := `def consume(queue):
    queue.pop()
`
	ents := extractEntities(t, "workers/consumer.py", src)
	calls := findCallsFrom(ents, "consume")
	if len(calls) != 1 {
		t.Fatalf("want 1 CALLS edge from consume, got %d (%+v)", len(calls), calls)
	}
	c := calls[0]
	if c.ToID != "pop" {
		t.Errorf("CALLS ToID = %q, want %q", c.ToID, "pop")
	}
	if c.Properties != nil {
		if alias := c.Properties["import_alias"]; alias != "" {
			t.Errorf("non-import receiver must not carry import_alias (got %q)", alias)
		}
		if c.Properties["disposition_hint"] != "ambiguous" {
			t.Errorf("non-import receiver: disposition_hint = %q, want %q", c.Properties["disposition_hint"], "ambiguous")
		}
	} else {
		t.Errorf("expected disposition_hint=ambiguous Property, got nil Properties")
	}
}

// TestRelativeImport_AbsoluteSourceModule confirms that
// `from . import steps` inside services/order_saga/app/orchestrator.py
// produces an IMPORTS edge whose source_module is the absolute dotted form
// (`services.order_saga.app`), not the literal "." text.
func TestRelativeImport_AbsoluteSourceModule(t *testing.T) {
	src := `from . import steps
from ..shared import util
`
	ents := extractEntities(t, "services/order_saga/app/orchestrator.py", src)

	// IMPORTS edges live on the file entity (entities[0]) after the
	// attachImportRelationships lift.
	if len(ents) == 0 {
		t.Fatalf("no entities returned")
	}
	var imports []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				imports = append(imports, r)
			}
		}
	}
	if len(imports) == 0 {
		t.Fatalf("no IMPORTS edges emitted")
	}

	want := map[string]string{
		"steps": "services.order_saga.app",
		"util":  "services.order_saga.shared",
	}
	got := map[string]string{}
	for _, imp := range imports {
		if imp.Properties == nil {
			continue
		}
		local := imp.Properties["local_name"]
		mod := imp.Properties["source_module"]
		if local == "" {
			continue
		}
		got[local] = mod
	}
	for local, wantMod := range want {
		if got[local] != wantMod {
			t.Errorf("import %q: source_module = %q, want %q", local, got[local], wantMod)
		}
	}
}
