// Go payload-shape sniffer (#2770 Phase 2A T1).
//
// Producer-side shapes:
//
//   - `json.NewDecoder(r.Body).Decode(&v)` / `json.Unmarshal(body, &v)`
//     where `v` is a struct declared in the same file → fields are the
//     struct's exported members (with `json:"..."` tag overrides where
//     present).
//   - `r.FormValue("X")`, `r.PostFormValue("X")`, `r.URL.Query().Get("X")`
//     — inline request reads bind to the enclosing handler.
//   - Response shapes: `json.NewEncoder(w).Encode(&v)`,
//     `json.Marshal(&v)`, `c.JSON(http.StatusOK, gin.H{"X": v, "Y": v})`,
//     `c.JSON(http.StatusOK, &v)` — same struct/literal recognition.
//
// Consumer-side shapes:
//
//   - `http.Post(url, "application/json", bytes.NewBuffer(b))` where
//     `b` was assembled from a literal `map[string]any{...}` /
//     `map[string]interface{}{...}` / a struct literal — we follow the
//     literal back when it is in the same function body.
//   - `client.Do(req)` where `req` body was assembled from `json.Marshal(map{...})`.
//
// Cross-file struct resolution is Phase 4; this sniffer is intentionally
// single-file. Optional/required: tags `json:",omitempty"` flip Optional
// = true on the corresponding field; everything else defaults to
// Optional = false.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("go", sniffPayloadShapesGo) }

// goStructHeaderRe matches `type Name struct {`. Capture group 1 = the
// type name. The struct body is consumed up to the matching `}` by a
// brace counter (see scanGoStructFields).
var goStructHeaderRe = regexp.MustCompile(
	`(?m)^type\s+([A-Z][\w]*)\s+struct\s*\{`,
)

// goStructFieldRe matches a struct field declaration:
//
//	Name Type `json:"jsonName,omitempty"`
//
// Capture group 1 = field name; group 2 = type; group 3 = the
// content of the json tag when present, else empty.
var goStructFieldRe = regexp.MustCompile(
	`(?m)^\s*([A-Z][\w]*)\s+(\*?\[?\]?[A-Za-z_][\w./\[\]\*]*)` +
		"(?:\\s+`[^`]*json:\"([^\"]*)\"[^`]*`)?",
)

// goDecodeIntoRe matches a JSON decoder/unmarshal call into &v.
// Capture group 1 = the bare identifier being decoded into.
var goDecodeIntoRe = regexp.MustCompile(
	`\bjson\s*\.\s*(?:NewDecoder\s*\([^)]*\)\s*\.\s*Decode|Unmarshal)\s*\(\s*(?:[^,)]*,\s*)?&\s*([A-Za-z_][\w]*)`,
)

// goEncodeFromRe matches a JSON encoder/marshal call from &v. Capture
// group 1 = the bare identifier being encoded.
var goEncodeFromRe = regexp.MustCompile(
	`\bjson\s*\.\s*(?:NewEncoder\s*\([^)]*\)\s*\.\s*Encode|Marshal(?:Indent)?)\s*\(\s*&?\s*([A-Za-z_][\w]*)`,
)

// goVarTypeRe matches `var name Type` and `name := Type{` and
// `name Type` parameter declarations. Capture group 1 = name; group 2
// = type. Single-line only.
var goVarTypeRe = regexp.MustCompile(
	`(?m)\b(?:var\s+)?([A-Za-z_][\w]*)\s+([A-Z][\w]*)\b|(?m)\b([A-Za-z_][\w]*)\s*:=\s*&?([A-Z][\w]*)\s*\{`,
)

// goFormValueRe matches `r.FormValue("X")`, `r.PostFormValue("X")`,
// `r.URL.Query().Get("X")`. Capture group 1 / 2 / 3 = field name.
var goFormValueRe = regexp.MustCompile(
	`\.\s*(?:FormValue|PostFormValue)\s*\(\s*"([A-Za-z_][\w]*)"\s*\)` +
		`|\.\s*URL\s*\.\s*Query\s*\(\s*\)\s*\.\s*Get\s*\(\s*"([A-Za-z_][\w]*)"\s*\)`,
)

// goGinHRe matches `gin.H{"X": v, "Y": v}` and the bare `c.JSON(...,
// gin.H{...})`. Capture group 1 = body between { and }.
var goGinHRe = regexp.MustCompile(
	`\bgin\s*\.\s*H\s*\{([^{}]*)\}`,
)

// goMapStringAnyRe matches `map[string]any{...}` and
// `map[string]interface{}{...}`. Capture group 1 = body.
var goMapStringAnyRe = regexp.MustCompile(
	`map\s*\[\s*string\s*\]\s*(?:any|interface\s*\{\s*\})\s*\{([^{}]*)\}`,
)

// goQuotedKeyRe matches `"X":` keys inside Go map literals. Capture 1
// = name.
var goQuotedKeyRe = regexp.MustCompile(`"([A-Za-z_][\w]*)"\s*:`)

// goJSONPostBodyRe matches `http.Post(url, contentType, body)` calls.
// Capture group 1 = inline URL; the body is recognised separately via
// the literal scans above. We only use this match's URL/verb for the
// EndpointHint binding.
var goJSONPostBodyRe = regexp.MustCompile(
	`\bhttp\s*\.\s*(Post|Get|PostForm)\s*\(\s*"([^"]*)"`,
)

