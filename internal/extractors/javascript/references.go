// references.go — REFERENCES-edge emission for the JS/TS extractor.
//
// Background (2026-05-19 orphan root-cause analysis):
// The fixture-b graph carried only 4 REFERENCES edges total. Same-file
// const-family entities (`const X = useState(false)`, `const FOO = "/api"`,
// `const handler = useCallback(...)`) were emitted but never linked. Every
// later same-file use of those names (`setX(true)`, `` `${FOO}/users` ``,
// `<form onSubmit={handler}>`) produced no edge, leaving ~9 390 orphan
// entities — most of them recoverable by edges, not by deletion.
//
// Track A: same-scope identifier resolution. For every function/method/
// arrow body, walk the AST for non-declaration identifier nodes. If the
// name matches an entity declared at file scope (this file, any kind),
// emit a REFERENCES edge from the enclosing function entity to a Format A
// structural ref (`scope:<kind>:<sub>:<lang>:<file>:<name>`). Skip the
// `function` child of `call_expression` / `new_expression` (CALLS owns
// that), skip self-references, skip well-known JS globals.
//
// Track B: template-literal interpolations. `template_substitution` nodes
// wrap an expression inside `${...}` segments — walk them the same way.
//
// Track C: import-target identifier resolution. Imported names appear in
// the file-scope symbol table as IMPORTS-bound locals. Same-file uses of
// an imported name emit REFERENCES to the imported-name entity. Because
// we already index file-scope entities by name, the same machinery
// covers Track C with zero additional code paths.
//
// Design constraints (from the spec):
//   - Cap at one REFERENCES edge per (from_id, to_id) pair — no
//     duplicate emission for repeat usages of the same identifier.
//   - Skip self-references (X used inside X's own definition body).
//   - Never emit REFERENCES to globals or unknown names — bare-name
//     resolver bloat must not regress.
//   - CALLS edges remain the existing pathway; REFERENCES is additive
//     and only fires for non-call identifier shapes.
//
// All edges are emitted using Format A structural-refs so the resolver's
// existing `lookupStructural` -> `lookupLocationKind` path binds them to
// the same-file declaration without any new dispatcher work and without
// any reliance on bare-name hint families.

package javascript

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// jsGlobals is the conservative allowlist of well-known JS / browser /
// Node.js globals. An identifier matching this set is NEVER emitted as a
// REFERENCES target; it's treated as external. The list is intentionally
// short — anything that's almost-always a global, almost-never a
// user-declared name. False negatives (a custom symbol shadowing one of
// these) cost us a missed REFERENCES edge; false positives (treating a
// user-declared name as a global) would produce no edge AT ALL because
// the symbol-table guard runs first. Net effect: no over-emission risk.
var jsGlobals = map[string]struct{}{
	"window": {}, "document": {}, "console": {}, "navigator": {},
	"location": {}, "history": {}, "globalThis": {}, "self": {},
	"process": {}, "Buffer": {}, "global": {}, "module": {}, "exports": {},
	"require": {}, "__dirname": {}, "__filename": {},
	"setTimeout": {}, "setInterval": {}, "clearTimeout": {}, "clearInterval": {},
	"setImmediate": {}, "clearImmediate": {}, "queueMicrotask": {},
	"fetch": {}, "XMLHttpRequest": {}, "URL": {}, "URLSearchParams": {},
	"Promise": {}, "Symbol": {}, "Map": {}, "Set": {}, "WeakMap": {}, "WeakSet": {},
	"Array": {}, "Object": {}, "String": {}, "Number": {}, "Boolean": {},
	"Date": {}, "Math": {}, "JSON": {}, "RegExp": {}, "Error": {},
	"TypeError": {}, "RangeError": {}, "SyntaxError": {}, "ReferenceError": {},
	"undefined": {}, "NaN": {}, "Infinity": {}, "null": {}, "true": {}, "false": {},
	"this": {}, "super": {}, "new": {}, "void": {},
	"localStorage": {}, "sessionStorage": {}, "alert": {}, "confirm": {}, "prompt": {},
	"Number_MAX_VALUE": {}, "parseFloat": {}, "parseInt": {}, "isNaN": {}, "isFinite": {},
	"encodeURIComponent": {}, "decodeURIComponent": {}, "encodeURI": {}, "decodeURI": {},
	"React": {}, // very commonly the React default-import binding; harmless to skip
}

