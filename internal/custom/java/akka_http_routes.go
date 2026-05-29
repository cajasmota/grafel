package java

import (
	"fmt"
	"regexp"
	"strings"
)

// Akka-HTTP Java DSL custom extractor — route extraction, handler attribution,
// middleware, DTO, auth, and tests.
//
// Akka-HTTP (Java DSL) uses a directive-based DSL for routing:
//
//	path("users", () ->
//	    get(() -> complete("OK"))
//	)
//
//	pathPrefix("api", () ->
//	    path("items", () ->
//	        get(() -> complete(items))
//	    )
//	)
//
// The outermost directives are composed via concat() or Route.concat().
// HTTP method directives (get, post, put, delete, patch, head, options)
// wrap the handler logic.  Auth is handled via authenticateBasic(...),
// authenticateOAuth2(...), or authorize(...) directives.  Middleware-equivalent
// behaviour is achieved via handleExceptions(...), withRequestTimeout(...),
// and similar cross-cutting directives.  DTO body extraction uses
// entity(as(MyDto.class), ...) or Jackson unmarshal directives.
//
// Akka-HTTP has NO built-in DI, AOP, or transaction management:
//   - DI:           di_binding_extraction, di_injection_point, di_scope_resolution → not_applicable
//   - AOP:          advice_attribution, aspect_extraction, pointcut_resolution     → not_applicable
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//     transaction_rollback_rules                                                   → not_applicable
//
// Coverage cells delivered (#3092):
//   - Routing:    route_extraction, endpoint_synthesis, handler_attribution → partial
//   - Auth:       auth_coverage                                              → partial
//   - Validation: dto_extraction, request_validation                        → partial
//   - Middleware: middleware_coverage                                        → partial
//   - Testing:    tests_linkage                                              → partial
//   - DI:         di_binding_extraction, di_injection_point, di_scope_resolution → not_applicable
//   - AOP:        advice_attribution, aspect_extraction, pointcut_resolution     → not_applicable
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//     transaction_rollback_rules                                                  → not_applicable

// akkaHTTPFrameworks is the set of framework identifiers that activate the
// Akka-HTTP extractor.
var akkaHTTPFrameworks = map[string]bool{
	"akka-http":   true,
	"akka_http":   true,
	"akkahttp":    true,
	"akka-http-java": true,
}

