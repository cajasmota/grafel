package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runMongoidAgg drives scanRubyMongoidAggregation over `src` and collects the
// emitted stage entities + join/relation edges.
func runMongoidAgg(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("ruby", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanRubyMongoidAggregation(src, funcs, "app/models/book.rb", "ruby",
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)
	return ents, rels
}

func mongoidFindJoin(rels []types.RelationshipRecord, from, to string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == string(types.RelationshipKindJoinsCollection) &&
			rels[i].FromID == "Class:"+from && rels[i].ToID == "Class:"+to {
			return &rels[i]
		}
	}
	return nil
}

// Aggregation $lookup with Ruby hash-rocket syntax. Asserts the exact
// JOINS_COLLECTION(Class:Book -> Class:Author) edge ids + the join sub-fields,
// plus the SCOPE.DataAccess stage entity.
func TestMongoidAgg_Lookup_HashRocket(t *testing.T) {
	src := `
class Book
  include Mongoid::Document
  def self.with_authors
    collection.aggregate([
      { '$lookup' => { 'from' => 'authors', 'localField' => 'author_id', 'foreignField' => '_id', 'as' => 'author' } }
    ])
  end
end

result = Book.collection.aggregate([
  { '$lookup' => { 'from' => 'authors', 'localField' => 'author_id', 'foreignField' => '_id', 'as' => 'author' } }
])
`
	ents, rels := runMongoidAgg(t, src)

	j := mongoidFindJoin(rels, "Book", "Author")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Author); rels=%+v", rels)
	}
	if got := j.Properties["local_field"]; got != "author_id" {
		t.Errorf("local_field = %q, want author_id", got)
	}
	if got := j.Properties["foreign_field"]; got != "_id" {
		t.Errorf("foreign_field = %q, want _id", got)
	}
	if got := j.Properties["as"]; got != "author" {
		t.Errorf("as = %q, want author", got)
	}
	// The JOINS_COLLECTION edge carries the SHARED mongoAggPatternType (the
	// shared mongoAggJoinEdge builder), matching the JS/Python/Go/Java siblings;
	// the stage ENTITY carries the Mongoid-specific tag (asserted below).
	if got := j.Properties["pattern_type"]; got != mongoAggPatternType {
		t.Errorf("edge pattern_type = %q, want %q", got, mongoAggPatternType)
	}

	// Stage entity asserted.
	var foundStage bool
	for _, e := range ents {
		if e.Kind == mongoAggStageEntityKind && e.Subtype == "$lookup" &&
			e.Properties["collection"] == "Book" && e.Properties["from"] == "authors" {
			foundStage = true
		}
	}
	if !foundStage {
		t.Errorf("expected a $lookup SCOPE.DataAccess stage entity for Book; ents=%+v", ents)
	}
}

// store_in collection override resolves the aggregating collection token, but
// capitalisedSingular canonicalises it back to the same Class node.
func TestMongoidAgg_StoreInCollection(t *testing.T) {
	src := `
class Book
  include Mongoid::Document
  store_in collection: 'tomes'
end

Book.collection.aggregate([
  { '$lookup' => { 'from' => 'authors', 'as' => 'author' } }
])
`
	_, rels := runMongoidAgg(t, src)
	// "tomes" -> capitalisedSingular -> "Tome".
	if j := mongoidFindJoin(rels, "Tome", "Author"); j == nil {
		t.Fatalf("expected JOINS_COLLECTION(Class:Tome -> Class:Author) via store_in; rels=%+v", rels)
	}
}

// Association macros in a Mongoid::Document class emit relation edges.
// belongs_to :author -> Book->Author ; has_many :reviews -> Book->Review.
func TestMongoidAssociations_BelongsToHasMany(t *testing.T) {
	src := `
class Book
  include Mongoid::Document
  belongs_to :author
  has_many :reviews
  embeds_many :pages
end
`
	_, rels := runMongoidAgg(t, src)

	if j := mongoidFindJoin(rels, "Book", "Author"); j == nil {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Author) for belongs_to; rels=%+v", rels)
	} else {
		if j.Properties["via"] != "belongs_to" {
			t.Errorf("via = %q, want belongs_to", j.Properties["via"])
		}
		if j.Properties["association"] != "author" {
			t.Errorf("association = %q, want author", j.Properties["association"])
		}
	}
	// has_many :reviews -> singularised Review.
	if j := mongoidFindJoin(rels, "Book", "Review"); j == nil {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Review) for has_many :reviews; rels=%+v", rels)
	}
	// embeds_many :pages -> singularised Page.
	if j := mongoidFindJoin(rels, "Book", "Page"); j == nil {
		t.Fatalf("expected JOINS_COLLECTION(Class:Book -> Class:Page) for embeds_many :pages; rels=%+v", rels)
	}
}

