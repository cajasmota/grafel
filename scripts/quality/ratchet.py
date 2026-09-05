#!/usr/bin/env python3
"""Ratchet gate for the extraction-quality golden fixtures (Refs #6231).

The strict gate (`scripts/quality/run.sh` with no flags) demands 100% must-have
recall on every fixture. That gate has never been green across the full fixture
set — it was true for the five Phase-2 fixtures and rotted silently as fifteen
more were added. Rather than delete the measurement or pretend it passes, this
ratchet records what each fixture *actually* recalls today and fails when that
number moves in either direction:

  * a DROP is a regression — the change under test broke extraction;
  * a RISE is unrecorded progress — the baseline must be updated so the new,
    higher figure becomes the floor and the improvement gets credit.

It also fails on structural drift: a fixture directory with no baseline entry,
a baseline entry with no fixture directory, and a fixture directory carrying no
expected.json.

That last one used to be tolerated when the baseline recorded it as
`expectations_missing` (Refs #6273). Two directories — groovy-grails-mini and
swift-swiftui-mini — sat in that state from the day they were added: never
measured, never mentioned, and re-recorded as fine by every --update-baseline.
Every recall figure quoted from this benchmark had a denominator of 18 while
naming 20. Everything under golden/ is now gated; a corpus that is not meant to
be graded does not belong under golden/.

Freshness: the reports directory is not cleared between runs, so this script
refuses to grade a report that was not written by the current run. run.sh
exports QUALITY_RUN_STAMP and stamps every report with it; a report carrying a
different stamp, or none, is treated as absent. Without this a run in which the
indexer produced nothing at all grades the previous run's JSON and passes.

Known regressions: a recorded floor is a bare number, so the gate cannot tell
one that was always this low from one that was lowered deliberately because a
change traded it for a larger gain elsewhere. The `known_regressions` list in
the baseline records those trades — fixture, metric, the figure before and
after, and the owning issue — and is carried across --update-baseline, restated
when the figure moves, and dropped once the figure recovers. It is printed on
every check, including passing ones. internal/quality/baseline_test.go holds
the matching Go-side set so the annotation cannot be deleted without review.

Known limitation: the ratchet compares must-have *counts*, not identities. An
extractor that stops emitting must-have A while starting to emit must-have B
holds the count and passes. Every report already carries `missing_entities` /
`missing_relationships`; the gate does not yet consult them.

Usage (normally via scripts/quality/run.sh --ratchet / --update-baseline):

    ratchet.py check  <reports-dir> <golden-dir> <baseline.json>
    ratchet.py update <reports-dir> <golden-dir> <baseline.json>
    ratchet.py canon  <baseline.json>

`canon` is the canonicality gate (Refs #6466): it re-serialises the baseline
through the same call `update` writes with and fails if the bytes on disk
differ. It reads no reports and needs no indexer run, so it is driven from
internal/quality/baseline_test.go and gates every PR.

Exit status: 0 pass, 2 ratchet violation, 1 usage/IO error.
"""

import difflib
import json
import os
import subprocess
import sys
from datetime import date

BASELINE_VERSION = 1


def fixture_names(golden_dir):
    return sorted(
        d for d in os.listdir(golden_dir)
        if os.path.isdir(os.path.join(golden_dir, d)) and not d.startswith(".")
    )


def has_expectations(golden_dir, name):
    return os.path.exists(os.path.join(golden_dir, name, "expected.json"))


def run_stamp():
    """The current run's freshness stamp. Absent means the caller did not go
    through run.sh, and we refuse to grade rather than grade something stale."""
    stamp = os.environ.get("QUALITY_RUN_STAMP", "")
    if not stamp:
        raise SystemExit(
            "ratchet: QUALITY_RUN_STAMP is not set — reports cannot be proved "
            "fresh. Invoke via scripts/quality/run.sh (--ratchet / "
            "--update-baseline), not by calling ratchet.py directly."
        )
    return stamp


