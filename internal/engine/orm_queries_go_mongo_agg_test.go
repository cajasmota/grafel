package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runGoMongoAgg drives scanGoMongoAggregation over `src` and collects the
// emitted stage entities + join edges.
func runGoMongoAgg(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("go", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanGoMongoAggregation(src, funcs, "svc/agg.go", "go",
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)
	return ents, rels
}

// goImport is the mongo-driver import block the gate requires.
const goImport = `import (
	"context"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/bson"
)
`

// Classic bson.D tuple-form $lookup, inline mongo.Pipeline literal, with the
// aggregating collection resolved from the inline db.Collection("books") call.
// Asserts the JOINS_COLLECTION edge Class:Book -> Class:Author plus the join
// sub-fields, and the stage entity props.
func TestGoMongoAgg_BsonD_InlineCollection_LookupEdge(t *testing.T) {
	src := goImport + `
func report(ctx context.Context, db *mongo.Database) {
	db.Collection("books").Aggregate(ctx, mongo.Pipeline{
		bson.D{{"$match", bson.D{{"published", true}}}},
		bson.D{{"$lookup", bson.D{
			{"from", "authors"},
			{"localField", "author_id"},
			{"foreignField", "_id"},
			{"as", "author"},
		}}},
		bson.D{{"$unwind", "$author"}},
	})
}
`
	ents, rels := runGoMongoAgg(t, src)

	// JOINS_COLLECTION edge: Class:Book -> Class:Author (capitalisedSingular of
	// "books" -> "Book", "authors" -> "Author").
	join := findJoinTo(rels, "Author")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Author; rels=%+v", rels)
	}
	if join.FromID != "Class:Book" {
		t.Errorf("join FromID = %q, want Class:Book", join.FromID)
	}
	if join.ToID != "Class:Author" {
		t.Errorf("join ToID = %q, want Class:Author", join.ToID)
	}
	if join.Properties["local_field"] != "author_id" {
		t.Errorf("join local_field = %q, want author_id", join.Properties["local_field"])
	}
	if join.Properties["foreign_field"] != "_id" {
		t.Errorf("join foreign_field = %q, want _id", join.Properties["foreign_field"])
	}
	if join.Properties["as"] != "author" {
		t.Errorf("join as = %q, want author", join.Properties["as"])
	}
	if join.Properties["stage"] != "lookup" {
		t.Errorf("join stage = %q, want lookup", join.Properties["stage"])
	}

	// Stage order preserved.
	gotOrder := stageSubtypesInOrder(ents)
	wantOrder := []string{"$match", "$lookup", "$unwind"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("stage order = %v, want %v", gotOrder, wantOrder)
	}
	for i, e := range ents {
		if e.Properties["stage_index"] != itoa(i) {
			t.Errorf("stage %d (%s) stage_index = %q, want %d", i, e.Subtype, e.Properties["stage_index"], i)
		}
		if e.Properties["collection"] != "books" {
			t.Errorf("stage %d collection = %q, want books", i, e.Properties["collection"])
		}
		if e.Properties["caller"] != "report" {
			t.Errorf("stage %d caller = %q, want report", i, e.Properties["caller"])
		}
		if e.Kind != string(types.EntityKindDataAccess) {
			t.Errorf("stage %d kind = %q, want %s", i, e.Kind, types.EntityKindDataAccess)
		}
	}

	lk := findStage(ents, "$lookup")
	if lk == nil {
		t.Fatal("no $lookup stage entity")
	}
	if lk.Properties["from"] != "authors" {
		t.Errorf("$lookup stage from = %q, want authors", lk.Properties["from"])
	}
}

