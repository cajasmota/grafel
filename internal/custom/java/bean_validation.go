// Bean Validation extractor for Java — #3100
//
// Delivers four Class B coverage cells for lang.java.validation.bean-validation:
//
//	custom_validator_extraction  missing  → partial
//	  Scans classes implementing ConstraintValidator<A,T> and emits
//	  SCOPE.CustomValidator entities with the annotation type and validated
//	  type captured as properties.
//
//	constraint_extraction        partial  → full
//	  Extends the existing string-level annotation capture by parsing value
//	  bounds from @Size(min,max), @Min(value), @Max(value), and
//	  @Pattern(regexp) — both field-level and parameter-level.
//
//	nested_model_extraction      partial  → full
//	  Detects @Valid-annotated fields and parameters and emits a VALIDATES
//	  edge from the owning DTO class to the nested type, enabling the graph
//	  to represent the full recursive validation scope.
//
//	schema_extraction            partial  → full
//	  Extends constraint annotation recognition to field-level declarations
//	  inside DTO classes (not just handler method parameters), emitting
//	  SCOPE.Schema entities for DTO fields carrying Bean Validation annotations.
//
// Framework gate: fires for any Java source file whose framework key is one of
// the bean_validation family; gracefully no-ops on unrelated frameworks.
//
// Approach: regex + line-buffer (same philosophy as java_annotation_params.go).
// We do NOT reach into tree-sitter. Multi-annotation field declarations are
// captured by scanning an annotation window before each field declaration.
package java

import (
	"regexp"
	"strings"
)

// ── framework gate ─────────────────────────────────────────────────────────

var beanValidationFrameworks = map[string]bool{
	"bean_validation": true, "bean-validation": true, "beanvalidation": true,
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"java_ee": true, "javaee": true,
	"spring_boot": true, "spring": true, "spring-boot": true,
	"quarkus": true, "micronaut": true, "helidon": true,
	"jaxrs": true, "jax-rs": true,
	"microprofile": true, "eclipse-microprofile": true,
}

// ── constraint validation annotation regexes ────────────────────────────────

// bvConstraintValidatorRE matches:
//
//	class Foo implements ConstraintValidator<AnnotType, ValidatedType>
//
// Group 1 = class name, Group 2 = annotation type, Group 3 = validated type
var bvConstraintValidatorRE = regexp.MustCompile(
	`(?s)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+` +
		`(?:extends\s+\w+\s+)?implements\s+[^{]*` +
		`ConstraintValidator\s*<\s*(\w+)\s*,\s*([^>]+?)\s*>`)

// bvConstraintAnnotRE matches @Constraint(validatedBy = …) on a type declaration.
// Group 1 = the annotation class name declared above the @interface.
var bvConstraintAnnotRE = regexp.MustCompile(
	`(?s)@Constraint\s*\([^)]*\)\s*(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public\s+)?@interface\s+(\w+)`)

