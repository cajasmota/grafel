package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runMongoAgg drives scanJSMongoAggregation over `src` and collects the
// emitted stage entities + join edges.
func runMongoAgg(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("javascript", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanJSMongoAggregation(src, funcs, "svc/agg.js", "javascript",
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)
	return ents, rels
}

// stageSubtypesInOrder returns the Subtype of each stage entity in emission
// order — used to assert the pipeline order is preserved.
func stageSubtypesInOrder(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		out = append(out, e.Subtype)
	}
	return out
}

func findStage(ents []types.EntityRecord, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func findJoinTo(rels []types.RelationshipRecord, toClass string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == string(types.RelationshipKindJoinsCollection) &&
			rels[i].ToID == "Class:"+toClass {
			return &rels[i]
		}
	}
	return nil
}

// Mongoose model, multi-line pipeline with $match, $lookup (classic form),
// $unwind, $group, $sort, $limit. Asserts the $lookup `from`, the stage
// subtypes + order, the $group._id and accumulators.
func TestMongoAgg_Mongoose_LookupGroupOrder(t *testing.T) {
	src := `
const mongoose = require('mongoose');
async function report() {
  return Order.aggregate([
    { $match: { status: 'paid' } },
    { $lookup: {
        from: 'customers',
        localField: 'customerId',
        foreignField: '_id',
        as: 'customer'
    } },
    { $unwind: '$customer' },
    { $group: {
        _id: '$customer.region',
        total: { $sum: '$amount' },
        count: { $sum: 1 }
    } },
    { $sort: { total: -1 } },
    { $limit: 10 },
  ]);
}
`
	ents, rels := runMongoAgg(t, src)

	gotOrder := stageSubtypesInOrder(ents)
	wantOrder := []string{"$match", "$lookup", "$unwind", "$group", "$sort", "$limit"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("stage order = %v, want %v", gotOrder, wantOrder)
	}

	// stage_index must match position.
	for i, e := range ents {
		if e.Properties["stage_index"] != itoa(i) {
			t.Errorf("stage %d (%s) stage_index = %q, want %d",
				i, e.Subtype, e.Properties["stage_index"], i)
		}
		if e.Properties["collection"] != "Order" {
			t.Errorf("stage %d collection = %q, want Order", i, e.Properties["collection"])
		}
	}

	// $lookup stage carries the from/local/foreign/as props.
	lk := findStage(ents, "$lookup")
	if lk == nil {
		t.Fatal("no $lookup stage entity emitted")
	}
	if lk.Properties["from"] != "customers" {
		t.Errorf("$lookup from = %q, want customers", lk.Properties["from"])
	}
	if lk.Properties["local_field"] != "customerId" {
		t.Errorf("$lookup local_field = %q, want customerId", lk.Properties["local_field"])
	}
	if lk.Properties["foreign_field"] != "_id" {
		t.Errorf("$lookup foreign_field = %q, want _id", lk.Properties["foreign_field"])
	}
	if lk.Properties["as"] != "customer" {
		t.Errorf("$lookup as = %q, want customer", lk.Properties["as"])
	}

	// $group._id + accumulator names.
	grp := findStage(ents, "$group")
	if grp == nil {
		t.Fatal("no $group stage entity emitted")
	}
	if grp.Properties["group_id"] != "'$customer.region'" {
		t.Errorf("$group group_id = %q, want '$customer.region'", grp.Properties["group_id"])
	}
	if grp.Properties["accumulators"] != "total,count" {
		t.Errorf("$group accumulators = %q, want total,count", grp.Properties["accumulators"])
	}

	// JOIN edge: Order -> Customer (singularised), with field props.
	join := findJoinTo(rels, "Customer")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Customer; rels=%+v", rels)
	}
	if join.FromID != "Class:Order" {
		t.Errorf("join FromID = %q, want Class:Order", join.FromID)
	}
	if join.Properties["stage"] != "lookup" {
		t.Errorf("join stage = %q, want lookup", join.Properties["stage"])
	}
	if join.Properties["local_field"] != "customerId" ||
		join.Properties["foreign_field"] != "_id" ||
		join.Properties["as"] != "customer" {
		t.Errorf("join field props wrong: %+v", join.Properties)
	}
}

