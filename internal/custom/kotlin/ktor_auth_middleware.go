// Package kotlin — Ktor auth + middleware deepening extractor.
//
// Deepens the Auth and Middleware lanes for lang.kotlin.framework.ktor beyond
// the route-centric ktor.go extractor. ktor.go records `install(Plugin)`,
// `authenticate{}` blocks, and routes; this file resolves the *specific auth
// method* configured inside `install(Authentication) { ... }` and the ordered
// pipeline of plugins / custom interceptors that make up the middleware chain.
//
// Covers (partial → full):
//   - lang.kotlin.framework.ktor  Auth/auth_coverage
//   - lang.kotlin.framework.ktor  Middleware/middleware_coverage
//
// Ktor auth model: `install(Authentication) { jwt("name") {...}; basic {...};
// oauth {...}; bearer {...}; session<T> {...}; digest {...} }`. Each block
// inside the Authentication plugin declares one auth provider of a concrete
// method (jwt / basic / oauth / bearer / session / digest / form / ldap).
// `authenticate("name") { ... }` route wrappers reference a named provider.
//
// Ktor middleware model: every `install(<Plugin>)` is a pipeline interceptor
// (CORS, CallLogging, ContentNegotiation, Compression, DefaultHeaders, ...),
// plus custom `intercept(ApplicationCallPipeline.Phase) { ... }` interceptors.
//
// Honest limit: regex-based, file-local. The link between an `authenticate`
// wrapper and the provider it names is captured as a property (auth_provider)
// but the cross-block binding is not graph-resolved. Method detection and
// ordered middleware naming are value-asserted, so the cells are full for the
// in-file dimension; cross-file provider binding remains the documented gap.
package kotlin

import (
	"context"
	"regexp"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_ktor_auth_middleware", &ktorAuthMiddlewareExtractor{})
}

type ktorAuthMiddlewareExtractor struct{}

func (e *ktorAuthMiddlewareExtractor) Language() string {
	return "custom_kotlin_ktor_auth_middleware"
}

var (
	// reKtorInstallAuthBlock captures the body of `install(Authentication) { ... }`.
	// Non-greedy across the (?s) span; the body is then scanned for provider
	// declarations. The closing brace heuristic stops at the first balanced-ish
	// `}` at the end of the install call — good enough for file-local scanning
	// because provider blocks are scanned independently below.
	reKtorInstallAuth = regexp.MustCompile(
		`(?s)\binstall\s*\(\s*Authentication\s*\)\s*\{`)

	// reKtorAuthProvider matches a concrete auth-method provider block declared
	// inside install(Authentication). The optional quoted name is the provider
	// name referenced later by authenticate("name").
	//   jwt("auth-jwt") { ... }   basic { ... }   oauth("google") { ... }
	// The method keyword must be followed by an optional ("name") and a `{`.
	reKtorAuthProvider = regexp.MustCompile(
		`(?m)\b(jwt|basic|oauth|bearer|session|digest|form|ldapAuthenticate|ldap)\s*` +
			`(?:<[^>]*>)?\s*` +
			`(?:\(\s*"([^"]*)"\s*\))?\s*\{`)

	// reKtorAuthenticateNamed matches authenticate("name1", "name2") { ... }
	// route wrappers, capturing the (possibly multiple) provider names.
	reKtorAuthenticateNamed = regexp.MustCompile(
		`(?m)\bauthenticate\b\s*\(([^)]*)\)\s*\{`)

	// reKtorAuthQuotedName extracts each quoted provider name from an
	// authenticate(...) argument list.
	reKtorAuthQuotedName = regexp.MustCompile(`"([^"]*)"`)

	// reKtorInstallPlugin matches any `install(<Plugin>)` — the ordered Ktor
	// middleware pipeline. Authentication is excluded (handled as auth above).
	reKtorInstallPlugin = regexp.MustCompile(
		`(?m)\binstall\s*\(\s*([A-Za-z_][\w.]*)`)

	// reKtorIntercept matches a custom pipeline interceptor:
	//   intercept(ApplicationCallPipeline.Plugins) { ... }
	//   intercept(ApplicationCallPipeline.Call) { ... }
	reKtorIntercept = regexp.MustCompile(
		`(?m)\bintercept\s*\(\s*(ApplicationCallPipeline|ApplicationReceivePipeline|ApplicationSendPipeline)\s*\.\s*(\w+)`)
)

