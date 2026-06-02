package python

// graphql_dataloader.go — Python GraphQL DataLoader N+1 batch-loader
// extraction (#3624, epic #3607).
//
// GraphQL servers in Python (Strawberry, Ariadne, Graphene) avoid the classic
// N+1 resolver problem with the `aiodataloader` package: a DataLoader batches
// the per-key loads issued by sibling field resolvers into a single call to a
// user-supplied async batch function.
//
//	from aiodataloader import DataLoader
//
//	async def batch_users(keys):
//	    rows = await db.fetch_users(keys)
//	    return [rows.get(k) for k in keys]
//
//	user_loader = DataLoader(batch_fn=batch_users)   # or DataLoader(batch_users)
//
//	@strawberry.field
//	async def author(self, info) -> User:
//	    return await user_loader.load(self.author_id)   # batched fetch
//
// This extractor records the N+1-avoidance wiring (cross-language consistent
// with the JS/TS dataloader pass):
//
//	target  : SCOPE.DataLoader  subtype "dataloader"
//	          Name = the LHS variable the loader is assigned to (e.g. "user_loader")
//	edge 1  : BATCHES   loader → batch function   (the load_fn / first positional)
//	edge 2  : USES      resolver → loader         (each `<loader>.load(id)` site,
//	                                                attached to the enclosing def)
//
// Both edges carry Properties["via"] = "graphql_dataloader".
//
// Honest-partial: only loaders assigned to a simple module/class-level name are
// captured, and the batch function is recorded only when it is a bare name
// (`load_fn=batch_users` or `DataLoader(batch_users)`); a lambda or inline
// callable yields a loader entity with no BATCHES edge. A `.load()` call whose
// enclosing `def` cannot be resolved (e.g. module top-level) is skipped.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_graphql_dataloader", &GraphQLDataLoaderExtractor{})
}

// GraphQLDataLoaderExtractor extracts aiodataloader DataLoader instances and
// the resolver→loader / loader→batch-fn wiring around them.
type GraphQLDataLoaderExtractor struct{}

func (e *GraphQLDataLoaderExtractor) Language() string { return "python_graphql_dataloader" }

var (
	// dlAssignRe matches `  user_loader = DataLoader(...)` capturing the LHS
	// name (group 1) and the constructor argument body (group 2). Anchored to
	// line start (with optional indentation) so `self.user_loader = ...` and
	// bare-name forms both match via the LHS group, which tolerates a trailing
	// attribute. Only the trailing identifier of the LHS is used as the name.
	dlAssignRe = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_][\w.]*)\s*=\s*DataLoader\(([^\n]*)\)`)

	// dlLoadFnKwRe pulls `load_fn=batch_users` or `batch_load_fn=batch_users`
	// out of a constructor body (group 1 = the batch function name).
	dlLoadFnKwRe = regexp.MustCompile(`(?:load_fn|batch_load_fn)\s*=\s*([A-Za-z_]\w*)`)

	// dlFirstPosRe pulls a leading bare positional argument (`DataLoader(batch_users)`)
	// — group 1 = the batch function name. Requires the arg to be a plain name
	// not followed by `=` (which would make it a keyword) or `(` (a call).
	dlFirstPosRe = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*(?:,|$)`)

	// dlLoadCallRe matches a `<loader>.load(` or `<loader>.load_many(` call,
	// capturing the receiver expression (group 1) and the load method (group 2).
	// The receiver may be dotted (`self.user_loader`, `info.context.user_loader`);
	// only its trailing identifier is matched against known loaders.
	dlLoadCallRe = regexp.MustCompile(`([A-Za-z_][\w.]*)\.(load|load_many)\(`)

	// pyDefRe matches a python `def` / `async def` line, capturing leading
	// indentation (group 1) and the function name (group 2).
	pyDefRe = regexp.MustCompile(`(?m)^([ \t]*)(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`)
)

