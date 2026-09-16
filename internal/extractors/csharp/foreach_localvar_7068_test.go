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
// `out[name] = typ` overwrote an existing binding REGARDLESS of source order —
// turning a previously-correct `Order.Ship` into `Line.Ship`. That would have
// been a regression minted by #7068, not a pre-existing condition.
//
// TWO AXES ARE VARIED HERE, and the second exists because varying only the
// first is what let a real regression through review:
//
//  1. SOURCE ORDER (local-first / foreach-first). The two passes are separate
//     walks, so source order does not protect you.
//
//  2. THE KIND OF PRIOR BINDING. An earlier revision varied ONLY source order
//     and held the binding form constant at `local_declaration_statement` in
//     both rows — then generalised the result to "a name already bound is
//     never touched". `local_declaration_statement` is the ONLY binding form
//     this extractor collects, so that guard consulted our own `out` map and
//     answered "did we TYPE this name?" rather than "is this name TAKEN?".
//     Measured consequence: `using (Conn c = Open()) { c.Close(); }` beside
//     `foreach (Order c in orders)` emitted `Order.Close` on an `Order` with
//     no `Close`, where the arm-off tree emitted the correct `Conn.Close`.
//     Each row below is a DIFFERENT binding form reaching the same collision.
//
// COMPILER CAVEAT, stated because it is load-bearing and could not be removed:
// there is NO C# COMPILER in this environment, so no fixture here has been
// compiler-checked. Every row rests on one language rule — a `foreach`
// variable's scope is its own statement, so a SIBLING block or statement may
// reuse the name without CS0136. Rows are restricted to forms where that rule
// is the only thing in question.
//
// DELIBERATELY NOT WRITTEN, rather than written on an unchecked premise:
//
//   - `out var c` / `out T c` (declaration_expression). An `out` declaration is
//     scoped to the ENCLOSING BLOCK, which encloses the sibling `foreach`, so
//     the collision is very likely CS0136 and no such program exists to grade.
//   - lambda parameters and local-function parameters. Whether a lambda or
//     local-function parameter may reuse a sibling `foreach` variable's name is
//     a shadowing rule this author could not demonstrate without a compiler.
//
// csNamesBoundOutsideForeach DOES cover all three of those node types, so the
// behaviour is implemented; it is the GRADING that is missing, and a mutant
// removing `parameter`, `implicit_parameter` or `declaration_expression` from
// that list survives this suite. Settling it needs `csc`.
func TestCSharp_Foreach7068_ForeachNeverClobbersALocal(t *testing.T) {
	// Each case is one PRIOR BINDING FORM, in both source orders relative to
	// the foreach that tries to take its name.
	forms := []struct {
		name   string
		decl   string // binds `o` to Order and calls o.Ship()
		detail string
	}{
		{
			"local-declaration",
			"{ Order o = Get(); o.Ship(); }",
			"variable_declarator, typed — the only form the locals pass collects",
		},
		{
			"untyped-var-local",
			"{ var o = Get(); o.Ship(); }",
			"variable_declarator whose type inferImplicitLocalType cannot derive " +
				"(a plain call RHS, #4685), so the name is a local declaration that " +
				"never enters `out` — the ledger must record names regardless of typing",
		},
		{
			"using-statement",
			"using (Order o = Get()) { o.Ship(); }",
			"variable_declarator reached through a using_statement, which the " +
				"extractor does not collect at all",
		},
		{
			"catch-clause",
			"try { } catch (Order o) { o.Ship(); }",
			"catch_declaration — a binding node type of its own",
		},
	}
	for _, f := range forms {
		for _, order := range []string{"prior-first", "foreach-first"} {
			t.Run(f.name+"/"+order, func(t *testing.T) {
				loop := "foreach (Line o in lines) { o.Touch(); }"
				body := f.decl + "\n            " + loop
				if order == "foreach-first" {
					body = loop + "\n            " + f.decl
				}
				src := `
using System;
using System.Collections.Generic;

namespace Shop
{
    public class Order : IDisposable { public void Ship() {} public void Dispose() {} }
    public class Line  { public void Touch() {} }

    public class Runner
    {
        private Order Get() { return null; }

        // CONTROL — a foreach with no colliding prior binding, same file.
        public void Control(List<Line> lines)
        {
            foreach (Line c in lines) { c.Touch(); }
        }

        public void M(List<Line> lines)
        {
            ` + body + `
        }
    }
}
`
				ents := runCSharp(t, src)
				if got := fe7068Targets(t, ents, "Runner.Control"); !csHasTarget(got, "Line.Touch") {
					t.Fatalf("CONTROL Runner.Control: missing CALLS -> Line.Touch; got %v", got)
				}
				got := fe7068Targets(t, ents, "Runner.M")
				if csHasTarget(got, "Line.Ship") {
					t.Errorf("%s/%s (%s): the foreach binding took a name already bound and "+
						"emitted the WRONG dotted target Line.Ship; got %v",
						f.name, order, f.detail, got)
				}
				// `o` is bound by the prior form, so the foreach must refuse it.
				// Whether o.Ship() then comes out typed depends on whether that
				// form is one the extractor types at all — which is exactly the
				// point: refusing costs nothing a bare leaf did not already cost,
				// and the resolver's same-file tier launders a bare leaf (#7071).
				if !csHasTarget(got, "Order.Ship") && !csHasTarget(got, "Ship") {
					t.Errorf("%s/%s (%s): o.Ship() produced neither Order.Ship nor a bare "+
						"Ship; got %v", f.name, order, f.detail, got)
				}
			})
		}
	}
}

