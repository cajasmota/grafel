package php

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// emitPhpFieldMembers returns one SCOPE.Schema/field EntityRecord per typed
// class property and per promoted constructor parameter of the class named
// ownerName, plus the bare base-class name from the class's base_clause (for
// EXTENDS, resolved against in-file types by the caller).
//
// Issue #4854 — before this pass PHP class properties were not emitted as field
// entities outside the framework/ORM-bound custom emitters
// (internal/custom/php, #4613). A plain PHP data class therefore resolved to a
// SCOPE.Component with ZERO field children, so the dashboard shape endpoint
// returned rows:[] — the same gap #4850/#4855 closed for Go and #4845/#4851
// for JS/TS.
//
// Property/param Name is "<Owner>.<prop>" (the PHP member name with the leading
// `$` stripped) so it dedups by Name in MergeWithCustom against the framework
// DTO field members.
//
//	Kind      = "SCOPE.Schema"
//	Subtype   = "field"
//	Name      = "<Owner>.<member>"
//	Signature = "<type> $<member>"
func emitPhpFieldMembers(
	node ts.Node,
	body ts.Node,
	src []byte,
	ownerName, filePath string,
) ([]types.EntityRecord, string) {
	if ownerName == "" {
		return nil, ""
	}

	var fields []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, typ string, at ts.Node) {
		name = strings.TrimPrefix(name, "$")
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		startLine := int(at.StartPoint().Row) + 1
		endLine := int(at.EndPoint().Row) + 1
		dotted := ownerName + "." + name
		sig := "$" + name
		if typ != "" {
			sig = typ + " $" + name
		}
		fields = append(fields, types.EntityRecord{
			Name:          dotted,
			QualifiedName: dotted,
			Kind:          "SCOPE.Schema",
			Subtype:       "field",
			SourceFile:    filePath,
			StartLine:     startLine,
			EndLine:       endLine,
			Language:      "php",
			Signature:     sig,
			QualityScore:  1.0,
			Properties: map[string]string{
				"field_name":   name,
				"field_type":   typ,
				"parent_class": ownerName,
			},
			Metadata:           map[string]interface{}{"subtype": "field", "owner": ownerName},
			EnrichmentRequired: false,
		})
	}

	if body != nil {
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch == nil {
				continue
			}
			switch ch.Type() {
			case "property_declaration":
				typ := phpDeclaredType(ch, src)
				for j := 0; j < int(ch.ChildCount()); j++ {
					el := ch.Child(j)
					if el == nil || el.Type() != "property_element" {
						continue
					}
					if vn := findFirstChildOfType(el, "variable_name"); vn != nil {
						add(phpVariableName(vn, src), typ, ch)
					}
				}
			case "method_declaration":
				// Constructor-promoted parameters: a formal_parameters child
				// whose entries carry a visibility_modifier.
				if !strings.EqualFold(childFieldText(ch, "name", src), "__construct") {
					continue
				}
				fp := findFirstChildOfType(ch, "formal_parameters")
				if fp == nil {
					continue
				}
				for j := 0; j < int(fp.ChildCount()); j++ {
					p := fp.Child(j)
					if p == nil || p.Type() != "property_promotion_parameter" {
						continue
					}
					typ := phpDeclaredType(p, src)
					if vn := findFirstChildOfType(p, "variable_name"); vn != nil {
						add(phpVariableName(vn, src), typ, p)
					}
				}
			}
		}
	}

	return fields, phpBaseClassName(node, src)
}

// phpVariableName returns the bare identifier of a variable_name node
// (`$name` → "name").
func phpVariableName(vn ts.Node, src []byte) string {
	if id := findFirstChildOfType(vn, "name"); id != nil {
		return string(src[id.StartByte():id.EndByte()])
	}
	return strings.TrimPrefix(string(src[vn.StartByte():vn.EndByte()]), "$")
}

// phpDeclaredType returns the textual type annotation that precedes the
// property_element / variable_name in a property_declaration or
// property_promotion_parameter. Returns "" for untyped members.
func phpDeclaredType(n ts.Node, src []byte) string {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		switch ch.Type() {
		case "primitive_type", "named_type", "optional_type", "union_type",
			"intersection_type", "type_list":
			return strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()]))
		}
	}
	return ""
}

// phpBaseClassName returns the bare parent-class name from a class
// declaration's base_clause (`class A extends B` → "B"), or "" for none.
// Interfaces appear under class_interface_clause and are intentionally
// excluded.
func phpBaseClassName(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	bc := findFirstChildOfType(node, "base_clause")
	if bc == nil {
		return ""
	}
	if nm := findFirstChildOfType(bc, "name"); nm != nil {
		return strings.TrimSpace(string(src[nm.StartByte():nm.EndByte()]))
	}
	return ""
}

// attachPhpExtends emits class→base EXTENDS edges from each owner's stashed
// Metadata "base_candidate", restricted to base classes declared in this same
// file so the dashboard shape walker can recurse into inherited members.
func attachPhpExtends(records []types.EntityRecord) []types.EntityRecord {
	known := make(map[string]bool)
	for i := range records {
		if records[i].Kind == "SCOPE.Component" {
			known[records[i].Name] = true
		}
	}
	for i := range records {
		if records[i].Kind != "SCOPE.Component" || records[i].Metadata == nil {
			continue
		}
		base, _ := records[i].Metadata["base_candidate"].(string)
		owner := records[i].Name
		if base != "" && base != owner && known[base] {
			dup := false
			for _, ex := range records[i].Relationships {
				if ex.Kind == "EXTENDS" && ex.ToID == base {
					dup = true
					break
				}
			}
			if !dup {
				records[i].Relationships = append(records[i].Relationships,
					types.RelationshipRecord{FromID: owner, ToID: base, Kind: "EXTENDS"})
			}
		}
		delete(records[i].Metadata, "base_candidate")
	}
	return records
}
