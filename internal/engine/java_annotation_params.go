// Java JAX-RS / Spring / Quarkus parameter-location extraction (#1936 Phase 1).
//
// The Phase 0 indexer already extracted path + (implicit/explicit) body parameters
// for Java HTTP handlers (see #1908 / #1909). Real REST controllers also take
// query, header, cookie, form, and matrix parameters — these need to be
// surfaced in the endpoint detail Parameters table so the dashboard tells the
// FULL story for an endpoint, not just the URL template + body.
//
// Coverage (Phase 1):
//
//   - JAX-RS:  @PathParam, @QueryParam, @HeaderParam, @CookieParam,
//     @FormParam, @MatrixParam, @BeanParam
//   - Spring:  @PathVariable, @RequestParam (query OR form), @RequestHeader,
//     @CookieValue, @RequestBody, @ModelAttribute (form)
//   - Common:  @DefaultValue, Bean Validation (@NotNull, @Valid,
//     @Min/@Max/@Size/@Pattern/@Email/@NotBlank/@NotEmpty)
//
// Each emitted parameter is serialised as a JSON record and the full list is
// written to the `parameters` property on the http_endpoint_definition entity.
// The dashboard handler in v2_paths.go reads this property and renders one
// row per parameter with the `In` chip set accordingly.
//
// We deliberately keep this layer string/regex-based (line-buffer parser
// matching the existing #682/#683 fix) rather than reaching back into
// tree-sitter — the param fragment we already capture in
// buildMethodEndpointsWithAuth is sufficient for the annotation styles real
// JAX-RS / Spring code uses. Multi-line parameter lists are rare in REST
// controllers; when they occur we still capture the first-line parameters,
// which is the dominant case for the Parameters table.
package engine

import (
	"encoding/json"
	"regexp"
	"strings"
)

// JavaParam is the wire shape of one extracted parameter. The JSON tags are
// the contract consumed by the dashboard (`internal/dashboard/v2_paths.go`
// `handleV2PathDetail` → `parameters[]`). Stable across releases.
type JavaParam struct {
	Name         string   `json:"name"`
	In           string   `json:"in"` // path|query|header|cookie|body|form|matrix
	Type         string   `json:"type"`
	Required     bool     `json:"required"`
	DefaultValue string   `json:"default_value,omitempty"`
	Annotations  []string `json:"annotations,omitempty"`
}

// extractJavaParameters parses a method parameter fragment (everything after
// the opening `(` on the method declaration line, possibly truncated for
// multi-line signatures) and the surrounding verb set, and returns one
// JavaParam per parameter — including the implicit body param when the verb
// allows it (the same heuristic used by inferRequestBodyParam from #1909).
//
// `verbs` is the deduplicated verb list for the endpoint. It controls whether
// an unannotated parameter is treated as the implicit request body.
//
// The returned slice is order-preserving (matches source order) so the
// rendered Parameters table reflects the method signature.
func extractJavaParameters(paramFrag string, verbs []string) []JavaParam {
	// The captured fragment includes everything from the opening '(' to the
	// end of either a single-line signature ("…) { return null; }") or a
	// multi-line one stitched by joinMultiLineParams. Cut at the matching
	// close paren so the method body / brace / semicolon doesn't leak into
	// the last parameter's type or name.
	paramFrag = strings.TrimSpace(trimAtMatchingClose(paramFrag))
	if paramFrag == "" {
		return nil
	}

	hasBodyVerb := false
	for _, v := range verbs {
		if jaxrsVerbsThatHaveBody[strings.ToUpper(v)] {
			hasBodyVerb = true
			break
		}
	}

	chunks := splitTopLevelCommas(paramFrag)
	out := make([]JavaParam, 0, len(chunks))
	implicitBodyTaken := false

	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		// Framework-injected context objects never show up in the Parameters
		// table (HttpServletRequest, UriInfo, Principal, …).
		typGuess, _ := extractParamTypeAndName(chunk)
		if typGuess != "" && isJavaNoisyType(typGuess) {
			continue
		}
		// @Context-annotated parameters are framework injections — skip them.
		if hasAnnotation(chunk, "@Context") {
			continue
		}

		p, ok := classifyJavaParam(chunk, hasBodyVerb, &implicitBodyTaken)
		if !ok {
			continue
		}
		out = append(out, p)
	}
	return out
}

