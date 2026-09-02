package csharp

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// emitFieldMembers returns one SCOPE.Schema/field EntityRecord per property,
// public field, and record positional parameter of the type named ownerName.
//
// Issue #6742 moved base-list parsing out of here into hierarchy.go, which
// emits the EXTENDS / IMPLEMENTS edges for every supertype rather than only
// the in-file ones.
//
// Issue #4854 — before this pass C# class/record/struct members were consumed
// for receiver resolution (collectFieldTypes) but only the endpoint/DTO-bound
// subset emitted field entities (internal/custom/csharp/dto_field_members.go,
// #4715). A plain data class therefore resolved to a SCOPE.Component with ZERO
// field children, so the dashboard shape endpoint returned rows:[] — the same
// gap #4850/#4855 closed for Go and #4845/#4851 for JS/TS.
//
// Field Name is "<Owner>.<Property>" (the C# member name, NOT a wire name) so
// it folds against the custom DTO field members in MergeWithCustom —
// the richer custom entity (validators, library, optionality) wins.
//
//	Kind      = "SCOPE.Schema"
//	Subtype   = "field"
//	Name      = "<Owner>.<member>"
//	Signature = "<type> <member>"
func emitFieldMembers(
	node ts.Node,
	body ts.Node,
	src []byte,
	ownerName, filePath string,
) []types.EntityRecord {
	if ownerName == "" {
		return nil
	}

	var fields []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, typ string, startNode ts.Node) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		startLine := int(startNode.StartPoint().Row) + 1
		endLine := int(startNode.EndPoint().Row) + 1
		dotted := ownerName + "." + name
		sig := name
		if typ != "" {
			sig = typ + " " + name
		}
		fields = append(fields, types.EntityRecord{
			Name:          dotted,
			QualifiedName: dotted,
			Kind:          "SCOPE.Schema",
			Subtype:       "field",
			SourceFile:    filePath,
			StartLine:     startLine,
			EndLine:       endLine,
			Language:      "csharp",
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

	// Record positional parameters: `record Foo(int Id, string Name)`. The
	// parameter_list is a direct child of the record_declaration node.
	if node != nil && node.Type() == "record_declaration" {
		if pl := findChildByType(node, "parameter_list"); pl != nil {
			for i := 0; i < int(pl.ChildCount()); i++ {
				p := pl.Child(i)
				if p == nil || p.Type() != "parameter" {
					continue
				}
				typ := leafTypeName(p.ChildByFieldName("type"), src)
				name := childFieldText(p, "name", src)
				add(name, typ, p)
			}
		}
	}

	if body != nil {
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch == nil {
				continue
			}
			switch ch.Type() {
			case "property_declaration":
				typ := leafTypeName(ch.ChildByFieldName("type"), src)
				name := childFieldText(ch, "name", src)
				add(name, typ, ch)
			case "field_declaration":
				vd := findChildByType(ch, "variable_declaration")
				if vd == nil {
					continue
				}
				typ := leafTypeName(vd.ChildByFieldName("type"), src)
				for j := 0; j < int(vd.ChildCount()); j++ {
					d := vd.Child(j)
					if d == nil || d.Type() != "variable_declarator" {
						continue
					}
					name := childFieldText(d, "name", src)
					if name == "" {
						for k := 0; k < int(d.ChildCount()); k++ {
							cc := d.Child(k)
							if cc != nil && cc.Type() == "identifier" {
								name = string(src[cc.StartByte():cc.EndByte()])
								break
							}
						}
					}
					add(name, typ, ch)
				}
			}
		}
	}

	return fields
}