// Explicit class_name: overrides the symbol-derived target; snake symbols
// camelise.
func TestMongoidAssociations_ClassNameAndCamelise(t *testing.T) {
	src := `
class Order
  include Mongoid::Document
  belongs_to :buyer, class_name: 'Customer'
  has_many :line_items
end
`
	_, rels := runMongoidAgg(t, src)

	if j := mongoidFindJoin(rels, "Order", "Customer"); j == nil {
		t.Fatalf("expected class_name override JOINS_COLLECTION(Class:Order -> Class:Customer); rels=%+v", rels)
	}
	// has_many :line_items -> singularise + camelise -> LineItem.
	if j := mongoidFindJoin(rels, "Order", "LineItem"); j == nil {
		t.Fatalf("expected JOINS_COLLECTION(Class:Order -> Class:LineItem); rels=%+v", rels)
	}
}

// Negative: a dynamic `from` (variable, not a quoted literal) yields NO join edge
// (the stage entity may exist but carries no from / no JOINS_COLLECTION).
func TestMongoidAgg_DynamicFrom_NoJoin(t *testing.T) {
	src := `
class Book
  include Mongoid::Document
end

from_coll = 'authors'
Book.collection.aggregate([
  { '$lookup' => { 'from' => from_coll, 'as' => 'author' } }
])
`
	_, rels := runMongoidAgg(t, src)
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindJoinsCollection) {
			t.Fatalf("expected NO join edge for dynamic from; got %+v", r)
		}
	}
}

// Negative: a non-Mongoid Ruby class (no include Mongoid::Document) is gated out
// — its belongs_to macro must NOT emit a relation edge. The file mentions
// Mongoid only incidentally so the pass gate opens but the class span filter
// excludes it.
func TestMongoidAssociations_NonMongoidClass_Gated(t *testing.T) {
	src := `
# references Mongoid in a comment so the file gate opens
class LegacyUser < ActiveRecord::Base
  belongs_to :account
end
`
	_, rels := runMongoidAgg(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no edges from a non-Mongoid::Document class; got %+v", rels)
	}
}

// Negative: a variable-bound pipeline (not an inline array literal) is left
// unresolved — no stage entities, no join edges.
func TestMongoidAgg_VariableBoundPipeline_Skipped(t *testing.T) {
	src := `
class Book
  include Mongoid::Document
end

pipeline = [{ '$lookup' => { 'from' => 'authors', 'as' => 'author' } }]
Book.collection.aggregate(pipeline)
`
	ents, rels := runMongoidAgg(t, src)
	for _, r := range rels {
		if r.Properties["pattern_type"] == mongoidAggPatternType {
			t.Fatalf("expected no agg join edge for variable-bound pipeline; got %+v", r)
		}
	}
	for _, e := range ents {
		if e.Properties["pattern_type"] == mongoidAggPatternType {
			t.Fatalf("expected no agg stage entity for variable-bound pipeline; got %+v", e)
		}
	}
}

// $graphLookup is recognised and emits a join + stage.
func TestMongoidAgg_GraphLookup(t *testing.T) {
	src := `
class Category
  include Mongoid::Document
end

Category.collection.aggregate([
  { '$graphLookup' => { 'from' => 'categories', 'startWith' => '$parent_id', 'as' => 'ancestors' } }
])
`
	ents, rels := runMongoidAgg(t, src)
	// categories -> capitalisedSingular -> Category (self-referential graph lookup).
	if j := mongoidFindJoin(rels, "Category", "Category"); j == nil {
		t.Fatalf("expected JOINS_COLLECTION(Class:Category -> Class:Category) for graphLookup; rels=%+v", rels)
	}
	var found bool
	for _, e := range ents {
		if e.Subtype == "$graphLookup" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a $graphLookup stage entity; ents=%+v", ents)
	}
}
