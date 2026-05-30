// MongoDB aggregation-pipeline internal extraction for JS/TS (#3426).
//
// orm_queries_jsts.go and orm_queries_jsts_drivers.go already DETECT the
// `.aggregate([...])` call itself as an aggregate QUERIES edge (mongoose
// `Model.aggregate(...)`, raw driver `db.collection('c').aggregate(...)`),
// but they treat the pipeline array as an opaque argument blob. The pipeline
// is where the actual data-flow lives: a `$lookup` stage is an implicit
// cross-collection JOIN that the migration needs to reason about, and each
// stage is a distinct transformation step worth representing as a node.
//
// This pass is a SIBLING to the existing aggregate detection: it does NOT
// re-emit the aggregate QUERIES edge. Instead it locates the same call sites,
// parses the inline pipeline array literal with a brace/bracket-depth,
// string-aware scanner, and emits:
//
//  1. JOINS_COLLECTION relationship — for every `$lookup` / `$graphLookup`
//     stage, an edge from the aggregating collection/model → the `from`
//     collection. Properties: local_field, foreign_field, as, stage.
//     This is the highest-value output: the implicit application-side join
//     MongoDB has no schema FK for.
//
//  2. SCOPE.DataAccess pipeline-stage entities — one per stage, anchored at
//     the aggregate call site, with subtype = the stage operator
//     ($match, $group, $unwind, $facet, $project, $sort, $limit, $addFields,
//     $lookup, $graphLookup). Stage ORDER is preserved as a stage_index
//     property. Selected stages capture extra structure:
//     - $group:  the `_id` expression + accumulator field names
//     - $facet:  the named sub-pipeline keys
//
// RESOLUTION (#3440 asks 2-JS + 3): the pipeline argument is resolved in three
// shapes, in priority order:
//
//  1. INLINE array literal — `.aggregate([ {$lookup...}, ... ])`. Parsed in
//     place (the original #3426 behaviour).
//
//  2. VARIABLE-bound (ask 2-JS) — `.aggregate(pipeline)` where `pipeline` is
//     a bare identifier. We follow the single same-function-scope binding
//     `const pipeline = [ ... ]` (also `let` / `var`) whose initializer is an
//     array literal, then parse it exactly as if it were inline. If the
//     binding is not found in the same function scope, or its initializer is
//     not a literal array (e.g. `= stages`, `= buildPipeline()`), it is left
//     unresolved — honest, no fabrication.
//
//  3. BUILDER `.build()` (ask 3) — `.aggregate(builder.build())` /
//     `.aggregate(b.build())` where `builder` is constructed via a fluent
//     chain `new AggregationBuilder().match({...}).lookup({ from, ... })...`.
//     We resolve the builder variable to its construction chain in the same
//     function scope (or accept an inline `new X()....build()` one-liner),
//     walk the chained `.match()/.lookup()/.group()/...` method calls, and
//     emit one stage entity per method (`.match`→$match, `.lookup`→$lookup,
//     `.group`→$group, `.unwind`→$unwind, `.project`→$project, `.sort`→$sort,
//     `.limit`→$limit, `.skip`→$skip, `.addFields`→$addFields,
//     `.graphLookup`→$graphLookup, `.facet`→$facet) plus a JOINS_COLLECTION
//     edge for every `.lookup({from})` / `.graphLookup({from})`. This is the
//     idiomatic NestJS / Papr pattern.
//
// HONEST LIMIT: resolution is statically local to the enclosing function. A
// pipeline assembled across files, built behind a conditional, mutated by
// `.push()`, or spread from another array is captured only to the extent it is
// statically present — we never fabricate stages or joins we cannot see. The
// receiver resolution is likewise local: `Model.aggregate(...)` uses the
// capitalised model name, `db.collection('c').aggregate(...)` uses the
// collection-string argument; an aggregate on an unrecognised receiver shape
// is skipped.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoAggPatternType tags every entity/edge this pass emits.
const mongoAggPatternType = "mongo_aggregation"

// mongoAggStageEntityKind is the entity Kind used for a single pipeline
// stage. SCOPE.DataAccess models a data-access operation; the stage operator
// lives in Subtype.
var mongoAggStageEntityKind = string(types.EntityKindDataAccess)

// mongoAggJoinEdgeKind is the cross-collection join edge emitted for
// $lookup / $graphLookup.
var mongoAggJoinEdgeKind = string(types.RelationshipKindJoinsCollection)

