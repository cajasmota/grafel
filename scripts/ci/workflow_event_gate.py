#!/usr/bin/env python3
"""Gate: an `if:` guard may only compare `github.event_name` to an event its own
workflow can actually receive (Refs #7250).

THE GAP THIS FILLS
`acceptance.yml` carries a job, `pr-linux`, guarded by
`if: github.event_name == 'pull_request'`. That workflow's `on:` block is
`workflow_run` + `workflow_dispatch`. The guard therefore matches under no
trigger the workflow has, and the job — checkout, setup-go, build, --version,
selftest — has been unreachable since the `pull_request` trigger was removed.
Nothing failed. Nothing reported. Dead CI configuration reads as coverage from
every direction except a hand audit of the `on:` block, which is how three
separate stale-CI defects (#7244, #7247, #7250) were each found by somebody who
happened to look.

This is the same defect shape tools/node-type-gate exists for, one domain over:
a string literal compared against a set of valid names that is itself machine
readable, where a literal outside that set is a SILENT NO-OP rather than an
error. Both halves here are machine readable — the `on:` block and the `if:`
guard. The prose around them is the only hand-maintained part, and prose is
what #7250 found rotted. So this gate checks the machine-readable half only; it
does NOT attempt to validate prose, which is not mechanically decidable and
would produce false positives by the file.

SCOPE, exactly
For every workflow in .github/workflows/, every `if:` guard — job level and
step level — that compares `github.event_name` to a string literal with `==` or
`!=` must name an event in that workflow's EFFECTIVE event set. Nothing else is
checked. A guard that is merely never *satisfied* for other reasons (a label
that is never applied, a ref that never matches) is out of scope: those are
facts about the world, not about the file.

EFFECTIVE EVENT SET — why it is not just the `on:` keys
Two propagation rules, both of which the naive "literal must appear in `on:`"
rule gets wrong, and the second of which would have made this gate fire falsely
on test.yml the day it was written:

  1. `workflow_call`. A called workflow sees the CALLER's event in
     `github.event_name`, not `workflow_call`. test.yml subscribes to
     workflow_call / workflow_dispatch / pull_request_target / pull_request and
     guards on `github.event_name == 'push'` — correctly, because release.yml
     (`on: push: tags: ['v*']`) calls it via `uses: ./.github/workflows/
     test.yml`. So a workflow_call-subscribed workflow inherits the effective
     set of every local workflow that calls it, resolved transitively.
  2. `workflow_run` does NOT propagate. A workflow_run-triggered run sees
     `github.event_name == 'workflow_run'`; the upstream event is available at
     `github.event.workflow_run.event`, a different expression this gate does
     not look at. acceptance.yml reads exactly that, correctly.

A workflow that subscribes to `workflow_call` and is called by NOTHING local
cannot have its guards resolved (a caller could live in another repository), so
its guards are reported as unresolvable and counted, never silently skipped.

ALLOW-LIST
An entry suppresses one (workflow, job/step, event) violation and must carry a
ticket. A row that matches nothing is itself a failure: an allow-list is prose,
and prose rots — this one cannot rot silently, because deleting the job it
covers turns the gate red until the row goes too.

EXIT CODES
  0  no unallowed violations, and the scan met its floors.
  1  the gate's verdict: at least one guard names an event its workflow cannot
     receive and no allow-list row covers it; or an allow-list row matched
     nothing; or the scan read fewer workflows/guards than its floor, i.e. it
     is not looking at the tree and its verdict means nothing.
  2  the gate could not run: a workflow file could not be read, or a `jobs:`
     block could not be located in a file that has one.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from dataclasses import dataclass, field

# ── Floors ───────────────────────────────────────────────────────────────────
# A scan that reads nothing reports "no violations" in exactly the same words as
# a scan that reads everything. These are derived from the tree at the time of
# writing — re-derive them from the tree rather than trusting this line — and
# set
# below it with room for ordinary churn. Raise them, never lower them to make a
# run green. The gate prints both counts on every run, so the current figures
# are always one run away; a number written here would be the hand-maintained
# half again.
MIN_WORKFLOWS = 12
MIN_GUARDS = 15

# ── Allow-list ───────────────────────────────────────────────────────────────
# (workflow filename, job or step id, event literal) -> justification.
# Each row MUST name a ticket. A row that matches no violation fails the gate.
ALLOWED: dict[tuple[str, str, str], str] = {
    (
        "acceptance.yml",
        "pr-linux",
        "pull_request",
    ): (
        "#7250 / #7248: the `pull_request` trigger was removed to conserve "
        "free-tier minutes and `pr-linux` was left in place, dormant, because "
        "delete-vs-restore is an owner decision that is still open. The job is "
        "documented as dormant at its definition and in the `on:` block. When "
        "that decision lands — either way — this row must go: restoring the "
        "trigger makes it unnecessary, deleting the job makes it stale, and a "
        "stale row fails this gate."
    ),
}

EVENT_NAME_CMP = re.compile(
    r"github\.event_name\s*(==|!=)\s*(['\"])([A-Za-z_][A-Za-z0-9_]*)\2"
)
LOCAL_USES = re.compile(r"^\s*uses:\s*\./\.github/workflows/([^\s#]+)")


@dataclass
class Guard:
    workflow: str
    owner: str  # job name, or "<job> / step: <name>"
    level: str  # "job" | "step"
    line: int
    op: str
    event: str


@dataclass
class Workflow:
    path: str
    name: str
    on_events: set[str] = field(default_factory=set)
    calls: set[str] = field(default_factory=set)
    guards: list[Guard] = field(default_factory=list)
    has_jobs: bool = False
    job_names: list[str] = field(default_factory=list)


def strip_comment(line: str) -> str:
    """Drop a trailing YAML comment, respecting quotes.

    Comments matter here: acceptance.yml's `on:` block *documents* the dormant
    guard in prose that contains the exact expression the regex looks for. A
    gate that read comments would find its own violation inside the note that
    discloses it.
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


