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
// SCOPE — these pipeline forms are resolved:
//
//   - INLINE list literal: `coll.aggregate([ {"$lookup": {...}}, ... ])`.
//   - VARIABLE-BOUND (the important case — ~100% of real legacy usage):
//     `pipeline = [ {"$lookup": {...}}, ... ]` one statement up, then
//     `coll.aggregate(pipeline)`. We follow the identifier to its
//     same-function assignment whose RHS is a list literal (single-binding
//     follow).
//   - BUILDER-FUNCTION INDIRECTION (#3866, "ask 3"): the pipeline is produced by
//     a same-file builder function rather than a local literal:
//     `coll.aggregate(build_fn())` (direct call argument) OR
//     `pipeline = build_fn(); coll.aggregate(pipeline)` (call binding then use).
//     We resolve `build_fn` to its same-file definition and scan ITS body for
//     the returned pipeline list literal (`return [ ... ]` or
//     `pipeline = [ ... ]; return pipeline`). The `$lookup` stages from the
//     builder body are attributed to the AGGREGATING collection at the executor
//     site. Bounded follow (1-2 hops, same file). Cross-module / dynamic
//     dispatch / non-literal builder return → left unresolved — honest.
//
// The aggregating collection is recovered from pymongo idioms: `db.coll`,
// `db["coll"]`, `client.db.coll`, `get_collection("coll")`, a bare `*_cls` /
// Collection variable name, or `get_collection(CONST)` where CONST is a
// resolvable module-level / UPPER_SNAKE collection-name constant (#3866) — the
// constant resolves to the named collection node (Class:Inspection), so the
// anchor + JOINS_COLLECTION FromID land on the real collection rather than the
// shared ext:get_collection node or an all-caps phantom.
//
// HONEST LIMIT: cross-module builders, dynamic builder dispatch, runtime URLs,
// and unresolvable (lowercase-local-var) get_collection constants stay
// unresolved. We never fabricate stages or joins we cannot statically see.
package engine

import (
	"os"
	"path/filepath"
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

// mongoAggPyGetCollAnyArgRe recovers the FIRST argument of a `get_collection(...)`
// call whether it is a quoted string OR a bare constant identifier
// (`get_collection(INSPECTIONS)` — the legacy idiom binds a module-level
// collection-name constant). Group 1 is the quoted name, group 2 the bare
// identifier; exactly one is set. Used when following a receiver variable's
// binding (`coll = ...get_collection(X)`), where the constant name is the best
// available collection token.
var mongoAggPyGetCollAnyArgRe = regexp.MustCompile(
	`get_collection\(\s*(?:['"]([a-zA-Z_][\w$.]*)['"]|([A-Za-z_][\w]*))`,
)

// mongoAggPyDirectGetCollConstRe matches a `get_collection(CONST)` call whose
// single argument is a BARE constant identifier (NOT a quoted string), used as
// the immediate receiver of `.aggregate(`. Group 1 is the constant name. The
// pattern anchors at end-of-window (immediately before the `.aggregate` dot, up
// to surrounding whitespace) so it only fires when get_collection is the direct
// receiver — `db.get_collection(INSPECTIONS).aggregate(...)`.
var mongoAggPyDirectGetCollConstRe = regexp.MustCompile(
	`get_collection\(\s*([A-Za-z_]\w*)\s*\)\s*$`,
)

// mongoAggPyResolveDirectGetCollConst handles the direct
// `get_collection(BARE_CONST).aggregate(...)` receiver form, resolving the
// constant to its collection token. Returns "" when the immediate receiver is
// not a bare-constant get_collection call (e.g. a quoted arg already resolved
// elsewhere, a dynamic variable arg, or a different receiver shape) — honest:
// the caller then falls back to the normal receiver resolution.
func mongoAggPyResolveDirectGetCollConst(src string, dotPos int) string {
	winStart := dotPos - 200
	if winStart < 0 {
		winStart = 0
	}
	window := src[winStart:dotPos]
	m := mongoAggPyDirectGetCollConstRe.FindStringSubmatch(window)
	if m == nil {
		return ""
	}
	name := m[1]
	// A quoted arg can never match `([A-Za-z_]\w*)` framed by `(` and `)` with
	// no quotes, so this is unambiguously a bare identifier. But a bare
	// identifier may be a genuine module-level constant OR a dynamic local
	// variable (`get_collection(coll_var)`). We only resolve when it is plausibly
	// a constant: a same-file `NAME = "value"` definition exists, OR the name is
	// UPPER_SNAKE_CASE (the cross-module collection-constant convention). A
	// lowercase local var stays unresolved — honest-partial, current behavior.
	if mongoAggPyConstAssignRe(name).MatchString(src) || isUpperSnakeConst(name) {
		return mongoAggPyResolveCollConst(src, name)
	}
	return ""
}

// isUpperSnakeConst reports whether `name` follows the UPPER_SNAKE_CASE
// convention used for module-level collection-name constants (only A-Z, 0-9 and
// underscore, with at least one letter). Lowercase/mixed names — likely local
// variables — return false.
func isUpperSnakeConst(name string) bool {
	hasLetter := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			hasLetter = true
		case (c >= '0' && c <= '9') || c == '_':
		default:
			return false
		}
	}
	return hasLetter
}

// mongoAggPyCollVarBindRe matches a same-function binding of a collection
// variable: `<ident> = <rhs>` at the start of a (possibly indented) line. The
// RHS is recovered separately and inspected for a get_collection / subscript /
// dotted collection shape.
var mongoAggPyCollVarBindReCache = map[string]*regexp.Regexp{}

func mongoAggPyCollVarBindRe(ident string) *regexp.Regexp {
	if re, ok := mongoAggPyCollVarBindReCache[ident]; ok {
		return re
	}
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(ident) + `\s*=\s*(.+)$`)
	mongoAggPyCollVarBindReCache[ident] = re
	return re
}

