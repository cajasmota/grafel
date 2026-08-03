// endpoint_response_codes.go — HTTP status-code-set stamping for Rust web
// frameworks (#4965, child of epic #3628 cross-language fan-out, Routing/
// endpoint_response_codes). Sibling of endpoint_deprecation.go.
//
// Rust greenfield: prior to this pass every Rust HTTP-framework cell for
// endpoint_response_codes was `missing` (13/13). The flagship engine pass
// (internal/engine/http_endpoint_response_codes.go, applyEndpointResponseCodes)
// stamps a flat property contract on synthesised `http_endpoint_definition`
// entities — but Rust HTTP endpoints are emitted as `SCOPE.Operation/endpoint`
// entities by the custom .rs route extractors (axum.go `.route("/p", verb(h))`,
// actix_web.go `#[get("/p")]`, rocket.go `#[get("/p")]`), so the engine pass —
// gated on Kind==http_endpoint_definition — can never reach them. This is the
// SAME situation endpoint_deprecation.go faced; the resolution is identical:
// re-emit the endpoint op carrying the status-code contract from the framework's
// own idioms, merging onto the producer route op via MergeWithCustom, which
// keys on (SourceFile, Kind, Name) since #6104.
//
// Property contract (mirrors the flagship http_endpoint_response_codes.go):
//
//	response_codes        — comma-joined, sorted, unique list of HTTP status codes
//	                        the handler can return (e.g. "201,404"); present only
//	                        when at least one literal code was resolved.
//	success_code          — the single 2xx code on the happy path, when exactly
//	                        one 2xx code is present (e.g. "201"); omitted when zero
//	                        or several 2xx codes are present (ambiguous).
//	response_codes_source — the dominant signal that fired (evidence).
//
// Three recognised Rust surfaces (Names match the producer extractors so the
// stamped op merges onto the plain route op by Name):
//
//	axum — `.route("/path", verb(handler))`. The route names a handler by symbol;
//	    the status idioms (`StatusCode::CREATED`, a `(StatusCode::NOT_FOUND, …)`
//	    tuple return, `StatusCode::from_u16(404)`) live in the `fn handler` body
//	    ELSEWHERE in the file, so we build a handler→verdict map from every fn body
//	    and attach it to the routes that name that handler. Path is the composed
//	    (nest-prefixed) route literal; Name is `METHOD fullPath`.
//
//	actix-web — `#[get("/path")]` directly above `fn handler`. Status comes from
//	    the `HttpResponse::Created()` / `.status(StatusCode::X)` builders in the fn
//	    body (the fn that follows the macro).
//
//	rocket — `#[get("/path")]` + `.mount("/p", routes![…])`. Status comes from a
//	    `Status::Created` / `status::Custom(Status::NotFound, …)` in the fn body.
//
// Recognised Rust status idioms (each contributes one literal code; honest-
// partial — a status through a variable is NOT a literal and is skipped):
//
//	StatusCode::CREATED / StatusCode::NOT_FOUND … — the axum/http enum constant.
//	    Resolved via the enum-name → code table (mirrors goStatusConstCodes).
//	Status::Created / Status::NotFound … — the rocket enum constant (CamelCase
//	    variant of the same names).
//	StatusCode::from_u16(404) / Status::new(201) — a numeric literal constructor.
//	HttpResponse::Created() / HttpResponse::NotFound() … — the actix builder
//	    whose method name names the status (Ok→200, Created→201, …).
//	.status(StatusCode::X) / .status(404) — an actix/axum status setter.
//
// Honest-partial (NEVER fabricated): a handler with NO resolvable literal status
// is NOT re-emitted (the plain route op from the producer extractor stands — the
// framework default 200 is never fabricated); a dynamic status
// (`StatusCode::from_u16(code)`, `.status(my_status)`) is skipped while any
// sibling literal in the same body is still recorded. success_code is omitted
// when the 2xx set is empty or ambiguous (>1).
//
// Honesty: partial — heuristic regex on the handler body, scoped to the
// framework's own route idioms so an unrelated `StatusCode::X` on a non-route
// helper fn is not mis-attributed to an endpoint.
//
// Refs #4965, #3628.
package rust

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_rust_endpoint_response_codes", &rustEndpointResponseCodesExtractor{})
}

