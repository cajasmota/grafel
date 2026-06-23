// references.go — REFERENCES-edge emission for the Go extractor.
//
// Analog of #641 (JS/TS), #650 (Python) and #670 (Java) for Go. Prior to
// this pass the Go extractor emitted 0 REFERENCES edges per
// function/method body — every same-scope identifier use, every
// `r.<field>` reference on a receiver method, every `T.M` method
// expression, every type-assertion and composite-literal type
// reference, and every imported-name reference outside of a CALLS
// context produced no edge. The audit (REFERENCES/fn ≈ 0.00 on every
// Go corpus: chi, gin, mini-redis, fixture-d Go subset) drove the
// orphan rate up across the entire Go family.
//
// This pass mirrors the JS/TS / Python / Java emitReferences:
//
//   1. Build a file-scope symbol table from the entities already emitted
//      by the primary extractor pass. The table maps Name → entity-kind
//      metadata so we can build the right structural-ref Format A
//      target ID for each reference.
//
//   2. Walk every function_declaration / method_declaration body for
//      identifier-shaped nodes that are NOT in declaration position and
//      are NOT the function child of a `call_expression` (those are
//      owned by CALLS). For each, look up the identifier in the symbol
//      table and emit a REFERENCES edge from the enclosing operation
//      entity.
//
//   3. Handle Go-specific shapes:
//        - Receiver-method `r.<field>` (`(r *Receiver) M() { r.field }`)
//          → look up `<Receiver>.<field>` against the symbol table
//          (struct fields stamped by collectStructFieldTypes).
//        - Package-level vars and consts referenced inside functions —
//          today the Go extractor doesn't emit entities for var/const
//          declarations, so these bind only when a sibling Type /
//          Function entity shares the name (rare in practice).
//        - Imported-name references (`time.Now`, `fmt.Println`) — the
//          selector_expression's bare operand (`time`) resolves to the
//          IMPORTS placeholder via the symbol table's bareName index.
//          Note: `time.Now()` as a CALL is owned by CALLS; this pass
//          fires for non-call shapes (`var f = time.Now`, `if errors.Is(...)`).
//        - Type assertions `x.(*SomeType)` — the type child is a
//          pointer_type / type_identifier; the inner identifier
//          resolves against the bareSymbols table.
//        - Composite literals `MyType{Field: value}` — the literal's
//          type child is a type_identifier; binds against bareSymbols.
//          The field name (`Field`) resolves via the dotted lookup
//          `MyType.Field` against the symbol table.
//        - Interface satisfaction checks `var _ MyInterface = &Impl{}`
//          — handled by the variable-declaration walk; the type
//          identifier (MyInterface) and composite-literal type (Impl)
//          both bind.
//        - Method expressions `T.M` — the selector_expression's
//          operand is a type_identifier; binds to T via bareSymbols,
//          and the selector field is resolved as `T.M` via
//          dottedSymbols.
//        - Type switches `switch x := y.(type) { case *T: ... }` — the
//          case clause's type list contains type_identifier nodes that
//          the walk binds normally.
//        - Iota in const blocks — `iota` is a Go builtin; filtered.
//        - Blank identifier `_` — never references a project entity;
//          filtered.
//
//   4. Skip Go builtins (`len`, `cap`, `make`, `new`, `append`, `copy`,
//      `delete`, `panic`, `recover`, `print`, `println`, plus the
//      predeclared types `int`, `string`, `bool`, `error`, etc.) and
//      reserved words. A user-declared name that shadows a builtin is
//      impossible at top level (the parser rejects it for type names),
//      so this list is purely a noise filter.
//
// Cap: one REFERENCES edge per (from_id, to_id) pair to prevent N-uses
// inflation. Self-references (a function body referencing its own
// emitted name) are filtered. CALLS edges remain the existing pathway —
// REFERENCES is strictly additive and only fires for non-call
// identifier shapes.
//
// The Format A structural-ref shape this emits is:
//
//	scope:operation:ref:go:<file>:<name>            — function/method targets
//	scope:component:ref:go:<file>:<name>            — struct/interface/import targets
//	scope:schema:ref:go:<file>:<name>               — type-alias targets
//
// The resolver's structuralKindFamilies covers operation and component
// scope segments; the existing lookupStructural → lookupLocationKind
// path binds these edges to their declaration without any new
// dispatcher work and without any reliance on bare-name hint families.

