// PHP payload-shape sniffer (#2771 Phase 2A T2).
//
// Producer-side shapes (Laravel / Symfony / Slim / vanilla):
//
//   - Laravel FormRequest `rules()` returning `[ 'name' => '...', ... ]`
//     — the array keys are the validated request field set.
//   - `$request->only(['a', 'b'])`, `$request->all()` (we skip — no
//     usable shape), `$request->get('x')`, `$request->input('x')` —
//     bare bracket / get reads bind to the enclosing controller method.
//   - `$_POST['x']` / `$_GET['x']` superglobal reads (vanilla / legacy).
//   - `return response()->json([ 'x' => ..., 'y' => ... ])` and
//     `return new JsonResponse([ 'x' => ... ])` — inline assoc-array
//     response bodies.
//
// Consumer-side shapes (Guzzle):
//
//   - `$client->request('POST', $url, [ 'json' => [ 'x' => ... ] ])`
//   - `$client->post($url, [ 'form_params' => [ 'x' => ... ] ])`
//   - `$client->post($url, [ 'json' => [ 'x' => ... ] ])`
//
// Optional/required: PHP doesn't statically annotate. Phase 2A leaves
// Optional default-false on every shape. Symfony `@SerializedName` is
// out of scope for the regex sniffer — Phase 4 candidate.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("php", sniffPayloadShapesPHP) }

// phpRulesArrayRe matches a Laravel FormRequest `public function
// rules()` returning an inline assoc array. Capture group 1 = array
// body between the outer `[` and matching `]` (single-level — bounded
// by [^\[\]] so nested arrays terminate the match honestly).
var phpRulesArrayRe = regexp.MustCompile(
	`\bfunction\s+rules\s*\([^)]*\)\s*(?::\s*\w+\s*)?\{\s*return\s*\[([^\[\]]*)\]`,
)

// phpRequestReadRe matches `$request->get('x')`, `$request->input('x')`,
// and `$request->only(['x', 'y'])`. Capture group 1 = single name for
// get/input; group 2 = the bracketed name list for only.
var phpRequestReadRe = regexp.MustCompile(
	`\$(?:request|req)\s*->\s*(?:get|input)\s*\(\s*['"]([A-Za-z_][\w]*)['"]\s*\)` +
		`|\$(?:request|req)\s*->\s*only\s*\(\s*\[([^\[\]]*)\]`,
)

// phpSuperglobalRe matches `$_POST['x']` / `$_GET['x']`. Capture
// group 1 = the bare name.
var phpSuperglobalRe = regexp.MustCompile(
	`\$_(?:POST|GET|REQUEST)\s*\[\s*['"]([A-Za-z_][\w]*)['"]\s*\]`,
)

// phpJsonResponseRe matches `response()->json([...])` and
// `new JsonResponse([...])`. Capture group 1 = the array body.
var phpJsonResponseRe = regexp.MustCompile(
	`\bresponse\s*\(\s*\)\s*->\s*json\s*\(\s*\[([^\[\]]*)\]` +
		`|\bnew\s+JsonResponse\s*\(\s*\[([^\[\]]*)\]`,
)

// phpGuzzleBodyRe matches Guzzle outbound calls with an inline body
// option. Capture groups:
//
//	1 = HTTP verb when present (request('POST', ...) → POST)
//	2 = inline URL
//	3 = body assoc array (json / form_params)
var phpGuzzleBodyRe = regexp.MustCompile(
	`->\s*(?:request\s*\(\s*['"](GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)['"]\s*,\s*['"]([^'"]*)['"]` +
		`|(get|post|put|patch|delete|head|options)\s*\(\s*['"]([^'"]*)['"])` +
		`[^)]*?(?:'json'|"json"|'form_params'|"form_params")\s*=>\s*\[([^\[\]]*)\]`,
)

// phpAssocKeyRe matches a key in a PHP assoc array literal:
// `'name' =>` or `"name" =>`. Capture group 1 = the bare name.
var phpAssocKeyRe = regexp.MustCompile(
	`['"]([A-Za-z_][\w]*)['"]\s*=>`,
)

func sniffPayloadShapesPHP(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanPHPFuncHeaders(content)
	var out []PayloadShape

	// Producer-side: FormRequest rules() → request shape (high conf).
	for _, m := range phpRulesArrayRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractPHPAssocKeys(body)
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			fn = "rules"
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 1.0,
		})
	}

	// Producer-side: $request->get/input/only reads.
	reqFields := map[string][]PayloadField{}
	reqFirstLine := map[string]int{}
	collect := func(fn string, line int, names ...string) {
		for _, n := range names {
			if n == "" {
				continue
			}
			reqFields[fn] = append(reqFields[fn], PayloadField{Name: n})
		}
		if _, ok := reqFirstLine[fn]; !ok {
			reqFirstLine[fn] = line
		}
	}
	for _, m := range phpRequestReadRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		switch {
		case m[2] >= 0:
			collect(fn, line, content[m[2]:m[3]])
		case m[4] >= 0:
			// Extract single-quoted strings from the only-list body.
			body := content[m[4]:m[5]]
			for _, sm := range regexp.MustCompile(`['"]([A-Za-z_][\w]*)['"]`).FindAllStringSubmatchIndex(body, -1) {
				if len(sm) >= 4 {
					collect(fn, line, body[sm[2]:sm[3]])
				}
			}
		}
	}
	// Producer-side: $_POST / $_GET superglobal reads bucket the same way.
	for _, m := range phpSuperglobalRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		collect(fn, line, content[m[2]:m[3]])
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

	// Producer-side: response()->json([...]) literal response shape.
	for _, m := range phpJsonResponseRe.FindAllStringSubmatchIndex(content, -1) {
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
		fields := extractPHPAssocKeys(body)
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

	// Consumer-side: Guzzle json / form_params body.
	for _, m := range phpGuzzleBodyRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		var verb, url string
		switch {
		case m[2] >= 0:
			verb = strings.ToUpper(content[m[2]:m[3]])
			url = content[m[4]:m[5]]
		case m[6] >= 0:
			verb = strings.ToUpper(content[m[6]:m[7]])
			url = content[m[8]:m[9]]
		}
		body := content[m[10]:m[11]]
		fields := extractPHPAssocKeys(body)
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

// extractPHPAssocKeys lifts `'name' =>` / `"name" =>` keys from a PHP
// assoc-array body. Deduped, source order preserved.
func extractPHPAssocKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range phpAssocKeyRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 4 {
			continue
		}
		fields = append(fields, PayloadField{Name: body[m[2]:m[3]]})
	}
	return DedupFields(fields)
}
