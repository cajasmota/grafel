package kotlin

// const_valueset.go — value-carrying SCOPE.Enum value-set nodes for Kotlin
// enums and constant COLLECTIONS (#4428, extends #4429 / epic #4419). The
// shared cross-language builder in internal/extractor (EnumEntity / EnumMember /
// StripLiteralQuotes) emits a structured `members_json` [{key,value,line}]
// property on every value-set node, so a downstream cross-graph parity-audit
// can diff the literal {key,value} set without re-parsing source.
//
// Kotlin had no value-set extraction before this; three shapes map to a node:
//
//   - `enum class E(val v: String) { A("a"), B("b") }` →
//     kind_hint="kotlin_enum". Each enum_entry's name is the member; the first
//     string-literal constructor argument is its value (value-less when the
//     entry has no literal argument — no fabricated ordinal).
//   - `object Pages { const val CORE_ADMIN = "core-admin"; ... }` /
//     a class with grouped `const val` string properties →
//     kind_hint="kotlin_const_group", node named after the object/class.
//   - `val ROUTES = mapOf("a" to "x", ...)` top-level or object-level →
//     kind_hint="kotlin_const_map", each `"k" to "v"` infix pair a member.
//
// Honest-partial: a const-group node needs ≥2 string-literal `const val`
// members; a mapOf node is emitted only when every argument is a
// `"literal" to "literal"` pair (any non-literal pair disqualifies the map).

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// emitEnumValueSet builds the value-carrying SCOPE.Enum node for a Kotlin
// `enum class`, capturing each entry's first string-literal constructor
// argument as its value. ok=false when the declaration is not an enum class or
// has no entries.
func emitEnumValueSet(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := firstChildOfType(node, file.Content, "type_identifier")
	if name == "" {
		return types.EntityRecord{}, false
	}
	body := findChildByTypeKt(node, "enum_class_body")
	if body == nil {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		entry := body.Child(i)
		if entry == nil || entry.Type() != "enum_entry" {
			continue
		}
		mname := firstChildOfType(entry, file.Content, "simple_identifier")
		if mname == "" {
			continue
		}
		mval := firstStringArg(findChildByTypeKt(entry, "value_arguments"), file)
		members = append(members, extractor.EnumMember{
			Name:  mname,
			Value: mval,
			Line:  int(entry.StartPoint().Row) + 1,
		})
	}
	return extractor.EnumEntity(
		name, "kotlin", "kotlin_enum", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// emitConstGroupValueSet builds a value-set from the grouped `const val`
// string-literal properties of an object/class body. groupName names the
// enclosing object/class. ok=false when fewer than two such members exist (a
// single constant is not an enumerated value-set).
func emitConstGroupValueSet(body *sitter.Node, file extractor.FileInput, groupName string) (types.EntityRecord, bool) {
	if body == nil || groupName == "" {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		prop := body.Child(i)
		if prop == nil || prop.Type() != "property_declaration" {
			continue
		}
		if !hasConstModifier(prop, file.Content) {
			continue
		}
		name, val, ok := constValStringProp(prop, file)
		if !ok {
			continue
		}
		members = append(members, extractor.EnumMember{
			Name:  name,
			Value: val,
			Line:  int(prop.StartPoint().Row) + 1,
		})
	}
	if len(members) < 2 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		groupName, "kotlin", "kotlin_const_group", file.Path,
		int(body.StartPoint().Row)+1, int(body.EndPoint().Row)+1, members,
	)
}

// emitMapValueSet builds a value-set for a `val X = mapOf("a" to "b", ...)`
// property_declaration (top-level or object/class-level). ok=false when the
// initialiser is not a mapOf call, has no arguments, or any argument is not a
// closed `"literal" to "literal"` pair (honest-partial).
func emitMapValueSet(prop *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	varDecl := findChildByTypeKt(prop, "variable_declaration")
	if varDecl == nil {
		return types.EntityRecord{}, false
	}
	mapName := firstChildOfType(varDecl, file.Content, "simple_identifier")
	if mapName == "" {
		return types.EntityRecord{}, false
	}
	call := findChildByTypeKt(prop, "call_expression")
	if call == nil {
		return types.EntityRecord{}, false
	}
	if fn := firstChildOfType(call, file.Content, "simple_identifier"); !isMapFactory(fn) {
		return types.EntityRecord{}, false
	}
	suffix := findChildByTypeKt(call, "call_suffix")
	if suffix == nil {
		return types.EntityRecord{}, false
	}
	args := findChildByTypeKt(suffix, "value_arguments")
	if args == nil {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := 0; i < int(args.ChildCount()); i++ {
		va := args.Child(i)
		if va == nil || va.Type() != "value_argument" {
			continue
		}
		infix := findChildByTypeKt(va, "infix_expression")
		if infix == nil {
			return types.EntityRecord{}, false // not a `k to v` pair
		}
		key, val, ok := infixToPair(infix, file)
		if !ok {
			return types.EntityRecord{}, false
		}
		members = append(members, extractor.EnumMember{
			Name:  key,
			Value: val,
			Line:  int(va.StartPoint().Row) + 1,
		})
	}
	if len(members) == 0 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		mapName, "kotlin", "kotlin_const_map", file.Path,
		int(prop.StartPoint().Row)+1, int(prop.EndPoint().Row)+1, members,
	)
}

// infixToPair reads a `"key" to "value"` infix_expression: the operator must be
// the `to` infix function and both operands must be string literals. ok=false
// otherwise (a non-`to` infix, or a non-literal operand).
func infixToPair(infix *sitter.Node, file extractor.FileInput) (key, val string, ok bool) {
	// Shape: string_literal | simple_identifier("to") | string_literal.
	var lits []string
	sawTo := false
	for i := 0; i < int(infix.ChildCount()); i++ {
		ch := infix.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "string_literal":
			lits = append(lits, ktStringText(ch, file.Content))
		case "simple_identifier":
			if nodeTextKt(ch, file.Content) == "to" {
				sawTo = true
			} else {
				return "", "", false // non-literal operand
			}
		}
	}
	if !sawTo || len(lits) < 2 {
		return "", "", false
	}
	return lits[0], lits[1], true
}

