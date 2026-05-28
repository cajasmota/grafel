// Phase 2A payload-shape sniffers for T3 languages (#2777).
//
// Languages in scope: dart, groovy, lua, swift, multi, astro, clojure,
// crystal, elm, erlang, fsharp, haskell, idris, lisp, nim, ocaml, pony,
// reasonml, rescript, sml, solidity, svelte, verilog, vhdl, vue, zig.
//
// Sniffer decision per language:
//
//	swift   (Vapor)     — full: req.content.decode(Foo.self) with Codable
//	                      structs; struct fields are statically inspectable.
//	crystal (Kemal)     — partial: env.params.json["x"] key-access pattern.
//	nim     (Prologue)  — partial: ctx.json(T) + ctx.getQueryParam patterns.
//	solidity            — full: external/public function parameter names in
//	                      ABI declarations ARE the calldata payload shape.
//	fsharp  (Giraffe)   — partial: bindJson<T> binds to a record type;
//	                      record fields visible in type annotation.
//	clojure (Ring)      — partial: (:key body) / (:keys [a b]) destructuring.
//	astro               — partial: delegates to JSTS sniffer over <script>
//	                      blocks (same mechanism as Phase 0 markup_script.go).
//	svelte              — partial: same JSTS delegation.
//	vue                 — partial: same JSTS delegation.
//	dart                — not_applicable: jsonDecode returns Map<String,dynamic>;
//	                      field names not statically visible at call site.
//	groovy              — not_applicable: Grails JsonBuilder / render are
//	                      not statically inspectable without AST.
//	lua                 — not_applicable: Lapis/OpenResty body is a string;
//	                      no idiomatic field-access pattern.
//	elm                 — not_applicable: pure frontend, no server side.
//	erlang              — not_applicable: Cowboy/Ranch bodies decoded at
//	                      runtime; field access is dynamic maps.
//	haskell             — not_applicable: Servant type-level routing is
//	                      inspectable only at the type level; pattern is
//	                      not line-regex-extractable.
//	idris               — not_applicable: no production HTTP framework.
//	lisp                — not_applicable: CL-HTTP bodies are runtime strings.
//	ocaml               — not_applicable: Dream/Opium body is a string;
//	                      field access requires full type inference.
//	pony                — not_applicable: no production HTTP framework.
//	reasonml            — not_applicable: compiled to JS — covered by jsts.
//	rescript            — not_applicable: compiled to JS — covered by jsts.
//	sml                 — not_applicable: no production HTTP framework.
//	verilog             — not_applicable: hardware description language.
//	vhdl                — not_applicable: hardware description language.
//	zig                 — not_applicable: minimal web ecosystem; no idiomatic
//	                      HTTP field-access pattern at Phase 2A granularity.
//	multi               — not_applicable: multi-lang category has no single
//	                      field-access idiom.
package substrate

import (
	"regexp"
	"strings"
)

// ── Swift / Vapor ─────────────────────────────────────────────────────────────

func init() { RegisterPayloadShapeSniffer("swift", sniffPayloadShapesSwift) }

// swiftCodableStructRe matches a Codable struct definition.
//
//	struct Foo: Codable { ... }
//	struct Foo: Content { ... }  (Vapor's Content = Codable)
//
// Capture group 1 = struct name.
var swiftCodableStructRe = regexp.MustCompile(
	`(?m)^\s*(?:public\s+|internal\s+|fileprivate\s+|private\s+)?struct\s+([A-Za-z_][\w]*)\s*:\s*` +
		`(?:[A-Za-z_][\w,\s]*\b)?(?:Codable|Content|Decodable)\b`,
)

// swiftStoredPropRe matches a stored property inside a struct/class body.
//
//	var name: Type
//	let name: Type
//	var name: Type?      (optional → Optional = true)
//
// Capture groups: 1 = name, 2 = type (may have trailing ?).
var swiftStoredPropRe = regexp.MustCompile(
	`(?m)^\s+(?:(?:public|internal|private|fileprivate|open)\s+)?(?:var|let)\s+` +
		`([A-Za-z_][\w]*)\s*:\s*([A-Za-z_][\w<>?\[\], .]+)`,
)

// swiftDecodeCallRe matches `req.content.decode(Foo.self)` and variants.
// Capture group 1 = DTO type name.
var swiftDecodeCallRe = regexp.MustCompile(
	`\b(?:req(?:uest)?|request)\s*\.\s*content\s*\.\s*decode\s*\(\s*([A-Za-z_][\w]*)\s*\.self\s*\)` +
		`|\btry\s+(?:req(?:uest)?|request)\s*\.content\.decode\s*\(\s*([A-Za-z_][\w]*)\s*\.self\s*\)`,
)

