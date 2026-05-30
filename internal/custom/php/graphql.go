// Package php — regex/bracket-based webonyx/graphql-php extractor.
//
// webonyx/graphql-php is the de-facto code-first GraphQL server library for
// PHP. A schema is built from PHP `ObjectType` instances whose `fields` array
// declares the GraphQL fields of that type; each field's `resolve` closure or
// callable is the field resolver (the route-handler analog):
//
//	$queryType = new ObjectType([
//	    'name' => 'Query',
//	    'fields' => [
//	        'user' => [
//	            'type' => Type::nonNull($userType),
//	            'args' => ['id' => Type::nonNull(Type::id())],
//	            'resolve' => fn ($root, $args, $ctx) => $repo->find($args['id']),
//	        ],
//	        'users' => [
//	            'type' => Type::listOf($userType),
//	            'resolve' => function ($root, $args) { return $repo->all(); },
//	        ],
//	    ],
//	]);
//
//	$schema = new Schema([
//	    'query'    => $queryType,
//	    'mutation' => $mutationType,
//	]);
//
// Mapping (mirrors rust async-graphql / kotlin graphql-kotlin / jsts
// strawberry):
//
//   - Each field of an ObjectType named Query/Mutation/Subscription becomes a
//     synthetic GRAPHQL endpoint with verb GRAPHQL and path
//     /graphql/<TypeName>/<field>. The field's resolve closure/callable is the
//     handler (handler_name = <TypeName>.<field>).
//   - `new Schema([...])` is the schema root (SCOPE.Service), capturing its
//     query / mutation / subscription operation-root variables.
//   - A field's `'type'` reference (Type::nonNull($userType), Type::listOf(...),
//     or a bare custom `$userType`) is recorded as a DTO/type ref on the field
//     entity; ObjectType instances assigned to a variable (`$userType = new
//     ObjectType([... 'name' => 'User' ...])`) become SCOPE.Schema DTOs.
//
// HONEST LIMIT: this is file-local and structural. Schema-first / SDL
// (Lighthouse, `.graphql` schema files) is NOT handled here and is a separate
// follow-up. Field `type` refs that resolve to a type defined in another file
// are recorded by name only, not linked. ObjectTypes whose `name` is computed
// at runtime, or fields built via a `function () { return [...]; }` thunk
// rather than an inline array literal, are not chased.
//
// Registration key: "custom_php_graphql_php"
// Issue #3544 (epic #3505) — PHP webonyx/graphql-php coverage.
package php

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

func init() {
	extractor.Register("custom_php_graphql_php", &graphqlPHPExtractor{})
}

type graphqlPHPExtractor struct{}

func (e *graphqlPHPExtractor) Language() string { return "custom_php_graphql_php" }

var (
	// `new ObjectType(` — start of a webonyx object-type definition. The byte
	// offset of the `(` lets the caller balance the argument array.
	reGQLPObjectType = regexp.MustCompile(`new\s+ObjectType\s*\(`)
	// `new Schema(` — start of a webonyx schema construction.
	reGQLPSchema = regexp.MustCompile(`new\s+Schema\s*\(`)
	// `$var = new ObjectType(` — captures the PHP variable an ObjectType is
	// assigned to (group 1), used to associate a DTO type name with its var.
	reGQLPVarAssign = regexp.MustCompile(`\$([A-Za-z_]\w*)\s*=\s*new\s+ObjectType\s*\(`)
	// `'name' => 'Foo'` / "name" => "Foo" — the GraphQL type name inside an
	// ObjectType config array. Group 1 is the type name.
	reGQLPName = regexp.MustCompile(`['"]name['"]\s*=>\s*['"]([A-Za-z_]\w*)['"]`)
	// `'fields' => [` / "fields" => array( — start of the fields map. The byte
	// offset of the opening bracket lets the caller balance the fields block.
	reGQLPFields = regexp.MustCompile(`['"]fields['"]\s*=>\s*(\[|array\s*\()`)
	// A top-level field key inside a fields map: `'user' =>` / "users" =>.
	// Matched on the fields-block source with depth tracking by the caller so
	// only depth-0 keys (the field names) are taken.
	reGQLPFieldKey = regexp.MustCompile(`(?m)['"]([A-Za-z_]\w*)['"]\s*=>`)
	// `'resolve' =>` — marks a field config that carries an explicit resolver.
	reGQLPResolve = regexp.MustCompile(`['"]resolve['"]\s*=>`)
	// `'type' => <expr>` — captures the raw type expression for a field up to
	// the next comma at the field's own depth (caller bounds it).
	reGQLPType = regexp.MustCompile(`['"]type['"]\s*=>\s*`)
	// A schema operation root: `'query' => $queryType`. Group 1 is the
	// operation (query/mutation/subscription), group 2 the root variable.
	reGQLPSchemaRoot = regexp.MustCompile(`['"](query|mutation|subscription)['"]\s*=>\s*\$([A-Za-z_]\w*)`)
)

