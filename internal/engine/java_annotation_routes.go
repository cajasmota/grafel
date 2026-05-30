// Java JAX-RS / Spring MVC annotation route composition pass.
//
// Problem: Java REST controllers compose their full HTTP routes from
// class-level + method-level annotations:
//
//	@Path("/products")              // JAX-RS class-level
//	public class ProductsController {
//	    @GET
//	    @Path("/{id}")              // JAX-RS method-level
//	    public Product get(...) {}
//	}
//
//	@RequestMapping("/api/users")   // Spring class-level
//	@RestController
//	public class UserController {
//	    @GetMapping("/{id}")        // Spring method-level
//	    public User get(...) {}
//	}
//
// The old synthesizeJAXRS pass in http_endpoint_synthesis.go had two bugs
// (issues #682 and #683):
//
// Bug #682 — kind/name mismatch: synthesizeJAXRS emitted source_handler
// pointing at kind "Controller" with bare method name (e.g.
// "Controller:listProducts"). The Java AST extractor produces entities with
// kind "SCOPE.Operation" and name "ClassName.methodName". The resolver
// found nothing and dropped all 60 JAX-RS synthetics on fixture-d (Quarkus),
// producing 0 http_endpoint entities and blocking all d↔e cross-repo links.
//
// Bug #683 — regex budget exhausted: jaxrsMethodVerbRe used a {0,3} line
// budget between @VERB and @Path. When multiple annotations intervene
// (e.g. @POST @PermitAll @Path("/login") @Operation), the budget was
// consumed before @Path was reached, producing an incomplete path
// (e.g. "/auth" instead of "/auth/login").
//
// This pass fixes BOTH bugs by using a line-buffer approach instead of a
// single multi-line regex:
//
//  1. Scans every .java file line-by-line.
//  2. Accumulates consecutive annotation lines into a per-method buffer.
//  3. When a method declaration is encountered, searches the entire buffer
//     for verb and @Path annotations — no line budget constraint.
//  4. Emits source_handler = "SCOPE.Operation:ClassName.methodName" so
//     ResolveHTTPEndpointHandlers wires the correct IMPLEMENTS edge.
//
// JAX-RS verb annotations recognised:
//
//	@GET @POST @PUT @DELETE @PATCH @HEAD @OPTIONS
//
// Spring verb annotations recognised:
//
//	@GetMapping @PostMapping @PutMapping @DeleteMapping @PatchMapping
//	@RequestMapping(method = RequestMethod.X)
//
// Refs #682, #683.
package engine

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// JavaAnnotationFileReader returns the source bytes for a repo-relative
// path, or nil if the file is unavailable.
type JavaAnnotationFileReader func(relPath string) []byte

// javaClassDeclRe matches a Java class declaration and captures the class
// name. We do not anchor on visibility because the parsed Java sources we
// emit synthetics for in archigraph use both `public class` and bare
// `class` declarations.
var javaClassDeclRe = regexp.MustCompile(`(?m)^\s*(?:public\s+|abstract\s+|final\s+|static\s+)*class\s+(\w+)`)

// javaPathAnnotationRe matches @Path("value") OR @Path(value = "..."). The
// captured group is the raw path string.
var javaPathAnnotationRe = regexp.MustCompile(`@Path\s*\(\s*(?:value\s*=\s*)?"([^"]*)"\s*\)`)

// javaRequestMappingHeadRe matches the HEAD of a @RequestMapping annotation up
// to and including its opening paren. The argument list is extracted with a
// string-aware balanced-paren scan (javaAnnotationArgsAt) rather than a
// `([^)]*)` capture, so a path-template regex containing string-internal parens
// (e.g. `@RequestMapping("/items/{id:(\\d+)}")`) is not truncated.
var javaRequestMappingHeadRe = regexp.MustCompile(`@RequestMapping\s*\(`)

// javaSpringVerbMappingHeadRe matches the HEAD of a @GetMapping / @PostMapping /
// … annotation: the verb keyword and the opening paren (if any). It deliberately
// does NOT capture the argument list — the argument is extracted with a
// string-aware balanced-paren scan (javaSpringMappingArg) so a path-template
// regex containing parens inside a string literal
// (e.g. `@GetMapping("/items/{id:(\\d+)}")`) is not truncated at the first
// string-internal ')'. The previous `(?:\(([^)]*)\))?` capture stopped at that
// inner ')', silently dropping the path. Group 1 = verb keyword.
var javaSpringVerbMappingHeadRe = regexp.MustCompile(`@(Get|Post|Put|Delete|Patch)Mapping\s*\(?`)

// javaJAXRSVerbRe matches a bare JAX-RS verb annotation. Captures the verb.
var javaJAXRSVerbRe = regexp.MustCompile(`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

// javaStringArgRe extracts the first quoted string from an argument list
// (covers both `"/foo"` and `value = "/foo"`).
var javaStringArgRe = regexp.MustCompile(`"([^"]*)"`)

// javaMethodMethodArgRe captures the `method = RequestMethod.X` keyword in
// a @RequestMapping argument list.
var javaMethodMethodArgRe = regexp.MustCompile(`method\s*=\s*(?:RequestMethod\s*\.\s*)?(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)`)

// javaConsumesRe / javaProducesRe capture content-type metadata.
var javaConsumesRe = regexp.MustCompile(`@Consumes\s*\(([^)]+)\)`)
var javaProducesRe = regexp.MustCompile(`@Produces\s*\(([^)]+)\)`)