type rustEndpointResponseCodesExtractor struct{}

func (e *rustEndpointResponseCodesExtractor) Language() string {
	return "custom_rust_endpoint_response_codes"
}

// --- status-code resolution ---------------------------------------------------

// rustStatusEnumConstRe matches an `StatusCode::CREATED` / `Status::Created`
// enum reference. Group 1 = the variant name (SCREAMING_SNAKE for http's
// StatusCode, CamelCase for rocket's Status). Both resolve through the same
// name table (the table holds both spellings).
var rustStatusEnumConstRe = regexp.MustCompile(`\b(?:StatusCode|Status)::([A-Za-z_][A-Za-z0-9_]*)\b`)

// rustStatusFromNumRe matches a numeric status constructor:
// `StatusCode::from_u16(404)`, `Status::new(201)`, `Status::from_code(204)`.
// Group 1 = the 3-digit literal.
var rustStatusFromNumRe = regexp.MustCompile(`\b(?:StatusCode|Status)::(?:from_u16|new|from_code)\s*\(\s*(\d{3})\b`)

// rustStatusSetterRe matches a `.status(StatusCode::X)` / `.status(404)` setter
// (actix `HttpResponseBuilder::status`, axum `Response::status`). It also matches
// salvo's `.status_code(StatusCode::X)` (`res.status_code(...)`) and poem/tide's
// `.set_status(StatusCode::X)` / `.set_status(404)` via the optional
// `_code`/`set_` affixes. Group 1 = enum variant (optional), group 2 = numeric
// literal (optional).
var rustStatusSetterRe = regexp.MustCompile(`\.\s*(?:set_)?status(?:_code)?\s*\(\s*(?:(?:StatusCode|Status)::([A-Za-z_][A-Za-z0-9_]*)|(\d{3}))`)

// rustResponseBuilderNumRe matches tide's `Response::builder(201)` /
// `Response::new(404)` numeric status constructor (tide passes the status as the
// first positional arg). Group 1 = the 3-digit literal.
var rustResponseBuilderNumRe = regexp.MustCompile(`\bResponse::(?:builder|new)\s*\(\s*(\d{3})\b`)

// rustHyperRespArmRe matches a hyper match-arm route that DISPATCHES TO A NAMED
// handler: `(&Method::GET, "/path") => get_users(req).await`. Group 1 = verb,
// group 2 = path, group 3 = the handler-call symbol on the arm RHS (the bare
// fn-name preceding the first `(`). This mirrors the producer reHyperMatchArm
// (minor_fw_routing.go) but additionally captures the RHS handler so the resolved
// verdict can be looked up in the shared handler→verdict map, exactly as axum
// names a handler from `verb(handler)`. Inline-block arms
// (`… => { Response::builder()… }`) are NOT matched here — they carry no named
// handler and are handled by rustHyperRespInlineArmRe below.
var rustHyperRespArmRe = regexp.MustCompile(
	`&Method::(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*,\s*"([^"]+)"\s*\)?\s*=>\s*([A-Za-z_]\w*)\s*\(`,
)

// rustHyperRespInlineArmRe matches a hyper match-arm route whose body is an INLINE
// BLOCK: `(&Method::POST, "/path") => { … }`. Group 1 = verb, group 2 = path; the
// block body (from the `{`) is resolved directly via rustResolveResponseCodes, so
// a status idiom written inline in the arm (no separate handler fn) is still
// recovered. The match stops at the opening brace; the bounded body window is cut
// at the next sibling `fn`/arm boundary by rustRespBodyWindow.
var rustHyperRespInlineArmRe = regexp.MustCompile(
	`&Method::(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*,\s*"([^"]+)"\s*\)?\s*=>\s*\{`,
)

