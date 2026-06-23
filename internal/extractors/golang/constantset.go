package golang

// constantset.go — value-carrying SCOPE.Enum value-set nodes for Go constant
// COLLECTIONS that are not the classic same-file-named-type const block already
// handled by enum_valueset.go (#4426, extends #4420/#4429, epic #4419).
//
// Three additional Go constant-collection shapes become searchable value-sets,
// each carrying the shared structured members_json ([{key,value,line}]) emitted
// by extractor.EnumEntity, so a downstream cross-graph parity-audit reads the
// literal {key,value} set without re-parsing source:
//
//   1. Untyped grouped const block whose members share a non-trivial common
//      identifier prefix — the idiomatic "enum without a named type":
//
//          const (
//              StatusActive = "active"
//              StatusIdle   = "idle"
//          )
//
//      The shared prefix (`Status`) names the value-set. A grouped block whose
//      members share no common prefix (`A = 1; B = 2`) is NOT a value-set —
//      it is an incidental constant grouping, so no node is emitted
//      (precision-first; preserves the #4429 untyped-const negative behaviour).
//      iota members are recorded value-less (member name only) — no fabricated
//      ordinals (honest-partial), mirroring the typed go_iota path.
//
//   2. Typed string/int const group whose type is NOT declared in the same
//      file (so enum_valueset.go's same-file-named-type gate skips it), e.g. a
//      const group typed by a type imported from another package. Captured the
//      same way as (1), named by the shared type name.
//
//   3. Package-level map-of-constants:
//
//          var Pages = map[string]string{"CoreAdmin": "core-admin", ...}
//
//      The variable identifier (`Pages`) names the value-set; each keyed_element
//      contributes a {key,value,line} member. Honest-partial: a node is emitted
//      ONLY when at least one value is a static literal; a map whose values are
//      all non-literal expressions (calls, identifiers) is not a closed
//      enumerated value-set and emits no node.
//
// The existing same-file-named-type const block (enum_valueset.go) keeps its
// dedicated path; this file only adds the shapes that path deliberately skips,
// so the two never double-emit for the same declaration.

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractGoConstantSets scans the file for the additional Go constant-collection
// shapes (untyped/foreign-typed grouped const blocks, package-level const maps)
// and emits one SCOPE.Enum value-set node per qualifying collection. `named` is
// the set of same-file enum-base type names already owned by extractGoEnums; a
// const block typed by one of those is left to that path (no double-emit).
func extractGoConstantSets(root ts.Node, src []byte, filePath string, named map[string]bool) []types.EntityRecord {
	if root == nil {
		return nil
	}
	var out []types.EntityRecord
	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "const_declaration":
			if rec, ok := buildGoConstBlockValueSet(n, src, filePath, named); ok {
				out = append(out, rec)
			}
			return
		case "var_declaration":
			out = append(out, buildGoConstMapValueSets(n, src, filePath)...)
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

// buildGoConstBlockValueSet handles a grouped const block that the same-file-
// named-type path (enum_valueset.go) does NOT own: either untyped, or typed by
// a type that is not a same-file enum-base named type. It emits a value-set
// named by the shared type (foreign-typed group) or by the members' common
// identifier prefix (untyped idiomatic enum). Returns ok=false when no such
// name can be derived (incidental const grouping), preserving precision.
func buildGoConstBlockValueSet(constDecl ts.Node, src []byte, filePath string, named map[string]bool) (types.EntityRecord, bool) {
	// A single-spec const declaration (`const Pi = 3.14`) is not a collection.
	specCount := 0
	for i := 0; i < int(constDecl.ChildCount()); i++ {
		if c := constDecl.Child(i); c != nil && c.Type() == "const_spec" {
			specCount++
		}
	}
	if specCount < 2 {
		return types.EntityRecord{}, false
	}

	// Determine the type carried by the first typed spec (Go repetition: the
	// type carries implicitly to subsequent specs).
	var blockType string
	for i := 0; i < int(constDecl.ChildCount()); i++ {
		spec := constDecl.Child(i)
		if spec == nil || spec.Type() != "const_spec" {
			continue
		}
		if tn := firstChildOfType(spec, "type_identifier"); tn != nil {
			blockType = nodeText(tn, src)
			break
		}
	}
	// Same-file enum-base named type → owned by enum_valueset.go; skip here.
	if blockType != "" && named[blockType] {
		return types.EntityRecord{}, false
	}

	var members []extractor.EnumMember
	for i := 0; i < int(constDecl.ChildCount()); i++ {
		spec := constDecl.Child(i)
		if spec == nil || spec.Type() != "const_spec" {
			continue
		}
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
	if len(members) < 2 {
		return types.EntityRecord{}, false
	}

	// Name + kind_hint:
	//   - foreign-typed group → the type name (a real declared value-set type).
	//   - untyped group → the members' common identifier prefix, the idiomatic
	//     enum-without-a-type. No usable prefix → not a value-set (skip).
	name := blockType
	kindHint := "go_typed_const_group"
	if name == "" {
		name = commonIdentifierPrefix(members)
		kindHint = "go_const_group"
		if name == "" {
			return types.EntityRecord{}, false
		}
	}

	return extractor.EnumEntity(
		name, "go", kindHint, filePath,
		int(constDecl.StartPoint().Row)+1, int(constDecl.EndPoint().Row)+1, members,
	)
}

// buildGoConstMapValueSets handles a `var X = map[K]V{...}` package-level
// constant map (and grouped `var ( X = map...; Y = map... )` blocks), emitting
// one value-set per qualifying map-literal-valued var_spec.
func buildGoConstMapValueSets(varDecl ts.Node, src []byte, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for i := 0; i < int(varDecl.ChildCount()); i++ {
		spec := varDecl.Child(i)
		if spec == nil || spec.Type() != "var_spec" {
			continue
		}
		name := nodeText(firstChildOfType(spec, "identifier"), src)
		if name == "" || name == "_" {
			continue
		}
		lit := mapLiteralValue(spec)
		if lit == nil {
			continue
		}
		members, anyLiteral := goMapMembers(lit, src)
		if len(members) == 0 || !anyLiteral {
			// Empty map, or a map whose values are all non-literal expressions
			// → not a closed enumerated value-set (honest-partial).
			continue
		}
		if rec, ok := extractor.EnumEntity(
			name, "go", "go_const_map", filePath,
			int(spec.StartPoint().Row)+1, int(spec.EndPoint().Row)+1, members,
		); ok {
			out = append(out, rec)
		}
	}
	return out
}

// mapLiteralValue returns the literal_value node of a var_spec whose RHS is a
// `map[K]V{...}` composite literal, or nil when the RHS is not a map literal.
func mapLiteralValue(spec ts.Node) ts.Node {
	el := firstChildOfType(spec, "expression_list")
	if el == nil {
		return nil
	}
	for i := 0; i < int(el.ChildCount()); i++ {
		ch := el.Child(i)
		if ch == nil || ch.Type() != "composite_literal" {
			continue
		}
		if firstChildOfType(ch, "map_type") == nil {
			return nil // a non-map composite literal (struct/slice) is not a const map.
		}
		return firstChildOfType(ch, "literal_value")
	}
	return nil
}

// goMapMembers extracts {key,value,line} members from a map literal_value node.
// Keys must be string literals (a constant-map key); values capture the static
// literal or "" for non-literal value expressions (the key stays enumerable).
// anyLiteral reports whether at least one value was a static literal.
func goMapMembers(lit ts.Node, src []byte) (members []extractor.EnumMember, anyLiteral bool) {
	for i := 0; i < int(lit.ChildCount()); i++ {
		ke := lit.Child(i)
		if ke == nil || ke.Type() != "keyed_element" {
			continue
		}
		// keyed_element children: literal_element (key), ':', literal_element (value).
		var elems []ts.Node
		for j := 0; j < int(ke.ChildCount()); j++ {
			c := ke.Child(j)
			if c != nil && c.Type() == "literal_element" {
				elems = append(elems, c)
			}
		}
		if len(elems) != 2 {
			continue
		}
		key := literalElementString(elems[0], src)
		if key == "" {
			continue // non-string-literal key (computed / identifier) — skip.
		}
		val := literalElementLiteral(elems[1], src)
		if val != "" {
			anyLiteral = true
		}
		members = append(members, extractor.EnumMember{
			Name:  key,
			Value: val,
			Line:  int(ke.StartPoint().Row) + 1,
		})
	}
	return members, anyLiteral
}

// literalElementString returns the string-literal text of a key literal_element
// (quotes stripped), or "" when the element is not a string literal.
func literalElementString(le ts.Node, src []byte) string {
	for i := 0; i < int(le.ChildCount()); i++ {
		c := le.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "interpreted_string_literal", "raw_string_literal":
			return extractor.StripLiteralQuotes(nodeText(c, src))
		}
	}
	return ""
}

// literalElementLiteral returns the static literal value of a value
// literal_element (string quotes stripped; int/float verbatim), or "" for a
// non-literal value expression (call, identifier, composite).
func literalElementLiteral(le ts.Node, src []byte) string {
	for i := 0; i < int(le.ChildCount()); i++ {
		c := le.Child(i)
		if c == nil || !c.IsNamed() {
			continue
		}
		switch c.Type() {
		case "interpreted_string_literal", "raw_string_literal":
			return extractor.StripLiteralQuotes(nodeText(c, src))
		case "int_literal", "float_literal", "true", "false":
			return nodeText(c, src)
		default:
			return ""
		}
	}
	return ""
}

// commonIdentifierPrefix returns the longest common leading identifier segment
// shared by every member name, used to name an untyped idiomatic enum
// (`StatusActive`, `StatusIdle` → `Status`). The prefix must be at least 2
// characters and must not equal a whole member name (so `A`, `AB` does not
// collapse to `A`). Returns "" when no such prefix exists, so an incidental
// const grouping (no shared prefix) emits no value-set node.
func commonIdentifierPrefix(members []extractor.EnumMember) string {
	if len(members) < 2 {
		return ""
	}
	prefix := members[0].Name
	for _, m := range members[1:] {
		prefix = sharedPrefix(prefix, m.Name)
		if prefix == "" {
			return ""
		}
	}
	// Trim to the last upper-case boundary so the prefix is a clean PascalCase
	// segment (`StatusActive`/`StatusArchived` → `Status`, not `StatusA`).
	prefix = trimToCamelBoundary(prefix)
	if len(prefix) < 2 {
		return ""
	}
	// Reject a prefix that equals a member name in full (then it is not a
	// shared *prefix* but a member, e.g. `Status`, `StatusActive`).
	for _, m := range members {
		if m.Name == prefix {
			return ""
		}
	}
	return prefix
}

// sharedPrefix returns the longest common leading substring of a and b.
func sharedPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// trimToCamelBoundary trims a PascalCase prefix back to its last complete
// upper-case-initiated segment boundary, so a partial trailing segment
// (`StatusA` from `StatusActive`/`StatusArchived`) reduces to `Status`. A
// prefix that already ends at a boundary is returned unchanged.
func trimToCamelBoundary(p string) string {
	// Find the last index i>0 where p[i] is upper-case (segment start). Trim
	// there only when characters follow that would make it a partial segment;
	// since p is a *common* prefix, anything after the last segment start is a
	// partial word, so cut at the last segment start (keeping >=1 segment).
	last := 0
	for i := 1; i < len(p); i++ {
		if p[i] >= 'A' && p[i] <= 'Z' {
			last = i
		}
	}
	if last > 0 {
		return p[:last]
	}
	return p
}
