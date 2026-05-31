// Java response-shape extraction for Spring MVC and JAX-RS / Quarkus.
//
// Spring MVC patterns recognized:
//
//   - public DtoClass handler(...)               return type → response_schema
//   - public ResponseEntity<DtoClass> handler()  ditto
//   - return ResponseEntity.ok(new Dto(...))     literal new-instance
//   - return ResponseEntity.status(404).body(...)
//
// JAX-RS / Quarkus patterns:
//
//   - public Response handler() with `@Schema` annotations on the
//     declared return wrapper class
//   - public DtoClass handler()                  return type used directly
//   - return Response.ok(new Dto(...)).build()
//
// For request bodies we honour `@RequestBody Dto body` (Spring) and the
// JAX-RS implicit body parameter (the un-annotated method argument).
package engine

import (
	"regexp"
	"strings"
)

// javaMethodSigRe matches the signature line of a Java method named `handler`.
// We capture the return type so it can be walked when it names a DTO class.
func javaMethodSigRe(handler string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?m)^(?:\s*(?:public|protected|private|static|final|abstract|synchronized)\s+)+` +
			`([A-Za-z_][\w<>,.\s\[\]]*?)\s+` + regexp.QuoteMeta(handler) + `\s*\(([^)]*)\)`,
	)
}

// javaReturnStatusRe captures the explicit status code in
// ResponseEntity.status(404) chains.
var javaReturnStatusRe = regexp.MustCompile(`ResponseEntity\s*\.\s*status\s*\(\s*(?:HttpStatus\.\w+\s*\(?\s*)?(\d{3})`)

// javaResponseStatusConstRe captures HttpStatus.NOT_FOUND-style references.
var javaResponseStatusConstRe = regexp.MustCompile(`HttpStatus\.([A-Z_]+)`)

// extractJavaShape resolves response/request shapes for a single Java
// handler in `src`. `framework` is "spring_mvc" or "jaxrs".
func extractJavaShape(src, handler, framework string) shape {
	var sh shape
	if handler == "" {
		return sh
	}
	// When the Spring composed-route pass emits a Route entity whose
	// name is a path (e.g. "/users/{id}") rather than a method name,
	// resolve the path back to the annotated handler method by
	// scanning the file for the matching @*Mapping annotation. This
	// keeps the shape extractor useful for YAML-only Spring extracts
	// where the AST pass isn't running (#722).
	if strings.HasPrefix(handler, "/") {
		if resolved := resolveSpringHandlerByPath(src, handler); resolved != "" {
			handler = resolved
		} else {
			return sh
		}
	}
	sig := javaMethodSigRe(handler)
	m := sig.FindStringSubmatch(src)
	if m == nil {
		return sh
	}
	retType := strings.TrimSpace(m[1])
	params := m[2]

	// Resolve the return type to a DTO class name, unwrapping common
	// JAX-RS / Spring containers: ResponseEntity<X>, Response, X[], List<X>.
	dto := unwrapJavaReturnType(retType)
	if dto != "" {
		schema := walkJavaClassFields(src, dto)
		if len(schema) > 0 {
			sh.responseSchema = schema
			for k := range schema {
				sh.responseKeys = append(sh.responseKeys, k)
			}
			sh.knownResponse = true
			sh.responseKeysSource = "java_dto"
		}
	}

	// Walk the method body for explicit status codes.
	body := findJavaMethodBody(src, handler)
	if body != "" {
		for _, sm := range javaReturnStatusRe.FindAllStringSubmatch(body, -1) {
			if n, err := atoi(sm[1]); err == nil {
				sh.statusCodes = append(sh.statusCodes, n)
			}
		}
		for _, sm := range javaResponseStatusConstRe.FindAllStringSubmatch(body, -1) {
			if code := javaHTTPStatusFromConst(sm[1]); code > 0 {
				sh.statusCodes = append(sh.statusCodes, code)
			}
		}
		// If the body contains `new Dto(...)` inside ResponseEntity.ok(...) or
		// Response.ok(...) and we don't yet have a schema, try that DTO.
		if sh.responseSchema == nil {
			if nm := regexp.MustCompile(`(?:ResponseEntity|Response)\s*\.\s*(?:ok|status\s*\(\s*\d+\s*\))\s*\(\s*new\s+([A-Z]\w*)\s*\(`).FindStringSubmatch(body); len(nm) >= 2 {
				schema := walkJavaClassFields(src, nm[1])
				if len(schema) > 0 {
					sh.responseSchema = schema
					for k := range schema {
						sh.responseKeys = append(sh.responseKeys, k)
					}
					sh.knownResponse = true
					sh.responseKeysSource = "java_dto"
				}
			}
		}
	}
	// Default status when body has none.
	if sh.knownResponse && len(sh.statusCodes) == 0 {
		sh.statusCodes = append(sh.statusCodes, 200)
	}

	// Request body via @RequestBody (Spring) or bare body argument (JAX-RS).
	if reqDto := extractJavaRequestDTO(params, framework); reqDto != "" {
		schema := walkJavaClassFields(src, reqDto)
		if len(schema) > 0 {
			sh.requestSchema = schema
			for k := range schema {
				sh.requestKeys = append(sh.requestKeys, k)
			}
		}
	}
	return sh
}

// findJavaMethodBody returns the text of the method body for `handler`.
// We rely on the fact that the signature ends with `)` immediately
// followed by (possibly through `throws ...`) an opening `{`.
func findJavaMethodBody(src, handler string) string {
	// Locate the signature.
	sig := javaMethodSigRe(handler)
	loc := sig.FindStringIndex(src)
	if loc == nil {
		return ""
	}
	// Find the `{` after the signature.
	open := strings.Index(src[loc[1]:], "{")
	if open < 0 {
		return ""
	}
	braceIdx := loc[1] + open
	end := findMatchingBracket(src, braceIdx)
	if end < 0 {
		return ""
	}
	return src[braceIdx+1 : end]
}

// unwrapJavaReturnType strips common containers (ResponseEntity<>, Response,
// List<>, [], CompletableFuture<>) to recover the inner DTO class name, or
// returns "" when the type is a primitive / void.
func unwrapJavaReturnType(t string) string {
	t = strings.TrimSpace(t)
	for _, wrap := range []string{"ResponseEntity<", "CompletableFuture<", "Mono<", "Flux<", "Optional<", "List<", "Set<", "Collection<", "Iterable<"} {
		if strings.HasPrefix(t, wrap) {
			t = strings.TrimSuffix(strings.TrimPrefix(t, wrap), ">")
			t = strings.TrimSpace(t)
		}
	}
	t = strings.TrimSuffix(t, "[]")
	// Strip generic params on the inner type itself.
	if i := strings.Index(t, "<"); i >= 0 {
		t = t[:i]
	}
	// Skip primitives + JAX-RS Response (we'll look at its body instead).
	switch t {
	case "void", "Void", "String", "int", "Integer", "long", "Long", "boolean", "Boolean", "double", "Double", "float", "Float", "Object", "Response":
		return ""
	}
	// Keep only identifier characters.
	if id := regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)`).FindStringSubmatch(t); len(id) >= 2 {
		return id[1]
	}
	return ""
}

// javaBodyTypeRe captures the leading type token of a Java parameter once any
// leading annotations have been stripped: `Dto<Generic> name` → `Dto<Generic>`.
var javaBodyTypeRe = regexp.MustCompile(`^([A-Z][\w<>,.\s\[\]]*?)\s+\w+`)

// stripLeadingJavaAnnotations removes a run of leading Java annotations
// (`@Name` optionally followed by a balanced `(...)` argument list) and the
// surrounding whitespace from `s`, returning the remainder. The argument-list
// scan is string-aware so a ')' inside an annotation's string literal
// (e.g. `@Schema(description = "(x)")`) does not truncate the strip. This is the
// parens-in-string-immune replacement for `(?:@\w+(?:\([^)]*\))?\s+)*`.
func stripLeadingJavaAnnotations(s string) string {
	i := 0
	for i < len(s) {
		// skip whitespace
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
			i++
		}
		if i >= len(s) || s[i] != '@' {
			break
		}
		j := i + 1
		for j < len(s) && (isJavaIdentChar(s[j]) || s[j] == '.') {
			j++
		}
		// optional whitespace then arg list
		k := j
		for k < len(s) && (s[k] == ' ' || s[k] == '\t' || s[k] == '\r' || s[k] == '\n') {
			k++
		}
		if k < len(s) && s[k] == '(' {
			closeAt := javaFindMatchingCloseString(s, k)
			if closeAt < 0 {
				break // unbalanced — leave the rest intact
			}
			i = closeAt + 1
		} else {
			i = j
		}
	}
	// trim leading whitespace of the remainder
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	return s[i:]
}

// extractJavaRequestDTO locates a @RequestBody-annotated argument (Spring)
// or the first un-annotated body argument (JAX-RS) and returns the DTO
// type name, or "" when none was found.
func extractJavaRequestDTO(params, framework string) string {
	params = strings.TrimSpace(params)
	if params == "" {
		return ""
	}
	if framework == "spring_mvc" {
		// Find the @RequestBody marker, then strip any further leading
		// annotations (e.g. @Valid, @Schema(description = "(x)")) with a
		// string-aware scan before reading the DTO type. The previous
		// `(?:@\w+(?:\([^)]*\))?\s+)*` skip stopped at the first ')' inside an
		// annotation's string literal, dropping the body shape.
		idx := strings.Index(params, "@RequestBody")
		if idx < 0 {
			return ""
		}
		rest := strings.TrimSpace(params[idx+len("@RequestBody"):])
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "final"))
		rest = stripLeadingJavaAnnotations(rest)
		if m := javaBodyTypeRe.FindStringSubmatch(rest); len(m) >= 2 {
			return unwrapJavaReturnType(m[1])
		}
		return ""
	}
	// JAX-RS: any argument with no @PathParam/@QueryParam/@HeaderParam/@Context
	// annotation is the request body (one per method).
	for _, p := range splitJavaParams(params) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		hasParamAnno := false
		for _, anno := range []string{"@PathParam", "@QueryParam", "@HeaderParam", "@MatrixParam", "@CookieParam", "@FormParam", "@BeanParam", "@Context"} {
			if strings.Contains(p, anno) {
				hasParamAnno = true
				break
			}
		}
		if hasParamAnno {
			continue
		}
		// Strip leading annotations (string-aware, so a paren inside an
		// annotation's string literal does not truncate the strip).
		stripped := stripLeadingJavaAnnotations(p)
		parts := strings.Fields(stripped)
		if len(parts) < 2 {
			continue
		}
		return unwrapJavaReturnType(parts[0])
	}
	return ""
}

