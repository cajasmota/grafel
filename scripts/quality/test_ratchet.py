#!/usr/bin/env python3
"""Self-tests for scripts/quality/ratchet.py (Refs #6564).

These exist because the property they pin — `measured_on` names a commit that
is reachable from the default branch, permanently — was written down in
baseline.json's own `measured_on_note` as a HUMAN INSTRUCTION ("re-pin this
field afterwards") and then skipped three times. Prose asserted a property the
writer did not enforce and no test observed.

They drive the writer, not the checked-in JSON: what is under test is what
`--update-baseline` stamps, in a synthetic repository whose shape (a feature
branch that has moved past the default branch) is the exact shape every
re-record is made on.

Run directly (`python3 scripts/quality/test_ratchet.py`) or through the Go
package that owns this gate — internal/quality drives them so they run in the
same suite as the baseline gate itself.
"""

import contextlib
import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest


def _load_ratchet():
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "ratchet.py")
    spec = importlib.util.spec_from_file_location("ratchet_under_test", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


ratchet = _load_ratchet()


def git(repo, *args):
    return subprocess.run(
        ["git", *args], cwd=repo, check=True,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    ).stdout.decode().strip()


def has_ref(repo, ref):
    """True if `ref` resolves in `repo`. Used to prove a fixture OMITS one."""
    return subprocess.call(
        ["git", "rev-parse", "--verify", "--quiet", ref],
        cwd=repo, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    ) == 0


@contextlib.contextmanager
def chdir(path):
    prior = os.getcwd()
    os.chdir(path)
    try:
        yield
    finally:
        os.chdir(prior)


def make_repo(root, with_origin=True):
    """A repo shaped like a re-record: default branch, then a feature branch
    that has moved past it. Returns (base_sha, feature_head_sha), short."""
    git(root, "init", "--quiet", "--initial-branch=main")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("1")
    git(root, "add", "a")
    git(root, "commit", "--quiet", "-m", "base")
    base = git(root, "rev-parse", "--short", "HEAD")
    if with_origin:
        # The remote-tracking ref is what a clone has; no network needed.
        git(root, "update-ref", "refs/remotes/origin/main", "HEAD")
        git(root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
    git(root, "checkout", "--quiet", "-b", "feature")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("2")
    git(root, "commit", "--quiet", "-am", "feature work")
    head = git(root, "rev-parse", "--short", "HEAD")
    assert base != head
    return base, head


def make_repo_stale_local_main(root):
    """A repo whose local `main` is BEHIND `refs/remotes/origin/main` — the
    ordinary state of any checkout that has not pulled recently.

    `make_repo` points local `main` and the remote-tracking ref at the same
    commit, so the preference order in DEFAULT_BRANCH_REFS is structurally
    unobservable there: both candidates resolve identically. Here they do not.

    Two things are deliberate and BOTH are load-bearing (Refs #6569):

    1. Local `main` is reset one commit back from `refs/remotes/origin/main`,
       so the two refs yield DIFFERENT merge-bases with the feature branch.
    2. `refs/remotes/origin/HEAD` is NOT set. `default_branch_ref()` consults
       that symref before it reaches DEFAULT_BRANCH_REFS at all, so with it
       present a remote ref wins regardless of how the tuple is ordered and
       the ordering stays unobservable for a second, independent reason.

    Returns (remote_tip_sha, stale_local_main_sha, feature_head_sha), short.
    The first is the correct answer: the merge-base of the feature branch with
    what the merge will actually land on.
    """
    git(root, "init", "--quiet", "--initial-branch=main")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("1")
    git(root, "add", "a")
    git(root, "commit", "--quiet", "-m", "old base")
    stale = git(root, "rev-parse", "--short", "HEAD")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("2")
    git(root, "commit", "--quiet", "-am", "landed on origin/main since the last pull")
    remote_tip = git(root, "rev-parse", "--short", "HEAD")
    # What a clone that has fetched but not merged looks like: the remote
    # -tracking ref is at the tip, the local branch is not. No origin/HEAD.
    git(root, "update-ref", "refs/remotes/origin/main", "HEAD")
    git(root, "checkout", "--quiet", "-b", "feature")
    git(root, "branch", "--quiet", "-f", "main", stale)
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("3")
    git(root, "commit", "--quiet", "-am", "feature work")
    head = git(root, "rev-parse", "--short", "HEAD")
    assert stale != remote_tip != head
    assert git(root, "rev-parse", "--short", "refs/heads/main") == stale
    return remote_tip, stale, head


STAMP = "test-stamp-6564"


def make_fixture(root, prior_extra=None):
    """The minimum on-disk shape `ratchet.py update` reads."""
    golden = os.path.join(root, "golden")
    reports = os.path.join(root, "reports")
    os.makedirs(os.path.join(golden, "demo-mini"))
    os.makedirs(reports)

    def write(path, obj):
        with open(path, "w") as fh:
            json.dump(obj, fh)

    write(os.path.join(golden, "demo-mini", "expected.json"), {
        "fixture_name": "demo-mini",
        "expected_entities": [{"name": "A", "kind": "SCOPE.Component", "must_exist": True}],
    })
    write(os.path.join(reports, "demo-mini.json"), {
        "fixture": "demo-mini", "run_stamp": STAMP,
        "entity_found": 4, "entity_expected": 10,
        "relationship_found": 0, "relationship_expected": 0,
        "forbidden_hits": 0,
    })
    baseline = os.path.join(root, "baseline.json")
    prior = {
        "version": 1,
        "regenerate": "scripts/quality/run.sh --runs 1 --update-baseline",
        "fixtures": {"demo-mini": {
            "entity_found": 4, "entity_expected": 10,
            "relationship_found": 0, "relationship_expected": 0}},
        "known_regressions": [],
    }
    prior.update(prior_extra or {})
    write(baseline, prior)
    return golden, reports, baseline


class MeasuredOnIsDurable(unittest.TestCase):
    """`measured_on` must be reachable from the default branch by construction.

    A feature-branch HEAD is not: squash — this repo's merge mode — orphans it
    the moment the PR lands, and the reachability gate then goes red on main,
    attributed to whoever opens the next unrelated PR.
    """

    def test_records_merge_base_not_branch_head(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, head = make_repo(root)
            got = ratchet.git_sha()
            self.assertNotEqual(
                got, head,
                "git_sha() stamped the feature-branch HEAD; squash-merge orphans it")
            self.assertEqual(
                got, base,
                "git_sha() must stamp the merge-base with the default branch")

    def test_recorded_sha_is_an_ancestor_of_the_default_branch(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            make_repo(root)
            sha = ratchet.git_sha()
            code = subprocess.call(
                ["git", "merge-base", "--is-ancestor", sha, "refs/remotes/origin/main"],
                cwd=root)
            self.assertEqual(code, 0, f"{sha!r} is not an ancestor of the default branch")

    def test_degrades_to_unknown_when_no_default_branch_is_available(self):
        """No remote, no main: a shallow or detached checkout cannot answer.

        The honest answer is "unknown" — never HEAD, which is the value that
        looks right and is not durable.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            git(root, "init", "--quiet", "--initial-branch=wip")
            git(root, "config", "user.email", "t@example.com")
            git(root, "config", "user.name", "t")
            git(root, "config", "commit.gpgsign", "false")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("1")
            git(root, "add", "a")
            git(root, "commit", "--quiet", "-m", "only")
            head = git(root, "rev-parse", "--short", "HEAD")
            got = ratchet.git_sha()
            self.assertNotEqual(got, head, "degraded to HEAD, which is the bug")
            self.assertEqual(got, "unknown")

    def test_falls_back_to_the_local_default_branch_without_a_remote(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, head = make_repo(root, with_origin=False)
            self.assertEqual(ratchet.git_sha(), base)

    def test_prefers_the_remote_tracking_ref_over_a_stale_local_main(self):
        """DEFAULT_BRANCH_REFS tries remote-tracking refs first, and says why:
        they are what the merge lands on, and a local `main` can be stale.

        Nothing observed that until #6569. This does: local `main` is a commit
        behind `refs/remotes/origin/main`, so preferring the local ref anchors
        `measured_on` to an older point than the branch will really merge onto
        — provenance that claims to derive from further back than it does.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            remote_tip, stale, head = make_repo_stale_local_main(root)
            # The premise the kill rests on: with origin/HEAD set, the symref
            # short-circuits default_branch_ref() before the tuple is read and
            # the ordering is unobservable no matter what the local ref is.
            self.assertFalse(
                has_ref(root, "refs/remotes/origin/HEAD"),
                "fixture set refs/remotes/origin/HEAD; the symref would decide "
                "this before DEFAULT_BRANCH_REFS is consulted, and the kill "
                "below would prove nothing about the ordering")

            self.assertEqual(
                ratchet.default_branch_ref(), "refs/remotes/origin/main",
                "resolved a local ref while a remote-tracking ref existed")
            got = ratchet.git_sha()
            self.assertNotEqual(
                got, stale,
                "git_sha() anchored measured_on to the stale local main")
            self.assertNotEqual(got, head, "stamped the feature-branch HEAD")
            self.assertEqual(
                got, remote_tip,
                "measured_on must be the merge-base with the remote-tracking "
                "ref — what the merge will actually land on")


    def test_origin_head_names_a_default_branch_the_tuple_does_not_list(self):
        """`refs/remotes/origin/HEAD` is consulted before DEFAULT_BRANCH_REFS,
        and that is the only thing that can answer a repo whose default branch
        is neither `main` nor `master`. Nothing observed it until #6569:
        deleting the symref lookup left the suite green, because every fixture
        also had an `origin/main` for the tuple to find.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            git(root, "init", "--quiet", "--initial-branch=release")
            git(root, "config", "user.email", "t@example.com")
            git(root, "config", "user.name", "t")
            git(root, "config", "commit.gpgsign", "false")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("1")
            git(root, "add", "a")
            git(root, "commit", "--quiet", "-m", "base")
            base = git(root, "rev-parse", "--short", "HEAD")
            git(root, "update-ref", "refs/remotes/origin/release", "HEAD")
            git(root, "symbolic-ref", "refs/remotes/origin/HEAD",
                "refs/remotes/origin/release")
            git(root, "checkout", "--quiet", "-b", "feature")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("2")
            git(root, "commit", "--quiet", "-am", "feature work")
            head = git(root, "rev-parse", "--short", "HEAD")

            # Nothing in DEFAULT_BRANCH_REFS can resolve here: the premise.
            for ref in ratchet.DEFAULT_BRANCH_REFS:
                self.assertFalse(
                    has_ref(root, ref),
                    f"{ref} resolves, so the tuple could answer without the symref")

            self.assertEqual(
                ratchet.default_branch_ref(), "refs/remotes/origin/release")
            got = ratchet.git_sha()
            self.assertNotEqual(got, "unknown", "failed to find the default branch")
            self.assertNotEqual(got, head, "stamped the feature-branch HEAD")
            self.assertEqual(got, base)


class WriteTimeRefusal(unittest.TestCase):
    """The killing mutant from #6564: revert the sha source to `rev-parse HEAD`,
    run the writer on a feature branch, squash-merge. Nothing at write time
    objected — a human instruction was the only guard, and it failed three
    times. The writer must refuse."""

    def test_build_refuses_a_measured_on_that_is_not_an_ancestor(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            _, head = make_repo(root)
            golden, reports, baseline = make_fixture(root)
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            prior_sha, ratchet.git_sha = ratchet.git_sha, lambda: head
            self.addCleanup(setattr, ratchet, "git_sha", prior_sha)
            with self.assertRaises(SystemExit) as ctx:
                ratchet.build(golden, reports, baseline)
            self.assertIn("ancestor", str(ctx.exception).lower())

    def test_build_accepts_the_merge_base(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, _ = make_repo(root)
            golden, reports, baseline = make_fixture(root)
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            doc = ratchet.build(golden, reports, baseline)
            self.assertEqual(doc["measured_on"], base)

    def test_build_carries_the_measured_on_note_across_a_rebuild(self):
        """The note is ~4KB of provenance a fixed-key-set rebuild once deleted.
        Pinned here as well as in Go, because this change touches build()."""
        note = "x" * 4096 + " base commit, not the commit this file lands in"
        with tempfile.TemporaryDirectory() as root, chdir(root):
            make_repo(root)
            golden, reports, baseline = make_fixture(
                root, {"measured_on_note": note})
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            doc = ratchet.build(golden, reports, baseline)
            self.assertEqual(doc.get("measured_on_note"), note)


if __name__ == "__main__":
    if subprocess.call(["git", "--version"],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) != 0:
        print("git not on PATH; ratchet self-tests skipped", file=sys.stderr)
        sys.exit(0)
    unittest.main(verbosity=2)
