package java

import (
	"fmt"
	"regexp"
	"strings"
)

// Vert.x custom extractor — route extraction, handler attribution, middleware,
// DTO, auth, and tests.
//
// Vert.x Web uses a lambda/callback DSL for routing:
//
//	router.get("/path").handler(ctx -> ...)
//	router.route("/path").handler(routingContext -> ...)
//
// The router.route().handler() chain is also used for global middleware when
// no path is specified. Auth is handled via BasicAuthHandler.create(),
// JWTAuthHandler.create(), or manual header checks in route handlers.
//
// Vert.x has NO built-in DI, AOP, or transaction management:
//   - DI: di_binding_extraction, di_injection_point, di_scope_resolution → not_applicable
//   - AOP: advice_attribution, aspect_extraction, pointcut_resolution → not_applicable
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//     transaction_rollback_rules → not_applicable
//
// Coverage cells delivered (#3086):
//   - Routing:    route_extraction                   → partial
//   - Auth:       auth_coverage                      → partial
//   - Validation: dto_extraction, request_validation → partial
//   - Middleware: middleware_coverage                 → partial
//   - Testing:    tests_linkage                       → partial
//   - DI:         di_binding_extraction, di_injection_point, di_scope_resolution → not_applicable
//   - AOP:        advice_attribution, aspect_extraction, pointcut_resolution     → not_applicable
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//     transaction_rollback_rules                                                 → not_applicable

// vertxFrameworks is the set of framework identifiers that activate the Vert.x extractor.
var vertxFrameworks = map[string]bool{
	"vertx":     true,
	"vert.x":    true,
	"vert_x":    true,
	"vertx_web": true,
	"vertx-web": true,
}

var (
	// Route extraction: router.get("/path") or router.route("/path") DSL chains.
	// Captures verb and path. Supports all common HTTP verbs plus route() for
	// path-scoped handler chains.
	vertxRouteRE = regexp.MustCompile(
		`(?m)\brouter\s*\.\s*(get|post|put|delete|patch|head|options|route)\s*\(\s*"([^"]+)"`)

	// Global middleware: router.route().handler(...) with no path argument.
	// This form appears as router.route().handler(... without a path parameter.
	vertxGlobalRouteRE = regexp.MustCompile(
		`(?m)\brouter\s*\.\s*route\s*\(\s*\)\s*\.`)

	// Handler attribution: captures the lambda parameter or method reference
	// in .handler(ctx -> ...) or .handler(ClassName::method).
	// Three forms:
	//   1. Lambda: ctx ->
	//   2. Method ref: ClassName::method
	//   3. new Handler(): new ClassName()
	vertxHandlerRE = regexp.MustCompile(
		`\.handler\s*\(\s*` +
			`(?:` +
			`(\w+)\s*->` + // lambda param
			`|(\w+)::(\w+)` + // method reference
			`|new\s+(\w+)\s*\(` + // new Handler()
			`)`)

	// Auth: BasicAuthHandler.create(...) detection
	vertxBasicAuthRE = regexp.MustCompile(
		`\bBasicAuthHandler\s*\.\s*create\s*\(`)

	// Auth: JWTAuthHandler.create(...) detection
	vertxJWTAuthRE = regexp.MustCompile(
		`\bJWTAuthHandler\s*\.\s*create\s*\(`)

	// Auth: OAuth2AuthHandler or generic auth handler patterns
	vertxOAuth2RE = regexp.MustCompile(
		`\b(?:OAuth2AuthHandler|AuthorizationHandler|RedirectAuthHandler)\s*\.\s*create\s*\(`)

	// Auth: manual Authorization header check in handler
	vertxAuthGuardRE = regexp.MustCompile(
		`ctx\s*\.\s*request\s*\(\s*\)\s*\.\s*getHeader\s*\(\s*"Authorization"\s*\)|` +
			`ctx\s*\.\s*user\s*\(\s*\)|` +
			`ctx\s*\.\s*fail\s*\(\s*401`)

	// DTO extraction: body().as(Foo.class) or body().asPojo(Foo.class)
	vertxBodyAsRE = regexp.MustCompile(
		`\bbody\s*\(\s*\)\s*\.\s*as(?:Pojo)?\s*\(\s*(\w+)\s*\.class\s*\)`)

	// DTO extraction: request().bodyHandler(...) with JSON body
	vertxBodyHandlerRE = regexp.MustCompile(
		`\bbodyHandler\s*\(\s*(?:\w+)\s*->\s*\{[^}]*\.mapTo\s*\(\s*(\w+)\s*\.class\s*\)`)

	// Request validation: validator pattern (vertx-web-validation)
	// RequestParameterProcessor / body validation using `BodyProcessorFactory.create(Foo.class)`
	vertxValidatorRE = regexp.MustCompile(
		`(?:RequestPredicate\s*\.\s*|BodyProcessorFactory\s*\.\s*create\s*\(\s*)(\w+)\s*\.class`)

	// Tests: VertxTestContext + JUnit5 integration
	vertxTestContextRE = regexp.MustCompile(
		`\bVertxTestContext\b`)

	// Tests: @RunWith(VertxUnitRunner.class) - Vert.x Unit runner (JUnit 4 style)
	vertxUnitRunnerRE = regexp.MustCompile(
		`@RunWith\s*\(\s*VertxUnitRunner\s*\.class\s*\)`)

	// Tests: @ExtendWith(VertxExtension.class) - Vert.x JUnit 5 extension
	vertxExtensionRE = regexp.MustCompile(
		`@ExtendWith\s*\(\s*VertxExtension\s*\.class\s*\)`)

	// Tests: @Test methods in Vert.x test classes
	vertxTestMethodRE = regexp.MustCompile(
		`(?s)@Test\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|private\s+)?(?:\w+\s+)*` +
			`void\s+(\w+)\s*\(`)
)

