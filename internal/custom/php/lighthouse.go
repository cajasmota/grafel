// Package php — regex-based Lighthouse (Laravel GraphQL) SDL extractor.
//
// Lighthouse (nuwave/lighthouse) is the schema-first Laravel GraphQL framework.
// It sits ON TOP of webonyx/graphql-php (modelled separately in graphql.go,
// issue #3544): instead of constructing ObjectType instances in PHP, Lighthouse
// reads a `.graphql` SDL schema in which server-side directives — `@all`,
// `@paginate`, `@find`, `@create`, `@field(resolver: ...)`, `@hasMany`, … —
// declare how each field is resolved. A typical schema.graphql:
//
//	type Query {
//	    users: [User!]! @paginate
//	    user(id: ID! @eq): User @find
//	    me: User @field(resolver: "App\\GraphQL\\Queries\\Me")
//	}
//
//	type Mutation {
//	    createUser(name: String!): User @create
//	    deleteUser(id: ID!): User @delete
//	}
//
//	type User {
//	    id: ID!
//	    name: String!
//	    posts: [Post!]! @hasMany
//	}
//
// Mapping (mirrors webonyx graphql.go / rust async-graphql / jsts
// graphql-resolvers):
//
//   - Each field of the root operation types Query / Mutation / Subscription
//     becomes a synthetic GRAPHQL endpoint with verb GRAPHQL and path
//     /graphql/<Operation>/<field>. The Lighthouse resolver directive on the
//     field (@all, @paginate, @field, …) is the handler attribution; for
//     @field(resolver: "Class@method") the resolver class is captured.
//   - Each non-root SDL `type` becomes a SCOPE.Schema DTO. Its relation fields
//     carrying an Eloquent-relation directive (@hasMany, @belongsTo, …) record
//     the directive so the field's resolver provenance is honest.
//
// HONEST LIMIT: this is file-local and SDL-structural. It reads the Lighthouse
// directive vocabulary off the `.graphql` schema; it does NOT chase the PHP
// resolver class named by @field(resolver:) into its definition (recorded by
// name only), nor schema-stitching across multiple imported `.graphql` files,
// nor programmatically-registered directives. PHP `#[...]` attribute resolvers
// (custom directive classes) are recorded by name when the field cites them via
// @field, but their PHP body is not parsed here.
//
// Registration key: "custom_php_lighthouse". Lighthouse SDL lives in `.graphql`
// files (language "graphql"), so this extractor gates on language "graphql"
// plus a Lighthouse-directive signal — unlike the php-language extractors in
// this package — and is reached because the dispatch routes the "graphql"
// language to the custom_php_ extractor set (mirrors prisma/sql → JS routing).
//
// Issue #3556 (epic #3505) — PHP Lighthouse (Laravel GraphQL) coverage.
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
	extractor.Register("custom_php_lighthouse", &lighthouseExtractor{})
}

type lighthouseExtractor struct{}

func (e *lighthouseExtractor) Language() string { return "custom_php_lighthouse" }

var (
	// `type Foo {` — start of an SDL type definition. Group 1 is the type name.
	// The byte offset of the `{` lets the caller balance the field block.
	reLHType = regexp.MustCompile(`(?m)^\s*(?:extend\s+)?type\s+([A-Za-z_]\w*)\s*(?:implements[^\{]*)?\{`)
	// A field declaration inside a type body: `name(args): ReturnType @dir`.
	// Group 1 is the field name; the rest of the line (group 2) carries the
	// return type and any directives. Args in parens are tolerated.
	reLHField = regexp.MustCompile(`(?m)^\s*([A-Za-z_]\w*)\s*(?:\([^)]*\))?\s*:\s*([^\n]+)`)
	// A server-side resolver directive on a Query/Mutation/Subscription field.
	// These are the Lighthouse directives that make a field addressable.
	reLHResolverDir = regexp.MustCompile(`@(all|paginate|find|first|count|create|update|upsert|delete|forceDelete|restore|field|aggregate|hasMany|hasOne|belongsTo|belongsToMany|morphMany|morphOne|morphTo)\b`)
	// `@field(resolver: "App\\GraphQL\\Queries\\Me")` — captures the resolver
	// class/callable string named by an explicit @field directive. Group 1 is
	// the resolver reference (quote-stripped by the caller).
	reLHFieldResolver = regexp.MustCompile(`@field\s*\(\s*resolver\s*:\s*"([^"]+)"`)
)

// lhResolverDirective returns the first Lighthouse server-side resolver
// directive present on a field's line (e.g. "paginate", "all", "field"), or ""
// when the field carries no such directive (a plain data field).
func lhResolverDirective(line string) string {
	if m := reLHResolverDir.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// lhTypeBody returns the byte range [open+1, close) of the `{ ... }` block whose
// opening brace is the first `{` at or after from in src, balancing nested
// braces. Returns (-1,-1) when no balanced block is found.
func lhTypeBody(src string, from int) (int, int) {
	open := strings.IndexByte(src[from:], '{')
	if open < 0 {
		return -1, -1
	}
	open += from
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

// lhOperationForType maps an SDL type name to its GraphQL operation kind, or ""
// when the type is an ordinary object type (a DTO). Lighthouse/GraphQL
// convention names the roots Query / Mutation / Subscription.
func lhOperationForType(name string) string {
	switch name {
	case "Query", "Mutation", "Subscription":
		return name
	default:
		return ""
	}
}

func (e *lighthouseExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.lighthouse_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "lighthouse"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	// Lighthouse SDL lives in `.graphql` files (language "graphql"), not "php".
	if len(file.Content) == 0 || file.Language != "graphql" {
		return nil, nil
	}

	src := string(file.Content)

	// File-signal gate: require a Lighthouse-specific server-side directive so
	// this extractor is a no-op on plain GraphQL SDL (client schemas, Apollo,
	// federation, …). The directive vocabulary below is Lighthouse's own.
	if !reLHResolverDir.MatchString(src) {
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

	for _, m := range reLHType.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		operation := lhOperationForType(typeName)

		bs, be := lhTypeBody(src, m[0])
		if bs < 0 {
			continue
		}
		body := src[bs:be]

		if operation == "" {
			// Ordinary object type → SCOPE.Schema DTO.
			ent := makeEntity("lighthouse_dto:"+typeName, "SCOPE.Schema", "dto", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "lighthouse",
				"provenance", "INFERRED_FROM_LIGHTHOUSE_TYPE",
				"dto_name", typeName, "graphql_dto_role", "object")
			add(ent)
			continue
		}

		// Operation root → one GRAPHQL endpoint per field carrying a resolver
		// directive. Fields without a resolver directive on a root type are
		// rare but skipped (Lighthouse always resolves root fields via a
		// directive or a convention-matched resolver class).
		for _, fm := range reLHField.FindAllStringSubmatchIndex(body, -1) {
			field := body[fm[2]:fm[3]]
			line := body[fm[4]:fm[5]]
			directive := lhResolverDirective(line)
			if directive == "" {
				continue
			}

			path := "/graphql/" + operation + "/" + field
			name := "GRAPHQL " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, bs+fm[0]))
			setProps(&ent, "framework", "lighthouse",
				"provenance", "INFERRED_FROM_LIGHTHOUSE_RESOLVER",
				"http_method", "GRAPHQL", "verb", "GRAPHQL",
				"route_path", path, "graphql_operation", operation,
				"graphql_root", operation, "graphql_field", field,
				"handler_name", operation+"."+field,
				"lighthouse_directive", directive)
			if rm := reLHFieldResolver.FindStringSubmatch(line); rm != nil {
				setProps(&ent, "resolver_class", rm[1])
			}
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
