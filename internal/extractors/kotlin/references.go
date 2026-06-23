// references.go — REFERENCES-edge emission for the Kotlin extractor.
//
// Analog of #641 (JS/TS), #650 (Python) and #670 (Java) for Kotlin. The
// Kotlin extractor previously emitted ~0 REFERENCES edges per function
// body — every same-scope identifier use, every `this.<property>`
// reference, every `ClassName.staticMember` reference inside a
// companion object, and every imported-name reference outside of a
// CALLS context produced no edge. On the ktor-samples corpus this
// contributed to a 40.3% orphan rate; the recoverable set mirrors the
// JS/TS / Python / Java two-track pattern.
//
// This pass mirrors emitReferences in the sibling extractors:
//
//   1. Build a file-scope symbol table from the entities already
//      emitted by the primary extractor pass. The table maps Name →
//      entity-kind metadata so we can build the right structural-ref
//      Format A target ID for each reference. Kotlin's primary pass
//      emits operations with bare leaf names (no `Class.method`
//      qualification, unlike Java's #65 contract); we synthesise a
//      `Class.method` key for class/object members on the fly during
//      the walk so `this.<method>` and `ClassName.<method>` resolve.
//
//   2. Walk every function body + property initializer for
//      `simple_identifier` / `navigation_expression` nodes that are
//      NOT in declaration position and are NOT the callee of a
//      call_expression (those are owned by CALLS). For each, look up
//      the identifier in the symbol table and emit a REFERENCES edge
//      from the enclosing operation entity.
//
//   3. Handle Kotlin-specific shapes:
//        - `this.<property>` and bare `<property>` references within a
//          class (implicit `this`).
//        - `ClassName.method()` chain calls — the receiver root is
//          treated as a class REFERENCES.
//        - Property delegation (`by lazy { ... }`) — the delegate
//          expression is a normal expression walked recursively.
//        - Lambdas — same lambda-frame discipline as Java; parameter
//          captures are declarations, not references.
//        - Extension functions — the receiver type is implicit; the
//          symbol table indexes the function under its bare leaf name.
//        - Data class destructuring (`val (a, b) = pair`) — the
//          destructured names sit inside a `variable_declaration` and
//          are declaration position, not references.
//        - String interpolation `"${expr}"` — the embedded expression
//          is walked recursively.
//        - Top-level functions and properties — no enclosing class;
//          symbol-table lookup is bare.
//        - Companion objects (`Companion.method()` and bare
//          `method()` from inside a companion) — the companion's body
//          is walked under the enclosing class's parentClass so
//          bare-name resolution finds class-level members.
//
//   4. Skip Kotlin keywords (`val`, `var`, `fun`, `object`, `class`,
//      `interface`, `companion`, `it`, `this`, `super`, `null`,
//      `true`, `false`, ...) so the bare-name resolver isn't bloated
//      with noise edges that would never bind to a project entity.
//
// Cap: one REFERENCES edge per (from_id, to_id) pair to prevent N-uses
// inflation. Self-references (a function body referencing its own
// emitted name) are filtered. CALLS edges remain the existing pathway
// — REFERENCES is strictly additive and only fires for non-call
// identifier shapes.
//
// The Format A structural-ref shape this emits is:
//
//	scope:operation:ref:kotlin:<file>:<name>            — function targets
//	scope:component:ref:kotlin:<file>:<name>            — class / object targets
//	scope:schema:ref:kotlin:<file>:<name>               — property targets
//
// The resolver's structuralKindFamilies covers operation and component
// scope segments; the existing lookupStructural →
// lookupLocationKind path binds these edges to their declaration
// without any new dispatcher work.

package kotlin

