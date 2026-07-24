package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// runMongoAggPHP drives scanPHPMongoAggregation over `src` and collects the
// emitted stage entities + join edges.
func runMongoAggPHP(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("php", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanPHPMongoAggregation(src, funcs, "src/BookService.php", "php",
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

func phpFindJoinTo(rels []types.RelationshipRecord, toClass string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == string(types.RelationshipKindJoinsCollection) &&
			rels[i].ToID == "Class:"+toClass {
			return &rels[i]
		}
	}
	return nil
}

// Doctrine ODM fluent aggregation builder:
// createAggregationBuilder(Book::class)->lookup('authors')->localField(..)
// ->foreignField(..)->alias(..) → JOINS_COLLECTION(Class:Book -> Class:Author).
func TestMongoAggPHP_DoctrineFluentLookup(t *testing.T) {
	src := `<?php
namespace App;
use Doctrine\ODM\MongoDB\DocumentManager;

class BookService {
    public function withAuthors(DocumentManager $dm) {
        $builder = $dm->createAggregationBuilder(Book::class);
        $builder->lookup('authors')
            ->localField('author_id')
            ->foreignField('_id')
            ->alias('author');
        return $builder->getAggregation();
    }
}
`
	ents, rels := runMongoAggPHP(t, src)

	if n := len(rels); n != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", n, rels)
	}
	j := phpFindJoinTo(rels, "Author")
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
	if j.Properties.Get("local_field") != "author_id" || j.Properties.Get("foreign_field") != "_id" || j.Properties.Get("as") != "author" {
		t.Errorf("join props = %+v", j.Properties)
	}

	// Stage entity: one $lookup, collection=Book, on the right node kind.
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
	if e.Properties["collection"] != "Book" || e.Properties["from"] != "authors" {
		t.Errorf("stage props = %+v", e.Properties)
	}
	if e.Properties["caller"] != "withAuthors" {
		t.Errorf("stage caller = %q, want withAuthors", e.Properties["caller"])
	}
}

// Two fluent lookups in one builder → two distinct join edges to two collections.
func TestMongoAggPHP_DoctrineTwoLookups(t *testing.T) {
	src := `<?php
class CatalogService {
    public function run($dm) {
        $b = $dm->createAggregationBuilder(Book::class);
        $b->lookup('authors')->localField('author_id')->foreignField('_id')->alias('author');
        $b->lookup('publishers')->localField('publisher_id')->foreignField('_id')->alias('pub');
    }
}
`
	_, rels := runMongoAggPHP(t, src)
	if n := len(rels); n != 2 {
		t.Fatalf("expected 2 join edges, got %d: %+v", n, rels)
	}
	if j := phpFindJoinTo(rels, "Author"); j == nil || j.FromID != "Class:Book" {
		t.Errorf("missing JOINS_COLLECTION(Class:Book -> Class:Author); rels=%+v", rels)
	}
	if j := phpFindJoinTo(rels, "Publisher"); j == nil || j.FromID != "Class:Book" {
		t.Errorf("missing JOINS_COLLECTION(Class:Book -> Class:Publisher); rels=%+v", rels)
	}
}

// Doctrine ODM mapping-reference annotation: @ReferenceMany(targetDocument=
// Author::class) on a @Document class → JOINS_COLLECTION(Class:Book ->
// Class:Author), mirroring the Mongoose-ref convention.
func TestMongoAggPHP_DoctrineReferenceAnnotation(t *testing.T) {
	src := `<?php
/**
 * @Document(collection="books")
 */
class Book {
    /**
     * @ReferenceMany(targetDocument=Author::class)
     */
    private $authors;

    /**
     * @ReferenceOne(targetDocument=Publisher::class)
     */
    private $publisher;
}
`
	_, rels := runMongoAggPHP(t, src)
	if n := len(rels); n != 2 {
		t.Fatalf("expected 2 reference join edges, got %d: %+v", n, rels)
	}
	j := phpFindJoinTo(rels, "Author")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION edge to Class:Author; rels=%+v", rels)
	}
	if j.FromID != "Class:Book" {
		t.Errorf("reference join FromID = %q, want Class:Book", j.FromID)
	}
	if j.Properties.Get("via") != "reference" || j.Properties.Get("reference") != "ReferenceMany" {
		t.Errorf("reference join props = %+v", j.Properties)
	}
	if p := phpFindJoinTo(rels, "Publisher"); p == nil || p.FromID != "Class:Book" {
		t.Errorf("missing JOINS_COLLECTION(Class:Book -> Class:Publisher); rels=%+v", rels)
	}
}

