// MongoDB.Driver (C#) aggregation-pipeline internal extraction (#3848,
// epic #3837).
//
// This is the C# sibling of orm_queries_python_mongo_agg.go (#3440),
// orm_queries_go_mongo_agg.go (#3846), orm_queries_java_mongo_agg.go (#3845),
// and the Mongoose pass (#3844). The existing C# MongoDB.Driver scanner
// (scanCSharpDrivers in orm_queries_drivers_other.go) recognises the
// `db.GetCollection<T>("users")` collection target and emits a coarse QUERIES
// edge, but it does NOT understand an aggregation pipeline, and a `$lookup`
// stage there is an implicit cross-collection JOIN the migration must reason
// about (Mongo has no schema FK for it). This pass recovers, in the SAME
// emission shape as the Python/Go/Java/Mongoose siblings:
//
//  1. JOINS_COLLECTION relationship — for every `$lookup` stage, an edge from
//     the aggregating collection → the looked-up `from` collection. FromID /
//     ToID are `Class:<capitalisedSingular(collection)>`, identical to the
//     contract the other passes use, so a C# `$lookup` from books to authors
//     lands on the SAME `Class:Author` node a Mongoose `ref:` or a pymongo
//     `$lookup` would. Properties: local_field, foreign_field, as,
//     stage="lookup".
//
//  2. SCOPE.DataAccess pipeline-stage entities — one per `$lookup`, anchored at
//     the lookup site, subtype `$lookup`, stage_index preserved.
//
// Two MongoDB.Driver idioms are resolved:
//
//   - FLUENT positional `Lookup` (the canonical fluent-aggregate form):
//
//     db.GetCollection<Book>("books").Aggregate()
//     .Lookup("authors", "authorId", "_id", "author");
//
//     `IAggregateFluent.Lookup(from, localField, foreignField, as)` is a
//     positional 4-string overload — the join fields come from the positional
//     string-literal arguments (NOT JSON keys), exactly like Java's
//     `Aggregation.lookup(...)`. The aggregating collection is resolved from
//     the receiver of `.Aggregate()` (a same-file `GetCollection<T>("books")`
//     call — the quoted string wins; else the `<T>` generic type arg).
//
//   - BsonDocument `$lookup` pipeline stage:
//
//     new BsonDocument("$lookup", new BsonDocument {
//     { "from", "authors" }, { "localField", "authorId" },
//     { "foreignField", "_id" }, { "as", "author" } })
//
//     The C# collection-initialiser `{ "key", value }` tuple form mirrors Go's
//     bson.D; we recover the join fields from the `"key", "value"` pairs. Also
//     accepts the inline-document `new BsonDocument("$lookup", new BsonDocument
//     { ... })` shape and the `"$lookup"` key inside a larger pipeline stage.
//     The aggregating collection is resolved file-scoped from the nearest
//     `GetCollection<T>("books")` / `IMongoCollection<Book>` typing.
//
// HONEST LIMIT: a dynamic `from` (`.Lookup(collVar, ...)` / `{ "from", coll }`
// with no quoted value) yields NO edge — we never fabricate a join we cannot
// statically resolve. A dynamic / unresolvable aggregating collection leaves
// the stage unemitted rather than guessing. Cross-file pipeline assembly and
// the `Lookup<TForeign,...>(foreignCollectionExpr, ...)` strongly-typed overload
// whose foreign collection is an expression (not a literal) are out of scope.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoAggCSharpGateRe gates the scan to files that plausibly use the
// MongoDB.Driver aggregation surface, so we never scan arbitrary `.Aggregate(`
// / `.Lookup(` chains (e.g. a LINQ helper).
var mongoAggCSharpGateRe = regexp.MustCompile(
	`\b(?:MongoDB\.Driver|IMongoCollection|IMongoDatabase|IAggregateFluent|GetCollection|BsonDocument)\b`,
)

// mongoAggCSharpFluentLookupRe matches the fluent positional
// `.Lookup("authors", "authorId", "_id", "author")` overload. Groups:
// 1=from, 2=localField, 3=foreignField, 4=as. All four must be string
// literals; a dynamic arg in any position fails the match (honest skip — no
// edge). The optional `<...>` generic type arguments of the strongly-typed
// overload are tolerated before the paren.
var mongoAggCSharpFluentLookupRe = regexp.MustCompile(
	`\.\s*Lookup\s*(?:<[^>]*>\s*)?\(\s*"([A-Za-z_][\w$.-]*)"\s*,\s*"([A-Za-z_][\w$.-]*)"\s*,\s*"([A-Za-z_][\w$.-]*)"\s*,\s*"([A-Za-z_][\w$.-]*)"\s*\)`,
)

