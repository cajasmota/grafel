// Package kotlin — regex-based route extractors for Micronaut, Quarkus (JAX-RS),
// http4k, and Spring Boot (annotation-based composition) Kotlin frameworks.
//
// Routing.route_extraction coverage for:
//   - lang.kotlin.framework.spring-boot  (class @RequestMapping + method verb annotations)
//   - lang.kotlin.framework.micronaut    (@Controller + @Get/@Post/…)
//   - lang.kotlin.framework.quarkus      (JAX-RS @Path + @GET/@POST/…)
//   - lang.kotlin.framework.http4k       (routes("/p" bind …) DSL)
//
// Issue #3275 — Part of Kotlin routing + ORM-depth builds.
package kotlin

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
	extractor.Register("custom_kotlin_spring_routes", &kotlinSpringRoutesExtractor{})
	extractor.Register("custom_kotlin_micronaut_routes", &kotlinMicronautRoutesExtractor{})
	extractor.Register("custom_kotlin_quarkus_routes", &kotlinQuarkusRoutesExtractor{})
	extractor.Register("custom_kotlin_http4k_routes", &kotlinHttp4kRoutesExtractor{})
}

// ---------------------------------------------------------------------------
// Spring Boot — annotation-based route composition
// ---------------------------------------------------------------------------

// kotlinSpringRoutesExtractor emits SCOPE.Operation endpoint entities for
// Spring MVC / Spring Boot Kotlin controllers by composing the class-level
// @RequestMapping prefix with each method-level verb annotation.
//
// Pattern:
//
//	@RestController
//	@RequestMapping("/api")
//	class Foo {
//	    @GetMapping("/bar")   →  GET /api/bar
//	    @PostMapping("/baz")  →  POST /api/baz
//	}
type kotlinSpringRoutesExtractor struct{}

func (e *kotlinSpringRoutesExtractor) Language() string { return "custom_kotlin_spring_routes" }

