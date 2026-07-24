package ruby_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/ruby"
)

// extractFull (defined in observability_test.go) returns the raw EntityRecords
// with Relationships intact, unlike the shared `extract` helper.

func findNode(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "node" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// TestActiveGraphGraphRelatesEdge is the value-asserting topology test:
//
//	has_many :out, :movies, type: :ACTED_IN, model_class: 'Movie'
//	  in a Person ActiveGraph::Node
//	→ Person GRAPH_RELATES Movie (rel_type=ACTED_IN, direction=OUTGOING)
//
//	has_one :in, :studio, type: :OWNS, model_class: 'Studio'
//	→ Studio direction=INCOMING
func TestActiveGraphGraphRelatesEdge(t *testing.T) {
	src := `
class Person
  include ActiveGraph::Node
  property :name
  has_many :out, :movies, type: :ACTED_IN, model_class: 'Movie'
  has_one  :in,  :studio, type: :OWNS, model_class: 'Studio'
end

class Movie
  include ActiveGraph::Node
  property :title
end

class Studio
  include ActiveGraph::Node
end
`
	ents := extractFull(t, "custom_ruby_neo4j_activegraph", fi("person.rb", "ruby", src))

	person := findNode(ents, "Person")
	if person == nil {
		t.Fatal("Person node entity not extracted")
	}
	if findNode(ents, "Movie") == nil {
		t.Fatal("Movie node entity not extracted")
	}

	// Exactly the two GRAPH_RELATES edges off Person, with asserted values.
	var actedIn, owns *types.RelationshipRecord
	for i := range person.Relationships {
		r := &person.Relationships[i]
		if r.Kind != string(types.RelationshipKindGraphRelates) {
			t.Fatalf("unexpected relationship kind %q", r.Kind)
		}
		switch r.ToID {
		case "Class:Movie":
			actedIn = r
		case "Class:Studio":
			owns = r
		default:
			t.Fatalf("unexpected GRAPH_RELATES ToID %q", r.ToID)
		}
	}

	if actedIn == nil {
		t.Fatal("missing Person -GRAPH_RELATES-> Class:Movie edge")
	}
	if got := actedIn.Properties.Get("rel_type"); got != "ACTED_IN" {
		t.Errorf("Movie edge rel_type = %q, want ACTED_IN", got)
	}
	if got := actedIn.Properties.Get("direction"); got != "OUTGOING" {
		t.Errorf("Movie edge (has_many :out) direction = %q, want OUTGOING", got)
	}

	if owns == nil {
		t.Fatal("missing Person -GRAPH_RELATES-> Class:Studio edge")
	}
	if got := owns.Properties.Get("rel_type"); got != "OWNS" {
		t.Errorf("Studio edge rel_type = %q, want OWNS", got)
	}
	if got := owns.Properties.Get("direction"); got != "INCOMING" {
		t.Errorf("Studio edge (has_one :in) direction = %q, want INCOMING", got)
	}
}

// TestActiveGraphNoEdgeForPlainProperty is the negative test: a class with only
// a `property :name` declaration (no association) yields a node with zero
// GRAPH_RELATES edges.
func TestActiveGraphNoEdgeForPlainProperty(t *testing.T) {
	src := `
class Tag
  include ActiveGraph::Node
  property :name
end
`
	ents := extractFull(t, "custom_ruby_neo4j_activegraph", fi("tag.rb", "ruby", src))
	tag := findNode(ents, "Tag")
	if tag == nil {
		t.Fatal("Tag node entity not extracted")
	}
	if len(tag.Relationships) != 0 {
		t.Errorf("plain property class should have no GRAPH_RELATES edges, got %d", len(tag.Relationships))
	}
	// The property itself is still extracted as a schema property.
	var foundProp bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "property" && e.Name == "Tag.name" {
			foundProp = true
		}
	}
	if !foundProp {
		t.Error("expected Tag.name property entity")
	}
}

// TestActiveGraphCrossFileTargetHonestPartial: a model_class whose target is NOT
// a same-file ActiveGraph::Node is honest-partial — the relationship Component
// is emitted (with a target_node prop) but no GRAPH_RELATES edge hangs off the
// owner node.
func TestActiveGraphCrossFileTargetHonestPartial(t *testing.T) {
	src := `
class Person
  include ActiveGraph::Node
  has_many :out, :friends, type: :KNOWS, model_class: 'ExternalPerson'
end
`
	ents := extractFull(t, "custom_ruby_neo4j_activegraph", fi("person.rb", "ruby", src))
	person := findNode(ents, "Person")
	if person == nil {
		t.Fatal("Person node entity not extracted")
	}
	if len(person.Relationships) != 0 {
		t.Errorf("cross-file target should not emit a GRAPH_RELATES edge, got %d", len(person.Relationships))
	}
	// But the relationship Component is still present (honest-partial topology).
	var rel *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "relationship" {
			rel = &ents[i]
		}
	}
	if rel == nil {
		t.Fatal("expected a SCOPE.Component relationship entity")
	}
	if got := rel.Properties["target_node"]; got != "ExternalPerson" {
		t.Errorf("relationship target_node = %q, want ExternalPerson", got)
	}
}

// TestActiveGraphLegacyMixin verifies the legacy neo4j.rb mixin name
// (Neo4j::ActiveNode) also gates the extractor.
func TestActiveGraphLegacyMixin(t *testing.T) {
	src := `
class Account
  include Neo4j::ActiveNode
  has_one :out, :owner, type: :OWNED_BY, model_class: 'Owner'
end

class Owner
  include Neo4j::ActiveNode
end
`
	ents := extractFull(t, "custom_ruby_neo4j_activegraph", fi("account.rb", "ruby", src))
	account := findNode(ents, "Account")
	if account == nil {
		t.Fatal("Account node entity not extracted (legacy Neo4j::ActiveNode mixin)")
	}
	if len(account.Relationships) != 1 || account.Relationships[0].ToID != "Class:Owner" {
		t.Fatalf("expected one GRAPH_RELATES edge to Class:Owner, got %+v", account.Relationships)
	}
}

// TestActiveGraphNoMixinNoExtraction: a plain Ruby class without the node mixin
// produces nothing (gate negative).
func TestActiveGraphNoMixinNoExtraction(t *testing.T) {
	src := `
class PlainOldRuby
  def initialize
    @x = 1
  end
end
`
	ents := extractFull(t, "custom_ruby_neo4j_activegraph", fi("plain.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("non-activegraph file should yield no entities, got %d", len(ents))
	}
}
