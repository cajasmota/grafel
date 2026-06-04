// MongoDB aggregation-pipeline internal extraction for the official Go driver
// go.mongodb.org/mongo-driver (#3846, epic #3837).
//
// This is the Go sibling of orm_queries_jsts_mongo_agg.go (#3426) and
// orm_queries_python_mongo_agg.go (#3440). The existing Go mongo-driver
// extractor (internal/custom/golang/mongo_driver.go) DETECTS the
// `coll.Aggregate(...)` call as a coarse SCOPE.Operation query node, but it
// treats the pipeline argument as an opaque blob. The pipeline is where the
// data-flow lives: a `$lookup` stage is an implicit cross-collection JOIN the
// migration must reason about (MongoDB has no FK for it), and each stage is a
// distinct transformation worth a node. This pass mirrors the JS/Python
// emission shape EXACTLY so the four siblings share one contract:
//
//  1. JOINS_COLLECTION relationship — for every `$lookup` / `$graphLookup`
//     stage, an edge Class:<aggregating coll> → Class:<from coll>. Properties:
//     local_field, foreign_field, as, stage. This is the highest-value output.
//
//  2. SCOPE.DataAccess pipeline-stage entities — one per stage, anchored at the
//     Aggregate call site, subtype = the stage operator, stage order preserved
//     as stage_index. $group captures `_id` + accumulators; $facet its keys.
//
// GO-DRIVER IDIOMS — the pipeline is built from `bson` documents, in two stage
// syntaxes that this pass handles uniformly:
//
//   - bson.D tuple form (ordered): a stage is `bson.D{{"$lookup", bson.D{...}}}`
//     i.e. `{"key", value}` pairs with NO colon. The classic full-join form is
//     `bson.D{{"$lookup", bson.D{{"from","authors"},{"localField","author_id"},
//     {"foreignField","_id"},{"as","author"}}}}`.
//   - bson.M map form (unordered): a stage is `bson.M{"$lookup": bson.M{...}}`
//     i.e. `"key": value` pairs WITH a colon.
//
// The pipeline container is `mongo.Pipeline{ <stage>, <stage>, ... }` (a
// `[]bson.D`), or a bare `[]bson.D{...}` / `[]bson.M{...}` / `bson.A{...}`
// slice literal. We split it into top-level stage elements, then per stage
// recover the operator (first key) and, for $lookup/$graphLookup, the `from`
// string + the join sub-fields.
//
// AGGREGATING COLLECTION — recovered from the receiver of `.Aggregate(`:
//
//   - `db.Collection("books").Aggregate(...)`     → "books" (inline literal).
//   - `coll.Aggregate(...)` where `coll` is bound one-or-more statements up to
//     `db.Collection("books")` (the dominant idiom) — we follow the nearest
//     same-function binding `coll := db.Collection("books")` / `coll =
//     ...Collection("books")` and recover the literal collection name.
//
// HONEST LIMIT: a dynamic `from` (variable / expression, not a string literal),
// a dynamic collection (`db.Collection(name)` with a non-literal arg, or an
// unresolvable receiver variable), a pipeline passed as a bare variable we
// can't follow to a literal, or a builder-produced pipeline are all left
// UNRESOLVED — we never fabricate a stage or a join we cannot statically see.
// The inline `mongo.Pipeline{...}` literal and the single same-function
// pipeline-variable binding are resolved; everything else is skipped.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoAggGoGateRe gates the scan: only files that import the official Go mongo
// driver are scanned, so we never poach an arbitrary `.Aggregate(` chain
// (e.g. a math/stats helper). Matches the mongo / bson / options subpackages.
var mongoAggGoGateRe = regexp.MustCompile(
	`go\.mongodb\.org/mongo-driver(?:/v\d+)?/(?:mongo|bson)`,
)

// mongoAggGoCallRe locates `.Aggregate(` call sites. The receiver (aggregating
// collection) and the pipeline argument are recovered by scanning, as in the
// JS/Python passes. Go's collection method is upper-camel `Aggregate`.
var mongoAggGoCallRe = regexp.MustCompile(`\.Aggregate\s*\(`)

