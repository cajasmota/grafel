// framework_dsl.go — issues #514 and #517.
//
// Express HTTP DSL allowlist (#514)
// ──────────────────────────────────
// Express (and Koa / Fastify / Hono) expose a routing+response DSL on the
// objects they return from their factory call:
//
//   const app = express();         // or require("express")()
//   app.get("/path", handler);     // receiver-stripped → bare "get"
//   app.post("/path", handler);    // → bare "post"
//   res.status(404).json({});      // → bare "status", "json"
//
// The JS extractor strips the receiver and emits bare CALLS edges ("get",
// "post", "status", "json", …). These names collide with ordinary user
// methods in any non-Express codebase, so they cannot be added to the
// global jsDynamicPatterns catalog (issue #104 precedent).
//
// Fix: scan the file's import bindings for express-family package imports.
// When found, tag any call_expression whose immediate receiver variable
// can be traced to a known Express-family binding with
// Properties["receiver_package"] = "express" on the CALLS edge. The
// resolver checks this property before classifyDispositionLang and routes
// those edges directly to DispositionDynamic.
//
// NestJS bootstrap listener (#517)
// ─────────────────────────────────
// NestJS bootstrap functions follow a consistent pattern:
//
//   const app = await NestFactory.create(AppModule);
//   await app.listen(3000);   // receiver-stripped → bare "listen"
//
// The TS extractor emits a bare CALLS edge to "listen". Adding "listen" to
// jsDynamicPatterns is unsafe (collision with EventEmitter, custom methods).
//
// Fix: scan for `NestFactory.create(...)` call_expressions assigned to a
// local variable ("nestFactoryLocals"). When a subsequent call has
// `<nestVar>.listen(...)`, stamp the same receiver_package="express"
// property so the resolver routes it to Dynamic.
//
// The receiver_package value is "express" for all framework families in
// this file — the resolver only needs to know "this is a framework DSL
// call", not which specific framework.

package javascript

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// expressFrameworkPkgs is the set of npm package specifiers that introduce
// Express-family app/router/request/response objects. Keys are the exact
// import-path strings (as they appear in import statements or require()
// calls).
var expressFrameworkPkgs = map[string]bool{
	"express":                  true,
	"koa":                      true,
	"fastify":                  true,
	"hono":                     true,
	"@hono/node-server":        true,
	"@nestjs/platform-express": true,
	"@nestjs/platform-fastify": true,
}

// PropReceiverPackage is the edge-property key the extractor stamps on
// CALLS edges whose receiver was bound to an Express-family object. The
// resolver reads this property to classify the edge as Dynamic.
const PropReceiverPackage = "receiver_package"

// PropReceiverPackageExpress is the value stamped for all Express-family
// receivers (express / koa / fastify / hono / NestJS platform adapters and
// NestFactory.create locals).
const PropReceiverPackageExpress = "express"

// frameworkDSLTracker holds the per-file state that
// extractCallRelationshipsWithFramework uses to detect framework-DSL
// receiver calls.
//
// It is built once per file by buildFrameworkDSLTracker — a cheap single
// pass over the already-available import bindings and a one-time AST scan
// for NestFactory.create assignments.
type frameworkDSLTracker struct {
	// expressLocals is the set of local variable names that were bound to
	// an Express-family factory call (the default export of express / koa /
	// fastify / hono). Example: `const app = express()` → expressLocals["app"].
	expressLocals map[string]bool

	// nestFactoryLocals is the set of local variable names that were
	// assigned from `await NestFactory.create(...)`. Example:
	// `const app = await NestFactory.create(AppModule)` →
	// nestFactoryLocals["app"].
	nestFactoryLocals map[string]bool

	// expressDefaultNames holds the local identifier names that are the
	// default-import binding for an express-family package.
	// `import express from "express"` → expressDefaultNames["express"].
	// Used to detect `express()` factory calls and `app = express()`.
	expressDefaultNames map[string]bool
}