def load_report(reports_dir, name, stamp):
    """Load a fixture's report, or None if it is absent OR stale.

    Stale is deliberately indistinguishable from absent to callers: a report
    left over from an earlier run must never be graded as a live result.
    """
    path = os.path.join(reports_dir, name + ".json")
    if not os.path.exists(path):
        return None
    try:
        with open(path) as fh:
            rep = json.load(fh)
    except Exception:
        return None
    if str(rep.get("run_stamp", "")) != stamp:
        return None
    return rep


# #6488 arm D. The aggregate extraction totals, recorded per fixture and
# compared in the growth direction only. Both are already computed by
# internal/quality/diff.go and published by internal/quality/report.go; until
# arm D `observed()` dropped them, so no baseline COULD express "extraction
# must not grow" and unnamed over-emission was ungated on every fixture.
#
# The order of this tuple is the order `build()` writes the two keys in, after
# the four recall figures. serialize() does not gate key order (see its
# docstring), so the committed order is pinned separately in
# internal/quality/baseline_test.go.
EXTRACTED_TOTAL_KEYS = ("entity_extracted_total", "relationship_extracted_total")


def observed(rep):
    """The report figures the baseline records, in the order it records them.

    The four recall figures default to 0 when absent, which is the honest
    reading: a report that found nothing found nothing.

    The extracted totals do NOT default. Absent means "this run did not measure
    it", which is a different fact from "it measured zero", and conflating them
    is how the gate would retire itself: an absent key read as 0 is a FALL, and
    a fall is allowed, so a binary that stopped emitting the field would pass
    forever. Callers see the key missing from the returned mapping and say so.

    A key that is PRESENT but not a number (null, a string) raises here rather
    than failing as a gate. That is untested and left as is: the only writer is
    internal/quality/report.go, which emits an int, and inventing a tolerant
    reading of a malformed report would be a second way to guess at a figure
    nobody measured.
    """
    obs = {
        "entity_found": int(rep.get("entity_found", 0)),
        "entity_expected": int(rep.get("entity_expected", 0)),
        "relationship_found": int(rep.get("relationship_found", 0)),
        "relationship_expected": int(rep.get("relationship_expected", 0)),
    }
    for key in EXTRACTED_TOTAL_KEYS:
        if key in rep:
            obs[key] = int(rep[key])
    return obs


# Refs to try, in order, when nothing names the default branch explicitly. The
# remote-tracking refs come first: they are what the merge will actually land
# on, and a local main can be stale or diverged.
DEFAULT_BRANCH_REFS = (
    "refs/remotes/origin/main",
    "refs/remotes/origin/master",
    "refs/heads/main",
    "refs/heads/master",
)


def _git(*args):
    """`git args...` stdout, or "" if git fails for any reason."""
    try:
        return subprocess.check_output(
            ["git", *args], stderr=subprocess.DEVNULL
        ).decode().strip()
    except Exception:
        return ""


def _resolves(ref):
    """True if `ref` names a commit in this checkout."""
    return bool(_git("rev-parse", "--verify", "--quiet", ref + "^{commit}"))


def base_ref_override():
    """The resolvable ref QUALITY_BASE_REF names, or None.

    Kept separate from the search below so callers can tell an OVERRIDE-supplied
    answer from a DISCOVERED one. `ensure_durable_measured_on` needs that
    distinction: until #6570 it re-derived the ref the sha had been computed
    from and asked whether the sha was reachable from it, which an override can
    never fail (Refs #6564, #6568).

    The value is stripped: its ordinary sources — `$(git symbolic-ref ...)` in a
    shell, a YAML block scalar, a hand-indented `env:` line, a CRLF checkout —
    all pad it (Refs #6633).
    """
    override = os.environ.get("QUALITY_BASE_REF", "").strip()
    if override and _resolves(override):
        return override
    return None


