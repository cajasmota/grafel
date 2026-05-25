# Pass 7 — Group synthesis

---

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use archigraph MCP tools:
`archigraph_inspect`, `archigraph_find`, `archigraph_expand`, `archigraph_stats`,
`archigraph_clusters`, `archigraph_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `archigraph_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `archigraph_whoami` before doing anything else in this pass. If it errors:
ABORT with: "archigraph MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."

## CRITICAL STORAGE DISCIPLINE
===========================
All generated documentation MUST be written under:
  `~/.archigraph/docs/<group>/...`

Determine `<group>` via the `archigraph_whoami` MCP call (the Pre-flight assertion
above). Pass it through every subsequent file write as `${OUTPUT_ROOT}`.

You are STRICTLY FORBIDDEN from writing documentation files into:
- The source repo's working tree (anywhere under `<repo>/docs/`, `<repo>/doc/`, etc.)
- The CWD unless CWD is already inside `~/.archigraph/docs/<group>/`
- Any path that is a git working directory

If you find yourself about to write to a repo path, STOP. The skill assumes
the archigraph-owned store. Writing elsewhere breaks the storage contract
and pollutes the user's source repo.

The daemon dashboard reads from `~/.archigraph/docs/<group>/` -- any output
written elsewhere is invisible to it.

### Pre-flight storage assertion -- SECOND action in this pass

Compute and verify the output root immediately after the `archigraph_whoami` call:

```bash
OUTPUT_ROOT="$HOME/.archigraph/docs/<group>/"   # substitute <group> from whoami
mkdir -p "$OUTPUT_ROOT"
echo "OUTPUT_ROOT=$OUTPUT_ROOT"
```

All file writes in this pass MUST use `${OUTPUT_ROOT}<relative-path>`. Never write to any
other location. If `mkdir -p` fails, ABORT: "Cannot create output directory at $OUTPUT_ROOT."
## CRITICAL OUTPUT DISCIPLINE
==========================
The generate-docs skill produces markdown files in the canonical store
at `~/.archigraph/docs/<group>/`. It does NOT produce:
- VitePress / Docusaurus / Sphinx / mkdocs scaffolding
- `package.json` or any build manifests for static site generators
- Any non-markdown asset that wraps the docs for publishing
- `.gitignore` entries

Publishing is downstream — handled by the archigraph dashboard or
external tooling. If you find yourself about to write a `config.ts`,
`package.json`, `mkdocs.yml`, `.vitepress/config.ts`, or any build
manifest, STOP. The skill's job is content, not infrastructure.




---


Tie the per-repo outputs into one group-level page. This page is what an executive, a new hire, or an external consumer reads first.

## Inputs

- `~/.archigraph/groups/<group>/domain.md`
- Every `~/.archigraph/docs/<group>/<repo-slug>/overview.md` produced in Pass 3
- Every `~/.archigraph/groups/<group>/cross-cutting/<topic>.md` produced in Pass 6
- `output-templates/group-synthesis.md`

## Output

```
~/.archigraph/groups/<group>/docs/group-synthesis.md
```

## Procedure

### Step 1 — Cross-group queries

```
archigraph_find(question="how do these services communicate", repo_filter=null, depth=3, token_budget=1500)
archigraph_find(question="cross-repo dependencies", repo_filter=null, depth=3, token_budget=1200)
```

`repo_filter=null` triggers the cross-group summary-first behavior described in `SKILL.md`.

### Step 2 — Confirm cross-repo edges

```
archigraph_cross_links(action=list, limit=100)
```

Anything with `status=accepted` is a confirmed cross-repo edge — describe these in the synthesis. Pending candidates are not facts; mention them only as "potential coupling under review."

### Step 3 — Render

Fill `output-templates/group-synthesis.md`. Required sections:

1. **What this group does** — one-paragraph mission lifted from `domain.md`.
2. **Repos at a glance** — table from `domain.md` repos block.
3. **Runtime communication map** — describe the synchronous and asynchronous edges across repos. Include:
   - HTTP call chains surfaced via `archigraph_traces` (process flows). Note: until #769 lands, only single-repo chains are available; describe cross-repo flows via `archigraph_cross_links` instead.
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
archigraph_save_finding(
  question="What is the synthesized architecture of the <group> group?",
  answer="<file: ~/.archigraph/groups/<group>/docs/group-synthesis.md>",
  type="synthesis",
)
```

Hand back to the orchestrator.

---

**[pass-07 telemetry]** Print at end of this pass:
```
[pass-07] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