// Doctrine ODM PHP 8 attribute form: #[Document] / #[ReferenceMany(
// targetDocument: Author::class)].
func TestMongoAggPHP_DoctrineReferenceAttribute(t *testing.T) {
	src := `<?php
use Doctrine\ODM\MongoDB\Mapping\Annotations as ODM;

#[ODM\Document(collection: "books")]
class Book {
    #[ODM\ReferenceMany(targetDocument: Author::class)]
    public iterable $authors;
}
`
	_, rels := runMongoAggPHP(t, src)
	if n := len(rels); n != 1 {
		t.Fatalf("expected 1 reference join edge, got %d: %+v", n, rels)
	}
	j := phpFindJoinTo(rels, "Author")
	if j == nil || j.FromID != "Class:Book" {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Author); rels=%+v", rels)
	}
	if j.Properties.Get("reference") != "ReferenceMany" {
		t.Errorf("reference kind = %q, want ReferenceMany", j.Properties.Get("reference"))
	}
}

// Laravel-MongoDB (jenssegers) raw $lookup PHP-array pipeline:
// Book::raw(fn($c) => $c->aggregate([['$lookup' => ['from' => 'authors', ...]]]))
// → JOINS_COLLECTION(Class:Book -> Class:Author).
func TestMongoAggPHP_LaravelRawLookup(t *testing.T) {
	src := `<?php
namespace App\Models;
use Jenssegers\Mongodb\Eloquent\Model;

class Book extends Model {
    protected $connection = 'mongodb';

    public static function withAuthors() {
        return self::raw(function ($collection) {
            return $collection->aggregate([
                ['$lookup' => ['from' => 'authors', 'localField' => 'author_id', 'foreignField' => '_id', 'as' => 'author']],
                ['$match' => ['status' => 'active']],
            ]);
        });
    }
}
`
	ents, rels := runMongoAggPHP(t, src)
	if n := len(rels); n != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", n, rels)
	}
	j := phpFindJoinTo(rels, "Author")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION edge to Class:Author; rels=%+v", rels)
	}
	if j.FromID != "Class:Book" {
		t.Errorf("join FromID = %q, want Class:Book (from Book::raw)", j.FromID)
	}
	if j.Properties.Get("local_field") != "author_id" || j.Properties.Get("foreign_field") != "_id" || j.Properties.Get("as") != "author" {
		t.Errorf("laravel $lookup props = %+v", j.Properties)
	}
	// One stage entity for the $lookup ($match yields no edge; only $lookup
	// produces a stage entity here).
	if n := len(ents); n != 1 {
		t.Fatalf("expected 1 $lookup stage entity, got %d: %+v", n, ents)
	}
	if ents[0].Properties["from"] != "authors" || ents[0].Properties["collection"] != "Book" {
		t.Errorf("stage props = %+v", ents[0].Properties)
	}
}

// NEGATIVE: a dynamic `from` (a variable, not a string literal) → NO edge.
func TestMongoAggPHP_DynamicFrom_NoEdge(t *testing.T) {
	src := `<?php
class BookService {
    public function run($dm, $coll) {
        $b = $dm->createAggregationBuilder(Book::class);
        $b->lookup($coll)->localField('author_id')->foreignField('_id')->alias('author');
    }
}
`
	ents, rels := runMongoAggPHP(t, src)
	if len(rels) != 0 {
		t.Fatalf("dynamic ->lookup($coll) must yield NO join edge, got %+v", rels)
	}
	if len(ents) != 0 {
		t.Fatalf("dynamic ->lookup($coll) must yield NO stage entity, got %+v", ents)
	}
}

// NEGATIVE: a dynamic targetDocument on a reference annotation → NO edge.
func TestMongoAggPHP_DynamicTargetDocument_NoEdge(t *testing.T) {
	src := `<?php
/** @Document */
class Book {
    /** @ReferenceMany(targetDocument=$target) */
    private $authors;
}
`
	_, rels := runMongoAggPHP(t, src)
	if len(rels) != 0 {
		t.Fatalf("dynamic targetDocument must yield NO join edge, got %+v", rels)
	}
}

// NEGATIVE: a non-mongo file (no gate signal) is never scanned.
func TestMongoAggPHP_GatedOut(t *testing.T) {
	src := `<?php
class PlainService {
    public function run($list) {
        $list->aggregate([1, 2, 3]);
    }
}
`
	ents, rels := runMongoAggPHP(t, src)
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("non-mongo file must be gated out, got ents=%+v rels=%+v", ents, rels)
	}
}
