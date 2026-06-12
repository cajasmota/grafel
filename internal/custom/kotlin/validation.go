// Package kotlin — validation extractor for Kotlin source files.
//
// Detects DTO shape and request-validation rules across three styles:
//
//  1. Bean Validation (javax.validation / jakarta.validation):
//     @Valid/@Validated on handler/controller function parameters, and
//     field-level constraints on data-class properties / constructor params:
//     @field:NotNull / @field:NotBlank / @field:Email / @field:Size(min,max) /
//     @field:Min / @field:Max / @field:Pattern(regexp=...) and the bare
//     (non-`field:`) annotation forms. Each constrained property becomes a
//     SCOPE.Pattern (subtype="request_validation") named
//     "validation:rule:<DTO>.<field>:<constraint>" carrying the specific
//     field, constraint kind and parsed bound(s).
//
//  2. konform DSL (io.konform.validation):
//     Validation<Foo> { Foo::bar { minLength(1); pattern("...") } } — each
//     property block's constraints become per-field request_validation rules
//     named "validation:rule:<Foo>.<bar>:<constraint>" with bound(s).
//
//  3. Valiktor / Arrow-style contract objects:
//     validate<T>(x) { ... } DSL blocks — a coarse request_validation pattern
//     plus the validated DTO schema.
//
// DTO extraction additionally walks every Kotlin `data class` and emits one
// SCOPE.Schema (subtype="dto") per class plus, for each property, the property
// name, declared type, nullability (`String?`), kotlinx-serialization /
// Jackson wire-name overrides (@SerialName / @JsonProperty) and any default —
// recorded as properties of the DTO schema entity (props_json) and asserted by
// value in tests.
//
// These entities cause request_validation and dto_extraction coverage cells to
// light up for the Kotlin backend framework records:
// spring-boot, ktor, micronaut, quarkus, http4k, javalin.
package kotlin

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_validation", &kotlinValidationExtractor{})
}

type kotlinValidationExtractor struct{}

func (e *kotlinValidationExtractor) Language() string { return "custom_kotlin_validation" }

// ---------------------------------------------------------------------------
// Regexes
// ---------------------------------------------------------------------------