package golang

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// goReservedNames is the conservative allowlist of Go builtins,
// predeclared types, reserved keywords, and language-level pseudo-names
// that should NEVER produce a REFERENCES edge to a project entity.
//
// The list is intentionally short — anything that's almost-always a
// built-in / keyword, almost-never a user-declared name. The
// symbol-table guard runs first, so even if a user shadows a builtin
// (rare) the lookup miss is the only consequence (no over-emission).
var goReservedNames = map[string]struct{}{
	// Builtin functions
	"append": {}, "cap": {}, "close": {}, "complex": {}, "copy": {},
	"delete": {}, "imag": {}, "len": {}, "make": {}, "new": {},
	"panic": {}, "print": {}, "println": {}, "real": {}, "recover": {},
	"min": {}, "max": {}, "clear": {},
	// Predeclared types
	"bool": {}, "byte": {}, "complex64": {}, "complex128": {},
	"error": {}, "float32": {}, "float64": {}, "int": {}, "int8": {},
	"int16": {}, "int32": {}, "int64": {}, "rune": {}, "string": {},
	"uint": {}, "uint8": {}, "uint16": {}, "uint32": {}, "uint64": {},
	"uintptr": {}, "any": {}, "comparable": {},
	// Predeclared values / iota / blank
	"true": {}, "false": {}, "nil": {}, "iota": {}, "_": {},
	// Reserved keywords (most are not identifier-shaped at parse but
	// kept defensively in case grammar drift exposes them).
	"break": {}, "case": {}, "chan": {}, "const": {}, "continue": {},
	"default": {}, "defer": {}, "else": {}, "fallthrough": {}, "for": {},
	"func": {}, "go": {}, "goto": {}, "if": {}, "import": {},
	"interface": {}, "map": {}, "package": {}, "range": {}, "return": {},
	"select": {}, "struct": {}, "switch": {}, "type": {}, "var": {},
}

// goSymbol is a single file-scope symbol-table entry, used to build the
// right structural-ref Format A target for each reference.
type goSymbol struct {
	kind    string
	subtype string
	name    string // emitted entity Name (may be "Receiver.method" for methods)
}

// goFrame tracks the enclosing operation while walking a file's CST
// during reference emission.
type goFrame struct {
	funcEmittedName string // "" outside a function/method; "Receiver.method" inside a method
	funcLeafName    string // bare leaf name of the enclosing operation
	receiverType    string // "" outside a method; receiver type otherwise
	receiverVar     string // bound name of the receiver parameter (e.g. "r" for `(r *Foo)`)
	// varTypes is the per-function local-variable type table built by
	// buildFunctionVarTypes in type_table.go (#1840). Maps a bound name
	// (parameter, receiver, short-var-decl LHS, var-spec name) to its
	// canonical type literal. handleGoSelector consults this when the
	// selector's operand is a bare identifier that isn't the receiver
	// var and isn't a PascalCase type/import — i.e. the dominant
	// `<localVar>.<field>` shape that drove same_package_unqualified
	// on the grafel corpus.
	varTypes goVarTypes
}

