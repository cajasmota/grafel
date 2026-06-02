package hierarchy

// mixin_trait_3840_test.go — Ruby mixin (`include`/`prepend`/`extend`) and PHP
// trait (`use`) hierarchy-edge extraction (#3840, epic #3829 MRO T8).
//
// These assert the hierarchy extractor emits an IMPLEMENTS edge from the
// enclosing class to the mixed-in module / used trait, carrying the
// distinguishing kind + base_name the MCP MRO walk consumes. They are
// value-asserting: they pin the FROM/TO names, the edge kind property, and the
// mixin op (precedence) — not just "an edge exists".

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findImplements returns the first IMPLEMENTS edge whose base_name matches.
func findImplements(recs []types.EntityRecord, baseName string) *types.RelationshipRecord {
	for ri := range recs {
		for rj := range recs[ri].Relationships {
			r := &recs[ri].Relationships[rj]
			if r.Kind == "IMPLEMENTS" && r.Properties["base_name"] == baseName {
				return r
			}
		}
	}
	return nil
}

// TestRubyInclude_EmitsImplementsToModule — `class User; include Greet; end`
// emits IMPLEMENTS User -> Greet tagged ruby_mixin / include.
func TestRubyInclude_EmitsImplementsToModule(t *testing.T) {
	src := "module Greet\n  def hello\n  end\nend\n\nclass User\n  include Greet\nend\n"
	got := runExtract(t, "ruby", "app/models/user.rb", src)

	edge := findImplements(got, "Greet")
	if edge == nil {
		t.Fatalf("expected IMPLEMENTS edge to Greet, got entities=%v", entityNames(got))
	}
	if edge.Properties["kind"] != "ruby_mixin" {
		t.Errorf("kind=%q, want ruby_mixin", edge.Properties["kind"])
	}
	if edge.Properties["mixin_op"] != "include" {
		t.Errorf("mixin_op=%q, want include", edge.Properties["mixin_op"])
	}
	if edge.FromID != classRef("app/models/user.rb", "User", "ruby") {
		t.Errorf("FromID=%q, want User class ref", edge.FromID)
	}
	if edge.ToID != ifaceRef("Greet", "ruby") {
		t.Errorf("ToID=%q, want Greet iface ref", edge.ToID)
	}
}

// TestRubyPrepend_RecordsPrependOp — prepend precedence is recoverable from the
// mixin_op property (prepend wins over the class's own method).
func TestRubyPrepend_RecordsPrependOp(t *testing.T) {
	src := "class User\n  prepend Loud\nend\n"
	got := runExtract(t, "ruby", "user.rb", src)
	edge := findImplements(got, "Loud")
	if edge == nil {
		t.Fatalf("expected IMPLEMENTS edge to Loud")
	}
	if edge.Properties["mixin_op"] != "prepend" {
		t.Errorf("mixin_op=%q, want prepend", edge.Properties["mixin_op"])
	}
}

// TestRubyExtend_RecordsExtendOp — `extend M` is a singleton-class mixin; still
// a member-promotion edge.
func TestRubyExtend_RecordsExtendOp(t *testing.T) {
	src := "class User\n  extend ClassMethods\nend\n"
	got := runExtract(t, "ruby", "user.rb", src)
	edge := findImplements(got, "ClassMethods")
	if edge == nil {
		t.Fatalf("expected IMPLEMENTS edge to ClassMethods")
	}
	if edge.Properties["mixin_op"] != "extend" {
		t.Errorf("mixin_op=%q, want extend", edge.Properties["mixin_op"])
	}
}

// TestRubyMixin_NamespacedModule_UsesLeaf — `include Foo::Bar` resolves to the
// leaf constant Bar (Ruby methods are keyed by their bare module leaf).
func TestRubyMixin_NamespacedModule_UsesLeaf(t *testing.T) {
	src := "class User\n  include Concerns::Auditable\nend\n"
	got := runExtract(t, "ruby", "user.rb", src)
	if edge := findImplements(got, "Auditable"); edge == nil {
		t.Fatalf("expected IMPLEMENTS edge with base_name=Auditable (leaf of Concerns::Auditable), got entities=%v", entityNames(got))
	}
}

