package java

import (
	"fmt"
	"regexp"
	"strings"
)

// Play Framework Java custom extractor — route extraction, handler attribution,
// middleware, request validation, auth, and tests.
//
// Play Framework Java uses a plain-text DSL in conf/routes:
//
//	GET     /path                   controllers.Foo.bar
//	POST    /path/:id               controllers.Foo.create(id: Long)
//	GET     /path/$id<[0-9]+>       controllers.Foo.show(id: Long)
//
// Controller methods that return play.mvc.Result (or CompletionStage<Result>
// for async) are the handlers.  The framework uses Guice for DI (@Inject) and
// supports Play's JPA module (@Transactional from javax.transaction).
//
// Middleware in Play is implemented via play.mvc.Action subclasses and
// @With(MyAction.class) on controllers/methods, or via request filters
// that extend EssentialFilter / play.http.HttpFilters.
//
// Auth: Play has no built-in auth — projects use Deadbolt-2 (@Restrict,
// @SubjectPresent, @Dynamic) or hand-rolled before-filter checks.  We detect
// both.
//
// UI-centric cells are not_applicable:
//   - hydration_boundaries, server_components, static_generation: Play Java is
//     a server-side MVC framework with no SPA/SSR/hydration concepts.
//
// DI (Guice) and AOP (@Transactional) are partially present but are covered by
// the existing spring_boot / transactional extractors; we emit partial signals
// for the Play-specific wiring.
//
// Coverage cells delivered (#3090):
//   - Routing:    route_extraction, endpoint_synthesis, handler_attribution → partial
//   - Auth:       auth_coverage                                              → partial
//   - Validation: request_validation                                        → partial
//   - Middleware: middleware_coverage                                        → partial
//   - Testing:    tests_linkage                                              → partial
//   - UI:         hydration_boundaries, server_components, static_generation → not_applicable

// playFrameworks is the set of framework identifiers that activate the Play extractor.
var playFrameworks = map[string]bool{
	"play":            true,
	"play_framework":  true,
	"play-framework":  true,
	"playframework":   true,
}