// swiftResponseEncodeRe matches `req.eventLoop.future(Foo(...))` or plain
// return of an Encodable-conforming value — conservative: only inline
// struct-literal construction `Foo(field: v, ...)`.
var swiftResponseEncodeRe = regexp.MustCompile(
	`\breturn\s+([A-Za-z_][\w]*)\s*\(([^)]*)\)`,
)

// swiftStructInitArgRe matches `label: value` inside a struct initialiser.
// Capture group 1 = label (the field name).
var swiftStructInitArgRe = regexp.MustCompile(
	`([A-Za-z_][\w]*)\s*:`,
)

// swiftFuncHeaderRe and scanSwiftFuncHeaders are declared in
// effect_sinks_swift.go (Phase 1A T3 — #2776).

func sniffPayloadShapesSwift(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	// Build a func-header list for attribution.
	headers := scanSwiftFuncHeaders(content)

	// 1. Build a map of Codable struct name → fields from struct declarations.
	codableFields := map[string][]PayloadField{}
	codablePos := map[string]int{} // struct declaration position for scan ordering
	type structScan struct {
		name  string
		start int
	}
	var structs []structScan
	for _, m := range swiftCodableStructRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		structs = append(structs, structScan{name: name, start: m[0]})
		codablePos[name] = m[0]
	}
	// For each Codable struct, extract stored properties that follow its
	// declaration until the next top-level closing brace.
	for _, s := range structs {
		body := extractBraceBody(content, s.start)
		var fields []PayloadField
		for _, m := range swiftStoredPropRe.FindAllStringSubmatchIndex(body, -1) {
			if len(m) < 6 {
				continue
			}
			fieldName := body[m[2]:m[3]]
			typ := strings.TrimSpace(body[m[4]:m[5]])
			optional := strings.HasSuffix(typ, "?")
			fields = append(fields, PayloadField{
				Name:     fieldName,
				Type:     strings.TrimSuffix(typ, "?"),
				Optional: optional,
			})
		}
		if len(fields) > 0 {
			codableFields[s.name] = DedupFields(fields)
		}
	}

	var out []PayloadShape

	// 2. Scan for decode calls — emit producer-side request shapes.
	for _, m := range swiftDecodeCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		var dtoName string
		if m[2] >= 0 {
			dtoName = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			dtoName = content[m[4]:m[5]]
		}
		if dtoName == "" {
			continue
		}
		fields, ok := codableFields[dtoName]
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
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 1.0,
		})
	}

	// 3. Scan for response encoding — `return Foo(field: v, ...)` patterns.
	for _, m := range swiftResponseEncodeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		dtoName := content[m[2]:m[3]]
		initArgs := content[m[4]:m[5]]
		// Use struct fields if available; fall back to init arg labels.
		var fields []PayloadField
		if f, ok := codableFields[dtoName]; ok {
			fields = f
		} else {
			for _, lm := range swiftStructInitArgRe.FindAllStringSubmatchIndex(initArgs, -1) {
				if len(lm) < 4 {
					continue
				}
				fields = append(fields, PayloadField{Name: initArgs[lm[2]:lm[3]]})
			}
			fields = DedupFields(fields)
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
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 0.85,
		})
	}

	return out
}

// extractBraceBody returns the text from the first `{` after offset `from`
// until its matching closing `}`, exclusive. Returns an empty string when no
// matching pair is found.
func extractBraceBody(content string, from int) string {
	start := strings.IndexByte(content[from:], '{')
	if start < 0 {
		return ""
	}
	start += from + 1
	depth := 1
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start:i]
			}
		}
	}
	return ""
}

// ── Solidity ──────────────────────────────────────────────────────────────────

func init() { RegisterPayloadShapeSniffer("solidity", sniffPayloadShapesSolidity) }

// solidityFuncRe matches external/public function declarations.
//
//	function transfer(address to, uint256 amount) external ...
//
// Capture groups: 1 = function name, 2 = parameter list.
var solidityFuncRe = regexp.MustCompile(
	`(?m)^\s*function\s+([A-Za-z_][\w]*)\s*\(([^)]*)\)\s*` +
		`(?:external|public)\b`,
)

