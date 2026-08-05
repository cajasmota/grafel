package ruby_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// TestRubyAttrAccessorCallStillBinds_6141 is the measurement that decided the
// shape of the #6141 fix, kept as a regression test because nothing else in
// the tree would catch its loss.
//
// Issue #6141 prescribes kind-filtering the resolver's leaf-name tiers to
// operationKindFamily, on the reasoning that a call target must be an
// operation. In Ruby that reasoning fails against the graph:
//
//   - field_members.go emits every `attr_accessor :owner` as the entity
//     `Account.owner` with Kind SCOPE.Schema — but in Ruby that IS a
//     generated method;
//   - ruby.go emits real methods under BARE names, and rubyCallTarget
//     discards the receiver, so the call site emits the bare stub `owner`;
//   - therefore lookupMemberByLeafName matching `.owner` against a
//     SCOPE.Schema entity is the ONLY route that can bind the edge.
//
// A hard operation-filter was implemented and this test measured it turning
// the binding into a dangle. That is why refs.go PREFERS operations instead
// of requiring them (scanLeafMembersPreferring), and why the precision half
// of #6141 is left as a documented gap.
//
// This runs the real Ruby extractor and the real resolver, and asserts on
// what the edge binds to BY CONTENT — never on a dangling count, which
// cannot see this at all.
func TestRubyAttrAccessorCallStillBinds_6141(t *testing.T) {
	const src = `class Account
  attr_accessor :owner

  def describe
    owner
  end
end
`
	ents := rbExtract(t, src, "app/models/account.rb")
	// The extractor leaves ID blank — IDs are computed later in the
	// pipeline, and BuildIndex reads "" as its ambiguity sentinel. Stamp
	// distinct IDs first or this measures nothing.
	for i := range ents {
		ents[i].ID = fmt.Sprintf("%016x", i+1)
	}

	// Precondition 1: the attr_accessor really is modelled as a field.
	var attrID string
	for _, e := range ents {
		if e.Name == "Account.owner" {
			if e.Kind != "SCOPE.Schema" {
				t.Fatalf("precondition changed: Account.owner now has Kind %q, not SCOPE.Schema. "+
					"If attr_accessor is now emitted as an operation, the #6141 precision gap can "+
					"be closed for Ruby — re-check the JS/TS case before doing so", e.Kind)
			}
			attrID = e.ID
		}
	}
	if attrID == "" {
		t.Fatal("precondition changed: no Account.owner entity emitted for attr_accessor :owner")
	}

	// Precondition 2: the call site really emits a BARE stub, which is what
	// forces the leaf-name tier to be the only binding route.
	found := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if strings.EqualFold(r.Kind, "CALLS") && r.ToID == "owner" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("precondition changed: no bare CALLS stub \"owner\" emitted from the method body")
	}

	idx := resolve.BuildIndex(ents)
	resolve.ReferencesEmbedded(ents, idx)

	byID := map[string]types.EntityRecord{}
	for _, e := range ents {
		byID[e.ID] = e
	}
	for _, e := range ents {
		if e.Name != "describe" {
			continue
		}
		for _, r := range e.Relationships {
			if !strings.EqualFold(r.Kind, "CALLS") {
				continue
			}
			tgt, ok := byID[r.ToID]
			if !ok {
				t.Fatalf("Ruby attr_accessor read DANGLES (ToID=%q). A kind filter on the "+
					"leaf-name tiers destroys this edge — see the comment above.", r.ToID)
			}
			if tgt.ID != attrID || tgt.Name != "Account.owner" || tgt.Kind != "SCOPE.Schema" {
				t.Fatalf("bound to id=%s kind=%s name=%s; want the attr_accessor entity "+
					"id=%s / SCOPE.Schema / Account.owner", tgt.ID, tgt.Kind, tgt.Name, attrID)
			}
			return
		}
	}
	t.Fatal("no CALLS edge found on the `describe` method — fixture no longer exercises the tier")
}
