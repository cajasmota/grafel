package java

import (
	"fmt"
	"regexp"
	"strings"
)

// Spring WebFlux functional routing + WebFilter extractor.
//
// Spring WebFlux supports two programming models:
//
//  1. Annotation-based (@RestController / @GetMapping etc.) — already handled
//     by spring_routes.go + java_annotation_routes.go.
//
//  2. Functional DSL (RouterFunction / RouterFunctions.route()) — this file.
//
// # Functional DSL patterns extracted
//
//   - RouterFunctions.route().GET("/path", handler) chain
//   - RouterFunctions.route(GET("/path"), handler) fluent builder
//   - @Bean RouterFunction<ServerResponse> methods whose body chains
//     .GET/.POST/.PUT/.DELETE/.PATCH
//
// # Middleware patterns extracted
//
//   - Classes that implement WebFilter: filter(ServerWebExchange, WebFilterChain)
//
// Coverage cells delivered (#3080):
//   - Routing:    route_extraction  → partial
//   - Middleware: middleware_coverage → partial
//
// Refs: https://docs.spring.io/spring-framework/docs/current/reference/html/web-reactive.html

// springWebFluxFrameworks is the set of framework identifiers that activate
// this extractor.
var springWebFluxFrameworks = map[string]bool{
	"spring_webflux": true,
	"spring-webflux": true,
	"springwebflux":  true,
}

var (
	// Functional DSL — chained verb form:
	//   RouterFunctions.route()
	//     .GET("/path", handler)
	//     .POST("/path", handler)
	//     ...
	// Also matches the two-argument route() overload:
	//   RouterFunctions.route(GET("/path"), handler)
	// Capture group 1: HTTP verb; capture group 2: path string.
	webFluxChainedRE = regexp.MustCompile(
		`\.\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"]+)"`)

	// Static predicate overload: RouterFunctions.route(GET("/path"), handler)
	// Capture group 1: verb; capture group 2: path.
	webFluxStaticPredicateRE = regexp.MustCompile(
		`\bRequestPredicates\s*\.\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"]+)"`)

	// RouterFunctions.route() — file-level signal that functional routing is present.
	webFluxRouterFunctionsRE = regexp.MustCompile(
		`\bRouterFunctions\s*\.\s*route\s*\(`)

	// @Bean RouterFunction declaration — gate for bean-backed functional routers.
	webFluxBeanRouterFunctionRE = regexp.MustCompile(
		`@Bean\b[^;{]*?\bRouterFunction\b`)

	// WebFilter implementation — middleware detection.
	// Matches: implements WebFilter  (possibly with other interfaces).
	webFilterImplRE = regexp.MustCompile(
		`\bclass\s+(\w+)\b[^{]*\bimplements\b[^{]*\bWebFilter\b`)

	// filter() method inside a WebFilter — confirms the impl is genuine and
	// lets us find the line number precisely.
	webFilterMethodRE = regexp.MustCompile(
		`\bpublic\s+(?:Mono<Void>|reactor\.core\.publisher\.Mono<Void>)\s+filter\s*\(`)
)

// ExtractSpringWebFlux runs the Spring WebFlux functional routing + WebFilter
// extractor. It emits:
//   - Route entities for each functional-DSL endpoint
//   - Middleware entities for each WebFilter implementation
func ExtractSpringWebFlux(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !springWebFluxFrameworks[ctx.Framework] {
		return result
	}

	// Quick-exit: no WebFlux signals in this file.
	if !strings.Contains(ctx.Source, "RouterFunction") &&
		!strings.Contains(ctx.Source, "RouterFunctions") &&
		!strings.Contains(ctx.Source, "WebFilter") &&
		!strings.Contains(ctx.Source, "RequestPredicates") {
		return result
	}

	seen := make(map[string]bool)
	seenRels := make(map[relKey]bool)
	_ = seenRels // relationships reserved for future handler attribution

	// -----------------------------------------------------------------------
	// Route extraction: functional DSL
	// -----------------------------------------------------------------------

	// Only scan files that contain RouterFunctions.route() or a @Bean RouterFunction
	// to avoid false positives from annotation-based controllers that happen to
	// mention "RouterFunction" in an import.
	hasFunctionalRouting := webFluxRouterFunctionsRE.MatchString(ctx.Source) ||
		webFluxBeanRouterFunctionRE.MatchString(ctx.Source)

	if hasFunctionalRouting {
		// Form 1: chained .GET("/path", ...) / .POST("/path", ...) etc.
		for _, idx := range webFluxChainedRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
			if len(idx) < 6 {
				continue
			}
			verb := ctx.Source[idx[2]:idx[3]]
			rawPath := ctx.Source[idx[4]:idx[5]]

			ref := fmt.Sprintf("spring_webflux:route:%s:%s:%s", verb, rawPath, ctx.FilePath)
			e := SecondaryEntity{
				Name:       rawPath,
				Kind:       "Route",
				SourceFile: ctx.FilePath,
				LineStart:  lineOf(ctx.Source, idx[0]),
				Provenance: "INFERRED_FROM_SPRING_WEBFLUX_ROUTE",
				Ref:        ref,
				Properties: map[string]any{
					"http_verb":  verb,
					"path":       rawPath,
					"framework":  "spring_webflux",
					"route_type": "functional_dsl",
				},
			}
			addEntity(&result, seen, e)
		}

		// Form 2: RequestPredicates.GET("/path") — two-argument route() overload.
		for _, idx := range webFluxStaticPredicateRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
			if len(idx) < 6 {
				continue
			}
			verb := ctx.Source[idx[2]:idx[3]]
			rawPath := ctx.Source[idx[4]:idx[5]]

			ref := fmt.Sprintf("spring_webflux:route:predicate:%s:%s:%s", verb, rawPath, ctx.FilePath)
			e := SecondaryEntity{
				Name:       rawPath,
				Kind:       "Route",
				SourceFile: ctx.FilePath,
				LineStart:  lineOf(ctx.Source, idx[0]),
				Provenance: "INFERRED_FROM_SPRING_WEBFLUX_PREDICATE_ROUTE",
				Ref:        ref,
				Properties: map[string]any{
					"http_verb":  verb,
					"path":       rawPath,
					"framework":  "spring_webflux",
					"route_type": "predicate_dsl",
				},
			}
			addEntity(&result, seen, e)
		}
	}

	// -----------------------------------------------------------------------
	// Middleware: WebFilter implementations
	// -----------------------------------------------------------------------
	for _, idx := range webFilterImplRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		className := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("spring_webflux:middleware:webfilter:%s:%s", className, ctx.FilePath)
		e := SecondaryEntity{
			Name:       className,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_SPRING_WEBFLUX_WEBFILTER",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "spring_webflux",
				"middleware_type": "web_filter",
				"filter_class":    className,
			},
		}
		addEntity(&result, seen, e)
	}

	// Also detect filter() method as a secondary confirmation / line-number
	// anchor if no class-level WebFilter impl was found.
	if webFilterMethodRE.MatchString(ctx.Source) && !webFilterImplRE.MatchString(ctx.Source) {
		pos := webFilterMethodRE.FindStringIndex(ctx.Source)
		ref := fmt.Sprintf("spring_webflux:middleware:webfilter_method:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "WebFilter",
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, pos[0]),
			Provenance: "INFERRED_FROM_SPRING_WEBFLUX_WEBFILTER",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "spring_webflux",
				"middleware_type": "web_filter",
			},
		}
		addEntity(&result, seen, e)
	}

	return result
}
