package csharp

// const_valueset.go — value-carrying SCOPE.Enum value-set nodes for C# constant
// COLLECTIONS (#4428, extends #4429 / epic #4419). The shared cross-language
// builder in internal/extractor (EnumEntity / EnumMember / StripLiteralQuotes)
// already emits a structured `members_json` [{key,value,line}] property on every
// value-set node, so a downstream cross-graph parity-audit can diff the literal
// {key,value} set without re-parsing source. C# enums already feed EnumEntity
// (buildEnumValueSet) and inherit members_json for free; THIS file adds the two
// constant-collection shapes a `.cs` source-of-truth uses instead of an enum:
//
//   - `static readonly Dictionary<K,V> X = new() { {"k","v"}, ... }` /
//     `new Dictionary<K,V> { ["k"] = "v", ... }` / collection-initializer const
//     maps → kind_hint="csharp_const_map". Captured per {key,value,line}.
//   - a class whose const / static-readonly STRING fields form a group
//     (`public const string CoreAdmin = "core-admin"; ...`) → one value-set
//     named after the class, kind_hint="csharp_const_group", each field a member.
//
// Honest-partial: only closed, all-string-literal collections emit a node. A
// dictionary entry whose value is a call / identifier / non-literal expression
// disqualifies the map (it is not a closed enumerated value-set). A class with
// fewer than two string-const fields emits no group node (a single constant is
// not a value-set).

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitConstCollectionsForClass scans the direct field_declaration children of a
// class body and emits value-set nodes for the two C# constant-collection
// shapes. className names the enclosing class (used as the const-group node
// name). Appended nodes never replace the field entities the default walk emits.
func emitConstCollectionsForClass(body ts.Node, file extractor.FileInput, className string, out *[]types.EntityRecord) {
	if body == nil {
		return
	}
	var groupMembers []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		fld := body.Child(i)
		if fld == nil || fld.Type() != "field_declaration" {
			continue
		}
		isConst, isStaticReadonly := fieldConstKind(fld)
		if !isConst && !isStaticReadonly {
			continue
		}
		// A Dictionary/collection-initialiser const map is its own value-set.
		if vs, ok := buildConstMapValueSet(fld, file); ok {
			*out = append(*out, vs)
			continue
		}
		// Otherwise, a string-literal scalar const contributes to the
		// class-level const group.
		if name, val, line, ok := constStringField(fld, file); ok {
			groupMembers = append(groupMembers, extractor.EnumMember{Name: name, Value: val, Line: line})
		}
	}
	// A class-level const-string group is a value-set only when it has at
	// least two members (a single constant is not an enumerated set).
	if className != "" && len(groupMembers) >= 2 {
		start := int(body.StartPoint().Row) + 1
		end := int(body.EndPoint().Row) + 1
		if ent, ok := extractor.EnumEntity(
			className, "csharp", "csharp_const_group", file.Path, start, end, groupMembers,
		); ok {
			*out = append(*out, ent)
		}
	}
}

// fieldConstKind reports whether a field_declaration carries the `const`
// modifier, or both `static` and `readonly` modifiers.
func fieldConstKind(fld ts.Node) (isConst, isStaticReadonly bool) {
	var static, readonly bool
	for i := 0; i < int(fld.ChildCount()); i++ {
		ch := fld.Child(i)
		if ch == nil || ch.Type() != "modifier" {
			continue
		}
		switch txt := ch.Child(0); {
		case txt == nil:
		default:
			switch txt.Type() {
			case "const":
				isConst = true
			case "static":
				static = true
			case "readonly":
				readonly = true
			}
		}
	}
	return isConst, static && readonly
}