// gqlpBalanced returns the byte range [open+1, close) of the bracket group
// whose opening bracket is the first `[` or `(` at or after `from` in src,
// balancing both `[]` and `()` so `array( ... )` and `[ ... ]` both work.
// Returns (-1,-1) when no opening bracket or no balanced close is found.
// Brackets inside single/double-quoted PHP strings are ignored.
func gqlpBalanced(src string, from int) (int, int) {
	open := -1
	for i := from; i < len(src); i++ {
		if src[i] == '[' || src[i] == '(' {
			open = i
			break
		}
	}
	if open == -1 {
		return -1, -1
	}
	depth := 0
	inStr := byte(0)
	for i := open; i < len(src); i++ {
		c := src[i]
		if inStr != 0 {
			if c == '\\' {
				i++ // skip escaped char
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
		case '[', '(':
			depth++
		case ']', ')':
			depth--
			if depth == 0 {
				return open + 1, i
			}
		}
	}
	return -1, -1
}

// gqlpTopLevelFieldKeys returns the field names declared at depth 0 of a
// fields-map body, in source order, paired with the byte offset of each key.
// Depth is tracked over `[]`/`()` (and PHP strings ignored) so only the
// outermost keys — the field names — are returned, not keys nested inside a
// field's own config array (type/args/resolve/...).
func gqlpTopLevelFieldKeys(body string) []struct {
	name   string
	offset int
} {
	var out []struct {
		name   string
		offset int
	}
	depth := 0
	inStr := byte(0)
	// Precompute key matches so we only have to check depth at their offsets.
	keys := reGQLPFieldKey.FindAllStringSubmatchIndex(body, -1)
	ki := 0
	for i := 0; i < len(body); i++ {
		// Emit any key whose match starts exactly here and sits at depth 0.
		for ki < len(keys) && keys[ki][0] == i {
			if depth == 0 && inStr == 0 {
				out = append(out, struct {
					name   string
					offset int
				}{body[keys[ki][2]:keys[ki][3]], keys[ki][0]})
			}
			ki++
		}
		c := body[i]
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
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		}
	}
	return out
}

// gqlpFieldConfigEnd returns the byte offset (relative to body) at which the
// field whose value begins at valStart ends — i.e. the comma at the field's
// own depth, or len(body). valStart should point just after the `=>` of the
// field key. This bounds a single field's config so type/resolve lookups stay
// within that field.
func gqlpFieldConfigEnd(body string, valStart int) int {
	depth := 0
	inStr := byte(0)
	for i := valStart; i < len(body); i++ {
		c := body[i]
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
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
	}
	return len(body)
}

// gqlpTypeRef extracts a human-readable type reference from a field config
// slice (the body between the field key and its terminating comma). It returns
// the inner type name of a Type::nonNull($x) / Type::listOf($x) wrapper, the
// bare custom-type variable ($userType -> userType), or a scalar name
// (Type::string() -> string). Returns "" when no `'type' =>` is present.
func gqlpTypeRef(cfg string) string {
	loc := reGQLPType.FindStringIndex(cfg)
	if loc == nil {
		return ""
	}
	expr := strings.TrimSpace(cfg[loc[1]:])
	// Bound the expr to its own value (up to a top-level comma).
	end := gqlpFieldConfigEnd(expr, 0)
	expr = strings.TrimSpace(expr[:end])

	// Unwrap Type::nonNull(...) / Type::listOf(...) repeatedly.
	for {
		if m := regexp.MustCompile(`^Type::(?:nonNull|listOf)\s*\(`).FindStringIndex(expr); m != nil {
			s, e := gqlpBalanced(expr, m[1]-1)
			if s < 0 {
				break
			}
			expr = strings.TrimSpace(expr[s:e])
			continue
		}
		break
	}
	// Scalar: Type::string() / Type::id() -> the scalar name.
	if m := regexp.MustCompile(`^Type::([A-Za-z_]\w*)\s*\(`).FindStringSubmatch(expr); m != nil {
		return m[1]
	}
	// Custom-type variable: $userType -> userType.
	if m := regexp.MustCompile(`^\$([A-Za-z_]\w*)`).FindStringSubmatch(expr); m != nil {
		return m[1]
	}
	// Bare class ref: UserType::class or UserType.
	if m := regexp.MustCompile(`^([A-Za-z_]\w*)`).FindStringSubmatch(expr); m != nil {
		return m[1]
	}
	return ""
}

// gqlpOperationForType maps an ObjectType `name` to its GraphQL operation kind,
// or "" when the type is an ordinary object type (a DTO, not an operation
// root). webonyx convention names the roots Query / Mutation / Subscription.
func gqlpOperationForType(name string) string {
	switch name {
	case "Query":
		return "Query"
	case "Mutation":
		return "Mutation"
	case "Subscription":
		return "Subscription"
	default:
		return ""
	}
}