def indent_of(line: str) -> int:
    return len(line) - len(line.lstrip(" "))


def parse_workflow(path: str) -> Workflow:
    with open(path, encoding="utf-8") as fh:
        raw = fh.read().splitlines()

    wf = Workflow(path=path, name=os.path.basename(path))
    code = [strip_comment(ln) for ln in raw]

    # ── `on:` ────────────────────────────────────────────────────────────────
    # Three legal spellings: `on: push`, `on: [push, pull_request]`, and a
    # mapping whose child keys are the event names. Note we parse text, not
    # YAML: a real YAML loader folds the bare key `on` to the boolean True
    # (the Norway problem's cousin), which is a trap this avoids entirely.
    i = 0
    while i < len(code):
        ln = code[i]
        if indent_of(ln) == 0 and re.match(r"^(on|\"on\"|'on'|True|true):", ln):
            rest = ln.split(":", 1)[1].strip()
            if rest.startswith("["):
                body = rest.strip("[]")
                wf.on_events |= {e.strip().strip("'\"") for e in body.split(",") if e.strip()}
            elif rest:
                wf.on_events.add(rest.strip("'\""))
            else:
                j = i + 1
                while j < len(code):
                    nxt = code[j]
                    if not nxt.strip():
                        j += 1
                        continue
                    ind = indent_of(nxt)
                    if ind == 0:
                        break
                    if ind == 2:
                        m = re.match(r"^\s{2}([A-Za-z_][A-Za-z0-9_]*):", nxt)
                        if m:
                            wf.on_events.add(m.group(1))
                    j += 1
            break
        i += 1

    # ── jobs, guards, and local reusable-workflow calls ───────────────────────
    in_jobs = False
    job = None
    step = None
    # An `if:` may be a folded/literal block (`if: >-`) whose value continues on
    # following, more-indented lines. Both acceptance.yml and test.yml use that
    # form for the guards this gate exists to read, so not handling it would
    # make the gate blind to its own motivating case.
    pending_if: tuple[str, str, int, int] | None = None  # owner, level, line, indent

    for n, ln in enumerate(code, start=1):
        if not ln.strip():
            continue
        ind = indent_of(ln)

        if pending_if is not None:
            owner, level, start_line, base_ind = pending_if
            if ind > base_ind:
                for m in EVENT_NAME_CMP.finditer(ln):
                    wf.guards.append(Guard(wf.name, owner, level, n, m.group(1), m.group(3)))
                continue
            pending_if = None

        if ind == 0:
            in_jobs = re.match(r"^jobs:", ln) is not None
            if in_jobs:
                wf.has_jobs = True
            job = None
            step = None
            continue
        if not in_jobs:
            continue

        m = re.match(r"^\s{2}([A-Za-z_][A-Za-z0-9_.-]*):\s*$", ln)
        if m:
            job = m.group(1)
            wf.job_names.append(job)
            step = None
            continue
        if job is None:
            continue

        m = LOCAL_USES.match(ln)
        if m:
            wf.calls.add(m.group(1))

        m = re.match(r"^\s+-?\s*name:\s*(.+?)\s*$", ln)
        if m and ind >= 6:
            step = m.group(1).strip("'\"")

        m = re.match(r"^(\s+)if:\s*(.*)$", ln)
        if m:
            base_ind = len(m.group(1))
            value = m.group(2)
            level = "job" if base_ind == 4 else "step"
            owner = job if level == "job" else f"{job} / step: {step or '?'}"
            if value.strip() in (">", ">-", "|", "|-", ""):
                pending_if = (owner, level, n, base_ind)
                continue
            for g in EVENT_NAME_CMP.finditer(value):
                wf.guards.append(Guard(wf.name, owner, level, n, g.group(1), g.group(3)))

    return wf


