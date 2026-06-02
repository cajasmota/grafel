package python

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_fastapi", &FastAPIExtractor{})
}

// FastAPIExtractor extracts FastAPI framework patterns: route decorators,
// Depends(), APIRouter, WebSocket, middleware, BackgroundTasks, and lifecycle
// events.
//
// Pydantic model classes (BaseModel / BaseSettings / RootModel subclasses) are
// intentionally NOT emitted as separate entities here. The base Python extractor
// already emits a canonical SCOPE.Component/class entity for every class
// definition in the same file, including Pydantic models. Emitting a second
// SCOPE.Schema entity for the same class from this extractor created within-file
// duplicates (one node per extractor) for names like "Order", inflating node
// counts without adding structural information. Framework properties for Pydantic
// models (orm_mode, env_prefix, alias_generator) are captured by the base Python
// extractor's applyFrameworkInnerClassProperties logic on the Config inner class.
// Issue #1501 — within-extractor dedup, fix 1/2.
type FastAPIExtractor struct{}

func (e *FastAPIExtractor) Language() string { return "python_fastapi" }

var (
	faRouteDecoratorRe = regexp.MustCompile(
		`(?m)@(\w+)\.(get|post|put|delete|patch|head|options|trace)\s*` +
			`\(\s*(?:r)?["']([^"']*)["']((?:[^()]|\([^()]*\))*)\)\s*\n` +
			// Tolerate intervening blank / comment lines and stacked sibling
			// decorators (e.g. slowapi `@limiter.limit("5/minute")`) between the
			// route decorator and `def` so the route is still recognised.
			`(?:[ \t]*(?:#[^\n]*|@[^\n]*)?\n)*` +
			`\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	faDependsParamRe = regexp.MustCompile(`=\s*Depends\s*\(\s*(\w+)\s*\)`)
	faAPIRouterRe    = regexp.MustCompile(`(?m)(\w+)\s*=\s*APIRouter\s*\(([^)]*)\)`)
	faRouterPrefixRe = regexp.MustCompile(`prefix\s*=\s*["']([^"']*)["']`)
	faRouterTagsRe   = regexp.MustCompile(`tags\s*=\s*\[\s*["']([^"']*)["']`)
	faWebSocketRe    = regexp.MustCompile(
		`(?m)@(\w+)\.websocket\s*\(\s*["']([^"']*)["'][^)]*\)\s*\n` +
			`(?:\s*(?:#[^\n]*)?\n)*` +
			`\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	faMiddlewareRe = regexp.MustCompile(
		`(?m)@(\w+)\.middleware\s*\(\s*["'](\w+)["'][^)]*\)\s*\n` +
			`(?:\s*(?:#[^\n]*)?\n)*` +
			`\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	faBGAddTaskRe = regexp.MustCompile(`(?m)(\w+)\.add_task\s*\(\s*(\w+)`)
	faLifecycleRe = regexp.MustCompile(
		`(?m)@(\w+)\.on_event\s*\(\s*["'](\w+)["'][^)]*\)\s*\n` +
			`(?:\s*(?:#[^\n]*)?\n)*` +
			`\s*(?:async\s+)?def\s+(\w+)\s*\(`)
)

// bgTaskHints are substrings that identify a BackgroundTasks variable name.
var bgTaskHints = []string{"background", "tasks", "bg_task", "bt"}

func (e *FastAPIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_fastapi")
	_, span := tracer.Start(ctx, "custom.python_fastapi")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord

	// 1. HTTP route decorators
	for _, idx := range allMatchesIndex(faRouteDecoratorRe, source) {
		appName := source[idx[2]:idx[3]]
		httpMethod := strings.ToUpper(source[idx[4]:idx[5]])
		path := source[idx[6]:idx[7]]
		decoratorArgs := source[idx[8]:idx[9]]
		handlerName := source[idx[10]:idx[11]]
		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "fastapi", "pattern_type": "route", "http_method": httpMethod, "path": path, "app_name": appName}
		// #3628 area #6 — endpoint protection. The handler's def signature is the
		// open-paren the route regex anchored on (idx[1] is just past `def name(`);
		// scan from there to the matching close-paren for Depends()/Security() auth
		// dependencies, and the decorator args for a `dependencies=[...]` kwarg.
		sig := pyCallArgRegion(source, idx[1])
		resolveFastAPIRouteAuth(sig, decoratorArgs).stamp(props)
		// #3628 rate-limit child — slowapi `@limiter.limit("5/minute")` is a
		// sibling decorator stacked with the route decorator (the route regex
		// can't include it), so scan the preceding decorator window.
		resolvePyEndpointRateLimit(decoratorWindow(source, idx[0], idx[1]), source).stamp(props)
		out = append(out, entity(handlerName, "SCOPE.Operation", "endpoint", file.Path, line, props))
	}

	// 2. Depends() injection
	for _, idx := range allMatchesIndex(faDependsParamRe, source) {
		depFn := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(depFn, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "fastapi", "pattern_type": "depends", "dependency_fn": depFn}))
	}

	// Note: Pydantic model classes (BaseModel subclasses) are not emitted here.
	// The base Python extractor already emits SCOPE.Component/class for every
	// class definition. Emitting a second SCOPE.Schema entity for the same class
	// creates within-file duplicates that inflate node counts (issue #1501).

	// 3. APIRouter instantiation
	for _, idx := range allMatchesIndex(faAPIRouterRe, source) {
		varName := source[idx[2]:idx[3]]
		args := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "fastapi", "pattern_type": "api_router", "var_name": varName}
		if pm := faRouterPrefixRe.FindStringSubmatch(args); pm != nil {
			props["prefix"] = pm[1]
		}
		if tm := faRouterTagsRe.FindStringSubmatch(args); tm != nil {
			props["tags"] = tm[1]
		}
		out = append(out, entity(varName, "SCOPE.Component", "", file.Path, line, props))
	}

	// 5. WebSocket routes
	for _, idx := range allMatchesIndex(faWebSocketRe, source) {
		appName := source[idx[2]:idx[3]]
		path := source[idx[4]:idx[5]]
		handlerName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		out = append(out, entity(handlerName, "SCOPE.Operation", "endpoint", file.Path, line,
			map[string]string{"framework": "fastapi", "pattern_type": "websocket", "path": path, "protocol": "websocket", "app_name": appName}))
	}

	// 6. Middleware
	for _, idx := range allMatchesIndex(faMiddlewareRe, source) {
		appName := source[idx[2]:idx[3]]
		mwType := source[idx[4]:idx[5]]
		handlerName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		out = append(out, entity(handlerName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "fastapi", "pattern_type": "middleware", "middleware_type": mwType, "app_name": appName}))
	}

	// 7. BackgroundTasks.add_task()
	for _, idx := range allMatchesIndex(faBGAddTaskRe, source) {
		btVar := source[idx[2]:idx[3]]
		taskFn := source[idx[4]:idx[5]]
		btLower := strings.ToLower(btVar)
		isBG := false
		for _, hint := range bgTaskHints {
			if strings.Contains(btLower, hint) {
				isBG = true
				break
			}
		}
		if !isBG {
			continue
		}
		line := lineOf(source, idx[0])
		out = append(out, entity(taskFn, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "fastapi", "pattern_type": "background_task", "task_fn": taskFn, "bt_var": btVar}))
	}

	// 8. Lifecycle events
	for _, idx := range allMatchesIndex(faLifecycleRe, source) {
		appName := source[idx[2]:idx[3]]
		eventType := source[idx[4]:idx[5]]
		handlerName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		out = append(out, entity(handlerName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "fastapi", "pattern_type": "lifecycle", "event_type": eventType, "app_name": appName}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