// mongoAggBuilderNameRe matches a constructed aggregation builder, e.g.
// `new AggregationBuilder()`, `new AggBuilder()`, `new PaprAggregationBuilder()`.
// The class name must end in "AggregationBuilder" or "AggBuilder" (case
// preserved) so we don't treat arbitrary builders (QueryBuilder, FormBuilder)
// as aggregation pipelines.
var mongoAggBuilderNameRe = regexp.MustCompile(
	`\bnew\s+[\w$]*(?:Agg|Aggregation)Builder\b`,
)

// mentionsMongoAggBuilder reports whether the source plausibly uses the fluent
// aggregation-builder pattern: a `new …AggregationBuilder()` / `…AggBuilder()`
// construction together with a terminating `.build()`. Both signals must be
// present so a file that merely imports a builder type but never builds a
// pipeline is not scanned.
func mentionsMongoAggBuilder(src string) bool {
	return mongoAggBuildCallRe.MatchString(src) &&
		mongoAggBuilderNameRe.MatchString(src)
}

// mongoAggCallRe locates `.aggregate(` call sites whose first argument opens
// an inline array literal. Two receiver shapes are recognised:
//
//	<Model>.aggregate([ ...        — capitalised model (mongoose / sequelize)
//	.collection('c')...aggregate([ — native driver (collection arg captured
//	                                 separately by mongoAggCollectionArgRe)
//
// The regex only anchors the `.aggregate(` token + optional leading `[`; the
// receiver is recovered by scanning leftward from the match so we can handle
// both `Model.aggregate` and the chained `db.collection('c').aggregate`.
var mongoAggCallRe = regexp.MustCompile(`\.aggregate\s*\(`)

// mongoAggModelRe pulls a capitalised model identifier immediately preceding
// `.aggregate`. Used when the receiver is a mongoose/sequelize Model.
var mongoAggModelRe = regexp.MustCompile(`([A-Z][A-Za-z0-9_$]*)\s*$`)

// mongoAggCollectionArgRe pulls the collection name out of the nearest
// preceding `.collection('c')` on the same chain (native driver).
var mongoAggCollectionArgRe = regexp.MustCompile(
	`\.collection\(\s*['"` + "`" + `]([a-zA-Z_][\w$.]*)['"` + "`" + `]\s*\)`,
)

// scanJSMongoAggregation walks `src`, finds `.aggregate([...])` call sites
// with an inline pipeline array, parses the pipeline, and emits stage
// entities + cross-collection join edges via the supplied appenders.
//
// emitStage(name, kind, subtype, line, props) appends a pipeline-stage
// entity. emitJoin(fromColl, toColl, props) appends a JOINS_COLLECTION edge.
func scanJSMongoAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	// Gate: only run where a mongo surface is plausible. Reuse the same
	// signals the existing detectors use (mongoose OR native driver), plus the
	// aggregation-builder signal (ask 3) — the NestJS/Papr builder pattern can
	// live in a file that names neither mongoose nor the native driver. This
	// keeps us off arbitrary `.aggregate(` chains (e.g. RxJS, lodash/fp) while
	// still reaching builder-produced pipelines.
	if !mentionsMongooseSequelize(src) && !mentionsMongoDriver(src) &&
		!mentionsMongoAggBuilder(src) {
		return
	}

	for _, loc := range mongoAggCallRe.FindAllStringIndex(src, -1) {
		openParen := loc[1] - 1 // index of '('
		// Resolve the aggregating collection/model from the receiver. The
		// strict resolver only accepts a `.collection('c')` chain or a
		// capitalised model — the authoritative collection shapes used by the
		// inline path.
		coll := mongoAggResolveReceiver(src, loc[0])
		caller := enclosingFuncAt(funcs, loc[0])
		callLine := lineOfOffset(src, loc[0])
		// The function scope window bounds variable/builder resolution so we
		// never pick up a binding from an unrelated function.
		scope := mongoAggScopeOf(src, funcs, loc[0])

		// 1. INLINE array literal — `.aggregate([ ... ])`. Requires the strict
		//    collection/model receiver (the authoritative shape).
		if arrStart := mongoAggSkipToArray(src, openParen); arrStart >= 0 {
			if coll != "" {
				if stages := mongoAggSplitStages(src, arrStart); len(stages) > 0 {
					mongoAggEmitStages(stages, coll, caller, callLine, path, lang,
						emitStage, emitJoin)
				}
			}
			continue
		}

		// The first argument is not an inline array. Recover the argument text
		// up to the matching `)` so we can classify it as a variable or a
		// `.build()` builder expression.
		arg := mongoAggFirstArg(src, openParen)
		if arg == "" {
			continue
		}

		// The builder / variable paths frequently aggregate on a lowercase
		// repository or runner (`repo.aggregate(...)`, `this.collection.…`),
		// which the strict resolver rejects. Fall back to the bare receiver
		// identifier so we still anchor the stages and — crucially — emit the
		// join's authoritative `to` side. The `from` collection in the JOIN
		// edge is what the migration needs; the receiver is best-effort.
		recv := coll
		if recv == "" {
			recv = mongoAggReceiverIdent(src, loc[0])
		}
		if recv == "" {
			continue
		}

		// 3. BUILDER `.build()` — `.aggregate(<chain>.build())` (inline) or
		//    `.aggregate(builder.build())` (resolve builder var in scope).
		if mongoAggBuildCallRe.MatchString(arg) {
			if chain := mongoAggResolveBuilderChain(src, scope, arg); chain != "" {
				if stages := mongoAggBuilderStages(chain); len(stages) > 0 {
					mongoAggEmitStages(stages, recv, caller, callLine, path, lang,
						emitStage, emitJoin)
				}
			}
			continue
		}

		// 2. VARIABLE-bound — `.aggregate(pipeline)` (bare identifier).
		if ident := mongoAggBareIdent(arg); ident != "" {
			if arrStart := mongoAggResolveArrayBinding(src, scope, ident); arrStart >= 0 {
				if stages := mongoAggSplitStages(src, arrStart); len(stages) > 0 {
					mongoAggEmitStages(stages, recv, caller, callLine, path, lang,
						emitStage, emitJoin)
				}
			}
			continue
		}
		// Anything else (call expression, spread, member access) is left
		// unresolved — honest skip.
	}
}