// mongoAggCSharpBsonLookupRe locates a `new BsonDocument("$lookup", ...)` stage
// (the BSON pipeline-stage form). The inner join document is parsed separately
// from the `{ "key", "value" }` collection-initialiser pairs. We also match a
// `"$lookup"` key appearing in a BsonDocument-initialiser stage (`{ "$lookup",
// new BsonDocument { ... } }`).
var mongoAggCSharpBsonLookupRe = regexp.MustCompile(`"\$lookup"`)

// mongoAggCSharpGetCollStrRe captures the aggregating collection from a
// `GetCollection<Book>("books")` call: the quoted argument wins. The generic
// type arg is optional.
var mongoAggCSharpGetCollStrRe = regexp.MustCompile(
	`\.\s*GetCollection\s*(?:<[^>]+>)?\s*\(\s*"([A-Za-z_][\w$.-]*)"`,
)

// mongoAggCSharpGetCollGenericRe captures the generic type argument of a
// `GetCollection<Book>(...)` call when the string argument is dynamic — the
// entity name is the collection token (resolved via capitalisedSingular like
// every other path). Group 1 is the bare type name (last segment of a possibly
// qualified `<Ns.Book>`).
var mongoAggCSharpGetCollGenericRe = regexp.MustCompile(
	`\.\s*GetCollection\s*<\s*(?:[\w.]*\.)?([A-Z][A-Za-z0-9_]*)\s*>`,
)

// mongoAggCSharpCollFieldTypeRe captures the element type of an
// `IMongoCollection<Book>` field/variable/property declaration — the typed
// collection's entity name, used when no GetCollection call is present in the
// file. Group 1 is the bare type name.
var mongoAggCSharpCollFieldTypeRe = regexp.MustCompile(
	`IMongoCollection\s*<\s*(?:[\w.]*\.)?([A-Z][A-Za-z0-9_]*)\s*>`,
)

