package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// #6916 (Tier B, aws_cdk arm): the two aws_cdk `Config` source_patterns used
// `name_group: 0`, so the entity's Name was the whole regex match —
// `"new cdk.App("` (JS/TS) and `"cdk.App()"` (Python).
//
// The relationship rule in the SAME file could therefore never bind:
//
//	source_type: Config
//	target_type: Component
//	relationship: CALLS
//	source_group: 2   // the VARIABLE in `new MyStack(app, 'Id')` -> "app"
//
// The edge's source name is the variable `app`; the entity's name was the
// marker string `new cdk.App(`. Those two strings can never be equal, so the
// edge shipped dangling at its source end — the #6906 `bug-extractor` shape.
//
// Narrowing the capture to the assigned variable (the express.yaml:91 shape)
// makes an ALREADY-EMITTED edge start binding. These tests pin the ARTEFACT:
// the entity's Name and the edge with BOTH endpoint IDs, driven through the
// real LoadAllRules() + Detector.Detect() path. A count is not evidence.
//
// The detector builds a relationship endpoint ID as "<type>:<captured name>"
// (detector.go:562) and an entity carries Kind + Name; the source end BINDS
// exactly when some emitted entity has Kind/Name equal to the edge's FromID
// halves. That equality is what these tests assert, in both directions.

// cdk6916TSApp is the canonical CDK entry point (`bin/app.ts`). The Stack class
// is declared in the same file so BOTH endpoints of the CALLS edge are emitted
// here and can be named.
const cdk6916TSApp = `import * as cdk from 'aws-cdk-lib';

export class ApiStack extends cdk.Stack {
}

const app = new cdk.App();
new ApiStack(app, 'ApiStack');
app.synth();
`

// cdk6916PyApp is the Python idiom. Python has no const/let/var, so the JS
// regex is NOT reusable here — this fixture is what pins that the Python rule
// was written for Python rather than copied.
const cdk6916PyApp = `import aws_cdk as cdk

class ApiStack(cdk.Stack):
    pass

app = cdk.App()
ApiStack(app, "ApiStack")
app.synth()
`

// cdk6916TSMultiDeclarator exists to grade the WIDENING mutant: replacing the
// variable capture `(\w+)` with `(.+)`. On a single-declarator line `(.+)` is
// still forced to stop at the `=`, so it captures "app" and is indistinguishable
// there. A multi-declarator line separates them: `(.+)` captures
// "region = 'eu-west-1', app" and the edge stops binding again.
const cdk6916TSMultiDeclarator = `import * as cdk from 'aws-cdk-lib';

export class ApiStack extends cdk.Stack {
}

const region = 'eu-west-1', app = new cdk.App();
new ApiStack(app, 'ApiStack');
`

// cdk6916PyAttrApp is the Python analogue and grades the SAME widening in the
// Python rule. Python has no declarator keyword, so the multi-declarator line
// above has no Python spelling; the shape that separates `(\w+)` from `(.+)` and
// from `([\w.]+)` here is an ATTRIBUTE target, `self.app = cdk.App()`, which is
// idiomatic when the App is built inside a class. `(\w+)` (with the rule's
// `(?:\w+\.)?` prefix consuming `self.`) names it "app"; the widened captures
// name it "self.app".
//
// CORRECTED (review ask 4): this fixture grades the NAME ONLY. It emits no
// relationship at all — the Python CALLS rule is `(\w+Stack)\s*\(\s*(\w+)\s*,`
// and `ApiStack(self.app, "ApiStack")` has no `,` directly after a bare `(\w+)`,
// so NEITHER spelling produces an edge here. The earlier comment implied "app"
// would bind and "self.app" would not; measured, neither does. The binding is
// graded by TestIssue6916_CDKCallsEdgeBindsAtItsSource, not here.
//
// The second method carries a MULTI-LEVEL attribute chain, `self.inner.app`. The
// rule's `(?:\w+\.)?` prefix is one level deep, so that line mints nothing —
// correct, because a dotted name could never equal the CALLS rule's `(\w+)`
// source anyway. It grades the dotted-capture mutant `(\w+)` -> `([\w.]+)`,
// which the single-level `self.app` line cannot: there the `(?:\w+\.)?` prefix
// already eats `self.` and both spellings name it "app".
const cdk6916PyAttrApp = `import aws_cdk as cdk

class ApiStack(cdk.Stack):
    pass


class Deployment:
    def build(self):
        self.app = cdk.App()
        ApiStack(self.app, "ApiStack")

    def build_nested(self):
        self.inner.app = cdk.App()
`

