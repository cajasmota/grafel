// enum_valueset.go — value-carrying SCOPE.Enum value-set nodes for Rust
// const/static constant collections and data-enums (data-model, epic #3628,
// ticket #4431; extends #4420/#4429). Reuses the shared cross-language builder
// in internal/extractor (EnumEntity / EnumMember / StripLiteralQuotes) so Rust
// const maps and enums converge on the SAME node model as Python const maps,
// TS const-objects, and the flagship enum extractors.
//
// The capability answers the enum/value-set parity question a rewrite needs:
// "what closed set of values does this source-of-truth carry?" — so a Rust
// `phf_map! { "core-admin" => "Core Admin" }` permission map (or a const slice
// map) is searchable by name and a downstream cross-graph parity-audit can diff
// its members against the Django PERMISSION_PAGES dict / v3 PermissionPage map.
//
// Detected Rust value-set shapes (all module/item scope):
//
//   - data-enum: `enum E { A = 1, B, C(u32) }` → kind_hint="rust_enum".
//     Each variant contributes its name; an explicit `= <literal>` discriminant
//     contributes the member value. Data-carrying variants (`C(u32)`,
//     `D { x: u8 }`) record the variant NAME only (value-less, honest-partial).
//   - const slice map: `const X: &[(&str, &str)] = &[("a","b"), ...]` →
//     kind_hint="rust_const_slice_map". Each 2-tuple of literals is one
//     {key,value} member. A non-2-literal-tuple element disqualifies the map.
//   - phf map: `static X: phf::Map<..> = phf_map! { "a" => "b", ... }` (also
//     `phf_set!`/`phf_ordered_map!`) → kind_hint="rust_phf_map". Each
//     `<lit> => <lit>` arrow pair is one member.
//   - lazy_static! / once_cell map: a `lazy_static! { static ref X: .. = {..} }`
//     or `once_cell`-initialised map whose body is a sequence of
//     `m.insert(<lit>, <lit>)` calls → kind_hint="rust_lazy_map". Each insert is
//     one member.
//   - module constant group: the remaining module-level scalar `const`/`static`
//     literal bindings (`const MAX: u32 = 100; const NAME: &str = "upvate";`)
//     are aggregated into ONE synthetic value-set named after the file stem +
//     "Constants" → kind_hint="rust_module_constants". Each binding is one
//     {name=value} member. Emitted ONLY when ≥2 such literal bindings exist (a
//     lone constant is not a "set").
//
// Honest-partial throughout: a collection emits a node ONLY when its membership
// is a closed set with at least one statically-known literal member; any
// non-literal / computed element is recorded value-less (enum / module group)
// or disqualifies the map (const-slice/phf/lazy maps, which are pure literal
// maps by construction).

package rust

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitRustConstValueSets is a supplemental pass (mirroring emitExceptionFlowEdges)
// that scans the file's top-level items for constant collections and data-enums,
// appending one SCOPE.Enum value-set entity per detected shape. It runs after
// the main walk so it never interferes with the struct/enum Component entities
// the walk already emits — the value-set node is an ADDITIONAL, name-searchable
// view of the same source-of-truth, keyed on a distinct QualifiedName.
func emitRustConstValueSets(root ts.Node, src []byte, filePath string, out *[]types.EntityRecord) {
	if root == nil {
		return
	}

	// A module constant group aggregates the file's loose scalar const/static
	// literal bindings into one value-set; collect them as we walk.
	var moduleConsts []extreg.EnumMember
	var moduleFirstLine, moduleLastLine int

	for i := 0; i < int(root.ChildCount()); i++ {
		item := root.Child(i)
		if item == nil {
			continue
		}
		switch item.Type() {
		case "enum_item":
			emitRustEnumValueSet(item, src, filePath, out)

		case "const_item", "static_item":
			// Try the structured-collection shapes first; if the binding is a
			// plain scalar literal, fold it into the module constant group.
			if emitRustConstCollection(item, src, filePath, out) {
				continue
			}
			name := childFieldText(item, "name", src)
			if name == "" {
				if id := firstChildOfType(item, "identifier"); id != nil {
					name = nodeTextR(id, src)
				}
			}
			lit := rustScalarLiteralValue(item, src)
			if name == "" || lit == "" {
				continue
			}
			ln := int(item.StartPoint().Row) + 1
			if moduleFirstLine == 0 {
				moduleFirstLine = ln
			}
			moduleLastLine = ln
			moduleConsts = append(moduleConsts, extreg.EnumMember{Name: name, Value: lit, Line: ln})

		case "macro_invocation":
			// Top-level `lazy_static! { static ref X: .. = {..}; }` — the binding
			// name and its map body live INSIDE the macro token_tree. Emit one
			// value-set per `static ref NAME` whose initializer body carries
			// `insert(<lit>, <lit>)` calls.
			emitRustLazyStaticValueSets(item, src, filePath, out)
		}
	}

	// A module constant group is a value-set only when ≥2 literal scalars are
	// declared at module scope — a single lone constant is not an enumerable set.
	if len(moduleConsts) >= 2 {
		groupName := rustModuleConstName(filePath)
		if ent, ok := extreg.EnumEntity(
			groupName, "rust", "rust_module_constants", filePath,
			moduleFirstLine, moduleLastLine, moduleConsts,
		); ok {
			*out = append(*out, ent)
		}
	}
}

