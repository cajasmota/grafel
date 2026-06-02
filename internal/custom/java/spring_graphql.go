// Package java — regex-based GraphQL-server extractor for the two dominant
// JVM GraphQL frameworks: Spring for GraphQL and Netflix DGS.
//
// Both frameworks are *annotation-driven* and code-first: a controller method
// becomes a root field of the Query / Mutation / Subscription operation when it
// carries the framework's mapping annotation. This extractor recognises those
// annotations and emits, for each resolver method, ONE synthetic GraphQL
// endpoint in the canonical archigraph shape
//
//	SCOPE.Operation  name "GRAPHQL /graphql/<Operation>/<field>"
//	                 route_path "/graphql/<Operation>/<field>"  verb GRAPHQL
//
// — IDENTICAL to the gqlgen (Go), HotChocolate (C#), graphql-kotlin and the
// JS/TS Apollo resolver synthesis. Matching this shape is what lets the GraphQL
// client-link pass (#3667) and the cross-repo HTTP linker join a client
// operation `query { users }` to its JVM resolver. A HANDLES edge is emitted
// from the endpoint to the resolver method so handler attribution is recorded.
//
// Spring for GraphQL (org.springframework.graphql)
// -------------------------------------------------
//
//	@Controller
//	class UserController {
//	    @QueryMapping        public List<User> users() { ... }   // Query.users
//	    @MutationMapping     public User createUser(...) { ... }  // Mutation.createUser
//	    @SubscriptionMapping public Flux<Event> events() { ... }  // Subscription.events
//	    @QueryMapping(name = "allUsers") public List<User> users2() {...} // Query.allUsers
//	    @SchemaMapping(typeName = "Query", field = "node") public Node n() {...}
//	}
//
// The root field defaults to the METHOD name (lowerCamel, which Java methods
// already are). `@QueryMapping(name="x")` / `@SchemaMapping(field="x")` override
// it. `@SchemaMapping(typeName="Query")` selects the operation explicitly; a
// bare `@SchemaMapping` on a method whose typeName is a SDL type (not a root)
// is a per-type field resolver and is NOT a root operation — we only emit a
// root endpoint when typeName resolves to Query/Mutation/Subscription.
//
// Netflix DGS (com.netflix.graphql.dgs)
// -------------------------------------
//
//	@DgsComponent
//	class UserFetcher {
//	    @DgsQuery        public User user(...) { ... }            // Query.user
//	    @DgsMutation     public User addUser(...) { ... }         // Mutation.addUser
//	    @DgsSubscription public Publisher<E> events() { ... }     // Subscription.events
//	    @DgsQuery(field = "allUsers") public List<User> users() {...} // Query.allUsers
//	    @DgsData(parentType = "Query", field = "search") public R s() {...}
//	}
//
// `@DgsData(parentType="Query", field="x")` is the general form; the
// `@DgsQuery/@DgsMutation/@DgsSubscription` shorthands fix parentType and
// default field to the method name. As with Spring, a `@DgsData` whose
// parentType is a non-root SDL type is a field resolver, not a root operation,
// and is skipped for endpoint synthesis.
//
// HONEST LIMIT (schema / route = partial). Root-operation discovery is
// annotation-driven and file-local: a method is a root field iff its mapping
// annotation is present in the same file. Field resolvers on SDL object types
// (`@SchemaMapping(typeName="User", field="orders")`,
// `@DgsData(parentType="User", ...)`) are intentionally NOT emitted as
// /graphql/<root>/<field> endpoints — they resolve nested fields, not root
// operations. The transport mount path is assumed to be the conventional
// `/graphql`; a custom `spring.graphql.path` / DGS servlet mapping is not read.
//
// Frameworks: lang.java.framework.spring-graphql, lang.java.framework.dgs
// Issue #3615 (epic #3607) — Java GraphQL server extraction.
package java

import (
	"regexp"
	"strings"
)

// springGraphQLFrameworks gates ExtractSpringGraphQL. Both Spring-for-GraphQL
// and DGS run on java sources; the dispatch hands us the candidate framework
// token (see patterns_dispatch.go frameworkMarkers).
var springGraphQLFrameworks = map[string]bool{
	"spring_graphql": true, "spring-graphql": true, "springgraphql": true,
	"dgs": true, "netflix_dgs": true, "netflix-dgs": true,
	// The Spring/DGS annotations frequently coexist with a plain spring_boot
	// candidate token; accept it so a @Controller + @QueryMapping file is not
	// missed when only the generic spring marker fired.
	"spring_boot": true,
}

