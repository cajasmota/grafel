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
	extractor.Register("python_pydantic", &PydanticExtractor{})
}

// PydanticExtractor extracts Pydantic validation patterns that the base Python
// extractor and the schema_detector do not model: custom validators
// (@field_validator / @validator / @model_validator), Field(...) constraints
// (gt=, max_length=, regex=, ...), and model-level coercion/config
// (model_config / Config inner class).
//
// It deliberately does NOT emit a second entity for the model class itself —
// the base Python extractor already emits one SCOPE.Component/class node per
// class definition, and emitting a duplicate inflates node counts (issue #1501,
// mirrored by the FastAPI extractor's same restraint). Every entity this
// extractor emits carries a synthetic, non-colliding name (e.g.
// "validate_<field>", "constraint_<field>") and the SCOPE.Pattern kind so it
// never shadows a real class/function node.
//
// Issue #2984 — Pydantic field_validator / constraint / coercion extraction.
type PydanticExtractor struct{}

func (e *PydanticExtractor) Language() string { return "python_pydantic" }

var (
	// @field_validator('a', 'b', mode='before')  (Pydantic v2)
	// @validator('a', pre=True)                  (Pydantic v1)
	// followed by an optional @classmethod and the validator def.
	pydValidatorRe = regexp.MustCompile(
		`(?m)^[ \t]*@(field_validator|validator)\s*\(([^)]*)\)\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`)
	// @model_validator(mode='after')  (v2) / @root_validator (v1) on a def.
	pydModelValidatorRe = regexp.MustCompile(
		`(?m)^[ \t]*@(model_validator|root_validator)\s*(?:\(([^)]*)\))?\s*\n` +
			`(?:[ \t]*@\w[\w.]*\s*(?:\([^)]*\))?\s*\n)*` +
			`[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`)
	// field: type = Field(...)  — capture field name + the Field(...) arg blob.
	pydFieldRe = regexp.MustCompile(
		`(?m)^[ \t]+(\w+)\s*:\s*[^=\n]+=\s*Field\s*\(([^)]*)\)`)
	// mode= / pre=True quote-tolerant extraction for validator dialect.
	pydModeKwargRe = regexp.MustCompile(`mode\s*=\s*["'](\w+)["']`)
	// model_config = ConfigDict(...) (v2) or `class Config:` inner class (v1).
	pydModelConfigRe = regexp.MustCompile(`(?m)^[ \t]+model_config\s*=\s*ConfigDict\s*\(([^)]*)\)`)
	pydConfigClassRe = regexp.MustCompile(`(?m)^[ \t]+class\s+Config\s*:`)

	// pydModelClassRe matches a Pydantic model class header — a class whose base
	// list includes BaseModel / BaseSettings (directly or as a known alias).
	// Issue #4613: each model's annotated fields become CONTAINS field members.
	pydModelClassRe = regexp.MustCompile(
		`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*\b(?:BaseModel|BaseSettings)\b[^)]*\)\s*:`)
)

// pydConstraintKeys are the Field() keyword constraints we surface. Order is
// deterministic for stable property emission.
var pydConstraintKeys = []string{
	"gt", "ge", "lt", "le",
	"min_length", "max_length", "min_items", "max_items",
	"multiple_of", "max_digits", "decimal_places",
	"pattern", "regex",
}

// pydCoercionConfigKeys are model_config / Config flags that affect type
// coercion behavior. Presence of any flags the model as coercion-aware.
var pydCoercionConfigKeys = []string{
	"strict", "coerce_numbers_to_str", "str_strip_whitespace",
	"validate_assignment", "populate_by_name", "allow_population_by_field_name",
	"use_enum_values", "arbitrary_types_allowed", "extra",
}

// pydFirstQuotedRe pulls quoted string literals out of a Field/decorator
// argument blob, e.g. `'name', 'email'` -> ["name","email"].
var pydFirstQuotedRe = regexp.MustCompile(`["']([^"']+)["']`)

// pydKwargStartRe matches the first keyword argument in a decorator call (a
// bareword identifier followed by a single "=", not "=="). Used to drop the
// kwarg tail before harvesting positional field-name string args.
var pydKwargStartRe = regexp.MustCompile(`\b\w+\s*=\s*[^=]`)

