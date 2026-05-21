// Response-shape extraction for synthetic http_endpoint entities (#722).
//
// For every http_endpoint that the synthesis pass emits, this module walks
// the same file looking for the named handler function body, parses the
// return statements, and records:
//
//   - response_keys      top-level keys of literal-object responses
//   - response_schema    {key: type} JSON map when type info is statically
//     recoverable (Spring DTO, JAX-RS @Schema, Pydantic
//     BaseModel, Go struct, NestJS DTO class)
//   - error_keys         keys observed in 4xx/5xx returns
//   - status_codes       sorted comma-separated list of emitted HTTP codes
//   - request_keys       request-body shape (FastAPI Pydantic param,
//     NestJS @Body() Dto, Spring @RequestBody Dto)
//   - request_schema     same shape as response_schema for the request body
//   - response_keys_known=false when the return value is a free variable
//     that we cannot statically resolve to a literal/typed shape.
//
// All properties are stored on the http_endpoint entity (Properties is
// map[string]string) — list values are joined with `,` for grep-friendly
// queries, schema maps are stored as a stable JSON-serialised string
// sorted by key for deterministic output.
//
// The pass is deliberately additive: when extraction fails it sets
// response_keys_known=false and leaves the rest unset. It never removes
// an existing property.
package engine

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// shape collects everything we learn about one endpoint's request/response
// shape from scanning a single handler. Empty fields are not written to
// the entity properties.
type shape struct {
	responseKeys       []string
	responseSchema     map[string]string
	errorKeys          []string
	statusCodes        []int
	requestKeys        []string
	requestSchema      map[string]string
	knownResponse      bool   // true when at least one return was statically parsed
	dynamicResponse    bool   // true when we saw a non-literal return value
	responseKeysSource string // "drf_serializer" or "java_dto" when applicable
}

// applyResponseShapes scans the freshly-emitted http_endpoint entities and
// attaches response/request shape properties extracted from the same file.
// It is safe to call with content==nil (no-op) and with frameworks that
// have no response-shape support (the per-framework dispatch falls through).
//
// The `entities` slice is the post-synthesis list returned by
// applyHTTPEndpointSynthesis. We mutate Properties in place.
func applyResponseShapes(lang string, content []byte, entities []types.EntityRecord) {
	if len(content) == 0 || len(entities) == 0 {
		return
	}
	src := string(content)
	for i := range entities {
		e := &entities[i]
		// #1217: apply response shape extraction to all three http endpoint kinds.
		if e.Kind != httpEndpointKind && e.Kind != httpEndpointDefinitionKind && e.Kind != httpEndpointCallKind {
			continue
		}
		if e.Properties == nil {
			continue
		}
		framework := e.Properties["framework"]
		handler := e.Properties["source_handler"]
		// source_handler is stored as "<Kind>:<Name>" (see emit closure
		// in http_endpoint_synthesis.go). Strip both common prefixes so
		// per-framework body scanners can use the bare handler name.
		if idx := strings.Index(handler, ":"); idx >= 0 {
			handler = handler[idx+1:]
		}
		var sh shape
		switch framework {
		case "django", "flask", "fastapi":
			// Always try the FastAPI walks (response_model decorator,
			// Pydantic request-body annotation) regardless of which
			// Python synth fired — the framework label is set by
			// whichever regex matches first, but the extraction logic
			// is additive and FastAPI projects often share decorator
			// shapes with Flask blueprints (#722).
			sh = extractPythonShape(src, handler, "fastapi")
		case "express":
			sh = extractJSShape(src, handler, "express")
		case "nestjs":
			sh = extractJSShape(src, handler, "nestjs")
		case "spring_mvc":
			sh = extractJavaShape(src, handler, "spring_mvc")
		case "jaxrs":
			sh = extractJavaShape(src, handler, "jaxrs")
		case "gin", "echo", "chi":
			sh = extractGoShape(src, handler, framework)
		default:
			continue
		}
		if !sh.knownResponse && !sh.dynamicResponse {
			continue
		}
		writeShapeProps(e.Properties, sh)
	}
}

// writeShapeProps serialises a shape struct onto an entity's Properties
// map. Lists go in as comma-joined values; schemas go in as a stable JSON
// string sorted by key for deterministic output.
func writeShapeProps(props map[string]string, sh shape) {
	if props == nil {
		return
	}
	if sh.dynamicResponse && !sh.knownResponse {
		props["response_keys_known"] = "false"
		return
	}
	if sh.knownResponse {
		props["response_keys_known"] = "true"
	}
	if len(sh.responseKeys) > 0 {
		props["response_keys"] = joinUniqueSorted(sh.responseKeys)
	}
	if len(sh.errorKeys) > 0 {
		props["error_keys"] = joinUniqueSorted(sh.errorKeys)
	}
	if len(sh.statusCodes) > 0 {
		props["status_codes"] = joinIntsSorted(sh.statusCodes)
	}
	if len(sh.requestKeys) > 0 {
		props["request_keys"] = joinUniqueSorted(sh.requestKeys)
	}
	if len(sh.responseSchema) > 0 {
		if s := marshalSchema(sh.responseSchema); s != "" {
			props["response_schema"] = s
		}
	}
	if len(sh.requestSchema) > 0 {
		if s := marshalSchema(sh.requestSchema); s != "" {
			props["request_schema"] = s
		}
	}
	if sh.responseKeysSource != "" {
		props["response_keys_source"] = sh.responseKeysSource
	}
}

