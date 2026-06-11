package python

import (
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// dto_field_members.go — Python DTO/serializer FIELD-as-member indexing,
// generalizing the NestJS / JS-TS model (issue #4635,
// javascript/validation_schema.go::emitSchemaFieldMembers) to Pydantic models
// and DRF serializers (issue #4613).
//
// Each request/query/response DTO's fields become `SCOPE.Schema` subtype=field
// sub-entities named `<Owner>.<field>`, carrying the SAME property shape as the
// JS field members so the cross-framework field-level diff tools + the
// dashboard /shape ShapeTree resolver treat all frameworks uniformly:
//
//	field_name   — the field's name
//	field_type   — normalized scalar/declared type ("string"/"integer"/...)
//	parent_class — the owning DTO/serializer class name
//	optional     — "true" when the field is optional / not required
//	validators   — space-joined constraint/validator markers (when any)
//	provenance   — INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP
//	library      — "pydantic" / "drf"
//
// The child's Signature is the Java-style `[@Validators ...] <type> <name>` so
// the shape resolver's parseFieldSignature recovers (annotations, type, name)
// exactly as it does for the JS/Java DTO fields. A CONTAINS edge binds each
// field to its owner. This file owns ONLY the field-member emission; the owner
// class entities are emitted by pydantic.go / django.go.

// pyDTOField is a captured DTO/serializer field: name, normalized type,
// validators/constraints, and optionality.
type pyDTOField struct {
	name       string
	typ        string
	validators []string
	optional   bool
}

// emitPyDTOFieldMembers emits one `SCOPE.Schema`/field sub-entity per field of a
// DTO/serializer. Each child carries its own CONTAINS membership edge whose
// FromID resolves to the owning class via the `Class:<owner>` byName fallback
// (see containsFieldEdge) — so NO class-named carrier entity is emitted (issue
// #1501 discipline). When ownerRels is non-nil the edge is ALSO appended there
// (used by callers that already own the serializer entity and want the edge on
// it instead); when nil, the edge lives only on the child. Mirrors the JS/Java
// field-membership model (#4635/#4367).
func emitPyDTOFieldMembers(
	ownerClass string,
	fields []pyDTOField,
	library, filePath string,
	ownerLine int,
	ownerRels *[]types.RelationshipRecord,
) []types.EntityRecord {
	if ownerClass == "" || len(fields) == 0 {
		return nil
	}
	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, f := range fields {
		if f.name == "" || seen[f.name] {
			continue
		}
		seen[f.name] = true

		annots := append([]string(nil), f.validators...)
		typ := f.typ
		if typ == "" {
			typ = "unknown"
		}

		// Java-style field signature: `[@Ann ...] Type name`.
		var sb strings.Builder
		for _, a := range annots {
			sb.WriteString(a)
			sb.WriteByte(' ')
		}
		sb.WriteString(typ)
		sb.WriteByte(' ')
		sb.WriteString(f.name)

		childName := ownerClass + "." + f.name
		props := map[string]string{
			"kind":         "SCOPE.Schema",
			"subtype":      "field",
			"library":      library,
			"pattern_type": "field",
			"field_name":   f.name,
			"field_type":   typ,
			"parent_class": ownerClass,
			"provenance":   "INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP",
		}
		if f.optional {
			props["optional"] = "true"
		}
		if len(annots) > 0 {
			props["validators"] = strings.Join(annots, " ")
		}
		child := entity(childName, "SCOPE.Schema", "field", filePath, ownerLine, props)
		child.Signature = sb.String()
		edge := containsFieldEdge(ownerClass, childName, f.name, library)
		if ownerRels != nil {
			// Caller owns the serializer entity and wants the edge on it.
			*ownerRels = append(*ownerRels, edge)
		} else {
			// No class carrier (Pydantic / DRF): the child carries its own
			// membership edge; FromID `Class:<owner>` resolves to the real class.
			child.Relationships = append(child.Relationships, edge)
		}
		out = append(out, child)
	}
	return out
}

// ── Pydantic model field parsing ─────────────────────────────────────────────

// pydModelFieldRe matches a Pydantic model body field declaration. Group 1 =
// leading indent, group 2 = field name, group 3 = the type annotation (up to
// `=` or EOL), group 4 = the RHS default expression (may be empty). Only
// annotated assignments are fields; bare `x = 1` class vars without an
// annotation are not Pydantic fields. The captured indent lets the caller keep
// only the model's direct-body fields (skip method-local annotated locals).
var pydModelFieldRe = regexp.MustCompile(
	`(?m)^([ \t]+)([a-zA-Z_]\w*)\s*:\s*([^=\n]+?)\s*(?:=\s*(.+))?$`)

// pyOptionalTypeRe detects `Optional[...]` / `... | None` / `Union[..., None]`.
var pyOptionalTypeRe = regexp.MustCompile(`(?:^|[\[,\s])None(?:[\],\s]|$)`)

// extractPydanticModelFields parses the body of a Pydantic model class into its
// field members. `bodyOffsetLine` is the line of the class header so child
// entities can be attributed near the model.
func extractPydanticModelFields(body string) []pyDTOField {
	var fields []pyDTOField
	seen := make(map[string]bool)
	// Determine the model's direct-body indent (the minimal indent of a matched
	// declaration) so deeper-indented method-local annotated assignments are not
	// mistaken for model fields.
	baseIndent := -1
	for _, m := range pydModelFieldRe.FindAllStringSubmatch(body, -1) {
		if n := len(m[1]); baseIndent < 0 || n < baseIndent {
			baseIndent = n
		}
	}
	for _, m := range pydModelFieldRe.FindAllStringSubmatch(body, -1) {
		if len(m[1]) != baseIndent {
			continue // not a direct model-body field (nested in a method/inner class)
		}
		name := m[2]
		annot := strings.TrimSpace(m[3])
		rhs := strings.TrimSpace(m[4])
		if name == "" || seen[name] {
			continue
		}
		// Skip the inner `class Config:` / `model_config` / method defs and
		// dunder declarations — these are not model fields.
		if name == "model_config" || strings.HasPrefix(name, "__") {
			continue
		}
		// A reserved-word annotation (e.g. the line was actually `def f(self):`)
		// will not reach here because the regex requires `name : type`.
		seen[name] = true

		optional := pydIsOptional(annot)
		var validators []string
		// A field with no `= ...` default and not Optional is required.
		if rhs == "" && !optional {
			// required — no explicit marker; leave validators empty.
		} else if rhs != "" && strings.HasPrefix(rhs, "Field(") {
			// Field(...) — capture recognized constraints as validator markers and
			// detect `default=`/`...` (Ellipsis = required).
			validators = pydFieldConstraintMarkers(rhs)
			if strings.Contains(rhs, "Field(...)") || regexp.MustCompile(`Field\(\s*\.\.\.`).MatchString(rhs) {
				// required sentinel
			} else {
				optional = true // has a default
			}
		} else if rhs != "" {
			// Has a literal default value → optional.
			optional = true
		}

		fields = append(fields, pyDTOField{
			name:       name,
			typ:        normalizePyType(annot),
			validators: validators,
			optional:   optional,
		})
	}
	return fields
}

// pydIsOptional reports whether a type annotation denotes an optional field.
func pydIsOptional(annot string) bool {
	if strings.HasPrefix(annot, "Optional[") || strings.HasPrefix(annot, "typing.Optional[") {
		return true
	}
	// `X | None` or `Union[X, None]`.
	if strings.Contains(annot, "|") && pyOptionalTypeRe.MatchString(annot) {
		return true
	}
	if strings.HasPrefix(annot, "Union[") && pyOptionalTypeRe.MatchString(annot) {
		return true
	}
	return false
}

// pydConstraintMarkerKeys are Field() kwargs surfaced as validator markers on a
// field member (parity with class-validator `@IsX`).
var pydConstraintMarkerKeys = []string{
	"gt", "ge", "lt", "le", "min_length", "max_length",
	"min_items", "max_items", "multiple_of", "max_digits",
	"decimal_places", "pattern", "regex",
}

// pydFieldConstraintMarkers maps recognized Field() constraints to `@key`
// markers, in deterministic order.
func pydFieldConstraintMarkers(rhs string) []string {
	var out []string
	for _, k := range pydConstraintMarkerKeys {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(k) + `\s*=`).MatchString(rhs) {
			out = append(out, "@"+k)
		}
	}
	return out
}

