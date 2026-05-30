// Package csharp — validation extractor for C# source files.
//
// Detects two validation styles:
//
//  1. FluentValidation: AbstractValidator<T> subclasses with .RuleFor(...) chains.
//     Emits SCOPE.Pattern (subtype="request_validation") per validator class
//     and SCOPE.Schema (subtype="dto") per validated DTO type T.
//
//  2. DataAnnotations: [Required]/[StringLength]/[Range]/[RegularExpression]/[EmailAddress]/
//     [MinLength]/[MaxLength]/[Compare]/[Phone]/[Url] attributes on model properties.
//     Emits SCOPE.Pattern (subtype="request_validation") per annotated property site
//     and SCOPE.Schema (subtype="dto") per model class bearing annotations.
//
//  3. [ApiController] auto-validation: presence of [ApiController] attribute triggers
//     automatic ModelState validation. Emits a single SCOPE.Pattern per file.
//
// These entities cause request_validation and dto_extraction coverage cells to light up
// for the 6 C# backend framework records.
package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_validation", &csharpValidationExtractor{})
}

type csharpValidationExtractor struct{}

func (e *csharpValidationExtractor) Language() string { return "custom_csharp_validation" }

// ---------------------------------------------------------------------------
// Regexes
// ---------------------------------------------------------------------------

var (
	// FluentValidation: class FooValidator : AbstractValidator<FooDto>
	reAbstractValidator = regexp.MustCompile(
		`\bclass\s+(\w+)\s*:\s*(?:\w+\.)*AbstractValidator\s*<\s*(\w+)\s*>`,
	)

	// FluentValidation: .RuleFor(x => x.Property)
	reRuleFor = regexp.MustCompile(
		`\.RuleFor\s*\(`,
	)

	// DataAnnotations on properties/fields — the most common validation attributes.
	// Matches [Required], [StringLength(...)], [Range(...)], [RegularExpression(...)],
	// [EmailAddress], [MinLength(...)], [MaxLength(...)], [Compare(...)], [Phone], [Url].
	reDataAnnotation = regexp.MustCompile(
		`\[\s*(?:Required|StringLength|Range|RegularExpression|EmailAddress|MinLength|MaxLength|Compare|Phone|Url)\s*(?:\([^)]*\))?\s*\]`,
	)

	// Model class that has data annotations — we capture the class name.
	// Matches "public class FooModel" or "public class FooRequest" etc.
	// We use this to emit a DTO entity for the model bearing annotations.
	reClassDecl = regexp.MustCompile(
		`(?m)^\s*(?:public\s+)?(?:partial\s+)?(?:internal\s+)?class\s+(\w+)`,
	)

	// [ApiController] auto-validation marker.
	reApiController = regexp.MustCompile(
		`\[ApiController\s*\]`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *csharpValidationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_validation_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
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
	// 1. FluentValidation — AbstractValidator<T> subclasses
	// -----------------------------------------------------------------------
	for _, m := range reAbstractValidator.FindAllStringSubmatchIndex(src, -1) {
		validatorName := src[m[2]:m[3]]
		dtoType := src[m[4]:m[5]]
		line := lineOf(src, m[0])

		// Emit validation pattern for the validator class itself.
		name := "validation:fluent:" + validatorName
		ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "csharp", line)
		setProps(&ent,
			"validation_framework", "FluentValidation",
			"validator_class", validatorName,
			"validated_type", dtoType,
		)
		add(ent)

		// Emit DTO schema entity for the validated type (if not primitive).
		if !csharpPrimitives[dtoType] {
			dtoEnt := makeEntity(dtoType, "SCOPE.Schema", "dto", file.Path, "csharp", line)
			setProps(&dtoEnt,
				"validation_framework", "FluentValidation",
				"provenance", "INFERRED_FROM_ABSTRACT_VALIDATOR",
			)
			add(dtoEnt)
		}
	}

	// If the file has any .RuleFor(... calls and we found validators, emit a
	// "has_rule_for" marker per file (idempotent — used as supporting signal).
	if reRuleFor.MatchString(src) {
		hasValidator := reAbstractValidator.MatchString(src)
		if hasValidator {
			name := "validation:fluent:rule_for:" + file.Path
			ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "csharp", 1)
			setProps(&ent, "validation_framework", "FluentValidation", "detail", "rule_for_present")
			add(ent)
		}
	}

	// -----------------------------------------------------------------------
	// 2. DataAnnotations — [Required] / [StringLength] / etc.
	// -----------------------------------------------------------------------
	if reDataAnnotation.MatchString(src) {
		// Emit a per-annotation-site pattern entity.
		for _, m := range reDataAnnotation.FindAllStringIndex(src, -1) {
			attrText := src[m[0]:m[1]]
			// Trim brackets and args to get the attribute name.
			attrName := reDataAnnotation.FindString(attrText)
			// Extract just the attribute identifier (before '(' or ']').
			cleanAttr := attrName
			if idx := indexByte(cleanAttr, '('); idx >= 0 {
				cleanAttr = cleanAttr[:idx]
			}
			if idx := indexByte(cleanAttr, ']'); idx >= 0 {
				cleanAttr = cleanAttr[:idx]
			}
			// Strip leading '[' and whitespace.
			cleanAttr = trimLeadingBracketSpace(cleanAttr)

			line := lineOf(src, m[0])
			name := "validation:annotation:" + cleanAttr + ":" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "csharp", line)
			setProps(&ent,
				"validation_framework", "DataAnnotations",
				"annotation", cleanAttr,
			)
			add(ent)
		}

		// Emit DTO schema entities for classes in this file that carry annotations.
		for _, m := range reClassDecl.FindAllStringSubmatchIndex(src, -1) {
			className := src[m[2]:m[3]]
			if csharpPrimitives[className] {
				continue
			}
			line := lineOf(src, m[0])
			dtoEnt := makeEntity(className, "SCOPE.Schema", "dto", file.Path, "csharp", line)
			setProps(&dtoEnt,
				"validation_framework", "DataAnnotations",
				"provenance", "INFERRED_FROM_DATA_ANNOTATION",
			)
			add(dtoEnt)
		}
	}

	// -----------------------------------------------------------------------
	// 3. [ApiController] auto-validation — emit once per file.
	// -----------------------------------------------------------------------
	if reApiController.MatchString(src) {
		idx := reApiController.FindStringIndex(src)
		line := 1
		if idx != nil {
			line = lineOf(src, idx[0])
		}
		name := "validation:ApiController:auto:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "request_validation", file.Path, "csharp", line)
		setProps(&ent,
			"validation_framework", "ApiController",
			"detail", "auto_model_state_validation",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// String helpers (no extra imports)
// ---------------------------------------------------------------------------

// indexByte returns the index of the first occurrence of b in s, or -1.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// trimLeadingBracketSpace strips a leading '[' and any surrounding whitespace.
func trimLeadingBracketSpace(s string) string {
	for len(s) > 0 && (s[0] == '[' || s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
