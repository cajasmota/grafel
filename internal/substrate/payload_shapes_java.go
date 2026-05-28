// Java payload-shape sniffer (#2770 Phase 2A T1).
//
// Producer-side shapes:
//
//   - Spring `@RequestBody <DTO> name` — the DTO's class members
//     (declared in the same file) become the request shape. When the
//     DTO is in a different file, only the parameter type name is
//     recorded; cross-file DTO resolution is Phase 4.
//   - Inline `Map<String, Object>` field reads inside a handler:
//     `body.get("X")`, `body.get("Y")`. These bind to the enclosing
//     handler's request shape.
//   - Response shapes: `ResponseEntity.ok(new Foo(...))` where Foo's
//     class is in the same file → fields are the public members of
//     Foo. `Map.of("X", v, "Y", v)` literal returns are also picked
//     up as inline shapes.
//
// Consumer-side shapes:
//
//   - `restTemplate.postForObject(url, request, ...)` where `request`
//     is a `Map.of("X", v, "Y", v)` literal — the literal contributes
//     the field set.
//   - `webClient.post().uri(url).bodyValue(Map.of("X", v, "Y", v))` —
//     same recognition.
//
// Optional/required: `@NotNull` / `Optional<T>` are observable in
// Java; the sniffer flips Optional=true for `Optional<T>` member
// declarations and leaves @NotNull as the default (required).
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("java", sniffPayloadShapesJava) }

// javaRequestBodyParamRe matches a `@RequestBody [Type] name` parameter
// in a method signature. Capture group 1 = DTO type name; group 2 =
// parameter name.
var javaRequestBodyParamRe = regexp.MustCompile(
	`@RequestBody\s+(?:@\w+(?:\([^)]*\))?\s+)*([A-Z][\w$<>,\s.]*)\s+([A-Za-z_$][\w$]*)`,
)

// javaDtoFieldRe matches a DTO class member declaration. Capture group
// 1 = type (may be `Optional<X>`); group 2 = field name.
// Recognises common access modifiers and final.
var javaDtoFieldRe = regexp.MustCompile(
	`(?m)^\s*(?:private|public|protected)\s+(?:final\s+)?` +
		`([A-Za-z_$][\w$<>,?\s.]*)\s+([A-Za-z_$][\w$]*)\s*(?:=|;)`,
)