// classifyJavaParam decides which `in` bucket a single parameter belongs to
// and fills out the JavaParam record. Returns ok=false when the parameter
// should be skipped (unparseable / framework injection / would-be implicit
// body when the verb forbids it and no annotation steered it elsewhere).
func classifyJavaParam(chunk string, hasBodyVerb bool, implicitBodyTaken *bool) (JavaParam, bool) {
	annos := collectAnnotations(chunk)
	typ, name := extractParamTypeAndName(chunk)
	if typ == "" {
		return JavaParam{}, false
	}

	// Required flag: any of @NotNull / @NotBlank / @NotEmpty / @Valid lifts
	// `required` to true. Bean-Validation @Valid is a structural-validation
	// marker but in practice it is only applied to required payloads.
	required := false
	for _, a := range annos {
		switch annotationHead(a) {
		case "@NotNull", "@NotBlank", "@NotEmpty", "@Valid":
			required = true
		}
	}

	// Try each location annotation in priority order. @PathParam / @PathVariable
	// outranks everything else because path params are inherently required.
	if v, ok := annotationValue(annos, "@PathParam", "@PathVariable"); ok {
		return paramRecord(name, v, "path", typ, true, defaultValueFor(annos), annos), true
	}
	if v, ok := annotationValue(annos, "@QueryParam", "@RequestParam"); ok {
		return paramRecord(name, v, "query", typ, requiredForRequestParam(annos, required), defaultValueFor(annos), annos), true
	}
	if v, ok := annotationValue(annos, "@HeaderParam", "@RequestHeader"); ok {
		return paramRecord(name, v, "header", typ, requiredForRequestParam(annos, required), defaultValueFor(annos), annos), true
	}
	if v, ok := annotationValue(annos, "@CookieParam", "@CookieValue"); ok {
		return paramRecord(name, v, "cookie", typ, requiredForRequestParam(annos, required), defaultValueFor(annos), annos), true
	}
	if v, ok := annotationValue(annos, "@FormParam", "@ModelAttribute"); ok {
		return paramRecord(name, v, "form", typ, required, defaultValueFor(annos), annos), true
	}
	if v, ok := annotationValue(annos, "@MatrixParam"); ok {
		return paramRecord(name, v, "matrix", typ, required, defaultValueFor(annos), annos), true
	}
	// Explicit Spring @RequestBody → body.
	if hasAnnotation(chunk, "@RequestBody") {
		return paramRecord(name, name, "body", typ, true, "", annos), true
	}
	// @BeanParam (JAX-RS) groups multiple sub-params; we cannot decompose without
	// the target class. Surface as a single composite row tagged "query" — the
	// most common case in real code — with the original annotation preserved so
	// the row is informative.
	if hasAnnotation(chunk, "@BeanParam") {
		return paramRecord(name, name, "query", typ, required, "", annos), true
	}
	// No location annotation. Implicit body candidate when the verb allows it
	// and we haven't already claimed an implicit body for this method.
	if hasBodyVerb && !*implicitBodyTaken {
		*implicitBodyTaken = true
		return paramRecord(name, name, "body", typ, true, "", annos), true
	}
	return JavaParam{}, false
}

// paramRecord composes a JavaParam, using `paramName` (from @QueryParam("x"))
// when present, falling back to the Java variable name. Annotation list is
// emitted as the bare heads (`@QueryParam`, `@DefaultValue`, …) — sufficient
// for the dashboard tooltip and avoids leaking full source fragments.
func paramRecord(varName, paramName, in, typ string, required bool, def string, annos []string) JavaParam {
	if paramName == "" {
		paramName = varName
	}
	heads := make([]string, 0, len(annos))
	for _, a := range annos {
		h := annotationHead(a)
		if h != "" {
			heads = append(heads, h)
		}
	}
	return JavaParam{
		Name:         paramName,
		In:           in,
		Type:         typ,
		Required:     required,
		DefaultValue: def,
		Annotations:  heads,
	}
}

// requiredForRequestParam mirrors Spring's contract: @RequestParam /
// @RequestHeader / @CookieValue parameters are required by default, unless
// `required = false` is set or a `@DefaultValue` / Spring `defaultValue` is
// provided (which implies optional). Bean-Validation @NotNull lifts back to
// required.
func requiredForRequestParam(annos []string, validationRequired bool) bool {
	if validationRequired {
		return true
	}
	for _, a := range annos {
		// Spring style: @RequestParam(required = false) / defaultValue
		if strings.Contains(a, "required") && strings.Contains(a, "false") {
			return false
		}
		if strings.Contains(a, "defaultValue") {
			return false
		}
		// JAX-RS: presence of @DefaultValue implies optional.
		if strings.HasPrefix(strings.TrimSpace(a), "@DefaultValue") {
			return false
		}
	}
	return true
}