def discovered_branch_ref():
    """The default-branch ref this checkout can work out for ITSELF, ignoring
    QUALITY_BASE_REF entirely. None if nothing resolves.
    """
    candidates = []
    head = _git("symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
    if head:
        candidates.append(head)
    candidates.extend(DEFAULT_BRANCH_REFS)
    for ref in candidates:
        if _resolves(ref):
            return ref
    return None


def default_branch_ref():
    """The ref measured_on is anchored to, or None if this checkout has none.

    None is a real state, not a bug: a shallow clone (actions/checkout defaults
    to fetch-depth 1) has no remote-tracking branch ref and no history to walk,
    a detached CI checkout may have neither, and a repo with no remote at all
    has only local branches. Callers degrade; they do not guess.

    QUALITY_BASE_REF overrides the search for a checkout that knows its own
    base and cannot be discovered from refs.
    """
    return base_ref_override() or discovered_branch_ref()


def git_sha():
    """The commit measured_on records: the MERGE-BASE of HEAD with the default
    branch, NOT HEAD (Refs #6564).

    HEAD on a feature branch is not durable, and this repo squash-merges, so
    the commit a re-record stamps is orphaned by the very PR that carries it —
    observed three times, each time surfacing as an unrelated red on main. The
    merge-base is on the default branch already, so it is reachable at write
    time and stays reachable through squash, rebase, amend and force-push. It
    is also the honest provenance answer: "measured on a tree derived from this
    point on main". The cost is that it does not name the branch's own commits,
    which is the correct trade for a field that exists for provenance.

    Degrades to "unknown" when no default branch is available. Never to HEAD:
    HEAD is the value that looks right and is not durable, and a wrong-looking
    "unknown" is caught by review and by internal/quality's shape check, while
    a plausible dangling sha is not.
    """
    ref = default_branch_ref()
    if ref is None:
        return "unknown"
    base = _git("merge-base", "HEAD", ref)
    if not base:
        return "unknown"
    return _git("rev-parse", "--short", base) or "unknown"


def ensure_durable_measured_on(sha):
    """Refuse to write a measured_on that the default branch cannot reach.

    Until #6564 the durability of this field rested on a human reading
    measured_on_note and re-pinning the value by hand after every re-record.
    That instruction was skipped three times in a row, and nothing at write
    time objected — the failure was discovered later, by someone reviewing an
    unrelated change. This is the objection.
    """
    if sha == "unknown":
        print(
            "ratchet: WARNING — no default branch ref in this checkout, so "
            "measured_on is recorded as 'unknown' rather than a sha that "
            "cannot be proved durable. Re-record from a full clone that has "
            "origin/main, or set QUALITY_BASE_REF.",
            file=sys.stderr,
        )
        return
    ref = default_branch_ref()
    if ref is None:
        return
    ok = subprocess.call(
        ["git", "merge-base", "--is-ancestor", sha, ref],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    if ok != 0:
        raise SystemExit(
            f"ratchet: refusing to write measured_on {sha!r} — it is not an "
            f"ancestor of {ref}. A feature-branch HEAD is not durable: squash "
            f"orphans it on merge and internal/quality goes red on main for "
            f"everyone. measured_on must be the merge-base with the default "
            f"branch (Refs #6564)."
        )
    _cross_check_override(sha, ref)


def _cross_check_override(sha, ref):
    """Second opinion when `ref` came from QUALITY_BASE_REF (Refs #6570).

    The check above asks whether `sha` is reachable from the ref `git_sha()`
    computed it from. With an override set, those are the same ref, so the
    question answers itself and the guard cannot object — pointing the override
    at a local feature branch re-enters #6564 with the writer stamping that
    branch's HEAD and this function accepting it.

    So when the ref was supplied by the operator rather than discovered, ask the
    ref the checkout found for itself as well.

    That second ref is only consulted when it shares history with the override.
    A discovered ref with NO merge-base against the operator's declared base is
    not describing this checkout at all — #6627's fixture is exactly that shape,
    an `origin/main` on an orphan root, "a different project's main" — and
    demanding ancestry of it would refuse every durable sha, which is the
    regression #6627 and #6633 pinned.

    Two things this function does NOT cover, recorded rather than rediscovered:

      * `override != ref` is unreachable-false today — `ref` is always
        `default_branch_ref()`, which returns the override whenever one
        resolves. Replacing it with `False` is EQUIVALENT under the current
        suite. It is defensive, not discriminating; do not read it as a
        condition that ever fires, and do not file it as a killable mutant.
      * `discovered is None` is a door this guard structurally cannot close. On
        a checkout with no `origin/HEAD` and no `main`/`master` — a default
        branch called `trunk` or `develop` — there is no second opinion to take
        and the override still vouches for itself exactly as before #6570. That
        is deliberate: it is the same "degrade, do not guess" policy as
        `default_branch_ref()` returning None, and inventing a ref to check
        against would be the guess that policy exists to avoid.
    """
    override = base_ref_override()
    if override is None or override != ref:
        return
    discovered = discovered_branch_ref()
    if discovered is None or discovered == override:
        return
    if not _git("merge-base", override, discovered):
        return
    ok = subprocess.call(
        ["git", "merge-base", "--is-ancestor", sha, discovered],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    if ok != 0:
        raise SystemExit(
            f"ratchet: refusing to write measured_on {sha!r} — QUALITY_BASE_REF "
            f"named {override}, and while {sha!r} is reachable from there it is "
            f"NOT an ancestor of {discovered}, the default branch this checkout "
            f"discovered for itself. An override cannot vouch for itself: point "
            f"it at a feature branch and the writer stamps a commit squash "
            f"orphans on merge, which is #6564 with the guard agreeing. Unset "
            f"QUALITY_BASE_REF, or point it at a ref {discovered} can reach "
            f"(Refs #6564, #6570)."
        )


def load_baseline(baseline_path):
    """The baseline currently on disk, or {} if it is absent or unreadable."""
    try:
        with open(baseline_path) as fh:
            return json.load(fh)
    except Exception:
        return {}


def carry_known_regressions(prior, fixtures):
    """Carry the known_regressions annotations across a re-record.

    The ratchet stores each floor as a bare number, so it cannot tell a figure
    that was always this low from one that was lowered deliberately and is
    tracked. known_regressions is where that difference is written down, and it
    has to survive --update-baseline or the drop it explains turns back into an
    ordinary-looking floor on the next run.

    Two rules keep the block from rotting into stale prose:
      * an entry whose figure has climbed back to its recorded high is DROPPED
        — the regression is over and nothing is left to explain;
      * an entry whose figure moved is restated with the observed value, so the
        block can never disagree with the numbers it annotates. `was` (the
        historic high) is preserved, because that is the fact the bare floor
        loses.
    """
    out = []
    for entry in prior.get("known_regressions", []) or []:
        name, metric = entry.get("fixture"), entry.get("metric")
        base = fixtures.get(name)
        if not isinstance(base, dict) or metric not in base:
            # Fixture deleted, un-gated, or the entry names a metric this
            # baseline does not record: nothing left to annotate.
            continue
        observed_now, was = base[metric], int(entry.get("was", 0))
        if observed_now >= was:
            continue
        out.append({**entry, "now": observed_now})
    return out


def format_known_regressions(entries):
    """Render the known_regressions block for the human-facing gate output."""
    if not entries:
        return []
    lines = [
        "",
        f"  {len(entries)} recorded floor(s) sit BELOW a level extraction once reached:",
    ]
    for e in entries:
        lines.append(
            f"    - {e.get('fixture')}.{e.get('metric')}: "
            f"{e.get('was')} -> {e.get('now')}  [{e.get('issue') or 'UNTRACKED'}]"
        )
    lines.append(
        "  These are tracked regressions, not the natural floor. See "
        "known_regressions in the baseline."
    )
    return lines


def build(golden_dir, reports_dir, baseline_path):
    """Build a fresh baseline document from the reports on disk."""
    stamp = run_stamp()
    prior = load_baseline(baseline_path)
    # Compute and validate the provenance sha BEFORE grading anything: a
    # baseline that cannot name a durable commit must not be written at all.
    measured_on = git_sha()
    ensure_durable_measured_on(measured_on)
    fixtures = {}
    for name in fixture_names(golden_dir):
        if not has_expectations(golden_dir, name):
            # Refs #6273. This used to record {"expectations_missing": True} and
            # carry on, which is how two directories stayed ungraded across every
            # re-record: the flag documented the gap and then check() below
            # accepted the flag as an answer, so the gap justified itself.
            raise SystemExit(
                f"ratchet: fixture {name!r} has no expected.json — refusing to "
                f"record a baseline that excuses it. Give it expectations, or "
                f"move the directory out of internal/quality/golden/."
            )
        rep = load_report(reports_dir, name, stamp)
        if rep is None:
            raise SystemExit(
                f"ratchet: no fresh quality report for fixture {name!r} in "
                f"{reports_dir} — cannot record a baseline from an incomplete run"
            )
        obs = observed(rep)
        # #6488 arm D. Recording a floor of 0 for a total the run never
        # reported is worse than refusing to record at all: 0 is a floor no
        # real total can hold, so every later run would read its own honest
        # figure as growth and the gate would be red until someone re-recorded
        # it away. Refuse loudly instead, and name what is missing.
        for key in EXTRACTED_TOTAL_KEYS:
            if key not in obs:
                raise SystemExit(
                    f"ratchet: the quality report for fixture {name!r} carries no "
                    f"{key} — it was written by a binary that predates the "
                    f"extracted-total gate. Rebuild grafel and re-run "
                    f"`scripts/quality/run.sh --runs 1 --update-baseline`."
                )
        fixtures[name] = obs
    doc = {
        "_comment": (
            "Recorded must-have recall per golden fixture. This is a ratchet, "
            "not a target: recall may not drop below these figures, and any "
            "rise must be recorded here. Regenerate with "
            "`scripts/quality/run.sh --runs 1 --update-baseline`."
        ),
        "version": BASELINE_VERSION,
        "measured_at": date.today().isoformat(),
        "measured_on": measured_on,
        "regenerate": "scripts/quality/run.sh --runs 1 --update-baseline",
        "fixtures": fixtures,
        "known_regressions": carry_known_regressions(prior, fixtures),
    }
    # measured_on_note explains what measured_on is (a BASE commit, not the
    # commit this file lands in). It is prose the generator cannot re-derive, so
    # it is carried rather than rebuilt. This rebuild-from-a-fixed-key-set is
    # what silently deleted the note the first time; carrying it is the fix.
    note = prior.get("measured_on_note")
    if note:
        doc["measured_on_note"] = note
    return doc


def check(golden_dir, reports_dir, baseline_path):
    stamp = run_stamp()
    try:
        with open(baseline_path) as fh:
            baseline = json.load(fh)
    except Exception as exc:
        print(f"ratchet: cannot read baseline {baseline_path}: {exc}", file=sys.stderr)
        return 1

    recorded = baseline.get("fixtures", {})
    known_regressions = baseline.get("known_regressions", []) or []
    present = fixture_names(golden_dir)
    failures = []

    for name in sorted(set(recorded) - set(present)):
        failures.append(
            f"{name}: recorded in baseline but the fixture directory is gone — "
            f"remove the entry (or restore the fixture)"
        )

    for name in present:
        base = recorded.get(name)
        if base is None:
            failures.append(
                f"{name}: fixture directory exists but has no baseline entry — "
                f"run --update-baseline so it is gated"
            )
            continue

        # Everything under golden/ is gated. There is no ungraded category, so
        # neither half of the old two-state dance survives: a missing
        # expected.json is a failure whether or not the baseline expected it to
        # be missing, and `expectations_missing` in the baseline is itself a
        # failure because it is the flag that used to make this pass (#6273).
        if not has_expectations(golden_dir, name):
            failures.append(
                f"{name}: has no expected.json — it produces no report and is "
                f"graded by nothing. Give it expectations, or move the directory "
                f"out of internal/quality/golden/"
            )
            continue

        if base.get("expectations_missing"):
            failures.append(
                f"{name}: baseline records it as having no expectations, but "
                f"expected.json is on disk — re-record with --update-baseline so "
                f"it is gated"
            )
            continue

        rep = load_report(reports_dir, name, stamp)
        if rep is None:
            failures.append(
                f"{name}: no report from THIS run (indexer or harness failure, or "
                f"only a stale report from an earlier run is present) — nothing "
                f"was measured, so nothing can be said to have held"
            )
            continue

        obs = observed(rep)
        forbidden = int(rep.get("forbidden_hits", 0))
        if forbidden > 0:
            failures.append(f"{name}: {forbidden} forbidden relationship hit(s) — always fatal")
        # #6488 arm B. Forbidden ENTITY hits are fatal on the same terms, and
        # are read from their own key: `forbidden_hits` counts edges only, and
        # a report written before this field existed simply reports 0 here.
        #
        # The number counts ROWS THAT FIRED, not offending entities — one row
        # satisfied by two identical offenders is 1. That is deliberate, and it
        # matches forbidden_hits on the edge side, which also appends once per
        # matching row. A count of entities would need the grader to enumerate
        # every candidate rather than resolve one, which is a different (and
        # looser) matcher than the recall path uses, and the two paths agreeing
        # on what "this entity" means is the property arm B is built on. The
        # wording says row so the number is not read as a headcount; the
        # report's forbidden_entities array names each offender.
        forbidden_entities = int(rep.get("forbidden_entity_hits", 0))
        if forbidden_entities > 0:
            failures.append(
                f"{name}: {forbidden_entities} forbidden entity row(s) fired — always fatal"
            )

        for metric in ("entity_found", "relationship_found"):
            want, got = int(base.get(metric, 0)), obs[metric]
            exp_key = metric.replace("_found", "_expected")
            total = obs[exp_key]
            if got < want:
                failures.append(
                    f"{name}: {metric} REGRESSED {want} -> {got} (of {total} must-have)"
                )
            elif got > want:
                failures.append(
                    f"{name}: {metric} IMPROVED {want} -> {got} (of {total} must-have) — "
                    f"record it with --update-baseline so the new floor holds"
                )

        for metric in ("entity_expected", "relationship_expected"):
            want, got = int(base.get(metric, 0)), obs[metric]
            if want != got:
                failures.append(
                    f"{name}: {metric} changed {want} -> {got} — the fixture's "
                    f"expectations were edited; re-record with --update-baseline"
                )

        # #6488 arm D. The aggregate extraction totals, gated in ONE direction.
        #
        # Why not the strict `!=` used just above for the *_expected figures:
        # those move only when a human edits a fixture's expected.json, so any
        # change is an authored change and demanding a re-record costs nothing.
        # The extracted totals move whenever ANY extractor gets better anywhere
        # — a new edge kind, a widened pattern, a framework recognised — so a
        # strict comparison would put this gate red on most legitimate
        # extractor PRs. A gate that is red by default is a gate people
        # re-record without reading, which is worse than no gate.
        #
        # Growth is the direction that carries the defect this exists to catch:
        # over-emission (the pre-#681 Java import placeholders raised
        # java-spring-mini's entity total 61 -> 86 and its relationship total
        # 196 -> 221 with all four recall figures and both forbidden counts
        # unmoved). A FALL is deliberately allowed and silent — it leaves the
        # recorded floor higher than the truth, which weakens this gate until
        # the next re-record but cannot make it lie.
        for metric in EXTRACTED_TOTAL_KEYS:
            if metric not in base:
                failures.append(
                    f"{name}: the baseline records no {metric} — it predates the "
                    f"extracted-total gate, so nothing bounds this fixture's "
                    f"emission; re-record with --update-baseline"
                )
                continue
            if metric not in obs:
                failures.append(
                    f"{name}: the baseline records {metric} {int(base[metric])} but "
                    f"this run's report does not carry the figure at all — nothing "
                    f"measured it, so no total can be said to have held"
                )
                continue
            want, got = int(base[metric]), obs[metric]
            if got > want:
                failures.append(
                    f"{name}: {metric} GREW {want} -> {got} — extraction emitted "
                    f"more than the recorded ceiling. If the new output is wanted, "
                    f"record it with --update-baseline; if it is not, this is "
                    f"over-emission no recall figure can see"
                )

    if failures:
        print("\n==> quality ratchet FAILED\n", file=sys.stderr)
        for f in failures:
            print(f"  - {f}", file=sys.stderr)
        for line in format_known_regressions(known_regressions):
            print(line, file=sys.stderr)
        print(
            f"\n  baseline: {baseline_path}\n"
            f"  regenerate: {baseline.get('regenerate', 'scripts/quality/run.sh --runs 1 --update-baseline')}\n",
            file=sys.stderr,
        )
        return 2

    gated = sum(1 for v in recorded.values() if not v.get("expectations_missing"))
    perfect = sum(
        1 for n, v in recorded.items()
        if not v.get("expectations_missing")
        and v.get("entity_found") == v.get("entity_expected")
        and v.get("relationship_found") == v.get("relationship_expected")
    )
    print(
        f"==> quality ratchet OK — {gated} gated fixtures held their baseline "
        f"({perfect} at 100% must-have recall)"
    )
    # Printed on SUCCESS too, and deliberately so: a tracked regression that is
    # only visible when the gate goes red is invisible exactly when someone is
    # in a position to act on it.
    for line in format_known_regressions(known_regressions):
        print(line)
    return 0


def serialize(doc):
    """The one authority on baseline.json's on-disk encoding (Refs #6466).

    `update` writes through this and `canon` re-derives through this, so the
    gate cannot drift from the writer: there is nothing to keep in sync.

    Encoding properties this fixes, and that `canon` therefore gates:
    ensure_ascii=True (non-ASCII stored as \\uXXXX), two-space indent with the
    (',', ': ') separators that implies, no HTML escaping, and the trailing
    newline.

    Key ORDER is not among them. json.dumps emits the mapping's own insertion
    order, so when `canon` feeds it a document parsed straight back from disk,
    the output echoes the file's existing order and any reordering passes.
    Gating order would require comparing against the order `build()` inserts
    keys in, which is only observable by running the builder over a fresh set
    of reports — out of reach for a check that reads nothing but the file.
    """
    return json.dumps(doc, indent=2) + "\n"


def canon(baseline_path):
    """Fail if baseline_path's bytes are not what `update` would have written.

    baseline.json is generated, but it is also a *test gate*, so unrelated
    encoding churn in it is worse than cosmetic: it hides the deliberate,
    reviewable change that a baseline diff is supposed to be. An agent editing
    three lines once round-tripped the file with ensure_ascii=False and
    rewrote four untouched em-dash escapes; nothing caught it. This is the
    equivalent of `go run ./tools/coverage fmt --check` for
    docs/coverage/registry.json.

    Exit status: 0 canonical, 2 not canonical, 1 unreadable/unparseable.
    """
    try:
        with open(baseline_path, "rb") as fh:
            raw = fh.read()
    except OSError as exc:
        print(f"ratchet canon: cannot read {baseline_path}: {exc}", file=sys.stderr)
        return 1
    try:
        text = raw.decode("utf-8")
        doc = json.loads(text)
    except (UnicodeDecodeError, ValueError) as exc:
        print(f"ratchet canon: {baseline_path} is not valid JSON: {exc}", file=sys.stderr)
        return 1

    want = serialize(doc)
    if want == text:
        print(f"==> quality ratchet: {baseline_path} is canonical")
        return 0

    diff = difflib.unified_diff(
        text.splitlines(keepends=True),
        want.splitlines(keepends=True),
        fromfile=f"{baseline_path} (committed)",
        tofile=f"{baseline_path} (as --update-baseline would write it)",
    )
    sys.stderr.writelines(diff)
    print(
        f"\nratchet canon: {baseline_path} is not what --update-baseline writes. "
        "Regenerate it with `scripts/quality/run.sh --runs 1 --update-baseline` "
        "rather than hand-editing or round-tripping it through another JSON writer.",
        file=sys.stderr,
    )
    return 2


def main(argv):
    mode = argv[1] if len(argv) > 1 else ""
    if mode == "canon":
        if len(argv) != 3:
            print(__doc__, file=sys.stderr)
            return 1
        return canon(argv[2])
    if len(argv) != 5:
        print(__doc__, file=sys.stderr)
        return 1
    reports_dir, golden_dir, baseline_path = argv[2:5]
    if mode == "update":
        doc = build(golden_dir, reports_dir, baseline_path)
        with open(baseline_path, "w") as fh:
            fh.write(serialize(doc))
        print(f"==> quality ratchet: wrote baseline {baseline_path}")
        return 0
    if mode == "check":
        return check(golden_dir, reports_dir, baseline_path)
    print(f"ratchet: unknown mode {mode!r}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