import (
	"path/filepath"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// kotlinReservedNames is the conservative allowlist of Kotlin keywords,
// soft keywords, and language-level pseudo-names that should NEVER
// produce a REFERENCES edge to a project entity. A user-declared name
// that shadows a hard keyword is impossible (the parser rejects it);
// this list is purely a noise filter on the identifier walk.
var kotlinReservedNames = map[string]struct{}{
	// Hard keywords
	"as": {}, "break": {}, "class": {}, "continue": {}, "do": {},
	"else": {}, "false": {}, "for": {}, "fun": {}, "if": {},
	"in": {}, "interface": {}, "is": {}, "null": {}, "object": {},
	"package": {}, "return": {}, "super": {}, "this": {}, "throw": {},
	"true": {}, "try": {}, "typealias": {}, "typeof": {}, "val": {},
	"var": {}, "when": {}, "while": {},
	// Soft / modifier keywords (context-sensitive but never a project entity)
	"by": {}, "catch": {}, "constructor": {}, "delegate": {},
	"dynamic": {}, "field": {}, "file": {}, "finally": {}, "get": {},
	"import": {}, "init": {}, "param": {}, "property": {},
	"receiver": {}, "set": {}, "setparam": {}, "where": {},
	"actual": {}, "abstract": {}, "annotation": {}, "companion": {},
	"const": {}, "crossinline": {}, "data": {}, "enum": {},
	"expect": {}, "external": {}, "final": {}, "infix": {},
	"inline": {}, "inner": {}, "internal": {}, "lateinit": {},
	"noinline": {}, "open": {}, "operator": {}, "out": {},
	"override": {}, "private": {}, "protected": {}, "public": {},
	"reified": {}, "sealed": {}, "suspend": {}, "tailrec": {},
	"vararg": {},
	// Pseudo-names
	"it": {}, "_": {},
	// Built-in types that look like simple_identifier in source
	"Unit": {}, "Nothing": {}, "Any": {},
}

// kotlinSymbol is a single file-scope symbol-table entry, used to
// build the right structural-ref Format A target for each reference.
type kotlinSymbol struct {
	kind    string
	subtype string
	name    string // emitted entity Name (Kotlin uses bare leaf for ops/properties)
}

// kotlinFrame tracks the enclosing operation / class while walking a
// file's CST during reference emission.
type kotlinFrame struct {
	funcEmittedName string // "" outside a function; bare leaf inside
	funcLeafName    string // bare leaf name of the enclosing operation
	parentClass     string // immediate enclosing class/object name
}

// emitReferences is the second-pass entry point invoked from Extract
// AFTER the primary walk has populated entities. It builds a
// file-scope symbol table from emitted entities, walks every function
// body + top-level property initializer, and appends REFERENCES edges
// to the enclosing operation entity.
//
// Mutates entities in place. Safe to call with an empty slice — no-op.
func emitReferences(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	// Phase 1 — build the file-scope symbol table.
	//
	// Index by THE NAME AS REFERENCED IN SOURCE. The Kotlin primary
	// pass emits methods with bare-leaf Name (no `Class.method`
	// qualification), so we index every operation under its bare name
	// AND synthesise `Class.method` dotted entries during the walk by
	// tracking the enclosing-class context. Properties are not
	// currently emitted by the primary pass; class/object names are.
	bareSymbols := make(map[string]kotlinSymbol)   // "foo" → method/class/import-leaf
	dottedSymbols := make(map[string]kotlinSymbol) // "Class.foo" → method (synthesised)
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		if e.Subtype == "file" || e.Subtype == "import" {
			// File carrier entity (#577) and import placeholders are
			// not project-binding targets for a same-file REFERENCES
			// edge. Cross-file binding for imported names is the
			// chain-fix-1 work item; see docs/verify2/per-repo-
			// residual-ledger.md.
			continue
		}
		switch {
		case e.Kind == "SCOPE.Operation":
			if _, exists := bareSymbols[e.Name]; !exists {
				bareSymbols[e.Name] = kotlinSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		case e.Kind == "SCOPE.Component" &&
			(e.Subtype == "class" || e.Subtype == "data_class" || e.Subtype == "object"):
			if _, exists := bareSymbols[e.Name]; !exists {
				bareSymbols[e.Name] = kotlinSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		}
	}

	// Phase 1b — synthesise dotted `Class.method` entries by walking the
	// CST surface for class/object → method memberships. This lets
	// `this.method` and `ClassName.method` resolve to the operation's
	// bare-leaf Name in the symbol table while still computing the
	// correct same-file binding.
	synthesiseKotlinDottedMembers(root, file, bareSymbols, dottedSymbols)

	if len(bareSymbols) == 0 && len(dottedSymbols) == 0 {
		return
	}

	// Phase 2 — walk every function body, tracking the enclosing
	// operation entity (by its emitted Name) and its enclosing class
	// (so `this.<member>` and `ClassName.<member>` resolve).
	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]bool)

	emit := func(fstack []kotlinFrame, sym kotlinSymbol) {
		if len(fstack) == 0 {
			return
		}
		top := fstack[len(fstack)-1]
		if top.funcEmittedName == "" || top.funcEmittedName == sym.name {
			return
		}
		key := edgeKey{top.funcEmittedName, sym.name}
		if seen[key] {
			return
		}
		seen[key] = true
		idx, ok := findKotlinEntityIndex(*entities, top.funcEmittedName, file.Path)
		if !ok {
			return
		}
		toID := buildKotlinReferenceTargetID(file.Path, sym)
		(*entities)[idx].Relationships = append((*entities)[idx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "REFERENCES",
			})
	}

	var walk func(n ts.Node, parentClass string, fstack []kotlinFrame)
	walk = func(n ts.Node, parentClass string, fstack []kotlinFrame) {
		if n == nil {
			return
		}
		nt := n.Type()

		// Class / object bodies update parentClass for nested walks.
		switch nt {
		case "class_declaration", "object_declaration":
			cls := kotlinDeclName(n, file.Content)
			body := kotlinFindBody(n)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), cls, fstack)
				}
			}
			return

		case "companion_object":
			// Companion bodies walk under the enclosing class so
			// bare-name resolution inside the companion still finds
			// class-level members. The companion's own name (usually
			// "Companion") is not added as a separate parentClass.
			body := kotlinFindBody(n)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), parentClass, fstack)
				}
			}
			return

		case "function_declaration":
			leaf := kotlinDeclName(n, file.Content)
			emitted := leaf
			body := findFunctionBody(n)
			if body == nil || leaf == "" {
				return
			}
			newFrame := kotlinFrame{
				funcEmittedName: emitted,
				funcLeafName:    leaf,
				parentClass:     parentClass,
			}
			newStack := append(fstack, newFrame)
			for i := 0; i < int(body.ChildCount()); i++ {
				walk(body.Child(i), parentClass, newStack)
			}
			return
		}

		// Identifier shapes — only fire inside an operation body.
		if len(fstack) > 0 {
			switch nt {
			case "simple_identifier":
				handleKotlinIdentifier(n, file, fstack, bareSymbols, emit)
			case "type_identifier":
				handleKotlinTypeIdentifier(n, file, fstack, bareSymbols, emit)
			case "navigation_expression":
				handleKotlinNavigationExpression(n, file, fstack, bareSymbols, dottedSymbols, emit)
			}
		}

		// Recurse into all children.
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			walk(n.Child(i), parentClass, fstack)
		}
	}

	walk(root, "", nil)
}

