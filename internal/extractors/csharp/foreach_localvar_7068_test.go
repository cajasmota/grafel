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
    public class Order { public void Process() {} public void Ship() {} public void Audit() {} }

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

        // ref readonly is the SAME ref_type node, but "same node type" is the
        // assumption this axis exists to test rather than to assert: round 1
        // held the modifier constant and only varied the element type.
        public void ByRefReadonly(ReadOnlySpan<Order> span)
        {
            foreach (ref readonly Order o in span) { o.Audit(); }
        }
    }
}
`
	ents := runCSharp(t, src)
	if got := fe7068Targets(t, ents, "Runner.Typed"); !csHasTarget(got, "Order.Ship") {
		t.Fatalf("CONTROL Runner.Typed: missing CALLS -> Order.Ship; got %v", got)
	}
	for _, c := range []struct{ op, bare, dotted string }{
		{"Runner.ByRef", "Process", "Order.Process"},
		{"Runner.ByRefReadonly", "Audit", "Order.Audit"},
	} {
		got := fe7068Targets(t, ents, c.op)
		if !csHasTarget(got, c.bare) {
			t.Errorf("%s: expected the bare leaf CALLS -> %s for a ref loop variable; got %v",
				c.op, c.bare, got)
		}
		if csHasTarget(got, c.dotted) {
			t.Errorf("%s: ref_type now binds — the gap is closed, update this test and the "+
				"comment in collectLocalVarTypes; got %v", c.op, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Round 2 — the three blocking findings of the adversarial review on #7070.
// ---------------------------------------------------------------------------

// TestCSharp_Foreach7068_EmptyTypeIsNotLedgered grades the `typ == ""` guard,
// which round 1 wrongly argued was equivalent.
//
// Round 1's argument reasoned only about the READ: receiverTypeName does
// `if t, ok := paramTypes[ident]; ok && t != ""`, so a "" value reads as absent.
// That is true and it is the wrong half — `out[name] = typ` is a WRITE into a
// map the locals pass has already filled. The review's distinguishing input
// exploited exactly that: a "" write CLOBBERED a correct local binding.
//
// That clobber is now impossible by construction (the merge below never
// overwrites a name the locals pass bound — see defect 3 in
// collectLocalVarTypes), so the review's original input no longer separates the
// mutant; TestCSharp_Foreach7068_ForeachNeverClobbersALocal pins that directly.
// The guard is STILL load-bearing, through the ambiguity ledger: an untyped
// `foreach` that reached the ledger would write "" for the name and then
// mutually-refuse the later, real binding of the same name. Two sibling
// `foreach` statements may legally reuse a name, since a loop variable's scope
// is its own statement.
//
// Without the guard: `o` is ledgered as "" by the ref loop, the typed loop
// disagrees, both are refused, and `o.Ship()` falls back to a bare `Ship`.
func TestCSharp_Foreach7068_EmptyTypeIsNotLedgered(t *testing.T) {
	src := `
using System;
using System.Collections.Generic;

namespace Shop
{
    public class Order { public void Ship() {} public void Process() {} }

    public class Runner
    {
        // CONTROL — a typed foreach with no same-name neighbour at all.
        public void Control(List<Order> orders)
        {
            foreach (Order c in orders) { c.Process(); }
        }

