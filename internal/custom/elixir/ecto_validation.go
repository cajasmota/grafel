// ecto_validation.go — deep Ecto changeset validation + cast (DTO) extraction.
//
// Raises lang.elixir.framework.phoenix Validation/dto_extraction and
// Validation/request_validation from partial to full (TS/JS bar parity).
//
// Part of issue #3470 (epic #3467).
//
// In Elixir the request DTO IS the Ecto changeset-cast struct: the cast/3
// field list enumerates the accepted (permitted) fields, and the chained
// validate_* / *_constraint calls describe per-field validation rules.
//
// Two deep passes, mirroring the Rails deep model (internal/custom/ruby):
//
//  1. Per-field cast entities (dto_extraction):
//     cast(attrs, [:name, :email, :age])
//     → ecto_cast_field:name  (props: field=name, cast_type=scalar)
//     → ecto_cast_field:email (props: field=email, cast_type=scalar)
//     → ecto_cast_field:age   (props: field=age, cast_type=scalar)
//     The schema `field :name, :string` declarations are used to enrich each
//     cast field with its declared Ecto type when present (field_type prop).
//
//  2. Per-field validation entities (request_validation):
//     validate_required([:name, :email]) → ecto_val:name:required,
//     ecto_val:email:required
//     validate_format(:email, ~r/@/)     → ecto_val:email:format (regex=~r/@/)
//     validate_length(:name, min: 1, max: 20)
//     → ecto_val:name:length (bound=min:1,max:20)
//     validate_number(:age, greater_than: 0)
//     → ecto_val:age:number (bound=greater_than:0)
//     validate_inclusion(:role, ["admin","user"])
//     → ecto_val:role:inclusion (bound=...)
//     validate_exclusion / validate_subset / validate_confirmation likewise.
//     unique_constraint(:email)          → ecto_val:email:unique_constraint
//     foreign_key_constraint(:user_id)   → ecto_val:user_id:foreign_key_constraint
//     check_constraint(:age, ...)        → ecto_val:age:check_constraint
//
// Collision safety: all package-level symbols added here are prefixed
// ectoVal/reEctoVal to avoid clashing with names in ecto.go.
package elixir

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

var (
	// cast(<source>, [:a, :b, :c]) — capture the field-list argument.
	// Source may be `attrs`, `params`, `%{...}` etc.; we only need the list.
	reEctoValCast = regexp.MustCompile(
		`(?ms)\bcast\s*\(\s*[^,]+,\s*\[([^\]]*)\]`,
	)

	// schema "x" do … field :name, :type … — capture field name + declared type.
	reEctoValSchemaField = regexp.MustCompile(
		`(?m)^\s*field\s+:([a-z_][a-zA-Z0-9_]*)\s*,\s*:([a-z_][a-zA-Z0-9_]*)`,
	)

	// validate_required([:a, :b]) or validate_required(:a) — capture arg blob.
	reEctoValRequired = regexp.MustCompile(
		`(?m)\bvalidate_required\s*\(\s*(\[[^\]]*\]|:[a-z_][a-zA-Z0-9_]*)`,
	)

	// validate_format(:field, ~r/regex/[flags]) — capture field + regex literal.
	reEctoValFormat = regexp.MustCompile(
		`(?m)\bvalidate_format\s*\(\s*:([a-z_][a-zA-Z0-9_]*)\s*,\s*(~r/[^/]*/[a-z]*|~r\{[^}]*\}[a-z]*)`,
	)

	// validate_length(:field, min: 1, max: 20) — capture field + option tail.
	reEctoValLength = regexp.MustCompile(
		`(?m)\bvalidate_length\s*\(\s*:([a-z_][a-zA-Z0-9_]*)\s*,\s*([^)]*)\)`,
	)

	// validate_number(:field, greater_than: 0) — capture field + option tail.
	reEctoValNumber = regexp.MustCompile(
		`(?m)\bvalidate_number\s*\(\s*:([a-z_][a-zA-Z0-9_]*)\s*,\s*([^)]*)\)`,
	)

	// validate_inclusion/exclusion/subset(:field, [...]) — capture kind + field + set.
	reEctoValSetMember = regexp.MustCompile(
		`(?m)\bvalidate_(inclusion|exclusion|subset)\s*\(\s*:([a-z_][a-zA-Z0-9_]*)\s*,\s*(\[[^\]]*\]|[A-Za-z_][\w.]*)`,
	)

	// validate_confirmation(:field) — capture field.
	reEctoValConfirmation = regexp.MustCompile(
		`(?m)\bvalidate_confirmation\s*\(\s*:([a-z_][a-zA-Z0-9_]*)`,
	)

	// validate_acceptance(:field) — capture field.
	reEctoValAcceptance = regexp.MustCompile(
		`(?m)\bvalidate_acceptance\s*\(\s*:([a-z_][a-zA-Z0-9_]*)`,
	)

	// *_constraint(:field[, ...]) — unique/foreign_key/check/exclusion constraints.
	reEctoValConstraint = regexp.MustCompile(
		`(?m)\b(unique|foreign_key|check|exclusion|no_assoc|assoc)_constraint\s*\(\s*:([a-z_][a-zA-Z0-9_]*)`,
	)

	// A bare :symbol (used to split a field list).
	reEctoValSymbol = regexp.MustCompile(`:([a-z_][a-zA-Z0-9_]*)`)
)

