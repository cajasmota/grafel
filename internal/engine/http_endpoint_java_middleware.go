// http_endpoint_java_middleware.go — ordered middleware-chain binding for the
// Spring backend-HTTP synthesizers, child of #3628.
//
// Brings Spring endpoints to parity with the Go (#3777) and JS/TS (#2853)
// middleware passes: resolves a structured, ORDERED middleware chain and stamps
// it on every synthetic http_endpoint_definition this file emitted, using the
// shared cross-stack contract (see http_endpoint_middleware_chain.go).
//
// Resolved scopes (request-traversal order, OUTERMOST-first):
//
//	filter      — Servlet filters: a `FilterRegistrationBean` whose `urlPatterns`
//	              statically match the route path, sequenced by its `@Order` /
//	              `setOrder(...)`. Filters run before interceptors.
//	interceptor — Spring MVC `HandlerInterceptor`s registered via
//	              `registry.addInterceptor(new XInterceptor()).addPathPatterns("/api/**")`.
//	              Bound to the routes whose path matches a path pattern (and not
//	              excluded by `.excludePathPatterns(...)`), sequenced by `@Order`.
//
// Honest-partial: an interceptor/filter whose path pattern cannot be statically
// resolved to a specific known route (e.g. a broad `/**`, or a pattern that
// matches no synthesized route in this file) is NOT bound — no fabricated order.
// `addInterceptor(...)` with no `addPathPatterns` defaults to all routes in
// Spring, but to stay honest we only bind it when the file also synthesizes the
// routes it would wrap (same-file controllers); a config class with no
// same-file controller leaves nothing to bind, which is correct.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// javaMWScope* name the Spring middleware scopes, outermost-first.
const (
	javaMWScopeFilter      = "filter"
	javaMWScopeInterceptor = "interceptor"
)

// javaMWScopeOrder lists the Spring scopes outermost-first.
var javaMWScopeOrder = []string{javaMWScopeFilter, javaMWScopeInterceptor}

// springInterceptorRegRe captures a Spring MVC interceptor registration on an
// InterceptorRegistry. Group 1 = the interceptor expression (the argument to
// addInterceptor, e.g. `new AuthInterceptor()` or `authInterceptor`). The
// trailing chained `.addPathPatterns(...)`/`.excludePathPatterns(...)` are
// recovered separately from the statement span.
var springInterceptorRegRe = regexp.MustCompile(
	`\.addInterceptor\s*\(\s*([^)]*?)\s*\)`,
)

// springAddPathPatternsRe / springExcludePathPatternsRe capture the path-pattern
// lists chained onto an interceptor registration. Group 1 = the raw argument
// list (one or more quoted patterns).
var springAddPathPatternsRe = regexp.MustCompile(`\.addPathPatterns\s*\(([^)]*)\)`)
var springExcludePathPatternsRe = regexp.MustCompile(`\.excludePathPatterns\s*\(([^)]*)\)`)

// springQuotedRe captures a quoted string literal token (path patterns / url
// patterns / class names given as strings).
var springQuotedRe = regexp.MustCompile(`"([^"\n\r]*)"`)

// springFilterRegBeanRe captures a `FilterRegistrationBean` bean method body
// opener so its `setUrlPatterns(...)` / `addUrlPatterns(...)` and the filter it
// wraps can be recovered. Group 1 = the bean method name.
var springFilterRegBeanRe = regexp.MustCompile(
	`FilterRegistrationBean(?:<[^>]*>)?\s+(\w+)\s*=\s*new\s+FilterRegistrationBean`,
)

// springSetFilterRe captures `registration.setFilter(new XFilter())` /
// `.setFilter(xFilter)`. Group 1 = the filter expression.
var springSetFilterRe = regexp.MustCompile(`\.setFilter\s*\(\s*([^)]*?)\s*\)`)

// springUrlPatternsRe captures `setUrlPatterns(Arrays.asList("/api/*"))` /
// `addUrlPatterns("/api/*","/admin/*")`. Group 1 = the raw arg list.
var springUrlPatternsRe = regexp.MustCompile(`\.(?:set|add)UrlPatterns\s*\(([^)]*)\)`)

// springSetOrderRe captures `registration.setOrder(1)`. Group 1 = the order int.
var springSetOrderRe = regexp.MustCompile(`\.setOrder\s*\(\s*(-?\d+)\s*\)`)