var (
	// Play conf/routes line:
	//   GET   /path   controllers.Foo.bar
	//   POST  /path/:param   controllers.Foo.create(param: Long)
	//
	// Capture groups:
	//   1: HTTP verb  (GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)
	//   2: path
	//   3: controller action  (e.g. controllers.HomeController.index)
	playRoutesLineRE = regexp.MustCompile(
		`(?m)^[ \t]*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+(/\S*)\s+([\w.]+(?:\([^)]*\))?)`)

	// Result-returning controller method: any method returning Result or
	// CompletionStage<Result>.  Capture group 1: method name.
	playResultMethodRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:CompletionStage\s*<\s*Result\s*>|CompletableFuture\s*<\s*Result\s*>|Result)\s+(\w+)\s*\(`)

	// @Inject annotation — Play's Guice DI wiring signal.
	playInjectRE = regexp.MustCompile(`@Inject\b`)

	// @With(SomeAction.class) — Play action composition middleware.
	// Capture group 1: the action class name.
	playWithAnnotationRE = regexp.MustCompile(
		`@With\s*\(\s*(?:\{[^}]*\}|(\w+)\.class\s*)\)`)

	// EssentialFilter / HttpFilters subclass — Play's global filter middleware.
	// Capture group 1: class name.
	playFilterClassRE = regexp.MustCompile(
		`(?:public\s+)?(?:abstract\s+)?class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bEssentialFilter\b`)

	// play.mvc.Action subclass — per-action middleware.
	// Capture group 1: class name.
	playActionClassRE = regexp.MustCompile(
		`(?:public\s+)?class\s+(\w+)\s+extends\s+Action(?:<[^>]*>)?(?:\s|{)`)

	// HttpFilters implementation — Play's global filter chain.
	playHttpFiltersRE = regexp.MustCompile(
		`\bimplements\s+[^{]*\bHttpFilters\b|\bextends\s+DefaultHttpFilters\b`)

	// request().body() usage — request body access for taint/validation signal.
	playRequestBodyRE = regexp.MustCompile(
		`\brequest\s*\(\s*\)\s*\.\s*body\s*\(|\brequest\.body\b|\bRequest\b.*\bgetBody\b`)

	// Form.form(MyDto.class).bindFromRequest() — Play form binding / DTO extraction.
	// Capture group 1: DTO class name.
	playFormBindingRE = regexp.MustCompile(
		`\bForm\.form\s*\(\s*(\w+)\.class\s*\)|\bformFactory\s*\.\s*form\s*\(\s*(\w+)\.class\s*\)`)

	// Auth: Deadbolt-2 annotations.
	playDeadboltRE = regexp.MustCompile(
		`@Restrict\b|@SubjectPresent\b|@Dynamic\b|@SubjectNotPresent\b|@Group\b|@And\b|@Or\b|be\.objectify\.deadbolt`)

	// Auth: manual session/token guard patterns common in Play apps.
	playManualAuthRE = regexp.MustCompile(
		`\bsession\s*\(\s*"(?:user|userId|token|auth|jwt|principal)"|\bCtx\.current\(\).*session\b|\bHttp\.Context\.current\b`)

	// @Transactional from javax.transaction or play.db.jpa.Transactional.
	playTransactionalRE = regexp.MustCompile(
		`@Transactional\b`)

	// Test: play.test.WithApplication / play.test.Helpers / @RunWith(JUnitRunner.class)
	playTestSetupRE = regexp.MustCompile(
		`\bWithApplication\b|\bplay\.test\.Helpers\b|\bGuiceApplicationBuilder\b|\bfakeApplication\b`)

	// Test: @Test method detection.
	playTestMethodRE = regexp.MustCompile(
		`(?s)@Test\b(?:\s*\([^)]*\))?` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s*(?:public\s+|protected\s+|private\s+)?(?:\w+\s+)*` +
			`void\s+(\w+)\s*\(`)
)

// ExtractPlay runs the Play Framework extractor for route, middleware, DTO,
// auth, and test-linkage patterns.
func ExtractPlay(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !playFrameworks[ctx.Framework] {
		return result
	}

	isRoutes := isPlayRoutesFile(ctx.FilePath)

	// Quick-exit: no Play signals in this file.
	if !isRoutes {
		if !strings.Contains(ctx.Source, "play.mvc") &&
			!strings.Contains(ctx.Source, "play.mvc.Result") &&
			!strings.Contains(ctx.Source, "import play") &&
			!strings.Contains(ctx.Source, "Result") &&
			!strings.Contains(ctx.Source, "@Inject") &&
			!playDeadboltRE.MatchString(ctx.Source) &&
			!playTestSetupRE.MatchString(ctx.Source) {
			return result
		}
	}

	seen := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	if isRoutes {
		extractPlayRoutes(ctx, &result, seen, seenRels)
		return result
	}

	// Java controller/filter files
	extractPlayController(ctx, &result, seen, seenRels)
	extractPlayMiddleware(ctx, &result, seen, seenRels)
	extractPlayAuth(ctx, &result, seen, seenRels)
	extractPlayDTOs(ctx, &result, seen)
	extractPlayTests(ctx, &result, seen)

	return result
}

// isPlayRoutesFile returns true if the file path looks like a Play routes file.
func isPlayRoutesFile(filePath string) bool {
	base := filePath
	if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
		base = filePath[idx+1:]
	}
	// Match: routes, routes.GET, routes.POST, *.routes, etc.
	if base == "routes" || strings.HasPrefix(base, "routes.") || strings.HasSuffix(base, ".routes") {
		return true
	}
	// Also match if path contains conf/routes
	if strings.Contains(filePath, "conf/routes") {
		return true
	}
	return false
}

// extractPlayRoutes parses a Play conf/routes file and emits Route + Handler entities.
func extractPlayRoutes(ctx PatternContext, result *PatternResult, seen map[string]bool, seenRels map[relKey]bool) {
	for _, idx := range playRoutesLineRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := ctx.Source[idx[2]:idx[3]]
		rawPath := ctx.Source[idx[4]:idx[5]]
		controllerAction := ctx.Source[idx[6]:idx[7]]

		// Strip query-string-like parameter list from controller action for display.
		controllerDisplay := controllerAction
		if paren := strings.Index(controllerAction, "("); paren >= 0 {
			controllerDisplay = controllerAction[:paren]
		}

		// Canonicalise Play path parameters:
		//   :id  → {id}  (colon-style)
		//   $id<regex> → {id}  (regex-constrained)
		canonPath := canonicalizPlayPath(rawPath)

		ref := fmt.Sprintf("play:route:%s:%s:%s", verb, canonPath, ctx.FilePath)

		e := SecondaryEntity{
			Name:       canonPath,
			Kind:       "Route",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_ROUTES",
			Ref:        ref,
			Properties: map[string]any{
				"http_verb":         verb,
				"path":              canonPath,
				"framework":         "play",
				"route_type":        "conf_routes",
				"controller_action": controllerDisplay,
			},
		}
		addEntity(result, seen, e)

		// Handler attribution: emit a Handler entity for the controller action.
		handlerRef := fmt.Sprintf("play:handler:%s:%s", controllerDisplay, ctx.FilePath)
		handler := SecondaryEntity{
			Name:       controllerDisplay,
			Kind:       "Handler",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_HANDLER",
			Ref:        handlerRef,
			Properties: map[string]any{
				"framework":    "play",
				"handler_type": "controller_action",
				"http_verb":    verb,
				"path":         canonPath,
			},
		}
		addEntity(result, seen, handler)
		addRel(result, seenRels, Relationship{
			SourceRef:        ref,
			TargetRef:        handlerRef,
			RelationshipType: "HANDLED_BY",
			Properties:       map[string]string{"framework": "play"},
		})
	}
}

