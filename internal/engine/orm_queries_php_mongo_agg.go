// PHP MongoDB aggregation `$lookup` + mapping-reference join extraction for the
// Doctrine MongoDB ODM and Laravel-MongoDB (jenssegers/mongodb) (#3849, epic
// #3837). This is the PHP sibling of orm_queries_python_mongo_agg.go (#3440),
// orm_queries_jsts_mongo_agg.go / orm_queries_jsts_mongoose_populate.go
// (#3426/#3844), orm_queries_go_mongo_agg.go (#3846) and
// orm_queries_java_mongo_agg.go (#3845). It completes the cross-language Mongo
// epic: every supported backend language now surfaces the application-side
// cross-collection join (`$lookup` / mapping reference) that MongoDB has no
// schema FK for, in the SAME emission shape:
//
//  1. JOINS_COLLECTION relationship — Class:<aggregating coll> →
//     Class:<from coll>, with FromID / ToID `Class:<capitalisedSingular(...)>`
//     so a PHP `$lookup` from Book to authors lands on the SAME `Class:Author`
//     node a Mongoose `ref:`, a pymongo `$lookup` or a Spring Data `$lookup`
//     would. Properties: local_field, foreign_field, as, stage.
//
//  2. SCOPE.DataAccess pipeline-stage entities — one per recovered `$lookup`,
//     anchored at the lookup site, subtype `$lookup`, stage_index preserved.
//
// Three PHP idioms are resolved:
//
//   - Doctrine ODM FLUENT aggregation builder (the canonical ODM form):
//
//     $builder = $dm->createAggregationBuilder(Book::class);
//     $builder->lookup('authors')->localField('authorId')
//     .foreignField('_id')->alias('author');
//
//     The aggregating collection is the `Book::class` argument of
//     `createAggregationBuilder(...)` (or a same-file `@Document(collection=
//     "books")` on the builder's document class). The looked-up `from` is the
//     `->lookup('authors')` argument; the join fields come from the fluent
//     `->localField(..)->foreignField(..)->alias(..)` chain. ODM names the
//     output field `alias`, mapped onto the shared `as` property.
//
//   - Doctrine ODM MAPPING REFERENCE annotations / attributes — the schema-level
//     reference that is the ODM analogue of a Mongoose `ref:`:
//
//     #[Document(collection: "books")] class Book {
//     #[ReferenceMany(targetDocument: Author::class)] public $authors;
//     }
//     /** @Document @ReferenceOne(targetDocument=Author::class) */
//
//     `@ReferenceMany` / `@ReferenceOne` / `@EmbedMany` / `@EmbedOne` with a
//     `targetDocument=Author::class` (or quoted `"Author"`) on a property of a
//     `@Document` class → JOINS_COLLECTION(owning Document → targetDocument),
//     mirroring the Mongoose-ref / Mongoid-association convention. The owning
//     document is the enclosing `@Document` class name.
//
//   - Laravel-MongoDB (jenssegers/mongodb) raw `$lookup` aggregation — the PHP
//     ARRAY pipeline (`'$lookup' => ['from' => 'authors', ...]`, fat-arrow
//     syntax, no JSON colons):
//
//     Book::raw(fn($c) => $c->aggregate([
//     ['$lookup' => ['from' => 'authors', 'localField' => 'author_id',
//     'foreignField' => '_id', 'as' => 'author']],
//     ]));
//
//     The aggregating collection is the Eloquent-Mongo model the call is made on
//     (`Book::raw(...)` → Book, or a same-file `protected $collection = "books"`
//     / model class name). The pipeline is a PHP array of `['$op' => [...]]`
//     stages; each `$lookup` stage's `from`/`localField`/`foreignField`/`as`
//     fields use PHP fat-arrow (`=>`) string values.
//
// HONEST LIMIT: a dynamic `from` (a variable / expression, `->lookup($coll)` /
// `'from' => $coll`), a dynamic targetDocument, or an unresolvable aggregating
// collection yields NO edge — we never fabricate a join we cannot statically
// resolve. Cross-file pipeline assembly and runtime-built pipelines are out of
// scope (left unresolved — honest-partial).
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoAggPHPGateRe gates the scan to files that plausibly use a PHP Mongo ODM
// or Laravel-MongoDB surface, so we never scan arbitrary `->aggregate(` chains
// (e.g. a Collection helper) or `@ReferenceMany` on a relational ORM.
var mongoAggPHPGateRe = regexp.MustCompile(
	`(?:createAggregationBuilder|ReferenceMany|ReferenceOne|EmbedMany|EmbedOne|` +
		`jenssegers|MongoDB\\|Doctrine\\\\ODM\\\\MongoDB|ODM\\\\MongoDB|` +
		`\$lookup|\bDocument\b)`,
)

