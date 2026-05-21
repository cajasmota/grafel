---
name: using-archigraph
description: >
  Teaches an AI agent how to use archigraph MCP tools effectively when working
  on a registered codebase. Covers tool selection, Pass-based orientation
  workflows, worked examples, and explicit anti-patterns.
type: behavior
when-to-use: >
  Invoke when you open a codebase that has archigraph indexed, when you are
  about to navigate a large or unfamiliar codebase, or when you are asked a
  structural question (trace a flow, find callers, understand module layout)
  and the archigraph daemon is running. Do NOT invoke for single-symbol lookups
  or for codebases that have never been indexed.
---

# using-archigraph

A practical guide for AI agents working in an archigraph-registered codebase.
This skill covers when to use archigraph vs. grep, which of the 32 MCP tools
to call for which task, Pass-based workflows, and hard anti-patterns.

---

## 1. Should I use archigraph at all?

Use archigraph when the answer requires **graph traversal** — understanding
relationships between entities, tracing call chains, discovering callers,
mapping module clusters. Reach for grep/Read when you need **text search** —
find where a string literal appears, read a specific file, check a config value.

### Decision matrix

| Task | Tool |
|------|------|
| "What calls `PaymentService.charge`?" | `archigraph_find` + `archigraph_expand` |
| "Where is the string `PAYMENT_GATEWAY_URL` used?" | `grep` / `rg` |
| "Trace from the checkout route to the database write" | `archigraph_trace` or `archigraph_traces` |
| "Read the implementation of `OrderSerializer`" | `archigraph_get_source` |
| "Which modules exist in this repo?" | `archigraph_clusters` |
| "What changed in the last hour?" | `archigraph_recent_activity` |
| "How many entities does this group have?" | `archigraph_stats` |
| "Find where `ORDER_STATUS_PENDING` is defined" | `grep` / `rg` |
| "Which HTTP endpoints does the frontend call?" | `archigraph_endpoints(action=calls)` |
| "Are there any orphan call-sites (calls with no handler)?" | `archigraph_endpoints(action=calls, orphan_only=true)` |
| "What is the overall architecture of this group?" | `archigraph_clusters` + `archigraph_stats` |
| "What is `OrderViewSet`'s source?" | `archigraph_get_source` |

**Skip archigraph entirely** when:
- The question is answered by reading a single known file.
- You only need to search for a literal string or regex pattern.
- The repo has never been indexed (check: `archigraph_whoami` returns `source: "none"`).
- The task takes fewer steps via grep than via graph query.

---

## 2. Orientation (Pass 0 — always run first)

Before doing anything else in an unfamiliar codebase, run a three-call
orientation sequence. This costs ~200 tokens and prevents dozens of wasted
traversals.

```
# Step 1: Am I in a registered group? Which repo am I in?
archigraph_whoami()

# Step 2: How big is this group? Any repos unavailable?
archigraph_stats()

# Step 3: What are the major subsystems (Louvain communities)?
archigraph_clusters()
```

From these three calls you learn:
- The group name, the active repo, and any sibling repos.
- Total entity + relationship counts per repo (ballpark for scoping decisions).
- The top-3 entities in each community, which is a fast module map.

**Only proceed to deeper passes once you know the landscape.**

---

## 3. Tool catalogue (all 19 tools)

### 3.1 Orientation tools

#### `archigraph_whoami`
Resolves the group and repo for your current working directory. Call this
first in every session. Returns `source` (`cwd-marker`, `singleton`, `none`)
so you know how confident the resolution is.

```
archigraph_whoami()
→ { "group": "orders-platform", "repo": "orders-api", "source": "cwd-marker" }
```

CLI equivalent: `archigraph status`

#### `archigraph_stats`
Corpus-level counts: entities, relationships, communities per repo, plus any
repos that failed to load. Use to scope token budgets — a 50k-entity repo
needs tighter `token_budget` limits than a 2k-entity one.

```
archigraph_stats()
→ { "entities": 8432, "relationships": 41200, "cross_repo_links": 23, "repos": [...] }
```

CLI equivalent: `archigraph stats` (not yet implemented; use MCP)

#### `archigraph_clusters`
Returns Louvain communities pre-computed during indexing. Each cluster has a
`size`, a `modularity` score, and `top_entities` — the highest-PageRank
entities in the cluster. Use to discover module names you can then target with
`archigraph_find`.

