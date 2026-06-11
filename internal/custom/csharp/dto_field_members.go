package csharp

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// dto_field_members.go — C#/.NET DTO FIELD-as-member indexing (issue #4715),
// generalizing the uniform DTO field-member model from the JS
// (javascript/validation_schema.go::emitSchemaFieldMembers, #4635), Python
// (python/dto_field_members.go, #4613), Java (java/spring_dto_fields.go, #4613)
// and Go (golang/dto_field_members.go, #4715) emitters.
//
// Each DataAnnotations-bearing DTO/record class's properties become
// `SCOPE.Schema` subtype=field sub-entities named `<Class>.<Property>`, carrying
// the SAME property shape as the other frameworks so the cross-framework
// field-level diff tools + the dashboard /shape ShapeTree resolver treat all
// frameworks uniformly:
//
//	field_name   — the property name
//	field_type   — normalized scalar/declared type ("string"/"integer"/...)
//	parent_class — the owning DTO/record class name
//	optional     — "true" when the property is nullable (`string?`) and not [Required]
//	validators   — space-joined `@Attr` markers from DataAnnotation attributes
//	provenance   — INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP
//	library      — "data-annotations"
//
// The child's Signature is the Java-style `[@Attr ...] <type> <name>` so the
// shape resolver's parseFieldSignature recovers (annotations, type, name)
// exactly as it does for the JS/Java/Python/Go DTO fields. A CONTAINS edge binds
// each field to its owner class via the `Class:<Class>` byName fallback.

// csDTOProperty is a captured property: name, normalized type, optionality, and
// DataAnnotation validator markers.
type csDTOProperty struct {
	name       string
	typ        string
	validators []string
	optional   bool
}

// reCsProperty matches an auto-property declaration inside a class body:
//
//	public string Name { get; set; }
//	public int? Age { get; init; }
//	public required string Email { get; set; }
//
// Group 1 = the declared type (with optional `?`), group 2 = the property name.
var reCsProperty = regexp.MustCompile(
	`(?m)^\s*public\s+(?:required\s+|virtual\s+|override\s+|new\s+)*([A-Za-z_][\w<>,.\[\]?]*)\s+([A-Za-z_]\w*)\s*\{\s*(?:get|set|init)`)

// reCsAttrName extracts the bare attribute name from a `[Attr(...)]` block.
var reCsAttrName = regexp.MustCompile(`[A-Za-z_]\w*`)

// csScalarKind normalizes a C# type token to a scalar type, parity with the
// JS/Python/Go scalar maps.
var csScalarKind = map[string]string{
	"string": "string", "String": "string", "char": "string", "Char": "string",
	"Guid": "string", "Uri": "string",
	"int": "integer", "Int32": "integer", "long": "integer", "Int64": "integer",
	"short": "integer", "Int16": "integer", "byte": "integer", "uint": "integer",
	"ulong": "integer", "ushort": "integer", "sbyte": "integer",
	"float": "number", "double": "number", "decimal": "number",
	"Single": "number", "Double": "number", "Decimal": "number",
	"bool": "boolean", "Boolean": "boolean",
	"DateTime": "date", "DateTimeOffset": "date", "DateOnly": "date", "TimeOnly": "date",
	"object": "any", "dynamic": "any",
}

// normalizeCsType strips the nullable `?`, generic args, and array markers, then
// normalizes via csScalarKind. List<T>/IEnumerable<T>/arrays → "array";
// Dictionary<K,V> → "object".
func normalizeCsType(typ string) string {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimSuffix(typ, "?")
	if strings.HasSuffix(typ, "[]") {
		return "array"
	}
	// Generic collections.
	if i := strings.IndexByte(typ, '<'); i >= 0 {
		head := typ[:i]
		switch head {
		case "List", "IList", "IEnumerable", "ICollection", "HashSet", "IReadOnlyList", "Collection":
			return "array"
		case "Dictionary", "IDictionary", "IReadOnlyDictionary":
			return "object"
		}
		typ = head
	}
	if k, ok := csScalarKind[typ]; ok {
		return k
	}
	return typ
}

// csDataAnnotationNames are the DataAnnotation attributes surfaced as validator
// markers on a field member (parity with class-validator `@IsX`).
var csDataAnnotationNames = map[string]bool{
	"Required": true, "StringLength": true, "Range": true, "MinLength": true,
	"MaxLength": true, "RegularExpression": true, "EmailAddress": true,
	"Phone": true, "Url": true, "Compare": true, "CreditCard": true,
	"DataType": true, "EnumDataType": true,
}

