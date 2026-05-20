# generate-docs

Generate module-organized markdown documentation for every repo in a registered archigraph group, then stitch it into a group-level synthesis with cross-repo links.

## When to use this skill

Invoke this skill when the user asks for any of:

- "Document this repo / this group."
- "Regenerate the docs after the recent refactor."
- "Write API reference / module guide / cross-repo overview."
Do not invoke it for one-off docstrings, README touch-ups, or commit-message writing. The skill assumes the archigraph daemon is running, the target repos are registered (`archigraph register <repo>`), and each repo has been indexed at least once.

## Inputs the skill expects

- A running archigraph daemon (`archigraph status` should show "running"). All indexing, MCP serving, and cross-repo linking run inside the daemon; there is no separate per-repo process to manage (ADR-0017).
- A resolved archigraph group (the skill calls `archigraph_whoami` first to confirm).
- Per-repo `<repo>/.archigraph/graph.json` produced by the daemon on the most recent index run.
- Group state under `~/.archigraph/groups/<group>/`.
- Optional enrichment candidates at `<repo>/.archigraph/enrichment-candidates.json`.

If the daemon is not running, the skill stops at Pass 0 and tells the user to run `archigraph start`. If a repo is not yet registered, the skill tells the user to run `archigraph register <repo-path>`. Do not invoke `archigraph index` directly — it is now a thin RPC client that delegates to the daemon.

## Pass numbering (Pass 0 through Pass 13)

The skill is a strict pipeline. Each pass has a dedicated prompt file under `prompts/`. A subagent reads the prompt and follows it; the orchestrator (this skill) tracks progress and gates each pass on the previous one's output.

### Expected time per pass

Time estimates assume typical small-to-medium codebases (1k–10k source entities). Larger corpora (100k+ entities) may take 2–3× longer in inventory and parallel passes.

