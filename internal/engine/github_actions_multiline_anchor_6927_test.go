package engine

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// #6927 — the cicd arm. The same defect #6917 fixed for sinatra and #6943 for
// python, in rules/cicd/frameworks/github_actions.yaml. detector.go compiles
// rule patterns with plain regexp.Compile at :156 and :177, so `^` means START
// OF TEXT. Six patterns were affected:
//
//	^name:\s+["']?([^"'\n]+)                       -> Service
//	^\s{2}(\w[\w-]*):\s*\n\s+(?:runs-on|uses):     -> Operation
//	^\s+-\s+name:\s+["']?([^"'\n]+)                -> Task
//	^on:\s*\n\s+(\w+):                             -> Config
//	^on:\s+(\w+)                                   -> Config
//	^env:\s*\n\s+(\w+):                            -> Config
//
// Three of them could never fire AT ALL — `\s{2}`, `\s+-` and a preceding
// `env:` block all require something before the match, and start-of-text
// forbids it. The other three fired only on a file whose very first byte was
// `name:` or `on:`.
//
// Two things are graded here that the golden fixture
// (internal/quality/golden/github-actions-workflows-mini) structurally cannot
// see, and that is why this file exists alongside it:
//
//  1. The Task rule's blast radius. ansible_core.yaml carries an UN-anchored
//     `-\s+name:` -> Task rule in the same `yaml` bucket, so every Task the
//     GitHub Actions rule could over-emit is already in the graph from the
//     other rule file. At graph level the gate is invisible; against a
//     detector holding ONLY this rule set it is not.
//  2. Which RULE produced an entity. The fixture asserts what the graph
//     contains; these tests assert what this one rule file contributes.
//
// Graded in BOTH directions (#6902), because a recall assertion is
// structurally blind to over-firing.

// ghaOnlyRules returns the loaded rule map reduced to the GitHub Actions rule
// set alone. The rules are the REAL embedded YAML — the defect IS the shipped
// pattern text, so a hand-written copy would leave the shipped one unobserved
// — but every other yaml rule set is dropped, so nothing another rule file
// mints can be mistaken for evidence about this one.
func ghaOnlyRules(t *testing.T) map[string][]FrameworkRule {
	t.Helper()
	all, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	out := map[string][]FrameworkRule{}
	for lang, frs := range all {
		for _, fr := range frs {
			if fr.Frameworks.Name == "GitHub Actions" {
				out[lang] = append(out[lang], fr)
			}
		}
	}
	// The loader keys rule sets by their BUCKET directory (`cicd`); it is
	// compile() that aliases the bucket onto the `yaml` language via
	// dormantBucketAliases, which is why these tests hand Detect a
	// Language of "yaml" while the map key stays "cicd". If that stops being
	// true the tests are measuring nothing, so it is asserted rather than
	// assumed.
	if len(out["cicd"]) != 1 {
		t.Fatalf("expected exactly one GitHub Actions rule set in the cicd bucket, got %d",
			len(out["cicd"]))
	}
	return out
}

