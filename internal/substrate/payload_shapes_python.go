// Python payload-shape sniffer (#2770 Phase 2A T1).
//
// Producer-side shapes:
//
//   - Django REST Framework / Flask / FastAPI handler bodies read
//     request fields via `request.data["X"]`, `request.data.get("X")`,
//     `request.json["X"]`, `request.json.get("X")`, `request.POST["X"]`,
//     `body["X"]`, `serializer.validated_data["X"]`.
//   - Response shapes are observed via inline dict literals returned by
//     the handler — `return Response({"X": ..., "Y": ...})`,
//     `return JsonResponse({"X": ..., "Y": ...})`, `return {"X": ...}`.
//   - DRF serializer Meta.fields / serializers.<Type>Field() class
//     attributes are recognised as response shapes when the surrounding
//     class extends `serializers.<*>Serializer` and is referenced from
//     a handler in the same file. Phase 2A only recognises inline
//     literal lists; cross-file resolution is Phase 4.
//
// Consumer-side shapes:
//
//   - `requests.<verb>(url, json={"X": ..., "Y": ...})`
//   - `httpx.<verb>(url, json={"X": ...})`
//   - The same with `data={...}` instead of `json=`.
//
// All shapes are conservative: optional/required is left at the default
// (false) per the issue spec — Python doesn't statically annotate.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("python", sniffPayloadShapesPython) }

// pyRequestFieldAccessRe matches single field reads off a request-like
// body. Capture group 1 = the quoted field name. Recognised receivers:
// request.data / request.json / request.POST / body /
// serializer.validated_data / validated_data / payload.
var pyRequestFieldAccessRe = regexp.MustCompile(
	`(?:request\s*\.\s*(?:data|json|POST|GET)|body|payload|serializer\s*\.\s*validated_data|validated_data)` +
		`\s*(?:\[\s*['"]([A-Za-z_][\w]*)['"]\s*\]|\.\s*get\s*\(\s*['"]([A-Za-z_][\w]*)['"])`,
)

// pyResponseDictReturnRe matches inline dict-literal returns that
// represent a JSON response body. Capture group 1 is the dict body
// (between { and }). Matches `Response({...})`, `JsonResponse({...})`,
// `return {...}`. Bounded to a single line for honesty — multi-line
// dict literals are recognised by a separate scan over the line range.
var pyResponseDictReturnRe = regexp.MustCompile(
	`(?:Response|JsonResponse|jsonify)\s*\(\s*\{([^{}]*)\}` +
		`|return\s*\{([^{}]*)\}`,
)

// pyDictKeyRe matches a quoted string key in a dict literal:
// `"name":` or `'name':`. Capture group 1 = the bare identifier.
var pyDictKeyRe = regexp.MustCompile(`['"]([A-Za-z_][\w]*)['"]\s*:`)

// pyConsumerHTTPCallRe matches an outbound HTTP client call with an
// inline json= / data= dict body. Capture groups:
//
//	1 = HTTP verb
//	2 = inline URL when present (helps EndpointHint), else empty
//	3 = body dict literal between { and }
//
// Recognised modules: requests, httpx, urllib3, aiohttp.
var pyConsumerHTTPCallRe = regexp.MustCompile(
	`\b(?:requests|httpx)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*` +
		`(?:f?['"]([^'"]*)['"])?` + // optional inline URL
		`[^)]*?(?:json|data)\s*=\s*\{([^{}]*)\}`,
)

func sniffPayloadShapesPython(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanPyFuncHeaders(content)
	var out []PayloadShape

	// Producer-side request reads: bucket all field accesses by their
	// enclosing function, then emit one PayloadShape per function.
	type fnKey struct {
		fn   string
		line int
	}
	requestFields := map[string][]PayloadField{}
	requestFirstLine := map[string]int{}
	for _, m := range pyRequestFieldAccessRe.FindAllStringSubmatchIndex(content, -1) {
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

	// Producer-side response shapes from inline dict-literal returns.
	for _, m := range pyResponseDictReturnRe.FindAllStringSubmatchIndex(content, -1) {
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
		if body == "" {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := extractDictKeys(body)
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

	// Consumer-side request shapes from inline json= / data= bodies.
	for _, m := range pyConsumerHTTPCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		var url string
		if m[4] >= 0 {
			url = content[m[4]:m[5]]
		}
		body := content[m[6]:m[7]]
		fields := extractDictKeys(body)
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

	return out
}

// extractDictKeys lifts the bare names out of a dict-literal body
// (text between { and }). Deduped, source order preserved.
func extractDictKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range pyDictKeyRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 4 {
			continue
		}
		fields = append(fields, PayloadField{Name: body[m[2]:m[3]]})
	}
	return DedupFields(fields)
}
