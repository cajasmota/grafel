package csharp_test

import (
	"strings"
	"testing"
)

// #7072 — collectLocalVarTypes is a FLAT walk over the whole method body into
// one name-keyed map, so two locals of the same name in SIBLING blocks collide
// and the last declarator walked wins. The loser's calls then get a receiver
// type that name never had — and because the fabricated type is a real
// same-file type, the dotted edge BINDS. Bind rate, orphan rate and dangle
// count all score that as a success; it is the #7056 signature.
//
// The fix refuses an ambiguous name outright (same two-stage ledger the
// `foreach` arm already uses for feTypes/feAmbiguous), so a collision degrades
// to the honest BARE leaf instead of a confident wrong one.

// cs7072Fixture wraps the given method body in a file carrying four types with
// DISJOINT method sets, so every emitted target names both the receiver type
// the extractor chose and the call site it came from — no target can hide
// behind another, and a wrong receiver is not merely "a different spelling of
// the right answer".
func cs7072Fixture(body string) string {
	return `
using System;
using System.Collections.Generic;

namespace Shop
{
    public class Order    { public void Ship() {} public void Fill() {} }
    public class Customer { public void Bill() {} public void Greet() {} }

    public class Runner
    {
        private Order    MakeOrder()    { return null; }
        private Customer MakeCustomer() { return null; }
        private ref Order Slot() { throw new Exception(); }

        public void M(List<Order> orders)
        {
` + body + `
        }
    }
}
`
}

// TestCSharp7072_SiblingScopeCollisionIsRefused is the RED case: two
// same-name, DIFFERENT-type locals in sibling blocks.
//
// On main the map holds whichever declarator findAllNodes walked last, so ONE
// of the two call sites is emitted with a receiver type it never had. The
// assertion is on the fabricated target specifically, not merely on "the right
// one is present": the defect is not a missing edge, it is a WRONG edge that
// binds.
func TestCSharp7072_SiblingScopeCollisionIsRefused(t *testing.T) {
	src := cs7072Fixture(`
            { Order o = MakeOrder(); o.Ship(); }
            { Customer o = MakeCustomer(); o.Bill(); }
`)
	got := fe7072Targets(t, src)

	// Neither cross-product target may be emitted. Both are checked because
	// which one the flat map produces depends on walk order, which is an
	// implementation detail of findAllNodes and not something to pin.
	for _, wrong := range []string{"Customer.Ship", "Order.Bill"} {
		if csHasTarget(got, wrong) {
			t.Errorf("emitted %q: the local-type map bound `o` to a type that "+
				"call site's `o` never had, and because %s is a real same-file "+
				"type the edge BINDS and is scored as a confident success "+
				"(#7056); got %v", wrong, strings.SplitN(wrong, ".", 2)[0], got)
		}
	}
	// The honest degradation: an ambiguous receiver falls back to the bare
	// leaf, which the resolver's same-file tier may still launder correctly
	// (#7071) and which is at least visibly unresolved when it does not.
	for _, bare := range []string{"Ship", "Bill"} {
		if !csHasTarget(got, bare) {
			t.Errorf("a refused ambiguous receiver must still emit the bare "+
				"leaf CALLS -> %s; got %v", bare, got)
		}
	}
}

// TestCSharp7072_SameNameSameTypeStillBinds is the must-still-bind direction.
// Two sibling blocks each declaring `Order o` are NOT a collision — the name
// maps to one type, so refusing it would be pure recall loss for no soundness
// gain. A collision rule that keys on the NAME alone fails this.
func TestCSharp7072_SameNameSameTypeStillBinds(t *testing.T) {
	src := cs7072Fixture(`
            { Order o = MakeOrder(); o.Ship(); }
            { Order o = MakeOrder(); o.Fill(); }
`)
	got := fe7072Targets(t, src)
	for _, want := range []string{"Order.Ship", "Order.Fill"} {
		if !csHasTarget(got, want) {
			t.Errorf("same-name/SAME-type locals are not a collision and must "+
				"still bind; missing %q, got %v", want, got)
		}
	}
	for _, bare := range []string{"Ship", "Fill"} {
		if csHasTarget(got, bare) {
			t.Errorf("emitted the bare leaf %q instead of the typed receiver — "+
				"the collision rule is over-refusing on name alone; got %v",
				bare, got)
		}
	}
}