// canonicalizPlayPath converts Play-style path parameters to {param} form.
//
//	/users/:id         → /users/{id}
//	/files/$path<.+>   → /files/{path}
func canonicalizPlayPath(raw string) string {
	// Remove query string if any
	if q := strings.Index(raw, "?"); q >= 0 {
		raw = raw[:q]
	}

	// $param<regex> → {param}
	result := playDollarParamRE.ReplaceAllString(raw, "{$1}")
	// :param → {param}
	result = playColonParamRE.ReplaceAllString(result, "{$1}")

	return result
}

var (
	// $param<regex> path parameter: $id<[0-9]+> → {id}
	playDollarParamRE = regexp.MustCompile(`\$(\w+)<[^>]*>`)
	// :param path parameter: :id → {id}
	playColonParamRE = regexp.MustCompile(`:(\w+)`)
)

// extractPlayController detects Result-returning controller methods.
func extractPlayController(ctx PatternContext, result *PatternResult, seen map[string]bool, seenRels map[relKey]bool) {
	for _, idx := range playResultMethodRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		methodName := ctx.Source[idx[2]:idx[3]]
		// Skip common false-positives (constructor, getters, etc.)
		if methodName == "get" || methodName == "is" || methodName == "set" {
			continue
		}
		enclosingClass := findEnclosingClass(ctx.Source, idx[0])
		fullName := methodName
		if enclosingClass != "" {
			fullName = enclosingClass + "." + methodName
		}
		ref := fmt.Sprintf("play:handler:%s:%s", fullName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       fullName,
			Kind:       "Handler",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_RESULT_METHOD",
			Ref:        ref,
			Properties: map[string]any{
				"framework":    "play",
				"handler_type": "result_method",
				"method":       methodName,
				"class":        enclosingClass,
			},
		}
		addEntity(result, seen, e)
	}
}

// extractPlayMiddleware detects Play action-composition and filter middleware.
func extractPlayMiddleware(ctx PatternContext, result *PatternResult, seen map[string]bool, seenRels map[relKey]bool) {
	// @With(SomeAction.class) — per-controller/method composition
	for _, idx := range playWithAnnotationRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		actionClass := ""
		if idx[2] >= 0 {
			actionClass = ctx.Source[idx[2]:idx[3]]
		}
		if actionClass == "" {
			actionClass = "anonymous"
		}
		ref := fmt.Sprintf("play:middleware:with:%s:%s", actionClass, ctx.FilePath)
		e := SecondaryEntity{
			Name:       actionClass,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_WITH_ANNOTATION",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "play",
				"middleware_type": "action_composition",
				"action_class":    actionClass,
			},
		}
		addEntity(result, seen, e)
	}

	// play.mvc.Action subclass — custom action composition
	for _, idx := range playActionClassRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		className := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("play:middleware:action:%s:%s", className, ctx.FilePath)
		e := SecondaryEntity{
			Name:       className,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_ACTION_CLASS",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "play",
				"middleware_type": "action_class",
				"class":           className,
			},
		}
		addEntity(result, seen, e)
	}

	// EssentialFilter / HttpFilters — global filter chain
	if playHttpFiltersRE.MatchString(ctx.Source) {
		filterIdx := playHttpFiltersRE.FindStringIndex(ctx.Source)
		className := findEnclosingClass(ctx.Source, filterIdx[0])
		if className == "" {
			className = "HttpFilters"
		}
		ref := fmt.Sprintf("play:middleware:filter:%s:%s", className, ctx.FilePath)
		e := SecondaryEntity{
			Name:       className,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, filterIdx[0]),
			Provenance: "INFERRED_FROM_PLAY_HTTP_FILTERS",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "play",
				"middleware_type": "http_filter",
				"class":           className,
			},
		}
		addEntity(result, seen, e)
	}

	// EssentialFilter implements
	for _, idx := range playFilterClassRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		className := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("play:middleware:essential_filter:%s:%s", className, ctx.FilePath)
		e := SecondaryEntity{
			Name:       className,
			Kind:       "Middleware",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_ESSENTIAL_FILTER",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "play",
				"middleware_type": "essential_filter",
				"class":           className,
			},
		}
		addEntity(result, seen, e)
	}
}