// findKotlinEntityIndex returns the index of the file-local entity
// whose Name matches the supplied emittedName. Linear scan —
// acceptable because the per-file entity count is in the dozens.
func findKotlinEntityIndex(entities []types.EntityRecord, emittedName, filePath string) (int, bool) {
	for i := range entities {
		if entities[i].SourceFile == filePath && entities[i].Name == emittedName {
			return i, true
		}
	}
	return -1, false
}

// kotlinDeclName returns the leaf name of a class/object/function
// declaration. Tree-sitter-kotlin uses `type_identifier` for
// class/object names and `simple_identifier` for function names; both
// shapes are accepted via the field name or first-child fallback.
func kotlinDeclName(n ts.Node, src []byte) string {
	if name := n.ChildByFieldName("name"); name != nil {
		return string(src[name.StartByte():name.EndByte()])
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		t := ch.Type()
		if t == "type_identifier" || t == "simple_identifier" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	return ""
}

// kotlinFindBody returns the body child of a class/object/companion
// declaration. Tree-sitter-kotlin uses `class_body` / `object_body` /
// `enum_class_body` depending on the declaration shape.
func kotlinFindBody(node ts.Node) ts.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		t := ch.Type()
		if t == "class_body" || t == "object_body" || t == "enum_class_body" {
			return ch
		}
	}
	return nil
}

