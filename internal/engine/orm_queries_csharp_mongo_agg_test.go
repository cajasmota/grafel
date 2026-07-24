package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// runMongoAggCSharp drives scanCSharpMongoAggregation over `src` and collects
// the emitted stage entities + join edges.
func runMongoAggCSharp(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("csharp", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanCSharpMongoAggregation(src, funcs, "Services/BookService.cs", "csharp",
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

func csharpFindJoinTo(rels []types.RelationshipRecord, toClass string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == string(types.RelationshipKindJoinsCollection) &&
			rels[i].ToID == "Class:"+toClass {
			return &rels[i]
		}
	}
	return nil
}

// Fluent positional `.Aggregate().Lookup("authors","authorId","_id","author")`
// with the aggregating collection resolved from the
// `GetCollection<Book>("books")` quoted string argument. Asserts the
// JOINS_COLLECTION(Class:Book -> Class:Author) edge ids + props.
func TestMongoAggCSharp_FluentLookup_GetCollectionString(t *testing.T) {
	src := `
using MongoDB.Driver;

public class BookService {
    private readonly IMongoDatabase _db;

    public List<BookView> WithAuthors() {
        var results = _db.GetCollection<Book>("books").Aggregate()
            .Lookup("authors", "authorId", "_id", "author")
            .ToList();
        return results;
    }
}
`
	ents, rels := runMongoAggCSharp(t, src)

	if n := len(rels); n != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", n, rels)
	}
	j := csharpFindJoinTo(rels, "Author")
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
	if j.Properties.Get("local_field") != "authorId" || j.Properties.Get("foreign_field") != "_id" || j.Properties.Get("as") != "author" {
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
	if e.Properties["caller"] != "WithAuthors" {
		t.Errorf("stage caller = %q, want WithAuthors", e.Properties["caller"])
	}
}

// Aggregating collection resolved from the `GetCollection<Book>` generic type
// argument when the string argument is dynamic.
func TestMongoAggCSharp_FluentLookup_GenericTypeCollection(t *testing.T) {
	src := `
using MongoDB.Driver;
public class BookService {
    public void Run(string name) {
        _db.GetCollection<Book>(name).Aggregate()
            .Lookup("publishers", "publisherId", "_id", "publisher");
    }
}
`
	_, rels := runMongoAggCSharp(t, src)
	j := csharpFindJoinTo(rels, "Publisher")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION edge to Class:Publisher; rels=%+v", rels)
	}
	if j.FromID != "Class:Book" {
		t.Errorf("join FromID = %q, want Class:Book (from <Book> generic)", j.FromID)
	}
}

// Aggregating collection resolved from an `IMongoCollection<Book>` field typing.
func TestMongoAggCSharp_FluentLookup_FieldTypeCollection(t *testing.T) {
	src := `
using MongoDB.Driver;
public class BookRepository {
    private readonly IMongoCollection<Book> _books;

    public void Run() {
        _books.Aggregate().Lookup("authors", "authorId", "_id", "author");
    }
}
`
	_, rels := runMongoAggCSharp(t, src)
	j := csharpFindJoinTo(rels, "Author")
	if j == nil || j.FromID != "Class:Book" {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Author); rels=%+v", rels)
	}
}

// BsonDocument `$lookup` pipeline stage with `{ "key", "value" }` tuple fields.
func TestMongoAggCSharp_BsonDocumentLookup(t *testing.T) {
	src := `
using MongoDB.Driver;
using MongoDB.Bson;
public class BookService {
    public void Run() {
        var coll = _db.GetCollection<Book>("books");
        var lookup = new BsonDocument("$lookup", new BsonDocument {
            { "from", "authors" },
            { "localField", "authorId" },
            { "foreignField", "_id" },
            { "as", "author" }
        });
        var pipeline = new[] { lookup };
        coll.Aggregate<BsonDocument>(pipeline);
    }
}
`
	ents, rels := runMongoAggCSharp(t, src)
	if n := len(rels); n != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", n, rels)
	}
	j := csharpFindJoinTo(rels, "Author")
	if j == nil || j.FromID != "Class:Book" {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Author); rels=%+v", rels)
	}
	if j.Properties.Get("local_field") != "authorId" || j.Properties.Get("foreign_field") != "_id" || j.Properties.Get("as") != "author" {
		t.Errorf("bson-lookup props = %+v", j.Properties)
	}
	if n := len(ents); n != 1 {
		t.Fatalf("expected 1 stage entity, got %d: %+v", n, ents)
	}
	if ents[0].Properties["from"] != "authors" || ents[0].Properties["collection"] != "books" {
		t.Errorf("bson stage props = %+v", ents[0].Properties)
	}
}