func (e *PydanticExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_pydantic")
	_, span := tracer.Start(ctx, "custom.python_pydantic")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)

	// Gate: only run when the file actually references Pydantic. This avoids
	// emitting spurious "constraint"/"validator" nodes for unrelated code that
	// happens to call a function named Field() or validator().
	if !pydanticReferenced(source) {
		return nil, nil
	}

	dialect := pydanticDialect(source)
	var out []types.EntityRecord

	// 1. Field-level custom validators: @field_validator / @validator.
	for _, idx := range allMatchesIndex(pydValidatorRe, source) {
		decorator := source[idx[2]:idx[3]]
		args := source[idx[4]:idx[5]]
		fnName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		fields := quotedArgs(args)
		props := map[string]string{
			"framework":    "pydantic",
			"pattern_type": "field_validator",
			"decorator":    decorator,
			"validator_fn": fnName,
			"dialect":      validatorDialect(decorator, dialect),
		}
		if len(fields) > 0 {
			props["fields"] = strings.Join(fields, ",")
		}
		if m := pydModeKwargRe.FindStringSubmatch(args); m != nil {
			props["mode"] = m[1]
		} else if strings.Contains(args, "pre=True") || strings.Contains(args, "pre = True") {
			props["mode"] = "before"
		}
		out = append(out, entity("validate_"+fnName, "SCOPE.Pattern", "", file.Path, line, props))
	}

	// 2. Model-level validators: @model_validator / @root_validator.
	for _, idx := range allMatchesIndex(pydModelValidatorRe, source) {
		decorator := source[idx[2]:idx[3]]
		var args string
		if idx[4] >= 0 {
			args = source[idx[4]:idx[5]]
		}
		fnName := source[idx[6]:idx[7]]
		line := lineOf(source, idx[0])
		props := map[string]string{
			"framework":    "pydantic",
			"pattern_type": "model_validator",
			"decorator":    decorator,
			"validator_fn": fnName,
			"dialect":      validatorDialect(decorator, dialect),
		}
		if m := pydModeKwargRe.FindStringSubmatch(args); m != nil {
			props["mode"] = m[1]
		}
		out = append(out, entity("validate_"+fnName, "SCOPE.Pattern", "", file.Path, line, props))
	}

	// 3. Field(...) constraints.
	for _, idx := range allMatchesIndex(pydFieldRe, source) {
		fieldName := source[idx[2]:idx[3]]
		args := source[idx[4]:idx[5]]
		constraints := extractConstraints(args)
		if len(constraints) == 0 {
			continue // a bare Field(...) / Field(default=...) carries no constraint.
		}
		line := lineOf(source, idx[0])
		props := map[string]string{
			"framework":    "pydantic",
			"pattern_type": "constraint",
			"field":        fieldName,
			"dialect":      dialect,
		}
		for k, v := range constraints {
			props["constraint_"+k] = v
		}
		out = append(out, entity("constraint_"+fieldName, "SCOPE.Pattern", "", file.Path, line, props))
	}

	// 4. Model config / coercion recognition. v2 ConfigDict and v1 inner
	// `class Config:` can both appear in one file (different models), so emit
	// every occurrence rather than just the first.
	for _, m := range allMatchesIndex(pydModelConfigRe, source) {
		args := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		out = append(out, configEntity("v2", "model_config", args, file.Path, line))
	}
	for _, m := range allMatchesIndex(pydConfigClassRe, source) {
		// v1 inner `class Config:` — capture the flags from the indented body.
		body := configClassBody(source, m[1])
		line := lineOf(source, m[0])
		out = append(out, configEntity("v1", "Config", body, file.Path, line))
	}

	// 5. Field-as-member sub-entities (issue #4613). Each `class X(BaseModel)`
	// model's annotated fields become `SCOPE.Schema`/field child entities with a
	// CONTAINS edge from the model — parity with the JS/TS class-validator DTO
	// field members (#4635) so cross-framework request/response FIELD-level diffs
	// work. The owner class node itself is emitted by the base Python extractor;
	// here we hang the CONTAINS membership off a lightweight owner carrier so the
	// edge binds to the real class via the `Class:<name>` byName fallback.
	for _, idx := range allMatchesIndex(pydModelClassRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		body := pydModelBody(source, idx[0])
		fields := extractPydanticModelFields(body)
		if len(fields) == 0 {
			continue
		}
		// Each field child carries its own CONTAINS edge (FromID `Class:<name>`
		// resolves to the base-extractor model node). We do NOT emit a class-named
		// carrier entity (issue #1501 discipline).
		out = append(out, emitPyDTOFieldMembers(className, fields, "pydantic", file.Path, line, nil)...)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// pydModelBody returns the indented body of a Pydantic model class starting at
// the class header byte offset, up to the first dedent. Mirrors the Django
// extractClassBody scan but is local to keep the pydantic extractor standalone.
func pydModelBody(source string, classStart int) string {
	lines := strings.Split(source[classStart:], "\n")
	if len(lines) == 0 {
		return ""
	}
	classIndent := len(lines[0]) - len(strings.TrimLeft(lines[0], " \t"))
	var body []string
	for i, ln := range lines {
		if i == 0 {
			continue
		}
		stripped := strings.TrimSpace(ln)
		if stripped == "" {
			body = append(body, ln)
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " \t"))
		if indent <= classIndent {
			break
		}
		body = append(body, ln)
	}
	return strings.Join(body, "\n")
}

