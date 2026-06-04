// Mongoid (Ruby) aggregation `$lookup` joins + association relations extraction
// (#3847, epic #3837).
//
// This is the Ruby sibling of the four already-landed Mongo ODM passes:
// orm_queries_jsts_mongo_agg.go (#3426), orm_queries_python_mongo_agg.go
// (#3440), orm_queries_go_mongo_agg.go (#3846), and
// orm_queries_java_mongo_agg.go (#3845), plus the Mongoose ref/populate relation
// sibling orm_queries_jsts_mongoose_populate.go (#3844/#3889). The existing Ruby
// surface (scanRubyORM / scanRubyDrivers) handles ActiveRecord verbs and the raw
// Mongo Ruby driver `client[:coll]` topology, but it does NOT understand Mongoid:
// neither the `Model.collection.aggregate([...])` pipeline (where a `$lookup`
// stage is an implicit cross-collection JOIN) nor the Mongoid association macros
// (`belongs_to` / `has_many` / `embeds_many` — the document-reference join the
// rewrite target reasons about). This pass brings Mongoid up to the same
// contract the other ODMs share:
//
//  1. JOINS_COLLECTION relationship — for every `$lookup` / `$graphLookup` stage
//     in a `Model.collection.aggregate([...])` pipeline, an edge
//     Class:<aggregating coll> -> Class:<from coll>; and for every Mongoid
//     association macro in a `Mongoid::Document` class, an edge
//     Class:<owning model> -> Class:<associated model>. The aggregating
//     collection / owning model is resolved from the Ruby class name (its
//     conventional collection) or an explicit `store_in collection: 'books'`.
//
//  2. SCOPE.DataAccess pipeline-stage entities — one per aggregation stage,
//     anchored at the aggregate call site, subtype = the stage operator, stage
//     order preserved as stage_index. Mirrors the JS/Python/Go/Java shape.
//
// MONGOID IDIOMS:
//
//   - Aggregation: `Book.collection.aggregate([{ '$lookup' => { 'from' =>
//     'authors', 'localField' => 'author_id', 'foreignField' => '_id', 'as' =>
//     'author' } }])`. Ruby hashes use the rocket `=>` (or, for symbol keys,
//     `key:`) separator rather than the JS/JSON `:`; the stage key is a quoted
//     string `'$lookup'`. We split the inline array literal into stages with the
//     shared splitter and parse each with a Ruby-aware string-field reader that
//     accepts BOTH `=>` and `:` separators.
//   - Associations: inside a class that `include Mongoid::Document`, the macros
//     `belongs_to :author`, `has_many :books`, `has_one :profile`,
//     `embeds_many :pages`, `embeds_one :cover`, `embedded_in :book` declare a
//     document reference. Each macro names an associated model (singularised /
//     camelised from the association symbol, or an explicit `class_name:`).
//
// HONEST LIMIT: a dynamic aggregating collection (a non-class receiver we can't
// resolve), a dynamic `from` (variable / expression, not a quoted literal), a
// variable-bound pipeline (not an inline array literal), an association with a
// dynamic `class_name:`, or any macro outside a `Mongoid::Document` class are
// all left UNRESOLVED — we never fabricate a stage or a join we cannot
// statically see. The inline `Model.collection.aggregate([...])` literal and the
// static association macros are resolved; everything else is skipped.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoidAggPatternType tags the aggregation-pipeline outputs of this pass.
const mongoidAggPatternType = "mongoid_aggregation"

// mongoidRelationPatternType tags the association-relation edges of this pass.
const mongoidRelationPatternType = "mongoid_association"

// mongoidGateRe gates the whole pass: only files that mention Mongoid are
// scanned, so we never poach an arbitrary `.aggregate([...])` chain or a
// `belongs_to` macro from a non-Mongoid (e.g. ActiveRecord) class.
var mongoidGateRe = regexp.MustCompile(`\bMongoid\b`)

// mongoidAggCallRe locates a `<Receiver>.collection.aggregate(` call site. The
// receiver immediately preceding `.collection.aggregate` is the Mongoid model
// class (`Book.collection.aggregate(...)`). Group 1 is the model class name.
var mongoidAggCallRe = regexp.MustCompile(
	`([A-Za-z_][A-Za-z0-9_:]*)\s*\.\s*collection\s*\.\s*aggregate\s*\(`,
)

