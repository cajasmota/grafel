// Spring Data MongoDB (Java) aggregation-pipeline internal extraction (#3845,
// epic #3837).
//
// This is the Java sibling of orm_queries_python_mongo_agg.go (#3440) and
// orm_queries_jsts_mongo_agg.go / orm_queries_jsts_mongoose_populate.go
// (#3426/#3844). The existing Java ORM/driver scanners (scanJavaORM,
// scanJavaDrivers) recognise the native driver `database.getCollection("x")`
// target, but they do NOT understand a Spring Data MongoDB aggregation
// pipeline, and a `$lookup` stage there is an implicit cross-collection JOIN
// the migration must reason about (Mongo has no schema FK for it). This pass
// recovers, in the SAME emission shape as the Python/Mongoose siblings:
//
//  1. JOINS_COLLECTION relationship — for every `$lookup` stage, an edge from
//     the aggregating collection → the looked-up `from` collection. FromID /
//     ToID are `Class:<capitalisedSingular(collection)>`, identical to the
//     contract the Python/JS passes use, so a Spring Data `$lookup` from Book
//     to authors lands on the SAME `Class:Author` node a Mongoose `ref:` or a
//     pymongo `$lookup` would. Properties: local_field, foreign_field, as,
//     stage="lookup".
//
//  2. SCOPE.DataAccess pipeline-stage entities — one per `$lookup`, anchored at
//     the lookup site, subtype `$lookup`, stage_index preserved.
//
// Two Spring Data idioms are resolved:
//
//   - FLUENT `LookupOperation` (the canonical `MongoTemplate` form):
//
//     LookupOperation lookup = LookupOperation.newLookup()
//     .from("authors").localField("authorId").foreignField("_id").as("authors");
//     Aggregation agg = Aggregation.newAggregation(lookup, match);
//     mongoTemplate.aggregate(agg, "books", Result.class);
//
//     The join fields come from the fluent `.from(..)/.localField(..)/...`
//     method calls (NOT JSON keys). The aggregating collection is resolved from
//     the `mongoTemplate.aggregate(agg, "books"|Book.class, ...)` executor call
//     in the same file (string-literal collection wins; else the `X.class`
//     entity name; else a same-file `@Document("books")` on the aggregating
//     class). `Aggregation.lookup("authors","authorId","_id","authors")` — the
//     positional static-factory shorthand — is also recovered.
//
//   - `@Aggregation(pipeline={ "{ $lookup: { from: 'authors', ... } }" })` on a
//     repository method: the pipeline stages are MongoDB extended-JSON string
//     literals. We concatenate the pipeline string array, feed it to the SAME
//     JSON stage splitter/lookup parser the JS/Python passes use, and resolve
//     the aggregating collection from the repository's `@Document`-typed entity
//     (the `MongoRepository<Book, ...>` / `@Document` generic) when available,
//     else from a class-name heuristic.
//
// HONEST LIMIT: a dynamic `from` (`.from(collVar)` / `.from(SOME_CONST)` with no
// quoted value) yields NO edge — we never fabricate a join we cannot statically
// resolve. A dynamic aggregating collection (`mongoTemplate.aggregate(agg,
// resolveColl(), ...)`) leaves the FromID as the fallback class name rather
// than guessing. Cross-file pipeline assembly is out of scope.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoAggJavaGateRe gates the scan to files that plausibly use Spring Data
// MongoDB aggregation, so we never scan arbitrary `.aggregate(` chains.
var mongoAggJavaGateRe = regexp.MustCompile(
	`\b(?:LookupOperation|Aggregation\.newAggregation|Aggregation\.lookup|MongoTemplate|mongoTemplate|@Aggregation|org\.springframework\.data\.mongodb)\b`,
)

// mongoAggJavaLookupOpRe locates a fluent `LookupOperation.newLookup()` chain
// start. The full chain (`.from(..).localField(..).foreignField(..).as(..)`) is
// recovered by scanning forward from the match.
var mongoAggJavaLookupOpRe = regexp.MustCompile(`LookupOperation\s*\.\s*newLookup\s*\(\s*\)`)

// mongoAggJavaFluentFieldRe captures a `.method("literal")` call within a
// LookupOperation fluent chain (group 1 = method, group 2 = string literal).
// Only quoted string args are captured — a variable arg (`.from(coll)`) yields
// no match, which is the honest dynamic-skip.
var mongoAggJavaFluentFieldRe = regexp.MustCompile(
	`\.\s*(from|localField|foreignField|as)\s*\(\s*"([A-Za-z_][\w$.-]*)"\s*\)`,
)

