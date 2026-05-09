package sql_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/sql"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #141: SQL schema CONTAINS / REFERENCES edges previously emitted bare
// column names (e.g. "name", "id") which collided cross-language with Java
// method calls and tripped the bug-resolver classifier. The fix:
//   - Column entity Name is qualified as "<table>.<column>" so ComputeID is
//     unique per (file, table, column).
//   - CONTAINS ToID is a Format B structural-ref:
//       scope:schema:column:sql:<file>:<table>#<column>
//   - REFERENCES FromID is the same Format B structural-ref so the column
//     collision class disappears for FK edges too.

const twoTablesSameColumnFixture = `CREATE TABLE Pet (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE Owner (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);
`

func extractIssue141(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("sql")
	if !ok {
		t.Fatal("sql extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return entities
}

// TestIssue141_TwoTables_NameColumn_DistinctIDs is the headline invariant
// from the issue: two tables both having a `name` column in the same SQL
// file produce distinct entity IDs (so they don't merge under the same
// graph node) and have distinct, qualified entity Names.
func TestIssue141_TwoTables_NameColumn_DistinctIDs(t *testing.T) {
	entities := extractIssue141(t, twoTablesSameColumnFixture, "migrations/petclinic.sql")

	var pet, owner *types.EntityRecord
	for i := range entities {
		e := &entities[i]
		if e.Subtype != "column" || e.Properties["column"] != "name" {
			continue
		}
		switch e.Properties["table"] {
		case "Pet":
			pet = e
		case "Owner":
			owner = e
		}
	}
	if pet == nil {
		t.Fatal("expected Pet.name column entity")
	}
	if owner == nil {
		t.Fatal("expected Owner.name column entity")
	}

	// Qualified Names — required for unique ComputeID and byMember
	// population in the resolver.
	if pet.Name != "Pet.name" {
		t.Errorf("Pet.name entity Name=%q, want %q", pet.Name, "Pet.name")
	}
	if owner.Name != "Owner.name" {
		t.Errorf("Owner.name entity Name=%q, want %q", owner.Name, "Owner.name")
	}

	// Distinct ComputeIDs.
	petID := pet.ComputeID()
	ownerID := owner.ComputeID()
	if petID == "" || ownerID == "" {
		t.Fatalf("ComputeID returned empty: pet=%q owner=%q", petID, ownerID)
	}
	if petID == ownerID {
		t.Errorf("ComputeID collision: Pet.name and Owner.name both hash to %q", petID)
	}
}

// TestIssue141_ContainsEdges_StructuralRef verifies that table→column
// CONTAINS edges emit Format B structural-refs as ToIDs.
func TestIssue141_ContainsEdges_StructuralRef(t *testing.T) {
	filePath := "migrations/petclinic.sql"
	entities := extractIssue141(t, twoTablesSameColumnFixture, filePath)

	var pet *types.EntityRecord
	for i := range entities {
		e := &entities[i]
		if e.Subtype == "table" && e.Name == "Pet" {
			pet = e
			break
		}
	}
	if pet == nil {
		t.Fatal("expected Pet table entity")
	}

	wantTargets := map[string]bool{
		"scope:schema:column:sql:" + filePath + ":Pet#id":   false,
		"scope:schema:column:sql:" + filePath + ":Pet#name": false,
	}
	for _, r := range pet.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		if !strings.HasPrefix(r.ToID, "scope:schema:column:sql:") {
			t.Errorf("CONTAINS ToID=%q, expected structural-ref prefix", r.ToID)
		}
		if _, ok := wantTargets[r.ToID]; ok {
			wantTargets[r.ToID] = true
		}
	}
	for target, found := range wantTargets {
		if !found {
			t.Errorf("expected CONTAINS edge with ToID=%q", target)
		}
	}
}

// TestIssue141_ReferencesFromID_StructuralRef verifies that column-level
// REFERENCES (foreign-key) edges use a Format B structural-ref as FromID.
func TestIssue141_ReferencesFromID_StructuralRef(t *testing.T) {
	src := `CREATE TABLE accounts (
    id SERIAL PRIMARY KEY
);

CREATE TABLE sessions (
    id SERIAL PRIMARY KEY,
    account_id INTEGER REFERENCES accounts(id)
);
`
	filePath := "migrations/sessions.sql"
	entities := extractIssue141(t, src, filePath)

	var found bool
	wantFrom := "scope:schema:column:sql:" + filePath + ":sessions#account_id"
	for _, e := range entities {
		if e.Subtype != "column" || e.Properties["column"] != "account_id" {
			continue
		}
		if e.Properties["table"] != "sessions" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			if r.FromID != wantFrom {
				t.Errorf("REFERENCES FromID=%q, want %q", r.FromID, wantFrom)
			}
			found = true
		}
	}
	if !found {
		t.Error("expected REFERENCES edge on sessions.account_id")
	}
}

// TestIssue141_AlterTableFK_StructuralRef verifies the same FromID shape on
// ALTER TABLE FK emissions (both attach-to-existing and synthetic-column
// branches).
func TestIssue141_AlterTableFK_StructuralRef(t *testing.T) {
	src := `CREATE TABLE users (
    id SERIAL PRIMARY KEY
);

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER
);

ALTER TABLE orders ADD CONSTRAINT fk_orders_users FOREIGN KEY (user_id) REFERENCES users(id);
ALTER TABLE shipments ADD FOREIGN KEY (customer_id) REFERENCES users(id);
`
	filePath := "migrations/alter.sql"
	entities := extractIssue141(t, src, filePath)

	wantOrders := "scope:schema:column:sql:" + filePath + ":orders#user_id"
	wantShipments := "scope:schema:column:sql:" + filePath + ":shipments#customer_id"

	gotOrders, gotShipments := false, false
	for _, e := range entities {
		if e.Subtype != "column" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			switch r.FromID {
			case wantOrders:
				gotOrders = true
			case wantShipments:
				gotShipments = true
			}
		}
	}
	if !gotOrders {
		t.Errorf("expected REFERENCES FromID=%q (attach-to-existing branch)", wantOrders)
	}
	if !gotShipments {
		t.Errorf("expected REFERENCES FromID=%q (synthetic-column branch)", wantShipments)
	}
}