// ectoValDeepExtract appends deep Ecto cast (DTO) and changeset validation
// entities to the provided accumulator. It is called from ectoExtractor.Extract
// after the structural passes so both entity sets coexist.
func ectoValDeepExtract(src, filePath, language string, add func(types.EntityRecord)) {
	fieldTypes := ectoValSchemaFieldTypes(src)
	ectoValParseCast(src, filePath, language, fieldTypes, add)
	ectoValParseValidations(src, filePath, language, add)
}

// ectoValSchemaFieldTypes maps schema field name → declared Ecto type, so cast
// DTO fields can be enriched with their type when the schema is in the same file.
func ectoValSchemaFieldTypes(src string) map[string]string {
	out := map[string]string{}
	for _, m := range reEctoValSchemaField.FindAllStringSubmatch(src, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// ---------------------------------------------------------------------------
// 1. cast(attrs, [:a, :b]) → per-field DTO entities
// ---------------------------------------------------------------------------

func ectoValParseCast(src, filePath, language string, fieldTypes map[string]string, add func(types.EntityRecord)) {
	for _, idx := range reEctoValCast.FindAllStringSubmatchIndex(src, -1) {
		listBlob := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		for _, fm := range reEctoValSymbol.FindAllStringSubmatch(listBlob, -1) {
			field := fm[1]
			name := "ecto_cast_field:" + field
			ent := makeEntity(name, "SCOPE.Pattern", "dto_extraction", filePath, language, ln)
			setProps(&ent,
				"framework", "ecto",
				"provenance", "DEEP_ECTO_CAST",
				"signal", "dto",
				"field", field,
				"cast_type", "scalar",
			)
			if ft, ok := fieldTypes[field]; ok {
				ent.Properties["field_type"] = ft
			}
			add(ent)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. validate_* / *_constraint → per-field validation entities
// ---------------------------------------------------------------------------

func ectoValParseValidations(src, filePath, language string, add func(types.EntityRecord)) {
	emit := func(field, validator string, lineNum int, props ...string) {
		name := "ecto_val:" + field + ":" + validator
		ent := makeEntity(name, "SCOPE.Pattern", "request_validation", filePath, language, lineNum)
		setProps(&ent,
			"framework", "ecto",
			"provenance", "DEEP_ECTO_VALIDATE",
			"signal", "validation",
			"field", field,
			"validator", validator,
		)
		setProps(&ent, props...)
		add(ent)
	}

	// validate_required([:a, :b]) / validate_required(:a)
	for _, idx := range reEctoValRequired.FindAllStringSubmatchIndex(src, -1) {
		blob := src[idx[2]:idx[3]]
		lineNum := lineOf(src, idx[0])
		for _, fm := range reEctoValSymbol.FindAllStringSubmatch(blob, -1) {
			emit(fm[1], "required", lineNum)
		}
	}

	// validate_format(:email, ~r/@/)
	for _, idx := range reEctoValFormat.FindAllStringSubmatchIndex(src, -1) {
		field := src[idx[2]:idx[3]]
		regex := src[idx[4]:idx[5]]
		emit(field, "format", lineOf(src, idx[0]), "regex", regex)
	}

	// validate_length(:name, min: 1, max: 20)
	for _, idx := range reEctoValLength.FindAllStringSubmatchIndex(src, -1) {
		field := src[idx[2]:idx[3]]
		bound := ectoValNormalizeOpts(src[idx[4]:idx[5]])
		emit(field, "length", lineOf(src, idx[0]), "bound", bound)
	}

	// validate_number(:age, greater_than: 0)
	for _, idx := range reEctoValNumber.FindAllStringSubmatchIndex(src, -1) {
		field := src[idx[2]:idx[3]]
		bound := ectoValNormalizeOpts(src[idx[4]:idx[5]])
		emit(field, "number", lineOf(src, idx[0]), "bound", bound)
	}

	// validate_inclusion/exclusion/subset(:role, [...])
	for _, idx := range reEctoValSetMember.FindAllStringSubmatchIndex(src, -1) {
		kind := src[idx[2]:idx[3]]
		field := src[idx[4]:idx[5]]
		set := strings.TrimSpace(src[idx[6]:idx[7]])
		emit(field, kind, lineOf(src, idx[0]), "bound", set)
	}

	// validate_confirmation(:password)
	for _, idx := range reEctoValConfirmation.FindAllStringSubmatchIndex(src, -1) {
		emit(src[idx[2]:idx[3]], "confirmation", lineOf(src, idx[0]))
	}

	// validate_acceptance(:terms)
	for _, idx := range reEctoValAcceptance.FindAllStringSubmatchIndex(src, -1) {
		emit(src[idx[2]:idx[3]], "acceptance", lineOf(src, idx[0]))
	}

	// unique_constraint/foreign_key_constraint/check_constraint(:field)
	for _, idx := range reEctoValConstraint.FindAllStringSubmatchIndex(src, -1) {
		kind := src[idx[2]:idx[3]] + "_constraint"
		field := src[idx[4]:idx[5]]
		emit(field, kind, lineOf(src, idx[0]))
	}
}

// ectoValNormalizeOpts collapses an option tail like " min: 1, max: 20 " into a
// stable comma-joined "min:1,max:20" form for the bound property.
func ectoValNormalizeOpts(tail string) string {
	parts := strings.Split(tail, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// "min: 1" → "min:1"
		p = strings.ReplaceAll(p, ": ", ":")
		p = strings.Join(strings.Fields(p), "")
		out = append(out, p)
	}
	return strings.Join(out, ",")
}
