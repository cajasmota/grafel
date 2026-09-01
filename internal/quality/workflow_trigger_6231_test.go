package quality

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These tests gate .github/workflows/quality.yml, which is the ONLY place
// extraction recall is graded (Refs #6231).
//
// WHY A TEST AND NOT A COMMENT
// quality.yml was workflow_dispatch-only from its creation until #6231. In that
// state nothing on CI graded recall at all: `./internal/quality` checks fixture
// SHAPE, and the actual grading runs through the `grafel quality` CLI, which
// only that workflow invokes. The cost was measured — a kotlin-spring-mini
// regression (entity_found 18 -> 13) shipped through three PRs and 39 green CI
// legs before a manual dispatch on 2026-08-31 found it.
//
// After #6231 the workflow's header forbids returning it to dispatch-only. A
// header is not a gate. These tests are.
//
// WHAT THE SECOND TEST GUARDS IS THE MORE IMPORTANT HALF
// Adding the `pull_request` trigger alone would have made every PR run RED, and
// silently so — for a reason that is invisible in the workflow diff.
// `inputs.mode` is empty on every event that is not workflow_dispatch, and
// scripts/verify2/run-quality.sh resolves an unset QUALITY_MODE to the STRICT
// 100%-recall gate, which does not pass on the full fixture set today. The
// workflow therefore has to supply its own `|| 'ratchet'` fallback, and nothing
// else observes that it still does.
//
// TestRunQualityTreatsAnUnsetModeAsStrict_6231 is the positive control for
// that: it pins the run-quality.sh behaviour that makes the fallback
// load-bearing. Without it, asserting "the workflow resolves to ratchet" would
// be a test of a string nobody has shown to matter.

const qualityWorkflowRel = "quality.yml"

// readQualityWorkflow parses .github/workflows/quality.yml into a generic tree.
//
// Note on the `on:` key: YAML 1.1 reads a bare `on` as the boolean true, and
// some parsers (Python's yaml.safe_load among them) do exactly that. yaml.v3
// follows the YAML 1.2 core schema, where only `true`/`false` are booleans, so
// the key arrives as the string "on". Both are looked up rather than assumed,
// because a test that silently found no triggers would pass for the wrong
// reason.
func readQualityWorkflow(t *testing.T) map[any]any {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", ".github", "workflows", qualityWorkflowRel))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[any]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// workflowTriggers returns the `on:` mapping, failing loudly if it is absent or
// is not a mapping — the two shapes under which a trigger assertion would
// otherwise vacuously pass.
func workflowTriggers(t *testing.T, doc map[any]any) map[string]any {
	t.Helper()
	var node any
	var ok bool
	if node, ok = doc["on"]; !ok {
		node, ok = doc[true] // YAML 1.1 parsers hand back the boolean key
	}
	if !ok {
		keys := make([]string, 0, len(doc))
		for k := range doc {
			keys = append(keys, fmt.Sprint(k))
		}
		t.Fatalf("quality.yml has no `on:` key at all; top-level keys were %v", keys)
	}
	m, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("quality.yml `on:` is %T, not a mapping of triggers: %#v", node, node)
	}
	return m
}

// TestQualityWorkflowRunsOnPullRequests_6231 pins the trigger itself.
//
// It also pins that workflow_dispatch survives, because the dispatch path with
// its `mode` choice is how anyone sees the outstanding strict-mode gap, and a
// change that swapped one trigger for the other would otherwise pass.
func TestQualityWorkflowRunsOnPullRequests_6231(t *testing.T) {
	on := workflowTriggers(t, readQualityWorkflow(t))

	if _, ok := on["pull_request"]; !ok {
		present := make([]string, 0, len(on))
		for k := range on {
			present = append(present, k)
		}
		t.Fatalf("quality.yml must run on pull_request — it is the only CI grading of "+
			"extraction recall, and #6231 made it always-on after a regression shipped "+
			"through 39 green legs without it. Triggers present: %v", present)
	}
	if _, ok := on["workflow_dispatch"]; !ok {
		t.Errorf("quality.yml lost its workflow_dispatch trigger; the mode=strict dispatch " +
			"is the only way to see the outstanding 100%%-recall gap")
	}
}

// qualityModeExpr returns the QUALITY_MODE value as written in the workflow,
// together with the name of the step that sets it.
func qualityModeExpr(t *testing.T, doc map[any]any) (step, expr string) {
	t.Helper()
	jobs, ok := doc["jobs"].(map[string]any)
	if !ok {
		t.Fatalf("quality.yml has no jobs mapping")
	}
	for _, jv := range jobs {
		job, ok := jv.(map[string]any)
		if !ok {
			continue
		}
		steps, ok := job["steps"].([]any)
		if !ok {
			continue
		}
		for _, sv := range steps {
			s, ok := sv.(map[string]any)
			if !ok {
				continue
			}
			env, ok := s["env"].(map[string]any)
			if !ok {
				continue
			}
			if v, ok := env["QUALITY_MODE"]; ok {
				return fmt.Sprint(s["name"]), fmt.Sprint(v)
			}
		}
	}
	t.Fatalf("no step in quality.yml sets QUALITY_MODE; the gate's mode is then " +
		"whatever run-quality.sh defaults to, which is strict")
	return "", ""
}