// mongoAggGoCollLiteralRe recovers an inline `db.Collection("books")` receiver:
// the quoted argument is the collection name.
var mongoAggGoCollLiteralRe = regexp.MustCompile(
	`\.Collection\(\s*"([a-zA-Z_][\w$.]*)"\s*\)\s*$`,
)

// mongoAggGoReceiverIdentRe recovers a bare receiver identifier immediately
// preceding `.Aggregate` (`coll`, `c.books`), capturing the LAST segment.
var mongoAggGoReceiverIdentRe = regexp.MustCompile(`([A-Za-z_]\w*)\s*$`)

// mongoAggGoCollBindReCache caches per-ident binding matchers.
var mongoAggGoCollBindReCache = map[string]*regexp.Regexp{}

// mongoAggGoCollBindRe matches a same-function binding of a collection variable
// `<ident> := <rhs>` or `<ident> = <rhs>` at the start of a (possibly indented)
// line. The RHS is recovered separately and inspected for a
// `.Collection("name")` call.
func mongoAggGoCollBindRe(ident string) *regexp.Regexp {
	if re, ok := mongoAggGoCollBindReCache[ident]; ok {
		return re
	}
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(ident) + `\s*:?=\s*(.+)$`)
	mongoAggGoCollBindReCache[ident] = re
	return re
}

// scanGoMongoAggregation walks `src`, finds Go-driver `.Aggregate(...)` call
// sites, resolves the aggregating collection + the pipeline literal, parses
// each bson stage, and emits stage entities + $lookup/$graphLookup join edges.
func scanGoMongoAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	if !mongoAggGoGateRe.MatchString(src) {
		return
	}

	for _, loc := range mongoAggGoCallRe.FindAllStringIndex(src, -1) {
		openParen := loc[1] - 1 // index of '('
		coll := mongoAggGoResolveReceiver(src, loc[0])
		if coll == "" {
			continue // dynamic / unresolvable receiver — honest skip.
		}

		listLiteral := mongoAggGoResolvePipeline(src, openParen, loc[0])
		if listLiteral == "" {
			continue // dynamic / builder pipeline — honest skip.
		}
		stages := mongoAggGoSplitStages(listLiteral)
		if len(stages) == 0 {
			continue
		}
		caller := enclosingFuncAt(funcs, loc[0])
		callLine := lineOfOffset(src, loc[0])

		for idx, st := range stages {
			op := mongoAggGoStageOp(st)
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

			// Stage entity Name — computed up front so the node-anchored
			// JOINS_COLLECTION twin (#4244) can reference THIS stage entity.
			name := mongoAggStageName(coll, callLine, idx, op)

			switch op {
			case "$lookup":
				lk := mongoAggGoParseLookup(st)
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
					// #4244 — node-anchored twin.
					emitJoin(mongoAggStageJoinEdge(name, path, lang, lk, "lookup"))
				}
			case "$graphLookup":
				lk := mongoAggGoParseLookup(st)
				if lk.from != "" {
					props["from"] = lk.from
					if lk.as != "" {
						props["as"] = lk.as
					}
					emitJoin(mongoAggJoinEdge(coll, lk, "graphLookup"))
					// #4244 — node-anchored twin (graphLookup stage node).
					emitJoin(mongoAggStageJoinEdge(name, path, lang, lk, "graphLookup"))
				}
			case "$group":
				if id, accs := mongoAggGoParseGroup(st); id != "" || accs != "" {
					if id != "" {
						props["group_id"] = id
					}
					if accs != "" {
						props["accumulators"] = accs
					}
				}
			case "$facet":
				if keys := mongoAggGoParseFacetKeys(st); keys != "" {
					props["facets"] = keys
				}
			}

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
}

