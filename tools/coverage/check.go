package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// `coverage check` is the ONE place the coverage-docs gate is defined.
//
// Before #6866 the gate lived in .github/workflows/coverage-docs.yml as
// five separate steps. Three were `./tools/coverage` subcommands
// (validate, backfill --check, fmt --check); the other two — `gen`
// followed by a comparison against working-tree state — existed nowhere
// but the YAML. A developer could run every subcommand the tool
// offered, see three green exits, and still be red in CI. That is
// measured, not inferred: PR #6864 burned two CI cycles on exactly that
// tree.
//
// The fix is not "a shorter invocation". The fix is that gateSteps()
// below is the SINGLE source of truth and coverage-docs.yml calls this
// one command. A step added here is a step CI runs, with no second copy
// of the list to forget to update — which is the whole point of #6866,
// since the workflow subscribes to internal/extractors/** and #6852 has
// eleven language arms left to land through it.
//
// Two properties this file is obliged to preserve:
//
//   - Failure fidelity. Each step carries the error annotation the
//     corresponding workflow step used to print, verbatim where one
//     existed. A wrapper collapsing five distinct failures into one
//     opaque exit code would be WORSE than the five steps it replaces:
//     the two #6864 failures were diagnosable at a glance precisely
//     because the messages named the cause and the fix.
//   - Untracked-page visibility (#6354). See docsSyncStep.

// gateStep is one step of the coverage-docs gate.
//
// Name mirrors the workflow step name it replaces so a CI log line and
// a local run read the same. Hint is the operator-facing annotation
// emitted on failure; it is what tells a developer WHICH of the five
// checks failed and what to run to fix it.
type gateStep struct {
	Name string
	Hint string
	Run  func(env *checkEnv) error
}

// checkEnv is the shared state a gate run threads through its steps.
//
// PreGen/PostGen are populated by the gen step and consumed by the
// docs-sync step: the two are deliberately kept as two steps (matching
// the workflow) so that a generator crash and a stale page report
// different failures rather than one merged "docs are wrong".
type checkEnv struct {
	RepoRoot     string
	RegistryPath string
	Out          io.Writer
	Err          io.Writer

	PreGen  map[string]string
	PostGen map[string]string
}

// docsCoverageDir is the tree the gate generates into and compares.
func (e *checkEnv) docsCoverageDir() string {
	return filepath.Join(e.RepoRoot, docsDir)
}

// gateSteps returns the gate, in order. THIS IS THE GATE — there is no
// second copy in .github/workflows/coverage-docs.yml, which runs
// `go run ./tools/coverage check` and nothing else. Adding a step here
// adds it to CI; the two cannot drift because there is only one list.
func gateSteps() []gateStep {
	return []gateStep{
		{
			Name: "Validate schema + cites",
			Hint: "docs/coverage/registry.json failed schema/cite validation. Fix the errors printed above and re-run 'go run ./tools/coverage validate'. A cite that names a symbol but the wrong line is drift: the declaration moved, the hand-typed number did not (#6673).",
			Run: func(env *checkEnv) error {
				return cmdValidate([]string{"--file", env.RegistryPath, "--repo-root", env.RepoRoot}, env.Out, env.Err)
			},
		},
		{
			// Redundant with step 1 while completenessGateIsError is
			// true — both run missingTaxonomyCells, and validate fires
			// first — and kept deliberately for the case where it is
			// not. That constant is documented as a flippable severity
			// knob; flipped, validate downgrades an incomplete record to
			// an advisory warning and THIS is the only step that fails
			// on it. Deleting it would make the knob silently unsafe to
			// flip. See TestCompletenessSeverityKnobDecidesWhoCatchesIt,
			// which runs both sides of the knob rather than asserting
			// this in prose (#6868).
			Name: "Guard against incomplete grouped records (#2971)",
			Hint: "docs/coverage/registry.json has grouped records missing cells the taxonomy declares. Run 'go run ./tools/coverage backfill' locally and commit.",
			Run: func(env *checkEnv) error {
				return cmdBackfill([]string{"--file", env.RegistryPath, "--check"}, env.Out)
			},
		},
		{
			Name: "Verify registry.json is canonical (guards against recompaction, #2907)",
			Hint: "docs/coverage/registry.json is not canonical (likely whole-file re-serialized/compacted). Run 'go run ./tools/coverage fmt' locally and commit.",
			Run: func(env *checkEnv) error {
				return cmdFmt([]string{"--file", env.RegistryPath, "--check"}, env.Out)
			},
		},
		{
			Name: "Regenerate docs",
			Hint: "'go run ./tools/coverage gen' failed outright — the generator errored before any sync comparison could run. This is a generator/template fault, not a stale page.",
			Run:  genStep,
		},
		{
			Name: "Verify docs are in sync with JSON",
			Hint: "docs/coverage/ is out of sync with registry.json (stale, orphaned, or hand-added page). Run 'go run ./tools/coverage gen' locally and commit the result, deletions included.",
			Run:  docsSyncStep,
		},
	}
}

