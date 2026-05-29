package python

// http_reqresp_generic.go — generic Python HTTP dto_extraction + request_validation
// extractor for 13 non-FastAPI/Flask frameworks plus Django.
//
// Capabilities covered:
//   - dto_extraction: Pydantic-annotated function parameters (BaseModel subclass
//     type hints) used as request-body DTOs in any of the 14 framework route/handler
//     functions. Marshmallow schema.load() call sites in handler bodies are also
//     detected as DTO acceptance evidence.
//   - request_validation: Django Form.is_valid() + cleaned_data patterns, DRF
//     request.data + .validated_data + serializer.is_valid() patterns, and Pydantic
//     model_validate / parse_obj / from_orm calls in handler bodies.
//
// Issue #3185 — Python generic HTTP dto_extraction + request_validation extractor
// (13 frameworks + Django).

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
	extractor.Register("python_http_reqresp_generic", &HTTPReqRespGenericExtractor{})
}

// HTTPReqRespGenericExtractor detects:
//
//  1. Pydantic BaseModel type-hinted parameters in route/view/handler functions
//     across 14 Python web frameworks (dto_extraction).
//
//  2. Django Form / ModelForm .is_valid() + .cleaned_data usage for request
//     validation evidence (request_validation).
//
//  3. DRF request.data / .validated_data / serializer.is_valid() patterns
//     (request_validation for django-drf).
//
//  4. Generic Pydantic model_validate / parse_obj / parse_raw / from_orm calls
//     in any handler body (request_validation — framework-agnostic).
//
//  5. Marshmallow Schema.load() call sites in handler bodies (dto_extraction
//     corroboration for frameworks that use marshmallow as their validation layer).
type HTTPReqRespGenericExtractor struct{}

func (e *HTTPReqRespGenericExtractor) Language() string { return "python_http_reqresp_generic" }

// ─── framework imports that gate extraction ───────────────────────────────────

// genericFrameworkImportRe matches any import of the 14 target frameworks.
var genericFrameworkImportRe = regexp.MustCompile(
	`(?m)(?:import|from)\s+(?:aiohttp|bottle|cherrypy|falcon|hug|litestar|pyramid|quart|robyn|sanic|starlette|strawberry|tornado|django)`)

// ─── Pydantic / BaseModel detection ──────────────────────────────────────────

// genericPydanticParamRe matches `paramName: TypeHint` in a function signature
// where TypeHint could be a Pydantic model. Matches both plain and Optional[T]
// annotations. False positives are filtered by ghrSkipTypes.
var genericPydanticParamRe = regexp.MustCompile(
	`(?:^|[,(\s])(\w+)\s*:\s*([\w\[\], |.]+?)(?:\s*(?:=\s*[^,)]+)?\s*(?:,|\)|$))`)

// genericFuncDefRe matches any function/method definition that looks like a
// view/handler (we match broadly and let body-based heuristics filter).
var genericFuncDefRe = regexp.MustCompile(
	`(?m)(?:async\s+)?def\s+(\w+)\s*\(`)

// ─── handler markers for each framework ──────────────────────────────────────

// aiohttp: web.RouteTableDef or app.router.add_* or @routes.get/post/...
var aiohttpRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)*(?:get|post|put|patch|delete|head|options|route)\s*\(\s*(?:r)?["'][^"']*["']`)

// bottle: @app.route or @bottle.route or @app.get/post/...
var bottleRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)*(?:route|get|post|put|delete|patch)\s*\(\s*(?:r)?["'][^"']*["']`)

// cherrypy: @cherrypy.expose
var cherrypyExposeRe = regexp.MustCompile(`(?m)@(?:\w+\.)*expose\b`)

// falcon: on_get / on_post / on_put / on_delete methods on Resource classes
var falconMethodRe = regexp.MustCompile(
	`(?m)def\s+on_(?:get|post|put|patch|delete|head|options)\s*\(`)

