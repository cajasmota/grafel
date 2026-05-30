// Package kotlin — validation extractor for Kotlin source files.
//
// Detects two validation styles:
//
//  1. Bean Validation (javax.validation / jakarta.validation):
//     @Valid/@Validated on handler/controller function parameters.
//     @NotNull/@Size/@Pattern/@Email/@Min/@Max/@NotBlank/@NotEmpty on data-class
//     fields or constructor parameters.
//     Emits SCOPE.Pattern (subtype="request_validation") per annotation site
//     and SCOPE.Schema (subtype="dto") per data class bearing annotations.
//
//  2. Dry::Validation-style contract objects:
//     Kotlin "contract" DSLs (Valiktor / Arrow Validation contract blocks).
//     Emits SCOPE.Pattern (subtype="request_validation") for contract schemas
//     and SCOPE.Schema (subtype="dto") for the parameterised type.
//
// These entities cause request_validation and dto_extraction coverage cells to
// light up for the 6 Kotlin backend framework records:
// spring-boot, ktor, micronaut, quarkus, http4k, javalin.
package kotlin

import (
	"context"
	"regexp"
	"strconv"

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
	// Matches: @Valid FooRequest, @Validated @RequestBody BarDto, etc.
	reValidAnnotationParam = regexp.MustCompile(
		`@(?:Valid|Validated)\b`,
	)

	// Field-level Bean Validation annotations on data-class constructor params
	// or body properties.
	reFieldAnnotation = regexp.MustCompile(
		`@(?:NotNull|NotBlank|NotEmpty|Size|Pattern|Email|Min|Max|Positive|PositiveOrZero|Negative|NegativeOrZero|DecimalMin|DecimalMax|Digits|Future|Past|FutureOrPresent|PastOrPresent|AssertTrue|AssertFalse)\b`,
	)

	// data class FooRequest(...) or data class FooDto(...)
	reDataClass = regexp.MustCompile(
		`(?m)^\s*(?:data\s+class|class)\s+([A-Z][A-Za-z0-9_]*)\s*[(:@]`,
	)

	// Valiktor / Arrow-style: validate(foo) { ... } or Validator { ... }
	// Also matches dry-validation Contract blocks.
	reValidationContract = regexp.MustCompile(
		`\b(?:validate|Validator)\s*(?:<\s*([A-Z][A-Za-z0-9_]*)\s*>)?\s*\(`,
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

	// -----------------------------------------------------------------------
	// 1. @Valid / @Validated on handler parameters → request_validation
	// -----------------------------------------------------------------------
	if reValidAnnotationParam.MatchString(src) {
		for _, m := range reValidAnnotationParam.FindAllStringIndex(src, -1) {
			attrText := src[m[0]:m[1]]
			line := lineOf(src, m[0])
			name := "validation:handler:" + attrText + ":" + file.Path + ":" + strconv.Itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "kotlin", line)
			setProps(&ent,
				"validation_framework", "BeanValidation",
				"annotation", attrText,
				"provenance", "INFERRED_FROM_VALID_ANNOTATION",
			)
			add(ent)
		}
	}

	// -----------------------------------------------------------------------
	// 2. Field-level annotations (@NotNull, @Size, @Email, etc.) → request_validation
	//    + emit DTO schema for data classes in the file
	// -----------------------------------------------------------------------
	if reFieldAnnotation.MatchString(src) {
		for _, m := range reFieldAnnotation.FindAllStringIndex(src, -1) {
			attrText := src[m[0]:m[1]]
			line := lineOf(src, m[0])
			name := "validation:field:" + attrText + ":" + file.Path + ":" + strconv.Itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "kotlin", line)
			setProps(&ent,
				"validation_framework", "BeanValidation",
				"annotation", attrText,
				"provenance", "INFERRED_FROM_FIELD_ANNOTATION",
			)
			add(ent)
		}

		// Emit DTO schema entities for data classes in this file.
		for _, m := range reDataClass.FindAllStringSubmatchIndex(src, -1) {
			className := src[m[2]:m[3]]
			if kotlinPrimitives[className] {
				continue
			}
			line := lineOf(src, m[0])
			dtoEnt := makeEntity(className, "SCOPE.Schema", "dto", file.Path, "kotlin", line)
			setProps(&dtoEnt,
				"validation_framework", "BeanValidation",
				"provenance", "INFERRED_FROM_FIELD_ANNOTATION",
			)
			add(dtoEnt)
		}
	}

	// -----------------------------------------------------------------------
	// 3. Valiktor / Arrow contract blocks → request_validation + dto
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