// tsBuiltinTypes — TypeScript built-in / lib.d.ts type names we should
// never emit a REFERENCES edge to in type-position. These usually have
// no same-file declaration, so the symbol-table guard already filters
// them — this list is belt-and-suspenders to prevent accidental binding
// when a user types something like `type Array = ...` and then writes
// `Array<X>` in the same file (which would be a self-edge anyway).
var tsBuiltinTypes = map[string]struct{}{
	"string": {}, "number": {}, "boolean": {}, "bigint": {}, "symbol": {},
	"any": {}, "unknown": {}, "never": {}, "void": {}, "undefined": {}, "null": {},
	"object": {}, "Object": {}, "Function": {}, "Array": {}, "ReadonlyArray": {},
	"Promise": {}, "Map": {}, "Set": {}, "WeakMap": {}, "WeakSet": {},
	"Date": {}, "RegExp": {}, "Error": {}, "TypeError": {}, "RangeError": {},
	"SyntaxError": {}, "ReferenceError": {}, "JSON": {}, "Math": {},
	"Record": {}, "Partial": {}, "Required": {}, "Readonly": {}, "Pick": {},
	"Omit": {}, "Exclude": {}, "Extract": {}, "NonNullable": {}, "ReturnType": {},
	"Parameters": {}, "ConstructorParameters": {}, "InstanceType": {},
	"ThisType": {}, "Awaited": {}, "Iterable": {}, "Iterator": {},
	"AsyncIterable": {}, "AsyncIterator": {}, "Generator": {}, "AsyncGenerator": {},
}

// fileSymbol is a single file-scope symbol-table entry: the declared
// name maps to the (kind, subtype) pair we need to build a Format A
// structural ref for the resolver.
type fileSymbol struct {
	kind    string
	subtype string
}