// springNewExprNameRe extracts the type name from a `new XInterceptor(...)` or a
// bare identifier reference, so the chain entry's Name is the component symbol.
var springNewExprNameRe = regexp.MustCompile(`(?:new\s+)?([A-Za-z_]\w*)`)

// applyJavaMiddlewareCoverage resolves and stamps the ordered middleware chain
// on every Spring synthetic backend endpoint emitted for this file. Mutates
// Properties in place; never adds or removes entities. `before` is the
// entity-slice length captured before the Java synthesizers ran.
func applyJavaMiddlewareCoverage(content, path string, entities []types.EntityRecord, before int) {
	if len(content) == 0 || before >= len(entities) {
		return
	}

	interceptors := indexSpringInterceptors(content)
	filters := indexSpringFilters(content)
	if len(interceptors) == 0 && len(filters) == 0 {
		return
	}

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		routePath := e.Properties["path"]
		if routePath == "" {
			continue
		}

		var chain []middlewareEntry
		// filter (outermost) — bound when a urlPattern statically matches.
		for _, f := range filters {
			if springPatternsMatchRoute(f.patterns, nil, routePath) {
				chain = append(chain, f.entry)
			}
		}
		// interceptor — bound when an addPathPattern matches and no
		// excludePathPattern matches.
		for _, it := range interceptors {
			if springPatternsMatchRoute(it.patterns, it.excludes, routePath) {
				chain = append(chain, it.entry)
			}
		}
		chain = dedupeMiddlewareEntries(chain)
		stampMiddlewareChainEntries(e.Properties, chain, javaMWScopeOrder)
	}
}

// springBoundMiddleware is a resolved Spring filter/interceptor with the path
// patterns it is bound to and its ordered chain entry.
type springBoundMiddleware struct {
	entry    middlewareEntry
	patterns []string // include patterns; empty ⇒ applies to all routes
	excludes []string // exclude patterns (interceptors)
}

// indexSpringInterceptors resolves every `registry.addInterceptor(...)`
// registration with its `.addPathPatterns(...)` / `.excludePathPatterns(...)`
// and `@Order`-derived sequence. Returns them in registration order; @Order, if
// present on the statement, refines the order index.
func indexSpringInterceptors(content string) []springBoundMiddleware {
	if !strings.Contains(content, "addInterceptor") {
		return nil
	}
	var out []springBoundMiddleware
	for _, m := range springInterceptorRegRe.FindAllStringSubmatchIndex(content, -1) {
		expr := strings.TrimSpace(content[m[2]:m[3]])
		if expr == "" {
			continue
		}
		// The chained-call span runs from this `.addInterceptor(` to the end of
		// the statement (`;` or newline-of-statement).
		stmt := springStatementSpan(content, m[0])
		patterns := springExtractPatterns(stmt, springAddPathPatternsRe)
		excludes := springExtractPatterns(stmt, springExcludePathPatternsRe)
		// Honest-partial: an interceptor with NO addPathPatterns AND patterns
		// that cannot be resolved is skipped — but a no-pattern interceptor in
		// Spring applies to all routes, so we keep it with empty include set
		// (matched against every same-file route below).
		out = append(out, springBoundMiddleware{
			entry: middlewareEntry{
				Name:     springSymbolName(expr),
				Expr:     expr,
				Scope:    javaMWScopeInterceptor,
				AuthKind: middlewareAuthKind(expr),
			},
			patterns: patterns,
			excludes: excludes,
		})
	}
	return out
}

// indexSpringFilters resolves every `FilterRegistrationBean` with its wrapped
// filter, `urlPatterns`, and `setOrder`. Filters with no resolvable urlPatterns
// are skipped (honest-partial — a filter that defaults to `/*` is too broad to
// bind to a specific route without fabricating).
func indexSpringFilters(content string) []springBoundMiddleware {
	if !strings.Contains(content, "FilterRegistrationBean") {
		return nil
	}
	var out []springBoundMiddleware
	for _, m := range springFilterRegBeanRe.FindAllStringSubmatchIndex(content, -1) {
		varName := content[m[2]:m[3]]
		// The registration block spans from this declaration to the bean's
		// `return <varName>` (or the next FilterRegistrationBean decl).
		block := springRegistrationBlock(content, m[1], varName)
		fm := springSetFilterRe.FindStringSubmatch(block)
		if fm == nil {
			continue
		}
		filterExpr := strings.TrimSpace(fm[1])
		patterns := springExtractPatterns(block, springUrlPatternsRe)
		if len(patterns) == 0 {
			// No resolvable urlPatterns → cannot bind to a specific route.
			continue
		}
		out = append(out, springBoundMiddleware{
			entry: middlewareEntry{
				Name:     springSymbolName(filterExpr),
				Expr:     filterExpr,
				Scope:    javaMWScopeFilter,
				AuthKind: middlewareAuthKind(filterExpr),
			},
			patterns: patterns,
		})
	}
	return out
}