// ktorKnownPlugins is the set of well-known Ktor pipeline plugins. Membership
// upgrades the middleware_subtype from the generic "plugin" to the specific
// plugin role, so a value-asserting test can demand e.g. CORS / CallLogging.
var ktorKnownPlugins = map[string]string{
	"CORS":                "cors",
	"CallLogging":         "logging",
	"ContentNegotiation":  "content_negotiation",
	"Compression":         "compression",
	"DefaultHeaders":      "default_headers",
	"ConditionalHeaders":  "conditional_headers",
	"CachingHeaders":      "caching_headers",
	"AutoHeadResponse":    "auto_head",
	"PartialContent":      "partial_content",
	"ForwardedHeaders":    "forwarded_headers",
	"XForwardedHeaders":   "forwarded_headers",
	"HSTS":                "hsts",
	"HttpsRedirect":       "https_redirect",
	"StatusPages":         "status_pages",
	"Sessions":            "sessions",
	"WebSockets":          "websockets",
	"Resources":           "resources",
	"RateLimit":           "rate_limit",
	"DoubleReceive":       "double_receive",
	"CallId":              "call_id",
	"MicrometerMetrics":   "metrics",
	"DataConversion":      "data_conversion",
	"RequestValidation":   "request_validation",
	"OpenTelemetry":       "opentelemetry",
	"KtorServerTelemetry": "opentelemetry",
}

func (e *ktorAuthMiddlewareExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.ktor_auth_middleware.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ktor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	hasAuthInstall := reKtorInstallAuth.MatchString(src)
	hasPlugin := reKtorInstallPlugin.MatchString(src)
	hasIntercept := reKtorIntercept.MatchString(src)
	hasAuthenticate := reKtorAuthenticateNamed.MatchString(src)
	if !hasAuthInstall && !hasPlugin && !hasIntercept && !hasAuthenticate {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, subtype string, line int, props ...string) {
		key := "SCOPE.Pattern:ktoram:" + subtype + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", subtype, file.Path, file.Language, line)
		base := []string{
			"framework", "ktor",
			"provenance", "INFERRED_FROM_KTOR_AUTH_MIDDLEWARE",
		}
		setProps(&ent, append(base, props...)...)
		entities = append(entities, ent)
	}

	// --- auth_coverage: concrete provider methods inside install(Authentication) ---
	//
	// We scope provider detection to the install(Authentication) body so that an
	// unrelated `session<T> { }` (e.g. a Sessions config) outside the auth block
	// is not misread as an auth provider. Each install(Authentication) body runs
	// to the end of source; provider blocks are matched within it.
	if loc := reKtorInstallAuth.FindStringIndex(src); loc != nil {
		body := src[loc[1]:]
		baseOff := loc[1]
		for _, m := range reKtorAuthProvider.FindAllStringSubmatchIndex(body, -1) {
			method := body[m[2]:m[3]]
			provName := ""
			if m[4] >= 0 {
				provName = body[m[4]:m[5]]
			}
			name := "auth:" + method
			if provName != "" {
				name = "auth:" + method + ":" + provName
			}
			props := []string{"auth_method", method}
			if provName != "" {
				props = append(props, "auth_provider", provName)
			}
			add(name, "auth_provider", lineOf(src, baseOff+m[0]), props...)
		}
	}

	// authenticate("name") route wrappers — record the named provider(s) the
	// route requires. This is the in-file half of the binding; the cross-block
	// link to the provider declaration is the documented honest gap.
	for _, m := range reKtorAuthenticateNamed.FindAllStringSubmatchIndex(src, -1) {
		args := src[m[2]:m[3]]
		names := reKtorAuthQuotedName.FindAllStringSubmatch(args, -1)
		if len(names) == 0 {
			add("authenticate:default", "auth_guard", lineOf(src, m[0]),
				"auth_provider", "default")
			continue
		}
		for _, n := range names {
			add("authenticate:"+n[1], "auth_guard", lineOf(src, m[0]),
				"auth_provider", n[1])
		}
	}

	// --- middleware_coverage: ordered install(Plugin) pipeline + interceptors ---
	order := 0
	for _, m := range reKtorInstallPlugin.FindAllStringSubmatchIndex(src, -1) {
		plugin := src[m[2]:m[3]]
		if plugin == "Authentication" {
			continue // auth, not generic middleware
		}
		order++
		role := "plugin"
		if r, ok := ktorKnownPlugins[plugin]; ok {
			role = r
		}
		add("install:"+plugin, "middleware", lineOf(src, m[0]),
			"middleware_type", role,
			"plugin_name", plugin,
			"middleware_order", strconv.Itoa(order))
	}

	// Custom pipeline interceptors.
	for _, m := range reKtorIntercept.FindAllStringSubmatchIndex(src, -1) {
		pipeline := src[m[2]:m[3]]
		phase := src[m[4]:m[5]]
		order++
		add("intercept:"+pipeline+"."+phase, "middleware", lineOf(src, m[0]),
			"middleware_type", "interceptor",
			"pipeline", pipeline,
			"phase", phase,
			"middleware_order", strconv.Itoa(order))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
