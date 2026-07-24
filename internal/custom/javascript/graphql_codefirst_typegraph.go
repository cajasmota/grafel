package javascript

// graphql_codefirst_typegraph.go — code-first GraphQL schema type→type graph
// for the JS/TS code-first servers (epic #3628, child of #3804 / completes the
// SDL pass shipped in #3805).
//
// Background
// ----------
// The SDL extractor (internal/extractors/graphql) models the GraphQL schema's
// entity-relationship graph for *.graphql files: an object-typed field
// (`type User { orders: [Order!]! }`) becomes a GRAPH_RELATES edge between the
// owning type node and the referenced type node, carrying list/nullable
// cardinality (#3805). Code-first servers build the schema from TS code instead
// of an SDL string, so the SDL regexes never fire on them and the #3607-family
// synthesizers only emit the root Query/Mutation/Subscription *operation*
// endpoints — never the data object-type nodes (User, Order) nor the
// relationships between them.
//
// This extractor closes that gap for the three TS code-first builders:
//
//	TypeGraphQL  @ObjectType() class User { @Field(() => [Order]) orders: Order[] }
//	Nexus        objectType({ name:'User', definition(t){ t.list.field('orders',{type:'Order'}) }})
//	Pothos       builder.objectType('User', { fields: t => ({ orders: t.field({ type: ['Order'] }) }) })
//
// For each object type it emits a SCOPE.Schema/subtype="type" node addressed
// with the SAME canonical structural ref the SDL pass uses
// (BuildOperationStructuralRef("graphql", file, TypeName)) so the two passes
// converge on one identity per type, and a GRAPH_RELATES edge per
// object-typed field carrying the identical cardinality property contract:
//
//	{field_name, list, nullable, item_nullable, cardinality:to_one|to_many,
//	 self_ref, graphql_field, framework}
//
// Honest-partial: a field whose referenced type is a built-in scalar, or is not
// a known object type declared *in the same file*, produces NO edge. Cross-file
// targets and unresolved thunks are skipped — never guessed.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extreg.Register("custom_js_graphql_codefirst_typegraph", &graphqlCodeFirstTypeGraphExtractor{})
}

type graphqlCodeFirstTypeGraphExtractor struct{}

func (e *graphqlCodeFirstTypeGraphExtractor) Language() string {
	return "custom_js_graphql_codefirst_typegraph"
}

// gqlcfBuiltinScalars mirrors the SDL pass: these never make a type→type edge.
// "Date"/"DateTime"/"JSON" are common custom scalars in code-first servers and
// are excluded too (they are not object types).
var gqlcfBuiltinScalars = map[string]bool{
	"String": true, "Int": true, "Float": true, "Boolean": true, "ID": true,
	"Date": true, "DateTime": true, "JSON": true, "number": true, "string": true,
	"boolean": true,
}