// constStringField extracts a scalar string-literal constant field's name,
// value and 1-indexed line. ok=false when the field is not a single
// string-literal-valued declarator (e.g. a Dictionary map, an int const, or a
// non-literal initialiser).
func constStringField(fld ts.Node, file extractor.FileInput) (name, value string, line int, ok bool) {
	decl := findChildByType(fld, "variable_declaration")
	if decl == nil {
		return "", "", 0, false
	}
	// Only `string`-typed scalar consts join the group (`predefined_type`
	// "string"); a Dictionary<...> generic_name is handled as a map.
	pt := findChildByType(decl, "predefined_type")
	if pt == nil || nodeText(pt, file.Content) != "string" {
		return "", "", 0, false
	}
	vd := findChildByType(decl, "variable_declarator")
	if vd == nil {
		return "", "", 0, false
	}
	var mname, mval string
	gotVal := false
	for i := 0; i < int(vd.ChildCount()); i++ {
		ch := vd.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "identifier" && mname == "" {
			mname = nodeText(ch, file.Content)
			continue
		}
		if mname != "" && ch.IsNamed() {
			if ch.Type() != "string_literal" {
				return "", "", 0, false // non-literal initialiser
			}
			mval = stringLiteralText(ch, file.Content)
			gotVal = true
		}
	}
	if mname == "" || !gotVal {
		return "", "", 0, false
	}
	return mname, mval, int(vd.StartPoint().Row) + 1, true
}

// buildConstMapValueSet emits a value-set for a Dictionary / collection-
// initialiser constant map field. It handles both the object-initialiser
// `{ "k", "v" }` element form and the indexer `["k"] = "v"` form. ok=false when
// the field is not a Dictionary-shaped map, has no initialiser entries, or any
// entry is not a {string-literal key, string-literal value} pair (honest-partial
// closed value-set).
func buildConstMapValueSet(fld ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	decl := findChildByType(fld, "variable_declaration")
	if decl == nil {
		return types.EntityRecord{}, false
	}
	// The declared type must be a generic Dictionary<...> (or Immutable*) — a
	// plain string const is not a map.
	gn := findChildByType(decl, "generic_name")
	if gn == nil {
		return types.EntityRecord{}, false
	}
	typeName := nodeText(findChildByType(gn, "identifier"), file.Content)
	if !isDictionaryType(typeName) {
		return types.EntityRecord{}, false
	}
	vd := findChildByType(decl, "variable_declarator")
	if vd == nil {
		return types.EntityRecord{}, false
	}
	mapName := nodeText(findChildByType(vd, "identifier"), file.Content)
	if mapName == "" {
		return types.EntityRecord{}, false
	}
	// Locate the outer initializer_expression: it hangs off the
	// implicit_object_creation_expression (`new() {...}`) or
	// object_creation_expression (`new Dictionary<...> {...}`).
	creation := findChildByType(vd, "implicit_object_creation_expression")
	if creation == nil {
		creation = findChildByType(vd, "object_creation_expression")
	}
	if creation == nil {
		return types.EntityRecord{}, false
	}
	init := findChildByType(creation, "initializer_expression")
	if init == nil {
		return types.EntityRecord{}, false
	}
	members, ok := collectMapInitMembers(init, file)
	if !ok || len(members) == 0 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		mapName, "csharp", "csharp_const_map", file.Path,
		int(fld.StartPoint().Row)+1, int(fld.EndPoint().Row)+1, members,
	)
}

// collectMapInitMembers walks an initializer_expression's entries, accepting
// either nested `{ "k", "v" }` element initialisers or `["k"] = "v"` indexer
// assignments. It returns ok=false the moment any entry is not a
// string-literal/string-literal pair so a non-closed map emits no node.
func collectMapInitMembers(init ts.Node, file extractor.FileInput) ([]extractor.EnumMember, bool) {
	var members []extractor.EnumMember
	for i := 0; i < int(init.ChildCount()); i++ {
		entry := init.Child(i)
		if entry == nil || !entry.IsNamed() {
			continue
		}
		switch entry.Type() {
		case "initializer_expression":
			// `{ "key", "value" }` — two string-literal element children.
			key, val, ok := twoStringLiterals(entry, file)
			if !ok {
				return nil, false
			}
			members = append(members, extractor.EnumMember{Name: key, Value: val, Line: int(entry.StartPoint().Row) + 1})
		case "assignment_expression":
			// `["key"] = "value"` — element_binding_expression = string_literal.
			key, val, ok := indexerAssignment(entry, file)
			if !ok {
				return nil, false
			}
			members = append(members, extractor.EnumMember{Name: key, Value: val, Line: int(entry.StartPoint().Row) + 1})
		default:
			return nil, false
		}
	}
	return members, true
}