// Raw native driver, db.collection('orders').aggregate, with a $lookup that
// uses the sub-pipeline form (from + pipeline + as, no local/foreignField)
// and a $facet stage. Asserts the join `from`, the facet sub-pipeline names,
// and that the receiver collection resolves from .collection('orders').
func TestMongoAgg_RawDriver_LookupPipelineAndFacet(t *testing.T) {
	src := `
const { MongoClient } = require('mongodb');
async function run(db) {
  return db.collection('orders').aggregate([
    { $match: { active: true } },
    { $lookup: {
        from: 'products',
        as: 'items',
        pipeline: [
          { $match: { inStock: true } },
          { $project: { name: 1, price: 1 } }
        ]
    } },
    { $facet: {
        byStatus: [ { $group: { _id: '$status', n: { $sum: 1 } } } ],
        byMonth: [ { $group: { _id: '$month', n: { $sum: 1 } } } ]
    } },
  ]);
}
`
	ents, rels := runMongoAgg(t, src)

	gotOrder := stageSubtypesInOrder(ents)
	wantOrder := []string{"$match", "$lookup", "$facet"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("stage order = %v, want %v (nested pipeline stages must NOT leak as top-level)", gotOrder, wantOrder)
	}

	for _, e := range ents {
		if e.Properties["collection"] != "orders" {
			t.Errorf("stage %s collection = %q, want orders", e.Subtype, e.Properties["collection"])
		}
	}

	lk := findStage(ents, "$lookup")
	if lk == nil {
		t.Fatal("no $lookup stage entity emitted")
	}
	if lk.Properties["from"] != "products" {
		t.Errorf("$lookup from = %q, want products", lk.Properties["from"])
	}
	if lk.Properties["as"] != "items" {
		t.Errorf("$lookup as = %q, want items", lk.Properties["as"])
	}

	// $facet sub-pipeline names.
	fc := findStage(ents, "$facet")
	if fc == nil {
		t.Fatal("no $facet stage entity emitted")
	}
	if fc.Properties["facets"] != "byStatus,byMonth" {
		t.Errorf("$facet facets = %q, want byStatus,byMonth", fc.Properties["facets"])
	}

	// JOIN edge orders -> Product (singularised). 'orders' aggregating coll
	// singularises to Order on the FromID side.
	join := findJoinTo(rels, "Product")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Product; rels=%+v", rels)
	}
	if join.FromID != "Class:Order" {
		t.Errorf("join FromID = %q, want Class:Order", join.FromID)
	}
	if join.Properties["as"] != "items" {
		t.Errorf("join as = %q, want items", join.Properties["as"])
	}
}

// $graphLookup also emits a cross-collection join edge.
func TestMongoAgg_GraphLookup_EmitsJoin(t *testing.T) {
	src := `
const mongoose = require('mongoose');
function tree() {
  return Employee.aggregate([
    { $match: { active: true } },
    { $graphLookup: {
        from: 'employees',
        startWith: '$reportsTo',
        connectFromField: 'reportsTo',
        connectToField: '_id',
        as: 'hierarchy'
    } },
  ]);
}
`
	ents, rels := runMongoAgg(t, src)

	gl := findStage(ents, "$graphLookup")
	if gl == nil {
		t.Fatal("no $graphLookup stage entity emitted")
	}
	if gl.Properties["from"] != "employees" {
		t.Errorf("$graphLookup from = %q, want employees", gl.Properties["from"])
	}

	join := findJoinTo(rels, "Employee")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Employee; rels=%+v", rels)
	}
	if join.Properties["stage"] != "graphLookup" {
		t.Errorf("join stage = %q, want graphLookup", join.Properties["stage"])
	}
	if join.Properties["as"] != "hierarchy" {
		t.Errorf("join as = %q, want hierarchy", join.Properties["as"])
	}
}

// HONEST LIMIT: a dynamically-built pipeline (variable, not inline literal)
// must NOT produce fabricated stages or joins.
func TestMongoAgg_DynamicPipeline_Unresolved(t *testing.T) {
	src := `
const mongoose = require('mongoose');
function dyn(stages) {
  const pipeline = stages;
  return Order.aggregate(pipeline);
}
`
	ents, rels := runMongoAgg(t, src)
	if len(ents) != 0 {
		t.Errorf("dynamic pipeline produced %d stage entities, want 0: %+v", len(ents), ents)
	}
	if len(rels) != 0 {
		t.Errorf("dynamic pipeline produced %d join edges, want 0: %+v", len(rels), rels)
	}
}

// ---------------------------------------------------------------------------
// #3440 ask 2-JS — VARIABLE-bound pipeline resolution.
// ---------------------------------------------------------------------------