var (
	// Route path: path("segment") or path("seg1" / "seg2") directive.
	// Capture group 1: first path segment (string literal).
	akkaHTTPPathRE = regexp.MustCompile(
		`\bpath\s*\(\s*"([^"]+)"`)

	// Nested routing: pathPrefix("prefix") directive.
	// Capture group 1: prefix string.
	akkaHTTPPathPrefixRE = regexp.MustCompile(
		`\bpathPrefix\s*\(\s*"([^"]+)"`)

	// HTTP method directives: get(() -> ...), post(() -> ...), etc.
	// Capture group 1: HTTP method name (lowercase).
	akkaHTTPMethodRE = regexp.MustCompile(
		`\b(get|post|put|delete|patch|head|options)\s*\(\s*(?:\(\s*\)|[a-z_]\w*\s*->|\(\s*\)\s*->)`)

	// Handler attribution: method reference or lambda returning a Route.
	// Three forms:
	//   1. complete(handler.method(...))
	//   2. ClassName::method
	//   3. () -> handlerObj.methodName(...)
	akkaHTTPHandlerRE = regexp.MustCompile(
		`(?:` +
			`(\w+)::(\w+)` + // method reference: Handler::method
			`|complete\s*\(\s*(\w+)\s*\.\s*(\w+)` + // complete(handler.method
			`)`)

	// Auth: authenticateBasic("realm", ...) directive.
	akkaHTTPAuthBasicRE = regexp.MustCompile(
		`\bauthenticateBasic\s*\(`)

	// Auth: authenticateOAuth2("realm", ...) directive.
	akkaHTTPAuthOAuth2RE = regexp.MustCompile(
		`\bauthenticateOAuth2\s*\(`)

	// Auth: authorize(...) / authorizeAsync(...) directive.
	akkaHTTPAuthorizationRE = regexp.MustCompile(
		`\bauthorize(?:Async)?\s*\(`)

	// Auth: headerValueByName("Authorization", ...) manual auth extraction.
	akkaHTTPAuthHeaderRE = regexp.MustCompile(
		`\bheaderValueByName\s*\(\s*"Authorization"`)

	// DTO body extraction: entity(as(MyDto.class), ...) Jackson form.
	// Capture group 1: DTO class name.
	akkaHTTPEntityAsRE = regexp.MustCompile(
		`\bentity\s*\(\s*as\s*\(\s*(\w+)\s*\.class\s*\)`)

	// DTO body extraction: Unmarshaller.sync / unmarshal(..., MyDto.class) form.
	// Capture group 1: DTO class name.
	akkaHTTPUnmarshalRE = regexp.MustCompile(
		`\bunmarshal\s*\([^,]+,\s*(\w+)\s*\.class\s*\)`)

	// Request validation: parameter("name", ...) / parameterOptional(...) /
	// formField("name", ...) directives.
	akkaHTTPParamRE = regexp.MustCompile(
		`\b(?:parameter|parameterOptional|formField|headerValue)\s*\(\s*"(\w[^"]*)"`)

	// Middleware: handleExceptions(...) exception handling directive.
	akkaHTTPHandleExceptionsRE = regexp.MustCompile(
		`\bhandleExceptions\s*\(`)

	// Middleware: withRequestTimeout(...) timeout directive.
	akkaHTTPTimeoutRE = regexp.MustCompile(
		`\bwithRequestTimeout\s*\(`)

	// Middleware: logRequest / logResult / logRequestResult directives.
	akkaHTTPLogRE = regexp.MustCompile(
		`\b(?:logRequest|logResult|logRequestResult)\s*\(`)

	// Middleware: respondWithHeader / respondWithDefaultHeader directives.
	akkaHTTPHeaderDirectiveRE = regexp.MustCompile(
		`\b(?:respondWithHeader|respondWithDefaultHeader)\s*\(`)

	// Tests: RouteTest / RouteTestCase / testRoute usage (akka-http-testkit).
	akkaHTTPRouteTestRE = regexp.MustCompile(
		`\b(?:RouteTest|RouteTestCase|testRoute|runRoute|checkRoute)\b`)

	// Tests: testkit imports or WithRouteTesting.
	akkaHTTPTestKitRE = regexp.MustCompile(
		`akka\.http\.scaladsl\.testkit|akka\.http\.javadsl\.testkit|WithRouteTesting`)

	// Tests: @Test method declarations.
	akkaHTTPTestMethodRE = regexp.MustCompile(
		`(?s)@Test\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|private\s+)?(?:\w+\s+)*` +
			`void\s+(\w+)\s*\(`)
)

