// Python response-shape extraction for Django, DRF, Flask, FastAPI.
//
// All four frameworks share enough surface that we use a single body
// scanner: find the `def <handler>(...)` block, walk every `return`
// statement, and classify by call shape:
//
//   - Response({...})                   Django REST Framework / DRF
//   - Response({...}, status=400)       error-status variant
//   - JsonResponse({...})               Django stdlib
//   - jsonify({...})                    Flask
//   - {...}                             dict literal (Flask, FastAPI)
//   - SomeModel(...)                    Pydantic / dataclass return; the
//     caller-side type is taken from a
//     FastAPI response_model decorator
//     (when present) or the function
//     return-type annotation.
//
// Request-body extraction (request_keys/request_schema) parses FastAPI
// parameter annotations: `body: SomeModel` → walk SomeModel's class body
// for `field: type` declarations.
package engine

import (
	"regexp"
	"strings"
)

// fastapiResponseModelRe captures the response_model=ClassName kwarg
// off a FastAPI decorator above the handler. Multiple decorators are
// supported; we only need the response_model token.
var fastapiResponseModelRe = regexp.MustCompile(`response_model\s*=\s*([A-Za-z_][\w.]*)`)

// pyClassFieldRe pulls `name: type` declarations out of a Pydantic /
// dataclass body. Group 1 is the field name, group 2 is the type token
// (everything up to `=` or end-of-line).
var pyClassFieldRe = regexp.MustCompile(`(?m)^[ \t]+([a-zA-Z_]\w*)\s*:\s*([^=\n#]+?)\s*(?:=|$)`)

// pyReturnRe matches `return <expr>` indented under a handler body.
// We anchor on word-boundary to skip yields/returns-in-comments.
var pyReturnRe = regexp.MustCompile(`(?m)^[ \t]+return\b\s*(.*)$`)

// pyStatusKwargRe captures `status=<int>` inside a Response(...) call.
var pyStatusKwargRe = regexp.MustCompile(`\bstatus(?:_code)?\s*=\s*(\d{3})`)

// pyStatusHTTPConstRe handles `status=status.HTTP_404_NOT_FOUND` symbolic forms.
var pyStatusHTTPConstRe = regexp.MustCompile(`\bstatus(?:_code)?\s*=\s*status\.HTTP_(\d{3})_`)

// findHandlerBody returns the source slice that contains the body of the
// named Python function, or "" if not found. We use indentation to
// determine where the function ends — the body is every line whose
// indent strictly exceeds the indent of the `def` line, up to the first
// line that breaks that condition.
func findHandlerBody(src, name string) string {
	if name == "" {
		return ""
	}
	re := regexp.MustCompile(`(?m)^([ \t]*)(?:async\s+)?def\s+` + regexp.QuoteMeta(name) + `\s*\(`)
	loc := re.FindStringSubmatchIndex(src)
	if loc == nil {
		return ""
	}
	defIndent := loc[3] - loc[2] // length of capture group 1
	// Start from the end of the def line. Find next newline.
	startLine := loc[0]
	headEnd := strings.Index(src[startLine:], "\n")
	if headEnd < 0 {
		return ""
	}
	bodyStart := startLine + headEnd + 1
	// The signature might span multiple lines (Python allows wrapped
	// argument lists); skip until we reach a line ending with `:`.
	for {
		// Find end of current line.
		nl := strings.Index(src[bodyStart-1:], "\n")
		if nl < 0 {
			break
		}
		prev := src[startLine : bodyStart-1+nl]
		if strings.Contains(prev, "):") || strings.HasSuffix(strings.TrimRight(prev, " \t\r"), ":") {
			break
		}
		bodyStart = bodyStart - 1 + nl + 1
		if bodyStart >= len(src) {
			return ""
		}
	}
	// Walk subsequent lines: include them while their indent > defIndent
	// (or while they're blank). Stop at the first non-blank, indent <= defIndent.
	i := bodyStart
	bodyEnd := bodyStart
	for i < len(src) {
		lineEnd := strings.Index(src[i:], "\n")
		if lineEnd < 0 {
			lineEnd = len(src) - i
		}
		line := src[i : i+lineEnd]
		stripped := strings.TrimLeft(line, " \t")
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			// blank/comment line; keep going.
			i += lineEnd + 1
			bodyEnd = i
			continue
		}
		indent := len(line) - len(stripped)
		if indent <= defIndent {
			break
		}
		i += lineEnd + 1
		bodyEnd = i
	}
	// Clamp bodyEnd to source length in case the last line lacked a newline.
	if bodyEnd > len(src) {
		bodyEnd = len(src)
	}
	return src[bodyStart:bodyEnd]
}

