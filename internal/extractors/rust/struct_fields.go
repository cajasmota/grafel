package rust

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitRustStructFields returns one SCOPE.Schema/field EntityRecord per named
// field of the struct named ownerName, honouring serde wire-name/skip
// attributes, plus a class→field CONTAINS edge attached to the owner.
//
// Issue #4854 — before this pass Rust struct fields were only emitted by the
// serde/utoipa/ORM-bound custom emitters (internal/custom/rust, #4635). A plain
// data struct therefore resolved to a SCOPE.Component with ZERO field children,
// so the dashboard shape endpoint returned rows:[] — the same gap #4850/#4855
// closed for Go and #4845/#4851 for JS/TS. Rust has no inheritance, so there is
// NO EXTENDS pass.
//
// Field Name is "<Owner>.<wire>" where <wire> is the serde rename target if
// present (else the Rust field name) so it dedups by Name in MergeWithCustom
// against the serde DTO field members; `#[serde(skip)]` fields are excluded.
//
//	Kind      = "SCOPE.Schema"
//	Subtype   = "field"
//	Name      = "<Owner>.<wire>"
//	Signature = "<type> <wire>"
func emitRustStructFields(
	node ts.Node,
	file extractor.FileInput,
	ownerName string,
) []types.EntityRecord {
	body := findRustFieldList(node)
	if body == nil || ownerName == "" {
		return nil
	}
	return rustFieldsFromList(body, file, ownerName)
}

// emitRustEnumVariantFields returns SCOPE.Schema/field entities for the named
// fields of struct-style enum variants (`Variant { x: i32 }`), keyed
// "<Enum>.<Variant>.<field>". Tuple/unit variants carry no named fields and
// contribute none (best-effort: Rust has no field name to model).
func emitRustEnumVariantFields(
	node ts.Node,
	file extractor.FileInput,
	ownerName string,
) []types.EntityRecord {
	if ownerName == "" {
		return nil
	}
	vl := node.ChildByFieldName("body")
	if vl == nil {
		vl = findChildOfType(node, "enum_variant_list")
	}
	if vl == nil {
		return nil
	}
	var out []types.EntityRecord
	for i := 0; i < int(vl.ChildCount()); i++ {
		v := vl.Child(i)
		if v == nil || v.Type() != "enum_variant" {
			continue
		}
		vname := childFieldText(v, "name", file.Content)
		if vname == "" {
			if id := findChildOfType(v, "identifier"); id != nil {
				vname = string(file.Content[id.StartByte():id.EndByte()])
			}
		}
		fl := findChildOfType(v, "field_declaration_list")
		if fl == nil || vname == "" {
			continue
		}
		out = append(out, rustFieldsFromList(fl, file, ownerName+"."+vname)...)
	}
	return out
}

// rustFieldsFromList builds field entities for every field_declaration in a
// field_declaration_list, applying the serde rename/skip attribute that
// immediately precedes each field.
func rustFieldsFromList(
	body ts.Node,
	file extractor.FileInput,
	owner string,
) []types.EntityRecord {
	var out []types.EntityRecord
	seen := make(map[string]bool)
	var pendingRename string
	var pendingSkip bool
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		switch ch.Type() {
		case "attribute_item":
			rename, skip := rustSerdeAttr(ch, file.Content)
			if rename != "" {
				pendingRename = rename
			}
			if skip {
				pendingSkip = true
			}
			continue
		case "field_declaration":
		default:
			continue
		}
		skip := pendingSkip
		rename := pendingRename
		pendingSkip = false
		pendingRename = ""
		if skip {
			continue
		}
		nameNode := ch.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = findChildOfType(ch, "field_identifier")
		}
		if nameNode == nil {
			continue
		}
		fname := string(file.Content[nameNode.StartByte():nameNode.EndByte()])
		wire := fname
		if rename != "" {
			wire = rename
		}
		if wire == "" || seen[wire] {
			continue
		}
		seen[wire] = true
		typ := childFieldText(ch, "type", file.Content)
		dotted := owner + "." + wire
		sig := wire
		if typ != "" {
			sig = typ + " " + wire
		}
		out = append(out, types.EntityRecord{
			Name:          dotted,
			QualifiedName: dotted,
			Kind:          "SCOPE.Schema",
			Subtype:       "field",
			SourceFile:    file.Path,
			StartLine:     int(ch.StartPoint().Row) + 1,
			EndLine:       int(ch.EndPoint().Row) + 1,
			Language:      "rust",
			Signature:     sig,
			QualityScore:  1.0,
			Properties: map[string]string{
				"field_name":   fname,
				"field_type":   typ,
				"wire_name":    wire,
				"parent_class": owner,
			},
			Metadata:           map[string]interface{}{"subtype": "field", "owner": owner},
			EnrichmentRequired: false,
		})
	}
	return out
}

// rustSerdeAttr parses an attribute_item, returning the serde rename target
// (if any) and whether the field is serde-skipped. Mirrors the regex logic in
// internal/custom/rust/fw_validation.go so the wire names align for dedup.
func rustSerdeAttr(attr ts.Node, src []byte) (rename string, skip bool) {
	text := string(src[attr.StartByte():attr.EndByte()])
	if !strings.Contains(text, "serde") {
		return "", false
	}
	if strings.Contains(text, "skip") {
		skip = true
	}
	if i := strings.Index(text, "rename"); i >= 0 {
		rest := text[i+len("rename"):]
		if q := strings.IndexByte(rest, '"'); q >= 0 {
			rest = rest[q+1:]
			if e := strings.IndexByte(rest, '"'); e >= 0 {
				rename = rest[:e]
			}
		}
	}
	return rename, skip
}

// findRustFieldList returns the field_declaration_list child of a struct_item,
// or nil for a tuple/unit struct (no named fields).
func findRustFieldList(node ts.Node) ts.Node {
	if node == nil {
		return nil
	}
	if b := node.ChildByFieldName("body"); b != nil && b.Type() == "field_declaration_list" {
		return b
	}
	return findChildOfType(node, "field_declaration_list")
}

// findChildOfType returns the first direct child of n with the given type.
func findChildOfType(n ts.Node, typ string) ts.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if c := n.Child(i); c != nil && c.Type() == typ {
			return c
		}
	}
	return nil
}

// attachRustFieldContains attaches a struct/enum→field CONTAINS edge to the
// owner Component (at componentIdx) for each SCOPE.Schema/field member emitted
// for it (including enum-variant fields keyed "<Enum>.<Variant>.<field>").
func attachRustFieldContains(
	records []types.EntityRecord,
	componentIdx int,
	filePath string,
	fieldEnts []types.EntityRecord,
) {
	for _, fe := range fieldEnts {
		toID := extractor.BuildSchemaFieldStructuralRef("rust", filePath, fe.Name)
		records[componentIdx].Relationships = append(records[componentIdx].Relationships,
			types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
	}
}
