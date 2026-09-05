package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The tests below grade `coverage check` — the single definition of the
// coverage-docs gate (#6866) — on the two failure shapes that were
// actually measured on PR #6864, plus a clean tree.
//
// The load-bearing assertion in every failing case is WHICH step fired,
// not merely that one did. A wrapper that catches only one of the two
// shapes, or that collapses five distinct failures into one opaque exit
// code, is the outcome #6866 exists to prevent — and an "it failed"
// assertion would pass on exactly that wrapper. So each case asserts the
// expected step's annotation is present AND every other step's
// annotation is absent.

// citedFile is the stub source file the fixture registry cites. It is a
// real Go file because the notes-citation checker parses it with go/ast
// (see cite_symbol.go): the citation names a SYMBOL and the checker
// resolves the symbol's declaration line, so drift needs a genuine
// declaration to drift away from.
const citedFile = "internal/engine/http_endpoint_synthesis.go"

const citedFileSrc = `package engine

// synthesizeEndpoints is the symbol the stale-citation case cites.
func synthesizeEndpoints() {}

func other() {}
`

// declLineOfSynthesizeEndpoints is the 1-based line of the
// `func synthesizeEndpoints` declaration in citedFileSrc. A citation to
// any other line is drift.
const declLineOfSynthesizeEndpoints = 4

// newGateTree builds a self-contained repo root that PASSES all five
// gate steps: a canonical registry, every cited path present on disk,
// and docs/coverage/ freshly generated from that registry.
//
// Individual tests then break exactly one property, so the step that
// fires is attributable to that break and nothing else.
func newGateTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	regPath := filepath.Join(root, filepath.FromSlash(defaultRegistryPath))
	if err := os.MkdirAll(filepath.Dir(regPath), 0o755); err != nil {
		t.Fatalf("mkdir docs/coverage: %v", err)
	}
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(regPath, data, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	// Seed each step green in turn: backfill fills the lane cells the
	// grouped fixture record's taxonomy declares (otherwise validate AND
	// backfill --check both fire and no test could attribute a failure);
	// the cite stubs satisfy the cite-exists check; fmt canonicalizes;
	// gen puts docs/coverage/ in sync.
	if _, _, err := runCmd(t, "backfill", "--file", regPath); err != nil {
		t.Fatalf("seed backfill: %v", err)
	}
	for _, rel := range citePaths(t, regPath) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(citedFileSrc), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	if _, _, err := runCmd(t, "fmt", "--file", regPath); err != nil {
		t.Fatalf("seed fmt: %v", err)
	}
	if _, _, err := runCmd(t, "gen", "--file", regPath, "--out", root); err != nil {
		t.Fatalf("seed gen: %v", err)
	}
	return root
}

