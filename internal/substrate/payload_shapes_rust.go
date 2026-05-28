// Rust payload-shape sniffer (#2771 Phase 2A T2).
//
// Producer-side shapes (Axum / Actix / Rocket / Warp handlers):
//
//   - `Json<T>` / `web::Json<T>` extractor in a handler signature —
//     the wrapped struct T's fields (when declared in the same file)
//     become the request shape.
//   - `#[derive(Deserialize)] struct T { a: String, b: Option<i32> }`
//     declarations contribute their fields to the same-file struct map.
//     `Option<U>` flips the Optional flag on the field.
//   - Response shapes: `Json(T { a, b })` literal constructors and
//     return types like `-> Json<UserResponse>` paired with a same-file
//     struct definition.
//
// Consumer-side shapes (reqwest):
//
//   - `client.post(url).json(&body)` where `body` is an inline literal
//     `serde_json::json!({"a": ..., "b": ...})` — the json! body is
//     captured as the request shape.
//   - `.body(format!(r#"{{"a": ...}}"#))` style raw-string bodies are
//     out of scope (no static structure).
//
// Optional/required: Rust's `Option<T>` is observable and flips the
// Optional flag; everything else stays default-false.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("rust", sniffPayloadShapesRust) }

// rustStructHeaderRe matches `pub struct Name {` / `struct Name {`.
// Capture group 1 = the type name.
var rustStructHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:pub\s+(?:\(crate\)\s+)?)?struct\s+([A-Z][\w]*)\s*\{`,
)

// rustStructFieldRe matches `pub? name: Type,` inside a struct body.
// Capture group 1 = field name; group 2 = type (may be `Option<X>`).
// Bounded by comma or end-of-line; the body iteration trims commas.
var rustStructFieldRe = regexp.MustCompile(
	`(?m)^\s*(?:pub\s+(?:\([^)]*\)\s+)?)?([a-z_][\w]*)\s*:\s*([^,\n]+?)\s*[,\n]`,
)

// rustJsonExtractorRe matches `Json<T>` / `web::Json<T>` extractor
// parameters in a handler signature. Capture group 1 = the type name.
var rustJsonExtractorRe = regexp.MustCompile(
	`(?:web\s*::\s*)?Json\s*<\s*([A-Z][\w]*)\s*>`,
)

// rustJsonMacroRe matches `serde_json::json!({...})` and bare `json!({...})`.
// Capture group 1 = the inline object body between `{` and `}`.
var rustJsonMacroRe = regexp.MustCompile(
	`\b(?:serde_json\s*::\s*)?json!\s*\(\s*\{([^{}]*)\}`,
)

// rustClientPostRe matches `client.<verb>(url)` reqwest builder calls.
// Capture group 1 = verb, group 2 = inline URL when present. Used to
// tag the consumer shape with a hint.
var rustClientPostRe = regexp.MustCompile(
	`\bclient\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*['"]([^'"]*)['"]`,
)

// rustQuotedKeyRe matches a JSON-style key in a json!({...}) body.
var rustQuotedKeyRe = regexp.MustCompile(`"([A-Za-z_][\w]*)"\s*:`)

func sniffPayloadShapesRust(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanRustFuncHeaders(content)
	structFields := scanRustStructFields(content)
	clientHints := scanRustClientHints(content, headers)

	var out []PayloadShape

	// Producer-side: Json<T> extractor → request shape from struct T.
	for _, m := range rustJsonExtractorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		typ := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := structFields[typ]
		conf := 0.9
		if len(fields) == 0 {
			conf = 0.4 // cross-file unresolved
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

	// Producer-side & Consumer-side: json!({...}) literals. We bind
	// each literal to its enclosing function; if that function is a
	// known reqwest client caller (it has a `client.post(...)` shape
	// in the same body), we tag it as consumer. Otherwise it's a
	// producer response shape.
	for _, m := range rustJsonMacroRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractRustJSONKeys(body)
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

// scanRustStructFields walks the file once and returns a map of struct
// name → []PayloadField. Body is delimited by a brace counter from the
// opening `{` of the struct header to the matching `}`.
func scanRustStructFields(content string) map[string][]PayloadField {
	out := map[string][]PayloadField{}
	for _, m := range rustStructHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		body, ok := goBalancedBlock(content, m[1]-1)
		if !ok {
			continue
		}
		var fields []PayloadField
		for _, fm := range rustStructFieldRe.FindAllStringSubmatchIndex(body+"\n", -1) {
			if len(fm) < 6 {
				continue
			}
			fname := body[fm[2]:fm[3]]
			ftype := strings.TrimSpace(body[fm[4]:fm[5]])
			optional := strings.HasPrefix(ftype, "Option<")
			fields = append(fields, PayloadField{
				Name:     fname,
				Type:     ftype,
				Optional: optional,
			})
		}
		if len(fields) > 0 {
			out[name] = DedupFields(fields)
		}
	}
	return out
}

// rustClientCallHint captures the URL/verb tagged onto a reqwest call.
type rustClientCallHint struct {
	url  string
	verb string
}

// scanRustClientHints returns the set of functions that contain a
// `client.<verb>(url)` reqwest builder. The consumer-side recognition
// uses this to disambiguate json! literals that are HTTP bodies vs
// response constructors.
func scanRustClientHints(content string, headers []funcHeader) map[string]rustClientCallHint {
	out := map[string]rustClientCallHint{}
	for _, m := range rustClientPostRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		out[fn] = rustClientCallHint{
			url:  content[m[4]:m[5]],
			verb: strings.ToUpper(content[m[2]:m[3]]),
		}
	}
	return out
}

// extractRustJSONKeys lifts JSON-style keys out of a `json!({...})`
// body. Deduped, source order preserved.
func extractRustJSONKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range rustQuotedKeyRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 4 {
			continue
		}
		fields = append(fields, PayloadField{Name: body[m[2]:m[3]]})
	}
	return DedupFields(fields)
}
