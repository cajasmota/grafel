package engine

import (
	"context"
	"regexp"
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

// TestIssue6927_GitHubActions_SingleEventRuleAlsoReadsBlockForm pins a property
// the old `# on: single event` comment denied: `\s` matches a newline, so
// `^on:\s+(\w+)` reads the FIRST event of a block-form trigger too. That is why
// its corpus count (235 matches) is one ABOVE the block-form rule's (234)
// rather than complementary to it, and it is asserted against the shipped
// pattern text because at entity level the two rules mint the same Config and
// the duplicate folds away — leaving the claim unobservable.
func TestIssue6927_GitHubActions_SingleEventRuleAlsoReadsBlockForm(t *testing.T) {
	var single string
	for _, sp := range ghaSourcePatterns(t) {
		if strings.Contains(sp.Pattern, `^on:\s+(\w+)`) {
			single = sp.Pattern
		}
	}
	if single == "" {
		t.Fatal("the single-event on: pattern is no longer in github_actions.yaml")
	}
	re := regexp.MustCompile(single)
	m := re.FindStringSubmatch("name: CI\non:\n  push:\n    branches: [main]\n")
	if m == nil {
		t.Fatalf("%q did not match a block-form trigger; the rule's own comment says it does", single)
	}
	if m[1] != "push" {
		t.Errorf("block-form capture = %q, want %q", m[1], "push")
	}
}
