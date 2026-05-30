// Package kotlin — Micronaut and Quarkus auth, middleware, and DI extractors
// for Kotlin source.
//
// The corresponding Java extractors (internal/custom/java/micronaut.go,
// internal/custom/java/quarkus.go) are Java-only. This file adds coverage for
// the same frameworks when the source language is Kotlin.
//
// Covers:
//   - lang.kotlin.framework.micronaut  Auth/auth_coverage        (missing → partial)
//   - lang.kotlin.framework.micronaut  Middleware/middleware_coverage (missing → partial)
//   - lang.kotlin.framework.micronaut  DI/di_binding_extraction  (missing → partial)
//   - lang.kotlin.framework.micronaut  DI/di_injection_point     (missing → partial)
//   - lang.kotlin.framework.micronaut  DI/di_scope_resolution    (missing → partial)
//   - lang.kotlin.framework.quarkus    Auth/auth_coverage        (missing → partial)
//   - lang.kotlin.framework.quarkus    Middleware/middleware_coverage (missing → partial)
//   - lang.kotlin.framework.quarkus    DI/di_binding_extraction  (missing → partial)
//   - lang.kotlin.framework.quarkus    DI/di_injection_point     (missing → partial)
//   - lang.kotlin.framework.quarkus    DI/di_scope_resolution    (missing → partial)
//
// Micronaut auth: @Secured, @RolesAllowed, SecurityRule / ServerRequestFilter +
// ExecutorService auth interceptors. Micronaut uses JSR-330 annotations for DI
// (@Inject, @Singleton, @Prototype) and @ServerFilter for middleware.
//
// Quarkus auth: @RolesAllowed, @PermitAll, @DenyAll (JAX-RS security),
// @Authenticated (SmallRye JWT). Quarkus uses CDI scopes for DI
// (@ApplicationScoped, @RequestScoped, @Inject) and @Provider +
// ContainerRequestFilter for middleware.
//
// Honest limit: regex-based, file-local. No cross-file CDI graph resolution.
// Cells are partial, not full.
package kotlin

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_micronaut", &kotlinMicronautExtractor{})
	extractor.Register("custom_kotlin_quarkus", &kotlinQuarkusExtractor{})
}

// ---------------------------------------------------------------------------
// Micronaut Kotlin extractor
// ---------------------------------------------------------------------------

type kotlinMicronautExtractor struct{}

func (e *kotlinMicronautExtractor) Language() string { return "custom_kotlin_micronaut" }

var (
	// --- Micronaut Auth ---

	// reKtMnSecured matches @Secured("ROLE_ADMIN") / @Secured(SecurityRule.IS_AUTHENTICATED).
	reKtMnSecured = regexp.MustCompile(
		`@Secured\s*\(\s*(?:SecurityRule\.(\w+)|"([^"]*)")\s*\)`)

	// reKtMnRolesAllowed matches @RolesAllowed({"ADMIN", "USER"}).
	reKtMnRolesAllowed = regexp.MustCompile(
		`@RolesAllowed\s*\(([^)]+)\)`)

	// reKtMnPermitAll matches @PermitAll / @Unauthenticated.
	reKtMnPermitAll = regexp.MustCompile(`@(PermitAll|Unauthenticated)\b`)

	// --- Micronaut Middleware ---

	// reKtMnServerFilter matches @ServerFilter class declarations.
	reKtMnServerFilter = regexp.MustCompile(
		`(?m)^\s*(?:(?:open|abstract|internal|private|public)\s+)*class\s+(\w+)[^{]*:\s*[^{]*HttpServerFilter\b`)

	// reKtMnFilterAnnotation matches @Filter("/**") or @ServerFilter on a class.
	reKtMnFilterAnnotation = regexp.MustCompile(
		`@(?:ServerFilter|Filter)\s*(?:\([^)]*\))?\s*(?:@\w+[^{]*?)*class\s+(\w+)`)

	// --- Micronaut DI ---

	// reKtMnSingletonProto matches @Singleton or @Prototype class declarations.
	reKtMnSingletonProto = regexp.MustCompile(
		`@(Singleton|Prototype|RequestScoped)\b[^{]*class\s+(\w+)`)

	// reKtMnInject matches @Inject on a property or constructor parameter.
	reKtMnInject = regexp.MustCompile(
		`@Inject\b\s*(?:val|var)?\s*(\w+)\s*:\s*([A-Z][\w<>, ]*)`)

	// reKtMnBean matches @Bean on a function in a @Factory class.
	reKtMnBean = regexp.MustCompile(
		`@Bean\b[^{;]*fun\s+(\w+)\s*\(`)
)

