package python_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #5158 — `getattr(self, name)()` reflective dispatch where `name` is a
// string literal bound earlier in the method body resolves the CALLS edge to
// `<Class>.<method>` via the reusable literal-binding resolver.

func findPyCall(ents []types.EntityRecord, caller, toID string) *types.RelationshipRecord {
	for i := range ents {
		// caller entity name matches the method short name OR its
		// Class.method qualified form.
		if ents[i].Name != caller && !endsWithMember(ents[i].Name, caller) {
			continue
		}
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == "CALLS" && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

func TestPy5158_HappyPath_BoundLiteral(t *testing.T) {
	src := `class OrderHandler:
    def run(self, ev):
        action = "handle_order"
        getattr(self, action)(ev)

    def handle_order(self, ev):
        pass
`
	ents := extractPy(t, src, "h.py")
	rel := findPyCall(ents, "run", "OrderHandler.handle_order")
	if rel == nil {
		t.Fatalf("expected resolved CALLS run→OrderHandler.handle_order; got %#v", relSummary(ents))
	}
	if rel.Properties["resolved_via"] != extractor.ResolvedViaLiteralBinding {
		t.Errorf("resolved_via = %q; want %q", rel.Properties["resolved_via"], extractor.ResolvedViaLiteralBinding)
	}
	if rel.Properties["dynamic_target"] != "action" {
		t.Errorf("dynamic_target = %q; want action", rel.Properties["dynamic_target"])
	}
}

func TestPy5158_HappyPath_InlineLiteral(t *testing.T) {
	src := `class H:
    def run(self):
        getattr(self, "handle_stock")()

    def handle_stock(self):
        pass
`
	ents := extractPy(t, src, "h.py")
	rel := findPyCall(ents, "run", "H.handle_stock")
	if rel == nil {
		t.Fatalf("expected resolved CALLS run→H.handle_stock; got %#v", relSummary(ents))
	}
	if rel.Properties["dynamic_target"] != "getattr-literal" {
		t.Errorf("inline form dynamic_target = %q; want getattr-literal", rel.Properties["dynamic_target"])
	}
}

func TestPy5158_LastWriteWins(t *testing.T) {
	src := `class H:
    def run(self):
        action = "a_method"
        action = "b_method"
        getattr(self, action)()

    def a_method(self):
        pass

    def b_method(self):
        pass
`
	ents := extractPy(t, src, "h.py")
	if findPyCall(ents, "run", "H.b_method") == nil {
		t.Fatal("last-write-wins: expected CALLS run→H.b_method")
	}
	if findPyCall(ents, "run", "H.a_method") != nil {
		t.Fatal("last-write-wins: stale CALLS run→H.a_method must NOT be emitted")
	}
}

func TestPy5158_TaintNoResolve(t *testing.T) {
	// action reassigned from a non-literal (a call) ⇒ tainted ⇒ no resolution.
	src := `class H:
    def run(self):
        action = "a_method"
        action = self.pick()
        getattr(self, action)()

    def a_method(self):
        pass
`
	ents := extractPy(t, src, "h.py")
	if rel := findPyCall(ents, "run", "H.a_method"); rel != nil {
		t.Fatalf("tainted binding must NOT resolve; got %#v", rel.Properties)
	}
}

func TestPy5158_NoMatch_NoOp_NonLiteralName(t *testing.T) {
	// name is a binary expression — never a static literal ⇒ no getattr edge.
	src := `class H:
    def run(self, ev):
        name = "on_" + ev
        getattr(self, name)(ev)

    def on_x(self, ev):
        pass
`
	ents := extractPy(t, src, "h.py")
	// No resolved getattr edge should be present (name is non-static).
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties["pattern_type"] == "getattr_dispatch" {
				t.Fatalf("non-static name must NOT produce getattr dispatch edge; got %#v", r)
			}
		}
	}
}

// endsWithMember reports whether name is "<X>.<member>".
func endsWithMember(name, member string) bool {
	return len(name) > len(member)+1 && name[len(name)-len(member)-1] == '.' &&
		name[len(name)-len(member):] == member
}

// relSummary renders a compact CALLS dump for failure messages.
func relSummary(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				out = append(out, e.Name+"->"+r.ToID)
			}
		}
	}
	return out
}