var (
	// reTGObjectType matches a TypeGraphQL `@ObjectType()` decorator (optionally
	// `@ObjectType("Name")` / `@ObjectType({ ... })`) immediately preceding a
	// class declaration. Group 1 = the class name; an explicit string name
	// argument (group capturing not used — TypeGraphQL's GraphQL type name
	// defaults to the class name, and the relationship graph is keyed on the
	// class identifier which is what other TS data also reference).
	reTGObjectType = regexp.MustCompile(
		`(?m)^[ \t]*@ObjectType\b[^\n]*\)?[ \t]*\r?\n(?:[ \t]*@[A-Za-z][^\n]*\r?\n)*[ \t]*(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_]\w*)`,
	)

	// reTGField matches a TypeGraphQL `@Field(() => Type)` / `@Field(() => [Type])`
	// decorator and the property it annotates. The decorator and the property may
	// share a line (`@Field(() => [Order]) orders: Order[];`) or be split across
	// lines; the optional separator `[ \t\r\n]*` tolerates both. The explicit
	// thunk return type is authoritative (TS arrays erase to `Type[]` which we
	// also parse as a list fallback). Group 1 = thunk type expression (may
	// include []/!?), group 2 = property name, group 3 = the TS declared type
	// (fallback when no thunk).
	reTGField = regexp.MustCompile(
		`@Field\s*\(\s*(?:\(\s*\)\s*=>\s*([^,)\n]+))?[^\n]*?\)[ \t]*(?:\r?\n[ \t]*)?([A-Za-z_]\w*)\s*[!?]?\s*:\s*([^;\n=]+)`,
	)

	// reNexusObjectType matches a Nexus `objectType({ name: 'User', ... })`
	// declaration. Group 1 = the type name string literal.
	reNexusObjectType = regexp.MustCompile(
		`objectType\s*\(\s*\{\s*name\s*:\s*['"]([A-Za-z_]\w*)['"]`,
	)

	// reNexusField matches a Nexus field registration inside a definition block:
	//	t.field('orders', { type: 'Order' })
	//	t.list.field('orders', { type: 'Order' })
	//	t.nonNull.field('owner', { type: 'User' })
	//	t.list.nonNull.field('tags', { type: 'Tag' })
	// Group 1 = the chain between `t.` and `.field` (list/nonNull/null modifiers),
	// group 2 = field name, group 3 = the type name (string-literal form).
	reNexusField = regexp.MustCompile(
		`\bt\.((?:list|nonNull|nullable)(?:\.(?:list|nonNull|nullable))*\.)?field\s*\(\s*['"]([A-Za-z_]\w*)['"]\s*,\s*\{[^}]*?\btype\s*:\s*['"]([A-Za-z_]\w*)['"]`,
	)

	// rePothosObjectType matches a Pothos `builder.objectType('User', { ... })`
	// (or `builder.objectType(UserRef, ...)` — only the string-literal form is
	// resolvable to a name). Group 1 = the type name string literal.
	rePothosObjectType = regexp.MustCompile(
		`\bbuilder\.objectType\s*\(\s*['"]([A-Za-z_]\w*)['"]`,
	)

	// rePothosField matches a Pothos field registration:
	//	orders: t.field({ type: ['Order'] })   → list
	//	owner:  t.field({ type: 'User' })       → singular
	//	tags:   t.field({ type: ['Tag'], nullable: true })
	// Group 1 = field name, group 2 = `[` when list, group 3 = type name.
	rePothosField = regexp.MustCompile(
		`([A-Za-z_]\w*)\s*:\s*t\.field\s*\(\s*\{[^}]*?\btype\s*:\s*(\[)?\s*['"]([A-Za-z_]\w*)['"]`,
	)
)

func (e *graphqlCodeFirstTypeGraphExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.js_graphql_codefirst_typegraph")
	_, span := tracer.Start(ctx, "custom.js_graphql_codefirst_typegraph")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	// Fast-path gate: bail unless one of the three code-first markers is present.
	hasTG := strings.Contains(src, "@ObjectType")
	hasNexus := strings.Contains(src, "objectType(")
	hasPothos := strings.Contains(src, "builder.objectType")
	if !hasTG && !hasNexus && !hasPothos {
		return nil, nil
	}

	// Pass 1: collect all object-type names declared in this file (per framework
	// the names co-resolve so a field can target any sibling type).
	known := map[string]bool{}
	if hasTG {
		for _, m := range reTGObjectType.FindAllStringSubmatch(src, -1) {
			known[m[1]] = true
		}
	}
	if hasNexus {
		for _, m := range reNexusObjectType.FindAllStringSubmatch(src, -1) {
			known[m[1]] = true
		}
	}
	if hasPothos {
		for _, m := range rePothosObjectType.FindAllStringSubmatch(src, -1) {
			known[m[1]] = true
		}
	}
	if len(known) == 0 {
		return nil, nil
	}

	// nodeFor lazily creates (once) the SCOPE.Schema type node for a known
	// object type, addressed with the SDL-canonical structural ref so the SDL
	// and code-first passes converge on a single identity per type.
	nodes := map[string]int{} // type name → index into entities
	var entities []types.EntityRecord
	nodeFor := func(name, framework string, line int) int {
		if idx, ok := nodes[name]; ok {
			return idx
		}
		ent := makeEntity(name, "SCOPE.Schema", "type", file.Path, file.Language, line)
		setProps(&ent,
			"graphql_type", name,
			"framework", framework,
			"code_first", "true",
			"structural_ref", extreg.BuildOperationStructuralRef("graphql", file.Path, name),
			"provenance", "INFERRED_FROM_CODEFIRST_GRAPHQL_OBJECTTYPE",
		)
		entities = append(entities, ent)
		nodes[name] = len(entities) - 1
		return nodes[name]
	}

	addEdge := func(owner, target, framework, fieldName string, tc gqlcfCardinality) {
		if target == owner {
			// self-ref is permitted (mirrors SDL self_ref); still emit.
		}
		ownerRef := extreg.BuildOperationStructuralRef("graphql", file.Path, owner)
		targetRef := extreg.BuildOperationStructuralRef("graphql", file.Path, target)
		props := types.Props{
			{K: "cardinality", V: gqlcfCardLabel(tc)},
			{K: "field_name", V: fieldName},
			{K: "framework", V: framework},
			{K: "graphql_field", V: owner + "." + fieldName},
			{K: "list", V: gqlcfBool(tc.list)},
			{K: "nullable", V: gqlcfBool(tc.nullable)},
			{K: "provenance", V: "INFERRED_FROM_CODEFIRST_GRAPHQL_FIELD"},
			{K: "self_ref", V: gqlcfBool(target == owner)},
		}
		if tc.list {
			props.Set("item_nullable", gqlcfBool(tc.itemNullable))
		}
		idx := nodes[owner]
		entities[idx].Relationships = append(entities[idx].Relationships,
			types.RelationshipRecord{
				FromID:     ownerRef,
				ToID:       targetRef,
				Kind:       string(types.RelationshipKindGraphRelates),
				Properties: props,
			})
	}

	// resolve a captured field target+modifiers into an edge if the base type is
	// a known sibling object type (not a scalar). Dedup per (owner,field,target).
	seen := map[string]bool{}
	emit := func(owner, framework, fieldName, target string, tc gqlcfCardinality) {
		if !known[target] || gqlcfBuiltinScalars[target] {
			return
		}
		key := owner + "|" + fieldName + "|" + target
		if seen[key] {
			return
		}
		seen[key] = true
		addEdge(owner, target, framework, fieldName, tc)
	}

	// Pass 2 (TypeGraphQL): walk each @ObjectType class body and its @Field
	// decorators. We slice the source per class so a field is attributed to the
	// class that owns it.
	if hasTG {
		for _, cls := range tgClassBodies(src) {
			nodeFor(cls.name, "typegraphql", cls.line)
			for _, fm := range reTGField.FindAllStringSubmatch(cls.body, -1) {
				thunk := strings.TrimSpace(fm[1])
				fieldName := fm[2]
				tsType := strings.TrimSpace(fm[3])
				base, tc := tgResolveFieldType(thunk, tsType)
				if base == "" {
					continue
				}
				emit(cls.name, "typegraphql", fieldName, base, tc)
			}
		}
	}

	// Pass 2 (Nexus): each objectType definition block; fields are t.*.field(...)
	// calls. We attribute by the nearest preceding objectType name.
	if hasNexus {
		for _, blk := range nexusObjectBodies(src) {
			nodeFor(blk.name, "nexus", blk.line)
			for _, fm := range reNexusField.FindAllStringSubmatch(blk.body, -1) {
				chain := fm[1]
				fieldName := fm[2]
				target := fm[3]
				tc := nexusCardinality(chain)
				emit(blk.name, "nexus", fieldName, target, tc)
			}
		}
	}

	// Pass 2 (Pothos): each builder.objectType('Name', { ... }) block.
	if hasPothos {
		for _, blk := range pothosObjectBodies(src) {
			nodeFor(blk.name, "pothos", blk.line)
			for _, fm := range rePothosField.FindAllStringSubmatch(blk.body, -1) {
				fieldName := fm[1]
				isList := fm[2] == "["
				target := fm[3]
				tc := gqlcfCardinality{list: isList, nullable: true, itemNullable: true}
				emit(blk.name, "pothos", fieldName, target, tc)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// --- cardinality model (mirrors internal/extractors/graphql/type_graph.go) ---

type gqlcfCardinality struct {
	list         bool
	nullable     bool
	itemNullable bool
}

func gqlcfBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func gqlcfCardLabel(tc gqlcfCardinality) string {
	if tc.list {
		return "to_many"
	}
	return "to_one"
}

// --- TypeGraphQL field-type resolution ---

// tgResolveFieldType resolves a TypeGraphQL field to its base GraphQL type and
// cardinality, preferring the explicit `@Field(() => …)` thunk (authoritative
// for nullability via list markers) and falling back to the TS declared type
// (`Order[]` / `Order`). Returns base="" when no object identifier is found.
func tgResolveFieldType(thunk, tsType string) (string, gqlcfCardinality) {
	// Prefer the thunk return type when present.
	if thunk != "" {
		base, tc, ok := tgParseTypeExpr(thunk)
		if ok {
			return base, tc
		}
	}
	base, tc, ok := tgParseTypeExpr(tsType)
	if !ok {
		return "", gqlcfCardinality{}
	}
	return base, tc
}

var tgIdentRe = regexp.MustCompile(`[A-Za-z_]\w*`)

// tgParseTypeExpr parses a TS/TypeGraphQL type expression into base + list.
//
//	[Order]   / Order[]   → list=true,  base=Order
//	Order               → list=false, base=Order
//	Order | null        → nullable handled by caller's default; base=Order
//
// TS code-first nullability is conventionally driven by `{ nullable: true }`
// option which we do not parse here; we default nullable=true (GraphQL default)
// and itemNullable=true, matching the SDL pass's behaviour for an un-annotated
// `[Order]`. Returns ok=false when no identifier base can be recovered.
func tgParseTypeExpr(expr string) (string, gqlcfCardinality, bool) {
	expr = strings.TrimSpace(expr)
	tc := gqlcfCardinality{nullable: true, itemNullable: true}
	if strings.HasPrefix(expr, "[") || strings.Contains(expr, "[]") {
		tc.list = true
	}
	// strip array/union/null noise and take the first type identifier.
	id := tgIdentRe.FindString(strings.TrimLeft(expr, "[ "))
	if id == "" || id == "null" || id == "undefined" {
		return "", tc, false
	}
	return id, tc, true
}

// nexusCardinality maps a Nexus `t.<chain>.field(...)` modifier chain to a
// cardinality. `list` ⇒ to_many; `nonNull` ⇒ non-nullable; absence ⇒ nullable.
func nexusCardinality(chain string) gqlcfCardinality {
	tc := gqlcfCardinality{nullable: true, itemNullable: true}
	if strings.Contains(chain, "list") {
		tc.list = true
	}
	if strings.Contains(chain, "nonNull") {
		tc.nullable = false
		tc.itemNullable = false
	}
	return tc
}

// --- block slicing helpers ---

type gqlcfBlock struct {
	name string
	line int
	body string
}

// tgClassBodies slices the source into one block per @ObjectType class, the
// block body running from the class declaration to the matching closing brace
// (brace-balanced). Fields are attributed to the class whose body contains them.
func tgClassBodies(src string) []gqlcfBlock {
	var out []gqlcfBlock
	for _, m := range reTGObjectType.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// find the class body's opening brace after the decorator match.
		open := strings.IndexByte(src[m[1]:], '{')
		if open < 0 {
			continue
		}
		start := m[1] + open
		end := gqlcfMatchBrace(src, start)
		if end <= start {
			continue
		}
		out = append(out, gqlcfBlock{name: name, line: lineOf(src, m[0]), body: src[start:end]})
	}
	return out
}

// nexusObjectBodies slices one block per `objectType({ name:'X', ... })`.
func nexusObjectBodies(src string) []gqlcfBlock {
	var out []gqlcfBlock
	for _, m := range reNexusObjectType.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// m[0] starts at the `objectType(` keyword; the arg object's opening
		// brace is the first `{` after it.
		open := strings.IndexByte(src[m[0]:], '{')
		if open < 0 {
			continue
		}
		start := m[0] + open
		end := gqlcfMatchBrace(src, start)
		if end <= start {
			continue
		}
		out = append(out, gqlcfBlock{name: name, line: lineOf(src, m[0]), body: src[start:end]})
	}
	return out
}

// pothosObjectBodies slices one block per `builder.objectType('X', { ... })`.
func pothosObjectBodies(src string) []gqlcfBlock {
	var out []gqlcfBlock
	for _, m := range rePothosObjectType.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		open := strings.IndexByte(src[m[1]:], '{')
		if open < 0 {
			continue
		}
		start := m[1] + open
		end := gqlcfMatchBrace(src, start)
		if end <= start {
			continue
		}
		out = append(out, gqlcfBlock{name: name, line: lineOf(src, m[0]), body: src[start:end]})
	}
	return out
}

// gqlcfMatchBrace returns the index just past the brace matching the one at
// `open` (which must be '{'), or -1 if unbalanced. String/template literals are
// not unescaped — adequate for the structural slicing this extractor needs.
func gqlcfMatchBrace(src string, open int) int {
	if open >= len(src) || src[open] != '{' {
		return -1
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