// bvValidatedClassRE matches @Validated on a class declaration.
// Group 1 = class name.
var bvValidatedClassRE = regexp.MustCompile(
	`(?s)@Validated\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

// bvFieldWithAnnotationsRE matches a Java field declaration that is preceded by
// at least one annotation. We capture:
//
//	Group 1 = everything from the first annotation up to the type+name token
//	Group 2 = type name
//	Group 3 = field name
//
// The (?s) flag is needed because the annotation block may span multiple lines.
var bvFieldWithAnnotationsRE = regexp.MustCompile(
	`(?m)((?:@\w+(?:\s*\([^()]*(?:\([^()]*\)[^()]*)*\))?\s*\n\s*)+)` +
		`(?:(?:private|protected|public)\s+)?` +
		`(?:(?:static|final|transient|volatile)\s+)*` +
		`(\w+(?:\s*<[^>]*>)?)\s+(\w+)\s*[;=]`)

// bvValidFieldRE detects @Valid (or @Validated) on a field or parameter.
// We use a simple line-buffer scan rather than complex lookahead.
var bvValidFieldRE = regexp.MustCompile(`@Valid(?:ated)?\b`)

// bvFieldTypeRE extracts the field/parameter type + name after annotation blocks.
var bvFieldTypeRE = regexp.MustCompile(
	`(?:(?:private|protected|public|static|final|transient|volatile)\s+)*` +
		`(\w+(?:\s*<[^>]*>)?)\s+(\w+)\s*[;=]`)

// bvAnnotationRE matches a single annotation in a source fragment. Mirrors
// the engine-layer javaParamAnnotationRe but is defined locally to avoid a
// cross-package import.
var bvAnnotationRE = regexp.MustCompile(`@\w+(?:\s*\([^()]*(?:\([^()]*\)[^()]*)*\))?`)

// bvAnnotationHead returns the "@Name" prefix of an annotation string,
// stripping any argument list. Local copy of engine.annotationHead.
func bvAnnotationHead(a string) string {
	a = strings.TrimSpace(a)
	if !strings.HasPrefix(a, "@") {
		return ""
	}
	for i := 1; i < len(a); i++ {
		c := a[i]
		if c == '(' || c == ' ' || c == '\t' {
			return a[:i]
		}
	}
	return a
}

// bvConstraintAnnotHeads is the set of standard Bean Validation annotation heads
// we want to capture for field-level schema extraction.
var bvConstraintAnnotHeads = map[string]bool{
	"@NotNull": true, "@NotBlank": true, "@NotEmpty": true,
	"@Size": true, "@Min": true, "@Max": true,
	"@Pattern": true, "@Email": true,
	"@Positive": true, "@PositiveOrZero": true,
	"@Negative": true, "@NegativeOrZero": true,
	"@DecimalMin": true, "@DecimalMax": true,
	"@Digits": true, "@Past": true, "@Future": true,
	"@AssertTrue": true, "@AssertFalse": true,
	"@Valid": true, "@Validated": true,
}

// ── constraint bound extraction ──────────────────────────────────────────────

// bvSizeRE extracts min/max from @Size(min=N, max=M).
var bvSizeRE = regexp.MustCompile(`@Size\s*\([^)]*\)`)

// bvSizeMinRE / bvSizeMaxRE extract individual bound values.
var bvSizeMinRE = regexp.MustCompile(`\bmin\s*=\s*(\d+)`)
var bvSizeMaxRE = regexp.MustCompile(`\bmax\s*=\s*(\d+)`)

// bvMinRE extracts @Min(value) or @Min(value=N).
var bvMinRE = regexp.MustCompile(`@Min\s*\(\s*(?:value\s*=\s*)?(-?\d+)\s*\)`)

// bvMaxRE extracts @Max(value) or @Max(value=N).
var bvMaxRE = regexp.MustCompile(`@Max\s*\(\s*(?:value\s*=\s*)?(-?\d+)\s*\)`)

// bvPatternRE extracts @Pattern(regexp="…").
var bvPatternRE = regexp.MustCompile(`@Pattern\s*\([^)]*regexp\s*=\s*"([^"]*)"`)

// bvCollectionWrapperRE matches Java generic-collection types of the form
// List<T>, Set<T>, Collection<T>, Iterable<T>, Page<T>, Flux<T>, Mono<T>,
// Optional<T>, Queue<T>, Deque<T>, SortedSet<T>.
// Group 1 = collection wrapper name; Group 2 = element type (stripped of trailing '>').
var bvCollectionWrapperRE = regexp.MustCompile(
	`^(List|Set|Collection|Iterable|Page|Flux|Mono|Optional|Queue|Deque|SortedSet)\s*<\s*(.+?)\s*>$`)