// mongoidStringFieldReCache caches per-key Ruby string-field matchers.
var mongoidStringFieldReCache = map[string]*regexp.Regexp{}

// mongoidStringField pulls the quoted string value bound to `key` in a Ruby
// hash, accepting BOTH the rocket (`'from' => 'authors'`) and the symbol-colon
// (`from: 'authors'`) separators. The key itself may be quoted (`'from'`) or a
// bare symbol-style identifier (`from`). Returns "" when the key is absent or
// its value is not a plain quoted string literal (e.g. a variable / expression)
// — honest: a dynamic value yields no field.
func mongoidStringField(stage, key string) string {
	re, ok := mongoidStringFieldReCache[key]
	if !ok {
		qk := regexp.QuoteMeta(key)
		re = regexp.MustCompile(
			`(?:['"]` + qk + `['"]|\b` + qk + `)\s*(?:=>|:)\s*['"]([a-zA-Z_][\w$.-]*)['"]`,
		)
		mongoidStringFieldReCache[key] = re
	}
	if m := re.FindStringSubmatch(stage); m != nil {
		return m[1]
	}
	return ""
}

// mongoidParseLookup extracts the join fields from a $lookup / $graphLookup
// stage written in Ruby-hash syntax.
func mongoidParseLookup(stage string) mongoAggLookup {
	return mongoAggLookup{
		from:         mongoidStringField(stage, "from"),
		localField:   mongoidStringField(stage, "localField"),
		foreignField: mongoidStringField(stage, "foreignField"),
		as:           mongoidStringField(stage, "as"),
	}
}

// scanRubyMongoidAggregation walks `src`, finds Mongoid
// `Model.collection.aggregate([...])` call sites, resolves the aggregating
// collection from the model class, parses each inline pipeline stage, and emits
// SCOPE.DataAccess stage entities + $lookup/$graphLookup JOINS_COLLECTION edges.
// Then it scans Mongoid association macros and emits a JOINS_COLLECTION relation
// edge per declared document reference.
func scanRubyMongoidAggregation(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	if !mongoidGateRe.MatchString(src) {
		return
	}

	scanRubyMongoidAggCalls(src, funcs, path, lang, emitStage, emitJoin)
	scanRubyMongoidAssociations(src, emitJoin)
}