// mongoAggJavaStaticLookupRe matches the positional static-factory shorthand
// `Aggregation.lookup("authors", "authorId", "_id", "authors")`. Groups:
// 1=from, 2=localField, 3=foreignField, 4=as. All four must be string literals;
// a dynamic arg in any position fails the match (honest skip — the fluent path
// or no edge).
var mongoAggJavaStaticLookupRe = regexp.MustCompile(
	`Aggregation\s*\.\s*lookup\s*\(\s*"([A-Za-z_][\w$.-]*)"\s*,\s*"([A-Za-z_][\w$.-]*)"\s*,\s*"([A-Za-z_][\w$.-]*)"\s*,\s*"([A-Za-z_][\w$.-]*)"\s*\)`,
)

// mongoAggJavaTemplateCollRe captures the aggregating collection from a
// `mongoTemplate.aggregate(agg, "books", Result.class)` executor call where the
// second argument is a quoted collection name.
var mongoAggJavaTemplateCollStrRe = regexp.MustCompile(
	`\.\s*aggregate\s*\(\s*[A-Za-z_]\w*\s*,\s*"([A-Za-z_][\w$.-]*)"`,
)

// mongoAggJavaTemplateCollClassRe captures the aggregating collection from a
// `mongoTemplate.aggregate(agg, Book.class, Result.class)` executor call where
// the second argument is an entity `.class` reference — the entity name is the
// collection token (resolved via capitalisedSingular like every other path).
var mongoAggJavaTemplateCollClassRe = regexp.MustCompile(
	`\.\s*aggregate\s*\(\s*[A-Za-z_]\w*\s*,\s*([A-Z][A-Za-z0-9_$]*)\s*\.\s*class`,
)

// mongoAggJavaDocumentRe captures a `@Document("books")` /
// `@Document(collection = "books")` / `@Document(value = "books")` collection
// name. Used to resolve the aggregating collection from the same-file entity
// when no executor-call collection is available.
var mongoAggJavaDocumentRe = regexp.MustCompile(
	`@Document\s*\(\s*(?:(?:collection|value)\s*=\s*)?"([A-Za-z_][\w$.-]*)"`,
)

// mongoAggJavaAggregationAnnoRe locates a repository-method `@Aggregation(`
// annotation; the `pipeline = {...}` string array is recovered by scanning the
// annotation argument.
var mongoAggJavaAggregationAnnoRe = regexp.MustCompile(`@Aggregation\s*\(`)

// mongoAggJavaRepoEntityRe captures the entity type parameter of a Spring Data
// Mongo repository declaration: `interface BookRepository extends
// MongoRepository<Book, String>` / `ReactiveMongoRepository<Book, ObjectId>` /
// `CrudRepository<Book, String>`. Group 1 is the entity (the aggregating
// collection token for an `@Aggregation` repository method).
var mongoAggJavaRepoEntityRe = regexp.MustCompile(
	`(?:Mongo|ReactiveMongo|Crud|PagingAndSorting)Repository\s*<\s*([A-Z][A-Za-z0-9_$]*)\s*,`,
)

// scanJavaSpringMongoAggregation walks `src`, finds Spring Data MongoDB
// aggregation `$lookup` joins (fluent LookupOperation, positional
// Aggregation.lookup, and `@Aggregation(pipeline={...})` string pipelines),
// resolves the aggregating collection, and emits SCOPE.DataAccess stage
// entities + JOINS_COLLECTION edges via the supplied appenders — matching the
// Python/Mongoose contract exactly.
func scanJavaSpringMongoAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	if !mongoAggJavaGateRe.MatchString(src) {
		return
	}

	// Resolve the aggregating collection ONCE per file: the executor call's
	// quoted/`.class` second argument wins, else a same-file `@Document(...)`,
	// else a `MongoRepository<Entity, ...>` entity. This is intentionally
	// file-scoped — the canonical Spring Data shape has one aggregating entity
	// per repository/service file.
	coll := mongoAggJavaResolveCollection(src)

	stageIdx := 0
	emitLookup := func(off int, lk mongoAggLookup) {
		if lk.from == "" || coll == "" {
			return // dynamic from or unresolved collection — honest skip.
		}
		emitJoin(mongoAggJoinEdge(coll, lk, "lookup"))
		// #4244 — the node-anchored twin (FromID = this stage's graph id →
		// Class:<from>) is emitted post-stamp by buildMongoAggStageJoinRels
		// from props["from"]. entityName MUST match the emitStage Name; the
		// `@L<line>` segment keeps the per-call-site stage node unique
		// (see mongoAggStageName).
		entityName := mongoAggStageName(coll, lineOfOffset(src, off), stageIdx, "$lookup")

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

	// Idiom 1a: positional static-factory `Aggregation.lookup(from, lf, ff, as)`.
	for _, m := range mongoAggJavaStaticLookupRe.FindAllStringSubmatchIndex(src, -1) {
		emitLookup(m[0], mongoAggLookup{
			from:         src[m[2]:m[3]],
			localField:   src[m[4]:m[5]],
			foreignField: src[m[6]:m[7]],
			as:           src[m[8]:m[9]],
		})
	}

	// Idiom 1b: fluent `LookupOperation.newLookup().from(..)....as(..)`. Scan a
	// bounded window forward from each chain start and pull the fluent fields.
	for _, loc := range mongoAggJavaLookupOpRe.FindAllStringIndex(src, -1) {
		lk := mongoAggJavaParseFluentLookup(src, loc[1])
		emitLookup(loc[0], lk)
	}

	// Idiom 2: `@Aggregation(pipeline = { "{ $lookup: {...} }", ... })` repo
	// method — extended-JSON string pipeline. Reuse the shared JSON splitter.
	for _, loc := range mongoAggJavaAggregationAnnoRe.FindAllStringIndex(src, -1) {
		openParen := loc[1] - 1
		pipeline := mongoAggJavaPipelineLiteral(src, openParen)
		if pipeline == "" {
			continue
		}
		stages := mongoAggSplitStages(pipeline, 0)
		for _, st := range stages {
			if mongoAggFirstKey(st) != "$lookup" {
				continue
			}
			emitLookup(loc[0], mongoAggParseLookup(st))
		}
	}
}

