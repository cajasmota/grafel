// JavaScript / TypeScript response-shape extraction for Express and NestJS.
//
// Express handler bodies are scanned for:
//
//   - res.json({...})              top-level keys
//   - res.json({key: typed.Type})  type info via the value position
//   - res.status(404).json({...})  status code + error_keys
//   - res.send(JSON.stringify({})) same shape as res.json
//   - return res.json(...)         alias form
//
// NestJS handlers additionally honour:
//
//   - @ApiResponse({ type: DtoClass })
//   - return-type annotation `: Promise<DtoClass>` or `: DtoClass`
//   - @Body() body: DtoClass parameter for request_schema
//
// In both cases, when the value is a class/interface declared in the
// same file, we walk the declaration to populate response_schema /
// request_schema with {field: type}.
package engine

import (
	"regexp"
	"strings"
)

// jsFuncBodyOpenRe locates the opening brace of a JS function or method
// body keyed by name. We support:
//   - function name(...)
//   - const name = (...) =>
//   - name(...) { (object method)
//   - async name(...) {
var jsFuncBodyOpenRes = []*regexp.Regexp{
	regexp.MustCompile(`function\s+%s\s*\([^)]*\)\s*\{`),
	regexp.MustCompile(`\b(?:const|let|var)\s+%s\s*=\s*(?:async\s*)?\([^)]*\)\s*=>\s*\{`),
	regexp.MustCompile(`\b(?:const|let|var)\s+%s\s*=\s*(?:async\s+)?function\s*\([^)]*\)\s*\{`),
	regexp.MustCompile(`(?m)^(?:\s*(?:public|private|protected|static|async)\s+)*%s\s*\([^)]*\)(?::\s*[A-Za-z_][\w<>,.\s\[\]|]+)?\s*\{`),
}

// findJSHandlerBody locates the body of a JS function/method named `name`
// and returns the text between (but not including) the opening `{` and
// its matching `}`. Returns "" when no body can be found.
func findJSHandlerBody(src, name string) string {
	if name == "" {
		return ""
	}
	q := regexp.QuoteMeta(name)
	for _, tmpl := range jsFuncBodyOpenRes {
		// Substitute the name into the pattern. Each template uses %s
		// exactly once; the unrelated `%` in regex syntax is escaped
		// via printf-friendly placeholder substitution.
		patStr := strings.Replace(tmpl.String(), "%s", q, 1)
		re := regexp.MustCompile(patStr)
		loc := re.FindStringIndex(src)
		if loc == nil {
			continue
		}
		// Find the brace that opens at loc[1]-1.
		braceIdx := loc[1] - 1
		if braceIdx >= len(src) || src[braceIdx] != '{' {
			continue
		}
		end := findMatchingBracket(src, braceIdx)
		if end < 0 {
			continue
		}
		return src[braceIdx+1 : end]
	}
	return ""
}

// jsResJsonRe and jsResStatusJsonRe locate Express response calls inside
// a handler body. The chained `.status(code).json(...)` form gets its own
// regex so we can record the explicit code.
var jsResJsonRe = regexp.MustCompile(`\bres\s*\.\s*json\s*\(`)
var jsResStatusJsonRe = regexp.MustCompile(`\bres\s*\.\s*status\s*\(\s*(\d{3})\s*\)\s*\.\s*(?:json|send)\s*\(`)
var jsResSendRe = regexp.MustCompile(`\bres\s*\.\s*send\s*\(`)

func extractJSShape(src, handler, framework string) shape {
	var sh shape
	if handler == "" {
		return sh
	}
	body := findJSHandlerBody(src, handler)
	if body == "" {
		return sh
	}
	// First pass: chained status().json() — explicit error code.
	for _, m := range jsResStatusJsonRe.FindAllStringSubmatchIndex(body, -1) {
		// m[0]:m[1] = full match, m[2]:m[3] = status digits.
		status := mustAtoi(body[m[2]:m[3]])
		// The opening paren is at m[1]-1.
		paren := m[1] - 1
		args := extractArgList(body, paren)
		if len(args) == 0 {
			recordStatus(&sh, status, false)
			continue
		}
		applyJSReturnArg(src, args[0], status, &sh)
		recordStatus(&sh, status, false)
	}
	// Second pass: bare res.json(...).
	for _, m := range jsResJsonRe.FindAllStringIndex(body, -1) {
		// The previous chained pass already covers res.status().json(...);
		// skip when this match is the .json piece of a chain.
		// Heuristic: look 30 chars back for `res.status(`.
		back := m[0] - 30
		if back < 0 {
			back = 0
		}
		if strings.Contains(body[back:m[0]], ".status(") {
			continue
		}
		paren := m[1] - 1
		args := extractArgList(body, paren)
		if len(args) == 0 {
			continue
		}
		applyJSReturnArg(src, args[0], 200, &sh)
		recordStatus(&sh, 200, false)
	}
	// Third pass: res.send(JSON.stringify({...})).
	for _, m := range jsResSendRe.FindAllStringIndex(body, -1) {
		paren := m[1] - 1
		args := extractArgList(body, paren)
		if len(args) == 0 {
			continue
		}
		// Strip JSON.stringify wrapper if present.
		arg := args[0]
		if strings.HasPrefix(arg, "JSON.stringify(") {
			inner := extractArgList(arg, len("JSON.stringify"))
			if len(inner) > 0 {
				arg = inner[0]
			}
		}
		applyJSReturnArg(src, arg, 200, &sh)
		recordStatus(&sh, 200, false)
	}
	// NestJS: walk @ApiResponse({type: Dto}) decorators above the method
	// and @Body() / return-type annotations on the signature.
	if framework == "nestjs" {
		if dto := lookupNestApiResponseType(src, handler); dto != "" {
			schema := walkJSClassFields(src, dto)
			if len(schema) > 0 {
				sh.responseSchema = schema
				for k := range schema {
					sh.responseKeys = append(sh.responseKeys, k)
				}
				sh.knownResponse = true
			}
		}
		if dto := lookupNestReturnType(src, handler); dto != "" && sh.responseSchema == nil {
			schema := walkJSClassFields(src, dto)
			if len(schema) > 0 {
				sh.responseSchema = schema
				for k := range schema {
					sh.responseKeys = append(sh.responseKeys, k)
				}
				sh.knownResponse = true
			}
		}
		if dto := lookupNestBodyType(src, handler); dto != "" {
			schema := walkJSClassFields(src, dto)
			if len(schema) > 0 {
				sh.requestSchema = schema
				for k := range schema {
					sh.requestKeys = append(sh.requestKeys, k)
				}
			}
		}
	}
	return sh
}

