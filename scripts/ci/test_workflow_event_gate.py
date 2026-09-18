#!/usr/bin/env python3
"""Positive controls for scripts/ci/workflow_event_gate.py (Refs #7250).

These are not optional. The gate is a scan-and-assert-absence tool, and that
shape has five independent ways to be a silent no-op: it reads nothing, it
reads the wrong files, it parses nothing out of them, it detects nothing, or it
detects and fails to fail. A count floor catches only the first. So each of the
five is driven here:

  reads nothing        -> test_floor_on_workflow_count
  wrong files          -> test_scans_yaml_and_yml
  parses nothing       -> test_floor_on_guard_count, test_folded_if_is_read,
                          test_step_level_guard_is_read
  detects nothing      -> test_planted_violation_is_detected,
                          test_real_tree_flags_pr_linux_without_allow_list
  detects, doesn't fail-> every case above asserts the EXIT CODE, not the text

and the permissive direction — a guard naming an event the workflow really has
must NOT fire — is driven separately, because a gate that fires on everything
is as useless as one that fires on nothing.

Run: python3 -m unittest -v scripts.ci.test_workflow_event_gate
"""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
GATE = os.path.join(HERE, "workflow_event_gate.py")
REPO = os.path.dirname(os.path.dirname(HERE))
REAL_WORKFLOWS = os.path.join(REPO, ".github", "workflows")


def run_gate(directory: str, *extra: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, GATE, "--dir", directory, *extra],
        capture_output=True,
        text=True,
    )


def no_floors(directory: str, *extra: str) -> subprocess.CompletedProcess:
    """Run against a synthetic tree, where the real floors do not apply."""
    return run_gate(directory, "--min-workflows", "1", "--min-guards", "1", *extra)


