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
import statistics
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

    This is NOT the only shape that kills the `sha == "unknown"` mutant, and
    an earlier draft of this file wrongly said it was. The ref-less checkout in
    `test_degrades_to_unknown_when_no_default_branch_is_available` kills it
    too: `ensure_durable_measured_on()` returns at its `ref is None` check, so
    the WARNING is never printed and the assertion on it fires. MEASURED, not
    assumed — with every premise guard in the test below neutralised and this
    fixture degraded to the ref-less shape, pristine `ratchet.py` stays green
    and the mutant still fails, on `'WARNING' not found in ''`.

    What this shape buys is the STRONGER consequence, and the one #6613 is
    actually about. Because a ref resolves, the mutant reaches `merge-base
    --is-ancestor unknown <ref>`; that call fails and the writer raises
    SystemExit — a hard refusal to write any baseline at all, where the design
    is an advisory warning. The ref-less shape can only observe a missing
    warning, never the refusal.

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


def is_ancestor(repo, sha, ref):
    """True if `sha` is reachable from `ref`, asked of git directly.

    The tests below use this to state a fixture's ancestry facts independently
    of `ratchet.py` — the whole point of #6627's first survivor is that the
    code can consult the WRONG ref and still produce a plausible answer, so the
    premises cannot be read back out of the function under test.
    """
    return subprocess.call(
        ["git", "merge-base", "--is-ancestor", sha, ref],
        cwd=repo, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    ) == 0


def set_base_ref(case, value):
    """Set (or, with `value=None`, unset) `QUALITY_BASE_REF` for one test and
    RESTORE whatever was there before, ambient value included.

    `os.environ.pop("QUALITY_BASE_REF", None)` with no restore drops an ambient
    value for the remainder of the process, which is a cross-test coupling: the
    next test to read it sees a different environment depending on whether this
    one ran first. Nothing in this file depends on that today — which is
    exactly why it would surface later, as an order-dependent failure in a test
    that has nothing to do with the one that broke it.
    """
    prior = os.environ.get("QUALITY_BASE_REF")

    def restore():
        if prior is None:
            os.environ.pop("QUALITY_BASE_REF", None)
        else:
            os.environ["QUALITY_BASE_REF"] = prior

    case.addCleanup(restore)
    if value is None:
        os.environ.pop("QUALITY_BASE_REF", None)
    else:
        os.environ["QUALITY_BASE_REF"] = value


def make_repo_divergent_default_ref(root):
    """A checkout whose default-branch ref is NOT `origin/main`, and where the
    two disagree about what is reachable (Refs #6627).

    Returns (branch_point, orphan_sha), short.

    Every other fixture in this file makes `refs/remotes/origin/main` the
    answer `default_branch_ref()` gives, so `ensure_durable_measured_on` could
    ignore that answer and hardcode `origin/main` with nothing to observe it.
    Here the discovered ref is `refs/remotes/origin/release`, fed in through
    `QUALITY_BASE_REF` — which exists precisely for a checkout that knows its
    own base and cannot be discovered from refs — and `origin/main` is present
    but sits on an ORPHAN history:

        r1 ── r2            <- refs/remotes/origin/release   (the real base)
         └── f1             <- feature, HEAD
        o1                  <- refs/remotes/origin/main      (disjoint)

    So the two refs disagree in BOTH directions, which is what makes the
    ancestry decision observable rather than merely reported:

      * `r1` (the branch point, and what `git_sha()` stamps) is reachable from
        the discovered ref and NOT from `origin/main`;
      * `o1` is reachable from `origin/main` and NOT from the discovered ref.

    `origin/main` deliberately RESOLVES, and that clause was MEASURED rather
    than assumed (Refs #6627). Deleting the ref from this fixture and
    re-applying the mutant: the two permissive tests still fail, but only
    because `merge-base --is-ancestor` cannot resolve a missing ref — an
    argument about a broken checkout, not about which ref the decision follows.
    The strict test, `test_refuses_a_sha_only_the_hardcoded_ref_can_reach`,
    goes GREEN under the mutant without it, because with no `origin/main` the
    mutant refuses `o1` for the same reason the real code does. So the clause
    is load-bearing for exactly one of the three tests, and is kept for all
    three because it is what makes the kill an argument about ref choice.
    """
    git(root, "init", "--quiet", "--initial-branch=trunk")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("1")
    git(root, "add", "a")
    git(root, "commit", "--quiet", "-m", "base")
    branch_point = git(root, "rev-parse", "--short", "HEAD")
    git(root, "branch", "feature")
    with open(os.path.join(root, "b"), "w") as fh:
        fh.write("someone else landed this")
    git(root, "add", "b")
    git(root, "commit", "--quiet", "-m", "release branch moves on")
    git(root, "update-ref", "refs/remotes/origin/release", "HEAD")

    # An unrelated root commit, so `origin/main` can resolve while sharing no
    # history at all with the branch the base ref names.
    git(root, "checkout", "--quiet", "--orphan", "unrelated")
    with open(os.path.join(root, "c"), "w") as fh:
        fh.write("a different project's main")
    git(root, "add", "c")
    git(root, "commit", "--quiet", "-m", "unrelated root")
    orphan = git(root, "rev-parse", "--short", "HEAD")
    git(root, "update-ref", "refs/remotes/origin/main", "HEAD")

    git(root, "checkout", "--quiet", "--force", "feature")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("2")
    git(root, "commit", "--quiet", "-am", "feature work")
    assert branch_point != orphan
    return branch_point, orphan


