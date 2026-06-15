# Progress tracking

The orchestrator records pass progress in `~/.grafel/groups/<group>/progress.json`. Writers do not touch this file directly — they hand back to the orchestrator with a structured result and the orchestrator updates progress.

## Schema

```json
{
  "group": "<group>",
  "started_at": "<RFC3339>",
  "passes": {
    "0_domain_qa": { "status": "done|pending|skipped", "completed_at": "<RFC3339>", "output": "<path>" },
    "1_inventory": { "status": "...", "completed_at": "...", "output": "..." },
    "2_plan":      { "status": "...", "completed_at": "...", "output": "..." },
    "3_overview":  { "status": "...", "per_repo": { "<slug>": "done|pending|failed" } },
    "4_cluster":   { "status": "...", "per_module": { "<repo>:<module>": "done|pending|failed" } },
    "5_reference": { "status": "...", "per_repo": { "<slug>": "done|pending|failed" } },
    "6_cross_cutting": { "status": "...", "per_topic": { "auth": "done|pending|failed" } },
    "7_synthesis": { "status": "...", "output": "<path>" },
    "8_cross_link": { "status": "...", "report": "<path>" }
  },
  "failures": [
    { "pass": "<id>", "scope": "<repo|module|topic>", "reason": "<short>", "timestamp": "<RFC3339>" }
  ]
}
```

## Resuming a partial run

If the user re-invokes `generate-docs` and `progress.json` exists, the orchestrator:

1. Loads `progress.json`.
2. Asks the user: "Resume from `<first non-done pass>`, or restart from Pass 0?"
3. Resumes by skipping `done` work and re-running everything `pending` or `failed`.

A pass marked `skipped` is treated as `done` for ordering — used for Pass 10/11/12 when no patterns were generated.

## Reporting

At the end of a run the orchestrator prints a compact summary:

- Files written (count by template).
- Cross-repo links accepted / rejected.
- Enrichment candidates resolved / rejected / left for human.
- Verification-checklist failures fixed inline vs deferred.
- Total elapsed time.