// TestCSharp7072_OrdinarySingleDeclarationUnaffected is the control. One
// declaration of each name, no collision anywhere: every receiver keeps its
// type. A guard that always fires kills this.
func TestCSharp7072_OrdinarySingleDeclarationUnaffected(t *testing.T) {
	src := cs7072Fixture(`
            Order o = MakeOrder();
            Customer c = MakeCustomer();
            o.Ship();
            c.Bill();
`)
	got := fe7072Targets(t, src)
	for _, want := range []string{"Order.Ship", "Customer.Bill"} {
		if !csHasTarget(got, want) {
			t.Errorf("an ordinary uncollided local lost its receiver type; "+
				"missing %q, got %v", want, got)
		}
	}
	for _, bare := range []string{"Ship", "Bill"} {
		if csHasTarget(got, bare) {
			t.Errorf("emitted the bare leaf %q for an uncollided local; got %v",
				bare, got)
		}
	}
}

// TestCSharp7072_NestedShadowingDegradesToBare establishes what happens for a
// case #7072 explicitly did not probe: an INNER block redeclaring an outer
// name with a different type.
//
// C# forbids this — CS0136, "a local named `o` cannot be declared in this
// scope because that name is used in an enclosing local scope" — so this
// fixture is believed NOT to be a compilable program. That is DERIVED FROM THE
// SPEC AND UNDERIVED BY EXECUTION: there is no C# compiler in this
// environment (`csc`, `dotnet`, `mono`, `mcs` all absent, verified), so the
// rule could not be demonstrated, and it is recorded rather than asserted in
// the same style as the `dynamic` note in csNonBindableTypeKeyword. What
// settles it: compile this fixture with `csc`.
//
// tree-sitter parses it regardless, so the extractor still has to answer. The
// flat map cannot tell nested from sibling, so the rule treats both alike and
// refuses. This test pins that the answer is the HONEST one (bare leaf) rather
// than a confident wrong one — it does not claim the outer binding is
// recovered. If the fixture is indeed uncompilable, nothing here is a recall
// loss on any real program.
func TestCSharp7072_NestedShadowingDegradesToBare(t *testing.T) {
	src := cs7072Fixture(`
            Order o = MakeOrder();
            o.Ship();
            { Customer o = MakeCustomer(); o.Bill(); }
`)
	got := fe7072Targets(t, src)
	for _, wrong := range []string{"Customer.Ship", "Order.Bill"} {
		if csHasTarget(got, wrong) {
			t.Errorf("nested shadowing produced the confidently-wrong %q; got %v",
				wrong, got)
		}
	}
	for _, bare := range []string{"Ship", "Bill"} {
		if !csHasTarget(got, bare) {
			t.Errorf("nested shadowing must degrade to the bare leaf %q; got %v",
				bare, got)
		}
	}
}

// TestCSharp7072_RefusalLeavesAnUnrelatedParamAlone pins that refusing a
// colliding local does not disturb an UNRELATED parameter in the same method:
// parameter `o` keeps its type while locals named `p` collide and are dropped.
//
// NOTE WHAT THIS DOES NOT PIN. It was originally named for the
// `params win over locals` precedence and graded nothing of the kind — the
// parameter and the locals have DIFFERENT names, so the two maps never share
// a key and inverting the merge left it green. The name claimed more than the
// body did. TestCSharp7072_ParamsWinOverALocalOfTheSameName is the test that
// actually exercises that line; this one is the non-interference control
// beside it.
func TestCSharp7072_RefusalLeavesAnUnrelatedParamAlone(t *testing.T) {
	src := `
using System;
using System.Collections.Generic;

namespace Shop
{
    public class Order    { public void Ship() {} public void Fill() {} }
    public class Customer { public void Bill() {} public void Greet() {} }

    public class Runner
    {
        private Order    MakeOrder()    { return null; }
        private Customer MakeCustomer() { return null; }
        private ref Order Slot() { throw new Exception(); }

        public void M(Order o)
        {
            o.Ship();
            { Order p = MakeOrder(); p.Fill(); }
            { Customer p = MakeCustomer(); p.Bill(); }
        }
    }
}
`
	got := fe7072Targets(t, src)
	if !csHasTarget(got, "Order.Ship") {
		t.Errorf("the parameter `o` lost its type; params must still win over "+
			"locals; got %v", got)
	}
	for _, wrong := range []string{"Order.Bill", "Customer.Fill"} {
		if csHasTarget(got, wrong) {
			t.Errorf("emitted the confidently-wrong %q for the colliding local "+
				"`p`; got %v", wrong, got)
		}
	}
}