// hug: @hug.get / @hug.post / etc.
var hugRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)*(?:get|post|put|patch|delete|head|options|cli)\s*\(\s*(?:r)?["'][^"']*["']`)

// litestar: @get / @post / @put / @patch / @delete (Litestar decorators)
var litestarRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)?(?:get|post|put|patch|delete)\s*\(\s*(?:r)?["'][^"']*["']`)

// pyramid: @view_config
var pyramidViewRe = regexp.MustCompile(`(?m)@(?:\w+\.)*view_config\b`)

// quart: @app.route / @app.get / etc. (mirrors Flask/aiohttp pattern)
var quartRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)*(?:route|get|post|put|patch|delete)\s*\(\s*(?:r)?["'][^"']*["']`)

// robyn: @app.get / @app.post / etc.
var robynRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)*(?:get|post|put|patch|delete|head|options)\s*\(\s*(?:r)?["'][^"']*["']`)

// sanic: @app.get / @app.route / @bp.get / etc.
var sanicRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)*(?:get|post|put|patch|delete|head|options|route|websocket)\s*\(\s*(?:r)?["'][^"']*["']`)

// starlette: @app.route / Mount / add_route (function-based); Route in routes=
var starletteRouteRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)*(?:route|get|post|put|patch|delete|head|options)\s*\(\s*(?:r)?["'][^"']*["']`)

// strawberry-graphql: @strawberry.type / @strawberry.mutation / @strawberry.query
var strawberryTypeRe = regexp.MustCompile(
	`(?m)@(?:\w+\.)?(?:type|input|interface|mutation|query|subscription)\b`)

// tornado: class handler(tornado.web.RequestHandler) + HTTP method defs
var tornadoHandlerRe = regexp.MustCompile(
	`(?m)class\s+\w+\s*\([^)]*(?:RequestHandler|web\.RequestHandler)[^)]*\)\s*:`)

// django forms/views: generic handler / CBV method
var djangoFormViewRe = regexp.MustCompile(
	`(?m)def\s+(?:get|post|put|patch|delete|dispatch)\s*\(\s*self`)

// ─── DTO / validation pattern regexes ─────────────────────────────────────────

// pydanticDTOParamRe matches `name: TypeAnnotation` in a parameter list and
// captures name + type. The leading `(?m)` anchor is NOT used here — we match
// on extracted parameter blocks, not raw file content.
var ghrPydanticParamRe = regexp.MustCompile(
	`\b(\w+)\s*:\s*([\w\[\], |.]+?)(?:\s*(?:=\s*[^,)]+)?\s*(?:,|\)|$))`)

// ghrSkipTypes contains bare Python primitives and framework request objects
// that are never Pydantic DTOs.
var ghrSkipTypes = map[string]bool{
	"int": true, "str": true, "float": true, "bool": true, "bytes": true,
	"None": true, "dict": true, "list": true, "set": true, "tuple": true,
	"Any": true, "object": true, "Optional": true, "List": true, "Dict": true,
	"Set": true, "Tuple": true, "Union": true, "Sequence": true,
	// framework request/response objects — never a body DTO
	"Request": true, "Response": true, "WebSocket": true, "HTTPConnection": true,
	"BackgroundTasks": true, "HTTPException": true, "status": true,
	// falcon-specific
	"req": true, "resp": true, "Request_": true,
	// tornado
	"self": true, "cls": true, "args": true, "kwargs": true,
	// common generic names that are too ambiguous
	"data": true, "body": true, "payload": true, "context": true, "ctx": true,
	// Pydantic internals — the *model class* is not itself a param type here
	"BaseModel": true, "BaseSettings": true, "RootModel": true,
	"datetime": true, "date": true, "time": true, "UUID": true, "Decimal": true,
	"Path": true, "HTTPAuthorizationCredentials": true, "Header": true,
	"Query": true, "Cookie": true, "Form": true, "File": true, "UploadFile": true,
}

// ghrInjectionRe detects framework dependency-injection tokens that should not
// be treated as body DTOs.
var ghrInjectionRe = regexp.MustCompile(
	`(?:Depends|Inject|Query|Path|Header|Cookie|Body|File|Form|Security|resolve)\s*\(`)

