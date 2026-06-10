// tests.go — TS/JS TESTS-edge emission (#1726).
//
// Ports the Python testmap pattern down into the JS/TS extractor so that
// each test function (it/test/describe block) in a Jest/Vitest/Mocha test
// file emits a TESTS edge for every production-looking call inside its body.
//
// Why a JS-native pass instead of relying solely on the cross-language
// internal/extractors/cross/testmap pass:
//
//  1. The cross/testmap pass DOES emit SCOPE.Pattern test_coverage entities
//     for Jest files, but every TESTS edge it produces has FromID =
//     "scope:operation:<file>#<test_qname>" — a structural-ref stub that
//     the resolver tries to bind through resolve/refs.go's testmap
//     short-form path. For JS/TS the test functions are anonymous arrow
//     callbacks passed to it(...)/test(...) — they don't exist as named
//     Operation entities in byLocation[file], so the FromID never resolves
//     and the edge is dropped. iter4 calibration confirmed this:
//     upvate-core (Python, named test_* def) gained TESTS edges; upvate-
//     frontend produced 1, upvate-mobile produced 0 across ~2500 entities.
//
//  2. Emitting the TESTS edge directly from the Operation entity that
//     contains the call (the enclosing named function, hook, or class
//     method that hosts the it() callback — or the file entity itself for
//     module-level it() calls) bypasses the resolver short-form path. The
//     FromID is the Operation's ComputeID hex, which lands in byLocation
//     and never goes through the testmap stub resolver.
//
//  3. The CALLS extractor already runs over every function body. This pass
//     re-uses those CALLS edges: when the source file is a test file AND
//     the callee is not a test helper / framework primitive, we ALSO emit
//     a TESTS edge alongside the CALLS edge. We do not REPLACE the CALLS
//     edge — downstream resolvers and the bug-rate calculator still need
//     CALLS to bind through normal channels.
//
// Detection conventions (filename + directory):
//
//   - *.test.{ts,tsx,js,jsx,mjs,cjs}
//   - *.spec.{ts,tsx,js,jsx,mjs,cjs}
//   - any file path that contains a "/__tests__/" segment
//   - any file path under a top-level or nested "/tests/" directory
//
// Stopwords:
//
//   The CALLS extractor already filters JS/TS built-in prototype methods
//   (Array.map, String.replace, …) via isBuiltinMethodName. On top of
//   that, this pass filters call targets that are themselves test-
//   framework primitives (it, test, describe, expect, jest.fn, vi.mock, …)
//   and common assertion helpers so the TESTS edges target production
//   code, not other parts of the test scaffolding.

package javascript

import (
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// testBlockCallees is the set of test-framework block functions whose final
// argument is a callback containing test-body code. `describe`/`suite`/
// `context` are grouping blocks (their body holds setup/teardown + nested
// it() blocks, handled recursively); `it`/`test`/`specify` are the leaf test
// blocks that own the production CALLS edges.
var testBlockCallees = map[string]bool{
	"describe": true, "suite": true, "context": true,
	"it": true, "test": true, "specify": true,
}

// testHookCallees is the set of setup/teardown hook functions whose callback
// bodies commonly construct the system-under-test (`beforeEach(() => {
// controller = new XController(svc); })`). Their bodies are scanned for
// local-variable construction bindings that the sibling it() blocks reuse,
// but they are not emitted as test Operations themselves.
var testHookCallees = map[string]bool{
	"beforeeach": true, "beforeall": true, "aftereach": true, "afterall": true,
	"before": true, "after": true, "setup": true, "teardown": true,
}

// jsTestFileExts is the set of file extensions that may host JS/TS test
// code under the `.test.` / `.spec.` naming convention. Mirrors
// jsVariantExts but adds `.mjs` and `.cjs` which platform_variants.go
// intentionally excludes (platform variants are tsx/jsx-only).
var jsTestFileExts = map[string]bool{
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
	".cjs": true,
}

// isJSTestFile reports whether filePath is a JS/TS test file according to
// the four conventions enumerated in the package doc. The match is
// case-sensitive; tree-sitter and the rest of the indexer treat file
// paths as-is on disk (the Metro/Node bundlers do too).
func isJSTestFile(filePath string) bool {
	if filePath == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	if !jsTestFileExts[ext] {
		return false
	}
	// Normalize separator so the directory-segment checks work the same
	// on Windows-style paths.
	norm := filepath.ToSlash(filePath)
	// Directory conventions.
	if strings.Contains(norm, "/__tests__/") || strings.HasPrefix(norm, "__tests__/") {
		return true
	}
	if strings.Contains(norm, "/tests/") || strings.HasPrefix(norm, "tests/") {
		return true
	}
	// Filename conventions: foo.test.ts / foo.spec.tsx / …
	base := strings.ToLower(filepath.Base(norm))
	stem := strings.TrimSuffix(base, ext)
	if strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") {
		return true
	}
	return false
}