// ExtractAkkaHTTP runs the Akka-HTTP extractor for route, middleware, DTO,
// auth, and test-linkage patterns.
func ExtractAkkaHTTP(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !akkaHTTPFrameworks[ctx.Framework] {
		return result
	}

	// Quick-exit: no Akka-HTTP signals in this file.
	if !strings.Contains(ctx.Source, "akka.http") && !strings.Contains(ctx.Source, "akka-http") &&
		!strings.Contains(ctx.Source, "Route") && !strings.Contains(ctx.Source, "path(") &&
		!strings.Contains(ctx.Source, "pathPrefix(") {
		return result
	}

	seen := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// ---------------------------------------------------------------------------
	// Route extraction + handler attribution
	//
	// Akka-HTTP Java DSL routes are expressed as nested directive calls.
	// We emit a Route entity for each path() / pathPrefix() occurrence combined
	// with any HTTP method directive found in its surrounding block.
	// ---------------------------------------------------------------------------

	// Scan for path() directives and emit route entities.
	for _, idx := range akkaHTTPPathRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		rawPath := ctx.Source[idx[2]:idx[3]]

		// Scan the surrounding block (up to 10 lines ahead) for an HTTP method directive.
		lineStart := idx[0]
		blockEnd := lineStart
		newlinesFound := 0
		for blockEnd < len(ctx.Source) && newlinesFound < 10 {
			if ctx.Source[blockEnd] == '\n' {
				newlinesFound++
			}
			blockEnd++
		}
		if blockEnd > len(ctx.Source) {
			blockEnd = len(ctx.Source)
		}

		// Also scan 5 lines BEFORE the path() call (method directive may precede path).
		blockStart := lineStart
		newlinesBefore := 0
		for blockStart > 0 && newlinesBefore < 5 {
			blockStart--
			if ctx.Source[blockStart] == '\n' {
				newlinesBefore++
			}
		}

		snippet := ctx.Source[blockStart:blockEnd]
		methodMatches := akkaHTTPMethodRE.FindAllStringSubmatch(snippet, -1)

		if len(methodMatches) == 0 {
			// Emit a path with ANY verb when no method directive is detected (e.g.
			// pathPrefix-only or concat-wrapped route blocks).
			verb := "ANY"
			ref := fmt.Sprintf("akka-http:route:%s:%s:%s", verb, rawPath, ctx.FilePath)
			e := SecondaryEntity{
				Name:       rawPath,
				Kind:       "Route",
				SourceFile: ctx.FilePath,
				LineStart:  lineOf(ctx.Source, idx[0]),
				Provenance: "INFERRED_FROM_AKKA_HTTP_PATH",
				Ref:        ref,
				Properties: map[string]any{
					"http_verb":  verb,
					"path":       rawPath,
					"framework":  "akka-http",
					"route_type": "directive_dsl",
				},
			}
			addEntity(&result, seen, e)
		} else {
			for _, mm := range methodMatches {
				verb := strings.ToUpper(mm[1])
				ref := fmt.Sprintf("akka-http:route:%s:%s:%s", verb, rawPath, ctx.FilePath)
				e := SecondaryEntity{
					Name:       rawPath,
					Kind:       "Route",
					SourceFile: ctx.FilePath,
					LineStart:  lineOf(ctx.Source, idx[0]),
					Provenance: "INFERRED_FROM_AKKA_HTTP_PATH",
					Ref:        ref,
					Properties: map[string]any{
						"http_verb":  verb,
						"path":       rawPath,
						"framework":  "akka-http",
						"route_type": "directive_dsl",
					},
				}
				addEntity(&result, seen, e)

				// Handler attribution: look for method reference or complete() call in snippet.
				if m := akkaHTTPHandlerRE.FindStringSubmatch(snippet); len(m) >= 3 {
					var handlerName string
					switch {
					case m[1] != "" && m[2] != "": // method ref: Handler::method
						handlerName = m[1] + "::" + m[2]
					case len(m) > 3 && m[3] != "" && m[4] != "": // complete(handler.method)
						handlerName = m[3] + "." + m[4]
					}
					if handlerName != "" {
						handlerRef := fmt.Sprintf("akka-http:handler:%s:%s", handlerName, ctx.FilePath)
						handler := SecondaryEntity{
							Name:       handlerName,
							Kind:       "Handler",
							SourceFile: ctx.FilePath,
							LineStart:  lineOf(ctx.Source, idx[0]),
							Provenance: "INFERRED_FROM_AKKA_HTTP_HANDLER",
							Ref:        handlerRef,
							Properties: map[string]any{
								"framework":    "akka-http",
								"handler_type": "directive_lambda",
								"http_verb":    verb,
								"path":         rawPath,
							},
						}
						addEntity(&result, seen, handler)
						addRel(&result, seenRels, Relationship{
							SourceRef:        ref,
							TargetRef:        handlerRef,
							RelationshipType: "HANDLED_BY",
							Properties:       map[string]string{"framework": "akka-http"},
						})
					}
				}
			}
		}
	}

	// Scan for pathPrefix() directives and emit Route entities for the prefix.
	for _, idx := range akkaHTTPPathPrefixRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		rawPrefix := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("akka-http:route-prefix:%s:%s", rawPrefix, ctx.FilePath)
		e := SecondaryEntity{
			Name:       rawPrefix,
			Kind:       "Route",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_PATH_PREFIX",
			Ref:        ref,
			Properties: map[string]any{
				"http_verb":  "ANY",
				"path":       rawPrefix,
				"framework":  "akka-http",
				"route_type": "path_prefix",
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// Middleware: handleExceptions, withRequestTimeout, logRequest*, respondWith*
	// ---------------------------------------------------------------------------
	if akkaHTTPHandleExceptionsRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:middleware:exception_handler:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "handleExceptions",
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPHandleExceptionsRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_MIDDLEWARE",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "akka-http",
				"middleware_type": "exception_handler",
			},
		}
		addEntity(&result, seen, e)
	}

	if akkaHTTPTimeoutRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:middleware:request_timeout:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "withRequestTimeout",
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPTimeoutRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_MIDDLEWARE",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "akka-http",
				"middleware_type": "request_timeout",
			},
		}
		addEntity(&result, seen, e)
	}

	if akkaHTTPLogRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:middleware:logging:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "logRequest",
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPLogRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_MIDDLEWARE",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "akka-http",
				"middleware_type": "logging",
			},
		}
		addEntity(&result, seen, e)
	}

	if akkaHTTPHeaderDirectiveRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:middleware:response_header:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "respondWithHeader",
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPHeaderDirectiveRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_MIDDLEWARE",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "akka-http",
				"middleware_type": "response_header",
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// Auth: authenticateBasic, authenticateOAuth2, authorize, Authorization header
	// ---------------------------------------------------------------------------
	if akkaHTTPAuthBasicRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:auth:basic_auth:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "authenticateBasic",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPAuthBasicRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_AUTH",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "akka-http",
				"auth_type": "basic_auth",
				"auth_hook": "authenticateBasic",
			},
		}
		addEntity(&result, seen, e)
	}

	if akkaHTTPAuthOAuth2RE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:auth:oauth2:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "authenticateOAuth2",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPAuthOAuth2RE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_AUTH",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "akka-http",
				"auth_type": "oauth2",
				"auth_hook": "authenticateOAuth2",
			},
		}
		addEntity(&result, seen, e)
	}

	if akkaHTTPAuthorizationRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:auth:authorization:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "authorize",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPAuthorizationRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_AUTH",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "akka-http",
				"auth_type": "authorize",
				"auth_hook": "authorize",
			},
		}
		addEntity(&result, seen, e)
	}

	if akkaHTTPAuthHeaderRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:auth:header_guard:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "Authorization_header",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, akkaHTTPAuthHeaderRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_AUTH_HEADER",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "akka-http",
				"auth_type": "header_guard",
				"auth_hook": "headerValueByName(Authorization)",
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// DTO extraction: entity(as(MyDto.class)) and unmarshal(..., MyDto.class)
	// ---------------------------------------------------------------------------
	for _, m := range akkaHTTPEntityAsRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 || primitiveTypes[m[1]] {
			continue
		}
		dtoName := m[1]
		ref := fmt.Sprintf("akka-http:dto:entity_as:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_AKKA_HTTP_DTO",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "akka-http",
				"dto_source": "entity(as(...))",
			},
		}
		addEntity(&result, seen, e)
	}

	for _, m := range akkaHTTPUnmarshalRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 || primitiveTypes[m[1]] {
			continue
		}
		dtoName := m[1]
		ref := fmt.Sprintf("akka-http:dto:unmarshal:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_AKKA_HTTP_DTO",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "akka-http",
				"dto_source": "unmarshal(..., MyDto.class)",
			},
		}
		addEntity(&result, seen, e)
	}

	// Request validation: parameter() / parameterOptional() / formField() directives.
	for _, m := range akkaHTTPParamRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 {
			continue
		}
		paramName := m[1]
		ref := fmt.Sprintf("akka-http:validation:param:%s:%s", paramName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       paramName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_AKKA_HTTP_PARAM",
			Ref:        ref,
			Properties: map[string]any{
				"framework":         "akka-http",
				"dto_source":        "parameter/formField directive",
				"request_validated": true,
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// Tests: akka-http-testkit usage + @Test methods
	// ---------------------------------------------------------------------------
	if akkaHTTPRouteTestRE.MatchString(ctx.Source) || akkaHTTPTestKitRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("akka-http:test_setup:testkit:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "akka-http-testkit",
			Kind:       "TestSetup",
			SourceFile: ctx.FilePath,
			LineStart: func() int {
				idx := akkaHTTPRouteTestRE.FindStringIndex(ctx.Source)
				if idx != nil {
					return lineOf(ctx.Source, idx[0])
				}
				idx = akkaHTTPTestKitRE.FindStringIndex(ctx.Source)
				if idx != nil {
					return lineOf(ctx.Source, idx[0])
				}
				return 1
			}(),
			Provenance: "INFERRED_FROM_AKKA_HTTP_TEST_SETUP",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "akka-http",
				"test_setup": "akka-http-testkit",
			},
		}
		addEntity(&result, seen, e)
	}

	for _, idx := range akkaHTTPTestMethodRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		methodName := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("akka-http:test:%s:%s", methodName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       methodName,
			Kind:       "TestCase",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_AKKA_HTTP_TEST",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "akka-http",
				"test_kind": "junit_method",
			},
		}
		addEntity(&result, seen, e)
	}

	return result
}
