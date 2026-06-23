// references.go — REFERENCES-edge emission for the Java extractor.
//
// Analog of #641 (JS/TS) and #650 (Python) for Java/JVM. The Java
// extractor previously emitted ~0 REFERENCES edges per method body —
// every same-scope identifier use, every `this.<field>` reference,
// every static `ClassName.MEMBER` reference, and every imported-name
// reference outside of a CALLS context produced no edge. On the
// fixture-d (Quarkus) corpus this contributed to a 63.4% orphan rate;
// ~616 entities are recoverable using the same two-track pattern that
// worked for JS/TS and Python.
//
// This pass mirrors the JS/TS/Python emitReferences:
//
//   1. Build a file-scope symbol table from the entities already emitted
//      by the primary extractor pass. The table maps Name → entity-kind
//      metadata so we can build the right structural-ref Format A
//      target ID for each reference.
//
//   2. Walk every method/constructor body for identifier-shaped nodes
//      that are NOT in declaration position and are NOT the callee of a
//      method_invocation / object_creation_expression (those are owned
//      by CALLS). For each, look up the identifier in the symbol table
//      and emit a REFERENCES edge from the enclosing operation entity.
//
//   3. Handle Java-specific shapes:
//        - `this.<field>` (field_access with `this` receiver) → look up
//          `<EnclosingClass>.<field>` against the symbol table.
//        - `<field>` (implicit this) → bare-name lookup against the
//          dotted symbol table keyed by the enclosing class.
//        - `ClassName.staticMember` (field_access with PascalCase
//          receiver) → look up `ClassName.staticMember` directly.
//        - `<TypeName>` in expressions (e.g. `Foo.class`, `new Foo()`
//          inside a cast) — the bare identifier resolves against the
//          symbol table's class entries.
//        - Imported-name references — the IMPORTS pass stamps
//          Properties["local_name"] on every non-wildcard import; the
//          symbol-table builder indexes that as a file-scope name.
//
//   4. Skip Java reserved keywords (`int`, `void`, `class`, `return`,
//      ...) and well-known language-level identifiers so the bare-name
//      resolver isn't bloated with noise edges that would never bind to
//      a project entity.
//
// Cap: one REFERENCES edge per (from_id, to_id) pair to prevent N-uses
// inflation. Self-references (a method body referencing its own emitted
// name) are filtered. CALLS edges remain the existing pathway —
// REFERENCES is strictly additive and only fires for non-call
// identifier shapes.
//
// The Format A structural-ref shape this emits is:
//
//	scope:operation:ref:java:<file>:<name>            — method targets
//	scope:component:ref:java:<file>:<name>            — class / interface targets
//	scope:schema:ref:java:<file>:<EnclosingClass>.<field> — field targets
//
// The resolver's structuralKindFamilies covers operation, component,
// and schema scope segments; the existing lookupStructural →
// lookupLocationKind path binds these edges to their declaration without
// any new dispatcher work and without any reliance on bare-name hint
// families.

package java

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// javaReservedNames is the conservative allowlist of Java reserved
// keywords, language-level pseudo-names, and primitive type names that
// should NEVER produce a REFERENCES edge to a project entity. A
// user-declared name that shadows a reserved word is impossible (the
// parser rejects it), so this list is purely a noise filter on the
// identifier walk.
var javaReservedNames = map[string]struct{}{
	// Primitive types
	"boolean": {}, "byte": {}, "char": {}, "double": {}, "float": {},
	"int": {}, "long": {}, "short": {}, "void": {},
	// Control flow / modifiers / declarations
	"abstract": {}, "assert": {}, "break": {}, "case": {}, "catch": {},
	"class": {}, "const": {}, "continue": {}, "default": {}, "do": {},
	"else": {}, "enum": {}, "extends": {}, "final": {}, "finally": {},
	"for": {}, "goto": {}, "if": {}, "implements": {}, "import": {},
	"instanceof": {}, "interface": {}, "native": {}, "new": {}, "package": {},
	"private": {}, "protected": {}, "public": {}, "return": {}, "static": {},
	"strictfp": {}, "super": {}, "switch": {}, "synchronized": {}, "this": {},
	"throw": {}, "throws": {}, "transient": {}, "try": {}, "volatile": {},
	"while": {}, "yield": {}, "record": {}, "sealed": {}, "permits": {},
	"non-sealed": {}, "var": {},
	// Literals / pseudo-names
	"true": {}, "false": {}, "null": {},
}