// testCallStopwords is the set of call-target leaf names that are NEVER
// emitted as TESTS edge targets. They are test-framework primitives,
// assertion helpers, or mock-library setup calls — the things you call
// FROM a test, not the production code you're testing.
//
// Compared with isBuiltinMethodName (Array/String/Promise prototypes)
// which is applied during CALLS extraction, this list focuses on the
// test-scaffolding vocabulary that survives CALLS filtering because
// the targets are bare functions, not method calls on built-in types.
//
// Kept lowercase; callers compare with strings.ToLower.
var testCallStopwords = map[string]bool{
	// Jest / Vitest / Mocha / Jasmine entry points
	"it": true, "test": true, "describe": true, "suite": true,
	"beforeeach": true, "beforeall": true, "aftereach": true, "afterall": true,
	"setup": true, "teardown": true,
	"xit": true, "xtest": true, "xdescribe": true,
	"fit": true, "ftest": true, "fdescribe": true,
	"it.only": true, "it.skip": true, "it.each": true, "it.todo": true,
	"test.only": true, "test.skip": true, "test.each": true, "test.todo": true,
	"describe.only": true, "describe.skip": true, "describe.each": true,

	// Assertion library entry points
	"expect": true, "assert": true, "should": true,

	// Jest mocking primitives
	"jest.fn": true, "jest.mock": true, "jest.spyon": true, "jest.dofeed": true,
	"jest.clearallmocks": true, "jest.resetallmocks": true, "jest.restoreallmocks": true,
	"jest.usefaketimers": true, "jest.userealtimers": true,
	"jest.advancetimersbytime": true, "jest.runalltimers": true,
	"jest.setsystemtime": true, "jest.requireactual": true,

	// Vitest mocking primitives
	"vi.fn": true, "vi.mock": true, "vi.spyon": true, "vi.unmock": true,
	"vi.clearallmocks": true, "vi.resetallmocks": true, "vi.restoreallmocks": true,
	"vi.usefaketimers": true, "vi.userealtimers": true,
	"vi.advancetimersbytime": true, "vi.runalltimers": true,
	"vi.importactual": true, "vi.hoisted": true,

	// Sinon
	"sinon.stub": true, "sinon.spy": true, "sinon.mock": true,
	"sinon.fake": true, "sinon.createsandbox": true, "sinon.restore": true,

	// Testing Library (React/DOM)
	"render": true, "screen": true, "fireevent": true, "waitfor": true,
	"waitforElementtoBeRemoved": true, "act": true, "cleanup": true,
	"renderhook": true,

	// Enzyme (legacy but still in long-tail of TS/JS codebases)
	"shallow": true, "mount": true, "configure": true,

	// Common cypress/playwright top-level vocabulary
	"cy.visit": true, "cy.get": true, "cy.wait": true,
	"page.goto": true, "page.click": true, "page.fill": true,

	// Node test runner primitives
	"t.test": true, "t.equal": true, "t.deepequal": true, "t.same": true,
}

