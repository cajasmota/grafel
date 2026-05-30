// Package scala — tapir endpoint-DSL extraction.
//
// tapir (https://tapir.softwaremill.com) describes HTTP endpoints as first-class
// `Endpoint` values that are independent of the serving backend (akka-http,
// pekko-http, http4s, netty, …). A single endpoint value carries the method,
// the path template, the input/output DTO references and the error DTO:
//
//	val getUser =
//	  endpoint
//	    .get
//	    .in("users" / path[Long]("id"))
//	    .in(query[String]("q"))
//	    .out(jsonBody[User])
//	    .errorOut(jsonBody[ErrorInfo])
//	    .serverLogic(handler)
//
// Because the routing and the request/response shape both live in the endpoint
// chain — regardless of which backend interprets it — tapir is the highest-value
// Scala framework to model structurally.
//
// This extractor parses each `endpoint`(.get/.post/…) chain into:
//   - one SCOPE.Operation/http_route entity carrying http_method + http_path,
//     where the path template is composed from `.in("seg" / path[T]("name") /
//     query[T]("q"))` (literal segs + path[T] → {name}; query params recorded
//     separately as query_params).
//   - DTO references for jsonBody[T] inputs (request DTO), outputs (response
//     DTO) and errorOut (error DTO).
//   - a handler attribution when `.serverLogic(handler)` /
//     `.serverLogicSuccess(handler)` is present.
//
// All matching is regex-based and file-local — the same honest limit as the
// other Scala framework extractors in frameworks.go. Issue #3507.
package scala

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

var (
	// tapirSignalRe gates the framework on a tapir-specific import or the
	// `sttp.tapir` package, so a plain akka/http4s file is never mis-labelled.
	tapirSignalRe = regexp.MustCompile(
		`\bsttp\.tapir\b|\bimport\s+sttp\.tapir|\btapir\.\b`)

	// tapirEndpointStartRe finds the start of an endpoint value chain. We accept
	// the bare `endpoint` builder as well as the input/output-only variants
	// (`infallibleEndpoint`). Group 0 is the anchor; the chain body is walked
	// forward from there with a balanced, statement-bounded scan.
	tapirEndpointStartRe = regexp.MustCompile(
		`\b(?:endpoint|infallibleEndpoint)\b`)

	// tapirMethodRe matches the method combinator `.get` / `.post` / … or the
	// explicit `.method(Method.GET)` form. Group 1 = combinator verb (lower),
	// group 2 = explicit Method.VERB (upper).
	tapirMethodRe = regexp.MustCompile(
		`\.(get|post|put|delete|patch|head|options)\b|\.method\s*\(\s*Method\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\)`)

	// tapirInRe matches each `.in( … )` input combinator argument blob.
	tapirInRe = regexp.MustCompile(`\.in\s*\(`)

	// tapirOutRe / tapirErrOutRe match `.out(` and `.errorOut(` combinators.
	tapirOutRe    = regexp.MustCompile(`\.out\s*\(`)
	tapirErrOutRe = regexp.MustCompile(`\.errorOut\s*\(`)

	// tapirJsonBodyRe captures jsonBody[T] / plainBody[T] / xmlBody[T] /
	// stringBody / byteArrayBody body-codec references. Group 1 = DTO type.
	tapirJsonBodyRe = regexp.MustCompile(
		`\b(?:jsonBody|plainBody|xmlBody|binaryBody|customCodecJsonBody)\s*\[\s*([A-Za-z_][\w.]*(?:\s*\[[^\]]*\])?)\s*\]`)

	// tapirPathSegLiteralRe captures a literal path segment string within an
	// `.in(...)` path expression: "users".
	tapirPathStringRe = regexp.MustCompile(`"([^"]*)"`)

	// tapirPathParamRe captures `path[T]("name")` typed path params. Group 1 =
	// the param name; when absent (anonymous path[Long]) we synthesise from type.
	tapirPathParamRe = regexp.MustCompile(
		`\bpath\s*\[\s*([A-Za-z_][\w.]*)\s*\]\s*(?:\(\s*"([^"]*)"\s*\))?`)

	// tapirQueryParamRe captures `query[T]("name")` query params.
	tapirQueryParamRe = regexp.MustCompile(
		`\bquery\s*\[\s*([A-Za-z_][\w.]*)\s*\]\s*\(\s*"([^"]*)"\s*\)`)

	// tapirServerLogicRe captures the handler reference bound to an endpoint.
	//   .serverLogic(handler) / .serverLogicSuccess(handler) /
	//   .serverLogicPure(handler)
	tapirServerLogicRe = regexp.MustCompile(
		`\.serverLogic(?:Success|Recover|Pure|Option|Error)?\s*\(\s*([A-Za-z_][\w.]*)`)
)

