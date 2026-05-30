// dry_types.go — dry-types / dry-struct type extraction for Ruby.
//
// dry-rb is the only Ruby framework with an explicit static type system:
//
//   - dry-types:   Types::String, Types::Integer.constrained(gt: 0),
//     Types::Hash.schema(name: Types::String), custom type aliases.
//   - dry-struct:  class Foo < Dry::Struct
//     attribute :name, Types::String
//     attribute :age,  Types::Integer
//     end
//   - dry-schema / dry-validation: schema { required(:name).filled(:string) }
//
// Coverage cell flipped:
//
//	lang.ruby.framework.dry-rb  Type System/type_extraction → partial
//
// Part of #3282.
package ruby

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("ruby_dry_types", &dryTypesExtractor{})
}

type dryTypesExtractor struct{}

func (e *dryTypesExtractor) Language() string { return "ruby_dry_types" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// class Foo < Dry::Struct (dry-struct model class)
	reDryStructClass = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*)\s*<\s*(?:Dry::Struct|Dry::Value)\b`,
	)

	// attribute :name, Types::String  (inside a Dry::Struct subclass)
	reDryStructAttr = regexp.MustCompile(
		`(?m)^\s*attribute\s+:([a-z_?]+),\s*(Types::[A-Za-z0-9_:.\(\)]+)`,
	)

	// attribute? :name, Types::... (optional attribute)
	reDryStructAttrOpt = regexp.MustCompile(
		`(?m)^\s*attribute\?\s+:([a-z_]+),\s*(Types::[A-Za-z0-9_:.\(\)]+)`,
	)

	// module Types / Types = Dry.Types() — type container definition
	reDryTypesModule = regexp.MustCompile(
		`(?m)^\s*(?:module\s+Types\b|Types\s*=\s*Dry\.Types\b|Types\s*=\s*Dry::Types)`,
	)

	// FooType = Types::String.constrained(...)  — named type alias
	reDryTypeAlias = regexp.MustCompile(
		`(?m)^\s*([A-Z][A-Za-z0-9_]*)\s*=\s*(Types::[A-Za-z0-9_:.\(\)]+)`,
	)

	// dry-schema required(:name).filled(:string) / optional(:age).value(:integer)
	reDrySchemaRule = regexp.MustCompile(
		`(?m)\b(required|optional)\(:([a-z_]+)\)`,
	)

	// Dry::Schema.Params / Dry::Schema.JSON / Dry::Validation::Contract
	reDrySchemaContract = regexp.MustCompile(
		`(?m)\b(?:Dry::Schema\.(Params|JSON|define)|Dry::Validation::Contract)\b`,
	)

	// include Dry::Monads / extend Dry::Initializer
	reDryInclude = regexp.MustCompile(
		`(?m)\b(?:include|extend|prepend)\s+(Dry::[A-Za-z0-9_:]+)`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *dryTypesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.dry_types_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "ruby" {
		return nil, nil
	}

	src := string(file.Content)

	// Fast guard: only process files with dry-rb signals.
	hasDry := strings.Contains(src, "Dry::") ||
		strings.Contains(src, "Types::") ||
		strings.Contains(src, "dry-") ||
		strings.Contains(src, "dry/")
	if !hasDry {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// ---- Dry::Struct subclasses ----
	for _, idx := range reDryStructClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		ent := makeEntity("dry_struct:"+className, "SCOPE.Schema", "type", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_STRUCT_CLASS",
			"struct_class", className,
		)
		add(ent)
	}

	// ---- attribute declarations inside Dry::Struct ----
	for _, idx := range reDryStructAttr.FindAllStringSubmatchIndex(src, -1) {
		attrName := src[idx[2]:idx[3]]
		attrType := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		name := "dry_attr:" + attrName
		ent := makeEntity(name, "SCOPE.Schema", "column", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_STRUCT_ATTR",
			"attr_name", attrName,
			"attr_type", attrType,
		)
		add(ent)
	}

	// ---- optional attribute? ----
	for _, idx := range reDryStructAttrOpt.FindAllStringSubmatchIndex(src, -1) {
		attrName := src[idx[2]:idx[3]]
		attrType := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		name := "dry_attr_opt:" + attrName
		ent := makeEntity(name, "SCOPE.Schema", "column", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_STRUCT_ATTR_OPT",
			"attr_name", attrName,
			"attr_type", attrType,
			"optional", "true",
		)
		add(ent)
	}

	// ---- Types module container ----
	if reDryTypesModule.MatchString(src) {
		loc := reDryTypesModule.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("dry_types_module", "SCOPE.Component", "type", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_TYPES_MODULE",
		)
		add(ent)
	}

	// ---- Named type aliases: FooType = Types::String ----
	for _, idx := range reDryTypeAlias.FindAllStringSubmatchIndex(src, -1) {
		aliasName := src[idx[2]:idx[3]]
		baseType := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		name := "dry_type_alias:" + aliasName
		ent := makeEntity(name, "SCOPE.Schema", "type", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_TYPE_ALIAS",
			"alias_name", aliasName,
			"base_type", baseType,
		)
		add(ent)
	}

	// ---- dry-schema / dry-validation contract ----
	if reDrySchemaContract.MatchString(src) {
		loc := reDrySchemaContract.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("dry_schema_contract", "SCOPE.Schema", "type", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_SCHEMA_CONTRACT",
		)
		add(ent)

		// Individual field rules.
		for _, idx := range reDrySchemaRule.FindAllStringSubmatchIndex(src, -1) {
			reqType := src[idx[2]:idx[3]]
			fieldName := src[idx[4]:idx[5]]
			ruleLn := lineOf(src, idx[0])
			ruleName := "dry_schema_rule:" + reqType + ":" + fieldName
			ruleEnt := makeEntity(ruleName, "SCOPE.Schema", "column", file.Path, file.Language, ruleLn)
			setProps(&ruleEnt,
				"framework", "dry-rb",
				"provenance", "INFERRED_FROM_DRY_SCHEMA_RULE",
				"field_name", fieldName,
				"field_required", reqType,
			)
			add(ruleEnt)
		}
	}

	// ---- Dry module inclusions ----
	for _, idx := range reDryInclude.FindAllStringSubmatchIndex(src, -1) {
		moduleName := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		name := "dry_include:" + moduleName
		ent := makeEntity(name, "SCOPE.Pattern", "mixin", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_INCLUDE",
			"module_name", moduleName,
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
