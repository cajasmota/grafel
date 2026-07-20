package mcp

// mro_mixin_trait_3840_test.go — value-asserting coverage for Ruby mixin
// (`include`/`prepend`/`extend`) and PHP trait (`use`) member resolution
// (#3840, epic #3829 MRO T8).
//
// T7 (#3987) generalised the MRO walk to follow IMPLEMENTS with a base_name FQN
// and resolve a member to its DEFINING BODY cross-file, requiring a REAL body
// (safe-by-construction). T8 emits Ruby-mixin / PHP-trait edges as IMPLEMENTS
// (kind=ruby_mixin / php_trait), so the SAME walk promotes a module/trait
// method into the including class. These tests model the post-index graph and
// assert the inherited stub resolves to the SPECIFIC module/trait body in the
// OTHER file — not len>0.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// rubyMixinDoc models `module Greet; def hello; ...; end; end` in greet.rb and
// `class User; include Greet; end` in user.rb, where User.hello is a bodyless
// inherited stub (User never declares hello). Greet.hello calls a helper, the
// real promoted-through edge.
func rubyMixinDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "mod", Name: "Greet", QualifiedName: "Greet",
				Kind: "SCOPE.Component", Subtype: "module", SourceFile: "greet.rb",
				StartLine: 1, EndLine: 5, Language: "ruby"},
			{ID: "mod_hello", Name: "Greet.hello", QualifiedName: "Greet.hello",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "greet.rb",
				StartLine: 2, EndLine: 4, Language: "ruby"},
			{ID: "mod_fmt", Name: "Greet.format", QualifiedName: "Greet.format",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "greet.rb",
				StartLine: 6, EndLine: 7, Language: "ruby"},
			{ID: "cls", Name: "User", QualifiedName: "User",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "user.rb",
				StartLine: 1, EndLine: 3, Language: "ruby"},
			// Bodyless inherited stub: User mixes in Greet#hello.
			{ID: "cls_hello", Name: "User.hello", QualifiedName: "User.hello",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "user.rb",
				StartLine: 0, EndLine: 0, Language: "ruby", Signature: "def hello"},
		},
		Relationships: []graph.Relationship{
			// include Greet -> IMPLEMENTS(kind=ruby_mixin) with base_name.
			graph.Relationship{ID: "e1", FromID: "cls", ToID: "mod", Kind: "IMPLEMENTS"}.WithProperties(map[string]string{"language": "ruby", "kind": "ruby_mixin", "mixin_op": "include", "base_name": "Greet"}),
			// The module method calls format — the real edge to follow through.
			graph.Relationship{ID: "e2", FromID: "mod_hello", ToID: "mod_fmt", Kind: "CALLS"}.WithProperties(map[string]string{"language": "ruby"}),
		},
	}
}

// TestRubyInclude_ResolvesToModuleBody — User#hello (a bodyless include stub)
// resolves to Greet#hello's real body, cross-file (greet.rb), via the
// IMPLEMENTS(ruby_mixin) walk.
func TestRubyInclude_ResolvesToModuleBody(t *testing.T) {
	srv := newTestServer(t, rubyMixinDoc())
	out := callInspect(t, srv, "cls_hello")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for Ruby mixin method, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != true {
		t.Fatalf("expected resolved=true for Ruby include, got %v (note=%v)", inh["resolved"], inh["note"])
	}
	if dc, _ := inh["defining_class"].(string); !strings.Contains(dc, "Greet") {
		t.Errorf("expected defining_class Greet, got %q", dc)
	}
	if rf, _ := inh["resolved_from"].(string); rf != "extends_in_repo" {
		t.Errorf("expected resolved_from extends_in_repo, got %q", rf)
	}
	if df, _ := inh["defining_file"].(string); df != "greet.rb" {
		t.Errorf("expected defining_file greet.rb (cross-file mixin), got %q", df)
	}
}

// TestRubyInclude_TraversalReachesModuleCallee — the bodyless stub hops via
// INHERITS to Greet.hello and surfaces its real callee Greet.format.
func TestRubyInclude_TraversalReachesModuleCallee(t *testing.T) {
	srv := newTestServer(t, rubyMixinDoc())
	out := callNeighbors3834(t, srv, "cls_hello", "out")
	names := calleeNames3834(t, out)
	if !containsString(names, "Greet.hello") {
		t.Fatalf("expected mixin stub to reach defining Greet.hello via INHERITS, got: %v", names)
	}
	if !containsString(names, "Greet.format") {
		t.Errorf("expected to reach module callee Greet.format THROUGH the body, got: %v", names)
	}
}

// phpTraitDoc models `trait Audit { public function log(){...} }` in Audit.php
// and `class Order { use Audit; }` in Order.php, where Order.log is a bodyless
// inherited stub. Audit.log calls a helper, the real promoted-through edge.
func phpTraitDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "trait", Name: "Audit", QualifiedName: "Audit",
				Kind: "SCOPE.Component", Subtype: "trait", SourceFile: "Audit.php",
				StartLine: 1, EndLine: 6, Language: "php"},
			{ID: "trait_log", Name: "Audit.log", QualifiedName: "Audit.log",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Audit.php",
				StartLine: 2, EndLine: 5, Language: "php"},
			{ID: "trait_persist", Name: "Audit.persist", QualifiedName: "Audit.persist",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Audit.php",
				StartLine: 7, EndLine: 8, Language: "php"},
			{ID: "cls", Name: "Order", QualifiedName: "Order",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "Order.php",
				StartLine: 1, EndLine: 3, Language: "php"},
			// Bodyless inherited stub: Order uses Audit::log.
			{ID: "cls_log", Name: "Order.log", QualifiedName: "Order.log",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Order.php",
				StartLine: 0, EndLine: 0, Language: "php", Signature: "public function log()"},
		},
		Relationships: []graph.Relationship{
			// use Audit -> IMPLEMENTS(kind=php_trait) with base_name.
			graph.Relationship{ID: "e1", FromID: "cls", ToID: "trait", Kind: "IMPLEMENTS"}.WithProperties(map[string]string{"language": "php", "kind": "php_trait", "base_name": "Audit"}),
			graph.Relationship{ID: "e2", FromID: "trait_log", ToID: "trait_persist", Kind: "CALLS"}.WithProperties(map[string]string{"language": "php"}),
		},
	}
}

