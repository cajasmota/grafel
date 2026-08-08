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


def load_report(reports_dir, name):
    path = os.path.join(reports_dir, name + ".json")
    if not os.path.exists(path):
        return None
    try:
        with open(path) as fh:
            return json.load(fh)
    except Exception:
        return None


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


def build(golden_dir, reports_dir):
    """Build a fresh baseline document from the reports on disk."""
    fixtures = {}
    for name in fixture_names(golden_dir):
        if not has_expectations(golden_dir, name):
            fixtures[name] = {
                "expectations_missing": True,
                "note": "fixture has src/ but no expected.json — it is never "
                        "evaluated. Not a recall failure; see #6231.",
            }
            continue
        rep = load_report(reports_dir, name)
        if rep is None:
            raise SystemExit(
                f"ratchet: no quality report for fixture {name!r} in {reports_dir} — "
                "cannot record a baseline from an incomplete run"
            )
        fixtures[name] = observed(rep)
    return {
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
    }


def check(golden_dir, reports_dir, baseline_path):
    try:
        with open(baseline_path) as fh:
            baseline = json.load(fh)
    except Exception as exc:
        print(f"ratchet: cannot read baseline {baseline_path}: {exc}", file=sys.stderr)
        return 1

    recorded = baseline.get("fixtures", {})
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

        rep = load_report(reports_dir, name)
        if rep is None:
            failures.append(
                f"{name}: no quality report produced (indexer or harness failure)"
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
    return 0


def main(argv):
    if len(argv) != 5:
        print(__doc__, file=sys.stderr)
        return 1
    mode, reports_dir, golden_dir, baseline_path = argv[1:5]
    if mode == "update":
        doc = build(golden_dir, reports_dir)
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
