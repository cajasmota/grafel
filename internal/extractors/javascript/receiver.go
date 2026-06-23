// Package javascript — issue #421 receiver-typing helpers.
//
// receiverTypedTarget converts a member_expression call node into a
// Format A structural-ref CALLS target when the receiver's static type
// is determinable from the enclosing class's typed fields, the caller's
// own typed parameters, and a relative import statement at the file
// scope. Anything that misses one of those checks returns "" so the
// caller falls back to the bare trailing-identifier shape that JS/TS
// extraction has always emitted.
//
// The mirror logic lives in internal/extractors/java/java.go's
// receiverTypeName / extractCallRelationships pair (issue #120). The
// JS/TS side differs from Java in that we resolve the receiver-type
// binding to a CONCRETE FILE PATH (not a Java-style dotted package
// path) — TypeScript imports are file-relative, not namespace-bound,
// so the resolver's byLocation index is the right binding target.
package javascript

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// withLocalReceiverTypes returns a classBindings frame that augments the
// supplied base frame with local-variable receiver types discovered inside
// `body`. It scans every `variable_declarator` reachable from the body for
// the two construction idioms that bind a local to a concrete, imported
// class (issue #4671):
//
//	const c = new ProposalController(mockSvc);   // direct construction
//	const svc = module.get(ProposalService);     // NestJS DI resolution
//	const svc = app.get(ProposalService);        //   (Test.createTestingModule…)
//	const svc = app.resolve(ProposalService);    //   (request-scoped DI)
//
// For each match it records `localName → ClassName` so that a subsequent
// `localName.method()` call types through `receiverTypedTarget` →
// `importByLocal[ClassName]` → a Format A structural ref keyed on the
// class's source file. Unit/controller specs are dominated by this local
// form, so without it their `controller.method()` calls never bind to the
// handler and ComputeCoverage undercounts test coverage ~4x.
//
// Precedence: the base frame (enclosing-class fields + the caller's typed
// parameters) is the CLOSER binding and always wins. A local only fills a
// name the base frame does not already type. When `base` is nil and no
// locals are found, the base frame is returned unchanged (nil) so the hot
// path for non-test, no-local bodies allocates nothing.
//
// The frame is append-only: `base` is never mutated. When at least one
// local is discovered we copy `base` into a fresh frame first.
func (x *extractor) withLocalReceiverTypes(body ts.Node, base *classBindings) *classBindings {
	if body == nil {
		return base
	}
	decls := findAllNodes(body, "variable_declarator")
	if len(decls) == 0 {
		return base
	}
	var locals map[string]string
	for _, d := range decls {
		name, typeName := x.localReceiverBinding(d)
		if name == "" || typeName == "" {
			continue
		}
		// The base frame is the closer binding (a class field or the
		// caller's own typed parameter) — never let a local shadow it.
		if base != nil {
			if _, taken := base.fields[name]; taken {
				continue
			}
		}
		if locals == nil {
			locals = map[string]string{}
		}
		// First declaration wins on a re-`let` collision — a conservative
		// bias matching the rest of the receiver binder (better to miss
		// than to mis-bind to a later reassignment of a different type).
		if _, seen := locals[name]; !seen {
			locals[name] = typeName
		}
	}
	if len(locals) == 0 {
		return base
	}
	frame := &classBindings{fields: map[string]string{}}
	if base != nil {
		frame.className = base.className
		for k, v := range base.fields {
			frame.fields[k] = v
		}
	}
	for k, v := range locals {
		if _, taken := frame.fields[k]; !taken {
			frame.fields[k] = v
		}
	}
	return frame
}

// localReceiverBinding inspects a `variable_declarator` node and returns
// the (localName, className) pair when the declarator binds a single
// identifier to a recognised class-construction idiom. Returns ("", "")
// for any other shape (destructuring LHS, non-construction RHS, dynamic
// class expression, …) so the caller skips it.
//
// Recognised RHS shapes:
//
//   - `new ClassName(...)`            — new_expression, constructor is a
//     bare (type_)identifier.
//   - `module.get(ClassName)` /
//     `app.get(ClassName)` /
//     `<x>.resolve(ClassName)`        — NestJS DI resolution: a member call
//     whose method is get/resolve and whose
//     sole argument is a class identifier.
func (x *extractor) localReceiverBinding(decl ts.Node) (string, string) {
	if decl == nil {
		return "", ""
	}
	nameNode := decl.ChildByFieldName("name")
	if nameNode == nil || nameNode.Type() != "identifier" {
		// Destructuring / array patterns don't name a single receiver.
		return "", ""
	}
	value := decl.ChildByFieldName("value")
	if value == nil {
		return "", ""
	}
	name := x.nodeText(nameNode)
	if name == "" {
		return "", ""
	}
	typeName := x.constructedTypeName(value)
	if typeName == "" {
		return "", ""
	}
	return name, typeName
}

