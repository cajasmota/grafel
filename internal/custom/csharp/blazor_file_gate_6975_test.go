package csharp_test

import (
	"testing"
)

// #6975 — `internal/custom/csharp/blazor.go` had no file gate, so it ran the
// whole Blazor rule set over every C# file in a repository, and
// `reBlazorCodeMethod` made the access modifier optional with no line anchor,
// so `var svc = new OrderService(` minted a SCOPE.Operation/function named
// `OrderService`.
//
// WHY THESE TESTS ASSERT MEMBERSHIP, NOT CARDINALITY. The defect was invisible
// to the existing suite because the entity count went UP — on `aspnetcore-mvc`
// (gate ON, in-process Index(), macOS/APFS) entities went 39,765 -> 75,174,
// +89.0%, while TESTS edges went 43,621 -> 36,982, -6,639. A count assertion
// in either direction is satisfied by that. Every assertion below therefore
// names a SPECIFIC entity that must exist or must not exist. The companion
// edge-side assertion lives in cmd/grafel (TestBlazorGateKeepsTestsEdges6975):
// it names the specific TESTS edge that the false positives were destroying.
//
// THE MUTANTS THESE ARE WRITTEN TO KILL, all in the more-permissive direction:
//
//	m1 — delete the `if !isRazorMarkup && !isRazorCodeBehind` gate entirely
//	     => TestBlazorGateRejectsOrdinaryCSharp6975 fails (three entities).
//	m2 — widen the gate to any `.cs` file
//	     => TestBlazorGateRejectsOrdinaryCSharp6975 fails (same three).
//	m3 — narrow the gate to `.razor` only, i.e. copy blazor_deep.go:179's
//	     effect and drop code-behind
//	     => TestBlazorGateAdmitsRazorCodeBehind6975 fails.
//	m4 — restore the optional access modifier / drop the `^[ \t]*` anchor on
//	     reBlazorCodeMethod
//	     => TestBlazorCodeMethodIgnoresConstructorCalls6975 fails, INSIDE a
//	     `.razor.cs` file, so the gate cannot mask it.
//	m5 — run the markup rules over code-behind too (isRazorMarkup widened to
//	     include `.razor.cs`)
//	     => TestBlazorGateAdmitsRazorCodeBehind6975 fails: the generic
//	     argument in `new Wrapper<Order>()` becomes a SCOPE.UIComponent.
//
// Added after review, each of which was ALIVE against the first revision:
//
//	m6 — drop the blazorNonTypeKeywords rejection (R5 residual). The anchor
//	     only excludes a MID-EXPRESSION `new X(`; a line-leading one survives,
//	     and `.razor.cs` is ordinary C# so the gate does not cover it
//	     => TestBlazorCodeMethodIgnoresConstructorCalls6975 fails.
//	m7 — delete the `file.Language != "csharp"` guard (R3)
//	     => TestBlazorLanguageGuardRejectsNonCSharp6975 fails.
//	m8 — widen the generic class `[^;\n]*` to `[^;]*` (MB-1)
//	     => TestBlazorCodeMethodGenericDoesNotSpanLines6975 fails.

// TestBlazorGateRejectsOrdinaryCSharp6975 pins the gate. `OrderService.cs` is
// ordinary ASP.NET Core C# with no Blazor whatsoever; before the gate it
// yielded a SCOPE.Operation/function for every constructor call and a
// SCOPE.UIComponent for every generic type argument.
func TestBlazorGateRejectsOrdinaryCSharp6975(t *testing.T) {
	src := `
namespace Shop.Services;

public class OrderService
{
    public async Task<List<Order>> ListAsync()
    {
        var repo = new OrderRepository(_db);
        var mapper = new OrderMapper();
        return await repo.AllAsync();
    }
}
`
	ents := extract(t, "custom_csharp_blazor", fi("Services/OrderService.cs", "csharp", src))

	// The three entities the un-gated extractor minted here. Named
	// individually rather than counted, so a mutant that keeps two of them
	// still fails.
	for _, bad := range []struct{ kind, name string }{
		{"SCOPE.Operation", "OrderRepository"}, // `new OrderRepository(` as a "code method"
		{"SCOPE.Operation", "ListAsync"},       // a real method, but not a Blazor one
		{"SCOPE.UIComponent", "List"},          // `List<Order>` as a "component tag"
	} {
		if containsEntity(ents, bad.kind, bad.name) {
			t.Errorf("ordinary C# file produced %s %q; blazor.go must not see plain .cs", bad.kind, bad.name)
		}
	}
	if len(ents) != 0 {
		t.Errorf("ordinary C# file produced %d entities, want 0: %+v", len(ents), ents)
	}
}