// fe7072Targets extracts CALLS targets from Runner.M.
func fe7072Targets(t *testing.T, src string) []string {
	t.Helper()
	return fe7068Targets(t, runCSharp(t, src), "Runner.M")
}

// TestCSharp7072_AmbiguityIsStickyAcrossAnAgreeingRedeclaration observes the
// STICKINESS the ledger's comment claims. Three declarators of `o` — Order,
// Customer, Order — mean the walk's LAST comparison agrees with the stored
// candidate. A ledger that recomputed the flag per declarator instead of
// latching it would clear the disagreement seen in the middle and publish
// `o` as an Order, re-minting the confidently-wrong `Customer.Bill` receiver
// for the middle site. Without this row that difference is unobserved and the
// word "sticky" in the comment is prose asserting what no test checks.
func TestCSharp7072_AmbiguityIsStickyAcrossAnAgreeingRedeclaration(t *testing.T) {
	src := cs7072Fixture(`
            { Order o = MakeOrder(); o.Ship(); }
            { Customer o = MakeCustomer(); o.Bill(); }
            { Order o = MakeOrder(); o.Fill(); }
`)
	got := fe7072Targets(t, src)
	// `o` disagreed at least once, so NO dotted receiver for it is defensible
	// — including the two that happen to be right for their own site, which a
	// flat map has no way to tell apart from the one that is wrong.
	for _, wrong := range []string{"Order.Ship", "Order.Bill", "Order.Fill", "Customer.Ship", "Customer.Bill", "Customer.Fill"} {
		if csHasTarget(got, wrong) {
			t.Errorf("emitted the dotted %q for a name that disagreed with "+
				"itself earlier in the walk — the ambiguity flag was cleared "+
				"by the later agreeing declaration; got %v", wrong, got)
		}
	}
	for _, bare := range []string{"Ship", "Bill", "Fill"} {
		if !csHasTarget(got, bare) {
			t.Errorf("missing the bare leaf %q; got %v", bare, got)
		}
	}
}

