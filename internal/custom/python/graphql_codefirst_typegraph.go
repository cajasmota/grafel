package python

// graphql_codefirst_typegraph.go — code-first GraphQL schema type→type graph
// for the Python code-first servers (epic #3628, child of #3804 / completes the
// SDL pass shipped in #3805).
//
// The SDL extractor (internal/extractors/graphql) models the object-type→type
// relationship graph for *.graphql files (`type User { orders: [Order!]! }` →
// GRAPH_RELATES User→Order with list/nullable cardinality, #3805). Python
// code-first servers declare the schema in Python classes, so the SDL regexes
// never fire and the #3607-family synthesizers only emit the root
// Query/Mutation/Subscription operation endpoints — never the data object-type
// nodes (User, Order) nor the relationships between them.
//
// This extractor closes that gap for the two Python code-first frameworks:
//
//	Strawberry  @strawberry.type
//	            class User:
//	                orders: list["Order"]
//	                owner: "Account"
//
//	Graphene    class User(graphene.ObjectType):
//	                orders = graphene.List(lambda: Order)   # or List(Order)
//	                owner  = graphene.Field(Account)
//
// For each object type it emits a SCOPE.Schema/subtype="type" node addressed
// with the SAME canonical structural ref the SDL pass uses
// (BuildOperationStructuralRef("graphql", file, TypeName)) so both passes
// converge on one identity per type, plus a GRAPH_RELATES edge per
// object-typed field carrying the identical cardinality property contract:
//
//	{field_name, list, nullable, item_nullable, cardinality:to_one|to_many,
//	 self_ref, graphql_field, framework}
//
// Honest-partial: a field whose referenced type is a scalar, or is not a known
// object type declared in the same file, produces NO edge. The graphene root
// types (Query/Mutation/Subscription) carry resolver fields not data relations
// and are excluded as owners — they are operation roots already modelled by the
// #3620 synthesizer.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("python_graphql_codefirst_typegraph", &GraphQLCodeFirstTypeGraphExtractor{})
}

// GraphQLCodeFirstTypeGraphExtractor builds the object-type→type relationship
// graph for Strawberry and Graphene code-first schemas.
type GraphQLCodeFirstTypeGraphExtractor struct{}

func (e *GraphQLCodeFirstTypeGraphExtractor) Language() string {
	return "python_graphql_codefirst_typegraph"
}

// gqlcfPyBuiltinScalars never make a type→type edge. Includes the GraphQL
// built-ins plus the python primitives and the strawberry/graphene scalar shims
// commonly used as field types.
var gqlcfPyBuiltinScalars = map[string]bool{
	"str": true, "int": true, "float": true, "bool": true, "bytes": true,
	"ID": true, "String": true, "Int": true, "Float": true, "Boolean": true,
	"datetime": true, "date": true, "time": true, "Decimal": true, "UUID": true,
	"JSON": true, "Any": true,
}

var (
	// reStrawType matches a `@strawberry.type` (or @strawberry.input is
	// excluded — inputs are not part of the object relationship graph) decorator
	// preceding a class. Group 1 = class name. Strawberry's @strawberry.federation
	// .type is also matched via the optional `.federation` segment.
	reStrawType = regexp.MustCompile(
		`(?m)^[ \t]*@strawberry(?:\.federation)?\.type\b[^\n]*\r?\n(?:[ \t]*@[^\n]*\r?\n)*[ \t]*class\s+([A-Za-z_]\w*)`,
	)

	// reGrapheneType matches a class whose base list contains ObjectType:
	//	class User(graphene.ObjectType):
	//	class User(ObjectType):
	// Group 1 = class name, group 2 = the base list.
	reGrapheneType = regexp.MustCompile(
		`(?m)^class\s+([A-Za-z_]\w*)\s*\(([^)]*ObjectType[^)]*)\)\s*:`,
	)

	// reStrawField matches a Strawberry annotated field:
	//	orders: list["Order"]
	//	owner: "Account"
	//	tags: List[Tag]
	//	maybe: Optional["Profile"]
	// Group 1 = field name, group 2 = the type annotation expression. Excludes
	// lines that are method defs.
	reStrawField = regexp.MustCompile(
		`(?m)^[ \t]+([A-Za-z_]\w*)\s*:\s*([^\n=]+?)\s*(?:=\s*[^\n]*)?$`,
	)

	// reGrapheneField matches a graphene class attribute bound to a type:
	//	orders = graphene.List(lambda: Order)
	//	orders = List(Order)
	//	owner  = graphene.Field(lambda: Account)
	//	owner  = Field("Account")
	// Group 1 = field name, group 2 = wrapper (List|Field), group 3 = the
	// argument body up to the first comma/paren close.
	reGrapheneField = regexp.MustCompile(
		`(?m)^[ \t]+([A-Za-z_]\w*)\s*=\s*(?:graphene\.)?(List|Field|NonNull)\s*\(\s*([^,)\n]+)`,
	)

	// reGrapheneIdent extracts the referenced type name from a graphene field
	// argument, tolerating `lambda: Order`, `"Order"`, `Order`, `NonNull(Order)`.
	reGrapheneIdent = regexp.MustCompile(`(?:lambda\s*:\s*)?["']?([A-Za-z_]\w*)["']?`)
)