// isTapirSource reports whether the file uses tapir's endpoint DSL.
func isTapirSource(src string) bool {
	if !tapirSignalRe.MatchString(src) {
		return false
	}
	// Secondary gate: an actual `endpoint` builder must appear, so a file that
	// merely imports a tapir type alias is not claimed.
	return strings.Contains(src, "endpoint")
}

// tapirEndpointChain holds the parsed shape of one endpoint value.
type tapirEndpointChain struct {
	method       string   // upper HTTP verb, "" when none declared yet
	path         string   // canonical path template, e.g. /users/{id}
	queryParams  []string // query param names
	requestDTOs  []string // jsonBody[T] referenced under .in(...)
	responseDTOs []string // jsonBody[T] referenced under .out(...)
	errorDTOs    []string // jsonBody[T] referenced under .errorOut(...)
	handler      string   // .serverLogic target, "" when absent
	startLine    int
}

// tapirChainBody returns the chain text from the `endpoint` anchor up to the end
// of the enclosing statement. tapir chains are method-call chains; we stop at a
// blank line, a top-level `val`/`def` for the NEXT declaration, or after a bounded
// number of characters. This keeps two adjacent endpoint vals from bleeding into
// one another while tolerating the idiomatic multi-line `.in(...).out(...)` layout.
func tapirChainBody(src string, anchor int) string {
	const maxChain = 1200
	end := anchor
	limit := anchor + maxChain
	if limit > len(src) {
		limit = len(src)
	}
	// Walk forward; stop when we hit a new top-level val/def declaration that is
	// not part of this chain (a line whose first non-space token is val/def AND
	// the previous non-space char was not a '.'), or two consecutive newlines.
	consecutiveNL := 0
	for end < limit {
		c := src[end]
		if c == '\n' {
			consecutiveNL++
			// Lookahead: a new `val `/`def `/`object `/`class ` at the next
			// line's start terminates the chain unless the next line continues
			// the dot-chain (starts with `.`).
			j := end + 1
			for j < limit && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			rest := src[j:min(j+8, len(src))]
			if strings.HasPrefix(rest, "val ") || strings.HasPrefix(rest, "def ") ||
				strings.HasPrefix(rest, "object ") || strings.HasPrefix(rest, "class ") {
				break
			}
			if consecutiveNL >= 2 {
				break
			}
		} else if c != ' ' && c != '\t' && c != '\r' {
			consecutiveNL = 0
		}
		end++
	}
	return src[anchor:end]
}