// vertxRolesAllowedRE matches @RolesAllowed({"ADMIN"}) / @RolesAllowed("ADMIN")
// when a Vert.x app layers JAX-RS-style authorization annotations on handlers.
var vertxRolesAllowedRE = regexp.MustCompile(`@RolesAllowed\s*\(\s*([^)]*)\)`)

// vertxAuthTokenRE pulls quoted role tokens from a @RolesAllowed argument list.
var vertxAuthTokenRE = regexp.MustCompile(`"([^"]+)"`)

// vertxFileAuth resolves the file-level auth posture for a Vert.x router source:
// the auth mechanism (jwt > oauth2 > basic, in decreasing specificity) wired via
// an AuthenticationHandler, plus any @RolesAllowed roles. Returns the zero
// authStamp (method == "") when the file carries no recognised auth handler /
// annotation, so an unprotected router stamps nothing.
func vertxFileAuth(source string) authStamp {
	var roles []string
	for _, m := range vertxRolesAllowedRE.FindAllStringSubmatch(source, -1) {
		for _, t := range vertxAuthTokenRE.FindAllStringSubmatch(m[1], -1) {
			roles = append(roles, t[1])
		}
	}

	switch {
	case vertxJWTAuthRE.MatchString(source):
		return authStamp{
			required: true, method: "middleware", confidence: "medium",
			guard: "JWTAuthHandler", mechanism: "jwt", roles: roles,
		}
	case vertxOAuth2RE.MatchString(source):
		return authStamp{
			required: true, method: "middleware", confidence: "medium",
			guard: "OAuth2AuthHandler", mechanism: "oauth2", roles: roles,
		}
	case vertxBasicAuthRE.MatchString(source):
		return authStamp{
			required: true, method: "middleware", confidence: "medium",
			guard: "BasicAuthHandler", mechanism: "basic", roles: roles,
		}
	case len(roles) > 0:
		// @RolesAllowed present without an explicit handler create() call (the
		// handler may be wired in another file) — still a real role requirement.
		return authStamp{
			required: true, method: "annotation", confidence: "medium",
			guard: "RolesAllowed", roles: roles,
		}
	}
	return authStamp{}
}