// ---------------------------------------------------------------------------
// Idiom 1: Doctrine ODM fluent aggregation builder
// ---------------------------------------------------------------------------

// mongoAggPHPBuilderRe locates a `createAggregationBuilder(Book::class)` call and
// captures the aggregating document class (group 1). The argument may be a
// `Book::class` reference or a quoted `"Book"` / `'Book'` document name. A
// dynamic argument (a variable) does not match — honest skip.
var mongoAggPHPBuilderRe = regexp.MustCompile(
	`createAggregationBuilder\s*\(\s*(?:\\?(?:[\w\\]+\\)?(\w+)::class|['"]([\w\\]*?\\)?(\w+)['"])\s*\)`,
)

// mongoAggPHPLookupRe locates a `->lookup('authors')` call in a Doctrine ODM
// fluent aggregation chain and captures the looked-up `from` collection (group
// 1). A dynamic argument (`->lookup($coll)`) does not match — honest skip.
var mongoAggPHPLookupRe = regexp.MustCompile(
	`->\s*lookup\s*\(\s*['"]([A-Za-z_][\w$.\\-]*)['"]\s*\)`,
)

// mongoAggPHPFluentFieldRe captures a `->method('literal')` call within a
// Doctrine ODM fluent lookup chain (group 1 = method, group 2 = string literal).
// ODM uses `localField`/`foreignField`/`alias` (alias == the JS/Java `as`). Only
// quoted string args are captured; a variable arg yields no match — honest skip.
var mongoAggPHPFluentFieldRe = regexp.MustCompile(
	`->\s*(localField|foreignField|alias|as)\s*\(\s*['"]([A-Za-z_][\w$.-]*)['"]\s*\)`,
)

// ---------------------------------------------------------------------------
// Idiom 2: Doctrine ODM mapping-reference annotations / attributes
// ---------------------------------------------------------------------------

// mongoAggPHPDocumentClassRe captures a `@Document`/`#[Document]`-mapped class
// name. Both the annotation form (`@Document` in a docblock above the class) and
// the PHP 8 attribute form (`#[Document(...)]`) precede a `class Name`. We pair a
// reference annotation with its enclosing class by span; this regex finds the
// class declarations so we can compute those spans.
var mongoAggPHPClassDeclRe = regexp.MustCompile(`\bclass\s+([A-Za-z_]\w*)`)

// mongoAggPHPReferenceRe matches a Doctrine ODM mapping-reference annotation or
// attribute with a static `targetDocument`:
//
//	#[ReferenceMany(targetDocument: Author::class)]
//	@ReferenceOne(targetDocument=Author::class)
//	@EmbedMany(targetDocument="Author")
//
// Group 1 = reference kind, group 2 = `Author` from `Author::class`, group 3 =
// `Author` from a quoted `"Author"` / `'Author'`. Exactly one of group 2/3 is
// set. A dynamic targetDocument (a variable) does not match — honest skip.
var mongoAggPHPReferenceRe = regexp.MustCompile(
	`\b(ReferenceMany|ReferenceOne|EmbedMany|EmbedOne)\b[^)\]]*?targetDocument\s*[:=]\s*` +
		`(?:\\?(?:[\w\\]+\\)?(\w+)::class|['"](?:[\w\\]*?\\)?(\w+)['"])`,
)