// bson.M map-form $lookup with a collection variable bound one statement up to
// db.Collection("orders"). Asserts the receiver follow + the join edge
// Class:Order -> Class:Customer.
func TestGoMongoAgg_BsonM_CollVarBinding_LookupEdge(t *testing.T) {
	src := goImport + `
func orders(ctx context.Context, db *mongo.Database) {
	coll := db.Collection("orders")
	pipeline := mongo.Pipeline{
		bson.D{{"$match", bson.M{"status": "paid"}}},
		bson.D{{"$lookup", bson.M{
			"from":         "customers",
			"localField":   "customer_id",
			"foreignField": "_id",
			"as":           "customer",
		}}},
	}
	coll.Aggregate(ctx, pipeline)
}
`
	_, rels := runGoMongoAgg(t, src)

	join := findJoinTo(rels, "Customer")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Customer; rels=%+v", rels)
	}
	if join.FromID != "Class:Order" {
		t.Errorf("join FromID = %q, want Class:Order (coll var -> db.Collection(\"orders\"))", join.FromID)
	}
	if join.Properties["local_field"] != "customer_id" {
		t.Errorf("join local_field = %q, want customer_id", join.Properties["local_field"])
	}
	if join.Properties["foreign_field"] != "_id" {
		t.Errorf("join foreign_field = %q, want _id", join.Properties["foreign_field"])
	}
}

// $graphLookup (bson.M) emits a join edge with from + as but not local/foreign.
func TestGoMongoAgg_GraphLookup_Edge(t *testing.T) {
	src := goImport + `
func tree(ctx context.Context, db *mongo.Database) {
	db.Collection("employees").Aggregate(ctx, mongo.Pipeline{
		bson.D{{"$graphLookup", bson.M{
			"from":             "employees",
			"startWith":        "$reportsTo",
			"connectFromField": "reportsTo",
			"connectToField":   "_id",
			"as":               "reportingHierarchy",
		}}},
	})
}
`
	ents, rels := runGoMongoAgg(t, src)

	join := findJoinTo(rels, "Employee")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Employee; rels=%+v", rels)
	}
	if join.FromID != "Class:Employee" {
		t.Errorf("join FromID = %q, want Class:Employee", join.FromID)
	}
	if join.Properties["stage"] != "graphLookup" {
		t.Errorf("join stage = %q, want graphLookup", join.Properties["stage"])
	}
	if join.Properties["as"] != "reportingHierarchy" {
		t.Errorf("join as = %q, want reportingHierarchy", join.Properties["as"])
	}
	st := findStage(ents, "$graphLookup")
	if st == nil || st.Properties["from"] != "employees" {
		t.Fatalf("$graphLookup stage missing or wrong from: %+v", st)
	}
}

// $group: _id + accumulators captured (bson.M form).
func TestGoMongoAgg_Group_IdAndAccumulators(t *testing.T) {
	src := goImport + `
func stats(ctx context.Context, db *mongo.Database) {
	db.Collection("sales").Aggregate(ctx, mongo.Pipeline{
		bson.D{{"$group", bson.M{
			"_id":   "$region",
			"total": bson.M{"$sum": "$amount"},
			"count": bson.M{"$sum": 1},
		}}},
	})
}
`
	ents, _ := runGoMongoAgg(t, src)
	grp := findStage(ents, "$group")
	if grp == nil {
		t.Fatal("no $group stage entity")
	}
	if grp.Properties["group_id"] != "\"$region\"" && grp.Properties["group_id"] != "$region" {
		// value text is captured raw; accept either the quoted literal form.
		t.Errorf("$group group_id = %q, want $region", grp.Properties["group_id"])
	}
	accs := grp.Properties["accumulators"]
	if !strings.Contains(accs, "total") || !strings.Contains(accs, "count") {
		t.Errorf("$group accumulators = %q, want total,count", accs)
	}
}

// NEGATIVE: a dynamic `from` (variable, not a string literal) yields NO join
// edge — honest-partial. The stage entity is still emitted but carries no
// from/join.
func TestGoMongoAgg_DynamicFrom_NoJoin(t *testing.T) {
	src := goImport + `
func dyn(ctx context.Context, db *mongo.Database, fromColl string) {
	db.Collection("books").Aggregate(ctx, mongo.Pipeline{
		bson.D{{"$lookup", bson.M{
			"from":         fromColl,
			"localField":   "author_id",
			"foreignField": "_id",
			"as":           "author",
		}}},
	})
}
`
	ents, rels := runGoMongoAgg(t, src)
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindJoinsCollection) {
			t.Fatalf("expected NO join edge for dynamic from, got %+v", r)
		}
	}
	// The $lookup stage entity is still recorded (the call is real) but has no
	// `from` property.
	lk := findStage(ents, "$lookup")
	if lk == nil {
		t.Fatal("expected $lookup stage entity even with dynamic from")
	}
	if _, ok := lk.Properties["from"]; ok {
		t.Errorf("dynamic-from $lookup should carry no from prop, got %q", lk.Properties["from"])
	}
}

