# generate-docs

Generate documentation for a registered archigraph group in two independent
**tiers**:

- **Technical tier** — module-organized markdown for every repo, stitched into a
  group-level synthesis with cross-repo links. For engineers. (Passes 0–14.)
- **Business tier** — a separate, group-synthesised `business/` doc set written
  for PMs / non-engineers: product capabilities, a business domain glossary,
  user journeys as plain-language narratives, and business rules reverse-
  engineered from the code. (Passes 15–19.) The webui surfaces this under its
  Business chooser tab (#1634).

The user picks one or both tiers. The two tiers are produced by separate pass
ranges and can run independently.

## Documentation tiers

Before planning, the orchestrator asks **which tier(s)** to generate:

> Generate **Technical** docs (per-repo engineering reference), **Business** docs
> (PM-facing product capabilities / journeys / rules synthesised across the
> group), or **both**?

The choice is recorded in `plan.json` `tiers: ["technical", "business"]`.

- **Technical only** → run Passes 0–8 (+10–14 as applicable). Skip 15–19.
- **Business only** → run Pass 0 (domain interview) and Pass 1 (inventory) for
  the graph signals the business passes need, then Passes 15–19. Skip 2–14
  except where a business pass reads a technical-tier file that does not exist —
  in that case the business writer falls back to graph queries (each business
  prompt names its fallback).
- **Both** → run the technical tier first (Passes 0–14), then the business tier
  (15–19); the business passes can reuse the just-written technical docs as
  input, which produces higher-fidelity business translation.

The tiers write to **disjoint locations** (technical: `~/.archigraph/docs/<group>/<repo-slug>/{overview,
modules,reference,…}`; business: `~/.archigraph/docs/<group>/business/…`), so they
never overwrite each other and the webui keeps them in separate views.

The **business tier is NOT per-repo.** It is synthesised across every repo in
the group and organised by business domain/capability. It is written into the
single group-level directory `~/.archigraph/docs/<group>/business/`, regardless
of how many repos make up the group. The webui surfaces this set directly under
the Business tab.

> **Storage location (#1624 — important).** The generate-docs skill NEVER
> writes into a source repo's working tree. All generated markdown (technical
> and business) lives under the archigraph-managed store
> `~/.archigraph/docs/<group>/...`. This keeps the source repos clean (no
> commit noise) and matches the #1626 principle that archigraph owns its
> outputs. The daemon dashboard reads from this location; pre-#1624
> `<repo>/docs/` directories produced by an earlier run are migrated into
> the store transparently on first read. If you ever see a writer subagent
> emitting a path that starts with the repo working tree, redirect it to
> the store path documented here.

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
| 14 | `prompts/14-frontmatter-validation.md` | Frontmatter validation pass: re-read every enriched doc file, parse its YAML frontmatter, and verify each field against the backend parser's expectations. Catches schema drift between the skill and the dashboard consumers. | 2–5 min |

### Business-tier passes (Pass 15 through Pass 19)

These run only when `"business"` is in `plan.tiers`. They are **group-synthesised**
(not per-repo) and write to `~/.archigraph/docs/<group>/business/`. Every business
writer reads `snippets/business-voice.md` first (PM audience, zero internal
symbols, no code mermaid). Pass 15 runs first (later passes link into the
glossary); Pass 19 runs last (it indexes the rest).

| Pass | Prompt | Purpose | Est. time |
|------|--------|---------|-----------|
| 15 | `prompts/15-business-domain.md` | Business domain model + glossary: business nouns (Inspection, Deficiency, Jurisdiction) defined in plain language, code collapsed to one term per concept. | 5–10 min |
| 16 | `prompts/16-business-capabilities.md` | Product capabilities: what the system does + why, derived from endpoints/flows/topics grouped by business outcome (a few capabilities, not one-per-endpoint). | 8–20 min |
| 17 | `prompts/17-business-journeys.md` | User journeys as plain-language narratives — a user accomplishing a goal end-to-end across the product. Replaces the audit's symbol-heavy mermaid "journey". | 5–15 min |
| 18 | `prompts/18-business-rules.md` | Business rules / requirements reverse-engineered from validation/permission/conditional logic, stated as product requirements. | 5–15 min |
| 19 | `prompts/19-business-overview.md` | Business landing page: pitch + indexes into capabilities, journeys, glossary, rules. Runs last. | 3–5 min |

**Total wall time:** typically **25–65 minutes** for small repos (1k entities), **1–2 hours** for medium repos (10k entities), **2–4 hours** for large repos (100k+ entities). Pass 4 parallelizes across module clusters, so the critical path is dominated by Pass 0 (user interaction), Passes 1–2 (discovery), and Passes 4–5 (content generation).

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

### Model selection — Haiku default, Sonnet for critical tier

**Never use Sonnet (or Opus) for the full enrichment corpus.** At Sonnet rates, a 5,800-entity run costs $70+ and takes 3–8 hours. Use the following tiered model selection keyed on each candidate's `criticality_band` field (set by `ComputeScore` in the indexer, sourced from `internal/enrichment/candidates.go`):

| `criticality_band` | Score | Model | Rationale |
|--------------------|-------|-------|-----------|
| `critical` | ≥ 80 | `claude-3-5-sonnet-20241022` | High-traffic endpoints, god nodes, articulation points — deeper analysis justified |
| `high` | 60–79 | `claude-3-haiku-20240307` | Important but not business-critical; Haiku sufficient |
| `medium` | 40–59 | `claude-3-haiku-20240307` | Moderate signal; Haiku sufficient |
| `low` | < 40 | `claude-3-haiku-20240307` | Marginal enrichment value; Haiku only |

**Never mix tiers in the same batch** — different tiers go to different models.

> **Agent host guidance:** See [docs/agent-hosts.md](../../docs/agent-hosts.md) for how to set Haiku as the active model in Claude Code, Cursor, and Windsurf before starting Pass 13.

### Batching — 20–50 entities per LLM call

Do not enrich one entity per call. Batch 20–50 entity stubs per LLM call to amortise per-call overhead.

**Batch size guidance:**
- Haiku batches: 30–50 entities (default 40)
- Sonnet batches: 20–30 entities (larger context per entity; default 25)
- Reduce batch size if the LLM returns truncated or incomplete results

**Entity stub shape** (include in each batch payload):

```json
{
  "entity_id": "ep-abc123",
  "kind": "http_endpoint",
  "name": "GET /api/orders",
  "criticality_band": "critical",
  "score": 82,
  "context": {
    "caller_count": 14,
    "callee_count": 3,
    "is_god_node": true,
    "neighbours": ["OrderService::create", "PaymentGateway::charge"]
  }
}
```

Populate `context` from `archigraph_expand(node="<entity_id>", depth=2)`. Include `caller_count`, `callee_count`, `is_god_node`, and up to 5 neighbour names.

**Batch request shape:**

```json
{
  "model": "claude-3-haiku-20240307",
  "batch_id": "pass13-batch-007",
  "entities": [
    { "entity_id": "...", "kind": "...", "name": "...", "context": { ... } }
  ],
  "instructions": "For each entity, emit only its YAML frontmatter block (--- delimited). Do not emit prose. Fill summary, rank, group, group_label, and gaps at minimum. Emit kind-appropriate per-kind fields only."
}
```

**Batch response shape** (what the LLM should return):

```json
{
  "batch_id": "pass13-batch-007",
  "results": [
    {
      "entity_id": "ep-abc123",
      "frontmatter": "---\nentity_id: ep-abc123\nkind: http_endpoint\nsummary: '...'\n..."
    }
  ],
  "skipped": ["ep-xyz999"],
  "error": null
}
```

After receiving results, write each `frontmatter` block to the entity's doc file (prepend; do not replace existing prose) and submit via `archigraph_enrichments(action=submit, ...)`.

### Resume semantics — safe to restart

Pass 13 is **idempotent**. Before assembling batches:

```
# 1. Fetch already-enriched entity IDs.
archigraph_enrichments(action=list, status="enriched")

# 2. Exclude those entity_ids from all batches.
#    This makes Pass 13 safe to restart after any failure without re-enriching
#    already-completed entities or incurring duplicate LLM costs.
```

If the daemon restarts mid-run, re-invoke Pass 13 — it will resume from where it left off.

### What Pass 13 does (per entity kind)

For every entity of kind `http_endpoint`, `process_flow`, or `message_topic`, the enrichment subagent:

1. **Merges** near-duplicates — identifies same logical entity in two places; emits `merged_into` on the redundant one.
2. **Disqualifies** false positives — marks regex-style noise entities with `disqualified: true`.
3. **Ranks** by importance — infers a 0..1 score from traffic signals (inbound caller count, business heuristic).
4. **Groups** by domain — LLM-inferred cluster key (e.g. `orders`, `auth`, `inventory`) + human-readable `group_label`.
5. **Summarises** — writes a one-sentence natural-language summary.
6. **Detects gaps** — lists structural problems (missing auth, missing error response, orphan producer, etc.).

The subagent writes the enrichment as YAML frontmatter at the **top** of the entity's existing doc file. If no doc file exists for the entity, a minimal file is created containing only the frontmatter block.

### Doc file discovery convention

The backend parsers locate enriched doc files via `docgen-state.json` (`GeneratedPaths` list). Each parser applies a two-pass lookup:

**For `message_topic` (Topology panel — `applyTopologyEnrichment`):**
1. Pass 1 (fast): any `GeneratedPaths` entry whose path contains the entity ID as a substring.
2. Pass 2 (fallback): any path containing `"topic"` or `"topology"` (case-insensitive) whose frontmatter has `kind: message_topic`. Used for hashed entity IDs where the path alone cannot match.

**For `process_flow` (Flows panel — `extractFlowDocsWithResolver`):**
1. Fast path: any path containing the entity ID or the word `"flow"` (case-insensitive) whose frontmatter has `kind: process_flow` or no `kind` set.
2. Tertiary pass: scan all paths for frontmatter whose `entity_id` field matches the entity ID (handles hashed IDs).

**For `http_endpoint` (Paths panel):** same entity-ID substring match; frontmatter `kind: http_endpoint` is used to reject wrong-kind docs.

When no match is found, the parser falls back to scanning the first non-heading non-empty line of the file as a plain-text summary.

**Canonical file placement** (when no prior doc exists for the entity):

```
~/.archigraph/docs/<group>/<repo-slug>/enrichments/<kind>/<entity_id>.md
```

Example: `api/docs/enrichments/message_topic/order-created.md`

This path will contain `"topic"` and the entity ID, making both lookup passes reliable.

### `docgen_status` states

The dashboard exposes a `docgen_status` field on every entity row:

| Status | Meaning |
|--------|---------|
| `enriched` | A doc file with valid YAML frontmatter (`kind` + `summary` present) was found and parsed. |
| `stale` | A doc file exists but its frontmatter is absent or has no `kind`/`summary` (legacy plain-prose file). For `message_topic`, stale is also set when the doc file's `mtime` is older than the topic's last index timestamp. |
| `pending` | No doc file found for this entity in `GeneratedPaths`. |

The skill should aim to move all entities from `pending` or `stale` to `enriched` by the end of Pass 13. Re-running Pass 13 on a `stale` entity re-emits the frontmatter block.

### Unified frontmatter schema

Every enriched entity doc file starts with a YAML frontmatter block delimited by `---`. **All fields are optional** — omit any field the LLM cannot determine with confidence. The dashboard backend falls back to first-line prose summary when frontmatter is absent.

The per-kind templates in `templates/` give copy-paste starting points. Example completed files are in `examples/`.

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
# Prose-enrichment fields (consumed by Paths detail panel):
parameters_explained: 'page and limit control pagination; limit is capped at 200 server-side'
response_shapes_explained: 'orders array contains Order objects with id, total, status, created_at'
examples: 'GET /api/orders?page=2&limit=20 — returns second page of 20 orders'
caveats: 'Soft-deleted orders are excluded unless ?include_deleted=true is passed'

# ── process_flow-specific fields ─────────────────────────────────────────────
steps:
  - Validate cart contents and check stock
  - Charge payment method via payment service
  - Persist order record to database
  - Emit order.created event to broker
preconditions: 'User is authenticated and cart is non-empty'
expected_outcome: 'Order persisted, confirmation email dispatched, inventory decremented'
# Prose-enrichment fields (consumed by Flows detail panel):
examples: 'Happy path: user checks out 3 items, payment succeeds, order.created emitted'
caveats: 'Stock check is advisory — race conditions possible under high load'

# ── message_topic-specific fields ────────────────────────────────────────────
purpose: 'Signals that a new order was placed; consumed by fulfillment, analytics, and notifications'
schema: '{ order_id: string, total: float, items: OrderItem[], user_id: string }'
typical_payload_size_bytes: 512
volume_estimate: high          # low | medium | high | very-high
expected_consumers: [order-fulfillment, analytics, notifications]
# Prose-enrichment fields (consumed by Topology detail panel):
examples: 'order.created published after checkout with order_id and items array'
caveats: 'Schema version 2 — consumers must handle missing discount_code field from v1 payloads'
---

## Description

Free-form prose continues here (existing content unchanged below this block).
```

> **Field selection rules**
> - Emit only `kind`-relevant per-kind fields. Do not emit `steps` for an `http_endpoint`; do not emit `method`/`path` for a `message_topic`.
> - Omit `rank` when you have no signal; do not fabricate a number.
> - `disqualified: true` suppresses the entity from the default dashboard view; only set it when clearly a false positive.
> - `merged_into` must reference an `entity_id` that exists in the same group.
> - `gaps` entries should be actionable (the user can act on them); avoid tautological observations.
> - `purpose` (message_topic) is distinct from `summary`: `summary` is a one-sentence overview for list views; `purpose` explains the business reason the topic exists, used in the detail panel.
> - `parameters_explained`, `response_shapes_explained`, `examples`, `caveats` are freeform prose fields consumed by the detail panel. They are not parsed structurally — write them as natural language.

### Per-kind field matrix

The backend parser (`enrichment_frontmatter.go`) reads all of these fields. Fields marked **consumed** are actively surfaced in the dashboard; **parsed** means they are stored but not yet rendered.

| Field | `http_endpoint` | `process_flow` | `message_topic` | Backend field |
|-------|----------------|----------------|-----------------|--------------|
| `entity_id` | consumed | consumed | consumed | `EntityID` |
| `kind` | consumed | consumed | consumed | `Kind` |
| `disqualified` | consumed | consumed | consumed | `Disqualified` |
| `merged_into` | consumed | consumed | consumed | `MergedInto` |
| `rank` | consumed | consumed | consumed | `Rank` |
| `group` | consumed | consumed | consumed | `Group` |
| `group_label` | consumed | consumed | consumed | `GroupLabel` |
| `summary` | consumed | consumed | consumed | `Summary` |
| `gaps` | consumed | consumed | consumed | `Gaps` |
| `method` | consumed | — | — | `Method` |
| `path` | consumed | — | — | `Path` |
| `parameters` | parsed | — | — | `Parameters` |
| `responses` | parsed | — | — | `Responses` |
| `auth` | consumed | — | — | `Auth` |
| `tables_touched` | consumed | — | — | `TablesTouched` |
| `parameters_explained` | prose | — | — | *(prose field — not parsed by backend)* |
| `response_shapes_explained` | prose | — | — | *(prose field — not parsed by backend)* |
| `steps` | — | consumed | — | `Steps` |
| `preconditions` | — | consumed | — | `Preconditions` |
| `expected_outcome` | — | consumed | — | `ExpectedOutcome` |
| `purpose` | — | — | prose | *(prose field — not parsed by backend)* |
| `schema` | — | — | consumed | `Schema` |
| `typical_payload_size_bytes` | — | — | consumed | `TypicalPayloadSizeBytes` |
| `volume_estimate` | — | — | consumed | `VolumeEstimate` |
| `expected_consumers` | — | — | consumed | `ExpectedConsumers` |
| `examples` | prose | prose | prose | *(prose field — not parsed by backend)* |
| `caveats` | prose | prose | prose | *(prose field — not parsed by backend)* |

> **Prose fields** (`parameters_explained`, `response_shapes_explained`, `purpose`, `examples`, `caveats`) are scalar string values stored in the YAML block for documentation completeness. The backend parser does not currently map them to struct fields — they pass through as unrecognised keys and are ignored. The dashboard detail panels read them from the raw frontmatter when the backend returns the full `enrichment` object. Emit them only when you have concrete information.

### `enrichment_health` per kind

The detail panels expose an `enrichment_health` object that reports which structured fields are filled. The fields checked differ per kind:

**`message_topic` (Topology detail panel — `computeEnrichmentHealth`):**
- `has_summary` — `summary` present
- `has_schema` — `schema` present
- `has_volume_estimate` — `volume_estimate` present
- `has_typical_payload_size` — `typical_payload_size_bytes` > 0
- `has_expected_consumers` — `expected_consumers` non-empty
- `has_gaps` — `gaps` non-empty
- `filled_field_count` / `total_field_count` (total = 6)

**`process_flow` (Flows detail panel — `enrichmentHealth`):**
- `summary` — `summary` present
- `preconditions` — `preconditions` present
- `expected_outcome` — `expected_outcome` present
- `steps` — `steps` non-empty
- `gaps` — `gaps` non-empty

Aim to populate all health-tracked fields when writing Pass 13 output.

### Pass 13 procedure

```
# ── Step 0: Resume check ─────────────────────────────────────────────────────
# Fetch already-enriched IDs to skip (idempotent restart).
already_enriched = archigraph_enrichments(action=list, status="enriched")
skip_ids = set(c.entity_id for c in already_enriched)

# ── Step 1: Collect candidates ───────────────────────────────────────────────
# Pull from the pre-computed enrichment queue (includes criticality_band + score).
candidates_ep   = archigraph_enrichments(action=list, kind="http_endpoint",  status="pending")
candidates_flow = archigraph_enrichments(action=list, kind="process_flow",   status="pending")
candidates_mt   = archigraph_enrichments(action=list, kind="message_topic",  status="pending")
all_candidates  = candidates_ep + candidates_flow + candidates_mt

# Filter already-done.
todo = [c for c in all_candidates if c.entity_id not in skip_ids]

# ── Step 2: Build entity stubs ───────────────────────────────────────────────
# For each candidate, expand neighbours for context.
# archigraph_expand(node="<entity_id>", depth=2)
# Include: caller_count, callee_count, is_god_node, up to 5 neighbour names.

# ── Step 3: Partition by criticality_band ────────────────────────────────────
critical_todo = [c for c in todo if c.criticality_band == "critical"]   # → Sonnet
other_todo    = [c for c in todo if c.criticality_band != "critical"]   # → Haiku

# ── Step 4: Print cost estimate before dispatching ───────────────────────────
# Rough estimate: critical × 800 tok, other × 500 tok.
# Print: "Enriching N entities (~$X). Proceed? [y/N]"
# Gate on user confirmation (or --yes flag for automation).

# ── Step 5: Dispatch Haiku batches (other_todo) ──────────────────────────────
# Batch size: 40 entities per call.
# Model: claude-3-haiku-20240307
for batch in chunks(other_todo, size=40):
    results = call_llm(model="claude-3-haiku-20240307", entities=batch)
    for r in results:
        write_frontmatter(r.entity_id, r.frontmatter)   # prepend; do not replace prose
        archigraph_enrichments(action=submit, entity_id=r.entity_id,
                               summary=r.summary, kind=r.kind)

# ── Step 6: Dispatch Sonnet batches (critical_todo) ──────────────────────────
# Batch size: 25 entities per call.
# Model: claude-3-5-sonnet-20241022
for batch in chunks(critical_todo, size=25):
    results = call_llm(model="claude-3-5-sonnet-20241022", entities=batch)
    for r in results:
        write_frontmatter(r.entity_id, r.frontmatter)
        archigraph_enrichments(action=submit, entity_id=r.entity_id,
                               summary=r.summary, kind=r.kind)

# ── Step 7: Verify ───────────────────────────────────────────────────────────
# Run snippets/verification-checklist.md for a sample (≥10 entities per tier).
# Hand back to orchestrator.
```

After writing, run `snippets/verification-checklist.md` for a sample of at least 10 entities per criticality tier. Hand back to the orchestrator when all entities are processed or when a partial batch failure has been documented.

## Pass 14 — Frontmatter validation pass

Pass 14 validates the YAML frontmatter emitted by Pass 13 against the backend parser's expectations. It catches schema drift (a field renamed in Go, a new required field added to the health check) before the user notices a blank panel in the dashboard.

### When to run Pass 14

Run Pass 14 immediately after Pass 13 completes. It is mandatory when enriching a group for the first time and recommended after any archigraph upgrade that mentions changes to `enrichment_frontmatter.go`.

### What Pass 14 checks

For every doc file in `GeneratedPaths` that contains a frontmatter block:

1. **Structural validity** — the file opens with `---` on line 1 and has a matching closing `---`.
2. **Required universal fields** — `kind` is one of `http_endpoint`, `process_flow`, `message_topic`.
3. **Kind isolation** — no cross-kind fields present (e.g. `steps` on an `http_endpoint`).
4. **Rank bounds** — if `rank` is present, it is a float in `[0, 1]`.
5. **`merged_into` integrity** — if non-empty, the referenced `entity_id` appears in another doc file in the same group.
6. **Health-tracked field coverage** — for each entity kind, report which health-tracked fields are missing:
   - `message_topic`: `summary`, `schema`, `volume_estimate`, `typical_payload_size_bytes`, `expected_consumers`, `gaps`.
   - `process_flow`: `summary`, `preconditions`, `expected_outcome`, `steps`, `gaps`.
7. **Discovery-path reachability** — the doc file's path is listed in `docgen-state.json`'s `GeneratedPaths`, ensuring the backend can locate it.

### Pass 14 output

The pass emits a validation report as a finding:

```
archigraph_save_finding(
  question="Pass 14 frontmatter validation report",
  answer="<summary of pass/fail counts, list of files with issues>",
  type="enrichment_validation",
)
```

Any entity with validation failures is **not** marked `enriched`; the pass sets those entities back to `pending` in the finding so a re-run of Pass 13 knows to revisit them.

Pass 14 does **not** modify doc files directly — it only reports. The user must re-run Pass 13 for any entity with failures.

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

For each repo `<r>` in the group, the skill writes into
`~/.archigraph/docs/<group>/<r>/` (NOT the repo working tree — #1624):

```
~/.archigraph/docs/<group>/<r>/
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
  enrichments/                 # Pass 13 — created only for entities with no prior doc file
    http_endpoint/
      <entity_id>.md           # template: templates/http_endpoint.md
    process_flow/
      <entity_id>.md           # template: templates/process_flow.md
    message_topic/
      <entity_id>.md           # template: templates/message_topic.md
```

Group-level technical output (cross-repo synthesis + cross-links) lands at
`~/.archigraph/docs/<group>/` (alongside the per-repo directories):

```
~/.archigraph/docs/<group>/
  group-synthesis.md           # Pass 7
  cross-links.md               # Pass 8 summary
```

### Business tier layout

The business tier (Passes 15–19) is group-synthesised and written to the
single group-level directory `~/.archigraph/docs/<group>/business/` (no
`primary_repo` indirection any more — #1624). The webui (#1634) reads this
set and surfaces it under its Business chooser tab.

```
~/.archigraph/docs/<group>/business/
  overview.md                  # Pass 19 — landing page + indexes (template: business-overview.md)
  capabilities/
    <capability-slug>.md       # Pass 16 — one per product capability (template: business-capability.md)
  domain-glossary.md           # Pass 15 — business vocabulary (template: business-domain-glossary.md)
  journeys/
    <journey-slug>.md          # Pass 17 — plain-language user journeys (template: business-journey.md)
  rules/
    index.md                   # Pass 18 — business rules / requirements (template: business-rules.md)
    <area>.md                  # optional — only if one area's rules outgrow index.md
```

Every business page is reverse-engineered from code, so each carries a collapsed
`<details>` provenance block at the bottom (the ONLY place a symbol/file path may
appear in a business page) so an engineer can audit it without polluting the PM
reading. All other content is plain business language with zero internal symbol
names per `snippets/business-voice.md`.

> **Note:** A doc-site (formerly Pass 9 VitePress config) is out of scope for this skill. It is planned for a separate milestone-2 effort. Pass 9 is intentionally reserved in the pass table.

## Conventions

The skill applies a stack-specific convention to every writer subagent. See `conventions/` for the registered conventions. Every convention requires `conventions/_graph-searchability.md` first, because that is the rule that makes documentation collide with code-symbol slugs in the graph (ADR-0007).

If the agent encounters a stack with no matching convention, it should stop and direct the user to run the `extend-convention` skill.

## Quality gates (snippets/verification-checklist.md)

Before any pass commits its output, the writer subagent runs the checks in `snippets/verification-checklist.md`. The orchestrator re-runs the same checklist before declaring the pass complete. The checklist incorporates three contracts that fix the 2026-05-23 docgen audit defects:

- **`snippets/anchor-contract.md`** — emitted `anchors:` frontmatter is *derived from* the headings the writer actually produced, never hand-authored. Fixes the 17 anchor-slug mismatches (`summary` declared vs `## Where it lives` written). Enforced per-file in the checklist and group-wide in Pass 8.
- **`snippets/link-hygiene.md`** — link targets must be real generated doc files; never link into source-code directories; bare-directory links need a real index target; relative paths use the filesystem dirname (not the registry slug) to avoid the `upvate-core`/`upvate_core` 404 class. Fixes the 37 broken links.
- **Volume control** (`prompts/02-plan.md` § Step 1b) — merge thin dir-derived modules, cap module count by LOC, and never schedule empty stub pages (`flows/index.md`). With archigraph #1620 communities now persisting, the plan prefers real graph communities over directory fallback. Fixes the 122-module over-fragmentation.
- **`snippets/business-voice.md`** — the business-tier style contract (PM audience, zero symbols, no code mermaid). Fixes the symbol-heavy "user-journeys.md" that read as technical, not business.

## Docgen Repair Feedback Contract

As each writer pass (Passes 3–6, 12, and 3a) reads source and reasons about the code it is describing, it may discover facts the static extractor missed. When a writer makes such a discovery it **MUST** emit a repair candidate so Fidelity climbs toward 100% over successive runs.

### When to emit

A writer SHOULD emit a candidate whenever it:

- Resolves an unresolved stub to a real entity (e.g. reads that `UserService` is imported from `./services/user.ts` and can pin the edge).
- Observes a dynamic-dispatch call site that it can reason about from source context.
- Recognises an unresolved stub as a well-known external library (e.g. `stripe`, `aws-sdk`).
- Notices two flow entities that represent the same business flow (merge candidate).
- Determines that an entity's Kind is mis-classified.

### How to emit

Append one JSON object per line to `docgen-repairs.jsonl` in the repo's state directory. Use `archigraph_apply_docgen_repairs` to find the path or ask the user. The record shape:

```json
{
  "type": "resolve_ref | add_edge | fix_kind | label_external | merge_flow",
  "source_entity_id": "<hex entity id>",
  "target": "<target id or ext:module or merge-target id>",
  "edge_kind": "CALLS",
  "new_kind": "Service",
  "confidence": 0.9,
  "evidence": "auth.go@line 42: import from ./services/user",
  "source": "generate-docs/pass-3a",
  "emitted_at": "2026-05-23T12:00:00Z"
}
```

Field rules (enforced by `internal/enrichment.DocgenRepairCandidate.Validate()`):

| Field | Required | Notes |
|-------|----------|-------|
| `type` | always | one of `resolve_ref`, `add_edge`, `fix_kind`, `label_external`, `merge_flow` |
| `source_entity_id` | always | the entity this repair applies to |
| `target` | all except `fix_kind` | resolved id, qualified name, or `ext:<module>` |
| `edge_kind` | for `add_edge` | relationship kind, e.g. `CALLS` |
| `new_kind` | for `fix_kind` | replacement Kind string |
| `confidence` | always | 0.0–1.0; use 0.9+ only when source is unambiguous |
| `evidence` | always | `"<file>@line N: <observation>"` — no multi-line strings |
| `source` | optional | which pass emitted this, e.g. `"generate-docs/pass-3a"` |

### Apply path

After the docgen run completes, call `archigraph_apply_docgen_repairs` (no required parameters). The daemon reads `docgen-repairs.jsonl` and:

- **Confidence ≥ 0.8 (high)** → applied immediately as enrichment resolutions in `enrichment-resolutions.json`; reflected on next daemon reload.
- **Confidence < 0.8 (low)** → queued in `docgen-repairs-pending.json` for human review; surfaced in the dashboard Pending tab.

The tool returns `fidelity_before`, `fidelity_after`, and `fidelity_delta` per repo so the effect of the docgen run is visible.

### Fidelity in `archigraph_stats`

After applying, `archigraph_stats` now includes `fidelity` (0–1 ratio), `fidelity_import_total`, and `fidelity_import_bug` so callers can track progress without re-running the quality endpoint.

## Related

- `skills/extend-convention/SKILL.md` - companion skill for adding a new stack convention.
- `skills/archigraph-repair/SKILL.md` - standalone repair flow for ad-hoc residual cleanup outside doc generation.
- ADR-0015 (`docs/adrs/0015-residual-repair-agent-enrichment.md`) - residual repair foundation; powers Passes 1a, 1b, and 3a.
- `docs/specs/repair-trust-model.md` - allowlist + verification rules enforced by `archigraph_repairs(action=submit)`.
- ADR-0007 (`docs/adrs/0007-doc-as-bridge-for-cross-repo-and-dynamic-connections.md`) - why backticked code identifiers in headings matter.
- ADR-0008 - caller-CWD-aware routing, which is why `repo_filter` defaults work without the agent passing `cwd` explicitly.
- `internal/dashboard/enrichment_frontmatter.go` - backend YAML frontmatter parser; source of truth for which fields are consumed vs. ignored.
- `internal/dashboard/handlers_topology.go` `applyTopologyEnrichment` - Topology panel enrichment lookup (PR #1182).
- `internal/dashboard/handlers_topology_detail.go` `computeEnrichmentHealth` - health field coverage check for `message_topic`.
- `internal/dashboard/handlers_flows.go` `extractFlowDocsWithResolver` + `enrichmentHealth` - Flows panel enrichment lookup and health check (PR #1181).
- `skills/generate-docs/templates/` - per-kind frontmatter template files for Pass 13.
- `skills/generate-docs/examples/` - fully-populated example enriched doc files per kind.
- `docs/agent-hosts.md` - how to configure Haiku in Claude Code, Cursor, and Windsurf before running Pass 13 (see #1288).