// mongoAggPyCrossFileResolver resolves an imported builder-function NAME to the
// SOURCE TEXT of the (single) sibling module that defines it, so a pipeline
// builder living in a DIFFERENT file than the `.aggregate()` executor can still
// be scanned (the dominant `service.py` imports `get_..._pipeline` from
// `queries.py` shape — deploy-8's #1 ask). Returns "" when the name is not
// imported, the module cannot be located on disk, or reading it fails — honest:
// the builder then stays unresolved exactly as before. Production builds this
// from the repo root + the executor file's import statements; tests inject an
// in-memory module map. A nil resolver disables cross-file follow entirely
// (same-file behaviour unchanged).
type mongoAggPyCrossFileResolver func(builderName string) string

// scanPythonMongoAggregation walks `src`, finds pymongo `.aggregate(...)` call
// sites, resolves the pipeline (inline list literal OR a single same-function
// variable binding), parses each stage, and emits stage entities + join edges.
//
// `resolveCrossFile` (nil-safe) resolves an imported builder function to the
// source of its defining sibling module so cross-file pipeline builders are
// scanned; pass nil to disable cross-file follow.
func scanPythonMongoAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	resolveCrossFile mongoAggPyCrossFileResolver,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	if !mongoAggPyGateRe.MatchString(src) {
		return
	}

	for _, loc := range mongoAggPyCallRe.FindAllStringIndex(src, -1) {
		openParen := loc[1] - 1 // index of '('
		coll := mongoAggPyResolveReceiverFull(src, loc[0])
		if coll == "" {
			continue
		}

		// Resolve the pipeline list literal (the full `[ ... ]`, brackets
		// included, so mongoAggSplitStages can scan it) for either form.
		listLiteral := mongoAggPyResolvePipeline(src, openParen, loc[0], resolveCrossFile)
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

			// Stage entity Name — computed up front so the node-anchored
			// JOINS_COLLECTION twin (#4244) can reference THIS stage entity by
			// its exact file+name in a Format-A structural-ref stub.
			//
			// #4244 re-fix: the Name MUST embed the call-site line. The graph
			// entity ID is graph.EntityID(repo, kind, NAME, file) — it ignores
			// StartLine and the looked-up `from`. A file with several
			// `coll.aggregate(...)` calls on the SAME collection (the upvate
			// building/service.py shape — four `inspections_cln.aggregate(...)`
			// calls) independently restarts stage indexing at #0, so stage #2 of
			// call A and stage #2 of call B previously produced the IDENTICAL
			// Name (`inspections.aggregate#2 $lookup`) and therefore the IDENTICAL
			// graph ID — collapsing two DISTINCT `$lookup` stages (different
			// `from`) into ONE node. That node then carried BOTH stages'
			// JOINS_COLLECTION twins, so neighbors() returned a cross-stage
			// mix (and the `find`-able node looked wrong / unresolvable). The
			// `@L<line>` segment makes the per-call-site stage Name — and thus
			// the node ID and the twin's FromID stub — unique.
			name := mongoAggStageName(coll, callLine, idx, op)

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
					// #4244 — also link the join FROM the $lookup DataAccess
					// node so it is traversable (node → JOINS_COLLECTION →
					// Class:<from>). Only when `from` is statically present.
					emitJoin(mongoAggStageJoinEdge(name, path, lang, lk, "lookup"))
				}
				// Correlated sub-pipeline join: a `$lookup` may carry a
				// `pipeline: [ ... ]` whose own `$lookup` stages are NESTED joins
				// (`$lookup:{from:'a', pipeline:[{$lookup:{from:'b'}}]}` joins BOTH
				// a and b). mongoAggSplitStages only sees the top-level array, so the
				// nested `from` is otherwise lost. Recurse into the sub-pipeline and
				// emit a JOINS_COLLECTION edge for each nested `from`, attributed to
				// the SAME aggregating collection (the correlated lookup runs against
				// it). Bounded recursion over the static stage text — honest.
				for _, nlk := range mongoAggCollectNestedLookups(st) {
					emitJoin(mongoAggJoinEdge(coll, nlk, "lookup"))
					// #4244 — node-anchored twin for each nested `from`, so the
					// correlated joins are also reachable from the $lookup node.
					emitJoin(mongoAggStageJoinEdge(name, path, lang, nlk, "lookup"))
				}
			case "$graphLookup":
				lk := mongoAggParseLookup(st)
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
	//
	// A bare identifier receiver (e.g. `inspections_cls`) is frequently a local
	// handle bound one or more statements up to a `get_collection(...)` /
	// `db["coll"]` / `db.coll` expression (the dominant legacy pymongo idiom:
	// `inspections_cls = MongoDBConnection.get_collection(INSPECTIONS)` then
	// `inspections_cls.aggregate(pipeline)`). Following that binding recovers the
	// REAL collection name so the JOINS_COLLECTION `from` side anchors on the
	// actual collection node rather than a mangled variable name. The full source
	// is needed to scan the binding, so this follow lives in the caller-supplied
	// wrapper below; here we return the bare segment as a fallback.
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

