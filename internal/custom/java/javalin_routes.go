package java

import (
	"fmt"
	"regexp"
	"strings"
)

// Javalin custom extractor — route extraction, handler attribution, middleware,
// DTO, auth, and tests.
//
// Javalin is a lightweight Java/Kotlin web framework that uses a lambda DSL for
// routing: `app.get("/path", ctx -> { ... })` — there are no annotations or
// resource classes. The framework has no built-in DI, AOP, or transaction
// management (DI/AOP/tx cells are not_applicable for Javalin).
//
// Coverage cells delivered (#3085):
//   - Routing:    route_extraction, endpoint_synthesis, handler_attribution → partial
//   - Auth:       auth_coverage                                              → partial
//   - Validation: dto_extraction, request_validation                        → partial
//   - Middleware: middleware_coverage                                        → partial
//   - Testing:    tests_linkage                                              → partial
//   - DI:         di_binding_extraction, di_injection_point, di_scope_resolution → not_applicable
//   - AOP:        advice_attribution, aspect_extraction, pointcut_resolution     → not_applicable
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//                   transaction_rollback_rules                                   → not_applicable

// javalinFrameworks is the set of framework identifiers that activate the
// Javalin extractor. Kotlin Javalin uses the same DSL:
//
//	app.get("/path") { ctx -> ... }
//
// The route regex (verb + path in quoted string) matches both Java lambda style
// and Kotlin trailing-lambda style identically.
var javalinFrameworks = map[string]bool{
	"javalin": true,
}

var (
	// Route registration: app.get("/path", handler) — captures verb and path.
	// Also matches app.get("/path", ctx -> ...) inline lambda and
	// app.get("/path", new HandlerClass()) reference forms.
	javalinRouteRE = regexp.MustCompile(
		`(?m)\bapp\s*\.\s*(get|post|put|delete|patch|head|options|before|after)\s*\(\s*"([^"]+)"`)

	// Handler attribution: captures the lambda parameter or method reference
	// after the path. Matches three forms:
	//   1. Lambda:    ctx ->
	//   2. Method ref: ClassName::method or this::method
	//   3. new Handler(): new ClassName()
	javalinHandlerRE = regexp.MustCompile(
		`(?m)\bapp\s*\.\s*(?:get|post|put|delete|patch|head|options)\s*\(\s*"[^"]+"\s*,\s*` +
			`(?:` +
			`(\w+)\s*->` + // lambda param
			`|(\w+)::(\w+)` + // method reference
			`|new\s+(\w+)\s*\(` + // new Handler()
			`)`)

	// Middleware: app.before() and app.after() with no path (global middleware)
	// and app.before("/path", handler) (path-scoped middleware).
	javalinBeforeAfterRE = regexp.MustCompile(
		`(?m)\bapp\s*\.\s*(before|after)\s*\(`)

	// DTO extraction: ctx.bodyAsClass(MyDto.class) — captures the DTO class name.
	javalinBodyAsClassRE = regexp.MustCompile(
		`\bctx\s*\.\s*bodyAsClass\s*\(\s*(\w+)\s*\.class\s*\)`)

	// Request validation: ctx.bodyValidator(MyDto.class) form.
	javalinBodyValidatorRE = regexp.MustCompile(
		`\bctx\s*\.\s*bodyValidator\s*\(\s*(\w+)\s*\.class\s*\)`)

	// Auth: common Javalin auth guard patterns:
	//   1. JavalinJWT.getTokenPayload / decodeToken usage
	//   2. ctx.attribute("user") checks in before handlers
	//   3. accessManager callback (app.accessManager / AccessManager implementation)
	//   4. ctx.basicAuthCredentials() / ctx.cookieStore()
	javalinAuthGuardRE = regexp.MustCompile(
		`(?:JavalinJWT\s*\.|` +
			`ctx\s*\.\s*attribute\s*\(\s*"(?:user|principal|token|auth|jwt|session)"\s*\)|` +
			`ctx\s*\.\s*(?:basicAuthCredentials|header\s*\(\s*"Authorization")` +
			`)`)

	// Access manager: app.accessManager(...) or config.accessManager(...)
	// Javalin v4 uses app.accessManager; v5 uses config.accessManager inside create().
	javalinAccessManagerRE = regexp.MustCompile(
		`(?m)\b(?:app|config)\s*\.\s*accessManager\s*\(`)

	// Per-route role guard: the trailing roles(...) argument of a route
	// registration, e.g.
	//   app.get("/admin", handler, roles(Role.ADMIN))
	//   app.post("/x", AdminHandler::handle, roles(Role.ADMIN, Role.USER))
	// Javalin's AccessManager receives this RouteRole set; a non-empty set means
	// the route requires those roles. Capture group 1 = the role argument list.
	javalinRouteRolesRE = regexp.MustCompile(
		`\broles\s*\(\s*([^)]*?)\s*\)`)

	// A single role token inside roles(...): Role.ADMIN, MyRoles.USER, ADMIN,
	// or "ADMIN". Capture group 1 = the role leaf name (enum constant or string).
	javalinRoleTokenRE = regexp.MustCompile(
		`(?:\w+\s*\.\s*)?(\w+)|"([^"]+)"`)

	// Tests: JavalinTest.create / JavalinTest.test / TestUtil.test —
	// Javalin's test utility setup detection. Both create (v4) and test (v5) forms.
	javalinTestRE = regexp.MustCompile(
		`\bJavalinTest\s*\.\s*(?:create|test)\b|\bTestUtil\s*\.\s*test\b`)

	// Tests: @Test methods in Javalin test classes (reuse junit5 if available,
	// but we also match directly for tests_linkage evidence).
	javalinTestMethodRE = regexp.MustCompile(
		`(?s)@Test\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|private\s+)?(?:\w+\s+)*` +
			`void\s+(\w+)\s*\(`)
)