var (
	// Spring for GraphQL mapping annotations whose operation is fixed by the
	// annotation name. Capture group 1 = annotation simple name, group 2 = the
	// optional argument list (without parens), group 3..5 = the method
	// signature (return type / name / params) following the annotation.
	//
	// Matches e.g.:
	//   @QueryMapping public List<User> users() {
	//   @MutationMapping(name = "createUser") User create(@Argument In in) {
	//   @SubscriptionMapping Flux<Event> events() {
	reSpringGQLMapping = regexp.MustCompile(
		`(?s)@(QueryMapping|MutationMapping|SubscriptionMapping)\b\s*(\([^)]*\))?\s*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?` +
			`(?:<[^>]*>\s*)?[\w.$<>\[\], ?]+?\s+(\w+)\s*\(`,
	)
	// Spring @SchemaMapping(typeName="Query", field="x") — explicit form whose
	// operation comes from typeName and field from field. Group 1 = arg list,
	// group 2 = method name (used as the field fallback when field= absent).
	reSpringSchemaMapping = regexp.MustCompile(
		`(?s)@SchemaMapping\b\s*(\([^)]*\))?\s*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?` +
			`(?:<[^>]*>\s*)?[\w.$<>\[\], ?]+?\s+(\w+)\s*\(`,
	)
	// Netflix DGS shorthand annotations whose operation is fixed by name.
	reDgsShorthand = regexp.MustCompile(
		`(?s)@(DgsQuery|DgsMutation|DgsSubscription)\b\s*(\([^)]*\))?\s*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?` +
			`(?:<[^>]*>\s*)?[\w.$<>\[\], ?]+?\s+(\w+)\s*\(`,
	)
	// Netflix DGS general @DgsData(parentType="Query", field="x").
	reDgsData = regexp.MustCompile(
		`(?s)@DgsData\b\s*(\([^)]*\))?\s*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?` +
			`(?:<[^>]*>\s*)?[\w.$<>\[\], ?]+?\s+(\w+)\s*\(`,
	)

	// Annotation-argument extractors for name / field / typeName / parentType.
	reGQLArgName       = regexp.MustCompile(`\bname\s*=\s*"([^"]*)"`)
	reGQLArgField      = regexp.MustCompile(`\bfield\s*=\s*"([^"]*)"`)
	reGQLArgTypeName   = regexp.MustCompile(`\btypeName\s*=\s*"([^"]*)"`)
	reGQLArgParentType = regexp.MustCompile(`\bparentType\s*=\s*"([^"]*)"`)
)

// springGQLOperationForMapping maps a Spring/DGS shorthand annotation name to
// its GraphQL root operation. Returns "" for an unrecognised name.
func springGQLOperationForMapping(annName string) string {
	switch annName {
	case "QueryMapping", "DgsQuery":
		return "Query"
	case "MutationMapping", "DgsMutation":
		return "Mutation"
	case "SubscriptionMapping", "DgsSubscription":
		return "Subscription"
	}
	return ""
}

// normalizeRootType returns the canonical Query/Mutation/Subscription root name
// for an explicit typeName/parentType argument, or "" when the value is a
// non-root SDL object type (a field-resolver, not a root operation).
func normalizeRootType(v string) string {
	switch v {
	case "Query", "Mutation", "Subscription":
		return v
	}
	return ""
}

// firstArg returns the first submatch of re in args, or "".
func firstArg(re *regexp.Regexp, args string) string {
	if m := re.FindStringSubmatch(args); m != nil {
		return m[1]
	}
	return ""
}

