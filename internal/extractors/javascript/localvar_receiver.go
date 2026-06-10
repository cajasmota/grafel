// Package javascript — issue #4671 local-variable receiver typing.
//
// The receiver binder in receiver.go types `this.<field>.method()` and bare
// `<param>.method()` shapes via the enclosing class's fields and the caller's
// typed parameters (issue #421). It does NOT type LOCAL VARIABLES that are
// assigned from a constructor call (`const c = new XController(svc)`) or a
// NestJS DI lookup (`const c = module.get(XController)`).
//
// That local-variable form is the DOMINANT shape in controller/service UNIT
// specs:
//
//	const controller = new XController(mockSvc);
//	describe('XController', () => {
//	  it('counts', () => { controller.getCounts('2025'); });
//	});
//
// Without local-var typing the `controller.getCounts(...)` call falls back to
// a bare trailing-method-name CALLS edge that never binds to the handler, so
// the test→handler CALLS edge is missing and ComputeCoverage marks the
// endpoint untested — the live-proven ~4x coverage undercount (graph 18.2%
// vs real ~80%) on upvate-v3.
//
// This file adds collectLocalVarTypes: a body-scoped scan that records every
// `<local> = new ClassName(...)` and `<local> = <di>.get(ClassName)` binding
// into a classBindings frame, so receiverTypedTarget resolves
// `<local>.method()` to `ClassName.method` exactly as it already does for
// constructor-injected fields. The recorded type is resolved to its declaring
// file via importByLocal (same path the field binder uses), so the structural
// ref binds cross-file in the resolver.
//
// Scope discipline: the scan is conservative — it only records a binding when
// the RHS is a recognised construction form. The receiver binder
// (receiverTypedTarget) still gates the final structural-ref emission on the
// class resolving through importByLocal, so an unimported / external class
// never misresolves. The scan is whole-subtree (it descends into nested
// blocks) because spec bodies place the construction in a `beforeEach`/
// `describe` scope and the call in a sibling `it` block; collecting across the
// subtree lets the per-`it` frame see the construction. Same-name collisions
// follow last-write-wins, mirroring runtime reassignment semantics.
//
// GENERALIZE (issue #4671, standing rule): the local-variable receiver-typing
// gap is identical in Java (`new XController(); c.method()`, Mockito
// @InjectMocks), Python (`c = XViewSet(); c.method()`), Go, Ruby, etc. TS/JS
// is implemented here; per-language follow-ups are filed separately.

package javascript

import (
	sitter "github.com/smacker/go-tree-sitter"
)

// diLookupMethods is the set of trailing method names on a NestJS / DI
// container that return a typed instance when called with a class token:
//
//	module.get(XController)
//	app.get(XController)
//	moduleRef.get(XController)
//	moduleRef.resolve(XController)        // request-scoped providers
//	TestBed.inject(XController)            // Angular testing
//
// The receiver of the lookup (module / app / moduleRef / TestBed) is not
// type-checked — any `<recv>.get(ClassToken)` / `<recv>.inject(ClassToken)` /
// `<recv>.resolve(ClassToken)` whose sole argument is a bare class identifier
// is treated as producing an instance of that class. This deliberately covers
// the dominant DI idioms without requiring us to model the container's own
// type.
var diLookupMethods = map[string]bool{
	"get":     true,
	"resolve": true,
	"inject":  true,
}

// frameWithLocalVarTypes returns a frame that carries the incoming frame's
// bindings PLUS every local-variable construction binding found in body.
// The incoming frame is never mutated — class-field frames are shared across
// sibling method bodies, so mutating in place would leak one method's locals
// into another. When body has no class-construction locals the incoming
// frame is returned unchanged (no allocation on the hot non-test path).
func (x *extractor) frameWithLocalVarTypes(body *sitter.Node, base *classBindings) *classBindings {
	if body == nil {
		return base
	}
	// Pre-scan to avoid cloning when there's nothing to add. We only clone
	// on a hit so the hot non-test path stays allocation-free.
	locals := &classBindings{fields: map[string]string{}}
	x.collectLocalVarTypes(body, locals)
	if len(locals.fields) == 0 {
		return base
	}
	merged := &classBindings{fields: map[string]string{}}
	if base != nil {
		merged.className = base.className
		for k, v := range base.fields {
			merged.fields[k] = v
		}
	}
	// Local constructions win over inherited field/param bindings of the
	// same name — the local is closer to the call site.
	for k, v := range locals.fields {
		merged.fields[k] = v
	}
	return merged
}

