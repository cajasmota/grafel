package sql_test

// Issue #1414: VIEW / PROCEDURE / TRIGGER deep DB-schema extraction.
//
// Acceptance criteria (mirrors ShipFast D23 MANIFEST §12):
//
//   Entity extraction:
//   [E1]  VIEW  order_summary         → SCOPE.Datastore / view
//   [E2]  PROCEDURE mark_order_paid   → SCOPE.Datastore / procedure
//   [E3]  FUNCTION log_order_status_change → SCOPE.Datastore / trigger_function
//   [E4]  TRIGGER trg_order_status_change  → SCOPE.Datastore / trigger
//   [E5]  TABLE orders                → SCOPE.Datastore / table (regression guard)
//   [E6]  TABLE order_status_audit    → SCOPE.Datastore / table
//   [E7]  TABLE daily_revenue         → SCOPE.Datastore / table
//
//   Edge extraction:
//   [R1]  order_summary  READS_FROM orders            (view body SELECT FROM orders)
//   [R2]  mark_order_paid WRITES_TO orders             (procedure body UPDATE orders)
//   [R3]  mark_order_paid WRITES_TO daily_revenue      (procedure body INSERT daily_revenue)
//   [R4]  log_order_status_change WRITES_TO order_status_audit (trigger fn body INSERT)
//   [R5]  trg_order_status_change FIRES log_order_status_change
//   [R6]  trg_order_status_change DEFINED_ON orders

import (
	"testing"
)

// loadFixture1414 reads the §12 migration fixture.
func loadFixture1414(t *testing.T) []byte {
	t.Helper()
	return loadFixture(t, "migration_006_views_procs_triggers.sql")
}

// ---- E1: VIEW order_summary ------------------------------------------------

func Test1414_E1_ViewExtracted(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "order_summary")
	if e == nil {
		t.Fatal("[E1] expected VIEW order_summary entity (SCOPE.Datastore/view)")
	}
	if e.Subtype != "view" {
		t.Errorf("[E1] expected Subtype=view, got %q", e.Subtype)
	}
}

// ---- E2: PROCEDURE mark_order_paid -----------------------------------------

func Test1414_E2_ProcedureExtracted(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "mark_order_paid")
	if e == nil {
		t.Fatal("[E2] expected PROCEDURE mark_order_paid entity (SCOPE.Datastore/procedure)")
	}
	if e.Subtype != "procedure" {
		t.Errorf("[E2] expected Subtype=procedure, got %q", e.Subtype)
	}
	if e.Signature == "" {
		t.Error("[E2] expected non-empty Signature")
	}
}

// ---- E3: TRIGGER FUNCTION log_order_status_change --------------------------

func Test1414_E3_TriggerFunctionExtracted(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "log_order_status_change")
	if e == nil {
		t.Fatal("[E3] expected FUNCTION log_order_status_change entity")
	}
	if e.Subtype != "trigger_function" {
		t.Errorf("[E3] expected Subtype=trigger_function (RETURNS TRIGGER), got %q", e.Subtype)
	}
}

// ---- E4: TRIGGER trg_order_status_change -----------------------------------

func Test1414_E4_TriggerExtracted(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_order_status_change")
	if e == nil {
		t.Fatal("[E4] expected TRIGGER trg_order_status_change entity (SCOPE.Datastore/trigger)")
	}
	if e.Subtype != "trigger" {
		t.Errorf("[E4] expected Subtype=trigger, got %q", e.Subtype)
	}
}

// ---- E5-E7: Tables are still extracted (regression guard) ------------------

func Test1414_E5E6E7_TablesRegressionGuard(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	for _, want := range []string{"orders", "order_status_audit", "daily_revenue"} {
		e := findEntity(entities, "SCOPE.Datastore", want)
		if e == nil {
			t.Errorf("[E5-E7] expected TABLE %q entity", want)
		} else if e.Subtype != "table" {
			t.Errorf("[E5-E7] TABLE %q Subtype=%q, want table", want, e.Subtype)
		}
	}
}