// parseTapirEndpoint parses a single endpoint chain body into its shape.
func parseTapirEndpoint(chain string, startLine int) tapirEndpointChain {
	ep := tapirEndpointChain{startLine: startLine}

	// Method.
	if m := tapirMethodRe.FindStringSubmatch(chain); m != nil {
		switch {
		case m[1] != "":
			ep.method = strings.ToUpper(m[1])
		case m[2] != "":
			ep.method = strings.ToUpper(m[2])
		}
	}

	// Path + query: walk every `.in(...)` arg blob IN ORDER. Path segments
	// (string literals + path[T]) compose the route template positionally;
	// query[T] are recorded separately. jsonBody[T] inside an `.in(...)` is a
	// request DTO.
	var segs []string
	for _, loc := range tapirInRe.FindAllStringIndex(chain, -1) {
		blob := balancedParens(chain, loc[1])
		// Body DTO under .in → request DTO. A jsonBody/query input is never a
		// path expression, so handle and skip path parsing for it.
		if tapirJsonBodyRe.MatchString(blob) {
			for _, bm := range tapirJsonBodyRe.FindAllStringSubmatch(blob, -1) {
				ep.requestDTOs = appendUnique(ep.requestDTOs, normalizeScalaType(bm[1]))
			}
			continue
		}
		// Query params query[T]("name") — not part of the path template.
		if tapirQueryParamRe.MatchString(blob) {
			for _, qm := range tapirQueryParamRe.FindAllStringSubmatch(blob, -1) {
				ep.queryParams = appendUnique(ep.queryParams, qm[2])
			}
			continue
		}
		// Path expression: `"users" / path[Long]("id") / "posts"`. Split on the
		// top-level `/` separators and classify each token positionally so the
		// segment order is preserved.
		segs = append(segs, tapirPathSegments(blob)...)
	}
	ep.path = canonicalScalaSlashes("/" + strings.Join(segs, "/"))

	// Response DTOs under .out(...).
	for _, loc := range tapirOutRe.FindAllStringIndex(chain, -1) {
		blob := balancedParens(chain, loc[1])
		for _, bm := range tapirJsonBodyRe.FindAllStringSubmatch(blob, -1) {
			ep.responseDTOs = appendUnique(ep.responseDTOs, normalizeScalaType(bm[1]))
		}
	}
	// Error DTOs under .errorOut(...).
	for _, loc := range tapirErrOutRe.FindAllStringIndex(chain, -1) {
		blob := balancedParens(chain, loc[1])
		for _, bm := range tapirJsonBodyRe.FindAllStringSubmatch(blob, -1) {
			ep.errorDTOs = appendUnique(ep.errorDTOs, normalizeScalaType(bm[1]))
		}
	}

	// Handler attribution.
	if hm := tapirServerLogicRe.FindStringSubmatch(chain); hm != nil {
		ep.handler = hm[1]
	}

	return ep
}

// tapirPathSegments parses a tapir `.in(...)` path expression blob into ordered
// canonical segments. The blob looks like `"users" / path[Long]("id") / "posts"`.
// Literal quoted strings become literal segments (split on any embedded '/');
// `path[T]("name")` becomes `{name}` (synthesised from the type when anonymous).
func tapirPathSegments(blob string) []string {
	var segs []string
	for _, raw := range splitTopLevelSlashes(blob) {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		// path[T]("name") typed path param.
		if pm := tapirPathParamRe.FindStringSubmatch(tok); pm != nil {
			name := pm[2]
			if name == "" {
				name = tapirParamNameForType(pm[1])
			}
			segs = append(segs, "{"+name+"}")
			continue
		}
		// Literal quoted segment "users" (may itself contain '/').
		if sm := tapirPathStringRe.FindStringSubmatch(tok); sm != nil {
			for _, p := range strings.Split(sm[1], "/") {
				p = strings.TrimSpace(p)
				if p != "" {
					segs = append(segs, p)
				}
			}
		}
	}
	return segs
}

