# Phase 6 - Extraction calibration (over / under audit)

Audit whether grafel is **over-extracting** (noise, phantoms, duplicates polluting the graph) or **under-extracting** (real relationships and entities missing). This is a structural audit of the graph itself, independent of the Phase 2/3 head-to-head. Your output is `calibration.json` plus an "Extraction calibration" section appended to the report from Phase 5.

Skip this phase only if the user passed `--no-calibration`.

## Inputs

- The resolved group (confirm with `grafel_whoami` if not already known).
- `grafel_stats` for the entity/relationship/cross-repo totals (the denominators).
- The real repos on disk for grep+read ground truth (the judge uses `rg`/`Read`, never the MCP, so the audit is honest).

## Protocol

### Step A - Establish the denominators

Call `grafel_stats`. Record total entities, total relationships, cross-repo links, and per-repo counts. Every rate you report is `count ÷ one of these denominators` (or ÷ the affected kind's count) — always state which.

### Step B - Over-extraction probes

For each category, gather counts via MCP, then confirm with grep+read that the flagged nodes really are noise/dupes.

1. **Duplicate-kind nodes.** Pick several representative symbols (a Django ViewSet/Model, a Celery task, a serializer). For each, `grafel_search_entities query=<name>` and group results by `(name, source_file)`. Count distinct `entity_id` and the set of `kind` values per group. If one source symbol yields >1 node, that is duplication. Report the duplication factor (nodes per real symbol) and the kind-pairs observed (e.g. `Component`+`View`, `ScheduledJob`+`Task`+`Operation`).
2. **Statement-level noise.** Search for entity names that are clearly not declarations: contains `= f"`, `= {`, `return `, ` == `, leading whitespace, or is not a valid identifier. Each hit is an expression mis-extracted as an entity. Estimate the rate within the affected kind (usually `Operation`).
3. **Framework scaffolding.** `grafel_endpoints action=definitions`. Count auto-generated routes (Django `/admin/...`, DRF browsable-API, framework health routes) vs hand-written app routes. Report scaffolding share of total endpoints.
4. **Generated / vendored pollution.** Sample `source_file` paths for `node_modules/`, `dist/`, `build/`, `migrations/`, `*.generated.*`, `*_pb2.py`, openapi output. Count nodes from such paths.
5. **Phantom edges (spot check).** Take 3-5 high-confidence edges (`grafel_find_callers`/`find_callees`) and grep the source to confirm the call really exists. Note any that don't.

### Step C - Under-extraction probes

1. **Missing relationships.**
   - `grafel_test_coverage` - if test entities exist but `TESTS edges (total) = 0`, that is total under-extraction of test linkage. Report covered% and edge count.
   - `grafel_topology action=orphan_publishers` / `orphan_subscribers` - count pub/sub topics with null counterpart. Cross-check Celery `@shared_task` / `.delay()` / `.apply_async()` call sites in source that produce no edge.
   - `grafel_endpoints action=calls orphan_only=true` - count orphan cross-repo HTTP calls. Then check WHY: compare a client path (e.g. `/inspections/{id}`) to the server route (`/api/v1/inspections/{pk}`). If the endpoint genuinely exists server-side, the orphan is a **normalization gap** (prefix/param-name), not a real missing endpoint — say so. Report cross-repo link coverage as `linked ÷ linkable`.
2. **Missing entities.** Pick 3-5 classes/functions per repo you can see with `rg`. Confirm each via `grafel_inspect` / `grafel_search_entities`. Any grep-visible symbol the MCP can't find is a miss.
3. **Empty qualified_names.** `grafel_inspect` a sample of entities across kinds; count those with `qualified_name == ""`. Report rate by kind. Empty qnames break path-finding and cross-repo joins.
4. **Unlinked framework patterns.** Look for DI bindings, Django signals (`@receiver`), route→handler wiring in source; check whether the graph has the corresponding edge.
5. **Missing kinds.** Note any structurally important kind that is absent or near-empty when source clearly contains instances.

### Honesty rule

Before claiming a miss, grep the real repo to prove the thing exists in source. Before claiming noise, grep to prove the node has no real referent. The audit blames the graph only for genuine mismatches.

## Output schema (`calibration.json`)

```json
{
  "version": 1,
  "audited_at": "<RFC3339>",
  "denominators": {"entities": 0, "relationships": 0, "cross_repo_links": 0},
  "over_extraction": [
    {"issue": "duplicate-kind nodes", "count": 0, "rate": "Nx per symbol", "kinds": ["Component","View"], "examples": [{"path": "", "line": 0}]}
  ],
  "under_extraction": [
    {"issue": "test entities with 0 TESTS edges", "count": 0, "rate": "0%", "examples": [{"path": "", "line": 0}]}
  ],
  "verdict": "over-biased | under-biased | balanced",
  "verdict_rationale": "...",
  "prune_recommendations": ["..."],
  "add_recommendations": ["..."]
}
```

## Report section to append

Append to the Phase 5 report (and copy into `<run-dir>/report.md`):

```markdown
## Extraction calibration

| Direction | Issue | Count | Rate | Example (path:line) |
|---|---|---:|---:|---|
| Over | ... | ... | ... | ... |
| Under | ... | ... | ... | ... |

**Calibration verdict:** `<over-biased / under-biased / balanced>` — `<one-line justification>`.

### Prune recommendations (over-extraction)
- `<what to drop>` — cites `<count/rate>`.

### Add recommendations (under-extraction)
- `<what to wire up>` — cites `<count/rate>`.
```

## Privacy

- Log entity kinds, counts, rates, paths, and line numbers only — never source-code snippets.
- Do not name competitor tools.

## Output

Write `calibration.json`, append the "Extraction calibration" section to the report at `--output` and `<run-dir>/report.md`, print the calibration verdict and the top over/under issue, and return to the orchestrator.
