package csharp_test

// Issue #7068 — `collectLocalVarTypes` matched the node type
// `for_each_statement`, which tree-sitter-c-sharp does not have. The grammar
// spells it `foreach_statement`. The literal was UNPAIRED (no arm anywhere
// matched the real name), so a `foreach` loop variable was never typed and
// every method call on one emitted a BARE-NAME CALLS target.
//
// The CST evidence (dumped from the same grammar these tests parse with; see
// the PR body for the full dump) for the `type` field of `foreach_statement`:
//
//	foreach (Order o     in xs)   type=identifier       → "Order"
//	foreach (string s    in xs)   type=predefined_type  → "string"
//	foreach (List<Order> g in xs) type=generic_name     → "List"
//	foreach (Order[] a   in xs)   type=array_type       → "Order"
//	foreach (Order? o    in xs)   type=nullable_type    → "Order"
//	foreach (Ns.Order o  in xs)   type=qualified_name   → "Order"
//	await foreach (Order o in …)  type=identifier       → "Order"
//	foreach (var o       in xs)   type=implicit_type    → "var"   ← NOT a type
//	foreach ((Order a, Order b) in xs)   no `type` field at all
//	foreach (var (a, b)  in xs)   type=implicit_type, left=tuple_pattern
//
// Every test below carries a NON-foreach control in the same fixture so a
// failure can be told apart from "nothing types at all".

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// fe7068Targets returns the CALLS targets of the named operation, failing the
// test if the operation itself is missing (so a fixture that stops extracting
// cannot read as "no forbidden target").
func fe7068Targets(t *testing.T, ents []types.EntityRecord, op string) []string {
	t.Helper()
	found := false
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == op {
			found = true
			break
		}
	}
	if !found {
		var names []string
		for i := range ents {
			if ents[i].Kind == "SCOPE.Operation" {
				names = append(names, ents[i].Name)
			}
		}
		t.Fatalf("no SCOPE.Operation entity named %q; got %s", op, strings.Join(names, ", "))
	}
	return csCallTargets(ents, op)
}

func csHasTarget(targets []string, want string) bool {
	for _, g := range targets {
		if g == want {
			return true
		}
	}
	return false
}

// foreachFixture is one file holding every graded `foreach` form plus the
// non-`foreach` control. Each receiver method has a DISTINCT name so the
// per-call `seen[target]` dedupe in extractCallRelationships cannot hide one
// form behind another.
const foreachFixture = `
using System.Collections.Generic;

namespace Shop
{
    public class Order
    {
        public void Process() {}
        public void Clone2() {}
        public void Ship() {}
        public void Audit() {}
        public void Verify() {}
        public void Await1() {}
        public void Outer1() {}
    }

    public class Line
    {
        public void Touch() {}
    }

    public class Runner
    {
        // CONTROL — an explicitly typed local, NOT a foreach. This was typed
        // before the fix and must stay typed after it.
        public void ViaLocal(List<Order> orders)
        {
            Order p = orders[0];
            p.Process();
        }

        public void ViaForeach(List<Order> orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        public void ViaForeachArrayElem(List<Order[]> groups)
        {
            foreach (Order[] a in groups) { a.Clone2(); }
        }

        public void ViaForeachNullable(List<Order> orders)
        {
            foreach (Order? o in orders) { o.Audit(); }
        }

        public void ViaForeachQualified(List<Order> orders)
        {
            foreach (Shop.Order o in orders) { o.Verify(); }
        }

        public void ViaForeachGenericElem(List<List<Order>> groups)
        {
            foreach (List<Order> g in groups) { g.Clear(); }
        }

        public async System.Threading.Tasks.Task ViaAwaitForeach(IAsyncEnumerable<Order> src)
        {
            await foreach (Order o in src) { o.Await1(); }
        }

        public void ViaNestedForeach(List<Order> orders)
        {
            foreach (Order o in orders)
            {
                o.Outer1();
                foreach (Line l in o.Lines) { l.Touch(); }
            }
        }
    }
}
`