// mongoAggGoResolveReceiver recovers the aggregating collection name from the
// text immediately preceding the `.Aggregate` token at `dotPos`. It tries, in
// order: an inline `db.Collection("c")` call, then a bare identifier receiver
// followed to its nearest same-function `coll := db.Collection("c")` binding.
// Returns "" when no string-literal collection is recoverable (dynamic arg or
// unresolvable variable) — honest skip.
func mongoAggGoResolveReceiver(src string, dotPos int) string {
	winStart := dotPos - 256
	if winStart < 0 {
		winStart = 0
	}
	window := src[winStart:dotPos]

	// `db.Collection("c").Aggregate(...)` — inline literal receiver.
	if m := mongoAggGoCollLiteralRe.FindStringSubmatch(window); m != nil {
		return m[1]
	}

	// Bare identifier receiver (`coll.Aggregate(...)`): follow its binding.
	m := mongoAggGoReceiverIdentRe.FindStringSubmatch(window)
	if m == nil {
		return ""
	}
	ident := m[1]
	// A bare identifier must be the actual receiver (not the tail of a
	// `.Collection("c")` already handled above, nor a call/subscript). Verify
	// the char before the identifier is not `)`/`"` (would imply we sliced into
	// a literal we already missed).
	start := dotPos - len(ident)
	if start > 0 {
		prev := src[start-1]
		if prev == ')' || prev == '"' || prev == ']' {
			return ""
		}
	}
	return mongoAggGoFollowCollBinding(src, ident, dotPos)
}

// mongoAggGoFollowCollBinding finds the nearest same-function binding of `ident`
// preceding `usePos` whose RHS contains a `.Collection("c")` call, and returns
// the literal collection name. Returns "" when there is no such binding in
// scope — honest skip (a dynamic collection or an out-of-scope handle).
func mongoAggGoFollowCollBinding(src, ident string, usePos int) string {
	funcStart := funcStartBeforeGo(src, usePos)
	re := mongoAggGoCollBindRe(ident)
	bestRHS := ""
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		if m[0] >= usePos {
			break // at/after the use — not a preceding binding.
		}
		if m[0] < funcStart {
			continue // earlier function — out of scope.
		}
		bestRHS = src[m[2]:m[3]] // last preceding binding wins
	}
	if bestRHS == "" {
		return ""
	}
	if cm := mongoAggGoCollLiteralAnyRe.FindStringSubmatch(bestRHS); cm != nil {
		return cm[1]
	}
	return ""
}

// mongoAggGoCollLiteralAnyRe matches a `.Collection("c")` call anywhere in a
// binding's RHS (not anchored to end-of-line), capturing the collection name.
var mongoAggGoCollLiteralAnyRe = regexp.MustCompile(
	`\.Collection\(\s*"([a-zA-Z_][\w$.]*)"`,
)

// funcStartBeforeGo returns the offset of the nearest `func` header preceding
// `pos` — the enclosing-function scope start used to bound binding resolution.
// Returns 0 (file scope) if none precedes `pos`.
func funcStartBeforeGo(src string, pos int) int {
	start := 0
	for _, m := range goOrmFuncRe.FindAllStringIndex(src, -1) {
		if m[0] >= pos {
			break
		}
		start = m[0]
	}
	return start
}

// mongoAggGoResolvePipeline returns the full pipeline list literal `{ ... }`
// (the braces of the slice literal, included) for the Aggregate call whose `(`
// is at `openParen`. The pipeline is the SECOND argument (the first is the
// context): `coll.Aggregate(ctx, mongo.Pipeline{ ... })`. Two forms:
//
//	coll.Aggregate(ctx, mongo.Pipeline{ ... })  — inline literal, parsed in place.
//	coll.Aggregate(ctx, pipeline)               — bare identifier; follow it to
//	                                              its nearest same-function
//	                                              `pipeline := <slice-lit>{ ... }`.
//
// Returns "" for any other pipeline shape (builder call, spread, non-literal
// binding) — honest unresolved.
func mongoAggGoResolvePipeline(src string, openParen, dotPos int) string {
	args := mongoAggGoCallArgs(src, openParen)
	// The pipeline is the last argument; tolerate a single-arg call too (some
	// helpers wrap context). We take the LAST top-level argument as the pipeline.
	if len(args) == 0 {
		return ""
	}
	pipeArg := strings.TrimSpace(args[len(args)-1])
	if pipeArg == "" {
		return ""
	}

	// Form 1: inline slice literal — `mongo.Pipeline{ ... }` / `[]bson.D{ ... }`
	// / `[]bson.M{ ... }` / `bson.A{ ... }`. Find the FIRST `{` and return the
	// balanced brace body (braces included).
	if body := mongoAggGoSliceLiteralBody(pipeArg); body != "" {
		return body
	}

	// Form 2: bare identifier — follow its same-function slice-literal binding.
	if mongoAggGoIdentRe.MatchString(pipeArg) {
		return mongoAggGoFollowPipelineBinding(src, pipeArg, dotPos)
	}
	return "" // builder / expression — honest unresolved.
}

