// Endpoint response status-code set stamping (epic #3628).
//
// A language-agnostic enrichment pass that runs at the tail of
// applyHTTPEndpointSynthesis, AFTER every per-language route synthesizer has
// emitted its http_endpoint_definition entities for the current file. Like the
// API-version / deprecation / pagination passes (see
// http_endpoint_deprecation.go and http_endpoint_pagination.go), it mutates
// Properties on the just-emitted producer endpoints in place — it never adds or
// removes entities, so it cannot regress upstream synthesis.
//
// It answers the graph question the endpoint surface could not previously
// answer: "what HTTP status codes can POST /users return?" — useful for
// API-contract parity between a producer and its consumers / OpenAPI spec.
//
// Property contract stamped on http_endpoint_definition:
//
//	response_codes — comma-joined, sorted, unique list of HTTP status codes the
//	                 handler can return (e.g. "201,404"); present only when at
//	                 least one literal code was resolved.
//	success_code   — the single 2xx code the handler returns on the happy path,
//	                 when exactly one 2xx code is present (e.g. "201"); omitted
//	                 when zero or several 2xx codes are present (ambiguous).
//	response_codes_source — the dominant signal that fired (evidence).
//
// HONEST-PARTIAL boundary (the whole point of QUALITY-FIRST here): a status
// expressed through a dynamic variable (`res.status(code)`, `status=my_status`)
// is NOT resolvable to a literal and is skipped — we still record any literal
// codes found alongside it, but never fabricate a value for the dynamic one. If
// NO literal code is resolvable, response_codes is left absent entirely. A
// status literal outside an endpoint handler body is not attributed to any
// endpoint.
//
// Signals, per framework:
//
//   - FastAPI (Python): decorator `status_code=201`; `raise HTTPException(
//     status_code=404)`; `JSONResponse(status_code=...)`; `Response(
//     status_code=...)`.
//   - DRF / Django (Python): `Response(data, status=status.HTTP_201_CREATED)`;
//     `HttpResponse(status=403)`; `raise NotFound` / `PermissionDenied` (DRF
//     exception → code mapping); default success 200.
//   - Express / Nest (JS/TS): `res.status(201)`; `res.sendStatus(204)`;
//     `@HttpCode(201)`; `throw new NotFoundException()` (Nest exc → code).
//   - Spring (Java/Kotlin): `@ResponseStatus(HttpStatus.CREATED)`;
//     `ResponseEntity.status(404)` / `.ok()` (200) / `.notFound()` (404) /
//     `.created(...)` (201); `throw new ResponseStatusException(NOT_FOUND)`.
//
// Refs #3628.
package engine

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// responseCodesVerdict is the resolved status-code set for one endpoint.
type responseCodesVerdict struct {
	codes  map[int]bool
	source string // evidence: the dominant signal that fired
}

func (v *responseCodesVerdict) add(code int) {
	if code < 100 || code > 599 {
		return
	}
	if v.codes == nil {
		v.codes = map[int]bool{}
	}
	v.codes[code] = true
}

func (v *responseCodesVerdict) merge(other responseCodesVerdict) {
	for c := range other.codes {
		v.add(c)
	}
	if v.source == "" {
		v.source = other.source
	}
}

// applyEndpointResponseCodes stamps response_codes / success_code on every
// producer endpoint at index >= before in `entities` that belongs to `path`.
// The status set is resolved from the route decorator/annotation region plus the
// handler body in the source file.
func applyEndpointResponseCodes(lang, content, path string, entities []types.EntityRecord, before int) {
	if content == "" || before < 0 || before >= len(entities) {
		return
	}
	normLang := normaliseResponseCodesLang(lang)

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		v := resolveEndpointResponseCodes(normLang, content, e)
		if len(v.codes) == 0 {
			continue
		}
		sorted := sortedCodes(v.codes)
		strs := make([]string, len(sorted))
		for j, c := range sorted {
			strs[j] = strconv.Itoa(c)
		}
		e.Properties["response_codes"] = strings.Join(strs, ",")
		if sc, ok := singleSuccessCode(sorted); ok {
			e.Properties["success_code"] = strconv.Itoa(sc)
		}
		if v.source != "" {
			e.Properties["response_codes_source"] = v.source
		}
	}
}

