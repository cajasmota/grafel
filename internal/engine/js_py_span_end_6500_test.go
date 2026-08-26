package engine

import "testing"

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
// correct end bound from a bound that is one too generous.
//
// This is the only assertion in this file that is not on the emitted artefact,
// and it is deliberate. The boundary state is `pos == end` — a call site
// beginning on the very byte a sibling's body ended. Every emitted-artefact
// fixture attempted for it missed by a byte: the call-site offsets the
// synthesis passes hand to the walk are the offsets of their own call-pattern
// matches, and none of them can be made to coincide exactly with a span end in
// source that is still valid JS/Python. Manufacturing one would have been
// forcing a kill rather than observing a behaviour.
//
// So the boundary is pinned where it lives: on the walk itself, which all 43
// consumers call directly with no intervening logic. The artefact-level tests
// above prove the walk's answer reaches `source_caller`; this proves the walk's
// answer is right at the boundary. Both sides are asserted, so a mutant cannot
// pass by moving the bound in EITHER direction.
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
