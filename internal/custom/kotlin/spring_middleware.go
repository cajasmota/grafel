// Package kotlin — Spring Security middleware extractor for Kotlin Spring Boot.
//
// Covers the Middleware lane for lang.kotlin.framework.spring-boot:
//   - middleware_coverage (missing → partial)
//
// Detects the following Spring Security / MVC interceptor patterns in Kotlin
// source (identical syntax to Java — no language-specific deltas):
//
//  1. SecurityFilterChain @Bean declarations:
//     @Bean fun securityFilterChain(http: HttpSecurity): SecurityFilterChain
//
//  2. OncePerRequestFilter / GenericFilterBean subclasses:
//     class MyFilter : OncePerRequestFilter() { override fun doFilterInternal(...) }
//
//  3. HandlerInterceptor implementations:
//     class MyInterceptor : HandlerInterceptor { override fun preHandle(...) }
//
//  4. WebMvcConfigurer.addInterceptors:
//     override fun addInterceptors(registry: InterceptorRegistry) { ... }
//
//  5. @EnableWebSecurity class declaration (framework-level signal).
//
// Emits SCOPE.Pattern (subtype=middleware) for each detected middleware
// component, carrying middleware_type and framework="spring" properties.
//
// Honest limit: regex-based, file-local. Cross-file filter chains are not
// resolved. Hence the registry cell is flipped to partial, not full.
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
	extractor.Register("custom_kotlin_spring_middleware", &kotlinSpringMiddlewareExtractor{})
}

type kotlinSpringMiddlewareExtractor struct{}

func (e *kotlinSpringMiddlewareExtractor) Language() string {
	return "custom_kotlin_spring_middleware"
}

var (
	// reKtSecurityFilterChainBean matches Spring Security SecurityFilterChain @Bean method.
	// @Bean\nfun securityFilterChain(http: HttpSecurity): SecurityFilterChain { ... }
	// Uses (?s) so it spans newlines between @Bean and fun declaration.
	reKtSecurityFilterChainBean = regexp.MustCompile(
		`(?s)@Bean\b[^{]*?fun\s+(\w+)\s*\([^)]*\)\s*:\s*SecurityFilterChain`)

	// reKtOncePerRequestFilter matches a class implementing/extending OncePerRequestFilter.
	reKtOncePerRequestFilter = regexp.MustCompile(
		`(?m)^\s*(?:(?:open|abstract|internal|private|public)\s+)*class\s+(\w+)[^{]*:\s*[^{]*(?:OncePerRequestFilter|GenericFilterBean|Filter)\b`)

	// reKtHandlerInterceptor matches a class implementing HandlerInterceptor.
	reKtHandlerInterceptor = regexp.MustCompile(
		`(?m)^\s*(?:(?:open|abstract|internal|private|public)\s+)*class\s+(\w+)[^{]*:\s*[^{]*HandlerInterceptor\b`)

	// reKtAddInterceptors matches WebMvcConfigurer.addInterceptors override.
	reKtAddInterceptors = regexp.MustCompile(
		`override\s+fun\s+addInterceptors\s*\(\s*registry\s*:\s*InterceptorRegistry`)

	// reKtEnableWebSecurity matches @EnableWebSecurity class-level annotation.
	reKtEnableWebSecurity = regexp.MustCompile(
		`(?s)@EnableWebSecurity\b[^{]*?class\s+(\w+)`)

	// reKtWebFilter matches Spring WebFlux @Component WebFilter implementations.
	reKtWebFilter = regexp.MustCompile(
		`(?m)^\s*(?:(?:open|abstract|internal|private|public)\s+)*class\s+(\w+)[^{]*:\s*[^{]*WebFilter\b`)
)

func (e *kotlinSpringMiddlewareExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_spring_middleware.extract",
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

	// Quick bail-out: require at least one Spring Security indicator.
	hasSecurity := reKtSecurityFilterChainBean.MatchString(src) ||
		reKtOncePerRequestFilter.MatchString(src) ||
		reKtHandlerInterceptor.MatchString(src) ||
		reKtAddInterceptors.MatchString(src) ||
		reKtEnableWebSecurity.MatchString(src) ||
		reKtWebFilter.MatchString(src)
	if !hasSecurity {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, middlewareType string, line int) {
		key := "SCOPE.Pattern:mw:" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "spring",
			"middleware_type", middlewareType,
			"provenance", "INFERRED_FROM_SPRING_MIDDLEWARE",
		)
		entities = append(entities, ent)
	}

	// 1. SecurityFilterChain @Bean
	for _, m := range reKtSecurityFilterChainBean.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		add(name, "security_filter_chain", lineOf(src, m[0]))
	}

	// 2. OncePerRequestFilter / GenericFilterBean
	for _, m := range reKtOncePerRequestFilter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		add(name, "servlet_filter", lineOf(src, m[0]))
	}

	// 3. HandlerInterceptor
	for _, m := range reKtHandlerInterceptor.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		add(name, "handler_interceptor", lineOf(src, m[0]))
	}

	// 4. addInterceptors override (WebMvcConfigurer)
	for _, m := range reKtAddInterceptors.FindAllStringSubmatchIndex(src, -1) {
		add("addInterceptors", "mvc_configurer", lineOf(src, m[0]))
	}

	// 5. @EnableWebSecurity class
	for _, m := range reKtEnableWebSecurity.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		add(name, "security_config", lineOf(src, m[0]))
	}

	// 6. WebFilter (WebFlux)
	for _, m := range reKtWebFilter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		add(name, "web_filter", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
