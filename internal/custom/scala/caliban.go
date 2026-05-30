package scala

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

// Caliban is a code-first / functional GraphQL server library for Scala
// (package `caliban.*`). A schema is declared by grouping resolver fields into
// `case class` "resolver" types — conventionally Queries / Mutations /
// Subscriptions — and wiring them into a `RootResolver` passed to `graphQL(...)`:
//
//	case class Queries(
//	  user: UserArgs => URIO[Any, User],
//	  users: () => List[User],
//	)
//	case class Mutations(createUser: NewUser => Task[User])
//
//	val api = graphQL(RootResolver(Queries(...), Mutations(...)))
//
// Each field of a resolver case class that is referenced as a root argument of
// `RootResolver(...)` becomes a GraphQL operation field. We synthesise one
// GRAPHQL endpoint per field with verb GRAPHQL and path
// /graphql/<Root>/<field>, mirroring the jsts / rust async-graphql / strawberry
// GraphQL model. The resolver case class supplies the operation root (the
// FIRST RootResolver argument is the Query root, the SECOND the Mutation root,
// the THIRD the Subscription root — Caliban's positional convention), and the
// field's function value is recorded as the handler.
//
// Schema DTO types are the case classes / enums carrying Caliban schema
// annotations (`@GQLDescription`, `@GQLName`, `@GQLDeprecated`, `@GQLInputName`)
// or whose schemas are derived via `Schema.gen` / `deriveSchema`. Each becomes a
// SCOPE.Schema DTO entity.
//
// The HTTP adapter (`http4sAdapter` / `pekkoAdapter` / `akkaHttpAdapter` /
// `playAdapter` / `quickAdapter`) is what serves the interpreter over HTTP. We
// note the adapter on a SCOPE.Service schema-root entity so the schema can be
// associated with the served endpoint, but we do not synthesise the transport
// route itself (the framework's own routing extractor owns that).
//
// HONEST LIMIT: the resolver-root binding is positional and intra-file. A field
// is only emitted as an addressable GraphQL operation when its owning case
// class is named directly inside the `RootResolver(...)` call in the SAME file.
// Cross-file composition (resolver case class defined in another module than
// `RootResolver`), nested object-type field resolvers, and resolvers built via
// `RootResolver.apply` indirection are not chased — those case-class fields are
// still emitted as DTO-shape detail by the type-system extractor, just not as
// top-level GraphQL paths.

func init() {
	extractor.Register("custom_scala_caliban", &calibanExtractor{})
}

type calibanExtractor struct{}

func (e *calibanExtractor) Language() string { return "custom_scala_caliban" }

var (
	// `graphQL(RootResolver(<args>))` — capture the RootResolver argument list
	// (group 1). The arg list is brace/paren-balanced separately; this regex
	// only locates the `RootResolver(` opener.
	reCalibanRootResolver = regexp.MustCompile(`RootResolver\s*\(`)

	// `case class <Name>(` — capture group 1 is the case-class name, used to
	// locate the resolver case class whose fields are GraphQL operations and to
	// enumerate its field declarations.
	reCalibanCaseClass = regexp.MustCompile(`case\s+class\s+([A-Za-z_]\w*)\s*\(`)

	// A field declaration head inside a case-class parameter list:
	//   user: UserArgs => URIO[Any, User]
	//   users: () => List[User]
	//   name: String
	// Capture group 1 is the field name, group 2 is its (trimmed) type text up
	// to the next top-level comma.
	reCalibanField = regexp.MustCompile(`(?m)^\s*([A-Za-z_]\w*)\s*:\s*`)

	// Caliban schema annotations that mark a case class / enum as a GraphQL
	// schema type. Capture group 2 is the annotated type name.
	reCalibanGQLType = regexp.MustCompile(
		`@GQL(?:Description|Name|InputName|Deprecated|Directive)\b[^\n]*\n\s*(?:@[A-Za-z_]\w*\b[^\n]*\n\s*)*(?:final\s+)?(?:sealed\s+)?(?:abstract\s+)?(case\s+class|case\s+object|class|trait|enum|object)\s+([A-Za-z_]\w*)`,
	)

	// `Schema.gen[..., T]` / `deriveSchema[T]` / `Schema.gen[T]` — derived schema
	// for type T. Capture group 1 is the last type argument (the schema'd type).
	reCalibanSchemaGen = regexp.MustCompile(`(?:Schema\.gen|deriveSchema)\s*\[([^\]]+)\]`)

	// HTTP adapter that serves the Caliban interpreter.
	reCalibanAdapter = regexp.MustCompile(`\b((?:http4s|pekko|akkaHttp|play|quick|zioHttp)Adapter)\b`)
)

