# Pass 1 — Inventory

---

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

Discover what is actually in each repo by querying grafel. Do not read source files directly. The graph is the source of truth for what entities exist; the writer passes will read source via `grafel_get_source` when they need verbatim snippets.

## Inputs

- `~/.grafel/groups/<group>/domain.md` from Pass 0.
- An grafel MCP session.

## Procedure

### Step 1 — Confirm group

Call `grafel_orient (view=me)`. Verify the resolved group matches what the user expected. If it does not, stop and ask the user to set `cwd` or pass `group` explicitly.

### Step 2 — Corpus metrics

For each repo `<r>` in the group:

```
grafel_orient(view="overview", repo_filter=["<r>"])
```

Record: total nodes, total edges, top entity kinds, top edge kinds, communities count, bridge-doc count.

### Step 3 — Community seeds

For each repo:

```
grafel_orient(view="clusters", repo_filter=["<r>"])
```

A community is a candidate "module" for Pass 2 to plan around. Capture community id, size, and the top-5 entities (by centrality) in each. This is your raw module list before grouping.

### Step 4 — Cross-repo bridges

```
grafel_cross_links(action=list, limit=50)
```

These are pending cross-repo edges. Note them — Pass 8 will resolve them, but writer subagents in Pass 4 should know they exist so they do not invent contradictory descriptions.

### Step 5 — Enrichment debt

For each repo:

```
grafel_docgen_apply(kind="enrichments", action=list, repo_filter=["<r>"], limit=20)
```

If anything blocks accurate documentation (e.g., an unresolved env-var, a class with unknown base), flag it. Pass 4 writers will need to either route around it or prompt the user during the deep-dive.

### Step 6 — Recent activity (optional)

If the user said "regenerate after the recent refactor", call `grafel_index_status(since=<timestamp>)` and tag those entities so Pass 2 prioritizes their modules.

## Output

Write `~/.grafel/groups/<group>/inventory.json`:

```json
{
  "group": "<group>",
  "generated_at": "<RFC3339>",
  "repos": [
    {
      "repo": "<slug>",
      "stats": { "nodes": 0, "edges": 0, "kinds": {}, "edge_kinds": {} },
      "communities": [
        { "id": "c1", "size": 0, "top_nodes": ["`Foo`", "`Bar`"] }
      ],
      "enrichment_debt": [
        { "candidate_id": "...", "kind": "...", "blocking": true }
      ]
    }
  ],
  "link_candidates": [
    { "candidate_id": "...", "from": "...", "to": "...", "method": "..." }
  ]
}
```

When the file is written, hand control back to the orchestrator with its path. Do not move on to Pass 2 yourself.

---

**[pass-01 telemetry]** Print at end of this pass:
```
[pass-01] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
