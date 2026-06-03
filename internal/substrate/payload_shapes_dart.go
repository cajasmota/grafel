// Dart payload-shape sniffer (Phase 2A T3 — #4035).
//
// Supersedes the original `not_applicable` ruling in payload_shapes_t3.go
// (which assumed jsonDecode → Map<String,dynamic> is the only idiom).
// The json_serializable / freezed ecosystem makes Dart DTOs statically
// inspectable:
//
//	@JsonSerializable()
//	class User {
//	  final int id;
//	  final String name;
//	  User({required this.id, required this.name});
//	  factory User.fromJson(Map<String, dynamic> json) => _$UserFromJson(json);
//	}
//
// A `@JsonSerializable` (or freezed `@freezed`) class with a
// `factory X.fromJson` is a request/response DTO whose field names + types
// are the wire shape. We lift those classes, then attribute shapes:
//
//   - consumer request: a Dio/http call-site that constructs a body —
//     `Dio().post('/users', data: {'name': name, 'email': email})` or
//     `data: user.toJson()` (toJson of a known DTO) → request shape.
//   - consumer response: `User.fromJson(jsonDecode(resp.data))` → response
//     shape (the consumer destructures the response into a known DTO).
//
// Both sides are CONSUMER-side (Flutter/Dart is overwhelmingly the client
// in this graph; the producer side is the backend in another language).
// payload_drift.go then cross-references these against the backend
// producer shapes over the cross-repo HTTP links.
//
// Honest scope: type/cross-file resolution is heuristic. We only resolve a
// `fromJson(...)` / `toJson()` DTO when its class is declared in the SAME
// file. Inline `data: {...}` map literals are resolved directly. A plain
// class (no @JsonSerializable / @freezed annotation) yields no DTO — its
// fields are not guaranteed to be the wire shape.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("dart", sniffPayloadShapesDart) }

// dartJsonSerializableClassRe matches a class annotated for JSON
// serialization. Fires when an `@JsonSerializable` or `@freezed` (or
// `@JsonSerializable(...)`) annotation precedes a `class Name`.
// Capture group 1 = class name.
var dartJsonSerializableClassRe = regexp.MustCompile(
	`(?m)^\s*@(?:JsonSerializable|freezed|Freezed|JsonObject)\b[^\n]*\n(?:\s*@[^\n]*\n)*\s*(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`,
)

// dartFinalFieldRe matches an instance field declaration inside a class
// body:
//
//	final int id;
//	final String name;
//	final String? email;     (nullable → Optional)
//	int id;
//
// Capture groups: 1 = type (may end in `?`), 2 = field name. Anchored to
// require the `final`/type form and a trailing `;` so it does not match
// method bodies.
var dartFinalFieldRe = regexp.MustCompile(
	`(?m)^\s*(?:final\s+|late\s+final\s+|const\s+)?` +
		`([A-Za-z_$][\w$]*(?:<[^>;=]*>)?\??)\s+([A-Za-z_$][\w$]*)\s*(?:;|=)`,
)