var (
	// reKtSpringClassMapping matches @RequestMapping on a class (with optional path arg).
	// Handles positional and value=/path= named args.
	reKtSpringClassMapping = regexp.MustCompile(
		`@RequestMapping\s*(?:\(\s*(?:value\s*=\s*|path\s*=\s*)?\"([^\"]*)\"\s*\))?`)

	// reKtSpringController matches @RestController or @Controller.
	reKtSpringController = regexp.MustCompile(`@(?:Rest)?Controller\b`)

	// reKtSpringVerbMapping matches @GetMapping, @PostMapping, etc. with optional path.
	reKtSpringVerbMapping = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch|Head|Options)Mapping\s*(?:\(\s*(?:value\s*=\s*|path\s*=\s*)?\"([^\"]*)\"\s*\))?`)

	// reKtSpringFunName matches a Kotlin function declaration name.
	reKtSpringFunName = regexp.MustCompile(`fun\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

var ktSpringVerbMap = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS",
}

func (e *kotlinSpringRoutesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_spring_routes.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "spring-boot"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	if !reKtSpringController.MatchString(src) {
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

	// Extract class-level @RequestMapping prefix.
	classPrefix := ""
	if m := reKtSpringClassMapping.FindStringSubmatchIndex(src); m != nil {
		if m[2] >= 0 {
			classPrefix = src[m[2]:m[3]]
		}
	}

	// Find each method-level verb mapping.
	for _, m := range reKtSpringVerbMapping.FindAllStringSubmatchIndex(src, -1) {
		verb := ktSpringVerbMap[src[m[2]:m[3]]]
		methodPath := ""
		if m[4] >= 0 {
			methodPath = src[m[4]:m[5]]
		}
		fullPath := joinKtRoutePaths(classPrefix, methodPath)
		if fullPath == "" {
			fullPath = "/"
		}
		name := verb + " " + fullPath
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "spring-boot",
			"http_method", verb,
			"path", fullPath,
			"provenance", "INFERRED_FROM_SPRING_ANNOTATION",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Micronaut — @Controller + @Get/@Post/… annotations
// ---------------------------------------------------------------------------

// kotlinMicronautRoutesExtractor emits SCOPE.Operation endpoint entities for
// Micronaut Kotlin controllers.
//
// Pattern:
//
//	@Controller("/x")
//	class Foo {
//	    @Get("/y")   →  GET /x/y
//	}
type kotlinMicronautRoutesExtractor struct{}

func (e *kotlinMicronautRoutesExtractor) Language() string { return "custom_kotlin_micronaut_routes" }

var (
	// reKtMnController matches @Controller with an optional base path.
	reKtMnController = regexp.MustCompile(
		`@Controller\s*(?:\(\s*(?:value\s*=\s*)?\"([^\"]*)\"\s*\))?`)

	// reKtMnVerb matches @Get, @Post, @Put, @Delete, @Patch, @Head, @Options.
	reKtMnVerb = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch|Head|Options)\s*(?:\(\s*(?:value\s*=\s*)?\"([^\"]*)\"\s*\))?`)
)

var ktMnVerbMap = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS",
}

func (e *kotlinMicronautRoutesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_micronaut_routes.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "micronaut"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "@Controller") {
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

	// Extract class-level @Controller base path.
	basePath := ""
	if m := reKtMnController.FindStringSubmatchIndex(src); m != nil {
		if m[2] >= 0 {
			basePath = src[m[2]:m[3]]
		}
	}

	// Find each verb handler.
	for _, m := range reKtMnVerb.FindAllStringSubmatchIndex(src, -1) {
		verb := ktMnVerbMap[src[m[2]:m[3]]]
		methodPath := ""
		if m[4] >= 0 {
			methodPath = src[m[4]:m[5]]
		}
		fullPath := joinKtRoutePaths(basePath, methodPath)
		if fullPath == "" {
			fullPath = "/"
		}
		name := verb + " " + fullPath
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "micronaut",
			"http_method", verb,
			"path", fullPath,
			"provenance", "INFERRED_FROM_MICRONAUT_ANNOTATION",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Quarkus — JAX-RS @Path + @GET/@POST/…
// ---------------------------------------------------------------------------

// kotlinQuarkusRoutesExtractor emits SCOPE.Operation endpoint entities for
// Quarkus (JAX-RS) Kotlin resources.
//
// Pattern:
//
//	@Path("/items")
//	class ItemResource {
//	    @GET              →  GET /items
//	    @POST             →  POST /items
//	    @Path("/{id}")
//	    @GET              →  GET /items/{id}
//	}
type kotlinQuarkusRoutesExtractor struct{}

func (e *kotlinQuarkusRoutesExtractor) Language() string { return "custom_kotlin_quarkus_routes" }

var (
	// reKtQkClassPath matches class-level @Path("...").
	reKtQkClassPath = regexp.MustCompile(
		`@Path\s*\(\s*\"([^\"]*)\"\s*\)`)

	// reKtQkHTTPVerb matches standalone @GET, @POST, @PUT, @DELETE, @PATCH, @HEAD, @OPTIONS.
	reKtQkHTTPVerb = regexp.MustCompile(
		`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

	// reKtQkMethodPath matches a method-level @Path("...") that may immediately
	// precede or follow an HTTP verb annotation.
	reKtQkMethodPath = regexp.MustCompile(
		`@Path\s*\(\s*\"([^\"]*)\"\s*\)\s*(?:\n\s*)?@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

	// reKtQkVerbThenPath matches @VERB followed by @Path.
	reKtQkVerbThenPath = regexp.MustCompile(
		`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b\s*(?:\n\s*)?@Path\s*\(\s*\"([^\"]*)\"\s*\)`)
)

func (e *kotlinQuarkusRoutesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_quarkus_routes.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "quarkus"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "@Path") && !strings.Contains(src, "@GET") && !strings.Contains(src, "@POST") {
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

	// Extract class-level @Path — first occurrence is the class path.
	basePath := ""
	allPaths := reKtQkClassPath.FindAllStringSubmatchIndex(src, -1)
	if len(allPaths) > 0 {
		m := allPaths[0]
		basePath = src[m[2]:m[3]]
	}

	// Pattern 1: @Path("sub") then @VERB on next line.
	for _, m := range reKtQkMethodPath.FindAllStringSubmatchIndex(src, -1) {
		subPath := src[m[2]:m[3]]
		verb := src[m[4]:m[5]]
		fullPath := joinKtRoutePaths(basePath, subPath)
		name := verb + " " + fullPath
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "quarkus",
			"http_method", verb,
			"path", fullPath,
			"provenance", "INFERRED_FROM_JAXRS_ANNOTATION",
		)
		add(ent)
	}

	// Pattern 2: @VERB then @Path("sub").
	for _, m := range reKtQkVerbThenPath.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		subPath := src[m[4]:m[5]]
		fullPath := joinKtRoutePaths(basePath, subPath)
		name := verb + " " + fullPath
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "quarkus",
			"http_method", verb,
			"path", fullPath,
			"provenance", "INFERRED_FROM_JAXRS_ANNOTATION",
		)
		add(ent)
	}

	// Pattern 3: bare @GET/@POST/… with no sub-@Path → maps to basePath.
	// Collect all positions that were already covered by patterns 1 and 2.
	coveredOffsets := make(map[int]bool)
	for _, m := range reKtQkMethodPath.FindAllStringSubmatchIndex(src, -1) {
		// The verb position is further into the match; mark the whole range.
		for i := m[0]; i <= m[1]; i++ {
			coveredOffsets[i] = true
		}
	}
	for _, m := range reKtQkVerbThenPath.FindAllStringSubmatchIndex(src, -1) {
		for i := m[0]; i <= m[1]; i++ {
			coveredOffsets[i] = true
		}
	}
	for _, m := range reKtQkHTTPVerb.FindAllStringSubmatchIndex(src, -1) {
		if coveredOffsets[m[0]] {
			continue
		}
		verb := src[m[2]:m[3]]
		fullPath := basePath
		if fullPath == "" {
			fullPath = "/"
		}
		name := verb + " " + fullPath
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "quarkus",
			"http_method", verb,
			"path", fullPath,
			"provenance", "INFERRED_FROM_JAXRS_ANNOTATION",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// http4k — routes("/p" bind ...) DSL
// ---------------------------------------------------------------------------

// kotlinHttp4kRoutesExtractor emits SCOPE.Operation endpoint entities for
// http4k routing DSL.
//
// Patterns:
//
//	routes(
//	    "/ping" bind GET to ::pingHandler,
//	    "/users" bind POST to ::createUser,
//	)
//
//	routes(
//	    "/api" bind routes(
//	        "/users" bind GET to ::listUsers,
//	    ),
//	)
type kotlinHttp4kRoutesExtractor struct{}

func (e *kotlinHttp4kRoutesExtractor) Language() string { return "custom_kotlin_http4k_routes" }

var (
	// reHttp4kBind matches:  "path" bind METHOD to handler
	// Captures: (path, METHOD)
	reHttp4kBind = regexp.MustCompile(
		`"([^"]+)"\s+bind\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

	// reHttp4kNestedBind matches:  "prefix" bind routes(
	// Captures: (prefix)
	reHttp4kNestedBind = regexp.MustCompile(
		`"([^"]+)"\s+bind\s+routes\s*\(`)
)

func (e *kotlinHttp4kRoutesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_http4k_routes.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "http4k"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "bind") || !strings.Contains(src, "routes") {
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

	// Flat bindings: "path" bind METHOD to handler.
	for _, m := range reHttp4kBind.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		verb := src[m[4]:m[5]]
		name := verb + " " + path
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "http4k",
			"http_method", verb,
			"path", path,
			"provenance", "INFERRED_FROM_HTTP4K_BIND",
		)
		add(ent)
	}

	// Nested prefix blocks: extract the prefix as a route scope entity.
	for _, m := range reHttp4kNestedBind.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(prefix, "SCOPE.Operation", "endpoint", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "http4k",
			"path", prefix,
			"route_type", "scope",
			"provenance", "INFERRED_FROM_HTTP4K_NESTED_BIND",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// joinKtRoutePaths composes a base path and a sub-path, normalising double
// slashes. Both parts may be empty.
func joinKtRoutePaths(base, sub string) string {
	if base == "" && sub == "" {
		return "/"
	}
	if base == "" {
		return ensureLeadingSlash(sub)
	}
	if sub == "" {
		return ensureLeadingSlash(base)
	}
	b := strings.TrimRight(base, "/")
	s := ensureLeadingSlash(sub)
	return b + s
}

func ensureLeadingSlash(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}
