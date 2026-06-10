// NestJS handler-signature extraction for the synthesized http_endpoint entity
// (#4568 / #4569).
//
// synthesizeNestJS (http_endpoint_synthesis.go) re-parses NestJS controllers
// and emits the canonical, controller-prefixed `http_endpoint` definition — the
// entity the dashboard Paths panel actually reads. Until now that synthesizer
// stamped only the route / verb / handler line; it captured NONE of the handler
// SIGNATURE. The dashboard reads `parameters` / `response_type` / `request_body_*`
// from THIS entity, so:
//
//   - Scalar `@Query('year') x: string` / `@Param('id') id: number` /
//     `@Headers('x')` params never surfaced → Parameters (0)/None (#4568). The
//     custom nestjs extractor DID capture them onto its SCOPE.Operation/endpoint,
//     but that is a different entity than the one the panel renders.
//   - `Promise<ProposalCountsResponse>` return types never set `response_type`,
//     so the Response row showed (1)→(none) even though the DTO is indexed
//     (#4569; the Schema-kind resolution gap is fixed dashboard-side too).
//
// This file gives the synthesizer self-contained (engine can't import a custom
// extractor) handler-signature parsing that writes the SAME wire shape the
// dashboard already decodes: a `parameters` JSON []JavaParam plus the
// response_type / response_is_array / response_void / request_body_type props.
//
// CROSS-FRAMEWORK NOTE: scalar request params (query/path/header) are a recurring
// gap across frameworks — Express `req.query`/`req.params`, FastAPI
// `Query()`/`Path()`, Spring `@RequestParam`/`@PathVariable`, DRF query_params.
// This pass implements NestJS; follow-ups are filed for the rest.
package engine

import (
	"encoding/json"
	"regexp"
	"strings"
)

// nestSigParamDecoratorRe matches a single param decorator + the parameter it
// annotates within a handler parameter list. Group 1 = decorator
// (Param|Query|Body|Headers|Header), group 2 = the decorator's first quoted
// binding key (when present), group 3 = the parameter identifier, group 4 = the
// TS type annotation (when present). Mirrors the custom extractor's
// reNestParamDecorator so both entities agree.
var nestSigParamDecoratorRe = regexp.MustCompile(
	`@(Param|Query|Body|Headers|Header)\s*\(\s*(?:['"]([^'"]*)['"])?[^)]*\)\s*(\w+)\??\s*(?::\s*([A-Za-z_][\w<>\[\], |.]*))?`,
)

// nestSigReturnTypeRe captures the method return-type annotation following the
// closing paren of the handler signature: `): Promise<UserDto> {` / `): UserDto {`.
var nestSigReturnTypeRe = regexp.MustCompile(`\)\s*:\s*([A-Za-z_][\w<>\[\], |.]*?)\s*[{;]`)

// nestSigDecoratorIn maps a NestJS param decorator to its OpenAPI `in` location.
var nestSigDecoratorIn = map[string]string{
	"Param":   "path",
	"Query":   "query",
	"Body":    "body",
	"Header":  "header",
	"Headers": "header",
}

// nestSigSkipTypes are TS built-in / framework types that are never response
// DTOs (so they don't set response_type) — they are honest scalars/no-content.
var nestSigSkipTypes = map[string]bool{
	"string": true, "number": true, "boolean": true, "any": true, "void": true,
	"unknown": true, "object": true, "Object": true, "Date": true, "Buffer": true,
	"Promise": true, "Observable": true, "Array": true, "Record": true,
	"Map": true, "Set": true, "Partial": true, "Request": true, "Response": true,
	"null": true, "undefined": true, "never": true, "this": true,
}

// nestSigEnvelopeWrappers are convention envelope generics whose single payload
// type argument carries the actual response DTO. Mirrors the custom extractor's
// nestEnvelopeWrappers so the unwrap is identical on both entities.
var nestSigEnvelopeWrappers = map[string]bool{
	"ApiResponse": true, "PagedResponse": true, "PaginatedResponse": true,
	"Paginated": true, "Page": true, "PageResponse": true, "Pagination": true,
	"CollectionResponse": true, "ListResponse": true, "DataResponse": true,
	"ResponseWrapper": true, "Wrapped": true, "Envelope": true,
	"ResponseEntity": true, "ApiResult": true, "Result": true,
}