// ExtractVertx runs the Vert.x extractor for route, middleware, DTO, auth,
// and test-linkage patterns.
func ExtractVertx(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !vertxFrameworks[ctx.Framework] {
		return result
	}

	// Quick-exit: no Vert.x signals in this file.
	if !strings.Contains(ctx.Source, "vertx") && !strings.Contains(ctx.Source, "Vertx") &&
		!strings.Contains(ctx.Source, "vert.x") && !strings.Contains(ctx.Source, "AbstractVerticle") &&
		!strings.Contains(ctx.Source, "Router") {
		return result
	}

	seen := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// File-level auth posture (#3862). A Vert.x AuthenticationHandler mounted on
	// the router protects the routes that follow it on the same router. Detecting
	// the precise route-subtree a handler guards is beyond single-file regex, so
	// we resolve a file-level mechanism (jwt/basic/oauth2) and stamp every route
	// in the file with that posture — a router file that wires JWTAuthHandler is
	// gating its routes. Honest-partial: confidence is "medium" (mechanism is
	// certain, per-route attribution is not), and a file with NO auth handler
	// stamps nothing.
	fileAuth := vertxFileAuth(ctx.Source)

	// ---------------------------------------------------------------------------
	// Route extraction + handler attribution
	// ---------------------------------------------------------------------------
	for _, idx := range vertxRouteRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 6 {
			continue
		}
		rawVerb := ctx.Source[idx[2]:idx[3]]
		rawPath := ctx.Source[idx[4]:idx[5]]

		// Normalize: route() without a verb is treated as a middleware chain
		// unless it has a .handler() that's actually a route. We still emit
		// it as route_type=path_chain to indicate partial coverage.
		verb := strings.ToUpper(rawVerb)
		if rawVerb == "route" {
			verb = "ANY"
		}

		ref := fmt.Sprintf("vertx:route:%s:%s:%s", verb, rawPath, ctx.FilePath)

		props := map[string]any{
			"http_verb":  verb,
			"path":       rawPath,
			"framework":  "vertx",
			"route_type": "lambda_dsl",
		}
		fileAuth.stamp(props)

		e := SecondaryEntity{
			Name:       rawPath,
			Kind:       "Route",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_VERTX_ROUTE",
			Ref:        ref,
			Properties: props,
		}
		addEntity(&result, seen, e)

		// Handler attribution: scan the line/block after the route for .handler(...)
		lineStart := idx[0]
		// Look ahead up to 2 lines for .handler() call
		lineEnd := lineStart
		newlinesFound := 0
		for lineEnd < len(ctx.Source) && newlinesFound < 3 {
			if ctx.Source[lineEnd] == '\n' {
				newlinesFound++
			}
			lineEnd++
		}
		if lineEnd > len(ctx.Source) {
			lineEnd = len(ctx.Source)
		}
		snippet := ctx.Source[lineStart:lineEnd]

		if m := vertxHandlerRE.FindStringSubmatch(snippet); len(m) >= 2 {
			var handlerName string
			switch {
			case len(m) > 1 && m[1] != "": // lambda param
				handlerName = m[1]
			case len(m) > 3 && m[2] != "" && m[3] != "": // method ref
				handlerName = m[2] + "::" + m[3]
			case len(m) > 4 && m[4] != "": // new Handler()
				handlerName = m[4]
			}

			if handlerName != "" {
				handlerRef := fmt.Sprintf("vertx:handler:%s:%s", handlerName, ctx.FilePath)
				handler := SecondaryEntity{
					Name:       handlerName,
					Kind:       "Handler",
					SourceFile: ctx.FilePath,
					LineStart:  lineOf(ctx.Source, idx[0]),
					Provenance: "INFERRED_FROM_VERTX_HANDLER",
					Ref:        handlerRef,
					Properties: map[string]any{
						"framework":    "vertx",
						"handler_type": "lambda",
						"http_verb":    verb,
						"path":         rawPath,
					},
				}
				addEntity(&result, seen, handler)
				addRel(&result, seenRels, Relationship{
					SourceRef:        ref,
					TargetRef:        handlerRef,
					RelationshipType: "HANDLED_BY",
					Properties:       map[string]string{"framework": "vertx"},
				})
			}
		}
	}

	// ---------------------------------------------------------------------------
	// Middleware: router.route().handler(...) — global middleware (no path)
	// ---------------------------------------------------------------------------
	for _, idx := range vertxGlobalRouteRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 2 {
			continue
		}
		lineStart := idx[0]
		// Look ahead for .handler() attribution
		lineEnd := lineStart
		newlinesFound := 0
		for lineEnd < len(ctx.Source) && newlinesFound < 3 {
			if ctx.Source[lineEnd] == '\n' {
				newlinesFound++
			}
			lineEnd++
		}
		if lineEnd > len(ctx.Source) {
			lineEnd = len(ctx.Source)
		}
		snippet := ctx.Source[lineStart:lineEnd]

		// Determine middleware type from the chained method names
		middlewareType := "request_handler"
		if strings.Contains(snippet, "BodyHandler") {
			middlewareType = "body_handler"
		} else if strings.Contains(snippet, "CorsHandler") {
			middlewareType = "cors_handler"
		} else if strings.Contains(snippet, "LoggerHandler") {
			middlewareType = "logger_handler"
		} else if strings.Contains(snippet, "SessionHandler") {
			middlewareType = "session_handler"
		} else if strings.Contains(snippet, "TimeoutHandler") {
			middlewareType = "timeout_handler"
		} else if strings.Contains(snippet, "StaticHandler") {
			middlewareType = "static_handler"
		} else if strings.Contains(snippet, "AuthenticationHandler") ||
			strings.Contains(snippet, "JWTAuthHandler") ||
			strings.Contains(snippet, "BasicAuthHandler") {
			middlewareType = "auth_handler"
		}

		ref := fmt.Sprintf("vertx:middleware:%s:%d:%s", middlewareType, lineOf(ctx.Source, idx[0]), ctx.FilePath)
		e := SecondaryEntity{
			Name:       middlewareType,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_VERTX_MIDDLEWARE",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "vertx",
				"middleware_type": middlewareType,
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// Auth: BasicAuthHandler, JWTAuthHandler, OAuth2AuthHandler, inline guards
	// ---------------------------------------------------------------------------
	if vertxBasicAuthRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("vertx:auth:basic_auth:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "BasicAuthHandler",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, vertxBasicAuthRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_VERTX_AUTH",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "vertx",
				"auth_type": "basic_auth",
				"auth_hook": "BasicAuthHandler.create",
			},
		}
		addEntity(&result, seen, e)
	}

	if vertxJWTAuthRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("vertx:auth:jwt_auth:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "JWTAuthHandler",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, vertxJWTAuthRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_VERTX_AUTH",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "vertx",
				"auth_type": "jwt_auth",
				"auth_hook": "JWTAuthHandler.create",
			},
		}
		addEntity(&result, seen, e)
	}

	if vertxOAuth2RE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("vertx:auth:oauth2:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "OAuth2AuthHandler",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, vertxOAuth2RE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_VERTX_AUTH",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "vertx",
				"auth_type": "oauth2",
				"auth_hook": "OAuth2AuthHandler.create",
			},
		}
		addEntity(&result, seen, e)
	}

	if vertxAuthGuardRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("vertx:auth:guard:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "auth_guard",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, vertxAuthGuardRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_VERTX_AUTH_GUARD",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "vertx",
				"auth_type": "inline_guard",
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// DTO extraction: body().as(Foo.class) and mapTo(Foo.class)
	// ---------------------------------------------------------------------------
	for _, m := range vertxBodyAsRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 || primitiveTypes[m[1]] {
			continue
		}
		dtoName := m[1]
		ref := fmt.Sprintf("vertx:dto:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_VERTX_DTO",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "vertx",
				"dto_source": "body().as",
			},
		}
		addEntity(&result, seen, e)
	}

	for _, m := range vertxBodyHandlerRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 || primitiveTypes[m[1]] {
			continue
		}
		dtoName := m[1]
		ref := fmt.Sprintf("vertx:dto:body_handler:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_VERTX_DTO",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "vertx",
				"dto_source": "bodyHandler.mapTo",
			},
		}
		addEntity(&result, seen, e)
	}

	// Request validation: vertx-web-validation RequestPredicate / BodyProcessorFactory
	for _, m := range vertxValidatorRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 || primitiveTypes[m[1]] {
			continue
		}
		dtoName := m[1]
		ref := fmt.Sprintf("vertx:validation:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_VERTX_VALIDATION",
			Ref:        ref,
			Properties: map[string]any{
				"framework":         "vertx",
				"dto_source":        "RequestPredicate/BodyProcessorFactory",
				"request_validated": true,
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// Tests: VertxTestContext / VertxExtension / VertxUnitRunner + @Test methods
	// ---------------------------------------------------------------------------
	testSetupDetected := false
	if vertxTestContextRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("vertx:test_setup:test_context:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "VertxTestContext",
			Kind:       "TestSetup",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, vertxTestContextRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_VERTX_TEST_SETUP",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "vertx",
				"test_setup": "VertxTestContext",
			},
		}
		addEntity(&result, seen, e)
		testSetupDetected = true
	}

	if vertxExtensionRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("vertx:test_setup:extension:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "VertxExtension",
			Kind:       "TestSetup",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, vertxExtensionRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_VERTX_TEST_SETUP",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "vertx",
				"test_setup": "VertxExtension",
			},
		}
		addEntity(&result, seen, e)
		testSetupDetected = true
	}

	if vertxUnitRunnerRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("vertx:test_setup:unit_runner:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "VertxUnitRunner",
			Kind:       "TestSetup",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, vertxUnitRunnerRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_VERTX_TEST_SETUP",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "vertx",
				"test_setup": "VertxUnitRunner",
			},
		}
		addEntity(&result, seen, e)
		testSetupDetected = true
	}

	_ = testSetupDetected // used for documentation clarity

	for _, idx := range vertxTestMethodRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		methodName := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("vertx:test:%s:%s", methodName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       methodName,
			Kind:       "TestCase",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_VERTX_TEST_METHOD",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "vertx",
				"test_annotation": "Test",
			},
		}
		addEntity(&result, seen, e)
	}

	return result
}