// genStep snapshots docs/coverage/ , runs `gen`, and snapshots again.
//
// The two snapshots — not a git comparison — are what the sync step
// grades. See docsSyncStep for why.
func genStep(env *checkEnv) error {
	pre, err := snapshotTree(env.docsCoverageDir())
	if err != nil {
		return fmt.Errorf("snapshot %s before gen: %w", docsDir, err)
	}
	env.PreGen = pre
	if err := cmdGen([]string{"--file", env.RegistryPath, "--out", env.RepoRoot}, env.Out); err != nil {
		return err
	}
	post, err := snapshotTree(env.docsCoverageDir())
	if err != nil {
		return fmt.Errorf("snapshot %s after gen: %w", docsDir, err)
	}
	env.PostGen = post
	return nil
}

// docsSyncStep fails when `gen` CHANGED anything under docs/coverage/.
//
// The workflow expressed this as `git add -N docs/coverage/` followed by
// `git diff --exit-code docs/coverage/`. Both encode the same intent —
// "regenerating produced no change" — but they are NOT equivalent, and
// the old one was half-blind.
//
// # `git add -N` blinded the deletion direction
//
// `git add -N <dir>` stages a deletion of a tracked file under <dir>.
// `git diff` compares the WORK TREE against the INDEX, so once the
// deletion is staged there is nothing left for it to report:
//
//	rm d/a.md;      git diff --quiet -- d/   # exit 1 — deletion visible
//	git add -N d/;  git diff --quiet -- d/   # exit 0 — deletion GONE
//
// The old gate ran exactly that pair, in that order. So it could never
// observe a page `gen` PRUNED — the direction its own error message
// promises ("commit the result, deletions included"). The `git add -N`
// was introduced by #6354 to make untracked ADDITIONS visible; it
// silently blinded deletions on the way. `pruneGenerated` is live (it
// deletes a by-language page whose slug the registry stopped producing),
// so this was reachable, not theoretical.
//
// The before/after content comparison sees both directions, because it
// never consults git about what is tracked.
//
// # Untracked pages (#6354) are kept structurally
//
// `git diff` alone cannot see a file git does not track, so a page `gen`
// newly emitted was caught only indirectly, via whatever summary.md
// delta happened to link it; a page emitted with no summary reference
// was invisible. `git add -N` was bolted on to patch that. The
// before/after comparison has no such blind spot to patch: a path that
// did not exist before `gen` and does after is an addition by
// construction, tracked, untracked or .gitignore'd.
//
// # Dirty trees get a correct answer
//
// `git diff docs/coverage/` also covers docs/coverage/registry.json —
// the very file a developer edits to make a coverage change. Running the
// gate mid-change therefore ALWAYS failed locally on an uncommitted
// registry edit: a divergence from HEAD that CI can never see (its
// checkout is clean) and that says nothing about whether the docs are in
// sync. Grading `gen`'s own delta answers the question actually being
// asked. Pre-existing uncommitted changes are still surfaced — reported
// by reportWorkingTreeState before the gate runs — they simply do not
// fail the gate.
//
// # How the two compare on a clean checkout
//
// They agree on every defect class this gate exists to catch, and the
// new formulation additionally catches two the old one could not:
//
//   - a pruned by-language orphan (old: PASS — the `add -N` bug above);
//   - a newly emitted page that is .gitignore'd (old: PASS — git will
//     not stage it, so `git diff` still cannot see it).
//
// One direction runs the other way, and it is worth stating precisely
// because the obvious phrasing of it is wrong. The old formulation
// graded "docs/coverage/ differs from HEAD", which is a SUPERSET of
// "`gen` changed something": it also covered files under that directory
// that `gen` never writes — in practice registry.json itself. So a mode
// change (chmod +x docs/coverage/registry.json) fails under the old
// formulation and passes under this one. Measured, both halves:
//
//   - on registry.json — old `git diff` exit 1, this step passes;
//   - on a GENERATED page — both pass. renderToFile writes through
//     os.CreateTemp + os.Rename, so the page ends up at the temp file's
//     mode whatever it was before; `gen` normalises the mode rather than
//     reporting it, and the old formulation saw nothing either.
//
// Neither is a defect class this gate exists to catch, and the
// registry.json half is the same superset that produced the dirty-tree
// false failure above. Stated here rather than chased.
//
// Note that `gen` has already rewritten the offending files by the time
// this step reports. That is inherent to the gate (the workflow's `gen`
// step did the same) and harmless: every generated page carries the
// DO NOT EDIT marker, so nothing hand-authored is at stake.
func docsSyncStep(env *checkEnv) error {
	changes := diffSnapshots(env.PreGen, env.PostGen)
	if len(changes) == 0 {
		fmt.Fprintf(env.Out, "ok: %s is in sync with %s\n", docsDir, env.RegistryPath)
		return nil
	}
	for _, c := range changes {
		fmt.Fprintf(env.Err, "  %-9s %s\n", c.Kind+":", filepath.ToSlash(filepath.Join(docsDir, c.Path)))
	}
	return fmt.Errorf("%s changed by 'gen': %d file(s) out of sync", docsDir, len(changes))
}