// extractPythonShape implements the shape extractor for all four Python
// frameworks. The `framework` argument tunes a few small per-framework
// behaviours (e.g. FastAPI response_model lookup uses decorators above
// the def line, which the others don't have in the same form).
func extractPythonShape(src, handler, framework string) shape {
	var sh shape
	if handler == "" {
		return sh
	}
	body := findHandlerBody(src, handler)
	if body == "" {
		return sh
	}
	// FastAPI response_model — look at the decorator block immediately
	// above the def line.
	if framework == "fastapi" {
		if m := lookupFastAPIResponseModel(src, handler); m != "" {
			schema := walkPyClassFields(src, m)
			if len(schema) > 0 {
				sh.responseSchema = schema
				keys := make([]string, 0, len(schema))
				for k := range schema {
					keys = append(keys, k)
				}
				sh.responseKeys = append(sh.responseKeys, keys...)
				sh.knownResponse = true
			}
		}
		// Request body: scan the def signature for `name: Model` annotations
		// where Model is a Pydantic class in the same file.
		if reqSchema := extractFastAPIRequestSchema(src, handler); len(reqSchema) > 0 {
			sh.requestSchema = reqSchema
			for k := range reqSchema {
				sh.requestKeys = append(sh.requestKeys, k)
			}
		}
	}
	// Scan returns.
	for _, m := range pyReturnRe.FindAllStringSubmatch(body, -1) {
		expr := strings.TrimSpace(m[1])
		if expr == "" || expr == "None" {
			continue
		}
		parsePyReturn(src, body, expr, &sh)
	}
	// If we still have only dynamicResponse (e.g. `return serializer.data`
	// resolved via self.get_serializer()), try the class-level serializer_class
	// as a last resort.
	if sh.dynamicResponse && !sh.knownResponse {
		if keys := drfResolveAndWalk(src, "", body); len(keys) > 0 {
			sh.responseKeys = append(sh.responseKeys, keys...)
			sh.knownResponse = true
			sh.dynamicResponse = false
			sh.responseKeysSource = "drf_serializer"
		}
	}
	return sh
}

