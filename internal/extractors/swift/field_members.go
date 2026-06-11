package swift

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// emitSwiftFieldMembers returns one SCOPE.Schema/field EntityRecord per stored
// property (let/var) of the struct/class named ownerName, plus the bare base /
// protocol type names from the declaration's inheritance_specifier (for
// EXTENDS, resolved against in-file types by the caller).
//
// Issue #4854 — before this pass Swift had no general field-membership pass at
// all (the custom Swift emitters cover SwiftUI/Vapor routes, not Codable data
// model fields). A plain Swift data struct therefore resolved to a
// SCOPE.Component with ZERO field children, so the dashboard shape endpoint
// returned rows:[] — the same gap #4850/#4855 closed for Go and #4845/#4851 for
// JS/TS.
//
// Computed properties (with a `computed_property { get … }` body) are NOT
// stored members and are excluded.
//
//	Kind      = "SCOPE.Schema"
//	Subtype   = "field"
//	Name      = "<Owner>.<prop>"
//	Signature = "<type> <prop>"
func emitSwiftFieldMembers(
	node, body *sitter.Node,
	src []byte,
	ownerName, filePath string,
) ([]types.EntityRecord, []string) {
	if ownerName == "" {
		return nil, nil
	}

	var fields []types.EntityRecord
	seen := make(map[string]bool)

	if body != nil {
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch == nil || ch.Type() != "property_declaration" {
				continue
			}
			// Stored properties only: a computed_property child marks a
			// get/set-only computed property (no backing storage).
			if firstChildOfType(ch, "computed_property") != nil {
				continue
			}
			name := swiftPropertyName(ch, src)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			typ := ""
			if ta := firstChildOfType(ch, "type_annotation"); ta != nil {
				typ = firstDescendantText(ta, src, "type_identifier")
			}
			dotted := ownerName + "." + name
			sig := name
			if typ != "" {
				sig = typ + " " + name
			}
			fields = append(fields, types.EntityRecord{
				Name:               dotted,
				QualifiedName:      dotted,
				Kind:               "SCOPE.Schema",
				Subtype:            "field",
				SourceFile:         filePath,
				StartLine:          int(ch.StartPoint().Row) + 1,
				EndLine:            int(ch.EndPoint().Row) + 1,
				Language:           "swift",
				Signature:          sig,
				QualityScore:       1.0,
				Properties: map[string]string{
					"field_name":   name,
					"field_type":   typ,
					"parent_class": ownerName,
				},
				Metadata:           map[string]interface{}{"subtype": "field", "owner": ownerName},
				EnrichmentRequired: false,
			})
		}
	}

	return fields, swiftInheritedTypeNames(node, src)
}

// swiftPropertyName returns the bound identifier of a property_declaration's
// `pattern` child (the first simple_identifier). Tuple bindings
// (`let (a, b)`) yield only the first identifier — best-effort.
func swiftPropertyName(prop *sitter.Node, src []byte) string {
	pat := firstChildOfType(prop, "pattern")
	if pat == nil {
		return ""
	}
	if id := firstDescendantText(pat, src, "simple_identifier"); id != "" {
		return id
	}
	return ""
}

// swiftInheritedTypeNames returns the bare type names from a class/struct
// declaration's inheritance_specifier list. The Swift grammar does not
// distinguish a superclass from adopted protocols, so all are returned; the
// caller's in-file filter keeps only types we actually modeled (a struct's
// `: Codable` thus drops out, while `class B: A` keeps the in-file A).
func swiftInheritedTypeNames(node *sitter.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil || ch.Type() != "inheritance_specifier" {
			continue
		}
		if name := firstDescendantText(ch, src, "type_identifier"); name != "" {
			out = append(out, strings.TrimSpace(name))
		}
	}
	return out
}

// firstChildOfType returns the first direct child of n with the given type.
func firstChildOfType(n *sitter.Node, typ string) *sitter.Node {
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

// attachSwiftExtends emits class→base EXTENDS edges from each owner's stashed
// Metadata "base_candidates", restricted to base types declared in this same
// file so the dashboard shape walker can recurse into inherited members.
// Adopted protocols and external superclasses drop out via the in-file filter.
func attachSwiftExtends(records []types.EntityRecord) []types.EntityRecord {
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
		cands, _ := records[i].Metadata["base_candidates"].([]string)
		owner := records[i].Name
		for _, base := range cands {
			if base == "" || base == owner || !known[base] {
				continue
			}
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
		delete(records[i].Metadata, "base_candidates")
	}
	return records
}