// ---------------------------------------------------------------------------
// Idiom 3: Laravel-MongoDB (jenssegers) raw `$lookup` aggregation
// ---------------------------------------------------------------------------

// mongoAggPHPAggregateRe locates an `->aggregate([ ... ])` call (the Laravel
// raw-aggregation pipeline). The pipeline array literal is recovered by scanning
// from the `[` that follows.
var mongoAggPHPAggregateRe = regexp.MustCompile(`->\s*aggregate\s*\(\s*\[`)

// mongoAggPHPRawModelRe captures the Eloquent-Mongo model a raw aggregation is
// made on: `Book::raw(...)` / `Book::raw(function ...)`. Group 1 is the model.
var mongoAggPHPRawModelRe = regexp.MustCompile(`(?:\\?(?:[\w\\]+\\)?)([A-Z]\w*)::raw\s*\(`)

// mongoAggPHPCollectionPropRe captures a `protected $collection = "books";`
// model property — the Eloquent-Mongo table/collection name override.
var mongoAggPHPCollectionPropRe = regexp.MustCompile(
	`\$collection\s*=\s*['"]([A-Za-z_][\w$.-]*)['"]`,
)

// mongoAggPHPArrayStringField pulls the PHP fat-arrow string value bound to
// `key` in a PHP array stage: `'from' => 'authors'` → "authors". Returns "" when
// the key is absent or its value is not a plain quoted literal (a variable /
// expression) — honest: a dynamic `from` yields no join edge.
var mongoAggPHPArrayFieldReCache = map[string]*regexp.Regexp{}

func mongoAggPHPArrayStringField(stage, key string) string {
	re, ok := mongoAggPHPArrayFieldReCache[key]
	if !ok {
		re = regexp.MustCompile(
			`['"]` + regexp.QuoteMeta(key) + `['"]\s*=>\s*['"]([A-Za-z_][\w$.-]*)['"]`,
		)
		mongoAggPHPArrayFieldReCache[key] = re
	}
	if m := re.FindStringSubmatch(stage); m != nil {
		return m[1]
	}
	return ""
}

// mongoAggPHPArrayStageOp returns the first `$op` key of a PHP-array stage, e.g.
// `$lookup` from `['$lookup' => [ ... ]]`. PHP-array stages have no `{` so the
// shared mongoAggFirstKey (JS-object oriented) is not used; we take the first
// quoted `$`-prefixed key.
func mongoAggPHPArrayStageOp(stage string) string {
	if m := mongoAggPHPFirstDollarKeyRe.FindStringSubmatch(stage); m != nil {
		return m[1]
	}
	return ""
}

// mongoAggPHPFirstDollarKeyRe matches the first quoted `$op` key in a PHP-array
// stage (the stage operator is always the first dollar-prefixed quoted key; a
// nested accumulator like `'$sum'` appears later as a value).
var mongoAggPHPFirstDollarKeyRe = regexp.MustCompile(`['"](\$[A-Za-z][\w$]*)['"]`)

// scanPHPMongoAggregation walks `src`, recovers PHP MongoDB cross-collection
// joins from the three idioms (Doctrine ODM fluent aggregation, Doctrine ODM
// mapping-reference annotations, and Laravel-MongoDB raw `$lookup`), resolves
// the aggregating collection, and emits SCOPE.DataAccess stage entities +
// JOINS_COLLECTION edges via the supplied appenders — matching the cross-
// language contract exactly.
func scanPHPMongoAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	if !mongoAggPHPGateRe.MatchString(src) {
		return
	}

	stageIdx := 0
	emitLookup := func(off int, coll string, lk mongoAggLookup, stageName string) {
		if lk.from == "" || coll == "" {
			return // dynamic from or unresolved collection — honest skip.
		}
		emitJoin(mongoAggJoinEdge(coll, lk, stageName))
		// #4244 — node-anchored twin emitted post-stamp by
		// buildMongoAggStageJoinRels from props["from"]. entityName MUST match
		// the emitStage Name; the `@L<line>` segment keeps the per-call-site
		// stage node unique so two `$lookup`s at the same per-file stage index
		// in different aggregations never collapse onto one graph node (see
		// mongoAggStageName).
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

	scanPHPDoctrineFluentAgg(src, emitLookup)
	scanPHPDoctrineReferences(src, emitJoin)
	scanPHPLaravelRawAgg(src, funcs, emitLookup)
}