// parsePyReturn inspects a single `return <expr>` and updates `sh` in place.
// `handlerBody` is the text of the enclosing function body, used for DRF
// local-variable resolution.
func parsePyReturn(src, handlerBody, expr string, sh *shape) {
	// Strip a trailing comment.
	if i := strings.Index(expr, " #"); i >= 0 {
		expr = strings.TrimSpace(expr[:i])
	}
	// Detect status code on the expression first (works for Response/JsonResponse).
	status := 0
	if m := pyStatusKwargRe.FindStringSubmatch(expr); len(m) >= 2 {
		if n, err := atoi(m[1]); err == nil {
			status = n
		}
	}
	if status == 0 {
		if m := pyStatusHTTPConstRe.FindStringSubmatch(expr); len(m) >= 2 {
			if n, err := atoi(m[1]); err == nil {
				status = n
			}
		}
	}
	// Common wrappers: Response(...), JsonResponse(...), jsonify(...).
	for _, wrapper := range []string{"Response", "JsonResponse", "jsonify"} {
		if idx := strings.Index(expr, wrapper+"("); idx == 0 || (idx > 0 && !isIdentChar(expr[idx-1])) {
			parenIdx := strings.Index(expr[idx:], "(")
			if parenIdx >= 0 {
				args := extractArgList(expr, idx+parenIdx)
				if len(args) > 0 {
					applyPyReturnArg(src, handlerBody, args[0], status, sh)
					recordStatus(sh, status, looksLikeError(args[0]))
					return
				}
			}
		}
	}
	// Bare dict literal (Flask / FastAPI implicit jsonify).
	if strings.HasPrefix(expr, "{") {
		// Could be `{...}` or `{...}, 200`.
		end := findMatchingBracket(expr, 0)
		if end > 0 {
			dict := expr[:end+1]
			rest := strings.TrimSpace(expr[end+1:])
			if strings.HasPrefix(rest, ",") {
				tail := strings.TrimSpace(strings.TrimPrefix(rest, ","))
				if n, err := atoi(strings.TrimRight(tail, " \r\n")); err == nil {
					status = n
				}
			}
			applyPyReturnArg(src, handlerBody, dict, status, sh)
			recordStatus(sh, status, false)
			return
		}
	}
	// `return SomeModel(...)` — walk the class fields if SomeModel is in this file.
	if m := regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)\s*\(`).FindStringSubmatch(expr); len(m) >= 2 {
		schema := walkPyClassFields(src, m[1])
		if len(schema) > 0 {
			if sh.responseSchema == nil {
				sh.responseSchema = schema
			}
			for k := range schema {
				sh.responseKeys = append(sh.responseKeys, k)
			}
			sh.knownResponse = true
			recordStatus(sh, status, false)
			return
		}
	}
	// `return serializer.data` / `return Response(serializer.data)` —
	// DRF idiom: try to resolve the serializer class and walk its fields.
	if strings.Contains(expr, ".data") || strings.Contains(expr, ".to_dict()") {
		// Extract the variable name before ".data" (e.g. "serializer" from "serializer.data").
		if dotIdx := strings.Index(expr, ".data"); dotIdx > 0 {
			varName := strings.TrimSpace(expr[:dotIdx])
			// Strip any wrapper: Response(serializer.data) → varName stays "serializer"
			if parenIdx := strings.LastIndexAny(varName, "("); parenIdx >= 0 {
				varName = strings.TrimSpace(varName[parenIdx+1:])
			}
			if varName != "" && varName != "self" {
				if keys := drfResolveAndWalk(src, varName, handlerBody); len(keys) > 0 {
					for _, k := range keys {
						sh.responseKeys = append(sh.responseKeys, k)
					}
					sh.knownResponse = true
					sh.responseKeysSource = "drf_serializer"
					return
				}
			}
		}
		sh.dynamicResponse = true
		return
	}
	// Free variable — mark dynamic.
	sh.dynamicResponse = true
}

// applyPyReturnArg merges a single argument (typically the body literal
// passed to Response(...)) into `sh`. `handlerBody` is passed for DRF
// serializer resolution when the arg is `serializer.data`.
func applyPyReturnArg(src, handlerBody, arg string, status int, sh *shape) {
	keys := extractDictKeys(arg)
	if len(keys) > 0 {
		sh.knownResponse = true
		if status >= 400 {
			sh.errorKeys = append(sh.errorKeys, keys...)
		} else {
			sh.responseKeys = append(sh.responseKeys, keys...)
		}
		return
	}
	// `Response(serializer.data)` — DRF pattern: the arg is `serializer.data`.
	if strings.Contains(arg, ".data") {
		if dotIdx := strings.Index(arg, ".data"); dotIdx > 0 {
			varName := strings.TrimSpace(arg[:dotIdx])
			if varName != "" && varName != "self" {
				if drfKeys := drfResolveAndWalk(src, varName, handlerBody); len(drfKeys) > 0 {
					sh.responseKeys = append(sh.responseKeys, drfKeys...)
					sh.knownResponse = true
					sh.responseKeysSource = "drf_serializer"
					return
				}
			}
		}
		sh.dynamicResponse = true
		return
	}
	// `return SomeModel(...)` inside a wrapper, e.g. Response(SomeModel(...)).
	if m := regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)\s*\(`).FindStringSubmatch(strings.TrimSpace(arg)); len(m) >= 2 {
		schema := walkPyClassFields(src, m[1])
		if len(schema) > 0 {
			if sh.responseSchema == nil {
				sh.responseSchema = schema
			}
			for k := range schema {
				sh.responseKeys = append(sh.responseKeys, k)
			}
			sh.knownResponse = true
			return
		}
	}
	// Unknown — flag dynamic.
	sh.dynamicResponse = true
}