// rustHyperArmBoundaryRe matches the start of the NEXT match arm — either a
// typed `(&Method::VERB, …)` arm or the `_ =>` wildcard/fallback arm — so an
// inline arm's body window is hard-clipped before a sibling arm's status literals.
var rustHyperArmBoundaryRe = regexp.MustCompile(`&Method::(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)|(?m)^\s*_\s*=>`)

// rustHttpResponseBuilderRe matches the actix `HttpResponse::Created()` /
// `HttpResponse::Ok()` … builder whose method names the status. Group 1 = the
// builder name. `HttpResponse::build(...)` (dynamic) is excluded by the table.
var rustHttpResponseBuilderRe = regexp.MustCompile(`\bHttpResponse::([A-Z][A-Za-z]+)\b`)

// rustStatusNameCodes maps the http/axum StatusCode SCREAMING_SNAKE variant
// names AND the rocket Status CamelCase variant names to their numeric codes.
// Both spellings of each status are present so a single lookup resolves either
// framework. (Mirrors the flagship springHTTPStatusName / goStatusConstCodes.)
var rustStatusNameCodes = map[string]int{
	// http::StatusCode (SCREAMING_SNAKE)
	"CONTINUE":               100,
	"OK":                     200,
	"CREATED":                201,
	"ACCEPTED":               202,
	"NO_CONTENT":             204,
	"RESET_CONTENT":          205,
	"PARTIAL_CONTENT":        206,
	"MOVED_PERMANENTLY":      301,
	"FOUND":                  302,
	"SEE_OTHER":              303,
	"NOT_MODIFIED":           304,
	"TEMPORARY_REDIRECT":     307,
	"PERMANENT_REDIRECT":     308,
	"BAD_REQUEST":            400,
	"UNAUTHORIZED":           401,
	"PAYMENT_REQUIRED":       402,
	"FORBIDDEN":              403,
	"NOT_FOUND":              404,
	"METHOD_NOT_ALLOWED":     405,
	"NOT_ACCEPTABLE":         406,
	"REQUEST_TIMEOUT":        408,
	"CONFLICT":               409,
	"GONE":                   410,
	"PRECONDITION_FAILED":    412,
	"PAYLOAD_TOO_LARGE":      413,
	"UNSUPPORTED_MEDIA_TYPE": 415,
	"UNPROCESSABLE_ENTITY":   422,
	"TOO_MANY_REQUESTS":      429,
	"INTERNAL_SERVER_ERROR":  500,
	"NOT_IMPLEMENTED":        501,
	"BAD_GATEWAY":            502,
	"SERVICE_UNAVAILABLE":    503,
	"GATEWAY_TIMEOUT":        504,
	// rocket::http::Status (CamelCase) — actix builder method names share these
	"Continue":             100,
	"Ok":                   200,
	"Created":              201,
	"Accepted":             202,
	"NoContent":            204,
	"ResetContent":         205,
	"PartialContent":       206,
	"MovedPermanently":     301,
	"Found":                302,
	"SeeOther":             303,
	"NotModified":          304,
	"TemporaryRedirect":    307,
	"PermanentRedirect":    308,
	"BadRequest":           400,
	"Unauthorized":         401,
	"PaymentRequired":      402,
	"Forbidden":            403,
	"NotFound":             404,
	"MethodNotAllowed":     405,
	"NotAcceptable":        406,
	"RequestTimeout":       408,
	"Conflict":             409,
	"Gone":                 410,
	"PreconditionFailed":   412,
	"PayloadTooLarge":      413,
	"UnsupportedMediaType": 415,
	"UnprocessableEntity":  422,
	"TooManyRequests":      429,
	"InternalServerError":  500,
	"NotImplemented":       501,
	"BadGateway":           502,
	"ServiceUnavailable":   503,
	"GatewayTimeout":       504,
}