// emitRustLazyStaticValueSets handles a top-level `lazy_static! { ... }` macro
// whose body declares one or more `static ref NAME: TYPE = { ..inserts.. };`
// bindings. The binding name follows the `ref` token; the map body is the
// initializer token_tree after the `=`, scanned for `insert(<lit>, <lit>)`
// calls (rustInsertMembers). One value-set is emitted per binding with a
// literal-insert body. Also covers `once_cell` Lazy maps when expressed via the
// same insert pattern.
func emitRustLazyStaticValueSets(macro ts.Node, src []byte, filePath string, out *[]types.EntityRecord) {
	if rustMacroName(macro, src) != "lazy_static" {
		return
	}
	tt := firstChildOfType(macro, "token_tree")
	if tt == nil {
		return
	}
	// Walk the token_tree's direct children, tracking the most recent `ref`-
	// introduced binding name; when its initializer token_tree is seen, emit.
	var pendingName string
	var pendingLine int
	for i := 0; i < int(tt.ChildCount()); i++ {
		ch := tt.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "identifier":
			txt := nodeTextR(ch, src)
			if txt == "ref" {
				// The next identifier sibling is the binding name.
				if nm := nextSiblingOfType(tt, i, "identifier"); nm != nil {
					pendingName = nodeTextR(nm, src)
					pendingLine = int(nm.StartPoint().Row) + 1
				}
			}
		case "token_tree":
			// Candidate initializer body for the pending binding.
			if pendingName == "" {
				continue
			}
			members := rustInsertMembers(ch, src)
			if len(members) > 0 {
				start := pendingLine
				if start == 0 {
					start = int(macro.StartPoint().Row) + 1
				}
				end := int(ch.EndPoint().Row) + 1
				if ent, ok := extreg.EnumEntity(pendingName, "rust", "rust_lazy_map", filePath, start, end, members); ok {
					*out = append(*out, ent)
				}
			}
			pendingName = ""
		}
	}
}

// emitRustEnumValueSet emits a value-carrying SCOPE.Enum node for a Rust
// `enum_item`. Each enum_variant contributes its name; an explicit `= <literal>`
// discriminant contributes the value. Data-carrying variants (tuple/struct)
// record the name only. Emitted ONLY when the enum has ≥1 variant.
func emitRustEnumValueSet(node ts.Node, src []byte, filePath string, out *[]types.EntityRecord) {
	name := childFieldText(node, "name", src)
	if name == "" {
		if id := firstChildOfType(node, "type_identifier"); id != nil {
			name = nodeTextR(id, src)
		}
	}
	if name == "" {
		return
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		body = firstChildOfType(node, "enum_variant_list")
	}
	if body == nil {
		return
	}
	var members []extreg.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		v := body.Child(i)
		if v == nil || v.Type() != "enum_variant" {
			continue
		}
		vn := childFieldText(v, "name", src)
		if vn == "" {
			if id := firstChildOfType(v, "identifier"); id != nil {
				vn = nodeTextR(id, src)
			}
		}
		if vn == "" {
			continue
		}
		// An explicit discriminant `Active = 1` carries a literal value; the
		// grammar places it as a `value`-fielded literal or as the trailing
		// named child after `=`.
		val := ""
		if vv := v.ChildByFieldName("value"); vv != nil {
			val = rustLiteralText(vv, src)
		} else {
			for j := int(v.ChildCount()) - 1; j >= 0; j-- {
				c := v.Child(j)
				if c == nil || !c.IsNamed() || c.Type() == "identifier" {
					continue
				}
				if lit := rustLiteralText(c, src); lit != "" {
					val = lit
				}
				break
			}
		}
		members = append(members, extreg.EnumMember{
			Name:  vn,
			Value: val,
			Line:  int(v.StartPoint().Row) + 1,
		})
	}
	start := int(node.StartPoint().Row) + 1
	end := int(node.EndPoint().Row) + 1
	if ent, ok := extreg.EnumEntity(name, "rust", "rust_enum", filePath, start, end, members); ok {
		*out = append(*out, ent)
	}
}