// solidityParamRe matches a single parameter in a Solidity function signature.
// Capture groups: 1 = type (e.g. "address", "uint256"), 2 = name.
var solidityParamRe = regexp.MustCompile(
	`(address(?:\s+payable)?|uint\d*|int\d*|bytes\d*|bool|string|[A-Za-z_][\w]*(?:\[\])?)\s+([A-Za-z_][\w]*)`,
)

func sniffPayloadShapesSolidity(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	var out []PayloadShape
	for _, m := range solidityFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		fnName := content[m[2]:m[3]]
		paramList := content[m[4]:m[5]]
		var fields []PayloadField
		for _, pm := range solidityParamRe.FindAllStringSubmatchIndex(paramList, -1) {
			if len(pm) < 6 {
				continue
			}
			fields = append(fields, PayloadField{
				Name: paramList[pm[4]:pm[5]],
				Type: paramList[pm[2]:pm[3]],
			})
		}
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		out = append(out, PayloadShape{
			Function:   fnName,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 1.0,
		})
	}
	return out
}

// ── Crystal / Kemal ───────────────────────────────────────────────────────────

func init() { RegisterPayloadShapeSniffer("crystal", sniffPayloadShapesCrystal) }

// crystalJSONParamRe matches `env.params.json["key"]` field reads.
// Capture group 1 = field name.
var crystalJSONParamRe = regexp.MustCompile(
	`\benv\s*\.\s*params\s*\.\s*json\s*\[\s*"([A-Za-z_][\w]*)"\s*\]`,
)

// crystalHTTPParamRe matches `env.params.url["key"]` / `env.params.body["key"]`.
// Capture group 1 = field name.
var crystalHTTPParamRe = regexp.MustCompile(
	`\benv\s*\.\s*params\s*\.\s*(?:url|body|query)\s*\[\s*"([A-Za-z_][\w]*)"\s*\]`,
)

// crystalResponseRenderRe matches `env.response.print {...}` or `{key: val}` returns.
// Capture group 1 = hash body.
var crystalResponseRenderRe = regexp.MustCompile(
	`\benv\s*\.\s*response\s*\.\s*print\s+\{([^{}]*)\}` +
		`|\bhalt\s+env[^,]*,\s*\{([^{}]*)\}`,
)

// crystalFuncHeaderRe and scanCrystalFuncHeaders are declared in
// effect_sinks_crystal.go (Phase 1A T3 — #2776).

func sniffPayloadShapesCrystal(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanCrystalFuncHeaders(content)
	var out []PayloadShape

	// Producer-side request: bucket json/url/body param reads by function.
	reqFields := map[string][]PayloadField{}
	reqFirstLine := map[string]int{}
	scanCrystalParamReads(content, crystalJSONParamRe, headers, reqFields, reqFirstLine)
	scanCrystalParamReads(content, crystalHTTPParamRe, headers, reqFields, reqFirstLine)
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

	// Producer-side response: env.response.print {...}.
	for _, m := range crystalResponseRenderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		var body string
		if m[2] >= 0 {
			body = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			body = content[m[4]:m[5]]
		}
		if body == "" {
			continue
		}
		fields := extractCrystalHashKeys(body)
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
			Confidence: 0.9,
		})
	}
	return out
}

func scanCrystalParamReads(content string, re *regexp.Regexp, headers []funcHeader,
	fields map[string][]PayloadField, firstLine map[string]int) {
	for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields[fn] = append(fields[fn], PayloadField{Name: name})
		if _, ok := firstLine[fn]; !ok {
			firstLine[fn] = line
		}
	}
}

// crystalHashKeyRe matches `key: value` or `"key" => value` in a hash literal.
var crystalHashKeyRe = regexp.MustCompile(
	`([A-Za-z_][\w]*)\s*:(?:[^:])` +
		`|"([A-Za-z_][\w]*)"\s*=>`,
)

