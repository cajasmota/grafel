package ruby

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitRubyTestScopeOwner emits a single SCOPE.Operation entity per RSpec spec
// file that owns every CALLS edge reachable from the spec's example / hook
// blocks (`it`/`specify`/`example`/`describe`/`context`/`before`/`after` etc.).
//
// Issue #4684 (the Ruby slice of epic #4615 / #4672). RSpec spec logic lives in
// anonymous `do ... end` callback blocks passed to `it`/`describe` — those
// blocks are NOT method declarations, so walk() (which only mines `method` /
// `singleton_method` bodies for CALLS) produced no owner for the
// `c.get_counts(...)` / `instance.action` calls inside them. The whole spec
// file therefore emitted zero CALLS edges, and ComputeCoverage saw every
// controller/handler reached only by a spec as untested — the exact symptom the
// TS/JS slice (#4680, javascript/tests.go::emitTestScopeOwner) fixed for
// anonymous `it()` arrow callbacks. Minitest's `def test_x` ARE named methods,
// already mined by walk(); only the RSpec anonymous-block case is RED, so this
// pass is gated to RSpec block-bearing spec files.
//
// Local-variable receiver typing (issue #4684 gap 1) is folded in here:
// withRubyLocalReceiverTypes scans each block body once for constructor
// bindings (`c = ProposalsController.new`, `instance = described_class.new`) so
// a subsequent `c.get_counts(...)` resolves to the dotted `ProposalsController.
// get_counts` target the resolver binds cross-file to the controller method —
// instead of an unresolvable bare `get_counts`. Mirrors the Python (#4681) and
// TS/JS (#4671) local-receiver typing.
//
// Scope discipline (mirrors #4680): only the example/hook block callbacks are
// mined. Calls inside named methods already have owners from walk(), so this
// pass never descends into a `method` / `singleton_method` declaration (no
// double-emit). Route-hit linkage (`get '/api/...'`) is handled separately by
// the RSpec custom extractor's e2e_route_calls path (#4371) and is unchanged.
//
// No-op for non-spec files and for spec files whose blocks resolve no CALLS.
func emitRubyTestScopeOwner(root ts.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if root == nil || !isRubySpecFile(file.Path) {
		return
	}
	// The class constant of the nearest enclosing `RSpec.describe X` —
	// `described_class.new` / an implicit `subject` types from it.
	bodies := collectRSpecBlockBodies(root, file.Content)
	if len(bodies) == 0 {
		return
	}
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, b := range bodies {
		for _, rel := range extractTestScopeCallRelationships(b.body, file.Content, b.describedClass) {
			if rel.ToID == "" || seen[rel.ToID] {
				continue
			}
			seen[rel.ToID] = true
			rels = append(rels, rel)
		}
	}
	// The test-scope owner is minted ONLY when the spec actually reaches a
	// production handler (a resolved CALLS edge). A shape-only assertion spec
	// that exercises no handler stays owner-less — the #4684 honest-exclusion
	// contract. The #4398 subject edge rides ALONG WITH that owner; it must not
	// be the sole reason an owner is created, so we gate it on CALLS being
	// present and append it after this check.
	if len(rels) == 0 {
		return
	}
	// #4398 — name-affinity TESTS edge to the subject class under test. Resolve
	// the subject from (in priority order) the spec's `described_class`
	// (top-level `describe Const`), then the spec-file-stem → class convention
	// (`user_spec.rb` → `User`). When a subject resolves, append a single TESTS
	// edge (`Class:<Subject>` stub the resolver binds to the in-repo class).
	// Honest: no edge when nothing resolves.
	if subj := resolveRSpecSubject(file.Path, bodies); subj != "" {
		rels = append(rels, types.RelationshipRecord{
			ToID: "Class:" + subj,
			Kind: string(types.RelationshipKindTests),
			Properties: map[string]string{
				"framework":    "rspec",
				"match_source": "spec_subject_affinity",
				"target_type":  subj,
			},
			Confidence: 0.9,
		})
	}
	name := rubyTestScopeName(file.Path)
	rec := types.EntityRecord{
		Name:          name,
		Kind:          "SCOPE.Operation",
		Subtype:       "test_scope",
		SourceFile:    file.Path,
		Language:      "ruby",
		StartLine:     1,
		EndLine:       1,
		Relationships: rels,
	}
	rec.Properties = map[string]string{
		"framework":   "rspec",
		"provenance":  "INFERRED_FROM_RSPEC_TEST_SCOPE",
		"test_scope":  "true",
		"description": "test scope " + name,
	}
	*out = append(*out, rec)
}

