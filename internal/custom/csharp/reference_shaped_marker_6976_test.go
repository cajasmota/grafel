package csharp_test

import (
	"context"
	"testing"

	_ "github.com/cajasmota/grafel/internal/custom/csharp"
	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6976 — the producer side of the declaration-outranks-reference tier.
//
// internal/resolve cannot derive "is this record a declaration or a mention?"
// from the record: no field means it, `provenance` is a producer id rather
// than a semantic class, helpers.go's makeEntity stamps StartLine == EndLine
// for the ENTIRE C# custom lane so span ranks the lane and not the record, and
// the same makeEntity stamps QualityScore 1.0 uniformly. The emit site is the
// only place that knows, so the marker is stamped there and this file pins it.
//
// WHY THE EXPECTATION IS AN INDEPENDENT LITERAL. wantMarked6976 below is
// hand-written, NOT derived from any production table. A test that ranges over
// the collection under test cannot detect a deletion from it — that exact
// mistake shipped once in this area and was caught only by re-scoring (#6975
// follow-up). Deleting a markReferenceShaped call must turn a row RED.
//
// THE MUTANTS THESE ARE WRITTEN TO KILL, more-permissive direction first:
//
//	m1 — delete any one of the six markReferenceShaped calls
//	     => that producer's row in TestReferenceShapedProducersAreMarked6976
//	        fails by name.
//	m2 — stamp markReferenceShaped unconditionally in makeEntity (the
//	     "mark the whole lane" shortcut)
//	     => TestDeclarationShapedProducersAreNotMarked6976 fails: an @page
//	        route, an [ApiController] route and a Blazor @code method are
//	        declaration-shaped and must stay unmarked.
//	m3 — change the stamped VALUE to anything but "true"
//	     => every marked row fails, because resolve.isReferenceShaped tests
//	        for the exact string.
//	m4 — rename the property on one side only
//	     => TestReferenceShapedPropSpelling_6976 in internal/resolve fails,
//	        and so does every row here, because the rows read the constant
//	        through resolve.ReferenceShapedProp rather than a local copy.

// wantMarked6976 is the closed scope of #6976: the SIX measured producers in
// the `aspnetcore-mvc` eviction population that mint an entity from a MENTION
// of a name declared elsewhere. Written out by hand.
//
// Deliberately NOT a repo-wide taxonomy of the ~340 custom producers — see the
// scope note on resolve.ReferenceShapedProp. Adding a seventh producer means
// adding a row here.
var wantMarked6976 = []struct {
	provenance string
	why        string
}{
	{"INFERRED_FROM_DOTNET_DI_PROVIDER", "constructor parameter type; the class is declared in its own file"},
	{"INFERRED_FROM_ASPNET_RETURN_TYPE", "ActionResult<T> return annotation mentions T"},
	{"INFERRED_FROM_ASPNET_FROM_BODY", "[FromBody] parameter annotation mentions the DTO type"},
	{"INFERRED_FROM_FROM_BODY", "the validation lane's [FromBody] rule, same shape"},
	{"INFERRED_FROM_BLAZOR_COMPONENT_REF", "a <Foo> markup tag is a USE of the component declared in Foo.razor"},
	{"INFERRED_FROM_BLAZOR_INJECT", "@inject IFoo names a service declared elsewhere"},
}

// wantUnmarked6976 is the negative control, and it is what stops the fix from
// being implemented as "mark the whole C# custom lane". Each of these
// producers reads a DECLARATION: it is entitled to the repository-wide byName
// slot and marking it would cost its own resolution.
var wantUnmarked6976 = []string{
	"INFERRED_FROM_BLAZOR_PAGE",        // @page "/counter" declares the route
	"INFERRED_FROM_BLAZOR_CODE_METHOD", // a method DECLARATION in an @code block
	"INFERRED_FROM_BLAZOR_PARAMETER",   // a [Parameter] property declaration
}

// csharpFixtures6976 are the sources that make every producer above fire. One
// map, consumed by both directions, so the marked and unmarked verdicts are
// read off the SAME extraction run and cannot drift apart.
func csharpFixtures6976() map[string]extreg.FileInput {
	return map[string]extreg.FileInput{
		"custom_csharp_dotnet_di": {
			Path: "src/Controllers/HomeController.cs", Language: "csharp",
			Content: []byte(`
public class HomeController {
    public HomeController(ActionContext context, IWrapperProvider wrapper) { }
}
`),
		},
		"custom_csharp_aspnet_reqresp": {
			Path: "src/Controllers/OrderController.cs", Language: "csharp",
			Content: []byte(`
[ApiController]
public class OrderController : ControllerBase {
    [HttpPost]
    public ActionResult<OrderResponse> Create([FromBody] OrderRequest body) { return null; }
}
`),
		},
		"custom_csharp_validation": {
			Path: "src/Controllers/PayController.cs", Language: "csharp",
			Content: []byte(`
public class PayController : ControllerBase {
    public IActionResult Pay([FromBody] PaymentRequest body) { return null; }
}
`),
		},
		"custom_csharp_blazor": {
			Path: "src/Pages/Counter.razor", Language: "csharp",
			Content: []byte(`@page "/counter"
@inject IWeatherService Weather

<WeatherTable Rows="@rows" />

@code {
    [Parameter] public int Start { get; set; }

    private void IncrementCount() { }
}
`),
		},
	}
}

// extractByProvenance6976 runs every fixture through its extractor and returns
// provenance -> whether the emitted record carries the reference marker, plus
// the entity name, so a row that never fired is distinguishable from a row
// that fired unmarked.
func extractByProvenance6976(t *testing.T) map[string]struct {
	marked bool
	name   string
} {
	t.Helper()
	got := map[string]struct {
		marked bool
		name   string
	}{}
	for key, in := range csharpFixtures6976() {
		e, ok := extreg.Get(key)
		if !ok {
			t.Fatalf("%s not registered", key)
		}
		recs, err := e.Extract(context.Background(), in)
		if err != nil {
			t.Fatalf("%s extract: %v", key, err)
		}
		for i := range recs {
			collect6976(got, &recs[i])
		}
	}
	return got
}

func collect6976(got map[string]struct {
	marked bool
	name   string
}, r *types.EntityRecord) {
	p := r.Properties["provenance"]
	if p == "" {
		return
	}
	// A provenance that fires more than once must agree with itself; OR-ing
	// would let one marked record vouch for an unmarked sibling.
	if prev, seen := got[p]; seen && prev.marked != (r.Properties[resolve.ReferenceShapedProp] == "true") {
		got[p] = struct {
			marked bool
			name   string
		}{false, prev.name + "|DISAGREES:" + r.Name}
		return
	}
	got[p] = struct {
		marked bool
		name   string
	}{r.Properties[resolve.ReferenceShapedProp] == "true", r.Name}
}

func TestReferenceShapedProducersAreMarked6976(t *testing.T) {
	got := extractByProvenance6976(t)
	for _, w := range wantMarked6976 {
		t.Run(w.provenance, func(t *testing.T) {
			v, fired := got[w.provenance]
			if !fired {
				t.Fatalf("fixture is vacuous: %s emitted no record, so this row asserts "+
					"nothing. Fix the fixture, not the expectation.", w.provenance)
			}
			if !v.marked {
				t.Errorf("%s emitted %q WITHOUT %s=true — %s. Unmarked, a mention of a name "+
					"still evicts the sole declaration of that name from the repository-wide "+
					"index and takes every bare-name edge to it down with it (#6976).",
					w.provenance, v.name, resolve.ReferenceShapedProp, w.why)
			}
		})
	}
}

func TestDeclarationShapedProducersAreNotMarked6976(t *testing.T) {
	got := extractByProvenance6976(t)
	for _, p := range wantUnmarked6976 {
		t.Run(p, func(t *testing.T) {
			v, fired := got[p]
			if !fired {
				t.Fatalf("fixture is vacuous: %s emitted no record, so this control asserts "+
					"nothing", p)
			}
			if v.marked {
				t.Errorf("%s emitted %q WITH %s=true, but it reads a DECLARATION. Marking it "+
					"costs it the repository-wide slot it is entitled to; #6976 is scoped to "+
					"records minted from a MENTION, not to the C# custom lane.",
					p, v.name, resolve.ReferenceShapedProp)
			}
		})
	}
}

// TestMarkedProducersAreExactlyTheMeasuredSix6976 closes the scope in the
// other direction: nothing in the fixtures above may acquire the marker
// without a row in wantMarked6976. Without this, a future "stamp it in
// makeEntity" shortcut would pass both tests above.
func TestMarkedProducersAreExactlyTheMeasuredSix6976(t *testing.T) {
	want := map[string]bool{}
	for _, w := range wantMarked6976 {
		want[w.provenance] = true
	}
	for p, v := range extractByProvenance6976(t) {
		if v.marked && !want[p] {
			t.Errorf("%s emitted %q with %s=true but is not one of the six measured "+
				"producers #6976 scopes the marker to. Either it is genuinely "+
				"reference-shaped — then add a row to wantMarked6976 with the measurement "+
				"that justifies it — or the stamp leaked.", p, v.name, resolve.ReferenceShapedProp)
		}
	}
}