// bvUnwrapCollectionType returns the effective type name to use for schema/
// validation purposes, the element type (when it is a collection), and a
// boolean indicating whether the raw type was a collection wrapper.
//
// Examples:
//
//	List<OrderItem>     → ("List", "OrderItem", true)
//	Set<Address>        → ("Set", "Address", true)
//	String              → ("String", "", false)
//	OrderDto            → ("OrderDto", "", false)
func bvUnwrapCollectionType(rawType string) (typeName, elementType string, isCollection bool) {
	rawType = strings.TrimSpace(rawType)
	if m := bvCollectionWrapperRE.FindStringSubmatch(rawType); m != nil {
		// Strip any nested generic from the element (e.g. "Map<String,V>" → "Map")
		elem := strings.TrimSpace(m[2])
		if idx := strings.IndexAny(elem, "<>"); idx >= 0 {
			elem = strings.TrimSpace(elem[:idx])
		}
		return m[1], elem, true
	}
	return rawType, "", false
}

// parseConstraintBounds returns a map of bound properties discovered in a
// source fragment (annotation block or parameter chunk). This is the
// constraint_extraction → full upgrade: we parse the literal bounds rather
// than just recording the annotation head.
func parseConstraintBounds(frag string) map[string]string {
	props := make(map[string]string)

	// @Size bounds
	if m := bvSizeRE.FindString(frag); m != "" {
		if v := bvSizeMinRE.FindStringSubmatch(m); v != nil {
			props["size_min"] = v[1]
		}
		if v := bvSizeMaxRE.FindStringSubmatch(m); v != nil {
			props["size_max"] = v[1]
		}
	}
	// @Min
	if v := bvMinRE.FindStringSubmatch(frag); v != nil {
		props["min_value"] = v[1]
	}
	// @Max
	if v := bvMaxRE.FindStringSubmatch(frag); v != nil {
		props["max_value"] = v[1]
	}
	// @Pattern regexp
	if v := bvPatternRE.FindStringSubmatch(frag); v != nil {
		props["pattern_regexp"] = v[1]
	}
	return props
}

// constraintAnnotsInWindow returns the Bean Validation annotation heads present
// in a source window (e.g. the lines preceding a field declaration).
func constraintAnnotsInWindow(window string) []string {
	raw := bvAnnotationRE.FindAllString(window, -1)
	var out []string
	for _, a := range raw {
		h := bvAnnotationHead(a)
		if bvConstraintAnnotHeads[h] {
			out = append(out, h)
		}
	}
	return out
}

// ── main extractor ──────────────────────────────────────────────────────────

