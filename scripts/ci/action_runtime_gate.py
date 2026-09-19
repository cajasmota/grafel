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

A `uses:` with no value on the same line — the value indented beneath it — is
legal YAML, is NOT read, and is a VIOLATION rather than a skip. See
USES_CONTINUED: a pin written that way was measured passing fully green while
carrying `checkout@v4`, with all three population checks still matching.

SUB-PATH ACTIONS KEY ON THEIR FULL PATH. `actions/cache/restore@v5` is a
different action from `actions/cache@v5` — a separate `action.yml` that can
ship a separate runtime — so it needs its OWN ACTION_RUNTIMES row and does not
inherit `actions/cache`'s. That is deliberate rather than an oversight, and it
means a contributor adding a common, legitimate sub-path pin will get a
violation; the message says which row to add.

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
        be read (a branch name, or a commit SHA — including an all-digit
        abbreviated one of ANY length, which is why a version marker is
        required rather than a length threshold) — unresolvable offline;
     c2. a `uses:` key whose value is not on the same line, so the pin is
        outside everything this gate reads AND outside all three population
        checks;
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
# are concentrated rather than spread: release.yml alone carries 14 of 72.
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
    "grammar-freshness.yml": 4,
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

# Exact, not a floor. 72 `uses:` lines total, of which 1 is the local
# `./.github/workflows/test.yml` call in release.yml, leaving 71 external pins.
# (9242ccbea's tree had 67; c21353489 added one checkout for the
# workflow-event-gate job, and this change adds three more — one per new job:
# action-runtime-gate, action-runtime-refresh and its drift-report sibling.)
#
# This number is not decorative. It is the ONLY check that sees an external pin
# rewritten as a local `./.github/workflows/...` call, which leaves every
# per-file count identical. It is also the check that caught the third checkout
# above, during this very change.
TOTAL_EXTERNAL_PINS = 71

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

# `uses:` with NOTHING after it — the value lives on a following, more-indented
# line:
#
#     - uses:
#         actions/checkout@v4
#
# That is legal YAML and legal Actions, and the pattern above cannot see it.
# Measured, not theorised: planting exactly that beside an existing `@v5` in a
# copy of the real tree produced 0 violations, rc=0, AND left the per-file
# count, the filename set and TOTAL_EXTERNAL_PINS all matching — a reintroduced
# `checkout@v4` sailing through fully green, which is the precise scenario this
# gate exists to stop. It also settles a question the manifest section raises:
# all three population checks CAN stay correct while a pin escapes, so the
# manifest is not a backstop for a parser blind spot.
#
# The value is NOT read off the continuation line. It could be, but a parser
# that half-understands a form is worse than one that refuses it: block
# scalars, anchors and flow mappings all continue a `uses:` key and each would
# need its own handling. So this is an UNPARSEABLE pin — counted, named, and an
# exit-1 violation telling the author to write the value inline. Fail closed.
USES_CONTINUED = re.compile(r"^\s*(?:-\s+)?uses:\s*$")

# A ref names a major version ONLY if it carries a VERSION MARKER: a leading
# `v`, or an embedded dot. `v5`, `v10`, `v2.31.1`, `5.1` — and nothing else.
#
# WHY A MARKER AND NOT A LENGTH. The previous shape was `^v?(\d+)(?:\.\d+)*$`
# with a SHA guard of `^[0-9a-f]{7,40}$` consulted first. That closed the
# 7-character case and left SIX characters wide open: `actions/checkout@123456`
# is a legal pin (GitHub resolves any unambiguous prefix, and
# `git rev-parse --short=6` produces exactly six), and it parsed as major
# 123456 — clearing every floor this gate will ever have, silently, forever.
#
# The probability a 6-char prefix is all-digits is (10/16)^6 ~= 6%, HIGHER than
# the 7-char case the guard was written for, so the threshold landed on the
# wrong side of its own argument. Lowering it to `{4,40}` would only move the
# boundary and would start swallowing legitimate short refs. A length threshold
# is exactly the kind of constant that reads as principled and is arbitrary.
#
# So the inference "all digits, therefore a version" is abandoned outright. A
# commit SHA can never carry a `v` prefix or a dot, so requiring one of those
# removes the ambiguity rather than relocating it, and no length appears in the
# rule at all. The dedicated SHA pattern is GONE with it: once the marker rule
# rejects every unmarked ref, a SHA guard is a redundant second guard that only
# fires where the first already did — two guards that can only fire together
# grade neither.
#
# WHAT IT COSTS: the bare `5` / `12` spelling, which resolves to unresolvable
# rather than to a major. Verified against the tree before choosing: every ref
# here is `v`-prefixed (`v2`, `v2.31.1`, `v3`, `v5`, `v6`, `v7`, `v8`) and
# ALLOWED is empty, so nothing is lost today. (An earlier control's docstring
# asserted bare `5` was "a spelling the tree uses". Nothing had checked, and it
# was false — the same unverified-prose defect this whole gate exists to catch.)
# Should somebody want it back, the fix is an explicit `ALLOWED` row, not a
# looser regex.
REF_MAJOR_V = re.compile(r"^v(\d+)(?:\.\d+)*$")
REF_MAJOR_DOTTED = re.compile(r"^(\d+)(?:\.\d+)+$")