// constructedTypeName returns the class identifier produced by a
// construction expression, or "" when `value` is not a recognised
// construction idiom. Handles `new ClassName(...)` and the NestJS DI
// `<container>.get(ClassName)` / `.resolve(ClassName)` shapes. Awaited DI
// (`await module.resolve(X)`) is unwrapped first.
func (x *extractor) constructedTypeName(value ts.Node) string {
	if value == nil {
		return ""
	}
	switch value.Type() {
	case "await_expression":
		// `const svc = await module.resolve(X)` — unwrap and recurse.
		count := int(value.ChildCount())
		for i := 0; i < count; i++ {
			ch := value.Child(i)
			if ch == nil || ch.Type() == "await" {
				continue
			}
			return x.constructedTypeName(ch)
		}
		return ""
	case "new_expression":
		// constructor field holds the class being instantiated.
		ctor := value.ChildByFieldName("constructor")
		if ctor == nil {
			return ""
		}
		switch ctor.Type() {
		case "identifier", "type_identifier":
			return x.nodeText(ctor)
		case "member_expression":
			// `new ns.ClassName()` — bind to the trailing class name. The
			// receiver binder resolves via importByLocal on the leaf, so a
			// namespaced constructor still types when its leaf was imported.
			prop := ctor.ChildByFieldName("property")
			if prop != nil {
				return x.nodeText(prop)
			}
		}
		return ""
	case "call_expression":
		return x.diResolvedTypeName(value)
	}
	return ""
}

// diResolvedTypeName recognises NestJS-style dependency resolution calls
// and returns the resolved class identifier:
//
//	module.get(ProposalService)        → "ProposalService"
//	app.get(ProposalController)        → "ProposalController"
//	moduleRef.resolve(RequestScopedSvc)→ "RequestScopedSvc"
//
// The call must be a member expression whose method is `get` or `resolve`
// and whose sole argument is a bare class identifier (the DI token form
// `module.get('TOKEN')` / `module.get<T>(SYMBOL)` with a string/symbol
// token is intentionally NOT typed — there is no class identifier to bind).
// Returns "" for any other call shape so non-DI calls (e.g. array.get(0))
// are ignored.
func (x *extractor) diResolvedTypeName(call ts.Node) string {
	if call == nil {
		return ""
	}
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return ""
	}
	prop := fn.ChildByFieldName("property")
	if prop == nil {
		return ""
	}
	switch x.nodeText(prop) {
	case "get", "resolve":
	default:
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	// Find the single positional class-identifier argument. More than one
	// positional argument, or a non-identifier first argument (string DI
	// token, options object), disqualifies the binding.
	var typeArg ts.Node
	posCount := 0
	count := int(args.ChildCount())
	for i := 0; i < count; i++ {
		a := args.Child(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "(", ")", ",", "comment":
			continue
		}
		posCount++
		if posCount > 1 {
			return ""
		}
		typeArg = a
	}
	if typeArg == nil {
		return ""
	}
	switch typeArg.Type() {
	case "identifier", "type_identifier":
		return x.nodeText(typeArg)
	}
	return ""
}

// receiverTypedTarget resolves a member_expression call (`<recv>.<method>`)
// to a structural-ref CALLS target when the receiver types through the
// supplied frame and the type is imported from a relative path. Returns
// "" on any miss.
//
// Supported receiver shapes:
//
//   - bare identifier `<recv>`         — typed via frame.fields[<recv>]
//   - `this.<field>`                   — typed via frame.fields[<field>]
//   - `this.<field>.<sub>` and deeper  — NOT handled (TypeScript chains
//     would require typing each step, which the extractor does not do).
//
// `method` is the trailing property identifier already extracted by the
// caller; we only resolve the receiver here.
func (x *extractor) receiverTypedTarget(memberExpr ts.Node, method string, frame *classBindings) string {
	if memberExpr == nil || method == "" || frame == nil {
		return ""
	}
	obj := memberExpr.ChildByFieldName("object")
	if obj == nil {
		return ""
	}
	recvName := x.receiverIdent(obj)
	if recvName == "" {
		return ""
	}
	typeName := frame.fields[recvName]
	if typeName == "" {
		return ""
	}
	// The receiver's declared type must correspond to a relatively-
	// imported binding so we can derive a concrete file path. External
	// imports (`from "typeorm"`) leave resolvedFile empty; we fall
	// back to bare-name CALLS in that case.
	binding, ok := x.importByLocal[typeName]
	if !ok || binding == nil || binding.resolvedFile == "" {
		return ""
	}
	return extreg.BuildOperationStructuralRef(x.language, binding.resolvedFile, method)
}

