package javascript

// enum_valueset.go — value-carrying SCOPE.Enum value-set nodes for TypeScript
// (data-model, epic #3628 / completes #3806). Reuses the shared cross-language
// builder in internal/extractor (EnumEntity / EnumMember / StripLiteralQuotes)
// so TS enums and string-literal unions converge on the same node model as
// Python, C#, Java, Go and Rails.
//
// Two TS constructs map to a value-set:
//
//   - `enum Status { Active = 'active', Inactive = 'inactive' }` →
//     kind_hint="ts_enum", values=[Active=active, Inactive=inactive].
//     Numeric/implicit enums (`enum Dir { Up, Down }`) record members only —
//     no fabricated ordinals (honest-partial).
//   - `type Role = 'admin' | 'user'` → kind_hint="ts_literal_union",
//     members/values are the literal arms. ONLY when EVERY arm of the union is
//     a string/number literal; a union with any type reference
//     (`string | Foo`) is NOT an enumerated value-set → no node.

import (
	sitter "github.com/smacker/go-tree-sitter"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// emitTSEnumValueSet builds the value-carrying SCOPE.Enum node for a TS
// `enum_declaration`, capturing each member's explicit literal value when the
// member is an `enum_assignment` with a string/number RHS.
func (x *extractor) emitTSEnumValueSet(n *sitter.Node, name string) {
	body := n.ChildByFieldName("body")
	if body == nil {
		return
	}
	var members []extreg.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "property_identifier", "identifier":
			// Bare member, no explicit value (numeric/implicit enum).
			members = append(members, extreg.EnumMember{Name: x.nodeText(ch)})
		case "enum_assignment":
			mn := ch.ChildByFieldName("name")
			mname := x.nodeText(mn)
			if mname == "" {
				if first := ch.Child(0); first != nil {
					mname = x.nodeText(first)
				}
			}
			if mname == "" {
				continue
			}
			mval := ""
			if v := ch.ChildByFieldName("value"); v != nil {
				if lit := tsLiteralValue(v, x); lit != "" {
					mval = lit
				}
			} else {
				// Fall back to the last named child after the `=`.
				for j := int(ch.ChildCount()) - 1; j >= 0; j-- {
					c := ch.Child(j)
					if c != nil && c.IsNamed() && c.Type() != "property_identifier" {
						if lit := tsLiteralValue(c, x); lit != "" {
							mval = lit
						}
						break
					}
				}
			}
			members = append(members, extreg.EnumMember{Name: mname, Value: mval})
		}
	}
	start, end := lines(n)
	if ent, ok := extreg.EnumEntity(name, x.language, "ts_enum", x.filePath, start, end, members); ok {
		x.entities = append(x.entities, ent)
	}
}

// emitTSLiteralUnionValueSet builds a SCOPE.Enum node for a string/number
// literal union type alias (`type Role = 'admin' | 'user'`). It emits a node
// ONLY when the RHS is a union_type whose EVERY arm is a literal_type wrapping
// a string/number literal. Any non-literal arm (type reference, predefined
// type, object type) disqualifies the alias — it is not an enumerated value-set.
func (x *extractor) emitTSLiteralUnionValueSet(n *sitter.Node, name string) {
	valueNode := n.ChildByFieldName("value")
	if valueNode == nil || valueNode.Type() != "union_type" {
		return
	}
	// A multi-arm union is left-nested by the grammar:
	//   `a | b | c` → union_type(union_type(a, b), c). Flatten recursively;
	// any non-literal arm disqualifies the whole alias (ok=false).
	var members []extreg.EnumMember
	if !collectUnionLiterals(valueNode, x, &members) || len(members) < 1 {
		return
	}
	start, end := lines(n)
	if ent, ok := extreg.EnumEntity(name, x.language, "ts_literal_union", x.filePath, start, end, members); ok {
		x.entities = append(x.entities, ent)
	}
}

// collectUnionLiterals flattens a (possibly left-nested) union_type, appending
// one EnumMember per literal arm. It returns false the moment it encounters a
// non-literal arm (type reference, predefined type, object type) so the caller
// emits no value-set node for a non-enumerated union.
func collectUnionLiterals(u *sitter.Node, x *extractor, out *[]extreg.EnumMember) bool {
	for i := 0; i < int(u.ChildCount()); i++ {
		arm := u.Child(i)
		if arm == nil || !arm.IsNamed() {
			continue
		}
		switch arm.Type() {
		case "union_type":
			if !collectUnionLiterals(arm, x, out) {
				return false
			}
		case "literal_type":
			lit := tsLiteralValue(arm, x)
			if lit == "" {
				return false
			}
			*out = append(*out, extreg.EnumMember{Name: lit, Value: lit})
		default:
			return false
		}
	}
	return true
}

// tsLiteralValue returns the statically-known literal text of a TS value node
// (string / number / unary minus number), with surrounding quotes stripped.
// Returns "" for non-literal nodes so callers can treat that as "not a literal".
func tsLiteralValue(n *sitter.Node, x *extractor) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "literal_type":
		// Unwrap to the inner literal node.
		if c := n.NamedChild(0); c != nil {
			return tsLiteralValue(c, x)
		}
		return ""
	case "string":
		return extreg.StripLiteralQuotes(x.nodeText(n))
	case "number":
		return x.nodeText(n)
	case "unary_expression":
		// e.g. -1
		return x.nodeText(n)
	}
	return ""
}