// resolveRSpecSubject resolves the class under test for an RSpec spec via name
// affinity (#4398). Priority:
//
//  1. `described_class` — the constant of the nearest `RSpec.describe Const`
//     (collectRSpecBlockBodies already threads this down per block); the
//     top-level describe's constant is the authoritative subject.
//  2. The spec-file-stem → class convention: `user_spec.rb` → `User`,
//     `order_service_spec.rb` → `OrderService` (snake_case stem, `_spec`
//     stripped, camelized). Used only when no `describe Const` was present —
//     a string-label `describe 'GET /users'` spec falls through to here.
//
// Returns "" when no subject resolves (honest exclusion — no spurious edge).
func resolveRSpecSubject(path string, bodies []rspecBlock) string {
	// (1) described_class from a `describe Const` — prefer the constant carried
	// by the OUTERMOST block (the first collected body threads the top-level
	// described class). Any non-empty describedClass is a high-confidence subject.
	for _, b := range bodies {
		if b.describedClass != "" {
			return rubyConstBaseName(b.describedClass)
		}
	}
	// (2) Spec-file-stem → class convention.
	return rubySpecStemToClass(path)
}

// rubyConstBaseName returns the final constant segment of a possibly
// `::`-qualified Ruby constant (`Admin::UsersController` → `UsersController`),
// so the `Class:<Subject>` stub matches the resolver's by-name index (which
// keys on the bare class name).
func rubyConstBaseName(c string) string {
	if i := strings.LastIndex(c, "::"); i >= 0 {
		return c[i+2:]
	}
	return c
}

// rubySpecStemToClass camelizes a spec file stem into the conventional class
// name under test: `user_spec.rb` → `User`, `order_service_spec.rb` →
// `OrderService`. Returns "" when the stem has no `_spec` suffix (so a non-spec
// path never fabricates a subject) or yields no token.
func rubySpecStemToClass(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	base = strings.TrimSuffix(base, ".rb")
	stem := strings.TrimSuffix(base, "_spec")
	if stem == base || stem == "" {
		return "" // no `_spec` suffix — not the spec-stem convention
	}
	return rubyCamelize(stem)
}

// rubyCamelize converts a snake_case identifier to PascalCase
// (`order_service` → `OrderService`). Empty segments are skipped.
func rubyCamelize(snake string) string {
	var b strings.Builder
	for _, part := range strings.Split(snake, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

// emitRubyMinitestSuite collapses a Minitest test case into a single
// test_suite entity per class, folding the `def test_*` example count to a
// property and emitting a name-affinity TESTS edge to the subject class under
// test (#4398).
//
// Minitest test cases are NAMED classes (`class UserTest < Minitest::Test`)
// whose `def test_x` examples ARE method declarations — so walk() already mines
// their bodies for CALLS edges (no anonymous-block orphan problem like RSpec).
// What was missing is (a) a one-per-class collapsed test_suite the coverage/
// dashboard surfaces recognise, and (b) the TESTS→subject edge crediting the
// class the suite exercises. This pass adds both, gated to test files that
// declare a Minitest/Test::Unit/ActiveSupport test case.
//
// Subject affinity: strip the conventional `Test` suffix from the test-case
// class name (`UserTest` → `User`, `OrderServiceTest` → `OrderService`), else
// fall back to the spec-file-stem convention. No `Test`-suffix and no stem
// match → no edge (honest exclusion).
//
// The suite Name is namespaced (`minitest_suite:<file>:<Class>`) so the
// resolver's by-name index never confuses it with the production class of the
// same base name and re-orphans it (the #4366 MergeWithCustom / #4343 lesson).
func emitRubyMinitestSuite(root ts.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if root == nil || !isRubyTestFile(file.Path) {
		return
	}
	for _, cls := range findAllNodes(root, "class") {
		super := childFieldText(cls, "superclass", file.Content)
		if !isMinitestSuperclass(super) {
			continue
		}
		className := childFieldText(cls, "name", file.Content)
		if className == "" {
			continue
		}
		testCount := countMinitestExamples(cls, file.Content)
		if testCount == 0 {
			continue // a test-case class with no `def test_*` exercises nothing
		}
		base := filepath.Base(filepath.ToSlash(file.Path))
		base = strings.TrimSuffix(base, ".rb")
		rec := types.EntityRecord{
			Name:       "minitest_suite:" + base + ":" + rubyConstBaseName(className),
			Kind:       "SCOPE.Operation",
			Subtype:    "test_suite",
			SourceFile: file.Path,
			Language:   "ruby",
			StartLine:  int(cls.StartPoint().Row) + 1,
			EndLine:    int(cls.EndPoint().Row) + 1,
			Properties: map[string]string{
				"framework":      "minitest",
				"test_framework": "minitest",
				"provenance":     "INFERRED_FROM_MINITEST_SUITE",
				"suite_label":    className,
				"test_count":     strconv.Itoa(testCount),
			},
		}
		if subj := resolveMinitestSubject(file.Path, className); subj != "" {
			rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
				ToID: "Class:" + subj,
				Kind: string(types.RelationshipKindTests),
				Properties: map[string]string{
					"framework":    "minitest",
					"match_source": "spec_subject_affinity",
					"target_type":  subj,
				},
				Confidence: 0.9,
			})
		}
		*out = append(*out, rec)
	}
}

