// Functional / fluent Mongo aggregation-builder extraction for JS/TS (#4320).
//
// orm_queries_jsts_mongo_agg.go already extracts data-access semantics from two
// builder shapes that terminate at a `.aggregate(...)` call site:
//
//   - inline pipeline array — `Model.aggregate([ {$lookup...}, ... ])`
//   - `new …AggregationBuilder().match(...).lookup(...).build()` passed to
//     `.aggregate(...)`.
//
// Both are gated on the `.aggregate(` token (mongoAggCallRe). But a large,
// idiomatic NestJS pattern never reaches `.aggregate(...)` in the same file:
// a FUNCTIONAL FACTORY builder whose pipeline functions RETURN the builder for
// a downstream `model.aggregate(pipeline.build())` executed elsewhere. The live
// upvate-v3 case (#4320) is `src/common/query/mongo/aggregation.builder.ts`:
//
//	export function mongo<Doc>(): AggregationBuilder<Doc> {
//	  return AggregationBuilder.create<Doc>();
//	}
//
// and its consumers, e.g. me-page-retrieve.pipeline.ts:
//
//	return mongo<MePageVersionSource>()
//	  .matchId(new Types.ObjectId(opts.versionId))
//	  .lookupOne({ from: 'me_pages', localField: 'page_id', foreignField: '_id', as: 'page' })
//	  .project({ ... })
//	  .limit(1);
//
// The old scanner produced NO SCOPE.DataAccess node and NO JOINS_COLLECTION
// edge for this file — flows showed generic `Function` steps and the terminal
// `AggregationBuilder` was mis-kinded `Render`. This pass closes that gap.
//
// RECOGNITION (generalises beyond Mongo): a "fluent query-builder chain" is a
// FACTORY entry (`mongo<…>()`, `mongo()`, `AggregationBuilder.create<…>()`,
// `AggregationBuilder.create()`) followed by a chain of `.method(arg)` calls.
// We map each chained method to a data-access stage operator and, for the
// lookup family, pull the joined collection out of the `from:` object property
// (the functional builder passes `{ from: '<collection>', ... }`, not a
// positional string). The chain is parsed from the factory call to the end of
// the enclosing statement (string/depth-aware), independent of whether a
// `.aggregate(...)` ever appears in the file.
//
// HONEST LIMITS:
//   - When the `from:` value is not a static string literal (a variable, an
//     enum, a runtime expression) no JOINS_COLLECTION edge is fabricated; the
//     DataAccess stage is still emitted (subtype $lookup) so the topology is
//     present and the join target is left honestly unresolved.
//   - The receiver collection (the "left" side of the join) is the functional
//     builder's enclosing pipeline function — there is no statically-bound base
//     Mongo collection at the factory call. The high-value output is the join
//     `to` side and the per-stage data-access topology; the collection-anchored
//     edge uses the pipeline name as a stable anchor.
//
// GENERALISATION / FOLLOW-UPS: the factory→stage recognition is structured so
// sibling fluent builders (TypeORM QueryBuilder, Prisma, Mongoose
// `.aggregate()`/`.lookup()`, Knex, SQLAlchemy Core) can reuse the same
// chain-walk by supplying their own factory regex + method→op table. Those are
// filed as #4334 children (see the PR body).
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoFuncBuilderFactoryRe matches the FUNCTIONAL factory entry points of the
// fluent aggregation builder, immediately before the chained `.method(...)`
// calls begin:
//
//	mongo<Doc>()                     mongo()
//	AggregationBuilder.create<Doc>() AggregationBuilder.create()
//	<X>AggregationBuilder.create()   <X>AggBuilder.create()
//
// The trailing `()` (with optional `<…>` type args) and a following `.` is
// asserted by the caller via the chain walk, not the regex, so multi-line
// chains are handled. The match END is positioned right after the factory's
// closing `)` so the chain walk starts at the first chained `.`.
// The optional `<…>` type-argument segment allows ONE level of nested angle
// brackets so generic args like `mongo<Record<string, unknown>>()` match (the
// live consumers use both the flat `mongo<MePageVersionSource>()` and the nested
// `mongo<Record<string, unknown>>()` forms).
var mongoFuncBuilderFactoryRe = regexp.MustCompile(
	`\b(?:mongo|[\w$]*(?:Agg|Aggregation)Builder\s*\.\s*create)\s*(?:<(?:[^<>()]|<[^<>()]*>)*>)?\s*\(\s*\)`,
)