// pyScalarKind normalizes a Python type token to a scalar type, parity with the
// JS schemaScalarKind map.
var pyScalarKind = map[string]string{
	"str": "string", "string": "string",
	"int": "integer", "integer": "integer",
	"float": "number", "Decimal": "number", "complex": "number",
	"bool": "boolean", "boolean": "boolean",
	"datetime": "date", "date": "date", "time": "date",
	"list": "array", "List": "array", "tuple": "array", "set": "array",
	"dict": "object", "Dict": "object",
	"bytes": "string", "UUID": "string", "EmailStr": "string",
	"Any": "any", "None": "null",
}

// normalizePyType strips Optional/Union wrappers and generics, returning a
// normalized scalar type.
func normalizePyType(annot string) string {
	annot = strings.TrimSpace(annot)
	// Unwrap Optional[X] / typing.Optional[X].
	if i := strings.Index(annot, "Optional["); i >= 0 {
		inner := annot[i+len("Optional["):]
		if j := strings.LastIndex(inner, "]"); j >= 0 {
			inner = inner[:j]
		}
		annot = strings.TrimSpace(inner)
	}
	// `X | None` → X.
	if strings.Contains(annot, "|") {
		parts := strings.Split(annot, "|")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "None" && p != "" {
				annot = p
				break
			}
		}
	}
	// Strip generic subscripts: List[str] → list, Dict[...] → dict.
	if i := strings.IndexAny(annot, "[ "); i >= 0 {
		annot = annot[:i]
	}
	// Strip dotted path: typing.List → List.
	if i := strings.LastIndex(annot, "."); i >= 0 {
		annot = annot[i+1:]
	}
	if k, ok := pyScalarKind[annot]; ok {
		return k
	}
	return annot
}