// applyJSReturnArg merges a single argument (the object passed to
// res.json / res.send) into `sh`.
func applyJSReturnArg(src, arg string, status int, sh *shape) {
	arg = strings.TrimSpace(arg)
	keys := extractDictKeys(arg)
	if len(keys) > 0 {
		sh.knownResponse = true
		if status >= 400 || looksLikeError(arg) {
			sh.errorKeys = append(sh.errorKeys, keys...)
		} else {
			sh.responseKeys = append(sh.responseKeys, keys...)
		}
		return
	}
	// `new DtoClass(...)` or bare `DtoClass(...)`.
	if m := regexp.MustCompile(`^(?:new\s+)?([A-Z][A-Za-z0-9_]*)\s*\(`).FindStringSubmatch(arg); len(m) >= 2 {
		schema := walkJSClassFields(src, m[1])
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
	sh.dynamicResponse = true
}

// walkJSClassFields locates `class X { ... }` or `interface X { ... }`
// in the source and returns a map of field name -> type token.
// Class-body declarations of the form `name: Type;` and `name: Type =`
// are both supported; method declarations are skipped.
var jsClassFieldRe = regexp.MustCompile(`(?m)^[ \t]+(?:public|private|protected|readonly|\s)*\s*([a-zA-Z_]\w*)(\??):\s*([^;=\n{]+?)(?:;|=|\n)`)

func walkJSClassFields(src, name string) map[string]string {
	// class or interface declaration.
	re := regexp.MustCompile(`(?m)^(?:export\s+)?(?:abstract\s+)?(?:class|interface)\s+` + regexp.QuoteMeta(name) + `\b[^{]*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return nil
	}
	braceIdx := loc[1] - 1
	if braceIdx >= len(src) || src[braceIdx] != '{' {
		return nil
	}
	end := findMatchingBracket(src, braceIdx)
	if end < 0 {
		return nil
	}
	body := src[braceIdx+1 : end]
	out := map[string]string{}
	for _, m := range jsClassFieldRe.FindAllStringSubmatch(body, -1) {
		fname := m[1]
		opt := m[2]
		ftype := strings.TrimSpace(m[3])
		// Skip constructor params / methods (heuristic: opening paren in type).
		if strings.Contains(ftype, "(") {
			continue
		}
		if opt == "?" {
			ftype = ftype + "|null"
		}
		out[fname] = ftype
	}
	return out
}

// lookupNestApiResponseType extracts the `type: Dto` token from an
// @ApiResponse({...}) decorator immediately above the handler.
func lookupNestApiResponseType(src, handler string) string {
	re := regexp.MustCompile(`@ApiResponse\s*\(\s*\{[^}]*\btype\s*:\s*([A-Za-z_][\w.]*)[^}]*\}\s*\)[\s\S]*?\b` + regexp.QuoteMeta(handler) + `\s*\(`)
	if m := re.FindStringSubmatch(src); len(m) >= 2 {
		return stripGeneric(m[1])
	}
	return ""
}

// lookupNestReturnType reads the return-type annotation on a method
// declaration, e.g. `findOne(id): Promise<UserDto>`.
func lookupNestReturnType(src, handler string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(handler) + `\s*\([^)]*\)\s*:\s*([^\{;\n]+)`)
	m := re.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	t := strings.TrimSpace(m[1])
	// Strip leading `async` markers and Promise<>/Observable<> wrappers.
	for _, wrap := range []string{"Promise<", "Observable<", "Awaited<"} {
		if strings.HasPrefix(t, wrap) {
			t = strings.TrimSuffix(strings.TrimPrefix(t, wrap), ">")
		}
	}
	// Strip array suffix.
	t = strings.TrimSuffix(t, "[]")
	if id := regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)`).FindStringSubmatch(t); len(id) >= 2 {
		return id[1]
	}
	return ""
}

// lookupNestBodyType extracts the type of an @Body() parameter on the
// handler signature.
func lookupNestBodyType(src, handler string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(handler) + `\s*\(([^)]*)\)`)
	m := re.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	// Find @Body() name: Type
	body := regexp.MustCompile(`@Body\s*\(\s*\)\s+\w+\s*:\s*([A-Z][A-Za-z0-9_]*)`).FindStringSubmatch(m[1])
	if len(body) >= 2 {
		return body[1]
	}
	return ""
}

func stripGeneric(s string) string {
	if i := strings.Index(s, "<"); i >= 0 {
		return s[:i]
	}
	return s
}

func mustAtoi(s string) int {
	n, err := atoi(s)
	if err != nil {
		return 0
	}
	return n
}
