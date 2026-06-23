// Package ruby implements the tree-sitter–based extractor for Ruby source files.
//
// Extracted entities:
//   - class            → Kind="SCOPE.Component", Subtype="class"
//   - module           → Kind="SCOPE.Component", Subtype="module"
//   - method           → Kind="SCOPE.Operation", Subtype="method"
//   - singleton_method → Kind="SCOPE.Operation", Subtype="singleton_method"
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package ruby

import (
	"context"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/txscope"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("ruby", &Extractor{})
}

// Extractor implements extractor.Extractor for Ruby.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "ruby" }

// Extract walks the tree-sitter CST and returns entity records for the Ruby file.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.TSTree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
	// cross-repo import linker (#566) can map IMPORTS edges back to the
	// originating repo via the resolver's byName index. Generalises the
	// JS/TS fix from #570/#575.
	entities = append(entities, extractor.FileEntity(file))
	root := file.TSTree.RootNode()
	walk(root, file, &entities)
	// Issue #3641 (epic #3625) — config-key consumption edges
	// (ENV['X'] / ENV.fetch('X')) → shared SCOPE.Config config_key nodes.
	emitConfigConsumerEdges(root, file.Content, &entities)
	// View-layer topology (epic #3628) — RENDERS edges from Rails controller
	// actions to a shared SCOPE.Template node for explicit `render 'path'` /
	// `render template:/partial:` shapes (symbol / implicit-convention renders
	// and dynamic names are dropped).
	emitTemplateRenderEdges(root, file.Content, &entities)
	// Localization topology (child of epic #3628) — USES_TRANSLATION edges from
	// methods to a shared SCOPE.TranslationKey node for Rails `I18n.t('k')` /
	// relative `t('.k')` shapes (dynamic keys + ambiguous bare `t('plain')`
	// dropped).
	emitTranslationKeyEdges(root, file.Content, &entities)
	// Error-flow topology (epic #3628) — THROWS / CATCHES edges from methods to a
	// shared SCOPE.ExceptionType node for typed `raise NotFoundError`,
	// `rescue NotFoundError => e` (incl. method-level + multi-class rescue), and
	// Rails `rescue_from RecordNotFound, with: :handler`. Bare rescue catch-all,
	// string raise, and bare re-raise are dropped (precision-first). Mirrors the
	// flagship convergence-node shape (internal/extractor/exception_flow.go).
	emitExceptionFlowEdges(root, file.Content, &entities)
	// Issue #4684 (epic #4615) — RSpec test-scope owner. RSpec example/hook
	// blocks (`it`/`describe`/`before` …) are anonymous `do ... end` callbacks,
	// not method declarations, so walk() never mined their CALLS edges. Emit one
	// SCOPE.Operation per spec file owning the receiver-typed CALLS edges to the
	// production handlers the spec exercises, so ComputeCoverage credits them
	// (test→CALLS→handler→endpoint). Mirrors javascript/tests.go (#4680). No-op
	// for non-spec files. Route-hit linkage (`get '/api/...'`) stays in the
	// RSpec custom extractor's e2e_route_calls path (#4371).
	emitRubyTestScopeOwner(root, file, &entities)
	// Issue #4398 (epic #4615) — Minitest test-case collapse. A
	// `class UserTest < Minitest::Test` with `def test_*` examples is collapsed
	// to one test_suite per class (example count folded to a property) plus a
	// name-affinity TESTS edge to the subject class under test (`UserTest` →
	// `User`). Mirrors the JS/Go/Java/Python collapse (#4343/#4358/#4359/#4357).
	// No-op for non-test files and classes that are not Minitest test cases.
	emitRubyMinitestSuite(root, file, &entities)
	// Issue #4854 — in-file superclass EXTENDS for field-membership recursion.
	entities = attachRubyExtends(entities)
	// Issue #90 — tag every embedded relationship with the source language
	// so the resolver picks the Ruby dynamic-pattern catalog.
	extractor.TagRelationshipsLanguage(entities, "ruby")
	extractor.TagEntitiesLanguage(entities, "ruby")
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
//
// PORT-2-FIX-2-ALL (#41): class/module declarations attach a CONTAINS edge
// per method declared inside the body, every method body emits CALLS edges
// with stub to_id, and top-level `require`/`require_relative`/`load` calls
// emit IMPORTS module entities mirroring the Python extractor's shape.
func walk(node ts.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class", "module":
		subtype := node.Type() // "class" or "module"
		rec, ok := buildComponent(node, file, subtype)
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, out)
			}
			return
		}
		classIdx := len(*out)
		*out = append(*out, rec)
		body := node.ChildByFieldName("body")
		if body == nil {
			// Tree-sitter ruby exposes the class body as the unnamed `body_statement`
			// child rather than a labelled field; fall back to scanning children.
			for i := range node.ChildCount() {
				ch := node.Child(int(i))
				if ch.Type() == "body_statement" {
					body = ch
					break
				}
			}
		}
		if body != nil {
			// Rails ActiveRecord `enum status: {...}` declarations → value-set
			// SCOPE.Enum nodes (data-model, epic #3628).
			for i := range body.ChildCount() {
				if vs, vok := buildRailsEnumValueSet(body.Child(int(i)), file); vok {
					*out = append(*out, vs)
				}
			}
			// #4427 — class/module-body constant COLLECTIONS
			// (`KINDS = { a: 1 }.freeze`, `STATUSES = %w[..]`) → per-constant
			// value-set SCOPE.Enum nodes are emitted by the recursive
			// `case "assignment"` branch when walk() descends into the body
			// below (avoids a double-emit).
			//
			// #4427 — a `module Roles; ADMIN='admin'; USER='user'; end` group
			// of scalar constants → one value-set named after the module/class.
			// Constants bound to collections are excluded (they get their own
			// node above).
			if gs, gok := buildModuleConstGroupValueSet(rec.Name, body, file); gok {
				*out = append(*out, gs)
			}
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				// Issue #4684 (epic #4615) — class-qualify each method with a
				// QualifiedName ("<Class>.<method>") WITHOUT touching its bare
				// Name (existing CONTAINS structural-refs and Rails route
				// resolution rely on the bare Name). The resolver indexes
				// QualifiedName globally (byQualifiedName, #100), so a
				// receiver-typed CALLS target like `ProposalsController.
				// get_counts` — emitted by the RSpec test-scope owner once a
				// local is typed from `ProposalsController.new` — resolves
				// cross-file to this method. Mirrors the dotted Name Python
				// (#4681) / Java (#4682) carry; Ruby keeps the bare Name and
				// adds the qualifier as metadata only. First (innermost) class
				// wins: a nested class stamps its own methods before this outer
				// loop runs, so we never overwrite an already-qualified child.
				if child.QualifiedName == "" {
					child.QualifiedName = rec.Name + "." + child.Name
				}
				// Issue #140 — bare-name CONTAINS targets are 100%
				// ambiguous in Rails apps where dozens of controllers
				// share the same `create`/`destroy`/`index` methods.
				// Emit a structural-ref (Format A) keyed on the source
				// file so the resolver disambiguates by location;
				// each Rails class is its own file by convention so
				// the file-local method name is unique.
				toID := extractor.BuildOperationStructuralRef("ruby", file.Path, child.Name)
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
			// Issue #4854 — general field membership: one SCOPE.Schema/field per
			// attr_accessor/attr_reader/attr_writer symbol + a class→field
			// CONTAINS edge so a plain Ruby data class has field children (these
			// are the only declaratively-present members; Ruby has no static
			// field declarations otherwise).
			emitRubyAttrFields(out, classIdx, body, file.Content, rec.Name, file.Path)
		}
		// Issue #4854 — stash the in-file superclass for the EXTENDS post-pass
		// so the shape walker can recurse into inherited attr fields.
		if sc := classSuperclass(node, file.Content); sc != "" {
			if (*out)[classIdx].Metadata == nil {
				(*out)[classIdx].Metadata = map[string]interface{}{}
			}
			(*out)[classIdx].Metadata["base_candidate"] = sc
		}
		return

	case "method":
		if rec, ok := buildMethod(node, file, "function"); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, rec.Name)...)
			rec.Properties = stampRubyTx(node, file, rec.Properties)
			*out = append(*out, rec)
		}
		return

	case "singleton_method":
		if rec, ok := buildMethod(node, file, "function"); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, rec.Name)...)
			rec.Properties = stampRubyTx(node, file, rec.Properties)
			*out = append(*out, rec)
		}
		return

	case "call":
		if rec, ok := buildRequireImport(node, file); ok {
			*out = append(*out, rec)
		}

	case "assignment":
		// #4427 — top-level / nested constant COLLECTIONS outside any class or
		// module body (`PERMISSION_PAGES = { ... }.freeze` at file scope). The
		// class/module branch handles body-scoped constants directly; this
		// catches the file-scope and arbitrarily-nested cases. Recursion
		// continues below so we never miss a deeper assignment.
		if vs, vok := buildConstCollectionValueSet(node, file); vok {
			*out = append(*out, vs)
		}
		// Issue #4854 — `Const = Struct.new(:a, :b)` / `Data.define(:a, :b)`
		// synthesises a SCOPE.Component data class + one field member per
		// declared accessor, with class→field CONTAINS edges.
		if sd := emitRubyStructDefine(node, file); len(sd) > 0 {
			*out = append(*out, sd...)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// invocation descendant of body. Tree-sitter-ruby distinguishes:
//
//	call       — receiver.method(args) form, "method" field carries the name
//	command    — bare method args  (e.g. `puts "x"`), no receiver
//	identifier — bare invocation w/o args (e.g. `helper`) — appears as a
//	             standalone identifier statement inside body_statement
//
// All three shapes resolve to a bare callee name; FromID is left empty so
// buildDocument substitutes the caller's entity ID at emit time. Self-recursion
// is dropped.
func extractCallRelationships(body ts.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	seen := make(map[string]bool)
	var rels []types.RelationshipRecord
	// addAt appends a CALLS edge with a 1-based line number sourced from the
	// tree-sitter node that represents the call site.
	addAt := func(target string, callNode ts.Node) {
		if target == "" || target == callerName {
			return
		}
		if seen[target] {
			return
		}
		seen[target] = true
		// Line is 1-based: tree-sitter StartPoint().Row is 0-based.
		callLine := strconv.Itoa(int(callNode.StartPoint().Row) + 1)
		rels = append(rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: map[string]string{"line": callLine},
		})
	}
	// Pass 1: explicit call / command / method_call / yield / super.
	for _, n := range findAllNodes(body, "call", "command", "method_call") {
		addAt(rubyCallTarget(n, src), n)
	}
	// Pass 2: bare identifier statements inside body_statement / then / else
	// blocks. These are method invocations like `helper` with no args.
	for _, ident := range findAllNodes(body, "identifier") {
		parent := ident.Parent()
		if parent == nil {
			continue
		}
		pt := parent.Type()
		if pt != "body_statement" && pt != "then" && pt != "else" && pt != "begin" && pt != "ensure" {
			continue
		}
		addAt(string(src[ident.StartByte():ident.EndByte()]), ident)
	}
	return rels
}