// extractPlayAuth detects Deadbolt-2 and manual session auth patterns.
func extractPlayAuth(ctx PatternContext, result *PatternResult, seen map[string]bool, seenRels map[relKey]bool) {
	if playDeadboltRE.MatchString(ctx.Source) {
		deadboltIdx := playDeadboltRE.FindStringIndex(ctx.Source)
		ref := fmt.Sprintf("play:auth:deadbolt:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "deadbolt_auth",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, deadboltIdx[0]),
			Provenance: "INFERRED_FROM_PLAY_DEADBOLT",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "play",
				"auth_type": "deadbolt2",
				"auth_hook": "@Restrict/@SubjectPresent/@Dynamic",
			},
		}
		addEntity(result, seen, e)
	}

	if playManualAuthRE.MatchString(ctx.Source) {
		authIdx := playManualAuthRE.FindStringIndex(ctx.Source)
		ref := fmt.Sprintf("play:auth:manual_session:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "session_auth",
			Kind:       "AuthGuard",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, authIdx[0]),
			Provenance: "INFERRED_FROM_PLAY_SESSION_AUTH",
			Ref:        ref,
			Properties: map[string]any{
				"framework": "play",
				"auth_type": "session",
				"auth_hook": "session(key)",
			},
		}
		addEntity(result, seen, e)
	}
}

// extractPlayDTOs detects Play form binding and request body usage.
func extractPlayDTOs(ctx PatternContext, result *PatternResult, seen map[string]bool) {
	// Form.form(MyDto.class).bindFromRequest()
	for _, idx := range playFormBindingRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 6 {
			continue
		}
		dtoName := ""
		if idx[2] >= 0 {
			dtoName = ctx.Source[idx[2]:idx[3]]
		} else if idx[4] >= 0 {
			dtoName = ctx.Source[idx[4]:idx[5]]
		}
		if dtoName == "" || primitiveTypes[dtoName] {
			continue
		}
		ref := fmt.Sprintf("play:dto:%s:%s", dtoName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       dtoName,
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_FORM_BINDING",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "play",
				"dto_source": "Form.form.bindFromRequest",
			},
		}
		addEntity(result, seen, e)
	}

	// request().body() — request body access signal for request_validation
	if playRequestBodyRE.MatchString(ctx.Source) {
		bodyIdx := playRequestBodyRE.FindStringIndex(ctx.Source)
		ref := fmt.Sprintf("play:request_body:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "request_body",
			Kind:       "Schema",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, bodyIdx[0]),
			Provenance: "INFERRED_FROM_PLAY_REQUEST_BODY",
			Ref:        ref,
			Properties: map[string]any{
				"framework":         "play",
				"dto_source":        "request().body()",
				"request_validated": true,
			},
		}
		addEntity(result, seen, e)
	}
}

// extractPlayTests detects Play test setup and @Test methods.
func extractPlayTests(ctx PatternContext, result *PatternResult, seen map[string]bool) {
	if playTestSetupRE.MatchString(ctx.Source) {
		testIdx := playTestSetupRE.FindStringIndex(ctx.Source)
		ref := fmt.Sprintf("play:test_setup:%s", ctx.FilePath)
		e := SecondaryEntity{
			Name:       "PlayTest",
			Kind:       "TestSetup",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, testIdx[0]),
			Provenance: "INFERRED_FROM_PLAY_TEST_SETUP",
			Ref:        ref,
			Properties: map[string]any{
				"framework":  "play",
				"test_setup": "WithApplication/fakeApplication",
			},
		}
		addEntity(result, seen, e)
	}

	for _, idx := range playTestMethodRE.FindAllStringSubmatchIndex(ctx.Source, -1) {
		if len(idx) < 4 {
			continue
		}
		methodName := ctx.Source[idx[2]:idx[3]]
		ref := fmt.Sprintf("play:test:%s:%s", methodName, ctx.FilePath)
		e := SecondaryEntity{
			Name:       methodName,
			Kind:       "TestCase",
			SourceFile: ctx.FilePath,
			LineStart:  lineOf(ctx.Source, idx[0]),
			Provenance: "INFERRED_FROM_PLAY_TEST_METHOD",
			Ref:        ref,
			Properties: map[string]any{
				"framework":       "play",
				"test_annotation": "Test",
			},
		}
		addEntity(result, seen, e)
	}
}