// javaClassHeaderRe matches `class <Name>` so the sniffer can pair a
// DTO type back to its declaring class block (single-file resolution).
// Capture group 1 = class name.
var javaClassHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+|static\s+)*class\s+([A-Z][\w$]*)\b`,
)

// javaResponseMapOfRe matches `Map.of("X", v, "Y", v)` and
// `ImmutableMap.of(...)` inline maps. Capture group 1 = the arg body
// between parens.
var javaResponseMapOfRe = regexp.MustCompile(
	`\b(?:Map|ImmutableMap)\s*\.\s*of\s*\(([^()]*)\)`,
)

// javaBodyGetRe matches `body.get("X")` style request reads. Capture
// group 1 = field name.
var javaBodyGetRe = regexp.MustCompile(
	`\b(?:body|payload|request|req)\s*\.\s*get\s*\(\s*"([A-Za-z_$][\w$]*)"\s*\)`,
)

// javaResponseEntityRe matches `ResponseEntity.ok(new X(...))` so we
// can pick up X as the response DTO type. Capture group 1 = type.
var javaResponseEntityRe = regexp.MustCompile(
	`ResponseEntity\s*\.\s*(?:ok|status\([^)]*\)\.body)\s*\(\s*new\s+([A-Z][\w$]*)\s*\(`,
)

// javaConsumerBodyValueRe matches `.bodyValue(Map.of(...))` —
// captures the inner Map.of arg body.
var javaConsumerBodyValueRe = regexp.MustCompile(
	`\.\s*bodyValue\s*\(\s*(?:Map|ImmutableMap)\s*\.\s*of\s*\(([^()]*)\)`,
)

// javaQuotedKeyRe matches a quoted-string key argument used inside
// `Map.of(...)` literals (alternating key/value args). Capture group 1
// is the key name.
var javaQuotedKeyRe = regexp.MustCompile(`"([A-Za-z_$][\w$]*)"\s*,`)

func sniffPayloadShapesJava(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanJavaFuncHeaders(content)
	classDTOFields := scanJavaClassDTOFields(content)

	var out []PayloadShape

	// Producer-side: @RequestBody parameters → handler request shape.
	for _, m := range javaRequestBodyParamRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		dtoType := strings.TrimSpace(content[m[2]:m[3]])
		dtoType = stripGenericSuffix(dtoType)
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := classDTOFields[dtoType]
		if len(fields) == 0 {
			// Unresolved DTO (cross-file). Emit a low-confidence empty
			// shape so the drift detector knows the handler reads a
			// typed body but can't enumerate fields; downstream tools
			// can filter on Confidence < 0.5 to suppress.
			out = append(out, PayloadShape{
				Function:   fn,
				Line:       line,
				Direction:  PayloadDirectionRequest,
				Side:       PayloadSideProducer,
				Fields:     nil,
				Confidence: 0.4,
			})
			continue
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 0.9,
		})
	}

	// Producer-side: inline body.get("X") reads as a fallback when the
	// handler uses Map<String, Object> instead of a DTO.
	bodyGetFields := map[string][]PayloadField{}
	bodyGetFirstLine := map[string]int{}
	for _, m := range javaBodyGetRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		bodyGetFields[fn] = append(bodyGetFields[fn], PayloadField{Name: name})
		if _, ok := bodyGetFirstLine[fn]; !ok {
			bodyGetFirstLine[fn] = line
		}
	}
	for fn, fields := range bodyGetFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       bodyGetFirstLine[fn],
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.7,
		})
	}

	// Producer-side: ResponseEntity.ok(new X(...)) → response shape
	// from X's class members (single-file).
	for _, m := range javaResponseEntityRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		dtoType := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := classDTOFields[dtoType]
		if len(fields) == 0 {
			continue
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 0.9,
		})
	}

	// Producer-side: Map.of("X", v, "Y", v) literal returns.
	for _, m := range javaResponseMapOfRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]] + "," // trailing comma so the key regex picks the last one too
		fields := extractJavaMapOfKeys(body)
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
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 1.0,
		})
	}

	// Consumer-side: webClient.bodyValue(Map.of(...)) inline body.
	for _, m := range javaConsumerBodyValueRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]] + ","
		fields := extractJavaMapOfKeys(body)
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideConsumer,
			Fields:     fields,
			Confidence: 1.0,
		})
	}

	return out
}

// scanJavaClassDTOFields walks the file once and returns a map of
// className → []PayloadField. Each class block is delimited by the
// next class header or EOF; fields are recognised by javaDtoFieldRe
// inside the block.
//
// This is intentionally single-file: cross-file DTO resolution is a
// Phase 4 candidate per the issue spec.
func scanJavaClassDTOFields(content string) map[string][]PayloadField {
	type classBlock struct {
		name  string
		start int
		end   int
	}
	var classes []classBlock
	matches := javaClassHeaderRe.FindAllStringSubmatchIndex(content, -1)
	for i, m := range matches {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		start := m[1]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		classes = append(classes, classBlock{name: name, start: start, end: end})
	}
	out := map[string][]PayloadField{}
	for _, cb := range classes {
		body := content[cb.start:cb.end]
		var fields []PayloadField
		for _, m := range javaDtoFieldRe.FindAllStringSubmatchIndex(body, -1) {
			if len(m) < 6 {
				continue
			}
			typ := strings.TrimSpace(body[m[2]:m[3]])
			name := body[m[4]:m[5]]
			optional := strings.HasPrefix(typ, "Optional<") || strings.HasSuffix(typ, "Optional")
			fields = append(fields, PayloadField{
				Name:     name,
				Type:     typ,
				Optional: optional,
			})
		}
		if len(fields) > 0 {
			out[cb.name] = DedupFields(fields)
		}
	}
	return out
}

// extractJavaMapOfKeys picks the quoted-string keys out of the
// comma-separated arg list of a `Map.of(...)` literal. The body is
// expected to have a trailing comma added by the caller so the last
// key matches.
func extractJavaMapOfKeys(body string) []PayloadField {
	var fields []PayloadField
	// Map.of takes alternating (key, value) pairs. We pick every
	// quoted token that appears in an EVEN argument position (0, 2, ...).
	// Heuristic: count commas. A simple alternation works here because
	// most Map.of bodies don't have nested calls — when they do we
	// fall back to "any quoted identifier at an even comma boundary".
	parts := splitTopLevel(body)
	for i, p := range parts {
		if i%2 != 0 {
			continue
		}
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, `"`) && strings.HasSuffix(p, `"`) && len(p) > 2 {
			name := p[1 : len(p)-1]
			if isPlainIdent(name) {
				fields = append(fields, PayloadField{Name: name})
			}
		}
	}
	return DedupFields(fields)
}

// splitTopLevel splits a string on commas, ignoring commas inside
// matched parens / brackets / quotes. Used by extractJavaMapOfKeys to
// honour nested Map.of(...) calls without descending.
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	inStr := false
	last := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && (i == 0 || s[i-1] != '\\'):
			inStr = !inStr
		case inStr:
			// skip
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[last:i])
			last = i + 1
		}
	}
	if last < len(s) {
		out = append(out, s[last:])
	}
	return out
}

// stripGenericSuffix removes a `<...>` generic clause from a type
// name so `List<UserDto>` becomes `List` and `UserDto` is left alone.
func stripGenericSuffix(s string) string {
	if i := strings.IndexByte(s, '<'); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