// isMinitestSuperclass reports whether a class superclass marks a Minitest /
// Test::Unit / ActiveSupport test case. Tolerates `::`-qualified forms.
func isMinitestSuperclass(super string) bool {
	s := strings.TrimSpace(super)
	if s == "" {
		return false
	}
	switch rubyConstBaseName(s) {
	case "Test": // Minitest::Test
		return strings.Contains(s, "Minitest") || strings.Contains(s, "MiniTest")
	case "TestCase": // ActiveSupport::TestCase, Test::Unit::TestCase, Minitest::Test::Unit::TestCase
		return true
	case "Spec": // Minitest::Spec
		return strings.Contains(s, "Minitest") || strings.Contains(s, "MiniTest")
	}
	return false
}

// countMinitestExamples counts the `def test_*` method declarations directly in
// a Minitest test-case class body. Only methods whose name starts with `test_`
// (or is exactly `test`) are Minitest examples; helpers / `setup` / `teardown`
// are excluded from the count.
func countMinitestExamples(cls ts.Node, src []byte) int {
	n := 0
	for _, m := range findAllNodes(cls, "method") {
		name := childFieldText(m, "name", src)
		if name == "test" || strings.HasPrefix(name, "test_") {
			n++
		}
	}
	return n
}

// resolveMinitestSubject resolves the subject class a Minitest case tests via
// name affinity (#4398): strip the conventional `Test` suffix from the test-case
// class name (`UserTest` → `User`), else fall back to the spec-file-stem
// convention. Returns "" when neither yields a name (honest exclusion).
func resolveMinitestSubject(path, className string) string {
	base := rubyConstBaseName(className)
	if strings.HasSuffix(base, "Test") && len(base) > len("Test") {
		return strings.TrimSuffix(base, "Test")
	}
	// Minitest's file convention is `*_test.rb` (not `*_spec.rb`); reuse the
	// camelize convention on the `_test`-stripped stem.
	return rubyTestStemToClass(path)
}

// rubyTestStemToClass camelizes a `*_test.rb` file stem into the conventional
// class under test (`user_test.rb` → `User`). Returns "" when the stem has no
// `_test` suffix.
func rubyTestStemToClass(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	base = strings.TrimSuffix(base, ".rb")
	stem := strings.TrimSuffix(base, "_test")
	if stem == base || stem == "" {
		return ""
	}
	return rubyCamelize(stem)
}

// isRubyTestFile reports whether path is any Ruby test file — an RSpec spec
// (`*_spec.rb` / under `/spec/`) OR a Minitest/Test::Unit test (`*_test.rb` /
// `test_*.rb` / under `/test/`). Broader than isRubySpecFile so the Minitest
// collapse fires on the `test/` tree.
func isRubyTestFile(path string) bool {
	if isRubySpecFile(path) {
		return true
	}
	slashed := "/" + filepath.ToSlash(strings.ToLower(path))
	if strings.Contains(slashed, "/test/") || strings.Contains(slashed, "/tests/") {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, "_test.rb") || strings.HasPrefix(base, "test_")
}

// rspecBlock is an RSpec example/hook block body paired with the class constant
// of the nearest enclosing `describe`/`context` (for `described_class` typing).
type rspecBlock struct {
	body           ts.Node
	describedClass string
}

// rspecBlockMethods is the set of RSpec DSL block methods whose `do ... end`
// callback hosts spec logic that may call into production code. Mirrors
// javascript/tests.go::testBlockCallNames.
var rspecBlockMethods = map[string]bool{
	"it": true, "specify": true, "example": true, "scenario": true,
	"describe": true, "context": true, "feature": true,
	"before": true, "after": true, "around": true,
}

