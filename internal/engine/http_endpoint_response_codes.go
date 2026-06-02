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
		// JAX-RS / Jakarta REST, Quarkus, Micronaut, MicroProfile, Helidon,
		// Dropwizard share the JVM but use a DIFFERENT response API than Spring
		// (jakarta.ws.rs Response builders + WebApplicationException subclasses;
		// Micronaut HttpResponse builders + @Status). Resolve those too and merge
		// (#3857). Spring + JAX-RS shapes are mutually exclusive in practice, so a
		// merge cannot double-count.
		v.merge(jaxrsResponseCodes(region+"\n"+sig, body))
		// Lightweight / reactive JVM family (Javalin / Vert.x / Akka-HTTP / Struts /
		// Spring WebFlux) use yet a DIFFERENT response API (ctx.status / setStatusCode /
		// complete(StatusCodes) / @Result names / ServerResponse builders). Resolve
		// those and merge (#3858); again mutually exclusive in practice per file.
		v.merge(reactiveResponseCodes(region+"\n"+sig, body))
	case "javascript":
		v.merge(jsResponseCodes(region, body))
	case "go":
		// Go route registration and handler are SEPARATE functions, so the
		// StartLine-anchored decorator/body windows above do not reach the
		// handler body. Locate the handler func by its `source_handler`
		// reference and scan its real body (mirrors response_shape_go.go).
		v.merge(goResponseCodes(content, e))
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

// ---------------------------------------------------------------------------
// Go — gin / echo / chi / fiber / net-http / gorilla / huma
// ---------------------------------------------------------------------------
//
// The status idioms differ by framework but all resolve to a literal int or a
// well-known package constant suffix (http.StatusXxx / fiber.StatusXxx). The
// constant suffix → code mapping is shared with response_shape_go.go's
// goHTTPStatusFromName, extended here with the full set of codes endpoints
// commonly return.
//
//   - gin / echo: c.JSON(http.StatusCreated, x) / c.JSON(201, x);
//     c.Status(http.StatusNoContent); c.AbortWithStatus(403);
//     c.AbortWithStatusJSON(400, ...); ctx.NoContent(http.StatusNoContent).
//   - echo error:  echo.NewHTTPError(http.StatusNotFound, ...) /
//     echo.NewHTTPError(404).
//   - net/http / chi (stdlib): w.WriteHeader(http.StatusCreated);
//     http.Error(w, msg, http.StatusBadRequest) (2nd/3rd arg is the code).
//   - fiber:       c.Status(fiber.StatusOK).JSON(x); c.SendStatus(204);
//     fiber.NewError(fiber.StatusNotFound, ...).
//
// HONEST-PARTIAL: a status expressed through a variable (`c.JSON(code, x)`,
// `w.WriteHeader(myStatus)`) is not a literal and is skipped — we still record
// the literals found alongside. If no literal resolves, response_codes is left
// absent (the framework default 200 is NOT fabricated).

// goStatusCallRe matches the call shapes whose FIRST argument is the HTTP
// status: c.JSON / c.IndentedJSON / c.PureJSON / c.XML / c.Status / c.String /
// c.Data / c.AbortWithStatus / c.AbortWithStatusJSON / c.SendStatus /
// w.WriteHeader / ctx.NoContent / echo.NewHTTPError / fiber.NewError.
var goStatusFirstArgRe = regexp.MustCompile(
	`\b\w+\s*\.\s*(?:JSON|IndentedJSON|PureJSON|SecureJSON|AsciiJSON|XML|YAML|ProtoBuf|Status|String|Data|HTML|Render|AbortWithStatus|AbortWithStatusJSON|SendStatus|WriteHeader|NoContent|NewHTTPError|NewError)\s*\(\s*(\d{3}|http\.Status[A-Z][A-Za-z]+|fiber\.Status[A-Z][A-Za-z]+|echo\.[A-Z][A-Za-z]+)`,
)

