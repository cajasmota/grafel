package java_test

// neo4j_test.go — tests for the custom_java_neo4j extractor (#3098).
//
// Coverage cells exercised:
//   schema_extraction        (@Node + @Property + @Id scanning)
//   association_extraction   (@Relationship type/direction)
//   relationship_extraction  (@Relationship field → node type pairs)

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/java"
	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func neo4jFI(path, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: "java", Content: []byte(src)}
}

func runNeo4j(t *testing.T, file extreg.FileInput) []ormEnt {
	t.Helper()
	return runORM(t, "custom_java_neo4j", file)
}

// ---------------------------------------------------------------------------
// schema_extraction — @Node class + @Property + @Id
// ---------------------------------------------------------------------------

func TestNeo4jNodeEntityExtracted(t *testing.T) {
	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Id;
import org.springframework.data.neo4j.core.schema.GeneratedValue;
import org.springframework.data.neo4j.core.schema.Property;

@Node("Movie")
public class MovieEntity {

    @Id
    @GeneratedValue
    private Long id;

    @Property("title")
    private String title;

    @Property("released")
    private Integer released;
}
`
	ents := runNeo4j(t, neo4jFI("MovieEntity.java", src))

	if !hasEnt(ents, "SCOPE.Schema", "node", "MovieEntity") {
		t.Errorf("expected MovieEntity node entity, got %v", ents)
	}
	if !hasSub(ents, "id_field") {
		t.Errorf("expected id_field subtype for @Id, got %v", ents)
	}
	if !hasSub(ents, "property") {
		t.Errorf("expected property subtype for @Property fields, got %v", ents)
	}
}

func TestNeo4jNodeLabelFromAnnotation(t *testing.T) {
	src := `
import org.springframework.data.neo4j.core.schema.Node;

@Node("Person")
public class PersonEntity {
    @org.springframework.data.neo4j.core.schema.Id
    private Long id;
}
`
	ents := runNeo4j(t, neo4jFI("PersonEntity.java", src))
	if !hasEnt(ents, "SCOPE.Schema", "node", "PersonEntity") {
		t.Errorf("expected PersonEntity node entity, got %v", ents)
	}
}

func TestNeo4jPropertyNaming(t *testing.T) {
	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Property;

@Node
public class ActorEntity {
    @Property("name")
    private String fullName;

    @Property("born")
    private Integer birthYear;
}
`
	ents := runNeo4j(t, neo4jFI("ActorEntity.java", src))

	if !hasEnt(ents, "SCOPE.Schema", "property", "ActorEntity.name") {
		t.Errorf("expected ActorEntity.name property entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Schema", "property", "ActorEntity.born") {
		t.Errorf("expected ActorEntity.born property entity, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// association_extraction + relationship_extraction — @Relationship
// ---------------------------------------------------------------------------

func TestNeo4jRelationshipExtracted(t *testing.T) {
	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Relationship;
import org.springframework.data.neo4j.core.schema.Relationship.Direction;
import java.util.List;

@Node("Movie")
public class MovieEntity {

    @org.springframework.data.neo4j.core.schema.Id
    private Long id;

    @Relationship(type = "ACTED_IN", direction = Direction.INCOMING)
    private List<ActorEntity> actors;

    @Relationship(type = "DIRECTED", direction = Direction.INCOMING)
    private List<DirectorEntity> directors;
}
`
	ents := runNeo4j(t, neo4jFI("MovieEntity.java", src))

	if !hasSub(ents, "relationship") {
		t.Errorf("expected relationship subtype for @Relationship, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "relationship", "MovieEntity.ACTED_IN") {
		t.Errorf("expected MovieEntity.ACTED_IN relationship entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "relationship", "MovieEntity.DIRECTED") {
		t.Errorf("expected MovieEntity.DIRECTED relationship entity, got %v", ents)
	}
}

func TestNeo4jRelationshipOutgoing(t *testing.T) {
	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Relationship;

@Node
public class PersonEntity {

    @org.springframework.data.neo4j.core.schema.Id
    private Long id;

    @Relationship(type = "KNOWS", direction = Relationship.Direction.OUTGOING)
    private List<PersonEntity> friends;
}
`
	ents := runNeo4j(t, neo4jFI("PersonEntity.java", src))

	if !hasEnt(ents, "SCOPE.Component", "relationship", "PersonEntity.KNOWS") {
		t.Errorf("expected PersonEntity.KNOWS relationship entity, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Neo4j OGM (@NodeEntity / @RelationshipEntity)
// ---------------------------------------------------------------------------

func TestNeo4jOGMNodeEntityExtracted(t *testing.T) {
	src := `
import org.neo4j.ogm.annotation.NodeEntity;
import org.neo4j.ogm.annotation.Relationship;
import org.neo4j.ogm.annotation.Id;

@NodeEntity
public class UserNode {

    @Id
    private Long id;

    private String username;

    @Relationship(type = "FOLLOWS", direction = Relationship.Direction.OUTGOING)
    private List<UserNode> following;
}
`
	ents := runNeo4j(t, neo4jFI("UserNode.java", src))

	if !hasEnt(ents, "SCOPE.Schema", "node", "UserNode") {
		t.Errorf("expected UserNode node entity from @NodeEntity, got %v", ents)
	}
	if !hasSub(ents, "relationship") {
		t.Errorf("expected relationship entity from OGM @Relationship, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// GRAPH_RELATES — graph-schema domain topology as a traversable edge (#3611)
// ---------------------------------------------------------------------------

// findGraphRelates returns the GRAPH_RELATES edge from the @Node entity named
// `fromNode` to the structural ref "Class:<toType>", or nil if absent.
func findGraphRelates(ents []types.EntityRecord, fromNode, toType string) *types.RelationshipRecord {
	for i := range ents {
		if !(ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "node" && ents[i].Name == fromNode) {
			continue
		}
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == string(types.RelationshipKindGraphRelates) && r.ToID == "Class:"+toType {
				return r
			}
		}
	}
	return nil
}

// TestNeo4jGraphRelatesEdge proves the headline capability: an @Node owning an
// @Relationship field to another same-file @Node type emits a traversable
// GRAPH_RELATES edge carrying the Neo4j rel_type and direction — the graph-DB
// analogue of JOINS_COLLECTION.  Person ──GRAPH_RELATES(ACTED_IN)──▶ Movie.
func TestNeo4jGraphRelatesEdge(t *testing.T) {
	ctx := context.Background()
	e, _ := extreg.Get("custom_java_neo4j")

	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Relationship;
import org.springframework.data.neo4j.core.schema.Relationship.Direction;

@Node("Movie")
public class Movie {
    @org.springframework.data.neo4j.core.schema.Id
    private Long id;
}

@Node("Person")
public class Person {
    @org.springframework.data.neo4j.core.schema.Id
    private Long id;

    @Relationship(type = "ACTED_IN", direction = Direction.OUTGOING)
    private Movie movie;
}
`
	ents, err := e.Extract(ctx, extreg.FileInput{Path: "Graph.java", Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	edge := findGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES→ Class:Movie edge, got %+v", ents)
	}
	if edge.Properties["rel_type"] != "ACTED_IN" {
		t.Errorf("rel_type: want ACTED_IN, got %q", edge.Properties["rel_type"])
	}
	if edge.Properties["direction"] != "OUTGOING" {
		t.Errorf("direction: want OUTGOING, got %q", edge.Properties["direction"])
	}
	if edge.Properties["field_name"] != "movie" {
		t.Errorf("field_name: want movie, got %q", edge.Properties["field_name"])
	}
	if edge.Properties["framework"] != "neo4j" {
		t.Errorf("framework: want neo4j, got %q", edge.Properties["framework"])
	}

	// The owner @Node entity is the source ("table"); the reverse edge must NOT
	// exist (direction is encoded as a property, not by swapping endpoints).
	if findGraphRelates(ents, "Movie", "Person") != nil {
		t.Error("did not expect a reverse Movie ─GRAPH_RELATES→ Person edge")
	}
}

// TestNeo4jGraphRelatesGenericCollection proves the edge resolves through a
// generic collection field: @Relationship List<Person> on Person → self-edge
// Person ─GRAPH_RELATES(KNOWS)→ Person.
func TestNeo4jGraphRelatesGenericCollection(t *testing.T) {
	ctx := context.Background()
	e, _ := extreg.Get("custom_java_neo4j")

	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Relationship;
import java.util.List;

@Node
public class Person {
    @org.springframework.data.neo4j.core.schema.Id
    private Long id;

    @Relationship(type = "KNOWS", direction = Relationship.Direction.OUTGOING)
    private List<Person> friends;
}
`
	ents, err := e.Extract(ctx, extreg.FileInput{Path: "Person.java", Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edge := findGraphRelates(ents, "Person", "Person")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES→ Class:Person self-edge, got %+v", ents)
	}
	if edge.Properties["rel_type"] != "KNOWS" {
		t.Errorf("rel_type: want KNOWS, got %q", edge.Properties["rel_type"])
	}
}

// TestNeo4jGraphRelatesCrossFileDeferred proves honest-partial behaviour: when
// the @Relationship target type is NOT a same-file @Node (cross-file), no
// GRAPH_RELATES edge is emitted — the topology stays as `target_node` props on
// the relationship Component only. (ActorEntity/DirectorEntity are referenced
// but never declared @Node here.)
func TestNeo4jGraphRelatesCrossFileDeferred(t *testing.T) {
	ctx := context.Background()
	e, _ := extreg.Get("custom_java_neo4j")

	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Relationship;
import org.springframework.data.neo4j.core.schema.Relationship.Direction;
import java.util.List;

@Node("Movie")
public class MovieEntity {
    @org.springframework.data.neo4j.core.schema.Id
    private Long id;

    @Relationship(type = "ACTED_IN", direction = Direction.INCOMING)
    private List<ActorEntity> actors;
}
`
	ents, err := e.Extract(ctx, extreg.FileInput{Path: "MovieEntity.java", Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("expected NO GRAPH_RELATES edge for cross-file target, got %+v", r)
			}
		}
	}
}

// TestNeo4jGraphRelatesNonRelationshipFieldNoEdge proves the negative: a plain
// @Property field (no @Relationship) on an @Node emits no GRAPH_RELATES edge.
func TestNeo4jGraphRelatesNonRelationshipFieldNoEdge(t *testing.T) {
	ctx := context.Background()
	e, _ := extreg.Get("custom_java_neo4j")

	src := `
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Property;

@Node("Movie")
public class Movie {
    @org.springframework.data.neo4j.core.schema.Id
    private Long id;
}

@Node("Person")
public class Person {
    @Property("name")
    private String name;

    private Movie favourite;
}
`
	ents, err := e.Extract(ctx, extreg.FileInput{Path: "Graph.java", Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("plain field must not emit GRAPH_RELATES, got %+v", r)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Gate — non-Neo4j source must produce no entities
// ---------------------------------------------------------------------------

func TestNeo4jSkipsNonNeo4jSource(t *testing.T) {
	src := `
@Entity
@Table(name = "movies")
public class Movie {
    @Id
    private Long id;
    private String title;
}
`
	ents := runNeo4j(t, neo4jFI("Movie.java", src))
	if len(ents) != 0 {
		t.Errorf("neo4j extractor must not fire on plain JPA source, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Integration: fixture file
// ---------------------------------------------------------------------------

func TestNeo4jFixtureMovieEntity(t *testing.T) {
	ctx := context.Background()
	e, ok := extreg.Get("custom_java_neo4j")
	if !ok {
		t.Fatal("custom_java_neo4j not registered")
	}

	src := `
import org.springframework.data.neo4j.core.schema.Id;
import org.springframework.data.neo4j.core.schema.GeneratedValue;
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Property;
import org.springframework.data.neo4j.core.schema.Relationship;
import org.springframework.data.neo4j.core.schema.Relationship.Direction;
import java.util.List;

@Node("Movie")
public class MovieEntity {

    @Id
    @GeneratedValue
    private Long id;

    @Property("title")
    private String title;

    @Property("released")
    private Integer released;

    @Relationship(type = "ACTED_IN", direction = Direction.INCOMING)
    private List<ActorEntity> actors;

    @Relationship(type = "DIRECTED", direction = Direction.INCOMING)
    private List<DirectorEntity> directors;
}
`
	ents, err := e.Extract(ctx, extreg.FileInput{
		Path:     "testdata/fixtures/sources/java/neo4j/MovieEntity.java",
		Language: "java",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) == 0 {
		t.Fatal("expected entities from fixture, got none")
	}

	nodeFound, relFound := false, false
	for _, ent := range ents {
		if ent.Subtype == "node" {
			nodeFound = true
		}
		if ent.Subtype == "relationship" {
			relFound = true
		}
	}
	if !nodeFound {
		t.Errorf("expected at least one node entity from fixture, got %v", ents)
	}
	if !relFound {
		t.Errorf("expected at least one relationship entity from fixture, got %v", ents)
	}
}