// mongoAggJavaParseFluentLookup scans forward from `from` (just past
// `LookupOperation.newLookup()`) over a bounded window and recovers the fluent
// `.from/.localField/.foreignField/.as` string-literal fields. A field bound to
// a variable (no quoted literal) is left empty — honest: a missing `from` then
// suppresses the edge in emitLookup.
func mongoAggJavaParseFluentLookup(src string, from int) mongoAggLookup {
	end := from + 400
	if end > len(src) {
		end = len(src)
	}
	window := src[from:end]
	// Stop at the first statement terminator so we don't bleed into the next
	// statement's `.from(..)` (the chain is a single expression up to `;`).
	if semi := strings.IndexByte(window, ';'); semi >= 0 {
		window = window[:semi]
	}
	var lk mongoAggLookup
	for _, m := range mongoAggJavaFluentFieldRe.FindAllStringSubmatch(window, -1) {
		switch m[1] {
		case "from":
			if lk.from == "" {
				lk.from = m[2]
			}
		case "localField":
			lk.localField = m[2]
		case "foreignField":
			lk.foreignField = m[2]
		case "as":
			lk.as = m[2]
		}
	}
	return lk
}

// mongoAggJavaResolveCollection recovers the aggregating collection token for
// the file: a `mongoTemplate.aggregate(agg, "books"|Book.class, ...)` executor
// argument wins (string literal first, then the `.class` entity name), else a
// same-file `@Document(...)` collection, else a `MongoRepository<Entity, ...>`
// entity type. Returns "" when none is statically present.
func mongoAggJavaResolveCollection(src string) string {
	if m := mongoAggJavaTemplateCollStrRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	if m := mongoAggJavaTemplateCollClassRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	if m := mongoAggJavaDocumentRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	if m := mongoAggJavaRepoEntityRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	return ""
}

// mongoAggJavaPipelineLiteral returns a synthesised JSON array literal
// `[ <stage>, <stage> ]` built from the string elements of an `@Aggregation`
// `pipeline = { "...", "..." }` argument whose `(` is at `openParen`. Each
// quoted element is a MongoDB extended-JSON stage object; we join them into a
// bracketed list so the shared mongoAggSplitStages can scan it. Returns "" when
// no `pipeline = {...}` string array is present (e.g. only a `count`/`sort`
// attribute) or the elements are not string literals.
func mongoAggJavaPipelineLiteral(src string, openParen int) string {
	// Bound the scan to this annotation's argument list (matching `)`).
	close := mongoAggPyMatchParen(src, openParen)
	if close < 0 {
		return ""
	}
	arg := src[openParen+1 : close]
	// Collect every double-quoted string element. Spring's extended-JSON uses
	// single quotes for inner values (`from: 'authors'`), so the element
	// delimiter is the double quote — inner single quotes never terminate it.
	var elems []string
	inStr := false
	start := -1
	for i := 0; i < len(arg); i++ {
		c := arg[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				elems = append(elems, arg[start:i])
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			start = i + 1
		}
	}
	if len(elems) == 0 {
		return ""
	}
	return "[" + strings.Join(elems, ",") + "]"
}
