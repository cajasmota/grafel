# Pass 1 — Inventory

Discover what is actually in each repo by querying archigraph. Do not read source files directly. The graph is the source of truth for what entities exist; the writer passes will read source via `archigraph_get_source` when they need verbatim snippets.

## Inputs

- `~/.archigraph/groups/<group>/domain.md` from Pass 0.
- An archigraph MCP session.

## Procedure

### Step 1 — Confirm group

Call `archigraph_whoami`. Verify the resolved group matches what the user expected. If it does not, stop and ask the user to set `cwd` or pass `group` explicitly.

### Step 2 — Corpus metrics

For each repo `<r>` in the group:

```
archigraph_stats(repo_filter=["<r>"])
```

Record: total nodes, total edges, top entity kinds, top edge kinds, communities count, bridge-doc count.

### Step 3 — Community seeds

For each repo:

```
archigraph_clusters(repo_filter=["<r>"])
```

A community is a candidate "module" for Pass 2 to plan around. Capture community id, size, and the top-5 entities (by centrality) in each. This is your raw module list before grouping.

### Step 4 — Cross-repo bridges

```
archigraph_cross_links(action=list, limit=50)
```

These are pending cross-repo edges. Note them — Pass 8 will resolve them, but writer subagents in Pass 4 should know they exist so they do not invent contradictory descriptions.

### Step 5 — Enrichment debt

For each repo:

```
archigraph_enrichments(action=list, repo_filter=["<r>"], limit=20)
```

If anything blocks accurate documentation (e.g., an unresolved env-var, a class with unknown base), flag it. Pass 4 writers will need to either route around it or prompt the user during the deep-dive.

### Step 6 — Recent activity (optional)

If the user said "regenerate after the recent refactor", call `archigraph_recent_activity(since=<timestamp>)` and tag those entities so Pass 2 prioritizes their modules.

## Output

Write `~/.archigraph/groups/<group>/inventory.json`:

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