func normaliseResponseCodesLang(lang string) string {
	low := strings.ToLower(lang)
	switch low {
	case "typescript", "javascript_typescript":
		return "javascript"
	case "kotlin":
		return "java"
	}
	return low
}

// sortedCodes returns the ascending unique status codes.
func sortedCodes(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

// singleSuccessCode returns the lone 2xx code in a sorted code list, and whether
// exactly one 2xx code is present (so success_code is unambiguous).
func singleSuccessCode(sorted []int) (int, bool) {
	found := -1
	for _, c := range sorted {
		if c >= 200 && c < 300 {
			if found != -1 {
				return 0, false // more than one 2xx → ambiguous
			}
			found = c
		}
	}
	if found == -1 {
		return 0, false
	}
	return found, true
}

// resolveEndpointResponseCodes inspects the decorator region + handler body for
// status-code literals across the recognised framework shapes.
func resolveEndpointResponseCodes(lang, content string, e *types.EntityRecord) responseCodesVerdict {
	anchorLine := e.StartLine
	if anchorLine <= 0 {
		anchorLine = routeDeclarationLine(content, e.Properties["path"], e.Properties["verb"])
	}
	region, handlerStart := handlerDecoratorRegion(content, anchorLine)
	body := handlerBodyWindowLarge(content, handlerStart)

	var v responseCodesVerdict
	switch lang {
	case "python":
		v.merge(pythonResponseCodes(region, body))
	case "java":
		// For Spring the route annotation (@PostMapping) is the anchor; a sibling
		// `@ResponseStatus` annotation and the handler signature sit on the lines
		// just BELOW it (outside the upward decorator region). Include a small
		// forward window so @ResponseStatus is in scope.
		sig := forwardSignatureWindow(content, anchorLine)
		v.merge(javaResponseCodes(region+"\n"+sig, body))
	case "javascript":
		v.merge(jsResponseCodes(region, body))
	}
	return v
}

// handlerBodyWindowLarge returns a larger bounded window of the handler body
// than handlerBodyWindow (deprecation uses 1000) — a handler can raise / return
// several status codes spread across its body. 2500 bytes covers a typical
// handler without bleeding far into a sibling.
func handlerBodyWindowLarge(content string, handlerStart int) string {
	if handlerStart < 0 || handlerStart >= len(content) {
		return ""
	}
	end := handlerStart + 2500
	if end > len(content) {
		end = len(content)
	}
	return content[handlerStart:end]
}

// ---------------------------------------------------------------------------
// Python — FastAPI + DRF/Django
// ---------------------------------------------------------------------------

// pyDecoratorStatusRe matches a `status_code=201` kwarg in a route decorator
// (FastAPI `@app.post(..., status_code=201)`).
var pyDecoratorStatusRe = regexp.MustCompile(`status_code\s*=\s*(\d{3})`)

// pyHTTPStatusConstRe matches a Django/DRF `status.HTTP_201_CREATED` reference,
// capturing the numeric code embedded in the constant name.
var pyHTTPStatusConstRe = regexp.MustCompile(`status\.HTTP_(\d{3})_[A-Z0-9_]+`)

// Note: a `status=403` / `status_code=403` literal kwarg is matched by the
// package-level pyStatusKwargRe (response_shape_python.go), reused below. A
// non-literal value (`status=my_var`) does not match — honest-partial.

func pythonResponseCodes(region, body string) responseCodesVerdict {
	var v responseCodesVerdict

	// FastAPI decorator status_code=… (region = decorator + signature).
	for _, m := range pyDecoratorStatusRe.FindAllStringSubmatch(region, -1) {
		if c, err := strconv.Atoi(m[1]); err == nil {
			v.add(c)
			if v.source == "" {
				v.source = "status_code= decorator"
			}
		}
	}

	// DRF status.HTTP_xxx constants in Response(...) / HttpResponse(...).
	for _, m := range pyHTTPStatusConstRe.FindAllStringSubmatch(body, -1) {
		if c, err := strconv.Atoi(m[1]); err == nil {
			v.add(c)
			if v.source == "" {
				v.source = "status.HTTP_*"
			}
		}
	}

	// FastAPI/DRF status_code= / status= literals in the body (HTTPException,
	// JSONResponse, Response, HttpResponse). The package-level pyStatusKwargRe
	// matches both `status=NNN` and `status_code=NNN`.
	for _, m := range pyStatusKwargRe.FindAllStringSubmatch(body, -1) {
		if c, err := strconv.Atoi(m[1]); err == nil {
			v.add(c)
			if v.source == "" {
				v.source = "status= literal"
			}
		}
	}

	// DRF exception classes raised in the body → their conventional HTTP code.
	for name, code := range drfExceptionCodes {
		if raisedExceptionRe(name).MatchString(body) {
			v.add(code)
			if v.source == "" {
				v.source = "DRF exception"
			}
		}
	}

	return v
}

// drfExceptionCodes maps DRF's built-in APIException subclasses to the HTTP
// status code each one carries by definition (rest_framework/exceptions.py).
var drfExceptionCodes = map[string]int{
	"NotFound":             404,
	"PermissionDenied":     403,
	"NotAuthenticated":     401,
	"AuthenticationFailed": 401,
	"ValidationError":      400,
	"ParseError":           400,
	"MethodNotAllowed":     405,
	"NotAcceptable":        406,
	"Throttled":            429,
	"UnsupportedMediaType": 415,
}

// raisedExceptionRe matches `raise <Name>(` or `raise <Name>` for an exception
// class name (word-boundary, optionally followed by call parens).
func raisedExceptionRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`\braise\s+` + regexp.QuoteMeta(name) + `\b`)
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Spring
// ---------------------------------------------------------------------------