// ExtractBeanValidation is the entry point for the bean validation extractor.
// It fires for any Java source file belonging to a bean-validation–capable
// framework and emits:
//   - SCOPE.CustomValidator entities for ConstraintValidator<A,T> implementors
//   - SCOPE.Schema entities for DTO fields carrying constraint annotations
//   - VALIDATES relationships from DTO classes to nested @Valid types
func ExtractBeanValidation(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !beanValidationFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// ── 1. ConstraintValidator<A,T> implementors (custom_validator_extraction) ──

	for _, m := range bvConstraintValidatorRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		annotationType := source[m[4]:m[5]]
		validatedType := strings.TrimSpace(source[m[6]:m[7]])
		// Strip generic suffix from validated type (e.g. "String>" → "String")
		if idx := strings.IndexAny(validatedType, "<>"); idx >= 0 {
			validatedType = strings.TrimSpace(validatedType[:idx])
		}
		ref := "scope:custom_validator:bean_validation:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.CustomValidator", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_CONSTRAINT_VALIDATOR_IMPL", Ref: ref,
			Properties: map[string]any{
				"annotation_type":  annotationType,
				"validated_type":   validatedType,
				"framework":        ctx.Framework,
				"constraint_class": className,
			},
		})
	}

	// ── 2. @Constraint meta-annotation on custom annotation types ──────────────

	for _, m := range bvConstraintAnnotRE.FindAllStringSubmatchIndex(source, -1) {
		annotName := source[m[2]:m[3]]
		ref := "scope:schema:bean_validation_constraint:" + fp + ":" + annotName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: annotName, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_BEAN_VALIDATION_CONSTRAINT", Ref: ref,
			Properties: map[string]any{
				"subtype":   "custom_constraint",
				"framework": ctx.Framework,
			},
		})
	}

	// ── 3. DTO field-level constraints + bounds (schema_extraction + constraint_extraction) ──

	for _, m := range bvFieldWithAnnotationsRE.FindAllStringSubmatchIndex(source, -1) {
		annotBlock := source[m[2]:m[3]]
		rawTypeName := source[m[4]:m[5]]
		fieldName := source[m[6]:m[7]]

		// #3347 — generic-collection element unwrap:
		// When the field type is List<T>, Set<T>, Collection<T>, Iterable<T>,
		// or Page<T>, the validation target is the element type T, not the
		// collection wrapper. We record both: the raw type as "collection_type"
		// and the element type as the effective typeName for @Valid tracking.
		typeName, elementType, isCollection := bvUnwrapCollectionType(rawTypeName)

		// Skip non-field-level constructs (method return types, etc.)
		if primitiveTypes[typeName] && !strings.Contains(annotBlock, "@Valid") {
			annots := constraintAnnotsInWindow(annotBlock)
			if len(annots) == 0 {
				continue
			}
		}

		annots := constraintAnnotsInWindow(annotBlock)
		if len(annots) == 0 {
			continue
		}

		// Determine owning class
		ownerClass := findEnclosingClass(source, m[0])
		if ownerClass == "" {
			ownerClass = "unknown"
		}

		ref := "scope:schema:bean_validation_field:" + fp + ":" + ownerClass + "." + fieldName
		bounds := parseConstraintBounds(annotBlock)
		props := map[string]any{
			"subtype":     "field",
			"owner_class": ownerClass,
			"constraints": strings.Join(annots, ","),
			"framework":   ctx.Framework,
		}
		for k, v := range bounds {
			props[k] = v
		}
		if isCollection {
			props["collection_type"] = rawTypeName
			props["element_type"] = elementType
		}
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerClass + "." + fieldName, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_BEAN_VALIDATION_FIELD", Ref: ref,
			Properties: props,
		}) && ownerClass != "unknown" {
			// #4367 — CONTAINS membership: the DTO field belongs to its owning
			// (@Validated) class so it is a member, not an orphan. Carrier = the
			// field entity; source resolves to the real class via FromName.
			addRel(&result, seenRels, containsFieldRel(ownerClass, ref, fieldName, ctx.Framework))
		}

		// ── 4. @Valid recursion → VALIDATES edge (nested_model_extraction) ──────

		// #3347 — for @Valid on a List<T> / Set<T> field, the nested type to
		// validate is the element type T, not the collection wrapper. Using
		// elementType (or typeName when not a collection) ensures that the
		// VALIDATES edge points at the correct DTO.
		validTarget := typeName
		if isCollection && elementType != "" {
			validTarget = elementType
		}
		if strings.Contains(annotBlock, "@Valid") && !primitiveTypes[validTarget] {
			// #4367 — REFERENCES from the @Valid field to the nested DTO type, so
			// the field carries an outbound edge to the model it validates (the
			// VALIDATES edge below is class→class; this one is field→class).
			addRel(&result, seenRels, referencesClassRel(ref, validTarget, fieldName, ctx.Framework))
			ownerRef := "scope:class:bean_validation:" + fp + ":" + ownerClass
			nestedRef := findRefForType(validTarget, fp, "bean_validation", &result)
			addRel(&result, seenRels, Relationship{
				SourceRef: ownerRef, TargetRef: nestedRef,
				RelationshipType: "VALIDATES",
				Properties: map[string]string{
					"field":     fieldName,
					"via":       "valid_annotation",
					"framework": ctx.Framework,
				},
			})
		}
	}

	// ── 5. @Validated class-level detection ─────────────────────────────────

	for _, m := range bvValidatedClassRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:component:bean_validation_validated:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_VALIDATED_ANNOTATION", Ref: ref,
			Properties: map[string]any{
				"validated": "true",
				"framework": ctx.Framework,
			},
		})
	}

	return result
}