// NEGATIVE: a dynamic aggregating collection (db.Collection(name) with a
// variable arg, and no resolvable binding) yields NO stages/joins — honest skip.
func TestGoMongoAgg_DynamicCollection_Skipped(t *testing.T) {
	src := goImport + `
func dyn(ctx context.Context, db *mongo.Database, name string) {
	db.Collection(name).Aggregate(ctx, mongo.Pipeline{
		bson.D{{"$lookup", bson.M{"from": "authors", "as": "author"}}},
	})
}
`
	ents, rels := runGoMongoAgg(t, src)
	if len(ents) != 0 {
		t.Errorf("expected no stage entities for dynamic collection, got %+v", ents)
	}
	if len(rels) != 0 {
		t.Errorf("expected no join edges for dynamic collection, got %+v", rels)
	}
}

// NEGATIVE: no mongo-driver import => the gate skips the file entirely even if a
// `.Aggregate(` chain is present (e.g. a stats helper).
func TestGoMongoAgg_NoMongoImport_Gated(t *testing.T) {
	src := `package x
func f(s Stats) {
	s.Aggregate([]bson.D{{"$lookup": "x"}})
}
`
	ents, rels := runGoMongoAgg(t, src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected gate to skip non-mongo file, got ents=%+v rels=%+v", ents, rels)
	}
}

// NEGATIVE: a pipeline passed as an unresolvable bare variable (built by a
// builder call, no same-function slice-literal binding) yields no stages.
func TestGoMongoAgg_BuilderPipeline_Skipped(t *testing.T) {
	src := goImport + `
func build() mongo.Pipeline { return nil }
func run(ctx context.Context, db *mongo.Database) {
	pipeline := build()
	db.Collection("books").Aggregate(ctx, pipeline)
}
`
	ents, rels := runGoMongoAgg(t, src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected no output for builder-produced pipeline, got ents=%+v rels=%+v", ents, rels)
	}
}

// []bson.D{...} bare slice-literal pipeline form (no mongo.Pipeline alias) with
// classic tuple $lookup.
func TestGoMongoAgg_BsonDSliceLiteral_LookupEdge(t *testing.T) {
	src := goImport + `
func report(ctx context.Context, db *mongo.Database) {
	db.Collection("posts").Aggregate(ctx, []bson.D{
		{{"$lookup", bson.D{
			{"from", "users"},
			{"localField", "user_id"},
			{"foreignField", "_id"},
			{"as", "user"},
		}}},
	})
}
`
	_, rels := runGoMongoAgg(t, src)
	join := findJoinTo(rels, "User")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:User; rels=%+v", rels)
	}
	if join.FromID != "Class:Post" {
		t.Errorf("join FromID = %q, want Class:Post", join.FromID)
	}
	if join.Properties["local_field"] != "user_id" {
		t.Errorf("join local_field = %q, want user_id", join.Properties["local_field"])
	}
}

// Two $lookup stages in one pipeline -> two distinct join edges.
func TestGoMongoAgg_MultipleLookups(t *testing.T) {
	src := goImport + `
func report(ctx context.Context, db *mongo.Database) {
	db.Collection("orders").Aggregate(ctx, mongo.Pipeline{
		bson.D{{"$lookup", bson.D{{"from", "customers"}, {"as", "customer"}}}},
		bson.D{{"$lookup", bson.D{{"from", "products"}, {"as", "product"}}}},
	})
}
`
	_, rels := runGoMongoAgg(t, src)
	if findJoinTo(rels, "Customer") == nil {
		t.Errorf("missing join to Class:Customer; rels=%+v", rels)
	}
	if findJoinTo(rels, "Product") == nil {
		t.Errorf("missing join to Class:Product; rels=%+v", rels)
	}
	if len(rels) != 2 {
		t.Errorf("want 2 join edges, got %d: %+v", len(rels), rels)
	}
}
