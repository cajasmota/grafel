package cpp

// auth_middleware.go — auth + middleware scanner for C++ HTTP services.
//
// Covered surfaces:
//
//   - auth_coverage:
//   - Drogon: HttpFilter subclass declarations, JWT/bearer/token include or
//     usage patterns (jwt-cpp, libjwt, drogon JwtFilter idioms).
//   - Crow: CROW_MIDDLEWARES macro, CrowMiddleware struct pattern, JWT header
//     extraction.
//   - Pistache: custom Handler subclass matching auth keywords, JWT includes.
//   - cpprestsdk: http_request::headers() auth extraction, bearer token check.
//   - Generic: #include <jwt-cpp/jwt.h>, libjwt jwt_decode, OpenSSL HMAC
//     usage in an auth context.
//
//   - middleware_coverage:
//   - Drogon: HttpFilter::doFilter() override, app().registerFilter(),
//     DrogonFilter macro registration.
//   - Crow: CROW_MIDDLEWARES(app, Mw1, Mw2), crow::App<Mw...> template,
//     blueprint.add_blueprint().
//   - Pistache: custom Pistache::Http::Handler chain (struct extending Handler).
//   - cpprestsdk: http_listener pipeline (handler chaining via .support()).
//
// Honesty: partial — heuristic regex/substring match on source text. Does NOT
// perform import-resolution or data-flow analysis to confirm auth enforcement,
// and does not bind middleware to a specific route. Fixtures prove the
// detection surface.

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
	extractor.Register("custom_cpp_auth_middleware", &cppAuthMwExtractor{})
}

type cppAuthMwExtractor struct{}

func (e *cppAuthMwExtractor) Language() string { return "custom_cpp_auth_middleware" }

// ---------------------------------------------------------------------------
// Framework detection
// ---------------------------------------------------------------------------

var cppFrameworkMarkers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"drogon", regexp.MustCompile(`(?:#include\s*<drogon/|drogon::|\bDrogon\b)`)},
	{"crow", regexp.MustCompile(`(?:#include\s*<crow|crow::|\bCROW_ROUTE\b|\bCROW_MIDDLEWARES\b)`)},
	{"pistache", regexp.MustCompile(`(?:#include\s*<pistache/|Pistache::|Routes::)`)},
	{"cpprestsdk", regexp.MustCompile(`(?:#include\s*<cpprest/|web::http::|http_listener)`)},
}