// ExtractSpringGraphQL emits canonical GraphQL operation endpoints for Spring
// for GraphQL and Netflix DGS resolver methods, plus a HANDLES edge from each
// endpoint to its resolver method.
func ExtractSpringGraphQL(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !springGraphQLFrameworks[ctx.Framework] {
		return result
	}

	src := ctx.Source
	fp := ctx.FilePath

	// File-signal gate: require at least one Spring/DGS GraphQL mapping
	// annotation so this is a no-op on plain Spring MVC / JPA files.
	if !strings.Contains(src, "QueryMapping") &&
		!strings.Contains(src, "MutationMapping") &&
		!strings.Contains(src, "SubscriptionMapping") &&
		!strings.Contains(src, "SchemaMapping") &&
		!strings.Contains(src, "@Dgs") {
		return result
	}

	seenEnt := make(map[string]bool)
	seenRel := make(map[relKey]bool)

	// emit records one resolver method as a canonical GRAPHQL endpoint plus a
	// resolver-method entity and a HANDLES edge between them. `framework` is the
	// record-citing framework label ("spring-graphql" or "dgs").
	emit := func(operation, field, methodName, framework, provenance string, offset int) {
		if operation == "" || field == "" {
			return
		}
		owner := findEnclosingClass(src, offset)
		handlerName := methodName
		if owner != "" {
			handlerName = owner + "." + methodName
		}
		lineNo := lineOf(src, offset)
		path := "/graphql/" + operation + "/" + field
		name := "GRAPHQL " + path

		endpointRef := "scope:operation:" + framework + "_endpoint:" + fp + ":" + operation + "." + field
		addEntity(&result, seenEnt, SecondaryEntity{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "endpoint",
			SourceFile: fp,
			LineStart:  lineNo,
			LineEnd:    lineNo,
			Provenance: provenance,
			Ref:        endpointRef,
			Properties: map[string]any{
				"framework":         framework,
				"http_method":       "GRAPHQL",
				"verb":              "GRAPHQL",
				"route_path":        path,
				"path":              path,
				"graphql_operation": operation,
				"graphql_root":      owner,
				"graphql_field":     field,
				"resolver_method":   methodName,
				"handler_name":      handlerName,
			},
		})

		// Resolver-method entity + HANDLES edge (endpoint → resolver).
		resolverRef := "scope:operation:" + framework + "_resolver:" + fp + ":" + handlerName
		addEntity(&result, seenEnt, SecondaryEntity{
			Name:       handlerName,
			Kind:       "SCOPE.Operation",
			Subtype:    "graphql_resolver",
			SourceFile: fp,
			LineStart:  lineNo,
			LineEnd:    lineNo,
			Provenance: provenance,
			Ref:        resolverRef,
			Properties: map[string]any{
				"framework":         framework,
				"graphql_operation": operation,
				"graphql_field":     field,
				"resolver_method":   methodName,
				"resolver_class":    owner,
			},
		})
		addRel(&result, seenRel, Relationship{
			SourceRef:        endpointRef,
			TargetRef:        resolverRef,
			RelationshipType: "HANDLES",
			Properties: map[string]string{
				"framework":     framework,
				"graphql_field": field,
				"graphql_root":  operation,
				"match_source":  "graphql_resolver_annotation",
			},
		})
	}

	// 1. Spring for GraphQL shorthand mappings (@QueryMapping etc.).
	for _, m := range reSpringGQLMapping.FindAllStringSubmatchIndex(src, -1) {
		annName := src[m[2]:m[3]]
		args := ""
		if m[4] >= 0 {
			args = src[m[4]:m[5]]
		}
		methodName := src[m[6]:m[7]]
		operation := springGQLOperationForMapping(annName)
		field := methodName
		if n := firstArg(reGQLArgName, args); n != "" {
			field = n
		}
		emit(operation, field, methodName, "spring-graphql",
			"INFERRED_FROM_SPRING_GRAPHQL_MAPPING", m[0])
	}

	// 2. Spring for GraphQL @SchemaMapping(typeName=..., field=...).
	for _, m := range reSpringSchemaMapping.FindAllStringSubmatchIndex(src, -1) {
		args := ""
		if m[2] >= 0 {
			args = src[m[2]:m[3]]
		}
		methodName := src[m[4]:m[5]]
		// typeName defaults to the field-resolver case; only a Query/Mutation/
		// Subscription typeName denotes a ROOT operation we synthesize.
		operation := normalizeRootType(firstArg(reGQLArgTypeName, args))
		if operation == "" {
			continue // per-type field resolver — not a root operation.
		}
		field := methodName
		if f := firstArg(reGQLArgField, args); f != "" {
			field = f
		}
		emit(operation, field, methodName, "spring-graphql",
			"INFERRED_FROM_SPRING_GRAPHQL_SCHEMA_MAPPING", m[0])
	}

	// 3. Netflix DGS shorthand (@DgsQuery / @DgsMutation / @DgsSubscription).
	for _, m := range reDgsShorthand.FindAllStringSubmatchIndex(src, -1) {
		annName := src[m[2]:m[3]]
		args := ""
		if m[4] >= 0 {
			args = src[m[4]:m[5]]
		}
		methodName := src[m[6]:m[7]]
		operation := springGQLOperationForMapping(annName)
		field := methodName
		if f := firstArg(reGQLArgField, args); f != "" {
			field = f
		}
		emit(operation, field, methodName, "dgs",
			"INFERRED_FROM_DGS_MAPPING", m[0])
	}

	// 4. Netflix DGS general @DgsData(parentType=..., field=...).
	for _, m := range reDgsData.FindAllStringSubmatchIndex(src, -1) {
		args := ""
		if m[2] >= 0 {
			args = src[m[2]:m[3]]
		}
		methodName := src[m[4]:m[5]]
		operation := normalizeRootType(firstArg(reGQLArgParentType, args))
		if operation == "" {
			continue // field resolver on a non-root SDL type — skip.
		}
		field := methodName
		if f := firstArg(reGQLArgField, args); f != "" {
			field = f
		}
		emit(operation, field, methodName, "dgs",
			"INFERRED_FROM_DGS_DATA", m[0])
	}

	return result
}