// rubyCallTarget resolves the callee identifier from a Ruby call node.
// Ruby's tree-sitter grammar uses field names "method" (the called name)
// and "receiver" (optional left-hand side). Falls back to the first
// identifier child for older grammar variants.
func rubyCallTarget(call ts.Node, src []byte) string {
	if m := call.ChildByFieldName("method"); m != nil {
		t := m.Type()
		if t == "identifier" || t == "constant" || t == "operator" {
			return string(src[m.StartByte():m.EndByte()])
		}
	}
	// command: command_call has no `method` field — first identifier child is the name.
	for i := 0; i < int(call.ChildCount()); i++ {
		ch := call.Child(i)
		if ch.Type() == "identifier" || ch.Type() == "constant" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	return ""
}

// stampRubyTx adds transaction-boundary properties (#3628) to a method entity
// when an ActiveRecord `Model.transaction do ... end` / `Model.transaction { }`
// block is lexically present in the method's source span. No transitive
// propagation — only the method where `transaction do` appears is stamped.
func stampRubyTx(node ts.Node, file extractor.FileInput, props map[string]string) map[string]string {
	src := string(file.Content[node.StartByte():node.EndByte()])
	return txscope.DetectRuby(src).Apply(props)
}

// buildRequireImport emits a SCOPE.Component module entity with a single
// IMPORTS relationship for top-level require / require_relative / load calls.
// Returns (_, false) for any other call node.
func buildRequireImport(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// Only consider call nodes whose method identifier is one of the loaders.
	method := node.ChildByFieldName("method")
	if method == nil {
		return types.EntityRecord{}, false
	}
	mname := string(file.Content[method.StartByte():method.EndByte()])
	switch mname {
	case "require", "require_relative", "load", "autoload":
	default:
		return types.EntityRecord{}, false
	}
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return types.EntityRecord{}, false
	}
	// First string argument literal.
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg.Type() != "string" {
			continue
		}
		raw := strings.TrimSpace(string(file.Content[arg.StartByte():arg.EndByte()]))
		raw = strings.Trim(raw, "\"'")
		if raw == "" {
			continue
		}
		props := map[string]string{"require_kind": mname}
		// #4783 — stamp the `imported_name`/`local_name` contract so the
		// per-symbol external-node synthesis (#4515) can mint a stable
		// `ext:<gem>:<Const>` node. Only meaningful for gem requires (NOT
		// `require_relative`/`load`, which are intra-project file loads).
		// A required gem conventionally exposes a top-level constant that is the
		// CamelCased leaf of its require path (`require 'active_record'` → the
		// `ActiveRecord` constant; `require 'json'` → `JSON`-by-convention via
		// `Json`). `autoload :Const, 'path'` carries the symbol explicitly and is
		// handled below.
		if mname == "require" || mname == "autoload" {
			if c := rubyRequireConstant(node, file, mname, raw); c != "" {
				props["imported_name"] = c
				props["local_name"] = c
			}
		}
		return types.EntityRecord{
			Name:       raw,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: file.Path,
			Language:   "ruby",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     file.Path,
					ToID:       raw,
					Kind:       "IMPORTS",
					Properties: props,
				},
			},
		}, true
	}
	return types.EntityRecord{}, false
}