// receiverIdent extracts the receiver identifier from a member_expression
// `object` node. Returns the identifier name for:
//
//   - identifier nodes                — bare receiver `recv`
//   - member_expression `this.field`  — the field name
//
// Anything more elaborate (chained property accesses, parenthesised
// expressions, function-call results) returns "" so the caller drops
// to the bare-name fallback. Conservative bias: better miss than
// misresolve.
func (x *extractor) receiverIdent(obj ts.Node) string {
	if obj == nil {
		return ""
	}
	switch obj.Type() {
	case "identifier", "type_identifier", "property_identifier":
		return x.nodeText(obj)
	case "this":
		// Pure `this.method()` is a self-call shape; receiver
		// resolution doesn't apply (the extractor already captures
		// these via the bare-name path with the enclosing class as
		// context). Return "" so the caller emits the bare method.
		return ""
	case "member_expression":
		// Only `this.<field>` is supported. Anything deeper returns
		// "" — typing a chained property access would require knowing
		// the type of an arbitrary expression, which is out of scope.
		inner := obj.ChildByFieldName("object")
		prop := obj.ChildByFieldName("property")
		if inner == nil || prop == nil {
			return ""
		}
		if inner.Type() != "this" {
			return ""
		}
		return x.nodeText(prop)
	}
	return ""
}

// functionParamFrame builds a classBindings frame from a parameters
// node. The frame inherits the enclosing class's field map (when
// non-nil) so a method body sees BOTH `this.<field>` lookups and bare
// param identifiers. Parameter types win on collision — the parameter
// is closer to the call site than a same-named field.
//
// Supported parameter shapes:
//
//   - `name: Type`                            — required typed param
//   - `private/public/readonly name: Type`    — TS parameter property
//     (NestJS @Inject style;
//     this is the dominant
//     shape issue #421 cares
//     about — the extractor
//     imports both the field
//     and the param into the
//     same lookup scope).
//   - `name?: Type`                           — optional param
//
// Untyped parameters (`function f(x)`) and parameter destructuring
// shapes are silently skipped.
func (x *extractor) functionParamFrame(params ts.Node, base *classBindings) *classBindings {
	if params == nil && base == nil {
		return nil
	}
	frame := &classBindings{fields: map[string]string{}}
	if base != nil {
		frame.className = base.className
		for k, v := range base.fields {
			frame.fields[k] = v
		}
	}
	if params == nil {
		return frame
	}
	count := int(params.ChildCount())
	for i := 0; i < count; i++ {
		p := params.Child(i)
		if p == nil {
			continue
		}
		name, typ := x.paramNameAndType(p)
		if name == "" || typ == "" {
			continue
		}
		frame.fields[name] = typ // params win over base fields
	}
	return frame
}

// collectClassFields walks the immediate children of a class body and
// fills out with field-name → declared-type-leaf for every typed
// property declaration AND every constructor parameter property
// (TypeScript's `constructor(private foo: Foo)` shape).
//
// The TypeScript grammar emits these node types:
//
//   - public_field_definition  — typed class fields
//   - method_definition with name="constructor" — constructor params
//     carrying access
//     modifiers become
//     parameter properties.
//
// Anything else (untyped fields, methods, getters/setters) is skipped.
func (x *extractor) collectClassFields(body ts.Node, out map[string]string) {
	if body == nil {
		return
	}
	count := int(body.ChildCount())
	for i := 0; i < count; i++ {
		ch := body.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "public_field_definition", "field_definition":
			// Issue #771 — JS grammar uses "field_definition";
			// TS grammar uses "public_field_definition". The name
			// field also differs: TS uses "name", JS uses "property".
			name := x.childFieldText(ch, "name")
			if name == "" {
				name = x.childFieldText(ch, "property")
			}
			typ := x.typeAnnotationLeaf(ch.ChildByFieldName("type"))
			if name != "" && typ != "" {
				out[name] = typ
			}
		case "method_definition":
			nameNode := ch.ChildByFieldName("name")
			if nameNode == nil || x.nodeText(nameNode) != "constructor" {
				continue
			}
			x.collectConstructorParamProperties(ch, out)
		}
	}
}