// ── marshmallow Schema field parsing (#4714) ─────────────────────────────────

// mmDTOFieldRe matches a marshmallow declared field inside a Schema body:
//
//	name = fields.Str(required=True, allow_none=False)
//	email = ma.fields.Email()
//	items = fields.Nested(ItemSchema, many=True)
//
// Group 1 = field name, group 2 = the marshmallow field class (e.g. Str, Email,
// Nested), group 3 = the (possibly truncated at first `)`) argument blob.
var mmDTOFieldRe = regexp.MustCompile(
	`(?m)^[ \t]+(\w+)\s*=\s*(?:\w+\.)?fields\.(\w+)\s*\(([^)]*)`)

// mmFieldTypeKind maps a marshmallow field class to a normalized scalar type
// (parity with the JS/DRF scalar maps).
var mmFieldTypeKind = map[string]string{
	"Str": "string", "String": "string", "Email": "string", "URL": "string",
	"Url": "string", "UUID": "string", "Uuid": "string", "Constant": "string",
	"Int": "integer", "Integer": "integer",
	"Float": "number", "Decimal": "number", "Number": "number",
	"Bool": "boolean", "Boolean": "boolean",
	"Date": "date", "DateTime": "date", "Time": "date", "NaiveDateTime": "date",
	"AwareDateTime": "date", "TimeDelta": "date",
	"List": "array", "Tuple": "array",
	"Dict": "object", "Mapping": "object", "Nested": "object", "Pluck": "object",
	"Raw": "any", "Field": "any", "Function": "any", "Method": "any",
}

// mmFieldType normalizes a marshmallow field class name to a scalar type.
func mmFieldType(fieldClass string) string {
	if k, ok := mmFieldTypeKind[fieldClass]; ok {
		return k
	}
	return strings.ToLower(fieldClass)
}

// extractMarshmallowSchemaFields parses a marshmallow Schema body into field
// members. A field is optional when `required=True` is absent, or when
// `allow_none=True` / `load_default=` / `missing=` / `dump_only=True` is set.
func extractMarshmallowSchemaFields(body string) []pyDTOField {
	var fields []pyDTOField
	seen := make(map[string]bool)
	for _, m := range mmDTOFieldRe.FindAllStringSubmatch(body, -1) {
		name := m[1]
		fieldClass := m[2]
		args := m[3]
		if name == "" || seen[name] || strings.HasPrefix(name, "__") {
			continue
		}
		seen[name] = true

		required := regexp.MustCompile(`\brequired\s*=\s*True`).MatchString(args)
		optional := !required
		if regexp.MustCompile(`\ballow_none\s*=\s*True`).MatchString(args) ||
			regexp.MustCompile(`\bdump_only\s*=\s*True`).MatchString(args) ||
			regexp.MustCompile(`\b(?:load_default|missing|default)\s*=`).MatchString(args) {
			optional = true
		}

		var validators []string
		if required {
			validators = append(validators, "@required")
		}
		for _, kw := range []string{"allow_none", "load_only", "dump_only",
			"validate", "data_key"} {
			if regexp.MustCompile(`\b` + kw + `\s*=`).MatchString(args) {
				validators = append(validators, "@"+kw)
			}
		}

		fields = append(fields, pyDTOField{
			name:       name,
			typ:        mmFieldType(fieldClass),
			validators: validators,
			optional:   optional,
		})
	}
	return fields
}

