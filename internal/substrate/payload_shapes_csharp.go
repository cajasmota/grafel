// C# payload-shape sniffer (#2771 Phase 2A T2).
//
// Producer-side shapes (ASP.NET Core / Carter / FastEndpoints):
//
//   - `[FromBody] T name` parameter — the wrapped DTO T's public
//     properties (declared in the same file) become the request shape.
//   - DTO classes / records: `public string Name { get; set; }` and
//     positional record `record CreateDto(string Name, int Age)`.
//     `[Required]` is treated as default (Optional=false); nullable
//     reference / `int?` flips Optional=true.
//   - Response shapes: `return Ok(new T { A = ..., B = ... })` and
//     `return new JsonResult(new { A = ..., B = ... })` — anonymous
//     object initializers contribute their property set.
//
// Consumer-side shapes (HttpClient + JsonContent / StringContent):
//
//   - `client.PostAsJsonAsync(url, new { A = ..., B = ... })`
//   - `JsonContent.Create(new { A = ..., B = ... })`
//   - `new StringContent(JsonSerializer.Serialize(new { A = ... }))` —
//     out of scope for the regex sniffer (multi-call); we recognise
//     PostAsJsonAsync as the canonical idiom.
//
// Optional/required: `?` (nullable) flips Optional=true; everything
// else stays default-false (the `[Required]` attribute is implied).
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("csharp", sniffPayloadShapesCSharp) }

// csClassHeaderRe matches a C# class or record declaration.
// Capture group 1 = the type name.
var csClassHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:public\s+|internal\s+|private\s+|protected\s+|sealed\s+|abstract\s+|static\s+|partial\s+)*(?:class|record)\s+([A-Z][\w]*)\b`,
)

// csPropertyRe matches a public property declaration:
// `public string Name { get; set; }`. Capture group 1 = type;
// group 2 = name.
var csPropertyRe = regexp.MustCompile(
	`(?m)^\s*public\s+([A-Za-z_][\w<>?\[\],\s.]*?)\s+([A-Z][\w]*)\s*\{\s*get\s*;`,
)

// csPositionalRecordRe matches `record Name(string A, int B, ...)`.
// Capture group 1 = type name; group 2 = the parameter list body.
var csPositionalRecordRe = regexp.MustCompile(
	`(?m)^\s*(?:public\s+|internal\s+)?record\s+([A-Z][\w]*)\s*\(([^)]*)\)`,
)

// csFromBodyParamRe matches `[FromBody] T name`. Capture group 1 = T;
// group 2 = parameter name.
var csFromBodyParamRe = regexp.MustCompile(
	`\[FromBody\]\s+([A-Z][\w<>,?\s.]*)\s+([A-Za-z_][\w]*)`,
)

// csAnonObjectRe matches `new { A = ..., B = ... }`. Capture group 1 =
// body between `{` and `}`. Bounded single-level.
var csAnonObjectRe = regexp.MustCompile(
	`\bnew\s*\{([^{}]*)\}`,
)

// csPostAsJsonRe matches `client.PostAsJsonAsync(url, ...)`. Capture
// group 1 = inline URL when present, group 2 = verb (POST/PUT etc).
var csPostAsJsonRe = regexp.MustCompile(
	`\.\s*(Post|Put|Patch|Get|Delete)AsJsonAsync\s*\(\s*[$@]?"([^"]*)"`,
)

// csAnonAssignRe matches `Identifier =` inside an anonymous-object
// body. Capture group 1 = the property name. C# property casing is
// conventionally PascalCase but anonymous-object members can be any
// identifier shape; we accept both. The trailing `(?:[^=])` excludes
// `==` comparisons.
var csAnonAssignRe = regexp.MustCompile(
	`\b([A-Za-z_][\w]*)\s*=(?:[^=])`,
)

func sniffPayloadShapesCSharp(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanCSharpFuncHeaders(content)
	classFields := scanCSharpClassFields(content)

	var out []PayloadShape

	// Producer-side: [FromBody] T name → request shape from T.
	for _, m := range csFromBodyParamRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		typ := strings.TrimSpace(content[m[2]:m[3]])
		typ = stripGenericSuffix(typ)
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := classFields[typ]
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

	// Producer-side: anonymous-object initializers as response shape.
	// We must also tag PostAsJsonAsync usages as consumer; detect those
	// by checking whether the surrounding line contains the marker.
	clientLines := scanCSharpClientLines(content)
	for _, m := range csAnonObjectRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractCSharpAnonKeys(body)
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if hint, ok := clientLines[line]; ok {
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

// scanCSharpClassFields walks the file once and returns a map of
// className → []PayloadField. Recognises both property-style classes
// and positional records.
func scanCSharpClassFields(content string) map[string][]PayloadField {
	out := map[string][]PayloadField{}
	// Property-style: bucket between consecutive class headers.
	type block struct {
		name       string
		start, end int
	}
	var blocks []block
	matches := csClassHeaderRe.FindAllStringSubmatchIndex(content, -1)
	for i, m := range matches {
		if len(m) < 4 {
			continue
		}
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		blocks = append(blocks, block{name: content[m[2]:m[3]], start: m[1], end: end})
	}
	for _, b := range blocks {
		body := content[b.start:b.end]
		var fields []PayloadField
		for _, fm := range csPropertyRe.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 6 {
				continue
			}
			typ := strings.TrimSpace(body[fm[2]:fm[3]])
			name := body[fm[4]:fm[5]]
			optional := strings.HasSuffix(typ, "?")
			fields = append(fields, PayloadField{Name: name, Type: typ, Optional: optional})
		}
		if len(fields) > 0 {
			out[b.name] = DedupFields(fields)
		}
	}
	// Positional records: shape = constructor parameters.
	for _, m := range csPositionalRecordRe.FindAllStringSubmatchIndex(content, -1) {
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
			// `Type Name` — split on last whitespace.
			i := strings.LastIndexAny(p, " \t")
			if i <= 0 {
				continue
			}
			typ := strings.TrimSpace(p[:i])
			fname := strings.TrimSpace(p[i+1:])
			if !isPlainIdent(fname) {
				continue
			}
			optional := strings.HasSuffix(typ, "?")
			fields = append(fields, PayloadField{Name: fname, Type: typ, Optional: optional})
		}
		if len(fields) > 0 {
			out[name] = DedupFields(fields)
		}
	}
	return out
}

// csharpClientHint mirrors the rust hint struct.
type csharpClientHint struct {
	url  string
	verb string
}

// scanCSharpClientLines returns a line-keyed map of HttpClient
// PostAsJsonAsync / PutAsJsonAsync call sites. The anonymous-object
// recognition uses this to flip side=consumer when the literal is the
// body argument on the same line.
func scanCSharpClientLines(content string) map[int]csharpClientHint {
	out := map[int]csharpClientHint{}
	for _, m := range csPostAsJsonRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		line := lineOfOffset(content, m[0])
		verb := strings.ToUpper(content[m[2]:m[3]])
		// HttpClient.{Verb}AsJsonAsync → HTTP verb is the prefix.
		out[line] = csharpClientHint{url: content[m[4]:m[5]], verb: verb}
	}
	return out
}

// extractCSharpAnonKeys lifts `Name =` property assignments out of a
// `new { ... }` body. Deduped, source order preserved.
func extractCSharpAnonKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range csAnonAssignRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 4 {
			continue
		}
		fields = append(fields, PayloadField{Name: body[m[2]:m[3]]})
	}
	return DedupFields(fields)
}
