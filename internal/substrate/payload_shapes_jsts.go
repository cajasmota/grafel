// JS/TS payload-shape sniffer (#2770 Phase 2A T1).
//
// Producer-side shapes (Express / Koa / Fastify / Next.js API routes):
//
//   - `req.body.X`, `req.body["X"]`, `request.body.X` — field reads off
//     the request body. Each access contributes one PayloadField to the
//     enclosing handler's request shape.
//   - `res.json({X, Y})`, `res.send({X, Y})`, `res.status(...).json({X})`,
//     `return Response.json({X, Y})`, `return new Response(JSON.stringify({X}))`
//     — inline response shapes. Object-shorthand `{X, Y}` and explicit
//     `{X: ..., Y: ...}` are both recognised.
//
// Consumer-side shapes (axios / fetch / useForm / react-hook-form /
// useMutation body builders):
//
//   - `axios.<verb>(url, {X, Y})` and `axios({url, method, data: {X}})`
//   - `fetch(url, {body: JSON.stringify({X, Y})})`
//   - `useForm({defaultValues: {X, Y}})`,
//     `useForm<{X, Y}>(...)` (TS generic),
//     `useMutation({...}).mutate({X, Y})` — destructured directly.
//
// All shapes use the conservative confidence model from the issue:
// inline literal evidence is 1.0; multi-statement evidence (request
// reads spread across the handler body) drops to 0.8.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("jsts", sniffPayloadShapesJSTS) }

// jstsFuncHeaderRe is re-used from effect_sinks_jsts.go. It already
// recognises arrow / function / method shorthand.

// jstsRequestFieldAccessRe matches `req.body.X`, `req.body["X"]`,
// `request.body.X`. Capture group 1 / 2 = field name.
var jstsRequestFieldAccessRe = regexp.MustCompile(
	`\b(?:req|request|ctx\.request)\s*\.\s*body\s*` +
		`(?:\.\s*([A-Za-z_][\w]*)|\[\s*['"]([A-Za-z_][\w]*)['"]\s*\])`,
)

// jstsResponseSendRe matches the start of a `res.json({...})` /
// `res.send({...})` / `res.status(...).json({...})` /
// `Response.json({...})` call. Capture group 1 is the literal body
// between `{` and the first balanced `}` on the same statement — we
// extract keys conservatively, single-level only.
var jstsResponseSendRe = regexp.MustCompile(
	`(?:\bres\s*\.\s*(?:json|send)|\bres\s*\.\s*status\s*\([^)]*\)\s*\.\s*(?:json|send)|\bResponse\s*\.\s*json|new\s+Response\s*\(\s*JSON\s*\.\s*stringify)\s*\(\s*\{([^{}]*)\}`,
)

// jstsConsumerAxiosRe matches `axios.<verb>(url, {...})` and
// `fetch(url, {body: JSON.stringify({...})})`. Capture groups:
//
//	1 = verb (axios) — uppercased in code
//	2 = inline URL (when present) for axios
//	3 = body literal for axios
//	4 = inline URL for fetch
//	5 = body literal for fetch (inside JSON.stringify)
var jstsConsumerAxiosRe = regexp.MustCompile(
	`\baxios\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*` +
		`(?:[\x60'"]([^\x60'"]*)[\x60'"])?` +
		`[^)]*?\{([^{}]*)\}` +
		`|\bfetch\s*\(\s*(?:[\x60'"]([^\x60'"]*)[\x60'"])?` +
		`[^)]*?JSON\s*\.\s*stringify\s*\(\s*\{([^{}]*)\}`,
)

// jstsUseFormRe matches `useForm({defaultValues: {...}})` and the TS
// generic form `useForm<{...}>(...)`. Capture groups:
//
//	1 = defaultValues body
//	2 = TS generic body
var jstsUseFormRe = regexp.MustCompile(
	`\buseForm\s*\(\s*\{[^{}]*defaultValues\s*:\s*\{([^{}]*)\}` +
		`|\buseForm\s*<\s*\{([^{}]*)\}\s*>`,
)

// jstsObjectShorthandKeyRe matches keys in either shorthand
// (`{X, Y}`) or explicit (`{X: ..., Y: ...}`) form within an object
// literal body. Capture group 1 = key name.
var jstsObjectShorthandKeyRe = regexp.MustCompile(
	`([A-Za-z_$][\w$]*)\s*(?::|,|\}|$)`,
)