// collectRSpecBlockBodies returns the `do_block` body of every RSpec DSL block
// (it/specify/describe/context/before/after/...) in the file, each paired with
// the class constant of the nearest enclosing `describe`/`context` so
// `described_class` can be typed. Blocks nest (describe → it); we recurse into a
// matched block's body so inner `it` bodies are mined too, threading the
// described-class down. We do NOT descend into `method`/`singleton_method`
// declarations — walk() already owns their CALLS edges.
func collectRSpecBlockBodies(root ts.Node, src []byte) []rspecBlock {
	var out []rspecBlock
	walkRSpecBlocks(root, src, "", &out)
	return out
}

func walkRSpecBlocks(n ts.Node, src []byte, describedClass string, out *[]rspecBlock) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "method", "singleton_method":
		// Named methods already have owners from walk(); never double-mine.
		return
	case "call":
		if mname := rspecBlockMethodName(n, src); mname != "" {
			body := rubyDoBlockBody(n)
			if body != nil {
				dc := describedClass
				// `describe`/`context` with a constant first argument sets the
				// described class for any `described_class` inside.
				if mname == "describe" || mname == "context" || mname == "feature" {
					if c := rspecConstantArg(n, src); c != "" {
						dc = c
					}
				}
				*out = append(*out, rspecBlock{body: body, describedClass: dc})
				// Recurse into the block body to find nested it()/before() blocks,
				// carrying the (possibly updated) described class.
				for i := 0; i < int(body.ChildCount()); i++ {
					walkRSpecBlocks(body.Child(i), src, dc, out)
				}
				return
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkRSpecBlocks(n.Child(i), src, describedClass, out)
	}
}

// rspecBlockMethodName returns the RSpec DSL block method name (it/describe/...)
// of a `call` node when it is one of rspecBlockMethods, or "" otherwise. Handles
// both the bare form (`it 'x' do`) and the `RSpec.describe X do` receiver form.
func rspecBlockMethodName(call ts.Node, src []byte) string {
	m := call.ChildByFieldName("method")
	if m == nil {
		// Bare `it 'x' do` parses with the method as the first identifier child.
		for i := 0; i < int(call.ChildCount()); i++ {
			ch := call.Child(i)
			if ch.Type() == "identifier" {
				name := string(src[ch.StartByte():ch.EndByte()])
				if rspecBlockMethods[name] {
					return name
				}
				return ""
			}
		}
		return ""
	}
	name := string(src[m.StartByte():m.EndByte()])
	if rspecBlockMethods[name] {
		return name
	}
	return ""
}

// rubyDoBlockBody returns the body_statement of a call's trailing `do ... end`
// block (or `{ ... }` brace block), or nil when the call has no block.
func rubyDoBlockBody(call ts.Node) ts.Node {
	for i := 0; i < int(call.ChildCount()); i++ {
		ch := call.Child(i)
		if ch.Type() == "do_block" || ch.Type() == "block" {
			if b := ch.ChildByFieldName("body"); b != nil {
				return b
			}
			for j := 0; j < int(ch.ChildCount()); j++ {
				if ch.Child(j).Type() == "body_statement" {
					return ch.Child(j)
				}
			}
			return ch
		}
	}
	return nil
}

// rspecConstantArg returns the first constant argument of a `describe X`/
// `context X` call (e.g. `RSpec.describe ProposalsController` → "ProposalsController"),
// or "" when the first argument is a string label / not a constant.
func rspecConstantArg(call ts.Node, src []byte) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		for i := 0; i < int(call.ChildCount()); i++ {
			if call.Child(i).Type() == "argument_list" {
				args = call.Child(i)
				break
			}
		}
	}
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		switch a.Type() {
		case "constant", "scope_resolution":
			return string(src[a.StartByte():a.EndByte()])
		case "string":
			return ""
		}
	}
	return ""
}

// extractTestScopeCallRelationships mines a single RSpec block body for CALLS
// edges, typing local-variable receivers from constructor bindings (issue
// #4684). describedClass is the class constant of the nearest enclosing
// `describe`, used to type `described_class.new` locals.
//
// Only receiver-typed method calls (`local.method` where `local` is a typed
// constructor binding) and explicit-constant calls (`Const.method`) yield a
// CALLS edge here; bare unresolvable calls (DSL matchers like `expect`,
// `have_http_status`) are dropped — the test-scope owner exists to credit the
// production handler the spec exercises, not to enumerate matcher noise.
func extractTestScopeCallRelationships(body ts.Node, src []byte, describedClass string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	locals := rubyLocalReceiverTypes(body, src, describedClass)
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, call := range findAllNodes(body, "call") {
		target := rubyTypedCallTarget(call, src, locals)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		line := strconv.Itoa(int(call.StartPoint().Row) + 1)
		rels = append(rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: map[string]string{"line": line},
		})
	}
	return rels
}

