// C/C++ payload-shape sniffer (#2771 Phase 2A T2).
//
// C/C++ web frameworks are mostly opaque from a static-analysis view —
// payloads are typically `const char*` blobs or hand-rolled JSON
// readers, so there is no equivalent of Python's DRF serializer or
// Spring's `@RequestBody` DTO to mine. We restrict this sniffer to the
// two cases where structure is statically observable:
//
//   - cpprestsdk producer: `body[U("x")]` field reads via the
//     `web::json::value` accessor. Each access contributes one field
//     to the enclosing handler's request shape.
//   - cpprestsdk producer response literal: a `json::value::object`
//     followed by repeated `result[U("x")] = ...` assignments. Each
//     assignment contributes one field to the response shape.
//   - libcurl consumer: `curl_easy_setopt(..., CURLOPT_POSTFIELDS,
//     "x=1&y=2");` — the urlencoded body is parsed for keys.
//
// Everything else (Boost.Beast, Drogon, Pistache, Crow) is recognised
// by file (so the registry cell flips to "partial" rather than "full"
// — Phase 4 would add per-framework recognisers).
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("c-cpp", sniffPayloadShapesCCPP) }

// cppBodyAccessRe matches a cpprestsdk JSON-value access:
// `body[U("name")]` or `value[U("name")]` or
// `request[U("name")]`. Capture group 1 = the field name.
var cppBodyAccessRe = regexp.MustCompile(
	`\b(?:body|value|request|req|json)\s*\[\s*U\s*\(\s*"([A-Za-z_][\w]*)"\s*\)\s*\]`,
)

// cppResponseAssignRe matches `result[U("name")] = ...` and
// `response[U("name")] = ...` — typical cpprestsdk response builders.
// Capture group 1 = the field name. Reuses cppBodyAccessRe shape but
// requires a trailing `=` to disambiguate writes from reads.
var cppResponseAssignRe = regexp.MustCompile(
	`\b(?:result|response|resp|output|out)\s*\[\s*U\s*\(\s*"([A-Za-z_][\w]*)"\s*\)\s*\]\s*=(?:[^=])`,
)

// cppCurlPostFieldsRe matches `curl_easy_setopt(handle, CURLOPT_POSTFIELDS, "x=1&y=2")`.
// Capture group 1 = the urlencoded body string.
var cppCurlPostFieldsRe = regexp.MustCompile(
	`\bcurl_easy_setopt\s*\([^,]*,\s*CURLOPT_POSTFIELDS(?:_COPY)?\s*,\s*"([^"]*)"\s*\)`,
)

func sniffPayloadShapesCCPP(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanCCPPFuncHeaders(content)
	var out []PayloadShape

	// Producer-side: body[U("x")] reads bucket by enclosing function.
	reqFields := map[string][]PayloadField{}
	reqFirstLine := map[string]int{}
	for _, m := range cppBodyAccessRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		reqFields[fn] = append(reqFields[fn], PayloadField{Name: name})
		if _, ok := reqFirstLine[fn]; !ok {
			reqFirstLine[fn] = line
		}
	}
	for fn, fields := range reqFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       reqFirstLine[fn],
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.8,
		})
	}

	// Producer-side: result[U("x")] = ... assignments → response.
	respFields := map[string][]PayloadField{}
	respFirstLine := map[string]int{}
	for _, m := range cppResponseAssignRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		respFields[fn] = append(respFields[fn], PayloadField{Name: name})
		if _, ok := respFirstLine[fn]; !ok {
			respFirstLine[fn] = line
		}
	}
	for fn, fields := range respFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       respFirstLine[fn],
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.9,
		})
	}

	// Consumer-side: libcurl CURLOPT_POSTFIELDS urlencoded body.
	for _, m := range cppCurlPostFieldsRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractURLEncodedKeys(body)
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

// extractURLEncodedKeys parses a `"x=1&y=2"` urlencoded body string
// into a deduped field slice. Empty / malformed segments are skipped.
func extractURLEncodedKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, seg := range strings.Split(body, "&") {
		if seg == "" {
			continue
		}
		eq := strings.IndexByte(seg, '=')
		var key string
		if eq >= 0 {
			key = seg[:eq]
		} else {
			key = seg
		}
		key = strings.TrimSpace(key)
		if !isPlainIdent(key) {
			continue
		}
		fields = append(fields, PayloadField{Name: key})
	}
	return DedupFields(fields)
}