// defaultValueFor returns the JAX-RS @DefaultValue("…") string, or the
// Spring defaultValue = "…" arg, or empty when neither is present.
func defaultValueFor(annos []string) string {
	for _, a := range annos {
		if strings.HasPrefix(strings.TrimSpace(a), "@DefaultValue") {
			if v := firstQuotedArg(a); v != "" {
				return v
			}
		}
		if strings.Contains(a, "defaultValue") {
			if v := namedQuotedArg(a, "defaultValue"); v != "" {
				return v
			}
		}
	}
	return ""
}

// namedQuotedArg returns the quoted argument associated with `key = "…"`
// inside an annotation argument list, or empty when the key is absent.
// Robust against the order of keyed args (`value = "x", defaultValue = "y"`).
var javaNamedArgRe = map[string]*regexp.Regexp{}

func namedQuotedArg(anno, key string) string {
	re, ok := javaNamedArgRe[key]
	if !ok {
		re = regexp.MustCompile(key + `\s*=\s*"([^"]*)"`)
		javaNamedArgRe[key] = re
	}
	if m := re.FindStringSubmatch(anno); m != nil {
		return m[1]
	}
	return ""
}

// firstQuotedArg returns the contents of the first "…"-delimited substring
// inside `s`, or empty when there is none.
func firstQuotedArg(s string) string {
	if m := javaStringArgRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// hasAnnotation reports whether the parameter chunk contains a specific
// annotation head (e.g. @RequestBody). Robust against trailing `(` arg lists.
func hasAnnotation(chunk, head string) bool {
	// Word-boundary check so @RequestBodyAdvice doesn't match @RequestBody.
	idx := strings.Index(chunk, head)
	if idx < 0 {
		return false
	}
	end := idx + len(head)
	if end >= len(chunk) {
		return true
	}
	c := chunk[end]
	return c == '(' || c == ' ' || c == '\t' || c == ',' || c == ')'
}

// annotationValue scans the annotation list for any of the candidate heads
// and returns the first quoted argument (the "wire" param name) for the
// first match. When the annotation is bare (no quoted arg) the returned
// string is empty and ok=true — caller falls back to the Java variable name.
func annotationValue(annos []string, candidates ...string) (string, bool) {
	for _, a := range annos {
		head := annotationHead(a)
		for _, c := range candidates {
			if head == c {
				return firstQuotedArg(a), true
			}
		}
	}
	return "", false
}

// annotationHead extracts the leading @Name token from an annotation
// fragment, stripping any argument list.
func annotationHead(a string) string {
	a = strings.TrimSpace(a)
	if !strings.HasPrefix(a, "@") {
		return ""
	}
	end := len(a)
	for i := 1; i < len(a); i++ {
		c := a[i]
		if c == '(' || c == ' ' || c == '\t' {
			end = i
			break
		}
	}
	return a[:end]
}

// javaParamAnnotationRe matches one annotation fragment in a parameter chunk:
// either a marker (`@Foo`) or a parameterised form (`@Foo(args)`). Captures
// the entire match. Arguments may contain nested parentheses with depth 1
// (e.g. `@Pattern(regexp = "\\d+", message = "...")` is fine).
var javaParamAnnotationRe = regexp.MustCompile(`@\w+(?:\s*\([^()]*(?:\([^()]*\)[^()]*)*\))?`)

// collectAnnotations returns every `@Annotation(…)?` substring present in
// the parameter chunk, in source order.
func collectAnnotations(chunk string) []string {
	return javaParamAnnotationRe.FindAllString(chunk, -1)
}

// trimAtMatchingClose returns the prefix of `s` up to (but not including)
// the `)` that closes the parameter list. The opening `(` was already
// consumed before `s` was handed to us, so depth starts at 0 and we walk
// forward until depth would go negative.
func trimAtMatchingClose(s string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return s[:i]
			}
			depth--
		}
	}
	return s
}

// EncodeJavaParameters serialises a slice of JavaParam records into the
// canonical JSON representation written to the `parameters` entity property.
// Empty slice returns "" so the property is omitted entirely.
func EncodeJavaParameters(ps []JavaParam) string {
	if len(ps) == 0 {
		return ""
	}
	b, err := json.Marshal(ps)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeJavaParameters is the inverse of EncodeJavaParameters. Returns nil
// on empty input or malformed JSON (consumers tolerate the absence).
func DecodeJavaParameters(s string) []JavaParam {
	if s == "" {
		return nil
	}
	var out []JavaParam
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
