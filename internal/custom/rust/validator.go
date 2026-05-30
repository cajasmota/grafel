package rust

// validator.go — coverage for the Rust `validator` crate as a standalone
// validation library (record lang.rust.validation.validator, epic #3505,
// issue #3545).
//
// The validator crate is the de-facto declarative validation library in the
// Rust ecosystem. It is framework-agnostic: a `#[derive(Validate)]` struct with
// `#[validate(...)]` field attributes can be validated by calling `.validate()`
// anywhere — inside an HTTP handler, a CLI, a background job, or a test.
//
// fw_validation.go already detects validator-crate signals *in the context of
// HTTP framework DTOs* (records lang.rust.framework.*). This extractor records
// the same crate as a first-class VALIDATION LIBRARY, emitting library-semantic
// entities that map onto the validation_lib taxonomy lanes:
//
//   schema_extraction          — #[derive(Validate)] struct → validator_schema
//   nested_model_extraction    — #[validate(nested)] field → nested_validation
//   constraint_extraction      — each #[validate(<rule>)] → SCOPE.Constraint with
//                                the SPECIFIC field + constraint kind + bound/regex
//   custom_validator_extraction— #[validate(custom = "fn")] / schema-level
//                                #[validate(schema(function = "fn"))]
//   type_coercion_recognition  — (n/a for validator; coercion is serde's job)
//   tests_linkage              — substrate via the general rust test linkage
//
// Detection is regex-based on source text (honest `partial`→`full` where a
// fixture proves the exact captured value). The heavy parsing — balanced struct
// bodies, per-field serde/validate attribute windows, typed constraint parsing
// with bounds — is reused from fw_validation.go (extractDTOs / parseValidateRules).
//
// Issue #3545 — lang.rust.validation.validator.

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
	extractor.Register("custom_rust_validator", &rustValidatorExtractor{})
}

type rustValidatorExtractor struct{}

func (e *rustValidatorExtractor) Language() string { return "custom_rust_validator" }

var (
	// container-level #[validate(schema(function = "fn"))] — struct-wide custom
	// validator (validator crate's whole-struct hook).
	reValidatorSchemaFn = regexp.MustCompile(
		`#\[validate\s*\(\s*schema\s*\(([^)]*)\)\s*\)\]`,
	)

	// #[derive(... Validate ...)] with validator crate full-path variant too.
	reValidatorDerive = regexp.MustCompile(
		`#\[derive\([^)]*\b(?:Validate|validator::Validate)\b[^)]*\)\]`,
	)
)

func (e *rustValidatorExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_validator_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)

	// Cheap gate: only act on files that mention the validator crate. This keeps
	// the extractor a no-op on the vast majority of Rust files and avoids
	// double-counting plain serde DTOs (which are dto_extraction, not validation).
	if !reValidatorDerive.MatchString(src) && !strings.Contains(src, "validator") {
		// `.validate()` alone is too ambiguous (many crates expose it); require a
		// validator-crate anchor in the file.
		return nil, nil
	}

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
	// 1. schema_extraction + constraint_extraction + nested_model_extraction
	//    + custom_validator_extraction  — from #[derive(Validate)] structs.
	// -----------------------------------------------------------------------
	for _, dto := range extractDTOs(src) {
		if !dto.hasValidate {
			// validator coverage is about Validate-derived structs specifically.
			continue
		}

		// schema_extraction: the validated struct itself.
		schemaEnt := makeEntity("validator_schema:"+dto.structName,
			"SCOPE.Schema", "validator_schema",
			file.Path, file.Language, lineOf(src, dto.offset))
		setProps(&schemaEnt,
			"provenance", "INFERRED_FROM_VALIDATE_DERIVE",
			"crate", "validator",
			"struct_name", dto.structName,
			"field_count", itoa(len(dto.fields)),
		)
		add(schemaEnt)

		// struct-level custom validator: #[validate(schema(function = "fn"))].
		// It sits in the container-attribute window between the derive and the
		// struct body — re-scan a bounded slice ahead of the derive offset.
		head := src[dto.offset:]
		if len(head) > 600 {
			head = head[:600]
		}
		for _, sm := range reValidatorSchemaFn.FindAllStringSubmatch(head, -1) {
			fn := firstStringArg(strings.TrimSpace(sm[1]), "function")
			cName := "validator_custom:" + dto.structName + ":schema"
			cEnt := makeEntity(cName, "SCOPE.Pattern", "custom_validator",
				file.Path, file.Language, lineOf(src, dto.offset))
			setProps(&cEnt,
				"provenance", "INFERRED_FROM_VALIDATE_SCHEMA_FN",
				"crate", "validator",
				"struct_name", dto.structName,
				"scope", "struct",
				"function", fn,
			)
			add(cEnt)
		}

		// Per-field constraints, nested markers, and field-level custom fns.
		for _, f := range dto.fields {
			for _, c := range f.constraints {
				switch c.kind {
				case "nested":
					// nested_model_extraction: #[validate(nested)] recurses into
					// the field's own Validate type.
					nName := "validator_nested:" + dto.structName + "." + f.name
					nEnt := makeEntity(nName, "SCOPE.Schema", "nested_validation",
						file.Path, file.Language, lineOf(src, f.offset))
					setProps(&nEnt,
						"provenance", "INFERRED_FROM_VALIDATE_NESTED",
						"crate", "validator",
						"struct_name", dto.structName,
						"field_name", f.name,
						"field_type", f.typ,
					)
					add(nEnt)

				case "custom":
					// custom_validator_extraction: field-level custom fn.
					cName := "validator_custom:" + dto.structName + "." + f.name
					cEnt := makeEntity(cName, "SCOPE.Pattern", "custom_validator",
						file.Path, file.Language, lineOf(src, f.offset))
					setProps(&cEnt,
						"provenance", "INFERRED_FROM_VALIDATE_CUSTOM",
						"crate", "validator",
						"struct_name", dto.structName,
						"field_name", f.name,
						"scope", "field",
						"function", c.value,
					)
					add(cEnt)

				default:
					// constraint_extraction: one SCOPE.Constraint per rule,
					// carrying the SPECIFIC field + kind + bound(s)/regex.
					cName := "validator_constraint:" + dto.structName + "." + f.name + ":" + c.kind
					cEnt := makeEntity(cName, "SCOPE.Constraint", "validator_constraint",
						file.Path, file.Language, lineOf(src, f.offset))
					setProps(&cEnt,
						"provenance", "INFERRED_FROM_VALIDATE_ATTR",
						"crate", "validator",
						"struct_name", dto.structName,
						"field_name", f.name,
						"constraint_kind", c.kind,
					)
					if c.min != "" {
						setProps(&cEnt, "min", c.min)
					}
					if c.max != "" {
						setProps(&cEnt, "max", c.max)
					}
					if c.value != "" {
						setProps(&cEnt, "value", c.value)
					}
					add(cEnt)
				}
			}
		}
	}

	// -----------------------------------------------------------------------
	// 2. validation invocation — .validate()? / .validate() call sites.
	// -----------------------------------------------------------------------
	for _, m := range reValidateCall.FindAllStringIndex(src, -1) {
		ent := makeEntity("validator_validate_call", "SCOPE.Pattern", "validation_invocation",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"provenance", "INFERRED_FROM_VALIDATE_CALL",
			"crate", "validator",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