func (e *kotlinMicronautExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_micronaut.extract",
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

	hasMicronaut := reKtMnSecured.MatchString(src) ||
		reKtMnRolesAllowed.MatchString(src) ||
		reKtMnServerFilter.MatchString(src) ||
		reKtMnFilterAnnotation.MatchString(src) ||
		reKtMnSingletonProto.MatchString(src) ||
		reKtMnInject.MatchString(src) ||
		reKtMnBean.MatchString(src) ||
		reKtMnPermitAll.MatchString(src)
	if !hasMicronaut {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, subtype, prop string, line int) {
		key := "SCOPE.Pattern:mn:" + subtype + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", subtype, file.Path, file.Language, line)
		setProps(&ent,
			"framework", "micronaut",
			"provenance", "INFERRED_FROM_MICRONAUT_KOTLIN",
			"kind", prop,
		)
		entities = append(entities, ent)
	}

	// Auth
	for _, m := range reKtMnSecured.FindAllStringSubmatchIndex(src, -1) {
		rule := ""
		if m[2] >= 0 {
			rule = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			rule = src[m[4]:m[5]]
		}
		add("@Secured:"+rule, "auth_policy", "secured", lineOf(src, m[0]))
	}
	for _, m := range reKtMnRolesAllowed.FindAllStringSubmatchIndex(src, -1) {
		roles := src[m[2]:m[3]]
		add("@RolesAllowed:"+roles, "auth_policy", "roles_allowed", lineOf(src, m[0]))
	}
	for _, m := range reKtMnPermitAll.FindAllStringSubmatchIndex(src, -1) {
		anno := src[m[2]:m[3]]
		add("@"+anno, "auth_policy", "permit_all", lineOf(src, m[0]))
	}

	// Middleware
	for _, m := range reKtMnServerFilter.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		add(className, "middleware", "server_filter", lineOf(src, m[0]))
	}
	for _, m := range reKtMnFilterAnnotation.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		add(className, "middleware", "filter_annotation", lineOf(src, m[0]))
	}

	// DI: bindings
	for _, m := range reKtMnSingletonProto.FindAllStringSubmatchIndex(src, -1) {
		scope := src[m[2]:m[3]]
		className := src[m[4]:m[5]]
		ent := makeEntity(className, "SCOPE.Pattern", "di_binding", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "micronaut",
			"di_scope", scope,
			"provenance", "INFERRED_FROM_MICRONAUT_KOTLIN",
		)
		key := "SCOPE.Pattern:mn:di_binding:" + className
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}
	for _, m := range reKtMnBean.FindAllStringSubmatchIndex(src, -1) {
		funcName := src[m[2]:m[3]]
		add(funcName, "di_binding", "bean_method", lineOf(src, m[0]))
	}

	// DI: injection points
	for _, m := range reKtMnInject.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		typeName := src[m[4]:m[5]]
		add(fieldName+":"+typeName, "di_injection_point", "field_inject", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Quarkus Kotlin extractor
// ---------------------------------------------------------------------------

type kotlinQuarkusExtractor struct{}

func (e *kotlinQuarkusExtractor) Language() string { return "custom_kotlin_quarkus" }

var (
	// --- Quarkus Auth ---

	// reKtQkRolesAllowed matches @RolesAllowed (JAX-RS / Quarkus Security).
	reKtQkRolesAllowed = regexp.MustCompile(
		`@RolesAllowed\s*\(([^)]+)\)`)

	// reKtQkPermitDenyAll matches @PermitAll / @DenyAll.
	reKtQkPermitDenyAll = regexp.MustCompile(`@(PermitAll|DenyAll)\b`)

	// reKtQkAuthenticated matches @Authenticated (SmallRye JWT / Quarkus OIDC).
	reKtQkAuthenticated = regexp.MustCompile(`@Authenticated\b`)

	// reKtQkNoCache matches @NoCache security hint.
	reKtQkNoCache = regexp.MustCompile(`@io\.quarkus\.security\.\w*`)

	// --- Quarkus Middleware ---

	// reKtQkContainerFilter matches @Provider ContainerRequestFilter /
	// ContainerResponseFilter implementations.
	reKtQkContainerFilter = regexp.MustCompile(
		`(?m)^\s*(?:(?:open|abstract|internal|private|public)\s+)*class\s+(\w+)[^{]*:\s*[^{]*ContainerRequest(?:Filter|Context)\b`)

	// reKtQkResponseFilter matches ContainerResponseFilter.
	reKtQkResponseFilter = regexp.MustCompile(
		`(?m)^\s*(?:(?:open|abstract|internal|private|public)\s+)*class\s+(\w+)[^{]*:\s*[^{]*ContainerResponseFilter\b`)

	// reKtQkProviderAnnotation matches @Provider on a class.
	reKtQkProviderAnnotation = regexp.MustCompile(
		`@Provider\b[^{]*class\s+(\w+)`)

	// --- Quarkus DI (CDI) ---

	// reKtQkCDIScope matches CDI scope annotations on Kotlin classes.
	reKtQkCDIScope = regexp.MustCompile(
		`@(ApplicationScoped|RequestScoped|SessionScoped|Singleton|Dependent)\b[^{]*class\s+(\w+)`)

	// reKtQkInject matches @Inject on a Kotlin property.
	reKtQkInject = regexp.MustCompile(
		`@Inject\b\s*(?:val|var|lateinit var)?\s*(\w+)\s*:\s*([A-Z][\w<>, ]*)`)

	// reKtQkProduces matches @Produces CDI producer method.
	reKtQkProduces = regexp.MustCompile(
		`@Produces\b[^{;]*fun\s+(\w+)\s*\(`)
)

func (e *kotlinQuarkusExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_quarkus.extract",
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

	hasQuarkus := reKtQkRolesAllowed.MatchString(src) ||
		reKtQkPermitDenyAll.MatchString(src) ||
		reKtQkAuthenticated.MatchString(src) ||
		reKtQkContainerFilter.MatchString(src) ||
		reKtQkResponseFilter.MatchString(src) ||
		reKtQkProviderAnnotation.MatchString(src) ||
		reKtQkCDIScope.MatchString(src) ||
		reKtQkInject.MatchString(src) ||
		reKtQkProduces.MatchString(src) ||
		reKtQkNoCache.MatchString(src)
	if !hasQuarkus {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, subtype, prop string, line int) {
		key := "SCOPE.Pattern:qk:" + subtype + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", subtype, file.Path, file.Language, line)
		setProps(&ent,
			"framework", "quarkus",
			"provenance", "INFERRED_FROM_QUARKUS_KOTLIN",
			"kind", prop,
		)
		entities = append(entities, ent)
	}

	// Auth
	for _, m := range reKtQkRolesAllowed.FindAllStringSubmatchIndex(src, -1) {
		roles := src[m[2]:m[3]]
		add("@RolesAllowed:"+roles, "auth_policy", "roles_allowed", lineOf(src, m[0]))
	}
	for _, m := range reKtQkPermitDenyAll.FindAllStringSubmatchIndex(src, -1) {
		anno := src[m[2]:m[3]]
		add("@"+anno, "auth_policy", "permit_deny_all", lineOf(src, m[0]))
	}
	for _, m := range reKtQkAuthenticated.FindAllStringSubmatchIndex(src, -1) {
		add("@Authenticated", "auth_policy", "authenticated", lineOf(src, m[0]))
	}

	// Middleware
	for _, m := range reKtQkContainerFilter.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		add(className, "middleware", "container_request_filter", lineOf(src, m[0]))
	}
	for _, m := range reKtQkResponseFilter.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		add(className, "middleware", "container_response_filter", lineOf(src, m[0]))
	}
	for _, m := range reKtQkProviderAnnotation.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		add(className, "middleware", "provider", lineOf(src, m[0]))
	}

	// DI: bindings
	for _, m := range reKtQkCDIScope.FindAllStringSubmatchIndex(src, -1) {
		scope := src[m[2]:m[3]]
		className := src[m[4]:m[5]]
		ent := makeEntity(className, "SCOPE.Pattern", "di_binding", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "quarkus",
			"di_scope", scope,
			"provenance", "INFERRED_FROM_QUARKUS_KOTLIN",
		)
		key := "SCOPE.Pattern:qk:di_binding:" + className
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}
	for _, m := range reKtQkProduces.FindAllStringSubmatchIndex(src, -1) {
		funcName := src[m[2]:m[3]]
		add(funcName, "di_binding", "produces_method", lineOf(src, m[0]))
	}

	// DI: injection points
	for _, m := range reKtQkInject.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		typeName := src[m[4]:m[5]]
		add(fieldName+":"+typeName, "di_injection_point", "field_inject", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
