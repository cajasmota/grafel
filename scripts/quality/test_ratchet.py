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
import io
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


def make_repo(root, with_origin=True, advance_default_branch=True):
    """A repo shaped like a re-record: a feature branch cut from the default
    branch, which then moved on without it.

    Returns (base_sha, feature_head_sha, default_tip_sha), short.

    THREE DISTINCT COMMITS BY CONSTRUCTION. `base` is the branch point,
    `tip` is where the default-branch ref now points, and they are deliberately
    not the same commit: a fixture that leaves the ref tip AT the branch point
    cannot tell `merge-base(HEAD, ref)` apart from `rev-parse(ref)`, and every
    test of git_sha() built on it is vacuous for the property it claims to pin
    (Refs #6607). `advance_default_branch=False` builds exactly that degenerate
    shape and exists only so a test can prove it is degenerate.
    """
    git(root, "init", "--quiet", "--initial-branch=main")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("1")
    git(root, "add", "a")
    git(root, "commit", "--quiet", "-m", "base")
    base = git(root, "rev-parse", "--short", "HEAD")
    # Cut the feature branch HERE — at `base` — before the default branch moves.
    git(root, "branch", "feature")
    if advance_default_branch:
        with open(os.path.join(root, "b"), "w") as fh:
            fh.write("someone else landed this")
        git(root, "add", "b")
        git(root, "commit", "--quiet", "-m", "default branch moves on")
    tip = git(root, "rev-parse", "--short", "HEAD")
    if with_origin:
        # The remote-tracking ref is what a clone has; no network needed.
        git(root, "update-ref", "refs/remotes/origin/main", "HEAD")
        git(root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
    git(root, "checkout", "--quiet", "feature")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("2")
    git(root, "commit", "--quiet", "-am", "feature work")
    head = git(root, "rev-parse", "--short", "HEAD")
    assert base != head
    assert tip != head
    return base, head, tip


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



def make_repo_shallow_disconnected(root):
    """A doubly-shallow CI checkout: `refs/remotes/origin/main` RESOLVES, but
    shares no *reachable* commit with HEAD (Refs #6613).

    This is the shape `default_branch_ref()`'s docstring names first — "a
    shallow clone (actions/checkout defaults to fetch-depth 1)" — built the
    way CI really produces it, with no network: clone the feature branch at
    depth 1, then fetch the default branch at depth 1 into its own
    remote-tracking ref. Both tips exist; the history that would join them was
    grafted away, so `merge-base HEAD origin/main` has no answer and
    `git_sha()` degrades to "unknown".

    It is deliberately NOT the "no default branch at all" shape that
    `test_degrades_to_unknown_when_no_default_branch_is_available` uses. That
    one degrades to "unknown" too, but `ensure_durable_measured_on()` then
    returns at its `ref is None` check and the `sha == "unknown"` guard above
    it is never the thing that carried the case — so the guard stays
    unobserved. Here a ref DOES resolve, so the guard is the only thing
    standing between a legitimately-degraded sha and a `merge-base
    --is-ancestor unknown <ref>` that fails and raises SystemExit.

    Returns the working checkout's path.
    """
    origin = os.path.join(root, "origin")
    work = os.path.join(root, "work")
    os.makedirs(origin)
    git(origin, "init", "--quiet", "--initial-branch=main")
    git(origin, "config", "user.email", "t@example.com")
    git(origin, "config", "user.name", "t")
    git(origin, "config", "commit.gpgsign", "false")
    with open(os.path.join(origin, "a"), "w") as fh:
        fh.write("1")
    git(origin, "add", "a")
    git(origin, "commit", "--quiet", "-m", "root")
    with open(os.path.join(origin, "a"), "w") as fh:
        fh.write("2")
    git(origin, "commit", "--quiet", "-am", "second, so depth 1 truncates something")
    git(origin, "checkout", "--quiet", "-b", "feature")
    with open(os.path.join(origin, "a"), "w") as fh:
        fh.write("3")
    git(origin, "commit", "--quiet", "-am", "feature work")
    git(origin, "checkout", "--quiet", "main")
    with open(os.path.join(origin, "a"), "w") as fh:
        fh.write("4")
    git(origin, "commit", "--quiet", "-am", "default branch moves on")

    git(root, "clone", "--quiet", "--depth", "1", "--branch", "feature",
        "file://" + origin, work)
    git(work, "config", "user.email", "t@example.com")
    git(work, "config", "user.name", "t")
    # The second shallow fetch: CI asks for the base branch it wants to diff
    # against and gets one commit of it, unconnected to the one it has.
    git(work, "fetch", "--quiet", "--depth", "1", "origin",
        "main:refs/remotes/origin/main")
    return work


def make_repo_annotated_tag_default_ref(root):
    """A checkout whose candidate default-branch ref is an ANNOTATED TAG.

    Returns (tag_ref, tagged_commit_sha, feature_head_sha), short.

    Exists to be a CONTROL, not a kill: see
    `AnnotatedTagCandidateIsNotADistinguisher`.
    """
    git(root, "init", "--quiet", "--initial-branch=main")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("1")
    git(root, "add", "a")
    git(root, "commit", "--quiet", "-m", "base")
    tagged = git(root, "rev-parse", "--short", "HEAD")
    git(root, "tag", "-a", "-m", "release", "v1")
    git(root, "checkout", "--quiet", "-b", "feature")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("2")
    git(root, "commit", "--quiet", "-am", "feature work")
    head = git(root, "rev-parse", "--short", "HEAD")
    return "refs/tags/v1", tagged, head


def make_repo_non_commit_candidate_ref(root):
    """A checkout where the FIRST candidate ref resolves but is not a commit,
    and a correct `origin/main` sits behind it (Refs #6613).

    `refs/tags/tree-only` is a tag on a TREE. `rev-parse --verify` accepts it;
    `rev-parse --verify <ref>^{commit}` does not. That difference is the whole
    of the `^{commit}` peel in `default_branch_ref()`, and it is the only shape
    that can observe it — an annotated tag resolves under both spellings (see
    `AnnotatedTagCandidateIsNotADistinguisher`).

    The candidate is fed in through `QUALITY_BASE_REF`, which is the only
    candidate slot that can hold an arbitrary ref: `DEFAULT_BRANCH_REFS` names
    branches, and `refs/remotes/origin/HEAD` is a symref to one. Tags on
    non-commit objects are real (git.git ships `junio-gpg-pub`, a tag on a
    blob), so pointing the override at one is a mistake a human can make.

    Returns (bad_ref, good_base_sha, feature_head_sha), short.
    """
    git(root, "init", "--quiet", "--initial-branch=main")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("1")
    git(root, "add", "a")
    git(root, "commit", "--quiet", "-m", "base")
    base = git(root, "rev-parse", "--short", "HEAD")
    git(root, "tag", "tree-only", git(root, "rev-parse", "HEAD^{tree}"))
    git(root, "update-ref", "refs/remotes/origin/main", "HEAD")
    with open(os.path.join(root, "b"), "w") as fh:
        fh.write("someone else landed this")
    git(root, "add", "b")
    git(root, "commit", "--quiet", "-m", "default branch moves on")
    git(root, "update-ref", "refs/remotes/origin/main", "HEAD")
    git(root, "checkout", "--quiet", "-b", "feature", base)
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("2")
    git(root, "commit", "--quiet", "-am", "feature work")
    head = git(root, "rev-parse", "--short", "HEAD")
    return "refs/tags/tree-only", base, head


def make_repo_criss_cross(root):
    """A repo where HEAD and the default-branch ref have TWO merge-bases.

    Criss-cross history is what two branches that each merged the other's
    earlier tip produce, and it is ordinary in any repo where a long-lived
    branch is kept up to date by merging rather than rebasing. `merge-base`
    picks one; `merge-base --all` prints both.

    Every other fixture here is linear, so `--all` and plain `merge-base` are
    indistinguishable in all of them (Refs #6613).

    Returns (feature_head_sha, [merge_base_shas]), short.
    """
    git(root, "init", "--quiet", "--initial-branch=main")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "r"), "w") as fh:
        fh.write("root")
    git(root, "add", "r")
    git(root, "commit", "--quiet", "-m", "root")
    git(root, "checkout", "--quiet", "-b", "left")
    with open(os.path.join(root, "l"), "w") as fh:
        fh.write("l")
    git(root, "add", "l")
    git(root, "commit", "--quiet", "-m", "left work")
    left_tip = git(root, "rev-parse", "--short", "HEAD")
    git(root, "checkout", "--quiet", "-b", "right", "main")
    with open(os.path.join(root, "rt"), "w") as fh:
        fh.write("r")
    git(root, "add", "rt")
    git(root, "commit", "--quiet", "-m", "right work")
    right_tip = git(root, "rev-parse", "--short", "HEAD")
    # The criss-cross: each side merges the other's tip. Neither merge commit
    # is an ancestor of the other, and both `left work` and `right work` are
    # ancestors of both — so both are merge-bases.
    git(root, "merge", "--quiet", "--no-edit", "-m", "right merges left", left_tip)
    git(root, "checkout", "--quiet", "left")
    git(root, "merge", "--quiet", "--no-edit", "-m", "left merges right", right_tip)
    # The default branch is one arm; the checkout under measurement is the other.
    git(root, "update-ref", "refs/remotes/origin/main", "refs/heads/right")
    git(root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
    git(root, "checkout", "--quiet", "left")
    head = git(root, "rev-parse", "--short", "HEAD")
    bases = [
        git(root, "rev-parse", "--short", sha)
        for sha in git(root, "merge-base", "--all", "HEAD",
                       "refs/remotes/origin/main").split()
    ]
    return head, bases


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
            base, head, _tip = make_repo(root)
            got = ratchet.git_sha()
            self.assertNotEqual(
                got, head,
                "git_sha() stamped the feature-branch HEAD; squash-merge orphans it")
            self.assertEqual(
                got, base,
                "git_sha() must stamp the merge-base with the default branch")

    def test_records_the_branch_point_not_the_default_branch_tip(self):
        """The distinction `test_records_merge_base_not_branch_head` does NOT
        make (Refs #6607).

        That test separates the merge-base from HEAD. This one separates it
        from the OTHER commit it could plausibly be: the tip of the default
        branch ref. `merge-base(HEAD, ref)` and `rev-parse(ref)` agree on every
        repo whose ref tip sits at the branch point, so only a fixture where
        the default branch has moved on can tell the two apart — and only that
        distinction makes `measured_on` mean "the point this was measured from"
        rather than "wherever main is now".
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, _head, tip = make_repo(root)
            # Premise, asserted rather than assumed: if a later fixture change
            # collapses these two commits, this test must fail here and say so,
            # not keep passing while observing nothing.
            self.assertNotEqual(
                base, tip,
                "DEGENERATE FIXTURE: the default-branch ref tip is the branch "
                "point, so merge-base(HEAD, ref) and rev-parse(ref) cannot be "
                "told apart and this test observes nothing. Advance the "
                "default branch after cutting `feature` (Refs #6607).")
            self.assertEqual(
                tip, git(root, "rev-parse", "--short", "refs/remotes/origin/main"),
                "premise broken: `tip` is not where the default branch ref points")

            got = ratchet.git_sha()

            self.assertNotEqual(
                got, tip,
                "git_sha() stamped the default-branch TIP; measured_on would "
                "then drift with main and claim a newer provenance point than "
                "the tree the numbers were measured on")
            self.assertEqual(
                got, base,
                "git_sha() must stamp the branch point — the merge-base of "
                "HEAD with the default branch")

    def test_the_degenerate_fixture_shape_cannot_tell_the_two_apart(self):
        """Control for the test above: prove the premise is load-bearing.

        With the default branch left at the branch point, the ref tip IS the
        merge-base — so a `rev-parse(ref)` implementation and the real one
        return the same commit. This is the shape every fixture had before
        #6607, and it is why the mutant survived.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, _head, tip = make_repo(root, advance_default_branch=False)
            self.assertEqual(
                base, tip,
                "expected the degenerate shape to put the ref tip at the branch point")
            self.assertEqual(ratchet.git_sha(), base)

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
            base, head, _tip = make_repo(root, with_origin=False)
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


    def test_origin_head_outranks_a_present_origin_main(self):
        """`refs/remotes/origin/HEAD` is consulted BEFORE DEFAULT_BRANCH_REFS,
        so it wins even when the tuple has an answer of its own. It is the only
        thing that can name a default branch that is neither `main` nor
        `master`, and it must not be demoted below a stale `origin/main` that
        happens to exist.

        Nothing observed either half until #6569: deleting the symref lookup
        left the suite green because every fixture also had an `origin/main`
        at the branch point, and moving the lookup *after* the tuple left it
        green because no fixture had both a symref and a competing
        `origin/main`. This one has both, at different commits.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            git(root, "init", "--quiet", "--initial-branch=release")
            git(root, "config", "user.email", "t@example.com")
            git(root, "config", "user.name", "t")
            git(root, "config", "commit.gpgsign", "false")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("1")
            git(root, "add", "a")
            git(root, "commit", "--quiet", "-m", "abandoned main")
            stale_main = git(root, "rev-parse", "--short", "HEAD")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("2")
            git(root, "commit", "--quiet", "-am", "base")
            base = git(root, "rev-parse", "--short", "HEAD")
            git(root, "update-ref", "refs/remotes/origin/release", "HEAD")
            git(root, "symbolic-ref", "refs/remotes/origin/HEAD",
                "refs/remotes/origin/release")
            # A competing tuple candidate, at a DIFFERENT commit. Without it
            # the symref's precedence is unobservable: demoting the lookup
            # below the tuple changes nothing when the tuple cannot answer.
            git(root, "update-ref", "refs/remotes/origin/main", stale_main)
            git(root, "checkout", "--quiet", "-b", "feature")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("3")
            git(root, "commit", "--quiet", "-am", "feature work")
            head = git(root, "rev-parse", "--short", "HEAD")

            # The premise, asserted rather than assumed: the tuple CAN answer
            # here, and its answer is the wrong one.
            self.assertTrue(
                has_ref(root, "refs/remotes/origin/main"),
                "no competing tuple candidate; the symref would win by default")
            self.assertNotEqual(stale_main, base)

            self.assertEqual(
                ratchet.default_branch_ref(), "refs/remotes/origin/release",
                "resolved a DEFAULT_BRANCH_REFS entry over refs/remotes/origin/HEAD")
            got = ratchet.git_sha()
            self.assertNotEqual(got, "unknown", "failed to find the default branch")
            self.assertNotEqual(got, head, "stamped the feature-branch HEAD")
            self.assertNotEqual(
                got, stale_main,
                "anchored measured_on to origin/main while origin/HEAD named "
                "a different default branch")
            self.assertEqual(got, base)


    def test_quality_base_ref_override_outranks_origin_head(self):
        """`QUALITY_BASE_REF` is first in the candidate list, above even the
        `origin/HEAD` symref, and the docstring says why: it is the escape
        hatch for "a checkout that knows its own base and cannot be discovered
        from refs". An override that automatic discovery can silently outvote
        is not an escape hatch.

        Nothing observed the top rung until now. #6569 pinned symref-over-tuple
        and tuple-over-local; moving the override below the symref left the
        suite green, because no fixture set the variable in a checkout that
        also had a resolvable `origin/HEAD` naming somewhere else.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            git(root, "init", "--quiet", "--initial-branch=release")
            git(root, "config", "user.email", "t@example.com")
            git(root, "config", "user.name", "t")
            git(root, "config", "commit.gpgsign", "false")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("1")
            git(root, "add", "a")
            git(root, "commit", "--quiet", "-m", "the base this checkout knows it has")
            override_base = git(root, "rev-parse", "--short", "HEAD")
            git(root, "update-ref", "refs/remotes/origin/stable", "HEAD")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("2")
            git(root, "commit", "--quiet", "-am", "what discovery would find instead")
            discovered = git(root, "rev-parse", "--short", "HEAD")
            git(root, "update-ref", "refs/remotes/origin/release", "HEAD")
            git(root, "symbolic-ref", "refs/remotes/origin/HEAD",
                "refs/remotes/origin/release")
            git(root, "checkout", "--quiet", "-b", "feature")
            with open(os.path.join(root, "a"), "w") as fh:
                fh.write("3")
            git(root, "commit", "--quiet", "-am", "feature work")
            head = git(root, "rev-parse", "--short", "HEAD")

            # The premise, asserted rather than assumed: discovery has a real
            # answer of its own here, and it is NOT the override's. Without
            # this the override would win merely by being the only candidate
            # that resolves, and the ordering would be unobservable again.
            self.assertTrue(
                has_ref(root, "refs/remotes/origin/HEAD"),
                "no competing symref; the override would win by default")
            self.assertTrue(
                has_ref(root, "refs/remotes/origin/stable"),
                "the override names a ref that does not resolve")
            self.assertNotEqual(override_base, discovered)

            os.environ["QUALITY_BASE_REF"] = "refs/remotes/origin/stable"
            self.addCleanup(os.environ.pop, "QUALITY_BASE_REF", None)

            self.assertEqual(
                ratchet.default_branch_ref(), "refs/remotes/origin/stable",
                "discovery outvoted an explicitly-set QUALITY_BASE_REF")
            got = ratchet.git_sha()
            self.assertNotEqual(got, "unknown", "failed to honour the override")
            self.assertNotEqual(got, head, "stamped the feature-branch HEAD")
            self.assertNotEqual(
                got, discovered,
                "anchored measured_on to what origin/HEAD found, silently "
                "ignoring QUALITY_BASE_REF")
            self.assertEqual(got, override_base)


class WriteTimeRefusal(unittest.TestCase):
    """The killing mutant from #6564: revert the sha source to `rev-parse HEAD`,
    run the writer on a feature branch, squash-merge. Nothing at write time
    objected — a human instruction was the only guard, and it failed three
    times. The writer must refuse."""

    def test_build_refuses_a_measured_on_that_is_not_an_ancestor(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            _, head, _tip = make_repo(root)
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
            base, _head, _tip = make_repo(root)
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



class DegradedShaIsAdvisoryNotFatal(unittest.TestCase):
    """`ensure_durable_measured_on("unknown")` must WARN and return.

    `git_sha()`'s docstring commits to "unknown" as the honest degradation, and
    #6569 pinned that `git_sha()` PRODUCES it. Nothing observed what the WRITER
    does when handed it. Removing the `sha == "unknown"` early return leaves
    every test green while turning an advisory warning into a hard SystemExit
    on exactly the checkouts the design set out to tolerate: `merge-base
    --is-ancestor unknown <ref>` fails, and the writer refuses to write
    anything at all (Refs #6613).
    """

    def test_writer_warns_and_records_unknown_on_a_shallow_disconnected_checkout(self):
        with tempfile.TemporaryDirectory() as root:
            work = make_repo_shallow_disconnected(root)
            with chdir(work):
                # Premise 1 — the degradation this test is about really
                # happened. Without it the writer never sees "unknown" and the
                # guard below is not the code under test.
                self.assertEqual(
                    ratchet.git_sha(), "unknown",
                    "DEGENERATE FIXTURE: git_sha() found a merge-base, so the "
                    "sha never degrades and the 'unknown' guard is never "
                    "reached. The checkout must be shallow on BOTH sides "
                    "(Refs #6613).")
                # Premise 2 — and, load-bearing, the guard is what carries the
                # case. `ensure_durable_measured_on` returns early a SECOND
                # time when `default_branch_ref()` is None; if that were the
                # state here, this test would pass with the guard deleted and
                # observe nothing.
                self.assertTrue(
                    has_ref(work, "refs/remotes/origin/main"),
                    "DEGENERATE FIXTURE: no default-branch ref resolves, so "
                    "ensure_durable_measured_on() returns at its `ref is None` "
                    "check and the 'unknown' guard is unobserved")
                self.assertIsNotNone(
                    ratchet.default_branch_ref(),
                    "DEGENERATE FIXTURE: default_branch_ref() is None, so the "
                    "second early return carries this case, not the guard "
                    "under test")
                # Premise 3 — what the guard is protecting against is real
                # here: the call it skips genuinely fails.
                self.assertNotEqual(
                    0,
                    subprocess.call(
                        ["git", "merge-base", "--is-ancestor", "unknown",
                         "refs/remotes/origin/main"],
                        cwd=work,
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL),
                    "premise broken: `merge-base --is-ancestor unknown <ref>` "
                    "succeeded, so skipping it would cost nothing and this "
                    "test could not tell the guard's presence from its absence")

                golden, reports, baseline = make_fixture(work)
                os.environ["QUALITY_RUN_STAMP"] = STAMP
                self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)

                err = io.StringIO()
                with contextlib.redirect_stderr(err):
                    try:
                        doc = ratchet.build(golden, reports, baseline)
                    except SystemExit as exc:
                        self.fail(
                            "the writer REFUSED a legitimately-degraded sha "
                            "instead of warning: a shallow checkout is a "
                            "supported state and 'unknown' is its documented "
                            f"answer, not a fatal error ({exc})")
                self.assertEqual(
                    doc["measured_on"], "unknown",
                    "the writer must record the honest 'unknown', never a sha "
                    "it cannot prove durable")
                self.assertIn(
                    "WARNING", err.getvalue(),
                    "the degradation was silent; it is advisory, so it must "
                    "still be said out loud")

    def test_ensure_durable_does_not_raise_on_unknown_directly(self):
        """The unit statement of the above, with no writer around it."""
        with tempfile.TemporaryDirectory() as root:
            work = make_repo_shallow_disconnected(root)
            with chdir(work):
                self.assertIsNotNone(
                    ratchet.default_branch_ref(),
                    "DEGENERATE FIXTURE: with no ref the second early return "
                    "carries this, not the guard under test")
                err = io.StringIO()
                with contextlib.redirect_stderr(err):
                    ratchet.ensure_durable_measured_on("unknown")
                self.assertIn("WARNING", err.getvalue())

    def test_the_no_ref_shape_cannot_observe_the_unknown_guard(self):
        """Control: prove premise 2 above is load-bearing.

        `test_degrades_to_unknown_when_no_default_branch_is_available` builds a
        checkout with no default-branch ref at all. It also degrades to
        "unknown" — but `ensure_durable_measured_on` then returns at `ref is
        None`, so the guard under test is dead code for that shape. Any kill
        built on it would be vacuous.
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
            self.assertEqual(ratchet.git_sha(), "unknown")
            self.assertIsNone(
                ratchet.default_branch_ref(),
                "expected the degenerate shape to have no default-branch ref")


class CandidateRefsMustPeelToACommit(unittest.TestCase):
    """`default_branch_ref()` accepts a candidate only if it peels to a commit.

    Every ref in every other fixture is already a commit, so `rev-parse --verify
    <ref>` and `rev-parse --verify <ref>^{commit}` agree everywhere and the peel
    does no work (Refs #6613).
    """

    def test_a_ref_that_resolves_but_is_not_a_commit_is_skipped(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            bad_ref, base, head = make_repo_non_commit_candidate_ref(root)

            # Premise 1 — the bad candidate DOES resolve. If it did not, a
            # peel-less check would reject it too and this test would pass
            # either way.
            self.assertTrue(
                has_ref(root, bad_ref),
                f"DEGENERATE FIXTURE: {bad_ref} does not resolve at all, so "
                f"`rev-parse --verify <ref>` would reject it without the peel "
                f"and the peel is unobserved")
            # Premise 2 — and it does NOT peel to a commit. That gap is the
            # entire behaviour under test.
            self.assertFalse(
                has_ref(root, bad_ref + "^{commit}"),
                f"DEGENERATE FIXTURE: {bad_ref} peels to a commit, so both "
                f"spellings accept it and this test observes nothing")
            # Premise 3 — a real answer exists behind it, so a correct
            # implementation has somewhere to fall through TO.
            self.assertTrue(has_ref(root, "refs/remotes/origin/main"))

            os.environ["QUALITY_BASE_REF"] = bad_ref
            self.addCleanup(os.environ.pop, "QUALITY_BASE_REF", None)

            self.assertEqual(
                ratchet.default_branch_ref(), "refs/remotes/origin/main",
                f"accepted {bad_ref}, which resolves but is not a commit; the "
                f"`^{{commit}}` peel is what rejects it")
            got = ratchet.git_sha()
            self.assertNotEqual(
                got, "unknown",
                "measured_on silently emptied: the candidate was accepted "
                "without peeling, and merge-base against a non-commit fails")
            self.assertNotEqual(got, head, "stamped the feature-branch HEAD")
            self.assertEqual(got, base)


class AnnotatedTagCandidateIsNotADistinguisher(unittest.TestCase):
    """Control, and a correction to a plausible-sounding fixture idea.

    An annotated tag looks like the obvious way to observe the `^{commit}`
    peel, and it is NOT one: `rev-parse --verify refs/tags/v1` and `rev-parse
    --verify refs/tags/v1^{commit}` both succeed, `default_branch_ref()`
    returns the ref STRING either way, and `merge-base` / `merge-base
    --is-ancestor` peel tags themselves. A kill built on an annotated tag would
    be a false DEAD. This test exists so the next person does not have to
    rediscover that (Refs #6613).
    """

    def test_both_spellings_accept_an_annotated_tag(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            tag_ref, tagged, head = make_repo_annotated_tag_default_ref(root)
            self.assertTrue(has_ref(root, tag_ref))
            self.assertTrue(
                has_ref(root, tag_ref + "^{commit}"),
                "an annotated tag peels to a commit; that is why it cannot "
                "tell the two spellings apart")
            self.assertNotEqual(
                git(root, "rev-parse", tag_ref),
                git(root, "rev-parse", tag_ref + "^{commit}"),
                "premise broken: this tag is lightweight, not annotated — the "
                "tag object and the commit must be different objects")

            os.environ["QUALITY_BASE_REF"] = tag_ref
            self.addCleanup(os.environ.pop, "QUALITY_BASE_REF", None)

            self.assertEqual(ratchet.default_branch_ref(), tag_ref)
            got = ratchet.git_sha()
            self.assertNotEqual(got, head)
            self.assertEqual(
                got, tagged,
                "merge-base peels the tag itself, so the resolved answer is "
                "the tagged commit whether or not default_branch_ref() peeled")


class MergeBasePicksOneCommit(unittest.TestCase):
    """`git_sha()` must stamp A merge-base, not the whole set.

    Criss-cross history has more than one. `merge-base --all` prints them all,
    `rev-parse --short` cannot parse the multi-line result, and `git_sha()`
    degrades to "unknown" — a provenance field quietly emptied on exactly the
    repositories with the most complex history. Every other fixture here is
    linear, so nothing observed it (Refs #6613).
    """

    def test_stamps_a_merge_base_when_history_has_several(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            head, bases = make_repo_criss_cross(root)

            # Premise, asserted rather than assumed: this history really does
            # have more than one merge-base. A later fixture edit that
            # linearises it must fail HERE, loudly, not keep passing while
            # observing nothing.
            self.assertGreater(
                len(bases), 1,
                f"DEGENERATE FIXTURE: HEAD and the default-branch ref have "
                f"{len(bases)} merge-base(s). With one, `merge-base` and "
                f"`merge-base --all` print the same single line and this test "
                f"cannot tell them apart (Refs #6613). Restore the criss-cross "
                f"merges.")
            self.assertTrue(
                has_ref(root, "refs/remotes/origin/main"),
                "premise broken: no default-branch ref to take a merge-base with")

            got = ratchet.git_sha()

            self.assertNotEqual(
                got, "unknown",
                "measured_on was silently emptied on a criss-cross history: "
                "more than one merge-base was collected and `rev-parse "
                "--short` could not parse the multi-line result")
            self.assertNotEqual(got, head, "stamped the feature-branch HEAD")
            self.assertIn(
                got, bases,
                f"stamped {got!r}, which is not one of this history's "
                f"merge-bases {bases}")

    def test_a_linear_history_cannot_observe_the_multi_base_case(self):
        """Control: prove the premise above is load-bearing.

        `make_repo`'s shape — every other fixture's shape — has exactly one
        merge-base, so `merge-base` and `merge-base --all` are the same call
        there. This is why the mutant survived.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, _head, _tip = make_repo(root)
            bases = git(root, "merge-base", "--all", "HEAD",
                        "refs/remotes/origin/main").split()
            self.assertEqual(
                len(bases), 1,
                "expected the linear shape to have exactly one merge-base")
            self.assertEqual(git(root, "rev-parse", "--short", bases[0]), base)


if __name__ == "__main__":
    if subprocess.call(["git", "--version"],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) != 0:
        print("git not on PATH; ratchet self-tests skipped", file=sys.stderr)
        sys.exit(0)
    unittest.main(verbosity=2)