// testCallStopwordSuffixes is matched on the LOWER-CASED dotted suffix of
// the call target. Any target ending in one of these (e.g. ".tobe",
// ".toequal") is skipped — these are jest/chai assertion finishers.
var testCallStopwordSuffixes = []string{
	".tobe", ".toequal", ".tostrictequal", ".tomatchObject", ".tomatchsnapshot",
	".tohavebeencalled", ".tohavebeencalledwith", ".tohavebeencalledtimes",
	".tohavebeenlastcalledwith", ".tohavebeennthcalledwith",
	".tothrow", ".tothrowError", ".tothroworror",
	".toreturn", ".toreturnwith", ".tohavereturned",
	".tocontain", ".tocontainequal", ".tomatch", ".tomatchInline",
	".tobeundefined", ".tobedefined", ".tobenull", ".tobenan",
	".tobetruthy", ".tobefalsy", ".tobegreaterthan", ".tobelessthan",
	".tobegreaterthanorequal", ".tobelessthanorequal", ".tobecloseto",
	".tobeinstance", ".tobeinstanceof",
	".not.tobe", ".not.toequal", ".not.tohavebeencalled",
	".mockreturnvalue", ".mockreturnvalueonce", ".mockresolvedvalue",
	".mockresolvedvalueonce", ".mockrejectedvalue", ".mockrejectedvalueonce",
	".mockimplementation", ".mockimplementationonce",
	".mockclear", ".mockreset", ".mockrestore",
	".called", ".calledonce", ".calledwith", ".callcount",
}

// isTestCallStopword reports whether a callee identifier (as emitted by
// extractCallRelationships into RelationshipRecord.ToID) is a test-
// scaffolding primitive that must NOT be promoted into a TESTS edge.
//
// Match rules (case-insensitive):
//
//   - exact match against testCallStopwords (covers "expect", "jest.fn",
//     "vi.mock", "render", …).
//   - dotted-suffix match against testCallStopwordSuffixes (covers chai/
//     jest assertion finishers like ".toBe", ".toHaveBeenCalledWith").
//   - the trailing identifier starts with "mock" — covers user-defined
//     mocks named `mockGetUser`, `mockedFetch`, etc.
//
// Structural-ref stubs (containing ':') are NEVER stopwords — those are
// resolver-bound cross-file refs that point at real production entities
// in another file, exactly the targets we want to surface.
func isTestCallStopword(target string) bool {
	if target == "" {
		return false
	}
	// Structural refs always survive — the resolver will bind them.
	if strings.Contains(target, ":") {
		return false
	}
	low := strings.ToLower(target)
	if testCallStopwords[low] {
		return true
	}
	for _, sfx := range testCallStopwordSuffixes {
		if strings.HasSuffix(low, sfx) {
			return true
		}
	}
	// Trailing identifier starts with "mock" — user-defined mocks.
	tail := low
	if idx := strings.LastIndexByte(low, '.'); idx >= 0 {
		tail = low[idx+1:]
	}
	if strings.HasPrefix(tail, "mock") {
		return true
	}
	return false
}

