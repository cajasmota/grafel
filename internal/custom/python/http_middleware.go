// http_middleware.go — Python HTTP-framework middleware detection.
//
// Extracts middleware registration patterns for the Python http_backend
// frameworks that do NOT have a dedicated custom extractor with middleware
// support (Django/FastAPI/Flask are handled by their own extractors).
//
// Covered frameworks and their canonical middleware patterns:
//
//	aiohttp      — app.middlewares=[mw1, mw2] list assignment
//	bottle       — @bottle.hook('before_request') / @app.hook(...)
//	cherrypy     — cherrypy.tools.<name> = cherrypy.Tool(...) / @cherrypy.expose
//	               pipeline via ['/'].tools.<name>.on = True
//	falcon       — falcon.App(middleware=[...]) / app.add_middleware(mw)
//	hug          — @hug.request_middleware / @hug.response_middleware
//	litestar     — Litestar(middleware=[...]) / app.register(MiddlewareProtocol)
//	pyramid      — config.add_tween('pkg.Tween') tween factory registration
//	quart        — @app.before_request / @app.after_request (Flask-compatible)
//	robyn        — app.before_request() / app.after_request() decorators
//	sanic        — app.register_middleware(fn, 'request'|'response')
//	               @app.middleware('request') decorator
//	starlette    — Starlette(middleware=[...]) / app.add_middleware(Cls) /
//	               @app.middleware("http") decorator
//	strawberry   — strawberry.Schema(extensions=[...]) GraphQL middleware list
//	tornado      — RequestHandler.prepare() + set_default_headers() hooks;
//	               middleware via Application settings / transform classes
//
// Issue #3054.
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
	extractor.Register("python_http_middleware", &HTTPMiddlewareExtractor{})
}

// HTTPMiddlewareExtractor detects middleware registration patterns across the
// Python HTTP backend frameworks not covered by dedicated extractors.
type HTTPMiddlewareExtractor struct{}

