package java

// const_collection.go — value-carrying SCOPE.Enum value-set nodes for Java
// constant COLLECTIONS (data-model, epic #4419 / #4334, extends #4420/#4429).
//
// #4429 indexed Python/TS module-level const maps and TS `as const` objects as
// queryable SCOPE.Enum value-sets so a downstream parity-audit can diff the
// literal {key,value} set without re-parsing source. This file adds the Java
// const-COLLECTION shapes, feeding the SAME shared builder
// (extractor.EnumEntity / EnumMember / StripLiteralQuotes) so Java converges on
// the identical value-set model — NO new Kind, reuse SCOPE.Enum.
//
// Shapes detected (all must be a `static final` field, or an interface
// constant, whose value is a closed all-/mostly-literal collection):
//
//   - `static final Map<K,V> X = Map.of("a", "b", ...)`   → java_const_map
//   - `static final Map<K,V> X = Map.ofEntries(entry("a","b"), ...)`
//   - Guava `ImmutableMap.of("a","b", ...)`
//   - Guava `ImmutableMap.<K,V>builder().put("a","b").put(...).build()`
//   - `static final String[] X = {"a", "b"}`              → java_const_array
//   - a GROUP of `String FOO = "foo";` constants in a class/interface
//     (≥2 sibling static-final string/number constants) → java_const_group
//
// Honest-partial: a key or value that is not a string/number/bool literal is
// captured by its source expression text (so the member is still enumerable),
// mirroring the cross-language honest-partial convention. A collection with no
// statically-resolvable key is skipped.

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// buildJavaConstCollections inspects a class/interface/enum body and emits
// SCOPE.Enum value-set nodes for every constant collection it recognises:
//   - each `static final` map/array field whose initializer is a closed
//     literal collection → one value-set named "<Type>.<FIELD>";
//   - the GROUP of scalar string/number constants declared directly in the
//     body (≥2) → one value-set named "<Type>" with kind_hint java_const_group.
//
// typeName is the enclosing class/interface/enum simple name (already package-
// unqualified, matching buildField's parentType). bodyNode is the
// class_body / interface_body / enum_body node.
func buildJavaConstCollections(typeName string, bodyNode ts.Node, file extractor.FileInput) []types.EntityRecord {
	if bodyNode == nil || typeName == "" {
		return nil
	}
	var out []types.EntityRecord
	var groupMembers []extractor.EnumMember

	for i := 0; i < int(bodyNode.ChildCount()); i++ {
		ch := bodyNode.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "field_declaration":
			// Only `static final` fields are constants.
			if !javaFieldIsStaticFinal(ch) {
				continue
			}
			decl, init := javaFieldDeclaratorInit(ch)
			if decl == nil || init == nil {
				continue
			}
			fname := childFieldText(decl, "name", file.Content)
			if fname == "" {
				continue
			}
			// Collection-shaped initializers → one value-set per field.
			if members, hint, ok := javaCollectionMembers(init, file.Content); ok {
				if rec, rok := extractor.EnumEntity(
					typeName+"."+fname, "java", hint, file.Path,
					int(ch.StartPoint().Row)+1, int(ch.EndPoint().Row)+1, members,
				); rok {
					out = append(out, rec)
				}
				continue
			}
			// Scalar constant (String FOO = "foo"; int LIMIT = 5;) → collect
			// into the per-type constant group.
			if v, ok := javaScalarLiteral(init, file.Content); ok && javaConstName(fname) {
				groupMembers = append(groupMembers, extractor.EnumMember{
					Name: fname, Value: v, Line: int(ch.StartPoint().Row) + 1,
				})
			}

		case "constant_declaration":
			// Interface constants are implicitly `public static final`.
			decl, init := javaFieldDeclaratorInit(ch)
			if decl == nil || init == nil {
				continue
			}
			fname := childFieldText(decl, "name", file.Content)
			if fname == "" {
				continue
			}
			if members, hint, ok := javaCollectionMembers(init, file.Content); ok {
				if rec, rok := extractor.EnumEntity(
					typeName+"."+fname, "java", hint, file.Path,
					int(ch.StartPoint().Row)+1, int(ch.EndPoint().Row)+1, members,
				); rok {
					out = append(out, rec)
				}
				continue
			}
			if v, ok := javaScalarLiteral(init, file.Content); ok && javaConstName(fname) {
				groupMembers = append(groupMembers, extractor.EnumMember{
					Name: fname, Value: v, Line: int(ch.StartPoint().Row) + 1,
				})
			}
		}
	}

	// A GROUP of ≥2 scalar constants in one type is a value-set (the classic
	// "constants interface" / permission-name holder). A single lone constant
	// is left as an ordinary field — it is not a comparable value-set.
	if len(groupMembers) >= 2 {
		if rec, ok := extractor.EnumEntity(
			typeName, "java", "java_const_group", file.Path,
			groupMembers[0].Line, groupMembers[len(groupMembers)-1].Line, groupMembers,
		); ok {
			out = append(out, rec)
		}
	}
	return out
}