        // The ref loop yields no type. It must not enter the ledger for o,
        // or it will refuse the typed loop below it.
        public void RefThenTyped(Span<Order> span, List<Order> orders)
        {
            foreach (ref Order o in span) { }
            foreach (Order o in orders) { o.Ship(); }
        }
    }
}
`
	ents := runCSharp(t, src)

	if got := fe7068Targets(t, ents, "Runner.Control"); !csHasTarget(got, "Order.Process") {
		t.Fatalf("CONTROL Runner.Control: missing CALLS -> Order.Process; got %v", got)
	}

	got := fe7068Targets(t, ents, "Runner.RefThenTyped")
	if !csHasTarget(got, "Order.Ship") {
		t.Errorf("Runner.RefThenTyped: missing CALLS -> Order.Ship — an untyped foreach "+
			"poisoned the ambiguity ledger for `o`; got %v", got)
	}
	if csHasTarget(got, "Ship") {
		t.Errorf("Runner.RefThenTyped: emitted BARE CALLS -> Ship; got %v", got)
	}
}

// TestCSharp_Foreach7068_ForeachNeverClobbersALocal grades defect 3: this pass
// runs after the local_declaration_statement pass over the same map, so a plain
// `out[name] = typ` overwrote a local's binding REGARDLESS of source order —
// turning a previously-correct `Order.Ship` into `Line.Ship`. That would have
// been a regression minted by #7068, not a pre-existing condition.
//
// Both source orders are asserted, because the two passes are separate walks
// and the review's point was precisely that source order does not protect you.
func TestCSharp_Foreach7068_ForeachNeverClobbersALocal(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"local-first", `
            { Order o = Get(); o.Ship(); }
            foreach (Line o in lines) { o.Touch(); }`},
		{"foreach-first", `
            foreach (Line o in lines) { o.Touch(); }
            { Order o = Get(); o.Ship(); }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `
using System.Collections.Generic;

namespace Shop
{
    public class Order { public void Ship() {} }
    public class Line  { public void Touch() {} }

    public class Runner
    {
        private Order Get() { return null; }

        // CONTROL — a foreach with no colliding local, in the same file.
        public void Control(List<Line> lines)
        {
            foreach (Line c in lines) { c.Touch(); }
        }

        public void M(List<Line> lines)
        {` + c.body + `
        }
    }
}
`
			ents := runCSharp(t, src)
			if got := fe7068Targets(t, ents, "Runner.Control"); !csHasTarget(got, "Line.Touch") {
				t.Fatalf("CONTROL Runner.Control: missing CALLS -> Line.Touch; got %v", got)
			}
			got := fe7068Targets(t, ents, "Runner.M")
			if !csHasTarget(got, "Order.Ship") {
				t.Errorf("Runner.M/%s: the foreach binding clobbered the local `Order o` — "+
					"a previously-correct edge was broken; got %v", c.name, got)
			}
			if csHasTarget(got, "Line.Ship") {
				t.Errorf("Runner.M/%s: emitted the WRONG dotted target Line.Ship; got %v", c.name, got)
			}
		})
	}
}

// TestCSharp_Foreach7068_ConflictingSiblingsRefuseEachOther grades the other
// half of the two-stage merge. Two sibling `foreach` loops binding one name to
// DIFFERENT types cannot both be right, and the flat per-method map cannot model
// block scope. Refusing both degrades to a bare leaf, which the resolver treats
// as its bare-name class; picking a winner would emit a WRONG DOTTED target,
// which binds and is scored as a confident success (#7071).
//
// Same type on both loops is not a conflict and must still bind — that is the
// positive control, and it is what separates "refuses on conflict" from
// "refuses whenever a name repeats".
func TestCSharp_Foreach7068_ConflictingSiblingsRefuseEachOther(t *testing.T) {
	src := `
using System.Collections.Generic;

namespace Shop
{
    public class Order { public void Ship() {} public void Audit() {} }
    public class Line  { public void Touch() {} }

    public class Runner
    {
        // CONTROL — same name, SAME type: not a conflict, must bind.
        public void SameType(List<Order> a, List<Order> b)
        {
            foreach (Order o in a) { o.Ship(); }
            foreach (Order o in b) { o.Audit(); }
        }

        public void DifferentTypes(List<Order> a, List<Line> b)
        {
            foreach (Order o in a) { o.Ship(); }
            foreach (Line o in b) { o.Touch(); }
        }
    }
}
`
	ents := runCSharp(t, src)

	ctl := fe7068Targets(t, ents, "Runner.SameType")
	for _, want := range []string{"Order.Ship", "Order.Audit"} {
		if !csHasTarget(ctl, want) {
			t.Errorf("CONTROL Runner.SameType: missing CALLS -> %s — a repeated name is being "+
				"refused even without a type conflict; got %v", want, ctl)
		}
	}

	got := fe7068Targets(t, ents, "Runner.DifferentTypes")
	for _, forbidden := range []string{"Order.Touch", "Line.Ship"} {
		if csHasTarget(got, forbidden) {
			t.Errorf("Runner.DifferentTypes: emitted the WRONG dotted target %s; got %v", forbidden, got)
		}
	}
	for _, want := range []string{"Ship", "Touch"} {
		if !csHasTarget(got, want) {
			t.Errorf("Runner.DifferentTypes: expected the bare leaf CALLS -> %s "+
				"(conflicting siblings refuse each other); got %v", want, got)
		}
	}
}

