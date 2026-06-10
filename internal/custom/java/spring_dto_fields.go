package java

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// spring_dto_fields.go — Java Spring DTO FIELD-as-member indexing (issue #4613),
// generalizing the NestJS / JS-TS model (#4635,
// javascript/validation_schema.go::emitSchemaFieldMembers) to Spring request/
// query/response DTOs.
//
// The existing bean_validation.go emits per-field `SCOPE.Schema`/field members
// ONLY for fields that carry a Bean Validation annotation (@NotNull, @Size, …).
// A Spring DTO whose POJO fields / record components have NO constraints (a
// plain `@RequestBody CreateUserRequest` carrying `String name; int age;`, or a
// `record CreateUser(String name, int age)`) therefore had ZERO field members,
// so request/response FIELD-level diffs were limited. This extractor closes that
// gap by emitting a member for EVERY field/record-component of a DTO-shaped
// class, carrying the SAME property shape as the JS/Python field members:
//
//	field_name, field_type, parent_class, optional, validators, provenance,
//	library="spring"
//
// plus a CONTAINS edge to the owning class. Bean-validation annotations present
// on a field are surfaced as the `validators` set; `@NotNull`/`@NotEmpty`/
// `@NotBlank`/primitive types mark a field required, `Optional<T>`/`@Nullable`
// mark it optional.
//
// To avoid double-emitting members already produced by bean_validation.go (for
// annotated fields), the dedup is by entity Ref (`scope:schema:
// bean_validation_field:<fp>:<Owner>.<field>`), shared with that extractor — the
// dispatcher dedups by Ref so whichever fires first wins and the membership edge
// is identical.

var springDtoFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_mvc": true, "spring-mvc": true, "springmvc": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