var (
	// @Valid or @Validated on a function parameter (handler/controller style).
	reValidAnnotationParam = regexp.MustCompile(`@(?:Valid|Validated)\b`)

	// Field-level Bean Validation annotation, optionally with a Kotlin
	// use-site target (`@field:`, `@get:`, `@param:`, `@property:`) and an
	// optional argument list. Submatch 1 = constraint name, submatch 2 = raw
	// argument text inside parens (may be empty).
	reBeanConstraint = regexp.MustCompile(
		`@(?:field:|get:|param:|property:|set:)?` +
			`(NotNull|NotBlank|NotEmpty|Size|Pattern|Email|Min|Max|Positive|PositiveOrZero|Negative|NegativeOrZero|DecimalMin|DecimalMax|Digits|Future|Past|FutureOrPresent|PastOrPresent|AssertTrue|AssertFalse)` +
			`\b(?:\s*\(([^)]*)\))?`,
	)

	// data class FooRequest(...) or class FooDto(...). Submatch 1 = class name.
	reDataClass = regexp.MustCompile(
		`(?m)^\s*(?:@\w+(?:\([^)]*\))?\s*)*(?:data\s+class|class)\s+([A-Z][A-Za-z0-9_]*)\s*[(:@]`,
	)

	// A constructor/body property declaration carrying optional annotations.
	// Captures the leading annotation block (g1), val/var (g2), property name
	// (g3), declared type up to a delimiter (g4) and any default after `=` (g5).
	reDataProperty = regexp.MustCompile(
		`(?m)((?:@(?:field:|get:|param:|property:|set:)?\w+(?:\s*\([^)]*\))?\s*)*)` +
			`\b(val|var)\s+([a-z_][A-Za-z0-9_]*)\s*:\s*` +
			// type: ident with one optional level of generic args, optional `?`.
			`([A-Za-z_][A-Za-z0-9_.]*(?:\s*<[^<>]*>)?\??)` +
			`(?:\s*=\s*([^,)\n]+))?`,
	)

	// kotlinx.serialization @SerialName("wire") wire-name override.
	reSerialName = regexp.MustCompile(`@SerialName\s*\(\s*"([^"]+)"\s*\)`)
	// Jackson @JsonProperty("wire") wire-name override.
	reJsonProperty = regexp.MustCompile(`@JsonProperty\s*\(\s*"([^"]+)"\s*\)`)

	// Valiktor / Arrow-style: validate(foo) { ... } or Validator { ... }.
	reValidationContract = regexp.MustCompile(
		`\b(?:validate|Validator)\s*(?:<\s*([A-Z][A-Za-z0-9_]*)\s*>)?\s*\(`,
	)

	// @Valid (any Kotlin use-site target) on a property — the cascade marker
	// that drives nested-model recursion. Matches @Valid, @field:Valid,
	// @get:Valid, @property:Valid, @param:Valid.
	reFieldValid = regexp.MustCompile(`@(?:field:|get:|param:|property:|set:)?Valid\b`)

	// Element-position @Valid inside a generic, e.g. List<@Valid AddressDto> or
	// Map<String, @Valid AddressDto>. Submatch 1 = the annotated element type.
	reElementValid = regexp.MustCompile(`@Valid\s+([A-Z][A-Za-z0-9_.]*)`)

	// konform: Validation<Foo> { ... } — submatch 1 = validated type.
	reKonformHead = regexp.MustCompile(`\bValidation\s*<\s*([A-Z][A-Za-z0-9_]*)\s*>\s*\{`)
	// konform property block:  Foo::bar { ... }  — submatch 1 = receiver type,
	// submatch 2 = property name, submatch 3 = open-brace offset anchor.
	reKonformProperty = regexp.MustCompile(`([A-Z][A-Za-z0-9_]*)\s*::\s*([a-z_][A-Za-z0-9_]*)\s*(?:\.\s*has)?\s*\{`)
	// konform constraint call inside a property block:  minLength(1) / pattern("..").
	reKonformConstraint = regexp.MustCompile(
		`\b(minLength|maxLength|minItems|maxItems|minimum|maximum|exclusiveMinimum|exclusiveMaximum|pattern|enum|const|notBlank|required|uniqueItems|multipleOf|minimumValue|maximumValue)\b(?:\s*\(\s*([^)]*?)\s*\))?`,
	)
)