// rustRespCodesVerdict is the resolved status-code set for one endpoint.
type rustRespCodesVerdict struct {
	codes  map[int]bool
	source string
}

func (v *rustRespCodesVerdict) add(code int, source string) {
	if code < 100 || code > 599 {
		return
	}
	if v.codes == nil {
		v.codes = map[int]bool{}
	}
	v.codes[code] = true
	if v.source == "" {
		v.source = source
	}
}

// rustResolveResponseCodes inspects a handler body window for the recognised
// Rust status idioms. Honest-partial: no literal → empty verdict.
func rustResolveResponseCodes(body string) rustRespCodesVerdict {
	var v rustRespCodesVerdict
	if body == "" {
		return v
	}

	// HttpResponse::Created() / ::Ok() … actix builders (most specific first so
	// its evidence label wins).
	for _, m := range rustHttpResponseBuilderRe.FindAllStringSubmatch(body, -1) {
		if c, ok := rustStatusNameCodes[m[1]]; ok {
			v.add(c, "HttpResponse::"+m[1]+"()")
		}
	}

	// StatusCode::from_u16(NNN) / Status::new(NNN) numeric constructors.
	for _, m := range rustStatusFromNumRe.FindAllStringSubmatch(body, -1) {
		if c, err := strconv.Atoi(m[1]); err == nil {
			v.add(c, "StatusCode::from_u16")
		}
	}

	// tide Response::builder(NNN) / Response::new(NNN) positional status.
	for _, m := range rustResponseBuilderNumRe.FindAllStringSubmatch(body, -1) {
		if c, err := strconv.Atoi(m[1]); err == nil {
			v.add(c, "Response::builder()")
		}
	}

	// .status(StatusCode::X) / .status(404) setters.
	for _, m := range rustStatusSetterRe.FindAllStringSubmatch(body, -1) {
		if m[1] != "" {
			if c, ok := rustStatusNameCodes[m[1]]; ok {
				v.add(c, ".status()")
			}
		} else if m[2] != "" {
			if c, err := strconv.Atoi(m[2]); err == nil {
				v.add(c, ".status()")
			}
		}
	}

	// Bare StatusCode::X / Status::X enum references (tuple returns
	// `(StatusCode::NOT_FOUND, body)`, `Ok(StatusCode::CREATED)`, …). Resolved
	// LAST so a more specific idiom above owns the evidence label.
	for _, m := range rustStatusEnumConstRe.FindAllStringSubmatch(body, -1) {
		if c, ok := rustStatusNameCodes[m[1]]; ok {
			v.add(c, "StatusCode::"+m[1])
		}
	}

	return v
}