// nestSignature is the parsed handler signature for one NestJS route.
type nestSignature struct {
	Params         []JavaParam
	ResponseType   string
	ResponseIsArr  bool
	ResponseVoid   bool
	RequestBody    string // DTO type of the @Body() param, when any
	RequestBodyArg string // identifier of the @Body() param
}

// nestjsReadSignature reads the handler signature starting at the handler
// declaration line (handlerLineIdx, 0-based into `lines`). It stitches the
// (possibly multi-line) parameter list via balanced-paren scanning, then reads
// the trailing return-type annotation. Returns the parsed params + response
// shape. A handler with no decorated params and no DTO return type yields an
// empty (but non-nil-meaningful) signature.
func nestjsReadSignature(lines []string, handlerLineIdx int) nestSignature {
	var sig nestSignature
	if handlerLineIdx < 0 || handlerLineIdx >= len(lines) {
		return sig
	}
	// Join from the handler line until the signature's opening `(` is balanced.
	// Cap the look-ahead so a malformed source can't run away.
	const maxLines = 60
	var b strings.Builder
	for i := handlerLineIdx; i < len(lines) && i < handlerLineIdx+maxLines; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	region := b.String()

	openIdx := strings.IndexByte(region, '(')
	if openIdx < 0 {
		return sig
	}
	paramsBlock, closeIdx := nestSigBalancedParens(region, openIdx+1)

	sig.Params = nestSigParseParams(paramsBlock)
	for _, p := range sig.Params {
		if p.In == "body" && sig.RequestBody == "" {
			sig.RequestBody = p.Type
			sig.RequestBodyArg = p.Name
		}
	}

	// Return type: look at the text right after the matching close paren.
	if closeIdx >= 0 && closeIdx < len(region) {
		tail := region[closeIdx:]
		if rm := nestSigReturnTypeRe.FindStringSubmatch(tail); rm != nil {
			dto, isArr, isVoid := nestSigResolveResponseType(rm[1])
			sig.ResponseType = dto
			sig.ResponseIsArr = isArr
			sig.ResponseVoid = isVoid
		}
	}
	return sig
}

// nestSigBalancedParens scans from `start` (just past an opening `(`) and
// returns the inner text up to the matching close paren plus the index of that
// close paren (in `src`). Tracks <>/[]/{} so generic args and inline object
// types don't unbalance the count.
func nestSigBalancedParens(src string, start int) (string, int) {
	depth := 1
	i := start
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
		return src[start:], -1
	}
	return src[start : i-1], i - 1
}

// nestSigParseParams parses a handler parameter block into ordered JavaParams,
// one per request-shaping decorator (@Param/@Query/@Body/@Header(s)). Decorator-
// less and framework-injection params are skipped.
func nestSigParseParams(paramsBlock string) []JavaParam {
	if strings.TrimSpace(paramsBlock) == "" {
		return nil
	}
	var out []JavaParam
	for _, m := range nestSigParamDecoratorRe.FindAllStringSubmatch(paramsBlock, -1) {
		dec := m[1]
		key := strings.TrimSpace(m[2])
		ident := strings.TrimSpace(m[3])
		rawType := nestSigCleanType(m[4])

		in := nestSigDecoratorIn[dec]
		if in == "" {
			continue
		}
		name := key
		if name == "" {
			name = ident // @Query()/@Body() whole-object: fall back to identifier
		}
		if name == "" {
			continue
		}
		p := JavaParam{
			Name:        name,
			In:          in,
			Annotations: []string{"@" + dec},
		}
		if rawType != "" {
			if dto := nestSigUnwrapType(rawType); dto != "" {
				p.Type = dto
			} else {
				p.Type = strings.TrimSpace(strings.SplitN(rawType, "|", 2)[0])
			}
		}
		if in == "path" {
			p.Required = true // path params are always required
		}
		out = append(out, p)
	}
	return out
}