func (e *HTTPMiddlewareExtractor) Language() string { return "python_http_middleware" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// aiohttp: app.middlewares = [mw1, mw2, ...] or web.Application(middlewares=[...])
	aiohttpMiddlewaresAssignRe = regexp.MustCompile(
		`(?m)(\w+)\.middlewares\s*=\s*\[`)
	aiohttpMiddlewaresKwargRe = regexp.MustCompile(
		`(?m)web\.Application\s*\([^)]*middlewares\s*=\s*\[`)

	// bottle: @bottle.hook('before_request') or @app.hook('after_request')
	bottleHookRe = regexp.MustCompile(
		`(?m)@(?:bottle|\w+)\.hook\s*\(\s*["'](before_request|after_request|before_response|after_response)["']\s*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// cherrypy: cherrypy.tools.<name> = cherrypy.Tool(...)
	cherrypyToolAssignRe = regexp.MustCompile(
		`(?m)cherrypy\.tools\.(\w+)\s*=\s*cherrypy\.Tool\s*\(`)
	// cherrypy: ['/'].tools.<name>.on = True in _cp_config or config
	cherrypyToolOnRe = regexp.MustCompile(
		`(?m)['"]/[^'"]*['"].*tools\.(\w+)\.on\s*=\s*True`)

	// falcon: falcon.App(middleware=[...]) / falcon.API(middleware=[...])
	falconAppMiddlewareRe = regexp.MustCompile(
		`(?m)falcon\.(?:App|API)\s*\([^)]*middleware\s*=\s*\[`)
	// falcon: app.add_middleware(SomeMW())
	falconAddMiddlewareRe = regexp.MustCompile(
		`(?m)(\w+)\.add_middleware\s*\(\s*(\w+)`)

	// hug: @hug.request_middleware / @hug.response_middleware
	hugMiddlewareRe = regexp.MustCompile(
		`(?m)@hug\.(request_middleware|response_middleware)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// litestar: Litestar(middleware=[...])
	litestarMiddlewareKwargRe = regexp.MustCompile(
		`(?m)Litestar\s*\([^)]*middleware\s*=\s*\[`)
	// litestar: AbstractMiddlewareFactory / MiddlewareProtocol class
	litestarMiddlewareClassRe = regexp.MustCompile(
		`(?m)^class\s+(\w+)\s*\([^)]*(?:MiddlewareProtocol|AbstractMiddleware)[^)]*\)\s*:`)

	// pyramid: config.add_tween('pkg.tween_factory')
	pyramidAddTweenRe = regexp.MustCompile(
		`(?m)config\.add_tween\s*\(\s*["']([^"']+)["']`)

	// quart: @app.before_request / @app.after_request (Flask-compatible)
	quartHookRe = regexp.MustCompile(
		`(?m)@(\w+)\.(before_request|after_request|teardown_request|before_app_request|after_app_request)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	// quart: Quart() import signal
	quartImportRe = regexp.MustCompile(`(?m)(?:from quart import|import quart)`)

	// robyn: app.before_request(fn) / app.after_request(fn) / @app.before_request
	robynBeforeRe = regexp.MustCompile(
		`(?m)@(\w+)\.(before_request|after_request)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	robynBeforeCallRe = regexp.MustCompile(
		`(?m)(\w+)\.(before_request|after_request)\s*\(\s*(\w+)\s*\)`)

	// sanic: app.register_middleware(fn, 'request') / @app.middleware('request')
	sanicRegisterMiddlewareRe = regexp.MustCompile(
		`(?m)(\w+)\.register_middleware\s*\(\s*(\w+)\s*,\s*["'](request|response)["']`)
	sanicMiddlewareDecoratorRe = regexp.MustCompile(
		`(?m)@(\w+)\.middleware\s*\(\s*["'](request|response)["']\s*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// starlette: Starlette(middleware=[...]) or app.add_middleware(Cls, ...)
	starletteMiddlewareKwargRe = regexp.MustCompile(
		`(?m)Starlette\s*\([^)]*middleware\s*=\s*\[`)
	starletteAddMiddlewareRe = regexp.MustCompile(
		`(?m)(\w+)\.add_middleware\s*\(\s*(\w+)`)
	// @app.middleware("http") decorator  — shared with FastAPI
	starletteMiddlewareDecoratorRe = regexp.MustCompile(
		`(?m)@(\w+)\.middleware\s*\(\s*["']http["']\s*\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// strawberry: strawberry.Schema(extensions=[MyExtension, ...])
	strawberrySchemaExtRe = regexp.MustCompile(
		`(?m)strawberry\.Schema\s*\([^)]*extensions\s*=\s*\[`)
	// GraphQL middleware list on execute
	strawberryMiddlewareRe = regexp.MustCompile(
		`(?m)middleware\s*=\s*\[\s*(\w+)`)

	// tornado: def prepare(self) inside a RequestHandler subclass
	tornadoPrepareRe = regexp.MustCompile(
		`(?m)^\s{4,}def\s+prepare\s*\(\s*self`)
	// tornado: Application(settings={'transforms': [...]}) / transform classes
	tornadoTransformRe = regexp.MustCompile(
		`(?m)(?:OutputTransform|GZipContentEncoding|ChunkedTransferEncoding)`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *HTTPMiddlewareExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_http_middleware")
	_, span := tracer.Start(ctx, "custom.python_http_middleware")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	var out []types.EntityRecord

	// -----------------------------------------------------------------------
	// aiohttp
	// -----------------------------------------------------------------------
	if strings.Contains(src, "aiohttp") || strings.Contains(src, "web.Application") {
		for _, idx := range allMatchesIndex(aiohttpMiddlewaresAssignRe, src) {
			appVar := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity(appVar+".middlewares", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "aiohttp", "pattern_type": "middleware_list", "app_var": appVar}))
		}
		for _, idx := range allMatchesIndex(aiohttpMiddlewaresKwargRe, src) {
			line := lineOf(src, idx[0])
			out = append(out, entity("web.Application.middlewares", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "aiohttp", "pattern_type": "middleware_list"}))
		}
	}

	// -----------------------------------------------------------------------
	// bottle
	// -----------------------------------------------------------------------
	if strings.Contains(src, "bottle") || strings.Contains(src, "Bottle") {
		for _, idx := range allMatchesIndex(bottleHookRe, src) {
			hookType := src[idx[2]:idx[3]]
			funcName := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "bottle", "pattern_type": "hook", "hook_type": hookType}))
		}
	}

	// -----------------------------------------------------------------------
	// cherrypy
	// -----------------------------------------------------------------------
	if strings.Contains(src, "cherrypy") {
		for _, idx := range allMatchesIndex(cherrypyToolAssignRe, src) {
			toolName := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity("cherrypy.tools."+toolName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "cherrypy", "pattern_type": "tool", "tool_name": toolName}))
		}
		for _, idx := range allMatchesIndex(cherrypyToolOnRe, src) {
			toolName := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity("cherrypy.tools."+toolName+".on", "SCOPE.Config", "",
				file.Path, line,
				map[string]string{"framework": "cherrypy", "pattern_type": "tool_enable", "tool_name": toolName}))
		}
	}

	// -----------------------------------------------------------------------
	// falcon
	// -----------------------------------------------------------------------
	if strings.Contains(src, "falcon") {
		if falconAppMiddlewareRe.MatchString(src) {
			line := lineOf(src, falconAppMiddlewareRe.FindStringIndex(src)[0])
			out = append(out, entity("falcon.App.middleware", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "falcon", "pattern_type": "middleware_list"}))
		}
		for _, idx := range allMatchesIndex(falconAddMiddlewareRe, src) {
			appVar := src[idx[2]:idx[3]]
			mwClass := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(mwClass, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "falcon", "pattern_type": "add_middleware", "app_var": appVar, "middleware": mwClass}))
		}
	}

	// -----------------------------------------------------------------------
	// hug
	// -----------------------------------------------------------------------
	if strings.Contains(src, "hug") {
		for _, idx := range allMatchesIndex(hugMiddlewareRe, src) {
			mwType := src[idx[2]:idx[3]]
			funcName := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "hug", "pattern_type": "middleware", "middleware_type": mwType}))
		}
	}

	// -----------------------------------------------------------------------
	// litestar
	// -----------------------------------------------------------------------
	if strings.Contains(src, "litestar") || strings.Contains(src, "Litestar") {
		if litestarMiddlewareKwargRe.MatchString(src) {
			line := lineOf(src, litestarMiddlewareKwargRe.FindStringIndex(src)[0])
			out = append(out, entity("Litestar.middleware", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "litestar", "pattern_type": "middleware_list"}))
		}
		for _, idx := range allMatchesIndex(litestarMiddlewareClassRe, src) {
			className := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity(className, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "litestar", "pattern_type": "middleware_class"}))
		}
	}

	// -----------------------------------------------------------------------
	// pyramid
	// -----------------------------------------------------------------------
	if strings.Contains(src, "pyramid") || strings.Contains(src, "Configurator") {
		for _, idx := range allMatchesIndex(pyramidAddTweenRe, src) {
			tweenPath := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity(tweenPath, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "pyramid", "pattern_type": "tween", "tween_factory": tweenPath}))
		}
	}

	// -----------------------------------------------------------------------
	// quart (Flask-compatible async — detects quart import + before/after hooks)
	// -----------------------------------------------------------------------
	if quartImportRe.MatchString(src) || strings.Contains(src, "Quart(") {
		for _, idx := range allMatchesIndex(quartHookRe, src) {
			appVar := src[idx[2]:idx[3]]
			hookType := src[idx[4]:idx[5]]
			funcName := src[idx[6]:idx[7]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "quart", "pattern_type": "request_hook", "hook_type": hookType, "app_var": appVar}))
		}
	}

	// -----------------------------------------------------------------------
	// robyn
	// -----------------------------------------------------------------------
	if strings.Contains(src, "robyn") || strings.Contains(src, "Robyn") {
		for _, idx := range allMatchesIndex(robynBeforeRe, src) {
			hookType := src[idx[4]:idx[5]]
			funcName := src[idx[6]:idx[7]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "robyn", "pattern_type": "lifecycle_hook", "hook_type": hookType}))
		}
		for _, idx := range allMatchesIndex(robynBeforeCallRe, src) {
			hookType := src[idx[4]:idx[5]]
			funcName := src[idx[6]:idx[7]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "robyn", "pattern_type": "lifecycle_hook_call", "hook_type": hookType}))
		}
	}

	// -----------------------------------------------------------------------
	// sanic
	// -----------------------------------------------------------------------
	if strings.Contains(src, "sanic") || strings.Contains(src, "Sanic") {
		for _, idx := range allMatchesIndex(sanicRegisterMiddlewareRe, src) {
			appVar := src[idx[2]:idx[3]]
			funcName := src[idx[4]:idx[5]]
			phase := src[idx[6]:idx[7]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "sanic", "pattern_type": "middleware", "phase": phase, "app_var": appVar}))
		}
		for _, idx := range allMatchesIndex(sanicMiddlewareDecoratorRe, src) {
			phase := src[idx[4]:idx[5]]
			funcName := src[idx[6]:idx[7]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "sanic", "pattern_type": "middleware_decorator", "phase": phase}))
		}
	}

	// -----------------------------------------------------------------------
	// starlette
	// -----------------------------------------------------------------------
	if strings.Contains(src, "starlette") || strings.Contains(src, "Starlette") {
		if starletteMiddlewareKwargRe.MatchString(src) {
			line := lineOf(src, starletteMiddlewareKwargRe.FindStringIndex(src)[0])
			out = append(out, entity("Starlette.middleware", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "starlette", "pattern_type": "middleware_list"}))
		}
		for _, idx := range allMatchesIndex(starletteAddMiddlewareRe, src) {
			appVar := src[idx[2]:idx[3]]
			mwClass := src[idx[4]:idx[5]]
			// skip fastapi.go's own add_middleware if fastapi also present — let fastapi.go own it
			if strings.Contains(src, "FastAPI") || strings.Contains(src, "fastapi") {
				continue
			}
			line := lineOf(src, idx[0])
			out = append(out, entity(mwClass, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "starlette", "pattern_type": "add_middleware", "app_var": appVar, "middleware": mwClass}))
		}
		for _, idx := range allMatchesIndex(starletteMiddlewareDecoratorRe, src) {
			appVar := src[idx[2]:idx[3]]
			funcName := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(funcName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "starlette", "pattern_type": "middleware_decorator", "app_var": appVar}))
		}
	}

	// -----------------------------------------------------------------------
	// strawberry-graphql
	// -----------------------------------------------------------------------
	if strings.Contains(src, "strawberry") {
		if strawberrySchemaExtRe.MatchString(src) {
			line := lineOf(src, strawberrySchemaExtRe.FindStringIndex(src)[0])
			out = append(out, entity("strawberry.Schema.extensions", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "strawberry-graphql", "pattern_type": "schema_extensions"}))
		}
		for _, idx := range allMatchesIndex(strawberryMiddlewareRe, src) {
			mwName := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity(mwName, "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "strawberry-graphql", "pattern_type": "graphql_middleware", "middleware": mwName}))
		}
	}

	// -----------------------------------------------------------------------
	// tornado
	// -----------------------------------------------------------------------
	if strings.Contains(src, "tornado") || strings.Contains(src, "RequestHandler") {
		for _, idx := range allMatchesIndex(tornadoPrepareRe, src) {
			line := lineOf(src, idx[0])
			out = append(out, entity("prepare", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "tornado", "pattern_type": "prepare_hook"}))
		}
		if tornadoTransformRe.MatchString(src) {
			line := lineOf(src, tornadoTransformRe.FindStringIndex(src)[0])
			out = append(out, entity("tornado.transform", "SCOPE.Pattern", "",
				file.Path, line,
				map[string]string{"framework": "tornado", "pattern_type": "transform_middleware"}))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