// TestCSharp_Foreach7068_LoopVarTypesReceiver is the core grading test: a
// method call on a `foreach` loop variable of a same-file type MUST resolve to
// `Type.Method`, not to a bare `Method`.
//
// The `ViaLocal` control pins the other direction: if the whole local-type
// table stopped working, the control fails too and the foreach rows below
// cannot be mistaken for a fix.
func TestCSharp_Foreach7068_LoopVarTypesReceiver(t *testing.T) {
	ents := runCSharp(t, foreachFixture)

	cases := []struct {
		op      string
		want    string
		forbid  string
		comment string
	}{
		{"Runner.ViaLocal", "Order.Process", "Process", "CONTROL: explicit local, typed before and after"},
		{"Runner.ViaForeach", "Order.Ship", "Ship", "explicitly typed loop variable"},
		{"Runner.ViaForeachArrayElem", "Order.Clone2", "Clone2", "array element type — leaf is the element"},
		{"Runner.ViaForeachNullable", "Order.Audit", "Audit", "nullable element type"},
		{"Runner.ViaForeachQualified", "Order.Verify", "Verify", "qualified element type — leaf is the last segment"},
		{"Runner.ViaForeachGenericElem", "List.Clear", "Clear", "generic element type — leaf is the raw name"},
		{"Runner.ViaAwaitForeach", "Order.Await1", "Await1", "await foreach is still a foreach_statement"},
		{"Runner.ViaNestedForeach", "Order.Outer1", "Outer1", "outer loop variable of a nested foreach"},
		{"Runner.ViaNestedForeach", "Line.Touch", "Touch", "inner loop variable of a nested foreach"},
	}
	for _, c := range cases {
		got := fe7068Targets(t, ents, c.op)
		if !csHasTarget(got, c.want) {
			t.Errorf("%s (%s): missing CALLS -> %q; got %v", c.op, c.comment, c.want, got)
		}
		if csHasTarget(got, c.forbid) {
			t.Errorf("%s (%s): emitted BARE CALLS -> %q (should be %q); got %v",
				c.op, c.comment, c.forbid, c.want, got)
		}
	}
}

// TestCSharp_Foreach7068_ImplicitVarNotFabricated pins the defect the audit of
// the never-executed arm found: `leafTypeName` returns the raw text "var" for
// an `implicit_type` node, so correcting the node-type literal ALONE would have
// bound the loop variable to the pseudo-type `var` and emitted `var.Process` —
// a fabricated receiver type, exactly the emit-wrong direction #7056 is about.
//
// The conservative choice (mirroring the `local_declaration_statement` path's
// `isImplicitVarType` guard, #4685) is to leave a `var` loop variable UNTYPED:
// the call falls back to its bare leaf. A `foreach`'s `right` field is the
// COLLECTION, not the element, so inferring the element type would need generic
// argument analysis this extractor does not do — that is a separate limit, not
// something this fix supplies.
func TestCSharp_Foreach7068_ImplicitVarNotFabricated(t *testing.T) {
	src := `
using System.Collections.Generic;

namespace Shop
{
    public class Order { public void Process() {} public void Ship() {} }

    public class Runner
    {
        // CONTROL — the explicitly typed sibling in the same file.
        public void Typed(List<Order> orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        public void Implicit(List<Order> orders)
        {
            foreach (var o in orders) { o.Process(); }
        }
    }
}
`
	ents := runCSharp(t, src)

	if got := fe7068Targets(t, ents, "Runner.Typed"); !csHasTarget(got, "Order.Ship") {
		t.Fatalf("CONTROL Runner.Typed: missing CALLS -> Order.Ship; got %v", got)
	}

	got := fe7068Targets(t, ents, "Runner.Implicit")
	for _, target := range got {
		if strings.HasPrefix(target, "var.") {
			t.Errorf("Runner.Implicit: fabricated pseudo-type receiver %q from `foreach (var o …)`; got %v",
				target, got)
		}
	}
	if !csHasTarget(got, "Process") {
		t.Errorf("Runner.Implicit: expected the bare leaf CALLS -> Process (var stays untyped); got %v", got)
	}
}