// ── attrs / dataclasses field parsing (#4714) ────────────────────────────────

// dcAnnotatedFieldRe matches an annotated class-body attribute, the shape both
// dataclasses and attrs-with-annotations use:
//
//	name: str
//	age: int = 0
//	tags: list[str] = field(default_factory=list)
//	nickname: Optional[str] = None
//	count: int = attr.ib(default=0)
//
// Group 1 = leading indent, group 2 = name, group 3 = type annotation, group 4
// = RHS default expression (may be empty). Shares the direct-body-indent
// discipline with the Pydantic field regex.
var dcAnnotatedFieldRe = regexp.MustCompile(
	`(?m)^([ \t]+)([a-zA-Z_]\w*)\s*:\s*([^=\n]+?)\s*(?:=\s*(.+))?$`)

// extractAttrsDataclassFields parses a @dataclass / @attr.s / @define class body
// into field members. A field is optional when it has any default — a literal,
// `field(default=...)` / `field(default_factory=...)`, `attr.ib(default=...)` /
// `attr.ib(factory=...)`, or an `Optional[...]` / `X | None` annotation.
func extractAttrsDataclassFields(body string) []pyDTOField {
	var fields []pyDTOField
	seen := make(map[string]bool)
	baseIndent := -1
	for _, m := range dcAnnotatedFieldRe.FindAllStringSubmatch(body, -1) {
		if n := len(m[1]); baseIndent < 0 || n < baseIndent {
			baseIndent = n
		}
	}
	for _, m := range dcAnnotatedFieldRe.FindAllStringSubmatch(body, -1) {
		if len(m[1]) != baseIndent {
			continue // nested in a method / inner class — not a direct field
		}
		name := m[2]
		annot := strings.TrimSpace(m[3])
		rhs := strings.TrimSpace(m[4])
		if name == "" || seen[name] || strings.HasPrefix(name, "__") {
			continue
		}
		if name == "model_config" {
			continue
		}
		seen[name] = true

		optional := pydIsOptional(annot)
		var validators []string
		if rhs != "" {
			// Any RHS is a default → optional. Recognize field()/attr.ib() shapes
			// and surface their validator/constraint kwargs as markers.
			optional = true
			if regexp.MustCompile(`^(?:attr\.ib|attrib|attr\.attrib|field|attrs\.field)\s*\(`).MatchString(rhs) {
				// A field()/attr.ib() with no default and not Optional is required
				// unless it carries default=/factory=/default_factory=.
				hasDefault := regexp.MustCompile(`\b(?:default|factory|default_factory)\s*=`).MatchString(rhs)
				if !hasDefault && !pydIsOptional(annot) {
					optional = false
				}
				for _, kw := range []string{"validator", "converter"} {
					if regexp.MustCompile(`\b` + kw + `\s*=`).MatchString(rhs) {
						validators = append(validators, "@"+kw)
					}
				}
			}
		}

		fields = append(fields, pyDTOField{
			name:       name,
			typ:        normalizePyType(annot),
			validators: validators,
			optional:   optional,
		})
	}
	return fields
}

// ── DRF serializer field parsing ─────────────────────────────────────────────

// drfFieldDeclRe matches an explicit DRF serializer field declaration:
//
//	name = serializers.CharField(required=False, allow_null=True)
//	name = CharField(...)
//	items = ItemSerializer(many=True)
//
// Group 1 = field name, group 2 = the field-class callee (dotted allowed),
// group 3 = the call argument blob (best-effort, single-line / shallow).
var drfFieldDeclRe = regexp.MustCompile(
	`(?m)^[ \t]+(\w+)\s*=\s*([\w.]*(?:Field|Serializer))\s*\(([^)]*)`)