// mongoAggPyResolveReceiverFull recovers the aggregating collection, following a
// bare-identifier receiver to its same-function binding when that resolves to a
// real collection (a `get_collection(...)` call, a `db["coll"]` subscript, or a
// `db.coll` dotted access). Falls back to the bare identifier from
// mongoAggPyResolveReceiver when no clarifying binding is in scope — honest, and
// preserves the existing simple-receiver behaviour. `dotPos` is the index of the
// `.` before `aggregate`.
func mongoAggPyResolveReceiverFull(src string, dotPos int) string {
	// Direct `get_collection(CONST).aggregate(...)` / `db.get_collection(CONST)`
	// where CONST is a BARE constant identifier (the legacy idiom binds a
	// module-level collection-name constant rather than a quoted string). The
	// quoted form was already resolved inside mongoAggPyResolveReceiver via
	// mongoAggPyGetCollRe; only the bare-constant arg reaches here. We resolve the
	// constant to the collection token (same-file value, else lowercased name) so
	// the anchor + JOINS_COLLECTION FromID lands on the real collection node
	// (Class:Inspection) instead of the all-caps phantom (Class:INSPECTIONS).
	if real := mongoAggPyResolveDirectGetCollConst(src, dotPos); real != "" {
		return real
	}
	recv := mongoAggPyResolveReceiver(src, dotPos)
	if recv == "" {
		return ""
	}
	// Only attempt a binding-follow for a bare identifier receiver — i.e. the
	// quoted/subscript/get_collection forms already returned the real name, and
	// a bare identifier is the only shape that can be a local handle. We detect
	// "already a real name" cheaply: those forms can contain characters a Python
	// identifier cannot, but the simplest robust check is to re-scan: if the
	// bare-identifier path produced `recv`, the text immediately before dotPos is
	// exactly that identifier with no preceding `]`/`)` /`.`+name chain implying
	// a subscript or call we already resolved.
	if !isPyBareIdentReceiver(src, dotPos, recv) {
		return recv
	}
	if real := mongoAggPyFollowCollBinding(src, recv, dotPos); real != "" {
		return real
	}
	return recv
}

// isPyBareIdentReceiver reports whether the receiver immediately before dotPos is
// the bare identifier `ident` itself (not the tail of a `db.coll` dotted chain,
// a `db["coll"]` subscript, or a `get_collection(...)` call — those were already
// resolved to a real name). It checks that the char before the identifier is not
// `.`, `]`, or `)`.
func isPyBareIdentReceiver(src string, dotPos int, ident string) bool {
	start := dotPos - len(ident)
	if start < 0 || src[start:dotPos] != ident {
		return false
	}
	if start == 0 {
		return true
	}
	prev := src[start-1]
	return prev != '.' && prev != ']' && prev != ')'
}

// mongoAggPyFollowCollBinding finds the nearest same-function binding of `ident`
// preceding `usePos` and, if its RHS is a recognisable collection expression,
// returns the real collection name. Returns "" when there is no clarifying
// binding in scope (the caller then keeps the bare identifier). Recognised RHS
// shapes: `...get_collection("c")` / `...get_collection(CONST)`, `db["c"]` /
// `db['c']`, and a trailing `db.c` dotted access.
func mongoAggPyFollowCollBinding(src, ident string, usePos int) string {
	funcStart := funcStartBefore(src, usePos)
	re := mongoAggPyCollVarBindRe(ident)
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
	rhs := strings.TrimSpace(bestRHS)

	// get_collection("c") / get_collection(CONST).
	if m := mongoAggPyGetCollAnyArgRe.FindStringSubmatch(rhs); m != nil {
		if m[1] != "" {
			return m[1] // quoted collection name
		}
		if m[2] != "" {
			// Bare constant identifier (e.g. `get_collection(INSPECTIONS)`) — the
			// legacy idiom where a collection-name constant stands in for the
			// quoted name. The constant NAME is NOT the collection name: the
			// migration parity oracle (and every string-literal viewset form)
			// anchors the join on the COLLECTION, so we must resolve the constant
			// to its string VALUE. Without this, `INSPECTIONS` flows straight into
			// capitalisedSingular which leaves the all-caps token unchanged
			// (`Class:INSPECTIONS`), a phantom node that never matches the real
			// `Class:Inspection` collection node the literal `"inspections"`
			// produces — so the task-file joins orphan and vanish from the graph
			// while ~57 string-literal viewset aggregations extract fine.
			return mongoAggPyResolveCollConst(src, m[2])
		}
	}
	// db["c"] / db['c'] subscript anywhere in the RHS.
	if m := mongoAggPyCollSubscriptRe.FindStringSubmatch(rhs); m != nil {
		return m[1]
	}
	// Trailing `db.coll` dotted access — last dotted segment, but only if the RHS
	// is a pure dotted chain (no call/subscript), to avoid grabbing a method name.
	if m := mongoAggPyCollDottedRe.FindStringSubmatch(rhs); m != nil {
		seg := m[1]
		if seg != "self" && seg != "cls" {
			return seg
		}
	}
	return ""
}

// mongoAggPyConstAssignRe matches a module-level constant assignment
// `NAME = "value"` / `NAME = 'value'` at column 0 (top-level, not indented),
// capturing the string value. Used to resolve a collection-name constant
// (`INSPECTIONS = "inspections"`) to its value when defined in the same file.
var mongoAggPyConstAssignReCache = map[string]*regexp.Regexp{}

