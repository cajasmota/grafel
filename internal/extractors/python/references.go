// references.go — REFERENCES-edge emission for the Python extractor.
//
// Analog of #641 for Python. The Python extractor previously emitted ~0
// REFERENCES edges (audit 2026-05-19: REFERENCES/fn ≈ 0.00 on the
// click / flask-realworld / django-realworld / pandas / client-fixture-a
// corpora). Every same-scope identifier use, every `self.<attr>`
// reference, every f-string interpolation, and every imported-name
// reference outside of a CALLS context produced no edge — leaving
// thousands of entities orphan in the audit.
//
// This pass mirrors the JS/TS emitReferences (#641):
//
//   1. Build a file-scope symbol table from the entities already emitted
//      by the primary extractor pass (extractImports, walkNode). The
//      table maps Name → entity-kind metadata so we can build the right
//      structural-ref Format A target ID for each reference.
//
//   2. Walk every function/method body for identifier-shaped nodes that
//      are NOT in declaration position and are NOT the function child of
//      a `call` node (those are owned by CALLS). For each, look up the
//      identifier in the symbol table and emit a REFERENCES edge from
//      the enclosing function entity.
//
//   3. Handle Python-specific shapes:
//        - `self.<attr>` → look up `<parentClass>.<attr>` in the symbol
//          table (class fields emitted by extractClassFields #526).
//        - f-string `f"...{X}..."` → the tree-sitter Python grammar
//          surfaces the interpolated expression's identifiers as plain
//          `identifier` nodes inside an `interpolation` parent, which
//          the generic recursion already walks.
//        - bare `<imported_name>` → file-scope symbol table includes
//          every import entity's Name (the dotted module path); we
//          additionally index every binding's local_name so a bare use
//          of an imported leaf resolves.
//
//   4. Skip well-known Python builtins (print, len, str, int, range,
//      list, dict, ...) so the bare-name resolver isn't bloated with
//      noise edges that would never bind to a project entity.
//
// Cap: one REFERENCES edge per (from_id, to_id) pair to prevent N-uses
// inflation. Self-references (a function body referencing its own name)
// are filtered. CALLS edges remain the existing pathway — REFERENCES is
// strictly additive and only fires for non-call identifier shapes.
//
// The Format A structural-ref shape this emits is:
//
//	scope:operation:ref:python:<file>:<name>           — function/method targets
//	scope:component:ref:python:<file>:<name>           — class / module / file targets
//	scope:schema:ref:python:<file>:<parentClass>.<attr> — class-field (#526) targets
//
// The resolver's structuralKindFamilies covers operation, component,
// and schema scope segments; the existing lookupStructural →
// lookupLocationKind path binds these edges to their declaration without
// any new dispatcher work and without any reliance on bare-name hint
// families.

