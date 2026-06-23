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
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// testBlockCallNames is the set of test-framework block functions whose
// arrow/function callback body hosts spec logic. A module-level call to one
// of these (describe/it/test/...) is the owner-less call site issue #4671
// addresses: its callback is not a named entity, so walk() never ran the
// call-relationship extractor over it.
var testBlockCallNames = map[string]bool{
	"describe": true, "it": true, "test": true, "suite": true, "context": true,
	"beforeeach": true, "beforeall": true, "aftereach": true, "afterall": true,
	"before": true, "after": true,
}

// emitTestScopeOwner emits a single SCOPE.Operation entity per JS/TS test
// file that owns every CALLS edge reachable from the module-level
// test-framework blocks (describe/it/test/before*/after*). Those blocks pass
// their spec logic as arrow/function callbacks that are NOT named entities,
// so walk() produced no owner for the `subject.method()` calls inside them —
// the root cause that left controller-unit specs unable to credit coverage
// (#4671). The call extractor runs with a nil base frame; withLocalReceiverTypes
// (invoked inside extractCallRelationships) types the locally-constructed
// subjects (`const c = new XController()`, `module.get(X)`) so their method
// calls resolve to the imported class's source file.
//
// Scope discipline: only the module-level test-block callbacks are mined.
// Calls inside named functions / class methods already have owners from
// walk(), so re-mining them here would double-emit CALLS edges. We therefore
// walk the program's children, pick the test-block call statements, and
// extract from their callback bodies only — never descending into named
// function/class declarations.
//
// No-op for non-test files and for test files with no module-level blocks.
func (x *extractor) emitTestScopeOwner(root ts.Node) {
	if root == nil || !isJSTestFile(x.filePath) {
		return
	}
	bodies := x.collectTestBlockBodies(root)
	if len(bodies) == 0 {
		return
	}
	scopeName := testScopeName(x.filePath)
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, body := range bodies {
		for _, rel := range x.extractCallRelationships(body, scopeName, nil) {
			// Only CALLS edges define the test→subject reachability that
			// ComputeCoverage credits; drop the sibling navigation / uses /
			// validates edges this extractor also produces (they belong to
			// real owner entities, not this synthetic test scope).
			if rel.Kind != "CALLS" {
				continue
			}
			key := rel.ToID
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			rels = append(rels, rel)
		}
	}
	if len(rels) == 0 {
		return
	}
	x.emitWithRels(
		scopeName,
		"SCOPE.Operation",
		root,
		"test_scope",
		"test scope "+scopeName,
		rels,
	)
}

// collectTestBlockBodies returns the callback bodies of every module-level
// test-framework block (describe/it/test/before*/after*) in the program.
// Blocks may be nested (describe → it); we recurse into a matched block's
// callback so inner it() bodies are mined too, but we do NOT descend into
// named function/class declarations (their calls already have owners).
func (x *extractor) collectTestBlockBodies(root ts.Node) []ts.Node {
	var out []ts.Node
	x.walkTestBlocks(root, &out)
	return out
}

// walkTestBlocks recursively finds test-block call callbacks. It treats a
// call_expression whose callee leaf is a test-block name as a block, records
// its callback body, and recurses into that body to catch nested blocks.
func (x *extractor) walkTestBlocks(n ts.Node, out *[]ts.Node) {
	if n == nil {
		return
	}
	switch n.Type() {
	// Do not descend into named declarations — walk() already owns their
	// call relationships.
	case "function_declaration", "class_declaration", "method_definition":
		return
	case "call_expression":
		if body := x.testBlockCallbackBody(n); body != nil {
			*out = append(*out, body)
			// Recurse into the callback body to find nested it()/test()
			// blocks; their bodies are appended too.
			x.walkTestBlocks(body, out)
			return
		}
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		x.walkTestBlocks(n.Child(i), out)
	}
}

// testBlockCallbackBody returns the body node of a test-block call's
// arrow/function callback, or nil when the call is not a recognised test
// block or carries no function callback. Handles `describe('x', () => {...})`,
// `it('y', async () => {...})`, and `it('y', function () {...})`.
func (x *extractor) testBlockCallbackBody(call ts.Node) ts.Node {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	var leaf string
	switch fn.Type() {
	case "identifier":
		leaf = x.nodeText(fn)
	case "member_expression":
		// `it.only(...)`, `describe.each(...)` etc. — match on the object leaf.
		obj := fn.ChildByFieldName("object")
		if obj != nil && obj.Type() == "identifier" {
			leaf = x.nodeText(obj)
		}
	}
	if leaf == "" || !testBlockCallNames[strings.ToLower(leaf)] {
		return nil
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	count := int(args.ChildCount())
	for i := 0; i < count; i++ {
		a := args.Child(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "arrow_function", "function_expression", "function":
			if b := a.ChildByFieldName("body"); b != nil {
				return b
			}
		}
	}
	return nil
}

// testScopeName derives a stable name for the per-file test-scope owner
// entity from the file path: the base filename with its extension stripped,
// suffixed with "::testScope" so it never collides with a production symbol.
func testScopeName(filePath string) string {
	base := filepath.Base(filepath.ToSlash(filePath))
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return stem + "::testScope"
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