func detectCPPFramework(src string) string {
	for _, m := range cppFrameworkMarkers {
		if m.re.MatchString(src) {
			return m.name
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Auth signal catalog
// ---------------------------------------------------------------------------

type cppAuthSignal struct {
	re    *regexp.Regexp
	atype string // jwt | bearer | api_key | middleware_auth | session
}

var cppAuthSignals = []cppAuthSignal{
	// jwt-cpp: #include <jwt-cpp/jwt.h> or jwt::decode / jwt::create
	{regexp.MustCompile(`#include\s*<jwt-cpp/`), "jwt"},
	{regexp.MustCompile(`\bjwt::(decode|create|verify)\s*\(`), "jwt"},
	// libjwt: jwt_decode / jwt_add_grant
	{regexp.MustCompile(`\bjwt_decode\s*\(`), "jwt"},
	{regexp.MustCompile(`\bjwt_add_grant\s*\(`), "jwt"},
	// Drogon HttpFilter — class derived from HttpFilter
	{regexp.MustCompile(`(?:class|struct)\s+\w+\s*:\s*(?:public\s+)?(?:drogon::)?HttpFilter\b`), "middleware_auth"},
	// Drogon JwtFilter or any class with Auth/Jwt in name extending HttpFilter
	{regexp.MustCompile(`class\s+\w*(?:[Aa]uth|[Jj]wt|[Bb]earer|[Tt]oken)\w*\s*:\s*(?:public\s+)?(?:drogon::)?HttpFilter\b`), "middleware_auth"},
	// Crow middleware struct with auth keywords
	{regexp.MustCompile(`struct\s+\w*(?:[Aa]uth|[Jj]wt|[Bb]earer|[Tt]oken)\w*\s*\{[^}]{0,200}before_handle`), "middleware_auth"},
	// CROW_MIDDLEWARES macro (registers middleware into app)
	{regexp.MustCompile(`\bCROW_MIDDLEWARES\s*\(`), "middleware_auth"},
	// Generic Authorization: Bearer header extraction
	{regexp.MustCompile(`(?i)"Authorization"\s*.*(?:Bearer|bearer)`), "bearer"},
	// OpenSSL HMAC in an auth context (JWT signature verification)
	{regexp.MustCompile(`\bHMAC(?:_CTX)?_(?:new|init|update|final)\s*\(`), "jwt"},
	// cpprestsdk: extract Authorization header
	{regexp.MustCompile(`request\.headers\(\)\.(?:find|has_key)\s*\(\s*"Authorization"`), "bearer"},
	// Pistache: handler class with auth/jwt keywords
	{regexp.MustCompile(`struct\s+\w*(?:[Aa]uth|[Jj]wt|[Bb]earer)\w*\s*:\s*(?:public\s+)?(?:Pistache::Http::)?Handler\b`), "middleware_auth"},
	// api_key patterns
	{regexp.MustCompile(`(?i)"X-Api-Key"|"x-api-key"|\bapi_key\b`), "api_key"},
	// session / cookie-based auth
	{regexp.MustCompile(`(?i)\bsession(?:_id|_token|_key|Manager|Cookie)?\b`), "session"},
	// oatpp JWT filter / interceptor
	{regexp.MustCompile(`(?:class|struct)\s+\w*(?:[Aa]uth|[Jj]wt)\w*\s*:\s*(?:public\s+)?oatpp::`), "middleware_auth"},
}

// ---------------------------------------------------------------------------
// Middleware signal catalog
// ---------------------------------------------------------------------------

type cppMwSignal struct {
	re        *regexp.Regexp
	framework string
	subtype   string
}

var cppMwSignals = []cppMwSignal{
	// Drogon: void doFilter(const HttpRequestPtr&, ...) override
	{regexp.MustCompile(`void\s+doFilter\s*\(\s*const\s+(?:drogon::)?HttpRequestPtr`), "drogon", "doFilter"},
	// Drogon: app().registerFilter<FilterClass>()
	{regexp.MustCompile(`\bapp\s*\(\s*\)\s*\.registerFilter\s*<`), "drogon", "registerFilter"},
	// Drogon: DrogonFilter macro
	{regexp.MustCompile(`\bDROGON_FILTER\s*\(`), "drogon", "DrogonFilter"},
	// Crow: CROW_MIDDLEWARES(app, Mw1, Mw2, ...)
	{regexp.MustCompile(`\bCROW_MIDDLEWARES\s*\(\s*\w+\s*(?:,\s*\w+)+`), "crow", "CROW_MIDDLEWARES"},
	// Crow: crow::App<Mw1, Mw2> (template parameter = middleware)
	{regexp.MustCompile(`crow::App\s*<[^>]+>`), "crow", "crow_app_template"},
	// Crow: before_handle / after_handle methods (middleware struct interface)
	{regexp.MustCompile(`void\s+before_handle\s*\(.*crow::request`), "crow", "before_handle"},
	{regexp.MustCompile(`void\s+after_handle\s*\(.*crow::request`), "crow", "after_handle"},
	// Pistache: class/struct extending Pistache::Http::Handler (chain pattern)
	{regexp.MustCompile(`(?:class|struct)\s+\w+\s*:\s*(?:public\s+)?(?:Pistache::)?(?:Http::)?Handler\b`), "pistache", "Handler"},
	// cpprestsdk: chained .support() pipeline (already captured in routes; also middleware)
	{regexp.MustCompile(`\blistener\s*\.\s*support\s*\(`), "cpprestsdk", "listener_support"},
	// oatpp: AuthorizationHandler / Interceptor
	{regexp.MustCompile(`(?:class|struct)\s+\w+\s*:\s*(?:public\s+)?oatpp::web::server::interceptor::`), "oatpp", "interceptor"},
	// poco: addHandler / addRequestHandler
	{regexp.MustCompile(`\baddRequestHandler\s*\(`), "poco", "addRequestHandler"},
	// restbed: service.publish with content_filter_handler
	{regexp.MustCompile(`resource->set_(?:failed_)?filter_handler\s*\(`), "restbed", "filter_handler"},
}

// authKeywords classify middleware as auth-related.
var cppAuthKeywords = []string{
	"auth", "Auth", "jwt", "JWT", "bearer", "Bearer", "oauth", "OAuth",
	"session", "Session", "api_key", "ApiKey", "require", "Require",
	"identity", "Identity", "claims", "Claims", "token", "Token",
}

func isCPPAuthMiddleware(expr string) bool {
	for _, kw := range cppAuthKeywords {
		if strings.Contains(expr, kw) {
			return true
		}
	}
	return false
}

func (e *cppAuthMwExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_auth_mw_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" {
		return nil, nil
	}

	src := string(file.Content)
	framework := detectCPPFramework(src)

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

	// --- Auth coverage ---
	for _, sig := range cppAuthSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			detail := strings.TrimSpace(src[m[0]:m[1]])
			// Truncate long matches to keep entity names manageable.
			if len(detail) > 80 {
				detail = detail[:80]
			}
			name := "auth:" + sig.atype + ":" + detail
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", framework,
				"provenance", "INFERRED_FROM_CPP_AUTH",
				"pattern_kind", "auth",
				"auth_subtype", sig.atype,
			)
			add(ent)
		}
	}

	// --- Middleware coverage ---
	for _, sig := range cppMwSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			mwExpr := strings.TrimSpace(src[m[0]:m[1]])
			if len(mwExpr) > 80 {
				mwExpr = mwExpr[:80]
			}
			name := "middleware:" + sig.subtype + ":" + mwExpr
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			fw := sig.framework
			if fw == "" {
				fw = framework
			}
			setProps(&ent,
				"framework", fw,
				"provenance", "INFERRED_FROM_CPP_MIDDLEWARE",
				"pattern_kind", "middleware",
				"middleware_kind", sig.subtype,
			)
			add(ent)

			// Cross-emit auth entity when middleware expr looks auth-related.
			if isCPPAuthMiddleware(mwExpr) {
				authName := "auth:middleware_auth:" + mwExpr
				authEnt := makeEntity(authName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&authEnt,
					"framework", fw,
					"provenance", "INFERRED_FROM_CPP_AUTH",
					"pattern_kind", "auth",
					"auth_subtype", "middleware_auth",
				)
				add(authEnt)
			}
		}
	}

	span.SetAttributes(
		attribute.String("framework", framework),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}
