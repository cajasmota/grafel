package rust

// auth.go — framework-agnostic auth + middleware scanner for Rust HTTP
// services (issue #3269). Detects two families:
//
//   - auth_coverage: JWT token validation (jsonwebtoken crate, decode/encode),
//     framework-specific auth middleware (actix-web-httpauth, axum's
//     RequireAuth / from_fn auth extractors, rocket request guards that mention
//     "auth"/"jwt"/"bearer", poem Middleware that matches auth patterns), and
//     generic Bearer/JWT string patterns.
//
//   - middleware_coverage: framework-specific middleware registration surfaces
//     (actix .wrap(), axum/tower .layer(), rocket Fairing attach, poem Endpoint
//     middleware, salvo Handler trait, tide Middleware trait, warp Filter::and,
//     gotham add_middleware, hyper tower Service wrapper) where the actix/axum
//     surfaces complement rather than duplicate actix_web.go / axum.go —
//     those files already emit SCOPE.Pattern for .wrap()/.layer() matches;
//     this file adds the auth-classified subset and the other 8 frameworks.
//
// Honesty:
//
//	partial — heuristic regex/substring match on source text. Does NOT perform
//	import-resolution or data-flow analysis to confirm a value actually enforces
//	auth, and does not bind to a specific route. Fixtures prove detection surface.

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
	extractor.Register("custom_rust_auth", &rustAuthExtractor{})
}

type rustAuthExtractor struct{}

func (e *rustAuthExtractor) Language() string { return "custom_rust_auth" }

// ---------------------------------------------------------------------------
// Auth signal catalog
// ---------------------------------------------------------------------------

type rustAuthSignal struct {
	re        *regexp.Regexp
	atype     string // auth_subtype: jwt | bearer | oauth | session | api_key | middleware_auth
	nameGroup int
}

var rustAuthSignals = []rustAuthSignal{
	// jsonwebtoken crate: import or direct usage
	{regexp.MustCompile(`\bjsonwebtoken\b`), "jwt", 0},
	// jsonwebtoken direct type usage (when imported without 'use jsonwebtoken')
	{regexp.MustCompile(`\b(?:DecodingKey|EncodingKey|Validation|TokenData)::(?:\w+)\s*\(`), "jwt", 0},
	// actix-web-httpauth
	{regexp.MustCompile(`\bactix_web_httpauth\b`), "middleware_auth", 0},
	{regexp.MustCompile(`\bHttpAuthentication::bearer\s*\(`), "bearer", 0},
	{regexp.MustCompile(`\bHttpAuthentication::basic\s*\(`), "basic_auth", 0},
	// axum auth patterns: RequireAuth, from_fn with auth hint
	{regexp.MustCompile(`\bRequireAuth(?:::[A-Za-z_]\w*)?\b`), "middleware_auth", 0},
	// axum-login / axum_login crate
	{regexp.MustCompile(`\baxum_login::`), "middleware_auth", 0},
	// generic JWT Bearer token patterns in source
	{regexp.MustCompile(`(?i)\bAuthorization\s*:\s*Bearer\b`), "bearer", 0},
	{regexp.MustCompile(`(?i)\bjwt_secret\b|\bjwt_token\b|\bbearer_token\b`), "jwt", 0},
	// rocket request guards that look like auth
	{regexp.MustCompile(`impl\s+(?:<[^>]*>\s+)?(?:FromRequest|RequestGuard)(?:<[^>]*>)?\s+for\s+(\w*(?:[Aa]uth|[Jj]wt|[Bb]earer|[Uu]ser|[Cc]laims)\w*)`), "middleware_auth", 1},
	// poem middleware auth patterns
	{regexp.MustCompile(`#\[poem::handler\][\s\S]{0,200}(?:[Aa]uthorization|[Bb]earer|[Jj]wt)`), "middleware_auth", 0},
	// oauth2 crate
	{regexp.MustCompile(`\boauth2::`), "oauth", 0},
	// tower-http auth (used by axum/tower/hyper)
	{regexp.MustCompile(`\btower_http::(?:auth|validate_request)::`), "middleware_auth", 0},
	// session middleware (actix-session, tower-sessions)
	{regexp.MustCompile(`\b(?:actix_session|tower_sessions)::`), "session", 0},
	// api key patterns
	{regexp.MustCompile(`(?i)\bapi_key\b|\bx-api-key\b`), "api_key", 0},
}

// ---------------------------------------------------------------------------
// Middleware signal catalog
// ---------------------------------------------------------------------------

type rustMwSignal struct {
	re        *regexp.Regexp
	framework string // specific framework or "multi" for framework-agnostic
	subtype   string
}