// kotlinPrimitives are Kotlin built-in types that should not be emitted as schema entities.
var kotlinPrimitives = map[string]bool{
	"String": true, "Int": true, "Long": true, "Double": true, "Float": true,
	"Boolean": true, "Char": true, "Byte": true, "Short": true, "Unit": true,
	"Any": true, "Nothing": true, "Number": true, "List": true, "Map": true,
	"Set": true, "Collection": true, "Iterable": true, "Sequence": true,
	"Array": true, "Pair": true, "Triple": true,
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *kotlinValidationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_validation_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "kotlin" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Index data-class heads by source offset so each property/constraint can
	// be attributed to its enclosing DTO.
	classHeads := indexDataClasses(src)
	classAt := func(off int) string {
		name := ""
		for _, h := range classHeads {
			if h.off <= off {
				name = h.name
			} else {
				break
			}
		}
		return name
	}

	// -----------------------------------------------------------------------
	// 1. @Valid / @Validated on handler parameters → request_validation
	// -----------------------------------------------------------------------
	for _, m := range reValidAnnotationParam.FindAllStringIndex(src, -1) {
		attrText := src[m[0]:m[1]]
		line := lineOf(src, m[0])
		name := "validation:handler:" + attrText + ":" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "kotlin", line)
		setProps(&ent,
			"validation_framework", "BeanValidation",
			"validation_kind", "handler_marker",
			"annotation", attrText,
			"provenance", "INFERRED_FROM_VALID_ANNOTATION",
		)
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 2. Field-level bean-validation constraints → one rule per field+constraint
	// -----------------------------------------------------------------------
	for _, m := range reBeanConstraint.FindAllStringSubmatchIndex(src, -1) {
		constraint := src[m[2]:m[3]]
		argText := ""
		if m[4] >= 0 {
			argText = src[m[4]:m[5]]
		}
		line := lineOf(src, m[0])
		dto := classAt(m[0])
		field := beanFieldName(src, m[1])
		fieldRef := dto
		if field != "" {
			fieldRef = dto + "." + field
		}
		name := "validation:rule:" + fieldRef + ":" + constraint
		ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "kotlin", line)
		setProps(&ent,
			"validation_framework", "BeanValidation",
			"validation_kind", "rule",
			"dto", dto,
			"field_name", field,
			"constraint", constraint,
			"provenance", "INFERRED_FROM_FIELD_ANNOTATION",
		)
		for k, v := range beanConstraintBounds(constraint, argText) {
			ent.Properties[k] = v
		}
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 3. DTO schema + property extraction (every data class)
	// -----------------------------------------------------------------------
	emitDataClasses(add, src, classHeads, file.Path)

	// -----------------------------------------------------------------------
	// 4. konform DSL → per-field request_validation rules + dto
	// -----------------------------------------------------------------------
	emitKonform(add, src, file.Path)

	// -----------------------------------------------------------------------
	// 5. Valiktor / Arrow contract blocks → request_validation + dto
	// -----------------------------------------------------------------------
	for _, m := range reValidationContract.FindAllStringSubmatchIndex(src, -1) {
		line := lineOf(src, m[0])
		typeName := ""
		if m[2] >= 0 {
			typeName = src[m[2]:m[3]]
		}
		name := "validation:contract:" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "kotlin", line)
		setProps(&ent,
			"validation_framework", "Valiktor",
			"validation_kind", "contract",
			"provenance", "INFERRED_FROM_VALIDATION_CONTRACT",
		)
		if typeName != "" {
			setProps(&ent, "validated_type", typeName)
		}
		add(ent)

		if typeName != "" && !kotlinPrimitives[typeName] {
			dtoEnt := makeEntity(typeName, "SCOPE.Schema", "dto", file.Path, "kotlin", line)
			setProps(&dtoEnt,
				"validation_framework", "Valiktor",
				"provenance", "INFERRED_FROM_VALIDATION_CONTRACT",
			)
			add(dtoEnt)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type classHead struct {
	name string
	off  int
}

func indexDataClasses(src string) []classHead {
	var heads []classHead
	for _, m := range reDataClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		if kotlinPrimitives[className] {
			continue
		}
		heads = append(heads, classHead{name: className, off: m[0]})
	}
	return heads
}

// beanFieldName resolves the property name a bean-validation annotation applies
// to by scanning forward from the end of the annotation match for the next
// `val`/`var <name>` declaration on the same logical property. Annotations
// precede their property in Kotlin constructor/body declarations.
func beanFieldName(src string, from int) string {
	// Look ahead a bounded window for the property declaration.
	end := from + 200
	if end > len(src) {
		end = len(src)
	}
	window := src[from:end]
	if m := rePropDeclLookahead.FindStringSubmatch(window); m != nil {
		return m[1]
	}
	return ""
}

var rePropDeclLookahead = regexp.MustCompile(
	`(?:@(?:field:|get:|param:|property:|set:)?\w+(?:\s*\([^)]*\))?\s*)*` +
		`(?:val|var)\s+([a-z_][A-Za-z0-9_]*)`,
)

// beanConstraintBounds parses the parsed bound(s) of a bean-validation
// constraint argument list into typed property keys.
func beanConstraintBounds(constraint, argText string) map[string]string {
	out := map[string]string{}
	argText = strings.TrimSpace(argText)
	if argText == "" {
		return out
	}
	switch constraint {
	case "Size", "Digits", "Length":
		if v := namedArg(argText, "min"); v != "" {
			out["min"] = v
		}
		if v := namedArg(argText, "max"); v != "" {
			out["max"] = v
		}
		if v := namedArg(argText, "integer"); v != "" {
			out["integer"] = v
		}
		if v := namedArg(argText, "fraction"); v != "" {
			out["fraction"] = v
		}
	case "Min", "Max", "DecimalMin", "DecimalMax":
		if v := namedArg(argText, "value"); v != "" {
			out["value"] = v
		} else if v := firstScalarArg(argText); v != "" {
			out["value"] = v
		}
	case "Pattern":
		if v := namedArg(argText, "regexp"); v != "" {
			out["regexp"] = v
		}
	}
	return out
}

// namedArg extracts a `name = value` argument (value may be quoted) from a
// comma-separated annotation argument list.
func namedArg(args, name string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*=\s*("(?:[^"\\]|\\.)*"|[^,)]+)`)
	m := re.FindStringSubmatch(args)
	if m == nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"`)
}

