#!/usr/bin/env python3
"""Positive controls for scripts/ci/action_runtime_gate.py and its periodic
other half, scripts/ci/action_runtime_refresh.py (Refs #7253).

These are not optional. The gate is a scan-and-assert-absence tool, and that
shape has five independent ways to be a silent no-op: it reads nothing, it
reads the wrong files, it parses nothing out of them, it detects nothing, or it
detects and fails to fail. A count floor catches only the first. So each of the
five is driven here:

  reads nothing         -> test_floor_on_workflow_count,
                           test_missing_directory_is_exit_2
  wrong files           -> test_scans_yaml_and_yml
  parses nothing        -> test_floor_on_pin_count, test_step_and_job_uses_forms
  detects nothing       -> test_planted_node20_pin_fires,
                           test_planted_violation_in_a_copy_of_the_real_tree
  detects, doesn't fail -> every case above asserts the EXIT CODE, not the text

THE PERMISSIVE DIRECTION IS SCORED FIRST, because it is the one that matters: a
gate that PASSES a node20 pin it should have caught is invisible, while a gate
that trips on a legitimate pin is loud and self-correcting. So the planted
violation must FIRE (a gate that has never failed is not known to work), and it
must name the file, the line and the action.

`msys2/setup-msys2` is the standing regression case: already node24 at v2, no
v3 to bump to, and wrong under any rule of the form "every action at major N".
It is asserted from the real tree, and its manifest minimum is pinned at 2.

Run: python3 -m unittest -v scripts.ci.test_action_runtime_gate
"""

from __future__ import annotations

import contextlib
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
GATE = os.path.join(HERE, "action_runtime_gate.py")
REFRESH = os.path.join(HERE, "action_runtime_refresh.py")
REPO = os.path.dirname(os.path.dirname(HERE))
REAL_WORKFLOWS = os.path.join(REPO, ".github", "workflows")
HOST = os.path.join(REPO, ".github", "workflows", "node-type-gate.yml")
PERIODIC_HOST = os.path.join(REPO, ".github", "workflows", "grammar-freshness.yml")


def run_gate(directory: str, *extra: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, GATE, "--dir", directory, *extra],
        capture_output=True,
        text=True,
    )


def no_floors(directory: str, *extra: str) -> subprocess.CompletedProcess:
    """Against a synthetic tree, where the real floors do not apply."""
    return run_gate(directory, "--min-workflows", "1", "--min-pins", "1", *extra)


# ALLOWED is EMPTY in the checked-in gate, on purpose: every pin in this tree is
# on a supported runtime, so the exemption channel is closed rather than merely
# documented. That is also why no mutant reachable from the checked-in tree can
# grade it — a disclosed-as-uncovered shape is where to score, not where to
# stop. This driver runs the REAL main() with rows injected, so both arms
# (suppression, staleness) are exercised as shipped rather than re-implemented.
_DRIVER = """
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("arg", sys.argv[1])
mod = importlib.util.module_from_spec(spec)
sys.modules["arg"] = mod
spec.loader.exec_module(mod)
mod.ALLOWED.clear()
mod.ALLOWED.update({tuple(k.split("|")): v for k, v in json.loads(sys.argv[2]).items()})
sys.argv = ["action_runtime_gate.py"] + sys.argv[3:]
sys.exit(mod.main())
"""


def run_gate_with_allowed(
    directory: str, rows: dict[str, str], *extra: str
) -> subprocess.CompletedProcess:
    driver = os.path.join(directory, "_driver.py")
    with open(driver, "w", encoding="utf-8") as fh:
        fh.write(_DRIVER)
    return subprocess.run(
        [
            sys.executable,
            driver,
            GATE,
            json.dumps(rows),
            "--dir",
            directory,
            "--min-workflows",
            "1",
            "--min-pins",
            "1",
            *extra,
        ],
        capture_output=True,
        text=True,
    )