// dartFromJsonRe matches a `X.fromJson(` call — consumer destructuring a
// response into a known DTO. Capture group 1 = DTO class name.
var dartFromJsonRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*)\s*\.\s*fromJson\s*\(`,
)

// dartHTTPBodyRe matches a Dio / http call-site that carries a request
// body. Recognises both:
//
//	dio.post('/users', data: {'name': name})
//	Dio().post('/users', data: user.toJson())
//	http.post(url, body: jsonEncode({'name': name}))
//
// Capture group 1 = the verb, group 2 = the `data:`/`body:` argument tail
// up to the end of the call (best-effort, single-line). The body content
// is parsed separately by dartMapKeyRe / dartToJsonRe.
var dartHTTPBodyRe = regexp.MustCompile(
	`\b(?:dio|_dio|Dio\(\)|client|_client|http)\s*\.\s*(post|put|patch)\s*\(([^\n]*)`,
)

// dartMapKeyRe matches a `'key':` or `"key":` entry in a Dart map literal.
// Capture group 1/2 = the key.
var dartMapKeyRe = regexp.MustCompile(
	`'([A-Za-z_$][\w$]*)'\s*:` + `|"([A-Za-z_$][\w$]*)"\s*:`,
)

// dartToJsonRe matches `<ident>.toJson()` — the body is a known DTO.
// Capture group 1 = the variable/expression head (used only to detect the
// pattern; the DTO type is resolved from the surrounding declaration when
// available, else the shape is skipped).
var dartToJsonCallRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*)\s*\.\s*toJson\s*\(\s*\)`,
)

func sniffPayloadShapesDart(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanDartFuncHeaders(content)

	// 1. Lift @JsonSerializable / @freezed DTO classes → field maps.
	dtoFields := map[string][]PayloadField{}
	for _, m := range dartJsonSerializableClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		className := content[m[2]:m[3]]
		body := extractBraceBody(content, m[2])
		var fields []PayloadField
		for _, fm := range dartFinalFieldRe.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 6 {
				continue
			}
			typ := strings.TrimSpace(body[fm[2]:fm[3]])
			name := body[fm[4]:fm[5]]
			// Skip obvious non-field forms (static const, get/set keywords).
			if isDartFieldNoise(typ, name) {
				continue
			}
			optional := strings.HasSuffix(typ, "?")
			fields = append(fields, PayloadField{
				Name:     name,
				Type:     strings.TrimSuffix(typ, "?"),
				Optional: optional,
			})
		}
		fields = DedupFields(fields)
		if len(fields) > 0 {
			dtoFields[className] = fields
		}
	}

	var out []PayloadShape

	// 2. Consumer response shapes: `X.fromJson(...)` where X is a known DTO.
	for _, m := range dartFromJsonRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		dto := content[m[2]:m[3]]
		fields, ok := dtoFields[dto]
		if !ok || len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideConsumer,
			Fields:     fields,
			Confidence: 0.9,
		})
	}

	// 3. Consumer request shapes: Dio/http post/put/patch call bodies.
	for _, m := range dartHTTPBodyRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		tail := content[m[4]:m[5]]
		fields := dartRequestBodyFields(tail, dtoFields)
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideConsumer,
			Fields:     fields,
			Confidence: 0.85,
			VerbHint:   verb,
		})
	}

	return out
}

// dartRequestBodyFields extracts request-body fields from a Dio/http call
// tail. Two idioms:
//
//	data: {'name': name, 'email': email}   → inline map keys
//	data: user.toJson()                    → DTO fields (resolved by the
//	                                         variable's declared type when
//	                                         that type is a known DTO)
//
// Inline map literals are high-signal and resolved directly. A
// `<var>.toJson()` body is resolved only when the variable name itself
// matches a known DTO class name case-insensitively (e.g. `user.toJson()`
// with a `class User`) — a deliberately conservative heuristic.
func dartRequestBodyFields(tail string, dtoFields map[string][]PayloadField) []PayloadField {
	// Prefer inline map keys when present.
	var fields []PayloadField
	for _, m := range dartMapKeyRe.FindAllStringSubmatchIndex(tail, -1) {
		if len(m) < 6 {
			continue
		}
		var key string
		if m[2] >= 0 {
			key = tail[m[2]:m[3]]
		} else if m[4] >= 0 {
			key = tail[m[4]:m[5]]
		}
		if key != "" {
			fields = append(fields, PayloadField{Name: key})
		}
	}
	if len(fields) > 0 {
		return DedupFields(fields)
	}

	// Fall back to <var>.toJson() with a name→DTO match.
	if cm := dartToJsonCallRe.FindStringSubmatch(tail); cm != nil {
		varName := cm[1]
		for dto, f := range dtoFields {
			if strings.EqualFold(dto, varName) {
				return f
			}
		}
	}
	return nil
}

// isDartFieldNoise rejects type/name pairs that are not instance payload
// fields: control keywords leaking through, and `get`/`set`/`return`
// fragments. Conservative — only filters clear non-fields.
func isDartFieldNoise(typ, name string) bool {
	switch typ {
	case "return", "get", "set", "if", "for", "while", "switch", "factory",
		"final", "const", "static", "var", "class", "void", "assert":
		return true
	}
	switch name {
	case "fromJson", "toJson":
		return true
	}
	return false
}