// TestBlazorGateAdmitsRazorCodeBehind6975 pins the OTHER direction of the same
// gate: Blazor code-behind lives in `.razor.cs`, and a `.razor`-only gate
// copied from blazor_deep.go:179 would silently drop it. It also pins that
// code-behind gets the CODE rules only — `<Foo>` there is a generic type
// argument, not a component reference.
func TestBlazorGateAdmitsRazorCodeBehind6975(t *testing.T) {
	src := `
namespace Shop.Pages;

public partial class Counter : ComponentBase
{
    [Parameter] public int StartAt { get; set; }

    protected override async Task<List<Order>> OnInitializedAsync()
    {
        var box = new Wrapper<Order>();
    }

    void IncrementCount()
    {
    }
}
`
	ents := extract(t, "custom_csharp_blazor", fi("Pages/Counter.razor.cs", "csharp", src))

	// Code rules DO run over code-behind.
	// Declared with a NESTED generic return type (`Task<List<Order>>`) on
	// purpose: an inner-`>`-terminated generic class in the pattern drops this
	// declaration silently, which a mutant run caught during #6975.
	if !containsEntity(ents, "SCOPE.Operation", "OnInitializedAsync") {
		t.Error("code-behind: missing SCOPE.Operation OnInitializedAsync (a .razor-only gate loses this; so does an inner-`>` generic class)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "IncrementCount") {
		t.Error("code-behind: missing SCOPE.Operation IncrementCount (a modifier-less method must still be found)")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "param:StartAt") {
		t.Error("code-behind: missing SCOPE.Pattern param:StartAt")
	}

	// Markup rules DO NOT. The component-tag rule is the one that grades this
	// split, and it is graded on the GENERIC ARGUMENT, not the outer type: in
	// `new Wrapper<Order>()` the substring `<Order>` is what
	// reBlazorComponentTag matches, and `Wrapper` is not matched at all. An
	// earlier revision of this test asserted the absence of `Wrapper` and was
	// therefore satisfied by a mutant that ran the markup rules over
	// code-behind — it named the wrong entity. The four `@`-directive rules
	// cannot be graded here at all: they are `(?m)^@…`-anchored and a line
	// beginning `@page` is not legal C#, so they are unreachable in a
	// `.razor.cs` file whether or not the guard exists. The guard still covers
	// them because the rule is about which SYNTAX a file carries, not about
	// which rules happen to be reachable today.
	if containsEntity(ents, "SCOPE.UIComponent", "Order") {
		t.Error("code-behind: the generic argument in `new Wrapper<Order>()` produced a UIComponent; the component-tag rule is markup-only")
	}
	// And the constructor call must not become a method, even here.
	if containsEntity(ents, "SCOPE.Operation", "Wrapper") {
		t.Error("code-behind: `new Wrapper<Order>()` produced a SCOPE.Operation")
	}
}

// TestBlazorCodeMethodIgnoresConstructorCalls6975 grades the regex on its own
// merits, INSIDE a file the gate admits, so removing the gate cannot make this
// pass and tightening the gate cannot make it pass vacuously. Every name below
// appears only as a constructor call or a mid-expression call.
func TestBlazorCodeMethodIgnoresConstructorCalls6975(t *testing.T) {
	// MID-EXPRESSION and LINE-LEADING forms are both present on purpose. The
	// anchor alone handles only the first: an earlier revision of this test
	// used exclusively mid-expression calls, and simply moving
	// `new PriceCalculator(_rates)` onto its own continuation line — which is
	// how a long assignment is formatted — made the fixture pass while the
	// defect was fully intact. `.razor.cs` is ordinary C#, so the gate does
	// not cover this shape; blazorNonTypeKeywords does.
	src := `
public partial class Cart : ComponentBase
{
    private void Recalculate()
    {
        var a = new PriceCalculator(_rates);
        Total = new MoneyFormatter().Format(a.Sum());
        var snapshot =
            new CartSnapshot(Items);
        new AuditEntry(Items).Write();
        return Publish(snapshot);
    }

    private void Refresh()
    {
        if (Items.Count == 0)
            Clear();
        else if (Items.Count > 100)
            Trim();
    }
}
`
	ents := extract(t, "custom_csharp_blazor", fi("Pages/Cart.razor.cs", "csharp", src))

	// Mid-expression `new X(` — killed by the anchor.
	for _, name := range []string{"PriceCalculator", "MoneyFormatter"} {
		if containsEntity(ents, "SCOPE.Operation", name) {
			t.Errorf("mid-expression `new %s(` minted a SCOPE.Operation; the code-method pattern must be line-anchored", name)
		}
	}
	// LINE-LEADING `new X(` — survives the anchor, killed by the type-token
	// keyword rejection. 2,503 such sites in the corpus C#.
	for _, name := range []string{"CartSnapshot", "AuditEntry"} {
		if containsEntity(ents, "SCOPE.Operation", name) {
			t.Errorf("line-leading `new %s(` minted a SCOPE.Operation; the anchor does not exclude this shape, blazorNonTypeKeywords must", name)
		}
	}
	// `return F(` — type slot holds `return`. 1,666 sites.
	if containsEntity(ents, "SCOPE.Operation", "Publish") {
		t.Error("`return Publish(` minted a SCOPE.Operation named Publish; the type slot holds `return`, not a type")
	}
	// `else if (` — type slot `else`, name slot `if`. 293 sites.
	if containsEntity(ents, "SCOPE.Operation", "if") {
		t.Error("`else if (` minted a SCOPE.Operation named `if`")
	}
	// Positive controls: the real declarations ARE still found, so the test
	// cannot pass by the pattern (or the keyword filter) matching nothing.
	for _, name := range []string{"Recalculate", "Refresh"} {
		if !containsEntity(ents, "SCOPE.Operation", name) {
			t.Errorf("the real method declaration %s was not found — pattern is now too narrow", name)
		}
	}
}

// TestBlazorMarkupStillExtractedFromRazor6975 is the vacuity guard for the
// gate as a whole: a `.razor` markup file must still produce every rule's
// output. Without this, gating to nothing at all would pass every test above.
func TestBlazorMarkupStillExtractedFromRazor6975(t *testing.T) {
	src := `@page "/counter"
@layout MainLayout
@inherits CounterBase
@inject IOrderService Orders
<MyCustomCard title="Hello" />

@code {
    [Parameter] public int StartAt { get; set; }

    void IncrementCount()
    {
        var fmt = new MoneyFormatter();
    }
}
`
	ents := extract(t, "custom_csharp_blazor", fi("Pages/Counter.razor", "csharp", src))

	for _, want := range []struct{ kind, name string }{
		{"SCOPE.Operation", "/counter"},
		{"SCOPE.Component", "layout:MainLayout"},
		{"SCOPE.Component", "inherits:CounterBase"},
		{"SCOPE.Component", "IOrderService"},
		{"SCOPE.UIComponent", "MyCustomCard"},
		{"SCOPE.Pattern", "param:StartAt"},
		{"SCOPE.Operation", "IncrementCount"},
	} {
		if !containsEntity(ents, want.kind, want.name) {
			t.Errorf("razor markup: missing %s %q", want.kind, want.name)
		}
	}
	// The tightened code-method pattern must not re-open the defect here either.
	if containsEntity(ents, "SCOPE.Operation", "MoneyFormatter") {
		t.Error("razor markup: `new MoneyFormatter()` minted a SCOPE.Operation")
	}
}

// TestBlazorLanguageGuardRejectsNonCSharp6975 grades the `file.Language !=
// "csharp"` guard, which a review mutant showed was ungraded: deleting it
// passed both this package and all of cmd/grafel.
//
// The guard is what makes `.razor` unreachable. Both inputs below have a path
// the SUFFIX gate admits, so only the language check can reject them, and both
// carry content every rule matches — a vacuous pass is not available.
func TestBlazorLanguageGuardRejectsNonCSharp6975(t *testing.T) {
	src := `@page "/counter"
@inject IOrderService Orders
<MyCustomCard title="Hello" />

@code {
    void IncrementCount()
    {
    }
}
`
	// "razor" is the language a real `.razor` file actually classifies as
	// (classifier.go:345), which is why the extractor never sees one.
	for _, lang := range []string{"razor", "vbnet", "java"} {
		ents := extract(t, "custom_csharp_blazor", fi("Pages/Counter.razor", lang, src))
		if len(ents) != 0 {
			t.Errorf("language %q produced %d entities, want 0: %+v", lang, len(ents), ents)
		}
	}
	// Control: the identical content under "csharp" DOES produce entities, so
	// the loop above is rejecting on language and not on content.
	if ents := extract(t, "custom_csharp_blazor", fi("Pages/Counter.razor", "csharp", src)); len(ents) == 0 {
		t.Error("control: the same content under language \"csharp\" produced nothing")
	}
}

// TestBlazorCodeMethodGenericDoesNotSpanLines6975 grades the `\n` in the
// generic class `(?:<[^;\n]*>)?`, which a review mutant showed was ungraded:
// widening it to `[^;]*` left this package and cmd/grafel green.
//
// Without the `\n` the class is greedy ACROSS LINES, so the `<` opening the
// multi-line parameter list below reaches the `>` of the `=>` several lines
// later and the pattern then reads ` Sum(` as a declaration — inventing a
// method out of two unrelated constructs. `Sum` is declared nowhere.
func TestBlazorCodeMethodGenericDoesNotSpanLines6975(t *testing.T) {
	src := `
public partial class Report : ComponentBase
{
    void Render(
        Dictionary<string, int> counts)
    {
    }

    int Total => Sum();
}
`
	ents := extract(t, "custom_csharp_blazor", fi("Pages/Report.razor.cs", "csharp", src))

	if containsEntity(ents, "SCOPE.Operation", "Sum") {
		t.Error("a generic opened on one line reached a `>` several lines later and minted a SCOPE.Operation named Sum, which is declared nowhere")
	}
	// Positive control: the real declaration between them is still found, so
	// the assertion above cannot pass by the rule producing nothing at all.
	if !containsEntity(ents, "SCOPE.Operation", "Render") {
		t.Error("the real method declaration Render was not found — pattern is now too narrow")
	}
}