// TestRubyMixin_Nested_AttributedToInnermost — a mixin inside a nested class is
// attributed to the innermost open class, not the outer module.
func TestRubyMixin_Nested_AttributedToInnermost(t *testing.T) {
	src := "module Outer\n  class Inner\n    include Greet\n  end\nend\n"
	got := runExtract(t, "ruby", "n.rb", src)
	edge := findImplements(got, "Greet")
	if edge == nil {
		t.Fatalf("expected IMPLEMENTS edge to Greet")
	}
	if edge.FromID != classRef("n.rb", "Inner", "ruby") {
		t.Errorf("FromID=%q, want Inner (innermost), not Outer", edge.FromID)
	}
}

// TestRubyMixin_TopLevel_NoEdge — a mixin with no enclosing class produces no
// edge (nothing to attach the promoted member to). Honest, no fabrication.
func TestRubyMixin_TopLevel_NoEdge(t *testing.T) {
	src := "include Kernel\n"
	got := runExtract(t, "ruby", "top.rb", src)
	if edge := findImplements(got, "Kernel"); edge != nil {
		t.Errorf("top-level mixin should emit no IMPLEMENTS edge, got %+v", edge)
	}
}

// TestPHPTrait_EmitsImplementsToTrait — `class Order { use Audit; }` emits
// IMPLEMENTS Order -> Audit tagged php_trait.
func TestPHPTrait_EmitsImplementsToTrait(t *testing.T) {
	src := "<?php\ntrait Audit {\n  public function log() {}\n}\nclass Order {\n  use Audit;\n}\n"
	got := runExtract(t, "php", "src/Order.php", src)

	edge := findImplements(got, "Audit")
	if edge == nil {
		t.Fatalf("expected IMPLEMENTS edge to Audit, got entities=%v", entityNames(got))
	}
	if edge.Properties["kind"] != "php_trait" {
		t.Errorf("kind=%q, want php_trait", edge.Properties["kind"])
	}
	if edge.FromID != classRef("src/Order.php", "Order", "php") {
		t.Errorf("FromID=%q, want Order class ref", edge.FromID)
	}
	if edge.ToID != ifaceRef("Audit", "php") {
		t.Errorf("ToID=%q, want Audit iface ref", edge.ToID)
	}
}

// TestPHPTrait_MultipleAndNamespaced — `use A, B;` emits two edges; a
// namespaced trait resolves to its leaf.
func TestPHPTrait_MultipleAndNamespaced(t *testing.T) {
	src := "<?php\nclass Order {\n  use Audit, App\\Traits\\Timestamps;\n}\n"
	got := runExtract(t, "php", "Order.php", src)
	if findImplements(got, "Audit") == nil {
		t.Errorf("expected IMPLEMENTS to Audit")
	}
	if findImplements(got, "Timestamps") == nil {
		t.Errorf("expected IMPLEMENTS to Timestamps (leaf of App\\Traits\\Timestamps)")
	}
}

// TestPHPTrait_TopLevelImportNotTrait — a namespace import `use Foo\Bar;` at
// file scope (NOT inside a class) must NOT become a trait IMPLEMENTS edge.
func TestPHPTrait_TopLevelImportNotTrait(t *testing.T) {
	src := "<?php\nnamespace App;\nuse Foo\\Bar;\nuse Psr\\Log\\LoggerInterface;\nclass Svc extends Base {\n}\n"
	got := runExtract(t, "php", "Svc.php", src)
	if edge := findImplements(got, "Bar"); edge != nil && edge.Properties["kind"] == "php_trait" {
		t.Errorf("top-level import use Foo\\Bar must not be a php_trait edge, got %+v", edge)
	}
	if edge := findImplements(got, "LoggerInterface"); edge != nil && edge.Properties["kind"] == "php_trait" {
		t.Errorf("top-level import must not be a php_trait edge, got %+v", edge)
	}
}

// TestPHPTrait_WithAdaptationBlock — `use Audit { foo as bar; }` still emits the
// trait edge (the adaptation block terminates the name list at `{`).
func TestPHPTrait_WithAdaptationBlock(t *testing.T) {
	src := "<?php\nclass Order {\n  use Audit { log as auditLog; }\n}\n"
	got := runExtract(t, "php", "Order.php", src)
	if findImplements(got, "Audit") == nil {
		t.Fatalf("expected IMPLEMENTS to Audit even with adaptation block")
	}
}