func mongoAggPyConstAssignRe(name string) *regexp.Regexp {
	if re, ok := mongoAggPyConstAssignReCache[name]; ok {
		return re
	}
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*=\s*['"]([a-zA-Z_][\w$.]*)['"]`)
	mongoAggPyConstAssignReCache[name] = re
	return re
}

// mongoAggPyResolveCollConst turns a bare collection-name constant identifier
// (e.g. `INSPECTIONS`) into the collection token used to anchor the join.
//
//  1. Same-file definition: if `NAME = "value"` exists at module scope in this
//     file, return the literal value ("inspections") — exact, matches a quoted
//     receiver.
//  2. Cross-module import (the real `_get_me_inspections` case — `INSPECTIONS`
//     comes from `core.mongodb_collections`): the value isn't in this file, so
//     fall back to the constant NAME LOWERCASED. UPPER_SNAKE collection
//     constants are conventionally the upper-cased collection name, so
//     lowercasing recovers a token that canonicalises (via capitalisedSingular)
//     to the SAME node as the string literal would: `INSPECTIONS` → `inspections`
//     → `Class:Inspection`, identical to the 57 working viewset forms. This is
//     the honest-partial: we don't fabricate a value, we normalise the name so
//     the edge lands on the real collection node instead of a phantom all-caps
//     one.
func mongoAggPyResolveCollConst(src, name string) string {
	if m := mongoAggPyConstAssignRe(name).FindStringSubmatch(src); m != nil {
		return m[1]
	}
	return strings.ToLower(name)
}

// mongoAggPyCollSubscriptRe matches a `db["coll"]` / `db['coll']` subscript
// anywhere in a receiver-variable binding's RHS.
var mongoAggPyCollSubscriptRe = regexp.MustCompile(`\[\s*['"]([a-zA-Z_][\w$.]*)['"]\s*\]`)

// mongoAggPyCollDottedRe matches a pure dotted access (`db.coll`,
// `client.db.coll`) — no trailing call or subscript — capturing the last segment.
var mongoAggPyCollDottedRe = regexp.MustCompile(`^[A-Za-z_][\w.]*\.([A-Za-z_]\w*)$`)

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
func mongoAggPyResolvePipeline(src string, openParen, dotPos int, resolveCrossFile mongoAggPyCrossFileResolver) string {
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

	// A leading identifier opens one of three forms: a bare-var argument
	// (`pipeline`), a builder CALL argument (`build_fn()`), or an unresolvable
	// expression. Lex the identifier, then look at what follows.
	if isPyIdentStart(src[i]) {
		idStart := i
		for i < len(src) && isPyIdentChar(src[i]) {
			i++
		}
		ident := src[idStart:i]
		j := i
		for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
			j++
		}
		if j >= len(src) {
			return ""
		}
		funcStart := funcStartBefore(src, dotPos)

		switch src[j] {
		case ')':
			// Form 2: bare identifier argument (`pipeline`).
			// (a) Shape C (#3928): pipeline built IMPERATIVELY in the same function
			// via `pipeline = []; pipeline.append({...}); pipeline.extend([...])`,
			// then `coll.aggregate(pipeline)`. Reconstruct it from the init +
			// append/extend additions preceding the call. Tried FIRST because it
			// subsumes a list-literal init (an `append`/`extend` after a non-empty
			// literal would otherwise be lost by the plain-binding follow, and an
			// EMPTY `pipeline = []` init followed only by appends has no useful
			// literal at all). Fires only when an append/extend anchor contributes a
			// stage; otherwise "" and we fall back to the plain literal binding.
			if body := src[funcStart:dotPos]; mongoAggPyHasMutation(body, ident, len(body)) {
				if lit := mongoAggPyImperativePipeline(body, ident, len(body)); lit != "" {
					return lit
				}
			}
			// (b) nearest preceding `pipeline = [ ... ]` literal binding.
			if lit := mongoAggPyFollowBinding(src, ident, dotPos, funcStart); lit != "" {
				return lit
			}
			// (c) builder indirection: `pipeline = build_fn()` → resolve the
			// builder's definition and scan its body for the returned pipeline.
			if fn := mongoAggPyFollowCallBinding(src, ident, dotPos, funcStart); fn != "" {
				return mongoAggPyResolveBuilderBody(src, fn, resolveCrossFile)
			}
			return "" // non-literal / non-builder binding — honest unresolved.
		case '(':
			// Form 3: direct builder CALL argument — `coll.aggregate(build_fn())`.
			// Require the call to be JUST `ident()` (empty args or simple args),
			// closing immediately before the aggregate `)`. We accept any balanced
			// argument list, then resolve the builder body. Bounded 1-hop follow.
			callClose := mongoAggPyMatchParen(src, j)
			if callClose < 0 {
				return ""
			}
			k := callClose + 1
			for k < len(src) && (src[k] == ' ' || src[k] == '\t' || src[k] == '\n' || src[k] == '\r') {
				k++
			}
			if k >= len(src) || src[k] != ')' {
				return "" // e.g. `build()['x']`, `build() + extra` — unresolved.
			}
			return mongoAggPyResolveBuilderBody(src, ident, resolveCrossFile)
		default:
			return "" // `pipeline + extra`, `pipeline['x']`, attribute — unresolved.
		}
	}
	return ""
}

// mongoAggPyMatchParen returns the index of the `)` matching the `(` at `open`,
// string-aware and depth-aware. Returns -1 if unbalanced. Used to skip over a
// builder call's argument list (`build_fn(db, status="x")`).
func mongoAggPyMatchParen(src string, open int) int {
	if open >= len(src) || src[open] != '(' {
		return -1
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
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
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

// mongoAggPyCallBindReCache caches the per-ident `ident = build_fn(...)` matcher
// used for builder-call indirection.
var mongoAggPyCallBindReCache = map[string]*regexp.Regexp{}

// mongoAggPyCallBindRe matches a same-function binding `ident = build_fn(...)`
// whose RHS is a plain function CALL (a bare callee name immediately followed by
// `(`), capturing the callee name in group 1. It deliberately does NOT match a
// list literal (`ident = [`), a subscript, a method chain (`a.b()`), or an
// expression with a trailing operator — those are handled elsewhere or left
// unresolved. The `(?:\s*$|\s*\))` tail keeps it to a single `build()` call (no
// `build() + extra`), but allows arguments inside the parens via `[^\n]*`.
func mongoAggPyCallBindRe(ident string) *regexp.Regexp {
	if re, ok := mongoAggPyCallBindReCache[ident]; ok {
		return re
	}
	re := regexp.MustCompile(
		`(?m)^[ \t]*` + regexp.QuoteMeta(ident) + `\s*=\s*([A-Za-z_]\w*)\s*\(`,
	)
	mongoAggPyCallBindReCache[ident] = re
	return re
}

// mongoAggPyFollowCallBinding finds the nearest same-function binding
// `ident = build_fn(...)` preceding `usePos` and returns the callee (builder)
// name when the entire RHS is JUST that call (`ident = build_fn(args)` with
// nothing trailing). Returns "" when the binding is absent, is not a plain call,
// or has a trailing expression/subscript — honest-partial. This is the
// `pipeline = build_fn(); coll.aggregate(pipeline)` builder-indirection hop.
func mongoAggPyFollowCallBinding(src, ident string, usePos, funcStart int) string {
	re := mongoAggPyCallBindRe(ident)
	bestFn := ""
	bestParen := -1
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		if m[0] >= usePos {
			break
		}
		if m[0] < funcStart {
			continue
		}
		bestFn = src[m[2]:m[3]]
		bestParen = m[1] - 1 // index of '(' after the callee
	}
	if bestFn == "" || bestParen < 0 {
		return ""
	}
	// The RHS must be JUST `build_fn(...)` — its matching `)` is the last
	// non-whitespace char on the logical line. Reject `build_fn() + x`,
	// `build_fn()[0]`, `a.build_fn()` (the latter can't match the anchor anyway).
	close := mongoAggPyMatchParen(src, bestParen)
	if close < 0 {
		return ""
	}
	k := close + 1
	for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
		k++
	}
	if k < len(src) && src[k] != '\n' && src[k] != '\r' {
		return "" // trailing expression — not a pure builder call.
	}
	return bestFn
}