// javalinRouteRoles extracts the role names from an inline roles(...) guard in a
// Javalin route registration. pathEnd is the offset just past the closing quote
// of the route path; we scan the remainder of that statement (up to the next
// route call or two newlines) for a roles(...) argument and return the role leaf
// names. Returns nil when the route carries no roles(...) guard (honest-partial:
// a bare app.get("/x", handler) is unprotected).
func javalinRouteRoles(source string, pathEnd int) []string {
	if pathEnd >= len(source) {
		return nil
	}
	// Bound the scan to this route statement: stop at the next "app." route
	// registration or after two newlines, whichever comes first, so a later
	// route's roles(...) is never mis-attributed.
	end := pathEnd
	newlines := 0
	for end < len(source) && newlines < 2 {
		if source[end] == '\n' {
			newlines++
		}
		if strings.HasPrefix(source[end:], "app.") || strings.HasPrefix(source[end:], "app ") {
			break
		}
		end++
	}
	stmt := source[pathEnd:end]
	m := javalinRouteRolesRE.FindStringSubmatch(stmt)
	if m == nil {
		return nil
	}
	var roles []string
	for _, tm := range javalinRoleTokenRE.FindAllStringSubmatch(m[1], -1) {
		tok := tm[1]
		if tm[2] != "" { // quoted string form
			tok = tm[2]
		}
		tok = strings.TrimSpace(tok)
		if tok != "" {
			roles = append(roles, tok)
		}
	}
	return roles
}