// calibanArgList returns the byte range [start,end) of the argument list whose
// opening `(` is at openParen (the index of the `(`). Paren-balanced; returns
// (-1,-1) when balance cannot be found.
func calibanArgList(src string, openParen int) (int, int) {
	depth := 0
	for i := openParen; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return openParen + 1, i
			}
		}
	}
	return -1, -1
}

// calibanCaseClassParams returns the byte range [start,end) of the parameter
// list of the case class whose `case class Name(` opener has its `(` at
// openParen. Paren-balanced (handles nested generic-free parens in default
// values). Returns (-1,-1) on imbalance.
func calibanCaseClassParams(src string, openParen int) (int, int) {
	return calibanArgList(src, openParen)
}

// calibanSplitTopLevel splits s on commas that are at bracket/paren depth 0,
// so `a: X => Y, b: (Int, Z) => W` yields two parts, not four.
func calibanSplitTopLevel(s string) []string {
	var parts []string
	depth := 0
	last := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[last:i])
				last = i + 1
			}
		}
	}
	parts = append(parts, s[last:])
	return parts
}

// calibanResolverFields parses the field NAMES declared in a case-class
// parameter list. Each top-level `name: Type` segment contributes its name.
func calibanResolverFields(params string) []string {
	var fields []string
	for _, seg := range calibanSplitTopLevel(params) {
		m := reCalibanField.FindStringSubmatch(seg)
		if m == nil {
			continue
		}
		fields = append(fields, m[1])
	}
	return fields
}

// calibanOperationForIndex maps a RootResolver positional argument index to its
// GraphQL operation kind (0=Query, 1=Mutation, 2=Subscription, Caliban's
// positional convention).
func calibanOperationForIndex(i int) string {
	switch i {
	case 0:
		return "Query"
	case 1:
		return "Mutation"
	case 2:
		return "Subscription"
	default:
		return "Operation"
	}
}

// calibanRootName extracts the resolver type name from a RootResolver argument
// expression, e.g. `Queries(userService.getUser, ...)` -> "Queries",
// `Queries` -> "Queries". Returns "" when no leading type identifier is found.
func calibanRootName(arg string) string {
	arg = strings.TrimSpace(arg)
	m := regexp.MustCompile(`^([A-Za-z_]\w*)`).FindStringSubmatch(arg)
	if m == nil {
		return ""
	}
	return m[1]
}

