package php

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitPHPTestScopeOwner emits a single SCOPE.Operation entity per Pest spec file
// that owns every CALLS edge reachable from the spec's anonymous closure
// callbacks (`it('...', function () {...})` / `test('...', fn () => ...)` and the
// describe/beforeEach/etc. block bodies that wrap them).
//
// Issue #4686 (the PHP slice of epic #4615 / #4672). Pest test logic lives in
// anonymous closures passed to `it`/`test`/`describe` — those closures are NOT
// method or function declarations, so walk() (which only mines
// `method_declaration` / `function_definition` bodies for CALLS) produced no
// owner for the `$c->getCounts()` calls inside them. A Pest spec file therefore
// emitted zero CALLS edges, and ComputeCoverage saw every controller/handler
// reached only by a Pest spec as untested — the exact symptom the TS/JS slice
// (#4680, javascript/tests.go::emitTestScopeOwner) and the Ruby slice (#4684,
// ruby/tests.go::emitRubyTestScopeOwner) fixed for anonymous `it()` callbacks.
//
// PHPUnit's `public function test_x()` / `#[Test]` methods ARE named method
// declarations, already mined by walk() — including the local-variable receiver
// typing (`$c = new XController(); $c->getCounts()`) that collectLocalVarTypes /
// phpCallTarget already resolve. Only the Pest anonymous-closure case is RED, so
// this pass mines ONLY the anonymous closure / arrow-function bodies of Pest DSL
// calls and never descends into a `method_declaration` / `function_definition`
// (walk() already owns those CALLS edges → no double-emit).
//
// Local-variable receiver typing is reused as-is: the closure bodies are fed
// through the exact same extractCallRelationships used for named methods, so a
// `$c = new XController()` binding inside an `it()` closure types the subsequent
// `$c->getCounts()` to the dotted `XController.getCounts` target the resolver
// binds cross-file to the controller method (issue #4686 gap 1, mirrors Ruby
// #4684 / Python #4681 / TS/JS #4671). Route-hit linkage (`$this->getJson('/x')`)
// is handled separately by the custom Pest/PHPUnit extractor's e2e_route_calls
// path — this owner is purely for closure-body CALLS.
//
// No-op for non-test files and for spec files whose closures resolve no CALLS.
func emitPHPTestScopeOwner(root ts.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if root == nil || !isPHPTestFile(file.Path) {
		return
	}
	bodies := collectPestClosureBodies(root, file.Content)
	if len(bodies) == 0 {
		return
	}
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, body := range bodies {
		// Reuse the named-method call resolver. callerName is the synthetic
		// scope name; parentClass is "" (a Pest closure has no enclosing class),
		// so $this-> calls fall through to the bare leaf — correct, because in
		// Pest `$this` is the TestCase, not a production handler.
		for _, rel := range extractCallRelationships(body, file.Content, phpTestScopeName(file.Path), "") {
			if rel.ToID == "" || seen[rel.ToID] {
				continue
			}
			// Drop bare (unresolved) leaves — a Pest closure is littered with
			// DSL/assertion calls (`expect`, `assertStatus`, `toBe`) that carry
			// no receiver type. The owner exists to credit the production
			// handler the spec exercises, so keep only dotted Class.method
			// targets (receiver-typed or static). Mirrors Ruby #4684.
			if !strings.Contains(rel.ToID, ".") {
				continue
			}
			seen[rel.ToID] = true
			rels = append(rels, rel)
		}
	}
	if len(rels) == 0 {
		return
	}
	name := phpTestScopeName(file.Path)
	rec := types.EntityRecord{
		Name:          name,
		Kind:          "SCOPE.Operation",
		Subtype:       "test_scope",
		SourceFile:    file.Path,
		Language:      "php",
		StartLine:     1,
		EndLine:       1,
		Relationships: rels,
		Properties: map[string]string{
			"framework":   "pest",
			"provenance":  "INFERRED_FROM_PEST_TEST_SCOPE",
			"test_scope":  "true",
			"description": "test scope " + name,
		},
	}
	*out = append(*out, rec)
}