func (e *graphqlPHPExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.graphql_php_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "graphql-php"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}

	src := string(file.Content)

	// File-signal gate: require a webonyx marker so this extractor is a no-op
	// on plain PHP / Laravel / Symfony files. `new ObjectType` / `new Schema`
	// (in conjunction with a GraphQL type expression) are the unambiguous
	// signals.
	if !strings.Contains(src, "ObjectType") && !strings.Contains(src, "new Schema") {
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

	// Map each ObjectType variable-assignment offset to its variable name so a
	// DTO ObjectType (named non-root) can record its source variable.
	varAt := make(map[int]string)
	for _, m := range reGQLPVarAssign.FindAllStringSubmatchIndex(src, -1) {
		// m[0] is the start of "$var", and the ObjectType `new` follows; key by
		// the position of the `new ObjectType(` so we can join with the loop
		// below, which iterates over `new ObjectType(` matches.
		newOff := strings.Index(src[m[0]:], "new")
		if newOff >= 0 {
			varAt[m[0]+newOff] = src[m[2]:m[3]]
		}
	}

	// 1. ObjectType definitions: roots → GRAPHQL endpoints, others → DTOs.
	for _, m := range reGQLPObjectType.FindAllStringIndex(src, -1) {
		// Balance the ObjectType( ... ) argument group, then the inner config
		// array literal.
		argStart, argEnd := gqlpBalanced(src, m[1]-1)
		if argStart < 0 {
			continue
		}
		cfg := src[argStart:argEnd]

		nameM := reGQLPName.FindStringSubmatch(cfg)
		if nameM == nil {
			continue // anonymous / runtime-computed name — not chased.
		}
		typeName := nameM[1]
		operation := gqlpOperationForType(typeName)

		// Locate the fields map within this ObjectType config.
		fieldsLoc := reGQLPFields.FindStringIndex(cfg)

		if operation == "" {
			// Ordinary object type → SCOPE.Schema DTO.
			ent := makeEntity("graphql_dto:"+typeName, "SCOPE.Schema", "dto", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "graphql-php",
				"provenance", "INFERRED_FROM_GRAPHQL_PHP_DTO",
				"dto_name", typeName, "graphql_dto_role", "object")
			if v, ok := varAt[m[0]]; ok {
				setProps(&ent, "dto_source_var", v)
			}
			add(ent)
			continue
		}

		// Operation root → one GRAPHQL endpoint per top-level field.
		if fieldsLoc == nil {
			continue
		}
		// fieldsLoc[1] points just past the `[`/`array(` token; step back one
		// char so gqlpBalanced starts on the opening bracket of the fields map.
		fs, fe := gqlpBalanced(cfg, fieldsLoc[1]-1)
		if fs < 0 {
			continue
		}
		fieldsBody := cfg[fs:fe]

		for _, fk := range gqlpTopLevelFieldKeys(fieldsBody) {
			field := fk.name
			// Bound this field's config to look for resolve / type.
			valStart := fk.offset
			if idx := strings.Index(fieldsBody[fk.offset:], "=>"); idx >= 0 {
				valStart = fk.offset + idx + 2
			}
			cfgEnd := gqlpFieldConfigEnd(fieldsBody, valStart)
			fieldCfg := fieldsBody[valStart:cfgEnd]

			path := "/graphql/" + typeName + "/" + field
			name := "GRAPHQL " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, argStart+fs+fk.offset))
			setProps(&ent, "framework", "graphql-php",
				"provenance", "INFERRED_FROM_GRAPHQL_PHP_RESOLVER",
				"http_method", "GRAPHQL", "verb", "GRAPHQL",
				"route_path", path, "graphql_operation", operation,
				"graphql_root", typeName, "graphql_field", field,
				"handler_name", typeName+"."+field)
			if reGQLPResolve.MatchString(fieldCfg) {
				setProps(&ent, "has_resolver", "true")
			}
			if ref := gqlpTypeRef(fieldCfg); ref != "" {
				setProps(&ent, "graphql_field_type", ref)
			}
			add(ent)
		}
	}

	// 2. new Schema([...]) → SCOPE.Service schema root with operation roots.
	for _, m := range reGQLPSchema.FindAllStringIndex(src, -1) {
		argStart, argEnd := gqlpBalanced(src, m[1]-1)
		if argStart < 0 {
			continue
		}
		cfg := src[argStart:argEnd]

		roots := map[string]string{}
		for _, rm := range reGQLPSchemaRoot.FindAllStringSubmatch(cfg, -1) {
			roots[rm[1]] = rm[2]
		}
		if len(roots) == 0 {
			continue
		}
		ent := makeEntity("graphql_schema:"+file.Path, "SCOPE.Service", "graphql_schema", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "graphql-php",
			"provenance", "INFERRED_FROM_GRAPHQL_PHP_SCHEMA")
		if v, ok := roots["query"]; ok {
			setProps(&ent, "query_root", v)
		}
		if v, ok := roots["mutation"]; ok {
			setProps(&ent, "mutation_root", v)
		}
		if v, ok := roots["subscription"]; ok {
			setProps(&ent, "subscription_root", v)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