func (e *GraphQLCodeFirstTypeGraphExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_graphql_codefirst_typegraph")
	_, span := tracer.Start(ctx, "custom.python_graphql_codefirst_typegraph")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	hasStraw := strings.Contains(src, "@strawberry") && strings.Contains(src, ".type")
	hasGraphene := strings.Contains(src, "ObjectType")
	if !hasStraw && !hasGraphene {
		return nil, nil
	}

	// Pass 1: collect declared object-type names + their (line, body) blocks.
	type clsBlock struct {
		name      string
		framework string
		line      int
		body      string
		isRoot    bool // graphene Query/Mutation/Subscription — owner-excluded
	}
	var blocks []clsBlock
	known := map[string]bool{}

	if hasStraw {
		for _, m := range reStrawType.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			body := pyClassBody(src, m[3])
			blocks = append(blocks, clsBlock{name: name, framework: "strawberry", line: lineOf(src, m[0]), body: body})
			known[name] = true
		}
	}
	if hasGraphene {
		for _, m := range reGrapheneType.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			body := pyClassBody(src, m[3])
			isRoot := name == "Query" || name == "Mutation" || name == "Subscription"
			blocks = append(blocks, clsBlock{name: name, framework: "graphene", line: lineOf(src, m[0]), body: body, isRoot: isRoot})
			if !isRoot {
				known[name] = true
			}
		}
	}
	if len(known) == 0 {
		return nil, nil
	}

	nodes := map[string]int{}
	var out []types.EntityRecord
	nodeFor := func(name, framework string, line int) int {
		if idx, ok := nodes[name]; ok {
			return idx
		}
		props := types.Props{
			{K: "code_first", V: "true"},
			{K: "framework", V: framework},
			{K: "graphql_type", V: name},
			{K: "provenance", V: "INFERRED_FROM_CODEFIRST_GRAPHQL_OBJECTTYPE"},
			{K: "structural_ref", V: extractor.BuildOperationStructuralRef("graphql", file.Path, name)},
		}
		out = append(out, entity(name, "SCOPE.Schema", "type", file.Path, line, props.Snapshot()))
		nodes[name] = len(out) - 1
		return nodes[name]
	}

	seen := map[string]bool{}
	emit := func(owner, framework, fieldName, target string, tc gqlcfPyCard) {
		if !known[target] || gqlcfPyBuiltinScalars[target] {
			return
		}
		key := owner + "|" + fieldName + "|" + target
		if seen[key] {
			return
		}
		seen[key] = true
		ownerRef := extractor.BuildOperationStructuralRef("graphql", file.Path, owner)
		targetRef := extractor.BuildOperationStructuralRef("graphql", file.Path, target)
		props := types.Props{
			{K: "cardinality", V: gqlcfPyCardLabel(tc)},
			{K: "field_name", V: fieldName},
			{K: "framework", V: framework},
			{K: "graphql_field", V: owner + "." + fieldName},
			{K: "list", V: gqlcfPyBool(tc.list)},
			{K: "nullable", V: gqlcfPyBool(tc.nullable)},
			{K: "provenance", V: "INFERRED_FROM_CODEFIRST_GRAPHQL_FIELD"},
			{K: "self_ref", V: gqlcfPyBool(target == owner)},
		}
		if tc.list {
			props.Set("item_nullable", gqlcfPyBool(tc.itemNullable))
		}
		idx := nodes[owner]
		out[idx].Relationships = append(out[idx].Relationships,
			types.RelationshipRecord{
				FromID:     ownerRef,
				ToID:       targetRef,
				Kind:       string(types.RelationshipKindGraphRelates),
				Properties: props,
			})
	}

	for _, blk := range blocks {
		if blk.isRoot {
			continue // graphene operation root — not a data owner.
		}
		nodeFor(blk.name, blk.framework, blk.line)
		switch blk.framework {
		case "strawberry":
			for _, fm := range reStrawField.FindAllStringSubmatch(blk.body, -1) {
				fieldName := fm[1]
				ann := strings.TrimSpace(fm[2])
				if strings.HasPrefix(ann, "Callable") || ann == "" {
					continue
				}
				base, tc := strawParseAnnotation(ann)
				if base == "" {
					continue
				}
				emit(blk.name, "strawberry", fieldName, base, tc)
			}
		case "graphene":
			for _, fm := range reGrapheneField.FindAllStringSubmatch(blk.body, -1) {
				fieldName := fm[1]
				wrapper := fm[2]
				arg := fm[3]
				base := grapheneTypeArg(arg)
				if base == "" {
					continue
				}
				tc := gqlcfPyCard{nullable: true, itemNullable: true}
				if wrapper == "List" {
					tc.list = true
				}
				if wrapper == "NonNull" {
					tc.nullable = false
				}
				emit(blk.name, "graphene", fieldName, base, tc)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// --- cardinality model (mirrors internal/extractors/graphql/type_graph.go) ---

type gqlcfPyCard struct {
	list         bool
	nullable     bool
	itemNullable bool
}

func gqlcfPyBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func gqlcfPyCardLabel(tc gqlcfPyCard) string {
	if tc.list {
		return "to_many"
	}
	return "to_one"
}

var gqlcfPyIdentRe = regexp.MustCompile(`[A-Za-z_]\w*`)

// strawParseAnnotation resolves a Strawberry field annotation into its base
// object type and cardinality.
//
//	list["Order"] / List[Order] / Sequence[Order] → list=true,  base=Order
//	Optional["Profile"]                            → list=false, nullable=true
//	"Account"                                      → list=false
//
// Returns base="" when no identifier base can be recovered. Forward-ref string
// quotes are stripped. `Optional[...]` flags nullable; a bare annotation is
// non-null in Strawberry (we record nullable=false to match its semantics).
func strawParseAnnotation(ann string) (string, gqlcfPyCard) {
	tc := gqlcfPyCard{itemNullable: false}
	lower := ann
	isList := strings.Contains(lower, "list[") || strings.Contains(lower, "List[") ||
		strings.Contains(lower, "Sequence[") || strings.Contains(lower, "tuple[")
	tc.list = isList
	if strings.Contains(ann, "Optional[") || strings.HasSuffix(strings.TrimSpace(ann), "| None") ||
		strings.Contains(ann, "Optional ") {
		tc.nullable = true
		tc.itemNullable = isList
	}
	// Strip wrappers and quotes, take the LAST identifier inside the innermost
	// brackets (the actual element type), falling back to the first ident.
	inner := ann
	if i := strings.LastIndex(inner, "["); i >= 0 {
		if j := strings.Index(inner[i:], "]"); j >= 0 {
			inner = inner[i+1 : i+j]
		} else {
			inner = inner[i+1:]
		}
	}
	inner = strings.Trim(strings.TrimSpace(inner), `"'`)
	id := gqlcfPyIdentRe.FindString(inner)
	if id == "" || id == "None" {
		// fall back to the whole annotation's first identifier
		id = gqlcfPyIdentRe.FindString(strings.Trim(ann, `"'`))
	}
	if id == "" || id == "None" || id == "Optional" || id == "list" || id == "List" {
		return "", tc
	}
	return id, tc
}

// grapheneTypeArg extracts the referenced type name from a graphene field
// argument expression (`lambda: Order`, `"Order"`, `Order`, `NonNull(Account)`).
func grapheneTypeArg(arg string) string {
	arg = strings.TrimSpace(arg)
	// Unwrap a NonNull(...) inner.
	if strings.HasPrefix(arg, "NonNull(") {
		arg = strings.TrimPrefix(arg, "NonNull(")
	}
	m := reGrapheneIdent.FindStringSubmatch(arg)
	if m == nil {
		return ""
	}
	id := m[1]
	if id == "lambda" || id == "NonNull" || id == "List" || id == "Field" {
		return ""
	}
	return id
}

// pyClassBody returns the indented body of a class whose header ends at byte
// offset `headerEnd` (the offset just past the class name). It collects the
// contiguous run of lines more-indented than the `class` keyword line, which is
// the conventional Python class body. Adequate for attribute-level field
// scanning without a full parser.
func pyClassBody(src string, headerEnd int) string {
	// Find the `:` ending the class header, then start at the next line.
	nl := strings.IndexByte(src[headerEnd:], '\n')
	if nl < 0 {
		return ""
	}
	start := headerEnd + nl + 1
	lines := strings.Split(src[start:], "\n")
	var body []string
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			body = append(body, ln)
			continue
		}
		// stop at the first non-indented line (dedent to module/class level).
		if ln[0] != ' ' && ln[0] != '\t' {
			break
		}
		body = append(body, ln)
	}
	return strings.Join(body, "\n")
}
