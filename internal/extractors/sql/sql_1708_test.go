package sql_test

// Issue #1708: PostgreSQL `CREATE TRIGGER ... AFTER UPDATE OF <col> ON <table>`
// (column-list event form) was not matched by triggerRE, so the trigger
// entity and the FIRES / DEFINED_ON edges were never emitted on real-world
// fixtures (polyglot-platform-services-orders 002_views_procs_triggers.sql).
//
// The fixture migration_007_trigger_update_of.sql exercises both single-column
// (`UPDATE OF status`) and multi-column (`UPDATE OF status, total_cents`)
// forms. The fix relaxes the DML segment of triggerRE to skip ANY tokens
// between the event keyword and the `ON` clause.

import "testing"

func loadFixture1708(t *testing.T) []byte {
	t.Helper()
	return loadFixture(t, "migration_007_trigger_update_of.sql")
}

func Test1708_TriggerColumnListExtracted(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1708(t), "migrations/007_trigger_update_of.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_order_status_change")
	if e == nil {
		t.Fatal("expected TRIGGER trg_order_status_change (UPDATE OF col form)")
	}
	if e.Subtype != "trigger" {
		t.Errorf("expected Subtype=trigger, got %q", e.Subtype)
	}
}

func Test1708_TriggerMultiColumnListExtracted(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1708(t), "migrations/007_trigger_update_of.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_orders_multi_col")
	if e == nil {
		t.Fatal("expected TRIGGER trg_orders_multi_col (UPDATE OF col1, col2 form)")
	}
}

func Test1708_TriggerFiresEdge(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1708(t), "migrations/007_trigger_update_of.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_order_status_change")
	if e == nil {
		t.Fatal("trigger entity missing")
	}
	fires := collectEdges(e.Relationships, "FIRES")
	if !fires["log_order_status_change"] {
		t.Errorf("expected FIRES log_order_status_change, got %v", fires)
	}
}

func Test1708_TriggerDefinedOnEdge(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1708(t), "migrations/007_trigger_update_of.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_order_status_change")
	if e == nil {
		t.Fatal("trigger entity missing")
	}
	definedOn := collectEdges(e.Relationships, "DEFINED_ON")
	if !definedOn["orders"] {
		t.Errorf("expected DEFINED_ON orders, got %v", definedOn)
	}
}
