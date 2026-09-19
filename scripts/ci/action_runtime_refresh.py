#!/usr/bin/env python3
"""Half 2 of #7253: re-derive ACTION_RUNTIMES from upstream and fail on drift.

WHY THIS EXISTS
scripts/ci/action_runtime_gate.py enforces, offline and on every PR, that each
`uses:` pin sits at or above its action's first node24 major. That floor lives
in a hand-written dict. A hand-written dict is prose, and prose rots exactly the
way the pins rotted in #7243 — silently, in the green direction. This job is the
thing that stops it rotting silently: it is the ONLY place the manifest is
compared against the authority it claims to summarise.

It is periodic, not per-PR, because it is network I/O. Ten HTTPS reads on the
critical path of every push is a real cost and a real flake source, to re-learn
a fact that changes a few times a year. Per-PR gets the offline floor; the
schedule gets the truth.

WHAT IT CHECKS, both directions
For every row `action -> {min_major, at_min, below_min}` in ACTION_RUNTIMES:

  1. `runs.using` at `v<min_major>` is node24 or newer, and equals `at_min`.
     If this fails the manifest is TOO LOW: the gate has been passing pins on a
     deprecated runtime. This is the direction that matters.
  2. `v<min_major - 1>` is absent, or its `runs.using` is a deprecated Node
     runtime, and equals `below_min`. If this fails the manifest is TOO HIGH:
     the gate has been rejecting legitimate pins. Loud and self-correcting, but
     still drift.

and, independently of the manifest, for every distinct (action, ref) actually
pinned in .github/workflows/: `runs.using` at that exact ref is node24 or newer.
That last check is the property the issue asks for, asserted against the refs
the tree really carries rather than against a summary of them — so a manifest
row that is right about `v6` cannot vouch for a `v6.0.0-beta` pin nobody
checked.

EXIT CODES. Exhaustive; extend it in the same commit as any new failure path.
  0  every row matches upstream and every pinned ref is on a supported runtime.
  1  DRIFT: a manifest row disagrees with upstream, or a pinned ref resolves to
     a deprecated (or non-Node) runtime. A fact about this repo.
  2  the tool could not run AND had established no drift before failing: an
     upstream read failed for a reason that is not a 404, the response was not
     parseable, or an `action.yml` had no `runs.using` at all. Kept DISTINCT
     from 1 on purpose — a rate-limited run and a rotted manifest must not look
     the same, or the first teaches everyone to ignore the second.

     A run that had ALREADY found drift when the failure hit exits 1 and prints
     what it found. Anything else lets one 503 erase a real finding: rows are
     walked in sorted order, so a late failure used to swallow every earlier
     one silently. "Distinct" has to mean the transient does not destroy the
     evidence, not merely that it carries a different number.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import re
import sys
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

from action_runtime_gate import (  # noqa: E402
    ACTION_RUNTIMES,
    MIN_NODE_MAJOR,
    major_of,
    parse_workflow,
)

API = "https://api.github.com/repos/{repo}/contents/{path}?ref={ref}"
NODE_USING = re.compile(r"^node([0-9]+)$")  # ASCII, as above
USING_LINE = re.compile(r"^\s+using:\s*['\"]?([A-Za-z0-9_.-]+)['\"]?\s*$")


# ── DRIFT_CAUSES: the table this script and its control BOTH read ───────────
# Same contract as FAILURE_CAUSES in action_runtime_gate.py, and the same
# reason: the control that checks the host workflow's exhaustive failure list
# was SAMPLING it with hand-written phrases and fell behind three times — most
# recently on the cut-short semantic THIS script introduced, whose entire
# documenting paragraph could be deleted with the suite still green.
#
# cause -> (emitted label, phrase that must appear in grammar-freshness.yml)
# Every exit-1 finding is raised through `record(<cause>, ...)`, which renders
# the label and rejects an unknown cause.
# cause -> (emitted label, every phrase its header clause must contain). The
# phrase side is a tuple for the same reason as FAILURE_CAUSES: a clause whose
# detail is the whole point must have that detail pinned, not just its topic
# sentence.
DRIFT_CAUSES: dict[str, tuple[str, tuple[str, ...]]] = {
    "manifest_too_low": (
        "DRIFT (manifest TOO LOW)",
        ("minimum major is TOO LOW", "passing pins it should have caught"),
    ),
    "manifest_too_high": (
        "DRIFT (manifest TOO HIGH)",
        ("minimum major is TOO HIGH", "rejecting legitimate pins"),
    ),
    "evidence_stale_at_min": (
        "DRIFT (recorded evidence stale)",
        ("recorded evidence (the runtime observed at the minimum major",),
    ),
    "evidence_stale_below_min": (
        "DRIFT (recorded evidence stale)",
        ("at the one below) no longer matches upstream",),
    ),
    "pinned_ref_deprecated": (
        "VIOLATION (pinned ref on a deprecated runtime)",
        ("deprecated or non-Node runtime",),
    ),
    "pinned_ref_gone": (
        "DRIFT (pinned ref is gone)",
        ("or its tag no longer exists",),
    ),
    "cut_short_with_drift": (
        "CUT SHORT",
        (
            "ALREADY found drift when the failure hit exits 1",
            # the reason exit 1 outranks exit 2 here, which is the finding
            "does not DESTROY the evidence",
        ),
    ),
}


class Unavailable(Exception):
    """The tool could not run — exit 2, never confused with drift."""


def fetch(url: str) -> bytes | None:
    """GET, returning None for a 404 (a tag or file that does not exist)."""
    req = urllib.request.Request(url, headers={"Accept": "application/vnd.github+json"})
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.read()
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return None
        raise Unavailable(f"{url}: HTTP {exc.code} {exc.reason}") from exc
    except OSError as exc:
        raise Unavailable(f"{url}: {exc}") from exc


def runs_using(action: str, ref: str) -> str | None:
    """`runs.using` for `owner/repo[/path]` at `ref`, or None if the ref has no
    action file (the tag does not exist).

    Text, not YAML: this is stdlib-only by contract — no pip, no PyYAML — and
    the `runs:` block's `using:` is a single unambiguous line in every action
    file GitHub accepts. Both `action.yml` and `action.yaml` are tried, because
    GitHub accepts either and an action that uses the second would otherwise
    read as a missing tag.
    """
    parts = action.split("/")
    repo = "/".join(parts[:2])
    subdir = "/".join(parts[2:])
    found_file = False
    for fname in ("action.yml", "action.yaml"):
        path = f"{subdir}/{fname}" if subdir else fname
        body = fetch(API.format(repo=repo, path=path, ref=ref))
        if body is None:
            continue
        found_file = True
        try:
            content = base64.b64decode(json.loads(body)["content"]).decode("utf-8")
        except (ValueError, KeyError, UnicodeDecodeError) as exc:
            raise Unavailable(f"{action}@{ref}: unparseable API response: {exc}")
        for line in content.splitlines():
            m = USING_LINE.match(line)
            if m:
                return m.group(1)
        raise Unavailable(
            f"{action}@{ref}: {fname} has no `runs.using:` line. The file "
            f"exists but the authority this tool rests on is not in it."
        )
    if found_file:
        raise Unavailable(f"{action}@{ref}: action file found but not read")
    return None


def supported(using: str | None) -> bool:
    if using is None:
        return False
    m = NODE_USING.match(using)
    return bool(m) and int(m.group(1)) >= MIN_NODE_MAJOR


DRIFT_MARKER = "grafel-drift:yes"
UNDOCUMENTED_DRIFT = "undocumented drift cause"


def record(drift: list[str], cause: str, body: str) -> None:
    """The only way a drift finding is created.

    Rejects an unknown cause rather than accepting it, so a typo crashes at the
    finding site instead of producing an exit-1 reason no header documents.
    """
    # The explicit check must carry its own MESSAGE, and the control must
    # assert that message. Without it this guard is ungradeable: the very next
    # line indexes DRIFT_CAUSES and raises KeyError by itself, so deleting the
    # check left `assertRaises(KeyError)` passing and the mutant ALIVE — two
    # guards that can only fire together grade neither. The message is the only
    # thing that distinguishes "rejected on purpose" from "crashed anyway".
    if cause not in DRIFT_CAUSES:
        raise KeyError(
            f"{UNDOCUMENTED_DRIFT} {cause!r}: add it to DRIFT_CAUSES and to "
            f"grammar-freshness.yml's exhaustive list"
        )
    label = DRIFT_CAUSES[cause][0]
    drift.append(f"\n{label} {body}")


def render_markdown(
    drift: list[str], derived: dict[str, dict], cut_short: str | None
) -> str:
    """The tracking-issue body. Rendering is deliberately separate from the
    verdict: this returns text, never an exit code.

    WHY THE ISSUE EXISTS AT ALL. A monthly cron that only goes red is a
    notification nobody opens — and the message it goes red with, after a
    network blip, reads transient. The manifest rotting is the one failure this
    half exists to catch, so it gets the same treatment grammars.lock already
    gets in this workflow: one recurring, idempotent tracking issue.
    """
    out = []
    if drift:
        out.append(f"<!-- {DRIFT_MARKER} -->")
        out.append("")
        out.append(
            "`ACTION_RUNTIMES` in `scripts/ci/action_runtime_gate.py` no longer "
            "describes upstream. That dict is the offline gate's entire "
            "authority, so every PR since the drift began has been graded "
            "against a stale floor."
        )
        if cut_short:
            out.append("")
            out.append(
                f"> This run was CUT SHORT (`{cut_short}`). The findings below "
                f"were already established and are real; there may be more."
            )
        out.append("")
        out.append(f"## {len(drift)} finding(s)")
        for d in drift:
            out.append("")
            out.append("```")
            out.append(d.strip())
            out.append("```")
    else:
        out.append("No drift: every `ACTION_RUNTIMES` row matches upstream.")
    out.append("")
    out.append("## Rows as upstream reports them now")
    out.append("")
    out.append("| action | min_major | runs.using at min | at min-1 |")
    out.append("|---|---|---|---|")
    for action in sorted(derived):
        d = derived[action]
        out.append(
            f"| `{action}` | v{d['min_major']} | `{d['at_min']}` | "
            f"`{d['below_min']}` |"
        )
    out.append("")
    out.append(
        "Re-derive with `python3 scripts/ci/action_runtime_refresh.py --print`."
    )
    return "\n".join(out) + "\n"


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--dir", default=".github/workflows")
    ap.add_argument(
        "--print",
        dest="do_print",
        action="store_true",
        help="print the ACTION_RUNTIMES rows this upstream would produce, for "
        "re-pinning action_runtime_gate.py after a legitimate drift.",
    )
    ap.add_argument(
        "--markdown",
        action="store_true",
        help="RENDER ONLY: write a markdown drift report to stdout and exit 0 "
        "whatever it finds. This is a reporting mode for the tracking-issue "
        "job, NOT a verdict — the job that decides is the one run without this "
        "flag, and a control asserts the gate job does not pass it. A body "
        "containing the `grafel-drift:yes` marker line is what tells the "
        "workflow there is something to file.",
    )
    args = ap.parse_args()

    drift: list[str] = []

    # ── 0: PRECONDITIONS, before any network and outside the drift try ───────
    # Reading the tree is local and cannot flake, so it is settled first. It
    # also must not interact with the exit-1/exit-2 rule below: "the directory
    # yielded no pins" means the tool was pointed at the wrong place, and that
    # is an exit 2 whatever else was found — unlike a mid-run upstream failure,
    # it casts doubt on the run's premise rather than merely truncating it.
    # (Found by the control for that rule: once drift could outrank Unavailable,
    # an empty directory started reporting drift-exit-1 instead.)
    try:
        if not os.path.isdir(args.dir):
            raise Unavailable(f"not a directory: {args.dir}")
        pinned: set[tuple[str, str]] = set()
        for f in sorted(os.listdir(args.dir)):
            if not f.endswith((".yml", ".yaml")):
                continue
            for pin in parse_workflow(os.path.join(args.dir, f)):
                if pin.action is not None and pin.ref:
                    pinned.add((pin.action, pin.ref))
        if not pinned:
            raise Unavailable(
                f"no external pins found in {args.dir}; this job would report "
                f"green having checked nothing."
            )
    except Unavailable as exc:
        print(f"action-runtime-refresh: cannot run: {exc}", file=sys.stderr)
        return 2

    # ── 1 & 2: every manifest row, both directions ───────────────────────────
    derived: dict[str, dict] = {}
    try:
        for action in sorted(ACTION_RUNTIMES):
            row = ACTION_RUNTIMES[action]
            m = row["min_major"]
            at_min = runs_using(action, f"v{m}")
            below = runs_using(action, f"v{m - 1}") if m > 1 else None
            derived[action] = {"min_major": m, "at_min": at_min, "below_min": below}

            if not supported(at_min):
                record(
                    drift,
                    "manifest_too_low",
                    f"{action}\n"
                    f"  ACTION_RUNTIMES says v{m} is the first supported major, "
                    f"but upstream `runs.using` at v{m} is {at_min!r}.\n"
                    f"  => the offline gate has been passing pins on a "
                    f"deprecated runtime. This is the direction that matters. "
                    f"Find the real first supported major and raise the row."
                )
            elif at_min != row["at_min"]:
                record(
                    drift,
                    "evidence_stale_at_min",
                    f"{action}\n"
                    f"  row records `at_min={row['at_min']!r}` at v{m}; upstream "
                    f"now says {at_min!r}. Still supported, but the row's "
                    f"evidence no longer describes upstream — update it."
                )

            if below is not None and supported(below):
                record(
                    drift,
                    "manifest_too_high",
                    f"{action}\n"
                    f"  ACTION_RUNTIMES says v{m} is the first supported major, "
                    f"but v{m - 1} is already {below!r}.\n"
                    f"  => the offline gate is rejecting legitimate pins. Lower "
                    f"the row (and check further down: v{m - 2} may qualify too)."
                )
            elif below != row["below_min"]:
                record(
                    drift,
                    "evidence_stale_below_min",
                    f"{action}\n"
                    f"  row records `below_min={row['below_min']!r}` at v{m - 1}; "
                    f"upstream now says {below!r}. Update the row."
                )

        # ── 3: every distinct ref the tree actually pins ─────────────────────
        for action, ref in sorted(pinned):
            using = runs_using(action, ref)
            if using is None:
                record(
                    drift,
                    "pinned_ref_gone",
                    f"{action}@{ref}\n"
                    f"  no action file at that ref upstream — the tag was moved "
                    f"or deleted. The offline gate cannot see this."
                )
                continue
            if not supported(using):
                record(
                    drift,
                    "pinned_ref_deprecated",
                    f"{action}@{ref}\n"
                    f"  upstream `runs.using` is {using!r}, and node"
                    f"{MIN_NODE_MAJOR} is the floor.\n"
                    f"  => the tree carries a pin the offline gate accepted. "
                    f"Either its major floor is wrong, or this exact ref is not "
                    f"what its major implies."
                )
    except Unavailable as exc:
        # DRIFT ALREADY FOUND IS NOT ERASED BY A LATER BLIP. Rows are walked in
        # sorted order, so a 503 on `msys2` used to swallow a real `TOO LOW` on
        # `actions/cache` or `actions/checkout` found seconds earlier: the whole
        # derivation sat in this one `try`, and the early `return 2` printed
        # nothing. Measured: `checkout@v5 -> node20` plus a later row failing
        # gave exit 2 with `TOO LOW` absent from the output entirely. A
        # rate-limited run was supposed to merely LOOK different from a rotted
        # manifest; as written it destroyed the evidence.
        #
        # So: the partial findings are printed either way, and a run that found
        # drift before the failure exits 1, because the drift is an established
        # fact about this repository and the failure is not a reason to doubt
        # it. Only a run that found NOTHING before failing exits 2 — that is the
        # case where the verdict really is unknown.
        print(f"action-runtime-refresh: cannot run: {exc}", file=sys.stderr)
        if args.markdown:
            # Render-only mode never decides. A cut-short run still renders
            # whatever it established, and says so in the body.
            sys.stdout.write(render_markdown(drift, derived, str(exc)))
            return 0
        if not drift:
            return 2
        print(
            f"action-runtime-refresh: the run was "
            f"{DRIFT_CAUSES['cut_short_with_drift'][0]} by the error above, "
            f"but {len(drift)} drift finding(s) were already established and "
            f"are reported below. Exiting 1, not 2: what follows is a fact "
            f"about this repository, not a fact about the network. Re-run to "
            f"see whether there is more.",
            file=sys.stderr,
        )
        for d in drift:
            print(d, file=sys.stderr)
        return 1

    if args.markdown:
        # Before the summary line below, which would otherwise land in the body.
        sys.stdout.write(render_markdown(drift, derived, None))
        return 0

    print(
        f"action-runtime-refresh: {len(ACTION_RUNTIMES)} manifest row(s) and "
        f"{len(pinned)} distinct pinned ref(s) checked against upstream; "
        f"{len(drift)} drift finding(s)."
    )
    if args.do_print:
        for action in sorted(derived):
            d = derived[action]
            print(
                f'    "{action}": {{"min_major": {d["min_major"]}, '
                f'"at_min": {d["at_min"]!r}, "below_min": {d["below_min"]!r}}},'
            )
    for d in drift:
        print(d, file=sys.stderr)
    if drift:
        print(
            "\nACTION_RUNTIMES in scripts/ci/action_runtime_gate.py no longer "
            "describes upstream. That dict is the offline gate's entire "
            "authority, so this is the manifest rotting — the failure mode this "
            "job exists to make loud. Re-derive with --print.",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
