// http_endpoint_java_middleware_xframework.go — cross-framework JVM middleware
// chain binding (#3859, child of epic #3854).
//
// The base Spring pass (http_endpoint_java_middleware.go) resolves Servlet
// FilterRegistrationBean filters and Spring MVC HandlerInterceptors. This file
// brings the OTHER JVM HTTP frameworks onto the same cross-stack
// `middleware_chain`/`middleware_count`/`middleware_names`/`middleware_scope`
// contract:
//
//	Spring-WebFlux — a class `implements WebFilter` is a global reactive request
//	                 filter that wraps every route in the same file (Spring binds
//	                 a WebFilter to the whole reactive chain). Bound to every
//	                 WebFlux/Spring endpoint this file synthesised. Scope=filter.
//	JAX-RS         — a `@Provider class … implements ContainerRequestFilter`
//	  (Jakarta)      (or ContainerResponseFilter) is a global JAX-RS request
//	                 filter applied to every resource method in the application,
//	                 unless `@NameBinding`-restricted. Without a name-binding it is
//	                 global → bound to every JAX-RS endpoint in the file.
//	                 Scope=filter. Honest-partial: a `@NameBinding`-annotated
//	                 filter is NOT globally bound (its activation is selective and
//	                 not statically resolvable to a specific route here).
//	Javalin        — `app.before("/glob", handler)` / `app.after(...)` register
//	                 before/after handlers. A path-glob `before("/api/*", …)` binds
//	                 to the routes whose path matches the glob; a bare
//	                 `before(handler)` (no path) binds to every Javalin route in
//	                 the file. Scope=filter.
//
// Honest binding discipline mirrors the Spring pass: a filter is only bound when
// it can be statically attributed to the routes this file emits. No fabricated
// path matches; same-file scope only.
//
// Like the base pass this mutates endpoint Properties in place and never adds or
// removes entities. It runs INSIDE applyJavaMiddlewareCoverage (one chain per
// endpoint, deduped, stamped once) so the Spring filters/interceptors and these
// cross-framework filters compose into a single ordered chain.
package engine

import (
	"regexp"
	"strings"
)

// javaWebFilterClassRe captures a Spring-WebFlux `WebFilter` implementation:
// `class LoggingWebFilter implements WebFilter`. Group 1 = the class name. The
// `@Component`/`@Order` decorations are recovered separately for ordering.
var javaWebFilterClassRe = regexp.MustCompile(
	`class\s+(\w+)[^\{]*\bimplements\b[^\{]*\bWebFilter\b`)

// javaJaxrsFilterClassRe captures a JAX-RS provider filter:
// `class AuthRequestFilter implements ContainerRequestFilter`. Group 1 = the
// class name. Both ContainerRequestFilter and ContainerResponseFilter qualify.
var javaJaxrsFilterClassRe = regexp.MustCompile(
	`class\s+(\w+)[^\{]*\bimplements\b[^\{]*\b(ContainerRequestFilter|ContainerResponseFilter)\b`)

// javalinBeforeAfterRe captures a Javalin `app.before("/glob", …)` /
// `app.after(…)` / `before(…)` registration. Group 1 = the verb (before|after),
// group 2 = the optional quoted path glob (empty for the no-path form).
var javalinBeforeAfterRe = regexp.MustCompile(
	`\b(?:\w+\.)?(before|after)(?:Matched)?\s*\(\s*(?:"([^"]*)"\s*,)?`)

// indexJavaWebFilters returns the WebFilter classes declared in the file as
// global filter chain entries (one per WebFilter class). Empty when the file
// declares no WebFilter.
func indexJavaWebFilters(content string) []middlewareEntry {
	if !strings.Contains(content, "WebFilter") {
		return nil
	}
	var out []middlewareEntry
	for _, m := range javaWebFilterClassRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		out = append(out, middlewareEntry{
			Name:     name,
			Expr:     name,
			Scope:    javaMWScopeFilter,
			AuthKind: middlewareAuthKind(name),
		})
	}
	return out
}