package python

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// pythonBuiltins is the conservative allowlist of well-known Python
// built-ins, common stdlib types, and language keywords / pseudo-names
// that should NEVER produce a REFERENCES edge to a project entity. The
// list is intentionally short — anything that's almost-always a
// built-in, almost-never a user-declared name. A user-declared name
// that shadows a built-in costs a missed REFERENCES edge; the opposite
// (treating a user name as a built-in) produces no edge AT ALL because
// the symbol-table guard runs first, so there is no over-emission risk.
var pythonBuiltins = map[string]struct{}{
	// Types & constructors
	"int": {}, "float": {}, "str": {}, "bytes": {}, "bytearray": {},
	"bool": {}, "list": {}, "dict": {}, "set": {}, "frozenset": {},
	"tuple": {}, "type": {}, "object": {}, "complex": {}, "memoryview": {},
	"range": {}, "slice": {}, "property": {}, "classmethod": {}, "staticmethod": {},
	"super": {}, "enumerate": {}, "zip": {}, "filter": {}, "map": {}, "reversed": {},
	"sorted": {}, "iter": {}, "next": {},
	// I/O & introspection
	"print": {}, "input": {}, "open": {}, "repr": {}, "format": {}, "hash": {},
	"id": {}, "vars": {}, "dir": {}, "help": {}, "len": {}, "ord": {}, "chr": {},
	"hex": {}, "oct": {}, "bin": {}, "abs": {}, "round": {}, "divmod": {}, "pow": {},
	"sum": {}, "min": {}, "max": {}, "any": {}, "all": {},
	// Type / attribute machinery
	"isinstance": {}, "issubclass": {}, "callable": {}, "getattr": {}, "setattr": {},
	"hasattr": {}, "delattr": {}, "globals": {}, "locals": {}, "eval": {}, "exec": {},
	"compile": {}, "__import__": {},
	// Exceptions (most common)
	"Exception": {}, "ValueError": {}, "TypeError": {}, "KeyError": {},
	"IndexError": {}, "AttributeError": {}, "NameError": {}, "RuntimeError": {},
	"StopIteration": {}, "StopAsyncIteration": {}, "GeneratorExit": {},
	"NotImplementedError": {}, "OSError": {}, "FileNotFoundError": {},
	"PermissionError": {}, "ImportError": {}, "ModuleNotFoundError": {},
	"ZeroDivisionError": {}, "ArithmeticError": {}, "LookupError": {},
	"UnicodeError": {}, "UnicodeDecodeError": {}, "UnicodeEncodeError": {},
	"AssertionError": {}, "BaseException": {}, "EOFError": {}, "MemoryError": {},
	"SystemExit": {}, "KeyboardInterrupt": {}, "RecursionError": {},
	"OverflowError": {}, "FloatingPointError": {},
	"BufferError": {}, "ConnectionError": {}, "TimeoutError": {},
	"BrokenPipeError": {}, "ConnectionAbortedError": {}, "ConnectionRefusedError": {},
	"ConnectionResetError": {}, "FileExistsError": {}, "IsADirectoryError": {},
	"NotADirectoryError": {}, "InterruptedError": {}, "ProcessLookupError": {},
	"ChildProcessError": {}, "BlockingIOError": {}, "ReferenceError": {},
	"SyntaxError": {}, "IndentationError": {}, "TabError": {}, "SystemError": {},
	"Warning": {}, "DeprecationWarning": {}, "PendingDeprecationWarning": {},
	"UserWarning": {}, "FutureWarning": {}, "ImportWarning": {},
	"UnicodeWarning": {}, "BytesWarning": {}, "ResourceWarning": {},
	// Pseudo-names / literals / keywords that appear as identifiers
	"True": {}, "False": {}, "None": {}, "NotImplemented": {}, "Ellipsis": {},
	"self": {}, "cls": {}, "_": {}, "__name__": {}, "__main__": {},
	"__file__": {}, "__doc__": {}, "__class__": {}, "__init__": {},
	"__dict__": {}, "__module__": {}, "__qualname__": {},
}

// pySymbol is a single file-scope symbol-table entry, used to build the
// right structural-ref Format A target for each reference.
type pySymbol struct {
	kind    string
	subtype string
	name    string // emitted entity Name (may be "Class.method" for methods or "Class.attr" for fields)
}