func (e *calibanExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_caliban.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "caliban"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)

	// File-signal gate: require a Caliban marker so this extractor is a no-op on
	// plain Scala / tapir / http4s files. `graphQL(`, `RootResolver`, a Caliban
	// import, or a Caliban schema annotation are the unambiguous signals.
	if !strings.Contains(src, "RootResolver") &&
		!strings.Contains(src, "caliban.") &&
		!strings.Contains(src, "import caliban") &&
		!strings.Contains(src, "@GQL") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Index every case class by name -> its parameter-list field names, so a
	// RootResolver argument can be resolved to the fields it contributes.
	type ccInfo struct {
		fields []string
		off    int
	}
	caseClasses := make(map[string]ccInfo)
	for _, m := range reCalibanCaseClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// m[1] is the index just past the matched `(`; back up to the `(`.
		openParen := strings.IndexByte(src[m[3]:m[1]], '(')
		if openParen < 0 {
			continue
		}
		openParen += m[3]
		pStart, pEnd := calibanCaseClassParams(src, openParen)
		if pStart < 0 {
			continue
		}
		caseClasses[name] = ccInfo{
			fields: calibanResolverFields(src[pStart:pEnd]),
			off:    m[0],
		}
	}

	// 1. graphQL(RootResolver(<roots>)) -> one GRAPHQL endpoint per field of
	//    each positional root resolver case class.
	for _, rm := range reCalibanRootResolver.FindAllStringIndex(src, -1) {
		openParen := rm[1] - 1 // index of the `(` in `RootResolver(`
		aStart, aEnd := calibanArgList(src, openParen)
		if aStart < 0 {
			continue
		}
		args := calibanSplitTopLevel(src[aStart:aEnd])
		var rootNames []string
		for i, arg := range args {
			root := calibanRootName(arg)
			if root == "" {
				continue
			}
			operation := calibanOperationForIndex(i)
			cc, ok := caseClasses[root]
			if !ok {
				// Root referenced but its case class is not in this file
				// (cross-file composition). Record the root name for the
				// schema entity; fields are not addressable here.
				rootNames = append(rootNames, root)
				continue
			}
			rootNames = append(rootNames, root)
			for _, field := range cc.fields {
				path := "/graphql/" + root + "/" + field
				name := "GRAPHQL " + path
				ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, cc.off))
				setProps(&ent, "framework", "caliban",
					"provenance", "INFERRED_FROM_CALIBAN_RESOLVER",
					"http_method", "GRAPHQL", "verb", "GRAPHQL",
					"route_path", path, "graphql_operation", operation,
					"graphql_root", root, "graphql_field", field,
					"handler_name", root+"."+field)
				add(ent)
			}
		}

		// Schema-root entity capturing the wired resolver roots + adapter.
		schemaEnt := makeEntity("graphql_schema:"+strings.Join(rootNames, ","), "SCOPE.Service", "graphql_schema", file.Path, file.Language, lineOf(src, rm[0]))
		setProps(&schemaEnt, "framework", "caliban",
			"provenance", "INFERRED_FROM_CALIBAN_SCHEMA",
			"schema_roots", strings.Join(rootNames, ","))
		if len(rootNames) >= 1 {
			setProps(&schemaEnt, "query_root", rootNames[0])
		}
		if len(rootNames) >= 2 {
			setProps(&schemaEnt, "mutation_root", rootNames[1])
		}
		if len(rootNames) >= 3 {
			setProps(&schemaEnt, "subscription_root", rootNames[2])
		}
		if am := reCalibanAdapter.FindStringSubmatch(src); am != nil {
			setProps(&schemaEnt, "http_adapter", am[1])
		}
		add(schemaEnt)
	}

	// 2. Caliban schema-annotated types (@GQLDescription / @GQLName / ...) ->
	//    SCOPE.Schema DTO.
	for _, m := range reCalibanGQLType.FindAllStringSubmatchIndex(src, -1) {
		decl := src[m[2]:m[3]]
		typeName := src[m[4]:m[5]]
		ent := makeEntity("graphql_dto:"+typeName, "SCOPE.Schema", "dto", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "caliban",
			"provenance", "INFERRED_FROM_CALIBAN_DTO",
			"dto_name", typeName, "graphql_dto_role", calibanDTORole(decl),
			"declaration", strings.TrimSpace(decl))
		add(ent)
	}

	// 3. Schema.gen[T] / deriveSchema[T] -> SCOPE.Schema DTO for the derived
	//    type. The schema'd type is the LAST type argument.
	for _, m := range reCalibanSchemaGen.FindAllStringSubmatchIndex(src, -1) {
		typeArgs := calibanSplitTopLevel(src[m[2]:m[3]])
		typeName := calibanLeadingType(typeArgs[len(typeArgs)-1])
		if typeName == "" {
			continue
		}
		ent := makeEntity("graphql_dto:"+typeName, "SCOPE.Schema", "dto", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "caliban",
			"provenance", "INFERRED_FROM_CALIBAN_SCHEMA_GEN",
			"dto_name", typeName, "graphql_dto_role", "derived")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// calibanDTORole classifies a schema DTO by its declaration keyword.
func calibanDTORole(decl string) string {
	switch {
	case strings.HasPrefix(decl, "enum"):
		return "enum"
	case strings.HasPrefix(decl, "trait"):
		return "interface"
	case strings.HasPrefix(decl, "case object"), strings.HasPrefix(decl, "object"):
		return "object"
	case strings.HasPrefix(decl, "case class"):
		return "object"
	default:
		return "object"
	}
}

// calibanLeadingType strips a type expression down to its leading type name,
// e.g. `List[User]` -> "List", `User` -> "User", `Any, User` already split.
func calibanLeadingType(t string) string {
	t = strings.TrimSpace(t)
	m := regexp.MustCompile(`^([A-Za-z_]\w*)`).FindStringSubmatch(t)
	if m == nil {
		return ""
	}
	return m[1]
}
