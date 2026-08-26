package engine

import (
	"strings"
	"testing"
)

// #6500 (arm A) — jsFuncSpan carried only (offset, name), so enclosingJSFuncAt
// was a nearest-PRECEDING walk with no end bound: a call site attributed to the
// last declaration that STARTED before it, whether or not that declaration's
// body still contained it. pyFuncSpan was a type alias of jsFuncSpan, so the
// Python spans inherited the same unboundedness — and could not have shared a
// brace-derived end even if one had existed.
//
// These tests grade the EMITTED ARTEFACT — the `source_caller` property that
// http_endpoint_resolve.go turns into a FETCHES edge, and that sse_edges.go
// folds into a Stream entity's identity — not the span slice, and not a count.
// A count can improve while attribution gets worse.
//
// Axis varied: whether a CLOSED sibling declaration sits between the true
// enclosing function and the call site.
// Axis held constant: the call shape (`fetch("...")` for JS, `requests.get`
// for Python), the endpoint path family, the file language, and the fact that
// a legitimate enclosing declaration exists. Every case below is a single-file
// module with exactly one call site, so the only thing distinguishing the cases
// is where that call site sits relative to the sibling's closing brace/dedent.
//
// The `Inside` cases are the direction controls: they pin that the walk still
// attributes when it should, so a mutant cannot pass by never attributing.

// callerFor runs the detector over one file and returns the `source_caller`
// property of the synthetic http_endpoint with the given ID. It fails the test
// if no such synthetic was emitted, so a case can never pass vacuously by
// losing its endpoint.
func callerFor(t *testing.T, language, path, src, endpointID string) string {
	t.Helper()
	_, res := runDetect(t, language, path, src)
	for _, e := range res.Entities {
		if e.ID == endpointID {
			return e.Properties["source_caller"]
		}
	}
	t.Fatalf("no synthetic emitted for %q (nothing to attribute)", endpointID)
	return ""
}