class Scratch:
    def __init__(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name

    def write(self, name: str, body: str) -> str:
        path = os.path.join(self.dir, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(textwrap.dedent(body).lstrip("\n"))
        return path

    def copy_real_tree(self) -> str:
        """A scratch copy of .github/workflows, so a planted violation can be
        driven against the REAL population without touching the real tree."""
        dst = os.path.join(self.dir, "workflows")
        shutil.copytree(REAL_WORKFLOWS, dst)
        return dst


def load(path: str, name: str):
    import importlib.util

    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod


class GateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.s = Scratch()
        self.addCleanup(self.s.tmp.cleanup)

    def _gate(self):
        mod = load(GATE, "arg")
        self.addCleanup(sys.modules.pop, "arg", None)
        return mod

    # ── the permissive direction, scored first ───────────────────────────────

    def test_real_tree_is_green(self) -> None:
        """Every pin in the tree today is on a supported runtime."""
        r = run_gate(REAL_WORKFLOWS, "--manifest")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_legitimate_pins_do_not_fire(self) -> None:
        """A gate that fires on everything is as useless as one that never does.

        Every action's exact minimum major, as measured upstream, in one file.
        """
        self.s.write(
            "ok.yml",
            """
            name: ok
            jobs:
              j:
                runs-on: ubuntu-latest
                steps:
                  - uses: actions/checkout@v5
                  - uses: actions/setup-go@v6
                  - uses: actions/setup-node@v5
                  - uses: actions/cache@v5
                  - uses: actions/github-script@v8
                  - uses: actions/upload-artifact@v6
                  - uses: actions/download-artifact@v7
                  - uses: softprops/action-gh-release@v3
                  - uses: msys2/setup-msys2@v2
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_a_major_above_the_minimum_does_not_fire(self) -> None:
        """The rule is a floor on the major, not equality with it."""
        self.s.write(
            "ahead.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout@v7
                  - uses: actions/download-artifact@v8
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # ── msys2: the mandatory regression case ─────────────────────────────────

    def test_msys2_v2_is_already_node24_and_is_not_flagged(self) -> None:
        """Already node24 at v2, and there is no v3 to bump it to.

        Any rule of the form "every action at major N" breaks this pin. It is
        asserted from the REAL tree (both spellings the tree carries) rather
        than from a synthetic one, and the manifest minimum is pinned at 2 so a
        well-meaning bump of the ROW is caught too.
        """
        mod = self._gate()
        self.assertEqual(mod.ACTION_RUNTIMES["msys2/setup-msys2"]["min_major"], 2)
        self.assertEqual(
            mod.ACTION_RUNTIMES["msys2/setup-msys2"]["at_min"], "node24"
        )

        pins = []
        for f in sorted(os.listdir(REAL_WORKFLOWS)):
            if f.endswith((".yml", ".yaml")):
                pins += [
                    p
                    for p in mod.parse_workflow(os.path.join(REAL_WORKFLOWS, f))
                    if p.action == "msys2/setup-msys2"
                ]
        refs = sorted({p.ref for p in pins})
        self.assertEqual(
            refs,
            ["v2", "v2.31.1"],
            "premise: the tree still carries the msys2 pins this case is about",
        )
        # Both spellings must pass, including the three-component one.
        for ref in refs:
            self.assertGreaterEqual(mod.major_of(ref), 2)
        r = run_gate(REAL_WORKFLOWS, "--no-allow-list")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertNotIn("msys2", r.stderr)

    def test_upload_and_download_artifact_minimums_differ(self) -> None:
        """The pair that breaks every lockstep assumption.

        download-artifact shipped TWO node20 majors (v5, v6) after
        upload-artifact's v5. A manifest that unified them would pass a
        `download-artifact@v6` pin on node20, which is the permissive direction.
        """
        mod = self._gate()
        self.assertEqual(mod.ACTION_RUNTIMES["actions/upload-artifact"]["min_major"], 6)
        self.assertEqual(
            mod.ACTION_RUNTIMES["actions/download-artifact"]["min_major"], 7
        )
        self.s.write(
            "pair.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/download-artifact@v6
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("actions/download-artifact", r.stderr)

    def test_every_real_pin_has_a_manifest_row(self) -> None:
        """Fail-closed is only meaningful if the tree is fully covered today.

        Also the census: 9 distinct actions, 10 distinct (action, ref) pairs.
        """
        mod = self._gate()
        actions, refs = set(), set()
        for f in sorted(os.listdir(REAL_WORKFLOWS)):
            if not f.endswith((".yml", ".yaml")):
                continue
            for p in mod.parse_workflow(os.path.join(REAL_WORKFLOWS, f)):
                if p.action is not None:
                    actions.add(p.action)
                    refs.add((p.action, p.ref))
        self.assertEqual(len(actions), 9, sorted(actions))
        self.assertEqual(len(refs), 10, sorted(refs))
        self.assertEqual(actions, set(mod.ACTION_RUNTIMES))

    # ── detection: the planted violation must FIRE ───────────────────────────

    def test_planted_node20_pin_fires(self) -> None:
        """`actions/checkout@v4` — the exact pin that was reintroduced — and the
        report must name the file, the line and the action."""
        self.s.write(
            "bad.yml",
            """
            name: bad
            jobs:
              j:
                runs-on: ubuntu-latest
                steps:
                  - uses: actions/checkout@v4
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("bad.yml:6", r.stderr)  # file AND line
        self.assertIn("actions/checkout", r.stderr)
        self.assertIn("v5", r.stderr)  # what it must be bumped to

    def test_planted_violation_in_a_copy_of_the_real_tree(self) -> None:
        """The same plant, against the REAL population, with --manifest on.

        A violation whose per-file `uses:` count is unchanged must still fire:
        the manifest is a population check, not the detector, and a gate that
        only fires when the count also moves would miss every in-place
        downgrade — which is the entire defect of #7253.
        """
        tree = self.s.copy_real_tree()
        target = os.path.join(tree, "windows.yml")
        with open(target, encoding="utf-8") as fh:
            body = fh.read()
        self.assertEqual(
            body.count("- uses: actions/checkout@v5"),
            1,
            "premise: the plant target is present exactly once",
        )
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(body.replace("- uses: actions/checkout@v5",
                                  "- uses: actions/checkout@v4", 1))
        with open(target, encoding="utf-8") as fh:
            after = fh.read()
        self.assertEqual(after.count("actions/checkout@v4"), 1, "the plant landed")

        r = run_gate(tree, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("windows.yml", r.stderr)
        self.assertIn("actions/checkout", r.stderr)
        self.assertNotIn("SCAN MANIFEST MISMATCH", r.stderr,
                         "the manifest must be intact: the DETECTOR fired")

        # restore, and the same tree is green again
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(body)
        r = run_gate(tree, "--manifest")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_unknown_action_is_a_violation_not_a_skip(self) -> None:
        """Fail closed. A ticket-free exemption for every newly added action is
        an exemption on exactly the day the pin is chosen."""
        self.s.write(
            "new.yml",
            """
            jobs:
              j:
                steps:
                  - uses: some/brand-new-action@v1
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("no ACTION_RUNTIMES row", r.stderr)

    def test_sha_pin_is_a_violation(self) -> None:
        """A commit SHA carries no major, so its runtime is unresolvable
        offline. Guessing would be the permissive direction."""
        self.s.write(
            "sha.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("no major version can be read", r.stderr)

    def test_branch_ref_is_a_violation(self) -> None:
        self.s.write(
            "branch.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout@main
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("no major version can be read", r.stderr)

    def test_unpinned_action_is_a_violation(self) -> None:
        """No `@ref` at all floats on the action's default branch."""
        self.s.write(
            "floating.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    # ── parsing ──────────────────────────────────────────────────────────────

    def test_step_and_job_uses_forms(self) -> None:
        """`- uses:` (step) and bare `uses:` (job-level reusable call) both
        count. A parser blind to the second sees release.yml's call as absent."""
        self.s.write(
            "forms.yml",
            """
            jobs:
              a:
                uses: ./.github/workflows/test.yml
              b:
                steps:
                  - uses: actions/checkout@v5
                  - name: x
                    uses: actions/cache@v5
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("3 `uses:` pin(s) (2 external, 1 local, 0 unparseable)", r.stdout)

    def test_local_call_is_counted_but_not_runtime_checked(self) -> None:
        """A local reusable workflow has no upstream `runs.using`. It must not
        be a violation, and must not be invisible either — an uncounted pin is
        one the manifest cannot see go missing."""
        self.s.write(
            "local.yml",
            """
            jobs:
              a:
                uses: ./.github/workflows/test.yml
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("1 `uses:` pin(s) (0 external, 1 local, 0 unparseable)", r.stdout)

    def test_comment_is_not_a_pin(self) -> None:
        """The false-positive direction. This repo's workflow headers discuss
        pins in prose; a comment-blind scan invents violations."""
        self.s.write(
            "commented.yml",
            """
            # historical note: this used to be `uses: actions/checkout@v4`
            jobs:
              j:
                steps:
                  # - uses: actions/setup-go@v5
                  - uses: actions/checkout@v5  # was v4 before 9242ccbea
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("1 `uses:` pin(s)", r.stdout)

    def test_quoted_hash_in_a_uses_value_is_not_a_comment(self) -> None:
        mod = self._gate()
        self.assertEqual(
            mod.strip_comment("  - uses: 'a/b@v1#x'  # tail"), "  - uses: 'a/b@v1#x'  "
        )

    def test_scans_yaml_and_yml(self) -> None:
        """Reading the wrong files is a distinct no-op from reading none."""
        self.s.write(
            "a.yaml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout@v4
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("a.yaml", r.stderr)

    def test_subpath_action_keeps_its_path_in_the_key(self) -> None:
        """`owner/repo/sub@v1` is a different action from `owner/repo@v1` and
        must not borrow its row."""
        mod = self._gate()
        self.assertEqual(
            mod.split_uses("actions/checkout/nested@v5"),
            ("actions/checkout/nested", "v5"),
        )
        self.s.write(
            "sub.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout/nested@v5
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("no ACTION_RUNTIMES row", r.stderr)

    def test_major_of_rejects_non_version_refs(self) -> None:
        """Branch names and decorated tags. The version-marker spellings and the
        all-digit boundary are graded separately, under CM-38 below."""
        mod = self._gate()
        self.assertEqual(mod.major_of("v5"), 5)
        self.assertEqual(mod.major_of("v2.31.1"), 2)
        self.assertEqual(mod.major_of("5.1"), 5)
        self.assertIsNone(mod.major_of("main"))
        self.assertIsNone(mod.major_of("v5-beta"))
        self.assertIsNone(mod.major_of(None))
        # TWO-DIGIT majors. Nothing in the tree exercises one today, so
        # narrowing the regex to a single digit left the suite green while this
        # control's name claimed to cover the spellings. github-script is at v9
        # upstream right now, so v10 is one release away — and a v10 read as
        # major 1 would be BELOW every floor, i.e. a loud false positive on the
        # day the bump lands rather than a silent pass. Still wrong, still
        # graded here.
        self.assertEqual(mod.major_of("v10"), 10)
        self.assertEqual(mod.major_of("v10.0.1"), 10)
        self.assertEqual(mod.major_of("v123"), 123)

    # ── floors and exit 2 ────────────────────────────────────────────────────

    def test_floor_on_workflow_count(self) -> None:
        self.s.write("one.yml", "jobs:\n  j:\n    steps:\n      - uses: actions/cache@v5\n")
        r = run_gate(self.s.dir, "--min-workflows", "12", "--min-pins", "1")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("FLOOR: read 1 workflow(s)", r.stderr)

    def test_floor_on_pin_count(self) -> None:
        self.s.write("one.yml", "jobs:\n  j:\n    steps:\n      - uses: actions/cache@v5\n")
        r = run_gate(self.s.dir, "--min-workflows", "1", "--min-pins", "50")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("FLOOR: parsed 1 `uses:` pin(s)", r.stderr)

    def test_missing_directory_is_exit_2(self) -> None:
        r = run_gate(os.path.join(self.s.dir, "nope"))
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)

    def test_a_readable_tree_is_not_exit_2(self) -> None:
        """Otherwise exit 2 could be the ordinary verdict and mean nothing."""
        self.s.write("ok.yml", "jobs:\n  j:\n    steps:\n      - uses: actions/cache@v5\n")
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # ── the manifest: an exact pin, not a floor ──────────────────────────────

    def test_real_tree_matches_its_pinned_manifest_exactly(self) -> None:
        mod = self._gate()
        actual, external = {}, 0
        for f in sorted(os.listdir(REAL_WORKFLOWS)):
            if not f.endswith((".yml", ".yaml")):
                continue
            pins = mod.parse_workflow(os.path.join(REAL_WORKFLOWS, f))
            actual[f] = len(pins)
            external += sum(1 for p in pins if p.action is not None)
        self.assertEqual(actual, mod.SCAN_MANIFEST)
        self.assertEqual(external, mod.TOTAL_EXTERNAL_PINS)
        self.assertEqual(external, 71)

    def test_manifest_fires_when_a_pin_bearing_workflow_vanishes(self) -> None:
        """The hole a floor leaves. release.yml alone carries 14 of 72 pins;
        deleting it whole still clears any floor with headroom."""
        tree = self.s.copy_real_tree()
        os.remove(os.path.join(tree, "release.yml"))
        self.assertEqual(run_gate(tree).returncode, 0, "premise: floors alone pass")
        r = run_gate(tree, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("MISSING release.yml", r.stderr)

    def test_manifest_fires_on_a_per_file_count_drift(self) -> None:
        tree = self.s.copy_real_tree()
        target = os.path.join(tree, "windows.yml")
        with open(target, encoding="utf-8") as fh:
            body = fh.read()
        self.assertEqual(body.count("- uses: actions/checkout@v5"), 1, "premise")
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(body.replace("- uses: actions/checkout@v5", "", 1))
        r = run_gate(tree, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("DRIFT windows.yml: pinned 3 `uses:` line(s), scanned 2", r.stderr)

    def test_manifest_fires_on_an_unpinned_new_workflow(self) -> None:
        tree = self.s.copy_real_tree()
        with open(os.path.join(tree, "brand-new.yml"), "w", encoding="utf-8") as fh:
            fh.write("jobs:\n  j:\n    steps:\n      - uses: actions/cache@v5\n")
        r = run_gate(tree, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("UNPINNED brand-new.yml", r.stderr)

    def test_external_total_catches_what_per_file_counts_cannot(self) -> None:
        """Rewriting an external pin as a local call leaves every per-file
        `uses:` count identical while removing a pin from the runtime check.
        TOTAL_EXTERNAL_PINS is the only thing that sees it."""
        tree = self.s.copy_real_tree()
        target = os.path.join(tree, "windows.yml")
        with open(target, encoding="utf-8") as fh:
            body = fh.read()
        self.assertEqual(body.count("- uses: actions/checkout@v5"), 1, "premise")
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(
                body.replace(
                    "- uses: actions/checkout@v5",
                    "- uses: ./.github/workflows/test.yml",
                    1,
                )
            )
        r = run_gate(tree, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn("SCAN MANIFEST MISMATCH", r.stderr,
                         "per-file counts are untouched — that is the point")
        self.assertIn("EXTERNAL PIN TOTAL MISMATCH", r.stderr)

    def test_print_manifest_emits_the_tree_manifest(self) -> None:
        r = run_gate(REAL_WORKFLOWS, "--print-manifest")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('"release.yml": 14,', r.stdout)
        self.assertIn("TOTAL_EXTERNAL_PINS = 71", r.stdout)

    # ── allow-list: both arms, on an empty-by-design dict ────────────────────

    def test_allow_list_row_suppresses_one_pin(self) -> None:
        self.s.write(
            "bad.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout@v4
            """,
        )
        self.assertEqual(no_floors(self.s.dir).returncode, 1, "premise: it fires")
        r = run_gate_with_allowed(
            self.s.dir, {"bad.yml|actions/checkout|v4": "#9999: pinned for X"}
        )
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("ALLOWED bad.yml:4", r.stdout)

    def test_allow_list_row_that_matches_nothing_is_stale(self) -> None:
        """An allow-list is prose, and prose rots. It cannot rot silently."""
        self.s.write(
            "bad.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/checkout@v5
            """,
        )
        r = run_gate_with_allowed(
            self.s.dir, {"bad.yml|actions/checkout|v4": "#9999: pinned for X"}
        )
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("STALE ALLOW-LIST ROW", r.stderr)

    def test_allow_list_does_not_leak_across_workflows(self) -> None:
        """The key includes the FILENAME: a justification written about one
        workflow's pin is not true of another's."""
        self.s.write(
            "a.yml",
            "jobs:\n  j:\n    steps:\n      - uses: actions/checkout@v4\n",
        )
        self.s.write(
            "b.yml",
            "jobs:\n  j:\n    steps:\n      - uses: actions/checkout@v4\n",
        )
        r = run_gate_with_allowed(
            self.s.dir, {"a.yml|actions/checkout|v4": "#9999: only a.yml"}
        )
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("b.yml", r.stderr)
        self.assertNotIn("VIOLATION a.yml", r.stderr)

    def test_no_allow_list_flag_reports_what_the_list_covers(self) -> None:
        self.s.write(
            "bad.yml",
            "jobs:\n  j:\n    steps:\n      - uses: actions/checkout@v4\n",
        )
        r = run_gate_with_allowed(
            self.s.dir,
            {"bad.yml|actions/checkout|v4": "#9999: pinned for X"},
            "--no-allow-list",
        )
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_checked_in_allow_list_is_empty(self) -> None:
        """The exemption channel is closed, not merely documented. If a row is
        ever added, this control is the place that records the decision."""
        self.assertEqual(self._gate().ALLOWED, {})

    # ── the host job: a skipped job reports SUCCESS ──────────────────────────

    def _job_block(self, path: str, job: str) -> str:
        """One job's own text, bounded.

        A `  # ──` banner ENDS the block. Without that the block ran on into the
        next job's banner comment, and the reachability control below then read
        the words `if:` out of that comment's PROSE and failed on a job that has
        no `if:` at all. A control whose verdict depends on the wording of a
        neighbouring comment is measuring the wrong thing in both directions:
        it can fail on clean configuration, and a banner that happens to avoid
        the words would let a real `if:` through. The extraction control below
        pins that the boundary really is drawn.
        """
        with open(path, encoding="utf-8") as fh:
            lines = fh.read().splitlines()
        start = next(i for i, ln in enumerate(lines) if ln == f"  {job}:")
        end = len(lines)
        for i in range(start + 1, len(lines)):
            ln = lines[i]
            if ln.startswith("  # ──"):
                end = i
                break
            if ln.strip() and not ln.startswith("    ") and not ln.startswith("  #"):
                end = i
                break
        return "\n".join(lines[start:end])

    def test_host_job_cannot_be_skipped_as_success(self) -> None:
        """A skipped job reports SUCCESS. So does a continue-on-error step.

        Asserting the WORKFLOW's triggers is the wrong altitude — it leaves
        `if: false` on the job and `continue-on-error: true` on the step both
        undetected. Job-level reachability is asserted on the job's own block.
        """
        block = self._job_block(HOST, "action-runtime-gate")
        self.assertIn("runs-on: ubuntu-latest", block, "premise: job block found")
        self.assertIn("action_runtime_gate.py --manifest", block)
        for forbidden in ("if:", "continue-on-error", "needs:"):
            self.assertNotIn(
                forbidden,
                block,
                f"`{forbidden}` in the action-runtime-gate job: it can then be "
                f"skipped or excused, and a skipped job reports success",
            )

    def test_host_job_block_extraction_is_not_vacuous(self) -> None:
        """The control above is worthless if the block is empty or the whole
        file: both failure modes pass `assertNotIn` trivially."""
        block = self._job_block(HOST, "action-runtime-gate")
        self.assertTrue(block.startswith("  action-runtime-gate:"))
        self.assertNotIn("node-type-gate:", block)
        self.assertNotIn("workflow_event_gate.py", block)
        self.assertNotIn("go run ./tools/node-type-gate", block)
        self.assertGreater(len(block.splitlines()), 8)
        self.assertLess(len(block.splitlines()), 60)

    def test_ci_actually_passes_the_manifest_flag(self) -> None:
        """A manifest nobody turns on is the same nothing as no manifest.

        The strongest population check is opt-in, so what must be pinned is that
        CI opts in. Without this, deleting `--manifest` from the workflow leaves
        every other control in this file green.
        """
        with open(HOST, encoding="utf-8") as fh:
            body = fh.read()
        self.assertIn("run: python3 scripts/ci/action_runtime_gate.py --manifest", body)
        self.assertIn(
            "run: python3 -m unittest -v scripts.ci.test_action_runtime_gate", body
        )

    def test_host_workflow_is_always_on(self) -> None:
        """A gate inside a dormant workflow is the defect this exists to catch.

        Not a substitute for the job-block control above — it is the other
        altitude, and both are needed.
        """
        with open(HOST, encoding="utf-8") as fh:
            head = fh.read().split("\njobs:", 1)[0]
        self.assertRegex(head, r"(?m)^  pull_request:")
        self.assertRegex(head, r"(?m)^  push:")

    # ── half 2: the periodic re-derivation ───────────────────────────────────

    def test_periodic_job_is_wired_and_not_skippable(self) -> None:
        """The offline manifest is prose. This job is the only thing that stops
        it rotting silently, so it must actually be scheduled and must not be
        excusable."""
        with open(PERIODIC_HOST, encoding="utf-8") as fh:
            body = fh.read()
        self.assertIn("run: python3 scripts/ci/action_runtime_refresh.py", body)
        head = body.split("\njobs:", 1)[0]
        self.assertRegex(head, r"(?m)^  schedule:")
        self.assertRegex(head, r"(?m)^    - cron:")
        block = self._job_block(PERIODIC_HOST, "action-runtime-refresh")
        self.assertIn("runs-on: ubuntu-latest", block, "premise: job block found")
        self.assertIn("action_runtime_refresh.py", block)
        self.assertTrue(block.startswith("  action-runtime-refresh:"))
        self.assertNotIn("freshness:", block)
        self.assertNotIn("grammar-freshness", block)
        self.assertGreater(len(block.splitlines()), 8)
        self.assertLess(len(block.splitlines()), 40)
        for forbidden in ("if:", "continue-on-error", "needs:"):
            self.assertNotIn(forbidden, block, f"`{forbidden}` in the refresh job")

    def test_both_halves_named_in_the_host_failure_lists(self) -> None:
        """The workflow headers forbid adding a failure path without listing it.

        Each new exit-1 cause is named in its host's exhaustive list, in the
        same commit that added it.
        """
        with open(HOST, encoding="utf-8") as fh:
            head = fh.read()
        for phrase in (
            "no ACTION_RUNTIMES row",
            "first node24 major",
            "no readable major",
            # round 2 added TWO exit-1 causes to the gate. The all-digit-SHA
            # widening of cause 3 and the continued-`uses:` cause 3b were both
            # written into the header and NEITHER was added here — a mutant
            # deleting cause 3b from the header stayed ALIVE. The rule this
            # control enforces is exactly the rule it had just broken.
            "ALL-DIGIT abbreviated commit SHA",
            "VERSION MARKER",
            "a `uses:` key whose value sits on a FOLLOWING line",
            "an ALLOWED row matched nothing",
            "external-pin\n#      total does not match the manifest",
            "fewer workflows or fewer pins than its floors",
        ):
            self.assertIn(phrase, head, f"missing from the action-runtime-gate list: {phrase}")
        with open(PERIODIC_HOST, encoding="utf-8") as fh:
            phead = fh.read()
        for phrase in ("TOO LOW", "TOO HIGH", "recorded evidence", "deprecated or non-Node"):
            self.assertIn(phrase, phead, f"missing from the refresh list: {phrase}")


    # ── F1: a `uses:` whose value is on the next line ────────────────────────

    def test_continued_uses_is_a_violation(self) -> None:
        """THE HOLE THAT SHIPPED IN THE FIRST ROUND.

            - uses:
                actions/checkout@v4

        is legal YAML and legal Actions. The same-line pattern cannot see it, so
        a reintroduced `checkout@v4` written this way passed FULLY GREEN in a
        copy of the real tree — 0 violations, rc 0 — with the per-file count,
        the filename set AND the external total all still matching, because the
        `uses:` LINE is still there and still counted. That settles a question
        the manifest section raises: all three population checks can be correct
        while a pin escapes. The manifest is not a backstop for a parser blind
        spot, so the parser has to refuse the form.
        """
        self.s.write(
            "continued.yml",
            """
            jobs:
              j:
                runs-on: ubuntu-latest
                steps:
                  - uses:
                      actions/checkout@v4
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("continued.yml:5", r.stderr)
        self.assertIn("value is on a following line", r.stderr)

    def test_continued_uses_in_a_copy_of_the_real_tree(self) -> None:
        """The measured scenario, end to end: planted beside a legitimate pin in
        the real population, with --manifest on and every count intact."""
        tree = self.s.copy_real_tree()
        target = os.path.join(tree, "windows.yml")
        with open(target, encoding="utf-8") as fh:
            body = fh.read()
        old = "      - uses: actions/checkout@v5\n"
        self.assertEqual(body.count(old), 1, "premise: the plant target")
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(body.replace(old, "      - uses:\n          actions/checkout@v4\n", 1))
        with open(target, encoding="utf-8") as fh:
            after = fh.read()
        self.assertEqual(after.count("actions/checkout@v4"), 1, "the plant landed")

        r = run_gate(tree, "--manifest")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("windows.yml", r.stderr)
        # The population checks are NOT what caught it: the `uses:` line is
        # still present, so the per-file count is unchanged. Asserting that
        # here is what stops this control silently degrading into a duplicate
        # of the manifest tests.
        self.assertNotIn("SCAN MANIFEST MISMATCH", r.stderr)
        self.assertIn("value is on a following line", r.stderr)

    def test_continued_uses_is_counted_so_the_manifest_still_adds_up(self) -> None:
        """An unparseable pin must not vanish from the census as well."""
        self.s.write(
            "continued.yml",
            """
            jobs:
              j:
                steps:
                  - uses:
                      actions/checkout@v4
                  - uses: actions/cache@v5
            """,
        )
        r = no_floors(self.s.dir)
        self.assertIn("2 `uses:` pin(s) (1 external, 0 local, 1 unparseable)", r.stdout)

    # ── F2 / CM-38: a major needs a VERSION MARKER, not a length ─────────────

    def test_an_unmarked_run_of_digits_is_never_a_major(self) -> None:
        """EXHAUSTIVE OVER LENGTH, because the bug was a length threshold.

        Round 2 shipped `SHA_LIKE = ^[0-9a-f]{7,40}$` consulted before the
        version regex. That closed `1234567` and left SIX characters open:
        `actions/checkout@123456` is a legal pin (GitHub resolves any
        unambiguous prefix; `git rev-parse --short=6` produces six) and it
        parsed as major 123456, clearing every floor forever. A 6-char prefix
        is all-digits with p ~= 6% — HIGHER than the 7-char case the guard was
        written for — so the threshold sat on the wrong side of its own
        argument.

        Lowering the threshold would only move the boundary, so the boundary is
        gone: a major is recognised only with a `v` prefix or an embedded dot,
        and a commit SHA can carry neither. This enumerates EVERY length from 1
        to 40 rather than sampling, because sampling is what let the last
        threshold through — a hand-picked `1234567` said nothing about `123456`.
        """
        mod = self._gate()
        for n in range(1, 41):
            self.assertIsNone(mod.major_of("1" * n), f"{n} digits")
            self.assertIsNone(mod.major_of("9" * n), f"{n} nines")
        # the specific neighbours of the retired threshold, named so that
        # re-introducing a length cannot pass by moving it
        for ref in ("12345", "123456", "1234567", "12345678"):
            self.assertIsNone(mod.major_of(ref), ref)
        # hex and non-hex alike, and the case-varied form (loud either way)
        for ref in ("deadbeef", "DEADBEEF", "a" * 40, "0123456789abcdef"):
            self.assertIsNone(mod.major_of(ref), ref)

    def test_the_version_marker_spellings_survive(self) -> None:
        """The permissive direction for CM-38: removing the inference must not
        cost a spelling anything actually uses.

        Verified against the tree before the rule was chosen — every ref here is
        `v`-prefixed — so the bare `5` spelling costs nothing. That check was
        the point: an earlier version of this control asserted bare `5` was "a
        spelling the tree uses", nothing had ever checked, and it was FALSE.
        """
        mod = self._gate()
        self.assertEqual(mod.major_of("v5"), 5)
        self.assertEqual(mod.major_of("v10"), 10)
        self.assertEqual(mod.major_of("v10.0.1"), 10)
        self.assertEqual(mod.major_of("v123"), 123)
        self.assertEqual(mod.major_of("v2.31.1"), 2)
        self.assertEqual(mod.major_of("v1234567"), 1234567)  # `v` says version
        self.assertEqual(mod.major_of("5.1"), 5)  # a dot says version
        self.assertEqual(mod.major_of("2.31.1"), 2)
        # and every ref the real tree carries resolves
        actual = set()
        for f in sorted(os.listdir(REAL_WORKFLOWS)):
            if f.endswith((".yml", ".yaml")):
                for pin in mod.parse_workflow(os.path.join(REAL_WORKFLOWS, f)):
                    if pin.action is not None and pin.ref:
                        actual.add(pin.ref)
        self.assertEqual(
            actual,
            {"v2", "v2.31.1", "v3", "v5", "v6", "v7", "v8"},
            "premise: the tree's ref spellings, which the rule was chosen against",
        )
        for ref in actual:
            self.assertIsNotNone(mod.major_of(ref), ref)

    def test_no_length_threshold_remains_in_the_rule(self) -> None:
        """The retired constant must not creep back.

        A length threshold reads as principled and is arbitrary; this is the
        cheapest thing that notices one returning. The rule is two regexes with
        no repetition bound, and the retired `SHA_LIKE` name is gone rather than
        left as a second, redundant guard — two guards that can only fire
        together grade neither.
        """
        mod = self._gate()
        src = open(GATE, encoding="utf-8").read()
        self.assertNotIn("SHA_LIKE", src)
        self.assertFalse(hasattr(mod, "SHA_LIKE"))
        self.assertEqual(mod.REF_MAJOR_V.pattern, r"^v(\d+)(?:\.\d+)*$")
        self.assertEqual(mod.REF_MAJOR_DOTTED.pattern, r"^(\d+)(?:\.\d+)+$")

    def test_unmarked_digit_pins_are_violations_end_to_end(self) -> None:
        """Not just the helper: both sides of the retired boundary must fail as
        real pins, with the message naming the marker rule."""
        for ref in ("1234567", "123456"):
            with self.subTest(ref=ref):
                scratch = Scratch()
                self.addCleanup(scratch.tmp.cleanup)
                scratch.write(
                    "shortsha.yml",
                    f"""
                    jobs:
                      j:
                        steps:
                          - uses: actions/checkout@{ref}
                    """,
                )
                r = no_floors(scratch.dir)
                self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
                self.assertIn("no major version can be read", r.stderr)
                self.assertIn("version marker", r.stderr)

    # ── F7: the sub-path violation must say what to do ───────────────────────

    def test_subpath_violation_names_the_row_to_add(self) -> None:
        """`actions/cache/restore@v5` is a legitimate, common pin and IS a
        violation here by design. A contributor hitting it needs the hint, or
        the fail-closed rule reads as a bug."""
        self.s.write(
            "sub.yml",
            """
            jobs:
              j:
                steps:
                  - uses: actions/cache/restore@v5
            """,
        )
        r = no_floors(self.s.dir)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("actions/cache/restore", r.stderr)
        self.assertIn("needs its own row", r.stderr)

    # ── F6: the drift report job ─────────────────────────────────────────────

    def test_job_block_boundaries_are_real(self) -> None:
        """Grades the extractor the two reachability controls rest on.

        Both failure modes of a block extractor pass `assertNotIn` for the wrong
        reason: an empty block trivially contains nothing, and an over-long one
        drags in a neighbour's text. The second is not hypothetical — the block
        for `action-runtime-refresh` ran into the NEXT job's banner comment and
        the reachability control failed on the word `if:` appearing in that
        comment's prose, on a job carrying no `if:` at all.
        """
        gate_block = self._job_block(PERIODIC_HOST, "action-runtime-refresh")
        report_block = self._job_block(PERIODIC_HOST, "action-runtime-refresh-report")
        self.assertTrue(gate_block.startswith("  action-runtime-refresh:"))
        self.assertTrue(
            report_block.startswith("  action-runtime-refresh-report:")
        )
        # the gate block stops AT the report job's banner, not inside it
        self.assertNotIn("THIRD JOB", gate_block)
        self.assertNotIn("action-runtime-refresh-report", gate_block)
        self.assertNotIn("upsert-tracking-issue", gate_block)
        # and neither block is the whole file
        self.assertNotIn("freshness:", gate_block)
        self.assertNotIn("freshness:", report_block)
        self.assertGreater(len(gate_block.splitlines()), 8)
        self.assertLess(len(gate_block.splitlines()), 40)
        self.assertGreater(len(report_block.splitlines()), 8)
        self.assertLess(len(report_block.splitlines()), 70)

    def test_drift_report_job_is_wired(self) -> None:
        """A monthly cron that only goes red is a notification nobody opens.

        The report job must depend on the gate job, run on its failure, and use
        the repo's existing idempotent upsert rather than a second mechanism.
        """
        with open(PERIODIC_HOST, encoding="utf-8") as fh:
            body = fh.read()
        block = self._job_block(PERIODIC_HOST, "action-runtime-refresh-report")
        self.assertIn("needs: [action-runtime-refresh]", block)
        self.assertIn("if: failure()", block)
        self.assertIn("action_runtime_refresh.py --markdown > issue-body.md", block)
        self.assertIn(".github/scripts/upsert-tracking-issue.sh", block)
        self.assertIn("ISSUE_MARKER: 'grafel-tracker:action-runtime-drift'", block)
        # The DECIDE step. Without it the job files a tracking issue on every
        # red run, including the transient one it exists to distinguish — and a
        # tracker that fires monthly regardless is the notification nobody opens
        # all over again. A mutant replacing the grep with `true` was ALIVE.
        import importlib.util as _il
        spec = _il.spec_from_file_location("arr_marker", REFRESH)
        marker_mod = _il.module_from_spec(spec)
        sys.modules["arr_marker"] = marker_mod
        self.addCleanup(sys.modules.pop, "arr_marker", None)
        self.addCleanup(sys.modules.pop, "action_runtime_gate", None)
        spec.loader.exec_module(marker_mod)
        # the grep and the renderer must agree on the marker, across two files
        self.assertIn(f"grep -q '{marker_mod.DRIFT_MARKER}' issue-body.md", block)
        self.assertIn("steps.drift.outputs.any == 'true'", block)
        self.assertIn("steps.drift.outputs.any == 'false'", block)
        self.assertTrue(os.path.exists(
            os.path.join(REPO, ".github", "scripts", "upsert-tracking-issue.sh")
        ))
        self.assertIn("issues: write", body)

    def test_the_gate_job_never_passes_the_render_only_flag(self) -> None:
        """`--markdown` exits 0 whatever it finds. In the VERDICT job it would
        be precisely the green no-op this whole change exists to prevent —
        the `--manifest`-missing lesson, one flag over."""
        block = self._job_block(PERIODIC_HOST, "action-runtime-refresh")
        self.assertIn("action_runtime_refresh.py --print", block)
        self.assertNotIn("--markdown", block)


class RefreshTest(unittest.TestCase):
    """Half 2, driven offline. The network is stubbed: these grade the drift
    LOGIC, not GitHub's availability. The live run is the monthly job."""

    def _refresh(self):
        mod = load(REFRESH, "arr")
        self.addCleanup(sys.modules.pop, "arr", None)
        self.addCleanup(sys.modules.pop, "action_runtime_gate", None)
        return mod

    def test_supported_classifies_the_real_runtimes(self) -> None:
        mod = self._refresh()
        self.assertTrue(mod.supported("node24"))
        self.assertTrue(mod.supported("node28"))
        for bad in ("node12", "node16", "node20", "composite", "docker", None):
            self.assertFalse(mod.supported(bad), bad)

    def test_using_line_regex_reads_the_real_shapes(self) -> None:
        mod = self._refresh()
        for line, want in (
            ("  using: node24", "node24"),
            ("  using: 'node20'", "node20"),
            ('  using: "composite"', "composite"),
        ):
            m = mod.USING_LINE.match(line)
            self.assertIsNotNone(m, line)
            self.assertEqual(m.group(1), want)
        # a `using:` at column 0 is not inside a `runs:` block
        self.assertIsNone(mod.USING_LINE.match("using: node24"))

    def _run_main(self, mod, table: dict[str, str], argv=None) -> tuple[int, str]:
        """Drive the real main() with runs_using stubbed from a lookup table.

        Returns the exit code AND stderr, because the exit code alone does not
        distinguish the drift ARMS. Measured, not assumed: suppressing the
        `TOO LOW` arm entirely left the suite fully green, because a row whose
        minimum has become node20 ALSO has stale recorded evidence, and that
        second arm fired instead. Two guards that can only fire together grade
        neither, so every arm below asserts its own classification.
        """
        mod.runs_using = lambda action, ref: table.get(f"{action}@{ref}")
        old = sys.argv
        sys.argv = ["action_runtime_refresh.py", "--dir", REAL_WORKFLOWS] + (argv or [])
        err = io.StringIO()
        try:
            with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                rc = mod.main()
        finally:
            sys.argv = old
        return rc, err.getvalue()

    def _real_table(self, mod, overrides: dict[str, str] | None = None) -> dict[str, str]:
        """An upstream that agrees with the checked-in manifest, plus the refs
        the real tree pins — then whatever the caller wants to break."""
        gate = sys.modules["action_runtime_gate"]
        table: dict[str, str] = {}
        for action, row in gate.ACTION_RUNTIMES.items():
            m = row["min_major"]
            table[f"{action}@v{m}"] = row["at_min"]
            if row["below_min"] is not None:
                table[f"{action}@v{m - 1}"] = row["below_min"]
        for f in sorted(os.listdir(REAL_WORKFLOWS)):
            if not f.endswith((".yml", ".yaml")):
                continue
            for p in gate.parse_workflow(os.path.join(REAL_WORKFLOWS, f)):
                if p.action is not None and p.ref:
                    table.setdefault(f"{p.action}@{p.ref}", "node24")
        table.update(overrides or {})
        return table

    def test_agreeing_upstream_is_green(self) -> None:
        """The permissive direction for half 2."""
        mod = self._refresh()
        rc, err = self._run_main(mod, self._real_table(mod))
        self.assertEqual(rc, 0, err)

    def test_manifest_too_low_is_drift(self) -> None:
        """The direction that matters: the offline gate has been passing pins
        on a deprecated runtime. Its own arm is asserted, not just exit 1."""
        mod = self._refresh()
        table = self._real_table(mod, {"actions/checkout@v5": "node20"})
        rc, err = self._run_main(mod, table)
        self.assertEqual(rc, 1)
        self.assertIn("DRIFT (manifest TOO LOW) actions/checkout", err)

    def test_manifest_too_high_is_drift(self) -> None:
        """The offline gate has been rejecting legitimate pins."""
        mod = self._refresh()
        table = self._real_table(mod, {"actions/checkout@v4": "node24"})
        rc, err = self._run_main(mod, table)
        self.assertEqual(rc, 1)
        self.assertIn("DRIFT (manifest TOO HIGH) actions/checkout", err)

    def test_stale_recorded_evidence_is_drift(self) -> None:
        """Still supported, but the row no longer describes upstream.

        `node26` is chosen so this fires the evidence arm ALONE: it separates
        the third arm from the first two rather than resting on whichever of
        them happens to fire.
        """
        mod = self._refresh()
        table = self._real_table(mod, {"actions/checkout@v5": "node26"})
        rc, err = self._run_main(mod, table)
        self.assertEqual(rc, 1)
        self.assertIn("DRIFT (recorded evidence stale) actions/checkout", err)
        self.assertNotIn("TOO LOW", err)
        self.assertNotIn("TOO HIGH", err)

    def test_a_pinned_ref_on_a_deprecated_runtime_is_drift(self) -> None:
        """Checked against the ref the tree really carries, not a summary of
        it: `v2.31.1` is a distinct fact from `v2`."""
        mod = self._refresh()
        table = self._real_table(mod, {"msys2/setup-msys2@v2.31.1": "node20"})
        rc, err = self._run_main(mod, table)
        self.assertEqual(rc, 1)
        self.assertIn(
            "VIOLATION (pinned ref on a deprecated runtime) "
            "msys2/setup-msys2@v2.31.1",
            err,
        )
        # and the manifest row for v2 is untouched, so no manifest arm fires
        self.assertNotIn("TOO LOW", err)

    def test_a_vanished_upstream_tag_is_drift(self) -> None:
        mod = self._refresh()
        table = self._real_table(mod)
        del table["actions/download-artifact@v7"]
        rc, err = self._run_main(mod, table)
        self.assertEqual(rc, 1)
        self.assertIn("DRIFT (pinned ref is gone) actions/download-artifact@v7", err)

    def test_below_min_evidence_drift_is_reported(self) -> None:
        """F3 — THE FOURTH ARM, and the same masking as the first two.

        `_real_table` only ever perturbed the `@v{min_major}` entry, so nothing
        in the suite ever moved `@v{min_major - 1}`. Mutating the `below_min`
        comparison to never fire left all 51 tests green: half of every row's
        recorded evidence — including the half the failure-list prose promises —
        was asserted by prose alone. `node16` keeps v4 unsupported, so the
        TOO HIGH arm cannot fire and this arm is scored on its own.
        """
        mod = self._refresh()
        table = self._real_table(mod, {"actions/checkout@v4": "node16"})
        rc, err = self._run_main(mod, table)
        self.assertEqual(rc, 1)
        self.assertIn("DRIFT (recorded evidence stale) actions/checkout", err)
        self.assertIn("below_min", err)
        self.assertNotIn("TOO HIGH", err)
        self.assertNotIn("TOO LOW", err)

    def test_drift_found_before_a_failure_is_not_erased(self) -> None:
        """F5 — an upstream blip used to DESTROY a real finding.

        The whole derivation sat in one `try` with a bare `return 2`, and rows
        are walked in sorted order — so a 503 on `msys2` swallowed a `TOO LOW`
        on `actions/cache` or `actions/checkout` found seconds earlier. Measured:
        exit 2 with `TOO LOW` absent from the output entirely. Exit 2 was
        supposed to merely LOOK different from a rotted manifest; instead it
        erased the evidence.

        A run that established drift before failing now exits 1 and prints it:
        the drift is a fact about this repository, and the failure is not a
        reason to doubt it.
        """
        mod = self._refresh()
        table = self._real_table(mod, {"actions/checkout@v5": "node20"})
        real = mod.runs_using

        def flaky(action, ref):
            # sorted order: cache, checkout, ..., msys2 is late
            if action.startswith("msys2/"):
                raise mod.Unavailable("HTTP 503 Service Unavailable")
            return table.get(f"{action}@{ref}")

        mod.runs_using = flaky
        old = sys.argv
        sys.argv = ["action_runtime_refresh.py", "--dir", REAL_WORKFLOWS]
        err = io.StringIO()
        try:
            with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                rc = mod.main()
        finally:
            sys.argv = old
            mod.runs_using = real
        out = err.getvalue()
        self.assertEqual(rc, 1, out)
        self.assertIn("503", out, "the transient is still reported")
        self.assertIn("CUT SHORT", out)
        self.assertIn("DRIFT (manifest TOO LOW) actions/checkout", out)

    def test_a_failure_with_no_drift_yet_is_still_exit_2(self) -> None:
        """The other side of F5: when nothing was established, the verdict
        really is unknown and must not masquerade as a clean bill either way."""
        mod = self._refresh()
        table = self._real_table(mod)

        def flaky(action, ref):
            if action == "actions/cache":  # first in sorted order
                raise mod.Unavailable("HTTP 503 Service Unavailable")
            return table.get(f"{action}@{ref}")

        mod.runs_using = flaky
        old = sys.argv
        sys.argv = ["action_runtime_refresh.py", "--dir", REAL_WORKFLOWS]
        err = io.StringIO()
        try:
            with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                rc = mod.main()
        finally:
            sys.argv = old
        self.assertEqual(rc, 2, err.getvalue())

    # ── F6: the render-only mode ─────────────────────────────────────────────

    def test_markdown_mode_marks_drift_and_never_decides(self) -> None:
        """The report job's input. It must exit 0 whatever it finds — it is a
        renderer — and it must carry the marker the workflow greps for, or the
        tracking issue is never filed."""
        mod = self._refresh()
        table = self._real_table(mod, {"actions/checkout@v5": "node20"})
        mod.runs_using = lambda a, r: table.get(f"{a}@{r}")
        old = sys.argv
        sys.argv = ["action_runtime_refresh.py", "--dir", REAL_WORKFLOWS, "--markdown"]
        out = io.StringIO()
        try:
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
                rc = mod.main()
        finally:
            sys.argv = old
        body = out.getvalue()
        self.assertEqual(rc, 0, "render-only never decides")
        self.assertIn("grafel-drift:yes", body)
        self.assertIn("TOO LOW", body)
        self.assertIn("| `actions/checkout` | v5 |", body)
        # and the summary line must NOT land in the issue body
        self.assertNotIn("drift finding(s).", body)

    def test_markdown_mode_omits_the_marker_when_clean(self) -> None:
        """Otherwise the workflow files a tracking issue every single month."""
        mod = self._refresh()
        table = self._real_table(mod)
        mod.runs_using = lambda a, r: table.get(f"{a}@{r}")
        old = sys.argv
        sys.argv = ["action_runtime_refresh.py", "--dir", REAL_WORKFLOWS, "--markdown"]
        out = io.StringIO()
        try:
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
                rc = mod.main()
        finally:
            sys.argv = old
        body = out.getvalue()
        self.assertEqual(rc, 0)
        self.assertNotIn("grafel-drift:yes", body)
        self.assertIn("No drift", body)

    def test_markdown_mode_renders_a_cut_short_run(self) -> None:
        """A transient must not silently produce a body that reads complete."""
        mod = self._refresh()
        table = self._real_table(mod, {"actions/checkout@v5": "node20"})

        def flaky(action, ref):
            if action.startswith("msys2/"):
                raise mod.Unavailable("HTTP 503")
            return table.get(f"{action}@{ref}")

        mod.runs_using = flaky
        old = sys.argv
        sys.argv = ["action_runtime_refresh.py", "--dir", REAL_WORKFLOWS, "--markdown"]
        out = io.StringIO()
        try:
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
                rc = mod.main()
        finally:
            sys.argv = old
        body = out.getvalue()
        self.assertEqual(rc, 0)
        self.assertIn("grafel-drift:yes", body)
        self.assertIn("CUT SHORT", body)

    def test_network_failure_is_exit_2_not_drift(self) -> None:
        """A rate-limited run and a rotted manifest must not look the same, or
        the first teaches everyone to ignore the second."""
        mod = self._refresh()

        def boom(action, ref):
            raise mod.Unavailable("rate limited")

        mod.runs_using = boom
        old = sys.argv
        sys.argv = ["action_runtime_refresh.py", "--dir", REAL_WORKFLOWS]
        try:
            self.assertEqual(mod.main(), 2)
        finally:
            sys.argv = old

    def test_an_empty_pin_set_is_exit_2_not_a_green_no_op(self) -> None:
        """Otherwise the job reports success having checked nothing."""
        mod = self._refresh()
        with tempfile.TemporaryDirectory() as empty:
            mod.runs_using = lambda a, r: "node24"
            old = sys.argv
            sys.argv = ["action_runtime_refresh.py", "--dir", empty]
            try:
                self.assertEqual(mod.main(), 2)
            finally:
                sys.argv = old

    def test_refresh_shares_the_gate_manifest(self) -> None:
        """One dict, two readers. A refresh job checking its own private copy
        would grade nothing."""
        mod = self._refresh()
        gate = sys.modules["action_runtime_gate"]
        self.assertIs(mod.ACTION_RUNTIMES, gate.ACTION_RUNTIMES)
        self.assertEqual(mod.MIN_NODE_MAJOR, 24)


if __name__ == "__main__":
    unittest.main()