// splitJavaParams splits a parameter list on top-level commas (i.e.
// commas not inside generic <...> or annotation (...) brackets).
func splitJavaParams(params string) []string {
	depth := 0
	var out []string
	last := 0
	for i := 0; i < len(params); i++ {
		c := params[i]
		switch c {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, params[last:i])
				last = i + 1
			}
		}
	}
	if last < len(params) {
		out = append(out, params[last:])
	}
	return out
}

// walkJavaClassFields locates `class X` or `record X(...)` in the source
// and returns a map of field name -> type. Records have a different
// shape — their components are declared in the parentheses.
//
// Enhanced to handle:
// - Java records: `public record X(String id, String name)`
// - Lombok @Value / @Data classes: all declared fields (any access modifier)
// - @JsonProperty("alias") annotations: use the alias string as the key name
//
// Field declarations are recovered by the string-aware javaScanClassFields
// walker (below) rather than a monolithic regex, so a ')' inside an
// annotation string literal does not truncate the type/name capture.

// javaJsonPropertyRe captures the string value from @JsonProperty("fieldName").
var javaJsonPropertyRe = regexp.MustCompile(`@JsonProperty\s*\(\s*["']?([A-Za-z_][\w-]*)["']?\s*\)`)

// javaFieldNoAnnoRe matches a single field declaration once its leading
// annotations have already been stripped (string-aware) from the line.
// Group 1 = type, group 2 = field name. Modifiers are consumed before the
// captures so they do not leak into the type.
var javaFieldNoAnnoRe = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|final|transient|volatile)\s+)*([A-Za-z_][\w<>,.\[\]]*?)\s+([a-zA-Z_]\w*)\s*[;=]`)

// javaFieldMatch is one field declaration recovered by javaScanClassFields.
type javaFieldMatch struct {
	annotations string // raw leading-annotation text (for @JsonProperty lookup)
	ftype       string // declared field type
	fname       string // field name
	line        string // the line with annotations stripped (for modifier checks)
}

// javaScanClassFields walks a class/record body statement-by-statement and
// returns the field declarations. Leading annotations (which may span several
// lines) are consumed with a string-aware scan so a ')' inside an annotation
// string literal (e.g. `@Schema(description = "amount in cents (USD)")` above or
// beside a field) does not truncate the field type/name capture. This is the
// parens-in-string-immune replacement for the `(?:@\w+(?:\([^)]*\))?\s+)*`
// annotation prefix that the `javaAnyFieldRe` / `javaFieldRe` FindAll scanners
// used. Nested blocks (`{...}`) and parenthesised groups (method parameter
// lists, initialisers) are skipped string-aware so method bodies are not
// mistaken for fields.
func javaScanClassFields(body string) []javaFieldMatch {
	var out []javaFieldMatch
	i := 0
	annoStart := -1 // start offset of the current accumulated annotation run
	for i < len(body) {
		c := body[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(body) && body[i+1] == '/':
			for i < len(body) && body[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(body) && body[i+1] == '*':
			i += 2
			for i+1 < len(body) && !(body[i] == '*' && body[i+1] == '/') {
				i++
			}
			i += 2
		case c == '@':
			// Start (or continue) an annotation run. Consume name + optional
			// string-aware argument list.
			if annoStart < 0 {
				annoStart = i
			}
			i++
			for i < len(body) && (isJavaIdentChar(body[i]) || body[i] == '.') {
				i++
			}
			j := i
			for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\r' || body[j] == '\n') {
				j++
			}
			if j < len(body) && body[j] == '(' {
				closeAt := javaFindMatchingCloseString(body, j)
				if closeAt < 0 {
					i = len(body)
				} else {
					i = closeAt + 1
				}
			}
		case c == '{':
			// Nested block (method body, initialiser, anonymous class). Skip
			// string-aware to its matching close brace and drop any pending
			// annotation run.
			closeAt := javaFindMatchingBrace(body, i)
			if closeAt < 0 {
				i = len(body)
			} else {
				i = closeAt + 1
			}
			annoStart = -1
		case c == '(':
			// Parenthesised group at statement level (e.g. a method signature
			// whose modifiers/return type we just walked, or a record-style
			// member). Skip it string-aware; it is not a simple field.
			closeAt := javaFindMatchingCloseString(body, i)
			if closeAt < 0 {
				i = len(body)
			} else {
				i = closeAt + 1
			}
		default:
			// Statement start. Capture up to the terminating ';' or the first
			// '=' / '(' / '{' so a field initialiser or a method signature does
			// not bleed across the field regex.
			stmtStart := i
			term := i
			for term < len(body) && body[term] != ';' && body[term] != '=' && body[term] != '(' && body[term] != '{' {
				term++
			}
			stmt := body[stmtStart:term]
			// Reconstruct a candidate `<stmt>;` for the field regex so the `[;=]`
			// terminator anchor is satisfied uniformly.
			if m := javaFieldNoAnnoRe.FindStringSubmatch(stmt + ";"); m != nil {
				annotations := ""
				if annoStart >= 0 {
					annotations = body[annoStart:stmtStart]
				}
				out = append(out, javaFieldMatch{
					annotations: annotations,
					ftype:       m[1],
					fname:       m[2],
					line:        stmt,
				})
			}
			annoStart = -1
			// Advance past this statement: to the ';' (consumed) or, when the
			// terminator was '=' / '(' / '{', skip the remainder of the
			// statement / block string-aware.
			i = javaAdvancePastStatement(body, term)
		}
	}
	return out
}

// javaAdvancePastStatement advances from a terminator offset (a ';', '=', '('
// or '{') to the offset just past the end of the current statement, skipping
// balanced brace/paren groups string-aware. A ';' ends the statement directly.
func javaAdvancePastStatement(body string, term int) int {
	if term >= len(body) {
		return len(body)
	}
	i := term
	for i < len(body) {
		c := body[i]
		switch c {
		case ';':
			return i + 1
		case '{':
			closeAt := javaFindMatchingBrace(body, i)
			if closeAt < 0 {
				return len(body)
			}
			// A '{' may itself terminate a member (e.g. a method body); after
			// its close the statement is done.
			return closeAt + 1
		case '(':
			closeAt := javaFindMatchingCloseString(body, i)
			if closeAt < 0 {
				return len(body)
			}
			i = closeAt + 1
		case '"', '\'':
			closeAt := javaFindStringEnd(body, i)
			if closeAt < 0 {
				return len(body)
			}
			i = closeAt + 1
		default:
			i++
		}
	}
	return i
}

// javaFindMatchingBrace walks from `open` (a '{') to its matching '}',
// honouring string/char literals so a brace inside a string does not affect
// the depth count. Returns -1 when unbalanced.
func javaFindMatchingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			end := javaFindStringEnd(s, i)
			if end < 0 {
				return -1
			}
			i = end
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// javaFindStringEnd walks from `start` (the opening double- or single-quote of
// a string/char literal) to the matching closing quote of the same kind,
// honouring backslash escapes. Returns the closing-quote index, or -1 when
// unterminated.
func javaFindStringEnd(s string, start int) int {
	quote := s[start]
	for i := start + 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == quote {
			return i
		}
	}
	return -1
}

func walkJavaClassFields(src, name string) map[string]string {
	// Record form first: `record X(Type a, Type b)`.
	// Handle multi-line records by finding the opening paren and matching bracket.
	recHeaderRe := regexp.MustCompile(`(?m)^(?:public\s+|private\s+|protected\s+)?record\s+` + regexp.QuoteMeta(name) + `\s*\(`)
	if loc := recHeaderRe.FindStringIndex(src); loc != nil {
		parenStart := loc[1] - 1
		parenEnd := findMatchingBracket(src, parenStart)
		if parenEnd > parenStart {
			params := src[parenStart+1 : parenEnd]
			out := map[string]string{}
			for _, p := range splitJavaParams(params) {
				// Strip leading annotations from each record component
				// (string-aware so a paren inside a @Schema string literal does
				// not truncate the strip and drop the component).
				p = strings.TrimSpace(p)
				p = stripLeadingJavaAnnotations(p)
				parts := strings.Fields(strings.TrimSpace(p))
				if len(parts) >= 2 {
					out[parts[len(parts)-1]] = parts[len(parts)-2]
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	// Class / interface form.
	re := regexp.MustCompile(`(?m)^(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+|static\s+|\s)*(?:class|interface)\s+` + regexp.QuoteMeta(name) + `\b[^{]*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return nil
	}
	// Detect Lombok @Value or @Data annotation in the 200-char window before the class keyword.
	preClass := ""
	if loc[0] > 200 {
		preClass = src[loc[0]-200 : loc[0]]
	} else {
		preClass = src[:loc[0]]
	}
	isLombok := strings.Contains(preClass, "@Value") || strings.Contains(preClass, "@Data")

	braceIdx := loc[1] - 1
	end := findMatchingBracket(src, braceIdx)
	if end < 0 {
		return nil
	}
	body := src[braceIdx+1 : end]
	out := map[string]string{}

	fields := javaScanClassFields(body)

	if isLombok {
		// For Lombok classes, walk all declared fields regardless of access modifier.
		for _, f := range fields {
			fname := f.fname
			ftype := f.ftype
			if strings.Contains(ftype, "(") {
				continue
			}
			// Check for @JsonProperty alias.
			if jp := javaJsonPropertyRe.FindStringSubmatch(f.annotations); len(jp) >= 2 {
				fname = jp[1]
			}
			out[fname] = strings.TrimSpace(ftype)
		}
		return out
	}

	// Plain class: walk public/package-visible fields and @JsonProperty-annotated fields.
	for _, f := range fields {
		ftype := f.ftype
		if strings.Contains(ftype, "(") {
			continue
		}
		// @JsonProperty-annotated field → include regardless of access modifier.
		if jp := javaJsonPropertyRe.FindStringSubmatch(f.annotations); len(jp) >= 2 {
			out[jp[1]] = strings.TrimSpace(ftype)
			continue
		}
		// Public fields are always included.
		if strings.Contains(f.line, "public") {
			out[f.fname] = strings.TrimSpace(ftype)
		}
	}
	// If nothing was found with the new logic, fall back to including all
	// scanned fields (for plain public-field DTOs without @JsonProperty).
	if len(out) == 0 {
		for _, f := range fields {
			if strings.Contains(f.ftype, "(") {
				continue
			}
			out[f.fname] = strings.TrimSpace(f.ftype)
		}
	}
	return out
}

