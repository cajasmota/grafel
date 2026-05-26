---
name: archigraph-tech-docs
description: Generate the per-repo and group-level technical documentation set — overview, modules, reference, cross-cutting, group synthesis, cross-links, and patterns. For engineers. Runs 13 orchestrated passes using the archigraph MCP as the primary navigation surface. Supports delta re-runs (only rewrite modules whose graph changed).
when-to-use: User asks to "document this repo", "generate technical docs", "write the module guide", "regenerate docs after a refactor", "write API reference", "cross-link the repos", or invokes /archigraph-tech-docs explicitly. Requires the archigraph daemon to be running and the group to be indexed.
---

# archigraph-tech-docs

Generate the complete technical documentation set for a registered archigraph group: per-module READMEs, API reference, cross-cutting concerns, group synthesis, cross-repo links, and pattern library.

This skill is a direct extraction of the technical tier (Passes 0–12 + Pass 20) from `/generate-docs`. It is the largest and most expensive skill in the archigraph family.

## CRITICAL TOOL DISCIPLINE (enforced on every pass — read before ANY action)

For ANY question about "what entities/files exist in this codebase", "who calls X", "what does Y import", "what's in module Z", you MUST use archigraph MCP tools: `archigraph_inspect`, `archigraph_find`, `archigraph_expand`, `archigraph_stats`, `archigraph_clusters`, `archigraph_traces`, `archigraph_whoami`.

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase **for entity discovery**. Use Bash ONLY for reading specific source line ranges that `archigraph_get_source` returns, running `archigraph docgen --llm-mode=apply`, and writing output files into the staging directory.

If the MCP returns empty or seems wrong, file a side ticket and ABORT — do NOT silently substitute grep results for graph queries.

### Pre-flight assertion (FIRST action in every pass)

Call `archigraph_whoami` before doing anything else. If it errors: ABORT with "archigraph MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/archigraph-tech-docs`."

## When to use this skill

Invoke when the user asks for any of:

- "Document this repo / this group."
- "Regenerate the docs after the recent refactor."
- "Write API reference / module guide / cross-repo overview."
- "Fill the LLM bundle" / "run docgen in LLM mode" / "orchestrate the bundle."

Do not invoke it for business-tier docs (that is `/archigraph-business-docs`), enrichment (that is `/archigraph-graph-enrich`), or residual cleanup (that is `/archigraph-resolve`).

## Prerequisites

- A running archigraph daemon (`archigraph status` should show "running").
- A resolved archigraph group.
- Per-repo `<repo>/.archigraph/graph.json` from the most recent index run.
- Recommended (not required): run `/archigraph-resolve` first to minimize residual edges. Run `/archigraph-graph-quality` to confirm the graph foundation is solid before spending tokens on writing.

## Staging-dir + atomic promote architecture

Documentation is written into a **staging directory** during the run and atomically promoted to the canonical store only when all passes complete without errors. This makes in-repo write accidents architecturally impossible.

1. **Pass 2** calls `archigraph_docgen_start_run(group="<group>")` → receives `run_id` and `staging_path`.
2. **Passes 3–12** write doc files into `<staging_path>/<relative-path>` using the Write tool.
3. **Pass 8** and **Pass 14** (if enrichment exists) call `archigraph_docgen_validate(run_id)`.
4. **Pass 20** (or end of pass chain) calls `archigraph_docgen_promote(run_id)` to atomically move staging → canonical.

## Pass chain

| Pass | Prompt | Purpose | Est. time |
|------|--------|---------|-----------|
| 0 | `prompts/00-domain-qa.md` | First-run domain interview | 5–10 min interactive |
| 1 | `prompts/01-inventory.md` | Discover repos and entities | 2–5 min |
| 2 | `prompts/02-plan.md` | Per-module documentation plan with token estimates | 2–3 min |
| 3 | `prompts/03-overview.md` | Repo-level overview.md for every repo | 3–5 min |
| 3a | `prompts/03a-generation-time-repair.md` | Inline repair hook for Passes 3–6 + 12 | (integrated) |
| 4 | `prompts/04-cluster.md` | Per-module deep-dive (parallel writer subagents) | 5–20 min |
| 5 | `prompts/05-reference.md` | Reference docs: API, config, deployment | 3–8 min |
| 6 | `prompts/06-cross-cutting.md` | Cross-cutting: auth, logging, error handling | 2–5 min |
| 7 | `prompts/07-group-synthesis.md` | Group-level synthesis page | 3–5 min |
| 8 | `prompts/08-cross-link.md` | Validate links + cross-repo link candidates | 2–4 min |
| 10 | `prompts/10-pattern-convergence.md` | Aggregate pattern candidates | 2–3 min |
| 11 | `prompts/11-pattern-cross-link.md` | Populate pattern documentation_url fields | 1–2 min |
| 12 | `prompts/12-pattern-prose.md` | Emit docs/patterns/<category>/<id>.md | 2–4 min |
| 20 | `prompts/20-llm-orchestrate.md` | LLM-mode orchestrate (standalone; only when bundle files exist) | varies |