// emitReferences is the second-pass entry point invoked from Extract
// AFTER the primary walk + extractImportEntities + extractTypes have
// populated entities. It builds a file-scope symbol table from emitted
// entities, walks every function/method body, and appends REFERENCES
// edges to the enclosing operation entity.
//
// Mutates entities in place. Safe to call with an empty slice — no-op.
func emitReferences(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord, structFields map[string]map[string]string) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	// Phase 1 — build the file-scope symbol table.
	//
	// Index by THE NAME AS REFERENCED IN SOURCE. For methods emitted
	// with Name="Receiver.method" (#66) we index both the leaf name
	// (implicit-this shape: `M()` inside another method of the same
	// receiver) and the dotted form (`Foo.M` method-expression or
	// `r.M` receiver-attribute when receiver type is Foo).
	//
	// Imports: the Go extractor emits ToID = importPath and Name =
	// importPath. We index under the LOCAL package name (last segment
	// of the import path, OR the alias if one was set). The aliased
	// metadata key holds the alias.
	bareSymbols := make(map[string]goSymbol)   // "foo" → fn/type/import
	dottedSymbols := make(map[string]goSymbol) // "Receiver.foo" → method / struct field
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		if e.Subtype == "file" {
			continue
		}
		switch {
		case e.Kind == "SCOPE.Operation":
			// Methods: Name = "Receiver.method"; index by leaf AND dotted.
			leaf := e.Name
			if dot := strings.LastIndexByte(e.Name, '.'); dot >= 0 {
				leaf = e.Name[dot+1:]
				if _, exists := dottedSymbols[e.Name]; !exists {
					dottedSymbols[e.Name] = goSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
				}
			}
			if _, exists := bareSymbols[leaf]; !exists {
				bareSymbols[leaf] = goSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		case e.Kind == "SCOPE.Component" &&
			(e.Subtype == "struct" || e.Subtype == "interface"):
			if _, exists := bareSymbols[e.Name]; !exists {
				bareSymbols[e.Name] = goSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		case e.Kind == "SCOPE.Schema" && e.Subtype == "type_alias":
			if _, exists := bareSymbols[e.Name]; !exists {
				bareSymbols[e.Name] = goSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		case e.Kind == "SCOPE.Component" && e.Subtype == "":
			// Import placeholder. Name is the full importPath; index
			// under the LOCAL package identifier so `pkg.Foo` in source
			// resolves. Local name = alias when present, else last
			// path segment.
			local := ""
			if e.Metadata != nil {
				if a, ok := e.Metadata["alias"].(string); ok && a != "" {
					local = a
				}
			}
			if local == "" {
				if slash := strings.LastIndexByte(e.Name, '/'); slash >= 0 {
					local = e.Name[slash+1:]
				} else {
					local = e.Name
				}
			}
			// Strip any version segment from local name (`v5` for
			// `github.com/go-chi/chi/v5`). If the last segment looks
			// like `vN`, fall back to the segment before it.
			if isVersionSegment(local) {
				if slash := strings.LastIndexByte(e.Name, '/'); slash >= 0 {
					trimmed := e.Name[:slash]
					if slash2 := strings.LastIndexByte(trimmed, '/'); slash2 >= 0 {
						local = trimmed[slash2+1:]
					} else {
						local = trimmed
					}
				}
			}
			if local == "" {
				continue
			}
			if _, exists := bareSymbols[local]; !exists {
				bareSymbols[local] = goSymbol{kind: e.Kind, subtype: e.Subtype, name: e.Name}
			}
		}
	}

	// Struct fields are NOT emitted as standalone entities in the Go
	// extractor today; they live only as DEPENDS_ON edges from the
	// owning Component. To support `r.<field>` references on receiver
	// methods, we synthesise dotted lookups against the structFields
	// map collected in collectStructFieldTypes. We bind to the
	// owning struct's Component entity (kind=SCOPE.Component) so the
	// REFERENCES edge points to the struct (the field lives on it).
	for structName, fields := range structFields {
		sym, hasStruct := bareSymbols[structName]
		if !hasStruct {
			continue
		}
		for fieldName := range fields {
			dotted := structName + "." + fieldName
			if _, exists := dottedSymbols[dotted]; !exists {
				// Bind to the struct entity itself (the field is part of it).
				dottedSymbols[dotted] = sym
			}
		}
	}

	if len(bareSymbols) == 0 && len(dottedSymbols) == 0 {
		return
	}

	// Phase 2 — walk every function/method body, tracking the
	// enclosing operation (by its emitted Name) and the method's
	// receiver type + variable (so `r.<field>` resolves to
	// `<Receiver>.<field>`).
	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]bool)

	// #4683 — file-local constructor return-type table, shared by every
	// per-function var-type table so `x := NewFoo()` types `x` as Foo for the
	// REFERENCES selector pass too (parity with the CALLS pass).
	ctorReturns := collectFileConstructorReturns(
		findAll(root, "function_declaration", "method_declaration"), file.Content)

	// emit appends a REFERENCES edge from the top-of-stack function to
	// the resolved symbol. When viaReceiverType is non-empty, it's
	// stamped on the edge's Properties as a diagnostic so #1840's
	// "resolved via local-var type chain" cases are visible in
	// audits (e.g. you can count how many edges the v1 lightweight
	// scope rescued from same_package_unqualified). Empty
	// viaReceiverType produces a property-less edge for parity with
	// pre-#1840 receiver / PascalCase paths.
	emit := func(fstack []goFrame, sym goSymbol, viaReceiverType string) {
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
		idx, ok := findGoEntityIndex(*entities, top.funcEmittedName, file.Path)
		if !ok {
			return
		}
		toID := buildGoReferenceTargetID(file.Path, sym)
		rec := types.RelationshipRecord{
			ToID: toID,
			Kind: "REFERENCES",
		}
		if viaReceiverType != "" {
			rec.Properties = map[string]string{"via_receiver_type": viaReceiverType}
		}
		(*entities)[idx].Relationships = append((*entities)[idx].Relationships, rec)
	}

	var walk func(n ts.Node, fstack []goFrame)
	walk = func(n ts.Node, fstack []goFrame) {
		if n == nil {
			return
		}
		nt := n.Type()

		switch nt {
		case "function_declaration", "method_declaration":
			nameNode := n.ChildByFieldName("name")
			leaf := ""
			emitted := ""
			recvType := ""
			recvVar := ""
			if nameNode != nil {
				leaf = nodeText(nameNode, file.Content)
				emitted = leaf
			}
			if nt == "method_declaration" {
				recvNode := n.ChildByFieldName("receiver")
				recvType = receiverTypeName(recvNode, file.Content)
				recvVar = receiverParamName(recvNode, file.Content)
				if recvType != "" && emitted != "" {
					emitted = recvType + "." + leaf
				}
			}
			body := n.ChildByFieldName("body")
			if body == nil {
				return
			}
			// #1840 — build the per-function var-type table so the
			// selector handler can resolve `<localVar>.<field>`. Cost
			// is one extra pass over the body's
			// short_var_declaration / var_declaration nodes plus a
			// scan of the parameter_list; both already done by the
			// CALLS pass, so the absolute walk cost is small. When
			// the function has no params and no typeable locals,
			// buildFunctionVarTypes returns nil and the selector
			// handler short-circuits via lookupVarType's nil check.
			newFrame := goFrame{
				funcEmittedName: emitted,
				funcLeafName:    leaf,
				receiverType:    recvType,
				receiverVar:     recvVar,
				varTypes:        buildFunctionVarTypes(n, file.Content, ctorReturns),
			}
			newStack := fstack
			if emitted != "" {
				newStack = append(fstack, newFrame)
			}
			// Walk only the body — parameter list / result list are
			// declarations, not user references.
			count := int(body.ChildCount())
			for i := 0; i < count; i++ {
				walk(body.Child(i), newStack)
			}
			return
		}

		if len(fstack) > 0 {
			switch nt {
			case "identifier", "type_identifier":
				handleGoIdentifier(n, file, fstack, bareSymbols, emit)
			case "selector_expression":
				handleGoSelector(n, file, fstack, bareSymbols, dottedSymbols, emit)
			}
		}

		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			walk(n.Child(i), fstack)
		}
	}

	walk(root, nil)
}

