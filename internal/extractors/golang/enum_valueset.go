package golang

// enum_valueset.go — value-carrying SCOPE.Enum value-set nodes for Go
// (data-model, epic #3628 / completes #3806). Go has no `enum` keyword; the
// idiom is a named integer/string type plus a parenthesised const block typed
// by it:
//
//	type Status int
//	const (
//	    Active Status = iota
//	    Inactive
//	    Pending
//	)
//
// This reuses the shared cross-language builder in internal/extractor
// (EnumEntity / EnumMember / StripLiteralQuotes) so Go const-group enums
// converge on the same node model as Python, C#, TS, Java and Rails.
//
// Detection (precision-first):
//   - The const block's FIRST const_spec must name a type (the second
//     identifier in `Name Type = ...`), AND that type must be declared in the
//     same file as a named type (`type Status int`). A const block typed by a
//     builtin only (or untyped) is NOT treated as an enum value-set.
//   - The named type carries implicitly to subsequent specs (Go repetition),
//     so every const_spec in the block is a member of the enum.
//
// Values:
//   - iota-based members are recorded value-less (member name only) — the
//     extractor does not materialise the synthetic ordinal (honest-partial).
//   - explicit literal members (`Red Color = "red"`, `Mask Flag = 1`) capture
//     the literal value.

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractGoEnums scans the file for const blocks typed by a same-file named
// type and emits one SCOPE.Enum value-set node per such block.
func extractGoEnums(root ts.Node, src []byte, filePath string) []types.EntityRecord {
	if root == nil {
		return nil
	}
	named := collectNamedTypes(root, src)
	if len(named) == 0 {
		return nil
	}
	var out []types.EntityRecord
	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "const_declaration" {
			if rec, ok := buildGoEnumValueSet(n, src, filePath, named); ok {
				out = append(out, rec)
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

// collectNamedTypes returns the set of type names declared in the file whose
// underlying type is a primitive (int/string family) — the Go enum idiom. A
// struct/interface/func type is excluded (those are never enum bases).
func collectNamedTypes(root ts.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "type_spec" {
			name := nodeText(firstChildOfType(n, "type_identifier"), src)
			// The underlying type is the second named child.
			if name != "" {
				if under := typeSpecUnderlying(n); under != nil && isEnumBaseType(under) {
					out[name] = true
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

// typeSpecUnderlying returns the underlying-type node of a type_spec (the node
// after the type name), or nil.
func typeSpecUnderlying(spec ts.Node) ts.Node {
	seenName := false
	for i := 0; i < int(spec.ChildCount()); i++ {
		ch := spec.Child(i)
		if ch == nil || !ch.IsNamed() {
			continue
		}
		if !seenName {
			seenName = true
			continue
		}
		return ch
	}
	return nil
}

// isEnumBaseType reports whether an underlying-type node is a primitive base a
// Go enum is built on (a bare type_identifier naming an int/string/numeric
// builtin). Struct/interface/map/func/etc. underlying types are not enum bases.
func isEnumBaseType(under ts.Node) bool {
	return under.Type() == "type_identifier"
}

// buildGoEnumValueSet builds the SCOPE.Enum node for one const block, when the
// block is typed by a same-file named type. enumName is taken from the first
// const_spec's type. Returns ok=false otherwise.
func buildGoEnumValueSet(constDecl ts.Node, src []byte, filePath string, named map[string]bool) (types.EntityRecord, bool) {
	// Find the enum type from the first const_spec carrying a type identifier.
	var enumType string
	for i := 0; i < int(constDecl.ChildCount()); i++ {
		spec := constDecl.Child(i)
		if spec == nil || spec.Type() != "const_spec" {
			continue
		}
		if tn := firstChildOfType(spec, "type_identifier"); tn != nil {
			enumType = nodeText(tn, src)
			break
		}
	}
	if enumType == "" || !named[enumType] {
		return types.EntityRecord{}, false
	}

	var members []extractor.EnumMember
	for i := 0; i < int(constDecl.ChildCount()); i++ {
		spec := constDecl.Child(i)
		if spec == nil || spec.Type() != "const_spec" {
			continue
		}
		// Member name(s): the leading identifier(s) before the type/`=`.
		name := nodeText(firstChildOfType(spec, "identifier"), src)
		if name == "" || name == "_" {
			continue
		}
		members = append(members, extractor.EnumMember{
			Name:  name,
			Value: goConstSpecLiteral(spec, src),
			Line:  int(spec.StartPoint().Row) + 1,
		})
	}

	return extractor.EnumEntity(
		enumType, "go", "go_iota", filePath,
		int(constDecl.StartPoint().Row)+1, int(constDecl.EndPoint().Row)+1, members,
	)
}

// goConstSpecLiteral returns the statically-known literal value of a const_spec
// RHS, or "" for iota / non-literal / absent values (honest-partial).
func goConstSpecLiteral(spec ts.Node, src []byte) string {
	el := firstChildOfType(spec, "expression_list")
	if el == nil {
		return ""
	}
	for i := 0; i < int(el.ChildCount()); i++ {
		ch := el.Child(i)
		if ch == nil || !ch.IsNamed() {
			continue
		}
		switch ch.Type() {
		case "interpreted_string_literal", "raw_string_literal":
			return extractor.StripLiteralQuotes(nodeText(ch, src))
		case "int_literal", "float_literal":
			return nodeText(ch, src)
		default:
			// iota, binary_expression, call, etc. → not a captured literal.
			return ""
		}
	}
	return ""
}
