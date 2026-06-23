package golang

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// extractStructFieldEntities returns one SCOPE.Schema/field EntityRecord per
// named field of the struct named ownerName, plus EXTENDS RelationshipRecords
// (returned as the second result, to be attached to the OWNER struct record)
// for each embedded/anonymous field.
//
// Issue #4850 — before this pass Go struct fields were only consumed for
// DEPENDS_ON edges (extractStructFieldDependencies) and call-dispatch stamping
// (collectStructFieldTypes); they were never emitted as graph entities. A Go
// DTO/struct therefore resolved to a SCOPE.Component with ZERO field children,
// so the dashboard shape endpoint returned rows:[] and classHasFieldChildren
// was false — exactly the gap #4845/#4851 closed for JS/TS DTOs.
//
// Field entity shape mirrors the Java (#690), Python (#689), Kotlin and Scala
// field-membership emitters so the shared dashboard shape walker
// (internal/dashboard/shape_tree.go) parses them uniformly:
//
//	Kind       = "SCOPE.Schema"
//	Subtype    = "field"
//	Name       = "<Struct>.<field>"          (dotted, file-scoped resolver key)
//	Signature  = "<type> <field>"            (parseFieldSignature contract)
//
// The CONTAINS edge from the struct to each field entity is emitted later by
// attachClassContains via extractor.BuildSchemaFieldStructuralRef, keyed on the
// same dotted Name + file path.
//
// Embedded fields:
//
//	type Base struct { ID string }
//	type User struct { Base; Name string }   // Base is embedded (promoted)
//
// An embedded field has a type but no field_identifier. We model it as an
// EXTENDS edge from the embedding struct to the embedded type (mirroring the
// JS/TS `extends Base` handling in heritage.go), so the shape walker recurses
// into the base struct's fields (cycle-guarded, subclass fields shadow
// inherited). Only embedded types declared in this file (knownTypeNames) get
// an EXTENDS edge, keeping the graph conservative — package-external embeds
// resolve cross-file is out of scope for this pass.
func extractStructFieldEntities(
	typeBody ts.Node,
	src []byte,
	ownerName, filePath string,
	knownTypeNames map[string]bool,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if typeBody == nil || ownerName == "" {
		return nil, nil
	}

	var fields []types.EntityRecord
	var extends []types.RelationshipRecord
	seenField := make(map[string]bool)
	seenExtends := make(map[string]bool)

	for _, field := range topLevelFieldDeclarations(typeBody) {
		typeNode := field.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		typeText := nodeText(typeNode, src)
		if typeText == "" {
			continue
		}
		startLine, endLine := nodeLines(field)

		// Collect every leading identifier before the type and the optional
		// struct tag. A field_declaration carries either field_identifier(s)
		// (`A, B int` → two fields sharing one type) or no name node at all
		// (embedded/anonymous field). The trailing raw_string_literal is the
		// `json:"..." validate:"..."` tag, if present.
		var names []string
		var tag string
		for k := 0; k < int(field.ChildCount()); k++ {
			ch := field.Child(k)
			switch ch.Type() {
			case "field_identifier":
				names = append(names, nodeText(ch, src))
			case "raw_string_literal", "interpreted_string_literal":
				tag = strings.Trim(nodeText(ch, src), "`\"")
			}
		}
		jsonName, jsonSkip := goJSONWireName(tag)

		if len(names) == 0 {
			// Embedded/anonymous field — model as EXTENDS to the embedded
			// type if it is declared in this file.
			base := innermostTypeName(typeText)
			if base != "" && base != ownerName && knownTypeNames[base] && !seenExtends[base] {
				seenExtends[base] = true
				extends = append(extends, types.RelationshipRecord{
					FromID: ownerName,
					ToID:   base,
					Kind:   "EXTENDS",
				})
			}
			continue
		}

		for _, fname := range names {
			if fname == "" {
				continue
			}
			// Wire name: prefer the json tag name (matching the endpoint-bound
			// DTO field members emitted by internal/custom/golang so the two
			// passes dedup by Name in MergeWithCustom and the richer custom
			// entity wins). Only single-name fields carry a meaningful tag.
			wire := fname
			if len(names) == 1 {
				if jsonSkip {
					continue // json:"-" — excluded from the wire shape
				}
				if jsonName != "" {
					wire = jsonName
				}
			}
			if wire == "" || seenField[wire] {
				continue
			}
			seenField[wire] = true
			dotted := ownerName + "." + wire
			fields = append(fields, types.EntityRecord{
				Name:               dotted,
				QualifiedName:      dotted,
				Kind:               "SCOPE.Schema",
				Subtype:            "field",
				SourceFile:         filePath,
				StartLine:          startLine,
				EndLine:            endLine,
				Language:           "go",
				Signature:          typeText + " " + wire,
				QualityScore:       1.0,
				Metadata:           map[string]interface{}{"subtype": "field", "owner": ownerName},
				EnrichmentRequired: false,
			})
		}
	}

	return fields, extends
}

// goJSONWireName parses a raw struct tag (e.g. `json:"name,omitempty" validate:"required"`)
// and returns the json wire name and whether the field is json-excluded
// (`json:"-"`). It mirrors the wire-name logic in internal/custom/golang so the
// primary-pass field entity Name matches the endpoint-bound DTO field member and
// the two dedup in MergeWithCustom. Returns ("", false) when there is no json
// tag or only a bare `json:"...,opts"` with an empty name part (keep Go name).
func goJSONWireName(tag string) (name string, skip bool) {
	const key = "json:"
	i := strings.Index(tag, key)
	if i < 0 {
		return "", false
	}
	rest := tag[i+len(key):]
	// The value is quoted: json:"<value>".
	if len(rest) == 0 || rest[0] != '"' {
		return "", false
	}
	rest = rest[1:]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		rest = rest[:j]
	}
	first := rest
	if c := strings.IndexByte(rest, ','); c >= 0 {
		first = rest[:c]
	}
	if first == "-" {
		return "", true
	}
	return first, false
}

// topLevelFieldDeclarations returns the field_declaration nodes that are DIRECT
// members of the struct (children of its field_declaration_list), NOT the
// fields of a nested anonymous struct type (`Inner struct { ... }`). A naive
// recursive findAll would mis-attribute nested-struct fields to the outer
// struct; iterating only the immediate field_declaration_list children keeps
// membership correct.
func topLevelFieldDeclarations(structType ts.Node) []ts.Node {
	var list ts.Node
	for i := 0; i < int(structType.ChildCount()); i++ {
		if c := structType.Child(i); c.Type() == "field_declaration_list" {
			list = c
			break
		}
	}
	if list == nil {
		return nil
	}
	var out []ts.Node
	for i := 0; i < int(list.ChildCount()); i++ {
		if c := list.Child(i); c.Type() == "field_declaration" {
			out = append(out, c)
		}
	}
	return out
}

// innermostTypeName strips Go pointer / qualifier decoration from an embedded
// field's verbatim type text to recover the bare type name used as the
// intra-file EXTENDS target. `*Base` → "Base"; `pkg.Base` → "" (package-
// external, not an intra-file embed we can resolve); `Base` → "Base".
func innermostTypeName(typeText string) string {
	t := typeText
	for len(t) > 0 && (t[0] == '*' || t[0] == ' ') {
		t = t[1:]
	}
	// A package-qualified embed (pkg.Base) is cross-package; we only emit
	// EXTENDS for intra-file base types, so reject any dotted form.
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c == '.' {
			return ""
		}
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '_') {
			// Generic/array/map decoration — not a bare embedded named type.
			return ""
		}
	}
	return t
}
