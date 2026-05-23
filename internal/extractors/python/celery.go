// celery.go — Celery decorator + dispatch-edge enrichment (#1979, #1980).
//
// The base extractor emits a vanilla SCOPE.Operation entity for every
// `def <name>` it walks. When the def is decorated with @shared_task /
// @app.task / @celery.task, two facets are missing today:
//
//  1. The entity carries no `is_task: true` marker and none of the
//     operationally-relevant decorator kwargs (`bind`, `max_retries`,
//     `default_retry_delay`, `autoretry_for`, `retry_backoff`, `name`,
//     `queue`, `routing_key`, `serializer`) — so docgen cannot describe
//     retry policy / routing without re-parsing the source.
//
//  2. The custom Celery extractor in internal/custom/python/celery.go
//     emits a SEPARATE SCOPE.Service/task entity at the decorator line,
//     producing TWO graph entities for ONE function declaration. Search
//     results are confusing and edge endpoints split across two nodes.
//
// This file closes both gaps by running as a post-extraction pass that:
//   - Finds every @shared_task / @app.task / @celery.task decorator in the
//     file and matches it to the SCOPE.Operation entity emitted at the
//     def line directly below.
//   - Stamps `is_task: "true"`, `framework: "celery"`, and the full set of
//     decorator kwargs onto the Operation's Properties.
//   - Computes `task_name` from the explicit `name=` kwarg or, when absent,
//     the default routing key `<module>.<function>` (Celery's default).
//   - Emits a CALLS edge from each `<task>.delay(...)` / `.apply_async(...)`
//     / `.s(...)` / `.si(...)` call site's enclosing function to the task.
//     This is the SAME-FILE counterpart of the cross-file pass in
//     internal/engine/django_signal_pubsub_edges.go (ApplyCeleryDispatchEdges):
//     the engine pass needs every file in the repo to resolve cross-module
//     dispatch; this pass runs per-file and emits edges that the resolver
//     can bind locally even before the engine pass runs. Both passes
//     dedupe on (caller, callee).
//
// The duplicate Task entity emitted by the custom extractor is NOT removed
// here (extractors are append-only by contract); it is folded by the index
// builder's foldClassHierarchyShadows / kind-priority logic which already
// dedups same-source-location entities. Annotating the Operation entity
// with `is_task: "true"` ensures downstream consumers can pick the
// Operation form as the canonical task representation regardless of
// fold order.
package python

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// celeryDecoratorRe matches a Celery task decorator directly above a def
// (possibly with intervening stacked decorators) and captures the def name.
// Group 1 = full decorator kwargs string (the `(...)` payload, or "" when
// the decorator is bare). Group 2 = def name.
//
// Recognised decorator shapes:
//
//	@shared_task
//	@shared_task(bind=True, max_retries=3)
//	@app.task
//	@app.task(queue="default")
//	@celery.task
//	@celery.task(name="myapp.tasks.foo")
//	@myapp_celery.task(...)
// celeryDecoratorHeadRe matches the @<celery-decorator> head (no
// arguments). It is used to locate every Celery decorator start position;
// matchCeleryDecorator then extracts the optional balanced `(...)` body
// and the following def name via a small hand-rolled scanner so that
// nested parens (autoretry_for=(Exception,), tuple args, ...) parse
// correctly. A pure regex cannot match balanced parens.
var celeryDecoratorHeadRe = regexp.MustCompile(
	`(?m)^\s*@(?:shared_task|[\w.]+\.task)\b`,
)

// celeryDecoratorDefRe matches the def line (possibly with intervening
// stacked decorators on their own lines) that follows a Celery decorator.
// Group 1 = def name.
var celeryDecoratorDefRe = regexp.MustCompile(
	`(?m)^\s*(?:@[^\n]*\n\s*)*(?:async\s+)?def\s+(\w+)\s*\(`,
)

// celeryDispatchCallRe matches a Celery dispatch call site. Group 1 = the
// receiver chain ending in the task identifier (we use only the final
// dotted segment as the task name candidate); group 2 = dispatch method.
var celeryDispatchCallRe = regexp.MustCompile(
	`(?m)\b([\w.]+?)\.(delay|apply_async|s|si)\s*\(`,
)