// firstScalarArg returns the first positional scalar argument.
func firstScalarArg(args string) string {
	parts := strings.SplitN(args, ",", 2)
	v := strings.TrimSpace(parts[0])
	if strings.Contains(v, "=") {
		return ""
	}
	return strings.Trim(v, `"`)
}

// emitDataClasses emits one SCOPE.Schema(dto) per data class and records each
// property's name, type, nullability, wire-name override and default into the
// DTO entity's properties (encoded compactly + per-property keys).
func emitDataClasses(add func(types.EntityRecord), src string, heads []classHead, filePath string) {
	for i, h := range heads {
		// Body span: from this class head to the next class head (or EOF).
		bodyEnd := len(src)
		if i+1 < len(heads) {
			bodyEnd = heads[i+1].off
		}
		body := src[h.off:bodyEnd]
		line := lineOf(src, h.off)

		dtoEnt := makeEntity(h.name, "SCOPE.Schema", "dto", filePath, "kotlin", line)
		setProps(&dtoEnt, "provenance", "INFERRED_FROM_DATA_CLASS")

		var propList []string
		var validatesEdges []types.RelationshipRecord
		for _, pm := range reDataProperty.FindAllStringSubmatchIndex(body, -1) {
			annoBlock := body[pm[2]:pm[3]]
			pname := body[pm[6]:pm[7]]
			ptype := strings.TrimSpace(body[pm[8]:pm[9]])
			pdefault := ""
			if pm[10] >= 0 {
				pdefault = strings.TrimSpace(body[pm[10]:pm[11]])
			}
			nullable := strings.HasSuffix(ptype, "?")

			wire := pname
			if w := reSerialName.FindStringSubmatch(annoBlock); w != nil {
				wire = w[1]
			} else if w := reJsonProperty.FindStringSubmatch(annoBlock); w != nil {
				wire = w[1]
			}

			// ── Nested @Valid recursion → VALIDATES edge (#4972, parity Java #3605).
			// When a property carries @Valid (any use-site target) — or the
			// generic element form List<@Valid X> — emit a class→class VALIDATES
			// edge from the owning DTO to the nested DTO type. For a collection
			// property the validation target is the element type, not the wrapper.
			if nested := nestedValidTarget(annoBlock, ptype); nested != "" && !kotlinPrimitives[nested] {
				validatesEdges = append(validatesEdges, types.RelationshipRecord{
					ToID: nested,
					Kind: "VALIDATES",
					Properties: map[string]string{
						"field":     pname,
						"via":       "valid_annotation",
						"framework": "BeanValidation",
						"owner_dto": h.name,
					},
				})
			}

			// Per-property keys (value-asserted by tests).
			setProps(&dtoEnt,
				"prop."+pname+".type", ptype,
				"prop."+pname+".nullable", strconv.FormatBool(nullable),
				"prop."+pname+".wire_name", wire,
			)
			if pdefault != "" {
				dtoEnt.Properties["prop."+pname+".default"] = pdefault
			}
			propList = append(propList, pname)
		}
		if len(propList) > 0 {
			setProps(&dtoEnt, "properties", strings.Join(propList, ","))
			setProps(&dtoEnt, "property_count", strconv.Itoa(len(propList)))
		}
		if len(validatesEdges) > 0 {
			dtoEnt.Relationships = append(dtoEnt.Relationships, validatesEdges...)
		}
		add(dtoEnt)
	}
}