// rubyRequireConstant derives the top-level constant a require/autoload binds,
// where statically recoverable (#4783):
//
//   - `autoload :Foo, 'foo/bar'`  → "Foo" (explicit symbol — authoritative).
//   - `require 'active_record'`   → "ActiveRecord" (CamelCased leaf — the Ruby
//     gem convention that the require's leaf path segment names the exposed
//     constant).
//
// Returns "" when no constant is recoverable (e.g. a leaf that isn't a legal
// constant stem). The synth layer keys the per-symbol node by this name; an
// over-eager guess merely mints an extra node, so we stay conservative.
func rubyRequireConstant(node ts.Node, file extractor.FileInput, mname, raw string) string {
	if mname == "autoload" {
		// `autoload :Const, 'path'` — first argument is the explicit symbol.
		if args := node.ChildByFieldName("arguments"); args != nil {
			for i := 0; i < int(args.NamedChildCount()); i++ {
				a := args.NamedChild(i)
				if a.Type() == "simple_symbol" {
					sym := strings.TrimPrefix(string(file.Content[a.StartByte():a.EndByte()]), ":")
					sym = strings.TrimSpace(sym)
					if rubyIsConstantStem(sym) {
						return sym
					}
				}
			}
		}
		return ""
	}
	// require: CamelCase the leaf path segment.
	leaf := raw
	if i := strings.LastIndexByte(leaf, '/'); i >= 0 {
		leaf = leaf[i+1:]
	}
	return rubyCamelizeConstant(leaf)
}