// cdk6916TSAnnotated carries a TYPE ANNOTATION, which is ordinary TypeScript and
// which no fixture covered (review ask 2 / mutant RM4). Deleting the rule's
// `(?:\s*:\s*[\w.]+)?` group makes `(\w+)` land inside the annotation and mint
// `Config:App` — the TYPE name — so the edge dangles again.
const cdk6916TSAnnotated = `import * as cdk from 'aws-cdk-lib';

export class ApiStack extends cdk.Stack {
}

const app: cdk.App = new cdk.App();
new ApiStack(app, 'ApiStack');
`

// cdk6916PyMultiTarget is the review BLOCKER made into a fixture. Python's
// `App()` is a plain call, so the text of a multi-target assignment contains the
// literal substring `env = cdk.App()`. An unanchored `(\w+)\s*=` captures `env`
// — which is the string "prod" — and the CALLS edge then BINDS TO THE WRONG
// VARIABLE. Before #6916 that edge merely dangled; a confidently wrong endpoint
// is worse than a known-bad one.
//
// The rule's `(?m)^[ \t]*` anchor makes this mint nothing instead. That is the
// deliberate cost and it is pinned as such: no Config entity, and no relationship
// sourced on a Config.
const cdk6916PyMultiTarget = `import aws_cdk as cdk

class EnvStack(cdk.Stack):
    pass

app, env = cdk.App(), "prod"
EnvStack(env, "EnvStack")
`

// cdk6916TSDestructured is the nearest JS/TS spelling of the same hazard. It is
// here to SHOW the JS rule does not have the seam rather than to assume it: the
// JS pattern requires `= new ... App(` immediately, so `(\w+)` can only ever be
// the token directly left of that `=`, which in JS is the assignment target.
// A destructuring form matches nothing at all.
const cdk6916TSDestructured = `import * as cdk from 'aws-cdk-lib';

export class EnvStack extends cdk.Stack {
}

const [app, env] = [new cdk.App(), 'prod'];
new EnvStack(env, 'EnvStack');
`

// cdk6916TSPlainAppCall / cdk6916TSNewAppCall are the OVER-FIRING pair (review
// ask 3 / coordinator mutant CV-1). They are the same file except for the word
// `new`. `App()` as a plain call is an ordinary React component invocation in a
// repo with no CDK anywhere; `new` is the only thing keeping this rule off it.
//
// The `new` leg is the POSITIVE CONTROL: it proves the rule can reach this file
// at all, so the absence asserted on the other leg is not vacuous. Both are
// deliberately free of any CDK marker.
const cdk6916TSPlainAppCall = `import App from './App';

function bootstrap() {
  const app = App();
  return app;
}
`

const cdk6916TSNewAppCall = `import App from './App';

function bootstrap() {
  const app = new App();
  return app;
}
`

// cdk6916TSBareApp / cdk6916PyBareApp are the construction with NO assignment.
// This is the deliberate cost of the narrowing and is pinned as such: an
// unassigned App mints no Config entity at all. It is also the fixture that
// kills a mutant re-widening the pattern to match without the assignment.
//
// Each carries the Stack class as its POSITIVE CONTROL: the file must still
// emit Component:ApiStack, so "no Config entity" cannot pass because the
// fixture stopped producing anything.
const cdk6916TSBareApp = `import * as cdk from 'aws-cdk-lib';

export class ApiStack extends cdk.Stack {
}

new cdk.App();
`