// javaMethodDeclRe matches the start of a Java method declaration so we
// can extract the method name following a block of annotations. We scan
// line-by-line in the file walker; this regex matches one method-decl line.
//
// We accept: modifiers, generic return type, method-name, opening paren.
// Group 1 = method name; group 2 = rest of line after the opening paren
// (the parameter fragment, may be partial for multi-line signatures).
var javaMethodDeclRe = regexp.MustCompile(`^\s*(?:public|protected|private|static|final|abstract|synchronized|default|\s)+[\w<>\[\],.\s?]+?\s+(\w+)\s*\((.*)`)

// jaxrsNonBodyAnnotations is the set of JAX-RS / Spring parameter annotations
// whose presence means the parameter is NOT the request body.
// Issue #1909: a JAX-RS method parameter that carries none of these is the
// implicit request body (applies to POST/PUT/PATCH/DELETE).
var jaxrsNonBodyAnnotations = []string{
	"@PathParam", "@QueryParam", "@HeaderParam", "@FormParam",
	"@CookieParam", "@Context", "@MatrixParam", "@BeanParam",
	"@PathVariable", "@RequestParam", "@RequestHeader", // Spring equivalents
}

// jaxrsVerbsThatHaveBody is the set of HTTP verbs where a request body is
// plausible. GET/HEAD/OPTIONS are excluded to avoid false positives.
var jaxrsVerbsThatHaveBody = map[string]bool{
	"POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// inferRequestBodyParam parses the parameter fragment from a method declaration
// and returns the (type, name) of the first parameter that carries no JAX-RS
// binding annotation. Returns ("","") when no body param is found or when the
// verb set does not include a body-eligible verb.
//
// paramFrag is everything after the opening '(' on the method declaration line
// (may not include the closing ')' for multi-line signatures — single-line is
// the dominant case for REST controllers).
func inferRequestBodyParam(paramFrag string, verbs []string) (bodyType, bodyName string) {
	hasBodyVerb := false
	for _, v := range verbs {
		if jaxrsVerbsThatHaveBody[strings.ToUpper(v)] {
			hasBodyVerb = true
			break
		}
	}
	if !hasBodyVerb {
		return "", ""
	}
	// Trim trailing ')' or '{' if present.
	paramFrag = strings.TrimRight(strings.TrimSpace(paramFrag), "){")
	params := splitTopLevelCommas(paramFrag)
	for _, chunk := range params {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		// @RequestBody (Spring) is an EXPLICIT body marker — highest priority.
		if strings.Contains(chunk, "@RequestBody") {
			t, n := extractParamTypeAndName(chunk)
			if t != "" {
				return t, n
			}
		}
		// Skip if any non-body annotation is present.
		skip := false
		for _, anno := range jaxrsNonBodyAnnotations {
			if strings.Contains(chunk, anno) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		// No binding annotation — this parameter is the implicit body.
		t, n := extractParamTypeAndName(chunk)
		if t != "" && !isJavaNoisyType(t) {
			return t, n
		}
	}
	return "", ""
}

// isJavaNoisyType returns true for type names that are framework/context
// objects and should NOT be surfaced as request body types.
func isJavaNoisyType(t string) bool {
	switch t {
	case "void", "Response", "UriInfo", "SecurityContext",
		"HttpServletRequest", "HttpServletResponse",
		"Principal", "AsyncResponse", "SSEEventSink":
		return true
	}
	return false
}

// splitTopLevelCommas splits s on commas that are not nested inside < > or ( ).
func splitTopLevelCommas(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '(':
			depth++
		case '>', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// extractParamTypeAndName strips annotations from a parameter fragment and
// returns (type, name). Returns ("","") when the fragment cannot be parsed.
func extractParamTypeAndName(p string) (typ, name string) {
	// Remove annotation tokens: @Word or @Word(args).
	var sb strings.Builder
	i := 0
	for i < len(p) {
		if p[i] == '@' {
			i++ // skip '@'
			for i < len(p) && (p[i] == '_' || (p[i] >= 'A' && p[i] <= 'Z') ||
				(p[i] >= 'a' && p[i] <= 'z') || (p[i] >= '0' && p[i] <= '9')) {
				i++
			}
			// Skip optional (...)
			if i < len(p) && p[i] == '(' {
				depth := 1
				i++
				for i < len(p) && depth > 0 {
					if p[i] == '(' {
						depth++
					} else if p[i] == ')' {
						depth--
					}
					i++
				}
			}
			sb.WriteByte(' ')
			continue
		}
		sb.WriteByte(p[i])
		i++
	}
	tokens := strings.Fields(sb.String())
	if len(tokens) < 2 {
		return "", ""
	}
	// Last token = variable name, everything before = type.
	// Strip trailing ')' or '{' that may appear when the method param list was
	// captured on the same line as the opening brace (e.g. "body) {").
	name = strings.TrimRight(tokens[len(tokens)-1], "){;")
	// Strip leading "final" modifier from type.
	typeTokens := tokens[:len(tokens)-1]
	if len(typeTokens) > 0 && typeTokens[0] == "final" {
		typeTokens = typeTokens[1:]
	}
	if len(typeTokens) == 0 {
		return "", ""
	}
	typ = strings.TrimSpace(strings.Join(typeTokens, " "))
	// Reject bare modifiers that leaked through.
	switch typ {
	case "final", "synchronized", "volatile", "transient", "static":
		return "", ""
	}
	return typ, name
}

// extractJavaReturnType pulls the return-type token from a Java method
// declaration line and returns it as a simple type name (without generics,
// arrays, or `Response`/`ResponseEntity<…>` framework wrappers unwrapped).
//
// Refs #1935 Phase 1 — used to surface the response DTO type on the
// endpoint entity so the dashboard can render a ShapeTree subtree under
// the Response section.
//
// The strategy is: take the substring before " <methodName>(", strip
// leading modifiers, and return what remains. When the return type is a
// framework wrapper that holds a payload generically (e.g.
// `ResponseEntity<LoginResponse>`, `Response<LoginResponse>`, Mono/Flux,
// CompletableFuture, Uni, Multi), unwrap one level of generics.
//
// Returns "" for void, framework-noisy types, or unparseable lines.
func extractJavaReturnType(line, methodName string) string {
	openIdx := strings.Index(line, methodName+"(")
	if openIdx < 0 {
		return ""
	}
	head := strings.TrimSpace(line[:openIdx])
	if head == "" {
		return ""
	}
	// Strip leading modifiers.
	modifiers := map[string]bool{
		"public": true, "protected": true, "private": true,
		"static": true, "final": true, "abstract": true,
		"synchronized": true, "default": true, "native": true,
	}
	tokens := strings.Fields(head)
	for len(tokens) > 0 && modifiers[tokens[0]] {
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return ""
	}
	ret := strings.Join(tokens, " ")
	// Drop generic-method type parameters: `<T> T foo()` → strip the leading
	// `<T>` (heuristic: starts with `<`, find the matching `>`).
	if strings.HasPrefix(ret, "<") {
		depth := 1
		i := 1
		for i < len(ret) && depth > 0 {
			switch ret[i] {
			case '<':
				depth++
			case '>':
				depth--
			}
			i++
		}
		if depth == 0 && i < len(ret) {
			ret = strings.TrimSpace(ret[i:])
		}
	}
	// Unwrap one level of common framework wrappers (Response<T>,
	// ResponseEntity<T>, CompletableFuture<T>, Mono<T>, Flux<T>, Uni<T>,
	// Multi<T>, Optional<T>).
	for _, w := range []string{
		"ResponseEntity", "CompletableFuture", "Mono", "Flux",
		"Uni", "Multi", "Optional", "Response",
	} {
		prefix := w + "<"
		if strings.HasPrefix(ret, prefix) && strings.HasSuffix(ret, ">") {
			inner := strings.TrimSpace(ret[len(prefix) : len(ret)-1])
			if inner != "" {
				ret = inner
			}
			break
		}
	}
	// Reject framework-noisy wrappers we did NOT unwrap (bare `Response`,
	// `void`, generic raw collections of unknown element type).
	if isJavaNoisyType(ret) || ret == "void" {
		return ""
	}
	// Strip array suffix `[]` so we resolve the element type.
	ret = strings.TrimSuffix(ret, "[]")
	// Strip trailing generics for the simple-name surface: `List<Foo>` →
	// keep as-is (dashboard will display the full token), but `Foo<Bar>`
	// without a known wrapper above should remain so callers can decide.
	return strings.TrimSpace(ret)
}

// ApplyJavaAnnotationRoutesWithContext is the auth-aware variant of
// ApplyJavaAnnotationRoutes. The JavaAuthContext supplies project-wide
// signals (Quarkus security extension + application.properties permission
// blocks) so each emitted endpoint carries a resolved auth_policy
// (#1942 Phase 1).
//
// Existing callers can continue using ApplyJavaAnnotationRoutes which
// forwards an empty context — endpoints from those callsites get the
// "unknown" policy (matching the Phase 0 #1950 muted chip).
func ApplyJavaAnnotationRoutesWithContext(
	javaFiles []string,
	fileReader JavaAnnotationFileReader,
	authCtx JavaAuthContext,
) []types.EntityRecord {
	var out []types.EntityRecord
	seen := map[string]bool{}

	for _, relPath := range javaFiles {
		if !strings.HasSuffix(relPath, ".java") {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		src := string(content)
		// Cheap pre-filter: skip files that have no HTTP annotation.
		if !containsAnyHTTPAnnotation(src) {
			continue
		}

		for _, ep := range extractJavaEndpointsWithAuth(src, relPath, authCtx) {
			if seen[ep.ID] {
				continue
			}
			seen[ep.ID] = true
			out = append(out, ep)
		}
	}
	return out
}

// ApplyJavaAnnotationRoutes scans the supplied Java files for JAX-RS or
// Spring MVC annotation patterns and returns a slice of synthetic
// http_endpoint EntityRecords. Caller appends these to the existing
// entity slice; ResolveHTTPEndpointHandlers wires them to the matching
// SCOPE.Operation handlers.
//
// This is the replacement for the synthesizeJAXRS function in
// http_endpoint_synthesis.go, which had two bugs (see #682, #683):
//  1. Emitted source_handler with wrong kind ("Controller" instead of
//     "SCOPE.Operation") and wrong name format ("methodName" instead of
//     "ClassName.methodName") — causing the resolver to drop all synthetics.
//  2. Used a {0,3} line-budget regex that failed when more than 3 annotations
//     appeared between @VERB and @Path — producing truncated paths.
//
// Both are fixed here: kind is "SCOPE.Operation", name is "ClassName.methodName",
// and the annotation buffer collects ALL annotation lines before a method
// declaration with no line budget.
func ApplyJavaAnnotationRoutes(
	javaFiles []string,
	fileReader JavaAnnotationFileReader,
) []types.EntityRecord {
	return ApplyJavaAnnotationRoutesWithContext(javaFiles, fileReader, JavaAuthContext{})
}

// containsAnyHTTPAnnotation reports whether the source likely contains
// JAX-RS or Spring MVC route annotations.
func containsAnyHTTPAnnotation(src string) bool {
	switch {
	case strings.Contains(src, "@Path("):
		return true
	case strings.Contains(src, "@RequestMapping"):
		return true
	case strings.Contains(src, "@GetMapping"),
		strings.Contains(src, "@PostMapping"),
		strings.Contains(src, "@PutMapping"),
		strings.Contains(src, "@DeleteMapping"),
		strings.Contains(src, "@PatchMapping"):
		return true
	case strings.Contains(src, "@GET"),
		strings.Contains(src, "@POST"),
		strings.Contains(src, "@PUT"),
		strings.Contains(src, "@DELETE"),
		strings.Contains(src, "@PATCH"):
		return true
	}
	return false
}

// classFrame holds per-class state during file scan: the class name (for
// handler-reference composition), the class-level path prefix, and
// class-level content-type metadata that method-level routes inherit.
//
// classAnnoText / classLine are the auth-resolver inputs added in #1942
// Phase 1: the joined annotation block above the class declaration and the
// 1-based line on which `class ClassName` appears. Both are propagated
// to every endpoint emitted under this frame so class-level @Secured /
// @RolesAllowed / @PermitAll inherit correctly.
type classFrame struct {
	name          string
	prefix        string
	framework     string // "jaxrs" or "spring" (best-effort)
	classConsumes string
	classProduces string
	classAnnoText string
	classLine     int
}

// extractJavaEndpoints walks a Java source file and returns the synthetic
// http_endpoint records for every annotated handler method.
//
// Strategy:
//   - Split file into lines.
//   - Maintain a per-class frame. When `class X` is encountered, capture
//     the most recent annotation block above it as the class prefix.
//   - When a method declaration is found, gather the immediately preceding
//     annotation block, parse its verb + optional method-level path, and
//     compose the full route against the current class frame.
//
// Fix for #683: this deliberately uses a lightweight line-oriented parser
// rather than a multi-line regex with a fixed line-count budget. The
// annotation buffer collects ALL annotation lines, regardless of how many
// intervene between @VERB and @Path. The verb and @Path searches operate
// on the full buffer, so @POST @PermitAll @Path("/login") @Operation
// correctly produces "/login" as the method path.
func extractJavaEndpoints(src, relPath string) []types.EntityRecord {
	return extractJavaEndpointsWithAuth(src, relPath, JavaAuthContext{})
}

// extractJavaEndpointsWithAuth is the auth-aware variant. It tracks line
// numbers for class and method declarations so the resolved auth_policy
// source-chain can point at file:line refs (consumed by the dashboard's
// expandable evidence panel — #1949 RefLine).
func extractJavaEndpointsWithAuth(src, relPath string, authCtx JavaAuthContext) []types.EntityRecord {
	lines := strings.Split(src, "\n")

	var out []types.EntityRecord
	var cur classFrame
	// Buffer of consecutive annotation/blank lines immediately above the
	// current code line. Cleared by any non-annotation, non-blank line.
	var annoBuf []string

	flushAnnoBuf := func() []string {
		buf := annoBuf
		annoBuf = nil
		return buf
	}

	for idx, line := range lines {
		lineNo := idx + 1
		trimmed := strings.TrimSpace(line)

		// Track annotation lines (and blank lines, which can occur between
		// annotations in the wild).
		if strings.HasPrefix(trimmed, "@") || trimmed == "" {
			annoBuf = append(annoBuf, trimmed)
			continue
		}
		// Comment lines: ignore but do not reset annoBuf (devs sometimes
		// put a comment between annotations and the declaration).
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Class declaration?
		if m := javaClassDeclRe.FindStringSubmatch(line); m != nil {
			classAnnos := flushAnnoBuf()
			cur = buildClassFrame(m[1], classAnnos)
			cur.classAnnoText = strings.Join(classAnnos, "\n")
			cur.classLine = lineNo
			continue
		}

		// Method declaration?
		if m := javaMethodDeclRe.FindStringSubmatch(line); m != nil {
			methodName := m[1]
			// m[2] is the rest of the line after the opening '(' — used for
			// request body inference (#1909) and full parameter extraction
			// (#1936 Phase 1). For multi-line signatures (one parameter per
			// line, common in JAX-RS code), we splice subsequent lines until
			// we hit the closing ')'.
			paramFrag := ""
			if len(m) > 2 {
				paramFrag = m[2]
			}
			paramFrag = joinMultiLineParams(paramFrag, lines, idx)
			methodAnnos := flushAnnoBuf()
			if cur.name == "" {
				// Method declared before any class header (shouldn't happen
				// in valid Java but harmless to skip).
				continue
			}
			// Refs #1935 Phase 1 — extract the return type from the method
			// declaration so the endpoint can surface a `response_type`
			// property used by the ShapeTree response subtree.
			returnType := extractJavaReturnType(line, methodName)
			eps := buildMethodEndpointsWithAuth(cur, methodName, paramFrag, methodAnnos, lineNo, relPath, authCtx)
			for i := range eps {
				if returnType != "" && eps[i].Properties != nil {
					eps[i].Properties["response_type"] = returnType
				}
			}
			out = append(out, eps...)
			continue
		}

		// Any other code line resets the annotation buffer.
		flushAnnoBuf()
	}
	return out
}

// buildClassFrame parses the annotation block immediately above a class
// declaration and produces the per-class state used when emitting method
// routes.
func buildClassFrame(className string, annos []string) classFrame {
	cf := classFrame{name: className}
	joined := strings.Join(annos, "\n")

	// JAX-RS class-level @Path.
	if m := javaPathAnnotationRe.FindStringSubmatch(joined); m != nil {
		cf.prefix = m[1]
		cf.framework = "jaxrs"
	}
	// Spring class-level @RequestMapping. Path may be the bare quoted
	// arg or `value = "..."`.
	if args := javaAnnotationArgsAt(joined, javaRequestMappingHeadRe); len(args) > 0 {
		if sm := javaStringArgRe.FindStringSubmatch(args[0]); sm != nil {
			cf.prefix = sm[1]
			cf.framework = "spring"
		}
	}
	// Class-level Consumes/Produces — captured as raw substring so we
	// can surface as property when emitting method routes.
	if m := javaConsumesRe.FindStringSubmatch(joined); m != nil {
		cf.classConsumes = strings.TrimSpace(m[1])
	}
	if m := javaProducesRe.FindStringSubmatch(joined); m != nil {
		cf.classProduces = strings.TrimSpace(m[1])
	}
	// Spring @RestController / @Controller signals Spring framework even
	// when @RequestMapping is absent at the class level (some endpoints
	// rely on method-level @GetMapping alone).
	if cf.framework == "" {
		if strings.Contains(joined, "@RestController") || strings.Contains(joined, "@Controller") {
			cf.framework = "spring"
		}
	}
	return cf
}

// buildMethodEndpoints walks the annotation block above a method
// declaration and produces one or more http_endpoint records.
//
// Fix for #682: source_handler is set to "SCOPE.Operation:ClassName.methodName"
// (not "Controller:methodName" as in the old synthesizeJAXRS). This matches
// the entity kind/name emitted by the Java AST extractor, allowing the
// resolver to find and wire the IMPLEMENTS edge.
//
// Fix for #683: because this function receives the entire annotation buffer
// (not a regex-windowed slice), it correctly handles annotation stacks of
// any depth. @Path will be found regardless of how many @PermitAll,
// @Consumes, @Operation, @RateLimited, etc. annotations precede it.
//
// Issue #1909: paramFrag is the rest of the method declaration line after the
// opening '(' — used to infer the JAX-RS request body parameter type.
func buildMethodEndpoints(cf classFrame, methodName, paramFrag string, annos []string, relPath string) []types.EntityRecord {
	return buildMethodEndpointsWithAuth(cf, methodName, paramFrag, annos, 0, relPath, JavaAuthContext{})
}

// buildMethodEndpointsWithAuth is the auth-aware variant. It accepts the
// method's declaration line (1-based) so the resolved auth_policy
// source-chain entries point at file:line refs, and a JavaAuthContext so
// Quarkus framework default + config-driven permissions contribute when
// no explicit annotation covers the handler. Refs #1942 Phase 1.
func buildMethodEndpointsWithAuth(
	cf classFrame,
	methodName, paramFrag string,
	annos []string,
	methodLine int,
	relPath string,
	authCtx JavaAuthContext,
) []types.EntityRecord {
	joined := strings.Join(annos, "\n")

	// Collect method-level paths (may be empty).
	methodPath := ""
	if m := javaPathAnnotationRe.FindStringSubmatch(joined); m != nil {
		methodPath = m[1]
	}

	// Collect verbs.
	var verbs []string
	// JAX-RS bare verbs.
	for _, m := range javaJAXRSVerbRe.FindAllStringSubmatch(joined, -1) {
		verbs = append(verbs, strings.ToUpper(m[1]))
	}
	// Spring specialised mappings (@GetMapping, etc.). The verb is read from
	// the head match; the argument list is extracted with a string-aware
	// balanced scan so a path-template regex with string-internal parens
	// (e.g. `@GetMapping("/items/{id:(\\d+)}")`) is not truncated.
	verbHeads := javaSpringVerbMappingHeadRe.FindAllStringSubmatch(joined, -1)
	verbArgs := javaAnnotationArgsAt(joined, javaSpringVerbMappingHeadRe)
	for i, m := range verbHeads {
		verb := strings.ToUpper(m[1])
		verbs = append(verbs, verb)
		// If the specialised mapping carries an inline path arg, use it.
		if methodPath == "" && i < len(verbArgs) && verbArgs[i] != "" {
			if sm := javaStringArgRe.FindStringSubmatch(verbArgs[i]); sm != nil {
				methodPath = sm[1]
			}
		}
	}
	// Method-level @RequestMapping. Captures the verb from the `method=...`
	// keyword (if any) and the path from the first quoted string. The argument
	// list is extracted with the same string-aware balanced scan.
	for _, args := range javaAnnotationArgsAt(joined, javaRequestMappingHeadRe) {
		// Path.
		if methodPath == "" {
			if sm := javaStringArgRe.FindStringSubmatch(args); sm != nil {
				methodPath = sm[1]
			}
		}
		// Verb(s). If method= is absent the mapping accepts ANY verb.
		methodVerbs := parseRequestMappingMethods(args)
		if len(methodVerbs) == 0 {
			methodVerbs = []string{"ANY"}
		}
		verbs = append(verbs, methodVerbs...)
	}

	if len(verbs) == 0 {
		// No verb annotation found — not a route.
		return nil
	}

	// Method-level Consumes/Produces (override class-level when present).
	methodConsumes := cf.classConsumes
	methodProduces := cf.classProduces
	if m := javaConsumesRe.FindStringSubmatch(joined); m != nil {
		methodConsumes = strings.TrimSpace(m[1])
	}
	if m := javaProducesRe.FindStringSubmatch(joined); m != nil {
		methodProduces = strings.TrimSpace(m[1])
	}

	framework := cf.framework
	if framework == "" {
		// Pure method-level Spring annotation with no class hint.
		framework = "spring"
	}

	canonFW := httproutes.FrameworkJAXRS
	if framework == "spring" {
		canonFW = httproutes.FrameworkSpring
	}

	composed := joinPathFragments(cf.prefix, methodPath)
	canonical := httproutes.Canonicalize(canonFW, composed)
	if canonical == "" {
		return nil
	}

	// Deduplicate verbs in case both @GET and a method=GET were present.
	verbSet := map[string]bool{}
	var uniqueVerbs []string
	for _, v := range verbs {
		if verbSet[v] {
			continue
		}
		verbSet[v] = true
		uniqueVerbs = append(uniqueVerbs, v)
	}

	// Issue #1909 — infer request body type from the method parameter fragment.
	// inferRequestBodyParam needs the full verb list to decide eligibility.
	bodyType, bodyParamName := inferRequestBodyParam(paramFrag, uniqueVerbs)

	// Issue #1936 Phase 1 — extract ALL parameter locations (query / header /
	// cookie / form / matrix / path / body) for the Parameters table. The
	// JSON-encoded slice is attached to every emitted endpoint entity below.
	allParams := extractJavaParameters(paramFrag, uniqueVerbs)
	paramsJSON := EncodeJavaParameters(allParams)

	// Issue #1938 Phase 1 — extract @APIResponse / @ApiResponse per-status-code
	// metadata from the method's annotation block. The JSON-encoded slice is
	// attached to every emitted endpoint entity so the dashboard can render a
	// per-status tab strip with ShapeTree expansion for resolved DTOs.
	apiResponses := extractAPIResponseAnnotations(joined)
	apiResponsesJSON := EncodeAPIResponses(apiResponses)

	// #1942 Phase 1 — resolve auth_policy from class + method + Quarkus context.
	policy := ResolveJavaAuthPolicy(
		joined, methodLine,
		cf.classAnnoText, cf.name, cf.classLine,
		relPath, canonical, authCtx,
	)
	policyJSON := EncodeAuthPolicy(policy)

	var out []types.EntityRecord
	for _, verb := range uniqueVerbs {
		id := httproutes.SyntheticID(verb, canonical)
		props := map[string]string{
			"verb":         verb,
			"path":         canonical,
			"framework":    framework,
			"pattern_type": "java_annotation_routes",
			// Fix for #682: use "SCOPE.Operation:ClassName.methodName" to
			// match the kind/name the Java AST extractor emits.
			"source_handler": "SCOPE.Operation:" + cf.name + "." + methodName,
		}
		// #1942 Phase 1 — serialise the resolved auth policy on every emitted
		// endpoint. The dashboard reads `auth_policy` (JSON) for the source
		// chain and the flat companion fields for cheap filtering.
		if policyJSON != "" {
			props["auth_policy"] = policyJSON
		}
		props["auth_method"] = policy.Method
		props["auth_confidence"] = policy.Confidence
		if policy.Required {
			props["auth_required"] = "true"
		} else if policy.Method != "unknown" {
			props["auth_required"] = "false"
		}
		if len(policy.Roles) > 0 {
			props["auth_roles"] = strings.Join(policy.Roles, ",")
		}
		if methodConsumes != "" {
			props["consumes"] = methodConsumes
		}
		if methodProduces != "" {
			props["produces"] = methodProduces
		}
		// Issue #1909 — emit request_body_type and request_body_param_name when
		// the method carries an inferred or explicit JAX-RS / Spring request body.
		if bodyType != "" && jaxrsVerbsThatHaveBody[strings.ToUpper(verb)] {
			props["request_body_type"] = bodyType
			if bodyParamName != "" {
				props["request_body_param_name"] = bodyParamName
			}
		}
		// Issue #1936 Phase 1 — emit the full parameter list (query / header /
		// cookie / form / matrix / path / body) for the Parameters table. We
		// trim the body row from the per-verb list when the verb cannot carry
		// a body so a GET endpoint with `(@QueryParam String q, FooDTO maybeBody)`
		// does not surface `maybeBody` as a body row.
		if paramsJSON != "" {
			if jaxrsVerbsThatHaveBody[strings.ToUpper(verb)] {
				props["parameters"] = paramsJSON
			} else {
				filtered := make([]JavaParam, 0, len(allParams))
				for _, pp := range allParams {
					if pp.In == "body" || pp.In == "form" {
						continue
					}
					filtered = append(filtered, pp)
				}
				if enc := EncodeJavaParameters(filtered); enc != "" {
					props["parameters"] = enc
				}
			}
		}

		// Issue #1938 Phase 1 — attach the per-status @APIResponse list.
		if apiResponsesJSON != "" {
			props["api_responses"] = apiResponsesJSON
		}

		// #3347 — path-variable names derived from the URL template so the
		// graph records which segments are dynamic (e.g. "/users/{id}" →
		// path_params="id"). Complements the existing `parameters` JSON which
		// carries the same information but is harder to query flat.
		if ppNames := extractRouteTemplatePathParams(canonical); ppNames != "" {
			props["path_params"] = ppNames
		}

		out = append(out, types.EntityRecord{
			ID:                 id,
			Name:               id,
			Kind:               httpEndpointKind,
			SourceFile:         relPath,
			Language:           "java",
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.85,
		})
	}
	return out
}

// joinMultiLineParams splices subsequent source lines onto the captured
// parameter fragment until the depth of unbalanced parentheses returns to
// zero (meaning we have seen the closing `)`). Used for multi-line method
// signatures where each parameter sits on its own line.
//
// The lines slice is the file split on "\n"; `startIdx` is the 0-based index
// of the method-declaration line. Returns the accumulated fragment including
// the closing `)` so existing trimming logic (`TrimRight ")}"`) still works.
func joinMultiLineParams(firstLine string, lines []string, startIdx int) string {
	depth := strings.Count(firstLine, "(") - strings.Count(firstLine, ")")
	// The opening '(' was already consumed by the regex; depth here counts
	// only what's INSIDE the param list. If we have already seen the closing
	// ')' on the same line, depth will be -1 and we're done.
	if depth < 0 {
		return firstLine
	}
	if depth == 0 && strings.Contains(firstLine, ")") {
		return firstLine
	}
	var sb strings.Builder
	sb.WriteString(firstLine)
	for i := startIdx + 1; i < len(lines); i++ {
		next := lines[i]
		sb.WriteByte(' ')
		sb.WriteString(next)
		depth += strings.Count(next, "(") - strings.Count(next, ")")
		if depth < 0 {
			break
		}
	}
	return sb.String()
}

// parseRequestMappingMethods extracts every `method = RequestMethod.X` (or
// `method = {RequestMethod.X, RequestMethod.Y}`) value from a
// @RequestMapping argument list. Returns empty when no method keyword is
// present (caller treats this as ANY).
func parseRequestMappingMethods(args string) []string {
	var out []string
	for _, m := range javaMethodMethodArgRe.FindAllStringSubmatch(args, -1) {
		out = append(out, strings.ToUpper(m[1]))
	}
	return out
}

// joinPathFragments is shared with http_endpoint_synthesis.go (defined
// there). We do not redefine it here.

// ---------------------------------------------------------------------------
// #1938 Phase 1 — @APIResponse per-status-code extraction
// ---------------------------------------------------------------------------

// javaAPIResponseCodeStringRe matches the responseCode = "NNN" form used by
// MicroProfile OpenAPI: @APIResponse(responseCode = "200", ...).
var javaAPIResponseCodeStringRe = regexp.MustCompile(`responseCode\s*=\s*"(\d{3})"`)

// javaAPIResponseCodeIntRe matches the code = NNN (integer) form used by
// JAX-RS 2.x / Swagger: @ApiResponse(code = 404, ...).
// The word-boundary lookahead ensures we don't match "response_code = 404".
var javaAPIResponseCodeIntRe = regexp.MustCompile(`(?:^|[\s,(])code\s*=\s*(\d{3})\b`)

// javaAPIResponseSchemaRe captures `implementation = Foo.class` or
// `response = Foo.class` inside an @APIResponse / @Content(...) block.
var javaAPIResponseSchemaRe = regexp.MustCompile(
	`(?:implementation|response)\s*=\s*([A-Z][A-Za-z0-9_]*)\s*\.class`)

// javaAPIResponseAnnotationRe detects both @APIResponse (MicroProfile) and
// @ApiResponse (Swagger / JAX-RS 2.x). The annotation opens with a '('.
//   - @APIResponse(responseCode = "200", ...)    — MicroProfile OpenAPI
//   - @ApiResponse(code = 404, ...)              — Swagger / Jakarta RS
//
// The case-insensitive prefix `(?i)API` matches both `API` and `Api`.
var javaAPIResponseAnnotationRe = regexp.MustCompile(`@(?i:API)Response\s*\(`)

// APIResponseEntry holds one parsed @APIResponse annotation.
type APIResponseEntry struct {
	StatusCode   int    `json:"status_code"`
	TypeEntityID string `json:"type_entity_id,omitempty"`
	TypeName     string `json:"type_name,omitempty"`
	HasChildren  bool   `json:"has_children,omitempty"`
}

// extractAPIResponseAnnotations scans the joined annotation block for all
// @APIResponse / @ApiResponse annotations and returns the parsed list.
//
// Strategy: split the joined text into per-annotation segments by finding each
// @APIResponse( / @ApiResponse( occurrence, then scan forward to find the
// balanced-paren closing ')'. This handles nested @Content(@Schema(...))
// without requiring a [^)]* inner pattern that would fail on nested parens.
//
// The list is order-preserving (annotation declaration order). A status code
// that appears more than once is collapsed to the first occurrence.
func extractAPIResponseAnnotations(joined string) []APIResponseEntry {
	if !strings.Contains(joined, "@APIResponse") && !strings.Contains(joined, "@ApiResponse") &&
		!strings.Contains(joined, "@Apiresponse") {
		return nil
	}

	// Find all annotation start positions.
	locs := javaAPIResponseAnnotationRe.FindAllStringIndex(joined, -1)
	if len(locs) == 0 {
		return nil
	}

	seen := map[int]bool{}
	var out []APIResponseEntry

	for _, loc := range locs {
		// loc[1] is the position after the opening '(' of the annotation.
		openAt := loc[1] - 1 // position of the '('
		if openAt >= len(joined) {
			continue
		}
		// Find the matching closing ')' walking forward.
		closeAt := findMatchingClose(joined, openAt)
		var block string
		if closeAt > openAt {
			block = joined[openAt : closeAt+1]
		} else {
			// Unbalanced — take the rest of the line.
			lineEnd := strings.IndexByte(joined[loc[0]:], '\n')
			if lineEnd < 0 {
				block = joined[loc[0]:]
			} else {
				block = joined[loc[0] : loc[0]+lineEnd]
			}
		}

		// Extract status code from this annotation block.
		code := 0
		if m := javaAPIResponseCodeStringRe.FindStringSubmatch(block); len(m) >= 2 {
			code = parseDigits(m[1])
		} else if m := javaAPIResponseCodeIntRe.FindStringSubmatch(block); len(m) >= 2 {
			code = parseDigits(m[1])
		}
		if code == 0 || seen[code] {
			continue
		}
		seen[code] = true

		entry := APIResponseEntry{StatusCode: code}
		// Extract DTO type from implementation=Foo.class or response=Foo.class.
		if sm := javaAPIResponseSchemaRe.FindStringSubmatch(block); len(sm) >= 2 {
			entry.TypeName = sm[1]
		}
		out = append(out, entry)
	}
	return out
}

// findMatchingClose walks forward from position `open` (a '(') and returns the
// index of the matching ')'. Returns -1 when the input is unbalanced.
//
// Parens, single- and double-quoted string literals are honoured: a '(' or ')'
// inside a Java string literal (e.g. a Spring path-template regex
// `@GetMapping("/items/{id:(\\d+)}")`) does NOT affect the depth count, so the
// scan returns the annotation's true closing paren rather than stopping at the
// first ')' inside a string. This prevents the path-truncation bug class where
// `[^)]*` stops at a string-internal ')'.
func findMatchingClose(s string, open int) int {
	depth := 0
	var quote byte // 0 = not in a string; otherwise the opening quote char
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' { // skip the escaped char inside a string literal
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// javaAnnotationArgsAt extracts the full, balanced argument list of every
// annotation whose head (up to and INCLUDING the opening '(') is matched by
// `headRe` in `joined`. headRe must end its match at the opening paren. The
// returned slice contains the inner argument text (without the surrounding
// parens) for each occurrence, using a string-aware balanced scan so parens
// inside string literals do not truncate the argument. The optional `verbs`
// out-param style is avoided; callers re-match the verb from headRe separately.
func javaAnnotationArgsAt(joined string, headRe *regexp.Regexp) []string {
	locs := headRe.FindAllStringIndex(joined, -1)
	if len(locs) == 0 {
		return nil
	}
	var out []string
	for _, loc := range locs {
		// The head match ends at (or just after) the opening '('. Locate the
		// '(' at the end of the matched head.
		open := loc[1] - 1
		if open < 0 || open >= len(joined) || joined[open] != '(' {
			// No argument list (e.g. bare `@GetMapping`).
			out = append(out, "")
			continue
		}
		closeAt := findMatchingClose(joined, open)
		if closeAt > open {
			out = append(out, joined[open+1:closeAt])
		} else {
			out = append(out, "")
		}
	}
	return out
}

// parseDigits converts a pure-ASCII decimal string to int. Returns 0 on error.
func parseDigits(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// EncodeAPIResponses serialises the []APIResponseEntry slice to JSON for
// storage in entity Properties["api_responses"]. Returns "" when empty.
func EncodeAPIResponses(entries []APIResponseEntry) string {
	if len(entries) == 0 {
		return ""
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeAPIResponses deserialises the value of Properties["api_responses"].
func DecodeAPIResponses(raw string) []APIResponseEntry {
	if raw == "" {
		return nil
	}
	var out []APIResponseEntry
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// extractRouteTemplatePathParams returns a comma-separated list of path-variable
// names from a URL template string such as "/api/users/{id}/orders/{orderId}".
// Returns "" when the path contains no variables.
// #3347 — surfaces {id}-style path variables as a flat property on http_endpoint
// entities so consumers can query which endpoints use dynamic path segments
// without parsing the full URL template or the parameters JSON.
func extractRouteTemplatePathParams(path string) string {
	var params []string
	inBrace := false
	start := 0
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '{':
			inBrace = true
			start = i + 1
		case '}':
			if inBrace {
				token := path[start:i]
				// Strip optional regex constraint after ':' (e.g. {id:[0-9]+}).
				if j := strings.IndexByte(token, ':'); j >= 0 {
					token = token[:j]
				}
				if token != "" {
					params = append(params, token)
				}
				inBrace = false
			}
		}
	}
	return strings.Join(params, ",")
}