// constValStringProp reads a `const val NAME = "literal"` property's name and
// string value. ok=false when the initialiser is not a single string literal.
func constValStringProp(prop *sitter.Node, file extractor.FileInput) (name, val string, ok bool) {
	varDecl := findChildByTypeKt(prop, "variable_declaration")
	if varDecl == nil {
		return "", "", false
	}
	name = firstChildOfType(varDecl, file.Content, "simple_identifier")
	if name == "" {
		return "", "", false
	}
	lit := findChildByTypeKt(prop, "string_literal")
	if lit == nil {
		return "", "", false
	}
	return name, ktStringText(lit, file.Content), true
}

// hasConstModifier reports whether a property_declaration carries the `const`
// property modifier (`modifiers > property_modifier`). The grammar represents
// the `const` keyword as the property_modifier node's own text (it has no child
// `const` token), so the modifier text is compared directly.
func hasConstModifier(prop *sitter.Node, src []byte) bool {
	mods := findChildByTypeKt(prop, "modifiers")
	if mods == nil {
		return false
	}
	for i := 0; i < int(mods.ChildCount()); i++ {
		ch := mods.Child(i)
		if ch == nil || ch.Type() != "property_modifier" {
			continue
		}
		if nodeTextKt(ch, src) == "const" {
			return true
		}
	}
	return false
}

// firstStringArg returns the inner text of the first string_literal inside a
// value_arguments node, or "" when none is present (a value-less enum entry).
func firstStringArg(args *sitter.Node, file extractor.FileInput) string {
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		va := args.Child(i)
		if va == nil || va.Type() != "value_argument" {
			continue
		}
		if lit := findChildByTypeKt(va, "string_literal"); lit != nil {
			return ktStringText(lit, file.Content)
		}
	}
	return ""
}

// ktStringText returns the inner text of a Kotlin string_literal node. The
// grammar exposes the body as `string_content` child(ren); falling back to
// StripLiteralQuotes covers an empty / interpolated literal.
func ktStringText(lit *sitter.Node, src []byte) string {
	if lit == nil {
		return ""
	}
	if content := findChildByTypeKt(lit, "string_content"); content != nil {
		return nodeTextKt(content, src)
	}
	return extractor.StripLiteralQuotes(nodeTextKt(lit, src))
}

// isMapFactory reports whether fn is a Kotlin standard map factory whose
// arguments are `key to value` pairs.
func isMapFactory(fn string) bool {
	switch fn {
	case "mapOf", "mutableMapOf", "linkedMapOf", "sortedMapOf", "hashMapOf":
		return true
	}
	return false
}

// findChildByTypeKt returns the first direct child of node with the given type,
// or nil.
func findChildByTypeKt(node *sitter.Node, t string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == t {
			return ch
		}
	}
	return nil
}

// nodeTextKt returns the source text spanned by node.
func nodeTextKt(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}