```
archigraph_clusters(repo_filter=["orders-api"])
→ [{ "id": 3, "size": 47, "modularity": 0.41, "top_entities": ["OrderViewSet", ...] }]
```

---

### 3.2 Discovery tools

#### `archigraph_find`
BM25-ranked graph query, optionally BFS-expanded from each top hit. The
primary discovery tool — reach for it when you know what you are looking for
but not where it lives.

Key parameters:
- `question` — natural-language query (BM25 tokenises it against entity labels
  and qualified names).
- `depth` — BFS hops from each match (default 3; use 1 for a tight result set,
  0 to skip BFS entirely).
- `token_budget` — max approximate tokens in compact output (default 800;
  lower for orientation, higher for deep inventory).
- `repo_filter` — restrict to one or more repos by slug.
- `context_filter` — restrict BFS traversal to specific edge kinds
  (`CALLS`, `IMPORTS`, `PUBLISHES_TO`, etc.).

```
archigraph_find(question="payment processing charge refund", depth=2, token_budget=600)
archigraph_find(question="order HTTP routes endpoints", repo_filter=["orders-api"], depth=1)
archigraph_find(question="auth token validation middleware", context_filter=["CALLS"], depth=3)
```

CLI equivalent: `archigraph search "payment processing"` (thin RPC wrapper)

#### `archigraph_inspect`
Look up a single entity by ID, prefixed cross-repo ID, qualified name, or
label. Use when `archigraph_find` gave you an ID and you want the full
structured record (file, line range, PageRank, community, properties). Also
auto-attaches any saved findings that reference this entity.

```
archigraph_inspect(label_or_id="OrderViewSet")
archigraph_inspect(label_or_id="orders-api::a1b2c3d4e5f60718")
```

---

### 3.3 Traversal tools

#### `archigraph_expand`
BFS neighbour expansion from a single node. Use after `archigraph_inspect` to
walk outward from a known entity. Returns cross-repo overlay edges when they
exist.

```
archigraph_expand(node="OrderViewSet", depth=2)
archigraph_expand(node="orders-api::a1b2c3d4e5f60718", depth=1)
```

CLI equivalent: `archigraph expand <id>` (partial; MCP preferred)

#### `archigraph_trace`
Confidence-weighted shortest path (Dijkstra) between two nodes. Use when you
want to verify that a known source entity connects to a known target entity,
and you want to see the exact edge sequence. Cross-repo aware.

```
archigraph_trace(source="CheckoutController", target="PaymentsTable")
→ { "path": ["CheckoutController","ChargeService","PaymentsRepository","PaymentsTable"],
    "edges": [{"kind":"CALLS"},{"kind":"CALLS"},{"kind":"USES"}],
    "weakest_link_confidence": 0.7, "crosses_repos": false, "found": true }
```

CLI equivalent: `archigraph trace <source> <target>`

#### `archigraph_traces`
Pre-computed process flow traces. The indexer runs BFS from heuristically
detected entry points (route handlers, `main`, framework lifecycle hooks) and
stores linearised call chains as `Process` entities. Three sub-actions:

- `list` — top-ranked processes (optionally cross-stack-only).
- `get` — full step chain for a specific `process_id`.
- `follow` — ad-hoc forward BFS from any entity (not just pre-computed entry
  points). Useful for probing flows the indexer did not auto-detect.

```
# Discover what complex flows exist
archigraph_traces(action="list", cross_stack_only=true, limit=10)

# Inspect a specific flow in full
archigraph_traces(action="get", process_id="orders-api::proc:df0cd633e7f8f7f4")

# Probe a custom entry point
archigraph_traces(action="follow", entry_point_id="InvoiceController", max_depth=6)
```

---

### 3.4 Source tools

#### `archigraph_get_source`
Returns the actual source lines for a node, with configurable context lines.
Use after identifying an entity via `archigraph_find` or `archigraph_inspect`
when you need to read the implementation, not just its graph metadata.

```
archigraph_get_source(node_id="OrderViewSet", context_lines=30)
→ line-numbered source text from core/views/order.py:42
```

CLI equivalent: read the file manually at the reported `source_file:start_line`

#### `archigraph_recent_activity`
Returns entities whose source files were modified after a given RFC3339
timestamp. Use at the start of an investigation when you need to find what
changed recently (e.g. after a deploy, or to scope a code-review).

```
archigraph_recent_activity(since="2026-05-20T00:00:00Z", limit=20)
```

---

### 3.5 HTTP endpoint tools