// emitReferences is the second-pass entry point invoked from Extract
// AFTER the primary walk + extractImports + extractClassFields have
// populated entities. It builds a file-scope symbol table from emitted
// entities, walks every function-like body, and appends REFERENCES
// edges to the enclosing function entity.
//
// Mutates entities in place. Safe to call with an empty slice — no-op.
func emitReferences(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	// Phase 1 — build the file-scope symbol table.
	//
	// We index by THE NAME AS REFERENCED IN SOURCE, not by the entity's
	// emitted Name. For module-level functions and classes those are
	// identical; for methods the emitted Name is "Class.method" but the
	// in-source reference shape varies (`self.method`, `cls.method`, or
	// just `method` from inside the same class). We index methods under
	// their leaf name AND under their dotted form so both shapes resolve.
	//
	// For #526 class-field entities (Subtype="field", Name="Class.attr")
	// we index under the dotted form so `self.<attr>` lookups (which
	// the walk below qualifies with the enclosing class) bind directly.
	bareSymbols := make(map[string]pySymbol)   // "foo" → fn/class/module
	dottedSymbols := make(map[string]pySymbol) // "Class.foo" → method/field
	entIdxByName := make(map[string]int)       // bare "foo" → entity index
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "file":
			// Issue #577 file-level carrier entity — its Name (the file path)
			// must NOT be added to bareSymbols as a reference target. But its
			// IMPORTS edges ARE indexed (issue #693): IMPORTS edges now live on
			// the file entity rather than standalone module placeholder entities,
			// so we read local_name here instead of from subtype="module" entities.
			for _, r := range e.Relationships {
				if r.Kind != "IMPORTS" {
					continue
				}
				local := r.Properties["local_name"]
				if local == "" {
					continue
				}
				// Use the imported_name as the symbol name so the structural-ref
				// resolves back to the right ext: entity (e.g. "Optional" not
				// the full module path "typing.Optional").
				importedName := r.Properties["imported_name"]
				if importedName == "" {
					importedName = r.Properties["source_module"]
				}
				if importedName == "" {
					importedName = local
				}
				if _, exists := bareSymbols[local]; !exists {
					bareSymbols[local] = pySymbol{
						kind:    "SCOPE.Component",
						subtype: "module",
						name:    importedName,
					}
					entIdxByName[local] = i
				}
			}
			continue // file entity itself is not a reference target
		case e.Kind == "SCOPE.Operation":
			// Methods: Name = "Class.method"; index by leaf AND dotted.
			leaf := e.Name
			if dot := strings.LastIndexByte(e.Name, '.'); dot >= 0 {
				leaf = e.Name[dot+1:]
				if _, exists := dottedSymbols[e.Name]; !exists {
					dottedSymbols[e.Name] = pySymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
				}
			}
			if _, exists := bareSymbols[leaf]; !exists {
				bareSymbols[leaf] = pySymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
				entIdxByName[leaf] = i
			}
		case e.Kind == "SCOPE.Schema" && e.Subtype == "field":
			// #526 class-field: Name = "Class.attr"; only useful via
			// dotted lookup (self.attr / cls.attr below).
			if _, exists := dottedSymbols[e.Name]; !exists {
				dottedSymbols[e.Name] = pySymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		case e.Kind == "SCOPE.Component" && e.Subtype == "class":
			if _, exists := bareSymbols[e.Name]; !exists {
				bareSymbols[e.Name] = pySymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
				entIdxByName[e.Name] = i
			}
		}
	}
	if len(bareSymbols) == 0 && len(dottedSymbols) == 0 {
		return
	}

	// Phase 2 — walk every function-like body, tracking the enclosing
	// function entity (by its emitted Name) and its enclosing class
	// (so `self.<attr>` resolves to "<class>.<attr>").
	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]bool)

	emit := func(fstack []frame, sym pySymbol) {
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
		idx, ok := findEntityIndex(*entities, top.funcEmittedName, file.Path)
		if !ok {
			return
		}
		toID := buildPyReferenceTargetID(file.Path, sym)
		(*entities)[idx].Relationships = append((*entities)[idx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "REFERENCES",
			})
	}

	// Walk recursively. parentClass propagates down class_definition
	// bodies; the function frame stack grows on function_definition /
	// lambda nodes.
	var walk func(n ts.Node, parentClass string, fstack []frame)
	walk = func(n ts.Node, parentClass string, fstack []frame) {
		if n == nil {
			return
		}
		nt := n.Type()

		// Class bodies update parentClass.
		if nt == "class_definition" {
			nameNode := n.ChildByFieldName("name")
			cls := ""
			if nameNode != nil {
				cls = nodeText(nameNode, file.Content)
			}
			childCls := cls
			if parentClass != "" && cls != "" {
				childCls = parentClass + "." + cls
			}
			body := n.ChildByFieldName("body")
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), childCls, fstack)
				}
			}
			return
		}

		// decorated_definition wraps a class or function; recurse into
		// the inner definition with the same parentClass.
		if nt == "decorated_definition" {
			inner := n.ChildByFieldName("definition")
			if inner != nil {
				walk(inner, parentClass, fstack)
			}
			return
		}

		// function_definition / lambda push a new frame.
		if nt == "function_definition" || nt == "lambda" {
			var leaf, emitted string
			if nt == "function_definition" {
				if nameNode := n.ChildByFieldName("name"); nameNode != nil {
					leaf = nodeText(nameNode, file.Content)
					emitted = leaf
					if parentClass != "" {
						emitted = parentClass + "." + leaf
					}
				}
			}
			newFrame := frame{funcEmittedName: emitted, funcLeafName: leaf, parentClass: parentClass}
			// For lambdas the emitted name is empty — REFERENCES from
			// inside a lambda is attributed to the enclosing function
			// frame (next-up on the stack). We accomplish this by NOT
			// pushing a frame when leaf is empty.
			newStack := fstack
			if emitted != "" {
				newStack = append(fstack, newFrame)
			}
			// Function body walk. Inside a function we must STILL
			// recurse into nested classes / functions (closures, inner
			// classes) so we don't miss their references.
			count := int(n.ChildCount())
			for i := 0; i < count; i++ {
				walk(n.Child(i), parentClass, newStack)
			}
			return
		}

		// Identifier-shaped node: try to emit a REFERENCES edge.
		if nt == "identifier" {
			handleIdentifier(n, file, parentClass, fstack, bareSymbols, dottedSymbols, emit)
		} else if nt == "attribute" {
			// `self.X` / `cls.X` shape: look up X in dottedSymbols
			// keyed by the enclosing class. Always recurse afterwards
			// so the receiver itself (an identifier, possibly an
			// imported name) ALSO produces a REFERENCES edge if
			// applicable.
			handleAttribute(n, file, fstack, dottedSymbols, emit)
		}

		// Recurse into all children.
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			walk(n.Child(i), parentClass, fstack)
		}
	}

	walk(root, "", nil)
}