// mongoAggGoIdentRe matches a bare Go identifier (the whole string).
var mongoAggGoIdentRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)

// mongoAggGoSliceLiteralBody recognises a pipeline slice-literal expression and
// returns its balanced `{ ... }` body (braces included). It accepts the known
// pipeline container prefixes (`mongo.Pipeline`, `[]bson.D`, `[]bson.M`,
// `bson.A`, `[]interface{}`, `[]any`) so we don't treat an arbitrary `T{...}`
// as a pipeline. Returns "" if the expression doesn't open one of those
// containers or the braces are unbalanced.
func mongoAggGoSliceLiteralBody(expr string) string {
	expr = strings.TrimSpace(expr)
	brace := strings.IndexByte(expr, '{')
	if brace < 0 {
		return ""
	}
	prefix := strings.TrimSpace(expr[:brace])
	if !mongoAggGoIsPipelineContainer(prefix) {
		return ""
	}
	return mongoAggGoBraceBody(expr, brace)
}

// mongoAggGoIsPipelineContainer reports whether `prefix` (the text before the
// opening `{` of a composite literal) names a recognised pipeline container.
// `[]interface{}` / `[]any` carry their own braces, handled by suffix match.
func mongoAggGoIsPipelineContainer(prefix string) bool {
	switch prefix {
	case "mongo.Pipeline", "[]bson.D", "[]bson.M", "bson.A", "[]bson.E":
		return true
	}
	// `[]interface` (the `{}` of interface{} is consumed as the brace) and
	// `[]any` slice-of-document forms.
	if strings.HasSuffix(prefix, "[]interface") || prefix == "[]any" {
		return true
	}
	return false
}

// mongoAggGoFollowPipelineBinding finds the nearest same-function binding
// `ident := <slice-lit>{ ... }` / `ident = <slice-lit>{ ... }` preceding
// `usePos` and returns its balanced `{ ... }` body. Returns "" when no
// slice-literal binding is in scope — honest unresolved.
func mongoAggGoFollowPipelineBinding(src, ident string, usePos int) string {
	funcStart := funcStartBeforeGo(src, usePos)
	re := mongoAggGoCollBindRe(ident) // reuse the `ident :?= (.+)` matcher
	bestRHS := ""
	bestPos := -1
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		if m[0] >= usePos {
			break
		}
		if m[0] < funcStart {
			continue
		}
		bestRHS = src[m[2]:m[3]]
		bestPos = m[2]
	}
	if bestRHS == "" || bestPos < 0 {
		return ""
	}
	// The single-line RHS capture may be truncated mid-literal for a multi-line
	// pipeline (`pipeline := mongo.Pipeline{` then stages on following lines).
	// Resolve from the literal's opening brace in the FULL src so the balanced
	// scan spans newlines.
	trimmed := strings.TrimSpace(bestRHS)
	brace := strings.IndexByte(trimmed, '{')
	if brace < 0 {
		return ""
	}
	if !mongoAggGoIsPipelineContainer(strings.TrimSpace(trimmed[:brace])) {
		return ""
	}
	// Map the brace offset back into src: bestPos is where the RHS starts in
	// src; find the first '{' from there.
	absBrace := strings.IndexByte(src[bestPos:], '{')
	if absBrace < 0 {
		return ""
	}
	return mongoAggGoBraceBody(src, bestPos+absBrace)
}

// mongoAggGoCallArgs returns the top-level argument substrings of a call given
// the index of its `(`. String- and depth-aware (handles `"` and backtick
// strings, nested {}/[]/() ). Splits only on commas at paren-depth 1.
func mongoAggGoCallArgs(src string, openParen int) []string {
	if openParen >= len(src) || src[openParen] != '(' {
		return nil
	}
	var args []string
	depthParen, depthBrace, depthBracket := 0, 0, 0
	inStr := byte(0)
	start := openParen + 1
	for i := openParen; i < len(src); i++ {
		c := src[i]
		if inStr != 0 {
			if c == '\\' && inStr != '`' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '`':
			inStr = c
		case '(':
			depthParen++
		case ')':
			depthParen--
			if depthParen == 0 {
				if seg := strings.TrimSpace(src[start:i]); seg != "" || len(args) > 0 {
					args = append(args, src[start:i])
				}
				return args
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
				args = append(args, src[start:i])
				start = i + 1
			}
		}
	}
	return args
}

