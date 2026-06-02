package rust

// graphql_codefirst_typegraph.go — code-first GraphQL schema type→type graph
// for async-graphql (epic #3872, Rust parity audit #3884, completes #3804 for
// Rust). Mirrors internal/custom/{python,javascript}/graphql_codefirst_typegraph.go.
//
// The flat DTO catalog (internal/custom/rust/async_graphql.go) already emits a
// SCOPE.Schema/dto node per #[derive(SimpleObject/InputObject/Enum/...)] type
// and a GRAPHQL endpoint per #[Object] resolver method. What it does NOT emit is
// the typed field→type relationship graph (the #3804 lane): which object type
// references which other object type, and with what cardinality. Python and
// JS/TS each ship a dedicated graphql_codefirst_typegraph extractor that closes
// this for their code-first frameworks; Rust had no equivalent, so
// async-graphql's Schema.type_graph_extraction was HONESTLY missing.
//
// This extractor closes that gap for async-graphql:
//
//	#[derive(SimpleObject)]
//	struct User {
//	    id: ID,                 // scalar -> no edge
//	    name: String,           // scalar -> no edge
//	    orders: Vec<Order>,     // GRAPH_RELATES User->Order  cardinality=to_many
//	    account: Option<Account>, // GRAPH_RELATES User->Account nullable to_one
//	}
//
//	struct Query;
//	#[Object]
//	impl Query {
//	    // resolver return type -> GRAPH_RELATES Query->User
//	    async fn user(&self, id: ID) -> Result<User> { ... }
//	    async fn orders(&self) -> Vec<Order> { ... }
//	}
//
// Each object type (SimpleObject / MergedObject struct, the resolver root of an
// #[Object] impl, and #[derive(Interface)]) becomes a SCOPE.Schema/type node
// addressed with the SAME canonical structural ref the SDL pass and the py/jsts
// code-first passes use (BuildOperationStructuralRef("graphql", file, TypeName)),
// so identities converge on one node per type. Each object-typed struct field
// and each resolver return type becomes a GRAPH_RELATES edge carrying the
// identical cardinality property contract as the SDL / py / jsts emitters:
//
//	{field_name, list, nullable, item_nullable, cardinality:to_one|to_many,
//	 self_ref, graphql_field, framework}
//
// HONEST LIMITS:
//   - Field-type resolution is heuristic and same-file: a field whose innermost
//     type identifier is a scalar (ID/String/i32/...) or is not an object type
//     declared in the SAME file produces NO edge. Cross-module references and
//     custom scalars are not chased.
//   - InputObject and Enum types are intentionally NOT owners — inputs/enums are
//     the flat DTO catalog (already credited) and carry no object→object data
//     relations in the output type graph. (An InputObject field that references
//     another InputObject is left to the DTO catalog.)
//   - A plain `struct` with no async-graphql derive emits no type node and no
//     edge; a non-GraphQL `impl` (no #[Object]) emits no resolver edge.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("rust_graphql_codefirst_typegraph", &rustGraphQLTypeGraphExtractor{})
}

// rustGraphQLTypeGraphExtractor builds the object-type→type relationship graph
// for async-graphql code-first schemas.
type rustGraphQLTypeGraphExtractor struct{}

func (e *rustGraphQLTypeGraphExtractor) Language() string {
	return "rust_graphql_codefirst_typegraph"
}

// rustGqlScalars never make a type→type edge: the GraphQL built-ins plus the
// common Rust primitive / std types used as async-graphql scalar fields.
var rustGqlScalars = map[string]bool{
	"ID": true, "String": true, "Int": true, "Float": true, "Boolean": true,
	"str": true, "bool": true, "char": true,
	"i8": true, "i16": true, "i32": true, "i64": true, "i128": true, "isize": true,
	"u8": true, "u16": true, "u32": true, "u64": true, "u128": true, "usize": true,
	"f32": true, "f64": true,
	"Uuid": true, "DateTime": true, "NaiveDateTime": true, "NaiveDate": true,
	"Decimal": true, "Json": true, "Value": true, "Bytes": true,
}