// splitTopLevelSlashes splits a tapir path expression on `/` separators that are
// not nested inside [] or () — so `path[Long]("id")` is one token, not split on
// brackets, and a `/` inside a quoted literal or type param does not break a seg.
func splitTopLevelSlashes(s string) []string {
	var parts []string
	depth := 0
	inStr := false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inStr = !inStr
		case inStr:
			// skip
		case c == '[' || c == '(':
			depth++
		case c == ']' || c == ')':
			if depth > 0 {
				depth--
			}
		case c == '/' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// tapirParamNameForType returns a synthetic param name for an anonymous path[T].
func tapirParamNameForType(typ string) string {
	switch normalizeScalaType(typ) {
	case "Long", "Int", "Short", "BigInt":
		return "id"
	case "UUID", "java.util.UUID":
		return "uuid"
	case "String":
		return "segment"
	}
	return "param"
}

// normalizeScalaType strips a leading package qualifier for readability while
// preserving any type-parameter suffix (List[User] stays List[User]).
func normalizeScalaType(t string) string {
	t = strings.TrimSpace(t)
	// Keep generic params intact; only de-qualify the head type name.
	head := t
	rest := ""
	if i := strings.IndexByte(t, '['); i >= 0 {
		head = t[:i]
		rest = t[i:]
	}
	if dot := strings.LastIndexByte(head, '.'); dot >= 0 {
		head = head[dot+1:]
	}
	return head + rest
}

// appendUnique appends v to s only when not already present.
func appendUnique(s []string, v string) []string {
	if v == "" {
		return s
	}
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// extractTapirEndpoints parses every endpoint chain in src into entities:
// one http_route per endpoint (method + path + DTO refs + handler) and one
// dto_ref entity per request/response/error DTO so consumers can navigate the
// wire shape independently of the case-class field model in validation.go.
func extractTapirEndpoints(src string, file fileMeta) []types.EntityRecord {
	var out []types.EntityRecord
	seen := map[string]bool{}

	for _, loc := range tapirEndpointStartRe.FindAllStringIndex(src, -1) {
		// Skip the `endpoint` token when it is part of a longer identifier such
		// as `endpoints` or `myEndpoint`: require a non-identifier (or BOF) just
		// before and a `.`/whitespace just after to be a builder start.
		startIdx := loc[0]
		if startIdx > 0 {
			p := src[startIdx-1]
			if p == '_' || p == '.' || (p >= 'a' && p <= 'z') || (p >= 'A' && p <= 'Z') || (p >= '0' && p <= '9') {
				continue
			}
		}
		chain := tapirChainBody(src, startIdx)
		// A bare `endpoint` with no method/in/out combinator is not a real route
		// site (e.g. a base endpoint alias with nothing chained on this line).
		if !strings.Contains(chain, ".in(") && !strings.Contains(chain, ".out(") &&
			!tapirMethodRe.MatchString(chain) {
			continue
		}
		ep := parseTapirEndpoint(chain, lineOf(src, startIdx))

		method := ep.method
		if method == "" {
			method = "ANY"
		}
		name := "tapir:" + method + ":" + ep.path
		key := "http_route:" + name
		if seen[key] {
			continue
		}
		seen[key] = true

		ent := makeEntity(name, "SCOPE.Operation", "http_route", file.Path, file.Language, ep.startLine)
		setProps(&ent, "framework", "tapir", "http_method", method, "http_path", ep.path,
			"provenance", "TAPIR_ENDPOINT_DSL")
		if len(ep.queryParams) > 0 {
			setProps(&ent, "query_params", strings.Join(ep.queryParams, ","))
		}
		if len(ep.requestDTOs) > 0 {
			setProps(&ent, "request_dto", strings.Join(ep.requestDTOs, ","))
		}
		if len(ep.responseDTOs) > 0 {
			setProps(&ent, "response_dto", strings.Join(ep.responseDTOs, ","))
		}
		if len(ep.errorDTOs) > 0 {
			setProps(&ent, "error_dto", strings.Join(ep.errorDTOs, ","))
		}
		if ep.handler != "" {
			setProps(&ent, "handler", ep.handler, "handler_attribution", "true")
		}
		out = append(out, ent)

		// Emit a dto_ref entity per request/response/error DTO so the wire shape
		// is navigable. These are SCOPE.Type/dto_ref (distinct from the
		// case-class SCOPE.Type/dto entities in validation.go) and carry the
		// role (request/response/error) + the owning route.
		emitDTORef := func(role string, dtos []string) {
			for _, dto := range dtos {
				rn := "tapir_dto:" + role + ":" + dto + ":" + name
				rk := "dto_ref:" + rn
				if seen[rk] {
					continue
				}
				seen[rk] = true
				de := makeEntity(rn, "SCOPE.Type", "dto_ref", file.Path, file.Language, ep.startLine)
				setProps(&de, "framework", "tapir", "provenance", "TAPIR_BODY_CODEC",
					"role", role, "dto", dto, "route", name, "http_method", method, "http_path", ep.path)
				out = append(out, de)
			}
		}
		emitDTORef("request", ep.requestDTOs)
		emitDTORef("response", ep.responseDTOs)
		emitDTORef("error", ep.errorDTOs)
	}

	return out
}