// scanCSharpMongoAggregation walks `src`, finds MongoDB.Driver aggregation
// `$lookup` joins (fluent positional `.Lookup(...)` and `new BsonDocument(
// "$lookup", ...)` pipeline stages), resolves the aggregating collection, and
// emits SCOPE.DataAccess stage entities + JOINS_COLLECTION edges via the
// supplied appenders — matching the Python/Go/Java/Mongoose contract exactly.
func scanCSharpMongoAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	if !mongoAggCSharpGateRe.MatchString(src) {
		return
	}

	// Resolve the aggregating collection ONCE per file: a
	// `GetCollection<T>("books")` quoted string wins, else the `GetCollection<T>`
	// generic entity name, else an `IMongoCollection<Book>` field typing. This is
	// file-scoped — the canonical repository/service shape has one aggregating
	// collection per file.
	coll := mongoAggCSharpResolveCollection(src)
	if coll == "" {
		return // dynamic / unresolvable collection — honest skip.
	}

	stageIdx := 0
	emitLookup := func(off int, lk mongoAggLookup) {
		if lk.from == "" {
			return // dynamic from — honest skip.
		}
		emitJoin(mongoAggJoinEdge(coll, lk, "lookup"))
		// #4244 — node-anchored twin so the join is reachable from the
		// $lookup DataAccess node. entityName MUST match the emitStage Name.
		// The `@L<line>` segment keeps the per-call-site stage node unique
		// (see mongoAggStageName).
		entityName := mongoAggStageName(coll, lineOfOffset(src, off), stageIdx, "$lookup")
		emitJoin(mongoAggStageJoinEdge(entityName, path, lang, lk, "lookup"))

		props := map[string]string{
			"pattern_type": mongoAggPatternType,
			"collection":   coll,
			"stage_index":  itoa(stageIdx),
			"stage":        "$lookup",
			"from":         lk.from,
		}
		if lk.localField != "" {
			props["local_field"] = lk.localField
		}
		if lk.foreignField != "" {
			props["foreign_field"] = lk.foreignField
		}
		if lk.as != "" {
			props["as"] = lk.as
		}
		if caller := enclosingFuncAt(funcs, off); caller != "" {
			props["caller"] = caller
		}
		emitStage(types.EntityRecord{
			Name:               entityName,
			Kind:               mongoAggStageEntityKind,
			Subtype:            "$lookup",
			SourceFile:         path,
			StartLine:          lineOfOffset(src, off),
			EndLine:            lineOfOffset(src, off),
			Language:           lang,
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
		stageIdx++
	}

	// Idiom 1: fluent positional `.Lookup(from, localField, foreignField, as)`.
	for _, m := range mongoAggCSharpFluentLookupRe.FindAllStringSubmatchIndex(src, -1) {
		emitLookup(m[0], mongoAggLookup{
			from:         src[m[2]:m[3]],
			localField:   src[m[4]:m[5]],
			foreignField: src[m[6]:m[7]],
			as:           src[m[8]:m[9]],
		})
	}

	// Idiom 2: `new BsonDocument("$lookup", new BsonDocument { ... })` stage —
	// the join fields come from the `{ "key", "value" }` collection-initialiser
	// pairs. Parse the BSON document body that follows the `"$lookup"` key.
	for _, loc := range mongoAggCSharpBsonLookupRe.FindAllStringIndex(src, -1) {
		lk := mongoAggCSharpParseBsonLookup(src, loc[1])
		emitLookup(loc[0], lk)
	}
}

// mongoAggCSharpResolveCollection recovers the aggregating collection token for
// the file: a `GetCollection<T>("books")` quoted argument wins (matching the
// other passes' string-literal-first rule), else the `GetCollection<T>` generic
// entity name, else an `IMongoCollection<Book>` field/variable typing. Returns
// "" when none is statically present — honest skip.
func mongoAggCSharpResolveCollection(src string) string {
	if m := mongoAggCSharpGetCollStrRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	if m := mongoAggCSharpGetCollGenericRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	if m := mongoAggCSharpCollFieldTypeRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	return ""
}

// mongoAggCSharpParseBsonLookup recovers the join fields from a BsonDocument
// `$lookup` stage, scanning forward from `from` (just past the `"$lookup"`
// key) over the inner BSON document. The fields are `{ "from", "authors" }`
// collection-initialiser pairs (the C# analogue of Go's bson.D tuple form);
// we also accept the `"from": "authors"` map form for robustness. A field
// bound to a variable (no quoted literal) is left empty — honest: a missing
// `from` then suppresses the edge in emitLookup.
func mongoAggCSharpParseBsonLookup(src string, from int) mongoAggLookup {
	// Bound the scan to the `$lookup` value document. Find the FIRST `{` after
	// the key (opening the inner `new BsonDocument { ... }` body) and take its
	// balanced body so we don't bleed into a sibling stage's fields.
	brace := strings.IndexByte(src[from:], '{')
	if brace < 0 {
		return mongoAggLookup{}
	}
	open := from + brace
	body := mongoAggCSharpBraceBody(src, open)
	if body == "" {
		// Fallback: bounded window when the brace is unbalanced/truncated.
		end := from + 400
		if end > len(src) {
			end = len(src)
		}
		body = src[from:end]
	}
	return mongoAggLookup{
		from:         mongoAggCSharpBsonField(body, "from"),
		localField:   mongoAggCSharpBsonField(body, "localField"),
		foreignField: mongoAggCSharpBsonField(body, "foreignField"),
		as:           mongoAggCSharpBsonField(body, "as"),
	}
}

// mongoAggCSharpBraceBody returns the balanced `{ ... }` literal (braces
// included) whose opening `{` is at `open`, string-aware. Returns "" if the
// brace is unbalanced.
func mongoAggCSharpBraceBody(src string, open int) string {
	if open >= len(src) || src[open] != '{' {
		return ""
	}
	depth := 0
	inStr := byte(0)
	for i := open; i < len(src); i++ {
		c := src[i]
		if inStr != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	return ""
}

// mongoAggCSharpBsonFieldReCache caches per-key field matchers.
var mongoAggCSharpBsonFieldReCache = map[string]*regexp.Regexp{}

// mongoAggCSharpBsonField pulls the plain string-literal value bound to `key`
// in a BsonDocument body, in either C# form:
//
//	{ "from", "authors" }   — collection-initialiser tuple (comma separator).
//	{ "from": "authors" }   — element-init map form (colon separator).
//
// Returns "" when the key is absent or its value is not a plain `"..."`
// literal (e.g. a variable / expression) — honest: a dynamic `from` yields no
// join edge.
func mongoAggCSharpBsonField(body, key string) string {
	re, ok := mongoAggCSharpBsonFieldReCache[key]
	if !ok {
		re = regexp.MustCompile(
			`"` + regexp.QuoteMeta(key) + `"\s*[,:]\s*"([a-zA-Z_][\w$.-]*)"`,
		)
		mongoAggCSharpBsonFieldReCache[key] = re
	}
	if m := re.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}
