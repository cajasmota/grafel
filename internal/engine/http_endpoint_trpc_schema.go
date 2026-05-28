// tRPC input-schema extraction (#2865). The procedure synthesizer
// (synthesizeTRPC) emits one http_endpoint_definition per leaf procedure but
// records only its verb + dotted path — it says nothing about the procedure's
// INPUT SCHEMA, i.e. the `.input(z.object({ … }))` validator that defines the
// request contract. That left schema_extraction only partial: procedures were
// addressable but their typed input was invisible.
//
// This pass re-walks the same routers (same-file, reusing parseTRPCRouters /
// parseTRPCProperties / walkTRPCRouter's path logic) to recover, per leaf
// procedure, the raw text of its `.input(...)` argument plus the validator
// library it uses (zod / valibot / yup / superstruct / arktype / a bare typed
// generic). It then stamps `input_schema` (the raw schema expression),
// `input_schema_lib`, and `has_input_schema=true` on the matching
// http_endpoint_definition the tRPC synthesizer just emitted — keyed on the
// shared dotted `path` property, exactly like applyRPCTransportBinding stamps
// `transport`. Procedures with no `.input(...)` are left unstamped (honest:
// "this procedure takes no validated input").
package engine

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// applyTRPCSchemaBinding stamps input-schema properties on the tRPC
// http_endpoint_definition entities emitted into `entities` at index `from` or
// later. Same-file, signal-based, append-property-only — never adds or removes
// entities, so it cannot regress the surrounding pipeline.
func applyTRPCSchemaBinding(content string, entities []types.EntityRecord, from int) {
	if !trpcFileLooksLikeTRPC(content) {
		return
	}
	byPath := trpcInputSchemasByPath(content)
	if len(byPath) == 0 {
		return
	}
	for i := from; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Properties == nil || e.Properties["framework"] != "trpc" {
			continue
		}
		info, ok := byPath[e.Properties["path"]]
		if !ok {
			continue
		}
		e.Properties["has_input_schema"] = "true"
		e.Properties["input_schema"] = info.schema
		if info.lib != "" {
			e.Properties["input_schema_lib"] = info.lib
		}
	}
}

// trpcInputSchema is the recovered input contract for one procedure.
type trpcInputSchema struct {
	schema string // raw text of the .input(...) argument, whitespace-collapsed
	lib    string // validator library: zod | valibot | yup | superstruct | arktype | ""
}

// trpcInputSchemasByPath walks the routers in `content` and returns a map of
// dotted procedure path → recovered input schema. Mirrors synthesizeTRPC's
// router roots / referenced-child logic so the dotted paths match exactly the
// IDs the synthesizer emitted.
func trpcInputSchemasByPath(content string) map[string]trpcInputSchema {
	routers := parseTRPCRouters(content)
	if len(routers) == 0 {
		return nil
	}
	byName := map[string]*trpcRouter{}
	for i := range routers {
		byName[routers[i].name] = &routers[i]
	}
	referenced := map[string]bool{}
	for i := range routers {
		for _, p := range parseTRPCProperties(routers[i]) {
			trimmed := strings.TrimSpace(p.value)
			if trpcIdentRe.MatchString(trimmed) {
				if _, ok := byName[trimmed]; ok {
					referenced[trimmed] = true
				}
			}
		}
	}
	out := map[string]trpcInputSchema{}
	for i := range routers {
		if referenced[routers[i].name] {
			continue
		}
		walkTRPCSchema(&routers[i], "", byName, map[string]bool{}, out)
	}
	return out
}

// walkTRPCSchema mirrors walkTRPCRouter but, instead of emitting endpoints,
// records each leaf procedure's `.input(...)` schema keyed by dotted path.
func walkTRPCSchema(
	r *trpcRouter,
	prefix string,
	byName map[string]*trpcRouter,
	seen map[string]bool,
	out map[string]trpcInputSchema,
) {
	if seen[r.name] {
		return
	}
	seen[r.name] = true
	defer delete(seen, r.name)

	for _, p := range parseTRPCProperties(*r) {
		path := joinTRPCPath(prefix, p.key)
		trimmed := strings.TrimSpace(p.value)
		if trpcIdentRe.MatchString(trimmed) {
			if child, ok := byName[trimmed]; ok {
				walkTRPCSchema(child, path, byName, seen, out)
			}
			continue
		}
		// Only leaf procedures (those carrying a verb call) get a path ID.
		if trpcVerbRe.FindStringSubmatchIndex(p.value) == nil {
			continue
		}
		if schema, ok := extractTRPCInputArg(p.value); ok {
			out[path] = trpcInputSchema{
				schema: schema,
				lib:    trpcSchemaLib(schema),
			}
		}
	}
}

// extractTRPCInputArg returns the raw argument text of the FIRST `.input(...)`
// call inside a procedure value, with interior whitespace collapsed. The
// argument may itself contain balanced parens / braces (e.g.
// `z.object({ id: z.string() })`), so we scan for the matching close paren.
func extractTRPCInputArg(value string) (string, bool) {
	idx := strings.Index(value, ".input(")
	if idx < 0 {
		return "", false
	}
	open := idx + len(".input(") - 1 // position of the '('
	end, ok := matchClosingParen(value, open)
	if !ok {
		return "", false
	}
	arg := strings.TrimSpace(value[open+1 : end])
	if arg == "" {
		return "", false
	}
	return collapseWhitespace(arg), true
}

// matchClosingParen returns the index of the ')' that closes the '(' at
// openAt, tracking nested parens/braces/brackets and skipping string literals.
func matchClosingParen(src string, openAt int) (int, bool) {
	depth := 0
	for i := openAt; i < len(src); i++ {
		switch src[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth == 0 {
				return i, true
			}
		case '"', '\'', '`':
			ni, ok := skipString(src, i)
			if !ok {
				return 0, false
			}
			i = ni - 1
		}
	}
	return 0, false
}

// trpcSchemaLib infers the validator library from the schema expression.
func trpcSchemaLib(schema string) string {
	switch {
	case strings.HasPrefix(schema, "z.") || strings.Contains(schema, "z.object") || strings.Contains(schema, "z.string"):
		return "zod"
	case strings.HasPrefix(schema, "v.") || strings.Contains(schema, "v.object"):
		return "valibot"
	case strings.Contains(schema, "yup."):
		return "yup"
	case strings.Contains(schema, "superstruct") || strings.Contains(schema, "object("):
		return "superstruct"
	case strings.Contains(schema, "arktype") || strings.HasPrefix(schema, "type("):
		return "arktype"
	default:
		return ""
	}
}

// collapseWhitespace replaces runs of whitespace with a single space so the
// stamped schema is a stable single-line property value.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