// ExtractJavalin runs the Javalin extractor for route, middleware, DTO, auth,
// and test-linkage patterns.
// Accepts both Java and Kotlin source: Kotlin Javalin uses the same
// app.get("/path") { ctx -> ... } routing DSL — the path-extraction regex
// matches identically for both languages.
func ExtractJavalin(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !javalinFrameworks[ctx.Framework] {
		return result
	}

	// Quick-exit: no Javalin signals in this file.
	if !strings.Contains(ctx.Source, "javalin") && !strings.Contains(ctx.Source, "Javalin") {
		return result
	}

	seen := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// File-level auth posture: an AccessManager wired on the app means EVERY
	// route is gated through it. Routes that carry an explicit roles(...) set get
	// a high-confidence role policy; routes with no inline roles inherit a
	// low-confidence "auth required" posture (the AccessManager decides at
	// runtime). Without an AccessManager and without inline roles we stamp
	// nothing (honest-partial).
	hasAccessManager := javalinAccessManagerRE.MatchString(ctx.Source)

	// ---------------------------------------------------------------------------
	// Route extraction + handler attribution
	// ---------------------------------------------------------------------------
	for _, idx := range javalinRouteRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 6 {
			continue
		}
		rawVerb := ctx.Source[idx[2]:idx[3]]
		rawPath := ctx.Source[idx[4]:idx[5]]

		// before/after are middleware — handled separately below.
		if rawVerb == "before" || rawVerb == "after" {
			continue
		}

		verb := strings.ToUpper(rawVerb)
		ref := fmt.Sprintf("javalin:route:%s:%s:%s", verb, rawPath, ctx.FilePath)

		props := map[string]any{
			"http_verb":  verb,
			"path":       rawPath,
			"framework":  "javalin",
			"route_type": "lambda_dsl",
		}

		// Auth: inline roles(...) guard on this route registration, scanned from
		// the route's own statement (path → end of statement).
		if roles := javalinRouteRoles(ctx.Source, idx[5]); len(roles) > 0 {
			authStamp{
				required:   true,
				method:     "middleware",
				confidence: "high",
				guard:      "roles(" + strings.Join(roles, ",") + ")",
				roles:      roles,
			}.stamp(props)
		} else if hasAccessManager {
			authStamp{
				required:   true,
				method:     "middleware",
				confidence: "low",
				guard:      "accessManager",
			}.stamp(props)
		}

		e := SecondaryEntity{
			Name:       rawPath,
			Kind:       "Route",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_JAVALIN_ROUTE",
			Ref:        ref,
			Properties: props,
		}
		addEntity(&result, seen, e)

		// Handler attribution: look for the handler expression after the path.
		// Scan the line containing this route for a handler pattern.
		lineStart := idx[0]
		lineEnd := strings.Index(ctx.Source[lineStart:], "\n")
		if lineEnd < 0 {
			lineEnd = len(ctx.Source) - lineStart
		}
		lineContent := ctx.Source[lineStart : lineStart+lineEnd]

		var handlerName string
		if m := javalinHandlerRE.FindStringSubmatch(lineContent); len(m) >= 2 {
			switch {
			case m[1] != "": // lambda param
				handlerName = m[1]
			case m[2] != "" && m[3] != "": // method ref
				handlerName = m[2] + "::" + m[3]
			case m[4] != "": // new Handler()
				handlerName = m[4]
			}
		}

		if handlerName != "" {
			handlerRef := fmt.Sprintf("javalin:handler:%s:%s", handlerName, ctx.FilePath)
			handler := SecondaryEntity{
				Name:       handlerName,
				Kind:       "Handler",
				SourceFile: ctx.FilePath,
				LineStart:  lineOf(ctx.Source, idx[0]),
				Provenance: "INFERRED_FROM_JAVALIN_HANDLER",
				Ref:        handlerRef,
				Properties: map[string]any{
					"framework":    "javalin",
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
				Properties:       map[string]string{"framework": "javalin"},
			})
		}
	}

	// ---------------------------------------------------------------------------
	// Middleware: app.before() and app.after() handlers
	// ---------------------------------------------------------------------------
	for _, idx := range javalinBeforeAfterRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		handlerType := ctx.Source[idx[2]:idx[3]] // "before" or "after"
		ref := fmt.Sprintf("javalin:middleware:%s:%d:%s", handlerType, lineOf(ctx.Source, idx[0]), ctx.FilePath)

		e := SecondaryEntity{
			Name:       handlerType + "_handler",
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_JAVALIN_MIDDLEWARE",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "javalin",
				"middleware_type": handlerType,
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// Auth: access manager + auth guard patterns in before handlers
	// ---------------------------------------------------------------------------
	if javalinAccessManagerRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("javalin:auth:access_manager:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "accessManager",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, javalinAccessManagerRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_JAVALIN_ACCESS_MANAGER",
			Ref:        ref,
			Properties: map[string]any{
				"framework":   "javalin",
				"auth_type":   "access_manager",
				"auth_hook":   "app.accessManager",
				"description": "Javalin AccessManager interface — centralized role-based auth hook",
			},
		}
		addEntity(&result, seen, e)
	}

	if javalinAuthGuardRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("javalin:auth:guard:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "auth_guard",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, javalinAuthGuardRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_JAVALIN_AUTH_GUARD",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "javalin",
				"auth_type": "inline_guard",
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// DTO extraction: ctx.bodyAsClass(MyDto.class)
	// ---------------------------------------------------------------------------
	for _, m := range javalinBodyAsClassRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 || primitiveTypes[m[1]] {
			continue
		}
		dtoName := m[1]
		ref := fmt.Sprintf("javalin:dto:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_JAVALIN_DTO",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "javalin",
				"dto_source": "ctx.bodyAsClass",
			},
		}
		addEntity(&result, seen, e)
	}

	// Request validation: ctx.bodyValidator(MyDto.class)
	for _, m := range javalinBodyValidatorRE.FindAllStringSubmatch(ctx.Source, -1) {
		if len(m) < 2 || primitiveTypes[m[1]] {
			continue
		}
		dtoName := m[1]
		ref := fmt.Sprintf("javalin:validation:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, strings.Index(ctx.Source, m[0])),
			Provenance: "INFERRED_FROM_JAVALIN_VALIDATION",
			Ref:        ref,
			Properties: map[string]any{
				"framework":         "javalin",
				"dto_source":        "ctx.bodyValidator",
				"request_validated": true,
			},
		}
		addEntity(&result, seen, e)
	}

	// ---------------------------------------------------------------------------
	// Tests: JavalinTest.create / TestUtil.test + @Test methods
	// ---------------------------------------------------------------------------
	if javalinTestRE.MatchString(ctx.Source) {
		ref := fmt.Sprintf("javalin:test_setup:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "JavalinTest",
			Kind:       "TestSetup",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, javalinTestRE.FindStringIndex(ctx.Source)[0]),
			Provenance: "INFERRED_FROM_JAVALIN_TEST_SETUP",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "javalin",
				"test_setup": "JavalinTest.create",
			},
		}
		addEntity(&result, seen, e)
	}

	for _, idx := range javalinTestMethodRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		methodName := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("javalin:test:%s:%s", methodName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       methodName,
			Kind:       "TestCase",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_JAVALIN_TEST_METHOD",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "javalin",
				"test_annotation": "Test",
			},
		}
		addEntity(&result, seen, e)
	}

	return result
}