// emitTestsEdgesForTestFile walks every Operation entity emitted for the
// current file and, for each CALLS relationship whose target is a
// plausible production function, appends a sibling TESTS relationship.
//
// Wiring: called from Extract() AFTER walk() + emitReferences() so the
// Operation entities and their CALLS relationships are already in place.
// A no-op when isJSTestFile(x.filePath) returns false, so the hot path
// for the ~95% of non-test files in a typical repo costs only the
// filename check.
//
// We do NOT mutate the CALLS edge (its existence is load-bearing for
// the downstream resolver). The TESTS edge is added as a NEW
// RelationshipRecord targeting the same ToID, with Properties carrying
// the test_framework hint when one was already detected.
//
// Confidence: every emitted TESTS edge from this pass is high-confidence
// (direct call inside a test body). The naming-convention fallback path
// (low confidence) is still owned by the cross-language testmap
// extractor — that pass continues to run alongside this one.
func (x *extractor) emitTestsEdgesForTestFile() {
	if !isJSTestFile(x.filePath) {
		return
	}
	framework := detectTestFramework(x.filePath)
	for i := range x.entities {
		ent := &x.entities[i]
		// Only Operation entities have meaningful call relationships
		// here. We intentionally skip SCOPE.Component (file/class) and
		// SCOPE.Schema entities — calls on those are infrastructural,
		// not test→production bindings.
		if ent.Kind != "SCOPE.Operation" {
			continue
		}
		// Collect new TESTS edges in a side slice so we don't mutate
		// the underlying slice while iterating it.
		var add []types.RelationshipRecord
		seen := map[string]bool{}
		for _, rel := range ent.Relationships {
			if rel.Kind != "CALLS" {
				continue
			}
			if rel.ToID == "" {
				continue
			}
			if isTestCallStopword(rel.ToID) {
				continue
			}
			if seen[rel.ToID] {
				continue
			}
			seen[rel.ToID] = true
			props := map[string]string{
				"confidence":     "high",
				"test_framework": framework,
				"provenance":     "DIRECT_CALL_IN_TEST_BODY",
			}
			// Preserve receiver_package when the original CALLS edge
			// carried it — downstream consumers want the same routing
			// metadata on the derived TESTS edge.
			if rel.Properties != nil {
				if pkg, ok := rel.Properties[PropReceiverPackage]; ok && pkg != "" {
					props[PropReceiverPackage] = pkg
				}
			}
			add = append(add, types.RelationshipRecord{
				ToID:       rel.ToID,
				Kind:       string(types.RelationshipKindTests),
				Properties: props,
			})
		}
		if len(add) > 0 {
			ent.Relationships = append(ent.Relationships, add...)
		}
	}
}

// extractTestScopeOperations indexes test-framework block callbacks
// (describe/it/test/...) in a test file as call-bearing SCOPE.Operation
// entities, so the production calls inside them yield CALLS edges (which
// emitTestsEdgesForTestFile then promotes to TESTS edges). Issue #4671.
//
// Why this pass is needed: the generic walk emits Operation entities only for
// NAMED functions / methods / arrow-const declarations. A controller UNIT spec
// has none of those — its bodies are anonymous callbacks passed to
// `describe(...)` / `it(...)`. Before this pass such a spec produced ZERO
// call-bearing entities (only the file entity + IMPORTS), so the handler call
// `controller.getCounts(...)` was never extracted and the test→handler CALLS
// edge never existed — the live-proven dominant cause of the ~4x coverage
// undercount on upvate-v3.
//
// Each leaf test block (it/test/specify) becomes one SCOPE.Operation
// (subtype "test") whose CALLS edges come from extractCallRelationships over
// the callback body. Receiver typing for `<localVar>.method()` is seeded from
// local-variable construction bindings (`const c = new XController(svc)` /
// `module.get(XController)`) collected across the ENCLOSING describe subtree,
// so a system-under-test constructed in a `beforeEach` hook is visible to the
// it() blocks that call it.
//
// No-op for non-test files (cheap filename check).
func (x *extractor) extractTestScopeOperations(root *sitter.Node) {
	if root == nil || !isJSTestFile(x.filePath) {
		return
	}
	// Seed an empty file-scope frame; describe blocks accumulate local-var
	// construction bindings as we descend. Top-level (module-scope)
	// constructions outside any describe are collected here too.
	fileScope := &classBindings{fields: map[string]string{}}
	x.collectModuleLevelConstructions(root, fileScope)
	x.walkTestScope(root, fileScope)
}