// findGoEntityIndex returns the index of the file-local entity whose
// Name matches the supplied emittedName. Linear scan — acceptable
// because the per-file entity count is in the dozens.
func findGoEntityIndex(entities []types.EntityRecord, emittedName, filePath string) (int, bool) {
	for i := range entities {
		if entities[i].SourceFile == filePath && entities[i].Name == emittedName {
			return i, true
		}
	}
	return -1, false
}

// handleGoIdentifier handles a bare `identifier` / `type_identifier`
// node:
//   - skip if in declaration position (parent's `name` field is this
//     node, or parent is a parameter / var_spec declarator).
//   - skip if this is the function child of a `call_expression` —
//     CALLS owns it.
//   - skip if this is the `operand` child of a selector_expression —
//     the selector handler decides the right binding.
//   - skip Go reserved keywords / builtins / predeclared types.
//   - skip self-name (matches enclosing operation's leaf).
//   - skip the receiver variable (`r` in `(r *Foo)`).
//   - otherwise look up in bareSymbols and emit.
func handleGoIdentifier(
	n ts.Node,
	file extractor.FileInput,
	fstack []goFrame,
	bareSymbols map[string]goSymbol,
	emit func([]goFrame, goSymbol, string),
) {
	name := nodeText(n, file.Content)
	if name == "" {
		return
	}
	if _, isReserved := goReservedNames[name]; isReserved {
		return
	}
	if isGoDeclarationPosition(n) {
		return
	}
	if isGoCallCallee(n) {
		return
	}
	if isGoSelectorOperand(n) {
		// Owned by the selector handler.
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
	if name == top.receiverVar && top.receiverVar != "" {
		return
	}
	emit(fstack, sym, "")
}

// handleGoSelector handles `obj.attr` (selector_expression) nodes:
//   - if obj is the receiver variable (`r` in a method on `Foo`),
//     attempt `Foo.attr` lookup in dottedSymbols (method or field).
//   - if obj is a PascalCase identifier (`T.M` method expression /
//     `T{Field: ...}` composite literal field), attempt `T.attr` in
//     dottedSymbols.
//   - the operand identifier itself binds to bareSymbols (resolves
//     import-package references like `time.Now`, type references
//     like `T.M`, etc.) UNLESS the selector is the function child of
//     a call_expression — in which case CALLS owns it.
func handleGoSelector(
	n ts.Node,
	file extractor.FileInput,
	fstack []goFrame,
	bareSymbols map[string]goSymbol,
	dottedSymbols map[string]goSymbol,
	emit func([]goFrame, goSymbol, string),
) {
	// Skip if this selector IS the function child of a call_expression.
	// The operand identifier is still picked up via the recursion (it's
	// no longer a selector-operand because we returned early here) —
	// except we DO want to bind the operand for `pkg.Fn()` calls (the
	// `pkg` import reference). To keep parity with CALLS we let the
	// recursion handle the operand identifier; isGoSelectorOperand only
	// returns true when the parent IS this selector. So when this
	// selector is the call's function, we return early, recursion
	// descends into the operand identifier, isGoSelectorOperand returns
	// true (parent is selector_expression), and the bare handler skips.
	// To still capture the `pkg` reference we explicitly emit here.
	operand := n.ChildByFieldName("operand")
	field := n.ChildByFieldName("field")
	if operand == nil {
		return
	}
	isCallee := false
	if parent := n.Parent(); parent != nil && parent.Type() == "call_expression" {
		if fn := parent.ChildByFieldName("function"); ts.SameNode(fn, n) {
			isCallee = true
		}
	}

	top := fstack[len(fstack)-1]

	// Operand-side binding: when operand is a bare identifier matching
	// the file-scope symbol table (an import package, a type for
	// method-expression, an enclosing struct for `T{...}.M`), emit a
	// REFERENCES edge for it. This catches `time.Now` (time → import),
	// `Foo.M` (Foo → struct/interface), etc.
	if operand.Type() == "identifier" {
		opName := nodeText(operand, file.Content)
		if opName != "" {
			if _, isReserved := goReservedNames[opName]; !isReserved {
				if opName != top.funcLeafName && opName != top.receiverVar {
					if sym, ok := bareSymbols[opName]; ok {
						emit(fstack, sym, "")
					}
				}
			}
		}
	}

	// Field-side: when operand is the receiver variable, try
	// `<ReceiverType>.<field>` in dottedSymbols.
	if field != nil && operand.Type() == "identifier" && !isCallee {
		opName := nodeText(operand, file.Content)
		fieldName := nodeText(field, file.Content)
		if fieldName != "" && opName != "" {
			// Receiver-method `r.<field>` shape.
			emittedViaPath := false
			if top.receiverVar != "" && opName == top.receiverVar && top.receiverType != "" {
				dotted := top.receiverType + "." + fieldName
				if sym, ok := dottedSymbols[dotted]; ok {
					if dotted != top.funcEmittedName {
						emit(fstack, sym, "")
						emittedViaPath = true
					}
				}
			}
			// PascalCase receiver — `T.M` method expression or
			// `T{Field: value}` composite literal field.
			if !emittedViaPath && isPascalCase(opName) {
				dotted := opName + "." + fieldName
				if sym, ok := dottedSymbols[dotted]; ok {
					if dotted != top.funcEmittedName {
						emit(fstack, sym, "")
						emittedViaPath = true
					}
				}
			}
			// #1840 — local-var type chain. When the operand is a
			// bare identifier that didn't match the receiver-var
			// path (above) and isn't a PascalCase type/import, try
			// the per-function var-type table. This rescues the
			// dominant same_package_unqualified bucket: parameters
			// typed as a project struct, `:=`-declared composite
			// literals, and `var x T` declarations where T is a
			// same-file struct.
			//
			// Why guard on emittedViaPath: the receiver var is
			// also present in varTypes (see buildFunctionVarTypes)
			// so without the guard we'd double-emit the receiver
			// `r.<field>` edge with two different code paths.
			// PascalCase identifiers can also alias a local var
			// (rare but legal), and again we'd double-emit; first-
			// hit-wins is the right choice for a unique-edge
			// invariant the seen-map already enforces, but the
			// guard is cheaper than the second lookup + seen-check.
			//
			// Fallback semantics: a lookup miss (varTypes returns
			// "" or the dotted lookup misses) leaves the selector
			// unbound — the resolver downstream still routes it to
			// the existing same_package_unqualified bucket. No
			// regression risk vs. pre-#1840 behaviour.
			if !emittedViaPath {
				if opType := top.varTypes.lookupVarType(opName); opType != "" {
					dotted := opType + "." + fieldName
					if sym, ok := dottedSymbols[dotted]; ok {
						if dotted != top.funcEmittedName {
							// Stamp the resolved receiver type so audits
							// can attribute rescued edges to this path.
							emit(fstack, sym, opType)
						}
					}
				}
			}
		}
	}
}

// isGoDeclarationPosition reports whether the identifier sits in a
// position that DECLARES the name rather than USES it.
func isGoDeclarationPosition(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if nameField := parent.ChildByFieldName("name"); nameField != nil && nameField == n {
		return true
	}
	// For var_spec / const_spec / short_var_declaration / field_declaration
	// the `name` field (handled above) is the declared identifier; the
	// `type` and `value` fields contain expression/type references that
	// are NOT declarations.
	pt := parent.Type()
	switch pt {
	case "var_spec", "const_spec", "field_declaration":
		// Multiple `name` children possible (`var a, b T`). Tree-sitter
		// Go grammar surfaces these as un-fielded children before the
		// `type` field. Approximate: a child appearing BEFORE the `type`
		// field is a declared name.
		if typeField := parent.ChildByFieldName("type"); typeField != nil {
			if n.StartByte() < typeField.StartByte() {
				return true
			}
			return false
		}
		// No explicit type — every identifier child is a declared name
		// except those within the `value` (initialiser) subtree.
		if valueField := parent.ChildByFieldName("value"); valueField != nil {
			if n.StartByte() < valueField.StartByte() {
				return true
			}
		}
		return false
	case "short_var_declaration":
		// `x := expr` — LHS identifiers are declarations. The grammar
		// surfaces LHS via the `left` field; identifiers in that
		// subtree are declarations.
		if left := parent.ChildByFieldName("left"); left != nil {
			if n.StartByte() >= left.StartByte() && n.EndByte() <= left.EndByte() {
				return true
			}
		}
		return false
	case "parameter_declaration", "variadic_parameter_declaration",
		"type_parameter_declaration":
		// `f(x, y T)` — the `name` field (handled above) holds the
		// declared parameter names. The `type` field is a USAGE. Any
		// identifier appearing BEFORE the `type` child is a name.
		if typeField := parent.ChildByFieldName("type"); typeField != nil {
			if n.StartByte() < typeField.StartByte() {
				return true
			}
			return false
		}
		return true
	case "type_spec", "type_alias":
		// `type Foo struct{...}` — `name` (handled above) is the
		// declared type. The `type` field is the underlying type,
		// which contains identifier references we want.
		return false
	case "method_spec":
		// Interface method declaration: name is the method, parameters
		// and result are usages. Only the `name` field declares.
		return false
	case "import_spec", "package_clause",
		"label_name", "labeled_statement", "keyed_element":
		// keyed_element is the `Field: value` shape in composite
		// literals — the key identifier is a struct-field name, but
		// the field's owning type is the literal's type; the binding
		// is handled via the dotted lookup in handleGoSelector. The
		// bare-name pass would emit a spurious edge against an
		// unrelated bareSymbols entry sharing the field's name, so we
		// skip here.
		return true
	}
	return false
}

// isGoCallCallee reports whether the identifier node is the `function`
// child of a `call_expression` node. CALLS owns that edge.
func isGoCallCallee(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if parent.Type() != "call_expression" {
		return false
	}
	return ts.SameNode(parent.ChildByFieldName("function"), n)
}

// isGoSelectorOperand reports whether the identifier node is the
// `operand` (or `field`) child of a selector_expression — those are
// owned by handleGoSelector to avoid double-emission.
func isGoSelectorOperand(n ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if parent.Type() != "selector_expression" {
		return false
	}
	if op := parent.ChildByFieldName("operand"); ts.SameNode(op, n) {
		return true
	}
	if f := parent.ChildByFieldName("field"); ts.SameNode(f, n) {
		return true
	}
	return false
}

// buildGoReferenceTargetID emits a Format A structural-ref for the
// resolver's lookupStructural → lookupLocationKind path. Operation-
// kinded targets emit `scope:operation:ref:...`, Component-kinded
// targets emit `scope:component:ref:...`, Schema-kinded targets emit
// `scope:schema:ref:...`. Must stay aligned with
// structuralKindFamilies in internal/resolve/refs.go.
func buildGoReferenceTargetID(filePath string, sym goSymbol) string {
	scopeSeg := "component"
	switch sym.kind {
	case "SCOPE.Operation":
		scopeSeg = "operation"
	case "SCOPE.Schema":
		scopeSeg = "schema"
	}
	return "scope:" + scopeSeg + ":ref:go:" + filepath.ToSlash(filePath) + ":" + sym.name
}

// isPascalCase reports whether s starts with an uppercase ASCII letter.
// Heuristic for distinguishing type names (`Foo`) from value names
// (`foo`) in selector_expression receivers.
func isPascalCase(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// isVersionSegment reports whether s looks like a Go-module version
// directory (`v2`, `v3`, ...). Used to skip the trailing version
// segment when computing an import's local package name.
func isVersionSegment(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