const cdk6916PyBareApp = `import aws_cdk as cdk

class ApiStack(cdk.Stack):
    pass

cdk.App()
`

func detect6916(t *testing.T, path, lang, src string) *DetectResult {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	res, err := New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

func rels6916(res *DetectResult) []string {
	out := make([]string, 0, len(res.Relationships))
	for _, r := range res.Relationships {
		out = append(out, fmt.Sprintf("%s --%s--> %s", r.FromID, r.Kind, r.ToID))
	}
	sort.Strings(out)
	return out
}

// entityIDs6916 renders every emitted entity in the same "<Kind>:<Name>" shape
// the detector uses for relationship endpoints, which is what makes the
// binding assertion below a real comparison rather than two separate greps.
func entityIDs6916(res *DetectResult) []string {
	out := make([]string, 0, len(res.Entities))
	for _, e := range res.Entities {
		out = append(out, fmt.Sprintf("%s:%s", e.Kind, e.Name))
	}
	sort.Strings(out)
	return out
}

// TestIssue6916_CDKAppConfigIsNamedAfterVariable pins the entity Name. Before
// the change it failed with Config:"new cdk.App(" / Config:"cdk.App()".
func TestIssue6916_CDKAppConfigIsNamedAfterVariable(t *testing.T) {
	for _, tc := range []struct {
		name, path, lang, src string
		// forbidden is the marker Name the shipped `name_group: 0` produced.
		forbidden string
	}{
		{"typescript", "bin/app.ts", "typescript", cdk6916TSApp, "new cdk.App("},
		{"typescript with type annotation", "bin/app.ts", "typescript", cdk6916TSAnnotated, "new cdk.App("},
		{"python", "app.py", "python", cdk6916PyApp, "cdk.App()"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, tc.path, tc.lang, tc.src))

			if !slices.Contains(ids, "Config:app") {
				t.Errorf("the CDK App entity is not named after its assigned variable; "+
					"expected Config:app among the emitted entities:\n  %s",
					strings.Join(ids, "\n  "))
			}
			if slices.Contains(ids, "Config:"+tc.forbidden) {
				t.Errorf("the #6916 marker Name is back: Config:%q. name_group must be the "+
					"variable capture, not 0 (the whole match).", tc.forbidden)
			}
		})
	}
}

// TestIssue6916_CDKCallsEdgeBindsAtItsSource is the point of the change: the
// already-shipped `Config CALLS Component` edge now resolves at BOTH ends.
// Both endpoint IDs are named explicitly.
func TestIssue6916_CDKCallsEdgeBindsAtItsSource(t *testing.T) {
	for _, tc := range []struct{ name, path, lang, src string }{
		{"typescript", "bin/app.ts", "typescript", cdk6916TSApp},
		{"typescript with type annotation", "bin/app.ts", "typescript", cdk6916TSAnnotated},
		{"python", "app.py", "python", cdk6916PyApp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := detect6916(t, tc.path, tc.lang, tc.src)
			rels := rels6916(res)
			ids := entityIDs6916(res)

			const want = "Config:app --CALLS--> Component:ApiStack"
			if !slices.Contains(rels, want) {
				t.Fatalf("expected the CDK app->stack edge %q; relationships emitted:\n  %s",
					want, strings.Join(rels, "\n  "))
			}

			// The binding itself: every endpoint of that edge must name an
			// entity this same run emitted. Before #6916 the source end,
			// Config:app, had no referent — the entity was Config:"new cdk.App(".
			for _, endpoint := range []string{"Config:app", "Component:ApiStack"} {
				if !slices.Contains(ids, endpoint) {
					t.Errorf("edge %q dangles: endpoint %s names no emitted entity. Entities:\n  %s",
						want, endpoint, strings.Join(ids, "\n  "))
				}
			}
		})
	}
}