// TestCSharp_Foreach7068_DynamicIsNotFabricated grades the second half of the
// keyword guard. `dynamic` is a contextual keyword meaning "no static type",
// but the grammar spells it `identifier`, so leafTypeName returns "dynamic"
// verbatim and the arm would emit `dynamic.Process` — a fabricated DOTTED
// target at a brand-new site, in exactly the direction the `var` guard closes.
// main emits the honest bare `Process` here, so this would have been a
// regression minted by #7068.
func TestCSharp_Foreach7068_DynamicIsNotFabricated(t *testing.T) {
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

        public void Dyn(List<Order> orders)
        {
            foreach (dynamic d in orders) { d.Process(); }
        }
    }
}
`
	ents := runCSharp(t, src)
	if got := fe7068Targets(t, ents, "Runner.Typed"); !csHasTarget(got, "Order.Ship") {
		t.Fatalf("CONTROL Runner.Typed: missing CALLS -> Order.Ship; got %v", got)
	}
	got := fe7068Targets(t, ents, "Runner.Dyn")
	if csHasTarget(got, "dynamic.Process") {
		t.Errorf("Runner.Dyn: fabricated pseudo-type receiver dynamic.Process; got %v", got)
	}
	if !csHasTarget(got, "Process") {
		t.Errorf("Runner.Dyn: expected the bare leaf CALLS -> Process; got %v", got)
	}
}

// TestCSharp_Foreach7068_TypeParameterIsFabricated_KnownWrong PINS KNOWN-WRONG
// BEHAVIOUR. An open generic type parameter in a foreach parses as `identifier`
// and is bound like any other named type, so `foreach (T o in xs)` inside
// `class C<T>` emits the fabricated dotted target `T.Process`.
//
// This is NOT fixed here, and the honest reason is that refusing it needs
// type-parameter SCOPE, which this extractor has nowhere — classCtx carries only
// `fields`. The neighbouring #6912 field-type pass records the identical hole as
// its own known limitation with its own pinned over-fire tests. A name blocklist
// guessing at `T`/`TKey`/`TValue` would fire on a real class legitimately named
// `T` and would leave the actual gap ungraded.
//
// A fix is EXPECTED to break this test. When it does, invert the assertion
// rather than deleting it, and update the type-parameter note in
// collectLocalVarTypes.
func TestCSharp_Foreach7068_TypeParameterIsFabricated_KnownWrong(t *testing.T) {
	src := `
using System.Collections.Generic;

namespace Shop
{
    public class Order { public void Ship() {} }