// collectModuleLevelConstructions records construction bindings declared at
// module scope (outside any describe/it block) — e.g.
// `const controller = new XController(mockSvc);` written at the top of the
// spec file rather than inside a beforeEach. These are visible to every test
// block in the file.
func (x *extractor) collectModuleLevelConstructions(root *sitter.Node, scope *classBindings) {
	for i := 0; i < int(root.ChildCount()); i++ {
		ch := root.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "lexical_declaration", "variable_declaration", "expression_statement":
			x.collectLocalVarTypes(ch, scope)
		}
	}
}

// walkTestScope descends the CST looking for test-framework block calls.
// inherited carries local-variable construction bindings collected from
// enclosing describe scopes (so beforeEach-constructed instances flow down to
// nested it() blocks). It is never mutated — each describe scope clones it and
// adds its own hook/body constructions.
func (x *extractor) walkTestScope(n *sitter.Node, inherited *classBindings) {
	if n == nil {
		return
	}
	if n.Type() == "call_expression" {
		callee := strings.ToLower(x.testBlockCalleeName(n))
		switch {
		case testBlockCallees[callee]:
			x.handleTestBlock(n, callee, inherited)
			return
		case testHookCallees[callee]:
			// Hooks are scanned for construction bindings at the describe
			// level (see handleTestBlock); a hook body has no nested it()
			// blocks, so descend no further for block detection.
			return
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		x.walkTestScope(n.Child(i), inherited)
	}
}

// handleTestBlock processes a describe/it/test call. A grouping block
// (describe/suite/context) collects local-var construction bindings from its
// direct setup hooks + statements into a child frame, then recurses into the
// callback body to reach nested it() blocks. A leaf test block (it/test/
// specify) emits one SCOPE.Operation carrying the CALLS edges extracted from
// the callback body, with receiver typing seeded by the inherited +
// block-local construction bindings.
func (x *extractor) handleTestBlock(call *sitter.Node, callee string, inherited *classBindings) {
	cbBody := x.testBlockCallbackBody(call)
	if cbBody == nil {
		return
	}
	if callee == "describe" || callee == "suite" || callee == "context" {
		// Clone the inherited frame and add this describe scope's own
		// construction bindings: those declared directly in the describe
		// body AND those inside its setup/teardown hooks (where the
		// system-under-test is typically `new`-ed up).
		scope := cloneBindings(inherited)
		x.collectLocalVarTypes(cbBody, scope)
		for i := 0; i < int(cbBody.ChildCount()); i++ {
			x.collectHookConstructions(cbBody.Child(i), scope)
		}
		for i := 0; i < int(cbBody.ChildCount()); i++ {
			x.walkTestScope(cbBody.Child(i), scope)
		}
		return
	}
	// Leaf test block: build the frame (inherited construction bindings +
	// any locals declared inside this it() body) and extract the calls.
	frame := cloneBindings(inherited)
	name := x.testBlockName(call, callee)
	rels := x.extractCallRelationships(cbBody, name, frame)
	if len(rels) == 0 {
		return
	}
	sig := fmt.Sprintf("%s(%q)", callee, name)
	x.emitWithRels(name, "SCOPE.Operation", call, "test", sig, rels)
}

// cloneBindings returns a deep copy of b's field map (b may be nil).
func cloneBindings(b *classBindings) *classBindings {
	out := &classBindings{fields: map[string]string{}}
	if b != nil {
		out.className = b.className
		for k, v := range b.fields {
			out.fields[k] = v
		}
	}
	return out
}

// collectHookConstructions records local-variable construction bindings from
// a setup/teardown hook callback body into scope. A no-op for non-hook nodes.
func (x *extractor) collectHookConstructions(n *sitter.Node, scope *classBindings) {
	if n == nil {
		return
	}
	// Hooks appear as `expression_statement(call_expression)` or a bare
	// call_expression. Descend through the statement wrapper to find the call.
	call := n
	if call.Type() == "expression_statement" && call.ChildCount() > 0 {
		call = call.Child(0)
	}
	if call == nil || call.Type() != "call_expression" {
		return
	}
	if !testHookCallees[strings.ToLower(x.testBlockCalleeName(call))] {
		return
	}
	if body := x.testBlockCallbackBody(call); body != nil {
		x.collectLocalVarTypes(body, scope)
	}
}

// testBlockCalleeName returns the callee identifier of a call_expression,
// handling bare `it(...)` and dotted `it.each(...)` / `describe.only(...)`
// shapes (returns the leading identifier "it" / "describe"). Returns "" when
// the callee is not a plain-identifier shape.
func (x *extractor) testBlockCalleeName(call *sitter.Node) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return x.nodeText(fn)
	case "member_expression":
		// `it.only` / `describe.skip` / `it.each` — the grouping/leaf
		// identity is the object identifier.
		obj := fn.ChildByFieldName("object")
		if obj != nil && obj.Type() == "identifier" {
			return x.nodeText(obj)
		}
	case "call_expression":
		// `it.each(table)(name, cb)` — the inner call's callee is
		// `it.each`; recurse to recover "it".
		return x.testBlockCalleeName(fn)
	}
	return ""
}

