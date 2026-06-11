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
	extractor.Register("python_flask", &FlaskExtractor{})
}

// FlaskExtractor extracts Flask framework patterns: route decorators,
// Blueprints, request hooks, error handlers, Flask-SQLAlchemy, Flask-Login,
// Flask-WTF, Flask-RESTful, Flask-SocketIO, app.config, and CLI commands.
type FlaskExtractor struct{}

func (e *FlaskExtractor) Language() string { return "python_flask" }

var (
	flRouteDecoratorRe = regexp.MustCompile(
		`(?m)@(\w+)\.route\s*\(\s*(?:r)?["']([^"']*)["']([^)]*)\)(?:\s*\n(?:\s*@[\w.]+(?:\([^)]*\))?\s*\n)*)\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flRouteMethodsRe    = regexp.MustCompile(`methods\s*=\s*\[([^\]]+)\]`)
	flHTTPMethodDecorRe = regexp.MustCompile(
		`(?m)@(\w+)\.(get|post|put|patch|delete|options|head)\s*\(\s*(?:r)?["']([^"']*)["']([^)]*)\)(?:\s*\n(?:\s*@[\w.]+(?:\([^)]*\))?\s*\n)*)\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flBlueprintRe       = regexp.MustCompile(`(?m)(\w+)\s*=\s*Blueprint\s*\(\s*["'](\w+)["']([^)]*)\)`)
	flURLPrefixRe       = regexp.MustCompile(`url_prefix\s*=\s*["']([^"']*)["']`)
	flRegBlueprintRe    = regexp.MustCompile(`(?m)(\w+)\.register_blueprint\s*\(\s*(\w+)([^)]*)\)`)
	flRequestHookRe     = regexp.MustCompile(`(?m)@(\w+)\.(before_request|after_request|teardown_request|before_app_request|after_app_request)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flErrorHandlerRe    = regexp.MustCompile(`(?m)@(\w+)\.errorhandler\s*\(\s*(\w+)\s*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flDBModelRe         = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*db\.Model[^)]*\)\s*:`)
	flLoginRequiredRe   = regexp.MustCompile(`(?m)@login_required\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flFlaskFormRe       = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*FlaskForm[^)]*\)\s*:`)
	flRestfulResourceRe = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*(?:(?:restful\.)?Resource)[^)]*\)\s*:`)
	flAddResourceRe     = regexp.MustCompile(`(?m)(?:\w+)\.add_resource\s*\(\s*(\w+)\s*,\s*(?:r)?["']([^"']*)["']`)
	flSocketIOOnRe      = regexp.MustCompile(`(?m)@(\w+)\.on\s*\(\s*["']([^"']*)["']([^)]*)\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	flAppConfigAssignRe = regexp.MustCompile(`(?m)(\w+)\.config\s*\[\s*["']([A-Z_][A-Z0-9_]*)["']`)
	flAppConfigFromRe   = regexp.MustCompile(`(?m)(\w+)\.config\.(from_object|from_envvar|from_pyfile|from_mapping)\s*\(`)
	flCLICommandRe      = regexp.MustCompile(`(?m)@(\w+)\.cli\.command\s*\(\s*(?:["']([^"']*)["'])?\s*[^)]*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// Flask-WTF form.validate_on_submit() detection (issue #3346).
	// Matches: if form.validate_on_submit(): or result = form.validate_on_submit()
	// Captures the form variable name and the enclosing function if available.
	flValidateOnSubmitRe = regexp.MustCompile(`(?m)\b(\w+)\.validate_on_submit\s*\(\s*\)`)
)

func (e *FlaskExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_flask")
	_, span := tracer.Start(ctx, "custom.python_flask")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord

	// 1. @app.route / @bp.route
	for _, idx := range allMatchesIndex(flRouteDecoratorRe, source) {
		appVar := source[idx[2]:idx[3]]
		path := source[idx[4]:idx[5]]
		decoratorArgs := source[idx[6]:idx[7]]
		funcName := source[idx[8]:idx[9]]
		methods := "GET"
		if mm := flRouteMethodsRe.FindStringSubmatch(decoratorArgs); mm != nil {
			methods = parseHTTPMethods(mm[1])
		}
		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "flask", "pattern_type": "route", "path": path, "http_methods": methods, "blueprint": appVar}
		// #3628 area #6 — endpoint protection. The full match spans the route
		// decorator through the stacked decorators to `def`; scan it for a
		// Flask-Login / Flask-Security auth decorator (the route decorator itself
		// is never an auth decorator, so it can't false-positive).
		resolveFlaskDecoratorAuth(source[idx[0]:idx[1]]).stamp(props)
		// #3628 rate-limit child — flask-limiter `@limiter.limit("100/hour")` /
		// django-ratelimit `@ratelimit(rate='5/m')` may be stacked above the
		// route decorator, so widen to the full preceding decorator block.
		resolvePyEndpointRateLimit(decoratorWindow(source, idx[0], idx[1]), source).stamp(props)
		// #4752 — stamp the view source (decorator block through the handler body)
		// so the flask resolver's source-scan fallback fires in the LIVE diff for
		// any decorator shape the structured props above don't cover.
		props["view_source"] = flViewSource(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Operation", "endpoint", file.Path, line, props))
	}

	// 2. HTTP method shorthand (@app.get, @app.post, etc.)
	for _, idx := range allMatchesIndex(flHTTPMethodDecorRe, source) {
		appVar := source[idx[2]:idx[3]]
		httpMethod := strings.ToUpper(source[idx[4]:idx[5]])
		path := source[idx[6]:idx[7]]
		funcName := source[idx[10]:idx[11]]
		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "flask", "pattern_type": "route", "path": path, "http_methods": httpMethod, "blueprint": appVar}
		resolveFlaskDecoratorAuth(source[idx[0]:idx[1]]).stamp(props)
		resolvePyEndpointRateLimit(decoratorWindow(source, idx[0], idx[1]), source).stamp(props)
		props["view_source"] = flViewSource(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Operation", "endpoint", file.Path, line, props))
	}

	// 3. Blueprints
	for _, idx := range allMatchesIndex(flBlueprintRe, source) {
		varName := source[idx[2]:idx[3]]
		bpName := source[idx[4]:idx[5]]
		constructorArgs := source[idx[6]:idx[7]]
		urlPrefix := ""
		if pm := flURLPrefixRe.FindStringSubmatch(constructorArgs); pm != nil {
			urlPrefix = pm[1]
		}
		line := lineOf(source, idx[0])
		out = append(out, entity(bpName, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "blueprint", "var_name": varName, "url_prefix": urlPrefix}))
	}
	_ = flRegBlueprintRe // register_blueprint creates OWNS edges (relationships not EntityRecords)

	// 4. Request hooks
	for _, idx := range allMatchesIndex(flRequestHookRe, source) {
		appVar := source[idx[2]:idx[3]]
		hookType := source[idx[4]:idx[5]]
		funcName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "request_hook", "hook_type": hookType, "app_var": appVar}))
	}

	// 5. Error handlers
	for _, idx := range allMatchesIndex(flErrorHandlerRe, source) {
		appVar := source[idx[2]:idx[3]]
		errorCode := source[idx[4]:idx[5]]
		funcName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "error_handler", "error_code": errorCode, "app_var": appVar}))
	}

	// 6. Flask-SQLAlchemy db.Model
	for _, idx := range allMatchesIndex(flDBModelRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Schema", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "db_model"}))
	}

	// 7. Flask-Login @login_required (metadata only)
	for _, idx := range allMatchesIndex(flLoginRequiredRe, source) {
		funcName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(funcName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "login_required", "auth_required": "true"}))
	}

	// 8. Flask-WTF FlaskForm
	for _, idx := range allMatchesIndex(flFlaskFormRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Schema", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "flask_form"}))
	}

	// 9. Flask-RESTful Resource
	for _, idx := range allMatchesIndex(flRestfulResourceRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "restful_resource"}))
	}
	_ = flAddResourceRe // DEPENDS_ON relationship, not entity

	// 10. Flask-SocketIO
	for _, idx := range allMatchesIndex(flSocketIOOnRe, source) {
		appName := source[idx[2]:idx[3]]
		eventName := source[idx[4]:idx[5]]
		handlerName := source[idx[8]:idx[9]]
		line := lineOf(source, idx[0])
		out = append(out, entity(handlerName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "socketio_handler", "event_name": eventName, "app_name": appName}))
	}

	// 11. app.config
	for _, idx := range allMatchesIndex(flAppConfigAssignRe, source) {
		configKey := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(configKey, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "config", "config_key": configKey}))
	}
	for _, idx := range allMatchesIndex(flAppConfigFromRe, source) {
		method := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity("config."+method, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "config_method", "method": method}))
	}

	// 12. CLI commands
	for _, idx := range allMatchesIndex(flCLICommandRe, source) {
		funcName := source[idx[6]:idx[7]]
		cmdName := funcName
		if idx[4] != -1 {
			cmdName = source[idx[4]:idx[5]]
		}
		line := lineOf(source, idx[0])
		out = append(out, entity(cmdName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "flask", "pattern_type": "cli_command", "func_name": funcName}))
	}

	// 13. Flask-WTF form.validate_on_submit() detection (issue #3346).
	// De-duplicate: only emit one entity per distinct form variable per file.
	seenVOS := map[string]bool{}
	for _, idx := range allMatchesIndex(flValidateOnSubmitRe, source) {
		formVar := source[idx[2]:idx[3]]
		if seenVOS[formVar] {
			continue
		}
		seenVOS[formVar] = true
		line := lineOf(source, idx[0])
		out = append(out, entity(formVar+".validate_on_submit", "SCOPE.Pattern", "form_submit", file.Path, line,
			map[string]string{
				"framework":    "flask",
				"pattern_type": "validate_on_submit",
				"form_var":     formVar,
			}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// flViewSource returns the view source slice starting at the route decorator
// (offset `start`) bounded to a small window — enough to carry the stacked auth
// decorators and the handler signature for the flask resolver's source-scan
// fallback (#4752) while keeping the graph payload small.
func flViewSource(source string, start int) string {
	const maxSource = 2048
	end := start + maxSource
	if end > len(source) {
		end = len(source)
	}
	return strings.TrimSpace(source[start:end])
}

func parseHTTPMethods(raw string) string {
	parts := strings.Split(raw, ",")
	var methods []string
	for _, p := range parts {
		m := strings.TrimSpace(p)
		m = strings.Trim(m, `"'`)
		m = strings.ToUpper(strings.TrimSpace(m))
		if m != "" {
			methods = append(methods, m)
		}
	}
	return strings.Join(methods, ",")
}
