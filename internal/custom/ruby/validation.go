// validation.go — Ruby validation + DTO extractor.
//
// Covers the Validation lane for all 8 Ruby http_backend frameworks:
//
//	request_validation  — Rails strong params (params.require(:x).permit(...)),
//	                      ActiveModel::Validations (validates :field, presence:),
//	                      dry-validation (Dry::Validation.Contract / schema),
//	                      Grape params block, Hanami::Validator / Hanami::Action::Params,
//	                      Roda / Cuba / Sinatra / Padrino rack-level param reads
//	dto_extraction      — strong params permit hash, dry-validation / dry-struct field
//	                      declarations, ActiveModel attribute definitions
//
// Detection is heuristic regex: the extractor recognises canonical call-site
// patterns but does NOT perform cross-file dataflow, so all cells are set to
// `partial`.  This is consistent with the observability and auth extractors.
//
// A single extractor key "custom_ruby_validation" is registered; it runs on any Ruby
// file regardless of framework.
//
// Part of issue #3282.
package ruby

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_ruby_validation", &rubyValidationExtractor{})
}

// rubyValidationExtractor detects validation and DTO-like patterns across Ruby
// source files.
type rubyValidationExtractor struct{}

func (e *rubyValidationExtractor) Language() string { return "custom_ruby_validation" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// --------------- Rails strong params ---------------

	// params.require(:model_name)  /  params.require("model_name")
	rbStrongParamRequire = regexp.MustCompile(
		`(?m)\bparams\.require\s*\(\s*["':][a-z_]+["']?\s*\)`,
	)

	// .permit(:field, :other)  /  .permit(field: [], nested: [:a])
	rbStrongParamPermit = regexp.MustCompile(
		`(?m)\.permit\s*\(([^)]+)\)`,
	)

	// --------------- ActiveModel / ActiveRecord validates ---------------

	// validates :field, presence: true / validates :email, format: {...}
	rbAMValidates = regexp.MustCompile(
		`(?m)^\s*validates?\s+:([a-z_?!]+)`,
	)

	// validate :method_name (custom validator method)
	rbAMValidateMethod = regexp.MustCompile(
		`(?m)^\s*validate\s+:([a-z_?!]+)`,
	)

	// validates_presence_of / validates_length_of / validates_uniqueness_of (classic API)
	rbAMValidatesClassic = regexp.MustCompile(
		`(?m)^\s*(validates_(?:presence|length|uniqueness|format|numericality|inclusion|exclusion|confirmation|acceptance|associated)_of)\s+:([a-z_]+)`,
	)

	// attr_accessor / attr_reader used as DTO field declarations in ActiveModel
	rbAMAttrAccessor = regexp.MustCompile(
		`(?m)^\s*attr_(?:accessor|reader|writer)\s+:([a-z_]+(?:,\s*:[a-z_]+)*)`,
	)

	// --------------- dry-validation ---------------

	// Dry::Validation.Contract / Contract.new
	rbDryContractDef = regexp.MustCompile(
		`(?m)\bDry::Validation\.Contract\b|\bDry::Validation::Contract\b`,
	)

	// schema / json / params block inside a Contract
	rbDrySchemaBlock = regexp.MustCompile(
		`(?m)^\s*(?:schema|json|params)\s+do`,
	)

	// required(:field) / optional(:field)
	rbDryRequired = regexp.MustCompile(
		`(?m)\b(required|optional)\s*\(\s*:([a-z_]+)\s*\)`,
	)

	// rule(:field) { ... }
	rbDryRule = regexp.MustCompile(
		`(?m)^\s*rule\s*\(\s*:([a-z_]+)\s*\)`,
	)

	// --------------- dry-struct (DTO) ---------------

	// attribute :field, Types::...
	rbDryStructAttribute = regexp.MustCompile(
		`(?m)^\s*attribute\s+:([a-z_]+),\s*Types::`,
	)

	// --------------- Grape params block ---------------

	// params do / group :name do (Grape validation)
	rbGrapeParams = regexp.MustCompile(
		`(?m)^\s*params\s+do\b`,
	)

	// requires :field / optional :field
	rbGrapeRequires = regexp.MustCompile(
		`(?m)^\s*(requires|optional)\s+:([a-z_]+)`,
	)

	// --------------- Hanami params / validator ---------------

	// Hanami::Action::Params / include Hanami::Action::Params
	rbHanamiParams = regexp.MustCompile(
		`(?m)\bHanami(?:::Action)?::Params\b`,
	)

	// param :field (Hanami param declaration)
	rbHanamiParam = regexp.MustCompile(
		`(?m)^\s*param\s+:([a-z_]+)`,
	)

	// --------------- Generic rack/Sinatra/Cuba/Roda/Padrino ---------------

	// params[:field]  /  params["field"]
	// Match params[:name], params["name"], params['name'].
	rbParamAccess = regexp.MustCompile(
		`(?m)\bparams\[(?:["']([a-z_]+)["']|:([a-z_]+))\]`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rubyValidationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.validation_extractor.extract",
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

	// Fast guard: skip files with no validation-relevant tokens.
	hasValidation := strings.Contains(src, "params") ||
		strings.Contains(src, "validates") ||
		strings.Contains(src, "validate ") ||
		strings.Contains(src, "Dry::Validation") ||
		strings.Contains(src, "dry-validation") ||
		strings.Contains(src, "Hanami::Action::Params") ||
		strings.Contains(src, "attribute :") ||
		strings.Contains(src, "required(") ||
		strings.Contains(src, "optional(")
	if !hasValidation {
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

	// #4367 — owning model class for this file (one model per file is the Rails
	// convention). When present, ActiveModel `validates :field` declarations
	// carry a CONTAINS edge from the model so the validation field is a member,
	// not an orphan. Plain (non-model) classes carrying validations stay
	// honest-partial (no model node to anchor membership).
	valOwnerModel := ""
	if mm := reARModelClass.FindStringSubmatch(src); mm != nil {
		valOwnerModel = mm[1]
	}

	// 1. Rails strong params: params.require(...)
	for _, idx := range rbStrongParamRequire.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		call := strings.TrimSpace(src[idx[0]:idx[1]])
		ent := makeEntity("strong_params:"+call, "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "rails",
			"provenance", "INFERRED_FROM_STRONG_PARAMS_REQUIRE",
			"signal", "validation",
		)
		add(ent)
	}

	// 2. .permit(...) — emit a DTO shape entity listing permitted fields.
	for _, idx := range rbStrongParamPermit.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		fields := strings.TrimSpace(src[idx[2]:idx[3]])
		name := "permit:" + truncate(fields, 60)
		ent := makeEntity(name, "SCOPE.Schema", "dto_field", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "rails",
			"provenance", "INFERRED_FROM_STRONG_PARAMS_PERMIT",
			"signal", "dto",
			"permitted_fields", fields,
		)
		add(ent)
	}

	// 3. ActiveModel validates :field
	for _, idx := range rbAMValidates.FindAllStringSubmatchIndex(src, -1) {
		field := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		ent := makeEntity("validates:"+field, "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "activemodel",
			"provenance", "INFERRED_FROM_AM_VALIDATES",
			"signal", "validation",
			"field", field,
		)
		if valOwnerModel != "" {
			setProps(&ent, "owner_model", valOwnerModel)
			ent.Relationships = append(ent.Relationships,
				containsFieldEdge(valOwnerModel, ent.ID, field, "activemodel"))
		}
		add(ent)
	}

	// 4. validate :method (custom validator)
	for _, idx := range rbAMValidateMethod.FindAllStringSubmatchIndex(src, -1) {
		method := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		ent := makeEntity("validate_method:"+method, "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "activemodel",
			"provenance", "INFERRED_FROM_AM_VALIDATE_METHOD",
			"signal", "validation",
			"method", method,
		)
		add(ent)
	}

	// 5. validates_presence_of / validates_uniqueness_of etc.
	for _, idx := range rbAMValidatesClassic.FindAllStringSubmatchIndex(src, -1) {
		macro := src[idx[2]:idx[3]]
		field := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		ent := makeEntity(fmt.Sprintf("%s:%s", macro, field), "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "activemodel",
			"provenance", "INFERRED_FROM_AM_VALIDATES_CLASSIC",
			"signal", "validation",
			"macro", macro,
			"field", field,
		)
		if valOwnerModel != "" {
			setProps(&ent, "owner_model", valOwnerModel)
			ent.Relationships = append(ent.Relationships,
				containsFieldEdge(valOwnerModel, ent.ID, field, "activemodel"))
		}
		add(ent)
	}

	// 6. attr_accessor :field — DTO field declarations in ActiveModel / plain structs.
	for _, idx := range rbAMAttrAccessor.FindAllStringSubmatchIndex(src, -1) {
		fields := strings.TrimSpace(src[idx[2]:idx[3]])
		ln := lineOf(src, idx[0])
		// May be a comma-separated list; emit one entity per field.
		for _, f := range splitSymbols(fields) {
			ent := makeEntity("attr:"+f, "SCOPE.Schema", "dto_field", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "ruby",
				"provenance", "INFERRED_FROM_ATTR_ACCESSOR",
				"signal", "dto",
				"field", f,
			)
			add(ent)
		}
	}

	// 7. dry-validation Contract definition.
	if rbDryContractDef.MatchString(src) || rbDrySchemaBlock.MatchString(src) {
		// Emit one entity for the contract scope.
		loc := rbDryContractDef.FindStringIndex(src)
		if loc == nil {
			loc = rbDrySchemaBlock.FindStringIndex(src)
		}
		if loc != nil {
			ln := lineOf(src, loc[0])
			ent := makeEntity("dry_validation_contract", "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "dry-rb",
				"provenance", "INFERRED_FROM_DRY_VALIDATION_CONTRACT",
				"signal", "validation",
			)
			add(ent)
		}

		// required(:field) / optional(:field) → DTO field entities.
		for _, idx := range rbDryRequired.FindAllStringSubmatchIndex(src, -1) {
			qualifier := src[idx[2]:idx[3]]
			field := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			ent := makeEntity(fmt.Sprintf("dry_%s:%s", qualifier, field), "SCOPE.Schema", "dto_field", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "dry-rb",
				"provenance", "INFERRED_FROM_DRY_REQUIRED_OPTIONAL",
				"signal", "dto",
				"qualifier", qualifier,
				"field", field,
			)
			add(ent)
		}

		// rule(:field) → validation rule entity.
		for _, idx := range rbDryRule.FindAllStringSubmatchIndex(src, -1) {
			field := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("dry_rule:"+field, "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "dry-rb",
				"provenance", "INFERRED_FROM_DRY_RULE",
				"signal", "validation",
				"field", field,
			)
			add(ent)
		}
	}

	// 8. dry-struct: attribute :field, Types::...
	for _, idx := range rbDryStructAttribute.FindAllStringSubmatchIndex(src, -1) {
		field := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		ent := makeEntity("dry_struct_attr:"+field, "SCOPE.Schema", "dto_field", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_STRUCT_ATTRIBUTE",
			"signal", "dto",
			"field", field,
		)
		add(ent)
	}

	// 9. Grape params block + requires/optional.
	if rbGrapeParams.MatchString(src) {
		loc := rbGrapeParams.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("grape_params_block", "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "grape",
			"provenance", "INFERRED_FROM_GRAPE_PARAMS_BLOCK",
			"signal", "validation",
		)
		add(ent)

		for _, idx := range rbGrapeRequires.FindAllStringSubmatchIndex(src, -1) {
			qualifier := src[idx[2]:idx[3]]
			field := src[idx[4]:idx[5]]
			ln2 := lineOf(src, idx[0])
			name := fmt.Sprintf("grape_%s:%s", qualifier, field)
			ent2 := makeEntity(name, "SCOPE.Schema", "dto_field", file.Path, file.Language, ln2)
			setProps(&ent2,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_REQUIRES",
				"signal", "dto",
				"qualifier", qualifier,
				"field", field,
			)
			add(ent2)
		}
	}

	// 10. Hanami params / validator.
	if rbHanamiParams.MatchString(src) {
		loc := rbHanamiParams.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("hanami_action_params", "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "hanami",
			"provenance", "INFERRED_FROM_HANAMI_PARAMS",
			"signal", "validation",
		)
		add(ent)

		for _, idx := range rbHanamiParam.FindAllStringSubmatchIndex(src, -1) {
			field := src[idx[2]:idx[3]]
			ln2 := lineOf(src, idx[0])
			ent2 := makeEntity("hanami_param:"+field, "SCOPE.Schema", "dto_field", file.Path, file.Language, ln2)
			setProps(&ent2,
				"framework", "hanami",
				"provenance", "INFERRED_FROM_HANAMI_PARAM",
				"signal", "dto",
				"field", field,
			)
			add(ent2)
		}
	}

	// 11. Generic param access params[:field] — Sinatra, Cuba, Roda, Padrino.
	//     Only emit when none of the more specific patterns fired; acts as a
	//     signal that the file reads request params (partial evidence for
	//     request_validation).
	if !rbStrongParamRequire.MatchString(src) && !rbGrapeParams.MatchString(src) &&
		!rbHanamiParams.MatchString(src) && !rbDryContractDef.MatchString(src) {
		for _, idx := range rbParamAccess.FindAllStringSubmatchIndex(src, -1) {
			// Group 1: params["field"] / params['field'], Group 2: params[:field]
			var field string
			if idx[2] != -1 {
				field = src[idx[2]:idx[3]]
			} else if idx[4] != -1 {
				field = src[idx[4]:idx[5]]
			} else {
				continue
			}
			ln := lineOf(src, idx[0])
			ent := makeEntity("param_access:"+field, "SCOPE.Pattern", "request_validation", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "rack",
				"provenance", "INFERRED_FROM_PARAM_ACCESS",
				"signal", "validation",
				"field", field,
			)
			add(ent)
		}
	}

	// Deep Rails extraction: per-validator rule entities + per-field strong params.
	// Called after the heuristic passes so both entity sets coexist.
	railsValDeepExtract(src, file, add)

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// truncate shortens s to at most maxLen runes.
func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen])
}

// splitSymbols splits a comma-separated list of Ruby symbols (:foo, :bar) and
// returns just the symbol names without the leading colon.
func splitSymbols(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, ":")
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