// javaFieldIsStaticFinal reports whether a field_declaration carries BOTH the
// `static` and `final` modifiers (the Java constant idiom).
func javaFieldIsStaticFinal(field ts.Node) bool {
	mods := firstChildOfType(field, "modifiers")
	if mods == nil {
		return false
	}
	hasStatic, hasFinal := false, false
	for i := 0; i < int(mods.ChildCount()); i++ {
		switch mods.Child(i).Type() {
		case "static":
			hasStatic = true
		case "final":
			hasFinal = true
		}
	}
	return hasStatic && hasFinal
}

// javaFieldDeclaratorInit returns the variable_declarator and its initializer
// (the named node after `=`) for a field_declaration / constant_declaration.
// Returns nil,nil when there is no single-variable initializer.
func javaFieldDeclaratorInit(field ts.Node) (decl ts.Node, init ts.Node) {
	decl = firstChildOfType(field, "variable_declarator")
	if decl == nil {
		return nil, nil
	}
	init = decl.ChildByFieldName("value")
	if init == nil {
		// Fall back: first named child after the `=` token.
		seenEq := false
		for i := 0; i < int(decl.ChildCount()); i++ {
			c := decl.Child(i)
			if c.Type() == "=" {
				seenEq = true
				continue
			}
			if seenEq && c.IsNamed() {
				init = c
				break
			}
		}
	}
	return decl, init
}

// javaCollectionMembers recognises a closed literal collection initializer and
// returns its members plus the kind_hint. Recognised:
//   - Map.of(...) / ImmutableMap.of(...)                     → java_const_map
//   - Map.ofEntries(entry(...), ...)                         → java_const_map
//   - ImmutableMap.builder().put(...)...build()              → java_const_map
//   - array_initializer { lit, lit, ... }                    → java_const_array
//
// ok=false for any other initializer shape.
func javaCollectionMembers(init ts.Node, src []byte) ([]extractor.EnumMember, string, bool) {
	switch init.Type() {
	case "array_initializer":
		members := javaArrayMembers(init, src)
		if len(members) == 0 {
			return nil, "", false
		}
		return members, "java_const_array", true
	case "method_invocation":
		if members, ok := javaMapInvocationMembers(init, src); ok {
			return members, "java_const_map", true
		}
	}
	return nil, "", false
}

// javaArrayMembers returns each literal element of a `{ "a", "b" }`
// array_initializer as a self-keyed member (Name==Value==literal), so a
// constant string array is enumerable as a value-set. The 0-based element
// index seeds nothing; the literal text is the identity.
func javaArrayMembers(arr ts.Node, src []byte) []extractor.EnumMember {
	var out []extractor.EnumMember
	for i := 0; i < int(arr.NamedChildCount()); i++ {
		el := arr.NamedChild(i)
		if el == nil {
			continue
		}
		v, ok := javaLiteralText(el, src)
		if !ok {
			// Non-literal element → capture its source expr text so the
			// member stays enumerable (honest-partial).
			v = strings.TrimSpace(nodeText(el, src))
		}
		if v == "" {
			continue
		}
		out = append(out, extractor.EnumMember{
			Name: v, Value: v, Line: int(el.StartPoint().Row) + 1,
		})
	}
	return out
}