// TestCSharp_Foreach7068_UsingBindingKeepsItsReceiver is the F-A regression in
// its measured form, at the extractor boundary.
//
// `Conn` is the only declarer of `Close` in the file, so the BARE `Close` the
// arm-off tree emits is laundered to the correct `Conn.Close` by the resolver's
// same-file leaf-name tier (#7071). An earlier revision of this arm took the
// name `c` — bound by a `using`, which the extractor does not collect — and
// emitted `Order.Close` instead, on an `Order` that has no `Close`: a correct
// edge turned into a fabricated dotted one. Measured end to end through
// `grafel quality`, not only here.
//
// The assertion is deliberately on the FORBIDDEN target rather than on a
// required dotted one: what this arm owes is not to type `c`, but never to
// type it WRONG.
func TestCSharp_Foreach7068_UsingBindingKeepsItsReceiver(t *testing.T) {
	src := `
using System;
using System.Collections.Generic;

namespace Shop
{
    public class Order { public void Ship() {} }
    public class Conn : IDisposable { public void Close() {} public void Dispose() {} }

    public class Runner
    {
        private Conn Open() { return null; }

        // CONTROL — a foreach whose name nothing else binds, same file.
        public void Control(List<Order> orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        public void M(List<Order> orders)
        {
            foreach (Order c in orders) { }
            using (Conn c = Open()) { c.Close(); }
        }
    }
}
`
	ents := runCSharp(t, src)
	if got := fe7068Targets(t, ents, "Runner.Control"); !csHasTarget(got, "Order.Ship") {
		t.Fatalf("CONTROL Runner.Control: missing CALLS -> Order.Ship; got %v", got)
	}
	got := fe7068Targets(t, ents, "Runner.M")
	if csHasTarget(got, "Order.Close") {
		t.Errorf("Runner.M: the foreach took `c`, a name bound by a using statement, and "+
			"retargeted c.Close() to Order.Close — Order has no Close; got %v", got)
	}
	if !csHasTarget(got, "Close") {
		t.Errorf("Runner.M: expected the bare leaf CALLS -> Close, which the resolver's "+
			"same-file tier binds to Conn.Close; got %v", got)
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
//
// THIS TEST RESTS ON AN UNVERIFIED LANGUAGE PREMISE, stated here because the
// assertion below otherwise reads as settled. `var` and `dynamic` are both
// contextual keywords, and they are believed NOT to be symmetric: a user type
// named `var` is prohibited by the language, while one named `dynamic` is
// believed to be PERMITTED and to shadow the keyword in type position. If that
// is right, `class dynamic {}` beside `foreach (dynamic d in xs)` is a legal
// program whose correct edge this refusal silently deletes — an over-refusal
// with no symptom (#7056).
//
// NO C# COMPILER EXISTS IN THIS ENVIRONMENT, so neither the reviewer who raised
// this nor the author could demonstrate the rule either way, and asserting a
// language rule nobody ran is the defect class this whole change exists to fix.
// What settles it: compile `class dynamic {}` with `csc`. If it compiles, this
// test and csNonBindableTypeKeyword both need the `dynamic` case reconsidered.
// Until then the direction is the conservative one — the guard drops an edge
// rather than fabricating a dotted target — and its blast radius is this arm.
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

    public interface IProcessable { void Process(); }

    // The where-clause is load-bearing for COMPILABILITY, not for the
    // parse: an unconstrained T makes o.Process() CS1061, so the fixture
    // would assert fabrication on input no compiler accepts. A constrained T
    // parses identically (type=identifier), so the node shape under test is
    // unchanged and the program is now one that can exist.
    public class Box<T> where T : IProcessable
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

// TestCSharp_Foreach7068_LinqBoundNamesAreRefused is F-1: round 3's defect
// one level over, plus refusal rows for the three ordinary LINQ range
// variables that had been on the ledger without one. The claimed-names ledger listed `from_clause`, `let_clause`
// and `join_clause` BECAUSE LINQ range variables bind names — and `into` binds
// one by the same rule, so the list was self-inconsistent rather than merely
// short.
//
// Measured against main, where the arm never ran:
//
//	main:  [Count GetHashCode]        — the honest bare leaf
//	round 3: [Order.Count GetHashCode] — fabricated; Order has no Count
//
// Two distinct CST shapes, confirmed by dump, and only one of them has a node:
//
//	group … into c   `into` + a bare `identifier` as DIRECT CHILDREN of
//	                 query_expression — nothing for findAllNodes to match,
//	                 which is why it was missed. Found positionally.
//	select … into c  the same shape.
//	join … into c    wrapped in a `join_into_clause`, now on the ledger's list.
//
// The `join` case is the instructive one: `join_clause` was ALREADY on the list
// and has no `name` field, so the first-identifier fallback returned the join
// RANGE variable `b` and stopped — correct as far as it went, which is exactly
// what hid the missing continuation variable `c`.
//
// LEGALITY, unverified and stated because it is load-bearing: no C# compiler
// exists in this environment. These rows rest on the same rule every row in
// ForeachNeverClobbersALocal rests on — a foreach variable's scope is its own
// statement, so a sibling statement may reuse the name.
func TestCSharp_Foreach7068_LinqBoundNamesAreRefused(t *testing.T) {
	forms := []struct {
		name  string
		query string
		shape string
	}{
		{
			"group-into",
			"from o in orders group o by o.GetHashCode() into c select c.Count()",
			"bare identifier under query_expression — no node type to match",
		},
		{
			"select-into",
			"from o in orders select o into c select c.Count()",
			"same nodeless shape as group-into",
		},
		{
			"join-into",
			"from a in orders join b in orders on a.GetHashCode() equals b.GetHashCode() into c select c.Count()",
			"join_into_clause — a node, but hidden behind join_clause's own range variable",
		},
		// The three ORDINARY range variables. These were on the ledger from the
		// start but had no refusal row of their own, so a mutant dropping any of
		// them survived. They collide by the same rule and the same shape as the
		// `into` rows above, so grading them costs three lines and removes three
		// silently-ungraded entries.
		{
			"from-range",
			"from c in orders select c.Count()",
			"from_clause — the range variable proper",
		},
		{
			"let-range",
			"from a in orders let c = a.GetHashCode() select c.Count()",
			"let_clause — no `name` field; the first-identifier fallback supplies it",
		},
		{
			"join-range",
			"from a in orders join c in orders on a.GetHashCode() equals c.GetHashCode() select c.Count()",
			"join_clause — no `name` field; fallback returns the range variable",
		},
	}
	for _, f := range forms {
		for _, order := range []string{"foreach-first", "query-first"} {
			t.Run(f.name+"/"+order, func(t *testing.T) {
				loop := "foreach (Order c in orders) { }"
				query := "var q = " + f.query + ";"
				body := loop + "\n            " + query
				if order == "query-first" {
					body = query + "\n            " + loop
				}
				src := `
using System.Collections.Generic;
using System.Linq;

namespace Shop
{
    public class Order { public void Ship() {} }

    public class Runner
    {
        // CONTROL — a foreach whose name nothing else binds, same file.
        public void Control(List<Order> orders)
        {
            foreach (Order o in orders) { o.Ship(); }
        }

        public void M(List<Order> orders)
        {
            ` + body + `
        }
    }
}
`
				ents := runCSharp(t, src)
				if got := fe7068Targets(t, ents, "Runner.Control"); !csHasTarget(got, "Order.Ship") {
					t.Fatalf("CONTROL Runner.Control: missing CALLS -> Order.Ship; got %v", got)
				}
				got := fe7068Targets(t, ents, "Runner.M")
				if csHasTarget(got, "Order.Count") {
					t.Errorf("%s/%s (%s): the foreach took `c`, a name the LINQ `into` "+
						"continuation binds, and fabricated Order.Count — Order has no "+
						"Count; got %v", f.name, order, f.shape, got)
				}
				if !csHasTarget(got, "Count") {
					t.Errorf("%s/%s (%s): expected the bare leaf CALLS -> Count; got %v",
						f.name, order, f.shape, got)
				}
			})
		}
	}
}

// TestCSharp_Foreach7068_NonCollidingBindingIsKept is F-2, and it grades the
// direction every other table in this file structurally cannot.
//
// WHAT THESE ROWS ADD, stated carefully because the first version of this
// header overclaimed it and the overclaim is the exact defect class this PR
// exists to fix.
//
// The header said the over-refusal direction "was graded by nothing" before
// this table. That is FALSE, and scoring says so. Restoring the previous
// revision's test file and re-scoring two over-refusal mutants against it:
//
//	refuse every foreach name unconditionally  → DEAD, 21 failing lines
//	claim every identifier in the body         → DEAD, 21 failing lines
//
// both across 11 distinct tests, killed by their CONTROL rows — including
// `ForeachNeverClobbersALocal`, which the old header named as structurally
// incapable of detecting over-refusal. Sharper still: those controls are
// `t.Fatalf`, so under those mutants the subtest ABORTS before any refusal
// assertion executes. Even the narrow reading — "the refusal rows passed" — is
// not something anyone observed.
//
// What this table actually adds is PER-FORM RESOLUTION. Previously a
// too-broad ledger failed a control somewhere and said only "something
// over-refuses"; these rows say WHICH binding form does, one row each, with no
// shadowing anywhere in the fixtures — so unlike every other table here their
// legality does not rest on the sibling-scope rule, and the absent C# compiler
// does not weaken them.
//
// WHAT IT DOES NOT GRADE: any individual ledger entry. VARIED = the binding
// form; HELD CONSTANT = the bound name differs from the loop variable `o`. `o`
// is bound only by the `foreach_statement`, which is deliberately not on the
// ledger, so no entry-level widening can put `o` into it. Proof rather than
// argument: adding `"foreach_statement"` to the ledger's node list — the
// textbook over-refusal — is DEAD, but it does NOT fail this table. Across the
// round-4 mutant set this table was never a unique killer.
//
// Over-refusal remains the safe direction: the arm never ran before #7068, so
// it can only add, and a dropped receiver costs unrealized recall rather than
// emitting anything wrong. The reason to keep the rows is that this ledger has
// grown under review twice, and a growing list eventually swallows names it was
// never meant to touch.
//
// LEGALITY: unlike every other table here, these fixtures involve NO shadowing
// whatsoever — each prior binding has a name the foreach does not use — so their
// legality does not depend on the sibling-scope rule the other rows rest on, and
// the absence of a C# compiler does not weaken them.
func TestCSharp_Foreach7068_NonCollidingBindingIsKept(t *testing.T) {
	forms := []struct {
		name string
		stmt string
		node string
	}{
		{"local-declaration", "Line n1 = null; n1.Touch();", "variable_declarator"},
		{"using-statement", "using (Conn n2 = Open()) { n2.Close(); }", "variable_declarator via using_statement"},
		{"catch-clause", "try { } catch (Boom n3) { n3.Bang(); }", "catch_declaration"},
		{"lambda-implicit-param", "var f1 = xs.Select(n4 => n4.Touch());", "implicit_parameter"},
		{"lambda-typed-param", "var f2 = xs.Select((Line n5) => n5.Touch());", "parameter"},
		{"local-function-param", "void Inner(Line n6) { n6.Touch(); }", "parameter via local_function_statement"},
		{"out-declaration", "Fetch(out Line n7); n7.Touch();", "declaration_expression"},
		{"is-pattern", "if (probe is Line n8) { n8.Touch(); }", "declaration_pattern"},
		{"linq-from", "var q1 = from n9 in xs select n9;", "from_clause"},
		{"linq-let", "var q2 = from a in xs let n10 = a.Touch() select n10;", "let_clause"},
		{"linq-join", "var q3 = from a in xs join n11 in xs on a.K() equals n11.K() select n11;", "join_clause"},
		{"linq-group-into", "var q4 = from a in xs group a by a.K() into n12 select n12.Count();", "positional `into` scan"},
		{"linq-join-into", "var q5 = from a in xs join b in xs on a.K() equals b.K() into n13 select n13.Count();", "join_into_clause"},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			src := `
using System;
using System.Collections.Generic;
using System.Linq;

namespace Shop
{
    public class Order { public void Ship() {} }
    public class Line  { public void Touch() {} public int K() { return 0; } }
    public class Boom : Exception { public void Bang() {} }
    public class Conn : IDisposable { public void Close() {} public void Dispose() {} }

    public class Runner
    {
        private Conn Open() { return null; }
        private void Fetch(out Line v) { v = null; }
        private object probe = null;

        public void M(List<Order> orders, List<Line> xs)
        {
            ` + f.stmt + `
            foreach (Order o in orders) { o.Ship(); }
        }
    }
}
`
			ents := runCSharp(t, src)
			got := fe7068Targets(t, ents, "Runner.M")
			if !csHasTarget(got, "Order.Ship") {
				t.Errorf("%s (%s): a NON-colliding %s suppressed the loop variable's "+
					"receiver type — the ledger is over-refusing; got %v",
					f.name, f.node, f.node, got)
			}
			if csHasTarget(got, "Ship") {
				t.Errorf("%s (%s): emitted the bare leaf CALLS -> Ship alongside/instead of "+
					"Order.Ship; got %v", f.name, f.node, got)
			}
		})
	}
}