**Note:** Passes 1a and 1b (residual repair) are NOT part of this skill. Run `/archigraph-resolve` before invoking this skill. Pass 3a is an inline hook used by writer passes, not a standalone pass.

**Total wall time:** 25–65 min (small repos), 1–4 h (large repos). Pass 4 is the critical path; it parallelises across module clusters.

## LLM-mode orchestrate (Pass 20)

Pass 20 is a standalone mode triggered only when `*-page-bundle.json` files exist in the docgen output directory. Do NOT use Pass 20 inside the standard Passes 0–12 pipeline. See `prompts/20-llm-orchestrate.md` for the full procedure.

## Conventions

The skill applies stack-specific conventions to every writer subagent. See `conventions/` for registered conventions. Every convention requires `conventions/_graph-searchability.md` first.

If the agent encounters a stack with no matching convention, stop and direct the user to run the `extend-convention` skill.

## Quality gates

Before any pass commits output, the writer runs `snippets/verification-checklist.md`. The checklist enforces:
- **`snippets/anchor-contract.md`** — emitted anchors derived from actual headings (never hand-authored).
- **`snippets/link-hygiene.md`** — link targets are real generated doc files; no in-source-tree links.
- **Volume control** (`prompts/02-plan.md` § Step 1b) — merge thin modules, cap by LOC, no empty stub pages.

## Docgen Repair Feedback Contract

Writer passes (3, 3a, 4, 5, 6, 12) emit repair candidates to `docgen-repairs.jsonl` when they discover facts the static extractor missed. See `snippets/docgen-repair-emission.md` for the full emission procedure.

## Output layout

```
~/.archigraph/docs/<group>/<repo-slug>/
  overview.md
  modules/<module-slug>/
    README.md
    api.md
    flows.md
  reference/
    config.md
    deployment.md
    scripts.md
    dependencies.md
  how-to/local-dev.md
  glossary.md
~/.archigraph/docs/<group>/
  group-synthesis.md
  cross-links.md
  docs/patterns/<category>/<id>.md
```

## archigraph MCP tool surface

- `archigraph_whoami`, `archigraph_find`, `archigraph_inspect`, `archigraph_expand`
- `archigraph_trace`, `archigraph_traces`, `archigraph_clusters`, `archigraph_stats`
- `archigraph_get_source`, `archigraph_recent_activity`
- `archigraph_save_finding`, `archigraph_list_findings`
- `archigraph_cross_links`, `archigraph_enrichments`, `archigraph_repairs`
- `archigraph_patterns`
- `archigraph_docgen_start_run`, `archigraph_docgen_status`, `archigraph_docgen_validate`, `archigraph_docgen_promote`, `archigraph_docgen_abort`, `archigraph_docgen_list`

## Related

- `skills/archigraph-resolve/SKILL.md` — run before this skill to clean up residuals.
- `skills/archigraph-graph-quality/SKILL.md` — run before this skill to validate graph health.
- `skills/archigraph-graph-enrich/SKILL.md` — run alongside or after for dashboard enrichment.
- `skills/extend-convention/SKILL.md` — run if the stack has no convention file.
- ADR-0015, ADR-0018 — residual repair and pattern discovery designs.

## Read next

After generating technical docs, generate the business-facing doc set or run deeper analysis:
→ `/archigraph-business-docs` — generate PM-facing capabilities, journeys, and business rules synthesised across the group.
→ `/archigraph-security-audit` — run a two-phase security audit using the tech docs as context.
→ `/archigraph-consult` — run a panel of specialist personas (architect, security auditor, business analyst) against the generated docs.