// treeChange is one path whose content `gen` added, modified or removed.
type treeChange struct {
	Kind string // "added" | "modified" | "deleted"
	Path string // slash-relative to docs/coverage/
}

// snapshotTree hashes every regular file under dir, keyed by its path
// relative to dir. A missing dir snapshots as empty rather than erroring
// so a first-ever `gen` reads as a tree full of additions.
func snapshotTree(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return nil
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// diffSnapshots reports every path whose presence or content differs,
// sorted so the output is stable across runs and platforms.
func diffSnapshots(before, after map[string]string) []treeChange {
	var changes []treeChange
	for path, sum := range after {
		old, existed := before[path]
		switch {
		case !existed:
			changes = append(changes, treeChange{Kind: "added", Path: path})
		case old != sum:
			changes = append(changes, treeChange{Kind: "modified", Path: path})
		}
	}
	for path := range before {
		if _, still := after[path]; !still {
			changes = append(changes, treeChange{Kind: "deleted", Path: path})
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].Kind < changes[j].Kind
	})
	return changes
}

// reportWorkingTreeState prints the uncommitted state of docs/coverage/
// as it stood BEFORE the gate ran anything.
//
// This is advisory and never fails the gate: `gen` is about to rewrite
// generated pages, and knowing what was already dirty is what stops a
// developer reading a legitimate uncommitted edit as a gate failure (or
// vice versa). It is read-only — `git status --porcelain`, never
// `git add` — so it cannot disturb the caller's index. Any git failure
// (no git on PATH, not a work tree, a git archive export) is silently
// ignored: the gate does not depend on git for its verdict.
func reportWorkingTreeState(env *checkEnv) {
	cmd := exec.Command("git", "-C", env.RepoRoot, "status", "--porcelain", "--", docsDir)
	out, err := cmd.Output()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	var dirty []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			dirty = append(dirty, l)
		}
	}
	if len(dirty) == 0 {
		return
	}
	fmt.Fprintf(env.Out, "note: %s has %d uncommitted change(s) before the gate runs:\n", docsDir, len(dirty))
	for _, l := range dirty {
		fmt.Fprintf(env.Out, "  %s\n", l)
	}
	fmt.Fprintf(env.Out, "note: these do not fail the gate; the gate grades what 'gen' itself changes.\n")
}

// cmdCheck runs the whole coverage-docs gate and is what
// .github/workflows/coverage-docs.yml invokes.
//
// It stops at the first failing step — the workflow did too (a failing
// step aborts the job), and running `gen` over a registry that failed
// validation would only produce noise on top of the real error.
func cmdCheck(args []string, out, errw io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "repository root: the registry, the cite targets and the generated docs are all resolved under it")
	path := fs.String("file", "", "path to the registry JSON (default <repo-root>/"+defaultRegistryPath+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	env := &checkEnv{RepoRoot: *repoRoot, RegistryPath: *path, Out: out, Err: errw}
	if env.RegistryPath == "" {
		env.RegistryPath = filepath.Join(env.RepoRoot, defaultRegistryPath)
	}

	reportWorkingTreeState(env)

	steps := gateSteps()
	for i, step := range steps {
		fmt.Fprintf(out, "==> [%d/%d] %s\n", i+1, len(steps), step.Name)
		if err := step.Run(env); err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			fmt.Fprintf(errw, "::error::%s\n", step.Hint)
			return fmt.Errorf("coverage check failed at step %d/%d %q", i+1, len(steps), step.Name)
		}
	}
	fmt.Fprintf(out, "ok: coverage-docs gate passed (%d/%d steps)\n", len(steps), len(steps))
	return nil
}