// collectLocalVarTypes scans body (whole subtree) for local-variable
// declarations whose initialiser is a class-construction form, recording each
// `<localName> -> <ClassName>` binding into frame.fields. frame must be
// non-nil; collisions follow last-write-wins.
//
// Recognised initialiser shapes:
//
//	const c = new ClassName(...)              // direct construction
//	let  c = new ClassName()                  // (var / let / const all fine)
//	const c = module.get(ClassName)           // NestJS DI container lookup
//	const c = moduleRef.resolve(ClassName)    // request-scoped DI
//	const c = TestBed.inject(ClassName)       // Angular DI
//
// Plain `c = expr` assignment statements (no declarator keyword) are also
// scanned so `beforeEach(() => { controller = new X(svc); })` rebinds an
// outer `let controller;` — the construction commonly lives in a setup
// callback while the declaration is a bare `let` in the describe scope.
func (x *extractor) collectLocalVarTypes(body *sitter.Node, frame *classBindings) {
	if body == nil || frame == nil || frame.fields == nil {
		return
	}
	// variable_declarator covers `const/let/var c = <init>`; the
	// assignment_expression covers a later bare `c = <init>` rebinding.
	decls := findAllNodes(body, "variable_declarator", "assignment_expression")
	for _, d := range decls {
		name, className := x.localVarConstruction(d)
		if name == "" || className == "" {
			continue
		}
		frame.fields[name] = className
	}
}

// localVarConstruction returns the (localName, className) pair when node is a
// variable_declarator or assignment_expression whose target is a plain
// identifier and whose initialiser is a recognised class-construction form.
// Returns ("","") on any miss.
func (x *extractor) localVarConstruction(node *sitter.Node) (string, string) {
	if node == nil {
		return "", ""
	}
	var nameNode, initNode *sitter.Node
	switch node.Type() {
	case "variable_declarator":
		nameNode = node.ChildByFieldName("name")
		initNode = node.ChildByFieldName("value")
	case "assignment_expression":
		nameNode = node.ChildByFieldName("left")
		initNode = node.ChildByFieldName("right")
	default:
		return "", ""
	}
	if nameNode == nil || initNode == nil {
		return "", ""
	}
	// Only plain-identifier targets — destructuring (`const { a } = ...`)
	// and member targets (`this.x = ...`) are out of scope here.
	if nameNode.Type() != "identifier" {
		return "", ""
	}
	className := x.constructionClassName(initNode)
	if className == "" {
		return "", ""
	}
	return x.nodeText(nameNode), className
}

// constructionClassName extracts the constructed class name from an
// initialiser expression. Handles:
//
//   - new_expression:        `new ClassName(...)`  → "ClassName"
//   - call_expression DI:    `module.get(ClassName)` / `.resolve(...)` /
//     `.inject(...)`         → "ClassName" (sole bare-identifier argument)
//
// Awaited DI lookups (`await moduleRef.resolve(ClassName)`) are unwrapped.
// Returns "" for any other shape.
func (x *extractor) constructionClassName(init *sitter.Node) string {
	if init == nil {
		return ""
	}
	switch init.Type() {
	case "await_expression":
		// `await module.resolve(ClassName)` — unwrap and recurse.
		for i := 0; i < int(init.ChildCount()); i++ {
			ch := init.Child(i)
			if ch == nil || ch.Type() == "await" {
				continue
			}
			return x.constructionClassName(ch)
		}
		return ""
	case "new_expression":
		ctor := init.ChildByFieldName("constructor")
		if ctor == nil {
			return ""
		}
		switch ctor.Type() {
		case "identifier", "type_identifier":
			return x.nodeText(ctor)
		}
		return ""
	case "call_expression":
		return x.diLookupClassName(init)
	}
	return ""
}

// diLookupClassName returns the class-token argument of a DI container lookup
// call (`<recv>.get(ClassName)` / `.resolve(ClassName)` / `.inject(ClassName)`)
// when the call has exactly one argument and that argument is a bare class
// identifier. Returns "" otherwise.
func (x *extractor) diLookupClassName(call *sitter.Node) string {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return ""
	}
	prop := fn.ChildByFieldName("property")
	if prop == nil {
		return ""
	}
	if !diLookupMethods[x.nodeText(prop)] {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	// Find the single class-identifier argument. Skip punctuation; bail if
	// there is more than one value argument (a generic `get(token, opts)`
	// shape is not a class-instance lookup we can type confidently).
	var classArg *sitter.Node
	valueArgs := 0
	for i := 0; i < int(args.ChildCount()); i++ {
		a := args.Child(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "(", ")", ",":
			continue
		}
		valueArgs++
		classArg = a
	}
	if valueArgs != 1 || classArg == nil {
		return ""
	}
	switch classArg.Type() {
	case "identifier", "type_identifier":
		return x.nodeText(classArg)
	}
	return ""
}
