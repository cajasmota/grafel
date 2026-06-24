# Pass 4 — Per-module deep-dive

---

## Staging path

Read `run_id` and `staging_path` from `~/.grafel/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.grafel/docs/<group>/`. Wherever this prompt says `~/.grafel/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_subgraph`, `grafel_orient (view=overview)`,
`grafel_orient (view=clusters)`, `grafel_orient (view=me)`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_orient (view=me)` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

For every entry under `plan.passes.4_cluster.modules`, produce a self-contained module page (or set of pages). This pass runs writer subagents **in parallel** — one subagent per module — bounded by a configured concurrency limit.

> **Pass 3a hook active.** Before writing any paragraph that describes an entity, run the generation-time repair hook from `prompts/03a-generation-time-repair.md`. Auto-repair residuals where unambiguous; otherwise emit the documented "Runtime-resolved edge" callout from that prompt. Do not silently drop unresolved outbound edges.

## Inputs (per writer subagent)

- The single module entry from `plan.json` you are responsible for.
- The convention file named in that entry (under `conventions/`).
- `conventions/_graph-searchability.md` — universal; read first.
- `output-templates/module-readme.md` and `output-templates/flows.md`.
- `~/.grafel/groups/<group>/domain.md`.

## Output

Per module:

```
~/.grafel/docs/<group>/<repo-slug>/modules/<module-slug>/
  README.md      # always (output-templates/module-readme.md)
  flows.md       # if the module has runtime flows worth diagramming
  api.md         # only if the module is the primary owner of a public API; otherwise Pass 5 owns api.md at the repo level
```

## Procedure

### Step 1 — Pull the module subgraph

Use the community ids in your plan entry:

```
grafel_find(
  question="<module title> architecture",
  repo_filter=["<repo>"],
  depth=3,
  token_budget=<plan.token_budget>,
)
```

Then for each of the top-5 entities in the module:

```
grafel_subgraph(node="<entity>", depth=2, repo_filter=["<repo>"])
```

These neighbors are what you describe in the module README's "Key entities" section.

### Step 2 — Find dynamic edges and process flows

The graph cannot see runtime-only couplings. The convention file lists places where these typically live for the stack (e.g., for `django.md`: signal connections, middleware, async tasks). For each, ask:

```
grafel_find(
  question="<dynamic-edge-pattern>",
  repo_filter=["<repo>"],
  depth=2,
  token_budget=400,
)
```

When you find one, name both ends of the connection in a backticked heading inside `flows.md` so the slug-collision rule (ADR-0007) bridges them in the graph. Example:

```markdown
## How `OrderCreated` reaches `BillingService`
```

**Process flows (added in #724).** For modules that own entry points (HTTP route handlers, scheduled jobs, message consumers), call:

```
grafel_trace(action=list, repo_filter=["<repo>"], limit=25)
```

This returns pre-computed BFS call chains from the indexer's pass over the CALLS graph. For each process whose `entry_id` falls within your module's community, either:

- Include the call chain directly in `flows.md` as a numbered list under a `## Process flows` section, OR
- Call `grafel_trace(action=follow, entry_point_id=<id>, max_depth=8)` for entities that were not selected as pre-computed entry points.

Until #769 lands, `grafel_trace` returns chains that stay within a single repo — describe cross-repo flows from `grafel_cross_links` instead.

**New edge kinds to surface in prose.** grafel now emits several richer edge kinds introduced in 2026-05. When you encounter these via `grafel_subgraph` or `grafel_find`, include the corresponding narrative:

- **`FETCHES`** (HTTP consumer → endpoint): "Frontend `X` FETCHES backend endpoint `Y` via `Z`." Include in `flows.md` under an "HTTP consumer flows" section.
- **`QUERIES`** (code → ORM table/column): "Service `A` QUERIES table `B` (columns `C`, `D`)." Include in `flows.md` under "Data access flows" or in `reference/api.md` if the module is the primary owner.
- **`PUBLISHES_TO`** (producer → broker): "Producer `X` PUBLISHES_TO topic `Y`." Include in `flows.md` under "Event flows" or "Message flows".
- **`SUBSCRIBES_TO`** (consumer → broker): "Consumer `C` SUBSCRIBES_TO topic `Y` to receive messages." Include alongside `PUBLISHES_TO` in "Event flows" or "Message flows".
- **`TRANSFORMS`** (stream processor): "Stream processor `S` TRANSFORMS topic `A` → topic `B`." Include in `flows.md` under "Event flows" or "Message flows".
- **Real-time edges** (`WS_SUBSCRIBES_TO`, `WS_CONNECTS`, `WS_EMITS`, `STREAMS_FROM`, `STREAMS_TO`, `GRAPHQL_SUBSCRIBES`, `GRAPHQL_PUBLISHES`): Document WebSocket, SSE, and GraphQL subscription flows. Examples: "Client `C` WS_SUBSCRIBES_TO server `S` to receive live updates on channel `X`"; "GraphQL subscription server `S` GRAPHQL_PUBLISHES events to subscriber `C`."

When you find entities of kind `Queue` (generic message broker abstraction, e.g., RabbitMQ, SQS, Google Pub-Sub) or `MessageTopic` (Kafka-specific topic), treat them as message destinations and document them in the event-flows section rather than the data-model section. Note the distinction: `Queue` is a broker-agnostic concept, while `MessageTopic` is Kafka-specific.

**Empty-result rule (#1618 — the top quality failure).** When `grafel_related (direction=callees)`, `grafel_related (direction=callers)`, `grafel_subgraph`, or `grafel_trace(action=follow)` returns an empty edge list for a valid entity, the response carries `"result": "no_outgoing_edges"` / `"no_incoming_edges"` / `"no_edges"` / `"no_outgoing_calls"`. This is an explicit graph signal, not a gap to fill.

Required behaviour:
- **State the absence verbatim**: "The graph records no callee/edge for `X`."
- **Never** infer a plausible relationship from training data, context, or naming convention to fill the gap. Confident fabrication ("create() does an ORM save", "store calls LoginSerializer") is the most common quality failure and the hardest to catch after the fact.
- If you suspect an extraction gap (the connection may exist in code but was not indexed), say so explicitly and mark it unverified — do not state it as fact.
- A `result` field on a traversal response means: **the entity was found, the graph was queried, nothing was returned.** This is different from `entity not found` (which is an error).

### Step 3 — Pull source where needed

Within your `source_snippets` budget, use `grafel_get_source(node_id=<id>, context_lines=20)` for entities whose intent is unclear from name + neighbors alone. Quote at most ~10 lines per snippet in the doc; reference the file path in backticks.

### Step 4 — Render

Fill the templates strictly. Do not invent extra top-level sections. The conventions file may add stack-specific sections (e.g., Django adds a "Migrations" section); follow the convention but do not stray.

### Step 5 — Cross-link

If your module references entities owned by another module, link to that module's `README.md`. If those references cross repo boundaries, render them per `snippets/cross-link-format.md` instead.

### Step 6 — Verification

Run every check in `snippets/verification-checklist.md`. The orchestrator will reject your output otherwise.

### Step 7 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`. This pass is
the richest source of repair candidates because it reads the deepest source
context (Step 3 pulled source for many entities via `grafel_get_source`).

Collect every observation made during Steps 1–6:
- Stubs in the module subgraph that resolved to imports you actually read.
- Dynamic dispatch sites (event bus, string registries, dependency-injection
  factories) whose callees you identified from source.
- Entities in the module whose `kind` contradicts what you saw in source.
- Outbound edges to well-known external libraries you recognised.
- Two flows in the module that represent the same workflow under different names.

Append candidates to `~/.grafel/groups/<group>/docgen-repairs.jsonl`.
Use `source: "generate-docs/pass-4"` in every candidate.

### Step 8 — Save

```
grafel_findings(action="save", question="What does the <module-slug> module do in <repo>?",
  answer="<file: ~/.grafel/docs/<group>/<repo-slug>/modules/<module-slug>/README.md>",
  type="module",
  nodes=["<top-entity-1>", "<top-entity-2>"],
  repo_filter=["<repo>"],)
```

Hand control back to the orchestrator. The orchestrator joins all writer subagents and starts Pass 5 only when every module has produced verified output.

---

**[pass-04 telemetry]** Print at end of this pass:
```
[pass-04] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
