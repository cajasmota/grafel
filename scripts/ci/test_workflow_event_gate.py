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

    def test_comment_does_not_create_a_guard(self) -> None:
        """The dormancy note in acceptance.yml quotes the guard verbatim.

        A comment-blind scan finds a violation inside the prose that DISCLOSES
        the violation, and the fix for that false positive is to delete the
        disclosure — the exact opposite of what #7250 asks for.
        """
        self.s.write(
            "commented.yml",
            """
            on:
              # `pull_request` removed; the job below is dormant (its
              # `if: github.event_name == 'pull_request'` guard never matches).
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
        self.assertIn("1 github.event_name comparison(s)", r.stdout)

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

    def test_workflow_call_without_local_caller_is_unresolvable(self) -> None:
        """No local caller means the event set is unknowable, not empty.

        Reporting it as a violation would be a fabricated verdict; skipping it
        silently would hide a whole workflow from the gate. It is printed and
        counted instead.
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
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("unresolvable", r.stdout)

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

    def test_real_tree_meets_its_own_floors(self) -> None:
        """The checked-in floors are below the tree, with room, and not at zero."""
        import importlib.util

        spec = importlib.util.spec_from_file_location("weg", GATE)
        mod = importlib.util.module_from_spec(spec)
        assert spec.loader is not None
        # dataclasses resolves annotations through sys.modules on 3.12+.
        sys.modules["weg"] = mod
        self.addCleanup(sys.modules.pop, "weg", None)
        spec.loader.exec_module(mod)
        paths = [
            os.path.join(REAL_WORKFLOWS, f)
            for f in os.listdir(REAL_WORKFLOWS)
            if f.endswith((".yml", ".yaml"))
        ]
        wfs = [mod.parse_workflow(p) for p in paths]
        guards = sum(len(w.guards) for w in wfs)
        self.assertGreater(mod.MIN_WORKFLOWS, 0)
        self.assertGreater(mod.MIN_GUARDS, 0)
        self.assertGreaterEqual(len(wfs), mod.MIN_WORKFLOWS)
        self.assertGreaterEqual(guards, mod.MIN_GUARDS)


if __name__ == "__main__":
    unittest.main()