// mongoAggPyResolveBuilderBody resolves a builder function `fnName` defined in
// the SAME file and returns the pipeline list literal `[ ... ]` it produces.
// Two body shapes are followed (bounded, 1-2 hops, no cross-module):
//
//	def build(): return [ ... ]                      — direct returned literal.
//	def build(): pipeline = [ ... ]; return pipeline — local var then return it.
//
// Returns "" when the builder is not defined in this file, returns something
// other than a same-function list-literal (a call, a comprehension, a mutated
// var), or the literal is unbalanced — honest-partial: the executor site then
// stays unresolved rather than fabricating stages.
func mongoAggPyResolveBuilderBody(src, fnName string, resolveCrossFile mongoAggPyCrossFileResolver) string {
	defStart, bodyEnd := mongoAggPyFuncBody(src, fnName)
	if defStart < 0 {
		// Same-file def absent — the builder is IMPORTED from a sibling module
		// (the dominant `service.py` does `from .queries import build` then
		// `coll.aggregate(build(...))`, with `build` defined in `queries.py`).
		// Resolve the import to that module's source and scan ITS body for the
		// returned pipeline. Bounded 1-file follow; unresolvable import → "".
		if resolveCrossFile == nil {
			return ""
		}
		otherSrc := resolveCrossFile(fnName)
		if otherSrc == "" || otherSrc == src {
			return ""
		}
		// No further cross-file hop (nil resolver): bound the follow to one file
		// so a builder that itself imports another builder stays honest-partial.
		return mongoAggPyResolveBuilderBody(otherSrc, fnName, nil)
	}
	body := src[defStart:bodyEnd]

	// Shape A: `return [ ... ]` — a list literal returned directly. Find the
	// first `return [` and return its balanced literal.
	if loc := mongoAggPyReturnListRe.FindStringIndex(body); loc != nil {
		// loc[1]-1 is the index of '[' (the regex ends at the bracket).
		if lit := mongoAggPyBracketBody(body, loc[1]-1); lit != "" {
			return lit
		}
	}

	// Shape B: `return <ident>` where `<ident> = [ ... ]` earlier in the body.
	if m := mongoAggPyReturnIdentRe.FindStringSubmatchIndex(body); m != nil {
		retIdent := body[m[2]:m[3]]
		retPos := m[0] // offset of the `return <ident>` within the body
		// Shape C (#3928): the pipeline var is built IMPERATIVELY via
		// append/extend (with or without a list-literal init), then returned.
		// Reconstruct it from init + append/extend additions preceding the return,
		// but ONLY when a mutation is present — a plain `pipeline = [ ... ]; return
		// pipeline` keeps using the literal-binding follow below (no behavior
		// change vs #3866).
		if mongoAggPyHasMutation(body, retIdent, retPos) {
			if lit := mongoAggPyImperativePipeline(body, retIdent, retPos); lit != "" {
				return lit
			}
		}
		// usePos = end of body (the binding precedes the return); funcStart = 0
		// because `body` is already sliced to this one function.
		if lit := mongoAggPyFollowBinding(body, retIdent, len(body), 0); lit != "" {
			return lit
		}
	}
	return ""
}

// mongoAggPyAppendCallReCache caches per-ident `pipeline.append(` matchers.
var mongoAggPyAppendCallReCache = map[string]*regexp.Regexp{}

// mongoAggPyAppendCallRe matches a `<ident>.append(` call (a single-stage push
// onto an imperatively-built pipeline), capturing nothing — the `(` index is
// recovered from the match end so the dict-literal argument can be balanced.
func mongoAggPyAppendCallRe(ident string) *regexp.Regexp {
	if re, ok := mongoAggPyAppendCallReCache[ident]; ok {
		return re
	}
	re := regexp.MustCompile(`(?m)` + regexp.QuoteMeta(ident) + `\.append\s*\(`)
	mongoAggPyAppendCallReCache[ident] = re
	return re
}

// mongoAggPyExtendCallReCache caches per-ident `pipeline.extend(` matchers.
var mongoAggPyExtendCallReCache = map[string]*regexp.Regexp{}

// mongoAggPyExtendCallRe matches a `<ident>.extend(` call (a multi-stage splice
// onto an imperatively-built pipeline).
func mongoAggPyExtendCallRe(ident string) *regexp.Regexp {
	if re, ok := mongoAggPyExtendCallReCache[ident]; ok {
		return re
	}
	re := regexp.MustCompile(`(?m)` + regexp.QuoteMeta(ident) + `\.extend\s*\(`)
	mongoAggPyExtendCallReCache[ident] = re
	return re
}