// Decorator-kwarg sub-patterns. Each captures group 1 = value (raw token
// or quoted string contents).
var (
	celeryKwBindRe          = regexp.MustCompile(`\bbind\s*=\s*(True|False)`)
	celeryKwMaxRetriesRe    = regexp.MustCompile(`\bmax_retries\s*=\s*(\d+|None)`)
	celeryKwDefaultDelayRe  = regexp.MustCompile(`\bdefault_retry_delay\s*=\s*(\d+)`)
	celeryKwAutoretryRe     = regexp.MustCompile(`\bautoretry_for\s*=\s*\(([^)]*)\)`)
	celeryKwRetryBackoffRe  = regexp.MustCompile(`\bretry_backoff\s*=\s*(True|False|\d+)`)
	celeryKwNameRe          = regexp.MustCompile(`\bname\s*=\s*["']([^"']+)["']`)
	celeryKwQueueRe         = regexp.MustCompile(`\bqueue\s*=\s*["']([^"']+)["']`)
	celeryKwRoutingKeyRe    = regexp.MustCompile(`\brouting_key\s*=\s*["']([^"']+)["']`)
	celeryKwSerializerRe    = regexp.MustCompile(`\bserializer\s*=\s*["']([^"']+)["']`)
)

// applyCeleryAnnotations runs the post-extraction Celery annotation +
// dispatch-edge pass for one file. Safe to call on every Python file;
// short-circuits when the source contains no Celery decorators.
func applyCeleryAnnotations(file extractor.FileInput, entities *[]types.EntityRecord) {
	if entities == nil || len(*entities) == 0 {
		return
	}
	src := string(file.Content)
	if len(src) == 0 {
		return
	}

	hasDecorator := strings.Contains(src, "shared_task") ||
		strings.Contains(src, ".task")
	hasDispatch := strings.Contains(src, ".delay(") ||
		strings.Contains(src, ".apply_async(") ||
		strings.Contains(src, ".s(") ||
		strings.Contains(src, ".si(")
	if !hasDecorator && !hasDispatch {
		return
	}

	mod := filePathToModule(file.Path)
	taskNames := map[string]bool{}

	// 1. Annotate every Operation entity that sits at a @shared_task /
	//    @app.task / @celery.task decorated def.
	if hasDecorator {
		for _, head := range celeryDecoratorHeadRe.FindAllStringIndex(src, -1) {
			// head[1] points just past the decorator name. Optional `(...)`
			// kwargs body lives at head[1] (possibly preceded by whitespace).
			kwargs := ""
			scan := head[1]
			// Skip horizontal whitespace.
			for scan < len(src) && (src[scan] == ' ' || src[scan] == '\t') {
				scan++
			}
			if scan < len(src) && src[scan] == '(' {
				end, ok := matchBalancedParen(src, scan)
				if !ok {
					continue
				}
				kwargs = src[scan+1 : end]
				scan = end + 1
			}
			// Advance to next def line, skipping any stacked decorators.
			defMatch := celeryDecoratorDefRe.FindStringSubmatchIndex(src[scan:])
			if defMatch == nil {
				continue
			}
			defName := src[scan+defMatch[2] : scan+defMatch[3]]
			taskNames[defName] = true

			props := parseCeleryDecoratorKwargs(kwargs)
			props["is_task"] = "true"
			props["framework"] = "celery"
			// task_name — explicit `name=` wins; otherwise fall back to
			// Celery's default routing key `<module>.<function>`.
			if explicit, ok := props["name"]; ok && explicit != "" {
				props["task_name"] = explicit
			} else if mod != "" {
				props["task_name"] = mod + "." + defName
			} else {
				props["task_name"] = defName
			}

			// Find the matching Operation entity. Match by leaf name on
			// this file. When multiple methods named the same exist (rare
			// for tasks since module-level functions dominate), the first
			// hit wins — there is no reliable line match because the regex
			// matches the decorator line, not the def line.
			for i := range *entities {
				e := &(*entities)[i]
				if e.Kind != "SCOPE.Operation" || e.SourceFile != file.Path {
					continue
				}
				leaf := e.Name
				if dot := strings.LastIndexByte(leaf, '.'); dot >= 0 {
					leaf = leaf[dot+1:]
				}
				if leaf != defName {
					continue
				}
				if e.Properties == nil {
					e.Properties = map[string]string{}
				}
				for k, v := range props {
					if _, exists := e.Properties[k]; !exists {
						e.Properties[k] = v
					}
				}
				break
			}
		}
	}

	// 2. Emit CALLS edges for in-file dispatch calls. The cross-file pass
	//    (engine.ApplyCeleryDispatchEdges) handles the much more common
	//    case of `tasks/foo.py` defining the task and `views/x.py`
	//    dispatching it; this pass closes the same-file gap so individual
	//    test fixtures can assert on the edge without running the whole
	//    repo through the engine.
	if !hasDispatch || len(taskNames) == 0 {
		return
	}
	for _, idx := range celeryDispatchCallRe.FindAllStringSubmatchIndex(src, -1) {
		chain := src[idx[2]:idx[3]]
		// Use the final dotted segment as the task-name candidate so both
		// `process_change.delay()` and `tasks.process_change.delay()`
		// resolve to the same task.
		parts := strings.Split(chain, ".")
		taskName := parts[len(parts)-1]
		if !taskNames[taskName] {
			continue
		}
		caller := enclosingPyFunction(src, idx[0])
		if caller == "" || caller == taskName {
			continue
		}
		callerIdx := findOperationByLeafName(*entities, file.Path, caller)
		if callerIdx < 0 {
			continue
		}
		// Target ToID is a structural ref to the task Operation by file +
		// name; the resolver binds it via byLocation.
		toID := extractor.BuildOperationStructuralRef("python", file.Path, taskName)

		// Dedup by (caller, target).
		dup := false
		for _, r := range (*entities)[callerIdx].Relationships {
			if r.Kind == "CALLS" && r.ToID == toID {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		(*entities)[callerIdx].Relationships = append((*entities)[callerIdx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "CALLS",
				Properties: map[string]string{
					"language":     "python",
					"framework":    "celery",
					"pattern_type": "celery_dispatch",
					"dispatch":     "async",
					"method":       src[idx[4]:idx[5]],
				},
			})
	}
}

// matchBalancedParen returns the byte offset of the closing `)` that
// matches the opening `(` at openIdx in src. Tracks single and double
// quoted string literals so parens inside strings do not unbalance the
// count. Returns (0, false) when the open paren is unbalanced.
func matchBalancedParen(src string, openIdx int) (int, bool) {
	if openIdx < 0 || openIdx >= len(src) || src[openIdx] != '(' {
		return 0, false
	}
	depth := 0
	i := openIdx
	for i < len(src) {
		c := src[i]
		switch c {
		case '"', '\'':
			// Skip the string literal — match up to the next unescaped quote
			// of the same type. Triple-quoted strings are not handled here
			// because decorator-kwargs never use them in practice.
			quote := c
			i++
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				if src[i] == quote {
					break
				}
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
		i++
	}
	return 0, false
}

// parseCeleryDecoratorKwargs parses the inner kwargs string of a Celery
// task decorator and returns the operationally-relevant properties.
// Recognised kwargs (any subset present is captured):
//
//	bind, max_retries, default_retry_delay, autoretry_for, retry_backoff,
//	name, queue, routing_key, serializer
//
// Values are stored as raw strings (the tokens as written in source) so
// downstream consumers can reproduce them verbatim. Missing kwargs are
// simply absent from the map.
func parseCeleryDecoratorKwargs(args string) map[string]string {
	props := map[string]string{}
	if args == "" {
		return props
	}
	if m := celeryKwBindRe.FindStringSubmatch(args); m != nil {
		props["bind"] = m[1]
	}
	if m := celeryKwMaxRetriesRe.FindStringSubmatch(args); m != nil {
		props["max_retries"] = m[1]
	}
	if m := celeryKwDefaultDelayRe.FindStringSubmatch(args); m != nil {
		props["default_retry_delay"] = m[1]
	}
	if m := celeryKwAutoretryRe.FindStringSubmatch(args); m != nil {
		// Strip whitespace; the captured group already excludes parens.
		props["autoretry_for"] = strings.TrimSpace(m[1])
	}
	if m := celeryKwRetryBackoffRe.FindStringSubmatch(args); m != nil {
		props["retry_backoff"] = m[1]
	}
	if m := celeryKwNameRe.FindStringSubmatch(args); m != nil {
		props["name"] = m[1]
	}
	if m := celeryKwQueueRe.FindStringSubmatch(args); m != nil {
		props["queue"] = m[1]
	}
	if m := celeryKwRoutingKeyRe.FindStringSubmatch(args); m != nil {
		props["routing_key"] = m[1]
	}
	if m := celeryKwSerializerRe.FindStringSubmatch(args); m != nil {
		props["serializer"] = m[1]
	}
	return props
}
