// MongoDB aggregation-pipeline internal extraction for Python pymongo / motor
// (#3440, asks 1+2).
//
// This is the Python sibling of orm_queries_jsts_mongo_agg.go (#3430). The
// existing Python ORM scanner (orm_queries_python.go) only recognises the
// Django `Model.objects.aggregate(...)` verb; it does NOT understand the
// pymongo driver idiom `<collection>.aggregate(<pipeline>)`, and it treats the
// pipeline argument as an opaque blob. The pipeline is where the data-flow
// lives: a `$lookup` stage is an implicit cross-collection JOIN the migration
// must reason about (the legacy Django backend is the parity oracle — 151
// `$lookup` stages), and each stage is a distinct transformation worth a node.
//
// This pass mirrors the JS emission shape exactly:
//
//  1. JOINS_COLLECTION relationship — for every `$lookup` / `$graphLookup`
//     stage, an edge from the aggregating collection → the `from` collection.
//     Properties: local_field, foreign_field, as, stage. This is the
//     highest-value output: the application-side join Mongo has no FK for.
//
//  2. SCOPE.DataAccess pipeline-stage entities — one per stage, anchored at the
//     aggregate call site, subtype = the stage operator, stage order preserved
//     as stage_index. $group captures `_id` + accumulators; $facet its keys.
//
// SCOPE — two pipeline forms are resolved:
//
//   - INLINE list literal: `coll.aggregate([ {"$lookup": {...}}, ... ])`.
//   - VARIABLE-BOUND (the important case — ~100% of real legacy usage):
//     `pipeline = [ {"$lookup": {...}}, ... ]` one statement up, then
//     `coll.aggregate(pipeline)`. We follow the identifier to its
//     same-function assignment whose RHS is a list literal (single-binding
//     follow). If the binding isn't a literal list in the same scope, we leave
//     it unresolved — honest.
//
// The aggregating collection is recovered from pymongo idioms: `db.coll`,
// `db["coll"]`, `client.db.coll`, `get_collection("coll")`, or a bare
// `*_cls` / Collection variable name.
//
// HONEST LIMIT: builder/`.build()`-produced pipelines and cross-function
// pipelines stay unresolved (that is ask 3, a separate PR). We never fabricate
// stages or joins we cannot statically see.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoAggPyGate signals whether a pymongo/motor surface is plausible in the
// file, so we don't scan arbitrary `.aggregate(` chains (e.g. pandas).
var mongoAggPyGateRe = regexp.MustCompile(
	`\b(?:pymongo|motor|MongoClient|AsyncIOMotorClient|get_collection|get_database)\b`,
)

// mongoAggPyCallRe locates `.aggregate(` call sites. The receiver and the
// first argument are recovered by scanning, as in the JS pass.
var mongoAggPyCallRe = regexp.MustCompile(`\.aggregate\s*\(`)

