package golang

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// dto_field_members.go — Go struct-tag DTO FIELD-as-member indexing (issue
// #4715), generalizing the uniform DTO field-member model from the JS
// (javascript/validation_schema.go::emitSchemaFieldMembers, #4635), Python
// (python/dto_field_members.go, #4613) and Java (java/spring_dto_fields.go,
// #4613) emitters.
//
// Each request/response DTO struct's fields become `SCOPE.Schema` subtype=field
// sub-entities named `<Struct>.<field>`, carrying the SAME property shape as the
// other frameworks so the cross-framework field-level diff tools + the dashboard
// /shape ShapeTree resolver treat all frameworks uniformly:
//
//	field_name   — the wire name (json tag name when present, else the Go field)
//	field_type   — normalized scalar/declared type ("string"/"integer"/...)
//	parent_class — the owning DTO struct name
//	optional     — "true" when the field is optional (omitempty / pointer /
//	               no `required` validate/binding rule)
//	validators   — space-joined `@rule` markers from validate:/binding: tags
//	provenance   — INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP
//	library      — "go-struct-tags"
//
// The child's Signature is the Java-style `[@Validators ...] <type> <name>` so
// the shape resolver's parseFieldSignature recovers (annotations, type, name)
// exactly as it does for the JS/Java/Python DTO fields. A CONTAINS edge binds
// each field to its owner struct via the `Class:<Struct>` byName fallback.

// goScalarKind normalizes a Go type token to a scalar type, parity with the
// JS/Python scalar maps.
var goScalarKind = map[string]string{
	"string": "string", "rune": "string", "byte": "string",
	"int": "integer", "int8": "integer", "int16": "integer", "int32": "integer",
	"int64": "integer", "uint": "integer", "uint8": "integer", "uint16": "integer",
	"uint32": "integer", "uint64": "integer", "uintptr": "integer",
	"float32": "number", "float64": "number",
	"complex64": "number", "complex128": "number",
	"bool":      "boolean",
	"time.Time": "date", "Time": "date",
	"interface{}": "any", "any": "any",
}

// normalizeGoType strips pointer/slice/map markers and returns a normalized
// scalar type. A slice `[]T` → "array"; a map `map[K]V` → "object"; otherwise the
// element type is normalized via goScalarKind (falling back to the bare token).
func normalizeGoType(typ string) string {
	typ = strings.TrimSpace(typ)
	if strings.HasPrefix(typ, "map[") {
		return "object"
	}
	if strings.HasPrefix(typ, "[]") {
		return "array"
	}
	typ = strings.TrimPrefix(typ, "*")
	if k, ok := goScalarKind[typ]; ok {
		return k
	}
	// A dotted type like `pkg.Type` → its short name (best-effort).
	if i := strings.LastIndex(typ, "."); i >= 0 {
		short := typ[i+1:]
		if k, ok := goScalarKind[typ]; ok {
			return k
		}
		return short
	}
	return typ
}

// goTagValueRe extracts a `<key>:"<value>"` pair from a struct tag's raw body.
func goTagValue(tag, key string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `:"([^"]*)"`)
	if m := re.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

// goValidatorMarkers maps the comma-separated rules in a `validate:"..."` /
// `binding:"..."` tag value to `@rule` markers (parity with class-validator
// `@IsX`). Bare flags (`required`, `email`) and parameterized rules
// (`min=3`, `max=255`) are both surfaced; the rule name is the marker.
func goValidatorMarkers(ruleSpec string) []string {
	var out []string
	for _, raw := range strings.Split(ruleSpec, ",") {
		r := strings.TrimSpace(raw)
		if r == "" || r == "-" {
			continue
		}
		// `min=3` → marker `@min`; `oneof=a b c` → `@oneof`.
		name := r
		if i := strings.IndexAny(name, "= "); i >= 0 {
			name = name[:i]
		}
		if name == "" {
			continue
		}
		out = append(out, "@"+name)
	}
	return out
}

// goDTORequired reports whether a struct field is required, given its parsed
// validate/binding rule list. A field is required when its rules contain a bare
// `required` rule.
func goDTORequired(rules []string) bool {
	for _, r := range rules {
		if strings.TrimSpace(r) == "required" {
			return true
		}
	}
	return false
}

// emitGoDTOFieldMembers emits one `SCOPE.Schema`/field sub-entity per field of a
// DTO struct, plus a CONTAINS edge from the owning struct to each child. Mirrors
// the JS/Python/Java field-membership model so cross-framework FIELD-level diffs
// are uniform. The owner struct node is `Class:<Struct>` (resolved via the
// byName fallback); each child carries its own CONTAINS membership edge.
func emitGoDTOFieldMembers(
	structName string,
	fields []dtoField,
	filePath, language string,
	ownerLine int,
) []types.EntityRecord {
	if structName == "" || len(fields) == 0 {
		return nil
	}
	const library = "go-struct-tags"
	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, f := range fields {
		// Embedded base structs (gorm.Model) and skip-marked fields aren't DTO
		// wire fields; the json tag `-` excludes a field from serialization.
		if f.Name == "" || f.Name == "struct" {
			continue
		}

		jsonTag := goTagValue(f.Tag, "json")
		wireName := f.Name
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] == "-" {
				continue // explicitly excluded from the wire shape
			}
			if parts[0] != "" {
				wireName = parts[0]
			}
		}
		if seen[wireName] {
			continue
		}
		seen[wireName] = true

		// Rules come from validate: or binding: (gin) tags.
		ruleSpec := goTagValue(f.Tag, "validate")
		if ruleSpec == "" {
			ruleSpec = goTagValue(f.Tag, "binding")
		}
		var ruleList []string
		for _, r := range strings.Split(ruleSpec, ",") {
			if r = strings.TrimSpace(r); r != "" {
				ruleList = append(ruleList, r)
			}
		}
		validators := goValidatorMarkers(ruleSpec)

		// Optionality: a field is optional unless it carries a `required` rule.
		// A pointer type or json `omitempty` independently marks it optional.
		required := goDTORequired(ruleList)
		optional := !required
		if strings.HasPrefix(strings.TrimSpace(f.Type), "*") {
			optional = true
		}
		if jsonTag != "" && strings.Contains(jsonTag, "omitempty") {
			optional = true
		}

		typ := normalizeGoType(f.Type)
		if typ == "" {
			typ = "unknown"
		}

		// Java-style field signature: `[@Ann ...] Type name`.
		var sb strings.Builder
		for _, a := range validators {
			sb.WriteString(a)
			sb.WriteByte(' ')
		}
		sb.WriteString(typ)
		sb.WriteByte(' ')
		sb.WriteString(wireName)

		childName := structName + "." + wireName
		child := makeEntity(childName, "SCOPE.Schema", "field", filePath, language, ownerLine)
		child.Signature = sb.String()
		setProps(&child,
			"library", library,
			"pattern_type", "field",
			"field_name", wireName,
			"field_type", typ,
			"parent_class", structName,
			"provenance", "INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP")
		if optional {
			setProps(&child, "optional", "true")
		}
		if len(validators) > 0 {
			setProps(&child, "validators", strings.Join(validators, " "))
		}
		child.Relationships = append(child.Relationships,
			containsFieldEdge(structName, child.ID, wireName, library))
		out = append(out, child)
	}
	return out
}