    public class Box<T>
    {
        // CONTROL — a real same-file type in the same class, which must bind.
        public void Typed(List<Order> orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        public void Each(List<T> items)
        {
            foreach (T o in items) { o.Process(); }
        }
    }
}
`
	ents := runCSharp(t, src)
	if got := fe7068Targets(t, ents, "Box.Typed"); !csHasTarget(got, "Order.Ship") {
		t.Fatalf("CONTROL Box.Typed: missing CALLS -> Order.Ship; got %v", got)
	}
	got := fe7068Targets(t, ents, "Box.Each")
	if !csHasTarget(got, "T.Process") {
		t.Errorf("KNOWN-WRONG pin is stale: expected the fabricated CALLS -> T.Process. "+
			"If type-parameter scope has been implemented, invert this assertion and update "+
			"the type-parameter note in collectLocalVarTypes; got %v", got)
	}
}

// TestCSharp_Foreach7068_PointerAndAliasQualified closes the enumeration gap the
// review found: round 1's comment presented its list of binding forms as a
// closed "confirmed set" while omitting two that reach leafTypeName through
// SHARED branches — pointer_type (the `array_type, pointer_type` case) and
// alias_qualified_name (the `qualified_name` case).
//
// The two do NOT behave alike, and the difference only shows up in compilable
// C#. The review's pointer example was `foreach (int* p in xs) { p.ToString(); }`
// → `int.ToString`, but that is not valid C#: a pointer has no members, and `.`
// on one does not compile. The only way to call through a pointer is `->`, which
// is not a `member_access_expression`, so the receiver-typing path never sees it.
// The loop variable IS entered into the local-type map — that part of the review
// is right — but no compilable C# turns it into a dotted CALLS target. Recorded
// as the observed bare result rather than a claimed binding.
//
// alias_qualified_name does bind, and only in its SINGLE-segment form:
// `global::A.B.Foo` parses as a qualified_name whose qualifier merely contains
// an alias, so `global::Order` needs Order in the global namespace to be both
// compilable and an alias_qualified_name. Hence the separate fixture.
func TestCSharp_Foreach7068_PointerAndAliasQualified(t *testing.T) {
	t.Run("pointer-stays-bare", func(t *testing.T) {
		src := `
using System.Collections.Generic;

namespace Shop
{
    public struct Node { public void Touch() {} }
    public class Order { public void Ship() {} }

    public unsafe class Runner
    {
        // CONTROL — the plain identifier form, in the same file.
        public void Typed(List<Order> orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        public void Ptr(Node*[] xs)
        {
            foreach (Node* p in xs) { p->Touch(); }
        }
    }
}
`
		ents := runCSharp(t, src)
		if got := fe7068Targets(t, ents, "Runner.Typed"); !csHasTarget(got, "Order.Ship") {
			t.Fatalf("CONTROL Runner.Typed: missing CALLS -> Order.Ship; got %v", got)
		}
		got := fe7068Targets(t, ents, "Runner.Ptr")
		if !csHasTarget(got, "Touch") {
			t.Errorf("Runner.Ptr: expected the bare leaf CALLS -> Touch — `->` is not a "+
				"member_access_expression, so no receiver type is applied; got %v", got)
		}
		if csHasTarget(got, "Node.Touch") {
			t.Errorf("Runner.Ptr: pointer member access now yields a dotted target — the "+
				"comment in collectLocalVarTypes needs updating; got %v", got)
		}
	})

	t.Run("alias-qualified-binds", func(t *testing.T) {
		src := `
using System.Collections.Generic;

public class Order { public void Audit() {} public void Ship() {} }

public class Runner
{
    // CONTROL — the plain identifier form, in the same file.
    public void Typed(List<Order> orders)
    {
        foreach (Order o in orders) { o.Ship(); }
    }

    public void Alias(List<Order> orders)
    {
        foreach (global::Order o in orders) { o.Audit(); }
    }
}
`
		ents := runCSharp(t, src)
		if got := fe7068Targets(t, ents, "Runner.Typed"); !csHasTarget(got, "Order.Ship") {
			t.Fatalf("CONTROL Runner.Typed: missing CALLS -> Order.Ship; got %v", got)
		}
		if got := fe7068Targets(t, ents, "Runner.Alias"); !csHasTarget(got, "Order.Audit") {
			t.Errorf("Runner.Alias: alias_qualified_name should bind to its leaf segment; got %v", got)
		}
	})
}