var (
	// #[derive(... SimpleObject | MergedObject ...)] struct Name { ... }
	// Output object types — these are the owners of the field graph.
	reGqlTGObjectStruct = regexp.MustCompile(
		`#\[derive\s*\(([^)]*\b(?:SimpleObject|MergedObject)\b[^)]*)\)\]\s*(?:#\[[^\]]*\]\s*)*(?:pub\s+)?struct\s+([A-Za-z_]\w*)`,
	)
	// #[derive(... Interface ...)] enum Name — async-graphql interfaces are
	// modelled as enums; they are object-graph nodes (a possible field target).
	reGqlTGInterface = regexp.MustCompile(
		`#\[derive\s*\(([^)]*\bInterface\b[^)]*)\)\]\s*(?:#\[[^\]]*\]\s*)*(?:pub\s+)?enum\s+([A-Za-z_]\w*)`,
	)
	// #[Object ...] (resolver impl) immediately preceding `impl <Root>`.
	// Group 1 = the resolver root type name (Query/Mutation/User/...).
	reGqlTGObjectImpl = regexp.MustCompile(
		`#\[Object[^\]]*\]\s*(?:#\[[^\]]*\]\s*)*impl\s+(?:<[^>]*>\s*)?([A-Za-z_]\w*)`,
	)
	// A struct field line: `pub orders: Vec<Order>,` — group 1 = field name,
	// group 2 = the type expression up to the trailing comma / line end. Skips
	// attribute lines (`#[graphql(...)]`) which do not match (no `:` field form).
	reGqlTGStructField = regexp.MustCompile(
		`(?m)^[ \t]*(?:pub\s+(?:\([^)]*\)\s*)?)?([A-Za-z_]\w*)\s*:\s*([^,\n{}]+?)\s*,?\s*$`,
	)
	// A resolver method header: `async fn user(&self, ...) -> Result<User> {`
	// or `fn name(&self) -> String {`. Group 1 = field name. The return type is
	// recovered separately from the `->` arrow to the opening `{`.
	reGqlTGResolverFn = regexp.MustCompile(
		`(?m)^[ \t]*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)\s*\(`,
	)
	// Innermost-or-last identifier extractor for a Rust type expression.
	rustTGIdentRe = regexp.MustCompile(`[A-Za-z_]\w*`)
)

func (e *rustGraphQLTypeGraphExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.rust_graphql_codefirst_typegraph")
	_, span := tracer.Start(ctx, "custom.rust_graphql_codefirst_typegraph")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}
	src := string(file.Content)

	// File-signal gate: require an async-graphql marker (mirrors async_graphql.go).
	if !strings.Contains(src, "#[Object") &&
		!strings.Contains(src, "SimpleObject") &&
		!strings.Contains(src, "MergedObject") &&
		!strings.Contains(src, "Interface") {
		return nil, nil
	}

	// Pass 1: collect the set of declared object-type names (edge targets) and
	// the (owner, body) blocks whose fields/resolvers we will walk.
	type ownerBlock struct {
		name string
		kind string // "struct" | "resolver"
		line int
		body string
	}
	var owners []ownerBlock
	known := map[string]bool{}

	// SimpleObject / MergedObject structs are both owners and edge targets.
	for _, m := range reGqlTGObjectStruct.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[4]:m[5]]
		bodyStart, bodyEnd := agqlBlockBody(src, m[5])
		body := ""
		if bodyStart >= 0 {
			body = src[bodyStart:bodyEnd]
		}
		owners = append(owners, ownerBlock{name: name, kind: "struct", line: lineOf(src, m[0]), body: body})
		known[name] = true
	}
	// Interface enums are edge targets (and nodes), not field owners here.
	for _, m := range reGqlTGInterface.FindAllStringSubmatchIndex(src, -1) {
		known[src[m[4]:m[5]]] = true
	}
	// #[Object] impl <Root> resolver blocks are owners; their return types are
	// edge targets. The root is NOT itself a known field-target type unless it is
	// also a SimpleObject (rare) — we still emit its node so its resolver edges
	// have a source node.
	for _, m := range reGqlTGObjectImpl.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		bodyStart, bodyEnd := agqlBlockBody(src, m[1])
		body := ""
		if bodyStart >= 0 {
			body = src[bodyStart:bodyEnd]
		}
		owners = append(owners, ownerBlock{name: name, kind: "resolver", line: lineOf(src, m[0]), body: body})
	}

	if len(owners) == 0 {
		return nil, nil
	}

	nodes := map[string]int{}
	var out []types.EntityRecord
	nodeFor := func(name string, line int) {
		if _, ok := nodes[name]; ok {
			return
		}
		ent := makeEntity(name, "SCOPE.Schema", "type", file.Path, file.Language, line)
		setProps(&ent,
			"graphql_type", name,
			"framework", "async-graphql",
			"code_first", "true",
			"structural_ref", extractor.BuildOperationStructuralRef("graphql", file.Path, name),
			"provenance", "INFERRED_FROM_CODEFIRST_GRAPHQL_OBJECTTYPE",
		)
		out = append(out, ent)
		nodes[name] = len(out) - 1
	}

	seen := map[string]bool{}
	emit := func(owner, fieldName, target string, tc rustGqlCard) {
		if !known[target] || rustGqlScalars[target] {
			return
		}
		key := owner + "|" + fieldName + "|" + target
		if seen[key] {
			return
		}
		seen[key] = true
		ownerRef := extractor.BuildOperationStructuralRef("graphql", file.Path, owner)
		targetRef := extractor.BuildOperationStructuralRef("graphql", file.Path, target)
		props := map[string]string{
			"field_name":    fieldName,
			"list":          rustGqlBool(tc.list),
			"nullable":      rustGqlBool(tc.nullable),
			"cardinality":   rustGqlCardLabel(tc),
			"self_ref":      rustGqlBool(target == owner),
			"graphql_field": owner + "." + fieldName,
			"framework":     "async-graphql",
			"provenance":    "INFERRED_FROM_CODEFIRST_GRAPHQL_FIELD",
		}
		if tc.list {
			props["item_nullable"] = rustGqlBool(tc.itemNullable)
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

	for _, ow := range owners {
		nodeFor(ow.name, ow.line)
		switch ow.kind {
		case "struct":
			for _, fm := range reGqlTGStructField.FindAllStringSubmatch(ow.body, -1) {
				fieldName := fm[1]
				typeExpr := strings.TrimSpace(fm[2])
				base, tc := rustParseTypeExpr(typeExpr)
				if base == "" {
					continue
				}
				emit(ow.name, fieldName, base, tc)
			}
		case "resolver":
			for _, fm := range reGqlTGResolverFn.FindAllStringSubmatchIndex(ow.body, -1) {
				fieldName := ow.body[fm[2]:fm[3]]
				ret := rustResolverReturnType(ow.body, fm[0])
				if ret == "" {
					continue
				}
				base, tc := rustParseTypeExpr(ret)
				if base == "" {
					continue
				}
				emit(ow.name, fieldName, base, tc)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// --- cardinality model (mirrors internal/extractors/graphql/type_graph.go) ---

type rustGqlCard struct {
	list         bool
	nullable     bool
	itemNullable bool
}

func rustGqlBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func rustGqlCardLabel(tc rustGqlCard) string {
	if tc.list {
		return "to_many"
	}
	return "to_one"
}

// rustParseTypeExpr resolves a Rust type expression into its base object type
// name and GraphQL cardinality.
//
//	Vec<Order>          -> base=Order  list=true  (non-null list, non-null item)
//	Option<Account>     -> base=Account nullable=true
//	Option<Vec<Tag>>    -> base=Tag    list=true  nullable=true  item non-null
//	Vec<Option<Tag>>    -> base=Tag    list=true  item_nullable=true
//	Order               -> base=Order  (non-null to_one)
//	&User / Arc<User>   -> base=User   (reference/smart-pointer wrappers unwrapped)
//
// async-graphql semantics: a bare type is non-null; Option<T> is the nullable
// marker; Vec<T>/[T]/slices are lists. Returns base="" when no object identifier
// can be recovered (e.g. a bare scalar, a tuple, or an unparseable expression).
func rustParseTypeExpr(expr string) (string, rustGqlCard) {
	tc := rustGqlCard{}
	e := strings.TrimSpace(expr)
	// Strip leading reference / lifetime / mut markers.
	e = strings.TrimPrefix(e, "&")
	e = strings.TrimSpace(e)
	for strings.HasPrefix(e, "'") {
		// drop a lifetime token like `'a `
		if sp := strings.IndexByte(e, ' '); sp >= 0 {
			e = strings.TrimSpace(e[sp+1:])
		} else {
			break
		}
	}
	e = strings.TrimPrefix(e, "mut ")
	e = strings.TrimSpace(e)

	// Unwrap transparent smart-pointer / container wrappers that do not change
	// the GraphQL cardinality: Arc<T>, Box<T>, Rc<T>, Result<T>, FieldResult<T>.
	transparent := map[string]bool{
		"Arc": true, "Box": true, "Rc": true, "Result": true,
		"FieldResult": true, "Ref": true, "RwLock": true, "Mutex": true,
	}
	for {
		head, inner, ok := rustSplitGeneric(e)
		if !ok || !transparent[head] {
			break
		}
		// Result<T, E> -> keep only the first generic arg.
		if c := rustTopComma(inner); c >= 0 {
			inner = inner[:c]
		}
		e = strings.TrimSpace(inner)
	}

	// Now classify list / option wrappers, recursing inward.
	for {
		head, inner, ok := rustSplitGeneric(e)
		if !ok {
			break
		}
		switch head {
		case "Option":
			tc.nullable = true
			if tc.list {
				// Option inside a list element -> item nullable.
				tc.itemNullable = true
			}
			e = strings.TrimSpace(inner)
		case "Vec", "VecDeque", "HashSet", "BTreeSet", "Slice":
			tc.list = true
			e = strings.TrimSpace(inner)
		case "Arc", "Box", "Rc", "Result", "FieldResult", "Ref", "RwLock", "Mutex":
			if c := rustTopComma(inner); c >= 0 {
				inner = inner[:c]
			}
			e = strings.TrimSpace(inner)
		default:
			// Unknown generic (e.g. a custom wrapper) — take the wrapper head as
			// the base type candidate and stop.
			e = head
			goto done
		}
	}
done:
	// Slice syntax `[Tag]` -> list of Tag.
	if strings.HasPrefix(e, "[") {
		tc.list = true
		e = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(e, "["), "]"))
		// `[Tag; N]` -> drop the length.
		if sc := strings.IndexByte(e, ';'); sc >= 0 {
			e = strings.TrimSpace(e[:sc])
		}
	}
	id := rustTGIdentRe.FindString(e)
	if id == "" {
		return "", tc
	}
	return id, tc
}

// rustSplitGeneric splits `Head<Inner>` into ("Head", "Inner", true). Returns
// ok=false when the expression is not a single generic application. The match is
// brace-balanced on `<`/`>` so nested generics are kept intact in Inner.
func rustSplitGeneric(e string) (string, string, bool) {
	lt := strings.IndexByte(e, '<')
	if lt <= 0 || !strings.HasSuffix(e, ">") {
		return "", "", false
	}
	head := strings.TrimSpace(e[:lt])
	if rustTGIdentRe.FindString(head) != head {
		return "", "", false
	}
	inner := e[lt+1 : len(e)-1]
	return head, inner, true
}

// rustTopComma returns the index of the first top-level comma in a generic
// argument list (depth-0 w.r.t. nested `<>`), or -1 when there is none.
func rustTopComma(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// rustResolverReturnType returns the return-type expression of a resolver
// method whose header starts at byte offset `fnStart` within body. It scans for
// the `->` arrow before the opening `{` of the method body and returns the text
// between them, trimmed. Returns "" for a unit-return resolver (no `->`).
func rustResolverReturnType(body string, fnStart int) string {
	// Find the matching `)` of the parameter list to skip commas/arrows inside.
	open := strings.IndexByte(body[fnStart:], '(')
	if open < 0 {
		return ""
	}
	open += fnStart
	depth := 0
	i := open
	for ; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				goto afterParams
			}
		}
	}
	return ""
afterParams:
	rest := body[i+1:]
	brace := strings.IndexByte(rest, '{')
	semi := strings.IndexByte(rest, ';')
	end := brace
	if semi >= 0 && (end < 0 || semi < end) {
		end = semi
	}
	if end < 0 {
		end = len(rest)
	}
	sig := rest[:end]
	arrow := strings.Index(sig, "->")
	if arrow < 0 {
		return ""
	}
	ret := strings.TrimSpace(sig[arrow+2:])
	// Drop a `where ...` clause tail if present.
	if w := strings.Index(ret, " where "); w >= 0 {
		ret = strings.TrimSpace(ret[:w])
	}
	return ret
}