// synthesiseKotlinDottedMembers walks the CST once to build
// `Class.method` → operation-entity entries for every function
// declared inside a class/object body. The primary extractor emits
// these methods with bare-leaf Name; the dotted synthesis lets
// `this.<method>` and `ClassName.<method>` resolve via dottedSymbols
// to the same emitted entity.
func synthesiseKotlinDottedMembers(
	root ts.Node,
	file extractor.FileInput,
	bareSymbols, dottedSymbols map[string]kotlinSymbol,
) {
	var walk func(n ts.Node, parentClass string)
	walk = func(n ts.Node, parentClass string) {
		if n == nil {
			return
		}
		nt := n.Type()
		switch nt {
		case "class_declaration", "object_declaration":
			cls := kotlinDeclName(n, file.Content)
			body := kotlinFindBody(n)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), cls)
				}
			}
			return
		case "companion_object":
			body := kotlinFindBody(n)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), parentClass)
				}
			}
			return
		case "function_declaration":
			if parentClass == "" {
				return
			}
			leaf := kotlinDeclName(n, file.Content)
			if leaf == "" {
				return
			}
			dotted := parentClass + "." + leaf
			if _, ok := dottedSymbols[dotted]; !ok {
				if sym, ok := bareSymbols[leaf]; ok {
					dottedSymbols[dotted] = sym
				}
			}
			return
		}
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")
}

// handleKotlinIdentifier handles a bare `simple_identifier` node:
//   - skip if in declaration position (parent's `name` field is this
//     node, or parent is a known declaration container).
//   - skip if this is the callee of a call_expression (CALLS owns it).
//   - skip if this is the trailing identifier of a navigation_suffix
//     (handleKotlinNavigationExpression owns it).
//   - skip Kotlin reserved keywords / pseudo-names.
//   - skip self-name (an identifier matching the enclosing function's
//     leaf name).
//   - otherwise look up in bareSymbols and emit.
func handleKotlinIdentifier(
	n ts.Node,
	file extractor.FileInput,
	fstack []kotlinFrame,
	bareSymbols map[string]kotlinSymbol,
	emit func([]kotlinFrame, kotlinSymbol),
) {
	name := string(file.Content[n.StartByte():n.EndByte()])
	if name == "" {
		return
	}
	if _, isReserved := kotlinReservedNames[name]; isReserved {
		return
	}
	if isKotlinDeclarationPosition(n) {
		return
	}
	if isKotlinCallCallee(n) {
		return
	}
	if isKotlinNavigationSuffixField(n) {
		// `obj.<this>` — owned by handleKotlinNavigationExpression.
		return
	}
	sym, ok := bareSymbols[name]
	if !ok {
		return
	}
	top := fstack[len(fstack)-1]
	if name == top.funcLeafName {
		return
	}
	emit(fstack, sym)
}

