package engine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// #6914 (step 1): gin, chi, echo and fiber each carried the SAME relationship
// rule
//
//	pattern:      '\.Use\s*\(\s*(\w[\w.]*)\s*(?:,\s*(\w[\w.]*))?\s*\)'
//	source_type:  Middleware
//	target_type:  Controller
//	relationship: ROUTES_TO
//	source_group: 1
//	target_group: 2
//
// whose second capture group is optional. Driven through the real
// LoadAllRules() + Detector.Detect() path on the fixtures below, the rule was
// measured to be wrong in every direction it could take:
//
//	r.Use(middleware.Logger)                     -> group 2 empty, detector.go
//	                                                `continue`s: no edge, ever.
//	r.Use(middleware.Logger, middleware.Recoverer)
//	                                             -> FOUR copies (one per rule
//	                                                file) of
//	                                                Middleware:middleware.Logger
//	                                                --ROUTES_TO-->
//	                                                Controller:middleware.Recoverer
//	r.Use(a, b, c)                               -> matches nothing.
//	r.Use(gin.Recovery())                        -> `(\w[\w.]*)\s*\)` cannot
//	                                                match `()`: invisible.
//
// The two-argument edge is FABRICATED: `Use(A, B)` registers two middlewares
// on the router; it does not make A route to B. And it fires on ordinary chi
// source, so it is not a theoretical case. The four rules were deleted.
//
// These tests pin the ARTEFACT — the edges the shipped rules actually emit for
// a `.Use` line — not the rule count and not the YAML text. A count-of-rules or
// grep-the-YAML assertion passes the moment someone re-adds the edge in a
// different spelling; asserting the emitted graph does not.
//
// NOT in scope, deliberately: the `Middleware` ENTITY rules are untouched and
// TestIssue6914_UseEntityRuleStillMintsMiddleware below holds them in place —
// whether that shadow population should exist at all is #6914 step 4, and
// internal/quality/golden/go-chi-mini/expected.json asserts the entity today.
// A correct `Middleware REGISTERED_ON <app>` edge (step 2) is blocked on #6916
// (the `name_group: 0` router anchor); if it ever lands it will legitimately
// need this pin updated — deliberately, with the new edge named here.

// use6914TwoArgDotted and use6914TwoArgBare are the two-argument form in its
// TWO identifier shapes. Both are needed, and the first alone is not enough.
//
// The deleted pattern captured `(\w[\w.]*)` — dotted OR bare — so a fixture
// using only `middleware.Logger` pins the ARITY axis and leaves its neighbour,
// the IDENTIFIER SHAPE, wide open. Review mutant M-R5 exploited exactly that:
// re-adding the rule with the pattern narrowed to the house style already used
// by chi's own route rule, `'\.Use\s*\(\s*(\w+)\s*,\s*(\w+)\s*\)'`, is the
// same defect with the same source_type/target_type/kind/groups, fires on
// textbook gin middleware — and a dotted-only fixture sleeps through it.
//
// Each carries a route line as its POSITIVE CONTROL. Both files emit ZERO
// relationships in total once the rules are gone, so without a control edge an
// assertion of the form "no relationship touches a Middleware endpoint" would
// keep passing even if the constant stopped producing anything at all — a
// vacuous absence. The control edge is named per-fixture in the table below.
const use6914TwoArgDotted = `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Get("/health", healthHandler)
	http.ListenAndServe(":8080", r)
}
`

// use6914TwoArgMultiline is the SAME two-argument content in gofmt's own
// layout. Coordinator mutant CQ-1 re-added the defect with the arguments
// separated by a newline —
// `'\.Use\s*\(\s*(\w[\w.]*)\s*,\s*\n\s*(\w[\w.]*)'`, same
// source_type/target_type/kind/groups — and a table whose every leg put both
// arguments on ONE line slept through it. The deleted rules used `\s*`, and
// RE2's `\s` matches `\n`, so they DID fire on this layout; the pin covered it
// only incidentally, never by assertion, which is exactly what a narrower
// re-add walks through.
//
// Three axes are enumerated here and that is where this stops: arity (the
// NoCorrectEdgeWasLost test), identifier shape, and line layout. The goal is a
// pin that survives the obvious rewrites, not one that anticipates every
// possible regex.
const use6914TwoArgMultiline = `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		middleware.Logger,
	)
	r.Get("/orders", listOrders)
	http.ListenAndServe(":8080", r)
}
`

const use6914TwoArgBare = `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	v1 := r.Group("/v1")
	v1.Use(AuthRequired, Logger)
	r.GET("/v1/items", listItems)
	r.Run(":8080")
}
`

// use6914OneArg is the common single-argument form. It mints the Middleware
// ENTITY and — before and after the deletion alike — no edge.
const use6914OneArg = `package main

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
}
`

// use6914ThreeArg and use6914CallExpr are the two remaining forms. Both
// produced nothing before the deletion, so the deletion cannot have cost them
// anything — asserted rather than asserted-in-prose.
const use6914ThreeArg = `package main

func main() {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger)
}
`

const use6914CallExpr = `package main

func main() {
	r := gin.Default()
	r.Use(gin.Recovery())
}
`