// nestedValidTarget resolves the nested DTO type a @Valid cascade annotation
// targets on a data-class property, or "" when the property is not @Valid.
// It handles both the property-level annotation form (@field:Valid val nested:
// AddressDto) and the generic-element form (val items: List<@Valid AddressDto>),
// unwrapping a single level of collection / generic wrapper to the element type.
func nestedValidTarget(annoBlock, ptype string) string {
	// Generic-element form: List<@Valid AddressDto> / Map<String, @Valid X>.
	if m := reElementValid.FindStringSubmatch(ptype); m != nil {
		return strings.TrimSuffix(m[1], "?")
	}
	// Property-level @Valid (any use-site target) on the leading annotation block.
	if !reFieldValid.MatchString(annoBlock) {
		return ""
	}
	return validationElementType(ptype)
}

// validationElementType strips nullability and unwraps one level of a generic
// collection wrapper (List<T>, Set<T>, Collection<T>, Iterable<T>, Array<T>) to
// the element type T, so the VALIDATES edge points at the validated DTO rather
// than the collection. For Map<K,V> the value type V is taken.
func validationElementType(ptype string) string {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(ptype), "?"))
	open := strings.IndexByte(t, '<')
	if open < 0 {
		return t
	}
	wrapper := strings.TrimSpace(t[:open])
	inner := strings.TrimSpace(strings.TrimSuffix(t[open+1:], ">"))
	switch wrapper {
	case "List", "Set", "Collection", "Iterable", "Sequence", "Array", "MutableList", "MutableSet":
		return strings.TrimSuffix(strings.TrimSpace(inner), "?")
	case "Map", "MutableMap":
		// Map<K, V> → value type V.
		if c := strings.LastIndexByte(inner, ','); c >= 0 {
			return strings.TrimSuffix(strings.TrimSpace(inner[c+1:]), "?")
		}
		return ""
	}
	// Unknown generic — treat the raw wrapper as the validated type.
	return wrapper
}

// emitKonform parses konform Validation<T> { T::field { constraint(bound) } }
// DSL blocks, emitting one request_validation rule per field+constraint with
// the parsed bound, plus the validated DTO schema.
func emitKonform(add func(types.EntityRecord), src, filePath string) {
	for _, hm := range reKonformHead.FindAllStringSubmatchIndex(src, -1) {
		validated := src[hm[2]:hm[3]]
		open := hm[1] - 1 // index of the head '{'
		end := matchBraceKotlin(src, open)
		if end < 0 {
			end = len(src)
		}
		blockBody := src[open+1 : end]
		blockBase := open + 1
		line := lineOf(src, hm[0])

		if !kotlinPrimitives[validated] {
			dtoEnt := makeEntity(validated, "SCOPE.Schema", "dto", filePath, "kotlin", line)
			setProps(&dtoEnt,
				"validation_framework", "konform",
				"provenance", "INFERRED_FROM_KONFORM",
			)
			add(dtoEnt)
		}

		// Each property block:  Foo::field { ... }.
		for _, pm := range reKonformProperty.FindAllStringSubmatchIndex(blockBody, -1) {
			field := blockBody[pm[4]:pm[5]]
			pOpen := pm[1] - 1 // the property block '{'
			pEnd := matchBraceKotlin(blockBody, pOpen)
			if pEnd < 0 {
				pEnd = len(blockBody)
			}
			propBody := blockBody[pOpen+1 : pEnd]
			pLine := lineOf(src, blockBase+pm[0])

			for _, cm := range reKonformConstraint.FindAllStringSubmatchIndex(propBody, -1) {
				constraint := propBody[cm[2]:cm[3]]
				bound := ""
				if cm[4] >= 0 {
					bound = strings.Trim(strings.TrimSpace(propBody[cm[4]:cm[5]]), `"`)
				}
				name := "validation:rule:" + validated + "." + field + ":" + constraint
				ent := makeEntity(name, "SCOPE.Pattern", "request_validation", filePath, "kotlin", pLine)
				setProps(&ent,
					"validation_framework", "konform",
					"validation_kind", "rule",
					"dto", validated,
					"field_name", field,
					"constraint", constraint,
					"provenance", "INFERRED_FROM_KONFORM",
				)
				if bound != "" {
					ent.Properties["bound"] = bound
				}
				add(ent)
			}
		}
	}
}

// matchBraceKotlin returns the index of the brace matching the one at open,
// skipping over string literals. Returns -1 when unbalanced.
func matchBraceKotlin(src string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == quote && (i == 0 || src[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
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