// pestBlockMethods is the set of Pest DSL functions whose anonymous closure
// argument hosts spec logic that may call into production code. Mirrors
// ruby/tests.go::rspecBlockMethods and javascript/tests.go::testBlockCallNames.
var pestBlockMethods = map[string]bool{
	"it": true, "test": true, "describe": true,
	"beforeEach": true, "afterEach": true,
	"beforeAll": true, "afterAll": true,
}

// collectPestClosureBodies returns the body node of every anonymous closure /
// arrow function passed to a Pest DSL call (it/test/describe/beforeEach/...) in
// the file. describe blocks nest (describe → it); we recurse into a matched
// closure's body so inner it() closures are mined too. We do NOT descend into a
// `method_declaration` / `function_definition` — walk() already owns their CALLS
// edges (no double-emit).
func collectPestClosureBodies(root ts.Node, src []byte) []ts.Node {
	var out []ts.Node
	walkPestBlocks(root, src, &out)
	return out
}

func walkPestBlocks(n ts.Node, src []byte, out *[]ts.Node) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "method_declaration", "function_definition":
		// Named declarations already have owners from walk(); never double-mine.
		return
	case "function_call_expression":
		if isPestDSLCall(n, src) {
			if body := pestClosureBody(n); body != nil {
				*out = append(*out, body)
				// Recurse into the closure body to find nested it()/test()
				// closures inside a describe() block.
				for i := 0; i < int(body.ChildCount()); i++ {
					walkPestBlocks(body.Child(i), src, out)
				}
				return
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkPestBlocks(n.Child(i), src, out)
	}
}

// isPestDSLCall reports whether a function_call_expression invokes a bare Pest
// DSL function (it/test/describe/beforeEach/...). The callee must be a plain
// `name` leaf — a namespaced or variable callee is not a Pest global.
func isPestDSLCall(call ts.Node, src []byte) bool {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "name" {
		return false
	}
	name := string(src[fn.StartByte():fn.EndByte()])
	return pestBlockMethods[name]
}

// pestClosureBody returns the compound_statement / expression body of the
// anonymous closure or arrow function passed as an argument to a Pest DSL call,
// or nil when no closure argument is present (e.g. a `it('pending')` with no
// callback, or a higher-order chain). The closure is the
// anonymous_function_creation_expression / arrow_function inside the call's
// `arguments`.
func pestClosureBody(call ts.Node) ts.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		// `arguments` wraps each value in an `argument` node.
		var val ts.Node = arg
		if arg.Type() == "argument" && arg.NamedChildCount() > 0 {
			val = arg.NamedChild(0)
		}
		switch val.Type() {
		case "anonymous_function_creation_expression", "anonymous_function":
			if b := val.ChildByFieldName("body"); b != nil {
				return b
			}
			if b := findFirstChildOfType(val, "compound_statement"); b != nil {
				return b
			}
		case "arrow_function":
			// fn () => EXPR — the body is the expression after `=>`. There is no
			// `body` field consistently; the last named child is the body expr.
			if b := val.ChildByFieldName("body"); b != nil {
				return b
			}
			if n := val.NamedChildCount(); n > 0 {
				return val.NamedChild(int(n) - 1)
			}
		}
	}
	return nil
}

// isPHPTestFile reports whether path is a PHPUnit / Pest test file. Matches the
// coverage classifier's PHP test-file convention (a `*Test.php` / `*_test.php`
// file, or any `.php` under a `/tests/` directory — the Pest default layout).
func isPHPTestFile(path string) bool {
	slashed := "/" + filepath.ToSlash(strings.ToLower(path))
	if strings.Contains(slashed, "/tests/") || strings.Contains(slashed, "/test/") {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, "test.php") || strings.HasSuffix(base, "_test.php")
}

// phpTestScopeName derives a stable per-file name for the test-scope owner from
// the spec path: the base filename with `.php` stripped, suffixed with
// "::testScope" so it never collides with a production symbol.
func phpTestScopeName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	base = strings.TrimSuffix(base, ".php")
	return base + "::testScope"
}
