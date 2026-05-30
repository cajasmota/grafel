// Package kotlin — http4k auth and middleware extractor.
//
// Covers:
//   - lang.kotlin.framework.http4k  Auth/auth_coverage        (missing → partial)
//   - lang.kotlin.framework.http4k  Middleware/middleware_coverage (missing → partial)
//
// http4k uses a purely functional filter-chain model. There are no annotations;
// every middleware is a `Filter` — a function `(HttpHandler) -> HttpHandler`.
// Auth is provided by `ServerFilters.BearerAuth`, `ServerFilters.BasicAuth`,
// `BearerAuthFilter`, `OAuthFilter`, or custom `Filter { req, next -> }` lambdas
// that check the `Authorization` header.
//
// DI for http4k: http4k has no built-in DI container (the library is
// intentionally dependency-injection-agnostic). Typical projects use Koin,
// manual constructor injection, or no DI framework at all. AOP and
// transaction management are equally absent from the http4k core.
// Those cells are not_applicable — handled only in the registry update.
//
// Honest limit: regex-based, file-local. The Filter composition chain is
// purely structural; no dataflow analysis. Cells are partial, not full.
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
	extractor.Register("custom_kotlin_http4k_auth_middleware", &http4kAuthMiddlewareExtractor{})
}

type http4kAuthMiddlewareExtractor struct{}

func (e *http4kAuthMiddlewareExtractor) Language() string {
	return "custom_kotlin_http4k_auth_middleware"
}

var (
	// reHttp4kServerFiltersAuth matches ServerFilters auth helpers:
	// ServerFilters.BearerAuth(...), ServerFilters.BasicAuth(...),
	// ServerFilters.ApiKey(...), ServerFilters.Cors(...)
	reHttp4kServerFiltersAuth = regexp.MustCompile(
		`\bServerFilters\s*\.\s*(BearerAuth|BasicAuth|ApiKey|Oauth|OpenApiAuth)\s*\(`)

	// reHttp4kBearerAuthFilter matches explicit BearerAuthFilter / BasicAuthFilter usage.
	reHttp4kBearerAuthFilter = regexp.MustCompile(
		`\b(BearerAuthFilter|BasicAuthFilter|OAuthFilter|DigestAuthFilter|ApiKeyFilter)\s*\(`)

	// reHttp4kAuthorizationHeader matches direct `Authorization` header checks (custom auth filter).
	reHttp4kAuthorizationHeader = regexp.MustCompile(
		`\brequest\s*\.\s*header\s*\(\s*"Authorization"\s*\)|\bAuthorization\b`)

	// reHttp4kThen matches Filter.then(handler) composition — the fundamental
	// http4k middleware chain pattern.
	reHttp4kThen = regexp.MustCompile(`\b(\w+)\s*\.\s*then\s*\(`)

	// reHttp4kServerFiltersMiddleware matches ServerFilters non-auth helpers:
	// ServerFilters.CatchLensFailure, ServerFilters.RequestTracing, etc.
	reHttp4kServerFiltersMiddleware = regexp.MustCompile(
		`\bServerFilters\s*\.\s*(CatchLensFailure|RequestTracing|GZip|Cors|` +
			`OpenTelemetry|CallId|InitialiseRequestContext|RecordMetrics)\s*\(`)

	// reHttp4kFilterLambda matches `Filter { next -> ... }` or `Filter { req, next -> ... }`
	// custom filter lambda declarations.
	reHttp4kFilterLambda = regexp.MustCompile(
		`\bFilter\s*\{[^{]*?(?:next|handler)\s*->`)

	// reHttp4kRoutes matches http4k routes("path" bind method to handler).
	reHttp4kRoutes = regexp.MustCompile(
		`\broutes\s*\(|\b"/[^"]*"\s+bind\b`)
)

func (e *http4kAuthMiddlewareExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.http4k_auth_middleware.extract",
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

	// Quick bail-out: require some http4k signal.
	hasHttp4k := reHttp4kThen.MatchString(src) ||
		reHttp4kServerFiltersAuth.MatchString(src) ||
		reHttp4kBearerAuthFilter.MatchString(src) ||
		reHttp4kFilterLambda.MatchString(src) ||
		reHttp4kRoutes.MatchString(src)
	if !hasHttp4k {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, subtype, mwType string, line int) {
		key := "SCOPE.Pattern:http4k:" + subtype + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", subtype, file.Path, file.Language, line)
		setProps(&ent,
			"framework", "http4k",
			"middleware_type", mwType,
			"provenance", "INFERRED_FROM_HTTP4K_FILTER",
		)
		entities = append(entities, ent)
	}

	// --- auth_coverage ---

	// ServerFilters.BearerAuth / BasicAuth / ApiKey
	for _, m := range reHttp4kServerFiltersAuth.FindAllStringSubmatchIndex(src, -1) {
		filterName := src[m[2]:m[3]]
		add("ServerFilters."+filterName, "auth_filter", strings.ToLower(filterName), lineOf(src, m[0]))
	}

	// Explicit *AuthFilter types
	for _, m := range reHttp4kBearerAuthFilter.FindAllStringSubmatchIndex(src, -1) {
		filterName := src[m[2]:m[3]]
		add(filterName, "auth_filter", strings.ToLower(filterName), lineOf(src, m[0]))
	}

	// --- middleware_coverage ---

	// ServerFilters non-auth
	for _, m := range reHttp4kServerFiltersMiddleware.FindAllStringSubmatchIndex(src, -1) {
		filterName := src[m[2]:m[3]]
		add("ServerFilters."+filterName, "middleware", strings.ToLower(filterName), lineOf(src, m[0]))
	}

	// Filter { next -> } lambdas
	cnt := 0
	for _, m := range reHttp4kFilterLambda.FindAllStringSubmatchIndex(src, -1) {
		cnt++
		add("filter_lambda#"+strings.Repeat("x", cnt%100), "middleware", "custom_filter", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