// buildFrameworkDSLTracker constructs a frameworkDSLTracker from the
// extractor's already-populated importByLocal map and a tree-walk to find
// NestFactory.create assignments.
//
// importByLocal is consulted to identify which local names are the default
// exports of Express-family packages. The root node is walked to find:
//
//   - variable_declarator nodes whose value is a call_expression where
//     the callee is one of the expressDefaultNames (capturing
//     `const app = express()`).
//   - variable_declarator nodes whose value is an await_expression
//     wrapping a member-expression call to NestFactory.create (capturing
//     `const app = await NestFactory.create(AppModule)`).
func (x *extractor) buildFrameworkDSLTracker(root ts.Node) *frameworkDSLTracker {
	t := &frameworkDSLTracker{
		expressLocals:       make(map[string]bool),
		nestFactoryLocals:   make(map[string]bool),
		expressDefaultNames: make(map[string]bool),
	}
	// Collect default-import names for express-family packages.
	for localName, b := range x.importByLocal {
		if b == nil {
			continue
		}
		if expressFrameworkPkgs[b.importPath] && b.importedName == "default" {
			t.expressDefaultNames[localName] = true
		}
	}
	// Walk the AST to find factory calls and NestFactory.create calls.
	if root != nil {
		t.scanForFrameworkLocals(x, root)
	}
	return t
}

// scanForFrameworkLocals performs an iterative AST walk looking for:
//
//   - `const app = express()` / `const router = express.Router()`:
//     LHS identifier is added to expressLocals when the RHS call_expression
//     has a callee that is either an expressDefaultName or a member-expr
//     `<expressDefaultName>.Router`.
//
//   - `const app = await NestFactory.create(...)`:
//     LHS identifier is added to nestFactoryLocals.
//
//   - CommonJS `const app = require("express")()`:
//     The inner require() resolves the package; the outer () creates the
//     app. Handled by checking if the callee is itself a call_expression
//     whose function is `require` and whose argument is an express-family
//     package string.
func (t *frameworkDSLTracker) scanForFrameworkLocals(x *extractor, root ts.Node) {
	stack := make([]ts.Node, 0, 64)
	stack = append(stack, root)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "variable_declarator" {
			t.processVariableDeclarator(x, n)
			// Still recurse into children — nested functions may have their own
			// local express calls (albeit rare).
		}
		count := int(n.ChildCount())
		for i := count - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
}

// processVariableDeclarator examines a single variable_declarator node and
// populates expressLocals, expressDefaultNames, or nestFactoryLocals as
// appropriate.
//
// Handled shapes:
//
//   - `const express = require("express")`      → expressDefaultNames["express"]
//   - `const app = express()`                   → expressLocals["app"]
//   - `const app = require("express")()`        → expressLocals["app"]
//   - `const app = await NestFactory.create(..)` → nestFactoryLocals["app"]
func (t *frameworkDSLTracker) processVariableDeclarator(x *extractor, n ts.Node) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil || nameNode.Type() != "identifier" {
		return
	}
	localName := x.nodeText(nameNode)
	if localName == "" {
		return
	}

	valueNode := n.ChildByFieldName("value")
	if valueNode == nil {
		return
	}

	// CommonJS: `const express = require("express")` — the value is a
	// call_expression to require() with a string arg that is an express-
	// family package. Add the local name to expressDefaultNames so the
	// subsequent `const app = express()` detection fires correctly.
	if valueNode.Type() == "call_expression" && t.isRequireExpressCall(x, valueNode) {
		t.expressDefaultNames[localName] = true
		return
	}

	// Unwrap await_expression — `const app = await NestFactory.create(...)`.
	inner := valueNode
	if inner.Type() == "await_expression" {
		// await_expression has a single non-await child.
		for i := 0; i < int(inner.ChildCount()); i++ {
			ch := inner.Child(i)
			if ch != nil && ch.Type() != "await" {
				inner = ch
				break
			}
		}
	}

	if inner.Type() != "call_expression" {
		return
	}

	// Check for express() or require("express")() shapes.
	if t.isExpressFactoryCall(x, inner) {
		t.expressLocals[localName] = true
		return
	}

	// Check for NestFactory.create(...) shape.
	if t.isNestFactoryCreateCall(x, inner) {
		t.nestFactoryLocals[localName] = true
		return
	}
}

// isExpressFactoryCall returns true when callNode is:
//   - `express()` — bare call on an express-default-import name
//   - `express.Router()` or `app.use(express.Router())` style (Router factory)
//   - `require("express")()` — CommonJS factory
//   - `fastify()`, `new Fastify()`, `Hono()`, `new Koa()` — analogous
func (t *frameworkDSLTracker) isExpressFactoryCall(x *extractor, callNode ts.Node) bool {
	fn := callNode.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	switch fn.Type() {
	case "identifier":
		// `express()`, `fastify()`, `Hono()`, `Koa()` etc.
		name := x.nodeText(fn)
		return t.expressDefaultNames[name]
	case "member_expression":
		// `express.Router()`, `fastify.default()`, etc.
		obj := fn.ChildByFieldName("object")
		if obj == nil {
			return false
		}
		objName := x.nodeText(obj)
		return t.expressDefaultNames[objName]
	case "call_expression":
		// `require("express")()` — CommonJS double-call.
		return t.isRequireExpressCall(x, fn)
	}
	return false
}