// javaResponseStatusRe matches `@ResponseStatus(HttpStatus.CREATED)` /
// `@ResponseStatus(code = HttpStatus.NOT_FOUND)` / `@ResponseStatus(value = ...)`.
var javaResponseStatusRe = regexp.MustCompile(`@ResponseStatus\s*\(\s*(?:(?:code|value)\s*=\s*)?HttpStatus\.([A-Z_0-9]+)`)

// javaResponseStatusNumericRe matches a numeric @ResponseStatus(200).
var javaResponseStatusNumericRe = regexp.MustCompile(`@ResponseStatus\s*\(\s*(?:(?:code|value)\s*=\s*)?(\d{3})\b`)

// javaResponseEntityStatusRe matches `ResponseEntity.status(HttpStatus.X)` and
// `ResponseEntity.status(404)`.
var javaResponseEntityStatusRe = regexp.MustCompile(`ResponseEntity\s*\.\s*status\s*\(\s*(?:HttpStatus\.([A-Z_0-9]+)|(\d{3}))`)

// javaResponseEntityFactoryRe matches the ResponseEntity factory helpers that
// imply a fixed status: ok()→200, created(...)→201, accepted()→202,
// noContent()→204, notFound()→404, badRequest()→400.
var javaResponseEntityFactoryRe = regexp.MustCompile(`ResponseEntity\s*\.\s*(ok|created|accepted|noContent|notFound|badRequest|internalServerError|unprocessableEntity)\b`)

// javaResponseStatusExceptionRe matches `new ResponseStatusException(HttpStatus.X`
// and `throw new ResponseStatusException(404`.
var javaResponseStatusExceptionRe = regexp.MustCompile(`ResponseStatusException\s*\(\s*(?:HttpStatus\.([A-Z_0-9]+)|(\d{3}))`)

