// Django ORM field-access edge synthesis (issue #2279).
//
// Phase A: lift the `filter_keys` property bag that applyORMQueries already
// records on every Django ORM call site (internal/engine/orm_queries.go)
// into first-class graph edges between the call site and the targeted
// SCOPE.Schema(subtype=field) entity emitted by the Python extractor
// (internal/extractors/python/extractor.go:1411-1421).
//
// Architecture mirrors the peer engine passes (orm_queries.go,
// django_drf_actions.go): a single function called from detector.go that
// walks pre-existing detector output and APPENDS new edges. Append-only —
// never modifies or removes existing entities/edges, so it cannot regress
// the surrounding pipeline's bug-rate on files that contain no ORM calls.
//
// Edge selection (verb → kind), derived from the orm_queries `operation`
// canonicalisation:
//
//   - find / aggregate (filter, get, exclude, values, values_list,
//     order_by, annotate, select_related, prefetch_related, all)
//     → READS_FIELD
//   - create / update (update, create, save, update_or_create,
//     bulk_create, bulk_update)
//     → WRITES_FIELD
//   - delete → skipped (filter_keys on `.filter().delete()` were really
//     filter intent; the brief explicitly excludes delete from this pass)
//
// Field resolution is strictly intra-file in Phase A: we look for a
// SCOPE.Schema(subtype=field) entity whose Name is `<Model>.<field>`
// (the convention set at extractor.go:1411-1412). If the model can't be
// resolved or the field can't be found on that model, the edge is
// SKIPPED silently — no dangling edges, no panics. Phase B will extend
// to cross-file lookups + a full obj.attr attribute-access scanner.
//
// Django lookup-suffix stripping handles the field traversal grammar
// documented at https://docs.djangoproject.com/en/stable/ref/models/querysets/#field-lookups
// (`__icontains`, `__in`, `__gte`, `__lt`, `__isnull`, etc.) AND
// relation traversals (`author__name` — the FIRST segment is the local
// field; the remainder crosses relations and is out of scope here).
//
// Refs #2279. Closes the orphan class on ORM-only field references:
// bench Q08 (User.cognito_id usage) was unanswerable structurally with
// 60+ grep hits and 0 graph nodes/edges before this PR.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ormReadsFieldKind / ormWritesFieldKind are the directed-edge Kinds
// emitted by this pass. Aliased through the types package so the typed
// enum stays canonical (see kinds.go:RelationshipKindReadsField).
var (
	ormReadsFieldKind  = string(types.RelationshipKindReadsField)
	ormWritesFieldKind = string(types.RelationshipKindWritesField)
)

// ormFieldEdgesPatternType is the pattern_type property attached to
// every emitted READS_FIELD / WRITES_FIELD edge; matches the existing
// pattern_type convention used by orm_queries.go.
const ormFieldEdgesPatternType = "orm_field_edges"

