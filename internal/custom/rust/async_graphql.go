package rust

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// async-graphql is a code-first GraphQL server library for Rust. Schemas are
// declared by attaching the `#[Object]` attribute macro to an `impl` block
// whose methods are the GraphQL resolver fields, and by deriving
// `SimpleObject` / `InputObject` on structs that become schema DTO types.
//
//	#[derive(SimpleObject)]
//	struct User { id: ID, name: String }
//
//	struct Query;
//	#[Object]
//	impl Query {
//	    async fn user(&self, ctx: &Context<'_>, id: ID) -> Result<User> { ... }
//	}
//
//	let schema = Schema::build(Query, Mutation, EmptySubscription).finish();
//
// Each resolver method on a Query/Mutation/Subscription root maps to a
// synthetic GraphQL endpoint with verb GRAPHQL and path
// /graphql/<Root>/<field>, mirroring the jsts/strawberry GraphQL model. Each
// SimpleObject/InputObject struct becomes a SCOPE.Schema DTO. Schema::build
// captures the schema root and its three operation-root type arguments.
//
// HONEST LIMIT: when the resolver `impl` block is not one of the three
// recognised roots (Query/Mutation/Subscription) — e.g. a nested
// `#[Object] impl User` field resolver — the operation root is inferred as
// "Object" and the fields are still emitted, but they are not addressable
// from a top-level GraphQL path. Cross-file schema composition (root types
// defined in another module than Schema::build) is not chased.

func init() {
	extractor.Register("custom_rust_async_graphql", &asyncGraphQLExtractor{})
}

type asyncGraphQLExtractor struct{}

func (e *asyncGraphQLExtractor) Language() string { return "custom_rust_async_graphql" }

var (
	// `#[Object]` (optionally with arguments like `#[Object(name = "...")]`)
	// immediately preceding an `impl <Root>` block. Capture group 1 is the
	// implemented type name, which is the GraphQL operation root.
	reAGQLObjectImpl = regexp.MustCompile(
		`#\[Object[^\]]*\]\s*(?:#\[[^\]]*\]\s*)*impl\s+(?:<[^>]*>\s*)?([A-Za-z_]\w*)`,
	)
	// A resolver method inside an `#[Object] impl` block:
	//   async fn user(&self, ...) -> ...
	//   fn name(&self) -> ...
	// Capture group 1 is the field name.
	reAGQLResolverFn = regexp.MustCompile(
		`(?m)^\s*(?:pub\s+)?async\s+fn\s+([A-Za-z_]\w*)\s*\(|^\s*(?:pub\s+)?fn\s+([A-Za-z_]\w*)\s*\(`,
	)
	// `#[derive(... SimpleObject ...)] struct Name` and the InputObject /
	// Enum / Union / Interface / MergedObject variants. Capture group 1 is the
	// derive list, group 2 is the type name.
	reAGQLDeriveType = regexp.MustCompile(
		`#\[derive\s*\(([^)]*\b(?:SimpleObject|InputObject|MergedObject|MergedSubscription)\b[^)]*)\)\]\s*(?:#\[[^\]]*\]\s*)*(?:pub\s+)?struct\s+([A-Za-z_]\w*)`,
	)
	// `#[derive(... Enum ...)] enum Name` — async-graphql GraphQL enum.
	reAGQLDeriveEnum = regexp.MustCompile(
		`#\[derive\s*\(([^)]*\bEnum\b[^)]*)\)\]\s*(?:#\[[^\]]*\]\s*)*(?:pub\s+)?enum\s+([A-Za-z_]\w*)`,
	)
	// `Schema::build(Query, Mutation, Subscription)` — capture the three root
	// type arguments (group 1 = the raw argument list).
	reAGQLSchemaBuild = regexp.MustCompile(
		`Schema::build\s*\(\s*([^)]*)\)`,
	)
)

// agqlBlockBody returns the byte range [start,end) of the `impl` block body
// beginning at the `{` that follows implHeaderEnd. Brace-balanced; returns
// (-1,-1) when no opening brace or balance can be found.
func agqlBlockBody(src string, implHeaderEnd int) (int, int) {
	open := strings.IndexByte(src[implHeaderEnd:], '{')
	if open < 0 {
		return -1, -1
	}
	open += implHeaderEnd
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return open + 1, i
			}
		}
	}
	return -1, -1
}

