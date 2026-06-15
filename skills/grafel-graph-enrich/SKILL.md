---
name: grafel-graph-enrich
description: Annotate the graph with structured YAML frontmatter on http_endpoint, process_flow, and message_topic entities so the dashboard panels (Paths, Flows, Topology) light up. No prose docs — frontmatter only. Idempotent and delta-aware; safe to re-run after any index update.
when-to-use: User asks to "enrich the Paths panel", "add rank and summaries to my flows", "make the dashboard Topology show data", "enrich endpoints", or invokes /grafel-graph-enrich explicitly. Run after /grafel-resolve (or /grafel-graph-quality) and before /grafel-tech-docs if you want enriched entity summaries inlined into module READMEs.
---

# grafel-graph-enrich

Annotate the grafel knowledge graph with structured YAML frontmatter so the dashboard's Paths, Flows, and Topology panels display rich data. This skill runs in two sequential phases:

- **Phase 1 — Enrichment** (`prompts/13-enrichment.md`): emit YAML frontmatter for every `http_endpoint`, `process_flow`, and `message_topic` entity. Uses tiered model selection (Haiku for most entities, Sonnet for `critical` band) and batches 20–50 entities per LLM call. Idempotent — already-enriched entities are skipped.
- **Phase 2 — Validation** (`prompts/14-frontmatter-validation.md`): re-read every enriched doc file and verify each field against the backend parser's expectations. Catches schema drift before the user notices a blank dashboard panel.

The skill is independent of prose documentation. The dashboard becomes useful with enrichment alone — even if no tech docs or business docs have been written.

## When to use this skill

Invoke when the user asks for any of:

- "Enrich the Paths panel" / "add summaries to my flows" / "make Topology show data."
- "Run the enrichment pass."
- "The dashboard panels are blank — fix that."
- `/grafel-graph-enrich` (slash command).

Do **not** invoke it for prose documentation (that is `/grafel-tech-docs`). Do not invoke it to repair residual edges (that is `/grafel-resolve`).

## Inputs the skill expects

- A running grafel daemon (`grafel_whoami` must succeed).
- A registered, indexed group with enrichment candidates in the queue (`grafel_enrichments(action=list, status="pending")` returns results).
- Optional: `--delta-only` flag — process only entities with `status="pending"` that are newly added since the last run (the default behaviour; the flag makes it explicit).
- Optional: `--yes` flag — skip the cost-estimate confirmation prompt (for automation).
- Optional: `--group <name>` — override group when CWD is ambiguous.

If `grafel_enrichments(action=list, status="pending")` returns zero results, the skill exits with "No pending enrichment candidates. Nothing to do." (success).

## Cost and model selection

| `criticality_band` | Score | Model | Batch size |
|--------------------|-------|-------|------------|
| `critical` | ≥ 80 | `claude-3-5-sonnet-20241022` | 25 entities/call |
| `high` | 60–79 | `claude-3-haiku-20240307` | 40 entities/call |
| `medium` | 40–59 | `claude-3-haiku-20240307` | 40 entities/call |
| `low` | < 40 | `claude-3-haiku-20240307` | 40 entities/call |

**Never mix tiers in the same batch.** Print a cost estimate before dispatching (rough: `critical × 800 tok + other × 500 tok`) and gate on user confirmation unless `--yes` is set.

Typical cost for a 5,800-entity group: **$5–$15** total. A 1,000-entity group: **$1–$3**.

## Idempotency and delta mode

Phase 1 always begins by fetching `grafel_enrichments(action=list, status="enriched")` and excluding those entity IDs from all batches. This makes the skill safe to restart after any failure without re-enriching or incurring duplicate LLM costs.

State is persisted at `~/.grafel/groups/<group>/grafel-graph-enrich/state.json`:

```json
{
  "last_run_timestamp": "<rfc3339>",
  "last_run_enriched_count": <N>,
  "last_run_skipped_count": <N>
}
```

## Procedure

### Pre-flight
Call `grafel_whoami`. If it errors: abort with "grafel MCP not configured for this directory. Run `/mcp` to fix."

### Phase 1 — Enrichment
Follow the full procedure in `prompts/13-enrichment.md`. Key steps:
1. Resume check: fetch already-enriched IDs (skip them).
2. Collect all pending candidates (three kinds: `http_endpoint`, `process_flow`, `message_topic`).
3. Build entity stubs with `grafel_expand(node=<id>, depth=2)` for neighbour context.
4. Partition by `criticality_band` → Sonnet vs Haiku batches.
5. Print cost estimate; gate on user confirmation (or `--yes`).
6. Dispatch batches, write YAML frontmatter to doc files, call `grafel_enrichments(action=submit)`.
7. Run verification checklist on a sample of ≥10 entities per tier.

### Phase 2 — Validation
Immediately after Phase 1 completes, run `prompts/14-frontmatter-validation.md`. This re-reads every enriched doc file, checks structural validity, required fields, kind isolation, rank bounds, `merged_into` integrity, health-tracked field coverage, and discovery-path reachability. Any entity that fails validation is set back to `pending` for a re-run of Phase 1.

### Summary
End with:
> Enriched **N** entities (critical: C via Sonnet, other: O via Haiku). Validation: **P** passed, **F** failed (set back to pending). Total cost: ~$X.

## grafel MCP tool surface

- `grafel_whoami` — group/repo resolution.
- `grafel_enrichments` — primary tool (`action=list|submit|reject`).
- `grafel_expand` — depth-2 neighbour context for entity stubs.
- `grafel_save_finding` — persist the Phase 2 validation report.

## Output layout

```
~/.grafel/docs/<group>/<repo-slug>/enrichments/
  http_endpoint/
    <entity_id>.md    # YAML frontmatter + optional prose
  process_flow/
    <entity_id>.md
  message_topic/
    <entity_id>.md
```

Frontmatter is prepended to existing doc files when a doc already exists; a minimal file is created otherwise. The canonical placement for new files is `~/.grafel/docs/<group>/<repo-slug>/enrichments/<kind>/<entity_id>.md`.

## Related

- `skills/generate-docs/SKILL.md` — the monolith this skill was extracted from; see Pass 13 and Pass 14 for the original context.
- `internal/dashboard/enrichment_frontmatter.go` — backend YAML frontmatter parser; source of truth for which fields are consumed.
- `internal/dashboard/handlers_topology.go` — Topology panel enrichment lookup.
- `internal/dashboard/handlers_flows.go` — Flows panel enrichment lookup.
- ADR-0015 — residual repair; run `/grafel-resolve` before enrichment to minimize "orphan endpoint" findings.
- `skills/grafel-resolve/SKILL.md` — upstream skill for residual resolution.

## Read next

After enriching the graph, generate prose documentation or run a security audit:
→ `/grafel-tech-docs` — generate per-module technical documentation for engineers.
→ `/grafel-business-docs` — generate PM-facing capabilities, journeys, and business rules.
→ `/grafel-security-audit` — run a two-phase security audit against the enriched graph.