// mongoAggGoBraceBody returns the balanced `{ ... }` literal (braces included)
// whose opening `{` is at `open`, string- and depth-aware. Returns "" if the
// brace is unbalanced.
func mongoAggGoBraceBody(src string, open int) string {
	if open >= len(src) || src[open] != '{' {
		return ""
	}
	depth := 0
	inStr := byte(0)
	for i := open; i < len(src); i++ {
		c := src[i]
		if inStr != 0 {
			if c == '\\' && inStr != '`' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '`':
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

// mongoAggGoSplitStages splits a pipeline slice-literal body `{ <stage>, ... }`
// into its top-level stage element substrings. Each element is a `bson.D{...}`
// / `bson.M{...}` / `bson.E{...}` stage document. String- and depth-aware: it
// splits only on commas at brace-depth 1 (the slice level), so nested stage
// documents, nested arrays, and quoted commas don't break the split. The outer
// `mongo.Pipeline{` / `[]bson.D{` prefix has already been stripped by passing
// the brace body; we re-find the outer `{` here to anchor depth.
func mongoAggGoSplitStages(body string) []string {
	// body is `{ ... }`; scan from the first '{'.
	open := strings.IndexByte(body, '{')
	if open < 0 {
		return nil
	}
	var stages []string
	depthBrace := 0
	depthBracket := 0
	depthParen := 0
	inStr := byte(0)
	segStart := -1
	flush := func(end int) {
		if segStart < 0 {
			return
		}
		if seg := strings.TrimSpace(body[segStart:end]); seg != "" {
			stages = append(stages, seg)
		}
		segStart = -1
	}
	for i := open; i < len(body); i++ {
		c := body[i]
		if inStr != 0 {
			if c == '\\' && inStr != '`' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '`':
			inStr = c
		case '{':
			depthBrace++
			if depthBrace == 1 {
				segStart = i + 1 // opening of the slice literal itself
			}
		case '}':
			depthBrace--
			if depthBrace == 0 {
				flush(i)
				return stages
			}
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case '(':
			depthParen++
		case ')':
			depthParen--
		case ',':
			if depthBrace == 1 && depthBracket == 0 && depthParen == 0 {
				flush(i)
				segStart = i + 1
			}
		}
	}
	return stages
}

// mongoAggGoStageOp returns the pipeline-stage operator (the first key) of a
// bson stage document, handling BOTH go-driver forms:
//
//	bson.D{{"$lookup", ...}}   — tuple form: the first key is the first string
//	                             literal inside the first inner `{...}` pair.
//	bson.M{"$lookup": ...}     — map form: the first `"key":` before a colon.
//
// Returns "" if no `$`-prefixed operator key is found.
func mongoAggGoStageOp(stage string) string {
	// The stage operator is the FIRST `$`-prefixed quoted key in the document,
	// in either form: tuple `bson.D{{"$group", ...}}` (no colon) or map
	// `bson.M{"$match": ...}` (colon). A nested accumulator like `"$sum":`
	// appears LATER in the string, so taking the earliest `"$..."` literal is
	// correct for both forms and never grabs a nested operator.
	if m := mongoAggGoFirstDollarStrRe.FindStringSubmatch(stage); m != nil {
		return m[1]
	}
	return ""
}

// mongoAggGoFirstDollarStrRe matches the first `"$op"` string literal in a bson
// stage document (works for both tuple and map forms — the stage operator is
// always the first dollar-prefixed quoted key).
var mongoAggGoFirstDollarStrRe = regexp.MustCompile(`"(\$[A-Za-z][\w$]*)"`)

// mongoAggGoParseLookup extracts the join fields from a $lookup / $graphLookup
// stage, handling both bson.D (tuple) and bson.M (map) forms. The classic full
// join carries from/localField/foreignField/as; the sub-pipeline form carries
// from + as. String values must be plain string literals (a dynamic/expression
// value is honestly left empty).
func mongoAggGoParseLookup(stage string) mongoAggLookup {
	return mongoAggLookup{
		from:         mongoAggGoStringField(stage, "from"),
		localField:   mongoAggGoStringField(stage, "localField"),
		foreignField: mongoAggGoStringField(stage, "foreignField"),
		as:           mongoAggGoStringField(stage, "as"),
	}
}

// mongoAggGoStringFieldReCache caches per-key field matchers.
var mongoAggGoStringFieldReCache = map[string]*regexp.Regexp{}

// mongoAggGoStringField pulls the plain string-literal value bound to `key` in a
// bson stage, in either form:
//
//	bson.D{{"from", "authors"}}  — tuple: `"from", "authors"` (comma separator).
//	bson.M{"from": "authors"}    — map:   `"from": "authors"` (colon separator).
//
// Returns "" when the key is absent or its value is not a plain `"..."` literal
// (e.g. a variable, `collName`, or an expression) — honest: a dynamic `from`
// yields no join edge.
func mongoAggGoStringField(stage, key string) string {
	re, ok := mongoAggGoStringFieldReCache[key]
	if !ok {
		re = regexp.MustCompile(
			`"` + regexp.QuoteMeta(key) + `"\s*[,:]\s*"([a-zA-Z_][\w$.]*)"`,
		)
		mongoAggGoStringFieldReCache[key] = re
	}
	if m := re.FindStringSubmatch(stage); m != nil {
		return m[1]
	}
	return ""
}

// mongoAggGoParseGroup extracts the `_id` expression text and the accumulator
// field names from a $group stage, in either bson form. The `_id` value is the
// value bound to the `_id` key; accumulators are the OTHER top-level keys of the
// group document. Returns (idText, "field1,field2,...").
func mongoAggGoParseGroup(stage string) (idText string, accumulators string) {
	body := mongoAggGoStageOperatorBody(stage, "$group")
	if body == "" {
		return "", ""
	}
	keys := mongoAggGoTopLevelFields(body)
	var accs []string
	for _, kv := range keys {
		if kv.key == "_id" {
			idText = mongoAggCollapseWS(strings.TrimSpace(kv.val))
			continue
		}
		accs = append(accs, kv.key)
	}
	return idText, strings.Join(accs, ",")
}

// mongoAggGoParseFacetKeys returns the comma-joined named sub-pipeline keys of a
// $facet stage (e.g. "byStatus,byMonth").
func mongoAggGoParseFacetKeys(stage string) string {
	body := mongoAggGoStageOperatorBody(stage, "$facet")
	if body == "" {
		return ""
	}
	var names []string
	for _, kv := range mongoAggGoTopLevelFields(body) {
		names = append(names, kv.key)
	}
	return strings.Join(names, ",")
}

// mongoAggGoStageOperatorBody returns the inner document body bound to the stage
// operator `op` (e.g. `$group`), handling both forms. For `bson.D{{"$group",
// bson.D{ <body> }}}` it returns the `<body>` between the inner braces; for
// `bson.M{"$group": bson.M{ <body> }}` likewise. String- and depth-aware.
func mongoAggGoStageOperatorBody(stage, op string) string {
	idx := strings.Index(stage, `"`+op+`"`)
	if idx < 0 {
		return ""
	}
	// Find the first `{` after the operator key — that opens the operator's
	// value document (bson.D{...} / bson.M{...}). Skip the bson type name.
	i := idx + len(op) + 2 // past `"op"`
	brace := strings.IndexByte(stage[i:], '{')
	if brace < 0 {
		return ""
	}
	open := i + brace
	full := mongoAggGoBraceBody(stage, open)
	if len(full) < 2 {
		return ""
	}
	return full[1 : len(full)-1] // strip outer braces
}

// mongoAggGoTopLevelFields splits a bson document body into its top-level
// key/value pairs, handling BOTH the tuple form (`{"k", v}, {"k2", v2}`) and the
// map form (`"k": v, "k2": v2`). Keys are the quoted string literals at the top
// nesting level; values run to the next top-level separator. We detect the form
// by whether the first non-space char is `{` (tuple) vs a quote (map).
func mongoAggGoTopLevelFields(body string) []mongoAggKV {
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		return mongoAggGoExtractTupleFields(body)
	}
	return mongoAggGoExtractMapFields(body)
}

// mongoAggGoExtractTupleFields handles bson.D tuple bodies: a sequence of
// `{"key", value}` pairs at the top level. Each top-level `{...}` is one pair;
// inside it the first quoted string is the key and the remainder (after the
// first top-level comma) is the value.
func mongoAggGoExtractTupleFields(body string) []mongoAggKV {
	var out []mongoAggKV
	depth := 0
	inStr := byte(0)
	pairStart := -1
	for i := 0; i < len(body); i++ {
		c := body[i]
		if inStr != 0 {
			if c == '\\' && inStr != '`' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '`':
			inStr = c
		case '{':
			depth++
			if depth == 1 {
				pairStart = i + 1
			}
		case '}':
			if depth == 1 && pairStart >= 0 {
				if kv := mongoAggGoParseTuplePair(body[pairStart:i]); kv.key != "" {
					out = append(out, kv)
				}
				pairStart = -1
			}
			depth--
		}
	}
	return out
}

// mongoAggGoParseTuplePair parses the inside of a tuple pair `"key", value`
// (braces already stripped): the first quoted string is the key, everything
// after the first top-level comma is the value.
func mongoAggGoParseTuplePair(inner string) mongoAggKV {
	inStr := byte(0)
	keyStart, keyEnd := -1, -1
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if inStr != 0 {
			if c == '\\' && inStr != '`' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
				keyEnd = i
				// Find the comma after the key, value is the rest.
				j := i + 1
				for j < len(inner) && (inner[j] == ' ' || inner[j] == '\t' || inner[j] == '\n' || inner[j] == '\r') {
					j++
				}
				if j < len(inner) && inner[j] == ',' {
					return mongoAggKV{
						key: inner[keyStart:keyEnd],
						val: strings.TrimSpace(inner[j+1:]),
					}
				}
				return mongoAggKV{key: inner[keyStart:keyEnd]}
			}
			continue
		}
		if c == '"' || c == '`' {
			inStr = c
			keyStart = i + 1
		}
	}
	return mongoAggKV{}
}