// mongoAggPyReceiverRe recovers the aggregating collection token immediately
// preceding `.aggregate`. Captures the LAST dotted segment or a bracket key:
//
//	db.orders.aggregate(...)         → "orders"
//	client.db.m_devices.aggregate(.) → "m_devices"
//	orders_cls.aggregate(...)        → "orders_cls"
//
// `db["coll"]` and `get_collection("coll")` are handled separately because the
// collection name is a quoted argument, not an identifier segment.
var mongoAggPyReceiverRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*$`)

// mongoAggPyBracketRecvRe recovers a `db["coll"]` / `db['coll']` subscript
// receiver: the quoted key is the collection name.
var mongoAggPyBracketRecvRe = regexp.MustCompile(
	`\[\s*['"]([a-zA-Z_][\w$.]*)['"]\s*\]\s*$`,
)

// mongoAggPyGetCollRe recovers a `get_collection("coll")` / `db.get_collection('coll')`
// receiver: the quoted first argument is the collection name.
var mongoAggPyGetCollRe = regexp.MustCompile(
	`get_collection\(\s*['"]([a-zA-Z_][\w$.]*)['"]`,
)

// scanPythonMongoAggregation walks `src`, finds pymongo `.aggregate(...)` call
// sites, resolves the pipeline (inline list literal OR a single same-function
// variable binding), parses each stage, and emits stage entities + join edges.
func scanPythonMongoAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	if !mongoAggPyGateRe.MatchString(src) {
		return
	}

	for _, loc := range mongoAggPyCallRe.FindAllStringIndex(src, -1) {
		openParen := loc[1] - 1 // index of '('
		coll := mongoAggPyResolveReceiver(src, loc[0])
		if coll == "" {
			continue
		}

		// Resolve the pipeline list literal (the full `[ ... ]`, brackets
		// included, so mongoAggSplitStages can scan it) for either form.
		listLiteral := mongoAggPyResolvePipeline(src, openParen, loc[0])
		if listLiteral == "" {
			continue // dynamic / builder pipeline — honest skip.
		}
		stages := mongoAggSplitStages(listLiteral, 0)
		if len(stages) == 0 {
			continue
		}
		caller := enclosingFuncAt(funcs, loc[0])
		callLine := lineOfOffset(src, loc[0])

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
}

// mongoAggPyResolveReceiver recovers the aggregating collection name from the
// text immediately preceding the `.aggregate` token at `dotPos`. It tries, in
// order: a `db["coll"]` subscript, a `get_collection("coll")` call, then the
// last dotted/bare identifier segment (`db.orders` → "orders", `orders_cls` →
// "orders_cls"). Returns "" when nothing recognisable precedes the call.
func mongoAggPyResolveReceiver(src string, dotPos int) string {
	winStart := dotPos - 200
	if winStart < 0 {
		winStart = 0
	}
	window := src[winStart:dotPos]

	// `db["coll"].aggregate(...)` / `db['coll'].aggregate(...)`.
	if m := mongoAggPyBracketRecvRe.FindStringSubmatch(window); m != nil {
		return m[1]
	}
	// `...get_collection("coll").aggregate(...)` — must end at dotPos.
	if m := mongoAggPyGetCollRe.FindStringSubmatchIndex(window); m != nil {
		// Ensure this get_collection(...) is the immediate receiver: the only
		// thing between its close-paren and dotPos is whitespace.
		tail := strings.TrimSpace(window[m[1]:])
		if tail == "" || strings.HasPrefix(tail, ")") {
			return window[m[2]:m[3]]
		}
	}
	// `db.orders.aggregate(...)` / `orders_cls.aggregate(...)` — last segment.
	if m := mongoAggPyReceiverRe.FindStringSubmatch(window); m != nil {
		seg := m[1]
		// Skip obvious non-collection keywords.
		if seg == "self" || seg == "cls" {
			return ""
		}
		return seg
	}
	return ""
}

// mongoAggPyResolvePipeline returns the full pipeline list literal `[ ... ]`
// (brackets included) for the aggregate call whose `(` is at `openParen`. Two
// forms:
//
//	coll.aggregate([ ... ])        — inline literal, parsed in place.
//	coll.aggregate(pipeline)       — single identifier; follow it to its
//	                                 nearest preceding same-function assignment
//	                                 `pipeline = [ ... ]` and return that body.
//
// Returns "" for any other first-argument shape (builder var, call expr,
// kwargs, non-literal binding) — honest unresolved.
func mongoAggPyResolvePipeline(src string, openParen, dotPos int) string {
	// Skip whitespace after '('.
	i := openParen + 1
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if i >= len(src) {
		return ""
	}

	// Form 1: inline list literal.
	if src[i] == '[' {
		return mongoAggPyBracketBody(src, i)
	}

	// Form 2: a bare identifier argument (`pipeline`) → follow the binding.
	if isPyIdentStart(src[i]) {
		idStart := i
		for i < len(src) && isPyIdentChar(src[i]) {
			i++
		}
		ident := src[idStart:i]
		// The arg must be JUST the identifier: next non-space char is `)`.
		j := i
		for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
			j++
		}
		if j >= len(src) || src[j] != ')' {
			return "" // e.g. `pipeline + extra`, `build()`, kwargs — unresolved.
		}
		return mongoAggPyFollowBinding(src, ident, dotPos, funcStartBefore(src, dotPos))
	}
	return ""
}

// mongoAggPyBracketBody returns the full balanced `[...]` literal (brackets
// included) whose opening `[` is at `open`, string- and depth-aware. Returns ""
// if the bracket is unbalanced. The result is suitable for mongoAggSplitStages.
func mongoAggPyBracketBody(src string, open int) string {
	if open >= len(src) || src[open] != '[' {
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
		case '\'', '"':
			inStr = c
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	return ""
}

// mongoAggPyAssignRe matches a Python assignment `<ident> = [` at the start of
// a (possibly indented) line. Used to locate a variable-bound pipeline's
// definition. The `[` that follows confirms the RHS opens a list literal.
var mongoAggPyAssignReCache = map[string]*regexp.Regexp{}

func mongoAggPyAssignRe(ident string) *regexp.Regexp {
	if re, ok := mongoAggPyAssignReCache[ident]; ok {
		return re
	}
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(ident) + `\s*=\s*\[`)
	mongoAggPyAssignReCache[ident] = re
	return re
}

// mongoAggPyFollowBinding finds the nearest assignment `ident = [ ... ]` that
// precedes the aggregate call (at `usePos`) and lies within the enclosing
// function (at or after `funcStart`), and returns its full list literal
// `[ ... ]`. Single-binding follow: the LAST such assignment before the use
// wins. Returns "" if no literal-list binding is found in scope.
func mongoAggPyFollowBinding(src, ident string, usePos, funcStart int) string {
	re := mongoAggPyAssignRe(ident)
	best := -1
	for _, m := range re.FindAllStringIndex(src, -1) {
		// m[1]-1 is the index of the `[` that opens the RHS list.
		if m[0] >= usePos {
			break // assignment is at/after the use — not a preceding binding.
		}
		if m[0] < funcStart {
			continue // belongs to an earlier function — out of scope.
		}
		best = m[1] - 1 // index of '['
	}
	if best < 0 {
		return ""
	}
	return mongoAggPyBracketBody(src, best)
}

// funcStartBefore returns the offset of the nearest `def`/`async def` line
// header that precedes `pos`, i.e. the start of the enclosing function body
// scope used to bound variable-binding resolution. Returns 0 (file scope) if
// none precedes `pos`.
func funcStartBefore(src string, pos int) int {
	start := 0
	for _, m := range pyOrmFuncRe.FindAllStringIndex(src, -1) {
		if m[0] >= pos {
			break
		}
		start = m[0]
	}
	return start
}

func isPyIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isPyIdentChar(c byte) bool {
	return isPyIdentStart(c) || (c >= '0' && c <= '9')
}