// TestCSharp_Foreach7068_DeconstructionBindsNothing pins the second defect of
// the never-executed arm. The arm had a "first identifier child" fallback for
// the loop-variable NAME, used when the `left` field is not an identifier. The
// only form that reaches it is a deconstructing `foreach (var (a, b) in pairs)`
// — type=implicit_type, left=tuple_pattern — and on that input the fallback
// scans the statement's direct children and finds `pairs`, the COLLECTION, not
// a loop variable. It would have bound `pairs` -> "var" and emitted
// `var.Count()` for any call on the collection later in the body.
//
// Both deconstructing forms must bind nothing: the explicitly typed one has no
// `type` field at all, and the `var` one is refused by the implicit guard.
func TestCSharp_Foreach7068_DeconstructionBindsNothing(t *testing.T) {
	src := `
using System.Collections.Generic;

namespace Shop
{
    public class Order { public void Process() {} public void Ship() {} }

    public class Runner
    {
        // CONTROL — the explicitly typed sibling in the same file.
        public void Typed(List<Order> orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        private List<(Order, Order)> Fetch() { return null; }

        // pairs is an implicitly typed local whose initialiser is a plain
        // method call, so inferImplicitLocalType leaves it UNTYPED (#4685) and
        // nothing else in the file binds the name. That is what makes
        // pairs.Clear() a clean probe for the deleted name fallback: the only
        // way pairs could acquire a type here is the fallback mis-binding it.
        public void VarDeconstruct()
        {
            var pairs = Fetch();
            foreach (var (a, b) in pairs) { a.Process(); pairs.Clear(); }
        }

        public void TypedDeconstruct(List<(Order, Order)> pairs2)
        {
            foreach ((Order a, Order b) in pairs2) { a.Process(); }
        }
    }
}
`
	ents := runCSharp(t, src)

	if got := fe7068Targets(t, ents, "Runner.Typed"); !csHasTarget(got, "Order.Ship") {
		t.Fatalf("CONTROL Runner.Typed: missing CALLS -> Order.Ship; got %v", got)
	}

	for _, op := range []string{"Runner.VarDeconstruct", "Runner.TypedDeconstruct"} {
		for _, target := range fe7068Targets(t, ents, op) {
			if strings.HasPrefix(target, "var.") {
				t.Errorf("%s: deconstructing foreach fabricated receiver %q", op, target)
			}
		}
	}
	// The collection `pairs` must not have been bound to "var" by the
	// name fallback: `pairs.Clear()` stays bare.
	got := fe7068Targets(t, ents, "Runner.VarDeconstruct")
	if !csHasTarget(got, "Clear") {
		t.Errorf("Runner.VarDeconstruct: expected bare CALLS -> Clear for pairs.Clear(); got %v", got)
	}
}

// TestCSharp_Foreach7068_RefElementStaysBare observes, rather than merely
// asserting in prose, that `foreach (ref Order o in span)` does NOT bind: the
// grammar gives `type=ref_type`, leafTypeName has no case for it, and the raw
// text "ref Order" contains a space so isCsIdentifierText rejects it. This is a
// remaining gap in the form space, recorded so a later change that closes it
// has to update a test rather than discover the claim was stale. The explicitly
// typed sibling is the control.
func TestCSharp_Foreach7068_RefElementStaysBare(t *testing.T) {
	src := `
using System;

namespace Shop
{
    public class Order { public void Process() {} public void Ship() {} }

    public class Runner
    {
        // CONTROL — the explicitly typed sibling in the same file.
        public void Typed(Order[] orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        public void ByRef(Span<Order> span)
        {
            foreach (ref Order o in span) { o.Process(); }
        }
    }
}
`
	ents := runCSharp(t, src)
	if got := fe7068Targets(t, ents, "Runner.Typed"); !csHasTarget(got, "Order.Ship") {
		t.Fatalf("CONTROL Runner.Typed: missing CALLS -> Order.Ship; got %v", got)
	}
	got := fe7068Targets(t, ents, "Runner.ByRef")
	if !csHasTarget(got, "Process") {
		t.Errorf("Runner.ByRef: expected the bare leaf CALLS -> Process for a ref loop variable; got %v", got)
	}
	if csHasTarget(got, "Order.Process") {
		t.Errorf("Runner.ByRef: ref_type now binds — the gap is closed, update this test and the "+
			"comment in collectLocalVarTypes; got %v", got)
	}
}
