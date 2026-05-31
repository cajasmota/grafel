package java

import (
	"regexp"
	"strings"
)

// JAX-RS DTO extractor: SCOPE.Schema entities + ACCEPTS_INPUT / RETURNS
// relationships for Jakarta EE and MicroProfile JAX-RS resource classes.
//
// Coverage cells delivered (#2996):
//   - lang.java.framework.jakarta-ee  → Validation.dto_extraction  (partial)
//   - lang.java.framework.microprofile → Validation.dto_extraction  (partial)
//
// Approach: scan for @Path-annotated resource classes, then walk their
// JAX-RS verb methods (@GET/@POST/@PUT/@DELETE/@PATCH) to extract:
//   • Implicit body param type  (POST/PUT/PATCH — first unannotated param)
//   • Return type               (unwrapped from Response<T> / CompletionStage<T>)
//
// The extractor deliberately shares the skip-type list from
// spring_request_response.go so that primitive/framework types are not
// surfaced as DTO entities.

var jaxrsDTOFrameworks = map[string]bool{
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"microprofile": true, "eclipse-microprofile": true,
	// Runtime MicroProfile implementations.
	"open_liberty": true, "payara": true, "helidon": true,
	// Dropwizard uses Jersey (JAX-RS) and @Valid for request validation (#3087).
	"dropwizard": true,
}