// goHTTPErrorRe matches `http.Error(w, msg, http.StatusBadRequest)` /
// `http.Error(w, msg, 400)` where the status is the THIRD argument.
var goHTTPErrorRe = regexp.MustCompile(
	`\bhttp\.Error\s*\(\s*[^,]+,\s*[^,]+,\s*(\d{3}|http\.Status[A-Z][A-Za-z]+)\s*\)`,
)

// goStatusTokenRe parses a single resolved status token into either a numeric
// literal (group 1) or a constant suffix (group 2), accepting the http.Status*
// and fiber.Status* constant families (which share the stdlib code values).
var goStatusTokenRe = regexp.MustCompile(`^(?:(\d{3})|(?:http|fiber)\.Status([A-Z][A-Za-z]+))$`)

// goResponseCodes resolves the literal status-code set returned by a Go
// handler. The handler body is located via the endpoint's source_handler
// reference (route registration and handler are separate funcs in Go).
func goResponseCodes(content string, e *types.EntityRecord) responseCodesVerdict {
	var v responseCodesVerdict
	handler := e.Properties["source_handler"]
	if idx := strings.Index(handler, ":"); idx >= 0 {
		handler = handler[idx+1:]
	}
	if handler == "" {
		return v
	}
	body := findGoHandlerBody(content, handler)
	if body == "" {
		return v
	}

	for _, m := range goStatusFirstArgRe.FindAllStringSubmatch(body, -1) {
		if c, ok := goResolveStatusToken(m[1]); ok {
			v.add(c)
			if v.source == "" {
				v.source = "status call"
			}
		}
	}
	for _, m := range goHTTPErrorRe.FindAllStringSubmatch(body, -1) {
		if c, ok := goResolveStatusToken(m[1]); ok {
			v.add(c)
			if v.source == "" {
				v.source = "http.Error"
			}
		}
	}
	return v
}

// goResolveStatusToken resolves a status token (a 3-digit literal or an
// http.Status*/fiber.Status* constant) to its numeric code. An `echo.X`
// constant is a non-status echo symbol and is rejected (returns false).
func goResolveStatusToken(tok string) (int, bool) {
	tok = strings.TrimSpace(tok)
	m := goStatusTokenRe.FindStringSubmatch(tok)
	if m == nil {
		return 0, false
	}
	if m[1] != "" {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n, true
		}
		return 0, false
	}
	if c := goStatusConstCode(m[2]); c > 0 {
		return c, true
	}
	return 0, false
}

// goStatusConstCode maps the net/http (and fiber, which mirrors it) status
// constant suffix to its numeric code. Superset of response_shape_go.go's
// goHTTPStatusFromName, covering the codes endpoints commonly return.
func goStatusConstCode(name string) int {
	if c, ok := goStatusConstCodes[name]; ok {
		return c
	}
	return 0
}

var goStatusConstCodes = map[string]int{
	"Continue":              100,
	"OK":                    200,
	"Created":               201,
	"Accepted":              202,
	"NonAuthoritativeInfo":  203,
	"NoContent":             204,
	"ResetContent":          205,
	"PartialContent":        206,
	"MovedPermanently":      301,
	"Found":                 302,
	"SeeOther":              303,
	"NotModified":           304,
	"TemporaryRedirect":     307,
	"PermanentRedirect":     308,
	"BadRequest":            400,
	"Unauthorized":          401,
	"PaymentRequired":       402,
	"Forbidden":             403,
	"NotFound":              404,
	"MethodNotAllowed":      405,
	"NotAcceptable":         406,
	"RequestTimeout":        408,
	"Conflict":              409,
	"Gone":                  410,
	"PreconditionFailed":    412,
	"RequestEntityTooLarge": 413,
	"UnsupportedMediaType":  415,
	"UnprocessableEntity":   422,
	"TooManyRequests":       429,
	"InternalServerError":   500,
	"NotImplemented":        501,
	"BadGateway":            502,
	"ServiceUnavailable":    503,
	"GatewayTimeout":        504,
}