// mongoFuncBuilderMethodOp maps a FUNCTIONAL aggregation-builder method name to
// its MongoDB pipeline stage operator. It is a superset of
// mongoAggBuilderMethodOp: it adds the functional builder's higher-level method
// names (matchId/matchExpr/lookupOne/lookupPipeline/lookupJoinOne/…/shape/pick/
// rename/groupBy/…). Methods that do not correspond to a data-access stage
// (thru/build/rawBuild/rawStages) return "" and are skipped — never fabricated.
//
// The boolean second return reports whether the method is in the LOOKUP family
// (its argument carries a `from:` joined-collection property).
func mongoFuncBuilderMethodOp(method string) (op string, isLookup bool) {
	switch method {
	// --- lookup family: arg is an object with `from: '<collection>'` ----------
	case "lookup", "lookupOne", "lookupPipeline", "lookupPipelineOne",
		"lookupJoinOne", "lookupJoinMany":
		return "$lookup", true
	case "graphLookup":
		return "$graphLookup", true
	// --- match family ----------------------------------------------------------
	case "match", "matchAst", "matchExpr", "matchEqVar", "matchInVar",
		"matchEqVars", "matchIf", "matchId", "matchInIf":
		return "$match", false
	// --- projection / shaping --------------------------------------------------
	case "project", "pick", "omit", "rename", "compute", "shape", "groupDistinct":
		return "$project", false
	case "set", "addFields":
		return "$addFields", false
	case "unset":
		return "$unset", false
	// --- grouping --------------------------------------------------------------
	case "group", "groupBy":
		return "$group", false
	// --- reshaping / ordering / paging ----------------------------------------
	case "replaceRoot":
		return "$replaceRoot", false
	case "unwind":
		return "$unwind", false
	case "sort":
		return "$sort", false
	case "skip":
		return "$skip", false
	case "limit":
		return "$limit", false
	case "count":
		return "$count", false
	case "facet", "facetPaginate", "pagedFacet":
		return "$facet", false
	}
	return "", false
}

// scanJSMongoFuncBuilder finds FUNCTIONAL aggregation-builder factory chains
// (`mongo<…>().…` / `AggregationBuilder.create<…>().…`) and emits per-stage
// SCOPE.DataAccess entities + JOINS_COLLECTION edges, reusing mongoAggEmitStages
// so the output contract is identical to the `.aggregate(...)` paths. It runs as
// a SIBLING to scanJSMongoAggregation and never re-emits the aggregate QUERIES
// edge. Pipelines that DO terminate at `.aggregate(builder.build())` in the same
// file are still handled by scanJSMongoAggregation; this pass targets the case
// where the chain is RETURNED / assigned without a co-located `.aggregate(...)`.
func scanJSMongoFuncBuilder(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	// Gate: only run where the functional builder surface is plausible. The
	// factory token itself is the signal; this keeps us off arbitrary `mongo`
	// identifiers that are not the builder factory (the chain walk additionally
	// requires at least one recognised stage method, so a bare `mongo()` with no
	// chain emits nothing).
	for _, loc := range mongoFuncBuilderFactoryRe.FindAllStringIndex(src, -1) {
		chainStart := loc[1] // just past the factory's `)`
		chain := mongoFuncBuilderChain(src, chainStart)
		if chain == "" {
			continue
		}
		stages := mongoFuncBuilderStages(chain)
		if len(stages) == 0 {
			continue
		}
		caller := enclosingFuncAt(funcs, loc[0])
		callLine := lineOfOffset(src, loc[0])
		// Anchor the collection-side of the topology at the enclosing pipeline
		// function — there is no statically-bound base collection at a functional
		// factory call. The join `to` side (the real value) is resolved from the
		// stage `from:` property by mongoAggEmitStages. Fall back to a stable
		// literal when the factory call is at file scope.
		anchor := caller
		if anchor == "" {
			anchor = "AggregationPipeline"
		}
		mongoAggEmitStages(stages, anchor, caller, callLine, path, lang,
			emitStage, emitJoin)
	}
}