func sniffPayloadShapesGo(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanGoFuncHeaders(content)
	structs := scanGoStructFields(content)
	varTypes := scanGoVarTypes(content)

	var out []PayloadShape

	// Producer-side: json.Decoder.Decode(&v) → request shape from v's
	// resolved struct.
	for _, m := range goDecodeIntoRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		v := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		typ := varTypes[v]
		fields := structs[typ]
		if len(fields) == 0 {
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

	// Producer-side: inline FormValue / Query().Get reads.
	formFields := map[string][]PayloadField{}
	formFirstLine := map[string]int{}
	for _, m := range goFormValueRe.FindAllStringSubmatchIndex(content, -1) {
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
		formFields[fn] = append(formFields[fn], PayloadField{Name: name})
		if _, ok := formFirstLine[fn]; !ok {
			formFirstLine[fn] = line
		}
	}
	for fn, fields := range formFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       formFirstLine[fn],
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.85,
		})
	}

	// Producer-side: gin.H{...} response literals.
	for _, m := range goGinHRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractGoMapKeys(body)
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

	// Producer-side: json.Encode(&v) → response shape from v's struct.
	for _, m := range goEncodeFromRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		v := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		typ := varTypes[v]
		fields := structs[typ]
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

	// Consumer-side: map[string]any{...} literals used as request
	// bodies. We don't try to confirm they're wrapped in a request —
	// the heuristic is "any map[string]any literal inside a function
	// that also makes an http client call". Conservative: only emit
	// when the function nearestHeader has at least one http.Post-shaped
	// call site.
	if httpPostFns := scanGoHTTPPostFns(content, headers); len(httpPostFns) > 0 {
		for _, m := range goMapStringAnyRe.FindAllStringSubmatchIndex(content, -1) {
			if len(m) < 4 {
				continue
			}
			body := content[m[2]:m[3]]
			fields := extractGoMapKeys(body)
			if len(fields) == 0 {
				continue
			}
			line := lineOfOffset(content, m[0])
			fn := nearestHeader(headers, line)
			if fn == "" || !httpPostFns[fn] {
				continue
			}
			out = append(out, PayloadShape{
				Function:   fn,
				Line:       line,
				Direction:  PayloadDirectionRequest,
				Side:       PayloadSideConsumer,
				Fields:     fields,
				Confidence: 0.85,
			})
		}
	}

	return out
}

// scanGoStructFields walks the file once and returns a map of
// structName → []PayloadField. The struct body is consumed up to the
// matching `}` by a brace counter.
func scanGoStructFields(content string) map[string][]PayloadField {
	out := map[string][]PayloadField{}
	for _, m := range goStructHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		// Walk from the `{` of the struct header to the matching `}`.
		body, ok := goBalancedBlock(content, m[1]-1)
		if !ok {
			continue
		}
		var fields []PayloadField
		for _, fm := range goStructFieldRe.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 8 {
				continue
			}
			fname := body[fm[2]:fm[3]]
			ftype := strings.TrimSpace(body[fm[4]:fm[5]])
			jsonTag := ""
			if fm[6] >= 0 {
				jsonTag = body[fm[6]:fm[7]]
			}
			optional := false
			if jsonTag != "" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] != "" && parts[0] != "-" {
					fname = parts[0]
				}
				for _, p := range parts[1:] {
					if p == "omitempty" {
						optional = true
					}
				}
			}
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

// goBalancedBlock consumes s starting at the `{` at or after openIdx
// and returns the body (between the braces, exclusive) plus true on
// success.
func goBalancedBlock(s string, openIdx int) (string, bool) {
	for openIdx < len(s) && s[openIdx] != '{' {
		openIdx++
	}
	if openIdx >= len(s) {
		return "", false
	}
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], true
			}
		}
	}
	return "", false
}

// scanGoVarTypes builds a map from local-variable name to its declared
// type (best-effort, single-statement). Used by the decoder/encoder
// recognisers to bind &v back to a struct definition.
func scanGoVarTypes(content string) map[string]string {
	out := map[string]string{}
	for _, m := range goVarTypeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		// Either branch may have matched. Prefer the `name :=` branch
		// over the `var name Type` branch since composite-literal
		// expressions are more explicit.
		switch {
		case m[6] >= 0:
			out[content[m[6]:m[7]]] = content[m[8]:m[9]]
		case m[2] >= 0:
			// `var name Type` or `name Type` (parameter); we accept
			// both shapes since the regex doesn't distinguish them.
			name := content[m[2]:m[3]]
			typ := content[m[4]:m[5]]
			if _, ok := out[name]; !ok {
				out[name] = typ
			}
		}
	}
	return out
}

// scanGoHTTPPostFns returns the set of functions that contain an
// outbound HTTP POST-shaped call. The consumer-side map-literal scan
// uses this to suppress map literals that are clearly not request
// bodies.
func scanGoHTTPPostFns(content string, headers []funcHeader) map[string]bool {
	out := map[string]bool{}
	for _, m := range goJSONPostBodyRe.FindAllStringSubmatchIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		if fn := nearestHeader(headers, line); fn != "" {
			out[fn] = true
		}
	}
	// Also consider `.Do(` / `.Post(` style without the http.* prefix.
	for _, m := range regexp.MustCompile(`\.\s*(?:Do|Post|PostForm)\s*\(`).FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		if fn := nearestHeader(headers, line); fn != "" {
			out[fn] = true
		}
	}
	return out
}

// extractGoMapKeys lifts quoted-string keys out of a map-literal body.
func extractGoMapKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range goQuotedKeyRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 4 {
			continue
		}
		fields = append(fields, PayloadField{Name: body[m[2]:m[3]]})
	}
	return DedupFields(fields)
}
