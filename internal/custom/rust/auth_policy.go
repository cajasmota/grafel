package rust

// auth_policy.go — deep auth + middleware extraction for Rust HTTP services
// (issue #3414, epic #3409). Where auth.go performs a broad heuristic signal
// scan, this file recovers the *specific* middleware/guard names and the auth
// *policy* they enforce — auth_method (bearer|jwt|basic|apikey|oauth|session)
// and auth_required (bool) — and enumerates tower ServiceBuilder layer chains
// in source order. This is the value that lifts auth_coverage /
// middleware_coverage from heuristic-partial to value-asserting-full for the
// flagship frameworks (axum, actix-web, rocket).
//
// Honesty boundary: these matchers bind a guard/middleware to its *declared*
// name and the auth method named at the call/impl site. Cross-file resolution
// of a from_fn handler or actix validator to its definition is NOT performed;
// when a guard is bound to a validator symbol we record validator_name but do
// not chase it across files. That residual is documented per-framework in the
// coverage registry notes.

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// auth method classification
// ---------------------------------------------------------------------------

// authPolicyMethod maps an auth expression / type name to a canonical auth
// method token. Order matters: more specific tokens are tested first.
func authPolicyMethod(expr string) string {
	l := strings.ToLower(expr)
	switch {
	case strings.Contains(l, "bearer"):
		return "bearer"
	case strings.Contains(l, "basic"):
		return "basic"
	case strings.Contains(l, "jwt") || strings.Contains(l, "jsonwebtoken") ||
		strings.Contains(l, "decodingkey") || strings.Contains(l, "encodingkey") ||
		strings.Contains(l, "claims"):
		return "jwt"
	case strings.Contains(l, "apikey") || strings.Contains(l, "api_key") ||
		strings.Contains(l, "x-api-key"):
		return "apikey"
	case strings.Contains(l, "oauth"):
		return "oauth"
	case strings.Contains(l, "session") || strings.Contains(l, "identity"):
		return "session"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// axum / tower deep matchers
// ---------------------------------------------------------------------------

var (
	// middleware::from_fn(auth_fn) and from_fn_with_state(state, auth_fn) —
	// captures the named middleware fn so it can be bound as a guard.
	reAxumFromFn = regexp.MustCompile(
		`\bfrom_fn(?:_with_state)?\s*\(\s*(?:[A-Za-z_]\w*\s*,\s*)?([A-Za-z_]\w*)\s*\)`,
	)
	// .route_layer(<expr>) — per-route layer (auth typically applied here).
	reAxumRouteLayer = regexp.MustCompile(
		`\.route_layer\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
	// tower_http ValidateRequestHeaderLayer::bearer(token) / ::basic(..) /
	// ::custom(..) — a concrete header-validation auth layer.
	reValidateRequestHeader = regexp.MustCompile(
		`\bValidateRequestHeaderLayer::([A-Za-z_]\w*)\s*\(`,
	)
	// tower_http RequireAuthorizationLayer::bearer(..) / ::basic(..).
	reRequireAuthLayer = regexp.MustCompile(
		`\bRequireAuthorizationLayer::([A-Za-z_]\w*)\s*\(`,
	)
	// Custom extractor guard: `impl FromRequestParts for AuthUser` /
	// `impl<S> FromRequestParts<S> for CurrentUser` / async-trait FromRequest.
	reFromRequestPartsImpl = regexp.MustCompile(
		`impl\s*(?:<[^>]*>\s*)?FromRequestParts(?:<[^>]*>)?\s+for\s+([A-Za-z_]\w*)`,
	)
	reFromRequestImpl = regexp.MustCompile(
		`impl\s*(?:<[^>]*>\s*)?FromRequest(?:<[^>]*>)?\s+for\s+([A-Za-z_]\w*)`,
	)
	// tower ServiceBuilder layer chain: a ServiceBuilder::new() followed by the
	// run of method calls up to the statement terminator. We capture the chain
	// body then enumerate the .layer(...) calls within it in source order.
	// [^;]* tolerates the nested parens of each layer constructor (e.g.
	// .layer(TraceLayer::new_for_http())) which a per-.layer lazy match cannot.
	reServiceBuilderChain = regexp.MustCompile(
		`ServiceBuilder::new\s*\(\s*\)([^;]*)`,
	)
	// One .layer(Type...) inside a ServiceBuilder chain — captures the leading
	// type path of the layer argument.
	reChainLayer = regexp.MustCompile(
		`\.layer\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
)

// ---------------------------------------------------------------------------
// actix-web deep matchers
// ---------------------------------------------------------------------------

var (
	// HttpAuthentication::bearer(validator) / ::basic(validator) — bind the
	// validator symbol and the method.
	reActixHttpAuth = regexp.MustCompile(
		`\bHttpAuthentication::([A-Za-z_]\w*)\s*\(\s*([A-Za-z_]\w*)`,
	)
	// Custom actix middleware: `impl<S, B> Transform<S, ServiceRequest> for X`.
	reActixTransform = regexp.MustCompile(
		`impl\s*(?:<[^>]*>\s*)?Transform\s*<[^>]*>\s+for\s+([A-Za-z_]\w*)`,
	)
)

// ---------------------------------------------------------------------------
// rocket deep matchers
// ---------------------------------------------------------------------------

// Rocket fairing (reRocketFairing) regex is already declared in rocket.go and
// reused here. rocket.go's reRocketGuard requires whitespace after `impl`; we
// use a no-space-tolerant variant so `impl<'r> FromRequest<'r> for X` matches.
var reRocketGuardAuth = regexp.MustCompile(
	`impl\s*(?:<[^>]*>\s*)?FromRequest(?:<[^>]*>)?\s+for\s+([A-Za-z_]\w*)`,
)

// stripCtor trims a trailing associated-constructor call segment from a
// captured layer/middleware type path so the recorded name is the type, not the
// constructor. Rust convention: type segments are UpperCamel and constructor
// methods (new, new_for_http, default, bearer, basic, builder) are snake_case
// starting lowercase. We therefore strip the final `::segment` iff that segment
// begins with a lowercase letter, leaving multi-segment type paths
// (tower_http::trace::TraceLayer) intact since their last segment is uppercase.
func stripCtor(name string) string {
	idx := strings.LastIndex(name, "::")
	if idx < 0 {
		return name
	}
	last := name[idx+2:]
	if last == "" {
		return name
	}
	c := last[0]
	if c >= 'a' && c <= 'z' {
		return name[:idx]
	}
	return name
}

// emitAuthPolicy walks the deep matchers and appends the enriched
// guard/middleware/layer-chain entities. add() is the dedup-aware appender from
// rustAuthExtractor.Extract; it shares the same `seen` set so policy entities
// never collide with the broad-signal entities.
func emitAuthPolicy(src, filePath, language, framework string, add func(types.EntityRecord)) {
	authEnt := func(name, subtype string, line int, kv ...string) {
		ent := makeEntity(name, "SCOPE.Pattern", "", filePath, language, line)
		setProps(&ent,
			"framework", framework,
			"provenance", "INFERRED_FROM_RUST_AUTH",
			"pattern_kind", "auth",
			"auth_subtype", subtype,
		)
		setProps(&ent, kv...)
		add(ent)
	}
	mwEnt := func(name, mwKind string, line int, kv ...string) {
		ent := makeEntity(name, "SCOPE.Pattern", "", filePath, language, line)
		setProps(&ent,
			"framework", framework,
			"provenance", "INFERRED_FROM_RUST_MIDDLEWARE",
			"pattern_kind", "middleware",
			"middleware_kind", mwKind,
		)
		setProps(&ent, kv...)
		add(ent)
	}

	// --- axum: middleware::from_fn(auth_fn) ---
	for _, m := range reAxumFromFn.FindAllStringSubmatchIndex(src, -1) {
		fn := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		method := authPolicyMethod(fn)
		// from_fn is a generic middleware surface; classify as auth only when the
		// fn name looks auth-shaped, else record a plain middleware guard.
		mwEnt("middleware:from_fn:"+fn, "from_fn", line,
			"middleware_name", fn, "guard_fn", fn)
		if isAuthMiddleware(fn) {
			authEnt("auth:from_fn:"+fn, "middleware_auth", line,
				"guard_name", fn, "auth_method", method, "auth_required", "true")
		}
	}

	// --- axum: .route_layer(X) ---
	for _, m := range reAxumRouteLayer.FindAllStringSubmatchIndex(src, -1) {
		layer := stripCtor(src[m[2]:m[3]])
		line := lineOf(src, m[0])
		mwEnt("middleware:route_layer:"+layer, "route_layer", line,
			"middleware_name", layer, "layer_scope", "route")
		if isAuthMiddleware(layer) {
			authEnt("auth:route_layer:"+layer, "middleware_auth", line,
				"guard_name", layer, "auth_method", authPolicyMethod(layer),
				"auth_required", "true")
		}
	}

	// --- tower-http: ValidateRequestHeaderLayer::bearer/basic/custom ---
	for _, m := range reValidateRequestHeader.FindAllStringSubmatchIndex(src, -1) {
		variant := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		method := authPolicyMethod(variant)
		if method == "unknown" {
			method = strings.ToLower(variant)
		}
		authEnt("auth:validate_request_header:"+variant, "middleware_auth", line,
			"guard_name", "ValidateRequestHeaderLayer::"+variant,
			"auth_method", method, "auth_required", "true")
	}

	// --- tower-http: RequireAuthorizationLayer::bearer/basic ---
	for _, m := range reRequireAuthLayer.FindAllStringSubmatchIndex(src, -1) {
		variant := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		method := authPolicyMethod(variant)
		if method == "unknown" {
			method = strings.ToLower(variant)
		}
		authEnt("auth:require_authorization:"+variant, "middleware_auth", line,
			"guard_name", "RequireAuthorizationLayer::"+variant,
			"auth_method", method, "auth_required", "true")
	}

	// --- axum custom extractor guards: impl FromRequestParts for X ---
	for _, m := range reFromRequestPartsImpl.FindAllStringSubmatchIndex(src, -1) {
		guard := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		authEnt("auth:extractor_guard:"+guard, "middleware_auth", line,
			"guard_name", guard, "guard_kind", "from_request_parts",
			"auth_method", authPolicyMethod(guard),
			"auth_required", "true")
	}
	// FromRequest (non-Parts) guards — only when auth-shaped, to avoid catching
	// every body extractor.
	for _, m := range reFromRequestImpl.FindAllStringSubmatchIndex(src, -1) {
		guard := src[m[2]:m[3]]
		if !isAuthMiddleware(guard) {
			continue
		}
		line := lineOf(src, m[0])
		authEnt("auth:extractor_guard:"+guard, "middleware_auth", line,
			"guard_name", guard, "guard_kind", "from_request",
			"auth_method", authPolicyMethod(guard),
			"auth_required", "true")
	}

	// --- tower ServiceBuilder ordered layer chain ---
	for _, cm := range reServiceBuilderChain.FindAllStringSubmatchIndex(src, -1) {
		chainBody := src[cm[2]:cm[3]]
		chainLine := lineOf(src, cm[0])
		order := 0
		var names []string
		for _, lm := range reChainLayer.FindAllStringSubmatch(chainBody, -1) {
			layer := stripCtor(lm[1])
			names = append(names, layer)
			mwEnt("middleware:tower_layer:"+layer, "tower_layer", chainLine,
				"middleware_name", layer,
				"layer_order", itoa(order),
				"layer_chain", "service_builder")
			if isAuthMiddleware(layer) {
				authEnt("auth:tower_layer:"+layer, "middleware_auth", chainLine,
					"guard_name", layer, "auth_method", authPolicyMethod(layer),
					"auth_required", "true", "layer_order", itoa(order))
			}
			order++
		}
		if len(names) > 0 {
			ent := makeEntity("middleware:layer_chain:"+strings.Join(names, ">"),
				"SCOPE.Pattern", "", filePath, language, chainLine)
			setProps(&ent,
				"framework", framework,
				"provenance", "INFERRED_FROM_RUST_MIDDLEWARE",
				"pattern_kind", "middleware",
				"middleware_kind", "layer_chain",
				"layer_count", itoa(len(names)),
				"layer_order_list", strings.Join(names, ">"),
			)
			add(ent)
		}
	}

	// --- actix: HttpAuthentication::bearer(validator) ---
	for _, m := range reActixHttpAuth.FindAllStringSubmatchIndex(src, -1) {
		variant := src[m[2]:m[3]]
		validator := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		method := authPolicyMethod(variant)
		if method == "unknown" {
			method = strings.ToLower(variant)
		}
		authEnt("auth:http_authentication:"+variant, "middleware_auth", line,
			"guard_name", "HttpAuthentication::"+variant,
			"validator_name", validator,
			"auth_method", method, "auth_required", "true")
	}

	// --- actix: custom Transform middleware ---
	for _, m := range reActixTransform.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		mwEnt("middleware:transform:"+name, "transform", line,
			"middleware_name", name, "middleware_trait", "Transform")
		if isAuthMiddleware(name) {
			authEnt("auth:transform:"+name, "middleware_auth", line,
				"guard_name", name, "auth_method", authPolicyMethod(name),
				"auth_required", "true")
		}
	}

	// --- rocket: fairings ---
	for _, m := range reRocketFairing.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		mwEnt("middleware:fairing:"+name, "fairing", line,
			"middleware_name", name, "middleware_trait", "Fairing")
		if isAuthMiddleware(name) {
			authEnt("auth:fairing:"+name, "middleware_auth", line,
				"guard_name", name, "auth_method", authPolicyMethod(name),
				"auth_required", "true")
		}
	}

	// --- rocket: request guards (auth-shaped) ---
	for _, m := range reRocketGuardAuth.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !isAuthMiddleware(name) {
			continue
		}
		// Skip if axum FromRequest already handled this in an axum file; the
		// shared `seen` set dedups by name anyway, so re-emit is harmless and
		// keeps rocket-only files covered.
		line := lineOf(src, m[0])
		authEnt("auth:request_guard:"+name, "middleware_auth", line,
			"guard_name", name, "guard_kind", "from_request",
			"auth_method", authPolicyMethod(name), "auth_required", "true")
	}
}