def make_repo_no_default_branch_ref(root):
    """A local repo with ONE REAL COMMIT and no candidate default-branch ref
    whatsoever (Refs #6627).

    Returns the short HEAD sha.

    The branch is `wip`, there is no remote, no `origin/HEAD`, and no local
    `main` or `master`, so every entry in `DEFAULT_BRANCH_REFS` misses and
    `default_branch_ref()` returns None. Unlike
    `test_degrades_to_unknown_when_no_default_branch_is_available`, the caller
    here holds a sha that RESOLVES — which is the combination that reaches
    `ensure_durable_measured_on`'s second early return.
    """
    git(root, "init", "--quiet", "--initial-branch=wip")
    git(root, "config", "user.email", "t@example.com")
    git(root, "config", "user.name", "t")
    git(root, "config", "commit.gpgsign", "false")
    with open(os.path.join(root, "a"), "w") as fh:
        fh.write("1")
    git(root, "add", "a")
    git(root, "commit", "--quiet", "-m", "only")
    return git(root, "rev-parse", "--short", "HEAD")


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
        # #6488 arm D. Every report `grafel quality` writes carries these, and
        # both `build` and `check` now read them, so the shared fixture carries
        # them too: a fixture missing them would exercise the refusal paths in
        # every unrelated test instead of the happy path those tests are about.
        "entity_extracted_total": 12, "relationship_extracted_total": 7,
    })
    baseline = os.path.join(root, "baseline.json")
    prior = {
        "version": 1,
        "regenerate": "scripts/quality/run.sh --runs 1 --update-baseline",
        "fixtures": {"demo-mini": {
            "entity_found": 4, "entity_expected": 10,
            "relationship_found": 0, "relationship_expected": 0,
            "entity_extracted_total": 12, "relationship_extracted_total": 7}},
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

            set_base_ref(self, "refs/remotes/origin/stable")

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
                # The three checks below pin the fixture's SHAPE. None of
                # them is what makes the kill — measured: neutralise all three,
                # degrade the fixture to the ref-less shape, and pristine stays
                # green while the mutant still fails. What they do is keep this
                # test observing the consequence it claims to (the SystemExit,
                # not merely a missing warning), and make a later fixture edit
                # fail HERE by name instead of somewhere further down.
                #
                # Shape 1 — the sha really degrades. Without this the writer
                # never sees "unknown" at all.
                self.assertEqual(
                    ratchet.git_sha(), "unknown",
                    "DEGENERATE FIXTURE: git_sha() found a merge-base, so the "
                    "sha never degrades and the 'unknown' guard is never "
                    "reached. The checkout must be shallow on BOTH sides "
                    "(Refs #6613).")
                # Shape 2 — a ref resolves. `ensure_durable_measured_on`
                # returns early a SECOND time when `default_branch_ref()` is
                # None; in that state the mutant is caught by the missing
                # WARNING rather than by the SystemExit, and this fixture stops
                # being distinguishable from the ref-less one.
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
                # Shape 3 — what the guard protects against is real here:
                # the call it skips genuinely fails.
                self.assertNotEqual(
                    0,
                    subprocess.call(
                        ["git", "merge-base", "--is-ancestor", "unknown",
                         "refs/remotes/origin/main"],
                        cwd=work,
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL),
                    "premise broken: `merge-base --is-ancestor unknown <ref>` "
                    "succeeded, so skipping it would cost nothing and this test "
                    "would no longer be observing the SystemExit it exists for")

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

    def test_the_no_ref_shape_observes_only_the_weaker_consequence(self):
        """Companion to the fixture above — and a correction (Refs #6613).

        `test_degrades_to_unknown_when_no_default_branch_is_available` builds a
        checkout with no default-branch ref at all. It degrades to "unknown"
        too, and it DOES kill the `sha == "unknown"` mutant: with the guard
        deleted, `ensure_durable_measured_on` falls through to its `ref is
        None` return, the WARNING is never printed, and an assertion on that
        warning fires.

        An earlier draft of this file claimed the reverse — that the guard was
        "dead code for that shape" and any kill built on it "would be vacuous".
        That was prose asserting what no test observed, and measuring it showed
        it false. It is recorded here rather than quietly deleted because this
        file exists to fight exactly that habit.

        What the ref-less shape genuinely cannot observe is the SystemExit: it
        never reaches the `merge-base --is-ancestor unknown <ref>` call. That
        is the user-visible half of the defect, and it is why the
        shallow-disconnected fixture is worth its cost — not because it is the
        only shape that can kill the mutant.
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

            set_base_ref(self, bad_ref)

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

            set_base_ref(self, tag_ref)

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


class AncestryFollowsTheDiscoveredRef(unittest.TestCase):
    """`ensure_durable_measured_on` must check durability against the ref
    `default_branch_ref()` returned — not against a hardcoded `origin/main`.

    Hardcoding it survived the whole suite until #6627, because every fixture's
    default branch ref WAS `origin/main`. `QUALITY_BASE_REF` exists for the
    opposite case: a checkout that cannot be discovered from refs and names its
    own base. Under the mutant an operator sets it, watches `git_sha()` honour
    it, and then has durability judged against a ref that may not exist in that
    checkout at all — `merge-base --is-ancestor` against a missing or unrelated
    ref exits non-zero, so a perfectly durable sha is refused, with an error
    message naming a ref the operator never chose.

    That last clause is why these tests assert the DECISION and never the
    message: the message already interpolates `ref`, so an assertion on its
    text goes green on a call that consulted the wrong ref and merely reported
    the right one. That is not a theoretical worry: under the mutant this
    suite's failure output reads "not an ancestor of
    refs/remotes/origin/release" on a call that asked about `origin/main`.

    Every premise below was drop-tested (Refs #6627): with the "unknown" check
    neutralised and `branch_point` replaced by `"unknown"`, the mutant goes
    green again — the first early return carries the call and the ancestry
    check is never reached. The `err.getvalue() == ""` assertion is what
    catches that degradation, and it fires on pristine `ratchet.py`.
    """

    BASE_REF = "refs/remotes/origin/release"
    HARDCODED = "refs/remotes/origin/main"

    def _fixture(self, root):
        """Build the repo, arm the override, and assert every premise the two
        tests below rely on — independently of `ratchet.py` where it matters."""
        branch_point, orphan = make_repo_divergent_default_ref(root)
        set_base_ref(self, self.BASE_REF)

        self.assertEqual(
            ratchet.default_branch_ref(), self.BASE_REF,
            "DEGENERATE FIXTURE: the discovered ref is not the override, so "
            "this fixture cannot tell the discovered ref from any other")
        self.assertTrue(
            has_ref(root, self.HARDCODED),
            "DEGENERATE FIXTURE: the hardcoded ref does not resolve here, so a "
            "kill would only prove that a missing ref fails, not that the "
            "decision follows the discovered one")
        # The two refs must disagree in both directions, or one of the two
        # tests below is vacuous.
        self.assertTrue(is_ancestor(root, branch_point, self.BASE_REF))
        self.assertFalse(is_ancestor(root, branch_point, self.HARDCODED))
        self.assertTrue(is_ancestor(root, orphan, self.HARDCODED))
        self.assertFalse(is_ancestor(root, orphan, self.BASE_REF))
        # Neither sha may be "unknown", or the FIRST early return carries the
        # call and the ancestry check is never reached.
        self.assertNotIn("unknown", (branch_point, orphan))
        return branch_point, orphan

    def test_accepts_a_sha_only_the_discovered_ref_can_reach(self):
        """Permissive direction: the durable sha must be written, not refused."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            branch_point, _orphan = self._fixture(root)
            err = io.StringIO()
            try:
                with contextlib.redirect_stderr(err):
                    ratchet.ensure_durable_measured_on(branch_point)
            except SystemExit as exc:
                self.fail(
                    f"the writer refused {branch_point!r}, which IS reachable "
                    f"from the discovered base ref {self.BASE_REF} — the "
                    f"ancestry check consulted some other ref ({exc})")
            self.assertEqual(
                err.getvalue(), "",
                "the 'unknown' guard fired; the ancestry check was never "
                "reached and this test observed nothing")

    def test_refuses_a_sha_only_the_hardcoded_ref_can_reach(self):
        """Strict direction: reachability from `origin/main` must not launder a
        sha the real base ref cannot reach."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            _branch_point, orphan = self._fixture(root)
            with self.assertRaises(
                SystemExit,
                msg=(f"the writer accepted {orphan!r}, which is reachable only "
                     f"from {self.HARDCODED} and NOT from the discovered base "
                     f"ref {self.BASE_REF}: durability was judged against the "
                     f"wrong ref"),
            ):
                ratchet.ensure_durable_measured_on(orphan)

    def test_the_writer_completes_on_a_checkout_whose_base_is_not_origin_main(self):
        """The operator-visible half: `--update-baseline` must produce a
        baseline here, not a refusal to write anything at all."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            branch_point, _orphan = self._fixture(root)
            golden, reports, baseline = make_fixture(root)
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            self.assertEqual(
                ratchet.git_sha(), branch_point,
                "premise broken: the writer is not about to stamp the branch "
                "point, so what follows does not exercise the durability check")
            try:
                doc = ratchet.build(golden, reports, baseline)
            except SystemExit as exc:
                self.fail(
                    f"--update-baseline refused to write on a checkout whose "
                    f"base ref is {self.BASE_REF} ({exc})")
            self.assertEqual(doc["measured_on"], branch_point)


class PaddedBaseRefOverrideIsHonoured(unittest.TestCase):
    """`QUALITY_BASE_REF` must survive the whitespace its own sources add
    (Refs #6633).

    `default_branch_ref()` strips the override, and until this class nothing
    observed it: dropping the `.strip()` passed all 24 tests. The property was
    asserted only in the docstring ("QUALITY_BASE_REF overrides the search"),
    which is the dominant defect shape in this file.

    None of this whitespace is hypothetical, and that is why the padding uses
    four different characters. A trailing NEWLINE is what both ordinary sources
    of this value append: `QUALITY_BASE_REF=$(git symbolic-ref …)` in a shell,
    and a YAML block scalar in a workflow. A leading SPACE is what a
    hand-edited `env:` line or a `read`-into-variable produces. A leading TAB
    is what hand-indenting that same line produces. A trailing CR is what a
    workflow file checked out with CRLF endings produces on any runner.

    Witness, with `QUALITY_BASE_REF="\\t refs/remotes/origin/release\\r\\n"` on
    the `make_repo_divergent_default_ref` fixture:

        | .strip() | default_branch_ref()          | git_sha()   | durable? |
        | present  | refs/remotes/origin/release   | branch pt   | accepted |
        | absent   | refs/remotes/origin/main      | "unknown"   | REFUSED  |

    Every consequence in that second row is wrong and none of them mentions
    whitespace: the override is discarded, the search falls through to the
    `origin/main` that is disjoint from this checkout's history, provenance
    degrades to "unknown", and a sha that IS durable against the operator's
    real base is refused — naming a ref they never chose.

    These tests assert the RESOLVED REF, the STAMPED SHA and the ancestry
    DECISION, never the message text. #6630 established why: the `SystemExit`
    f-string interpolates `ref`, so a message assertion reads back the ref the
    code chose and can go green on a call that consulted a different one.

    Per-clause drop analysis (measured, Refs #6633). FOUR mutant families were
    scored, not one: `.strip()` deleted, `.strip()` -> `.rstrip()`, `.strip()`
    -> `.lstrip()`, and `.strip()` -> `.strip(" \\n")`. All four survive the
    suite without this class and all four die with it.

      * The LEADING whitespace in `PADDED` is load-bearing: pad only on the
        right and the `.rstrip()` mutant goes green, because `rstrip()` removes
        exactly the padding that shape supplies.
      * The TRAILING whitespace is load-bearing for the mirror reason: pad only
        on the left and the `.lstrip()` mutant goes green.
      * The TAB and the CR are load-bearing together: pad with space and
        newline alone and `.strip(" \\n")` goes green, because that argument
        names exactly the two characters such a padding contains. `.strip()`
        with no argument removes a SET, so a padding drawn from part of that
        set cannot pin the set.
      * `origin/main` resolving in the fixture is load-bearing for
        `test_the_durability_decision_follows_the_padded_override`: delete it
        and the discarded override leaves NO ref at all, so
        `ensure_durable_measured_on` takes its second early return and accepts
        the sha quietly — the mutant survives that test. The other two tests
        kill it either way, since `default_branch_ref()` returns None and
        `git_sha()` still degrades to "unknown".
      * `test_the_durability_decision_follows_the_padded_override` is
        DECORATIVE for this mutant family, and is labelled so rather than
        credited with weight it does not carry: delete it and every strip
        mutant still dies, on the two remaining tests. It is kept for two
        reasons that are not "it kills something here" — it is the consequence
        an operator actually sees (a refusal naming a ref they never chose),
        and it is the SUITE-WIDE UNIQUE killer of a different mutant: one that
        consumes the override unstripped downstream of `default_branch_ref()`.
        Decorative for this family and load-bearing for that one are both true,
        and the distinction is the point.

    Measured drop matrix (3 selected tests; "survives" = mutant goes green):

        drop                      | no strip | rstrip() | lstrip() | strip(" \\n")
        (none)                    |  3 fail  |  3 fail  |  3 fail  |  3 fail
        pad right only            |  3 fail  | SURVIVES |  3 fail  |  3 fail
        pad left only             |  3 fail  |  3 fail  | SURVIVES |  3 fail
        pad space + newline only  |  3 fail  |  3 fail  |  3 fail  | SURVIVES
        origin/main deleted       |  2 fail  |  2 fail  |  2 fail  |  2 fail
                                  |   (the durability test passes in this column-wide row)
        durability test deleted   |  2 fail  |  2 fail  |  2 fail  |  2 fail

    Recorded EQUIVALENCES — measured, and recorded as equivalent rather than
    as kills, because an equivalent mutant that is filed as DEAD is a false
    claim about what the suite observes:

      * Moving the strip past the emptiness test — `if override:` on the raw
        value, `candidates.append(override.strip())` — is EQUIVALENT. The only
        input that could diverge is a whitespace-ONLY value: there the mutant
        appends `""`, `rev-parse --verify "^{commit}"` fails, and the search
        falls through to exactly the ref the pristine code reaches by not
        appending at all. Re-measured here over 12 inputs — five of them
        whitespace-only, which is the only shape that could diverge — with 0
        divergences. Do not write a test for it, and do not file it as DEAD.
    """

    BASE_REF = "refs/remotes/origin/release"
    FALLBACK = "refs/remotes/origin/main"
    # Padded on BOTH sides on purpose: one side alone cannot distinguish
    # `.strip()` from the one-sided strip that happens to remove it. And with
    # FOUR distinct whitespace characters, because `.strip()` with no argument
    # removes a set, and a padding of only space and newline leaves
    # `.strip(" \\n")` — a plausible "be explicit about what we strip" edit —
    # indistinguishable from it. Tab and CR both have real sources: a
    # hand-indented `env:` line and a CRLF-checked-out workflow file.
    PADDED = "\t refs/remotes/origin/release\r\n"

    def _fixture(self, root):
        """Build the repo, arm the padded override, and assert every premise
        directly of git — never by reading it back out of `ratchet.py`."""
        branch_point, orphan = make_repo_divergent_default_ref(root)
        set_base_ref(self, self.PADDED)

        # Premise 1 — the STRIPPED spelling really does resolve here. Without
        # this the tests below could pass on a ref that never worked.
        self.assertTrue(
            has_ref(root, self.BASE_REF + "^{commit}"),
            f"DEGENERATE FIXTURE: {self.BASE_REF} does not resolve, so nothing "
            f"below distinguishes a stripped override from a broken one")

        # Premise 2 — and every UNSTRIPPED spelling genuinely does not, one per
        # mutant family. If any of these resolved, that mutant would be
        # equivalent on this fixture and its death would prove nothing.
        for label, spelling in (
            ("no strip at all", self.PADDED),
            ("rstrip() only, leading space survives", self.PADDED.rstrip()),
            ("lstrip() only, trailing newline survives", self.PADDED.lstrip()),
            ('strip(" \\n") only, tab and CR survive', self.PADDED.strip(" \n")),
        ):
            self.assertFalse(
                has_ref(root, spelling + "^{commit}"),
                f"DEGENERATE FIXTURE: {spelling!r} ({label}) resolves, so this "
                f"padding cannot observe the strip that removes it")

        # Premise 3 — the ref the search falls through to is present and is a
        # DIFFERENT one. Without it the mutant produces None rather than a
        # wrong answer, which is a weaker and differently-caused failure.
        self.assertTrue(
            has_ref(root, self.FALLBACK),
            f"DEGENERATE FIXTURE: {self.FALLBACK} does not resolve, so a "
            f"discarded override yields no ref rather than the wrong ref")
        self.assertNotEqual(self.BASE_REF, self.FALLBACK)

        # Premise 4 — the two refs disagree about the branch point, so the
        # durability decision is observable and not merely reported.
        self.assertTrue(is_ancestor(root, branch_point, self.BASE_REF))
        self.assertFalse(is_ancestor(root, branch_point, self.FALLBACK))
        self.assertNotEqual(branch_point, orphan)
        return branch_point

    def test_the_padded_override_is_the_ref_that_is_chosen(self):
        """The resolved ref itself — the value every consequence hangs off."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            self._fixture(root)
            self.assertEqual(
                ratchet.default_branch_ref(), self.BASE_REF,
                f"QUALITY_BASE_REF={self.PADDED!r} was discarded: the search "
                f"fell through to a ref the operator never chose. The override "
                f"exists for a checkout that cannot be discovered from refs, "
                f"and an override that silently does nothing is worse than "
                f"none, because nothing signals that it was ignored")

    def test_the_padded_override_still_stamps_a_real_provenance_sha(self):
        """The writer's own output: a sha, not the "unknown" degradation."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            branch_point = self._fixture(root)
            self.assertEqual(
                ratchet.git_sha(), branch_point,
                f"measured_on degraded away from the branch point with a "
                f"perfectly good base ref set: the padded override was "
                f"discarded and merge-base was taken against {self.FALLBACK}, "
                f"which shares no history with this checkout")

    def test_the_durability_decision_follows_the_padded_override(self):
        """The operator-visible consequence: the durable sha must be written,
        not refused against a ref they never named."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            branch_point = self._fixture(root)
            err = io.StringIO()
            try:
                with contextlib.redirect_stderr(err):
                    ratchet.ensure_durable_measured_on(branch_point)
            except SystemExit as exc:
                self.fail(
                    f"the writer refused {branch_point!r}, which IS reachable "
                    f"from the padded override {self.PADDED!r} once stripped — "
                    f"durability was judged against some other ref ({exc})")
            self.assertEqual(
                err.getvalue(), "",
                "the 'unknown' guard fired, so the ancestry check was never "
                "reached and this test observed nothing")


class SecondEarlyReturnIsReached(unittest.TestCase):
    """`ensure_durable_measured_on` must return quietly when handed a REAL sha
    in a checkout with no default-branch ref (Refs #6627).

    No fixture reached that guard: the shapes with no ref all degrade `sha` to
    "unknown", so the FIRST early return carries them, and every shape with a
    real sha has a ref. Deleting the guard therefore changed nothing any test
    could see — while, if it ever fired, `merge-base --is-ancestor <sha> None`
    raises TypeError inside subprocess: a traceback out of the writer where the
    design is a quiet degradation.

    This is the mirror of the trap #6613 recorded from the other side, where
    the SECOND guard carried the obvious no-ref fixture and left the FIRST one
    dead. One fixture cannot pin both; this is the one that pins this one.

    Both premises are load-bearing, measured rather than assumed (Refs #6627).
    Give this repo a local `main` and the mutant goes green — a ref is found
    and the ancestry check runs normally. Hand the call `"unknown"` instead of
    a real sha and the mutant goes green — the first early return carries it.
    """

    def test_returns_quietly_for_a_real_sha_with_no_ref_to_check_against(self):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            sha = make_repo_no_default_branch_ref(root)
            set_base_ref(self, None)

            # Premise 1 — the sha resolves. Asked of git, not of ratchet.py.
            self.assertTrue(
                has_ref(root, sha + "^{commit}"),
                f"DEGENERATE FIXTURE: {sha!r} does not resolve to a commit")
            # Premise 2 — the ref hunt really comes up empty, and every
            # candidate slot is accounted for.
            for candidate in ("refs/remotes/origin/HEAD",) + ratchet.DEFAULT_BRANCH_REFS:
                self.assertFalse(
                    has_ref(root, candidate),
                    f"DEGENERATE FIXTURE: {candidate} resolves here, so the "
                    f"guard under test is not the one that returns")
            self.assertIsNone(
                ratchet.default_branch_ref(),
                "DEGENERATE FIXTURE: a default branch ref was found, so the "
                "call falls through to a real ancestry check")
            # Premise 3 — the FIRST guard is not the one returning.
            self.assertNotEqual(sha, "unknown")

            err = io.StringIO()
            try:
                with contextlib.redirect_stderr(err):
                    ratchet.ensure_durable_measured_on(sha)
            except SystemExit as exc:
                self.fail(
                    f"the writer refused {sha!r} instead of degrading quietly: "
                    f"a checkout with no default-branch ref cannot prove "
                    f"durability either way ({exc})")
            except TypeError as exc:
                self.fail(
                    f"the missing-ref guard was gone, so the ancestry check ran "
                    f"with ref=None and blew up inside subprocess ({exc})")
            self.assertEqual(
                err.getvalue(), "",
                "the 'unknown' guard fired, so the guard under test was never "
                "reached; this test observed nothing")

    def test_the_writer_cannot_reach_this_guard_on_its_own(self):
        """Control, and the reason the test above is a direct unit call.

        In this checkout `git_sha()` degrades to "unknown", so `build()` can
        only ever hand `ensure_durable_measured_on` the value the FIRST guard
        catches. There is no writer-level route to the second guard, which is
        exactly why nothing observed it.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            make_repo_no_default_branch_ref(root)
            set_base_ref(self, None)
            self.assertIsNone(ratchet.default_branch_ref())
            self.assertEqual(
                ratchet.git_sha(), "unknown",
                "if the writer can produce a real sha here, drive this guard "
                "through build() instead of calling it directly")


class AnOverrideCannotVouchForItself(unittest.TestCase):
    """`ensure_durable_measured_on` must not let `QUALITY_BASE_REF` be its own
    authority (Refs #6564, #6568, #6570).

    `git_sha()` computes the stamped sha FROM `default_branch_ref()`, and until
    #6570 the durability guard re-derived that same ref and asked whether the
    sha was reachable from it. With an override set those are the same ref by
    construction, so the question answers itself: point `QUALITY_BASE_REF` at a
    local feature branch and the writer stamps that branch's HEAD, the guard
    agrees, and the recorded value is orphaned by the squash that lands the PR
    — #6564 exactly, re-entered through the one door the guard did not cover.

    The fix asks the ref the CHECKOUT discovered for itself as a second opinion,
    which is why these tests are structured around three separate cases rather
    than one:

      * an override the discovered branch CANNOT reach must now be refused —
        the permissive direction, and the red control for #6570;
      * an override the discovered branch CAN reach must still be honoured, or
        the fix has merely made the override useless;
      * a discovered ref that shares NO history with the override must NOT get
        a veto. That clause is not a convenience: `make_repo_divergent_default_
        ref` builds an `origin/main` on an orphan root — "a different project's
        main" — and `AncestryFollowsTheDiscoveredRef` / `PaddedBaseRefOverride
        IsHonoured` both require a durable sha to be ACCEPTED there. A
        cross-check without the disjointness gate refuses it, which is the
        regression #6627 and #6633 pinned. Neither of those classes was
        modified for #6570.

    These tests assert the DECISION and the ARTEFACT — a `SystemExit` or a
    written `measured_on` — never a counter and never the message text, for the
    reason #6630 established: the message interpolates the ref it chose, so a
    text assertion reads back the code's own claim about which ref it consulted.
    """

    def test_refuses_a_sha_the_discovered_default_branch_cannot_reach(self):
        """PERMISSIVE DIRECTION — the red control for #6570.

        Before the fix this goes green by ACCEPTING the feature HEAD.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, head, tip = make_repo(root)
            set_base_ref(self, "refs/heads/feature")

            # Premises, asked of git directly rather than of ratchet.py.
            self.assertTrue(has_ref(root, "refs/heads/feature"))
            self.assertTrue(has_ref(root, "refs/remotes/origin/main"))
            self.assertTrue(
                is_ancestor(root, head, "refs/heads/feature"),
                "DEGENERATE FIXTURE: the override cannot reach the sha, so a "
                "refusal would come from the FIRST check and prove nothing "
                "about the cross-check")
            self.assertFalse(
                is_ancestor(root, head, "refs/remotes/origin/main"),
                "DEGENERATE FIXTURE: the discovered branch can already reach "
                "the sha, so there is nothing for the cross-check to object to")
            self.assertNotEqual(base, head)
            self.assertNotEqual(tip, head)
            self.assertEqual(
                ratchet.git_sha(), head,
                "premise broken: the writer is not stamping the feature HEAD, "
                "so this test does not exercise the #6564 shape at all")

            with self.assertRaises(
                SystemExit,
                msg=(f"the writer ACCEPTED {head!r}, a feature-branch HEAD that "
                     f"origin/main cannot reach, because QUALITY_BASE_REF named "
                     f"the very branch the sha was computed from. Squash "
                     f"orphans that commit on merge and internal/quality goes "
                     f"red on main for everyone (#6564)"),
            ):
                ratchet.ensure_durable_measured_on(head)

    def test_it_refuses_a_REMOTE_namespace_override_just_the_same(self):
        """The same refusal when the override lives under `refs/remotes/`.

        VARIED: the override's ref NAMESPACE (`refs/remotes/origin/feature`
        rather than `refs/heads/feature`).
        HELD CONSTANT: everything else — the same history, the same commit
        stamped, the same `origin/main` that cannot reach it, the same expected
        refusal.

        Measured, not assumed: without this test the two lines

            if override.startswith("refs/remotes/"):
                return

        at the top of `_cross_check_override` survive the entire suite —
        `Ran 32 tests ... OK`, exit 0 — because every REFUSING fixture here used
        a `refs/heads/` override while the only remote-namespace overrides
        (`refs/remotes/origin/release`) appeared in ACCEPTING tests. One axis
        pinned, its neighbour wide open.

        The neighbour is not hypothetical. `QUALITY_BASE_REF=$(git symbolic-ref
        refs/remotes/origin/HEAD)` — the shape the docstring itself suggests —
        is a remote ref, so a narrowing in exactly this direction would have
        passed CI while disabling the guard for the commonest spelling of the
        override.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            _base, head, _tip = make_repo(root)
            git(root, "update-ref", "refs/remotes/origin/feature",
                "refs/heads/feature")
            set_base_ref(self, "refs/remotes/origin/feature")

            self.assertTrue(has_ref(root, "refs/remotes/origin/feature"))
            self.assertTrue(
                has_ref(root, "refs/remotes/origin/main"),
                "DEGENERATE FIXTURE: nothing for the cross-check to consult")
            # The gate must NOT skip: these two remote refs share history, so a
            # refusal here is about the override, not about disjointness.
            self.assertEqual(
                subprocess.call(
                    ["git", "merge-base", "refs/remotes/origin/feature",
                     "refs/remotes/origin/main"], cwd=root,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL), 0,
                "DEGENERATE FIXTURE: the two refs are disjoint, so the "
                "disjointness gate carries this test and the namespace is "
                "never reached")
            self.assertFalse(
                is_ancestor(root, head, "refs/remotes/origin/main"))
            self.assertEqual(ratchet.git_sha(), head)

            with self.assertRaises(
                SystemExit,
                msg=(f"the writer ACCEPTED {head!r} because the override was "
                     f"spelled under refs/remotes/. A remote-tracking ref is "
                     f"not evidence of durability: this one points at the very "
                     f"feature branch whose HEAD squash will orphan")):
                ratchet.ensure_durable_measured_on(head)

    def test_it_refuses_on_a_checkout_with_no_origin_HEAD_symref(self):
        """The same refusal on a checkout that has `refs/remotes/origin/main`
        but NO `refs/remotes/origin/HEAD`.

        Measured, not assumed (Refs #6570): a cross-check that reads only the
        symref and treats its absence as "nothing discovered, accept" survives
        the whole suite without this test, because every other fixture with a
        remote sets the symref. The shape is ordinary — `origin/HEAD` is written
        by `git clone`, not by `git remote add` + `git fetch`, and it is exactly
        the shape `make_repo_stale_local_main` documents — so an override on
        such a checkout would still vouch for itself, which is the whole of the
        bug. DEFAULT_BRANCH_REFS is the fallback that must be consulted.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            _base, head, _tip = make_repo(root)
            git(root, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
            set_base_ref(self, "refs/heads/feature")

            self.assertFalse(
                has_ref(root, "refs/remotes/origin/HEAD"),
                "DEGENERATE FIXTURE: the symref is still present, so this is "
                "the same shape as the test above")
            self.assertTrue(has_ref(root, "refs/remotes/origin/main"))
            self.assertFalse(
                is_ancestor(root, head, "refs/remotes/origin/main"))
            self.assertEqual(ratchet.git_sha(), head)

            with self.assertRaises(
                SystemExit,
                msg=(f"the writer ACCEPTED {head!r} because `origin/HEAD` was "
                     f"absent — the cross-check treated a checkout that plainly "
                     f"has origin/main as having no discoverable base, so the "
                     f"override vouched for itself again")):
                ratchet.ensure_durable_measured_on(head)

    def test_the_writer_refuses_rather_than_stamping_a_feature_head(self):
        """The operator-visible half: `--update-baseline` must not produce a
        baseline carrying the orphan-to-be."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            _base, head, _tip = make_repo(root)
            set_base_ref(self, "refs/heads/feature")
            golden, reports, baseline = make_fixture(root)
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)

            try:
                doc = ratchet.build(golden, reports, baseline)
            except SystemExit as exc:
                # ANY SystemExit is not enough: build() has other exits, and a
                # test that accepts them all goes green on a refusal that had
                # nothing to do with durability. The sha is the one part of the
                # message that is not the code reading back its own ref choice
                # (#6630), so it is what this asserts.
                self.assertIn(
                    head, str(exc),
                    f"--update-baseline exited, but not over {head!r} — this "
                    f"test would pass on an unrelated refusal")
                return
            self.fail(
                f"--update-baseline wrote measured_on {doc['measured_on']!r} "
                f"(feature HEAD is {head!r}), which origin/main cannot reach")

    def test_an_override_the_discovered_branch_agrees_with_is_still_honoured(self):
        """The cross-check must FIRE and PASS here, not make the override
        useless. Without this, "refuse whenever an override is set" is green."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            base, _head, tip = make_repo(root)
            set_base_ref(self, "refs/heads/main")

            # The override and the ref this checkout discovers for itself must
            # be DIFFERENT refs, or the cross-check short-circuits and this test
            # observes nothing. Asked of git, not of ratchet.py, so the premise
            # holds whichever side of the fix this file is read on.
            self.assertEqual(
                git(root, "symbolic-ref", "refs/remotes/origin/HEAD"),
                "refs/remotes/origin/main")
            self.assertEqual(ratchet.default_branch_ref(), "refs/heads/main")
            # ... and history-connected, or the disjointness gate carries it.
            self.assertNotEqual(
                git(root, "merge-base", "refs/heads/main",
                    "refs/remotes/origin/main"), "")
            self.assertTrue(is_ancestor(root, base, "refs/remotes/origin/main"))
            self.assertEqual(ratchet.git_sha(), base)
            self.assertNotEqual(base, tip)

            err = io.StringIO()
            try:
                with contextlib.redirect_stderr(err):
                    ratchet.ensure_durable_measured_on(base)
            except SystemExit as exc:
                self.fail(
                    f"the writer refused {base!r}, which BOTH the override and "
                    f"origin/main can reach — the cross-check now vetoes every "
                    f"override rather than only the ones that cannot be "
                    f"vouched for ({exc})")
            self.assertEqual(
                err.getvalue(), "",
                "the 'unknown' guard fired; neither ancestry check was reached "
                "and this test observed nothing")

    def test_a_discovered_ref_disjoint_from_the_override_gets_no_veto(self):
        """The disjointness gate, named directly.

        `AncestryFollowsTheDiscoveredRef` and `PaddedBaseRefOverrideIsHonoured`
        both fail without it; this states WHY as its own proposition, so a
        future edit that drops the gate reads a failure about disjoint history
        rather than one about the padded override.
        """
        with tempfile.TemporaryDirectory() as root, chdir(root):
            branch_point, orphan = make_repo_divergent_default_ref(root)
            set_base_ref(self, "refs/remotes/origin/release")

            self.assertTrue(
                has_ref(root, "refs/remotes/origin/main"),
                "DEGENERATE FIXTURE: no discovered ref, so there is no veto to "
                "withhold")
            self.assertNotEqual(
                subprocess.call(
                    ["git", "merge-base", "refs/remotes/origin/release",
                     "refs/remotes/origin/main"], cwd=root,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL), 0,
                "DEGENERATE FIXTURE: the two refs share history, so this is not "
                "the disjoint case")
            self.assertFalse(is_ancestor(root, branch_point,
                                         "refs/remotes/origin/main"))
            self.assertNotEqual(branch_point, orphan)

            err = io.StringIO()
            try:
                with contextlib.redirect_stderr(err):
                    ratchet.ensure_durable_measured_on(branch_point)
            except SystemExit as exc:
                self.fail(
                    f"the writer refused {branch_point!r} on the say-so of an "
                    f"origin/main that shares NO history with this checkout — "
                    f"a different project's main was given a veto over the "
                    f"operator's declared base ({exc})")
            self.assertEqual(err.getvalue(), "")


class ForbiddenEntityHitsAreFatal(unittest.TestCase):
    """#6488 arm B. `check` reads forbidden_entity_hits from its own key.

    Folding it into `forbidden_hits` was rejected because that key means
    "false-positive EDGES" to every recorded baseline. The cost of the separate
    key is that a second read has to exist, and a read that does not exist
    fails silently and forever: the report would carry the hit, the ratchet
    would print OK, and the fixture would be graded on recall alone — which is
    precisely the blindness the field was added to remove.
    """

    def _run_check(self, report_extra):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            golden, reports, baseline = make_fixture(root)
            with open(os.path.join(reports, "demo-mini.json")) as fh:
                rep = json.load(fh)
            rep.update(report_extra)
            with open(os.path.join(reports, "demo-mini.json"), "w") as fh:
                json.dump(rep, fh)
            err = io.StringIO()
            with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                rc = ratchet.check(golden, reports, baseline)
            return rc, err.getvalue()

    def test_a_clean_report_still_passes(self):
        """The negative control. Without it a check that failed unconditionally
        would score identically on the test below."""
        rc, err = self._run_check({"forbidden_entity_hits": 0})
        self.assertEqual(rc, 0, f"a clean report was failed: {err}")

    def test_a_forbidden_entity_hit_fails_the_ratchet(self):
        rc, err = self._run_check({"forbidden_entity_hits": 2})
        self.assertEqual(
            rc, 2,
            "a report carrying 2 forbidden ENTITY hits passed the ratchet — "
            "entity over-emission is gated by nothing else")
        self.assertIn("forbidden entity row(s) fired", err)

    def test_a_report_predating_the_field_is_not_a_failure(self):
        """Absent means zero, not "unknown". Every baseline recorded before
        this key existed omits it, and a check that treated absence as a hit
        would fail the whole corpus on a schema change."""
        rc, err = self._run_check({})
        self.assertEqual(rc, 0, f"a report with no forbidden_entity_hits key failed: {err}")


class ExtractedTotalsAreGatedInTheGrowthDirection(unittest.TestCase):
    """#6488 arm D. `entity_extracted_total` / `relationship_extracted_total`
    are recorded per fixture and compared in the GROWTH direction only.

    The gap: both totals were already computed (internal/quality/diff.go) and
    published (internal/quality/report.go), and `observed()` dropped them. No
    baseline could express "extraction must not grow", so unnamed over-emission
    was ungated on all 31 fixtures — measured, not argued: restoring the
    pre-#681 per-import placeholder entity in the Java extractor raises
    java-spring-mini's entity_extracted_total 61 -> 86 and its
    relationship_extracted_total 196 -> 221 while leaving all four gated
    figures at 29/29 and 27/27 and both forbidden counts at 0, i.e. exit 0.

    The risk this class grades is VACUITY: a number recorded and never read.
    Every case below therefore observes `check`'s exit status, not the shape of
    the baseline document.
    """

    def _run_check(self, report_extra=None, baseline_edit=None):
        """Run `check` over the shared fixture, with the report and/or the
        recorded baseline entry edited first. Returns (exit code, stderr)."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            golden, reports, baseline = make_fixture(root)
            report_path = os.path.join(reports, "demo-mini.json")
            with open(report_path) as fh:
                rep = json.load(fh)
            for key, val in (report_extra or {}).items():
                if val is None:
                    rep.pop(key, None)
                else:
                    rep[key] = val
            with open(report_path, "w") as fh:
                json.dump(rep, fh)
            if baseline_edit is not None:
                with open(baseline) as fh:
                    doc = json.load(fh)
                baseline_edit(doc["fixtures"]["demo-mini"])
                with open(baseline, "w") as fh:
                    json.dump(doc, fh)
            err = io.StringIO()
            with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                rc = ratchet.check(golden, reports, baseline)
            return rc, err.getvalue()

    def test_unchanged_totals_pass(self):
        """The negative control. Without it every case below scores the same on
        a `check` that failed unconditionally."""
        rc, err = self._run_check()
        self.assertEqual(rc, 0, f"a report matching its baseline was failed: {err}")

    def test_a_raised_entity_total_fails_and_names_the_fixture(self):
        rc, err = self._run_check({"entity_extracted_total": 13})
        self.assertEqual(
            rc, 2,
            "entity_extracted_total rose above the recorded floor and the "
            "ratchet passed — unnamed entity over-emission is gated by nothing "
            f"else. stderr: {err}")
        self.assertIn("entity_extracted_total", err)
        self.assertIn("demo-mini", err)

    def test_a_raised_relationship_total_fails_and_names_the_fixture(self):
        """The entity case alone does not distinguish a loop over both metrics
        from a hard-coded read of the entity key."""
        rc, err = self._run_check({"relationship_extracted_total": 8})
        self.assertEqual(
            rc, 2,
            f"relationship_extracted_total rose and the ratchet passed: {err}")
        self.assertIn("relationship_extracted_total", err)
        self.assertIn("demo-mini", err)

    def test_a_fallen_total_passes(self):
        """The asymmetry, pinned. Growth is over-emission; a fall is an
        extractor getting tighter, and failing it would put the gate red on
        every legitimate improvement and train people to re-record blind."""
        rc, err = self._run_check(
            {"entity_extracted_total": 1, "relationship_extracted_total": 0})
        self.assertEqual(rc, 0, f"a FALL in the extracted totals was failed: {err}")

    def test_a_report_without_the_totals_is_not_a_silent_pass(self):
        """The vacuity case. `int(rep.get(key, 0))` would read an absent key as
        0, i.e. as a fall, and pass — so a binary that stopped emitting the
        field would silently retire the gate rather than break it."""
        rc, err = self._run_check(
            {"entity_extracted_total": None, "relationship_extracted_total": None})
        self.assertEqual(
            rc, 2,
            f"a report carrying neither extracted total passed the ratchet: {err}")
        self.assertIn("entity_extracted_total", err)
        self.assertIn("demo-mini", err)

    def test_a_baseline_without_the_totals_demands_a_re_record(self):
        """Distinct from the case above, and only this one fires when the
        REPORT carries both totals: a baseline recorded before arm D records no
        floor, and treating that as "nothing to compare" is how a schema
        addition ungates the whole corpus in silence."""
        def strip(entry):
            entry.pop("entity_extracted_total", None)
            entry.pop("relationship_extracted_total", None)
        rc, err = self._run_check(baseline_edit=strip)
        self.assertEqual(
            rc, 2,
            f"a baseline recording no extracted totals was accepted: {err}")
        self.assertIn("--update-baseline", err)
        self.assertIn("demo-mini", err)

    def test_update_records_both_totals_after_the_recall_figures(self):
        """`build` writes them, and writes them LAST. The order is asserted
        because `serialize()` does not gate key order (its own docstring says
        so) — `canon` feeds json.dumps a document parsed straight from disk, so
        it echoes whatever order the file already has. internal/quality/
        baseline_test.go pins the committed file's order; this pins the
        writer's, and the two together are what keep them equal."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            make_repo(root)
            golden, reports, baseline = make_fixture(root)
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            doc = ratchet.build(golden, reports, baseline)
        self.assertEqual(
            list(doc["fixtures"]["demo-mini"].keys()),
            ["entity_found", "entity_expected",
             "relationship_found", "relationship_expected",
             "entity_extracted_total", "relationship_extracted_total"])
        self.assertEqual(doc["fixtures"]["demo-mini"]["entity_extracted_total"], 12)
        self.assertEqual(doc["fixtures"]["demo-mini"]["relationship_extracted_total"], 7)

    def test_update_refuses_a_report_missing_EITHER_total(self):
        """Recording a floor of 0 from an absent key would be worse than
        refusing: every subsequent run would read its real total as growth.

        Each key is deleted on its own. Deleting only the entity total leaves
        the relationship half of build()'s refusal graded by nothing —
        `EXTRACTED_TOTAL_KEYS[:1]` in build() survived exactly that test, and
        the two halves of the loop are a masking pair until both are named.
        """
        for missing in ("entity_extracted_total", "relationship_extracted_total"):
            with self.subTest(missing=missing):
                with tempfile.TemporaryDirectory() as root, chdir(root):
                    make_repo(root)
                    golden, reports, baseline = make_fixture(root)
                    path = os.path.join(reports, "demo-mini.json")
                    with open(path) as fh:
                        rep = json.load(fh)
                    del rep[missing]
                    with open(path, "w") as fh:
                        json.dump(rep, fh)
                    os.environ["QUALITY_RUN_STAMP"] = STAMP
                    self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
                    with self.assertRaises(SystemExit) as ctx:
                        ratchet.build(golden, reports, baseline)
                    # Not any SystemExit: build() has several, and a test that
                    # accepts them all goes green on an unrelated refusal. The
                    # message must name the key that is actually missing, so
                    # the two subtests cannot be satisfied by one branch.
                    self.assertIn(missing, str(ctx.exception))
                    self.assertIn("demo-mini", str(ctx.exception))


# ---------------------------------------------------------------------------
# #7084 — the merge step in scripts/quality/run.sh, driven directly.
# ---------------------------------------------------------------------------

RUN_SH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "run.sh")

AGGREGATOR_OPEN = 'python3 - "$tmpdir" "$json" "$name" <<\'PY\''
STABILITY_OPEN = 'python3 - "$tmpdir" <<\'PY\''


def extract_heredoc(opener, tokens, what):
    """Slice one python heredoc program out of run.sh.

    These programs are heredocs inside a shell script, so they cannot be
    imported. Copying one into this file would test a COPY: the run.sh text
    could go back to the median and every case below would stay green. So the
    real bytes are sliced out and executed. The slice is asserted, not assumed
    — a heredoc that has moved or been renamed must fail loudly here rather
    than yield an empty program that "passes" everything.
    """
    with open(RUN_SH) as fh:
        lines = fh.read().splitlines()
    opens = [i for i, ln in enumerate(lines) if opener in ln]
    if len(opens) != 1:
        raise AssertionError(
            f"expected exactly 1 {what} heredoc in run.sh, found {len(opens)} "
            f"— the step moved; this harness is measuring nothing")
    start = opens[0] + 1
    end = None
    for i in range(start, len(lines)):
        if lines[i] == "PY":
            end = i
            break
    if end is None:
        raise AssertionError(f"the {what} heredoc in run.sh is unterminated")
    body = "\n".join(lines[start:end]) + "\n"
    # Content floor. An extraction that silently produced a stub would behave
    # uniformly on every input and read as a clean gate.
    for token in tokens:
        if token not in body:
            raise AssertionError(
                f"extracted {what} does not mention {token!r} — the slice is wrong")
    return body


def extract_aggregator():
    """The per-fixture merge program (medians, max, gate exit code)."""
    return extract_heredoc(
        AGGREGATOR_OPEN,
        ("forbidden_hits", "forbidden_entity_hits", "known_bad_silent",
         "silent_in_every_run", "sys.exit(2)"),
        "aggregator")


def extract_stability():
    """The short-circuit predicate: `yes` stops the repeat loop early."""
    return extract_heredoc(
        STABILITY_OPEN,
        ("entity_recall", "relationship_recall", "known_bad_silent", "yes", "no"),
        "stability")


def merge_runs(tmp, runs, fixture_name="demo-mini"):
    """Run the real run.sh merge program over `runs` (a list of report dicts).

    Returns (exit code, merged report dict or None).
    """
    rundir = os.path.join(tmp, "runs")
    os.makedirs(rundir, exist_ok=True)
    for i, rep in enumerate(runs):
        with open(os.path.join(rundir, f"run{i}.json"), "w") as fh:
            json.dump(rep, fh)
    prog = os.path.join(tmp, "aggregator_under_test.py")
    with open(prog, "w") as fh:
        fh.write(extract_aggregator())
    out = os.path.join(tmp, "merged.json")
    env = dict(os.environ)
    env["QUALITY_RUN_STAMP"] = STAMP
    proc = subprocess.run(
        [sys.executable, prog, rundir, out, fixture_name],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
    merged = None
    if os.path.exists(out):
        with open(out) as fh:
            merged = json.load(fh)
    return proc.returncode, merged


def run_report(**extra):
    """One per-run report as `grafel quality --json` writes it."""
    rep = {
        "fixture": "demo-mini", "run_stamp": STAMP,
        "entity_recall": 0.4, "relationship_recall": 0.0,
        "entity_found": 4, "entity_expected": 10,
        "relationship_found": 0, "relationship_expected": 0,
        "forbidden_hits": 0, "forbidden_entity_hits": 0,
        "entity_extracted_total": 12, "relationship_extracted_total": 7,
    }
    rep.update(extra)
    return rep


# The one shape the median erases: a forbidden row seen by a MINORITY of the
# runs. Defined once, at module scope, because a control that asserts about its
# own private copy of the shape grades nothing the other cases execute — flip
# the graded cases to unanimous and such a control stays green while direction
# 1 silently degenerates into a duplicate of the all-runs case.
MINORITY_RUNS = [0, 0, 1, 0, 1]
UNANIMOUS_RUNS = [1, 1, 1, 1, 1]
CLEAN_RUNS = [0, 0, 0, 0, 0]
# The run loop short-circuits at 3 (see ShortCircuitCannotTruncateAForbiddenRow),
# so the minority shape has to survive at the FLOOR too, not only at 5.
MINORITY_RUNS_AT_THE_SHORT_CIRCUIT_FLOOR = [0, 1, 0]


class MinorityFiringRowIsConstructible(unittest.TestCase):
    """POSITIVE CONTROL for the cases below.

    Every assertion in `ForbiddenHitsSurviveTheMerge` rests on one premise:
    that this harness can build a set of runs in which a forbidden row fires in
    a MINORITY of them — the only shape the median erases. If the premise were
    false (all five runs identical, say) the direction-1 cases would be graded
    by a fixture that cannot express the defect, and would pass on the broken
    code too.
    """

    def test_the_fixture_shape_is_a_minority_and_the_median_erases_it(self):
        """Both lengths, because both are configurations run.sh really runs:
        QUALITY_RUNS defaults to 5, and the stability short-circuit can stop
        the loop at 3."""
        for runs in (MINORITY_RUNS, MINORITY_RUNS_AT_THE_SHORT_CIRCUIT_FLOOR):
            with self.subTest(runs=runs):
                firing = [n for n in runs if n > 0]
                self.assertGreater(
                    len(firing), 0, "no run fires; there is nothing to erase")
                self.assertLess(
                    len(firing), len(runs) / 2,
                    "the row fires in half or more of the runs — the median would "
                    "keep it, so this fixture cannot express the defect")
                self.assertEqual(
                    int(statistics.median(runs)), 0,
                    "the median of this fixture is non-zero, so the OLD aggregator "
                    "would have caught it and direction 1 grades nothing")


class ForbiddenHitsSurviveTheMerge(unittest.TestCase):
    """#7084. `forbidden_*` is aggregated across runs with MAX, not the median.

    `forbidden_relationships` is the only mechanism this repo has for "this
    edge is wrong", and it is documented as ALWAYS FATAL. ratchet.py fails on
    `forbidden > 0`, so with a 5-run median a row firing in two runs merged to
    0 and the gate never saw it — the guarantee was silently conditional on the
    defect being deterministic, which #7077/#7072/#7079 are not.

    Each case asserts the EMITTED FAILURE TEXT out of `check`, not the merged
    integer: the integer is the thing under edit, so asserting on it would
    grade the edit against itself.
    """

    EDGE_MSG = "demo-mini: 1 forbidden relationship hit(s) — always fatal"
    ENT_MSG = "demo-mini: 1 forbidden entity row(s) fired — always fatal"

    def _merge_then_check(self, runs):
        """Merge `runs` with run.sh's own program, then grade the merged report
        with ratchet.check. Returns (aggregator rc, check rc, check stderr)."""
        with tempfile.TemporaryDirectory() as root, chdir(root):
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            golden, reports, baseline = make_fixture(root)
            agg_rc, merged = merge_runs(root, runs)
            self.assertIsNotNone(merged, "the aggregator wrote no merged report")
            with open(os.path.join(reports, "demo-mini.json"), "w") as fh:
                json.dump(merged, fh)
            err = io.StringIO()
            with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                rc = ratchet.check(golden, reports, baseline)
            return agg_rc, rc, err.getvalue(), merged

    @staticmethod
    def _runs(counts, key="forbidden_hits"):
        offender = {"from": "A", "to": "B", "kind": "SCOPE.Calls"}
        if key == "forbidden_entity_hits":
            detail_key, offender = "forbidden_entities", {"name": "A", "kind": "SCOPE.Component"}
        else:
            detail_key = "forbidden"
        out = []
        for n in counts:
            extra = {key: n}
            if n > 0:
                extra[detail_key] = [offender]
            out.append(run_report(**extra))
        return out

    def test_a_row_firing_in_a_minority_of_runs_fails_the_gate(self):
        """Direction 1. Two of five runs saw the wrong edge; the graph
        contained it. On the median this merged to 0 and `check` printed OK."""
        _agg_rc, rc, err, _ = self._merge_then_check(self._runs(MINORITY_RUNS))
        self.assertIn(
            self.EDGE_MSG, err,
            "a forbidden EDGE row that fired in 2 of 5 runs was not named by "
            "the gate — the always-fatal guarantee is conditional on the "
            "defect being deterministic")
        self.assertEqual(rc, 2)

    def test_an_entity_row_firing_in_a_minority_of_runs_fails_the_gate(self):
        """The same direction for the ENTITY key. The two counts are separate
        keys and separate reads; fixing one and leaving the other is exactly
        the shape that survived here before."""
        _agg_rc, rc, err, merged = self._merge_then_check(
            self._runs(MINORITY_RUNS, key="forbidden_entity_hits"))
        self.assertIn(self.ENT_MSG, err)
        self.assertEqual(rc, 2)
        # The reporting surface's entity twin. Its edge counterpart is graded
        # below; asserting only the zero direction on this one would be
        # satisfied by a constant 0.
        self.assertEqual(
            merged["forbidden_entity_hits_runs"], 2,
            "the merged report does not say how many runs saw the entity row fire")
        self.assertEqual(
            merged.get("forbidden_entities"),
            [{"name": "A", "kind": "SCOPE.Component"}],
            "the offending entity is not named in the merged report")

    def test_a_row_firing_in_no_run_still_passes(self):
        """Direction 2, the negative control. "Make it stricter" must not
        become "fire on everything": a gate that is always red is as
        uninformative as one that is never red."""
        _agg_rc, rc, err, merged = self._merge_then_check(self._runs(CLEAN_RUNS))
        self.assertEqual(rc, 0, f"a clean five-run merge was failed: {err}")
        self.assertNotIn("forbidden", err)
        self.assertNotIn(
            "forbidden", merged,
            "a clean merge carried an offender array")
        self.assertEqual(merged["forbidden_hits"], 0)
        self.assertEqual(merged["forbidden_hits_runs"], 0)
        self.assertEqual(merged["forbidden_entity_hits_runs"], 0)

    def test_a_row_firing_in_every_run_fails_with_the_message_it_has_today(self):
        """Direction 2's other half. The unanimous case is the one that already
        worked; its wording is what operators and #6488's own tests read. A
        change that made the minority case fatal by rewording the unanimous one
        would break every reader for no reason."""
        _agg_rc, rc, err, _ = self._merge_then_check(self._runs(UNANIMOUS_RUNS))
        self.assertEqual(rc, 2)
        self.assertIn(self.EDGE_MSG, err)

    def test_the_merged_report_names_the_intermittent_offender_and_its_run_count(self):
        """Fatal is not the same as visible. The detail arrays are inherited
        from one arbitrary run (`reports[-1]`), so a row that did not fire in
        THAT run would be counted by the scalar and named by nothing — and a
        reader could not tell a row that fired once from one that fired five
        times. Asserted on the merged document because that is the artefact a
        human opens, not on the failure text."""
        with tempfile.TemporaryDirectory() as root:
            # The offender fires only in run 1, so the LAST run — the one whose
            # detail arrays are inherited — did not see it.
            runs = self._runs([0, 1, 0, 0, 0])
            _rc, merged = merge_runs(root, runs)
        self.assertEqual(merged["forbidden_hits"], 1)
        self.assertEqual(
            merged["forbidden_hits_runs"], 1,
            "the merged report does not say how many runs saw the row fire")
        self.assertEqual(
            merged.get("forbidden"), [{"from": "A", "to": "B", "kind": "SCOPE.Calls"}],
            "the offender seen only in a non-final run is absent from the "
            "merged report's detail array")

    def test_the_same_offender_seen_twice_is_named_once(self):
        """F3. The union de-duplicates, and nothing asserted it: the
        direction-1 fixture fires the SAME offender in both of its two runs, so
        dropping the `seen` check changes no other case. A report that named
        one wrong edge twice would read as two distinct defects.

        The second copy is key-order-permuted on purpose — de-duplication is
        done on `json.dumps(..., sort_keys=True)`, so a fingerprint that
        depended on dict order would let the permuted copy through."""
        offender = {"from": "A", "to": "B", "kind": "SCOPE.Calls"}
        permuted = {"kind": "SCOPE.Calls", "to": "B", "from": "A"}
        runs = [
            run_report(),
            run_report(forbidden_hits=1, forbidden=[offender]),
            run_report(),
            run_report(forbidden_hits=1, forbidden=[permuted]),
            run_report(),
        ]
        with tempfile.TemporaryDirectory() as root:
            _rc, merged = merge_runs(root, runs)
        self.assertEqual(
            len(merged["forbidden"]), 1,
            "one wrong edge seen in two runs is named twice in the merged report")
        self.assertEqual(merged["forbidden_hits"], 1)
        self.assertEqual(merged["forbidden_hits_runs"], 2)

    def test_the_count_is_per_run_and_the_array_is_across_runs(self):
        """F4. A divergence this PR introduces, stated rather than discovered.

        Before, the count and the detail array came from the same run and could
        not disagree. `max` over the counts and a UNION over the arrays makes
        them independent: two runs, one DISTINCT offender each, gives
        forbidden_hits=1 while `forbidden` names two.

        Which to believe: `forbidden_hits` is "the most rows any single run
        saw" — it is the gate's trigger and nothing else; `forbidden` is "every
        distinct wrong edge any run produced" — it is the list to act on, and
        it is the larger of the two whenever the offenders differ between runs.
        The gate fires on either being non-empty, so the divergence cannot
        weaken it; it can only under-state the headline number."""
        runs = [
            run_report(),
            run_report(forbidden_hits=1, forbidden=[{"from": "A", "to": "B"}]),
            run_report(),
            run_report(forbidden_hits=1, forbidden=[{"from": "C", "to": "D"}]),
            run_report(),
        ]
        with tempfile.TemporaryDirectory() as root:
            _rc, merged = merge_runs(root, runs)
        self.assertEqual(
            merged["forbidden_hits"], 1,
            "forbidden_hits is not the per-run maximum")
        self.assertEqual(
            [o["from"] for o in merged["forbidden"]], ["A", "C"],
            "the union dropped an offender only one run saw, or reordered them")
        self.assertEqual(
            merged["forbidden_hits_runs"], 2,
            "the run counter cannot distinguish this from a single-run hit")

    def test_the_aggregator_itself_fails_a_minority_row_in_strict_mode(self):
        """The OTHER grader. `check` is not the only consumer: run.sh reads the
        merge program's own exit status, and in strict mode that status alone
        decides the fixture. A fix that satisfied the ratchet but left the
        aggregator exiting 0 would leave strict mode blind.

        Recall is held at full here (found == expected) so the aggregator's
        recall branch cannot be the thing producing the exit code — otherwise
        both cases would exit 2 and neither would be about forbidden hits.

        BOTH keys, not one. The heredoc has two separate `regressed = True`
        branches, one per key, and a loop over one key leaves the other's
        branch deletable with the suite green — the same one-key-behind shape
        this PR closed on the `check` side.
        """
        full = {"entity_found": 10, "entity_expected": 10}
        for key in ("forbidden_hits", "forbidden_entity_hits"):
            for label, counts, want in (("clean", CLEAN_RUNS, 0),
                                        ("minority", MINORITY_RUNS, 2)):
                with self.subTest(key=key, case=label):
                    runs = [run_report(**{key: n}, **full) for n in counts]
                    with tempfile.TemporaryDirectory() as root:
                        rc, _merged = merge_runs(root, runs)
                    self.assertEqual(
                        rc, want,
                        f"the aggregator returned {rc} for the {label} "
                        f"{key} case")

    def test_the_recall_metrics_are_still_medianed(self):
        """Scope guard. Recall is a number with real jitter and the median is
        right for it; if this edit had reached entity_found, a single unlucky
        run would red the build. The odd run out is deliberately the LAST one
        so `merged` inheriting `base` cannot satisfy this by accident."""
        with tempfile.TemporaryDirectory() as root:
            runs = [run_report(entity_found=n) for n in (4, 4, 4, 4, 1)]
            _rc, merged = merge_runs(root, runs)
        self.assertEqual(
            merged["entity_found"], 4,
            "entity_found is no longer the median — one bad run now reds the gate")
        self.assertEqual(merged["entity_recall_min"], 0.4)
        self.assertEqual(merged["entity_recall_max"], 0.4)


class RunShIsParseable(unittest.TestCase):
    """`bash -n` over run.sh. Cheap, and it caught a real break in this PR.

    The short-circuit program is a heredoc inside a COMMAND SUBSTITUTION, and
    bash 3.2 — which is what macOS ships, and what any contributor on a Mac
    runs this script with — scans the substitution body for quotes even when
    the heredoc is opened `<<'PY'`. A single apostrophe in a python comment
    inside that heredoc makes the entire script unparseable, with the error
    reported hundreds of lines away. Nothing else in the suite executes run.sh
    as a shell script, so without this the break would reach a contributor.
    """

    def test_bash_can_parse_run_sh(self):
        if subprocess.call(["bash", "--version"],
                           stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL) != 0:  # pragma: no cover
            self.skipTest("bash not on PATH")
        proc = subprocess.run(["bash", "-n", RUN_SH],
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.assertEqual(
            proc.returncode, 0,
            f"run.sh does not parse: {proc.stderr.decode()}")


def run_stability(tmp, runs):
    """Run run.sh's own short-circuit predicate over `runs`. Returns its
    verdict, "yes" (stop the repeat loop now) or "no" (keep going)."""
    rundir = os.path.join(tmp, "runs")
    os.makedirs(rundir, exist_ok=True)
    for i, rep in enumerate(runs):
        with open(os.path.join(rundir, f"run{i}.json"), "w") as fh:
            json.dump(rep, fh)
    prog = os.path.join(tmp, "stability_under_test.py")
    with open(prog, "w") as fh:
        fh.write(extract_stability())
    proc = subprocess.run(
        [sys.executable, prog, rundir],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return proc.stdout.decode().strip()


class ShortCircuitCannotTruncateAForbiddenRow(unittest.TestCase):
    """#7084, second half. The repeat loop stops early when RECALL is stable.

    `run.sh` breaks out of the run loop once 3 runs agree on entity_recall and
    relationship_recall to within 0.5pp. That predicate did not look at the
    forbidden counts at all — so for a recall-stable fixture (most of them)
    QUALITY_RUNS=5 was effectively 3, and a row firing with p~0.4 was missed
    21.6% of the time rather than 7.8%. Aggregating with `max` fixes what
    happens to the runs that were taken; it cannot recover a run that the loop
    decided not to take.

    A forbidden hit in any completed run therefore vetoes the short-circuit:
    the cheapest thing to do with evidence that the graph is intermittently
    wrong is to keep looking. Recall stability is left exactly as it was — the
    veto only ever makes the loop run longer, never shorter.
    """

    @staticmethod
    def _stable_runs(n=3, **extra_last):
        runs = [run_report(entity_recall=0.40, relationship_recall=0.50)
                for _ in range(n)]
        runs[-1].update(extra_last)
        return runs

    def test_a_clean_recall_stable_fixture_still_short_circuits(self):
        """The negative control. A veto that fired unconditionally would make
        every fixture run the full five and would score identically on the
        cases below."""
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(
                run_stability(root, self._stable_runs()), "yes",
                "a clean, recall-stable fixture no longer short-circuits — the "
                "veto is firing on everything")

    def test_a_forbidden_edge_hit_vetoes_the_short_circuit(self):
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(
                run_stability(root, self._stable_runs(forbidden_hits=1)), "no",
                "the loop stopped at 3 runs with a forbidden EDGE row already "
                "observed — the remaining runs, which is the evidence the gate "
                "needs, were never taken")

    def test_a_forbidden_entity_hit_vetoes_the_short_circuit(self):
        """The entity twin. Two keys, two reads; a veto that checked one leaves
        the other's runs truncated exactly as before."""
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(
                run_stability(root, self._stable_runs(forbidden_entity_hits=1)), "no",
                "the loop stopped at 3 runs with a forbidden ENTITY row already "
                "observed")

    def test_recall_instability_still_vetoes_it(self):
        """The pre-existing behaviour, pinned so the veto cannot be bolted on
        by replacing the recall predicate rather than adding to it."""
        runs = self._stable_runs()
        runs[-1]["entity_recall"] = 0.80
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(run_stability(root, runs), "no")

    def test_two_runs_are_never_enough(self):
        """The floor is 3. Asserted because the veto is implemented in the same
        program and a rewrite could drop the length guard unnoticed."""
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(run_stability(root, self._stable_runs(n=2)), "no")

    def test_the_truncated_run_count_still_catches_a_minority_row(self):
        """The consequence, end to end: even at the short-circuit floor of 3, a
        row seen by one run is fatal. This is the case the median erased most
        often, because 3 runs is what most fixtures actually execute."""
        runs = [run_report(forbidden_hits=n, entity_found=10, entity_expected=10)
                for n in MINORITY_RUNS_AT_THE_SHORT_CIRCUIT_FLOOR]
        with tempfile.TemporaryDirectory() as root:
            rc, merged = merge_runs(root, runs)
        self.assertEqual(rc, 2, "a 1-in-3 forbidden row did not fail the fixture")
        self.assertEqual(merged["forbidden_hits"], 1)
        self.assertEqual(merged["forbidden_hits_runs"], 1)


# ---------------------------------------------------------------------------
# #7056 — the known-bad precision channel.
# ---------------------------------------------------------------------------

KB_ISSUE = "#6425"
KB_NOTE = "yul operation minted from an assembly block label"
KB_LABEL = "Vault --[CALLS]--> helper"
KB_ENTITY_LABEL = "Ghost (SCOPE.Schema)"

# A known_bad row comes in two classes, and EVERY consumer of the channel reads
# both: declared_known_bad() walks a two-element tuple of fixture keys,
# internal/quality/diff.go has one arm per class, report.go serialises them into
# one array, and the run.sh short-circuit veto reads that array.
#
# Parameterising on this tuple rather than testing the edge class and trusting
# the twin is deliberate. Dropping "forbidden_entities" from declared_known_bad
# was ALIVE against a suite that exercised only relationship rows: `declared`
# computed 0, the "nothing declared, nothing reported" short-circuit fired, and
# a stale binary silently retired an entity-class finding — the exact failure
# the cross-check exists to close. Scoping the run.sh veto to
# class == "relationship" was ALIVE for the same reason.
KB_CLASSES = ("relationship", "entity")


def kb_label(cls):
    return KB_LABEL if cls == "relationship" else KB_ENTITY_LABEL


def kb_row(label=None, cls="relationship", issue=KB_ISSUE, note=KB_NOTE):
    """One serialised known_bad row, as internal/quality/report.go writes it."""
    if label is None:
        label = kb_label(cls)
    return {"class": cls, "label": label, "issue": issue, "note": note}


def declare_known_bad(golden, n=1, fixture="demo-mini", cls="relationship"):
    """Add `n` known_bad forbidden rows of class `cls` to a fixture's
    expected.json.

    The DECLARATION is the floor this channel grades against — deliberately
    not a figure in baseline.json — so a test that wants a known-bad row has
    to write it here, in the hand-authored file, exactly as an author would.

    The two classes land under DIFFERENT keys (forbidden_relationships vs
    forbidden_entities), which is precisely why a helper that could only write
    one of them left half of declared_known_bad() ungraded.
    """
    path = os.path.join(golden, fixture, "expected.json")
    with open(path) as fh:
        fix = json.load(fh)
    if cls == "relationship":
        fix["forbidden_relationships"] = [
            {"from_name": "Vault", "kind": "CALLS", "to_name": f"helper{i}",
             "known_bad": True, "issue": KB_ISSUE, "note": KB_NOTE}
            for i in range(n)
        ]
    elif cls == "entity":
        fix["forbidden_entities"] = [
            {"name": f"Ghost{i}", "kind": "SCOPE.Schema",
             "known_bad": True, "issue": KB_ISSUE, "note": KB_NOTE}
            for i in range(n)
        ]
    else:  # pragma: no cover - a typo in a subTest must not silently declare 0
        raise AssertionError(f"unknown known_bad class {cls!r}")
    with open(path, "w") as fh:
        json.dump(fix, fh)


def kb_report(cls="relationship", **extra):
    """A merged per-fixture report carrying the known-bad keys."""
    rep = {
        "fixture": "demo-mini", "run_stamp": STAMP,
        "entity_found": 4, "entity_expected": 10,
        "relationship_found": 0, "relationship_expected": 0,
        "forbidden_hits": 0, "forbidden_entity_hits": 0,
        "entity_extracted_total": 12, "relationship_extracted_total": 7,
        "known_bad_declared": 1, "known_bad_hits": 1,
        "known_bad": [kb_row(cls=cls)],
    }
    rep.update(extra)
    return rep


class DeclaredKnownBadReadsBothClasses(unittest.TestCase):
    """POSITIVE CONTROL for every parameterised case below.

    All of them rest on one premise: that `declare_known_bad(cls=...)` really
    produces a fixture whose declared count is 1 for BOTH classes. If the
    entity branch wrote a key ratchet.py does not read, `declared` would be 0,
    the short-circuit in known_bad_failures would fire, and every entity
    subTest would pass against a fixture that cannot express the defect —
    which is the shape that let the entity half stay ungraded in the first
    place.
    """

    def test_each_class_declares_exactly_one_row(self):
        for cls in KB_CLASSES:
            with self.subTest(cls=cls), tempfile.TemporaryDirectory() as root:
                golden, _reports, _baseline = make_fixture(root)
                declare_known_bad(golden, 1, cls=cls)
                self.assertEqual(
                    ratchet.declared_known_bad(golden, "demo-mini"), 1,
                    f"a {cls}-class known_bad row is not counted as declared; every "
                    f"{cls} case below is graded by a fixture that declares nothing")

    def test_the_two_classes_use_different_fixture_keys(self):
        """The reason one helper cannot stand in for the other, asserted rather
        than assumed."""
        seen = {}
        for cls in KB_CLASSES:
            with tempfile.TemporaryDirectory() as root:
                golden, _r, _b = make_fixture(root)
                declare_known_bad(golden, 1, cls=cls)
                with open(os.path.join(golden, "demo-mini", "expected.json")) as fh:
                    fix = json.load(fh)
                seen[cls] = sorted(
                    k for k in ("forbidden_relationships", "forbidden_entities")
                    if fix.get(k))
        self.assertNotEqual(
            seen["relationship"], seen["entity"],
            "both classes wrote the same fixture key, so one of them is not the "
            "class it claims to be and the pair is one test wearing two names")


class KnownBadIsRecordedNotFatal(unittest.TestCase):
    """Direction 1. A recorded precision defect must not fail the gate — and
    must not be SILENT about it either.

    forbidden_relationships is the only mechanism in this repo that can say
    "this edge is wrong", and a hit is unconditionally fatal, so a defect that
    has been found and cannot be fixed today can only be fixed now or not
    written down. golden/solidity-mini/NOTICE.md carries two such findings as
    markdown prose for exactly that reason.

    Every case here asserts the EMITTED TEXT, never the exit code alone. A
    green run that mentions nothing is the failure mode this channel would
    otherwise introduce: the defect would be recorded in a file and reported
    by no instrument, which is where it started.
    """

    def _check(self, rep, declared=1, cls="relationship"):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            golden, reports, baseline = make_fixture(root)
            if declared:
                declare_known_bad(golden, declared, cls=cls)
            with open(os.path.join(reports, "demo-mini.json"), "w") as fh:
                json.dump(rep, fh)
            out, err = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                rc = ratchet.check(golden, reports, baseline)
            return rc, out.getvalue() + err.getvalue()

    def test_a_firing_known_bad_row_passes_the_gate(self):
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                rc, text = self._check(kb_report(cls=cls), cls=cls)
                self.assertEqual(
                    rc, 0,
                    f"a {cls}-class known_bad row that fired failed the gate — the "
                    f"channel is still the always-fatal one it exists to replace:"
                    f"\n{text}")

    def test_a_firing_known_bad_row_is_named_in_the_output(self):
        """The row AND its issue. A pass that prints a bare count sends the
        reader back to the graph to work out which finding is still open, and
        a pass that prints nothing at all is prose in a JSON file."""
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                _rc, text = self._check(kb_report(cls=cls), cls=cls)
                self.assertIn(kb_label(cls), text,
                              "the passing gate does not name the known-bad row")
                self.assertIn(KB_ISSUE, text,
                              "the passing gate does not name the issue that owns it — an "
                              "untracked known-bad is indistinguishable from an accepted one")
                self.assertIn(KB_NOTE, text,
                              "the passing gate does not carry the note, so the mechanism "
                              "is lost and only the delta survives")

    def test_positive_control_the_same_row_without_known_bad_is_fatal(self):
        """Without this, every case above would pass on a gate that had simply
        stopped reading forbidden_hits."""
        rc, text = self._check(
            kb_report(forbidden_hits=1, known_bad_declared=0, known_bad_hits=0,
                      known_bad=[]),
            declared=0)
        self.assertEqual(rc, 2, "an ordinary forbidden hit is no longer fatal")
        self.assertIn("always fatal", text)

    def test_the_channel_is_off_by_default(self):
        """A fixture that declares no known_bad rows, graded by a report that
        carries none, is unaffected. Asserted because the cross-check below
        fails a report whose known_bad_declared disagrees with the fixture, and
        a version of it that fired on 0 == None would red every fixture."""
        rc, text = self._check(
            {"fixture": "demo-mini", "run_stamp": STAMP,
             "entity_found": 4, "entity_expected": 10,
             "relationship_found": 0, "relationship_expected": 0,
             "forbidden_hits": 0,
             "entity_extracted_total": 12, "relationship_extracted_total": 7},
            declared=0)
        self.assertEqual(rc, 0, f"a fixture with no known_bad rows went red:\n{text}")
        self.assertNotIn("known-bad", text)


class KnownBadIsNotAOneWayValve(unittest.TestCase):
    """DIRECTION 2, and the whole reason this is a mechanism rather than a
    mute button.

    A known_bad row asserts that the graph STILL HAS a defect. When it stops
    firing, one of two things happened, and they are indistinguishable from
    here: the defect was fixed (in which case the row must become an ordinary
    always-fatal forbidden row so the fix is locked in), or the producer that
    emitted the offending output quietly died (in which case a real defect has
    been replaced by a bigger one).

    Without this half, `known_bad` would be a one-way valve — a fixed defect
    recorded as broken forever, and a dead producer wearing the same face as a
    fix. That is the recall-blindness of this milestone re-created on the
    precision side, and it is why ratchet.py fails an IMPROVED recall floor
    rather than quietly accepting it.
    """

    def _check(self, rep, declared=1, cls="relationship"):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            golden, reports, baseline = make_fixture(root)
            if declared:
                declare_known_bad(golden, declared, cls=cls)
            with open(os.path.join(reports, "demo-mini.json"), "w") as fh:
                json.dump(rep, fh)
            out, err = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                rc = ratchet.check(golden, reports, baseline)
            return rc, out.getvalue() + err.getvalue()

    @staticmethod
    def _silent(cls):
        return dict(known_bad_hits=0, known_bad=[],
                    known_bad_silent=[kb_row(cls=cls)])

    def test_a_row_that_stopped_firing_fails_the_gate(self):
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                rc, _text = self._check(kb_report(cls=cls, **self._silent(cls)), cls=cls)
                self.assertEqual(
                    rc, 2,
                    f"a {cls}-class known_bad row that stopped firing was accepted — "
                    f"the channel is a one-way valve, and a dead producer now reads "
                    f"as a fix")

    def test_it_names_the_row_the_issue_and_both_readings(self):
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                _rc, text = self._check(kb_report(cls=cls, **self._silent(cls)), cls=cls)
                self.assertIn(kb_label(cls), text,
                              "the failure does not name the row that went quiet")
                self.assertIn(KB_ISSUE, text,
                              "the failure does not name the issue that owns it")
                self.assertIn("STOPPED FIRING", text)
                self.assertIn(
                    "producer", text,
                    "the failure does not tell the reader that a dead producer and a fix "
                    "look identical here — without that, every occurrence is read as a fix")

    def test_a_stale_binary_cannot_erase_a_declared_row(self):
        """The report predates the channel, so nothing measured the rows. That
        is not the same fact as "the rows did not fire", and reading it as one
        would let an old binary retire the finding silently — the #6488 arm D
        reasoning about an absent extracted total, on this key.

        BOTH CLASSES. This is the case that was one-sided: declared_known_bad()
        walks a two-key tuple, and with only relationship rows in the suite,
        dropping "forbidden_entities" from it was ALIVE — `declared` came back
        0, the "nothing declared, nothing reported" short-circuit in
        known_bad_failures fired, and an entity-class finding was retired in
        silence by exactly the stale binary this check exists to catch.
        """
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                rep = kb_report(cls=cls)
                del rep["known_bad_declared"]
                del rep["known_bad_hits"]
                del rep["known_bad"]
                rc, text = self._check(rep, cls=cls)
                self.assertEqual(
                    rc, 2,
                    f"a report with no known-bad keys graded a fixture that declares "
                    f"a {cls}-class known_bad row:\n{text}")
                self.assertIn("predates", text)

    def test_a_fixture_edited_without_a_rerun_is_red(self):
        """Two declared, one graded. The second row was measured by nothing, so
        it could not have been seen to go silent. Both classes, because the
        count this compares against is the one the two-key tuple produces."""
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                rc, text = self._check(kb_report(cls=cls), declared=2, cls=cls)
                self.assertEqual(rc, 2, text)
                self.assertIn("declares 2 known_bad row(s)", text)


class KnownBadOutputShapeIsGraded(unittest.TestCase):
    """The SHAPE of the two lines this channel emits, not merely their content.

    Print-on-success is the entire anti-rot mechanism here: a known-bad row
    cannot quietly rot because a reader sees it on every green run. The bracket
    is where a reader looks for a tracking id, and a line that fills it with a
    sentence of prose degrades exactly the affordance the mechanism exists to
    provide.

    Asserting that the issue and the note are both PRESENT does not grade that.
    Three role swaps were ALIVE against a suite that did:

      * green line, issue <-> note: emitted
        `[phantom yul operation ...]  #6425` instead of
        `[#6425]  phantom yul operation ...`;
      * STOPPED FIRING line, label <-> issue;
      * STOPPED FIRING line, class <-> label.

    Each is observable, none is equivalent, and all three left 81 tests green.
    So the assertion is on the ASSEMBLED substring, in order, with the brackets
    where they belong.

    A note on why this is not covered by the Go side: these are two DIFFERENT
    green-print surfaces. internal/quality reports pin `WriteHuman`, which is
    what a developer running `grafel quality` by hand reads; this pins
    `ratchet.py`, which is what CI reads. Neither substitutes for the other and
    each needs its own content assertion.
    """

    def _gate_output(self, rep, cls):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            golden, reports, baseline = make_fixture(root)
            declare_known_bad(golden, 1, cls=cls)
            with open(os.path.join(reports, "demo-mini.json"), "w") as fh:
                json.dump(rep, fh)
            out, err = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                rc = ratchet.check(golden, reports, baseline)
            return rc, out.getvalue() + err.getvalue()

    def test_the_green_line_puts_the_issue_in_the_bracket_and_the_note_after(self):
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                rc, text = self._gate_output(kb_report(cls=cls), cls)
                self.assertEqual(rc, 0, f"premise broken, this is not a green run:\n{text}")
                want = f"{cls} {kb_label(cls)}  [{KB_ISSUE}]  {KB_NOTE}"
                self.assertIn(
                    want, text,
                    f"the passing gate does not emit the known-bad row in the "
                    f"documented shape.\n  want substring: {want!r}\n  got:\n{text}")
                # Said twice on purpose, because the first assertion would also
                # hold if the bracket happened to contain the issue AND the
                # prose. The bracket is a tracking-id slot.
                self.assertNotIn(
                    f"[{KB_NOTE}]", text,
                    "the note is inside the bracket — that is where a reader looks "
                    "for the tracking id, and prose there degrades the one "
                    "affordance print-on-success exists to give")

    def test_the_stopped_firing_line_reads_class_label_then_the_issue(self):
        """The RED surface has the same exposure, and it was ungraded on TWO
        role pairs rather than one. It carries no note, so there is no
        issue/note pair to confuse — but `class`, `label` and `issue` are three
        interpolations in one line and only their presence was asserted."""
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                rep = kb_report(cls=cls, known_bad_hits=0, known_bad=[],
                                known_bad_silent=[kb_row(cls=cls)])
                rc, text = self._gate_output(rep, cls)
                self.assertEqual(rc, 2, f"premise broken, this is not a red run:\n{text}")
                want = f"known_bad {cls} {kb_label(cls)} STOPPED FIRING [{KB_ISSUE}]"
                self.assertIn(
                    want, text,
                    f"the failing gate does not emit the silent row in the "
                    f"documented shape.\n  want substring: {want!r}\n  got:\n{text}")


class UpdateBaselineCannotLaunderAKnownBad(unittest.TestCase):
    """The forgetting vector, answered by NOT recording the figure.

    Every other figure this gate grades lives in baseline.json, which
    --update-baseline rewrites from observation. A known-bad count recorded
    there would be re-recorded as the new normal the first time a row stopped
    firing — the #6273 shape, where a flag in the generated baseline documented
    a gap and then justified it. So the floor is the DECLARATION in
    expected.json, which no mode of ratchet.py writes to.
    """

    def _build(self, rep, declared=1, cls="relationship"):
        with tempfile.TemporaryDirectory() as root, chdir(root):
            os.environ["QUALITY_RUN_STAMP"] = STAMP
            self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
            make_repo(root)
            golden, reports, baseline = make_fixture(root)
            if declared:
                declare_known_bad(golden, declared, cls=cls)
            with open(os.path.join(reports, "demo-mini.json"), "w") as fh:
                json.dump(rep, fh)
            return ratchet.build(golden, reports, baseline)

    def test_the_written_baseline_records_no_known_bad_figure(self):
        """The property the whole design rests on. If a count appeared here,
        the next --update-baseline would be free to lower it."""
        doc = self._build(kb_report())
        blob = json.dumps(doc)
        self.assertNotIn(
            "known_bad", blob,
            "--update-baseline wrote a known-bad figure into baseline.json; it is "
            "now a number a re-record can weaken, which is the forgetting vector "
            "this design exists to avoid")
        self.assertEqual(
            sorted(doc["fixtures"]["demo-mini"]),
            sorted(["entity_found", "entity_expected", "relationship_found",
                    "relationship_expected", "entity_extracted_total",
                    "relationship_extracted_total"]),
            "the per-fixture entry grew a key")

    def test_it_refuses_to_re_record_while_a_row_is_silent(self):
        """A re-record taken while a declared row is quiet would leave the
        operator believing the tree is in a recordable state. Refuse, on the
        same terms build() already refuses a report with no extracted total."""
        for cls in KB_CLASSES:
            with self.subTest(cls=cls), self.assertRaises(SystemExit) as ctx:
                self._build(kb_report(cls=cls, known_bad_hits=0, known_bad=[],
                                      known_bad_silent=[kb_row(cls=cls)]), cls=cls)
            self.assertIn("STOPPED FIRING", str(ctx.exception))

    def test_it_refuses_a_report_that_predates_the_channel(self):
        """Both classes: the writer reads the same two-key declaration the
        checker does, so an entity-only fixture must refuse a stale report
        exactly as an edge-only one does."""
        for cls in KB_CLASSES:
            rep = kb_report(cls=cls)
            for k in ("known_bad_declared", "known_bad_hits", "known_bad"):
                del rep[k]
            with self.subTest(cls=cls), self.assertRaises(SystemExit) as ctx:
                self._build(rep, cls=cls)
            self.assertIn("predates", str(ctx.exception))


class KnownBadSurvivesTheMerge(unittest.TestCase):
    """The aggregator, driven with run.sh's own bytes.

    Two aggregations, and they are complements of each other:

      * the rows that FIRED are unioned, for #7084's reason — a row seen by
        any run fired, and a median of positive observations discards evidence.
      * the rows that went SILENT are INTERSECTED. Only a row that no run saw
        fire has stopped firing. A union there would red the gate on every
        intermittent producer; a median would be #7084 re-entered on the key
        that grades the other direction.
    """

    @staticmethod
    def _runs(fired, cls="relationship"):
        """One report per bool in `fired`: True the row fired, False it did not."""
        out = []
        for hit in fired:
            rep = run_report(entity_found=10, entity_expected=10)
            rep["known_bad_declared"] = 1
            rep["known_bad_hits"] = 1 if hit else 0
            rep["known_bad"] = [kb_row(cls=cls)] if hit else []
            rep["known_bad_silent"] = [] if hit else [kb_row(cls=cls)]
            out.append(rep)
        return out

    def test_a_row_firing_in_a_minority_of_runs_is_alive(self):
        """The shape the median erased on the forbidden side, read here in the
        opposite direction: two firing runs out of five mean the row is still
        firing, and the fixture must stay green."""
        with tempfile.TemporaryDirectory() as root:
            rc, merged = merge_runs(root, self._runs([False, False, True, False, True]))
        self.assertEqual(rc, 0, "a known_bad row that fired twice failed the fixture")
        self.assertEqual(merged["known_bad_hits"], 1)
        self.assertEqual(merged.get("known_bad_silent", []), [],
                         "a row that fired in 2 of 5 runs was reported as silent")
        self.assertEqual(len(merged["known_bad"]), 1,
                         "the firing row is not named in the merged report")

    def test_a_row_silent_in_every_run_survives_to_the_gate(self):
        with tempfile.TemporaryDirectory() as root:
            rc, merged = merge_runs(root, self._runs([False] * 5))
        self.assertEqual(
            rc, 2,
            "the aggregator passed a fixture whose known_bad row went silent in "
            "every run — strict mode reads this exit code and nothing else")
        self.assertEqual(len(merged["known_bad_silent"]), 1)
        self.assertEqual(merged["known_bad_hits"], 0)

    def test_the_merged_report_then_reds_the_ratchet(self):
        """End to end: the merge program and `check` agree. Two graders, and a
        fix reaching only one of them leaves the other blind. Run for both
        classes, because the declaration `check` compares the merged report
        against is the two-key one."""
        for cls in KB_CLASSES:
            with self.subTest(cls=cls):
                with tempfile.TemporaryDirectory() as root, chdir(root):
                    os.environ["QUALITY_RUN_STAMP"] = STAMP
                    self.addCleanup(os.environ.pop, "QUALITY_RUN_STAMP", None)
                    golden, reports, baseline = make_fixture(root)
                    declare_known_bad(golden, 1, cls=cls)
                    _rc, merged = merge_runs(root, self._runs([False] * 5, cls=cls))
                    with open(os.path.join(reports, "demo-mini.json"), "w") as fh:
                        json.dump(merged, fh)
                    err = io.StringIO()
                    with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                        rc = ratchet.check(golden, reports, baseline)
                self.assertEqual(rc, 2)
                self.assertIn("STOPPED FIRING", err.getvalue())

    def test_a_run_that_predates_the_channel_drops_the_counters(self):
        """One old report among five is enough. Writing 0 instead would be a
        measurement nobody took, and the gate would read it as a fixture with
        nothing to grade."""
        runs = self._runs([True] * 5)
        for k in ("known_bad_declared", "known_bad_hits", "known_bad"):
            del runs[2][k]
        with tempfile.TemporaryDirectory() as root:
            _rc, merged = merge_runs(root, runs)
        self.assertNotIn("known_bad_declared", merged)
        self.assertNotIn("known_bad_hits", merged)

    def test_the_firing_union_is_not_inherited_from_one_run(self):
        """`merged` starts as a copy of reports[-1]. A row that fired only in
        run 0 must still be named, or the passing gate reports a count it
        cannot attribute."""
        runs = self._runs([True, False, False])
        with tempfile.TemporaryDirectory() as root:
            _rc, merged = merge_runs(root, runs)
        self.assertEqual(len(merged.get("known_bad", [])), 1,
                         "the firing row was inherited from the last run and lost")


class ShortCircuitCannotDeclareARowDead(unittest.TestCase):
    """#7056 against #7084's loop. The gate fails a row that is silent in EVERY
    run, so the number of runs taken is load-bearing in this direction too:
    stopping at 3 because recall was stable would let an intermittent producer
    be declared dead on three samples.

    The veto can only make the loop run LONGER, and more runs can only turn a
    false RED into a green — it can never hide a row that fires.
    """

    @staticmethod
    def _stable_runs(n=3, **extra_last):
        runs = [run_report(entity_recall=0.40, relationship_recall=0.50)
                for _ in range(n)]
        runs[-1].update(extra_last)
        return runs

    def test_a_silent_known_bad_row_vetoes_the_short_circuit(self):
        """BOTH CLASSES. The veto reads a serialised row, and a predicate
        narrowed to `class == "relationship"` is ALIVE against an edge-only
        suite. Its blast radius is a false RED rather than a false GREEN — an
        intermittent ENTITY producer would get three samples instead of five
        and could be declared dead — which is why it is graded here rather
        than given a case of its own, but it is graded."""
        for cls in KB_CLASSES:
            with self.subTest(cls=cls), tempfile.TemporaryDirectory() as root:
                self.assertEqual(
                    run_stability(root, self._stable_runs(
                        known_bad_silent=[kb_row(cls=cls)])), "no",
                    f"the loop stopped at 3 runs with a {cls}-class known_bad row "
                    f"already observed silent — the remaining runs are the only "
                    f"evidence that could prove it still fires")

    def test_a_firing_known_bad_row_does_not_veto_it(self):
        """The negative control, and a real scope guard: known-bad rows are the
        steady state once this channel is used, so a veto that fired on a
        FIRING row would make every such fixture run the full five forever."""
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(
                run_stability(root, self._stable_runs(
                    known_bad=[kb_row(cls="entity")], known_bad_hits=1)), "yes",
                "a known_bad row that is firing normally now vetoes the "
                "short-circuit — every fixture using the channel runs 5 runs")


if __name__ == "__main__":
    if subprocess.call(["git", "--version"],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) != 0:
        print("git not on PATH; ratchet self-tests skipped", file=sys.stderr)
        sys.exit(0)
    unittest.main(verbosity=2)