`archigraph_endpoints` (#1281 consolidation of endpoint_definitions + endpoint_calls + endpoint_stats):

#### `archigraph_endpoints(action=definitions)`
Lists HTTP endpoint handler/route definitions (`http_endpoint_definition`
kind). Use to audit what server-side routes exist.

```
archigraph_endpoints(action="definitions", repo_filter=["orders-api"])
→ { "definitions": [{ "entity_id": "...", "method": "POST", "path": "/api/v1/orders" }], "count": 7 }
```

#### `archigraph_endpoints(action=calls)`
Lists client-side call-sites (`http_endpoint_call` kind). Use to find what
HTTP calls clients make, and to surface orphan callers (calls with no matching
definition in the group).

```
archigraph_endpoints(action="calls", repo_filter=["mobile-app"])
archigraph_endpoints(action="calls", orphan_only=true)  # only unmatched calls
```

#### `archigraph_endpoints(action=stats)`
Counts definitions vs. calls vs. legacy entities vs. orphan callers per repo.
Use to assess migration progress or quickly understand the HTTP surface size.

```
archigraph_endpoints(action="stats")
→ { "totals": { "definitions": 12, "calls": 8, "orphan_calls": 2 }, "migrated": true }
```

---

### 3.6 Queue management tools

#### `archigraph_enrichments`
Manages enrichment candidates — LLM-enrichable entities (`http_endpoint`,
`process_flow`, `message_topic`) pre-identified by the indexer. Use when you
want to submit or review structured metadata (summary, rank, gaps) for
dashboard surfaces.

```
archigraph_enrichments(action="list", kind="http_endpoint", limit=10)
archigraph_enrichments(action="submit", candidate_id="ec-1", value="Handles order creation", confidence=0.9)
archigraph_enrichments(action="reject", candidate_id="ec-2", reason="False positive — test fixture")
```

#### `archigraph_cross_links`
Manages cross-repo link candidates detected by the indexer. Use when you want
to confirm, override, or reject candidate edges between repos.

```
archigraph_cross_links(action="list", repo_filter=["mobile-app"], limit=5)
archigraph_cross_links(action="accept", candidate_id="lc-abc123")
archigraph_cross_links(action="reject", candidate_id="lc-def456", reason="Same-repo false positive")
```

#### `archigraph_repairs`
Manages residual-edge repair queue (unresolved stubs from the indexer).
Use during repair sessions to annotate stubs with their correct resolutions.
See the `/archigraph-repair` skill for the full flow.

```
archigraph_repairs(action="list", repo_filter=["orders-api"], limit=20)
archigraph_repairs(action="submit", residual_id="er:deadbeef00000001",
                   resolution="bind_to_entity", target_entity_id="a1b2c3d4e5f60718",
                   confidence=0.95)
```

---

### 3.7 Memory tools

#### `archigraph_save_finding`
Persists a Q/A pair to the group's memory directory. Use to record decisions,
insights, or cross-session notes so future agents pick them up.

```
archigraph_save_finding(
  question="How does the refund flow connect to billing?",
  answer="RefundService.issue → BillingGateway.reverse → stripe.charges.create",
  type="decision",
  nodes=["orders-api::refund-service-id", "billing-api::billing-gateway-id"]
)
```

#### `archigraph_list_findings`
Reads back previously saved findings, optionally filtered by entity or
timestamp. Call at the start of a session to load prior context before
querying the graph.

```
archigraph_list_findings(since="2026-05-01T00:00:00Z")
archigraph_list_findings(entity_id="ChargeService")
```

---

### 3.8 Diagnostics

#### `archigraph_get_telemetry`
Returns server uptime, per-tool call counters, error rates, and p50/p95
latency. Use only when debugging MCP server issues — not for normal workflows.

```
archigraph_get_telemetry()
→ { "uptime_ms": 1234567, "reload_count": 12, "tools": { "archigraph_find": { "p95_ms": 31.7 } } }
```

---

## 4. Pass-based workflows

Structure investigations as a series of passes, each with a clear scope and
exit condition. This prevents token waste from over-querying.

### Pass 0 — Orient (always)

```
archigraph_whoami()                          # confirm group + repo
archigraph_stats()                           # entity counts, any unavailable repos
archigraph_clusters()                        # module map
archigraph_list_findings(since="<last-week>")  # prior session context
```

Exit when: you know the group, the active repo, the approximate size, and
the top-level module names.

### Pass 1 — Locate

Find the entities relevant to your task using BM25.

```
archigraph_find(question="<your topic>", depth=1, token_budget=600)
```

Use `depth=1` or `depth=0` first to keep results tight. If no hits, broaden
the query. Note the entity IDs of top hits.

Exit when: you have a ranked list of candidate entities with IDs and file
locations.

### Pass 2 — Inspect and expand

Zoom in on the top candidates.

```
archigraph_inspect(label_or_id="<entity>")   # full record + attached findings
archigraph_expand(node="<entity>", depth=2)  # neighbours
```

Use `archigraph_get_source` when you need to read the actual code. Reserve
this for entities where graph metadata alone is insufficient.

Exit when: you understand the entity's role, its immediate dependencies, and
its callers.

### Pass 3 — Trace (if flow is the question)

When the question is about a process ("how does X reach Y?"):

```
archigraph_traces(action="list", cross_stack_only=true)   # find pre-computed flows
archigraph_traces(action="get", process_id="<id>")         # full step chain
# OR for ad-hoc:
archigraph_trace(source="<start>", target="<end>")
archigraph_traces(action="follow", entry_point_id="<start>", max_depth=6)
```

Exit when: you have a step-by-step chain from entry point to terminal, with
edge kinds.

### Pass 4 — Synthesise

Save your conclusions so future agents benefit.

```
archigraph_save_finding(
  question="<the question you answered>",
  answer="<your synthesis>",
  type="note",
  nodes=["<entity-id-1>", "<entity-id-2>"]
)
```

---

## 5. Common workflows with examples

### "Where is this HTTP endpoint implemented?"

```
# 1. Orient
archigraph_whoami()

# 2. List server-side definitions
archigraph_endpoints(action="definitions", repo_filter=["orders-api"])

# 3. Find the matching entity
archigraph_find(question="POST /api/v1/orders create order", depth=1)

# 4. Inspect + expand for auth and DB edges
archigraph_inspect(label_or_id="createOrder")
archigraph_expand(node="createOrder", depth=2)

# 5. Read source if needed
archigraph_get_source(node_id="createOrder", context_lines=40)
```

### "Trace a process flow end to end"

```
# 1. List pre-computed cross-stack flows
archigraph_traces(action="list", cross_stack_only=true, limit=15)

# 2. Get the full step chain for a candidate
archigraph_traces(action="get", process_id="orders-api::proc:df0cd633e7f8f7f4")

# 3. If the entry point is not pre-computed, follow from it
archigraph_traces(action="follow", entry_point_id="CheckoutController.submit",
                  max_depth=8, branching_factor=3)
```

### "Find orphan code (callers with no matching handler)"

```
archigraph_endpoints(action="stats")                          # check orphan_calls count
archigraph_endpoints(action="calls", orphan_only=true)        # list them
# For each orphan call:
archigraph_inspect(label_or_id="<orphan-entity-id>")
archigraph_expand(node="<orphan-entity-id>", depth=1)
```

### "Understand what changed since the last deploy"

```
archigraph_recent_activity(since="2026-05-20T10:00:00Z", limit=30)
# For high-impact changed entities:
archigraph_expand(node="<changed-entity>", depth=2)
```

### "Map cross-repo dependencies"

```
archigraph_stats()                                    # see cross_repo_links count
archigraph_cross_links(action="list", limit=20)       # inspect candidates
archigraph_trace(source="mobile-app::UIComponent",
                 target="api-backend::OrderService")  # verify a specific chain
```

### "Find all callers of a function"

```
# archigraph_find does BFS from the entity — callers appear as CALLS neighbours
archigraph_find(question="PaymentService.charge", depth=2, context_filter=["CALLS"])
# OR
archigraph_inspect(label_or_id="PaymentService.charge")  # check pagerank for importance
archigraph_expand(node="PaymentService.charge", depth=1)  # direct neighbours include callers
```

---

## 6. Anti-patterns

### Do not use archigraph for symbol lookup

```
# WRONG — grep is faster and exact:
archigraph_find(question="PAYMENT_GATEWAY_URL")

# RIGHT:
rg "PAYMENT_GATEWAY_URL" .
```

### Do not use archigraph_find with depth=3 for orientation

A wide BFS at depth=3 from a BM25 hit can return hundreds of entities and
blow the token budget. Start at `depth=1` or `depth=0` and expand only when
needed.

```
# WRONG for orientation:
archigraph_find(question="auth", depth=3, token_budget=2000)

# RIGHT:
archigraph_find(question="auth middleware", depth=1, token_budget=500)
```

### Do not skip Pass 0

Jumping straight to `archigraph_find` without `archigraph_whoami` can mean
you are querying the wrong group, or an unavailable repo. Pass 0 costs ~200
tokens and prevents misrouted queries.

### Do not use archigraph for reading a known file

If you already know the file path, use `Read` — it is cheaper and exact.
`archigraph_get_source` is for when you only know the entity label and want
the graph to resolve the file for you.

### Do not over-call archigraph_expand

`archigraph_expand` at depth 3 on a high-PageRank `Component` can return
thousands of edges. Cap depth at 2 for routine use; use depth=3 only when
specifically mapping a large subsystem.

### Do not use the old tool names

Tool names changed in #668. The following names no longer exist and will
return "tool not found" errors:

| Old name (broken) | Current name |
|-------------------|--------------|
| `archigraph_search` | `archigraph_find` |
| `archigraph_describe` | `archigraph_inspect` |
| `archigraph_related` | `archigraph_expand` |
| `archigraph_list_clusters` | `archigraph_clusters` |
| `archigraph_graph_stats` | `archigraph_stats` |
| `archigraph_list_enrichment_candidates` | `archigraph_enrichments(action=list)` |
| `archigraph_submit_enrichment` | `archigraph_enrichments(action=submit)` |
| `archigraph_reject_enrichment` | `archigraph_enrichments(action=reject)` |
| `archigraph_list_link_candidates` | `archigraph_cross_links(action=list)` |
| `archigraph_resolve_link_candidate` | `archigraph_cross_links(action=accept\|reject)` |
| `archigraph_list_residuals` | `archigraph_repairs(action=list)` |
| `archigraph_submit_repair` | `archigraph_repairs(action=submit)` |

---

## 7. Reading responses

### Entity IDs

- **Bare ID** (`a1b2c3d4e5f60718`) — single-repo scope. Safe to use without prefix.
- **Prefixed ID** (`orders-api::a1b2c3d4e5f60718`) — multi-repo response. Preserve
  the prefix when passing the ID back to another tool call.

### Entity kinds

The MCP strips the `SCOPE.` prefix. You will see `Component`, `Operation`,
`Schema`, `Queue`, etc. — not `SCOPE.Component`. The internal `graph.json`
uses the prefixed form; agent code should use the stripped form.

### Confidence values

`archigraph_trace` returns `weakest_link_confidence` — the lowest-confidence
edge along the path. Intra-repo edges default to 0.95; cross-repo overlay
edges default to 0.7 unless explicitly set. A path with confidence < 0.5
should be verified with `archigraph_get_source` before relying on it.

### `findings` on inspect/trace

`archigraph_inspect` and `archigraph_trace` auto-attach a `findings` array
of previously saved Q/A pairs that reference the queried entity. Read these
before querying further — a prior agent may have already answered your
question.

---

## 8. Scoping rules

| Scenario | How to scope |
|----------|-------------|
| Single repo question | `repo_filter=["<repo-slug>"]` |
| Cross-repo question | Omit `repo_filter` (default = whole group) |
| Different group | `group="<group-name>"` (rarely needed) |
| All repos explicitly | `repo_filter=["*"]` |

The daemon resolves your group from CWD by default (ADR-0008). If
`archigraph_whoami` returns `source: "none"`, the daemon could not infer a
group — provide `group=` explicitly or navigate to a registered repo.

---

## 9. Related skills

- `/generate-docs` — full documentation pipeline that uses archigraph at every
  pass. This skill is a prerequisite for understanding what `/generate-docs`
  is doing internally.
- `/archigraph-repair` — standalone repair flow for residual edges. Uses
  `archigraph_repairs` (Passes 1a, 1b, 3a in `/generate-docs`).
- `/archigraph-quality-check` — benchmarks archigraph MCP against grep+read
  before a docgen run.
- `/archigraph-patterns-discover` — finds and records structural patterns
  across the codebase.

## 10. References

- `internal/mcp/SCHEMA.md` — canonical tool contract (inputs, outputs, notes)
- ADR-0003 — SCOPE entity taxonomy (kind names)
- ADR-0008 — CWD-aware group routing
- ADR-0009 — Cross-repo ID namespacing (`<repo>::<localId>`)
- ADR-0015 — Residual-edge repair flow
- ADR-0018 — Agent-learned pattern store
