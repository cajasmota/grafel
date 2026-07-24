package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extreg.Register("custom_js_express", &expressExtractor{})
}

type expressExtractor struct{}

func (e *expressExtractor) Language() string { return "custom_js_express" }

// Compiled regexes for Express.js extraction.
var (
	reExpressRoute = regexp.MustCompile(
		`(?i)(?:app|router|\w+)\s*\.\s*(get|post|put|delete|patch|all|options|head)\s*\(\s*['` + "`" + `"]([^'"` + "`" + ` ]+)['` + "`" + `"]`,
	)
	reExpressRouter = regexp.MustCompile(
		`(?:const|let|var)\s+(\w+)(?:\s*:\s*\w+)?\s*=\s*(?:express\.)?Router\s*\(\s*\)`,
	)
	reExpressMount = regexp.MustCompile(
		`(?:\w+)\s*\.\s*use\s*\(\s*['` + "`" + `"]([^'"` + "`" + ` ]+)['` + "`" + `"]\s*,\s*(\w+)`,
	)
	reExpressMiddleware = regexp.MustCompile(
		`(?:\w+)\s*\.\s*use\s*\(\s*([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:\s*\([^)]*\))?)`,
	)
	reExpressErrorHandler = regexp.MustCompile(
		`(?:function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function\s*)?)` +
			`\s*\(\s*\w+(?:\s*:\s*\w+)?\s*,\s*\w+(?:\s*:\s*\w+)?\s*,\s*\w+(?:\s*:\s*\w+)?\s*,\s*next\b`,
	)
	reExpressInlineError = regexp.MustCompile(
		`(?:\w+)\s*\.\s*use\s*\(\s*(?:async\s+)?function\s*\(\s*\w+(?:\s*:\s*\w+)?\s*,\s*\w+(?:\s*:\s*\w+)?\s*,\s*\w+(?:\s*:\s*\w+)?\s*,\s*next\b`,
	)
	reExpressStatic = regexp.MustCompile(
		`express\s*\.\s*static\s*\(\s*([^)]+)\)`,
	)
	reExpressSet = regexp.MustCompile(
		"(?:\\w+)\\s*\\.\\s*set\\s*\\(\\s*['\"`]([^'\"`]+)['\"`]",
	)
	reExpressEngine = regexp.MustCompile(
		"(?:\\w+)\\s*\\.\\s*engine\\s*\\(\\s*['\"`]([^'\"`]+)['\"`]",
	)
	reExpressPassport = regexp.MustCompile(
		`passport\s*\.\s*use\s*\(\s*(?:new\s+)?([A-Z][A-Za-z0-9_]*(?:Strategy|Auth)?)\s*\(`,
	)
	reExpressPassportNamed = regexp.MustCompile(
		"passport\\s*\\.\\s*use\\s*\\(\\s*['\"`]([^'\"` ]+)['\"`]\\s*,",
	)
	reExpressSocketIO = regexp.MustCompile(
		"(?:\\w+)\\s*\\.\\s*on\\s*\\(\\s*['\"`](connection|disconnect|[a-z_][a-z0-9_:]*)['\"`]",
	)
	reExpressParam = regexp.MustCompile(
		"(?:\\w+)\\s*\\.\\s*param\\s*\\(\\s*['\"`]([^'\"` ]+)['\"`]",
	)
	// reExpressTypedReq matches a TypeScript `Request<P, ResBody, ReqBody>`
	// handler-parameter annotation. Express types the request-body DTO as the
	// THIRD generic argument and the response-body DTO as the SECOND. Untyped
	// `req.body` handlers carry no Request<...> annotation → no edge
	// (honest-partial). #3629/#3607 endpoint→DTO.
	reExpressTypedReq = regexp.MustCompile(
		`:\s*Request\s*<\s*([^,<>]*)\s*,\s*([^,<>]*)\s*,\s*([A-Za-z_][A-Za-z0-9_]*)`,
	)
	reExpressResGeneric = regexp.MustCompile(
		`:\s*Response\s*<\s*([A-Za-z_][A-Za-z0-9_]*)`,
	)
)

// expressSkipDTOTypes are built-ins that never represent a request/response DTO.
var expressSkipDTOTypes = map[string]bool{
	"any": true, "unknown": true, "object": true, "Object": true,
	"string": true, "number": true, "boolean": true, "void": true,
	"null": true, "undefined": true, "never": true,
}

