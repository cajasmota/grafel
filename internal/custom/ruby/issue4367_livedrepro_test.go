package ruby_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
)

// Issue #4367 LIVE-REPRO — ActiveRecord half.
//
// Runs the ACTUAL registered custom_ruby_activerecord + custom_ruby_validation
// extractors + the ACTUAL resolve.BuildIndex symbol table over a faithful
// ActiveRecord model, and asserts that association declarations (has_many /
// belongs_to) and validations are CLASS MEMBERS (CONTAINS from the model) and
// that associations carry a REFERENCES edge to their singularized+camelized
// target model that RESOLVES in the symbol table.
//
// Pre-fix: `has_many:items` / `belongs_to:customer` / `assoc:*` / `fk:*` /
// `validates:*` were emitted standalone with no CONTAINS and no REFERENCES.

const arOrderSrc = `class Order < ApplicationRecord
  belongs_to :customer
  has_many :items
  has_one :invoice
  validates :status, presence: true
  validates_presence_of :total
end
`

const arCustomerSrc = `class Customer < ApplicationRecord
  has_many :orders
end
`

const arItemSrc = `class Item < ApplicationRecord
  belongs_to :order
end
`

const arInvoiceSrc = `class Invoice < ApplicationRecord
  belongs_to :order
end
`

func rbExtract4367(t *testing.T, key, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(key)
	if !ok {
		t.Fatalf("%s not registered", key)
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "ruby", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return ents
}

func TestIssue4367_ActiveRecord_FieldMembership_AndRelationTargets(t *testing.T) {
	var all []types.EntityRecord
	all = append(all, rbExtract4367(t, "custom_ruby_activerecord", "app/models/order.rb", arOrderSrc)...)
	all = append(all, rbExtract4367(t, "custom_ruby_validation", "app/models/order.rb", arOrderSrc)...)
	all = append(all, rbExtract4367(t, "custom_ruby_activerecord", "app/models/customer.rb", arCustomerSrc)...)
	all = append(all, rbExtract4367(t, "custom_ruby_activerecord", "app/models/item.rb", arItemSrc)...)
	all = append(all, rbExtract4367(t, "custom_ruby_activerecord", "app/models/invoice.rb", arInvoiceSrc)...)

	var contains, references int
	refTargets := map[string]bool{}
	for _, e := range all {
		for _, r := range e.Relationships {
			switch r.Kind {
			case string(types.RelationshipKindContains):
				if r.Properties["member"] == "field" {
					contains++
				}
			case string(types.RelationshipKindReferences):
				references++
				refTargets[r.ToID] = true
			}
		}
	}

	if contains == 0 {
		t.Fatalf("AR field CONTAINS membership = 0 (orphan associations/validations); want > 0 (#4367)")
	}
	if references == 0 {
		t.Fatalf("AR association REFERENCES = 0; want > 0 (#4367)")
	}
	t.Logf("ActiveRecord: CONTAINS field edges=%d, REFERENCES edges=%d", contains, references)

	// Singularization: has_many :items -> Item, belongs_to :customer -> Customer.
	for _, want := range []string{"Class:Item", "Class:Customer"} {
		if !refTargets[want] {
			t.Errorf("expected REFERENCES target %q (singularized/camelized) not emitted (#4367); got %v", want, refTargets)
		}
	}

	// Targets must RESOLVE against the real model class nodes in the symbol
	// table. In production these are the base tree-sitter Ruby class entities
	// (Name == bare class name); the custom AR extractor emits a `model:<Name>`
	// schema node, so the `Class:<Name>` byName convention binds to the base
	// class node. We add faithful base class nodes here (the tree-sitter Ruby
	// extractor runs alongside the custom one in the real pipeline).
	for _, cls := range []string{"Order", "Customer", "Item", "Invoice"} {
		c := types.EntityRecord{
			Name: cls, Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "app/models/" + cls + ".rb", Language: "ruby",
			Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "class"},
		}
		c.ID = c.ComputeID()
		all = append(all, c)
	}

	idx := resolve.BuildIndex(all)
	for _, target := range []string{"Class:Customer", "Class:Item", "Class:Invoice"} {
		if _, ok := idx.Lookup(target); !ok {
			t.Errorf("symbol table did NOT resolve REFERENCES target %q (#4367)", target)
		}
	}
	if _, ok := idx.Lookup("Class:Order"); !ok {
		t.Errorf("symbol table did NOT resolve CONTAINS owner Class:Order (#4367)")
	}
}