var (
	// sdfClassRE matches a (non-controller) class header. Group 1 = class name,
	// group 2 = the byte just past `{`. We capture the brace to delimit the body.
	sdfClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final|static)\s+)*class\s+(\w+)\b[^{;]*\{`)
	// sdfRecordRE matches a Java record declaration and its component list.
	// Group 1 = record name, group 2 = the raw component list.
	sdfRecordRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:final|static)\s+)*record\s+(\w+)\s*\(([\s\S]*?)\)`)
	// sdfControllerAnnRE detects an @…Controller / @Service / @Repository /
	// @Configuration class we should NOT treat as a DTO.
	sdfControllerAnnRE = regexp.MustCompile(
		`@(?:RestController|Controller|Service|Repository|Configuration|Component|ControllerAdvice|RestControllerAdvice)\b`)
	// sdfFieldRE matches a POJO field declaration (one per line). Group 1 = type,
	// group 2 = name. Annotations are recovered from a preceding window.
	sdfFieldRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:private|protected|public)\s+(?:final\s+)?` +
			`([A-Za-z_][\w.]*(?:\s*<[^>]*>)?(?:\[\])?)\s+(\w+)\s*[;=]`)
)

// sdfDtoNameRE recognizes a DTO-shaped class name by the conventional suffixes.
var sdfDtoNameRE = regexp.MustCompile(`(?:Dto|DTO|Request|Response|Payload|Form|Command|Query|Resource|Model|Body|View|Input|Output)$`)

// sdfScalarKind normalizes a Java type to a scalar type, parity with the JS
// schemaScalarKind / Python pyScalarKind maps.
var sdfScalarKind = map[string]string{
	"String": "string", "char": "string", "Character": "string",
	"CharSequence": "string", "UUID": "string",
	"int": "integer", "Integer": "integer", "long": "integer", "Long": "integer",
	"short": "integer", "Short": "integer", "byte": "integer", "Byte": "integer",
	"BigInteger": "integer",
	"float": "number", "Float": "number", "double": "number", "Double": "number",
	"BigDecimal": "number",
	"boolean": "boolean", "Boolean": "boolean",
	"LocalDate": "date", "LocalDateTime": "date", "Instant": "date",
	"Date": "date", "OffsetDateTime": "date", "ZonedDateTime": "date",
	"List": "array", "Set": "array", "Collection": "array", "Iterable": "array",
	"Map": "object", "Object": "any",
}

// normalizeJavaType strips generics / array suffixes and maps to a scalar type.
func normalizeJavaType(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasSuffix(raw, "[]") {
		return "array"
	}
	base := raw
	if i := strings.IndexAny(base, "<["); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[i+1:]
	}
	if base == "Optional" {
		return "object" // Optional<T> wrapper — element handled separately for optionality
	}
	if k, ok := sdfScalarKind[base]; ok {
		return k
	}
	return base
}

// sdfPrimitiveRequired reports whether a Java type is a non-null primitive
// (cannot be null → required by construction).
var sdfPrimitiveRequired = map[string]bool{
	"int": true, "long": true, "double": true, "float": true,
	"boolean": true, "char": true, "byte": true, "short": true,
}

// ExtractSpringDTOFields emits FIELD-as-member sub-entities for Spring DTO
// POJOs and records. Fires for Spring frameworks only.
func ExtractSpringDTOFields(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !springDtoFrameworks[ctx.Framework] {
		return result
	}
	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// ── records: every component is a member ────────────────────────────────
	for _, m := range sdfRecordRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		compBlob := source[m[4]:m[5]]
		line := lineOf(source, m[0])
		for _, c := range parseRecordComponents(compBlob) {
			emitSpringField(&result, seenRefs, seenRels, fp, name, c, line, ctx.Framework)
		}
	}

	// ── POJO classes: skip Spring stereotypes; require DTO-shaped name OR the
	// class is referenced by a @RequestBody/@ModelAttribute in the file ───────
	requestTypes := springRequestBodyTypes(source)
	for _, m := range sdfClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		headerStart := m[0]
		// Skip stereotype-annotated classes (controllers/services/etc.). Only the
		// annotation block immediately preceding THIS class header counts — bound
		// the lookback at the previous `}`/`;`/`{` so a sibling controller's
		// annotations earlier in the file don't spuriously skip a later DTO class.
		annWindow := classAnnotationWindow(source, headerStart)
		if sdfControllerAnnRE.MatchString(annWindow) {
			continue
		}
		isDTO := sdfDtoNameRE.MatchString(name) || requestTypes[name]
		if !isDTO {
			continue
		}
		bodyStart := m[1] - 1 // index of `{`
		body := braceBody(source, bodyStart)
		line := lineOf(source, headerStart)
		for _, fm := range sdfFieldRE.FindAllStringSubmatchIndex(body, -1) {
			rawType := strings.TrimSpace(body[fm[2]:fm[3]])
			fieldName := body[fm[4]:fm[5]]
			// Recover annotations from the preceding window within the body.
			annWin := body[maxInt(0, fm[0]-300):fm[0]]
			c := javaFieldComp{
				name:        fieldName,
				rawType:     rawType,
				annotations: fieldAnnotsInWindow(annWin),
			}
			emitSpringField(&result, seenRefs, seenRels, fp, name, c, line, ctx.Framework)
		}
	}

	return result
}

// javaFieldComp is a captured DTO field / record component.
type javaFieldComp struct {
	name        string
	rawType     string
	annotations []string
}

// emitSpringField appends a `SCOPE.Schema`/field member + CONTAINS edge for one
// DTO field, deduping on the bean_validation-shared ref so annotated fields are
// not double-emitted.
func emitSpringField(result *PatternResult, seenRefs map[string]bool, seenRels map[relKey]bool,
	fp, ownerClass string, c javaFieldComp, line int, framework string) {
	if c.name == "" || ownerClass == "" {
		return
	}
	ref := "scope:schema:bean_validation_field:" + fp + ":" + ownerClass + "." + c.name

	typ := normalizeJavaType(c.rawType)
	optional, required := springFieldOptionality(c.rawType, c.annotations)

	props := map[string]any{
		"kind":         "SCOPE.Schema",
		"subtype":      "field",
		"library":      "spring",
		"pattern_type": "field",
		"field_name":   c.name,
		"field_type":   typ,
		"parent_class": ownerClass,
		"owner_class":  ownerClass,
		"provenance":   "INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP",
		"framework":    framework,
	}
	if len(c.annotations) > 0 {
		props["validators"] = strings.Join(c.annotations, " ")
		props["constraints"] = strings.Join(stripAt(c.annotations), ",")
	}
	if optional {
		props["optional"] = "true"
	}
	if required {
		props["required"] = "true"
	}

	// Java-style signature: `[@Ann ...] Type name`.
	var sb strings.Builder
	for _, a := range c.annotations {
		sb.WriteString(a)
		sb.WriteByte(' ')
	}
	sb.WriteString(typ)
	sb.WriteByte(' ')
	sb.WriteString(c.name)

	emitted := addEntity(result, seenRefs, SecondaryEntity{
		Name: ownerClass + "." + c.name, Kind: "SCOPE.Schema", Subtype: "field",
		SourceFile: fp, LineStart: line, LineEnd: line,
		Provenance: "INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP", Ref: ref,
		Properties: props,
	})
	if emitted {
		addRel(result, seenRels, containsFieldRel(ownerClass, ref, c.name, framework))
	}
}

// springFieldOptionality infers whether a Spring DTO field is optional/required
// from its type and bean-validation annotations.
func springFieldOptionality(rawType string, annots []string) (optional, required bool) {
	base := strings.TrimSpace(rawType)
	if i := strings.IndexAny(base, "<["); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[i+1:]
	}
	for _, a := range annots {
		switch a {
		case "@NotNull", "@NotEmpty", "@NotBlank":
			required = true
		case "@Nullable":
			optional = true
		}
	}
	if base == "Optional" {
		optional = true
	}
	if sdfPrimitiveRequired[base] {
		required = true
	}
	return optional, required
}

// springRequestBodyTypes returns the set of types referenced by a
// @RequestBody / @ModelAttribute parameter in the file — these are request
// DTOs whose field membership we always emit (even without a DTO-suffix name).
var sdfReqBodyParamRE = regexp.MustCompile(
	`@(?:RequestBody|ModelAttribute)\b(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s+([A-Za-z_][\w.]*)(?:\s*<[^>]*>)?\s+\w+`)

func springRequestBodyTypes(source string) map[string]bool {
	out := map[string]bool{}
	for _, m := range sdfReqBodyParamRE.FindAllStringSubmatch(source, -1) {
		t := m[1]
		if i := strings.LastIndex(t, "."); i >= 0 {
			t = t[i+1:]
		}
		out[t] = true
	}
	return out
}

// parseRecordComponents splits a record component list `@Ann Type a, Type b`
// into individual components with their annotations.
func parseRecordComponents(blob string) []javaFieldComp {
	var out []javaFieldComp
	for _, part := range splitTopLevelCommas(blob) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		annots := fieldAnnotsInWindow(part)
		// Remove annotations to isolate `Type name`.
		clean := regexp.MustCompile(`@\w+(?:\([^)]*\))?\s*`).ReplaceAllString(part, "")
		clean = strings.TrimSpace(clean)
		toks := strings.Fields(collapseGenerics(clean))
		if len(toks) < 2 {
			continue
		}
		out = append(out, javaFieldComp{
			name:        toks[len(toks)-1],
			rawType:     strings.Join(toks[:len(toks)-1], " "),
			annotations: annots,
		})
	}
	return out
}

// splitTopLevelCommas splits on commas not nested inside <...> or (...).
func splitTopLevelCommas(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
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

// collapseGenerics removes whitespace inside generic brackets so `List < X >`
// tokenizes as one type token.
func collapseGenerics(s string) string {
	var sb strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		}
		if (r == ' ' || r == '\t') && depth > 0 {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// fieldAnnotsInWindow extracts `@Annotation` heads from a text window.
var sdfAnnHeadRE = regexp.MustCompile(`@(\w+)`)

func fieldAnnotsInWindow(window string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range sdfAnnHeadRE.FindAllStringSubmatch(window, -1) {
		a := "@" + m[1]
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// stripAt removes the leading `@` from annotation heads.
func stripAt(annots []string) []string {
	out := make([]string, 0, len(annots))
	for _, a := range annots {
		out = append(out, strings.TrimPrefix(a, "@"))
	}
	return out
}

// braceBody returns the substring inside the braces beginning at openBraceIdx
// (the index of `{`), reading balanced `{`/`}`.
func braceBody(s string, openBraceIdx int) string {
	if openBraceIdx < 0 || openBraceIdx >= len(s) || s[openBraceIdx] != '{' {
		return ""
	}
	depth := 0
	for i := openBraceIdx; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[openBraceIdx+1 : i]
			}
		}
	}
	return s[openBraceIdx+1:]
}

// classAnnotationWindow returns the text from the nearest preceding `}`, `{`,
// or `;` up to the class header — i.e. only this declaration's own leading
// annotations/modifiers, not unrelated earlier code.
func classAnnotationWindow(source string, headerStart int) string {
	start := headerStart
	for start > 0 {
		c := source[start-1]
		if c == '}' || c == '{' || c == ';' {
			break
		}
		start--
	}
	return source[start:headerStart]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = types.RelationshipKindContains // keep types import anchored