// rubyLocalReceiverTypes scans a block body for local-variable constructor
// bindings and returns a `localName → ClassName` map so a subsequent
// `localName.method(...)` types to `ClassName.method` (issue #4684, mirrors the
// Python #4681 / TS/JS #4671 local-receiver typing).
//
// Recognised binding shapes (conservative — a guess risks mis-binding):
//
//	c        = ProposalsController.new      # explicit constant constructor
//	instance = described_class.new          # implicit subject construction
//
// The RHS must be a `.new` call whose receiver is a PascalCase constant (or the
// `described_class` keyword, typed from the enclosing describe). A factory
// helper (`x = make_thing()`), a namespaced/unknown receiver, or a non-`.new`
// RHS yields no entry — honest exclusion. First binding wins on re-assign.
func rubyLocalReceiverTypes(body ts.Node, src []byte, describedClass string) map[string]string {
	var locals map[string]string
	for _, a := range findAllNodes(body, "assignment") {
		lhs := a.ChildByFieldName("left")
		if lhs == nil || lhs.Type() != "identifier" {
			continue
		}
		rhs := a.ChildByFieldName("right")
		if rhs == nil || rhs.Type() != "call" {
			continue
		}
		cls := rubyConstructedClass(rhs, src, describedClass)
		if cls == "" {
			continue
		}
		name := string(src[lhs.StartByte():lhs.EndByte()])
		if name == "" {
			continue
		}
		if locals == nil {
			locals = map[string]string{}
		}
		if _, ok := locals[name]; !ok {
			locals[name] = cls
		}
	}
	return locals
}

// rubyConstructedClass returns the class name a `.new` constructor call yields,
// or "" when the RHS is not a recognised user-class construction. Handles
// `ProposalsController.new` (PascalCase constant receiver) and
// `described_class.new` (typed from the enclosing describe constant).
func rubyConstructedClass(call ts.Node, src []byte, describedClass string) string {
	method := call.ChildByFieldName("method")
	if method == nil || string(src[method.StartByte():method.EndByte()]) != "new" {
		return ""
	}
	recv := call.ChildByFieldName("receiver")
	if recv == nil {
		return ""
	}
	switch recv.Type() {
	case "constant", "scope_resolution":
		name := string(src[recv.StartByte():recv.EndByte()])
		if isRubyPascalConstant(name) {
			return name
		}
	case "identifier":
		if string(src[recv.StartByte():recv.EndByte()]) == "described_class" && describedClass != "" {
			return describedClass
		}
	}
	return ""
}

// rubyTypedCallTarget resolves a `call` node to a dotted `Class.method` CALLS
// target using the local-receiver type map, or "" when the receiver is not a
// typed local / known constant. A receiver that is a PascalCase constant
// (`ProposalsController.create`) types directly. The `.new` constructor itself
// and DSL/bare calls without a typed receiver are dropped.
func rubyTypedCallTarget(call ts.Node, src []byte, locals map[string]string) string {
	method := call.ChildByFieldName("method")
	if method == nil {
		return ""
	}
	mname := string(src[method.StartByte():method.EndByte()])
	if mname == "" || mname == "new" {
		return ""
	}
	recv := call.ChildByFieldName("receiver")
	if recv == nil {
		return ""
	}
	switch recv.Type() {
	case "identifier":
		rname := string(src[recv.StartByte():recv.EndByte()])
		if cls := locals[rname]; cls != "" {
			return cls + "." + mname
		}
	case "constant", "scope_resolution":
		rname := string(src[recv.StartByte():recv.EndByte()])
		if isRubyPascalConstant(rname) {
			return rname + "." + mname
		}
	}
	return ""
}

// isRubyPascalConstant reports whether name is a Ruby constant beginning with an
// uppercase letter (a class/module name), tolerating `::`-qualified forms.
func isRubyPascalConstant(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}

// isRubySpecFile reports whether path is an RSpec spec file (`*_spec.rb`, or any
// `.rb` under a `/spec/` directory). Matches the coverage classifier's Ruby
// test-file convention (internal/graph/coverage.go::isTestFile).
func isRubySpecFile(path string) bool {
	slashed := "/" + filepath.ToSlash(strings.ToLower(path))
	if strings.Contains(slashed, "/spec/") {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, "_spec.rb")
}

// rubyTestScopeName derives a stable per-file name for the test-scope owner from
// the spec path: the base filename with `.rb` stripped, suffixed with
// "::testScope" so it never collides with a production symbol.
func rubyTestScopeName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	base = strings.TrimSuffix(base, ".rb")
	return base + "::testScope"
}