// rubyIsConstantStem reports whether s is a legal Ruby constant name
// (begins with an upper-case letter, contains only word characters).
func rubyIsConstantStem(s string) bool {
	if s == "" || !(s[0] >= 'A' && s[0] <= 'Z') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// rubyCamelizeConstant converts a snake/lower require leaf into the conventional
// Ruby constant name (`active_record` → "ActiveRecord", `json` → "Json"),
// returning "" when the leaf has no constant-able characters.
func rubyCamelizeConstant(leaf string) string {
	leaf = strings.TrimSpace(leaf)
	if leaf == "" {
		return ""
	}
	var b strings.Builder
	upNext := true
	for i := 0; i < len(leaf); i++ {
		c := leaf[i]
		switch {
		case c == '_' || c == '-':
			upNext = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			if upNext {
				if c >= 'a' && c <= 'z' {
					c -= 32
				}
				upNext = false
			}
			b.WriteByte(c)
		case c >= '0' && c <= '9':
			if b.Len() == 0 {
				return "" // constants cannot start with a digit
			}
			b.WriteByte(c)
		default:
			// Non-identifier char (e.g. a leftover extension dot) — stop.
		}
	}
	out := b.String()
	if !rubyIsConstantStem(out) {
		return ""
	}
	return out
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
func findAllNodes(root ts.Node, kinds ...string) []ts.Node {
	if root == nil {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []ts.Node
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if set[n.Type()] {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// buildComponent creates a Component entity for class/module definitions.
// Rails-specific framework labelling is applied via tagRails:
// controllers, models, migrations and routes get framework="rails" plus
// a kind discriminator in Properties.
func buildComponent(node ts.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	rec := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "ruby",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}
	tagRails(&rec, node, file.Content, file.Path)
	return rec, true
}

// buildMethod creates an Operation entity for method definitions.
func buildMethod(node ts.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	sig := buildMethodSignature(node, file.Content)
	// Python adds "()" to Ruby method signatures for parity
	if !strings.Contains(sig, "(") {
		sig = sig + "()"
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "ruby",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
	}, true
}

// childFieldText extracts the text of a named child field.
func childFieldText(node ts.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildMethodSignature builds a def signature (first line).
func buildMethodSignature(node ts.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature for class/module.
func buildClassSignature(node ts.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return name
}