// javaSymbol is a single file-scope symbol-table entry, used to build
// the right structural-ref Format A target for each reference.
type javaSymbol struct {
	kind    string
	subtype string
	name    string // emitted entity Name (may be "Class.method" / "Class.field")
}

// javaFrame tracks the enclosing operation / class while walking a
// file's CST during reference emission.
type javaFrame struct {
	funcEmittedName string // "" outside a method/constructor; "Class.method" inside
	funcLeafName    string // bare leaf name of the enclosing operation
	parentClass     string // immediate enclosing class/interface/enum name
}

// emitReferences is the second-pass entry point invoked from Extract
// AFTER the primary walk + buildImport have populated entities. It
// builds a file-scope symbol table from emitted entities, walks every
// method/constructor body, and appends REFERENCES edges to the
// enclosing operation entity.
//
// Mutates entities in place. Safe to call with an empty slice — no-op.
func emitReferences(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	// Phase 1 — build the file-scope symbol table.
	//
	// Index by THE NAME AS REFERENCED IN SOURCE. For methods emitted
	// with Name="Class.method" (#65) we index both the leaf name
	// (implicit-this shape) and the dotted form (this.method /
	// Class.method). For fields emitted as "name" (bare) we ALSO
	// synthesise a "Class.name" key by examining the field's enclosing
	// class via SourceLine bookkeeping — but the Java extractor's
	// buildField emits a bare Name today, so we cannot get the
	// enclosing-class qualification at extraction time without a deeper
	// walk. Conservative bias: index fields under bare name only.
	// `this.<field>` resolves via the bare-name path (see handleField
	// below).
	bareSymbols := make(map[string]javaSymbol)   // "foo" → method/class/import
	dottedSymbols := make(map[string]javaSymbol) // "Class.foo" → method
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		if e.Subtype == "file" {
			// Issue #577 file-level carrier entity — never reference
			// it via REFERENCES; its Name is the file path.
			continue
		}
		switch {
		case e.Kind == "SCOPE.Operation":
			// Methods: Name = "Class.method" (#65) for class members,
			// bare for module-level. Index by leaf AND dotted.
			leaf := e.Name
			if dot := strings.LastIndexByte(e.Name, '.'); dot >= 0 {
				leaf = e.Name[dot+1:]
				if _, exists := dottedSymbols[e.Name]; !exists {
					dottedSymbols[e.Name] = javaSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
				}
			}
			if _, exists := bareSymbols[leaf]; !exists {
				bareSymbols[leaf] = javaSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		case e.Kind == "SCOPE.Schema" && e.Subtype == "field":
			// Issue #690 — fields are emitted with qualified names
			// "<Class>.<field>" (e.g. "Box.counter"). Index by:
			//   1. dottedSymbols["Box.counter"] — `this.counter` and
			//      `ClassName.counter` lookups in handleFieldAccess.
			//   2. bareSymbols["counter"] (leaf only) — bare field
			//      references in method bodies: `return counter;`,
			//      `counter++`. Only when no method/class entry already
			//      occupies the leaf slot (same don't-displace policy as
			//      operations). If a getter shares the leaf name the bare
			//      identifier is more likely a call; the dotted lookup in
			//      handleFieldAccess still handles `this.counter`.
			if _, exists := dottedSymbols[e.Name]; !exists {
				dottedSymbols[e.Name] = javaSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
			leaf := e.Name
			if dot := strings.LastIndexByte(e.Name, '.'); dot >= 0 {
				leaf = e.Name[dot+1:]
			}
			if _, exists := bareSymbols[leaf]; !exists {
				bareSymbols[leaf] = javaSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		case e.Kind == "SCOPE.Component" &&
			(e.Subtype == "class" || e.Subtype == "interface" || e.Subtype == "enum"):
			if _, exists := bareSymbols[e.Name]; !exists {
				bareSymbols[e.Name] = javaSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		}
		// NOTE: Java import placeholders (Kind=SCOPE.Component, Subtype="")
		// are intentionally NOT indexed into the file-scope symbol table.
		// Unlike the Python extractor (which emits an entity per imported
		// name with Name="module.leaf"), the Java buildImport emits a
		// single entity per import statement keyed by the top package
		// segment ("com" for `import com.foo.Bar`). A REFERENCES edge
		// targeting that top segment would never bind via lookupStructural
		// because the same-file lookup index keys on the dotted local
		// name. Cross-file binding for imported names is the
		// chain-fix-1 work item (mirror of Python's IMPORTS-driven
		// REFERENCES rewrite) — see docs/verify2/per-repo-residual-
		// ledger.md.
	}
	if len(bareSymbols) == 0 && len(dottedSymbols) == 0 {
		return
	}

	// Phase 2 — walk every method/constructor body, tracking the
	// enclosing operation entity (by its emitted Name) and its
	// enclosing class (so `this.<field>` and `ClassName.member`
	// resolve to "<Class>.<member>").
	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]bool)

	emit := func(fstack []javaFrame, sym javaSymbol) {
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
		toID := buildJavaReferenceTargetID(file.Path, sym)
		(*entities)[idx].Relationships = append((*entities)[idx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "REFERENCES",
			})
	}

	// emitCrossFileFieldHint emits a REFERENCES stub for a `this.<attr>`
	// access where the field is NOT in the current file's symbol table —
	// i.e. it may be inherited from a parent class declared in another
	// file (issue #667). The stub targets the field via the enclosing
	// class name so the resolver can follow EXTENDS edges:
	//
	//   scope:schema:ref:java:<file>:<EnclosingClass>.<attr>
	//
	// The resolver's lookupStructural → lookupLocationKind path will
	// first try the same file and miss; the new Java cross-file field
	// fallback added to lookupStructural then probes byPackageMember and
	// the global schema index to find the field in a parent class file.
	emitCrossFileFieldHint := func(fstack []javaFrame, enclosingClass, attr string) {
		if len(fstack) == 0 {
			return
		}
		top := fstack[len(fstack)-1]
		if top.funcEmittedName == "" || enclosingClass == "" || attr == "" {
			return
		}
		dottedName := enclosingClass + "." + attr
		key := edgeKey{top.funcEmittedName, dottedName}
		if seen[key] {
			return
		}
		seen[key] = true
		idx, ok := findEntityIndex(*entities, top.funcEmittedName, file.Path)
		if !ok {
			return
		}
		toID := buildJavaReferenceTargetID(file.Path, javaSymbol{
			kind:    "SCOPE.Schema",
			subtype: "field",
			name:    dottedName,
		})
		(*entities)[idx].Relationships = append((*entities)[idx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "REFERENCES",
			})
	}

	var walk func(n ts.Node, parentClass string, fstack []javaFrame)
	walk = func(n ts.Node, parentClass string, fstack []javaFrame) {
		if n == nil {
			return
		}
		nt := n.Type()

		// Class / interface / enum bodies update parentClass.
		switch nt {
		case "class_declaration", "interface_declaration", "enum_declaration":
			nameNode := n.ChildByFieldName("name")
			cls := ""
			if nameNode != nil {
				cls = nodeText(nameNode, file.Content)
			}
			body := n.ChildByFieldName("body")
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					child := body.Child(i)
					if child != nil && child.Type() == "enum_body_declarations" {
						for j := 0; j < int(child.ChildCount()); j++ {
							walk(child.Child(j), cls, fstack)
						}
						continue
					}
					walk(child, cls, fstack)
				}
			}
			return

		case "method_declaration", "constructor_declaration":
			nameNode := n.ChildByFieldName("name")
			leaf := ""
			emitted := ""
			if nameNode != nil {
				leaf = nodeText(nameNode, file.Content)
				emitted = leaf
				if parentClass != "" {
					emitted = parentClass + "." + leaf
				}
			}
			body := n.ChildByFieldName("body")
			if body == nil {
				return
			}
			newFrame := javaFrame{funcEmittedName: emitted, funcLeafName: leaf, parentClass: parentClass}
			newStack := fstack
			if emitted != "" {
				newStack = append(fstack, newFrame)
			}
			// Walk only the body — declarations (parameters, throws,
			// return type) are NOT references in the senses we want.
			// The body's own descendants WILL hit type_identifier nodes
			// (local var decls, casts, instanceof checks) which we DO
			// want to bind.
			for i := 0; i < int(body.ChildCount()); i++ {
				walk(body.Child(i), parentClass, newStack)
			}
			return
		}

		// Identifier shapes — only fire inside an operation body.
		if len(fstack) > 0 {
			switch nt {
			case "identifier":
				handleIdentifier(n, file, fstack, bareSymbols, emit)
			case "type_identifier":
				handleTypeIdentifier(n, file, fstack, bareSymbols, emit)
			case "field_access":
				handleFieldAccess(n, file, fstack, bareSymbols, dottedSymbols, emit, emitCrossFileFieldHint)
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

// findEntityIndex returns the index of the file-local entity whose Name
// matches the supplied emittedName. Linear scan — acceptable because
// the per-file entity count is in the dozens.
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
//   - skip if this is the callee of a method_invocation (CALLS owns it)
//   - skip Java reserved keywords / pseudo-names
//   - skip self-name (an identifier matching the enclosing operation's leaf)
//   - otherwise look up in bareSymbols and emit
func handleIdentifier(
	n ts.Node,
	file extractor.FileInput,
	fstack []javaFrame,
	bareSymbols map[string]javaSymbol,
	emit func([]javaFrame, javaSymbol),
) {
	name := nodeText(n, file.Content)
	if name == "" {
		return
	}
	if _, isReserved := javaReservedNames[name]; isReserved {
		return
	}
	if isJavaDeclarationPosition(n) {
		return
	}
	if isJavaCallCallee(n) {
		return
	}
	if isJavaFieldAccessField(n) {
		// `obj.<this>` — owned by handleFieldAccess.
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

// handleTypeIdentifier handles `type_identifier` nodes — references to
// a class/interface/enum used in a cast (`(Foo)x`), instanceof
// (`x instanceof Foo`), generic parameter (`List<Foo>`), local variable
// declaration (`Foo x = ...`), or return-type position. These are
// strictly REFERENCES, never CALLS.
func handleTypeIdentifier(
	n ts.Node,
	file extractor.FileInput,
	fstack []javaFrame,
	bareSymbols map[string]javaSymbol,
	emit func([]javaFrame, javaSymbol),
) {
	name := nodeText(n, file.Content)
	if name == "" {
		return
	}
	if _, isReserved := javaReservedNames[name]; isReserved {
		return
	}
	sym, ok := bareSymbols[name]
	if !ok {
		return
	}
	// Only bind to class/interface/enum entries; method/field bareSymbols
	// keyed by the same leaf are not type references.
	if sym.kind != "SCOPE.Component" {
		return
	}
	top := fstack[len(fstack)-1]
	if name == top.funcLeafName {
		return
	}
	emit(fstack, sym)
}

// handleFieldAccess handles `obj.attr` nodes:
//   - if obj is `this` and dottedSymbols has `<ParentClass>.<attr>`,
//     emit REFERENCES to that method entity.
//   - if obj is `this` and bareSymbols has `<attr>` as a field, emit
//     REFERENCES to the field entity.
//   - if obj is a PascalCase identifier (ClassName.staticMember):
//   - look up `<ClassName>.<attr>` in dottedSymbols (method).
//   - fall back to bareSymbols[<attr>] for field/method bare match.
//   - otherwise leave the receiver to the generic identifier walk
//     (recursion handles it).
func handleFieldAccess(
	n ts.Node,
	file extractor.FileInput,
	fstack []javaFrame,
	bareSymbols map[string]javaSymbol,
	dottedSymbols map[string]javaSymbol,
	emit func([]javaFrame, javaSymbol),
	emitHint func(fstack []javaFrame, enclosingClass, attr string),
) {
	// Skip when this field_access IS the `object` child of a
	// method_invocation — CALLS owns that edge through its own resolver.
	if parent := n.Parent(); parent != nil && parent.Type() == "method_invocation" {
		if obj := parent.ChildByFieldName("object"); obj == n {
			// Receiver position of a call — the call's own resolver
			// builds the dotted target. Do NOT double-emit here, but
			// DO let the recursion descend so a static-context receiver
			// (`ClassName.staticField.method()`) still produces a
			// REFERENCES edge to `ClassName`.
			return
		}
	}
	objChild := n.ChildByFieldName("object")
	fieldChild := n.ChildByFieldName("field")
	if objChild == nil || fieldChild == nil {
		return
	}
	attr := nodeText(fieldChild, file.Content)
	if attr == "" {
		return
	}
	top := fstack[len(fstack)-1]

	switch objChild.Type() {
	case "this":
		// `this.<attr>` shape — look up `<ParentClass>.<attr>` first
		// (method), fall back to bare attr (field).
		if top.parentClass != "" {
			dotted := top.parentClass + "." + attr
			if sym, ok := dottedSymbols[dotted]; ok {
				if dotted == top.funcEmittedName {
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
			return
		}
		// Issue #667 — `this.<attr>` misses the local symbol table;
		// the field may be inherited from a parent class in another
		// file. Emit a cross-file hint stub so the resolver can
		// follow EXTENDS edges to find it.
		if emitHint != nil && top.parentClass != "" {
			emitHint(fstack, top.parentClass, attr)
		}
	case "identifier":
		recv := nodeText(objChild, file.Content)
		if recv == "" {
			return
		}
		if _, isReserved := javaReservedNames[recv]; isReserved {
			return
		}
		// PascalCase receiver → likely a ClassName. Try the dotted
		// lookup `ClassName.<attr>` against the symbol table.
		if isPascalCase(recv) {
			dotted := recv + "." + attr
			if sym, ok := dottedSymbols[dotted]; ok {
				if dotted == top.funcEmittedName {
					return
				}
				emit(fstack, sym)
				return
			}
		}
		// Otherwise no binding from the dotted form; recursion will
		// handle the bare identifier separately.
	}
}

// isJavaDeclarationPosition reports whether the identifier node sits in
// a position that DECLARES the name rather than USES it.
//
// Recognised shapes (all return true):
//
//	parent's field `name` is this node — method_declaration,
//	  constructor_declaration, class_declaration, interface_declaration,
//	  enum_declaration, formal_parameter, variable_declarator, etc.
//	parent is `variable_declarator` (LHS of a local var decl)
//	parent is `catch_formal_parameter`
//	parent is `import_declaration` / `package_declaration`
//	parent is `inferred_parameters` (lambda parameter list)
func isJavaDeclarationPosition(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if nameField := parent.ChildByFieldName("name"); nameField != nil && nameField == n {
		return true
	}
	switch parent.Type() {
	case "variable_declarator", "formal_parameter", "spread_parameter",
		"catch_formal_parameter", "inferred_parameters", "type_parameter",
		"package_declaration", "import_declaration",
		"scoped_identifier", "scoped_type_identifier", "scoped_absolute_identifier":
		// scoped_identifier children are dotted segments of package/import
		// paths — never user-code references.
		return true
	}
	return false
}

// isJavaCallCallee reports whether the identifier node is the `name`
// child of a `method_invocation` node, or the type child of an
// `object_creation_expression`. CALLS owns those edges — REFERENCES
// would double-count.
func isJavaCallCallee(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	switch parent.Type() {
	case "method_invocation":
		return parent.ChildByFieldName("name") == n
	}
	return false
}

// isJavaFieldAccessField reports whether the identifier node is the
// `field` child of a `field_access` node. The field_access handler
// owns the binding decision; the bare identifier walk should skip it
// to avoid double-emission.
func isJavaFieldAccessField(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if parent.Type() != "field_access" {
		return false
	}
	return parent.ChildByFieldName("field") == n
}

// buildJavaReferenceTargetID emits a Format A structural-ref for the
// resolver's lookupStructural → lookupLocationKind path. Operation-
// kinded targets emit `scope:operation:ref:...`, Schema-kinded fields
// emit `scope:schema:ref:...`, everything else emits
// `scope:component:ref:...`. Must stay aligned with
// structuralKindFamilies in internal/resolve/refs.go.
func buildJavaReferenceTargetID(filePath string, sym javaSymbol) string {
	scopeSeg := "component"
	switch sym.kind {
	case "SCOPE.Operation":
		scopeSeg = "operation"
	case "SCOPE.Schema":
		scopeSeg = "schema"
	}
	return "scope:" + scopeSeg + ":ref:java:" + filepath.ToSlash(filePath) + ":" + sym.name
}