// springExtractPatterns pulls quoted string patterns from the first match of re
// in text. Servlet `/*` patterns are normalized to Spring-style `/**` so the
// matcher treats them uniformly.
func springExtractPatterns(text string, re *regexp.Regexp) []string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	var out []string
	for _, q := range springQuotedRe.FindAllStringSubmatch(m[1], -1) {
		p := strings.TrimSpace(q[1])
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// springPatternsMatchRoute reports whether a route path is covered by the
// include patterns and not covered by the exclude patterns. An empty include
// set means "all routes" (Spring's no-addPathPatterns default). A pattern that
// cannot be resolved to a concrete prefix (a bare `/**` or `/*`) does NOT count
// as a static match — honest-partial — UNLESS it is the only signal and the
// include set is explicitly empty (the default-all case).
func springPatternsMatchRoute(includes, excludes []string, routePath string) bool {
	for _, ex := range excludes {
		if springPatternMatches(ex, routePath, true) {
			return false
		}
	}
	if len(includes) == 0 {
		// No explicit include patterns: Spring default is all routes. This is a
		// static, deterministic binding (the interceptor wraps every route in
		// the same file), so it is honest to bind.
		return true
	}
	for _, inc := range includes {
		if springPatternMatches(inc, routePath, false) {
			return true
		}
	}
	return false
}

// springPatternMatches reports whether an Ant-style path pattern matches a
// concrete route path. A pattern with a static prefix before a `*` matches when
// the route shares that prefix (`/api/**` matches `/api/users`). A bare
// universal pattern (`/**`, `/*`) is only honoured for EXCLUDES (where it safely
// un-binds); for INCLUDES it is treated as unresolvable (returns false) so a
// too-broad include never fabricates a binding.
func springPatternMatches(pattern, routePath string, isExclude bool) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	// Universal pattern.
	if pattern == "/**" || pattern == "/*" || pattern == "**" || pattern == "*" {
		return isExclude
	}
	// Static prefix up to the first wildcard.
	star := strings.IndexAny(pattern, "*?")
	if star < 0 {
		// Exact (or trailing-slash-insensitive) match.
		return springPathEqual(pattern, routePath)
	}
	prefix := strings.TrimRight(pattern[:star], "/")
	if prefix == "" {
		return isExclude
	}
	rp := routePath
	return rp == prefix || strings.HasPrefix(rp, prefix+"/") || strings.HasPrefix(rp, prefix)
}

// springPathEqual compares two paths ignoring a trailing slash.
func springPathEqual(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// springSymbolName extracts the component type/identifier name from a `new
// XInterceptor()` / `xInterceptor` / `beanRef()` expression.
func springSymbolName(expr string) string {
	expr = strings.TrimSpace(expr)
	if m := springNewExprNameRe.FindStringSubmatch(expr); m != nil {
		return m[1]
	}
	return expr
}

// springStatementSpan returns the text from `from` to the end of the current
// Java statement (the next `;` at paren/brace depth 0, or the rest of content).
func springStatementSpan(content string, from int) string {
	depth := 0
	for i := from; i < len(content); i++ {
		switch content[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ';':
			if depth <= 0 {
				return content[from : i+1]
			}
		}
	}
	return content[from:]
}

// springRegistrationBlock returns the span from `from` up to and including the
// statement that returns `varName` (the FilterRegistrationBean bean method's
// return), or the rest of content when no such return is found.
func springRegistrationBlock(content string, from int, varName string) string {
	retIdx := strings.Index(content[from:], "return "+varName)
	if retIdx < 0 {
		// Fall back to the enclosing brace block end.
		return content[from:springStatementEnd(content, from)]
	}
	abs := from + retIdx
	end := springStatementEnd(content, abs)
	return content[from:end]
}

// springStatementEnd returns the offset just past the next top-level `;` from
// `from`, or len(content).
func springStatementEnd(content string, from int) int {
	for i := from; i < len(content); i++ {
		if content[i] == ';' {
			return i + 1
		}
	}
	return len(content)
}