// TestIssue6916_GreedyCaptureWouldBreakBinding grades the `(.+)` widening of
// the variable capture, in BOTH rules. The single-assignment fixtures cannot
// grade it: there `(.+)` is pinned by the `=` and captures "app" too, so it is
// indistinguishable. Each language needs the shape that separates them, and
// the two languages do NOT share one — JS/TS has a multi-declarator `const`
// line, Python has no declarator keyword at all and uses an attribute target.
func TestIssue6916_GreedyCaptureWouldBreakBinding(t *testing.T) {
	identifier := regexp.MustCompile(`^\w+$`)

	for _, tc := range []struct{ name, path, lang, src string }{
		{"typescript multi-declarator const", "bin/app.ts", "typescript", cdk6916TSMultiDeclarator},
		{"python attribute target", "app.py", "python", cdk6916PyAttrApp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, tc.path, tc.lang, tc.src))

			if !slices.Contains(ids, "Config:app") {
				t.Errorf("the CDK App Config entity must be named `app` here too; the variable "+
					"capture has to be identifier-shaped, not greedy. Entities:\n  %s",
					strings.Join(ids, "\n  "))
			}
			for _, id := range ids {
				name, ok := strings.CutPrefix(id, "Config:")
				if !ok {
					continue
				}
				// These fixtures carry no CfnOutput, so every Config here
				// comes from the App rule and must be a bare identifier —
				// anything else cannot equal the CALLS rule's `(\w+)` source.
				if !identifier.MatchString(name) {
					t.Errorf("a Config entity was minted with a non-identifier Name: %q. "+
						"The CALLS rule sources on `(\\w+)`, so such a name can never bind.", name)
				}
			}
		})
	}
}

// TestIssue6916_UnassignedAppMintsNoConfig pins the DELIBERATE COST of the
// narrowing, so it is a decision on the record rather than a silent regression.
// A bare `new cdk.App()` cannot be passed to a Stack constructor, so it has no
// edge to anchor; the shipped rule gave it a marker node instead.
func TestIssue6916_UnassignedAppMintsNoConfig(t *testing.T) {
	for _, tc := range []struct{ name, path, lang, src string }{
		{"typescript", "bin/app.ts", "typescript", cdk6916TSBareApp},
		{"python", "app.py", "python", cdk6916PyBareApp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, tc.path, tc.lang, tc.src))

			for _, id := range ids {
				// CfnOutput also mints Config; this fixture has none, so any
				// Config here comes from the App rule.
				if strings.HasPrefix(id, "Config:") {
					t.Errorf("an UNASSIGNED `App()` construction minted %q. The #6916 rule is "+
						"anchored on the assignment on purpose: without a variable there is "+
						"nothing for the CALLS edge to bind to.", id)
				}
			}

			// Positive control — without it the loop above passes vacuously
			// on a fixture that stopped producing anything at all.
			if !slices.Contains(ids, "Component:ApiStack") {
				t.Errorf("positive control missing: this fixture no longer emits "+
					"Component:ApiStack, so the absence assertion above proves nothing. Entities:\n  %s",
					strings.Join(ids, "\n  "))
			}
		})
	}
}