// citePaths returns every repo-relative path cited anywhere in the
// registry, across all three capability tiers.
func citePaths(t *testing.T, regPath string) []string {
	t.Helper()
	reg, err := loadRegistry(regPath)
	if err != nil {
		t.Fatalf("load for cites: %v", err)
	}
	seen := map[string]struct{}{citedFile: {}}
	add := func(caps map[string]Capability) {
		for _, c := range caps {
			for _, cite := range c.Cites {
				seen[cite] = struct{}{}
			}
		}
	}
	for _, rec := range reg.Records {
		add(rec.Capabilities)
		for _, g := range rec.Groups {
			add(g)
		}
		for _, g := range rec.FrameworkSpecific {
			add(g)
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// runGate runs `check` against root and returns stdout, stderr, err.
func runGate(t *testing.T, root string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run([]string{"check", "--repo-root", root}, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// assertStepFired asserts the gate failed at exactly the step named by
// wantStep (matched on its Hint, which is the operator-facing message)
// and that no other step's hint was printed.
func assertStepFired(t *testing.T, stderr string, err error, wantStep string) {
	t.Helper()
	if err == nil {
		t.Fatalf("gate passed; expected failure at step %q\nstderr:\n%s", wantStep, stderr)
	}
	var want string
	for _, s := range gateSteps() {
		if s.Name == wantStep {
			want = s.Hint
		}
	}
	if want == "" {
		t.Fatalf("no gate step named %q", wantStep)
	}
	if !strings.Contains(stderr, "::error::"+want) {
		t.Fatalf("expected the %q annotation, got stderr:\n%s", wantStep, stderr)
	}
	for _, s := range gateSteps() {
		if s.Name == wantStep {
			continue
		}
		if strings.Contains(stderr, "::error::"+s.Hint) {
			t.Fatalf("step %q also reported; a gate failure must name ONE cause.\nstderr:\n%s", s.Name, stderr)
		}
	}
	if !strings.Contains(err.Error(), wantStep) {
		t.Fatalf("returned error does not name the failing step %q: %v", wantStep, err)
	}
}

// TestCheckPassesOnSyncedTree is the negative control for every case
// below: without it, a `check` that failed unconditionally would satisfy
// all four failure tests.
func TestCheckPassesOnSyncedTree(t *testing.T) {
	root := newGateTree(t)
	stdout, stderr, err := runGate(t, root)
	if err != nil {
		t.Fatalf("clean tree failed the gate: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "gate passed (5/5 steps)") {
		t.Fatalf("expected all five steps to run and report, got stdout:\n%s", stdout)
	}
}

// TestCheckFailsOnStaleCitation reproduces PR #6864's FIRST red CI round:
// registry.json cites a declaration by a line number the declaration has
// moved away from (bicep's extractResources/extractOutputs shifted 16
// lines). The gate must fail at validate.
func TestCheckFailsOnStaleCitation(t *testing.T) {
	root := newGateTree(t)
	staleLine := declLineOfSynthesizeEndpoints - 2
	setNotes(t, root, "lang.python.framework.flask", "endpoint_synthesis",
		"`synthesizeEndpoints` ("+citedFile+":"+itoa(staleLine)+")")

	_, stderr, err := runGate(t, root)
	assertStepFired(t, stderr, err, "Validate schema + cites")
	if !strings.Contains(stderr, "is stale") {
		t.Fatalf("expected the stale-cite diagnosis in stderr:\n%s", stderr)
	}
}

// TestCheckFailsOnStaleGeneratedPage reproduces PR #6864's SECOND red CI
// round: citations fixed, but the detail page regenerated from them was
// not committed. Everything upstream is green, so this fails at the
// docs-sync step — and asserting WHICH message fires is the point: a
// wrapper reporting the citation failure here would be lying.
func TestCheckFailsOnStaleGeneratedPage(t *testing.T) {
	root := newGateTree(t)
	page := filepath.Join(root, filepath.FromSlash(docsDir), "detail", "lang.python.framework.flask.md")
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("fixture page missing, cannot stale it: %v", err)
	}
	if err := os.WriteFile(page, []byte("stale content that gen will overwrite\n"), 0o644); err != nil {
		t.Fatalf("stale the page: %v", err)
	}

	_, stderr, err := runGate(t, root)
	assertStepFired(t, stderr, err, "Verify docs are in sync with JSON")
	if !strings.Contains(stderr, "modified:") || !strings.Contains(stderr, "detail/lang.python.framework.flask.md") {
		t.Fatalf("expected the offending path to be named:\n%s", stderr)
	}
}

// TestCheckFailsOnNewlyEmittedPage keeps the #6354 property. The
// workflow needed `git add -N` for it: a page `gen` newly emits is
// untracked, and `git diff` alone cannot see an untracked file, so such
// a page was invisible to the gate unless a summary.md delta happened to
// link it. Grading gen's own before/after delta has no such blind spot —
// a path absent before `gen` and present after is an addition whatever
// git thinks of it. This test is what stops that property from being
// dropped along with the git step.
func TestCheckFailsOnNewlyEmittedPage(t *testing.T) {
	root := newGateTree(t)
	page := filepath.Join(root, filepath.FromSlash(docsDir), "detail", "lang.python.framework.flask.md")
	if err := os.Remove(page); err != nil {
		t.Fatalf("remove page: %v", err)
	}

	_, stderr, err := runGate(t, root)
	assertStepFired(t, stderr, err, "Verify docs are in sync with JSON")
	if !strings.Contains(stderr, "added:") {
		t.Fatalf("a page gen newly emits must be reported as an addition:\n%s", stderr)
	}
}

// TestCheckFailsOnPrunedOrphanPage pins the DELETION direction, which is
// the one the preserved hint explicitly promises ("commit the result,
// deletions included") and which nothing else here observes.
//
// The shape is a generated by-language page for a slug the registry no
// longer produces: `gen` prunes it (pruneGenerated — the delete half of
// the write-then-prune contract, #6354) and the gate must report the
// removal. This is a real shape, not a contrived one: pruneGenerated's
// own doc comment describes exactly it.
//
// Two deliberate choices about the input, because they are what make
// this a third INDEPENDENT kill rather than a duplicate of the other
// two:
//
//   - An orphaned page, not a removed record. Removing a record also
//     rewrites summary.md and the pivot pages (measured: 3 modified
//     files, 0 deletions), so that case fires on `modified` and would
//     stay red with the `deleted` branch gone. An orphaned page changes
//     nothing else: the reported delta is exactly one deletion.
//   - The page is under by-language/, the ONLY directory `gen` prunes. A
//     detail/ or by-category/ page whose record left the registry is not
//     pruned and survives as an orphan; the gate is blind to that and
//     always was, since an untouched committed page also equals HEAD and
//     the previous `git diff` formulation could not see it either. Noted
//     rather than fixed here — it is a `gen` gap, not a gate gap.
func TestCheckFailsOnPrunedOrphanPage(t *testing.T) {
	root := newGateTree(t)
	orphan := filepath.Join(root, filepath.FromSlash(docsDir), "by-language", "cobol.md")
	// The DO NOT EDIT marker is what makes the page prunable: prune only
	// deletes pages this generator wrote, so a hand-authored file is left
	// alone. Without the marker this test would be vacuous.
	if err := os.WriteFile(orphan, []byte(doNotEditMarker+"\n\n# COBOL\n"), 0o644); err != nil {
		t.Fatalf("write orphan page: %v", err)
	}

	_, stderr, err := runGate(t, root)
	assertStepFired(t, stderr, err, "Verify docs are in sync with JSON")
	if !strings.Contains(stderr, "deleted:") {
		t.Fatalf("a page gen pruned must be reported as a deletion:\n%s", stderr)
	}
	if !strings.Contains(stderr, "by-language/cobol.md") {
		t.Fatalf("expected the pruned path to be named:\n%s", stderr)
	}
	// Exactly one change, and it is the deletion. Without this the case
	// would survive deleting diffSnapshots' `deleted` branch the moment
	// any incidental modification crept into the fixture.
	if strings.Contains(stderr, "modified:") || strings.Contains(stderr, "added:") {
		t.Fatalf("the pruned-orphan case must report ONLY a deletion, else it does not grade that branch alone:\n%s", stderr)
	}
	if _, statErr := os.Stat(orphan); statErr == nil {
		t.Fatal("gen did not prune the orphan page; the test is not exercising the deletion path at all")
	}
}

// TestCheckFailsOnNonCanonicalRegistry pins the fmt step (#2907): a
// whole-file re-serialization of registry.json must fail the gate, and
// must fail it with the fmt message rather than something downstream.
func TestCheckFailsOnNonCanonicalRegistry(t *testing.T) {
	root := newGateTree(t)
	regPath := filepath.Join(root, filepath.FromSlash(defaultRegistryPath))
	reg, err := loadRegistry(regPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	compact, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(regPath, compact, 0o644); err != nil {
		t.Fatalf("write compact: %v", err)
	}

	_, stderr, err2 := runGate(t, root)
	assertStepFired(t, stderr, err2, "Verify registry.json is canonical (guards against recompaction, #2907)")
}

// TestGateRosterMatchesTheWorkflow pins the five steps `coverage check`
// replaced, by name and in order. coverage-docs.yml no longer lists
// them — it runs this one command — so this roster is what stops a step
// being silently dropped from the gate, which would be indistinguishable
// from the pre-#6866 situation the issue describes.
func TestGateRosterMatchesTheWorkflow(t *testing.T) {
	want := []string{
		"Validate schema + cites",
		"Guard against incomplete grouped records (#2971)",
		"Verify registry.json is canonical (guards against recompaction, #2907)",
		"Regenerate docs",
		"Verify docs are in sync with JSON",
	}
	got := gateSteps()
	if len(got) != len(want) {
		t.Fatalf("gate has %d step(s), want %d: %v", len(got), len(want), stepNames(got))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("step %d = %q, want %q", i, got[i].Name, want[i])
		}
		if strings.TrimSpace(got[i].Hint) == "" {
			t.Errorf("step %q has no failure hint; a step that cannot say why it failed is the defect #6866 forbids", got[i].Name)
		}
	}
}

func stepNames(steps []gateStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Name
	}
	return out
}

// stepNamed returns the gate step with the given name, failing when the
// gate does not carry it.
func stepNamed(t *testing.T, name string) gateStep {
	t.Helper()
	for _, s := range gateSteps() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("gate has no step %q; roster is %v", name, stepNames(gateSteps()))
	return gateStep{}
}

// TestCheckFailsOnBackfillGap pins the backfill step (#2971): a grouped
// record missing a cell its subcategory taxonomy declares must be caught.
//
// It exercises the step directly rather than through the whole gate,
// because the two guards MASK each other: validateGroupedCompleteness is
// the validate-time mirror of this same check and, with
// completenessGateIsError true, it reports the identical gap as a
// validate ERROR one step earlier. So no whole-gate input can attribute
// a failure to the backfill step today — running it in isolation is the
// only way to grade it rather than grade validate twice. (Deleting the
// step from gateSteps() still fails this test: stepNamed cannot find it.)
func TestCheckFailsOnBackfillGap(t *testing.T) {
	root := newGateTree(t)
	regPath := filepath.Join(root, filepath.FromSlash(defaultRegistryPath))
	reg, err := loadRegistry(regPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dropped := ""
	for i := range reg.Records {
		rec := &reg.Records[i]
		if len(groupsForSubcategory(rec.Subcategory)) == 0 {
			continue
		}
		for g, caps := range rec.Groups {
			for k := range caps {
				delete(rec.Groups[g], k)
				dropped = rec.ID + "/" + g + "/" + k
				break
			}
			if dropped != "" {
				break
			}
		}
		if dropped != "" {
			break
		}
	}
	if dropped == "" {
		t.Fatal("fixture has no grouped record with a taxonomy; cannot build a backfill gap")
	}
	if err := saveRegistry(regPath, reg); err != nil {
		t.Fatalf("save: %v", err)
	}

	step := stepNamed(t, "Guard against incomplete grouped records (#2971)")
	var out, errw bytes.Buffer
	env := &checkEnv{RepoRoot: root, RegistryPath: regPath, Out: &out, Err: &errw}

	if err := step.Run(env); err == nil {
		t.Fatalf("backfill step passed a registry missing %s\nstdout:\n%s", dropped, out.String())
	}
	// Positive control: the same step is green once the gap is filled,
	// so the failure above is attributable to the dropped cell and not
	// to the step erroring on this tree for some unrelated reason.
	if _, _, err := runCmd(t, "backfill", "--file", regPath); err != nil {
		t.Fatalf("refill: %v", err)
	}
	if err := step.Run(env); err != nil {
		t.Fatalf("backfill step still failing after refill: %v", err)
	}
}

// TestCheckDirtyTreeGradesGenNotHead states and pins the dirty-tree
// behaviour the workflow's `git diff docs/coverage/` got wrong.
//
// registry.json LIVES under docs/coverage/, so `git diff docs/coverage/`
// covered the very file a coverage change edits: running the gate with
// an uncommitted registry edit always failed locally, on a divergence CI
// (whose checkout is clean) can never see and which says nothing about
// whether the docs are in sync. `check` grades what `gen` itself
// changes, so an uncommitted-but-regenerated tree PASSES — and the
// uncommitted state is still surfaced, as an advisory note.
func TestCheckDirtyTreeGradesGenNotHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := newGateTree(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "seed")

	// An uncommitted registry edit, with docs regenerated from it: in
	// sync, but divergent from HEAD across several files.
	setNotes(t, root, "lang.python.framework.flask", "endpoint_synthesis",
		"`synthesizeEndpoints` ("+citedFile+":"+itoa(declLineOfSynthesizeEndpoints)+")")
	if _, _, err := runCmd(t, "gen", "--file", filepath.Join(root, filepath.FromSlash(defaultRegistryPath)), "--out", root); err != nil {
		t.Fatalf("regen: %v", err)
	}

	stdout, stderr, err := runGate(t, root)
	if err != nil {
		t.Fatalf("a dirty-but-in-sync tree must pass: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "uncommitted change(s) before the gate runs") {
		t.Fatalf("the uncommitted state must still be surfaced:\n%s", stdout)
	}
	if !strings.Contains(stdout, "registry.json") {
		t.Fatalf("the note must name the dirty paths:\n%s", stdout)
	}
}

// setNotes writes notes prose onto one flat capability cell, canonically
// (so the fmt step stays green and only the intended property changes).
func setNotes(t *testing.T, root, id, capKey, notes string) {
	t.Helper()
	regPath := filepath.Join(root, filepath.FromSlash(defaultRegistryPath))
	reg, err := loadRegistry(regPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rec := findRecord(reg, id)
	if rec == nil {
		t.Fatalf("fixture missing record %q", id)
	}
	cell, ok := rec.Capabilities[capKey]
	if !ok {
		t.Fatalf("fixture record %q missing capability %q", id, capKey)
	}
	cell.Notes = notes
	rec.Capabilities[capKey] = cell
	if err := saveRegistry(regPath, reg); err != nil {
		t.Fatalf("save: %v", err)
	}
}

// itoa avoids importing strconv for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