// mongoAggGoExtractMapFields handles bson.M map bodies: `"key": value` pairs at
// the top level separated by commas. Reuses the same depth-aware value scan as
// the JS pass's top-level-keys helper, but keys here are always quoted strings.
func mongoAggGoExtractMapFields(body string) []mongoAggKV {
	var out []mongoAggKV
	i := 0
	n := len(body)
	for i < n {
		// Skip separators/whitespace.
		for i < n && (body[i] == ',' || body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}
		// Key must be a quoted string.
		if body[i] != '"' && body[i] != '`' {
			// Not a recognisable map key — skip to next comma at top level.
			i = mongoAggGoSkipToTopComma(body, i)
			continue
		}
		q := body[i]
		i++
		keyStart := i
		for i < n && body[i] != q {
			if body[i] == '\\' && q != '`' {
				i++
			}
			i++
		}
		key := body[keyStart:i]
		if i < n {
			i++ // past closing quote
		}
		// Skip to ':'.
		for i < n && body[i] != ':' {
			i++
		}
		if i >= n {
			break
		}
		i++ // past ':'
		valStart := i
		i = mongoAggGoSkipToTopComma(body, i)
		val := strings.TrimSpace(body[valStart:i])
		if key != "" {
			out = append(out, mongoAggKV{key: key, val: val})
		}
	}
	return out
}

// mongoAggGoSkipToTopComma returns the index of the next top-level comma at or
// after `i` (or len(body)), string- and depth-aware.
func mongoAggGoSkipToTopComma(body string, i int) int {
	n := len(body)
	depthBrace, depthBracket, depthParen := 0, 0, 0
	inStr := byte(0)
	for ; i < n; i++ {
		c := body[i]
		if inStr != 0 {
			if c == '\\' && inStr != '`' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '`':
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
				return i
			}
		}
	}
	return n
}
