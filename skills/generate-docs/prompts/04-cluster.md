# Pass 4 — Per-module deep-dive

For every entry under `plan.passes.4_cluster.modules`, produce a self-contained module page (or set of pages). This pass runs writer subagents **in parallel** — one subagent per module — bounded by a configured concurrency limit.

> **Pass 3a hook active.** Before writing any paragraph that describes an entity, run the generation-time repair hook from `prompts/03a-generation-time-repair.md`. Auto-repair residuals where unambiguous; otherwise emit the documented "Runtime-resolved edge" callout from that prompt. Do not silently drop unresolved outbound edges.

## Inputs (per writer subagent)

- The single module entry from `plan.json` you are responsible for.
- The convention file named in that entry (under `conventions/`).
- `conventions/_graph-searchability.md` — universal; read first.
- `output-templates/module-readme.md` and `output-templates/flows.md`.
- `~/.archigraph/groups/<group>/domain.md`.

## Output

Per module:

```
<repo>/docs/modules/<module-slug>/
  README.md      # always (output-templates/module-readme.md)
  flows.md       # if the module has runtime flows worth diagramming
  api.md         # only if the module is the primary owner of a public API; otherwise Pass 5 owns api.md at the repo level
```

## Procedure

### Step 1 — Pull the module subgraph

Use the community ids in your plan entry:

```
archigraph_find(
  question="<module title> architecture",
  repo_filter=["<repo>"],
  depth=3,
  token_budget=<plan.token_budget>,
)
```

Then for each of the top-5 entities in the module:

```
archigraph_expand(node="<entity>", depth=2, repo_filter=["<repo>"])
```

These neighbors are what you describe in the module README's "Key entities" section.

### Step 2 — Find dynamic edges and process flows

The graph cannot see runtime-only couplings. The convention file lists places where these typically live for the stack (e.g., for `django.md`: signal connections, middleware, async tasks). For each, ask:

```
archigraph_find(
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
archigraph_traces(action=list, repo_filter=["<repo>"], limit=25)
```

This returns pre-computed BFS call chains from the indexer's pass over the CALLS graph. For each process whose `entry_id` falls within your module's community, either:

- Include the call chain directly in `flows.md` as a numbered list under a `## Process flows` section, OR
- Call `archigraph_traces(action=follow, entry_point_id=<id>, max_depth=8)` for entities that were not selected as pre-computed entry points.

Until #769 lands, `archigraph_traces` returns chains that stay within a single repo — describe cross-repo flows from `archigraph_cross_links` instead.

**New edge kinds to surface in prose.** archigraph now emits several richer edge kinds introduced in 2026-05. When you encounter these via `archigraph_expand` or `archigraph_find`, include the corresponding narrative:

- **`FETCHES`** (HTTP consumer → endpoint): "Frontend `X` FETCHES backend endpoint `Y` via `Z`." Include in `flows.md` under an "HTTP consumer flows" section.
- **`QUERIES`** (code → ORM table/column): "Service `A` QUERIES table `B` (columns `C`, `D`)." Include in `flows.md` under "Data access flows" or in `reference/api.md` if the module is the primary owner.
- **`PUBLISHES_TO`** (producer → broker): "Producer `X` PUBLISHES_TO topic `Y`." Include in `flows.md` under "Event flows" or "Message flows".
- **`SUBSCRIBES_TO`** (consumer → broker): "Consumer `C` SUBSCRIBES_TO topic `Y` to receive messages." Include alongside `PUBLISHES_TO` in "Event flows" or "Message flows".
- **`TRANSFORMS`** (stream processor): "Stream processor `S` TRANSFORMS topic `A` → topic `B`." Include in `flows.md` under "Event flows" or "Message flows".
- **Real-time edges** (`WS_SUBSCRIBES_TO`, `WS_CONNECTS`, `WS_EMITS`, `STREAMS_FROM`, `STREAMS_TO`, `GRAPHQL_SUBSCRIBES`, `GRAPHQL_PUBLISHES`): Document WebSocket, SSE, and GraphQL subscription flows. Examples: "Client `C` WS_SUBSCRIBES_TO server `S` to receive live updates on channel `X`"; "GraphQL subscription server `S` GRAPHQL_PUBLISHES events to subscriber `C`."

When you find entities of kind `Queue` (generic message broker abstraction, e.g., RabbitMQ, SQS, Google Pub-Sub) or `MessageTopic` (Kafka-specific topic), treat them as message destinations and document them in the event-flows section rather than the data-model section. Note the distinction: `Queue` is a broker-agnostic concept, while `MessageTopic` is Kafka-specific.

### Step 3 — Pull source where needed

Within your `source_snippets` budget, use `archigraph_get_source(node_id=<id>, context_lines=20)` for entities whose intent is unclear from name + neighbors alone. Quote at most ~10 lines per snippet in the doc; reference the file path in backticks.

### Step 4 — Render

Fill the templates strictly. Do not invent extra top-level sections. The conventions file may add stack-specific sections (e.g., Django adds a "Migrations" section); follow the convention but do not stray.

### Step 5 — Cross-link

If your module references entities owned by another module, link to that module's `README.md`. If those references cross repo boundaries, render them per `snippets/cross-link-format.md` instead.

### Step 6 — Verification

Run every check in `snippets/verification-checklist.md`. The orchestrator will reject your output otherwise.

### Step 7 — Save

```
archigraph_save_finding(
  question="What does the <module-slug> module do in <repo>?",
  answer="<file: <repo>/docs/modules/<module-slug>/README.md>",
  type="module",
  nodes=["<top-entity-1>", "<top-entity-2>"],
  repo_filter=["<repo>"],
)
```

Hand control back to the orchestrator. The orchestrator joins all writer subagents and starts Pass 5 only when every module has produced verified output.
