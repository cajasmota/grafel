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
	extractor.Register("python_fastapi_reqresp", &FastAPIReqRespExtractor{})
}

// FastAPIReqRespExtractor extracts ACCEPTS_INPUT and RETURNS patterns from
// FastAPI endpoints: Pydantic body parameters and response_model / return
// type annotations.
type FastAPIReqRespExtractor struct{}

func (e *FastAPIReqRespExtractor) Language() string { return "python_fastapi_reqresp" }

var (
	farrRouteHeaderRe = regexp.MustCompile(
		`(?s)@(\w+)\.(get|post|put|delete|patch|head|options|trace)\s*` +
			`\(\s*(?:r)?["']([^"']*)["']` +
			`(.*?)\)\s*\n` +
			`(?:\s*(?:#[^\n]*)?\n)*` +
			`\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	farrResponseModelRe = regexp.MustCompile(`response_model\s*=\s*([\w\[\], |.]+?)(?:\s*[,)]|$)`)
	farrReturnAnnotRe   = regexp.MustCompile(`\)\s*->\s*([\w\[\], |.]+?)\s*:`)
	farrParamAnnotRe    = regexp.MustCompile(`(?:^|[^*])\b(\w+)\s*:\s*([\w\[\], |.]+?)(?:\s*(?:,|\)|$))`)
)

var farrSkipTypes = map[string]bool{
	"int": true, "str": true, "float": true, "bool": true, "bytes": true,
	"None": true, "dict": true, "list": true, "set": true, "tuple": true,
	"Any": true, "object": true, "Optional": true, "List": true, "Dict": true,
	"Set": true, "Tuple": true, "Union": true, "Sequence": true,
	"Response": true, "JSONResponse": true, "HTMLResponse": true,
	"Request": true, "BackgroundTasks": true, "HTTPException": true, "status": true,
	"BaseModel": true, "BaseSettings": true, "RootModel": true,
	"datetime": true, "date": true, "time": true, "UUID": true, "Decimal": true, "Path": true,
}

var farrInjectionRe = regexp.MustCompile(`(?:Depends|Query|Path|Header|Cookie|Body|File|Form|Security)\s*\(`)

// farrInjectedParamRe captures a param whose initializer is a Depends()/Query()
// injection (#4476). Group 1 = param name, group 2 = the (optional) type
// annotation. Only Depends/Query carry whole-object Pydantic DTOs; Path/Header/
// Cookie/File/Form bind scalars, so they're excluded here.
var farrInjectedParamRe = regexp.MustCompile(
	`\b(\w+)\s*(?::\s*([\w\[\], |.]+?))?\s*=\s*(?:Depends|Query)\s*\(`)

func (e *FastAPIReqRespExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_fastapi_reqresp")
	_, span := tracer.Start(ctx, "custom.python_fastapi_reqresp")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord
	seenDTOs := make(map[string]bool)

	emitDTO := func(dtoName string, line int, kind string) {
		if seenDTOs[dtoName] {
			return
		}
		seenDTOs[dtoName] = true
		out = append(out, entity(dtoName, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "fastapi", "pattern_type": "request_response_dto", "kind": kind}))
	}

	for _, idx := range allMatchesIndex(farrRouteHeaderRe, source) {
		decoratorArgs := source[idx[8]:idx[9]]
		handlerName := source[idx[10]:idx[11]]
		line := lineOf(source, idx[0])

		// Extract params block via balanced parens
		paramsBlock, closeOffset := extractParamsBlock(source, idx[1])

		// ACCEPTS_INPUT — accumulate request DTOs from this handler's params.
		// emitAccepts is idempotent per (param,dto) within the handler.
		seenAccept := make(map[string]bool)
		emitAccepts := func(paramName, dtoName, matchSource string) {
			if dtoName == "" {
				return
			}
			key := paramName + ":" + dtoName
			if seenAccept[key] {
				return
			}
			seenAccept[key] = true
			emitDTO(dtoName, line, "dto")
			ep := entity(handlerName+":accepts:"+dtoName, "SCOPE.Operation", "endpoint", file.Path, line,
				map[string]string{"framework": "fastapi", "pattern_type": "accepts_input", "param_name": paramName, "dto_type": dtoName, "match_source": matchSource})
			// ACCEPTS_INPUT edge: endpoint -> request DTO type (#3629/#4476).
			// FromID is left empty so graph assembly binds it to this endpoint
			// entity; ToID is the structural ref to the real Pydantic class.
			ep.Relationships = append(ep.Relationships, types.RelationshipRecord{
				ToID:       "Class:" + dtoName,
				Kind:       string(types.RelationshipKindAcceptsInput),
				Properties: map[string]string{"framework": "fastapi", "match_source": matchSource, "param_name": paramName, "dto_type": dtoName},
			})
			out = append(out, ep)
		}

		// (1) Plain body params: `payload: CreateRequest`. Skip params that are
		// injected (`= Depends()/Query()/...`) — those are handled by pass (2) as
		// the FastAPI analog of the NestJS @Query() DTO (#4476).
		for _, pm := range farrParamAnnotRe.FindAllStringSubmatch(paramsBlock, -1) {
			paramName := pm[1]
			typeRaw := pm[2]
			if paramName == "self" || paramName == "cls" || paramName == "args" || paramName == "kwargs" {
				continue
			}
			if paramIsInjected(paramName, paramsBlock) {
				continue
			}
			emitAccepts(paramName, unwrapType(typeRaw), "body_param_annotation")
		}

		// (2) #4476 — query-model / dependency Pydantic DTOs. These are bound via
		// `= Depends(Model)`, `: Model = Depends()`, `= Query(...)` (whole-object)
		// or `Annotated[Model, Query()]`. They previously had NO inbound edge.
		// Conservative: only when the dependency/annotation resolves to a real
		// PascalCase model class (a provider function like `Depends(get_db)` is
		// skipped). The annotated-`Annotated[...]` form is also covered by pass
		// (1) when it carries no `=` initializer (unwrapType strips Annotated);
		// emitAccepts dedups so double-coverage is harmless.
		for _, im := range farrInjectedParamRe.FindAllStringSubmatch(paramsBlock, -1) {
			paramName := im[1]
			annot := strings.TrimSpace(im[2])
			if paramName == "self" || paramName == "cls" {
				continue
			}
			// Prefer the dependency callee's explicit model arg; fall back to the
			// param's type annotation (e.g. `q: FilterParams = Depends()`).
			dto := farrDependencyDTO(paramName, paramsBlock)
			if dto == "" && annot != "" {
				dto = unwrapType(annot)
			}
			emitAccepts(paramName, dto, "dependency_query_model")
		}

		// RETURNS: response_model= kwarg
		if rm := farrResponseModelRe.FindStringSubmatch(decoratorArgs); rm != nil {
			dtoName := unwrapType(rm[1])
			if dtoName != "" {
				emitDTO(dtoName, line, "response")
				ep := entity(handlerName+":returns:"+dtoName, "SCOPE.Operation", "endpoint", file.Path, line,
					map[string]string{"framework": "fastapi", "pattern_type": "returns", "dto_type": dtoName, "match_source": "response_model_decorator"})
				ep.Relationships = append(ep.Relationships, types.RelationshipRecord{
					ToID:       "Class:" + dtoName,
					Kind:       string(types.RelationshipKindReturns),
					Properties: map[string]string{"framework": "fastapi", "match_source": "response_model_decorator", "dto_type": dtoName},
				})
				out = append(out, ep)
			}
		}

		// RETURNS: -> ReturnType annotation
		afterClose := source[closeOffset:min(closeOffset+120, len(source))]
		if ret := farrReturnAnnotRe.FindStringSubmatch(afterClose); ret != nil {
			dtoName := unwrapType(strings.TrimSpace(ret[1]))
			if dtoName != "" {
				emitDTO(dtoName, line, "response")
				ep := entity(handlerName+":returns:"+dtoName, "SCOPE.Operation", "endpoint", file.Path, line,
					map[string]string{"framework": "fastapi", "pattern_type": "returns", "dto_type": dtoName, "match_source": "return_type_annotation"})
				ep.Relationships = append(ep.Relationships, types.RelationshipRecord{
					ToID:       "Class:" + dtoName,
					Kind:       string(types.RelationshipKindReturns),
					Properties: map[string]string{"framework": "fastapi", "match_source": "return_type_annotation", "dto_type": dtoName},
				})
				out = append(out, ep)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

func extractParamsBlock(source string, openParenEnd int) (string, int) {
	depth := 1
	i := openParenEnd
	for i < len(source) && depth > 0 {
		switch source[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		i++
	}
	if depth != 0 {
		return "", openParenEnd
	}
	closeOffset := i - 1
	return source[openParenEnd:closeOffset], closeOffset
}

func paramIsInjected(paramName, paramsBlock string) bool {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(paramName) + `\s*(?::[^,=]+)?\s*=\s*` + farrInjectionRe.String())
	return pattern.MatchString(paramsBlock)
}

// farrDependencyDTO resolves the Pydantic DTO a query-model / dependency param
// binds (#4476). It inspects the `= Depends(...)` / `= Query(...)` initializer
// for an explicit model argument — `Depends(FilterParams)` — returning that
// class name when it resolves to a real DTO (not a primitive/builtin and not a
// bare `Depends()`/`Query()`). When the initializer carries no class argument
// (e.g. `q: FilterParams = Depends()`) the caller falls back to the param's
// type annotation. Returns "" when no DTO can be resolved.
var farrDependsArgRe = regexp.MustCompile(`(?:Depends|Query)\s*\(\s*([\w.]+)?`)

func farrDependencyDTO(paramName, paramsBlock string) string {
	// Capture the initializer expression for this param: everything after `=`
	// up to the next top-level comma or the end of the block.
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(paramName) + `\s*(?::[^,=]+)?\s*=\s*((?:Depends|Query)\s*\([^)]*\))`)
	m := re.FindStringSubmatch(paramsBlock)
	if m == nil {
		return ""
	}
	am := farrDependsArgRe.FindStringSubmatch(m[1])
	if am == nil || am[1] == "" {
		return "" // bare Depends()/Query() — fall back to annotation
	}
	// Strip a dotted prefix (module.Model → Model) and apply the same
	// primitive/builtin filtering used for body params.
	arg := am[1]
	if i := strings.LastIndex(arg, "."); i >= 0 {
		arg = arg[i+1:]
	}
	if farrSkipTypes[arg] {
		return ""
	}
	// A dependency that resolves to a lowercase identifier is conventionally a
	// provider function (get_db, get_current_user), not a model class. DTO
	// models are PascalCase; gate on that to stay conservative.
	if arg == "" || arg[0] < 'A' || arg[0] > 'Z' {
		return ""
	}
	return arg
}

var (
	unwrapGenericRe = regexp.MustCompile(`^(?:List|Optional|Set|Tuple|Sequence|Union)\[(.+)\]$`)
	unwrapPipeOptRe = regexp.MustCompile(`^(\w+)\s*\|\s*None$`)
	// #4476 — `Annotated[FilterParams, Query()]` (query-model dependency form):
	// the first type argument is the real DTO; the rest are FastAPI markers.
	unwrapAnnotatedRe = regexp.MustCompile(`^Annotated\[\s*([\w.]+)`)
)

func unwrapType(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := unwrapAnnotatedRe.FindStringSubmatch(raw); m != nil {
		raw = m[1]
		if i := strings.LastIndex(raw, "."); i >= 0 {
			raw = raw[i+1:]
		}
	}
	if m := unwrapPipeOptRe.FindStringSubmatch(raw); m != nil {
		raw = m[1]
	}
	if m := unwrapGenericRe.FindStringSubmatch(raw); m != nil {
		inner := strings.TrimSpace(m[1])
		unwrapped := unwrapType(inner)
		if unwrapped != "" {
			raw = unwrapped
		} else {
			return ""
		}
	}
	baseRe := regexp.MustCompile(`^(\w+)`)
	bm := baseRe.FindStringSubmatch(raw)
	if bm == nil {
		return ""
	}
	base := bm[1]
	if farrSkipTypes[base] {
		return ""
	}
	return base
}