var (
	// jaxrsPathAnchorRE locates an @Path annotation NAME (the argument list is
	// then consumed string-aware, so a ')' inside the path template string does
	// not truncate it).
	jaxrsPathAnchorRE = regexp.MustCompile(`@Path\b`)

	// jaxrsClassDeclRE matches a class declaration head once any annotation block
	// has been skipped. Captures the class name.
	jaxrsClassDeclRE = regexp.MustCompile(
		`^(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// jaxrsVerbAnchorRE locates a bare JAX-RS verb annotation. Captures the verb.
	jaxrsVerbAnchorRE = regexp.MustCompile(`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

	// jaxrsMethodDeclRE matches a JAX-RS method declaration head once any
	// intervening annotation block has been skipped string-aware. The visibility
	// modifier is consumed before the captures so it does not leak into the
	// return type. Group 1 = return type, group 2 = method name, group 3 = the
	// parameter fragment up to (and excluding) the closing ')'.
	jaxrsMethodDeclRE = regexp.MustCompile(
		`^(?:public|protected|private)\s+(?:static\s+)?` +
			`(?:<[^>]*>\s*)?([\w<>\[\], ]+?)\s+(\w+)\s*\(([^)]*)`)

	// Response<T> / CompletionStage<T> / Uni<T> / Multi<T> wrapper unwrap.
	jaxrsResponseWrapRE = regexp.MustCompile(
		`(?:Response|CompletionStage|Uni|Multi|CompletableFuture)\s*<\s*([\w<>, ]+?)\s*>`)
)

// jaxrsIsIdentChar reports whether c is valid in a Java identifier.
func jaxrsIsIdentChar(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// jaxrsFindStringEnd walks from `start` (the opening double- or single-quote of
// a string/char literal) to the matching closing quote of the same kind,
// honouring backslash escapes. Returns the closing-quote index, or -1 when
// unterminated.
func jaxrsFindStringEnd(s string, start int) int {
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

// jaxrsFindMatchingClose walks from `open` (a '(') to its matching ')',
// honouring string/char literals so a ')' inside a string does not affect the
// depth count. Returns the matching-paren index, or -1 when unbalanced.
func jaxrsFindMatchingClose(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			end := jaxrsFindStringEnd(s, i)
			if end < 0 {
				return -1
			}
			i = end
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

// jaxrsSkipAnnotationsToDecl returns the offset of the first non-whitespace,
// non-comment, non-annotation token at or after `from`. Annotations (including
// a balanced, string-aware `(...)` argument list — so a ')' inside an
// @Operation(summary = "Get (all)") string does not truncate the skip) and
// whitespace/comments are skipped. Returns -1 if EOF is reached first.
func jaxrsSkipAnnotationsToDecl(s string, from int) int {
	i := from
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
		case c == '@':
			i++
			for i < len(s) && (jaxrsIsIdentChar(s[i]) || s[i] == '.') {
				i++
			}
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
				i++
			}
			if i < len(s) && s[i] == '(' {
				closeAt := jaxrsFindMatchingClose(s, i)
				if closeAt < 0 {
					return -1
				}
				i = closeAt + 1
			}
		default:
			return i
		}
	}
	return -1
}

// jaxrsResourceClass is a @Path-annotated resource class.
type jaxrsResourceClass struct {
	name   string
	offset int // offset of the @Path anchor that introduced the class
}

// jaxrsCollectResourceClasses scans `source` for @Path-annotated resource
// classes. The @Path argument list and any following annotations are consumed
// string-aware, so a ')' inside the path-template string (or inside an
// intervening @Operation/@Tag string) does not truncate the class binding.
func jaxrsCollectResourceClasses(source string) []jaxrsResourceClass {
	var out []jaxrsResourceClass
	for _, loc := range jaxrsPathAnchorRE.FindAllStringIndex(source, -1) {
		// Skip the annotation block (starting at the @Path anchor) string-aware,
		// then require a class declaration head at the resulting offset.
		decl := jaxrsSkipAnnotationsToDecl(source, loc[0])
		if decl < 0 {
			continue
		}
		if m := jaxrsClassDeclRE.FindStringSubmatch(source[decl:]); m != nil {
			name := m[1]
			dup := false
			for _, c := range out {
				if c.name == name {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, jaxrsResourceClass{name: name, offset: loc[0]})
			}
		}
	}
	return out
}

// jaxrsVerbMethod is a JAX-RS verb method binding recovered string-aware.
type jaxrsVerbMethod struct {
	verb       string
	returnType string
	methodName string
	paramFrag  string
	offset     int // offset of the verb anchor
}

// jaxrsCollectVerbMethods scans `source` for JAX-RS verb annotations and binds
// each to its method declaration. The intervening annotation block is skipped
// string-aware, so a ')' inside an @Operation(summary = "Get (all)") string
// does not break the verb→method binding (which previously dropped the route /
// DTO). Returns one entry per verb anchor that resolves to a method head.
func jaxrsCollectVerbMethods(source string) []jaxrsVerbMethod {
	var out []jaxrsVerbMethod
	for _, m := range jaxrsVerbAnchorRE.FindAllStringSubmatchIndex(source, -1) {
		verb := source[m[2]:m[3]]
		// Skip from just after the verb annotation, over any intervening
		// annotations, to the method declaration head.
		decl := jaxrsSkipAnnotationsToDecl(source, m[1])
		if decl < 0 {
			continue
		}
		dm := jaxrsMethodDeclRE.FindStringSubmatch(source[decl:])
		if dm == nil {
			continue
		}
		out = append(out, jaxrsVerbMethod{
			verb:       verb,
			returnType: dm[1],
			methodName: dm[2],
			paramFrag:  dm[3],
			offset:     m[0],
		})
	}
	return out
}

// jaxrsDTOSkipTypes extends srrSkipTypes with JAX-RS-specific noisy types.
var jaxrsDTOSkipTypes = func() map[string]bool {
	m := make(map[string]bool, len(srrSkipTypes)+8)
	for k, v := range srrSkipTypes {
		m[k] = v
	}
	// JAX-RS-specific.
	for _, t := range []string{
		"Response", "ResponseBuilder", "StreamingOutput",
		"URI", "UriInfo", "MultivaluedMap",
	} {
		m[t] = true
	}
	return m
}()

// jaxrsNonBodyAnnotationsLocal lists JAX-RS parameter annotations that mean
// the parameter is NOT the implicit request body.
var jaxrsNonBodyAnnotationsLocal = []string{
	"@PathParam", "@QueryParam", "@HeaderParam", "@FormParam",
	"@CookieParam", "@Context", "@MatrixParam", "@BeanParam",
}

// jaxrsBodyVerbsLocal is the set of HTTP verbs that carry a request body.
var jaxrsBodyVerbsLocal = map[string]bool{
	"POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// inferJaxrsBodyType parses the parameter fragment of a JAX-RS method and
// returns the type of the first parameter that has no binding annotation
// (the implicit request body). Returns "" when the verb does not carry a body
// or when no unbound parameter exists.
func inferJaxrsBodyType(paramFrag string, verb string) string {
	if !jaxrsBodyVerbsLocal[strings.ToUpper(verb)] {
		return ""
	}
	// Strip trailing ')' or '{'.
	paramFrag = strings.TrimRight(strings.TrimSpace(paramFrag), "){")
	// Split on top-level commas (no nesting awareness needed for typical REST signatures).
	for _, chunk := range strings.Split(paramFrag, ",") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		skip := false
		for _, anno := range jaxrsNonBodyAnnotationsLocal {
			if strings.Contains(chunk, anno) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		// Last word before any '<' is the type.
		parts := strings.Fields(chunk)
		for _, p := range parts {
			p = strings.TrimRight(p, "<>[]")
			if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
				return p
			}
		}
	}
	return ""
}

// ExtractJakartaJaxrsDTO runs the JAX-RS DTO extractor for Jakarta EE and
// MicroProfile. It emits SCOPE.Schema entities for request/response DTO
// types and ACCEPTS_INPUT / RETURNS relationships linking them to the
// JAX-RS resource method entity.
func ExtractJakartaJaxrsDTO(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !jaxrsDTOFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath

	// Collect resource class offsets so we can identify the owning class for
	// each method. The scan is string-aware so a ')' inside a @Path template or
	// an intervening annotation string does not break the class binding.
	classes := jaxrsCollectResourceClasses(source)

	// Quick exit: skip files that have no JAX-RS resource class.
	if len(classes) == 0 {
		return result
	}

	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	findOwner := func(offset int) string {
		var owner string
		for _, c := range classes {
			if c.offset <= offset {
				owner = c.name
			}
		}
		return owner
	}

	ensureDTO := func(dtoName string, lineNo int) string {
		ref := "scope:schema:jaxrs_dto:" + fp + ":" + dtoName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: dtoName, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineNo, LineEnd: lineNo,
			Provenance: "INFERRED_FROM_JAXRS_DTO", Ref: ref,
			Properties: map[string]any{"kind": "dto", "framework": ctx.Framework},
		})
		return ref
	}

	unwrap := func(raw string) string {
		if m := jaxrsResponseWrapRE.FindStringSubmatch(raw); m != nil {
			raw = m[1]
		}
		return unwrapReturnType(raw)
	}

	for _, vm := range jaxrsCollectVerbMethods(source) {
		verb := vm.verb
		returnTypeRaw := vm.returnType
		methodName := vm.methodName
		paramFrag := vm.paramFrag
		lineNo := lineOf(source, vm.offset)

		owner := findOwner(vm.offset)
		if owner == "" {
			continue
		}
		endpointRef := "scope:operation:jaxrs_endpoint:" + fp + ":" + owner + "." + methodName

		// ACCEPTS_INPUT — implicit body param for body-eligible verbs.
		bodyType := inferJaxrsBodyType(paramFrag, verb)
		if bodyType != "" && !jaxrsDTOSkipTypes[bodyType] {
			dtoRef := ensureDTO(bodyType, lineNo)
			addRel(&result, seenRels, Relationship{
				SourceRef: endpointRef, TargetRef: dtoRef,
				RelationshipType: "ACCEPTS_INPUT",
				Properties:       map[string]string{"match_source": "jaxrs_implicit_body", "dto_type": bodyType},
			})
		}

		// RETURNS — method return type.
		dtoName := unwrap(returnTypeRaw)
		if dtoName != "" && !jaxrsDTOSkipTypes[dtoName] {
			dtoRef := ensureDTO(dtoName, lineNo)
			addRel(&result, seenRels, Relationship{
				SourceRef: endpointRef, TargetRef: dtoRef,
				RelationshipType: "RETURNS",
				Properties: map[string]string{
					"match_source":    "jaxrs_return_type",
					"return_type_raw": returnTypeRaw,
					"dto_type":        dtoName,
				},
			})
		}
	}

	return result
}