// indexJaxrsProviderFilters returns the JAX-RS global provider filters declared
// in the file. A filter restricted by a `@NameBinding` meta-annotation is
// honest-partial (selective, not globally bound) and is skipped here.
func indexJaxrsProviderFilters(content string) []middlewareEntry {
	if !strings.Contains(content, "ContainerRequestFilter") &&
		!strings.Contains(content, "ContainerResponseFilter") {
		return nil
	}
	var out []middlewareEntry
	for _, m := range javaJaxrsFilterClassRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		// Honest-partial: a name-bound filter (its own @NameBinding-meta custom
		// annotation present on the class block) is selectively activated, not
		// global; skip rather than over-bind. Detect the common explicit marker.
		block := content[maxInt(0, m[0]-200):m[0]]
		if strings.Contains(block, "@NameBinding") {
			continue
		}
		out = append(out, middlewareEntry{
			Name:     name,
			Expr:     name,
			Scope:    javaMWScopeFilter,
			AuthKind: middlewareAuthKind(name),
		})
	}
	return out
}

// javalinFilter is a Javalin before/after handler with its optional path glob.
type javalinFilter struct {
	entry middlewareEntry
	glob  string // path glob ("" ⇒ applies to all routes)
}

// indexJavalinFilters returns the Javalin before/after registrations in the
// file. A glob path is kept so it can be matched against each route; a bare
// before/after with no path binds to every route.
func indexJavalinFilters(content string) []javalinFilter {
	// File-signal gate (mirrors synthesizeJavalin): only treat before/after as
	// Javalin handlers in a Javalin file, so a stray `.before(`/`.after(` in an
	// unrelated JVM file never fabricates a chain entry.
	if !strings.Contains(content, "javalin") && !strings.Contains(content, "Javalin") {
		return nil
	}
	if !strings.Contains(content, "before") && !strings.Contains(content, "after") {
		return nil
	}
	var out []javalinFilter
	for _, m := range javalinBeforeAfterRe.FindAllStringSubmatchIndex(content, -1) {
		phase := content[m[2]:m[3]]
		glob := ""
		if m[4] >= 0 {
			glob = content[m[4]:m[5]]
		}
		name := "javalin:" + phase
		if glob != "" {
			name = "javalin:" + phase + "(" + glob + ")"
		}
		out = append(out, javalinFilter{
			entry: middlewareEntry{
				Name:     name,
				Expr:     name,
				Scope:    javaMWScopeFilter,
				AuthKind: middlewareAuthKind(glob),
			},
			glob: glob,
		})
	}
	return out
}

// javalinGlobMatches reports whether a Javalin path glob matches a route path.
// An empty glob matches every route. `/api/*` matches `/api/...`; `*` / `/*`
// match everything.
func javalinGlobMatches(glob, routePath string) bool {
	if glob == "" {
		return true
	}
	// Javalin globs use `*` (single segment) and `<name>` path params; for the
	// before/after coverage we treat `*` as a prefix wildcard.
	if glob == "*" || glob == "/*" {
		return true
	}
	star := strings.IndexAny(glob, "*<")
	if star < 0 {
		return springPathEqual(glob, routePath)
	}
	prefix := strings.TrimRight(glob[:star], "/")
	if prefix == "" {
		return true
	}
	rp := strings.TrimRight(routePath, "/")
	return rp == prefix || strings.HasPrefix(rp, prefix+"/") || strings.HasPrefix(rp, prefix)
}

// crossFrameworkJavaMiddleware returns the cross-framework (WebFlux WebFilter /
// JAX-RS provider filter / Javalin before-after) chain entries bound to a single
// route path. Globals (WebFilter, JAX-RS provider) bind to every route;
// Javalin before/after binds per glob. These compose with the Spring
// filter/interceptor chain in applyJavaMiddlewareCoverage.
func crossFrameworkJavaMiddleware(globalFilters []middlewareEntry, javalin []javalinFilter, routePath string) []middlewareEntry {
	var chain []middlewareEntry
	chain = append(chain, globalFilters...)
	for _, jf := range javalin {
		if javalinGlobMatches(jf.glob, routePath) {
			chain = append(chain, jf.entry)
		}
	}
	return chain
}

// maxInt returns the larger of two ints (local helper to avoid importing a math
// dependency for two call sites).
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