func (e *GraphQLDataLoaderExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_graphql_dataloader")
	_, span := tracer.Start(ctx, "custom.python_graphql_dataloader")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	source := string(file.Content)
	// Fast-path gate: the file must mention DataLoader and import aiodataloader
	// (or the Strawberry dataloader shim). Avoids matching unrelated classes
	// named DataLoader.
	if !strings.Contains(source, "DataLoader") {
		return nil, nil
	}
	if !strings.Contains(source, "aiodataloader") &&
		!strings.Contains(source, "strawberry.dataloader") &&
		!strings.Contains(source, "from strawberry import dataloader") {
		return nil, nil
	}

	var out []types.EntityRecord

	// 1. Loader entities + BATCHES edges. Record the trailing-identifier name of
	//    each loader so that load()-site USES edges can be gated to known loaders.
	loaderNames := make(map[string]bool)
	loaderLines := make(map[string]int)
	for _, m := range dlAssignRe.FindAllStringSubmatchIndex(source, -1) {
		lhs := source[m[2]:m[3]]
		body := source[m[4]:m[5]]
		line := lineOf(source, m[0])

		name := lhs
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if name == "" {
			continue
		}
		loaderNames[name] = true
		loaderLines[name] = line

		props := map[string]string{
			"via":         "graphql_dataloader",
			"loader_name": name,
			"language":    "python",
		}
		var rels []types.RelationshipRecord
		if batchFn := pyBatchFnName(body); batchFn != "" {
			props["batch_fn"] = batchFn
			rels = append(rels, types.RelationshipRecord{
				ToID: batchFn,
				Kind: string(types.RelationshipKindBatches),
				Properties: map[string]string{
					"via":      "graphql_dataloader",
					"language": "python",
				},
			})
		}
		ld := entity(name, string(types.EntityKindDataLoader), "dataloader", file.Path, line, props)
		ld.Relationships = rels
		out = append(out, ld)
	}

	if len(loaderNames) == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	// 2. resolver→loader USES edges. For each `<loader>.load(id)` call, attach a
	//    USES edge to the enclosing def. One carrier entity per (def-line) so
	//    multiple loads in the same resolver converge on one owner.
	defs := pyDefIndex(source)
	type carrier struct {
		name string
		line int
		uses map[string]bool
	}
	carriers := make(map[int]*carrier) // keyed by def start line
	for _, m := range dlLoadCallRe.FindAllStringSubmatchIndex(source, -1) {
		recv := source[m[2]:m[3]]
		name := recv
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if !loaderNames[name] {
			continue
		}
		callLine := lineOf(source, m[0])
		d := enclosingDef(defs, callLine)
		if d == nil {
			continue // top-level load() — no resolver to anchor the edge.
		}
		c := carriers[d.line]
		if c == nil {
			c = &carrier{name: d.name, line: d.line, uses: make(map[string]bool)}
			carriers[d.line] = c
		}
		c.uses[name] = true
	}

	for _, c := range carriers {
		ownerRef := fmt.Sprintf("dataloader_resolver:%s:%d", file.Path, c.line)
		owner := entity(ownerRef, "SCOPE.Operation", "resolver", file.Path, c.line, map[string]string{
			"via":           "graphql_dataloader",
			"resolver_name": c.name,
			"language":      "python",
		})
		for loaderName := range c.uses {
			owner.Relationships = append(owner.Relationships, types.RelationshipRecord{
				ToID: loaderName,
				Kind: string(types.RelationshipKindUses),
				Properties: map[string]string{
					"via":      "graphql_dataloader",
					"loader":   loaderName,
					"language": "python",
				},
			})
		}
		out = append(out, owner)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// pyBatchFnName resolves the batch function name from a DataLoader constructor
// body. Prefers a `load_fn=` / `batch_load_fn=` keyword; falls back to a bare
// leading positional name. Returns "" for lambdas / inline callables.
func pyBatchFnName(body string) string {
	if km := dlLoadFnKwRe.FindStringSubmatch(body); km != nil {
		return km[1]
	}
	if pm := dlFirstPosRe.FindStringSubmatch(body); pm != nil {
		// Guard: a bare leading name that is immediately a kwarg (`foo=`) or a
		// call (`foo(`) is not a batch-fn positional. dlFirstPosRe already
		// requires `,` or end-of-string after the name, so `foo=` / `foo(`
		// won't match. Also exclude obvious non-fn keywords.
		switch pm[1] {
		case "max_batch_size", "cache", "cache_key_fn", "load_fn", "batch_load_fn":
			return ""
		}
		return pm[1]
	}
	return ""
}

// pyDef is a single python function definition: its name, start line (1-based),
// and indentation width (number of leading spaces/tabs).
type pyDef struct {
	name   string
	line   int
	indent int
}

// pyDefIndex returns all `def` / `async def` definitions in source, in source
// order, with their 1-based line numbers and indentation widths.
func pyDefIndex(source string) []pyDef {
	var defs []pyDef
	for _, m := range pyDefRe.FindAllStringSubmatchIndex(source, -1) {
		indent := m[3] - m[2]
		name := source[m[4]:m[5]]
		defs = append(defs, pyDef{name: name, line: lineOf(source, m[0]), indent: indent})
	}
	return defs
}

// enclosingDef returns the innermost def whose body contains callLine: the
// last def at or before callLine. Honest-partial — it uses source order plus
// line position rather than full block-scope analysis, which is sufficient for
// the common single-resolver-per-def GraphQL shape. Returns nil when callLine
// precedes every def (module top-level).
func enclosingDef(defs []pyDef, callLine int) *pyDef {
	var best *pyDef
	for i := range defs {
		if defs[i].line > callLine {
			break
		}
		best = &defs[i]
	}
	return best
}