func extractCrystalHashKeys(body string) []PayloadField {
	var fields []PayloadField
	for _, m := range crystalHashKeyRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 6 {
			continue
		}
		var name string
		if m[2] >= 0 {
			name = body[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = body[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		fields = append(fields, PayloadField{Name: name})
	}
	return DedupFields(fields)
}

// ── Nim / Prologue + Jester ───────────────────────────────────────────────────

func init() { RegisterPayloadShapeSniffer("nim", sniffPayloadShapesNim) }

// nimQueryParamRe matches `ctx.getQueryParam("key")` / `request.params["key"]`.
// Capture group 1 = field name.
var nimQueryParamRe = regexp.MustCompile(
	`\bctx\s*\.\s*getQueryParam\s*\(\s*"([A-Za-z_][\w]*)"\s*\)` +
		`|\brequest\s*\.\s*params\s*\[\s*"([A-Za-z_][\w]*)"\s*\]`,
)

// nimFormDataRe matches `ctx.getFormParam("key")` / `ctx.multipart.data["key"]`.
// Capture group 1 = field name.
var nimFormDataRe = regexp.MustCompile(
	`\bctx\s*\.\s*getFormParam\s*\(\s*"([A-Za-z_][\w]*)"\s*\)` +
		`|\bctx\s*\.\s*multipart\s*\.\s*data\s*\[\s*"([A-Za-z_][\w]*)"\s*\]`,
)

// nimFuncHeaderRe and scanNimFuncHeaders are declared in
// effect_sinks_nim.go (Phase 1A T3 — #2776).

func sniffPayloadShapesNim(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanNimFuncHeaders(content)
	var out []PayloadShape

	reqFields := map[string][]PayloadField{}
	reqFirstLine := map[string]int{}
	scanNimParamReads(content, nimQueryParamRe, headers, reqFields, reqFirstLine)
	scanNimParamReads(content, nimFormDataRe, headers, reqFields, reqFirstLine)
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
	return out
}

func scanNimParamReads(content string, re *regexp.Regexp, headers []funcHeader,
	fields map[string][]PayloadField, firstLine map[string]int) {
	for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		var name string
		if m[2] >= 0 {
			name = content[m[2]:m[3]]
		} else if m[4] >= 0 {
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
		fields[fn] = append(fields[fn], PayloadField{Name: name})
		if _, ok := firstLine[fn]; !ok {
			firstLine[fn] = line
		}
	}
}

// ── F# / Giraffe ──────────────────────────────────────────────────────────────

func init() { RegisterPayloadShapeSniffer("fsharp", sniffPayloadShapesFSharp) }

// fsharpBindJsonRe matches `bindJson<DtoType>` handler composition.
// Capture group 1 = DTO type name.
var fsharpBindJsonRe = regexp.MustCompile(
	`\bbindJson\s*<\s*([A-Za-z_][\w]*)\s*>`,
)

// fsharpRecordTypeRe matches F# record type definitions.
//
//	type Foo = { name: string; email: string }
//
// Capture groups: 1 = type name, 2 = field block.
var fsharpRecordTypeRe = regexp.MustCompile(
	`(?m)^\s*type\s+([A-Za-z_][\w]*)\s*=\s*\{([^}]+)\}`,
)

// fsharpRecordFieldRe matches a single field in an F# record.
// Uses multiline mode so $ anchors to the end of each line.
//
//	Name: string
//
// Capture groups: 1 = field name, 2 = type (trailing whitespace trimmed).
var fsharpRecordFieldRe = regexp.MustCompile(
	`(?m)^\s*([A-Za-z_][\w]*)\s*:\s*([A-Za-z_][\w<>. ]+?)\s*$`,
)

// fsharpLetFuncRe matches `let name` or `let name arg =` handler definitions.
var fsharpLetFuncRe = regexp.MustCompile(
	`(?m)^\s*let\s+([A-Za-z_][\w]*)\s`,
)

func sniffPayloadShapesFSharp(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanFSharpFuncHeaders(content)

	// Build record-type map: type name → fields.
	recordFields := map[string][]PayloadField{}
	for _, m := range fsharpRecordTypeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		typeName := content[m[2]:m[3]]
		body := content[m[4]:m[5]]
		var fields []PayloadField
		for _, fm := range fsharpRecordFieldRe.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 6 {
				continue
			}
			fieldName := body[fm[2]:fm[3]]
			typ := strings.TrimSpace(body[fm[4]:fm[5]])
			optional := strings.HasSuffix(typ, "option") || strings.HasPrefix(typ, "option<") || strings.HasPrefix(typ, "Option<")
			fields = append(fields, PayloadField{
				Name:     fieldName,
				Type:     typ,
				Optional: optional,
			})
		}
		if len(fields) > 0 {
			recordFields[typeName] = DedupFields(fields)
		}
	}

	var out []PayloadShape

	// Emit request shapes for each bindJson<T> call.
	for _, m := range fsharpBindJsonRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		typeName := content[m[2]:m[3]]
		fields, ok := recordFields[typeName]
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
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 0.85,
		})
	}
	return out
}

func scanFSharpFuncHeaders(content string) []funcHeader {
	var out []funcHeader
	for _, m := range fsharpLetFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, funcHeader{
			Name: content[m[2]:m[3]],
			Line: lineOfOffset(content, m[0]),
		})
	}
	return out
}