// twoStringLiterals reads the first two string_literal children of a nested
// `{ "k", "v" }` element initializer. ok=false when fewer than two string
// literals are present (a non-string key or value disqualifies the map).
func twoStringLiterals(entry ts.Node, file extractor.FileInput) (key, val string, ok bool) {
	var lits []string
	for i := 0; i < int(entry.ChildCount()); i++ {
		ch := entry.Child(i)
		if ch == nil || ch.Type() != "string_literal" {
			continue
		}
		lits = append(lits, stringLiteralText(ch, file.Content))
	}
	if len(lits) < 2 {
		return "", "", false
	}
	return lits[0], lits[1], true
}

// indexerAssignment reads an `["key"] = "value"` assignment_expression: the key
// is the string_literal inside the element_binding_expression, the value is the
// string_literal RHS. ok=false when either side is not a string literal.
func indexerAssignment(entry ts.Node, file extractor.FileInput) (key, val string, ok bool) {
	binding := findChildByType(entry, "element_binding_expression")
	if binding == nil {
		return "", "", false
	}
	keyLit := findDeepStringLiteral(binding, file)
	if keyLit == "" {
		return "", "", false
	}
	// The RHS value is the first top-level string_literal child of the
	// assignment (after the element_binding_expression / `=`).
	var valLit string
	for i := 0; i < int(entry.ChildCount()); i++ {
		ch := entry.Child(i)
		if ch != nil && ch.Type() == "string_literal" {
			valLit = stringLiteralText(ch, file.Content)
			break
		}
	}
	if valLit == "" {
		return "", "", false
	}
	return keyLit, valLit, true
}

// findDeepStringLiteral returns the text of the first string_literal found in a
// shallow DFS of node, or "" when none is present. Used to pull the key out of
// an `["key"]` element_binding_expression's `argument` wrapper.
func findDeepStringLiteral(node ts.Node, file extractor.FileInput) string {
	if node == nil {
		return ""
	}
	if node.Type() == "string_literal" {
		return stringLiteralText(node, file.Content)
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		if s := findDeepStringLiteral(node.Child(i), file); s != "" {
			return s
		}
	}
	return ""
}

// stringLiteralText returns the inner text of a C# string_literal node, with
// the surrounding quotes stripped. Tree-sitter exposes the content as a
// `string_literal_content` child; falling back to StripLiteralQuotes over the
// raw token covers verbatim/interpolated tokens that lack that child.
func stringLiteralText(lit ts.Node, src []byte) string {
	if lit == nil {
		return ""
	}
	if content := findChildByType(lit, "string_literal_content"); content != nil {
		return nodeText(content, src)
	}
	return extractor.StripLiteralQuotes(nodeText(lit, src))
}

// isDictionaryType reports whether a generic type's bare name is a recognised
// map-like collection (Dictionary / IDictionary / ImmutableDictionary /
// SortedDictionary / ConcurrentDictionary / ReadOnlyDictionary).
func isDictionaryType(name string) bool {
	switch name {
	case "Dictionary", "IDictionary", "IReadOnlyDictionary",
		"ImmutableDictionary", "IImmutableDictionary",
		"SortedDictionary", "ConcurrentDictionary", "ReadOnlyDictionary":
		return true
	}
	return false
}