func detect6927GHA(t *testing.T, path, src string) *DetectResult {
	t.Helper()
	det := New(ghaOnlyRules(t))
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "yaml",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

func has6927GHA(res *DetectResult, kind, name string) bool {
	for _, e := range res.Entities {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

func names6927GHA(res *DetectResult, kind string) []string {
	var out []string
	for _, e := range res.Entities {
		if e.Kind == kind {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

// gha6927Workflow is the fixture's build.yml, held here as a const so the
// engine-level assertions and the golden fixture describe the SAME source. It
// opens with a comment header — the shape every real workflow has, and the
// reason start-of-text found a `name:` in only 99 of the 269 workflow files in
// the measured corpus.
const gha6927Workflow = `# Storefront build pipeline.
#
# Deliberately NOT under .github/.
name: Storefront CI
on:
  pull_request:
    branches: [main]
  push:
    branches: [main]
env:
  REGISTRY: ghcr.io
jobs:
  build:
    runs-on: ubuntu-latest
    name: Build and test
    env:
      BUNDLE_PATH: vendor/bundle
    strategy:
      matrix:
        runs-on: [ubuntu-latest, macos-latest]
    steps:
      - uses: actions/checkout@v4
      - name: Compile assets
        run: make assets
      # - name: Deploy to staging
      #   run: make deploy
      - uses: actions/setup-node@v4
        with:
          node-version: 20
      - name: Upload bundle
        run: make upload
  publish:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - uses: actions/upload-artifact@v4
      - name: Push image
        run: docker push "$REGISTRY/storefront"
  notification:
    needs: [build, publish]
    runs-on: ubuntu-latest
    steps:
      - name: Post build result
        run: ./ci/notify.sh
`

// gha6927ReusableCaller exercises the `uses:` half of the job rule's
// alternation: a reusable-workflow call has no `runs-on:` anywhere in the job.
const gha6927ReusableCaller = `# Nightly smoke run.
name: Nightly Smoke
on: workflow_dispatch
jobs:
  smoke:
    uses: ./.github/workflows/build.yml
env:
  SMOKE_REGION: eu-west-1
`

// TestIssue6927_GitHubActions_WorkflowYieldsJobsTriggersAndEnv is the recall
// direction. Vertical position is the axis it varies: nothing sits on line 1,
// the second job is far down the file, and the reusable caller's `env:` block
// is the last construct in its file.
func TestIssue6927_GitHubActions_WorkflowYieldsJobsTriggersAndEnv(t *testing.T) {
	res := detect6927GHA(t, "ci/workflows/build.yml", gha6927Workflow)

	for _, want := range []struct{ kind, name string }{
		{"Service", "Storefront CI"},
		{"Operation", "build"},
		{"Operation", "publish"},
		{"Config", "pull_request"},
		{"Config", "REGISTRY"},
		{"Task", "Compile assets"},
		{"Task", "Upload bundle"},
		{"Task", "Push image"},
		{"Task", "Post build result"},
	} {
		if !has6927GHA(res, want.kind, want.name) {
			t.Errorf("%s %q not extracted from a realistic workflow; Services=%v Operations=%v "+
				"Configs=%v Tasks=%v", want.kind, want.name,
				names6927GHA(res, "Service"), names6927GHA(res, "Operation"),
				names6927GHA(res, "Config"), names6927GHA(res, "Task"))
		}
	}

	res2 := detect6927GHA(t, "ci/workflows/nightly.yml", gha6927ReusableCaller)
	for _, want := range []struct{ kind, name string }{
		{"Service", "Nightly Smoke"},
		// The `uses:` half of `(?:runs-on|uses)`. Without this the alternation
		// is graded on one branch only.
		{"Operation", "smoke"},
		// `on: <event>` on one line, which the block-form rule cannot reach.
		{"Config", "workflow_dispatch"},
		// Last construct in the file — the far end of the vertical axis.
		{"Config", "SMOKE_REGION"},
	} {
		if !has6927GHA(res2, want.kind, want.name) {
			t.Errorf("%s %q not extracted from a reusable-workflow caller; Services=%v "+
				"Operations=%v Configs=%v", want.kind, want.name,
				names6927GHA(res2, "Service"), names6927GHA(res2, "Operation"),
				names6927GHA(res2, "Config"))
		}
	}
}

// TestIssue6927_GitHubActions_AnchorsHoldInsideAWorkflow is the forbidden
// direction on the ANCHOR axis, inside a file that SATISFIES the framework
// gate. Every row here is held by `^` alone; none of them is held by
// requires_framework, which is what keeps the two fences distinguishable.
func TestIssue6927_GitHubActions_AnchorsHoldInsideAWorkflow(t *testing.T) {
	res := detect6927GHA(t, "ci/workflows/build.yml", gha6927Workflow)

	for _, bad := range []struct{ kind, name, why string }{
		{"Service", "Compile assets",
			"`^name:` is column-0 only; a step's indented `name:` is a Task, not the workflow"},
		{"Service", "Push image", "same, for a step in the second job"},
		{"Service", "Build and test",
			"a JOB's indented `name:` — the display-name key every workflow may carry. This is " +
				"the row that kills `^name:` -> `^\\s*name:`: a step's `- name:` opens with a " +
				"dash, so `\\s*` alone cannot reach it and the mutant survived every other row"},
		{"Config", "BUNDLE_PATH",
			"`^env:` distinguishes the WORKFLOW-level env block from the job-level one this key sits in"},
		{"Operation", "matrix",
			"`\\s{2}` is exactly two; `strategy.matrix` is deeper and is not a job"},
		{"Operation", "strategy", "same, one level up"},
		{"Config", "needs",
			"`notification:` is a legal job name ENDING in `on:`; without `^` both on: rules read the next key"},
		{"Config", "20",
			"`node-version: 20` contains the substring `on: 20`; without `^` the single-event rule mints it"},
		{"Task", "Deploy to staging",
			"a commented-out step. `^\\s+-` requires the dash to open the line's content"},
	} {
		if has6927GHA(res, bad.kind, bad.name) {
			t.Errorf("over-fire: %s %q was extracted from a workflow — %s", bad.kind, bad.name, bad.why)
		}
	}
}

// gha6927HelmChart, gha6927AnsiblePlay and gha6927TravisConfig are plain YAML.
// None names GitHub Actions anywhere, and each carries one of the three shapes
// the widened patterns would otherwise claim.
const gha6927HelmChart = `apiVersion: v2
name: storefront-chart
description: Storefront deployment chart
type: application
version: 0.4.1
`

const gha6927AnsiblePlay = `- hosts: web
  become: true
  tasks:
    - name: Install nginx
      apt:
        name: nginx
        state: present
    - name: Restart nginx
      service:
        name: nginx
        state: restarted
`

const gha6927TravisConfig = `language: ruby
rvm:
  - 3.1
env:
  MATRIX_KEY: fast
script:
  - bundle exec rake
`

// TestIssue6927_GitHubActions_GateHoldsPlainYAML is the forbidden direction on
// the FRAMEWORK-GATE axis, and the reason this change is not `(?m)` alone.
//
// `^name:`, `^\s+- name:` and `^env:` are plain YAML with nothing about GitHub
// Actions in them, and Detect resolves rule sets by file.Language while the
// cicd bucket is aliased onto `yaml`, so under `(?m)` alone every chart,
// playbook and CI config in a repo would be typed as workflow parts. Measured
// on 671 real YAML files from 59 upstream repos: 34 `^name:` matches in 34
// non-workflow files, 194 step matches in 49, 3 env matches in 3 — against
// ZERO for all three once `requires_framework: true` is on.
//
// The Task half of this cannot be graded by the golden fixture at all:
// ansible_core.yaml's un-anchored `- name:` rule mints those Tasks anyway, so
// the graph looks identical with the gate on or off. Here it does not.
func TestIssue6927_GitHubActions_GateHoldsPlainYAML(t *testing.T) {
	for _, tc := range []struct {
		path, src string
		bad       []struct{ kind, name string }
	}{
		{"deploy/helm/Chart.yaml", gha6927HelmChart, []struct{ kind, name string }{
			{"Service", "storefront-chart"},
		}},
		{"ops/provision.yml", gha6927AnsiblePlay, []struct{ kind, name string }{
			{"Task", "Install nginx"},
			{"Task", "Restart nginx"},
		}},
		{"ops/legacy-ci.yml", gha6927TravisConfig, []struct{ kind, name string }{
			{"Service", "language"},
			{"Config", "MATRIX_KEY"},
		}},
	} {
		res := detect6927GHA(t, tc.path, tc.src)
		for _, bad := range tc.bad {
			if has6927GHA(res, bad.kind, bad.name) {
				t.Errorf("over-fire: %s %q extracted from %s, which names GitHub Actions "+
					"nowhere — requires_framework is not holding", bad.kind, bad.name, tc.path)
			}
		}
		if len(res.Entities) != 0 {
			var got []string
			for _, e := range res.Entities {
				got = append(got, e.Kind+":"+e.Name)
			}
			sort.Strings(got)
			t.Errorf("%s: the GitHub Actions rule set extracted %d entities from plain YAML: %v",
				tc.path, len(res.Entities), got)
		}
	}
}

// ghaSourcePatterns returns the shipped GitHub Actions source_patterns.
func ghaSourcePatterns(t *testing.T) []SourcePattern {
	t.Helper()
	return ghaOnlyRules(t)["cicd"][0].SourcePatterns
}

// TestIssue6927_GitHubActions_NoDollarInWidenedPatterns pins the reason the fix
// is per-pattern rather than at the compile site. `(?m)` changes `$` as well as
// `^`, and this repo ships ungated `$`-bearing patterns elsewhere (#6927 names
// docker_compose and ansible_core). None of the patterns in THIS file carries a
// `$`, so the second effect cannot reach them — asserted rather than claimed,
// because a later edit that adds one would otherwise change the meaning of
// every `(?m)` here silently.
func TestIssue6927_GitHubActions_NoDollarInWidenedPatterns(t *testing.T) {
	multiline := 0
	for _, sp := range ghaSourcePatterns(t) {
		if !strings.HasPrefix(sp.Pattern, "(?m)") {
			continue
		}
		multiline++
		if strings.Contains(sp.Pattern, "$") {
			t.Errorf("pattern %q is (?m) AND carries a `$` — under (?m) that becomes "+
				"end-of-LINE, which is a behaviour change nothing here grades", sp.Pattern)
		}
	}
	if multiline != 6 {
		t.Errorf("found %d (?m) source_patterns in github_actions.yaml, want 6 — the sweep in "+
			"#6927 counted six `^`-anchored patterns in this file, so a different number means "+
			"this test's coverage claim needs re-deriving", multiline)
	}
}

// TestIssue6927_GitHubActions_GatedPatternsAreLoaded is the #6152-shaped half:
// requires_framework is inert without import_markers (detector.go logs it and
// frameworkPresent then returns false for every file), so a gate that looks
// applied in the YAML can be a no-op. Both halves are asserted.
func TestIssue6927_GitHubActions_GatedPatternsAreLoaded(t *testing.T) {
	fr := ghaOnlyRules(t)["cicd"][0]
	if len(fr.Frameworks.Detection.ImportMarkers) == 0 {
		t.Fatal("github_actions.yaml declares no frameworks.detection.import_markers, so every " +
			"requires_framework pattern in it can never fire")
	}
	var gated []string
	for _, sp := range fr.SourcePatterns {
		if sp.RequiresFramework {
			gated = append(gated, sp.Pattern)
		}
	}
	sort.Strings(gated)
	if len(gated) != 3 {
		t.Errorf("expected 3 requires_framework patterns (the workflow name, the step name and "+
			"the workflow-level env block — the three that are plain YAML), got %d: %v",
			len(gated), gated)
	}
	// And the markers must actually select a workflow. A marker set that
	// matches nothing is the same no-op wearing a different hat.
	det := New(ghaOnlyRules(t))
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path: "ci/workflows/build.yml", Content: []byte(gha6927Workflow), Language: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !has6927GHA(res, "Service", "Storefront CI") {
		t.Error("the marker set does not admit a realistic workflow, so the gate is not a gate " +
			"but an off switch")
	}
}

// TestIssue6927_GitHubActions_EachImportMarkerAdmitsAWorkflowAlone grades the
// marker set MARKER BY MARKER, in the recall direction.
//
// PR #6945 review M2: the only marker mutant originally scored was "replace the
// set with one that matches nothing", and deleting a SINGLE marker was ALIVE at
// the engine suite, the fixture gate AND the ratchet ceiling. The fixture's
// workflows are over-determined — build.yml satisfies three markers at once and
// nightly.yml satisfies `.github/workflows/` through its `uses:` target — so
// dropping any one of them changes nothing anywhere, while the input that
// exercises it is the most ordinary workflow there is: `runs-on:` is the only
// marker a `run:`-only workflow carries, and under that deletion it loses its
// Service and every step Task.
//
// Each case therefore carries EXACTLY ONE marker, and that premise is asserted
// rather than eyeballed: a case that quietly grew a second marker would grade
// the set again instead of the marker, which is the defect this test exists to
// close.
func TestIssue6927_GitHubActions_EachImportMarkerAdmitsAWorkflowAlone(t *testing.T) {
	markers := ghaOnlyRules(t)["cicd"][0].Frameworks.Detection.ImportMarkers
	if len(markers) != 4 {
		t.Fatalf("expected 4 import_markers, got %d: %v — this test carries one minimal "+
			"workflow per marker and a new one would be ungraded", len(markers), markers)
	}

	cases := []struct {
		marker string
		src    string
		// want is one gated entity the workflow must yield. A gated pattern is
		// the only thing the marker can affect, so an un-gated one would pass
		// with the gate wide open.
		wantKind, wantName string
	}{
		{
			marker: "runs-on:",
			// The plainest workflow there is: no marketplace action, no
			// expression, no reusable call. `runs-on:` is its ONLY marker.
			src: `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Build it
        run: make
`,
			wantKind: "Service", wantName: "CI",
		},
		{
			marker: "uses: actions/",
			src: `name: Checkout only
on: push
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
`,
			wantKind: "Service", wantName: "Checkout only",
		},
		{
			marker: "${{",
			src: `name: Expression only
on: push
jobs:
  deploy:
    steps:
      - name: Print the sha
        run: echo ${{ github.sha }}
`,
			wantKind: "Task", wantName: "Print the sha",
		},
		{
			marker: ".github/workflows/",
			src: `name: Reusable caller
on: workflow_dispatch
jobs:
  call:
    uses: ./.github/workflows/build.yml
`,
			wantKind: "Service", wantName: "Reusable caller",
		},
	}

	if len(cases) != len(markers) {
		t.Fatalf("%d cases for %d markers", len(cases), len(markers))
	}
	for _, tc := range cases {
		// Premise: exactly one marker, and it is the one this case is for.
		var present []string
		for _, m := range markers {
			if strings.Contains(tc.src, m) {
				present = append(present, m)
			}
		}
		if len(present) != 1 || present[0] != tc.marker {
			t.Errorf("case %q carries markers %v, want exactly [%q] — it grades the marker SET, "+
				"not this marker", tc.marker, present, tc.marker)
			continue
		}
		res := detect6927GHA(t, "ci/workflows/case.yml", tc.src)
		if !has6927GHA(res, tc.wantKind, tc.wantName) {
			t.Errorf("marker %q alone did not admit a workflow: %s %q missing. Deleting this one "+
				"marker silently drops every gated entity from a workflow that carries only it",
				tc.marker, tc.wantKind, tc.wantName)
		}
	}
}

// TestIssue6927_GitHubActions_JobRuleReadsOnlyRunsOnAndUses grades the
// `(?:runs-on|uses)` alternation, which is the ENTIRE justification for leaving
// the job rule ungated: the argument for no requires_framework there is that
// the regex names GitHub Actions' own job vocabulary itself.
//
// PR #6945 review M3: widening it to `(?:runs-on|uses|needs)` was ALIVE at the
// engine suite AND at the fixture gate, and died only incidentally on the
// ratchet's generic entity-count ceiling — which fires for any extra entity and
// would not fire at all for a same-count substitution. An aggregate that
// somebody will eventually re-baseline must not be the only thing holding a
// rule's blast radius.
//
// Asserted as an EXACT set rather than as "contains build": a membership check
// is blind to exactly the direction this mutant moves in.
func TestIssue6927_GitHubActions_JobRuleReadsOnlyRunsOnAndUses(t *testing.T) {
	got := names6927GHA(detect6927GHA(t, "ci/workflows/build.yml", gha6927Workflow), "Operation")
	want := []string{"build", "publish"}
	if !slices.Equal(got, want) {
		t.Errorf("job rule minted Operations %v, want exactly %v. `notification:` is a job whose "+
			"first key is `needs:`, so the rule must NOT see it; anything else here means the "+
			"alternation reads a key that is not `runs-on:` or `uses:`", got, want)
	}

	got2 := names6927GHA(detect6927GHA(t, "ci/workflows/nightly.yml", gha6927ReusableCaller), "Operation")
	want2 := []string{"smoke"}
	if !slices.Equal(got2, want2) {
		t.Errorf("job rule minted Operations %v in the reusable caller, want exactly %v", got2, want2)
	}
}

// gha6927OnRules returns the shipped `on:`-trigger rules, selected by BEHAVIOUR
// rather than by pattern text.
//
// PR #6945 review M4/M5: the previous version of this helper found its rule
// with strings.Contains(sp.Pattern, `^on:\s+(\w+)`) and t.Fatal'd when that
// missed. That made its verdict a fact about the pattern's SPELLING. A real
// anchor weakening (`(?m)^on:` -> `(?m)^\s*on:`) turned the test red with "the
// pattern is no longer in github_actions.yaml" — it had lost its selector, not
// observed a behaviour change — and a behaviour-PRESERVING rewrite
// (`(\w+)` -> `([\w]+)`) produced the identical failure. A test that goes red
// when you rename something and green-for-the-wrong-reason when you break
// something is worse than no test, because it reads as coverage.
//
// Selecting on what the compiled pattern DOES survives any rewrite that keeps
// the behaviour and survives none that changes it.
func gha6927OnRules(t *testing.T) (single, block *regexp.Regexp) {
	t.Helper()
	// Deliberately carry NOTHING but the trigger: a `name:` line would be read
	// by the workflow-name rule and the classification below would pick it up
	// as an `on:` rule.
	const singleForm = "on: push\n"
	const blockForm = "on:\n  push:\n    branches: [main]\n"
	for _, sp := range ghaSourcePatterns(t) {
		re, err := regexp.Compile(sp.Pattern)
		if err != nil {
			t.Fatalf("shipped pattern does not compile: %q: %v", sp.Pattern, err)
		}
		switch {
		case re.MatchString(singleForm):
			if single != nil {
				t.Fatalf("two rules read a single-line `on: push`: %q and %q", single, sp.Pattern)
			}
			single = re
		case re.MatchString(blockForm):
			if block != nil {
				t.Fatalf("two rules read ONLY a block-form trigger: %q and %q", block, sp.Pattern)
			}
			block = re
		}
	}
	if single == nil || block == nil {
		t.Fatalf("github_actions.yaml no longer carries two `on:` rules (single=%v block=%v)",
			single, block)
	}
	return single, block
}

// TestIssue6927_GitHubActions_SingleEventRuleAlsoReadsBlockForm pins a property
// the old `# on: single event` comment denied: `\s` matches a newline, so
// `^on:\s+(\w+)` reads the FIRST event of a block-form trigger too. That is why
// its corpus count (235 matches) is one ABOVE the block-form rule's (234)
// rather than complementary to it. It is asserted against the compiled pattern
// because at entity level the two rules mint the same Config for the same file
// and the duplicate folds away, leaving the claim unobservable.
func TestIssue6927_GitHubActions_SingleEventRuleAlsoReadsBlockForm(t *testing.T) {
	single, _ := gha6927OnRules(t)
	m := single.FindStringSubmatch("on:\n  push:\n    branches: [main]\n")
	if m == nil {
		t.Fatal("the single-event on: rule did not match a block-form trigger; its own comment " +
			"says it does, and the corpus counts (235 vs 234) rest on it")
	}
	if m[1] != "push" {
		t.Errorf("block-form capture = %q, want %q", m[1], "push")
	}
}

// TestIssue6927_GitHubActions_OnRulesAreColumnZeroOnly grades the half of the
// `on:` anchors that the fixture's forbidden rows do not reach.
//
// `Config:needs` and `Config:20` grade removing `^` OUTRIGHT. They say nothing
// about WEAKENING it: `(?m)^on:` -> `(?m)^\s*on:` keeps the line anchor and
// still reads an indented `on:` key, and both `on:` rules are ungated — so
// under that weakening they fire on any indented `on:` in any YAML file in any
// indexed repo, which is the blast radius the column-0 anchor exists to
// prevent. PR #6945 review M4 found that ALIVE at the fixture gate.
func TestIssue6927_GitHubActions_OnRulesAreColumnZeroOnly(t *testing.T) {
	single, block := gha6927OnRules(t)
	// An indented `on:` in each of the two trigger shapes. Both are ordinary
	// YAML that any repo can hold under a key of its own.
	const indentedSingle = "jobs:\n  build:\n    on: push\n"
	const indentedBlock = "jobs:\n  build:\n    on:\n      push:\n"
	for _, tc := range []struct {
		name string
		re   *regexp.Regexp
		src  string
	}{
		{"single-event rule, indented single form", single, indentedSingle},
		{"single-event rule, indented block form", single, indentedBlock},
		{"block-form rule, indented block form", block, indentedBlock},
		{"block-form rule, indented single form", block, indentedSingle},
	} {
		if m := tc.re.FindStringSubmatch(tc.src); m != nil {
			t.Errorf("%s: matched an INDENTED `on:` and captured %q. Column 0 is the whole fence "+
				"on these two rules — they carry no requires_framework — so a weakened anchor "+
				"reads a nested `on:` key in every YAML file the cicd bucket is offered",
				tc.name, m[len(m)-1])
		}
	}
}
