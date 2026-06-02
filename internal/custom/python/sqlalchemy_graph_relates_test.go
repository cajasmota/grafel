// sqlalchemy_graph_relates_test.go — value-asserting tests for GRAPH_RELATES
// model↔model edges (with cardinality) emitted from SQLAlchemy relationship().

package python_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func saGraphRelates(ents []types.EntityRecord, from, to string) *types.RelationshipRecord {
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

func saExtractRaw(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("python_sqlalchemy")
	if !ok {
		t.Fatal("python_sqlalchemy extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "models.py",
		Content:  []byte(src),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func TestSQLAlchemyGraphRelatesEdges(t *testing.T) {
	src := `from sqlalchemy.orm import relationship
from sqlalchemy import ForeignKey, Column, Integer

class User(Base):
    __tablename__ = "users"
    id = Column(Integer, primary_key=True)
    orders = relationship("Order", back_populates="user")
    profile = relationship("Profile", uselist=False)

class Order(Base):
    __tablename__ = "orders"
    id = Column(Integer, primary_key=True)
    user_id = Column(Integer, ForeignKey("users.id"))
`
	ents := saExtractRaw(t, src)

	hm := saGraphRelates(ents, "Class:User", "Class:Order")
	if hm == nil {
		t.Fatal("expected GRAPH_RELATES User → Order (collection relationship)")
	}
	if hm.Properties["cardinality"] != "one_to_many" {
		t.Errorf("collection relationship cardinality: want one_to_many, got %q", hm.Properties["cardinality"])
	}

	o2o := saGraphRelates(ents, "Class:User", "Class:Profile")
	if o2o == nil || o2o.Properties["cardinality"] != "one_to_one" {
		t.Errorf("expected GRAPH_RELATES User → Profile one_to_one (uselist=False), got %v", o2o)
	}
}

// A class with no relationship() calls must not emit GRAPH_RELATES edges.
func TestSQLAlchemyNoRelationshipNoEdge(t *testing.T) {
	src := `from sqlalchemy import Column, Integer, String

class User(Base):
    __tablename__ = "users"
    id = Column(Integer, primary_key=True)
    name = Column(String)
`
	ents := saExtractRaw(t, src)
	for ei := range ents {
		for _, r := range ents[ei].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("unexpected GRAPH_RELATES edge: %v", r)
			}
		}
	}
}