// mongoAggPyBraceBody returns the full balanced `{...}` object literal (braces
// included) whose opening `{` is at `open`, string- and depth-aware. Returns ""
// if unbalanced. Used to capture a single `pipeline.append({...})` stage dict.
func mongoAggPyBraceBody(src string, open int) string {
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
		case '\'', '"':
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

// mongoAggPyHasMutation reports whether `ident` is mutated by at least one
// `.append(`/`.extend(` call before `usePos` in `body`. Used to decide whether
// the imperative reconstruction should take priority over a plain list-literal
// binding follow: when there is NO mutation, the existing literal path handles
// the pipeline unchanged (preserving all #3866 behavior); only the presence of
// an append/extend warrants the new reconstruction.
func mongoAggPyHasMutation(body, ident string, usePos int) bool {
	if loc := mongoAggPyAppendCallRe(ident).FindStringIndex(body); loc != nil && loc[0] < usePos {
		return true
	}
	if loc := mongoAggPyExtendCallRe(ident).FindStringIndex(body); loc != nil && loc[0] < usePos {
		return true
	}
	return false
}

// mongoAggPyImperativePipeline reconstructs an imperatively-built pipeline.
//
// Given a function `body` (already sliced to ONE function), a pipeline variable
// `ident`, and `usePos` (the offset within `body` of the consuming `return
// pipeline` / `aggregate(pipeline)` — additions after this point are ignored),
// it accumulates stages, in source order, from:
//
//	pipeline = []                          — empty init (zero stages), and
//	pipeline = [ {stage}, ... ]            — list-literal init (its stages), then
//	pipeline.append({ stage })             — one dict-literal stage appended, and
//	pipeline.extend([ {stage}, ... ])      — list-literal stages spliced in.
//
// It returns a SYNTHETIC pipeline list literal `[ stage, stage, ... ]` built
// from the accumulated stage substrings, suitable for mongoAggSplitStages, so
// the rest of the pass (lookup parse, join edges, stage nodes) is unchanged.
//
// Honest-partial — an addition is contributed ONLY when its argument is a
// literal of the right shape: append of a `{...}` dict literal, extend of a
// `[...]` list literal. `pipeline.append(stage_var)` (a non-literal var),
// `pipeline.extend(other_pipe)`, dynamic stage construction, or a non-`[]`
// init (e.g. `pipeline = build_base()`) contribute NOTHING from that statement;
// if NO recognised init AND no recognised additions exist, "" is returned and
// the caller stays unresolved. We never fabricate a stage we cannot see.
func mongoAggPyImperativePipeline(body, ident string, usePos int) string {
	type acc struct {
		pos   int
		stage string
	}
	var stages []acc
	hasAnchor := false // saw a `pipeline = []`/`[...]` init or any append/extend

	// 1. Init binding: nearest `ident = [ ... ]` (possibly empty) before usePos.
	//    We reuse the assignment matcher, then balance the bracket ourselves so an
	//    empty `[]` is handled (zero stages but a valid anchor).
	initRe := mongoAggPyAssignRe(ident)
	initBracket := -1
	for _, m := range initRe.FindAllStringIndex(body, -1) {
		if m[0] >= usePos {
			break
		}
		initBracket = m[1] - 1 // index of '['
	}
	if initBracket >= 0 {
		hasAnchor = true
		if lit := mongoAggPyBracketBody(body, initBracket); lit != "" {
			for _, st := range mongoAggSplitStages(lit, 0) {
				stages = append(stages, acc{pos: initBracket, stage: st})
			}
		}
	}

	// 2. `pipeline.append({...})` — each dict-literal arg is one stage.
	for _, loc := range mongoAggPyAppendCallRe(ident).FindAllStringIndex(body, -1) {
		callPos := loc[0]
		if callPos >= usePos {
			break
		}
		hasAnchor = true
		open := loc[1] - 1 // index of '('
		j := open + 1
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\n' || body[j] == '\r') {
			j++
		}
		if j >= len(body) || body[j] != '{' {
			continue // append of a non-dict-literal (var, call) — honest skip.
		}
		if obj := mongoAggPyBraceBody(body, j); obj != "" {
			stages = append(stages, acc{pos: callPos, stage: obj})
		}
	}

	// 3. `pipeline.extend([...])` — each list-literal element is one stage.
	for _, loc := range mongoAggPyExtendCallRe(ident).FindAllStringIndex(body, -1) {
		callPos := loc[0]
		if callPos >= usePos {
			break
		}
		hasAnchor = true
		open := loc[1] - 1 // index of '('
		j := open + 1
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\n' || body[j] == '\r') {
			j++
		}
		if j >= len(body) || body[j] != '[' {
			continue // extend of a non-list-literal (var, call) — honest skip.
		}
		if lit := mongoAggPyBracketBody(body, j); lit != "" {
			for _, st := range mongoAggSplitStages(lit, 0) {
				stages = append(stages, acc{pos: callPos, stage: st})
			}
		}
	}

	if !hasAnchor || len(stages) == 0 {
		return ""
	}

	// Order stages by their source position so init → append → extend interleave
	// exactly as written. sort.SliceStable preserves intra-statement order (the
	// list-literal element order from a single init/extend).
	sortStableByPos := func() {
		for i := 1; i < len(stages); i++ {
			for j := i; j > 0 && stages[j-1].pos > stages[j].pos; j-- {
				stages[j-1], stages[j] = stages[j], stages[j-1]
			}
		}
	}
	sortStableByPos()

	var b strings.Builder
	b.WriteByte('[')
	for i, s := range stages {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s.stage)
	}
	b.WriteByte(']')
	return b.String()
}

// mongoAggPyReturnListRe matches a `return [` (the start of a returned list
// literal) anywhere in a function body.
var mongoAggPyReturnListRe = regexp.MustCompile(`(?m)^[ \t]*return\s+\[`)

