package ruby_test

// activerecord_graph_relates_test.go — value-asserting tests for the
// GRAPH_RELATES model↔model edges (with cardinality) emitted from ActiveRecord
// associations.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func arGraphRelates(ents []types.EntityRecord, from, to string) *types.RelationshipRecord {
	for ei := range ents {
		for ri := range ents[ei].Relationships {
			r := &ents[ei].Relationships[ri]
			if r.Kind == string(types.RelationshipKindGraphRelates) &&
				r.FromID == from && r.ToID == to {
				return r
			}
		}
	}
	return nil
}

func TestARGraphRelatesCardinality(t *testing.T) {
	src := `
class User < ApplicationRecord
  has_many :orders
  has_one :profile
  has_and_belongs_to_many :tags
  has_secure_password
  validates :email, presence: true
end
`
	ents := arExtractRaw(t, "app/models/user.rb", src)

	hm := arGraphRelates(ents, "Class:User", "Class:Order")
	if hm == nil {
		t.Fatal("expected GRAPH_RELATES User → Order (has_many)")
	}
	if hm.Properties["cardinality"] != "one_to_many" {
		t.Errorf("has_many cardinality: want one_to_many, got %q", hm.Properties["cardinality"])
	}

	ho := arGraphRelates(ents, "Class:User", "Class:Profile")
	if ho == nil || ho.Properties["cardinality"] != "one_to_one" {
		t.Errorf("expected GRAPH_RELATES User → Profile one_to_one, got %v", ho)
	}

	habtm := arGraphRelates(ents, "Class:User", "Class:Tag")
	if habtm == nil || habtm.Properties["cardinality"] != "many_to_many" {
		t.Errorf("expected GRAPH_RELATES User → Tag many_to_many, got %v", habtm)
	}

	// Negative: non-association macros must not produce GRAPH_RELATES edges.
	for ei := range ents {
		for _, r := range ents[ei].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				to := r.ToID
				if to == "Class:Password" || to == "Class:Email" || to == "Class:Presence" {
					t.Errorf("fabricated edge from non-association macro: %v", r)
				}
			}
		}
	}
}

func TestARBelongsToManyToOne(t *testing.T) {
	src := `
class Order < ApplicationRecord
  belongs_to :user
end
`
	ents := arExtractRaw(t, "app/models/order.rb", src)
	bt := arGraphRelates(ents, "Class:Order", "Class:User")
	if bt == nil {
		t.Fatal("expected GRAPH_RELATES Order → User (belongs_to)")
	}
	if bt.Properties["cardinality"] != "many_to_one" {
		t.Errorf("belongs_to cardinality: want many_to_one, got %q", bt.Properties["cardinality"])
	}
}

// class_name: option must redirect the target to the named class.
func TestARClassNameTargetRedirect(t *testing.T) {
	src := `
class Comment < ApplicationRecord
  belongs_to :author, class_name: "User"
end
`
	ents := arExtractRaw(t, "app/models/comment.rb", src)
	if e := arGraphRelates(ents, "Class:Comment", "Class:User"); e == nil {
		t.Fatal("expected GRAPH_RELATES Comment → User via class_name: \"User\"")
	}
	if e := arGraphRelates(ents, "Class:Comment", "Class:Author"); e != nil {
		t.Errorf("must not target inflected :author when class_name: overrides it, got %v", e)
	}
}

// polymorphic belongs_to has no single concrete target → no fabricated edge.
func TestARPolymorphicNoEdge(t *testing.T) {
	src := `
class Comment < ApplicationRecord
  belongs_to :commentable, polymorphic: true
end
`
	ents := arExtractRaw(t, "app/models/comment.rb", src)
	for ei := range ents {
		for _, r := range ents[ei].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("polymorphic association must not emit GRAPH_RELATES, got %v", r)
			}
		}
	}
}
