# Phase 1 — Static security analysis

Runs deterministic, graph-based security checks. No LLM calls. Produces `phase1-findings.json`.

## Pre-flight

Call `grafel_orient (view=me)`. If it errors, abort.

If `--since <sha>` was passed: call `grafel_index_status(since=<sha>)` to get the changed entity set and restrict all queries below to that set.

## Step 1 — Auth coverage sweep

```
# Get all http_endpoint entities in the group
endpoints = grafel_find(query="kind:http_endpoint", limit=500)
```

For each endpoint:
- Call `grafel_subgraph(node=<id>, depth=1)` to see its edges.
- Check for any edge of kind `AUTHENTICATED_BY`, `REQUIRES_PERMISSION`, or `GUARDED_BY`.
- If none found: classify as `auth_missing`.

## Step 2 — Reachability classification

For each `auth_missing` endpoint:
- Check the entity's `tags` or `metadata` for `internal_only`, `health_check`, `webhook`, `internal_endpoint` markers.
- If none: classify as `publicly_reachable`.
- `publicly_reachable` + `auth_missing` = `severity: high`.

## Step 3 — Orphan endpoints

Endpoints with zero inbound callers (no `CALLS_TO`, `ROUTES_TO`, or similar edge pointing at them) AND `auth_missing`:
- Classify as `orphan_no_auth`. Severity: medium (may be dead code, but surface anyway).

## Step 4 — PII field exposure

```
pii_fields = grafel_find(query="kind:DataField tags:pii", limit=200)
```

For each PII field: trace whether any `http_endpoint` returns it AND that endpoint is `auth_missing`. If so: `pii_exposed`, severity: critical.

## Step 5 — Residual edges on auth paths

```
residuals = grafel_docgen_apply(kind="repairs", action=list, limit=100)
```

For each residual whose `from_entity` is an `http_endpoint` or `middleware`: classify as `residual_on_security_path`, severity: info. These are candidates for `/grafel-resolve`.

## Step 6 — Emit findings

Write `phase1-findings.json`:

```json
{
  "group": "<group>",
  "generated_at": "<rfc3339>",
  "since_sha": "<sha or null>",
  "findings": [
    {
      "fingerprint": "<sha256(entity_id+check_name)>",
      "entity_id": "<id>",
      "entity_name": "<name>",
      "check_name": "auth_missing",
      "severity_static": "high",
      "summary": "GET /api/orders has no authentication edge in the graph.",
      "repo": "<repo-slug>"
    }
  ]
}
```

## Reporting back

Print:
> Phase 1 complete: **N** findings (critical: C, high: H, medium: M, low: L, info: I). Proceeding to Phase 2.

If `--phase1-only`: print the table and exit.