// mongoAggPyReturnIdentRe matches `return <ident>` (a bare identifier return),
// capturing the identifier — the pipeline-variable a builder returns after
// assembling it.
var mongoAggPyReturnIdentRe = regexp.MustCompile(`(?m)^[ \t]*return\s+([A-Za-z_]\w*)\s*$`)

// mongoAggPyFuncBody returns the [start,end) byte offsets of the body of the
// SAME-file function named `fnName` — from its `def`/`async def` header to the
// start of the next top-level-or-shallower-or-equal `def` (or EOF). The body
// span is generous (it may include nested defs) but is bounded to this function
// for the purpose of scanning a returned pipeline. Returns (-1,-1) when no such
// function is defined in `src`.
func mongoAggPyFuncBody(src, fnName string) (int, int) {
	re := mongoAggPyNamedDefRe(fnName)
	m := re.FindStringIndex(src)
	if m == nil {
		return -1, -1
	}
	start := m[0]
	// End at the next def header at the same or shallower indentation. Simplest
	// honest bound: the next `def `/`async def ` line whose header begins after
	// our header. enclosingFuncAt / funcStartBefore use the same pyOrmFuncRe.
	end := len(src)
	for _, d := range pyOrmFuncRe.FindAllStringIndex(src, -1) {
		if d[0] > start {
			end = d[0]
			break
		}
	}
	return start, end
}

// mongoAggPyNamedDefReCache caches per-name `def fnName(` matchers.
var mongoAggPyNamedDefReCache = map[string]*regexp.Regexp{}