// handleKotlinTypeIdentifier handles `type_identifier` nodes —
// references to a class/object used in a type position (cast,
// `is` check, generic parameter, local variable type annotation, or
// return-type position). These are strictly REFERENCES, never CALLS.
func handleKotlinTypeIdentifier(
	n ts.Node,
	file extractor.FileInput,
	fstack []kotlinFrame,
	bareSymbols map[string]kotlinSymbol,
	emit func([]kotlinFrame, kotlinSymbol),
) {
	name := string(file.Content[n.StartByte():n.EndByte()])
	if name == "" {
		return
	}
	if _, isReserved := kotlinReservedNames[name]; isReserved {
		return
	}
	sym, ok := bareSymbols[name]
	if !ok {
		return
	}
	// Only bind to class/object entries; function bareSymbols keyed
	// by the same leaf are not type references.
	if sym.kind != "SCOPE.Component" {
		return
	}
	top := fstack[len(fstack)-1]
	if name == top.funcLeafName {
		return
	}
	emit(fstack, sym)
}

// handleKotlinNavigationExpression handles `obj.attr` / `obj.method`
// shapes. The Kotlin grammar uses `navigation_expression` with
// `navigation_suffix` children carrying the `.name` portion.
//
//   - If the receiver is `this` (a `this_expression` child) and
//     dottedSymbols has `<ParentClass>.<attr>`, emit REFERENCES to that
//     method entity.
//   - If the receiver is a PascalCase `simple_identifier`
//     (`ClassName.staticMember`), look up `<ClassName>.<attr>` in
//     dottedSymbols (method), then fall back to bareSymbols[<attr>]
//     for bare match.
//   - Otherwise leave the receiver to the generic identifier walk —
//     the recursion will descend into the receiver and handle each
//     `simple_identifier` independently.
func handleKotlinNavigationExpression(
	n ts.Node,
	file extractor.FileInput,
	fstack []kotlinFrame,
	bareSymbols, dottedSymbols map[string]kotlinSymbol,
	emit func([]kotlinFrame, kotlinSymbol),
) {
	// Skip when this navigation_expression IS the head of a call —
	// CALLS owns that edge through its own resolver.
	if parent := n.Parent(); parent != nil && parent.Type() == "call_expression" {
		// Check if this is the first child (the callee head).
		if parent.ChildCount() > 0 && ts.SameNode(parent.Child(0), n) {
			// Receiver position of a call — the call's own resolver
			// builds the dotted target. Do NOT double-emit here.
			return
		}
	}

	// Find the trailing navigation_suffix and extract its simple_identifier.
	var lastSuffix ts.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == "navigation_suffix" {
			lastSuffix = ch
		}
	}
	if lastSuffix == nil {
		return
	}
	var attr string
	for i := int(lastSuffix.ChildCount()) - 1; i >= 0; i-- {
		ch := lastSuffix.Child(i)
		if ch.Type() == "simple_identifier" {
			attr = string(file.Content[ch.StartByte():ch.EndByte()])
			break
		}
	}
	if attr == "" {
		return
	}
	if _, isReserved := kotlinReservedNames[attr]; isReserved {
		return
	}

	// The receiver is the first child (anything that's not a
	// navigation_suffix and appears before the first suffix).
	var receiver ts.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == "navigation_suffix" {
			break
		}
		receiver = ch
	}
	if receiver == nil {
		return
	}
	top := fstack[len(fstack)-1]
	rt := receiver.Type()

	switch rt {
	case "this_expression":
		// `this.<attr>` shape — look up `<ParentClass>.<attr>` first,
		// fall back to bare attr.
		if top.parentClass != "" {
			dotted := top.parentClass + "." + attr
			if sym, ok := dottedSymbols[dotted]; ok {
				if attr == top.funcLeafName {
					return
				}
				emit(fstack, sym)
				return
			}
		}
		if sym, ok := bareSymbols[attr]; ok {
			if attr == top.funcLeafName {
				return
			}
			emit(fstack, sym)
		}
	case "simple_identifier":
		recv := string(file.Content[receiver.StartByte():receiver.EndByte()])
		if recv == "" {
			return
		}
		if _, isReserved := kotlinReservedNames[recv]; isReserved {
			return
		}
		// PascalCase receiver → likely a ClassName / ObjectName. Try
		// the dotted lookup `ClassName.<attr>` against the symbol
		// table.
		if isKotlinPascalCase(recv) {
			dotted := recv + "." + attr
			if sym, ok := dottedSymbols[dotted]; ok {
				if attr == top.funcLeafName {
					return
				}
				emit(fstack, sym)
				return
			}
			// Fall back to the bare receiver as a class/object
			// REFERENCES — `Companion.method` where Companion is a
			// known object resolves here.
			if sym, ok := bareSymbols[recv]; ok && sym.kind == "SCOPE.Component" {
				emit(fstack, sym)
			}
		}
		// Otherwise leave the bare identifier to the generic walk.
	}
}