| Pass | Prompt | Purpose | Est. time |
|------|--------|---------|-----------|
| 0 | `prompts/00-domain-qa.md` | First-run domain interview: what is this group, who owns it, what are the deployment boundaries. | 5–10 min interactive |
| 1 | `prompts/01-inventory.md` | Discover repos and entities via `archigraph_find` / `archigraph_stats` / `archigraph_clusters`. | 2–5 min |
| 1a | `prompts/01a-residual-repair-sweep.md` | Pre-Q&A repair sweep (ADR-0015): list residuals via `archigraph_repairs(action=list)`, auto-resolve unambiguous ones, surface the rest as questions for Pass 1b. | 1–3 min |
| 1b | `prompts/01b-repair-aware-qa.md` | Repair-aware Q&A: walk the user through residuals Pass 1a could not auto-resolve; each answer becomes an `archigraph_repairs(action=submit)` call. | 2–10 min (depends on residual count) |
| 2 | `prompts/02-plan.md` | Produce a per-module documentation plan with token estimates. | 2–3 min |
| 3 | `prompts/03-overview.md` | Repo-level `overview.md` for every repo. | 3–5 min |
| 3a | `prompts/03a-generation-time-repair.md` | Hook (not a standalone pass): every writer in Passes 3-6 + 12 inspects outbound residuals of the entity it is about to describe, repairs in-place when possible, documents as runtime-resolved otherwise. | (integrated into Passes 3–6 + 12) |
| 4 | `prompts/04-cluster.md` | Per-module deep-dive (parallel writer subagents, one per cluster). | 5–20 min (highly parallelized) |
| 5 | `prompts/05-reference.md` | Reference docs: API, config, deployment, scripts, dependencies. | 3–8 min |
| 6 | `prompts/06-cross-cutting.md` | Cross-cutting concerns: auth, logging, error handling, observability. | 2–5 min |
| 7 | `prompts/07-group-synthesis.md` | Group-level synthesis page that ties the repos together. (Cross-repo chains pending #769; until then writers should reach cross-repo via `archigraph_cross_links`). | 3–5 min |
| 8 | `prompts/08-cross-link.md` | Validate links and resolve cross-repo link candidates via `archigraph_cross_links`. | 2–4 min |
| — | *(Pass 9 reserved — planned for milestone-2 doc-site work)* | | |
| 10 | `prompts/10-pattern-convergence.md` | Aggregate subagent pattern candidates + promote convergent ones (ADR-0018 Phase 4). | 2–3 min |
| 11 | `prompts/11-pattern-cross-link.md` | Populate each approved pattern's `documentation_url` (ADR-0018 Phase 5). | 1–2 min |
| 12 | `prompts/12-pattern-prose.md` | Emit `docs/patterns/<category>/<id>.md` per approved pattern (ADR-0018 Phase 6). | 2–4 min |
| 13 | `prompts/13-enrichment.md` | LLM enrichment pass: emit unified YAML frontmatter for `http_endpoint`, `process_flow`, and `message_topic` entities (merge, disqualify, rank, group, summarise, detect gaps). Dashboard surfaces consume this data. | 5–15 min |

**Total wall time:** typically **25–60 minutes** for small repos (1k entities), **1–2 hours** for medium repos (10k entities), **2–4 hours** for large repos (100k+ entities). Pass 4 parallelizes across module clusters, so the critical path is dominated by Pass 0 (user interaction), Passes 1–2 (discovery), and Passes 4–5 (content generation).

If a pass appears to hang:
1. Check `archigraph status` — the daemon must be running and idle (not indexing another repo).
2. Check the agent console in Claude Code for errors. Common issues: daemon timeout, network glitch, or user timeout in Pass 1b (too many residuals to resolve interactively).
3. To resume, re-invoke `/generate-docs` in the same CWD — the orchestrator checks for completed passes and skips them.

During Pass 4 (per-module writers), each subagent additionally emits `PatternCandidate` entities via `archigraph_patterns(action=record, as_candidate=true)` whenever it observes ≥ `per_subagent_threshold` (default 2) instances of a structural recurrence in its slice. The candidates aggregate in Pass 10, cross-link in Pass 11, and produce dedicated markdown in Pass 12. The full design is in [ADR-0018](../../docs/adrs/0018-agent-learned-patterns.md).

Passes 1a and 1b integrate the ADR-0015 residual-repair flow into doc generation. **Pass 1a scope (narrow):** auto-resolves only (a) residuals that match a saved repair template with confidence ≥ 0.8, and (b) recognised third-party API stubs (e.g. `stripe.charges.create`, `https://api.<vendor>/...`). It does NOT attempt `bind_to_entity` resolutions — those require entity-level data that Pass 1 (inventory) does not provide. All `bind_to_entity` candidates are surfaced to Pass 1b as user questions, or deferred to Pass 3a (generation-time) where the writer has full subgraph context. Pass 1b is a templates-driven interactive Q&A that translates user answers into `archigraph_repairs(action=submit)` calls. Pass 3a is a hook (not a numbered pass): every writer in Passes 3–6 and 12 runs it before describing an entity, so any residual that escaped the sweep (including `bind_to_entity` deferred from Pass 1a) gets one more chance to repair with local subgraph context, or failing that, gets surfaced in prose as a documented runtime-resolved edge per ADR-0007. The standalone `/archigraph-repair` skill (`skills/archigraph-repair/SKILL.md`) exposes the same flow outside doc generation for ad-hoc cleanup.

## Pass 13 — LLM enrichment pass

Pass 13 is an optional post-documentation enrichment step. Run it after the prose docs are complete (Pass 12) when the user wants to enrich dashboard surfaces (Paths, Flows, Topology) with structured metadata.

### When to run Pass 13

The pass is **on-demand** — not part of a standard first-time doc generation run. Trigger it when:
- The user explicitly asks for enriched dashboard data ("enrich the Paths panel", "add rank and summaries to my flows").
- The `archigraph_enrichments(action=list)` call returns enrichment candidates with status `pending`.

### What Pass 13 does (per entity kind)

For every entity of kind `http_endpoint`, `process_flow`, or `message_topic`, the enrichment subagent:

1. **Merges** near-duplicates — identifies same logical entity in two places; emits `merged_into` on the redundant one.
2. **Disqualifies** false positives — marks regex-style noise entities with `disqualified: true`.
3. **Ranks** by importance — infers a 0..1 score from traffic signals (inbound caller count, business heuristic).
4. **Groups** by domain — LLM-inferred cluster key (e.g. `orders`, `auth`, `inventory`) + human-readable `group_label`.
5. **Summarises** — writes a one-sentence natural-language summary.
6. **Detects gaps** — lists structural problems (missing auth, missing error response, orphan producer, etc.).

The subagent writes the enrichment as YAML frontmatter at the **top** of the entity's existing doc file. If no doc file exists for the entity, a minimal file is created containing only the frontmatter block.

### Unified frontmatter schema

Every enriched entity doc file starts with a YAML frontmatter block delimited by `---`. **All fields are optional** — omit any field the LLM cannot determine with confidence. The dashboard backend falls back to first-line prose summary when frontmatter is absent.

```yaml
---
entity_id: <graph entity ID, e.g. "ep-abc123">
kind: http_endpoint          # http_endpoint | process_flow | message_topic
disqualified: false          # true = LLM considers this a false-positive entity
merged_into: ""              # non-empty = this entity is superseded by the named entity_id
rank: 0.78                   # 0..1 importance score (omit if unknown)
group: orders                # short domain key, lower-case, no spaces
group_label: 'Order processing'   # human-readable group name
summary: 'Returns paginated list of orders for the authenticated user'
gaps:
  - 'No error response documented for 4xx status codes'
  - 'Auth requirement not enforced — missing decorator'

# ── http_endpoint-specific fields ────────────────────────────────────────────
method: GET
path: /api/orders
parameters:
  - name: page
    in: query
    type: int
    required: false
    default: 1
    description: Page number (1-indexed)
  - name: limit
    in: query
    type: int
    required: false
    default: 50
responses:
  '200':
    description: Paginated order list
    shape: '{ orders: Order[], total: int, page: int }'
  '401':
    description: Unauthenticated
  '400':
    description: Invalid query params
auth: 'Bearer token required (JWT)'
tables_touched: [orders, order_items]

# ── process_flow-specific fields ─────────────────────────────────────────────
steps:
  - Validate cart contents and check stock
  - Charge payment method via payment service
  - Persist order record to database
  - Emit order.created event to broker
preconditions: 'User is authenticated and cart is non-empty'
expected_outcome: 'Order persisted, confirmation email dispatched, inventory decremented'

# ── message_topic-specific fields ────────────────────────────────────────────
schema: '{ order_id: string, total: float, items: OrderItem[], user_id: string }'
typical_payload_size_bytes: 512
volume_estimate: high          # low | medium | high | very-high
expected_consumers: [order-fulfillment, analytics, notifications]
---

## Description

Free-form prose continues here (existing content unchanged below this block).
```

> **Field selection rules**
> - Emit only `kind`-relevant per-kind fields. Do not emit `steps` for an `http_endpoint`.
> - Omit `rank` when you have no signal; do not fabricate a number.
> - `disqualified: true` suppresses the entity from the default dashboard view; only set it when clearly a false positive.
> - `merged_into` must reference an `entity_id` that exists in the same group.
> - `gaps` entries should be actionable (the user can act on them); avoid tautological observations.

### Pass 13 procedure

```
# 1. List entities to enrich.
archigraph_find(question="HTTP endpoints routes", depth=1, token_budget=1200)
archigraph_find(question="process flows call chains", depth=1, token_budget=1200)
archigraph_find(question="message topics broker queues", depth=1, token_budget=800)

# 2. For each entity, inspect neighbours (auth edges, QUERIES edges, PUBLISHES_TO).
archigraph_expand(node="<entity_id>", depth=2)

# 3. Check enrichment candidates queue for pre-computed signals.
archigraph_enrichments(action=list, kind="http_endpoint")
archigraph_enrichments(action=list, kind="process_flow")
archigraph_enrichments(action=list, kind="message_topic")

# 4. Write frontmatter. Prepend to the existing doc file; do not replace prose.
# 5. Submit enrichment record so the daemon tracks it.
archigraph_enrichments(action=submit, entity_id="<id>", summary="...", kind="<kind>")
```

After writing, run `snippets/verification-checklist.md` for each entity. Hand back to the orchestrator when all entities processed.

## archigraph MCP tool surface

The skill is built around the archigraph MCP server. The agent should call these tools directly (no shell-out to the `archigraph` CLI for read paths):

- `archigraph_whoami` — resolve the group/repo for the caller.
- `archigraph_find` — BM25-ranked query expanded by BFS; primary discovery tool. (Was `archigraph_search` before #668.)
- `archigraph_inspect` — look up an entity by id/qualified name/label. (Was `archigraph_describe` before #668.)
- `archigraph_expand` — depth-bounded neighbor expansion. (Was `archigraph_related` before #668.)
- `archigraph_trace` — confidence-weighted path between two nodes (cross-repo aware).
- `archigraph_traces` — process-flow query surface (`action=list|get|follow`); surfaces BFS call chains from entry points. Added in #724.
- `archigraph_clusters` — Louvain communities, used to seed module clustering in Pass 2. (Was `archigraph_list_clusters` before #668.)
- `archigraph_stats` — corpus-level metrics (used in Pass 1 inventory). (Was `archigraph_graph_stats` before #668.)
- `archigraph_get_source` — retrieve source-file snippet for a node.
- `archigraph_recent_activity` — list entities whose source files changed since a timestamp.
- `archigraph_save_finding` — persist a question/answer pair into the group memory directory.
- `archigraph_list_findings` — list previously saved findings, optionally filtered by entity or type.
- `archigraph_cross_links` — cross-repo link candidates (`action=list|accept|reject`). Replaces `archigraph_list_link_candidates` + `archigraph_resolve_link_candidate` (#668).
- `archigraph_enrichments` — enrichment candidates (`action=list|submit|reject`). Replaces `archigraph_list_enrichment_candidates` + `archigraph_submit_enrichment` + `archigraph_reject_enrichment` (#668).
- `archigraph_repairs` — residual-edge repair queue (`action=list|submit`). Used by Passes 1a, 1b, and the Pass 3a hook to annotate runtime-resolved edges per ADR-0015.
- `archigraph_patterns` — pattern store (`action=query|record|refine|apply|reject|promote`). Used by Passes 4, 10, 11, and 12 per ADR-0018.
- `archigraph_get_telemetry` — server uptime and per-tool counters (debugging only).

### Calling conventions

- `repo_filter="<repo_slug>"` scopes a call to a single repo. Default behavior infers the repo from caller CWD via `archigraph_whoami`.
- `repo_filter=null` (or omitted with `cwd` outside any registered repo) returns a summary across the whole group; use this for cross-group questions.
- `group=<name>` is only needed when the caller CWD is ambiguous or the user explicitly switched groups.
- Strip the `SCOPE.` prefix from any node-kind labels you print to the user (the schema uses `SCOPE.Component`, `SCOPE.Module`, etc., but agent-facing examples should say `Component`, `Module`).

## Output layout

For each repo `<r>` in the group, the skill writes into `<r>/docs/`:

```
docs/
  overview.md                  # Pass 3
  modules/
    <module-slug>/
      README.md                # Pass 4 (template: output-templates/module-readme.md)
      api.md                   # Pass 5
      flows.md                 # Pass 4
  reference/
    config.md
    deployment.md
    scripts.md
    dependencies.md
    misc.md
  how-to/
    local-dev.md
  glossary.md
```

Group-level output lands at `~/.archigraph/groups/<group>/docs/`:

```
docs/
  group-synthesis.md           # Pass 7
  cross-links.md               # Pass 8 summary
```

> **Note:** A doc-site (formerly Pass 9 VitePress config) is out of scope for this skill. It is planned for a separate milestone-2 effort. Pass 9 is intentionally reserved in the pass table.

## Conventions

The skill applies a stack-specific convention to every writer subagent. See `conventions/` for the registered conventions. Every convention requires `conventions/_graph-searchability.md` first, because that is the rule that makes documentation collide with code-symbol slugs in the graph (ADR-0007).

If the agent encounters a stack with no matching convention, it should stop and direct the user to run the `extend-convention` skill.

## Quality gates (snippets/verification-checklist.md)

Before any pass commits its output, the writer subagent runs the checks in `snippets/verification-checklist.md`. The orchestrator re-runs the same checklist before declaring the pass complete.

## Related

- `skills/extend-convention/SKILL.md` - companion skill for adding a new stack convention.
- `skills/archigraph-repair/SKILL.md` - standalone repair flow for ad-hoc residual cleanup outside doc generation.
- ADR-0015 (`docs/adrs/0015-residual-repair-agent-enrichment.md`) - residual repair foundation; powers Passes 1a, 1b, and 3a.
- `docs/specs/repair-trust-model.md` - allowlist + verification rules enforced by `archigraph_repairs(action=submit)`.
- ADR-0007 (`docs/adrs/0007-doc-as-bridge-for-cross-repo-and-dynamic-connections.md`) - why backticked code identifiers in headings matter.
- ADR-0008 - caller-CWD-aware routing, which is why `repo_filter` defaults work without the agent passing `cwd` explicitly.
