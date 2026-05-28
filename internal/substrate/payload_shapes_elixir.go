// Elixir payload-shape sniffer (#2771 Phase 2A T2).
//
// Producer-side shapes (Phoenix controllers / Plug pipelines):
//
//   - `def action(conn, %{"a" => a, "b" => b})` — Phoenix's idiomatic
//     params map destructuring. The string keys are the request shape.
//   - `params["a"]`, `Map.get(params, "a")` — bare read fallback.
//   - `Ecto.Schema` `field :name, :type` declarations contribute to a
//     same-module schema map. When `cast(attrs, [:a, :b])` is called
//     with a literal list, those names are also recorded as the
//     request shape for the surrounding changeset function.
//   - `conn |> json(%{a: ..., b: ...})` and `json(conn, %{...})` —
//     inline map response bodies. Bracket-key and bareword-atom forms
//     are both recognised.
//
// Consumer-side shapes (HTTPoison / Tesla):
//
//   - `HTTPoison.<verb>(url, Jason.encode!(%{a: ..., b: ...}))`
//   - `Tesla.<verb>(client, url, %{a: ..., b: ...})`
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("elixir", sniffPayloadShapesElixir) }

// exDefRe is re-used from effect_sinks_elixir.go via scanElixirFuncHeaders.

// exDestructureRe matches a function head with a destructured params
// map as the second argument: `def action(conn, %{"a" => a, "b" => b})`.
// Capture group 1 = the literal map body between %{ and }.
var exDestructureRe = regexp.MustCompile(
	`\bdef\s+[A-Za-z_][\w?!]*\s*\([^)]*,\s*%\{([^{}]*)\}\s*\)`,
)

// exParamsReadRe matches `params["x"]` and `Map.get(params, "x")` /
// `Map.get(params, :x)`. Capture groups 1/2/3 each hold the name.
var exParamsReadRe = regexp.MustCompile(
	`\bparams\s*\[\s*"([A-Za-z_][\w]*)"\s*\]` +
		`|\bMap\s*\.\s*get\s*\(\s*params\s*,\s*"([A-Za-z_][\w]*)"` +
		`|\bMap\s*\.\s*get\s*\(\s*params\s*,\s*:([A-Za-z_][\w]*)`,
)

// exEctoFieldRe matches `field :name, :type` declarations inside an
// Ecto schema block. Capture group 1 = the field atom.
var exEctoFieldRe = regexp.MustCompile(
	`(?m)^\s*field\s+:([A-Za-z_][\w]*)`,
)

// exCastListRe matches `cast(attrs, [:a, :b, :c])`. Capture group 1 =
// the atom list body between `[` and `]`.
var exCastListRe = regexp.MustCompile(
	`\bcast\s*\([^,]*,\s*\[([^\[\]]*)\]`,
)

// exJSONResponseRe matches `json(conn, %{...})` and `|> json(%{...})`.
// Capture group 1 = body between %{ and }.
var exJSONResponseRe = regexp.MustCompile(
	`\bjson\s*\(\s*(?:conn\s*,\s*)?%\{([^{}]*)\}` +
		`|\|>\s*json\s*\(\s*%\{([^{}]*)\}`,
)

// exConsumerRe matches HTTPoison / Tesla outbound calls with an inline
// map body. Capture groups:
//
//	1 = HTTPoison verb, 2 = url, 3 = body inside Jason.encode!(%{...})
//	4 = Tesla verb,     5 = url, 6 = body inside %{...}
var exConsumerRe = regexp.MustCompile(
	`\bHTTPoison\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*"([^"]*)"[^)]*?Jason\s*\.\s*encode!\s*\(\s*%\{([^{}]*)\}` +
		`|\bTesla\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*[^,]+,\s*"([^"]*)"[^)]*?%\{([^{}]*)\}`,
)

// exMapKeyRe matches a key in an Elixir map literal: `"name" =>`
// (string keys) or bareword atom `name:`. Capture groups 1/2 hold the
// name (first non-empty wins).
var exMapKeyRe = regexp.MustCompile(
	`"([A-Za-z_][\w]*)"\s*=>` +
		`|\b([A-Za-z_][\w]*)\s*:`,
)

// exAtomListRe matches a `:name` atom inside a list literal.
var exAtomListRe = regexp.MustCompile(`:([A-Za-z_][\w]*)`)

func sniffPayloadShapesElixir(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanElixirFuncHeaders(content)
	var out []PayloadShape

	// Producer-side: def action(conn, %{"a" => a}) — destructure.
	for _, m := range exDestructureRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		fields := extractElixirMapKeys(body)
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
			Fields:     fields,
			Confidence: 1.0,
		})
	}

	// Producer-side: bare params reads.
	paramFields := map[string][]PayloadField{}
	paramFirstLine := map[string]int{}
	for _, m := range exParamsReadRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
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
		}
		if name == "" {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		paramFields[fn] = append(paramFields[fn], PayloadField{Name: name})
		if _, ok := paramFirstLine[fn]; !ok {
			paramFirstLine[fn] = line
		}
	}
	for fn, fields := range paramFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       paramFirstLine[fn],
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.8,
		})
	}

	// Producer-side: Ecto `cast(attrs, [:a, :b])` whitelist.
	for _, m := range exCastListRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		var fields []PayloadField
		for _, am := range exAtomListRe.FindAllStringSubmatchIndex(body, -1) {
			if len(am) >= 4 {
				fields = append(fields, PayloadField{Name: body[am[2]:am[3]]})
			}
		}
		fields = DedupFields(fields)
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
			Fields:     fields,
			Confidence: 0.9,
		})
	}

	// Producer-side: Ecto `field :name, :type` schema declarations.
	schemaFields := map[string][]PayloadField{}
	schemaFirstLine := map[string]int{}
	for _, m := range exEctoFieldRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		schemaFields[fn] = append(schemaFields[fn], PayloadField{Name: name})
		if _, ok := schemaFirstLine[fn]; !ok {
			schemaFirstLine[fn] = line
		}
	}
	for fn, fields := range schemaFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       schemaFirstLine[fn],
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.85,
		})
	}

	// Producer-side: json(conn, %{...}) response.
	for _, m := range exJSONResponseRe.FindAllStringSubmatchIndex(content, -1) {
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
		fields := extractElixirMapKeys(body)
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

	// Consumer-side: HTTPoison / Tesla inline body.
	for _, m := range exConsumerRe.FindAllStringSubmatchIndex(content, -1) {
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
		fields := extractElixirMapKeys(body)
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

// extractElixirMapKeys lifts keys out of an Elixir map literal body
// (text between %{ and }). Both "name" => and bareword name: forms.
func extractElixirMapKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range exMapKeyRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 6 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = body[m[2]:m[3]]
		case m[4] >= 0:
			name = body[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		fields = append(fields, PayloadField{Name: name})
	}
	return DedupFields(fields)
}