def effective_events(wf: Workflow, by_name: dict[str, Workflow]) -> tuple[set[str], bool]:
    """(effective event set, resolvable).

    `workflow_call` inherits every local caller's effective set, transitively.
    A workflow_call-subscribed workflow with no local caller is unresolvable —
    a caller may live in another repository, so we cannot know its events.
    """
    seen: set[str] = set()

    def walk(name: str) -> tuple[set[str], bool]:
        if name in seen:
            return set(), True
        seen.add(name)
        cur = by_name.get(name)
        if cur is None:
            return set(), False
        events = set(cur.on_events)
        if "workflow_call" not in cur.on_events:
            return events, True
        callers = [o for o in by_name.values() if name in o.calls]
        if not callers:
            return events, False
        ok = True
        for caller in callers:
            sub, sub_ok = walk(caller.name)
            events |= sub
            ok = ok and sub_ok
        return events, ok

    return walk(wf.name)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--dir", default=".github/workflows", help="workflow directory")
    ap.add_argument("--min-workflows", type=int, default=MIN_WORKFLOWS)
    ap.add_argument("--min-guards", type=int, default=MIN_GUARDS)
    ap.add_argument(
        "--no-allow-list",
        action="store_true",
        help="ignore ALLOWED; every violation is reported. Used by the self-tests "
        "to prove the gate still sees the guards the allow-list covers.",
    )
    args = ap.parse_args()

    if not os.path.isdir(args.dir):
        print(f"workflow-event-gate: not a directory: {args.dir}", file=sys.stderr)
        return 2

    paths = sorted(
        os.path.join(args.dir, f)
        for f in os.listdir(args.dir)
        if f.endswith((".yml", ".yaml"))
    )
    workflows = []
    for p in paths:
        try:
            workflows.append(parse_workflow(p))
        except OSError as exc:
            print(f"workflow-event-gate: cannot read {p}: {exc}", file=sys.stderr)
            return 2
    by_name = {w.name: w for w in workflows}

    # A workflow with a `jobs:` key but no job parsed out of it means the job
    # keys are not where this parser looks (a file indented four spaces, say).
    # That file would contribute zero guards and the run would still report
    # "no violations" — a whole workflow silently exempt. It is an exit 2, the
    # tool-cannot-run verdict, not a quiet skip.
    blind = [w.name for w in workflows if w.has_jobs and not w.job_names]
    if blind:
        print(
            "workflow-event-gate: `jobs:` present but no job could be parsed in: "
            + ", ".join(blind)
            + ". The parser is not reading these files; its verdict on them is "
            "meaningless.",
            file=sys.stderr,
        )
        return 2

    total_guards = 0
    violations: list[tuple[Guard, set[str]]] = []
    allowed_hits: set[tuple[str, str, str]] = set()
    unresolvable: list[str] = []

    for wf in workflows:
        events, resolvable = effective_events(wf, by_name)
        total_guards += len(wf.guards)
        if not resolvable:
            if wf.guards:
                unresolvable.append(
                    f"  {wf.name}: subscribes to workflow_call with no local caller; "
                    f"{len(wf.guards)} guard(s) unresolvable"
                )
            continue
        for g in wf.guards:
            if g.event in events:
                continue
            key = (wf.name, g.owner, g.event)
            if not args.no_allow_list and key in ALLOWED:
                allowed_hits.add(key)
                print(
                    f"ALLOWED {wf.name}:{g.line} {g.level} `{g.owner}` "
                    f"compares github.event_name {g.op} '{g.event}' — {ALLOWED[key]}"
                )
                continue
            violations.append((g, events))

    print(
        f"workflow-event-gate: {len(workflows)} workflow(s), "
        f"{total_guards} github.event_name comparison(s) in `if:` guards, "
        f"{len(violations)} violation(s), {len(allowed_hits)} allow-listed."
    )
    for line in unresolvable:
        print(line)

    failed = False

    for g, events in violations:
        print(
            f"\nVIOLATION {g.workflow}:{g.line}\n"
            f"  {g.level}: {g.owner}\n"
            f"  guard: github.event_name {g.op} '{g.event}'\n"
            f"  but the workflow's effective event set is: "
            f"{', '.join(sorted(events)) or '(none)'}\n"
            f"  => this guard can never be satisfied under any trigger this "
            f"workflow has. Either subscribe to '{g.event}', change the guard, "
            f"delete the dead configuration, or add a ticket-bearing allow-list "
            f"row to {os.path.basename(__file__)}.",
            file=sys.stderr,
        )
        failed = True

    if not args.no_allow_list:
        for key, why in ALLOWED.items():
            if key in allowed_hits:
                continue
            # A row whose workflow is not in the scanned directory cannot be
            # judged stale — that is the ordinary state when the self-tests
            # point the gate at a synthetic tree. It is printed rather than
            # dropped, so pointing the gate somewhere unexpected is visible.
            if key[0] not in by_name:
                print(
                    f"NOTE: allow-list row {key!r} not checked — {key[0]} is not "
                    f"in {args.dir}."
                )
                continue
            print(
                f"\nSTALE ALLOW-LIST ROW {key!r}\n"
                f"  justification: {why}\n"
                f"  {key[0]} is in the tree but no guard there matches this row. "
                f"The job it covers was fixed, renamed or deleted — delete the row.",
                file=sys.stderr,
            )
            failed = True

    if len(workflows) < args.min_workflows:
        print(
            f"\nFLOOR: read {len(workflows)} workflow(s), floor is "
            f"{args.min_workflows}. The gate is not looking at the tree.",
            file=sys.stderr,
        )
        failed = True
    if total_guards < args.min_guards:
        print(
            f"\nFLOOR: parsed {total_guards} github.event_name comparison(s), floor "
            f"is {args.min_guards}. The gate is reading files but extracting "
            f"nothing from them.",
            file=sys.stderr,
        )
        failed = True

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
