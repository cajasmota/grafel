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

		// ACCEPTS_INPUT: body parameters
		for _, pm := range farrParamAnnotRe.FindAllStringSubmatch(paramsBlock, -1) {
			paramName := pm[1]
			typeRaw := pm[2]
			if paramName == "self" || paramName == "cls" || paramName == "args" || paramName == "kwargs" {
				continue
			}
			if paramIsInjected(paramName, paramsBlock) {
				continue
			}
			dtoName := unwrapType(typeRaw)
			if dtoName == "" {
				continue
			}
			emitDTO(dtoName, line, "dto")
			out = append(out, entity(handlerName+":accepts:"+dtoName, "SCOPE.Operation", "endpoint", file.Path, line,
				map[string]string{"framework": "fastapi", "pattern_type": "accepts_input", "param_name": paramName, "dto_type": dtoName}))
		}

		// RETURNS: response_model= kwarg
		if rm := farrResponseModelRe.FindStringSubmatch(decoratorArgs); rm != nil {
			dtoName := unwrapType(rm[1])
			if dtoName != "" {
				emitDTO(dtoName, line, "response")
				out = append(out, entity(handlerName+":returns:"+dtoName, "SCOPE.Operation", "endpoint", file.Path, line,
					map[string]string{"framework": "fastapi", "pattern_type": "returns", "dto_type": dtoName, "match_source": "response_model_decorator"}))
			}
		}

		// RETURNS: -> ReturnType annotation
		afterClose := source[closeOffset:min(closeOffset+120, len(source))]
		if ret := farrReturnAnnotRe.FindStringSubmatch(afterClose); ret != nil {
			dtoName := unwrapType(strings.TrimSpace(ret[1]))
			if dtoName != "" {
				emitDTO(dtoName, line, "response")
				out = append(out, entity(handlerName+":returns:"+dtoName, "SCOPE.Operation", "endpoint", file.Path, line,
					map[string]string{"framework": "fastapi", "pattern_type": "returns", "dto_type": dtoName, "match_source": "return_type_annotation"}))
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

var (
	unwrapGenericRe    = regexp.MustCompile(`^(?:List|Optional|Set|Tuple|Sequence|Union)\[(.+)\]$`)
	unwrapPipeOptRe    = regexp.MustCompile(`^(\w+)\s*\|\s*None$`)
)

func unwrapType(raw string) string {
	raw = strings.TrimSpace(raw)
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