// rustSortedCodes returns the ascending unique status codes.
func rustSortedCodes(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

// rustSingleSuccessCode returns the lone 2xx code in a sorted list, and whether
// exactly one 2xx code is present (so success_code is unambiguous). Mirrors the
// flagship singleSuccessCode.
func rustSingleSuccessCode(sorted []int) (int, bool) {
	found := -1
	for _, c := range sorted {
		if c >= 200 && c < 300 {
			if found != -1 {
				return 0, false
			}
			found = c
		}
	}
	if found == -1 {
		return 0, false
	}
	return found, true
}

// rustStampResponseCodes writes the flat status-code contract onto an endpoint
// entity from a resolved verdict. No-op when no literal code was resolved.
func rustStampResponseCodes(e *types.EntityRecord, v rustRespCodesVerdict) bool {
	if len(v.codes) == 0 {
		return false
	}
	sorted := rustSortedCodes(v.codes)
	strs := make([]string, len(sorted))
	for j, c := range sorted {
		strs[j] = strconv.Itoa(c)
	}
	setProps(e, "response_codes", strings.Join(strs, ","))
	if sc, ok := rustSingleSuccessCode(sorted); ok {
		setProps(e, "success_code", strconv.Itoa(sc))
	}
	if v.source != "" {
		setProps(e, "response_codes_source", v.source)
	}
	return true
}

// --- extractor entry point ----------------------------------------------------

func (e *rustEndpointResponseCodesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_endpoint_response_codes.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard: a status-code surface must mention a status idiom. Includes
	// the minor-framework idioms (#5018): `.status_code(` (salvo), `.set_status(`
	// (poem/tide), and tide's `Response::builder(NNN)` positional status.
	if !strings.Contains(src, "StatusCode") && !strings.Contains(src, "Status::") &&
		!strings.Contains(src, "HttpResponse::") && !strings.Contains(src, ".status(") &&
		!strings.Contains(src, ".status_code(") && !strings.Contains(src, ".set_status(") &&
		!strings.Contains(src, "Response::builder(") && !strings.Contains(src, "Response::new(") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	for _, ent := range e.extractAxum(src, file) {
		add(ent)
	}
	for _, ent := range e.extractMacroFramework(src, file, "actix_web") {
		add(ent)
	}
	for _, ent := range e.extractMacroFramework(src, file, "rocket") {
		add(ent)
	}
	// #5018 — remaining handler-named HTTP frameworks. Each re-runs its producer
	// extractor's route regexes and attaches the resolved verdict to the handler
	// the route names (same shared StatusCode table + handler→verdict map as axum).
	for _, ent := range e.extractHandlerNamed(src, file) {
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// --- axum surface -------------------------------------------------------------

// extractAxum re-emits the status-code-stamped endpoint op for every
// `.route("/p", verb(handler))` whose handler fn body resolves at least one
// literal status code. The Name matches axum.go (`METHOD fullPath`) so the
// stamped op merges onto the plain route op.
func (e *rustEndpointResponseCodesExtractor) extractAxum(src string, file extractor.FileInput) []types.EntityRecord {
	if !strings.Contains(src, ".route") && !strings.Contains(src, ".nest") {
		return nil
	}

	// Build handler-name → verdict from every fn body.
	handlerCodes := map[string]rustRespCodesVerdict{}
	for _, fm := range rustDepFnRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[fm[2]:fm[3]]
		body := rustRespBodyWindow(src, fm[1])
		if v := rustResolveResponseCodes(body); len(v.codes) > 0 {
			handlerCodes[name] = v
		}
	}
	if len(handlerCodes) == 0 {
		return nil
	}

	// Recompute the nest-prefix map exactly as axum.go does so Names match.
	nestPrefix := map[string]string{}
	for _, m := range reAxumNest.FindAllStringSubmatchIndex(src, -1) {
		nestPrefix[src[m[4]:m[5]]] = rustNormalizePath(src[m[2]:m[3]])
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, m := range reAxumRoute.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		methodRouter := src[m[4]:m[5]]
		prefix := axumRouteNestPrefix(src, m[0], nestPrefix)
		fullPath := rustJoinPaths(prefix, path)
		for _, vm := range reAxumMethodRouter.FindAllStringSubmatch(methodRouter, -1) {
			method := strings.ToUpper(vm[1])
			handler := vm[2]
			name := method + " " + fullPath
			if seen[name] {
				continue
			}
			verdict, ok := handlerCodes[handler]
			if !ok {
				continue // leave the plain route op to axum.go
			}
			seen[name] = true
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_RESPONSE_CODES",
				"http_method", method, "route_path", fullPath, "handler_name", handler)
			if prefix != "" {
				setProps(&ent, "nest_prefix", prefix)
			}
			rustStampResponseCodes(&ent, verdict)
			out = append(out, ent)
		}
	}
	return out
}

// --- actix-web / rocket macro surface -----------------------------------------

// extractMacroFramework re-emits the status-code-stamped endpoint op for every
// actix/rocket attribute-macro route whose handler-fn body resolves at least one
// literal status code. Names match the producer extractors so the stamped op
// merges. Path composition mirrors endpoint_deprecation.go (rocket mount prefix;
// actix macro paths are NOT scope-prefixed).
func (e *rustEndpointResponseCodesExtractor) extractMacroFramework(src string, file extractor.FileInput, framework string) []types.EntityRecord {
	switch framework {
	case "actix_web":
		if !strings.Contains(src, "actix") && !strings.Contains(src, "HttpResponse") &&
			!strings.Contains(src, "web::") && !strings.Contains(src, "Responder") {
			return nil
		}
	case "rocket":
		if !strings.Contains(src, "rocket") && !strings.Contains(src, "routes!") &&
			!strings.Contains(src, "#[launch]") {
			return nil
		}
	}

	// rocket: handler → mount prefix (mirrors rocket.go mountPrefix).
	mountPrefix := map[string]string{}
	if framework == "rocket" {
		for _, mm := range reRocketMount.FindAllStringSubmatch(src, -1) {
			prefix := rustNormalizePath(mm[1])
			for _, h := range strings.Split(mm[2], ",") {
				h = strings.TrimSpace(h)
				if idx := strings.LastIndex(h, "::"); idx >= 0 {
					h = h[idx+2:]
				}
				if h != "" {
					mountPrefix[h] = prefix
				}
			}
		}
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, m := range rustMacroVerbRe.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := rustNormalizePath(src[m[4]:m[5]])

		handler, bodyStart := rustFnAfter(src, m[1])
		if bodyStart < 0 {
			continue
		}

		fullPath := path
		if framework == "rocket" {
			fullPath = rustJoinPaths(mountPrefix[handler], path)
		}
		name := method + " " + fullPath
		if seen[name] {
			continue
		}

		verdict := rustResolveResponseCodes(rustRespBodyWindow(src, bodyStart))
		if len(verdict.codes) == 0 {
			continue
		}
		seen[name] = true

		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework,
			"provenance", "INFERRED_FROM_"+strings.ToUpper(framework)+"_RESPONSE_CODES",
			"http_method", method, "route_pattern", fullPath)
		if handler != "" {
			setProps(&ent, "handler_name", handler)
		}
		if framework == "rocket" && mountPrefix[handler] != "" {
			setProps(&ent, "mount_prefix", mountPrefix[handler])
		}
		rustStampResponseCodes(&ent, verdict)
		out = append(out, ent)
	}
	return out
}