// findEntityIndex returns the index of the file-local entity whose Name
// matches the supplied emittedName. Linear scan — acceptable because
// the per-file entity count is in the dozens and this is called once
// per unique (from, to) REFERENCES pair.
func findEntityIndex(entities []types.EntityRecord, emittedName, filePath string) (int, bool) {
	for i := range entities {
		if entities[i].SourceFile == filePath && entities[i].Name == emittedName {
			return i, true
		}
	}
	return -1, false
}

// handleIdentifier handles a bare `identifier` node:
//   - skip if in declaration position (parent's `name` field is this node)
//   - skip if this is the `function` child of a `call` (CALLS owns it)
//   - skip if it's the receiver child of an attribute access whose
//     parent is a `call` (e.g. `obj` in `obj.foo()` — also CALLS-owned)
//   - skip Python built-ins
//   - skip self-name (an identifier matching the enclosing function's leaf)
//   - otherwise look up in bareSymbols and emit
func handleIdentifier(
	n ts.Node,
	file extractor.FileInput,
	parentClass string,
	fstack []frame,
	bareSymbols map[string]pySymbol,
	_ map[string]pySymbol,
	emit func([]frame, pySymbol),
) {
	if len(fstack) == 0 {
		return
	}
	name := nodeText(n, file.Content)
	if name == "" {
		return
	}
	if _, isBuiltin := pythonBuiltins[name]; isBuiltin {
		return
	}
	if isPyDeclarationPosition(n) {
		return
	}
	if isPyCallCallee(n) {
		return
	}
	sym, ok := bareSymbols[name]
	if !ok {
		return
	}
	// Self-reference filter: a function body uses its own name (e.g.
	// recursive call without parens, default-arg expression). Drop.
	top := fstack[len(fstack)-1]
	if name == top.funcLeafName {
		return
	}
	emit(fstack, sym)
}