// javaResponseEntityFactoryCodes maps a ResponseEntity factory to its code.
var javaResponseEntityFactoryCodes = map[string]int{
	"ok":                  200,
	"created":             201,
	"accepted":            202,
	"noContent":           204,
	"badRequest":          400,
	"notFound":            404,
	"unprocessableEntity": 422,
	"internalServerError": 500,
}

func javaResponseCodes(region, body string) responseCodesVerdict {
	var v responseCodesVerdict

	// @ResponseStatus on the handler (decorator region).
	for _, m := range javaResponseStatusRe.FindAllStringSubmatch(region, -1) {
		if c, ok := springHTTPStatusCode(m[1]); ok {
			v.add(c)
			if v.source == "" {
				v.source = "@ResponseStatus"
			}
		}
	}
	for _, m := range javaResponseStatusNumericRe.FindAllStringSubmatch(region, -1) {
		if c, err := strconv.Atoi(m[1]); err == nil {
			v.add(c)
			if v.source == "" {
				v.source = "@ResponseStatus"
			}
		}
	}

	// ResponseEntity.status(...) in the body.
	for _, m := range javaResponseEntityStatusRe.FindAllStringSubmatch(body, -1) {
		if m[1] != "" {
			if c, ok := springHTTPStatusCode(m[1]); ok {
				v.add(c)
			}
		} else if m[2] != "" {
			if c, err := strconv.Atoi(m[2]); err == nil {
				v.add(c)
			}
		}
		if v.source == "" {
			v.source = "ResponseEntity.status"
		}
	}

	// ResponseEntity factory helpers in the body.
	for _, m := range javaResponseEntityFactoryRe.FindAllStringSubmatch(body, -1) {
		if c, ok := javaResponseEntityFactoryCodes[m[1]]; ok {
			v.add(c)
			if v.source == "" {
				v.source = "ResponseEntity." + m[1] + "()"
			}
		}
	}

	// ResponseStatusException in the body.
	for _, m := range javaResponseStatusExceptionRe.FindAllStringSubmatch(body, -1) {
		if m[1] != "" {
			if c, ok := springHTTPStatusCode(m[1]); ok {
				v.add(c)
			}
		} else if m[2] != "" {
			if c, err := strconv.Atoi(m[2]); err == nil {
				v.add(c)
			}
		}
		if v.source == "" {
			v.source = "ResponseStatusException"
		}
	}

	return v
}

// springHTTPStatusName maps Spring's HttpStatus enum names to their numeric
// codes. Covers the codes endpoints commonly return.
var springHTTPStatusName = map[string]int{
	"OK":                     200,
	"CREATED":                201,
	"ACCEPTED":               202,
	"NO_CONTENT":             204,
	"MOVED_PERMANENTLY":      301,
	"FOUND":                  302,
	"NOT_MODIFIED":           304,
	"BAD_REQUEST":            400,
	"UNAUTHORIZED":           401,
	"FORBIDDEN":              403,
	"NOT_FOUND":              404,
	"METHOD_NOT_ALLOWED":     405,
	"NOT_ACCEPTABLE":         406,
	"CONFLICT":               409,
	"GONE":                   410,
	"UNSUPPORTED_MEDIA_TYPE": 415,
	"UNPROCESSABLE_ENTITY":   422,
	"TOO_MANY_REQUESTS":      429,
	"INTERNAL_SERVER_ERROR":  500,
	"NOT_IMPLEMENTED":        501,
	"BAD_GATEWAY":            502,
	"SERVICE_UNAVAILABLE":    503,
}

// springHTTPStatusCode resolves an HttpStatus enum name (or a numeric-suffixed
// name) to its code. A pure numeric token is rejected here (handled by the
// numeric regexes); only the named enum constants resolve.
func springHTTPStatusCode(name string) (int, bool) {
	c, ok := springHTTPStatusName[name]
	return c, ok
}

