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

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/custom/java"
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
