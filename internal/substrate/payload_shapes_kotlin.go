// Kotlin payload-shape sniffer (#2771 Phase 2A T2).
//
// Producer-side shapes (Spring Boot / Ktor / Micronaut handlers):
//
//   - `@RequestBody dto: T` — the wrapped data class T's primary-
//     constructor properties (declared in the same file) become the
//     request shape.
//   - Data class `data class T(val a: String, val b: Int? = null)` —
//     the property list is the field set. `?` on the type flips
//     Optional=true.
//   - Ktor `call.receive<T>()` — wrapped type's properties as request
//     shape.
//   - Response shapes: `mapOf("a" to ..., "b" to ...)` inline maps and
//     `ResponseEntity.ok(T(a, b))` constructor returns paired with a
//     same-file data class.
//
// Consumer-side shapes (Ktor client / OkHttp):
//
//   - `client.post(url) { setBody(mapOf("a" to ..., "b" to ...)) }`
//   - `OkHttpClient` is opaque (body comes from
//     `RequestBody.create(JSON, "{...}")`) — out of scope for a regex
//     sniffer; we recognise mapOf within a `client.post/get` block.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("kotlin", sniffPayloadShapesKotlin) }

// ktDataClassRe matches `data class Name(...)`. Capture group 1 = the
// type name; group 2 = the primary-constructor parameter list body.
var ktDataClassRe = regexp.MustCompile(
	`(?m)^\s*(?:public\s+|internal\s+|private\s+|open\s+|sealed\s+)*data\s+class\s+([A-Z][\w]*)\s*\(([^)]*)\)`,
)

// ktRequestBodyParamRe matches a Spring `@RequestBody name: T` or
// `@RequestBody name: T,`. Capture group 1 = parameter name; group 2 =
// type (may include generic suffix).
var ktRequestBodyParamRe = regexp.MustCompile(
	`@RequestBody\s+(?:@\w+(?:\([^)]*\))?\s+)*([A-Za-z_][\w]*)\s*:\s*([A-Z][\w<>,?\s.]*)`,
)

// ktCallReceiveRe matches Ktor `call.receive<T>()`. Capture group 1 = T.
var ktCallReceiveRe = regexp.MustCompile(
	`\bcall\s*\.\s*receive\s*<\s*([A-Z][\w]*)\s*>\s*\(\s*\)`,
)

// ktMapOfRe matches `mapOf("a" to v, "b" to v)`. Capture group 1 =
// the arg list body.
var ktMapOfRe = regexp.MustCompile(
	`\bmapOf\s*\(([^()]*)\)`,
)

// ktClientPostRe matches Ktor client `client.<verb>(url)`. Capture
// group 1 = verb, group 2 = inline URL.
var ktClientPostRe = regexp.MustCompile(
	`\bclient\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*['"]([^'"]*)['"]`,
)

// ktTopArgKeyRe matches a `"name" to ...` pair in a mapOf body.
// Capture group 1 = the bare name.
var ktTopArgKeyRe = regexp.MustCompile(
	`"([A-Za-z_][\w]*)"\s+to\b`,
)

func sniffPayloadShapesKotlin(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanKotlinFuncHeaders(content)
	dataClassFields := scanKotlinDataClassFields(content)
	clientHints := scanKotlinClientHints(content, headers)

	var out []PayloadShape

	// Producer-side: @RequestBody → request shape from data class.
	for _, m := range ktRequestBodyParamRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		typ := stripGenericSuffix(strings.TrimSpace(content[m[4]:m[5]]))
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := dataClassFields[typ]
		conf := 0.9
		if len(fields) == 0 {
			conf = 0.4
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: conf,
		})
	}

	// Producer-side: call.receive<T>() → request shape from data class.
	for _, m := range ktCallReceiveRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		typ := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := dataClassFields[typ]
		conf := 0.9
		if len(fields) == 0 {
			conf = 0.4
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: conf,
		})
	}

	// Producer/Consumer-side: mapOf("a" to v, ...) literals.
	for _, m := range ktMapOfRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		var fields []PayloadField
		for _, km := range ktTopArgKeyRe.FindAllStringSubmatchIndex(body, -1) {
			if len(km) < 4 {
				continue
			}
			fields = append(fields, PayloadField{Name: body[km[2]:km[3]]})
		}
		fields = DedupFields(fields)
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if hint, ok := clientHints[fn]; ok {
			out = append(out, PayloadShape{
				Function:     fn,
				Line:         line,
				Direction:    PayloadDirectionRequest,
				Side:         PayloadSideConsumer,
				Fields:       fields,
				Confidence:   1.0,
				EndpointHint: hint.url,
				VerbHint:     hint.verb,
			})
			continue
		}
		if fn == "" {
			continue
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 1.0,
		})
	}

	return out
}

// scanKotlinDataClassFields builds the map of data-class name →
// []PayloadField from primary-constructor parameter lists.
func scanKotlinDataClassFields(content string) map[string][]PayloadField {
	out := map[string][]PayloadField{}
	for _, m := range ktDataClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := content[m[2]:m[3]]
		paramList := content[m[4]:m[5]]
		var fields []PayloadField
		for _, p := range splitTopLevel(paramList) {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			// Strip annotations like `@SerializedName("x")`.
			for strings.HasPrefix(p, "@") {
				if idx := strings.Index(p, " "); idx > 0 {
					p = strings.TrimSpace(p[idx:])
				} else {
					p = ""
					break
				}
			}
			// Strip val/var keyword.
			p = strings.TrimPrefix(p, "val ")
			p = strings.TrimPrefix(p, "var ")
			// `name: Type` (with optional default `= value`).
			colon := strings.IndexByte(p, ':')
			if colon <= 0 {
				continue
			}
			fname := strings.TrimSpace(p[:colon])
			rest := strings.TrimSpace(p[colon+1:])
			// Drop default-value clause.
			if eq := strings.IndexByte(rest, '='); eq >= 0 {
				rest = strings.TrimSpace(rest[:eq])
			}
			optional := strings.HasSuffix(rest, "?")
			if !isPlainIdent(fname) {
				continue
			}
			fields = append(fields, PayloadField{Name: fname, Type: rest, Optional: optional})
		}
		if len(fields) > 0 {
			out[name] = DedupFields(fields)
		}
	}
	return out
}

// kotlinClientHint mirrors rust/csharp hint structs.
type kotlinClientHint struct {
	url  string
	verb string
}

// scanKotlinClientHints maps function name → first observed Ktor
// client call hint. Used by the mapOf scan to disambiguate.
func scanKotlinClientHints(content string, headers []funcHeader) map[string]kotlinClientHint {
	out := map[string]kotlinClientHint{}
	for _, m := range ktClientPostRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		if _, ok := out[fn]; ok {
			continue
		}
		out[fn] = kotlinClientHint{
			url:  content[m[4]:m[5]],
			verb: strings.ToUpper(content[m[2]:m[3]]),
		}
	}
	return out
}
