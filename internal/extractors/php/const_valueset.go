package php

// const_valueset.go — value-carrying SCOPE.Enum value-set nodes for PHP
// constant collections (data-model, epic #3628 / #4419; extends #4420/#4429
// and the sibling Ruby work #4427). PHP source-of-truth maps and enumerations
// held in `const` arrays, class-constant groups, PHP 8.1 backed enums, and
// `define()` maps were invisible to search_entities and could not be diffed by
// a downstream cross-graph parity-audit. This generalises the same
// SCOPE.Enum value-set model used by Python const maps, TS const-objects,
// Rails enums and Ruby constant collections to the PHP constant shapes:
//
//   - array-const map (file- or class-scope):
//        const PERMISSION_PAGES = ['core_admin' => 'core-admin', 'user' => 'u'];
//     → value-set named PERMISSION_PAGES, members core_admin=core-admin, ...
//
//   - class-constant GROUP (≥1 scalar `const` in a class/interface/trait body):
//        class Pages { const CORE_ADMIN = 'core-admin'; const USER = 'user'; }
//     → value-set named after the class (Pages), members CORE_ADMIN=core-admin, ...
//
//   - PHP 8.1 backed enum (case name → backing value):
//        enum Status: string { case Active = 'active'; case Done = 'done'; }
//     → value-set named Status, members Active=active, Done=done
//     (additive to the existing SCOPE.Schema/enum node emitted by buildEnum).
//
//   - define() map:
//        define('FOO', ['a' => 1, 'b' => 2]);
//     → value-set named FOO, members a=1, b=2
//
// Reuses the shared extractor.EnumEntity helper (no new Kind, no helper
// change) so each member is recorded in the structured members set
// (Properties["members"] / ["values"]) alongside every other language.
//
// Honest-partial: an empty collection emits no node; a NON-LITERAL value
// records its source EXPRESSION TEXT (not dropped) so the key set stays
// complete and a parity-audit can see the value is dynamic. A bare scalar
// constant outside a class is not a collection and emits nothing on its own;
// a scalar array element (list, no `=>`) records the element as a value-less
// member (its literal in the Name slot) so the member roster stays complete.

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitConstValueSets walks the CST and appends one SCOPE.Enum value-set node
// per recognised PHP constant collection (array-const map, class-const group,
// backed enum, define() map). Called from Extract after the structural walk.
func emitConstValueSets(node ts.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	walkConstValueSets(node, file, "", out)
}

