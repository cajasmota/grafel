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
	sitter "github.com/smacker/go-tree-sitter"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

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
func (x *extractor) receiverTypedTarget(memberExpr *sitter.Node, method string, frame *classBindings) string {
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
func (x *extractor) receiverIdent(obj *sitter.Node) string {
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
//                                               (NestJS @Inject style;
//                                               this is the dominant
//                                               shape issue #421 cares
//                                               about — the extractor
//                                               imports both the field
//                                               and the param into the
//                                               same lookup scope).
//   - `name?: Type`                           — optional param
//
// Untyped parameters (`function f(x)`) and parameter destructuring
// shapes are silently skipped.
func (x *extractor) functionParamFrame(params *sitter.Node, base *classBindings) *classBindings {
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
//                                                  carrying access
//                                                  modifiers become
//                                                  parameter properties.
//
// Anything else (untyped fields, methods, getters/setters) is skipped.
func (x *extractor) collectClassFields(body *sitter.Node, out map[string]string) {
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
		case "public_field_definition":
			name := x.childFieldText(ch, "name")
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
func (x *extractor) collectConstructorParamProperties(ctor *sitter.Node, out map[string]string) {
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
func (x *extractor) hasAccessModifier(p *sitter.Node) bool {
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
func (x *extractor) paramNameAndType(p *sitter.Node) (string, string) {
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
func (x *extractor) typeAnnotationLeaf(ann *sitter.Node) string {
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
func (x *extractor) typeNodeLeaf(t *sitter.Node) string {
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