// configEntity builds the model-config / coercion pattern entity from a config
// argument/body blob, recording any recognized coercion-affecting flags.
func configEntity(configDialect, configForm, blob, path string, line int) types.EntityRecord {
	props := map[string]string{
		"framework":    "pydantic",
		"pattern_type": "model_config",
		"config_form":  configForm,
		"dialect":      configDialect,
	}
	var flags []string
	for _, k := range pydCoercionConfigKeys {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(k) + `\s*=`).MatchString(blob) {
			flags = append(flags, k)
		}
	}
	if len(flags) > 0 {
		props["coercion_flags"] = strings.Join(flags, ",")
	}
	return entity("model_config_"+configForm, "SCOPE.Pattern", "", path, line, props)
}

// extractConstraints pulls recognized Field() keyword constraints into a map.
func extractConstraints(args string) map[string]string {
	out := map[string]string{}
	for _, k := range pydConstraintKeys {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(k) + `\s*=\s*([^,)]+)`)
		if m := re.FindStringSubmatch(args); m != nil {
			out[k] = strings.TrimSpace(m[1])
		}
	}
	return out
}

// quotedArgs returns the positional quoted string literals in a decorator
// argument blob, in source order. It stops at the first keyword argument
// (a top-level `name=`), so `'a', 'b', mode='before'` yields ["a","b"] and not
// "before". Used to recover the field names a validator targets.
func quotedArgs(args string) []string {
	// Truncate at the first keyword argument: a bareword identifier followed by
	// "=" that is not "==". Positional string args always precede kwargs in a
	// decorator call.
	if loc := pydKwargStartRe.FindStringIndex(args); loc != nil {
		args = args[:loc[0]]
	}
	var out []string
	for _, m := range pydFirstQuotedRe.FindAllStringSubmatch(args, -1) {
		out = append(out, m[1])
	}
	return out
}

// configClassBody returns the indented body following a `class Config:` header
// starting at byte offset start, up to the first dedent (a non-blank line whose
// indentation is <= the class header's). Best-effort, regex-free scan.
func configClassBody(source string, start int) string {
	rest := source[start:]
	lines := strings.Split(rest, "\n")
	var body []string
	for i, ln := range lines {
		if i == 0 {
			continue // the remainder of the `class Config:` line
		}
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		// First indented body line establishes membership; a line with no
		// leading whitespace marks the dedent that ends the inner class.
		if ln == trimmed {
			break
		}
		body = append(body, ln)
	}
	return strings.Join(body, "\n")
}

// pydanticReferenced reports whether the source references Pydantic at all.
func pydanticReferenced(source string) bool {
	return strings.Contains(source, "pydantic") ||
		strings.Contains(source, "BaseModel") ||
		strings.Contains(source, "BaseSettings") ||
		strings.Contains(source, "field_validator") ||
		strings.Contains(source, "ConfigDict")
}

// pydanticDialect infers the dominant Pydantic major version from the source.
// v2 markers (field_validator, model_validator, ConfigDict, pattern=) win over
// v1 markers; ambiguous files report "unknown".
func pydanticDialect(source string) string {
	v2 := strings.Contains(source, "field_validator") ||
		strings.Contains(source, "model_validator") ||
		strings.Contains(source, "ConfigDict") ||
		strings.Contains(source, "model_config")
	v1 := strings.Contains(source, "@validator") ||
		strings.Contains(source, "@root_validator") ||
		regexp.MustCompile(`(?m)^[ \t]+class\s+Config\s*:`).MatchString(source)
	switch {
	case v2 && !v1:
		return "v2"
	case v1 && !v2:
		return "v1"
	case v2 && v1:
		return "mixed"
	default:
		return "unknown"
	}
}

// validatorDialect maps a specific decorator to its Pydantic dialect, falling
// back to the file-level dialect when the decorator is version-neutral.
func validatorDialect(decorator, fileDialect string) string {
	switch decorator {
	case "field_validator", "model_validator":
		return "v2"
	case "validator", "root_validator":
		return "v1"
	default:
		return fileDialect
	}
}