// drfFieldTypeKind maps a DRF field class to a normalized scalar type.
var drfFieldTypeKind = map[string]string{
	"CharField": "string", "EmailField": "string", "SlugField": "string",
	"URLField": "string", "UUIDField": "string", "RegexField": "string",
	"FilePathField": "string", "IPAddressField": "string",
	"IntegerField": "integer", "FloatField": "number", "DecimalField": "number",
	"BooleanField": "boolean", "NullBooleanField": "boolean",
	"DateField": "date", "DateTimeField": "date", "TimeField": "date",
	"DurationField": "date",
	"ListField": "array", "MultipleChoiceField": "array",
	"DictField": "object", "JSONField": "object", "HStoreField": "object",
	"ChoiceField": "enum",
	"SerializerMethodField": "any", "ReadOnlyField": "any", "HiddenField": "any",
	"PrimaryKeyRelatedField": "integer", "HyperlinkedRelatedField": "string",
	"SlugRelatedField": "string", "StringRelatedField": "string",
}

// drfFieldType normalizes a DRF field/serializer callee to a scalar type.
func drfFieldType(callee string) string {
	short := callee
	if i := strings.LastIndex(short, "."); i >= 0 {
		short = short[i+1:]
	}
	if k, ok := drfFieldTypeKind[short]; ok {
		return k
	}
	// A nested serializer field → its target shape is an object.
	if strings.HasSuffix(short, "Serializer") {
		return "object"
	}
	return strings.ToLower(strings.TrimSuffix(short, "Field"))
}

// extractDRFSerializerFields parses an explicit-declared DRF serializer body
// into field members. `required=False`, `allow_null=True`, `read_only=True`,
// and `default=` mark a field optional.
func extractDRFSerializerFields(body string) []pyDTOField {
	var fields []pyDTOField
	seen := make(map[string]bool)
	for _, m := range drfFieldDeclRe.FindAllStringSubmatch(body, -1) {
		name := m[1]
		callee := m[2]
		args := m[3]
		if name == "" || seen[name] || strings.HasPrefix(name, "__") {
			continue
		}
		seen[name] = true

		optional := false
		if regexp.MustCompile(`required\s*=\s*False`).MatchString(args) ||
			regexp.MustCompile(`allow_null\s*=\s*True`).MatchString(args) ||
			regexp.MustCompile(`read_only\s*=\s*True`).MatchString(args) ||
			regexp.MustCompile(`\bdefault\s*=`).MatchString(args) {
			optional = true
		}

		var validators []string
		for _, kw := range []string{"required", "allow_null", "read_only", "write_only",
			"max_length", "min_length", "max_value", "min_value"} {
			if regexp.MustCompile(`\b` + kw + `\s*=`).MatchString(args) {
				validators = append(validators, "@"+kw)
			}
		}

		fields = append(fields, pyDTOField{
			name:       name,
			typ:        drfFieldType(callee),
			validators: validators,
			optional:   optional,
		})
	}
	return fields
}

// ── DRF ModelSerializer Meta.fields parsing ──────────────────────────────────

// drfMetaFieldsListRe matches a `fields = [...]` / `fields = (...)` declaration
// inside a serializer's inner `class Meta:`. Group 1 = the bracketed list body.
var drfMetaFieldsListRe = regexp.MustCompile(
	`(?m)^[ \t]+fields\s*=\s*[\[\(]([^\]\)]*)[\]\)]`)

// drfMetaFieldsAllRe matches `fields = "__all__"` / `fields = '__all__'`.
var drfMetaFieldsAllRe = regexp.MustCompile(
	`(?m)^[ \t]+fields\s*=\s*["']__all__["']`)

// drfMetaResult carries the result of statically reading a ModelSerializer's
// Meta.fields. When isAll is true the field set is model-derived and not
// statically enumerable; names holds the explicit Meta list otherwise.
type drfMetaResult struct {
	names []string
	isAll bool
	found bool
}

// extractDRFMetaFields reads a serializer body's inner `class Meta:` `fields`
// declaration. `fields = "__all__"` → isAll (model-derived, unenumerated);
// `fields = [...]` → the enumerated name list.
func extractDRFMetaFields(body string) drfMetaResult {
	if drfMetaFieldsAllRe.MatchString(body) {
		return drfMetaResult{isAll: true, found: true}
	}
	if m := drfMetaFieldsListRe.FindStringSubmatch(body); m != nil {
		var names []string
		for _, q := range regexp.MustCompile(`["']([^"']+)["']`).FindAllStringSubmatch(m[1], -1) {
			names = append(names, q[1])
		}
		if len(names) > 0 {
			sort.Strings(names)
			return drfMetaResult{names: names, found: true}
		}
		return drfMetaResult{found: true}
	}
	return drfMetaResult{}
}