// emitRustConstCollection inspects a const_item / static_item and, when its
// value is a recognised literal MAP shape (const slice map, phf_map!,
// lazy_static!/once_cell map), emits a value-set node and returns true. A plain
// scalar binding (or any non-map value) returns false so the caller can fold it
// into the module constant group.
func emitRustConstCollection(item ts.Node, src []byte, filePath string, out *[]types.EntityRecord) bool {
	name := childFieldText(item, "name", src)
	if name == "" {
		if id := firstChildOfType(item, "identifier"); id != nil {
			name = nodeTextR(id, src)
		}
	}
	if name == "" {
		return false
	}

	// The initializer expression is the named child following the `=` token.
	// For a const/static it is the last named child that is not the name
	// identifier or the type expression — locate it by scanning after `=`.
	val := rustInitExpr(item)
	if val == nil {
		// lazy_static! places the binding INSIDE the macro token_tree, so a
		// bare `lazy_static! { ... }` item has no top-level initializer. Handle
		// that macro form here (the name is "ref X" inside the tree).
		return false
	}

	start := int(item.StartPoint().Row) + 1
	end := int(item.EndPoint().Row) + 1

	switch val.Type() {
	case "reference_expression":
		// `&[ ... ]` — unwrap to the array_expression.
		if arr := firstChildOfType(val, "array_expression"); arr != nil {
			if members, ok := rustSliceTupleMembers(arr, src); ok {
				if ent, ok2 := extreg.EnumEntity(name, "rust", "rust_const_slice_map", filePath, start, end, members); ok2 {
					*out = append(*out, ent)
				}
				return true
			}
		}
	case "array_expression":
		if members, ok := rustSliceTupleMembers(val, src); ok {
			if ent, ok2 := extreg.EnumEntity(name, "rust", "rust_const_slice_map", filePath, start, end, members); ok2 {
				*out = append(*out, ent)
			}
			return true
		}
	case "macro_invocation":
		if members, ok := rustPhfMapMembers(val, src); ok {
			if ent, ok2 := extreg.EnumEntity(name, "rust", "rust_phf_map", filePath, start, end, members); ok2 {
				*out = append(*out, ent)
			}
			return true
		}
	}
	return false
}

// rustInitExpr returns the initializer expression node of a const_item /
// static_item — the named expression child appearing after the `=` token.
// Returns nil when the item has no initializer.
func rustInitExpr(item ts.Node) ts.Node {
	sawEq := false
	for i := 0; i < int(item.ChildCount()); i++ {
		ch := item.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "=" {
			sawEq = true
			continue
		}
		if sawEq && ch.IsNamed() {
			return ch
		}
	}
	return nil
}

// rustSliceTupleMembers reads a `[("a","b"), ("c","d"), ...]` array_expression
// as a key→value map. Each element MUST be a 2-element tuple_expression of
// literals; any other element disqualifies the whole map (ok=false). A single
// literal-element array (`["a", "b"]`, a value LIST) yields name=value members
// where key==value, so a const value list is still an enumerable set.
func rustSliceTupleMembers(arr ts.Node, src []byte) ([]extreg.EnumMember, bool) {
	var members []extreg.EnumMember
	sawTuple := false
	for i := 0; i < int(arr.ChildCount()); i++ {
		el := arr.Child(i)
		if el == nil || !el.IsNamed() {
			continue
		}
		switch el.Type() {
		case "tuple_expression":
			sawTuple = true
			var lits []ts.Node
			for j := 0; j < int(el.ChildCount()); j++ {
				c := el.Child(j)
				if c != nil && c.IsNamed() {
					lits = append(lits, c)
				}
			}
			if len(lits) != 2 {
				return nil, false
			}
			k := rustLiteralText(lits[0], src)
			v := rustLiteralText(lits[1], src)
			if k == "" || v == "" {
				return nil, false
			}
			members = append(members, extreg.EnumMember{Name: k, Value: v, Line: int(el.StartPoint().Row) + 1})
		default:
			// A bare-literal value list element (e.g. string in `["a","b"]`).
			if sawTuple {
				return nil, false
			}
			lit := rustLiteralText(el, src)
			if lit == "" {
				return nil, false
			}
			members = append(members, extreg.EnumMember{Name: lit, Value: lit, Line: int(el.StartPoint().Row) + 1})
		}
	}
	if len(members) == 0 {
		return nil, false
	}
	return members, true
}

