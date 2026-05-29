package python

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_marshmallow", &MarshmallowExtractor{})
}

// MarshmallowExtractor extracts marshmallow schema, field, validator, nested,
// and coercion patterns from Python source files.
//
// It emits SCOPE.Pattern entities for:
//   - Schema class definitions (class Foo(Schema):)
//   - Field declarations (fields.Str(), fields.Email(), etc.)
//   - Nested schema references (fields.Nested(OtherSchema))
//   - @validates / @validates_schema decorator functions
//   - @post_load / @pre_load coercion hooks
//
// It deliberately does NOT re-emit the Schema class entity — the base Python
// extractor emits a SCOPE.Component/class node for every class definition.
// Every entity here carries SCOPE.Pattern so it never shadows the class node.
//
// Issue #2985 — marshmallow schema/constraint/validator extraction.
type MarshmallowExtractor struct{}

func (e *MarshmallowExtractor) Language() string { return "python_marshmallow" }

var (
	// class UserSchema(Schema): / class UserSchema(ma.Schema):
	mmSchemaClassRe = regexp.MustCompile(
		`(?m)^class\s+(\w+)\s*\(\s*(?:\w+\.)?Schema\s*\)\s*:`)

	// name = fields.Str() / name = fields.Email(required=True) etc.
	mmFieldRe = regexp.MustCompile(
		`(?m)^[ \t]+(\w+)\s*=\s*(?:\w+\.)?fields\.(\w+)\s*\(([^)]*)\)`)

	// name = fields.Nested(OtherSchema) / Nested("OtherSchema", many=True)
	mmNestedRe = regexp.MustCompile(
		`(?m)^[ \t]+(\w+)\s*=\s*(?:\w+\.)?fields\.Nested\s*\(\s*([^\s,)]+)`)

	// @validates('field_name') / @validates_schema
	mmValidatesRe = regexp.MustCompile(
		`(?m)^[ \t]*@(?:\w+\.)?validates\s*\(\s*["']([^"']+)["']\s*\)\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`)
	mmValidatesSchemaRe = regexp.MustCompile(
		`(?m)^[ \t]*@(?:\w+\.)?validates_schema\s*(?:\([^)]*\))?\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`)

	// @post_load / @pre_load — coercion/deserialization hooks
	mmCoercionHookRe = regexp.MustCompile(
		`(?m)^[ \t]*@(?:\w+\.)?(?:post_load|pre_load|post_dump|pre_dump)\s*(?:\([^)]*\))?\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`)

	// Coercion kwarg on a field, e.g. load_default=, missing=, data_key=
	mmCoercionKwargRe = regexp.MustCompile(`\b(?:load_default|missing|data_key|load_only|dump_only)\s*=`)
)

// marshmallowReferenced reports whether the source likely uses marshmallow.
func marshmallowReferenced(source string) bool {
	return strings.Contains(source, "marshmallow") ||
		strings.Contains(source, "from ma import") ||
		// common aliased import: import marshmallow as ma
		strings.Contains(source, "import marshmallow") ||
		// class Foo(Schema): without explicit marshmallow import
		mmSchemaClassRe.MatchString(source)
}

func (e *MarshmallowExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_marshmallow")
	_, span := tracer.Start(ctx, "custom.python_marshmallow")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)

	if !marshmallowReferenced(source) {
		return nil, nil
	}

	var out []types.EntityRecord

	// 1. Schema class declarations — emit a SCOPE.Pattern entry so the
	// coverage validator can cite this extractor as proof of schema_extraction.
	for _, idx := range allMatchesIndex(mmSchemaClassRe, source) {
		schemaName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(
			"schema_"+schemaName, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":    "marshmallow",
				"pattern_type": "schema_class",
				"schema_name":  schemaName,
			},
		))
	}

	// 2. Field declarations — capture field name + marshmallow field type.
	for _, idx := range allMatchesIndex(mmFieldRe, source) {
		fieldName := source[idx[2]:idx[3]]
		fieldType := source[idx[4]:idx[5]]
		args := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		props := map[string]string{
			"framework":    "marshmallow",
			"pattern_type": "field",
			"field":        fieldName,
			"field_type":   fieldType,
		}
		if strings.Contains(args, "required=True") {
			props["required"] = "true"
		}
		if m := regexp.MustCompile(`validate\s*=\s*(\w[\w.]*)`).FindStringSubmatch(args); m != nil {
			props["validate"] = m[1]
		}
		out = append(out, entity(
			"field_"+fieldName, string(types.EntityKindPattern), "",
			file.Path, line, props,
		))
	}

	// 3. Nested field declarations — evidence of nested_model_extraction.
	for _, idx := range allMatchesIndex(mmNestedRe, source) {
		fieldName := source[idx[2]:idx[3]]
		nestedSchema := strings.Trim(source[idx[4]:idx[5]], `"'`)
		line := lineOf(source, idx[0])
		out = append(out, entity(
			"nested_"+fieldName, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":     "marshmallow",
				"pattern_type":  "nested_field",
				"field":         fieldName,
				"nested_schema": nestedSchema,
			},
		))
	}

	// 4. @validates('field') field-level validators.
	for _, idx := range allMatchesIndex(mmValidatesRe, source) {
		targetField := source[idx[2]:idx[3]]
		fnName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(
			"validate_"+fnName, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":    "marshmallow",
				"pattern_type": "field_validator",
				"target_field": targetField,
				"validator_fn": fnName,
			},
		))
	}

	// 5. @validates_schema cross-field validators.
	for _, idx := range allMatchesIndex(mmValidatesSchemaRe, source) {
		fnName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(
			"validate_schema_"+fnName, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":    "marshmallow",
				"pattern_type": "schema_validator",
				"validator_fn": fnName,
			},
		))
	}

	// 6. @post_load / @pre_load coercion hooks.
	for _, idx := range allMatchesIndex(mmCoercionHookRe, source) {
		fnName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		// Determine the exact hook decorator from the source text.
		hookText := source[idx[0]:idx[1]]
		hookType := "post_load"
		for _, ht := range []string{"pre_load", "post_load", "pre_dump", "post_dump"} {
			if strings.Contains(hookText, ht) {
				hookType = ht
				break
			}
		}
		out = append(out, entity(
			"coerce_"+fnName, string(types.EntityKindPattern), "",
			file.Path, line,
			map[string]string{
				"framework":    "marshmallow",
				"pattern_type": "coercion_hook",
				"hook_type":    hookType,
				"hook_fn":      fnName,
			},
		))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