// Django form validation: form_var.is_valid()  / form.cleaned_data
var djangoIsValidRe = regexp.MustCompile(`\b(\w+)\s*\.\s*is_valid\s*\(\s*\)`)
var djangoCleanedDataRe = regexp.MustCompile(`\b(\w+)\s*\.\s*cleaned_data\b`)

// Django Form class definition (evidence that form patterns exist in file)
var djangoFormClassRe = regexp.MustCompile(
	`(?m)^class\s+(\w+)\s*\([^)]*(?:Form|ModelForm|BaseForm)[^)]*\)\s*:`)

// DRF / Django REST Framework validation evidence
var drfRequestDataRe = regexp.MustCompile(`\brequest\s*\.\s*(?:data|query_params|FILES)\b`)
var drfValidatedDataRe = regexp.MustCompile(`\b(\w+)\s*\.\s*validated_data\b`)
var drfIsValidRe = regexp.MustCompile(`\b(\w+)\s*\.\s*is_valid\s*\(`)

// Pydantic model_validate / parse_obj / parse_raw used directly in a handler body
var ghrPydanticCallRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*(?:model_validate|parse_obj|parse_raw|from_orm|model_validate_json)\s*\(`)

// Marshmallow schema.load() in handler body (dto_extraction corroboration)
var ghrMarshmallowLoadRe = regexp.MustCompile(`(\w+)(?:\(\))?\s*\.\s*load\s*\(`)

// ─── helpers ─────────────────────────────────────────────────────────────────

// ghrGenericNames are variable names that are too generic to be informative DTO names
// when used as schema/marshmallow load targets. Note: "form" is intentionally NOT
// included here because Django form.is_valid() on a variable named "form" is
// exactly the pattern we want to detect.
var ghrGenericNames = map[string]bool{
	"schema": true, "self": true, "cls": true, "request": true, "response": true,
	"data": true, "payload": true, "body": true, "json": true,
	"args": true, "kwargs": true, "result": true, "output": true, "input": true,
	"obj": true, "instance": true, "db": true, "g": true, "session": true,
	"serializer": true, "s": true,
}

// ─── extractor ────────────────────────────────────────────────────────────────

func (e *HTTPReqRespGenericExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_http_reqresp_generic")
	_, span := tracer.Start(ctx, "custom.python_http_reqresp_generic")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)

	// Gate: only process files that reference one of the 14 target frameworks
	// OR Django/DRF (which share the django import).
	if !genericFrameworkImportRe.MatchString(source) {
		return nil, nil
	}

	var out []types.EntityRecord
	seenDTOs := make(map[string]bool)
	seenValidation := make(map[string]bool)

	// detectFramework returns the primary framework label for a file based on imports.
	framework := detectFrameworkLabel(source)

	emitDTO := func(dtoName, fw string, line int) {
		key := fw + ":" + dtoName
		if seenDTOs[key] {
			return
		}
		seenDTOs[key] = true
		out = append(out, entity(
			"dto_"+dtoName, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":    fw,
				"pattern_type": "request_dto",
				"dto_type":     dtoName,
				"via":          "dto_extraction",
			},
		))
	}

	emitValidation := func(key, fw, validationKind string, line int) {
		uniq := fw + ":" + key
		if seenValidation[uniq] {
			return
		}
		seenValidation[uniq] = true
		out = append(out, entity(
			"reqval_"+key, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":       fw,
				"pattern_type":    "request_validation",
				"validation_kind": validationKind,
				"via":             "request_validation",
			},
		))
	}

	// ── Pass 1: Pydantic-annotated parameters in function definitions ──────────
	//
	// Walk every function definition in the file.  For each one, extract the
	// parameters block and check for type-annotated params whose type is not a
	// known primitive / framework injection.  Only process the function when it
	// is decorated with a framework-specific route decorator OR is a handler-
	// shaped method (falcon on_*, tornado HTTP methods, django get/post).
	type funcDef struct {
		name         string
		paramStart   int // byte offset of the opening paren
		decorBefore  string
		matchStart   int
	}

	var funcs []funcDef
	for _, idx := range allMatchesIndex(genericFuncDefRe, source) {
		name := source[idx[2]:idx[3]]
		// idx[1] is the end of the full match; the full regex ends with \(, so
		// idx[1] points to the first byte inside the parameter list (after the
		// opening paren).  This is the offset extractParamsBlock expects.
		paramStart := idx[1]
		// collect preceding decorators (up to 500 bytes before def)
		decorStart := 0
		if idx[0] > 500 {
			decorStart = idx[0] - 500
		}
		decorBefore := source[decorStart:idx[0]]
		funcs = append(funcs, funcDef{name, paramStart, decorBefore, idx[0]})
	}

	for i, fn := range funcs {
		if !isHandlerFunction(fn.name, fn.decorBefore, source, framework) {
			continue
		}

		paramsBlock, closeOffset := extractParamsBlock(source, fn.paramStart)
		line := lineOf(source, fn.matchStart)

		// ── DTO extraction: Pydantic params ──────────────────────────────────
		for _, pm := range ghrPydanticParamRe.FindAllStringSubmatch(paramsBlock, -1) {
			paramName := pm[1]
			typeRaw := pm[2]
			if paramName == "self" || paramName == "cls" {
				continue
			}
			// Skip framework injection tokens
			if ghrParamIsInjected(paramName, paramsBlock) {
				continue
			}
			dtoName := unwrapType(typeRaw)
			if dtoName == "" || ghrSkipTypes[dtoName] {
				continue
			}
			emitDTO(dtoName, framework, line)
			// Also emit an ACCEPTS_INPUT operation entity
			out = append(out, entity(
				fn.name+":accepts:"+dtoName,
				string(types.EntityKindOperation), "endpoint",
				file.Path, line,
				map[string]string{
					"framework":    framework,
					"pattern_type": "accepts_input",
					"param_name":   paramName,
					"dto_type":     dtoName,
					"via":          "dto_extraction",
				},
			))
		}

		// ── request_validation: Pydantic model_validate / parse_obj calls ────
		bodyEnd := len(source)
		if i+1 < len(funcs) {
			bodyEnd = funcs[i+1].matchStart
		}
		colonPos := strings.Index(source[closeOffset:min(bodyEnd, len(source))], ":")
		if colonPos == -1 {
			continue
		}
		bodyStart := closeOffset + colonPos + 1
		bodyRegion := source[bodyStart:min(bodyStart+3000, bodyEnd)]

		for _, m := range ghrPydanticCallRe.FindAllStringSubmatch(bodyRegion, -1) {
			modelName := m[1]
			if ghrSkipTypes[modelName] || ghrGenericNames[strings.ToLower(modelName)] {
				continue
			}
			bodyLine := lineOf(source, bodyStart) + strings.Count(bodyRegion[:strings.Index(bodyRegion, m[0])], "\n")
			emitValidation("pydantic_call:"+modelName, framework, "pydantic_model_validate", bodyLine)
		}

		// ── request_validation: Django form.is_valid() ────────────────────────
		if strings.Contains(framework, "django") {
			for _, m := range djangoIsValidRe.FindAllStringSubmatch(bodyRegion, -1) {
				varName := m[1]
				if ghrGenericNames[strings.ToLower(varName)] {
					continue
				}
				bodyLine := lineOf(source, bodyStart)
				emitValidation("form_is_valid:"+varName, framework, "django_form_is_valid", bodyLine)
			}
			for _, m := range djangoCleanedDataRe.FindAllStringSubmatch(bodyRegion, -1) {
				varName := m[1]
				if ghrGenericNames[strings.ToLower(varName)] {
					continue
				}
				bodyLine := lineOf(source, bodyStart)
				emitValidation("cleaned_data:"+varName, framework, "django_form_cleaned_data", bodyLine)
			}

			// DRF: request.data / .validated_data / .is_valid()
			if drfRequestDataRe.MatchString(bodyRegion) {
				bodyLine := lineOf(source, bodyStart)
				emitValidation("drf_request_data", framework, "drf_request_data", bodyLine)
			}
			for _, m := range drfValidatedDataRe.FindAllStringSubmatch(bodyRegion, -1) {
				varName := m[1]
				if ghrGenericNames[strings.ToLower(varName)] {
					continue
				}
				bodyLine := lineOf(source, bodyStart)
				emitValidation("drf_validated_data:"+varName, framework, "drf_validated_data", bodyLine)
			}
			for _, m := range drfIsValidRe.FindAllStringSubmatch(bodyRegion, -1) {
				varName := m[1]
				if ghrGenericNames[strings.ToLower(varName)] {
					continue
				}
				bodyLine := lineOf(source, bodyStart)
				emitValidation("drf_is_valid:"+varName, framework, "drf_serializer_is_valid", bodyLine)
			}
		}

		// ── dto_extraction: marshmallow schema.load() ─────────────────────────
		for _, sm := range ghrMarshmallowLoadRe.FindAllStringSubmatch(bodyRegion, -1) {
			schemaVar := sm[1]
			if ghrGenericNames[strings.ToLower(schemaVar)] || ghrSkipTypes[schemaVar] {
				continue
			}
			canonical := canonicalSchemaName(schemaVar)
			if canonical == "" {
				continue
			}
			emitDTO(canonical, framework, lineOf(source, bodyStart))
		}
	}

	// ── Pass 2: file-level Django Form class definitions ──────────────────────
	// A file containing a Form/ModelForm class definition is proof of
	// dto_extraction capability even without a handler being detected.
	if strings.Contains(framework, "django") {
		for _, idx := range allMatchesIndex(djangoFormClassRe, source) {
			className := source[idx[2]:idx[3]]
			line := lineOf(source, idx[0])
			emitDTO(className, framework, line)
		}
		// Also emit a file-level request_validation marker if the file contains
		// DRF serializer.is_valid() or request.data patterns at module scope
		// (not necessarily inside a detected handler).
		if drfRequestDataRe.MatchString(source) {
			emitValidation("drf_request_data_file", framework, "drf_request_data", 1)
		}
		for _, m := range drfIsValidRe.FindAllStringSubmatch(source, -1) {
			varName := m[1]
			if !ghrGenericNames[strings.ToLower(varName)] && !ghrSkipTypes[varName] {
				emitValidation("drf_is_valid_file:"+varName, framework, "drf_serializer_is_valid", 1)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ─── framework label detection ────────────────────────────────────────────────

// detectFrameworkLabel returns the primary framework label for a file based on
// import analysis.  The label matches the framework id used in coverage records
// (lowercase, hyphen-separated where needed).
func detectFrameworkLabel(source string) string {
	switch {
	case strings.Contains(source, "import aiohttp") || strings.Contains(source, "from aiohttp"):
		return "aiohttp"
	case strings.Contains(source, "import bottle") || strings.Contains(source, "from bottle"):
		return "bottle"
	case strings.Contains(source, "import cherrypy") || strings.Contains(source, "from cherrypy"):
		return "cherrypy"
	case strings.Contains(source, "import falcon") || strings.Contains(source, "from falcon"):
		return "falcon"
	case strings.Contains(source, "import hug") || strings.Contains(source, "from hug"):
		return "hug"
	case strings.Contains(source, "import litestar") || strings.Contains(source, "from litestar"):
		return "litestar"
	case strings.Contains(source, "import pyramid") || strings.Contains(source, "from pyramid"):
		return "pyramid"
	case strings.Contains(source, "import quart") || strings.Contains(source, "from quart"):
		return "quart"
	case strings.Contains(source, "import robyn") || strings.Contains(source, "from robyn"):
		return "robyn"
	case strings.Contains(source, "import sanic") || strings.Contains(source, "from sanic"):
		return "sanic"
	case strings.Contains(source, "import starlette") || strings.Contains(source, "from starlette"):
		return "starlette"
	case strings.Contains(source, "import strawberry") || strings.Contains(source, "from strawberry"):
		return "strawberry-graphql"
	case strings.Contains(source, "import tornado") || strings.Contains(source, "from tornado"):
		return "tornado"
	case strings.Contains(source, "import django") || strings.Contains(source, "from django"):
		return "django"
	default:
		return "python"
	}
}

// ─── handler-function heuristics ─────────────────────────────────────────────

// isHandlerFunction returns true if the function definition is a likely
// HTTP/view handler based on its name, preceding decorators, and framework.
func isHandlerFunction(name, decorBefore, source, framework string) bool {
	// Falcon: on_get / on_post / etc.
	if strings.HasPrefix(name, "on_") &&
		(strings.Contains(name, "get") || strings.Contains(name, "post") ||
			strings.Contains(name, "put") || strings.Contains(name, "patch") ||
			strings.Contains(name, "delete") || strings.Contains(name, "head") ||
			strings.Contains(name, "options")) {
		return true
	}
	// Tornado: get / post / put / delete / patch handler methods
	if framework == "tornado" &&
		(name == "get" || name == "post" || name == "put" ||
			name == "patch" || name == "delete" || name == "head") {
		return true
	}
	// Django CBV HTTP methods and function-based views
	if framework == "django" &&
		(name == "get" || name == "post" || name == "put" ||
			name == "patch" || name == "delete" || name == "dispatch") {
		return true
	}
	// Django FBV: function whose name contains "view" (e.g. contact_view, login_view)
	if framework == "django" && strings.HasSuffix(name, "_view") {
		return true
	}
	// Pyramid: @view_config
	if pyramidViewRe.MatchString(decorBefore) {
		return true
	}
	// Strawberry: @strawberry.mutation / @strawberry.query
	if framework == "strawberry-graphql" && strawberryTypeRe.MatchString(decorBefore) {
		return true
	}
	// Generic decorator patterns: @app.get/post/..., @router.get/post/..., @routes.get/...
	// This covers litestar @post/@get, sanic @app.post, quart @app.post, bottle @app.post,
	// aiohttp @routes.get, robyn @app.get, starlette @app.route etc.
	genericDecoRe := regexp.MustCompile(
		`@(?:\w+\.)*(?:get|post|put|patch|delete|head|options|route|websocket)\s*\(`)
	if genericDecoRe.MatchString(decorBefore) {
		return true
	}
	// cherrypy: @expose
	if cherrypyExposeRe.MatchString(decorBefore) {
		return true
	}
	// hug: @hug.get/post/etc
	if hugRouteRe.MatchString(decorBefore) {
		return true
	}
	// Starlette / aiohttp: function referenced in app.router.add_* or Route(...) patterns
	// Without a decorator, look for the function name appearing after add_route/add_post/Route.
	if framework == "starlette" || framework == "aiohttp" {
		routeRefRe := regexp.MustCompile(`(?:add_(?:route|get|post|put|patch|delete)|Route)\s*\([^)]*\b` + regexp.QuoteMeta(name) + `\b`)
		if routeRefRe.MatchString(source) {
			return true
		}
		// Also: any function with recognisable handler suffixes
		if strings.HasSuffix(name, "_handler") || strings.HasSuffix(name, "_view") ||
			strings.HasSuffix(name, "_endpoint") || strings.HasSuffix(name, "_route") {
			return true
		}
	}
	return false
}

// ─── injection detection ──────────────────────────────────────────────────────

var ghrInjectionPattern = regexp.MustCompile(
	`(?:Depends|Inject|Query|Path|Header|Cookie|Body|File|Form|Security|resolve)\s*\(`)

func ghrParamIsInjected(paramName, paramsBlock string) bool {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(paramName) + `\s*(?::[^,=]+)?\s*=\s*` + ghrInjectionPattern.String())
	return pattern.MatchString(paramsBlock)
}