// testBlockCallbackBody returns the statement_block body of the LAST
// function/arrow argument of a test-block call. Returns nil when the final
// argument is not a function with a block body (e.g. an it() with only a name,
// or an arrow with an expression body — no statements to scan).
func (x *extractor) testBlockCallbackBody(call *sitter.Node) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var fn *sitter.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		a := args.Child(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "arrow_function", "function_expression", "function":
			fn = a // last function-typed arg wins
		}
	}
	if fn == nil {
		return nil
	}
	body := fn.ChildByFieldName("body")
	if body == nil || body.Type() != "statement_block" {
		return nil
	}
	return body
}

// testBlockName derives a stable entity name for a test block from its first
// string-literal argument (the test description), falling back to the callee
// when no literal description is present. The line suffix avoids collisions
// when two it() blocks share a description.
func (x *extractor) testBlockName(call *sitter.Node, callee string) string {
	args := call.ChildByFieldName("arguments")
	desc := ""
	if args != nil {
		for i := 0; i < int(args.ChildCount()); i++ {
			a := args.Child(i)
			if a == nil {
				continue
			}
			if a.Type() == "string" || a.Type() == "template_string" {
				desc = strings.Trim(x.nodeText(a), "`'\"")
				break
			}
		}
	}
	if desc == "" {
		desc = callee
	}
	line := int(call.StartPoint().Row) + 1
	return fmt.Sprintf("%s:%s@%d", callee, desc, line)
}

// detectTestFramework returns a best-guess framework name from the file
// path conventions alone. We do NOT parse the source for import strings
// here — the cross/testmap pass already does that. This is purely a
// metadata hint stamped onto TESTS edges for downstream filtering /
// reporting.
//
// Heuristics:
//
//   - cypress conventions (/cypress/, .cy.) → "cypress"
//   - playwright conventions (/playwright/, .pw., e2e/) → "playwright"
//   - everything else under .test./.spec./__tests__/tests/ → "jest"
//     (jest is the dominant JS/TS unit-test runner; vitest mimics its
//     API and matchers — distinguishing them needs source-text inspection
//     which we leave to testmap).
func detectTestFramework(filePath string) string {
	norm := strings.ToLower(filepath.ToSlash(filePath))
	switch {
	case strings.Contains(norm, "/cypress/"),
		strings.Contains(norm, ".cy."):
		return "cypress"
	case strings.Contains(norm, "/playwright/"),
		strings.Contains(norm, ".pw."),
		strings.Contains(norm, "/e2e/") && (strings.HasSuffix(norm, ".test.ts") ||
			strings.HasSuffix(norm, ".spec.ts")):
		return "playwright"
	}
	return "jest"
}