// scanRubyMongoidAggCalls handles the `Model.collection.aggregate([...])` path.
func scanRubyMongoidAggCalls(
	src string,
	funcs []funcSpan,
	path string,
	lang string,
	emitStage func(ent types.EntityRecord),
	emitJoin func(rel types.RelationshipRecord),
) {
	for _, m := range mongoidAggCallRe.FindAllStringSubmatchIndex(src, -1) {
		model := src[m[2]:m[3]]
		coll := mongoidCollectionForModel(src, model)
		if coll == "" {
			continue
		}
		openParen := m[1] - 1 // index of '('

		listLiteral := mongoidAggPipelineLiteral(src, openParen)
		if listLiteral == "" {
			continue // dynamic / variable-bound pipeline — honest skip.
		}
		stages := mongoAggSplitStages(listLiteral, 0)
		if len(stages) == 0 {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		callLine := lineOfOffset(src, m[0])

		for idx, st := range stages {
			op := mongoAggFirstKey(st)
			if op == "" {
				continue
			}
			props := map[string]string{
				"pattern_type": mongoidAggPatternType,
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
				lk := mongoidParseLookup(st)
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
				lk := mongoidParseLookup(st)
				if lk.from != "" {
					props["from"] = lk.from
					if lk.as != "" {
						props["as"] = lk.as
					}
					emitJoin(mongoAggJoinEdge(coll, lk, "graphLookup"))
					// #4244 — node-anchored twin (graphLookup stage node).
					emitJoin(mongoAggStageJoinEdge(name, path, lang, lk, "graphLookup"))
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

// mongoidStoreInReCache caches per-model `store_in collection:` matchers.
var mongoidStoreInReCache = map[string]*regexp.Regexp{}

// mongoidCollectionForModel returns the collection token used to anchor the
// aggregating side of a join for the Mongoid model `model`. Preference order:
//
//  1. An explicit `store_in collection: 'books'` / `store_in collection: :books`
//     declaration anywhere in the file (authoritative override of the default).
//  2. The conventional collection derived from the class name — Mongoid
//     pluralises+downcases the class to form the collection, but the shared
//     `capitalisedSingular` (used on the FromID by mongoAggJoinEdge) canonicalises
//     a collection token back to `Class:<Singular>`. To land the FromID on the
//     same `Class:Book` node the model itself produces, we feed the bare class
//     name (last `::` segment) — `capitalisedSingular("Book")` == "Book".
//
// Returns "" only for an empty/garbage model token.
func mongoidCollectionForModel(src, model string) string {
	// Last `::`-segment of a possibly namespaced class (`Catalog::Book` -> `Book`).
	name := model
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	if name == "" {
		return ""
	}
	re, ok := mongoidStoreInReCache[name]
	if !ok {
		// store_in collection: 'books' | "books" | :books  (same-file).
		re = regexp.MustCompile(
			`store_in\s+collection:\s*(?:['"]([a-zA-Z_][\w$.-]*)['"]|:([a-zA-Z_]\w*))`,
		)
		mongoidStoreInReCache[name] = re
	}
	if m := re.FindStringSubmatch(src); m != nil {
		if m[1] != "" {
			return m[1]
		}
		if m[2] != "" {
			return m[2]
		}
	}
	return name
}

// mongoidAggPipelineLiteral returns the full inline pipeline array literal
// `[ ... ]` (brackets included) that is the first argument of an aggregate call
// whose `(` is at `openParen`. Only an inline array literal is resolved; a
// variable-bound pipeline (`aggregate(pipeline)`) is left unresolved — honest.
func mongoidAggPipelineLiteral(src string, openParen int) string {
	i := openParen + 1
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if i >= len(src) || src[i] != '[' {
		return ""
	}
	return mongoidBracketBody(src, i)
}

// mongoidBracketBody returns the full balanced `[...]` literal (brackets
// included) whose opening `[` is at `open`, string- and depth-aware. Returns ""
// if the bracket is unbalanced.
func mongoidBracketBody(src string, open int) string {
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

// ---------------------------------------------------------------------------
// Mongoid association macros (document-reference relations)
// ---------------------------------------------------------------------------

// mongoidClassHeaderRe matches a Ruby class header `class Book` /
// `class Catalog::Book < Something`, capturing the class name (group 1).
var mongoidClassHeaderRe = regexp.MustCompile(`(?m)^[ \t]*class\s+([A-Za-z_][\w:]*)`)

// mongoidDocumentIncludeRe matches the `include Mongoid::Document` marker that
// makes a class a Mongoid document.
var mongoidDocumentIncludeRe = regexp.MustCompile(`include\s+Mongoid::Document`)

// mongoidAssocRe matches a Mongoid association macro and captures the macro name
// (group 1) and the association symbol (group 2):
//
//	belongs_to :author
//	has_many   :books
//	embeds_many :pages, class_name: 'Page'
//	embedded_in :book
var mongoidAssocRe = regexp.MustCompile(
	`(?m)^[ \t]*(belongs_to|has_many|has_one|embeds_many|embeds_one|embedded_in|has_and_belongs_to_many)\s+:([A-Za-z_]\w*)`,
)

// mongoidClassNameOptRe captures an explicit `class_name: 'Author'` /
// `class_name: "Author"` option on an association macro line, which overrides
// the symbol-derived target model name. A dynamic class_name (a variable) does
// not match — honest skip (we fall back to the symbol-derived name).
var mongoidClassNameOptRe = regexp.MustCompile(
	`class_name:\s*['"]([A-Za-z_][\w:]*)['"]`,
)

// scanRubyMongoidAssociations emits a JOINS_COLLECTION relation edge for each
// Mongoid association macro declared inside a `Mongoid::Document` class. The
// owning model is the enclosing class; the target model is the explicit
// `class_name:` option, else the camelised singular of the association symbol.
func scanRubyMongoidAssociations(src string, emitJoin func(rel types.RelationshipRecord)) {
	classes := mongoidDocumentClassSpans(src)
	if len(classes) == 0 {
		return
	}
	seen := make(map[string]bool)
	for _, m := range mongoidAssocRe.FindAllStringSubmatchIndex(src, -1) {
		owner := mongoidEnclosingDocClass(classes, m[0])
		if owner == "" {
			continue // macro outside any Mongoid::Document class — gated out.
		}
		macro := src[m[2]:m[3]]
		sym := src[m[4]:m[5]]

		// The full macro line (to the end of the line) carries the options.
		lineEnd := strings.IndexByte(src[m[0]:], '\n')
		var line string
		if lineEnd < 0 {
			line = src[m[0]:]
		} else {
			line = src[m[0] : m[0]+lineEnd]
		}

		target := ""
		if cm := mongoidClassNameOptRe.FindStringSubmatch(line); cm != nil {
			target = mongoidLastSegment(cm[1])
		} else {
			target = mongoidSymbolToModel(sym, macro)
		}
		from := capitalisedSingular(mongoidLastSegment(owner))
		to := capitalisedSingular(target)
		if from == "" || to == "" || from == to {
			continue
		}
		key := from + "->" + to + ":" + sym
		if seen[key] {
			continue
		}
		seen[key] = true
		emitJoin(types.RelationshipRecord{
			FromID: fmt.Sprintf("Class:%s", from),
			ToID:   fmt.Sprintf("Class:%s", to),
			Kind:   string(types.RelationshipKindJoinsCollection),
			Properties: map[string]string{
				"pattern_type": mongoidRelationPatternType,
				"via":          macro,
				"association":  sym,
			},
		})
	}
}

// mongoidDocClassSpan is the [start,end) byte span of a class that includes
// Mongoid::Document, together with its class name.
type mongoidDocClassSpan struct {
	name  string
	start int
	end   int
}

// mongoidDocumentClassSpans locates every `class X ... include Mongoid::Document`
// block and computes its body span (header start to the next class header at the
// same-or-shallower position, else EOF) so an association macro can be attributed
// to the owning model. Only classes that actually include Mongoid::Document are
// returned — a plain ActiveRecord class is excluded.
func mongoidDocumentClassSpans(src string) []mongoidDocClassSpan {
	headers := mongoidClassHeaderRe.FindAllStringSubmatchIndex(src, -1)
	var spans []mongoidDocClassSpan
	for i, h := range headers {
		start := h[0]
		end := len(src)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		// Only a class whose body includes Mongoid::Document is a Mongoid doc.
		if !mongoidDocumentIncludeRe.MatchString(src[start:end]) {
			continue
		}
		spans = append(spans, mongoidDocClassSpan{
			name:  src[h[2]:h[3]],
			start: start,
			end:   end,
		})
	}
	return spans
}

// mongoidEnclosingDocClass returns the name of the Mongoid document class whose
// span contains `pos`, or "" if none.
func mongoidEnclosingDocClass(spans []mongoidDocClassSpan, pos int) string {
	for _, s := range spans {
		if pos >= s.start && pos < s.end {
			return s.name
		}
	}
	return ""
}

// mongoidLastSegment returns the last `::`-separated segment of a (possibly
// namespaced) class name: `Catalog::Book` -> `Book`.
func mongoidLastSegment(name string) string {
	if i := strings.LastIndex(name, "::"); i >= 0 {
		return name[i+2:]
	}
	return name
}

// mongoidSymbolToModel converts an association symbol to its target model name.
// Collection macros (has_many / embeds_many / has_and_belongs_to_many) take a
// plural symbol, so the symbol is singularised; reference macros (belongs_to /
// has_one / embeds_one / embedded_in) already name a singular. The result is
// camelised: `books` -> `Book`, `author` -> `Author`, `blog_posts` ->
// `BlogPost`. capitalisedSingular (applied by the caller) is a no-op on an
// already-singular CamelCase token.
func mongoidSymbolToModel(sym, macro string) string {
	base := sym
	switch macro {
	case "has_many", "embeds_many", "has_and_belongs_to_many":
		base = mongoidSingularise(base)
	}
	return mongoidCamelise(base)
}

// mongoidSingularise applies the same lightweight pluralisation rules as
// capitalisedSingular (ies->y, ses->s, trailing-s drop) to a lower_snake symbol.
func mongoidSingularise(s string) string {
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ses") && len(s) > 3:
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && len(s) > 1:
		return s[:len(s)-1]
	}
	return s
}

// mongoidCamelise turns a lower_snake_case association symbol into a CamelCase
// model name: `blog_post` -> `BlogPost`, `author` -> `Author`.
func mongoidCamelise(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}
