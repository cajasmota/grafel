# Phase 7 — Delta mode (--since <sha>)

This phase executes only when `/grafel-graph-quality` is invoked with `--since <sha>`. It restricts the benchmark to entities that changed since the given commit, making the quality check fast enough to run as a CI gate after every index run.

## When to use

- CI pipeline: after `grafel index`, run `/grafel-graph-quality --since $BEFORE_SHA` to verify no regression in MCP quality for the changed entities.
- Daily maintenance: `--since <yesterday's sha>` to spot-check yesterday's delta.
- Full re-benchmark: omit `--since` (default) — Phases 1–6 run on the full group.

## Pre-flight assertion

Call `grafel_whoami` before doing anything. If it errors, abort with:
"grafel MCP not configured for this directory. Run `/mcp` to fix."

## Inputs

- `--since <sha>`: the git commit SHA to diff against. The skill treats this as an opaque identifier passed to `grafel_recent_activity(since=<sha>)`.
- Existing full-benchmark output at `~/.grafel/quality-check/<prior-timestamp>/` (optional — used as baseline if `--baseline` is also set).

## Procedure

### Step 1 — Resolve the changed entity set

```
changed = grafel_recent_activity(since=<sha>)
```

`grafel_recent_activity` returns entities whose source files changed since the given timestamp or SHA. If the result is empty (nothing changed), print:

> No entities changed since `<sha>`. Nothing to benchmark. Exiting.

and exit cleanly (success, not error).

### Step 2 — Generate a restricted question set

Run Phase 1 (`prompts/01-question-generation.md`) with the restriction: generate questions **only** about entities in `changed`. Use the same nine question categories but constrain entity picks to `changed`.

If `changed` contains fewer than 5 entities, generate at minimum 5 questions by expanding to the 1-hop neighbours of the changed entities (via `grafel_expand`). This avoids a degenerate benchmark on trivial diffs.

Persist as `questions.json` in the run directory (same format as a full benchmark run).

### Step 3 — Run Phases 2, 3, 4, 5 on the restricted question set

These phases are identical to the full-benchmark versions. Run them with the restricted `questions.json`. The subagent isolation requirement (Phase 2 before Phase 3, independent contexts) still applies.

### Step 4 — Delta calibration

Run Phase 6 (`prompts/06-extraction-calibration.md`) restricted to the changed entities:
- Over-extraction audit: check only nodes whose `source_file` matches a file in `changed`.
- Under-extraction audit: check only relationships whose `from` or `to` entity is in `changed`.

Emit a `calibration.json` and append the "Extraction calibration" section to the report, clearly labeled:

```markdown
## Extraction calibration (delta: <N> entities since <sha>)
```

### Step 5 — Regression comparison (if --baseline is set)

If the user also passed `--baseline <prior-report-path>`, load the prior report and compare:
- For each question in the current `questions.json` that also appeared in the prior run (matched by question text), diff the MCP answer quality (full/partial/wrong/unknown) and the token cost.
- Emit a "Regression delta" section in the report showing: questions that regressed, questions that improved, unchanged.

If no prior baseline is available, omit the regression section.

### Step 6 — State file update

After a successful run, write `~/.grafel/groups/<group>/grafel-graph-quality/state.json`:

```json
{
  "last_run_sha": "<sha>",
  "last_run_timestamp": "<rfc3339>",
  "last_run_report": "<path to report.md>",
  "last_run_question_count": <N>,
  "last_run_mode": "delta"
}
```

This state file lets the next `--since` invocation know what SHA to diff from when the user omits `--since` (fall back to `last_run_sha`).

## Output

Same output layout as a full benchmark run, under:
```
~/.grafel/quality-check/<YYYY-MM-DD-HHMMSS>/
  questions.json       # Phase 1 (delta-restricted)
  without-mcp.json     # Phase 2
  with-mcp.json        # Phase 3
  judgment.json        # Phase 4
  report.md            # Phase 5 + delta calibration
  calibration.json     # Phase 6 (delta-restricted)
```

The report's title section includes:
```
**Mode:** delta — <N> entities changed since `<sha>`
```

## Telemetry

Print at end of this phase:
```
[phase-07-delta] grafel MCP calls: X | Bash invocations: Y | entities in delta: N
```