// ---- R1: order_summary READS_FROM orders -----------------------------------

func Test1414_R1_ViewReadsFromOrders(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "order_summary")
	if e == nil {
		t.Fatal("[R1] order_summary entity not found")
	}
	reads := collectEdges(e.Relationships, "READS_FROM")
	if !reads["orders"] {
		t.Errorf("[R1] expected order_summary READS_FROM orders, got %v", reads)
	}
}

// ---- R2: mark_order_paid WRITES_TO orders ----------------------------------

func Test1414_R2_ProcedureWritesToOrders(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "mark_order_paid")
	if e == nil {
		t.Fatal("[R2] mark_order_paid entity not found")
	}
	writes := collectEdges(e.Relationships, "WRITES_TO")
	if !writes["orders"] {
		t.Errorf("[R2] expected mark_order_paid WRITES_TO orders, got %v", writes)
	}
}

// ---- R3: mark_order_paid WRITES_TO daily_revenue ---------------------------

func Test1414_R3_ProcedureWritesToDailyRevenue(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "mark_order_paid")
	if e == nil {
		t.Fatal("[R3] mark_order_paid entity not found")
	}
	writes := collectEdges(e.Relationships, "WRITES_TO")
	if !writes["daily_revenue"] {
		t.Errorf("[R3] expected mark_order_paid WRITES_TO daily_revenue, got %v", writes)
	}
}

// ---- R4: log_order_status_change WRITES_TO order_status_audit --------------

func Test1414_R4_TriggerFnWritesToAudit(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "log_order_status_change")
	if e == nil {
		t.Fatal("[R4] log_order_status_change entity not found")
	}
	writes := collectEdges(e.Relationships, "WRITES_TO")
	if !writes["order_status_audit"] {
		t.Errorf("[R4] expected log_order_status_change WRITES_TO order_status_audit, got %v", writes)
	}
}

// ---- R5: trg_order_status_change FIRES log_order_status_change -------------

func Test1414_R5_TriggerFiresTriggerFn(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_order_status_change")
	if e == nil {
		t.Fatal("[R5] trg_order_status_change entity not found")
	}
	fires := collectEdges(e.Relationships, "FIRES")
	if !fires["log_order_status_change"] {
		t.Errorf("[R5] expected trg_order_status_change FIRES log_order_status_change, got %v", fires)
	}
}

// ---- R6: trg_order_status_change DEFINED_ON orders -------------------------

func Test1414_R6_TriggerDefinedOnOrders(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_order_status_change")
	if e == nil {
		t.Fatal("[R6] trg_order_status_change entity not found")
	}
	definedOn := collectEdges(e.Relationships, "DEFINED_ON")
	if !definedOn["orders"] {
		t.Errorf("[R6] expected trg_order_status_change DEFINED_ON orders, got %v", definedOn)
	}
}

// ---- ProcedureSignature: Signature field says CREATE PROCEDURE --------------

func Test1414_ProcedureSignature(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "mark_order_paid")
	if e == nil {
		t.Fatal("mark_order_paid not found")
	}
	if e.Signature != "CREATE PROCEDURE mark_order_paid" {
		t.Errorf("expected Signature='CREATE PROCEDURE mark_order_paid', got %q", e.Signature)
	}
}

// ---- TriggerSignature: Signature contains table and function ----------------

func Test1414_TriggerSignature(t *testing.T) {
	entities := extractSQLBytes(t, loadFixture1414(t), "migrations/006_views_procs_triggers.sql")
	e := findEntity(entities, "SCOPE.Datastore", "trg_order_status_change")
	if e == nil {
		t.Fatal("trg_order_status_change not found")
	}
	if e.Signature == "" {
		t.Error("expected non-empty trigger Signature")
	}
}