// scanPHPDoctrineFluentAgg handles the Doctrine ODM fluent aggregation builder:
// `$builder = $dm->createAggregationBuilder(Book::class); $builder->lookup(
// 'authors')->localField(..)->foreignField(..)->alias(..)`. The aggregating
// document class is resolved from the `createAggregationBuilder(...)` argument
// (file-scoped — the canonical ODM service builds one aggregation per builder).
func scanPHPDoctrineFluentAgg(src string, emitLookup func(off int, coll string, lk mongoAggLookup, stageName string)) {
	coll := mongoAggPHPResolveBuilderCollection(src)
	if coll == "" {
		return // no statically resolvable aggregating document — honest skip.
	}
	for _, loc := range mongoAggPHPLookupRe.FindAllStringSubmatchIndex(src, -1) {
		from := src[loc[2]:loc[3]]
		lk := mongoAggPHPParseFluentLookup(src, loc[1], from)
		emitLookup(loc[0], coll, lk, "lookup")
	}
}

// mongoAggPHPResolveBuilderCollection returns the aggregating document token from
// the first `createAggregationBuilder(Book::class)` call in the file (group 2 =
// `Book::class` name, groups 3/4 = quoted document name). Returns "" when no
// builder call with a static document argument is present.
func mongoAggPHPResolveBuilderCollection(src string) string {
	m := mongoAggPHPBuilderRe.FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	if m[1] != "" { // Book::class
		return m[1]
	}
	if m[3] != "" { // quoted "Book" (group 3 is the trailing name after optional ns)
		return m[3]
	}
	return ""
}

// mongoAggPHPParseFluentLookup scans forward from `from` (just past the
// `->lookup('coll')` call) over a bounded window and recovers the fluent
// `->localField/->foreignField/->alias` string-literal fields. A field bound to
// a variable yields no match (honest dynamic-skip). `fromColl` seeds lk.from.
func mongoAggPHPParseFluentLookup(src string, scanFrom int, fromColl string) mongoAggLookup {
	end := scanFrom + 400
	if end > len(src) {
		end = len(src)
	}
	window := src[scanFrom:end]
	// Stop at the first statement terminator so we don't bleed into the next
	// statement's fluent fields (the chain is a single expression up to `;`).
	if semi := strings.IndexByte(window, ';'); semi >= 0 {
		window = window[:semi]
	}
	lk := mongoAggLookup{from: fromColl}
	for _, m := range mongoAggPHPFluentFieldRe.FindAllStringSubmatch(window, -1) {
		switch m[1] {
		case "localField":
			lk.localField = m[2]
		case "foreignField":
			lk.foreignField = m[2]
		case "alias", "as":
			lk.as = m[2]
		}
	}
	return lk
}

// scanPHPDoctrineReferences handles Doctrine ODM mapping-reference annotations /
// attributes: a `@ReferenceMany`/`@ReferenceOne`/`@EmbedMany`/`@EmbedOne` with a
// static `targetDocument` on a property of a `@Document` class →
// JOINS_COLLECTION(owning Document → targetDocument), mirroring the Mongoose-ref
// convention. The owning document is the enclosing class name.
func scanPHPDoctrineReferences(src string, emitJoin func(rel types.RelationshipRecord)) {
	classes := mongoAggPHPClassSpans(src)
	if len(classes) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, m := range mongoAggPHPReferenceRe.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		target := ""
		if m[4] >= 0 { // Author::class
			target = src[m[4]:m[5]]
		} else if m[6] >= 0 { // quoted "Author"
			target = src[m[6]:m[7]]
		}
		if target == "" {
			continue
		}
		owner := mongoAggPHPOwnerClassAt(classes, m[0])
		if owner == "" {
			continue
		}
		from := capitalisedSingular(owner)
		to := capitalisedSingular(target)
		if from == "" || to == "" {
			continue
		}
		key := from + "->" + to + ":" + kind
		if seen[key] {
			continue
		}
		seen[key] = true
		emitJoin(types.RelationshipRecord{
			FromID: fmt.Sprintf("Class:%s", from),
			ToID:   fmt.Sprintf("Class:%s", to),
			Kind:   mongoAggJoinEdgeKind,
			Properties: map[string]string{
				"pattern_type": mongoAggPatternType,
				"via":          "reference",
				"reference":    kind,
				"ref":          target,
			},
		})
	}
}

