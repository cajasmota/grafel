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
a baseline entry with no fixture directory, and a fixture that carries no
expected.json without being explicitly recorded as such.

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

Exit status: 0 pass, 2 ratchet violation, 1 usage/IO error.
"""

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


def observed(rep):
    return {
        "entity_found": int(rep.get("entity_found", 0)),
        "entity_expected": int(rep.get("entity_expected", 0)),
        "relationship_found": int(rep.get("relationship_found", 0)),
        "relationship_expected": int(rep.get("relationship_expected", 0)),
    }


def git_sha():
    try:
        return subprocess.check_output(
            ["git", "rev-parse", "--short", "HEAD"], stderr=subprocess.DEVNULL
        ).decode().strip()
    except Exception:
        return "unknown"


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
    fixtures = {}
    for name in fixture_names(golden_dir):
        if not has_expectations(golden_dir, name):
            fixtures[name] = {
                "expectations_missing": True,
                "note": "fixture has src/ but no expected.json — it is never "
                        "evaluated. Not a recall failure; see #6231.",
            }
            continue
        rep = load_report(reports_dir, name, stamp)
        if rep is None:
            raise SystemExit(
                f"ratchet: no fresh quality report for fixture {name!r} in "
                f"{reports_dir} — cannot record a baseline from an incomplete run"
            )
        fixtures[name] = observed(rep)
    doc = {
        "_comment": (
            "Recorded must-have recall per golden fixture. This is a ratchet, "
            "not a target: recall may not drop below these figures, and any "
            "rise must be recorded here. Regenerate with "
            "`scripts/quality/run.sh --runs 1 --update-baseline`."
        ),
        "version": BASELINE_VERSION,
        "measured_at": date.today().isoformat(),
        "measured_on": git_sha(),
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

        if not has_expectations(golden_dir, name):
            if not base.get("expectations_missing"):
                failures.append(
                    f"{name}: has no expected.json but the baseline expects it to — "
                    f"the expectations file was deleted"
                )
            continue

        if base.get("expectations_missing"):
            failures.append(
                f"{name}: now HAS an expected.json but the baseline records it as "
                f"missing — run --update-baseline to start gating it"
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


def main(argv):
    if len(argv) != 5:
        print(__doc__, file=sys.stderr)
        return 1
    mode, reports_dir, golden_dir, baseline_path = argv[1:5]
    if mode == "update":
        doc = build(golden_dir, reports_dir, baseline_path)
        with open(baseline_path, "w") as fh:
            json.dump(doc, fh, indent=2)
            fh.write("\n")
        print(f"==> quality ratchet: wrote baseline {baseline_path}")
        return 0
    if mode == "check":
        return check(golden_dir, reports_dir, baseline_path)
    print(f"ratchet: unknown mode {mode!r}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
