# Pass 7 — Group synthesis

---

## Staging path

Read `run_id` and `staging_path` from `~/.grafel/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.grafel/docs/<group>/`. Wherever this prompt says `~/.grafel/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_expand`, `grafel_stats`,
`grafel_clusters`, `grafel_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_whoami` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

Tie the per-repo outputs into one group-level page. This page is what an executive, a new hire, or an external consumer reads first.

## Inputs

- `~/.grafel/groups/<group>/domain.md`
- Every `~/.grafel/docs/<group>/<repo-slug>/overview.md` produced in Pass 3
- Every `~/.grafel/groups/<group>/cross-cutting/<topic>.md` produced in Pass 6
- `output-templates/group-synthesis.md`

## Output

```
~/.grafel/groups/<group>/docs/group-synthesis.md
```

## Procedure

### Step 1 — Cross-group queries

```
grafel_find(question="how do these services communicate", repo_filter=null, depth=3, token_budget=1500)
grafel_find(question="cross-repo dependencies", repo_filter=null, depth=3, token_budget=1200)
```

`repo_filter=null` triggers the cross-group summary-first behavior described in `SKILL.md`.

### Step 2 — Confirm cross-repo edges

```
grafel_cross_links(action=list, limit=100)
```

Anything with `status=accepted` is a confirmed cross-repo edge — describe these in the synthesis. Pending candidates are not facts; mention them only as "potential coupling under review."

### Step 3 — Render

Fill `output-templates/group-synthesis.md`. Required sections:

1. **What this group does** — one-paragraph mission lifted from `domain.md`.
2. **Repos at a glance** — table from `domain.md` repos block.
3. **Runtime communication map** — describe the synchronous and asynchronous edges across repos. Include:
   - HTTP call chains surfaced via `grafel_traces` (process flows). Note: until #769 lands, only single-repo chains are available; describe cross-repo flows via `grafel_cross_links` instead.
   - `FETCHES` edges: which frontend/consumer calls which backend endpoint.
   - `PUBLISHES_TO` / `SUBSCRIBES_TO` / `TRANSFORMS` edges: event flows through `Queue` (generic brokers) and `MessageTopic` (Kafka-specific) entities.
   - Real-time edges (`WS_SUBSCRIBES_TO`, `WS_EMITS`, `GRAPHQL_SUBSCRIBES`, `GRAPHQL_PUBLISHES`, `STREAMS_FROM`, `STREAMS_TO`): WebSocket, SSE, and GraphQL subscription flows.
   - `QUERIES` edges: which services access which data stores.
   Use mermaid only if it does not duplicate prose.
4. **Dynamic couplings** — the ADR-0007 bridge edges. Each bullet names both ends in backticks.
5. **Cross-cutting summary** — one paragraph per cross-cutting topic, linking down to the per-topic aggregator.
6. **Where to look next** — links into per-repo `overview.md` files.

### Step 4 — Backtick discipline

Every code identifier in every heading must be backticked. The synthesis page is the single biggest accelerator of cross-repo bridge edges in the graph; missing backticks here cost more than anywhere else.

### Step 5 — Save

```
grafel_save_finding(
  question="What is the synthesized architecture of the <group> group?",
  answer="<file: ~/.grafel/groups/<group>/docs/group-synthesis.md>",
  type="synthesis",
)
```

Hand back to the orchestrator.

---

**[pass-07 telemetry]** Print at end of this pass:
```
[pass-07] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