// rustPhfMapMembers reads a `phf_map! { "a" => "b", ... }` (or phf_set! /
// phf_ordered_map! / a lazy_static!-style insert body) macro_invocation as a
// key→value map. It walks the macro's token_tree, pairing each `<lit> => <lit>`
// across `=>` tokens. It also recognises a lazy_static! body whose statements
// are `m.insert(<lit>, <lit>)` calls. Returns ok=false when no literal pair is
// found, so a non-map macro emits no node.
func rustPhfMapMembers(macro ts.Node, src []byte) ([]extreg.EnumMember, bool) {
	macroName := rustMacroName(macro, src)
	tt := firstChildOfType(macro, "token_tree")
	if tt == nil {
		return nil, false
	}

	// Arrow-pair form: phf_map! / phf_ordered_map! / any `lit => lit` body.
	var members []extreg.EnumMember
	flat := flattenNamed(tt)
	for i := 0; i < len(flat); i++ {
		if flat[i].node.Type() == "=>" {
			// previous literal is the key, next literal is the value.
			k := prevLiteral(flat, i, src)
			v := nextLiteral(flat, i, src)
			if k != "" && v != "" {
				members = append(members, extreg.EnumMember{Name: k, Value: v, Line: flat[i].line})
			}
		}
	}
	if len(members) > 0 {
		return members, true
	}

	// phf_set! / value-list form: a comma-separated list of bare literals.
	if strings.HasPrefix(macroName, "phf_set") {
		for _, fn := range flat {
			if isRustLiteralNode(fn.node) {
				lit := rustLiteralText(fn.node, src)
				if lit != "" {
					members = append(members, extreg.EnumMember{Name: lit, Value: lit, Line: fn.line})
				}
			}
		}
		if len(members) > 0 {
			return members, true
		}
	}

	// lazy_static! / once_cell insert form: `m.insert("k", "v");` statements.
	if im := rustInsertMembers(tt, src); len(im) > 0 {
		return im, true
	}
	return nil, false
}

// rustInsertMembers scans a macro/block token_tree for `*.insert(<lit>, <lit>)`
// call statements (the canonical lazy_static! / once_cell HashMap build form)
// and returns one member per insert. The receiver / method-name tokens are
// ignored; only the literal argument pair matters.
func rustInsertMembers(tt ts.Node, src []byte) []extreg.EnumMember {
	var members []extreg.EnumMember
	stack := []ts.Node{tt}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for i := 0; i < int(n.ChildCount()); i++ {
			ch := n.Child(i)
			if ch == nil {
				continue
			}
			// An `insert` identifier immediately followed by an argument
			// token_tree of two literals is one map entry.
			if ch.Type() == "identifier" && nodeTextR(ch, src) == "insert" {
				// the argument token_tree is the next sibling token_tree.
				if args := nextSiblingOfType(n, i, "token_tree"); args != nil {
					lits := literalChildren(args, src)
					if len(lits) == 2 {
						members = append(members, extreg.EnumMember{
							Name:  lits[0],
							Value: lits[1],
							Line:  int(ch.StartPoint().Row) + 1,
						})
					}
				}
			}
			stack = append(stack, ch)
		}
	}
	return members
}

// rustScalarLiteralValue returns the statically-known literal text of a
// const_item / static_item's initializer when it is a plain scalar literal
// (string / int / float / bool / char). Returns "" for collection / non-literal
// initializers (which are handled by emitRustConstCollection or skipped).
func rustScalarLiteralValue(item ts.Node, src []byte) string {
	val := rustInitExpr(item)
	if val == nil {
		return ""
	}
	return rustLiteralText(val, src)
}