// mongoAggPyNamedDefRe matches the `def fnName(` / `async def fnName(` header of
// a specific function.
func mongoAggPyNamedDefRe(name string) *regexp.Regexp {
	if re, ok := mongoAggPyNamedDefReCache[name]; ok {
		return re
	}
	re := regexp.MustCompile(`(?m)^[ \t]*(?:async\s+)?def\s+` + regexp.QuoteMeta(name) + `\s*\(`)
	mongoAggPyNamedDefReCache[name] = re
	return re
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

// mongoAggCollectNestedLookups extracts the $lookup join targets nested inside a
// correlated $lookup stage's `pipeline: [ ... ]` sub-array (and any deeper
// sub-pipelines, recursively). The outer stage's OWN `from` is handled by the
// caller; this returns ONLY the nested ones. A correlated lookup such as
//
//	{"$lookup": {"from": "m_contracts",
//	             "pipeline": [{"$lookup": {"from": "m_group_device_settings"}}]}}
//
// is a join against BOTH m_contracts (caller) AND m_group_device_settings
// (nested) — the nested $lookup is invisible to the top-level stage splitter, so
// without this it never produces an edge. We locate each `pipeline:` value that
// is a `[ ... ]` literal, split it into stages, and for every `$lookup` /
// `$graphLookup` stage emit its parsed lookup AND recurse into ITS sub-pipeline.
// Honest: a `from` that is non-literal (expression/variable) parses to "" and is
// skipped; a `pipeline` value that is not a bracket literal is skipped.
func mongoAggCollectNestedLookups(stage string) []mongoAggLookup {
	var out []mongoAggLookup
	for _, sub := range mongoAggPipelineSubArrays(stage) {
		for _, st := range mongoAggSplitStages(sub, 0) {
			op := mongoAggFirstKey(st)
			if op != "$lookup" && op != "$graphLookup" {
				// Even a non-lookup nested stage may itself carry a sub-pipeline
				// (e.g. a nested correlated stage); recurse to be exhaustive.
				out = append(out, mongoAggCollectNestedLookups(st)...)
				continue
			}
			if lk := mongoAggParseLookup(st); lk.from != "" {
				out = append(out, lk)
			}
			// Recurse: the nested $lookup may have its OWN correlated sub-pipeline.
			out = append(out, mongoAggCollectNestedLookups(st)...)
		}
	}
	return out
}

// mongoAggPipelineSubArrays returns every `pipeline: [ ... ]` value (the full
// balanced bracket literal) that appears as a key inside `stage`. String- and
// depth-aware: it walks the stage text, and at each `pipeline` key whose value
// opens with `[`, captures the balanced array. Multiple sub-pipelines in one
// stage (e.g. inside a `$facet`-like shape) are all returned.
func mongoAggPipelineSubArrays(stage string) []string {
	var arrays []string
	for _, loc := range mongoAggPipelineKeyRe.FindAllStringIndex(stage, -1) {
		// loc[1] is just past the matched `pipeline` ... `:`; skip whitespace to
		// the value, which must open a list literal to be a stage array.
		j := loc[1]
		for j < len(stage) && (stage[j] == ' ' || stage[j] == '\t' || stage[j] == '\n' || stage[j] == '\r') {
			j++
		}
		if j >= len(stage) || stage[j] != '[' {
			continue
		}
		if lit := mongoAggPyBracketBody(stage, j); lit != "" {
			arrays = append(arrays, lit)
		}
	}
	return arrays
}

// mongoAggPipelineKeyRe matches a `pipeline:` / `"pipeline":` / `'pipeline':`
// object key (the value follows). Used to locate correlated $lookup sub-arrays.
var mongoAggPipelineKeyRe = regexp.MustCompile(`(?:\bpipeline\b|['"]pipeline['"])\s*:`)

// mongoAggPyFromImportRe matches a SINGLE-LINE Python `from <module> import
// <names>` statement, capturing the module path (group 1, e.g. `.queries`,
// `core.services.building.queries`) and the imported-names clause (group 2,
// e.g. `build, other` or `build as b`). The leading `import\s+` is constrained
// to NOT be followed by an opening paren so the multi-line parenthesised form
// is handled by mongoAggPyFromImportParenRe instead (the two are disjoint).
var mongoAggPyFromImportRe = regexp.MustCompile(`(?m)^[ \t]*from\s+([.\w]+)\s+import\s+([^\n(#][^\n#]*)`)

// mongoAggPyFromImportParenRe matches the MULTI-LINE parenthesised import form
//
//	from core.services.building.queries import (
//	    get_inspection_devices_pipeline,
//	    get_inspection_devices_filters_pipeline,
//	)
//
// capturing the module path (group 1) and the full parenthesised names clause
// (group 2, newlines and trailing commas included — split/trimmed by the caller).
// `(?s)` lets `.` span newlines; `[^)]*` is non-greedy enough because a Python
// import-name list never contains a nested `)`. This is the dominant real
// builder-import shape in upvate-core's service.py (deploy-8 item-2): without it
// the cross-file builder name never resolves and the $lookup `from` collections
// orphan.
var mongoAggPyFromImportParenRe = regexp.MustCompile(`(?ms)^[ \t]*from\s+([.\w]+)\s+import\s+\(([^)]*)\)`)

// newMongoAggPyCrossFileResolver builds a cross-file builder resolver bound to
// the executor file's imports. For a builder NAME it:
//
//  1. scans `src` for a `from <module> import ... <NAME> ...` statement,
//  2. resolves `<module>` (relative `.`/`..` or absolute dotted) to a sibling
//     `.py` file path under `repoRoot` (relative to the executor `path`),
//  3. reads and returns that file's source.
//
// Returns a closure yielding "" when the name is not imported, the module can't
// be located, or the read fails — the builder then stays unresolved (honest).
// When `repoRoot` is empty (path not absolutifiable) the resolver still works if
// `path` is itself absolute; otherwise it yields "" for every name.
func newMongoAggPyCrossFileResolver(repoRoot, path, src string) mongoAggPyCrossFileResolver {
	// Pre-scan imports once: NAME → module path. Both single-line and
	// parenthesised multi-line `from ... import (...)` forms are scanned so a
	// builder imported via the dominant multi-line list still resolves.
	nameToModule := map[string]string{}
	addImport := func(module, clause string) {
		for _, raw := range strings.Split(clause, ",") {
			name := strings.TrimSpace(raw)
			// Drop any trailing inline comment (multi-line lists may carry one
			// per line: `name,  # keep`).
			if i := strings.IndexByte(name, '#'); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			if name == "" || name == "*" {
				continue
			}
			// `build as b` — the LOCAL alias is what the executor calls, so key on
			// the alias; the def in the target file still carries the ORIGINAL name,
			// so we must remember both. We resolve the target body by the original
			// def name, so store original under the alias key.
			orig := name
			if parts := strings.Fields(name); len(parts) == 3 && parts[1] == "as" {
				name = parts[2] // alias used at the call site
				orig = parts[0] // original def name in the target module
			}
			// Strip any stray parens from a `from x import (a, b)` single-line list.
			name = strings.Trim(name, "() \t")
			orig = strings.Trim(orig, "() \t")
			if name == "" {
				continue
			}
			nameToModule[name] = module + "\x00" + orig
		}
	}
	for _, m := range mongoAggPyFromImportRe.FindAllStringSubmatch(src, -1) {
		addImport(m[1], m[2])
	}
	for _, m := range mongoAggPyFromImportParenRe.FindAllStringSubmatch(src, -1) {
		addImport(m[1], m[2])
	}

	return func(builderName string) string {
		enc, ok := nameToModule[builderName]
		if !ok {
			return ""
		}
		module := enc
		if i := strings.IndexByte(enc, '\x00'); i >= 0 {
			module = enc[:i]
		}
		file := mongoAggPyModuleToFile(repoRoot, path, module)
		if file == "" {
			return ""
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

// mongoAggPyModuleToFile resolves a Python import module path to a sibling `.py`
// file path on disk, relative to the executor file `path` under `repoRoot`.
//
//   - Relative imports (`.queries`, `..pkg.mod`): each leading dot is one parent
//     directory of the executor file's package, then the remaining dotted
//     segments are directory steps, with `.py` appended to the last.
//   - Absolute imports (`core.services.building.queries`): resolved from
//     `repoRoot` (the dotted path is the package path from the source root). When
//     `repoRoot` is empty this cannot be located → "".
//
// Returns "" when the resulting file does not exist (a package `__init__.py`
// re-export, a namespace package, or a third-party module — honest skip).
func mongoAggPyModuleToFile(repoRoot, path, module string) string {
	if module == "" {
		return ""
	}
	// Executor file's directory (absolute when possible).
	execAbs := path
	if repoRoot != "" && !filepath.IsAbs(execAbs) {
		execAbs = filepath.Join(repoRoot, path)
	}
	execDir := filepath.Dir(execAbs)

	var candidate string
	if strings.HasPrefix(module, ".") {
		// Relative import. Count leading dots: 1 dot = current package dir.
		dots := 0
		for dots < len(module) && module[dots] == '.' {
			dots++
		}
		rest := module[dots:] // dotted remainder after the dots
		dir := execDir
		// Each dot BEYOND the first ascends one parent (`.` = same dir, `..` = parent).
		for k := 1; k < dots; k++ {
			dir = filepath.Dir(dir)
		}
		if rest == "" {
			// `from . import x` — x is a module in the current package; without the
			// imported submodule name we cannot point at a file. Honest "".
			return ""
		}
		segs := strings.Split(rest, ".")
		parts := append([]string{dir}, segs...)
		candidate = filepath.Join(parts...) + ".py"
	} else {
		// Absolute dotted import, resolved from the repo root.
		if repoRoot == "" {
			return ""
		}
		segs := strings.Split(module, ".")
		parts := append([]string{repoRoot}, segs...)
		candidate = filepath.Join(parts...) + ".py"
	}
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate
	}
	return ""
}

func isPyIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isPyIdentChar(c byte) bool {
	return isPyIdentStart(c) || (c >= '0' && c <= '9')
}