// isRequireExpressCall returns true when callNode is `require("<express-pkg>")`.
func (t *frameworkDSLTracker) isRequireExpressCall(x *extractor, callNode ts.Node) bool {
	if callNode.Type() != "call_expression" {
		return false
	}
	fn := callNode.ChildByFieldName("function")
	if fn == nil || x.nodeText(fn) != "require" {
		return false
	}
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "string" {
			raw := x.nodeText(ch)
			pkg := strings.Trim(raw, `"'`+"`")
			if expressFrameworkPkgs[pkg] {
				return true
			}
		}
	}
	return false
}

// isNestFactoryCreateCall returns true when callNode is
// `NestFactory.create(...)` (any number of arguments; the import-path
// check is deferred to the caller — NestFactory is always from @nestjs/core
// in real-world code but we match by name to avoid a package-binding walk).
func (t *frameworkDSLTracker) isNestFactoryCreateCall(x *extractor, callNode ts.Node) bool {
	fn := callNode.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return false
	}
	obj := fn.ChildByFieldName("object")
	prop := fn.ChildByFieldName("property")
	if obj == nil || prop == nil {
		return false
	}
	return x.nodeText(obj) == "NestFactory" && x.nodeText(prop) == "create"
}

// receiverPackageForCall returns PropReceiverPackageExpress when callNode
// is a member-expression call (`<recv>.<method>(...)`) where the receiver
// variable was bound to a framework-DSL object (express app, koa app,
// fastify instance, hono app, or NestJS application). Returns "" otherwise.
//
// This is the per-call gate: if the receiver is a known framework local,
// the CALLS edge produced for this call should carry
// Properties["receiver_package"] = "express".
func (t *frameworkDSLTracker) receiverPackageForCall(x *extractor, callNode ts.Node) string {
	if t == nil {
		return ""
	}
	fn := callNode.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return ""
	}
	obj := fn.ChildByFieldName("object")
	if obj == nil {
		return ""
	}
	// Support single-chain: `app.get(...)`, `router.use(...)`, `res.status(...)`.
	// Support double-chain: `res.status(404).json(...)` — the outer receiver is
	// a call_expression; we check the innermost identifier recursively.
	recvName := x.frameworkReceiverIdent(obj)
	if recvName == "" {
		return ""
	}
	if t.expressLocals[recvName] || t.nestFactoryLocals[recvName] {
		return PropReceiverPackageExpress
	}
	return ""
}

// frameworkReceiverIdent extracts the root receiver identifier from a
// (potentially chained) member expression object. Returns the bare
// identifier name for:
//
//   - identifier node             → "app", "router", "res", ...
//   - `res.status(404)` (call) → follow the callee's object: "res"
//   - `app.route("/x").get(...)` → follow to "app"
//
// Stops at depth 4 to avoid spending time on deeply-nested expressions
// that are unlikely to be framework chains. Returns "" on any miss.
func (x *extractor) frameworkReceiverIdent(obj ts.Node) string {
	const maxDepth = 4
	cur := obj
	for depth := 0; depth < maxDepth; depth++ {
		if cur == nil {
			return ""
		}
		switch cur.Type() {
		case "identifier":
			return x.nodeText(cur)
		case "member_expression":
			// `a.b` — walk the object side.
			cur = cur.ChildByFieldName("object")
		case "call_expression":
			// `a.b()` — walk the function's object side.
			fn := cur.ChildByFieldName("function")
			if fn == nil {
				return ""
			}
			if fn.Type() == "identifier" {
				return x.nodeText(fn)
			}
			if fn.Type() == "member_expression" {
				cur = fn.ChildByFieldName("object")
			} else {
				return ""
			}
		case "await_expression":
			// Rare: `(await factory.create()).listen()` — skip await.
			for i := 0; i < int(cur.ChildCount()); i++ {
				ch := cur.Child(i)
				if ch != nil && ch.Type() != "await" {
					cur = ch
					break
				}
			}
		default:
			return ""
		}
	}
	return ""
}