// jstsReservedKey filters keywords that would otherwise be picked up
// by the broad shorthand regex.
var jstsReservedKey = map[string]bool{
	"true": true, "false": true, "null": true, "undefined": true,
	"return": true, "if": true, "else": true, "function": true,
	"const": true, "let": true, "var": true, "new": true,
	"this": true, "typeof": true, "instanceof": true,
	"async": true, "await": true, "yield": true,
}

func sniffPayloadShapesJSTS(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanJSTSFuncHeaders(content)
	var out []PayloadShape

	// Producer-side request reads.
	requestFields := map[string][]PayloadField{}
	requestFirstLine := map[string]int{}
	for _, m := range jstsRequestFieldAccessRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		requestFields[fn] = append(requestFields[fn], PayloadField{Name: name})
		if _, ok := requestFirstLine[fn]; !ok {
			requestFirstLine[fn] = line
		}
	}
	for fn, fields := range requestFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       requestFirstLine[fn],
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.8,
		})
	}

	// Producer-side response sends.
	for _, m := range jstsResponseSendRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractObjectKeysJSTS(body)
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

	// Consumer-side request shapes from axios / fetch.
	for _, m := range jstsConsumerAxiosRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		var verb, url, body string
		switch {
		case m[2] >= 0: // axios.<verb> branch
			verb = strings.ToUpper(content[m[2]:m[3]])
			if m[4] >= 0 {
				url = content[m[4]:m[5]]
			}
			body = content[m[6]:m[7]]
		case m[10] >= 0: // fetch(...) branch — body inside JSON.stringify
			body = content[m[10]:m[11]]
			if m[8] >= 0 {
				url = content[m[8]:m[9]]
			}
			// fetch() defaults to GET; the body presence implies POST/PUT.
			// Leave VerbHint empty when not explicitly specified.
		}
		fields := extractObjectKeysJSTS(body)
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, PayloadShape{
			Function:     fn,
			Line:         line,
			Direction:    PayloadDirectionRequest,
			Side:         PayloadSideConsumer,
			Fields:       fields,
			Confidence:   1.0,
			EndpointHint: url,
			VerbHint:     verb,
		})
	}

	// Consumer-side form shapes from useForm.
	for _, m := range jstsUseFormRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		var body string
		switch {
		case m[2] >= 0:
			body = content[m[2]:m[3]]
		case m[4] >= 0:
			body = content[m[4]:m[5]]
		}
		fields := extractObjectKeysJSTS(body)
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
			Confidence: 0.85,
		})
	}

	return out
}

// extractObjectKeysJSTS lifts identifier keys out of an inline object-
// literal body. Recognises both shorthand (`{x, y}`) and explicit
// (`{x: v, y: v}`) forms. Filters reserved-word collisions and TS type
// keywords. Single-level — nested objects are not descended into.
func extractObjectKeysJSTS(body string) []PayloadField {
	var fields []PayloadField
	// Split on commas at the top level (no balanced-brace handling — we
	// already restrict the regex to single-level via [^{}]*).
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Strip spread operator.
		if strings.HasPrefix(part, "...") {
			continue
		}
		// `key: value` or shorthand `key`.
		colon := strings.IndexByte(part, ':')
		var key string
		if colon > 0 {
			key = strings.TrimSpace(part[:colon])
		} else {
			key = part
		}
		// Strip TS optional marker (`key?: type` already stripped at colon).
		key = strings.TrimSuffix(key, "?")
		// Strip trailing whitespace / closing brace fragments.
		key = strings.TrimSpace(key)
		// Bracketed computed keys are skipped — non-static.
		if strings.HasPrefix(key, "[") {
			continue
		}
		// Quoted string keys.
		if strings.HasPrefix(key, "'") || strings.HasPrefix(key, `"`) {
			key = strings.Trim(key, `'"`)
		}
		if key == "" || jstsReservedKey[key] {
			continue
		}
		// Reject anything that doesn't look like a bare identifier.
		if !isPlainIdent(key) {
			continue
		}
		fields = append(fields, PayloadField{Name: key})
	}
	return DedupFields(fields)
}

// isPlainIdent reports whether s is a non-empty identifier in the
// `[A-Za-z_$][\w$]*` shape. Single-pass byte loop — no regex allocation.
func isPlainIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z',
			c >= 'a' && c <= 'z',
			c == '_', c == '$':
			// always ok
		case (c >= '0' && c <= '9') && i > 0:
			// ok in non-leading position
		default:
			return false
		}
	}
	return true
}
