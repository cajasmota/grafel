// Ruby payload-shape sniffer (#2771 Phase 2A T2).
//
// Producer-side shapes (Rails / Sinatra / Grape / Hanami handlers):
//
//   - `params.require(:foo).permit(:a, :b, :c)` — the permit list is
//     the canonical Rails strong-parameter request shape.
//   - `params[:x]`, `params.fetch(:x)`, `params.fetch("x")` — bare
//     symbol / string indexed reads bind to the enclosing action.
//   - `render json: { x: ..., y: ... }` — inline hash returned as the
//     response body. The hash literal is captured single-line.
//   - Grape `expose :name` declarations inside an `Entity` class become
//     the response shape for any endpoint that `presents` the entity
//     (the present/expose pairing is recognised conservatively per
//     enclosing block via nearestHeader).
//
// Consumer-side shapes (HTTParty / Faraday / Net::HTTP / RestClient):
//
//   - `HTTParty.<verb>(url, body: { x: ... }.to_json)`,
//     `HTTParty.<verb>(url, query: { x: ... })`
//   - `Faraday.<verb>(url) { |req| req.body = { x: ... }.to_json }` —
//     we match the inline hash regardless of the block wrapper.
//   - `Net::HTTP.post_form(URI(url), { "x" => ... })` — the form-hash
//     literal contributes the field set.
//
// Optional/required: Ruby doesn't statically annotate. Phase 2A leaves
// Optional default-false on every shape.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("ruby", sniffPayloadShapesRuby) }

// rubyPermitListRe matches the canonical Rails strong-parameter pattern
// `params.require(:foo).permit(:a, :b, :c)`. Capture group 1 = the
// permit arg list between parens (we then split on commas to extract
// the bare symbols).
var rubyPermitListRe = regexp.MustCompile(
	`\bparams\s*(?:\.\s*require\s*\([^)]*\))?\s*\.\s*permit\s*\(([^)]*)\)`,
)

// rubyParamsIndexRe matches `params[:x]` and `params["x"]` and
// `params.fetch(:x)` / `params.fetch("x")`. Capture groups 1/2/3/4 each
// hold the bare name (first non-empty wins).
var rubyParamsIndexRe = regexp.MustCompile(
	`\bparams\s*` +
		`(?:\[\s*:([A-Za-z_][\w]*)\s*\]` +
		`|\[\s*['"]([A-Za-z_][\w]*)['"]\s*\]` +
		`|\.\s*fetch\s*\(\s*:([A-Za-z_][\w]*)` +
		`|\.\s*fetch\s*\(\s*['"]([A-Za-z_][\w]*)['"])`,
)

// rubyRenderJSONRe matches `render json: { ... }`. Capture group 1 is
// the hash body between { and the first balanced } on the same line.
var rubyRenderJSONRe = regexp.MustCompile(
	`\brender\s+json\s*:\s*\{([^{}]*)\}`,
)

// rubyGrapeExposeRe matches Grape `expose :name` (the entity pattern).
// Capture group 1 = the field name.
var rubyGrapeExposeRe = regexp.MustCompile(
	`(?m)^\s*expose\s+:([A-Za-z_][\w]*)`,
)

// rubyConsumerHTTPRe matches inline outbound calls with a hash body.
// Capture groups:
//
//	1 = HTTParty verb when present
//	2 = inline URL for HTTParty
//	3 = body hash (between { and })
//	4 = Faraday verb when present
//	5 = inline URL for Faraday
//	6 = body hash for Faraday (req.body = {...})
var rubyConsumerHTTPRe = regexp.MustCompile(
	`\bHTTParty\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*['"]([^'"]*)['"][^)]*?(?:body|query|json)\s*:\s*\{([^{}]*)\}` +
		`|\bFaraday\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*['"]([^'"]*)['"][^)]*?\.\s*body\s*=\s*\{([^{}]*)\}`,
)

// rubyHashKeyRe matches a key in a Ruby hash literal: bare `name:`,
// symbol `:name =>`, or string `"name" =>`. Capture groups 1/2/3 hold
// the name (first non-empty wins).
var rubyHashKeyRe = regexp.MustCompile(
	`([A-Za-z_][\w]*)\s*:` +
		`|:([A-Za-z_][\w]*)\s*=>` +
		`|['"]([A-Za-z_][\w]*)['"]\s*=>`,
)

// rubySymbolListRe matches a single `:name` symbol inside the permit
// argument list. Bare identifiers with no colon are ignored — Rails
// permit only accepts symbols / strings / hashes.
var rubySymbolListRe = regexp.MustCompile(`:([A-Za-z_][\w]*)`)

func sniffPayloadShapesRuby(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanRubyFuncHeaders(content)
	var out []PayloadShape

	// Producer-side: permit list → request shape (high confidence).
	for _, m := range rubyPermitListRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		var fields []PayloadField
		for _, sm := range rubySymbolListRe.FindAllStringSubmatchIndex(body, -1) {
			if len(sm) < 4 {
				continue
			}
			fields = append(fields, PayloadField{Name: body[sm[2]:sm[3]]})
		}
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
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 1.0,
		})
	}

	// Producer-side: bare params[:x] / params.fetch reads — fallback
	// when the handler doesn't use strong parameters.
	idxFields := map[string][]PayloadField{}
	idxFirstLine := map[string]int{}
	for _, m := range rubyParamsIndexRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		case m[6] >= 0:
			name = content[m[6]:m[7]]
		case m[8] >= 0:
			name = content[m[8]:m[9]]
		}
		if name == "" {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		idxFields[fn] = append(idxFields[fn], PayloadField{Name: name})
		if _, ok := idxFirstLine[fn]; !ok {
			idxFirstLine[fn] = line
		}
	}
	for fn, fields := range idxFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       idxFirstLine[fn],
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.8,
		})
	}

	// Producer-side: render json: {...} response shape.
	for _, m := range rubyRenderJSONRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractRubyHashKeys(body)
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

	// Producer-side: Grape expose declarations bucket-by-enclosing-
	// header (the Entity class' header binds them).
	exposeFields := map[string][]PayloadField{}
	exposeFirstLine := map[string]int{}
	for _, m := range rubyGrapeExposeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		exposeFields[fn] = append(exposeFields[fn], PayloadField{Name: name})
		if _, ok := exposeFirstLine[fn]; !ok {
			exposeFirstLine[fn] = line
		}
	}
	for fn, fields := range exposeFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       exposeFirstLine[fn],
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.85,
		})
	}

	// Consumer-side: HTTParty / Faraday inline body.
	for _, m := range rubyConsumerHTTPRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 14 {
			continue
		}
		var verb, url, body string
		switch {
		case m[2] >= 0:
			verb = strings.ToUpper(content[m[2]:m[3]])
			url = content[m[4]:m[5]]
			body = content[m[6]:m[7]]
		case m[8] >= 0:
			verb = strings.ToUpper(content[m[8]:m[9]])
			url = content[m[10]:m[11]]
			body = content[m[12]:m[13]]
		}
		fields := extractRubyHashKeys(body)
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

// extractRubyHashKeys lifts the keys out of a Ruby hash-literal body,
// recognising both modern (`name: value`) and rocket (`:name => value`
// / `"name" => value`) forms. Deduped, source order preserved.
func extractRubyHashKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range rubyHashKeyRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 8 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = body[m[2]:m[3]]
		case m[4] >= 0:
			name = body[m[4]:m[5]]
		case m[6] >= 0:
			name = body[m[6]:m[7]]
		}
		if name == "" {
			continue
		}
		fields = append(fields, PayloadField{Name: name})
	}
	return DedupFields(fields)
}
