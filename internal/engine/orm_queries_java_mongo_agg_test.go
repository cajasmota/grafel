package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// runMongoAggJava drives scanJavaSpringMongoAggregation over `src` and collects
// the emitted stage entities + join edges.
func runMongoAggJava(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("java", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanJavaSpringMongoAggregation(src, funcs, "svc/BookService.java", "java",
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) {
			// #4244 — drop the node-anchored JOINS_COLLECTION twin so the
			// count/identity assertions below see the collection-anchored
			// edge set they were written against.
			if r.Properties.Get("anchor") == "stage_node" {
				return
			}
			rels = append(rels, r)
		},
	)
	return ents, rels
}

func javaFindJoinTo(rels []types.RelationshipRecord, toClass string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == string(types.RelationshipKindJoinsCollection) &&
			rels[i].ToID == "Class:"+toClass {
			return &rels[i]
		}
	}
	return nil
}

// Fluent LookupOperation with the aggregating collection resolved from the
// `mongoTemplate.aggregate(agg, "books", ...)` executor string argument.
// Asserts the JOINS_COLLECTION(Class:Book -> Class:Author) edge ids + props.
func TestMongoAggJava_FluentLookup_TemplateStringCollection(t *testing.T) {
	src := `
import org.springframework.data.mongodb.core.MongoTemplate;
import org.springframework.data.mongodb.core.aggregation.LookupOperation;
import org.springframework.data.mongodb.core.aggregation.Aggregation;

public class BookService {
    public List<BookView> withAuthors() {
        LookupOperation lookup = LookupOperation.newLookup()
            .from("authors").localField("authorId").foreignField("_id").as("authors");
        Aggregation agg = Aggregation.newAggregation(lookup);
        return mongoTemplate.aggregate(agg, "books", BookView.class).getMappedResults();
    }
}
`
	ents, rels := runMongoAggJava(t, src)

	if n := len(rels); n != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", n, rels)
	}
	j := javaFindJoinTo(rels, "Author")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION edge to Class:Author; rels=%+v", rels)
	}
	if j.FromID != "Class:Book" {
		t.Errorf("join FromID = %q, want Class:Book", j.FromID)
	}
	if j.ToID != "Class:Author" {
		t.Errorf("join ToID = %q, want Class:Author", j.ToID)
	}
	if j.Properties.Get("stage") != "lookup" {
		t.Errorf("join stage = %q, want lookup", j.Properties.Get("stage"))
	}
	if j.Properties.Get("local_field") != "authorId" || j.Properties.Get("foreign_field") != "_id" || j.Properties.Get("as") != "authors" {
		t.Errorf("join props = %+v", j.Properties)
	}

	// Stage entity: one $lookup, collection=books, on the right node kind.
	if n := len(ents); n != 1 {
		t.Fatalf("expected 1 stage entity, got %d: %+v", n, ents)
	}
	e := ents[0]
	if e.Subtype != "$lookup" {
		t.Errorf("stage subtype = %q, want $lookup", e.Subtype)
	}
	if e.Kind != string(types.EntityKindDataAccess) {
		t.Errorf("stage kind = %q, want SCOPE.DataAccess", e.Kind)
	}
	if e.Properties["collection"] != "books" || e.Properties["from"] != "authors" {
		t.Errorf("stage props = %+v", e.Properties)
	}
}

// Aggregating collection resolved from a `Book.class` second argument.
func TestMongoAggJava_FluentLookup_TemplateClassCollection(t *testing.T) {
	src := `
import org.springframework.data.mongodb.core.aggregation.LookupOperation;
public class BookService {
    public void run() {
        LookupOperation lookup = LookupOperation.newLookup()
            .from("publishers").localField("publisherId").foreignField("_id").as("pub");
        Aggregation agg = Aggregation.newAggregation(lookup);
        mongoTemplate.aggregate(agg, Book.class, BookView.class);
    }
}
`
	_, rels := runMongoAggJava(t, src)
	j := javaFindJoinTo(rels, "Publisher")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION edge to Class:Publisher; rels=%+v", rels)
	}
	if j.FromID != "Class:Book" {
		t.Errorf("join FromID = %q, want Class:Book (from Book.class)", j.FromID)
	}
}

// Positional static-factory `Aggregation.lookup("authors","authorId","_id","authors")`.
func TestMongoAggJava_StaticLookupShorthand(t *testing.T) {
	src := `
import org.springframework.data.mongodb.core.aggregation.Aggregation;
@Document("books")
public class BookService {
    public void run() {
        Aggregation agg = Aggregation.newAggregation(
            Aggregation.lookup("authors", "authorId", "_id", "authors"));
        mongoTemplate.aggregate(agg, "books", BookView.class);
    }
}
`
	_, rels := runMongoAggJava(t, src)
	if n := len(rels); n != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", n, rels)
	}
	j := javaFindJoinTo(rels, "Author")
	if j == nil || j.FromID != "Class:Book" {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Author); rels=%+v", rels)
	}
	if j.Properties.Get("local_field") != "authorId" || j.Properties.Get("foreign_field") != "_id" {
		t.Errorf("static-lookup props = %+v", j.Properties)
	}
}

