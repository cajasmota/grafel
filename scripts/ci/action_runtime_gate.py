#!/usr/bin/env python3
"""Gate: every `uses:` pin in .github/workflows/ must resolve to a Node runtime
GitHub has not deprecated (Refs #7253).

THE GAP THIS FILLS
9242ccbea moved all 67 external action pins to their first node24 major,
because the runner was force-migrating node20 actions and the shim's withdrawal
would have stopped every workflow in this repo at once. Nothing prevented that
from being undone one line at a time, and it WAS undone within hours: a branch
rebased directly onto that commit added `uses: actions/checkout@v4` — the only
`@v4` among 21 `@v5`. A human reviewer caught it. No check did, because the
property "every pinned action resolves to a supported Node runtime" is
perfectly machine-checkable and was checked by nothing.

WHY THIS IS NOT "ALL `actions/*` AT MAJOR N"
The migration is not mechanical, and a version rule stated in prose is wrong in
BOTH directions. Measured from upstream `action.yml` at each tag:

  actions/upload-artifact    v5 node20, v6 node24   -> first node24 major 6
  actions/download-artifact  v5 node20, v6 node20,  -> first node24 major 7
                             v7 node24
  actions/github-script      v7 node20, v8 node24   -> first node24 major 8
  softprops/action-gh-release v2 node20, v3 node24  -> first node24 major 3
  msys2/setup-msys2          v1 node12, v2 node24   -> first node24 major 2

upload-artifact and download-artifact moved in lockstep for years and do not
here; msys2/setup-msys2@v2 is ALREADY node24 and must not be "fixed" by a bump
to a v3 that does not exist. So the thing asserted is the PROPERTY —
`runs.using` at the pinned ref is not a deprecated Node runtime — and the only
authority for it is the action's own `action.yml` upstream.

THE TWO HALVES, AND WHY THEY ARE SPLIT
Resolving `runs.using` upstream is one HTTPS read per distinct action. Doing
that on every PR would put network I/O — a real flake source and a real cost —
on the critical path of every push, to re-learn a fact that changes a few times
a year.

  half 1 (this file)  OFFLINE, every PR. ACTION_RUNTIMES below records, per
                      action, the lowest major whose `runs.using` is node24 or
                      newer. Every pin in the tree must be at or above it.
                      No network, sub-second, cannot flake.
  half 2 (periodic)   scripts/ci/action_runtime_refresh.py re-derives
                      ACTION_RUNTIMES from upstream on a schedule and fails on
                      drift. The manifest below is prose, and prose rots — that
                      is the whole lesson of #7243. The periodic job is what
                      stops it rotting SILENTLY.

Half 1 alone would rot. Half 2 alone would be a monthly check on a property
that regresses per-PR. Neither is redundant.

SCOPE, exactly
Every line in .github/workflows/*.yml|*.yaml that, after YAML comment
stripping, matches `uses: <value>`. A value beginning `./` is a local reusable
workflow — it has no upstream runtime, is counted and NOT runtime-checked.
Everything else must parse as `<owner>/<repo>[/<path>]@<ref>` where `<ref>`
carries a major version, and must have a row in ACTION_RUNTIMES.

WHAT IS NOT READ, stated plainly, because an undisclosed blind spot in a tool
built to catch undisclosed blind spots is the worst version of the defect:
actions reached through a composite action's own `action.yml`, or through a
reusable workflow in ANOTHER repository, are invisible here — this gate reads
only the files in this directory. Container jobs (`container:`) and
`docker://` images are not Node actions at all and have no row; a `docker://`
pin would therefore fail as UNKNOWN ACTION, which is the intended fail-closed
direction — it demands a human decision rather than inventing an exemption.

FAIL CLOSED, DELIBERATELY. An action with no row in ACTION_RUNTIMES is a
VIOLATION, not a skip. A skip would be an unbounded, ticket-free exemption
channel: every new action anybody adds would be exempt from the gate on the day
it is added, which is exactly the day the pin is chosen. The cost of failing
closed is that adding an action requires adding a row — and adding that row is
the moment somebody looks up its runtime.

EXIT CODES
This list is EXHAUSTIVE and must be extended by the SAME commit that adds a new
way to fail. A gate that fails for a reason its own header says is impossible is
how gates get muted the first time it happens.

  0  every pin is at or above its action's first supported major, and every
     check below passed.
  1  the gate's verdict — one of:
     a. a pin's major is below its action's first node24 major (the defect);
     b. a `uses:` value names an action with no ACTION_RUNTIMES row;
     c. a `uses:` value has no `@ref`, or a ref from which no major version can
        be read (a branch name, a commit SHA) — unresolvable offline;
     d. an ALLOWED row matched no violation while its workflow is still
        present, or names a workflow outside the pinned set under --manifest;
     e. `--manifest`: the scanned filename set, a per-file `uses:` count, or
        the external-pin total does not match SCAN_MANIFEST/TOTAL_EXTERNAL_PINS;
     f. the scan read fewer workflows or fewer pins than its floors, i.e. it is
        not looking at the tree and its verdict means nothing.
  2  the gate could not run: the workflow directory is missing, or a workflow
     file could not be read.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from dataclasses import dataclass

# ── The runtime floor ────────────────────────────────────────────────────────
# GitHub has deprecated node12, node16 and node20. node24 is current. This is
# the ONE version number in the file that is about Node rather than about an
# action, and it is a floor on a runtime that only moves forward.
MIN_NODE_MAJOR = 24

# ── ACTION_RUNTIMES: the pinned manifest, half 1's whole authority ───────────
# action -> first major version whose upstream `runs.using` is node24 or newer.
#
# Derived from upstream `action.yml` at each tag, NOT from a rule about the
# `actions/` org — see the header for the two pairs that break any such rule.
# `evidence` is the measurement this row asserts: the runtime observed at
# `min_major`, and the runtime at `min_major - 1` (or None when the row's
# minimum is the action's own v1, so there is no predecessor to observe). Both
# halves are re-derived by scripts/ci/action_runtime_refresh.py on a schedule;
# a row that stops matching upstream fails THAT job, which is the only thing
# standing between this dict and the prose rot of #7243.
#
# Re-derive with: python3 scripts/ci/action_runtime_refresh.py --print
ACTION_RUNTIMES: dict[str, dict] = {
    "actions/checkout": {"min_major": 5, "at_min": "node24", "below_min": "node20"},
    "actions/setup-go": {"min_major": 6, "at_min": "node24", "below_min": "node20"},
    "actions/setup-node": {"min_major": 5, "at_min": "node24", "below_min": "node20"},
    "actions/cache": {"min_major": 5, "at_min": "node24", "below_min": "node20"},
    "actions/github-script": {"min_major": 8, "at_min": "node24", "below_min": "node20"},
    # The pair that breaks every lockstep assumption: same org, same release
    # cadence for years, two different first-node24 majors. download-artifact
    # shipped TWO node20 majors (v5 and v6) after upload-artifact's v5.
    "actions/upload-artifact": {"min_major": 6, "at_min": "node24", "below_min": "node20"},
    "actions/download-artifact": {"min_major": 7, "at_min": "node24", "below_min": "node20"},
    "softprops/action-gh-release": {"min_major": 3, "at_min": "node24", "below_min": "node20"},
    # ALREADY node24 at the major this repo pins, and there is no v3. A rule
    # that bumped every pin would break this one; the regression control for
    # this gate is that `msys2/setup-msys2@v2` stays green and unbumped.
    "msys2/setup-msys2": {"min_major": 2, "at_min": "node24", "below_min": "node12"},
}

# ── Floors ───────────────────────────────────────────────────────────────────
# A scan that reads nothing reports "no violations" in exactly the same words as
# a scan that reads everything. These are a backstop for a run pointed at an
# arbitrary directory; they are NOT the instrument — see SCAN_MANIFEST.
MIN_WORKFLOWS = 12
MIN_PINS = 50

# ── The scan manifest: an EXACT pin, not a floor ─────────────────────────────
# A floor with slack is a hole the exact size of the slack, and the pins here
# are concentrated rather than spread: release.yml alone carries 14 of 71.
# MIN_PINS against a live total therefore leaves room for a whole pin-bearing
# file to vanish while the run still prints a well-formed green line.
#
# So `--manifest` (which CI passes) pins the exact filename SET, the exact
# `uses:` count per file — LOCAL reusable-workflow calls included, so a pin
# cannot be hidden by rewriting it into a local call — and the exact total of
# external pins. Re-derive with `--print-manifest`; never edit a number here to
# make a run green without knowing which pin moved.
SCAN_MANIFEST: dict[str, int] = {
    "acceptance.yml": 7,
    "board-hygiene.yml": 1,
    "coverage-docs.yml": 2,
    "cross-platform-compile.yml": 3,
    "grammar-freshness.yml": 3,
    "language-release-calendar.yml": 1,
    "module-hygiene.yml": 2,
    "node-type-gate.yml": 4,
    "perf.yml": 6,
    "pre-merge.yml": 3,
    "quality.yml": 3,
    "release.yml": 14,
    "test.yml": 6,
    "windows-cgo-experiment.yml": 7,
    "windows-installers.yml": 6,
    "windows.yml": 3,
}

# Exact, not a floor. 71 `uses:` lines total, of which 1 is the local
# `./.github/workflows/test.yml` call in release.yml, leaving 70 external pins.
# (9242ccbea's tree had 67; c21353489 added one checkout for the
# workflow-event-gate job, and this change adds two more — one per new job.)
TOTAL_EXTERNAL_PINS = 70

# ── Allow-list ───────────────────────────────────────────────────────────────
# (workflow filename, action, ref) -> justification. Suppresses one pin that is
# below its action's minimum. Each row MUST name a ticket, and a row matching
# nothing is itself a failure: an allow-list is prose, and prose rots.
#
# EMPTY, and that is the point. Every pin in this tree is on a supported
# runtime, so the exemption channel is closed rather than merely documented.
# The controls drive both of its arms with rows injected at runtime, because a
# dict that is empty by design is a shape no mutant on the checked-in tree can
# reach.
ALLOWED: dict[tuple[str, str, str], str] = {}

# `uses:` as a step entry (`- uses: x`), a bare mapping key (`uses: x`), or a
# job-level reusable-workflow call. The value runs to the first whitespace or
# `#`; comment stripping has already run, so a trailing comment is gone.
USES = re.compile(r"^\s*(?:-\s+)?uses:\s*(\S+)\s*$")

# A ref carrying a major version: `v5`, `v2.31.1`, `5`, `5.1`. A commit SHA or
# a branch name deliberately does NOT match — it is unresolvable offline and is
# reported as such rather than guessed at.
REF_MAJOR = re.compile(r"^v?(\d+)(?:\.\d+)*$")


@dataclass
class Pin:
    workflow: str
    line: int
    raw: str  # the whole `uses:` value as written
    action: str | None  # owner/repo[/path], or None for a local call
    ref: str | None


def strip_comment(line: str) -> str:
    """Drop a trailing YAML comment, respecting quotes.

    Load-bearing in one direction only, and it is the FALSE-POSITIVE one: this
    repo's workflow headers discuss pins in prose (board-hygiene.yml's banner
    names `v8` and `v9` of github-script in a comment), and a comment-blind
    scan that read `# was uses: actions/checkout@v4` as a pin would invent a
    violation in a file that has none. The controls plant exactly that.
    """
    out = []
    quote = None
    for i, ch in enumerate(line):
        if quote:
            out.append(ch)
            if ch == quote:
                quote = None
            continue
        if ch in "'\"":
            quote = ch
            out.append(ch)
            continue
        if ch == "#" and (i == 0 or line[i - 1].isspace()):
            break
        out.append(ch)
    return "".join(out)


def split_uses(value: str) -> tuple[str | None, str | None]:
    """`owner/repo/path@ref` -> ("owner/repo/path", "ref"). No `@` -> (v, None)."""
    v = value.strip().strip("'\"")
    if "@" not in v:
        return v, None
    name, _, ref = v.rpartition("@")
    return name, ref


def parse_workflow(path: str) -> list[Pin]:
    with open(path, encoding="utf-8") as fh:
        raw = fh.read().splitlines()
    name = os.path.basename(path)
    pins: list[Pin] = []
    for n, line in enumerate(raw, start=1):
        code = strip_comment(line)
        m = USES.match(code)
        if not m:
            continue
        value = m.group(1).strip("'\"")
        if value.startswith("./") or value.startswith("../"):
            pins.append(Pin(name, n, value, None, None))
            continue
        action, ref = split_uses(value)
        pins.append(Pin(name, n, value, action, ref))
    return pins


def major_of(ref: str | None) -> int | None:
    if ref is None:
        return None
    m = REF_MAJOR.match(ref)
    return int(m.group(1)) if m else None


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--dir", default=".github/workflows", help="workflow directory")
    ap.add_argument("--min-workflows", type=int, default=MIN_WORKFLOWS)
    ap.add_argument("--min-pins", type=int, default=MIN_PINS)
    ap.add_argument(
        "--manifest",
        action="store_true",
        help="enforce SCAN_MANIFEST and TOTAL_EXTERNAL_PINS: the exact set of "
        "workflow files, the exact `uses:` count in each, and the exact number "
        "of external pins. CI runs with this on; it is what makes a vanished "
        "workflow red instead of quietly green.",
    )
    ap.add_argument(
        "--print-manifest",
        action="store_true",
        help="print the manifest this tree would produce, for re-pinning.",
    )
    ap.add_argument(
        "--no-allow-list",
        action="store_true",
        help="ignore ALLOWED; every violation is reported. Used by the controls "
        "to prove the gate still sees what the allow-list covers.",
    )
    args = ap.parse_args()

    if not os.path.isdir(args.dir):
        print(f"action-runtime-gate: not a directory: {args.dir}", file=sys.stderr)
        return 2

    paths = sorted(
        os.path.join(args.dir, f)
        for f in os.listdir(args.dir)
        if f.endswith((".yml", ".yaml"))
    )
    per_file: dict[str, int] = {}
    pins: list[Pin] = []
    for p in paths:
        try:
            got = parse_workflow(p)
        except OSError as exc:
            print(f"action-runtime-gate: cannot read {p}: {exc}", file=sys.stderr)
            return 2
        per_file[os.path.basename(p)] = len(got)
        pins.extend(got)

    external = [p for p in pins if p.action is not None]
    local = [p for p in pins if p.action is None]

    violations: list[str] = []
    allowed_hits: set[tuple[str, str, str]] = set()

    for pin in external:
        key = (pin.workflow, pin.action, pin.ref or "")
        row = ACTION_RUNTIMES.get(pin.action)
        major = major_of(pin.ref)

        if row is None:
            if not args.no_allow_list and key in ALLOWED:
                allowed_hits.add(key)
                print(f"ALLOWED {pin.workflow}:{pin.line} {pin.raw} — {ALLOWED[key]}")
                continue
            violations.append(
                f"\nVIOLATION {pin.workflow}:{pin.line}\n"
                f"  uses: {pin.raw}\n"
                f"  action: {pin.action}\n"
                f"  => no ACTION_RUNTIMES row. This gate fails closed: an action "
                f"with no recorded runtime is not checked, and an unchecked "
                f"action is how the next node20 pin gets in. Look up its "
                f"`runs.using` upstream (`python3 scripts/ci/"
                f"action_runtime_refresh.py --print`) and add a row to "
                f"{os.path.basename(__file__)}."
            )
            continue

        if major is None:
            if not args.no_allow_list and key in ALLOWED:
                allowed_hits.add(key)
                print(f"ALLOWED {pin.workflow}:{pin.line} {pin.raw} — {ALLOWED[key]}")
                continue
            violations.append(
                f"\nVIOLATION {pin.workflow}:{pin.line}\n"
                f"  uses: {pin.raw}\n"
                f"  action: {pin.action}\n"
                f"  ref: {pin.ref!r}\n"
                f"  => no major version can be read from this ref, so its Node "
                f"runtime cannot be resolved offline. A commit SHA or a branch "
                f"name needs a ticket-bearing ALLOWED row in "
                f"{os.path.basename(__file__)} recording which runtime it was "
                f"checked to be on, and when."
            )
            continue

        if major >= row["min_major"]:
            continue

        if not args.no_allow_list and key in ALLOWED:
            allowed_hits.add(key)
            print(f"ALLOWED {pin.workflow}:{pin.line} {pin.raw} — {ALLOWED[key]}")
            continue

        violations.append(
            f"\nVIOLATION {pin.workflow}:{pin.line}\n"
            f"  uses: {pin.raw}\n"
            f"  action: {pin.action}\n"
            f"  pinned major: v{major}; first major on node{MIN_NODE_MAJOR} or "
            f"newer: v{row['min_major']}\n"
            f"  upstream runs.using at v{row['min_major']}: {row['at_min']}; "
            f"at v{row['min_major'] - 1}: {row['below_min']}\n"
            f"  => this pin resolves to a DEPRECATED Node runtime. The runner "
            f"force-migrates such actions today and will stop running them. "
            f"Bump it to at least v{row['min_major']} — note the minimum is per "
            f"ACTION, not per org: upload-artifact reaches node24 at v6 and "
            f"download-artifact only at v7."
        )

    print(
        f"action-runtime-gate: {len(paths)} workflow(s), {len(pins)} `uses:` "
        f"pin(s) ({len(external)} external, {len(local)} local), "
        f"{len(violations)} violation(s), {len(allowed_hits)} allow-listed."
    )
    failed = False

    if args.print_manifest:
        for name in sorted(per_file):
            print(f'    "{name}": {per_file[name]},')
        print(f"    TOTAL_EXTERNAL_PINS = {len(external)}")

    if args.manifest:
        missing = sorted(set(SCAN_MANIFEST) - set(per_file))
        extra = sorted(set(per_file) - set(SCAN_MANIFEST))
        drifted = sorted(
            (n, SCAN_MANIFEST[n], per_file[n])
            for n in set(SCAN_MANIFEST) & set(per_file)
            if SCAN_MANIFEST[n] != per_file[n]
        )
        if missing or extra or drifted:
            print("\nSCAN MANIFEST MISMATCH", file=sys.stderr)
            for n in missing:
                print(
                    f"  MISSING {n}: pinned, but not scanned. A pin-bearing "
                    f"workflow that disappears is exactly what a slack floor "
                    f"cannot see.",
                    file=sys.stderr,
                )
            for n in extra:
                print(
                    f"  UNPINNED {n}: scanned, but not in SCAN_MANIFEST. Add it "
                    f"— and while you are there, check its pins.",
                    file=sys.stderr,
                )
            for n, want, got in drifted:
                print(
                    f"  DRIFT {n}: pinned {want} `uses:` line(s), scanned {got}. "
                    f"Either a pin moved and the manifest must follow, or the "
                    f"parser has gone blind to one.",
                    file=sys.stderr,
                )
            print("  Re-derive with --print-manifest once you know WHICH pin moved.",
                  file=sys.stderr)
            failed = True
        if len(external) != TOTAL_EXTERNAL_PINS:
            print(
                f"\nEXTERNAL PIN TOTAL MISMATCH: pinned {TOTAL_EXTERNAL_PINS}, "
                f"scanned {len(external)}. The per-file counts above include "
                f"local `./.github/workflows/...` calls, so this total is the "
                f"one that says how many pins the runtime check actually "
                f"examined.",
                file=sys.stderr,
            )
            failed = True

    for v in violations:
        print(v, file=sys.stderr)
        failed = True

    if not args.no_allow_list:
        for key, why in ALLOWED.items():
            if key in allowed_hits:
                continue
            if key[0] not in per_file:
                # Outside --manifest the scanned set is whatever the directory
                # happens to hold, so a row about an absent file cannot be
                # judged — the ordinary state when the controls point the gate
                # at a synthetic tree. Under --manifest the filename set is
                # PINNED, so "not there" is a fact about the tree and the row
                # is stale like any other.
                if args.manifest:
                    print(
                        f"\nSTALE ALLOW-LIST ROW {key!r}\n"
                        f"  {key[0]} is not in the pinned workflow set at all. "
                        f"The workflow was deleted or renamed — delete the row.",
                        file=sys.stderr,
                    )
                    failed = True
                else:
                    print(
                        f"NOTE: allow-list row {key!r} not checked — {key[0]} is "
                        f"not in {args.dir}."
                    )
                continue
            print(
                f"\nSTALE ALLOW-LIST ROW {key!r}\n"
                f"  justification: {why}\n"
                f"  {key[0]} is in the tree but no pin there matches this row. "
                f"The pin was bumped, moved or deleted — delete the row.",
                file=sys.stderr,
            )
            failed = True

    if len(paths) < args.min_workflows:
        print(
            f"\nFLOOR: read {len(paths)} workflow(s), floor is "
            f"{args.min_workflows}. The gate is not looking at the tree.",
            file=sys.stderr,
        )
        failed = True
    if len(pins) < args.min_pins:
        print(
            f"\nFLOOR: parsed {len(pins)} `uses:` pin(s), floor is "
            f"{args.min_pins}. The gate is reading files but extracting nothing "
            f"from them.",
            file=sys.stderr,
        )
        failed = True

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