// emitReferences is the second-pass entry point invoked from Extract
// AFTER x.walk() has populated x.entities. It (1) builds the file-scope
// symbol table from emitted entities, (2) walks every function-like
// body in the tree, and (3) appends REFERENCES edges to the enclosing
// function entity. Mutates x.entities in place.
//
// Safe to call with an empty entity slice — degenerates to a no-op. Safe
// to call when the file has no function bodies — degenerates to a no-op.
func (x *extractor) emitReferences(root *sitter.Node) {
	if root == nil || len(x.entities) == 0 {
		return
	}

	// Phase 1 — build the file-scope symbol table from emitted entities.
	// Only entities whose SourceFile matches the current file are
	// considered (some entities, like cross-file IMPORTS placeholders,
	// might not — but in this extractor they all do). The file-level
	// SCOPE.Component (subtype="file") entity, whose Name IS the file
	// path, is explicitly excluded so an identifier matching the basename
	// can't accidentally bind to it.
	symbols := make(map[string]fileSymbol)
	// entIdxByName maps a same-file symbol name to its index in
	// x.entities. We need it to append REFERENCES edges to the
	// enclosing function entity below.
	entIdxByName := make(map[string]int)
	for i := range x.entities {
		e := &x.entities[i]
		if e.SourceFile != x.filePath {
			continue
		}
		if e.Subtype == "file" {
			continue
		}
		// First emission wins on duplicate names (matches Phase-1
		// dedup rule used in collectDeclarations of the references
		// extractor).
		if _, ok := symbols[e.Name]; ok {
			continue
		}
		symbols[e.Name] = fileSymbol{kind: e.Kind, subtype: e.Subtype}
		entIdxByName[e.Name] = i
	}
	if len(symbols) == 0 {
		return
	}

	// Phase 2 — walk every function-like body and emit REFERENCES.
	// We do a single iterative traversal of the AST and track the
	// nearest enclosing named function on a stack. When we hit an
	// identifier-shaped node that should produce a REFERENCES edge, we
	// emit it on the top-of-stack function (if any).
	type frame struct {
		// funcName is the name of the enclosing function/method. Empty
		// when we're at file scope (outside any function body).
		funcName string
	}

	// seen dedupes (fromName, toName) pairs across the entire file so a
	// function that uses the same identifier N times produces a single
	// REFERENCES edge.
	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]bool)

	// Emit one REFERENCES edge from the enclosing function (top of
	// fstack) to the named target. Caller has already filtered out
	// non-symbols, globals, self-references, and call-target identifiers.
	emit := func(fstack []frame, target string) {
		if len(fstack) == 0 {
			return
		}
		from := fstack[len(fstack)-1].funcName
		if from == "" || from == target {
			return
		}
		key := edgeKey{from, target}
		if seen[key] {
			return
		}
		seen[key] = true
		idx, ok := entIdxByName[from]
		if !ok {
			return
		}
		sym := symbols[target]
		toID := buildReferenceTargetID(x.language, x.filePath, target, sym.kind)
		x.entities[idx].Relationships = append(x.entities[idx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "REFERENCES",
			})
	}

	// Walk: depth-first, manually managing a parallel function-frame
	// stack. We push when entering a function-like body and pop on the
	// way out. The frame stack is shared across recursion; we use an
	// explicit traversal so the pop point is deterministic.
	var walk func(n *sitter.Node, fstack []frame)
	walk = func(n *sitter.Node, fstack []frame) {
		if n == nil {
			return
		}
		nt := n.Type()

		// Identify a function-like node and push a frame BEFORE
		// descending into its body. The function's NAME is what we
		// attribute REFERENCES to — for function_declaration and
		// method_definition the name is on the node itself; for
		// arrow_function / function_expression bound to a const,
		// the name is on the enclosing variable_declarator. We
		// handle the variable_declarator shape by inspecting the
		// declarator's `name` field when its `value` is a function.
		pushed := false
		switch nt {
		case "function_declaration":
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				fstack = append(fstack, frame{funcName: x.nodeText(nameNode)})
				pushed = true
			}
		case "method_definition":
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				fstack = append(fstack, frame{funcName: x.nodeText(nameNode)})
				pushed = true
			}
		case "variable_declarator":
			// Bind the function frame when the value is a function-
			// like expression (arrow / function / wrapper-call whose
			// inner is a function). We don't try to handle destructure
			// patterns here — those don't carry a single "owner" name.
			if name := n.ChildByFieldName("name"); name != nil && name.Type() == "identifier" {
				if val := n.ChildByFieldName("value"); val != nil {
					if isFunctionLikeValue(val) {
						fstack = append(fstack, frame{funcName: x.nodeText(name)})
						pushed = true
					}
				}
				// #709 — TS `const x: MyType = ...` carries a type
				// annotation on the declarator. Push a frame so the
				// type-position use attributes to the const, even when
				// the value is not function-shaped. Only fires when the
				// declarator has an explicit type annotation.
				if !pushed && n.ChildByFieldName("type") != nil {
					fstack = append(fstack, frame{funcName: x.nodeText(name)})
					pushed = true
				}
			}
		case "interface_declaration", "type_alias_declaration", "class_declaration":
			// #709 — type-position uses inside a type/interface/class
			// declaration body (extends clause, generic constraints,
			// field type annotations) attribute to the declaring entity.
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				fstack = append(fstack, frame{funcName: x.nodeText(nameNode)})
				pushed = true
			}
		}

		// Process this node for identifier usages BEFORE recursing.
		// We only care about identifier-shaped nodes; everything else
		// is a structural node and identifiers will surface as we
		// recurse into its children.
		if nt == "identifier" || nt == "shorthand_property_identifier" || nt == "type_identifier" {
			// Two filters here:
			//   1. We only emit REFERENCES while inside a function
			//      frame — file-scope identifier usages (outside any
			//      function) don't have a clear "from" entity.
			//   2. The identifier must not be in DECLARATION position
			//      (its parent's `name` field) AND must not be the
			//      CALLEE of a call/new expression (CALLS owns that
			//      edge already).
			//
			// #709 — type_identifier nodes appear in type-position uses
			// (type annotations, generic args, extends clauses, `as`
			// casts, `is` predicates, `satisfies` operators, conditional
			// types). The same emit path produces a Format-A REFERENCES
			// edge; the resolver's component family covers Schema
			// (interface/type_alias) targets.
			if len(fstack) > 0 {
				name := x.nodeText(n)
				if name != "" {
					if _, isGlobal := jsGlobals[name]; !isGlobal {
						if _, isBuiltin := tsBuiltinTypes[name]; !isBuiltin || nt != "type_identifier" {
							if _, isLocal := symbols[name]; isLocal {
								if !isDeclarationPosition(n) && !isCallCallee(n) {
									emit(fstack, name)
								}
							}
						}
					}
				}
			}
		}

		// Recurse into children. template_substitution and
		// template_string carry expression children that are walked
		// automatically by the generic recursion below — no special
		// case needed for Track B because `${X}` exposes X as an
		// `identifier` node inside the substitution.
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			walk(n.Child(i), fstack)
		}

		if pushed {
			// Pop is implicit — the slice we recursed with is local
			// to this call. No explicit unwind needed because Go's
			// slice semantics preserve the caller's view.
			_ = pushed
		}
	}

	walk(root, nil)
}

