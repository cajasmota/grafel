package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_nestjs", &nestjsExtractor{})
}

type nestjsExtractor struct{}

func (e *nestjsExtractor) Language() string { return "custom_js_nestjs" }

var (
	reNestModule = regexp.MustCompile(
		`@Module\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestController = regexp.MustCompile(
		`@Controller\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestInjectable = regexp.MustCompile(
		`@Injectable\s*\([^)]*\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	// reNestHTTPMethod matches a verb decorator and the handler method name.
	// Real NestJS handlers stack additional decorators between the verb
	// decorator and the method signature — `@HttpCode(HttpStatus.OK)`,
	// `@UseGuards(...)`, `@Header(...)`, `@RequirePage(...)` — so we skip a run
	// of intervening decorators (each optionally taking a single-level
	// argument list) plus visibility/async modifiers before the method name.
	// Before #4325 the regex required the verb decorator to be immediately
	// adjacent to the method, so EVERY @Post/@Patch that set @HttpCode (the
	// dominant POST idiom) was silently dropped from the endpoint inventory.
	reNestHTTPMethod = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch|Options|Head|All)\s*\(([^)]*)\)\s*(?:@[A-Za-z_]\w*(?:\s*\([^()]*\))?\s*)*(?:public\s+|private\s+|protected\s+|async\s+)*(\w+)\s*\(`,
	)
	// reNestBodyParam captures a @Body()-decorated handler parameter and its DTO
	// type: `@Body() dto: CreateUserDto`. Plain @Body() with no type yields no
	// edge (honest-partial). #3629/#3607 endpoint→DTO ACCEPTS_INPUT.
	reNestBodyParam = regexp.MustCompile(
		`@Body\s*\([^)]*\)\s*\w+\s*:\s*([A-Za-z_][A-Za-z0-9_]*)`,
	)
	// reNestReturnType captures the method return-type annotation that follows the
	// closing paren of the handler signature: `): Promise<UserDto> {` or
	// `): UserDto {`. Generic wrappers (Promise/Observable/Array) are unwrapped.
	reNestReturnType = regexp.MustCompile(
		`^\s*:\s*([A-Za-z_][\w<>\[\], |]*?)\s*[{;]`,
	)
	reNestGuard = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bCanActivate\b`,
	)
	reNestInterceptor = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bNestInterceptor\b`,
	)
	reNestGateway = regexp.MustCompile(
		`@WebSocketGateway\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestSubscribeMessage = regexp.MustCompile(
		`@SubscribeMessage\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestResolver = regexp.MustCompile(
		`@Resolver\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestQuery = regexp.MustCompile(
		`@Query\s*\((?:[^()]*|\([^()]*\))*\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestMutation = regexp.MustCompile(
		`@Mutation\s*\((?:[^()]*|\([^()]*\))*\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestSubscription = regexp.MustCompile(
		`@Subscription\s*\((?:[^()]*|\([^()]*\))*\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestPipe = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bPipeTransform\b`,
	)
	reNestMessagePattern = regexp.MustCompile(
		`@MessagePattern\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestEventPattern = regexp.MustCompile(
		`@EventPattern\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestCatch = regexp.MustCompile(
		`@Catch\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reNestCron = regexp.MustCompile(
		`@Cron\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestInterval = regexp.MustCompile(
		`@Interval\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
	)
	reNestCreateParamDecorator = regexp.MustCompile(
		`(?:export\s+)?const\s+([A-Z][A-Za-z0-9_]*)\s*=\s*createParamDecorator\s*\(`,
	)
	reNestPathString = regexp.MustCompile(`['"]([^'"]*?)['"]`)

	// reAngularImport matches an ES import from any @angular/* package. Used to
	// detect Angular source files so the NestJS regex pass can bow out and let
	// the core javascript AST Angular path own them (#2933).
	reAngularImport = regexp.MustCompile(`from\s+['"]@angular/[^'"]+['"]`)
)

var nestHTTPVerbMap = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Options": "OPTIONS", "Head": "HEAD", "All": "ALL",
}

// nestSkipDTOTypes are TS built-in / framework types that are never request or
// response DTOs and must not produce ACCEPTS_INPUT/RETURNS edges.
var nestSkipDTOTypes = map[string]bool{
	"string": true, "number": true, "boolean": true, "any": true, "void": true,
	"unknown": true, "object": true, "Object": true, "Date": true, "Buffer": true,
	"Promise": true, "Observable": true, "Array": true, "Record": true,
	"Map": true, "Set": true, "Partial": true, "Request": true, "Response": true,
	"null": true, "undefined": true, "never": true, "this": true,
}

// nestParamsBlock returns the handler parameter list starting at openParenEnd
// (the byte just past the method's opening `(`), via balanced-paren scanning,
// and the offset of the matching close paren.
func nestParamsBlock(src string, openParenEnd int) (string, int) {
	depth := 1
	i := openParenEnd
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '(', '<', '[', '{':
			depth++
		case ')', '>', ']', '}':
			depth--
		}
		i++
	}
	if depth != 0 {
		return "", openParenEnd
	}
	return src[openParenEnd : i-1], i - 1
}

// nestUnwrapType strips generic wrappers (Promise<T>, Observable<T>, Array<T>,
// T[]) to the innermost user type, returning "" for built-ins/primitives.
func nestUnwrapType(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "[]")
	for {
		open := strings.IndexByte(raw, '<')
		if open < 0 {
			break
		}
		base := strings.TrimSpace(raw[:open])
		if !nestSkipDTOTypes[base] {
			// A user generic like UserDto<T> — keep the base type.
			raw = base
			break
		}
		// Wrapper generic: descend into the first type argument.
		close := strings.LastIndexByte(raw, '>')
		if close <= open {
			break
		}
		inner := raw[open+1 : close]
		if comma := strings.IndexByte(inner, ','); comma >= 0 {
			inner = inner[:comma]
		}
		raw = strings.TrimSpace(inner)
		raw = strings.TrimSuffix(raw, "[]")
	}
	// Union `T | null` → first member.
	if pipe := strings.IndexByte(raw, '|'); pipe >= 0 {
		raw = strings.TrimSpace(raw[:pipe])
	}
	base := strings.TrimSpace(raw)
	if base == "" || nestSkipDTOTypes[base] {
		return ""
	}
	// Must be a bare identifier (no leftover punctuation).
	for _, r := range base {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return base
}

func (e *nestjsExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.nestjs_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "nestjs"),
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

	// #2933: NestJS and Angular share decorator shapes (@Injectable services,
	// `implements CanActivate` guards, `implements PipeTransform` pipes). The
	// core javascript AST path owns Angular extraction; if this file imports
	// from @angular/*, treat it as Angular and skip the NestJS regex pass so
	// the two extractors never emit colliding entity IDs for the same class.
	if reAngularImport.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// @Module
	for _, m := range reNestModule.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "module", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_MODULE")
		addEntity(ent)
	}

	// @Controller
	for _, m := range reNestController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "controller", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_CONTROLLER")
		addEntity(ent)
	}

	// @Injectable — provider. Capture the DI scope when declared via
	// @Injectable({ scope: Scope.REQUEST }) so the oracle knows the provider's
	// lifecycle (#3628 area #5).
	scopeByClass := map[string]string{}
	for _, m := range reNestInjectableScope.FindAllStringSubmatch(src, -1) {
		scopeByClass[m[2]] = m[1]
	}
	for _, m := range reNestInjectable.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "service", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_INJECTABLE",
			"di_provider", "true")
		if s := scopeByClass[name]; s != "" {
			setProps(&ent, "di_scope", s)
		}
		addEntity(ent)
	}

	// HTTP verb methods
	for _, m := range reNestHTTPMethod.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		pathArg := src[m[4]:m[5]]
		methodName := src[m[6]:m[7]]
		httpMethod := nestHTTPVerbMap[verb]
		routePath := ""
		if pm := reNestPathString.FindStringSubmatch(pathArg); pm != nil {
			routePath = pm[1]
		}
		name := fmt.Sprintf("%s %s", httpMethod, methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "http_method", httpMethod,
			"route_path", routePath, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_ROUTE")

		// endpoint→DTO edges (#3629/#3607): the regex match ends just past the
		// handler's opening `(`. Read the balanced param list for @Body() DTOs
		// (ACCEPTS_INPUT) and the trailing return-type annotation (RETURNS).
		paramsBlock, closeOff := nestParamsBlock(src, m[1])
		for _, bm := range reNestBodyParam.FindAllStringSubmatch(paramsBlock, -1) {
			dto := nestUnwrapType(bm[1])
			if dto == "" {
				continue
			}
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID: "Class:" + dto,
				Kind: string(types.RelationshipKindAcceptsInput),
				Properties: map[string]string{
					"framework": "nestjs", "match_source": "body_param_annotation", "dto_type": dto,
				},
			})
		}

		// #4325 — surface the handler's @Param/@Query/@Body decorators in the
		// `parameters` property (the Paths panel reads this, NOT the edges) so
		// the Parameters table lists path/query/body params. The @Body DTO row
		// agrees with the ACCEPTS_INPUT edge above.
		if params := extractNestHandlerParams(paramsBlock); len(params) > 0 {
			if pj := encodeNestParams(params); pj != "" {
				setProps(&ent, "parameters", pj)
			}
			// Surface request-body type/name for the dashboard's body fields.
			for _, p := range params {
				if p.In == "body" {
					setProps(&ent, "request_body_type", p.Type, "request_body_param_name", p.Name)
					break
				}
			}
			// #4464 — emit an ACCEPTS_INPUT graph EDGE from the handler to each
			// whole-DTO request param so request DTOs (and, via #4328 CONTAINS,
			// their fields) stop floating as orphan degree-1 nodes. The @Body DTO
			// already gets its ACCEPTS_INPUT above (reNestBodyParam), so this pass
			// covers the @Query/@Param/@Headers whole-object DTOs that previously
			// had NO inbound edge (the upvate-v3 PermitListQueryDto orphan-ring
			// root cause). Conservative: only whole-object DTOs (no quoted
			// binding key — a `@Query('id')`/`@Param('id')` selects a single
			// primitive field, not the DTO) whose unwrapped type resolves to a
			// real user type. The `Class:<dto>` stub is bound merge-stably by the
			// central name resolver post-merge (same mechanism as @Body / the
			// FastAPI/Flask req-resp DTO edges), so the edge survives merge.
			for _, p := range params {
				if p.In == "body" {
					continue // already emitted via reNestBodyParam above
				}
				if p.QuotedKey {
					continue // single-field selector, not a whole DTO
				}
				dto := nestUnwrapType(p.Type)
				if dto == "" {
					continue // primitive / built-in — honest-partial, no edge
				}
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					ToID: "Class:" + dto,
					Kind: string(types.RelationshipKindAcceptsInput),
					Properties: map[string]string{
						"framework": "nestjs", "match_source": "param_decorator", "dto_type": dto,
						"param_in": p.In,
					},
				})
			}
		}

		if rm := reNestReturnType.FindStringSubmatch(src[closeOff+1 : min(closeOff+200, len(src))]); rm != nil {
			if dto := nestUnwrapType(rm[1]); dto != "" {
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					ToID: "Class:" + dto,
					Kind: string(types.RelationshipKindReturns),
					Properties: map[string]string{
						"framework": "nestjs", "match_source": "return_type_annotation", "dto_type": dto,
					},
				})
				// #4325 — the RETURNS edge alone made the Paths panel count a
				// response but render "(none)" because the Response row reads
				// the `response_type` PROPERTY, which was never set. Set it so
				// the actual DTO name renders.
				setProps(&ent, "response_type", dto)
			}
		}
		addEntity(ent)
	}

	// Guards
	for _, m := range reNestGuard.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "guard", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GUARD")
		addEntity(ent)
	}

	// Interceptors
	for _, m := range reNestInterceptor.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "interceptor", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_INTERCEPTOR")
		addEntity(ent)
	}

	// WebSocket gateways
	for _, m := range reNestGateway.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "gateway", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GATEWAY")
		addEntity(ent)
	}

	// @SubscribeMessage
	for _, m := range reNestSubscribeMessage.FindAllStringSubmatchIndex(src, -1) {
		eventArg := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		event := ""
		if pm := reNestPathString.FindStringSubmatch(eventArg); pm != nil {
			event = pm[1]
		}
		name := fmt.Sprintf("subscribe:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "event", event, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_SUBSCRIBE_MESSAGE")
		addEntity(ent)
	}

	// @Resolver
	for _, m := range reNestResolver.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "resolver", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_RESOLVER")
		addEntity(ent)
	}

	// @Query
	for _, m := range reNestQuery.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GRAPHQL_QUERY")
		addEntity(ent)
	}

	// @Mutation
	for _, m := range reNestMutation.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "mutation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GRAPHQL_MUTATION")
		addEntity(ent)
	}

	// @Subscription (GraphQL)
	for _, m := range reNestSubscription.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "subscription", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_GRAPHQL_SUBSCRIPTION")
		addEntity(ent)
	}

	// PipeTransform
	for _, m := range reNestPipe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "pipe", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_PIPE")
		addEntity(ent)
	}

	// @MessagePattern
	for _, m := range reNestMessagePattern.FindAllStringSubmatchIndex(src, -1) {
		patternArg := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("msg:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "pattern_arg", patternArg, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_MESSAGE_PATTERN")
		addEntity(ent)
	}

	// @EventPattern
	for _, m := range reNestEventPattern.FindAllStringSubmatchIndex(src, -1) {
		patternArg := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("event:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "pattern_arg", patternArg, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_EVENT_PATTERN")
		addEntity(ent)
	}

	// @Catch (exception filter)
	for _, m := range reNestCatch.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "exception_filter", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_CATCH")
		addEntity(ent)
	}

	// @Cron
	for _, m := range reNestCron.FindAllStringSubmatchIndex(src, -1) {
		cronExpr := strings.TrimFunc(src[m[2]:m[3]], isQuoteOrSpace)
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("cron:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "job", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "cron_expression", cronExpr, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_CRON")
		addEntity(ent)
	}

	// @Interval
	for _, m := range reNestInterval.FindAllStringSubmatchIndex(src, -1) {
		intervalArg := strings.TrimSpace(src[m[2]:m[3]])
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("interval:%s", methodName)
		ent := makeEntity(name, "SCOPE.Operation", "job", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "interval_ms", intervalArg, "method_name", methodName,
			"provenance", "INFERRED_FROM_NESTJS_INTERVAL")
		addEntity(ent)
	}

	// createParamDecorator
	for _, m := range reNestCreateParamDecorator.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "param_decorator", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "nestjs", "provenance", "INFERRED_FROM_NESTJS_PARAM_DECORATOR")
		addEntity(ent)
	}

	// Dependency-injection graph (#3628 area #5): merge INJECTED_INTO / BINDS /
	// USES edges onto their file-local owning entity. The owner key is the bare
	// entity Name (consumer class, controller, route operation, or module).
	diEdges := extractNestDIEdges(src)
	if len(diEdges) > 0 {
		// Bootstrap files (main.ts) bind cross-cutting providers app-wide via
		// app.useGlobal*() but have no module/class entity to own those edges.
		// Emit a synthetic `app` application entity so the global USES edges are
		// retained and the bound classes are connected (#4329).
		if nestHasGlobalWiring(src) {
			appEnt := makeEntity(nestAppEntityName, "SCOPE.Pattern", "application",
				file.Path, file.Language, 1)
			setProps(&appEnt, "framework", "nestjs",
				"provenance", "INFERRED_FROM_NESTJS_BOOTSTRAP")
			addEntity(appEnt)
		}
		byName := make(map[string]int, len(entities))
		for i := range entities {
			// First entity wins per name (controllers/services are unique by
			// class name within a file; route ops are unique by "<VERB> <m>").
			if _, ok := byName[entities[i].Name]; !ok {
				byName[entities[i].Name] = i
			}
		}
		edgeCount := 0
		for owner, rels := range diEdges {
			idx, ok := byName[owner]
			if !ok {
				continue
			}
			entities[idx].Relationships = append(entities[idx].Relationships, rels...)
			edgeCount += len(rels)
		}
		span.SetAttributes(attribute.Int("di_edge_count", edgeCount))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
