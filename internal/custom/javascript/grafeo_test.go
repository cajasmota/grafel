package javascript_test

// grafeo_test.go — tests for the custom_js_grafeo extractor (epic #3606).
//
// Covers grafeo-ogm (Neo4j TS OGM, GraphQL-SDL-driven) graph-schema extraction
// and the headline GRAPH_RELATES topology edge: a @node type whose @relationship
// field targets a same-document @node type emits a traversable
// owner ──GRAPH_RELATES(rel_type,direction)──▶ Class:<TargetLabel> edge.
// Mirrors the neomodel (#3609) / neogma (#3610) / Java SDN (#3663) templates.
//
// Fixtures use grafeo-ogm's ACTUAL SDL syntax, cited from the cloned repo:
//   - examples/schema.graphql (Author/Book/Category/Review + WrittenBy @relationshipProperties)
//   - README.md "Quick Start" Book/Author/Category schema
//   - tests/schema-parser.spec.ts (@node(labels:[...]), @relationship direction)

import (
	"context"
	"testing"

	_ "github.com/cajasmota/grafel/internal/custom/javascript"
	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// findGrafeoGraphRelates returns the GRAPH_RELATES edge from the node entity
// labelled fromLabel to "Class:<toLabel>", or nil.
func findGrafeoGraphRelates(ents []types.EntityRecord, fromLabel, toLabel string) *types.RelationshipRecord {
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

func grafeoHasNode(ents []types.EntityRecord, label string) bool {
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "node" && e.Name == label {
			return true
		}
	}
	return false
}

func runGrafeo(t *testing.T, lang, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_grafeo")
	if !ok {
		t.Fatal("custom_js_grafeo not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "schema.graphql", Language: lang, Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// ---------------------------------------------------------------------------
// node extraction — @node type label
// ---------------------------------------------------------------------------

// TestGrafeoNodesExtracted proves each @node type/interface becomes a
// SCOPE.Schema/node, while a @relationshipProperties type is excluded (negative).
// Fixture: examples/schema.graphql (verbatim shape).
func TestGrafeoNodesExtracted(t *testing.T) {
	src := `
interface Entity @node {
  id: ID! @id
  name: String!
}

type Author @node implements Entity {
  id: ID! @id @unique
  name: String!
  books: [Book!]! @relationship(type: "WRITTEN_BY", direction: IN, properties: "WrittenBy")
}

type Book @node {
  id: ID! @id @unique
  title: String!
  author: Author! @relationship(type: "WRITTEN_BY", direction: OUT, properties: "WrittenBy")
  categories: [Category!]! @relationship(type: "IN_CATEGORY", direction: OUT)
}

type Category @node {
  id: ID! @id @unique
  name: String!
}

type WrittenBy @relationshipProperties {
  role: String
  year: Int
}
`
	ents := runGrafeo(t, "graphql", src)
	for _, label := range []string{"Entity", "Author", "Book", "Category"} {
		if !grafeoHasNode(ents, label) {
			t.Errorf("expected @node entity %q, got %+v", label, ents)
		}
	}
	// @relationshipProperties type must NOT become a node (negative).
	if grafeoHasNode(ents, "WrittenBy") {
		t.Errorf("@relationshipProperties type WrittenBy must NOT be a node, got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// GRAPH_RELATES — the headline node→node topology edge
// ---------------------------------------------------------------------------

// TestGrafeoGraphRelatesOutgoing proves Book ─GRAPH_RELATES(WRITTEN_BY,OUTGOING)→
// Class:Author for `author: Author! @relationship(type:"WRITTEN_BY", direction:OUT)`.
// Fixture: examples/schema.graphql Book.author.
func TestGrafeoGraphRelatesOutgoing(t *testing.T) {
	src := `
type Author @node {
  id: ID! @id @unique
  name: String!
}

type Book @node {
  id: ID! @id @unique
  title: String!
  author: Author! @relationship(type: "WRITTEN_BY", direction: OUT, properties: "WrittenBy")
}
`
	ents := runGrafeo(t, "graphql", src)
	edge := findGrafeoGraphRelates(ents, "Book", "Author")
	if edge == nil {
		t.Fatalf("expected Book ─GRAPH_RELATES→ Class:Author edge, got %+v", ents)
	}
	if edge.Properties.Get("rel_type") != "WRITTEN_BY" {
		t.Errorf("rel_type: want WRITTEN_BY, got %q", edge.Properties.Get("rel_type"))
	}
	if edge.Properties.Get("direction") != "OUTGOING" {
		t.Errorf("direction: want OUTGOING, got %q", edge.Properties.Get("direction"))
	}
	if edge.Properties.Get("field_name") != "author" {
		t.Errorf("field_name: want author, got %q", edge.Properties.Get("field_name"))
	}
	if edge.Properties.Get("framework") != "grafeo-ogm" {
		t.Errorf("framework: want grafeo-ogm, got %q", edge.Properties.Get("framework"))
	}
	if edge.Properties.Get("relationship_properties") != "WrittenBy" {
		t.Errorf("relationship_properties: want WrittenBy, got %q", edge.Properties.Get("relationship_properties"))
	}
	// direction is a property, not endpoint-swapping: no reverse edge from this field.
	if findGrafeoGraphRelates(ents, "Author", "Book") != nil {
		t.Error("did not expect a reverse Author ─GRAPH_RELATES→ Book edge from Book.author")
	}
}

// TestGrafeoGraphRelatesIncoming proves direction IN → INCOMING for
// `books: [Book!]! @relationship(type:"WRITTEN_BY", direction:IN)`.
// Fixture: examples/schema.graphql Author.books / README Author.books.
func TestGrafeoGraphRelatesIncoming(t *testing.T) {
	src := `
type Book @node {
  id: ID! @id @unique
  title: String!
}

type Author @node {
  id: ID! @id @unique
  name: String!
  books: [Book!]! @relationship(type: "WRITTEN_BY", direction: IN, properties: "WrittenBy")
}
`
	ents := runGrafeo(t, "graphql", src)
	edge := findGrafeoGraphRelates(ents, "Author", "Book")
	if edge == nil {
		t.Fatalf("expected Author ─GRAPH_RELATES→ Class:Book edge, got %+v", ents)
	}
	if edge.Properties.Get("direction") != "INCOMING" {
		t.Errorf("direction: want INCOMING, got %q", edge.Properties.Get("direction"))
	}
	if edge.Properties.Get("rel_type") != "WRITTEN_BY" {
		t.Errorf("rel_type: want WRITTEN_BY, got %q", edge.Properties.Get("rel_type"))
	}
}

// TestGrafeoInlineTypeDefs proves the extractor works on grafeo SDL embedded in
// a TS template literal (the `new OGM({ typeDefs })` form), not just standalone
// .graphql files. Fixture: README Quick Start + tests/tier1-features.spec.ts
// (`typeDefs: \`type Book @node { ... }\“).
func TestGrafeoInlineTypeDefs(t *testing.T) {
	src := "import { OGM } from 'grafeo-ogm';\n" +
		"const ogm = new OGM({\n" +
		"  typeDefs: `\n" +
		"    type Category @node {\n" +
		"      id: ID! @id @unique\n" +
		"      name: String!\n" +
		"    }\n" +
		"    type Book @node {\n" +
		"      id: ID! @id @unique\n" +
		"      categories: [Category!]! @relationship(type: \"IN_CATEGORY\", direction: OUT)\n" +
		"    }\n" +
		"  `,\n" +
		"  driver,\n" +
		"});\n"
	ents := runGrafeo(t, "typescript", src)
	if !grafeoHasNode(ents, "Book") || !grafeoHasNode(ents, "Category") {
		t.Fatalf("expected Book + Category nodes from inline typeDefs, got %+v", ents)
	}
	edge := findGrafeoGraphRelates(ents, "Book", "Category")
	if edge == nil {
		t.Fatalf("expected Book ─GRAPH_RELATES→ Class:Category edge, got %+v", ents)
	}
	if edge.Properties.Get("rel_type") != "IN_CATEGORY" || edge.Properties.Get("direction") != "OUTGOING" {
		t.Errorf("want IN_CATEGORY/OUTGOING, got %q/%q",
			edge.Properties.Get("rel_type"), edge.Properties.Get("direction"))
	}
}

// TestGrafeoNodeLabelsDirective proves @node(labels: ["Entity","User"]) sets the
// primary label to the first entry. Fixture: tests/schema-parser.spec.ts
// "should parse @node labels directive".
func TestGrafeoNodeLabelsDirective(t *testing.T) {
	src := `
type Permission @node {
  id: ID! @id @unique
}

type User @node(labels: ["Entity", "User"]) {
  id: ID! @id @unique
  name: String!
  permissions: [Permission!]! @relationship(type: "HAS_PERMISSION", direction: OUT)
}
`
	ents := runGrafeo(t, "graphql", src)
	if !grafeoHasNode(ents, "Entity") {
		t.Fatalf("expected node labelled Entity (first @node label), got %+v", ents)
	}
	if grafeoHasNode(ents, "User") {
		t.Errorf("primary label should be Entity, not the type name User; got %+v", ents)
	}
	// Owner label uses the primary label "Entity".
	edge := findGrafeoGraphRelates(ents, "Entity", "Permission")
	if edge == nil {
		t.Fatalf("expected Entity ─GRAPH_RELATES→ Class:Permission edge, got %+v", ents)
	}
	if edge.Properties.Get("rel_type") != "HAS_PERMISSION" {
		t.Errorf("rel_type: want HAS_PERMISSION, got %q", edge.Properties.Get("rel_type"))
	}
}

// TestGrafeoSelfEdge proves a self-referential relationship resolves:
// User ─GRAPH_RELATES(FOLLOWS)→ User. Fixture: README "Social graphs" use case.
func TestGrafeoSelfEdge(t *testing.T) {
	src := `
type User @node {
  id: ID! @id @unique
  follows: [User!]! @relationship(type: "FOLLOWS", direction: OUT)
}
`
	ents := runGrafeo(t, "graphql", src)
	edge := findGrafeoGraphRelates(ents, "User", "User")
	if edge == nil {
		t.Fatalf("expected User ─GRAPH_RELATES→ Class:User self-edge, got %+v", ents)
	}
	if edge.Properties.Get("rel_type") != "FOLLOWS" {
		t.Errorf("rel_type: want FOLLOWS, got %q", edge.Properties.Get("rel_type"))
	}
}

// TestGrafeoCrossDocTargetDeferred proves honest-partial: a @relationship whose
// target GraphQL type is NOT a @node in the same document emits no edge — the
// topology stays only as the target_type prop on the relationship Component.
func TestGrafeoCrossDocTargetDeferred(t *testing.T) {
	src := `
type Book @node {
  id: ID! @id @unique
  publisher: Publisher! @relationship(type: "PUBLISHED_BY", direction: OUT)
}
`
	ents := runGrafeo(t, "graphql", src)
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("expected NO GRAPH_RELATES edge for non-node target, got %+v", r)
			}
		}
	}
	foundProp := false
	for _, e := range ents {
		if e.Subtype == "relationship" && e.Properties["target_type"] == "Publisher" {
			foundProp = true
			if _, ok := e.Properties["target_node"]; ok {
				t.Errorf("target_node prop must be absent for deferred target, got %+v", e.Properties)
			}
		}
	}
	if !foundProp {
		t.Errorf("expected relationship Component with target_type=Publisher prop, got %+v", ents)
	}
}

// TestGrafeoPlainNodeNoEdge proves the negative: a @node with no @relationship
// field emits no GRAPH_RELATES edge.
func TestGrafeoPlainNodeNoEdge(t *testing.T) {
	src := `
type Category @node {
  id: ID! @id @unique
  name: String!
}

type Tag @node {
  id: ID! @id @unique
  label: String!
}
`
	// No @relationship anywhere → gate requires @node + (@relationship OR marker).
	// Add a marker so the gate fires but still no rel edges expected.
	src = "import { OGM } from 'grafeo-ogm';\n" + src
	ents := runGrafeo(t, "typescript", src)
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("plain @node must not emit GRAPH_RELATES, got %+v", r)
			}
		}
	}
}

// TestGrafeoSkipsNonGrafeoSource proves the gate: a Lighthouse-style GraphQL
// schema (server resolver directives, no @node) produces nothing.
func TestGrafeoSkipsNonGrafeoSource(t *testing.T) {
	src := `
type Query {
  users: [User!]! @all
  user(id: ID! @eq): User @find
}

type User {
  id: ID!
  name: String!
}
`
	ents := runGrafeo(t, "graphql", src)
	if len(ents) != 0 {
		t.Errorf("grafeo extractor must not fire on Lighthouse (@all/@find) schema, got %+v", ents)
	}
}

// TestGrafeoWrongLanguage proves the language gate: grafeo SDL content in a
// non-{ts,js,graphql} language produces nothing.
func TestGrafeoWrongLanguage(t *testing.T) {
	src := `
type Book @node {
  author: Author! @relationship(type: "WRITTEN_BY", direction: OUT)
}
type Author @node { id: ID! @id }
`
	ents := runGrafeo(t, "python", src)
	if len(ents) != 0 {
		t.Errorf("grafeo extractor must not fire on language=python, got %+v", ents)
	}
}