// evalGitHubExpr evaluates the small subset of GitHub expression syntax this
// workflow uses: a single `${{ ... }}` holding one or more `||`-separated
// operands, each either a single-quoted literal or a context reference.
//
// GitHub's `||` yields the first operand that is truthy, and the empty string
// is falsy — that is the entire mechanism under test. Anything outside this
// subset fails the test rather than being guessed at, so a rewrite into a form
// this evaluator cannot read is reported instead of silently accepted.
func evalGitHubExpr(t *testing.T, expr string, ctx map[string]string) string {
	t.Helper()
	trimmed := strings.TrimSpace(expr)
	if !strings.HasPrefix(trimmed, "${{") || !strings.HasSuffix(trimmed, "}}") {
		t.Fatalf("QUALITY_MODE is %q, which this test cannot evaluate; it expects a single "+
			"${{ ... }} expression", expr)
	}
	body := strings.TrimSpace(trimmed[3 : len(trimmed)-2])

	var last string
	for _, operand := range strings.Split(body, "||") {
		operand = strings.TrimSpace(operand)
		switch {
		case strings.HasPrefix(operand, "'") && strings.HasSuffix(operand, "'") && len(operand) >= 2:
			last = operand[1 : len(operand)-1]
		default:
			v, ok := ctx[operand]
			if !ok {
				t.Fatalf("QUALITY_MODE references %q, which this test has no value for; "+
					"known context keys: %v", operand, ctx)
			}
			last = v
		}
		if last != "" && last != "false" {
			return last
		}
	}
	return last
}

// TestQualityWorkflowSelectsRatchetWithoutAnInput_6231 is the half that guards
// the failure #6231 nearly shipped: on pull_request and push there is no
// `inputs.mode`, and an empty QUALITY_MODE selects the strict gate.
func TestQualityWorkflowSelectsRatchetWithoutAnInput_6231(t *testing.T) {
	step, expr := qualityModeExpr(t, readQualityWorkflow(t))

	// The pull_request / push context: workflow_dispatch inputs do not exist,
	// so `inputs.mode` evaluates to the empty string.
	got := evalGitHubExpr(t, expr, map[string]string{"inputs.mode": ""})

	if got != "ratchet" {
		t.Fatalf("step %q sets QUALITY_MODE to %q, which resolves to %q on an event with no "+
			"inputs. run-quality.sh reads anything other than ratchet/update-baseline as the "+
			"STRICT 100%%-recall gate, which does not pass on the full fixture set — so every "+
			"pull_request run would be red. Keep a literal 'ratchet' fallback.",
			step, expr, got)
	}
}

// TestRunQualityTreatsAnUnsetModeAsStrict_6231 is the premise the test above
// rests on, asserted directly rather than assumed.
//
// It drives scripts/verify2/run-quality.sh in a hermetic copy of the tree —
// run-quality.sh derives REPO_ROOT as dirname($0)/../.., so laying it out under
// <tmp>/scripts/verify2/ makes <tmp> the root — with a stub
// scripts/quality/run.sh that reports the arguments it was handed. No fixture
// is indexed and the real golden set is never touched.
//
// Both directions are asserted in one test on purpose. The ratchet leg is the
// control: it pins that this harness CAN observe --ratchet being passed, so the
// unset leg's silence is attributable to the mode resolution and not to a stub
// that never sees any argument.
func TestRunQualityTreatsAnUnsetModeAsStrict_6231(t *testing.T) {
	root := t.TempDir()
	verify2 := filepath.Join(root, "scripts", "verify2")
	quality := filepath.Join(root, "scripts", "quality")
	for _, d := range []string{verify2, quality} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	src, err := filepath.Abs(filepath.Join("..", "..", "scripts", "verify2", "run-quality.sh"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(filepath.Join(verify2, "run-quality.sh"), raw, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/usr/bin/env bash\necho \"DELEGATED ARGS: $*\"\n"
	if err := os.WriteFile(filepath.Join(quality, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(mode string) string {
		t.Helper()
		cmd := exec.Command("bash", filepath.Join(verify2, "run-quality.sh"))
		// os.Environ() is inherited, so an operator with QUALITY_MODE or
		// QUALITY_RUNS exported must not be able to change what this measures.
		cmd.Env = append(os.Environ(),
			"QUALITY_MODE="+mode,
			"QUALITY_RUNS=",
			"GRAFEL_BIN=",
			"QUALITY_OUT_DIR="+filepath.Join(root, "reports", "quality"),
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run-quality.sh with QUALITY_MODE=%q failed: %v\n%s", mode, err, out)
		}
		return string(out)
	}

	if got := run("ratchet"); !strings.Contains(got, "--ratchet") {
		t.Fatalf("control leg: QUALITY_MODE=ratchet did not reach run.sh as --ratchet, so this "+
			"harness cannot observe the flag at all. Output:\n%s", got)
	}
	if got := run(""); strings.Contains(got, "--ratchet") {
		t.Fatalf("an unset QUALITY_MODE now selects the ratchet gate. That is a fine thing to "+
			"change, but quality.yml's `|| 'ratchet'` fallback exists solely because it did "+
			"not — update both together. Output:\n%s", got)
	}
}