// ── Clojure / Ring ────────────────────────────────────────────────────────────

func init() { RegisterPayloadShapeSniffer("clojure", sniffPayloadShapesClojure) }

// clojureBodyKeyRe matches `(:key body)` field reads — Ring idiom.
// Capture group 1 = field name.
var clojureBodyKeyRe = regexp.MustCompile(
	`\(:([A-Za-z_-][\w-]*)\s+(?:body|params|data|request)\b`,
)

// clojureRingMetaKeys are Ring request-map keys that represent request
// metadata, not payload fields. Excluded from field extraction.
var clojureRingMetaKeys = map[string]bool{
	"body": true, "headers": true, "uri": true, "method": true,
	"status": true, "query-string": true, "server-name": true,
	"server-port": true, "remote-addr": true, "scheme": true,
	"request-method": true, "content-type": true, "content-length": true,
}

// clojureDestructureRe matches `{:keys [a b c]}` destructuring of a request map.
// Capture group 1 = the keys list.
var clojureDestructureRe = regexp.MustCompile(
	`\{:keys\s+\[([^\]]+)\]\}`,
)

// clojureResponseRe matches `(response/ok {:key val})`.
// Capture group 1 = map body.
var clojureResponseRe = regexp.MustCompile(
	`\b(?:response/ok|ring\.util\.response/response)\s+\{([^{}]*)\}`,
)

// clojureMapKeyRe matches `:key val` inside a Ring response map literal.
var clojureMapKeyRe = regexp.MustCompile(`:([A-Za-z_-][\w-]*)`)

// clojureDefnRe matches `(defn name`.
var clojureDefnRe = regexp.MustCompile(
	`(?m)^\(defn-?\s+([A-Za-z_!?*+\-][\w!?*+\-]*)`,
)

func sniffPayloadShapesClojure(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanClojureFuncHeaders(content)
	var out []PayloadShape

	// Producer-side request: (:key body) reads. Filter Ring metadata keys
	// (e.g. :body, :headers) that are request envelope entries, not payload fields.
	reqFields := map[string][]PayloadField{}
	reqFirstLine := map[string]int{}
	for _, m := range clojureBodyKeyRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if clojureRingMetaKeys[name] {
			continue
		}
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
	// {:keys [a b]} destructuring — high confidence (inline literal list).
	for _, m := range clojureDestructureRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		keyList := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		for _, tok := range strings.Fields(keyList) {
			tok = strings.TrimSpace(tok)
			if tok != "" && isPlainIdentClojure(tok) {
				reqFields[fn] = append(reqFields[fn], PayloadField{Name: tok})
			}
		}
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

	// Producer-side response: (response/ok {:key val}).
	for _, m := range clojureResponseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		var fields []PayloadField
		for _, km := range clojureMapKeyRe.FindAllStringSubmatchIndex(body, -1) {
			if len(km) < 4 {
				continue
			}
			fields = append(fields, PayloadField{Name: body[km[2]:km[3]]})
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
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 0.9,
		})
	}
	return out
}

func scanClojureFuncHeaders(content string) []funcHeader {
	var out []funcHeader
	for _, m := range clojureDefnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, funcHeader{
			Name: content[m[2]:m[3]],
			Line: lineOfOffset(content, m[0]),
		})
	}
	return out
}

// isPlainIdentClojure reports whether s is a Clojure-valid bare symbol
// (no colons, no spaces, non-empty).
func isPlainIdentClojure(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '(' || c == ')' || c == '[' || c == ']' {
			return false
		}
	}
	return true
}

// ── Vue / Svelte / Astro — markup-script payload shapes ───────────────────────
//
// Single-file components embed JS/TS inside <script> blocks. For
// payload-shape inference, extract every <script> block and delegate
// to the JSTS sniffer. Line numbers are offset to match the original
// markup file (mirroring the Phase 0 markup_script.go approach).

func init() {
	RegisterPayloadShapeSniffer("svelte", sniffPayloadShapesMarkupScript)
	RegisterPayloadShapeSniffer("vue", sniffPayloadShapesMarkupScript)
	RegisterPayloadShapeSniffer("astro", sniffPayloadShapesMarkupScript)
}

func sniffPayloadShapesMarkupScript(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	var out []PayloadShape
	for _, m := range scriptBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		lineOffset := lineOfOffset(content, m[2]) - 1
		for _, s := range sniffPayloadShapesJSTS(body) {
			s.Line += lineOffset
			out = append(out, s)
		}
	}
	return out
}