func (e *expressExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.express_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "express"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.SourceFile)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. Router instantiation
	routerVars := make(map[string]bool)
	for _, m := range reExpressRouter.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		routerVars[name] = true
		ent := makeEntity(name, "SCOPE.Component", "router", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "provenance", "INFERRED_FROM_EXPRESS_ROUTER")
		addEntity(ent)
	}

	// 2. Mount points
	for _, m := range reExpressMount.FindAllStringSubmatchIndex(src, -1) {
		mountPath := src[m[2]:m[3]]
		routerVar := src[m[4]:m[5]]
		if !routerVars[routerVar] {
			continue
		}
		name := fmt.Sprintf("mount:%s->%s", mountPath, routerVar)
		ent := makeEntity(name, "SCOPE.Pattern", "mount", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "mount_path", mountPath, "router_var", routerVar,
			"provenance", "INFERRED_FROM_EXPRESS_MOUNT")
		addEntity(ent)
	}

	// 3. Route handlers
	for _, m := range reExpressRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "http_method", method, "route_path", path,
			"provenance", "INFERRED_FROM_EXPRESS_ROUTE")

		// endpoint→DTO edges (#3629/#3607), TypeScript only. The handler arrow
		// function follows the path on the same call; scan the window up to the
		// arrow `=>` for a `Request<P, ResBody, ReqBody>` annotation. Untyped
		// `req.body` handlers produce no annotation → no edge (honest-partial).
		if lang == "typescript" {
			window := src[m[5]:min(m[5]+240, len(src))]
			if arrow := strings.Index(window, "=>"); arrow >= 0 {
				window = window[:arrow]
			}
			if rm := reExpressTypedReq.FindStringSubmatch(window); rm != nil {
				if dto := strings.TrimSpace(rm[3]); dto != "" && !expressSkipDTOTypes[dto] {
					ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
						ToID: "Class:" + dto,
						Kind: string(types.RelationshipKindAcceptsInput),
						Properties: types.Props{
							{K: "dto_type", V: dto},
							{K: "framework", V: "express"},
							{K: "match_source", V: "request_generic"},
						},
					})
				}
				if resBody := strings.TrimSpace(rm[2]); resBody != "" && !expressSkipDTOTypes[resBody] {
					ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
						ToID: "Class:" + resBody,
						Kind: string(types.RelationshipKindReturns),
						Properties: types.Props{
							{K: "dto_type", V: resBody},
							{K: "framework", V: "express"},
							{K: "match_source", V: "request_generic_resbody"},
						},
					})
				}
			}
			if rm := reExpressResGeneric.FindStringSubmatch(window); rm != nil {
				if dto := strings.TrimSpace(rm[1]); dto != "" && !expressSkipDTOTypes[dto] {
					ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
						ToID: "Class:" + dto,
						Kind: string(types.RelationshipKindReturns),
						Properties: types.Props{
							{K: "dto_type", V: dto},
							{K: "framework", V: "express"},
							{K: "match_source", V: "response_generic"},
						},
					})
				}
			}
		}
		addEntity(ent)
	}

	// 4. Error handlers (named)
	errorHandlerNames := make(map[string]bool)
	for _, m := range reExpressErrorHandler.FindAllStringSubmatchIndex(src, -1) {
		var name string
		if m[2] >= 0 {
			name = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = src[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		errorHandlerNames[name] = true
		ent := makeEntity(name, "SCOPE.Pattern", "error_handler", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "param_count", "4",
			"provenance", "INFERRED_FROM_EXPRESS_ERROR_HANDLER")
		addEntity(ent)
	}

	// 4b. Inline anonymous error handlers
	inlineCount := 0
	for _, m := range reExpressInlineError.FindAllStringIndex(src, -1) {
		inlineCount++
		name := fmt.Sprintf("errorHandler_%d", inlineCount)
		ent := makeEntity(name, "SCOPE.Pattern", "error_handler", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "param_count", "4", "inline", "true",
			"provenance", "INFERRED_FROM_EXPRESS_ERROR_HANDLER")
		addEntity(ent)
	}

	// 5. Middleware (app.use without path prefix)
	for _, m := range reExpressMiddleware.FindAllStringSubmatchIndex(src, -1) {
		expr := strings.TrimSpace(src[m[2]:m[3]])
		baseName := strings.FieldsFunc(expr, func(r rune) bool { return r == '(' || r == ' ' })[0]
		if errorHandlerNames[baseName] || routerVars[baseName] {
			continue
		}
		// Prefix the standalone middleware entity name with "middleware:" so it
		// does NOT collide in the symbol table with the real middleware
		// function/factory symbol of the same bare name (#4380). Before this, a
		// bare `app.use(authMiddleware)` emitted a SCOPE.Pattern entity literally
		// named `authMiddleware`, which made the resolver see two `authMiddleware`
		// entities and treat the name as ambiguous — so the app→middleware USES
		// edge could never bind to the real function.
		name := "middleware:" + expr
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "provenance", "INFERRED_FROM_EXPRESS_MIDDLEWARE",
			"middleware_expr", expr)
		addEntity(ent)
	}

	// 6. express.static()
	for _, m := range reExpressStatic.FindAllStringSubmatchIndex(src, -1) {
		arg := strings.TrimFunc(src[m[2]:m[3]], isQuoteOrSpace)
		cleanArg := strings.Split(arg, ",")[0]
		cleanArg = strings.TrimFunc(cleanArg, isQuoteOrSpace)
		var name string
		if cleanArg != "" {
			name = "static:" + cleanArg
		} else {
			name = "static"
		}
		ent := makeEntity(name, "SCOPE.Pattern", "static", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "static_path", cleanArg,
			"provenance", "INFERRED_FROM_EXPRESS_STATIC")
		addEntity(ent)
	}

	// 7. app.set / app.engine
	for _, m := range reExpressSet.FindAllStringSubmatchIndex(src, -1) {
		key := src[m[2]:m[3]]
		name := "set:" + key
		ent := makeEntity(name, "SCOPE.Pattern", "config", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "config_kind", "set", "key", key,
			"provenance", "INFERRED_FROM_EXPRESS_CONFIG")
		addEntity(ent)
	}
	for _, m := range reExpressEngine.FindAllStringSubmatchIndex(src, -1) {
		eng := src[m[2]:m[3]]
		name := "engine:" + eng
		ent := makeEntity(name, "SCOPE.Pattern", "config", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "config_kind", "engine", "engine", eng,
			"provenance", "INFERRED_FROM_EXPRESS_CONFIG")
		addEntity(ent)
	}

	// 8. Passport strategies
	for _, m := range reExpressPassport.FindAllStringSubmatchIndex(src, -1) {
		cls := src[m[2]:m[3]]
		name := "passport:" + cls
		ent := makeEntity(name, "SCOPE.Pattern", "passport", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "strategy_class", cls,
			"provenance", "INFERRED_FROM_EXPRESS_PASSPORT")
		addEntity(ent)
	}
	for _, m := range reExpressPassportNamed.FindAllStringSubmatchIndex(src, -1) {
		sname := src[m[2]:m[3]]
		name := "passport:" + sname
		ent := makeEntity(name, "SCOPE.Pattern", "passport", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "strategy_name", sname,
			"provenance", "INFERRED_FROM_EXPRESS_PASSPORT")
		addEntity(ent)
	}

	// 9. Socket.io
	for _, m := range reExpressSocketIO.FindAllStringSubmatchIndex(src, -1) {
		event := src[m[2]:m[3]]
		name := "socket:" + event
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "event", event,
			"provenance", "INFERRED_FROM_EXPRESS_SOCKETIO")
		addEntity(ent)
	}

	// 10. app.param()
	for _, m := range reExpressParam.FindAllStringSubmatchIndex(src, -1) {
		param := src[m[2]:m[3]]
		name := "param:" + param
		ent := makeEntity(name, "SCOPE.Pattern", "param_middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "express", "param", param,
			"provenance", "INFERRED_FROM_EXPRESS_PARAM")
		addEntity(ent)
	}

	// 11. Global middleware wiring (#4380): app.use(...) registers cross-cutting
	// middleware/routers app-wide. Emit a synthetic `app` entity that owns an
	// app → middleware USES edge for each registration (global=true, di_role,
	// 0-based order). The edge targets resolve to the real middleware function /
	// router entity through the cross-file symbol table. Generalizes #4329.
	globalEdges := extractExpressGlobalMiddleware(src, routerVars)
	if len(globalEdges) > 0 && expressHasGlobalMiddleware(src) {
		appEnt := makeEntity(expressAppEntityName, "SCOPE.Pattern", "application",
			file.Path, file.Language, 1)
		setProps(&appEnt, "framework", "express",
			"provenance", "INFERRED_FROM_EXPRESS_BOOTSTRAP")
		addEntity(appEnt)

		byName := make(map[string]int, len(entities))
		for i := range entities {
			if _, ok := byName[entities[i].Name]; !ok {
				byName[entities[i].Name] = i
			}
		}
		for owner, rels := range globalEdges {
			if idx, ok := byName[owner]; ok {
				entities[idx].Relationships = append(entities[idx].Relationships, rels...)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