// mongoFuncBuilderChain captures the fluent `.method(...)` chain text beginning
// at `from` (the byte just after the factory's closing `)`), up to the end of
// the enclosing statement. It is string- and depth-aware so a chain spanning
// multiple lines (the idiomatic formatting) is kept whole, and a top-level `;`
// or a non-continuing newline terminates it. The leading content before the
// first `.` is skipped; "" is returned when no chained method follows.
func mongoFuncBuilderChain(src string, from int) string {
	// Require a chained `.` to follow (allowing whitespace). A factory call with
	// no chain (`return mongo();`) is not a pipeline.
	i := from
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\r' || src[i] == '\n') {
		i++
	}
	if i >= len(src) || src[i] != '.' {
		return ""
	}
	// Reuse the statement-terminator scan from the .aggregate builder path so the
	// multi-line/`;`/newline semantics stay identical.
	return strings.TrimSpace(mongoAggInitializer(src, i))
}

// mongoFuncBuilderStages walks a functional builder chain and synthesises
// pipeline-stage substrings in `{ $op: <arg> }` form — the SAME shape the inline
// splitter and the `new …Builder()` walker produce — so mongoAggEmitStages can
// uniformly extract `$lookup` join fields (the lookup family already passes a
// `{ from: '…', localField, foreignField, as }` object literal that
// mongoAggParseLookup reads directly), `$group` ids, `$facet` keys, etc.
//
// It differs from mongoAggBuilderStages only in the method→op table
// (mongoFuncBuilderMethodOp, a superset covering the higher-level functional
// methods). The scan is string- and depth-aware so a `.method(` token inside an
// argument object/string is never mistaken for a top-level chained call.
func mongoFuncBuilderStages(chain string) []string {
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
		op, isLookup := mongoFuncBuilderMethodOp(method)
		if op != "" {
			arg := mongoAggFirstArg(chain, k)
			// The lookup family passes the join spec object directly
			// (`lookupOne({ from: 'me_pages', ... })`). mongoAggParseLookup reads
			// `from:`/`localField:`/`foreignField:`/`as:` straight out of that
			// object, so wrapping it as `{ $lookup: <obj> }` is sufficient. For
			// the lookupJoinOne/Many helpers the collection is the FIRST positional
			// string arg (`lookupJoinOne('m_devices', { on, as })`); normalise that
			// into a `{ from: '…' }` object so the same parser resolves it.
			if isLookup {
				arg = mongoFuncBuilderNormaliseLookupArg(arg)
			}
			stages = append(stages, "{ "+op+": "+arg+" }")
		}
		i = k - 1
	}
	return stages
}

// mongoFuncBuilderLeadingStringRe matches a leading string-literal positional
// argument (single/double/backtick quoted) of a lookup-family helper whose
// collection is passed positionally rather than as a `from:` property — e.g.
// `lookupJoinOne('m_devices', { on: [...], as: 'device' })`.
var mongoFuncBuilderLeadingStringRe = regexp.MustCompile(
	`^\s*['"` + "`" + `]([a-zA-Z_$][\w$.]*)['"` + "`" + `]`,
)

// mongoFuncBuilderNormaliseLookupArg ensures a lookup-family argument is in a
// shape mongoAggParseLookup can read its `from` from. When the argument already
// contains a `from:` object property (the lookup/lookupOne/lookupPipeline form),
// it is returned unchanged. When the collection is the FIRST positional string
// (the lookupJoinOne/lookupJoinMany form), it is rewritten to a synthetic
// `{ from: '<coll>' }` object so the join `to` side resolves. Non-static
// collections (variable/enum first arg) are left unchanged — honestly
// unresolved, no fabricated join.
func mongoFuncBuilderNormaliseLookupArg(arg string) string {
	if mongoAggStringField("{ "+arg+" }", "from") != "" {
		return arg
	}
	if m := mongoFuncBuilderLeadingStringRe.FindStringSubmatch(arg); m != nil {
		return "{ from: '" + m[1] + "' }"
	}
	return arg
}