var rustMwSignals = []rustMwSignal{
	// actix: .wrap(Middleware) — complements actix_web.go which captures the type
	// We emit the auth-classified subset here so auth_coverage fires
	{regexp.MustCompile(`\.wrap\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`), "actix", "wrap"},
	// axum / tower: .layer(Middleware) — complements axum.go
	{regexp.MustCompile(`\.layer\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`), "axum_tower", "layer"},
	// rocket: .attach(Fairing) fairing attachment
	{regexp.MustCompile(`\.attach\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`), "rocket", "attach"},
	// poem: poem::EndpointExt::with / .with(middleware)
	{regexp.MustCompile(`\.with\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`), "poem", "with"},
	// salvo: impl Handler for T pattern / #[handler] macro
	{regexp.MustCompile(`(?:impl\s+(?:<[^>]*>\s+)?Handler\s+for\s+(\w+)|#\[handler\])`), "salvo", "handler"},
	// tide: server.middleware(mw)
	{regexp.MustCompile(`\.middleware\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`), "tide", "middleware"},
	// warp: Filter::and / filter.with(warp::log)
	{regexp.MustCompile(`\bwarp::(?:log|cors|trace)\s*\(`), "warp", "filter"},
	// gotham: add_middleware call
	{regexp.MustCompile(`\badd_middleware\s*\(`), "gotham", "add_middleware"},
	// hyper: tower Service::new / ServiceBuilder::new().layer
	{regexp.MustCompile(`\bServiceBuilder::new\s*\(\s*\)\s*\.layer\s*\(`), "hyper", "service_builder"},
	// generic tower ServiceBuilder (used across tower/hyper/axum)
	{regexp.MustCompile(`\btower::ServiceBuilder\b|ServiceBuilder::new\b`), "tower", "service_builder"},
}

// authKeywords classify a middleware expression as auth-related.
var authKeywords = []string{
	"auth", "Auth", "jwt", "JWT", "bearer", "Bearer", "oauth", "OAuth",
	"session", "Session", "apikey", "api_key", "ApiKey", "require", "Require",
	"identity", "Identity", "claims", "Claims", "token", "Token",
}

func isAuthMiddleware(expr string) bool {
	for _, kw := range authKeywords {
		if strings.Contains(expr, kw) {
			return true
		}
	}
	return false
}

func (e *rustAuthExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_auth_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	framework := detectRustObsFramework(src) // reuse framework detection from observability.go

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
	for _, sig := range rustAuthSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			detail := ""
			if sig.nameGroup > 0 && len(m) >= (sig.nameGroup+1)*2 {
				s := m[sig.nameGroup*2]
				en := m[sig.nameGroup*2+1]
				if s >= 0 && en >= 0 {
					detail = src[s:en]
				}
			}
			if detail == "" {
				detail = src[m[0]:m[1]]
			}
			detail = strings.TrimSpace(detail)

			name := "auth:" + sig.atype + ":" + detail
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", framework,
				"provenance", "INFERRED_FROM_RUST_AUTH",
				"pattern_kind", "auth",
				"auth_subtype", sig.atype,
				"auth_method", authPolicyMethod(sig.atype+" "+detail),
			)
			add(ent)
		}
	}

	// --- Middleware coverage ---
	for _, sig := range rustMwSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			mwExpr := ""
			if len(m) >= 4 && m[2] >= 0 {
				mwExpr = src[m[2]:m[3]]
			}
			if mwExpr == "" {
				mwExpr = src[m[0]:m[1]]
			}
			mwExpr = strings.TrimSpace(mwExpr)

			name := "middleware:" + sig.subtype + ":" + mwExpr
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", framework,
				"provenance", "INFERRED_FROM_RUST_MIDDLEWARE",
				"pattern_kind", "middleware",
				"middleware_kind", sig.subtype,
			)
			add(ent)

			// Also emit an auth entity if the middleware expression looks like auth
			if isAuthMiddleware(mwExpr) {
				authName := "auth:middleware_auth:" + mwExpr
				authEnt := makeEntity(authName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&authEnt,
					"framework", framework,
					"provenance", "INFERRED_FROM_RUST_AUTH",
					"pattern_kind", "auth",
					"auth_subtype", "middleware_auth",
				)
				add(authEnt)
			}
		}
	}

	// --- Deep auth policy + middleware/guard/layer-chain extraction ---
	// Recovers specific guard/middleware names and the enforced auth policy
	// (auth_method + auth_required) and enumerates tower layer chains in order.
	emitAuthPolicy(src, file.Path, file.Language, framework, add)

	span.SetAttributes(
		attribute.String("framework", framework),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}
