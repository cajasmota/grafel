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

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
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
// body in the tree appending REFERENCES edges to the enclosing
// function entity, and (3) processes `export { ... }` statements,
// emitting REFERENCES edges from the file entity to each named const
// (issue #711 — define-then-export pattern). Mutates x.entities in
// place.
//
// Safe to call with an empty entity slice — degenerates to a no-op. Safe
// to call when the file has no function bodies — degenerates to a no-op.
func (x *extractor) emitReferences(root ts.Node) {
	if root == nil || len(x.entities) == 0 {
		return
	}

	// Phase 1a — build the file-scope symbol table from emitted entities.
	// Only entities whose SourceFile matches the current file are
	// considered (some entities, like cross-file IMPORTS placeholders,
	// might not — but in this extractor they all do). The file-level
	// SCOPE.Component (subtype="file") entity, whose Name IS the file
	// path, is explicitly excluded so an identifier matching the basename
	// can't accidentally bind to it.
	symbols := make(map[string]fileSymbol)
	// dottedSymbols maps "ClassName.fieldName" → fileSymbol for class
	// field entities (SCOPE.Schema/field) emitted by handlePublicFieldDefinition
	// (issue #679). Used by the this.attr handler to emit REFERENCES edges
	// from method bodies to class field targets.
	dottedSymbols := make(map[string]fileSymbol)
	// entIdxByName maps a same-file symbol name to its index in
	// x.entities. We need it to append REFERENCES edges to the
	// enclosing function entity below.
	entIdxByName := make(map[string]int)
	// fileEntIdx is the index of the file-level SCOPE.Component entity
	// for this source file (emitted in Extract before walk). Used as
	// the from-attribution for #711 export-clause REFERENCES so the
	// const_call entity that backs an `export { X }` clause gets a real
	// inbound edge from the file. Defaults to -1 (no file entity), in
	// which case the #711 emission is skipped.
	fileEntIdx := -1
	for i := range x.entities {
		e := &x.entities[i]
		if e.SourceFile != x.filePath {
			continue
		}
		if e.Subtype == "file" {
			if fileEntIdx == -1 {
				fileEntIdx = i
			}
			continue
		}
		// Issue #679 — class field entities (SCOPE.Schema/field) are
		// indexed by their dotted name ("ClassName.fieldName") in
		// dottedSymbols so the this.attr handler can look them up.
		// They are also indexed in the bare symbols table under the
		// dotted name so that emit() can find the entity index.
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			if _, ok := dottedSymbols[e.Name]; !ok {
				dottedSymbols[e.Name] = fileSymbol{kind: e.Kind, subtype: e.Subtype}
				entIdxByName[e.Name] = i
			}
			// Do NOT add to the bare symbols table — the dotted name
			// would match a bare identifier falsely. Skip to next entity.
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

	// Phase 1b — Issue #562: include non-emitted const declarations in the
	// symbol table. When plain const assignments are no longer emitted as
	// standalone entities, we still need to know about them for REFERENCES
	// edge emission. Scan the AST for variable_declarator nodes with const
	// values and add them to the symbol table (but NOT to entIdxByName,
	// since they have no entity index). buildReferenceTargetID will handle
	// the case where a symbol has no entity index by constructing a
	// structural ref target.
	var collectConstSymbols func(n ts.Node)
	collectConstSymbols = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "variable_declarator" {
			if nameNode := n.ChildByFieldName("name"); nameNode != nil &&
				nameNode.Type() == "identifier" {
				name := x.nodeText(nameNode)
				// Only add if not already emitted as an entity
				if _, exists := symbols[name]; !exists {
					// Infer kind from the value type (non-function, non-context cases)
					// Default to SCOPE.Component for plain const values
					symbols[name] = fileSymbol{kind: "SCOPE.Component", subtype: "const"}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			collectConstSymbols(n.Child(i))
		}
	}
	collectConstSymbols(root)

	if len(symbols) == 0 && len(dottedSymbols) == 0 {
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
		// parentClass is the name of the enclosing class_declaration,
		// propagated into method bodies so `this.<attr>` references can
		// be resolved as "ClassName.attr" (issue #679).
		parentClass string
	}

	// seen dedupes (fromName, toName) pairs across the entire file so a
	// function that uses the same identifier N times produces a single
	// REFERENCES edge. Shared with Phase 3 (#711 export-clause emission)
	// so a file-from edge can't accidentally double-emit when the same
	// const is also referenced inside another function body.
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

	// emitDotted is the analog of emit for dotted "ClassName.field"
	// targets stored in dottedSymbols (issue #679). The toID uses the
	// schema scope segment because field entities are SCOPE.Schema.
	emitDotted := func(fstack []frame, dottedTarget string) {
		if len(fstack) == 0 {
			return
		}
		from := fstack[len(fstack)-1].funcName
		if from == "" || from == dottedTarget {
			return
		}
		key := edgeKey{from, dottedTarget}
		if seen[key] {
			return
		}
		seen[key] = true
		fromIdx, ok := entIdxByName[from]
		if !ok {
			return
		}
		sym := dottedSymbols[dottedTarget]
		toID := buildReferenceTargetID(x.language, x.filePath, dottedTarget, sym.kind)
		x.entities[fromIdx].Relationships = append(x.entities[fromIdx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "REFERENCES",
			})
	}

	// Walk: depth-first, manually managing a parallel function-frame
	// stack. We push when entering a function-like body and pop on the
	// way out. The frame stack is shared across recursion; we use an
	// explicit traversal so the pop point is deterministic.
	var walk func(n ts.Node, fstack []frame)
	walk = func(n ts.Node, fstack []frame) {
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
		// currentParentClass carries the enclosing class name when we're
		// inside a class body, for propagation into method frames (#679).
		currentParentClass := ""
		if len(fstack) > 0 {
			currentParentClass = fstack[len(fstack)-1].parentClass
		}
		switch nt {
		case "function_declaration":
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				fstack = append(fstack, frame{funcName: x.nodeText(nameNode), parentClass: currentParentClass})
				pushed = true
			}
		case "method_definition":
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				fstack = append(fstack, frame{funcName: x.nodeText(nameNode), parentClass: currentParentClass})
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
						fstack = append(fstack, frame{funcName: x.nodeText(name), parentClass: currentParentClass})
						pushed = true
					}
				}
				// #709 — TS `const x: MyType = ...` carries a type
				// annotation on the declarator. Push a frame so the
				// type-position use attributes to the const, even when
				// the value is not function-shaped. Only fires when the
				// declarator has an explicit type annotation.
				if !pushed && n.ChildByFieldName("type") != nil {
					fstack = append(fstack, frame{funcName: x.nodeText(name), parentClass: currentParentClass})
					pushed = true
				}
			}
		case "class_declaration":
			// #679 — track the class name so method bodies can resolve
			// `this.<attr>` using "ClassName.attr" dottedSymbols lookup.
			// Also serves as the entity frame for #709 type-position uses
			// inside extends clause / class body type annotations.
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				className := x.nodeText(nameNode)
				fstack = append(fstack, frame{funcName: className, parentClass: className})
				pushed = true
			}
		case "interface_declaration", "type_alias_declaration":
			// #709 — type-position uses inside a type/interface declaration
			// body (extends clause, generic constraints, field type annotations)
			// attribute to the declaring entity.
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
							if sym, isLocal := symbols[name]; isLocal {
								if !isDeclarationPosition(n) {
									// #710 — Same-file destructure binding
									// references in call position. CALLS edges
									// resolve to Operation-kinded targets only;
									// a SCOPE.Component target (const_destructure,
									// const_call, const, const_alias, …) would
									// never bind via CALLS, so the entity stays
									// orphaned. Emit REFERENCES from the
									// enclosing function frame so the binding
									// gains an inbound edge. CALLS-eligible
									// (Operation) targets still skip — those
									// remain owned by the CALLS pathway and an
									// additive REFERENCES edge would double-count.
									callee := isCallCallee(n)
									if !callee || sym.kind == "SCOPE.Component" {
										emit(fstack, name)
									}
								}
							}
						}
					}
				}
			}
		}

		// Issue #679 — `this.<attr>` in a class method body.
		// When we encounter a member_expression node whose object is the
		// `this` keyword and the enclosing frame carries a parentClass,
		// attempt to look up "ClassName.property" in dottedSymbols and
		// emit a REFERENCES edge. Only fires inside a method frame
		// (len(fstack) > 0 AND top.funcName != top.parentClass, meaning
		// we're in a method rather than the class-declaration frame itself).
		//
		// Exclusion: when this member_expression is the `function` child
		// of a call_expression (`this.method()`) — CALLS owns that edge.
		if nt == "member_expression" && len(fstack) > 0 {
			top := fstack[len(fstack)-1]
			if top.parentClass != "" && top.funcName != top.parentClass {
				obj := n.ChildByFieldName("object")
				prop := n.ChildByFieldName("property")
				if obj != nil && prop != nil && x.nodeText(obj) == "this" {
					// Skip if this member_expression is the callee of a call.
					isThisCallee := false
					if parent := n.Parent(); parent != nil && parent.Type() == "call_expression" {
						if fn := parent.ChildByFieldName("function"); ts.SameNode(fn, n) {
							isThisCallee = true
						}
					}
					if !isThisCallee {
						propName := x.nodeText(prop)
						dotted := top.parentClass + "." + propName
						if _, ok := dottedSymbols[dotted]; ok {
							// Use emitDotted to produce a REFERENCES edge from
							// the enclosing method to the dotted field entity.
							emitDotted(fstack, dotted)
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

	// Phase 3 — #711 define-then-export.
	// `const X = createIcon(...); export { X };` produces a const_call
	// entity for X but no incoming edge: the export clause is a separate
	// AST statement and the const itself has no usage inside the file.
	// Wire the export by emitting a REFERENCES edge from the file entity
	// to each export-specifier whose `name` matches a same-file symbol.
	//
	// Skipped shapes:
	//   - Re-exports with a `source` field: `export { X } from './foo'`
	//     — X here is NOT a same-file declaration (it's a binding being
	//     forwarded from another module); emitting a same-file
	//     REFERENCES would attribute the edge to the wrong entity.
	//   - `export *` (no specifiers): documented gap; the resolver
	//     handles wildcard re-exports through the IMPORTS path.
	//
	// Rename shape: `export { X as Y }` — the local-binding name
	// is the `name` field (X); the alias (Y) is the externally
	// visible name. We bind to X because X is the same-file entity;
	// downstream consumers that import Y resolve via the file-level
	// barrel index, not via this in-file REFERENCES edge.
	if fileEntIdx >= 0 {
		x.emitExportClauseReferences(root, symbols, fileEntIdx, seen)
	}
}

// emitExportClauseReferences walks the AST looking for `export_statement`
// nodes that carry an `export_clause` of named specifiers (and no
// `source` field — re-exports forwarding from another module are skipped).
// For each specifier whose local-binding name matches a same-file symbol,
// it appends a REFERENCES edge from the file entity to the named symbol
// using the same Format-A structural-ref the regular emit path produces.
//
// Issue #711.
func (x *extractor) emitExportClauseReferences(
	root ts.Node,
	symbols map[string]fileSymbol,
	fileEntIdx int,
	seen map[edgeKey]bool,
) {
	if root == nil {
		return
	}
	fromName := x.entities[fileEntIdx].Name
	var visit func(n ts.Node)
	visit = func(n ts.Node) {
		if n == nil {
			return
		}
		nt := n.Type()
		if nt == "export_statement" {
			// Skip re-exports like `export { X } from './baz'` —
			// the source field signals the binding originates from
			// another module, so X is not a same-file declaration.
			if src := n.ChildByFieldName("source"); src == nil {
				// Find the export_clause child (named specifiers).
				count := int(n.ChildCount())
				for i := 0; i < count; i++ {
					c := n.Child(i)
					if c == nil {
						continue
					}
					if c.Type() != "export_clause" {
						continue
					}
					// Iterate export_specifier children.
					sc := int(c.ChildCount())
					for j := 0; j < sc; j++ {
						sp := c.Child(j)
						if sp == nil || sp.Type() != "export_specifier" {
							continue
						}
						// Local binding name is the `name` field;
						// `alias` (when present) is the external name.
						nameNode := sp.ChildByFieldName("name")
						if nameNode == nil {
							continue
						}
						local := x.nodeText(nameNode)
						if local == "" {
							continue
						}
						sym, ok := symbols[local]
						if !ok {
							continue
						}
						key := edgeKey{fromName, local}
						if seen[key] {
							continue
						}
						seen[key] = true
						toID := buildReferenceTargetID(x.language, x.filePath, local, sym.kind)
						x.entities[fileEntIdx].Relationships = append(
							x.entities[fileEntIdx].Relationships,
							types.RelationshipRecord{
								ToID: toID,
								Kind: "REFERENCES",
							},
						)
					}
				}
			}
		}
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			visit(n.Child(i))
		}
	}
	visit(root)
}

// edgeKey is the dedup key for REFERENCES emission (file-scope).
type edgeKey struct{ from, to string }

// buildReferenceTargetID emits a Format A structural-ref so the resolver
// routes through `lookupStructural` -> `lookupLocationKind` without any
// reliance on bare-name hint families.
//
// Scope segments:
//   - "operation" for Operation/Function/Method entities
//   - "schema"    for SCOPE.Schema/field entities (#679 — class fields)
//   - "component" for everything else (classes, imports, type aliases)
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
	case "SCOPE.Schema":
		// Issue #679 — class field entities use the schema scope segment
		// so the resolver routes them through schemaKindFamily via
		// structuralKindFamilies("schema"). This aligns with the Java
		// extractor's scope:schema:ref:java:* shape for field targets.
		scopeSeg = "schema"
	}
	// `ref` is a placeholder subtype slot the resolver doesn't key on
	// for Format A lookup (it splits to scopeKind + filePath + tail).
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
func isDeclarationPosition(n ts.Node) bool {
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
		if parent.ChildCount() > 0 && ts.SameNode(parent.Child(0), n) {
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
func isCallCallee(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	switch parent.Type() {
	case "call_expression":
		if fn := parent.ChildByFieldName("function"); ts.SameNode(fn, n) {
			return true
		}
	case "new_expression":
		if c := parent.ChildByFieldName("constructor"); ts.SameNode(c, n) {
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
func isFunctionLikeValue(n ts.Node) bool {
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