class Scratch:
    def __init__(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name

    def write(self, name: str, body: str) -> str:
        path = os.path.join(self.dir, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(textwrap.dedent(body).lstrip("\n"))
        return path


class GateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.s = Scratch()
        self.addCleanup(self.s.tmp.cleanup)

    # ── detection ────────────────────────────────────────────────────────────

    def test_planted_violation_is_detected(self) -> None:
        """A guard naming an event the workflow does not subscribe to fails."""
        self.s.write(
            "dormant.yml",
            """
            name: dormant
            on:
              workflow_dispatch:
            jobs:
              ghost-job:
                if: github.event_name == 'pull_request'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("dormant.yml", r.stderr)
        self.assertIn("ghost-job", r.stderr)
        self.assertIn("pull_request", r.stderr)
        # The report must carry the actual `on:` set, not merely the complaint —
        # that is the half a reader needs to decide which way to fix it.
        self.assertIn("workflow_dispatch", r.stderr)

    def test_not_equal_comparison_is_detected(self) -> None:
        """`!=` against an impossible event is the same rotted literal."""
        self.s.write(
            "ne.yml",
            """
            on:
              push:
                branches: [main]
            jobs:
              j:
                if: github.event_name != 'pull_request_target'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("pull_request_target", r.stderr)

    def test_step_level_guard_is_read(self) -> None:
        """Step guards rot exactly like job guards; the scan must reach them."""
        self.s.write(
            "steps.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              j:
                runs-on: ubuntu-latest
                steps:
                  - name: only on a PR
                    if: github.event_name == 'pull_request'
                    run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("step", r.stderr)
        self.assertIn("only on a PR", r.stderr)

    def test_folded_if_is_read(self) -> None:
        """`if: >-` continuation lines carry the guards this gate exists for.

        acceptance.yml and test.yml both write their real gates this way. A
        parser that reads only same-line `if:` values is blind to the motivating
        case while reporting a healthy guard count from the rest of the tree.
        """
        self.s.write(
            "folded.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              j:
                if: >-
                  github.event_name == 'workflow_dispatch' ||
                  github.event_name == 'release'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("'release'", r.stderr)
        # and the sibling clause on the SAME folded block, which IS subscribed,
        # must not be reported — both halves of the block are graded.
        self.assertNotIn("'workflow_dispatch'\n", r.stderr)

    def test_scans_yaml_and_yml(self) -> None:
        """Reading the right files: `.yaml` is as legal as `.yml`."""
        self.s.write(
            "other.yaml",
            """
            on:
              schedule:
                - cron: '0 0 * * *'
            jobs:
              j:
                if: github.event_name == 'push'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("other.yaml", r.stderr)

    # ── the permissive direction ─────────────────────────────────────────────

    def test_subscribed_event_does_not_fire(self) -> None:
        """A guard naming an event the workflow really has must stay green."""
        self.s.write(
            "fine.yml",
            """
            on:
              pull_request:
              push:
                branches: [main]
              workflow_dispatch:
            jobs:
              j:
                if: >-
                  github.event_name == 'pull_request' ||
                  github.event_name == 'push' ||
                  github.event_name == 'workflow_dispatch'
                runs-on: ubuntu-latest
                steps:
                  - name: s
                    if: github.event_name != 'push'
                    run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("4 github.event_name comparison(s)", r.stdout)

    def test_on_list_and_scalar_forms(self) -> None:
        """`on: [a, b]` and `on: push` are legal spellings of the same thing.

        Getting these wrong yields an EMPTY event set, i.e. a gate that fires on
        every guard — the loud failure — or, for the scalar form, one that fires
        on the only guard there is.
        """
        self.s.write(
            "list.yml",
            """
            on: [push, workflow_dispatch]
            jobs:
              j:
                if: github.event_name == 'push'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        self.s.write(
            "scalar.yml",
            """
            on: pull_request
            jobs:
              j:
                if: github.event_name == 'pull_request'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("2 github.event_name comparison(s)", r.stdout)

    def test_comment_on_a_job_if_does_not_create_a_guard(self) -> None:
        """A trailing comment on a real `if:` must not invent a guard.

        The earlier version of this control planted its comment in the `on:`
        block, where guards are never collected (`in_jobs` is false there) — so
        it passed identically with `strip_comment` reduced to `return line`, and
        graded nothing. The plant is now on a job-level `if:` inside `jobs:`,
        which is the only place a comment can actually reach the guard regex.

        Each comment carries a FULL `github.event_name == '<event>'` expression
        naming an event the workflow does not subscribe to — the shape a comment
        actually takes when it records what a guard used to be. A comment
        mentioning a bare event name in prose would not have graded anything:
        the regex needs the whole comparison, so the first draft of this control
        left `strip_comment -> return line` ALIVE. Now: parent exit 0 (2 guards),
        mutant exit 1 (4 guards, two of them phantom).
        """
        self.s.write(
            "trailing.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              j:
                if: github.event_name == 'workflow_dispatch'  # was github.event_name == 'release' before #123
                runs-on: ubuntu-latest
                steps:
                  - name: s
                    if: github.event_name == 'workflow_dispatch'  # NOT github.event_name == 'pull_request' — see the banner
                    run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("2 github.event_name comparison(s)", r.stdout)

    def test_quoted_hash_inside_an_if_is_not_a_comment(self) -> None:
        """`#` inside a quoted string is data, not the start of a comment.

        Truncating there would silently drop the rest of a real guard — the
        blind direction, which no violation ever announces.
        """
        self.s.write(
            "hashy.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              j:
                if: contains(github.event.head_commit.message, '#7250') && github.event_name == 'release'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("'release'", r.stderr)

    # ── the parser cannot go blind on a whole file ───────────────────────────

    def test_quoted_job_key_is_exit_2_not_a_silent_skip(self) -> None:
        """Legal YAML the parser cannot read must be exit 2, not a quiet pass.

        A file with `jobs:` but no job parsed out of it contributes zero guards
        and leaves the run reporting "no violations" — a whole workflow silently
        exempt. A quoted job key is legal YAML and does exactly that.
        """
        self.s.write(
            "quoted.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              "ghost":
                if: github.event_name == 'pull_request'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("quoted.yml", r.stderr)

    def test_anchored_job_key_is_exit_2_not_a_silent_skip(self) -> None:
        """Same blindness through a different legal spelling: a YAML anchor."""
        self.s.write(
            "anchored.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              j: &tpl
                if: github.event_name == 'pull_request'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("anchored.yml", r.stderr)

    def test_a_readable_file_is_not_exit_2(self) -> None:
        """The permissive direction for the blind-parse guard.

        Without this, `return 2` unconditionally would satisfy both controls
        above.
        """
        self.s.write(
            "plain.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              j:
                if: github.event_name == 'workflow_dispatch'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # ── the allow-list is scoped to ONE file ─────────────────────────────────

    def test_allow_list_does_not_leak_across_workflows(self) -> None:
        """A row about acceptance.yml's `pr-linux` must not excuse another file.

        Two workflows may carry identically-named jobs, and a justification
        written about one is not true of the other. Under a filename-blind
        lookup the gate printed acceptance.yml's justification — "documented as
        dormant at its definition and in the `on:` block" — verbatim against a
        job in a different file where none of it is true, and exited 0.
        """
        self.s.write(
            "zz-copied-pr-linux.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              pr-linux:
                if: github.event_name == 'pull_request'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("zz-copied-pr-linux.yml", r.stderr)
        self.assertIn("VIOLATION", r.stderr)
        # and it must not have been excused with the OTHER file's reasoning
        self.assertNotIn("ALLOWED zz-copied-pr-linux.yml", r.stdout)

    # ── workflow_call propagation ────────────────────────────────────────────

    def test_workflow_call_inherits_caller_events(self) -> None:
        """A called workflow sees the CALLER's event in `github.event_name`.

        Without this rule the gate fires falsely on test.yml, which guards on
        `== 'push'` while subscribing only to workflow_call / dispatch / PR
        events — correctly, because release.yml (`on: push: tags`) calls it.
        This is the case that makes the naive "literal must appear in `on:`"
        rule wrong, and it is a real workflow in this repo, not a hypothetical.
        """
        self.s.write(
            "callee.yml",
            """
            on:
              workflow_call:
              workflow_dispatch:
            jobs:
              j:
                if: github.event_name == 'push'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        self.s.write(
            "caller.yml",
            """
            on:
              push:
                tags: ['v*']
            jobs:
              call:
                uses: ./.github/workflows/callee.yml
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_workflow_call_without_local_caller_is_a_violation(self) -> None:
        """An unknowable event set is a FAILURE, not a silent skip.

        This used to exit 0 with a NOTE line. That was a second exemption
        channel beside the ticketed allow-list, and an unbounded one: no ticket,
        never stale, covering every guard in the file at once, and not even
        limited to workflow_call-ONLY files — `workflow_call` alongside
        `push: [main]` would have silenced a live `pull_request` guard in the
        same file. It is now red unless a ticket-bearing UNRESOLVABLE_ALLOWED
        row covers it.
        """
        self.s.write(
            "orphan.yml",
            """
            on:
              workflow_call:
            jobs:
              j:
                if: github.event_name == 'push'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("orphan.yml", r.stderr)
        self.assertIn("UNRESOLVABLE", r.stderr)

    def test_unresolvable_with_no_guards_is_not_a_violation(self) -> None:
        """The permissive direction: nothing to resolve is not a failure.

        Without this, failing on every workflow_call-with-no-caller file
        regardless of content would satisfy the control above.
        """
        self.s.write(
            "orphan.yml",
            """
            on:
              workflow_call:
            jobs:
              j:
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        # min-guards 0: this fixture deliberately has no guard at all, so the
        # guard floor would fire for an unrelated reason and mask the verdict.
        r = run_gate(self.s.dir, "--min-workflows", "1", "--min-guards", "0")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_workflow_call_plus_push_still_reports_its_guards(self) -> None:
        """The case that makes the old channel worse than it looks.

        `workflow_call` + `push: [main]` with no local caller: the file has a
        perfectly ordinary, checkable trigger, and a live `pull_request` guard
        that is genuinely dead. The old code skipped the whole file.
        """
        self.s.write(
            "mixed.yml",
            """
            on:
              workflow_call:
              push:
                branches: [main]
            jobs:
              j:
                if: github.event_name == 'pull_request'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("mixed.yml", r.stderr)

    # ── floors ───────────────────────────────────────────────────────────────

    def test_floor_on_workflow_count(self) -> None:
        self.s.write("one.yml", "on:\n  push:\njobs:\n  j:\n    runs-on: x\n")
        r = run_gate(self.s.dir, "--min-workflows", "99", "--min-guards", "0")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("not looking at the tree", r.stderr)

    def test_floor_on_guard_count(self) -> None:
        self.s.write("one.yml", "on:\n  push:\njobs:\n  j:\n    runs-on: x\n")
        r = run_gate(self.s.dir, "--min-workflows", "1", "--min-guards", "99")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("FLOOR: parsed 0 github.event_name comparison(s)", r.stderr)

    def test_missing_directory_is_exit_2(self) -> None:
        r = run_gate(os.path.join(self.s.dir, "nope"))
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)

    # ── the real tree ────────────────────────────────────────────────────────

    def test_real_tree_is_green(self) -> None:
        r = run_gate(REAL_WORKFLOWS)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_real_tree_flags_pr_linux_without_allow_list(self) -> None:
        """`pr-linux` is a LIVE positive, not a hypothetical.

        The gate's only real-tree finding is currently suppressed by one
        allow-list row. That makes the allow-list the single thing standing
        between this gate and a red main — so the detection underneath it is
        asserted directly. If this test stops failing-over-to-detection, the
        gate has gone blind and the green run above means nothing.
        """
        r = run_gate(REAL_WORKFLOWS, "--no-allow-list")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("acceptance.yml", r.stderr)
        self.assertIn("pr-linux", r.stderr)
        self.assertIn("pull_request", r.stderr)

    def test_real_tree_allow_list_row_is_not_stale(self) -> None:
        """Every allow-list row must match something.

        An allow-list is prose, and prose rots — that is the whole subject of
        #7250. This one cannot rot silently: delete `pr-linux` and the row stops
        matching, which is a failure, not a quiet pass.
        """
        r = run_gate(REAL_WORKFLOWS)
        self.assertNotIn("STALE ALLOW-LIST ROW", r.stderr)
        self.assertIn("1 allow-listed", r.stdout)

    def test_stale_allow_list_row_fails(self) -> None:
        """The stale-row check itself is graded, on a tree with no pr-linux."""
        self.s.write(
            "acceptance.yml",
            """
            on:
              workflow_dispatch:
            jobs:
              acceptance:
                if: github.event_name == 'workflow_dispatch'
                runs-on: ubuntu-latest
                steps:
                  - run: true
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("STALE ALLOW-LIST ROW", r.stderr)

    def test_real_tree_matches_its_pinned_manifest_exactly(self) -> None:
        """The population check is an EXACT pin, not a floor with slack.

        The floors alone were a hole. Guards here are concentrated — three files
        carry 11 + 6 + 6 and thirteen carry none — so MIN_GUARDS=15 against a
        live 23 left room for a whole guard-bearing file to vanish while the run
        still printed "15 workflows, 17 comparisons" and exited 0, allow-list
        hit intact. This asserts the filename SET and the per-file guard counts,
        so a workflow that is added, removed or renamed is red until somebody
        re-pins it — and re-pinning is the moment they look at its guards.
        """
        mod = self._load_gate()
        actual = {
            f: len(mod.parse_workflow(os.path.join(REAL_WORKFLOWS, f)).guards)
            for f in os.listdir(REAL_WORKFLOWS)
            if f.endswith((".yml", ".yaml"))
        }
        self.assertEqual(actual, mod.SCAN_MANIFEST)
        # and the pin is not vacuously all-zero
        self.assertEqual(sum(mod.SCAN_MANIFEST.values()), 23)
        self.assertEqual(
            {k: v for k, v in mod.SCAN_MANIFEST.items() if v},
            {"acceptance.yml": 11, "test.yml": 6, "windows-cgo-experiment.yml": 6},
        )

    def test_manifest_fires_when_a_guard_bearing_workflow_vanishes(self) -> None:
        """The exact demonstration the floors could not make.

        A scratch copy of the real tree with test.yml deleted: the old floors
        passed it (16->15 workflows, 23->17 guards, both still above 12/15).
        The manifest must not.
        """
        import shutil

        for f in os.listdir(REAL_WORKFLOWS):
            if f.endswith((".yml", ".yaml")):
                shutil.copy(os.path.join(REAL_WORKFLOWS, f), self.s.dir)
        os.remove(os.path.join(self.s.dir, "test.yml"))

        # The floors, on their own, are green on this tree — the hole, shown.
        floors_only = run_gate(self.s.dir)
        self.assertEqual(floors_only.returncode, 0, floors_only.stdout + floors_only.stderr)
        self.assertIn("15 workflow(s)", floors_only.stdout)

        with_manifest = run_gate(self.s.dir, "--manifest")
        self.assertEqual(with_manifest.returncode, 1, with_manifest.stdout)
        self.assertIn("SCAN MANIFEST MISMATCH", with_manifest.stderr)
        self.assertIn("MISSING test.yml", with_manifest.stderr)

    def test_manifest_fires_on_a_guard_count_drift(self) -> None:
        """A guard the parser stops seeing is drift, not absence."""
        import shutil

        for f in os.listdir(REAL_WORKFLOWS):
            if f.endswith((".yml", ".yaml")):
                shutil.copy(os.path.join(REAL_WORKFLOWS, f), self.s.dir)
        target = os.path.join(self.s.dir, "windows-cgo-experiment.yml")
        with open(target, encoding="utf-8") as fh:
            body = fh.read()
        needle = "github.event_name == 'workflow_dispatch' ||"
        # Assert the occurrence count BEFORE editing: a silently-unapplied edit
        # would leave the tree pinned-and-matching and read as a passing test
        # arguing the opposite. This file has exactly two; one is neutralised.
        self.assertEqual(body.count(needle), 2, "anchor count changed upstream")
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(body.replace(needle, "true ||", 1))
        with open(target, encoding="utf-8") as fh:
            self.assertEqual(fh.read().count(needle), 1, "edit did not land")

        r = run_gate(self.s.dir, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout)
        self.assertIn("DRIFT windows-cgo-experiment.yml", r.stderr)
        self.assertIn("pinned 6 guard(s), scanned 5", r.stderr)

    def test_manifest_fires_on_an_unpinned_new_workflow(self) -> None:
        """A workflow added without a pin is red until somebody looks at it."""
        import shutil

        for f in os.listdir(REAL_WORKFLOWS):
            if f.endswith((".yml", ".yaml")):
                shutil.copy(os.path.join(REAL_WORKFLOWS, f), self.s.dir)
        self.s.write("brand-new.yml", "on:\n  push:\njobs:\n  j:\n    runs-on: x\n")

        r = run_gate(self.s.dir, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout)
        self.assertIn("UNPINNED brand-new.yml", r.stderr)

    def test_ci_actually_passes_the_manifest_flag(self) -> None:
        """A manifest nobody turns on is the same nothing as no manifest.

        The gate's strongest population check is opt-in, so the thing that must
        be pinned is that CI opts in. Without this, deleting `--manifest` from
        the workflow leaves all 28 controls green.
        """
        host = os.path.join(REPO, ".github", "workflows", "node-type-gate.yml")
        with open(host, encoding="utf-8") as fh:
            body = fh.read()
        self.assertIn(
            "run: python3 scripts/ci/workflow_event_gate.py --manifest", body
        )
        self.assertIn(
            "run: python3 -m unittest -v scripts.ci.test_workflow_event_gate", body
        )
        # and the host must stay always-on: a gate inside a dormant workflow is
        # the defect this whole change exists to catch.
        mod = self._load_gate()
        host_wf = mod.parse_workflow(host)
        self.assertIn("pull_request", host_wf.on_events)
        self.assertIn("push", host_wf.on_events)

    def _load_gate(self):
        import importlib.util

        spec = importlib.util.spec_from_file_location("weg", GATE)
        mod = importlib.util.module_from_spec(spec)
        assert spec.loader is not None
        # dataclasses resolves annotations through sys.modules on 3.12+.
        sys.modules["weg"] = mod
        self.addCleanup(sys.modules.pop, "weg", None)
        spec.loader.exec_module(mod)
        return mod


if __name__ == "__main__":
    unittest.main()