// extractCsharpDTOFields parses a C# class body into its annotated property
// members. `body` is the source between the class's opening and closing brace.
// A property is required when it carries a `[Required]` attribute or the
// `required` modifier; it is optional when its type is nullable (`T?`) and not
// required.
func extractCsharpDTOFields(body string) []csDTOProperty {
	var fields []csDTOProperty
	seen := make(map[string]bool)
	for _, m := range reCsProperty.FindAllStringSubmatchIndex(body, -1) {
		typ := strings.TrimSpace(body[m[2]:m[3]])
		name := body[m[4]:m[5]]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		// Collect the DataAnnotation attributes attached to this property: scan
		// the lines immediately preceding the declaration for `[Attr...]` blocks.
		decl := body[m[0]:m[1]]
		hasRequiredModifier := regexp.MustCompile(`\brequired\b`).MatchString(decl)
		attrs := csPropertyAttributes(body, m[0])

		var validators []string
		hasRequiredAttr := false
		for _, a := range attrs {
			if csDataAnnotationNames[a] {
				validators = append(validators, "@"+a)
			}
			if a == "Required" {
				hasRequiredAttr = true
			}
		}

		nullable := strings.HasSuffix(typ, "?")
		optional := nullable && !hasRequiredAttr && !hasRequiredModifier

		fields = append(fields, csDTOProperty{
			name:       name,
			typ:        normalizeCsType(typ),
			validators: validators,
			optional:   optional,
		})
	}
	return fields
}

// csPropertyAttributes returns the DataAnnotation attribute names attached to a
// property whose declaration starts at byte offset `declStart`, by scanning the
// contiguous block of `[...]` attribute lines immediately above it.
func csPropertyAttributes(body string, declStart int) []string {
	// Walk back over the preceding attribute / blank lines.
	start := declStart
	for start > 0 {
		lineEnd := start - 1
		if body[lineEnd] != '\n' {
			break
		}
		lineStart := strings.LastIndexByte(body[:lineEnd], '\n') + 1
		line := strings.TrimSpace(body[lineStart:lineEnd])
		if line == "" || strings.HasPrefix(line, "[") {
			start = lineStart
			continue
		}
		break
	}
	block := body[start:declStart]
	var names []string
	for _, m := range regexp.MustCompile(`\[([^\]]*)\]`).FindAllStringSubmatch(block, -1) {
		// An attribute block may hold multiple comma-separated attributes.
		for _, part := range strings.Split(m[1], ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if nm := reCsAttrName.FindString(part); nm != "" {
				names = append(names, nm)
			}
		}
	}
	return names
}

// csContainsFieldEdge builds the structural CONTAINS membership edge from an
// owning DTO class to one of its property field members. FromID names the owner
// class (`Class:<owner>`) so the resolver binds it to the real class entity;
// ToID is the field entity's own ID.
func csContainsFieldEdge(ownerClass, memberID, fieldName string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: "Class:" + ownerClass,
		ToID:   memberID,
		Kind:   string(types.RelationshipKindContains),
		Properties: map[string]string{
			"framework":  "data-annotations",
			"language":   "csharp",
			"member":     "field",
			"field_name": fieldName,
			"provenance": "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP",
		},
	}
}

// emitCsharpDTOFieldMembers emits one `SCOPE.Schema`/field sub-entity per
// property of a DTO class, each carrying its own CONTAINS membership edge.
// Mirrors the JS/Python/Java/Go field-membership model.
func emitCsharpDTOFieldMembers(
	className string,
	fields []csDTOProperty,
	filePath string,
	ownerLine int,
) []types.EntityRecord {
	if className == "" || len(fields) == 0 {
		return nil
	}
	const library = "data-annotations"
	var out []types.EntityRecord
	for _, f := range fields {
		typ := f.typ
		if typ == "" {
			typ = "unknown"
		}
		// Java-style field signature: `[@Attr ...] Type name`.
		var sb strings.Builder
		for _, a := range f.validators {
			sb.WriteString(a)
			sb.WriteByte(' ')
		}
		sb.WriteString(typ)
		sb.WriteByte(' ')
		sb.WriteString(f.name)

		childName := className + "." + f.name
		child := makeEntity(childName, "SCOPE.Schema", "field", filePath, "csharp", ownerLine)
		child.Signature = sb.String()
		setProps(&child,
			"library", library,
			"pattern_type", "field",
			"field_name", f.name,
			"field_type", typ,
			"parent_class", className,
			"provenance", "INFERRED_FROM_SCHEMA_FIELD_MEMBERSHIP")
		if f.optional {
			setProps(&child, "optional", "true")
		}
		if len(f.validators) > 0 {
			setProps(&child, "validators", strings.Join(f.validators, " "))
		}
		child.Relationships = append(child.Relationships,
			csContainsFieldEdge(className, child.ID, f.name))
		out = append(out, child)
	}
	return out
}

// csClassBody returns the body between the class's opening `{` and its matching
// `}`, given the byte offset of the class declaration keyword. Quote-aware so
// braces inside string literals are ignored.
func csClassBody(src string, classDeclStart int) string {
	open := strings.IndexByte(src[classDeclStart:], '{')
	if open < 0 {
		return ""
	}
	open += classDeclStart
	depth := 0
	var quote byte
	for i := open; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == quote && src[i-1] != '\\' {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i]
			}
		}
	}
	return src[open+1:]
}
