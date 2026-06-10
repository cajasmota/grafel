package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/golang"
)

// Issue #4367 LIVE-REPRO — GORM half.
//
// Runs the ACTUAL registered custom_go_gorm extractor + the ACTUAL
// resolve.BuildIndex symbol table over a faithful GORM model file, and asserts
// that GORM column/association fields are CLASS MEMBERS (CONTAINS from the model
// struct) and that association fields carry a REFERENCES edge to their target
// model that RESOLVES against the real model entity in the symbol table.
//
// Pre-fix: field:/rel: entities were emitted standalone with no CONTAINS and no
// REFERENCES (degree-0 orphans); the untagged `Customer Customer` association
// was dropped entirely.

const gormSrc = `package models

import "gorm.io/gorm"

type Order struct {
	gorm.Model
	Status     string ` + "`gorm:\"column:status;type:varchar(32)\"`" + `
	CustomerID uint
	Customer   Customer
	Items      []Item ` + "`gorm:\"foreignKey:OrderID\"`" + `
}

type Customer struct {
	gorm.Model
	Name string ` + "`gorm:\"column:name\"`" + `
}

type Item struct {
	gorm.Model
	OrderID uint
	SKU     string ` + "`gorm:\"column:sku\"`" + `
}
`

func gormExtract4367(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_go_gorm")
	if !ok {
		t.Fatal("custom_go_gorm not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "go", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func TestIssue4367_GORM_FieldMembership_AndRelationTargets(t *testing.T) {
	ents := gormExtract4367(t, "models/order.go", gormSrc)

	var contains, references int
	var refTargets []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			switch r.Kind {
			case string(types.RelationshipKindContains):
				if r.Properties["member"] == "field" {
					contains++
				}
			case string(types.RelationshipKindReferences):
				references++
				refTargets = append(refTargets, r.ToID)
			}
		}
	}

	// Membership: at minimum status + Customer + Items on Order, name on
	// Customer, sku on Item — multiple CONTAINS field edges.
	if contains == 0 {
		t.Fatalf("GORM field CONTAINS membership = 0 (orphan fields); want > 0 (#4367)")
	}
	// Relation targets: Order->Customer, Order->Item at least.
	if references < 2 {
		t.Fatalf("GORM relation REFERENCES = %d; want >= 2 (Customer, Item) (#4367)", references)
	}
	t.Logf("GORM: CONTAINS field edges=%d, REFERENCES edges=%d", contains, references)

	// Untagged association `Customer Customer` must now be recovered.
	foundUntagged := false
	for _, e := range ents {
		if e.Properties["untagged"] == "true" && e.Properties["target_model"] == "Customer" {
			foundUntagged = true
		}
	}
	if !foundUntagged {
		t.Errorf("untagged association `Customer Customer` not recovered (#4367)")
	}

	// REFERENCES targets must RESOLVE against the real model entities.
	idx := resolve.BuildIndex(ents)
	for _, target := range []string{"Class:Customer", "Class:Item"} {
		if _, ok := idx.Lookup(target); !ok {
			t.Errorf("symbol table did NOT resolve REFERENCES target %q — relation stays orphan (#4367)", target)
		}
	}

	// And CONTAINS source (Class:Order) must resolve to the Order model node.
	if _, ok := idx.Lookup("Class:Order"); !ok {
		t.Errorf("symbol table did NOT resolve CONTAINS owner Class:Order (#4367)")
	}
}
