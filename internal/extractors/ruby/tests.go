package ruby

import (
	"path/filepath"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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
func emitRubyTestScopeOwner(root *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
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
	if len(rels) == 0 {
		return
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

// rspecBlock is an RSpec example/hook block body paired with the class constant
// of the nearest enclosing `describe`/`context` (for `described_class` typing).
type rspecBlock struct {
	body           *sitter.Node
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
func collectRSpecBlockBodies(root *sitter.Node, src []byte) []rspecBlock {
	var out []rspecBlock
	walkRSpecBlocks(root, src, "", &out)
	return out
}

func walkRSpecBlocks(n *sitter.Node, src []byte, describedClass string, out *[]rspecBlock) {
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
func rspecBlockMethodName(call *sitter.Node, src []byte) string {
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
func rubyDoBlockBody(call *sitter.Node) *sitter.Node {
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
func rspecConstantArg(call *sitter.Node, src []byte) string {
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
func extractTestScopeCallRelationships(body *sitter.Node, src []byte, describedClass string) []types.RelationshipRecord {
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
func rubyLocalReceiverTypes(body *sitter.Node, src []byte, describedClass string) map[string]string {
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
func rubyConstructedClass(call *sitter.Node, src []byte, describedClass string) string {
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
func rubyTypedCallTarget(call *sitter.Node, src []byte, locals map[string]string) string {
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