// TestCSharp7072_UntypedSiblingPoisonsTheName is the round-2 red (F1).
//
// The first cut of the #7072 ledger let an UNTYPED declarator slip past it:
// when `var` RHS inference yields nothing the declarator `continue`d before
// reaching the ambiguity check, so the name was never poisoned. A typed
// sibling then kept the map to itself and its type was applied to the
// untyped sibling's call sites as well — a confidently wrong dotted receiver
// on a real same-file type, which BINDS. That is the exact #7072/#7056
// signature the fix was opened to remove, surviving inside the fix.
//
// "We could not type this declaration" is not the same as "no declaration
// happened here". The ledger records the NAME regardless, with an empty type
// standing for "bound here, type unknown", and an empty type disagrees with
// every real one.
func TestCSharp7072_UntypedSiblingPoisonsTheName(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"typed-first", `
            { Order o = MakeOrder();    o.Ship(); }
            { var   o = MakeCustomer(); o.Ship(); }
`},
		{"untyped-first", `
            { var   o = MakeCustomer(); o.Ship(); }
            { Order o = MakeOrder();    o.Ship(); }
`},
		// The same hole from the other direction: an untypeable DECLARED type
		// rather than an uninferable `var`. `ref_type` has no leafTypeName
		// case, so a `ref` local's type comes back empty and the declaration
		// used to be skipped before the declarator loop even started. It binds
		// a name all the same.
		{"ref-local-sibling", `
            { Order o = MakeOrder();      o.Ship(); }
            { ref Order o = ref Slot();   o.Ship(); }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := fe7072Targets(t, cs7072Fixture(tc.body))
			if csHasTarget(got, "Order.Ship") {
				t.Errorf("emitted Order.Ship: the `var` sibling could not be "+
					"typed, so it did not poison `o`, and the typed sibling's "+
					"Order was applied to a call site whose `o` was whatever "+
					"MakeCustomer returns — a wrong dotted receiver on a real "+
					"same-file type, which binds (#7056); got %v", got)
			}
			if !csHasTarget(got, "Ship") {
				t.Errorf("a poisoned name must still emit the bare leaf Ship; "+
					"got %v", got)
			}
		})
	}
}

// TestCSharp7072_UntypedAloneIsNotPoison is the over-refusal control for the
// row above, in three shapes. Recording untyped declarations in the ledger
// must not cost anything where there is no disagreement: a lone untyped
// `var`, two untyped `var`s sharing a name, and an untyped `var` sitting
// beside an unrelated typed local must all behave exactly as before.
func TestCSharp7072_UntypedAloneIsNotPoison(t *testing.T) {
	got := fe7072Targets(t, cs7072Fixture(`
            Order o = MakeOrder();
            o.Ship();
            { var u = MakeCustomer(); u.Bill(); }
            { var u = MakeCustomer(); u.Greet(); }
`))
	if !csHasTarget(got, "Order.Ship") {
		t.Errorf("an untyped `var` elsewhere in the body suppressed an "+
			"UNRELATED typed local's receiver — the ledger is over-refusing "+
			"across names; got %v", got)
	}
	// `u` was never typed by anyone, so its calls stay bare — unchanged
	// behaviour, and no fabricated receiver appears for it either.
	for _, bare := range []string{"Bill", "Greet"} {
		if !csHasTarget(got, bare) {
			t.Errorf("an untyped `var` local must keep its bare leaf %q; got %v",
				bare, got)
		}
	}
	for _, wrong := range []string{"Customer.Bill", "Order.Bill", "Customer.Greet", "Order.Greet"} {
		if csHasTarget(got, wrong) {
			t.Errorf("fabricated %q for a local nothing ever typed; got %v",
				wrong, got)
		}
	}
}

// TestCSharp7072_ParamsWinOverALocalOfTheSameName pins the precedence the
// `params win over locals` line at extractCallRelationships actually encodes
// (F2). The earlier test named for this pinned nothing of the kind: its
// parameter was `o` and its locals were `p`, so the two maps never shared a
// key and inverting the merge left the suite green.
//
// The fixture needs a parameter and a local that GENUINELY collide. C#
// forbids that — CS0136, a local may not reuse an enclosing scope's name —
// so this is believed not to be a compilable program. DERIVED FROM THE SPEC
// AND UNDERIVED BY EXECUTION: there is no C# compiler in this environment
// (`csc`, `dotnet`, `mono`, `mcs` absent, verified), recorded the way
// csNonBindableTypeKeyword records its own `dynamic` question. tree-sitter
// parses it regardless and the merge has to answer, so the answer is pinned
// where the code claims it. What settles the compilability: `csc`.
func TestCSharp7072_ParamsWinOverALocalOfTheSameName(t *testing.T) {
	src := `
using System;
using System.Collections.Generic;

namespace Shop
{
    public class Order    { public void Ship() {} public void Fill() {} }
    public class Customer { public void Bill() {} public void Greet() {} }

    public class Runner
    {
        private Customer MakeCustomer() { return null; }
        private ref Order Slot() { throw new Exception(); }

        public void M(Order o)
        {
            Customer o = MakeCustomer();
            o.Ship();
        }
    }
}
`
	got := fe7072Targets(t, src)
	if !csHasTarget(got, "Order.Ship") {
		t.Errorf("the PARAMETER type lost to a local of the same name; "+
			"extractCallRelationships merges locals first and overwrites them "+
			"with params precisely so params win; got %v", got)
	}
	if csHasTarget(got, "Customer.Ship") {
		t.Errorf("the local overwrote the parameter — the merge order at "+
			"`params win over locals` is inverted; got %v", got)
	}
}