// mongoAggPHPClassSpan is the [start,end) byte span of a PHP class body together
// with its name. References inside the span are attributed to this owner.
type mongoAggPHPClassSpan struct {
	name  string
	start int
	end   int
}

// mongoAggPHPClassSpans locates every `class Name { ... }` and computes its body
// span (matching-brace range) so a reference annotation can be attributed to its
// enclosing document class.
func mongoAggPHPClassSpans(src string) []mongoAggPHPClassSpan {
	var spans []mongoAggPHPClassSpan
	for _, m := range mongoAggPHPClassDeclRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		i := m[1]
		for i < len(src) && src[i] != '{' {
			i++
		}
		if i >= len(src) {
			continue
		}
		end := mongooseMatchBrace(src, i)
		if end < 0 {
			end = len(src)
		}
		spans = append(spans, mongoAggPHPClassSpan{name: name, start: i, end: end})
	}
	return spans
}

// mongoAggPHPOwnerClassAt returns the name of the class whose body contains
// `pos`, or "" if none.
func mongoAggPHPOwnerClassAt(spans []mongoAggPHPClassSpan, pos int) string {
	for _, s := range spans {
		if pos >= s.start && pos < s.end {
			return s.name
		}
	}
	return ""
}

// scanPHPLaravelRawAgg handles Laravel-MongoDB (jenssegers) raw `$lookup`
// aggregation: `Book::raw(fn($c) => $c->aggregate([ ['$lookup' => [...]], ...]))`.
// The aggregating collection is the Eloquent-Mongo model (`Book::raw` → Book,
// else a `$collection` property, else the file's model class). The pipeline is a
// PHP array of `['$op' => [...]]` stages with fat-arrow string values.
func scanPHPLaravelRawAgg(src string, funcs []funcSpan, emitLookup func(off int, coll string, lk mongoAggLookup, stageName string)) {
	coll := mongoAggPHPResolveRawModel(src)
	if coll == "" {
		return
	}
	for _, loc := range mongoAggPHPAggregateRe.FindAllStringIndex(src, -1) {
		// loc[1]-1 is the index of the `[` that opens the pipeline array.
		arrStart := loc[1] - 1
		stages := mongoAggSplitStages(src, arrStart)
		if len(stages) == 0 {
			continue
		}
		for _, st := range stages {
			if mongoAggPHPArrayStageOp(st) != "$lookup" {
				continue
			}
			lk := mongoAggLookup{
				from:         mongoAggPHPArrayStringField(st, "from"),
				localField:   mongoAggPHPArrayStringField(st, "localField"),
				foreignField: mongoAggPHPArrayStringField(st, "foreignField"),
				as:           mongoAggPHPArrayStringField(st, "as"),
			}
			emitLookup(loc[0], coll, lk, "lookup")
		}
	}
}

// mongoAggPHPResolveRawModel returns the aggregating Eloquent-Mongo collection
// token: a `Model::raw(...)` model name wins, else a `protected $collection =
// "books"` property, else a same-file class name. Returns "" when none is
// statically present.
func mongoAggPHPResolveRawModel(src string) string {
	if m := mongoAggPHPRawModelRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	if m := mongoAggPHPCollectionPropRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	if m := mongoAggPHPClassDeclRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	return ""
}
