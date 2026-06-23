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
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// emitTSEnumValueSet builds the value-carrying SCOPE.Enum node for a TS
// `enum_declaration`, capturing each member's explicit literal value when the
// member is an `enum_assignment` with a string/number RHS.
func (x *extractor) emitTSEnumValueSet(n ts.Node, name string) {
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
func (x *extractor) emitTSLiteralUnionValueSet(n ts.Node, name string) {
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
func collectUnionLiterals(u ts.Node, x *extractor, out *[]extreg.EnumMember) bool {
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

// emitTSConstObjectValueSet builds a value-carrying SCOPE.Enum node for a
// const-object constant collection — the TS/JS sibling of a Python module dict.
// #4420: `export const PermissionPage = { CoreAdmin: 'core-admin', ... } as
// const` is the v3 source-of-truth permission map; emitting it as a
// name-searchable value-set lets a downstream cross-graph parity-audit diff it
// against the Django PERMISSION_PAGES dict.
//
// valueNode is the variable_declarator's value. Honest-partial: a node is
// emitted ONLY when the value is an object literal whose membership is a closed
// set of string/number-literal-valued entries AND at least one value is a
// literal — a plain object whose values are callables / calls is not an
// enumerated constant set and emits no node. The `as const` assertion is the
// strong signal but not required (a permission-style all-literal map qualifies);
// any non-literal value disqualifies the object.
func (x *extractor) emitTSConstObjectValueSet(name string, valueNode ts.Node) {
	if name == "" || valueNode == nil {
		return
	}
	obj := unwrapAsConstObject(valueNode)
	if obj == nil {
		return
	}
	var members []extreg.EnumMember
	allLiteral := true
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		pair := obj.NamedChild(i)
		if pair == nil || pair.Type() != "pair" {
			// Spread / method / shorthand — not a plain key:literal map.
			if pair != nil && pair.IsNamed() {
				allLiteral = false
			}
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		valNode := pair.ChildByFieldName("value")
		key := constObjectKey(keyNode, x)
		if key == "" {
			continue
		}
		lit := tsLiteralValue(valNode, x)
		if lit == "" {
			// A non-literal value (identifier, call, object) means this is not a
			// closed enumerated value-set.
			allLiteral = false
		}
		ln := 1
		if pair != nil {
			ln = int(pair.StartPoint().Row) + 1
		}
		members = append(members, extreg.EnumMember{Name: key, Value: lit, Line: ln})
	}
	// Closed value-set gate: every entry must be a literal-valued pair.
	if !allLiteral {
		return
	}
	start, end := lines(valueNode)
	if ent, ok := extreg.EnumEntity(name, x.language, "ts_const_object", x.filePath, start, end, members); ok {
		x.entities = append(x.entities, ent)
	}
}

// unwrapAsConstObject returns the underlying object literal node when valueNode
// is an `as_expression` / `satisfies_expression` wrapping an object literal —
// i.e. `{...} as const`, `{...} as SomeType`, or `{...} satisfies T`. The type
// assertion is REQUIRED: a bare object literal (`const ROUTES = {...}`) is
// resolver state, not a declared source-of-truth value-set (issue #562), so it
// is deliberately NOT treated as a constant collection. The `as const` /
// `satisfies` assertion is the author's explicit "this is a closed, immutable
// value-set" signal that distinguishes the v3 PermissionPage map (#4420) from
// an incidental config object.
func unwrapAsConstObject(valueNode ts.Node) ts.Node {
	switch valueNode.Type() {
	case "as_expression", "satisfies_expression":
		if inner := valueNode.NamedChild(0); inner != nil && inner.Type() == "object" {
			return inner
		}
	}
	return nil
}

// constObjectKey reduces an object-literal key node to its bare key string,
// handling `property_identifier` (Foo:), `string` ('Foo':) and `number` keys.
// Computed keys ([expr]:) yield "" (skipped).
func constObjectKey(keyNode ts.Node, x *extractor) string {
	if keyNode == nil {
		return ""
	}
	switch keyNode.Type() {
	case "property_identifier", "identifier":
		return x.nodeText(keyNode)
	case "string":
		return extreg.StripLiteralQuotes(x.nodeText(keyNode))
	case "number":
		return x.nodeText(keyNode)
	}
	return ""
}

// tsLiteralValue returns the statically-known literal text of a TS value node
// (string / number / unary minus number), with surrounding quotes stripped.
// Returns "" for non-literal nodes so callers can treat that as "not a literal".
func tsLiteralValue(n ts.Node, x *extractor) string {
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