// applyORMFieldEdges runs after applyORMQueries (which emits the QUERIES
// edges this pass pivots off of) and APPENDS one READS_FIELD or
// WRITES_FIELD edge per (call site, resolved field) pair.
//
// `lang` is currently honoured only for "python" — Django ORM is the
// Phase A scope. Other languages no-op cleanly; Phase B will extend
// the pass to JS/TS Prisma, Java JPA, etc.
func applyORMFieldEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	pass1Entities := args.Pass1Entities
	entities := args.Entities
	relationships := args.Relationships
	if lang != "python" {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	// Build a per-(model,field) index for THIS file.
	//
	// Preferred source: Pass 1 SCOPE.Schema(subtype=field) entities plumbed
	// in via FileInput.Pass1Entities (issue #2352). These are the canonical
	// records the Python extractor emits at python/extractor.go:1411-1421
	// — exact, no regex re-parse.
	//
	// Fallback: regex scan via BuildFieldIndex. Triggered when the
	// side-channel is empty, which happens for (a) test fixtures that
	// construct FileInput directly without going through Pass 1, and
	// (b) the subprocess-extract path that merges Pass 1+2.5 outside the
	// per-file detector loop.
	fieldIdx := buildPlumbedPythonORMFieldIndex(path, pass1Entities)
	plumbed := len(fieldIdx) > 0
	if !plumbed {
		fieldIdx = BuildFieldIndex(string(content))
	}
	_ = plumbed // reserved for future telemetry / debug hooks

	// Phase B (issue #2448): cross-file resolution via the closure attached
	// by the coordinator. When the model is defined in a SIBLING file
	// (canonical Django split — models.py defines User, views.py imports
	// it), the intra-file fieldIdx misses; this closure lets us recover
	// the field set without re-parsing the whole repo. Nil is valid —
	// direct test fixtures that don't go through the coordinator path
	// see the pre-#2448 intra-file-only behaviour.
	crossFile := args.CrossFileFields
	resolveCrossFile := func(modelName, field string) bool {
		if crossFile == nil {
			return false
		}
		ents := crossFile(modelName)
		if len(ents) == 0 {
			return false
		}
		needle := modelName + "." + field
		for _, e := range ents {
			if e.Kind != "SCOPE.Schema" || e.Subtype != "field" {
				continue
			}
			if e.Name == needle {
				return true
			}
		}
		return false
	}

	// Early-exit only when BOTH the intra-file index and the cross-file
	// lookup are unavailable. Previously this short-circuited on an empty
	// intra-file index — that's wrong under Phase B because views.py
	// (no models) legitimately needs the cross-file branch to fire.
	if len(fieldIdx) == 0 && crossFile == nil {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	// Scan the source directly for Django ORM call chains. We can't
	// rely on the QUERIES edges' filter_keys alone because orm_queries
	// records kwargs from the FIRST call in a chain (`.filter(id=…)`)
	// — chain-terminal writes (`.filter(id=…).update(cognito_id=…)`)
	// would otherwise lose the write kwargs.
	//
	// Pivot: locate every `<Model>.objects` anchor in the source, then
	// walk forward through the chain extracting each `.<verb>(<args>)`
	// link. The model name is fixed from the anchor; per-link we
	// classify the verb and emit one edge per resolved kwarg.
	src := string(content)
	funcs := indexEnclosingFunctions("python", src)
	for _, m := range djangoChainAnchorRe.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		// `m[1]` is the offset just past `<Model>.objects`. The chain
		// continues with one or more `.<verb>(...)` calls.
		caller := enclosingFuncAt(funcs, m[0])
		fromID := buildCallerID("python", caller, path)
		for _, link := range walkChainLinks(src, m[1]) {
			edgeKind := ormFieldEdgeKindForVerb(link.verb)
			if edgeKind == "" {
				continue
			}
			for _, key := range extractKwargKeys(link.args) {
				field := stripDjangoLookup(key)
				if field == "" {
					continue
				}
				fqName := modelName + "." + field
				resolution := ""
				if fieldIdx[fqName] {
					resolution = "intra_file"
				} else if resolveCrossFile(modelName, field) {
					resolution = "cross_file"
				}
				if resolution == "" {
					continue
				}
				relationships = append(relationships, types.RelationshipRecord{
					FromID: fromID,
					ToID:   "Class:" + fqName,
					Kind:   edgeKind,
					Properties: map[string]string{
						"orm":          "django",
						"verb":         link.verb,
						"field":        field,
						"model":        modelName,
						"resolution":   resolution,
						"pattern_type": ormFieldEdgesPatternType,
					},
				})
			}
		}
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// djangoChainAnchorRe matches the start of every Django ORM queryset
// chain: `<Model>.objects`. The chain that follows is consumed by
// walkChainLinks.
var djangoChainAnchorRe = regexp.MustCompile(
	`\b([A-Z][A-Za-z0-9_]*)\.objects\b`,
)

// chainLink is one `.verb(args)` segment of a Django ORM queryset chain.
type chainLink struct {
	verb string
	args string
}

// walkChainLinks parses the QuerySet method chain starting at `pos` and
// returns every `.<verb>(<args>)` segment until the chain ends (newline
// not followed by a continuation, semicolon, or non-chain character).
//
// Defensive: bounded at 8 KB of source to avoid runaway scans on
// pathological inputs. Args strings are returned WITHOUT the wrapping
// parens; callers should pass them to extractKwargKeys.
func walkChainLinks(src string, pos int) []chainLink {
	var out []chainLink
	end := pos + 8192
	if end > len(src) {
		end = len(src)
	}
	for pos < end {
		// Skip whitespace + newlines + line-continuation backslashes so
		// chains broken across lines (`.filter(\n    id=1\n).update(...)`)
		// are still walked.
		for pos < end {
			c := src[pos]
			if c == ' ' || c == '\t' || c == '\n' || c == '\\' || c == '\r' {
				pos++
				continue
			}
			break
		}
		if pos >= end || src[pos] != '.' {
			break
		}
		pos++ // consume '.'
		// Verb identifier.
		vs := pos
		for pos < end {
			c := src[pos]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '_' {
				pos++
				continue
			}
			break
		}
		if pos == vs {
			break
		}
		verb := src[vs:pos]
		// Optional whitespace before the opening paren.
		for pos < end && (src[pos] == ' ' || src[pos] == '\t') {
			pos++
		}
		if pos >= end || src[pos] != '(' {
			// Property access without a call (e.g. `.objects` itself):
			// not a chain link we can act on. Stop — anything further
			// without parens is unlikely to be a verb invocation.
			break
		}
		args := matchCall(src, pos, 4096)
		// Advance past the matched call. matchCall returns the inner
		// args; we need to find the matching `)` ourselves to continue.
		closeIdx := pos + 1 + len(args)
		if closeIdx >= end || src[closeIdx] != ')' {
			// Unbalanced — bail out, keep what we have.
			out = append(out, chainLink{verb: verb, args: args})
			break
		}
		pos = closeIdx + 1
		out = append(out, chainLink{verb: verb, args: args})
	}
	return out
}

// extractKwargKeys parses a raw call-args blob like `cognito_id="x", name="y"`
// and returns the kwarg identifiers (LHS of each `=`). Returns nil for
// empty / non-kwarg args (e.g. positional-only).
//
// Note: deliberately simpler than orm_queries.parseFilterKeys — that
// helper drops ORM-internal keys like `where` / `select` (Prisma /
// Sequelize). Django ORM does not use those names, and stripping them
// here would also accept `__hidden=True` (an underscore-prefixed
// kwarg) as a "key" — we want every kwarg LHS through, the lookup-suffix
// step below normalises traversals.
var kwargKeyRe = regexp.MustCompile(`(?:^|[,(\s])([a-zA-Z_][a-zA-Z0-9_]*)\s*=`)

func extractKwargKeys(args string) []string {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	matches := kwargKeyRe.FindAllStringSubmatch(args, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		k := m[1]
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// ormFieldEdgeKindForVerb maps a Django ORM queryset verb to the
// field-access edge kind. Returns "" for verbs that should not emit
// a field edge in Phase A (delete; non-ORM methods).
//
// Read verbs come straight from the brief:
//
//	filter, get, exclude, values, values_list, order_by, annotate,
//	select_related, prefetch_related
//
// Write verbs likewise:
//
//	update, create, save, update_or_create, bulk_create, bulk_update
//
// `delete` is intentionally excluded: the brief does not list it under
// either category, and its kwargs (when present) are normally chained
// filters that already match a separate `.filter()` link in the
// queryset chain — so we'd double-count.
func ormFieldEdgeKindForVerb(verb string) string {
	switch verb {
	case "filter", "get", "exclude", "values", "values_list",
		"order_by", "annotate", "select_related", "prefetch_related":
		return ormReadsFieldKind
	case "update", "create", "save", "update_or_create",
		"bulk_create", "bulk_update":
		return ormWritesFieldKind
	}
	return ""
}

// stripDjangoLookup strips Django lookup suffixes and relation
// traversals from a filter-keys token and returns the LOCAL field name.
//
// Examples:
//
//	cognito_id              → cognito_id
//	cognito_id__isnull      → cognito_id
//	name__icontains         → name
//	author__name            → author       (FIRST segment; remainder
//	                                        crosses relations — Phase B)
//	created_at__date__gte   → created_at
//	__hidden                → ""           (defensive: empty / malformed)
//
// We split on the first `__` separator and keep the head, matching the
// Django field-lookup grammar at
// https://docs.djangoproject.com/en/stable/ref/models/querysets/#field-lookups
func stripDjangoLookup(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if idx := strings.Index(key, "__"); idx >= 0 {
		key = key[:idx]
	}
	// Defensive: trailing single underscores are legal in Python ids;
	// only an entirely empty head (e.g. raw "__foo" → "") is rejected.
	if key == "" {
		return ""
	}
	return key
}