// A pipeline bound to a `const` in the same function scope, then passed by
// identifier to `.aggregate(pipeline)`, resolves exactly as if inline: the
// $lookup join + stage order are recovered.
func TestMongoAgg_VariableBound_ResolvesInlinePipeline(t *testing.T) {
	src := `
const mongoose = require('mongoose');
async function report() {
  const pipeline = [
    { $match: { status: 'paid' } },
    { $lookup: {
        from: 'orders',
        localField: 'orderId',
        foreignField: '_id',
        as: 'order'
    } },
    { $group: { _id: '$order.region', total: { $sum: '$amount' } } },
  ];
  return Customer.aggregate(pipeline);
}
`
	ents, rels := runMongoAgg(t, src)

	gotOrder := stageSubtypesInOrder(ents)
	wantOrder := []string{"$match", "$lookup", "$group"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("stage order = %v, want %v", gotOrder, wantOrder)
	}

	lk := findStage(ents, "$lookup")
	if lk == nil {
		t.Fatal("no $lookup stage entity emitted from variable-bound pipeline")
	}
	if lk.Properties["from"] != "orders" {
		t.Errorf("$lookup from = %q, want orders", lk.Properties["from"])
	}
	if lk.Properties["local_field"] != "orderId" {
		t.Errorf("$lookup local_field = %q, want orderId", lk.Properties["local_field"])
	}

	// JOIN edge Customer -> Order (singularised), local/foreign fields carried.
	join := findJoinTo(rels, "Order")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Order; rels=%+v", rels)
	}
	if join.FromID != "Class:Customer" {
		t.Errorf("join FromID = %q, want Class:Customer", join.FromID)
	}
	if join.Properties["local_field"] != "orderId" ||
		join.Properties["foreign_field"] != "_id" ||
		join.Properties["as"] != "order" {
		t.Errorf("join field props wrong: %+v", join.Properties)
	}
}

// `let` and `var` bindings resolve too (not only `const`).
func TestMongoAgg_VariableBound_LetAndVar(t *testing.T) {
	src := `
const mongoose = require('mongoose');
function a() {
  let p = [ { $lookup: { from: 'users', as: 'u' } } ];
  return Post.aggregate(p);
}
function b() {
  var q = [ { $lookup: { from: 'tags', as: 't' } } ];
  return Post.aggregate(q);
}
`
	_, rels := runMongoAgg(t, src)
	if findJoinTo(rels, "User") == nil {
		t.Errorf("let-bound pipeline: no join to Class:User; rels=%+v", rels)
	}
	if findJoinTo(rels, "Tag") == nil {
		t.Errorf("var-bound pipeline: no join to Class:Tag; rels=%+v", rels)
	}
}

// HONEST LIMIT (ask 2-JS negative): a variable bound to a NON-literal (another
// variable, a function call) must NOT fabricate any stage or join.
func TestMongoAgg_VariableBound_NonLiteral_Unresolved(t *testing.T) {
	src := `
const mongoose = require('mongoose');
function dyn(stages) {
  const pipeline = stages;        // not an array literal
  return Order.aggregate(pipeline);
}
function dyn2() {
  const pipeline = buildPipeline(); // call expression, not a literal
  return Order.aggregate(pipeline);
}
`
	ents, rels := runMongoAgg(t, src)
	if len(ents) != 0 {
		t.Errorf("non-literal binding produced %d stages, want 0: %+v", len(ents), ents)
	}
	if len(rels) != 0 {
		t.Errorf("non-literal binding produced %d joins, want 0: %+v", len(rels), rels)
	}
}

// ---------------------------------------------------------------------------
// #3440 ask 3 — BUILDER `.build()` pipeline resolution.
// ---------------------------------------------------------------------------

// Inline one-liner builder chain passed straight to `.aggregate(...)`.
func TestMongoAgg_Builder_InlineChain(t *testing.T) {
	src := `
import { AggregationBuilder } from './agg';
class ReportRepo {
  async run() {
    return this.repo.aggregate(
      new AggregationBuilder()
        .match({ status: 'open' })
        .lookup({ from: 'users', localField: 'uid', foreignField: '_id', as: 'user' })
        .group({ _id: '$user.team', total: { $sum: '$amount' }, n: { $sum: 1 } })
        .sort({ total: -1 })
        .build()
    );
  }
}
`
	ents, rels := runMongoAgg(t, src)

	gotOrder := stageSubtypesInOrder(ents)
	wantOrder := []string{"$match", "$lookup", "$group", "$sort"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("builder stage order = %v, want %v", gotOrder, wantOrder)
	}

	lk := findStage(ents, "$lookup")
	if lk == nil {
		t.Fatal("no $lookup stage entity emitted from builder chain")
	}
	if lk.Properties["from"] != "users" {
		t.Errorf("builder $lookup from = %q, want users", lk.Properties["from"])
	}
	if lk.Properties["local_field"] != "uid" {
		t.Errorf("builder $lookup local_field = %q, want uid", lk.Properties["local_field"])
	}
	if lk.Properties["as"] != "user" {
		t.Errorf("builder $lookup as = %q, want user", lk.Properties["as"])
	}

	grp := findStage(ents, "$group")
	if grp == nil {
		t.Fatal("no $group stage entity emitted from builder chain")
	}
	if grp.Properties["accumulators"] != "total,n" {
		t.Errorf("builder $group accumulators = %q, want total,n", grp.Properties["accumulators"])
	}

	join := findJoinTo(rels, "User")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:User from builder; rels=%+v", rels)
	}
	if join.Properties["local_field"] != "uid" || join.Properties["as"] != "user" {
		t.Errorf("builder join field props wrong: %+v", join.Properties)
	}
}