// ---------------------------------------------------------------------------
// JS / TS — Express + Nest
// ---------------------------------------------------------------------------

// jsResStatusRe matches `res.status(201)` / `response.status(404)` with a numeric
// literal. A non-numeric argument (a variable) does not match — honest-partial.
var jsResStatusRe = regexp.MustCompile(`\.\s*status\s*\(\s*(\d{3})\s*\)`)

// jsResSendStatusRe matches `res.sendStatus(204)`.
var jsResSendStatusRe = regexp.MustCompile(`\.\s*sendStatus\s*\(\s*(\d{3})\s*\)`)

// jsStatusCodeAssignRe matches `res.statusCode = 201`.
var jsStatusCodeAssignRe = regexp.MustCompile(`\.\s*statusCode\s*=\s*(\d{3})\b`)

// nestHTTPCodeRe matches a Nest `@HttpCode(201)` decorator.
var nestHTTPCodeRe = regexp.MustCompile(`@HttpCode\s*\(\s*(\d{3})\s*\)`)

// nestThrowExceptionRe matches `throw new NotFoundException(` etc.; the class
// name is mapped to a code via nestExceptionCodes.
var nestThrowExceptionRe = regexp.MustCompile(`new\s+([A-Z][A-Za-z]*Exception)\b`)

// nestHTTPStatusRe matches `HttpStatus.CREATED` / `HttpStatus.NOT_FOUND` (Nest
// re-uses the same enum names as Spring).
var nestHTTPStatusRe = regexp.MustCompile(`HttpStatus\.([A-Z_0-9]+)`)

// nestExceptionCodes maps Nest's built-in HttpException subclasses to codes.
var nestExceptionCodes = map[string]int{
	"BadRequestException":           400,
	"UnauthorizedException":         401,
	"ForbiddenException":            403,
	"NotFoundException":             404,
	"MethodNotAllowedException":     405,
	"NotAcceptableException":        406,
	"RequestTimeoutException":       408,
	"ConflictException":             409,
	"GoneException":                 410,
	"PayloadTooLargeException":      413,
	"UnsupportedMediaTypeException": 415,
	"UnprocessableEntityException":  422,
	"TooManyRequestsException":      429,
	"InternalServerErrorException":  500,
	"NotImplementedException":       501,
	"BadGatewayException":           502,
	"ServiceUnavailableException":   503,
	"GatewayTimeoutException":       504,
}

func jsResponseCodes(region, body string) responseCodesVerdict {
	var v responseCodesVerdict

	// Nest @HttpCode in the decorator region.
	for _, m := range nestHTTPCodeRe.FindAllStringSubmatch(region, -1) {
		if c, err := strconv.Atoi(m[1]); err == nil {
			v.add(c)
			if v.source == "" {
				v.source = "@HttpCode"
			}
		}
	}

	// res.status(NNN) / res.sendStatus(NNN) / res.statusCode = NNN in the body.
	for _, re := range []*regexp.Regexp{jsResStatusRe, jsResSendStatusRe, jsStatusCodeAssignRe} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if c, err := strconv.Atoi(m[1]); err == nil {
				v.add(c)
				if v.source == "" {
					v.source = "res.status()"
				}
			}
		}
	}

	// Nest HttpStatus.X enum references in the body.
	for _, m := range nestHTTPStatusRe.FindAllStringSubmatch(body, -1) {
		if c, ok := springHTTPStatusCode(m[1]); ok {
			v.add(c)
			if v.source == "" {
				v.source = "HttpStatus.*"
			}
		}
	}

	// Nest exception throws → mapped code.
	for _, m := range nestThrowExceptionRe.FindAllStringSubmatch(body, -1) {
		if c, ok := nestExceptionCodes[m[1]]; ok {
			v.add(c)
			if v.source == "" {
				v.source = "Nest exception"
			}
		}
	}

	return v
}
