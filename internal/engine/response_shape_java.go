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

// extractJavaRequestDTO locates a @RequestBody-annotated argument (Spring)
// or the first un-annotated body argument (JAX-RS) and returns the DTO
// type name, or "" when none was found.
func extractJavaRequestDTO(params, framework string) string {
	params = strings.TrimSpace(params)
	if params == "" {
		return ""
	}
	if framework == "spring_mvc" {
		re := regexp.MustCompile(`@RequestBody\s+(?:final\s+)?(?:@\w+(?:\([^)]*\))?\s+)*([A-Z][\w<>,.\s\[\]]*?)\s+\w+`)
		if m := re.FindStringSubmatch(params); len(m) >= 2 {
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
		// Strip leading annotations.
		stripped := regexp.MustCompile(`^(?:@\w+(?:\([^)]*\))?\s+)*`).ReplaceAllString(p, "")
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
var javaFieldRe = regexp.MustCompile(`(?m)^\s*(?:@\w+(?:\([^)]*\))?\s+)*(?:public|private|protected|static|final|\s)+\s*([A-Za-z_][\w<>,.\[\]]*?)\s+([a-zA-Z_]\w*)\s*[;=]`)

func walkJavaClassFields(src, name string) map[string]string {
	// Record form first: `record X(Type a, Type b)`.
	rec := regexp.MustCompile(`(?m)^(?:public\s+|private\s+|protected\s+)?record\s+` + regexp.QuoteMeta(name) + `\s*\(([^)]*)\)`)
	if m := rec.FindStringSubmatch(src); len(m) >= 2 {
		out := map[string]string{}
		for _, p := range splitJavaParams(m[1]) {
			parts := strings.Fields(strings.TrimSpace(p))
			if len(parts) >= 2 {
				out[parts[len(parts)-1]] = parts[len(parts)-2]
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	// Class / interface form.
	re := regexp.MustCompile(`(?m)^(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+|static\s+|\s)*(?:class|interface)\s+` + regexp.QuoteMeta(name) + `\b[^{]*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return nil
	}
	braceIdx := loc[1] - 1
	end := findMatchingBracket(src, braceIdx)
	if end < 0 {
		return nil
	}
	body := src[braceIdx+1 : end]
	out := map[string]string{}
	for _, m := range javaFieldRe.FindAllStringSubmatch(body, -1) {
		fname := m[2]
		ftype := m[1]
		// Skip obvious non-field declarations: method bodies start with `(`.
		// Also skip if the "type" looks like a method-return that wasn't
		// followed by `;` or `=` — defensive.
		if strings.Contains(ftype, "(") {
			continue
		}
		out[fname] = strings.TrimSpace(ftype)
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