// BsonDocument `$lookup` with the colon map form `{ "from": "authors" }`.
func TestMongoAggCSharp_BsonDocumentLookup_ColonForm(t *testing.T) {
	src := `
using MongoDB.Driver;
public class BookService {
    public void Run() {
        var coll = _db.GetCollection<Book>("books");
        var lookup = new BsonDocument {
            { "$lookup", new BsonDocument {
                { "from", "tags" }, { "localField", "tagId" },
                { "foreignField", "_id" }, { "as", "tags" } } }
        };
    }
}
`
	_, rels := runMongoAggCSharp(t, src)
	j := csharpFindJoinTo(rels, "Tag")
	if j == nil || j.FromID != "Class:Book" {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Tag); rels=%+v", rels)
	}
}

// Two fluent lookups → two distinct join edges with monotonic stage_index.
func TestMongoAggCSharp_TwoFluentLookups(t *testing.T) {
	src := `
using MongoDB.Driver;
public class BookService {
    public void Run() {
        _db.GetCollection<Book>("books").Aggregate()
            .Lookup("authors", "authorId", "_id", "author")
            .Lookup("publishers", "publisherId", "_id", "publisher");
    }
}
`
	ents, rels := runMongoAggCSharp(t, src)
	if len(rels) != 2 {
		t.Fatalf("expected 2 join edges, got %d: %+v", len(rels), rels)
	}
	if csharpFindJoinTo(rels, "Author") == nil || csharpFindJoinTo(rels, "Publisher") == nil {
		t.Fatalf("expected joins to Author and Publisher; rels=%+v", rels)
	}
	if len(ents) != 2 {
		t.Fatalf("expected 2 stage entities, got %d", len(ents))
	}
	if ents[0].Properties["stage_index"] != "0" || ents[1].Properties["stage_index"] != "1" {
		t.Errorf("stage_index not monotonic: %q, %q",
			ents[0].Properties["stage_index"], ents[1].Properties["stage_index"])
	}
}

// Negative: a dynamic `from` (variable, not a literal) yields NO edge.
func TestMongoAggCSharp_DynamicFrom_NoEdge(t *testing.T) {
	src := `
using MongoDB.Driver;
public class BookService {
    public void Run(string fromColl) {
        _db.GetCollection<Book>("books").Aggregate()
            .Lookup(fromColl, "authorId", "_id", "author");
    }
}
`
	ents, rels := runMongoAggCSharp(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no join edge for dynamic from, got %+v", rels)
	}
	if len(ents) != 0 {
		t.Fatalf("expected no stage entity for dynamic from, got %+v", ents)
	}
}

// Negative: dynamic BsonDocument `from` (variable value) yields NO edge.
func TestMongoAggCSharp_BsonDynamicFrom_NoEdge(t *testing.T) {
	src := `
using MongoDB.Driver;
public class BookService {
    public void Run() {
        var coll = _db.GetCollection<Book>("books");
        var lookup = new BsonDocument("$lookup", new BsonDocument {
            { "from", fromVar },
            { "localField", "authorId" }
        });
    }
}
`
	_, rels := runMongoAggCSharp(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no join edge for dynamic bson from, got %+v", rels)
	}
}

// Negative: unresolvable aggregating collection (no GetCollection / typing)
// yields NO edge even with a static fluent lookup.
func TestMongoAggCSharp_NoCollection_NoEdge(t *testing.T) {
	src := `
using MongoDB.Driver;
public class BookService {
    public void Run(IAggregateFluent<Book> agg) {
        agg.Lookup("authors", "authorId", "_id", "author");
    }
}
`
	_, rels := runMongoAggCSharp(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no join edge without a resolvable collection, got %+v", rels)
	}
}

// Gated out: a file with a `.Lookup(...)` chain but no MongoDB.Driver surface
// must not be scanned.
func TestMongoAggCSharp_GatedOut(t *testing.T) {
	src := `
public class Geo {
    public void Run() {
        table.Aggregate().Lookup("authors", "authorId", "_id", "author");
    }
}
`
	ents, rels := runMongoAggCSharp(t, src)
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("expected gated-out file to emit nothing; ents=%+v rels=%+v", ents, rels)
	}
}