// rustNextFnRe matches the start of the NEXT `fn` definition (a sibling handler)
// so the body window can be clipped at it — status literals declared on a later
// handler in the same file must not be pooled into this endpoint's
// response_codes. Mirrors the flagship trimPythonHandlerBody boundary cut.
var rustNextFnRe = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:async\s+)?fn\s+\w+`)

// rustRespBodyWindow returns a bounded window of a handler body, clipped at the
// next sibling `fn` definition. A handler can return several status codes spread
// across its body, so the cap (2500 bytes, mirroring the flagship
// handlerBodyWindowLarge) is generous — but it is hard-clipped at the next fn so
// a sibling handler's status literals never bleed in.
func rustRespBodyWindow(src string, bodyStart int) string {
	if bodyStart < 0 || bodyStart >= len(src) {
		return ""
	}
	end := bodyStart + 2500
	if end > len(src) {
		end = len(src)
	}
	window := src[bodyStart:end]
	if loc := rustNextFnRe.FindStringIndex(window); loc != nil {
		window = window[:loc[0]]
	}
	return window
}

// --- #5018: remaining handler-named HTTP frameworks ---------------------------
//
// poem / warp / tide / gotham / salvo all attribute a route to a named handler
// function whose body lives ELSEWHERE in the file (exactly the axum situation).
// The recipe is therefore identical to extractAxum: build a handler→verdict map
// from every fn body once, then re-run each framework's PRODUCER route regexes
// (the ones in minor_fw_routing.go) and stamp the verdict onto the routes that
// name a resolving handler. Reusing the producer regexes guarantees the emitted
// Name (`METHOD path`) merges onto the plain producer route op by Name.
//
// Per-framework status idioms (all flow through rustResolveResponseCodes):
//
//	poem   — `Response::builder().status(StatusCode::X)`, a bare `StatusCode::X`
//	         return, or `.set_status(...)`. (poem re-exports http::StatusCode.)
//	warp   — `warp::reply::with_status(reply, StatusCode::CREATED)` — the bare
//	         StatusCode::X enum ref in the handler body is resolved.
//	tide   — `Response::builder(201)` / `Response::new(404)` positional status,
//	         or `resp.set_status(201)`.
//	gotham — a `(StatusCode::X, body)` create_response / bare StatusCode::X.
//	salvo  — `res.status_code(StatusCode::CREATED)`.
//	hyper  — a `match (req.method(), path) { (&Method::GET, "/p") => handler(req) }`
//	         match-arm. The arm RHS NAMES a handler whose body lives elsewhere
//	         (resolved via the shared handler→verdict map, exactly like axum); an
//	         INLINE-block arm (`=> { Response::builder().status(StatusCode::X) }`)
//	         is resolved directly from the arm body window.
//
// tower (a middleware/service-composition layer that defines no verb+path routes —
// `route_extraction` is structurally partial for it) has no endpoint to stamp and
// is therefore not_applicable for endpoint_response_codes. Honest-partial +
// no-fabrication hold: a route whose handler resolves no literal status is left to
// the producer.
func (e *rustEndpointResponseCodesExtractor) extractHandlerNamed(src string, file extractor.FileInput) []types.EntityRecord {
	// Build handler-name → verdict from every fn body once (shared across all
	// frameworks below — a handler is resolved the same way regardless of which
	// framework's route happens to name it).
	handlerCodes := map[string]rustRespCodesVerdict{}
	for _, fm := range rustDepFnRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[fm[2]:fm[3]]
		body := rustRespBodyWindow(src, fm[1])
		if v := rustResolveResponseCodes(body); len(v.codes) > 0 {
			handlerCodes[name] = v
		}
	}
	if len(handlerCodes) == 0 {
		return nil
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	// emit stamps one endpoint op carrying the verdict for `handler`, keyed by
	// the producer's `METHOD path` Name so it merges. No-op when the handler had
	// no resolvable literal status (honest-partial).
	emit := func(framework, method, path, handler string, off int) {
		verdict, ok := handlerCodes[handler]
		if !ok {
			return
		}
		name := method + " " + path
		if seen[name] {
			return
		}
		seen[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, off))
		setProps(&ent, "framework", framework,
			"provenance", "INFERRED_FROM_"+strings.ToUpper(framework)+"_RESPONSE_CODES",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		rustStampResponseCodes(&ent, verdict)
		out = append(out, ent)
	}

	// poem — `.at("/path", get(handler).post(h2))`.
	if strings.Contains(src, ".at") {
		for _, m := range rePoemAt.FindAllStringSubmatchIndex(src, -1) {
			path := rustNormalizePath(src[m[2]:m[3]])
			methodRouter := src[m[4]:m[5]]
			for _, vm := range rePoemVerb.FindAllStringSubmatch(methodRouter, -1) {
				emit("poem", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
	}

	// tide — `app.at("/path").get(a).post(b)`.
	if strings.Contains(src, ".at") {
		for _, m := range reTideAt.FindAllStringSubmatchIndex(src, -1) {
			path := rustNormalizePath(src[m[2]:m[3]])
			verbChain := src[m[4]:m[5]]
			for _, vm := range reTideVerb.FindAllStringSubmatch(verbChain, -1) {
				emit("tide", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
	}

	// gotham — `route.get("/path").to(handler)` and `.associate("/p", |a| {…})`.
	if strings.Contains(src, "route.") || strings.Contains(src, ".associate") {
		for _, m := range reGothamRoute.FindAllStringSubmatchIndex(src, -1) {
			emit("gotham", strings.ToUpper(src[m[2]:m[3]]), rustNormalizePath(src[m[4]:m[5]]), src[m[6]:m[7]], m[0])
		}
		for _, m := range reGothamAssociate.FindAllStringSubmatchIndex(src, -1) {
			path := rustNormalizePath(src[m[2]:m[3]])
			body := src[m[4]:m[5]]
			for _, vm := range reGothamAssocVerb.FindAllStringSubmatch(body, -1) {
				emit("gotham", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
	}

	// salvo — `Router::with_path("p").get(handler)` verb chains. Mirrors the
	// producer path composition (with_path prefix + optional .path segment).
	if strings.Contains(src, "Router") || strings.Contains(src, "router") {
		for _, m := range reSalvoPath.FindAllStringSubmatchIndex(src, -1) {
			var withPath, dotPath string
			if m[2] >= 0 {
				withPath = rustNormalizePath(src[m[2]:m[3]])
			}
			if m[4] >= 0 {
				dotPath = rustNormalizePath(src[m[4]:m[5]])
			}
			path := rustJoinPaths(withPath, dotPath)
			if path != "" && !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			verbChain := src[m[6]:m[7]]
			for _, vm := range reSalvoVerb.FindAllStringSubmatch(verbChain, -1) {
				emit("salvo", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
	}

	// warp — filter chain `warp::path...(...).and_then(handler)`/`.map(handler)`.
	// Path/method recovered from the chain blob exactly as the producer does.
	if strings.Contains(src, "warp::") {
		for _, m := range reWarpChain.FindAllStringSubmatchIndex(src, -1) {
			blob := src[m[0]:m[1]]
			handler := src[m[2]:m[3]]
			method := "GET"
			if mm := reWarpChainMethod.FindStringSubmatch(blob); mm != nil {
				method = strings.ToUpper(mm[1])
			}
			path := ""
			if pm := reWarpPathMacroIn.FindStringSubmatch(blob); pm != nil {
				path = normWarpPath(pm[1])
			} else if pf := reWarpPathFn.FindStringSubmatch(blob); pf != nil {
				path = "/" + strings.Trim(pf[1], "/")
			}
			if path == "" {
				continue
			}
			emit("warp", method, path, handler, m[0])
		}
	}

	// hyper — `match (req.method(), path) { (&Method::GET, "/p") => handler(req) }`.
	// The arm RHS NAMES a handler resolved via the shared handler→verdict map (the
	// named-handler form), OR the arm is an INLINE block whose body carries the
	// status idiom directly (the inline form). Both honour honest-partial.
	if strings.Contains(src, "Method::") {
		// Named-handler arms: `=> handler(req)`.
		for _, m := range rustHyperRespArmRe.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[m[2]:m[3]])
			path := rustNormalizePath(src[m[4]:m[5]])
			handler := src[m[6]:m[7]]
			emit("hyper", method, path, handler, m[0])
		}
		// Inline-block arms: `=> { … StatusCode::X … }`. Resolve the arm body
		// window directly (no named handler), then stamp by the producer Name.
		for _, m := range rustHyperRespInlineArmRe.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[m[2]:m[3]])
			path := rustNormalizePath(src[m[4]:m[5]])
			name := method + " " + path
			if seen[name] {
				continue
			}
			// m[1] is just past the matched `{`; resolve the block body window,
			// clipped at the NEXT match arm so a sibling arm's status never bleeds in.
			window := rustRespBodyWindow(src, m[1])
			if loc := rustHyperArmBoundaryRe.FindStringIndex(window); loc != nil {
				window = window[:loc[0]]
			}
			verdict := rustResolveResponseCodes(window)
			if len(verdict.codes) == 0 {
				continue
			}
			seen[name] = true
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "hyper",
				"provenance", "INFERRED_FROM_HYPER_RESPONSE_CODES",
				"http_method", method, "route_pattern", path)
			rustStampResponseCodes(&ent, verdict)
			out = append(out, ent)
		}
	}

	return out
}