// isKotlinDeclarationPosition reports whether the simple_identifier
// node sits in a position that DECLARES the name rather than USES it.
//
// Recognised shapes (all return true):
//
//	parent's field `name` is this node — function_declaration,
//	  class_declaration, object_declaration, class_parameter, …
//	parent is `variable_declaration` (LHS of val/var)
//	parent is `parameter` / `function_value_parameter` / `class_parameter`
//	parent is `type_parameter`
//	parent is `import_header` / `package_header`
//	parent is `lambda_parameters`
//	parent is `property_declaration` and the identifier appears before
//	  any `=` in the original source (defensive — the variable_declaration
//	  guard above handles the common case).
func isKotlinDeclarationPosition(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if nameField := parent.ChildByFieldName("name"); nameField != nil && nameField == n {
		return true
	}
	switch parent.Type() {
	case "variable_declaration",
		"parameter",
		"function_value_parameter",
		"class_parameter",
		"type_parameter",
		"package_header",
		"import_header",
		"lambda_parameters",
		"anonymous_function":
		return true
	case "function_declaration", "class_declaration", "object_declaration",
		"property_declaration", "secondary_constructor", "primary_constructor":
		// Declaration-name child appears as the first simple_identifier;
		// if `name` field is unset, treat the first simple_identifier
		// child as the declared name.
		for i := 0; i < int(parent.ChildCount()); i++ {
			ch := parent.Child(i)
			if ts.SameNode(ch, n) {
				return true
			}
			if ch.Type() == "simple_identifier" || ch.Type() == "type_identifier" {
				// First simple_identifier seen before n means n is not
				// the declared name.
				return false
			}
		}
	}
	return false
}

// isKotlinCallCallee reports whether the simple_identifier node is
// the callee head of a `call_expression` node (the first child when
// that child is `simple_identifier`). CALLS owns those edges —
// REFERENCES would double-count.
func isKotlinCallCallee(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if parent.Type() != "call_expression" {
		return false
	}
	return parent.ChildCount() > 0 && ts.SameNode(parent.Child(0), n)
}

// isKotlinNavigationSuffixField reports whether the simple_identifier
// node is the trailing identifier of a `navigation_suffix` node. The
// navigation_expression handler owns the binding decision; the bare
// identifier walk should skip it to avoid double-emission.
func isKotlinNavigationSuffixField(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	return parent.Type() == "navigation_suffix"
}

// isKotlinPascalCase reports whether s starts with an uppercase ASCII
// letter (a conservative class/object-name heuristic).
func isKotlinPascalCase(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// buildKotlinReferenceTargetID emits a Format A structural-ref for
// the resolver's lookupStructural → lookupLocationKind path.
// Operation-kinded targets emit `scope:operation:ref:...`, everything
// else emits `scope:component:ref:...`. Must stay aligned with
// structuralKindFamilies in internal/resolve/refs.go.
func buildKotlinReferenceTargetID(filePath string, sym kotlinSymbol) string {
	scopeSeg := "component"
	switch sym.kind {
	case "SCOPE.Operation":
		scopeSeg = "operation"
	case "SCOPE.Schema":
		scopeSeg = "schema"
	}
	return "scope:" + scopeSeg + ":ref:kotlin:" + filepath.ToSlash(filePath) + ":" + sym.name
}
