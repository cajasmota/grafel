package sql_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #389 (PORT-RELS-SQL): DML emission.
//
// Coverage matrix gap: SELECT → READS_FROM, INSERT/UPDATE/DELETE → WRITES_TO.
//
// In SQL files, raw DML statements are not standalone identity-bearing
// entities. We attach READS_FROM / WRITES_TO edges to the surrounding
// scope entity:
//
//   - CREATE VIEW v AS SELECT ... FROM t          → v READS_FROM t
//   - CREATE FUNCTION f ... BEGIN SELECT ... END  → f READS_FROM t
//   - CREATE FUNCTION f ... BEGIN INSERT ... END  → f WRITES_TO t
//   - CREATE FUNCTION f ... BEGIN UPDATE ... END  → f WRITES_TO t
//   - CREATE FUNCTION f ... BEGIN DELETE ... END  → f WRITES_TO t

func collectEdges(rels []types.RelationshipRecord, kind string) map[string]bool {
	out := map[string]bool{}
	for _, r := range rels {
		if r.Kind == kind {
			out[r.ToID] = true
		}
	}
	return out
}

func TestSQLExtractor_ViewReadsFromTables(t *testing.T) {
	src := loadFixture(t, "migration_005_dml.sql")
	entities := extractSQLBytes(t, src, "migrations/005_dml.sql")

	view := findEntity(entities, "SCOPE.Datastore", "active_sessions")
	if view == nil {
		t.Fatal("expected CREATE VIEW active_sessions to be emitted")
	}
	reads := collectEdges(view.Relationships, "READS_FROM")
	for _, want := range []string{"accounts", "sessions"} {
		if !reads[want] {
			t.Errorf("expected active_sessions READS_FROM %q (got %v)", want, reads)
		}
	}
}

func TestSQLExtractor_FunctionReadsAndWrites(t *testing.T) {
	src := loadFixture(t, "migration_005_dml.sql")
	entities := extractSQLBytes(t, src, "migrations/005_dml.sql")

	fn := findEntity(entities, "SCOPE.Datastore", "record_event")
	if fn == nil {
		t.Fatal("expected CREATE FUNCTION record_event to be emitted")
	}

	reads := collectEdges(fn.Relationships, "READS_FROM")
	writes := collectEdges(fn.Relationships, "WRITES_TO")

	if !reads["accounts"] {
		t.Errorf("expected record_event READS_FROM accounts (got %v)", reads)
	}
	if !writes["audit_log"] {
		t.Errorf("expected record_event WRITES_TO audit_log (got %v)", writes)
	}
	if !writes["sessions"] {
		t.Errorf("expected record_event WRITES_TO sessions (got %v)", writes)
	}

	// Dedupe: UPDATE sessions and DELETE FROM sessions should produce ONE
	// WRITES_TO edge, not two.
	count := 0
	for _, r := range fn.Relationships {
		if r.Kind == "WRITES_TO" && r.ToID == "sessions" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 WRITES_TO sessions edge (deduped), got %d", count)
	}
}

func TestSQLExtractor_DMLProperties(t *testing.T) {
	src := loadFixture(t, "migration_005_dml.sql")
	entities := extractSQLBytes(t, src, "migrations/005_dml.sql")

	fn := findEntity(entities, "SCOPE.Datastore", "record_event")
	if fn == nil {
		t.Fatal("function record_event not found")
	}
	for _, r := range fn.Relationships {
		switch r.Kind {
		case "READS_FROM":
			if r.Properties["dml"] == "" {
				t.Errorf("READS_FROM edge to %q missing Properties[dml]", r.ToID)
			}
		case "WRITES_TO":
			if r.Properties["dml"] == "" {
				t.Errorf("WRITES_TO edge to %q missing Properties[dml]", r.ToID)
			}
		}
	}
}