// mongoAggEmitStages emits one SCOPE.DataAccess stage entity per parsed stage
// substring, plus a JOINS_COLLECTION edge for each $lookup / $graphLookup. It
// is shared by all three resolution paths (inline, variable-bound, builder) so
// stage-property extraction and join emission are identical regardless of how
// the pipeline was recovered.
func mongoAggEmitStages(
	stages []string,
	coll, caller string,
	callLine int,
	path, lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	for idx, st := range stages {
		op := mongoAggFirstKey(st)
		if op == "" {
			continue
		}
		props := map[string]string{
			"pattern_type": mongoAggPatternType,
			"collection":   coll,
			"stage_index":  itoa(idx),
			"stage":        op,
		}
		if caller != "" {
			props["caller"] = caller
		}

		switch op {
		case "$lookup":
			lk := mongoAggParseLookup(st)
			if lk.from != "" {
				props["from"] = lk.from
				if lk.localField != "" {
					props["local_field"] = lk.localField
				}
				if lk.foreignField != "" {
					props["foreign_field"] = lk.foreignField
				}
				if lk.as != "" {
					props["as"] = lk.as
				}
				emitJoin(mongoAggJoinEdge(coll, lk, "lookup"))
			}
		case "$graphLookup":
			lk := mongoAggParseLookup(st)
			if lk.from != "" {
				props["from"] = lk.from
				if lk.as != "" {
					props["as"] = lk.as
				}
				emitJoin(mongoAggJoinEdge(coll, lk, "graphLookup"))
			}
		case "$group":
			id, accs := mongoAggParseGroup(st)
			if id != "" {
				props["group_id"] = id
			}
			if accs != "" {
				props["accumulators"] = accs
			}
		case "$facet":
			if keys := mongoAggParseFacetKeys(st); keys != "" {
				props["facets"] = keys
			}
		}

		name := fmt.Sprintf("%s.aggregate#%d %s", coll, idx, op)
		emitStage(types.EntityRecord{
			Name:       name,
			Kind:       mongoAggStageEntityKind,
			Subtype:    op,
			SourceFile: path,
			StartLine:  callLine,
			EndLine:    callLine,
			Language:   lang,
			Properties: props,

			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}
}

// mongoAggResolveReceiver recovers the aggregating collection/model name from
// the text immediately preceding the `.aggregate` token at `dotPos`.
//
//	db.collection('orders').aggregate(...)  → "orders" (native driver)
//	Order.aggregate(...)                    → "Order"  (mongoose model)
//
// Native-driver `.collection('c')` wins when present on the same chain (it is
// the authoritative collection name); otherwise we fall back to a capitalised
// model identifier. Returns "" when neither shape is recognised.
func mongoAggResolveReceiver(src string, dotPos int) string {
	// Look back over a bounded window for a `.collection('c')` on the chain.
	winStart := dotPos - 200
	if winStart < 0 {
		winStart = 0
	}
	window := src[winStart:dotPos]
	if cm := mongoAggCollectionArgRe.FindAllStringSubmatch(window, -1); len(cm) > 0 {
		// Nearest (last) collection() on the chain.
		return cm[len(cm)-1][1]
	}
	// Fall back to a capitalised model identifier directly before `.aggregate`.
	// dotPos points at the '.', so scan the identifier ending at dotPos.
	if mm := mongoAggModelRe.FindStringSubmatch(src[winStart:dotPos]); mm != nil {
		return mm[1]
	}
	return ""
}

// mongoAggReceiverIdentRe pulls the final identifier of the receiver chain
// directly before `.aggregate` (e.g. `repo` from `this.repo`, `coll` from
// `db.coll`). Used as a best-effort anchor for the builder / variable paths
// where the receiver is a lowercase repository/runner the strict resolver
// rejects.
var mongoAggReceiverIdentRe = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*$`)

// mongoAggReceiverIdent returns the bare receiver identifier immediately
// preceding the `.aggregate` token at `dotPos`, or "" if none is present.
func mongoAggReceiverIdent(src string, dotPos int) string {
	winStart := dotPos - 120
	if winStart < 0 {
		winStart = 0
	}
	if m := mongoAggReceiverIdentRe.FindStringSubmatch(src[winStart:dotPos]); m != nil {
		return m[1]
	}
	return ""
}

// mongoAggSkipToArray returns the index of the `[` that opens the pipeline
// array literal, given the `(` of the aggregate call. It skips only
// whitespace between `(` and `[`; if the first non-space token is not `[`
// (e.g. a variable name or spread), the pipeline is dynamic and we return -1.
func mongoAggSkipToArray(src string, openParen int) int {
	i := openParen + 1
	for i < len(src) {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '[' {
			return i
		}
		return -1
	}
	return -1
}

// mongoAggSplitStages splits the pipeline array literal starting at `arrStart`
// (the `[`) into its top-level stage substrings. It is string-aware (handles
// '/"/` quotes with escapes) and depth-aware (only splits on commas at array
// depth 1 / brace depth 0), so nested objects, nested arrays, and quoted
// commas don't break the split. Trailing commas yield no empty stage.
func mongoAggSplitStages(src string, arrStart int) []string {
	if arrStart >= len(src) || src[arrStart] != '[' {
		return nil
	}
	var stages []string
	depthBracket := 0 // []
	depthBrace := 0   // {}
	depthParen := 0   // ()
	inStr := byte(0)
	segStart := -1

	flush := func(end int) {
		if segStart < 0 {
			return
		}
		seg := strings.TrimSpace(src[segStart:end])
		if seg != "" {
			stages = append(stages, seg)
		}
		segStart = -1
	}

	for i := arrStart; i < len(src); i++ {
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
		case '\'', '"', '`':
			inStr = c
		case '[':
			depthBracket++
			if depthBracket == 1 {
				// opening of the pipeline array itself
				segStart = i + 1
			}
		case ']':
			depthBracket--
			if depthBracket == 0 {
				flush(i)
				return stages
			}
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		case '(':
			depthParen++
		case ')':
			depthParen--
		case ',':
			// Split only at top level of the pipeline array.
			if depthBracket == 1 && depthBrace == 0 && depthParen == 0 {
				flush(i)
				segStart = i + 1
			}
		}
	}
	// Unterminated array — return whatever we resolved.
	return stages
}

// mongoAggFirstKey returns the first object key of a stage substring, e.g.
// "$match" from `{ $match: { ... } }`. It is string- and depth-aware so a key
// inside a nested object is never mistaken for the stage operator. Returns ""
// if no top-level key is found.
func mongoAggFirstKey(stage string) string {
	// Find the opening brace of the stage object.
	i := 0
	for i < len(stage) && stage[i] != '{' {
		i++
	}
	if i >= len(stage) {
		return ""
	}
	i++ // past '{'
	// Skip whitespace.
	for i < len(stage) && (stage[i] == ' ' || stage[i] == '\t' || stage[i] == '\n' || stage[i] == '\r') {
		i++
	}
	if i >= len(stage) {
		return ""
	}
	// Quoted key?
	if stage[i] == '\'' || stage[i] == '"' || stage[i] == '`' {
		q := stage[i]
		i++
		start := i
		for i < len(stage) && stage[i] != q {
			if stage[i] == '\\' {
				i++
			}
			i++
		}
		return stage[start:i]
	}
	// Bare key (identifier, possibly `$`-prefixed).
	start := i
	for i < len(stage) && (isMongoKeyChar(stage[i])) {
		i++
	}
	return stage[start:i]
}

func isMongoKeyChar(c byte) bool {
	return c == '$' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// mongoAggLookup holds the join-relevant fields pulled from a $lookup /
// $graphLookup stage.
type mongoAggLookup struct {
	from         string
	localField   string
	foreignField string
	as           string
}

// mongoAggParseLookup extracts the join fields from a $lookup / $graphLookup
// stage. It handles both the classic `{ from, localField, foreignField, as }`
// form and the `{ from, pipeline: [...], as }` sub-pipeline form (we still
// recover `from` + `as`). String values may be single/double/backtick quoted.
func mongoAggParseLookup(stage string) mongoAggLookup {
	return mongoAggLookup{
		from:         mongoAggStringField(stage, "from"),
		localField:   mongoAggStringField(stage, "localField"),
		foreignField: mongoAggStringField(stage, "foreignField"),
		as:           mongoAggStringField(stage, "as"),
	}
}

// mongoAggStringField pulls the quoted string value of `key` from a stage
// substring: `from: 'orders'` / `"from": "orders"` → "orders". Returns "" if
// the key is absent or its value is not a plain string literal (e.g. an
// expression / variable — which we honestly cannot resolve statically).
func mongoAggStringField(stage, key string) string {
	re := regexp.MustCompile(
		`(?:\b` + regexp.QuoteMeta(key) + `\b|['"` + "`" + `]` + regexp.QuoteMeta(key) +
			`['"` + "`" + `])\s*:\s*['"` + "`" + `]([a-zA-Z_$][\w$.]*)['"` + "`" + `]`,
	)
	m := re.FindStringSubmatch(stage)
	if m == nil {
		return ""
	}
	return m[1]
}

// mongoAggJoinEdge builds the JOINS_COLLECTION relationship from the
// aggregating collection to the looked-up `from` collection.
func mongoAggJoinEdge(fromColl string, lk mongoAggLookup, stageName string) types.RelationshipRecord {
	props := map[string]string{
		"pattern_type": mongoAggPatternType,
		"stage":        stageName,
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
	return types.RelationshipRecord{
		FromID:     fmt.Sprintf("Class:%s", capitalisedSingular(fromColl)),
		ToID:       fmt.Sprintf("Class:%s", capitalisedSingular(lk.from)),
		Kind:       mongoAggJoinEdgeKind,
		Properties: props,
	}
}

// mongoAggParseGroup extracts the `_id` expression text and the accumulator
// field names from a $group stage. The `_id` value can be a string
// (`_id: '$status'`), an object (`_id: { y: '$year' }`), or null; we capture a
// compact text form. Accumulators are the OTHER top-level keys of the group
// object (total, count, …). Returns (idText, "field1,field2,...").
func mongoAggParseGroup(stage string) (idText string, accumulators string) {
	body := mongoAggStageBody(stage, "$group")
	if body == "" {
		return "", ""
	}
	keys := mongoAggTopLevelKeys(body)
	var accs []string
	for _, kv := range keys {
		if kv.key == "_id" {
			idText = strings.TrimSpace(kv.val)
			// Collapse internal whitespace for a compact, stable property.
			idText = mongoAggCollapseWS(idText)
			continue
		}
		accs = append(accs, kv.key)
	}
	return idText, strings.Join(accs, ",")
}

// mongoAggParseFacetKeys returns the comma-joined named sub-pipeline keys of a
// $facet stage (e.g. "byStatus,byMonth").
func mongoAggParseFacetKeys(stage string) string {
	body := mongoAggStageBody(stage, "$facet")
	if body == "" {
		return ""
	}
	keys := mongoAggTopLevelKeys(body)
	var names []string
	for _, kv := range keys {
		names = append(names, kv.key)
	}
	return strings.Join(names, ",")
}

// mongoAggStageBody returns the substring inside the `{...}` value of the
// named stage operator, e.g. for `{ $group: { _id: ..., total: ... } }` and
// op "$group" it returns `_id: ..., total: ...`. String- and depth-aware.
func mongoAggStageBody(stage, op string) string {
	// Locate `op` followed by `:` then `{`.
	idx := strings.Index(stage, op)
	if idx < 0 {
		return ""
	}
	i := idx + len(op)
	for i < len(stage) && stage[i] != ':' {
		i++
	}
	if i >= len(stage) {
		return ""
	}
	i++ // past ':'
	for i < len(stage) && (stage[i] == ' ' || stage[i] == '\t' || stage[i] == '\n' || stage[i] == '\r') {
		i++
	}
	if i >= len(stage) || stage[i] != '{' {
		return ""
	}
	// Balanced-brace, string-aware scan for the matching '}'.
	depth := 0
	inStr := byte(0)
	bodyStart := i + 1
	for ; i < len(stage); i++ {
		c := stage[i]
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
		case '\'', '"', '`':
			inStr = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return stage[bodyStart:i]
			}
		}
	}
	return ""
}

// mongoAggKV is a top-level key/value pair within an object body.
type mongoAggKV struct {
	key string
	val string
}

// mongoAggTopLevelKeys splits an object body (the text between the outer
// braces) into its top-level key/value pairs. String- and depth-aware so
// nested objects/arrays are kept whole inside the value. Keys may be quoted
// or bare.
func mongoAggTopLevelKeys(body string) []mongoAggKV {
	var out []mongoAggKV
	i := 0
	n := len(body)
	for i < n {
		// Skip leading separators/whitespace.
		for i < n && (body[i] == ',' || body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}
		// Parse key.
		var key string
		if body[i] == '\'' || body[i] == '"' || body[i] == '`' {
			q := body[i]
			i++
			start := i
			for i < n && body[i] != q {
				if body[i] == '\\' {
					i++
				}
				i++
			}
			key = body[start:i]
			if i < n {
				i++ // past closing quote
			}
		} else {
			start := i
			for i < n && isMongoKeyChar(body[i]) {
				i++
			}
			key = body[start:i]
		}
		// Skip to ':'.
		for i < n && body[i] != ':' {
			i++
		}
		if i >= n {
			break
		}
		i++ // past ':'
		// Capture value up to the top-level comma.
		valStart := i
		depthBrace, depthBracket, depthParen := 0, 0, 0
		inStr := byte(0)
		for i < n {
			c := body[i]
			if inStr != 0 {
				if c == '\\' {
					i += 2
					continue
				}
				if c == inStr {
					inStr = 0
				}
				i++
				continue
			}
			switch c {
			case '\'', '"', '`':
				inStr = c
			case '{':
				depthBrace++
			case '}':
				depthBrace--
			case '[':
				depthBracket++
			case ']':
				depthBracket--
			case '(':
				depthParen++
			case ')':
				depthParen--
			case ',':
				if depthBrace == 0 && depthBracket == 0 && depthParen == 0 {
					goto done
				}
			}
			i++
		}
	done:
		val := strings.TrimSpace(body[valStart:i])
		if key != "" {
			out = append(out, mongoAggKV{key: key, val: val})
		}
		// i is at the comma (or n); loop's leading skip advances past it.
	}
	return out
}

// mongoAggCollapseWS replaces runs of whitespace with a single space so a
// multi-line `_id` object becomes a compact, stable one-line property value.
func mongoAggCollapseWS(s string) string {
	var b strings.Builder
	prevSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteByte(c)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// ---------------------------------------------------------------------------
// #3440 ask 2-JS (variable-bound) + ask 3 (builder) resolution helpers.
// ---------------------------------------------------------------------------

// mongoAggScope is the [start,end) byte range of the function enclosing an
// aggregate call site. Variable/builder binding lookups are confined to this
// window so a binding from an unrelated function is never followed.
type mongoAggScope struct {
	start int
	end   int
}

// mongoAggScopeOf returns the source range of the function enclosing `pos`.
// funcSpan only records each function's START offset, so the scope END is
// approximated as the start of the NEXT function declaration (or end of file).
// This is intentionally coarse — same as enclosingFuncAt's attribution model —
// and is sufficient to keep binding resolution function-local for the flat,
// one-function-per-handler shapes this pass targets. When `pos` precedes the
// first recorded function, the scope is the whole file.
func mongoAggScopeOf(src string, funcs []funcSpan, pos int) mongoAggScope {
	start := 0
	end := len(src)
	for i, f := range funcs {
		if f.offset > pos {
			// First function declared after pos bounds the scope end.
			end = f.offset
			break
		}
		start = f.offset
		// Tentatively, the scope ends at the next function (updated above if
		// that next function is itself after pos).
		if i+1 < len(funcs) {
			end = funcs[i+1].offset
		} else {
			end = len(src)
		}
	}
	return mongoAggScope{start: start, end: end}
}

// mongoAggFirstArg returns the trimmed text of the first argument of a call,
// given the index of its `(`. It scans to the matching `)` (string- and
// depth-aware) and returns everything up to the first top-level comma or the
// closing paren. Returns "" if the call is unterminated.
func mongoAggFirstArg(src string, openParen int) string {
	if openParen >= len(src) || src[openParen] != '(' {
		return ""
	}
	depthParen := 0
	depthBrace := 0
	depthBracket := 0
	inStr := byte(0)
	start := openParen + 1
	for i := openParen; i < len(src); i++ {
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
		case '\'', '"', '`':
			inStr = c
		case '(':
			depthParen++
		case ')':
			depthParen--
			if depthParen == 0 {
				return strings.TrimSpace(src[start:i])
			}
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case ',':
			if depthParen == 1 && depthBrace == 0 && depthBracket == 0 {
				return strings.TrimSpace(src[start:i])
			}
		}
	}
	return ""
}

// mongoAggIdentRe matches a bare JS identifier (the whole string).
var mongoAggIdentRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// mongoAggBareIdent returns `arg` if it is a single bare identifier (a
// variable reference like `pipeline`), else "". Member accesses, calls, array
// literals, and spreads are rejected.
func mongoAggBareIdent(arg string) string {
	if mongoAggIdentRe.MatchString(arg) {
		return arg
	}
	return ""
}

// mongoAggResolveArrayBinding finds, within the function scope, the binding
// `const|let|var <ident> = [` and returns the byte index of that `[` (in the
// full `src`) so the existing stage splitter can parse it. Returns -1 if there
// is no such binding in scope or its initializer is not an array literal
// (e.g. `const pipeline = stages;` or `= buildPipeline()`), which is the
// honest-unresolved case. The last in-scope binding wins (closest assignment
// before the call is the common single-binding case).
func mongoAggResolveArrayBinding(src string, scope mongoAggScope, ident string) int {
	re := regexp.MustCompile(
		`(?:^|[^\w$])(?:const|let|var)\s+` + regexp.QuoteMeta(ident) + `\s*=\s*\[`,
	)
	region := src[scope.start:scope.end]
	locs := re.FindAllStringIndex(region, -1)
	if len(locs) == 0 {
		return -1
	}
	// Take the last binding; its `[` is the final byte of the match.
	last := locs[len(locs)-1]
	return scope.start + last[1] - 1
}

// mongoAggBuildCallRe detects a `.build()` terminator on the aggregate
// argument — the signature of the builder pattern (ask 3).
var mongoAggBuildCallRe = regexp.MustCompile(`\.build\s*\(\s*\)`)

// mongoAggBuilderVarRe captures the receiver identifier of a `<ident>.build()`
// call when the argument is a bare `builder.build()` (not an inline chain).
var mongoAggBuilderVarRe = regexp.MustCompile(`^([A-Za-z_$][\w$]*)\.build\s*\(\s*\)$`)

// mongoAggResolveBuilderChain returns the fluent construction chain text whose
// `.build()` produces the aggregate argument.
//
//   - Inline one-liner — `new AggBuilder().match({...}).lookup({...}).build()`
//     passed directly: the arg itself is the chain.
//   - Variable form — `builder.build()`: resolve `builder` to its construction
//     statement `const builder = new AggBuilder().match(...)...` within the
//     function scope and return that chain.
//
// Returns "" when the builder variable cannot be resolved to a construction
// chain in the same scope (e.g. it is built in another function or behind a
// branch) — honest-unresolved, no fabrication.
func mongoAggResolveBuilderChain(src string, scope mongoAggScope, arg string) string {
	// Inline chain: the argument already contains the `new ...().build()`.
	if m := mongoAggBuilderVarRe.FindStringSubmatch(arg); m != nil {
		ident := m[1]
		return mongoAggBuilderBindingChain(src, scope, ident)
	}
	// Otherwise the arg is itself a chain ending in .build(); strip the
	// trailing `.build()` and use the rest as the chain.
	return mongoAggStripBuild(arg)
}

// mongoAggBuilderBindingChain finds `const|let|var <ident> = <chain>;` in scope
// and returns the chain text (initializer), or "" if absent. The initializer
// runs from `=` to the statement terminator (`;` or newline) at top level.
func mongoAggBuilderBindingChain(src string, scope mongoAggScope, ident string) string {
	re := regexp.MustCompile(
		`(?:^|[^\w$])(?:const|let|var)\s+` + regexp.QuoteMeta(ident) + `\s*=\s*`,
	)
	region := src[scope.start:scope.end]
	locs := re.FindAllStringIndex(region, -1)
	if len(locs) == 0 {
		return ""
	}
	last := locs[len(locs)-1]
	init := mongoAggInitializer(region, last[1])
	return strings.TrimSpace(init)
}

// mongoAggInitializer returns the right-hand side of an assignment starting at
// `from` up to the top-level statement terminator (`;` or unbraced newline),
// string- and depth-aware so chained calls spanning multiple lines are kept
// whole.
func mongoAggInitializer(region string, from int) string {
	depthParen, depthBrace, depthBracket := 0, 0, 0
	inStr := byte(0)
	for i := from; i < len(region); i++ {
		c := region[i]
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
		case '\'', '"', '`':
			inStr = c
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case ';':
			if depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				return region[from:i]
			}
		case '\n':
			// A newline terminates the statement only at top level AND when we
			// are not mid-chain (next non-space char is not `.`). This lets a
			// fluent chain break across lines while still ending plain stmts.
			if depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				if !mongoAggNextIsDot(region, i+1) {
					return region[from:i]
				}
			}
		}
	}
	return region[from:]
}

// mongoAggNextIsDot reports whether the next non-whitespace character at or
// after `i` is a `.` (chain continuation).
func mongoAggNextIsDot(s string, i int) bool {
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		return c == '.'
	}
	return false
}

// mongoAggStripBuild removes a trailing `.build()` (and any whitespace) from a
// builder chain expression.
func mongoAggStripBuild(chain string) string {
	loc := mongoAggBuildCallRe.FindStringIndex(chain)
	if loc == nil {
		return ""
	}
	return strings.TrimSpace(chain[:loc[0]])
}

// mongoAggBuilderMethodOp maps a fluent builder method name to its MongoDB
// pipeline stage operator. Only methods that correspond 1:1 to a pipeline
// stage are recognised; an unknown method (e.g. `.options(...)`) is skipped so
// it does not appear as a fabricated stage.
func mongoAggBuilderMethodOp(method string) string {
	switch method {
	case "match":
		return "$match"
	case "lookup":
		return "$lookup"
	case "graphLookup":
		return "$graphLookup"
	case "group":
		return "$group"
	case "unwind":
		return "$unwind"
	case "project":
		return "$project"
	case "sort":
		return "$sort"
	case "limit":
		return "$limit"
	case "skip":
		return "$skip"
	case "addFields", "set":
		return "$addFields"
	case "facet":
		return "$facet"
	case "count":
		return "$count"
	case "replaceRoot":
		return "$replaceRoot"
	case "sample":
		return "$sample"
	}
	return ""
}

// mongoAggBuilderStages walks a fluent builder chain (with the trailing
// `.build()` already stripped) and synthesises pipeline-stage substrings in
// `{ $op: <arg> }` form — the SAME shape the inline-array splitter produces —
// so mongoAggEmitStages can extract `$lookup` join fields, `$group` ids, etc.
// uniformly. Each recognised top-level `.method(arg)` becomes one stage, in
// call order. Methods that do not map to a pipeline stage are skipped (not
// fabricated). The scan is string- and depth-aware so a `.method(` token
// appearing INSIDE an argument object (or string) is never mistaken for a
// top-level chained call.
func mongoAggBuilderStages(chain string) []string {
	var stages []string
	inStr := byte(0)
	depthParen, depthBrace, depthBracket := 0, 0, 0
	for i := 0; i < len(chain); i++ {
		c := chain[i]
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
		case '\'', '"', '`':
			inStr = c
			continue
		case '{':
			depthBrace++
			continue
		case '}':
			depthBrace--
			continue
		case '[':
			depthBracket++
			continue
		case ']':
			depthBracket--
			continue
		case '(':
			depthParen++
			continue
		case ')':
			depthParen--
			continue
		}
		// Only consider a `.method(` at the top level of the chain.
		if c != '.' || depthParen != 0 || depthBrace != 0 || depthBracket != 0 {
			continue
		}
		// Parse the method identifier following the dot.
		j := i + 1
		nameStart := j
		for j < len(chain) && isMongoIdentChar(chain[j]) {
			j++
		}
		method := chain[nameStart:j]
		// Skip whitespace to the expected `(`.
		k := j
		for k < len(chain) && (chain[k] == ' ' || chain[k] == '\t' || chain[k] == '\n' || chain[k] == '\r') {
			k++
		}
		if k >= len(chain) || chain[k] != '(' {
			continue
		}
		op := mongoAggBuilderMethodOp(method)
		if op != "" {
			arg := mongoAggFirstArg(chain, k)
			stages = append(stages, "{ "+op+": "+arg+" }")
		}
		// Resume scanning at the method's `(` (k) so the main loop counts its
		// depth via the normal `(`/`)` cases. We jump i forward past the
		// already-consumed identifier+whitespace, but NOT past the paren — that
		// keeps depthParen balanced so a `.method(` token nested inside this
		// argument is correctly guarded out. `i = k-1` because the loop's i++
		// lands on k (the `(`) next iteration.
		i = k - 1
	}
	return stages
}

// isMongoIdentChar reports whether c can appear in a JS identifier.
func isMongoIdentChar(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
