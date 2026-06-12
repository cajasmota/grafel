# MCP tools — Admin & repair

[← Back to the MCP tools index](../mcp-tools.md)

Enrichment and repair queues, session metrics, and local-only telemetry events.

---

## `archigraph_enrichments`

Enrichment candidate queue.

Key parameters: `action` (required: `list`/`submit`/`reject`), `kind`, `limit` (default 10), `candidate_id`, `value`, `confidence` (default 1), `reason`, `repo_filter[]`.

Output: pending candidates (`list`) or a resolution acknowledgement (`submit`/`reject`).

---

## `archigraph_repairs`

Residual-edge repair queue.

Key parameters: `action` (required: `list`/`submit`), `repo_filter[]`, `limit` (default 20), `offset` (default 0).

Output: pending residual edges (`list`) or a resolution acknowledgement (`submit`).

---

## `archigraph_mcp_metrics`

Current-session tool-call metrics plus persisted daily rollups.

Key parameters: `days` (default 3). Group-agnostic — no `cwd` routing needed.

Output: per-tool `calls`, `errors`, `p50_ms`, `p95_ms` for the current daemon session, plus up to N days of rollup records from `~/.archigraph/metrics/mcp-YYYY-MM-DD.jsonl`.

---

## `archigraph_persona_event`

Record a persona lifecycle event. **LOCAL ONLY** — data never leaves the machine; events land in `~/.archigraph/events/persona-events-YYYY-MM-DD.jsonl`.

Key parameters: `persona` (required), `event_type` (required: `invoke`/`consult_out`/`save_finding`), `target_persona`, `depth` (default 0), `chain[]`, `metadata`.

**When to call**: at session start (`event_type=invoke`) and on each Consult-Out. Group-agnostic — no `cwd` routing needed.

---

## `archigraph_feedback_event`

Record agent-experience feedback for a test run. **LOCAL ONLY.**

Key parameters: `outcome` (required), `group`, `phase`, `library`, `capability`, `note`.

Output: a write acknowledgement.
