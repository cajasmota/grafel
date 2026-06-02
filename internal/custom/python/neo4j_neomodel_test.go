package python_test

// neo4j_neomodel_test.go — tests for the python_neomodel extractor (#3609,
// epic #3606).
//
// Covers neomodel (Neo4j Python OGM) StructuredNode graph-schema extraction and
// the headline GRAPH_RELATES topology edge: a StructuredNode owning a
// RelationshipTo/RelationshipFrom to another same-file StructuredNode subclass
// emits a traversable owner ──GRAPH_RELATES(rel_type,direction)──▶
// Class:<TargetLabel> edge. Mirrors the Java SDN template (#3663).

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runNeomodel(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get("python_neomodel")
	if !ok {
		t.Fatal("python_neomodel not registered")
	}
	ents, err := e.Extract(context.Background(),
		extractor.FileInput{Path: "models.py", Language: "python", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// findNeomodelGraphRelates returns the GRAPH_RELATES edge from the node entity
// named fromNode to "Class:<toNode>", or nil.
func findNeomodelGraphRelates(ents []types.EntityRecord, fromNode, toNode string) *types.RelationshipRecord {
	for i := range ents {
		if !(ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "node" && ents[i].Name == fromNode) {
			continue
		}
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == string(types.RelationshipKindGraphRelates) && r.ToID == "Class:"+toNode {
				return r
			}
		}
	}
	return nil
}

func hasNeomodelNode(ents []types.EntityRecord, name string) bool {
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "node" && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// node + property extraction
// ---------------------------------------------------------------------------

func TestNeomodelNodeAndProperty(t *testing.T) {
	src := `
from neomodel import StructuredNode, StringProperty, IntegerProperty

class Movie(StructuredNode):
    title = StringProperty(unique_index=True)
    released = IntegerProperty()
`
	ents := runNeomodel(t, src)
	if !hasNeomodelNode(ents, "Movie") {
		t.Fatalf("expected Movie node entity, got %+v", ents)
	}
	var foundTitle bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "property" && e.Name == "Movie.title" {
			foundTitle = true
			if e.Properties["property_type"] != "StringProperty" {
				t.Errorf("Movie.title property_type: want StringProperty, got %q", e.Properties["property_type"])
			}
		}
	}
	if !foundTitle {
		t.Errorf("expected Movie.title property entity, got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// GRAPH_RELATES — the headline node→node topology edge
// ---------------------------------------------------------------------------

// TestNeomodelGraphRelatesEdge proves Person ─GRAPH_RELATES(ACTED_IN,OUTGOING)→
// Class:Movie for a RelationshipTo('Movie','ACTED_IN').
func TestNeomodelGraphRelatesEdge(t *testing.T) {
	src := `
from neomodel import StructuredNode, StringProperty, RelationshipTo

class Movie(StructuredNode):
    title = StringProperty()

class Person(StructuredNode):
    name = StringProperty()
    movies = RelationshipTo('Movie', 'ACTED_IN')
`
	ents := runNeomodel(t, src)

	edge := findNeomodelGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES→ Class:Movie edge, got %+v", ents)
	}
	if edge.Properties["rel_type"] != "ACTED_IN" {
		t.Errorf("rel_type: want ACTED_IN, got %q", edge.Properties["rel_type"])
	}
	if edge.Properties["direction"] != "OUTGOING" {
		t.Errorf("direction: want OUTGOING, got %q", edge.Properties["direction"])
	}
	if edge.Properties["field_name"] != "movies" {
		t.Errorf("field_name: want movies, got %q", edge.Properties["field_name"])
	}
	if edge.Properties["framework"] != "neomodel" {
		t.Errorf("framework: want neomodel, got %q", edge.Properties["framework"])
	}

	// direction is a property, not endpoint-swapping: no reverse edge.
	if findNeomodelGraphRelates(ents, "Movie", "Person") != nil {
		t.Error("did not expect a reverse Movie ─GRAPH_RELATES→ Person edge")
	}
}

// TestNeomodelGraphRelatesFromIncoming proves RelationshipFrom → INCOMING.
func TestNeomodelGraphRelatesFromIncoming(t *testing.T) {
	src := `
from neomodel import StructuredNode, StringProperty, RelationshipFrom

class Person(StructuredNode):
    name = StringProperty()

class Movie(StructuredNode):
    title = StringProperty()
    actors = RelationshipFrom('Person', 'ACTED_IN')
`
	ents := runNeomodel(t, src)
	edge := findNeomodelGraphRelates(ents, "Movie", "Person")
	if edge == nil {
		t.Fatalf("expected Movie ─GRAPH_RELATES→ Class:Person edge, got %+v", ents)
	}
	if edge.Properties["direction"] != "INCOMING" {
		t.Errorf("direction: want INCOMING, got %q", edge.Properties["direction"])
	}
	if edge.Properties["rel_type"] != "ACTED_IN" {
		t.Errorf("rel_type: want ACTED_IN, got %q", edge.Properties["rel_type"])
	}
}

// TestNeomodelGraphRelatesSelfEdge proves a self-referential RelationshipTo
// resolves: Person ─GRAPH_RELATES(KNOWS)→ Person.
func TestNeomodelGraphRelatesSelfEdge(t *testing.T) {
	src := `
from neomodel import StructuredNode, StringProperty, RelationshipTo

class Person(StructuredNode):
    name = StringProperty()
    friends = RelationshipTo('Person', 'KNOWS')
`
	ents := runNeomodel(t, src)
	edge := findNeomodelGraphRelates(ents, "Person", "Person")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES→ Class:Person self-edge, got %+v", ents)
	}
	if edge.Properties["rel_type"] != "KNOWS" {
		t.Errorf("rel_type: want KNOWS, got %q", edge.Properties["rel_type"])
	}
}

// TestNeomodelGraphRelatesCrossFileDeferred proves honest-partial: a target
// label that is NOT a same-file StructuredNode emits no edge — the topology
// stays only as the target_node prop on the relationship Component.
func TestNeomodelGraphRelatesCrossFileDeferred(t *testing.T) {
	src := `
from neomodel import StructuredNode, StringProperty, RelationshipTo

class Person(StructuredNode):
    name = StringProperty()
    movies = RelationshipTo('Movie', 'ACTED_IN')
`
	ents := runNeomodel(t, src)
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("expected NO GRAPH_RELATES edge for cross-file target, got %+v", r)
			}
		}
	}
	foundProp := false
	for _, e := range ents {
		if e.Subtype == "relationship" && e.Properties["target_node"] == "Movie" {
			foundProp = true
		}
	}
	if !foundProp {
		t.Errorf("expected relationship Component with target_node=Movie prop, got %+v", ents)
	}
}

// TestNeomodelPlainPropertyNoEdge proves the negative: a plain *Property field
// (no Relationship) on a StructuredNode emits no GRAPH_RELATES edge.
func TestNeomodelPlainPropertyNoEdge(t *testing.T) {
	src := `
from neomodel import StructuredNode, StringProperty

class Movie(StructuredNode):
    title = StringProperty()

class Person(StructuredNode):
    name = StringProperty()
    favourite = StringProperty()
`
	ents := runNeomodel(t, src)
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("plain property must not emit GRAPH_RELATES, got %+v", r)
			}
		}
	}
}

// TestNeomodelSkipsNonNeomodelSource proves the gate: a plain SQLAlchemy/Django
// model file produces nothing.
func TestNeomodelSkipsNonNeomodelSource(t *testing.T) {
	src := `
from django.db import models

class Movie(models.Model):
    title = models.CharField(max_length=200)
`
	ents := runNeomodel(t, src)
	if len(ents) != 0 {
		t.Errorf("neomodel extractor must not fire on non-neomodel source, got %+v", ents)
	}
}
