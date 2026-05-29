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
	// @Path class declaration — captures class name.
	jaxrsResourceClassRE = regexp.MustCompile(
		`(?s)@Path\s*\([^)]*\)\s*(?:(?:@\w+(?:\s*\([^)]*\))?\s*)*)` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// JAX-RS verb method — captures verb, return type, method name, and param fragment.
	// The visibility modifier is consumed before the capture group so it does not
	// leak into the return type.
	jaxrsVerbMethodRE = regexp.MustCompile(
		`(?s)@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b` +
			`(?:\s*(?:@\w+(?:\s*\([^)]*\))?\s*))*` +
			`\s*(?:public|protected|private)\s+(?:static\s+)?` +
			`(?:<[^>]*>\s*)?([\w<>\[\], ]+?)\s+(\w+)\s*\(([^)]*)`)

	// Response<T> / CompletionStage<T> / Uni<T> / Multi<T> wrapper unwrap.
	jaxrsResponseWrapRE = regexp.MustCompile(
		`(?:Response|CompletionStage|Uni|Multi|CompletableFuture)\s*<\s*([\w<>, ]+?)\s*>`)
)

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

	// Quick exit: skip files that have no JAX-RS path annotation.
	if !jaxrsResourceClassRE.MatchString(source) {
		return result
	}

	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// Collect resource class offsets so we can identify the owning class for
	// each method.
	type classEntry struct {
		name   string
		offset int
	}
	var classes []classEntry
	for _, m := range jaxrsResourceClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		dup := false
		for _, c := range classes {
			if c.name == name {
				dup = true
				break
			}
		}
		if !dup {
			classes = append(classes, classEntry{name, m[0]})
		}
	}

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

	for _, m := range jaxrsVerbMethodRE.FindAllStringSubmatchIndex(source, -1) {
		verb := source[m[2]:m[3]]
		returnTypeRaw := source[m[4]:m[5]]
		methodName := source[m[6]:m[7]]
		paramFrag := source[m[8]:m[9]]
		lineNo := lineOf(source, m[0])

		owner := findOwner(m[0])
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