// TestPHPTrait_ResolvesToTraitBody — Order::log (a bodyless trait-use stub)
// resolves to Audit::log's real body, cross-file (Audit.php).
func TestPHPTrait_ResolvesToTraitBody(t *testing.T) {
	srv := newTestServer(t, phpTraitDoc())
	out := callInspect(t, srv, "cls_log")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for PHP trait method, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != true {
		t.Fatalf("expected resolved=true for PHP trait, got %v (note=%v)", inh["resolved"], inh["note"])
	}
	if dc, _ := inh["defining_class"].(string); !strings.Contains(dc, "Audit") {
		t.Errorf("expected defining_class Audit, got %q", dc)
	}
	if df, _ := inh["defining_file"].(string); df != "Audit.php" {
		t.Errorf("expected defining_file Audit.php (cross-file trait), got %q", df)
	}
}

// TestPHPTrait_TraversalReachesTraitCallee — the bodyless stub hops via INHERITS
// to Audit.log and surfaces its real callee Audit.persist.
func TestPHPTrait_TraversalReachesTraitCallee(t *testing.T) {
	srv := newTestServer(t, phpTraitDoc())
	out := callNeighbors3834(t, srv, "cls_log", "out")
	names := calleeNames3834(t, out)
	if !containsString(names, "Audit.log") {
		t.Fatalf("expected trait stub to reach defining Audit.log via INHERITS, got: %v", names)
	}
	if !containsString(names, "Audit.persist") {
		t.Errorf("expected to reach trait callee Audit.persist THROUGH the body, got: %v", names)
	}
}

// TestRubyInclude_ExternalModule_HonestUnresolved — NEGATIVE: a class including
// an EXTERNAL/unindexed module (no in-repo body, no pack entry) resolves
// honest-unresolved. No fabricated body, no synthetic INHERITS edge.
func TestRubyInclude_ExternalModule_HonestUnresolved(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "cls", Name: "User", QualifiedName: "User",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "user.rb",
				StartLine: 1, EndLine: 3, Language: "ruby"},
			// Inherited stub whose module is external (no indexed body).
			{ID: "cls_serialize", Name: "User.to_json", QualifiedName: "User.to_json",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "user.rb",
				StartLine: 0, EndLine: 0, Language: "ruby", Signature: "def to_json"},
			{ID: "ext", Name: "Comparable", Kind: "SCOPE.External", Language: "ruby"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "e1", FromID: "cls", ToID: "ext", Kind: "IMPLEMENTS"}.WithProperties(map[string]string{"language": "ruby", "kind": "ruby_mixin", "mixin_op": "include", "base_name": "Comparable"}),
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "cls_serialize")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for unresolved mixin member, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != false {
		t.Errorf("expected resolved=false for external module, got %v", inh["resolved"])
	}
	if _, hasDC := inh["defining_class"]; hasDC {
		t.Errorf("unresolved member must NOT carry a defining_class, got %v", inh["defining_class"])
	}
	if edges := mroOutboundEdges(srv.State.groups["test"].Repos["repo1"], "cls_serialize"); len(edges) != 0 {
		t.Errorf("expected no synthetic INHERITS edge for unresolved member, got: %+v", edges)
	}
}

// TestPHPTrait_MethodNotInTrait_Unresolved — NEGATIVE: a member that exists in
// NO mixin/trait (the trait declares log, not ship) is unresolved. The trait
// body is indexed but does not declare the member, so no fabrication.
func TestPHPTrait_MethodNotInTrait_Unresolved(t *testing.T) {
	doc := phpTraitDoc()
	// Add a bodyless stub Order.ship — Audit declares no `ship`.
	doc.Entities = append(doc.Entities, graph.Entity{
		ID: "cls_ship", Name: "Order.ship", QualifiedName: "Order.ship",
		Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Order.php",
		StartLine: 0, EndLine: 0, Language: "php", Signature: "public function ship()",
	})
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "cls_ship")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != false {
		t.Errorf("expected resolved=false for member declared in no trait, got %v", inh["resolved"])
	}
	if _, hasDC := inh["defining_class"]; hasDC {
		t.Errorf("unresolved member must NOT carry a defining_class, got %v", inh["defining_class"])
	}
}

// TestRubyPrepend_ResolvesToModuleBody — `prepend` is also followed (prepend
// wins over the class's own method per Ruby MRO; here the class declares no
// hello body so the prepended module body is what resolves). Asserts the
// prepend edge resolves cross-file the same as include.
func TestRubyPrepend_ResolvesToModuleBody(t *testing.T) {
	doc := rubyMixinDoc()

	doc.Relationships[0].PropSet("mixin_op", "prepend")
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "cls_hello")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for Ruby prepend, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != true {
		t.Fatalf("expected resolved=true for Ruby prepend, got %v (note=%v)", inh["resolved"], inh["note"])
	}
	if df, _ := inh["defining_file"].(string); df != "greet.rb" {
		t.Errorf("expected defining_file greet.rb, got %q", df)
	}
}