// nestSigCleanType trims a captured TS type to the single param's type, cutting
// at the first top-level comma (the char class permits `,` for generic args).
func nestSigCleanType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	depth := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '<', '[':
			depth++
		case '>', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				raw = raw[:i]
				return strings.TrimRight(strings.TrimSpace(raw), ", ")
			}
		}
	}
	return strings.TrimRight(strings.TrimSpace(raw), ", ")
}

// nestSigUnwrapType strips generic/array/envelope wrappers to the innermost user
// DTO name, returning "" for built-ins/primitives.
func nestSigUnwrapType(raw string) string {
	dto, _, _ := nestSigResolveResponseType(raw)
	return dto
}

// nestSigResolveResponseType unwraps a response/return type to its innermost
// user DTO, reporting array-payload and genuine no-content (void) results.
// Mirrors the custom extractor's nestResolveResponseType.
func nestSigResolveResponseType(raw string) (dto string, isArray, isVoid bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, false
	}
	if strings.HasSuffix(raw, "[]") {
		isArray = true
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "[]"))
	}
	for {
		open := strings.IndexByte(raw, '<')
		if open < 0 {
			break
		}
		base := strings.TrimSpace(raw[:open])
		isWrapper := nestSigSkipTypes[base] || nestSigEnvelopeWrappers[base]
		if !isWrapper {
			raw = base // a user generic like UserDto<T> — keep the base type
			break
		}
		if base == "Array" || base == "Set" {
			isArray = true
		}
		closeIdx := strings.LastIndexByte(raw, '>')
		if closeIdx <= open {
			break
		}
		inner := raw[open+1 : closeIdx]
		if comma := strings.IndexByte(inner, ','); comma >= 0 {
			inner = inner[:comma]
		}
		raw = strings.TrimSpace(inner)
		if strings.HasSuffix(raw, "[]") {
			isArray = true
			raw = strings.TrimSpace(strings.TrimSuffix(raw, "[]"))
		}
	}
	if pipe := strings.IndexByte(raw, '|'); pipe >= 0 {
		raw = strings.TrimSpace(raw[:pipe])
	}
	base := strings.TrimSpace(raw)
	switch base {
	case "void", "undefined", "never":
		return "", isArray, true
	}
	if base == "" || nestSigSkipTypes[base] {
		return "", isArray, false
	}
	for _, r := range base {
		if !(r == '_' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return "", isArray, false
		}
	}
	// Strip a qualifier prefix (e.g. `dto.UserDto` → `UserDto`) for the bare
	// type name the entity index keys on.
	if dot := strings.LastIndexByte(base, '.'); dot >= 0 {
		base = base[dot+1:]
	}
	return base, isArray, false
}

// nestSigEncodeParams marshals the param list to the canonical JSON the
// dashboard decodes (engine.DecodeJavaParameters). Empty → "".
func nestSigEncodeParams(ps []JavaParam) string {
	if len(ps) == 0 {
		return ""
	}
	b, err := json.Marshal(ps)
	if err != nil {
		return ""
	}
	return string(b)
}

// stampNestSignature writes the parsed handler signature onto a synthesized
// http_endpoint entity's Properties so the dashboard Paths panel renders the
// Parameters table and the Response shape. Idempotent: only sets a property
// when it carries information.
func stampNestSignature(props map[string]string, sig nestSignature) {
	if props == nil {
		return
	}
	if pj := nestSigEncodeParams(sig.Params); pj != "" {
		props["parameters"] = pj
	}
	if sig.RequestBody != "" {
		props["request_body_type"] = sig.RequestBody
		if sig.RequestBodyArg != "" {
			props["request_body_param_name"] = sig.RequestBodyArg
		}
	}
	switch {
	case sig.ResponseType != "":
		props["response_type"] = sig.ResponseType
		if sig.ResponseIsArr {
			props["response_is_array"] = "true"
		}
	case sig.ResponseVoid:
		props["response_void"] = "true"
	}
}