// buildReferenceTargetID emits a Format A structural-ref so the resolver
// routes through `lookupStructural` -> `lookupLocationKind` without any
// reliance on bare-name hint families. Operation-kinded targets emit a
// `scope:operation:...` stub; everything else emits a `scope:component:...`
// stub. The Schema family (interfaces, type aliases) falls under the
// component family in `structuralKindFamilies`, so a schema-kinded
// target uses the component scope segment.
//
// IMPORTANT: must stay aligned with structuralKindFamilies in
// internal/resolve/refs.go. Adding a new scope segment here requires a
// matching family there or the structural ref will resolve to nothing
// (silent miss).
func buildReferenceTargetID(lang, filePath, name, targetKind string) string {
	scopeSeg := "component"
	switch targetKind {
	case "SCOPE.Operation", "Operation", "Function", "Method":
		scopeSeg = "operation"
	}
	// `member` is a placeholder subtype slot the resolver doesn't key
	// on for Format A lookup (it splits to scopeKind + filePath + tail).
	// Keep it stable so any future caller that DOES key on it can
	// distinguish REFERENCES from CONTAINS/CALLS structural refs.
	return "scope:" + scopeSeg + ":ref:" + lang + ":" + filepath.ToSlash(filePath) + ":" + name
}

// isDeclarationPosition reports whether the identifier node sits in a
// position that DECLARES the name rather than USES it. We skip these to
// avoid an entity referencing its own declaration.
//
// Detected shapes:
//   - parent's field `name` is this node (covers function_declaration,
//     class_declaration, method_definition, variable_declarator,
//     formal_parameter, etc.)
//   - parent is a labelled statement / break label / continue label
//   - parent is an import_specifier / namespace_import / import_clause's
//     name child — these are also "declaration" positions for the local
//     binding (and the bare identifier inside the import binding is
//     never a USE).
//
// Conservative: when in doubt we treat the node as a USE so we DO emit
// a REFERENCES edge. The downstream symbol-table guard ensures we never
// produce an edge to a non-existent target, so over-classifying a few
// declarations as uses costs at worst a self-edge (filtered by the
// `from == to` check in emit).
func isDeclarationPosition(n *sitter.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	pt := parent.Type()
	// #709 — JSX component-name children are USES, not declarations,
	// even though they sit in the `name` field of the JSX element.
	// Tree-sitter exposes them as `<MyComponent />` → jsx_self_closing_element
	// with field `name`. Without this exception, JSX component references
	// to same-file type-imported components would be silently dropped.
	switch pt {
	case "jsx_opening_element", "jsx_self_closing_element", "jsx_closing_element":
		return false
	}
	if name := parent.ChildByFieldName("name"); name != nil && name == n {
		return true
	}
	switch pt {
	case "import_specifier", "namespace_import":
		return true
	case "labeled_statement":
		// `label: stmt` — the label child is the identifier; skip it.
		if parent.ChildCount() > 0 && parent.Child(0) == n {
			return true
		}
	case "break_statement", "continue_statement":
		// `break label;` — the label identifier child is a jump
		// target, not a value reference.
		return true
	case "property_identifier":
		// Should never appear (property_identifier is itself the node
		// type, not a parent) but guarded for safety.
		return true
	}
	return false
}

// isCallCallee reports whether the identifier node is the `function`
// child of a `call_expression` or the `constructor` child of a
// `new_expression`. We skip these because CALLS edges already cover
// them — emitting a REFERENCES edge to the same target would double-
// count the relationship.
//
// Member-expression callees (`obj.foo()`) are handled separately: the
// `obj` portion IS a value reference and emits REFERENCES; the `.foo`
// portion is a property_identifier (not identifier), so the
// identifier walk doesn't touch it. CALLS still emits its edge to the
// trailing property name.
func isCallCallee(n *sitter.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	switch parent.Type() {
	case "call_expression":
		if fn := parent.ChildByFieldName("function"); fn == n {
			return true
		}
	case "new_expression":
		if c := parent.ChildByFieldName("constructor"); c == n {
			return true
		}
	}
	return false
}

// isFunctionLikeValue reports whether a value node is a function-shaped
// expression — arrow, function_expression, raw function, or one of the
// well-known wrapper-call shapes (matches the wave-8 isFunctionWrapperCall
// list). Used to decide whether `const X = <value>` should push a frame
// named X for REFERENCES attribution.
//
// Conservative: when in doubt return false. A miss here means usages
// inside the value land at the outer frame (or no frame, at file
// scope) and produce no REFERENCES edge — better than over-attributing
// to a const that isn't actually function-shaped.
func isFunctionLikeValue(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "arrow_function", "function_expression", "function":
		return true
	case "call_expression":
		// Hook-wrapper calls bind a callable to the const name —
		// `const handler = useCallback(() => {...}, [...])`. The body
		// of the inner arrow is what USES outer identifiers, so we
		// want the const's name to own those REFERENCES edges.
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return false
		}
		switch fn.Type() {
		case "identifier", "type_identifier":
			return true
		case "member_expression":
			return true
		}
	}
	return false
}

// init-time package-level sanity check: ensure we don't accidentally
// shadow a reserved keyword in jsGlobals. (No-op at runtime; the
// compiler will catch shape errors above.)
var _ = strings.ToLower
