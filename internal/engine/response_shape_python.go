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
//                                       caller-side type is taken from a
//                                       FastAPI response_model decorator
//                                       (when present) or the function
//                                       return-type annotation.
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
		parsePyReturn(src, expr, &sh)
	}
	return sh
}

// parsePyReturn inspects a single `return <expr>` and updates `sh` in place.
func parsePyReturn(src, expr string, sh *shape) {
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
					applyPyReturnArg(src, args[0], status, sh)
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
			applyPyReturnArg(src, dict, status, sh)
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
	// `return serializer.data` — known DRF idiom; we cannot resolve the
	// serializer from the local return alone, but mark known-dynamic.
	if strings.Contains(expr, ".data") || strings.Contains(expr, ".to_dict()") {
		sh.dynamicResponse = true
		return
	}
	// Free variable — mark dynamic.
	sh.dynamicResponse = true
}

// applyPyReturnArg merges a single argument (typically the body literal
// passed to Response(...)) into `sh`.
func applyPyReturnArg(src, arg string, status int, sh *shape) {
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
	re := regexp.MustCompile(`@[\w.]+\([^)]*\)\s*[\r\n]+(?:\s*@[^\r\n]*[\r\n]+)*\s*(?:async\s+)?def\s+` + regexp.QuoteMeta(handler) + `\s*\(`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return ""
	}
	region := src[loc[0]:loc[1]]
	if m := fastapiResponseModelRe.FindStringSubmatch(region); len(m) >= 2 {
		return m[1]
	}
	return ""
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
