package sql_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/sql"
	"github.com/cajasmota/archigraph/internal/types"
)

// loadFixture reads a testdata file relative to this package.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	root := repoRootSQL(t)
	path := filepath.Join(root, "internal", "extractors", "sql", "testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func extractSQLBytes(t *testing.T, src []byte, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("sql")
	if !ok {
		t.Fatal("sql extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return entities
}

func findEntity(entities []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Kind == kind && entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

// TestSQLExtractor_ColumnEntitiesEmitted verifies columns are extracted.
//
// Issue #141: column entities are emitted with Name="<table>.<column>" so
// EntityRecord.ComputeID is unique per (file, table, column). The short
// column identifier is preserved in Properties["column"].
func TestSQLExtractor_ColumnEntitiesEmitted(t *testing.T) {
	src := loadFixture(t, "migration_001_init.sql")
	entities := extractSQLBytes(t, src, "migrations/001_init.sql")

	shortNames := map[string]bool{}
	for _, e := range entities {
		if e.Subtype == "column" {
			shortNames[e.Properties["column"]] = true
			if e.Kind != "SCOPE.Schema" {
				t.Errorf("column %q expected Kind=SCOPE.Schema, got %q", e.Name, e.Kind)
			}
			// Name must be qualified "<table>.<column>".
			if got, want := e.Name, e.Properties["table"]+"."+e.Properties["column"]; got != want {
				t.Errorf("column entity Name=%q, want %q (issue #141)", got, want)
			}
		}
	}
	for _, want := range []string{"id", "email", "account_id", "token", "event_kind"} {
		if !shortNames[want] {
			t.Errorf("expected column %q to be extracted", want)
		}
	}
}

// TestSQLExtractor_TableContainsColumns verifies CONTAINS edges.
//
// Issue #141: CONTAINS ToIDs are now Format B structural-refs of the form
// "scope:schema:column:sql:<file>:<table>#<column>" so column-name targets
// can no longer collide cross-language with operations of the same bare name.
func TestSQLExtractor_TableContainsColumns(t *testing.T) {
	src := loadFixture(t, "migration_001_init.sql")
	filePath := "migrations/001_init.sql"
	entities := extractSQLBytes(t, src, filePath)

	accounts := findEntity(entities, "SCOPE.Datastore", "accounts")
	if accounts == nil {
		t.Fatal("accounts table not found")
	}
	wantCols := map[string]bool{"id": false, "email": false, "created_at": false}
	for _, r := range accounts.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		// Validate structural-ref shape.
		wantPrefix := "scope:schema:column:sql:" + filePath + ":accounts#"
		if !strings.HasPrefix(r.ToID, wantPrefix) {
			t.Errorf("CONTAINS ToID=%q, expected prefix %q (issue #141)", r.ToID, wantPrefix)
			continue
		}
		col := strings.TrimPrefix(r.ToID, wantPrefix)
		if _, ok := wantCols[col]; ok {
			wantCols[col] = true
		}
	}
	for col, found := range wantCols {
		if !found {
			t.Errorf("expected accounts CONTAINS column %q", col)
		}
	}
}

// TestSQLExtractor_InlineForeignKey verifies inline REFERENCES is captured.
func TestSQLExtractor_InlineForeignKey(t *testing.T) {
	src := loadFixture(t, "migration_001_init.sql")
	entities := extractSQLBytes(t, src, "migrations/001_init.sql")

	// account_id column on sessions table — there are also account_id columns
	// on audit_log; pick the one whose Properties.table=sessions.
	var found bool
	for _, e := range entities {
		if e.Subtype != "column" || e.Properties["column"] != "account_id" {
			continue
		}
		if e.Properties["table"] != "sessions" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && r.ToID == "accounts" {
				found = true
				if r.Properties["reference_kind"] != "foreign_key" {
					t.Errorf("expected reference_kind=foreign_key, got %q", r.Properties["reference_kind"])
				}
				// Issue #141: FromID must be a Format B structural-ref.
				wantFrom := "scope:schema:column:sql:migrations/001_init.sql:sessions#account_id"
				if r.FromID != wantFrom {
					t.Errorf("REFERENCES FromID=%q, want %q", r.FromID, wantFrom)
				}
			}
		}
	}
	if !found {
		t.Error("expected sessions.account_id → accounts FK relationship")
	}
}

// TestSQLExtractor_TableLevelForeignKey verifies CONSTRAINT FOREIGN KEY parsing.
func TestSQLExtractor_TableLevelForeignKey(t *testing.T) {
	src := loadFixture(t, "migration_001_init.sql")
	entities := extractSQLBytes(t, src, "migrations/001_init.sql")

	wantPairs := map[string]string{
		"account_id": "accounts",
		"session_id": "sessions",
	}
	for col, expectTo := range wantPairs {
		found := false
		for _, e := range entities {
			if e.Subtype != "column" || e.Properties["column"] != col || e.Properties["table"] != "audit_log" {
				continue
			}
			for _, r := range e.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == expectTo {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("expected audit_log.%s table-level FK → %s", col, expectTo)
		}
	}
}

// TestSQLExtractor_IndexReferencesTable verifies INDEX → table edge.
func TestSQLExtractor_IndexReferencesTable(t *testing.T) {
	src := loadFixture(t, "migration_001_init.sql")
	entities := extractSQLBytes(t, src, "migrations/001_init.sql")

	idx := findEntity(entities, "SCOPE.Datastore", "idx_sessions_account")
	if idx == nil {
		t.Fatal("expected idx_sessions_account index")
	}
	found := false
	for _, r := range idx.Relationships {
		if r.Kind == "INDEXES" && r.ToID == "sessions" {
			found = true
		}
	}
	if !found {
		t.Error("expected idx_sessions_account INDEXES sessions")
	}
}

// TestSQLExtractor_NoStrayConstraintColumns ensures keywords are not parsed as columns.
func TestSQLExtractor_NoStrayConstraintColumns(t *testing.T) {
	src := loadFixture(t, "migration_001_init.sql")
	entities := extractSQLBytes(t, src, "migrations/001_init.sql")

	bad := map[string]bool{
		"PRIMARY": true, "FOREIGN": true, "CONSTRAINT": true,
		"UNIQUE": true, "CHECK": true, "INDEX": true, "KEY": true,
	}
	for _, e := range entities {
		if e.Subtype != "column" {
			continue
		}
		if bad[e.Properties["column"]] {
			t.Errorf("column entity has keyword name %q", e.Properties["column"])
		}
	}
}