// collectConstructorParamProperties extracts TypeScript parameter
// properties from a constructor method_definition. Parameter properties
// are formal_parameter children carrying an accessibility_modifier
// (`private`, `public`, `protected`) or a `readonly` modifier; the
// parameter doubles as a class field of the same name and type.
//
// NestJS @Inject() style:
//
//	constructor(
//	  private readonly userService: UserService,
//	  @Inject('CACHE') private cache: CacheService,
//	) {}
//
// Both `userService` and `cache` end up in out as field bindings.
//
// Plain typed parameters without an access modifier are NOT class
// fields in TypeScript — they're local to the constructor body — so
// they are skipped here. They DO show up in the constructor's own
// parameter frame via functionParamFrame, but the receiver binder
// only uses the class field frame for `this.<recv>` lookups.
//
// Issue #421 relaxation: NestJS commonly relies on parameter
// properties for injection, so the rule above (require an access
// modifier) covers the dominant pattern. Bare-typed constructor
// parameters do not become class fields (matching TS semantics).
func (x *extractor) collectConstructorParamProperties(ctor ts.Node, out map[string]string) {
	params := ctor.ChildByFieldName("parameters")
	if params == nil {
		return
	}
	count := int(params.ChildCount())
	for i := 0; i < count; i++ {
		p := params.Child(i)
		if p == nil {
			continue
		}
		// `required_parameter` / `optional_parameter` carry the
		// accessibility modifier as a direct child. The modifier may
		// be absent for plain locals.
		if !x.hasAccessModifier(p) {
			continue
		}
		name, typ := x.paramNameAndType(p)
		if name == "" || typ == "" {
			continue
		}
		out[name] = typ
	}
}

// hasAccessModifier reports whether p (a constructor parameter node)
// carries a `public` / `private` / `protected` / `readonly` modifier,
// which makes it a TypeScript parameter property (a class field
// declared inline). The grammar exposes the modifier as an
// `accessibility_modifier` child; `readonly` is a separate
// `readonly` token.
func (x *extractor) hasAccessModifier(p ts.Node) bool {
	count := int(p.ChildCount())
	for i := 0; i < count; i++ {
		ch := p.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "accessibility_modifier", "readonly":
			return true
		}
	}
	return false
}

// paramNameAndType returns the (name, leaf type) pair of a formal
// parameter node. Handles required_parameter and optional_parameter
// shapes; returns ("", "") when the parameter is untyped or uses a
// destructuring pattern the extractor does not analyse.
func (x *extractor) paramNameAndType(p ts.Node) (string, string) {
	if p == nil {
		return "", ""
	}
	switch p.Type() {
	case "required_parameter", "optional_parameter":
		// pattern → name; type → type annotation
		nameNode := p.ChildByFieldName("pattern")
		typ := x.typeAnnotationLeaf(p.ChildByFieldName("type"))
		if nameNode == nil || typ == "" {
			return "", ""
		}
		if nameNode.Type() != "identifier" {
			// Destructuring shapes — skip.
			return "", ""
		}
		return x.nodeText(nameNode), typ
	}
	return "", ""
}

// typeAnnotationLeaf returns the leaf type identifier of a type
// annotation node. Strips the leading colon, generic parameters, and
// array suffixes:
//
//	`UserService`           → "UserService"
//	`Repository<User>`      → "Repository"
//	`UserService[]`         → "UserService"
//	`Promise<User>`         → "Promise"
//
// Union and intersection types return "" — picking one branch would
// be arbitrary.
func (x *extractor) typeAnnotationLeaf(ann ts.Node) string {
	if ann == nil {
		return ""
	}
	// type_annotation wraps a single type node after the colon.
	if ann.Type() == "type_annotation" {
		count := int(ann.ChildCount())
		for i := 0; i < count; i++ {
			ch := ann.Child(i)
			if ch == nil {
				continue
			}
			if ch.Type() == ":" {
				continue
			}
			return x.typeNodeLeaf(ch)
		}
		return ""
	}
	return x.typeNodeLeaf(ann)
}

// typeNodeLeaf walks a type node and returns its leaf type identifier.
func (x *extractor) typeNodeLeaf(t ts.Node) string {
	if t == nil {
		return ""
	}
	switch t.Type() {
	case "type_identifier":
		return x.nodeText(t)
	case "predefined_type":
		// `string`, `number`, `boolean` etc. — useful as opaque type
		// markers, but no class to bind to, so the receiver binder
		// won't find an import. Return verbatim.
		return x.nodeText(t)
	case "generic_type":
		// First named child is the underlying type_identifier or
		// nested generic.
		count := int(t.ChildCount())
		for i := 0; i < count; i++ {
			ch := t.Child(i)
			if ch == nil {
				continue
			}
			if ch.Type() == "type_identifier" || ch.Type() == "nested_type_identifier" {
				return x.nodeText(ch)
			}
		}
	case "array_type":
		// `T[]` — leaf is the element type.
		count := int(t.ChildCount())
		for i := 0; i < count; i++ {
			ch := t.Child(i)
			if ch != nil && ch.Type() != "[" && ch.Type() != "]" {
				return x.typeNodeLeaf(ch)
			}
		}
	case "nested_type_identifier":
		// `Foo.Bar` — pick the rightmost type_identifier.
		var last string
		count := int(t.ChildCount())
		for i := 0; i < count; i++ {
			ch := t.Child(i)
			if ch != nil && ch.Type() == "type_identifier" {
				last = x.nodeText(ch)
			}
		}
		return last
	}
	return ""
}