// handleAttribute handles `obj.attr` nodes:
//   - if obj is `self` and the enclosing class has a `Class.attr` entry
//     in dottedSymbols, emit REFERENCES to that entity.
//   - if obj is `cls`, same (class-method receiver).
//   - otherwise leave the receiver to the generic identifier walk.
func handleAttribute(
	n ts.Node,
	file extractor.FileInput,
	fstack []frame,
	dottedSymbols map[string]pySymbol,
	emit func([]frame, pySymbol),
) {
	if len(fstack) == 0 {
		return
	}
	// Skip when this attribute IS the `function` child of an outer
	// call_expression — CALLS owns that edge.
	if parent := n.Parent(); parent != nil && parent.Type() == "call" {
		if fn := parent.ChildByFieldName("function"); fn == n {
			return
		}
	}
	obj := n.ChildByFieldName("object")
	attr := n.ChildByFieldName("attribute")
	if obj == nil || attr == nil {
		return
	}
	if obj.Type() != "identifier" {
		return
	}
	recv := nodeText(obj, file.Content)
	if recv != "self" && recv != "cls" {
		return
	}
	top := fstack[len(fstack)-1]
	if top.parentClass == "" {
		return
	}
	attrName := nodeText(attr, file.Content)
	if attrName == "" {
		return
	}
	dotted := top.parentClass + "." + attrName
	sym, ok := dottedSymbols[dotted]
	if !ok {
		return
	}
	// Self-reference filter.
	if dotted == top.funcEmittedName {
		return
	}
	emit(fstack, sym)
}

// frame is defined inside emitReferences via closure typing; redeclared
// at package scope so the helper signatures above can refer to it.
type frame struct {
	funcEmittedName string
	funcLeafName    string
	parentClass     string
}

// isPyDeclarationPosition reports whether the identifier node sits in a
// position that DECLARES the name rather than USES it.
//
// Recognised shapes (all return true):
//
//	parent's field `name` is this node — function_definition,
//	  class_definition, parameter (typed_parameter, default_parameter,
//	  typed_default_parameter), keyword_argument, etc.
//	parent is `aliased_import` / `import_from_statement` / `import_statement`
//	parent is `as_pattern` and this is the `alias` field (e.g.
//	  `except X as e:` — `e` is a declaration).
//	parent is `global_statement` / `nonlocal_statement`
//	parent is `assignment` and this is the `left` field
//	parent is `for_in_clause` / `for_statement` and this is the `left` field
func isPyDeclarationPosition(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if nameField := parent.ChildByFieldName("name"); nameField != nil && nameField == n {
		return true
	}
	if leftField := parent.ChildByFieldName("left"); leftField != nil && leftField == n {
		// Assignment LHS, augmented_assignment LHS, for-loop target.
		pt := parent.Type()
		if pt == "assignment" || pt == "augmented_assignment" ||
			pt == "for_statement" || pt == "for_in_clause" {
			return true
		}
	}
	if aliasField := parent.ChildByFieldName("alias"); aliasField != nil && aliasField == n {
		return true
	}
	switch parent.Type() {
	case "aliased_import", "import_from_statement", "import_statement",
		"global_statement", "nonlocal_statement", "dotted_name", "as_pattern":
		return true
	case "keyword_argument":
		// `func(name=value)` — the `name` slot is a keyword, not a value
		// reference. Tree-sitter typically exposes this as field "name";
		// already covered above, but explicit case keeps the filter
		// robust to grammar-version drift.
		if first := parent.NamedChild(0); first == n {
			return true
		}
	}
	return false
}

// isPyCallCallee reports whether the identifier node is the `function`
// child of a `call` node. CALLS owns that edge — REFERENCES would
// double-count.
func isPyCallCallee(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if parent.Type() != "call" {
		return false
	}
	return parent.ChildByFieldName("function") == n
}

// buildPyReferenceTargetID emits a Format A structural-ref for the
// resolver's lookupStructural → lookupLocationKind path. Operation-
// kinded targets emit `scope:operation:ref:...`, Schema-kinded class
// fields emit `scope:schema:ref:...`, everything else emits
// `scope:component:ref:...`. Must stay aligned with
// structuralKindFamilies in internal/resolve/refs.go.
func buildPyReferenceTargetID(filePath string, sym pySymbol) string {
	scopeSeg := "component"
	switch sym.kind {
	case "SCOPE.Operation":
		scopeSeg = "operation"
	case "SCOPE.Schema":
		scopeSeg = "schema"
	}
	return "scope:" + scopeSeg + ":ref:python:" + filepath.ToSlash(filePath) + ":" + sym.name
}