// `@Aggregation(pipeline={...})` repository-method string pipeline. The
// aggregating collection comes from the `MongoRepository<Book, String>` entity.
func TestMongoAggJava_AggregationAnnotationPipeline(t *testing.T) {
	src := `
import org.springframework.data.mongodb.repository.Aggregation;
import org.springframework.data.mongodb.repository.MongoRepository;

public interface BookRepository extends MongoRepository<Book, String> {
    @Aggregation(pipeline = {
        "{ $lookup: { from: 'authors', localField: 'authorId', foreignField: '_id', as: 'authors' } }",
        "{ $match: { status: 'active' } }"
    })
    List<BookView> withAuthors();
}
`
	ents, rels := runMongoAggJava(t, src)
	if n := len(rels); n != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", n, rels)
	}
	j := javaFindJoinTo(rels, "Author")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION edge to Class:Author; rels=%+v", rels)
	}
	if j.FromID != "Class:Book" {
		t.Errorf("join FromID = %q, want Class:Book (from MongoRepository<Book,...>)", j.FromID)
	}
	if j.Properties.Get("local_field") != "authorId" || j.Properties.Get("foreign_field") != "_id" || j.Properties.Get("as") != "authors" {
		t.Errorf("annotation $lookup props = %+v", j.Properties)
	}
	// One stage entity for the $lookup only ($match does not produce an edge,
	// but the annotation path only emits stage entities for $lookup here).
	if n := len(ents); n != 1 {
		t.Fatalf("expected 1 $lookup stage entity, got %d: %+v", n, ents)
	}
	if ents[0].Properties["from"] != "authors" {
		t.Errorf("stage from = %q, want authors", ents[0].Properties["from"])
	}
}

// Two fluent lookups in one method → two distinct join edges to two collections.
func TestMongoAggJava_TwoFluentLookups(t *testing.T) {
	src := `
import org.springframework.data.mongodb.core.aggregation.LookupOperation;
public class BookService {
    public void run() {
        LookupOperation l1 = LookupOperation.newLookup().from("authors").localField("authorId").foreignField("_id").as("authors");
        LookupOperation l2 = LookupOperation.newLookup().from("publishers").localField("publisherId").foreignField("_id").as("pub");
        Aggregation agg = Aggregation.newAggregation(l1, l2);
        mongoTemplate.aggregate(agg, "books", BookView.class);
    }
}
`
	_, rels := runMongoAggJava(t, src)
	if n := len(rels); n != 2 {
		t.Fatalf("expected 2 join edges, got %d: %+v", n, rels)
	}
	if javaFindJoinTo(rels, "Author") == nil {
		t.Errorf("missing JOINS_COLLECTION to Class:Author; rels=%+v", rels)
	}
	if javaFindJoinTo(rels, "Publisher") == nil {
		t.Errorf("missing JOINS_COLLECTION to Class:Publisher; rels=%+v", rels)
	}
}

// NEGATIVE: a dynamic `from` (variable, not a string literal) → NO edge.
func TestMongoAggJava_DynamicFrom_NoEdge(t *testing.T) {
	src := `
import org.springframework.data.mongodb.core.aggregation.LookupOperation;
public class BookService {
    public void run(String coll) {
        LookupOperation lookup = LookupOperation.newLookup()
            .from(coll).localField("authorId").foreignField("_id").as("authors");
        Aggregation agg = Aggregation.newAggregation(lookup);
        mongoTemplate.aggregate(agg, "books", BookView.class);
    }
}
`
	ents, rels := runMongoAggJava(t, src)
	if len(rels) != 0 {
		t.Fatalf("dynamic .from(coll) must yield NO join edge, got %+v", rels)
	}
	if len(ents) != 0 {
		t.Fatalf("dynamic .from(coll) must yield NO stage entity, got %+v", ents)
	}
}

// NEGATIVE: a non-mongo file (no gate signal) is never scanned.
func TestMongoAggJava_GatedOut(t *testing.T) {
	src := `
public class PlainService {
    public void run() {
        list.aggregate(x);
        something.from("authors");
    }
}
`
	ents, rels := runMongoAggJava(t, src)
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("non-spring-mongo file must be gated out, got ents=%+v rels=%+v", ents, rels)
	}
}