// Builder constructed into a variable, then `builder.build()` passed to
// aggregate. The construction chain is resolved within the function scope.
func TestMongoAgg_Builder_VariableBound(t *testing.T) {
	src := `
import { AggBuilder } from './agg';
async function membersByOrg(repo) {
  const builder = new AggBuilder()
    .match({ active: true })
    .graphLookup({ from: 'orgs', startWith: '$orgId', connectFromField: 'orgId', connectToField: '_id', as: 'orgTree' });
  return repo.aggregate(builder.build());
}
`
	ents, rels := runMongoAgg(t, src)

	gotOrder := stageSubtypesInOrder(ents)
	wantOrder := []string{"$match", "$graphLookup"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("builder-var stage order = %v, want %v", gotOrder, wantOrder)
	}

	gl := findStage(ents, "$graphLookup")
	if gl == nil {
		t.Fatal("no $graphLookup stage from variable-bound builder")
	}
	if gl.Properties["from"] != "orgs" {
		t.Errorf("$graphLookup from = %q, want orgs", gl.Properties["from"])
	}

	join := findJoinTo(rels, "Org")
	if join == nil {
		t.Fatalf("no JOINS_COLLECTION edge to Class:Org; rels=%+v", rels)
	}
	if join.Properties["stage"] != "graphLookup" {
		t.Errorf("join stage = %q, want graphLookup", join.Properties["stage"])
	}
	if join.Properties["as"] != "orgTree" {
		t.Errorf("join as = %q, want orgTree", join.Properties["as"])
	}
}

// A `.lookup(...)` token appearing INSIDE an argument object must NOT be
// mistaken for a top-level chained builder stage (depth-awareness guard).
func TestMongoAgg_Builder_NestedMethodTokenNotSplit(t *testing.T) {
	src := `
import { AggregationBuilder } from './agg';
function f(repo) {
  return repo.aggregate(
    new AggregationBuilder()
      .match({ note: 'see .lookup({from:fake}) in docs', expr: { $eq: ['$a', '$b'] } })
      .lookup({ from: 'real', as: 'r' })
      .build()
  );
}
`
	ents, rels := runMongoAgg(t, src)

	gotOrder := stageSubtypesInOrder(ents)
	wantOrder := []string{"$match", "$lookup"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("builder stage order = %v, want %v (nested token leaked)", gotOrder, wantOrder)
	}
	// Only the real lookup join must exist.
	if findJoinTo(rels, "Real") == nil {
		t.Errorf("no join to Class:Real; rels=%+v", rels)
	}
	if findJoinTo(rels, "Fake") != nil {
		t.Errorf("fabricated join to Class:Fake from a string-embedded token; rels=%+v", rels)
	}
}

// HONEST LIMIT (ask 3 negative): a builder constructed in ANOTHER function and
// only `.build()`-invoked here cannot be resolved → no fabricated joins.
func TestMongoAgg_Builder_BuiltAcrossFunctions_Unresolved(t *testing.T) {
	src := `
import { AggregationBuilder } from './agg';
function makeBuilder() {
  return new AggregationBuilder()
    .match({ active: true })
    .lookup({ from: 'secrets', as: 's' });
}
async function run(repo) {
  const builder = makeBuilder();   // construction chain is in another function
  return repo.aggregate(builder.build());
}
`
	ents, rels := runMongoAgg(t, src)
	// The makeBuilder() function itself has no .aggregate(), so its chain must
	// not be attributed to run()'s aggregate. run()'s builder resolves to a
	// call expression (makeBuilder()), which is not a construction chain.
	for _, j := range rels {
		if j.ToID == "Class:Secret" {
			t.Errorf("fabricated cross-function builder join to Class:Secret; rels=%+v", rels)
		}
	}
	// And run()'s aggregate must produce no stages (builder unresolved).
	for _, e := range ents {
		if e.Properties["caller"] == "run" {
			t.Errorf("run() builder unresolved but emitted stage: %+v", e)
		}
	}
}

// Guard: nested commas/objects/strings inside a stage must not split it.
func TestMongoAgg_NestedAndStringCommasDoNotSplit(t *testing.T) {
	src := `
const mongoose = require('mongoose');
function f() {
  return Sale.aggregate([
    { $match: { $or: [ { a: 1 }, { b: 2 } ], note: 'a, b, c' } },
    { $project: { label: { $concat: ['x', ',', 'y'] } } },
  ]);
}
`
	ents, _ := runMongoAgg(t, src)
	got := stageSubtypesInOrder(ents)
	want := []string{"$match", "$project"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stage order = %v, want %v (nested/string commas leaked)", got, want)
	}
}