// TestIssue6916_MultiTargetAssignmentDoesNotMisattribute is the review blocker,
// pinned. The failure mode it guards is NOT "an edge goes missing" — it is "an
// edge binds to the wrong variable", which the rest of this file cannot see
// because the wrong name (`env`) is identifier-shaped and passes every other
// assertion here.
//
// Both legs assert the same two things: no Config entity is minted from a
// multi-target / destructuring assignment, and the `Config:env --CALLS-->` edge
// the relationship rule emits from `EnvStack(env, …)` therefore does NOT BIND.
//
// That edge is emitted either way — the relationship rule is independent of the
// entity rule and fires on the `EnvStack(env,` text alone. It dangled before
// #6916 and it must go on dangling: the whole point of this issue is that a
// dangling endpoint is a known-bad one, while an endpoint that resolves to the
// WRONG variable is a wrong answer stated confidently. Asserting "the edge is
// absent" would be false; asserting "the edge does not bind" is the real
// guarantee. Component:EnvStack is the positive control on each leg.
func TestIssue6916_MultiTargetAssignmentDoesNotMisattribute(t *testing.T) {
	for _, tc := range []struct {
		name, path, lang, src string
		// wrong is the name the unanchored capture donated. Named so a future
		// failure reads as this defect rather than as a general absence.
		wrong string
	}{
		{"python tuple assignment", "app.py", "python", cdk6916PyMultiTarget, "env"},
		{"typescript array destructuring", "bin/app.ts", "typescript", cdk6916TSDestructured, "env"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := detect6916(t, tc.path, tc.lang, tc.src)
			ids := entityIDs6916(res)
			rels := rels6916(res)

			for _, id := range ids {
				if strings.HasPrefix(id, "Config:") {
					t.Errorf("a multi-target assignment minted %q. The App is bound to `app`; "+
						"any other name here makes the CALLS edge resolve to the WRONG variable "+
						"(%q is a string literal), which is worse than the dangling edge it replaced.",
						id, tc.wrong)
				}
			}
			for _, r := range rels {
				src, _, ok := strings.Cut(r, " --")
				if !ok || !strings.HasPrefix(src, "Config:") {
					continue
				}
				if slices.Contains(ids, src) {
					t.Errorf("the edge %q BINDS: its source %s names an entity emitted by this "+
						"same run. It dangled before #6916 and must go on dangling — resolving it "+
						"to %q (a string literal, not the App) is a confidently wrong answer.",
						r, src, tc.wrong)
				}
			}

			// Positive control — without it both loops pass on a fixture that
			// stopped producing anything at all.
			if !slices.Contains(ids, "Component:EnvStack") {
				t.Errorf("positive control missing: this fixture no longer emits "+
					"Component:EnvStack, so the absence assertions above prove nothing. Entities:\n  %s",
					strings.Join(ids, "\n  "))
			}
		})
	}
}

// TestIssue6916_PlainAppCallIsNotACDKApp grades the rule in the OVER-FIRING
// direction — the direction every mutant on the first round of this change
// missed, and the one #7013 measured at 24.5% of minted framework entities
// landing in repos that do not use the framework (Config at 60.5%).
//
// The interaction is worth stating: making the Name useful also makes an
// over-firing hit HARDER to spot. Before #6916 a stray match was named `App(`,
// visibly junk; now it would be named `app`, which reads as a real CDK
// application.
func TestIssue6916_PlainAppCallIsNotACDKApp(t *testing.T) {
	// Negative: `const app = App()` in a file with no CDK anywhere.
	ids := entityIDs6916(detect6916(t, "src/bootstrap.tsx", "typescript", cdk6916TSPlainAppCall))
	for _, id := range ids {
		if strings.HasPrefix(id, "Config:") {
			t.Errorf("a plain `App()` call in a non-CDK file minted %q. `new` is the only thing "+
				"separating this rule from every JS file that calls a function named App().", id)
		}
	}

	// Positive control: the SAME file with `new` added must mint Config:app.
	// Without this the assertion above would also pass if the rule had stopped
	// firing on this path, this language, or this content entirely.
	ctrl := entityIDs6916(detect6916(t, "src/bootstrap.tsx", "typescript", cdk6916TSNewAppCall))
	if !slices.Contains(ctrl, "Config:app") {
		t.Errorf("positive control missing: `const app = new App()` at the same path no longer "+
			"mints Config:app, so the absence asserted above proves nothing about `new`. Entities:\n  %s",
			strings.Join(ctrl, "\n  "))
	}
}
