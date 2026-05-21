package fitness_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/quality/fitness"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func makeDoc(entities []graph.Entity, rels []graph.Relationship) *graph.Document {
	return &graph.Document{
		Entities:      entities,
		Relationships: rels,
	}
}

func entity(id, name, kind, file string) graph.Entity {
	return graph.Entity{ID: id, Name: name, Kind: kind, SourceFile: file}
}

func rel(id, fromID, toID, kind string) graph.Relationship {
	return graph.Relationship{ID: id, FromID: fromID, ToID: toID, Kind: kind}
}

// ─────────────────────────────────────────────────────────────────────────────
// forbid
// ─────────────────────────────────────────────────────────────────────────────

func TestEvaluate_Forbid_Triggers(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "UserHandler", "http_endpoint_definition", "handler.go"),
			entity("e2", "users_table", "DatabaseTable", "db.go"),
		},
		[]graph.Relationship{
			rel("r1", "e1", "e2", "IMPORTS"),
		},
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:   "No DB in handlers",
			Forbid: "http_endpoint_definition -> DatabaseTable",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 1 {
		t.Fatalf("expected 1 failed rule, got %d", result.FailedRules)
	}
	if len(result.Results[0].Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Results[0].Violations))
	}
	v := result.Results[0].Violations[0]
	if v.Kind != "forbid" {
		t.Errorf("expected kind=forbid, got %s", v.Kind)
	}
	if v.FromEntity == nil || v.FromEntity.ID != "e1" {
		t.Errorf("expected from entity e1")
	}
}

func TestEvaluate_Forbid_NoViolation(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "UserService", "Service", "svc.go"),
			entity("e2", "users_table", "DatabaseTable", "db.go"),
		},
		[]graph.Relationship{
			rel("r1", "e1", "e2", "IMPORTS"),
		},
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:   "No DB in handlers",
			Forbid: "http_endpoint_definition -> DatabaseTable",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 0 {
		t.Errorf("expected 0 failed rules, got %d", result.FailedRules)
	}
}

func TestEvaluate_Forbid_WithEdgeKind(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "UserHandler", "http_endpoint_definition", "handler.go"),
			entity("e2", "users_table", "DatabaseTable", "db.go"),
		},
		[]graph.Relationship{
			rel("r1", "e1", "e2", "CALLS"),
		},
	)
	// Pattern with specific edge kind: only flag CALLS edges.
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:   "No direct CALLS from handler to DB",
			Forbid: "http_endpoint_definition -[CALLS]-> DatabaseTable",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 1 {
		t.Errorf("expected 1 failed rule, got %d", result.FailedRules)
	}
}

func TestEvaluate_Forbid_ExceptionByID(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "UserHandler", "http_endpoint_definition", "handler.go"),
			entity("e2", "users_table", "DatabaseTable", "db.go"),
		},
		[]graph.Relationship{
			rel("r1", "e1", "e2", "IMPORTS"),
		},
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:   "No DB in handlers",
			Forbid: "http_endpoint_definition -> DatabaseTable",
			Except: []string{"e1"}, // except the specific entity
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 0 {
		t.Errorf("expected 0 failed rules (exception applies), got %d", result.FailedRules)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// require
// ─────────────────────────────────────────────────────────────────────────────

func TestEvaluate_Require_HasNoOutbound_Triggers(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "UserModel", "Model", "model.go"),
			entity("e2", "OrderService", "Service", "svc.go"),
		},
		[]graph.Relationship{
			rel("r1", "e1", "e2", "IMPORTS"),
		},
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:    "Models depend on nothing",
			Require: "Model has-no-outbound-IMPORTS",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 1 {
		t.Fatalf("expected 1 failed rule, got %d", result.FailedRules)
	}
}

func TestEvaluate_Require_HasNoOutbound_Passes(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "UserModel", "Model", "model.go"),
			entity("e2", "OrderService", "Service", "svc.go"),
		},
		[]graph.Relationship{
			// Only CALLS, not IMPORTS — rule is about IMPORTS.
			rel("r1", "e1", "e2", "CALLS"),
		},
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:    "Models depend on nothing",
			Require: "Model has-no-outbound-IMPORTS",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 0 {
		t.Errorf("expected 0 failed rules, got %d", result.FailedRules)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// threshold
// ─────────────────────────────────────────────────────────────────────────────

func TestEvaluate_Threshold_OrphanRate(t *testing.T) {
	// 3 entities, 1 inbound edge → 2/3 orphans = 66.7%
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "A", "Service", "a.go"),
			entity("e2", "B", "Service", "b.go"),
			entity("e3", "C", "Service", "c.go"),
		},
		[]graph.Relationship{
			rel("r1", "e1", "e2", "IMPORTS"), // e2 has inbound; e1 and e3 are orphans
		},
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:      "Low orphan rate",
			Threshold: "orphan_rate_pct < 30",
			Severity:  "warn",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 1 {
		t.Fatalf("expected 1 failed rule (orphan rate 66.7%% > 30%%), got %d", result.FailedRules)
	}
	if result.WarnCount != 1 {
		t.Errorf("expected warn_count=1, got %d", result.WarnCount)
	}
}

func TestEvaluate_Threshold_EntityCount(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "A", "Service", "a.go"),
			entity("e2", "B", "Service", "b.go"),
		},
		nil,
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:      "Entity count check",
			Threshold: "entity_count >= 2",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 0 {
		t.Errorf("expected 0 failed rules (entity_count=2 >= 2), got %d", result.FailedRules)
	}
}

func TestEvaluate_Threshold_UnknownMetric(t *testing.T) {
	doc := makeDoc(nil, nil)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:      "Unknown metric",
			Threshold: "nonexistent_metric <= 5",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 1 {
		t.Errorf("expected 1 failed rule (unknown metric), got %d", result.FailedRules)
	}
	v := result.Results[0].Violations[0]
	if v.Severity != "error" {
		t.Errorf("expected severity error for unknown metric, got %s", v.Severity)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// severity
// ─────────────────────────────────────────────────────────────────────────────

func TestEvaluate_SeverityWarn_NotError(t *testing.T) {
	doc := makeDoc(
		[]graph.Entity{
			entity("e1", "UserHandler", "http_endpoint_definition", "handler.go"),
			entity("e2", "users_table", "DatabaseTable", "db.go"),
		},
		[]graph.Relationship{
			rel("r1", "e1", "e2", "IMPORTS"),
		},
	)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name:     "Soft: No DB in handlers",
			Forbid:   "http_endpoint_definition -> DatabaseTable",
			Severity: "warn",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.ErrorCount != 0 {
		t.Errorf("expected 0 errors (severity=warn), got %d", result.ErrorCount)
	}
	if result.WarnCount != 1 {
		t.Errorf("expected 1 warn, got %d", result.WarnCount)
	}
	if result.HasFailures() {
		t.Errorf("HasFailures() should be false for warn-only violations")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parse errors
// ─────────────────────────────────────────────────────────────────────────────

func TestEvaluate_NoClause_IsParseError(t *testing.T) {
	doc := makeDoc(nil, nil)
	cfg := &fitness.Config{
		Rules: []fitness.RuleConfig{{
			Name: "Empty rule",
		}},
	}
	result := fitness.Evaluate(cfg, doc)
	if result.FailedRules != 1 {
		t.Errorf("expected 1 failed rule for empty clause, got %d", result.FailedRules)
	}
}