// lookupFastAPIResponseModel scans the source for a FastAPI decorator
// immediately above the def line and returns the response_model class
// name, or "" when none was found.
func lookupFastAPIResponseModel(src, handler string) string {
	// Locate the handler's `def` line, then take the contiguous decorator
	// block immediately above it as the search region. This is immune to a
	// route decorator whose argument contains a ')' inside a string literal
	// (e.g. a regex route `@app.get("/items/{id:(\d+)}", response_model=...)`):
	// the previous leading `@[\w.]+\([^)]*\)` match stopped at that inner ')'
	// and failed to anchor to the def, dropping the response shape entirely.
	defRe := regexp.MustCompile(`(?m)^[ \t]*(?:async\s+)?def\s+` + regexp.QuoteMeta(handler) + `\s*\(`)
	loc := defRe.FindStringIndex(src)
	if loc == nil {
		return ""
	}
	// Walk backwards over the lines preceding the def, collecting the
	// contiguous run of decorator / decorator-continuation lines.
	prefix := src[:loc[0]]
	lines := strings.Split(prefix, "\n")
	// Drop a trailing empty element produced by the newline immediately before
	// the def line, so the scan starts on the real decorator line.
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	start := len(lines) // first line index (in `lines`) that belongs to the block
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			// FastAPI decorators are normally contiguous; stop at a blank gap.
			break
		}
		if strings.HasPrefix(t, "@") || strings.HasPrefix(t, ")") ||
			strings.HasPrefix(t, "#") || isDecoratorContinuation(t) {
			start = i
			continue
		}
		break
	}
	if start >= len(lines) {
		return ""
	}
	region := strings.Join(lines[start:], "\n")
	if m := fastapiResponseModelRe.FindStringSubmatch(region); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// isDecoratorContinuation reports whether a trimmed line is plausibly a
// continuation of a multi-line decorator argument list (e.g. a `response_model=`
// or `summary="..."` kwarg line, or a closing-paren line). Used to extend the
// decorator block scanned for `response_model=` upward from a handler def.
func isDecoratorContinuation(t string) bool {
	// kwarg-style continuation: `name=...` or quoted/paren fragments.
	if strings.Contains(t, "=") || strings.HasPrefix(t, "\"") ||
		strings.HasPrefix(t, "'") || strings.HasSuffix(t, ",") ||
		strings.HasSuffix(t, "(") {
		return true
	}
	return false
}

