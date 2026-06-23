package java

// enum_valueset.go — value-carrying SCOPE.Enum value-set nodes for Java
// (data-model, epic #3628 / completes #3806). Reuses the shared cross-language
// builder in internal/extractor (EnumEntity / EnumMember / StripLiteralQuotes)
// so Java enums converge on the same model as Python, C#, TS, Go and Rails.
//
//   - `enum Status { ACTIVE, INACTIVE }` → members ACTIVE, INACTIVE (no values).
//   - `enum Color { RED("#f00"), GREEN("#0f0") }` → constructor-arg enums:
//     the FIRST literal constructor argument is captured as the member value,
//     so Color.RED carries value "#f00" (kind_hint="java_enum").
//
// Honest-partial: a constant whose first constructor argument is not a literal
// (a method call, a reference to another constant, a complex expression) is
// recorded value-less — the member name is kept, no fabricated value.

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// buildJavaEnumValueSet builds the SCOPE.Enum node for a Java `enum_declaration`.
func buildJavaEnumValueSet(node ts.Node, file extractor.FileInput) (recOut types.EntityRecord, ok bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch == nil || ch.Type() != "enum_constant" {
			continue
		}
		mname := childFieldText(ch, "name", file.Content)
		if mname == "" {
			// Fall back to the first identifier child.
			if idn := firstChildOfType(ch, "identifier"); idn != nil {
				mname = nodeText(idn, file.Content)
			}
		}
		if mname == "" {
			continue
		}
		mval := javaEnumConstantValue(ch, file.Content)
		members = append(members, extractor.EnumMember{Name: mname, Value: mval})
	}
	return extractor.EnumEntity(
		name, "java", "java_enum", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// javaEnumConstantValue returns the statically-known literal value of an
// enum_constant's first constructor argument, or "" when the constant has no
// argument list or the first argument is not a literal.
func javaEnumConstantValue(constNode ts.Node, src []byte) string {
	args := childByType(constNode, "argument_list")
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		a := args.Child(i)
		if a == nil || !a.IsNamed() {
			continue
		}
		// First named argument is the value we capture.
		switch a.Type() {
		case "string_literal", "decimal_integer_literal", "decimal_floating_point_literal",
			"hex_integer_literal", "character_literal", "true", "false":
			return extractor.StripLiteralQuotes(nodeText(a, src))
		default:
			// Non-literal first argument → honest-partial, no value.
			return ""
		}
	}
	return ""
}

// firstChildOfType returns the first direct child of n with the given type.
func firstChildOfType(n ts.Node, typ string) ts.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch != nil && ch.Type() == typ {
			return ch
		}
	}
	return nil
}

// childByType is an alias kept local to this file to avoid coupling to other
// helpers' naming; identical semantics to firstChildOfType.
func childByType(n ts.Node, typ string) ts.Node {
	return firstChildOfType(n, typ)
}