// rustLiteralText returns the normalised literal text of a Rust literal node
// (quotes stripped for strings/chars), or "" when n is not a literal.
func rustLiteralText(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "string_literal", "raw_string_literal", "char_literal":
		return extreg.StripLiteralQuotes(strings.TrimSpace(nodeTextR(n, src)))
	case "integer_literal", "float_literal", "boolean_literal":
		return strings.TrimSpace(nodeTextR(n, src))
	case "negative_literal", "unary_expression":
		return strings.TrimSpace(nodeTextR(n, src))
	}
	return ""
}

// isRustLiteralNode reports whether n is a Rust literal value node.
func isRustLiteralNode(n ts.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "string_literal", "raw_string_literal", "char_literal",
		"integer_literal", "float_literal", "boolean_literal",
		"negative_literal":
		return true
	}
	return false
}

// flatNode pairs a token-tree descendant node with its 1-based source line.
type flatNode struct {
	node ts.Node
	line int
}

// flattenNamed returns the named descendants of a token_tree in pre-order,
// EXCEPT it does not descend into nested token_tree groups whose own children
// are arguments (so a `phf_map!` arrow body is read at one level). The `=>`
// arrow tokens are unnamed but significant, so they are included explicitly.
func flattenNamed(tt ts.Node) []flatNode {
	var out []flatNode
	for i := 0; i < int(tt.ChildCount()); i++ {
		ch := tt.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "=>" || ch.IsNamed() {
			out = append(out, flatNode{node: ch, line: int(ch.StartPoint().Row) + 1})
		}
	}
	return out
}

// prevLiteral returns the normalised literal text of the nearest literal node
// before index i in a flattened token list, or "".
func prevLiteral(flat []flatNode, i int, src []byte) string {
	for j := i - 1; j >= 0; j-- {
		if isRustLiteralNode(flat[j].node) {
			return rustLiteralText(flat[j].node, src)
		}
	}
	return ""
}

// nextLiteral returns the normalised literal text of the nearest literal node
// after index i in a flattened token list, or "".
func nextLiteral(flat []flatNode, i int, src []byte) string {
	for j := i + 1; j < len(flat); j++ {
		if isRustLiteralNode(flat[j].node) {
			return rustLiteralText(flat[j].node, src)
		}
	}
	return ""
}

// literalChildren returns the normalised literal texts of the direct named
// literal children of node (used for `insert(<lit>, <lit>)` argument trees).
func literalChildren(node ts.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if isRustLiteralNode(ch) {
			out = append(out, rustLiteralText(ch, src))
		}
	}
	return out
}

// rustMacroName returns the bare macro name of a macro_invocation, handling
// both a plain `identifier` (`phf_map!`) and a path-qualified
// `scoped_identifier` (`lazy_static::lazy_static!`) — for the latter the LAST
// path segment (the macro name) is returned.
func rustMacroName(macro ts.Node, src []byte) string {
	if id := firstChildOfType(macro, "identifier"); id != nil {
		return nodeTextR(id, src)
	}
	if sc := firstChildOfType(macro, "scoped_identifier"); sc != nil {
		// Last identifier child is the macro name.
		var last ts.Node
		for i := 0; i < int(sc.ChildCount()); i++ {
			if ch := sc.Child(i); ch != nil && ch.Type() == "identifier" {
				last = ch
			}
		}
		if last != nil {
			return nodeTextR(last, src)
		}
	}
	return ""
}

// firstChildOfType returns the first direct child of node with the given type.
func firstChildOfType(node ts.Node, kind string) ts.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == kind {
			return ch
		}
	}
	return nil
}

// nextSiblingOfType returns the first sibling AFTER index i (among parent's
// children) whose type matches kind.
func nextSiblingOfType(parent ts.Node, i int, kind string) ts.Node {
	for j := i + 1; j < int(parent.ChildCount()); j++ {
		ch := parent.Child(j)
		if ch != nil && ch.Type() == kind {
			return ch
		}
	}
	return nil
}

// nodeTextR returns the source text of a node.
func nodeTextR(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

// rustModuleConstName derives the synthetic value-set name for a file's
// module-level scalar constant group from the file stem (e.g. `config.rs` →
// `ConfigConstants`).
func rustModuleConstName(filePath string) string {
	base := filepath.Base(filePath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.ReplaceAll(stem, "-", "_")
	parts := strings.Split(stem, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	if b.Len() == 0 {
		return "ModuleConstants"
	}
	b.WriteString("Constants")
	return b.String()
}