func (e *asyncGraphQLExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.async_graphql_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "async-graphql"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)

	// File-signal gate: require an async-graphql marker so this extractor is a
	// no-op on plain Rust / diesel / axum files. `#[Object]`, the derive of a
	// GraphQL-only object, or Schema::build are the unambiguous signals.
	if !strings.Contains(src, "#[Object") &&
		!strings.Contains(src, "SimpleObject") &&
		!strings.Contains(src, "InputObject") &&
		!strings.Contains(src, "Schema::build") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. #[Object] impl <Root> { async fn field(...) } -> one GRAPHQL endpoint
	//    per resolver method, path /graphql/<Root>/<field>.
	for _, m := range reAGQLObjectImpl.FindAllStringSubmatchIndex(src, -1) {
		root := src[m[2]:m[3]]
		bodyStart, bodyEnd := agqlBlockBody(src, m[1])
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]

		operation := agqlOperationForRoot(root)

		for _, fm := range reAGQLResolverFn.FindAllStringSubmatchIndex(body, -1) {
			var field string
			if fm[2] >= 0 {
				field = body[fm[2]:fm[3]]
			} else if fm[4] >= 0 {
				field = body[fm[4]:fm[5]]
			}
			if field == "" {
				continue
			}
			fieldOff := bodyStart + fm[0]
			path := rustNormalizePath("/graphql/" + root + "/" + field)
			name := "GRAPHQL " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, fieldOff))
			setProps(&ent, "framework", "async-graphql",
				"provenance", "INFERRED_FROM_ASYNC_GRAPHQL_RESOLVER",
				"http_method", "GRAPHQL", "verb", "GRAPHQL",
				"route_path", path, "graphql_operation", operation,
				"graphql_root", root, "graphql_field", field,
				"handler_name", root+"."+field)
			add(ent)
		}
	}

	// 2. #[derive(SimpleObject/InputObject/...)] struct -> SCOPE.Schema DTO.
	for _, m := range reAGQLDeriveType.FindAllStringSubmatchIndex(src, -1) {
		derives := strings.TrimSpace(src[m[2]:m[3]])
		typeName := src[m[4]:m[5]]
		role := agqlDTORole(derives)
		ent := makeEntity("graphql_dto:"+typeName, "SCOPE.Schema", "dto", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "async-graphql",
			"provenance", "INFERRED_FROM_ASYNC_GRAPHQL_DTO",
			"dto_name", typeName, "graphql_dto_role", role, "derives", derives)
		add(ent)
	}

	// 3. #[derive(Enum)] enum -> SCOPE.Schema DTO (graphql enum).
	for _, m := range reAGQLDeriveEnum.FindAllStringSubmatchIndex(src, -1) {
		derives := strings.TrimSpace(src[m[2]:m[3]])
		typeName := src[m[4]:m[5]]
		ent := makeEntity("graphql_dto:"+typeName, "SCOPE.Schema", "dto", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "async-graphql",
			"provenance", "INFERRED_FROM_ASYNC_GRAPHQL_DTO",
			"dto_name", typeName, "graphql_dto_role", "enum", "derives", derives)
		add(ent)
	}

	// 4. Schema::build(Query, Mutation, Subscription) -> SCOPE.Service schema root.
	for _, m := range reAGQLSchemaBuild.FindAllStringSubmatchIndex(src, -1) {
		rawArgs := src[m[2]:m[3]]
		roots := agqlSplitRoots(rawArgs)
		ent := makeEntity("graphql_schema:"+strings.Join(roots, ","), "SCOPE.Service", "graphql_schema", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "async-graphql",
			"provenance", "INFERRED_FROM_ASYNC_GRAPHQL_SCHEMA",
			"schema_roots", strings.Join(roots, ","))
		if len(roots) >= 1 {
			setProps(&ent, "query_root", roots[0])
		}
		if len(roots) >= 2 {
			setProps(&ent, "mutation_root", roots[1])
		}
		if len(roots) >= 3 {
			setProps(&ent, "subscription_root", roots[2])
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// agqlOperationForRoot maps a root type name to its GraphQL operation kind.
func agqlOperationForRoot(root string) string {
	switch root {
	case "Query", "QueryRoot":
		return "Query"
	case "Mutation", "MutationRoot":
		return "Mutation"
	case "Subscription", "SubscriptionRoot":
		return "Subscription"
	default:
		return "Object"
	}
}

// agqlDTORole classifies a DTO by its async-graphql derive list.
func agqlDTORole(derives string) string {
	switch {
	case strings.Contains(derives, "InputObject"):
		return "input"
	case strings.Contains(derives, "SimpleObject"):
		return "object"
	case strings.Contains(derives, "MergedObject"):
		return "merged_object"
	case strings.Contains(derives, "MergedSubscription"):
		return "merged_subscription"
	default:
		return "object"
	}
}

// agqlSplitRoots splits the Schema::build argument list into the individual
// root-type identifiers, stripping constructor-call suffixes (e.g.
// `EmptyMutation::default()` -> `EmptyMutation`).
func agqlSplitRoots(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Drop any `::...` or `(...)` constructor tail; keep the leading ident.
		if i := strings.IndexAny(p, ":("); i >= 0 {
			p = p[:i]
		}
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
