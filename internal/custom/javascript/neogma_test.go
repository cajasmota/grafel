package javascript_test

// neogma_test.go — tests for the custom_js_neogma extractor (#3610, epic #3606).
//
// Covers neogma (Neo4j JS/TS OGM) ModelFactory graph-schema extraction and the
// headline GRAPH_RELATES topology edge: an owner node model whose
// relationships.<key>.model resolves to a same-file ModelFactory binding emits
// a traversable owner ──GRAPH_RELATES(rel_type,direction)──▶ Class:<TargetLabel>
// edge. Mirrors the Java SDN template (#3663).

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// findNeogmaGraphRelates returns the GRAPH_RELATES edge from the node entity
// labelled fromLabel to "Class:<toLabel>", or nil.
func findNeogmaGraphRelates(ents []types.EntityRecord, fromLabel, toLabel string) *types.RelationshipRecord {
	for i := range ents {
		if !(ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "node" && ents[i].Name == fromLabel) {
			continue
		}
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == string(types.RelationshipKindGraphRelates) && r.ToID == "Class:"+toLabel {
				return r
			}
		}
	}
	return nil
}

func runNeogma(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_neogma")
	if !ok {
		t.Fatal("custom_js_neogma not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "graph.ts", Language: "typescript", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// hasNodeLabel returns true if a SCOPE.Schema/node entity named label exists.
func hasNodeLabel(ents []types.EntityRecord, label string) bool {
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "node" && e.Name == label {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// node extraction — ModelFactory label
// ---------------------------------------------------------------------------

func TestNeogmaNodeExtracted(t *testing.T) {
	src := `
import { ModelFactory } from 'neogma';

const Person = ModelFactory({
  label: 'Person',
  schema: { name: { type: 'string', required: true } },
}, neogma);
`
	ents := runNeogma(t, src)
	if !hasNodeLabel(ents, "Person") {
		t.Fatalf("expected Person node entity (label), got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// GRAPH_RELATES — the headline node→node topology edge
// ---------------------------------------------------------------------------

// TestNeogmaGraphRelatesEdge proves Person ─GRAPH_RELATES(ACTED_IN,OUTGOING)→
// Class:Movie when relationships.actedIn.model resolves to a same-file Movie
// ModelFactory binding.
func TestNeogmaGraphRelatesEdge(t *testing.T) {
	src := `
import { ModelFactory } from 'neogma';

const Movie = ModelFactory({
  label: 'Movie',
  schema: { title: { type: 'string' } },
}, neogma);

const Person = ModelFactory({
  label: 'Person',
  schema: { name: { type: 'string' } },
  relationships: {
    actedIn: { model: Movie, name: 'ACTED_IN', direction: 'out' },
  },
}, neogma);
`
	ents := runNeogma(t, src)

	edge := findNeogmaGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES→ Class:Movie edge, got %+v", ents)
	}
	if edge.Properties["rel_type"] != "ACTED_IN" {
		t.Errorf("rel_type: want ACTED_IN, got %q", edge.Properties["rel_type"])
	}
	if edge.Properties["direction"] != "OUTGOING" {
		t.Errorf("direction: want OUTGOING, got %q", edge.Properties["direction"])
	}
	if edge.Properties["field_name"] != "actedIn" {
		t.Errorf("field_name: want actedIn, got %q", edge.Properties["field_name"])
	}
	if edge.Properties["framework"] != "neogma" {
		t.Errorf("framework: want neogma, got %q", edge.Properties["framework"])
	}

	// direction is a property, not endpoint-swapping: no reverse edge.
	if findNeogmaGraphRelates(ents, "Movie", "Person") != nil {
		t.Error("did not expect a reverse Movie ─GRAPH_RELATES→ Person edge")
	}
}

// TestNeogmaGraphRelatesIncoming proves direction 'in' → INCOMING.
func TestNeogmaGraphRelatesIncoming(t *testing.T) {
	src := `
import { ModelFactory } from 'neogma';

const Person = ModelFactory({
  label: 'Person',
  schema: { name: { type: 'string' } },
}, neogma);

const Movie = ModelFactory({
  label: 'Movie',
  schema: { title: { type: 'string' } },
  relationships: {
    actors: { model: Person, name: 'ACTED_IN', direction: 'in' },
  },
}, neogma);
`
	ents := runNeogma(t, src)
	edge := findNeogmaGraphRelates(ents, "Movie", "Person")
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

// TestNeogmaGraphRelatesSelfEdge proves a self-referential model edge resolves:
// Person ─GRAPH_RELATES(KNOWS)→ Person.
func TestNeogmaGraphRelatesSelfEdge(t *testing.T) {
	src := `
import { ModelFactory } from 'neogma';

const Person = ModelFactory({
  label: 'Person',
  schema: { name: { type: 'string' } },
  relationships: {
    friends: { model: Person, name: 'KNOWS', direction: 'out' },
  },
}, neogma);
`
	ents := runNeogma(t, src)
	edge := findNeogmaGraphRelates(ents, "Person", "Person")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES→ Class:Person self-edge, got %+v", ents)
	}
	if edge.Properties["rel_type"] != "KNOWS" {
		t.Errorf("rel_type: want KNOWS, got %q", edge.Properties["rel_type"])
	}
}

// TestNeogmaGraphRelatesCrossFileDeferred proves honest-partial: a model:
// reference that is NOT a same-file ModelFactory binding emits no edge — the
// topology stays only as the target_model prop on the relationship Component.
func TestNeogmaGraphRelatesCrossFileDeferred(t *testing.T) {
	src := `
import { ModelFactory } from 'neogma';
import { Movie } from './movie';

const Person = ModelFactory({
  label: 'Person',
  schema: { name: { type: 'string' } },
  relationships: {
    actedIn: { model: Movie, name: 'ACTED_IN', direction: 'out' },
  },
}, neogma);
`
	ents := runNeogma(t, src)
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("expected NO GRAPH_RELATES edge for cross-file model, got %+v", r)
			}
		}
	}
	// The relationship Component must still record the deferred target as a prop.
	foundProp := false
	for _, e := range ents {
		if e.Subtype == "relationship" && e.Properties["target_model"] == "Movie" {
			foundProp = true
		}
	}
	if !foundProp {
		t.Errorf("expected relationship Component with target_model=Movie prop, got %+v", ents)
	}
}

// TestNeogmaPlainSchemaFieldNoEdge proves the negative: a model with only a
// schema (no relationships block) emits no GRAPH_RELATES edge.
func TestNeogmaPlainSchemaFieldNoEdge(t *testing.T) {
	src := `
import { ModelFactory } from 'neogma';

const Person = ModelFactory({
  label: 'Person',
  schema: { name: { type: 'string' }, favouriteMovie: { type: 'string' } },
}, neogma);
`
	ents := runNeogma(t, src)
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("plain schema must not emit GRAPH_RELATES, got %+v", r)
			}
		}
	}
}

// TestNeogmaSkipsNonNeogmaSource proves the gate: non-neogma source produces
// nothing.
func TestNeogmaSkipsNonNeogmaSource(t *testing.T) {
	src := `
import mongoose from 'mongoose';
const Person = new mongoose.Schema({ name: String });
`
	ents := runNeogma(t, src)
	if len(ents) != 0 {
		t.Errorf("neogma extractor must not fire on mongoose source, got %+v", ents)
	}
}