// resolveSpringHandlerByPath finds a Spring controller method annotated
// with @GetMapping / @PostMapping / @RequestMapping(path=...) matching
// the given path string, and returns the method name. Empty when no
// matching annotation is present.
func resolveSpringHandlerByPath(src, path string) string {
	// Try each verb-mapping annotation (and the generic @RequestMapping).
	// The annotation may use a bare string ("/users/{id}") or a
	// path/value kwarg ({"/users/{id}"}). We only require the path
	// string to appear inside the annotation parentheses.
	patterns := []string{
		`@(?:Get|Post|Put|Patch|Delete|Request)Mapping\s*\(\s*[^)]*` + regexp.QuoteMeta(`"`+path+`"`) + `[^)]*\)[\s\S]*?\b(?:public|protected|private)\s+[^;{]+?\s+([a-zA-Z_]\w*)\s*\(`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(src); len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}

// javaHTTPStatusFromConst maps a small set of HttpStatus enum names to
// their numeric codes. Only the common ones are covered; anything else
// returns 0 (no status recorded).
func javaHTTPStatusFromConst(name string) int {
	switch name {
	case "OK":
		return 200
	case "CREATED":
		return 201
	case "ACCEPTED":
		return 202
	case "NO_CONTENT":
		return 204
	case "BAD_REQUEST":
		return 400
	case "UNAUTHORIZED":
		return 401
	case "FORBIDDEN":
		return 403
	case "NOT_FOUND":
		return 404
	case "CONFLICT":
		return 409
	case "UNPROCESSABLE_ENTITY":
		return 422
	case "INTERNAL_SERVER_ERROR":
		return 500
	}
	return 0
}