// walkConstValueSets is a class-aware DFS. parentType is the bare name of the
// immediately-enclosing class/interface/trait (or "" at file scope) so a group
// of scalar class constants can be named after its declaring type.
func walkConstValueSets(node ts.Node, file extractor.FileInput, parentType string, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration", "interface_declaration", "trait_declaration":
		typeName := childFieldText(node, "name", file.Content)
		body := phpDeclBody(node)
		if body != nil {
			// Array-const maps inside the body are still emitted individually
			// (named by the const), while scalar class constants are grouped
			// into one value-set named after the declaring type.
			if rec, ok := buildClassConstGroup(typeName, body, file); ok {
				*out = append(*out, rec)
			}
		}
		// Recurse with this type as the parent so nested array-const maps and
		// enums declared in the body are still visited.
		for i := range int(node.ChildCount()) {
			walkConstValueSets(node.Child(i), file, typeName, out)
		}
		return

	case "const_declaration":
		// Array-valued const → its own value-set (file- or class-scope).
		for _, rec := range buildArrayConstValueSets(node, file) {
			*out = append(*out, rec)
		}

	case "enum_declaration":
		if rec, ok := buildEnumValueSet(node, file); ok {
			*out = append(*out, rec)
		}

	case "function_call_expression":
		if rec, ok := buildDefineValueSet(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range int(node.ChildCount()) {
		walkConstValueSets(node.Child(i), file, parentType, out)
	}
}

// buildArrayConstValueSets returns a value-set node for every `const_element`
// in a const_declaration whose value is an array literal. A `const` with a
// scalar value contributes nothing here (scalars are grouped at class scope by
// buildClassConstGroup). Returns nil for empty arrays (honest-partial).
func buildArrayConstValueSets(node ts.Node, file extractor.FileInput) []types.EntityRecord {
	var recs []types.EntityRecord
	for i := range int(node.ChildCount()) {
		el := node.Child(i)
		if el == nil || el.Type() != "const_element" {
			continue
		}
		name := childFieldText(el, "name", file.Content)
		if name == "" {
			name = firstNamedChildText(el, "name", file.Content)
		}
		arr := firstChildOfTypePHP(el, "array_creation_expression")
		if name == "" || arr == nil {
			continue
		}
		members := arrayMembers(arr, file.Content)
		if rec, ok := extractor.EnumEntity(
			name, "php", "php_const_map", file.Path,
			int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
		); ok {
			recs = append(recs, rec)
		}
	}
	return recs
}

// buildClassConstGroup collects every scalar (non-array) class constant in a
// declaration body into a single value-set named after the declaring type.
// Array-valued constants are excluded (they get their own node via
// buildArrayConstValueSets). Returns ok=false when the type has no scalar
// constants (honest-partial).
func buildClassConstGroup(typeName string, body ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	if typeName == "" || body == nil {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	startLine := int(body.StartPoint().Row) + 1
	endLine := int(body.EndPoint().Row) + 1
	for i := range int(body.ChildCount()) {
		cd := body.Child(i)
		if cd == nil || cd.Type() != "const_declaration" {
			continue
		}
		for j := range int(cd.ChildCount()) {
			el := cd.Child(j)
			if el == nil || el.Type() != "const_element" {
				continue
			}
			// Skip array-valued constants — they are emitted on their own.
			if firstChildOfTypePHP(el, "array_creation_expression") != nil {
				continue
			}
			name := childFieldText(el, "name", file.Content)
			if name == "" {
				name = firstNamedChildText(el, "name", file.Content)
			}
			if name == "" {
				continue
			}
			members = append(members, extractor.EnumMember{
				Name:  name,
				Value: constElementValue(el, file.Content),
			})
		}
	}
	return extractor.EnumEntity(
		typeName, "php", "php_class_const", file.Path,
		startLine, endLine, members,
	)
}

// buildEnumValueSet emits a value-set node for a PHP 8.1 enum_declaration,
// mapping each case name to its backing value (pure enums emit value-less
// members). Additive to the SCOPE.Schema/enum node from buildEnum.
func buildEnumValueSet(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	body := phpDeclBody(node)
	if body == nil {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	for i := range int(body.ChildCount()) {
		ch := body.Child(i)
		if ch == nil || ch.Type() != "enum_case" {
			continue
		}
		caseName := childFieldText(ch, "name", file.Content)
		if caseName == "" {
			continue
		}
		members = append(members, extractor.EnumMember{
			Name:  caseName,
			Value: literalValueNode(ch.ChildByFieldName("value"), file.Content),
		})
	}
	return extractor.EnumEntity(
		name, "php", "php_enum", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// buildDefineValueSet emits a value-set node for a `define('NAME', [...])`
// call whose second argument is an array literal. Non-array define() calls
// and dynamic names emit nothing (honest-partial).
func buildDefineValueSet(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	fn := node.ChildByFieldName("function")
	if fn == nil {
		fn = firstChildOfTypePHP(node, "name")
	}
	if fn == nil || nodeTextPHP(fn, file.Content) != "define" {
		return types.EntityRecord{}, false
	}
	args := node.ChildByFieldName("arguments")
	if args == nil {
		args = firstChildOfTypePHP(node, "arguments")
	}
	if args == nil {
		return types.EntityRecord{}, false
	}

	var name string
	var arr ts.Node
	for i := range int(args.ChildCount()) {
		a := args.Child(i)
		if a == nil || a.Type() != "argument" {
			continue
		}
		inner := firstNamedChild(a)
		if inner == nil {
			continue
		}
		if name == "" {
			if inner.Type() == "string" {
				name = extractor.StripLiteralQuotes(nodeTextPHP(inner, file.Content))
			}
			continue
		}
		if arr == nil && inner.Type() == "array_creation_expression" {
			arr = inner
		}
	}
	if name == "" || arr == nil {
		return types.EntityRecord{}, false
	}
	members := arrayMembers(arr, file.Content)
	return extractor.EnumEntity(
		name, "php", "php_define_map", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// arrayMembers builds EnumMembers from a PHP array_creation_expression.
// A keyed element (`'k' => v`) becomes member k with value v; a list element
// (bare value, no `=>`) becomes a value-less member whose name is the literal
// (so the roster stays complete for a list of constants). Non-literal values
// record their source expression text rather than being dropped.
func arrayMembers(arr ts.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	for i := range int(arr.ChildCount()) {
		el := arr.Child(i)
		if el == nil || el.Type() != "array_element_initializer" {
			continue
		}
		keyNode, valNode := arrayElementKeyValue(el)
		if keyNode != nil {
			members = append(members, extractor.EnumMember{
				Name:  literalValueNode(keyNode, src),
				Value: literalValueNode(valNode, src),
			})
			continue
		}
		// List element (no key): record the literal in the Name slot.
		if valNode != nil {
			members = append(members, extractor.EnumMember{
				Name: literalValueNode(valNode, src),
			})
		}
	}
	return members
}

// arrayElementKeyValue splits an array_element_initializer into its key and
// value nodes. When the element has a `=>` token the named child before it is
// the key and the one after it the value; otherwise it is a list element and
// the (single) named child is the value (key == nil).
func arrayElementKeyValue(el ts.Node) (key, val ts.Node) {
	var named []ts.Node
	hasArrow := false
	for i := range int(el.ChildCount()) {
		ch := el.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "=>" {
			hasArrow = true
			continue
		}
		if ch.IsNamed() {
			named = append(named, ch)
		}
	}
	if hasArrow && len(named) >= 2 {
		return named[0], named[1]
	}
	if len(named) >= 1 {
		return nil, named[len(named)-1]
	}
	return nil, nil
}

// constElementValue returns the statically-known literal of a const_element's
// value, or the raw expression text for a non-literal value (honest-partial).
func constElementValue(el ts.Node, src []byte) string {
	// The value node is the last named child after the `=` token.
	var val ts.Node
	seenEq := false
	for i := range int(el.ChildCount()) {
		ch := el.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "=" {
			seenEq = true
			continue
		}
		if seenEq && ch.IsNamed() {
			val = ch
		}
	}
	if val == nil {
		return ""
	}
	return literalValueNode(val, src)
}

// literalValueNode returns the normalised literal of a value node. String
// literals are unquoted; integer / float / boolean / name literals are
// returned verbatim. Any other (computed / dynamic) expression returns its
// source text so the value is recorded rather than dropped (honest-partial).
func literalValueNode(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "string", "encapsed_string":
		return extractor.StripLiteralQuotes(nodeTextPHP(n, src))
	case "integer", "float", "boolean", "name", "true", "false", "null":
		return nodeTextPHP(n, src)
	}
	// Non-literal: record the raw expression text.
	return nodeTextPHP(n, src)
}

// --- small node helpers (local to this file) ---------------------------------

func firstChildOfTypePHP(n ts.Node, typ string) ts.Node {
	if n == nil {
		return nil
	}
	for i := range int(n.ChildCount()) {
		ch := n.Child(i)
		if ch != nil && ch.Type() == typ {
			return ch
		}
	}
	return nil
}

func firstNamedChild(n ts.Node) ts.Node {
	if n == nil {
		return nil
	}
	for i := range int(n.ChildCount()) {
		ch := n.Child(i)
		if ch != nil && ch.IsNamed() {
			return ch
		}
	}
	return nil
}

// firstNamedChildText returns the text of the first named child whose type is
// excludeAfter-agnostic match by type, used as a fallback when the "name"
// field is unlabelled in the grammar revision.
func firstNamedChildText(n ts.Node, typ string, src []byte) string {
	if c := firstChildOfTypePHP(n, typ); c != nil {
		return nodeTextPHP(c, src)
	}
	return ""
}

func nodeTextPHP(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}