@dataclass
class Pin:
    workflow: str
    line: int
    raw: str  # the whole `uses:` value as written
    action: str | None  # owner/repo[/path], or None for a local call or an
    # unparseable `uses:`
    ref: str | None
    unparseable: bool = False  # `uses:` whose value is not on the same line


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
            if USES_CONTINUED.match(code):
                pins.append(
                    Pin(name, n, "<value not on the `uses:` line>", None, None,
                        unparseable=True)
                )
            continue
        value = m.group(1).strip("'\"")
        if value.startswith("./") or value.startswith("../"):
            pins.append(Pin(name, n, value, None, None))
            continue
        action, ref = split_uses(value)
        pins.append(Pin(name, n, value, action, ref))
    return pins


def major_of(ref: str | None) -> int | None:
    """The major version a ref names, or None when it names none.

    A VERSION MARKER is required — see REF_MAJOR_V / REF_MAJOR_DOTTED. An
    unmarked all-digit ref (`123456`, `1234567`) is a plausible abbreviated
    commit SHA, not a major, and returning None routes it to the same
    "unresolvable offline" violation a branch name gets. That is the
    fail-closed direction, and it is reached without any length threshold.
    """
    if ref is None:
        return None
    m = REF_MAJOR_V.match(ref) or REF_MAJOR_DOTTED.match(ref)
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
    local = [p for p in pins if p.action is None and not p.unparseable]
    unparseable = [p for p in pins if p.unparseable]

    violations: list[str] = []
    allowed_hits: set[tuple[str, str, str]] = set()

    # A `uses:` whose value is on a following line is counted (so the per-file
    # manifest number still adds up) but cannot be resolved, and is a violation
    # rather than a skip. It is NOT allow-listable: unlike a SHA pin there is
    # no fact to record about it, only a line to rewrite.
    for pin in unparseable:
        violations.append(
            f"\nVIOLATION {pin.workflow}:{pin.line}\n"
            f"  uses: {pin.raw}\n"
            f"  => the `uses:` value is on a following line. That is legal "
            f"YAML, and this gate deliberately does not read it: a pin written "
            f"this way was measured to pass fully green — 0 violations, and the "
            f"per-file count, the filename set and the external total all still "
            f"matching — while carrying `actions/checkout@v4`. Write the value "
            f"on the `uses:` line itself."
        )

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
                f"{os.path.basename(__file__)}.\n"
                f"  NOTE: a sub-path action keys on its FULL path, so "
                f"`actions/cache/restore` needs its own row even though "
                f"`actions/cache` has one — they are separate action files and "
                f"can ship separate runtimes."
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
                f"runtime cannot be resolved offline. A major is recognised "
                f"only with a version marker — a leading `v` or an embedded "
                f"dot — because an unmarked run of digits is a plausible "
                f"abbreviated commit SHA at every length. A commit SHA or a "
                f"branch name needs a ticket-bearing ALLOWED row in "
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
        f"pin(s) ({len(external)} external, {len(local)} local, "
        f"{len(unparseable)} unparseable), "
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