// TestJSSpanEnd_ClosedSiblingDoesNotCapture_6500 is the JS/TS half.
func TestJSSpanEnd_ClosedSiblingDoesNotCapture_6500(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		endpoint string
		want     string
	}{
		{
			// THE DEFECT. `helper` starts after `outerLoad` and CLOSES before
			// the call. Unbounded, the nearest preceding declaration is
			// `helper`; bounded, the innermost span containing the call is
			// `outerLoad`.
			name: "call trails a closed nested sibling inside the enclosing function",
			src: "export async function outerLoad() {\n" +
				"  function helper(x) { return x; }\n" +
				"  return fetch(\"/api/outer\");\n" +
				"}\n",
			endpoint: "http:GET:/api/outer",
			want:     "Function:outerLoad",
		},
		{
			// DIRECTION CONTROL: the call really IS inside the nested
			// function. Innermost-containing must still pick `helper`. A
			// mutant that simply stops attributing fails here.
			name: "call genuinely inside the nested function still attributes to it",
			src: "export async function outerLoad() {\n" +
				"  function helper() {\n" +
				"    return fetch(\"/api/inner\");\n" +
				"  }\n" +
				"  return helper;\n" +
				"}\n",
			endpoint: "http:GET:/api/inner",
			want:     "Function:helper",
		},
		{
			// DIRECTION CONTROL: no sibling at all. The plain shape must be
			// unaffected by bounding.
			name: "plain single function is unaffected",
			src: "export async function loadOrders() {\n" +
				"  return fetch(\"/api/orders\");\n" +
				"}\n",
			endpoint: "http:GET:/api/orders",
			want:     "Function:loadOrders",
		},
		{
			// BOUNDARY, observed at the artefact: the call-site offset the walk
			// receives is EXACTLY helper.end.
			//
			// The pass that emits a TS `fetch()` synthetic is the AST one
			// (http_endpoint_client_ast.go:114, `extraction_method="ast"`), which
			// hands the walk `int(call.StartByte())` — the `f` of `fetch` itself.
			// The regex pass (fetchCallRe) also runs, but its match begins on the
			// `[^\w$.]` byte BEFORE `fetch` (here the `}`, one lower) and its emit
			// is deduped away by the AST one. Measured on this fixture: AST offset
			// 70, regex offset 69, helper span [37,70). So abutting the call to the
			// sibling's `}` lands the AST offset on the end byte precisely.
			//
			// An end bound one byte too generous (`pos <= end`, or `end+1`) puts
			// this call back inside `helper`. Verified discriminating: clean emits
			// Function:outerLoad, `pos <= end` emits Function:helper.
			name: "call match begins exactly at the closed sibling's end offset",
			src: "export async function outerLoad() {\n" +
				"  function helper(x) { return x; }fetch(\"/api/boundary\");\n" +
				"  return 1;\n" +
				"}\n",
			endpoint: "http:GET:/api/boundary",
			want:     "Function:outerLoad",
		},
		{
			// Two closed siblings, deeper nesting: a naive scan that stops at
			// the FIRST `}` it sees would end `outerLoad` inside `helper` and
			// drop the call out of every span.
			name: "two closed nested siblings before the call",
			src: "export async function outerLoad() {\n" +
				"  function a() { return 1; }\n" +
				"  function b() { if (a()) { return 2; } return 3; }\n" +
				"  return fetch(\"/api/two\");\n" +
				"}\n",
			endpoint: "http:GET:/api/two",
			want:     "Function:outerLoad",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := callerFor(t, "typescript", "span.ts", tc.src, tc.endpoint); got != tc.want {
				t.Errorf("source_caller = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPySpanEnd_ClosedSiblingDoesNotCapture_6500 is the Python half. It exists
// because arm A SPLITS pyFuncSpan off the jsFuncSpan alias: Python has no
// braces, so its end is derived from indentation. Applying the JS brace scan to
// these fixtures produces a span that either swallows the file (no `{` to
// close) or collapses to nothing, and either way this test goes red — which is
// what stops the split from being a vacuous rename.
func TestPySpanEnd_ClosedSiblingDoesNotCapture_6500(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		endpoint string
		want     string
	}{
		{
			// THE DEFECT, Python shape. `helper` dedents before the call.
			name: "call trails a dedented nested def inside the enclosing def",
			src: "import requests\n" +
				"\n" +
				"def outer_load():\n" +
				"    def helper(x):\n" +
				"        return x\n" +
				"    return requests.get(\"/api/outer\")\n",
			endpoint: "http:GET:/api/outer",
			want:     "Function:outer_load",
		},
		{
			// DIRECTION CONTROL: genuinely inside the nested def.
			name: "call genuinely inside the nested def still attributes to it",
			src: "import requests\n" +
				"\n" +
				"def outer_load():\n" +
				"    def helper():\n" +
				"        return requests.get(\"/api/inner\")\n" +
				"    return helper\n",
			endpoint: "http:GET:/api/inner",
			want:     "Function:helper",
		},
		{
			// DIRECTION CONTROL: plain shape, no sibling.
			name: "plain single def is unaffected",
			src: "import requests\n" +
				"\n" +
				"def load_orders():\n" +
				"    return requests.get(\"/api/orders\")\n",
			endpoint: "http:GET:/api/orders",
			want:     "Function:load_orders",
		},
		{
			// A blank line inside the nested body must not be read as a dedent
			// (a blank line has no indentation to compare).
			name: "blank line inside the nested body is not a dedent",
			src: "import requests\n" +
				"\n" +
				"def outer_load():\n" +
				"    def helper(x):\n" +
				"        y = x\n" +
				"\n" +
				"        return y\n" +
				"    return requests.get(\"/api/blank\")\n",
			endpoint: "http:GET:/api/blank",
			want:     "Function:outer_load",
		},
		{
			// A def whose parameter list wraps across lines: the header end
			// cannot be found by taking the first newline after `def`.
			name: "multi-line parameter list on the nested def",
			src: "import requests\n" +
				"\n" +
				"def outer_load():\n" +
				"    def helper(\n" +
				"        x,\n" +
				"        y,\n" +
				"    ):\n" +
				"        return x + y\n" +
				"    return requests.get(\"/api/wrapped\")\n",
			endpoint: "http:GET:/api/wrapped",
			want:     "Function:outer_load",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := callerFor(t, "python", "span.py", tc.src, tc.endpoint); got != tc.want {
				t.Errorf("source_caller = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSpanEnd_NoEnclosingSpanBehaviourIsPreserved_6500 pins the case arm A
// deliberately does NOT change: a call site contained in NO span at all.
//
// Bounding spans makes "no enclosing function" a reachable state for the first
// time. Deciding what the 43 consumers of enclosingJSFuncAt should do in that
// state is arm B of #6500 and is BLOCKED on a policy decision, because for
// sse_edges.go and websocket_edges.go the caller is folded into an entity's
// IDENTITY (`channelPath := "/" + caller`) rather than into a property — so
// returning "" there would rename graph nodes, not merely blank a field.
//
// Arm A therefore keeps the nearest-preceding walk as the FALLBACK for a call
// site no span contains. These assertions record today's answers verbatim,
// including the ones that are still wrong, so that arm B changing them is a
// visible, deliberate diff rather than a silent one. Do not "fix" these
// expectations without the arm B decision.
func TestSpanEnd_NoEnclosingSpanBehaviourIsPreserved_6500(t *testing.T) {
	t.Run("js trailing top-level code after the last function closed", func(t *testing.T) {
		src := "export function loadOrders() {\n" +
			"  return 1;\n" +
			"}\n" +
			"\n" +
			"const warm = fetch(\"/api/trailing\");\n"
		// STILL WRONG, deliberately: `loadOrders` has closed. Arm B owns this.
		if got := callerFor(t, "typescript", "trailing.ts", src, "http:GET:/api/trailing"); got != "Function:loadOrders" {
			t.Errorf("source_caller = %q, want the preserved pre-#6500 fallback %q", got, "Function:loadOrders")
		}
	})

	t.Run("python trailing module-level code after the last def dedented", func(t *testing.T) {
		src := "import requests\n" +
			"\n" +
			"def load_orders():\n" +
			"    return 1\n" +
			"\n" +
			"WARM = requests.get(\"/api/trailing\")\n"
		// STILL WRONG, deliberately: `load_orders` has dedented. Arm B owns this.
		if got := callerFor(t, "python", "trailing.py", src, "http:GET:/api/trailing"); got != "Function:load_orders" {
			t.Errorf("source_caller = %q, want the preserved pre-#6500 fallback %q", got, "Function:load_orders")
		}
	})
}

// TestSpanEnd_IntervalIsHalfOpen_6500 pins the ONE byte that separates a
// correct end bound from a bound that is one too generous, on both sides
// (`end-1` inside, `end` outside), so the bound cannot be moved in either
// direction.
//
// An earlier revision of this file claimed the JS boundary was unreachable
// through an emitted artefact. Review disproved the conclusion, and chasing it
// down disproved the stated reason too, so both are recorded here.
//
// The old reason — "the offset the pipeline hands the walk is not the
// call-pattern match offset" — was half right and useless. A stack probe shows
// TWO passes see this call: the AST pass (http_endpoint_client_ast.go:114)
// with the `fetch` identifier's own start byte, and the fetchCallRe pass with
// the byte before it. The AST pass emits first and the regex emit is deduped
// away, so the offset that decides the artefact is the AST one. That is not an
// unreachable offset, it is just a different one — abut the call to the
// sibling's `}` and it lands exactly on the span end. The fixture is now the
// artefact-level row `call match begins exactly at the closed sibling's end
// offset` in TestJSSpanEnd_ClosedSiblingDoesNotCapture_6500 above, and it is
// what actually kills a `pos <= end` mutant on the JS side.
//
// What survives here is the PYTHON half plus a JS unit-level companion. The
// Python boundary genuinely is not reachable through an artefact: pySpanEnd
// ends a body at the START of the dedented line, indentation included, while
// any call still inside the enclosing body must be indented — so the call
// offset is always the span end plus that indentation, never equal to it. A
// call at column zero would have dedented out of the enclosing def too, which
// answers the same either way. Rather than manufacture a fixture to force the
// kill, the Python boundary is pinned on the walk, which all 43 consumers call
// directly with no intervening logic.
func TestSpanEnd_IntervalIsHalfOpen_6500(t *testing.T) {
	t.Run("js", func(t *testing.T) {
		src := "export async function outerLoad() {\n" +
			"  function helper(x) { return x; }\n" +
			"  return 1;\n" +
			"}\n"
		funcs := indexJSEnclosingFunctions(src)
		var helper jsFuncSpan
		for _, f := range funcs {
			if f.name == "helper" {
				helper = f
			}
		}
		if helper.end <= helper.offset {
			t.Fatalf("helper span has no usable end: %+v (spans %+v)", helper, funcs)
		}
		if got := enclosingJSFuncAt(funcs, helper.end-1); got != "helper" {
			t.Errorf("at helper.end-1 (last byte INSIDE the body) = %q, want %q", got, "helper")
		}
		if got := enclosingJSFuncAt(funcs, helper.end); got != "outerLoad" {
			t.Errorf("at helper.end (first byte OUTSIDE the body) = %q, want %q", got, "outerLoad")
		}
	})

	t.Run("python", func(t *testing.T) {
		src := "import requests\n" +
			"\n" +
			"def outer_load():\n" +
			"    def helper(x):\n" +
			"        return x\n" +
			"    return 1\n"
		funcs := indexPyEnclosingFunctions(src)
		var helper pyFuncSpan
		for _, f := range funcs {
			if f.name == "helper" {
				helper = f
			}
		}
		if helper.end <= helper.offset {
			t.Fatalf("helper span has no usable end: %+v (spans %+v)", helper, funcs)
		}
		if got := enclosingPyFuncAt(funcs, helper.end-1); got != "helper" {
			t.Errorf("at helper.end-1 (last byte INSIDE the body) = %q, want %q", got, "helper")
		}
		if got := enclosingPyFuncAt(funcs, helper.end); got != "outer_load" {
			t.Errorf("at helper.end (first byte OUTSIDE the body) = %q, want %q", got, "outer_load")
		}
	})
}

// TestSpanEnd_UnestablishableEndClaimsNothing_6500 grades the degenerate case:
// a declaration jsSpanEnd cannot find an end for.
//
// A TS overload / ambient signature has no body at all, so the header scan hits
// `;` and gives up. The question is what such a span should then claim, and the
// permissive answer — "everything to end of file" — is invisible to every other
// test in this file, because in the usual layout the bodyless declaration sits
// ABOVE a real function whose span wins on innermost-ness anyway.
//
// It becomes visible only when the degenerate declaration is NOT the nearest
// preceding one. Here `ghost` is declared first, `loadOrders` opens and closes
// after it, and the call trails both. No span contains the call, so the
// preserved fallback answers with the nearest preceding name — `loadOrders`. A
// degenerate span that claimed the rest of the file would contain the call and
// out-rank the fallback, answering `ghost`: a name from a declaration that has
// no body and could not have called anything.
//
// So: an end that could not be established must claim NOTHING, not everything.
// That is what keeps every failure mode of the brace scan degrading to the
// pre-#6500 answer instead of inventing a new wrong one.
func TestSpanEnd_UnestablishableEndClaimsNothing_6500(t *testing.T) {
	src := "declare function ghost(id: string): void;\n" +
		"\n" +
		"export function loadOrders() {\n" +
		"  return 1;\n" +
		"}\n" +
		"\n" +
		"const warm = fetch(\"/api/degenerate\");\n"

	// The behavioural assertion comes FIRST, so that a mutant which makes the
	// degenerate span greedy is caught by the emitted artefact rather than by
	// the diagnostic below it.
	got := callerFor(t, "typescript", "degenerate.ts", src, "http:GET:/api/degenerate")
	if got != "Function:loadOrders" {
		t.Errorf("source_caller = %q, want %q (a bodyless declaration must claim no call sites)", got, "Function:loadOrders")
	}

	// Premise guard, reported after the fact: if jsSpanEnd ever learns to bound
	// `ghost`, this test stops exercising the degenerate path and must be
	// rewritten. It is a diagnostic, not the pin.
	for _, f := range indexJSEnclosingFunctions(src) {
		if f.name == "ghost" && f.end != f.offset {
			t.Errorf("premise gone: ghost span is now bounded %+v — this test no longer exercises the degenerate path", f)
		}
	}
}

// TestJSSpanEnd_UnmaskedBraceOverExtends_6500 pins the failure mode #6500's
// first revision wrongly claimed could not happen.
//
// That revision asserted, in the PR body and in three code comments, that
// "every failure mode of jsSpanEnd and pySpanEnd produces a span that is too
// small", so a mis-computed end "degrades to the status quo and never invents a
// new wrong caller". A `{` inside an ordinary string falsifies it. The brace
// scan is blind to quoted text, so the `"{"` below leaves it one level down and
// the closing `}` of `a` is consumed as if it balanced the string's brace. `a`
// then runs to the `}` in the trailing `"}"` string — 115 bytes, swallowing the
// whole of `b` and the module-level call after it.
//
// This is the direction that actually costs something:
//
//   - too SMALL: the call falls out of the span onto the preserved fallback,
//     which is the pre-#6500 answer. No regression.
//   - too LARGE: the span CONTAINS call sites outside the real body and
//     out-ranks the fallback, so the answer differs from what pre-#6500 code
//     produced. Here the nearest-preceding walk answered `b`; the bounded walk
//     answers `a`. And because sse_edges.go builds a Stream entity's ID as
//     `"/" + caller`, that difference renames a node rather than mis-labelling
//     a property.
//
// The row asserts `a` — today's wrong answer — deliberately, in the same spirit
// as TestSpanEnd_NoEnclosingSpanBehaviourIsPreserved_6500. The fix is to mask
// string, template and comment bytes before the brace scan, which is owed from
// #6447 and is a whole-pattern change, not an arm A change. When that lands
// this row should answer `b` (or better, `""`); update it deliberately then.
func TestJSSpanEnd_UnmaskedBraceOverExtends_6500(t *testing.T) {
	src := "function a() {\n" +
		"  const s = \"{\";\n" +
		"  return 1;\n" +
		"}\n" +
		"function b() { return 2; }\n" +
		"const warm = fetch(\"/api/overextend\");\n" +
		"const t = \"}\";\n"

	// The behavioural claim first: an over-extended span really does reach the
	// artefact and really does differ from the nearest-preceding answer.
	got := callerFor(t, "typescript", "overextend.ts", src, "http:GET:/api/overextend")
	if got != "Function:a" {
		t.Errorf("source_caller = %q, want %q — the over-extension this test exists to record has changed; "+
			"if string masking landed, the honest answer is now %q and this row should be updated, not deleted",
			got, "Function:a", "Function:b")
	}

	// And the mechanism, so a future reader can see WHY without re-deriving it:
	// `a` must genuinely span past `b`, not merely happen to win the walk.
	var spanA, spanB jsFuncSpan
	for _, f := range indexJSEnclosingFunctions(src) {
		switch f.name {
		case "a":
			spanA = f
		case "b":
			spanB = f
		}
	}
	if !(spanA.end > spanB.offset) {
		t.Errorf("premise gone: a=%+v no longer swallows b=%+v — the brace scan stopped over-extending", spanA, spanB)
	}
}

// TestJSSpanEnd_HeaderBudgetCosts_6500 observes what jsSpanHeaderBudget does,
// rather than what its comment used to claim it prevented.
//
// The old comment said the budget stopped a runaway scan from "minting an
// overlapping span, which is worse than no span at all". Nothing observed that,
// and a review mutant raising the budget to 1<<30 survived the whole
// internal/engine suite. What the budget demonstrably DOES is give up on a
// header longer than 512 bytes between `)` and `{`, leaving that declaration
// with no end at all — so it claims none of its own body, and a call site
// inside it falls through to the preceding declaration.
//
// The 652-byte generic return annotation below is ordinary, if ugly,
// TypeScript. `big` gets no span, so the call in its body attributes to the
// closed nested `helper` — the exact pre-#6500 mis-attribution, reintroduced by
// the budget rather than prevented by it.
//
// This row pins a LIMITATION, not a desired answer. Raising the budget makes
// this case answer "Function:big", which is CORRECT and an improvement; if you
// do that, update this expectation deliberately rather than deleting the row.
// It exists so that changing the constant is a visible diff.
func TestJSSpanEnd_HeaderBudgetCosts_6500(t *testing.T) {
	long := "Record<string, " + strings.Repeat("Array<", 90) + "string" + strings.Repeat(">", 90) + ">"
	src := "export function big(x: string): " + long + " {\n" +
		"  function helper() { return 1; }\n" +
		"  return fetch(\"/api/big\");\n" +
		"}\n"

	// Behaviour first, so that a change to the budget is caught by the emitted
	// artefact rather than by the premise diagnostic below it.
	got := callerFor(t, "typescript", "big.ts", src, "http:GET:/api/big")
	if got != "Function:helper" {
		t.Errorf("source_caller = %q, want %q (the budget's cost). If the budget was raised, "+
			"the answer becomes %q, which is better — update this row deliberately",
			got, "Function:helper", "Function:big")
	}

	// Premise diagnostic, reported after the fact, never instead of it.
	if len(long) <= jsSpanHeaderBudget {
		t.Errorf("premise gone: annotation is %d bytes, budget is %d — this fixture no longer exceeds it",
			len(long), jsSpanHeaderBudget)
	}
}