func detect6914Go(t *testing.T, src string) *DetectResult {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	res, err := New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     "src/main.go",
		Content:  []byte(src),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

// rels6914 renders every relationship as "FromID --Kind--> ToID". The detector
// builds both endpoint IDs as "<type>:<captured name>", so the rendered string
// carries the endpoint KINDS as well as the names — which is what lets these
// assertions distinguish the `Middleware` KIND from an entity merely NAMED
// something middleware-ish (the name/kind confusion that bit the grounding
// sweep on nim-objects-mini's `from_name: "Config"`).
func rels6914(res *DetectResult) []string {
	out := make([]string, 0, len(res.Relationships))
	for _, r := range res.Relationships {
		out = append(out, fmt.Sprintf("%s --%s--> %s", r.FromID, r.Kind, r.ToID))
	}
	sort.Strings(out)
	return out
}

// TestIssue6914_TwoArgUseEmitsNoMiddlewareEdge is the regression pin. Before
// the deletion it failed with four copies of the fabricated edge.
//
// The assertion is on the endpoint KIND, not on the exact fabricated edge
// string, so it also kills a re-add that swaps the capture groups, reverses the
// direction, renames the captured symbols, or narrows the identifier pattern.
//
// KNOWN UNCOVERED, deliberately: a re-add that also RENAMES THE KINDS — say
// `source_type: Handler`, `target_type: Controller`, `relationship: CALLS` —
// slips past this, because nothing in the emitted edge still says "Middleware".
// Killing it would mean asserting that a `.Use` line emits NO relationship at
// all. That is true today, but it would fight #6914 step 2, whose intended
// `Middleware REGISTERED_ON <app>` edge comes off exactly these lines once
// #6916 unblocks a per-variable router anchor. Naming the gap here beats
// closing it in a way that a later, correct edge has to fight.
func TestIssue6914_TwoArgUseEmitsNoMiddlewareEdge(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		// control is an edge the fixture must STILL emit. Without it an
		// absence assertion cannot fail, and a fixture that quietly stopped
		// producing anything would read as a pass. Verified able to fail:
		// deleting the route line from either constant drops that fixture to
		// zero relationships and this assertion fires.
		//
		// It is a liveness control, NOT a pin on any one framework's route
		// rule — several Go rule files match the same route line, so flipping
		// gin.yaml's ROUTES_TO to LINKS_TO leaves the control edge standing.
		// Do not read a passing control as evidence about a specific rule.
		control string
	}{
		{
			name:    "dotted identifiers (chi middleware stack)",
			src:     use6914TwoArgDotted,
			control: "Route:/health --ROUTES_TO--> Controller:healthHandler",
		},
		{
			name:    "bare identifiers (gin group middleware)",
			src:     use6914TwoArgBare,
			control: "Route:/v1/items --ROUTES_TO--> Controller:listItems",
		},
		{
			name:    "gofmt multi-line layout (chi stack of three)",
			src:     use6914TwoArgMultiline,
			control: "Route:/orders --ROUTES_TO--> Controller:listOrders",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := rels6914(detect6914Go(t, tc.src))

			var offenders []string
			for _, r := range got {
				if strings.HasPrefix(r, "Middleware:") || strings.Contains(r, "--> Middleware:") {
					offenders = append(offenders, r)
				}
			}
			if len(offenders) > 0 {
				t.Errorf("a two-argument `.Use(A, B)` line emitted %d edge(s) touching a Middleware endpoint; "+
					"`Use(A, B)` registers two middlewares, it does not relate A to B:\n  %s",
					len(offenders), strings.Join(offenders, "\n  "))
			}

			if !slices.Contains(got, tc.control) {
				t.Errorf("positive control missing: this fixture no longer emits %q, so the "+
					"absence assertion above proves nothing. Relationships emitted:\n  %s",
					tc.control, strings.Join(got, "\n  "))
			}
		})
	}

	// Name the exact edge the four deleted rules produced, so a reader of a
	// future failure can tell this defect returning from some other rule
	// growing a Middleware endpoint.
	const fabricated = "Middleware:middleware.Logger --ROUTES_TO--> Controller:middleware.Recoverer"
	for _, r := range rels6914(detect6914Go(t, use6914TwoArgDotted)) {
		if r == fabricated {
			t.Errorf("the #6914 fabricated edge is back verbatim: %s", fabricated)
		}
	}
}

// TestIssue6914_UseEntityRuleStillMintsMiddleware holds the ENTITY rules in
// place. Only the four RELATIONSHIP rules were deleted; deleting the entity
// rules is #6914 step 4 and would break
// internal/quality/golden/go-chi-mini/expected.json. Without this, an
// over-broad "fix" that rips out the whole `.Use` block passes the pin above
// for the wrong reason.
func TestIssue6914_UseEntityRuleStillMintsMiddleware(t *testing.T) {
	res := detect6914Go(t, use6914OneArg)

	found := false
	for _, e := range res.Entities {
		if e.Kind == "Middleware" && e.Name == "middleware.Logger" {
			found = true
		}
	}
	if !found {
		t.Errorf("single-arg `r.Use(middleware.Logger)` no longer mints Middleware:middleware.Logger; "+
			"only the relationship rules were in scope for #6914 step 1. Entities: %v", res.Entities)
	}
}

// TestIssue6914_NoCorrectEdgeWasLost demonstrates the other half of the
// deletion's justification. The single-arg, three-arg and call-expression forms
// each produced ZERO relationships while the rules were still present, so the
// deletion cannot have removed a correct edge from any of them. Restoring the
// four rules leaves every expectation below unchanged — which is the point:
// what the rules contributed on these forms was nothing.
func TestIssue6914_NoCorrectEdgeWasLost(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"one arg (the common form)", use6914OneArg},
		{"three args", use6914ThreeArg},
		{"call expression (the commonest gin idiom)", use6914CallExpr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rels6914(detect6914Go(t, tc.src)); len(got) != 0 {
				t.Errorf("expected no relationships from this `.Use` form, got %d:\n  %s",
					len(got), strings.Join(got, "\n  "))
			}
		})
	}
}