// extractFastAPIRequestSchema walks the def signature looking for a
// parameter annotated with a Pydantic-class type from the same file.
// Returns the class's field map, or nil when nothing was found.
func extractFastAPIRequestSchema(src, handler string) map[string]string {
	re := regexp.MustCompile(`(?:async\s+)?def\s+` + regexp.QuoteMeta(handler) + `\s*\(([^)]*)\)`)
	m := re.FindStringSubmatch(src)
	if len(m) < 2 {
		return nil
	}
	args := m[1]
	for _, arg := range strings.Split(args, ",") {
		arg = strings.TrimSpace(arg)
		// Match `name: TypeName` with TypeName starting capital.
		parts := strings.SplitN(arg, ":", 2)
		if len(parts) != 2 {
			continue
		}
		typ := strings.TrimSpace(parts[1])
		// Strip default value.
		if eq := strings.Index(typ, "="); eq >= 0 {
			typ = strings.TrimSpace(typ[:eq])
		}
		// Take leading identifier.
		idMatch := regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)`).FindStringSubmatch(typ)
		if len(idMatch) < 2 {
			continue
		}
		// FastAPI special types we should skip.
		switch idMatch[1] {
		case "Request", "Response", "BackgroundTasks", "Depends", "Path", "Query", "Header", "Cookie", "Body", "Form", "File", "UploadFile":
			continue
		}
		schema := walkPyClassFields(src, idMatch[1])
		if len(schema) > 0 {
			return schema
		}
	}
	return nil
}

// walkPyClassFields locates `class <name>(...):` in the source and returns
// a map of `field -> type` for every `name: type` declaration in the
// class body. Returns nil when the class is not found.
func walkPyClassFields(src, name string) map[string]string {
	re := regexp.MustCompile(`(?m)^([ \t]*)class\s+` + regexp.QuoteMeta(name) + `\b[^\n]*:`)
	loc := re.FindStringSubmatchIndex(src)
	if loc == nil {
		return nil
	}
	classIndent := loc[3] - loc[2]
	// Find the class body bounds the same way we did for handlers.
	headEnd := strings.Index(src[loc[0]:], "\n")
	if headEnd < 0 {
		return nil
	}
	bodyStart := loc[0] + headEnd + 1
	i := bodyStart
	bodyEnd := bodyStart
	for i < len(src) {
		lineEnd := strings.Index(src[i:], "\n")
		if lineEnd < 0 {
			lineEnd = len(src) - i
		}
		line := src[i : i+lineEnd]
		stripped := strings.TrimLeft(line, " \t")
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			i += lineEnd + 1
			bodyEnd = i
			continue
		}
		indent := len(line) - len(stripped)
		if indent <= classIndent {
			break
		}
		i += lineEnd + 1
		bodyEnd = i
	}
	body := src[bodyStart:bodyEnd]
	out := map[string]string{}
	for _, m := range pyClassFieldRe.FindAllStringSubmatch(body, -1) {
		fname := m[1]
		ftype := strings.TrimSpace(m[2])
		// Skip dunder fields and obvious non-field declarations.
		if strings.HasPrefix(fname, "_") {
			continue
		}
		out[fname] = ftype
	}
	return out
}

// ---------------------------------------------------------------------------
// DRF Serializer walking
// ---------------------------------------------------------------------------

// drfSerializerFieldRe matches class-attribute serializer field assignments:
//
//	field_name = serializers.CharField(...)
//	field_name = serializers.IntegerField(read_only=True)
//	nested_field = NestedSerializer()
//	nested_field = NestedSerializer(many=True)
var drfSerializerFieldRe = regexp.MustCompile(`(?m)^[ \t]+([a-zA-Z_]\w*)\s*=\s*(?:serializers\.\w+|[A-Z][A-Za-z0-9_]*Serializer|[A-Z][A-Za-z0-9_]*)\s*\(`)

// drfMetaFieldsListRe captures `fields = ['id', 'name']` or `fields = ("id", "name")` in a Meta class.
var drfMetaFieldsListRe = regexp.MustCompile(`(?m)fields\s*=\s*[\[\(](.*?)[\]\)]`)

// drfLocalVarTypeRe matches `varname = SomeSerializer(...)` to track local variable types.
var drfLocalVarTypeRe = regexp.MustCompile(`(?m)[ \t]+([a-zA-Z_]\w*)\s*=\s*([A-Z][A-Za-z0-9_]*)\s*\(`)

// drfSerializerClassRe matches `varname = self.get_serializer(...)` or `varname = self.serializer_class(...)`.
var drfGetSerializerRe = regexp.MustCompile(`(?m)[ \t]+([a-zA-Z_]\w*)\s*=\s*self\.(?:get_serializer|serializer_class)\s*\(`)

// drfClassSerializerClassRe finds `serializer_class = SomeSerializer` as a class-level attribute.
var drfClassSerializerClassRe = regexp.MustCompile(`(?m)[ \t]+serializer_class\s*=\s*([A-Z][A-Za-z0-9_]*)`)

// drfResolveAndWalk resolves a DRF serializer variable/class to its field names.
// `varName` is the local variable name (e.g. "serializer"), `handlerBody` is the
// method body text. Falls back to class-level `serializer_class` in the full source.
// Returns nil when resolution fails.
func drfResolveAndWalk(src, varName, handlerBody string) []string {
	// 1. Look for local var assignment: `varName = SomeSerializer(...)`
	varRe := regexp.MustCompile(`(?m)[ \t]+` + regexp.QuoteMeta(varName) + `\s*=\s*([A-Z][A-Za-z0-9_]*)\s*\(`)
	if m := varRe.FindStringSubmatch(src); len(m) >= 2 {
		if keys := walkDRFSerializer(src, m[1]); len(keys) > 0 {
			return keys
		}
	}
	// 2. Look for `self.get_serializer(...)` or `self.serializer_class(...)`
	//    in the handler body — resolve via class-level serializer_class.
	if m := drfGetSerializerRe.FindStringSubmatch(handlerBody + src); len(m) >= 2 {
		// Ignore the variable name matched — just resolve via class attribute.
		if m2 := drfClassSerializerClassRe.FindStringSubmatch(src); len(m2) >= 2 {
			if keys := walkDRFSerializer(src, m2[1]); len(keys) > 0 {
				return keys
			}
		}
	}
	// 3. Class-level serializer_class attribute.
	if m := drfClassSerializerClassRe.FindStringSubmatch(src); len(m) >= 2 {
		if keys := walkDRFSerializer(src, m[1]); len(keys) > 0 {
			return keys
		}
	}
	return nil
}

// walkDRFSerializer walks a DRF Serializer class by name and returns its field names.
// Handles:
// - Plain Serializer: field attrs like `name = serializers.CharField(...)`
// - ModelSerializer with `Meta.fields = [...]`: reads the list
// - Nested serializers: `nested = NestedSerializer()` → adds "nested" as a key
func walkDRFSerializer(src, name string) []string {
	// Find the class body.
	re := regexp.MustCompile(`(?m)^([ \t]*)class\s+` + regexp.QuoteMeta(name) + `\b[^\n]*:`)
	loc := re.FindStringSubmatchIndex(src)
	if loc == nil {
		return nil
	}
	classIndent := loc[3] - loc[2]
	headEnd := strings.Index(src[loc[0]:], "\n")
	if headEnd < 0 {
		return nil
	}
	bodyStart := loc[0] + headEnd + 1
	i := bodyStart
	bodyEnd := bodyStart
	for i < len(src) {
		lineEnd := strings.Index(src[i:], "\n")
		if lineEnd < 0 {
			lineEnd = len(src) - i
		}
		line := src[i : i+lineEnd]
		stripped := strings.TrimLeft(line, " \t")
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			i += lineEnd + 1
			bodyEnd = i
			continue
		}
		indent := len(line) - len(stripped)
		if indent <= classIndent {
			break
		}
		i += lineEnd + 1
		bodyEnd = i
	}
	if bodyEnd > len(src) {
		bodyEnd = len(src)
	}
	body := src[bodyStart:bodyEnd]

	// Check if this is a ModelSerializer with Meta.fields.
	if metaKeys := drfReadMetaFields(body); len(metaKeys) > 0 {
		return metaKeys
	}

	// Walk explicit field assignments.
	var keys []string
	for _, m := range drfSerializerFieldRe.FindAllStringSubmatch(body, -1) {
		fname := m[1]
		if strings.HasPrefix(fname, "_") {
			continue
		}
		// Skip class Meta itself.
		if fname == "Meta" {
			continue
		}
		keys = append(keys, fname)
	}
	return keys
}

// drfReadMetaFields looks for a `class Meta:` block inside a Serializer body
// and extracts the `fields` list.
func drfReadMetaFields(body string) []string {
	// Locate `class Meta:` in the body.
	metaRe := regexp.MustCompile(`(?m)^([ \t]*)class\s+Meta\s*:`)
	loc := metaRe.FindStringSubmatchIndex(body)
	if loc == nil {
		return nil
	}
	metaIndent := loc[3] - loc[2]
	headEnd := strings.Index(body[loc[0]:], "\n")
	if headEnd < 0 {
		return nil
	}
	metaBodyStart := loc[0] + headEnd + 1
	j := metaBodyStart
	metaBodyEnd := metaBodyStart
	for j < len(body) {
		lineEnd := strings.Index(body[j:], "\n")
		if lineEnd < 0 {
			lineEnd = len(body) - j
		}
		line := body[j : j+lineEnd]
		stripped := strings.TrimLeft(line, " \t")
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			j += lineEnd + 1
			metaBodyEnd = j
			continue
		}
		indent := len(line) - len(stripped)
		if indent <= metaIndent {
			break
		}
		j += lineEnd + 1
		metaBodyEnd = j
	}
	if metaBodyEnd > len(body) {
		metaBodyEnd = len(body)
	}
	metaBody := body[metaBodyStart:metaBodyEnd]

	// Look for `fields = ['id', 'name', ...]` or `fields = ("id", "name")`.
	// Handle multi-line by collapsing newlines.
	collapsed := strings.ReplaceAll(metaBody, "\n", " ")
	m := drfMetaFieldsListRe.FindStringSubmatch(collapsed)
	if len(m) < 2 {
		return nil
	}
	// Check for `fields = '__all__'`.
	inner := strings.TrimSpace(m[1])
	if strings.Contains(inner, "__all__") {
		return nil // can't statically enumerate __all__
	}
	// Extract quoted field names.
	var keys []string
	for _, km := range regexp.MustCompile(`["']([a-zA-Z_]\w*)["']`).FindAllStringSubmatch(inner, -1) {
		keys = append(keys, km[1])
	}
	return keys
}

// recordStatus appends an observed status code; defaults to 200 when none
// was observed. `isError` is used as a hint when we couldn't read a
// status kwarg but the literal contained an "error"-ish key.
func recordStatus(sh *shape, status int, isError bool) {
	if status > 0 {
		sh.statusCodes = append(sh.statusCodes, status)
		return
	}
	if isError {
		sh.statusCodes = append(sh.statusCodes, 400)
		return
	}
	sh.statusCodes = append(sh.statusCodes, 200)
}

// looksLikeError returns true for argument strings that contain an
// `"error":` / `'error':` / `error:` key — a heuristic to classify
// returns missing an explicit status kwarg.
func looksLikeError(arg string) bool {
	lower := strings.ToLower(arg)
	return strings.Contains(lower, "\"error\"") ||
		strings.Contains(lower, "'error'") ||
		strings.Contains(lower, "error:") ||
		strings.Contains(lower, "\"detail\"") ||
		strings.Contains(lower, "'detail'")
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// atoi is strconv.Atoi with a returning err. We re-export here so the
// per-language extractors don't all need to import strconv directly.
func atoi(s string) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, errParseInt
		}
		n = n*10 + int(c-'0')
	}
	if len(s) == 0 {
		return 0, errParseInt
	}
	return n, nil
}

// errParseInt is the sentinel returned by atoi when parsing fails.
var errParseInt = parseIntErr{}

type parseIntErr struct{}

func (parseIntErr) Error() string { return "parse int" }