// javaMapInvocationMembers handles the three map-factory shapes by inspecting
// the receiver/leaf of a method_invocation chain. Returns the {key,value}
// members and ok=true only when at least one key was resolved.
func javaMapInvocationMembers(call ts.Node, src []byte) ([]extractor.EnumMember, bool) {
	leaf := javaInvocationLeaf(call, src)
	switch leaf {
	case "of":
		// Map.of(k,v,k,v,...) / ImmutableMap.of(k,v,...): flat alternating args.
		args := call.ChildByFieldName("arguments")
		if args == nil {
			return nil, false
		}
		members := javaFlatPairMembers(args, src)
		return members, len(members) > 0
	case "ofEntries":
		// Map.ofEntries(entry(k,v), entry(k,v), ...).
		args := call.ChildByFieldName("arguments")
		if args == nil {
			return nil, false
		}
		var out []extractor.EnumMember
		for i := 0; i < int(args.NamedChildCount()); i++ {
			e := args.NamedChild(i)
			if e == nil || e.Type() != "method_invocation" {
				continue
			}
			ea := e.ChildByFieldName("arguments")
			if ea == nil {
				continue
			}
			out = append(out, javaFlatPairMembers(ea, src)...)
		}
		return out, len(out) > 0
	case "build":
		// ImmutableMap.builder().put(k,v).put(k,v).build(): walk the receiver
		// chain collecting every .put(...) call's args.
		members := javaBuilderPutMembers(call, src)
		return members, len(members) > 0
	}
	return nil, false
}

// javaFlatPairMembers reads an argument_list of alternating key,value literals
// and returns one member per pair. A trailing odd argument is ignored.
func javaFlatPairMembers(args ts.Node, src []byte) []extractor.EnumMember {
	var lits []ts.Node
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil {
			continue
		}
		lits = append(lits, a)
	}
	var out []extractor.EnumMember
	for i := 0; i+1 < len(lits); i += 2 {
		key, kok := javaLiteralText(lits[i], src)
		if !kok {
			key = strings.TrimSpace(nodeText(lits[i], src))
		}
		if key == "" {
			continue
		}
		val, vok := javaLiteralText(lits[i+1], src)
		if !vok {
			val = strings.TrimSpace(nodeText(lits[i+1], src))
		}
		out = append(out, extractor.EnumMember{
			Name: key, Value: val, Line: int(lits[i].StartPoint().Row) + 1,
		})
	}
	return out
}

// javaBuilderPutMembers walks an ImmutableMap.builder().put(...).put(...).build()
// chain bottom-up, collecting each `.put(key, value)` pair. The chain is a
// left-nested method_invocation; we descend the receiver (object) field until
// we exhaust the chain.
func javaBuilderPutMembers(buildCall ts.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	// Start from the receiver of `.build()` and walk down `.put(...)` links.
	cur := buildCall.ChildByFieldName("object")
	for cur != nil && cur.Type() == "method_invocation" {
		if javaInvocationLeaf(cur, src) == "put" {
			if a := cur.ChildByFieldName("arguments"); a != nil {
				members = append(members, javaFlatPairMembers(a, src)...)
			}
		}
		cur = cur.ChildByFieldName("object")
	}
	// Chain was collected bottom-up (last .put first); reverse to source order.
	for l, r := 0, len(members)-1; l < r; l, r = l+1, r-1 {
		members[l], members[r] = members[r], members[l]
	}
	return members
}

// javaInvocationLeaf returns the method name (the `name` field) of a
// method_invocation, e.g. "of" for `Map.of(...)`, "build" for `...build()`.
func javaInvocationLeaf(call ts.Node, src []byte) string {
	if call == nil || call.Type() != "method_invocation" {
		return ""
	}
	if n := call.ChildByFieldName("name"); n != nil {
		return nodeText(n, src)
	}
	return ""
}

// javaScalarLiteral returns the literal value of a scalar constant initializer
// (string/number/bool/char), or ok=false when the initializer is not a single
// literal. Used to collect constant-group members.
func javaScalarLiteral(init ts.Node, src []byte) (string, bool) {
	return javaLiteralText(init, src)
}

// javaLiteralText returns the normalised literal text of a literal node
// (quotes stripped for strings), and ok=true only for recognised literal kinds.
func javaLiteralText(node ts.Node, src []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Type() {
	case "string_literal", "decimal_integer_literal", "decimal_floating_point_literal",
		"hex_integer_literal", "octal_integer_literal", "binary_integer_literal",
		"hex_floating_point_literal", "character_literal", "true", "false":
		return extractor.StripLiteralQuotes(nodeText(node, src)), true
	}
	return "", false
}

// javaConstName reports whether a field name follows the constant convention
// (UPPER_SNAKE, all-caps, or contains an underscore between caps). This keeps
// ordinary mutable-looking static-final fields (e.g. a cached `LOGGER`) — which
// are still constants — in the group; we intentionally include any
// all-uppercase or underscore-separated name and exclude camelCase fields,
// which are rarely value constants.
func javaConstName(name string) bool {
	if name == "" {
		return false
	}
	hasLower := false
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			hasLower = true
			break
		}
	}
	return !hasLower
}