func joinUniqueSorted(in []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func joinIntsSorted(in []int) string {
	seen := map[int]bool{}
	out := make([]int, 0, len(in))
	for _, n := range in {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	parts := make([]string, len(out))
	for i, n := range out {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func marshalSchema(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	// json.Marshal of map[string]string emits keys in alphabetical
	// order, so the resulting string is deterministic.
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Common helpers
// ---------------------------------------------------------------------------

// dictKeyRe matches `"key":` / `'key':` style key declarations.
var dictKeyRe = regexp.MustCompile(`["']([A-Za-z_][\w-]*)["']\s*:`)

// bareKeyRe matches JS-shorthand `key:` declarations (no quotes).
var bareKeyRe = regexp.MustCompile(`(?:^|[{,]\s*)([A-Za-z_]\w*)\s*:`)

// extractDictKeys returns the top-level keys of a Python/JS dict/object
// literal expressed as a string. Only literal `"key":` / `'key':` or
// bare-word `key:` pairs are extracted; nested objects are skipped via
// brace-depth tracking. Returns nil when the body is not a literal object
// (e.g. a variable name).
func extractDictKeys(body string) []string {
	body = strings.TrimSpace(body)
	if !(strings.HasPrefix(body, "{") && strings.HasSuffix(body, "}")) {
		return nil
	}
	inner := body[1 : len(body)-1]
	// Walk top-level only — track brace/paren/bracket depth so we don't
	// pick up keys nested inside arrays or sub-objects.
	var topLevel strings.Builder
	depth := 0
	inStr := byte(0)
	esc := false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if esc {
			esc = false
			topLevel.WriteByte(c)
			continue
		}
		if inStr != 0 {
			if c == '\\' {
				esc = true
			} else if c == inStr {
				inStr = 0
			}
			topLevel.WriteByte(c)
			continue
		}
		switch c {
		case '"', '\'', '`':
			inStr = c
			topLevel.WriteByte(c)
		case '{', '(', '[':
			depth++
			topLevel.WriteByte(c)
		case '}', ')', ']':
			depth--
			topLevel.WriteByte(c)
		default:
			if depth == 0 {
				topLevel.WriteByte(c)
			} else {
				topLevel.WriteByte(' ') // preserve offsets
			}
		}
	}
	flat := topLevel.String()
	var keys []string
	for _, m := range dictKeyRe.FindAllStringSubmatch(flat, -1) {
		keys = append(keys, m[1])
	}
	if len(keys) == 0 {
		// Trim leading whitespace so the first bare key (whose
		// preceding context after outer-brace stripping is " name:"
		// rather than "{ name:") still matches bareKeyRe's
		// `(?:^|[{,]\s*)` anchor. Without this trim,
		// `{ users: [], page: 1 }` produced only ["page"] because
		// " users" is preceded by a space, not by `{`, `,`, or string
		// start. Caught while fixing #753.
		flatTrim := strings.TrimLeft(flat, " \t\r\n")
		for _, m := range bareKeyRe.FindAllStringSubmatch(flatTrim, -1) {
			keys = append(keys, m[1])
		}
	}
	return keys
}

// findMatchingBracket returns the index of the closing `}`/`)`/`]` that
// matches the opening bracket at position `start` (which MUST point at
// one of `{([`). Returns -1 on overrun. Tracks string literals (single,
// double, backtick) so braces inside strings are ignored.
func findMatchingBracket(s string, start int) int {
	if start >= len(s) {
		return -1
	}
	open := s[start]
	var closeCh byte
	switch open {
	case '{':
		closeCh = '}'
	case '(':
		closeCh = ')'
	case '[':
		closeCh = ']'
	default:
		return -1
	}
	depth := 0
	inStr := byte(0)
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
			continue
		}
		if inStr != 0 {
			if c == '\\' {
				esc = true
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inStr = c
		case open:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractArgList returns the comma-separated top-level arguments of a
// function call starting at `(` at index `start`. Useful for parsing
// `Response({...}, status=400)` and `c.JSON(http.StatusOK, gin.H{...})`.
func extractArgList(s string, start int) []string {
	if start >= len(s) || s[start] != '(' {
		return nil
	}
	end := findMatchingBracket(s, start)
	if end < 0 {
		return nil
	}
	inner := s[start+1 : end]
	var args []string
	depth := 0
	inStr := byte(0)
	esc := false
	last := 0
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if esc {
			esc = false
			continue
		}
		if inStr != 0 {
			if c == '\\' {
				esc = true
			} else if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inStr = c
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[last:i]))
				last = i + 1
			}
		}
	}
	if last < len(inner) {
		args = append(args, strings.TrimSpace(inner[last:]))
	}
	return args
}
